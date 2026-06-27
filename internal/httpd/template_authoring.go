package httpd

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// 2026-05-31 数据业务模版本地创作 handler（阶段一）。
// 项目模版 = 五层树的根，origin=local。编码全自动，用户只填业务字段。

type localTemplateBody struct {
	ClassCode        string `json:"class_code"`
	Scope            string `json:"scope"`
	TemplateName     string `json:"template_name"`
	ShortCode        string `json:"short_code"`
	Manager          string `json:"manager"`
	Description      string `json:"description"`
	ApprovalBasis    string `json:"approval_basis"`
	SensitivityLevel string `json:"sensitivity_level"`
	Owner            string `json:"owner"`
}

func (b localTemplateBody) toInput() repository.CreateTemplateInput {
	return repository.CreateTemplateInput{
		ClassCode:        b.ClassCode,
		Scope:            b.Scope,
		TemplateName:     b.TemplateName,
		ShortCode:        b.ShortCode,
		Manager:          b.Manager,
		Description:      b.Description,
		ApprovalBasis:    b.ApprovalBasis,
		SensitivityLevel: b.SensitivityLevel,
		Owner:            b.Owner,
	}
}

// ListLocalTemplates GET /templates/authoring?class_code=&scope=
func ListLocalTemplates(c *gin.Context) {
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	list, err := repo.ListLocalTemplates(c.Query("class_code"), c.Query("scope"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// CreateLocalTemplate POST /templates
func CreateLocalTemplate(c *gin.Context) {
	var body localTemplateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	tpl, err := repo.CreateLocalTemplate(body.toInput())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": tpl})
}

// UpdateLocalTemplate PUT /templates/:id
func UpdateLocalTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body localTemplateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.UpdateLocalTemplate(id, body.toInput()); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteLocalTemplate DELETE /templates/:id
func DeleteLocalTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.DeleteLocalTemplate(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ListManageBusinessClasses GET /manage-business-classes —— 代理拉取「数据业务分类」下拉数据。
// 行业分类以「模版管理平台」(template-manage，:19092) 为准：与 scan 本地 business_classes
// 缓存(由模版同步从同一平台灌入)、以及创作模版时选的行业保持同源；模版 ingest 到 data-asset-manage
// 时按 code find-or-create，不会因行业不存在而丢失。
func ListManageBusinessClasses(c *gin.Context) {
	db := repository.GetDB()
	endpoint := repository.NewSystemConfigRepository(db).GetEffectiveTemplateServerEndpoint()
	classes, err := repository.FetchManageBusinessClasses(nil, endpoint)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": classes})
}

// ListManageUsers GET /manage-users —— 代理拉取 manage 端已注册用户（供「项目负责人」等下拉）
func ListManageUsers(c *gin.Context) {
	db := repository.GetDB()
	endpoint := repository.NewSystemConfigRepository(db).GetValue(repository.KeyManageEndpoint)
	users, err := repository.FetchManageUsers(nil, endpoint)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": users})
}

// PushTemplateToManage POST /templates/:id/push —— 把本地模版推送到 manage（反向同步）
func PushTemplateToManage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	db := repository.GetDB()
	endpoint := repository.NewSystemConfigRepository(db).GetValue(repository.KeyManageEndpoint)
	repo := repository.NewTemplateAuthoringRepository(db)
	remoteID, err := repo.PushTemplateToManage(nil, endpoint, id, false, "") // 模版同步，非立项
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"remote_id": remoteID}})
}

// InitiateTemplate POST /templates/:id/initiate —— 立项：把本地模版作为项目推送到 manage（is_project=1，进进度跟踪）
func InitiateTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	db := repository.GetDB()
	endpoint := repository.NewSystemConfigRepository(db).GetValue(repository.KeyManageEndpoint)
	repo := repository.NewTemplateAuthoringRepository(db)
	remoteID, err := repo.PushTemplateToManage(nil, endpoint, id, true, currentOperator(c)) // 立项，记立项人
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"remote_id": remoteID}})
}

