package httpd

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"data-asset-scan-go/internal/repository"
	"github.com/gin-gonic/gin"
)

// RegisterResourcesRoutes registers /resources routes
func RegisterResourcesRoutes(r *gin.RouterGroup) {
	r.GET("", GetResources)
	r.GET("/statistics", GetResourcesStatistics)
	r.GET("/suspect-summary", GetSuspectSummary) // 2026-05-27 一键忽略疑似非个人文件 - 预览
	r.POST("/claim", BatchClaimResources)
	r.POST("/ignore-all-suspect", IgnoreAllSuspect) // 2026-05-27 一键忽略疑似非个人文件 - 执行
	r.POST("/classify", BatchClassifyResources)
	r.POST("/classify/single", SingleClassifyResource)
	r.POST("/families/:id/batch-archive", RequireFamilyBatchArchiveAction(), BatchArchiveFamily) // V4-Q5 family 批量归目挂账
	r.POST("/:id/importance", OverrideResourceImportance)
}

// IgnoreSuspectParams 一键忽略 endpoint 的请求体
type IgnoreSuspectParams struct {
	BusinessType      string `json:"businessType"` // workspace / new_access / history_inventory / 空
	FullInventoryTime string `json:"fullInventoryTime"`
	ClaimantName      string `json:"claimant_name"`
	ClaimantUnit      string `json:"claimant_unit"`
}

func buildSuspectFilter(businessType, fullInventoryTime string) repository.SuspectFilter {
	var f repository.SuspectFilter
	if businessType != "" {
		bt := businessType
		f.BusinessType = &bt
	}
	if fullInventoryTime != "" {
		fit := fullInventoryTime
		f.FullInventoryTime = &fit
	}
	return f
}

// GetSuspectSummary handles GET /resources/suspect-summary
// 返回当前 tab 范围内未认领的疑似非个人文件数 + 前 10 条样本路径，
// 给前端横幅显示 + 弹一键确认对话框用。
func GetSuspectSummary(c *gin.Context) {
	filter := buildSuspectFilter(c.Query("businessType"), c.Query("fullInventoryTime"))
	repo := repository.NewDataResourcesRepository(repository.GetDB(), 100)
	count, samples, err := repo.SuspectSummary(filter, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"count":        count,
			"sample_paths": samples,
		},
	})
}

// IgnoreAllSuspect handles POST /resources/ignore-all-suspect
// 把当前 tab 范围内未认领的疑似非个人文件批量置 claim_status=4（已忽略）。
// 行为等价于用户挨个点「标为已忽略」。
func IgnoreAllSuspect(c *gin.Context) {
	var params IgnoreSuspectParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request body"})
		return
	}
	if params.ClaimantName == "" || params.ClaimantUnit == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing required fields: claimant_name, claimant_unit"})
		return
	}
	filter := buildSuspectFilter(params.BusinessType, params.FullInventoryTime)
	repo := repository.NewDataResourcesRepository(repository.GetDB(), 100)
	updated, err := repo.IgnoreAllSuspect(filter, params.ClaimantName, params.ClaimantUnit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"updatedCount": updated},
		"message": "已忽略 " + strconv.Itoa(updated) + " 条疑似非个人资源",
	})
}

// ClaimParams represents batch claim parameters
type ClaimParams struct {
	IDs          []int64 `json:"ids"`
	IsClaimed    int     `json:"is_claimed"`
	ClaimStatus  int     `json:"claim_status"`
	ClaimantName string  `json:"claimant_name"`
	ClaimantUnit string  `json:"claimant_unit"`
}

// ClassifyParams represents batch classify parameters
type ClassifyParams struct {
	IDs             []int64 `json:"ids"`
	ImportanceLevel int     `json:"importance_level"`
}

// SingleClassifyParams represents single classify parameters
type SingleClassifyParams struct {
	DataResourcesID int64  `json:"data_resources_id"`
	ImportanceLevel int    `json:"importance_level"`
	ResourcesName   string `json:"resources_name,omitempty"`
	ResourcesDesc   string `json:"resources_desc,omitempty"`
	ContentSubject  string `json:"content_subject,omitempty"`
}

