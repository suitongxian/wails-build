package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// FileOperationService 文件版本操作服务
//
// 涵盖 D2-D5：上传/绑定 + 类型校验 + checksum + 命名 + 输入只读约束 +
// 派生过程文件 + 提交产出 + 下游手工领取为输入。
type FileOperationService struct {
	DB         *sqlx.DB
	tplCache   *TemplateCacheRepository
	projRepo   *DataProjectRepository
	stageRepo  *ProjectStageRepository
	fvRepo     *FileVersionRepository
	ledgerRepo *AssetLedgerRepository
	eventRepo  *LifecycleEventRepository
	policyRepo *SecurityPolicyRepository
	userInfo   *UserInfoRepository
}

func NewFileOperationService(db *sqlx.DB) *FileOperationService {
	return &FileOperationService{
		DB:         db,
		tplCache:   NewTemplateCacheRepository(db),
		projRepo:   NewDataProjectRepository(db),
		stageRepo:  NewProjectStageRepository(db),
		fvRepo:     NewFileVersionRepository(db),
		ledgerRepo: NewAssetLedgerRepository(db),
		eventRepo:  NewLifecycleEventRepository(db),
		policyRepo: NewSecurityPolicyRepository(db),
		userInfo:   NewUserInfoRepository(db),
	}
}

// UploadInput 上传/绑定通用入参
//
// SourcePath：源文件在本机的绝对路径。服务会把它"复制"到项目存储位置（不修改原文件）。
// OriginalFileName：原始文件名（含扩展名），用于扩展名校验和命名 {original} 变量。
type UploadInput struct {
	SourcePath       string
	OriginalFileName string
	OperatorID       string            // V1：操作人用户名字符串
	OperatorUserID   int64             // V2：users.id（与 OperatorID 并存写入审计字段）
	Extras           map[string]string // 业务自定义命名变量
}

// UploadResult 上传结果
type UploadResult struct {
	FileVersion *models.FileVersion `json:"file_version"`
	Ledger      *models.AssetLedger `json:"ledger"`
	StoragePath string              `json:"storage_path"`
}

// =============================================================================
// D2 上传/绑定（首次绑定到 planned 文件版本）
// =============================================================================

// UploadOrBind 把源文件绑定到一个 planned 状态的文件版本
//
// 约束：
//   - 目标文件版本必须是 planned 状态（防止重复上传，D3 输入只读约束的核心）
//   - 文件扩展名必须在模版规则的 allowed_file_types 列表中
//   - 写入 storage_uri / checksum / file_size / file_type，状态切到 registered
//   - 同步底账状态、追加 register 生命周期事件
func (s *FileOperationService) UploadOrBind(fvID int64, in UploadInput) (*UploadResult, error) {
	log.Printf("[upload-svc] fv=%d UploadOrBind 开始", fvID)
	fv, err := s.fvRepo.FindByID(fvID)
	if err != nil {
		return nil, fmt.Errorf("文件版本不存在: %w", err)
	}
	if err := s.assertProjectMutable(fv.ProjectID); err != nil {
		return nil, err
	}
	if err := s.assertStageMutable(fv.ProjectStageID); err != nil {
		return nil, err
	}
	if fv.LifecycleStatus != "planned" {
		return nil, fmt.Errorf("文件版本当前状态为 %s，不能再次上传；如需修改：输入文件请派生为过程文件，过程/产出请创建新版本", fv.LifecycleStatus)
	}
	log.Printf("[upload-svc] fv=%d 状态校验通过 进入 bindToFileVersion", fvID)
	return s.bindToFileVersion(fv, in)
}

// =============================================================================
// D2 多版本：在同一文件规则下创建新版本
// =============================================================================

// CreateNewVersion 在同一规则下创建新版本（V1.0 → V2.0）
//
// 约束：
//   - 不能在 input 规则下创建新版本（输入只读，需派生为过程文件）
//   - 来源 fv 必须存在且属于同一规则
//   - 新 fv 的 source_file_version_id 指向 prevFvID，建立版本链
func (s *FileOperationService) CreateNewVersion(prevFvID int64, in UploadInput) (*UploadResult, error) {
	prev, err := s.fvRepo.FindByID(prevFvID)
	if err != nil {
		return nil, fmt.Errorf("源文件版本不存在: %w", err)
	}
	if err := s.assertProjectMutable(prev.ProjectID); err != nil {
		return nil, err
	}
	if err := s.assertStageMutable(prev.ProjectStageID); err != nil {
		return nil, err
	}
	if prev.DataState == "input" {
		return nil, fmt.Errorf("输入数据为只读：如需修改，请派生为过程文件（POST /file-versions/%d/derive）", prevFvID)
	}

	// 根据现有版本数生成新版本号
	count, err := s.fvRepo.CountByStageRule(prev.ProjectStageID, prev.LocalCode)
	if err != nil {
		return nil, err
	}
	newVersion := fmt.Sprintf("V%d.0", count+1)

	stage, err := s.stageRepo.FindByID(prev.ProjectStageID)
	if err != nil {
		return nil, err
	}
	project, err := s.projRepo.FindByID(prev.ProjectID)
	if err != nil {
		return nil, err
	}

	rule, err := s.findRuleForFV(prev)
	if err != nil {
		return nil, err
	}

	return s.createAndBindNewFV(project, stage, rule, prev.DataState, newVersion, &prevFvID, in)
}

// =============================================================================
// D4 派生过程文件（A: 从输入派生 / B: 手工新建过程，附 source）
// =============================================================================

// DeriveInput 派生入参
type DeriveInput struct {
	UploadInput
	TargetStageID   int64  // 目标环节（通常是当前环节，也可以是其他环节）
	TargetRuleCode  string // 目标规则（必须是 PRC 数据态）
	TargetVersionNo string // 留空使用 V1.0 / 现存 +1
}

