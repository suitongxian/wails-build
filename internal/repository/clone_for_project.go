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

// 立项「项目专属模版」：项目负责人关联模版后，可对本项目的模版增删改而不污染共享模版。
// 做法是关联时从所选模版深拷贝一份 origin=local 的「项目专属模版」（编码 TPL-PRJ-<appKey>），
// 之后该项目的所有结构编辑都落在这份副本上，保存时整树回灌 manage（按 code+version 幂等替换）。
//
// 编码确定化（TPL-PRJ-<appKey>）带来幂等：同一项目重复关联 → 直接复用已存在的项目专属模版，
// 保留其上已做的编辑，不会重复克隆。

// MarkTemplateEditedByStage 经环节定位其所属模版并置 edited=1。
func (r *TemplateAuthoringRepository) MarkTemplateEditedByStage(stageID int64) {
	_, _ = r.DB.Exec(`UPDATE data_templates SET edited = 1, update_time = ?
		WHERE id = (SELECT template_id FROM template_stages WHERE id = ?)`, time.Now(), stageID)
}

// MarkTemplateEditedByTask 经任务→环节定位其所属模版并置 edited=1。
func (r *TemplateAuthoringRepository) MarkTemplateEditedByTask(taskID int64) {
	_, _ = r.DB.Exec(`UPDATE data_templates SET edited = 1, update_time = ?
		WHERE id = (SELECT s.template_id FROM template_stages s
			JOIN template_tasks t ON t.template_stage_id = s.id WHERE t.id = ?)`, time.Now(), taskID)
}

// MarkTemplateEditedByFileRule 经标识→环节定位其所属模版并置 edited=1。
func (r *TemplateAuthoringRepository) MarkTemplateEditedByFileRule(ruleID int64) {
	_, _ = r.DB.Exec(`UPDATE data_templates SET edited = 1, update_time = ?
		WHERE id = (SELECT s.template_id FROM template_stages s
			JOIN template_file_rules fr ON fr.template_stage_id = s.id WHERE fr.id = ?)`, time.Now(), ruleID)
}

// ListCertifiedTemplates 列出本地的「项目认定模版」（单位最高权威，下拉置顶用）。
func (r *TemplateAuthoringRepository) ListCertifiedTemplates() ([]models.DataTemplate, error) {
	var list []models.DataTemplate
	err := r.DB.Select(&list, `SELECT * FROM data_templates WHERE disable = 0 AND certified = 1 ORDER BY id DESC`)
	return list, err
}

// ExtractCertifiedTemplate 把某项目的项目专属模版「提取」为单位「项目认定模版」：
// 以 manage 上该项目模版的【最终结构】为准（含环节责任人在别的机器所做改动），在本地另存为
// 一份新模版（origin=local、certified=1、已发布可立项、记录来源项目）。返回新模版本地 id。
func (r *TemplateAuthoringRepository) ExtractCertifiedTemplate(client *http.Client, endpoint, appKey, fromProjectCode string) (int64, error) {
	if strings.TrimSpace(appKey) == "" {
		return 0, fmt.Errorf("appKey 不能为空")
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return 0, fmt.Errorf("未配置 manage_endpoint")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Get(endpoint + "/api/templates/authoring-tree?code=" + url.QueryEscape(projectTemplateCode(appKey)))
	if err != nil {
		return 0, fmt.Errorf("拉取项目模版最终结构失败: %w", err)
	}
	defer resp.Body.Close()
	var tree maCloneResp
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return 0, fmt.Errorf("解析项目模版失败: %w", err)
	}
	if tree.Code != 0 || tree.Data == nil {
		return 0, fmt.Errorf("manage 返回异常: %s", tree.Message)
	}
	// 另存为一份全新的本地模版（自动新编码、重排各级编码——它是独立权威模版，不需沿用 TPL-PRJ 编码）。
	newID, err := r.rebuildClone(tree.Data.Template, tree.Data.Stages, false)
	if err != nil {
		return 0, err
	}
	from := strPtr(fromProjectCode)
	if _, err := r.DB.Exec(`UPDATE data_templates SET certified = 1, certified_from = ?, is_published = 1, edited = 0, update_time = ? WHERE id = ?`,
		from, time.Now(), newID); err != nil {
		return newID, fmt.Errorf("标记项目认定模版失败: %w", err)
	}
	return newID, nil
}

