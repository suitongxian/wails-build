package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func fakeManageLogin(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 0, "message": "ok",
			"data": map[string]interface{}{
				"token": "tok",
				"user": map[string]interface{}{
					"id": 7, "username": "liulaoshi", "display_name": "刘老师",
					"user_unit": "第一研究院", "user_department": "档案处", "role": "user", "status": "active",
				},
			},
		})
	}))
}

// 登录成功后应把账号(含密码)存入本机登录历史，供快速登录。
func TestHTTP_LoginHistory_SavedAfterLogin(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defer cleanup()
	manage := fakeManageLogin(t)
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "liulaoshi", "password": "secret123",
	})
	successOk(t, status, resp)

	status, resp = jsonReqNoBody(t, r, "GET", "/auth/login-history")
	successOk(t, status, resp)
	arr, _ := resp["data"].([]interface{})
	found := false
	for _, it := range arr {
		m, _ := it.(map[string]interface{})
		if m["username"] == "liulaoshi" && m["password"] == "secret123" && m["display_name"] == "刘老师" {
			found = true
		}
	}
	if !found {
		t.Fatalf("登录历史应含刚登录的用户(带密码)，实得 %v", arr)
	}
}

// 删除某条登录历史。
func TestHTTP_LoginHistory_Delete(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defer cleanup()
	manage := fakeManageLogin(t)
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)
	jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{"username": "liulaoshi", "password": "secret123"})

	status, resp := jsonReq(t, r, "POST", "/auth/login-history/delete", map[string]interface{}{"username": "liulaoshi"})
	successOk(t, status, resp)

	_, resp = jsonReqNoBody(t, r, "GET", "/auth/login-history")
	arr, _ := resp["data"].([]interface{})
	if len(arr) != 0 {
		t.Fatalf("删除后应为空，实得 %v", arr)
	}
}
