package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// TestWorkItemsFlow 验证 P3/P4/P5 scan 侧闭环：拉取我的工作事项 / 开始工作建目录 / 提交定稿
func TestWorkItemsFlow(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "liu") // 当前登录用户 username=liu
	root := withProjectRoot(t, db)

	// 本地已有该项目（模拟已同步/已立项到本地），ensureProjectSyncedLocal 命中本地、不去 manage 拉。
	// 编码改成 TPL-X 以对齐 manage「我的工作事项」返回的项目编码；两个环节自然为 STG-001/STG-002。
	ar := repository.NewTemplateAuthoringRepository(db)
	tplX, _ := ar.CreateLocalTemplate(repository.CreateTemplateInput{Scope: "unit", TemplateName: "印刷计划", SensitivityLevel: "general"})
	ar.CreateStage(tplX.ID, repository.StageInput{Name: "收稿登记"})
	ar.CreateStage(tplX.ID, repository.StageInput{Name: "排版"})
	db.MustExec(`UPDATE data_templates SET template_code = 'TPL-X' WHERE id = ?`, tplX.ID)

	var delivered []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/work-items/list":
			if req.URL.Query().Get("username") != "liu" {
				t.Errorf("应按当前用户 liu 查询，实得 %s", req.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":[
				{"template_code":"TPL-X","template_name":"印刷计划","stage_id":1,"stage_code":"STG-001","stage_name":"收稿登记","sort_order":1,"manager":"刘老师","members":"","delivered":0,"status":"ready"}
			]}`))
		case "/api/work-items/deliver":
			b, _ := io.ReadAll(req.Body)
			_ = b
			delivered = append(delivered, map[string]string{"hit": "1"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":null}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, srv.URL)

	// P3: 我的工作事项
	code, resp := jsonReqNoBody(t, r, "GET", "/my-work-items")
	if code != 200 || resp["success"] != true {
		t.Fatalf("拉取我的工作事项失败: %v", resp)
	}
	list, _ := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("应有 1 条工作事项，实得 %v", resp["data"])
	}
	if list[0].(map[string]interface{})["status"] != "ready" {
		t.Fatalf("状态应为 ready")
	}

	// P4: 开始工作 → 本机只建该环节目录
	code, resp = jsonReq(t, r, "POST", "/work-items/start", map[string]any{"project_code": "TPL-X", "stage_code": "STG-001"})
	if code != 200 || resp["success"] != true {
		t.Fatalf("开始工作失败: %v", resp)
	}
	for _, sub := range []string{"input", "process", "output"} {
		d := filepath.Join(root, "项目文件管理", "TPL-X", "stages", "STG-001", sub)
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("应已创建目录 %s: %v", d, err)
		}
	}
	// 只建了该环节，不建别的环节
	if _, err := os.Stat(filepath.Join(root, "项目文件管理", "TPL-X", "stages", "STG-002")); !os.IsNotExist(err) {
		t.Fatal("不应创建其它环节目录（按需单环节）")
	}

	// P5: 提交定稿 → 通知 manage
	code, resp = jsonReq(t, r, "POST", "/work-items/deliver", map[string]any{"template_code": "TPL-X", "stage_code": "STG-001"})
	if code != 200 || resp["success"] != true {
		t.Fatalf("提交定稿失败: %v", resp)
	}
	if len(delivered) != 1 {
		t.Fatalf("应通知 manage 交付一次，实得 %d", len(delivered))
	}
}
