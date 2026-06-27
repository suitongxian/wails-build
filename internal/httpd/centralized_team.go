package httpd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 团队代理（2026-06-11 先组队再分工）：scan 透传 manage 的承接 / 项目团队 / 环节团队端点，
// 注入当前登录用户作为 acceptor/actor，避免前端伪造身份。

// AcceptProjectProxy POST /centralized-projects/accept-project?id= —— 承接（approved→taken）。
// 承接时前端可带 output_custody_scope（项目过程文件管理模式，仅单位级项目可选 department）
// 以及 cycle_start / cycle_end（项目周期·计划起止日期，回显给立项人），
// 这里读前端 body 合并后再注入 acceptor 转发，由 manage 落库（整 body 透传，新增字段无需改动）。
func AcceptProjectProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	if body == nil {
		body = map[string]any{}
	}
	body["acceptor"] = currentOperator(c)
	b, _ := json.Marshal(body)
	proxyToManage(c, "POST", fmt.Sprintf("%s/api/centralized-projects/accept-project?id=%s", ep, encodeQuery(c.Query("id"))), b)
}

// TeamGetProxy GET /centralized-projects/team?application_id= —— 读项目团队。
func TeamGetProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/team?application_id=%s", ep, encodeQuery(c.Query("application_id"))), nil)
}

// TeamPostProxy POST /centralized-projects/team?application_id= —— 组建项目团队（actor=operator）。
func TeamPostProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	if body == nil {
		body = map[string]any{}
	}
	body["actor"] = currentOperator(c)
	b, _ := json.Marshal(body)
	proxyToManage(c, "POST", fmt.Sprintf("%s/api/centralized-projects/team?application_id=%s", ep, encodeQuery(c.Query("application_id"))), b)
}

// StageTeamGetProxy GET /centralized-projects/stage-team?application_id=&stage_code= —— 读环节团队。
func StageTeamGetProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/stage-team?application_id=%s&stage_code=%s",
		ep, encodeQuery(c.Query("application_id")), encodeQuery(c.Query("stage_code"))), nil)
}

// StageTeamPostProxy POST /centralized-projects/stage-team?application_id=&stage_code= —— 组建环节团队（actor=operator）。
func StageTeamPostProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	if body == nil {
		body = map[string]any{}
	}
	body["actor"] = currentOperator(c)
	b, _ := json.Marshal(body)
	proxyToManage(c, "POST", fmt.Sprintf("%s/api/centralized-projects/stage-team?application_id=%s&stage_code=%s",
		ep, encodeQuery(c.Query("application_id")), encodeQuery(c.Query("stage_code"))), b)
}
