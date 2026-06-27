package httpd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// RegisterFileVersionsRoutes 注册 /file-versions 路由
func RegisterFileVersionsRoutes(r *gin.RouterGroup) {
	r.GET("/:id", GetFileVersion)
	r.GET("/:id/security", GetFileVersionSecurity) // V4-Q4 §3.6 九宫格安全视图
	r.GET("/:id/events", ListFileVersionEvents)
	r.GET("/:id/chain", GetFileVersionChain)
	r.GET("/:id/ledger", GetFileVersionLedger)
	r.GET("/:id/source-distribution", GetFileVersionSourceDistribution) // V5-P1 Q3 桥接 fv 反查物理分布

	// 写操作受 F2 权限校验中间件保护
	r.POST("/:id/upload", RequireFileVersionProjectAction("write"), UploadFileVersion)             // D2 首次上传 (multipart)
	r.POST("/:id/bind", RequireFileVersionProjectAction("write"), BindFileVersion)                 // D2 绑定本机已有路径 (JSON)
	r.POST("/:id/new-version", RequireFileVersionProjectAction("write"), CreateNewVersion)         // D2 创建新版本
	r.POST("/:id/derive", RequireFileVersionProjectAction("write"), DeriveProcess)                 // D4 派生过程文件
	r.POST("/:id/submit", RequireFileVersionProjectAction("submit"), SubmitOutput)                 // D5 提交产出
	r.POST("/:id/sync-cabinet", RequireFileVersionProjectAction("submit"), SyncFileVersionCabinet) // V5-P5 重试上报部门柜
	r.POST("/:id/receive", RequireFileVersionReceiveAction(), ReceiveAsInput)                      // D5 下游领取（:id 是上游产出 fv）

	r.GET("/submittable", ListSubmittableOutputs) // 列出某项目下可领取的产出

	// V5-P1 Task 7 §4.3-4 项目版本文件手动归目归档（写操作，受 F2 权限校验中间件保护）
	r.POST("/:id/unbind", RequireFileVersionProjectAction("write"), UnbindFileVersionHandler)         // 解除绑定
	r.POST("/:id/reclassify", RequireFileVersionProjectAction("write"), ReclassifyFileVersionHandler) // 重新归类（解绑 + 新建到新目标）
}

// GetFileVersion GET /file-versions/:id
func GetFileVersion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewFileVersionRepository(repository.GetDB())
	fv, err := repo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": fv})
}

// GetFileVersionSecurity GET /file-versions/:id/security
//
// V4-Q4 §3.6 九宫格存储基线视图：返回该 fv 当前应处的存储位置 + 命中策略。
// 与 GET /:id（裸模型）区别：本端点把项目敏感等级、当前文件状态、storage_tier、
// 中文存储位置 label 全部聚合，前端可直接展示。
func GetFileVersionSecurity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	fvRepo := repository.NewFileVersionRepository(repository.GetDB())
	fv, err := fvRepo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	projRepo := repository.NewDataProjectRepository(repository.GetDB())
	proj, err := projRepo.FindByID(fv.ProjectID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "项目不存在: " + err.Error()})
		return
	}
	policyRepo := repository.NewSecurityPolicyRepository(repository.GetDB())
	info := repository.ResolveFileVersionSecurity(policyRepo, proj.SensitivityLevel, fv)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": info})
}

