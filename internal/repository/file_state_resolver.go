package repository

import (
	"data-asset-scan-go/internal/models"
)

// ResolveFileState 把 (data_state, stage_type, lifecycle_status) 映射到安全基线表的 file_state
//
// 设计依据《数据业务模版程序设计文档》7.7 安全等级 × 文件版本状态二维基线：
//
//	personal_process —— 个人在工作过程中产生的中间稿（input/process 默认）
//	personal_final   —— 个人定稿（process 提交后即将进入下游环节）
//	dept_stage       —— 部门工作环节版本（已被下游领取，跨人共享）
//	dept_final       —— 部门项目定稿（output 提交，进入归档前）
//	unit_release     —— 单位发布版本（项目结项归档后）
//
// 当前 V1 实现采用保守映射；后续可结合环节类型 / sealed 标志细化。
//
// 映射策略：
//
//	data_state=input
//	  - lifecycle=registered（直接领取） → dept_stage（来自下游领取，跨主体）
//	  - 其他                         → personal_process
//	data_state=process
//	  - lifecycle in {sealed, permanent} → dept_stage
//	  - 其他                            → personal_process
//	data_state=output
//	  - submitted_at != nil（已提交） → dept_final
//	  - lifecycle in {sealed, permanent} → unit_release
//	  - 其他                            → personal_final
func ResolveFileState(fv *models.FileVersion) string {
	switch fv.DataState {
	case "input":
		// 已被领取的输入即来自上游 fv（source_file_version_id 不为空）
		// 视为部门环节版本而非个人过程稿
		if fv.SourceFileVersionID != nil && fv.LifecycleStatus == "registered" {
			return FileStateDeptStage
		}
		return FileStatePersonalProcess
	case "process":
		if fv.LifecycleStatus == "sealed" || fv.LifecycleStatus == "permanent" {
			return FileStateDeptStage
		}
		return FileStatePersonalProcess
	case "output":
		if fv.LifecycleStatus == "sealed" || fv.LifecycleStatus == "permanent" {
			return FileStateUnitRelease
		}
		if fv.SubmittedAt != nil {
			return FileStateDeptFinal
		}
		return FileStatePersonalFinal
	}
	return FileStatePersonalProcess
}

// ResolveSecurityPolicyID 根据敏感等级 + 文件状态查找匹配的 security_policies.id
//
// 找不到时返回 nil（不阻塞业务）。
func ResolveSecurityPolicyID(repo *SecurityPolicyRepository, sensitivityLevel string, fv *models.FileVersion) *int64 {
	state := ResolveFileState(fv)
	policy, err := repo.FindByLevelAndState(sensitivityLevel, state)
	if err != nil || policy == nil {
		return nil
	}
	id := policy.ID
	return &id
}