// projectTemplateCode 项目专属模版的确定化编码。
func projectTemplateCode(appKey string) string {
	return "TPL-PRJ-" + strings.TrimSpace(appKey)
}

// FindProjectTemplate 按 appKey 查项目专属模版的本地 id；不存在返回 (0, nil)。
func (r *TemplateAuthoringRepository) FindProjectTemplate(appKey string) (int64, error) {
	if strings.TrimSpace(appKey) == "" {
		return 0, fmt.Errorf("appKey 不能为空")
	}
	var id int64
	err := r.DB.Get(&id, `SELECT id FROM data_templates WHERE template_code = ? AND disable = 0`, projectTemplateCode(appKey))
	if err != nil {
		return 0, nil // 不存在（含 sql.ErrNoRows）→ 视为未克隆
	}
	return id, nil
}

// EnsureEditableProjectTemplate 确保本机有该项目「项目专属模版」的可编辑本地副本，返回本地 id。
//   - 本机已有（关联即克隆的那台机器）→ 直接返回；
//   - 本机没有（如环节责任人在另一台机器补任务）→ 从 manage 按 TPL-PRJ-<appKey> 拉取五层树，
//     在本地重建为可编辑副本，并【保留】template_code 与各级编码，保证保存(整树回灌)落回 manage
//     的同一条记录、且分工按 code 关联不错位。
func (r *TemplateAuthoringRepository) EnsureEditableProjectTemplate(client *http.Client, endpoint, appKey string) (int64, error) {
	if strings.TrimSpace(appKey) == "" {
		return 0, fmt.Errorf("appKey 不能为空")
	}
	if id, _ := r.FindProjectTemplate(appKey); id > 0 {
		return id, nil
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return 0, fmt.Errorf("未配置 manage_endpoint")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	code := projectTemplateCode(appKey)
	resp, err := client.Get(endpoint + "/api/templates/authoring-tree?code=" + url.QueryEscape(code))
	if err != nil {
		return 0, fmt.Errorf("拉取项目专属模版失败: %w", err)
	}
	defer resp.Body.Close()
	var tree maCloneResp
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return 0, fmt.Errorf("解析项目专属模版失败: %w", err)
	}
	if tree.Code != 0 || tree.Data == nil {
		return 0, fmt.Errorf("manage 返回异常: %s", tree.Message)
	}
	return r.buildProjectTemplateFromManageTree(tree.Data.Template, tree.Data.Stages, code)
}

