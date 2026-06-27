# 三级分工级联 P3「文件任务受理 + 开始工作」实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** 文件任务参与人受理分给自己的任务，「开始工作」按模版建该任务的目录/空文件并接入现有工作台编辑；含 Tier-3 未读角标；下线「我的工作事项」导航。

**Architecture:** manage 给 `centralized_project_task_assignments` 加 `assignee_viewed_at`，加 4 repo 方法 + 4 接口。scan 加任务级脚手架 `ScaffoldTaskDocsForProject`、`start-task` 处理器（调 manage 置状态 + 本地建文件）、3 代理、新视图 + 路由/导航/Tier-3 角标，并从导航下线「我的工作事项」。

**Tech Stack:** manage Nitro+better-sqlite3+vitest；scan Gin+sqlx+Go test、Vue3/Vuetify+vitest。

**约束：** scan 用 yarn、前端测试前 `npm rebuild better-sqlite3`；manage `npx vitest`；严禁删改扫描文件（本期只在工作空间建空文件，幂等）；路径用 ProjectWorkspace(filepath.Join)跨平台。manage 信封 `{code,message,data}`，scan `{success,data,error}`。

---

## Task 1: manage — 迁移 + 4 repo 方法

**Files:** Modify `server/database/index.ts`, `server/database/centralized-project-repository.ts`; Test `tests/centralized-task-receive.test.ts`.

