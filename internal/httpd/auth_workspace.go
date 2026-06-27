package httpd

import (
	"fmt"
	"os"
	"path/filepath"

	"data-asset-scan-go/internal/repository"
)

// ensureDefaultWorkspaceForUser 在 KeyWorkspace 为空时把默认工作目录设为「系统桌面/我的工作空间」，
// 先查后建（已存在则跳过）并写库，同时在其下建「项目文件管理」子目录。已有配置则保留用户自定义，
// 但仍补建「项目文件管理」子目录。跨平台 mac/linux/windows。
// 错误一律返回给调用方，由调用方决定是否阻塞登录（设计上：登录流程拿到 error 只 log，不让 login 失败）。
func ensureDefaultWorkspaceForUser(cfg *repository.SystemConfigRepository, username string) error {
	path := cfg.GetWorkspace()
	if path == "" {
		path = repository.DefaultWorkspaceRoot() // 系统桌面/我的工作空间
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("mkdir workspace %s: %w", path, err)
	}
	// 工作空间下默认建「项目文件管理」空目录（先查后建，跨平台）。
	if err := os.MkdirAll(filepath.Join(path, repository.ProjectFilesDirName), 0755); err != nil {
		return fmt.Errorf("mkdir 项目文件管理 %s: %w", path, err)
	}
	cfg.SetWorkspace(path)
	return nil
}

// ensureDefaultScanAreaPath 在 KeyScanAreaPath 为空时把它设为 OS home dir。
// 已有配置则不动（保留用户自定义）。home 目录本就存在，无需 mkdir。
// 与 ensureDefaultWorkspaceForUser 一样，错误只 log 不阻塞登录。
func ensureDefaultScanAreaPath(cfg *repository.SystemConfigRepository) error {
	if existing := cfg.GetScanAreaPath(); existing != "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	cfg.SetScanAreaPath(home)
	return nil
}

// defaultControlType 是首次普查需要扫描的办公文档默认后缀列表。
// 与前端 SystemConfigView 兜底显示的值保持一致。
const defaultControlType = ".doc,.docx,.ppt,.pptx,.xls,.xlsx,.pdf"

// ensureDefaultControlType 在 KeyControlType 为空时设为 defaultControlType。
// 已有配置则不动（保留用户自定义）。无 IO，不会失败——签名仍带 error 是为了
// 与其它 ensure* 对称，便于未来扩展。
func ensureDefaultControlType(cfg *repository.SystemConfigRepository) error {
	if existing := cfg.GetControlType(); existing != "" {
		return nil
	}
	cfg.SetControlType(defaultControlType)
	return nil
}
