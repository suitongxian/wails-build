package repository

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// schemaSQL is embedded so no external file access is needed at runtime
var schemaSQL = `-- 系统配置表 system_config
CREATE TABLE system_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT UNIQUE NOT NULL,
    type TEXT NOT NULL,
    value TEXT,
    describe TEXT,
    create_time DATETIME NOT NULL,
    update_time DATETIME NOT NULL,
    disable INTEGER NOT NULL DEFAULT 0
);

-- 扫描任务表 scan_task
CREATE TABLE scan_task (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scan_type TEXT NOT NULL,
    file_scan_range TEXT,
    heartbeat INTEGER NOT NULL,
    workspace_path TEXT,
    task_state INTEGER NOT NULL,
    task_phase TEXT,
    task_error_message TEXT,
    scan_args TEXT,
    file_total INTEGER,
    file_scanned_count INTEGER,
    file_all_suffix_text TEXT,
    file_all_suffix_count INTEGER,
    file_count_suffix_count INTEGER,
    workspace_count INTEGER,
    end_time DATETIME,
    scan_log TEXT,
    create_time DATETIME NOT NULL,
    update_time DATETIME NOT NULL,
    disable INTEGER NOT NULL DEFAULT 0
);

-- 数据分布表 data_distributing
CREATE TABLE data_distributing (
    data_distribution_id INTEGER PRIMARY KEY AUTOINCREMENT,
    scan_task_id INTEGER,
    path TEXT NOT NULL,
    data_type INTEGER NOT NULL,
    scan_found_count INTEGER NOT NULL,
    content_sign TEXT NOT NULL,
    file_suffix TEXT,
    file_magic TEXT,
    file_create_time DATETIME,
    file_update_time DATETIME,
    file_read_time DATETIME,
    file_size INTEGER NOT NULL,
    file_hide INTEGER DEFAULT 0,
    upload_state INTEGER DEFAULT 0,
    ip TEXT NOT NULL,
    mac_address TEXT NOT NULL,
    parent_id INTEGER,
    scan_time DATETIME NOT NULL,
    create_time DATETIME NOT NULL,
    update_time DATETIME NOT NULL,
    disable INTEGER NOT NULL DEFAULT 0
);

-- 信息资源表 data_resources
CREATE TABLE data_resources (
    data_resources_id INTEGER PRIMARY KEY AUTOINCREMENT,
    content_sign TEXT NOT NULL,
    source_count INTEGER NOT NULL,
    workspace_source_count INTEGER NOT NULL,
    first_create_time DATETIME NOT NULL,
    resources_name TEXT,
    resources_desc TEXT,
    content_subject TEXT,
    content_type TEXT,
    is_claimed INTEGER DEFAULT 0,
    claim_status INTEGER DEFAULT 0,
    importance_level INTEGER DEFAULT 0,
    claim_time DATETIME,
    claimant_name TEXT,
    claimant_unit TEXT,
    data_level TEXT,
    data_share TEXT,
    file_magic TEXT,
    create_time DATETIME NOT NULL,
    update_time DATETIME NOT NULL,
    disable INTEGER NOT NULL DEFAULT 0
);

-- 文件数量统计表 file_statistics
CREATE TABLE file_statistics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scan_task_id INTEGER NOT NULL,
    file_total INTEGER NOT NULL DEFAULT 0,
    workspace_file_total INTEGER NOT NULL DEFAULT 0,
    history_file_count INTEGER NOT NULL DEFAULT 0,
    non_history_file_count INTEGER NOT NULL DEFAULT 0,
    workspace_file_claimed_count INTEGER NOT NULL DEFAULT 0,
    history_file_claimed_count INTEGER NOT NULL DEFAULT 0,
    non_history_file_claimed_count INTEGER NOT NULL DEFAULT 0,
    create_time DATETIME NOT NULL,
    update_time DATETIME NOT NULL,
    disable INTEGER NOT NULL DEFAULT 0
);

-- 创建用户信息表
CREATE TABLE user_info (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    company_name TEXT NOT NULL,
    user_name TEXT NOT NULL,
    department TEXT NOT NULL,
    ip TEXT NOT NULL,
    mac_address TEXT NOT NULL,
    work_address TEXT,
    phone TEXT,
    password_md5 TEXT,
    id_card TEXT UNIQUE,
    create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    disable INTEGER NOT NULL DEFAULT 0
);`