- [ ] **Step 1: 写失败测试** — 新建 `tests/centralized-task-receive.test.ts`：
```ts
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import Database from 'better-sqlite3'
import { readFileSync } from 'fs'
import { join } from 'path'

const h = vi.hoisted(() => ({ db: null as any }))
vi.mock('../server/database/index', () => ({ getDatabase: () => h.db }))
const DB_SQL = readFileSync(join(__dirname, '../server/database/database.sql'), 'utf-8')

const DDL = `
CREATE TABLE IF NOT EXISTS centralized_project_applications (
  id INTEGER PRIMARY KEY AUTOINCREMENT, scan_origin_id INTEGER NOT NULL, project_name VARCHAR(500) NOT NULL,
  owner_name VARCHAR(200) NOT NULL, status VARCHAR(40) NOT NULL DEFAULT 'pending', template_id INTEGER,
  template_code VARCHAR(100), template_version VARCHAR(40), sensitivity_level VARCHAR(40) DEFAULT 'general',
  create_time DATETIME DEFAULT CURRENT_TIMESTAMP, update_time DATETIME DEFAULT CURRENT_TIMESTAMP, disabled INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS centralized_project_stage_assignments (
  id INTEGER PRIMARY KEY AUTOINCREMENT, application_id INTEGER NOT NULL, stage_code VARCHAR(100) NOT NULL,
  stage_name VARCHAR(200) NOT NULL, assignee_username VARCHAR(200) NOT NULL, sort_order INTEGER DEFAULT 0,
  status VARCHAR(40) DEFAULT 'pending', disabled INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS centralized_project_task_assignments (
  id INTEGER PRIMARY KEY AUTOINCREMENT, application_id INTEGER NOT NULL, stage_code VARCHAR(100) NOT NULL,
  task_code VARCHAR(100) NOT NULL, task_name VARCHAR(200) NOT NULL, assignee_username VARCHAR(200) NOT NULL,
  sort_order INTEGER DEFAULT 0, status VARCHAR(40) NOT NULL DEFAULT 'pending', started_at DATETIME, completed_at DATETIME,
  assignee_viewed_at DATETIME, create_time DATETIME DEFAULT CURRENT_TIMESTAMP, disabled INTEGER DEFAULT 0
);`

function seedUser(db:any,u:string){ db.prepare(`INSERT INTO auth_users (username, display_name, password_hash, user_unit, user_department, phone, role, status, disabled) VALUES (?, ?, 'x','U','D','1','user','active',0)`).run(u,u) }

describe('文件任务受理', () => {
  let repo: any
  beforeEach(async () => {
    h.db = new Database(':memory:'); h.db.exec(DB_SQL); h.db.exec(DDL)
    seedUser(h.db,'u1'); seedUser(h.db,'u2')
    h.db.prepare(`INSERT INTO centralized_project_applications (scan_origin_id, project_name, owner_name, status, template_id, template_code) VALUES (1,'甲项目','lead','accepted',9,'TPL-X')`).run()
    h.db.prepare(`INSERT INTO centralized_project_stage_assignments (application_id, stage_code, stage_name, assignee_username) VALUES (1,'STG-1','收稿','lead')`).run()
    h.db.prepare(`INSERT INTO centralized_project_task_assignments (application_id, stage_code, task_code, task_name, assignee_username, status) VALUES (1,'STG-1','TK-1','录入','u1','pending')`).run()
    h.db.prepare(`INSERT INTO centralized_project_task_assignments (application_id, stage_code, task_code, task_name, assignee_username, status) VALUES (1,'STG-1','TK-2','校对','u2','pending')`).run()
    ;({ centralizedProjectRepository: repo } = await import('../server/database/centralized-project-repository'))
  })
  afterEach(() => { h.db.close(); vi.resetModules() })

  it('myTasksForAssignee 返回我的任务 + 上下文', () => {
    const list = repo.myTasksForAssignee('u1')
    expect(list.length).toBe(1)
    expect(list[0].task_code).toBe('TK-1')
    expect(list[0].project_name).toBe('甲项目')
    expect(list[0].template_code).toBe('TPL-X')
    expect(list[0].stage_name).toBe('收稿')
  })

  it('startTask 仅参与人可开始，置 in_progress', () => {
    expect(() => repo.startTask(1,'STG-1','TK-1','u2')).toThrow()
    const r = repo.startTask(1,'STG-1','TK-1','u1')
    expect(r.status).toBe('in_progress')
    expect(r.template_code).toBe('TPL-X')
    const row:any = h.db.prepare(`SELECT status, started_at FROM centralized_project_task_assignments WHERE task_code='TK-1' AND disabled=0`).get()
    expect(row.status).toBe('in_progress')
    expect(row.started_at).not.toBeNull()
  })

  it('task 未读：仅本人、置已读归零', () => {
    expect(repo.taskUnreadCountForAssignee('u1')).toBe(1)
    expect(repo.markTasksSeenForAssignee('u1')).toBe(1)
    expect(repo.taskUnreadCountForAssignee('u1')).toBe(0)
    expect(repo.taskUnreadCountForAssignee('u2')).toBe(1)
  })
})
```

- [ ] **Step 2: 运行确认失败** — `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-task-receive.test.ts` → FAIL（方法未定义）

- [ ] **Step 3: 加迁移列** — `server/database/index.ts`，在 P2 的 task 列迁移（`completed_at`）之后加：
```ts
  addColumnIfMissing(database, 'centralized_project_task_assignments', 'assignee_viewed_at', 'DATETIME')
```

- [ ] **Step 4: 实现 4 方法** — `centralized-project-repository.ts`，在 `markStagesSeenForAssignee` 之后加：
```ts
  /** 参与人的文件任务（含项目/环节/模版上下文）。 */
  myTasksForAssignee(assignee: string): Array<{ application_id: number; stage_code: string; stage_name: string | null; task_code: string; task_name: string; status: string; project_name: string; template_code: string | null }> {
    const db = getDatabase()
    return db.prepare(`
      SELECT t.application_id, t.stage_code, t.task_code, t.task_name, t.status,
             a.project_name, a.template_code,
             (SELECT s.stage_name FROM centralized_project_stage_assignments s
               WHERE s.application_id = t.application_id AND s.stage_code = t.stage_code AND s.disabled = 0 LIMIT 1) AS stage_name
        FROM centralized_project_task_assignments t
        JOIN centralized_project_applications a ON a.id = t.application_id
       WHERE t.assignee_username = ? AND t.disabled = 0 AND a.disabled = 0
       ORDER BY t.application_id DESC, t.id`).all((assignee || '').trim()) as any
  },

  /** 参与人开始工作：置 in_progress（仅本人、非已完成）。返回任务上下文供 scan 脚手架。 */
  startTask(applicationId: number, stageCode: string, taskCode: string, actor: string): { application_id: number; stage_code: string; task_code: string; template_code: string | null; status: string } {
    const db = getDatabase()
    const name = (actor || '').trim()
    const row = db.prepare(`SELECT id FROM centralized_project_task_assignments
       WHERE application_id = ? AND stage_code = ? AND task_code = ? AND assignee_username = ? AND disabled = 0`)
      .get(applicationId, stageCode, taskCode, name)
    if (!row) throw new Error('只有该文件任务参与人可以开始工作')
    db.prepare(`UPDATE centralized_project_task_assignments
       SET status = 'in_progress', started_at = CURRENT_TIMESTAMP
       WHERE application_id = ? AND stage_code = ? AND task_code = ? AND assignee_username = ? AND disabled = 0 AND status != 'completed'`)
      .run(applicationId, stageCode, taskCode, name)
    const app = this.findById(applicationId)
    return { application_id: applicationId, stage_code: stageCode, task_code: taskCode, template_code: app?.template_code ?? null, status: 'in_progress' }
  },

  /** 参与人未读任务数。 */
  taskUnreadCountForAssignee(assignee: string): number {
    const db = getDatabase()
    const r = db.prepare(`SELECT COUNT(*) AS c FROM centralized_project_task_assignments
        WHERE assignee_username = ? AND assignee_viewed_at IS NULL AND disabled = 0`).get((assignee || '').trim()) as { c: number }
    return r?.c ?? 0
  },

  /** 标记本人所有未读任务为已读。 */
  markTasksSeenForAssignee(assignee: string): number {
    const db = getDatabase()
    const res = db.prepare(`UPDATE centralized_project_task_assignments
        SET assignee_viewed_at = CURRENT_TIMESTAMP
        WHERE assignee_username = ? AND assignee_viewed_at IS NULL AND disabled = 0`).run((assignee || '').trim())
    return res.changes
  },
```

- [ ] **Step 5: 运行确认通过** — `npx vitest run tests/centralized-task-receive.test.ts` → 3 PASS。

- [ ] **Step 6: 提交**
```bash
cd /root/data/projects/data-asset-manage
git add server/database/index.ts server/database/centralized-project-repository.ts tests/centralized-task-receive.test.ts
git commit -m "feat(manage): 文件任务受理 repo（myTasks/startTask/task 未读 + assignee_viewed_at）"
```

---

## Task 2: manage — 4 接口

**Files:** Create under `server/api/centralized-projects/`: `my-tasks.get.ts`, `start-task.post.ts`, `task-unread-count.get.ts`, `mark-tasks-seen.post.ts`.

- [ ] **Step 1: my-tasks.get.ts**
```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'
/** GET /api/centralized-projects/my-tasks?assignee= → 我的文件任务 */
export default defineEventHandler((event) => {
  try {
    const assignee = String(getQuery(event).assignee || '').trim()
    if (!assignee) return { code: 400, message: 'assignee 必填', data: null }
    return { code: 0, message: 'success', data: centralizedProjectRepository.myTasksForAssignee(assignee) }
  } catch (e: any) { return { code: 500, message: e.message || '查询失败', data: null } }
})
```

- [ ] **Step 2: start-task.post.ts**
```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'
/** POST /api/centralized-projects/start-task body:{actor, application_id, stage_code, task_code} */
export default defineEventHandler(async (event) => {
  try {
    const body = await readBody<{ actor: string; application_id: number; stage_code: string; task_code: string }>(event)
    const actor = (body?.actor || '').trim()
    const appId = Number(body?.application_id)
    const stageCode = (body?.stage_code || '').trim()
    const taskCode = (body?.task_code || '').trim()
    if (!actor || !appId || !stageCode || !taskCode) return { code: 400, message: '参数不完整', data: null }
    return { code: 0, message: 'success', data: centralizedProjectRepository.startTask(appId, stageCode, taskCode, actor) }
  } catch (e: any) { return { code: 400, message: e.message || '开始失败', data: null } }
})
```

- [ ] **Step 3: task-unread-count.get.ts**
```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'
/** GET /api/centralized-projects/task-unread-count?assignee= → { count } */
export default defineEventHandler((event) => {
  try {
    const assignee = String(getQuery(event).assignee || '').trim()
    if (!assignee) return { code: 400, message: 'assignee 必填', data: null }
    return { code: 0, message: 'success', data: { count: centralizedProjectRepository.taskUnreadCountForAssignee(assignee) } }
  } catch (e: any) { return { code: 500, message: e.message || '查询失败', data: null } }
})
```

- [ ] **Step 4: mark-tasks-seen.post.ts**
```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'
/** POST /api/centralized-projects/mark-tasks-seen body:{assignee} → { updated } */
export default defineEventHandler(async (event) => {
  try {
    const body = await readBody<{ assignee: string }>(event)
    const assignee = String(body?.assignee || '').trim()
    if (!assignee) return { code: 400, message: 'assignee 必填', data: null }
    return { code: 0, message: 'success', data: { updated: centralizedProjectRepository.markTasksSeenForAssignee(assignee) } }
  } catch (e: any) { return { code: 500, message: e.message || '标记失败', data: null } }
})
```

- [ ] **Step 5: 验证** — `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-task-receive.test.ts` → PASS（无回归；端点本仓库不单测）。确认 4 文件方法名与 repo 一致。

- [ ] **Step 6: 提交**
```bash
cd /root/data/projects/data-asset-manage
git add server/api/centralized-projects/my-tasks.get.ts server/api/centralized-projects/start-task.post.ts server/api/centralized-projects/task-unread-count.get.ts server/api/centralized-projects/mark-tasks-seen.post.ts
git commit -m "feat(manage): 文件任务受理 4 接口（my-tasks/start-task/task 未读）"
```

---

## Task 3: scan — ScaffoldTaskDocsForProject 助手

**Files:** Modify `internal/repository/stage_scaffold.go`; Test `internal/repository/task_scaffold_test.go`.

- [ ] **Step 1: 写失败测试** — 新建 `internal/repository/task_scaffold_test.go`（模型同 centralized_stage_docs_test.go）：
```go
package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScaffoldTaskDocsForProject_OnlyThatTask(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk1, _ := repo.CreateTask(st.ID, TaskInput{Name: "录入", SensitivityLevel: "important"})
	tk2, _ := repo.CreateTask(st.ID, TaskInput{Name: "校对", SensitivityLevel: "important"})
	repo.CreateFileRule(tk1.ID, FileRuleInput{FileName: "录入登记表", DataState: "process", AllowedFileTypes: "docx"})
	repo.CreateFileRule(tk2.ID, FileRuleInput{FileName: "校对单", DataState: "process", AllowedFileTypes: "docx"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	vp := "CPA-555"
	if err := ws.CreateProjectTree(vp, []string{st.StageCode}); err != nil {
		t.Fatal(err)
	}

	created, err := ScaffoldTaskDocsForProject(db, tpl.TemplateCode, vp, st.StageCode, tk1.TaskCode)
	if err != nil {
		t.Fatalf("脚手架失败: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("应只建 TK-1 的 1 个文件，实得 %d: %v", len(created), created)
	}
	proc := ws.StageStateDir(vp, st.StageCode, "process")
	if _, err := os.Stat(filepath.Join(proc, "录入登记表.docx")); err != nil {
		t.Fatalf("应建录入登记表.docx: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proc, "校对单.docx")); err == nil {
		t.Fatal("不应建别的任务(校对)的文件")
	}
}
```
（注：`CreateTask` 返回的对象有 `TaskCode` 字段；若实际字段名不同，按实际调整。先读 `template_authoring_repo.go` 确认 `CreateTask`/`CreateFileRule`/返回结构的字段名。）

- [ ] **Step 2: 运行确认失败** — `cd /root/data/projects/data-asset-scan && go test ./internal/repository/ -run TestScaffoldTaskDocsForProject_OnlyThatTask -v` → FAIL（未定义）

- [ ] **Step 3: 实现助手** — 在 `internal/repository/stage_scaffold.go` 末尾加：
```go
// ScaffoldTaskDocsForProject 按 templateCode 下某「工作环节-文件任务」的过程文档标识，
// 在 projectCode(CPA 虚拟项目) 的该环节 process 目录预建空占位文件。幂等：已存在不覆盖。
func ScaffoldTaskDocsForProject(db *sqlx.DB, templateCode, projectCode, stageCode, taskCode string) ([]string, error) {
	var tplID int64
	if err := db.Get(&tplID, `SELECT id FROM data_templates WHERE template_code = ? AND disable = 0`, templateCode); err != nil {
		return nil, fmt.Errorf("本地模版缺失: %s: %w", templateCode, err)
	}
	var stageID int64
	if err := db.Get(&stageID, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ? AND disable = 0`, tplID, stageCode); err != nil {
		return nil, fmt.Errorf("环节不存在: %s: %w", stageCode, err)
	}
	var taskID int64
	if err := db.Get(&taskID, `SELECT id FROM template_tasks WHERE template_stage_id = ? AND task_code = ? AND disable = 0`, stageID, taskCode); err != nil {
		return nil, fmt.Errorf("文件任务不存在: %s: %w", taskCode, err)
	}
	type rule struct {
		FileName string `db:"file_name"`
		Allowed  string `db:"allowed_file_types"`
	}
	var rules []rule
	if err := db.Select(&rules, `
		SELECT file_name, allowed_file_types
		FROM template_file_rules
		WHERE template_task_id = ? AND disable = 0 AND data_state = 'process'`, taskID); err != nil {
		return nil, fmt.Errorf("读取文件任务文档标识失败: %w", err)
	}
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := NewProjectWorkspace(root)
	dir := ws.StageStateDir(projectCode, stageCode, "process")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("建过程目录失败 %s: %w", dir, err)
	}
	var created []string
	for _, r := range rules {
		if strings.TrimSpace(r.FileName) == "" {
			continue
		}
		path := filepath.Join(dir, sanitizeFileName(r.FileName)+firstFileExt(r.Allowed))
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
			return created, fmt.Errorf("建占位文件失败 %s: %w", path, err)
		}
		created = append(created, path)
	}
	return created, nil
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/repository/ -run TestScaffoldTaskDocsForProject_OnlyThatTask -v` → PASS

- [ ] **Step 5: 提交**
```bash
cd /root/data/projects/data-asset-scan
git add internal/repository/stage_scaffold.go internal/repository/task_scaffold_test.go
git commit -m "feat(scan): 任务级脚手架 ScaffoldTaskDocsForProject（按 template_task_id 建过程占位）"
```

---

## Task 4: scan — 3 代理 + start-task 处理器

**Files:** Modify `internal/httpd/centralized_projects.go`; Test `internal/httpd/centralized_projects_p3_test.go`.

- [ ] **Step 1: 写失败测试** — 新建 `internal/httpd/centralized_projects_p3_test.go`：
```go
package httpd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_CentralizedProjects_P3(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	// 本地模版五层 + 任务文件标识
	repo := repository.NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(repository.CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, repository.StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, repository.TaskInput{Name: "录入", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, repository.FileRuleInput{FileName: "录入登记表", DataState: "process", AllowedFileTypes: "docx"})

	root := t.TempDir()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyProjectRoot, root)

	var startBody, myTasksAssignee, markBody string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "my-tasks"):
			myTasksAssignee = req.URL.Query().Get("assignee")
		case strings.Contains(req.URL.Path, "start-task"):
			b := make([]byte, req.ContentLength); _, _ = req.Body.Read(b); startBody = string(b)
		case strings.Contains(req.URL.Path, "mark-tasks-seen"):
			b := make([]byte, req.ContentLength); _, _ = req.Body.Read(b); markBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"count":1,"updated":1}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// my-tasks 代理
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/my-tasks")
	successOk(t, status, resp)
	if myTasksAssignee != "u1" { t.Errorf("my-tasks assignee=%q want u1", myTasksAssignee) }

	// mark-tasks-seen body
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/mark-tasks-seen", map[string]interface{}{})
	successOk(t, status, resp)
	if !strings.Contains(markBody, "\"assignee\":\"u1\"") { t.Errorf("mark-tasks-seen body=%s", markBody) }

	// start-task：调 manage + 本地建文件
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/start-task", map[string]interface{}{
		"application_id": 555, "stage_code": st.StageCode, "task_code": tk.TaskCode, "template_code": tpl.TemplateCode,
	})
	successOk(t, status, resp)
	if !strings.Contains(startBody, "\"actor\":\"u1\"") { t.Errorf("start-task 应注入 actor=u1，body=%s", startBody) }
	// 文件建在 CPA-555 该环节 process
	ws := repository.NewProjectWorkspace(root)
	if _, err := os.Stat(filepath.Join(ws.StageStateDir("CPA-555", st.StageCode, "process"), "录入登记表.docx")); err != nil {
		t.Fatalf("start-task 应建任务过程文件: %v", err)
	}
}
```
（先读 `template_authoring_repo.go` 确认 `CreateTemplateInput/StageInput/TaskInput/FileRuleInput` 与返回字段 `TemplateCode/StageCode/TaskCode` 的真实名字；按实调整测试。）

- [ ] **Step 2: 运行确认失败** — `go test ./internal/httpd/ -run TestHTTP_CentralizedProjects_P3 -v` → FAIL（404）

- [ ] **Step 3: 注册路由** — 在 `RegisterCentralizedProjectsRoutes` 加：
```go
	r.GET("/my-tasks", MyTasksProxy)
	r.POST("/start-task", StartTaskHandler)
	r.GET("/task-unread-count", TaskUnreadCountProxy)
	r.POST("/mark-tasks-seen", MarkTasksSeenProxy)
