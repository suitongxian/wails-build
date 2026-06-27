package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_AuthLoginMirrorsManageUser(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defer cleanup()

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/auth/login" {
			t.Fatalf("unexpected manage path: %s", req.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode manage request: %v", err)
		}
		if body["username"] != "liulaoshi" || body["password"] != "secret123" {
			t.Fatalf("unexpected manage login body: %+v", body)
		}
		if body["computer_ip"] == "" || body["computer_mac"] == "" {
			t.Fatalf("missing terminal metadata: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "登录成功",
			"data": map[string]interface{}{
				"token": "manage-token-login",
				"user": map[string]interface{}{
					"id":              7,
					"username":        "liulaoshi",
					"display_name":    "刘老师",
					"user_unit":       "第一研究院",
					"user_department": "档案处",
					"phone":           "13800000000",
					"role":            "user",
					"status":          "active",
				},
			},
		})
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "liulaoshi",
		"password": "secret123",
	})
	successOk(t, status, resp)
	data := dataMap(t, resp)
	if data["token"] != "manage-token-login" {
		t.Fatalf("token mismatch: %+v", data)
	}
	user := data["user"].(map[string]interface{})
	if user["display_name"] != "刘老师" || user["user_unit"] != "第一研究院" {
		t.Fatalf("unexpected user: %+v", user)
	}

	var userInfoCount int
	if err := db.Get(&userInfoCount, `SELECT COUNT(*) FROM user_info WHERE user_name = '刘老师' AND company_name = '第一研究院' AND department = '档案处' AND disable = 0`); err != nil {
		t.Fatal(err)
	}
	if userInfoCount != 1 {
		t.Fatalf("user_info mirror count=%d, want 1", userInfoCount)
	}

	var usersCount int
	if err := db.Get(&usersCount, `SELECT COUNT(*) FROM users WHERE username = 'liulaoshi' AND display_name = '刘老师' AND company_name = '第一研究院' AND department = '档案处' AND disable = 0`); err != nil {
		t.Fatal(err)
	}
	if usersCount != 1 {
		t.Fatalf("users mirror count=%d, want 1", usersCount)
	}
}

func TestHTTP_AuthLoginMirrorsManageUserWhenDisplayNameMissing(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defer cleanup()

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/auth/login" {
			t.Fatalf("unexpected manage path: %s", req.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "登录成功",
			"data": map[string]interface{}{
				"token": "manage-token-admin",
				"user": map[string]interface{}{
					"id":              1,
					"username":        "admin",
					"display_name":    "",
					"user_unit":       "第一研究院",
					"user_department": "系统管理部",
					"role":            "admin",
					"status":          "active",
				},
			},
		})
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "admin",
		"password": "secret123",
	})
	successOk(t, status, resp)

	var usersCount int
	if err := db.Get(&usersCount, `SELECT COUNT(*) FROM users WHERE username = 'admin' AND display_name = 'admin' AND disable = 0`); err != nil {
		t.Fatal(err)
	}
	if usersCount != 1 {
		t.Fatalf("admin user should be mirrored with username fallback display_name, count=%d", usersCount)
	}
}

