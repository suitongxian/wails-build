package models

import "time"

// UserInfo represents a user record in the user_info table
type UserInfo struct {
	ID          int64      `db:"id" json:"id"`
	CompanyName string     `db:"company_name" json:"company_name"`
	UserName    string     `db:"user_name" json:"user_name"`
	Department  string     `db:"department" json:"department"`
	IP          string     `db:"ip" json:"ip"`
	MacAddress  string     `db:"mac_address" json:"mac_address"`
	WorkAddress *string    `db:"work_address" json:"work_address"`
	Phone       *string    `db:"phone" json:"phone"`
	PasswordMD5 *string    `db:"password_md5" json:"password_md5"`
	IDCard      *string    `db:"id_card" json:"id_card"`
	CreateTime  time.Time  `db:"create_time" json:"create_time"`
	UpdateTime  time.Time  `db:"update_time" json:"update_time"`
	Disable     int        `db:"disable" json:"disable"`
}

// CreateUserInfoParams represents parameters for creating a new user
type CreateUserInfoParams struct {
	CompanyName string  `db:"company_name"`
	UserName    string  `db:"user_name"`
	Department  string  `db:"department"`
	Phone       *string `db:"phone"`
	WorkAddress *string `db:"work_address"`
}

// UpdateUserInfoParams represents parameters for updating user information
type UpdateUserInfoParams struct {
	CompanyName *string `db:"company_name"`
	UserName    *string `db:"user_name"`
	Department  *string `db:"department"`
	Phone       *string `db:"phone"`
	WorkAddress *string `db:"work_address"`
}