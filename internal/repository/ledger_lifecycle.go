package repository

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// LedgerLifecycleService 底账与文件版本生命周期状态机服务
//
// 提供受 ValidStateTransition 守护的状态切换入口：
// 任何状态变更必须走该服务，确保 file_versions / asset_ledgers 状态同步、
// 自动追加 lifecycle_events、并在不合法的状态转换上报错。
type LedgerLifecycleService struct {
	DB         *sqlx.DB
	fvRepo     *FileVersionRepository
	ledgerRepo *AssetLedgerRepository
	eventRepo  *LifecycleEventRepository
	policyRepo *SecurityPolicyRepository
	projRepo   *DataProjectRepository
}

func NewLedgerLifecycleService(db *sqlx.DB) *LedgerLifecycleService {
	return &LedgerLifecycleService{
		DB:         db,
		fvRepo:     NewFileVersionRepository(db),
		ledgerRepo: NewAssetLedgerRepository(db),
		eventRepo:  NewLifecycleEventRepository(db),
		policyRepo: NewSecurityPolicyRepository(db),
		projRepo:   NewDataProjectRepository(db),
	}
}

// TransitionInput 状态切换入参
type TransitionInput struct {
	LedgerID       int64
	ToStatus       string
	OperatorID     string // V1：操作人用户名
	OperatorUserID int64  // V2：users.id
	Reason         string
	ApprovalRef    string
}

// EventTypeForTransition 把目标状态映射到事件类型
//
//	registered -> register
//	in_use     -> use
//	sealed     -> archive
//	destroyed  -> destroy
//	permanent  -> permanent
func EventTypeForTransition(to string) string {
	switch to {
	case "registered":
		return EventRegister
	case "in_use":
		return EventUse
	case "sealed":
		return EventArchive
	case "destroyed":
		return EventDestroy
	case "permanent":
		return EventPermanent
	default:
		return EventChange
	}
}

// EventNameForTransition 中文事件名
func EventNameForTransition(to string) string {
	switch to {
	case "registered":
		return "正式入账"
	case "in_use":
		return "投入使用"
	case "sealed":
		return "归档封存"
	case "destroyed":
		return "销账销毁"
	case "permanent":
		return "永存"
	default:
		return "状态变更"
	}
}

