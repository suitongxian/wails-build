package httpd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal"
	"data-asset-scan-go/internal/repository"
)

// RegisterCentralizedWorkbenchRoutes 注册 /centralized-projects/workbench/*
// 提供环节负责人在 stage_dir 下面工作的最小工作台能力：
//   - GET  /files?app_id&stage_code      列出 input/process/output 三桶
//   - POST /upload?app_id&stage_code&bucket  上传文件到对应桶
//   - GET  /open?app_id&stage_code&bucket&name 在本机打开文件
//
// 数据源是磁盘目录（由 StartStageTask 创建），不依赖 data_projects / file_versions
// 等正式立项表，与现有立项链路完全隔离。
// 旧的「文件桶工作台」(上传/本机打开) 已废弃删除；这里只保留「文件任务受理」内联在线
// 编辑所需的能力：列文档(files) + 读写文本(doc)。
func RegisterCentralizedWorkbenchRoutes(r *gin.RouterGroup) {
	r.GET("/workbench/open", OpenWorkbenchFile)                     // 本机打开：用默认程序（Office/WPS 等）打开文件，跨平台
	r.GET("/workbench/files", ListWorkbenchFiles)                   // 在线编辑：列出本环节 input/process/output 文档
	r.GET("/workbench/doc", ReadWorkbenchDoc)                       // 在线编辑（兜底）：读取文本内容（任意桶，input/output 只读查看）
	r.POST("/workbench/doc", SaveWorkbenchDoc)                      // 在线编辑（兜底）：保存文本内容（仅 process 桶可编辑）
	r.POST("/workbench/import-reference", ImportWorkbenchReference) // 参考文件：把本地所选文件拷贝进 reference 桶
}

// openLocalFileFn 用本机默认程序打开文件；包级变量，便于测试注入替换。
// 跨平台（windows/darwin/linux）实现见 internal.FileOpenerService。
var openLocalFileFn = func(path string) error {
	return internal.NewFileOpenerService().OpenFile(path)
}

// OpenWorkbenchFile GET /centralized-projects/workbench/open?app_id&stage_code&bucket&name[&project_code]
// 用本机默认程序（Office/WPS 等）打开文件——「本机打开」优先，在线编辑兜底。
func OpenWorkbenchFile(c *gin.Context) {
	stageDir, _, ok := resolveStageDir(c)
	if !ok {
		return
	}
	bucket := strings.TrimSpace(c.Query("bucket"))
	if !validBucket(bucket) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "bucket 仅支持 input / process / output"})
		return
	}
	name := filepath.Base(strings.TrimSpace(c.Query("name")))
	if name == "" || name == "." {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 name"})
		return
	}
	full := filepath.Join(stageDir, bucket, name)
	if _, err := os.Stat(full); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "文件不存在"})
		return
	}
	if err := openLocalFileFn(full); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "本机打开失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"path": full}})
}

// workbenchDocMaxBytes 在线编辑可加载的单文件上限（防止把超大/二进制文件灌进文本框）。
const workbenchDocMaxBytes = 2 * 1024 * 1024

func validBucket(b string) bool {
	return b == "input" || b == "reference" || b == "process" || b == "output"
}

// resolveStageDir 解析关键参数，返回 (workDir, projectCode, ok)。
// 五层落盘：带 task_code 时定位到文件任务目录 stages/{stage}/{task}（三态桶在其下）；
// 不带 task_code 时退回环节目录 stages/{stage}（向后兼容/环节级查看）。
// 任何参数缺失或工作空间目录（项目根）未配置都返回 4xx。
func resolveStageDir(c *gin.Context) (workDir, projectCode string, ok bool) {
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	root := strings.TrimSpace(cfg.GetEffectiveProjectRoot())
	if root == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置工作空间目录"})
		return "", "", false
	}
	appID := strings.TrimSpace(c.Query("app_id"))
	stageCode := strings.TrimSpace(c.Query("stage_code"))
	taskCode := strings.TrimSpace(c.Query("task_code"))     // 文件任务编码（FileTaskReceiveView 从 my-tasks 带来）
	codeParam := strings.TrimSpace(c.Query("project_code")) // manage 生成的立项编号（前端从 my-tasks 带来）
	if appID == "" || stageCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 app_id 或 stage_code"})
		return "", "", false
	}
	// 防止路径穿越
	if strings.ContainsAny(appID, "/\\.") || strings.ContainsAny(stageCode, "/\\.") ||
		strings.ContainsAny(taskCode, "/\\.") ||
		strings.ContainsAny(codeParam, "/\\") || strings.Contains(codeParam, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "非法字符"})
		return "", "", false
	}
	appIDInt, _ := strconv.ParseInt(appID, 10, 64)
	projectCode = centralizedDirCode(repository.GetDB(), appIDInt, codeParam)
	ws := repository.NewProjectWorkspace(root)
	if taskCode != "" {
		workDir = ws.TaskDir(projectCode, stageCode, taskCode)
	} else {
		workDir = ws.StageDir(projectCode, stageCode)
	}
	if _, err := os.Stat(workDir); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": fmt.Sprintf("工作目录不存在，请先点「开始工作」：%v", err)})
		return "", "", false
	}
	return workDir, projectCode, true
}

