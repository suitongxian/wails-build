package httpd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 提取前的改动清单接口：对比基线返回 新增/改名/删除（纯云端，不依赖本地 edited）。
func TestHTTP_TemplateDiff(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	repo := repository.NewTemplateAuthoringRepository(db)
	src, _ := repo.CreateLocalTemplate(repository.CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(src.ID, repository.StageInput{Name: "收稿"})
	repo.CloneLocalTemplateForApplication(src.ID, "7") // 本地基线 {收稿}

	// 最终：收稿→收件 改名 + 新增环节 装订
	final := `{"code":0,"message":"ok","data":{
		"template":{"template_code":"TPL-PRJ-7","template_version":"V1.0","template_name":"印刷","project_sensitivity_level":"important","scope":"unit"},
		"stages":[
			{"stage_code":"` + st.StageCode + `","stage_name":"收件","tasks":[]},
			{"stage_code":"STG-NEW","stage_name":"装订","tasks":[]}
		]}}`
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.URL.Path, "authoring-tree") {
			_, _ = w.Write([]byte(final))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/template-diff?application_id=7")
	successOk(t, status, resp)
	data := resp["data"].(map[string]any)
	if data["has_baseline"] != true {
		t.Fatalf("应有基线，实得 %v", data["has_baseline"])
	}
	seenRename, seenAdd := false, false
	for _, ci := range data["changes"].([]any) {
		c := ci.(map[string]any)
		if c["level"] == "stage" && c["type"] == "renamed" && c["name"] == "收件" {
			seenRename = true
		}
		if c["level"] == "stage" && c["type"] == "added" && c["name"] == "装订" {
			seenAdd = true
		}
	}
	if !seenRename || !seenAdd {
		t.Fatalf("应识别环节改名(收件)与新增(装订)，实得 %+v", data["changes"])
	}
}

// 重建本地 DB（无本地项目模版/基线）后：靠 manage 云端基线 + 最终结构仍能 diff 与提取。
func TestHTTP_TemplateDiffAndExtract_NoLocalDB(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")
	// 注意：不在本地克隆，模拟「删库重建」——本地无项目专属模版、无本地基线。

	baselineJSON := `{"stages":[{"code":"S1","name":"收稿","tasks":[{"code":"T1","name":"录入","rules":[{"code":"R1","name":"原稿"}]}]}]}`
	final := `{"code":0,"message":"ok","data":{
		"template":{"template_code":"TPL-PRJ-9","template_version":"V1.0","template_name":"印刷","project_sensitivity_level":"important","scope":"unit"},
		"stages":[{"stage_code":"S1","stage_name":"收稿","tasks":[
			{"task_code":"T1","task_name":"录入","file_rules":[
				{"file_rule_code":"R1","file_name":"原始稿件","data_state":"input","required":1,"allowed_file_types":"PDF"}
			]}
		]}]}}`
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.URL.Path, "authoring-tree"):
			_, _ = w.Write([]byte(final))
		case strings.Contains(req.URL.Path, "template-baseline"):
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"source_code":"TPL-X","source_version":"V1.0","baseline_json":` + jsonQuote(baselineJSON) + `}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0}`))
		}
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// diff：靠云端基线对比，识别文件改名（原稿→原始稿件）
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/template-diff?application_id=9")
	successOk(t, status, resp)
	changes := resp["data"].(map[string]any)["changes"].([]any)
	hit := false
	for _, ci := range changes {
		c := ci.(map[string]any)
		if c["level"] == "file_rule" && c["type"] == "renamed" && c["name"] == "原始稿件" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("重建本地库后仍应靠云端基线识别文件改名，实得 %+v", changes)
	}

	// 提取：本地无项目模版也能从 manage 最终结构重建并产出 certified 模版
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/extract-template?application_id=9&project_code=XM-9", map[string]any{})
	successOk(t, status, resp)
	newID := int64(resp["data"].(map[string]any)["template_id"].(float64))
	if cert, _ := repository.NewTemplateAuthoringRepository(db).GetLocalTemplate(newID); cert == nil || cert.Certified != 1 {
		t.Fatalf("重建本地库后提取应产出 certified=1 模版")
	}
}

// jsonQuote 将字符串编码为 JSON 字符串字面量（含引号），用于内嵌响应体。
func jsonQuote(s string) string {
	b := strings.ReplaceAll(s, `\`, `\\`)
	b = strings.ReplaceAll(b, `"`, `\"`)
	return `"` + b + `"`
}
