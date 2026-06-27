package httpd

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/ai"
	"data-asset-scan-go/internal/repository"
	"data-asset-scan-go/internal/textextract"
)

// V4-Q1-b §4.3 AI 归目工具 HTTP 端点
//
// 简版 §4.3 列出 5 个子功能，本提交实现 3 个核心端点：
//   - GET  /ai/classify/suggestions?resource_id=  — 拉单条资源的归目建议
//   - GET  /ai/classify/pending                   — 列未归目资源 + 各自建议
//   - POST /ai/classify/apply                     — 应用某建议（挂账）
//
// 注意：自动归目（高置信度直接挂）由客户端按 confidence 阈值决定，
// 服务端只暴露"列建议 + 应用"，把策略选择留给前端 UI。

// RegisterAIClassifyRoutes 注册 /ai/classify 路由
func RegisterAIClassifyRoutes(r *gin.RouterGroup) {
	r.GET("/suggestions", GetClassifySuggestions)
	r.GET("/pending", ListPendingForClassify)
	r.POST("/apply", ApplyClassifySuggestion)
	r.POST("/reject", RejectClassifySuggestion)
	r.POST("/bulk-dismiss", BulkDismissClassify)
}

// classifyAdapter 模块级单例（每次请求复用同一个 catalog provider）
//
// CatalogProvider 内部走 sqlx 查询；只读路径，无状态，线程安全。
func classifyAdapter() *ai.RuleBasedClassifyAdapter {
	provider := repository.NewDBCatalogProvider(repository.GetDB())
	return ai.NewRuleBasedClassifyAdapter(provider)
}

// GetClassifySuggestions GET /ai/classify/suggestions?resource_id=42&top_n=5
//
// 返回某 data_resource 的 AI 归目建议列表（按 confidence 降序）。
//
// V5-Phase3 §4.4 起：用 ai.EnrichInputForResource 注入完整元数据（mime / ext /
// sibling_count / parent_dir 等），并在本地路径存在时调 textextract 读首 200
// 字（按 rune 截）填入 Summary，给评分函数 (Task 3) 更丰富的信号。
func GetClassifySuggestions(c *gin.Context) {
	resourceID, err := strconv.ParseInt(c.Query("resource_id"), 10, 64)
	if err != nil || resourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "resource_id 必填"})
		return
	}

	db := repository.GetDB()
	in, err := ai.EnrichInputForResource(db, resourceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "资源不存在: " + err.Error()})
		return
	}
	// 本地文件存在时读首 200 字注入 Summary（已注入的 Summary 不动）
	if in.Path != "" && in.Summary == "" {
		body := textextract.ExtractTextWithTimeout(in.Path, 2*time.Second)
		if body != "" {
			runes := []rune(body)
			if len(runes) > 200 {
				body = string(runes[:200])
			}
			in.Summary = body
		}
	}

	adapter := classifyAdapter()
	if v := c.Query("top_n"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			adapter.SetTopN(n)
		}
	}
	suggestions, err := adapter.Classify(context.Background(), in)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"resource_id": resourceID,
			"input":       in,
			"suggestions": suggestions,
		},
	})
}

