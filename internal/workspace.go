package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"data-asset-scan-go/internal/config"
	"data-asset-scan-go/internal/sysproc"
)

// WorkspaceSubdirectory 工作空间子目录配置
type WorkspaceSubdirectory struct {
	Name     string // 目录名称
	IsHidden bool   // 是否为隐藏目录
}

// WorkspaceInitResult 工作空间初始化结果
type WorkspaceInitResult struct {
	Success       bool   // 是否成功
	WorkspacePath string // 工作空间路径
	CreatedNew    bool   // 是否新建了工作空间
	Message       string // 消息
}

// WorkspaceService 工作空间管理服务
type WorkspaceService struct {
	// 自定义工作空间路径（如果为空，使用默认路径）
	customWorkspacePath string
}

// WorkspaceService 的预设子目录
var DefaultSubdirectories = []WorkspaceSubdirectory{
	{Name: ".核心要件密码柜", IsHidden: true},
	{Name: ".重要文件档案柜", IsHidden: true},
	{Name: ".开放文本资料柜", IsHidden: true},
	{Name: ".个人数据保护区", IsHidden: true},
}

// DefaultWorkspaceName 工作空间默认名称
const DefaultWorkspaceName = "我的工作空间"

// NewWorkspaceService 创建工作空间服务
func NewWorkspaceService() *WorkspaceService {
	return &WorkspaceService{}
}

// SetCustomWorkspacePath 设置自定义工作空间路径
func (s *WorkspaceService) SetCustomWorkspacePath(path string) {
	s.customWorkspacePath = path
}

// InitializeWorkspace 初始化工作空间
func (s *WorkspaceService) InitializeWorkspace() (*WorkspaceInitResult, error) {
	// 1. 获取当前工作空间路径
	currentWorkspace := s.getWorkspacePath()
	if currentWorkspace != "" && directoryExists(currentWorkspace) {
		// 工作空间已存在
		if err := s.ensureDesktopShortcut(currentWorkspace); err != nil {
			// 快捷方式创建失败不影响主流程
			fmt.Printf("警告: 创建桌面快捷方式失败: %v\n", err)
		}
		return &WorkspaceInitResult{
			Success:       true,
			WorkspacePath: currentWorkspace,
			CreatedNew:    false,
			Message:       "工作空间目录已存在",
		}, nil
	}

	// 2. 需要创建新的工作空间
	return s.createWorkspace()
}

// createWorkspace 创建工作空间目录
func (s *WorkspaceService) createWorkspace() (*WorkspaceInitResult, error) {
	// 1. 创建主工作空间目录
	workspacePath := s.getWorkspacePath()
	if workspacePath == "" {
		workspacePath = filepath.Join(getHomeDir(), DefaultWorkspaceName)
	}

	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		return nil, fmt.Errorf("创建主工作空间目录失败: %w", err)
	}

	// 2. 创建子目录
	for _, subdir := range DefaultSubdirectories {
		subdirPath := filepath.Join(workspacePath, subdir.Name)
		if !directoryExists(subdirPath) {
			if err := os.MkdirAll(subdirPath, 0755); err != nil {
				return nil, fmt.Errorf("创建子目录失败: %w", err)
			}

			// 在 Windows 上设置隐藏属性
			if subdir.IsHidden && runtime.GOOS == "windows" {
				if err := setWindowsHiddenAttribute(subdirPath); err != nil {
					fmt.Printf("警告: 设置目录隐藏属性失败: %v\n", err)
				}
			}
		}
	}

	// 3. 在桌面创建快捷方式
	if err := s.createDesktopShortcut(workspacePath); err != nil {
		fmt.Printf("警告: 创建桌面快捷方式失败: %v\n", err)
	}

	return &WorkspaceInitResult{
		Success:       true,
		WorkspacePath: workspacePath,
		CreatedNew:    true,
		Message:       "工作空间创建成功",
	}, nil
}

// DeleteWorkspace 删除工作空间目录
func (s *WorkspaceService) DeleteWorkspace() error {
	workspacePath := s.getWorkspacePath()
	if workspacePath == "" {
		return fmt.Errorf("工作空间路径未设置")
	}

	if !directoryExists(workspacePath) {
		return fmt.Errorf("工作空间目录不存在")
	}

	// 删除工作空间
	if err := os.RemoveAll(workspacePath); err != nil {
		return fmt.Errorf("删除工作空间目录失败: %w", err)
	}

	return nil
}

// GetOSCompatiblePath 获取操作系统兼容的路径
func (s *WorkspaceService) GetOSCompatiblePath(path string) string {
	if path == "" {
		return ""
	}

	// 路径已经在正确的操作系统格式
	// 如果需要，可以添加额外的转换逻辑
	return filepath.FromSlash(path)
}

