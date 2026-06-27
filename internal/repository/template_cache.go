package repository

import (
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// SystemConfig keys for template / project sync
const (
	KeyManageEndpoint   = "manage_endpoint"
	KeyManageToken      = "manage_token"
	KeyProjectRoot      = "project_root"
	KeyAuthSession      = "auth_session"      // 登录会话(token+user)持久化，关闭终端重开后保持登录
	KeyTemplateEndpoint = "template_endpoint" // manage 上报基址（用于结项归档单向上报，复用同源 host）
	KeyArchiveEndpoint  = "archive_endpoint"  // 单独配置的归档上报基址（覆盖 KeyTemplateEndpoint）
	KeySyncToken        = "sync_token"        // X-Sync-Token，与模版拉取同源
	// KeyTemplateServerEndpoint 模版管理平台地址（template-manage，:19092），用于「同步远程模版」。
	// 与上报数据/文件的 manage 地址（server_endpoint）分离，是独立的第二台服务器。
	KeyTemplateServerEndpoint = "template_server_endpoint"
)

// TemplateCacheRepository 模版镜像数据仓储（写入侧）
//
// 设计：scan 端不与 manage 共享同一份 ID。本地表的 id 是本地自增；远端 ID 落
// 在 remote_id 字段中（用于排查）。本地的关联通过 template_id / template_stage_id
// 维护本地外键关系；查询时统一用本地 id。
type TemplateCacheRepository struct {
	DB *sqlx.DB
}

func NewTemplateCacheRepository(db *sqlx.DB) *TemplateCacheRepository {
	return &TemplateCacheRepository{DB: db}
}

// SaveBusinessClass upsert
func (r *TemplateCacheRepository) SaveBusinessClass(remoteID int64, code, name, btype string, desc *string) error {
	now := time.Now()
	_, err := r.DB.Exec(`INSERT INTO business_classes (remote_id, code, name, type, description, cached_at, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(code) DO UPDATE SET
			remote_id = excluded.remote_id,
			name = excluded.name,
			type = excluded.type,
			description = excluded.description,
			cached_at = excluded.cached_at,
			update_time = excluded.update_time,
			disable = 0`,
		remoteID, code, name, btype, desc, now, now, now)
	return err
}

// SaveTemplateInput 模版主信息镜像入参
type SaveTemplateInput struct {
	RemoteID                int64
	TemplateCode            string
	TemplateName            string
	TemplateVersion         string
	ClassCode               *string
	Scenario                *string
	Publisher               *string
	Status                  string
	ProjectSensitivityLevel string
	UseShareScope           *string
	SharingOpenConditions   *string
	Description             *string
	SourceEndpoint          *string
}

// SaveTemplate upsert（基于 template_code+template_version 唯一）。返回本地 id。
func (r *TemplateCacheRepository) SaveTemplate(in SaveTemplateInput) (int64, error) {
	now := time.Now()
	_, err := r.DB.Exec(`INSERT INTO data_templates (
			remote_id, template_code, template_name, template_version, class_code,
			scenario, publisher, status, project_sensitivity_level,
			use_share_scope, sharing_open_conditions, description, source_endpoint,
			cached_at, create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(template_code, template_version) DO UPDATE SET
			remote_id = excluded.remote_id,
			template_name = excluded.template_name,
			class_code = excluded.class_code,
			scenario = excluded.scenario,
			publisher = excluded.publisher,
			status = excluded.status,
			project_sensitivity_level = excluded.project_sensitivity_level,
			use_share_scope = excluded.use_share_scope,
			sharing_open_conditions = excluded.sharing_open_conditions,
			description = excluded.description,
			source_endpoint = excluded.source_endpoint,
			cached_at = excluded.cached_at,
			update_time = excluded.update_time,
			disable = 0`,
		in.RemoteID, in.TemplateCode, in.TemplateName, in.TemplateVersion, in.ClassCode,
		in.Scenario, in.Publisher, in.Status, in.ProjectSensitivityLevel,
		in.UseShareScope, in.SharingOpenConditions, in.Description, in.SourceEndpoint,
		now, now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.DB.Get(&id, `SELECT id FROM data_templates WHERE template_code = ? AND template_version = ?`,
		in.TemplateCode, in.TemplateVersion)
	return id, err
}

// SaveTemplateStageInput 工作环节镜像入参
type SaveTemplateStageInput struct {
	RemoteID         int64
	TemplateID       int64
	StageCode        string
	StageName        string
	StageType        string
	SortOrder        int
	Description      *string
	DefaultRoleCodes *string
}

// SaveTemplateStage upsert（template_id+stage_code 唯一）
func (r *TemplateCacheRepository) SaveTemplateStage(in SaveTemplateStageInput) (int64, error) {
	now := time.Now()
	_, err := r.DB.Exec(`INSERT INTO template_stages (
			remote_id, template_id, stage_code, stage_name, stage_type, sort_order,
			description, default_role_codes, cached_at, create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(template_id, stage_code) DO UPDATE SET
			remote_id = excluded.remote_id,
			stage_name = excluded.stage_name,
			stage_type = excluded.stage_type,
			sort_order = excluded.sort_order,
			description = excluded.description,
			default_role_codes = excluded.default_role_codes,
			cached_at = excluded.cached_at,
			update_time = excluded.update_time,
			disable = 0`,
		in.RemoteID, in.TemplateID, in.StageCode, in.StageName, in.StageType, in.SortOrder,
		in.Description, in.DefaultRoleCodes, now, now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.DB.Get(&id, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ?`,
		in.TemplateID, in.StageCode)
	return id, err
}

// SaveTemplateTaskInput 文件任务镜像入参（五层模版的中间层）
type SaveTemplateTaskInput struct {
	RemoteID        int64
	TemplateStageID int64
	TaskCode        string
	TaskName        string
	SortOrder       int
}

// SaveTemplateTask upsert（template_stage_id+task_code 唯一）。返回本地 id。
func (r *TemplateCacheRepository) SaveTemplateTask(in SaveTemplateTaskInput) (int64, error) {
	now := time.Now()
	_, err := r.DB.Exec(`INSERT INTO template_tasks (
			remote_id, template_stage_id, task_code, task_name, sort_order,
			cached_at, create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(template_stage_id, task_code) DO UPDATE SET
			remote_id = excluded.remote_id,
			task_name = excluded.task_name,
			sort_order = excluded.sort_order,
			cached_at = excluded.cached_at,
			update_time = excluded.update_time,
			disable = 0`,
		in.RemoteID, in.TemplateStageID, in.TaskCode, in.TaskName, in.SortOrder,
		now, now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.DB.Get(&id, `SELECT id FROM template_tasks WHERE template_stage_id = ? AND task_code = ?`,
		in.TemplateStageID, in.TaskCode)
	return id, err
}

// SaveTemplateFileRuleInput 文件版本规则镜像入参
type SaveTemplateFileRuleInput struct {
	RemoteID                int64
	TemplateStageID         int64
	TemplateTaskID          *int64
	FileRuleCode            string
	FileName                string
	DataState               string
	Required                int
	AllowedFileTypes        string
	NamingPattern           *string
	SummaryPattern          *string
	DefaultRetentionPolicy  *string
	DefaultSecurityPolicyID *int64
	SortOrder               int
}

// SaveTemplateFileRule upsert（template_stage_id+file_rule_code 唯一）
func (r *TemplateCacheRepository) SaveTemplateFileRule(in SaveTemplateFileRuleInput) (int64, error) {
	now := time.Now()
	_, err := r.DB.Exec(`INSERT INTO template_file_rules (
			remote_id, template_stage_id, template_task_id, file_rule_code, file_name, data_state, required,
			allowed_file_types, naming_pattern, summary_pattern, default_retention_policy,
			default_security_policy_id, sort_order, cached_at, create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(template_stage_id, file_rule_code) DO UPDATE SET
			remote_id = excluded.remote_id,
			template_task_id = excluded.template_task_id,
			file_name = excluded.file_name,
			data_state = excluded.data_state,
			required = excluded.required,
			allowed_file_types = excluded.allowed_file_types,
			naming_pattern = excluded.naming_pattern,
			summary_pattern = excluded.summary_pattern,
			default_retention_policy = excluded.default_retention_policy,
			default_security_policy_id = excluded.default_security_policy_id,
			sort_order = excluded.sort_order,
			cached_at = excluded.cached_at,
			update_time = excluded.update_time,
			disable = 0`,
		in.RemoteID, in.TemplateStageID, in.TemplateTaskID, in.FileRuleCode, in.FileName, in.DataState, in.Required,
		in.AllowedFileTypes, in.NamingPattern, in.SummaryPattern, in.DefaultRetentionPolicy,
		in.DefaultSecurityPolicyID, in.SortOrder, now, now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.DB.Get(&id, `SELECT id FROM template_file_rules WHERE template_stage_id = ? AND file_rule_code = ?`,
		in.TemplateStageID, in.FileRuleCode)
	return id, err
}

// =============================================
// 同步清理（prune）：让本地缓存镜像远端，删除远端已不存在的环节/任务/文件规则。
// 仅清理模版缓存表（data_templates/template_stages/template_tasks/template_file_rules），
// 不触碰任何磁盘扫描文件，亦不影响已立项项目（项目实例为独立 template_id 的拷贝）。
// =============================================

// PruneTemplateStages 删除该模版下 stage_code 不在 keep 内的环节，并级联删除其 task/file_rule。
// keep 为空表示该模版没有任何环节 —— 清空其下全部环节。
func (r *TemplateCacheRepository) PruneTemplateStages(tplID int64, keepStageCodes []string) error {
	// 先删将被移除环节的子表（file_rule → task），再删环节本身。
	subStale := `SELECT id FROM template_stages WHERE template_id = ?`
	args := []interface{}{tplID}
	if len(keepStageCodes) > 0 {
		inQ, inArgs, err := sqlx.In(` AND stage_code NOT IN (?)`, keepStageCodes)
		if err != nil {
			return err
		}
		subStale += inQ
		args = append(args, inArgs...)
	}
	if _, err := r.DB.Exec(r.DB.Rebind(`DELETE FROM template_file_rules WHERE template_stage_id IN (`+subStale+`)`), args...); err != nil {
		return err
	}
	if _, err := r.DB.Exec(r.DB.Rebind(`DELETE FROM template_tasks WHERE template_stage_id IN (`+subStale+`)`), args...); err != nil {
		return err
	}
	delStage := `DELETE FROM template_stages WHERE template_id = ?`
	if len(keepStageCodes) > 0 {
		inQ, inArgs, err := sqlx.In(` AND stage_code NOT IN (?)`, keepStageCodes)
		if err != nil {
			return err
		}
		delStage += inQ
		args2 := append([]interface{}{tplID}, inArgs...)
		_, err = r.DB.Exec(r.DB.Rebind(delStage), args2...)
		return err
	}
	_, err := r.DB.Exec(r.DB.Rebind(delStage), tplID)
	return err
}

// PruneStageTasks 删除该环节下 task_code 不在 keep 内的文件任务。keep 为空表示清空。
func (r *TemplateCacheRepository) PruneStageTasks(stageID int64, keepTaskCodes []string) error {
	if len(keepTaskCodes) == 0 {
		_, err := r.DB.Exec(`DELETE FROM template_tasks WHERE template_stage_id = ?`, stageID)
		return err
	}
	q, args, err := sqlx.In(`DELETE FROM template_tasks WHERE template_stage_id = ? AND task_code NOT IN (?)`, stageID, keepTaskCodes)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(r.DB.Rebind(q), args...)
	return err
}

// PruneStageFileRules 删除该环节下 file_rule_code 不在 keep 内的文件规则。keep 为空表示清空。
func (r *TemplateCacheRepository) PruneStageFileRules(stageID int64, keepFileRuleCodes []string) error {
	if len(keepFileRuleCodes) == 0 {
		_, err := r.DB.Exec(`DELETE FROM template_file_rules WHERE template_stage_id = ?`, stageID)
		return err
	}
	q, args, err := sqlx.In(`DELETE FROM template_file_rules WHERE template_stage_id = ? AND file_rule_code NOT IN (?)`, stageID, keepFileRuleCodes)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(r.DB.Rebind(q), args...)
	return err
}

// =============================================
// 查询方法
// =============================================

// ListTemplates 列出本地缓存的模版
func (r *TemplateCacheRepository) ListTemplates(status string) ([]models.DataTemplate, error) {
	q := `SELECT * FROM data_templates WHERE disable = 0`
	args := []interface{}{}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY update_time DESC`
	var list []models.DataTemplate
	if err := r.DB.Select(&list, q, args...); err != nil {
		return nil, err
	}
	return list, nil
}

// FindTemplateByID 按本地 id 查找模版
func (r *TemplateCacheRepository) FindTemplateByID(id int64) (*models.DataTemplate, error) {
	var t models.DataTemplate
	if err := r.DB.Get(&t, `SELECT * FROM data_templates WHERE id = ? AND disable = 0`, id); err != nil {
		return nil, err
	}
	return &t, nil
}

// FindTemplateByCode 按业务编码+版本查找模版
func (r *TemplateCacheRepository) FindTemplateByCode(code, version string) (*models.DataTemplate, error) {
	var t models.DataTemplate
	if err := r.DB.Get(&t, `SELECT * FROM data_templates WHERE template_code = ? AND template_version = ? AND disable = 0`, code, version); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListStagesByTemplate 列出模版下的工作环节
func (r *TemplateCacheRepository) ListStagesByTemplate(templateID int64) ([]models.TemplateStage, error) {
	var list []models.TemplateStage
	err := r.DB.Select(&list, `SELECT * FROM template_stages WHERE template_id = ? AND disable = 0 ORDER BY sort_order`, templateID)
	return list, err
}

// ListFileRulesByStage 列出环节下的文件规则
func (r *TemplateCacheRepository) ListFileRulesByStage(stageID int64) ([]models.TemplateFileRule, error) {
	var list []models.TemplateFileRule
	err := r.DB.Select(&list, `SELECT * FROM template_file_rules WHERE template_stage_id = ? AND disable = 0 ORDER BY sort_order, file_rule_code`, stageID)
	return list, err
}

// FullTemplate 模版完整结构
type FullTemplate struct {
	Template *models.DataTemplate `json:"template"`
	Stages   []FullStage          `json:"stages"`
}

// FullStage 工作环节及其文件规则
type FullStage struct {
	models.TemplateStage
	FileRules []models.TemplateFileRule `json:"file_rules"`
}

// GetFullTemplate 加载模版完整结构
func (r *TemplateCacheRepository) GetFullTemplate(templateID int64) (*FullTemplate, error) {
	tpl, err := r.FindTemplateByID(templateID)
	if err != nil {
		return nil, err
	}
	stages, err := r.ListStagesByTemplate(templateID)
	if err != nil {
		return nil, err
	}
	full := &FullTemplate{Template: tpl, Stages: make([]FullStage, 0, len(stages))}
	for _, s := range stages {
		rules, err := r.ListFileRulesByStage(s.ID)
		if err != nil {
			return nil, err
		}
		full.Stages = append(full.Stages, FullStage{TemplateStage: s, FileRules: rules})
	}
	return full, nil
}
