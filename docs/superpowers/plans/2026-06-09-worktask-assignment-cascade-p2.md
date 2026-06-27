# 三级分工级联 P2「文件任务指派」实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 工作环节负责人在「文件任务指派」页为本环节下每个文件任务指派工作参与人，并给该导航加 Tier-2 未读角标。

**Architecture:** 跨两仓库。manage 给 `centralized_project_task_assignments` 加 status 等列、给 `centralized_project_stage_assignments` 加 `assignee_viewed_at`，新增 4 个 repo 方法 + 4 个接口。scan 加 4 代理 + 新视图 FileTaskAssignView + 新导航/角标/路由。指派状态归 manage。

**Tech Stack:** manage: Nitro `defineEventHandler` + better-sqlite3 repo + vitest(in-memory)。scan: Gin proxy(复用 P1 `getManageEndpoint`/`proxyToManage`) + Go test + Vue3/Vuetify + vitest。

**约束：** scan 用 yarn；前端测试前 `npm rebuild better-sqlite3`；manage 用 `npx vitest run`。manage 信封 `{code,message,data}`，scan 信封 `{success,data,error}`。

---

## 文件结构
**manage**
- Modify `server/database/index.ts`：集中立项迁移块加 4 列。
- Modify `server/database/centralized-project-repository.ts`：4 个方法。
- Create `server/api/centralized-projects/stage-tasks.get.ts`、`assign-tasks.post.ts`、`stage-unread-count.get.ts`、`mark-stages-seen.post.ts`。
- Test `tests/centralized-task-assign.test.ts`。

**scan**
- Modify `internal/httpd/centralized_projects.go`：4 代理 + 路由。
- Test `internal/httpd/centralized_projects_p2_test.go`。
- Create `frontend_real/views/FileTaskAssignView.vue` + `frontend_real/__tests__/FileTaskAssignView.test.ts`。
- Modify `frontend_real/plugins/router.ts`（路由）、`frontend_real/App.vue`（导航+角标）。

---

## Task 1: manage — 迁移 + 4 个 repo 方法

**Files:**
- Modify: `server/database/index.ts`、`server/database/centralized-project-repository.ts`
- Test: `tests/centralized-task-assign.test.ts`

