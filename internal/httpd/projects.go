package httpd

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/models"
	"data-asset-scan-go/internal/repository"
)

// RegisterProjectsRoutes 注册 /projects 路由
func RegisterProjectsRoutes(r *gin.RouterGroup) {
	r.GET("", ListProjects)
	r.POST("", CreateProject)
	r.GET("/:id", GetProjectDetail)
	r.POST("/:id/activate", RequireProjectAction("write"), ActivateProject) // V3-8 §8.2
	r.POST("/:id/cancel", RequireProjectAction("close"), CancelProject)     // V3-8 §8.2
	r.GET("/:id/stages", ListProjectStages)
	r.GET("/:id/stages-with-rules", ListProjectStagesWithRules)                                     // V5-P1 Task 5: AI 归目调整对话框
	r.POST("/:id/stages/:stage_id/status", RequireProjectAction("write"), UpdateProjectStageStatus) // V3-3
	r.GET("/:id/members", ListProjectMembers)
	r.GET("/:id/file-versions", ListProjectFileVersions)
	r.GET("/:id/events", ListProjectEvents)
	r.GET("/:id/ledgers", ListProjectLedgers)

	// G1/G2/G3 结项 + 归档 + 上报（受 close 权限保护）
	r.GET("/:id/close/precheck", PrecheckProjectClose)
	r.POST("/:id/close", RequireProjectAction("close"), CloseProject)
	r.POST("/:id/sync", RequireProjectAction("close"), SyncProjectArchive)

	// 一键归档（按九宫格分流：个人→本地夹 / 部门、单位→上报云端 / 行业→跳过）
	r.POST("/quick-archive-all", QuickArchiveAllProjects)
	r.POST("/:id/quick-archive", RequireProjectAction("close"), QuickArchiveProject)
}

