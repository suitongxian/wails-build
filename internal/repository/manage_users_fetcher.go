package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ManageUser manage 端已注册用户（auth_users）的精简画像
type ManageUser struct {
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	UserUnit       string `json:"user_unit"`
	UserDepartment string `json:"user_department"`
	Status         string `json:"status"`
	Role           string `json:"role"` // system_admin / unit_admin / user —— 用于过滤管理员，不作为业务负责人候选
}

type manageUserListResp struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    []ManageUser `json:"data"`
}

// ManageBusinessClass 复用 template_fetcher.go 中已定义的同名类型（manage 端行业/业务分类）。

type manageClassListResp struct {
	Code    int                   `json:"code"`
	Message string                `json:"message"`
	Data    []ManageBusinessClass `json:"data"`
}

// ManageWorkItem manage 端"我的工作事项"（与 manage WorkItem JSON 对齐）
type ManageWorkItem struct {
	TemplateCode string `json:"template_code"`
	TemplateName string `json:"template_name"`
	StageID      int64  `json:"stage_id"`
	StageCode    string `json:"stage_code"`
	StageName    string `json:"stage_name"`
	SortOrder    int    `json:"sort_order"`
	Manager      string `json:"manager"`
	Members      string `json:"members"`
	Delivered    int    `json:"delivered"`
	Status       string `json:"status"` // pending / ready / delivered
}

type manageWorkItemResp struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    []ManageWorkItem `json:"data"`
}

// FetchManageWorkItems 按 username 从 manage 拉取"我的工作事项"。
func FetchManageWorkItems(client *http.Client, endpoint, username string) ([]ManageWorkItem, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Get(endpoint + "/api/work-items/list?username=" + url.QueryEscape(username))
	if err != nil {
		return nil, fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manage 返回非 200: %d", resp.StatusCode)
	}
	var raw manageWorkItemResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("manage 返回错误 code=%d msg=%s", raw.Code, raw.Message)
	}
	if raw.Data == nil {
		return []ManageWorkItem{}, nil
	}
	return raw.Data, nil
}

// DeliverStageToManage 通知 manage 某环节定稿已交付（→下游就绪）。
func DeliverStageToManage(client *http.Client, endpoint, templateCode, stageCode string) error {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	buf, _ := json.Marshal(map[string]string{"template_code": templateCode, "stage_code": stageCode})
	resp, err := client.Post(endpoint+"/api/work-items/deliver", "application/json", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("manage 返回非 200: %d", resp.StatusCode)
	}
	var raw struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if raw.Code != 0 {
		return fmt.Errorf("manage 交付失败 code=%d msg=%s", raw.Code, raw.Message)
	}
	return nil
}

// FetchManageBusinessClasses 从 manage 拉取行业/业务分类列表。
// 2026-05-31 数据业务分类归口 manage；scan 创作模版时下拉拉取选择。
func FetchManageBusinessClasses(client *http.Client, endpoint string) ([]ManageBusinessClass, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Get(endpoint + "/api/business-classes/list")
	if err != nil {
		return nil, fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manage 返回非 200: %d", resp.StatusCode)
	}
	var raw manageClassListResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("manage 返回错误 code=%d msg=%s", raw.Code, raw.Message)
	}
	if raw.Data == nil {
		return []ManageBusinessClass{}, nil
	}
	return raw.Data, nil
}

// FetchManageUsers 从 manage 拉取已注册用户列表（默认仅 active）。
// 供 scan 端「项目负责人」等下拉选择使用。endpoint 为 manage 基址；client 可注入便于测试。
func FetchManageUsers(client *http.Client, endpoint string) ([]ManageUser, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Get(endpoint + "/api/auth-users/list?status=active")
	if err != nil {
		return nil, fmt.Errorf("调用 manage 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manage 返回非 200: %d", resp.StatusCode)
	}
	var raw manageUserListResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("manage 返回错误 code=%d msg=%s", raw.Code, raw.Message)
	}
	if raw.Data == nil {
		return []ManageUser{}, nil
	}
	return raw.Data, nil
}
