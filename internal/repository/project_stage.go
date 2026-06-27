package repository

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// ProjectStageRepository project_stages 表
type ProjectStageRepository struct {
	DB *sqlx.DB
}

func NewProjectStageRepository(db *sqlx.DB) *ProjectStageRepository {
	return &ProjectStageRepository{DB: db}
}

// CreateProjectStageInput 入参
type CreateProjectStageInput struct {
	ProjectID         int64
	TemplateStageID   *int64
	StageCode         string
	StageName         string
	StageType         string
	SortOrder         int
	Status            string
	AssignedRoleCodes *string
	DirectoryPath     *string
}

// Insert 在事务内插入工作环节
func (r *ProjectStageRepository) Insert(tx sqlx.Ext, in CreateProjectStageInput) (int64, error) {
	now := time.Now()
	res, err := tx.Exec(`INSERT INTO project_stages (
		project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order,
		status, assigned_role_codes, directory_path, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		in.ProjectID, in.TemplateStageID, in.StageCode, in.StageName, in.StageType, in.SortOrder,
		in.Status, in.AssignedRoleCodes, in.DirectoryPath, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindByID 按本地 id 查找
func (r *ProjectStageRepository) FindByID(id int64) (*models.ProjectStage, error) {
	var s models.ProjectStage
	if err := r.DB.Get(&s, `SELECT * FROM project_stages WHERE id = ? AND disable = 0`, id); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListByProject 按项目查询所有环节（按 sort_order 排序）
func (r *ProjectStageRepository) ListByProject(projectID int64) ([]models.ProjectStage, error) {
	var list []models.ProjectStage
	err := r.DB.Select(&list, `SELECT * FROM project_stages WHERE project_id = ? AND disable = 0 ORDER BY sort_order`, projectID)
	return list, err
}

// FindByProjectAndCode 按项目+环节编码查找
func (r *ProjectStageRepository) FindByProjectAndCode(projectID int64, stageCode string) (*models.ProjectStage, error) {
	var s models.ProjectStage
	err := r.DB.Get(&s, `SELECT * FROM project_stages WHERE project_id = ? AND stage_code = ? AND disable = 0`,
		projectID, stageCode)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SetStatus 改变环节状态（内部使用，不做状态机校验）
//
// V3-3 起对外路径请走 UpdateStageStatus（带状态机校验）。
func (r *ProjectStageRepository) SetStatus(id int64, status string) error {
	_, err := r.DB.Exec(`UPDATE project_stages SET status = ?, update_time = ? WHERE id = ?`, status, time.Now(), id)
	return err
}

// ValidStageStatusTransition V3-3 §7.3 + §5.2 环节状态机
//
// 文档 §7.3："调整环节状态 pending、running、completed、skipped"
// 状态机（V3-UI option C：completed 硬终态，skipped 软终态可撤销）：
//
//	pending    -> running / skipped
//	running    -> completed / skipped
//	completed  -> (终态，不可逆)
//	skipped    -> pending (允许撤销跳过，重新开工)
//
// 与 ledger 状态机（ValidStateTransition）独立维护：那个是文件版本/底账
// 生命周期，这个是项目环节工作进度。
func ValidStageStatusTransition(from, to string) bool {
	allowed := map[string][]string{
		"pending":   {"running", "skipped"},
		"running":   {"completed", "skipped"},
		"completed": {},          // 硬终态：完成后不可改
		"skipped":   {"pending"}, // 软终态：可撤销跳过
	}
	for _, t := range allowed[from] {
		if t == to {
			return true
		}
	}
	return false
}

// IsStageMutable V3-UI option C：判断环节当前是否允许文件操作
//
// pending / running 允许；completed / skipped 拒绝。
// 文件操作（上传/派生/新版本/提交/领取）调用前应校验。
func IsStageMutable(stageStatus string) bool {
	return stageStatus == "pending" || stageStatus == "running"
}

// UpdateStageStatus V3-3 受状态机守护的环节状态切换
//
// 入参：stageID + 目标 status + 可选 reason（用于审计）
// 校验：
//   - stage 必须存在且未禁用
//   - 转换必须在 ValidStageStatusTransition 允许列表里
//   - 项目状态必须可变（archived/cancelled 项目不允许）
func (r *ProjectStageRepository) UpdateStageStatus(stageID int64, toStatus string) error {
	stage, err := r.FindByID(stageID)
	if err != nil {
		return err
	}
	if !ValidStageStatusTransition(stage.Status, toStatus) {
		return fmt.Errorf("环节状态不允许从 %s 转换到 %s", stage.Status, toStatus)
	}
	return r.SetStatus(stageID, toStatus)
}

// UpdateDirectoryPath 更新目录路径
func (r *ProjectStageRepository) UpdateDirectoryPath(tx sqlx.Ext, id int64, path string) error {
	_, err := tx.Exec(`UPDATE project_stages SET directory_path = ?, update_time = ? WHERE id = ?`, path, time.Now(), id)
	return err
}
