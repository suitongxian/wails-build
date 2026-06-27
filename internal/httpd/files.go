package httpd

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"data-asset-scan-go/internal"
	"data-asset-scan-go/internal/models"
	"data-asset-scan-go/internal/repository"
	"github.com/gin-gonic/gin"
)

// RegisterFilesRoutes registers /files routes
func RegisterFilesRoutes(r *gin.RouterGroup) {
	r.GET("", GetFiles)
	r.POST("/open", OpenFileByContentSign)
	r.GET("/:id", GetFile)
	r.GET("/:id/copies", GetFileCopies)
	r.DELETE("/:id", DeleteFile)
}

// FileRecord represents a file record
type FileRecord struct {
	DataDistributionID int64   `json:"data_distribution_id"`
	ScanTaskID         *int64  `json:"scan_task_id"`
	Path               string  `json:"path"`
	DataType           int     `json:"data_type"`
	ScanFoundCount     int     `json:"scan_found_count"`
	ContentSign        string  `json:"content_sign"`
	FileSuffix         *string `json:"file_suffix"`
	FileMagic          *string `json:"file_magic"`
	FileCreateTime     *string `json:"file_create_time"`
	FileUpdateTime     *string `json:"file_update_time"`
	FileReadTime       *string `json:"file_read_time"`
	FileSize           int64   `json:"file_size"`
	FileHide           int     `json:"file_hide"`
	UploadState        int     `json:"upload_state"`
	IP                 string  `json:"ip"`
	MacAddress         string  `json:"mac_address"`
	ParentID           *int64  `json:"parent_id"`
	ScanTime           string  `json:"scan_time"`
	CreateTime         string  `json:"create_time"`
	UpdateTime         string  `json:"update_time"`
	Disable            int     `json:"disable"`
	CopyCount          int     `json:"copy_count,omitempty"`
}

// FilesQueryParams represents query parameters for GetFiles
type FilesQueryParams struct {
	Search           string `form:"search"`
	WorkspaceFilter  string `form:"workspaceFilter"`
	SurvivalFilter   string `form:"survivalFilter"`
	AccessTimeFilter string `form:"accessTimeFilter"`
	Page             int    `form:"page"`
	PageSize         int    `form:"pageSize"`
	NoPagination     bool   `form:"noPagination"`
}

// GetFiles handles GET /files
// Query params:
//
//	search, workspaceFilter (inside|outside|all),
//	survivalFilter (new|deleted|normal|all),
//	accessTimeFilter (new|history|all) — 以 full_inventory_time 为界，
//	  new=普查后新登记的文件，history=普查前的历史文件，all=不过滤；
//	page, pageSize, noPagination
func GetFiles(c *gin.Context) {
	var params FilesQueryParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid query parameters",
		})
		return
	}

	// Set defaults
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 50
	}
	if params.WorkspaceFilter == "" {
		params.WorkspaceFilter = "all"
	}
	if params.SurvivalFilter == "" {
		params.SurvivalFilter = "all"
	}
	if params.AccessTimeFilter == "" {
		params.AccessTimeFilter = "all"
	}

	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	workspacePath := configRepo.GetWorkspace()
	fullInventoryTime := strings.TrimSpace(configRepo.GetFullInventoryTime())

	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)

	rows, total, err := dataRepo.ListFilesWithFilters(repository.FilesListOptions{
		Search:            params.Search,
		WorkspacePath:     workspacePath,
		WorkspaceFilter:   params.WorkspaceFilter,
		SurvivalFilter:    params.SurvivalFilter,
		AccessTimeFilter:  params.AccessTimeFilter,
		FullInventoryTime: fullInventoryTime,
		Page:              params.Page,
		PageSize:          params.PageSize,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to get files",
		})
		return
	}

	files := make([]FileRecord, 0, len(rows))
	for _, row := range rows {
		rec := convertToFileRecord(row.DataDistribution)
		rec.CopyCount = row.CopyCount
		files = append(files, rec)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"files":    files,
			"total":    total,
			"page":     params.Page,
			"pageSize": params.PageSize,
		},
	})
}

