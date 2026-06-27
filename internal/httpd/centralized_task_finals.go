package httpd

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

// 任务级定稿（2026-06-11 五层落盘）：定稿是文件任务级的——谁编辑谁挑。
// 参与人在「工作事项」完成自己任务时，按本任务的 output 文档标识，从本任务 process
// 目录挑过程文件作定稿，拷到本任务 output 目录并自动归档。环节负责人交付只做汇总流转。

// TaskFinalsCandidates GET /centralized-projects/task-finals-candidates
//
//	?app_id&stage_code&task_code&template_code[&project_code]
//
// 返回该文件任务的 output 标识 + 该任务 process 下可挑的非空过程文件。
func TaskFinalsCandidates(c *gin.Context) {
	db := repository.GetDB()
	appID := strings.TrimSpace(c.Query("app_id"))
	stageCode := strings.TrimSpace(c.Query("stage_code"))
	taskCode := strings.TrimSpace(c.Query("task_code"))
	templateCode := strings.TrimSpace(c.Query("template_code"))
	projectCode := strings.TrimSpace(c.Query("project_code"))
	if appID == "" || stageCode == "" || taskCode == "" || templateCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 app_id/stage_code/task_code/template_code"})
		return
	}
	if err := ensureProjectSyncedLocal(templateCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	appIDInt, _ := strconv.ParseInt(appID, 10, 64)
	dirProject := centralizedDirCode(db, appIDInt, projectCode)

	rules, err := repository.ListTaskOutputRules(db, templateCode, stageCode, taskCode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	// 候选定稿来源：本任务 process/ + output/ 的非空文件（用户在哪填的都能挑）。
	files, _ := repository.ListTaskFinalCandidateFiles(db, dirProject, stageCode, taskCode)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"output_rules":  rules,
		"process_files": files,
	}})
}

// TaskFileRules GET /centralized-projects/task-file-rules
//
//	?stage_code&task_code&template_code
//
// 返回该文件任务下的全部文档标识（input/process/output）及属性，供「工作受理」展示文档属性。
func TaskFileRules(c *gin.Context) {
	db := repository.GetDB()
	stageCode := strings.TrimSpace(c.Query("stage_code"))
	taskCode := strings.TrimSpace(c.Query("task_code"))
	templateCode := strings.TrimSpace(c.Query("template_code"))
	if stageCode == "" || taskCode == "" || templateCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 stage_code/task_code/template_code"})
		return
	}
	if err := ensureProjectSyncedLocal(templateCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	rules, err := repository.ListTaskFileRules(db, templateCode, stageCode, taskCode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rules})
}

// SubmitTaskFinals POST /centralized-projects/submit-task-finals
// body{app_id, stage_code, task_code, template_code, project_code, selections:[{file_rule_code, source_file}]}
// 把本任务的过程文件按 output 标识拷成定稿落到本任务 output 目录，并自动归档（幂等）。
func SubmitTaskFinals(c *gin.Context) {
	db := repository.GetDB()
	var body struct {
		AppID        int64                       `json:"app_id"`
		StageCode    string                      `json:"stage_code"`
		TaskCode     string                      `json:"task_code"`
		TemplateCode string                      `json:"template_code"`
		ProjectCode  string                      `json:"project_code"`
		Selections   []repository.FinalSelection `json:"selections"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if body.AppID == 0 || body.StageCode == "" || body.TaskCode == "" || body.TemplateCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "参数不完整（app_id/stage_code/task_code/template_code）"})
		return
	}
	if err := ensureProjectSyncedLocal(body.TemplateCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	dirProject := centralizedDirCode(db, body.AppID, body.ProjectCode)

	// 五层落盘：每个 selection 落到本任务目录（统一打上 task_code，前端无需重复传）。
	for i := range body.Selections {
		body.Selections[i].TaskCode = body.TaskCode
	}
	var finals []string
	if len(body.Selections) > 0 {
		created, err := repository.SubmitStageFinalsToProject(db, body.TemplateCode, dirProject, body.StageCode, body.Selections)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
			return
		}
		finals = created
	}
	// 自动归档：遍历任务目录的 process+output 挂账（幂等，失败不阻断）。
	archive, _ := repository.AutoArchiveStageForProject(db, body.TemplateCode, dirProject, body.StageCode)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"finals": finals, "archive": archive}})
}
