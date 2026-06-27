package models

import "time"

// =============================================
// 数据业务模版镜像（cached from manage）
// =============================================

// BusinessClass 行业/业务职能分类
type BusinessClass struct {
	ID          int64     `db:"id" json:"id"`
	RemoteID    *int64    `db:"remote_id" json:"remote_id"`
	Code        string    `db:"code" json:"code"`
	Name        string    `db:"name" json:"name"`
	Type        string    `db:"type" json:"type"`
	ParentID    *int64    `db:"parent_id" json:"parent_id"`
	Description *string   `db:"description" json:"description"`
	CachedAt    time.Time `db:"cached_at" json:"cached_at"`
	CreateTime  time.Time `db:"create_time" json:"create_time"`
	UpdateTime  time.Time `db:"update_time" json:"update_time"`
	Disable     int       `db:"disable" json:"disable"`
}

// DataTemplate 数据业务模版主信息
type DataTemplate struct {
	ID                      int64   `db:"id" json:"id"`
	RemoteID                *int64  `db:"remote_id" json:"remote_id"`
	TemplateCode            string  `db:"template_code" json:"template_code"`
	TemplateName            string  `db:"template_name" json:"template_name"`
	TemplateVersion         string  `db:"template_version" json:"template_version"`
	ClassCode               *string `db:"class_code" json:"class_code"`
	Scenario                *string `db:"scenario" json:"scenario"`
	Publisher               *string `db:"publisher" json:"publisher"`
	Status                  string  `db:"status" json:"status"`
	IsPublished             int     `db:"is_published" json:"is_published"` // 本地模版「是否发布」：0未发布/1已发布；只有已发布才能立项
	ProjectSensitivityLevel string  `db:"project_sensitivity_level" json:"project_sensitivity_level"`
	UseShareScope           *string `db:"use_share_scope" json:"use_share_scope"`
	SharingOpenConditions   *string `db:"sharing_open_conditions" json:"sharing_open_conditions"`
	Description             *string `db:"description" json:"description"`
	SourceEndpoint          *string `db:"source_endpoint" json:"source_endpoint"`
	// Origin 区分模版来源：local=scan 本地创作，synced=从 manage 同步（默认）。
	Origin string `db:"origin" json:"origin"`
	// 以下为 scan 本地创作字段（原型「数据项目模版」表单项）；同步来的模版留空。
	Scope         string  `db:"scope" json:"scope"`                   // 模版归类 industry/unit/department/person
	ShortCode     *string `db:"short_code" json:"short_code"`         // 代号/简称
	Manager       *string `db:"manager" json:"manager"`               // 负责人
	Owner         *string `db:"owner" json:"owner"`                   // 数据所有权归属
	ApprovalBasis *string `db:"approval_basis" json:"approval_basis"` // 立项依据
	// 项目认定模版（2026-06-20）
	Edited        int     `db:"edited" json:"edited"`                 // 立项过程中是否动过结构（提取门禁）
	Certified     int     `db:"certified" json:"certified"`           // 是否「项目认定模版」（单位最高权威）
	CertifiedFrom *string `db:"certified_from" json:"certified_from"` // 认定来源项目编码（溯源）
	// 反向同步到 manage 的状态（阶段二）
	SyncStatus  *string    `db:"sync_status" json:"sync_status"`
	SyncMessage *string    `db:"sync_message" json:"sync_message"`
	SyncedAt    *time.Time `db:"synced_at" json:"synced_at"`
	CachedAt    time.Time  `db:"cached_at" json:"cached_at"`
	CreateTime  time.Time  `db:"create_time" json:"create_time"`
	UpdateTime  time.Time  `db:"update_time" json:"update_time"`
	Disable     int        `db:"disable" json:"disable"`
}

// TemplateStage 模版工作环节
type TemplateStage struct {
	ID               int64     `db:"id" json:"id"`
	RemoteID         *int64    `db:"remote_id" json:"remote_id"`
	TemplateID       int64     `db:"template_id" json:"template_id"`
	StageCode        string    `db:"stage_code" json:"stage_code"`
	StageName        string    `db:"stage_name" json:"stage_name"`
	StageType        string    `db:"stage_type" json:"stage_type"`
	SortOrder        int       `db:"sort_order" json:"sort_order"`
	Description      *string   `db:"description" json:"description"`
	DefaultRoleCodes *string   `db:"default_role_codes" json:"default_role_codes"`
	Manager          *string   `db:"manager" json:"manager"`                     // 责任人（显示名）
	Members          *string   `db:"members" json:"members"`                     // 参与人（显示名，逗号分隔）
	ManagerUsername  *string   `db:"manager_username" json:"manager_username"`   // 责任人 username（防重名/过滤）
	MembersUsernames *string   `db:"members_usernames" json:"members_usernames"` // 参与人 username（逗号分隔）
	CachedAt         time.Time `db:"cached_at" json:"cached_at"`
	CreateTime       time.Time `db:"create_time" json:"create_time"`
	UpdateTime       time.Time `db:"update_time" json:"update_time"`
	Disable          int       `db:"disable" json:"disable"`
}

