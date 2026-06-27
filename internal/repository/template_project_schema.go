package repository

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// runTemplateProjectMigrations applies schema for the data-business-template
// & project-instantiation feature (V1, scan side).
//
// All statements are idempotent (CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS),
// safe to run on fresh and pre-existing databases.
func runTemplateProjectMigrations(db *sqlx.DB) error {
	stmts := []string{
		// ===========================================================
		// Mirror tables: cached from manage side via /api/templates/full
		// ===========================================================

		// 行业/业务职能分类（镜像）
		`CREATE TABLE IF NOT EXISTS business_classes (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			remote_id    INTEGER,
			code         TEXT NOT NULL UNIQUE,
			name         TEXT NOT NULL,
			type         TEXT NOT NULL,
			parent_id    INTEGER,
			description  TEXT,
			cached_at    DATETIME NOT NULL,
			create_time  DATETIME NOT NULL,
			update_time  DATETIME NOT NULL,
			disable      INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_business_classes_code ON business_classes(code)`,

		// 数据业务模版主表（镜像）
		`CREATE TABLE IF NOT EXISTS data_templates (
			id                          INTEGER PRIMARY KEY AUTOINCREMENT,
			remote_id                   INTEGER,
			template_code               TEXT NOT NULL,
			template_name               TEXT NOT NULL,
			template_version            TEXT NOT NULL,
			class_code                  TEXT,
			scenario                    TEXT,
			publisher                   TEXT,
			status                      TEXT NOT NULL,
			project_sensitivity_level   TEXT NOT NULL,
			use_share_scope             TEXT,
			sharing_open_conditions     TEXT,
			description                 TEXT,
			source_endpoint             TEXT,
			cached_at                   DATETIME NOT NULL,
			create_time                 DATETIME NOT NULL,
			update_time                 DATETIME NOT NULL,
			disable                     INTEGER NOT NULL DEFAULT 0,
			UNIQUE(template_code, template_version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_data_templates_code ON data_templates(template_code)`,
		`CREATE INDEX IF NOT EXISTS idx_data_templates_status ON data_templates(status)`,

		// 模版工作环节（镜像）
		`CREATE TABLE IF NOT EXISTS template_stages (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			remote_id           INTEGER,
			template_id         INTEGER NOT NULL,
			stage_code          TEXT NOT NULL,
			stage_name          TEXT NOT NULL,
			stage_type          TEXT NOT NULL DEFAULT 'process',
			sort_order          INTEGER NOT NULL DEFAULT 0,
			description         TEXT,
			default_role_codes  TEXT,
			cached_at           DATETIME NOT NULL,
			create_time         DATETIME NOT NULL,
			update_time         DATETIME NOT NULL,
			disable             INTEGER NOT NULL DEFAULT 0,
			UNIQUE(template_id, stage_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_template_stages_template ON template_stages(template_id)`,

		// 模版文件版本规则（镜像）
		`CREATE TABLE IF NOT EXISTS template_file_rules (
			id                          INTEGER PRIMARY KEY AUTOINCREMENT,
			remote_id                   INTEGER,
			template_stage_id           INTEGER NOT NULL,
			file_rule_code              TEXT NOT NULL,
			file_name                   TEXT NOT NULL,
			data_state                  TEXT NOT NULL,
			required                    INTEGER NOT NULL DEFAULT 0,
			allowed_file_types          TEXT NOT NULL,
			naming_pattern              TEXT,
			summary_pattern             TEXT,
			default_retention_policy    TEXT,
			default_security_policy_id  INTEGER,
			sort_order                  INTEGER NOT NULL DEFAULT 0,
			cached_at                   DATETIME NOT NULL,
			create_time                 DATETIME NOT NULL,
			update_time                 DATETIME NOT NULL,
			disable                     INTEGER NOT NULL DEFAULT 0,
			UNIQUE(template_stage_id, file_rule_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_template_file_rules_stage ON template_file_rules(template_stage_id)`,

		// 模版文件任务（五层中间层：插在 工作环节 与 文件版本 之间）
		// 2026-05-31 scan 本地五层模版创作引入。同步自 manage 的 4 层模版不产生此层。
		`CREATE TABLE IF NOT EXISTS template_tasks (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			remote_id           INTEGER,
			template_stage_id   INTEGER NOT NULL,
			task_code           TEXT NOT NULL,
			task_name           TEXT NOT NULL,
			manager             TEXT,
			sensitivity_level   TEXT,
			sort_order          INTEGER NOT NULL DEFAULT 0,
			description         TEXT,
			cached_at           DATETIME NOT NULL,
			create_time         DATETIME NOT NULL,
			update_time         DATETIME NOT NULL,
			disable             INTEGER NOT NULL DEFAULT 0,
			UNIQUE(template_stage_id, task_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_template_tasks_stage ON template_tasks(template_stage_id)`,

		// ===========================================================
		// 三主体来源 subjects（归属/保管/安全主体下拉）
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS subjects (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			code         TEXT NOT NULL UNIQUE,
			name         TEXT NOT NULL,
			type         TEXT NOT NULL,
			parent_id    INTEGER,
			contact      TEXT,
			status       TEXT NOT NULL DEFAULT 'active',
			create_time  DATETIME NOT NULL,
			update_time  DATETIME NOT NULL,
			disable      INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_code ON subjects(code)`,
		`CREATE INDEX IF NOT EXISTS idx_subjects_type ON subjects(type)`,

		// ===========================================================
		// 安全策略 security_policies（基线 4 等级 × 5 文件状态）
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS security_policies (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			policy_code         TEXT NOT NULL UNIQUE,
			policy_name         TEXT NOT NULL,
			sensitivity_level   TEXT NOT NULL,
			file_state          TEXT,
			storage_tier        TEXT NOT NULL,
			permissions         TEXT NOT NULL,
			protection_rules    TEXT,
			audit_required      INTEGER NOT NULL DEFAULT 1,
			create_time         DATETIME NOT NULL,
			update_time         DATETIME NOT NULL,
			disable             INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_security_policies_level ON security_policies(sensitivity_level, file_state)`,

		// ===========================================================
		// data_projects 项目实例
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS data_projects (
			id                       INTEGER PRIMARY KEY AUTOINCREMENT,
			project_code             TEXT NOT NULL UNIQUE,
			project_name             TEXT NOT NULL,
			object_short_code        TEXT,
			template_id              INTEGER,
			template_code            TEXT NOT NULL,
			template_version         TEXT NOT NULL,
			task_summary             TEXT,
			approval_basis           TEXT,
			planned_start_date       DATETIME,
			planned_end_date         DATETIME,
			sensitivity_level        TEXT NOT NULL,
			management_mode          TEXT NOT NULL DEFAULT 'independent',
			owner_subject_id         INTEGER NOT NULL,
			custodian_subject_id     INTEGER NOT NULL,
			security_subject_id      INTEGER NOT NULL,
			status                   TEXT NOT NULL DEFAULT 'draft',
			project_root             TEXT,
			sync_status              TEXT,
			sync_message             TEXT,
			synced_at                DATETIME,
			created_by               TEXT,
			create_time              DATETIME NOT NULL,
			update_time              DATETIME NOT NULL,
			disable                  INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_data_projects_code ON data_projects(project_code)`,
		`CREATE INDEX IF NOT EXISTS idx_data_projects_status ON data_projects(status)`,
		`CREATE INDEX IF NOT EXISTS idx_data_projects_template ON data_projects(template_code, template_version)`,

		// ===========================================================
		// project_stages 项目工作环节实例
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS project_stages (
			id                       INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id               INTEGER NOT NULL,
			template_stage_id        INTEGER,
			stage_code               TEXT NOT NULL,
			stage_name               TEXT NOT NULL,
			stage_type               TEXT NOT NULL,
			sort_order               INTEGER NOT NULL,
			status                   TEXT NOT NULL DEFAULT 'pending',
			assigned_role_codes      TEXT,
			directory_path           TEXT,
			create_time              DATETIME NOT NULL,
			update_time              DATETIME NOT NULL,
			disable                  INTEGER NOT NULL DEFAULT 0,
			UNIQUE(project_id, stage_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_stages_project ON project_stages(project_id)`,

		// ===========================================================
		// file_versions 文件版本实例
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS file_versions (
			id                       INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id               INTEGER NOT NULL,
			project_stage_id         INTEGER NOT NULL,
			template_file_rule_id    INTEGER,
			file_version_code        TEXT NOT NULL,
			local_code               TEXT NOT NULL,
			display_name             TEXT NOT NULL,
			data_state               TEXT NOT NULL,
			version_no               TEXT NOT NULL DEFAULT 'V1.0',
			required                 INTEGER NOT NULL DEFAULT 0,
			file_type                TEXT,
			storage_uri              TEXT,
			checksum                 TEXT,
			file_size                INTEGER,
			source_file_version_id   INTEGER,
			produced_from_event_id   INTEGER,
			security_policy_id       INTEGER,
			lifecycle_status         TEXT NOT NULL DEFAULT 'planned',
			cabinet_sync_status      TEXT,
			cabinet_sync_message     TEXT,
			cabinet_synced_at        DATETIME,
			created_by               TEXT,
			create_time              DATETIME NOT NULL,
			update_time              DATETIME NOT NULL,
			disable                  INTEGER NOT NULL DEFAULT 0,
			UNIQUE(project_id, file_version_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_file_versions_project ON file_versions(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_file_versions_stage ON file_versions(project_stage_id)`,
		`CREATE INDEX IF NOT EXISTS idx_file_versions_state ON file_versions(data_state)`,
		`CREATE INDEX IF NOT EXISTS idx_file_versions_lifecycle ON file_versions(lifecycle_status)`,
		`CREATE INDEX IF NOT EXISTS idx_file_versions_source ON file_versions(source_file_version_id)`,

		// ===========================================================
		// asset_ledgers 数据资产标识底账
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS asset_ledgers (
			id                       INTEGER PRIMARY KEY AUTOINCREMENT,
			ledger_code              TEXT NOT NULL UNIQUE,
			file_version_id          INTEGER NOT NULL,
			class_code               TEXT,
			project_code             TEXT NOT NULL,
			stage_code               TEXT NOT NULL,
			file_version_code        TEXT NOT NULL,
			asset_name               TEXT NOT NULL,
			content_summary          TEXT,
			owner_subject_id         INTEGER NOT NULL,
			custodian_subject_id     INTEGER NOT NULL,
			security_subject_id      INTEGER NOT NULL,
			sensitivity_level        TEXT NOT NULL,
			marking_method           TEXT NOT NULL DEFAULT 'reference',
			source_ref               TEXT,
			current_storage_uri      TEXT,
			lifecycle_status         TEXT NOT NULL DEFAULT 'planned',
			create_time              DATETIME NOT NULL,
			update_time              DATETIME NOT NULL,
			disable                  INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_asset_ledgers_file ON asset_ledgers(file_version_id)`,
		`CREATE INDEX IF NOT EXISTS idx_asset_ledgers_project ON asset_ledgers(project_code)`,
		`CREATE INDEX IF NOT EXISTS idx_asset_ledgers_status ON asset_ledgers(lifecycle_status)`,

		// ===========================================================
		// lifecycle_events 生命周期事件流（仅追加）
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS lifecycle_events (
			id                       INTEGER PRIMARY KEY AUTOINCREMENT,
			file_version_id          INTEGER NOT NULL,
			ledger_id                INTEGER,
			event_type               TEXT NOT NULL,
			event_name               TEXT NOT NULL,
			operator_id              TEXT,
			from_subject_id          INTEGER,
			to_subject_id            INTEGER,
			from_storage_uri         TEXT,
			to_storage_uri           TEXT,
			reason                   TEXT,
			approval_ref             TEXT,
			metadata_before          TEXT,
			metadata_after           TEXT,
			create_time              DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_lifecycle_events_file ON lifecycle_events(file_version_id)`,
		`CREATE INDEX IF NOT EXISTS idx_lifecycle_events_ledger ON lifecycle_events(ledger_id)`,
		`CREATE INDEX IF NOT EXISTS idx_lifecycle_events_type ON lifecycle_events(event_type)`,

		// ===========================================================
		// project_members 项目成员与权限
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS project_members (
			id                       INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id               INTEGER NOT NULL,
			user_id                  INTEGER,
			subject_id               INTEGER NOT NULL,
			role_code                TEXT NOT NULL,
			stage_ids                TEXT,
			permission_actions       TEXT NOT NULL,
			create_time              DATETIME NOT NULL,
			update_time              DATETIME NOT NULL,
			disable                  INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_project ON project_members(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_subject ON project_members(subject_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_user ON project_members(user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_project_members_user_role ON project_members(project_id, user_id, role_code) WHERE user_id IS NOT NULL AND disable = 0`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_project_members_subject_role ON project_members(project_id, subject_id, role_code) WHERE user_id IS NULL AND disable = 0`,

		// ===========================================================
		// project_template_baseline 项目模版基线快照（2026-06-27）
		// 关联模版时把「所选原始模版」的五层树快照存一份，作为「提取项目模版」前
		// 差异对比的基线；与项目最终专属模版(TPL-PRJ-<app_key>)对比即得改动清单。
		// 一项目一条（app_key 主键，首次关联即写，复用不覆盖）。
		// ===========================================================
		`CREATE TABLE IF NOT EXISTS project_template_baseline (
			app_key        TEXT PRIMARY KEY,
			source_code    TEXT,
			source_version TEXT,
			baseline_json  TEXT NOT NULL,
			create_time    DATETIME NOT NULL
		)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply template/project migration: %w\nstmt: %s", err, stmt)
		}
	}

	// 已存在表的增列：CREATE TABLE IF NOT EXISTS 不会改既有表，须 ALTER（幂等守卫）。
	// 2026-05-31 模版创作迁到 scan：
	//   - data_templates.origin           区分 local（scan 本地创作）/ synced（manage 同步），默认 synced 保持旧行为
	//   - template_file_rules.template_task_id  本地五层模版把标识挂到「文件任务」上；同步来的 4 层模版留空仍挂 stage
	columnAdds := []struct {
		Table, Column, DDL string
	}{
		{"data_templates", "origin", "ALTER TABLE data_templates ADD COLUMN origin TEXT NOT NULL DEFAULT 'synced'"},
		{"template_file_rules", "template_task_id", "ALTER TABLE template_file_rules ADD COLUMN template_task_id INTEGER"},
		// 项目模版本地创作字段（原型「数据项目模版」表单项；同步来的模版这些列留空）
		{"data_templates", "scope", "ALTER TABLE data_templates ADD COLUMN scope TEXT NOT NULL DEFAULT 'industry'"},         // 模版归类 industry/unit/department/person
		{"data_templates", "is_published", "ALTER TABLE data_templates ADD COLUMN is_published INTEGER NOT NULL DEFAULT 0"}, // 本地模版是否发布：0未发布/1已发布（只有已发布才能立项）
		{"data_templates", "short_code", "ALTER TABLE data_templates ADD COLUMN short_code TEXT"},                           // 代号/简称
		{"data_templates", "manager", "ALTER TABLE data_templates ADD COLUMN manager TEXT"},                                 // 负责人
		{"data_templates", "owner", "ALTER TABLE data_templates ADD COLUMN owner TEXT"},                                     // 数据所有权归属
		{"data_templates", "approval_basis", "ALTER TABLE data_templates ADD COLUMN approval_basis TEXT"},                   // 立项依据
		// 本地模版推送到 manage 的同步状态（阶段二反向同步）
		{"data_templates", "sync_status", "ALTER TABLE data_templates ADD COLUMN sync_status TEXT"},   // ''/synced/error
		{"data_templates", "sync_message", "ALTER TABLE data_templates ADD COLUMN sync_message TEXT"}, // 失败原因
		{"data_templates", "synced_at", "ALTER TABLE data_templates ADD COLUMN synced_at DATETIME"},   // 最近成功推送时间
		// 工作环节(事项) 本地创作字段（原型「工作事项」：责任人/参与人）
		{"template_stages", "manager", "ALTER TABLE template_stages ADD COLUMN manager TEXT"}, // 责任人（显示名）
		{"template_stages", "members", "ALTER TABLE template_stages ADD COLUMN members TEXT"}, // 参与人（显示名，逗号分隔）
		// 2026-05-31 多人协同：责任人/参与人的 username（防重名，「我的工作事项」按 username 过滤匹配登录用户）
		{"template_stages", "manager_username", "ALTER TABLE template_stages ADD COLUMN manager_username TEXT"},   // 责任人 username
		{"template_stages", "members_usernames", "ALTER TABLE template_stages ADD COLUMN members_usernames TEXT"}, // 参与人 username（逗号分隔，与 members 对应）
		// 文件版本(文档标识) 本地创作字段（原型「文档标识」：敏感级别/起草人）
		{"template_file_rules", "sensitivity_level", "ALTER TABLE template_file_rules ADD COLUMN sensitivity_level TEXT"}, // 敏感级别（就高不就低用）
		{"template_file_rules", "drafter", "ALTER TABLE template_file_rules ADD COLUMN drafter TEXT"},                     // 起草人

		// 项目认定模版（2026-06-20）：项目立项过程中编辑过的项目专属模版，立项结束后可"提取"成
		// 单位最高权威模版。edited=立项过程中是否动过结构（提取按钮的门禁）；certified=是否项目认定模版；
		// certified_from=认定来源项目编码（溯源）。
		// L6 文档标识管控类字段（2026-06-21）：文档类别 + 安全/防扩散/归档要求 + 保留期/销毁规则
		{"template_file_rules", "category", "ALTER TABLE template_file_rules ADD COLUMN category TEXT"},
		{"template_file_rules", "security_requirement", "ALTER TABLE template_file_rules ADD COLUMN security_requirement TEXT"},
		{"template_file_rules", "diffusion_requirement", "ALTER TABLE template_file_rules ADD COLUMN diffusion_requirement TEXT"},
		{"template_file_rules", "archive_requirement", "ALTER TABLE template_file_rules ADD COLUMN archive_requirement TEXT"},
		{"template_file_rules", "retention_period_days", "ALTER TABLE template_file_rules ADD COLUMN retention_period_days INTEGER"},
		{"template_file_rules", "destruction_rule", "ALTER TABLE template_file_rules ADD COLUMN destruction_rule TEXT"},

		{"data_templates", "edited", "ALTER TABLE data_templates ADD COLUMN edited INTEGER NOT NULL DEFAULT 0"},
		{"data_templates", "certified", "ALTER TABLE data_templates ADD COLUMN certified INTEGER NOT NULL DEFAULT 0"},
		{"data_templates", "certified_from", "ALTER TABLE data_templates ADD COLUMN certified_from TEXT"},
	}
	for _, c := range columnAdds {
		exists, err := columnExists(db, c.Table, c.Column)
		if err != nil {
			return fmt.Errorf("check column %s.%s: %w", c.Table, c.Column, err)
		}
		if exists {
			continue
		}
		if _, err := db.Exec(c.DDL); err != nil {
			return fmt.Errorf("add column %s.%s: %w", c.Table, c.Column, err)
		}
	}

	return nil
}
