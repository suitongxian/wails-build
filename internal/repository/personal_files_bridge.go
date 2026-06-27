package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// V4-Q2 §4.2 part 2 — classify 完成后桥接挂账到 3 个内置个人文件归目容器
//
// 桥接逻辑：
//   归目保护 (SingleClassifyResource) 设置 data_resources.importance_level 后
//     ↓
//   按 importance_level 路由到对应内置容器：
//     1 (核心) → SYS-PERSONAL-CORE
//     2 (重要) → SYS-PERSONAL-IMPORTANT
//     3 (一般) → SYS-PERSONAL-GENERAL
//     4 (隐私) → 不桥接（个人隐私保护是独立流程）
//     其他    → 不桥接
//     ↓
//   创建 file_version + asset_ledger，并在 source_ref 中记录 data_resources 来源。
//   这里是归目挂账和标识编码，不写 storage_uri，不复制文件，不迁移过程版本。
//
// 幂等：用 file_version_code = <project_code>-GR-DA-OUT-001-DR<resource_id>
//   保证同一资源重复 classify 不重复挂账（已存在则更新一遍 importance_level）

// importanceToProjectCode V4-Q2 importance_level → 内置项目编码
//
// importance_level 1=核心 2=重要 3=一般 4=隐私 (V1 既有约定)
// 4 隐私不在 3 个内置项目里，简版 §4.2 只覆盖核心/重要/一般。
func importanceToProjectCode(level int) string {
	switch level {
	case 1:
		return PersonalCoreProjectCode
	case 2:
		return PersonalImportantProjectCode
	case 3:
		return PersonalGeneralProjectCode
	}
	return ""
}

func personalWorkItemSummary(name, desc, subject *string) *string {
	workItem := ""
	if subject != nil {
		workItem = strings.TrimSpace(*subject)
	}
	if workItem == "" || workItem == "file" {
		workItem = inferWorkItemFromName(name)
	}
	if workItem == "" && desc != nil {
		workItem = strings.TrimSpace(*desc)
	}
	if workItem == "" {
		return nil
	}
	summary := "工作事项/主题：" + workItem
	return &summary
}

func inferWorkItemFromName(name *string) string {
	if name == nil {
		return ""
	}
	s := strings.TrimSpace(*name)
	if s == "" {
		return ""
	}
	if dot := strings.LastIndex(s, "."); dot > 0 {
		s = s[:dot]
	}
	for _, sep := range []string{" - ", "-", "_", "（", "(", "【", "["} {
		if idx := strings.Index(s, sep); idx > 1 {
			return strings.TrimSpace(s[:idx])
		}
	}
	return s
}

// FamilyBatchArchiveItem family 批量归目挂账单条结果（per member）
type FamilyBatchArchiveItem struct {
	ResourceID    int64  `json:"resource_id"`
	ResourceName  string `json:"resource_name,omitempty"`
	FileVersionID int64  `json:"file_version_id,omitempty"`
	Status        string `json:"status"` // archived(legacy: 已挂账) / skipped_already / error
	ErrorMsg      string `json:"error_msg,omitempty"`
}

// FamilyBatchArchiveResult family 批量归目挂账总结
type FamilyBatchArchiveResult struct {
	FamilyID       int64                    `json:"family_id"`
	ProjectCode    string                   `json:"project_code"`
	StageCode      string                   `json:"stage_code"`
	FileRuleCode   string                   `json:"file_rule_code"`
	Total          int                      `json:"total"`
	Archived       int                      `json:"archived"`
	SkippedAlready int                      `json:"skipped_already"`
	Errors         int                      `json:"errors"`
	Details        []FamilyBatchArchiveItem `json:"details"`
}

// BridgeTargetProject 通用归目目标（项目 + 环节 + 文件规则三元组）
//
// Q5 batch family + Q1 AI apply 共用此结构。
type BridgeTargetProject struct {
	ProjectID          int64
	ProjectCode        string
	SensitivityLevel   string
	OwnerSubjectID     int64
	CustodianSubjectID int64
	SecuritySubjectID  int64
	StageID            int64
	StageCode          string
	FileRuleCode       string
	FileRuleID         int64
	DataState          string
}

