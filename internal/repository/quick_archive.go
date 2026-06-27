package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 一键归档（新机制，2026-06-24）：把工作空间里某项目目录下的文件，按九宫格
// （项目层级 scope → 夹/柜/室；文件级别 → 保密/档案/资料）归档。
//
//   - 个人(person) → 本地【复制】到 {工作空间}/个人{保密/档案/资料}夹/{项目名}/
//   - 部门/单位     → 上报云端 manage（见 quick_archive_cloud.go）
//   - 行业          → 跳过
//
// 只读取、复制，不删除/修改任何原文件（含被管控的扫描文件）。跨平台。

// QuickArchiveResult 一键归档结果。
type QuickArchiveResult struct {
	Route    ArchiveRoute `json:"-"`
	RouteTip string       `json:"route"`             // 文案：本地个人夹 / 上报云端 / 跳过
	Archived int          `json:"archived"`          // 本地新复制数 / 上云数
	Skipped  int          `json:"skipped"`           // 已存在跳过数
	Errors   []string     `json:"errors,omitempty"`
}

// archiveItem 一个待归档文件及其定级。
type archiveItem struct {
	Path   string
	Name   string
	Bucket string
	Level  string
}

// archivableBuckets 参与归档的桶（input 工作依据不归档）。
var archivableBuckets = []string{"reference", "process", "output"}

// collectArchivableFiles 遍历某项目目录，按桶定级，收集待归档文件清单。
// reference 桶按 sidecar 声明级，process=项目级，output=max(项目级,重要)。
func collectArchivableFiles(ws *ProjectWorkspace, projectCode, projectSensitivity string) []archiveItem {
	var items []archiveItem
	stagesRoot := filepath.Join(ws.ProjectDir(projectCode), "stages")
	stages, err := os.ReadDir(stagesRoot)
	if err != nil {
		return items
	}
	for _, st := range stages {
		if !st.IsDir() {
			continue
		}
		stageCode := st.Name()
		// 候选桶目录：每个文件任务的 reference/process/output + 历史遗留的环节级桶目录。
		var dirs []string
		taskCodes, _ := ws.ListTaskCodes(projectCode, stageCode)
		for _, tc := range taskCodes {
			for _, b := range archivableBuckets {
				dirs = append(dirs, ws.TaskStateDir(projectCode, stageCode, tc, b))
			}
		}
		for _, b := range archivableBuckets {
			dirs = append(dirs, ws.StageStateDir(projectCode, stageCode, b))
		}
		for _, dir := range dirs {
			bucket := filepath.Base(dir)
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue // 跳过子目录与隐藏元数据文件（如参考定级清单）
				}
				path := filepath.Join(dir, e.Name())
				if fi, err := e.Info(); err == nil && isPlaceholderFile(path, fi.Size()) {
					continue // 空占位文件未填内容，不归档
				}
				refLevel := ""
				if bucket == "reference" {
					if g, ok := ReadRefGrade(dir, e.Name()); ok {
						refLevel = g.SensitivityLevel
					}
				}
				level, ok := BucketArchiveLevel(bucket, projectSensitivity, refLevel)
				if !ok {
					continue
				}
				items = append(items, archiveItem{Path: path, Name: e.Name(), Bucket: bucket, Level: level})
			}
		}
	}
	return items
}

// sanitizeArchiveSegment 把项目名清成可作目录名的片段（替换路径分隔符等非法字符）。
func sanitizeArchiveSegment(s string) string {
	s = strings.TrimSpace(s)
	repl := func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}
	s = strings.Map(repl, s)
	s = strings.Trim(s, " .")
	if s == "" {
		s = "未命名项目"
	}
	return s
}

// ArchiveProjectLocal 个人(person)项目：把项目目录(工作空间)下的文件按九宫格复制到本机个人夹。
//   workspaceRoot —— 读取源文件的工作空间根（项目目录在其下的「项目文件管理」里）
//   personalRoot  —— 个人夹写入根（不在工作空间内，默认用户主目录下「数可信个人归档」）
// 幂等：目标已存在且内容一致则跳过；同名异内容则另存副本。原文件不动。
func ArchiveProjectLocal(workspaceRoot, personalRoot, projectCode, projectName, projectSensitivity string) (*QuickArchiveResult, error) {
	res := &QuickArchiveResult{Route: RouteLocal, RouteTip: "本地个人夹"}
	if workspaceRoot == "" {
		return res, fmt.Errorf("未配置工作空间目录")
	}
	if personalRoot == "" {
		return res, fmt.Errorf("未确定个人归档目录")
	}
	ws := NewProjectWorkspace(workspaceRoot)
	items := collectArchivableFiles(ws, projectCode, projectSensitivity)
	res.Archived, res.Skipped, res.Errors = copyItemsToPersonalFolders(personalRoot, projectName, items)
	res.RouteTip = "已归档到本机个人夹：" + personalRoot
	return res, nil
}

