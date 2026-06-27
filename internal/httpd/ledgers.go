package httpd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"

	"data-asset-scan-go/internal"
	"data-asset-scan-go/internal/models"
	"data-asset-scan-go/internal/repository"
)

// RegisterLedgersRoutes 注册 /ledgers 路由
//
// 数据资产标识底账（"账"）相关查询与状态切换。
func RegisterLedgersRoutes(r *gin.RouterGroup) {
	r.GET("", SearchLedgers)
	r.GET("/export.xlsx", ExportLedgersXLSX)            // 直接 stream 二进制（备用）
	r.GET("/export.csv", ExportLedgersCSV)              // V3-8 §7.5 / §8.4 CSV 格式
	r.GET("/export.json", ExportLedgersJSON)            // V3-8 §7.5 / §8.4 JSON 格式
	r.POST("/export-to-downloads", ExportLedgersToDisk) // 写入 ~/Downloads/ 返回路径（推荐 Wails 端使用）
	r.GET("/:id", GetLedger)
	r.GET("/:id/events", ListLedgerEvents)
	// 状态切换会改 file_versions/asset_ledgers 二者，要求 close 权限（与项目结项级动作同档）
	r.POST("/:id/transition", RequireLedgerProjectAction("close"), TransitionLedger)
	// V2-7: 过户（变更三主体之一），要求 share 权限
	r.POST("/:id/handover", RequireLedgerProjectAction("share"), HandoverLedger)
	// 2026-05-21 在本机用 OS 打开 ledger 关联的文件
	r.POST("/:id/open", OpenLedgerFile)
}