// resolveBridgeTarget 校验 project + stage + rule 三元组并返回完整 target
//
// 失败返回 (BridgeTargetProject{}, error)；成功返回 target 可被 bridgeOneResource 使用。
func resolveBridgeTarget(db *sqlx.DB, projectID int64, stageCode, fileRuleCode string) (BridgeTargetProject, error) {
	var t BridgeTargetProject
	t.StageCode = stageCode
	t.FileRuleCode = fileRuleCode

	type projRow struct {
		ID                 int64  `db:"id"`
		ProjectCode        string `db:"project_code"`
		Status             string `db:"status"`
		SensitivityLevel   string `db:"sensitivity_level"`
		OwnerSubjectID     int64  `db:"owner_subject_id"`
		CustodianSubjectID int64  `db:"custodian_subject_id"`
		SecuritySubjectID  int64  `db:"security_subject_id"`
	}
	var proj projRow
	if err := db.Get(&proj, `SELECT id, project_code, status, sensitivity_level,
		owner_subject_id, custodian_subject_id, security_subject_id
		FROM data_projects WHERE id = ? AND disable = 0`, projectID); err != nil {
		return t, fmt.Errorf("项目不存在: %w", err)
	}
	if proj.Status != "active" && proj.Status != "draft" {
		return t, fmt.Errorf("项目状态 %s 不允许归目挂账（仅 draft/active）", proj.Status)
	}
	t.ProjectID = proj.ID
	t.ProjectCode = proj.ProjectCode
	t.SensitivityLevel = proj.SensitivityLevel
	t.OwnerSubjectID = proj.OwnerSubjectID
	t.CustodianSubjectID = proj.CustodianSubjectID
	t.SecuritySubjectID = proj.SecuritySubjectID

	if err := db.Get(&t.StageID, `SELECT id FROM project_stages
		WHERE project_id = ? AND stage_code = ? AND disable = 0`, projectID, stageCode); err != nil {
		return t, fmt.Errorf("环节 %s 在项目 %s 下不存在: %w", stageCode, proj.ProjectCode, err)
	}
	type ruleRow struct {
		ID        int64  `db:"id"`
		DataState string `db:"data_state"`
	}
	var rule ruleRow
	if err := db.Get(&rule, `SELECT tfr.id, tfr.data_state FROM template_file_rules tfr
		JOIN template_stages ts ON ts.id = tfr.template_stage_id
		JOIN project_stages ps ON ps.template_stage_id = ts.id
		WHERE ps.id = ? AND tfr.file_rule_code = ? AND tfr.disable = 0
		LIMIT 1`, t.StageID, fileRuleCode); err != nil {
		return t, fmt.Errorf("文件规则 %s 在环节 %s 下不存在: %w", fileRuleCode, stageCode, err)
	}
	t.FileRuleID = rule.ID
	t.DataState = rule.DataState
	return t, nil
}

