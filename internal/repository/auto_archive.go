package repository

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// 主路径自动归档（2026-06-01）：用户按模版规则在环节目录里产出文件，点「完成」触发。
//
// 复用现有"归档=挂账"机制（不搬文件）：
//   路径确定性归类（项目/环节/态从目录读出）→ 就高不就低定级 → 设 importance_level
//   → BridgeClassifyWithFamilyPropagation 挂账到个人归目容器（个人空间，幂等）。
// 去重：data_resources 按 content_sign(MD5) 复用；挂账按 file_version_code 幂等。
// data_state 决定九宫格落位：本期统一先入"个人空间"对应密级容器（部门柜由 deliver 跨终端流转处理）。

// 模版密级(core/important/general) → 个人容器 importance_level(1核心/2重要/3一般)
func sensitivityToImportance(sens string) int {
	switch sens {
	case "core", "core_secret":
		return 1
	case "important":
		return 2
	default:
		return 3 // general / 空
	}
}

// 就高不就低：在候选密级里取最高（importance 数字最小）。
func highestImportance(levels ...int) int {
	best := 3
	for _, l := range levels {
		if l == 0 {
			continue
		}
		if l < best {
			best = l
		}
	}
	return best
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// AutoArchiveResult 自动归档结果
type AutoArchiveResult struct {
	Archived int      `json:"archived"` // 本次挂账文件数
	Skipped  int      `json:"skipped"`  // 已归档跳过数
	Errors   []string `json:"errors"`
}

// AutoArchiveStage 对某本地模版某环节的产出目录(process/output)做自动归档（点「完成」触发）。
// 模版即项目（templateCode 同时作目录码）。
func AutoArchiveStage(db *sqlx.DB, templateCode, stageCode string) (*AutoArchiveResult, error) {
	return AutoArchiveStageForProject(db, templateCode, templateCode, stageCode)
}

// AutoArchiveStageForProject 同 AutoArchiveStage，但归档规则/密级取自 templateCode、
// 产出目录落在 projectCode（集中立项 CPA-{应用id} 虚拟项目用）。
func AutoArchiveStageForProject(db *sqlx.DB, templateCode, projectCode, stageCode string) (*AutoArchiveResult, error) {
	if err := EnsurePersonalContext(db); err != nil {
		return nil, fmt.Errorf("准备个人归目容器失败: %w", err)
	}

	// 模版（项目）密级
	var tpl struct {
		ID                      int64  `db:"id"`
		ProjectSensitivityLevel string `db:"project_sensitivity_level"`
	}
	if err := db.Get(&tpl, `SELECT id, project_sensitivity_level FROM data_templates WHERE template_code = ? AND disable = 0`, templateCode); err != nil {
		return nil, fmt.Errorf("模版不存在: %s: %w", templateCode, err)
	}
	projImp := sensitivityToImportance(tpl.ProjectSensitivityLevel)

	// 环节
	var stageID int64
	if err := db.Get(&stageID, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ? AND disable = 0`, tpl.ID, stageCode); err != nil {
		return nil, fmt.Errorf("环节不存在: %s: %w", stageCode, err)
	}

	// 环节级"就高不就低"密级：取项目级 + 该环节内全部标识/任务的最高密级，
	// 作为该环节所有产出文件(process/output)的统一级别。
	//
	// 为何环节级而非按 data_state：定稿是过程文件的拷贝(同内容→同 content_sign)，
	// 若 process/output 各自定级，幂等去重会让"先归档者"锁死级别，定稿可能被过程草稿压低。
	// 统一取环节最高级，则同内容无论先归哪个都已是就高级别，无需危险的"升级-搬迁"。
	// 含义：高敏定稿所在环节的过程草稿也按高敏管控，符合就高不就低、偏保护。
	stageImp := projImp
	type ruleRow struct {
		Sensitivity     *string `db:"sensitivity_level"`
		TaskSensitivity *string `db:"task_sensitivity_level"`
	}
	var rules []ruleRow
	_ = db.Select(&rules, `
		SELECT fr.sensitivity_level, tt.sensitivity_level AS task_sensitivity_level
		FROM template_file_rules fr
		LEFT JOIN template_tasks tt ON tt.id = fr.template_task_id
		WHERE fr.template_stage_id = ? AND fr.disable = 0`, stageID)
	for _, r := range rules {
		if r.Sensitivity != nil {
			stageImp = highestImportance(stageImp, sensitivityToImportance(*r.Sensitivity))
		}
		if r.TaskSensitivity != nil {
			stageImp = highestImportance(stageImp, sensitivityToImportance(*r.TaskSensitivity))
		}
	}

	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := NewProjectWorkspace(root)
	resRepo := NewDataResourcesRepository(db, 100)

	result := &AutoArchiveResult{}
	// 归档对象：各文件任务的 过程(process)+定稿(output)；input 是上游引用不挂账。
	// 五层落盘：遍历环节下所有文件任务子目录 stages/{stage}/{task}/{process,output}；
	// 同时兼容历史遗留的环节级桶目录 stages/{stage}/{process,output}。
	var dirs []string
	taskCodes, _ := ws.ListTaskCodes(projectCode, stageCode)
	for _, tc := range taskCodes {
		dirs = append(dirs,
			ws.TaskStateDir(projectCode, stageCode, tc, "process"),
			ws.TaskStateDir(projectCode, stageCode, tc, "output"))
	}
	dirs = append(dirs,
		ws.StageStateDir(projectCode, stageCode, "process"),
		ws.StageStateDir(projectCode, stageCode, "output"))
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // 目录不存在/为空，跳过
		}
		imp := stageImp // 环节级就高不就低，process/output 同级
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			// 跳过未填写的占位文件（0 字节空占位，或 .pdf 的最小可打开占位）——未填内容不归档，
			// 且空文件 MD5 相同会撞车。用户填入内容后才参与归档。
			path := filepath.Join(dir, e.Name())
			if fi, err := e.Info(); err == nil && isPlaceholderFile(path, fi.Size()) {
				continue
			}
			sign, err := fileMD5(path)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", e.Name(), err))
				continue
			}
			// find-or-register data_resource（按 content_sign 去重）
			dr, _ := resRepo.GetByContentSign(sign)
			var resID int64
			if dr != nil {
				resID = dr.DataResourcesID
				// 已挂账过（importance_level 已定）→ 幂等跳过
				if dr.ImportanceLevel > 0 {
					result.Skipped++
					continue
				}
			} else {
				id, err := registerResource(db, sign, e.Name(), path)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: 登记失败 %v", e.Name(), err))
					continue
				}
				resID = id
			}
			name := e.Name()
			if err := resRepo.ClassifyResource(resID, imp, &name, nil, nil); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: 定级失败 %v", e.Name(), err))
				continue
			}
			if _, err := BridgeClassifyWithFamilyPropagation(db, resID); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: 挂账失败 %v", e.Name(), err))
				continue
			}
			result.Archived++
		}
	}
	return result, nil
}

// AutoArchiveAllWorkspace 巡检整个工作空间(project_root)，对每个 {项目}/stages/{环节} 跑自动归档。
//
// 关键：归档不能只靠用户点「完成」——否则没点完成的工作文件会一直游离在管理之外（盲区）。
// 该巡检独立于「完成」，随盘点扫描一起跑：把工作空间里按模版产出的文件持续按主路径入账归档（幂等），
// 不依赖交付动作。与通用扫描分域（通用扫描已排除 project_root），故工作空间文件只走主路径、不进兜底。
func AutoArchiveAllWorkspace(db *sqlx.DB) (*AutoArchiveResult, error) {
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	agg := &AutoArchiveResult{}
	if root == "" {
		return agg, nil
	}
	// 项目目录都建在「{工作空间}/项目文件管理/」下，巡检从该子目录遍历。
	pfRoot := filepath.Join(root, ProjectFilesDirName)
	projects, err := os.ReadDir(pfRoot)
	if err != nil {
		return agg, nil // 工作空间未建/为空，无可巡检
	}
	for _, proj := range projects {
		if !proj.IsDir() {
			continue
		}
		stagesDir := filepath.Join(pfRoot, proj.Name(), "stages")
		stages, err := os.ReadDir(stagesDir)
		if err != nil {
			continue
		}
		for _, st := range stages {
			if !st.IsDir() {
				continue
			}
			// 模版/环节可能已停用或被删（目录残留）→ AutoArchiveStage 返回 error，跳过不阻断巡检。
			res, err := AutoArchiveStage(db, proj.Name(), st.Name())
			if err != nil {
				continue
			}
			agg.Archived += res.Archived
			agg.Skipped += res.Skipped
			agg.Errors = append(agg.Errors, res.Errors...)
		}
	}
	return agg, nil
}

// registerResource 把单个磁盘文件登记成 data_resource（按 content_sign），返回 id。
func registerResource(db *sqlx.DB, contentSign, name, path string) (int64, error) {
	now := time.Now()
	first := now
	if fi, err := os.Stat(path); err == nil {
		first = fi.ModTime()
	}
	contentType := ""
	if i := strings.LastIndex(name, "."); i > 0 && i < len(name)-1 {
		contentType = strings.ToLower(name[i+1:])
	}
	res, err := db.Exec(`
		INSERT INTO data_resources
			(content_sign, source_count, workspace_source_count, first_create_time,
			 resources_name, content_subject, content_type, file_magic,
			 create_time, update_time, disable, data_origin)
		VALUES (?, 1, 1, ?, ?, 'file', ?, '', ?, ?, 0, ?)`,
		contentSign, first, name, contentType, now, now, currentDataOrigin(db))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
