package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 窗口左上角不再显示「Data Asset Scan」：标题留空。
func TestWindowTitleEmpty(t *testing.T) {
	if windowTitle != "" {
		t.Fatalf("窗口标题应留空（不在左上角显示应用名），实得 %q", windowTitle)
	}
}

// 图标悬浮提示/应用身份 = 数据业务治理系统：wails.json info.productName。
func TestWailsProductName(t *testing.T) {
	raw, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatalf("读取 wails.json 失败: %v", err)
	}
	var cfg struct {
		Info struct {
			ProductName string `json:"productName"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("解析 wails.json 失败: %v", err)
	}
	if cfg.Info.ProductName != "数据业务治理系统" {
		t.Fatalf("info.productName 应为「数据业务治理系统」，实得 %q", cfg.Info.ProductName)
	}
}

// 跨平台应用名（图标悬浮/菜单栏）都由 info.productName 驱动：
// macOS plist 的 CFBundleName + CFBundleDisplayName；Windows versioninfo 的 ProductName/FileDescription。
func TestPlatformIdentityUsesProductName(t *testing.T) {
	for _, f := range []string{"build/darwin/Info.dev.plist", "build/darwin/Info.plist"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		s := string(raw)
		for _, key := range []string{"CFBundleName", "CFBundleDisplayName"} {
			if !strings.Contains(s, key) {
				t.Errorf("%s 缺少 %s（菜单栏/程序坞名）", f, key)
			}
		}
		if !strings.Contains(s, "{{.Info.ProductName}}") {
			t.Errorf("%s 应以 ProductName 作为应用名来源", f)
		}
	}
	win, err := os.ReadFile("build/windows/info.json")
	if err != nil {
		t.Fatalf("读取 windows info.json 失败: %v", err)
	}
	ws := string(win)
	for _, field := range []string{"ProductName", "FileDescription"} {
		if !strings.Contains(ws, field) {
			t.Errorf("windows info.json 缺少 %s", field)
		}
	}
	if !strings.Contains(ws, "{{.Info.ProductName}}") {
		t.Errorf("windows info.json 应以 ProductName 作为应用名来源")
	}
}