// GetResources handles GET /resources
// Query params: page, pageSize, claimStatusFilter, claimStatusIn, importanceLevelFilter, businessTypeFilter, search
func GetResources(c *gin.Context) {
	page, _ := strconv.Atoi(defaultStr(c.Query("page"), "1"))
	pageSize, _ := strconv.Atoi(defaultStr(c.Query("pageSize"), "50"))

	queryParams := repository.DataResourcesQueryParams{
		Page:     page,
		PageSize: pageSize,
	}

	if v := c.Query("claimStatusFilter"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			n2 := n
			queryParams.ClaimStatusFilter = &n2
		}
	}
	if v := c.Query("claimStatusIn"); v != "" {
		queryParams.ClaimStatusIn = parseIntList(v)
	}
	if v := c.Query("importanceLevelFilter"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			n2 := n
			queryParams.ImportanceLevelFilter = &n2
		}
	}
	if v := c.Query("businessTypeFilter"); v != "" {
		queryParams.BusinessTypeFilter = &v
	}
	if v := c.Query("search"); v != "" {
		queryParams.Search = &v
	}
	// Default groupByFamily=true so the claim page shows the folded view; a
	// caller can pass groupByFamily=false explicitly to get the raw hash-group view.
	if v := c.Query("groupByFamily"); v == "" || v == "true" || v == "1" {
		queryParams.GroupByFamily = true
	}

	// businessTypeFilter=new_access/history_inventory 需要 full_inventory_time 作分界点
	// 之前漏注入，导致 repository 那段过滤永远短路，三个 tab 拿到同一份数据
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	if fit := configRepo.GetFullInventoryTime(); fit != "" {
		queryParams.FullInventoryTime = &fit
	}

	resourcesRepo := repository.NewDataResourcesRepository(repository.GetDB(), 100)
	result := resourcesRepo.GetResourcesWithPagination(queryParams)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// GetResourcesStatistics handles GET /resources/statistics
func GetResourcesStatistics(c *gin.Context) {
	resourcesRepo := repository.NewDataResourcesRepository(repository.GetDB(), 100)
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())

	fullInventoryTime := configRepo.GetFullInventoryTime()

	var fitPtr *string
	if fullInventoryTime != "" {
		fitPtr = &fullInventoryTime
	}

	statistics := resourcesRepo.GetResourcesStatistics(fitPtr)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    statistics,
	})
}

// BatchClaimResources handles POST /resources/claim
func BatchClaimResources(c *gin.Context) {
	var params ClaimParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request body"})
		return
	}

	if len(params.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing or invalid required field: ids"})
		return
	}
	if params.IsClaimed == 0 && params.ClaimStatus == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing required fields: is_claimed, claim_status"})
		return
	}
	if params.ClaimantName == "" || params.ClaimantUnit == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing required fields: claimant_name, claimant_unit"})
		return
	}

	db := repository.GetDB()
	resourcesRepo := repository.NewDataResourcesRepository(db, 100)

	// Since v2026-05 the frontend dialog explicitly computes which family members
	// to include (primary + same_content by default). The backend honors exactly
	// the IDs passed — no server-side family expansion is applied.
	updatedCount := resourcesRepo.BatchClaim(repository.BatchClaimParams{
		IDs:          params.IDs,
		IsClaimed:    params.IsClaimed,
		ClaimStatus:  params.ClaimStatus,
		ClaimantName: params.ClaimantName,
		ClaimantUnit: params.ClaimantUnit,
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"updatedCount": updatedCount},
		"message": "成功认领 " + strconv.Itoa(updatedCount) + " 条资源",
	})
}

