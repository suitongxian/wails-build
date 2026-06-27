package repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// V4-Q2 §4.2 内置 3 个个人文件项目自动建（如果模版同步过）
func TestEnsurePersonalFilesContext_Creates3Projects(t *testing.T) {
	db := openTestDB(t)
	// seed 一个 mock TPL-PERSONAL-FILES 模版（不依赖 manage）
	seedMockPersonalFilesTemplate(t, db)

	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}

	// 验证 3 个项目都建好
	codes := []string{PersonalCoreProjectCode, PersonalImportantProjectCode, PersonalGeneralProjectCode}
	for _, code := range codes {
		var id int64
		if err := db.Get(&id, `SELECT id FROM data_projects WHERE project_code = ? AND disable = 0`, code); err != nil {
			t.Errorf("项目 %s 应当存在: %v", code, err)
			continue
		}
		// 必须 active
		var status string
		db.Get(&status, `SELECT status FROM data_projects WHERE id = ?`, id)
		if status != "active" {
			t.Errorf("%s status 应为 active, got %s", code, status)
		}
	}

	// 验证 "本人" subject
	var subjID int64
	if err := db.Get(&subjID, `SELECT id FROM subjects WHERE code = ?`, PersonalUserSubjectCode); err != nil {
		t.Errorf("'本人' subject 应当存在: %v", err)
	}
}

// V4-Q2 三个项目敏感等级各异
func TestEnsurePersonalFilesContext_CorrectSensitivity(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		PersonalCoreProjectCode:      SensCoreSecret,
		PersonalImportantProjectCode: SensImportant,
		PersonalGeneralProjectCode:   SensGeneral,
	}
	for code, want := range expected {
		var got string
		db.Get(&got, `SELECT sensitivity_level FROM data_projects WHERE project_code = ?`, code)
		if got != want {
			t.Errorf("%s sensitivity_level = %s, want %s", code, got, want)
		}
	}
}

func TestEnsurePersonalFilesContext_PersonalContainersDoNotCreateWorkspaceRoot(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}

	for _, code := range []string{PersonalCoreProjectCode, PersonalImportantProjectCode, PersonalGeneralProjectCode} {
		var root sql.NullString
		if err := db.Get(&root, `SELECT project_root FROM data_projects WHERE project_code = ?`, code); err != nil {
			t.Fatal(err)
		}
		if root.Valid && root.String != "" {
			t.Errorf("%s should not set project_root/workspace directory, got %q", code, root.String)
		}
	}
}

// V4-Q2 幂等 — 重复调用不报错也不重复创建
func TestEnsurePersonalFilesContext_Idempotent(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	for i := 0; i < 3; i++ {
		if err := ensurePersonalFilesContext(db); err != nil {
			t.Fatalf("第 %d 次调用失败: %v", i, err)
		}
	}
	// 验证只有 3 行（不是 9）
	var count int
	db.Get(&count, `SELECT COUNT(*) FROM data_projects WHERE project_code LIKE 'SYS-PERSONAL-%'`)
	if count != 3 {
		t.Errorf("幂等性破坏：应为 3 个项目，got %d", count)
	}
}

// V4-Q2 模版未同步时静默跳过，不报错
func TestEnsurePersonalFilesContext_SkipsWhenTemplateMissing(t *testing.T) {
	db := openTestDB(t)
	// 不 seed 模版
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Errorf("模版缺失时不应报错: %v", err)
	}
	var count int
	db.Get(&count, `SELECT COUNT(*) FROM data_projects WHERE project_code LIKE 'SYS-PERSONAL-%'`)
	if count != 0 {
		t.Errorf("模版缺失时不应创建项目，got %d", count)
	}
}

// V4-Q2 每个内置项目应有 3 条 planned file_versions（IN/PRC/OUT 各 1）
func TestEnsurePersonalFilesContext_PlannedFVsCreated(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{PersonalCoreProjectCode, PersonalImportantProjectCode, PersonalGeneralProjectCode} {
		var count int
		db.Get(&count, `SELECT COUNT(*) FROM file_versions fv
			JOIN data_projects p ON p.id = fv.project_id
			WHERE p.project_code = ? AND fv.lifecycle_status = 'planned'`, code)
		if count != 3 {
			t.Errorf("%s 应有 3 条 planned fv (IN/PRC/OUT), got %d", code, count)
		}
	}
}

