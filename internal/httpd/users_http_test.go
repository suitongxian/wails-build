package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_ListUsers_ReturnsRegisteredUsers(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	if _, err := db.Exec(`INSERT INTO users (
		username, display_name, company_name, department, ip, mac_address,
		work_address, phone, status, create_time, update_time, disable
	) VALUES
		('zhangsan', '张三', '第一研究院', '档案处', '', '', '', '', 'active', ?, ?, 0),
		('disabled', '停用用户', '第一研究院', '档案处', '', '', '', '', 'inactive', ?, ?, 1),
		('lisi', '李四', '第一研究院', '技术处', '', '', '', '', 'active', ?, ?, 0)`,
		now, now, now, now, now, now); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('DEPT-ARCHIVE', '档案处', 'department', 'active', ?, ?, 0)`, now, now); err != nil {
		t.Fatalf("insert subject: %v", err)
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/users")
	if status != 200 || resp["success"] != true {
		t.Fatalf("GET /users failed: status=%d resp=%+v", status, resp)
	}
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 active users, got %d: %+v", len(data), data)
	}
	first := data[0].(map[string]interface{})
	if first["display_name"] != "张三" || first["department"] != "档案处" {
		t.Fatalf("unexpected first user: %+v", first)
	}
}

func TestHTTP_ListUsersSyncsRegisteredUsersFromManage(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/auth-users/list" {
			t.Fatalf("unexpected manage path: %s", req.URL.Path)
		}
		if req.URL.Query().Get("status") != "active" {
			t.Fatalf("expected active status filter, got %q", req.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "success",
			"data": []map[string]interface{}{
				{
					"username":        "wangwu",
					"display_name":    "王五",
					"user_unit":       "第一研究院",
					"user_department": "技术处",
					"phone":           "13800000000",
					"status":          "active",
				},
			},
		})
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/users")
	if status != 200 || resp["success"] != true {
		t.Fatalf("GET /users failed: status=%d resp=%+v", status, resp)
	}
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 synced user, got %d: %+v", len(data), data)
	}
	got := data[0].(map[string]interface{})
	if got["username"] != "wangwu" || got["display_name"] != "王五" || got["department"] != "技术处" {
		t.Fatalf("unexpected synced user: %+v", got)
	}

	var localID int64
	if err := db.Get(&localID, `SELECT id FROM users WHERE username = 'wangwu' AND disable = 0`); err != nil || localID == 0 {
		t.Fatalf("synced manage user should be persisted locally, id=%d err=%v", localID, err)
	}
}
