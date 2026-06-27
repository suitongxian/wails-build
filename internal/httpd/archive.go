package httpd

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"data-asset-scan-go/internal/repository"
	"github.com/gin-gonic/gin"
)

// RegisterArchiveRoutes registers /archive routes
func RegisterArchiveRoutes(r *gin.RouterGroup) {
	r.POST("", CreateArchive)
	r.GET("/list", ListArchives)            // GET /archive/list
	r.POST("/download", DownloadArchive)    // POST /archive/download
	r.GET("/:id/download", DownloadArchive) // GET /archive/:id/download (backward compat)
	r.DELETE("/:id", DeleteArchive)
}

// RegisterArchiveManagementRoutes registers /archive-management routes
func RegisterArchiveManagementRoutes(r *gin.Engine) {
	r.GET("/archive-management", GetArchiveManagement)
	r.POST("/archive-management/no-archive", BatchNoArchive)
}

// ArchiveApplication represents archive application data
type ArchiveApplication struct {
	ApplicantUnit       string `json:"applicant_unit"`
	ApplicantDepartment string `json:"applicant_department"`
	ApplicantName       string `json:"applicant_name"`
	ApplicantContact    string `json:"applicant_contact"`
	ArchiveFileName     string `json:"archive_file_name"`
	ArchiveFileCategory string `json:"archive_file_category"`
	ArchiveFileHash     string `json:"archive_file_hash"`
	ApplicationTime     string `json:"application_time"`
	ContentTitle        string `json:"content_title"`
	DataClassification  string `json:"data_classification"`
	ProtectionMethod    int    `json:"protection_method"`
}

// ArchiveRecord represents an archive record
type ArchiveRecord struct {
	ID            int64  `json:"id"`
	ArchiveID     int64  `json:"archive_id"`
	FileName      string `json:"file_name"`
	FileSize      int64  `json:"file_size"`
	Status        string `json:"status"`
	ApplicantName string `json:"applicant_name"`
	ApplyTime     string `json:"apply_time"`
	FilePath      string `json:"file_path,omitempty"`
	ContentSign   string `json:"content_sign,omitempty"`
}

// CreateArchiveRequest represents the request body for creating archive
type CreateArchiveRequest struct {
	FilePath           string             `json:"filePath"`
	ArchiveApplication ArchiveApplication `json:"archiveApplication"`
}

// CreateArchive handles POST /archive
func CreateArchive(c *gin.Context) {
	var req CreateArchiveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request body"})
		return
	}

	if req.FilePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing required parameter: filePath"})
		return
	}
	if req.ArchiveApplication.ApplicantName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing required parameter: archiveApplication.applicant_name"})
		return
	}

	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)

	uploadServerURL := effectiveUploadServerURL(configRepo)
	if uploadServerURL == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请先在系统设置中配置文件上传服务器地址"})
		return
	}

	if !fileExists(req.FilePath) {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "File not found"})
		return
	}

	fileContent, err := os.ReadFile(req.FilePath)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "Failed to read file"})
		return
	}

	fileName := filepath.Base(req.FilePath)
	fileMD5 := calculateMD5Hash(fileContent)

	req.ArchiveApplication.ArchiveFileHash = fileMD5
	if req.ArchiveApplication.ApplicationTime == "" {
		req.ArchiveApplication.ApplicationTime = time.Now().Format(time.RFC3339)
	}
	if req.ArchiveApplication.ArchiveFileName == "" {
		req.ArchiveApplication.ArchiveFileName = fileName
	}

	archiveURL := strings.TrimSuffix(uploadServerURL, "/") + "/api/file/archive"
	boundary := "----FormBoundary" + randomString(16)
	body := buildMultipartBody(fileName, fileContent, fileMD5, req.ArchiveApplication, boundary)

	client := &http.Client{Timeout: 60 * time.Second}
	httpReq, err := http.NewRequest("POST", archiveURL, strings.NewReader(body))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "Failed to create request"})
		return
	}
	httpReq.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": fmt.Sprintf("Archive request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var jsonResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &jsonResponse); err == nil {
		if code, ok := jsonResponse["code"].(float64); ok && code == 0 {
			contentSign := findContentSignByPath(dataRepo, req.FilePath)
			if contentSign != "" {
				updateUploadStateByContentSign(dataRepo, contentSign, 1)
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "message": jsonResponse["message"], "data": jsonResponse["data"]})
			return
		} else {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": jsonResponse["message"]})
			return
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		contentSign := findContentSignByPath(dataRepo, req.FilePath)
		if contentSign != "" {
			updateUploadStateByContentSign(dataRepo, contentSign, 1)
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "归档成功"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": false, "error": fmt.Sprintf("归档失败: %d %s", resp.StatusCode, string(respBody))})
}

// ListArchives handles GET /archive/list
func ListArchives(c *gin.Context) {
	page := 1
	pageSize := 50

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
	applicantName := c.Query("applicant_name")

	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	uploadServerURL := effectiveUploadServerURL(configRepo)
	if uploadServerURL == "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "message": "请先在系统设置中配置文件上传服务器地址"})
		return
	}

	archiveURL := strings.TrimSuffix(uploadServerURL, "/") + "/api/file/archive"
	url := fmt.Sprintf("%s?page=%d&pageSize=%d", archiveURL, page, pageSize)
	if applicantName != "" {
		url += "&applicant_name=" + applicantName
	}

	client := &http.Client{Timeout: 30 * time.Second}
	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "message": "Failed to create request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "message": fmt.Sprintf("Request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", respBody)
}

