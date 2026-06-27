package repository

import (
	"testing"
)

// workspace 已设：忽略 KeyProjectRoot 旧值，永远返回 workspace
func TestGetEffectiveProjectRoot_WorkspaceWins(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetWorkspace("/data/workspace")
	cfg.SetValue(KeyProjectRoot, "/legacy/project-root") // 老数据

	if got := cfg.GetEffectiveProjectRoot(); got != "/data/workspace" {
		t.Errorf("workspace 优先，got %q", got)
	}
}

// workspace 未设：回退到 KeyProjectRoot（升级期兼容）
func TestGetEffectiveProjectRoot_FallbackWhenWorkspaceEmpty(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetValue(KeyProjectRoot, "/legacy/project-root")

	if got := cfg.GetEffectiveProjectRoot(); got != "/legacy/project-root" {
		t.Errorf("workspace 空则用 KeyProjectRoot，got %q", got)
	}
}

// workspace 是纯空白：当成空处理，回退
func TestGetEffectiveProjectRoot_WhitespaceWorkspaceFallsBack(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	cfg.SetWorkspace("   ") // 只有空白
	cfg.SetValue(KeyProjectRoot, "/legacy/project-root")

	if got := cfg.GetEffectiveProjectRoot(); got != "/legacy/project-root" {
		t.Errorf("纯空白 workspace 应回退到 KeyProjectRoot，got %q", got)
	}
}

// 都没设：返回空，由上层校验报错
func TestGetEffectiveProjectRoot_BothEmpty(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	cfg := NewSystemConfigRepository(db)

	if got := cfg.GetEffectiveProjectRoot(); got != "" {
		t.Errorf("都没设应返回空，got %q", got)
	}
}
