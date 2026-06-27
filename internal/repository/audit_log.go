package repository

import (
	"encoding/json"
	"time"

	"github.com/jmoiron/sqlx"
)

// AuditLog §11.2 审计日志记录
//
// 与 lifecycle_events 区别：
//   - lifecycle_events 记录"文件版本/底账"层面的生命周期
//   - audit_logs 记录"模块级操作"——模版发布废弃、项目立项激活取消、
//     权限配置变更、导出操作等 lifecycle_events 没覆盖的横向操作。
//
// 字段对齐文档 §11.2。
type AuditLog struct {
	ID          int64     `db:"id" json:"id"`
	ActorID     string    `db:"actor_id" json:"actor_id"`
	ActorUserID *int64    `db:"actor_user_id" json:"actor_user_id"`
	Action      string    `db:"action" json:"action"`
	TargetType  string    `db:"target_type" json:"target_type"`
	TargetID    *int64    `db:"target_id" json:"target_id"`
	TargetCode  *string   `db:"target_code" json:"target_code"`
	BeforeJSON  *string   `db:"before_json" json:"before_json"`
	AfterJSON   *string   `db:"after_json" json:"after_json"`
	IPAddress   *string   `db:"ip_address" json:"ip_address"`
	Message     *string   `db:"message" json:"message"`
	CreateTime  time.Time `db:"create_time" json:"create_time"`
}

// AppendAuditInput Append 入参
//
// 任何字段都可以省（除 ActorID / Action / TargetType 必填）；
// Before/After 传 any 然后内部 JSON 编码。
type AppendAuditInput struct {
	ActorID     string
	ActorUserID int64
	Action      string
	TargetType  string
	TargetID    int64
	TargetCode  string
	Before      any
	After       any
	IPAddress   string
	Message     string
}

// AuditLogRepository audit_logs 表
type AuditLogRepository struct {
	DB *sqlx.DB
}

func NewAuditLogRepository(db *sqlx.DB) *AuditLogRepository {
	return &AuditLogRepository{DB: db}
}

// Append §11 写入一条审计记录。
//
// 写入失败仅 return error 给调用方决定是否阻塞——通常审计失败不应阻断业务，
// 但调用方可视严重程度选择 log 或 abort。
func (r *AuditLogRepository) Append(in AppendAuditInput) (int64, error) {
	now := time.Now()

	var actorUID *int64
	if in.ActorUserID > 0 {
		uid := in.ActorUserID
		actorUID = &uid
	}
	var targetID *int64
	if in.TargetID > 0 {
		tid := in.TargetID
		targetID = &tid
	}
	var targetCode *string
	if in.TargetCode != "" {
		targetCode = &in.TargetCode
	}
	var ip *string
	if in.IPAddress != "" {
		ip = &in.IPAddress
	}
	var msg *string
	if in.Message != "" {
		msg = &in.Message
	}

	var beforeJSON *string
	if in.Before != nil {
		if b, err := json.Marshal(in.Before); err == nil {
			s := string(b)
			beforeJSON = &s
		}
	}
	var afterJSON *string
	if in.After != nil {
		if b, err := json.Marshal(in.After); err == nil {
			s := string(b)
			afterJSON = &s
		}
	}

	res, err := r.DB.Exec(`INSERT INTO audit_logs (
		actor_id, actor_user_id, action, target_type, target_id, target_code,
		before_json, after_json, ip_address, message, create_time
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.ActorID, actorUID, in.Action, in.TargetType, targetID, targetCode,
		beforeJSON, afterJSON, ip, msg, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListByTarget 按 target_type + target_id 查询审计链
func (r *AuditLogRepository) ListByTarget(targetType string, targetID int64) ([]AuditLog, error) {
	var list []AuditLog
	err := r.DB.Select(&list, `SELECT * FROM audit_logs
		WHERE target_type = ? AND target_id = ?
		ORDER BY create_time DESC, id DESC`, targetType, targetID)
	return list, err
}

// Search 全文/筛选查询
type AuditSearchInput struct {
	Action     string
	TargetType string
	ActorID    string
	Limit      int
}

func (r *AuditLogRepository) Search(in AuditSearchInput) ([]AuditLog, error) {
	q := `SELECT * FROM audit_logs WHERE 1=1`
	args := []any{}
	if in.Action != "" {
		q += ` AND action = ?`
		args = append(args, in.Action)
	}
	if in.TargetType != "" {
		q += ` AND target_type = ?`
		args = append(args, in.TargetType)
	}
	if in.ActorID != "" {
		q += ` AND actor_id = ?`
		args = append(args, in.ActorID)
	}
	limit := in.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q += ` ORDER BY create_time DESC, id DESC LIMIT ?`
	args = append(args, limit)

	var list []AuditLog
	err := r.DB.Select(&list, q, args...)
	return list, err
}

// =============================================================================
// §11.1 文档列出的 8 类必须审计的操作，常量集中维护
// =============================================================================

// Action 常量（与 §11.1 文档列表语义一致）
const (
	// 1. 模版发布、废弃、复制、导入
	AuditTemplatePublish   = "template_publish"
	AuditTemplateDeprecate = "template_deprecate"
	AuditTemplateCopy      = "template_copy"
	AuditTemplateImport    = "template_import"
	AuditTemplateUpgrade   = "template_upgrade_version"
	// 2. 项目立项、激活、结项、取消
	AuditProjectCreate   = "project_create"
	AuditProjectActivate = "project_activate"
	AuditProjectClose    = "project_close"
	AuditProjectCancel   = "project_cancel"
	// 3. 三主体变更
	AuditSubjectHandover = "subject_handover"
	// 4. 文件 上传/领取/提交/移动/删除（已落 lifecycle_events，但保留这里以便统一查询）
	AuditFileUpload  = "file_upload"
	AuditFileReceive = "file_receive"
	AuditFileSubmit  = "file_submit"
	AuditFileMove    = "file_move"
	AuditFileDelete  = "file_delete"
	// 6. 权限配置变更
	AuditMemberAdd    = "member_add"
	AuditMemberUpdate = "member_update"
	AuditMemberRemove = "member_remove"
	// 7. 底账关键字段变更
	AuditLedgerChange = "ledger_change"
	// 8. 导出底账或归档包
	AuditExportLedger  = "export_ledger"
	AuditExportArchive = "export_archive"
	// V5-Phase1 §4.3-2 AI 归目应用/驳回（与 file_upload 并存，便于过滤 AI 归目链路）
	AuditAIClassifyApply  = "ai_classify_apply"
	AuditAIClassifyReject = "ai_classify_reject"
	// V5-Phase1 §4.3-4 解除绑定/重新归类
	AuditFvUnbind     = "fv_unbind"
	AuditFvReclassify = "fv_reclassify"
)

// Target type 常量
const (
	AuditTargetTemplate      = "template"
	AuditTargetProject       = "project"
	AuditTargetProjectMember = "project_member"
	AuditTargetLedger        = "ledger"
	AuditTargetFileVersion   = "file_version"
	AuditTargetExport        = "export"
	AuditTargetDataResource  = "data_resource"
)
