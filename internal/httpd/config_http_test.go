package httpd

import (
	"data-asset-scan-go/internal/repository"
	"testing"
)

// V1验证-1.40 GET /config 暴露数据业务模版配置字段
// 2026-05-24：三个 URL 合并为单一 server_endpoint；manage_token 已废弃不再回显
func TestHTTP_Config_GetExposesV1Fields(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetWorkspace("/tmp/abc") // 项目根现取自 workspace
	configRepo.SetValue(repository.KeyManageEndpoint, "http://localhost:3000")

	status, resp := jsonReqNoBody(t, r, "GET", "/config")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["project_root"] != "/tmp/abc" {
		t.Errorf("project_root mismatch: %v", d["project_root"])
	}
	// server_endpoint 是新主字段；三个老 URL 字段都回显同一个值
	if d["server_endpoint"] != "http://localhost:3000" {
		t.Errorf("server_endpoint mismatch: %v", d["server_endpoint"])
	}
	if d["manage_endpoint"] != "http://localhost:3000" {
		t.Errorf("manage_endpoint should mirror server_endpoint: %v", d["manage_endpoint"])
	}
	if d["upload_server_url"] != "http://localhost:3000" {
		t.Errorf("upload_server_url should mirror server_endpoint: %v", d["upload_server_url"])
	}
	if d["archive_endpoint"] != "http://localhost:3000" {
		t.Errorf("archive_endpoint should mirror server_endpoint: %v", d["archive_endpoint"])
	}
}

// 验证 effective* 在 DB 里空值时回退到默认服务端地址
// 2026-05-24：三个 URL 合并后，要清空全部三个 key 才会兜底到默认值
func TestHTTP_Config_GetReturnsDefaultServerAddresses(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetValue(repository.KeyManageEndpoint, "")
	configRepo.SetValue(repository.KeyUploadServerURL, "")
	configRepo.SetValue(repository.KeyArchiveEndpoint, "")

	status, resp := jsonReqNoBody(t, r, "GET", "/config")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	if d["server_endpoint"] != "http://47.95.233.47:19091" {
		t.Errorf("default server_endpoint mismatch: %v", d["server_endpoint"])
	}
	if d["manage_endpoint"] != "http://47.95.233.47:19091" {
		t.Errorf("default manage_endpoint mismatch: %v", d["manage_endpoint"])
	}
	if d["upload_server_url"] != "http://47.95.233.47:19091" {
		t.Errorf("default upload_server_url mismatch: %v", d["upload_server_url"])
	}
}

func TestHTTP_EffectiveUploadServerURLDefaultsForServerSideCalls(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetValue(repository.KeyManageEndpoint, "")
	configRepo.SetValue(repository.KeyUploadServerURL, "")
	configRepo.SetValue(repository.KeyArchiveEndpoint, "")

	if got := effectiveUploadServerURL(configRepo); got != "http://47.95.233.47:19091" {
		t.Fatalf("effective upload server URL should default for server-side calls, got %q", got)
	}
}

func TestHTTP_EffectiveManageEndpointDefaultsForServerSideCalls(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetValue(repository.KeyManageEndpoint, "")
	configRepo.SetValue(repository.KeyUploadServerURL, "")
	configRepo.SetValue(repository.KeyArchiveEndpoint, "")

	if got := effectiveManageEndpoint(configRepo); got != "http://47.95.233.47:19091" {
		t.Fatalf("effective manage endpoint should default for server-side calls, got %q", got)
	}
}

// V1验证-1.41 POST /config 写入并持久化 V1 字段
// 2026-05-24：三个 URL 合并为 server_endpoint；manage_token 已废弃不再写库
func TestHTTP_Config_SavePersistsV1Fields(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	status, resp := jsonReq(t, r, "POST", "/config", map[string]interface{}{
		"workspace":       "/data/scan-projects",
		"server_endpoint": "http://manage.local:3000",
	})
	successOk(t, status, resp)

	configRepo := repository.NewSystemConfigRepository(db)
	if v := configRepo.GetWorkspace(); v != "/data/scan-projects" {
		t.Errorf("workspace not persisted: %s", v)
	}
	// server_endpoint 应同步刷三个 key
	if v := configRepo.GetValue(repository.KeyManageEndpoint); v != "http://manage.local:3000" {
		t.Errorf("manage_endpoint not synced from server_endpoint: %s", v)
	}
	if v := configRepo.GetValue(repository.KeyUploadServerURL); v != "http://manage.local:3000" {
		t.Errorf("upload_server_url not synced from server_endpoint: %s", v)
	}
	if v := configRepo.GetValue(repository.KeyArchiveEndpoint); v != "http://manage.local:3000" {
		t.Errorf("archive_endpoint not synced from server_endpoint: %s", v)
	}

	// GET 回读 project_root 应取自 workspace
	_, resp2 := jsonReqNoBody(t, r, "GET", "/config")
	d := dataMap(t, resp2)
	if d["project_root"] != "/data/scan-projects" {
		t.Errorf("readback project_root: %v", d["project_root"])
	}
	if d["server_endpoint"] != "http://manage.local:3000" {
		t.Errorf("readback server_endpoint: %v", d["server_endpoint"])
	}
}

