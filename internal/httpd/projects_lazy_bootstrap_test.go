package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// TestHTTP_ListProjects_LazyBootstrapsPersonalContext 覆盖懒触发场景：
// 启动时 manage_endpoint 未配 → 没建出 SYS-PERSONAL-* 项目；
// 用户后来配上 endpoint，调 GET /projects?status=active 应当场尝试 bootstrap
// 并返回新建好的项目。
func TestHTTP_ListProjects_LazyBootstrapsPersonalContext(t *testing.T) {
	// 重置 throttle，避免被前面跑过的 lastBootstrapAttempt 影响
	resetBootstrapThrottleForTest()

	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	// mock manage：响应模板 fetch 请求
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/templates/full" {
			http.NotFound(w, req)
			return
		}
		ver := req.URL.Query().Get("version")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 0, "data": map[string]interface{}{
				"template": map[string]interface{}{
					"id":                        1,
					"template_code":             "TPL-PERSONAL-FILES",
					"template_name":             "个人文件模版",
					"template_version":          ver,
					"status":                    "active",
					"project_sensitivity_level": "general",
					"publisher":                 "system",
				},
				"stages": []map[string]interface{}{
					{
						"id": 101, "stage_code": "GR-DA", "stage_name": "个人归档",
						"stage_type": "record", "sort_order": 1,
						"file_rules": []map[string]interface{}{
							{"id": 201, "file_rule_code": "IN-001", "file_name": "来源",
								"data_state": "input", "allowed_file_types": "[\"*\"]"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	// 前置：本地没有任何项目
	repo := repository.NewDataProjectRepository(db)
	pre, _ := repo.List("active", "")
	if hasAnyPersonalProject(pre) {
		t.Fatal("前置：本地不应已有 personal 项目")
	}

	// GET /projects?status=active → 应触发 bootstrap → 返回 3 个 SYS-PERSONAL-* 项目
	status, resp := jsonReqNoBody(t, r, "GET", "/projects?status=active")
	successOk(t, status, resp)
	list, _ := resp["data"].([]interface{})
	personalCount := 0
	for _, p := range list {
		m, _ := p.(map[string]interface{})
		if code, _ := m["project_code"].(string); code == "SYS-PERSONAL-CORE" ||
			code == "SYS-PERSONAL-IMPORTANT" || code == "SYS-PERSONAL-GENERAL" {
			personalCount++
		}
	}
	if personalCount != 3 {
		t.Errorf("lazy bootstrap 后应有 3 个 SYS-PERSONAL-* 项目，实际 %d", personalCount)
	}
}
