package repository

import (
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// FileVersionRepository file_versions 表
type FileVersionRepository struct {
	DB *sqlx.DB
}

func NewFileVersionRepository(db *sqlx.DB) *FileVersionRepository {
	return &FileVersionRepository{DB: db}
}

// CreateFileVersionInput 入参
type CreateFileVersionInput struct {
	ProjectID           int64
	ProjectStageID      int64
	TemplateFileRuleID  *int64
	FileVersionCode     string
	LocalCode           string
	DisplayName         string
	DataState           string // input / process / output
	VersionNo           string
	Required            int
	FileType            *string
	StorageURI          *string
	Checksum            *string
	FileSize            *int64
	SourceFileVersionID *int64
	SecurityPolicyID    *int64
	LifecycleStatus     string  // planned / registered / ...
	CreatedBy           *string // V1：用户名字符串
	CreatedByUserID     *int64  // V2：users.id（与 CreatedBy 并存写入）
}

// Insert 在事务内插入文件版本
func (r *FileVersionRepository) Insert(tx sqlx.Ext, in CreateFileVersionInput) (int64, error) {
	now := time.Now()
	res, err := tx.Exec(`INSERT INTO file_versions (
		project_id, project_stage_id, template_file_rule_id, file_version_code, local_code,
		display_name, data_state, version_no, required, file_type, storage_uri, checksum, file_size,
		source_file_version_id, security_policy_id, lifecycle_status, created_by, created_by_user_id,
		create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		in.ProjectID, in.ProjectStageID, in.TemplateFileRuleID, in.FileVersionCode, in.LocalCode,
		in.DisplayName, in.DataState, in.VersionNo, in.Required, in.FileType, in.StorageURI, in.Checksum, in.FileSize,
		in.SourceFileVersionID, in.SecurityPolicyID, in.LifecycleStatus, in.CreatedBy, in.CreatedByUserID, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindByID 按本地 id 查找
func (r *FileVersionRepository) FindByID(id int64) (*models.FileVersion, error) {
	var f models.FileVersion
	if err := r.DB.Get(&f, `SELECT * FROM file_versions WHERE id = ? AND disable = 0`, id); err != nil {
		return nil, err
	}
	return &f, nil
}

// ListByProject 列出项目下所有文件版本
func (r *FileVersionRepository) ListByProject(projectID int64) ([]models.FileVersion, error) {
	var list []models.FileVersion
	err := r.DB.Select(&list, `SELECT * FROM file_versions WHERE project_id = ? AND disable = 0 ORDER BY project_stage_id, data_state, local_code, version_no DESC`, projectID)
	return list, err
}

// ListByStage 列出环节下所有文件版本
func (r *FileVersionRepository) ListByStage(stageID int64) ([]models.FileVersion, error) {
	var list []models.FileVersion
	err := r.DB.Select(&list, `SELECT * FROM file_versions WHERE project_stage_id = ? AND disable = 0 ORDER BY data_state, local_code, version_no DESC`, stageID)
	return list, err
}

// CountByStageRule 计算同一规则下已存在的版本数（用于判断是否需要新版本号）
func (r *FileVersionRepository) CountByStageRule(stageID int64, localCode string) (int, error) {
	var n int
	err := r.DB.Get(&n, `SELECT COUNT(*) FROM file_versions WHERE project_stage_id = ? AND local_code = ? AND disable = 0`, stageID, localCode)
	return n, err
}

// UpdateBinding 更新文件实体绑定（上传后调用）
func (r *FileVersionRepository) UpdateBinding(id int64, fileType, storageURI, checksum string, fileSize int64) error {
	_, err := r.DB.Exec(`UPDATE file_versions SET file_type = ?, storage_uri = ?, checksum = ?, file_size = ?, lifecycle_status = 'registered', update_time = ? WHERE id = ?`,
		fileType, storageURI, checksum, fileSize, time.Now(), id)
	return err
}

// UpdateLifecycleStatus 更新生命周期状态
func (r *FileVersionRepository) UpdateLifecycleStatus(tx sqlx.Ext, id int64, status string) error {
	_, err := tx.Exec(`UPDATE file_versions SET lifecycle_status = ?, update_time = ? WHERE id = ?`, status, time.Now(), id)
	return err
}
