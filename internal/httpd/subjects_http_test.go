package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_Subjects_ListRequiresManageEndpoint(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	status, resp := jsonReqNoBody(t, r, "GET", "/subjects")
	if status != http.StatusOK {
		t.Fatalf("expected HTTP 200 with business failure, got %d", status)
	}
	if success, _ := resp["success"].(bool); success {
		t.Fatalf("expected /subjects to fail without manage endpoint, got %+v", resp)
	}
	if resp["error"] == "" {
		t.Fatalf("expected clear error when manage endpoint is missing")
	}
}

func TestHTTP_Subjects_ListAutoSyncsFromManage(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/subjects/list" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		if got := req.URL.Query().Get("include_inactive"); got != "1" {
			t.Fatalf("expected include_inactive=1, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "success",
			"data": []map[string]interface{}{
				{"code": "DEPT-AUTO", "name": "自动同步部门", "type": "department", "status": "active"},
				{"code": "PERSON-AUTO", "name": "自动同步个人", "type": "person", "status": "active"},
			},
		})
	}))
	defer manage.Close()

	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/subjects?type=department")
	successOk(t, status, resp)
	list := dataList(t, resp)
	if len(list) != 1 {
		t.Fatalf("expected 1 synced department subject, got %d", len(list))
	}
	got := list[0].(map[string]interface{})
	if got["code"] != "DEPT-AUTO" {
		t.Fatalf("expected manage subject, got %+v", got)
	}
}

func TestHTTP_Subjects_ListHidesSystemSubjectsByDefault(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('SYS-PERSONAL-USER', '本人', 'person', 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0)`); err != nil {
		t.Fatalf("insert system subject: %v", err)
	}

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/subjects/list" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "success",
			"data": []map[string]interface{}{
				{"code": "DEPT-REAL", "name": "真实部门", "type": "department", "status": "active"},
			},
		})
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/subjects")
	successOk(t, status, resp)
	list := dataList(t, resp)
	for _, raw := range list {
		item := raw.(map[string]interface{})
		if item["code"] == "SYS-PERSONAL-USER" {
			t.Fatalf("system subject should be hidden by default: %+v", list)
		}
	}

	status, resp = jsonReqNoBody(t, r, "GET", "/subjects?include_system=true")
	successOk(t, status, resp)
	list = dataList(t, resp)
	foundSystem := false
	for _, raw := range list {
		item := raw.(map[string]interface{})
		if item["code"] == "SYS-PERSONAL-USER" {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Fatalf("include_system=true should return system subjects, got %+v", list)
	}
}

func TestHTTP_Subjects_WriteRoutesAreNotRegistered(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	writeRequests := []struct {
		method string
		path   string
		body   interface{}
	}{
		{method: "POST", path: "/subjects", body: map[string]interface{}{"code": "P-001", "name": "张三", "type": "person"}},
		{method: "PUT", path: "/subjects/1", body: map[string]interface{}{"name": "张三-改名"}},
		{method: "DELETE", path: "/subjects/1"},
		{method: "POST", path: "/subjects/sync"},
	}

	for _, tc := range writeRequests {
		status, _ := jsonReq(t, r, tc.method, tc.path, tc.body)
		if status != http.StatusNotFound {
			t.Fatalf("%s %s should not be registered in scan, got HTTP %d", tc.method, tc.path, status)
		}
	}
}
