package repository

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// migrateAuditUserRef 把 V1 审计字段（TEXT 用户名）反查映射到 V2 user_id。
//
// 覆盖三张表的四个字段：
//   - data_projects.created_by         → created_by_user_id
//   - file_versions.created_by         → created_by_user_id
//   - file_versions.submitted_by       → submitted_by_user_id
//   - lifecycle_events.operator_id     → operator_user_id
//
// 规则：
//   - 只回填 user_id IS NULL 的行（已有 user_id 的不动）
//   - 回填来源：users.username = <TEXT 字段值>（不区分大小写也行，但用精确匹配避免误伤）
//   - 找不到对应 user 的保持 NULL（脏数据由人工处理）
//   - 幂等：重复执行不会重复写入
//
// 为何这样做：
//   - V1 字段值是 "user_info.user_name" 或 currentOperator 字符串，可能"system"、空字符串、
//     已删除用户等；这些都映射不到 user，保留 NULL 而不是报错。
//   - V2 写入路径同时填两列，老数据靠这个迁移补 user_id。
//
// SetMaxOpenConns(1) 下不能在 rows.Next() 循环里再开 db.Get / db.Exec —— 故先把
// users 全量预载内存，再用 UPDATE...FROM 不可用（SQLite 不支持 UPDATE FROM 在所有版本），
// 改用相关子查询一次性 UPDATE。
func migrateAuditUserRef(db *sqlx.DB) error {
	// 各审计字段的 backfill SQL（idempotent，已有 user_id 的行跳过）
	stmts := []struct {
		Name string
		SQL  string
	}{
		{
			"data_projects.created_by_user_id",
			`UPDATE data_projects
			 SET created_by_user_id = (
			     SELECT id FROM users
			     WHERE users.username = data_projects.created_by
			       AND users.disable = 0
			     LIMIT 1
			 )
			 WHERE created_by_user_id IS NULL
			   AND created_by IS NOT NULL
			   AND created_by != ''`,
		},
		{
			"file_versions.created_by_user_id",
			`UPDATE file_versions
			 SET created_by_user_id = (
			     SELECT id FROM users
			     WHERE users.username = file_versions.created_by
			       AND users.disable = 0
			     LIMIT 1
			 )
			 WHERE created_by_user_id IS NULL
			   AND created_by IS NOT NULL
			   AND created_by != ''`,
		},
		{
			"file_versions.submitted_by_user_id",
			`UPDATE file_versions
			 SET submitted_by_user_id = (
			     SELECT id FROM users
			     WHERE users.username = file_versions.submitted_by
			       AND users.disable = 0
			     LIMIT 1
			 )
			 WHERE submitted_by_user_id IS NULL
			   AND submitted_by IS NOT NULL
			   AND submitted_by != ''`,
		},
		{
			"lifecycle_events.operator_user_id",
			`UPDATE lifecycle_events
			 SET operator_user_id = (
			     SELECT id FROM users
			     WHERE users.username = lifecycle_events.operator_id
			       AND users.disable = 0
			     LIMIT 1
			 )
			 WHERE operator_user_id IS NULL
			   AND operator_id IS NOT NULL
			   AND operator_id != ''`,
		},
	}

	for _, s := range stmts {
		if _, err := db.Exec(s.SQL); err != nil {
			return fmt.Errorf("backfill %s: %w", s.Name, err)
		}
	}
	return nil
}
