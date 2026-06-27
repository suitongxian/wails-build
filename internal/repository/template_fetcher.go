package repository

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ManageFullResponse 是 manage 端 GET /api/templates/full 的响应结构
//
//	{
//	  "code": 0,
//	  "message": "success",
//	  "data": {
//	    "template": {...},
//	    "business_class": {...} | null,
//	    "stages": [
//	      {
//	        ...stage fields,
//	        "file_rules": [...]
//	      }
//	    ]
//	  }
//	}
type ManageFullResponse struct {
	Code    int                  `json:"code"`
	Message string               `json:"message"`
	Data    *ManageFullStructure `json:"data"`
}

type ManageFullStructure struct {
	Template      *ManageTemplate      `json:"template"`
	BusinessClass *ManageBusinessClass `json:"business_class"`
	Stages        []ManageStage        `json:"stages"`
}

type ManageBusinessClass struct {
	ID          int64   `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description *string `json:"description"`
}

type ManageTemplate struct {
	ID                      int64   `json:"id"`
	TemplateCode            string  `json:"template_code"`
	TemplateName            string  `json:"template_name"`
	TemplateVersion         string  `json:"template_version"`
	ClassID                 *int64  `json:"class_id"`
	Scenario                *string `json:"scenario"`
	Publisher               string  `json:"publisher"`
	Status                  string  `json:"status"`
	ProjectSensitivityLevel string  `json:"project_sensitivity_level"`
	UseShareScope           *string `json:"use_share_scope"`
	SharingOpenConditions   *string `json:"sharing_open_conditions"`
	Description             *string `json:"description"`
}

type ManageStage struct {
	ID               int64            `json:"id"`
	StageCode        string           `json:"stage_code"`
	StageName        string           `json:"stage_name"`
	StageType        string           `json:"stage_type"`
	SortOrder        int              `json:"sort_order"`
	Description      *string          `json:"description"`
	DefaultRoleCodes *string          `json:"default_role_codes"`
	Tasks            []ManageTask     `json:"tasks"`
	FileRules        []ManageFileRule `json:"file_rules"`
}

// ManageTask 是 manage /api/templates/full 中 stage 下的「文件任务」层（五层模版的中间层）。
type ManageTask struct {
	ID        int64  `json:"id"`
	TaskCode  string `json:"task_code"`
	TaskName  string `json:"task_name"`
	SortOrder int    `json:"sort_order"`
}

type ManageFileRule struct {
	ID                     int64   `json:"id"`
	FileRuleCode           string  `json:"file_rule_code"`
	FileName               string  `json:"file_name"`
	DataState              string  `json:"data_state"`
	Required               int     `json:"required"`
	AllowedFileTypes       string  `json:"allowed_file_types"`
	NamingPattern          *string `json:"naming_pattern"`
	SummaryPattern         *string `json:"summary_pattern"`
	DefaultRetentionPolicy *string `json:"default_retention_policy"`
	SortOrder              int     `json:"sort_order"`
	// TaskCode 关联到 stage 下的某个 task（五层模版）。4 层模版留空。
	TaskCode *string `json:"task_code"`
}

// TemplateFetcher 从 manage 端拉取模版到本地缓存
type TemplateFetcher struct {
	cacheRepo  *TemplateCacheRepository
	configRepo *SystemConfigRepository
	httpClient *http.Client
}

func NewTemplateFetcher(cache *TemplateCacheRepository, config *SystemConfigRepository) *TemplateFetcher {
	return &TemplateFetcher{
		cacheRepo:  cache,
		configRepo: config,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// FetchByCode 按业务编码 + 版本拉取并写入本地缓存。返回本地模版 ID。
func (f *TemplateFetcher) FetchByCode(code, version string) (int64, error) {
	endpoint := strings.TrimRight(f.configRepo.GetValue(KeyManageEndpoint), "/")
	if endpoint == "" {
		return 0, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}

	q := url.Values{}
	q.Set("code", code)
	q.Set("version", version)
	reqURL := fmt.Sprintf("%s/api/templates/full?%s", endpoint, q.Encode())

	full, err := f.callManage(reqURL)
	if err != nil {
		return 0, err
	}
	return f.persist(full, endpoint)
}

// ---------------------------------------------------------------------------
// 从「模版管理平台」(template-manage，:19092) 按 code 拉取并写入本地缓存。
//
// 背景：分工/总览里的「在线」模版列表来自 ListTemplateServerActive（template-server，
// /api/local-templates/list），其 id 属于 template-server 库；而 FetchByID/FetchByCode
// 走的是 manage_endpoint（data-asset-manage，/api/templates/full）——是另一台服务器。
// 用 template-server 的 id 去 manage 查会取错/取空。本方法直接打 template-server 的
// /api/local-templates/tree，按原 template_code+version upsert 进缓存（复用 persist+prune）。
// ---------------------------------------------------------------------------

// tsTreeResp template-server GET /api/local-templates/tree 的响应（带各级 code）。
type tsTreeResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *struct {
		Template tsTreeTemplate `json:"template"`
		Stages   []tsTreeStage  `json:"stages"`
	} `json:"data"`
}

type tsTreeTemplate struct {
	TemplateCode            string  `json:"template_code"`
	TemplateName            string  `json:"template_name"`
	TemplateVersion         string  `json:"template_version"`
	ProjectSensitivityLevel string  `json:"project_sensitivity_level"`
	Status                  string  `json:"status"`
	Publisher               *string `json:"publisher"`
	Description             *string `json:"description"`
	Scenario                *string `json:"scenario"`
}

type tsTreeStage struct {
	StageCode        string       `json:"stage_code"`
	StageName        string       `json:"stage_name"`
	StageType        string       `json:"stage_type"`
	SortOrder        int          `json:"sort_order"`
	Description      *string      `json:"description"`
	DefaultRoleCodes *string      `json:"default_role_codes"`
	Tasks            []tsTreeTask `json:"tasks"`
}

type tsTreeTask struct {
	TaskCode  string           `json:"task_code"`
	TaskName  string           `json:"task_name"`
	SortOrder int              `json:"sort_order"`
	FileRules []tsTreeFileRule `json:"file_rules"`
}

type tsTreeFileRule struct {
	FileRuleCode           string  `json:"file_rule_code"`
	FileName               string  `json:"file_name"`
	DataState              string  `json:"data_state"`
	Required               int     `json:"required"`
	AllowedFileTypes       *string `json:"allowed_file_types"`
	NamingPattern          *string `json:"naming_pattern"`
	SummaryPattern         *string `json:"summary_pattern"`
	DefaultRetentionPolicy *string `json:"default_retention_policy"`
	SortOrder              int     `json:"sort_order"`
}

// FetchFromTemplateServer 从模版管理平台按 code(+version) 拉取五层结构写入本地缓存，返回本地模版 id。
// version 为空时取该 code 下任一已发布版本。
func (f *TemplateFetcher) FetchFromTemplateServer(code, version string) (int64, error) {
	endpoint := strings.TrimRight(f.configRepo.GetEffectiveTemplateServerEndpoint(), "/")
	if endpoint == "" {
		return 0, fmt.Errorf("未配置模版服务器地址，请在登录页「服务端配置」中填写")
	}
	if code == "" {
		return 0, fmt.Errorf("缺少 template_code")
	}

	// 1. 列表里按 code(+version) 定位 id（仅已发布）。
	list, err := f.ListTemplateServerActive()
	if err != nil {
		return 0, err
	}
	var id int64
	for _, it := range list {
		if it.TemplateCode != code {
			continue
		}
		if version != "" && it.TemplateVersion != version {
			continue
		}
		id = it.ID
		break
	}
	if id == 0 {
		return 0, fmt.Errorf("模版服务器未找到编码为 %s 的已发布模版", code)
	}

	// 2. 取五层树（带各级 code）。
	reqURL := fmt.Sprintf("%s/api/local-templates/tree?id=%d", endpoint, id)
	resp, err := f.httpClient.Get(reqURL)
	if err != nil {
		return 0, fmt.Errorf("拉取模版详情失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("模版服务器返回非 200: %d, body=%s", resp.StatusCode, string(body))
	}
	var tree tsTreeResp
	if err := json.Unmarshal(body, &tree); err != nil {
		return 0, fmt.Errorf("解析模版详情失败: %w", err)
	}
	if tree.Code != 0 || tree.Data == nil {
		return 0, fmt.Errorf("模版服务器返回异常: code=%d, msg=%s", tree.Code, tree.Message)
	}

	// 3. 映射为 ManageFullStructure（与 /api/templates/full 同构），复用 persist+prune。
	full := mapTemplateServerTree(id, tree.Data.Template, tree.Data.Stages)
	return f.persist(full, endpoint)
}

// mapTemplateServerTree 把 template-server 的五层树映射为 ManageFullStructure。
// 词表对齐：平台 core_secret → scan core；文件规则从各 task 下展平并回填 TaskCode。
func mapTemplateServerTree(remoteID int64, t tsTreeTemplate, stages []tsTreeStage) *ManageFullStructure {
	derefStr := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	mStages := make([]ManageStage, 0, len(stages))
	for _, s := range stages {
		tasks := make([]ManageTask, 0, len(s.Tasks))
		rules := make([]ManageFileRule, 0)
		for _, tk := range s.Tasks {
			tasks = append(tasks, ManageTask{TaskCode: tk.TaskCode, TaskName: tk.TaskName, SortOrder: tk.SortOrder})
			for _, r := range tk.FileRules {
				code := tk.TaskCode
				rules = append(rules, ManageFileRule{
					FileRuleCode:           r.FileRuleCode,
					FileName:               r.FileName,
					DataState:              r.DataState,
					Required:               r.Required,
					AllowedFileTypes:       derefStr(r.AllowedFileTypes),
					NamingPattern:          r.NamingPattern,
					SummaryPattern:         r.SummaryPattern,
					DefaultRetentionPolicy: r.DefaultRetentionPolicy,
					SortOrder:              r.SortOrder,
					TaskCode:               &code,
				})
			}
		}
		mStages = append(mStages, ManageStage{
			StageCode:        s.StageCode,
			StageName:        s.StageName,
			StageType:        s.StageType,
			SortOrder:        s.SortOrder,
			Description:      s.Description,
			DefaultRoleCodes: s.DefaultRoleCodes,
			Tasks:            tasks,
			FileRules:        rules,
		})
	}
	sens := t.ProjectSensitivityLevel
	if sens == "core_secret" {
		sens = "core"
	}
	return &ManageFullStructure{
		Template: &ManageTemplate{
			ID:                      remoteID,
			TemplateCode:            t.TemplateCode,
			TemplateName:            t.TemplateName,
			TemplateVersion:         t.TemplateVersion,
			Publisher:               derefStr(t.Publisher),
			Status:                  t.Status,
			ProjectSensitivityLevel: sens,
			Description:             t.Description,
			Scenario:                t.Scenario,
		},
		Stages: mStages,
	}
}

// ManageTemplateListItem manage 端 /api/templates/list 单条结构
type ManageTemplateListItem struct {
	ID              int64  `json:"id"`
	TemplateCode    string `json:"template_code"`
	TemplateName    string `json:"template_name"`
	TemplateVersion string `json:"template_version"`
	Status          string `json:"status"`
}

// ManageListResponse manage 端 /api/templates/list 响应包装
type ManageListResponse struct {
	Code    int                      `json:"code"`
	Message string                   `json:"message"`
	Data    []ManageTemplateListItem `json:"data"`
}

// FetchAllActiveResult FetchAllActive 的结果
type FetchAllActiveResult struct {
	TotalRemote int      // manage 端返回的 active 模版数
	Synced      int      // 成功同步到本地的条数
	Errors      []string // 同步失败的明细
	LocalIDs    []int64  // 成功同步的本地模版 id
}

// ListRemoteActive 仅 list（不 fetch full）：调 manage 的
// /api/templates/list?status=active 拿到全量 active 模版的列表，用于"总览"展示。
// 与 FetchAllActive 共用 manage 端响应结构，但不下载详情、不写入本地缓存。
func (f *TemplateFetcher) ListRemoteActive() ([]ManageTemplateListItem, error) {
	endpoint := strings.TrimRight(f.configRepo.GetValue(KeyManageEndpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	reqURL := fmt.Sprintf("%s/api/templates/list?status=active&is_project=0", endpoint)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	// manage_token 已废弃，不再发送 X-Sync-Token 头
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manage 返回非 200: %d, body=%s", resp.StatusCode, string(body))
	}
	var raw ManageListResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析模版列表失败: %w", err)
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("manage list 返回错误 code=%d, msg=%s", raw.Code, raw.Message)
	}
	// 过滤掉系统基础设施模版（个人文件模版供 scan 建归档容器用，非业务通用模版，不参与立项选择）。
	out := make([]ManageTemplateListItem, 0, len(raw.Data))
	for _, t := range raw.Data {
		if strings.HasPrefix(t.TemplateCode, "TPL-PERSONAL-FILES") {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// ListTemplateServerActive 从「模版管理平台」(template-manage，:19092) 列出模版，
// 调 /api/local-templates/list，映射为与 manage 同构的 ManageTemplateListItem（字段名一致，可直接解析）。
// 用于「数据业务模版总览」展示——云端模版列表现走这台独立的模版服务器。
func (f *TemplateFetcher) ListTemplateServerActive() ([]ManageTemplateListItem, error) {
	endpoint := strings.TrimRight(f.configRepo.GetEffectiveTemplateServerEndpoint(), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置模版服务器地址，请在登录页「服务端配置」中填写")
	}
	// 只拉「已发布」(active) 模版：审订中/AI生成的草稿不下发到终端。
	reqURL := endpoint + "/api/local-templates/list?status=active"
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用模版服务器失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模版服务器返回非 200: %d, body=%s", resp.StatusCode, string(body))
	}
	var raw ManageListResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析模版列表失败: %w", err)
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("模版服务器 list 返回错误 code=%d, msg=%s", raw.Code, raw.Message)
	}
	out := make([]ManageTemplateListItem, 0, len(raw.Data))
	for _, t := range raw.Data {
		if strings.HasPrefix(t.TemplateCode, "TPL-PERSONAL-FILES") {
			continue // 基础设施模版不参与展示
		}
		if t.Status != "" && t.Status != "active" {
			continue // 双保险：仅保留已发布模版（服务端已按 status=active 过滤）
		}
		out = append(out, t)
	}
	return out, nil
}

// FetchAllActive 同步 manage 端所有 active 状态的模版到本地
//
// 用于"重新同步"按钮：先调 manage 的 /api/templates/list?status=active
// 拿到全量列表，再逐个 FetchByID 持久化。
// 单条失败不打断整体，最后聚合返回 (成功数 + 失败明细)。
func (f *TemplateFetcher) FetchAllActive() (*FetchAllActiveResult, error) {
	raw, err := f.ListRemoteActive()
	if err != nil {
		return nil, err
	}

	result := &FetchAllActiveResult{
		TotalRemote: len(raw),
		Errors:      []string{},
		LocalIDs:    []int64{},
	}
	for _, tpl := range raw {
		localID, err := f.FetchByID(tpl.ID)
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("%s %s (remote_id=%d): %v", tpl.TemplateCode, tpl.TemplateVersion, tpl.ID, err))
			continue
		}
		result.Synced++
		result.LocalIDs = append(result.LocalIDs, localID)
	}
	return result, nil
}

// FetchByID 按 manage 端 ID 拉取
func (f *TemplateFetcher) FetchByID(remoteID int64) (int64, error) {
	endpoint := strings.TrimRight(f.configRepo.GetValue(KeyManageEndpoint), "/")
	if endpoint == "" {
		return 0, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}

	reqURL := fmt.Sprintf("%s/api/templates/full?id=%d", endpoint, remoteID)
	full, err := f.callManage(reqURL)
	if err != nil {
		return 0, err
	}
	return f.persist(full, endpoint)
}

// callManage 发送 HTTP 请求并解析响应
func (f *TemplateFetcher) callManage(reqURL string) (*ManageFullStructure, error) {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	// manage_token 已废弃，不再发送 X-Sync-Token 头

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manage 返回非 200: %d, body=%s", resp.StatusCode, string(body))
	}

	var raw ManageFullResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("manage 返回错误 code=%d, msg=%s", raw.Code, raw.Message)
	}
	if raw.Data == nil || raw.Data.Template == nil {
		return nil, fmt.Errorf("响应内容为空")
	}
	return raw.Data, nil
}

// persist 将拉取的结构写入本地缓存表（事务）
func (f *TemplateFetcher) persist(full *ManageFullStructure, endpoint string) (int64, error) {
	// 1. 业务分类（如果存在）
	var classCode *string
	if full.BusinessClass != nil {
		bc := full.BusinessClass
		if err := f.cacheRepo.SaveBusinessClass(bc.ID, bc.Code, bc.Name, bc.Type, bc.Description); err != nil {
			return 0, fmt.Errorf("保存 business_class 失败: %w", err)
		}
		classCode = &bc.Code
	}

	// 2. 模版主表
	tpl := full.Template
	localTplID, err := f.cacheRepo.SaveTemplate(SaveTemplateInput{
		RemoteID:                tpl.ID,
		TemplateCode:            tpl.TemplateCode,
		TemplateName:            tpl.TemplateName,
		TemplateVersion:         tpl.TemplateVersion,
		ClassCode:               classCode,
		Scenario:                tpl.Scenario,
		Publisher:               &tpl.Publisher,
		Status:                  tpl.Status,
		ProjectSensitivityLevel: tpl.ProjectSensitivityLevel,
		UseShareScope:           tpl.UseShareScope,
		SharingOpenConditions:   tpl.SharingOpenConditions,
		Description:             tpl.Description,
		SourceEndpoint:          &endpoint,
	})
	if err != nil {
		return 0, fmt.Errorf("保存 data_template 失败: %w", err)
	}

	// 3. 工作环节 + 文件规则
	keepStageCodes := make([]string, 0, len(full.Stages))
	for _, s := range full.Stages {
		keepStageCodes = append(keepStageCodes, s.StageCode)
		localStageID, err := f.cacheRepo.SaveTemplateStage(SaveTemplateStageInput{
			RemoteID:         s.ID,
			TemplateID:       localTplID,
			StageCode:        s.StageCode,
			StageName:        s.StageName,
			StageType:        s.StageType,
			SortOrder:        s.SortOrder,
			Description:      s.Description,
			DefaultRoleCodes: s.DefaultRoleCodes,
		})
		if err != nil {
			return 0, fmt.Errorf("保存 stage %s 失败: %w", s.StageCode, err)
		}
		// 文件任务层（五层模版中间层）：4 层模版此处为空。先存 task 拿到本地 id，供 file_rule 回填。
		taskLocalIDByCode := map[string]int64{}
		for _, tk := range s.Tasks {
			ltid, err := f.cacheRepo.SaveTemplateTask(SaveTemplateTaskInput{
				RemoteID:        tk.ID,
				TemplateStageID: localStageID,
				TaskCode:        tk.TaskCode,
				TaskName:        tk.TaskName,
				SortOrder:       tk.SortOrder,
			})
			if err != nil {
				return 0, fmt.Errorf("保存 task %s 失败: %w", tk.TaskCode, err)
			}
			taskLocalIDByCode[tk.TaskCode] = ltid
		}
		// 清理该环节下远端已删除的文件任务（镜像远端）。
		keepTaskCodes := make([]string, 0, len(s.Tasks))
		for _, tk := range s.Tasks {
			keepTaskCodes = append(keepTaskCodes, tk.TaskCode)
		}
		if err := f.cacheRepo.PruneStageTasks(localStageID, keepTaskCodes); err != nil {
			return 0, fmt.Errorf("清理 stage %s 的过期文件任务失败: %w", s.StageCode, err)
		}
		for _, r := range s.FileRules {
			var taskID *int64
			if r.TaskCode != nil && *r.TaskCode != "" {
				if id, ok := taskLocalIDByCode[*r.TaskCode]; ok {
					idCopy := id
					taskID = &idCopy
				}
			}
			if _, err := f.cacheRepo.SaveTemplateFileRule(SaveTemplateFileRuleInput{
				RemoteID:               r.ID,
				TemplateStageID:        localStageID,
				TemplateTaskID:         taskID,
				FileRuleCode:           r.FileRuleCode,
				FileName:               r.FileName,
				DataState:              r.DataState,
				Required:               r.Required,
				AllowedFileTypes:       r.AllowedFileTypes,
				NamingPattern:          r.NamingPattern,
				SummaryPattern:         r.SummaryPattern,
				DefaultRetentionPolicy: r.DefaultRetentionPolicy,
				SortOrder:              r.SortOrder,
			}); err != nil {
				return 0, fmt.Errorf("保存 file_rule %s 失败: %w", r.FileRuleCode, err)
			}
		}
		// 清理该环节下远端已删除的文件规则（镜像远端）。
		keepRuleCodes := make([]string, 0, len(s.FileRules))
		for _, r := range s.FileRules {
			keepRuleCodes = append(keepRuleCodes, r.FileRuleCode)
		}
		if err := f.cacheRepo.PruneStageFileRules(localStageID, keepRuleCodes); err != nil {
			return 0, fmt.Errorf("清理 stage %s 的过期文件规则失败: %w", s.StageCode, err)
		}
	}

	// 清理该模版下远端已删除的工作环节（连同其 task/file_rule 级联删除）。
	// 这是修复「manage 把模版环节改少后，scan 仍显示旧环节」的关键：旧的孤儿环节会残留在本地缓存。
	if err := f.cacheRepo.PruneTemplateStages(localTplID, keepStageCodes); err != nil {
		return 0, fmt.Errorf("清理模版 %s 的过期环节失败: %w", tpl.TemplateCode, err)
	}

	return localTplID, nil
}
