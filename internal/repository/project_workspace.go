package repository

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ProjectWorkspace 管理项目本地目录树
//
// 标准目录结构：
//
//	{root}/
//	  {project_code}/
//	    stages/
//	      {stage_code}/
//	        input/
//	        process/
//	        output/
//	    ...
//
// 2026-06-02 移除项目级 metadata/ 与 archive/ 占位目录（一直空着、无读写）；只保留 stages/。
//
// 项目根 root 来源于 system_config 的 project_root；缺省回退到「系统桌面/工作空间测试」。
type ProjectWorkspace struct {
	root string
}

// DefaultWorkspaceDirName 默认工作目录名（建在系统桌面下）。
const DefaultWorkspaceDirName = "我的工作空间"

// ProjectFilesDirName 工作空间下用于存放各项目目录的固定子目录名。
// 所有项目目录都建在「{工作空间}/项目文件管理/{项目编码}」下。
const ProjectFilesDirName = "项目文件管理"

// userHomeDir 跨平台取用户主目录（mac/linux=$HOME，windows=%USERPROFILE%）。
func userHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	return os.Getenv("USERPROFILE")
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// xdgDesktopDir 解析 Linux 桌面真实路径：优先环境变量 XDG_DESKTOP_DIR，
// 否则读 ~/.config/user-dirs.dirs 中的 XDG_DESKTOP_DIR="$HOME/..."。展开 $HOME。返回 "" 表示未配置。
func xdgDesktopDir(home string) string {
	expand := func(v string) string {
		v = strings.Trim(strings.TrimSpace(v), `"`)
		v = strings.ReplaceAll(v, "$HOME", home)
		v = strings.ReplaceAll(v, "${HOME}", home)
		return v
	}
	if v := strings.TrimSpace(os.Getenv("XDG_DESKTOP_DIR")); v != "" {
		return expand(v)
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(cfg, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "XDG_DESKTOP_DIR=") {
			return expand(strings.TrimPrefix(line, "XDG_DESKTOP_DIR="))
		}
	}
	return ""
}

// workspaceDesktopDir 跨平台获取系统桌面目录（mac/linux/windows）。
//   - macOS：$HOME/Desktop
//   - Windows：%USERPROFILE%\Desktop；若被 OneDrive 重定向则退到 %USERPROFILE%\OneDrive\Desktop
//   - Linux：优先 XDG_DESKTOP_DIR（环境变量 / ~/.config/user-dirs.dirs），否则 Desktop / 本地化「桌面」
//
// 任一路径都拿不到时，回退到 home/Desktop（上层 MkdirAll 仍会创建）。
func workspaceDesktopDir() string {
	home := userHomeDir()
	switch runtime.GOOS {
	case "windows":
		d := filepath.Join(home, "Desktop")
		if !isDir(d) {
			if od := filepath.Join(home, "OneDrive", "Desktop"); isDir(od) {
				return od
			}
		}
		return d
	case "darwin":
		return filepath.Join(home, "Desktop")
	default: // linux 及其它类 unix
		if x := xdgDesktopDir(home); x != "" {
			return x
		}
		for _, p := range []string{filepath.Join(home, "Desktop"), filepath.Join(home, "桌面")} {
			if isDir(p) {
				return p
			}
		}
		return filepath.Join(home, "Desktop")
	}
}

// DefaultWorkspaceRoot 默认工作目录：系统桌面下的「我的工作空间」。
func DefaultWorkspaceRoot() string {
	return filepath.Join(workspaceDesktopDir(), DefaultWorkspaceDirName)
}

// ensureWorkspaceDirs 先查后建工作空间根目录及其下的「项目文件管理」子目录（幂等，跨平台）。
func ensureWorkspaceDirs(root string) {
	if strings.TrimSpace(root) == "" {
		return
	}
	_ = os.MkdirAll(root, 0o755)
	_ = os.MkdirAll(filepath.Join(root, ProjectFilesDirName), 0o755)
}

// NewProjectWorkspace 构造（root 由 SystemConfig.GetValue("project_root") 决定）。
// 未配置时默认用「系统桌面/我的工作空间」，并先查后建（已存在则跳过），同时建「项目文件管理」子目录。
func NewProjectWorkspace(root string) *ProjectWorkspace {
	if strings.TrimSpace(root) == "" {
		root = DefaultWorkspaceRoot()
		ensureWorkspaceDirs(root) // 先查后建：桌面无「我的工作空间」/「项目文件管理」则创建
	}
	return &ProjectWorkspace{root: root}
}

// Root 返回项目根
func (w *ProjectWorkspace) Root() string {
	return w.root
}

// ProjectDir 返回项目目录：{工作空间}/项目文件管理/{项目编码}
func (w *ProjectWorkspace) ProjectDir(projectCode string) string {
	return filepath.Join(w.root, ProjectFilesDirName, projectCode)
}

