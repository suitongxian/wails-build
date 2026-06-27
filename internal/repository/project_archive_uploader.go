package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// ArchiveUploader 把已结项的归档清单单向上报到 data-asset-manage
//
// 端点（manage 侧）：POST /api/sync/project-archive
//
//	Header: X-Sync-Token (可选，与模版同步同源)
//	Body:   ArchiveManifest 完整 JSON
//
// V1 不要求 manage 侧实体存储，只需接收 manifest 并落表 project_archive_uploads。
// 上报失败时项目仍处 archived，sync_status = failed，可通过 /projects/:id/sync 重试。
type ArchiveUploader struct {
	DB         *sqlx.DB
	projRepo   *DataProjectRepository
	configRepo *SystemConfigRepository
}

func NewArchiveUploader(db *sqlx.DB) *ArchiveUploader {
	return &ArchiveUploader{
		DB:         db,
		projRepo:   NewDataProjectRepository(db),
		configRepo: NewSystemConfigRepository(db),
	}
}

// UploadResult 上报结果
type ArchiveUploadResult struct {
	Status   string `json:"status"`          // success / failed
	Endpoint string `json:"endpoint"`        // 实际请求的 URL
	Reply    string `json:"reply,omitempty"` // manage 端响应摘要
	Error    string `json:"error,omitempty"`
}

// Upload 把项目的 manifest 上报到 manage
//
// manifestPath 是 Close() 返回的 manifest.json 绝对路径。
// 函数会读取文件并 POST 到 manage `/api/sync/project-archive`。
// 任意失败会写 sync_status=failed + sync_message=错误描述。
func (u *ArchiveUploader) Upload(projectID int64, manifestPath string) (*ArchiveUploadResult, error) {
	project, err := u.projRepo.FindByID(projectID)
	if err != nil {
		return nil, err
	}
	if project.Status != "archived" {
		return nil, fmt.Errorf("项目状态非 archived（当前 %s），无法上报", project.Status)
	}

	// V1：成功移交过的项目不允许再次移交（防止重复入库 + 与 UI 防呆呼应）
	if project.SyncStatus != nil && *project.SyncStatus == "success" {
		return &ArchiveUploadResult{
			Status:   "success",
			Endpoint: "",
			Reply:    "本项目已成功移交，无需再次操作",
		}, fmt.Errorf("本项目已成功移交至档案库，不可重复移交")
	}

	// 优先用单独配置的归档端点；否则复用模版同步用的 manage_endpoint
	endpointBase := strings.TrimSpace(u.configRepo.GetValue(KeyArchiveEndpoint))
	if endpointBase == "" {
		endpointBase = strings.TrimSpace(u.configRepo.GetValue(KeyManageEndpoint))
	}
	if endpointBase == "" {
		return u.markFailed(projectID, "未配置 manage 上报端点（system_configs key=manage_endpoint 或 archive_endpoint）")
	}

	endpoint := strings.TrimRight(endpointBase, "/") + "/api/sync/project-archive"
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" {
		return u.markFailed(projectID, fmt.Sprintf("非法的上报端点：%s", endpoint))
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return u.markFailed(projectID, fmt.Sprintf("读取 manifest 失败：%v", err))
	}

	// 仅用单独配置的同步 token；manage_token 已废弃，不再 fallback
	token := strings.TrimSpace(u.configRepo.GetValue(KeySyncToken))

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(manifestBytes))
	if err != nil {
		return u.markFailed(projectID, err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Sync-Token", token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return u.markFailed(projectID, fmt.Sprintf("请求失败：%v", err))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return u.markFailed(projectID, fmt.Sprintf("manage 返回 %d: %s", resp.StatusCode, string(respBody)))
	}

	// 兼容两种 manage 响应风格：
	//   - Nuxt 风格：{code: 0, message, data}
	//   - 通用风格：{success: true, message, data}
	var rsp struct {
		Code    *int        `json:"code"`
		Success bool        `json:"success"`
		Message string      `json:"message"`
		Error   string      `json:"error"`
		Data    interface{} `json:"data"`
	}
	_ = json.Unmarshal(respBody, &rsp)
	ok := rsp.Success || (rsp.Code != nil && *rsp.Code == 0)
	if !ok {
		msg := rsp.Error
		if msg == "" {
			msg = rsp.Message
		}
		if msg == "" {
			msg = string(respBody)
		}
		return u.markFailed(projectID, fmt.Sprintf("manage 拒绝：%s", msg))
	}

	now := time.Now()
	if _, err := u.DB.Exec(`UPDATE data_projects SET sync_status = 'success', sync_message = ?, synced_at = ?, update_time = ? WHERE id = ?`,
		"manage 已接收", now, now, projectID); err != nil {
		return nil, err
	}

	return &ArchiveUploadResult{
		Status:   "success",
		Endpoint: endpoint,
		Reply:    rsp.Message,
	}, nil
}

func (u *ArchiveUploader) markFailed(projectID int64, msg string) (*ArchiveUploadResult, error) {
	now := time.Now()
	_, _ = u.DB.Exec(`UPDATE data_projects SET sync_status = 'failed', sync_message = ?, update_time = ? WHERE id = ?`,
		msg, now, projectID)
	return &ArchiveUploadResult{Status: "failed", Error: msg}, fmt.Errorf("%s", msg)
}