// getDesktopPath 获取桌面路径
func (s *WorkspaceService) getDesktopPath() string {
	homeDir := getHomeDir()

	switch runtime.GOOS {
	case "windows":
		return filepath.Join(homeDir, "Desktop")
	case "darwin":
		return filepath.Join(homeDir, "Desktop")
	case "linux":
		// 尝试多种可能的桌面路径
		possiblePaths := []string{
			filepath.Join(homeDir, "Desktop"),
			filepath.Join(homeDir, "桌面"),
		}
		for _, p := range possiblePaths {
			if directoryExists(p) {
				return p
			}
		}
		return filepath.Join(homeDir, "Desktop")
	default:
		return filepath.Join(homeDir, "Desktop")
	}
}

// getWorkspacePath 获取工作空间路径
func (s *WorkspaceService) getWorkspacePath() string {
	if s.customWorkspacePath != "" {
		return s.customWorkspacePath
	}
	// 尝试从配置获取
	cfg := config.GetConfig()
	if cfg != nil && cfg.Workspace != "" {
		return cfg.Workspace
	}
	return ""
}

// setWorkspacePath 设置工作空间路径到配置
func (s *WorkspaceService) setWorkspacePath(path string) error {
	cfg := config.GetConfig()
	if cfg != nil {
		cfg.Workspace = path
		return nil // Config is already saved by config.Save()
	}
	return fmt.Errorf("配置未初始化")
}

// ensureDesktopShortcut 确保桌面快捷方式存在
func (s *WorkspaceService) ensureDesktopShortcut(workspacePath string) error {
	return s.createDesktopShortcut(workspacePath)
}

// createDesktopShortcut 在桌面创建工作空间快捷方式
func (s *WorkspaceService) createDesktopShortcut(workspacePath string) error {
	desktopPath := s.getDesktopPath()
	if desktopPath == "" {
		return fmt.Errorf("无法获取桌面路径")
	}

	shortcutName := DefaultWorkspaceName

	switch runtime.GOOS {
	case "darwin":
		// macOS: 创建符号链接
		linkPath := filepath.Join(desktopPath, shortcutName)
		if !fileExists(linkPath) {
			if err := os.Symlink(workspacePath, linkPath); err != nil {
				return fmt.Errorf("创建符号链接失败: %w", err)
			}
		}
	case "linux":
		// Linux: 创建符号链接
		linkPath := filepath.Join(desktopPath, shortcutName)
		if !fileExists(linkPath) {
			if err := os.Symlink(workspacePath, linkPath); err != nil {
				return fmt.Errorf("创建符号链接失败: %w", err)
			}
		}
	case "windows":
		// Windows: 使用 PowerShell 创建快捷方式
		lnkPath := filepath.Join(desktopPath, shortcutName+".lnk")
		if !fileExists(lnkPath) {
			if err := createWindowsShortcut(lnkPath, workspacePath, DefaultWorkspaceName); err != nil {
				return fmt.Errorf("创建快捷方式失败: %w", err)
			}
		}
	}

	return nil
}

// createWindowsShortcut 使用 PowerShell 创建 Windows 快捷方式
func createWindowsShortcut(shortcutPath, targetPath, description string) error {
	psScript := fmt.Sprintf(`
$WshShell = New-Object -ComObject WScript.Shell
$Shortcut = $WshShell.CreateShortcut('%s')
$Shortcut.TargetPath = '%s'
$Shortcut.Description = '%s'
$Shortcut.Save()
`, shortcutPath, targetPath, description)

	cmd := exec.Command("powershell", "-Command", psScript)
	sysproc.Hide(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("PowerShell 执行失败: %w", err)
	}
	return nil
}

// setWindowsHiddenAttribute 设置 Windows 隐藏属性
func setWindowsHiddenAttribute(path string) error {
	cmd := exec.Command("attrib", "+h", path)
	sysproc.Hide(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("设置隐藏属性失败: %w", err)
	}
	return nil
}

// directoryExists 检查目录是否存在
func directoryExists(dirPath string) bool {
	info, err := os.Stat(dirPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// fileExists 检查文件或链接是否存在
func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// getHomeDir 获取用户主目录
func getHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return home
}

// IsWorkspaceValid 检查工作空间是否有效
func (s *WorkspaceService) IsWorkspaceValid() bool {
	workspacePath := s.getWorkspacePath()
	if workspacePath == "" {
		return false
	}
	return directoryExists(workspacePath)
}

// GetSubdirectoryPath 获取工作空间子目录路径
func (s *WorkspaceService) GetSubdirectoryPath(subdirName string) string {
	workspacePath := s.getWorkspacePath()
	if workspacePath == "" {
		return ""
	}
	return filepath.Join(workspacePath, subdirName)
}
