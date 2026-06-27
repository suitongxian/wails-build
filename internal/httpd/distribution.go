package httpd

import (
	"net/http"
	"strings"

	"data-asset-scan-go/internal/repository"
	"github.com/gin-gonic/gin"
)

// RegisterDistributionRoutes registers /distribution routes
func RegisterDistributionRoutes(r *gin.RouterGroup) {
	r.GET("", GetDataDistribution)
	r.GET("/file", GetFileDistribution)
}

// DistributionData represents data distribution statistics
type DistributionData struct {
	TotalFiles      int `json:"totalFiles"`
	WorkspaceFiles  int `json:"workspaceFiles"`
	HistoryFiles    int `json:"historyFiles"`
	NonHistoryFiles int `json:"nonHistoryFiles"`
	UploadedFiles   int `json:"uploadedFiles"`
	PendingFiles    int `json:"pendingFiles"`
}

// FileDistribution represents file distribution by type/suffix
type FileDistribution struct {
	Suffix     string  `json:"suffix"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// GetDataDistribution handles GET /distribution
func GetDataDistribution(c *gin.Context) {
	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())

	// Get all active files
	allFiles, err := dataRepo.GetActive()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to get distribution data",
		})
		return
	}

	workspacePath := configRepo.GetWorkspace()
	fullInventoryTime := configRepo.GetFullInventoryTime()

	totalFiles := len(allFiles)
	workspaceFiles := 0
	uploadedFiles := 0
	pendingFiles := 0
	historyFiles := 0
	nonHistoryFiles := 0

	for _, file := range allFiles {
		// Count workspace files
		if workspacePath != "" && isPathWithin(file.Path, workspacePath) {
			workspaceFiles++
		}

		// Count upload state
		switch file.UploadState {
		case 1: // Uploaded
			uploadedFiles++
		case 0, 2, 3: // Not uploaded, copy, or failed
			pendingFiles++
		}
	}

	// Calculate history vs non-history based on first_create_time
	if fullInventoryTime != "" {
		for _, file := range allFiles {
			if file.FileCreateTime != nil {
				if file.FileCreateTime.Format("2006-01-02T15:04:05Z") < fullInventoryTime {
					historyFiles++
				} else {
					nonHistoryFiles++
				}
			}
		}
	} else {
		// If no full inventory time, all files are non-history
		nonHistoryFiles = totalFiles
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": DistributionData{
			TotalFiles:      totalFiles,
			WorkspaceFiles:  workspaceFiles,
			HistoryFiles:    historyFiles,
			NonHistoryFiles: nonHistoryFiles,
			UploadedFiles:   uploadedFiles,
			PendingFiles:    pendingFiles,
		},
	})
}

// GetFileDistribution handles GET /distribution/file
// Returns file distribution grouped by file suffix/type
func GetFileDistribution(c *gin.Context) {
	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)

	// Get all active files
	allFiles, err := dataRepo.GetActive()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to get file distribution",
		})
		return
	}

	// Group by file suffix
	suffixCounts := make(map[string]int)
	totalCount := 0

	for _, file := range allFiles {
		suffix := ""
		if file.FileSuffix != nil && *file.FileSuffix != "" {
			suffix = strings.ToLower(*file.FileSuffix)
			if strings.HasPrefix(suffix, ".") {
				suffix = suffix[1:]
			}
		}
		if suffix == "" {
			suffix = "(no extension)"
		}
		suffixCounts[suffix]++
		totalCount++
	}

	// Convert to slice and calculate percentages
	distributions := make([]FileDistribution, 0, len(suffixCounts))
	for suffix, count := range suffixCounts {
		percentage := 0.0
		if totalCount > 0 {
			percentage = float64(count) / float64(totalCount) * 100
		}
		distributions = append(distributions, FileDistribution{
			Suffix:     suffix,
			Count:      count,
			Percentage: percentage,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    distributions,
	})
}
