package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// 上游定稿文件中转（2026-06-01）：
//   - 上游环节「提交定稿」→ UploadStageFinals 把本环节 output/ 文件上传到 manage。
//   - 下游环节「开始工作」→ PullUpstreamFinals 从 manage 取紧邻上游环节定稿，写入本环节 input/（工作依据）。
// 走 manage /api/projects/stage-finals(POST 上传) / upstream-finals(GET 取上游清单) / stage-final(GET 下载)。

func defaultHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return client
}

// UploadStageFinals 把某环节 output/ 下的非空文件逐个上传到 manage（重复覆盖）。返回成功数与错误。
func UploadStageFinals(db *sqlx.DB, client *http.Client, endpoint, templateCode, stageCode, uploadedBy string) (int, []string) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return 0, []string{"未配置 manage_endpoint"}
	}
	client = defaultHTTPClient(client)
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	dir := NewProjectWorkspace(root).StageStateDir(templateCode, stageCode, "output")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil // 无 output 目录/为空 → 无可上传
	}
	uploaded := 0
	var errs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if fi, err := e.Info(); err == nil && isPlaceholderFile(path, fi.Size()) {
			continue // 空/占位不传
		}
		if err := uploadOneFinal(client, endpoint, templateCode, stageCode, e.Name(), uploadedBy, path); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		uploaded++
	}
	return uploaded, errs
}

func uploadOneFinal(client *http.Client, endpoint, templateCode, stageCode, fileName, uploadedBy, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("template_code", templateCode)
	_ = w.WriteField("stage_code", stageCode)
	_ = w.WriteField("file_name", fileName)
	_ = w.WriteField("uploaded_by", uploadedBy)
	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	_ = w.Close()

	resp, err := client.Post(endpoint+"/api/projects/stage-finals", w.FormDataContentType(), &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var raw struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("解析响应失败")
	}
	if raw.Code != 0 {
		return fmt.Errorf("%s", raw.Message)
	}
	return nil
}

// PullUpstreamFinals 把紧邻上游环节的定稿拉到本环节 input/（覆盖同名）。返回拉取的文件名。
func PullUpstreamFinals(db *sqlx.DB, client *http.Client, endpoint, templateCode, stageCode string) ([]string, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置 manage_endpoint")
	}
	client = defaultHTTPClient(client)
	resp, err := client.Get(fmt.Sprintf("%s/api/projects/upstream-finals?template_code=%s&stage_code=%s",
		endpoint, url.QueryEscape(templateCode), url.QueryEscape(stageCode)))
	if err != nil {
		return nil, fmt.Errorf("取上游定稿清单失败: %w", err)
	}
	var listResp struct {
		Code int `json:"code"`
		Data *struct {
			UpstreamStageCode *string `json:"upstream_stage_code"`
			Files             []struct {
				ID       int64  `json:"id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("解析上游清单失败: %w", err)
	}
	resp.Body.Close()
	if listResp.Code != 0 || listResp.Data == nil || len(listResp.Data.Files) == 0 {
		return nil, nil // 无上游或上游未交付 → 无工作依据可拉
	}

	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	inputDir := NewProjectWorkspace(root).StageStateDir(templateCode, stageCode, "input")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return nil, fmt.Errorf("建 input 目录失败: %w", err)
	}
	var pulled []string
	for _, fr := range listResp.Data.Files {
		if err := downloadOneFinal(client, endpoint, fr.ID, filepath.Join(inputDir, sanitizeFileName(fr.FileName))); err != nil {
			continue // 单个失败不阻断其余
		}
		pulled = append(pulled, fr.FileName)
	}
	return pulled, nil
}

func downloadOneFinal(client *http.Client, endpoint string, id int64, dst string) error {
	resp, err := client.Get(fmt.Sprintf("%s/api/projects/stage-final?id=%d", endpoint, id))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败 status=%d", resp.StatusCode)
	}
	out, err := os.Create(dst) // 覆盖：上游定稿是权威工作依据，取最新
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}
