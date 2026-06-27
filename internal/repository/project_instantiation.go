package repository

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// ProjectInstantiationService 立项实例化服务
//
// 在事务内：
//  1. 校验模版存在 + 安全等级就高不就低
//  2. 生成项目编码
//  3. 写入 data_projects（锁定 template_code+version）
//  4. 复制 template_stages 到 project_stages
//  5. 复制 template_file_rules 到 file_versions（lifecycle_status = planned，VersionNo = V1.0）
//  6. 为每条 file_version 生成 asset_ledgers 草稿
//  7. 写入 project_members
//  8. 创建本地目录树
//  9. 写回 project_stages.directory_path 和 data_projects.project_root
//
// 任一步失败：事务回滚 + 已创建的目录不主动清理（重复立项使用同 project_code 时
// 需要先手动清理）。
type ProjectInstantiationService struct {
	DB           *sqlx.DB
	templateRepo *TemplateCacheRepository
	projectRepo  *DataProjectRepository
	stageRepo    *ProjectStageRepository
	fileRepo     *FileVersionRepository
	ledgerRepo   *AssetLedgerRepository
	memberRepo   *ProjectMemberRepository
	policyRepo   *SecurityPolicyRepository
	configRepo   *SystemConfigRepository
}

// NewProjectInstantiationService 构造
func NewProjectInstantiationService(db *sqlx.DB) *ProjectInstantiationService {
	return &ProjectInstantiationService{
		DB:           db,
		templateRepo: NewTemplateCacheRepository(db),
		projectRepo:  NewDataProjectRepository(db),
		stageRepo:    NewProjectStageRepository(db),
		fileRepo:     NewFileVersionRepository(db),
		ledgerRepo:   NewAssetLedgerRepository(db),
		memberRepo:   NewProjectMemberRepository(db),
		policyRepo:   NewSecurityPolicyRepository(db),
		configRepo:   NewSystemConfigRepository(db),
	}
}

// MemberInput 立项时的成员入参
//
// V2 起 UserID 是规范字段（与需求文档 §4.11 对齐）。
// SubjectID 保留向后兼容 V1（如果 UserID 已填，SubjectID 可为 0）。
type MemberInput struct {
	UserID            *int64   `json:"user_id"`    // V2 规范字段
	SubjectID         int64    `json:"subject_id"` // V1 遗留过渡
	RoleCode          string   `json:"role_code"`
	StageCodes        []string `json:"stage_codes"`        // 可参与环节编码
	PermissionActions []string `json:"permission_actions"` // 权限动作
}

// InstantiateInput 立项实例化总入参
type InstantiateInput struct {
	TemplateCode       string        `json:"template_code"`
	TemplateVersion    string        `json:"template_version"`
	ProjectName        string        `json:"project_name"`
	ObjectShortCode    string        `json:"object_short_code"`
	TaskSummary        string        `json:"task_summary"`
	ApprovalBasis      string        `json:"approval_basis"`
	PlannedStartDate   *time.Time    `json:"planned_start_date"`
	PlannedEndDate     *time.Time    `json:"planned_end_date"`
	SensitivityLevel   string        `json:"sensitivity_level"`
	ManagementMode     string        `json:"management_mode"` // independent / shared / mixed
	OwnerSubjectID     int64         `json:"owner_subject_id"`
	CustodianSubjectID int64         `json:"custodian_subject_id"`
	SecuritySubjectID  int64         `json:"security_subject_id"`
	Members            []MemberInput `json:"members"`
	CreatedBy          string        `json:"created_by"`         // V1 兼容：操作人用户名字符串
	CreatedByUserID    int64         `json:"created_by_user_id"` // V2：立项人 user.id（用于自动 enroll 为项目负责人）
	// 是否激活：true 直接 active，false 仅创建为 draft
	Activate bool `json:"activate"`
}

// InstantiateOutput 实例化输出
type InstantiateOutput struct {
	Project *models.DataProject    `json:"project"`
	Stages  []FullStageInstance    `json:"stages"`
	Members []models.ProjectMember `json:"members"`
}

// FullStageInstance 项目环节及其 file_versions
type FullStageInstance struct {
	models.ProjectStage
	FileVersions []models.FileVersion `json:"file_versions"`
}

