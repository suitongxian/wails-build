package httpd

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"data-asset-scan-go/internal/repository"

	"github.com/gin-gonic/gin"
)

// 一键归档（2026-06-24）：把工作空间里项目目录下的文件，按九宫格分流归档。
//   个人(person)→本地复制个人夹 / 部门、单位→上报云端 manage / 行业→跳过。
// 只读取、复制，不删除/修改任何原文件。

// quickArchiveProjectSummary 单个项目的归档结果摘要（全局归档时聚合用）。
type quickArchiveProjectSummary struct {
	ProjectCode string   `json:"project_code"`
	ProjectName string   `json:"project_name"`
	Scope       string   `json:"scope"`
	Route       string   `json:"route"`
	Archived    int      `json:"archived"`
	Skipped     int      `json:"skipped"`
	Errors      []string `json:"errors,omitempty"`
}

// QuickArchiveProject POST /projects/:id/quick-archive — 单个项目一键归档。
func QuickArchiveProject(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "非法项目 id"})
		return
	}
	db := repository.GetDB()
	ctx, err := repository.GetProjectArchiveContext(db, id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "项目不存在或已删除"})
		return
	}
	if repository.IsPersonalSystemProject(ctx.ProjectCode) {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "个人内置容器不参与一键归档"})
		return
	}
	root := strings.TrimSpace(repository.NewSystemConfigRepository(db).GetEffectiveProjectRoot())
	if root == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置工作空间目录"})
		return
	}
	res, err := repository.ArchiveProjectByScope(db, root, ctx.ProjectCode, ctx.ProjectName, ctx.Scope, ctx.Sensitivity, currentOperator(c), "", "")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error(), "data": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// QuickArchiveAllProjects POST /projects/quick-archive-all — 巡检工作空间所有项目目录批量归档。
func QuickArchiveAllProjects(c *gin.Context) {
	db := repository.GetDB()
	root := strings.TrimSpace(repository.NewSystemConfigRepository(db).GetEffectiveProjectRoot())
	if root == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置工作空间目录"})
		return
	}
	list, err := repository.NewDataProjectRepository(db).List("", "")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	operator := currentOperator(c)
	var summaries []quickArchiveProjectSummary
	totalArchived, totalSkipped := 0, 0
	for _, p := range list {
		if repository.IsPersonalSystemProject(p.ProjectCode) || p.Status == "cancelled" {
			continue // 内置个人容器、已取消项目不归档
		}
		ctx, cerr := repository.GetProjectArchiveContext(db, p.ID)
		if cerr != nil {
			continue
		}
		res, aerr := repository.ArchiveProjectByScope(db, root, ctx.ProjectCode, ctx.ProjectName, ctx.Scope, ctx.Sensitivity, operator, "", "")
		s := quickArchiveProjectSummary{
			ProjectCode: ctx.ProjectCode, ProjectName: ctx.ProjectName, Scope: ctx.Scope,
			Route: res.RouteTip, Archived: res.Archived, Skipped: res.Skipped, Errors: res.Errors,
		}
		if aerr != nil {
			s.Errors = append(s.Errors, aerr.Error())
		}
		totalArchived += res.Archived
		totalSkipped += res.Skipped
		summaries = append(summaries, s)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"total_archived": totalArchived,
		"total_skipped":  totalSkipped,
		"projects":       summaries,
	}})
}

// ── 集中立项项目的一键归档（这是当前在用的项目流程）──
//
// scope 取自项目专属模版 TPL-PRJ-<remote_id>（缺则默认 unit）；
// sensitivity 取自 centralized_project_applications.sensitivity_level；
// 目录名由 centralizedDirCode 算出（{项目名}-{编码}）。

type centralizedArchiveCtx struct {
	dir          string
	name         string
	scope        string
	sensitivity  string
	custodyScope string // 定稿保管层级（单位级项目可选 unit/department）
	custodyNote  string // 归档归属说明（选填）
}

