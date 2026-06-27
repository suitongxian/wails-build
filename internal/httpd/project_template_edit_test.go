package httpd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 项目专属模版载入+保存：载入返回可编辑 template_id 与五层树；新增空环节后保存整树回灌 manage。
func TestHTTP_ProjectTemplate_LoadEditSave(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	// manage：接 ingest（保存回灌）
	var ingestBody string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.URL.Path, "templates/ingest") {
			buf := make([]byte, req.ContentLength)
			_, _ = req.Body.Read(buf)
			ingestBody = string(buf)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"template_id":88}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 先在本地造出该项目的项目专属模版（模拟关联即克隆的结果）
	repo := repository.NewTemplateAuthoringRepository(db)
	src, _ := repo.CreateLocalTemplate(repository.CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(src.ID, repository.StageInput{Name: "收稿"})
	repo.CreateTask(st.ID, repository.TaskInput{Name: "录入", SensitivityLevel: "important"})
	if _, err := repo.CloneLocalTemplateForApplication(src.ID, "42"); err != nil {
		t.Fatal(err)
	}

	// 载入
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/project-template?application_id=42")
	successOk(t, status, resp)
	data := resp["data"].(map[string]any)
	cloneTplID := int64(data["template_id"].(float64))
	if data["template_code"].(string) != "TPL-PRJ-42" {
		t.Fatalf("应返回项目专属模版编码 TPL-PRJ-42，实得 %v", data["template_code"])
	}

	// 新增一个空环节（复用通用 stage CRUD），允许无文件任务
	st2, _ := repo.CreateStage(cloneTplID, repository.StageInput{Name: "新增空环节"})
	_ = st2

	// 保存：整树回灌 manage（应带上新增的空环节）
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/save-project-template?application_id=42", map[string]any{})
	successOk(t, status, resp)
	if !strings.Contains(ingestBody, "新增空环节") {
		t.Fatalf("保存应把新增空环节整树回灌 manage，ingestBody=%s", ingestBody)
	}
	if !strings.Contains(ingestBody, "\"template_code\":\"TPL-PRJ-42\"") {
		t.Fatalf("回灌应指向项目专属模版 TPL-PRJ-42，ingestBody=%s", ingestBody)
	}
}