// DeriveProcess 从一个 file_version 派生出新的过程文件
//
// 用例：
//   - 模式 A：在输入 fv 上点"派生过程"，选目标 PRC 规则，记录 source_file_version_id
//   - 模式 B：手工新建过程，选父级（输入或前一过程版本），上传/绑定
//
// 两种都建立 source_file_version_id 链路。
func (s *FileOperationService) DeriveProcess(sourceFvID int64, in DeriveInput) (*UploadResult, error) {
	source, err := s.fvRepo.FindByID(sourceFvID)
	if err != nil {
		return nil, fmt.Errorf("源文件版本不存在: %w", err)
	}
	if err := s.assertProjectMutable(source.ProjectID); err != nil {
		return nil, err
	}
	// V3-UI option C: 目标环节必须可变（completed/skipped 拒）
	if err := s.assertStageMutable(in.TargetStageID); err != nil {
		return nil, err
	}
	stage, err := s.stageRepo.FindByID(in.TargetStageID)
	if err != nil {
		return nil, fmt.Errorf("目标环节不存在: %w", err)
	}
	if stage.ProjectID != source.ProjectID {
		return nil, fmt.Errorf("目标环节与源文件不属于同一项目")
	}

	project, err := s.projRepo.FindByID(stage.ProjectID)
	if err != nil {
		return nil, err
	}

	// 在目标环节下找到对应的 PRC 规则
	rule, err := s.findRuleByCode(project, stage, in.TargetRuleCode)
	if err != nil {
		return nil, err
	}
	if rule.DataState != "process" {
		return nil, fmt.Errorf("派生只能在过程数据态规则下进行，目标规则 %s 数据态为 %s", rule.FileRuleCode, rule.DataState)
	}

	src := sourceFvID

	// V1 修正：如果目标规则下有立项时建的 planned 占位，**复用它**而不是另起 V2.0
	// （立项时已为每条 process/output 规则预创建 V1.0 planned，UI 上看到的占位）
	if planned := s.findPlannedByRule(stage.ID, rule.ID); planned != nil {
		// 复用 planned slot：升级为 registered + 写 source 链路
		return s.bindPlannedWithSource(planned, &src, in.UploadInput, project, stage, rule)
	}

	// 没有 planned 占位：常规多版本（V2.0/V3.0...）
	versionNo := strings.TrimSpace(in.TargetVersionNo)
	if versionNo == "" {
		count, err := s.fvRepo.CountByStageRule(stage.ID, rule.FileRuleCode)
		if err != nil {
			return nil, err
		}
		versionNo = fmt.Sprintf("V%d.0", count+1)
	}
	return s.createAndBindNewFV(project, stage, rule, "process", versionNo, &src, in.UploadInput)
}

// findPlannedByRule 在指定环节+规则下找一条 planned 占位 fv
//
// 立项时每条规则都会建一条 V1.0 planned；后续 derive/new-version 操作会消耗它。
// 找不到时返回 nil（不报错），调用方决定是否走多版本路径。
func (s *FileOperationService) findPlannedByRule(stageID, ruleID int64) *models.FileVersion {
	var fv models.FileVersion
	if err := s.DB.Get(&fv, `SELECT * FROM file_versions
		WHERE project_stage_id = ? AND template_file_rule_id = ? AND lifecycle_status = 'planned' AND disable = 0
		ORDER BY id LIMIT 1`, stageID, ruleID); err != nil {
		return nil
	}
	return &fv
}

// =============================================================================
// D5 提交产出
// =============================================================================

// SubmitOutput 标记产出文件版本为已提交（下游可见可领取）。
//
// V2：可选 operatorUserID（可变参数）传入 users.id，与 V1 的 operatorID 字符串并存写入审计字段。
// 测试和老调用方仍可只传 operatorID。
func (s *FileOperationService) SubmitOutput(fvID int64, operatorID string, operatorUserID ...int64) (*models.FileVersion, error) {
	fv, err := s.fvRepo.FindByID(fvID)
	if err != nil {
		return nil, err
	}
	if err := s.assertProjectMutable(fv.ProjectID); err != nil {
		return nil, err
	}
	if err := s.assertStageMutable(fv.ProjectStageID); err != nil {
		return nil, err
	}
	if fv.DataState != "output" {
		return nil, fmt.Errorf("提交产出仅对 output 数据态有效，当前 %s", fv.DataState)
	}
	if fv.LifecycleStatus != "registered" {
		return nil, fmt.Errorf("仅 registered 状态的产出可提交，当前 %s", fv.LifecycleStatus)
	}
	if fv.SubmittedAt != nil {
		return nil, fmt.Errorf("该产出已提交（%s）", fv.SubmittedAt.Format(time.RFC3339))
	}

	var uidPtr *int64
	if len(operatorUserID) > 0 && operatorUserID[0] > 0 {
		uid := operatorUserID[0]
		uidPtr = &uid
	}

	now := time.Now()
	if _, err := s.DB.Exec(
		`UPDATE file_versions SET submitted_at = ?, submitted_by = ?, submitted_by_user_id = ?, update_time = ? WHERE id = ?`,
		now, operatorID, uidPtr, now, fvID); err != nil {
		return nil, err
	}

	// 追加 change 事件标记提交
	reason := "提交产出"
	if _, err := s.eventRepo.AppendNoTx(AppendEventInput{
		FileVersionID:  fvID,
		EventType:      EventChange,
		EventName:      "提交产出",
		OperatorID:     &operatorID,
		OperatorUserID: uidPtr,
		Reason:         &reason,
	}); err != nil {
		return nil, err
	}

	// V5-P5：工作数据 output 提交后上报 manage 端部门柜。
	// 上报失败不回滚本地提交，失败状态写回 file_versions 供用户重试。
	_, _ = NewManagedArchiveReporter(s.DB).ReportFileVersionToCabinet(context.Background(), fvID)

	return s.fvRepo.FindByID(fvID)
}