// resolveCentralizedArchiveCtx 从本地 centralized_project_applications 解析归档上下文。
// 仅立项者本机有该行；参与人本机通常没有（项目从 manage 下发），此时返回 err，
// 由调用方改用请求体里随 my-tasks 下发的 scope/sensitivity/project_code。
func resolveCentralizedArchiveCtx(db interface {
	Get(dest interface{}, query string, args ...interface{}) error
}, remoteID int64) (*centralizedArchiveCtx, error) {
	var row struct {
		Name         string `db:"project_name"`
		Code         string `db:"project_code"`
		Sens         string `db:"sensitivity_level"`
		Scope        string `db:"project_scope"`
		CustodyScope string `db:"output_custody_scope"`
		CustodyNote  string `db:"output_custody_note"`
	}
	if err := db.Get(&row, `
		SELECT COALESCE(project_name,'') AS project_name,
		       COALESCE(project_code,'') AS project_code,
		       COALESCE(sensitivity_level,'general') AS sensitivity_level,
		       COALESCE(project_scope,'') AS project_scope,
		       COALESCE(output_custody_scope,'') AS output_custody_scope,
		       COALESCE(output_custody_note,'') AS output_custody_note
		FROM centralized_project_applications
		WHERE manage_remote_id = ? AND disable = 0 LIMIT 1`, remoteID); err != nil {
		return nil, err
	}
	scope := strings.TrimSpace(row.Scope) // 立项所选层级优先
	if scope == "" {
		// 兜底：旧数据无 project_scope 时，退回项目专属模版的 scope（再不行默认 unit）
		scope = "unit"
		var s string
		if err := db.Get(&s, `SELECT COALESCE(scope,'') FROM data_templates WHERE template_code = ? AND disable = 0 LIMIT 1`,
			"TPL-PRJ-"+strconv.FormatInt(remoteID, 10)); err == nil && strings.TrimSpace(s) != "" {
			scope = s
		}
	}
	return &centralizedArchiveCtx{
		dir:          centralizedDirCode(repository.GetDB(), remoteID, row.Code),
		name:         row.Name,
		scope:        scope,
		sensitivity:  row.Sens,
		custodyScope: strings.TrimSpace(row.CustodyScope),
		custodyNote:  strings.TrimSpace(row.CustodyNote),
	}, nil
}

// quickArchiveBody 参与人侧从 my-tasks 任务项带来的项目上下文（本机无 cpa 行时为权威来源）。
type quickArchiveBody struct {
	ProjectCode      string `json:"project_code"`
	ProjectName      string `json:"project_name"`
	ProjectScope     string `json:"project_scope"`
	SensitivityLevel string `json:"sensitivity_level"`
	CustodyScope     string `json:"output_custody_scope"` // 定稿保管层级
	CustodyNote      string `json:"output_custody_note"`  // 归档归属说明
}

// QuickArchiveCentralizedProject POST /centralized-projects/:remote_id/quick-archive
// 优先用请求体（随 my-tasks 下发的立项 scope/敏感级），本机有 cpa 行则作兜底。
func QuickArchiveCentralizedProject(c *gin.Context) {
	remoteID, err := strconv.ParseInt(c.Param("remote_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid remote_id"})
		return
	}
	var body quickArchiveBody
	_ = c.ShouldBindJSON(&body)
	db := repository.GetDB()

	local, _ := resolveCentralizedArchiveCtx(db, remoteID) // 参与人可能为 nil
	code := strings.TrimSpace(body.ProjectCode)
	name := strings.TrimSpace(body.ProjectName)
	scope := strings.TrimSpace(body.ProjectScope)
	sensitivity := strings.TrimSpace(body.SensitivityLevel)
	custodyScope := strings.TrimSpace(body.CustodyScope)
	custodyNote := strings.TrimSpace(body.CustodyNote)
	if local != nil {
		if name == "" {
			name = local.name
		}
		if scope == "" {
			scope = local.scope
		}
		if sensitivity == "" {
			sensitivity = local.sensitivity
		}
		if custodyScope == "" {
			custodyScope = local.custodyScope
		}
		if custodyNote == "" {
			custodyNote = local.custodyNote
		}
	}
	if scope == "" {
		scope = "unit"
	}
	if sensitivity == "" {
		sensitivity = "general"
	}
	// 目录：优先用体里 project_code（与 my-tasks/workbench 落盘一致）；否则用本机 cpa 推出的目录。
	var dir string
	switch {
	case code != "":
		dir = centralizedDirCode(db, remoteID, code)
	case local != nil:
		dir = local.dir
	default:
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "无法确定项目（缺 project_code）"})
		return
	}

	root := strings.TrimSpace(repository.NewSystemConfigRepository(db).GetEffectiveProjectRoot())
	if root == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置工作空间目录"})
		return
	}
	res, err := repository.ArchiveProjectByScope(db, root, dir, name, scope, sensitivity, currentOperator(c), custodyScope, custodyNote)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error(), "data": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// QuickArchiveCabinetFilesProxy GET /centralized-projects/quick-archive-files?scope=&sensitivity_level=