- [ ] **Step 1: 写失败测试** — 新建 `tests/centralized-task-assign.test.ts`：

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
  id INTEGER PRIMARY KEY AUTOINCREMENT, scan_origin_id INTEGER NOT NULL, scan_endpoint VARCHAR(255),
  project_name VARCHAR(500) NOT NULL, owner_name VARCHAR(200) NOT NULL, status VARCHAR(40) NOT NULL DEFAULT 'pending',
  template_id INTEGER, template_code VARCHAR(100), template_version VARCHAR(40),
  sensitivity_level VARCHAR(40) NOT NULL DEFAULT 'general', owner_viewed_at DATETIME,
  data_owner VARCHAR(300), submitted_by VARCHAR(200), reject_reason TEXT, reviewed_by VARCHAR(200), reviewed_at DATETIME,
  accepted_by VARCHAR(200), accepted_at DATETIME, closed_by VARCHAR(200), closed_at DATETIME, closure_summary TEXT,
  create_time DATETIME DEFAULT CURRENT_TIMESTAMP, update_time DATETIME DEFAULT CURRENT_TIMESTAMP, disabled INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS centralized_project_stage_assignments (
  id INTEGER PRIMARY KEY AUTOINCREMENT, application_id INTEGER NOT NULL, stage_code VARCHAR(100) NOT NULL,
  stage_name VARCHAR(200) NOT NULL, assignee_username VARCHAR(200) NOT NULL, sort_order INTEGER DEFAULT 0,
  status VARCHAR(40) NOT NULL DEFAULT 'pending', started_at DATETIME, completed_at DATETIME, assignee_viewed_at DATETIME,
  create_time DATETIME DEFAULT CURRENT_TIMESTAMP, update_time DATETIME DEFAULT CURRENT_TIMESTAMP, disabled INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS centralized_project_task_assignments (
  id INTEGER PRIMARY KEY AUTOINCREMENT, application_id INTEGER NOT NULL, stage_code VARCHAR(100) NOT NULL,
  task_code VARCHAR(100) NOT NULL, task_name VARCHAR(200) NOT NULL, assignee_username VARCHAR(200) NOT NULL,
  sort_order INTEGER DEFAULT 0, status VARCHAR(40) NOT NULL DEFAULT 'pending', started_at DATETIME, completed_at DATETIME,
  create_time DATETIME DEFAULT CURRENT_TIMESTAMP, disabled INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS template_stages (
  id INTEGER PRIMARY KEY AUTOINCREMENT, template_id INTEGER NOT NULL, stage_code VARCHAR(64) NOT NULL,
  stage_name VARCHAR(128) NOT NULL, sort_order INTEGER DEFAULT 0, disabled INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS template_tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT, template_stage_id INTEGER NOT NULL, task_code VARCHAR(64) NOT NULL,
  task_name VARCHAR(200) NOT NULL, sort_order INTEGER DEFAULT 0, disabled INTEGER DEFAULT 0
);`

function seedUser(db: any, u: string) {
  db.prepare(`INSERT INTO auth_users (username, display_name, password_hash, user_unit, user_department, phone, role, status, disabled)
     VALUES (?, ?, 'x', 'U', 'D', '1', 'user', 'active', 0)`).run(u, u)
}

describe('文件任务指派', () => {
  let repo: any
  beforeEach(async () => {
    h.db = new Database(':memory:'); h.db.exec(DB_SQL); h.db.exec(DDL)
    seedUser(h.db, 'lead'); seedUser(h.db, 'u1'); seedUser(h.db, 'u2')
    // 模版 9：环节 STG-1 下两个任务
    const sid = h.db.prepare(`INSERT INTO template_stages (template_id, stage_code, stage_name, sort_order) VALUES (9,'STG-1','收稿',0)`).run().lastInsertRowid
    h.db.prepare(`INSERT INTO template_tasks (template_stage_id, task_code, task_name, sort_order) VALUES (?,?,?,?)`).run(sid, 'TK-1', '录入', 0)
    h.db.prepare(`INSERT INTO template_tasks (template_stage_id, task_code, task_name, sort_order) VALUES (?,?,?,?)`).run(sid, 'TK-2', '校对', 1)
    // 项目 1（template_id=9），STG-1 环节负责人 lead
    h.db.prepare(`INSERT INTO centralized_project_applications (scan_origin_id, project_name, owner_name, status, template_id) VALUES (1,'P','lead','accepted',9)`).run()
    h.db.prepare(`INSERT INTO centralized_project_stage_assignments (application_id, stage_code, stage_name, assignee_username) VALUES (1,'STG-1','收稿','lead')`).run()
    ;({ centralizedProjectRepository: repo } = await import('../server/database/centralized-project-repository'))
  })
  afterEach(() => { h.db.close(); vi.resetModules() })

  it('stageTasksForAssignment 列出任务 + 回填参与人', () => {
    repo.assignTasks(1, 'STG-1', 'lead', [{ task_code: 'TK-1', task_name: '录入', assignee_username: 'u1' }])
    const list = repo.stageTasksForAssignment(1, 'STG-1')
    expect(list.length).toBe(2)
    expect(list[0].task_code).toBe('TK-1')
    expect(list[0].assignee_username).toBe('u1')
    expect(list[1].task_code).toBe('TK-2')
    expect(list[1].assignee_username).toBeNull()
  })

  it('assignTasks 仅环节负责人可分工；整体替换；status=pending', () => {
    expect(() => repo.assignTasks(1, 'STG-1', 'u2', [{ task_code: 'TK-1', task_name: '录入', assignee_username: 'u1' }])).toThrow()
    repo.assignTasks(1, 'STG-1', 'lead', [
      { task_code: 'TK-1', task_name: '录入', assignee_username: 'u1' },
      { task_code: 'TK-2', task_name: '校对', assignee_username: 'u2' },
    ])
    const rows = h.db.prepare(`SELECT * FROM centralized_project_task_assignments WHERE application_id=1 AND disabled=0 ORDER BY sort_order`).all()
    expect(rows.length).toBe(2)
    expect(rows[0].status).toBe('pending')
    // 再次分工整体替换
    repo.assignTasks(1, 'STG-1', 'lead', [{ task_code: 'TK-1', task_name: '录入', assignee_username: 'u2' }])
    const rows2 = h.db.prepare(`SELECT * FROM centralized_project_task_assignments WHERE application_id=1 AND disabled=0`).all()
    expect(rows2.length).toBe(1)
    expect(rows2[0].assignee_username).toBe('u2')
  })

  it('stage 未读：仅本人、置已读归零', () => {
    h.db.prepare(`INSERT INTO centralized_project_stage_assignments (application_id, stage_code, stage_name, assignee_username) VALUES (1,'STG-2','排版','lead')`).run()
    expect(repo.stageUnreadCountForAssignee('lead')).toBe(2) // STG-1 + STG-2 都未读
    const n = repo.markStagesSeenForAssignee('lead')
    expect(n).toBe(2)
    expect(repo.stageUnreadCountForAssignee('lead')).toBe(0)
  })
})
```

- [ ] **Step 2: 运行确认失败**
Run: `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-task-assign.test.ts`
Expected: FAIL（`repo.assignTasks` 未定义）

- [ ] **Step 3: 加迁移列** — 在 `server/database/index.ts` 集中立项 schema 函数里、stage/task 指派表创建之后（紧跟 P1 的 `owner_viewed_at` 那行附近，确保这些表已存在），加：
```ts
  // 2026-06-09 P2 文件任务指派
  addColumnIfMissing(database, 'centralized_project_stage_assignments', 'assignee_viewed_at', 'DATETIME')
  addColumnIfMissing(database, 'centralized_project_task_assignments', 'status', "VARCHAR(40) NOT NULL DEFAULT 'pending'")
  addColumnIfMissing(database, 'centralized_project_task_assignments', 'started_at', 'DATETIME')
  addColumnIfMissing(database, 'centralized_project_task_assignments', 'completed_at', 'DATETIME')
```

- [ ] **Step 4: 实现 4 个方法** — 在 `centralized-project-repository.ts` 的 `markSeenForOwner` 方法之后加：
```ts
  /** 某环节下的文件任务（取自模版）+ 回填已有参与人。 */
  stageTasksForAssignment(applicationId: number, stageCode: string): Array<{ task_code: string; task_name: string; assignee_username: string | null }> {
    const db = getDatabase()
    const app = this.findById(applicationId)
    if (!app || !(app as any).template_id) return []
    const stage = db.prepare(`SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ? AND disabled = 0`)
      .get((app as any).template_id, stageCode) as { id: number } | undefined
    if (!stage) return []
    const tasks = db.prepare(`SELECT task_code, task_name FROM template_tasks WHERE template_stage_id = ? AND disabled = 0 ORDER BY sort_order, id`)
      .all(stage.id) as Array<{ task_code: string; task_name: string }>
    const assigns = db.prepare(`SELECT task_code, assignee_username FROM centralized_project_task_assignments WHERE application_id = ? AND stage_code = ? AND disabled = 0`)
      .all(applicationId, stageCode) as Array<{ task_code: string; assignee_username: string }>
    const map = new Map(assigns.map(a => [a.task_code, a.assignee_username]))
    return tasks.map(t => ({ task_code: t.task_code, task_name: t.task_name, assignee_username: map.get(t.task_code) ?? null }))
  },

  /** 环节负责人为本环节文件任务指派参与人（整体替换，status=pending）。 */
  assignTasks(applicationId: number, stageCode: string, actor: string, assignments: Array<{ task_code: string; task_name: string; assignee_username: string }>): { stage_code: string; count: number } {
    const db = getDatabase()
    const own = db.prepare(`SELECT id FROM centralized_project_stage_assignments WHERE application_id = ? AND stage_code = ? AND assignee_username = ? AND disabled = 0`)
      .get(applicationId, stageCode, (actor || '').trim())
    if (!own) throw new Error('只有该环节负责人可以分工')
    for (const a of assignments) { if (a.assignee_username) assertOwnerRegistered(a.assignee_username) }
    const tx = db.transaction(() => {
      db.prepare(`UPDATE centralized_project_task_assignments SET disabled = 1 WHERE application_id = ? AND stage_code = ?`).run(applicationId, stageCode)
      const ins = db.prepare(`INSERT INTO centralized_project_task_assignments
        (application_id, stage_code, task_code, task_name, assignee_username, sort_order, status)
        VALUES (?, ?, ?, ?, ?, ?, 'pending')`)
      assignments.forEach((a, i) => { if (a.assignee_username) ins.run(applicationId, stageCode, a.task_code, a.task_name, a.assignee_username, i) })
    })
    tx()
    return { stage_code: stageCode, count: assignments.filter(a => a.assignee_username).length }
  },

  /** 环节负责人未读环节数。 */
  stageUnreadCountForAssignee(assignee: string): number {
    const db = getDatabase()
    const row = db.prepare(`SELECT COUNT(*) AS c FROM centralized_project_stage_assignments
        WHERE assignee_username = ? AND assignee_viewed_at IS NULL AND disabled = 0`).get((assignee || '').trim()) as { c: number }
    return row?.c ?? 0
  },

  /** 标记本人所有未读环节为已读。 */
  markStagesSeenForAssignee(assignee: string): number {
    const db = getDatabase()
    const res = db.prepare(`UPDATE centralized_project_stage_assignments
        SET assignee_viewed_at = CURRENT_TIMESTAMP
        WHERE assignee_username = ? AND assignee_viewed_at IS NULL AND disabled = 0`).run((assignee || '').trim())
    return res.changes
  },
```

- [ ] **Step 5: 运行确认通过**
Run: `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-task-assign.test.ts`
Expected: PASS（3 个用例）

- [ ] **Step 6: 提交**
```bash
cd /root/data/projects/data-asset-manage
git add server/database/index.ts server/database/centralized-project-repository.ts tests/centralized-task-assign.test.ts
git commit -m "feat(manage): 文件任务指派 repo（task status 列 + stageTasks/assignTasks/未读）"
```

---

## Task 2: manage — 4 个接口

**Files:** Create 4 endpoints under `server/api/centralized-projects/`。

- [ ] **Step 1: stage-tasks.get.ts**
```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'

/** GET /api/centralized-projects/stage-tasks?application_id=&stage_code= → 任务列表(带回填参与人) */
export default defineEventHandler((event) => {
  try {
    const q = getQuery(event)
    const appId = Number(q.application_id)
    const stageCode = String(q.stage_code || '').trim()
    if (!appId || !stageCode) return { code: 400, message: '缺少 application_id / stage_code', data: null }
    return { code: 0, message: 'success', data: centralizedProjectRepository.stageTasksForAssignment(appId, stageCode) }
  } catch (e: any) {
    return { code: 500, message: e.message || '查询失败', data: null }
  }
})
```

- [ ] **Step 2: assign-tasks.post.ts**
```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'

/** POST /api/centralized-projects/assign-tasks?application_id=&stage_code= body:{actor, assignments} */
export default defineEventHandler(async (event) => {
  try {
    const q = getQuery(event)
    const appId = Number(q.application_id)
    const stageCode = String(q.stage_code || '').trim()
    if (!appId || !stageCode) return { code: 400, message: '缺少 application_id / stage_code', data: null }
    const body = await readBody<{ actor: string; assignments: Array<{ task_code: string; task_name: string; assignee_username: string }> }>(event)
    const actor = (body?.actor || '').trim()
    if (!actor) return { code: 400, message: 'actor 必填', data: null }
    const data = centralizedProjectRepository.assignTasks(appId, stageCode, actor, body?.assignments || [])
    return { code: 0, message: 'success', data }
  } catch (e: any) {
    return { code: 400, message: e.message || '分工失败', data: null }
  }
})
```

- [ ] **Step 3: stage-unread-count.get.ts**
```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'

/** GET /api/centralized-projects/stage-unread-count?assignee= → { count } */
export default defineEventHandler((event) => {
  try {
    const assignee = String(getQuery(event).assignee || '').trim()
    if (!assignee) return { code: 400, message: 'assignee 必填', data: null }
    return { code: 0, message: 'success', data: { count: centralizedProjectRepository.stageUnreadCountForAssignee(assignee) } }
  } catch (e: any) {
    return { code: 500, message: e.message || '查询失败', data: null }
  }
})
```

- [ ] **Step 4: mark-stages-seen.post.ts**
```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'

/** POST /api/centralized-projects/mark-stages-seen body:{assignee} → { updated } */
export default defineEventHandler(async (event) => {
  try {
    const body = await readBody<{ assignee: string }>(event)
    const assignee = String(body?.assignee || '').trim()
    if (!assignee) return { code: 400, message: 'assignee 必填', data: null }
    return { code: 0, message: 'success', data: { updated: centralizedProjectRepository.markStagesSeenForAssignee(assignee) } }
  } catch (e: any) {
    return { code: 500, message: e.message || '标记失败', data: null }
  }
})
```

- [ ] **Step 5: 验证（编译 + 既有测试不回归）**
Run: `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-task-assign.test.ts && npx nuxi typecheck 2>&1 | tail -15`
Expected: 测试 PASS；typecheck 无新增错误（若 `nuxi typecheck` 在本环境不可用/超时，跳过它，靠 4 个文件的语法自检 + 既有测试不回归）。

- [ ] **Step 6: 提交**
```bash
cd /root/data/projects/data-asset-manage
git add server/api/centralized-projects/stage-tasks.get.ts server/api/centralized-projects/assign-tasks.post.ts server/api/centralized-projects/stage-unread-count.get.ts server/api/centralized-projects/mark-stages-seen.post.ts
git commit -m "feat(manage): 文件任务指派 4 接口（stage-tasks/assign-tasks/stage 未读）"
```

---

## Task 3: scan — 4 个代理接口

**Files:**
- Modify: `internal/httpd/centralized_projects.go`
- Test: `internal/httpd/centralized_projects_p2_test.go`

- [ ] **Step 1: 写失败测试** — 新建 `internal/httpd/centralized_projects_p2_test.go`：
```go
package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_CentralizedProjects_P2Proxies(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	var stageTasksQuery, assignBody, stageUnreadAssignee, markStagesBody string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "stage-tasks"):
			stageTasksQuery = req.URL.RawQuery
		case strings.Contains(req.URL.Path, "assign-tasks"):
			b, _ := io.ReadAll(req.Body)
			assignBody = string(b)
		case strings.Contains(req.URL.Path, "stage-unread-count"):
			stageUnreadAssignee = req.URL.Query().Get("assignee")
		case strings.Contains(req.URL.Path, "mark-stages-seen"):
			b, _ := io.ReadAll(req.Body)
			markStagesBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"count":2,"updated":1}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/stage-tasks?application_id=1&stage_code=STG-1")
	successOk(t, status, resp)
	if !strings.Contains(stageTasksQuery, "application_id=1") || !strings.Contains(stageTasksQuery, "stage_code=STG-1") {
		t.Errorf("stage-tasks 应透传 application_id/stage_code，实得 %q", stageTasksQuery)
	}

	status, resp = jsonReq(t, r, "POST", "/centralized-projects/assign-tasks?application_id=1&stage_code=STG-1", map[string]interface{}{
		"assignments": []map[string]interface{}{{"task_code": "TK-1", "task_name": "录入", "assignee_username": "u1"}},
	})
	successOk(t, status, resp)
	if !strings.Contains(assignBody, "\"actor\":\"lead\"") || !strings.Contains(assignBody, "TK-1") {
		t.Errorf("assign-tasks 应注入 actor=lead 并带 assignments，实得 %s", assignBody)
	}

	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/stage-unread-count")
	successOk(t, status, resp)
	if stageUnreadAssignee != "lead" {
		t.Errorf("stage-unread-count 应以 assignee=lead 转发，实得 %q", stageUnreadAssignee)
	}

	status, resp = jsonReq(t, r, "POST", "/centralized-projects/mark-stages-seen", map[string]interface{}{})
	successOk(t, status, resp)
	if !strings.Contains(markStagesBody, "\"assignee\":\"lead\"") {
		t.Errorf("mark-stages-seen 应在 body 注入 assignee=lead，实得 %s", markStagesBody)
	}
}
```

- [ ] **Step 2: 运行确认失败**
Run: `cd /root/data/projects/data-asset-scan && go test ./internal/httpd/ -run TestHTTP_CentralizedProjects_P2Proxies -v`
Expected: FAIL（路由 404）

- [ ] **Step 3: 注册路由** — 在 `RegisterCentralizedProjectsRoutes` 中加：
```go
	r.GET("/stage-tasks", StageTasksProxy)
	r.POST("/assign-tasks", AssignTasksProxy)
	r.GET("/stage-unread-count", StageUnreadCountProxy)
	r.POST("/mark-stages-seen", MarkStagesSeenProxy)
```

- [ ] **Step 4: 实现 handler** — 在 `centralized_projects.go` 末尾加（复用 P1 的 `getManageEndpoint`/`proxyToManage`/`currentOperator`，import 已齐）：
```go
// StageTasksProxy GET /centralized-projects/stage-tasks?application_id=&stage_code=
func StageTasksProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	url := fmt.Sprintf("%s/api/centralized-projects/stage-tasks?application_id=%s&stage_code=%s", ep, c.Query("application_id"), c.Query("stage_code"))
	proxyToManage(c, "GET", url, nil)
}

// AssignTasksProxy POST /centralized-projects/assign-tasks?application_id=&stage_code= —— 注入 actor=operator。
func AssignTasksProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	if body == nil {
		body = map[string]any{}
	}
	body["actor"] = currentOperator(c)
	b, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/centralized-projects/assign-tasks?application_id=%s&stage_code=%s", ep, c.Query("application_id"), c.Query("stage_code"))
	proxyToManage(c, "POST", url, b)
}

// StageUnreadCountProxy GET /centralized-projects/stage-unread-count —— assignee=operator。
func StageUnreadCountProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/stage-unread-count?assignee=%s", ep, currentOperator(c)), nil)
}

// MarkStagesSeenProxy POST /centralized-projects/mark-stages-seen —— assignee=operator（body）。
func MarkStagesSeenProxy(c *gin.Context) {
	ep, err := getManageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	b, _ := json.Marshal(map[string]any{"assignee": currentOperator(c)})
	proxyToManage(c, "POST", ep+"/api/centralized-projects/mark-stages-seen", b)
}
```

- [ ] **Step 5: 运行确认通过**
Run: `cd /root/data/projects/data-asset-scan && go test ./internal/httpd/ -run 'CentralizedProjects' -v`
Expected: PASS（新 P2 代理 + 既有不回归）

- [ ] **Step 6: 提交**
```bash
cd /root/data/projects/data-asset-scan
git add internal/httpd/centralized_projects.go internal/httpd/centralized_projects_p2_test.go
git commit -m "feat(scan): 代理 stage-tasks/assign-tasks/stage 未读 到 manage"
```

---

## Task 4: scan — FileTaskAssignView 视图

**Files:**
- Create: `frontend_real/views/FileTaskAssignView.vue`
- Test: `frontend_real/__tests__/FileTaskAssignView.test.ts`

- [ ] **Step 1: 写失败测试** — 新建 `frontend_real/__tests__/FileTaskAssignView.test.ts`：
```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import FileTaskAssignView from '../views/FileTaskAssignView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })
function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}
function mountView() { return mount(FileTaskAssignView, { global: { plugins: [vuetify] } }) }

describe('文件任务指派', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('列出我的工作环节', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('收稿')
    expect(wrapper.text()).toContain('甲项目')
  })

  it('分工弹窗列任务、提交调 assign-tasks 带 assignments', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      if (url.includes('/stage-tasks')) return ok([
        { task_code: 'TK-1', task_name: '录入', assignee_username: null },
        { task_code: 'TK-2', task_name: '校对', assignee_username: 'u1' },
      ])
      if (url.includes('/assign-tasks') && init?.method === 'POST') { posted.push({ url, body: JSON.parse(init.body as string) }); return ok({ count: 1 }) }
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAssign(vm.items[0])
    await flushPromises()
    expect(vm.assignDialog.tasks.length).toBe(2)
    expect(vm.assignDialog.tasks[1].assignee_username).toBe('u1') // 回填
    vm.assignDialog.tasks[0].assignee_username = 'u1'
    await vm.submitAssign()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].url).toContain('application_id=7')
    expect(posted[0].url).toContain('stage_code=STG-1')
    expect(posted[0].body.assignments.length).toBe(2)
  })
})
```

- [ ] **Step 2: 运行确认失败**
Run: `cd /root/data/projects/data-asset-scan && npm rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/FileTaskAssignView.test.ts`
Expected: FAIL（找不到 FileTaskAssignView）

- [ ] **Step 3: 实现视图** — 新建 `frontend_real/views/FileTaskAssignView.vue`：
```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { API_BASE } from '@/services/api'

interface MyStage {
  id: number
  application_id: number
  stage_code: string
  stage_name: string
  assignee_username: string
  status: string
  project_name: string
}
interface TaskRow { task_code: string; task_name: string; assignee_username: string | null }
interface OwnerOption { username: string; label: string }

const loading = ref(false)
const items = ref<MyStage[]>([])
const snackbar = ref({ show: false, text: '', color: 'success' })
const ownerOptions = ref<OwnerOption[]>([])

const assignDialog = ref({
  open: false, busy: false, applicationId: 0, stageCode: '', stageName: '', projectName: '',
  loading: false, tasks: [] as TaskRow[],
})

async function loadOwnerOptions() {
  try {
    const r = await fetch(`${API_BASE}/manage-users`)
    const j = await r.json()
    if (j.success && Array.isArray(j.data)) {
      ownerOptions.value = j.data.filter((u: any) => u.role !== 'system_admin')
        .map((u: any) => ({ username: u.username, label: `${u.display_name || u.username} (${u.username})` }))
    }
  } catch {}
}

async function load() {
  loading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/my-stages`)
    const j = await r.json()
    if (j.success) items.value = (j.data || []) as MyStage[]
  } finally { loading.value = false }
}