// TemplateTask 模版文件任务（五层中间层：工作环节 ▸ 文件任务 ▸ 文档标识）
// 2026-05-31 scan 本地五层模版创作引入；同步自 manage 的 4 层模版不产生此层。
type TemplateTask struct {
	ID               int64     `db:"id" json:"id"`
	RemoteID         *int64    `db:"remote_id" json:"remote_id"`
	TemplateStageID  int64     `db:"template_stage_id" json:"template_stage_id"`
	TaskCode         string    `db:"task_code" json:"task_code"`
	TaskName         string    `db:"task_name" json:"task_name"`
	Manager          *string   `db:"manager" json:"manager"`
	SensitivityLevel *string   `db:"sensitivity_level" json:"sensitivity_level"`
	SortOrder        int       `db:"sort_order" json:"sort_order"`
	Description      *string   `db:"description" json:"description"`
	CachedAt         time.Time `db:"cached_at" json:"cached_at"`
	CreateTime       time.Time `db:"create_time" json:"create_time"`
	UpdateTime       time.Time `db:"update_time" json:"update_time"`
	Disable          int       `db:"disable" json:"disable"`
}

// TemplateFileRule 模版文件版本规则
type TemplateFileRule struct {
	ID              int64  `db:"id" json:"id"`
	RemoteID        *int64 `db:"remote_id" json:"remote_id"`
	TemplateStageID int64  `db:"template_stage_id" json:"template_stage_id"`
	// TemplateTaskID 本地五层模版中所属「文件任务」；同步来的 4 层模版留空（仍按 stage 归属）。
	TemplateTaskID          *int64  `db:"template_task_id" json:"template_task_id"`
	FileRuleCode            string  `db:"file_rule_code" json:"file_rule_code"`
	FileName                string  `db:"file_name" json:"file_name"`
	DataState               string  `db:"data_state" json:"data_state"`
	Required                int     `db:"required" json:"required"`
	AllowedFileTypes        string  `db:"allowed_file_types" json:"allowed_file_types"`
	NamingPattern           *string `db:"naming_pattern" json:"naming_pattern"`
	SummaryPattern          *string `db:"summary_pattern" json:"summary_pattern"`
	DefaultRetentionPolicy  *string `db:"default_retention_policy" json:"default_retention_policy"`
	DefaultSecurityPolicyID *int64  `db:"default_security_policy_id" json:"default_security_policy_id"`
	SensitivityLevel        *string `db:"sensitivity_level" json:"sensitivity_level"` // 文件级敏感（就高不就低，本地创作）
	Drafter                 *string `db:"drafter" json:"drafter"`                     // 起草人（本地创作）
	// L6 文档标识管控类字段
	Category             *string   `db:"category" json:"category"`                           // 文档类别：未识别/个人/工作/非责任
	SecurityRequirement  *string   `db:"security_requirement" json:"security_requirement"`   // 安全要求：明文存储/加密存储
	DiffusionRequirement *string   `db:"diffusion_requirement" json:"diffusion_requirement"` // 防扩散：孤本模式/双孤本模式
	ArchiveRequirement   *string   `db:"archive_requirement" json:"archive_requirement"`     // 归档要求：个人文件夹/部门文件柜/单位文件室
	RetentionPeriodDays  *int      `db:"retention_period_days" json:"retention_period_days"` // 保留期天数（-1=永久）
	DestructionRule      *string   `db:"destruction_rule" json:"destruction_rule"`           // 销毁规则
	SortOrder            int       `db:"sort_order" json:"sort_order"`
	CachedAt             time.Time `db:"cached_at" json:"cached_at"`
	CreateTime           time.Time `db:"create_time" json:"create_time"`
	UpdateTime           time.Time `db:"update_time" json:"update_time"`
	Disable              int       `db:"disable" json:"disable"`
}