// Instantiate 主入口
func (s *ProjectInstantiationService) Instantiate(in InstantiateInput) (*InstantiateOutput, error) {
	if err := validateInstantiateInput(&in); err != nil {
		return nil, err
	}

	// 1. 加载模版完整结构
	tpl, err := s.templateRepo.FindTemplateByCode(in.TemplateCode, in.TemplateVersion)
	if err != nil {
		return nil, fmt.Errorf("模版未找到（%s %s）：先到 /templates 同步", in.TemplateCode, in.TemplateVersion)
	}
	if tpl.Status != "active" {
		return nil, fmt.Errorf("模版状态非 active（当前 %s），不能立项", tpl.Status)
	}
	full, err := s.templateRepo.GetFullTemplate(tpl.ID)
	if err != nil {
		return nil, fmt.Errorf("加载模版结构失败: %w", err)
	}

	// 2. 安全等级就高不就低
	finalLevel := HigherSensitivityLevel(tpl.ProjectSensitivityLevel, in.SensitivityLevel)

	// 3. 校验"必须有具备 close 权限的成员"
	//
	// V2：立项人会自动 enroll 为项目负责人 + 全权限（含 close）。
	// 所以只要 CreatedByUserID > 0 即视为已满足；否则按 V1 路径要求 members 里有 close。
	if in.CreatedByUserID <= 0 {
		hasClose := false
		for _, m := range in.Members {
			for _, a := range m.PermissionActions {
				if a == "close" {
					hasClose = true
					break
				}
			}
			if hasClose {
				break
			}
		}
		if !hasClose {
			return nil, fmt.Errorf("立项必须至少有一个成员具备 close 权限（项目负责人）")
		}
	}

	// 4. 解析项目根目录（已与「工作空间目录」合并，统一取 workspace）
	projectRoot := s.configRepo.GetEffectiveProjectRoot()
	workspace := NewProjectWorkspace(projectRoot)
	if err := workspace.EnsureProjectRootExists(); err != nil {
		return nil, fmt.Errorf("项目根目录不可写：%w", err)
	}

	// 5. 在事务内完成入库
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

	// 5.1 生成项目编码
	projectCode, err := GenerateProjectCode(tx, in.ObjectShortCode, time.Now())
	if err != nil {
		return nil, err
	}

	// 5.2 写入 data_projects
	status := "draft"
	if in.Activate {
		status = "active"
	}
	templateID := tpl.ID
	objectShort := in.ObjectShortCode
	taskSum := in.TaskSummary
	approvalBasis := in.ApprovalBasis
	createdBy := in.CreatedBy
	root := workspace.ProjectDir(projectCode)

	var createdByUserIDPtr *int64
	if in.CreatedByUserID > 0 {
		u := in.CreatedByUserID
		createdByUserIDPtr = &u
	}
	projectID, err := s.projectRepo.Insert(tx, CreateDataProjectInput{
		ProjectCode:        projectCode,
		ProjectName:        in.ProjectName,
		ObjectShortCode:    &objectShort,
		TemplateID:         &templateID,
		TemplateCode:       tpl.TemplateCode,
		TemplateVersion:    tpl.TemplateVersion,
		TaskSummary:        nullable(taskSum),
		ApprovalBasis:      nullable(approvalBasis),
		PlannedStartDate:   in.PlannedStartDate,
		PlannedEndDate:     in.PlannedEndDate,
		SensitivityLevel:   finalLevel,
		ManagementMode:     defaultStr(in.ManagementMode, "independent"),
		OwnerSubjectID:     in.OwnerSubjectID,
		CustodianSubjectID: in.CustodianSubjectID,
		SecuritySubjectID:  in.SecuritySubjectID,
		Status:             status,
		ProjectRoot:        &root,
		CreatedBy:          nullable(createdBy),
		CreatedByUserID:    createdByUserIDPtr,
	})
	if err != nil {
		return nil, fmt.Errorf("写 data_projects 失败: %w", err)
	}

	// 5.3 复制 template_stages → project_stages，记录 stage_code 与 local id 映射
	stageIDByCode := map[string]int64{}
	stagesOut := []FullStageInstance{}
	stageCodes := make([]string, 0, len(full.Stages))
	for _, ts := range full.Stages {
		tsID := ts.ID
		dirPath := workspace.StageDir(projectCode, ts.StageCode)
		psID, err := s.stageRepo.Insert(tx, CreateProjectStageInput{
			ProjectID:         projectID,
			TemplateStageID:   &tsID,
			StageCode:         ts.StageCode,
			StageName:         ts.StageName,
			StageType:         ts.StageType,
			SortOrder:         ts.SortOrder,
			Status:            "pending",
			AssignedRoleCodes: ts.DefaultRoleCodes,
			DirectoryPath:     &dirPath,
		})
		if err != nil {
			return nil, fmt.Errorf("写 project_stages %s 失败: %w", ts.StageCode, err)
		}
		stageIDByCode[ts.StageCode] = psID
		stageCodes = append(stageCodes, ts.StageCode)
		stagesOut = append(stagesOut, FullStageInstance{
			ProjectStage: models.ProjectStage{
				ID:              psID,
				ProjectID:       projectID,
				TemplateStageID: &tsID,
				StageCode:       ts.StageCode,
				StageName:       ts.StageName,
				StageType:       ts.StageType,
				SortOrder:       ts.SortOrder,
				Status:          "pending",
				DirectoryPath:   &dirPath,
			},
		})

		// 5.4 复制每条 file_rule → file_versions（planned）
		for _, fr := range ts.FileRules {
			frID := fr.ID
			fvCode := fmt.Sprintf("%s-%s-%s", projectCode, ts.StageCode, fr.FileRuleCode)
			fvID, err := s.fileRepo.Insert(tx, CreateFileVersionInput{
				ProjectID:          projectID,
				ProjectStageID:     psID,
				TemplateFileRuleID: &frID,
				FileVersionCode:    fvCode,
				LocalCode:          fr.FileRuleCode,
				DisplayName:        fr.FileName,
				DataState:          fr.DataState,
				VersionNo:          "V1.0",
				Required:           fr.Required,
				LifecycleStatus:    "planned",
				CreatedBy:          nullable(createdBy),
				CreatedByUserID:    createdByUserIDPtr,
			})
			if err != nil {
				return nil, fmt.Errorf("写 file_versions %s 失败: %w", fvCode, err)
			}
			// 5.5 创建 asset_ledgers 草稿
			ledgerCode, err := GenerateLedgerCode(tx, time.Now())
			if err != nil {
				return nil, fmt.Errorf("生成底账编号失败: %w", err)
			}
			classCode := tpl.ClassCode
			if _, err := s.ledgerRepo.Insert(tx, CreateLedgerInput{
				LedgerCode:         ledgerCode,
				FileVersionID:      fvID,
				ClassCode:          classCode,
				ProjectCode:        projectCode,
				StageCode:          ts.StageCode,
				FileVersionCode:    fvCode,
				AssetName:          fr.FileName,
				OwnerSubjectID:     in.OwnerSubjectID,
				CustodianSubjectID: in.CustodianSubjectID,
				SecuritySubjectID:  in.SecuritySubjectID,
				SensitivityLevel:   finalLevel,
				MarkingMethod:      "reference",
				LifecycleStatus:    "planned",
			}); err != nil {
				return nil, fmt.Errorf("写 asset_ledgers 失败: %w", err)
			}

			// 累积到输出
			lastIdx := len(stagesOut) - 1
			stagesOut[lastIdx].FileVersions = append(stagesOut[lastIdx].FileVersions, models.FileVersion{
				ID:                 fvID,
				ProjectID:          projectID,
				ProjectStageID:     psID,
				TemplateFileRuleID: &frID,
				FileVersionCode:    fvCode,
				LocalCode:          fr.FileRuleCode,
				DisplayName:        fr.FileName,
				DataState:          fr.DataState,
				VersionNo:          "V1.0",
				Required:           fr.Required,
				LifecycleStatus:    "planned",
			})
		}
	}

	// 5.6 写入 project_members
	membersOut := []models.ProjectMember{}
	createdByEnrolled := false // 立项人是否已经在传入的 members 列表中
	for _, m := range in.Members {
		// 把 stage_codes 转换为本项目实例的 stage_ids JSON
		var stageIDs []int64
		for _, code := range m.StageCodes {
			if id, ok := stageIDByCode[code]; ok {
				stageIDs = append(stageIDs, id)
			}
		}
		stageIDJSON := ""
		if len(stageIDs) > 0 {
			b, _ := json.Marshal(stageIDs)
			stageIDJSON = string(b)
		}
		permJSON, _ := json.Marshal(m.PermissionActions)
		mid, err := s.memberRepo.Insert(tx, CreateProjectMemberInput{
			ProjectID:         projectID,
			UserID:            m.UserID,
			SubjectID:         m.SubjectID,
			RoleCode:          m.RoleCode,
			StageIDs:          nullableJSON(stageIDJSON),
			PermissionActions: string(permJSON),
		})
		if err != nil {
			return nil, fmt.Errorf("写 project_members 失败: %w", err)
		}
		membersOut = append(membersOut, models.ProjectMember{
			ID:                mid,
			ProjectID:         projectID,
			UserID:            m.UserID,
			SubjectID:         m.SubjectID,
			RoleCode:          m.RoleCode,
			StageIDs:          nullableJSON(stageIDJSON),
			PermissionActions: string(permJSON),
		})
		// 标记：立项人是否已在 members 列表里
		if in.CreatedByUserID > 0 && m.UserID != nil && *m.UserID == in.CreatedByUserID {
			createdByEnrolled = true
		}
	}

	// V2-3 立项人自动 enroll（需求文档 §3.5.2：'可由立项人默认带入...
	// 应与项目成员角色授权联动'）
	// 若 CreatedByUserID > 0 且尚未在 members 中，自动添加为项目负责人 + 全权限。
	if in.CreatedByUserID > 0 && !createdByEnrolled {
		uid := in.CreatedByUserID
		defaultPerms := []string{"read", "write", "receive", "submit", "share", "archive", "close"}
		permJSON, _ := json.Marshal(defaultPerms)
		mid, err := s.memberRepo.Insert(tx, CreateProjectMemberInput{
			ProjectID:         projectID,
			UserID:            &uid,
			SubjectID:         0,
			RoleCode:          "项目负责人",
			StageIDs:          nil,
			PermissionActions: string(permJSON),
		})
		if err != nil {
			return nil, fmt.Errorf("自动加入立项人为项目负责人失败: %w", err)
		}
		membersOut = append(membersOut, models.ProjectMember{
			ID:                mid,
			ProjectID:         projectID,
			UserID:            &uid,
			RoleCode:          "项目负责人",
			PermissionActions: string(permJSON),
		})
	}

	// 5.7 创建本地目录树（事务即将提交前；目录创建失败也算事务失败）
	if err := workspace.CreateProjectTree(projectCode, stageCodes); err != nil {
		return nil, fmt.Errorf("创建项目目录失败: %w", err)
	}

	// 5.8 写入 project_stages 的 directory_path（已在插入时写入）+ data_projects.project_root（已在插入时写入）

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交事务失败: %w", err)
	}
	committed = true

	// 重新读取 project 以拿到完整字段
	created, err := s.projectRepo.FindByCode(projectCode)
	if err != nil {
		return nil, fmt.Errorf("读取已创建项目失败: %w", err)
	}

	return &InstantiateOutput{
		Project: created,
		Stages:  stagesOut,
		Members: membersOut,
	}, nil
}

