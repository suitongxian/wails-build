package repository

import (
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// 事件类型常量
const (
	EventRegister  = "register"
	EventUse       = "use"
	EventTransfer  = "transfer"
	EventChange    = "change"
	EventHandover  = "handover"
	EventArchive   = "archive"
	EventDestroy   = "destroy"
	EventPermanent = "permanent"
	// V5-Phase1 §4.3-4 解除绑定 + 重新归类
	EventUnbind     = "unbind"
	EventReclassify = "reclassify"
)

// LifecycleEventRepository 生命周期事件流（仅追加）
type LifecycleEventRepository struct {
	DB *sqlx.DB
}

func NewLifecycleEventRepository(db *sqlx.DB) *LifecycleEventRepository {
	return &LifecycleEventRepository{DB: db}
}

// AppendEventInput 追加事件入参
type AppendEventInput struct {
	FileVersionID  int64
	LedgerID       *int64
	EventType      string
	EventName      string
	OperatorID     *string // V1：操作人用户名字符串（保持 TEXT 列名，未改名以兼容历史）
	OperatorUserID *int64  // V2：users.id（与 OperatorID 并存写入）
	FromSubjectID  *int64
	ToSubjectID    *int64
	FromStorageURI *string
	ToStorageURI   *string
	Reason         *string
	ApprovalRef    *string
	MetadataBefore *string
	MetadataAfter  *string
}

// Append 在事务内追加事件（不可删除）
func (r *LifecycleEventRepository) Append(tx sqlx.Ext, in AppendEventInput) (int64, error) {
	now := time.Now()
	res, err := tx.Exec(`INSERT INTO lifecycle_events (
		file_version_id, ledger_id, event_type, event_name, operator_id, operator_user_id,
		from_subject_id, to_subject_id, from_storage_uri, to_storage_uri,
		reason, approval_ref, metadata_before, metadata_after, create_time
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.FileVersionID, in.LedgerID, in.EventType, in.EventName, in.OperatorID, in.OperatorUserID,
		in.FromSubjectID, in.ToSubjectID, in.FromStorageURI, in.ToStorageURI,
		in.Reason, in.ApprovalRef, in.MetadataBefore, in.MetadataAfter, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AppendNoTx 主连接追加（非事务场景）
func (r *LifecycleEventRepository) AppendNoTx(in AppendEventInput) (int64, error) {
	return r.Append(r.DB, in)
}

// ListByFileVersion 按文件版本列出事件链
func (r *LifecycleEventRepository) ListByFileVersion(fileVersionID int64) ([]models.LifecycleEvent, error) {
	var list []models.LifecycleEvent
	err := r.DB.Select(&list, `SELECT * FROM lifecycle_events WHERE file_version_id = ? ORDER BY create_time, id`, fileVersionID)
	return list, err
}

// ListByLedger 按底账列出事件
func (r *LifecycleEventRepository) ListByLedger(ledgerID int64) ([]models.LifecycleEvent, error) {
	var list []models.LifecycleEvent
	err := r.DB.Select(&list, `SELECT * FROM lifecycle_events WHERE ledger_id = ? ORDER BY create_time, id`, ledgerID)
	return list, err
}

// ListByProjectFileVersions 列出某项目所有 file_version 的事件
func (r *LifecycleEventRepository) ListByProject(projectCode string) ([]models.LifecycleEvent, error) {
	var list []models.LifecycleEvent
	err := r.DB.Select(&list, `SELECT le.* FROM lifecycle_events le
		JOIN asset_ledgers al ON al.id = le.ledger_id
		WHERE al.project_code = ?
		ORDER BY le.create_time, le.id`, projectCode)
	return list, err
}

// ValidStateTransition 校验状态转换合法性
//
// 状态机：
//
//	planned    -> registered（入账）
//	registered -> in_use（领取/使用）
//	in_use     -> registered（定稿提交）
//	registered -> sealed（归档封存）
//	in_use     -> sealed（归档封存）
//	sealed     -> destroyed（销账）
//	sealed     -> permanent（永存）
func ValidStateTransition(from, to string) bool {
	allowed := map[string][]string{
		"planned":    {"registered"},
		"registered": {"in_use", "sealed"},
		"in_use":     {"registered", "sealed"},
		"sealed":     {"destroyed", "permanent"},
		"destroyed":  {},
		"permanent":  {},
	}
	for _, t := range allowed[from] {
		if t == to {
			return true
		}
	}
	return false
}
