package httpd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// RequireProjectAction 要求当前操作人在 :id 指向的项目里有 action 权限
//
// 路由参数为 :id（项目 id）。中间件在路由处使用：
//
//	r.POST("/:id/upload", RequireProjectAction("write"), UploadFileVersion)
//
// 失败返回 403。
func RequireProjectAction(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || projectID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid project id"})
			return
		}
		operator := currentOperator(c)
		svc := repository.NewProjectAuthService(repository.GetDB())
		if err := svc.CheckProjectAction(operator, projectID, action); err != nil {
			if repository.IsPermissionDenied(err) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "error": err.Error(), "code": "PERMISSION_DENIED"})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.Next()
	}
}

// RequireFileVersionProjectAction 通过 file_version 反查项目，再做权限校验
//
// 路由参数为 :id（file_version id）。用于 /file-versions/:id/* 系列受保护操作。
func RequireFileVersionProjectAction(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		fvID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || fvID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid file version id"})
			return
		}
		fvRepo := repository.NewFileVersionRepository(repository.GetDB())
		fv, err := fvRepo.FindByID(fvID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"success": false, "error": "file version not found"})
			return
		}
		operator := currentOperator(c)
		svc := repository.NewProjectAuthService(repository.GetDB())
		if err := svc.CheckProjectStageAction(operator, fv.ProjectID, fv.ProjectStageID, action); err != nil {
			if repository.IsPermissionDenied(err) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "error": err.Error(), "code": "PERMISSION_DENIED"})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.Next()
	}
}

// RequireFileVersionReceiveAction 校验下游领取动作的目标环节权限。
//
// receive 路由里的 :id 是上游产出 file_version，真正被操作的是请求体里的
// target_stage_id。领取权限必须按目标环节校验，否则下游负责人会被上游环节
// 权限错误拦截。
func RequireFileVersionReceiveAction() gin.HandlerFunc {
	return func(c *gin.Context) {
		fvID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || fvID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid file version id"})
			return
		}
		fvRepo := repository.NewFileVersionRepository(repository.GetDB())
		fv, err := fvRepo.FindByID(fvID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"success": false, "error": "file version not found"})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		var req ReceiveAsInputRequest
		if err := json.Unmarshal(body, &req); err != nil || req.TargetStageID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid target stage"})
			return
		}

		operator := currentOperator(c)
		svc := repository.NewProjectAuthService(repository.GetDB())
		if err := svc.CheckProjectStageAction(operator, fv.ProjectID, req.TargetStageID, "receive"); err != nil {
			if repository.IsPermissionDenied(err) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "error": err.Error(), "code": "PERMISSION_DENIED"})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.Next()
	}
}

// RequireFamilyBatchArchiveAction 校验 family 批量归目目标环节权限。
//
// batch-archive 不是以 file_version id 为入口，目标项目/环节来自 JSON body。
// 因此必须在进入 handler 前解析 body，并按 project_id + stage_code 校验写入权限。
// 分流模式还需要同时校验 final_stage_code。
func RequireFamilyBatchArchiveAction() gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		var req BatchArchiveFamilyRequest
		if err := json.Unmarshal(body, &req); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body"})
			return
		}
		if req.ProjectID == 0 || req.StageCode == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "project_id / stage_code 必填"})
			return
		}

		stageIDs := []int64{}
		stageID, ok := projectStageIDByCode(c, req.ProjectID, req.StageCode)
		if !ok {
			return
		}
		stageIDs = append(stageIDs, stageID)
		if req.FinalStageCode != "" && req.FinalFileRuleCode != "" && req.FinalStageCode != req.StageCode {
			finalStageID, ok := projectStageIDByCode(c, req.ProjectID, req.FinalStageCode)
			if !ok {
				return
			}
			stageIDs = append(stageIDs, finalStageID)
		}

		operator := currentOperator(c)
		svc := repository.NewProjectAuthService(repository.GetDB())
		for _, id := range stageIDs {
			if err := svc.CheckProjectStageAction(operator, req.ProjectID, id, "write"); err != nil {
				if repository.IsPermissionDenied(err) {
					c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "error": err.Error(), "code": "PERMISSION_DENIED"})
					return
				}
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
				return
			}
		}
		c.Next()
	}
}

func projectStageIDByCode(c *gin.Context, projectID int64, stageCode string) (int64, bool) {
	var stageID int64
	if err := repository.GetDB().Get(&stageID, `SELECT id FROM project_stages WHERE project_id = ? AND stage_code = ? AND disable = 0`, projectID, stageCode); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid target stage"})
		return 0, false
	}
	return stageID, true
}

// RequireLedgerProjectAction 通过 ledger 反查项目，再做权限校验
//
// 路由参数为 :id（ledger id）。用于 /ledgers/:id/* 受保护操作。
func RequireLedgerProjectAction(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ledgerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || ledgerID == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid ledger id"})
			return
		}
		ledgerRepo := repository.NewAssetLedgerRepository(repository.GetDB())
		ledger, err := ledgerRepo.FindByID(ledgerID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"success": false, "error": "ledger not found"})
			return
		}
		// 通过 project_code 找 project_id
		var projectID int64
		if err := repository.GetDB().Get(&projectID, `SELECT id FROM data_projects WHERE project_code = ? AND disable = 0`, ledger.ProjectCode); err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"success": false, "error": "project for ledger not found"})
			return
		}
		operator := currentOperator(c)
		svc := repository.NewProjectAuthService(repository.GetDB())
		if err := svc.CheckProjectAction(operator, projectID, action); err != nil {
			if repository.IsPermissionDenied(err) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "error": err.Error(), "code": "PERMISSION_DENIED"})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.Next()
	}
}
