package httpd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// RegisterCentralizedProjectsRoutes 注册 /centralized-projects 路由
func RegisterCentralizedProjectsRoutes(r *gin.RouterGroup) {
	r.GET("", ListCentralizedProjects)
	r.POST("", CreateCentralizedProject)
	// 2026-06-08 编辑草稿 / 从草稿发布（id 在 body，避开与 :remote_id 的路由参数冲突）
	r.PUT("/draft", UpdateCentralizedProjectDraft)
	// 主动从 manage 拉一次最新审核结果，回写本地
	r.POST("/refresh", RefreshCentralizedProjectsFromManage)
	// 数据项目在线承接：拉取分配给当前用户的已通过项目 + 承接动作
	r.GET("/assigned", ListAssignedCentralizedProjects)
	r.POST("/:remote_id/accept", AcceptCentralizedProject)
	r.POST("/accept-project", AcceptProjectProxy) // 承接：approved→taken
	r.GET("/team", TeamGetProxy)                  // 项目团队：读
	r.POST("/team", TeamPostProxy)                // 项目团队：组建
	r.GET("/stage-team", StageTeamGetProxy)       // 环节团队：读
	r.POST("/stage-team", StageTeamPostProxy)     // 环节团队：组建
	r.GET("/involved", InvolvedProjectsProxy)     // 工作组：我参与的项目
	r.GET("/work-group", WorkGroupProxy)          // 工作组：详情
	// 环节负责人视角：拉自己的 stage 任务 + 启动任务 + 交付到下一环节
	r.GET("/my-stages", ListMyStageTasks)
	r.GET("/my-tasks", MyTasksProxy)
	r.POST("/start-task", StartTaskHandler)
	r.GET("/task-file-rules", TaskFileRules)               // 工作受理：该任务全部文档标识及属性
	r.GET("/task-finals-candidates", TaskFinalsCandidates) // 任务级定稿：候选(output 标识+过程文件)
	r.POST("/submit-task-finals", SubmitTaskFinals)        // 任务级定稿：提交(挑过程→定稿+归档)
	r.POST("/complete-task", CompleteTaskProxy)
	r.GET("/task-unread-count", TaskUnreadCountProxy)
	r.POST("/mark-tasks-seen", MarkTasksSeenProxy)
	r.POST("/stages/:stage_id/start", StartStageTask)
	r.POST("/stages/:stage_id/deliver", DeliverStageToNext)
	// 项目负责人视角：查看某 application 下所有环节的指派和进度
	r.GET("/:remote_id/stages", ListApplicationStages)
	// 项目负责人结项（透传 closer=当前登录人 + 单位级可带 move_file_ids 归卷单位室）
	r.POST("/:remote_id/close", CloseCentralizedProject)
	// 一键归档：按九宫格分流（个人→本地夹 / 部门、单位→上报云端 / 行业→跳过）
	r.POST("/quick-archive-all", QuickArchiveAllCentralized)
	r.GET("/quick-archive-files", QuickArchiveCabinetFilesProxy)
	// 结项归卷：拉某项目部门柜定稿清单（供单位级结项勾选移入单位室）
	r.GET("/:remote_id/final-files", CentralizedFinalFilesProxy)
	r.GET("/personal-archive-files", PersonalArchiveFilesList)
	r.POST("/:remote_id/quick-archive", QuickArchiveCentralizedProject)
	r.GET("/my-submissions", ListMySubmissions)
	r.POST("/set-template", SetTemplateProxy)
	// 立项过程中编辑项目专属模版（增删改工作事项/文件任务/标识）+ 整树回灌 manage
	r.GET("/project-template", ProjectTemplate)
	r.POST("/save-project-template", SaveProjectTemplate)
	r.GET("/template-diff", TemplateDiff)                  // 提取前的改动清单（对比基线）
	r.POST("/extract-template", ExtractCertifiedTemplate) // 提取项目认定模版（单位最高权威）
	r.GET("/unread-count", UnreadCountProxy)
	r.POST("/mark-seen", MarkSeenProxy)
	r.GET("/stage-tasks", StageTasksProxy)
	r.POST("/assign-tasks", AssignTasksProxy)
	r.GET("/stage-unread-count", StageUnreadCountProxy)
	r.POST("/mark-stages-seen", MarkStagesSeenProxy)
}

type centralizedProjectRow struct {
	ID                 int64   `db:"id" json:"id"`
	ProjectName        string  `db:"project_name" json:"project_name"`
	ProjectCode        *string `db:"project_code" json:"project_code"`
	OwnerName          string  `db:"owner_name" json:"owner_name"`
	Department         *string `db:"department" json:"department"`
	DataOwner          *string `db:"data_owner" json:"data_owner"`
	SubmittedBy        string  `db:"submitted_by" json:"submitted_by"`
	Status             string  `db:"status" json:"status"`
	SensitivityLevel   string  `db:"sensitivity_level" json:"sensitivity_level"`
	ProjectScope       string  `db:"project_scope" json:"project_scope"`
	OutputCustodyScope string  `db:"output_custody_scope" json:"output_custody_scope"`
	OutputCustodyNote  string  `db:"output_custody_note" json:"output_custody_note"`
	ApprovalBasis      *string `db:"approval_basis" json:"approval_basis"`
	Description        *string `db:"description" json:"description"`
	ManageRemoteID     *int64  `db:"manage_remote_id" json:"manage_remote_id"`
	SyncStatus         string  `db:"sync_status" json:"sync_status"`
	SyncError          *string `db:"sync_error" json:"sync_error"`
	RejectReason       *string `db:"reject_reason" json:"reject_reason"`
	ReviewedAt         *string `db:"reviewed_at" json:"reviewed_at"`
	CreateTime         string  `db:"create_time" json:"create_time"`
	UpdateTime         string  `db:"update_time" json:"update_time"`
	// 以下三项不落本地表，仅作 manage 云端权威值的展示叠加（多端联动）：
	// 项目周期（负责人承接时填）+ 整体完成率。db:"-" 让 sqlx 扫描本地行时忽略。
	CycleStart     *string `db:"-" json:"cycle_start"`
	CycleEnd       *string `db:"-" json:"cycle_end"`
	CompletionRate *int    `db:"-" json:"completion_rate"`
}