// ListFileVersionEvents GET /file-versions/:id/events
func ListFileVersionEvents(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewLifecycleEventRepository(repository.GetDB())
	list, err := repo.ListByFileVersion(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// GetFileVersionChain 返回某 fv 的来源/版本链
//
// 返回结构：
//
//	{ ancestors: [...], current: fv, descendants: [...] }
func GetFileVersionChain(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	db := repository.GetDB()
	repo := repository.NewFileVersionRepository(db)

	current, err := repo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	// 上溯 ancestors
	ancestors := []interface{}{}
	cursor := current
	for cursor != nil && cursor.SourceFileVersionID != nil {
		parent, err := repo.FindByID(*cursor.SourceFileVersionID)
		if err != nil {
			break
		}
		ancestors = append([]interface{}{parent}, ancestors...)
		cursor = parent
	}

	// 下溯 descendants
	descendants := []interface{}{}
	rows, err := db.Queryx(`SELECT * FROM file_versions WHERE source_file_version_id = ? AND disable = 0 ORDER BY create_time`, id)
	if err == nil {
		for rows.Next() {
			var fv map[string]interface{}
			fv = map[string]interface{}{}
			cols, _ := rows.Columns()
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err == nil {
				for i, col := range cols {
					fv[col] = vals[i]
				}
				descendants = append(descendants, fv)
			}
		}
		rows.Close()
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"current":     current,
		"ancestors":   ancestors,
		"descendants": descendants,
	}})
}

// GetFileVersionLedger GET /file-versions/:id/ledger
//
// 返回该文件版本对应的底账（如果已入账）。
func GetFileVersionLedger(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	l, err := repo.FindByFileVersion(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": l})
}

// GetFileVersionSourceDistribution GET /file-versions/:id/source-distribution
//
// V5-P1 Task Q3:
// 桥接 fv（BridgeClassifyToPersonalProject 等创建的）只填了 checksum，没填
// storage_uri；实际物理文件路径在 data_distributing 表里（通过 content_sign 关联）。
// 本端点用 fv.checksum 反查 data_distributing 返回路径 + 大小，让工作台不再
// 显示"尚未绑定实体文件"误导。
//
// Response:
//   - fv 不存在 → 404 + success=false
//   - fv 有 storage_uri 或没 checksum → 200 + data=null（前端各自处理）
//   - 有 checksum 但 distribution 查不到 → 200 + data={checksum} 仅
//   - 命中 → 200 + data={checksum, path, file_size, file_suffix, file_create_time}
func GetFileVersionSourceDistribution(c *gin.Context) {
	fvID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || fvID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	db := repository.GetDB()

	type fvRow struct {
		StorageURI *string `db:"storage_uri"`
		Checksum   *string `db:"checksum"`
	}
	var fv fvRow
	if err := db.Get(&fv, `SELECT storage_uri, checksum FROM file_versions WHERE id = ? AND disable = 0`, fvID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "fv not found"})
		return
	}

	// 已有 storage_uri（已直接绑定物理文件）→ 不需要反查
	if fv.StorageURI != nil && *fv.StorageURI != "" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
		return
	}
	// 没 checksum（典型 planned fv）→ 没法反查
	if fv.Checksum == nil || *fv.Checksum == "" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
		return
	}

	type distRow struct {
		Path           string  `db:"path"`
		FileSize       int64   `db:"file_size"`
		FileSuffix     *string `db:"file_suffix"`
		FileCreateTime *string `db:"file_create_time"`
	}
	var d distRow
	queryErr := db.Get(&d, `SELECT path, file_size, file_suffix, file_create_time FROM data_distributing
		WHERE content_sign = ? AND disable = 0 ORDER BY data_distribution_id LIMIT 1`, *fv.Checksum)
	if queryErr != nil {
		// 没找到分布 → 仅返回 checksum 不带 path
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{"checksum": *fv.Checksum},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"checksum":         *fv.Checksum,
			"path":             d.Path,
			"file_size":        d.FileSize,
			"file_suffix":      d.FileSuffix,
			"file_create_time": d.FileCreateTime,
		},
	})
}

