package models

import "time"

// FileStatistics represents a file statistics record in the file_statistics table
type FileStatistics struct {
	ID                    int64     `db:"id" json:"id"`
	ScanTaskID            int64     `db:"scan_task_id" json:"scan_task_id"`
	FileTotal             int       `db:"file_total" json:"file_total"`
	WorkspaceFileTotal    int       `db:"workspace_file_total" json:"workspace_file_total"`
	HistoryFileCount      int       `db:"history_file_count" json:"history_file_count"`
	NonHistoryFileCount   int       `db:"non_history_file_count" json:"non_history_file_count"`
	WorkspaceFileClaimedCount int   `db:"workspace_file_claimed_count" json:"workspace_file_claimed_count"`
	HistoryFileClaimedCount    int   `db:"history_file_claimed_count" json:"history_file_claimed_count"`
	NonHistoryFileClaimedCount int  `db:"non_history_file_claimed_count" json:"non_history_file_claimed_count"`
	CreateTime            time.Time `db:"create_time" json:"create_time"`
	UpdateTime            time.Time `db:"update_time" json:"update_time"`
	Disable               int       `db:"disable" json:"disable"`
}

// CreateFileStatisticsParams represents parameters for creating file statistics
type CreateFileStatisticsParams struct {
	ScanTaskID         int64 `db:"scan_task_id"`
	FileTotal          int   `db:"file_total"`
	WorkspaceFileTotal int   `db:"workspace_file_total"`
	HistoryFileCount   int   `db:"history_file_count"`
	NonHistoryFileCount int   `db:"non_history_file_count"`
}

// FileStatisticsResult represents the result of statistics calculation
type FileStatisticsResult struct {
	FileTotal          int `db:"file_total" json:"fileTotal"`
	WorkspaceFileTotal int `db:"workspace_file_total" json:"workspaceFileTotal"`
	HistoryFileCount   int `db:"history_file_count" json:"historyFileCount"`
	NonHistoryFileCount int `db:"non_history_file_count" json:"nonHistoryFileCount"`
}

// StatisticsGrowth represents growth statistics
type StatisticsGrowth struct {
	LastCount    int     `db:"last_count" json:"lastCount"`
	CurrentCount int     `db:"current_count" json:"currentCount"`
	GrowthCount  int     `db:"growth_count" json:"growthCount"`
	GrowthRate   float64 `db:"growth_rate" json:"growthRate"` // Percentage, e.g., 5.5 means 5.5%
}

// FileStatisticsComparison represents comparison between two statistics records
type FileStatisticsComparison struct {
	WorkspaceStatistics   StatisticsGrowth `db:"workspace_statistics" json:"workspaceStatistics"`
	NonHistoryStatistics  StatisticsGrowth `db:"non_history_statistics" json:"nonHistoryStatistics"`
	HistoryStatistics    StatisticsGrowth `db:"history_statistics" json:"historyStatistics"`
	HasComparison        bool             `db:"has_comparison" json:"hasComparison"`
}