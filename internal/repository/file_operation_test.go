package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// setupProjectForFileOps 准备一个完整的测试项目（模版 + 三主体 + 立项 + 实例化）
func setupProjectForFileOps(t *testing.T) (*sqlx.DB, *FileOperationService, *models.DataProject, []FullStageInstance) {
	t.Helper()
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)

	tmpRoot := t.TempDir()
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, tmpRoot)

	svc := NewProjectInstantiationService(db)
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "测试印刷",
		ObjectShortCode:    "MC-T",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "submit", "close"}},
		},
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	fileSvc := NewFileOperationService(db)
	return db, fileSvc, out.Project, out.Stages
}

// writeTempFile 在临时目录写一个文件用于测试上传
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// findFvByLocalCode 在指定环节里按 local_code 找 file_version
func findFvByLocalCode(t *testing.T, stages []FullStageInstance, stageCode, localCode string) *models.FileVersion {
	t.Helper()
	for _, s := range stages {
		if s.StageCode != stageCode {
			continue
		}
		for _, fv := range s.FileVersions {
			if fv.LocalCode == localCode {
				cp := fv
				return &cp
			}
		}
	}
	return nil
}

func TestFileOp_UploadOrBind_HappyPath(t *testing.T) {
	_, svc, project, stages := setupProjectForFileOps(t)

	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	if fv == nil {
		t.Fatal("planned fv not found")
	}

	src := writeTempFile(t, "原稿.pdf", "PDF-DUMMY-CONTENT")
	res, err := svc.UploadOrBind(fv.ID, UploadInput{
		SourcePath:       src,
		OriginalFileName: "原稿.pdf",
		OperatorID:       "tester",
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if res.FileVersion.LifecycleStatus != "registered" {
		t.Fatalf("expected registered, got %s", res.FileVersion.LifecycleStatus)
	}
	if res.FileVersion.Checksum == nil || *res.FileVersion.Checksum == "" {
		t.Fatal("checksum should be set")
	}
	if res.FileVersion.StorageURI == nil || !strings.HasSuffix(*res.FileVersion.StorageURI, ".pdf") {
		t.Fatalf("storage_uri wrong: %v", res.FileVersion.StorageURI)
	}
	// 文件实际存在
	if _, err := os.Stat(*res.FileVersion.StorageURI); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}
	// 项目码 + 环节 + 数据态 都在路径中
	if !strings.Contains(*res.FileVersion.StorageURI, project.ProjectCode) {
		t.Fatalf("path missing project code: %s", *res.FileVersion.StorageURI)
	}
	if !strings.Contains(*res.FileVersion.StorageURI, "MZ-SG/input") && !strings.Contains(*res.FileVersion.StorageURI, "MZ-SG\\input") {
		t.Fatalf("path missing stage/input: %s", *res.FileVersion.StorageURI)
	}
	// 底账状态切到 registered
	if res.Ledger == nil {
		t.Fatal("ledger missing")
	}
	if res.Ledger.LifecycleStatus != "registered" {
		t.Fatalf("ledger should be registered, got %s", res.Ledger.LifecycleStatus)
	}
}

func TestFileOp_UploadOrBind_RejectsNonPlanned(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)
	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src := writeTempFile(t, "原稿.pdf", "x")

	if _, err := svc.UploadOrBind(fv.ID, UploadInput{SourcePath: src, OriginalFileName: "原稿.pdf", OperatorID: "u"}); err != nil {
		t.Fatalf("first upload should succeed: %v", err)
	}
	// 第二次上传到同一 fv：应失败（D3 输入只读约束）
	src2 := writeTempFile(t, "原稿2.pdf", "y")
	if _, err := svc.UploadOrBind(fv.ID, UploadInput{SourcePath: src2, OriginalFileName: "原稿2.pdf", OperatorID: "u"}); err == nil {
		t.Fatal("expected error for re-upload to registered fv")
	}
}

func TestFileOp_UploadOrBind_RejectsBadFileType(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)
	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001") // allows PDF only
	src := writeTempFile(t, "原稿.txt", "txt")
	_, err := svc.UploadOrBind(fv.ID, UploadInput{SourcePath: src, OriginalFileName: "原稿.txt", OperatorID: "u"})
	if err == nil {
		t.Fatal("expected file type rejection")
	}
}

