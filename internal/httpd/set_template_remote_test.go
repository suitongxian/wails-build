package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 选「在线」模版分工：set-template 应按 code 从模版服务器拉结构 → ingest 进 manage →
// 用 manage 侧 template_id 调 set-template。这样后续 stage-tasks 才查得到文件任务。
func TestHTTP_SetTemplate_RemoteSource(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	// 模版服务器（template-manage :19092）：list 定位 id + tree 给带 code 的五层。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/local-templates/list":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":[{"id":7,"template_code":"SMYS","template_name":"书目印刷计划","template_version":"V1.0","status":"active"}]}`))
		case "/api/local-templates/tree":
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"template":{"template_code":"SMYS","template_name":"书目印刷计划","template_version":"V1.0","project_sensitivity_level":"core_secret","status":"active"},"stages":[{"stage_code":"S1","stage_name":"收稿","stage_type":"process","sort_order":1,"tasks":[{"task_code":"TK-1","task_name":"录入","sort_order":1,"file_rules":[{"file_rule_code":"IN-001","file_name":"原稿","data_state":"input","required":1,"allowed_file_types":"PDF","sort_order":1}]}]}]}}`))
		default:
			t.Errorf("非预期模版服务器路径: %s", req.URL.Path)
		}
	}))
	defer ts.Close()

	// manage（data-asset-manage :19091）：接 ingest（断言带到了 stages/tasks）+ set-template。
	var ingestBody, setTplBody string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.URL.Path, "templates/ingest"):
			buf, _ := io.ReadAll(req.Body)
			ingestBody = string(buf)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"template_id":88}}`))
		case strings.Contains(req.URL.Path, "set-template"):
			buf, _ := io.ReadAll(req.Body)
			setTplBody = string(buf)
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":1}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
		}
	}))
	defer manage.Close()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)
	cfg.SetValue(repository.KeyTemplateServerEndpoint, ts.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/set-template?id=1", map[string]interface{}{
		"template_code": "SMYS", "template_version": "V1.0", "source": "template-server",
	})
	successOk(t, status, resp)

	// ingest 应带上从模版服务器拉到的环节/任务
	if !strings.Contains(ingestBody, "\"stage_code\":\"S1\"") || !strings.Contains(ingestBody, "\"task_code\":\"TK-1\"") {
		t.Errorf("ingest 应携带模版服务器的环节/任务，body=%s", ingestBody)
	}
	// 关联即克隆：ingest/set-template 应指向「项目专属模版」TPL-PRJ-1（而非共享模版 SMYS），
	// 但环节/任务编码保留（上面已断言 S1/TK-1 在 ingest body 中）。
	if !strings.Contains(ingestBody, "\"template_code\":\"TPL-PRJ-1\"") {
		t.Errorf("ingest 应指向项目专属模版 TPL-PRJ-1，body=%s", ingestBody)
	}
	if !strings.Contains(setTplBody, "\"template_code\":\"TPL-PRJ-1\"") {
		t.Errorf("set-template 应指向项目专属模版 TPL-PRJ-1，body=%s", setTplBody)
	}
	// set-template 应使用 manage 侧 id=88
	if !strings.Contains(setTplBody, "\"template_id\":88") {
		t.Errorf("set-template 应使用 manage 侧 template_id=88，body=%s", setTplBody)
	}
	if !strings.Contains(setTplBody, "\"acceptor\":\"lead\"") {
		t.Errorf("set-template 应注入 acceptor=lead，body=%s", setTplBody)
	}

	// 模版应已落入 scan 本地缓存（FetchFromTemplateServer 的副作用）
	cacheRepo := repository.NewTemplateCacheRepository(db)
	list, _ := cacheRepo.ListTemplates("")
	var found bool
	for _, tp := range list {
		if tp.TemplateCode == "SMYS" {
			found = true
		}
	}
	if !found {
		t.Errorf("在线模版应已写入 scan 本地缓存")
	}
}