// =============================================
// 五层嵌套树（本地模版编辑器读取用）：项目 ▸ 事项 ▸ 任务 ▸ 标识
// =============================================

// LocalTemplateTaskNode 任务节点（含其下文档标识）
type LocalTemplateTaskNode struct {
	TemplateTask
	FileRules []TemplateFileRule `json:"file_rules"`
}

// LocalTemplateStageNode 事项节点（含其下任务）
type LocalTemplateStageNode struct {
	TemplateStage
	Tasks []LocalTemplateTaskNode `json:"tasks"`
}

// LocalTemplateTree 项目模版完整五层树
type LocalTemplateTree struct {
	Template DataTemplate             `json:"template"`
	Stages   []LocalTemplateStageNode `json:"stages"`
}

// =============================================
// 三主体 subjects
// =============================================

// Subject 三主体（人/部门/组织）
type Subject struct {
	ID         int64     `db:"id" json:"id"`
	Code       string    `db:"code" json:"code"`
	Name       string    `db:"name" json:"name"`
	Type       string    `db:"type" json:"type"`
	ParentID   *int64    `db:"parent_id" json:"parent_id"`
	Contact    *string   `db:"contact" json:"contact"`
	Status     string    `db:"status" json:"status"`
	CreateTime time.Time `db:"create_time" json:"create_time"`
	UpdateTime time.Time `db:"update_time" json:"update_time"`
	Disable    int       `db:"disable" json:"disable"`
}

// =============================================
// security_policies 安全策略基线
// =============================================

// SecurityPolicy 安全策略
type SecurityPolicy struct {
	ID               int64     `db:"id" json:"id"`
	PolicyCode       string    `db:"policy_code" json:"policy_code"`
	PolicyName       string    `db:"policy_name" json:"policy_name"`
	SensitivityLevel string    `db:"sensitivity_level" json:"sensitivity_level"`
	FileState        *string   `db:"file_state" json:"file_state"`
	StorageTier      string    `db:"storage_tier" json:"storage_tier"`
	Permissions      string    `db:"permissions" json:"permissions"`
	ProtectionRules  *string   `db:"protection_rules" json:"protection_rules"`
	AuditRequired    int       `db:"audit_required" json:"audit_required"`
	CreateTime       time.Time `db:"create_time" json:"create_time"`
	UpdateTime       time.Time `db:"update_time" json:"update_time"`
	Disable          int       `db:"disable" json:"disable"`
}

// =============================================
// data_projects 项目实例
// =============================================

// DataProject 项目实例
type DataProject struct {
	ID                 int64      `db:"id" json:"id"`
	ProjectCode        string     `db:"project_code" json:"project_code"`
	ProjectName        string     `db:"project_name" json:"project_name"`
	ObjectShortCode    *string    `db:"object_short_code" json:"object_short_code"`
	TemplateID         *int64     `db:"template_id" json:"template_id"`
	TemplateCode       string     `db:"template_code" json:"template_code"`
	TemplateVersion    string     `db:"template_version" json:"template_version"`
	TaskSummary        *string    `db:"task_summary" json:"task_summary"`
	ApprovalBasis      *string    `db:"approval_basis" json:"approval_basis"`
	PlannedStartDate   *time.Time `db:"planned_start_date" json:"planned_start_date"`
	PlannedEndDate     *time.Time `db:"planned_end_date" json:"planned_end_date"`
	SensitivityLevel   string     `db:"sensitivity_level" json:"sensitivity_level"`
	ManagementMode     string     `db:"management_mode" json:"management_mode"`
	OwnerSubjectID     int64      `db:"owner_subject_id" json:"owner_subject_id"`
	CustodianSubjectID int64      `db:"custodian_subject_id" json:"custodian_subject_id"`
	SecuritySubjectID  int64      `db:"security_subject_id" json:"security_subject_id"`
	Status             string     `db:"status" json:"status"`
	ProjectRoot        *string    `db:"project_root" json:"project_root"`
	SyncStatus         *string    `db:"sync_status" json:"sync_status"`
	SyncMessage        *string    `db:"sync_message" json:"sync_message"`
	SyncedAt           *time.Time `db:"synced_at" json:"synced_at"`
	CreatedBy          *string    `db:"created_by" json:"created_by"`
	CreatedByUserID    *int64     `db:"created_by_user_id" json:"created_by_user_id"`
	CreateTime         time.Time  `db:"create_time" json:"create_time"`
	UpdateTime         time.Time  `db:"update_time" json:"update_time"`
	Disable            int        `db:"disable" json:"disable"`
}