// 列出已上报云端的部门/单位柜室文件（供「档案在线阅卷」部门/单位页展示）。透传 manage。
func QuickArchiveCabinetFilesProxy(c *gin.Context) {
	ep := strings.TrimRight(strings.TrimSpace(repository.NewSystemConfigRepository(repository.GetDB()).GetValue(repository.KeyManageEndpoint)), "/")
	if ep == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	q := url.Values{}
	if s := strings.TrimSpace(c.Query("scope")); s != "" {
		q.Set("scope", s)
	}
	if s := strings.TrimSpace(c.Query("sensitivity_level")); s != "" {
		q.Set("sensitivity_level", s)
	}
	proxyToManage(c, "GET", ep+"/api/quick-archive/files?"+q.Encode(), nil)
}

// CentralizedFinalFilesProxy GET /centralized-projects/:remote_id/final-files
// 拉取某集中立项项目已归档到「部门柜」的定稿(output)文件清单，供项目负责人结项时勾选归卷单位室。
// 按 centralizedDirCode（项目名-编号，与一键归档落库键一致）+ scope=department + bucket=output 过滤透传 manage。
func CentralizedFinalFilesProxy(c *gin.Context) {
	remoteID, err := strconv.ParseInt(c.Param("remote_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid remote_id"})
		return
	}
	db := repository.GetDB()
	ep := strings.TrimRight(strings.TrimSpace(repository.NewSystemConfigRepository(db).GetValue(repository.KeyManageEndpoint)), "/")
	if ep == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	dir := centralizedDirCode(db, remoteID, strings.TrimSpace(c.Query("project_code")))
	q := url.Values{}
	q.Set("project_code", dir)
	q.Set("scope", "department")
	q.Set("bucket", "output")
	proxyToManage(c, "GET", ep+"/api/quick-archive/files?"+q.Encode(), nil)
}

// PersonalArchiveFilesList GET /centralized-projects/personal-archive-files?level=core|important|general
// 列出本机「个人{级别}文件夹」下的一键归档文件（供「档案在线阅卷·个人」展示）。
func PersonalArchiveFilesList(c *gin.Context) {
	db := repository.GetDB()
	cfg := repository.NewSystemConfigRepository(db)
	personalRoot := strings.TrimSpace(cfg.GetValue(repository.KeyPersonalArchiveRoot))
	if personalRoot == "" {
		personalRoot = strings.TrimSpace(cfg.GetEffectiveProjectRoot())
	}
	level := strings.TrimSpace(c.Query("level"))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": repository.ListPersonalArchiveFiles(personalRoot, level)})
}

// QuickArchiveAllCentralized POST /centralized-projects/quick-archive-all
// 归档当前用户提交的全部已发布（非草稿）集中立项项目。
func QuickArchiveAllCentralized(c *gin.Context) {
	db := repository.GetDB()
	root := strings.TrimSpace(repository.NewSystemConfigRepository(db).GetEffectiveProjectRoot())
	if root == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置工作空间目录"})
		return
	}
	operator := currentOperator(c)
	var rows []struct {
		RemoteID int64 `db:"manage_remote_id"`
	}
	if err := db.Select(&rows, `
		SELECT manage_remote_id FROM centralized_project_applications
		WHERE disable = 0 AND submitted_by = ? AND status != 'draft' AND manage_remote_id IS NOT NULL`, operator); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	var summaries []quickArchiveProjectSummary
	totalArchived, totalSkipped := 0, 0
	for _, r := range rows {
		ctx, cerr := resolveCentralizedArchiveCtx(db, r.RemoteID)
		if cerr != nil {
			continue
		}
		res, aerr := repository.ArchiveProjectByScope(db, root, ctx.dir, ctx.name, ctx.scope, ctx.sensitivity, operator, ctx.custodyScope, ctx.custodyNote)
		s := quickArchiveProjectSummary{
			ProjectCode: ctx.dir, ProjectName: ctx.name, Scope: ctx.scope,
			Route: res.RouteTip, Archived: res.Archived, Skipped: res.Skipped, Errors: res.Errors,
		}
		if aerr != nil {
			s.Errors = append(s.Errors, aerr.Error())
		}
		totalArchived += res.Archived
		totalSkipped += res.Skipped
		summaries = append(summaries, s)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"total_archived": totalArchived,
		"total_skipped":  totalSkipped,
		"projects":       summaries,
	}})
}