// ListCentralizedProjects GET /centralized-projects?page=&page_size=
//
// 仅返回当前用户作为 submitted_by 提交的条目 —— 多用户共用同一台 scan 终端时
// 互相不可见。manage 端审核结果（驳回原因 / 审核时间）在 refresh 路径回写。
func ListCentralizedProjects(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	db := repository.GetDB()
	operator := currentOperator(c)

	var total int
	if err := db.Get(&total,
		`SELECT COUNT(*) FROM centralized_project_applications
		  WHERE disable = 0 AND submitted_by = ?`, operator); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	var items []centralizedProjectRow
	if err := db.Select(&items,
		`SELECT id, project_name, project_code, owner_name, department, data_owner, submitted_by,
		        status, sensitivity_level, project_scope, output_custody_scope, output_custody_note, approval_basis, description,
		        manage_remote_id, sync_status, sync_error, reject_reason, reviewed_at,
		        create_time, update_time
		   FROM centralized_project_applications
		  WHERE disable = 0 AND submitted_by = ?
		  ORDER BY id DESC LIMIT ? OFFSET ?`, operator, pageSize, (page-1)*pageSize); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	// manage 云端权威叠加（多端联动）：状态(工程进展)/负责人/项目周期/整体完成率以 manage 为准，
	// 不依赖本地存储——本地表仅作离线兜底。同时把 manage 上本人提交、本地缺失的项目补进来。
	// 失败不阻断，仍返回本地结果。
	if endpoint := strings.TrimRight(strings.TrimSpace(repository.NewSystemConfigRepository(db).GetValue(repository.KeyManageEndpoint)), "/"); endpoint != "" && operator != "" && operator != "system" {
		if remoteRows, err := fetchManageSubmissions(endpoint, operator); err == nil {
			// 索引 manage 行：按 project_code 与 manage_remote_id。
			byCode := map[string]centralizedProjectRow{}
			byRemote := map[int64]centralizedProjectRow{}
			for _, rr := range remoteRows {
				if rr.ProjectCode != nil && *rr.ProjectCode != "" {
					byCode[*rr.ProjectCode] = rr
				}
				if rr.ManageRemoteID != nil {
					byRemote[*rr.ManageRemoteID] = rr
				}
			}
			haveCode := map[string]bool{}
			haveRemote := map[int64]bool{}
			for i := range items {
				var m *centralizedProjectRow
				if items[i].ProjectCode != nil && *items[i].ProjectCode != "" {
					if v, ok := byCode[*items[i].ProjectCode]; ok {
						m = &v
					}
				}
				if m == nil && items[i].ManageRemoteID != nil {
					if v, ok := byRemote[*items[i].ManageRemoteID]; ok {
						m = &v
					}
				}
				if m != nil {
					// 草稿(本地未推送)无 manage 对应行→不会进这里，保持本地草稿状态。
					items[i].Status = m.Status
					items[i].OwnerName = m.OwnerName
					items[i].CycleStart = m.CycleStart
					items[i].CycleEnd = m.CycleEnd
					items[i].CompletionRate = m.CompletionRate
				}
				if items[i].ProjectCode != nil && *items[i].ProjectCode != "" {
					haveCode[*items[i].ProjectCode] = true
				}
				if items[i].ManageRemoteID != nil {
					haveRemote[*items[i].ManageRemoteID] = true
				}
			}
			for _, rr := range remoteRows {
				if rr.ProjectCode != nil && *rr.ProjectCode != "" && haveCode[*rr.ProjectCode] {
					continue
				}
				if rr.ManageRemoteID != nil && haveRemote[*rr.ManageRemoteID] {
					continue
				}
				items = append(items, rr)
				total++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"items": items, "total": total, "page": page, "page_size": pageSize},
	})
}

// manageCentralizedRow 是 manage /api/centralized-projects/list 返回的行（仅取合并所需字段）。
type manageCentralizedRow struct {
	ID                 int64  `json:"id"`
	ProjectName        string `json:"project_name"`
	ProjectCode        string `json:"project_code"`
	OwnerName          string `json:"owner_name"`
	Department         string `json:"department"`
	DataOwner          string `json:"data_owner"`
	SubmittedBy        string `json:"submitted_by"`
	Status             string `json:"status"`
	SensitivityLevel   string `json:"sensitivity_level"`
	ProjectScope       string `json:"project_scope"`
	OutputCustodyScope string `json:"output_custody_scope"`
	OutputCustodyNote  string `json:"output_custody_note"`
	ApprovalBasis      string `json:"approval_basis"`
	Description        string `json:"description"`
	CycleStart         string `json:"cycle_start"`
	CycleEnd           string `json:"cycle_end"`
	CompletionRate     *int   `json:"completion_rate"`
	CreateTime         string `json:"create_time"`
	UpdateTime         string `json:"update_time"`
}

// fetchManageSubmissions 从 manage 拉取某用户提交的集中立项项目，映射成本地列表行（供 ListCentralizedProjects 合并）。
func fetchManageSubmissions(endpoint, submittedBy string) ([]centralizedProjectRow, error) {
	url := fmt.Sprintf("%s/api/centralized-projects/list?submitted_by=%s", endpoint, encodeQuery(submittedBy))
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code int                    `json:"code"`
		Data []manageCentralizedRow `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("manage 返回非 0: %d", out.Code)
	}
	rows := make([]centralizedProjectRow, 0, len(out.Data))
	for _, m := range out.Data {
		id := m.ID
		rows = append(rows, centralizedProjectRow{
			ID:                 m.ID,
			ProjectName:        m.ProjectName,
			ProjectCode:        strPtrOrNil(m.ProjectCode),
			OwnerName:          m.OwnerName,
			Department:         strPtrOrNil(m.Department),
			DataOwner:          strPtrOrNil(m.DataOwner),
			SubmittedBy:        m.SubmittedBy,
			Status:             m.Status,
			SensitivityLevel:   m.SensitivityLevel,
			ProjectScope:       m.ProjectScope,
			OutputCustodyScope: m.OutputCustodyScope,
			OutputCustodyNote:  m.OutputCustodyNote,
			ApprovalBasis:      strPtrOrNil(m.ApprovalBasis),
			Description:        strPtrOrNil(m.Description),
			ManageRemoteID:     &id,
			SyncStatus:         "synced",
			CycleStart:         strPtrOrNil(m.CycleStart),
			CycleEnd:           strPtrOrNil(m.CycleEnd),
			CompletionRate:     m.CompletionRate,
			CreateTime:         m.CreateTime,
			UpdateTime:         m.UpdateTime,
		})
	}
	return rows, nil
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CreateCentralizedProjectRequest POST /centralized-projects 入参
type CreateCentralizedProjectRequest struct {
	ProjectName        string `json:"project_name"`
	ProjectCode        string `json:"project_code"`
	OwnerName          string `json:"owner_name"`
	Department         string `json:"department"`
	DataOwner          string `json:"data_owner"`           // 数据权属（原"定数权"，选填）
	SensitivityLevel   string `json:"sensitivity_level"`    // core / important / general（决定 保密/档案/资料）
	ProjectScope       string `json:"project_scope"`        // person / department / unit（项目层级，决定 夹/柜/室 + 本地/上云）
	OutputCustodyScope string `json:"output_custody_scope"` // 定稿保管层级（仅单位级可选）：unit / department
	OutputCustodyNote  string `json:"output_custody_note"`  // 归档归属说明（选填）
	ApprovalBasis      string `json:"approval_basis"`       // 立项依据（选填，纯文本）
	Description        string `json:"description"`          // 项目简介（选填，纯文本）
	SaveAsDraft        bool   `json:"save_as_draft"`        // true=存草稿(不推manage)
}

// CreateCentralizedProject POST /centralized-projects
//
// save_as_draft=true：status='draft'，只存本地、不推 manage，仅校验项目名称非空。
// save_as_draft=false：status='approved' + 推送 manage（校验负责人已注册、敏感级合法）。
func CreateCentralizedProject(c *gin.Context) {
	var req CreateCentralizedProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	projectName := strings.TrimSpace(req.ProjectName)
	ownerName := strings.TrimSpace(req.OwnerName)
	department := strings.TrimSpace(req.Department)
	dataOwner := strings.TrimSpace(req.DataOwner)
	projectCode := strings.TrimSpace(req.ProjectCode)
	approvalBasis := strings.TrimSpace(req.ApprovalBasis)
	description := strings.TrimSpace(req.Description)
	sensitivity := strings.ToLower(strings.TrimSpace(req.SensitivityLevel))
	projectScope := strings.ToLower(strings.TrimSpace(req.ProjectScope))
	custodyScope := strings.ToLower(strings.TrimSpace(req.OutputCustodyScope))
	custodyNote := strings.TrimSpace(req.OutputCustodyNote)
	draft := req.SaveAsDraft

	if projectName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "项目名称必填"})
		return
	}
	if !draft {
		if ownerName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "负责人必填"})
			return
		}
		if sensitivity != "core" && sensitivity != "important" && sensitivity != "general" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": "项目敏感等级必填，且仅支持 core / important / general"})
			return
		}
		// 负责人必须是 manage 已注册 active 用户
		_ = syncUsersFromManage(c)
		userRepo := repository.NewUserRepository(repository.GetDB())
		if u, err := userRepo.FindByUsername(ownerName); err != nil || u == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": fmt.Sprintf("负责人「%s」未在系统中注册（或已禁用），请到管理端用户列表建账号后再立项", ownerName)})
			return
		}
	}
	if sensitivity != "core" && sensitivity != "important" && sensitivity != "general" {
		sensitivity = "general" // 草稿未填敏感级时落默认值
	}
	if projectScope != "person" && projectScope != "department" && projectScope != "unit" {
		projectScope = "unit" // 草稿未选层级时落默认值
	}
	// 定稿保管层级仅单位级项目可改投部门级；其余一律按项目层级（存 unit）。
	if !(projectScope == "unit" && custodyScope == "department") {
		custodyScope = "unit"
	}

	status := "approved"
	if draft {
		status = "draft"
	}
	now := time.Now()
	operator := currentOperator(c)
	db := repository.GetDB()
	res, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, project_code, owner_name, department, data_owner, approval_basis, description,
		 submitted_by, status, sync_status, sensitivity_level, project_scope, output_custody_scope, output_custody_note, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, 0)`,
		projectName, projectCode, ownerName, department, dataOwner, approvalBasis, description,
		operator, status, sensitivity, projectScope, custodyScope, custodyNote, now, now)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()

	syncStatus := "pending"
	projectCodeResp := "" // manage 立项时生成的唯一编号（草稿为空）
	if !draft {
		remoteID, projectCode, pushErr := pushCentralizedProjectToManage(db, id, projectName, ownerName, dataOwner, operator, sensitivity, projectScope, custodyScope, custodyNote, department, approvalBasis, description)
		if pushErr != nil {
			_, _ = db.Exec(`UPDATE centralized_project_applications
			                  SET sync_status='failed', sync_error=?, update_time=? WHERE id=?`,
				pushErr.Error(), time.Now(), id)
			syncStatus = "failed"
		} else {
			// 立项编号由 manage 统一生成并返回，回写本地作为该项目唯一标识（建目录用）。
			_, _ = db.Exec(`UPDATE centralized_project_applications
			                  SET sync_status='synced', sync_error=NULL, manage_remote_id=?, project_code=?, update_time=? WHERE id=?`,
				remoteID, projectCode, time.Now(), id)
			syncStatus = "synced"
			projectCodeResp = projectCode
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"id": id, "status": status, "sync_status": syncStatus, "project_code": projectCodeResp},
	})
}

