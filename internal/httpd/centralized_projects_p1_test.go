package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 三个代理接口：转发到 manage，注入 operator/owner。
func TestHTTP_CentralizedProjects_P1Proxies(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	var gotUnreadOwner, gotSetTplBody, gotMarkSeenBody string
	var ingestCalled bool
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.URL.Path, "unread-count"):
			gotUnreadOwner = req.URL.Query().Get("owner")
		case strings.Contains(req.URL.Path, "templates/ingest"):
			// set-template 现在先把模版结构 ingest 进 manage，返回 manage 侧 template_id。
			ingestCalled = true
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"template_id":77}}`))
			return
		case strings.Contains(req.URL.Path, "set-template"):
			buf, _ := io.ReadAll(req.Body)
			gotSetTplBody = string(buf)
		case strings.Contains(req.URL.Path, "mark-seen"):
			buf, _ := io.ReadAll(req.Body)
			gotMarkSeenBody = string(buf)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"count":3,"updated":2,"id":1}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 建一个最简本地模版（1 环节 1 任务 1 标识），供 set-template ingest 用。
	authRepo := repository.NewTemplateAuthoringRepository(db)
	tpl, err := authRepo.CreateLocalTemplate(repository.CreateTemplateInput{
		Scope: "unit", ClassCode: "IND-001", TemplateName: "测试模版", ShortCode: "TST",
		Manager: "lead", Owner: "院", SensitivityLevel: "general", ApprovalBasis: "x", Description: "d",
	})
	if err != nil {
		t.Fatalf("建本地模版失败: %v", err)
	}
	stage, err := authRepo.CreateStage(tpl.ID, repository.StageInput{Name: "收稿", Manager: "lead"})
	if err != nil {
		t.Fatalf("建环节失败: %v", err)
	}
	task, err := authRepo.CreateTask(stage.ID, repository.TaskInput{Name: "录入", SensitivityLevel: "general"})
	if err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
	if _, err := authRepo.CreateFileRule(task.ID, repository.FileRuleInput{FileName: "原稿", DataState: "input", AllowedFileTypes: "PDF"}); err != nil {
		t.Fatalf("建标识失败: %v", err)
	}

	// unread-count → owner=lead in query
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/unread-count")
	successOk(t, status, resp)
	if dataMap(t, resp)["count"] == nil {
		t.Errorf("unread-count 应回 count: %+v", resp)
	}
	if gotUnreadOwner != "lead" {
		t.Errorf("unread-count 应以 owner=lead 转发，实得 %q", gotUnreadOwner)
	}

	// mark-seen → owner=lead in body
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/mark-seen", map[string]interface{}{})
	successOk(t, status, resp)
	if !strings.Contains(gotMarkSeenBody, "\"owner\":\"lead\"") {
		t.Errorf("mark-seen 应在 body 注入 owner=lead，实得 %s", gotMarkSeenBody)
	}

	// set-template（本地模版）→ 先 ingest 进 manage，再用 manage 侧 template_id 调 set-template，注入 acceptor=lead
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/set-template?id=1", map[string]interface{}{
		"template_id": tpl.ID, "template_code": tpl.TemplateCode, "template_version": tpl.TemplateVersion, "source": "local",
	})
	successOk(t, status, resp)
	if !ingestCalled {
		t.Errorf("set-template 应先把模版结构 ingest 进 manage")
	}
	if !strings.Contains(gotSetTplBody, "\"acceptor\":\"lead\"") {
		t.Errorf("set-template 应注入 acceptor=lead，body=%s", gotSetTplBody)
	}
	// 透传的 template_id 应是 manage 侧返回的 77（不是 scan 本地 id）
	if !strings.Contains(gotSetTplBody, "\"template_id\":77") {
		t.Errorf("set-template 应使用 manage 侧 template_id=77，body=%s", gotSetTplBody)
	}
}