// ListProjects GET /projects?status=&keyword=
func ListProjects(c *gin.Context) {
	status := c.Query("status")
	keyword := c.Query("keyword")
	repo := repository.NewDataProjectRepository(repository.GetDB())
	list, err := repo.List(status, keyword)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// V4-Q2 懒触发：若是 active 查询且本地缺 SYS-PERSONAL-* 三个个人项目，
	// 同步再 bootstrap 一次（可能从 manage 拉模板并建项目），再重查。
	// 这覆盖"启动时 manage_endpoint 未配 / manage 不可达 / 模板后到位"等启动
	// 期 bootstrap 失败场景。throttle 防止 manage 不可达时反复阻塞 15s。
	if (status == "" || status == "active") && !hasAnyPersonalProject(list) && allowBootstrapAttempt() {
		repository.BootstrapPersonalProjects(repository.GetDB())
		// 重查
		list, err = repo.List(status, keyword)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// hasAnyPersonalProject 判断列表里是否已有任意一个 SYS-PERSONAL-* 个人项目
func hasAnyPersonalProject(list []models.DataProject) bool {
	for _, p := range list {
		if strings.HasPrefix(p.ProjectCode, "SYS-PERSONAL-") {
			return true
		}
	}
	return false
}

// 懒触发 bootstrap 的 throttle — 最近 30 秒内尝试过就跳过，避免 manage 不可
// 达时反复阻塞 15s。返回 true 表示允许尝试。
var (
	lastBootstrapAttempt time.Time
	bootstrapAttemptMu   sync.Mutex
)

func allowBootstrapAttempt() bool {
	bootstrapAttemptMu.Lock()
	defer bootstrapAttemptMu.Unlock()
	if time.Since(lastBootstrapAttempt) < 30*time.Second {
		return false
	}
	lastBootstrapAttempt = time.Now()
	return true
}

// resetBootstrapThrottleForTest 仅供测试调用，重置 throttle 状态
func resetBootstrapThrottleForTest() {
	bootstrapAttemptMu.Lock()
	defer bootstrapAttemptMu.Unlock()
	lastBootstrapAttempt = time.Time{}
}

// CreateProjectRequest 立项请求
type CreateProjectRequest struct {
	TemplateCode       string                   `json:"template_code"`
	TemplateVersion    string                   `json:"template_version"`
	ProjectName        string                   `json:"project_name"`
	ObjectShortCode    string                   `json:"object_short_code"`
	TaskSummary        string                   `json:"task_summary"`
	ApprovalBasis      string                   `json:"approval_basis"`
	PlannedStart       string                   `json:"planned_start_date"` // YYYY-MM-DD
	PlannedEnd         string                   `json:"planned_end_date"`   // YYYY-MM-DD
	SensitivityLevel   string                   `json:"sensitivity_level"`
	ManagementMode     string                   `json:"management_mode"`
	OwnerSubjectID     int64                    `json:"owner_subject_id"`
	CustodianSubjectID int64                    `json:"custodian_subject_id"`
	SecuritySubjectID  int64                    `json:"security_subject_id"`
	Members            []repository.MemberInput `json:"members"`
	Activate           bool                     `json:"activate"`
}

type projectMemberResponse struct {
	ID                int64     `db:"id" json:"id"`
	ProjectID         int64     `db:"project_id" json:"project_id"`
	UserID            *int64    `db:"user_id" json:"user_id"`
	SubjectID         int64     `db:"subject_id" json:"subject_id"`
	RoleCode          string    `db:"role_code" json:"role_code"`
	StageIDs          *string   `db:"stage_ids" json:"stage_ids"`
	PermissionActions string    `db:"permission_actions" json:"permission_actions"`
	CreateTime        time.Time `db:"create_time" json:"create_time"`
	UpdateTime        time.Time `db:"update_time" json:"update_time"`
	Disable           int       `db:"disable" json:"disable"`
	UserUsername      *string   `db:"user_username" json:"user_username,omitempty"`
	UserDisplayName   *string   `db:"user_display_name" json:"user_display_name,omitempty"`
	UserCompanyName   *string   `db:"user_company_name" json:"user_company_name,omitempty"`
	UserDepartment    *string   `db:"user_department" json:"user_department,omitempty"`
}

// CreateProject POST /projects 立项
func CreateProject(c *gin.Context) {
	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// 项目编码完全由后台生成：用户不再填业务对象简码。
	// 缺省 / 空白时统一用 "PROJ" 当前缀 → 最终生成 PROJ-{YYYY}-{NNN}。
	objectShortCode := req.ObjectShortCode
	if objectShortCode == "" {
		objectShortCode = "PROJ"
	}

	in := repository.InstantiateInput{
		TemplateCode:       req.TemplateCode,
		TemplateVersion:    req.TemplateVersion,
		ProjectName:        req.ProjectName,
		ObjectShortCode:    objectShortCode,
		TaskSummary:        req.TaskSummary,
		ApprovalBasis:      req.ApprovalBasis,
		SensitivityLevel:   req.SensitivityLevel,
		ManagementMode:     req.ManagementMode,
		OwnerSubjectID:     req.OwnerSubjectID,
		CustodianSubjectID: req.CustodianSubjectID,
		SecuritySubjectID:  req.SecuritySubjectID,
		Members:            req.Members,
		CreatedBy:          currentOperator(c), // V1 兼容：用户名字符串
		CreatedByUserID:    currentUserID(c),   // V2：立项人自动 enroll
		Activate:           req.Activate,
	}
	if req.PlannedStart != "" {
		if t, err := time.Parse("2006-01-02", req.PlannedStart); err == nil {
			in.PlannedStartDate = &t
		}
	}
	if req.PlannedEnd != "" {
		if t, err := time.Parse("2006-01-02", req.PlannedEnd); err == nil {
			in.PlannedEndDate = &t
		}
	}

	svc := repository.NewProjectInstantiationService(repository.GetDB())
	out, err := svc.Instantiate(in)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// V3-5 §11.1.2 项目立项审计（含激活，因为 V1 立项可同时 activate）
	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	action := repository.AuditProjectCreate
	if in.Activate {
		action = repository.AuditProjectActivate
	}
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      action,
		TargetType:  repository.AuditTargetProject,
		TargetID:    out.Project.ID,
		TargetCode:  out.Project.ProjectCode,
		After:       out.Project,
		IPAddress:   c.ClientIP(),
		Message:     "立项：" + out.Project.ProjectName,
	})

	// V3-A 收尾 §11.1.6 权限配置变更审计：立项时每个 member 落一条 member_add
	// 包括自动 enroll 的立项人（user_id != 0）和向导手动添加的成员
	memberRepo := repository.NewProjectMemberRepository(repository.GetDB())
	if members, err := memberRepo.ListByProject(out.Project.ID); err == nil {
		for _, m := range members {
			_, _ = auditRepo.Append(repository.AppendAuditInput{
				ActorID:     currentOperator(c),
				ActorUserID: currentUserID(c),
				Action:      repository.AuditMemberAdd,
				TargetType:  repository.AuditTargetProjectMember,
				TargetID:    m.ID,
				TargetCode:  out.Project.ProjectCode,
				After:       m,
				IPAddress:   c.ClientIP(),
				Message:     "立项时添加项目成员：role=" + m.RoleCode,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// GetProjectDetail GET /projects/:id
func GetProjectDetail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	pRepo := repository.NewDataProjectRepository(repository.GetDB())
	p, err := pRepo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "project not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": p})
}

// ListProjectStages GET /projects/:id/stages
func ListProjectStages(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewProjectStageRepository(repository.GetDB())
	stages, err := repo.ListByProject(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stages})
}

// ListProjectStagesWithRules GET /projects/:id/stages-with-rules
// 返回项目的所有 active 环节，并嵌入每个环节的 file rules（来自 template）。
// V5-P1 Task 5: AI 归目调整对话框需要这个三级选项数据。
func ListProjectStagesWithRules(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "INVALID_ID"})
		return
	}
	db := repository.GetDB()

	type stageRow struct {
		StageCode     string `db:"stage_code"`
		StageName     string `db:"stage_name"`
		SortOrder     int    `db:"sort_order"`
		TemplateStgID int64  `db:"template_stage_id"`
	}
	var stages []stageRow
	if err := db.Select(&stages, `
		SELECT stage_code, stage_name, sort_order, COALESCE(template_stage_id, 0) AS template_stage_id
		FROM project_stages
		WHERE project_id = ? AND disable = 0
		ORDER BY sort_order`, projectID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	type ruleRow struct {
		FileRuleCode string `db:"file_rule_code" json:"file_rule_code"`
		FileName     string `db:"file_name" json:"file_name"`
		DataState    string `db:"data_state" json:"data_state"`
	}
	type stageOut struct {
		StageCode string    `json:"stage_code"`
		StageName string    `json:"stage_name"`
		Rules     []ruleRow `json:"rules"`
	}
	out := make([]stageOut, 0, len(stages))
	for _, s := range stages {
		var rules []ruleRow
		if s.TemplateStgID > 0 {
			if err := db.Select(&rules, `
				SELECT file_rule_code, file_name, data_state
				FROM template_file_rules
				WHERE template_stage_id = ? AND disable = 0
				ORDER BY sort_order`, s.TemplateStgID); err != nil {
				// 单一 stage 的 rules 查询失败不应整体 fail，仅 log 并跳过 rules（stage 仍返回）
				log.Printf("[ListProjectStagesWithRules] rules query failed for stage %d: %v", s.TemplateStgID, err)
				rules = nil
			}
		}
		if rules == nil {
			rules = []ruleRow{}
		}
		out = append(out, stageOut{
			StageCode: s.StageCode,
			StageName: s.StageName,
			Rules:     rules,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"stages": out},
	})
}

