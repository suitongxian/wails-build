package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// TemplateAuthoringRepository 数据业务模版「本地创作」侧仓储。
//
// 2026-05-31 模版创作权从 manage 迁到 scan：scan 本地按五层
// （行业分类 ▸ 数据项目模版 ▸ 工作环节 ▸ 文件任务 ▸ 文档标识）创作模版，
// 后续阶段二再反向同步到 manage。本仓储与只读镜像/同步的
// TemplateCacheRepository 分开，专管本地写。
//
// 编码全自动：用户只填名称/描述等业务字段，各级 code 由后端按前缀递增生成，
// 契合「模版=目录树」心智（用户关心名字，编码是系统的事）。
type TemplateAuthoringRepository struct {
	DB *sqlx.DB
}

func NewTemplateAuthoringRepository(db *sqlx.DB) *TemplateAuthoringRepository {
	return &TemplateAuthoringRepository{DB: db}
}

// nextCodeWithPrefix 在指定表的 code 列上，对形如 {prefix}{NNN} 的编码求下一个序号（全表范围）。
func (r *TemplateAuthoringRepository) nextCodeWithPrefix(table, column, prefix string) (string, error) {
	return r.nextCodeScoped(table, column, prefix, "", nil)
}

// nextCodeScoped 同 nextCodeWithPrefix，但可限定在某父级范围内求序号（scopeCol=scopeVal）。
// 返回零填充 3 位的新编码（如 IND-001 / STG-001 / IN-001）。包含已软删除的行，避免编码复用。
// 数据量小（模版骨架），在 Go 侧解析数字后缀求最大值，简单可靠优于在 SQL 里做字符串截取。
func (r *TemplateAuthoringRepository) nextCodeScoped(table, column, prefix, scopeCol string, scopeVal interface{}) (string, error) {
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s LIKE ?", column, table, column) //nolint:gosec // table/column 为内部常量
	args := []interface{}{prefix + "%"}
	if scopeCol != "" {
		q += fmt.Sprintf(" AND %s = ?", scopeCol) //nolint:gosec // scopeCol 为内部常量
		args = append(args, scopeVal)
	}
	var codes []string
	if err := r.DB.Select(&codes, q, args...); err != nil {
		return "", err
	}
	max := 0
	for _, c := range codes {
		var n int
		if _, err := fmt.Sscanf(c, prefix+"%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("%s%03d", prefix, max+1), nil
}

// =============================================
// 行业分类 business_classes（本地创作）
// =============================================

// ListBusinessClasses 列出所有未删除的行业分类。
func (r *TemplateAuthoringRepository) ListBusinessClasses() ([]models.BusinessClass, error) {
	var list []models.BusinessClass
	err := r.DB.Select(&list, `SELECT * FROM business_classes WHERE disable = 0 ORDER BY id`)
	return list, err
}

// GetBusinessClass 按 id 读取（含已删除，便于排查）。
func (r *TemplateAuthoringRepository) GetBusinessClass(id int64) (*models.BusinessClass, error) {
	var bc models.BusinessClass
	if err := r.DB.Get(&bc, `SELECT * FROM business_classes WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return &bc, nil
}

// CreateBusinessClass 新建行业分类，code 自动生成（IND-NNN），type 固定 industry。
func (r *TemplateAuthoringRepository) CreateBusinessClass(name, description string) (*models.BusinessClass, error) {
	if name == "" {
		return nil, fmt.Errorf("行业分类名称不能为空")
	}
	code, err := r.nextCodeWithPrefix("business_classes", "code", "IND-")
	if err != nil {
		return nil, fmt.Errorf("生成行业编码失败: %w", err)
	}
	now := time.Now()
	var desc *string
	if description != "" {
		desc = &description
	}
	res, err := r.DB.Exec(`INSERT INTO business_classes (code, name, type, description, cached_at, create_time, update_time, disable)
		VALUES (?, ?, 'industry', ?, ?, ?, ?, 0)`,
		code, name, desc, now, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.GetBusinessClass(id)
}

// UpdateBusinessClass 更新名称与描述（编码不可改）。
func (r *TemplateAuthoringRepository) UpdateBusinessClass(id int64, name, description string) error {
	if name == "" {
		return fmt.Errorf("行业分类名称不能为空")
	}
	var desc *string
	if description != "" {
		desc = &description
	}
	res, err := r.DB.Exec(`UPDATE business_classes SET name = ?, description = ?, update_time = ? WHERE id = ? AND disable = 0`,
		name, desc, time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("行业分类不存在或已删除: id=%d", id)
	}
	return nil
}

// DeleteBusinessClass 软删除（disable=1）。
func (r *TemplateAuthoringRepository) DeleteBusinessClass(id int64) error {
	res, err := r.DB.Exec(`UPDATE business_classes SET disable = 1, update_time = ? WHERE id = ? AND disable = 0`,
		time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("行业分类不存在或已删除: id=%d", id)
	}
	return nil
}

// =============================================
// 数据项目模版 data_templates（本地创作，五层树的根）
// =============================================

// 合法取值（与系统其它处对齐：见 security_policy_seed / centralized_projects）
var validSensitivity = map[string]bool{"core": true, "important": true, "general": true}
var validScope = map[string]bool{"industry": true, "unit": true, "department": true, "person": true}

// CreateTemplateInput 项目模版创作入参（对应原型「数据项目模版」表单项）。
type CreateTemplateInput struct {
	ClassCode        string // 行业分类 code
	Scope            string // 模版归类 industry/unit/department/person
	TemplateName     string // 项目名称
	ShortCode        string // 代号/简称
	Manager          string // 负责人
	Description      string // 简介
	ApprovalBasis    string // 立项依据
	SensitivityLevel string // 敏感级别 core/important/general
	Owner            string // 数据所有权归属
}

func (in *CreateTemplateInput) validate() error {
	if in.TemplateName == "" {
		return fmt.Errorf("项目名称不能为空")
	}
	if in.SensitivityLevel != "" && !validSensitivity[in.SensitivityLevel] {
		return fmt.Errorf("非法敏感级别 %q（应为 core/important/general）", in.SensitivityLevel)
	}
	if in.Scope != "" && !validScope[in.Scope] {
		return fmt.Errorf("非法模版归类 %q（应为 industry/unit/department/person）", in.Scope)
	}
	return nil
}

// normalizeLocalScope 本地模版归类规整：留空默认「单位」；显式行业(industry)一律拒绝
// （行业模版是中心下发的通用模版，用户另存为/新建只能存为 单位/部门/个人）。
func normalizeLocalScope(scope string) (string, error) {
	if scope == "" {
		return "unit", nil
	}
	if scope == "industry" {
		return "", fmt.Errorf("本地模版不能保存为行业模版，请选择 单位/部门/个人")
	}
	if !validScope[scope] {
		return "", fmt.Errorf("非法模版归类 %q（应为 unit/department/person）", scope)
	}
	return scope, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GetLocalTemplate 按 id 读取本地模版。
func (r *TemplateAuthoringRepository) GetLocalTemplate(id int64) (*models.DataTemplate, error) {
	var t models.DataTemplate
	if err := r.DB.Get(&t, `SELECT * FROM data_templates WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListLocalTemplates 列出本地创作的模版（origin=local），可选按行业/归类过滤。
func (r *TemplateAuthoringRepository) ListLocalTemplates(classCode, scope string) ([]models.DataTemplate, error) {
	q := `SELECT * FROM data_templates WHERE disable = 0 AND origin = 'local'`
	args := []interface{}{}
	if classCode != "" {
		q += ` AND class_code = ?`
		args = append(args, classCode)
	}
	if scope != "" {
		q += ` AND scope = ?`
		args = append(args, scope)
	}
	q += ` ORDER BY id DESC`
	var list []models.DataTemplate
	err := r.DB.Select(&list, q, args...)
	return list, err
}

// CreateLocalTemplate 新建本地项目模版：origin=local，template_code 自动生成，
// 版本默认 V1.0，状态默认 draft。
func (r *TemplateAuthoringRepository) CreateLocalTemplate(in CreateTemplateInput) (*models.DataTemplate, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	scope, err := normalizeLocalScope(in.Scope)
	if err != nil {
		return nil, err
	}
	sens := in.SensitivityLevel
	if sens == "" {
		sens = "general"
	}
	code, err := r.nextCodeWithPrefix("data_templates", "template_code", "TPL-LOCAL-")
	if err != nil {
		return nil, fmt.Errorf("生成模版编码失败: %w", err)
	}
	now := time.Now()
	res, err := r.DB.Exec(`INSERT INTO data_templates
		(template_code, template_name, template_version, class_code, status, project_sensitivity_level,
		 origin, scope, short_code, manager, owner, approval_basis, description,
		 cached_at, create_time, update_time, disable)
		VALUES (?, ?, 'V1.0', ?, 'draft', ?, 'local', ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		code, in.TemplateName, strPtr(in.ClassCode), sens,
		scope, strPtr(in.ShortCode), strPtr(in.Manager), strPtr(in.Owner), strPtr(in.ApprovalBasis), strPtr(in.Description),
		now, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.GetLocalTemplate(id)
}

// UpdateLocalTemplate 更新本地模版基本信息（编码/版本/来源不可改）。
func (r *TemplateAuthoringRepository) UpdateLocalTemplate(id int64, in CreateTemplateInput) error {
	if err := in.validate(); err != nil {
		return err
	}
	scope, err := normalizeLocalScope(in.Scope)
	if err != nil {
		return err
	}
	sens := in.SensitivityLevel
	if sens == "" {
		sens = "general"
	}
	res, err := r.DB.Exec(`UPDATE data_templates SET
			template_name = ?, class_code = ?, project_sensitivity_level = ?,
			scope = ?, short_code = ?, manager = ?, owner = ?, approval_basis = ?, description = ?,
			update_time = ?
		WHERE id = ? AND disable = 0 AND origin = 'local'`,
		in.TemplateName, strPtr(in.ClassCode), sens,
		scope, strPtr(in.ShortCode), strPtr(in.Manager), strPtr(in.Owner), strPtr(in.ApprovalBasis), strPtr(in.Description),
		time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("本地模版不存在或已删除: id=%d", id)
	}
	return nil
}

// SetTemplatePublished 设置本地模版的「是否发布」状态（仅 origin=local）。
// 发布后该模版才能用于立项；可随时取消发布。
func (r *TemplateAuthoringRepository) SetTemplatePublished(id int64, published bool) error {
	v := 0
	if published {
		v = 1
	}
	res, err := r.DB.Exec(`UPDATE data_templates SET is_published = ?, update_time = ? WHERE id = ? AND disable = 0 AND origin = 'local'`,
		v, time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("本地模版不存在或已删除: id=%d", id)
	}
	return nil
}

// DeleteLocalTemplate 软删除本地模版（disable=1）。
func (r *TemplateAuthoringRepository) DeleteLocalTemplate(id int64) error {
	res, err := r.DB.Exec(`UPDATE data_templates SET disable = 1, update_time = ? WHERE id = ? AND disable = 0 AND origin = 'local'`,
		time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("本地模版不存在或已删除: id=%d", id)
	}
	return nil
}

// =============================================
// 反向同步：把本地五层模版推送到 manage（阶段二）
// =============================================

// ingest 负载结构（字段名须与 manage /api/templates/ingest 的 readBody 对齐）
type ingestFileRule struct {
	FileRuleCode     string  `json:"file_rule_code"`
	FileName         string  `json:"file_name"`
	DataState        string  `json:"data_state"`
	Required         bool    `json:"required"`
	AllowedFileTypes string  `json:"allowed_file_types"`
	NamingPattern    *string `json:"naming_pattern"`
	SummaryPattern   *string `json:"summary_pattern"`
	SensitivityLevel *string `json:"sensitivity_level"`
	Drafter          *string `json:"drafter"`
	// L6 文档标识管控类字段
	Category             *string `json:"category"`
	SecurityRequirement  *string `json:"security_requirement"`
	DiffusionRequirement *string `json:"diffusion_requirement"`
	ArchiveRequirement   *string `json:"archive_requirement"`
	RetentionPeriodDays  *int    `json:"retention_period_days"`
	DestructionRule      *string `json:"destruction_rule"`
	SortOrder            int     `json:"sort_order"`
}
type ingestTask struct {
	TaskCode         string           `json:"task_code"`
	TaskName         string           `json:"task_name"`
	Manager          *string          `json:"manager"`
	SensitivityLevel *string          `json:"sensitivity_level"`
	SortOrder        int              `json:"sort_order"`
	Description      *string          `json:"description"`
	FileRules        []ingestFileRule `json:"file_rules"`
}
type ingestStage struct {
	StageCode        string       `json:"stage_code"`
	StageName        string       `json:"stage_name"`
	StageType        string       `json:"stage_type"`
	SortOrder        int          `json:"sort_order"`
	Description      *string      `json:"description"`
	Manager          *string      `json:"manager"`
	Members          *string      `json:"members"`
	ManagerUsername  *string      `json:"manager_username"`
	MembersUsernames *string      `json:"members_usernames"`
	Tasks            []ingestTask `json:"tasks"`
}
type ingestTemplate struct {
	TemplateCode            string  `json:"template_code"`
	TemplateName            string  `json:"template_name"`
	TemplateVersion         string  `json:"template_version"`
	Scope                   string  `json:"scope"`
	ProjectSensitivityLevel string  `json:"project_sensitivity_level"`
	Description             *string `json:"description"`
	IsProject               int     `json:"is_project"`             // 0=模版同步 / 1=立项（生成项目）
	InitiatedBy             string  `json:"initiated_by,omitempty"` // 立项人 username（立项时带）
}
type ingestBusinessClass struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}
type IngestPayload struct {
	Template      ingestTemplate       `json:"template"`
	BusinessClass *ingestBusinessClass `json:"business_class"`
	Stages        []ingestStage        `json:"stages"`
}

// BuildIngestPayload 把本地模版的五层树组装成推送给 manage 的负载（纯函数，便于测试）。
func (r *TemplateAuthoringRepository) BuildIngestPayload(templateID int64) (*IngestPayload, error) {
	tree, err := r.GetLocalTemplateTree(templateID)
	if err != nil {
		return nil, err
	}
	p := &IngestPayload{
		Template: ingestTemplate{
			TemplateCode:            tree.Template.TemplateCode,
			TemplateName:            tree.Template.TemplateName,
			TemplateVersion:         tree.Template.TemplateVersion,
			Scope:                   tree.Template.Scope,
			ProjectSensitivityLevel: tree.Template.ProjectSensitivityLevel,
			Description:             tree.Template.Description,
		},
		Stages: make([]ingestStage, 0, len(tree.Stages)),
	}
	// 行业分类：始终带上 code，让 manage 端按 code 命中/创建（分类归口 manage，scan 本地可能无该行）。
	// 若 scan 本地恰好缓存了同 code 的分类，则补全 name/description；否则用 code 占位（manage 命中后忽略 name）。
	if tree.Template.ClassCode != nil && *tree.Template.ClassCode != "" {
		code := *tree.Template.ClassCode
		out := &ingestBusinessClass{Code: code, Name: code}
		var bc models.BusinessClass
		if err := r.DB.Get(&bc, `SELECT * FROM business_classes WHERE code = ? AND disable = 0`, code); err == nil {
			out.Name = bc.Name
			out.Description = bc.Description
		}
		p.BusinessClass = out
	}
	for _, st := range tree.Stages {
		es := ingestStage{
			StageCode: st.StageCode, StageName: st.StageName, StageType: st.StageType,
			SortOrder: st.SortOrder, Description: st.Description, Manager: st.Manager, Members: st.Members,
			ManagerUsername: st.ManagerUsername, MembersUsernames: st.MembersUsernames,
			Tasks: make([]ingestTask, 0, len(st.Tasks)),
		}
		for _, tk := range st.Tasks {
			et := ingestTask{
				TaskCode: tk.TaskCode, TaskName: tk.TaskName, Manager: tk.Manager,
				SensitivityLevel: tk.SensitivityLevel, SortOrder: tk.SortOrder, Description: tk.Description,
				FileRules: make([]ingestFileRule, 0, len(tk.FileRules)),
			}
			for _, fr := range tk.FileRules {
				et.FileRules = append(et.FileRules, ingestFileRule{
					FileRuleCode: fr.FileRuleCode, FileName: fr.FileName, DataState: fr.DataState,
					Required: fr.Required == 1, AllowedFileTypes: fr.AllowedFileTypes,
					NamingPattern: fr.NamingPattern, SummaryPattern: fr.SummaryPattern,
					SensitivityLevel: fr.SensitivityLevel, Drafter: fr.Drafter,
					Category: fr.Category, SecurityRequirement: fr.SecurityRequirement,
					DiffusionRequirement: fr.DiffusionRequirement, ArchiveRequirement: fr.ArchiveRequirement,
					RetentionPeriodDays: fr.RetentionPeriodDays, DestructionRule: fr.DestructionRule,
					SortOrder: fr.SortOrder,
				})
			}
			es.Tasks = append(es.Tasks, et)
		}
		p.Stages = append(p.Stages, es)
	}
	return p, nil
}

// MarkTemplatePushed 推送成功后回填 remote_id 与同步状态。
func (r *TemplateAuthoringRepository) MarkTemplatePushed(templateID, remoteID int64) error {
	_, err := r.DB.Exec(`UPDATE data_templates SET remote_id = ?, sync_status = 'synced', sync_message = NULL, synced_at = ?, update_time = ? WHERE id = ?`,
		remoteID, time.Now(), time.Now(), templateID)
	return err
}

// MarkTemplateSyncError 推送失败时记录错误。
func (r *TemplateAuthoringRepository) MarkTemplateSyncError(templateID int64, msg string) error {
	_, err := r.DB.Exec(`UPDATE data_templates SET sync_status = 'error', sync_message = ?, update_time = ? WHERE id = ?`,
		msg, time.Now(), templateID)
	return err
}

// manage /api/templates/ingest 的响应结构
type manageIngestResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *struct {
		TemplateID int64 `json:"template_id"`
	} `json:"data"`
}

// PushTemplateToManage 组装负载并 POST 到 manage 的 ingest 端点，成功后回填 remote_id。
// endpoint 为 manage 基址（如 http://127.0.0.1:3002）；client 可注入便于测试。
func (r *TemplateAuthoringRepository) PushTemplateToManage(client *http.Client, endpoint string, templateID int64, isProject bool, initiatedBy string) (int64, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return 0, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	// 立项闸门：只有「已发布」的本地模版才能立项（在联网前拦截）。
	if isProject {
		var pub int
		if err := r.DB.Get(&pub, `SELECT is_published FROM data_templates WHERE id = ? AND disable = 0`, templateID); err != nil {
			return 0, fmt.Errorf("模版不存在: %w", err)
		}
		if pub != 1 {
			return 0, fmt.Errorf("模版未发布，不能立项；请先在编辑器点「发布」")
		}
	}
	payload, err := r.BuildIngestPayload(templateID)
	if err != nil {
		return 0, err
	}
	// 立项 ≠ 模版同步：立项以模版为蓝本生成一个【全新项目实例】——
	//   标记 is_project=1，把 template_code 换成新生成的项目编码（不复用模版编码），并记录立项人。
	//   这样模版本身不动、每次立项都是独立项目（独立进度），再次立项不会更新同一个项目。
	if isProject {
		payload.Template.IsProject = 1
		payload.Template.TemplateCode = fmt.Sprintf("%s-P%d", payload.Template.TemplateCode, time.Now().UnixNano())
		payload.Template.InitiatedBy = initiatedBy
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Post(endpoint+"/api/templates/ingest", "application/json", bytes.NewReader(buf))
	if err != nil {
		_ = r.MarkTemplateSyncError(templateID, err.Error())
		return 0, fmt.Errorf("推送 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("manage 返回非 200: %d", resp.StatusCode)
		_ = r.MarkTemplateSyncError(templateID, msg)
		return 0, fmt.Errorf("%s, body=%s", msg, string(body))
	}
	var raw manageIngestResp
	if err := json.Unmarshal(body, &raw); err != nil {
		_ = r.MarkTemplateSyncError(templateID, "解析响应失败")
		return 0, fmt.Errorf("解析 manage 响应失败: %w", err)
	}
	if raw.Code != 0 || raw.Data == nil {
		_ = r.MarkTemplateSyncError(templateID, raw.Message)
		return 0, fmt.Errorf("manage 接收失败 code=%d msg=%s", raw.Code, raw.Message)
	}
	if err := r.MarkTemplatePushed(templateID, raw.Data.TemplateID); err != nil {
		return 0, err
	}
	return raw.Data.TemplateID, nil
}

// GetLocalTemplateTree 读取本地模版的完整五层树：项目 ▸ 事项 ▸ 任务 ▸ 标识。
// 供前端方案 A 树编辑器一次性渲染。树骨架小，N+1 查询可接受。
func (r *TemplateAuthoringRepository) GetLocalTemplateTree(templateID int64) (*models.LocalTemplateTree, error) {
	tpl, err := r.GetLocalTemplate(templateID)
	if err != nil {
		return nil, err
	}
	stages, err := r.ListStages(templateID)
	if err != nil {
		return nil, err
	}
	tree := &models.LocalTemplateTree{Template: *tpl, Stages: make([]models.LocalTemplateStageNode, 0, len(stages))}
	for _, st := range stages {
		tasks, err := r.ListTasks(st.ID)
		if err != nil {
			return nil, err
		}
		stageNode := models.LocalTemplateStageNode{TemplateStage: st, Tasks: make([]models.LocalTemplateTaskNode, 0, len(tasks))}
		for _, tk := range tasks {
			rules, err := r.ListFileRules(tk.ID)
			if err != nil {
				return nil, err
			}
			if rules == nil {
				rules = []models.TemplateFileRule{}
			}
			stageNode.Tasks = append(stageNode.Tasks, models.LocalTemplateTaskNode{TemplateTask: tk, FileRules: rules})
		}
		tree.Stages = append(tree.Stages, stageNode)
	}
	return tree, nil
}

// =============================================
// 工作环节(事项) template_stages（本地创作）
// =============================================

// StageInput 工作事项创作入参（原型「工作事项」表单项）。
type StageInput struct {
	Name             string // 事项名称
	Manager          string // 责任人（显示名）
	ManagerUsername  string // 责任人 username（防重名/过滤）
	Members          string // 参与人（显示名，逗号分隔）
	MembersUsernames string // 参与人 username（逗号分隔，与 Members 对应）
	Desc             string // 内容描述
}

// ListStages 列出某模版下的工作事项（按排序）。
func (r *TemplateAuthoringRepository) ListStages(templateID int64) ([]models.TemplateStage, error) {
	var list []models.TemplateStage
	err := r.DB.Select(&list, `SELECT * FROM template_stages WHERE template_id = ? AND disable = 0 ORDER BY sort_order, id`, templateID)
	return list, err
}

// CreateStage 新建工作事项：stage_code 按模版作用域自动生成 STG-NNN，sort_order 追加在末尾。
func (r *TemplateAuthoringRepository) CreateStage(templateID int64, in StageInput) (*models.TemplateStage, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("事项名称不能为空")
	}
	code, err := r.nextCodeScoped("template_stages", "stage_code", "STG-", "template_id", templateID)
	if err != nil {
		return nil, fmt.Errorf("生成事项编码失败: %w", err)
	}
	var nextSort int
	_ = r.DB.Get(&nextSort, `SELECT COALESCE(MAX(sort_order),0)+1 FROM template_stages WHERE template_id = ? AND disable = 0`, templateID)
	now := time.Now()
	res, err := r.DB.Exec(`INSERT INTO template_stages
		(template_id, stage_code, stage_name, stage_type, sort_order, description, manager, members, manager_username, members_usernames, cached_at, create_time, update_time, disable)
		VALUES (?, ?, ?, 'process', ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		templateID, code, in.Name, nextSort, strPtr(in.Desc), strPtr(in.Manager), strPtr(in.Members), strPtr(in.ManagerUsername), strPtr(in.MembersUsernames), now, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	var st models.TemplateStage
	if err := r.DB.Get(&st, `SELECT * FROM template_stages WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return &st, nil
}

// UpdateStage 更新事项名称/责任人/参与人/描述（编码不可改）。
func (r *TemplateAuthoringRepository) UpdateStage(id int64, in StageInput) error {
	if in.Name == "" {
		return fmt.Errorf("事项名称不能为空")
	}
	res, err := r.DB.Exec(`UPDATE template_stages SET stage_name = ?, manager = ?, members = ?, manager_username = ?, members_usernames = ?, description = ?, update_time = ?
		WHERE id = ? AND disable = 0`,
		in.Name, strPtr(in.Manager), strPtr(in.Members), strPtr(in.ManagerUsername), strPtr(in.MembersUsernames), strPtr(in.Desc), time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("事项不存在或已删除: id=%d", id)
	}
	return nil
}

// DeleteStage 软删除事项，并级联软删其下所有任务与标识（避免孤儿）。
func (r *TemplateAuthoringRepository) DeleteStage(id int64) error {
	now := time.Now()
	tx, err := r.DB.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.Exec(`UPDATE template_stages SET disable = 1, update_time = ? WHERE id = ? AND disable = 0`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("事项不存在或已删除: id=%d", id)
	}
	// 级联：标识（挂在该事项的任务上，或直接挂该 stage 的）→ 任务
	if _, err := tx.Exec(`UPDATE template_file_rules SET disable = 1, update_time = ?
		WHERE disable = 0 AND (template_stage_id = ? OR template_task_id IN (SELECT id FROM template_tasks WHERE template_stage_id = ?))`,
		now, id, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE template_tasks SET disable = 1, update_time = ? WHERE template_stage_id = ? AND disable = 0`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

// =============================================
// 文件任务 template_tasks（本地创作）
// =============================================

// TaskInput 文件任务创作入参（原型「文件任务」表单项）。
type TaskInput struct {
	Name             string // 任务名称
	Manager          string // 承办人
	SensitivityLevel string // 敏感级别 core/important/general
	Desc             string // 任务说明
}

// ListTasks 列出某事项下的文件任务。
func (r *TemplateAuthoringRepository) ListTasks(stageID int64) ([]models.TemplateTask, error) {
	var list []models.TemplateTask
	err := r.DB.Select(&list, `SELECT * FROM template_tasks WHERE template_stage_id = ? AND disable = 0 ORDER BY sort_order, id`, stageID)
	return list, err
}

// CreateTask 新建文件任务：task_code 按事项作用域自动生成 TK-NNN。
func (r *TemplateAuthoringRepository) CreateTask(stageID int64, in TaskInput) (*models.TemplateTask, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("任务名称不能为空")
	}
	if in.SensitivityLevel != "" && !validSensitivity[in.SensitivityLevel] {
		return nil, fmt.Errorf("非法敏感级别 %q（应为 core/important/general）", in.SensitivityLevel)
	}
	code, err := r.nextCodeScoped("template_tasks", "task_code", "TK-", "template_stage_id", stageID)
	if err != nil {
		return nil, fmt.Errorf("生成任务编码失败: %w", err)
	}
	var nextSort int
	_ = r.DB.Get(&nextSort, `SELECT COALESCE(MAX(sort_order),0)+1 FROM template_tasks WHERE template_stage_id = ? AND disable = 0`, stageID)
	now := time.Now()
	res, err := r.DB.Exec(`INSERT INTO template_tasks
		(template_stage_id, task_code, task_name, manager, sensitivity_level, sort_order, description, cached_at, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		stageID, code, in.Name, strPtr(in.Manager), strPtr(in.SensitivityLevel), nextSort, strPtr(in.Desc), now, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	var tk models.TemplateTask
	if err := r.DB.Get(&tk, `SELECT * FROM template_tasks WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return &tk, nil
}

// UpdateTask 更新任务名称/承办人/敏感级别/说明。
func (r *TemplateAuthoringRepository) UpdateTask(id int64, in TaskInput) error {
	if in.Name == "" {
		return fmt.Errorf("任务名称不能为空")
	}
	if in.SensitivityLevel != "" && !validSensitivity[in.SensitivityLevel] {
		return fmt.Errorf("非法敏感级别 %q（应为 core/important/general）", in.SensitivityLevel)
	}
	res, err := r.DB.Exec(`UPDATE template_tasks SET task_name = ?, manager = ?, sensitivity_level = ?, description = ?, update_time = ?
		WHERE id = ? AND disable = 0`,
		in.Name, strPtr(in.Manager), strPtr(in.SensitivityLevel), strPtr(in.Desc), time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("任务不存在或已删除: id=%d", id)
	}
	return nil
}

// DeleteTask 软删除任务，并级联软删其下标识。
func (r *TemplateAuthoringRepository) DeleteTask(id int64) error {
	now := time.Now()
	tx, err := r.DB.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.Exec(`UPDATE template_tasks SET disable = 1, update_time = ? WHERE id = ? AND disable = 0`, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("任务不存在或已删除: id=%d", id)
	}
	if _, err := tx.Exec(`UPDATE template_file_rules SET disable = 1, update_time = ? WHERE template_task_id = ? AND disable = 0`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

// =============================================
// 文档标识 template_file_rules（本地创作，叶子层）
// =============================================

var dataStatePrefix = map[string]string{"input": "IN-", "process": "PRC-", "output": "OUT-"}

// FileRuleInput 文档标识创作入参（原型「文档标识」表单项）。
type FileRuleInput struct {
	FileName         string // 文档实际名称
	DataState        string // 数据态 input/process/output
	Required         bool   // 是否必需
	AllowedFileTypes string // 允许文件类型（逗号分隔）
	NamingPattern    string // 命名模式（规范名称）
	SummaryPattern   string // 内容摘要模板
	Drafter          string // 起草人
	SensitivityLevel string // 敏感级别 core/important/general
	RetentionPolicy  string // 归档策略
	// L6 文档标识管控类字段
	Category             string // 文档类别：未识别/个人/工作/非责任
	SecurityRequirement  string // 安全要求：明文存储/加密存储
	DiffusionRequirement string // 防扩散：孤本模式/双孤本模式
	ArchiveRequirement   string // 归档要求：个人文件夹/部门文件柜/单位文件室
	RetentionPeriodDays  *int   // 保留期天数（-1=永久；nil=未设）
	DestructionRule      string // 销毁规则
}

// intPtrVal：*int → SQL 值（nil → NULL）。
func intPtrVal(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// firstAllowedType 每个文档标识只对应一个文件类型：从逗号串/JSON 数组里取第一个（去引号/空白），保留原书写。
func firstAllowedType(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.Trim(s, "[]"))
	for _, p := range strings.Split(s, ",") {
		t := strings.TrimSpace(strings.Trim(strings.TrimSpace(p), `"`))
		if t != "" {
			return t
		}
	}
	return ""
}

func (in *FileRuleInput) validate() error {
	if in.FileName == "" {
		return fmt.Errorf("文档实际名称不能为空")
	}
	if dataStatePrefix[in.DataState] == "" {
		return fmt.Errorf("非法数据态 %q（应为 input/process/output）", in.DataState)
	}
	if in.SensitivityLevel != "" && !validSensitivity[in.SensitivityLevel] {
		return fmt.Errorf("非法敏感级别 %q（应为 core/important/general）", in.SensitivityLevel)
	}
	return nil
}

// ListFileRules 列出某任务下的文档标识。
func (r *TemplateAuthoringRepository) ListFileRules(taskID int64) ([]models.TemplateFileRule, error) {
	var list []models.TemplateFileRule
	err := r.DB.Select(&list, `SELECT * FROM template_file_rules WHERE template_task_id = ? AND disable = 0 ORDER BY sort_order, id`, taskID)
	return list, err
}

// CreateFileRule 新建文档标识：file_rule_code 按数据态前缀(IN/PRC/OUT)、在所属事项作用域内自动递增。
// 同时回填 template_stage_id（任务所属事项），以沿用既有 UNIQUE(template_stage_id, file_rule_code) 约束。
func (r *TemplateAuthoringRepository) CreateFileRule(taskID int64, in FileRuleInput) (*models.TemplateFileRule, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	var stageID int64
	if err := r.DB.Get(&stageID, `SELECT template_stage_id FROM template_tasks WHERE id = ? AND disable = 0`, taskID); err != nil {
		return nil, fmt.Errorf("任务不存在: id=%d: %w", taskID, err)
	}
	prefix := dataStatePrefix[in.DataState]
	// 编码在「事项」作用域内按前缀递增（与既有 UNIQUE(template_stage_id, file_rule_code) 一致）
	code, err := r.nextCodeScoped("template_file_rules", "file_rule_code", prefix, "template_stage_id", stageID)
	if err != nil {
		return nil, fmt.Errorf("生成标识编码失败: %w", err)
	}
	var nextSort int
	_ = r.DB.Get(&nextSort, `SELECT COALESCE(MAX(sort_order),0)+1 FROM template_file_rules WHERE template_task_id = ? AND disable = 0`, taskID)
	required := 0
	if in.Required {
		required = 1
	}
	now := time.Now()
	res, err := r.DB.Exec(`INSERT INTO template_file_rules
		(template_stage_id, template_task_id, file_rule_code, file_name, data_state, required, allowed_file_types,
		 naming_pattern, summary_pattern, default_retention_policy, sensitivity_level, drafter,
		 category, security_requirement, diffusion_requirement, archive_requirement, retention_period_days, destruction_rule, sort_order,
		 cached_at, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		stageID, taskID, code, in.FileName, in.DataState, required, firstAllowedType(in.AllowedFileTypes),
		strPtr(in.NamingPattern), strPtr(in.SummaryPattern), strPtr(in.RetentionPolicy), strPtr(in.SensitivityLevel), strPtr(in.Drafter),
		strPtr(in.Category), strPtr(in.SecurityRequirement), strPtr(in.DiffusionRequirement), strPtr(in.ArchiveRequirement), intPtrVal(in.RetentionPeriodDays), strPtr(in.DestructionRule), nextSort,
		now, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	var fr models.TemplateFileRule
	if err := r.DB.Get(&fr, `SELECT * FROM template_file_rules WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return &fr, nil
}

// UpdateFileRule 更新文档标识。data_state 变更不重算编码（编码已分配，避免影响既有路径引用）。
func (r *TemplateAuthoringRepository) UpdateFileRule(id int64, in FileRuleInput) error {
	if err := in.validate(); err != nil {
		return err
	}
	required := 0
	if in.Required {
		required = 1
	}
	res, err := r.DB.Exec(`UPDATE template_file_rules SET
			file_name = ?, data_state = ?, required = ?, allowed_file_types = ?,
			naming_pattern = ?, summary_pattern = ?, default_retention_policy = ?, sensitivity_level = ?, drafter = ?,
			category = ?, security_requirement = ?, diffusion_requirement = ?, archive_requirement = ?, retention_period_days = ?, destruction_rule = ?,
			update_time = ?
		WHERE id = ? AND disable = 0`,
		in.FileName, in.DataState, required, firstAllowedType(in.AllowedFileTypes),
		strPtr(in.NamingPattern), strPtr(in.SummaryPattern), strPtr(in.RetentionPolicy), strPtr(in.SensitivityLevel), strPtr(in.Drafter),
		strPtr(in.Category), strPtr(in.SecurityRequirement), strPtr(in.DiffusionRequirement), strPtr(in.ArchiveRequirement), intPtrVal(in.RetentionPeriodDays), strPtr(in.DestructionRule),
		time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("文档标识不存在或已删除: id=%d", id)
	}
	return nil
}

// DeleteFileRule 软删除文档标识。
func (r *TemplateAuthoringRepository) DeleteFileRule(id int64) error {
	res, err := r.DB.Exec(`UPDATE template_file_rules SET disable = 1, update_time = ? WHERE id = ? AND disable = 0`, time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("文档标识不存在或已删除: id=%d", id)
	}
	return nil
}