```

- [ ] **Step 4: 实现 handlers** — 在 `centralized_projects.go` 末尾加（复用 `getManageEndpoint`/`proxyToManage`/`currentOperator`/`encodeQuery`；新增 import `repository` 已在；`bytes`/`io`/`encoding/json`/`time` 已在）：
```go
// MyTasksProxy GET /centralized-projects/my-tasks —— assignee=operator。
func MyTasksProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil { c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()}); return }
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/my-tasks?assignee=%s", ep, encodeQuery(currentOperator(c))), nil)
}

// TaskUnreadCountProxy GET /centralized-projects/task-unread-count —— assignee=operator。
func TaskUnreadCountProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil { c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()}); return }
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/task-unread-count?assignee=%s", ep, encodeQuery(currentOperator(c))), nil)
}

// MarkTasksSeenProxy POST /centralized-projects/mark-tasks-seen —— assignee=operator（body）。
func MarkTasksSeenProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil { c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()}); return }
	b, _ := json.Marshal(map[string]any{"assignee": currentOperator(c)})
	proxyToManage(c, "POST", ep+"/api/centralized-projects/mark-tasks-seen", b)
}

// StartTaskHandler POST /centralized-projects/start-task —— 调 manage 置 in_progress + 本地按任务建过程占位。
// body: {application_id, stage_code, task_code, template_code}
func StartTaskHandler(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil { c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()}); return }
	var body struct {
		ApplicationID int64  `json:"application_id"`
		StageCode     string `json:"stage_code"`
		TaskCode      string `json:"task_code"`
		TemplateCode  string `json:"template_code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()}); return
	}
	if body.ApplicationID == 0 || body.StageCode == "" || body.TaskCode == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "参数不完整"}); return
	}
	// 1) 调 manage 置状态（actor=operator 校验参与人）
	mb, _ := json.Marshal(map[string]any{
		"actor": currentOperator(c), "application_id": body.ApplicationID,
		"stage_code": body.StageCode, "task_code": body.TaskCode,
	})
	req, _ := http.NewRequest("POST", ep+"/api/centralized-projects/start-task", bytes.NewReader(mb))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil { c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()}); return }
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": out.Message}); return
	}
	// 2) 本地按任务建过程占位（虚拟项目 CPA-{appid}）
	vp := fmt.Sprintf("CPA-%d", body.ApplicationID)
	db := repository.GetDB()
	wsroot := repository.NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := repository.NewProjectWorkspace(wsroot)
	if _, err := ws.CreateStageDir(vp, body.StageCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "建目录失败: " + err.Error()}); return
	}
	created, scErr := repository.ScaffoldTaskDocsForProject(db, body.TemplateCode, vp, body.StageCode, body.TaskCode)
	if scErr != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": scErr.Error()}); return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"scaffolded": len(created), "app_id": body.ApplicationID, "stage_code": body.StageCode,
	}})
}
```

- [ ] **Step 5: 运行确认通过** — `go test ./internal/httpd/ -run 'CentralizedProjects' -v` → PASS（P3 + 既有不回归）

- [ ] **Step 6: 提交**
```bash
cd /root/data/projects/data-asset-scan
git add internal/httpd/centralized_projects.go internal/httpd/centralized_projects_p3_test.go
git commit -m "feat(scan): 文件任务受理代理 + start-task(置状态+按任务建占位)"
```

---

## Task 5: scan — FileTaskReceiveView 视图

**Files:** Create `frontend_real/views/FileTaskReceiveView.vue` + `frontend_real/__tests__/FileTaskReceiveView.test.ts`.

- [ ] **Step 1: 写失败测试**
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import FileTaskReceiveView from '../views/FileTaskReceiveView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })
function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}
function mountView() {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: { template: '<div/>' } },
    { path: '/stage-workbench', name: 'StageWorkbench', component: { template: '<div/>' } },
  ] })
  return { wrapper: mount(FileTaskReceiveView, { global: { plugins: [vuetify, router] } }), router }
}

