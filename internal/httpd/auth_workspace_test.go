package httpd

import (
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func TestEnsureDefaultWorkspace_EmptyConfig_CreatesAndSets(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	if err := ensureDefaultWorkspaceForUser(cfg, "zhang"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 默认工作目录 = 系统桌面/我的工作空间，且已创建
	got := cfg.GetWorkspace()
	if filepath.Base(got) != "我的工作空间" {
		t.Errorf("KeyWorkspace 应为「我的工作空间」目录，实得 %q", got)
	}
	desk := filepath.Base(filepath.Dir(got))
	if desk != "Desktop" && desk != "桌面" {
		t.Errorf("默认工作目录应在系统桌面下，实得父目录 %q", filepath.Dir(got))
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("workspace dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("workspace path exists but is not a directory")
	}
}

func TestEnsureDefaultWorkspace_PreservesUserCustom(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	cfg.SetWorkspace("/data/custom")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := ensureDefaultWorkspaceForUser(cfg, "zhang"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.GetWorkspace(); got != "/data/custom" {
		t.Errorf("KeyWorkspace should not be overwritten, got %q", got)
	}
	conv := filepath.Join(tmpHome, "zhang", "workspace")
	if _, err := os.Stat(conv); !os.IsNotExist(err) {
		t.Errorf("convention dir should not exist, but stat err=%v", err)
	}
}

func TestEnsureDefaultWorkspace_MkdirFailureReturnsError(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	// 把 HOME 指向一个文件而非目录，使 MkdirAll 失败
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpFile)

	err := ensureDefaultWorkspaceForUser(cfg, "zhang")
	if err == nil {
		t.Fatal("expected mkdir error")
	}
	if got := cfg.GetWorkspace(); got != "" {
		t.Errorf("KeyWorkspace should NOT be written on mkdir failure, got %q", got)
	}
}

// scan_area_path 默认 = OS home（首次普查的扫描根）
func TestEnsureDefaultScanAreaPath_EmptyConfig_SetsToHome(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := ensureDefaultScanAreaPath(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.GetScanAreaPath(); got != tmpHome {
		t.Errorf("ScanAreaPath = %q, want %q", got, tmpHome)
	}
}

// 已有 scan_area_path（用户自定义）不被覆盖
func TestEnsureDefaultScanAreaPath_PreservesUserCustom(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	cfg.SetScanAreaPath("/data/scan-root")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := ensureDefaultScanAreaPath(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.GetScanAreaPath(); got != "/data/scan-root" {
		t.Errorf("ScanAreaPath should be preserved, got %q", got)
	}
}

// control_type 默认 = defaultControlType（首次普查不会因为没配管控类型而失败）
func TestEnsureDefaultControlType_EmptyConfig_SetsToDefault(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	if err := ensureDefaultControlType(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.GetControlType(); got != defaultControlType {
		t.Errorf("ControlType = %q, want %q", got, defaultControlType)
	}
}

// 已有 control_type 不被覆盖
func TestEnsureDefaultControlType_PreservesUserCustom(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	cfg.SetControlType(".txt,.md")

	if err := ensureDefaultControlType(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.GetControlType(); got != ".txt,.md" {
		t.Errorf("ControlType should be preserved, got %q", got)
	}
}
