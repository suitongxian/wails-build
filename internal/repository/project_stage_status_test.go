package repository

import (
	"strings"
	"testing"
)

// V3-3 §5.2 状态机：合法转换（含 V3-UI option C: skipped → pending 回路）
func TestValidStageStatusTransition_Legal(t *testing.T) {
	cases := []struct{ from, to string }{
		{"pending", "running"},
		{"pending", "skipped"},
		{"running", "completed"},
		{"running", "skipped"},
		{"skipped", "pending"}, // V3-UI option C：撤销跳过
	}
	for _, c := range cases {
		if !ValidStageStatusTransition(c.from, c.to) {
			t.Errorf("%s -> %s 应允许", c.from, c.to)
		}
	}
}

// V3-3 §5.2 状态机：非法转换
func TestValidStageStatusTransition_Illegal(t *testing.T) {
	cases := []struct{ from, to string }{
		{"completed", "running"}, // 硬终态不能回滚
		{"completed", "pending"},
		{"completed", "skipped"},
		{"skipped", "running"},   // skipped 只能回 pending，不能直接 running
		{"skipped", "completed"}, // 同上
		{"running", "pending"},   // 不能倒退
		{"pending", "completed"}, // 必须先 running
	}
	for _, c := range cases {
		if ValidStageStatusTransition(c.from, c.to) {
			t.Errorf("%s -> %s 应拒绝", c.from, c.to)
		}
	}
}

// V3-UI option C: IsStageMutable 区分可写状态
func TestIsStageMutable(t *testing.T) {
	cases := []struct {
		status   string
		expected bool
	}{
		{"pending", true},
		{"running", true},
		{"completed", false},
		{"skipped", false},
	}
	for _, c := range cases {
		if got := IsStageMutable(c.status); got != c.expected {
			t.Errorf("IsStageMutable(%q) = %v, want %v", c.status, got, c.expected)
		}
	}
}

// V3-3 UpdateStageStatus pending → running 路径
func TestUpdateStageStatus_PendingToRunning(t *testing.T) {
	db, _, _, stages := setupProjectForFileOps(t)
	repo := NewProjectStageRepository(db)

	stage := stages[0].ProjectStage
	if stage.Status != "pending" {
		// setup 后 stage 默认 status = pending
		t.Logf("WARN: stage 初始状态 %s（预期 pending）", stage.Status)
	}

	if err := repo.UpdateStageStatus(stage.ID, "running"); err != nil {
		t.Fatalf("pending → running 应允许: %v", err)
	}
	updated, _ := repo.FindByID(stage.ID)
	if updated.Status != "running" {
		t.Errorf("状态应为 running, got %s", updated.Status)
	}
}

// V3-3 UpdateStageStatus running → completed
func TestUpdateStageStatus_RunningToCompleted(t *testing.T) {
	db, _, _, stages := setupProjectForFileOps(t)
	repo := NewProjectStageRepository(db)
	stage := stages[0].ProjectStage
	repo.SetStatus(stage.ID, "running") // 跳过 pending 校验直接置 running 模拟
	if err := repo.UpdateStageStatus(stage.ID, "completed"); err != nil {
		t.Fatalf("running → completed 应允许: %v", err)
	}
}

// V3-3 UpdateStageStatus 非法转换被拒
func TestUpdateStageStatus_RejectIllegal(t *testing.T) {
	db, _, _, stages := setupProjectForFileOps(t)
	repo := NewProjectStageRepository(db)
	stage := stages[0].ProjectStage
	repo.SetStatus(stage.ID, "completed")
	err := repo.UpdateStageStatus(stage.ID, "running")
	if err == nil {
		t.Fatal("completed → running 应拒绝")
	}
	if !strings.Contains(err.Error(), "不允许") {
		t.Errorf("错误应含'不允许'，got %v", err)
	}
}

// V3-3 stage 不存在返回错误
func TestUpdateStageStatus_StageNotFound(t *testing.T) {
	db, _, _, _ := setupProjectForFileOps(t)
	repo := NewProjectStageRepository(db)
	err := repo.UpdateStageStatus(99999, "running")
	if err == nil {
		t.Fatal("不存在的 stage 应报错")
	}
}