// PublishLocalTemplate POST /templates/:id/publish  body: { published: bool }
// 设置本地模版的「是否发布」状态；只有已发布的模版才能立项。
func PublishLocalTemplate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body struct {
		Published bool `json:"published"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.SetTemplatePublished(id, body.Published); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"is_published": body.Published}})
}

// GetLocalTemplateTree GET /templates/:id/tree —— 本地模版完整五层树
func GetLocalTemplateTree(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	tree, err := repo.GetLocalTemplateTree(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": tree})
}

// ============ 工作事项 /template-stages ============

func RegisterTemplateStagesRoutes(r *gin.RouterGroup) {
	r.GET("", ListTemplateStages) // ?template_id=
	r.POST("", CreateTemplateStage)
	r.PUT("/:id", UpdateTemplateStage)
	r.DELETE("/:id", DeleteTemplateStage)
}

type stageBody struct {
	TemplateID       int64  `json:"template_id"`
	Name             string `json:"name"`
	Manager          string `json:"manager"`
	ManagerUsername  string `json:"manager_username"`
	Members          string `json:"members"`
	MembersUsernames string `json:"members_usernames"`
	Desc             string `json:"desc"`
}

func (b stageBody) toInput() repository.StageInput {
	return repository.StageInput{
		Name: b.Name, Manager: b.Manager, ManagerUsername: b.ManagerUsername,
		Members: b.Members, MembersUsernames: b.MembersUsernames, Desc: b.Desc,
	}
}

func ListTemplateStages(c *gin.Context) {
	tid, _ := strconv.ParseInt(c.Query("template_id"), 10, 64)
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	list, err := repo.ListStages(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

func CreateTemplateStage(c *gin.Context) {
	var body stageBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	st, err := repo.CreateStage(body.TemplateID, body.toInput())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo.MarkTemplateEditedByStage(st.ID) // 用户编辑→置 edited（提取门禁）
	c.JSON(http.StatusOK, gin.H{"success": true, "data": st})
}

func UpdateTemplateStage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body stageBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.UpdateStage(id, body.toInput()); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo.MarkTemplateEditedByStage(id)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeleteTemplateStage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.DeleteStage(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo.MarkTemplateEditedByStage(id) // 软删后行仍在，可定位所属模版
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============ 文件任务 /template-tasks ============

func RegisterTemplateTasksRoutes(r *gin.RouterGroup) {
	r.GET("", ListTemplateTasks) // ?stage_id=
	r.POST("", CreateTemplateTask)
	r.PUT("/:id", UpdateTemplateTask)
	r.DELETE("/:id", DeleteTemplateTask)
}

type taskBody struct {
	StageID          int64  `json:"stage_id"`
	Name             string `json:"name"`
	Manager          string `json:"manager"`
	SensitivityLevel string `json:"sensitivity_level"`
	Desc             string `json:"desc"`
}

func (b taskBody) toInput() repository.TaskInput {
	return repository.TaskInput{Name: b.Name, Manager: b.Manager, SensitivityLevel: b.SensitivityLevel, Desc: b.Desc}
}

func ListTemplateTasks(c *gin.Context) {
	sid, _ := strconv.ParseInt(c.Query("stage_id"), 10, 64)
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	list, err := repo.ListTasks(sid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

func CreateTemplateTask(c *gin.Context) {
	var body taskBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	tk, err := repo.CreateTask(body.StageID, body.toInput())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo.MarkTemplateEditedByStage(body.StageID)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": tk})
}

func UpdateTemplateTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body taskBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.UpdateTask(id, body.toInput()); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo.MarkTemplateEditedByTask(id)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeleteTemplateTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.DeleteTask(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo.MarkTemplateEditedByTask(id)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============ 文档标识 /template-file-rules ============

func RegisterTemplateFileRulesRoutes(r *gin.RouterGroup) {
	r.GET("", ListTemplateFileRules) // ?task_id=
	r.POST("", CreateTemplateFileRule)
	r.PUT("/:id", UpdateTemplateFileRule)
	r.DELETE("/:id", DeleteTemplateFileRule)
}

type fileRuleBody struct {
	TaskID           int64  `json:"task_id"`
	FileName         string `json:"file_name"`
	DataState        string `json:"data_state"`
	Required         bool   `json:"required"`
	AllowedFileTypes string `json:"allowed_file_types"`
	NamingPattern    string `json:"naming_pattern"`
	SummaryPattern   string `json:"summary_pattern"`
	Drafter          string `json:"drafter"`
	SensitivityLevel string `json:"sensitivity_level"`
	RetentionPolicy  string `json:"retention_policy"`
	// L6 文档标识管控类字段
	Category             string `json:"category"`
	SecurityRequirement  string `json:"security_requirement"`
	DiffusionRequirement string `json:"diffusion_requirement"`
	ArchiveRequirement   string `json:"archive_requirement"`
	RetentionPeriodDays  *int   `json:"retention_period_days"`
	DestructionRule      string `json:"destruction_rule"`
}

func (b fileRuleBody) toInput() repository.FileRuleInput {
	return repository.FileRuleInput{
		FileName: b.FileName, DataState: b.DataState, Required: b.Required, AllowedFileTypes: b.AllowedFileTypes,
		NamingPattern: b.NamingPattern, SummaryPattern: b.SummaryPattern, Drafter: b.Drafter,
		SensitivityLevel: b.SensitivityLevel, RetentionPolicy: b.RetentionPolicy,
		Category: b.Category, SecurityRequirement: b.SecurityRequirement, DiffusionRequirement: b.DiffusionRequirement,
		ArchiveRequirement: b.ArchiveRequirement, RetentionPeriodDays: b.RetentionPeriodDays, DestructionRule: b.DestructionRule,
	}
}

func ListTemplateFileRules(c *gin.Context) {
	tid, _ := strconv.ParseInt(c.Query("task_id"), 10, 64)
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	list, err := repo.ListFileRules(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

func CreateTemplateFileRule(c *gin.Context) {
	var body fileRuleBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	fr, err := repo.CreateFileRule(body.TaskID, body.toInput())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo.MarkTemplateEditedByTask(body.TaskID)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": fr})
}

func UpdateTemplateFileRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body fileRuleBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	if err := repo.UpdateFileRule(id, body.toInput()); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo.MarkTemplateEditedByFileRule(id)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeleteTemplateFileRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	repo.MarkTemplateEditedByFileRule(id) // 删前定位所属模版置 edited
	if err := repo.DeleteFileRule(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