// =============================================
// project_stages 项目工作环节实例
// =============================================

// ProjectStage 项目工作环节
type ProjectStage struct {
	ID                int64     `db:"id" json:"id"`
	ProjectID         int64     `db:"project_id" json:"project_id"`
	TemplateStageID   *int64    `db:"template_stage_id" json:"template_stage_id"`
	StageCode         string    `db:"stage_code" json:"stage_code"`
	StageName         string    `db:"stage_name" json:"stage_name"`
	StageType         string    `db:"stage_type" json:"stage_type"`
	SortOrder         int       `db:"sort_order" json:"sort_order"`
	Status            string    `db:"status" json:"status"`
	AssignedRoleCodes *string   `db:"assigned_role_codes" json:"assigned_role_codes"`
	DirectoryPath     *string   `db:"directory_path" json:"directory_path"`
	CreateTime        time.Time `db:"create_time" json:"create_time"`
	UpdateTime        time.Time `db:"update_time" json:"update_time"`
	Disable           int       `db:"disable" json:"disable"`
}

// =============================================
// file_versions 文件版本实例
// =============================================

// FileVersion 文件版本实例
type FileVersion struct {
	ID                  int64      `db:"id" json:"id"`
	ProjectID           int64      `db:"project_id" json:"project_id"`
	ProjectStageID      int64      `db:"project_stage_id" json:"project_stage_id"`
	TemplateFileRuleID  *int64     `db:"template_file_rule_id" json:"template_file_rule_id"`
	FileVersionCode     string     `db:"file_version_code" json:"file_version_code"`
	LocalCode           string     `db:"local_code" json:"local_code"`
	DisplayName         string     `db:"display_name" json:"display_name"`
	DataState           string     `db:"data_state" json:"data_state"`
	VersionNo           string     `db:"version_no" json:"version_no"`
	Required            int        `db:"required" json:"required"`
	FileType            *string    `db:"file_type" json:"file_type"`
	StorageURI          *string    `db:"storage_uri" json:"storage_uri"`
	Checksum            *string    `db:"checksum" json:"checksum"`
	FileSize            *int64     `db:"file_size" json:"file_size"`
	SourceFileVersionID *int64     `db:"source_file_version_id" json:"source_file_version_id"`
	ProducedFromEventID *int64     `db:"produced_from_event_id" json:"produced_from_event_id"`
	SecurityPolicyID    *int64     `db:"security_policy_id" json:"security_policy_id"`
	LifecycleStatus     string     `db:"lifecycle_status" json:"lifecycle_status"`
	CreatedBy           *string    `db:"created_by" json:"created_by"`
	CreatedByUserID     *int64     `db:"created_by_user_id" json:"created_by_user_id"`
	SubmittedAt         *time.Time `db:"submitted_at" json:"submitted_at"`
	SubmittedBy         *string    `db:"submitted_by" json:"submitted_by"`
	SubmittedByUserID   *int64     `db:"submitted_by_user_id" json:"submitted_by_user_id"`
	OriginalFileName    *string    `db:"original_file_name" json:"original_file_name"`
	CabinetSyncStatus   *string    `db:"cabinet_sync_status" json:"cabinet_sync_status"`
	CabinetSyncMessage  *string    `db:"cabinet_sync_message" json:"cabinet_sync_message"`
	CabinetSyncedAt     *time.Time `db:"cabinet_synced_at" json:"cabinet_synced_at"`
	// V5-Phase1 §4.3-4 解绑 + 重新归类
	UnbindTime           *time.Time `db:"unbind_time" json:"unbind_time"`
	UnbindReason         *string    `db:"unbind_reason" json:"unbind_reason"`
	ReclassifiedFromFvID *int64     `db:"reclassified_from_fv_id" json:"reclassified_from_fv_id"`
	CreateTime           time.Time  `db:"create_time" json:"create_time"`
	UpdateTime           time.Time  `db:"update_time" json:"update_time"`
	Disable              int        `db:"disable" json:"disable"`
}

// =============================================
// asset_ledgers 数据资产标识底账
// =============================================

