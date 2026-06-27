package httpd

import (
	"net/http"

	"data-asset-scan-go/internal/repository"
	"github.com/gin-gonic/gin"
)

// RegisterStatisticsRoutes registers /statistics routes
func RegisterStatisticsRoutes(r *gin.RouterGroup) {
	r.GET("", GetStatistics)
	r.GET("/growth", GetGrowthRate)
}

// StatisticsGrowth represents growth statistics
type StatisticsGrowth struct {
	LastCount    int     `json:"lastCount"`
	CurrentCount int     `json:"currentCount"`
	GrowthCount  int     `json:"growthCount"`
	GrowthRate   float64 `json:"growthRate"`
}

// StatisticsComparison represents comparison between two scans
type StatisticsComparison struct {
	WorkspaceStatistics  StatisticsGrowth `json:"workspaceStatistics"`
	NonHistoryStatistics StatisticsGrowth `json:"nonHistoryStatistics"`
	HistoryStatistics    StatisticsGrowth `json:"historyStatistics"`
	HasComparison        bool             `json:"hasComparison"`
}

// GetStatistics handles GET /statistics
// Returns comparison data between last two scans
func GetStatistics(c *gin.Context) {
	statsRepo := repository.NewFileStatisticsRepository(repository.GetDB())
	taskRepo := repository.NewScanTaskRepository(repository.GetDB())

	// Get latest statistics
	latestStats, err := statsRepo.GetLatest()
	if err != nil || latestStats == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": StatisticsComparison{
				WorkspaceStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: 0,
					GrowthCount:  0,
					GrowthRate:   0,
				},
				NonHistoryStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: 0,
					GrowthCount:  0,
					GrowthRate:   0,
				},
				HistoryStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: 0,
					GrowthCount:  0,
					GrowthRate:   0,
				},
				HasComparison: false,
			},
		})
		return
	}

	// Get previous successful task
	previousTask, err := taskRepo.GetPreviousSuccessfulTask(latestStats.ScanTaskID)
	if err != nil || previousTask == nil {
		// No previous task, just return current stats
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": StatisticsComparison{
				WorkspaceStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.WorkspaceFileTotal,
					GrowthCount:  latestStats.WorkspaceFileTotal,
					GrowthRate:   0,
				},
				NonHistoryStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.NonHistoryFileCount,
					GrowthCount:  latestStats.NonHistoryFileCount,
					GrowthRate:   0,
				},
				HistoryStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.HistoryFileCount,
					GrowthCount:  latestStats.HistoryFileCount,
					GrowthRate:   0,
				},
				HasComparison: false,
			},
		})
		return
	}

	// Get previous statistics
	previousStats, err := statsRepo.GetByTaskId(int(previousTask.ID))
	if err != nil || previousStats == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": StatisticsComparison{
				WorkspaceStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.WorkspaceFileTotal,
					GrowthCount:  latestStats.WorkspaceFileTotal,
					GrowthRate:   0,
				},
				NonHistoryStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.NonHistoryFileCount,
					GrowthCount:  latestStats.NonHistoryFileCount,
					GrowthRate:   0,
				},
				HistoryStatistics: StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.HistoryFileCount,
					GrowthCount:  latestStats.HistoryFileCount,
					GrowthRate:   0,
				},
				HasComparison: false,
			},
		})
		return
	}

	// Calculate growth
	workspaceGrowth := calculateGrowth(previousStats.WorkspaceFileTotal, latestStats.WorkspaceFileTotal)
	nonHistoryGrowth := calculateGrowth(previousStats.NonHistoryFileCount, latestStats.NonHistoryFileCount)
	historyGrowth := calculateGrowth(previousStats.HistoryFileCount, latestStats.HistoryFileCount)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": StatisticsComparison{
			WorkspaceStatistics:  workspaceGrowth,
			NonHistoryStatistics: nonHistoryGrowth,
			HistoryStatistics:    historyGrowth,
			HasComparison:        true,
		},
	})
}

// GetGrowthRate handles GET /statistics/growth
// Returns growth rate for different categories
func GetGrowthRate(c *gin.Context) {
	statsRepo := repository.NewFileStatisticsRepository(repository.GetDB())
	taskRepo := repository.NewScanTaskRepository(repository.GetDB())

	// Get latest statistics
	latestStats, err := statsRepo.GetLatest()
	if err != nil || latestStats == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"workspaceGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: 0,
					GrowthCount:  0,
					GrowthRate:   0,
				},
				"nonHistoryGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: 0,
					GrowthCount:  0,
					GrowthRate:   0,
				},
				"historyGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: 0,
					GrowthCount:  0,
					GrowthRate:   0,
				},
			},
		})
		return
	}

	// Get previous successful task
	previousTask, err := taskRepo.GetPreviousSuccessfulTask(latestStats.ScanTaskID)
	if err != nil || previousTask == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"workspaceGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.WorkspaceFileTotal,
					GrowthCount:  latestStats.WorkspaceFileTotal,
					GrowthRate:   0,
				},
				"nonHistoryGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.NonHistoryFileCount,
					GrowthCount:  latestStats.NonHistoryFileCount,
					GrowthRate:   0,
				},
				"historyGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.HistoryFileCount,
					GrowthCount:  latestStats.HistoryFileCount,
					GrowthRate:   0,
				},
			},
		})
		return
	}

	// Get previous statistics
	previousStats, err := statsRepo.GetByTaskId(int(previousTask.ID))
	if err != nil || previousStats == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"workspaceGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.WorkspaceFileTotal,
					GrowthCount:  latestStats.WorkspaceFileTotal,
					GrowthRate:   0,
				},
				"nonHistoryGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.NonHistoryFileCount,
					GrowthCount:  latestStats.NonHistoryFileCount,
					GrowthRate:   0,
				},
				"historyGrowth": StatisticsGrowth{
					LastCount:    0,
					CurrentCount: latestStats.HistoryFileCount,
					GrowthCount:  latestStats.HistoryFileCount,
					GrowthRate:   0,
				},
			},
		})
		return
	}

	// Calculate growth
	workspaceGrowth := calculateGrowth(previousStats.WorkspaceFileTotal, latestStats.WorkspaceFileTotal)
	nonHistoryGrowth := calculateGrowth(previousStats.NonHistoryFileCount, latestStats.NonHistoryFileCount)
	historyGrowth := calculateGrowth(previousStats.HistoryFileCount, latestStats.HistoryFileCount)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"workspaceGrowth":  workspaceGrowth,
			"nonHistoryGrowth": nonHistoryGrowth,
			"historyGrowth":    historyGrowth,
		},
	})
}

// Helper function to calculate growth statistics
func calculateGrowth(lastCount, currentCount int) StatisticsGrowth {
	growthCount := currentCount - lastCount
	growthRate := 0.0
	if lastCount > 0 {
		growthRate = float64(growthCount) / float64(lastCount) * 100
	}
	return StatisticsGrowth{
		LastCount:    lastCount,
		CurrentCount: currentCount,
		GrowthCount:  growthCount,
		GrowthRate:   growthRate,
	}
}
