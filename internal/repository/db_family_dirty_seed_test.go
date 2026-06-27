package repository

import (
	"testing"
	"time"
)

// 全新数据库 + 没有任何家族行 → seed 出 "1"（用户应当点重建以触发首次构建）
func TestRunMigrations_FamilyDirtySeed_FreshDBNoFamilies(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	if got := cfg.GetValue(KeyFamilyDirty); got != "1" {
		t.Errorf("fresh DB without families: family_dirty = %q, want %q", got, "1")
	}
}

// 升级场景：库里有家族行 + family_dirty 还没 seed → seed 出 "0"（不打扰老用户）
func TestRunMigrations_FamilyDirtySeed_PreexistingFamilies(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	// 插一行 family 模拟「老用户已跑过分析」
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resource_family
		(primary_content_sign, member_count, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, 0)`,
		"pretend-sign", 2, now, now)
	if err != nil {
		t.Fatal(err)
	}

	// 模拟「升级新版本」：删掉首次 seed 的 family_dirty key，再跑一次迁移
	_, _ = db.Exec(`DELETE FROM system_config WHERE key = ?`, KeyFamilyDirty)
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}

	cfg := NewSystemConfigRepository(db)
	if got := cfg.GetValue(KeyFamilyDirty); got != "0" {
		t.Errorf("DB with pre-existing families: family_dirty = %q, want %q", got, "0")
	}
}

// 用户已显式设置过的值不会被 seed 覆盖（幂等）
func TestRunMigrations_FamilyDirtySeed_PreservesExistingValue(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetValue(KeyFamilyDirty, "0")
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	if got := cfg.GetValue(KeyFamilyDirty); got != "0" {
		t.Errorf("explicitly set value got overwritten: %q", got)
	}
}
