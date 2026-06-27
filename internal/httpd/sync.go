package httpd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"data-asset-scan-go/internal/repository"
	"github.com/gin-gonic/gin"
)

// RegisterSyncRoutes registers /sync routes
func RegisterSyncRoutes(r *gin.Engine) {
	r.POST("/sync/source", SyncSource)
}

// syncOutgoingRecord is the JSON payload one record contributes to the
// outgoing /api/sync/source request.
type syncOutgoingRecord struct {
	ContentMD5      string `json:"content_md5"`
	SourceIP        string `json:"source_ip"`
	SourceMac       string `json:"source_mac"`
	Quantity        int    `json:"quantity"`
	FirstCreateTime string `json:"first_create_time,omitempty"`
	ContentSubject  string `json:"content_subject,omitempty"`
	ContentType     string `json:"content_type,omitempty"`
	FileMagic       string `json:"file_magic,omitempty"`
	ClaimStatus     *int   `json:"claim_status,omitempty"`
	ClaimTime       string `json:"claim_time,omitempty"`
	ImportanceLevel *int   `json:"importance_level,omitempty"`
	DataShare       string `json:"data_share,omitempty"`
}

// SyncSource handles POST /sync/source
//
// Walks data_resources for rows newer than system_config.last_sync_time,
// uploads them in batches of 100 to {upload_server_url}/api/sync/source,
// then uploads aggregate statistics to {upload_server_url}/api/sync/file-statistics
// regardless of whether there were rows to send.
//
// Response:
// { success, message, data: { syncedCount, failedCount, totalCount, lastSyncTime, errors[] } }
func SyncSource(c *gin.Context) {
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	resourcesRepo := repository.NewDataResourcesRepository(repository.GetDB(), 100)

	uploadServerURL := effectiveUploadServerURL(configRepo)
	if uploadServerURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请先在系统设置中配置上传服务器地址",
		})
		return
	}

	lastSyncTime := configRepo.GetLastSyncTime()
	totalCount, err := resourcesRepo.CountPendingSyncRecords(lastSyncTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询待同步记录失败: " + err.Error(),
		})
		return
	}

	errs := []string{}
	syncedCount := 0
	failedCount := 0
	var maxUpdateTime time.Time
	if lastSyncTime != "" {
		if t, err := time.Parse(time.RFC3339, lastSyncTime); err == nil {
			maxUpdateTime = t
		}
	}

	if totalCount > 0 {
		const batchSize = 100
		for offset := 0; offset < totalCount; offset += batchSize {
			records, err := resourcesRepo.GetPendingSyncRecords(lastSyncTime, batchSize, offset)
			if err != nil {
				errs = append(errs, fmt.Sprintf("读取批次 offset=%d 失败: %v", offset, err))
				failedCount += batchSize
				continue
			}
			if len(records) == 0 {
				break
			}

			localIP := repository.GetLocalIP()
			localMAC := repository.GetLocalMAC()

			outgoing := make([]syncOutgoingRecord, 0, len(records))
			for _, r := range records {
				if r.UpdateTime.After(maxUpdateTime) {
					maxUpdateTime = r.UpdateTime
				}

				out := syncOutgoingRecord{
					ContentMD5: r.ContentSign,
					SourceIP:   localIP,
					SourceMac:  localMAC,
					Quantity:   r.SourceCount,
				}
				if !r.FirstCreateTime.IsZero() {
					out.FirstCreateTime = r.FirstCreateTime.Format(time.RFC3339)
				}
				if r.ContentSubject != nil {
					out.ContentSubject = *r.ContentSubject
				}
				if r.ContentType != nil {
					out.ContentType = *r.ContentType
				}
				if r.FileMagic != nil {
					out.FileMagic = *r.FileMagic
				}
				cs := r.ClaimStatus
				out.ClaimStatus = &cs
				if r.ClaimTime != nil {
					out.ClaimTime = r.ClaimTime.Format(time.RFC3339)
				}
				il := r.ImportanceLevel
				out.ImportanceLevel = &il
				if r.DataShare != nil {
					out.DataShare = *r.DataShare
				}

				// 老 TS 行为：source_ip / source_mac 缺失的记录跳过
				if out.SourceIP == "" || out.SourceMac == "" {
					failedCount++
					continue
				}
				outgoing = append(outgoing, out)
			}

			if len(outgoing) == 0 {
				continue
			}

			if err := postJSON(joinURL(uploadServerURL, "/api/sync/source"), outgoing); err != nil {
				failedCount += len(outgoing)
				errs = append(errs, fmt.Sprintf("批次 offset=%d 上传失败: %v", offset, err))
			} else {
				syncedCount += len(outgoing)
			}
		}
	}

	// Always upload statistics, regardless of whether any records moved.
	if err := uploadStatistics(uploadServerURL, configRepo, resourcesRepo); err != nil {
		errs = append(errs, "统计数据上传失败: "+err.Error())
	}

	if syncedCount > 0 && !maxUpdateTime.IsZero() {
		configRepo.SetLastSyncTime(maxUpdateTime.Format(time.RFC3339))
	}

	message := fmt.Sprintf("成功同步 %d 条记录", syncedCount)
	if failedCount > 0 {
		message = fmt.Sprintf("同步完成: 成功 %d 条, 失败 %d 条", syncedCount, failedCount)
	}
	if totalCount == 0 {
		message = "没有需要同步的记录"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": failedCount == 0,
		"message": message,
		"data": gin.H{
			"syncedCount":  syncedCount,
			"failedCount":  failedCount,
			"totalCount":   totalCount,
			"lastSyncTime": configRepo.GetLastSyncTime(),
			"errors":       errs,
		},
	})
}

