package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// 安全等级常量
const (
	SensGeneral    = "general"
	SensImportant  = "important"
	SensCoreSecret = "core_secret"
)

// 文件版本状态常量（基于需求文档 7.7 安全存储基线）
const (
	FileStatePersonalProcess = "personal_process" // 个人工作过程版本
	FileStatePersonalFinal   = "personal_final"   // 个人工作定稿版本
	FileStateDeptStage       = "dept_stage"       // 部门工作环节版本
	FileStateDeptFinal       = "dept_final"       // 部门项目定稿版本
	FileStateUnitRelease     = "unit_release"     // 单位最终发布版本
)

// 存储层级常量
const (
	StorageTierPersonalFolder    = "personal_folder"    // 个人文件夹
	StorageTierDepartmentCabinet = "department_cabinet" // 部门项目文件柜
	StorageTierUnitArchive       = "unit_archive"       // 单位文件室
	StorageTierSecureRoom        = "secure_room"        // 单位机要室
)

// SecurityPolicySeed 单条基线策略 seed 项
type SecurityPolicySeed struct {
	PolicyCode       string
	PolicyName       string
	SensitivityLevel string
	FileState        string
	StorageTier      string
	Permissions      []string
	ProtectionRules  map[string]interface{}
	AuditRequired    bool
}

// seedSecurityPolicies 写入安全策略基线（幂等）
//
// 依据《关于数据业务模版的设计.md》3.6 章 + 程序设计文档 7.7 节的"安全等级 × 文件版本状态"二维基线表。
// 三档安全等级（general / important / core_secret）× 五种文件状态 = 15 条基线策略。
func seedSecurityPolicies(db *sqlx.DB) error {
	// 基线表：每个等级在不同文件状态下的存储层级
	tier := func(level, state string) string {
		switch level {
		case SensCoreSecret:
			switch state {
			case FileStatePersonalProcess, FileStatePersonalFinal:
				return StorageTierPersonalFolder // 个人核心保密夹
			case FileStateDeptStage:
				return StorageTierDepartmentCabinet // 部门核心保密柜
			case FileStateDeptFinal:
				return StorageTierSecureRoom // 单位核心要件保密室
			case FileStateUnitRelease:
				return StorageTierSecureRoom // 单位核心要件保密室
			}
		case SensImportant:
			switch state {
			case FileStatePersonalProcess, FileStatePersonalFinal:
				return StorageTierPersonalFolder
			case FileStateDeptStage, FileStateDeptFinal:
				return StorageTierDepartmentCabinet
			case FileStateUnitRelease:
				return StorageTierUnitArchive
			}
		case SensGeneral:
			switch state {
			case FileStatePersonalProcess, FileStatePersonalFinal:
				return StorageTierPersonalFolder
			case FileStateDeptStage, FileStateDeptFinal:
				return StorageTierDepartmentCabinet
			case FileStateUnitRelease:
				return StorageTierUnitArchive
			}
		}
		return StorageTierPersonalFolder
	}

	// 默认权限集（按等级递进）
	permissions := func(level string) []string {
		switch level {
		case SensCoreSecret:
			// 核心级：禁止下载，限定数可信终端
			return []string{"read", "write", "submit", "archive"}
		case SensImportant:
			return []string{"read", "write", "receive", "upload", "submit", "archive", "share"}
		default:
			return []string{"read", "write", "receive", "upload", "submit", "archive", "share"}
		}
	}

	// 默认保护规则
	protection := func(level string) map[string]interface{} {
		switch level {
		case SensCoreSecret:
			return map[string]interface{}{
				"encrypt":           true,
				"watermark":         true,
				"no_local_download": true,
				"leak_check":        true,
				"trace_erase":       true,
			}
		case SensImportant:
			return map[string]interface{}{
				"encrypt":   true,
				"watermark": true,
			}
		default:
			return map[string]interface{}{}
		}
	}

	// 等级名称（中文，便于 UI 展示）
	levelLabel := map[string]string{
		SensCoreSecret: "核心(涉密)",
		SensImportant:  "重要",
		SensGeneral:    "一般",
	}
	stateLabel := map[string]string{
		FileStatePersonalProcess: "个人过程",
		FileStatePersonalFinal:   "个人定稿",
		FileStateDeptStage:       "部门环节",
		FileStateDeptFinal:       "部门定稿",
		FileStateUnitRelease:     "单位发布",
	}

	levels := []string{SensGeneral, SensImportant, SensCoreSecret}
	states := []string{FileStatePersonalProcess, FileStatePersonalFinal, FileStateDeptStage, FileStateDeptFinal, FileStateUnitRelease}

	now := time.Now()
	insertStmt := `INSERT INTO security_policies (
		policy_code, policy_name, sensitivity_level, file_state, storage_tier,
		permissions, protection_rules, audit_required, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	ON CONFLICT(policy_code) DO UPDATE SET
		policy_name = excluded.policy_name,
		storage_tier = excluded.storage_tier,
		permissions = excluded.permissions,
		protection_rules = excluded.protection_rules,
		audit_required = excluded.audit_required,
		update_time = excluded.update_time,
		disable = 0`

	for _, level := range levels {
		perms, _ := json.Marshal(permissions(level))
		prot, _ := json.Marshal(protection(level))
		for _, state := range states {
			code := fmt.Sprintf("SP-%s-%s", level, state)
			name := fmt.Sprintf("%s · %s", levelLabel[level], stateLabel[state])
			t := tier(level, state)

			audit := 1 // 默认审计
			if level == SensGeneral && state == FileStatePersonalProcess {
				audit = 0 // 个人过程数据可不强制审计
			}

			if _, err := db.Exec(insertStmt, code, name, level, state, t, string(perms), string(prot), audit, now, now); err != nil {
				return fmt.Errorf("seed security policy %s: %w", code, err)
			}
		}
	}

	return nil
}
