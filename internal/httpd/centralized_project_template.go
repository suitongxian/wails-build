package httpd

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// 立项过程中编辑「项目专属模版」：项目负责人/环节责任人对本项目的工作事项、文件任务、文档标识
// 增删改，都落在该项目专属模版（TPL-PRJ-<application_id>）上，不污染共享模版；保存时整树回灌 manage。
//
// 编辑本身复用通用的 /template-stages、/template-tasks、/template-file-rules CRUD（针对返回的
// template_id 操作）。本文件提供两个项目级编排接口：
//   - GET  /centralized-projects/project-template?application_id=  载入(必要时从 manage 拉成可编辑副本)
//   - POST /centralized-projects/save-project-template?application_id=  保存：整树回灌 manage

// ProjectTemplate GET /centralized-projects/project-template?application_id=
// 返回本项目可编辑的项目专属模版：{ template_id, template_code, tree }。
func ProjectTemplate(c *gin.Context) {
	appID := strings.TrimSpace(c.Query("application_id"))
	if appID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 application_id"})
		return
	}
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	authRepo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	localID, err := authRepo.EnsureEditableProjectTemplate(nil, ep, appID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	tree, err := authRepo.GetLocalTemplateTree(localID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"template_id":   localID,
		"template_code": tree.Template.TemplateCode,
		"tree":          tree,
	}})
}

// ExtractCertifiedTemplate POST /centralized-projects/extract-template?application_id=&project_code=
// 把本项目（立项过程中编辑过的）项目专属模版「提取」为单位「项目认定模版」：以 manage 上的最终结构
// 为准另存为一份新本地模版（certified=1、已发布、记录来源项目），直接生效为权威，无需发布/审批。
//
// 门禁全部走云端（manage 基线 vs 最终结构），不依赖本地 DB——支持多端 / 重建本地库后仍可提取：
//   - 有基线且确有改动 → 允许；
//   - 有基线但无改动   → 拒绝（无需提取）；
//   - 无基线（老项目） → 允许（兜底，按最终结构提取）。
func ExtractCertifiedTemplate(c *gin.Context) {
	appID := strings.TrimSpace(c.Query("application_id"))
	if appID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 application_id"})
		return
	}
	projectCode := strings.TrimSpace(c.Query("project_code"))
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	authRepo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	changes, hasBaseline, derr := authRepo.DiffProjectTemplate(nil, ep, appID)
	if derr != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": derr.Error()})
		return
	}
	if hasBaseline && len(changes) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "本项目立项过程中未改动模版结构，无需提取"})
		return
	}
	newID, err := authRepo.ExtractCertifiedTemplate(nil, ep, appID, projectCode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"template_id": newID}})
}

// TemplateDiff GET /centralized-projects/template-diff?application_id=
// 计算本项目专属模版相对「关联模版时所选原始模版」基线的改动清单（工作环节/文件任务/文件标识三级），
// 供「提取项目模版」前的确认弹窗逐条展示。纯云端对比（manage 基线 vs 最终结构），不依赖本地 DB。
func TemplateDiff(c *gin.Context) {
	appID := strings.TrimSpace(c.Query("application_id"))
	if appID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 application_id"})
		return
	}
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	authRepo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	changes, hasBaseline, err := authRepo.DiffProjectTemplate(nil, ep, appID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"changes": changes, "has_baseline": hasBaseline}})
}

// SaveProjectTemplate POST /centralized-projects/save-project-template?application_id=
// 把本项目专属模版的当前结构整树回灌 manage（按 code+version 幂等替换），让其它角色/机器看到最新结构。
func SaveProjectTemplate(c *gin.Context) {
	appID := strings.TrimSpace(c.Query("application_id"))
	if appID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 application_id"})
		return
	}
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	authRepo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	localID, err := authRepo.EnsureEditableProjectTemplate(nil, ep, appID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	manageID, err := authRepo.PushTemplateToManage(nil, ep, localID, false, "")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"template_id": localID, "manage_template_id": manageID}})
}
