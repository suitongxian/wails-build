package httpd

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"data-asset-scan-go/internal/repository"
	"data-asset-scan-go/internal/scanner"
	"github.com/gin-gonic/gin"
)

// RegisterScanRoutes registers /scan routes
func RegisterScanRoutes(r *gin.RouterGroup) {
	r.POST("/start", StartScan)
	r.GET("/current", GetCurrentScan)
	r.POST("/stop", StopScan)
}

// activeScanner tracks the currently running scanner so /scan/stop can cancel it.
// Only one scan runs at a time; concurrency is enforced via DB query for any
// task in 'run' state.
var (
	activeScanner   *scanner.AtomicScanner
	activeTaskID    int64
	activeScannerMu sync.Mutex
)

// StartScanRequest is the body of POST /scan/start
type StartScanRequest struct {
	ScanMode string `json:"scan_mode"`
}

// StartScan handles POST /scan/start
// Body: { "scan_mode": "FULL_INVENTORY" | "DAILY_CHECK" | "TARGETED_SCAN" }
// Returns immediately with the task id; progress is polled via
// GET /scan-tasks/:id.
func StartScan(c *gin.Context) {
	var req StartScanRequest
	_ = c.ShouldBindJSON(&req)

	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	taskRepo := repository.NewScanTaskRepository(repository.GetDB())

	// Refuse if a scan is already running (DB is the source of truth)
	if running, err := taskRepo.GetRunningTask(); err == nil && running != nil {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "已有扫描任务在进行中",
			"data":    gin.H{"taskId": running.ID},
		})
		return
	}

	// Validate config
	workspace := configRepo.GetWorkspace()
	scanAreaPath := configRepo.GetScanAreaPath()
	controlType := configRepo.GetControlType()
	excludeDirs := configRepo.GetScanExcludeDir()
	saveCode := configRepo.GetSaveCode()

	if workspace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请先配置工作空间目录"})
		return
	}
	if controlType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "配置中缺少管控文件类型 (control_type)"})
		return
	}

	extensions := scanner.ParseExtensions(controlType)
	if len(extensions) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的管控文件类型配置"})
		return
	}

	var excludeDirList []string
	if excludeDirs != "" {
		for _, d := range splitAndTrim(excludeDirs, ",") {
			if d != "" {
				excludeDirList = append(excludeDirList, d)
			}
		}
	}
	// 2026-06-01 主路径与兜底分域：只排除"数据业务模版项目目录"子树（按全路径前缀），不排除整个工作空间。
	//   - 项目目录(含 stages/ 子目录) → 主路径自动归档管辖，通用扫描不得触碰，避免抢文件；
	//   - 工作空间内项目目录之外的散文件 → 照常被扫到 → 自动认领，进「认领文件归档保护」手动归档。
	// 用全路径前缀(非目录名)排除：避免目录重名误伤、且扫描区=工作空间本身时也能精确排除。
	var excludePathList []string
	if projectRoot := configRepo.GetEffectiveProjectRoot(); projectRoot != "" {
		if entries, err := os.ReadDir(projectRoot); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				// 含 stages/ 子目录者视为"项目目录"（与工作空间巡检的结构假设一致）。
				if directoryExists(filepath.Join(projectRoot, e.Name(), "stages")) {
					excludePathList = append(excludePathList, filepath.Join(projectRoot, e.Name()))
				}
			}
		}
	}

	// Parse scan mode
	var scanMode scanner.ScanMode
	switch req.ScanMode {
	case "FULL_INVENTORY", "":
		scanMode = scanner.FullInventory
	case "DAILY_CHECK":
		scanMode = scanner.DailyCheck
	case "TARGETED_SCAN":
		scanMode = scanner.TargetedScan
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid scan_mode. Valid values: FULL_INVENTORY, DAILY_CHECK, TARGETED_SCAN",
		})
		return
	}

	// Snapshot the current max task id so we can detect the row that the
	// scanner goroutine is about to create — even if the scan fails fast and
	// the row transitions to 'fail' before our spin-wait sees it as 'run'.
	prevMaxID, _ := taskRepo.GetMaxTaskID()

	// Create scanner. The Scan goroutine is responsible for creating the task
	// row, persisting progress, and writing the terminal task_state.
	atomicScanner := scanner.NewAtomicScanner(repository.GetDB(), 100)

	activeScannerMu.Lock()
	activeScanner = atomicScanner
	activeTaskID = 0
	activeScannerMu.Unlock()

	// Run scan asynchronously
	go func() {
		defer func() {
			activeScannerMu.Lock()
			if activeScanner == atomicScanner {
				activeScanner = nil
				activeTaskID = 0
			}
			activeScannerMu.Unlock()
			atomicScanner.CloseProgressChan()
		}()

		_ = atomicScanner.Scan(scanner.AtomicScanOptions{
			Directory:        scanAreaPath,
			Extensions:       extensions,
			ExcludeDirs:      excludeDirList,
			ExcludePaths:     excludePathList,
			Workspace:        workspace,
			MD5Concurrency:   4,
			BatchSize:        100,
			ProgressInterval: 10,
			ScanMode:         scanMode,
			SaveCode:         saveCode,
		})

		// 2026-06-01 工作空间自动归档巡检：通用扫描已排除 project_root，
		// 故在此随盘点一起把工作空间各环节产出按主路径入账归档（幂等、独立于「完成」），
		// 避免用户长期不点完成导致工作文件游离管外的盲区。
		_, _ = repository.AutoArchiveAllWorkspace(repository.GetDB())
	}()

	// Wait briefly for the scanner goroutine to insert its task row so we can
	// return the id. We compare against prevMaxID (rather than querying for a
	// 'run' state) so fast-failing scans — which create the row and almost
	// immediately MarkFailed — still hand back a usable id; the frontend's
	// poll on that id will then show task_state='fail' on the very next tick.
	var taskID int64
	for i := 0; i < 40; i++ {
		if id, err := taskRepo.GetMaxTaskID(); err == nil && id > prevMaxID {
			taskID = id
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"taskId": taskID},
	})
}