// buildProjectTemplateFromManageTree 把 manage 五层树在本地重建为可编辑的项目专属模版，
// 保留 template_code（=code）与各级 stage/task/file_rule 编码。
func (r *TemplateAuthoringRepository) buildProjectTemplateFromManageTree(t maCloneTemplate, stages []maCloneStage, code string) (int64, error) {
	scope := t.Scope
	if scope == "" || scope == "industry" {
		scope = "unit"
	}
	tpl, err := r.CreateLocalTemplate(CreateTemplateInput{
		Scope:            scope,
		TemplateName:     t.TemplateName,
		ShortCode:        deref(t.ShortCode),
		Manager:          deref(t.Manager),
		Description:      deref(t.Description),
		ApprovalBasis:    deref(t.ApprovalBasis),
		SensitivityLevel: manageSensToScan(&t.ProjectSensitivityLevel),
		Owner:            deref(t.Owner),
	})
	if err != nil {
		return 0, fmt.Errorf("创建项目专属模版失败: %w", err)
	}
	version := t.TemplateVersion
	if version == "" {
		version = "V1.0"
	}
	if _, err := r.DB.Exec(`UPDATE data_templates SET template_code = ?, template_version = ? WHERE id = ?`, code, version, tpl.ID); err != nil {
		return 0, fmt.Errorf("设置项目专属模版编码失败: %w", err)
	}
	for _, s := range stages {
		st, err := r.CreateStage(tpl.ID, StageInput{
			Name:             s.StageName,
			Manager:          deref(s.Manager),
			ManagerUsername:  deref(s.ManagerUsername),
			Members:          deref(s.Members),
			MembersUsernames: deref(s.MembersUsernames),
			Desc:             deref(s.Description),
		})
		if err != nil {
			return tpl.ID, fmt.Errorf("重建环节 %q 失败: %w", s.StageName, err)
		}
		if s.StageCode != "" {
			if _, err := r.DB.Exec(`UPDATE template_stages SET stage_code = ? WHERE id = ?`, s.StageCode, st.ID); err != nil {
				return tpl.ID, err
			}
		}
		for _, tk := range s.Tasks {
			task, err := r.CreateTask(st.ID, TaskInput{
				Name:             tk.TaskName,
				Manager:          deref(tk.Manager),
				SensitivityLevel: manageSensToScan(tk.SensitivityLevel),
				Desc:             deref(tk.Description),
			})
			if err != nil {
				return tpl.ID, fmt.Errorf("重建任务 %q 失败: %w", tk.TaskName, err)
			}
			if tk.TaskCode != "" {
				if _, err := r.DB.Exec(`UPDATE template_tasks SET task_code = ? WHERE id = ?`, tk.TaskCode, task.ID); err != nil {
					return tpl.ID, err
				}
			}
			for _, fr := range tk.FileRules {
				created, err := r.CreateFileRule(task.ID, FileRuleInput{
					FileName:             fr.FileName,
					DataState:            fr.DataState,
					Required:             bool(fr.Required),
					AllowedFileTypes:     normalizeAllowedTypes(deref(fr.AllowedFileTypes)),
					NamingPattern:        deref(fr.NamingPattern),
					SummaryPattern:       deref(fr.SummaryPattern),
					Drafter:              deref(fr.Drafter),
					SensitivityLevel:     manageSensToScan(fr.SensitivityLevel),
					Category:             deref(fr.Category),
					SecurityRequirement:  deref(fr.SecurityRequirement),
					DiffusionRequirement: deref(fr.DiffusionRequirement),
					ArchiveRequirement:   deref(fr.ArchiveRequirement),
					RetentionPeriodDays:  fr.RetentionPeriodDays,
					DestructionRule:      deref(fr.DestructionRule),
				})
				if err != nil {
					return tpl.ID, fmt.Errorf("重建标识 %q 失败: %w", fr.FileName, err)
				}
				if fr.FileRuleCode != "" {
					if _, err := r.DB.Exec(`UPDATE template_file_rules SET file_rule_code = ? WHERE id = ?`, fr.FileRuleCode, created.ID); err != nil {
						return tpl.ID, err
					}
				}
			}
		}
	}
	return tpl.ID, nil
}

