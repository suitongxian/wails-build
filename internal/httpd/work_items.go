package httpd

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal"
	"data-asset-scan-go/internal/repository"
)

// 2026-05-31 多人协同「我的工作事项」（P3/P4/P5）：
//   - GET  /my-work-items      代理 manage，按当前登录用户拉取我的工作事项（含就绪状态）
//   - POST /work-items/start   开始工作：只在本机工作空间建该环节目录（不建全树）
//   - POST /work-items/deliver 提交定稿：通知 manage 该环节已交付（→ 下游就绪）

func RegisterWorkItemsRoutes(r gin.IRouter) {
	r.GET("/my-work-items", ListMyWorkItems)
	r.POST("/work-items/start", StartWorkItem)
	r.GET("/work-items/output-rules", GetStageOutputRules)   // 待交付定稿清单
	r.GET("/work-items/process-files", GetStageProcessFiles) // 可挑选的过程文件（非空）
	r.GET("/work-items/input-docs", GetStageInputDocs)       // 工作依据：input/ 下上游交付文件（只读）
	r.GET("/work-items/input-doc", GetStageInputDoc)         // 工作依据：读取某文件内容（只读查看）
	r.GET("/work-items/process-docs", GetStageProcessDocs)   // 在线编辑：过程文档清单（含空占位）
	r.GET("/work-items/doc", GetStageDoc)                    // 在线编辑：读取某过程文档内容
	r.POST("/work-items/doc", SaveStageDoc)                  // 在线编辑：保存某过程文档（自动按模版目录落地）
	r.POST("/work-items/deliver", DeliverWorkItem)
	r.GET("/my-projects", ListMyProjects)     // 我立项的项目（进度+可否结项）
	r.POST("/projects/close", CloseMyProject) // 结项（仅立项人本人）
}

func manageEndpoint() string {
	return repository.NewSystemConfigRepository(repository.GetDB()).GetValue(repository.KeyManageEndpoint)
}

