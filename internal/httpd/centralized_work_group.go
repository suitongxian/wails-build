package httpd

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 工作组代理（2026-06-11）：scan 透传 manage 的「我参与的项目」与「工作组详情」只读端点。

// InvolvedProjectsProxy GET /centralized-projects/involved —— 我参与的项目（username=operator）。
func InvolvedProjectsProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/involved?username=%s", ep, encodeQuery(currentOperator(c))), nil)
}

// WorkGroupProxy GET /centralized-projects/work-group?application_id= —— 工作组详情。
func WorkGroupProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/work-group?application_id=%s", ep, encodeQuery(c.Query("application_id"))), nil)
}