// =============================================================================
// D5 下游领取为输入
// =============================================================================

// ReceiveInput 领取入参
type ReceiveInput struct {
	SourceFileVersionID int64  // 上游已提交的产出 fv id
	TargetStageID       int64  // 下游环节 id
	TargetRuleCode      string // 下游环节下的输入规则编码（IN-XXX）
	OperatorID          string // V1：操作人用户名
	OperatorUserID      int64  // V2：users.id
}

// ReceiveAsInput 把上游提交的产出"领取"为下游环节的输入
//
// 不复制实体文件，只引用 storage_uri。新建的下游 fv：
//   - data_state = input
//   - lifecycle_status = registered（直接引用，无需再次上传）
//   - source_file_version_id 指向上游
//   - storage_uri / checksum / file_type 复制自上游
//
// 同时：
//   - 在上游 fv 追加 transfer 事件
//   - 在下游 fv 写入 register 事件 + 创建 asset_ledgers 草稿/正式入账
func (s *FileOperationService) ReceiveAsInput(in ReceiveInput) (*UploadResult, error) {
	source, err := s.fvRepo.FindByID(in.SourceFileVersionID)
	if err != nil {
		return nil, fmt.Errorf("上游文件版本不存在: %w", err)
	}
	// 注意：领取的目标项目可能不同于上游项目（V1 暂不支持跨项目，但保险起见同时校验）
	if err := s.assertProjectMutable(source.ProjectID); err != nil {
		return nil, err
	}
	// V3-UI option C：上游环节和下游环节都必须可变
	if err := s.assertStageMutable(source.ProjectStageID); err != nil {
		return nil, fmt.Errorf("上游 %w", err)
	}
	if err := s.assertStageMutable(in.TargetStageID); err != nil {
		return nil, fmt.Errorf("下游 %w", err)
	}
	if source.DataState != "output" {
		return nil, fmt.Errorf("仅可领取 output 数据态文件，当前 %s", source.DataState)
	}
	if source.SubmittedAt == nil {
		return nil, fmt.Errorf("上游产出未提交，不可领取")
	}
	if source.StorageURI == nil || *source.StorageURI == "" {
		return nil, fmt.Errorf("上游产出尚未绑定实体文件，不可领取")
	}

	targetStage, err := s.stageRepo.FindByID(in.TargetStageID)
	if err != nil {
		return nil, fmt.Errorf("目标环节不存在: %w", err)
	}
	if targetStage.ProjectID != source.ProjectID {
		return nil, fmt.Errorf("跨项目领取暂不支持")
	}
	project, err := s.projRepo.FindByID(targetStage.ProjectID)
	if err != nil {
		return nil, err
	}
	rule, err := s.findRuleByCode(project, targetStage, in.TargetRuleCode)
	if err != nil {
		return nil, err
	}
	if rule.DataState != "input" {
		return nil, fmt.Errorf("领取目标规则 %s 必须是 input 数据态，当前 %s", rule.FileRuleCode, rule.DataState)
	}

	// 已存在该规则下相同来源的领取记录则幂等返回
	var existingID int64
	if err := s.DB.Get(&existingID, `SELECT id FROM file_versions WHERE project_stage_id = ? AND template_file_rule_id = ? AND source_file_version_id = ? AND disable = 0 LIMIT 1`,
		targetStage.ID, rule.ID, source.ID); err == nil && existingID != 0 {
		fv, _ := s.fvRepo.FindByID(existingID)
		ledger, _ := s.ledgerRepo.FindByFileVersion(existingID)
		return &UploadResult{FileVersion: fv, Ledger: ledger, StoragePath: deref(fv.StorageURI)}, nil
	}

	// V1 修正：如果目标规则下有立项时建的 planned 占位，复用它而不是另起 V2.0
	// （与 derive 一致——避免列表里出现"V1.0 占位 + V2.0 已领取"两条混淆体验）
	if planned := s.findPlannedByRule(targetStage.ID, rule.ID); planned != nil {
		return s.bindPlannedAsReceive(planned, source, in.OperatorID, in.OperatorUserID, project, targetStage, rule)
	}

	// !! 在 BEGIN tx 之前完成所有 r.DB 读，避免 SetMaxOpenConns(1) 死锁 !!
	versionNo := "V1.0"
	count, _ := s.fvRepo.CountByStageRule(targetStage.ID, rule.FileRuleCode)
	if count > 0 {
		versionNo = fmt.Sprintf("V%d.0", count+1)
	}

	fvCode := fmt.Sprintf("%s-%s-%s", project.ProjectCode, targetStage.StageCode, rule.FileRuleCode)
	if count > 0 {
		fvCode = fmt.Sprintf("%s-%s", fvCode, versionNo)
	}

	srcID := source.ID
	ruleID := rule.ID
	storageURI := *source.StorageURI
	srcChecksum := source.Checksum
	srcFileType := source.FileType
	srcSize := source.FileSize

	// 计算安全策略（input + 已绑定 source → dept_stage）
	receivedFv := &models.FileVersion{
		DataState:           "input",
		LifecycleStatus:     "registered",
		SourceFileVersionID: &srcID,
		StorageURI:          &storageURI,
	}
	policyID := ResolveSecurityPolicyID(s.policyRepo, project.SensitivityLevel, receivedFv)

	// 预读模版与上游底账
	tpl, _ := s.tplCache.FindTemplateByCode(project.TemplateCode, project.TemplateVersion)
	classCode := (*string)(nil)
	if tpl != nil {
		classCode = tpl.ClassCode
	}
	srcLedger, _ := s.ledgerRepo.FindByFileVersion(source.ID)
	var srcLedgerID *int64
	if srcLedger != nil {
		srcLedgerID = &srcLedger.ID
	}

	tx, err := s.DB.Beginx()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	newFvID, err := s.fvRepo.Insert(tx, CreateFileVersionInput{
		ProjectID:           project.ID,
		ProjectStageID:      targetStage.ID,
		TemplateFileRuleID:  &ruleID,
		FileVersionCode:     fvCode,
		LocalCode:           rule.FileRuleCode,
		DisplayName:         rule.FileName,
		DataState:           "input",
		VersionNo:           versionNo,
		Required:            rule.Required,
		FileType:            srcFileType,
		StorageURI:          &storageURI,
		Checksum:            srcChecksum,
		FileSize:            srcSize,
		SourceFileVersionID: &srcID,
		SecurityPolicyID:    policyID,
		LifecycleStatus:     "registered",
		CreatedBy:           &in.OperatorID,
	})
	if err != nil {
		return nil, err
	}

	// 底账正式入账
	ledgerCode, err := GenerateLedgerCode(tx, time.Now())
	if err != nil {
		return nil, err
	}
	sourceRef, _ := json.Marshal(map[string]interface{}{
		"upstream_file_version_id":   source.ID,
		"upstream_file_version_code": source.FileVersionCode,
		"received_via":               "downstream_pickup",
	})
	srcRefStr := string(sourceRef)
	ledgerID, err := s.ledgerRepo.Insert(tx, CreateLedgerInput{
		LedgerCode:         ledgerCode,
		FileVersionID:      newFvID,
		ClassCode:          classCode,
		ProjectCode:        project.ProjectCode,
		StageCode:          targetStage.StageCode,
		FileVersionCode:    fvCode,
		AssetName:          rule.FileName,
		OwnerSubjectID:     project.OwnerSubjectID,
		CustodianSubjectID: project.CustodianSubjectID,
		SecuritySubjectID:  project.SecuritySubjectID,
		SensitivityLevel:   project.SensitivityLevel,
		MarkingMethod:      "reference",
		SourceRef:          &srcRefStr,
		CurrentStorageURI:  &storageURI,
		LifecycleStatus:    "registered",
	})
	if err != nil {
		return nil, err
	}

	opUserIDPtr := nullableInt64(in.OperatorUserID)
	// 上游追加 transfer 事件（srcLedgerID 已在 BEGIN tx 前预读）
	transferReason := fmt.Sprintf("被下游环节 %s 的 %s 领取使用",
		targetStage.StageName, rule.FileName)
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  source.ID,
		LedgerID:       srcLedgerID,
		EventType:      EventTransfer,
		EventName:      "下游领取",
		OperatorID:     &in.OperatorID,
		OperatorUserID: opUserIDPtr,
		ToStorageURI:   &storageURI,
		Reason:         &transferReason,
	}); err != nil {
		return nil, err
	}

	// 下游追加 register 事件
	regReason := fmt.Sprintf("领取上游 %s 作为本环节输入（%s）",
		source.DisplayName, rule.FileName)
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  newFvID,
		LedgerID:       &ledgerID,
		EventType:      EventRegister,
		EventName:      "下游领取入账",
		OperatorID:     &in.OperatorID,
		OperatorUserID: opUserIDPtr,
		FromStorageURI: &storageURI,
		ToStorageURI:   &storageURI,
		Reason:         &regReason,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	fv, _ := s.fvRepo.FindByID(newFvID)
	ledger, _ := s.ledgerRepo.FindByID(ledgerID)
	return &UploadResult{FileVersion: fv, Ledger: ledger, StoragePath: storageURI}, nil
}

