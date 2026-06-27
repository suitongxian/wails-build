package models

import "time"

// DataType represents the type of data (1=file, 2=database)
type DataType int

const (
	DataTypeFile     DataType = 1
	DataTypeDatabase DataType = 2
)

// UploadState represents the upload state of a file
type UploadState int

const (
	UploadStateNotUploaded UploadState = 0
	UploadStateUploaded   UploadState = 1
	UploadStateCopy       UploadState = 2
	UploadStateFailed     UploadState = 3
	UploadStateNoArchive  UploadState = 4
)

// DataDistribution represents a data distribution record in the data_distributing table
type DataDistribution struct {
	DataDistributionID int64      `db:"data_distribution_id" json:"data_distribution_id"`
	ScanTaskID         *int64     `db:"scan_task_id" json:"scan_task_id"`
	Path               string     `db:"path" json:"path"`
	DataType           DataType   `db:"data_type" json:"data_type"`
	ScanFoundCount     int        `db:"scan_found_count" json:"scan_found_count"`
	ContentSign        string     `db:"content_sign" json:"content_sign"`
	FileSuffix         *string    `db:"file_suffix" json:"file_suffix"`
	FileMagic          *string    `db:"file_magic" json:"file_magic"`
	FileCreateTime     *time.Time `db:"file_create_time" json:"file_create_time"`
	FileUpdateTime     *time.Time `db:"file_update_time" json:"file_update_time"`
	FileReadTime       *time.Time `db:"file_read_time" json:"file_read_time"`
	FileSize           int64      `db:"file_size" json:"file_size"`
	FileHide           int        `db:"file_hide" json:"file_hide"`
	UploadState        int        `db:"upload_state" json:"upload_state"`
	IP                 string     `db:"ip" json:"ip"`
	MacAddress         string     `db:"mac_address" json:"mac_address"`
	ParentID           *int64     `db:"parent_id" json:"parent_id"`
	ScanTime           time.Time  `db:"scan_time" json:"scan_time"`
	CreateTime         time.Time  `db:"create_time" json:"create_time"`
	UpdateTime         time.Time  `db:"update_time" json:"update_time"`
	Disable            int        `db:"disable" json:"disable"`
	// Spec B: 扫描期预计算文件特征（nullable；旧行为 NULL）
	Simhash       *int64     `db:"simhash" json:"-"`
	ContentHash   *string    `db:"content_hash" json:"-"`
	Phash         *string    `db:"phash" json:"-"`
	ExtractedText *string    `db:"extracted_text" json:"-"`
	FeatureMtime  *time.Time `db:"feature_mtime" json:"-"`
	FeatureSize   *int64     `db:"feature_size" json:"-"`
	// 2026-05-27 扫描期判定文件是否疑似非个人（系统目录/二进制后缀等）
	SuspectNonPersonal int `db:"suspect_non_personal" json:"suspect_non_personal"`
}

// CreateDataDistributionParams represents parameters for creating a data distribution record
type CreateDataDistributionParams struct {
	ScanTaskID     *int64     `db:"scan_task_id"`
	Path           string     `db:"path"`
	DataType       DataType   `db:"data_type"`
	ContentSign    string     `db:"content_sign"`
	FileSuffix     *string    `db:"file_suffix"`
	FileMagic      *string    `db:"file_magic"`
	FileCreateTime *time.Time `db:"file_create_time"`
	FileUpdateTime *time.Time `db:"file_update_time"`
	FileReadTime   *time.Time `db:"file_read_time"`
	FileSize       int64      `db:"file_size"`
	FileHide       int        `db:"file_hide"`
	IP             string     `db:"ip"`
	MacAddress     string     `db:"mac_address"`
	ParentID       *int64     `db:"parent_id"`
	ScanTime       time.Time  `db:"scan_time"`
}

// FileQueryOptions represents options for querying files
type FileQueryOptions struct {
	Page           int    `db:"page"`
	PageSize       int    `db:"page_size"`
	Search         string `db:"search"`
	WorkspacePath  string `db:"workspace_path"`
	WorkspaceFilter string `db:"workspace_filter"` // 'inside', 'outside', 'all'
	SurvivalFilter  string `db:"survival_filter"`  // 'new', 'deleted', 'normal', 'all'
}

// FileQueryResult represents a query result with files and total count
type FileQueryResult struct {
	Files []DataDistribution `db:"files" json:"files"`
	Total int64             `db:"total" json:"total"`
}

// FileWithCopyCount represents a file with its copy count
type FileWithCopyCount struct {
	DataDistribution
	CopyCount int64 `db:"copy_count" json:"copy_count"`
}

// ArchiveQueryOptions represents options for querying archive files
type ArchiveQueryOptions struct {
	Page               int    `db:"page"`
	PageSize           int    `db:"page_size"`
	Search             string `db:"search"`
	ArchiveType        string `db:"archive_type"` // 'pending', 'core', 'important', 'open'
	ImportanceLevelFilter *int `db:"importance_level_filter"`
}

// ArchiveFileResult represents a result of archive file query
type ArchiveFileResult struct {
	Files []FileWithCopyCount `db:"files" json:"files"`
	Total int64              `db:"total" json:"total"`
}

// ModifiedFileUpdate represents a file modification update
type ModifiedFileUpdate struct {
	DataDistributionID int64     `db:"data_distribution_id"`
	ContentSign        string    `db:"content_sign"`
	FileUpdateTime     *string   `db:"file_update_time"`
	FileReadTime       *string   `db:"file_read_time"`
	FileSize           int64     `db:"file_size"`
	FileMagic          *string   `db:"file_magic"`
}