package httpd

import (
	"net/http"
	"strconv"
	"time"

	"data-asset-scan-go/internal/models"
	"data-asset-scan-go/internal/repository"
	"data-asset-scan-go/internal/scanner"
	"github.com/gin-gonic/gin"
)

// RegisterTasksRoutes registers /tasks routes
func RegisterTasksRoutes(r *gin.RouterGroup) {
	r.GET("", GetTasks)
	r.GET("/:id", GetTaskDetail)
	r.POST("", CreateTask)
	r.DELETE("/:id", DeleteTask)
	r.POST("/:id/start", StartTask)
}

// ScanTask represents a scan task
type ScanTask struct {
	ID                   int64   `json:"id"`
	ScanType             string  `json:"scan_type"`
	FileScanRange        *string `json:"file_scan_range"`
	Heartbeat            int     `json:"heartbeat"`
	WorkspacePath        *string `json:"workspace_path"`
	TaskState            string  `json:"task_state"`
	TaskPhase            *string `json:"task_phase"`
	TaskErrorMessage     *string `json:"task_error_message"`
	ScanArgs             *string `json:"scan_args"`
	FileTotal            *int    `json:"file_total"`
	FileScannedCount     *int    `json:"file_scanned_count"`
	FileAllSuffixText    *string `json:"file_all_suffix_text"`
	FileAllSuffixCount   *int    `json:"file_all_suffix_count"`
	FileCountSuffixCount *int    `json:"file_count_suffix_count"`
	WorkspaceCount       *int    `json:"workspace_count"`
	EndTime              *string `json:"end_time"`
	ScanLog              *string `json:"scan_log"`
	CreateTime           string  `json:"create_time"`
	UpdateTime           string  `json:"update_time"`
	Disable              int     `json:"disable"`
	ParamsChanged        struct {
		WorkspacePathChanged bool `json:"workspacePathChanged"`
		ScanAreaPathChanged  bool `json:"scanAreaPathChanged"`
		ControlTypeChanged   bool `json:"controlTypeChanged"`
	} `json:"paramsChanged"`
}

// TaskProgressEvent represents SSE progress event
type TaskProgressEvent struct {
	Type          string `json:"type"`
	TaskID        int    `json:"taskId,omitempty"`
	Phase         string `json:"phase,omitempty"`
	ScannedCount  int    `json:"scannedCount"`
	TotalCount    int    `json:"totalCount"`
	CurrentFile   string `json:"currentFile,omitempty"`
	ElapsedMs     int64  `json:"elapsedMs"`
	Success       bool   `json:"success,omitempty"`
	ErrorMessage  string `json:"errorMessage,omitempty"`
	NewFiles      int    `json:"newFiles,omitempty"`
	NormalFiles   int    `json:"normalFiles,omitempty"`
	DeletedFiles  int    `json:"deletedFiles,omitempty"`
	ModifiedFiles int    `json:"modifiedFiles,omitempty"`
}

// GetTasks handles GET /tasks
// Query params: page, pageSize
func GetTasks(c *gin.Context) {
	page := 1
	pageSize := 20

	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if ps := c.Query("pageSize"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}

	taskRepo := repository.NewScanTaskRepository(repository.GetDB())

	result, err := taskRepo.GetTasksWithPagination(models.ScanTaskQueryParams{
		Page:     page,
		PageSize: pageSize,
	})

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to get tasks",
		})
		return
	}

	// Convert to response format
	tasks := make([]ScanTask, len(result.Tasks))
	for i, t := range result.Tasks {
		tasks[i] = ScanTask{
			ID:                   t.ID,
			ScanType:             string(t.ScanType),
			FileScanRange:        t.FileScanRange,
			Heartbeat:            t.Heartbeat,
			WorkspacePath:        t.WorkspacePath,
			TaskState:            string(t.TaskState),
			TaskPhase:            t.TaskPhase,
			TaskErrorMessage:     t.TaskErrorMessage,
			ScanArgs:             t.ScanArgs,
			FileTotal:            t.FileTotal,
			FileScannedCount:     t.FileScannedCount,
			FileAllSuffixText:    t.FileAllSuffixText,
			FileAllSuffixCount:   t.FileAllSuffixCount,
			FileCountSuffixCount: t.FileCountSuffixCount,
			WorkspaceCount:       t.WorkspaceCount,
			EndTime:              nil,
			ScanLog:              t.ScanLog,
			CreateTime:           t.CreateTime.Format("2006-01-02 15:04:05"),
			UpdateTime:           t.UpdateTime.Format("2006-01-02 15:04:05"),
			Disable:              t.Disable,
		}
		tasks[i].ParamsChanged.WorkspacePathChanged = t.ParamsChanged.WorkspacePathChanged
		tasks[i].ParamsChanged.ScanAreaPathChanged = t.ParamsChanged.ScanAreaPathChanged
		tasks[i].ParamsChanged.ControlTypeChanged = t.ParamsChanged.ControlTypeChanged

		if t.EndTime != nil {
			endTimeStr := t.EndTime.Format("2006-01-02 15:04:05")
			tasks[i].EndTime = &endTimeStr
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"tasks":    tasks,
			"total":    result.Total,
			"page":     result.Page,
			"pageSize": result.PageSize,
		},
	})
}

// CreateTaskRequest represents the request body for creating a task
type CreateTaskRequest struct {
	ScanType      string `json:"scan_type"`
	FileScanRange string `json:"file_scan_range"`
	WorkspacePath string `json:"workspace_path"`
	ScanArgs      string `json:"scan_args"`
	FileTotal     int    `json:"file_total"`
}