// =============================================================================
// 内部辅助：通用绑定流程（事务、复制文件、入账、追加事件）
// =============================================================================

// bindPlannedWithSource 把 planned 占位 fv 升级为 registered，同时写入 source_file_version_id
//
// 用于 derive 路径：立项时为 PRC-001 等过程规则预建了 V1.0 planned 占位，
// 派生时不再另起 V2.0，而是复用此占位填上文件 + 设置上游链路。
//
// 这是 bindToFileVersion 的"加 source 链路"变体；后者用于直接首次上传无来源场景。
func (s *FileOperationService) bindPlannedWithSource(
	fv *models.FileVersion,
	sourceFvID *int64,
	in UploadInput,
	project *models.DataProject,
	stage *models.ProjectStage,
	rule *models.TemplateFileRule,
) (*UploadResult, error) {
	// 类型校验
	ext := FileExtFromName(in.OriginalFileName)
	if !IsAllowedFileType(ext, rule.AllowedFileTypes) {
		return nil, fmt.Errorf("文件类型 %s 不在允许列表（规则 %s 允许 %s）", ext, rule.FileRuleCode, rule.AllowedFileTypes)
	}

	// 命名渲染 + 拷贝到存储
	storagePath, fileSize, checksum, err := s.copyFileToStorage(project, stage, rule, fv.VersionNo, in)
	if err != nil {
		return nil, err
	}

	// !! BEGIN tx 前完成所有 r.DB 读 !!
	registered := *fv
	registered.DataState = fv.DataState
	registered.LifecycleStatus = "registered"
	registered.StorageURI = &storagePath
	registered.SourceFileVersionID = sourceFvID
	policyID := ResolveSecurityPolicyID(s.policyRepo, project.SensitivityLevel, &registered)

	ledger, _ := s.ledgerRepo.FindByFileVersion(fv.ID)
	var srcFv *models.FileVersion
	if sourceFvID != nil {
		if f, err := s.fvRepo.FindByID(*sourceFvID); err == nil {
			srcFv = f
		}
	}

	tx, err := s.DB.Beginx()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// UPDATE：除了 bind 字段外，再写 source_file_version_id
	if _, err := tx.Exec(`UPDATE file_versions SET
			file_type = ?, storage_uri = ?, checksum = ?, file_size = ?,
			original_file_name = ?, security_policy_id = ?,
			source_file_version_id = ?,
			lifecycle_status = 'registered', update_time = ?
		WHERE id = ?`,
		ext, storagePath, checksum, fileSize, in.OriginalFileName, policyID,
		sourceFvID, time.Now(), fv.ID); err != nil {
		return nil, err
	}

	if ledger != nil {
		if _, err := tx.Exec(`UPDATE asset_ledgers SET current_storage_uri = ?, lifecycle_status = 'registered', update_time = ? WHERE id = ?`,
			storagePath, time.Now(), ledger.ID); err != nil {
			return nil, err
		}
	}

	// 追加 register 事件
	var ledgerID *int64
	if ledger != nil {
		ledgerID = &ledger.ID
	}
	regReason := fmt.Sprintf("派生自 %s（%s · %s），上传到 %s（%s · %s）",
		safeDisplayName(srcFv), safeDataState(srcFv), safeVersion(srcFv),
		rule.FileName, dataStateLabel(fv.DataState), fv.VersionNo)
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  fv.ID,
		LedgerID:       ledgerID,
		EventType:      EventRegister,
		EventName:      "派生入账",
		OperatorID:     &in.OperatorID,
		OperatorUserID: nullableInt64(in.OperatorUserID),
		ToStorageURI:   &storagePath,
		Reason:         &regReason,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	updated, _ := s.fvRepo.FindByID(fv.ID)
	updatedLedger, _ := s.ledgerRepo.FindByFileVersion(fv.ID)
	return &UploadResult{FileVersion: updated, Ledger: updatedLedger, StoragePath: storagePath}, nil
}

