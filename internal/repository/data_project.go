package repository

import (
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// DataProjectRepository data_projects 表
type DataProjectRepository struct {
	DB *sqlx.DB
}

func NewDataProjectRepository(db *sqlx.DB) *DataProjectRepository {
	return &DataProjectRepository{DB: db}
}

// CreateDataProjectInput 立项创建入参
type CreateDataProjectInput struct {
	ProjectCode        string
	ProjectName        string
	ObjectShortCode    *string
	TemplateID         *int64
	TemplateCode       string
	TemplateVersion    string
	TaskSummary        *string
	ApprovalBasis      *string
	PlannedStartDate   *time.Time
	PlannedEndDate     *time.Time
	SensitivityLevel   string
	ManagementMode     string
	OwnerSubjectID     int64
	CustodianSubjectID int64
	SecuritySubjectID  int64
	Status             string // draft / active / ...
	ProjectRoot        *string
	CreatedBy          *string // V1：用户名字符串
	CreatedByUserID    *int64  // V2：users.id（与 CreatedBy 并存写入）
}

// Insert 插入项目（在事务内调用，传 sqlx.Ext）
func (r *DataProjectRepository) Insert(tx sqlx.Ext, in CreateDataProjectInput) (int64, error) {
	now := time.Now()
	res, err := tx.Exec(`INSERT INTO data_projects (
		project_code, project_name, object_short_code, template_id, template_code, template_version,
		task_summary, approval_basis, planned_start_date, planned_end_date,
		sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id,
		status, project_root, created_by, created_by_user_id, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		in.ProjectCode, in.ProjectName, in.ObjectShortCode, in.TemplateID, in.TemplateCode, in.TemplateVersion,
		in.TaskSummary, in.ApprovalBasis, in.PlannedStartDate, in.PlannedEndDate,
		in.SensitivityLevel, in.ManagementMode,
		in.OwnerSubjectID, in.CustodianSubjectID, in.SecuritySubjectID,
		in.Status, in.ProjectRoot, in.CreatedBy, in.CreatedByUserID, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindByID 按本地 id 查找
func (r *DataProjectRepository) FindByID(id int64) (*models.DataProject, error) {
	var p models.DataProject
	if err := r.DB.Get(&p, `SELECT * FROM data_projects WHERE id = ? AND disable = 0`, id); err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByCode 按业务编码查找
func (r *DataProjectRepository) FindByCode(code string) (*models.DataProject, error) {
	var p models.DataProject
	if err := r.DB.Get(&p, `SELECT * FROM data_projects WHERE project_code = ? AND disable = 0`, code); err != nil {
		return nil, err
	}
	return &p, nil
}

// List 查询项目
func (r *DataProjectRepository) List(status, keyword string) ([]models.DataProject, error) {
	q := `SELECT * FROM data_projects WHERE disable = 0`
	args := []interface{}{}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	if keyword != "" {
		q += ` AND (project_code LIKE ? OR project_name LIKE ?)`
		args = append(args, "%"+keyword+"%", "%"+keyword+"%")
	}
	q += ` ORDER BY update_time DESC`
	var list []models.DataProject
	if err := r.DB.Select(&list, q, args...); err != nil {
		return nil, err
	}
	return list, nil
}

// SetStatus 修改项目状态
func (r *DataProjectRepository) SetStatus(tx sqlx.Ext, id int64, status string) error {
	_, err := tx.Exec(`UPDATE data_projects SET status = ?, update_time = ? WHERE id = ?`, status, time.Now(), id)
	return err
}

// UpdateProjectRoot 更新项目根目录
func (r *DataProjectRepository) UpdateProjectRoot(tx sqlx.Ext, id int64, root string) error {
	_, err := tx.Exec(`UPDATE data_projects SET project_root = ?, update_time = ? WHERE id = ?`, root, time.Now(), id)
	return err
}

// UpdateSyncStatus 更新结项上报状态
func (r *DataProjectRepository) UpdateSyncStatus(id int64, status, message string, syncedAt *time.Time) error {
	_, err := r.DB.Exec(`UPDATE data_projects SET sync_status = ?, sync_message = ?, synced_at = ?, update_time = ? WHERE id = ?`,
		status, message, syncedAt, time.Now(), id)
	return err
}
