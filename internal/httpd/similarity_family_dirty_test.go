package httpd

import (
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 验证：相似度分析成功后必须把 family_dirty 清零。
// 否则用户跑完分析仍看到「需要重建」提示，造成无限循环。
func TestHTTP_RunAnalysis_ClearsFamilyDirtyOnSuccess(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyFamilyDirty, "1") // 模拟扫描刚结束置 1

	taskRepo := repository.NewSimilarityTaskRepository(db)
	taskID, err := taskRepo.Create()
	if err != nil {
		t.Fatal(err)
	}

	// 同步执行（runAnalysis 内部已含 MarkSucceeded）
	runAnalysis(taskID)

	row, err := taskRepo.GetByID(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if row.TaskState != "succeed" {
		t.Fatalf("task state = %q (err=%v), want succeed", row.TaskState, row.ErrorMessage)
	}

	if got := cfg.GetValue(repository.KeyFamilyDirty); got != "0" {
		t.Errorf("after successful analysis: family_dirty = %q, want %q", got, "0")
	}
}