func safeDisplayName(fv *models.FileVersion) string {
	if fv == nil {
		return "未知来源"
	}
	return fv.DisplayName
}
func safeDataState(fv *models.FileVersion) string {
	if fv == nil {
		return ""
	}
	return dataStateLabel(fv.DataState)
}
func safeVersion(fv *models.FileVersion) string {
	if fv == nil {
		return ""
	}
	return fv.VersionNo
}

// bindPlannedAsReceive 把 planned 占位 fv 升级为 registered，作为下游领取的 input
//
// 用于 ReceiveAsInput 路径：立项时为 IN-002 等输入规则建了 V1.0 planned 占位，
// 下游领取时不另起 V2.0，而是复用此占位填入上游 storage_uri + 设置 source 链路。
//
// 与 bindPlannedWithSource 的区别：
//   - bindPlannedWithSource：派生场景，源文件被复制到本环节存储（新副本）
//   - bindPlannedAsReceive：领取场景，引用上游 storage_uri，不复制实体（保持"一份文件，多处引用"语义）
func (s *FileOperationService) bindPlannedAsReceive(
	fv *models.FileVersion,
	source *models.FileVersion,
	operatorID string,
	operatorUserID int64,
	project *models.DataProject,
	targetStage *models.ProjectStage,
	rule *models.TemplateFileRule,
) (*UploadResult, error) {
	opUserIDPtr := nullableInt64(operatorUserID)
	srcID := source.ID
	storageURI := *source.StorageURI

	// !! BEGIN tx 前所有 r.DB 读 !!
	policyFv := &models.FileVersion{
		DataState:           "input",
		LifecycleStatus:     "registered",
		SourceFileVersionID: &srcID,
		StorageURI:          &storageURI,
	}
	policyID := ResolveSecurityPolicyID(s.policyRepo, project.SensitivityLevel, policyFv)

	ledger, _ := s.ledgerRepo.FindByFileVersion(fv.ID)
	srcLedger, _ := s.ledgerRepo.FindByFileVersion(source.ID)
	var srcLedgerID *int64
	if srcLedger != nil {
		srcLedgerID = &srcLedger.ID
	}

	tx, err := s.DB.Beginx()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// UPDATE planned slot 升级为 registered，引用上游 storage_uri/checksum/file_type/size
	if _, err := tx.Exec(`UPDATE file_versions SET
			file_type = ?, storage_uri = ?, checksum = ?, file_size = ?,
			source_file_version_id = ?, security_policy_id = ?,
			lifecycle_status = 'registered', update_time = ?
		WHERE id = ?`,
		source.FileType, storageURI, source.Checksum, source.FileSize,
		&srcID, policyID, time.Now(), fv.ID); err != nil {
		return nil, err
	}

	// 同步底账：从 planned 升 registered + 写 source_ref + 当前 storage_uri
	if ledger != nil {
		sourceRef, _ := json.Marshal(map[string]interface{}{
			"upstream_file_version_id":   source.ID,
			"upstream_file_version_code": source.FileVersionCode,
			"received_via":               "downstream_pickup",
		})
		srcRefStr := string(sourceRef)
		if _, err := tx.Exec(`UPDATE asset_ledgers SET
				current_storage_uri = ?, source_ref = ?, lifecycle_status = 'registered', update_time = ?
			WHERE id = ?`,
			storageURI, srcRefStr, time.Now(), ledger.ID); err != nil {
			return nil, err
		}
	}

	// 上游 transfer 事件
	transferReason := fmt.Sprintf("被下游环节 %s 的 %s 领取使用",
		targetStage.StageName, rule.FileName)
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  source.ID,
		LedgerID:       srcLedgerID,
		EventType:      EventTransfer,
		EventName:      "下游领取",
		OperatorID:     &operatorID,
		OperatorUserID: opUserIDPtr,
		ToStorageURI:   &storageURI,
		Reason:         &transferReason,
	}); err != nil {
		return nil, err
	}

	// 下游 register 事件
	regReason := fmt.Sprintf("领取上游 %s 作为本环节输入（%s）",
		source.DisplayName, rule.FileName)
	var ledgerID *int64
	if ledger != nil {
		ledgerID = &ledger.ID
	}
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  fv.ID,
		LedgerID:       ledgerID,
		EventType:      EventRegister,
		EventName:      "下游领取入账",
		OperatorID:     &operatorID,
		OperatorUserID: opUserIDPtr,
		FromStorageURI: &storageURI,
		ToStorageURI:   &storageURI,
		Reason:         &regReason,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	updated, _ := s.fvRepo.FindByID(fv.ID)
	updatedLedger, _ := s.ledgerRepo.FindByFileVersion(fv.ID)
	return &UploadResult{FileVersion: updated, Ledger: updatedLedger, StoragePath: storageURI}, nil
}