// SearchLedgers GET /ledgers
//
// 查询参数：
//   - project_code 按项目编码筛选
//   - stage_code 按工作环节筛选
//   - sensitivity_level 按敏感等级筛选
//   - owner_subject_id 按所有权人筛选
//   - lifecycle_status 按生命周期状态筛选
//   - keyword 模糊查询（资产名/底账编号/文件版本编码）
func SearchLedgers(c *gin.Context) {
	in := repository.LedgerSearchInput{
		ProjectCode:      c.Query("project_code"),
		StageCode:        c.Query("stage_code"),
		SensitivityLevel: c.Query("sensitivity_level"),
		LifecycleStatus:  c.Query("lifecycle_status"),
		Keyword:          c.Query("keyword"),
	}
	if v := c.Query("owner_subject_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.OwnerSubjectID = n
		}
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	rows, err := repo.Search(in)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// GetLedger GET /ledgers/:id
func GetLedger(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	l, err := repo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": l})
}

// ListLedgerEvents GET /ledgers/:id/events
func ListLedgerEvents(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewLifecycleEventRepository(repository.GetDB())
	list, err := repo.ListByLedger(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// TransitionLedgerRequest 状态切换入参
type TransitionLedgerRequest struct {
	ToStatus    string `json:"to_status"`
	Reason      string `json:"reason"`
	ApprovalRef string `json:"approval_ref"`
}

// ExportLedgersXLSX GET /ledgers/export.xlsx?...（同 SearchLedgers 的所有筛选参数）
//
// 用 excelize 生成 .xlsx 直接 stream 返回，浏览器/Wails WebView 都能正常下载。
// 加 ?include_drafts=1 才包含 planned 草稿，默认排除（与 UI 默认行为一致）。
func ExportLedgersXLSX(c *gin.Context) {
	in := repository.LedgerSearchInput{
		ProjectCode:      c.Query("project_code"),
		StageCode:        c.Query("stage_code"),
		SensitivityLevel: c.Query("sensitivity_level"),
		LifecycleStatus:  c.Query("lifecycle_status"),
		Keyword:          c.Query("keyword"),
	}
	if v := c.Query("owner_subject_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.OwnerSubjectID = n
		}
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	rows, err := repo.Search(in)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	// 默认过滤草稿（与前端 UI 默认行为一致）
	includeDrafts := c.Query("include_drafts") == "1"
	if !includeDrafts {
		filtered := rows[:0]
		for _, r := range rows {
			if r.LifecycleStatus != "planned" {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	f, err := buildLedgerXLSX(rows)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer f.Close()

	filename := fmt.Sprintf("ledgers-%s.xlsx", time.Now().Format("20060102-150405"))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Cache-Control", "no-cache")
	if err := f.Write(c.Writer); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
}

// buildLedgerXLSX 把 ledger 行装配成一个 excelize.File（不写盘也不发送）
//
// 共用逻辑：表头加粗、敏感等级/状态/标识方式中文化、列宽自适应、
// 存储路径列加宽。供 stream 与 save-to-disk 两个端点复用。
func buildLedgerXLSX(rows []models.AssetLedger) (*excelize.File, error) {
	f := excelize.NewFile()
	sheet := "底账"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		f.Close()
		return nil, err
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{"底账编号", "资产名称", "项目编码", "环节", "文件版本编码", "敏感等级", "标识方式", "生命周期", "存储位置", "创建时间"}
	for i, h := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetCellValue(sheet, col+"1", h)
	}
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E8EAF6"}, Pattern: 1},
	})
	_ = f.SetCellStyle(sheet, "A1", "J1", style)

	sensLabel := map[string]string{"general": "一般", "important": "重要", "core_secret": "核心(涉密)"}
	statusLabel := map[string]string{"planned": "草稿", "registered": "已入账", "in_use": "使用中", "sealed": "已封存", "destroyed": "已销账", "permanent": "永存"}
	markingLabel := map[string]string{"reference": "引用式", "embedded": "内嵌式", "hybrid": "混合式"}

	for i, r := range rows {
		row := i + 2
		set := func(col string, v interface{}) { _ = f.SetCellValue(sheet, col+strconv.Itoa(row), v) }
		set("A", r.LedgerCode)
		set("B", r.AssetName)
		set("C", r.ProjectCode)
		set("D", r.StageCode)
		set("E", r.FileVersionCode)
		if v, ok := sensLabel[r.SensitivityLevel]; ok {
			set("F", v)
		} else {
			set("F", r.SensitivityLevel)
		}
		if v, ok := markingLabel[r.MarkingMethod]; ok {
			set("G", v)
		} else {
			set("G", r.MarkingMethod)
		}
		if v, ok := statusLabel[r.LifecycleStatus]; ok {
			set("H", v)
		} else {
			set("H", r.LifecycleStatus)
		}
		if r.CurrentStorageURI != nil {
			set("I", *r.CurrentStorageURI)
		}
		set("J", r.CreateTime.Format("2006-01-02 15:04:05"))
	}

	for i := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, col, col, 22)
	}
	_ = f.SetColWidth(sheet, "I", "I", 50)
	return f, nil
}

// ExportLedgersToDisk POST /ledgers/export-to-downloads
//
// 写 .xlsx 到用户 Downloads 目录（macOS/Linux: ~/Downloads；Windows: %USERPROFILE%\Downloads），
// 返回完整路径。前端用此端点：
//  1. 不依赖 WebView 的下载行为（Wails WebView 拦 blob/<a download> 经常不弹 native dialog）
//  2. 用户在 toast 里能看到具体保存位置，可选"复制路径"
//
// 请求体可省略；查询参数同 SearchLedgers，多一个 include_drafts=1。
func ExportLedgersToDisk(c *gin.Context) {
	in := repository.LedgerSearchInput{
		ProjectCode:      c.Query("project_code"),
		StageCode:        c.Query("stage_code"),
		SensitivityLevel: c.Query("sensitivity_level"),
		LifecycleStatus:  c.Query("lifecycle_status"),
		Keyword:          c.Query("keyword"),
	}
	if v := c.Query("owner_subject_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.OwnerSubjectID = n
		}
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	rows, err := repo.Search(in)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	includeDrafts := c.Query("include_drafts") == "1"
	if !includeDrafts {
		filtered := rows[:0]
		for _, r := range rows {
			if r.LifecycleStatus != "planned" {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	// 解析 Downloads 目录
	downloadsDir, err := userDownloadsDir()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "无法定位 Downloads 目录: " + err.Error()})
		return
	}
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "无法创建 Downloads 目录: " + err.Error()})
		return
	}

	// V3-8 §7.5: 通过 ?format= 区分 xlsx / csv / json；默认 xlsx 保持 V1 兼容
	format := c.Query("format")
	if format == "" {
		format = "xlsx"
	}
	ts := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("ledgers-%s.%s", ts, format)
	fullPath := filepath.Join(downloadsDir, filename)

	switch format {
	case "xlsx":
		f, err := buildLedgerXLSX(rows)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
			return
		}
		defer f.Close()
		if err := f.SaveAs(fullPath); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "保存文件失败: " + err.Error()})
			return
		}
	case "csv":
		var b strings.Builder
		b.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM 让 Excel 正确识别中文
		b.WriteString("底账编号,资产名称,所属项目,环节,文件版本,敏感等级,标识方式,生命周期,存储位置\n")
		csvEscape := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
		for _, r := range rows {
			uri := ""
			if r.CurrentStorageURI != nil {
				uri = *r.CurrentStorageURI
			}
			b.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
				csvEscape(r.LedgerCode), csvEscape(r.AssetName), csvEscape(r.ProjectCode),
				csvEscape(r.StageCode), csvEscape(r.FileVersionCode), csvEscape(r.SensitivityLevel),
				csvEscape(r.MarkingMethod), csvEscape(r.LifecycleStatus), csvEscape(uri)))
		}
		if err := os.WriteFile(fullPath, []byte(b.String()), 0o644); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "保存文件失败: " + err.Error()})
			return
		}
	case "json":
		payload := gin.H{"count": len(rows), "ledgers": rows, "exported_at": time.Now().Format(time.RFC3339)}
		bs, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
			return
		}
		if err := os.WriteFile(fullPath, bs, 0o644); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "保存文件失败: " + err.Error()})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "不支持的导出格式: " + format})
		return
	}

	stat, _ := os.Stat(fullPath)
	var size int64
	if stat != nil {
		size = stat.Size()
	}

	// V3-5 §11.1.8 导出底账审计
	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     currentOperator(c),
		ActorUserID: currentUserID(c),
		Action:      repository.AuditExportLedger,
		TargetType:  repository.AuditTargetExport,
		TargetCode:  filename,
		After:       gin.H{"count": len(rows), "size": size, "path": fullPath, "filters": in},
		IPAddress:   c.ClientIP(),
		Message:     fmt.Sprintf("导出底账 %d 条到 %s", len(rows), fullPath),
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"path":     fullPath,
			"filename": filename,
			"size":     size,
			"count":    len(rows),
		},
	})
}