describe('文件任务受理', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('列出我的文件任务', async () => {
    mockFetch((url) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'pending', project_name: '甲项目', template_code: 'TPL-X' },
      ])
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('录入')
    expect(wrapper.text()).toContain('甲项目')
    expect(wrapper.text()).toContain('开始工作')
  })

  it('开始工作调 start-task 后导航工作台', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'pending', project_name: '甲项目', template_code: 'TPL-X' },
      ])
      if (url.includes('/start-task') && init?.method === 'POST') { posted.push(JSON.parse(init.body as string)); return ok({ scaffolded: 1, app_id: 7, stage_code: 'STG-1' }) }
      return ok([])
    })
    const { wrapper, router } = mountView(); await flushPromises()
    const push = vi.spyOn(router, 'push')
    const vm: any = wrapper.vm
    await vm.startWork(vm.items[0])
    await flushPromises()
    expect(posted[0].application_id).toBe(7)
    expect(posted[0].task_code).toBe('TK-1')
    expect(posted[0].template_code).toBe('TPL-X')
    expect(push).toHaveBeenCalled()
    const arg = push.mock.calls[0][0] as any
    expect(arg.path).toBe('/stage-workbench')
    expect(arg.query.app_id).toBe(7)
    expect(arg.query.stage_code).toBe('STG-1')
  })
})
```

- [ ] **Step 2: 运行确认失败** — `cd /root/data/projects/data-asset-scan && npm rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/FileTaskReceiveView.test.ts` → FAIL

- [ ] **Step 3: 实现视图** — 新建 `frontend_real/views/FileTaskReceiveView.vue`:
```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { API_BASE } from '@/services/api'