// bindToFileVersion 把源文件复制到项目存储，并把 fv + ledger 切换到 registered 状态
func (s *FileOperationService) bindToFileVersion(fv *models.FileVersion, in UploadInput) (*UploadResult, error) {
	log.Printf("[upload-svc] fv=%d bindToFileVersion: 查 stage", fv.ID)
	stage, err := s.stageRepo.FindByID(fv.ProjectStageID)
	if err != nil {
		return nil, err
	}
	log.Printf("[upload-svc] fv=%d bindToFileVersion: 查 project", fv.ID)
	project, err := s.projRepo.FindByID(fv.ProjectID)
	if err != nil {
		return nil, err
	}
	log.Printf("[upload-svc] fv=%d bindToFileVersion: 查 rule", fv.ID)
	rule, err := s.findRuleForFV(fv)
	if err != nil {
		return nil, err
	}

	// 类型校验
	ext := FileExtFromName(in.OriginalFileName)
	if !IsAllowedFileType(ext, rule.AllowedFileTypes) {
		return nil, fmt.Errorf("文件类型 %s 不在允许列表（规则 %s 允许 %s）", ext, rule.FileRuleCode, rule.AllowedFileTypes)
	}

	// 命名渲染 + 拷贝到存储
	log.Printf("[upload-svc] fv=%d bindToFileVersion: 开始 copyFileToStorage", fv.ID)
	storagePath, fileSize, checksum, err := s.copyFileToStorage(project, stage, rule, fv.VersionNo, in)
	if err != nil {
		return nil, err
	}
	log.Printf("[upload-svc] fv=%d bindToFileVersion: copy 完成 path=%s size=%d", fv.ID, storagePath, fileSize)

	// !! 在 BEGIN tx 之前完成所有"读"，避免占用唯一连接后还想从池里再借 !!
	// （SQLite 池被 SetMaxOpenConns(1) 限制，事务内任何 r.DB.Get 都会死锁）

	// 计算安全策略（基于 data_state + lifecycle 后状态）
	registered := *fv
	registered.DataState = fv.DataState
	registered.LifecycleStatus = "registered"
	storagePathPtr := storagePath
	registered.StorageURI = &storagePathPtr
	policyID := ResolveSecurityPolicyID(s.policyRepo, project.SensitivityLevel, &registered)

	// 预读底账
	ledger, _ := s.ledgerRepo.FindByFileVersion(fv.ID)

	// 写库 + 追加事件（事务）
	log.Printf("[upload-svc] fv=%d bindToFileVersion: BEGIN tx", fv.ID)
	tx, err := s.DB.Beginx()
	if err != nil {
		return nil, err
	}
	log.Printf("[upload-svc] fv=%d bindToFileVersion: tx OK", fv.ID)
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`UPDATE file_versions SET
			file_type = ?, storage_uri = ?, checksum = ?, file_size = ?,
			original_file_name = ?, security_policy_id = ?,
			lifecycle_status = 'registered', update_time = ?
		WHERE id = ?`,
		ext, storagePath, checksum, fileSize, in.OriginalFileName, policyID, time.Now(), fv.ID); err != nil {
		return nil, err
	}

	// 同步底账
	if ledger != nil {
		if _, err := tx.Exec(`UPDATE asset_ledgers SET current_storage_uri = ?, lifecycle_status = 'registered', update_time = ? WHERE id = ?`,
			storagePath, time.Now(), ledger.ID); err != nil {
			return nil, err
		}
	}

	// 追加 register 事件
	var ledgerID *int64
	if ledger != nil {
		ledgerID = &ledger.ID
	}
	regReason := fmt.Sprintf("上传 %s（%s · %s）入账",
		rule.FileName, dataStateLabel(fv.DataState), fv.VersionNo)
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  fv.ID,
		LedgerID:       ledgerID,
		EventType:      EventRegister,
		EventName:      "正式入账",
		OperatorID:     &in.OperatorID,
		OperatorUserID: nullableInt64(in.OperatorUserID),
		ToStorageURI:   &storagePath,
		Reason:         &regReason,
	}); err != nil {
		return nil, err
	}

	log.Printf("[upload-svc] fv=%d bindToFileVersion: COMMIT", fv.ID)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	log.Printf("[upload-svc] fv=%d bindToFileVersion: COMMIT 完成", fv.ID)

	// COMMIT 之后才能再用 r.DB（连接已释放回池）
	updated, _ := s.fvRepo.FindByID(fv.ID)
	updatedLedger, _ := s.ledgerRepo.FindByFileVersion(fv.ID)
	log.Printf("[upload-svc] fv=%d bindToFileVersion: 返回响应", fv.ID)
	return &UploadResult{FileVersion: updated, Ledger: updatedLedger, StoragePath: storagePath}, nil
}

