package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// ProjectCloseService 项目结项服务
//
// 涵盖 G1/G2/G3：
//   - 结项预检：必填文件已上传、产出已提交、底账非 planned
//   - 生成归档清单 manifest.json + 项目根 SHA-256 摘要
//   - 项目状态置为 archived，所有 fv/底账状态推至 sealed
type ProjectCloseService struct {
	DB         *sqlx.DB
	projRepo   *DataProjectRepository
	stageRepo  *ProjectStageRepository
	fvRepo     *FileVersionRepository
	ledgerRepo *AssetLedgerRepository
	memberRepo *ProjectMemberRepository
	eventRepo  *LifecycleEventRepository
}

func NewProjectCloseService(db *sqlx.DB) *ProjectCloseService {
	return &ProjectCloseService{
		DB:         db,
		projRepo:   NewDataProjectRepository(db),
		stageRepo:  NewProjectStageRepository(db),
		fvRepo:     NewFileVersionRepository(db),
		ledgerRepo: NewAssetLedgerRepository(db),
		memberRepo: NewProjectMemberRepository(db),
		eventRepo:  NewLifecycleEventRepository(db),
	}
}

// PrecheckIssue 预检发现的问题
type PrecheckIssue struct {
	Severity string `json:"severity"` // error / warning
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// PrecheckResult 预检结果
//
// 注意：Issues 字段类型上是 nil-able slice，Go nil slice JSON 序列化为 null，
// 前端 access .length 会 NPE。所有构造路径必须显式初始化为空 slice。
type PrecheckResult struct {
	OK     bool            `json:"ok"`
	Issues []PrecheckIssue `json:"issues"`
}

// Precheck 检查项目能否结项
//
// 规则：
//   - error: 项目已 archived/cancelled
//   - error: 必填 (required=1) 的文件版本仍 planned（未上传）
//   - warning: output 已绑定但未 submitted_at
//   - warning: 任意 ledger 仍是 planned
func (s *ProjectCloseService) Precheck(projectID int64) (*PrecheckResult, error) {
	project, err := s.projRepo.FindByID(projectID)
	if err != nil {
		return nil, err
	}
	res := &PrecheckResult{OK: true, Issues: []PrecheckIssue{}}

	if project.Status == "archived" {
		res.Issues = append(res.Issues, PrecheckIssue{Severity: "error", Code: "ALREADY_ARCHIVED", Message: "项目已归档，不可重复结项"})
		res.OK = false
		return res, nil
	}
	if project.Status == "cancelled" {
		res.Issues = append(res.Issues, PrecheckIssue{Severity: "error", Code: "CANCELLED", Message: "项目已取消"})
		res.OK = false
		return res, nil
	}

	fvs, err := s.fvRepo.ListByProject(projectID)
	if err != nil {
		return nil, err
	}

	// 必填且仍 planned → 阻塞
	for _, fv := range fvs {
		if fv.Required == 1 && fv.LifecycleStatus == "planned" {
			res.Issues = append(res.Issues, PrecheckIssue{
				Severity: "error",
				Code:     "REQUIRED_NOT_REGISTERED",
				Message:  fmt.Sprintf("必填文件版本 %s（%s）尚未上传/绑定", fv.FileVersionCode, fv.DisplayName),
			})
		}
		if fv.DataState == "output" && fv.LifecycleStatus == "registered" && fv.SubmittedAt == nil {
			res.Issues = append(res.Issues, PrecheckIssue{
				Severity: "warning",
				Code:     "OUTPUT_NOT_SUBMITTED",
				Message:  fmt.Sprintf("产出文件 %s 已上传但未提交", fv.FileVersionCode),
			})
		}
	}

	// 是否所有 ledger 都至少到 registered
	rows, err := s.ledgerRepo.Search(LedgerSearchInput{ProjectCode: project.ProjectCode, LifecycleStatus: "planned"})
	if err == nil && len(rows) > 0 {
		for _, l := range rows {
			res.Issues = append(res.Issues, PrecheckIssue{
				Severity: "warning",
				Code:     "LEDGER_PLANNED",
				Message:  fmt.Sprintf("底账 %s（%s）仍是草稿状态", l.LedgerCode, l.AssetName),
			})
		}
	}

	for _, issue := range res.Issues {
		if issue.Severity == "error" {
			res.OK = false
			break
		}
	}
	return res, nil
}

// CloseInput 结项入参
type CloseInput struct {
	ProjectID      int64
	OperatorID     string // V1：操作人用户名
	OperatorUserID int64  // V2：users.id
	Reason         string // 可选：结项说明
	Force          bool   // 强制结项（忽略 warning）
}

// CloseOutput 结项输出
type CloseOutput struct {
	Project        *models.DataProject `json:"project"`
	ManifestPath   string              `json:"manifest_path"`
	ManifestSha256 string              `json:"manifest_sha256"`
	FileCount      int                 `json:"file_count"`
	LedgerCount    int                 `json:"ledger_count"`
	EventCount     int                 `json:"event_count"`
}

// Close 执行结项
//
// 步骤：
//  1. Precheck — 有 error 或（非 force 时有 warning）则中止
//  2. 生成归档清单 manifest.json，写入项目根
//  3. 更新所有 file_versions / asset_ledgers 至 sealed
//  4. 项目状态切到 archived
//  5. 追加 archive 事件
//  6. 单事务提交
func (s *ProjectCloseService) Close(in CloseInput) (*CloseOutput, error) {
	check, err := s.Precheck(in.ProjectID)
	if err != nil {
		return nil, err
	}
	hasError := false
	hasWarning := false
	for _, iss := range check.Issues {
		if iss.Severity == "error" {
			hasError = true
		}
		if iss.Severity == "warning" {
			hasWarning = true
		}
	}
	if hasError {
		return nil, fmt.Errorf("结项预检未通过（%d 个错误）", len(check.Issues))
	}
	if hasWarning && !in.Force {
		return nil, fmt.Errorf("结项预检有警告未确认（%d 个），如需继续请使用 force=true", len(check.Issues))
	}

	project, err := s.projRepo.FindByID(in.ProjectID)
	if err != nil {
		return nil, err
	}

	// 加载所有 file_versions / ledgers / events / members 用于 manifest
	fvs, err := s.fvRepo.ListByProject(in.ProjectID)
	if err != nil {
		return nil, err
	}
	ledgers, err := s.ledgerRepo.Search(LedgerSearchInput{ProjectCode: project.ProjectCode})
	if err != nil {
		return nil, err
	}
	events, err := s.eventRepo.ListByProject(project.ProjectCode)
	if err != nil {
		return nil, err
	}
	members, err := s.memberRepo.ListByProject(in.ProjectID)
	if err != nil {
		return nil, err
	}
	stages, err := s.stageRepo.ListByProject(in.ProjectID)
	if err != nil {
		return nil, err
	}

	manifest := buildManifest(project, stages, fvs, ledgers, events, members, subjectCodesForArchive(s.DB, project, ledgers), in.Reason, in.OperatorID)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	// 写到 {project_root}/manifest.json
	root := ""
	if project.ProjectRoot != nil {
		root = *project.ProjectRoot
	}
	if root == "" {
		ws := NewProjectWorkspace("")
		root = ws.ProjectDir(project.ProjectCode)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}
	manifestSha := sha256Hex(manifestBytes)

	// 数据库事务：sealed + archived
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

	now := time.Now()
	for _, fv := range fvs {
		// 已是终态的不动
		if fv.LifecycleStatus == "destroyed" || fv.LifecycleStatus == "permanent" || fv.LifecycleStatus == "sealed" {
			continue
		}
		if _, err := tx.Exec(`UPDATE file_versions SET lifecycle_status = 'sealed', update_time = ? WHERE id = ?`, now, fv.ID); err != nil {
			return nil, err
		}
	}
	for _, l := range ledgers {
		if l.LifecycleStatus == "destroyed" || l.LifecycleStatus == "permanent" || l.LifecycleStatus == "sealed" {
			continue
		}
		if _, err := tx.Exec(`UPDATE asset_ledgers SET lifecycle_status = 'sealed', update_time = ? WHERE id = ?`, now, l.ID); err != nil {
			return nil, err
		}
	}

	// 给每个 ledger 追加 archive 事件
	reasonStr := in.Reason
	if reasonStr == "" {
		reasonStr = "项目结项归档"
	}
	opUserIDPtr := nullableInt64(in.OperatorUserID)
	for _, l := range ledgers {
		ledgerID := l.ID
		opID := in.OperatorID
		if _, err := s.eventRepo.Append(tx, AppendEventInput{
			FileVersionID:  l.FileVersionID,
			LedgerID:       &ledgerID,
			EventType:      EventArchive,
			EventName:      "项目结项归档",
			OperatorID:     &opID,
			OperatorUserID: opUserIDPtr,
			Reason:         &reasonStr,
		}); err != nil {
			return nil, err
		}
	}

	// 项目状态置 archived，写 sync_status 等待上报
	syncStatus := "pending"
	syncMessage := fmt.Sprintf("manifest sha256=%s", manifestSha)
	if _, err := tx.Exec(`UPDATE data_projects SET status = 'archived', sync_status = ?, sync_message = ?, update_time = ? WHERE id = ?`,
		syncStatus, syncMessage, now, project.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	updatedProject, _ := s.projRepo.FindByID(in.ProjectID)
	return &CloseOutput{
		Project:        updatedProject,
		ManifestPath:   manifestPath,
		ManifestSha256: manifestSha,
		FileCount:      len(fvs),
		LedgerCount:    len(ledgers),
		EventCount:     len(events),
	}, nil
}

// =============================================================================
// 归档清单结构
// =============================================================================

// ArchiveManifest 归档清单 JSON 文档结构
//
// 与 manage 侧 /api/sync/project-archive 接口约定字段对齐：
//
//	body.project / body.stages / body.file_versions / body.ledgers / body.lifecycle_events
type ArchiveManifest struct {
	Schema          string               `json:"schema"`
	SourceTerminal  string               `json:"source_terminal,omitempty"`
	GeneratedAt     string               `json:"generated_at"`
	GeneratedBy     string               `json:"generated_by"`
	Reason          string               `json:"reason,omitempty"`
	Manifest        map[string]string    `json:"manifest,omitempty"` // 元信息容器（manifest_sha256 等）
	Project         ArchiveProject       `json:"project"`
	Stages          []ArchiveStage       `json:"stages"`
	FileVersions    []ArchiveFileVersion `json:"file_versions"`
	Ledgers         []ArchiveLedger      `json:"ledgers"`
	Members         []ArchiveMember      `json:"members"`
	LifecycleEvents []ArchiveEvent       `json:"lifecycle_events"`
	Stats           map[string]int       `json:"stats"`
}

type ArchiveProject struct {
	ProjectCode          string `json:"project_code"`
	ProjectName          string `json:"project_name"`
	TemplateCode         string `json:"template_code"`
	TemplateVersion      string `json:"template_version"`
	SensitivityLevel     string `json:"sensitivity_level"`
	ManagementMode       string `json:"management_mode"`
	OwnerSubjectID       int64  `json:"owner_subject_id"`
	CustodianSubjectID   int64  `json:"custodian_subject_id"`
	SecuritySubjectID    int64  `json:"security_subject_id"`
	OwnerSubjectCode     string `json:"owner_subject_code,omitempty"`
	CustodianSubjectCode string `json:"custodian_subject_code,omitempty"`
	SecuritySubjectCode  string `json:"security_subject_code,omitempty"`
	ProjectRoot          string `json:"project_root,omitempty"`
	CreatedBy            string `json:"created_by,omitempty"`         // V1：立项人用户名
	CreatedByUserID      *int64 `json:"created_by_user_id,omitempty"` // V2：users.id
	CreateTime           string `json:"create_time"`
}

type ArchiveStage struct {
	StageCode string `json:"stage_code"`
	StageName string `json:"stage_name"`
	StageType string `json:"stage_type"`
	SortOrder int    `json:"sort_order"`
	Status    string `json:"status"`
}

type ArchiveFileVersion struct {
	FileVersionID     int64  `json:"file_version_id,omitempty"`
	FileVersionCode   string `json:"file_version_code"`
	LocalCode         string `json:"local_code"`
	DisplayName       string `json:"display_name"`
	StageCode         string `json:"stage_code"`
	DataState         string `json:"data_state"`
	VersionNo         string `json:"version_no"`
	Required          int    `json:"required"`
	FileType          string `json:"file_type,omitempty"`
	StorageURI        string `json:"storage_uri,omitempty"`
	Checksum          string `json:"checksum,omitempty"`
	FileSize          int64  `json:"file_size,omitempty"`
	OriginalFileName  string `json:"original_file_name,omitempty"`
	LifecycleStatus   string `json:"lifecycle_status"`
	CreatedBy         string `json:"created_by,omitempty"`         // V1
	CreatedByUserID   *int64 `json:"created_by_user_id,omitempty"` // V2
	SubmittedAt       string `json:"submitted_at,omitempty"`
	SubmittedBy       string `json:"submitted_by,omitempty"`         // V1
	SubmittedByUserID *int64 `json:"submitted_by_user_id,omitempty"` // V2
	SourceFvCode      string `json:"source_fv_code,omitempty"`
}

type ArchiveLedger struct {
	LedgerID             int64  `json:"ledger_id,omitempty"`
	LedgerCode           string `json:"ledger_code"`
	FileVersionCode      string `json:"file_version_code"`
	AssetName            string `json:"asset_name"`
	StageCode            string `json:"stage_code"`
	SensitivityLevel     string `json:"sensitivity_level"`
	MarkingMethod        string `json:"marking_method"`
	LifecycleStatus      string `json:"lifecycle_status"`
	OwnerSubjectID       int64  `json:"owner_subject_id"`
	CustodianSubjectID   int64  `json:"custodian_subject_id"`
	SecuritySubjectID    int64  `json:"security_subject_id"`
	OwnerSubjectCode     string `json:"owner_subject_code,omitempty"`
	CustodianSubjectCode string `json:"custodian_subject_code,omitempty"`
	SecuritySubjectCode  string `json:"security_subject_code,omitempty"`
}

type ArchiveMember struct {
	SubjectID         int64    `json:"subject_id"`
	UserID            *int64   `json:"user_id,omitempty"` // V2：立项人 / 项目成员 user.id
	RoleCode          string   `json:"role_code"`
	StageIDs          string   `json:"stage_ids,omitempty"`
	PermissionActions []string `json:"permission_actions"`
}

type ArchiveEvent struct {
	EventType      string `json:"event_type"`
	EventName      string `json:"event_name"`
	FileVersionID  int64  `json:"file_version_id"`
	LedgerID       int64  `json:"ledger_id,omitempty"`
	OperatorID     string `json:"operator_id,omitempty"`      // V1
	OperatorUserID *int64 `json:"operator_user_id,omitempty"` // V2
	Reason         string `json:"reason,omitempty"`
	CreateTime     string `json:"create_time"`
}

func buildManifest(
	project *models.DataProject,
	stages []models.ProjectStage,
	fvs []models.FileVersion,
	ledgers []models.AssetLedger,
	events []models.LifecycleEvent,
	members []models.ProjectMember,
	subjectCodes map[int64]string,
	reason, operator string,
) *ArchiveManifest {
	// 排序便于稳定输出
	sort.Slice(stages, func(i, j int) bool { return stages[i].SortOrder < stages[j].SortOrder })
	sort.Slice(fvs, func(i, j int) bool { return fvs[i].FileVersionCode < fvs[j].FileVersionCode })
	sort.Slice(ledgers, func(i, j int) bool { return ledgers[i].LedgerCode < ledgers[j].LedgerCode })

	stageCodeByID := map[int64]string{}
	for _, s := range stages {
		stageCodeByID[s.ID] = s.StageCode
	}
	fvCodeByID := map[int64]string{}
	for _, f := range fvs {
		fvCodeByID[f.ID] = f.FileVersionCode
	}

	mfStages := make([]ArchiveStage, 0, len(stages))
	for _, s := range stages {
		mfStages = append(mfStages, ArchiveStage{
			StageCode: s.StageCode,
			StageName: s.StageName,
			StageType: s.StageType,
			SortOrder: s.SortOrder,
			Status:    s.Status,
		})
	}

	mfFvs := make([]ArchiveFileVersion, 0, len(fvs))
	for _, f := range fvs {
		af := ArchiveFileVersion{
			FileVersionID:     f.ID,
			FileVersionCode:   f.FileVersionCode,
			LocalCode:         f.LocalCode,
			DisplayName:       f.DisplayName,
			StageCode:         stageCodeByID[f.ProjectStageID],
			DataState:         f.DataState,
			VersionNo:         f.VersionNo,
			Required:          f.Required,
			LifecycleStatus:   f.LifecycleStatus,
			CreatedByUserID:   f.CreatedByUserID,
			SubmittedByUserID: f.SubmittedByUserID,
		}
		if f.FileType != nil {
			af.FileType = *f.FileType
		}
		if f.StorageURI != nil {
			af.StorageURI = *f.StorageURI
		}
		if f.Checksum != nil {
			af.Checksum = *f.Checksum
		}
		if f.FileSize != nil {
			af.FileSize = *f.FileSize
		}
		if f.OriginalFileName != nil {
			af.OriginalFileName = *f.OriginalFileName
		}
		if f.CreatedBy != nil {
			af.CreatedBy = *f.CreatedBy
		}
		if f.SubmittedAt != nil {
			af.SubmittedAt = f.SubmittedAt.Format(time.RFC3339)
		}
		if f.SubmittedBy != nil {
			af.SubmittedBy = *f.SubmittedBy
		}
		if f.SourceFileVersionID != nil {
			af.SourceFvCode = fvCodeByID[*f.SourceFileVersionID]
		}
		mfFvs = append(mfFvs, af)
	}

	mfLedgers := make([]ArchiveLedger, 0, len(ledgers))
	for _, l := range ledgers {
		mfLedgers = append(mfLedgers, ArchiveLedger{
			LedgerID:             l.ID,
			LedgerCode:           l.LedgerCode,
			FileVersionCode:      l.FileVersionCode,
			AssetName:            l.AssetName,
			StageCode:            l.StageCode,
			SensitivityLevel:     l.SensitivityLevel,
			MarkingMethod:        l.MarkingMethod,
			LifecycleStatus:      l.LifecycleStatus,
			OwnerSubjectID:       l.OwnerSubjectID,
			CustodianSubjectID:   l.CustodianSubjectID,
			SecuritySubjectID:    l.SecuritySubjectID,
			OwnerSubjectCode:     subjectCodes[l.OwnerSubjectID],
			CustodianSubjectCode: subjectCodes[l.CustodianSubjectID],
			SecuritySubjectCode:  subjectCodes[l.SecuritySubjectID],
		})
	}

	mfMembers := make([]ArchiveMember, 0, len(members))
	for _, m := range members {
		actions, _ := parsePermissionActions(m.PermissionActions)
		am := ArchiveMember{
			SubjectID:         m.SubjectID,
			UserID:            m.UserID,
			RoleCode:          m.RoleCode,
			PermissionActions: actions,
		}
		if m.StageIDs != nil {
			am.StageIDs = *m.StageIDs
		}
		mfMembers = append(mfMembers, am)
	}

	mfEvents := make([]ArchiveEvent, 0, len(events))
	for _, e := range events {
		ae := ArchiveEvent{
			EventType:      e.EventType,
			EventName:      e.EventName,
			FileVersionID:  e.FileVersionID,
			OperatorUserID: e.OperatorUserID,
			CreateTime:     e.CreateTime.Format(time.RFC3339),
		}
		if e.LedgerID != nil {
			ae.LedgerID = *e.LedgerID
		}
		if e.OperatorID != nil {
			ae.OperatorID = *e.OperatorID
		}
		if e.Reason != nil {
			ae.Reason = *e.Reason
		}
		mfEvents = append(mfEvents, ae)
	}

	mp := ArchiveProject{
		ProjectCode:          project.ProjectCode,
		ProjectName:          project.ProjectName,
		TemplateCode:         project.TemplateCode,
		TemplateVersion:      project.TemplateVersion,
		SensitivityLevel:     project.SensitivityLevel,
		ManagementMode:       project.ManagementMode,
		OwnerSubjectID:       project.OwnerSubjectID,
		CustodianSubjectID:   project.CustodianSubjectID,
		SecuritySubjectID:    project.SecuritySubjectID,
		OwnerSubjectCode:     subjectCodes[project.OwnerSubjectID],
		CustodianSubjectCode: subjectCodes[project.CustodianSubjectID],
		SecuritySubjectCode:  subjectCodes[project.SecuritySubjectID],
		CreatedByUserID:      project.CreatedByUserID,
		CreateTime:           project.CreateTime.Format(time.RFC3339),
	}
	if project.ProjectRoot != nil {
		mp.ProjectRoot = *project.ProjectRoot
	}
	if project.CreatedBy != nil {
		mp.CreatedBy = *project.CreatedBy
	}
	decision := DecideArchiveTargetForState(project.ProjectCode, project.SensitivityLevel, FileStateUnitRelease)

	stats := map[string]int{
		"file_versions": len(fvs),
		"ledgers":       len(ledgers),
		"events":        len(events),
		"stages":        len(stages),
		"members":       len(members),
	}

	return &ArchiveManifest{
		Schema:         "data-asset-scan/archive-manifest-v1",
		SourceTerminal: "data-asset-scan",
		GeneratedAt:    time.Now().Format(time.RFC3339),
		GeneratedBy:    operator,
		Reason:         reason,
		Manifest: map[string]string{
			"archive_phase":      decision.ArchivePhase,
			"archive_target":     decision.TargetTier,
			"archive_action":     decision.Action,
			"archive_file_state": decision.FileState,
			"storage_label":      decision.StorageLabel,
			"storage_authority":  "manage",
			"transfer_mode":      "structured_manifest",
		},
		Project:         mp,
		Stages:          mfStages,
		FileVersions:    mfFvs,
		Ledgers:         mfLedgers,
		Members:         mfMembers,
		LifecycleEvents: mfEvents,
		Stats:           stats,
	}
}

func subjectCodesForArchive(db *sqlx.DB, project *models.DataProject, ledgers []models.AssetLedger) map[int64]string {
	ids := map[int64]struct{}{}
	if project != nil {
		ids[project.OwnerSubjectID] = struct{}{}
		ids[project.CustodianSubjectID] = struct{}{}
		ids[project.SecuritySubjectID] = struct{}{}
	}
	for _, l := range ledgers {
		ids[l.OwnerSubjectID] = struct{}{}
		ids[l.CustodianSubjectID] = struct{}{}
		ids[l.SecuritySubjectID] = struct{}{}
	}
	out := map[int64]string{}
	for id := range ids {
		if id <= 0 {
			continue
		}
		var code string
		if err := db.Get(&code, `SELECT code FROM subjects WHERE id = ? AND disable = 0`, id); err == nil {
			out[id] = code
		}
	}
	return out
}

// sha256Hex 计算字节切片的 SHA-256（uppercase hex）
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	s := hex.EncodeToString(sum[:])
	bs := []byte(s)
	for i, c := range bs {
		if c >= 'a' && c <= 'z' {
			bs[i] = c - 32
		}
	}
	return string(bs)
}