// BatchClassifyResources handles POST /resources/classify
func BatchClassifyResources(c *gin.Context) {
	var params ClassifyParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request body"})
		return
	}

	if len(params.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing or invalid required field: ids"})
		return
	}
	if params.ImportanceLevel < 0 || params.ImportanceLevel > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid importance_level value. Valid values: 0-4"})
		return
	}

	resourcesRepo := repository.NewDataResourcesRepository(repository.GetDB(), 100)
	updatedCount := resourcesRepo.BatchClassify(params.IDs, params.ImportanceLevel)

	// V5-P1.1 §4.3-6 family-aware 桥接：dedup family（同一 family 由首个成员触发即整族处理）
	db := repository.GetDB()
	seenFamilies := map[int64]bool{}
	bridgedCount := 0
	propagatedCount := 0
	for _, id := range params.IDs {
		var famID *int64
		_ = db.Get(&famID, `SELECT family_id FROM data_resources WHERE data_resources_id = ?`, id)
		if famID != nil && *famID > 0 && seenFamilies[*famID] {
			continue // 同一 family 在前一个成员触发时已整族处理
		}
		result, err := repository.BridgeClassifyWithFamilyPropagation(db, id)
		if err == nil {
			bridgedCount += result.BridgedCount
			propagatedCount += result.PropagatedCount
		}
		if result.HasFamily {
			seenFamilies[result.FamilyID] = true
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"updatedCount":    updatedCount,
			"bridgedCount":    bridgedCount,
			"propagatedCount": propagatedCount,
		},
		"message": "成功归类 " + strconv.Itoa(updatedCount) + " 条资源，挂账 " + strconv.Itoa(bridgedCount) + " 条到个人归目容器（家族传播 " + strconv.Itoa(propagatedCount) + " 条）",
	})
}

// SingleClassifyResource handles POST /resources/classify/single
func SingleClassifyResource(c *gin.Context) {
	var params SingleClassifyParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request body"})
		return
	}

	if params.DataResourcesID == 0 || params.ImportanceLevel == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing required fields: data_resources_id, importance_level"})
		return
	}

	validValues := []int{1, 2, 3, 5}
	valid := false
	for _, v := range validValues {
		if params.ImportanceLevel == v {
			valid = true
			break
		}
	}
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid importance_level value. Valid values: 1, 2, 3, 5"})
		return
	}

	resourcesRepo := repository.NewDataResourcesRepository(repository.GetDB(), 100)
	var rn, rd, cs *string
	if params.ResourcesName != "" {
		rn = &params.ResourcesName
	}
	if params.ResourcesDesc != "" {
		rd = &params.ResourcesDesc
	}
	if params.ContentSubject != "" {
		cs = &params.ContentSubject
	}
	err := resourcesRepo.ClassifyResource(params.DataResourcesID, params.ImportanceLevel, rn, rd, cs)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// 方案甲（认领归档保护实体化）：归类为核心/重要/一般时，把文件实体【复制】进本机
	// 「个人{级别}文件夹」，使「档案在线阅卷·个人」可见（与一键归档同一落点）。
	// 不予归目(5)不复制；复制失败仅 log，不阻塞归类主流程。复制不删原件。
	personalArchived := 0
	if sens := repository.ImportanceLevelToSensitivity(params.ImportanceLevel); sens != "" {
		cfg := repository.NewSystemConfigRepository(repository.GetDB())
		personalRoot := strings.TrimSpace(cfg.GetValue(repository.KeyPersonalArchiveRoot))
		if personalRoot == "" {
			personalRoot = strings.TrimSpace(cfg.GetEffectiveProjectRoot())
		}
		if info, infoErr := resourcesRepo.GetResourceArchiveInfo(params.DataResourcesID); infoErr != nil {
			_ = c.Error(infoErr)
		} else {
			group := info.ContentSubject
			if strings.TrimSpace(group) == "" {
				group = info.ResourcesName
			}
			archived, _, errs := repository.ArchiveResourceToPersonalFolder(personalRoot, group, info.PrimaryPath, sens)
			personalArchived = archived
			for _, e := range errs {
				_ = c.Error(fmt.Errorf("个人文件夹归档: %s", e))
			}
		}
	}
	personalTip := ""
	if personalArchived > 0 {
		personalTip = "，文件已归入个人文件夹"
	}

	// V5-P1.1 §4.3-6 family-aware 桥接：classify 完触发"家族传播 importance_level + 整族 split 归目"
	// 失败仅 log 不阻塞主流程
	result, bridgeErr := repository.BridgeClassifyWithFamilyPropagation(repository.GetDB(), params.DataResourcesID)
	if bridgeErr != nil {
		_ = c.Error(bridgeErr)
	}
	if result.LeadFvID > 0 {
		msg := "归类成功并已挂账到个人归目容器"
		if result.HasFamily {
			msg = fmt.Sprintf("归类成功，已传播到家族 %d 个成员并挂账 %d 条（含定稿/过程分流）", result.PropagatedCount, result.BridgedCount)
		}
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    msg + personalTip,
			"bridged_fv": result.LeadFvID,
			"result":     result,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "归类成功" + personalTip})
}