// validateInstantiateInput 入参校验
func validateInstantiateInput(in *InstantiateInput) error {
	if strings.TrimSpace(in.TemplateCode) == "" || strings.TrimSpace(in.TemplateVersion) == "" {
		return fmt.Errorf("template_code 和 template_version 必填")
	}
	if strings.TrimSpace(in.ProjectName) == "" {
		return fmt.Errorf("project_name 必填")
	}
	if err := ValidateObjectShortCode(in.ObjectShortCode); err != nil {
		return err
	}
	if in.OwnerSubjectID == 0 || in.CustodianSubjectID == 0 || in.SecuritySubjectID == 0 {
		return fmt.Errorf("三主体（归属/保管/安全）必填")
	}
	switch in.SensitivityLevel {
	case SensGeneral, SensImportant, SensCoreSecret:
		// ok
	case "":
		return fmt.Errorf("sensitivity_level 必填")
	default:
		return fmt.Errorf("sensitivity_level 非法：%s", in.SensitivityLevel)
	}
	// V2：CreatedByUserID 提供时立项人会自动 enroll，Members 可以为空
	if len(in.Members) == 0 && in.CreatedByUserID <= 0 {
		return fmt.Errorf("至少配置一个项目成员")
	}
	return nil
}

// nullable string -> *string
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullableInt64 把 int64 转 *int64：<= 0 视为未设置返回 nil。
// 用于 V2 审计字段 OperatorUserID / CreatedByUserID 写入：0 表示未识别用户。
func nullableInt64(n int64) *int64 {
	if n <= 0 {
		return nil
	}
	return &n
}

func nullableJSON(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func defaultStr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
