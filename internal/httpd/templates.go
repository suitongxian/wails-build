package httpd

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// RegisterTemplatesRoutes 注册 /templates 路由
func RegisterTemplatesRoutes(r *gin.RouterGroup) {
	r.GET("", ListTemplates)
	r.GET("/remote-list", ListRemoteTemplates) // 直接 list manage 端 active 模板，不写入本地缓存
	r.GET("/authoring", ListLocalTemplates)    // 本地创作模版列表（origin=local），可按行业/归类过滤
	r.GET("/:id", GetTemplateFull)
	r.GET("/:id/tree", GetLocalTemplateTree) // 本地模版完整五层树（树编辑器渲染用）
	r.POST("/sync", SyncTemplate)
	r.POST("/sync-all", SyncAllTemplates) // 同步 manage 端所有 active 模版
	// 2026-05-31 本地模版创作 CRUD（阶段一·片3）
	r.POST("", CreateLocalTemplate)
	r.PUT("/:id", UpdateLocalTemplate)
	r.DELETE("/:id", DeleteLocalTemplate)
	// 阶段二·片10 反向同步：推送到 manage
	r.POST("/:id/push", PushTemplateToManage)
	// 2026-06-01 立项：从 manage 已有模版克隆为本地可编辑模版；把本地模版立项为项目
	r.POST("/clone-from-manage", CloneFromManage)
	// 2026-06-15 导入：上传 / 粘贴五层模版树 JSON，重建为本地可编辑模版
	r.POST("/import", ImportLocalTemplate)
	r.POST("/:id/initiate", InitiateTemplate)
	// 2026-06-02 「是否发布」：只有已发布的本地模版才能立项
	r.POST("/:id/publish", PublishLocalTemplate)
}

// CloneFromManage POST /templates/clone-from-manage  body: { template_code }
// 把 manage 端模版完整五层克隆到本地（origin=local，可编辑），返回新本地模版 id。
func CloneFromManage(c *gin.Context) {
	var body struct {
		TemplateCode string `json:"template_code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.TemplateCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code"})
		return
	}
	// 模版克隆走「模版管理平台」(template-manage，:19092)，与上报数据/文件的 manage 地址分离
	endpoint := repository.NewSystemConfigRepository(repository.GetDB()).GetEffectiveTemplateServerEndpoint()
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	id, err := repo.CloneTemplateServerToLocal(nil, endpoint, body.TemplateCode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": id}})
}

// ImportLocalTemplate POST /templates/import
// body: 五层模版树 JSON —— { "template": {...}, "stages": [ { ..., "tasks": [ { ..., "file_rules": [...] } ] } ] }
// 把整棵树在本地重建为可编辑模版（origin=local，新 template_code），返回新本地模版 id。
func ImportLocalTemplate(c *gin.Context) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "导入内容为空或读取失败"})
		return
	}
	repo := repository.NewTemplateAuthoringRepository(repository.GetDB())
	id, err := repo.ImportLocalTemplateFromJSON(raw)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": id}})
}

// ListTemplates GET /templates?status=
func ListTemplates(c *gin.Context) {
	status := c.Query("status")
	repo := repository.NewTemplateCacheRepository(repository.GetDB())
	list, err := repo.ListTemplates(status)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}

// GetTemplateFull GET /templates/:id 返回完整结构（环节+文件规则）
func GetTemplateFull(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	repo := repository.NewTemplateCacheRepository(repository.GetDB())
	full, err := repo.GetFullTemplate(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": full})
}

// SyncTemplateInput 拉取入参
//
// 请求体：
//
//	{ "code": "TPL-PRINT-BOOK", "version": "V2.1", "source": "template-server" }  // 从模版管理平台(:19092)按 code 拉
//	{ "code": "TPL-PRINT-BOOK", "version": "V2.1" }                              // 从 manage(:19091) /api/templates/full 拉
//	{ "remote_id": 1 }                                                          // 从 manage 按 id 拉
//
// source="template-server" 时走 FetchFromTemplateServer：在线列表(remote-list)来自
// 模版管理平台，其 id 不能拿去 manage 查，必须按 code 回到同一台服务器取详情。
type SyncTemplateInput struct {
	Code     string `json:"code"`
	Version  string `json:"version"`
	RemoteID int64  `json:"remote_id"`
	Source   string `json:"source"`
}

// SyncAllTemplates POST /templates/sync-all
//
// 一次性同步 manage 端所有 status=active 模版到本地缓存。
// 用于"重新同步"按钮：发现 manage 上新发布的模版能立刻在 scan 立项向导里出现。
//
// 响应：
//
//	{ success: true, data: { total_remote: N, synced: M, errors: [...] } }
func SyncAllTemplates(c *gin.Context) {
	cacheRepo := repository.NewTemplateCacheRepository(repository.GetDB())
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	fetcher := repository.NewTemplateFetcher(cacheRepo, configRepo)

	result, err := fetcher.FetchAllActive()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// 拉完模板后立即尝试建好 3 个个人项目（含 TPL-PERSONAL-FILES 时生效），无需重启。
	_ = repository.EnsurePersonalContext(repository.GetDB())

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total_remote": result.TotalRemote,
			"synced":       result.Synced,
			"errors":       result.Errors,
			"local_ids":    result.LocalIDs,
		},
	})
}

// SyncTemplate POST /templates/sync 从 manage 端拉取并写入本地缓存
func SyncTemplate(c *gin.Context) {
	var in SyncTemplateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if in.Source != "template-server" && in.RemoteID == 0 && (in.Code == "" || in.Version == "") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "需要提供 remote_id 或 code+version"})
		return
	}
	if in.Source == "template-server" && in.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "source=template-server 时需要提供 code"})
		return
	}

	cacheRepo := repository.NewTemplateCacheRepository(repository.GetDB())
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	fetcher := repository.NewTemplateFetcher(cacheRepo, configRepo)

	var localID int64
	var err error
	switch {
	case in.Source == "template-server":
		// 在线列表来自模版管理平台(:19092)，按 code 回到同一台服务器取详情
		localID, err = fetcher.FetchFromTemplateServer(in.Code, in.Version)
	case in.RemoteID > 0:
		localID, err = fetcher.FetchByID(in.RemoteID)
	default:
		localID, err = fetcher.FetchByCode(in.Code, in.Version)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	// 拉取后返回完整结构
	full, _ := cacheRepo.GetFullTemplate(localID)

	// 若同步的是 TPL-PERSONAL-FILES，立即尝试建/补 3 个个人项目，无需重启。
	if full != nil && full.Template.TemplateCode == "TPL-PERSONAL-FILES" {
		_ = repository.EnsurePersonalContext(repository.GetDB())
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": full})
}

// ListRemoteTemplates GET /templates/remote-list
//
// 直接 list manage 端 status=active 的模板，不写入本地缓存。
// 用于"数据业务模版总览"页面展示——让用户看到 manage 上有哪些可用模板
// 即可，是否要拉到本地由用户后续决定。
func ListRemoteTemplates(c *gin.Context) {
	cacheRepo := repository.NewTemplateCacheRepository(repository.GetDB())
	configRepo := repository.NewSystemConfigRepository(repository.GetDB())
	fetcher := repository.NewTemplateFetcher(cacheRepo, configRepo)
	// 云端模版列表走「模版管理平台」(template-manage，:19092)
	list, err := fetcher.ListTemplateServerActive()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": list})
}
