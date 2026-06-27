package repository

import (
	"strings"
	"testing"
)

// V3-UI option C: 环节切到 completed 后，该环节上的文件操作必须被拒
func TestStageMutability_BlockUploadWhenCompleted(t *testing.T) {
	db, svc, _, stages := setupProjectForFileOps(t)

	// 找一个 input fv
	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	if fv == nil {
		t.Skip("MZ-SG/IN-001 not in fixture")
	}

	// 把它所在环节切到 completed
	stageRepo := NewProjectStageRepository(db)
	stageRepo.SetStatus(fv.ProjectStageID, "completed") // 直接 SetStatus 绕状态机，模拟 V3-3 切换

	src := writeTempFile(t, "x.pdf", "x")
	_, err := svc.UploadOrBind(fv.ID, UploadInput{
		SourcePath: src, OriginalFileName: "x.pdf",
		OperatorID: "u",
	})
	if err == nil {
		t.Fatal("环节 completed 应当拒绝上传")
	}
	if !strings.Contains(err.Error(), "已完成") {
		t.Errorf("错误信息应包含'已完成'，got: %v", err)
	}
}

// V3-UI option C: skipped 状态拒绝上传
func TestStageMutability_BlockUploadWhenSkipped(t *testing.T) {
	db, svc, _, stages := setupProjectForFileOps(t)
	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	if fv == nil {
		t.Skip()
	}
	stageRepo := NewProjectStageRepository(db)
	stageRepo.SetStatus(fv.ProjectStageID, "skipped")

	src := writeTempFile(t, "x.pdf", "x")
	_, err := svc.UploadOrBind(fv.ID, UploadInput{
		SourcePath: src, OriginalFileName: "x.pdf",
		OperatorID: "u",
	})
	if err == nil {
		t.Fatal("环节 skipped 应当拒绝上传")
	}
	if !strings.Contains(err.Error(), "跳过") {
		t.Errorf("错误信息应包含'跳过'，got: %v", err)
	}
}

// V3-UI option C: 撤销跳过（skipped → pending）后允许上传
func TestStageMutability_AllowUploadAfterUnskip(t *testing.T) {
	db, svc, _, stages := setupProjectForFileOps(t)
	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	if fv == nil {
		t.Skip()
	}
	stageRepo := NewProjectStageRepository(db)
	stageRepo.SetStatus(fv.ProjectStageID, "skipped")
	// 撤销跳过 → 通过状态机正常转回 pending
	if err := stageRepo.UpdateStageStatus(fv.ProjectStageID, "pending"); err != nil {
		t.Fatalf("skipped → pending 应当允许（V3-UI option C）: %v", err)
	}

	src := writeTempFile(t, "x.pdf", "x")
	_, err := svc.UploadOrBind(fv.ID, UploadInput{
		SourcePath: src, OriginalFileName: "x.pdf",
		OperatorID: "u",
	})
	if err != nil {
		t.Errorf("撤销跳过后应允许上传: %v", err)
	}
}

// V3-UI option C: SubmitOutput 也被守卫
func TestStageMutability_BlockSubmitWhenCompleted(t *testing.T) {
	db, svc, _, stages := setupProjectForFileOps(t)
	// 找 output fv，先正常上传，再把环节切到 completed，最后 submit 应当拒
	fv := findFvByLocalCode(t, stages, "MZ-SG", "OUT-001")
	if fv == nil {
		t.Skip()
	}
	src := writeTempFile(t, "out.pdf", "out")
	_, err := svc.UploadOrBind(fv.ID, UploadInput{
		SourcePath: src, OriginalFileName: "out.pdf", OperatorID: "u",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 切环节到 completed
	stageRepo := NewProjectStageRepository(db)
	stageRepo.SetStatus(fv.ProjectStageID, "completed")

	if _, err := svc.SubmitOutput(fv.ID, "u"); err == nil {
		t.Fatal("环节 completed 时应拒绝 SubmitOutput")
	} else if !strings.Contains(err.Error(), "已完成") {
		t.Errorf("错误信息应含'已完成'，got: %v", err)
	}
}