// ActivateProject POST /projects/:id/activate （V3-8 §8.2）
//
// 把 draft 状态的项目设为 active。
// 对应文档 §6.2 "确认安全要求 + 实例化项目"——立项后可分两步：先 draft，后 activate。
// V1 默认立项 immediate activate；本端点支持 draft→active 显式切换。
func ActivateProject(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	pRepo := repository.NewDataProjectRepository(repository.GetDB())
	p, err := pRepo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "project not found"})
		return
	}
	if p.Status != "draft" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "仅 draft 项目可激活，当前 " + p.Status})
		return
	}
	tx, err := repository.GetDB().Beginx()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := pRepo.SetStatus(tx, id, "active"); err != nil {
		tx.Rollback()
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// V3-5 §11.1.2 项目激活审计
	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditProjectActivate,
		TargetType:  repository.AuditTargetProject,
		TargetID:    id,
		TargetCode:  p.ProjectCode,
		Before:      gin.H{"status": "draft"},
		After:       gin.H{"status": "active"},
		IPAddress:   c.ClientIP(),
	})

	updated, _ := pRepo.FindByID(id)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

// CancelProject POST /projects/:id/cancel （V3-8 §8.2）
//
// 在 draft 或 active 状态把项目设为 cancelled。
// archived 不可取消（已结项归档）。
func CancelProject(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)

	pRepo := repository.NewDataProjectRepository(repository.GetDB())
	p, err := pRepo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "project not found"})
		return
	}
	if p.Status != "draft" && p.Status != "active" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "仅 draft / active 项目可取消，当前 " + p.Status})
		return
	}
	tx, err := repository.GetDB().Beginx()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := pRepo.SetStatus(tx, id, "cancelled"); err != nil {
		tx.Rollback()
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// V3-5 §11.1.2 项目取消审计
	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditProjectCancel,
		TargetType:  repository.AuditTargetProject,
		TargetID:    id,
		TargetCode:  p.ProjectCode,
		Before:      gin.H{"status": p.Status},
		After:       gin.H{"status": "cancelled"},
		IPAddress:   c.ClientIP(),
		Message:     "取消原因：" + req.Reason,
	})

	updated, _ := pRepo.FindByID(id)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": updated})
}