interface MyTask {
  application_id: number
  stage_code: string
  stage_name: string | null
  task_code: string
  task_name: string
  status: string
  project_name: string
  template_code: string | null
}

const router = useRouter()
const loading = ref(false)
const items = ref<MyTask[]>([])
const snackbar = ref({ show: false, text: '', color: 'success' })
const starting = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/my-tasks`)
    const j = await r.json()
    if (j.success) items.value = (j.data || []) as MyTask[]
  } finally { loading.value = false }
}

function gotoWorkbench(it: MyTask) {
  router.push({ path: '/stage-workbench', query: {
    app_id: it.application_id, stage_code: it.stage_code,
    stage_name: it.stage_name || '', project_name: it.project_name || '',
  } })
}

async function startWork(it: MyTask) {
  starting.value = it.task_code
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/start-task`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ application_id: it.application_id, stage_code: it.stage_code, task_code: it.task_code, template_code: it.template_code }),
    })
    const j = await r.json()
    if (j.success) { snackbar.value = { show: true, text: `已开始工作，建了 ${j.data?.scaffolded ?? 0} 个文件`, color: 'success' }; gotoWorkbench(it) }
    else snackbar.value = { show: true, text: '开始失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '开始失败：' + (e?.message || String(e)), color: 'error' }
  } finally { starting.value = null }
}

