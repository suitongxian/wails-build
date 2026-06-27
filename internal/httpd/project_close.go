package httpd

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// 这些路由挂在 /projects 组下（在 RegisterProjectsRoutes 之后单独追加）。

// ProjectCloseRequest 结项请求
type ProjectCloseRequest struct {
	Reason string `json:"reason"`
	Force  bool   `json:"force"`
}

// PrecheckProjectClose GET /projects/:id/close/precheck
func PrecheckProjectClose(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	svc := repository.NewProjectCloseService(repository.GetDB())
	res, err := svc.Precheck(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// CloseProject POST /projects/:id/close
func CloseProject(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req ProjectCloseRequest
	_ = c.ShouldBindJSON(&req)
	svc := repository.NewProjectCloseService(repository.GetDB())
	out, err := svc.Close(repository.CloseInput{
		ProjectID:      id,
		OperatorID:     currentOperator(c),
		OperatorUserID: currentUserID(c),
		Reason:         req.Reason,
		Force:          req.Force,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// V3-5 §11.1.2 项目结项审计
	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditProjectClose,
		TargetType:  repository.AuditTargetProject,
		TargetID:    out.Project.ID,
		TargetCode:  out.Project.ProjectCode,
		After:       out.Project,
		IPAddress:   c.ClientIP(),
		Message:     "结项归档：" + req.Reason,
	})

	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// SyncProjectArchive POST /projects/:id/sync
//
// 上报已结项项目的 manifest.json 到 manage。可重复调用做重试。
func SyncProjectArchive(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	pRepo := repository.NewDataProjectRepository(repository.GetDB())
	project, err := pRepo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "project not found"})
		return
	}
	root := ""
	if project.ProjectRoot != nil {
		root = *project.ProjectRoot
	}
	if root == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "项目根目录未设置，无 manifest 可上报"})
		return
	}
	manifestPath := root + "/manifest.json"

	uploader := repository.NewArchiveUploader(repository.GetDB())
	res, err := uploader.Upload(id, manifestPath)
	if err != nil {
		// markFailed 已写库；这里把详情透出
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error(), "data": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}
