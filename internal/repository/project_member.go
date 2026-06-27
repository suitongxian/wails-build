package repository

import (
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// ProjectMemberRepository project_members 表
type ProjectMemberRepository struct {
	DB *sqlx.DB
}

func NewProjectMemberRepository(db *sqlx.DB) *ProjectMemberRepository {
	return &ProjectMemberRepository{DB: db}
}

// CreateProjectMemberInput 入参
//
// V2：优先填 UserID（与需求文档 §4.11 对齐）。
// SubjectID 字段过渡期保留，仅用于兼容 V1 测试和旧立项数据；新代码应填 UserID。
type CreateProjectMemberInput struct {
	ProjectID         int64
	UserID            *int64 // V2 规范字段
	SubjectID         int64  // V1 遗留过渡字段（V2 新代码可填 0）
	RoleCode          string
	StageIDs          *string // JSON 数组
	PermissionActions string  // JSON 数组
}

// Insert 在事务内插入成员
func (r *ProjectMemberRepository) Insert(tx sqlx.Ext, in CreateProjectMemberInput) (int64, error) {
	now := time.Now()
	res, err := tx.Exec(`INSERT INTO project_members (
		project_id, user_id, subject_id, role_code, stage_ids, permission_actions,
		create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		in.ProjectID, in.UserID, in.SubjectID, in.RoleCode, in.StageIDs, in.PermissionActions, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindByID 按 id 查找
func (r *ProjectMemberRepository) FindByID(id int64) (*models.ProjectMember, error) {
	var m models.ProjectMember
	if err := r.DB.Get(&m, `SELECT * FROM project_members WHERE id = ? AND disable = 0`, id); err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByProject 列出项目所有成员
func (r *ProjectMemberRepository) ListByProject(projectID int64) ([]models.ProjectMember, error) {
	var list []models.ProjectMember
	err := r.DB.Select(&list, `SELECT * FROM project_members WHERE project_id = ? AND disable = 0`, projectID)
	return list, err
}

// FindBySubjectInProject 查询某主体在项目中的成员记录（V1 遗留路径）
func (r *ProjectMemberRepository) FindBySubjectInProject(projectID, subjectID int64) ([]models.ProjectMember, error) {
	var list []models.ProjectMember
	err := r.DB.Select(&list, `SELECT * FROM project_members WHERE project_id = ? AND subject_id = ? AND disable = 0`, projectID, subjectID)
	return list, err
}

// FindByUserInProject V2 规范查询：按 user_id 找该用户在项目内的成员记录
//
// 与需求文档 §4.11 对齐。一个 user 在同一项目可能因为分阶段授权出现多条
// project_members 记录（不同 stage / role），所以返回 slice。
func (r *ProjectMemberRepository) FindByUserInProject(projectID, userID int64) ([]models.ProjectMember, error) {
	var list []models.ProjectMember
	err := r.DB.Select(&list, `SELECT * FROM project_members WHERE project_id = ? AND user_id = ? AND disable = 0`, projectID, userID)
	return list, err
}

// HasCloseAuthority 检查项目是否至少有一个成员有 close 权限
func (r *ProjectMemberRepository) HasCloseAuthority(projectID int64) (bool, error) {
	var n int
	err := r.DB.Get(&n, `SELECT COUNT(*) FROM project_members WHERE project_id = ? AND disable = 0 AND permission_actions LIKE '%"close"%'`, projectID)
	return n > 0, err
}
