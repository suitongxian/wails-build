package models

import "time"

// DataResources represents a data resource record in the data_resources table
type DataResources struct {
	DataResourcesID           int64      `db:"data_resources_id" json:"data_resources_id"`
	ContentSign               string     `db:"content_sign" json:"content_sign"`
	SourceCount               int        `db:"source_count" json:"source_count"`
	WorkspaceSourceCount      int        `db:"workspace_source_count" json:"workspace_source_count"`
	FirstCreateTime           time.Time  `db:"first_create_time" json:"first_create_time"`
	ResourcesName             *string    `db:"resources_name" json:"resources_name"`
	ResourcesDesc             *string    `db:"resources_desc" json:"resources_desc"`
	ContentSubject            *string    `db:"content_subject" json:"content_subject"`
	ContentType               *string    `db:"content_type" json:"content_type"`
	IsClaimed                 int        `db:"is_claimed" json:"is_claimed"`
	ClaimStatus               int        `db:"claim_status" json:"claim_status"`
	ImportanceLevel           int        `db:"importance_level" json:"importance_level"`
	ClaimTime                 *time.Time `db:"claim_time" json:"claim_time"`
	ClaimantName              *string    `db:"claimant_name" json:"claimant_name"`
	ClaimantUnit              *string    `db:"claimant_unit" json:"claimant_unit"`
	DataLevel                 *string    `db:"data_level" json:"data_level"`
	DataShare                 *string    `db:"data_share" json:"data_share"`
	FileMagic                 *string    `db:"file_magic" json:"file_magic"`
	FamilyID                  *int64     `db:"family_id" json:"family_id"`
	FamilyRelation            *string    `db:"family_relation" json:"family_relation"`
	FamilyScore               *float64   `db:"family_score" json:"family_score"`
	FamilyMemberCount         int        `db:"family_member_count" json:"family_member_count"`
	FamilySameContentCount    int        `db:"family_same_content_count" json:"family_same_content_count"`
	FamilyProcessVersionCount int        `db:"family_process_version_count" json:"family_process_version_count"`
	FamilyDerivedCount        int        `db:"family_derived_count" json:"family_derived_count"`
	CreateTime                time.Time  `db:"create_time" json:"create_time"`
	UpdateTime                time.Time  `db:"update_time" json:"update_time"`
	Disable                   int        `db:"disable" json:"disable"`
}

// DataArchive represents archive data which is primarily based on DataDistribution
// with additional resource information from DataResources
type DataArchive struct {
	DataDistribution
	CopyCount       int64 `db:"copy_count" json:"copy_count"`
	ImportanceLevel *int  `db:"importance_level" json:"importance_level"`
}

// DataResourcesWithPrimaryPath 在 DataResources 基础上多带一条"代表性物理路径"
// 用于责任认领列表 hover tooltip 显示完整路径，并辅助副本弹窗剔除主路径不重复展示。
// PrimaryPath 取自 data_distributing 表里同 content_sign 的最早入库记录
// （MIN(data_distribution_id)）。
type DataResourcesWithPrimaryPath struct {
	DataResources
	PrimaryPath *string `db:"primary_path" json:"primary_path"`
	// 2026-05-27 同 content_sign 任一 distributing 行 suspect=1 即为 1，让前端给行打 ⚠
	SuspectNonPersonal int `db:"suspect_non_personal" json:"suspect_non_personal"`
}

// ArchiveType represents the type of archive
type ArchiveType string

const (
	ArchiveTypePending   ArchiveType = "pending"
	ArchiveTypeCore      ArchiveType = "core"
	ArchiveTypeImportant ArchiveType = "important"
	ArchiveTypeOpen      ArchiveType = "open"
)

// ClaimStatus represents the claim status
type ClaimStatus int

const (
	ClaimStatusUnclassified    ClaimStatus = 0
	ClaimStatusPersonalPrivacy ClaimStatus = 1
	ClaimStatusPersonalWork    ClaimStatus = 2
	ClaimStatusNonResponsible  ClaimStatus = 3
)

// ImportanceLevel represents the importance level
type ImportanceLevel int

const (
	ImportanceLevelUnclassified ImportanceLevel = 0
	ImportanceLevelCore         ImportanceLevel = 1
	ImportanceLevelImportant    ImportanceLevel = 2
	ImportanceLevelOpen         ImportanceLevel = 3
	ImportanceLevelPrivacy      ImportanceLevel = 4
	ImportanceLevelNoArchive    ImportanceLevel = 5
)

// DataShare represents the data sharing type
type DataShare int

const (
	DataShareUnconditional DataShare = 0 // 无条件共享
	DataShareConditional   DataShare = 1 // 有条件共享
	DataShareNoShare       DataShare = 2 // 不予共享
)