// listBucket 单桶的文件清单（仅当前目录第一层，不递归）
type workbenchFileItem struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	IsDir   bool   `json:"is_dir"`
	Empty   bool   `json:"empty"` // 未填写的占位文件（0 字节，或 .pdf 最小占位）
}

// ListWorkbenchFiles GET /centralized-projects/workbench/files?app_id&stage_code
func ListWorkbenchFiles(c *gin.Context) {
	stageDir, projectCode, ok := resolveStageDir(c)
	if !ok {
		return
	}
	buckets := map[string][]workbenchFileItem{
		"input":     {},
		"reference": {},
		"process":   {},
		"output":    {},
	}
	for b := range buckets {
		dir := filepath.Join(stageDir, b)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			// 跳过隐藏的元数据文件（如参考定级清单 .archive-grade.json），不作为用户文件展示。
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info, ierr := e.Info()
			if ierr != nil {
				continue
			}
			buckets[b] = append(buckets[b], workbenchFileItem{
				Name:    e.Name(),
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
				IsDir:   e.IsDir(),
				Empty:   repository.IsPlaceholderFile(filepath.Join(dir, e.Name()), info.Size()),
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"project_code": projectCode,
			"stage_dir":    stageDir,
			"buckets":      buckets,
		},
	})
}

// ReadWorkbenchDoc GET /centralized-projects/workbench/doc?app_id&stage_code&bucket&name
// 在线编辑：读取某文档的文本内容。input/output 也可读（只读查看），process 可编辑。
func ReadWorkbenchDoc(c *gin.Context) {
	stageDir, _, ok := resolveStageDir(c)
	if !ok {
		return
	}
	bucket := strings.TrimSpace(c.Query("bucket"))
	if !validBucket(bucket) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "bucket 仅支持 input / process / output"})
		return
	}
	name := filepath.Base(strings.TrimSpace(c.Query("name")))
	if name == "" || name == "." {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 name"})
		return
	}
	full := filepath.Join(stageDir, bucket, name)
	info, err := os.Stat(full)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "文件不存在"})
		return
	}
	if info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "不是文件"})
		return
	}
	if info.Size() > workbenchDocMaxBytes {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "文件过大，暂不支持在线编辑（>2MB），请用「本机打开」"})
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "读取失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"name": name, "content": string(data), "editable": bucket == "process"}})
}

// SaveWorkbenchDoc POST /centralized-projects/workbench/doc
// body: { app_id, stage_code, bucket, name, content }
// 仅允许编辑 process 桶里的过程文档（input 是上游来料、output 是定稿，均不在线改）。
// 注意：这里写的是项目工作空间内的【过程工作产物】文件，不是被管控的扫描文件。
func SaveWorkbenchDoc(c *gin.Context) {
	// 用 query 复用 resolveStageDir：把 body 的 app_id/stage_code 也放进 query 上下文不便，
	// 这里直接读 body，再手动按 resolveStageDir 同样的方式定位目录。
	var in struct {
		AppID       string `json:"app_id"`
		StageCode   string `json:"stage_code"`
		TaskCode    string `json:"task_code"` // 文件任务编码（五层落盘定位到任务目录）
		Bucket      string `json:"bucket"`
		Name        string `json:"name"`
		Content     string `json:"content"`
		ProjectCode string `json:"project_code"` // manage 生成的立项编号（前端从 my-tasks 带来）
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	in.AppID = strings.TrimSpace(in.AppID)
	in.StageCode = strings.TrimSpace(in.StageCode)
	in.TaskCode = strings.TrimSpace(in.TaskCode)
	in.ProjectCode = strings.TrimSpace(in.ProjectCode)
	if in.AppID == "" || in.StageCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 app_id 或 stage_code"})
		return
	}
	if strings.ContainsAny(in.AppID, "/\\.") || strings.ContainsAny(in.StageCode, "/\\.") ||
		strings.ContainsAny(in.TaskCode, "/\\.") ||
		strings.ContainsAny(in.ProjectCode, "/\\") || strings.Contains(in.ProjectCode, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "非法字符"})
		return
	}
	if in.Bucket != "process" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "仅「过程(process)」文档可在线编辑；来料(input)与定稿(output)不在线修改"})
		return
	}
	name := filepath.Base(strings.TrimSpace(in.Name))
	if name == "" || name == "." {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 name"})
		return
	}
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	root := strings.TrimSpace(cfg.GetEffectiveProjectRoot())
	if root == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置工作空间目录"})
		return
	}
	ws := repository.NewProjectWorkspace(root)
	appIDInt, _ := strconv.ParseInt(in.AppID, 10, 64)
	dirProject := centralizedDirCode(repository.GetDB(), appIDInt, in.ProjectCode)
	// 五层落盘：带 task_code 落到任务目录，否则退回环节目录（兼容）。
	var procDir string
	if in.TaskCode != "" {
		procDir = ws.TaskStateDir(dirProject, in.StageCode, in.TaskCode, "process")
	} else {
		procDir = filepath.Join(ws.StageDir(dirProject, in.StageCode), "process")
	}
	if _, err := os.Stat(procDir); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "过程目录不存在，请先点「开始工作」"})
		return
	}
	full := filepath.Join(procDir, name)
	if err := os.WriteFile(full, []byte(in.Content), 0o644); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "保存失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"path": full}})
}

