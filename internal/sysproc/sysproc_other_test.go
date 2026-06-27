//go:build !windows

package sysproc

import (
	"os/exec"
	"testing"
)

// 非 Windows 平台 Hide 应为空操作：不改 SysProcAttr，命令仍能正常运行。
func TestHide_NoopOnNonWindows(t *testing.T) {
	cmd := exec.Command("echo", "hi")
	Hide(cmd)
	if cmd.SysProcAttr != nil {
		t.Fatalf("非 Windows 下 Hide 不应设置 SysProcAttr，实得 %+v", cmd.SysProcAttr)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Hide 后命令仍应能运行: %v", err)
	}
}

// Hide(nil) 不应 panic。
func TestHide_NilSafe(t *testing.T) {
	Hide(nil)
}
