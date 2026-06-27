package repository

import (
	"testing"
)

// 三个 key 都空：返回默认地址
func TestGetEffectiveServerEndpoint_DefaultWhenAllEmpty(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	// fresh migrated DB 会 seed 三个 key 为 DefaultServerEndpoint，先清空
	cfg.SetValue(KeyManageEndpoint, "")
	cfg.SetValue(KeyUploadServerURL, "")
	cfg.SetValue(KeyArchiveEndpoint, "")

	if got := cfg.GetEffectiveServerEndpoint(); got != DefaultServerEndpoint {
		t.Errorf("all empty 应返回默认地址，got %q", got)
	}
}

// 只设 manage_endpoint：返回 manage_endpoint
func TestGetEffectiveServerEndpoint_ManageOnly(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetValue(KeyManageEndpoint, "http://m.example:8080")
	cfg.SetValue(KeyUploadServerURL, "")
	cfg.SetValue(KeyArchiveEndpoint, "")

	if got := cfg.GetEffectiveServerEndpoint(); got != "http://m.example:8080" {
		t.Errorf("manage_endpoint 优先，got %q", got)
	}
}

// 只设 upload_server_url（老库场景，先升级 upload 后还没碰 manage）：返回 upload_server_url
func TestGetEffectiveServerEndpoint_UploadFallback(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetValue(KeyManageEndpoint, "")
	cfg.SetValue(KeyUploadServerURL, "http://u.example:8080")
	cfg.SetValue(KeyArchiveEndpoint, "")

	if got := cfg.GetEffectiveServerEndpoint(); got != "http://u.example:8080" {
		t.Errorf("upload_server_url 兜底，got %q", got)
	}
}

// 只设 archive_endpoint：返回 archive_endpoint
func TestGetEffectiveServerEndpoint_ArchiveFallback(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetValue(KeyManageEndpoint, "")
	cfg.SetValue(KeyUploadServerURL, "")
	cfg.SetValue(KeyArchiveEndpoint, "http://a.example:8080")

	if got := cfg.GetEffectiveServerEndpoint(); got != "http://a.example:8080" {
		t.Errorf("archive_endpoint 最后兜底，got %q", got)
	}
}

// 三个都设：以 manage 为准
func TestGetEffectiveServerEndpoint_ManageWins(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetValue(KeyManageEndpoint, "http://m.example:8080")
	cfg.SetValue(KeyUploadServerURL, "http://u.example:8080")
	cfg.SetValue(KeyArchiveEndpoint, "http://a.example:8080")

	if got := cfg.GetEffectiveServerEndpoint(); got != "http://m.example:8080" {
		t.Errorf("manage_endpoint 永远赢，got %q", got)
	}
}

// 纯空白当成空处理
func TestGetEffectiveServerEndpoint_WhitespaceSkipped(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetValue(KeyManageEndpoint, "   ")
	cfg.SetValue(KeyUploadServerURL, "http://real.example:8080")
	cfg.SetValue(KeyArchiveEndpoint, "")

	if got := cfg.GetEffectiveServerEndpoint(); got != "http://real.example:8080" {
		t.Errorf("纯空白应跳过，got %q", got)
	}
}
