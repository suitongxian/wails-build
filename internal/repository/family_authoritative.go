package repository

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// SetAuthoritativeResource 设置或更换 family 的权威源。
// 不触碰任何 ledger 行——「权威 / 参考」是查询时由 join 推导。
func SetAuthoritativeResource(db *sqlx.DB, familyID, resourceID int64) error {
	_, err := db.Exec(`UPDATE data_resource_family
		SET authoritative_resource_id = ?, update_time = CURRENT_TIMESTAMP
		WHERE family_id = ?`, resourceID, familyID)
	return err
}

// NeedsAuthoritativeArbitration 判断给定 resource 在 apply 重要级时是否需要先选权威源。
// 触发条件：family_id 非空 + family.member_count >= 2 + authoritative_resource_id IS NULL。
func NeedsAuthoritativeArbitration(db *sqlx.DB, resourceID int64) (bool, error) {
	var row struct {
		FamilyID    sql.NullInt64 `db:"family_id"`
		MemberCount int           `db:"member_count"`
		AuthID      sql.NullInt64 `db:"authoritative_resource_id"`
	}
	err := db.Get(&row, `SELECT dr.family_id,
	                            COALESCE(f.member_count, 0) AS member_count,
	                            f.authoritative_resource_id
	                       FROM data_resources dr
	                  LEFT JOIN data_resource_family f ON f.family_id = dr.family_id
	                      WHERE dr.data_resources_id = ? AND dr.disable = 0`, resourceID)
	if err != nil {
		return false, err
	}
	if !row.FamilyID.Valid {
		return false, nil
	}
	if row.MemberCount < 2 {
		return false, nil
	}
	if row.AuthID.Valid {
		return false, nil
	}
	return true, nil
}

// FamilyRole 在查询时推导成员在 family 的角色
type FamilyRole string

const (
	FamilyRoleStandalone    FamilyRole = "standalone"
	FamilyRoleAuthoritative FamilyRole = "authoritative"
	FamilyRolePending       FamilyRole = "pending_arbitration"
	FamilyRoleReference     FamilyRole = "reference"
)

// ResolveFamilyRole 在已知 resource 与其 family 的当前状态下计算角色
func ResolveFamilyRole(familyID sql.NullInt64, resourceID int64, authoritativeID sql.NullInt64) FamilyRole {
	if !familyID.Valid {
		return FamilyRoleStandalone
	}
	if authoritativeID.Valid && authoritativeID.Int64 == resourceID {
		return FamilyRoleAuthoritative
	}
	if !authoritativeID.Valid {
		return FamilyRolePending
	}
	return FamilyRoleReference
}