// BatchArchiveFamilyRequest V4-Q5 family 批量归目挂账入参
//
// V5-P1 Task9 §4.3-6 历史数据家族式自动归目分流：当 FinalStageCode + FinalFileRuleCode
// 同时非空时，进入"过程/定稿分流"模式（最新归 final，其余归 process）；
// 否则保持 V4-Q5 单目标行为。
type BatchArchiveFamilyRequest struct {
	ProjectID         int64  `json:"project_id"`
	StageCode         string `json:"stage_code"`
	FileRuleCode      string `json:"file_rule_code"`
	FinalStageCode    string `json:"final_stage_code"`
	FinalFileRuleCode string `json:"final_file_rule_code"`
}

// BatchArchiveFamily POST /resources/families/:id/batch-archive
//
// V4-Q5 §4.3.5 历史数据家族式自动归目（浅联动）。
// 把一个 family 的所有 member 批量挂到指定项目的指定环节文件规则下。
//
// 行为：
//   - family 内已挂账的 member（按 file_version_code 幂等键）跳过
//   - 其他 member 走单条挂账
//   - 返回每个 member 的状态详情
//
// V5-P1 Task9：若 final_stage_code + final_file_rule_code 同时给出，走"过程/定稿分流"
// 模式（最新一条归 final，其余归 process/stage_code）；否则走原单目标模式。
func BatchArchiveFamily(c *gin.Context) {
	familyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid family id"})
		return
	}
	var req BatchArchiveFamilyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.ProjectID == 0 || req.StageCode == "" || req.FileRuleCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "project_id / stage_code / file_rule_code 必填"})
		return
	}

	splitMode := req.FinalStageCode != "" && req.FinalFileRuleCode != ""

	var result *repository.FamilyBatchArchiveResult
	if splitMode {
		result, err = repository.BridgeFamilyToProjectSplit(
			repository.GetDB(), familyID, req.ProjectID,
			req.StageCode, req.FileRuleCode,
			req.FinalStageCode, req.FinalFileRuleCode,
		)
	} else {
		result, err = repository.BridgeFamilyToProject(
			repository.GetDB(), familyID, req.ProjectID, req.StageCode, req.FileRuleCode,
		)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// V3-5 §11.1.8 导出/归档审计（family 归目沿用既有审计类型）
	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	mode := "single"
	if splitMode {
		mode = "split"
	}
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditExportArchive,
		TargetType:  repository.AuditTargetProject,
		TargetID:    req.ProjectID,
		TargetCode:  result.ProjectCode,
		After:       result,
		IPAddress:   c.ClientIP(),
		Message: fmt.Sprintf("V4-Q5 family#%d 批量归目[%s]：%d 条挂账 / %d 跳过 / %d 错误",
			familyID, mode, result.Archived, result.SkippedAlready, result.Errors),
	})

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// Helper

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseIntList(s string) []int {
	var result []int
	for _, part := range split(s, ",") {
		if n, err := strconv.Atoi(trim(part)); err == nil {
			result = append(result, n)
		}
	}
	return result
}

func split(s, sep string) []string {
	var result []string
	for i := 0; i < len(s); {
		j := indexOf(s, sep, i)
		if j == -1 {
			result = append(result, s[i:])
			break
		}
		result = append(result, s[i:j])
		i = j + len(sep)
	}
	return result
}

func indexOf(s, substr string, start int) int {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	i, j := 0, len(s)-1
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j >= i && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
		j--
	}
	return s[i : j+1]
}

// OverrideResourceImportance POST /resources/:id/importance body {"level": 0|1|2|3|4}
//
// 用户手动指定级别。0 = 退回 pending；1/2/3 = 进对应通道；4 = 隐私旁路。
// 不触发 apply；只改 data_resources.importance_level。
func OverrideResourceImportance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body struct {
		Level int `json:"level"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if body.Level < 0 || body.Level > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "level 必须在 0~4 之间"})
		return
	}
	db := repository.GetDB()
	if _, err := db.Exec(`UPDATE data_resources SET importance_level = ?, update_time = ? WHERE data_resources_id = ? AND disable = 0`,
		body.Level, time.Now(), id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"resource_id": id, "level": body.Level}})
}