// GetFile handles GET /files/:id
func GetFile(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid file ID",
		})
		return
	}

	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)

	// Get all active records and find by ID
	allFiles, err := dataRepo.GetActive()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to get file",
		})
		return
	}

	for _, file := range allFiles {
		if file.DataDistributionID == id {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data":    convertToFileRecord(file),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"error":   "File not found",
	})
}

// DeleteFile handles DELETE /files/:id
func DeleteFile(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid file ID",
		})
		return
	}

	// Soft delete by setting disable = 1
	now := time.Now()
	query := `UPDATE data_distributing SET disable = 1, update_time = ? WHERE data_distribution_id = ?`
	_, err = repository.GetDB().Exec(query, now, id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to delete file",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "File deleted",
	})
}

// OpenFileByContentSign handles POST /files/open
// Body: { "contentSign": "..." }
func OpenFileByContentSign(c *gin.Context) {
	var req struct {
		ContentSign string `json:"contentSign"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ContentSign) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Invalid request: contentSign required",
		})
		return
	}

	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)
	opener := internal.NewFileOpenerService()

	result := opener.OpenFileByContentSign(req.ContentSign, func(cs string) []string {
		records, err := dataRepo.GetByContentSign(cs)
		if err != nil {
			return nil
		}
		paths := make([]string, 0, len(records))
		for _, r := range records {
			paths = append(paths, r.Path)
		}
		return paths
	})

	c.JSON(http.StatusOK, gin.H{
		"success": result.Success,
		"message": result.Message,
		"data": gin.H{
			"filePath": result.FilePath,
		},
	})
}

// GetFileCopies handles GET /files/:id/copies
// Here :id is interpreted as content_sign
func GetFileCopies(c *gin.Context) {
	contentSign := c.Param("id")
	if strings.TrimSpace(contentSign) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "contentSign required",
		})
		return
	}

	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)
	records, err := dataRepo.GetByContentSign(contentSign)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   "Failed to get copies",
		})
		return
	}

	copies := make([]FileRecord, 0, len(records))
	for _, r := range records {
		copies = append(copies, convertToFileRecord(r))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"copies": copies,
			"count":  len(copies),
		},
	})
}

// Helper functions

func convertToFileRecord(file models.DataDistribution) FileRecord {
	record := FileRecord{
		DataDistributionID: file.DataDistributionID,
		ScanTaskID:         file.ScanTaskID,
		Path:               file.Path,
		DataType:           int(file.DataType),
		ScanFoundCount:     file.ScanFoundCount,
		ContentSign:        file.ContentSign,
		FileSuffix:         file.FileSuffix,
		FileMagic:          file.FileMagic,
		FileSize:           file.FileSize,
		FileHide:           file.FileHide,
		UploadState:        file.UploadState,
		IP:                 file.IP,
		MacAddress:         file.MacAddress,
		ParentID:           file.ParentID,
		CreateTime:         file.CreateTime.Format("2006-01-02 15:04:05"),
		UpdateTime:         file.UpdateTime.Format("2006-01-02 15:04:05"),
		Disable:            file.Disable,
	}

	if file.FileCreateTime != nil {
		createTime := file.FileCreateTime.Format("2006-01-02 15:04:05")
		record.FileCreateTime = &createTime
	}
	if file.FileUpdateTime != nil {
		updateTime := file.FileUpdateTime.Format("2006-01-02 15:04:05")
		record.FileUpdateTime = &updateTime
	}
	if file.FileReadTime != nil {
		readTime := file.FileReadTime.Format("2006-01-02 15:04:05")
		record.FileReadTime = &readTime
	}
	record.ScanTime = file.ScanTime.Format("2006-01-02 15:04:05")

	return record
}

func isPathWithin(path, basePath string) bool {
	// Simple path containment check
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, "\\")
	basePath = strings.TrimSuffix(basePath, "/")
	basePath = strings.TrimSuffix(basePath, "\\")

	return strings.HasPrefix(path, basePath)
}