// centralizedDirCode 返回某集中立项项目用于建目录的名称：「{项目名称}-{项目编码}」。
// 项目编码（manage 单点生成的 XM-YYYY-NNNN，带唯一索引）保证目录全局唯一、不会重复；
// 前缀项目名称只为便于人辨认。编码优先用调用方显式传入的（来自 my-tasks），否则按 manage
// 立项 id 在本地库回查；都没有时回退旧式 CPA-{id}。项目名取不到时只用编码。
func centralizedDirCode(db interface {
	Get(dest interface{}, query string, args ...interface{}) error
}, manageAppID int64, explicit string) string {
	var row struct {
		Name string `db:"project_name"`
		Code string `db:"project_code"`
	}
	_ = db.Get(&row, `SELECT COALESCE(project_name,'') AS project_name, COALESCE(project_code,'') AS project_code
	                     FROM centralized_project_applications WHERE manage_remote_id = ? LIMIT 1`, manageAppID)
	code := strings.TrimSpace(explicit)
	if code == "" {
		code = strings.TrimSpace(row.Code)
	}
	if code == "" {
		code = fmt.Sprintf("CPA-%d", manageAppID)
	}
	if name := sanitizeDirSegment(row.Name); name != "" {
		return name + "-" + code
	}
	return code
}

// centralizedProjectCode 返回某集中立项项目的【裸项目编码】(project_code，manage 全局唯一)，
// 作跨机定稿交接的 manage 存储键——区别于 centralizedDirCode(含项目名，仅用于本机目录)。
// 取不到(草稿/未同步)返回空，调用方据此跳过跨机交接(同机流程不受影响)。
func centralizedProjectCode(db interface {
	Get(dest interface{}, query string, args ...interface{}) error
}, manageAppID int64) string {
	var code string
	_ = db.Get(&code, `SELECT COALESCE(project_code,'') FROM centralized_project_applications WHERE manage_remote_id = ? LIMIT 1`, manageAppID)
	return strings.TrimSpace(code)
}

// sanitizeDirSegment 把项目名清成可作目录名的片段：替换跨平台非法字符、去首尾点/空白、按字符截断长度。
func sanitizeDirSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\n', '\r', '\t':
			return '_'
		}
		return r
	}, s)
	s = strings.Trim(s, " .") // windows 不允许目录名以点/空格结尾
	if runes := []rune(s); len(runes) > 40 {
		s = strings.TrimSpace(string(runes[:40]))
	}
	return s
}

// pushCentralizedProjectToManage 把单条立项申请推到 manage 端。
// 返回 (manage 侧 id, manage 生成的立项编号 project_code, error)。编号由 manage 单点
// 生成并保证全局唯一（XM-YYYY-NNNN），scan 据此命名工作目录。
func pushCentralizedProjectToManage(db interface {
	Get(dest interface{}, query string, args ...interface{}) error
}, originID int64, projectName, ownerName, dataOwner, submittedBy, sensitivityLevel, projectScope, custodyScope, custodyNote, department, approvalBasis, description string) (int64, string, error) {
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		return 0, "", fmt.Errorf("未配置 manage_endpoint")
	}
	// scan_endpoint 用本终端稳定实例标识（不是 manage 地址）：配合 scan_origin_id 让去重键全局稳定，
	// 不同终端/重装互不串号，避免本地 id 重排撞上 manage 旧记录而"继承"已分工状态。
	scanInstance := cfg.EnsureScanInstanceID()
	body, _ := json.Marshal(map[string]interface{}{
		"scan_origin_id":       originID,
		"scan_endpoint":        scanInstance,
		"project_name":         projectName,
		"owner_name":           ownerName,
		"data_owner":           dataOwner,
		"submitted_by":         submittedBy,
		"sensitivity_level":    sensitivityLevel,
		"project_scope":        projectScope,
		"output_custody_scope": custodyScope,
		"output_custody_note":  custodyNote,
		"department":           department,
		"approval_basis":       approvalBasis,
		"description":          description,
	})
	req, _ := http.NewRequest("POST", endpoint+"/api/centralized-projects/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("manage 返回非 200: %d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Code int `json:"code"`
		Data struct {
			ID          int64  `json:"id"`
			ProjectCode string `json:"project_code"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, "", fmt.Errorf("解析 manage 响应失败: %w", err)
	}
	if out.Code != 0 {
		return 0, "", fmt.Errorf("manage 返回错误: %s", out.Message)
	}
	return out.Data.ID, out.Data.ProjectCode, nil
}

// RefreshCentralizedProjectsFromManage POST /centralized-projects/refresh
//
// 把本地仍 status='pending' 的条目按 scan_origin_id 批量去 manage 查最新审核
// 结果，回写到本地（status / reject_reason / reviewed_at）。前端"刷新"按钮调用。
func RefreshCentralizedProjectsFromManage(c *gin.Context) {
	db := repository.GetDB()
	cfg := repository.NewSystemConfigRepository(db)
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	operator := currentOperator(c)
	type pendingRow struct {
		ID int64 `db:"id"`
	}
	var pending []pendingRow
	// 去审核后立项即 approved，承接/结项只改 manage 端状态；这里同步所有未结项(非 closed)的本地行，
	// 让本地反映 accepted/closed，否则「结项」按钮(依赖全环节完成)关不掉、状态显示滞后。
	if err := db.Select(&pending, `SELECT id FROM centralized_project_applications
	                                  WHERE status NOT IN ('closed','draft') AND disable=0
	                                    AND submitted_by = ?`, operator); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if len(pending) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"updated": 0}})
		return
	}
	ids := make([]string, 0, len(pending))
	for _, p := range pending {
		ids = append(ids, strconv.FormatInt(p.ID, 10))
	}
	// scan_endpoint 用本终端稳定实例标识（与提交立项时一致），才能匹配回本终端提交的记录。
	scanInstance := cfg.EnsureScanInstanceID()
	url := fmt.Sprintf("%s/api/centralized-projects/by-origins?scan_endpoint=%s&origin_ids=%s",
		endpoint, encodeQuery(scanInstance), strings.Join(ids, ","))
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code int `json:"code"`
		Data []struct {
			ID           int64   `json:"id"`
			ScanOriginID int64   `json:"scan_origin_id"`
			OwnerName    string  `json:"owner_name"`
			Status       string  `json:"status"`
			RejectReason *string `json:"reject_reason"`
			ReviewedAt   *string `json:"reviewed_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "manage 返回非 0"})
		return
	}
	updated := 0
	for _, r := range out.Data {
		if r.Status == "pending" {
			continue
		}
		_, err := db.Exec(`UPDATE centralized_project_applications
		                      SET status = ?, owner_name = ?, reject_reason = ?, reviewed_at = ?, update_time = ?
		                    WHERE id = ?`,
			r.Status, r.OwnerName, r.RejectReason, r.ReviewedAt, time.Now(), r.ScanOriginID)
		if err == nil {
			updated++
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"updated": updated}})
}