// GetCurrentScan handles GET /scan/current
// Returns the running task (or null if none).
func GetCurrentScan(c *gin.Context) {
	taskRepo := repository.NewScanTaskRepository(repository.GetDB())
	task, err := taskRepo.GetRunningTask()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    convertScanTaskToResponse(*task),
	})
}

// StopScan handles POST /scan/stop
// Stops the currently active scan, if any.
//
// We claim activeScanner (set to nil) before calling Stop so a second
// concurrent stop request sees nil instead of trying to close the scanner's
// stopChan a second time, which would panic.
//
// NOTE: scanner.AtomicScanner.Stop() currently only closes stopChan and the
// scan loop does not yet observe it, so this is a no-op for the running scan
// itself; the pre-existing scanner-side issue is tracked separately.
func StopScan(c *gin.Context) {
	activeScannerMu.Lock()
	s := activeScanner
	activeScanner = nil
	activeScannerMu.Unlock()

	if s == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "No scan is currently running"})
		return
	}

	s.Stop()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Scan stop requested"})
}

// Helper functions

// parseExtensions 已废弃 —— 它会去掉前导点 ".pdf" → "pdf"，与 scanner 内部用
// filepath.Ext() 返回带点后缀的对比方式不兼容，导致扫描器把所有文件都过滤掉。
// 改用 scanner.ParseExtensions（会确保前导点存在，符合 filepath.Ext 输出）。
// 历史 bug 之所以没暴露，是因为之前用户的 workspace 就是 scan_area_path 本身或父目录，
// CollectWorkspaceSuffixes 会从那里收集带点后缀填进 extSet 救场。自动创建空 workspace 后救场消失。
func parseExtensions(controlType string) []string {
	return scanner.ParseExtensions(controlType)
}

func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(s, sep) {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// directoryExists checks if a directory exists
func directoryExists(dirPath string) bool {
	if info, err := os.Stat(dirPath); err == nil {
		return info.IsDir()
	}
	return false
}