// bridgeOneResource 把一个 data_resource 挂到 target 三元组下
//
// Q5 batch family 和 Q1 AI apply 共用本函数。
// 幂等：file_version_code = <project_code>-<stage_code>-<rule_code>-DR<resource_id>
func bridgeOneResource(db *sqlx.DB, target BridgeTargetProject, resourceID int64) (FamilyBatchArchiveItem, error) {
	item := FamilyBatchArchiveItem{ResourceID: resourceID}
	type drRow struct {
		ContentSign    string  `db:"content_sign"`
		ResourcesName  *string `db:"resources_name"`
		ResourcesDesc  *string `db:"resources_desc"`
		ContentSubject *string `db:"content_subject"`
	}
	var dr drRow
	if err := db.Get(&dr, `SELECT content_sign, resources_name, resources_desc, content_subject FROM data_resources WHERE data_resources_id = ?`, resourceID); err != nil {
		item.Status = "error"
		item.ErrorMsg = "查询 data_resource 失败: " + err.Error()
		return item, err
	}
	if dr.ResourcesName != nil {
		item.ResourceName = *dr.ResourcesName
	}

	fvCode := fmt.Sprintf("%s-%s-%s-DR%d", target.ProjectCode, target.StageCode, target.FileRuleCode, resourceID)
	var existing int64
	if err := db.Get(&existing, `SELECT id FROM file_versions WHERE file_version_code = ?`, fvCode); err == nil && existing > 0 {
		item.Status = "skipped_already"
		item.FileVersionID = existing
		return item, nil
	}

	displayName := item.ResourceName
	if displayName == "" {
		displayName = "归目文件"
	}
	now := time.Now()
	ruleIDPtr := target.FileRuleID
	checksumPtr := dr.ContentSign

	fvRes, err := db.Exec(`INSERT INTO file_versions (
		project_id, project_stage_id, template_file_rule_id, file_version_code, local_code,
		display_name, data_state, version_no, required, checksum, lifecycle_status,
		created_by, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, 'V1.0', 0, ?, 'registered', 'system', ?, ?, 0)`,
		target.ProjectID, target.StageID, &ruleIDPtr, fvCode, target.FileRuleCode,
		displayName, target.DataState, &checksumPtr, now, now)
	if err != nil {
		item.Status = "error"
		item.ErrorMsg = "挂账 file_version 失败: " + err.Error()
		return item, err
	}
	fvID, _ := fvRes.LastInsertId()
	item.FileVersionID = fvID

	ledgerCode, err := GenerateLedgerCode(db, now)
	if err == nil {
		contentSummary := personalWorkItemSummary(dr.ResourcesName, dr.ResourcesDesc, dr.ContentSubject)
		_, _ = db.Exec(`INSERT INTO asset_ledgers (
			ledger_code, file_version_id, project_code, stage_code, file_version_code, asset_name,
			content_summary, owner_subject_id, custodian_subject_id, security_subject_id,
			sensitivity_level, marking_method, lifecycle_status,
			source_ref, create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'reference', 'registered', ?, ?, ?, 0)`,
			ledgerCode, fvID, target.ProjectCode, target.StageCode, fvCode, displayName,
			contentSummary,
			target.OwnerSubjectID, target.CustodianSubjectID, target.SecuritySubjectID,
			target.SensitivityLevel,
			fmt.Sprintf(`{"bridge_from":"resource","resource_id":%d}`, resourceID),
			now, now)
	}

	_, _ = db.Exec(`INSERT INTO lifecycle_events (
		file_version_id, event_type, event_name, operator_id, reason, create_time
	) VALUES (?, ?, ?, 'system', ?, ?)`,
		fvID, EventRegister, "AI 归目挂账",
		fmt.Sprintf("V4-Q1 桥接：resource(%d) → %s/%s/%s",
			resourceID, target.ProjectCode, target.StageCode, target.FileRuleCode),
		now)

	item.Status = "archived"
	return item, nil
}

// BridgeResourceToTarget V4-Q1-b AI 归目应用接口
//
// 把单个 data_resource 挂到指定项目/环节/规则下，返回挂账结果。
// 与 BridgeFamilyToProject 区别：那个批量整个 family，这个仅 1 条。
func BridgeResourceToTarget(db *sqlx.DB, resourceID, projectID int64, stageCode, fileRuleCode string) (FamilyBatchArchiveItem, error) {
	target, err := resolveBridgeTarget(db, projectID, stageCode, fileRuleCode)
	if err != nil {
		return FamilyBatchArchiveItem{ResourceID: resourceID, Status: "error", ErrorMsg: err.Error()}, err
	}
	return bridgeOneResource(db, target, resourceID)
}

