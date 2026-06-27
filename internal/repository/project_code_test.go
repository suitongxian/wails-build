package repository

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestValidateObjectShortCode(t *testing.T) {
	cases := []struct {
		in    string
		valid bool
	}{
		{"MC", true},
		{"MC-NSXS", true},
		{"MOJ-NLF", true},
		{"BOOK1-V2", true},
		{"", false},
		{"mc", false},       // lowercase
		{"MC NSXS", false},  // space
		{"MC_NSXS", false},  // underscore
		{"MC--NSXS", false}, // double hyphen produces empty segment
		{"-MC", false},      // leading hyphen
		{"MC-", false},      // trailing hyphen
	}
	for _, c := range cases {
		err := ValidateObjectShortCode(c.in)
		if c.valid && err != nil {
			t.Errorf("expected %q to be valid, got %v", c.in, err)
		}
		if !c.valid && err == nil {
			t.Errorf("expected %q to be invalid", c.in)
		}
	}
}

func TestGenerateProjectCode_FirstAndIncrement(t *testing.T) {
	db := openTestDB(t)
	ts := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)

	// 第一个项目
	code, err := GenerateProjectCode(db, "MC-NSXS", ts)
	if err != nil {
		t.Fatalf("first gen: %v", err)
	}
	if code != "MC-NSXS-2024-001" {
		t.Fatalf("expected MC-NSXS-2024-001, got %s", code)
	}

	// 写入项目使下一次能查到
	insertProjectByCode(t, db, code)

	// 第二个项目（同前缀同年分）
	code, err = GenerateProjectCode(db, "MC-NSXS", ts)
	if err != nil {
		t.Fatalf("second gen: %v", err)
	}
	if code != "MC-NSXS-2024-002" {
		t.Fatalf("expected MC-NSXS-2024-002, got %s", code)
	}

	// 不同前缀重新计数
	code, err = GenerateProjectCode(db, "MOJ-NLF", ts)
	if err != nil {
		t.Fatalf("diff prefix: %v", err)
	}
	if code != "MOJ-NLF-2024-001" {
		t.Fatalf("expected MOJ-NLF-2024-001, got %s", code)
	}

	// 不同年分重新计数
	ts2 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	code, err = GenerateProjectCode(db, "MC-NSXS", ts2)
	if err != nil {
		t.Fatalf("diff year: %v", err)
	}
	if code != "MC-NSXS-2025-001" {
		t.Fatalf("expected MC-NSXS-2025-001, got %s", code)
	}
}

func TestGenerateProjectCode_FourDigitOverflow(t *testing.T) {
	db := openTestDB(t)
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// 模拟流水号已到 999
	insertProjectByCode(t, db, "MC-2024-999")
	code, err := GenerateProjectCode(db, "MC", ts)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if code != "MC-2024-1000" {
		t.Fatalf("expected MC-2024-1000, got %s", code)
	}
}

func TestGenerateProjectCode_InvalidShortCode(t *testing.T) {
	db := openTestDB(t)
	if _, err := GenerateProjectCode(db, "mc", time.Now()); err == nil {
		t.Fatal("expected validation error")
	}
}

// helpers ----

func insertProjectByCode(t *testing.T, db *sqlx.DB, code string) {
	t.Helper()
	db.MustExec(`INSERT INTO data_projects (
		project_code, project_name, template_code, template_version, sensitivity_level,
		owner_subject_id, custodian_subject_id, security_subject_id, status, create_time, update_time
	) VALUES (?, ?, 'TPL-X', 'V1.0', 'general', 1, 1, 1, 'draft', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		code, "项目-"+code)
}
