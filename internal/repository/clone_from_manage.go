package repository

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 立项「从已有模版克隆」（2026-06-01）：把 manage 端某模版的完整五层结构拉下来，
// 在 scan 本地重建为可编辑模版（origin=local，新 template_code），供立项人调整后再推回 manage 立项。
//
// 复用 manage GET /api/templates/authoring-tree?code= 返回的五层树（项目▸事项▸任务▸标识，含人员/敏感级）。
// 词表对齐：manage 用 core_secret，scan 用 core，克隆时映射回来。

type maCloneFileRule struct {
	FileRuleCode     string   `json:"file_rule_code"` // 项目专属模版回拉时保留编码（按 code 关联分工）
	FileName         string   `json:"file_name"`
	DataState        string   `json:"data_state"`
	Required         flexBool `json:"required"` // 兼容 true/false 与 0/1
	AllowedFileTypes *string  `json:"allowed_file_types"`
	NamingPattern    *string  `json:"naming_pattern"`
	SummaryPattern   *string  `json:"summary_pattern"` // 内容要求
	Drafter          *string  `json:"drafter"`
	SensitivityLevel *string  `json:"sensitivity_level"`
	// L6 文档标识管控类字段
	Category             *string `json:"category"`
	SecurityRequirement  *string `json:"security_requirement"`
	DiffusionRequirement *string `json:"diffusion_requirement"`
	ArchiveRequirement   *string `json:"archive_requirement"`
	RetentionPeriodDays  *int    `json:"retention_period_days"`
	DestructionRule      *string `json:"destruction_rule"`
}

// flexBool 兼容 JSON 中的布尔(true/false)、数字(1/0)、字符串("true"/"1")，统一为 bool。
// 不同来源的 required 表达不一：scan/manage 库为 0/1，模版平台「模版导出」为 true/false。
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case bool:
		*b = flexBool(x)
	case float64:
		*b = flexBool(x != 0)
	case string:
		*b = flexBool(x == "1" || strings.EqualFold(x, "true"))
	default:
		*b = false
	}
	return nil
}

type maCloneTask struct {
	TaskCode         string            `json:"task_code"` // 回拉项目专属模版时保留编码
	TaskName         string            `json:"task_name"`
	Manager          *string           `json:"manager"`
	SensitivityLevel *string           `json:"sensitivity_level"`
	Description      *string           `json:"description"`
	FileRules        []maCloneFileRule `json:"file_rules"`
}
type maCloneStage struct {
	StageCode        string        `json:"stage_code"` // 回拉项目专属模版时保留编码
	StageName        string        `json:"stage_name"`
	Manager          *string       `json:"manager"`
	ManagerUsername  *string       `json:"manager_username"`
	Members          *string       `json:"members"`
	MembersUsernames *string       `json:"members_usernames"`
	Description      *string       `json:"description"`
	Tasks            []maCloneTask `json:"tasks"`
}
type maCloneTemplate struct {
	TemplateCode            string  `json:"template_code"`
	TemplateVersion         string  `json:"template_version"`
	TemplateName            string  `json:"template_name"`
	ProjectSensitivityLevel string  `json:"project_sensitivity_level"`
	Scope                   string  `json:"scope"`
	ShortCode               *string `json:"short_code"`
	Manager                 *string `json:"manager"`
	Owner                   *string `json:"owner"`
	ApprovalBasis           *string `json:"approval_basis"`
	Description             *string `json:"description"`
}
type maCloneResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *struct {
		Template maCloneTemplate `json:"template"`
		Stages   []maCloneStage  `json:"stages"`
	} `json:"data"`
}

// manageSensToScan 把 manage 的 core_secret 映射回 scan 的 core；其余原样。
func manageSensToScan(s *string) string {
	if s == nil {
		return ""
	}
	if *s == "core_secret" {
		return "core"
	}
	return *s
}

// CloneManageTemplateToLocal 拉取 manage 模版完整五层并在本地重建为可编辑模版，返回新本地模版 id。
func (r *TemplateAuthoringRepository) CloneManageTemplateToLocal(client *http.Client, endpoint, templateCode string) (int64, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return 0, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	if templateCode == "" {
		return 0, fmt.Errorf("缺少 template_code")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := client.Get(endpoint + "/api/templates/authoring-tree?code=" + url.QueryEscape(templateCode))
	if err != nil {
		return 0, fmt.Errorf("拉取 manage 模版失败: %w", err)
	}
	defer resp.Body.Close()
	var tree maCloneResp
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return 0, fmt.Errorf("解析 manage 模版失败: %w", err)
	}
	if tree.Code != 0 || tree.Data == nil {
		return 0, fmt.Errorf("manage 返回异常: %s", tree.Message)
	}

	return r.rebuildClone(tree.Data.Template, tree.Data.Stages, false)
}