// BridgeFamilyToProject §4.3.5 简版"历史数据家族式自动归目" — 浅联动版（Q5 A）
//
// 把一个 family 的所有 member 批量挂到指定项目的指定环节文件规则下。
// 与 BridgeClassifyToPersonalProject 区别：
//   - 那个按 importance_level 自动路由到 3 个内置项目
//   - 这个让用户显式挑项目/环节/规则（任意 active 项目）
//
// 行为：
//   - family 内已挂账的 member（同一 project/stage/rule + content_sign）跳过，标 skipped_already
//   - 其他 member 走单条挂账逻辑
//   - 返回每个 member 的状态详情
//
// 幂等：file_version_code = <project_code>-<stage_code>-<file_rule_code>-DR<resource_id>
//
//	确保同一 resource 在同一项目/环节/规则下重复归目不重复挂账。
func BridgeFamilyToProject(db *sqlx.DB, familyID, projectID int64, stageCode, fileRuleCode string) (*FamilyBatchArchiveResult, error) {
	// 1-3: 校验目标项目/环节/规则三元组
	target, err := resolveBridgeTarget(db, projectID, stageCode, fileRuleCode)
	if err != nil {
		return nil, err
	}

	// 4. 取 family 所有 member
	famRepo := NewFamilyRepository(db)
	memberIDs, err := famRepo.IDsInFamily(familyID)
	if err != nil {
		return nil, fmt.Errorf("查询家族成员失败: %w", err)
	}
	if len(memberIDs) == 0 {
		return nil, fmt.Errorf("家族 %d 无成员或不存在", familyID)
	}

	result := &FamilyBatchArchiveResult{
		FamilyID:     familyID,
		ProjectCode:  target.ProjectCode,
		StageCode:    stageCode,
		FileRuleCode: fileRuleCode,
		Total:        len(memberIDs),
		Details:      make([]FamilyBatchArchiveItem, 0, len(memberIDs)),
	}

	// 5. 逐个 member 走通用 single-resource 桥接
	for _, resID := range memberIDs {
		item, _ := bridgeOneResource(db, target, resID)
		result.Details = append(result.Details, item)
		switch item.Status {
		case "archived":
			result.Archived++
		case "skipped_already":
			result.SkippedAlready++
		case "error":
			result.Errors++
		}
	}

	return result, nil
}

// BridgeFamilyToProjectSplit §4.3-6 历史数据家族式自动归目：过程 + 最新分流
//
// 把 family 内 member 按 first_create_time 倒序分流：
//   - 最新的 1 条（first_create_time 最大）→ final 目标
//   - 其余 → process 目标
//
// 若 process 与 final 目标三元组相同，等价于 BridgeFamilyToProject 单目标行为。
//
// 幂等：file_version_code = <project_code>-<stage_code>-<rule_code>-DR<resource_id>
//
//	确保同一 resource 在同一目标下重复归目不重复挂账。
func BridgeFamilyToProjectSplit(db *sqlx.DB, familyID, projectID int64,
	processStageCode, processRuleCode, finalStageCode, finalRuleCode string,
) (*FamilyBatchArchiveResult, error) {
	processTarget, err := resolveBridgeTarget(db, projectID, processStageCode, processRuleCode)
	if err != nil {
		return nil, fmt.Errorf("process 目标无效: %w", err)
	}
	finalTarget, err := resolveBridgeTarget(db, projectID, finalStageCode, finalRuleCode)
	if err != nil {
		return nil, fmt.Errorf("final 目标无效: %w", err)
	}

	// 取 family 成员，按 first_create_time 倒序（最新的排第一）。
	// family 成员关系存在 data_resources.family_id 上（参考 family.go 的 IDsInFamily）。
	type memberRow struct {
		ID    int64     `db:"data_resources_id"`
		Ctime time.Time `db:"first_create_time"`
	}
	var members []memberRow
	if err := db.Select(&members, `
		SELECT data_resources_id, first_create_time
		FROM data_resources
		WHERE family_id = ? AND disable = 0
		ORDER BY first_create_time DESC, data_resources_id DESC`, familyID); err != nil {
		return nil, fmt.Errorf("查 family 成员: %w", err)
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("家族 %d 无成员或不存在", familyID)
	}

	result := &FamilyBatchArchiveResult{
		FamilyID:     familyID,
		ProjectCode:  finalTarget.ProjectCode,
		StageCode:    "split:" + processStageCode + "→" + finalStageCode,
		FileRuleCode: "split:" + processRuleCode + "→" + finalRuleCode,
		Total:        len(members),
		Details:      make([]FamilyBatchArchiveItem, 0, len(members)),
	}

	for i, m := range members {
		var target BridgeTargetProject
		if i == 0 {
			target = finalTarget // 最新归 final
		} else {
			target = processTarget // 其余归 process
		}
		item, _ := bridgeOneResource(db, target, m.ID)
		result.Details = append(result.Details, item)
		switch item.Status {
		case "archived":
			result.Archived++
		case "skipped_already":
			result.SkippedAlready++
		case "error":
			result.Errors++
		}
	}
	return result, nil
}