// uniqueDest 在 dir 下为 name 找一个不冲突的目标路径：已存在则追加 (1)/(2)… 序号。
func uniqueDest(dir, name string) string {
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return dst
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
	return dst
}

// ImportWorkbenchReference POST /centralized-projects/workbench/import-reference (multipart/form-data)
// 把用户从本地选择的文件【拷贝】到当前文件任务的「参考文件(reference)」桶里。
// 参考文件通常来自外部导入而非自行编辑；这里只读取所选文件字节、在工作空间内新建副本，
// 不修改/删除用户原文件，也不触碰任何被管控的扫描文件。跨平台（win/mac/linux）。
func ImportWorkbenchReference(c *gin.Context) {
	appID := strings.TrimSpace(c.PostForm("app_id"))
	stageCode := strings.TrimSpace(c.PostForm("stage_code"))
	taskCode := strings.TrimSpace(c.PostForm("task_code"))
	codeParam := strings.TrimSpace(c.PostForm("project_code"))
	if appID == "" || stageCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 app_id 或 stage_code"})
		return
	}
	if strings.ContainsAny(appID, "/\\.") || strings.ContainsAny(stageCode, "/\\.") ||
		strings.ContainsAny(taskCode, "/\\.") ||
		strings.ContainsAny(codeParam, "/\\") || strings.Contains(codeParam, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "非法字符"})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少上传文件"})
		return
	}
	name := filepath.Base(fh.Filename)
	if name == "" || name == "." || strings.ContainsAny(name, "/\\") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "非法文件名"})
		return
	}
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	root := strings.TrimSpace(cfg.GetEffectiveProjectRoot())
	if root == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "未配置工作空间目录"})
		return
	}
	ws := repository.NewProjectWorkspace(root)
	appIDInt, _ := strconv.ParseInt(appID, 10, 64)
	dirProject := centralizedDirCode(repository.GetDB(), appIDInt, codeParam)
	var refDir string
	if taskCode != "" {
		refDir = ws.TaskStateDir(dirProject, stageCode, taskCode, "reference")
	} else {
		refDir = filepath.Join(ws.StageDir(dirProject, stageCode), "reference")
	}
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "创建参考文件目录失败：" + err.Error()})
		return
	}
	dst := uniqueDest(refDir, name)
	if err := c.SaveUploadedFile(fh, dst); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "导入失败：" + err.Error()})
		return
	}
	// 导入者的「归类定级声明」：类别(内部/外部/公开)决定默认级别，可手动改级；
	// 落 sidecar 到参考桶，供后续「一键归档」据此定级。按最终落盘文件名记录（uniqueDest 可能改名）。
	category := strings.TrimSpace(c.PostForm("category"))
	level := strings.TrimSpace(c.PostForm("sensitivity_level"))
	finalName := filepath.Base(dst)
	if err := repository.WriteRefGrade(refDir, finalName, category, level); err != nil {
		// 定级清单写失败不阻断导入（文件已落盘），仅提示。
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"path": dst, "name": finalName}, "warning": "定级声明保存失败：" + err.Error()})
		return
	}
	g, _ := repository.ReadRefGrade(refDir, finalName)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"path": dst, "name": finalName, "category": g.Category, "sensitivity_level": g.SensitivityLevel}})
}