function statusLabel(s: string): string {
  return ({ pending: '待开始', in_progress: '进行中', completed: '已完成' } as Record<string, string>)[s] || s
}

const headers = [
  { title: '项目', key: 'project_name' },
  { title: '工作环节', key: 'stage_name' },
  { title: '文件任务', key: 'task_name' },
  { title: '状态', key: 'status', width: '110px' },
  { title: '操作', key: 'actions', width: '160px', sortable: false },
]

onMounted(load)
</script>

<template>
  <div>
    <v-card elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-clipboard-check-outline</v-icon>
        文件任务受理
        <v-spacer />
        <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
      </v-card-title>
      <v-data-table :headers="headers" :items="items" :loading="loading" item-value="task_code" :items-per-page="50" hide-default-footer>
        <template #item.stage_name="{ item }">{{ item.stage_name || item.stage_code }}</template>
        <template #item.status="{ item }">
          <v-chip size="x-small" variant="tonal">{{ statusLabel(item.status) }}</v-chip>
        </template>
        <template #item.actions="{ item }">
          <v-btn v-if="item.status === 'pending'" size="small" color="primary" variant="flat" :loading="starting === item.task_code" prepend-icon="mdi-play" @click="startWork(item)">开始工作</v-btn>
          <v-btn v-else size="small" color="primary" variant="tonal" prepend-icon="mdi-folder-open-outline" @click="gotoWorkbench(item)">进入工作台</v-btn>
        </template>
        <template #no-data>
          <div class="text-center py-8 text-grey">暂无指派给我的文件任务</div>
        </template>
      </v-data-table>
    </v-card>
    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </div>
