package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 辅助：起一个 mock manage 服务，返回固定 session
func newMockManageForLogin(t *testing.T, username string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "登录成功",
			"data": map[string]interface{}{
				"token": "manage-token-" + username,
				"user": map[string]interface{}{
					"id":              42,
					"username":        username,
					"display_name":    username + "_display",
					"user_unit":       "第一研究院",
					"user_department": "档案处",
					"role":            "user",
					"status":          "active",
				},
			},
		})
	}))
}

func TestHTTP_AuthLogin_AutoCreatesWorkspaceWhenEmpty(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	successOk(t, status, resp)

	// 默认工作目录 = 系统桌面/我的工作空间，且已创建
	got := cfg.GetWorkspace()
	if filepath.Base(got) != "我的工作空间" {
		t.Errorf("KeyWorkspace 应为「我的工作空间」目录，实得 %q", got)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Errorf("workspace dir should exist, stat err=%v", err)
	}
}

func TestHTTP_AuthLogin_DoesNotOverwriteCustomWorkspace(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetWorkspace("/data/custom-keep")

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	successOk(t, status, resp)

	if got := cfg.GetWorkspace(); got != "/data/custom-keep" {
		t.Errorf("KeyWorkspace should be preserved, got %q", got)
	}
}

// 登录后 scan_area_path 默认 = OS home（首次普查不会因为没配扫描区域而失败）
func TestHTTP_AuthLogin_AutoSetsScanAreaPathToHomeWhenEmpty(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	successOk(t, status, resp)

	if got := cfg.GetScanAreaPath(); got != tmpHome {
		t.Errorf("ScanAreaPath = %q, want %q", got, tmpHome)
	}
}

// 已自定义 scan_area_path 登录后不被覆盖
func TestHTTP_AuthLogin_DoesNotOverwriteCustomScanAreaPath(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetScanAreaPath("/data/custom-scan-area")

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	successOk(t, status, resp)

	if got := cfg.GetScanAreaPath(); got != "/data/custom-scan-area" {
		t.Errorf("ScanAreaPath should be preserved, got %q", got)
	}
}

// 登录后 control_type 默认 = defaultControlType（首次普查不会缺失管控文件类型）
func TestHTTP_AuthLogin_AutoSetsControlTypeToDefaultWhenEmpty(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	successOk(t, status, resp)

	if got := cfg.GetControlType(); got != defaultControlType {
		t.Errorf("ControlType = %q, want %q", got, defaultControlType)
	}
}

func TestHTTP_AuthLogin_StillSucceedsWhenWorkspaceMkdirFails(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	// 让 HOME 指向一个文件，触发 MkdirAll 失败
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpFile)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	// 登录必须成功——自动化便利不能阻塞登录
	successOk(t, status, resp)

	// KeyWorkspace 应该仍为空（mkdir 失败时不写库）
	if got := cfg.GetWorkspace(); got != "" {
		t.Errorf("KeyWorkspace should remain empty after mkdir failure, got %q", got)
	}
}