// copyItemsToPersonalFolders 把若干文件按【文件级别】复制到本机个人{保密/档案/资料}夹下（幂等）。
// 参考/过程文件一律走这里（无论项目层级）；个人级项目的定稿也走这里。原文件不动。
func copyItemsToPersonalFolders(personalRoot, projectName string, items []archiveItem) (archived, skipped int, errs []string) {
	if personalRoot == "" {
		return 0, 0, []string{"未确定个人归档目录"}
	}
	projSeg := sanitizeArchiveSegment(projectName)
	for _, it := range items {
		folder := NineGridFolder("person", it.Level) // 始终个人夹：个人{保密/档案/资料}夹
		if folder == "" {
			continue
		}
		destDir := filepath.Join(personalRoot, folder, projSeg)
		dst := filepath.Join(destDir, it.Name)
		// 幂等：已存在且同内容 → 跳过；同名异内容 → 另存副本。
		if _, err := os.Stat(dst); err == nil {
			srcSign, _ := fileMD5(it.Path)
			dstSign, _ := fileMD5(dst)
			if srcSign != "" && srcSign == dstSign {
				skipped++
				continue
			}
			dst = uniqueArchiveDest(destDir, it.Name)
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", it.Name, err))
			continue
		}
		if err := copyFile(it.Path, dst); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", it.Name, err))
			continue
		}
		archived++
	}
	return archived, skipped, errs
}

// ImportanceLevelToSensitivity 把「认领归档保护·归类」的保护级别映射到文件敏感级。
//
//	1 核心级标识 → core
//	2 重要级标识 → important
//	3 一般级标识 → general
//	5 不予归目 / 其它 → ""（不复制实体）
func ImportanceLevelToSensitivity(level int) string {
	switch level {
	case 1:
		return "core"
	case 2:
		return "important"
	case 3:
		return "general"
	}
	return ""
}

// ArchiveResourceToPersonalFolder 认领归档保护（方案甲）：把单个文件按级别【复制】进
// 本机「个人{核心/重要/一般}文件夹」下，使「档案在线阅卷·个人」可见。
//
// group 作为分组目录段（一般取工作事项 content_subject，空时回落「认领归档保护」）。
// 复用一键归档的 copyItemsToPersonalFolders：幂等、复制不删原件、跨平台。
func ArchiveResourceToPersonalFolder(personalRoot, group, srcPath, level string) (archived, skipped int, errs []string) {
	if strings.TrimSpace(srcPath) == "" {
		return 0, 0, []string{"资源缺少文件路径，无法复制到个人文件夹"}
	}
	if strings.TrimSpace(group) == "" {
		group = "认领归档保护"
	}
	items := []archiveItem{{Path: srcPath, Name: filepath.Base(srcPath), Level: level}}
	return copyItemsToPersonalFolders(personalRoot, group, items)
}

// PersonalArchiveFile 本机个人文件夹里的一个归档文件（供「档案在线阅卷·个人」展示）。
type PersonalArchiveFile struct {
	FileName    string `json:"file_name"`
	ProjectName string `json:"project_name"`
	Level       string `json:"sensitivity_level"`
	Folder      string `json:"folder"` // 个人{核心/重要/一般}文件夹
	Size        int64  `json:"file_size"`
	ArchivedAt  string `json:"archived_at"`
}

// ListPersonalArchiveFiles 列出本机「个人{级别}文件夹」下的归档文件（按文件级别）。
// 结构：{personalRoot}/个人{级别}文件夹/{项目名}/{文件}。
func ListPersonalArchiveFiles(personalRoot, level string) []PersonalArchiveFile {
	out := []PersonalArchiveFile{}
	if personalRoot == "" {
		return out
	}
	folder := NineGridFolder("person", level)
	if folder == "" {
		return out
	}
	base := filepath.Join(personalRoot, folder)
	projects, err := os.ReadDir(base)
	if err != nil {
		return out
	}
	lvl := NormalizeSensitivity(level)
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		projDir := filepath.Join(base, p.Name())
		files, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}
			var size int64
			var at string
			if info, err := f.Info(); err == nil {
				size = info.Size()
				at = info.ModTime().Format("2006-01-02 15:04:05")
			}
			out = append(out, PersonalArchiveFile{
				FileName: f.Name(), ProjectName: p.Name(), Level: lvl,
				Folder: folder, Size: size, ArchivedAt: at,
			})
		}
	}
	return out
}

// uniqueArchiveDest 在 dir 下为 name 找不冲突的目标路径（已存在则追加 (1)/(2)…）。
func uniqueArchiveDest(dir, name string) string {
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return dst
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
	return dst
}
