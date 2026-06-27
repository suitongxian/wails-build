package httpd

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"data-asset-scan-go/internal/repository"
	"data-asset-scan-go/internal/similarity"

	"github.com/gin-gonic/gin"
)

// PreviewAnalyze POST /analyze/preview
// 给「重建相似关系」二次确认对话框估算耗时和待重算文件数。
// cache_miss_count = files needing re-extraction:
//   - feature_mtime/feature_size NULL (never cached)
//   - OR current file mtime/size differs from cached values (stale)
//   - OR stat fails (file gone — BuildFamilies fallback will handle)
func PreviewAnalyze(c *gin.Context) {
	db := repository.GetDB()

	// Count files that need re-extraction via mtime/size stat-based detection.
	type row struct {
		Path         string     `db:"path"`
		FeatureMtime *time.Time `db:"feature_mtime"`
		FeatureSize  *int64     `db:"feature_size"`
	}
	var rows []row
	_ = db.Select(&rows,
		`SELECT DISTINCT path, feature_mtime, feature_size FROM data_distributing WHERE disable = 0`)

	cacheMissCount := 0
	for _, r := range rows {
		if r.FeatureMtime == nil || r.FeatureSize == nil {
			cacheMissCount++
			continue
		}
		info, err := os.Stat(r.Path)
		if err != nil {
			// File gone or unreadable; count as miss (BuildFamilies will skip)
			cacheMissCount++
			continue
		}
		if info.Size() != *r.FeatureSize || !info.ModTime().Equal(*r.FeatureMtime) {
			cacheMissCount++
		}
	}

	taskRepo := repository.NewSimilarityTaskRepository(db)
	var lastRunAt interface{}
	var lastRunDurationSec int64
	if latest, err := taskRepo.LatestSucceeded(); err == nil && latest != nil {
		lastRunAt = latest.EndTime
		if latest.EndTime != nil {
			lastRunDurationSec = int64(latest.EndTime.Sub(latest.StartTime).Seconds())
		}
	}

	// family_stale：扫描产生过、但还没跑过相似度分析（或跑完后又重新扫描）→ 必须重建。
	// 不在 cache_miss_count 范畴里：即便所有特征值都新鲜，家族表也可能是空 / 残缺的。
	cfgRepo := repository.NewSystemConfigRepository(db)
	familyStale := cfgRepo.GetValue(repository.KeyFamilyDirty) == "1"

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"cache_miss_count":      cacheMissCount,
			"family_stale":          familyStale,
			"last_run_at":           lastRunAt,
			"last_run_duration_sec": lastRunDurationSec,
		},
	})
}

// runMu serializes analyze runs at the process level (DB-level guard is in
// SimilarityTaskRepository.HasRunning).
var runMu sync.Mutex

func RegisterSimilarityRoutes(r *gin.RouterGroup) {
	r.POST("/analyze/preview", PreviewAnalyze) // 新增：重建相似关系二次确认估算
	r.POST("/analyze", StartSimilarityAnalysis)
	r.GET("/task/:id", GetSimilarityTask)
	r.GET("/task/latest", GetLatestSimilarityTask)
}

// StartSimilarityAnalysis kicks off a goroutine that runs the analyzer.
// Returns 409 if a task is already running.
func StartSimilarityAnalysis(c *gin.Context) {
	taskRepo := repository.NewSimilarityTaskRepository(repository.GetDB())

	running, err := taskRepo.HasRunning()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	if running {
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": "an analysis task is already running"})
		return
	}

	taskID, err := taskRepo.Create()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	go runAnalysis(taskID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"task_id": taskID},
	})
}

// runAnalysis executes the full pipeline in the background. All errors are
// captured to the similarity_task row so the UI can surface them.
func runAnalysis(taskID int64) {
	runMu.Lock()
	defer runMu.Unlock()

	db := repository.GetDB()
	taskRepo := repository.NewSimilarityTaskRepository(db)
	cfgRepo := repository.NewSystemConfigRepository(db)
	distRepo := repository.NewDataDistributingRepository(db, 100)
	famRepo := repository.NewFamilyRepository(db)

	defer func() {
		if r := recover(); r != nil {
			_ = taskRepo.MarkFailed(taskID, asString(r))
		}
	}()

	if err := taskRepo.MarkRunning(taskID, "loading"); err != nil {
		_ = taskRepo.MarkFailed(taskID, err.Error())
		return
	}

	loader := &similarity.DBLoader{Repo: distRepo}
	persister := &similarity.DBPersister{Repo: famRepo}
	cfg := similarity.LoadConfigFromDB(cfgRepo)

	_ = taskRepo.UpdatePhase(taskID, "analyzing")
	res, err := similarity.AnalyzeFromDB(context.Background(), loader, persister, similarity.AnalyzerOptions{
		AnalyzeTaskID: &taskID,
		Reset:         true,
		Cfg:           cfg,
	})
	if err != nil {
		_ = taskRepo.MarkFailed(taskID, err.Error())
		return
	}

	_ = taskRepo.MarkSucceeded(taskID, res.InputCount, res.FamilyCount, res.MemberCount)
	// 家族表已与当前文件集合一致，清 dirty 让「重建相似关系」按钮回到「无需重建」态
	cfgRepo.SetValue(repository.KeyFamilyDirty, "0")
}

// RunAnalysisAsync is the non-HTTP entry point for triggering similarity analysis.
// Called by the scanner-completion hook. Returns whether a new task was kicked off
// (false means another task was already running and this call was skipped).
func RunAnalysisAsync() bool {
	taskRepo := repository.NewSimilarityTaskRepository(repository.GetDB())

	running, err := taskRepo.HasRunning()
	if err != nil {
		log.Printf("[similarity] auto-trigger: HasRunning check failed: %v", err)
		return false
	}
	if running {
		log.Printf("[similarity] auto-trigger: skipped, another task already running")
		return false
	}

	taskID, err := taskRepo.Create()
	if err != nil {
		log.Printf("[similarity] auto-trigger: create task failed: %v", err)
		return false
	}

	go runAnalysis(taskID)
	log.Printf("[similarity] auto-trigger: kicked task %d", taskID)
	return true
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return "unknown panic"
}

func GetSimilarityTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid task id"})
		return
	}
	repo := repository.NewSimilarityTaskRepository(repository.GetDB())
	row, err := repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

func GetLatestSimilarityTask(c *gin.Context) {
	repo := repository.NewSimilarityTaskRepository(repository.GetDB())
	row, err := repo.Latest()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