// AssetLedger 数据资产标识底账
type AssetLedger struct {
	ID                 int64     `db:"id" json:"id"`
	LedgerCode         string    `db:"ledger_code" json:"ledger_code"`
	FileVersionID      int64     `db:"file_version_id" json:"file_version_id"`
	ClassCode          *string   `db:"class_code" json:"class_code"`
	ProjectCode        string    `db:"project_code" json:"project_code"`
	StageCode          string    `db:"stage_code" json:"stage_code"`
	FileVersionCode    string    `db:"file_version_code" json:"file_version_code"`
	AssetName          string    `db:"asset_name" json:"asset_name"`
	ContentSummary     *string   `db:"content_summary" json:"content_summary"`
	OwnerSubjectID     int64     `db:"owner_subject_id" json:"owner_subject_id"`
	CustodianSubjectID int64     `db:"custodian_subject_id" json:"custodian_subject_id"`
	SecuritySubjectID  int64     `db:"security_subject_id" json:"security_subject_id"`
	SensitivityLevel   string    `db:"sensitivity_level" json:"sensitivity_level"`
	MarkingMethod      string    `db:"marking_method" json:"marking_method"`
	SourceRef          *string   `db:"source_ref" json:"source_ref"`
	CurrentStorageURI  *string   `db:"current_storage_uri" json:"current_storage_uri"`
	LifecycleStatus    string    `db:"lifecycle_status" json:"lifecycle_status"`
	CreateTime         time.Time `db:"create_time" json:"create_time"`
	UpdateTime         time.Time `db:"update_time" json:"update_time"`
	Disable            int       `db:"disable" json:"disable"`
	// 2026-05-21 三级分流治理 · 核心登记字段（代码层保留 memorandum_* 前缀）
	MemorandumTopic          *string    `db:"memorandum_topic" json:"memorandum_topic"`
	MemorandumClassification *string    `db:"memorandum_classification" json:"memorandum_classification"`
	MemorandumRegisteredAt   *time.Time `db:"memorandum_registered_at" json:"memorandum_registered_at"`
	MemorandumRegisteredBy   *int64     `db:"memorandum_registered_by" json:"memorandum_registered_by"`
	MemorandumSignatureHash  *string    `db:"memorandum_signature_hash" json:"memorandum_signature_hash"`
}

// =============================================
// lifecycle_events 生命周期事件流
// =============================================

// LifecycleEvent 生命周期事件
type LifecycleEvent struct {
	ID             int64     `db:"id" json:"id"`
	FileVersionID  int64     `db:"file_version_id" json:"file_version_id"`
	LedgerID       *int64    `db:"ledger_id" json:"ledger_id"`
	EventType      string    `db:"event_type" json:"event_type"`
	EventName      string    `db:"event_name" json:"event_name"`
	OperatorID     *string   `db:"operator_id" json:"operator_id"`
	OperatorUserID *int64    `db:"operator_user_id" json:"operator_user_id"`
	FromSubjectID  *int64    `db:"from_subject_id" json:"from_subject_id"`
	ToSubjectID    *int64    `db:"to_subject_id" json:"to_subject_id"`
	FromStorageURI *string   `db:"from_storage_uri" json:"from_storage_uri"`
	ToStorageURI   *string   `db:"to_storage_uri" json:"to_storage_uri"`
	Reason         *string   `db:"reason" json:"reason"`
	ApprovalRef    *string   `db:"approval_ref" json:"approval_ref"`
	MetadataBefore *string   `db:"metadata_before" json:"metadata_before"`
	MetadataAfter  *string   `db:"metadata_after" json:"metadata_after"`
	CreateTime     time.Time `db:"create_time" json:"create_time"`
}

// =============================================
// project_members 项目成员
// =============================================

// ProjectMember 项目成员
//
// V2 起 UserID 是规范字段（与需求文档 §4.11 对齐）。
// V1 SubjectID 字段保留过渡，V2 数据迁移会按 subjects.name 反查填充 UserID。
type ProjectMember struct {
	ID                int64     `db:"id" json:"id"`
	ProjectID         int64     `db:"project_id" json:"project_id"`
	UserID            *int64    `db:"user_id" json:"user_id"`       // V2 新增：与 users.id 关联
	SubjectID         int64     `db:"subject_id" json:"subject_id"` // V1 遗留，过渡期保留
	RoleCode          string    `db:"role_code" json:"role_code"`
	StageIDs          *string   `db:"stage_ids" json:"stage_ids"`
	PermissionActions string    `db:"permission_actions" json:"permission_actions"`
	CreateTime        time.Time `db:"create_time" json:"create_time"`
	UpdateTime        time.Time `db:"update_time" json:"update_time"`
	Disable           int       `db:"disable" json:"disable"`
}