// resolvePersonalStage 根据 dataState hint 找该项目下对应的 stage + rule
//
// hint="output" / "" / 未知 → GR-FINAL + OUT-001（默认定稿）
// hint="process"           → GR-DRAFT + PRC-001
// 项目只有 GR-DA 单环节（V1.0 兼容路径）→ 直接返回 GR-DA + OUT-001
func resolvePersonalStage(db *sqlx.DB, projectID int64, dataStateHint string) (stageID int64, stageCode, ruleCode, dataState string, ruleID int64, err error) {
	type stageRow struct {
		ID   int64  `db:"id"`
		Code string `db:"stage_code"`
	}
	var stages []stageRow
	if err = db.Select(&stages, `SELECT id, stage_code FROM project_stages WHERE project_id = ? AND disable = 0 ORDER BY sort_order`, projectID); err != nil {
		return
	}
	stageByCode := map[string]int64{}
	for _, s := range stages {
		stageByCode[s.Code] = s.ID
	}

	if _, ok := stageByCode["GR-FINAL"]; ok {
		// V2.0 路径
		if dataStateHint == "process" {
			stageID = stageByCode["GR-DRAFT"]
			stageCode = "GR-DRAFT"
			ruleCode = "PRC-001"
			dataState = "process"
		} else {
			stageID = stageByCode["GR-FINAL"]
			stageCode = "GR-FINAL"
			ruleCode = "OUT-001"
			dataState = "output"
		}
	} else if _, ok := stageByCode["GR-DA"]; ok {
		// V1.0 兼容路径
		stageID = stageByCode["GR-DA"]
		stageCode = "GR-DA"
		ruleCode = "OUT-001"
		dataState = "output"
	} else {
		err = fmt.Errorf("项目 %d 未配置 personal 环节", projectID)
		return
	}

	// 查 rule id
	_ = db.Get(&ruleID, `SELECT tfr.id FROM template_file_rules tfr
		JOIN template_stages ts ON ts.id = tfr.template_stage_id
		JOIN project_stages ps ON ps.template_stage_id = ts.id
		WHERE ps.id = ? AND tfr.file_rule_code = ? AND tfr.disable = 0 LIMIT 1`,
		stageID, ruleCode)
	return
}

// BridgeClassifyToPersonalProject 把已 classify 的 data_resources 挂到对应内置项目
//
// 默认归"定稿"环节（V2.0=GR-FINAL/OUT-001/output；V1.0 兼容路径=GR-DA/OUT-001/output）。
// "认领归类保护"是用户显式确认的归目动作，默认按最终版本标记；过程版本仍可由
// dataStateHint="process" 或家族分流进入 GR-DRAFT/PRC-001。
//
// 返回 (newFvID, nil) 表示成功新建；
// 返回 (existingFvID, nil) 表示之前已挂账（幂等）；
// 返回 (0, nil) 表示 importance_level 不在 {1,2,3} 内、或内置项目未就绪——静默跳过；
// 返回 (0, err) 表示发生错误。
func BridgeClassifyToPersonalProject(db *sqlx.DB, resourceID int64) (int64, error) {
	return BridgeClassifyToPersonalProjectWithState(db, resourceID, "output")
}

