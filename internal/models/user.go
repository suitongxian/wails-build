package models

import "time"

// User 系统使用者（能登录、能操作、被审计署名的"人"）
//
// V2 引入。与 V1 的 user_info 表的区别：
//   - users 有独立的、稳定的 INTEGER 主键（用作 project_members.user_id、
//     created_by、operator_id 等审计字段的外键内容）
//   - username 字段 UNIQUE，作为登录标识
//   - 与 subjects（数据责任主体：归属/保管/安全）解耦——一个 user 可以
//     不出现在任何 subject 里（典型：临时工有账号但不是数据所有人）；
//     subjects 可以独立于 user 存在（典型：合规部作为 organization 主体）
//
// 与需求文档对应：
//   - 程序设计与开发需求说明书 §4.11 project_members.user_id 引用
//   - 设计文档 §3.5.2 project_owner 类型"字符串/用户ID"示例 `U-ZHANGSAN`
type User struct {
	ID          int64      `db:"id" json:"id"`
	Username    string     `db:"username" json:"username"`          // 登录用名（UNIQUE）
	DisplayName string     `db:"display_name" json:"display_name"`  // 显示名（"张三"）
	CompanyName string     `db:"company_name" json:"company_name"`
	Department  string     `db:"department" json:"department"`
	Role        string     `db:"role" json:"role"` // 账号角色（从 manage 同步，用于组队/分工展示）
	IP          string     `db:"ip" json:"ip"`
	MacAddress  string     `db:"mac_address" json:"mac_address"`
	WorkAddress *string    `db:"work_address" json:"work_address"`
	Phone       *string    `db:"phone" json:"phone"`
	Status      string     `db:"status" json:"status"`               // active / inactive / suspended
	CreateTime  time.Time  `db:"create_time" json:"create_time"`
	UpdateTime  time.Time  `db:"update_time" json:"update_time"`
	Disable     int        `db:"disable" json:"disable"`
}

// CreateUserInput 创建用户入参
type CreateUserInput struct {
	Username    string
	DisplayName string
	CompanyName string
	Department  string
	Phone       *string
	WorkAddress *string
}

// UpdateUserInput 更新用户入参
type UpdateUserInput struct {
	DisplayName *string
	CompanyName *string
	Department  *string
	Phone       *string
	WorkAddress *string
	Status      *string
}
