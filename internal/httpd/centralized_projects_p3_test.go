package httpd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_CentralizedProjects_P3(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	repo := repository.NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(repository.CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, repository.StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, repository.TaskInput{Name: "录入", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, repository.FileRuleInput{FileName: "录入登记表", DataState: "process", AllowedFileTypes: "docx"})

	root := t.TempDir()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyProjectRoot, root)

	var startBody, myTasksAssignee, markBody, taskUnreadAssignee string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "my-tasks"):
			myTasksAssignee = req.URL.Query().Get("assignee")
		case strings.Contains(req.URL.Path, "start-task"):
			b, _ := io.ReadAll(req.Body)
			startBody = string(b)
		case strings.Contains(req.URL.Path, "mark-tasks-seen"):
			b, _ := io.ReadAll(req.Body)
			markBody = string(b)
		case strings.Contains(req.URL.Path, "task-unread-count"):
			taskUnreadAssignee = req.URL.Query().Get("assignee")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"count":1,"updated":1}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/my-tasks")
	successOk(t, status, resp)
	if myTasksAssignee != "u1" {
		t.Errorf("my-tasks assignee=%q want u1", myTasksAssignee)
	}

	status, resp = jsonReq(t, r, "POST", "/centralized-projects/mark-tasks-seen", map[string]interface{}{})
	successOk(t, status, resp)
	if !strings.Contains(markBody, "\"assignee\":\"u1\"") {
		t.Errorf("mark-tasks-seen body=%s", markBody)
	}

	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/task-unread-count")
	successOk(t, status, resp)
	if taskUnreadAssignee != "u1" {
		t.Errorf("task-unread-count assignee=%q want u1", taskUnreadAssignee)
	}

	status, resp = jsonReq(t, r, "POST", "/centralized-projects/start-task", map[string]interface{}{
		"application_id": 555, "stage_code": st.StageCode, "task_code": tk.TaskCode,
		"template_code": tpl.TemplateCode, "template_version": tpl.TemplateVersion,
	})
	successOk(t, status, resp)
	if !strings.Contains(startBody, "\"actor\":\"u1\"") {
		t.Errorf("start-task 应注入 actor=u1，body=%s", startBody)
	}
	ws := repository.NewProjectWorkspace(root)
	// 五层落盘：过程占位落到 stages/{stage}/{task}/process/
	if _, err := os.Stat(filepath.Join(ws.TaskStateDir("CPA-555", st.StageCode, tk.TaskCode, "process"), "录入登记表.docx")); err != nil {
		t.Fatalf("start-task 应建任务过程文件: %v", err)
	}

	// complete-task 代理：注入 actor=operator 并转发 manage。
	var completeBody string
	manageDone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "complete-task") {
			b, _ := io.ReadAll(req.Body)
			completeBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"ok":true}}`))
	}))
	defer manageDone.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manageDone.URL)

	status, resp = jsonReq(t, r, "POST", "/centralized-projects/complete-task", map[string]interface{}{
		"application_id": 555, "stage_code": st.StageCode, "task_code": tk.TaskCode,
	})
	successOk(t, status, resp)
	if !strings.Contains(completeBody, "\"actor\":\"u1\"") {
		t.Errorf("complete-task 应注入 actor=u1，body=%s", completeBody)
	}
}

// TestHTTP_StartTask_FetchesMissingTemplate 证明：参与人本地无该模版时，
// start-task 会自动经 TemplateFetcher.FetchByCode 从 manage /api/templates/full 拉取并落地，
// 随后 start-task 成功（不再因本地缺模版而报错）。
func TestHTTP_StartTask_FetchesMissingTemplate(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u9")

	root := t.TempDir()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyProjectRoot, root)

	const tplCode = "TPL-REMOTE-ONLY"
	const tplVer = "V1.0"
	const stageCode = "MZ-SG"
	const taskCode = "TASK-IN"

	// mock manage：同时处理 start-task 与 /api/templates/full。
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.URL.Path, "/api/templates/full"):
			if req.URL.Query().Get("code") != tplCode {
				t.Errorf("templates/full code=%q want %q", req.URL.Query().Get("code"), tplCode)
			}
			full := repository.ManageFullResponse{
				Code: 0, Message: "success",
				Data: &repository.ManageFullStructure{
					Template: &repository.ManageTemplate{
						ID: 1, TemplateCode: tplCode, TemplateName: "远端模版",
						TemplateVersion: tplVer, Publisher: "provider", Status: "active",
						ProjectSensitivityLevel: "important",
					},
					Stages: []repository.ManageStage{{
						ID: 10, StageCode: stageCode, StageName: "收稿登记", StageType: "process", SortOrder: 1,
						Tasks: []repository.ManageTask{
							{ID: 50, TaskCode: taskCode, TaskName: "录入", SortOrder: 0},
						},
						FileRules: []repository.ManageFileRule{
							{ID: 100, FileRuleCode: "PROC-001", FileName: "录入登记表", DataState: "process", Required: 1, AllowedFileTypes: `["docx"]`, SortOrder: 1, TaskCode: strPtr(taskCode)},
						},
					}},
				},
			}
			_ = json.NewEncoder(w).Encode(full)
		default: // start-task 等
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		}
	}))
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	// 本地无该模版。
	var before int
	_ = db.Get(&before, `SELECT COUNT(*) FROM data_templates WHERE template_code = ?`, tplCode)
	if before != 0 {
		t.Fatalf("前置：本地不应存在模版 %s", tplCode)
	}

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/start-task", map[string]interface{}{
		"application_id": 777, "stage_code": stageCode, "task_code": taskCode,
		"template_code": tplCode, "template_version": tplVer,
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}

	// 核心断言：start-task 触发了自动 FetchByCode，把远端模版落地到本地缓存。
	var after int
	_ = db.Get(&after, `SELECT COUNT(*) FROM data_templates WHERE template_code = ? AND disable = 0`, tplCode)
	if after == 0 {
		t.Fatalf("start-task 应自动 FetchByCode 落地本地模版 %s（拉取未生效）", tplCode)
	}
	// 不再因「本地缺少该模版且自动同步失败」而报错——自动同步这一步已成功。
	if errMsg, _ := resp["error"].(string); strings.Contains(errMsg, "本地缺少该模版") {
		t.Fatalf("自动同步应已成功，不应再报缺模版：%q", errMsg)
	}
	// fetcher 的 persist 现已镜像 template_tasks 层并回填 template_file_rules.template_task_id，
	// 故远端拉取的模版同样支持「任务级」建过程文件：start-task 应在该任务的 process 目录建出占位文件。
	ws := repository.NewProjectWorkspace(root)
	// 五层落盘：过程占位落到 stages/{stage}/{task}/process/
	procFile := filepath.Join(ws.TaskStateDir("CPA-777", stageCode, taskCode, "process"), "录入登记表.docx")
	if _, err := os.Stat(procFile); err != nil {
		t.Fatalf("远端模版任务级 start-task 应建过程文件 %s: %v", procFile, err)
	}
}

// strPtr 返回字符串指针（用于可选 JSON 字段构造）。
func strPtr(s string) *string { return &s }

// TestHTTP_StartTask_MissingTemplateFetchFails 兜底路径：本地无模版且自动同步失败时，
// 返回清晰、可操作的错误（提及「模版」），而非裸 SQL 错误。
func TestHTTP_StartTask_MissingTemplateFetchFails(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u8")

	root := t.TempDir()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyProjectRoot, root)

	// mock manage：start-task 成功，但 /api/templates/full 返回错误。
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.URL.Path, "/api/templates/full") {
			_, _ = w.Write([]byte(`{"code":404,"message":"模版不存在","data":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
	}))
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/start-task", map[string]interface{}{
		"application_id": 888, "stage_code": "MZ-SG", "task_code": "TASK-IN",
		"template_code": "TPL-NOPE", "template_version": "V1.0",
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if resp["success"] == true {
		t.Fatalf("缺模版且同步失败应 success:false，resp=%v", resp)
	}
	errMsg, _ := resp["error"].(string)
	if !strings.Contains(errMsg, "模版") {
		t.Fatalf("错误应提及「模版」且可操作，实得：%q", errMsg)
	}
}