// ListAssignedCentralizedProjects GET /centralized-projects/assigned
//
// 拉取 manage 端分配给当前 scan 用户（owner_name == currentOperator）
// 且 status IN (approved, accepted) 的项目列表。
// 不读本地表——视为 manage 数据的只读视图。
func ListAssignedCentralizedProjects(c *gin.Context) {
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	owner := currentOperator(c)
	if owner == "" || owner == "system" {
		// Bug2：识别不到登录用户时不要静默返回空（会被误当成"没有被指派的项目"）。
		// 明确报错，提示重新登录——否则被指派人永远看不到项目却不知为何。
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未识别登录用户，请重新登录后再查看被指派的项目"})
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	// 拉 approved + accepted 两类（同名 owner_name 全拉），前端可分桶展示
	type manageResp struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	merged := []map[string]any{}
	// 含 closed：结项后项目仍需在「项目工作分工」页展示，以便在该行末尾提供「提取项目模版」。
	for _, status := range []string{"approved", "taken", "assigning", "accepted", "closed"} {
		url := fmt.Sprintf("%s/api/centralized-projects/list?status=%s&owner_name=%s",
			endpoint, status, encodeQuery(owner))
		req, _ := http.NewRequest("GET", url, nil)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var out manageResp
		if err := json.Unmarshal(raw, &out); err != nil || out.Code != 0 {
			continue
		}
		merged = append(merged, out.Data...)
	}
	// 增补：本项目立项过程中是否改动过模版结构（「提取项目模版」按钮门禁）。
	// 仅对已结项(closed)项目计算（按钮只在结项后出现），且走云端基线对比——
	// 不依赖本地 DB，支持多端 / 重建本地库后仍准确。
	authRepo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	for _, row := range merged {
		appID := ""
		switch v := row["id"].(type) {
		case float64:
			appID = strconv.FormatInt(int64(v), 10)
		case string:
			appID = v
		}
		edited := false
		status, _ := row["status"].(string)
		if appID != "" && status == "closed" {
			// derr==nil 表示 manage 上确有本项目专属模版（走过关联模版）。
			//   有基线 → 仅当确有改动才提示；无基线（老项目）→ 无法判定，仍给出入口（弹窗会说明）。
			if changes, hasBaseline, derr := authRepo.DiffProjectTemplate(client, endpoint, appID); derr == nil {
				edited = !hasBaseline || len(changes) > 0
			}
		}
		row["project_template_edited"] = edited
	}
	// 回带解析出的登录名，供前端显示"当前识别登录名：X / 云端按此名查到 N 条"，
	// 让"身份对不上导致查空"一眼可见。
	c.JSON(http.StatusOK, gin.H{"success": true, "data": merged, "username": owner})
}

// AcceptCentralizedProject POST /centralized-projects/:remote_id/accept
//
// 透传 acceptor + template_id/code/version + stage_assignments 给 manage。
// 前端在弹窗里选模板 + 为每个 stage 选负责人，组成 payload 直接转发。
func AcceptCentralizedProject(c *gin.Context) {
	remoteID := c.Param("remote_id")
	if _, err := strconv.ParseInt(remoteID, 10, 64); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid remote_id"})
		return
	}
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}

	// 读前端 payload 并补全 acceptor
	var in map[string]any
	_ = c.ShouldBindJSON(&in)
	if in == nil {
		in = map[string]any{}
	}
	in["acceptor"] = currentOperator(c)

	// manage 的 accept 会用 payload.template_id 覆盖 application.template_id；前端传的是
	// scan/平台侧 id。这里换成 manage 侧 id（与 set-template 一致），否则 accept 会把
	// set-template 存好的正确 id 冲掉，导致任务层(stage-tasks) 查不到环节/任务。
	if manageTplID, rerr := resolveManageTemplateID(endpoint, in, remoteID); rerr == nil {
		in["template_id"] = manageTplID
	} else {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": rerr.Error()})
		return
	}
	body, _ := json.Marshal(in)

	url := fmt.Sprintf("%s/api/centralized-projects/accept?id=%s", endpoint, remoteID)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": out.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out.Data})
}