// createAndBindNewFV 创建新的 file_version 并绑定文件（用于多版本和派生）
func (s *FileOperationService) createAndBindNewFV(
	project *models.DataProject,
	stage *models.ProjectStage,
	rule *models.TemplateFileRule,
	dataState string,
	versionNo string,
	sourceFvID *int64,
	in UploadInput,
) (*UploadResult, error) {
	// 类型校验
	ext := FileExtFromName(in.OriginalFileName)
	if !IsAllowedFileType(ext, rule.AllowedFileTypes) {
		return nil, fmt.Errorf("文件类型 %s 不在允许列表（规则 %s 允许 %s）", ext, rule.FileRuleCode, rule.AllowedFileTypes)
	}

	// 拷贝文件到项目存储
	storagePath, fileSize, checksum, err := s.copyFileToStorage(project, stage, rule, versionNo, in)
	if err != nil {
		return nil, err
	}

	// !! 在 BEGIN tx 之前完成所有 r.DB 读，避免 SetMaxOpenConns(1) 导致死锁 !!
	count, _ := s.fvRepo.CountByStageRule(stage.ID, rule.FileRuleCode)
	fvCode := fmt.Sprintf("%s-%s-%s", project.ProjectCode, stage.StageCode, rule.FileRuleCode)
	if count > 0 {
		fvCode = fmt.Sprintf("%s-%s", fvCode, versionNo)
	}

	// 计算安全策略
	policyFv := &models.FileVersion{
		DataState:           dataState,
		LifecycleStatus:     "registered",
		SourceFileVersionID: sourceFvID,
	}
	policyID := ResolveSecurityPolicyID(s.policyRepo, project.SensitivityLevel, policyFv)

	// 预读模版以拿 class_code
	tpl, _ := s.tplCache.FindTemplateByCode(project.TemplateCode, project.TemplateVersion)
	classCode := (*string)(nil)
	if tpl != nil {
		classCode = tpl.ClassCode
	}

	// 预读源文件版本（用于事件 reason 文案 + 区分 new-version vs derive）
	var srcFv *models.FileVersion
	if sourceFvID != nil {
		if f, err := s.fvRepo.FindByID(*sourceFvID); err == nil {
			srcFv = f
		}
	}

	tx, err := s.DB.Beginx()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	ruleID := rule.ID

	newID, err := s.fvRepo.Insert(tx, CreateFileVersionInput{
		ProjectID:           project.ID,
		ProjectStageID:      stage.ID,
		TemplateFileRuleID:  &ruleID,
		FileVersionCode:     fvCode,
		LocalCode:           rule.FileRuleCode,
		DisplayName:         rule.FileName,
		DataState:           dataState,
		VersionNo:           versionNo,
		Required:            rule.Required,
		FileType:            &ext,
		StorageURI:          &storagePath,
		Checksum:            &checksum,
		FileSize:            &fileSize,
		SourceFileVersionID: sourceFvID,
		SecurityPolicyID:    policyID,
		LifecycleStatus:     "registered",
		CreatedBy:           &in.OperatorID,
		CreatedByUserID:     nullableInt64(in.OperatorUserID),
	})
	if err != nil {
		return nil, err
	}

	// 创建底账（直接 registered）
	ledgerCode, err := GenerateLedgerCode(tx, time.Now())
	if err != nil {
		return nil, err
	}
	var sourceRefStr *string
	if sourceFvID != nil {
		ref, _ := json.Marshal(map[string]interface{}{
			"source_file_version_id": *sourceFvID,
			"derive_kind":            dataState,
		})
		s := string(ref)
		sourceRefStr = &s
	}
	ledgerID, err := s.ledgerRepo.Insert(tx, CreateLedgerInput{
		LedgerCode:         ledgerCode,
		FileVersionID:      newID,
		ClassCode:          classCode,
		ProjectCode:        project.ProjectCode,
		StageCode:          stage.StageCode,
		FileVersionCode:    fvCode,
		AssetName:          rule.FileName,
		OwnerSubjectID:     project.OwnerSubjectID,
		CustodianSubjectID: project.CustodianSubjectID,
		SecuritySubjectID:  project.SecuritySubjectID,
		SensitivityLevel:   project.SensitivityLevel,
		MarkingMethod:      "reference",
		SourceRef:          sourceRefStr,
		CurrentStorageURI:  &storagePath,
		LifecycleStatus:    "registered",
	})
	if err != nil {
		return nil, err
	}

	// 追加 register 事件 — reason 用业务可读文案
	//
	// 三种场景文案区分：
	//   - 没有 source：新建 {versionNo} 版本（理论上 createAndBindNewFV 都有 source，留底）
	//   - source 同规则：新建 V{N+1} 版本（基于 V{N}）  // new-version 路径
	//   - source 不同规则：派生自 {上游 display_name}（{数据态}·{版本}），新建 {versionNo} 版本  // derive 路径
	reason := fmt.Sprintf("新建 %s 版本", versionNo)
	if srcFv != nil {
		if srcFv.LocalCode == rule.FileRuleCode {
			reason = fmt.Sprintf("新建 %s 版本（基于 %s）", versionNo, srcFv.VersionNo)
		} else {
			reason = fmt.Sprintf("派生自 %s（%s · %s），新建 %s 版本",
				srcFv.DisplayName, dataStateLabel(srcFv.DataState), srcFv.VersionNo, versionNo)
		}
	}
	if _, err := s.eventRepo.Append(tx, AppendEventInput{
		FileVersionID:  newID,
		LedgerID:       &ledgerID,
		EventType:      EventRegister,
		EventName:      "正式入账",
		OperatorID:     &in.OperatorID,
		OperatorUserID: nullableInt64(in.OperatorUserID),
		ToStorageURI:   &storagePath,
		Reason:         &reason,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	fv, _ := s.fvRepo.FindByID(newID)
	ledger, _ := s.ledgerRepo.FindByID(ledgerID)
	return &UploadResult{FileVersion: fv, Ledger: ledger, StoragePath: storagePath}, nil
}

// copyFileToStorage 把源文件复制到项目存储下，返回存储路径、文件大小、checksum
//
// 严禁修改/删除源文件（CLAUDE.md 约定）。
func (s *FileOperationService) copyFileToStorage(
	project *models.DataProject,
	stage *models.ProjectStage,
	rule *models.TemplateFileRule,
	versionNo string,
	in UploadInput,
) (string, int64, string, error) {
	// 计算源文件 SHA-256（不引入 scanner 包以避免 import 环）
	checksum, fileSize, err := computeFileSHA256(in.SourcePath)
	if err != nil {
		return "", 0, "", fmt.Errorf("计算 checksum 失败: %w", err)
	}

	root := ""
	if project.ProjectRoot != nil {
		root = *project.ProjectRoot
	}
	if root == "" {
		ws := NewProjectWorkspace("")
		root = ws.ProjectDir(project.ProjectCode)
	}
	targetDir := filepath.Join(root, "stages", stage.StageCode, rule.DataState)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", 0, "", err
	}

	// 渲染文件名
	pattern := ""
	if rule.NamingPattern != nil {
		pattern = *rule.NamingPattern
	}
	rendered := RenderNamingPattern(pattern, NamingContext{
		ProjectCode:      project.ProjectCode,
		ProjectName:      project.ProjectName,
		StageCode:        stage.StageCode,
		StageName:        stage.StageName,
		LocalCode:        rule.FileRuleCode,
		DisplayName:      rule.FileName,
		VersionNo:        versionNo,
		UserName:         in.OperatorID,
		OriginalFileName: in.OriginalFileName,
		Date:             time.Now(),
		Extras:           in.Extras,
	})

	ext := FileExtFromName(in.OriginalFileName)
	targetName := rendered
	if ext != "" && !strings.HasSuffix(strings.ToLower(targetName), "."+ext) {
		targetName = targetName + "." + ext
	}
	targetPath := filepath.Join(targetDir, targetName)

	// 文件名冲突 → 加时间戳避免覆盖
	if _, err := os.Stat(targetPath); err == nil {
		stem := strings.TrimSuffix(targetName, "."+ext)
		targetName = fmt.Sprintf("%s_%d.%s", stem, time.Now().UnixNano(), ext)
		targetPath = filepath.Join(targetDir, targetName)
	}

	if err := copyFile(in.SourcePath, targetPath); err != nil {
		return "", 0, "", err
	}

	return targetPath, fileSize, checksum, nil
}

// computeFileSHA256 流式计算文件 SHA-256（uppercase hex）
func computeFileSHA256(path string) (string, int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, err
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), fi.Size(), nil
}