// ExportLedgersCSV V3-8 §7.5 + §8.4 导出底账 CSV
//
// 文档 §7.5 "导出底账 支持 CSV、JSON"。
// 字段顺序与 XLSX 导出对齐，加 BOM 让 Excel 正确识别 UTF-8。
func ExportLedgersCSV(c *gin.Context) {
	rows, err := loadLedgersForExport(c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="ledgers.csv"`)
	// UTF-8 BOM
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	c.Writer.Write([]byte("底账编号,资产名称,所属项目,环节,文件版本,敏感等级,标识方式,生命周期,存储位置\n"))
	for _, r := range rows {
		uri := ""
		if r.CurrentStorageURI != nil {
			uri = *r.CurrentStorageURI
		}
		csvEscape := func(s string) string {
			s = strings.ReplaceAll(s, `"`, `""`)
			return `"` + s + `"`
		}
		line := fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			csvEscape(r.LedgerCode),
			csvEscape(r.AssetName),
			csvEscape(r.ProjectCode),
			csvEscape(r.StageCode),
			csvEscape(r.FileVersionCode),
			csvEscape(r.SensitivityLevel),
			csvEscape(r.MarkingMethod),
			csvEscape(r.LifecycleStatus),
			csvEscape(uri),
		)
		c.Writer.Write([]byte(line))
	}

	// V3-5 §11.1.8 导出底账审计
	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID: currentOperator(c), ActorUserID: currentUserID(c),
		Action: repository.AuditExportLedger, TargetType: repository.AuditTargetExport,
		TargetCode: "ledgers.csv",
		Message:    fmt.Sprintf("CSV 导出 %d 条", len(rows)),
		IPAddress:  c.ClientIP(),
	})
}