// ListMyStageTasks GET /centralized-projects/my-stages
//
// 拉 manage 端分配给当前用户的所有 stage 任务。前端「环节任务」页面用。
func ListMyStageTasks(c *gin.Context) {
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	owner := currentOperator(c)
	url := fmt.Sprintf("%s/api/centralized-projects/my-stages?assignee=%s", endpoint, encodeQuery(owner))
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code int `json:"code"`
		Data any `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "manage 返回非 0"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out.Data})
}

// StartStageTaskRequest 前端启动环节任务时附带的上下文。
// 后端用 application_id + stage_code 在本机按所选模板建目录。
type StartStageTaskRequest struct {
	ApplicationID int64    `json:"application_id"`
	StageCode     string   `json:"stage_code"`
	TemplateCode  string   `json:"template_code"`   // 承接所选模版编码，用于按「过程」文档标识预建占位
	AllStageCodes []string `json:"all_stage_codes"` // 该项目下全部环节编码，用于一次性建好整棵目录树
}

// StartStageTask POST /centralized-projects/stages/:stage_id/start
//
// 流程：
//  1. 校验本机配置 project_root 已填
//  2. 用 ProjectWorkspace.CreateProjectTree 按所选模板的环节列表建目录树
//     虚拟 project_code = "CPA-<application_id>"，与正式 data_projects 完全隔离
//  3. 透传 manage 的 start-stage 端点把 status 推到 in_progress
//  4. 返回目录路径给前端展示
func StartStageTask(c *gin.Context) {
	stageID := c.Param("stage_id")
	if _, err := strconv.ParseInt(stageID, 10, 64); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid stage_id"})
		return
	}
	db := repository.GetDB()
	cfg := repository.NewSystemConfigRepository(db)
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}

	// 1. 校验项目根目录（已与「工作空间目录」合并，统一取 workspace；workspace 空才回退到旧 KeyProjectRoot）
	projectRoot := strings.TrimSpace(cfg.GetEffectiveProjectRoot())
	if projectRoot == "" {
		c.JSON(http.StatusOK, gin.H{"success": false,
			"error": "尚未在系统配置中设置「工作空间目录」，请先到「系统配置」里设置后再启动环节任务"})
		return
	}

	// 2. 读取前端附带的 application_id / stage_code / all_stage_codes
	var req StartStageTaskRequest
	_ = c.ShouldBindJSON(&req)
	if req.ApplicationID == 0 || req.StageCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false,
			"error": "缺少 application_id 或 stage_code"})
		return
	}

	// 2.5 校验前置环节都已完成（sort_order 比当前小的所有 stage 必须 completed）
	if blockReason, err := checkPrerequisitesCompleted(endpoint, req.ApplicationID, req.StageCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "校验前置环节失败：" + err.Error()})
		return
	} else if blockReason != "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": blockReason})
		return
	}

	// 3. 建目录树（仅基于 application_id + stage_codes，不动 data_projects）
	workspace := repository.NewProjectWorkspace(projectRoot)
	if err := workspace.EnsureProjectRootExists(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false,
			"error": fmt.Sprintf("项目根目录不可写：%v", err)})
		return
	}
	// 项目目录名用唯一立项编码(project_code)，回退 CPA-{id}（与所有读取方一致）。
	virtualProjectCode := centralizedDirCode(db, req.ApplicationID, "")
	stageCodes := req.AllStageCodes
	if len(stageCodes) == 0 {
		stageCodes = []string{req.StageCode}
	}
	if err := workspace.EnsureProjectStageDirs(virtualProjectCode, stageCodes); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false,
			"error": fmt.Sprintf("创建工作目录树失败：%v", err)})
		return
	}
	stageDir := workspace.StageDir(virtualProjectCode, req.StageCode)

	// 3.4 跨机交接（增量·非阻断）：从 manage 拉【紧邻上游环节】的定稿到本环节 input/，
	//     让在【另一台电脑】承接的下游也能拿到上游产出当工作依据。
	//     同机场景下上游交付时已把 output→input 复制好，这里覆盖为云端权威版；失败不阻断启动。
	pulledInputs := 0
	if pc := centralizedProjectCode(db, req.ApplicationID); pc != "" {
		if up := immediateUpstreamStage(req.AllStageCodes, req.StageCode); up != "" {
			if files, perr := repository.PullCentralizedStageFinals(db, nil, endpoint, pc, up, virtualProjectCode, req.StageCode); perr == nil {
				pulledInputs = len(files)
			}
		}
	}

	// 3.5 按模版「过程」文档标识在本环节 process/ 下预建空占位（在线编辑直接填）。
	// 模版来自承接所选编码；本地缺则先从 manage 同步。失败不阻断启动（best-effort）。
	scaffolded := 0
	if req.TemplateCode != "" {
		if err := ensureProjectSyncedLocal(req.TemplateCode); err == nil {
			if paths, serr := repository.ScaffoldStageProcessDocsForProject(db, req.TemplateCode, virtualProjectCode, req.StageCode); serr == nil {
				scaffolded = len(paths)
			}
		}
	}

	// 4. 透传 manage 启动该 stage
	body, _ := json.Marshal(map[string]string{"actor": currentOperator(c)})
	url := fmt.Sprintf("%s/api/centralized-projects/start-stage?id=%s", endpoint, stageID)
	mreq, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	mreq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(mreq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": out.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"manage":               out.Data,
			"project_root":         projectRoot,
			"virtual_project_code": virtualProjectCode,
			"stage_dir":            stageDir,
			"scaffolded":           scaffolded,
			"pulled_inputs":        pulledInputs,
		},
	})
}

// immediateUpstreamStage 在有序环节列表里返回 stage 的紧邻上一个环节编码；首环节或找不到时返回空。
func immediateUpstreamStage(allStages []string, stage string) string {
	for i, s := range allStages {
		if s == stage {
			if i > 0 {
				return allStages[i-1]
			}
			return ""
		}
	}
	return ""
}

// encodeQuery 简单 URL query 编码
// encodeQuery 对查询参数值做标准百分号编码。
// 必须用 url.QueryEscape：中文(及其他非 ASCII)用户名要编码成 %XX 的 ASCII 形式，
// 否则原始 UTF-8 字节进 URL 后，manage(node/h3) 会解析成乱码，按 owner_name 匹配不到 → 空列表。
func encodeQuery(s string) string {
	return url.QueryEscape(s)
}

// DeliverStageToNext POST /centralized-projects/stages/:stage_id/deliver
//
// 环节负责人完成本环节工作后调用：
//  1. 找到该 application 下当前 stage 的下一环节（按 sort_order）
//  2. 把当前 stage_dir/output/ 整桶复制到下一环节 stage_dir/input/
//     （若下一环节目录还没建，先建出来）
//  3. 调 manage 把当前 stage status 推到 completed
//
// payload: { application_id, current_stage_code, all_stage_codes_of_this_user }
type DeliverStageRequest struct {
	ApplicationID    int64                       `json:"application_id"`
	CurrentStageCode string                      `json:"current_stage_code"`
	TemplateCode     string                      `json:"template_code"` // 用于按 output 标识定稿
	Selections       []repository.FinalSelection `json:"selections"`    // 为每个 output 标识挑选的过程文件（和改动前的「挑定稿」一致）
}

func DeliverStageToNext(c *gin.Context) {
	stageID := c.Param("stage_id")
	if _, err := strconv.ParseInt(stageID, 10, 64); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid stage_id"})
		return
	}
	db := repository.GetDB()
	cfg := repository.NewSystemConfigRepository(db)
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	projectRoot := strings.TrimSpace(cfg.GetEffectiveProjectRoot())
	if projectRoot == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置工作空间目录"})
		return
	}

	var req DeliverStageRequest
	_ = c.ShouldBindJSON(&req)
	if req.ApplicationID == 0 || req.CurrentStageCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 application_id 或 current_stage_code"})
		return
	}

	// 1. 拉该 application 下所有 stages，按 sort_order 找下一环节
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("%s/api/centralized-projects/application-stages?application_id=%d", endpoint, req.ApplicationID)
	resp, err := client.Get(url)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "查询环节列表失败：" + err.Error()})
		return
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var stagesOut struct {
		Code int `json:"code"`
		Data []struct {
			ID        int64  `json:"id"`
			StageCode string `json:"stage_code"`
			StageName string `json:"stage_name"`
			SortOrder int    `json:"sort_order"`
			Assignee  string `json:"assignee_username"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &stagesOut); err != nil || stagesOut.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "解析环节列表失败"})
		return
	}
	// 找 current 与 next
	var curSort int = -1
	for _, s := range stagesOut.Data {
		if s.StageCode == req.CurrentStageCode {
			curSort = s.SortOrder
			break
		}
	}
	if curSort < 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未找到当前环节"})
		return
	}
	// next = sort_order 大于 curSort 的最小者
	var next *struct {
		StageCode string
		StageName string
		Assignee  string
	}
	{
		minSort := int(^uint(0) >> 1)
		for _, s := range stagesOut.Data {
			if s.SortOrder > curSort && s.SortOrder < minSort {
				minSort = s.SortOrder
				next = &struct {
					StageCode string
					StageName string
					Assignee  string
				}{s.StageCode, s.StageName, s.Assignee}
			}
		}
	}

	// 项目目录名用唯一立项编码(project_code)，回退 CPA-{id}（与所有读取方一致）。
	virtualProjectCode := centralizedDirCode(db, req.ApplicationID, "")

	// 1.5 定稿（和改动前的「挑定稿」一致）：把用户挑选的过程文件按 output 标识规范名拷到 output/。
	// 定稿目录不展示给用户，由此一键交付时自动生成。
	if len(req.Selections) > 0 && req.TemplateCode != "" {
		_ = ensureProjectSyncedLocal(req.TemplateCode)
		if _, ferr := repository.SubmitStageFinalsToProject(db, req.TemplateCode, virtualProjectCode, req.CurrentStageCode, req.Selections); ferr != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "生成定稿失败：" + ferr.Error()})
			return
		}
	}

	// 1.6 自动归档：按模版规则把本环节 process/output 文件挂账到个人文件夹对应密级下
	// （和改动前一致；best-effort，不阻断交付）。
	if req.TemplateCode != "" {
		_, _ = repository.AutoArchiveStageForProject(db, req.TemplateCode, virtualProjectCode, req.CurrentStageCode)
	}

	// 2. 五层落盘跨环节交付（A1）：当前环节各任务定稿(output) → 下一环节每个任务 input/
	deliveredCount := 0
	if next != nil && req.TemplateCode != "" {
		var copyErr error
		deliveredCount, copyErr = repository.DeliverStageOutputToNextInputs(db, req.TemplateCode, virtualProjectCode, req.CurrentStageCode, next.StageCode)
		if copyErr != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "复制 output 到下一环节失败：" + copyErr.Error()})
			return
		}
	}

	// 3. 调 manage 标记 completed
	body, _ := json.Marshal(map[string]string{"actor": currentOperator(c)})
	completeURL := fmt.Sprintf("%s/api/centralized-projects/complete-stage?id=%s", endpoint, stageID)
	mreq, _ := http.NewRequest("POST", completeURL, bytes.NewReader(body))
	mreq.Header.Set("Content-Type", "application/json")
	mresp, err := client.Do(mreq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "调 manage 完成接口失败：" + err.Error()})
		return
	}
	defer mresp.Body.Close()
	mraw, _ := io.ReadAll(mresp.Body)
	var mout struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(mraw, &mout); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if mout.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": mout.Message})
		return
	}

	// 4. 跨机交接（增量·非阻断）：把本环节定稿上传 manage，供【另一台电脑】的下游承接时拉取。
	//    本机同机复制(step 2)照旧不变；上传失败不影响交付成功，仅在响应里回报。
	uploadedFinals := 0
	var uploadErrs []string
	if pc := centralizedProjectCode(db, req.ApplicationID); pc != "" {
		uploadedFinals, uploadErrs = repository.UploadCentralizedStageFinals(db, nil, endpoint, virtualProjectCode, pc, req.CurrentStageCode, currentOperator(c))
	}

	result := gin.H{
		"delivered_count": deliveredCount,
		"is_last_stage":   next == nil,
		"uploaded_finals": uploadedFinals,
	}
	if len(uploadErrs) > 0 {
		result["upload_errors"] = uploadErrs
	}
	if next != nil {
		result["next_stage_code"] = next.StageCode
		result["next_stage_name"] = next.StageName
		result["next_assignee"] = next.Assignee
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// copyDirShallow 把 src 目录里的一级条目（仅文件，不递归子目录）复制到 dst。
// 返回成功复制的文件数。dst 已存在则覆盖。
func copyDirShallow(src, dst string) (int, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		srcFile := filepath.Join(src, e.Name())
		dstFile := filepath.Join(dst, e.Name())
		if err := copyFile(srcFile, dstFile); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// ListApplicationStages GET /centralized-projects/:remote_id/stages
//
// 项目负责人查看某承接项目下所有工作环节的指派 + 进度。
// 透传 manage 的 /application-stages 端点。
func ListApplicationStages(c *gin.Context) {
	remoteID := c.Param("remote_id")
	if _, err := strconv.ParseInt(remoteID, 10, 64); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid remote_id"})
		return
	}
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	url := fmt.Sprintf("%s/api/centralized-projects/application-stages?application_id=%s", endpoint, remoteID)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code int `json:"code"`
		Data any `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "manage 返回非 0"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out.Data})
}

// checkPrerequisitesCompleted 调 manage 拉该 application 的所有 stages，
// 检查当前 stage（按 stage_code 定位）之前（sort_order 更小）的所有环节
// 是否都已 completed。返回 (blockReason, err)：
//   - blockReason 非空 → 拒绝启动并把它当作错误展示给用户
//   - err 非空 → manage 调用本身失败
//   - 都为空 → 放行
func checkPrerequisitesCompleted(manageEndpoint string, applicationID int64, currentStageCode string) (string, error) {
	url := fmt.Sprintf("%s/api/centralized-projects/application-stages?application_id=%d", manageEndpoint, applicationID)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code int `json:"code"`
		Data []struct {
			StageCode string `json:"stage_code"`
			StageName string `json:"stage_name"`
			Assignee  string `json:"assignee_username"`
			Status    string `json:"status"`
			SortOrder int    `json:"sort_order"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Code != 0 {
		return "", fmt.Errorf("manage 返回非 0")
	}
	// 找当前 stage 的 sort_order
	curSort := -1
	for _, s := range out.Data {
		if s.StageCode == currentStageCode {
			curSort = s.SortOrder
			break
		}
	}
	if curSort < 0 {
		return "", fmt.Errorf("未找到当前环节 %s", currentStageCode)
	}
	// 检查所有 sort_order < curSort 的环节是否都已 completed
	for _, s := range out.Data {
		if s.SortOrder < curSort && s.Status != "completed" {
			return fmt.Sprintf("前置环节「%s %s」尚未完成（负责人：%s，当前状态：%s），无法启动本环节",
				s.StageCode, s.StageName, s.Assignee, s.Status), nil
		}
	}
	return "", nil
}

// CloseCentralizedProject POST /centralized-projects/:remote_id/close
//
// 项目负责人结项。透传到 manage，closer 由后端补 currentOperator。
// manage 端会做完整校验：owner_name == closer + 所有 stage completed。
// 单位级项目：请求体可带 move_file_ids（部门柜定稿文件 id），随结项归卷到单位室。
func CloseCentralizedProject(c *gin.Context) {
	remoteID := c.Param("remote_id")
	if _, err := strconv.ParseInt(remoteID, 10, 64); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid remote_id"})
		return
	}
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	var in map[string]any
	_ = c.ShouldBindJSON(&in)
	if in == nil {
		in = map[string]any{}
	}
	in["closer"] = currentOperator(c)
	body, _ := json.Marshal(in)

	url := fmt.Sprintf("%s/api/centralized-projects/close?id=%s", endpoint, remoteID)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": out.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out.Data})
}

