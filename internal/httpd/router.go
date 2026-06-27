package httpd

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers all HTTP routes with Gin engine
func RegisterRoutes(r *gin.Engine) {
	// Apply CORS middleware globally
	r.Use(corsMiddleware())

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// User info routes (/user-info/*)
	userGroup := r.Group("/user-info")
	RegisterUserRoutes(userGroup)

	// Auth routes (/auth/*) - scan 代理 manage 账号登录/注册并维护本地会话
	authGroup := r.Group("/auth")
	RegisterAuthRoutes(authGroup)

	// Users routes (/users/*) - 项目成员与权限使用的真实用户列表
	usersGroup := r.Group("/users")
	RegisterUsersRoutes(usersGroup)

	// Tasks routes (/scan-tasks/*)
	tasksGroup := r.Group("/scan-tasks")
	RegisterTasksRoutes(tasksGroup)

	// Scan routes (/scan/*)
	scanGroup := r.Group("/scan")
	RegisterScanRoutes(scanGroup)

	// Files routes (/files/*)
	filesGroup := r.Group("/files")
	RegisterFilesRoutes(filesGroup)

	// Distribution routes (/distribution/*)
	distGroup := r.Group("/distribution")
	RegisterDistributionRoutes(distGroup)

	// Archive routes (/archive/*)
	archiveGroup := r.Group("/archive")
	RegisterArchiveRoutes(archiveGroup)

	// Archive-management routes (/archive-management)
	RegisterArchiveManagementRoutes(r)

	// Resources routes (/resources/*)
	resourcesGroup := r.Group("/resources")
	RegisterResourcesRoutes(resourcesGroup)

	// Statistics routes (/statistics/*)
	statsGroup := r.Group("/statistics")
	RegisterStatisticsRoutes(statsGroup)

	// Config routes (/config)
	r.GET("/config", GetConfig)
	r.POST("/config", SaveConfig)

	// Sync routes (/sync/*)
	RegisterSyncRoutes(r)

	// Similarity analysis routes (/similarity/*)
	similarityGroup := r.Group("/similarity")
	RegisterSimilarityRoutes(similarityGroup)

	// Family routes (/family/*)
	familyGroup := r.Group("/family")
	RegisterFamilyRoutes(familyGroup)

	// Subjects routes (/subjects/*) - scan 只读拉取 manage 三主体主数据
	subjectsGroup := r.Group("/subjects")
	RegisterSubjectsRoutes(subjectsGroup)

	// Business classes routes (/business-classes/*) - 行业分类本地创作 CRUD
	businessClassesGroup := r.Group("/business-classes")
	RegisterBusinessClassesRoutes(businessClassesGroup)

	// 模版三层本地创作 CRUD：事项 / 任务 / 标识
	RegisterTemplateStagesRoutes(r.Group("/template-stages"))
	RegisterTemplateTasksRoutes(r.Group("/template-tasks"))
	RegisterTemplateFileRulesRoutes(r.Group("/template-file-rules"))

	// 从 manage 拉取已注册用户（供模版「项目负责人」等下拉）
	r.GET("/manage-users", ListManageUsers)
	// 从 manage 拉取行业/业务分类（供模版「数据业务分类」下拉）
	r.GET("/manage-business-classes", ListManageBusinessClasses)

	// 多人协同「我的工作事项」（P3/P4/P5）
	RegisterWorkItemsRoutes(r)

	// Templates routes (/templates/*) - 数据业务模版（本地缓存 + 从 manage 拉取）
	templatesGroup := r.Group("/templates")
	RegisterTemplatesRoutes(templatesGroup)

	// Projects routes (/projects/*) - 数据业务项目（立项卷宗）
	projectsGroup := r.Group("/projects")
	RegisterProjectsRoutes(projectsGroup)

	// File versions routes (/file-versions/*) - 项目文件版本操作
	fileVersionsGroup := r.Group("/file-versions")
	RegisterFileVersionsRoutes(fileVersionsGroup)

	// Ledgers routes (/ledgers/*) - 数据资产标识底账（"账"）查询与状态切换
	ledgersGroup := r.Group("/ledgers")
	RegisterLedgersRoutes(ledgersGroup)

	// V3-5 Audit logs routes (/audit-logs/*) - §11 模块级审计日志查询
	auditGroup := r.Group("/audit-logs")
	RegisterAuditLogsRoutes(auditGroup)

	// V4-Q1-b AI 归目工具 (/ai/classify/*) - §4.3 项目版本文件 AI 归目
	aiGroup := r.Group("/ai/classify")
	RegisterAIClassifyRoutes(aiGroup)

	// 2026-05-21 三级分流：核心登记 (/memorandum/*) - 核心级正式登记通道
	memoGroup := r.Group("/memorandum")
	RegisterMemorandumRoutes(memoGroup)

	// 2026-05-21 数据业务集中立项（与 /projects 解耦）
	cpaGroup := r.Group("/centralized-projects")
	RegisterCentralizedProjectsRoutes(cpaGroup)
	// 2026-05-22 集中立项工作台 — 基于 stage_dir 的轻量文件操作
	RegisterCentralizedWorkbenchRoutes(cpaGroup)
}

// corsMiddleware returns a Gin middleware handler for CORS
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
