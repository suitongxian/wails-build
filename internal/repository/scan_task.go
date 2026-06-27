package repository

import (
	"time"

	"data-asset-scan-go/internal/models"
	"github.com/jmoiron/sqlx"
)

// ScanTaskRepository handles database operations for scan_task table
type ScanTaskRepository struct {
	DB *sqlx.DB
}

// NewScanTaskRepository creates a new ScanTaskRepository instance
func NewScanTaskRepository(db *sqlx.DB) *ScanTaskRepository {
	return &ScanTaskRepository{DB: db}
}

// Create creates a new scan task record
func (r *ScanTaskRepository) Create(params models.CreateScanTaskParams) (int64, error) {
	now := time.Now()
	query := `
		INSERT INTO scan_task (
			scan_type, file_scan_range, heartbeat, workspace_path, task_state,
			task_phase, task_error_message, scan_args, file_total, file_scanned_count,
			create_time, update_time, disable
		) VALUES (?, ?, 0, ?, 'run', 'initializing', '', ?, ?, 0, ?, ?, 0)
	`
	result, err := r.DB.Exec(query,
		params.ScanType,
		params.FileScanRange,
		params.WorkspacePath,
		params.ScanArgs,
		params.FileTotal,
		now,
		now,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateProgress updates the scan progress
func (r *ScanTaskRepository) UpdateProgress(taskId int64, params models.UpdateProgressParams) error {
	now := time.Now()
	query := `
		UPDATE scan_task
		SET heartbeat = ?, file_scanned_count = ?, task_phase = ?, update_time = ?
		WHERE id = ?
	`
	_, err := r.DB.Exec(query, params.Heartbeat, params.FileScannedCount, params.TaskPhase, now, taskId)
	return err
}

// MarkSucceeded marks a task as successfully completed and, on the very
// first successful scan task in this terminal's lifetime, closes the
// historical baseline window so subsequent inserts are tagged 'new'.
//
// 关窗用条件式 UPDATE：只有 baseline_completed_at 仍为 NULL/空 时才写值，
// 天然抗并发，多次 succeed 也只有第一次有效。
func (r *ScanTaskRepository) MarkSucceeded(taskId int64) error {
	now := time.Now()
	if _, err := r.DB.Exec(`
		UPDATE scan_task
		SET task_state = 'succeed', task_error_message = '', end_time = ?, update_time = ?
		WHERE id = ?
	`, now, now, taskId); err != nil {
		return err
	}

	if _, err := r.DB.Exec(`
		UPDATE system_config
		SET value = ?, update_time = ?
		WHERE key = 'baseline_completed_at'
		  AND disable = 0
		  AND (value IS NULL OR value = '')
	`, now.Format(time.RFC3339), now); err != nil {
		return err
	}
	return nil
}

// MarkFailed marks a task as failed
func (r *ScanTaskRepository) MarkFailed(taskId int64, errorMessage string) error {
	now := time.Now()
	query := `
		UPDATE scan_task
		SET task_state = 'fail', task_error_message = ?, end_time = ?, update_time = ?
		WHERE id = ?
	`
	_, err := r.DB.Exec(query, errorMessage, now, now, taskId)
	return err
}

// UpdateFileTotal updates the total file count
func (r *ScanTaskRepository) UpdateFileTotal(taskId int64, fileTotal int) error {
	now := time.Now()
	query := `UPDATE scan_task SET file_total = ?, update_time = ? WHERE id = ?`
	_, err := r.DB.Exec(query, fileTotal, now, taskId)
	return err
}

// GetMaxTaskID returns the largest scan_task.id, or 0 if the table is empty.
// Used by the start-scan handler to detect that a goroutine has inserted its
// task row even if the row already transitioned to a terminal state by the
// time we look (fast-failing scans).
func (r *ScanTaskRepository) GetMaxTaskID() (int64, error) {
	var id int64
	err := r.DB.Get(&id, `SELECT COALESCE(MAX(id), 0) FROM scan_task`)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// GetRunningTask returns the most recent task with task_state='run'.
// Returns (nil, nil) if no running task exists.
func (r *ScanTaskRepository) GetRunningTask() (*models.ScanTask, error) {
	var task models.ScanTask
	query := `
		SELECT * FROM scan_task
		WHERE task_state = 'run' AND disable = 0
		ORDER BY create_time DESC
		LIMIT 1
	`
	err := r.DB.Get(&task, query)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

// MarkOrphanRunsAsFailed marks any task still in 'run' state as failed.
// Should be called once at process startup so a previous crash does not leave
// stale rows that block new scans.
func (r *ScanTaskRepository) MarkOrphanRunsAsFailed() (int64, error) {
	now := time.Now()
	query := `
		UPDATE scan_task
		SET task_state = 'fail',
		    task_error_message = '进程异常退出',
		    end_time = ?,
		    update_time = ?
		WHERE task_state = 'run'
	`
	result, err := r.DB.Exec(query, now, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetByID retrieves a task by ID
func (r *ScanTaskRepository) GetByID(taskId int64) (*models.ScanTask, error) {
	var task models.ScanTask
	query := `SELECT * FROM scan_task WHERE id = ? AND disable = 0`
	err := r.DB.Get(&task, query, taskId)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

// UpdateWorkspaceInfo updates workspace information
func (r *ScanTaskRepository) UpdateWorkspaceInfo(taskId int64, params models.UpdateWorkspaceParams) error {
	now := time.Now()

	// Build dynamic update query
	setClauses := []string{"update_time = ?"}
	values := []interface{}{now}

	if params.WorkspacePath != nil {
		setClauses = append(setClauses, "workspace_path = ?")
		values = append(values, *params.WorkspacePath)
	}
	if params.FileAllSuffixCount != nil {
		setClauses = append(setClauses, "file_all_suffix_count = ?")
		values = append(values, *params.FileAllSuffixCount)
	}
	if params.FileCountSuffixCount != nil {
		setClauses = append(setClauses, "file_count_suffix_count = ?")
		values = append(values, *params.FileCountSuffixCount)
	}
	if params.WorkspaceCount != nil {
		setClauses = append(setClauses, "workspace_count = ?")
		values = append(values, *params.WorkspaceCount)
	}

	values = append(values, taskId)

	query := `UPDATE scan_task SET ` + joinStrings(setClauses, ", ") + ` WHERE id = ?`
	_, err := r.DB.Exec(query, values...)
	return err
}

// GetLastSuccessfulTask retrieves the last successful task
func (r *ScanTaskRepository) GetLastSuccessfulTask() (*models.ScanTask, error) {
	var task models.ScanTask
	query := `
		SELECT * FROM scan_task
		WHERE task_state = 'succeed' AND disable = 0
		ORDER BY create_time DESC
		LIMIT 1
	`
	err := r.DB.Get(&task, query)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

// GetPreviousSuccessfulTask retrieves the previous successful task before the given task
func (r *ScanTaskRepository) GetPreviousSuccessfulTask(taskId int64) (*models.ScanTask, error) {
	currentTask, err := r.GetByID(taskId)
	if err != nil || currentTask == nil {
		return nil, err
	}

	var task models.ScanTask
	query := `
		SELECT * FROM scan_task
		WHERE task_state = 'succeed' AND disable = 0 AND create_time < ?
		ORDER BY create_time DESC
		LIMIT 1
	`
	err = r.DB.Get(&task, query, currentTask.CreateTime)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

// calculateParamsChanged calculates parameter changes between two tasks
func calculateParamsChanged(current *models.ScanTask, previous *models.ScanTask) models.ParamsChanged {
	if previous == nil {
		return models.ParamsChanged{
			WorkspacePathChanged: false,
			ScanAreaPathChanged:  false,
			ControlTypeChanged:   false,
		}
	}
	return models.ParamsChanged{
		WorkspacePathChanged: current.WorkspacePath != nil && previous.WorkspacePath != nil && *current.WorkspacePath != *previous.WorkspacePath,
		ScanAreaPathChanged:  current.FileScanRange != nil && previous.FileScanRange != nil && *current.FileScanRange != *previous.FileScanRange,
		ControlTypeChanged:   current.FileAllSuffixText != nil && previous.FileAllSuffixText != nil && *current.FileAllSuffixText != *previous.FileAllSuffixText,
	}
}

// GetTasksWithPagination retrieves tasks with pagination
func (r *ScanTaskRepository) GetTasksWithPagination(params models.ScanTaskQueryParams) (*models.ScanTaskPageResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// Get total count
	var total int64
	countQuery := `SELECT COUNT(*) as count FROM scan_task WHERE disable = 0`
	err := r.DB.Get(&total, countQuery)
	if err != nil {
		return nil, err
	}

	// Get tasks
	query := `
		SELECT * FROM scan_task
		WHERE disable = 0
		ORDER BY create_time DESC
		LIMIT ? OFFSET ?
	`
	var tasks []models.ScanTask
	err = r.DB.Select(&tasks, query, pageSize, offset)
	if err != nil {
		return nil, err
	}

	// Add params changed info
	tasksWithParamsChanged := make([]models.ScanTaskWithParamsChanged, len(tasks))
	for i, task := range tasks {
		previousTask, _ := r.GetPreviousSuccessfulTask(task.ID)
		tasksWithParamsChanged[i] = models.ScanTaskWithParamsChanged{
			ScanTask:      task,
			ParamsChanged: calculateParamsChanged(&task, previousTask),
		}
	}

	return &models.ScanTaskPageResult{
		Tasks:    tasksWithParamsChanged,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// GetAllTasks retrieves all tasks with params changed info
func (r *ScanTaskRepository) GetAllTasks() ([]models.ScanTaskWithParamsChanged, error) {
	query := `
		SELECT * FROM scan_task
		WHERE disable = 0
		ORDER BY create_time DESC
	`
	var tasks []models.ScanTask
	err := r.DB.Select(&tasks, query)
	if err != nil {
		return nil, err
	}

	tasksWithParamsChanged := make([]models.ScanTaskWithParamsChanged, len(tasks))
	for i, task := range tasks {
		previousTask, _ := r.GetPreviousSuccessfulTask(task.ID)
		tasksWithParamsChanged[i] = models.ScanTaskWithParamsChanged{
			ScanTask:      task,
			ParamsChanged: calculateParamsChanged(&task, previousTask),
		}
	}
	return tasksWithParamsChanged, nil
}

// GetTaskDetailById retrieves task detail by ID with params changed info
func (r *ScanTaskRepository) GetTaskDetailById(taskId int64) (*models.ScanTaskWithParamsChanged, error) {
	task, err := r.GetByID(taskId)
	if err != nil || task == nil {
		return nil, err
	}

	previousTask, _ := r.GetPreviousSuccessfulTask(taskId)
	result := &models.ScanTaskWithParamsChanged{
		ScanTask:      *task,
		ParamsChanged: calculateParamsChanged(task, previousTask),
	}
	return result, nil
}

// joinStrings joins strings with separator
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