// BridgeClassifyToPersonalProjectWithState 允许调用方显式指定 data_state 桥接定向
//
// dataStateHint="process" → GR-DRAFT/PRC-001（过程版本）
// dataStateHint="output" / "" / 其他 → GR-FINAL/OUT-001（默认定稿）
// 项目只有 V1.0 单环节 GR-DA → 全部走 GR-DA/OUT-001（兼容路径，hint 被忽略）
func BridgeClassifyToPersonalProjectWithState(db *sqlx.DB, resourceID int64, dataStateHint string) (int64, error) {
	// 1. 拉 data_resources
	type drRow struct {
		ID              int64   `db:"data_resources_id"`
		ContentSign     string  `db:"content_sign"`
		ImportanceLevel int     `db:"importance_level"`
		ResourcesName   *string `db:"resources_name"`
		ResourcesDesc   *string `db:"resources_desc"`
		ContentSubject  *string `db:"content_subject"`
	}
	var dr drRow
	if err := db.Get(&dr, `SELECT data_resources_id, content_sign, importance_level, resources_name, resources_desc, content_subject
		FROM data_resources WHERE data_resources_id = ?`, resourceID); err != nil {
		return 0, fmt.Errorf("查询 data_resource(%d) 失败: %w", resourceID, err)
	}

	// 2. importance_level 路由
	projectCode := importanceToProjectCode(dr.ImportanceLevel)
	if projectCode == "" {
		// 隐私/未分类等不桥接
		return 0, nil
	}

	// V5-P1.1: 撤掉 Q2 软阻止（之前在此通过查 SYS-PERSONAL-* 下非 cancelled fv 提前 return 0）。
	// 现在由 BridgeClassifyWithFamilyPropagation 在家族级做"传播 + 整族 split 桥接"，
	// 重复挂账由下面的 fvCode 幂等检查兜底，逻辑更清晰。

	// 3. 找内置项目（若未建好则静默跳过——可能模版未同步）
	type projRow struct {
		ID                 int64  `db:"id"`
		ProjectCode        string `db:"project_code"`
		SensitivityLevel   string `db:"sensitivity_level"`
		OwnerSubjectID     int64  `db:"owner_subject_id"`
		CustodianSubjectID int64  `db:"custodian_subject_id"`
		SecuritySubjectID  int64  `db:"security_subject_id"`
	}
	var proj projRow
	if err := db.Get(&proj, `SELECT id, project_code, sensitivity_level,
		owner_subject_id, custodian_subject_id, security_subject_id
		FROM data_projects WHERE project_code = ? AND disable = 0`, projectCode); err != nil {
		// 内置项目未就绪
		return 0, nil
	}

	// 4. 按 hint 解析 stage + rule（V2.0=GR-DRAFT/GR-FINAL；V1.0 回退 GR-DA）
	stageID, stageCode, ruleCode, dataState, ruleID, err := resolvePersonalStage(db, proj.ID, dataStateHint)
	if err != nil {
		return 0, fmt.Errorf("内置项目 %s 解析 personal 环节失败: %w", projectCode, err)
	}

	// 5. 幂等检查：file_version_code 含 stage_code + rule_code 双键，
	// V1.0 路径仍生成 <project>-GR-DA-OUT-001-DR<id>（与旧行兼容），
	// V2.0 路径生成 <project>-GR-DRAFT-PRC-001-DR<id> 或 <project>-GR-FINAL-OUT-001-DR<id>
	//
	// 注意：cancelled 的 fv 不视为占位（解绑后允许重建）。
	// 若同 code 已存在 cancelled 行，需要让新行能写入——
	// 兼容做法：在原 fv_code 后追加 "-Rn"（重新绑定 n 次）保持唯一。
	fvCode := fmt.Sprintf("%s-%s-%s-DR%d", projectCode, stageCode, ruleCode, resourceID)
	var existing int64
	if err := db.Get(&existing, `SELECT id FROM file_versions WHERE file_version_code = ? AND lifecycle_status != 'cancelled'`, fvCode); err == nil && existing > 0 {
		return existing, nil
	}
	// 若 cancelled 行占用 fvCode（unique 约束），加 -R<n> 后缀让新行可插入
	var cancelledCount int
	if err := db.Get(&cancelledCount, `SELECT COUNT(*) FROM file_versions WHERE file_version_code LIKE ? || '%'`, fvCode); err == nil && cancelledCount > 0 {
		fvCode = fmt.Sprintf("%s-R%d", fvCode, cancelledCount)
	}

	var ruleIDPtr *int64
	if ruleID > 0 {
		ruleIDPtr = &ruleID
	}

	// 6. 新建 file_version (registered 状态，直接挂账)
	now := time.Now()
	displayName := "标记定稿"
	if dataState == "process" {
		displayName = "过程版本"
	}
	if dr.ResourcesName != nil && *dr.ResourcesName != "" {
		displayName = *dr.ResourcesName
	}
	localCode := ruleCode
	checksum := dr.ContentSign
	checksumPtr := &checksum

	res, err := db.Exec(`INSERT INTO file_versions (
		project_id, project_stage_id, template_file_rule_id, file_version_code, local_code,
		display_name, data_state, version_no, required, checksum, lifecycle_status,
		created_by, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, 'V1.0', 0, ?, 'registered', 'system', ?, ?, 0)`,
		proj.ID, stageID, ruleIDPtr, fvCode, localCode, displayName, dataState, checksumPtr, now, now)
	if err != nil {
		return 0, fmt.Errorf("挂账 file_version 失败: %w", err)
	}
	fvID, _ := res.LastInsertId()

	// 7. 生成底账记录（registered）
	ledgerCode, err := GenerateLedgerCode(db, now)
	if err != nil {
		return fvID, fmt.Errorf("生成 ledger code 失败: %w", err)
	}
	assetName := displayName
	contentSummary := personalWorkItemSummary(dr.ResourcesName, dr.ResourcesDesc, dr.ContentSubject)
	_, _ = db.Exec(`INSERT INTO asset_ledgers (
		ledger_code, file_version_id, project_code, stage_code, file_version_code, asset_name,
		content_summary, owner_subject_id, custodian_subject_id, security_subject_id,
		sensitivity_level, marking_method, lifecycle_status,
		source_ref, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'reference', 'registered', ?, ?, ?, 0)`,
		ledgerCode, fvID, proj.ProjectCode, stageCode, fvCode, assetName,
		contentSummary,
		proj.OwnerSubjectID, proj.CustodianSubjectID, proj.SecuritySubjectID,
		proj.SensitivityLevel,
		fmt.Sprintf(`{"bridge_from":"data_resources","resource_id":%d}`, resourceID),
		now, now)

	// 8. 生命周期事件：register
	_, _ = db.Exec(`INSERT INTO lifecycle_events (
		file_version_id, event_type, event_name, operator_id, reason, create_time
	) VALUES (?, ?, '认领归目自动挂账', 'system', ?, ?)`,
		fvID, EventRegister,
		fmt.Sprintf("V4-Q2 桥接：data_resource(%d) → %s/%s/%s", resourceID, projectCode, stageCode, ruleCode),
		now)

	return fvID, nil
}