// UpdateCentralizedProjectDraftRequest PUT /centralized-projects/draft 入参（id 在 body）
type UpdateCentralizedProjectDraftRequest struct {
	ID int64 `json:"id"`
	CreateCentralizedProjectRequest
}

// UpdateCentralizedProjectDraft PUT /centralized-projects/draft
//
// 仅 status='draft' 且属于当前提交者的记录可改。save_as_draft=false 即"从草稿发布"。
func UpdateCentralizedProjectDraft(c *gin.Context) {
	var req UpdateCentralizedProjectDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "id 必填"})
		return
	}
	db := repository.GetDB()
	operator := currentOperator(c)

	var cur struct {
		ID     int64  `db:"id"`
		Status string `db:"status"`
	}
	if err := db.Get(&cur, `SELECT id, status FROM centralized_project_applications
	                          WHERE id=? AND submitted_by=? AND disable=0`, req.ID, operator); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "草稿不存在或无权编辑"})
		return
	}
	if cur.Status != "draft" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "仅草稿可编辑"})
		return
	}

	projectName := strings.TrimSpace(req.ProjectName)
	ownerName := strings.TrimSpace(req.OwnerName)
	department := strings.TrimSpace(req.Department)
	dataOwner := strings.TrimSpace(req.DataOwner)
	projectCode := strings.TrimSpace(req.ProjectCode)
	approvalBasis := strings.TrimSpace(req.ApprovalBasis)
	description := strings.TrimSpace(req.Description)
	sensitivity := strings.ToLower(strings.TrimSpace(req.SensitivityLevel))
	projectScope := strings.ToLower(strings.TrimSpace(req.ProjectScope))
	custodyScope := strings.ToLower(strings.TrimSpace(req.OutputCustodyScope))
	custodyNote := strings.TrimSpace(req.OutputCustodyNote)
	draft := req.SaveAsDraft

	if projectName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "项目名称必填"})
		return
	}
	if !draft {
		if ownerName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "负责人必填"})
			return
		}
		if sensitivity != "core" && sensitivity != "important" && sensitivity != "general" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": "项目敏感等级必填，且仅支持 core / important / general"})
			return
		}
		_ = syncUsersFromManage(c)
		userRepo := repository.NewUserRepository(repository.GetDB())
		if u, err := userRepo.FindByUsername(ownerName); err != nil || u == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": fmt.Sprintf("负责人「%s」未在系统中注册（或已禁用），请到管理端用户列表建账号后再立项", ownerName)})
			return
		}
	}
	if sensitivity != "core" && sensitivity != "important" && sensitivity != "general" {
		sensitivity = "general"
	}
	if projectScope != "person" && projectScope != "department" && projectScope != "unit" {
		projectScope = "unit"
	}
	if !(projectScope == "unit" && custodyScope == "department") {
		custodyScope = "unit"
	}

	status := "draft"
	if !draft {
		status = "approved"
	}
	now := time.Now()
	if _, err := db.Exec(`UPDATE centralized_project_applications
		SET project_name=?, project_code=?, owner_name=?, department=?, data_owner=?,
		    approval_basis=?, description=?, sensitivity_level=?, project_scope=?, output_custody_scope=?, output_custody_note=?, status=?, update_time=?
		WHERE id=? AND submitted_by=?`,
		projectName, projectCode, ownerName, department, dataOwner,
		approvalBasis, description, sensitivity, projectScope, custodyScope, custodyNote, status, now, req.ID, operator); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	syncStatus := "pending"
	projectCodeResp := ""
	if !draft {
		remoteID, code, pushErr := pushCentralizedProjectToManage(db, req.ID, projectName, ownerName, dataOwner, operator, sensitivity, projectScope, custodyScope, custodyNote, department, approvalBasis, description)
		if pushErr != nil {
			_, _ = db.Exec(`UPDATE centralized_project_applications
			                  SET sync_status='failed', sync_error=?, update_time=? WHERE id=?`,
				pushErr.Error(), time.Now(), req.ID)
			syncStatus = "failed"
		} else {
			// 用 manage 生成的唯一编号覆盖（草稿阶段的手填项目代号不再作为唯一标识）。
			_, _ = db.Exec(`UPDATE centralized_project_applications
			                  SET sync_status='synced', sync_error=NULL, manage_remote_id=?, project_code=?, update_time=? WHERE id=?`,
				remoteID, code, time.Now(), req.ID)
			syncStatus = "synced"
			projectCodeResp = code
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"id": req.ID, "status": status, "sync_status": syncStatus, "project_code": projectCodeResp},
	})
}

