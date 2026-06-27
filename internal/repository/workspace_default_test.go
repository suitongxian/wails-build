package repository

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWorkspaceDesktopDir_LinuxXDGEnv Linux 下优先取 XDG_DESKTOP_DIR 环境变量（兼容本地化桌面名）。
func TestWorkspaceDesktopDir_LinuxXDGEnv(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("仅 linux 走 XDG")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	custom := filepath.Join(home, "我的桌面")
	t.Setenv("XDG_DESKTOP_DIR", custom)
	if got := workspaceDesktopDir(); got != custom {
		t.Fatalf("应取 XDG_DESKTOP_DIR=%q，实得 %q", custom, got)
	}
}

// TestXdgDesktopDir_FromUserDirsFile 无环境变量时从 ~/.config/user-dirs.dirs 解析并展开 $HOME。
func TestXdgDesktopDir_FromUserDirsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DESKTOP_DIR", "") // 清空环境变量，强制读文件
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "user-dirs.dirs"),
		[]byte("# config\nXDG_DESKTOP_DIR=\"$HOME/桌面\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "桌面")
	if got := xdgDesktopDir(home); got != want {
		t.Fatalf("应解析为 %q，实得 %q", want, got)
	}
}

// TestDefaultWorkspaceRoot 默认工作目录 = 系统桌面下的「我的工作空间」（跨平台）。
func TestDefaultWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)        // mac/linux
	t.Setenv("USERPROFILE", home) // windows
	got := DefaultWorkspaceRoot()
	if filepath.Base(got) != DefaultWorkspaceDirName {
		t.Fatalf("默认工作目录名应为 %q，实得 %q", DefaultWorkspaceDirName, got)
	}
	// 上一级应是桌面目录（Desktop 或本地化「桌面」）
	desk := filepath.Base(filepath.Dir(got))
	if desk != "Desktop" && desk != "桌面" {
		t.Fatalf("默认工作目录应在系统桌面下，实得父目录 %q", filepath.Dir(got))
	}
}

// NewProjectWorkspace 传空 root 时落到默认工作目录并自动创建。
func TestNewProjectWorkspace_DefaultCreatesDesktopDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := NewProjectWorkspace("")
	if filepath.Base(ws.Root()) != DefaultWorkspaceDirName {
		t.Fatalf("空 root 应回退到「我的工作空间」，实得 %q", ws.Root())
	}
	if fi, err := os.Stat(ws.Root()); err != nil || !fi.IsDir() {
		t.Fatalf("默认工作目录应已被创建: %v", err)
	}
	// 工作空间下应自动建「项目文件管理」空目录
	pf := filepath.Join(ws.Root(), ProjectFilesDirName)
	if fi, err := os.Stat(pf); err != nil || !fi.IsDir() {
		t.Fatalf("「项目文件管理」子目录应已被创建: %v", err)
	}
}

// 项目目录都建在「{工作空间}/项目文件管理/{项目编码}」下。
func TestProjectDir_UnderProjectFilesDir(t *testing.T) {
	ws := NewProjectWorkspace(t.TempDir())
	got := ws.ProjectDir("XM-2026-0002")
	wantSuffix := filepath.Join(ProjectFilesDirName, "XM-2026-0002")
	if filepath.Base(filepath.Dir(got)) != ProjectFilesDirName {
		t.Fatalf("项目目录应在「项目文件管理」下，实得 %q", got)
	}
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("项目目录应以 %q 结尾，实得 %q", wantSuffix, got)
	}
	// EnsureProjectRootExists 应同时建出根目录与「项目文件管理」
	if err := ws.EnsureProjectRootExists(); err != nil {
		t.Fatalf("EnsureProjectRootExists: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(ws.Root(), ProjectFilesDirName)); err != nil || !fi.IsDir() {
		t.Fatalf("EnsureProjectRootExists 应建出「项目文件管理」: %v", err)
	}
}

func TestComputeDefaultWorkspace_HappyPath(t *testing.T) {
	got, err := ComputeDefaultWorkspace("/Users/admin", "zhang")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("/Users/admin", "zhang", "workspace")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeDefaultWorkspace_RejectsSlash(t *testing.T) {
	_, err := ComputeDefaultWorkspace("/Users/admin", "a/b")
	if err == nil {
		t.Fatal("expected error for username containing slash")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Errorf("error should mention username, got: %v", err)
	}
}

func TestComputeDefaultWorkspace_RejectsBackslash(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("/Users/admin", "a\\b"); err == nil {
		t.Fatal("expected error for username containing backslash")
	}
}

func TestComputeDefaultWorkspace_RejectsDotDot(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("/Users/admin", ".."); err == nil {
		t.Fatal("expected error for username '..'")
	}
	if _, err := ComputeDefaultWorkspace("/Users/admin", "..foo"); err == nil {
		t.Fatal("expected error for username starting with '..'")
	}
}

func TestComputeDefaultWorkspace_RejectsNull(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("/Users/admin", "a\x00b"); err == nil {
		t.Fatal("expected error for username containing null byte")
	}
}

func TestComputeDefaultWorkspace_RejectsEmpty(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("/Users/admin", ""); err == nil {
		t.Fatal("expected error for empty username")
	}
	if _, err := ComputeDefaultWorkspace("/Users/admin", "   "); err == nil {
		t.Fatal("expected error for whitespace-only username")
	}
}

func TestComputeDefaultWorkspace_RejectsEmptyHomeDir(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("", "zhang"); err == nil {
		t.Fatal("expected error for empty home dir")
	}
}