// StageDir 返回工作环节根目录
func (w *ProjectWorkspace) StageDir(projectCode, stageCode string) string {
	return filepath.Join(w.ProjectDir(projectCode), "stages", stageCode)
}

// StageStateDir 返回环节下三态目录（input/process/output）
func (w *ProjectWorkspace) StageStateDir(projectCode, stageCode, dataState string) string {
	return filepath.Join(w.StageDir(projectCode, stageCode), dataState)
}

// ── 文件任务层（2026-06-11 五层落盘）──
// 目录在环节与三态之间多插一层「文件任务」：
//
//	{project}/stages/{stage}/{task}/{input,process,output}/
//
// 每个文件任务有独立三态目录，参与人之间互不干扰（解决多任务过程文件平铺撞名）。

// dataStateBuckets 项目目录下的四个固定桶（也用于把任务子目录与历史遗留的环节级桶目录区分开）：
// input=工作依据 / reference=参考文件 / process=过程文件 / output=定稿。
// 注：模版层已不含数据态（所有文件标识都是过程文件）；这四个桶是运行期固定目录结构。
var dataStateBuckets = []string{"input", "reference", "process", "output"}

// TaskDir 返回某文件任务的根目录：stages/{stage}/{task}
func (w *ProjectWorkspace) TaskDir(projectCode, stageCode, taskCode string) string {
	return filepath.Join(w.StageDir(projectCode, stageCode), taskCode)
}

// TaskStateDir 返回某文件任务下三态目录（input/process/output）
func (w *ProjectWorkspace) TaskStateDir(projectCode, stageCode, taskCode, dataState string) string {
	return filepath.Join(w.TaskDir(projectCode, stageCode, taskCode), dataState)
}

// CreateTaskDir 创建某文件任务的三态目录（含 input/process/output），幂等，返回任务根目录。
func (w *ProjectWorkspace) CreateTaskDir(projectCode, stageCode, taskCode string) (string, error) {
	dirs := []string{
		w.ProjectDir(projectCode),
		w.StageDir(projectCode, stageCode),
		w.TaskDir(projectCode, stageCode, taskCode),
	}
	for _, st := range dataStateBuckets {
		dirs = append(dirs, w.TaskStateDir(projectCode, stageCode, taskCode, st))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return w.TaskDir(projectCode, stageCode, taskCode), nil
}

// ListTaskCodes 列出某环节下已建的文件任务子目录名（用于归档/交付时遍历所有任务）。
// 排除历史遗留的环节级三态桶目录（input/process/output），只返回任务目录。
// 环节目录不存在时返回空列表（非错误）。
func (w *ProjectWorkspace) ListTaskCodes(projectCode, stageCode string) ([]string, error) {
	entries, err := os.ReadDir(w.StageDir(projectCode, stageCode))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var codes []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "input" || name == "reference" || name == "process" || name == "output" {
			continue // 跳过遗留的环节级桶目录
		}
		codes = append(codes, name)
	}
	return codes, nil
}

// CreateProjectTree 创建项目目录树
//
// 输入 stageCodes：项目下的所有工作环节编码
// 在已存在的目录上调用是幂等的（os.MkdirAll）
func (w *ProjectWorkspace) CreateProjectTree(projectCode string, stageCodes []string) error {
	dirs := []string{
		w.ProjectDir(projectCode),
	}
	for _, code := range stageCodes {
		dirs = append(dirs, w.StageDir(projectCode, code))
		for _, st := range dataStateBuckets {
			dirs = append(dirs, w.StageStateDir(projectCode, code, st))
		}
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

// CreateStageDir 只创建某一个环节的目录（含三态 input/process/output）。
// 用于多人协同：每个人开始自己的环节时，只在本机工作空间建该环节目录，不建全树。幂等。
func (w *ProjectWorkspace) CreateStageDir(projectCode, stageCode string) (string, error) {
	dirs := []string{
		w.ProjectDir(projectCode),
		w.StageDir(projectCode, stageCode),
	}
	for _, st := range dataStateBuckets {
		dirs = append(dirs, w.StageStateDir(projectCode, stageCode, st))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return w.StageDir(projectCode, stageCode), nil
}

// EnsureProjectStageDirs 只建项目目录与各环节目录（不建三态桶——五层落盘下桶在文件任务层）。
// 用于环节启动时先把骨架建好，任务三态目录随后由 scaffold/start-task 按任务建。幂等。
func (w *ProjectWorkspace) EnsureProjectStageDirs(projectCode string, stageCodes []string) error {
	dirs := []string{w.ProjectDir(projectCode)}
	for _, code := range stageCodes {
		dirs = append(dirs, w.StageDir(projectCode, code))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

// EnsureProjectRootExists 确保工作空间根目录及「项目文件管理」子目录都存在（先查后建，幂等）。
func (w *ProjectWorkspace) EnsureProjectRootExists() error {
	if err := os.MkdirAll(w.root, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(w.root, ProjectFilesDirName), 0o755)
}
