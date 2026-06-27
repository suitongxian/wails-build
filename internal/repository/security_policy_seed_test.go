package repository

import (
	"encoding/json"
	"testing"
)

func TestSecurityPolicySeed_FullMatrix(t *testing.T) {
	db := openTestDB(t)

	// runMigrations 已在 openTestDB 中调用，应当包含 seed
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM security_policies WHERE disable = 0`); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 15 {
		t.Fatalf("expected 15 baseline policies (3 levels x 5 states), got %d", count)
	}

	// 检查 3 个等级各 5 条
	for _, level := range []string{SensGeneral, SensImportant, SensCoreSecret} {
		var n int
		if err := db.Get(&n, `SELECT COUNT(*) FROM security_policies WHERE sensitivity_level = ? AND disable = 0`, level); err != nil {
			t.Fatal(err)
		}
		if n != 5 {
			t.Fatalf("level %s expected 5 rows, got %d", level, n)
		}
	}
	var internalCount int
	if err := db.Get(&internalCount, `SELECT COUNT(*) FROM security_policies WHERE sensitivity_level = 'internal' AND disable = 0`); err != nil {
		t.Fatal(err)
	}
	if internalCount != 0 {
		t.Fatalf("internal sensitivity should not be seeded, got %d rows", internalCount)
	}

	// 检查核心级有外泄检查保护规则
	var rules string
	if err := db.Get(&rules, `SELECT protection_rules FROM security_policies WHERE sensitivity_level = ? AND file_state = ? AND disable = 0`, SensCoreSecret, FileStatePersonalProcess); err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(rules), &m); err != nil {
		t.Fatalf("invalid protection_rules JSON: %v", err)
	}
	if v, ok := m["no_local_download"]; !ok || v != true {
		t.Fatalf("core_secret should have no_local_download=true, got %v", m)
	}
	if v, ok := m["leak_check"]; !ok || v != true {
		t.Fatalf("core_secret should have leak_check=true, got %v", m)
	}

	// 一般级 / 个人过程 不需要审计
	var audit int
	if err := db.Get(&audit, `SELECT audit_required FROM security_policies WHERE sensitivity_level = ? AND file_state = ? AND disable = 0`, SensGeneral, FileStatePersonalProcess); err != nil {
		t.Fatal(err)
	}
	if audit != 0 {
		t.Fatalf("general+personal_process should not require audit, got %d", audit)
	}

	// 重要级 / 单位发布 应使用单位档案室
	var tier string
	if err := db.Get(&tier, `SELECT storage_tier FROM security_policies WHERE sensitivity_level = ? AND file_state = ? AND disable = 0`, SensImportant, FileStateUnitRelease); err != nil {
		t.Fatal(err)
	}
	if tier != StorageTierUnitArchive {
		t.Fatalf("important+unit_release expected unit_archive, got %s", tier)
	}

	// 核心级 / 部门定稿 已达到单位核心要件保密室接收边界
	if err := db.Get(&tier, `SELECT storage_tier FROM security_policies WHERE sensitivity_level = ? AND file_state = ? AND disable = 0`, SensCoreSecret, FileStateDeptFinal); err != nil {
		t.Fatal(err)
	}
	if tier != StorageTierSecureRoom {
		t.Fatalf("core_secret+dept_final expected secure_room, got %s", tier)
	}

	// 核心级 / 单位发布 应使用机要室
	if err := db.Get(&tier, `SELECT storage_tier FROM security_policies WHERE sensitivity_level = ? AND file_state = ? AND disable = 0`, SensCoreSecret, FileStateUnitRelease); err != nil {
		t.Fatal(err)
	}
	if tier != StorageTierSecureRoom {
		t.Fatalf("core_secret+unit_release expected secure_room, got %s", tier)
	}

	// permissions 必须是合法 JSON 数组
	var permsStr string
	if err := db.Get(&permsStr, `SELECT permissions FROM security_policies WHERE sensitivity_level = ? AND file_state = ? AND disable = 0`, SensImportant, FileStatePersonalFinal); err != nil {
		t.Fatal(err)
	}
	var perms []string
	if err := json.Unmarshal([]byte(permsStr), &perms); err != nil {
		t.Fatalf("permissions JSON invalid: %v", err)
	}
	if len(perms) == 0 {
		t.Fatal("important should have permissions")
	}
}

func TestSecurityPolicySeed_Idempotent(t *testing.T) {
	db := openTestDB(t)
	if err := seedSecurityPolicies(db); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM security_policies`); err != nil {
		t.Fatal(err)
	}
	if n != 15 {
		t.Fatalf("re-seed should be idempotent, expected 15 got %d", n)
	}
}