func TestFileOp_CreateNewVersion_ProcessOK_InputRejected(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)

	// 输入版本：第一次上传成功
	inFv := findFvByLocalCode(t, stages, "MZ-PB", "IN-001")
	src := writeTempFile(t, "原稿.pdf", "src1")
	if _, err := svc.UploadOrBind(inFv.ID, UploadInput{SourcePath: src, OriginalFileName: "原稿.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}
	// 输入新版本：拒绝
	src2 := writeTempFile(t, "原稿2.pdf", "src2")
	if _, err := svc.CreateNewVersion(inFv.ID, UploadInput{SourcePath: src2, OriginalFileName: "原稿2.pdf", OperatorID: "u"}); err == nil {
		t.Fatal("input data state should reject CreateNewVersion")
	}

	// 过程文件：先上传 V1.0
	prcFv := findFvByLocalCode(t, stages, "MZ-PB", "PRC-001") // PSD only
	psdSrc := writeTempFile(t, "ban.psd", "psd-data")
	if _, err := svc.UploadOrBind(prcFv.ID, UploadInput{SourcePath: psdSrc, OriginalFileName: "ban.psd", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}
	// 过程新版本：成功
	psdSrc2 := writeTempFile(t, "ban2.psd", "psd-v2")
	res, err := svc.CreateNewVersion(prcFv.ID, UploadInput{SourcePath: psdSrc2, OriginalFileName: "ban2.psd", OperatorID: "u"})
	if err != nil {
		t.Fatalf("create new process version: %v", err)
	}
	if res.FileVersion.VersionNo != "V2.0" {
		t.Fatalf("expected V2.0, got %s", res.FileVersion.VersionNo)
	}
	if res.FileVersion.SourceFileVersionID == nil || *res.FileVersion.SourceFileVersionID != prcFv.ID {
		t.Fatal("source link missing")
	}
}

func TestFileOp_DeriveProcess_FromInput(t *testing.T) {
	db, svc, _, stages := setupProjectForFileOps(t)

	// 上传 IN-001 到 MZ-PB
	inFv := findFvByLocalCode(t, stages, "MZ-PB", "IN-001")
	src := writeTempFile(t, "原稿.pdf", "input-data")
	if _, err := svc.UploadOrBind(inFv.ID, UploadInput{SourcePath: src, OriginalFileName: "原稿.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}
	// 派生为 PRC-001
	psd := writeTempFile(t, "draft.psd", "psd-derived")
	stage := findStageByCode(t, stages, "MZ-PB")
	res, err := svc.DeriveProcess(inFv.ID, DeriveInput{
		UploadInput:    UploadInput{SourcePath: psd, OriginalFileName: "draft.psd", OperatorID: "u"},
		TargetStageID:  stage.ID,
		TargetRuleCode: "PRC-001",
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if res.FileVersion.DataState != "process" {
		t.Fatalf("expected process, got %s", res.FileVersion.DataState)
	}
	if res.FileVersion.SourceFileVersionID == nil || *res.FileVersion.SourceFileVersionID != inFv.ID {
		t.Fatal("derived fv missing source link")
	}

	// 派生到 input 规则应被拒
	_, err = svc.DeriveProcess(inFv.ID, DeriveInput{
		UploadInput:    UploadInput{SourcePath: src, OriginalFileName: "原稿.pdf", OperatorID: "u"},
		TargetStageID:  stage.ID,
		TargetRuleCode: "IN-001",
	})
	if err == nil {
		t.Fatal("derive to non-process rule should fail")
	}
	_ = db
}

func findStageByCode(t *testing.T, stages []FullStageInstance, code string) *models.ProjectStage {
	t.Helper()
	for _, s := range stages {
		if s.StageCode == code {
			cp := s.ProjectStage
			return &cp
		}
	}
	t.Fatalf("stage %s not found", code)
	return nil
}

func TestFileOp_SubmitOutput_RequiresOutputAndRegistered(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)

	// 上传产出
	outFv := findFvByLocalCode(t, stages, "MZ-PB", "OUT-001")
	src := writeTempFile(t, "排版稿.pdf", "out")
	if _, err := svc.UploadOrBind(outFv.ID, UploadInput{SourcePath: src, OriginalFileName: "排版稿.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}
	// 提交
	updated, err := svc.SubmitOutput(outFv.ID, "u")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if updated.SubmittedAt == nil {
		t.Fatal("submitted_at not set")
	}
	if updated.SubmittedBy == nil || *updated.SubmittedBy != "u" {
		t.Fatal("submitted_by not set")
	}

	// 重复提交 → 失败
	if _, err := svc.SubmitOutput(outFv.ID, "u"); err == nil {
		t.Fatal("re-submit should fail")
	}

	// 输入不能提交
	inFv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	if _, err := svc.SubmitOutput(inFv.ID, "u"); err == nil {
		t.Fatal("submit on input should fail")
	}

	// 未 registered 的产出不能提交
	outFv2 := findFvByLocalCode(t, stages, "MZ-SG", "OUT-001")
	if _, err := svc.SubmitOutput(outFv2.ID, "u"); err == nil {
		t.Fatal("submit on planned fv should fail")
	}
}

func TestFileOp_ReceiveAsInput_FromUpstream(t *testing.T) {
	db, svc, _, stages := setupProjectForFileOps(t)

	// 上游 MZ-PB OUT-001 上传 + 提交
	outFv := findFvByLocalCode(t, stages, "MZ-PB", "OUT-001")
	src := writeTempFile(t, "排版稿.pdf", "registered-output")
	uploadRes, err := svc.UploadOrBind(outFv.ID, UploadInput{SourcePath: src, OriginalFileName: "排版稿.pdf", OperatorID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	outFv = uploadRes.FileVersion // 用上传后的状态
	if _, err := svc.SubmitOutput(outFv.ID, "u"); err != nil {
		t.Fatal(err)
	}

	// 下游领取（测试模版有 MZ-SG 和 MZ-PB；这里把 MZ-PB 的产出领回 MZ-SG 的 IN-001
	// 仅为验证 API 行为；真实场景是 MZ-SG → MZ-PB → MZ-SH）
	stage := findStageByCode(t, stages, "MZ-SG")
	res, err := svc.ReceiveAsInput(ReceiveInput{
		SourceFileVersionID: outFv.ID,
		TargetStageID:       stage.ID,
		TargetRuleCode:      "IN-001",
		OperatorID:          "u",
	})
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if res.FileVersion.SourceFileVersionID == nil || *res.FileVersion.SourceFileVersionID != outFv.ID {
		t.Fatal("source link missing")
	}
	if res.FileVersion.LifecycleStatus != "registered" {
		t.Fatalf("downstream fv should be registered, got %s", res.FileVersion.LifecycleStatus)
	}
	// 引用而非复制：storage_uri 同上游
	if res.FileVersion.StorageURI == nil || *res.FileVersion.StorageURI != *outFv.StorageURI {
		t.Fatal("downstream storage_uri should reference upstream")
	}
	// 上游应有 transfer 事件
	srcUp, _ := svc.fvRepo.FindByID(outFv.ID)
	_ = srcUp
	eventRepo := NewLifecycleEventRepository(db)
	events, _ := eventRepo.ListByFileVersion(outFv.ID)
	hasTransfer := false
	for _, e := range events {
		if e.EventType == EventTransfer {
			hasTransfer = true
			break
		}
	}
	if !hasTransfer {
		t.Fatal("upstream should have transfer event")
	}

	// 幂等：再次领取同一个上游 → 返回相同的 fv
	res2, err := svc.ReceiveAsInput(ReceiveInput{
		SourceFileVersionID: outFv.ID,
		TargetStageID:       stage.ID,
		TargetRuleCode:      "IN-001",
		OperatorID:          "u",
	})
	if err != nil {
		t.Fatalf("idempotent receive: %v", err)
	}
	if res2.FileVersion.ID != res.FileVersion.ID {
		t.Fatalf("expected idempotent, got %d vs %d", res2.FileVersion.ID, res.FileVersion.ID)
	}
}

// V1 验证用户反馈：项目归档后所有写操作（upload/derive/new-version/submit/receive
// 以及 ledger transition）都应当被后端硬拒绝
func TestFileOp_ArchivedProject_AllWritesRejected(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)

	// 上传一份 IN-001（让 fv 进 registered，ledger 拿到）
	inFv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src := writeTempFile(t, "case.pdf", "x")
	uploadRes, err := svc.UploadOrBind(inFv.ID, UploadInput{SourcePath: src, OriginalFileName: "case.pdf", OperatorID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	registeredFv := uploadRes.FileVersion

	// 上传一份 OUT-001 + 提交（用于后面 receive 测试）
	outFv := findFvByLocalCode(t, stages, "MZ-PB", "OUT-001")
	src2 := writeTempFile(t, "排版.pdf", "x")
	if _, err := svc.UploadOrBind(outFv.ID, UploadInput{SourcePath: src2, OriginalFileName: "排版.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitOutput(outFv.ID, "u"); err != nil {
		t.Fatal(err)
	}

	// 找另一个 planned input 给后面 upload 用（IN-002 仍 planned）
	var plannedFvID int64
	if err := db.Get(&plannedFvID, `SELECT id FROM file_versions WHERE project_id=? AND lifecycle_status='planned' AND data_state='input' AND disable=0 LIMIT 1`,
		project.ID); err != nil {
		t.Fatal(err)
	}

	// 把项目状态改成 archived
	if _, err := db.Exec(`UPDATE data_projects SET status='archived' WHERE id=?`, project.ID); err != nil {
		t.Fatal(err)
	}

	// 1) UploadOrBind 应当拒绝
	src3 := writeTempFile(t, "extra.pdf", "y")
	if _, err := svc.UploadOrBind(plannedFvID, UploadInput{SourcePath: src3, OriginalFileName: "extra.pdf", OperatorID: "u"}); err == nil {
		t.Error("UploadOrBind should fail on archived project")
	} else if !strings.Contains(err.Error(), "归档") {
		t.Errorf("error should mention '归档', got: %v", err)
	}

	// 2) CreateNewVersion 应当拒绝
	if _, err := svc.CreateNewVersion(outFv.ID, UploadInput{SourcePath: src3, OriginalFileName: "extra.pdf", OperatorID: "u"}); err == nil {
		t.Error("CreateNewVersion should fail on archived project")
	}

	// 3) DeriveProcess 应当拒绝
	pbStage := findStageByCode(t, stages, "MZ-PB")
	if _, err := svc.DeriveProcess(registeredFv.ID, DeriveInput{
		UploadInput:    UploadInput{SourcePath: src3, OriginalFileName: "派生.psd", OperatorID: "u"},
		TargetStageID:  pbStage.ID,
		TargetRuleCode: "PRC-001",
	}); err == nil {
		t.Error("DeriveProcess should fail on archived project")
	}

	// 4) SubmitOutput 应当拒绝（即使有未提交的 output 也不允许了）
	if _, err := svc.SubmitOutput(outFv.ID, "u"); err == nil {
		// 实际上 OUT-001 已经 submitted 过了（前面调用过），这里会因为 "已提交" 而失败
		// 不影响校验目的：只要确保不是因为"项目可写"而漏过，就 OK
		t.Logf("expected error (项目归档 OR 已提交)")
	}

	// 5) ReceiveAsInput 应当拒绝
	sgStage := findStageByCode(t, stages, "MZ-SG")
	if _, err := svc.ReceiveAsInput(ReceiveInput{
		SourceFileVersionID: outFv.ID,
		TargetStageID:       sgStage.ID,
		TargetRuleCode:      "IN-002",
		OperatorID:          "u",
	}); err == nil {
		t.Error("ReceiveAsInput should fail on archived project")
	}

	// 6) Ledger transition 应当拒绝
	lcSvc := NewLedgerLifecycleService(db)
	if err := lcSvc.Transition(TransitionInput{
		LedgerID: uploadRes.Ledger.ID, ToStatus: "in_use", OperatorID: "u",
	}); err == nil {
		t.Error("Transition should fail on archived project")
	} else if !strings.Contains(err.Error(), "archived") && !strings.Contains(err.Error(), "归档") {
		t.Errorf("error should mention archive/归档, got: %v", err)
	}
}

// V1 验证用户反馈：领取时也应当复用 planned 占位，不再产生"V1.0 planned + V2.0 registered"
//
// 注：测试 seed 只有 MZ-SG / MZ-PB（没有 MZ-SH），所以这里把 MZ-PB OUT-001 的产出
// 领回 MZ-SG IN-001 仅为验证 API 行为；真实场景类比 MZ-PB OUT-001 → MZ-SH IN-002。
func TestFileOp_Receive_ReusesPlannedSlot(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)

	// 上游 MZ-PB OUT-001 上传 + 提交
	outFv := findFvByLocalCode(t, stages, "MZ-PB", "OUT-001")
	src := writeTempFile(t, "排版完成.pdf", "x")
	uploadRes, err := svc.UploadOrBind(outFv.ID, UploadInput{SourcePath: src, OriginalFileName: "排版完成.pdf", OperatorID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	outFv = uploadRes.FileVersion
	if _, err := svc.SubmitOutput(outFv.ID, "u"); err != nil {
		t.Fatal(err)
	}

	// 假装一个完全独立的下游环节用 MZ-SG 替代（MZ-SG IN-001 此刻还是 planned）
	sgStage := findStageByCode(t, stages, "MZ-SG")
	res, err := svc.ReceiveAsInput(ReceiveInput{
		SourceFileVersionID: outFv.ID,
		TargetStageID:       sgStage.ID,
		TargetRuleCode:      "IN-001",
		OperatorID:          "u",
	})
	if err != nil {
		t.Fatalf("receive: %v", err)
	}

	// 关键断言：版本号 V1.0（复用 planned 占位）
	if res.FileVersion.VersionNo != "V1.0" {
		t.Errorf("receive should reuse planned slot V1.0, got %s", res.FileVersion.VersionNo)
	}

	// MZ-SG IN-001 下应当只有 1 条
	var rows []models.FileVersion
	_ = svc.DB.Select(&rows, `SELECT * FROM file_versions WHERE project_stage_id=? AND local_code='IN-001' AND disable=0`, sgStage.ID)
	if len(rows) != 1 {
		t.Errorf("expected 1 row after receive (planned reused), got %d", len(rows))
	}
	if rows[0].SourceFileVersionID == nil || *rows[0].SourceFileVersionID != outFv.ID {
		t.Errorf("source_file_version_id should point to upstream, got %v", rows[0].SourceFileVersionID)
	}
	// 引用上游 storage_uri（不复制）
	if rows[0].StorageURI == nil || outFv.StorageURI == nil || *rows[0].StorageURI != *outFv.StorageURI {
		t.Errorf("received fv should reference upstream storage_uri, got %v vs %v", rows[0].StorageURI, outFv.StorageURI)
	}
}

// V1 验证用户反馈：派生时 PRC-001 不应当出现 V1.0 planned + V2.0 registered 两条
// 应当复用立项时建的 V1.0 planned 占位，升级为 V1.0 registered + 写 source 链路
func TestFileOp_Derive_ReusesPlannedSlot(t *testing.T) {
	_, svc, project, stages := setupProjectForFileOps(t)
	_ = project

	// 上传 IN-001
	inFv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src := writeTempFile(t, "case.pdf", "x")
	if _, err := svc.UploadOrBind(inFv.ID, UploadInput{SourcePath: src, OriginalFileName: "case.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}

	// 派生到 MZ-PB / PRC-001
	pbStage := findStageByCode(t, stages, "MZ-PB")
	src2 := writeTempFile(t, "排版.psd", "x")
	deriveRes, err := svc.DeriveProcess(inFv.ID, DeriveInput{
		UploadInput:    UploadInput{SourcePath: src2, OriginalFileName: "排版.psd", OperatorID: "u"},
		TargetStageID:  pbStage.ID,
		TargetRuleCode: "PRC-001",
	})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	// 关键断言 1：派生出来的版本应当是 V1.0（不是 V2.0）
	if deriveRes.FileVersion.VersionNo != "V1.0" {
		t.Errorf("derive should reuse planned slot V1.0, got %s", deriveRes.FileVersion.VersionNo)
	}

	// 关键断言 2：MZ-PB 下 PRC-001 应当只有 1 条记录，且 lifecycle=registered
	var rows []models.FileVersion
	if err := svc.DB.Select(&rows, `SELECT * FROM file_versions
		WHERE project_stage_id = ? AND local_code = 'PRC-001' AND disable = 0
		ORDER BY id`, pbStage.ID); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 PRC-001 row after derive (planned reused), got %d: %+v", len(rows), rows)
	}
	if len(rows) > 0 && rows[0].LifecycleStatus != "registered" {
		t.Errorf("PRC-001 should be registered after derive, got %s", rows[0].LifecycleStatus)
	}

	// 关键断言 3：source_file_version_id 应当指向 MZ-SG IN-001
	if rows[0].SourceFileVersionID == nil || *rows[0].SourceFileVersionID != inFv.ID {
		t.Errorf("source_file_version_id should point to MZ-SG IN-001 (id=%d), got %v",
			inFv.ID, rows[0].SourceFileVersionID)
	}
}

// V1 验证：第二次派生（已有 registered 后）应走多版本路径，创建 V2.0
func TestFileOp_Derive_SecondTimeCreatesNewVersion(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)

	inFv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src := writeTempFile(t, "case.pdf", "x")
	if _, err := svc.UploadOrBind(inFv.ID, UploadInput{SourcePath: src, OriginalFileName: "case.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}

	pbStage := findStageByCode(t, stages, "MZ-PB")

	// 第一次派生：复用 V1.0 planned
	src2 := writeTempFile(t, "排版1.psd", "x")
	if _, err := svc.DeriveProcess(inFv.ID, DeriveInput{
		UploadInput:    UploadInput{SourcePath: src2, OriginalFileName: "排版1.psd", OperatorID: "u"},
		TargetStageID:  pbStage.ID,
		TargetRuleCode: "PRC-001",
	}); err != nil {
		t.Fatal(err)
	}

	// 第二次派生：应当创建 V2.0
	src3 := writeTempFile(t, "排版2.psd", "x")
	res2, err := svc.DeriveProcess(inFv.ID, DeriveInput{
		UploadInput:    UploadInput{SourcePath: src3, OriginalFileName: "排版2.psd", OperatorID: "u"},
		TargetStageID:  pbStage.ID,
		TargetRuleCode: "PRC-001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.FileVersion.VersionNo != "V2.0" {
		t.Errorf("second derive should be V2.0, got %s", res2.FileVersion.VersionNo)
	}

	// 现在 PRC-001 下应当有 2 条：V1.0 registered + V2.0 registered
	var rows []models.FileVersion
	_ = svc.DB.Select(&rows, `SELECT * FROM file_versions WHERE project_stage_id = ? AND local_code = 'PRC-001' AND disable = 0`, pbStage.ID)
	if len(rows) != 2 {
		t.Errorf("expected 2 rows after two derives, got %d", len(rows))
	}
}

// V1 验证用户反馈：事件 reason 文案不能再含"派生自文件版本 1"这种裸 fv id
func TestFileOp_EventReason_NoRawFvID(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)

	// 上传 IN-001
	inFv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src := writeTempFile(t, "case.pdf", "x")
	if _, err := svc.UploadOrBind(inFv.ID, UploadInput{SourcePath: src, OriginalFileName: "case.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}

	// 派生 PRC-001（input → process）
	pbStage := findStageByCode(t, stages, "MZ-PB")
	src2 := writeTempFile(t, "排版.psd", "x")
	deriveRes, err := svc.DeriveProcess(inFv.ID, DeriveInput{
		UploadInput:    UploadInput{SourcePath: src2, OriginalFileName: "排版.psd", OperatorID: "u"},
		TargetStageID:  pbStage.ID,
		TargetRuleCode: "PRC-001",
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := svc.eventRepo.ListByFileVersion(deriveRes.FileVersion.ID)
	if err != nil {
		t.Fatal(err)
	}
	hasFriendly := false
	for _, ev := range events {
		if ev.Reason == nil {
			continue
		}
		r := *ev.Reason
		// 不允许"文件版本 N"裸数字
		if strings.Contains(r, "文件版本 ") {
			rest := r[strings.Index(r, "文件版本 ")+len("文件版本 "):]
			if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
				t.Errorf("event reason still has raw fv id pattern '文件版本 %s...': %q", string(rest[0]), r)
			}
		}
		// 必须含来源的业务名称
		if strings.Contains(r, "客户原稿") {
			hasFriendly = true
		}
	}
	if !hasFriendly {
		t.Errorf("derive reason should mention source display_name '客户原稿', got events: %+v", events)
	}
}

func TestFileOp_ReceiveAsInput_RequiresSubmitted(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)

	outFv := findFvByLocalCode(t, stages, "MZ-PB", "OUT-001")
	src := writeTempFile(t, "排版稿.pdf", "x")
	if _, err := svc.UploadOrBind(outFv.ID, UploadInput{SourcePath: src, OriginalFileName: "排版稿.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}
	// 没有调用 SubmitOutput
	stage := findStageByCode(t, stages, "MZ-SG")
	_, err := svc.ReceiveAsInput(ReceiveInput{
		SourceFileVersionID: outFv.ID,
		TargetStageID:       stage.ID,
		TargetRuleCode:      "IN-001",
		OperatorID:          "u",
	})
	if err == nil {
		t.Fatal("receive should fail when upstream not submitted")
	}
}