// ensureProjectSyncedLocal 确保某项目（manage 立项生成、编码如 TPL-x-P<nano>）已同步到本地 data_templates。
// worker 的"开始工作/提交定稿/自动归档"都按项目编码查本地五层结构；项目只在 manage，故首次接触时从 manage 拉到本地。
// 幂等：本地已有则跳过；best-effort（失败不抛，调用方各自处理"模版不存在"）。
func ensureProjectSyncedLocal(projectCode string) error {
	if projectCode == "" {
		return nil
	}
	// 集中立项虚拟项目（CPA-{application_id}）：目录树由承接后「开始工作」在本机建出，
	// 没有对应的 manage 模版可同步，直接放行按路径读写过程文档。
	if strings.HasPrefix(projectCode, "CPA-") {
		return nil
	}
	db := repository.GetDB()
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM data_templates WHERE template_code = ? AND disable = 0`, projectCode); err == nil && n > 0 {
		return nil // 本地已有
	}
	cacheRepo := repository.NewTemplateCacheRepository(db)
	configRepo := repository.NewSystemConfigRepository(db)
	fetcher := repository.NewTemplateFetcher(cacheRepo, configRepo)
	if _, err := fetcher.FetchByCode(projectCode, ""); err != nil { // 空版本 → manage 按编码取最新（项目编码唯一）
		return fmt.Errorf("同步项目到本地失败（请确认 manage 已更新并可达）: %w", err)
	}
	return nil
}

// ListMyWorkItems GET /my-work-items
func ListMyWorkItems(c *gin.Context) {
	username := currentOperator(c)
	items, err := repository.FetchManageWorkItems(nil, manageEndpoint(), username)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items, "username": username})
}

// ListMyProjects GET /my-projects —— 当前用户立项的项目（含进度、可否结项）
func ListMyProjects(c *gin.Context) {
	username := currentOperator(c)
	data, err := repository.FetchMyProjects(nil, manageEndpoint(), username)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data, "username": username})
}

type closeProjectBody struct {
	TemplateCode string `json:"template_code"`
	Reason       string `json:"reason"`
}

// CloseMyProject POST /projects/close —— 结项（manage 侧校验仅立项人本人 + 全量交付）
func CloseMyProject(c *gin.Context) {
	var body closeProjectBody
	if err := c.ShouldBindJSON(&body); err != nil || body.TemplateCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code"})
		return
	}
	if err := repository.CloseProjectOnManage(nil, manageEndpoint(), body.TemplateCode, currentOperator(c), body.Reason); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type startWorkBody struct {
	ProjectCode string `json:"project_code"` // 用模版编码作为本机项目目录名
	StageCode   string `json:"stage_code"`
}

// StartWorkItem POST /work-items/start —— 只建该环节目录
func StartWorkItem(c *gin.Context) {
	var body startWorkBody
	if err := c.ShouldBindJSON(&body); err != nil || body.ProjectCode == "" || body.StageCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 project_code / stage_code"})
		return
	}
	if err := ensureProjectSyncedLocal(body.ProjectCode); err != nil { // 先把项目五层结构同步到本地，否则后续 scaffold/归档找不到模版
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	root := repository.NewSystemConfigRepository(repository.GetDB()).GetEffectiveProjectRoot()
	ws := repository.NewProjectWorkspace(root)
	dir, err := ws.CreateStageDir(body.ProjectCode, body.StageCode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	// 按模版「文档标识」规则预建 process 空占位文件（output/input 不建；空占位不归档）。
	// ProjectCode 即模版编码 template_code，与目录名一致。失败不阻断（目录已建好），但回报。
	created, scErr := repository.ScaffoldStageFiles(repository.GetDB(), body.ProjectCode, body.StageCode)

	// 从 manage 拉取紧邻上游环节的定稿到本环节 input/（工作依据）。失败不阻断，回报。
	pulled, pullErr := repository.PullUpstreamFinals(repository.GetDB(), nil, manageEndpoint(), body.ProjectCode, body.StageCode)

	// 把 process/ 目录怼到用户面前——让"在对的地方干活"成为最省事的路（best-effort，不阻断）。
	procDir := ws.StageStateDir(body.ProjectCode, body.StageCode, "process")
	go func() { _ = internal.NewFileOpenerService().OpenFolder(procDir) }()

	resp := gin.H{"success": true, "data": gin.H{"stage_dir": dir, "process_dir": procDir, "scaffolded": created, "pulled_inputs": pulled}}
	if scErr != nil {
		resp["scaffold_error"] = scErr.Error()
	}
	if pullErr != nil {
		resp["pull_error"] = pullErr.Error()
	}
	c.JSON(http.StatusOK, resp)
}

// GetStageOutputRules GET /work-items/output-rules?template_code=&stage_code=
func GetStageOutputRules(c *gin.Context) {
	tc, sc := c.Query("template_code"), c.Query("stage_code")
	if tc == "" || sc == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code / stage_code"})
		return
	}
	if err := ensureProjectSyncedLocal(tc); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	rules, err := repository.ListStageOutputRules(repository.GetDB(), tc, sc)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rules})
}

// GetStageProcessFiles GET /work-items/process-files?template_code=&stage_code=
func GetStageProcessFiles(c *gin.Context) {
	tc, sc := c.Query("template_code"), c.Query("stage_code")
	if tc == "" || sc == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code / stage_code"})
		return
	}
	if err := ensureProjectSyncedLocal(tc); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	files, err := repository.ListStageProcessFiles(repository.GetDB(), tc, sc)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": files})
}

// GetStageProcessDocs GET /work-items/process-docs?template_code=&stage_code=
// 在线编辑用：列出本环节 process/ 下全部文档（含空占位，供选择编辑）。
func GetStageProcessDocs(c *gin.Context) {
	tc, sc := c.Query("template_code"), c.Query("stage_code")
	if tc == "" || sc == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code / stage_code"})
		return
	}
	if err := ensureProjectSyncedLocal(tc); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	docs, err := repository.ListStageProcessDocs(repository.GetDB(), tc, sc)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": docs})
}

// GetStageInputDocs GET /work-items/input-docs?template_code=&stage_code=
// 「工作依据」：列出本环节 input/ 下上游交付来的文件（只读展示）。
func GetStageInputDocs(c *gin.Context) {
	tc, sc := c.Query("template_code"), c.Query("stage_code")
	if tc == "" || sc == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code / stage_code"})
		return
	}
	if err := ensureProjectSyncedLocal(tc); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	docs, err := repository.ListStageInputDocs(repository.GetDB(), tc, sc)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": docs})
}

// GetStageInputDoc GET /work-items/input-doc?template_code=&stage_code=&name=
// 只读查看「工作依据」(input) 某文件内容。
func GetStageInputDoc(c *gin.Context) {
	tc, sc, name := c.Query("template_code"), c.Query("stage_code"), c.Query("name")
	if tc == "" || sc == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code / stage_code / name"})
		return
	}
	if err := ensureProjectSyncedLocal(tc); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	content, err := repository.ReadStageInputDoc(repository.GetDB(), tc, sc, name)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"content": content}})
}

// GetStageDoc GET /work-items/doc?template_code=&stage_code=&name=
func GetStageDoc(c *gin.Context) {
	tc, sc, name := c.Query("template_code"), c.Query("stage_code"), c.Query("name")
	if tc == "" || sc == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code / stage_code / name"})
		return
	}
	content, err := repository.ReadStageProcessDoc(repository.GetDB(), tc, sc, name)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"content": content}})
}

type saveDocBody struct {
	TemplateCode string `json:"template_code"`
	StageCode    string `json:"stage_code"`
	Name         string `json:"name"`
	Content      string `json:"content"`
}

// SaveStageDoc POST /work-items/doc —— 保存在线编辑内容，路径由 模版+文档标识 自动决定。
func SaveStageDoc(c *gin.Context) {
	var body saveDocBody
	if err := c.ShouldBindJSON(&body); err != nil || body.TemplateCode == "" || body.StageCode == "" || body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code / stage_code / name"})
		return
	}
	if err := ensureProjectSyncedLocal(body.TemplateCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	path, err := repository.WriteStageProcessDoc(repository.GetDB(), body.TemplateCode, body.StageCode, body.Name, body.Content)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"path": path}})
}

type deliverBody struct {
	TemplateCode string                      `json:"template_code"`
	StageCode    string                      `json:"stage_code"`
	Selections   []repository.FinalSelection `json:"selections"` // 每个 output 标识 ← 一个过程文件
}

// DeliverWorkItem POST /work-items/deliver —— 点「提交定稿」：
// ① 把选中的过程文件拷贝到 output/ 并按 output 标识规范名改名；
// ② 主路径自动归档本环节产出（环节级就高不就低、挂账，幂等）；
// ③ 通知 manage 该环节已交付 → 下游就绪。
func DeliverWorkItem(c *gin.Context) {
	var body deliverBody
	if err := c.ShouldBindJSON(&body); err != nil || body.TemplateCode == "" || body.StageCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 template_code / stage_code"})
		return
	}
	if err := ensureProjectSyncedLocal(body.TemplateCode); err != nil { // 确保本地有项目五层结构
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	// ① 挑选→拷贝→改名生成定稿（有选择才做；无选择则仅归档已在 output 的文件）
	var finals []string
	if len(body.Selections) > 0 {
		created, err := repository.SubmitStageFinals(repository.GetDB(), body.TemplateCode, body.StageCode, body.Selections)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
			return
		}
		finals = created
	}
	// ② 主路径自动归档（本地，幂等）。失败不阻断交付，但回报。
	archive, archErr := repository.AutoArchiveStage(repository.GetDB(), body.TemplateCode, body.StageCode)
	// ③ 上传本环节定稿到 manage（供下游拉取作为工作依据）。失败不阻断，回报。
	uploaded, upErrs := repository.UploadStageFinals(repository.GetDB(), nil, manageEndpoint(), body.TemplateCode, body.StageCode, currentOperator(c))
	// ④ 通知 manage 交付（驱动下游就绪）
	if err := repository.DeliverStageToManage(nil, manageEndpoint(), body.TemplateCode, body.StageCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error(), "archive": archive, "finals": finals})
		return
	}
	resp := gin.H{"success": true, "archive": archive, "finals": finals, "uploaded": uploaded}
	if archErr != nil {
		resp["archive_error"] = archErr.Error()
	}
	if len(upErrs) > 0 {
		resp["upload_errors"] = upErrs
	}
	c.JSON(http.StatusOK, resp)
}