// UpdateStageStatusRequest V3-3 §7.3 切换环节状态入参
type UpdateStageStatusRequest struct {
	ToStatus string `json:"to_status"` // running / completed / skipped
	Reason   string `json:"reason"`    // 可选，审计用
}

// UpdateProjectStageStatus POST /projects/:id/stages/:stage_id/status
//
// V3-3 §7.3 + §5.2 环节状态机受控切换
// 受 write 权限保护（环节进度调整属于本环节工作的一部分）
func UpdateProjectStageStatus(c *gin.Context) {
	stageID, err := strconv.ParseInt(c.Param("stage_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid stage_id"})
		return
	}
	var req UpdateStageStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.ToStatus == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "to_status 必填"})
		return
	}
	repo := repository.NewProjectStageRepository(repository.GetDB())
	if err := repo.UpdateStageStatus(stageID, req.ToStatus); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	stage, _ := repo.FindByID(stageID)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stage})
}

// ListProjectMembers GET /projects/:id/members
func ListProjectMembers(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var list []projectMemberResponse
	err = repository.GetDB().Select(&list, `
		SELECT
			pm.id, pm.project_id, pm.user_id, pm.subject_id, pm.role_code, pm.stage_ids,
			pm.permission_actions, pm.create_time, pm.update_time, pm.disable,
			u.username AS user_username,
			u.display_name AS user_display_name,
			u.company_name AS user_company_name,
			u.department AS user_department
		FROM project_members pm
		LEFT JOIN users u ON u.id = pm.user_id AND u.disable = 0
		WHERE pm.project_id = ? AND pm.disable = 0
		ORDER BY pm.id
	`, id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// ListProjectFileVersions GET /projects/:id/file-versions
func ListProjectFileVersions(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewFileVersionRepository(repository.GetDB())
	list, err := repo.ListByProject(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// ListProjectEvents GET /projects/:id/events
//
// 项目级生命周期事件流（带文件版本/底账上下文）。
// 可附带 ?event_type= ?stage_code= ?limit= 进一步筛选。
func ListProjectEvents(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	pRepo := repository.NewDataProjectRepository(repository.GetDB())
	p, err := pRepo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	in := repository.SearchEventsInput{
		ProjectCode: p.ProjectCode,
		EventType:   c.Query("event_type"),
		StageCode:   c.Query("stage_code"),
	}
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			in.Limit = n
		}
	}
	svc := repository.NewLedgerLifecycleService(repository.GetDB())
	events, err := svc.SearchEventsByProject(in)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": events})
}

// ListProjectLedgers GET /projects/:id/ledgers
func ListProjectLedgers(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	pRepo := repository.NewDataProjectRepository(repository.GetDB())
	p, err := pRepo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	rows, err := repo.Search(repository.LedgerSearchInput{
		ProjectCode:     p.ProjectCode,
		StageCode:       c.Query("stage_code"),
		LifecycleStatus: c.Query("lifecycle_status"),
		Keyword:         c.Query("keyword"),
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}