// ExportLedgersJSON V3-8 §7.5 + §8.4 导出底账 JSON
func ExportLedgersJSON(c *gin.Context) {
	rows, err := loadLedgersForExport(c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="ledgers.json"`)
	c.JSON(http.StatusOK, gin.H{"count": len(rows), "ledgers": rows})

	auditRepo := repository.NewAuditLogRepository(repository.GetDB())
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID: currentOperator(c), ActorUserID: currentUserID(c),
		Action: repository.AuditExportLedger, TargetType: repository.AuditTargetExport,
		TargetCode: "ledgers.json",
		Message:    fmt.Sprintf("JSON 导出 %d 条", len(rows)),
		IPAddress:  c.ClientIP(),
	})
}

// loadLedgersForExport 三个导出端点共用筛选 + include_drafts 过滤
func loadLedgersForExport(c *gin.Context) ([]models.AssetLedger, error) {
	in := repository.LedgerSearchInput{
		ProjectCode:      c.Query("project_code"),
		StageCode:        c.Query("stage_code"),
		SensitivityLevel: c.Query("sensitivity_level"),
		LifecycleStatus:  c.Query("lifecycle_status"),
		Keyword:          c.Query("keyword"),
	}
	if v := c.Query("owner_subject_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			in.OwnerSubjectID = n
		}
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	rows, err := repo.Search(in)
	if err != nil {
		return nil, err
	}
	if c.Query("include_drafts") != "1" {
		filtered := rows[:0]
		for _, r := range rows {
			if r.LifecycleStatus != "planned" {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	return rows, nil
}

// userDownloadsDir 返回当前用户的 Downloads 目录绝对路径
//
// macOS / Linux：~/Downloads
// Windows：%USERPROFILE%\Downloads
func userDownloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", fmt.Errorf("无法获取用户主目录")
	}
	return filepath.Join(home, "Downloads"), nil
}

// TransitionLedger POST /ledgers/:id/transition
//
// 受 ValidStateTransition 守护的合法状态切换：
//
//	registered → in_use / sealed
//	in_use     → registered / sealed
//	sealed     → destroyed / permanent
func TransitionLedger(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req TransitionLedgerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.ToStatus == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "to_status 必填"})
		return
	}
	svc := repository.NewLedgerLifecycleService(repository.GetDB())
	if err := svc.Transition(repository.TransitionInput{
		LedgerID:       id,
		ToStatus:       req.ToStatus,
		OperatorID:     currentOperator(c),
		OperatorUserID: currentUserID(c),
		Reason:         req.Reason,
		ApprovalRef:    req.ApprovalRef,
	}); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	l, _ := repo.FindByID(id)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": l})
}

// HandoverLedgerRequest 过户入参
type HandoverLedgerRequest struct {
	SubjectKind string `json:"subject_kind"` // owner / custodian / security
	ToSubjectID int64  `json:"to_subject_id"`
	Reason      string `json:"reason"`
	ApprovalRef string `json:"approval_ref"`
}

// HandoverLedger POST /ledgers/:id/handover
//
// V2-7: 把底账的三主体之一（归属/保管/安全）过户到新主体，
// 写一条 handover 生命周期事件（from→to）；状态机不变。
func HandoverLedger(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req HandoverLedgerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	svc := repository.NewLedgerLifecycleService(repository.GetDB())
	if err := svc.Handover(repository.HandoverInput{
		LedgerID:       id,
		SubjectKind:    req.SubjectKind,
		ToSubjectID:    req.ToSubjectID,
		Reason:         req.Reason,
		ApprovalRef:    req.ApprovalRef,
		OperatorID:     currentOperator(c),
		OperatorUserID: currentUserID(c),
	}); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	l, _ := repo.FindByID(id)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": l})
}

// OpenLedgerFile POST /ledgers/:id/open
//
// 用本机 OS 默认程序打开 ledger 关联的文件（即 current_storage_uri 指向的副本）。
// 仅本机调用：不返文件内容，只是触发 explorer/open/xdg-open。
func OpenLedgerFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid ledger id"})
		return
	}
	repo := repository.NewAssetLedgerRepository(repository.GetDB())
	l, err := repo.FindByID(id)
	if err != nil || l == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "ledger 不存在"})
		return
	}
	if l.CurrentStorageURI == nil || strings.TrimSpace(*l.CurrentStorageURI) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "ledger 未绑定文件存储路径"})
		return
	}
	opener := internal.NewFileOpenerService()
	if err := opener.OpenFile(*l.CurrentStorageURI); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"file_path": *l.CurrentStorageURI}})
}