async function openAssign(it: MyStage) {
  assignDialog.value = { open: true, busy: false, applicationId: it.application_id, stageCode: it.stage_code, stageName: it.stage_name, projectName: it.project_name, loading: true, tasks: [] }
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/stage-tasks?application_id=${it.application_id}&stage_code=${encodeURIComponent(it.stage_code)}`)
    const j = await r.json()
    assignDialog.value.tasks = j.success ? (j.data || []) as TaskRow[] : []
    if (assignDialog.value.tasks.length === 0) snackbar.value = { show: true, text: '该环节暂无文件任务', color: 'warning' }
  } finally { assignDialog.value.loading = false }
}

async function submitAssign() {
  const d = assignDialog.value
  d.busy = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/assign-tasks?application_id=${d.applicationId}&stage_code=${encodeURIComponent(d.stageCode)}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ assignments: d.tasks.map(t => ({ task_code: t.task_code, task_name: t.task_name, assignee_username: t.assignee_username || '' })) }),
    })
    const j = await r.json()
    if (j.success) { d.open = false; snackbar.value = { show: true, text: '已提交分工', color: 'success' }; await load() }
    else snackbar.value = { show: true, text: '分工失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '分工失败：' + (e?.message || String(e)), color: 'error' }
  } finally { d.busy = false }
}

function statusLabel(s: string): string {
  return ({ pending: '待开始', in_progress: '进行中', completed: '已完成' } as Record<string, string>)[s] || s
}

const headers = [
  { title: '项目', key: 'project_name' },
  { title: '工作环节', key: 'stage_name' },
  { title: '状态', key: 'status', width: '110px' },
  { title: '操作', key: 'actions', width: '120px', sortable: false },
]

onMounted(async () => { await loadOwnerOptions(); await load() })
</script>

<template>
  <div>
    <v-card elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-account-multiple-plus</v-icon>
        文件任务指派
        <v-spacer />
        <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
      </v-card-title>
      <v-data-table :headers="headers" :items="items" :loading="loading" item-value="id" :items-per-page="50" hide-default-footer>
        <template #item.status="{ item }">
          <v-chip size="x-small" variant="tonal">{{ statusLabel(item.status) }}</v-chip>
        </template>
        <template #item.actions="{ item }">
          <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-account-multiple-plus" @click="openAssign(item)">分工</v-btn>
        </template>
        <template #no-data>
          <div class="text-center py-8 text-grey">暂无指派给我的工作环节</div>
        </template>
      </v-data-table>
    </v-card>

    <v-dialog v-model="assignDialog.open" max-width="640" persistent scrollable>
      <v-card>
        <v-card-title>文件任务分工 · {{ assignDialog.stageName }}</v-card-title>
        <v-card-subtitle>{{ assignDialog.projectName }}</v-card-subtitle>
        <v-card-text style="max-height:60vh;">
          <v-progress-linear v-if="assignDialog.loading" indeterminate class="mb-3" />
          <div v-else-if="assignDialog.tasks.length === 0" class="text-grey py-4 text-center">该环节暂无文件任务</div>
          <v-row v-for="t in assignDialog.tasks" :key="t.task_code" class="align-center">
            <v-col cols="5"><div class="text-body-2">{{ t.task_name }}</div><div class="text-caption text-grey">{{ t.task_code }}</div></v-col>
            <v-col cols="7">
              <v-autocomplete v-model="t.assignee_username" :items="ownerOptions" item-title="label" item-value="username"
                label="工作参与人" variant="outlined" density="compact" clearable hide-details="auto" :disabled="assignDialog.busy" />
            </v-col>
          </v-row>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="assignDialog.busy" @click="assignDialog.open = false">取消</v-btn>
          <v-btn color="primary" variant="flat" :loading="assignDialog.busy" :disabled="assignDialog.tasks.length === 0" @click="submitAssign">提交</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </div>
</template>
```

- [ ] **Step 4: 运行确认通过**
Run: `cd /root/data/projects/data-asset-scan && npm rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/FileTaskAssignView.test.ts`
Expected: PASS（2 用例）

- [ ] **Step 5: 提交**
```bash
cd /root/data/projects/data-asset-scan
git add frontend_real/views/FileTaskAssignView.vue frontend_real/__tests__/FileTaskAssignView.test.ts
git commit -m "feat(scan): 文件任务指派视图 FileTaskAssignView"
```

---

## Task 5: scan — 路由 + 导航 + Tier-2 角标

**Files:**
- Modify: `frontend_real/plugins/router.ts`、`frontend_real/App.vue`

- [ ] **Step 1: 加路由** — 在 `plugins/router.ts` 的 routes 数组中（仿照 `/project-acceptance` 那条）加：
```ts
  {
    path: '/file-task-assign',
    name: 'FileTaskAssign',
    component: () => import('../views/FileTaskAssignView.vue'),
  },
```
（与文件里现有路由的写法保持一致——若现有用的是同步 import 或带 meta，则照其风格。）

- [ ] **Step 2: 加导航项** — 在 `App.vue` `navItems`「数据业务服务」组里，「工作事项分工」那项之后加：
```ts
      { title: '文件任务指派', icon: 'mdi-account-multiple-plus', to: '/file-task-assign', badge: 'filetask', hint: '作为工作环节负责人：为本环节每个文件任务指派参与人' },
```

- [ ] **Step 3: 角标脚本** — 在 `App.vue` `<script setup>` 加（仿照 P1 的 worktask 角标）：
```ts
const filetaskUnread = ref(0)

async function loadFiletaskUnread() {
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/stage-unread-count`)
    const j = await r.json()
    if (j.success) filetaskUnread.value = Number(j.data?.count) || 0
  } catch { /* 静默 */ }
}

