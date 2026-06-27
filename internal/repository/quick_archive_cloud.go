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

// 部门/单位项目一键归档：不在本地建夹，而是把归档清单【上报到云端 data-asset-manage】，
// 由云端按九宫格落入「部门档案柜 / 单位资料室」等容器。复用模版同步同源的端点与 token 配置。
//
// 端点（manage 侧待实现）：POST {manage}/api/sync/quick-archive
//   Header: X-Sync-Token（可选）
//   Body:   QuickArchiveCloudPayload JSON
//
// 本机不删除/修改任何原文件，只读取内容计算校验值随清单上报。

const quickArchiveCloudPath = "/api/sync/quick-archive"

// QuickArchiveCloudFile 上报的单个文件项。
type QuickArchiveCloudFile struct {
	Name             string `json:"name"`
	Bucket           string `json:"bucket"`
	SensitivityLevel string `json:"sensitivity_level"` // core/important/general
	TargetFolder     string `json:"target_folder"`     // 九宫格目标：部门档案柜 / 单位资料室 …
	Checksum         string `json:"checksum"`          // MD5
	Size             int64  `json:"size"`
}

// QuickArchiveCloudPayload 上报给 manage 的归档清单。
type QuickArchiveCloudPayload struct {
	Schema           string                  `json:"schema"`
	SourceTerminal   string                  `json:"source_terminal"`
	ProjectCode      string                  `json:"project_code"`
	ProjectName      string                  `json:"project_name"`
	Scope            string                  `json:"scope"`     // department/unit
	Container        string                  `json:"container"` // 部门柜 / 单位室
	SensitivityLevel string                  `json:"project_sensitivity_level"`
	GeneratedBy      string                  `json:"generated_by"`
	CustodyNote      string                  `json:"custody_note"` // 归档归属说明（选填）
	Files            []QuickArchiveCloudFile `json:"files"`
}

// buildCloudPayloadFromItems 用给定文件项（仅定稿）构造上报清单。
// 目标柜室按【项目层级 scope × 文件级别】算（如 部门档案柜 / 单位保密室）。
func buildCloudPayloadFromItems(projectCode, projectName, scope, projectSensitivity, operator string, items []archiveItem) QuickArchiveCloudPayload {
	_, prefix, suffix := ScopeRoute(scope)
	payload := QuickArchiveCloudPayload{
		Schema:           "data-asset-scan/quick-archive-v1",
		SourceTerminal:   "data-asset-scan",
		ProjectCode:      projectCode,
		ProjectName:      projectName,
		Scope:            scope,
		Container:        prefix + suffix, // 部门柜 / 单位室
		SensitivityLevel: NormalizeSensitivity(projectSensitivity),
		GeneratedBy:      operator,
	}
	for _, it := range items {
		sum, _ := fileMD5(it.Path)
		var size int64
		if fi, err := os.Stat(it.Path); err == nil {
			size = fi.Size()
		}
		payload.Files = append(payload.Files, QuickArchiveCloudFile{
			Name:             it.Name,
			Bucket:           it.Bucket,
			SensitivityLevel: it.Level,
			TargetFolder:     NineGridFolder(scope, it.Level),
			Checksum:         sum,
			Size:             size,
		})
	}
	return payload
}

// uploadQuickArchiveCloud 把清单 POST 到 manage，复用 manage_endpoint/archive_endpoint + sync_token 配置。
func uploadQuickArchiveCloud(db *sqlx.DB, payload QuickArchiveCloudPayload) (string, error) {
	cfg := NewSystemConfigRepository(db)
	endpointBase := strings.TrimSpace(cfg.GetValue(KeyArchiveEndpoint))
	if endpointBase == "" {
		endpointBase = strings.TrimSpace(cfg.GetValue(KeyManageEndpoint))
	}
	if endpointBase == "" {
		return "", fmt.Errorf("未配置 manage 上报端点（manage_endpoint 或 archive_endpoint）")
	}
	endpoint := strings.TrimRight(endpointBase, "/") + quickArchiveCloudPath
	if parsed, err := url.Parse(endpoint); err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("非法的上报端点：%s", endpoint)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(cfg.GetValue(KeySyncToken)); token != "" {
		req.Header.Set("X-Sync-Token", token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败：%v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("manage 返回 %d: %s", resp.StatusCode, string(respBody))
	}
	var rsp struct {
		Code    *int   `json:"code"`
		Success bool   `json:"success"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(respBody, &rsp)
	if !rsp.Success && !(rsp.Code != nil && *rsp.Code == 0) {
		msg := rsp.Error
		if msg == "" {
			msg = rsp.Message
		}
		if msg == "" {
			msg = string(respBody)
		}
		return "", fmt.Errorf("manage 拒绝：%s", msg)
	}
	return rsp.Message, nil
}
