package repository

import (
	"time"

	"data-asset-scan-go/internal/models"
	"github.com/jmoiron/sqlx"
)

// FileStatisticsRepository handles database operations for file statistics
type FileStatisticsRepository struct {
	DB *sqlx.DB
}

// NewFileStatisticsRepository creates a new FileStatisticsRepository
func NewFileStatisticsRepository(db *sqlx.DB) *FileStatisticsRepository {
	return &FileStatisticsRepository{DB: db}
}

// ExecuteAndSave executes file statistics calculation and saves the result
func (r *FileStatisticsRepository) ExecuteAndSave(taskId int, workspacePath *string, fullInventoryTime *string) *models.ScanStatistics {
	now := time.Now()

	// Calculate statistics
	stats := r.calculateStatistics(workspacePath, fullInventoryTime)

	// Insert into scan_statistics table
	query := `
		INSERT INTO scan_statistics (
			scan_task_id, file_total, workspace_file_total,
			history_file_count, non_history_file_count,
			create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, 0)
	`

	result, err := r.DB.Exec(query,
		taskId,
		stats.FileTotal,
		stats.WorkspaceFileTotal,
		stats.HistoryFileCount,
		stats.NonHistoryFileCount,
		now,
		now,
	)
	if err != nil {
		return nil
	}

	id, _ := result.LastInsertId()

	return &models.ScanStatistics{
		ID:                  id,
		ScanTaskID:          int64(taskId),
		FileTotal:           stats.FileTotal,
		WorkspaceFileTotal:  stats.WorkspaceFileTotal,
		HistoryFileCount:    stats.HistoryFileCount,
		NonHistoryFileCount: stats.NonHistoryFileCount,
		CreateTime:          now,
		UpdateTime:          now,
		Disable:             0,
	}
}

// calculateStatistics calculates file statistics from data_resources table
func (r *FileStatisticsRepository) calculateStatistics(workspacePath *string, fullInventoryTime *string) models.ScanStatisticsResult {
	var result models.ScanStatisticsResult

	// Get total file count (sum of source_count)
	var totalFileCount int
	r.DB.Get(&totalFileCount, `SELECT COALESCE(SUM(source_count), 0) FROM data_resources WHERE disable = 0`)
	result.FileTotal = totalFileCount

	// Get workspace file count (sum of workspace_source_count)
	var workspaceFileTotal int
	r.DB.Get(&workspaceFileTotal, `SELECT COALESCE(SUM(workspace_source_count), 0) FROM data_resources WHERE disable = 0`)
	result.WorkspaceFileTotal = workspaceFileTotal

	// Get history and non-history file counts based on first_create_time
	if fullInventoryTime != nil && *fullInventoryTime != "" {
		var historyCount, nonHistoryCount int
		r.DB.Get(&historyCount, `SELECT COALESCE(SUM(source_count), 0) FROM data_resources WHERE disable = 0 AND first_create_time < ?`, *fullInventoryTime)
		r.DB.Get(&nonHistoryCount, `SELECT COALESCE(SUM(source_count), 0) FROM data_resources WHERE disable = 0 AND first_create_time >= ?`, *fullInventoryTime)
		result.HistoryFileCount = historyCount
		result.NonHistoryFileCount = nonHistoryCount
	}

	return result
}

// GetByTaskId retrieves statistics by task ID
func (r *FileStatisticsRepository) GetByTaskId(taskId int) (*models.ScanStatistics, error) {
	var stats models.ScanStatistics
	query := `SELECT * FROM scan_statistics WHERE scan_task_id = ? AND disable = 0`
	err := r.DB.Get(&stats, query, taskId)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// GetLatest retrieves the latest statistics record
func (r *FileStatisticsRepository) GetLatest() (*models.ScanStatistics, error) {
	var stats models.ScanStatistics
	query := `SELECT * FROM scan_statistics WHERE disable = 0 ORDER BY create_time DESC LIMIT 1`
	err := r.DB.Get(&stats, query)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// GetGrowth calculates growth statistics between two scans
func (r *FileStatisticsRepository) GetGrowth(lastTaskId, currentTaskId int) (*models.ScanStatisticsGrowth, error) {
	lastStats, err := r.GetByTaskId(lastTaskId)
	if err != nil || lastStats == nil {
		return nil, err
	}

	currentStats, err := r.GetByTaskId(currentTaskId)
	if err != nil || currentStats == nil {
		return nil, err
	}

	lastCount := lastStats.FileTotal
	currentCount := currentStats.FileTotal
	growthCount := currentCount - lastCount
	growthRate := 0.0
	if lastCount > 0 {
		growthRate = float64(growthCount) / float64(lastCount) * 100
	}

	return &models.ScanStatisticsGrowth{
		LastCount:    lastCount,
		CurrentCount: currentCount,
		GrowthCount:  growthCount,
		GrowthRate:   growthRate,
	}, nil
}
