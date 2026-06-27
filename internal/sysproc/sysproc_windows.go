//go:build windows

package sysproc

import (
	"os/exec"
	"syscall"
)

// CreateNoWindow 对应 Windows 的 CREATE_NO_WINDOW 标志，
// 让子进程不分配控制台，从根上避免黑色控制台窗口闪现。
const createNoWindow = 0x08000000

// Hide 让外部命令在 Windows 下静默运行，不弹出黑色控制台窗口。
// 扫描时逐个调用 pdftotext、以及工作受理里用 cmd/explorer 打开文件，
// 默认都会闪一个控制台黑窗；设置 HideWindow + CREATE_NO_WINDOW 即可消除。
func Hide(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
