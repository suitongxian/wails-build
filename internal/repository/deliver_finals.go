package repository

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// 提交定稿（2026-06-01）：定稿不是从零写，是从过程文件挑选 → 拷贝到 output/ → 按 output 文档标识规范名改名。
//
// - copy 不删源：过程文件原样保留（守铁律「严禁删改扫描文件」+ 留痕）。
// - 多个 output 标识 → 用户为每个标识各挑一个过程文件。
// - 拷贝后由 AutoArchiveStage 按环节级就高不就低归档（定稿与过程源同内容→单资产、就高）。

// StageOutputRule 环节的一个 output 文档标识（供前端"提交定稿"摆出待交付清单）。
type StageOutputRule struct {
	FileRuleCode     string `json:"file_rule_code" db:"file_rule_code"`
	FileName         string `json:"file_name" db:"file_name"`
	AllowedFileTypes string `json:"allowed_file_types" db:"allowed_file_types"`
}

// resolveStageID 解析模版/环节 id（与 AutoArchiveStage/ScaffoldStageFiles 一致）。
func resolveStageID(db *sqlx.DB, templateCode, stageCode string) (int64, int64, error) {
	var tplID int64
	if err := db.Get(&tplID, `SELECT id FROM data_templates WHERE template_code = ? AND disable = 0`, templateCode); err != nil {
		return 0, 0, fmt.Errorf("模版不存在: %s: %w", templateCode, err)
	}
	var stageID int64
	if err := db.Get(&stageID, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ? AND disable = 0`, tplID, stageCode); err != nil {
		return 0, 0, fmt.Errorf("环节不存在: %s: %w", stageCode, err)
	}
	return tplID, stageID, nil
}

// ListStageOutputRules 列出环节的 output 文档标识（待交付定稿清单）。
func ListStageOutputRules(db *sqlx.DB, templateCode, stageCode string) ([]StageOutputRule, error) {
	_, stageID, err := resolveStageID(db, templateCode, stageCode)
	if err != nil {
		return nil, err
	}
	var rules []StageOutputRule
	if err := db.Select(&rules, `
		SELECT file_rule_code, file_name, allowed_file_types
		FROM template_file_rules
		WHERE template_stage_id = ? AND disable = 0 AND data_state = 'output'
		ORDER BY sort_order, id`, stageID); err != nil {
		return nil, fmt.Errorf("读取 output 标识失败: %w", err)
	}
	return rules, nil
}

// ListTaskOutputRules 列出某文件任务的 output 文档标识（该任务待交付定稿清单）。
// 五层落盘：定稿是任务级的——谁编辑谁挑，每个文件任务各有定稿。
func ListTaskOutputRules(db *sqlx.DB, templateCode, stageCode, taskCode string) ([]StageOutputRule, error) {
	_, stageID, err := resolveStageID(db, templateCode, stageCode)
	if err != nil {
		return nil, err
	}
	var taskID int64
	if err := db.Get(&taskID, `SELECT id FROM template_tasks WHERE template_stage_id = ? AND task_code = ? AND disable = 0`, stageID, taskCode); err != nil {
		return nil, fmt.Errorf("文件任务不存在: %s: %w", taskCode, err)
	}
	var rules []StageOutputRule
	if err := db.Select(&rules, `
		SELECT file_rule_code, file_name, allowed_file_types
		FROM template_file_rules
		WHERE template_task_id = ? AND disable = 0 AND data_state = 'output'
		ORDER BY sort_order, id`, taskID); err != nil {
		return nil, fmt.Errorf("读取任务 output 标识失败: %w", err)
	}
	return rules, nil
}

// TaskFileRuleAttr 文件任务下一个文档标识的完整属性（供「工作受理」展示文档属性）。
type TaskFileRuleAttr struct {
	FileRuleCode     string  `json:"file_rule_code" db:"file_rule_code"`
	FileName         string  `json:"file_name" db:"file_name"`
	DataState        string  `json:"data_state" db:"data_state"`                 // input/process/output
	Required         int     `json:"required" db:"required"`                     // 0/1 是否必需
	AllowedFileTypes string  `json:"allowed_file_types" db:"allowed_file_types"` // 允许文件类型
	NamingPattern    *string `json:"naming_pattern" db:"naming_pattern"`         // 命名规则
	SummaryPattern   *string `json:"summary_pattern" db:"summary_pattern"`       // 内容要求
	SensitivityLevel *string `json:"sensitivity_level" db:"sensitivity_level"`   // 敏感级别
	Drafter          *string `json:"drafter" db:"drafter"`                       // 起草人
	SortOrder        int     `json:"sort_order" db:"sort_order"`
}

// ListTaskFileRules 列出某文件任务下的全部文档标识（input/process/output 三态）及其属性，
// 供「工作受理」展示"该任务应交哪些文件、各自的属性要求"。
func ListTaskFileRules(db *sqlx.DB, templateCode, stageCode, taskCode string) ([]TaskFileRuleAttr, error) {
	_, stageID, err := resolveStageID(db, templateCode, stageCode)
	if err != nil {
		return nil, err
	}
	var taskID int64
	if err := db.Get(&taskID, `SELECT id FROM template_tasks WHERE template_stage_id = ? AND task_code = ? AND disable = 0`, stageID, taskCode); err != nil {
		return nil, fmt.Errorf("文件任务不存在: %s: %w", taskCode, err)
	}
	rules := []TaskFileRuleAttr{}
	if err := db.Select(&rules, `
		SELECT file_rule_code, file_name, data_state, required, allowed_file_types,
		       naming_pattern, summary_pattern, sensitivity_level, drafter, sort_order
		FROM template_file_rules
		WHERE template_task_id = ? AND disable = 0
		ORDER BY CASE data_state WHEN 'input' THEN 0 WHEN 'process' THEN 1 WHEN 'output' THEN 2 ELSE 3 END,
		         sort_order, id`, taskID); err != nil {
		return nil, fmt.Errorf("读取任务文档标识失败: %w", err)
	}
	return rules, nil
}

// ListTaskProcessFiles 列出某文件任务 process/ 下的非空文件名（供该任务挑定稿；空占位不列）。
func ListTaskProcessFiles(db *sqlx.DB, projectCode, stageCode, taskCode string) ([]string, error) {
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	dir := NewProjectWorkspace(root).TaskStateDir(projectCode, stageCode, taskCode, "process")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}, nil // 目录不存在 → 空列表
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if fi, err := e.Info(); err == nil && isPlaceholderFile(filepath.Join(dir, e.Name()), fi.Size()) {
			continue // 空/占位不列
		}
		files = append(files, e.Name())
	}
	return files, nil
}

// ListTaskFinalCandidateFiles 列出某文件任务可作定稿来源的非空文件：process/ + output/（去重）。
// 用户既可能在 process 编辑后挑定稿，也可能直接在 output 填好定稿占位——两处都列出供挑选。空占位不列。
func ListTaskFinalCandidateFiles(db *sqlx.DB, projectCode, stageCode, taskCode string) ([]string, error) {
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := NewProjectWorkspace(root)
	seen := map[string]bool{}
	files := []string{}
	for _, state := range []string{"process", "output"} {
		dir := ws.TaskStateDir(projectCode, stageCode, taskCode, state)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // 目录不存在 → 跳过
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			fi, err := e.Info()
			if err != nil || isPlaceholderFile(filepath.Join(dir, e.Name()), fi.Size()) {
				continue // 空/占位不列
			}
			if seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// ListStageProcessFiles 列出环节 process/ 下的非空文件名（供挑选定稿；空占位不列）。
func ListStageProcessFiles(db *sqlx.DB, templateCode, stageCode string) ([]string, error) {
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	dir := NewProjectWorkspace(root).StageStateDir(templateCode, stageCode, "process")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}, nil // 目录不存在 → 空列表
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if fi, err := e.Info(); err == nil && isPlaceholderFile(filepath.Join(dir, e.Name()), fi.Size()) {
			continue // 空/占位不列
		}
		files = append(files, e.Name())
	}
	return files, nil
}

// FinalSelection 一份定稿：把 process/ 下的 SourceFile 作为 FileRuleCode 这个 output 标识的定稿。
type FinalSelection struct {
	FileRuleCode string `json:"file_rule_code"`
	SourceFile   string `json:"source_file"` // process/ 下的文件名（仅取 basename，防穿越）
	TaskCode     string `json:"task_code"`   // 五层落盘：该定稿所属文件任务（空=退回环节级桶，兼容）
}

// SubmitStageFinals 按选择把过程文件拷贝到 output/ 并改名为各 output 标识的规范名，返回新建定稿路径。
func SubmitStageFinals(db *sqlx.DB, templateCode, stageCode string, selections []FinalSelection) ([]string, error) {
	return SubmitStageFinalsToProject(db, templateCode, templateCode, stageCode, selections)
}

// SubmitStageFinalsToProject 同 SubmitStageFinals，但定稿规则取自 templateCode、文件目录落到 projectCode。
// 集中立项用：规则来自真实模版，目录是 CPA-{应用id} 虚拟项目。
func SubmitStageFinalsToProject(db *sqlx.DB, templateCode, projectCode, stageCode string, selections []FinalSelection) ([]string, error) {
	if len(selections) == 0 {
		return nil, fmt.Errorf("未选择任何定稿文件")
	}
	_, stageID, err := resolveStageID(db, templateCode, stageCode)
	if err != nil {
		return nil, err
	}
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := NewProjectWorkspace(root)

	var created []string
	for _, sel := range selections {
		// 取 output 标识规范名 + 允许类型（file_rule_code 在环节内唯一，按 stageID 查即可）。
		// file_rule_code 为空 = 通用定稿（模版未预设定稿标识）：用所挑文件的本名作定稿名。
		var rule StageOutputRule
		if sel.FileRuleCode != "" {
			if err := db.Get(&rule, `
				SELECT file_rule_code, file_name, allowed_file_types
				FROM template_file_rules
				WHERE template_stage_id = ? AND file_rule_code = ? AND disable = 0 AND data_state = 'output'`,
				stageID, sel.FileRuleCode); err != nil {
				return created, fmt.Errorf("output 标识不存在: %s: %w", sel.FileRuleCode, err)
			}
		}

		// 五层落盘：定稿源/目标目录按该定稿所属文件任务定位；空 TaskCode 退回环节级桶（兼容）。
		var procDir, outDir string
		if sel.TaskCode != "" {
			procDir = ws.TaskStateDir(projectCode, stageCode, sel.TaskCode, "process")
			outDir = ws.TaskStateDir(projectCode, stageCode, sel.TaskCode, "output")
		} else {
			procDir = ws.StageStateDir(projectCode, stageCode, "process")
			outDir = ws.StageStateDir(projectCode, stageCode, "output")
		}
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return created, fmt.Errorf("建定稿目录失败: %w", err)
		}

		// 源：先在 process/ 找指定文件（仅 basename，防路径穿越）；
		// 找不到再回退 output/（用户可能直接在定稿目录填好了定稿）。
		srcName := filepath.Base(sel.SourceFile)
		srcPath := filepath.Join(procDir, srcName)
		srcInfo, err := os.Stat(srcPath)
		if err != nil || srcInfo.IsDir() {
			srcPath = filepath.Join(outDir, srcName)
			srcInfo, err = os.Stat(srcPath)
			if err != nil || srcInfo.IsDir() {
				return created, fmt.Errorf("源文件不存在: %s", srcName)
			}
		}
		if isPlaceholderFile(srcPath, srcInfo.Size()) {
			return created, fmt.Errorf("不能选空白占位文件作为定稿，请先填写内容: %s", srcName)
		}

		// 目标名：output 标识规范名 + 后缀。后缀优先沿用所挑过程文件的真实后缀
		// （上一环节交付什么格式，下游就拿到什么格式；不强制成标识声明的类型，避免
		//  内容是 docx 却被命名成 .pdf 的格式错配）；源文件无后缀时才回退到标识声明类型。
		ext := filepath.Ext(srcName)
		if ext == "" {
			ext = firstFileExt(rule.AllowedFileTypes)
		}
		// 有定稿标识 → 用标识规范名；通用定稿(无标识) → 用所挑文件本名（去原扩展再加回）。
		var dstName string
		if rule.FileName != "" {
			dstName = sanitizeFileName(rule.FileName) + ext
		} else {
			base := strings.TrimSuffix(srcName, filepath.Ext(srcName))
			dstName = sanitizeFileName(base) + ext
		}
		dstPath := filepath.Join(outDir, dstName)

		// 所挑文件恰好就是定稿本身（用户直接在 output 填好且已是规范名）→ 无需拷贝，避免覆盖自身。
		if srcPath == dstPath {
			created = append(created, dstPath)
			continue
		}
		if err := copyFileOverwrite(srcPath, dstPath); err != nil {
			return created, fmt.Errorf("拷贝定稿失败 %s→%s: %w", srcName, dstName, err)
		}
		created = append(created, dstPath)
	}
	return created, nil
}

// DeliverStageOutputToNextInputs 五层落盘的跨环节交付（方案 A1）：
// 把【当前环节所有文件任务的定稿(output)】汇总，复制到【下一环节每个文件任务的 input/】，
// 让下游每个参与人就近看到工作依据。不删源（守留痕）。返回复制的文件份数（目标任务×文件）。
// 兼容：当前环节若仅有遗留的环节级 output 也一并收；下一环节若未定义任务则退回环节级 input。
func DeliverStageOutputToNextInputs(db *sqlx.DB, templateCode, projectCode, currentStageCode, nextStageCode string) (int, error) {
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := NewProjectWorkspace(root)

	// 1. 汇总当前环节所有任务的 output（按文件名去重；跳过空文件）。
	srcDirs := []string{}
	curTasks, _ := ws.ListTaskCodes(projectCode, currentStageCode)
	for _, tc := range curTasks {
		srcDirs = append(srcDirs, ws.TaskStateDir(projectCode, currentStageCode, tc, "output"))
	}
	srcDirs = append(srcDirs, ws.StageStateDir(projectCode, currentStageCode, "output")) // 兼容遗留
	seen := map[string]bool{}
	var srcFiles []string
	for _, d := range srcDirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if fi, ferr := e.Info(); ferr == nil && isPlaceholderFile(filepath.Join(d, e.Name()), fi.Size()) {
				continue // 空/占位不交付
			}
			if seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			srcFiles = append(srcFiles, filepath.Join(d, e.Name()))
		}
	}
	if len(srcFiles) == 0 {
		return 0, nil
	}

	// 2. 下一环节的文件任务列表 → 各自的 input 目录（无任务则退回环节级 input）。
	var tplID, nextStageID int64
	if err := db.Get(&tplID, `SELECT id FROM data_templates WHERE template_code = ? AND disable = 0`, templateCode); err != nil {
		return 0, fmt.Errorf("模版不存在: %s: %w", templateCode, err)
	}
	if err := db.Get(&nextStageID, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ? AND disable = 0`, tplID, nextStageCode); err != nil {
		return 0, fmt.Errorf("下一环节不存在: %s: %w", nextStageCode, err)
	}
	var nextTasks []string
	if err := db.Select(&nextTasks, `SELECT task_code FROM template_tasks WHERE template_stage_id = ? AND disable = 0 ORDER BY sort_order, id`, nextStageID); err != nil {
		return 0, fmt.Errorf("读取下一环节任务失败: %w", err)
	}
	var dstInputDirs []string
	if len(nextTasks) == 0 {
		dstInputDirs = append(dstInputDirs, ws.StageStateDir(projectCode, nextStageCode, "input"))
	} else {
		for _, tc := range nextTasks {
			if _, err := ws.CreateTaskDir(projectCode, nextStageCode, tc); err != nil {
				return 0, fmt.Errorf("建下一环节任务目录失败: %w", err)
			}
			dstInputDirs = append(dstInputDirs, ws.TaskStateDir(projectCode, nextStageCode, tc, "input"))
		}
	}

	// 3. 每个源文件复制到每个目标任务的 input/。
	count := 0
	for _, dst := range dstInputDirs {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return count, fmt.Errorf("建下一环节 input 目录失败: %w", err)
		}
		for _, src := range srcFiles {
			if err := copyFileOverwrite(src, filepath.Join(dst, filepath.Base(src))); err != nil {
				return count, fmt.Errorf("复制工作依据失败 %s: %w", filepath.Base(src), err)
			}
			count++
		}
	}
	return count, nil
}

// copyFileOverwrite 复制文件内容（不删源；目标已存在则覆盖，支持重新提交定稿）。
func copyFileOverwrite(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst) // 截断覆盖
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