// 兼容性回归：老前端用 manage_endpoint / upload_server_url / archive_endpoint
// 任一字段单独写入，都应同步到三个 key
func TestHTTP_Config_LegacyURLFieldsSyncAllThree(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// 老前端只传 manage_endpoint
	jsonReq(t, r, "POST", "/config", map[string]interface{}{
		"manage_endpoint": "http://legacy.example:8080",
	})

	configRepo := repository.NewSystemConfigRepository(db)
	for _, key := range []string{repository.KeyManageEndpoint, repository.KeyUploadServerURL, repository.KeyArchiveEndpoint} {
		if v := configRepo.GetValue(key); v != "http://legacy.example:8080" {
			t.Errorf("key %s should be synced to legacy URL, got %s", key, v)
		}
	}
}

// 模版服务器地址：未配置时 GET /config 回退默认 :19092，且与 server_endpoint 分离
func TestHTTP_Config_TemplateServerEndpointDefault(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetValue(repository.KeyTemplateServerEndpoint, "") // 明确清空

	status, resp := jsonReqNoBody(t, r, "GET", "/config")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["template_server_endpoint"] != "http://47.95.233.47:19092" {
		t.Errorf("默认模版服务器地址应为 :19092，实得 %v", d["template_server_endpoint"])
	}
}

// 模版服务器地址：POST 独立持久化，且不影响 server_endpoint（两台服务器分离）
func TestHTTP_Config_TemplateServerEndpointPersistsSeparately(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	status, resp := jsonReq(t, r, "POST", "/config", map[string]interface{}{
		"server_endpoint":          "http://manage.local:19091",
		"template_server_endpoint": "http://tpl.local:19092",
	})
	successOk(t, status, resp)

	configRepo := repository.NewSystemConfigRepository(db)
	// 模版地址写入独立 key
	if v := configRepo.GetValue(repository.KeyTemplateServerEndpoint); v != "http://tpl.local:19092" {
		t.Errorf("模版服务器地址未独立持久化: %s", v)
	}
	// manage 地址（三个老 key）不应被模版地址污染
	if v := configRepo.GetEffectiveServerEndpoint(); v != "http://manage.local:19091" {
		t.Errorf("manage 地址被串改: %s", v)
	}
	// GET 回读两者分离
	_, resp2 := jsonReqNoBody(t, r, "GET", "/config")
	d := dataMap(t, resp2)
	if d["server_endpoint"] != "http://manage.local:19091" {
		t.Errorf("回读 server_endpoint: %v", d["server_endpoint"])
	}
	if d["template_server_endpoint"] != "http://tpl.local:19092" {
		t.Errorf("回读 template_server_endpoint: %v", d["template_server_endpoint"])
	}
}

// V1验证-1.42 trim 空白
func TestHTTP_Config_TrimsWhitespace(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	jsonReq(t, r, "POST", "/config", map[string]interface{}{
		"project_root": "  /padded/path  ",
	})

	configRepo := repository.NewSystemConfigRepository(db)
	if v := configRepo.GetValue(repository.KeyProjectRoot); v != "/padded/path" {
		t.Errorf("expected trimmed value '/padded/path', got '%s'", v)
	}
}

// V1验证-1.43 部分更新不影响其他字段
// 2026-05-24：用 daily_scan_interval 这个独立字段验证部分更新（manage_token 已废弃）
func TestHTTP_Config_PartialUpdate(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetWorkspace("/keep/me")
	configRepo.SetValue(repository.KeyManageEndpoint, "http://keep.me")

	// 只更新 daily_scan_interval
	jsonReq(t, r, "POST", "/config", map[string]interface{}{
		"daily_scan_interval": 42,
	})

	if v := configRepo.GetWorkspace(); v != "/keep/me" {
		t.Errorf("workspace should remain unchanged, got '%s'", v)
	}
	if v := configRepo.GetValue(repository.KeyManageEndpoint); v != "http://keep.me" {
		t.Errorf("manage_endpoint should remain unchanged, got '%s'", v)
	}
	if v := configRepo.GetDailyScanInterval(); v != 42 {
		t.Errorf("daily_scan_interval should be 42, got %d", v)
	}
}
