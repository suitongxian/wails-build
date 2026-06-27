package repository

import (
	"strings"

	"github.com/jmoiron/sqlx"
)

func migrateProjectMembersUserUnique(db *sqlx.DB) error {
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='project_members'`); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}

	var createSQL string
	if err := db.Get(&createSQL, `SELECT sql FROM sqlite_master WHERE type='table' AND name='project_members'`); err != nil {
		return err
	}
	needsRebuild := strings.Contains(createSQL, "UNIQUE(project_id, subject_id, role_code)") ||
		strings.Contains(createSQL, "UNIQUE (project_id, subject_id, role_code)")
	if needsRebuild {
		tx, err := db.Beginx()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		stmts := []string{
			`CREATE TABLE project_members_new (
				id                       INTEGER PRIMARY KEY AUTOINCREMENT,
				project_id               INTEGER NOT NULL,
				user_id                  INTEGER,
				subject_id               INTEGER NOT NULL DEFAULT 0,
				role_code                TEXT NOT NULL,
				stage_ids                TEXT,
				permission_actions       TEXT NOT NULL,
				create_time              DATETIME NOT NULL,
				update_time              DATETIME NOT NULL,
				disable                  INTEGER NOT NULL DEFAULT 0
			)`,
			`INSERT INTO project_members_new (
				id, project_id, user_id, subject_id, role_code, stage_ids,
				permission_actions, create_time, update_time, disable
			)
			SELECT id, project_id, user_id, subject_id, role_code, stage_ids,
				permission_actions, create_time, update_time, disable
			FROM project_members`,
			`DROP TABLE project_members`,
			`ALTER TABLE project_members_new RENAME TO project_members`,
		}
		for _, stmt := range stmts {
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_project_members_project ON project_members(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_subject ON project_members(subject_id)`,
		`CREATE INDEX IF NOT EXISTS idx_project_members_user ON project_members(user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_project_members_user_role ON project_members(project_id, user_id, role_code) WHERE user_id IS NOT NULL AND disable = 0`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_project_members_subject_role ON project_members(project_id, subject_id, role_code) WHERE user_id IS NULL AND disable = 0`,
	}
	for _, stmt := range indexes {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