// normalizeAllowedTypes 统一允许文件类型为逗号分隔（本地 CreateFileRule 期望的格式）。
// 模版管理平台 tree 返回的是 JSON 数组字符串（如 ["PDF","DOCX"]）；manage 旧接口是逗号串。
func normalizeAllowedTypes(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			return strings.Join(arr, ",")
		}
	}
	return s
}

// rebuildClone 把一棵五层克隆树在本地重建为可编辑模版（origin=local，新 template_code），返回新本地 id。
// typesAreJSON=true 时把 file_rules.allowed_file_types 由 JSON 数组转成逗号串。
func (r *TemplateAuthoringRepository) rebuildClone(t maCloneTemplate, stages []maCloneStage, typesAreJSON bool) (int64, error) {
	// 另存为本地裁剪：不允许保留「行业模版」归类——行业模版另存为后降级为「单位」，
	// 用户可在编辑器再改为 部门/个人（CreateLocalTemplate 会拒绝 industry）。
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
		return 0, fmt.Errorf("创建本地模版失败: %w", err)
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
			return tpl.ID, fmt.Errorf("克隆环节 %q 失败: %w", s.StageName, err)
		}
		for _, tk := range s.Tasks {
			task, err := r.CreateTask(st.ID, TaskInput{
				Name:             tk.TaskName,
				Manager:          deref(tk.Manager),
				SensitivityLevel: manageSensToScan(tk.SensitivityLevel),
				Desc:             deref(tk.Description),
			})
			if err != nil {
				return tpl.ID, fmt.Errorf("克隆任务 %q 失败: %w", tk.TaskName, err)
			}
			for _, fr := range tk.FileRules {
				types := deref(fr.AllowedFileTypes)
				if typesAreJSON {
					types = normalizeAllowedTypes(types)
				}
				if _, err := r.CreateFileRule(task.ID, FileRuleInput{
					FileName:             fr.FileName,
					DataState:            fr.DataState,
					Required:             bool(fr.Required),
					AllowedFileTypes:     types,
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
				}); err != nil {
					return tpl.ID, fmt.Errorf("克隆标识 %q 失败: %w", fr.FileName, err)
				}
			}
		}
	}
	return tpl.ID, nil
}

// tsListResp 模版管理平台 GET /api/local-templates/list 的精简结构（仅取定位用字段）。
type tsListResp struct {
	Code int `json:"code"`
	Data []struct {
		ID           int64  `json:"id"`
		TemplateCode string `json:"template_code"`
		Status       string `json:"status"`
	} `json:"data"`
}

// CloneTemplateServerToLocal 从「模版管理平台」(template-manage，:19092) 克隆模版到本地可编辑模版。
// 平台 tree 接口按 id 取，故先 GET /api/local-templates/list 用 template_code 定位 id，
// 再 GET /api/local-templates/tree?id= 取五层树重建。返回新本地模版 id。
func (r *TemplateAuthoringRepository) CloneTemplateServerToLocal(client *http.Client, endpoint, templateCode string) (int64, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return 0, fmt.Errorf("未配置模版服务器地址，请在登录页「服务端配置」中填写")
	}
	if templateCode == "" {
		return 0, fmt.Errorf("缺少 template_code")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	// 1. 列表里按 template_code 定位 id —— 仅在「已发布」(active) 模版中查找
	listResp, err := client.Get(endpoint + "/api/local-templates/list?status=active")
	if err != nil {
		return 0, fmt.Errorf("拉取模版列表失败: %w", err)
	}
	var list tsListResp
	err = json.NewDecoder(listResp.Body).Decode(&list)
	listResp.Body.Close()
	if err != nil {
		return 0, fmt.Errorf("解析模版列表失败: %w", err)
	}
	if list.Code != 0 {
		return 0, fmt.Errorf("模版平台 list 返回异常 code=%d", list.Code)
	}
	var id int64
	for _, it := range list.Data {
		if it.TemplateCode != templateCode {
			continue
		}
		if it.Status != "" && it.Status != "active" {
			return 0, fmt.Errorf("模版 %s 未发布，无法同步到本地（仅可同步已发布模版）", templateCode)
		}
		id = it.ID
		break
	}
	if id == 0 {
		return 0, fmt.Errorf("模版平台未找到编码为 %s 的已发布模版", templateCode)
	}

	// 2. 按 id 取五层树
	resp, err := client.Get(fmt.Sprintf("%s/api/local-templates/tree?id=%d", endpoint, id))
	if err != nil {
		return 0, fmt.Errorf("拉取模版详情失败: %w", err)
	}
	defer resp.Body.Close()
	var tree maCloneResp
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return 0, fmt.Errorf("解析模版详情失败: %w", err)
	}
	if tree.Code != 0 || tree.Data == nil {
		return 0, fmt.Errorf("模版平台返回异常: %s", tree.Message)
	}
	return r.rebuildClone(tree.Data.Template, tree.Data.Stages, true)
}
