package repository

import (
	"testing"
)

// 验证三个服务端地址在 fresh DB 上被 seed 出来
func TestRunMigrations_SeedsDefaultServerOnFreshDB(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)
	want := "http://47.95.233.47:19091"
	for _, key := range []string{KeyUploadServerURL, KeyManageEndpoint, KeyArchiveEndpoint} {
		if got := cfg.GetValue(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// 验证既有库里 value 为空字符串的会被 backfill 升级为默认值
func TestRunMigrations_BackfillsEmptyServerValues(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	// 模拟用户之前保存过空串，再次启动迁移
	cfg.SetValue(KeyManageEndpoint, "")
	cfg.SetValue(KeyUploadServerURL, "")

	// 重新跑一次迁移
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}

	want := "http://47.95.233.47:19091"
	if got := cfg.GetValue(KeyManageEndpoint); got != want {
		t.Errorf("manage_endpoint backfill: %q, want %q", got, want)
	}
	if got := cfg.GetValue(KeyUploadServerURL); got != want {
		t.Errorf("upload_server_url backfill: %q, want %q", got, want)
	}
}

// 验证既有库里旧 IP 会被自动迁移到新 IP（升级用户无感切换）
func TestRunMigrations_MigratesLegacyServerIP(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)
	cfg.SetValue(KeyManageEndpoint, "http://47.95.212.82:19091")
	cfg.SetValue(KeyUploadServerURL, "http://47.95.212.82:19091")
	cfg.SetValue(KeyArchiveEndpoint, "http://47.95.212.82:18080") // 同 IP 不同 port，也应迁移

	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}

	if got := cfg.GetValue(KeyManageEndpoint); got != "http://47.95.233.47:19091" {
		t.Errorf("manage_endpoint legacy migrate: %q", got)
	}
	if got := cfg.GetValue(KeyUploadServerURL); got != "http://47.95.233.47:19091" {
		t.Errorf("upload_server_url legacy migrate: %q", got)
	}
	if got := cfg.GetValue(KeyArchiveEndpoint); got != "http://47.95.233.47:18080" {
		t.Errorf("archive_endpoint legacy migrate (port preserved): %q", got)
	}
}

// 验证用户显式设置的非空值不会被覆盖
func TestRunMigrations_PreservesNonEmptyUserConfig(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetValue(KeyManageEndpoint, "http://custom.example:8080")
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	if got := cfg.GetValue(KeyManageEndpoint); got != "http://custom.example:8080" {
		t.Errorf("user-set value got overwritten: %q", got)
	}
}
