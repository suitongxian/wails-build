//go:build !windows

package sysproc

import "os/exec"

// Hide 在非 Windows 平台为空操作：mac/Linux 没有控制台黑窗口问题。
func Hide(cmd *exec.Cmd) {}
