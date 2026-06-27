package repository

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"data-asset-scan-go/internal/models"
)

// 项目模版差异（2026-06-27）：以「关联模版时所选的原始模版」为基线，对比项目最终专属模版，
// 列出 工作环节 / 文件任务 / 文件标识 三级的 新增/删除/改名 变更，供「提取项目模版」前的确认弹窗展示。
//
// 对齐方式：各级以 code 对齐（stage_code/task_code/file_rule_code）。克隆与编辑都保留 code，
// 故同 code 即同一节点：仅名称不同 → 改名；基线有最终无 → 删除；基线无最终有 → 新增。

// ── 中性快照结构（基线与最终统一成这种形状再比对）──

type snapFileRule struct {
	Code string `json:"code"`
	Name string `json:"name"`
}
type snapTask struct {
	Code  string         `json:"code"`
	Name  string         `json:"name"`
	Rules []snapFileRule `json:"rules"`
}
type snapStage struct {
	Code  string     `json:"code"`
	Name  string     `json:"name"`
	Tasks []snapTask `json:"tasks"`
}
type snapTree struct {
	Stages []snapStage `json:"stages"`
}

// TemplateChange 一条结构变更（提取确认弹窗逐条展示）。
type TemplateChange struct {
	Level string `json:"level"` // stage | task | file_rule
	Type  string `json:"type"`  // added | removed | renamed
	Stage string `json:"stage"` // 所属工作环节名（定位）
	Task  string `json:"task,omitempty"`
	Name  string `json:"name"`           // 当前名称（新增/删除）
	From  string `json:"from,omitempty"` // 改名前
	To    string `json:"to,omitempty"`   // 改名后
}

// snapFromLocalTree 把本地五层树（源模版）转成中性快照。
func snapFromLocalTree(t *models.LocalTemplateTree) snapTree {
	out := snapTree{Stages: make([]snapStage, 0)}
	if t == nil {
		return out
	}
	for _, st := range t.Stages {
		s := snapStage{Code: st.StageCode, Name: st.StageName, Tasks: make([]snapTask, 0)}
		for _, tk := range st.Tasks {
			task := snapTask{Code: tk.TaskCode, Name: tk.TaskName, Rules: make([]snapFileRule, 0)}
			for _, fr := range tk.FileRules {
				task.Rules = append(task.Rules, snapFileRule{Code: fr.FileRuleCode, Name: fr.FileName})
			}
			s.Tasks = append(s.Tasks, task)
		}
		out.Stages = append(out.Stages, s)
	}
	return out
}

// snapFromManageStages 把 manage authoring-tree 的环节列表（最终结构）转成中性快照。
func snapFromManageStages(stages []maCloneStage) snapTree {
	out := snapTree{Stages: make([]snapStage, 0)}
	for _, st := range stages {
		s := snapStage{Code: st.StageCode, Name: st.StageName, Tasks: make([]snapTask, 0)}
		for _, tk := range st.Tasks {
			task := snapTask{Code: tk.TaskCode, Name: tk.TaskName, Rules: make([]snapFileRule, 0)}
			for _, fr := range tk.FileRules {
				task.Rules = append(task.Rules, snapFileRule{Code: fr.FileRuleCode, Name: fr.FileName})
			}
			s.Tasks = append(s.Tasks, task)
		}
		out.Stages = append(out.Stages, s)
	}
	return out
}

// diffSnap 比对基线与最终，产出按 环节→任务→标识 顺序的变更清单。
func diffSnap(base, final snapTree) []TemplateChange {
	changes := make([]TemplateChange, 0)

	baseStage := map[string]snapStage{}
	for _, s := range base.Stages {
		baseStage[s.Code] = s
	}
	finalStage := map[string]snapStage{}
	for _, s := range final.Stages {
		finalStage[s.Code] = s
	}

	// 以最终为主序：新增 / 改名 / 任务与标识的下钻对比。
	for _, fs := range final.Stages {
		bs, ok := baseStage[fs.Code]
		if !ok {
			changes = append(changes, TemplateChange{Level: "stage", Type: "added", Stage: fs.Name, Name: fs.Name})
			continue
		}
		if bs.Name != fs.Name {
			changes = append(changes, TemplateChange{Level: "stage", Type: "renamed", Stage: fs.Name, Name: fs.Name, From: bs.Name, To: fs.Name})
		}
		changes = append(changes, diffTasks(fs.Name, bs, fs)...)
	}
	// 基线有、最终无 → 删除的环节。
	for _, bs := range base.Stages {
		if _, ok := finalStage[bs.Code]; !ok {
			changes = append(changes, TemplateChange{Level: "stage", Type: "removed", Stage: bs.Name, Name: bs.Name})
		}
	}
	return changes
}

func diffTasks(stageName string, bs, fs snapStage) []TemplateChange {
	changes := make([]TemplateChange, 0)
	baseTask := map[string]snapTask{}
	for _, t := range bs.Tasks {
		baseTask[t.Code] = t
	}
	finalTask := map[string]snapTask{}
	for _, t := range fs.Tasks {
		finalTask[t.Code] = t
	}
	for _, ft := range fs.Tasks {
		bt, ok := baseTask[ft.Code]
		if !ok {
			changes = append(changes, TemplateChange{Level: "task", Type: "added", Stage: stageName, Task: ft.Name, Name: ft.Name})
			continue
		}
		if bt.Name != ft.Name {
			changes = append(changes, TemplateChange{Level: "task", Type: "renamed", Stage: stageName, Task: ft.Name, Name: ft.Name, From: bt.Name, To: ft.Name})
		}
		changes = append(changes, diffRules(stageName, ft.Name, bt, ft)...)
	}
	for _, bt := range bs.Tasks {
		if _, ok := finalTask[bt.Code]; !ok {
			changes = append(changes, TemplateChange{Level: "task", Type: "removed", Stage: stageName, Task: bt.Name, Name: bt.Name})
		}
	}
	return changes
}

