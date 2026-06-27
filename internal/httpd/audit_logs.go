package httpd

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// RegisterAuditLogsRoutes 注册 /audit-logs 路由
//
// §11 模块级审计日志的对外查询入口；写入由各业务路径主动调用 AuditLogRepository.Append。
func RegisterAuditLogsRoutes(r *gin.RouterGroup) {
	r.GET("", SearchAuditLogs)
	r.GET("/target", ListAuditLogsByTarget)
}

// SearchAuditLogs GET /audit-logs?action=&target_type=&actor_id=&limit=
func SearchAuditLogs(c *gin.Context) {
	in := repository.AuditSearchInput{
		Action:     c.Query("action"),
		TargetType: c.Query("target_type"),
		ActorID:    c.Query("actor_id"),
	}
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			in.Limit = n
		}
	}
	repo := repository.NewAuditLogRepository(repository.GetDB())
	list, err := repo.Search(in)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// ListAuditLogsByTarget GET /audit-logs/target?target_type=project&target_id=42
//
// 用于"在某个对象详情页显示其审计链"的场景。
func ListAuditLogsByTarget(c *gin.Context) {
	targetType := c.Query("target_type")
	if targetType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "target_type 必填"})
		return
	}
	targetID, err := strconv.ParseInt(c.Query("target_id"), 10, 64)
	if err != nil || targetID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "target_id 无效"})
		return
	}
	repo := repository.NewAuditLogRepository(repository.GetDB())
	list, err := repo.ListByTarget(targetType, targetID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}
