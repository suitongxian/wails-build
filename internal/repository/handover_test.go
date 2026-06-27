package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// 准备：立项 + 让一个底账进入 registered（可过户态）
func setupLedgerInRegistered(t *testing.T) (*sqlx.DB, *models.AssetLedger, []int64) {
	t.Helper()
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "operator", DisplayName: "Op"})

	svc := NewProjectInstantiationService(db)
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "过户测试",
		ObjectShortCode:    "HV",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		CreatedByUserID:    u.ID,
		Activate:           true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 拿一个 planned 底账推到 registered
	var ledgerID int64
	if err := db.Get(&ledgerID, `SELECT id FROM asset_ledgers WHERE project_code = ? LIMIT 1`, out.Project.ProjectCode); err != nil {
		t.Fatal(err)
	}
	// 找对应 file_version 一起改
	var fvID int64
	db.Get(&fvID, `SELECT file_version_id FROM asset_ledgers WHERE id = ?`, ledgerID)
	now := time.Now()
	db.Exec(`UPDATE asset_ledgers SET lifecycle_status = 'registered', update_time = ? WHERE id = ?`, now, ledgerID)
	db.Exec(`UPDATE file_versions SET lifecycle_status = 'registered', update_time = ? WHERE id = ?`, now, fvID)

	ledgerRepo := NewAssetLedgerRepository(db)
	l, _ := ledgerRepo.FindByID(ledgerID)
	return db, l, []int64{owner, custodian, security}
}

// V2-7: 保管主体过户成功，asset_ledgers 列被更新，事件写入
func TestHandover_CustodianSuccess(t *testing.T) {
	db, ledger, subs := setupLedgerInRegistered(t)
	custodianFrom := subs[1]

	// 建一个新 subject 作为过户目标
	now := time.Now()
	res, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('S-NEW', '新保管方', 'department', 'active', ?, ?, 0)`, now, now)
	newSubID, _ := res.LastInsertId()

	svc := NewLedgerLifecycleService(db)
	if err := svc.Handover(HandoverInput{
		LedgerID:    ledger.ID,
		SubjectKind: "custodian",
		ToSubjectID: newSubID,
		Reason:      "团队重组",
		OperatorID:  "operator",
	}); err != nil {
		t.Fatal(err)
	}

	// 验证 asset_ledgers.custodian_subject_id 被更新
	var newCustodian int64
	db.Get(&newCustodian, `SELECT custodian_subject_id FROM asset_ledgers WHERE id = ?`, ledger.ID)
	if newCustodian != newSubID {
		t.Errorf("custodian_subject_id should be %d, got %d", newSubID, newCustodian)
	}

	// 事件应当包含 handover，from=旧 custodian，to=新
	var fromID, toID *int64
	var evType, evName string
	db.QueryRowx(`SELECT event_type, event_name, from_subject_id, to_subject_id FROM lifecycle_events
		WHERE ledger_id = ? AND event_type = 'handover' ORDER BY id DESC LIMIT 1`, ledger.ID).
		Scan(&evType, &evName, &fromID, &toID)
	if evType != "handover" {
		t.Errorf("event_type 应为 handover，got %s", evType)
	}
	if !strings.Contains(evName, "保管") {
		t.Errorf("event_name 应含'保管'，got %s", evName)
	}
	if fromID == nil || *fromID != custodianFrom {
		t.Errorf("from_subject_id 应为 %d，got %v", custodianFrom, fromID)
	}
	if toID == nil || *toID != newSubID {
		t.Errorf("to_subject_id 应为 %d，got %v", newSubID, toID)
	}

	// 状态机不变：仍是 registered
	var status string
	db.Get(&status, `SELECT lifecycle_status FROM asset_ledgers WHERE id = ?`, ledger.ID)
	if status != "registered" {
		t.Errorf("过户不应改变 lifecycle_status，应仍为 registered，got %s", status)
	}
}

// V2-7: subject_kind 校验
func TestHandover_InvalidSubjectKind(t *testing.T) {
	db, ledger, _ := setupLedgerInRegistered(t)
	svc := NewLedgerLifecycleService(db)
	err := svc.Handover(HandoverInput{
		LedgerID:    ledger.ID,
		SubjectKind: "unknown",
		ToSubjectID: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "subject_kind") {
		t.Errorf("应当报 subject_kind 错误，got %v", err)
	}
}

// V2-7: from == to 必拒
func TestHandover_SameSubject(t *testing.T) {
	db, ledger, subs := setupLedgerInRegistered(t)
	svc := NewLedgerLifecycleService(db)
	err := svc.Handover(HandoverInput{
		LedgerID:    ledger.ID,
		SubjectKind: "owner",
		ToSubjectID: subs[0], // 与当前 owner 相同
	})
	if err == nil || !strings.Contains(err.Error(), "无需过户") {
		t.Errorf("from==to 应拒绝，got %v", err)
	}
}

// V2-7: planned/sealed 状态不允许过户
func TestHandover_DisallowedStatus(t *testing.T) {
	db, ledger, _ := setupLedgerInRegistered(t)
	now := time.Now()
	res, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('S-X', 'X', 'department', 'active', ?, ?, 0)`, now, now)
	newSubID, _ := res.LastInsertId()

	// 把底账推到 sealed
	db.Exec(`UPDATE asset_ledgers SET lifecycle_status = 'sealed', update_time = ? WHERE id = ?`, now, ledger.ID)

	svc := NewLedgerLifecycleService(db)
	err := svc.Handover(HandoverInput{
		LedgerID:    ledger.ID,
		SubjectKind: "custodian",
		ToSubjectID: newSubID,
		OperatorID:  "operator",
	})
	if err == nil || !strings.Contains(err.Error(), "registered") {
		t.Errorf("sealed 状态应拒绝过户，got %v", err)
	}
}

// V2-7: 目标主体不存在 / 已禁用 必拒
func TestHandover_InvalidTargetSubject(t *testing.T) {
	db, ledger, _ := setupLedgerInRegistered(t)
	svc := NewLedgerLifecycleService(db)
	err := svc.Handover(HandoverInput{
		LedgerID:    ledger.ID,
		SubjectKind: "owner",
		ToSubjectID: 99999, // 不存在
	})
	if err == nil || !strings.Contains(err.Error(), "目标主体") {
		t.Errorf("不存在的 to_subject_id 应拒绝，got %v", err)
	}
}

// V2-7: 过户审计字段 operator_user_id 落库
func TestHandover_AuditUserID(t *testing.T) {
	db, ledger, _ := setupLedgerInRegistered(t)
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "auditor", DisplayName: "Auditor"})

	now := time.Now()
	res, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('S-A', 'A', 'department', 'active', ?, ?, 0)`, now, now)
	newSubID, _ := res.LastInsertId()

	svc := NewLedgerLifecycleService(db)
	if err := svc.Handover(HandoverInput{
		LedgerID:       ledger.ID,
		SubjectKind:    "security",
		ToSubjectID:    newSubID,
		OperatorID:     "auditor",
		OperatorUserID: u.ID,
	}); err != nil {
		t.Fatal(err)
	}

	var uid *int64
	db.Get(&uid, `SELECT operator_user_id FROM lifecycle_events
		WHERE ledger_id = ? AND event_type = 'handover' ORDER BY id DESC LIMIT 1`, ledger.ID)
	if uid == nil || *uid != u.ID {
		t.Errorf("operator_user_id 应为 %d，got %v", u.ID, uid)
	}
}
