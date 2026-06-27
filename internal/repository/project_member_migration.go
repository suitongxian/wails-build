package repository

import (
	"github.com/jmoiron/sqlx"
)

// migrateProjectMembersUserRef V1 → V2 一次性回填 project_members.user_id
//
// 策略：对每一条 project_members 记录（user_id 为 NULL）：
//  1. 拿 subject_id → 查 subjects.name 和 type
//  2. 如果 type='person'，按 subjects.name === users.display_name
//     或 subjects.name === users.username 反查
//  3. 找到唯一 user → 回填 user_id
//  4. 找不到（subject 不是 person / 没对应 user）→ 保留 user_id=NULL
//     并 disable=1 标记这条 member 失效（V1 数据偏离需求，不能映射）
//
// 调用时机：migration 链尾部。
// 幂等：只处理 user_id IS NULL AND disable=0 的记录。
//
// 注意：SetMaxOpenConns(1) 安全 — 先把行收齐到内存再逐条 UPDATE，
// 不在 rows.Next 循环内同时跑 db.Get。
func migrateProjectMembersUserRef(db *sqlx.DB) error {
	// 该表存在吗？fresh 数据库可能还没建（runTemplateProjectMigrations 之前）
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='project_members'`); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}

	// 先收待迁移的 (member_id, subject_id) 集合
	type pendingRow struct {
		ID        int64
		SubjectID int64
	}
	var pending []pendingRow
	rows, err := db.Queryx(`SELECT id, subject_id FROM project_members
		WHERE user_id IS NULL AND disable = 0 AND subject_id > 0`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p pendingRow
		if err := rows.Scan(&p.ID, &p.SubjectID); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, p)
	}
	rows.Close()

	if len(pending) == 0 {
		return nil
	}

	// 把 subjects 索引一下（id → name / type）
	type subjInfo struct {
		Name string
		Type string
	}
	subjIdx := map[int64]subjInfo{}
	subjRows, err := db.Queryx(`SELECT id, name, type FROM subjects WHERE disable = 0`)
	if err != nil {
		return err
	}
	for subjRows.Next() {
		var id int64
		var name, typ string
		if err := subjRows.Scan(&id, &name, &typ); err != nil {
			subjRows.Close()
			return err
		}
		subjIdx[id] = subjInfo{Name: name, Type: typ}
	}
	subjRows.Close()

	// 把 users 索引一下（display_name → id，username → id）
	userByDisplay := map[string]int64{}
	userByUsername := map[string]int64{}
	uRows, err := db.Queryx(`SELECT id, username, display_name FROM users WHERE disable = 0`)
	if err != nil {
		return err
	}
	for uRows.Next() {
		var id int64
		var username, displayName string
		if err := uRows.Scan(&id, &username, &displayName); err != nil {
			uRows.Close()
			return err
		}
		userByUsername[username] = id
		if displayName != "" {
			userByDisplay[displayName] = id
		}
	}
	uRows.Close()

	// 逐条尝试反查并 UPDATE
	for _, p := range pending {
		subj, ok := subjIdx[p.SubjectID]
		if !ok {
			continue
		}
		var userID int64
		if subj.Type == "person" {
			if id, found := userByDisplay[subj.Name]; found {
				userID = id
			} else if id, found := userByUsername[subj.Name]; found {
				userID = id
			}
		}
		if userID > 0 {
			if _, err := db.Exec(`UPDATE project_members SET user_id = ?, update_time = CURRENT_TIMESTAMP WHERE id = ?`, userID, p.ID); err != nil {
				return err
			}
		}
		// 找不到对应 user 的：不动 user_id（保持 NULL）。
		// 不主动 disable，避免破坏 V1 已有项目；新代码用 FindByUserInProject 自然会跳过 NULL。
	}
	return nil
}