// FamilyClassifyResult V5-P1.1 §4.3-6 classify 桥接结果（含家族传播）
type FamilyClassifyResult struct {
	LeadResourceID  int64 `json:"lead_resource_id"`
	LeadFvID        int64 `json:"lead_fv_id"` // 主资源的 fv id（兼容 SingleClassifyResource 返回 bridged_fv）
	HasFamily       bool  `json:"has_family"`
	FamilyID        int64 `json:"family_id,omitempty"`
	PropagatedCount int   `json:"propagated_count"` // 同 family 内被 update importance_level 的成员数（含 lead）
	BridgedCount    int   `json:"bridged_count"`    // 成功 archived 数
	SkippedCount    int   `json:"skipped_count"`    // skipped_already
	ErrorCount      int   `json:"error_count"`
}

// BridgeClassifyWithFamilyPropagation V5-P1.1 §4.3-6 完整实现：
//
//   - 若 resource 有 family_id → 把 importance_level 传播到全 family + 整族按 split 桥接
//     （最新→GR-FINAL/OUT-001，其余→GR-DRAFT/PRC-001）
//   - 若无 family_id → 退化到原 BridgeClassifyToPersonalProject 单条行为
//   - importance_level 不在 {1,2,3} → 不桥接、不传播（与原行为一致）
//
// 调用方（SingleClassifyResource / BatchClassifyResources）负责事先 UPDATE
// 主 resource 的 importance_level；本函数读取后再传播给同族成员。
func BridgeClassifyWithFamilyPropagation(db *sqlx.DB, resourceID int64) (FamilyClassifyResult, error) {
	result := FamilyClassifyResult{LeadResourceID: resourceID}

	// 1. 拉主资源
	type drRow struct {
		ID              int64  `db:"data_resources_id"`
		FamilyID        *int64 `db:"family_id"`
		ImportanceLevel int    `db:"importance_level"`
	}
	var dr drRow
	if err := db.Get(&dr, `SELECT data_resources_id, family_id, importance_level
		FROM data_resources WHERE data_resources_id = ?`, resourceID); err != nil {
		return result, fmt.Errorf("查 resource(%d): %w", resourceID, err)
	}

	// 2. 路由判断：importance_level → projectCode；空表示不桥接（隐私/未分类等）
	projectCode := importanceToProjectCode(dr.ImportanceLevel)
	if projectCode == "" {
		return result, nil
	}

	// 3. 无 family：退化到单条桥接
	if dr.FamilyID == nil || *dr.FamilyID == 0 {
		fvID, err := BridgeClassifyToPersonalProject(db, resourceID)
		if err != nil {
			result.ErrorCount = 1
			return result, err
		}
		if fvID > 0 {
			result.LeadFvID = fvID
			result.BridgedCount = 1
		}
		result.PropagatedCount = 1 // 仅 lead 自身
		return result, nil
	}

	// 4. 有 family：传播 importance_level + split 桥接
	result.HasFamily = true
	result.FamilyID = *dr.FamilyID

	famRepo := NewFamilyRepository(db)
	memberIDs, err := famRepo.IDsInFamily(*dr.FamilyID)
	if err != nil {
		return result, fmt.Errorf("查 family(%d) 成员: %w", *dr.FamilyID, err)
	}
	if len(memberIDs) == 0 {
		// family_id 存在但找不到成员（异常态）—— 退化到单条
		fvID, err := BridgeClassifyToPersonalProject(db, resourceID)
		if err != nil {
			result.ErrorCount = 1
			return result, err
		}
		if fvID > 0 {
			result.LeadFvID = fvID
			result.BridgedCount = 1
		}
		result.PropagatedCount = 1
		return result, nil
	}

	// 4a. 传播 importance_level 到所有 family 成员（含 lead）
	{
		placeholders := make([]string, len(memberIDs))
		args := make([]interface{}, 0, len(memberIDs)+2)
		args = append(args, dr.ImportanceLevel, time.Now())
		for i, id := range memberIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query := fmt.Sprintf(
			`UPDATE data_resources SET importance_level = ?, update_time = ? WHERE data_resources_id IN (%s)`,
			strings.Join(placeholders, ","))
		res, err := db.Exec(query, args...)
		if err != nil {
			return result, fmt.Errorf("传播 importance_level: %w", err)
		}
		affected, _ := res.RowsAffected()
		result.PropagatedCount = int(affected)
	}

	// 4b. 找 project id
	var projectID int64
	if err := db.Get(&projectID, `SELECT id FROM data_projects WHERE project_code = ? AND disable = 0`, projectCode); err != nil {
		// 内置项目未就绪（模版未同步等）—— 不报错，但记录为 0 archived
		return result, nil
	}

	// 4c. 整族 split 桥接（最新→GR-FINAL/OUT-001，其余→GR-DRAFT/PRC-001）
	splitResult, err := BridgeFamilyToProjectSplit(db, *dr.FamilyID, projectID,
		"GR-DRAFT", "PRC-001", // process target
		"GR-FINAL", "OUT-001", // final target
	)
	if err != nil {
		// 退化路径：split 失败（多见于 V1.0 模版只有 GR-DA 单环节）—— 对所有 member 走单条桥接
		for _, mid := range memberIDs {
			fvID, _ := BridgeClassifyToPersonalProject(db, mid)
			if fvID > 0 {
				result.BridgedCount++
				if mid == resourceID {
					result.LeadFvID = fvID
				}
			}
		}
		return result, nil
	}

	result.BridgedCount = splitResult.Archived
	result.SkippedCount = splitResult.SkippedAlready
	result.ErrorCount = splitResult.Errors

	// 找 lead 自己的 fv id
	for _, d := range splitResult.Details {
		if d.ResourceID == resourceID {
			result.LeadFvID = d.FileVersionID
			break
		}
	}

	return result, nil
}

// SyncImportanceFromProjectCode 把 personal 项目代码映射到 importance_level 的目标值。
// 非个人项目返回 0，调用方应解读为"不动"。
func SyncImportanceFromProjectCode(projectCode string) int {
	switch projectCode {
	case PersonalCoreProjectCode:
		return 1
	case PersonalImportantProjectCode:
		return 2
	case PersonalGeneralProjectCode:
		return 3
	default:
		return 0
	}
}

// sqlxDBExec 让 SyncResourceImportance 能同时接 *sqlx.DB 与 *sqlx.Tx
type sqlxDBExec interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// SyncResourceImportance 在 apply 成功后把 data_resources.importance_level 同步到目标级别。
// projectCode 非 SYS-PERSONAL-* 则跳过。
func SyncResourceImportance(db sqlxDBExec, resourceID int64, projectCode string) error {
	target := SyncImportanceFromProjectCode(projectCode)
	if target == 0 {
		return nil
	}
	_, err := db.Exec(`UPDATE data_resources SET importance_level = ?, update_time = ? WHERE data_resources_id = ?`,
		target, time.Now(), resourceID)
	return err
}