// UploadFileVersion POST /file-versions/:id/upload
//
// multipart/form-data：file=<file>, [extras]=<JSON 字符串，可选>
func UploadFileVersion(c *gin.Context) {
	t0 := time.Now()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	log.Printf("[upload] fv=%d 开始接收 multipart", id)
	src, original, err := saveUploadedToTemp(c)
	if err != nil {
		log.Printf("[upload] fv=%d multipart 失败: %v", id, err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": FriendlyError(err, "解析上传请求失败")})
		return
	}
	defer os.Remove(src)
	log.Printf("[upload] fv=%d 接收完成 file=%s 耗时=%s", id, original, time.Since(t0))

	in := repository.UploadInput{
		SourcePath:       src,
		OriginalFileName: original,
		OperatorID:       currentOperator(c),
		OperatorUserID:   currentUserID(c),
		Extras:           parseExtras(c.PostForm("extras")),
	}
	svc := repository.NewFileOperationService(repository.GetDB())
	t1 := time.Now()
	res, err := svc.UploadOrBind(id, in)
	if err != nil {
		log.Printf("[upload] fv=%d UploadOrBind 失败 耗时=%s: %v", id, time.Since(t1), err)
		c.JSON(http.StatusOK, gin.H{"success": false, "error": FriendlyError(err, "保存文件失败")})
		return
	}
	log.Printf("[upload] fv=%d 入库完成 storage=%s 总耗时=%s", id, res.StoragePath, time.Since(t0))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// BindFileVersionRequest 绑定本机已有路径
type BindFileVersionRequest struct {
	SourcePath       string            `json:"source_path"`
	OriginalFileName string            `json:"original_file_name"`
	Extras           map[string]string `json:"extras"`
}

// BindFileVersion POST /file-versions/:id/bind
func BindFileVersion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req BindFileVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.SourcePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "source_path 必填"})
		return
	}
	if req.OriginalFileName == "" {
		req.OriginalFileName = filepath.Base(req.SourcePath)
	}
	svc := repository.NewFileOperationService(repository.GetDB())
	res, err := svc.UploadOrBind(id, repository.UploadInput{
		SourcePath:       req.SourcePath,
		OriginalFileName: req.OriginalFileName,
		OperatorID:       currentOperator(c),
		OperatorUserID:   currentUserID(c),
		Extras:           req.Extras,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// CreateNewVersion POST /file-versions/:id/new-version (multipart)
func CreateNewVersion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	src, original, err := saveUploadedToTemp(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer os.Remove(src)
	svc := repository.NewFileOperationService(repository.GetDB())
	res, err := svc.CreateNewVersion(id, repository.UploadInput{
		SourcePath:       src,
		OriginalFileName: original,
		OperatorID:       currentOperator(c),
		OperatorUserID:   currentUserID(c),
		Extras:           parseExtras(c.PostForm("extras")),
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// DeriveProcess POST /file-versions/:id/derive (multipart)
//
// multipart fields:
//   - file=<binary>
//   - target_stage_id (form)
//   - target_rule_code (form)
//   - target_version_no (form, optional)
//   - extras (form, optional JSON)
func DeriveProcess(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	stageID, err := strconv.ParseInt(c.PostForm("target_stage_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "target_stage_id 必填"})
		return
	}
	ruleCode := c.PostForm("target_rule_code")
	if ruleCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "target_rule_code 必填"})
		return
	}
	src, original, err := saveUploadedToTemp(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer os.Remove(src)

	svc := repository.NewFileOperationService(repository.GetDB())
	res, err := svc.DeriveProcess(id, repository.DeriveInput{
		UploadInput: repository.UploadInput{
			SourcePath:       src,
			OriginalFileName: original,
			OperatorID:       currentOperator(c),
			OperatorUserID:   currentUserID(c),
			Extras:           parseExtras(c.PostForm("extras")),
		},
		TargetStageID:   stageID,
		TargetRuleCode:  ruleCode,
		TargetVersionNo: c.PostForm("target_version_no"),
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// SubmitOutput POST /file-versions/:id/submit
func SubmitOutput(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	svc := repository.NewFileOperationService(repository.GetDB())
	fv, err := svc.SubmitOutput(id, currentOperator(c), currentUserID(c))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": fv})
}

// SyncFileVersionCabinet POST /file-versions/:id/sync-cabinet
//
// 重试将已提交 output 文件版本上报到 manage 端部门柜。
func SyncFileVersionCabinet(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	res, err := repository.NewManagedArchiveReporter(repository.GetDB()).ReportFileVersionToCabinet(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error(), "data": res})
		return
	}
	if res != nil && res.Status == "skipped" {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "未配置 manage 上报端点，已跳过", "data": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// ReceiveAsInputRequest 领取入参
type ReceiveAsInputRequest struct {
	TargetStageID  int64  `json:"target_stage_id"`
	TargetRuleCode string `json:"target_rule_code"`
}

// ReceiveAsInput POST /file-versions/:id/receive
//
// :id 是上游产出 fv id；body 指定下游环节 + 规则
func ReceiveAsInput(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req ReceiveAsInputRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.TargetStageID == 0 || req.TargetRuleCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "target_stage_id 和 target_rule_code 必填"})
		return
	}
	svc := repository.NewFileOperationService(repository.GetDB())
	res, err := svc.ReceiveAsInput(repository.ReceiveInput{
		SourceFileVersionID: id,
		TargetStageID:       req.TargetStageID,
		TargetRuleCode:      req.TargetRuleCode,
		OperatorID:          currentOperator(c),
		OperatorUserID:      currentUserID(c),
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// ListSubmittableOutputs GET /file-versions/submittable?project_id=
//
// 返回项目内所有已提交（submitted_at NOT NULL）的产出文件版本，可供下游领取
func ListSubmittableOutputs(c *gin.Context) {
	pid, err := strconv.ParseInt(c.Query("project_id"), 10, 64)
	if err != nil || pid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "project_id 必填"})
		return
	}
	db := repository.GetDB()
	rows, err := db.Queryx(`SELECT * FROM file_versions
		WHERE project_id = ? AND data_state = 'output' AND submitted_at IS NOT NULL AND disable = 0
		ORDER BY submitted_at DESC`, pid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer rows.Close()
	var list []map[string]interface{}
	for rows.Next() {
		m := map[string]interface{}{}
		_ = rows.MapScan(m)
		list = append(list, m)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// =============================================================================
// V5-P1 Task 7 §4.3-4 项目版本文件手动归目归档
// =============================================================================

// UnbindFileVersionHandler POST /file-versions/:id/unbind
//
// Body: {"reason": "..."}
//
// 把 fv + ledger 置 cancelled，落 reclassify_history(unbind) + lifecycle_event +
// audit_logs(AuditFvUnbind)。reason 必填，二次解绑失败。
//
// 命名带 Handler 后缀以与 repository.UnbindFileVersion（业务函数）区分。
func UnbindFileVersionHandler(c *gin.Context) {
	fvID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || fvID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "reason 必填"})
		return
	}

	op := buildOperatorSnapshot(c)
	if err := repository.UnbindFileVersion(repository.GetDB(), fvID, body.Reason, op); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditFvUnbind,
		TargetType:  repository.AuditTargetFileVersion,
		TargetID:    fvID,
		Message:     "解除绑定：" + body.Reason,
		IPAddress:   c.ClientIP(),
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"status": "unbound", "fv_id": fvID},
	})
}

// ReclassifyFileVersionHandler POST /file-versions/:id/reclassify
//
// Body: {"new_project_id": N, "new_stage_code": "...", "new_file_rule_code": "...", "reason": "..."}
//
// 把原 fv 解绑 + 桥接到新目标，返回新 fv id；落 audit_logs(AuditFvReclassify)
// 其 target 是原 fv id（保留原始审计入口），Message 含新 fv id 与 reason。
//
// 命名带 Handler 后缀以与 repository.ReclassifyFileVersion（业务函数）区分。
func ReclassifyFileVersionHandler(c *gin.Context) {
	fvID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || fvID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body struct {
		NewProjectID    int64  `json:"new_project_id"`
		NewStageCode    string `json:"new_stage_code"`
		NewFileRuleCode string `json:"new_file_rule_code"`
		Reason          string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid body"})
		return
	}

	op := buildOperatorSnapshot(c)
	newFvID, err := repository.ReclassifyFileVersion(repository.GetDB(), repository.ReclassifyInput{
		OriginalFvID:    fvID,
		NewProjectID:    body.NewProjectID,
		NewStageCode:    body.NewStageCode,
		NewFileRuleCode: body.NewFileRuleCode,
		Reason:          body.Reason,
		OperatorUser:    op,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditFvReclassify,
		TargetType:  repository.AuditTargetFileVersion,
		TargetID:    fvID,
		Message:     fmt.Sprintf("重新归类到 fv(%d)：%s", newFvID, body.Reason),
		IPAddress:   c.ClientIP(),
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"status":         "reclassified",
			"original_fv_id": fvID,
			"new_fv_id":      newFvID,
		},
	})
}