// CloneLocalTemplateForApplication 为某集中立项项目克隆一份「项目专属模版」（origin=local），
// 使本项目的结构编辑与共享模版隔离。编码取 TPL-PRJ-<appKey> 确定化：
//   - 已存在（非删除）→ 直接复用，返回其 id（保留已有编辑，不重复克隆）；
//   - 不存在 → 从 srcID 深拷贝五层（环节→任务→标识），返回新建项目专属模版的本地 id。
func (r *TemplateAuthoringRepository) CloneLocalTemplateForApplication(srcID int64, appKey string) (int64, error) {
	if strings.TrimSpace(appKey) == "" {
		return 0, fmt.Errorf("appKey 不能为空")
	}
	if existing, _ := r.FindProjectTemplate(appKey); existing > 0 {
		return existing, nil
	}

	tree, err := r.GetLocalTemplateTree(srcID)
	if err != nil {
		return 0, fmt.Errorf("读取源模版失败: %w", err)
	}
	src := tree.Template

	// 关联即克隆的同时，把所选原始模版的五层树快照存为「项目模版基线」，
	// 供日后「提取项目模版」前的差异对比（首次关联即写，不覆盖）。
	_ = r.SaveProjectTemplateBaseline(appKey, src.TemplateCode, src.TemplateVersion, tree)

	scope := src.Scope
	if scope == "" || scope == "industry" {
		scope = "unit" // 项目专属模版不保留行业归类（与 rebuildClone 一致）
	}
	name := src.TemplateName
	tpl, err := r.CreateLocalTemplate(CreateTemplateInput{
		ClassCode:        deref(src.ClassCode),
		Scope:            scope,
		TemplateName:     name,
		ShortCode:        deref(src.ShortCode),
		Manager:          deref(src.Manager),
		Description:      deref(src.Description),
		ApprovalBasis:    deref(src.ApprovalBasis),
		SensitivityLevel: src.ProjectSensitivityLevel,
		Owner:            deref(src.Owner),
	})
	if err != nil {
		return 0, fmt.Errorf("创建项目专属模版失败: %w", err)
	}
	// 改成确定化编码，便于本项目后续按 appKey 复用、且各项目互不覆盖。
	if _, err := r.DB.Exec(`UPDATE data_templates SET template_code = ? WHERE id = ?`, projectTemplateCode(appKey), tpl.ID); err != nil {
		return 0, fmt.Errorf("设置项目专属模版编码失败: %w", err)
	}

	// 保留源的 stage/task/file_rule 编码：承接分工按 code 关联，前端无论从源模版还是项目专属
	// 模版取树，code 都一致，分工才不会错位。Create* 会自动生成新码，故创建后再改回源码。
	for _, sn := range tree.Stages {
		s := sn.TemplateStage
		st, err := r.CreateStage(tpl.ID, StageInput{
			Name:             s.StageName,
			Manager:          deref(s.Manager),
			ManagerUsername:  deref(s.ManagerUsername),
			Members:          deref(s.Members),
			MembersUsernames: deref(s.MembersUsernames),
			Desc:             deref(s.Description),
		})
		if err != nil {
			return tpl.ID, fmt.Errorf("克隆环节 %q 失败: %w", s.StageName, err)
		}
		if _, err := r.DB.Exec(`UPDATE template_stages SET stage_code = ? WHERE id = ?`, s.StageCode, st.ID); err != nil {
			return tpl.ID, fmt.Errorf("保留环节编码失败: %w", err)
		}
		for _, tn := range sn.Tasks {
			tk := tn.TemplateTask
			task, err := r.CreateTask(st.ID, TaskInput{
				Name:             tk.TaskName,
				Manager:          deref(tk.Manager),
				SensitivityLevel: deref(tk.SensitivityLevel),
				Desc:             deref(tk.Description),
			})
			if err != nil {
				return tpl.ID, fmt.Errorf("克隆任务 %q 失败: %w", tk.TaskName, err)
			}
			if _, err := r.DB.Exec(`UPDATE template_tasks SET task_code = ? WHERE id = ?`, tk.TaskCode, task.ID); err != nil {
				return tpl.ID, fmt.Errorf("保留任务编码失败: %w", err)
			}
			for _, fr := range tn.FileRules {
				created, err := r.CreateFileRule(task.ID, FileRuleInput{
					FileName:             fr.FileName,
					DataState:            fr.DataState,
					Required:             fr.Required == 1,
					AllowedFileTypes:     fr.AllowedFileTypes,
					NamingPattern:        deref(fr.NamingPattern),
					SummaryPattern:       deref(fr.SummaryPattern),
					Drafter:              deref(fr.Drafter),
					SensitivityLevel:     deref(fr.SensitivityLevel),
					Category:             deref(fr.Category),
					SecurityRequirement:  deref(fr.SecurityRequirement),
					DiffusionRequirement: deref(fr.DiffusionRequirement),
					ArchiveRequirement:   deref(fr.ArchiveRequirement),
					RetentionPeriodDays:  fr.RetentionPeriodDays,
					DestructionRule:      deref(fr.DestructionRule),
				})
				if err != nil {
					return tpl.ID, fmt.Errorf("克隆标识 %q 失败: %w", fr.FileName, err)
				}
				if _, err := r.DB.Exec(`UPDATE template_file_rules SET file_rule_code = ? WHERE id = ?`, fr.FileRuleCode, created.ID); err != nil {
					return tpl.ID, fmt.Errorf("保留标识编码失败: %w", err)
				}
			}
		}
	}
	return tpl.ID, nil
}