// getManageEndpoint 取已配置的 manage 地址（去尾斜杠），出错时返回 error。
func getManageEndpoint() (string, error) {
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	ep := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if ep == "" {
		return "", fmt.Errorf("未配置 manage_endpoint")
	}
	return ep, nil
}

// proxyToManage 转发请求并把 manage 的 {code,message,data} 翻成 scan 的 {success,data,error}。
func proxyToManage(c *gin.Context, method, url string, body []byte) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "构建请求失败: " + err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "解析 manage 响应失败"})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": out.Message})
		return
	}
	var data any
	_ = json.Unmarshal(out.Data, &data)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// CompleteTaskProxy POST /centralized-projects/complete-task —— 注入 actor=operator 转发 manage。
func CompleteTaskProxy(c *gin.Context) {
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
	proxyToManage(c, "POST", ep+"/api/centralized-projects/complete-task", b)
}

// SetTemplateProxy POST /centralized-projects/set-template?id=
//
// 选定模版时不能只把 scan/平台侧的 template_id 透传给 manage —— manage 的任务层
// (stage-tasks) 要按 application.template_id 在【manage 自己的库】里查 template_stages/
// template_tasks，而远程/本地模版的结构原本都不在 manage 库里。
// 故这里先确保模版落到 scan 本地（远程模版按 code 从模版服务器拉进缓存），再把完整
// 五层结构 ingest 进 manage（幂等 by code+version，返回 manage 侧 template_id），最后用
// 该 manage 侧 id 调 set-template。这样 manage 才查得到工作环节下的文件任务。
// resolveManageTemplateID 确保所选模版结构进了 manage 库，返回 manage 侧 template_id。
// body 需含 template_code/template_version/source(/template_id)。流程：
// ①确保模版在 scan 本地（source=template-server 时按 code 从模版服务器拉进缓存；
//
//	否则用 body.template_id 作 scan 本地 id）②PushTemplateToManage(非立项，幂等
//	by code+version) 把五层灌进 manage，拿 manage 侧 id。
//
// set-template 与 accept 都必须用它换出的 manage 侧 id 写 application.template_id，
// 否则任务层(stage-tasks) 按 application.template_id 在 manage 库查不到环节/任务。
// resolveManageTemplateID 解析所选模版到 scan 本地，再克隆为「项目专属模版」（按 appKey 确定化、
// 幂等复用），最后整树 ingest 进 manage 返回 manage 侧 template_id。appKey 为该集中立项项目的
// manage 远端 id：以此保证每个项目编辑自己的副本、互不污染共享模版；并把 body 的
// template_code/template_version 改写为副本的，让 manage 的 application 记录指向项目专属模版。
func resolveManageTemplateID(ep string, body map[string]any, appKey string) (int64, error) {
	code, _ := body["template_code"].(string)
	version, _ := body["template_version"].(string)
	source, _ := body["source"].(string)

	db := repository.GetDB()
	authRepo := repository.NewTemplateAuthoringRepository(db)

	var localID int64
	if source == "template-server" {
		if code == "" {
			return 0, fmt.Errorf("source=template-server 时需要 template_code")
		}
		fetcher := repository.NewTemplateFetcher(repository.NewTemplateCacheRepository(db), repository.NewSystemConfigRepository(db))
		id, err := fetcher.FetchFromTemplateServer(code, version)
		if err != nil {
			return 0, fmt.Errorf("拉取在线模版失败: %w", err)
		}
		localID = id
	} else {
		switch v := body["template_id"].(type) {
		case float64:
			localID = int64(v)
		case string:
			localID, _ = strconv.ParseInt(v, 10, 64)
		}
		if localID == 0 {
			return 0, fmt.Errorf("缺少 template_id")
		}
	}

	// 关联即克隆：把所选模版深拷贝成本项目专属模版（编辑隔离、不污染共享模版）。
	if strings.TrimSpace(appKey) != "" {
		// 克隆前先用源树构建基线快照（克隆后 localID 会被改写为项目专属模版 id）。
		var baselineJSON, srcCode, srcVer string
		if srcTree, err := authRepo.GetLocalTemplateTree(localID); err == nil {
			baselineJSON, _ = authRepo.BuildBaselineSnapshotJSON(srcTree)
			srcCode = srcTree.Template.TemplateCode
			srcVer = srcTree.Template.TemplateVersion
		}
		projID, err := authRepo.CloneLocalTemplateForApplication(localID, appKey)
		if err != nil {
			return 0, fmt.Errorf("克隆项目专属模版失败: %w", err)
		}
		localID = projID
		// 让 manage 的 application 记录指向项目专属模版的编码/版本。
		if proj, err := authRepo.GetLocalTemplate(projID); err == nil {
			body["template_code"] = proj.TemplateCode
			body["template_version"] = proj.TemplateVersion
		}
		// 基线推到 manage 云端存放（任意端均可在提取前对比差异）；失败不阻断关联。
		if baselineJSON != "" {
			pushTemplateBaselineToManage(ep, appKey, srcCode, srcVer, baselineJSON)
		}
	}

	manageTplID, err := authRepo.PushTemplateToManage(nil, ep, localID, false, "")
	if err != nil {
		return 0, fmt.Errorf("同步模版结构到 manage 失败: %w", err)
	}
	return manageTplID, nil
}