// Transition 切换底账+文件版本状态
//
// 步骤：
//  1. 加载底账与对应文件版本
//  2. 用 ValidStateTransition(from, to) 守护
//  3. 单事务内：UPDATE asset_ledgers + UPDATE file_versions + INSERT lifecycle_events
func (s *LedgerLifecycleService) Transition(in TransitionInput) error {
	ledger, err := s.ledgerRepo.FindByID(in.LedgerID)
	if err != nil {
		return fmt.Errorf("底账不存在: %w", err)
	}
	if !ValidStateTransition(ledger.LifecycleStatus, in.ToStatus) {
		return fmt.Errorf("不允许的状态转换：%s → %s", ledger.LifecycleStatus, in.ToStatus)
	}

	fv, err := s.fvRepo.FindByID(ledger.FileVersionID)
	if err != nil {
		return fmt.Errorf("关联文件版本不存在: %w", err)
	}

	// 项目结项归档后底账状态机不能用户手动驱动
	// 但 ProjectCloseService 内部要把所有底账推到 sealed —— 那条路径不走本服务，安全
	project, projErr := s.projRepo.FindByID(fv.ProjectID)
	if projErr == nil {
		if project.Status == "archived" || project.Status == "cancelled" {
			return fmt.Errorf("项目已 %s（%s），底账状态不可再手动切换", project.Status, project.ProjectCode)
		}
	}

	// !! 在 BEGIN tx 之前完成所有 r.DB 读，避免 SetMaxOpenConns(1) 死锁 !!
	// 切换后重新计算 file_state 对应的安全策略（"就高不就低"基线 + 状态联动）
	preview := *fv
	preview.LifecycleStatus = in.ToStatus
	var policyID *int64
	if project != nil {
		policyID = ResolveSecurityPolicyID(s.policyRepo, project.SensitivityLevel, &preview)
	}

	tx, err := s.DB.Beginx()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now()

	if _, err := tx.Exec(`UPDATE asset_ledgers SET lifecycle_status = ?, update_time = ? WHERE id = ?`,
		in.ToStatus, now, ledger.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE file_versions SET lifecycle_status = ?, security_policy_id = ?, update_time = ? WHERE id = ?`,
		in.ToStatus, policyID, now, fv.ID); err != nil {
		return err
	}

	var reason, approval *string
	if in.Reason != "" {
		reason = &in.Reason
	}
	if in.ApprovalRef != "" {
		approval = &in.ApprovalRef
	}
	var operatorID *string
	if in.OperatorID != "" {
		operatorID = &in.OperatorID
	}
	ledgerID := ledger.ID
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  fv.ID,
		LedgerID:       &ledgerID,
		EventType:      EventTypeForTransition(in.ToStatus),
		EventName:      EventNameForTransition(in.ToStatus),
		OperatorID:     operatorID,
		OperatorUserID: nullableInt64(in.OperatorUserID),
		FromStorageURI: fv.StorageURI,
		ToStorageURI:   fv.StorageURI,
		Reason:         reason,
		ApprovalRef:    approval,
	}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// =============================================================================
// V2-7 过户（Handover）
// =============================================================================

// HandoverInput 过户入参
//
// 一次过户改变某条底账的"三主体"之一：归属/保管/安全。
// 不改变 lifecycle_status（与 Transition 区分），追加 lifecycle_events 记录。
type HandoverInput struct {
	LedgerID       int64
	SubjectKind    string // "owner" / "custodian" / "security"
	ToSubjectID    int64
	Reason         string
	ApprovalRef    string
	OperatorID     string // V1：操作人用户名
	OperatorUserID int64  // V2：users.id（与 OperatorID 并存写入审计）
}

// 三主体之一过户事件名
func handoverEventName(kind string) string {
	switch kind {
	case "owner":
		return "归属主体过户"
	case "custodian":
		return "保管主体过户"
	case "security":
		return "安全主体过户"
	default:
		return "主体过户"
	}
}

// Handover 三主体过户
//
// 规则：
//  1. SubjectKind 必须是 owner/custodian/security 之一
//  2. ToSubjectID 必须是 subjects 表中已启用的主体
//  3. ToSubjectID 不能等于该 kind 当前的主体（无意义过户）
//  4. 底账状态必须是 registered 或 in_use（planned/sealed/destroyed/permanent 不允许）
//  5. 项目已 archived/cancelled 不允许过户
//  6. 单事务：UPDATE asset_ledgers + INSERT lifecycle_events（event_type=handover）
func (s *LedgerLifecycleService) Handover(in HandoverInput) error {
	// 入参校验
	var kindColumn string
	switch in.SubjectKind {
	case "owner":
		kindColumn = "owner_subject_id"
	case "custodian":
		kindColumn = "custodian_subject_id"
	case "security":
		kindColumn = "security_subject_id"
	default:
		return fmt.Errorf("subject_kind 必须是 owner/custodian/security，收到 %q", in.SubjectKind)
	}
	if in.ToSubjectID <= 0 {
		return fmt.Errorf("to_subject_id 无效")
	}

	// 目标主体存在
	var subjectExists int
	if err := s.DB.Get(&subjectExists, `SELECT COUNT(*) FROM subjects WHERE id = ? AND disable = 0 AND status = 'active'`, in.ToSubjectID); err != nil {
		return err
	}
	if subjectExists == 0 {
		return fmt.Errorf("目标主体不存在或已停用: %d", in.ToSubjectID)
	}

	ledger, err := s.ledgerRepo.FindByID(in.LedgerID)
	if err != nil {
		return fmt.Errorf("底账不存在: %w", err)
	}

	// 当前 kind 对应的 from subject
	var fromSubjectID int64
	switch in.SubjectKind {
	case "owner":
		fromSubjectID = ledger.OwnerSubjectID
	case "custodian":
		fromSubjectID = ledger.CustodianSubjectID
	case "security":
		fromSubjectID = ledger.SecuritySubjectID
	}
	if fromSubjectID == in.ToSubjectID {
		return fmt.Errorf("目标主体与当前 %s 主体相同，无需过户", in.SubjectKind)
	}

	// 状态守护：只允许 registered / in_use
	if ledger.LifecycleStatus != "registered" && ledger.LifecycleStatus != "in_use" {
		return fmt.Errorf("底账状态 %s 不允许过户（仅 registered / in_use 可过户）", ledger.LifecycleStatus)
	}

	fv, err := s.fvRepo.FindByID(ledger.FileVersionID)
	if err != nil {
		return fmt.Errorf("关联文件版本不存在: %w", err)
	}

	// 项目状态守护
	project, projErr := s.projRepo.FindByID(fv.ProjectID)
	if projErr == nil && project != nil {
		if project.Status == "archived" || project.Status == "cancelled" {
			return fmt.Errorf("项目已 %s（%s），不允许再过户", project.Status, project.ProjectCode)
		}
	}

	tx, err := s.DB.Beginx()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now()

	// UPDATE asset_ledgers 的对应主体列
	updateSQL := fmt.Sprintf(`UPDATE asset_ledgers SET %s = ?, update_time = ? WHERE id = ?`, kindColumn)
	if _, err := tx.Exec(updateSQL, in.ToSubjectID, now, ledger.ID); err != nil {
		return err
	}

	// 追加 handover 事件
	var reason, approval, operator *string
	if in.Reason != "" {
		reason = &in.Reason
	}
	if in.ApprovalRef != "" {
		approval = &in.ApprovalRef
	}
	if in.OperatorID != "" {
		operator = &in.OperatorID
	}
	var uidPtr *int64
	if in.OperatorUserID > 0 {
		u := in.OperatorUserID
		uidPtr = &u
	}
	ledgerID := ledger.ID
	fromID := fromSubjectID
	toID := in.ToSubjectID
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  fv.ID,
		LedgerID:       &ledgerID,
		EventType:      EventHandover,
		EventName:      handoverEventName(in.SubjectKind),
		OperatorID:     operator,
		OperatorUserID: uidPtr,
		FromSubjectID:  &fromID,
		ToSubjectID:    &toID,
		Reason:         reason,
		ApprovalRef:    approval,
	}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// SearchEventsInput 项目级事件流筛选
type SearchEventsInput struct {
	ProjectCode string
	EventType   string
	StageCode   string
	Limit       int
}

// EventWithContext 事件 + 关键上下文（用于项目级事件流页面）
type EventWithContext struct {
	ID              int64     `db:"id" json:"id"`
	FileVersionID   int64     `db:"file_version_id" json:"file_version_id"`
	LedgerID        *int64    `db:"ledger_id" json:"ledger_id"`
	EventType       string    `db:"event_type" json:"event_type"`
	EventName       string    `db:"event_name" json:"event_name"`
	OperatorID      *string   `db:"operator_id" json:"operator_id"`
	Reason          *string   `db:"reason" json:"reason"`
	ApprovalRef     *string   `db:"approval_ref" json:"approval_ref"`
	CreateTime      time.Time `db:"create_time" json:"create_time"`
	ProjectCode     *string   `db:"project_code" json:"project_code"`
	StageCode       *string   `db:"stage_code" json:"stage_code"`
	FileVersionCode *string   `db:"file_version_code" json:"file_version_code"`
	AssetName       *string   `db:"asset_name" json:"asset_name"`
}

// SearchEventsByProject 列出某项目所有事件（带文件版本/底账上下文）
func (s *LedgerLifecycleService) SearchEventsByProject(in SearchEventsInput) ([]EventWithContext, error) {
	limit := in.Limit
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	q := `SELECT le.id, le.file_version_id, le.ledger_id, le.event_type, le.event_name,
		le.operator_id, le.reason, le.approval_ref, le.create_time,
		al.project_code, al.stage_code, al.file_version_code, al.asset_name
		FROM lifecycle_events le
		LEFT JOIN asset_ledgers al ON al.id = le.ledger_id
		WHERE 1=1`
	args := []interface{}{}
	if in.ProjectCode != "" {
		q += ` AND al.project_code = ?`
		args = append(args, in.ProjectCode)
	}
	if in.StageCode != "" {
		q += ` AND al.stage_code = ?`
		args = append(args, in.StageCode)
	}
	if in.EventType != "" {
		q += ` AND le.event_type = ?`
		args = append(args, in.EventType)
	}
	q += ` ORDER BY le.create_time DESC, le.id DESC LIMIT ?`
	args = append(args, limit)

	var list []EventWithContext
	if err := s.DB.Select(&list, q, args...); err != nil {
		return nil, err
	}
	return list, nil
}
