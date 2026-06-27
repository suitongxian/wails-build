package models

import "time"

// ScanType represents the type of scan task
type ScanType string

const (
	ScanTypeFile     ScanType = "FILE"
	ScanTypeDatabase ScanType = "DATABASE"
)

// TaskState represents the state of a scan task
type TaskState string

const (
	TaskStateRun     TaskState = "run"
	TaskStateSucceed TaskState = "succeed"
	TaskStateFail    TaskState = "fail"
)

// ScanTask represents a scan task record in the scan_task table
type ScanTask struct {
	ID                int64      `db:"id" json:"id"`
	ScanType          ScanType   `db:"scan_type" json:"scan_type"`
	FileScanRange     *string    `db:"file_scan_range" json:"file_scan_range"`
	Heartbeat         int        `db:"heartbeat" json:"heartbeat"`
	WorkspacePath     *string    `db:"workspace_path" json:"workspace_path"`
	TaskState         TaskState  `db:"task_state" json:"task_state"`
	TaskPhase         *string    `db:"task_phase" json:"task_phase"`
	TaskErrorMessage  *string    `db:"task_error_message" json:"task_error_message"`
	ScanArgs          *string    `db:"scan_args" json:"scan_args"`
	FileTotal         *int       `db:"file_total" json:"file_total"`
	FileScannedCount  *int       `db:"file_scanned_count" json:"file_scanned_count"`
	FileAllSuffixText *string    `db:"file_all_suffix_text" json:"file_all_suffix_text"`
	FileAllSuffixCount *int      `db:"file_all_suffix_count" json:"file_all_suffix_count"`
	FileCountSuffixCount *int     `db:"file_count_suffix_count" json:"file_count_suffix_count"`
	WorkspaceCount    *int       `db:"workspace_count" json:"workspace_count"`
	EndTime           *time.Time `db:"end_time" json:"end_time"`
	ScanLog           *string    `db:"scan_log" json:"scan_log"`
	CreateTime        time.Time  `db:"create_time" json:"create_time"`
	UpdateTime        time.Time  `db:"update_time" json:"update_time"`
	Disable           int        `db:"disable" json:"disable"`
}

// ParamsChanged represents information about parameter changes
type ParamsChanged struct {
	WorkspacePathChanged  bool `db:"workspace_path_changed" json:"workspacePathChanged"`
	ScanAreaPathChanged   bool `db:"scan_area_path_changed" json:"scanAreaPathChanged"`
	ControlTypeChanged    bool `db:"control_type_changed" json:"controlTypeChanged"`
}

// ScanTaskWithParamsChanged represents a scan task with parameter change information
type ScanTaskWithParamsChanged struct {
	ScanTask
	ParamsChanged ParamsChanged `db:"params_changed" json:"paramsChanged"`
}

// ScanTaskQueryParams represents query parameters for scan tasks
type ScanTaskQueryParams struct {
	Page     int `db:"page"`
	PageSize int `db:"page_size"`
}

// ScanTaskPageResult represents a paginated result of scan tasks
type ScanTaskPageResult struct {
	Tasks   []ScanTaskWithParamsChanged `db:"tasks" json:"tasks"`
	Total   int64                      `db:"total" json:"total"`
	Page    int                        `db:"page" json:"page"`
	PageSize int                       `db:"page_size" json:"pageSize"`
}

// CreateScanTaskParams represents parameters for creating a scan task
type CreateScanTaskParams struct {
	ScanType     ScanType `db:"scan_type"`
	FileScanRange *string `db:"file_scan_range"`
	WorkspacePath *string `db:"workspace_path"`
	ScanArgs     *string `db:"scan_args"`
	FileTotal     *int    `db:"file_total"`
}

// UpdateProgressParams represents parameters for updating scan progress
type UpdateProgressParams struct {
	Heartbeat       int    `db:"heartbeat"`
	FileScannedCount int   `db:"file_scanned_count"`
	TaskPhase       *string `db:"task_phase"`
}

// UpdateWorkspaceParams represents parameters for updating workspace info
type UpdateWorkspaceParams struct {
	WorkspacePath       *string `db:"workspace_path"`
	FileAllSuffixCount  *int    `db:"file_all_suffix_count"`
	FileCountSuffixCount *int   `db:"file_count_suffix_count"`
	WorkspaceCount      *int    `db:"workspace_count"`
}