// ListPendingForClassify GET /ai/classify/pending?origin=new|historical&page=1&page_size=20&min_confidence=0.6
//
// 列出待归目资源 (claim_status=2 且 importance_level=0)。
//   - origin=new（默认）：附带 AI 建议（与旧行为一致）。
//   - origin=historical：跳过 AI 建议（suggestions=[]），由前端按需展开走 /suggestions 端点。
//
// 响应包装：{ items, total, page, page_size }。
func ListPendingForClassify(c *gin.Context) {
	origin := c.DefaultQuery("origin", "new")
	if origin != "new" && origin != "historical" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "origin 只能是 new 或 historical"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	db := repository.GetDB()

	type pendingRow struct {
		ID            int64   `db:"data_resources_id"`
		ResourcesName *string `db:"resources_name"`
	}

	var total int
	if err := db.Get(&total,
		`SELECT COUNT(*) FROM data_resources
		  WHERE claim_status = 2 AND importance_level = 0 AND disable = 0
		    AND ai_classify_rejected_at IS NULL
		    AND data_origin = ?`, origin); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	offset := (page - 1) * pageSize
	var pending []pendingRow
	if err := db.Select(&pending,
		`SELECT data_resources_id, resources_name
		   FROM data_resources
		  WHERE claim_status = 2 AND importance_level = 0 AND disable = 0
		    AND ai_classify_rejected_at IS NULL
		    AND data_origin = ?
		  ORDER BY data_resources_id DESC LIMIT ? OFFSET ?`,
		origin, pageSize, offset); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	type itemOut struct {
		ResourceID   int64                         `json:"resource_id"`
		ResourceName string                        `json:"resource_name"`
		Suggestions  []ai.ClassificationSuggestion `json:"suggestions"`
	}
	items := make([]itemOut, 0, len(pending))

	if origin == "historical" {
		for _, p := range pending {
			items = append(items, itemOut{
				ResourceID:   p.ID,
				ResourceName: strDeref(p.ResourcesName),
				Suggestions:  []ai.ClassificationSuggestion{},
			})
		}
	} else {
		minConfidence := 0.0
		if v := c.Query("min_confidence"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				minConfidence = f
			}
		}
		adapter := classifyAdapter()
		for _, p := range pending {
			in, err := ai.EnrichInputForResource(db, p.ID)
			if err != nil {
				continue
			}
			if in.Path != "" && in.Summary == "" {
				body := textextract.ExtractTextWithTimeout(in.Path, 2*time.Second)
				if body != "" {
					runes := []rune(body)
					if len(runes) > 200 {
						body = string(runes[:200])
					}
					in.Summary = body
				}
			}
			sugs, _ := adapter.Classify(context.Background(), in)
			filtered := make([]ai.ClassificationSuggestion, 0, len(sugs))
			for _, s := range sugs {
				if s.Confidence >= minConfidence {
					filtered = append(filtered, s)
				}
			}
			items = append(items, itemOut{
				ResourceID:   p.ID,
				ResourceName: strDeref(p.ResourcesName),
				Suggestions:  filtered,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// ApplyClassifyRequest POST /ai/classify/apply
type ApplyClassifyRequest struct {
	ResourceID   int64  `json:"resource_id"`
	ProjectID    int64  `json:"project_id"`
	StageCode    string `json:"stage_code"`
	FileRuleCode string `json:"file_rule_code"`
}

// ApplyClassifySuggestion POST /ai/classify/apply
//
// 用户在 AI 建议确认界面选中某建议后调用此端点：
//   - 把 data_resource 挂到指定项目/环节/规则
//   - 调 BridgeResourceToTarget 复用 Q5 桥接通道
//   - 落 audit_logs
func ApplyClassifySuggestion(c *gin.Context) {
	var req ApplyClassifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.ResourceID == 0 || req.ProjectID == 0 || req.StageCode == "" || req.FileRuleCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "resource_id / project_id / stage_code / file_rule_code 全部必填"})
		return
	}

	projectCode := lookupProjectCode(req.ProjectID)

	// 三级分流：重要级 + 多源未确权 → 拦截让用户先选权威源
	if projectCode == repository.PersonalImportantProjectCode {
		needs, _ := repository.NeedsAuthoritativeArbitration(repository.GetDB(), req.ResourceID)
		if needs {
			var familyID int64
			_ = repository.GetDB().Get(&familyID,
				`SELECT family_id FROM data_resources WHERE data_resources_id = ?`, req.ResourceID)
			famRepo := repository.NewFamilyRepository(repository.GetDB())
			members, _ := famRepo.ListFamilyMembers(familyID)
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "需要先选权威源",
				"data": gin.H{
					"family_id": familyID,
					"members":   members,
				},
			})
			return
		}
	}

	item, err := repository.BridgeResourceToTarget(
		repository.GetDB(), req.ResourceID, req.ProjectID, req.StageCode, req.FileRuleCode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// 三级分流：apply 成功后把 importance_level 与目标项目代码同步（非个人项目则跳过）
	if projectCode != "" {
		_ = repository.SyncResourceImportance(repository.GetDB(), req.ResourceID, projectCode)
	}

	// V3-5 §11.1.4 文件操作审计（AI 归目本质是 file_version 创建）
	// V5-Phase1 §4.3-2 起改用 AuditAIClassifyApply 与 reject 对称，便于审计链筛选
	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditAIClassifyApply,
		TargetType:  repository.AuditTargetFileVersion,
		TargetID:    item.FileVersionID,
		TargetCode:  item.ResourceName,
		After:       gin.H{"resource_id": req.ResourceID, "project_id": req.ProjectID, "stage": req.StageCode, "rule": req.FileRuleCode, "status": item.Status},
		IPAddress:   c.ClientIP(),
		Message:     "AI 归目应用",
	})

	resp := gin.H{"success": true, "data": item}
	if projectCode == repository.PersonalCoreProjectCode {
		// 核心级 ledger 已建出但 memorandum_registered_at 仍为 NULL，前端应跳到核心登记待办页
		resp["hint"] = "transferred_to_memorandum_pending"
	}
	c.JSON(http.StatusOK, resp)
}

// RejectClassifyRequest POST /ai/classify/reject 入参
type RejectClassifyRequest struct {
	ResourceID int64  `json:"resource_id"`
	Reason     string `json:"reason"`
}

