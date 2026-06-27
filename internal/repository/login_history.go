package repository

import (
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// LoginHistoryEntry 本机登录历史一条（含密码，仅存本机，供登录页快速登录自动填充）。
type LoginHistoryEntry struct {
	Username       string `db:"username" json:"username"`
	Password       string `db:"password" json:"password"`
	DisplayName    string `db:"display_name" json:"display_name"`
	UserUnit       string `db:"user_unit" json:"user_unit"`
	UserDepartment string `db:"user_department" json:"user_department"`
	ManageEndpoint string `db:"manage_endpoint" json:"manage_endpoint"`
	LastLoginAt    string `db:"last_login_at" json:"last_login_at"`
}

// LoginHistoryRepository 本机登录历史仓库。表在首次构造时按需创建（幂等），不改 InitDB 迁移顺序。
type LoginHistoryRepository struct {
	DB *sqlx.DB
}

func NewLoginHistoryRepository(db *sqlx.DB) *LoginHistoryRepository {
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS login_history (
		username        TEXT PRIMARY KEY,
		password        TEXT NOT NULL DEFAULT '',
		display_name    TEXT NOT NULL DEFAULT '',
		user_unit       TEXT NOT NULL DEFAULT '',
		user_department TEXT NOT NULL DEFAULT '',
		manage_endpoint TEXT NOT NULL DEFAULT '',
		last_login_at   TEXT NOT NULL DEFAULT ''
	)`)
	return &LoginHistoryRepository{DB: db}
}

// Upsert 按 username 去重保存/更新（密码、显示名、最近登录时间等）。LastLoginAt 为空时用当前时间。
func (r *LoginHistoryRepository) Upsert(e LoginHistoryEntry) error {
	if strings.TrimSpace(e.Username) == "" {
		return nil
	}
	if strings.TrimSpace(e.LastLoginAt) == "" {
		e.LastLoginAt = time.Now().Format("2006-01-02 15:04:05")
	}
	_, err := r.DB.Exec(`INSERT INTO login_history
		(username, password, display_name, user_unit, user_department, manage_endpoint, last_login_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			password = excluded.password,
			display_name = excluded.display_name,
			user_unit = excluded.user_unit,
			user_department = excluded.user_department,
			manage_endpoint = excluded.manage_endpoint,
			last_login_at = excluded.last_login_at`,
		e.Username, e.Password, e.DisplayName, e.UserUnit, e.UserDepartment, e.ManageEndpoint, e.LastLoginAt)
	return err
}

// List 按最近登录倒序列出登录历史。
func (r *LoginHistoryRepository) List() ([]LoginHistoryEntry, error) {
	var list []LoginHistoryEntry
	err := r.DB.Select(&list, `SELECT username, password, display_name, user_unit, user_department, manage_endpoint, last_login_at
		FROM login_history ORDER BY last_login_at DESC, username ASC`)
	return list, err
}

// Delete 删除某条登录历史（供用户移除不想保留的快速登录项）。
func (r *LoginHistoryRepository) Delete(username string) error {
	_, err := r.DB.Exec(`DELETE FROM login_history WHERE username = ?`, username)
	return err
}