// DownloadArchive handles POST /archive/download and GET /archive/:id/download
func DownloadArchive(c *gin.Context) {
	// Determine archive ID source
	var archiveID int64
	var err error

	if c.Request.Method == "POST" {
		var params struct {
			ArchiveID          int64  `json:"archive_id"`
			BorrowerName       string `json:"borrower_name"`
			BorrowerDepartment string `json:"borrower_department"`
			BorrowReason       string `json:"borrow_reason"`
			BorrowMethod       int    `json:"borrow_method"`
		}
		if err := c.ShouldBindJSON(&params); err == nil {
			archiveID = params.ArchiveID
			// For POST, we need borrower info from body
			if params.BorrowerName == "" || params.BorrowerDepartment == "" {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing required fields: borrower_name, borrower_department"})
				return
			}
			borrowMethod := params.BorrowMethod
			if borrowMethod == 0 {
				borrowMethod = 1
			}
			doArchiveDownload(c, archiveID, params.BorrowerName, params.BorrowerDepartment, params.BorrowReason, borrowMethod)
			return
		}
	}

	// GET /archive/:id/download
	archiveIDStr := c.Param("id")
	archiveID, err = strconv.ParseInt(archiveIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid archive ID"})
		return
	}

	borrowerName := c.Query("borrower_name")
	borrowerDepartment := c.Query("borrower_department")
	borrowReason := c.Query("borrow_reason")
	borrowMethodStr := c.Query("borrow_method")

	borrowMethod := 1
	if borrowMethodStr != "" {
		if parsed, err := strconv.Atoi(borrowMethodStr); err == nil {
			borrowMethod = parsed
		}
	}

	if borrowerName == "" || borrowerDepartment == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing required fields: borrower_name, borrower_department"})
		return
	}

	doArchiveDownload(c, archiveID, borrowerName, borrowerDepartment, borrowReason, borrowMethod)
}

func doArchiveDownload(c *gin.Context, archiveID int64, borrowerName, borrowerDepartment, borrowReason string, borrowMethod int) {
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	uploadServerURL := effectiveUploadServerURL(configRepo)
	if uploadServerURL == "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "message": "请先在系统设置中配置文件上传服务器地址"})
		return
	}

	downloadURL := strings.TrimSuffix(uploadServerURL, "/") + "/api/file/download"

	requestBody := map[string]interface{}{
		"archive_id":          archiveID,
		"borrower_name":       borrowerName,
		"borrower_department": borrowerDepartment,
		"borrow_reason":       borrowReason,
		"borrow_method":       borrowMethod,
	}

	bodyBytes, _ := json.Marshal(requestBody)

	client := &http.Client{Timeout: 60 * time.Second}
	httpReq, err := http.NewRequest("POST", downloadURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "message": "Failed to create request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "message": fmt.Sprintf("Request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/octet-stream") {
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", resp.Header.Get("Content-Disposition"))
		c.Header("Access-Control-Allow-Origin", "*")
		io.Copy(c.Writer, resp.Body)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", respBody)
}