// 关闭终端再打开应保持登录：登录后会话落本地库，进程内会话清空（模拟重启）后
// GET /session 仍认证；登出清库后再重启则不再认证。
func TestHTTP_AuthSessionPersistsAcrossRestart(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defer cleanup()

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "登录成功",
			"data": map[string]interface{}{
				"token": "persist-token",
				"user": map[string]interface{}{
					"id": 9, "username": "wanglaoshi", "display_name": "王老师",
					"user_unit": "第一研究院", "user_department": "信息处",
					"role": "user", "status": "active",
				},
			},
		})
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 登录成功
	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "wanglaoshi", "password": "secret123",
	})
	successOk(t, status, resp)

	// 会话已落本地库
	if v := repository.NewSystemConfigRepository(db).GetValue(repository.KeyAuthSession); v == "" {
		t.Fatalf("登录后会话应落库")
	}

	// 模拟「关闭 scan」：清空进程内会话，但不动本地库
	currentAuthSession.Lock()
	currentAuthSession.session = nil
	currentAuthSession.Unlock()

	// 模拟「重新打开」：仍应认证（从库恢复）
	status, resp = jsonReqNoBody(t, r, "GET", "/auth/session")
	successOk(t, status, resp)
	session := dataMap(t, resp)
	if session["authenticated"] != true {
		t.Fatalf("重启后应仍保持登录: %+v", session)
	}
	if session["token"] != "persist-token" {
		t.Fatalf("恢复的 token 不对: %+v", session)
	}

	// 登出应清库
	status, resp = jsonReqNoBody(t, r, "POST", "/auth/logout")
	successOk(t, status, resp)
	if v := repository.NewSystemConfigRepository(db).GetValue(repository.KeyAuthSession); v != "" {
		t.Fatalf("登出后会话应清库, 实得 %q", v)
	}

	// 再次模拟「重启」：库已清，不应再认证
	currentAuthSession.Lock()
	currentAuthSession.session = nil
	currentAuthSession.Unlock()
	status, resp = jsonReqNoBody(t, r, "GET", "/auth/session")
	successOk(t, status, resp)
	session = dataMap(t, resp)
	if session["authenticated"] != false {
		t.Fatalf("登出并重启后不应再登录: %+v", session)
	}
}

// 登录有效期 1 天：超期后即便本地库还存着会话，重开也不再认证，且过期会话被清库。
func TestHTTP_AuthSessionExpiresAfterTTL(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defer cleanup()

	// 往库里写一条「1 小时前就过期」的会话，模拟距上次登录已超 1 天
	expired := persistedSession{
		Token:     "stale-token",
		User:      authUser{ID: 9, Username: "wanglaoshi", DisplayName: "王老师", Status: "active"},
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}
	b, _ := json.Marshal(expired)
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyAuthSession, string(b))

	// 清进程内会话，模拟「重新打开 scan」
	currentAuthSession.Lock()
	currentAuthSession.session = nil
	currentAuthSession.expiresAt = time.Time{}
	currentAuthSession.Unlock()

	// 过期 → 不应认证
	status, resp := jsonReqNoBody(t, r, "GET", "/auth/session")
	successOk(t, status, resp)
	session := dataMap(t, resp)
	if session["authenticated"] != false {
		t.Fatalf("登录超过 1 天应失效: %+v", session)
	}
	// 过期会话应被清库
	if v := repository.NewSystemConfigRepository(db).GetValue(repository.KeyAuthSession); v != "" {
		t.Fatalf("过期会话应被清库, 实得 %q", v)
	}
}

func TestHTTP_AuthRegisterSessionAndLogout(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defer cleanup()

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/auth/register" {
			t.Fatalf("unexpected manage path: %s", req.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode manage request: %v", err)
		}
		if body["username"] != "zhangsan" || body["display_name"] != "张三" {
			t.Fatalf("unexpected manage register body: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "注册成功",
			"data": map[string]interface{}{
				"token": "manage-token-register",
				"user": map[string]interface{}{
					"id":              8,
					"username":        "zhangsan",
					"display_name":    "张三",
					"user_unit":       "第一研究院",
					"user_department": "综合处",
					"phone":           "13900000000",
					"role":            "user",
					"status":          "active",
				},
			},
		})
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/auth/register", map[string]interface{}{
		"username":        "zhangsan",
		"password":        "secret123",
		"display_name":    "张三",
		"user_unit":       "第一研究院",
		"user_department": "综合处",
		"phone":           "13900000000",
	})
	successOk(t, status, resp)

	status, resp = jsonReqNoBody(t, r, "GET", "/auth/session")
	successOk(t, status, resp)
	session := dataMap(t, resp)
	if session["authenticated"] != true {
		t.Fatalf("session should be authenticated: %+v", session)
	}

	status, resp = jsonReqNoBody(t, r, "POST", "/auth/logout")
	successOk(t, status, resp)

	status, resp = jsonReqNoBody(t, r, "GET", "/auth/session")
	successOk(t, status, resp)
	session = dataMap(t, resp)
	if session["authenticated"] != false {
		t.Fatalf("session should be cleared: %+v", session)
	}
}
