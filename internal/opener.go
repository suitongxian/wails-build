package internal

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"

	"data-asset-scan-go/internal/sysproc"
)

// OpenFileResult 文件打开结果
type OpenFileResult struct {
	Success  bool   // 是否成功
	Message  string // 消息
	FilePath string // 文件路径（如果成功）
}

// FileOpenerService 文件打开服务
type FileOpenerService struct{}

// NewFileOpenerService 创建文件打开服务
func NewFileOpenerService() *FileOpenerService {
	return &FileOpenerService{}
}

// OpenFile 使用系统默认程序打开文件
func (s *FileOpenerService) OpenFile(filePath string) error {
	if !fileExists(filePath) {
		return fmt.Errorf("文件不存在: %s", filePath)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Windows: 使用 start 命令
		cmd = exec.Command("cmd", "/c", "start", "", filePath)
	case "darwin":
		// macOS: 使用 open 命令
		cmd = exec.Command("open", filePath)
	case "linux":
		// Linux: 使用 xdg-open 命令
		cmd = exec.Command("xdg-open", filePath)
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	sysproc.Hide(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}

	return nil
}

// OpenFolder 打开文件夹
func (s *FileOpenerService) OpenFolder(folderPath string) error {
	if !directoryExists(folderPath) {
		return fmt.Errorf("文件夹不存在: %s", folderPath)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Windows: 使用 explorer 命令
		cmd = exec.Command("explorer", folderPath)
	case "darwin":
		// macOS: 使用 open 命令
		cmd = exec.Command("open", folderPath)
	case "linux":
		// Linux: 使用 xdg-open 命令
		cmd = exec.Command("xdg-open", folderPath)
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	sysproc.Hide(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("打开文件夹失败: %w", err)
	}

	return nil
}

// OpenInExplorer 在文件管理器中打开并定位到指定文件/文件夹
func (s *FileOpenerService) OpenInExplorer(path string) error {
	if !fileExists(path) && !directoryExists(path) {
		return fmt.Errorf("路径不存在: %s", path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Windows: 使用 explorer /select, 命令选中文件
		cmd = exec.Command("explorer", "/select,", absPath)
	case "darwin":
		// macOS: 使用 open -R 命令显示文件
		cmd = exec.Command("open", "-R", absPath)
	case "linux":
		// Linux: 使用 xdg-open 打开父目录
		dir := filepath.Dir(absPath)
		cmd = exec.Command("xdg-open", dir)
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	sysproc.Hide(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("在文件管理器中打开失败: %w", err)
	}

	return nil
}

// OpenFileByContentSign 根据 content_sign 打开文件
// 这个方法会查询数据库找到匹配的文件并打开
// 注意：这个方法需要数据库支持，如果不需要数据库版本可以使用 OpenFile
func (s *FileOpenerService) OpenFileByContentSign(contentSign string, getFilePathsFunc func(string) []string) *OpenFileResult {
	// 获取所有匹配的路径
	paths := getFilePathsFunc(contentSign)

	if len(paths) == 0 {
		return &OpenFileResult{
			Success: false,
			Message: "未找到相关文件记录",
		}
	}

	// 遍历路径，找到第一个存在的文件并打开
	for _, path := range paths {
		if fileExists(path) {
			if err := s.OpenFile(path); err != nil {
				return &OpenFileResult{
					Success: false,
					Message: fmt.Sprintf("文件打开失败: %v", err),
				}
			}
			return &OpenFileResult{
				Success:  true,
				Message:  "文件已打开",
				FilePath: path,
			}
		}
	}

	// 所有文件都不存在
	return &OpenFileResult{
		Success: false,
		Message: "文件已移除或不存在",
	}
}

// revealInExplorer 在文件管理器中打开并选中文件
func (s *FileOpenerService) revealInExplorer(path string) error {
	dir := filepath.Dir(path)
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("open", "-R", path)
		sysproc.Hide(cmd)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("在 Finder 中显示文件失败: %w", err)
		}
		return nil
	case "windows":
		cmd := exec.Command("explorer", "/select,", path)
		sysproc.Hide(cmd)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("在资源管理器中显示文件失败: %w", err)
		}
		return nil
	default:
		cmd := exec.Command("xdg-open", dir)
		sysproc.Hide(cmd)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("在文件管理器中打开失败: %w", err)
		}
		return nil
	}
}

// RevealInFinder 在 macOS Finder 中显示文件（仅 macOS）
func (s *FileOpenerService) RevealInFinder(path string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("此方法仅适用于 macOS")
	}

	if !fileExists(path) && !directoryExists(path) {
		return fmt.Errorf("路径不存在: %s", path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}

	cmd := exec.Command("open", "-R", absPath)
	sysproc.Hide(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("在 Finder 中显示失败: %w", err)
	}

	return nil
}

// OpenURL 在浏览器中打开 URL
func (s *FileOpenerService) OpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	sysproc.Hide(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("打开 URL 失败: %w", err)
	}

	return nil
}