// CreateTask handles POST /tasks
func CreateTask(c *gin.Context) {
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request body",
		})
		return
	}

	taskRepo := repository.NewScanTaskRepository(repository.GetDB())

	var scanType models.ScanType = models.ScanTypeFile
	if req.ScanType == "DATABASE" {
		scanType = models.ScanTypeDatabase
	}

	var fileScanRange *string
	if req.FileScanRange != "" {
		fileScanRange = &req.FileScanRange
	}

	var workspacePath *string
	if req.WorkspacePath != "" {
		workspacePath = &req.WorkspacePath
	}

	var scanArgs *string
	if req.ScanArgs != "" {
		scanArgs = &req.ScanArgs
	}

	var fileTotal *int
	if req.FileTotal > 0 {
		fileTotal = &req.FileTotal
	}

	taskID, err := taskRepo.Create(models.CreateScanTaskParams{
		ScanType:      scanType,
		FileScanRange: fileScanRange,
		WorkspacePath: workspacePath,
		ScanArgs:      scanArgs,
		FileTotal:     fileTotal,
	})

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to create task",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"taskId": taskID,
		},
	})
}

// DeleteTask handles DELETE /tasks/:id
func DeleteTask(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid task ID",
		})
		return
	}

	taskRepo := repository.NewScanTaskRepository(repository.GetDB())

	// Get the task first to verify it exists
	task, err := taskRepo.GetByID(taskID)
	if err != nil || task == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Task not found",
		})
		return
	}

	// Mark as disabled (soft delete)
	now := time.Now()
	query := `UPDATE scan_task SET disable = 1, update_time = ? WHERE id = ?`
	_, err = repository.GetDB().Exec(query, now, taskID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to delete task",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Task deleted",
	})
}

// StartTask handles POST /tasks/:id/start
func StartTask(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid task ID",
		})
		return
	}

	taskRepo := repository.NewScanTaskRepository(repository.GetDB())
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())

	// Get the task
	task, err := taskRepo.GetByID(taskID)
	if err != nil || task == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Task not found",
		})
		return
	}

	// Get scan parameters from config
	workspace := configRepo.GetWorkspace()
	scanAreaPath := configRepo.GetScanAreaPath()
	controlType := configRepo.GetControlType()
	excludeDirs := configRepo.GetScanExcludeDir()
	saveCode := configRepo.GetSaveCode()

	// Parse extensions
	extensions := parseExtensions(controlType)
	if len(extensions) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "No file extensions configured",
		})
		return
	}

	// Parse exclude directories
	var excludeDirList []string
	if excludeDirs != "" {
		for _, d := range splitAndTrim(excludeDirs, ",") {
			if d != "" {
				excludeDirList = append(excludeDirList, d)
			}
		}
	}

	// Determine scan mode
	var scanMode scanner.ScanMode
	if task.ScanArgs != nil && *task.ScanArgs != "" {
		scanMode = scanner.ScanMode(*task.ScanArgs)
	} else {
		scanMode = scanner.FullInventory
	}

	// Create atomic scanner
	atomicScanner := scanner.NewAtomicScanner(repository.GetDB(), 100)

	// Start scan in a goroutine
	go func() {
		result := atomicScanner.Scan(scanner.AtomicScanOptions{
			Directory:        scanAreaPath,
			Extensions:       extensions,
			ExcludeDirs:      excludeDirList,
			Workspace:        workspace,
			MD5Concurrency:   4,
			BatchSize:        100,
			ProgressInterval: 10,
			ScanMode:         scanMode,
			SaveCode:         saveCode,
		})

		// Log result
		_ = result
	}()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Task started",
	})
}

// convertScanTaskToResponse converts a ScanTask model to the API response struct
func convertScanTaskToResponse(t models.ScanTask) ScanTask {
	resp := ScanTask{
		ID:                   t.ID,
		ScanType:             string(t.ScanType),
		FileScanRange:        t.FileScanRange,
		Heartbeat:            t.Heartbeat,
		WorkspacePath:        t.WorkspacePath,
		TaskState:            string(t.TaskState),
		TaskPhase:            t.TaskPhase,
		TaskErrorMessage:     t.TaskErrorMessage,
		ScanArgs:             t.ScanArgs,
		FileTotal:            t.FileTotal,
		FileScannedCount:     t.FileScannedCount,
		FileAllSuffixText:    t.FileAllSuffixText,
		FileAllSuffixCount:   t.FileAllSuffixCount,
		FileCountSuffixCount: t.FileCountSuffixCount,
		WorkspaceCount:       t.WorkspaceCount,
		ScanLog:              t.ScanLog,
		CreateTime:           t.CreateTime.Format("2006-01-02 15:04:05"),
		UpdateTime:           t.UpdateTime.Format("2006-01-02 15:04:05"),
		Disable:              t.Disable,
	}
	// ParamsChanged is computed from cross-task comparison and not stored on
	// the bare ScanTask model; leave fields at zero for the single-task detail
	// endpoint. The list endpoint computes it via ScanTaskWithParamsChanged.

	if t.EndTime != nil {
		s := t.EndTime.Format("2006-01-02 15:04:05")
		resp.EndTime = &s
	}
	return resp
}

// GetTaskDetail handles GET /scan-tasks/:id
func GetTaskDetail(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid task ID",
		})
		return
	}

	taskRepo := repository.NewScanTaskRepository(repository.GetDB())
	task, err := taskRepo.GetByID(taskID)
	if err != nil || task == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Task not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    convertScanTaskToResponse(*task),
	})
}
