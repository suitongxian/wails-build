package repository

import (
	"strings"

	"data-asset-scan-go/internal/models"
)

const (
	ArchiveActionNoSync       = "no_sync"
	ArchiveActionSync         = "sync"
	ArchiveActionManualReview = "manual_review"
)

// ArchiveDecision 是 scan 端对“是否进入 manage，以及进入哪个柜/室”的唯一判断结果。
//
// 这里刻意把“个人归目”和“正式归档”分开：个人文件只挂账归目，正式项目在
// 文件状态达到部门定稿或单位发布后，才生成可上报 manage 的归档目标。
type ArchiveDecision struct {
	Action       string `json:"action"`
	TargetTier   string `json:"target_tier,omitempty"`
	ArchivePhase string `json:"archive_phase,omitempty"`
	FileState    string `json:"file_state"`
	StorageLabel string `json:"storage_label,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

func IsPersonalProjectCode(projectCode string) bool {
	return strings.HasPrefix(projectCode, "SYS-PERSONAL-")
}

func DecideFileVersionArchiveTarget(project *models.DataProject, fv *models.FileVersion) ArchiveDecision {
	if project == nil {
		return ArchiveDecision{Action: ArchiveActionManualReview, Reason: "缺少项目信息"}
	}
	fileState := ""
	if fv != nil {
		fileState = ResolveFileState(fv)
	}
	return DecideArchiveTargetForState(project.ProjectCode, project.SensitivityLevel, fileState)
}

func DecideArchiveTargetForState(projectCode, sensitivityLevel, fileState string) ArchiveDecision {
	if IsPersonalProjectCode(projectCode) {
		return ArchiveDecision{
			Action:    ArchiveActionNoSync,
			FileState: fileState,
			Reason:    "个人文件只做本地归目和来源记录，不自动同步 manage",
		}
	}

	switch fileState {
	case FileStatePersonalProcess, FileStatePersonalFinal, FileStateDeptStage:
		return ArchiveDecision{
			Action:    ArchiveActionNoSync,
			FileState: fileState,
			Reason:    "尚未形成归档上报事件",
		}
	case FileStateDeptFinal:
		if normalizeArchiveLevel(sensitivityLevel) == SensCoreSecret {
			return syncDecision(sensitivityLevel, fileState, StorageTierSecureRoom)
		}
		return syncDecision(sensitivityLevel, fileState, StorageTierDepartmentCabinet)
	case FileStateUnitRelease:
		if normalizeArchiveLevel(sensitivityLevel) == SensCoreSecret {
			return syncDecision(sensitivityLevel, fileState, StorageTierSecureRoom)
		}
		return syncDecision(sensitivityLevel, fileState, StorageTierUnitArchive)
	}

	return ArchiveDecision{
		Action:    ArchiveActionManualReview,
		FileState: fileState,
		Reason:    "未知文件状态，需人工确认归档目标",
	}
}

func syncDecision(sensitivityLevel, fileState, targetTier string) ArchiveDecision {
	return ArchiveDecision{
		Action:       ArchiveActionSync,
		TargetTier:   targetTier,
		ArchivePhase: targetTier,
		FileState:    fileState,
		StorageLabel: ResolveStorageLabel(sensitivityLevel, fileState),
	}
}

func normalizeArchiveLevel(level string) string {
	return level
}