// copyFile 二进制安全的文件复制（不修改源文件）
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// findRuleForFV 通过 fv 反查模版规则
func (s *FileOperationService) findRuleForFV(fv *models.FileVersion) (*models.TemplateFileRule, error) {
	if fv.TemplateFileRuleID == nil {
		return nil, fmt.Errorf("文件版本未关联模版规则")
	}
	var rule models.TemplateFileRule
	if err := s.DB.Get(&rule, `SELECT * FROM template_file_rules WHERE id = ? AND disable = 0`, *fv.TemplateFileRuleID); err != nil {
		return nil, err
	}
	return &rule, nil
}

// findRuleByCode 在指定环节下按 file_rule_code 找规则
func (s *FileOperationService) findRuleByCode(project *models.DataProject, stage *models.ProjectStage, ruleCode string) (*models.TemplateFileRule, error) {
	if stage.TemplateStageID == nil {
		return nil, fmt.Errorf("环节缺少 template_stage_id 关联")
	}
	var rule models.TemplateFileRule
	if err := s.DB.Get(&rule, `SELECT * FROM template_file_rules WHERE template_stage_id = ? AND file_rule_code = ? AND disable = 0`,
		*stage.TemplateStageID, ruleCode); err != nil {
		return nil, fmt.Errorf("规则 %s 在环节 %s 下未找到: %w", ruleCode, stage.StageCode, err)
	}
	_ = project
	return &rule, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// assertProjectMutable 验证项目处于"可写"状态（非 archived/cancelled）
//
// 项目结项归档后整个卷宗进入只读：file_versions / asset_ledgers / 事件流
// 都不允许再变更。任何写动作（上传/派生/新版本/提交/领取/状态切换）
// 都应在最早可能的位置调此守卫。
//
// 返回明确的中文错误便于 UI 直接展示。
func (s *FileOperationService) assertProjectMutable(projectID int64) error {
	project, err := s.projRepo.FindByID(projectID)
	if err != nil {
		return fmt.Errorf("项目不存在: %w", err)
	}
	switch project.Status {
	case "archived":
		return fmt.Errorf("项目已结项归档（%s），所有文件版本已封存，不可再变更", project.ProjectCode)
	case "cancelled":
		return fmt.Errorf("项目已取消（%s），不可再变更", project.ProjectCode)
	}
	return nil
}

// assertStageMutable V3-UI option C：验证环节处于"可写"状态
//
// completed 是硬终态，环节不可再操作文件。
// skipped 是软终态，需先切回 pending 才能再操作。
// 其他状态（pending / running）放行。
//
// 调用方：所有文件写动作（upload/bind/new-version/derive/submit/receive）
// 都应在 assertProjectMutable 之后调用此函数。
func (s *FileOperationService) assertStageMutable(stageID int64) error {
	stage, err := s.stageRepo.FindByID(stageID)
	if err != nil {
		return fmt.Errorf("环节不存在: %w", err)
	}
	if !IsStageMutable(stage.Status) {
		switch stage.Status {
		case "completed":
			return fmt.Errorf("环节「%s」已标记为已完成，不可再变更；如需重新工作，请先升级模版或开新项目", stage.StageName)
		case "skipped":
			return fmt.Errorf("环节「%s」已跳过，不可在此环节直接操作；如需重新做请先撤销跳过（切回'待办'）", stage.StageName)
		default:
			return fmt.Errorf("环节「%s」当前状态 %s 不允许变更", stage.StageName, stage.Status)
		}
	}
	return nil
}

// dataStateLabel 把 input/process/output 转成中文标签，给事件 reason / UI 文案用
func dataStateLabel(s string) string {
	switch s {
	case "input":
		return "输入"
	case "process":
		return "过程"
	case "output":
		return "产出"
	default:
		return s
	}
}
