package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// userColumns 显式列清单（避免 SELECT * + sqlx 严格模式失败）
const userColumns = `id, username, display_name, company_name, department, role,
	ip, mac_address, work_address, phone, status,
	create_time, update_time, disable`

// UserRepository users 表数据访问
type UserRepository struct {
	DB *sqlx.DB
}

func NewUserRepository(db *sqlx.DB) *UserRepository {
	return &UserRepository{DB: db}
}

// FindByID 按主键查找（找不到返回 nil, nil）
func (r *UserRepository) FindByID(id int64) (*models.User, error) {
	var u models.User
	err := r.DB.Get(&u, `SELECT `+userColumns+` FROM users WHERE id = ? AND disable = 0`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByUsername 按 username 查找（用于登录 / currentOperator）
func (r *UserRepository) FindByUsername(username string) (*models.User, error) {
	var u models.User
	err := r.DB.Get(&u, `SELECT `+userColumns+` FROM users WHERE username = ? AND disable = 0`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetActiveUser 取当前活跃用户（与 user_info.GetActiveUser 语义一致，
// 用于单用户终端模式下的 currentOperator）
func (r *UserRepository) GetActiveUser() (*models.User, error) {
	var u models.User
	err := r.DB.Get(&u, `SELECT `+userColumns+` FROM users WHERE disable = 0 ORDER BY id DESC LIMIT 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// List 列出所有活跃用户
func (r *UserRepository) List() ([]models.User, error) {
	var list []models.User
	err := r.DB.Select(&list, `SELECT `+userColumns+` FROM users WHERE disable = 0 ORDER BY id`)
	return list, err
}

// Create 新建用户
func (r *UserRepository) Create(in models.CreateUserInput) (*models.User, error) {
	if in.Username == "" {
		return nil, fmt.Errorf("username 必填")
	}
	if in.DisplayName == "" {
		return nil, fmt.Errorf("display_name 必填")
	}
	now := time.Now()
	res, err := r.DB.Exec(`INSERT INTO users
		(username, display_name, company_name, department, ip, mac_address,
		 work_address, phone, status, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, '', '', ?, ?, 'active', ?, ?, 0)`,
		in.Username, in.DisplayName, in.CompanyName, in.Department,
		in.WorkAddress, in.Phone, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.FindByID(id)
}

// Update 更新用户（按指针字段判断哪些要改）
func (r *UserRepository) Update(id int64, in models.UpdateUserInput) (*models.User, error) {
	cur, err := r.FindByID(id)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, fmt.Errorf("user not found: %d", id)
	}
	if in.DisplayName != nil {
		cur.DisplayName = *in.DisplayName
	}
	if in.CompanyName != nil {
		cur.CompanyName = *in.CompanyName
	}
	if in.Department != nil {
		cur.Department = *in.Department
	}
	if in.Phone != nil {
		cur.Phone = in.Phone
	}
	if in.WorkAddress != nil {
		cur.WorkAddress = in.WorkAddress
	}
	if in.Status != nil {
		cur.Status = *in.Status
	}
	_, err = r.DB.Exec(`UPDATE users SET
		display_name = ?, company_name = ?, department = ?,
		work_address = ?, phone = ?, status = ?, update_time = ?
		WHERE id = ?`,
		cur.DisplayName, cur.CompanyName, cur.Department,
		cur.WorkAddress, cur.Phone, cur.Status, time.Now(), id)
	if err != nil {
		return nil, err
	}
	return r.FindByID(id)
}

// SoftDelete 软删除
func (r *UserRepository) SoftDelete(id int64) error {
	_, err := r.DB.Exec(`UPDATE users SET disable = 1, update_time = ? WHERE id = ?`, time.Now(), id)
	return err
}

// UpsertFromUserInfo 把 user_info 行同步到 users 表。
//
// 用于 V2 闭环：用户在终端"修改机主信息"对话框里改名/换部门时，
// HTTP /user-info 处理器调用本方法，保证 users 表与 user_info 始终一致。
//
// 规则：
//   - 以 user_info.user_name 作为 users.username（UNIQUE）的匹配键
//   - 命中已有 users 行：更新 display_name / company_name / department /
//     ip / mac / work_address / phone（status / disable 不动）
//   - 不命中：插入新 users 行，status='active', disable=0
//
// 与 migrateUserInfoToUsers（一次性迁移）的区别：这是**写入时同步**，
// 让任何时间点的 user_info 编辑都能在 users 表里立刻可见。
func (r *UserRepository) UpsertFromUserInfo(ui *models.UserInfo) (*models.User, error) {
	if ui == nil || ui.UserName == "" {
		return nil, fmt.Errorf("user_info 为空或缺 user_name")
	}
	now := time.Now()
	existing, _ := r.FindByUsername(ui.UserName)
	if existing != nil {
		_, err := r.DB.Exec(`UPDATE users SET
			display_name = ?, company_name = ?, department = ?,
			ip = ?, mac_address = ?, work_address = ?, phone = ?, update_time = ?
			WHERE id = ?`,
			ui.UserName, ui.CompanyName, ui.Department,
			ui.IP, ui.MacAddress, ui.WorkAddress, ui.Phone, now, existing.ID)
		if err != nil {
			return nil, err
		}
		return r.FindByID(existing.ID)
	}
	res, err := r.DB.Exec(`INSERT INTO users (
		username, display_name, company_name, department, ip, mac_address,
		work_address, phone, status, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, 0)`,
		ui.UserName, ui.UserName, ui.CompanyName, ui.Department,
		ui.IP, ui.MacAddress, ui.WorkAddress, ui.Phone, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.FindByID(id)
}

func (r *UserRepository) UpsertManagedAuthUser(user ManagedAuthUser) (*models.User, error) {
	user.Username = strings.TrimSpace(user.Username)
	user.DisplayName = strings.TrimSpace(user.DisplayName)
	if user.Username == "" {
		return nil, fmt.Errorf("manage user 缺 username")
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	now := time.Now()
	ip := getLocalIP()
	mac := getLocalMAC()
	existing, err := r.FindByUsername(user.Username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		_, err := r.DB.Exec(`UPDATE users SET
			display_name = ?, company_name = ?, department = ?, role = ?,
			ip = ?, mac_address = ?, phone = ?, status = 'active', update_time = ?
			WHERE id = ? AND disable = 0`,
			user.DisplayName, user.UserUnit, user.UserDepartment, user.Role,
			ip, mac, user.Phone, now, existing.ID)
		if err != nil {
			return nil, err
		}
		return r.FindByID(existing.ID)
	}
	res, err := r.DB.Exec(`INSERT INTO users (
		username, display_name, company_name, department, role, ip, mac_address,
		work_address, phone, status, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, 'active', ?, ?, 0)`,
		user.Username, user.DisplayName, user.UserUnit, user.UserDepartment, user.Role,
		ip, mac, user.Phone, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.FindByID(id)
}

// migrateFromUserInfo V1 → V2 一次性迁移：把 user_info 现有数据复制到 users
//
// 调用时机：runMigrations 里在 users 表 CREATE 之后立即跑。
// 幂等：通过 username 唯一约束防止重复插入；user_info 现有记录可重复扫描，
// 已经存在对应 username 的 users 行不再插。
//
// 字段对应：
//
//	user_info.user_name        → users.username + users.display_name（同值）
//	user_info.company_name     → users.company_name
//	user_info.department       → users.department
//	user_info.ip / mac_address → 同名复制
//	user_info.work_address     → 同
//	user_info.phone            → 同
//	user_info.disable          → 同
func migrateUserInfoToUsers(db *sqlx.DB) error {
	// 看 user_info 表存在吗（fresh 数据库可能还没建）
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='user_info'`); err != nil {
		return err
	}
	if n == 0 {
		return nil // user_info 不存在，跳过
	}

	// 一次性把 user_info 全部行收到内存（避免 SetMaxOpenConns(1) 下迭代
	// rows 同时 db.Get 检查 users 表导致死锁）
	type row struct {
		UserName, CompanyName, Department, IP, MacAddress string
		WorkAddress, Phone                                sql.NullString
		CreateTime, UpdateTime                            time.Time
		Disable                                           int
	}
	var rowsBuf []row
	rows, err := db.Queryx(`SELECT user_name, company_name, department, ip, mac_address,
		work_address, phone, create_time, update_time, disable
		FROM user_info WHERE disable = 0`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.UserName, &r.CompanyName, &r.Department, &r.IP, &r.MacAddress,
			&r.WorkAddress, &r.Phone, &r.CreateTime, &r.UpdateTime, &r.Disable); err != nil {
			rows.Close()
			return err
		}
		if r.UserName != "" {
			rowsBuf = append(rowsBuf, r)
		}
	}
	rows.Close()

	// 拿到已存在的 users.username 集合（一次查询，避免循环里查）
	var existing []string
	if err := db.Select(&existing, `SELECT username FROM users`); err != nil {
		return err
	}
	exists := make(map[string]bool, len(existing))
	for _, u := range existing {
		exists[u] = true
	}

	for _, r := range rowsBuf {
		if exists[r.UserName] {
			continue
		}
		var wa, ph interface{}
		if r.WorkAddress.Valid {
			wa = r.WorkAddress.String
		}
		if r.Phone.Valid {
			ph = r.Phone.String
		}
		if _, err := db.Exec(`INSERT INTO users
			(username, display_name, company_name, department, ip, mac_address,
			 work_address, phone, status, create_time, update_time, disable)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
			r.UserName, r.UserName, r.CompanyName, r.Department, r.IP, r.MacAddress,
			wa, ph, r.CreateTime, r.UpdateTime, r.Disable); err != nil {
			return fmt.Errorf("migrate user_info[%s] failed: %w", r.UserName, err)
		}
		exists[r.UserName] = true
	}
	return nil
}