func diffRules(stageName, taskName string, bt, ft snapTask) []TemplateChange {
	changes := make([]TemplateChange, 0)
	baseRule := map[string]snapFileRule{}
	for _, fr := range bt.Rules {
		baseRule[fr.Code] = fr
	}
	finalRule := map[string]snapFileRule{}
	for _, fr := range ft.Rules {
		finalRule[fr.Code] = fr
	}
	for _, fr := range ft.Rules {
		br, ok := baseRule[fr.Code]
		if !ok {
			changes = append(changes, TemplateChange{Level: "file_rule", Type: "added", Stage: stageName, Task: taskName, Name: fr.Name})
			continue
		}
		if br.Name != fr.Name {
			changes = append(changes, TemplateChange{Level: "file_rule", Type: "renamed", Stage: stageName, Task: taskName, Name: fr.Name, From: br.Name, To: fr.Name})
		}
	}
	for _, br := range bt.Rules {
		if _, ok := finalRule[br.Code]; !ok {
			changes = append(changes, TemplateChange{Level: "file_rule", Type: "removed", Stage: stageName, Task: taskName, Name: br.Name})
		}
	}
	return changes
}

// ── 基线持久化 ──

// SaveProjectTemplateBaseline 关联模版时把所选原始模版的五层树快照存为基线（一项目一条，已存在则不覆盖）。
func (r *TemplateAuthoringRepository) SaveProjectTemplateBaseline(appKey, sourceCode, sourceVersion string, tree *models.LocalTemplateTree) error {
	if strings.TrimSpace(appKey) == "" {
		return fmt.Errorf("appKey 不能为空")
	}
	snap := snapFromLocalTree(tree)
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(`INSERT OR IGNORE INTO project_template_baseline (app_key, source_code, source_version, baseline_json, create_time) VALUES (?, ?, ?, ?, ?)`,
		appKey, sourceCode, sourceVersion, string(b), time.Now())
	return err
}

// getProjectTemplateBaseline 取本地项目基线快照；不存在返回 ok=false。
func (r *TemplateAuthoringRepository) getProjectTemplateBaseline(appKey string) (snapTree, bool) {
	var js string
	if err := r.DB.Get(&js, `SELECT baseline_json FROM project_template_baseline WHERE app_key = ?`, appKey); err != nil {
		return snapTree{}, false
	}
	var snap snapTree
	if err := json.Unmarshal([]byte(js), &snap); err != nil {
		return snapTree{}, false
	}
	return snap, true
}

// BuildBaselineSnapshotJSON 把源模版五层树序列化为基线快照 JSON（推送到 manage 云端存放用）。
func (r *TemplateAuthoringRepository) BuildBaselineSnapshotJSON(tree *models.LocalTemplateTree) (string, error) {
	b, err := json.Marshal(snapFromLocalTree(tree))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// fetchManageBaseline 从 manage 取云端基线快照（多端可对比）；不存在返回 ok=false。
func (r *TemplateAuthoringRepository) fetchManageBaseline(client *http.Client, endpoint, appKey string) (snapTree, bool) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Get(endpoint + "/api/centralized-projects/template-baseline?application_id=" + url.QueryEscape(appKey))
	if err != nil {
		return snapTree{}, false
	}
	defer resp.Body.Close()
	var out struct {
		Code int `json:"code"`
		Data *struct {
			BaselineJSON string `json:"baseline_json"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return snapTree{}, false
	}
	if out.Code != 0 || out.Data == nil || strings.TrimSpace(out.Data.BaselineJSON) == "" {
		return snapTree{}, false
	}
	var snap snapTree
	if err := json.Unmarshal([]byte(out.Data.BaselineJSON), &snap); err != nil {
		return snapTree{}, false
	}
	return snap, true
}

// DiffProjectTemplate 计算项目模版相对基线的改动清单：拉 manage 上项目专属模版的最终结构，与基线对比。
// 返回 (changes, hasBaseline, error)。无基线时 hasBaseline=false（前端据此提示无法逐条对比）。
func (r *TemplateAuthoringRepository) DiffProjectTemplate(client *http.Client, endpoint, appKey string) ([]TemplateChange, bool, error) {
	if strings.TrimSpace(appKey) == "" {
		return nil, false, fmt.Errorf("appKey 不能为空")
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil, false, fmt.Errorf("未配置 manage_endpoint")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Get(endpoint + "/api/templates/authoring-tree?code=" + url.QueryEscape(projectTemplateCode(appKey)))
	if err != nil {
		return nil, false, fmt.Errorf("拉取项目模版最终结构失败: %w", err)
	}
	defer resp.Body.Close()
	var tree maCloneResp
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, false, fmt.Errorf("解析项目模版失败: %w", err)
	}
	if tree.Code != 0 || tree.Data == nil {
		return nil, false, fmt.Errorf("manage 返回异常: %s", tree.Message)
	}
	final := snapFromManageStages(tree.Data.Stages)
	// 优先用 manage 云端基线（任意端可对比）；取不到再回退本地基线（旧数据/离线兜底）。
	base, ok := r.fetchManageBaseline(client, endpoint, appKey)
	if !ok {
		base, ok = r.getProjectTemplateBaseline(appKey)
	}
	if !ok {
		return []TemplateChange{}, false, nil
	}
	return diffSnap(base, final), true, nil
}