</template>
```

- [ ] **Step 4: 运行确认通过** — `npm rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/FileTaskReceiveView.test.ts` → PASS

- [ ] **Step 5: 提交**
```bash
cd /root/data/projects/data-asset-scan
git add frontend_real/views/FileTaskReceiveView.vue frontend_real/__tests__/FileTaskReceiveView.test.ts
git commit -m "feat(scan): 文件任务受理视图 FileTaskReceiveView（开始工作→建占位→进工作台）"
```

---

## Task 6: scan — 路由 + 导航 + Tier-3 角标 + 下线我的工作事项

**Files:** Modify `frontend_real/plugins/router.ts`, `frontend_real/App.vue`, `frontend_real/__tests__/frontend-integration.test.ts`.

- [ ] **Step 1: 加路由** — router.ts 加（用 @/views 别名）：
```ts
  {
    path: '/file-task-receive',
    name: 'FileTaskReceive',
    component: () => import('@/views/FileTaskReceiveView.vue'),
  },
```

- [ ] **Step 2: 导航** — App.vue navItems「数据业务服务」组：在「文件任务指派」后加「文件任务受理」，并把「我的工作事项」那项**注释掉**（保留路由 /my-work-items 与 MyWorkItemsView 作回滚）：
```ts
      { title: '文件任务受理', icon: 'mdi-clipboard-check-outline', to: '/file-task-receive', badge: 'recvtask', hint: '作为文件任务参与人：开始工作建目录与文件，进入工作台编辑' },
      // 2026-06-09 三级分工级联：「我的工作事项」由 工作事项分工/文件任务指派/文件任务受理 取代，导航下线（路由保留作回滚）
      // { title: '我的工作事项', icon: 'mdi-clipboard-check-outline', to: '/my-work-items', hint: '...' },
