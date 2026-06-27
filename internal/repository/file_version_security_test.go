package repository

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/models"
)

// V4-Q4 ResolveFileVersionSecurity 端到端聚合视图：项目级敏感 + fv 状态 → label
func TestResolveFileVersionSecurity_OutputSubmittedDeptFinal(t *testing.T) {
	db := openTestDB(t)
	policyRepo := NewSecurityPolicyRepository(db)

	submitted := time.Now()
	fv := &models.FileVersion{
		ID:              42,
		DataState:       "output",
		LifecycleStatus: "registered",
		SubmittedAt:     &submitted, // 已提交 → dept_final
	}

	info := ResolveFileVersionSecurity(policyRepo, SensImportant, fv)
	if info.FileVersionID != 42 {
		t.Errorf("fv_id wrong: %d", info.FileVersionID)
	}
	if info.FileState != FileStateDeptFinal {
		t.Errorf("file_state = %s, want %s", info.FileState, FileStateDeptFinal)
	}
	if info.FileStateLabel != "部门项目定稿版本" {
		t.Errorf("file_state_label = %s", info.FileStateLabel)
	}
	if info.StorageLabel != "部门重要项目档案柜" {
		t.Errorf("storage_label = %s, want 部门重要项目档案柜", info.StorageLabel)
	}
	if info.StorageTier != StorageTierDepartmentCabinet {
		t.Errorf("storage_tier = %s", info.StorageTier)
	}
}

// V4-Q4 输入文件 + planned → personal_process
func TestResolveFileVersionSecurity_InputPlannedCore(t *testing.T) {
	db := openTestDB(t)
	policyRepo := NewSecurityPolicyRepository(db)
	fv := &models.FileVersion{
		ID: 1, DataState: "input", LifecycleStatus: "planned",
	}
	info := ResolveFileVersionSecurity(policyRepo, SensCoreSecret, fv)
	if info.FileState != FileStatePersonalProcess {
		t.Errorf("file_state = %s", info.FileState)
	}
	if info.StorageLabel != "个人核心文件保密夹" {
		t.Errorf("core_secret + personal_process 应为'个人核心文件保密夹', got %s", info.StorageLabel)
	}
}

// V4-Q4 sealed output → unit_release
func TestResolveFileVersionSecurity_SealedOutputGeneral(t *testing.T) {
	db := openTestDB(t)
	policyRepo := NewSecurityPolicyRepository(db)
	fv := &models.FileVersion{
		ID: 3, DataState: "output", LifecycleStatus: "sealed",
	}
	info := ResolveFileVersionSecurity(policyRepo, SensGeneral, fv)
	if info.FileState != FileStateUnitRelease {
		t.Errorf("file_state = %s", info.FileState)
	}
	if info.StorageLabel != "单位一般文本资料室" {
		t.Errorf("general + unit_release: %s", info.StorageLabel)
	}
}
