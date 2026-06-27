package httpd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 提取项目认定模版：门禁走云端基线对比（不依赖本地 edited 标记）。
// 无改动→拒绝；有改动→提取产出 certified=1 的本地权威模版。
func TestHTTP_ExtractCertifiedTemplate(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	repo := repository.NewTemplateAuthoringRepository(db)
	src, _ := repo.CreateLocalTemplate(repository.CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(src.ID, repository.StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, repository.TaskInput{Name: "录入"})
	fr, _ := repo.CreateFileRule(tk.ID, repository.FileRuleInput{FileName: "原稿", DataState: "input", AllowedFileTypes: "PDF"})
	repo.CloneLocalTemplateForApplication(src.ID, "7") // 写本地基线（源结构）

	// 最终结构由 finalJSON 控制：先与基线一致（无改动），再改名（有改动）。
	unchanged := manageTreeJSON(st.StageCode, "收稿", tk.TaskCode, "录入", fr.FileRuleCode, "原稿")
	changed := manageTreeJSON(st.StageCode, "收稿", tk.TaskCode, "录入", fr.FileRuleCode, "原始稿件") // 文件标识改名
	finalJSON := unchanged
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.URL.Path, "authoring-tree") {
			_, _ = w.Write([]byte(finalJSON))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`)) // template-baseline 无云端数据 → 回退本地基线
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 无改动（最终==基线）→ 门禁拒绝
	status, resp := jsonReq(t, r, "POST", "/centralized-projects/extract-template?application_id=7&project_code=XM-7", map[string]any{})
	if status != 200 || resp["success"] != false {
		t.Fatalf("无改动时应被拒绝，实得 %v", resp)
	}

	// 有改动（文件改名）→ 提取成功
	finalJSON = changed
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/extract-template?application_id=7&project_code=XM-7", map[string]any{})
	successOk(t, status, resp)
	newID := int64(resp["data"].(map[string]any)["template_id"].(float64))
	cert, _ := repo.GetLocalTemplate(newID)
	if cert.Certified != 1 || cert.IsPublished != 1 {
		t.Fatalf("提取产物应 certified=1 且已发布，实得 certified=%d published=%d", cert.Certified, cert.IsPublished)
	}
	if cert.CertifiedFrom == nil || *cert.CertifiedFrom != "XM-7" {
		t.Fatalf("应记录来源项目 XM-7，实得 %v", cert.CertifiedFrom)
	}
}

// manageTreeJSON 构造 manage authoring-tree 响应（单环节/单任务/单标识）。
func manageTreeJSON(stageCode, stageName, taskCode, taskName, ruleCode, ruleName string) string {
	return `{"code":0,"message":"ok","data":{
		"template":{"template_code":"TPL-PRJ-7","template_version":"V1.0","template_name":"印刷","project_sensitivity_level":"important","scope":"unit"},
		"stages":[{"stage_code":"` + stageCode + `","stage_name":"` + stageName + `","tasks":[
			{"task_code":"` + taskCode + `","task_name":"` + taskName + `","file_rules":[
				{"file_rule_code":"` + ruleCode + `","file_name":"` + ruleName + `","data_state":"input","required":1,"allowed_file_types":"PDF"}
			]}
		]}]}}`
}
