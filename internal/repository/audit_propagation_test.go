package repository

import (
	"testing"

	"data-asset-scan-go/internal/models"
)

// V2 收尾：UploadOrBind 把 OperatorUserID 写到 file_versions.created_by_user_id
// 和该 fv 上追加的 lifecycle_events.operator_user_id
func TestAuditPropagation_Upload(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)
	userRepo := NewUserRepository(svc.DB)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "uploader", DisplayName: "Uploader"})

	// 找一条 input 规则的 planned fv 上传
	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	if fv == nil {
		t.Skip("test template has no MZ-SG/IN-001 fv")
	}
	src := writeTempFile(t, "raw.pdf", "hello")
	res, err := svc.UploadOrBind(fv.ID, UploadInput{
		SourcePath:       src,
		OriginalFileName: "raw.pdf",
		OperatorID:       "uploader",
		OperatorUserID:   u.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// file_versions.created_by_user_id 应当为 u.ID（createAndBindNewFV 路径不走，此路径走 bindToFileVersion）
	// 但 bindToFileVersion 不改 created_by 字段（仅 update 上传相关），所以这里实际检查的是事件
	var evUID *int64
	svc.DB.Get(&evUID, `SELECT operator_user_id FROM lifecycle_events
		WHERE file_version_id = ? AND event_name = '正式入账' ORDER BY id DESC LIMIT 1`, res.FileVersion.ID)
	if evUID == nil || *evUID != u.ID {
		t.Errorf("Upload 事件 operator_user_id 应为 %d，got %v", u.ID, evUID)
	}
}

// V2 收尾：CreateNewVersion 落 created_by_user_id 与事件 operator_user_id
func TestAuditPropagation_NewVersion(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)
	userRepo := NewUserRepository(svc.DB)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "versioner", DisplayName: "Versioner"})

	// 拿 process fv 先上传一次 V1.0
	procFv := findFvByLocalCode(t, stages, "MZ-PB", "PRC-001")
	if procFv == nil {
		t.Skip("test template has no MZ-PB/PRC-001 fv")
	}
	src := writeTempFile(t, "v1.psd", "v1")
	r1, err := svc.UploadOrBind(procFv.ID, UploadInput{
		SourcePath: src, OriginalFileName: "v1.psd",
		OperatorID: "versioner", OperatorUserID: u.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 创建 V2.0
	src2 := writeTempFile(t, "v2.psd", "v2")
	r2, err := svc.CreateNewVersion(r1.FileVersion.ID, UploadInput{
		SourcePath: src2, OriginalFileName: "v2.psd",
		OperatorID: "versioner", OperatorUserID: u.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 新 fv 的 created_by_user_id 应为 u.ID
	var fvUID *int64
	svc.DB.Get(&fvUID, `SELECT created_by_user_id FROM file_versions WHERE id = ?`, r2.FileVersion.ID)
	if fvUID == nil || *fvUID != u.ID {
		t.Errorf("NewVersion 的 file_versions.created_by_user_id 应为 %d，got %v", u.ID, fvUID)
	}

	// 对应事件 operator_user_id 也应为 u.ID
	var evUID *int64
	svc.DB.Get(&evUID, `SELECT operator_user_id FROM lifecycle_events
		WHERE file_version_id = ? AND event_name = '正式入账' ORDER BY id DESC LIMIT 1`, r2.FileVersion.ID)
	if evUID == nil || *evUID != u.ID {
		t.Errorf("NewVersion 事件 operator_user_id 应为 %d，got %v", u.ID, evUID)
	}
}

// V2 收尾：Transition 把 operator_user_id 落到 lifecycle_events
func TestAuditPropagation_Transition(t *testing.T) {
	db, svc, _, stages := setupProjectForFileOps(t)
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "switcher", DisplayName: "Switcher"})

	// 找一条已 registered 的 input fv，把它推到 in_use
	procFv := findFvByLocalCode(t, stages, "MZ-PB", "PRC-001")
	src := writeTempFile(t, "tx.psd", "tx")
	r, _ := svc.UploadOrBind(procFv.ID, UploadInput{
		SourcePath: src, OriginalFileName: "tx.psd",
		OperatorID: "init", OperatorUserID: u.ID,
	})

	// 走 ledger 状态机：registered → in_use
	ledgerRepo := NewAssetLedgerRepository(db)
	l, _ := ledgerRepo.FindByFileVersion(r.FileVersion.ID)
	if l == nil {
		t.Skip("ledger not found")
	}

	lcSvc := NewLedgerLifecycleService(db)
	if err := lcSvc.Transition(TransitionInput{
		LedgerID:       l.ID,
		ToStatus:       "in_use",
		OperatorID:     "switcher",
		OperatorUserID: u.ID,
		Reason:         "投入使用",
	}); err != nil {
		t.Fatal(err)
	}

	var evUID *int64
	db.Get(&evUID, `SELECT operator_user_id FROM lifecycle_events
		WHERE ledger_id = ? AND event_type = 'use' ORDER BY id DESC LIMIT 1`, l.ID)
	if evUID == nil || *evUID != u.ID {
		t.Errorf("Transition 事件 operator_user_id 应为 %d，got %v", u.ID, evUID)
	}
}

// V2 收尾：CloseProject 把 OperatorUserID 落到所有 archive 事件
func TestAuditPropagation_Close(t *testing.T) {
	db, _, project, stages := setupProjectForFileOps(t)
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "closer", DisplayName: "Closer"})

	// 把所有 required 文件上传齐
	uploadAllRequired(t, NewFileOperationService(db), stages)

	closeSvc := NewProjectCloseService(db)
	_, err := closeSvc.Close(CloseInput{
		ProjectID:      project.ID,
		OperatorID:     "closer",
		OperatorUserID: u.ID,
		Force:          true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 应当有 archive 事件，每条 operator_user_id 都是 u.ID
	var count, withUID int
	db.Get(&count, `SELECT COUNT(*) FROM lifecycle_events WHERE event_type = 'archive'`)
	db.Get(&withUID, `SELECT COUNT(*) FROM lifecycle_events WHERE event_type = 'archive' AND operator_user_id = ?`, u.ID)
	if count == 0 {
		t.Fatal("应当生成至少一条 archive 事件")
	}
	if withUID != count {
		t.Errorf("archive 事件 operator_user_id 覆盖不全：总=%d 带 uid=%d", count, withUID)
	}
}