// V4-Q4 §3.6 九宫格存储基线 — (敏感等级 × 文件版本状态) → 中文存储位置 label
//
// 文档 §3.6 列了一张 3 级敏感 × 5 种文件状态 的二维表，共 15 个具体存储位置名称。
//
// 当前 storage_tier 4 个枚举（personal_folder / department_cabinet /
// unit_archive / secure_room）是宏观分类；本函数返回的是更细的中文标签，
// 便于 UI 直接展示给业务用户。
//
// 注意：返回 label 不影响实际存储位置（按 Q4 决策"只记字符串不搬文件"）。
func ResolveStorageLabel(sensitivityLevel, fileState string) string {
	level := sensitivityLevel

	type cell struct {
		level, state string
	}
	table := map[cell]string{
		// 核心（涉密）
		{SensCoreSecret, FileStatePersonalProcess}: "个人核心文件保密夹",
		{SensCoreSecret, FileStatePersonalFinal}:   "部门核心项目保密柜",
		{SensCoreSecret, FileStateDeptStage}:       "部门核心项目保密柜",
		{SensCoreSecret, FileStateDeptFinal}:       "单位核心要件保密室",
		{SensCoreSecret, FileStateUnitRelease}:     "单位核心要件保密室",

		// 重要（权威）
		{SensImportant, FileStatePersonalProcess}: "个人重要文件档案夹",
		{SensImportant, FileStatePersonalFinal}:   "个人重要文件档案夹",
		{SensImportant, FileStateDeptStage}:       "部门重要项目档案柜",
		{SensImportant, FileStateDeptFinal}:       "部门重要项目档案柜",
		{SensImportant, FileStateUnitRelease}:     "单位重要文件档案室",

		// 一般（开放） — 注意 personal_final 升档为个人重要档案夹（文档 §3.6 明列）
		{SensGeneral, FileStatePersonalProcess}: "个人一般文件资料夹",
		{SensGeneral, FileStatePersonalFinal}:   "个人重要文件档案夹",
		{SensGeneral, FileStateDeptStage}:       "部门一般项目资料柜",
		{SensGeneral, FileStateDeptFinal}:       "部门一般项目资料柜",
		{SensGeneral, FileStateUnitRelease}:     "单位一般文本资料室",
	}
	if label, ok := table[cell{level, fileState}]; ok {
		return label
	}
	// 兜底：未知组合返回 storage_tier 中文
	return tierLabelZh(StorageTierPersonalFolder)
}

// tierLabelZh storage_tier 枚举的中文 fallback label
func tierLabelZh(tier string) string {
	switch tier {
	case StorageTierPersonalFolder:
		return "个人文件夹"
	case StorageTierDepartmentCabinet:
		return "部门文件柜"
	case StorageTierUnitArchive:
		return "单位文件室"
	case StorageTierSecureRoom:
		return "单位机要室"
	}
	return tier
}

// FileVersionSecurityInfo §3.6 九宫格暴露给前端的聚合视图
type FileVersionSecurityInfo struct {
	FileVersionID    int64  `json:"file_version_id"`
	SensitivityLevel string `json:"sensitivity_level"` // 来自项目（V1 项目级敏感等级）
	FileState        string `json:"file_state"`        // ResolveFileState(fv) 算的 5 个枚举之一
	FileStateLabel   string `json:"file_state_label"`  // 中文
	StorageTier      string `json:"storage_tier"`      // 4 个宏观枚举
	StorageLabel     string `json:"storage_label"`     // 九宫格中文（15 种）
	PolicyID         *int64 `json:"policy_id"`         // 命中的 security_policies.id
}

// ResolveFileVersionSecurity 给前端返回完整的安全视图（项目敏感等级 + fv 状态 + 命中策略 + 中文 label）
func ResolveFileVersionSecurity(
	policyRepo *SecurityPolicyRepository,
	projectSensitivity string,
	fv *models.FileVersion,
) FileVersionSecurityInfo {
	state := ResolveFileState(fv)
	policyID := ResolveSecurityPolicyID(policyRepo, projectSensitivity, fv)

	// 从命中的策略拿 storage_tier，未命中时按基线推断
	storageTier := StorageTierPersonalFolder
	if policyID != nil {
		if pol, err := policyRepo.FindByID(*policyID); err == nil && pol != nil {
			storageTier = pol.StorageTier
		}
	}

	return FileVersionSecurityInfo{
		FileVersionID:    fv.ID,
		SensitivityLevel: projectSensitivity,
		FileState:        state,
		FileStateLabel:   fileStateLabelZh(state),
		StorageTier:      storageTier,
		StorageLabel:     ResolveStorageLabel(projectSensitivity, state),
		PolicyID:         policyID,
	}
}

func fileStateLabelZh(state string) string {
	switch state {
	case FileStatePersonalProcess:
		return "个人工作过程版本"
	case FileStatePersonalFinal:
		return "个人工作定稿版本"
	case FileStateDeptStage:
		return "部门工作环节版本"
	case FileStateDeptFinal:
		return "部门项目定稿版本"
	case FileStateUnitRelease:
		return "单位最终发布版本"
	}
	return state
}
