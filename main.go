package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gin-gonic/gin"
	"data-asset-scan-go/internal/httpd"
	"data-asset-scan-go/internal/repository"
	"data-asset-scan-go/internal/similarity"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
)

// 注意必须用 all: 前缀。Go embed 默认会跳过下划线 `_` 或点 `.` 开头的文件，
// 而 vite 生成的共享辅助 chunk（如 _plugin-vue_export-helper-XXX.js）正是
// 下划线开头——不加 all: 会导致这些 chunk 没被 embed 进二进制，运行时
// 404，并把所有引用它们的路由 view 一起拖坏。
//go:embed all:frontend-assets
var assets embed.FS

// windowTitle 桌面窗口标题栏文字。留空：不在窗口左上角显示「Data Asset Scan」字样。
// （应用身份/图标悬浮提示走 wails.json info.productName =「数据业务治理系统」。）
const windowTitle = ""

func main() {
	// Get user data directory for database
	userDataDir := getUserDataDir()
	// 2026-05-22 多加一层 db/ 子目录，把数据库文件与未来其它运行时数据隔离开
	dbDir := filepath.Join(userDataDir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Fatalf("Failed to create user data directory: %v", err)
	}

	// Initialize database
	dbPath := filepath.Join(dbDir, "data.db")
	log.Printf("Database path: %s", dbPath)

	if err := repository.InitDB(dbPath); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	log.Println("Database initialized and schema loaded successfully")
	similarity.SetDB(repository.GetDB())

	// Clean up orphan running tasks left over from a previous crash
	taskRepo := repository.NewScanTaskRepository(repository.GetDB())
	if n, err := taskRepo.MarkOrphanRunsAsFailed(); err != nil {
		log.Printf("Failed to clean up orphan running tasks: %v", err)
	} else if n > 0 {
		log.Printf("Marked %d orphan running task(s) as failed", n)
	}

	// 后台引导：尝试从 manage 拉取 TPL-PERSONAL-FILES 并建好 3 个个人项目。
	// 失败仅 log，不阻塞 scan 启动；manage 不可达时等用户在立项向导主动同步。
	go repository.BootstrapPersonalProjects(repository.GetDB())

	// Create Gin app
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	// Register all HTTP routes
	httpd.RegisterRoutes(engine)

	// Start Gin HTTP server in a goroutine
	go func() {
		log.Println("Starting HTTP server on :3001")
		if err := engine.Run(":3001"); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	log.Println("Starting Wails application...")
	if err := wails.Run(&options.App{
		Title:  windowTitle,
		Width:  1280,
		Height: 800,
		Assets: assets,
	}); err != nil {
		log.Fatalf("Failed to run Wails: %v", err)
	}
}

func getUserDataDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, ".local", "share", "data-asset-scan")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "data-asset-scan")
	default:
		return filepath.Join(home, ".local", "share", "data-asset-scan")
	}
}