// RejectClassifySuggestion POST /ai/classify/reject
//
// 用户在 AI 建议确认界面"驳回"后调用此端点：
//   - 把 data_resource 标记为 ai_classify_rejected_at = now + reject_reason = 用户填写
//   - pending 列表后续会过滤掉该资源，避免反复出现
//   - 落 audit_logs（AuditAIClassifyReject）
func RejectClassifySuggestion(c *gin.Context) {
	var req RejectClassifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.ResourceID <= 0 || strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "resource_id 和 reason 必填"})
		return
	}

	// 资源存在性校验
	type drRow struct {
		Name *string `db:"resources_name"`
	}
	var dr drRow
	if err := repository.GetDB().Get(&dr,
		`SELECT resources_name FROM data_resources WHERE data_resources_id = ? AND disable = 0`,
		req.ResourceID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "资源不存在: " + err.Error()})
		return
	}

	now := time.Now()
	if _, err := repository.GetDB().Exec(`UPDATE data_resources
		SET ai_classify_rejected_at = ?, ai_classify_reject_reason = ?, update_time = ?
		WHERE data_resources_id = ?`,
		now, req.Reason, now, req.ResourceID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditAIClassifyReject,
		TargetType:  repository.AuditTargetDataResource,
		TargetID:    req.ResourceID,
		TargetCode:  strDeref(dr.Name),
		After:       gin.H{"resource_id": req.ResourceID, "reason": req.Reason, "rejected_at": now},
		IPAddress:   c.ClientIP(),
		Message:     "AI 归目驳回: " + req.Reason,
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"status":      "rejected",
			"resource_id": req.ResourceID,
			"rejected_at": now,
			"reason":      req.Reason,
		},
	})
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// lookupProjectCode 反查 project_id → project_code。失败返空串。
func lookupProjectCode(projectID int64) string {
	var code string
	_ = repository.GetDB().Get(&code, `SELECT project_code FROM data_projects WHERE id = ?`, projectID)
	return code
}

// BulkDismissClassifyRequest POST /ai/classify/bulk-dismiss 入参
type BulkDismissClassifyRequest struct {
	ResourceIDs []int64 `json:"resource_ids"`
	Reason      string  `json:"reason"`
}

// BulkDismissClassify POST /ai/classify/bulk-dismiss
//
// 批量把若干历史数据标记为"已人工治理（跳过 AI 归目）"：
//   - 仅接受 data_origin='historical' 的资源；任何不符合都整批 400 回退
//   - 校验通过后 UPDATE + 逐条 audit_logs
func BulkDismissClassify(c *gin.Context) {
	var req BulkDismissClassifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if len(req.ResourceIDs) == 0 || len(req.ResourceIDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "resource_ids 必须包含 1~500 个 id"})
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "reason 必填"})
		return
	}

	db := repository.GetDB()

	type validRow struct {
		ID   int64   `db:"data_resources_id"`
		Name *string `db:"resources_name"`
	}
	placeholders := make([]string, len(req.ResourceIDs))
	args := make([]interface{}, len(req.ResourceIDs))
	for i, id := range req.ResourceIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	selectQ := `SELECT data_resources_id, resources_name FROM data_resources
	            WHERE data_resources_id IN (` + strings.Join(placeholders, ",") + `)
	              AND disable = 0
	              AND (
	                  data_origin = 'historical'
	                  OR (data_origin = 'new' AND importance_level = 3)
	              )`
	var rows []validRow
	if err := db.Select(&rows, selectQ, args...); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if len(rows) != len(req.ResourceIDs) {
		seen := make(map[int64]bool, len(rows))
		for _, r := range rows {
			seen[r.ID] = true
		}
		invalid := make([]int64, 0)
		for _, id := range req.ResourceIDs {
			if !seen[id] {
				invalid = append(invalid, id)
			}
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "存在非历史、非一般级新数据 / 已删除 / 不存在的资源",
			"data":    gin.H{"invalid_ids": invalid},
		})
		return
	}

	now := time.Now()
	updateQ := `UPDATE data_resources
	            SET ai_classify_rejected_at = ?, ai_classify_reject_reason = ?, update_time = ?
	            WHERE data_resources_id IN (` + strings.Join(placeholders, ",") + `)
	              AND disable = 0
	              AND (
	                  data_origin = 'historical'
	                  OR (data_origin = 'new' AND importance_level = 3)
	              )`
	updArgs := append([]interface{}{now, req.Reason, now}, args...)
	if _, err := db.Exec(updateQ, updArgs...); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	auditRepo := repository.NewAuditLogRepository(db)
	for _, row := range rows {
		_, _ = auditRepo.Append(repository.AppendAuditInput{
			ActorID:     currentOperator(c),
			ActorUserID: currentUserID(c),
			Action:      repository.AuditAIClassifyReject,
			TargetType:  repository.AuditTargetDataResource,
			TargetID:    row.ID,
			TargetCode:  strDeref(row.Name),
			After:       gin.H{"resource_id": row.ID, "reason": req.Reason, "rejected_at": now, "bulk": true},
			IPAddress:   c.ClientIP(),
			Message:     "AI 归目批量标已治理: " + req.Reason,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"dismissed": len(rows)},
	})
}
