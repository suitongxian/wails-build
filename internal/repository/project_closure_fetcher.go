package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 立项人结项相关：scan 代理 manage 的「我立项的项目」与「结项」。
// 项目实体在 manage（is_project=1），scan 只按当前登录用户代理查询/结项。

// FetchMyProjects 拉取当前用户立项的项目（含进度与可否结项），原样返回 data 供前端渲染。
func FetchMyProjects(client *http.Client, endpoint, username string) (json.RawMessage, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	if username == "" {
		return json.RawMessage("[]"), nil
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Get(endpoint + "/api/projects/mine?username=" + url.QueryEscape(username))
	if err != nil {
		return nil, fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	var raw struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("manage 返回异常: %s", raw.Message)
	}
	if len(raw.Data) == 0 {
		return json.RawMessage("[]"), nil
	}
	return raw.Data, nil
}

// CloseProjectOnManage 通知 manage 对某项目结项（manage 侧校验仅立项人本人 + 全量交付）。
func CloseProjectOnManage(client *http.Client, endpoint, templateCode, username, reason string) error {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	buf, _ := json.Marshal(map[string]string{"template_code": templateCode, "username": username, "reason": reason})
	resp, err := client.Post(endpoint+"/api/projects/close", "application/json", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	var raw struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if raw.Code != 0 {
		return fmt.Errorf("%s", raw.Message) // 把 manage 的不可结项原因透传给用户
	}
	return nil
}