var (
	db     *sqlx.DB
	once   sync.Once
	dbPath string
)

// InitDB initializes the database connection with the given path
func InitDB(databasePath string) error {
	var err error
	once.Do(func() {
		dbPath = databasePath
		db, err = openDB(databasePath)
	})
	return err
}

// openDB opens a SQLite database connection
func openDB(databasePath string) (*sqlx.DB, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(databasePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	sqlDB, err := sql.Open("sqlite3", databasePath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	sqlDB.SetMaxOpenConns(1) // SQLite only supports one writer at a time
	sqlDB.SetMaxIdleConns(1)

	// Verify connection
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := sqlx.NewDb(sqlDB, "sqlite3")

	// Load schema if tables don't exist
	if err := loadSchema(db); err != nil {
		return nil, fmt.Errorf("failed to load schema: %w", err)
	}

	// Apply idempotent migrations for tables/columns added after the initial schema
	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// 不再在启动时种入演示模版：本地模版库保持干净，由用户/中心下发真实模版。

	return db, nil
}

// RunMigrationsForTest is the test-only entry point for runMigrations.
// Production code calls runMigrations from openDB; this lets external test
// packages bring up a fresh DB without going through the package singleton.
func RunMigrationsForTest(db *sqlx.DB) error {
	if err := loadSchema(db); err != nil {
		return err
	}
	return runMigrations(db)
}

// seedClaimFamilyDefaults seeds claim_family_default_policy and claim_family_skip_dialog
// to system_config table with default values. Uses INSERT OR IGNORE to preserve existing
// user-set values. Idempotent.
func seedClaimFamilyDefaults(db *sqlx.DB) error {
	seeds := map[string]string{
		KeyClaimFamilyDefaultPolicy: ClaimFamilyPolicySameContentOnly,
		KeyClaimFamilySkipDialog:    "false",
	}
	now := time.Now()
	for key, val := range seeds {
		_, err := db.Exec(`INSERT OR IGNORE INTO system_config
			(key, type, value, create_time, update_time, disable)
			VALUES (?, 'string', ?, ?, ?, 0)`, key, val, now, now)
		if err != nil {
			return fmt.Errorf("seed %s: %w", key, err)
		}
	}
	return nil
}

// runMigrations applies idempotent schema changes added after the initial release.
// Each statement uses IF NOT EXISTS or a column-existence guard so it is safe to
// run on both fresh databases and databases populated by an older build.
func runMigrations(db *sqlx.DB) error {
	// New tables — IF NOT EXISTS makes these idempotent.
	tableStmts := []string{
		`CREATE TABLE IF NOT EXISTS data_resource_family (
			family_id            INTEGER PRIMARY KEY AUTOINCREMENT,
			primary_content_sign TEXT NOT NULL,
			primary_resource_id  INTEGER,
			member_count         INTEGER NOT NULL DEFAULT 0,
			algorithm            TEXT,
			highest_score        REAL,
			analyze_task_id      INTEGER,
			create_time          DATETIME NOT NULL,
			update_time          DATETIME NOT NULL,
			disable              INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_family_primary_cs ON data_resource_family(primary_content_sign)`,
		`CREATE TABLE IF NOT EXISTS content_text_cache (
			content_sign      TEXT PRIMARY KEY,
			extracted_text    TEXT,
			normalized_hash   TEXT,
			text_byte_size    INTEGER,
			extract_status    TEXT NOT NULL,
			create_time       DATETIME NOT NULL,
			update_time       DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_text_cache_norm ON content_text_cache(normalized_hash)`,
		`CREATE TABLE IF NOT EXISTS similarity_task (
			task_id        INTEGER PRIMARY KEY AUTOINCREMENT,
			task_state     TEXT NOT NULL,
			phase          TEXT,
			input_count    INTEGER NOT NULL DEFAULT 0,
			family_count   INTEGER NOT NULL DEFAULT 0,
			member_count   INTEGER NOT NULL DEFAULT 0,
			error_message  TEXT,
			start_time     DATETIME NOT NULL,
			end_time       DATETIME,
			create_time    DATETIME NOT NULL,
			update_time    DATETIME NOT NULL
		)`,
		// V5-Phase1 §4.3-4 解绑 + 重新归类历史表
		`CREATE TABLE IF NOT EXISTS reclassify_history (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			original_fv_id   INTEGER NOT NULL,
			new_fv_id        INTEGER NULL,
			action           TEXT NOT NULL,
			reason           TEXT NOT NULL,
			operator_user_id INTEGER NULL,
			operator_name    TEXT NULL,
			create_time      DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reclassify_history_orig ON reclassify_history(original_fv_id)`,
		// 2026-05-21 数据业务集中立项：简化的意向登记表，与正式立项 data_projects 解耦
		`CREATE TABLE IF NOT EXISTS centralized_project_applications (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			project_name TEXT NOT NULL,
			owner_name   TEXT NOT NULL,
			submitted_by TEXT,
			status       TEXT NOT NULL DEFAULT 'pending',
			create_time  DATETIME NOT NULL,
			update_time  DATETIME NOT NULL,
			disable      INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cpa_status ON centralized_project_applications(status, create_time)`,
	}
	for _, stmt := range tableStmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply migration table: %w\nstmt: %s", err, stmt)
		}
	}

	// 数据业务模版与项目卷宗相关迁移（V1）
	if err := runTemplateProjectMigrations(db); err != nil {
		return fmt.Errorf("apply template/project migrations: %w", err)
	}

	// 安全策略基线 seed（幂等）
	if err := seedSecurityPolicies(db); err != nil {
		return fmt.Errorf("seed security policies: %w", err)
	}

	// V2 身份体系：users 表（独立于 user_info 与 subjects）
	usersDDL := `CREATE TABLE IF NOT EXISTS users (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		username      TEXT NOT NULL UNIQUE,
		display_name  TEXT NOT NULL,
		company_name  TEXT NOT NULL DEFAULT '',
		department    TEXT NOT NULL DEFAULT '',
		ip            TEXT NOT NULL DEFAULT '',
		mac_address   TEXT NOT NULL DEFAULT '',
		work_address  TEXT,
		phone         TEXT,
		status        TEXT NOT NULL DEFAULT 'active',
		create_time   DATETIME NOT NULL,
		update_time   DATETIME NOT NULL,
		disable       INTEGER NOT NULL DEFAULT 0
	)`
	if _, err := db.Exec(usersDDL); err != nil {
		return fmt.Errorf("create users table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`); err != nil {
		return fmt.Errorf("create users index: %w", err)
	}
	// 一次性把 user_info 现有数据迁到 users（幂等）
	if err := migrateUserInfoToUsers(db); err != nil {
		return fmt.Errorf("migrate user_info -> users: %w", err)
	}

	// Columns added to data_resources — PRAGMA-based existence guard.
	columnAdds := []struct {
		Table, Column, DDL string
	}{
		{"data_resources", "family_id", "ALTER TABLE data_resources ADD COLUMN family_id INTEGER"},
		{"data_resources", "family_relation", "ALTER TABLE data_resources ADD COLUMN family_relation TEXT"},
		{"data_resources", "family_score", "ALTER TABLE data_resources ADD COLUMN family_score REAL"},
		// V5-Phase1 §4.3-2 AI 归目"驳回"：人工拒绝归目后过滤出 pending 列表
		{"data_resources", "ai_classify_rejected_at", "ALTER TABLE data_resources ADD COLUMN ai_classify_rejected_at DATETIME"},
		{"data_resources", "ai_classify_reject_reason", "ALTER TABLE data_resources ADD COLUMN ai_classify_reject_reason TEXT"},
		// V1 文件版本扩展
		{"file_versions", "submitted_at", "ALTER TABLE file_versions ADD COLUMN submitted_at DATETIME"},
		{"file_versions", "submitted_by", "ALTER TABLE file_versions ADD COLUMN submitted_by TEXT"},
		// V2 身份体系：project_members 加 user_id（与 subject_id 并存过渡）
		{"project_members", "user_id", "ALTER TABLE project_members ADD COLUMN user_id INTEGER"},
		// 2026-06-13 组建团队 v2：users 加 role（从 manage 同步的账号角色，用于组队/分工展示）
		{"users", "role", "ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT ''"},
		{"file_versions", "original_file_name", "ALTER TABLE file_versions ADD COLUMN original_file_name TEXT"},
		// V2 审计字段：与 V1 TEXT 字段并存，逐步过渡到 user_id
		{"data_projects", "created_by_user_id", "ALTER TABLE data_projects ADD COLUMN created_by_user_id INTEGER"},
		{"file_versions", "created_by_user_id", "ALTER TABLE file_versions ADD COLUMN created_by_user_id INTEGER"},
		{"file_versions", "submitted_by_user_id", "ALTER TABLE file_versions ADD COLUMN submitted_by_user_id INTEGER"},
		{"lifecycle_events", "operator_user_id", "ALTER TABLE lifecycle_events ADD COLUMN operator_user_id INTEGER"},
		// V5-P5 工作数据提交后上报 manage 部门柜的状态
		{"file_versions", "cabinet_sync_status", "ALTER TABLE file_versions ADD COLUMN cabinet_sync_status TEXT"},
		{"file_versions", "cabinet_sync_message", "ALTER TABLE file_versions ADD COLUMN cabinet_sync_message TEXT"},
		{"file_versions", "cabinet_synced_at", "ALTER TABLE file_versions ADD COLUMN cabinet_synced_at DATETIME"},
		// V5-Phase1 §4.3-4 解绑 + 重新归类：file_versions 加三列
		{"file_versions", "unbind_time", "ALTER TABLE file_versions ADD COLUMN unbind_time DATETIME"},
		{"file_versions", "unbind_reason", "ALTER TABLE file_versions ADD COLUMN unbind_reason TEXT"},
		{"file_versions", "reclassified_from_fv_id", "ALTER TABLE file_versions ADD COLUMN reclassified_from_fv_id INTEGER"},
		// 2026-05-21 集中立项推送 / 审核回写：在 centralized_project_applications 上加 5 列
		{"centralized_project_applications", "manage_remote_id", "ALTER TABLE centralized_project_applications ADD COLUMN manage_remote_id INTEGER"},
		{"centralized_project_applications", "sync_status", "ALTER TABLE centralized_project_applications ADD COLUMN sync_status TEXT NOT NULL DEFAULT 'pending'"},
		{"centralized_project_applications", "sync_error", "ALTER TABLE centralized_project_applications ADD COLUMN sync_error TEXT"},
		{"centralized_project_applications", "reject_reason", "ALTER TABLE centralized_project_applications ADD COLUMN reject_reason TEXT"},
		{"centralized_project_applications", "reviewed_at", "ALTER TABLE centralized_project_applications ADD COLUMN reviewed_at DATETIME"},
		// 2026-05-22 立项敏感等级：core / important / general（应用层校验，DB 不加 CHECK）
		{"centralized_project_applications", "sensitivity_level", "ALTER TABLE centralized_project_applications ADD COLUMN sensitivity_level TEXT NOT NULL DEFAULT 'general'"},
		{"centralized_project_applications", "project_scope", "ALTER TABLE centralized_project_applications ADD COLUMN project_scope TEXT NOT NULL DEFAULT 'unit'"}, // 2026-06-24 项目层级 person/department/unit（决定 夹/柜/室 + 本地/上云）
		{"centralized_project_applications", "output_custody_scope", "ALTER TABLE centralized_project_applications ADD COLUMN output_custody_scope TEXT NOT NULL DEFAULT 'unit'"}, // 2026-06-24 定稿保管层级（单位级项目可改投 department）
		{"centralized_project_applications", "output_custody_note", "ALTER TABLE centralized_project_applications ADD COLUMN output_custody_note TEXT"},                       // 2026-06-24 归档归属说明（选填）
		{"centralized_project_applications", "data_owner", "ALTER TABLE centralized_project_applications ADD COLUMN data_owner TEXT"}, // 2026-06-02 定数权（数据所有权归属）
		// 2026-06-08 立项书：基本信息/责任主体/立项依据/项目简介 4 列
		{"centralized_project_applications", "project_code", "ALTER TABLE centralized_project_applications ADD COLUMN project_code TEXT"},
		{"centralized_project_applications", "department", "ALTER TABLE centralized_project_applications ADD COLUMN department TEXT"},
		{"centralized_project_applications", "approval_basis", "ALTER TABLE centralized_project_applications ADD COLUMN approval_basis TEXT"},
		{"centralized_project_applications", "description", "ALTER TABLE centralized_project_applications ADD COLUMN description TEXT"},
		// 2026-05-20 历史/新数据区分：INSERT 时根据 baseline 标记 'historical'|'new'，落库后不变
		{"data_resources", "data_origin", "ALTER TABLE data_resources ADD COLUMN data_origin TEXT NOT NULL DEFAULT 'new'"},
		// 2026-05-21 三级分流治理：核心登记 5 列 + 家族权威源 1 列
		{"asset_ledgers", "memorandum_topic", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_topic TEXT"},
		{"asset_ledgers", "memorandum_classification", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_classification TEXT"},
		{"asset_ledgers", "memorandum_registered_at", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_registered_at DATETIME"},
		{"asset_ledgers", "memorandum_registered_by", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_registered_by INTEGER"},
		{"asset_ledgers", "memorandum_signature_hash", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_signature_hash TEXT"},
		{"data_resource_family", "authoritative_resource_id", "ALTER TABLE data_resource_family ADD COLUMN authoritative_resource_id INTEGER"},
		// Spec B: 扫描期预计算文件特征（6 列）
		{"data_distributing", "simhash", "ALTER TABLE data_distributing ADD COLUMN simhash INTEGER"},
		{"data_distributing", "content_hash", "ALTER TABLE data_distributing ADD COLUMN content_hash TEXT"},
		{"data_distributing", "phash", "ALTER TABLE data_distributing ADD COLUMN phash TEXT"},
		{"data_distributing", "extracted_text", "ALTER TABLE data_distributing ADD COLUMN extracted_text TEXT"},
		{"data_distributing", "feature_mtime", "ALTER TABLE data_distributing ADD COLUMN feature_mtime DATETIME"},
		{"data_distributing", "feature_size", "ALTER TABLE data_distributing ADD COLUMN feature_size INTEGER"},
		// 2026-05-27 suspect 标识：扫描期判断文件是否疑似非个人（系统目录 / 二进制后缀等），
		// 认领页给行级 ⚠ 提示 + 顶部一键忽略入口。
		// 0=未识别（默认）/ 1=疑似非个人
		{"data_distributing", "suspect_non_personal", "ALTER TABLE data_distributing ADD COLUMN suspect_non_personal INTEGER NOT NULL DEFAULT 0"},
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

	// 2026-05-20 历史/新数据区分：
	//  - 索引服务于 AI 归目 pending 查询的常用三联条件
	//  - 在 baseline 关闭前，所有存量行回填为 historical（幂等：关闭后不再匹配）
	//  - 种子化 baseline_completed_at 配置行，留 NULL 等首次扫描完成时落值
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_data_resources_origin_claim
		ON data_resources(data_origin, claim_status, importance_level)`); err != nil {
		return fmt.Errorf("create idx_data_resources_origin_claim: %w", err)
	}
	if _, err := db.Exec(`UPDATE data_resources
		SET data_origin = 'historical'
		WHERE data_origin = 'new'
		  AND NOT EXISTS (
		      SELECT 1 FROM system_config
		      WHERE key = 'baseline_completed_at' AND value IS NOT NULL AND value <> '' AND disable = 0
		  )`); err != nil {
		return fmt.Errorf("backfill historical data_origin: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO system_config (key, type, value, create_time, update_time, disable)
		VALUES ('baseline_completed_at', 'string', NULL, ?, ?, 0)`,
		time.Now(), time.Now()); err != nil {
		return fmt.Errorf("seed baseline_completed_at: %w", err)
	}

	// Spec A 2026-05-26: 声明家族交互 claim_family_default_policy 和 claim_family_skip_dialog 配置
	// 使用 INSERT OR IGNORE 确保不覆盖用户已设置的值（幂等）
	if err := seedClaimFamilyDefaults(db); err != nil {
		return fmt.Errorf("seed claim family defaults: %w", err)
	}

	// Plan B Task 8: feature precompute on/off, default on
	{
		now := time.Now()
		if _, err := db.Exec(`INSERT OR IGNORE INTO system_config
			(key, type, value, create_time, update_time, disable)
			VALUES (?, 'string', ?, ?, ?, 0)`,
			KeyFeaturePrecomputeEnabled, "true", now, now); err != nil {
			return fmt.Errorf("seed feature_precompute_enabled: %w", err)
		}
	}

	// 2026-05-26 family_dirty seed：扫描后置 1，分析成功后置 0。
	// 首次注入时根据 data_resource_family 是否有历史数据决定默认：
	//   有历史家族 → "0"（老用户不会被打扰要重建）
	//   无历史家族 → "1"（新用户应当点「重建相似关系」来触发首次构建）
	// INSERT OR IGNORE 保证不覆盖已存在值。
	{
		var existingFamilies int
		_ = db.Get(&existingFamilies, `SELECT COUNT(*) FROM data_resource_family`)
		initial := "1"
		if existingFamilies > 0 {
			initial = "0"
		}
		now := time.Now()
		if _, err := db.Exec(`INSERT OR IGNORE INTO system_config
			(key, type, value, create_time, update_time, disable)
			VALUES (?, 'string', ?, ?, ?, 0)`,
			KeyFamilyDirty, initial, now, now); err != nil {
			return fmt.Errorf("seed similarity_family_dirty: %w", err)
		}
	}

	// 2026-05-22 服务端三地址默认值：文件上传 / 服务端 / 归档上报都默认指向同一台。
	// 行为等价于用户在配置页面填写该地址并保存：
	//   - key 不存在 → INSERT
	//   - key 存在且 value 为空（NULL 或 ''）→ UPDATE 写入默认
	//   - key 存在且 value 非空 → 保持不动，不覆盖用户显式设置过的值
	const defaultServer = "http://47.95.233.47:19091"
	for _, key := range []string{KeyUploadServerURL, KeyManageEndpoint, KeyArchiveEndpoint} {
		now := time.Now()
		if _, err := db.Exec(`INSERT OR IGNORE INTO system_config (key, type, value, create_time, update_time, disable)
			VALUES (?, 'string', ?, ?, ?, 0)`,
			key, defaultServer, now, now); err != nil {
			return fmt.Errorf("seed default server config %s: %w", key, err)
		}
		if _, err := db.Exec(`UPDATE system_config
			SET value = ?, update_time = ?
			WHERE key = ? AND (value IS NULL OR value = '')`,
			defaultServer, now, key); err != nil {
			return fmt.Errorf("backfill default server config %s: %w", key, err)
		}
	}

	// 2026-05-22 老 IP 自动迁移：把 system_config 里所有含旧 IP 47.95.212.82 的
	// value 改写成 47.95.233.47。升级用户无感切换，不影响显式设置过其它地址的人。
	if _, err := db.Exec(`UPDATE system_config
		SET value = REPLACE(value, '47.95.212.82', '47.95.233.47'),
		    update_time = ?
		WHERE value LIKE '%47.95.212.82%' AND disable = 0`, time.Now()); err != nil {
		return fmt.Errorf("rewrite legacy server IP: %w", err)
	}

	// 2026-05-21 三级分流治理：相关索引
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_family_authoritative
		ON data_resource_family(authoritative_resource_id)`); err != nil {
		return fmt.Errorf("create idx_family_authoritative: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_ledger_memorandum_registered
		ON asset_ledgers(memorandum_registered_at)`); err != nil {
		return fmt.Errorf("create idx_ledger_memorandum_registered: %w", err)
	}

	if err := migrateProjectMembersUserUnique(db); err != nil {
		return fmt.Errorf("migrate project_members user unique: %w", err)
	}

	// V2：列加完之后，把 project_members.subject_id 反查映射到 user_id
	if err := migrateProjectMembersUserRef(db); err != nil {
		return fmt.Errorf("migrate project_members user_id: %w", err)
	}

	// V2-4：审计字段从 V1 TEXT 用户名反查映射到 user_id
	if err := migrateAuditUserRef(db); err != nil {
		return fmt.Errorf("migrate audit user_id: %w", err)
	}

	// V3-5 §11.2 audit_logs 模块级审计表
	auditDDL := `CREATE TABLE IF NOT EXISTS audit_logs (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		actor_id      TEXT NOT NULL,      -- 操作人标识（V1 username / V2 也可放 user.id 字符串）
		actor_user_id INTEGER,            -- V2：users.id（可空，旧数据可能没有）
		action        TEXT NOT NULL,      -- 见 §11.1：template_publish / project_close / member_change ...
		target_type   TEXT NOT NULL,      -- template / project / project_member / ledger / file_version / archive_export ...
		target_id     INTEGER,            -- 操作对象主键（按 target_type 决定指向哪张表）
		target_code   TEXT,               -- 操作对象业务编码（template_code / project_code 等）
		before_json   TEXT,               -- 变更前快照 JSON（可空）
		after_json    TEXT,               -- 变更后快照 JSON（可空）
		ip_address    TEXT,               -- 客户端 IP
		message       TEXT,               -- 自由文本说明
		create_time   DATETIME NOT NULL
	)`
	if _, err := db.Exec(auditDDL); err != nil {
		return fmt.Errorf("create audit_logs table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_logs(actor_id, create_time)`); err != nil {
		return fmt.Errorf("create audit_logs index actor: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_target ON audit_logs(target_type, target_id)`); err != nil {
		return fmt.Errorf("create audit_logs index target: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action, create_time)`); err != nil {
		return fmt.Errorf("create audit_logs index action: %w", err)
	}

	// V4-Q2 §4.2 个人文件项目化管理：自动建 3 个内置项目
	// 失败仅 log，不阻塞启动（用户后续通过立项向导"重新同步"模版后下次启动会再试）
	if err := ensurePersonalFilesContext(db); err != nil {
		fmt.Printf("[db] ensure personal files context: %v (will retry on next startup)\n", err)
	}

	// Spec B: 扫描期预计算文件特征
	if err := migrateDataDistributingFeatureCache(db); err != nil {
		return fmt.Errorf("migrate data_distributing feature cache: %w", err)
	}

	return nil
}

// columnExists returns true if the given column is present on the table.
func columnExists(db *sqlx.DB, table, column string) (bool, error) {
	rows, err := db.Queryx(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, nil
}

// loadSchema reads and executes the embedded SQL schema if tables don't exist
func loadSchema(db *sqlx.DB) error {
	// Check if tables already exist by querying a known table
	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='system_config'")
	if err == nil && count > 0 {
		// Tables already exist, no need to load schema
		return nil
	}

	// Execute embedded schema SQL
	statements := splitSQLStatements(schemaSQL)
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		_, err := db.Exec(stmt)
		if err != nil {
			return fmt.Errorf("failed to execute SQL statement: %w\nStatement: %s", err, stmt)
		}
	}

	return nil
}

// splitSQLStatements splits SQL content into individual statements
func splitSQLStatements(sqlContent string) []string {
	// Remove MySQL-style comments (-- comments)
	lines := strings.Split(sqlContent, "\n")
	var cleanLines []string
	for _, line := range lines {
		if idx := strings.Index(line, "--"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}
	cleanContent := strings.Join(cleanLines, "\n")

	// Split by semicolon
	var statements []string
	var current strings.Builder
	inString := false
	for _, ch := range cleanContent {
		if ch == '\'' {
			inString = !inString
		}
		if ch == ';' && !inString {
			statements = append(statements, current.String())
			current.Reset()
		} else {
			current.WriteRune(ch)
		}
	}
	// Add any remaining content
	if current.Len() > 0 {
		stmt := strings.TrimSpace(current.String())
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}
	return statements
}

// GetDB returns the database connection instance
func GetDB() *sqlx.DB {
	if db == nil {
		panic("database not initialized, call InitDB first")
	}
	return db
}

// TryGetDB returns the database connection instance, or nil if not yet initialized.
// Prefer GetDB() in production code where the DB is expected to be initialized.
// Use TryGetDB() in code that must not panic when called before DB init (e.g., feature flags).
func TryGetDB() *sqlx.DB {
	return db
}

// GetDBPath returns the current database path
func GetDBPath() string {
	return dbPath
}

// CloseDB closes the database connection
func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

// SetTestDB replaces the package-level DB connection for tests
//
// 仅在测试中使用：让 httpd 等 handler 调用 GetDB() 时拿到测试数据库连接。
// 调用方负责在测试结束时把它复位（一般用 t.Cleanup 即可）。
func SetTestDB(testDB *sqlx.DB) (restore func()) {
	prev := db
	db = testDB
	return func() { db = prev }
}

// LoadFromEnv loads database path from environment variable or default location
func LoadFromEnv() error {
	// Try environment variable first
	if path := os.Getenv("DATABASE_PATH"); path != "" {
		return InitDB(path)
	}

	// Try default locations
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}

	// Default path based on OS
	var defaultPath string
	switch runtime.GOOS {
	case "windows":
		defaultPath = filepath.Join(os.Getenv("APPDATA"), "data-asset-scan", "data.db")
	case "darwin":
		defaultPath = filepath.Join(home, ".local", "share", "data-asset-scan", "data.db")
	default:
		defaultPath = filepath.Join(home, ".local", "share", "data-asset-scan", "data.db")
	}

	return InitDB(defaultPath)
}
