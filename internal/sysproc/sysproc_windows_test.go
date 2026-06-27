//go:build windows

package sysproc

import (
	"os/exec"
	"syscall"
	"testing"
)

// Windows 平台 Hide 应设置 HideWindow 且带上 CREATE_NO_WINDOW 标志，从而不弹黑窗口。
func TestHide_SetsFlagsOnWindows(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "echo", "hi")
	Hide(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("Windows 下 Hide 应设置 SysProcAttr")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("Windows 下 Hide 应置 HideWindow=true")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("Windows 下 Hide 应带 CREATE_NO_WINDOW，实得 flags=0x%x", cmd.SysProcAttr.CreationFlags)
	}
}

// 已存在的 CreationFlags 不应被覆盖，只按位叠加。
func TestHide_PreservesExistingFlags(t *testing.T) {
	const existing = 0x00000010 // 占位的既有标志
	cmd := exec.Command("cmd", "/c", "echo", "hi")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: existing}
	Hide(cmd)
	if cmd.SysProcAttr.CreationFlags&existing == 0 {
		t.Fatal("Hide 不应抹掉已有的 CreationFlags")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("Hide 应叠加 CREATE_NO_WINDOW")
	}
}