// V5-P1 Task2 §4.2-6 TPL-PERSONAL-FILES V2.0 起草修改 + 定稿 两个工作环节
//
// 当本地缓存中存在 V2.0 模版时，ensurePersonalFilesContext 应优先用 V2.0
// 实例化 3 个内置项目，每个项目的 project_stages 应有 2 条
// （GR-DRAFT + GR-FINAL）。
func TestEnsurePersonalFilesContext_V2_TwoStages(t *testing.T) {
	db := openTestDB(t)

	// Seed V2.0 模版（不 seed V1.0，验证 V2.0 优先生效）
	seedPersonalFilesTemplateV2InTest(t, db)

	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatalf("ensure failed: %v", err)
	}

	// 3 个项目都应有 2 个工作环节（GR-DRAFT + GR-FINAL）
	for _, code := range []string{PersonalCoreProjectCode, PersonalImportantProjectCode, PersonalGeneralProjectCode} {
		var projID int64
		if err := db.Get(&projID, `SELECT id FROM data_projects WHERE project_code = ?`, code); err != nil {
			t.Fatalf("project %s not found: %v", code, err)
		}
		type stageRow struct {
			StageCode string `db:"stage_code"`
			SortOrder int    `db:"sort_order"`
		}
		var stages []stageRow
		if err := db.Select(&stages, `SELECT stage_code, sort_order FROM project_stages WHERE project_id = ? ORDER BY sort_order`, projID); err != nil {
			t.Fatal(err)
		}
		if len(stages) != 2 {
			t.Errorf("project %s should have 2 stages, got %d", code, len(stages))
		}
		if len(stages) >= 2 && (stages[0].StageCode != "GR-DRAFT" || stages[1].StageCode != "GR-FINAL") {
			t.Errorf("project %s stages should be [GR-DRAFT, GR-FINAL], got %v", code, stages)
		}

		// 同时验证 data_projects.template_version 也是 V2.0
		var ver string
		_ = db.Get(&ver, `SELECT template_version FROM data_projects WHERE id = ?`, projID)
		if ver != "V2.0" {
			t.Errorf("project %s template_version = %q, want V2.0", code, ver)
		}
	}
}

// seedPersonalFilesTemplateV2InTest seed V2.0 模版（双环节：GR-DRAFT 起草修改 / GR-FINAL 定稿）
func seedPersonalFilesTemplateV2InTest(t *testing.T, db *sqlx.DB) {
	t.Helper()
	now := time.Now()
	res, err := db.Exec(`INSERT INTO data_templates (
		template_code, template_name, template_version, status, project_sensitivity_level,
		cached_at, create_time, update_time, disable
	) VALUES ('TPL-PERSONAL-FILES', '个人文件项目化管理模版', 'V2.0', 'active', 'general', ?, ?, ?, 0)`,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	tplID, _ := res.LastInsertId()
	draftRes, err := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-DRAFT', '个人文件起草与修改', 'process', 1, ?, ?, ?, 0)`, tplID, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	draftID, _ := draftRes.LastInsertId()
	finalRes, err := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-FINAL', '个人文件定稿', 'record', 2, ?, ?, ?, 0)`, tplID, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	finalID, _ := finalRes.LastInsertId()
	if _, err := db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'PRC-001', '过程版本', 'process', 0, '["*"]', ?, ?, ?, 0)`, draftID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'OUT-001', '归档定稿', 'output', 0, '["*"]', ?, ?, ?, 0)`, finalID, now, now, now); err != nil {
		t.Fatal(err)
	}
}

// 测试用：seed 一个最小可用的 TPL-PERSONAL-FILES 模版（不走 manage）
func seedMockPersonalFilesTemplate(t *testing.T, db *sqlx.DB) {
	t.Helper()
	now := time.Now()
	res, err := db.Exec(`INSERT INTO data_templates (
		template_code, template_name, template_version, status, project_sensitivity_level,
		cached_at, create_time, update_time, disable
	) VALUES (?, ?, ?, 'active', 'general', ?, ?, ?, 0)`,
		"TPL-PERSONAL-FILES", "个人文件项目化管理模版", "V1.0", now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	tplID, _ := res.LastInsertId()
	stageRes, err := db.Exec(`INSERT INTO template_stages (
		template_id, stage_code, stage_name, stage_type, sort_order,
		cached_at, create_time, update_time, disable
	) VALUES (?, 'GR-DA', '个人归档', 'record', 1, ?, ?, ?, 0)`, tplID, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	stageID, _ := stageRes.LastInsertId()
	for _, r := range []struct {
		code, name, state string
	}{
		{"IN-001", "来源文件", "input"},
		{"PRC-001", "过程版本", "process"},
		{"OUT-001", "归档定稿", "output"},
	} {
		_, err := db.Exec(`INSERT INTO template_file_rules (
			template_stage_id, file_rule_code, file_name, data_state,
			required, allowed_file_types, cached_at, create_time, update_time, disable
		) VALUES (?, ?, ?, ?, 0, '["*"]', ?, ?, ?, 0)`,
			stageID, r.code, r.name, r.state, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
}
