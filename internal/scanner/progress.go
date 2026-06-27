package scanner

// ProgressUpdateType represents the type of progress update
type ProgressUpdateType string

const (
	// ProgressUpdate indicates a regular progress update
	ProgressUpdateTypeValue ProgressUpdateType = "progress"
	// CompleteUpdate indicates scan completion
	CompleteUpdate ProgressUpdateType = "complete"
	// ErrorUpdate indicates an error occurred
	ErrorUpdate ProgressUpdateType = "error"
)

// ProgressUpdate represents an SSE progress update sent during scanning
type ProgressUpdate struct {
	// Type indicates the type of update: "progress", "complete", or "error"
	Type string `json:"type"`

	// Phase indicates the current phase of scanning
	Phase string `json:"phase,omitempty"`

	// ScannedCount is the number of files scanned so far
	ScannedCount int `json:"scannedCount"`

	// TotalCount is the total number of files to scan (0 if unknown)
	TotalCount int `json:"totalCount"`

	// CurrentFile is the path of the file currently being processed
	CurrentFile string `json:"currentFile,omitempty"`

	// ElapsedMs is the elapsed time in milliseconds since scan started
	ElapsedMs int64 `json:"elapsedMs"`

	// DAILY_CHECK specific fields
	// NewFiles is the count of new files found in DAILY_CHECK mode
	NewFiles int `json:"newFiles,omitempty"`
	// NormalFiles is the count of unchanged files in DAILY_CHECK mode
	NormalFiles int `json:"normalFiles,omitempty"`
	// DeletedFiles is the count of deleted files in DAILY_CHECK mode
	DeletedFiles int `json:"deletedFiles,omitempty"`
	// ModifiedFiles is the count of modified files in DAILY_CHECK mode
	ModifiedFiles int `json:"modifiedFiles,omitempty"`

	// Indeterminate indicates whether the progress is indeterminate
	// (true when totalCount is not yet known)
	Indeterminate bool `json:"indeterminate,omitempty"`

	// Success indicates whether the scan completed successfully
	// Only present in "complete" type updates
	Success bool `json:"success,omitempty"`
}

// ScanProgressInfo represents internal progress information
type ScanProgressInfo struct {
	TaskId       int
	Phase        string
	ScannedCount int
	TotalCount   int
	CurrentFile  string
}