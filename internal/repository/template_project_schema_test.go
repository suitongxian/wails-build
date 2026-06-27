package repository

import (
	"testing"
)

// TestTemplateProjectSchema_AllTablesExist 验证 V1 全部新表都已创建
func TestTemplateProjectSchema_AllTablesExist(t *testing.T) {
	db := openTestDB(t)

	tables := []string{
		// 镜像表
		"business_classes",
		"data_templates",
		"template_stages",
		"template_file_rules",
		// 三主体
		"subjects",
		// 安全策略
		"security_policies",
		// 项目卷宗
		"data_projects",
		"project_stages",
		"file_versions",
		"asset_ledgers",
		"lifecycle_events",
		"project_members",
	}

	for _, tab := range tables {
		var n int
		if err := db.Get(&n, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tab); err != nil {
			t.Fatalf("查询 %s 表存在性失败: %v", tab, err)
		}
		if n != 1 {
			t.Fatalf("表 %s 未创建", tab)
		}
	}
}

// TestTemplateProjectSchema_TaskLayerAndOrigin 验证「文件任务」中间层与本地创作标记
//
// 2026-05-31 模版创作迁到 scan：在 工作环节(stage) 与 文件版本(file_rule) 之间
// 插入「文件任务(template_tasks)」中间层，凑齐五层；data_templates 增 origin
// 区分 scan 本地创作(local) 与从 manage 同步(synced)；file_rules 增 template_task_id
// 以便本地五层模版把标识挂到任务上（同步来的 4 层模版该列留空，仍挂 stage）。
func TestTemplateProjectSchema_TaskLayerAndOrigin(t *testing.T) {
	db := openTestDB(t)

	// template_tasks 表存在
	var n int
	if err := db.Get(&n, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='template_tasks'"); err != nil {
		t.Fatalf("查询 template_tasks 表存在性失败: %v", err)
	}
	if n != 1 {
		t.Fatal("表 template_tasks 未创建")
	}

	// data_templates.origin 列存在
	if ok, err := columnExists(db, "data_templates", "origin"); err != nil {
		t.Fatalf("检查 data_templates.origin 失败: %v", err)
	} else if !ok {
		t.Fatal("data_templates 缺少 origin 列")
	}

	// template_file_rules.template_task_id 列存在
	if ok, err := columnExists(db, "template_file_rules", "template_task_id"); err != nil {
		t.Fatalf("检查 template_file_rules.template_task_id 失败: %v", err)
	} else if !ok {
		t.Fatal("template_file_rules 缺少 template_task_id 列")
	}

	// template_tasks 的 (template_stage_id, task_code) 唯一
	insertTask := func(stageID int64, code string) error {
		_, e := db.Exec(`INSERT INTO template_tasks (template_stage_id, task_code, task_name, sort_order, cached_at, create_time, update_time)
			VALUES (?, ?, '任务', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, stageID, code)
		return e
	}
	if err := insertTask(1, "TK-001"); err != nil {
		t.Fatalf("第一次插入 template_tasks 失败: %v", err)
	}
	if err := insertTask(1, "TK-001"); err == nil {
		t.Fatal("期望 (template_stage_id, task_code) 唯一约束触发")
	}
	if err := insertTask(2, "TK-001"); err != nil {
		t.Fatalf("不同环节允许相同 task_code: %v", err)
	}

	// origin 默认值为 'synced'（不显式给值时）
	if _, err := db.Exec(`INSERT INTO data_templates (template_code, template_name, template_version, status, project_sensitivity_level, cached_at, create_time, update_time)
		VALUES ('TPL-ORIGIN', 'x', 'V1.0', 'draft', 'general', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("插入 data_templates 失败: %v", err)
	}
	var origin string
	if err := db.Get(&origin, "SELECT origin FROM data_templates WHERE template_code='TPL-ORIGIN'"); err != nil {
		t.Fatalf("读取 origin 失败: %v", err)
	}
	if origin != "synced" {
		t.Fatalf("origin 默认值应为 'synced'，实得 %q", origin)
	}
}

// TestTemplateProjectSchema_Idempotent 验证迁移可重复执行
func TestTemplateProjectSchema_Idempotent(t *testing.T) {
	db := openTestDB(t)
	if err := runTemplateProjectMigrations(db); err != nil {
		t.Fatalf("第二次执行迁移失败（应当幂等）: %v", err)
	}
	if err := runTemplateProjectMigrations(db); err != nil {
		t.Fatalf("第三次执行迁移失败（应当幂等）: %v", err)
	}
}

// TestTemplateProjectSchema_UniqueConstraints 验证关键唯一约束
func TestTemplateProjectSchema_UniqueConstraints(t *testing.T) {
	db := openTestDB(t)

	// data_templates 的 (template_code, template_version) 唯一
	_, err := db.Exec(`INSERT INTO data_templates (template_code, template_name, template_version, status, project_sensitivity_level, cached_at, create_time, update_time) VALUES ('TPL-A', 'A', 'V1.0', 'active', 'general', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("第一次插入 data_templates 失败: %v", err)
	}
	_, err = db.Exec(`INSERT INTO data_templates (template_code, template_name, template_version, status, project_sensitivity_level, cached_at, create_time, update_time) VALUES ('TPL-A', 'A 重复', 'V1.0', 'active', 'general', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err == nil {
		t.Fatal("期望唯一约束触发，但插入成功")
	}
	// 不同 version 应该可以
	_, err = db.Exec(`INSERT INTO data_templates (template_code, template_name, template_version, status, project_sensitivity_level, cached_at, create_time, update_time) VALUES ('TPL-A', 'A 新版本', 'V1.1', 'active', 'general', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("不同版本应可插入: %v", err)
	}

	// data_projects 的 project_code 唯一
	insertProject := func(code string) error {
		_, e := db.Exec(`INSERT INTO data_projects (project_code, project_name, template_code, template_version, sensitivity_level, owner_subject_id, custodian_subject_id, security_subject_id, status, create_time, update_time)
			VALUES (?, '项目', 'TPL-A', 'V1.0', 'general', 1, 1, 1, 'draft', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, code)
		return e
	}
	if err := insertProject("MC-2024-001"); err != nil {
		t.Fatalf("第一次插入项目失败: %v", err)
	}
	if err := insertProject("MC-2024-001"); err == nil {
		t.Fatal("期望 project_code 唯一约束触发")
	}

	// file_versions (project_id, file_version_code) 唯一
	insertFV := func(projectID int64, code string) error {
		_, e := db.Exec(`INSERT INTO file_versions (project_id, project_stage_id, file_version_code, local_code, display_name, data_state, version_no, lifecycle_status, create_time, update_time)
			VALUES (?, 1, ?, 'IN-001', 'x', 'input', 'V1.0', 'planned', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, projectID, code)
		return e
	}
	if err := insertFV(1, "MC-2024-001-MZ-SG-IN-001"); err != nil {
		t.Fatalf("第一次插入 file_versions 失败: %v", err)
	}
	if err := insertFV(1, "MC-2024-001-MZ-SG-IN-001"); err == nil {
		t.Fatal("同项目同编码应当唯一")
	}
	if err := insertFV(2, "MC-2024-001-MZ-SG-IN-001"); err != nil {
		t.Fatalf("不同项目允许相同 file_version_code: %v", err)
	}

	// asset_ledgers.ledger_code 唯一
	if _, err := db.Exec(`INSERT INTO asset_ledgers (ledger_code, file_version_id, project_code, stage_code, file_version_code, asset_name, owner_subject_id, custodian_subject_id, security_subject_id, sensitivity_level, marking_method, lifecycle_status, create_time, update_time) VALUES ('L-001', 1, 'MC-2024-001', 'MZ-SG', 'IN-001', '原稿', 1, 1, 1, 'general', 'reference', 'planned', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("第一次插入 ledger 失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO asset_ledgers (ledger_code, file_version_id, project_code, stage_code, file_version_code, asset_name, owner_subject_id, custodian_subject_id, security_subject_id, sensitivity_level, marking_method, lifecycle_status, create_time, update_time) VALUES ('L-001', 2, 'MC-2024-002', 'MZ-SG', 'IN-001', '原稿', 1, 1, 1, 'general', 'reference', 'planned', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("ledger_code 唯一约束应当生效")
	}
}