// uploadStatistics posts aggregate stats to /api/sync/file-statistics.
func uploadStatistics(serverURL string, configRepo *repository.SystemConfigRepository, resourcesRepo *repository.DataResourcesRepository) error {
	fullInventoryTime := configRepo.GetFullInventoryTime()
	var fitPtr *string
	if fullInventoryTime != "" {
		fitPtr = &fullInventoryTime
	}
	stats := resourcesRepo.GetResourcesStatistics(fitPtr)

	historyCount := stats.HistoryFileCount
	if historyCount < 0 {
		historyCount = 0
	}
	nonHistoryCount := stats.NonHistoryFileCount
	if nonHistoryCount < 0 {
		nonHistoryCount = 0
	}
	historyClaimed := stats.HistoryClaimedCount
	if historyClaimed < 0 {
		historyClaimed = 0
	}
	nonHistoryClaimed := stats.NonHistoryClaimedCount
	if nonHistoryClaimed < 0 {
		nonHistoryClaimed = 0
	}

	body := map[string]interface{}{
		"computer_ip":                    repository.GetLocalIP(),
		"computer_mac":                   repository.GetLocalMAC(),
		"file_total":                     stats.TotalFileCount,
		"workspace_file_total":           stats.WorkspaceTotalCount,
		"history_file_count":             historyCount,
		"non_history_file_count":         nonHistoryCount,
		"workspace_file_claimed_count":   stats.WorkspaceClaimedCount,
		"history_file_claimed_count":     historyClaimed,
		"non_history_file_claimed_count": nonHistoryClaimed,
		"unclassified_file_count":        stats.UnclassifiedCount,
		"core_file_count":                stats.CoreCount,
		"important_file_count":           stats.ImportantCount,
		"open_file_count":                stats.OpenCount,
		"private_file_count":             stats.PrivacyCount,
	}

	return postJSON(joinURL(serverURL, "/api/sync/file-statistics"), body)
}

// joinURL safely joins a base URL with a path.
func joinURL(base, p string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base + p
	}
	rel, err := url.Parse(p)
	if err != nil {
		return base + p
	}
	return u.ResolveReference(rel).String()
}

// postJSON sends a JSON-encoded POST request and returns an error if the
// server responds with non-2xx or the request itself fails.
func postJSON(targetURL string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