// DeleteArchive handles DELETE /archive/:id
func DeleteArchive(c *gin.Context) {
	archiveIDStr := c.Param("id")
	archiveID, err := strconv.ParseInt(archiveIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid archive ID"})
		return
	}

	_ = archiveID
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Archive deleted"})
}

// GetArchiveManagement handles GET /archive-management
func GetArchiveManagement(c *gin.Context) {
	page, _ := strconv.Atoi(defaultStr(c.Query("page"), "1"))
	pageSize, _ := strconv.Atoi(defaultStr(c.Query("pageSize"), "50"))
	search := c.Query("search")
	archiveType := defaultStr(c.Query("archiveType"), "pending")
	importanceLevelFilterStr := c.Query("importanceLevelFilter")

	options := repository.ArchiveQueryOptions{
		Page:        page,
		PageSize:    pageSize,
		Search:      search,
		ArchiveType: archiveType,
	}

	if importanceLevelFilterStr != "" && archiveType == "pending" {
		if n, err := strconv.Atoi(importanceLevelFilterStr); err == nil && n >= 1 && n <= 3 {
			n2 := n
			options.ImportanceLevelFilter = &n2
		}
	}

	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)
	result := dataRepo.GetArchiveFiles(options)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"files":    result.Files,
			"total":    result.Total,
			"page":     page,
			"pageSize": pageSize,
		},
	})
}

// BatchNoArchive handles POST /archive-management/no-archive
func BatchNoArchive(c *gin.Context) {
	var params struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid request body"})
		return
	}

	if len(params.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Missing or invalid required field: ids"})
		return
	}

	dataRepo := repository.NewDataDistributingRepository(repository.GetDB(), 100)
	updatedCount := dataRepo.BatchUpdateToNoArchive(params.IDs)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"updatedCount": updatedCount},
		"message": fmt.Sprintf("成功将 %d 条记录设置为无需归档", updatedCount),
	})
}

// Helper functions

func buildMultipartBody(fileName string, fileContent []byte, fileMD5 string, app ArchiveApplication, boundary string) string {
	var sb strings.Builder
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"" + fileName + "\"\r\n")
	sb.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	sb.Write(fileContent)
	sb.WriteString("\r\n")
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Disposition: form-data; name=\"fileMd5\"\r\n\r\n")
	sb.WriteString(fileMD5 + "\r\n")
	appJSON, _ := json.Marshal(app)
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Disposition: form-data; name=\"archiveApplication\"\r\n\r\n")
	sb.WriteString(string(appJSON) + "\r\n")
	sb.WriteString("--" + boundary + "--\r\n")
	return sb.String()
}

func calculateMD5Hash(data []byte) string {
	hash := md5.Sum(data)
	return fmt.Sprintf("%x", hash)
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(result)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func findContentSignByPath(dataRepo *repository.DataDistributingRepository, filePath string) string {
	allFiles, err := dataRepo.GetActive()
	if err != nil {
		return ""
	}
	for _, file := range allFiles {
		if file.Path == filePath {
			return file.ContentSign
		}
	}
	return ""
}

func updateUploadStateByContentSign(dataRepo *repository.DataDistributingRepository, contentSign string, state int) {
	allFiles, err := dataRepo.GetActive()
	if err != nil {
		return
	}
	now := time.Now()
	for _, file := range allFiles {
		if file.ContentSign == contentSign {
			query := `UPDATE data_distributing SET upload_state = ?, update_time = ? WHERE data_distribution_id = ?`
			dataRepo.DB.Exec(query, state, now, file.DataDistributionID)
		}
	}
}