async function clearFiletaskUnread() {
  try {
    await fetch(`${API_BASE}/centralized-projects/mark-stages-seen`, { method: 'POST' })
    filetaskUnread.value = 0
  } catch { /* 保留角标 */ }
}
```
在 `onMounted` 的非 shellless 分支里加 `loadFiletaskUnread()`；在已有的 `watch(() => route.path, ...)` 里扩展（保留 worktask 分支）：
```ts
watch(() => route.path, (p, old) => {
  if (isShelllessPage.value) return
  if (p === '/project-acceptance') clearWorktaskUnread()
  else if (old === '/project-acceptance') loadWorktaskUnread()
  if (p === '/file-task-assign') clearFiletaskUnread()
  else if (old === '/file-task-assign') loadFiletaskUnread()
})
```

- [ ] **Step 4: 角标模板** — 在导航子项 `v-list-item` 的 `#append` 插槽里扩展条件（与 worktask 并存）。若现有是单一 `#append v-if="child.badge === 'worktask' ..."`，改为分别处理或通用化：
```vue
                  <template #append v-if="(child.badge === 'worktask' && worktaskUnread > 0) || (child.badge === 'filetask' && filetaskUnread > 0)">
                    <v-chip size="x-small" color="error" variant="flat">{{ child.badge === 'worktask' ? worktaskUnread : filetaskUnread }}</v-chip>
                  </template>
```
（以实际循环变量名 `child` 为准。）