```

- [ ] **Step 3: 角标脚本** — App.vue 加（仿 filetask）：
```ts
const recvtaskUnread = ref(0)
async function loadRecvtaskUnread() {
  try { const r = await fetch(`${API_BASE}/centralized-projects/task-unread-count`); const j = await r.json(); if (j.success) recvtaskUnread.value = Number(j.data?.count) || 0 } catch {}
}
async function clearRecvtaskUnread() {
  try { await fetch(`${API_BASE}/centralized-projects/mark-tasks-seen`, { method: 'POST' }); recvtaskUnread.value = 0 } catch {}
}
```
onMounted 非 shellless 分支加 `loadRecvtaskUnread()`；route watch 加（保留已有 worktask/filetask 分支）：
```ts
  if (p === '/file-task-receive') clearRecvtaskUnread()
  else if (old === '/file-task-receive') loadRecvtaskUnread()
```

- [ ] **Step 4: 角标模板** — 扩展 `#append` 条件再加 recvtask（沿用 child 变量）：
```vue
                  <template #append v-if="(child.badge === 'worktask' && worktaskUnread > 0) || (child.badge === 'filetask' && filetaskUnread > 0) || (child.badge === 'recvtask' && recvtaskUnread > 0)">
                    <v-chip size="x-small" color="error" variant="flat">{{ child.badge === 'worktask' ? worktaskUnread : child.badge === 'filetask' ? filetaskUnread : recvtaskUnread }}</v-chip>
                  </template>
```

- [ ] **Step 5: 整合测试** — 更新 `frontend_real/__tests__/frontend-integration.test.ts` 的导航断言：原断言「我的工作事项」存在的地方改为断言其**不在导航**、并新增「文件任务受理」断言。具体：把 nav-items 测试里 `expect(wrapper.text()).toContain('我的工作事项')` 改为 `expect(drawerHtml).not.toContain('我的工作事项')`（用 drawerHtml），并在立项流程那条测试加 `expect(drawerHtml).toContain('文件任务受理')` + `expect(drawerHtml).toContain('/file-task-receive')`；其中「环节任务已并入我的工作事项」相关旧断言按现实改写（我的工作事项已下线）。读该测试文件按实际调整，使全绿。

- [ ] **Step 6: 验证** — `cd /root/data/projects/data-asset-scan && yarn build 2>&1 | tail -6` 成功；`yarn vitest run frontend_real/__tests__/ 2>&1 | tail -10` 全绿。

- [ ] **Step 7: 提交**
```bash
cd /root/data/projects/data-asset-scan
git add frontend_real/plugins/router.ts frontend_real/App.vue frontend_real/__tests__/frontend-integration.test.ts
git commit -m "feat(scan): 「文件任务受理」导航/路由 + Tier-3 角标；导航下线我的工作事项"
```

---

## 自查
- **Spec 覆盖**：迁移+repo(T1)/接口(T2)/任务脚手架(T3)/代理+start-task(T4)/视图(T5)/路由导航角标+下线(T6) 全覆盖。
- **类型一致**：start-task body `{application_id,stage_code,task_code,template_code}`（scan 视图→scan handler；scan handler→manage 用 `{actor,application_id,stage_code,task_code}`）；my-tasks 字段与 MyTask 接口一致；task-unread `{count}`/mark-tasks-seen `{updated}`。
- **占位符**：无 TODO；每步完整代码（仅 T3/T4 测试要求实现者先确认 `CreateTask`/`CreateFileRule` 返回字段真名，已显式说明）。

## 风险与注意
- **CreateTask/CreateFileRule 返回字段**：T3/T4 测试用 `tk.TaskCode`/`tpl.TemplateCode`/`st.StageCode`——实现者先读 `internal/repository/template_authoring_repo.go` 确认字段真名，按实调整测试（其余逻辑不变）。
- **start-task 非纯代理**：先调 manage 置状态成功后才本地建文件；manage 拒绝（非参与人）则不建文件。
- **严禁删改扫描文件**：脚手架只在 CPA 工作空间建空文件、幂等跳过已存在，不碰扫描文件。
- **migration 顺序**：assignee_viewed_at 加在 task 表创建之后（与 P2 同处尾部）。
- **整合测试**：T6 下线「我的工作事项」后，原断言它存在的用例必须改，否则红。