// buildOperatorSnapshot 从 gin context 构造 repository.UserSnapshot
//
// currentUserID 返回 0 表示无活跃 users 行（V1 兼容路径或未 setup）。
// UserSnapshot.UserID 为 *int64，所以 0 转 nil 以避免误写入 reclassify_history。
func buildOperatorSnapshot(c *gin.Context) *repository.UserSnapshot {
	var uidPtr *int64
	if uid := currentUserID(c); uid > 0 {
		v := uid
		uidPtr = &v
	}
	return &repository.UserSnapshot{
		UserID: uidPtr,
		Name:   currentOperator(c),
	}
}

// =============================================================================
// 辅助
// =============================================================================

// saveUploadedToTemp 把 multipart 上传的文件保存到临时路径并返回该路径与原始文件名
func saveUploadedToTemp(c *gin.Context) (string, string, error) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return "", "", err
	}
	src, err := fileHeader.Open()
	if err != nil {
		return "", "", err
	}
	defer src.Close()

	tmp, err := os.CreateTemp("", "upload-*"+filepath.Ext(fileHeader.Filename))
	if err != nil {
		return "", "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, src); err != nil {
		return "", "", err
	}
	return tmp.Name(), fileHeader.Filename, nil
}

func parseExtras(s string) map[string]string {
	if s == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// currentOperator 获取当前操作人标识（V1 用 user_info 表中的活跃用户名）
//
// V2 起：先尝试从 users 表读 username（V2-1 引入），找不到再 fallback 到
// user_info（V1 兼容）。最终结果仍是字符串以保持向后兼容；新代码可以
// 用 currentUserID() 直接拿 user.id。
func currentOperator(c *gin.Context) string {
	if username := currentSessionUsername(); username != "" {
		return username
	}
	usersRepo := repository.NewUserRepository(repository.GetDB())
	if u, err := usersRepo.GetActiveUser(); err == nil && u != nil {
		return u.Username
	}
	repo := repository.NewUserInfoRepository(repository.GetDB())
	if u, err := repo.GetActiveUser(); err == nil && u != nil {
		return u.UserName
	}
	return "system"
}

// currentUserID 获取当前登录用户的 users.id（V2 新代码用）
//
// 返回 0 表示没有活跃 user（V1 单用户模式且未 setup，或 fresh DB）。
// 上层应当根据返回值决定走 V2 严格路径还是 V1 兼容路径。
func currentUserID(c *gin.Context) int64 {
	usersRepo := repository.NewUserRepository(repository.GetDB())
	if username := currentSessionUsername(); username != "" {
		if u, err := usersRepo.FindByUsername(username); err == nil && u != nil {
			return u.ID
		}
		return 0
	}
	if u, err := usersRepo.GetActiveUser(); err == nil && u != nil {
		return u.ID
	}
	return 0
}

func currentSessionUsername() string {
	if s := activeSession(); s != nil {
		return s.User.Username
	}
	return ""
}
