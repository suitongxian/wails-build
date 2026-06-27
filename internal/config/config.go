package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// AppConfig 应用配置结构体，与 ConfigService.ts 中的 AppConfig 接口对应
type AppConfig struct {
	ControlType       string `yaml:"control_type"`        // 管控文件类型
	SaveCode          string `yaml:"save_code"`            // 安全操作码
	DailyScanInterval int    `yaml:"daily_scan_interval"`  // 日常盘点扫描间隔时间（分钟）
	ScanAreaPath      string `yaml:"scan_area_path"`       // 扫描区域路径
	Workspace         string `yaml:"workspace"`            // 工作空间目录
	ScanExcludeDir    string `yaml:"scan_exclude_dir"`     // 扫描排除目录
	UploadServerURL   string `yaml:"upload_server_url"`    // 上传服务器地址
}

var (
	cfg  *AppConfig
	once sync.Once
)

// GetConfig 获取全局配置实例（单例模式）
func GetConfig() *AppConfig {
	once.Do(func() {
		cfg = &AppConfig{}
	})
	return cfg
}

// LoadFromFile 从指定路径加载 YAML 配置文件
func LoadFromFile(configPath string) (*AppConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("配置文件不存在: %s", configPath)
	}

	var config AppConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, nil
}

// Load 从用户数据目录加载 config.yaml
func Load(userDataDir string) (*AppConfig, error) {
	configPath := filepath.Join(userDataDir, "config.yaml")
	return LoadFromFile(configPath)
}

// GetConfigPath 获取默认配置文件路径
func GetConfigPath() string {
	return filepath.Join(GetDefaultUserDataDir(), "config.yaml")
}

// GetDefaultUserDataDir 获取默认用户数据目录
func GetDefaultUserDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}

	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "data-asset-scan")
	case "darwin":
		return filepath.Join(home, ".local", "share", "data-asset-scan")
	default:
		return filepath.Join(home, ".local", "share", "data-asset-scan")
	}
}

// LoadWithDefaults 加载配置，如果文件不存在则返回默认配置
func LoadWithDefaults(userDataDir string) (*AppConfig, error) {
	configPath := filepath.Join(userDataDir, "config.yaml")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 返回默认配置
		return &AppConfig{
			ControlType:       ".doc,.ppt,.docx,.pdf",
			SaveCode:          "",
			DailyScanInterval: 60,
			ScanAreaPath:      "",
			Workspace:         "",
			ScanExcludeDir:    "",
			UploadServerURL:   "",
		}, nil
	}

	return Load(userDataDir)
}

// GetControlTypes 获取管控类型列表
func (c *AppConfig) GetControlTypes() []string {
	if c.ControlType == "" {
		return []string{}
	}
	var types []string
	for _, t := range strings.Split(c.ControlType, ",") {
		trimmed := strings.TrimSpace(t)
		if trimmed != "" {
			types = append(types, trimmed)
		}
	}
	return types
}

// GetSaveCode 获取安全操作码
func (c *AppConfig) GetSaveCode() string {
	return c.SaveCode
}

// GetDailyScanInterval 获取日常盘点扫描间隔时间（分钟）
func (c *AppConfig) GetDailyScanInterval() int {
	return c.DailyScanInterval
}

// GetScanAreaPath 获取扫描区域路径
func (c *AppConfig) GetScanAreaPath() string {
	return c.ScanAreaPath
}

// GetWorkspace 获取工作空间目录
func (c *AppConfig) GetWorkspace() string {
	return c.Workspace
}

// GetScanExcludeDir 获取扫描排除目录
func (c *AppConfig) GetScanExcludeDir() string {
	return c.ScanExcludeDir
}

// GetUploadServerURL 获取上传服务器地址
func (c *AppConfig) GetUploadServerURL() string {
	return c.UploadServerURL
}