// pushTemplateBaselineToManage 把项目模版基线快照推到 manage 云端存放（提取前差异对比用）。
// 一项目一条、首次写入即固化（manage 侧 INSERT OR IGNORE）；失败仅记日志、不阻断关联流程。
func pushTemplateBaselineToManage(ep, appKey, sourceCode, sourceVersion, baselineJSON string) {
	payload, _ := json.Marshal(map[string]any{
		"source_code":    sourceCode,
		"source_version": sourceVersion,
		"baseline_json":  baselineJSON,
	})
	url := strings.TrimRight(ep, "/") + "/api/centralized-projects/template-baseline?application_id=" + encodeQuery(appKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[template-baseline] 推送基线到 manage 失败(app=%s): %v", appKey, err)
		return
	}
	_ = resp.Body.Close()
}

func SetTemplateProxy(c *gin.Context) {
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

	manageTplID, rerr := resolveManageTemplateID(ep, body, c.Query("id"))
	if rerr != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": rerr.Error()})
		return
	}

	// 用 manage 侧 template_id 调 set-template（任务层据此查 template_stages/template_tasks）。
	body["template_id"] = manageTplID
	body["acceptor"] = currentOperator(c)
	b, _ := json.Marshal(body)
	proxyToManage(c, "POST", fmt.Sprintf("%s/api/centralized-projects/set-template?id=%s", ep, c.Query("id")), b)
}

// UnreadCountProxy GET /centralized-projects/unread-count —— owner=operator（query）。
func UnreadCountProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/unread-count?owner=%s", ep, currentOperator(c)), nil)
}

// MarkSeenProxy POST /centralized-projects/mark-seen —— owner=operator（body）。
func MarkSeenProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	b, _ := json.Marshal(map[string]any{"owner": currentOperator(c)})
	proxyToManage(c, "POST", ep+"/api/centralized-projects/mark-seen", b)
}

// StageTasksProxy GET /centralized-projects/stage-tasks?application_id=&stage_code=
func StageTasksProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	url := fmt.Sprintf("%s/api/centralized-projects/stage-tasks?application_id=%s&stage_code=%s", ep, encodeQuery(c.Query("application_id")), encodeQuery(c.Query("stage_code")))
	proxyToManage(c, "GET", url, nil)
}

// AssignTasksProxy POST /centralized-projects/assign-tasks?application_id=&stage_code= —— 注入 actor=operator。
func AssignTasksProxy(c *gin.Context) {
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
	url := fmt.Sprintf("%s/api/centralized-projects/assign-tasks?application_id=%s&stage_code=%s", ep, encodeQuery(c.Query("application_id")), encodeQuery(c.Query("stage_code")))
	proxyToManage(c, "POST", url, b)
}

// StageUnreadCountProxy GET /centralized-projects/stage-unread-count —— assignee=operator。
func StageUnreadCountProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/stage-unread-count?assignee=%s", ep, encodeQuery(currentOperator(c))), nil)
}

// MarkStagesSeenProxy POST /centralized-projects/mark-stages-seen —— assignee=operator（body）。
func MarkStagesSeenProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	b, _ := json.Marshal(map[string]any{"assignee": currentOperator(c)})
	proxyToManage(c, "POST", ep+"/api/centralized-projects/mark-stages-seen", b)
}

// ListMySubmissions GET /centralized-projects/my-submissions
//
// 拉 manage 端 submitted_by == currentOperator 的所有项目，含完整状态字段
// （accepted_at / closed_at / closure_summary 等）。供「数据项目结项管理」
// 页面使用。
func ListMySubmissions(c *gin.Context) {
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if endpoint == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置 manage_endpoint"})
		return
	}
	owner := currentOperator(c)
	if owner == "" || owner == "system" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []interface{}{}})
		return
	}
	url := fmt.Sprintf("%s/api/centralized-projects/list?submitted_by=%s", endpoint, encodeQuery(owner))
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code int `json:"code"`
		Data any `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "manage 返回非 0"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out.Data})
}

// MyTasksProxy GET /centralized-projects/my-tasks —— assignee=operator。
func MyTasksProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/my-tasks?assignee=%s", ep, encodeQuery(currentOperator(c))), nil)
}

// TaskUnreadCountProxy GET /centralized-projects/task-unread-count —— assignee=operator。
func TaskUnreadCountProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/task-unread-count?assignee=%s", ep, encodeQuery(currentOperator(c))), nil)
}

// MarkTasksSeenProxy POST /centralized-projects/mark-tasks-seen —— assignee=operator（body）。
func MarkTasksSeenProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	b, _ := json.Marshal(map[string]any{"assignee": currentOperator(c)})
	proxyToManage(c, "POST", ep+"/api/centralized-projects/mark-tasks-seen", b)
}

// StartTaskHandler POST /centralized-projects/start-task —— 调 manage 置 in_progress + 本地按任务建过程占位。
func StartTaskHandler(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	var body struct {
		ApplicationID   int64  `json:"application_id"`
		StageCode       string `json:"stage_code"`
		TaskCode        string `json:"task_code"`
		TemplateCode    string `json:"template_code"`
		TemplateVersion string `json:"template_version"`
		ProjectCode     string `json:"project_code"` // manage 生成的立项编号，作目录名
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if body.ApplicationID == 0 || body.StageCode == "" || body.TaskCode == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "参数不完整"})
		return
	}
	mb, _ := json.Marshal(map[string]any{
		"actor": currentOperator(c), "application_id": body.ApplicationID,
		"stage_code": body.StageCode, "task_code": body.TaskCode,
	})
	mreq, err := http.NewRequest("POST", ep+"/api/centralized-projects/start-task", bytes.NewReader(mb))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	mreq.Header.Set("Content-Type", "application/json")
	mresp, err := (&http.Client{Timeout: 15 * time.Second}).Do(mreq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer mresp.Body.Close()
	raw, _ := io.ReadAll(mresp.Body)
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": out.Message})
		return
	}
	db := repository.GetDB()
	vp := centralizedDirCode(db, body.ApplicationID, body.ProjectCode)

	// 参与人本地可能没有该模版：先确保本地有，否则按 code+version 从 manage 自动拉取并落地，
	// 避免 ScaffoldTaskDocsForProject 抛出晦涩的「本地模版缺失/sql: no rows」错误。
	var localCnt int
	_ = db.Get(&localCnt, `SELECT COUNT(*) FROM data_templates WHERE template_code = ? AND disable = 0`, body.TemplateCode)
	if localCnt == 0 {
		cfgRepo := repository.NewSystemConfigRepository(db)
		fetcher := repository.NewTemplateFetcher(repository.NewTemplateCacheRepository(db), cfgRepo)
		if _, ferr := fetcher.FetchByCode(body.TemplateCode, body.TemplateVersion); ferr != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "本地缺少该模版且自动同步失败，请到「模板库」同步后再开始：" + ferr.Error()})
			return
		}
	}

	wsroot := repository.NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := repository.NewProjectWorkspace(wsroot)
	// 五层落盘：建该文件任务的三态目录（stages/{stage}/{task}/{input,process,output}）。
	if _, err := ws.CreateTaskDir(vp, body.StageCode, body.TaskCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "建目录失败: " + err.Error()})
		return
	}
	created, scErr := repository.ScaffoldTaskDocsForProject(db, body.TemplateCode, vp, body.StageCode, body.TaskCode)
	if scErr != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": scErr.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"scaffolded": len(created), "app_id": body.ApplicationID, "stage_code": body.StageCode,
	}})
}