- [ ] **Step 5: 验证构建**
Run: `cd /root/data/projects/data-asset-scan && yarn build 2>&1 | tail -6`
Then: `yarn vitest run frontend_real/__tests__/frontend-integration.test.ts 2>&1 | tail -10`
Expected: build 成功；整合测试通过（若整合测试断言了导航项集合且需补「文件任务指派」断言，按需补上使其通过）。

- [ ] **Step 6: 提交**
```bash
cd /root/data/projects/data-asset-scan
git add frontend_real/plugins/router.ts frontend_real/App.vue frontend_real/__tests__/frontend-integration.test.ts
git commit -m "feat(scan): 新增「文件任务指派」导航/路由 + Tier-2 未读角标"
```

---

## 自查（Self-Review）
- **Spec 覆盖**：迁移列(T1) / stageTasks+assignTasks+未读 repo(T1) / 4 接口(T2) / scan 代理(T3) / 视图(T4) / 导航路由角标(T5) —— 均有任务。
- **类型一致**：`assign-tasks` body `{actor, assignments:[{task_code,task_name,assignee_username}]}` 三处一致（manage 接口/repo/scan 代理注入 actor + 视图发 assignments）；`stage-unread-count` 回 `{count}`、`mark-stages-seen` 回 `{updated}`；`stage-tasks` 回 `[{task_code,task_name,assignee_username}]` 与视图 TaskRow 一致；my-stages 字段(application_id/stage_code/stage_name/project_name/status)与 MyStage 接口一致。
- **占位符**：无 TODO；每步给完整代码。

## 风险与注意
- **manage 迁移位置**：4 个 addColumnIfMissing 必须在 stage/task 指派表创建之后执行（与 P1 owner_viewed_at 同一函数尾部即可）。
- **既有 task 指派行**：P1 accept 可能写过"仅记录"的 task 指派（无 status），加 `status DEFAULT 'pending'` 后这些行 status=pending，符合 P2 语义。
- **my-stages 复用**：scan 已有 `/centralized-projects/my-stages` 代理（P1 前就有），视图直接用,本计划不改它。
- **测试前置**：scan 前端 `npm rebuild better-sqlite3`；manage `npx vitest`。
- **整合测试**：T5 改导航后,若 `frontend-integration.test.ts` 的导航断言需同步,按实际补「文件任务指派」断言。
