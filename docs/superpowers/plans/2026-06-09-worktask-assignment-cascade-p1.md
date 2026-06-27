# 三级分工级联 P1「工作事项分工」实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把项目负责人的承接流程拆成「选模版(确定) → 分工(派环节负责人,提交)」两步，并给「工作事项分工」导航加未读红点。

**Architecture:** 跨两仓库。manage(data-asset-manage, Nuxt/better-sqlite3) 加 `set-template` 接口与未读列/接口；scan(data-asset-scan, Go/Gin + Vue/Vuetify) 加三个代理接口、导航改名+角标、重写 ProjectAcceptanceView 为两步流程。指派状态仍归 manage。

**Tech Stack:** manage: Nitro `defineEventHandler` + better-sqlite3 repository + vitest(in-memory DB)。scan: Gin proxy + Go test(httptest mock manage) + Vue3/Vuetify + vitest。

**约束（CLAUDE.md）：** 每个任务用例通过再下一个；scan 用 yarn；严禁删改扫描文件代码；scan 前端测试前 `npm rebuild better-sqlite3`（注：better-sqlite3 属后端/测试依赖）。manage 仓库无此 yarn 限制，用其既有脚本。

**响应信封：** manage 接口统一 `{ code, message, data }`（code=0 成功）；scan 接口统一 `{ success, data, error }`。

---

## 文件结构

**manage（data-asset-manage）**
- 修改 `server/database/centralized-project-repository.ts`：interface 加 `owner_viewed_at`；新增 `setTemplate` / `unreadCountForOwner` / `markSeenForOwner`。
- 修改 `server/database/index.ts`：在集中立项列迁移块（约 397 行 data_owner 之后）加 `owner_viewed_at` 列。
- 新增 `server/api/centralized-projects/set-template.post.ts`、`unread-count.get.ts`、`mark-seen.post.ts`。
- 新增测试 `tests/centralized-set-template-unread.test.ts`。

**scan（data-asset-scan）**
- 修改 `internal/httpd/centralized_projects.go`：3 个代理 handler + 路由。
- 新增测试 `internal/httpd/centralized_projects_p1_test.go`。
- 修改 `frontend_real/App.vue`：导航改名 + 未读角标。
- 修改 `frontend_real/views/ProjectAcceptanceView.vue`：两步流程。
- 修改/新增 `frontend_real/__tests__/ProjectAcceptanceView.test.ts`。

---

## Task 1: manage — set-template（写模版关联，保持 approved）

**Files:**
- Modify: `server/database/centralized-project-repository.ts`
- Create: `server/api/centralized-projects/set-template.post.ts`
- Test: `tests/centralized-set-template-unread.test.ts`

- [ ] **Step 1: 写失败测试**

新建 `tests/centralized-set-template-unread.test.ts`（DDL 含新列 `owner_viewed_at`，供后续任务复用）：

```ts
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import Database from 'better-sqlite3'
import { readFileSync } from 'fs'
import { join } from 'path'

const h = vi.hoisted(() => ({ db: null as any }))
vi.mock('../server/database/index', () => ({ getDatabase: () => h.db }))
const DB_SQL = readFileSync(join(__dirname, '../server/database/database.sql'), 'utf-8')

const CPA_DDL = `
CREATE TABLE IF NOT EXISTS centralized_project_applications (
  id INTEGER PRIMARY KEY AUTOINCREMENT, scan_origin_id INTEGER NOT NULL, scan_endpoint VARCHAR(255),
  project_name VARCHAR(500) NOT NULL, owner_name VARCHAR(200) NOT NULL, data_owner VARCHAR(300), submitted_by VARCHAR(200),
  status VARCHAR(40) NOT NULL DEFAULT 'pending', reject_reason TEXT, reviewed_by VARCHAR(200), reviewed_at DATETIME,
  accepted_by VARCHAR(200), accepted_at DATETIME, template_id INTEGER, template_code VARCHAR(100), template_version VARCHAR(40),
  closed_by VARCHAR(200), closed_at DATETIME, closure_summary TEXT, sensitivity_level VARCHAR(40) NOT NULL DEFAULT 'general',
  owner_viewed_at DATETIME,
  create_time DATETIME DEFAULT CURRENT_TIMESTAMP, update_time DATETIME DEFAULT CURRENT_TIMESTAMP, disabled INTEGER DEFAULT 0
);`

function seedUser(db: any, username: string, name: string) {
  db.prepare(`INSERT INTO auth_users (username, display_name, password_hash, user_unit, user_department, phone, role, status, disabled)
     VALUES (?, ?, 'x', '单位01', '部门01', '13800000000', 'user', 'active', 0)`).run(username, name)
}
function seedProject(db: any, owner: string, status = 'approved') {
  return db.prepare(`INSERT INTO centralized_project_applications (scan_origin_id, project_name, owner_name, status)
     VALUES (1, 'P', ?, ?)`).run(owner, status).lastInsertRowid as number
}

describe('set-template', () => {
  let repo: any
  beforeEach(async () => {
    h.db = new Database(':memory:'); h.db.exec(DB_SQL); h.db.exec(CPA_DDL)
    seedUser(h.db, 'lead', '负责人')
    ;({ centralizedProjectRepository: repo } = await import('../server/database/centralized-project-repository'))
  })
  afterEach(() => { h.db.close(); vi.resetModules() })

  it('写入模版字段且状态保持 approved', () => {
    const id = seedProject(h.db, 'lead')
    const row = repo.setTemplate(id, { acceptor: 'lead', template_id: 9, template_code: 'TPL-X', template_version: 'V1.0' })
    expect(row.template_code).toBe('TPL-X')
    expect(row.template_id).toBe(9)
    expect(row.status).toBe('approved')
  })

  it('非 approved 项目拒绝', () => {
    const id = seedProject(h.db, 'lead', 'accepted')
    expect(() => repo.setTemplate(id, { acceptor: 'lead', template_id: 9, template_code: 'TPL-X', template_version: 'V1' })).toThrow()
  })

  it('acceptor 非 owner 拒绝', () => {
    const id = seedProject(h.db, 'lead')
    expect(() => repo.setTemplate(id, { acceptor: 'other', template_id: 9, template_code: 'TPL-X', template_version: 'V1' })).toThrow()
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-set-template-unread.test.ts`
Expected: FAIL（`repo.setTemplate` 不是函数）

- [ ] **Step 3: interface 加列 + 实现 setTemplate**

在 `centralized-project-repository.ts` 的 `interface CentralizedProjectApplication` 中，`closure_summary` 之后加一行：

```ts
  owner_viewed_at: string | null
```

在 `centralizedProjectRepository` 对象里（紧跟 `accept(...)` 方法之后）加：

```ts
  /** 一级分工第一步：仅写模版关联，状态保持 approved（与 accept 解耦，可断点续做）。 */
  setTemplate(id: number, input: { acceptor: string; template_id: number; template_code: string; template_version: string }): CentralizedProjectApplication | null {
    const db = getDatabase()
    const cur = this.findById(id)
    if (!cur) return null
    if (cur.status !== 'approved') throw new Error('项目状态不允许重选模版')
    if (cur.owner_name !== input.acceptor) throw new Error('只有项目指定负责人可以操作此项目')
    if (!input.template_id || !input.template_code) throw new Error('必须选择一个模板')
    db.prepare(
      `UPDATE centralized_project_applications
          SET template_id = ?, template_code = ?, template_version = ?
        WHERE id = ?`
    ).run(input.template_id, input.template_code, (input.template_version || '').trim(), id)
    return this.findById(id)
  },
```

- [ ] **Step 4: 新增接口**

新建 `server/api/centralized-projects/set-template.post.ts`：

```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'

/**
 * POST /api/centralized-projects/set-template?id=
 * body: { acceptor, template_id, template_code, template_version }
 * 一级分工第一步：负责人选定模版（状态保持 approved）。
 */
export default defineEventHandler(async (event) => {
  try {
    const id = Number(getQuery(event).id)
    if (!id) return { code: 400, message: '缺少 id', data: null }
    const body = await readBody<{ acceptor: string; template_id: number; template_code: string; template_version: string }>(event)
    const acceptor = (body?.acceptor || '').trim()
    if (!acceptor) return { code: 400, message: 'acceptor 必填', data: null }
    const row = centralizedProjectRepository.setTemplate(id, {
      acceptor,
      template_id: Number(body?.template_id) || 0,
      template_code: (body?.template_code || '').trim(),
      template_version: (body?.template_version || '').trim(),
    })
    if (!row) return { code: 404, message: '申请不存在', data: null }
    return { code: 0, message: 'success', data: row }
  } catch (e: any) {
    return { code: 400, message: e.message || '设置模版失败', data: null }
  }
})
```

- [ ] **Step 5: 运行确认通过**

Run: `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-set-template-unread.test.ts`
Expected: PASS（3 个 set-template 用例）

- [ ] **Step 6: 提交**

```bash
cd /root/data/projects/data-asset-manage
git add server/database/centralized-project-repository.ts server/api/centralized-projects/set-template.post.ts tests/centralized-set-template-unread.test.ts
git commit -m "feat(manage): 集中立项 set-template 接口（选模版，保持 approved）"
```

---

## Task 2: manage — 未读列 + unread-count / mark-seen

**Files:**
- Modify: `server/database/index.ts`（迁移加列）
- Modify: `server/database/centralized-project-repository.ts`（两个方法）
- Create: `server/api/centralized-projects/unread-count.get.ts`、`mark-seen.post.ts`
- Test: `tests/centralized-set-template-unread.test.ts`（追加）

- [ ] **Step 1: 写失败测试（追加）**

向 `tests/centralized-set-template-unread.test.ts` 追加一个 describe：

```ts
describe('未读计数 / 标记已读', () => {
  let repo: any
  beforeEach(async () => {
    h.db = new Database(':memory:'); h.db.exec(DB_SQL); h.db.exec(CPA_DDL)
    seedUser(h.db, 'lead', '负责人'); seedUser(h.db, 'other', '别人')
    ;({ centralizedProjectRepository: repo } = await import('../server/database/centralized-project-repository'))
  })
  afterEach(() => { h.db.close(); vi.resetModules() })

  it('未读 = 我名下 approved 且 owner_viewed_at 为空', () => {
    seedProject(h.db, 'lead')        // 未读
    seedProject(h.db, 'lead')        // 未读
    seedProject(h.db, 'lead', 'accepted') // 非 approved，不计
    seedProject(h.db, 'other')       // 他人，不计
    expect(repo.unreadCountForOwner('lead')).toBe(2)
    expect(repo.unreadCountForOwner('other')).toBe(1)
  })

  it('mark-seen 把我的未读置已读，计数归零，不影响他人', () => {
    seedProject(h.db, 'lead'); seedProject(h.db, 'lead'); seedProject(h.db, 'other')
    const n = repo.markSeenForOwner('lead')
    expect(n).toBe(2)
    expect(repo.unreadCountForOwner('lead')).toBe(0)
    expect(repo.unreadCountForOwner('other')).toBe(1)
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-set-template-unread.test.ts`
Expected: FAIL（`unreadCountForOwner` 未定义）

- [ ] **Step 3: 加迁移列**

在 `server/database/index.ts` 集中立项列迁移块中，紧跟 `addColumnIfMissing(database, 'centralized_project_applications', 'data_owner', 'VARCHAR(300)')`（约 397 行）之后加：

```ts
  // 2026-06-09 三级分工 P1：负责人未读追踪（NULL=未读）
  addColumnIfMissing(database, 'centralized_project_applications', 'owner_viewed_at', 'DATETIME')
```

- [ ] **Step 4: 实现两个方法**

在 `centralized-project-repository.ts` 的 `setTemplate` 方法之后加：

```ts
  /** 负责人未读数：我名下、approved、owner_viewed_at 为空。 */
  unreadCountForOwner(owner: string): number {
    const db = getDatabase()
    const row = db.prepare(
      `SELECT COUNT(*) AS c FROM centralized_project_applications
        WHERE owner_name = ? AND status = 'approved' AND owner_viewed_at IS NULL AND disabled = 0`
    ).get((owner || '').trim()) as { c: number }
    return row?.c ?? 0
  },

  /** 标记本人所有未读为已读，返回更新条数。 */
  markSeenForOwner(owner: string): number {
    const db = getDatabase()
    const res = db.prepare(
      `UPDATE centralized_project_applications
          SET owner_viewed_at = CURRENT_TIMESTAMP
        WHERE owner_name = ? AND status = 'approved' AND owner_viewed_at IS NULL AND disabled = 0`
    ).run((owner || '').trim())
    return res.changes
  },
```

- [ ] **Step 5: 新增接口**

新建 `server/api/centralized-projects/unread-count.get.ts`：

```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'

/** GET /api/centralized-projects/unread-count?owner= → { count } */
export default defineEventHandler((event) => {
  const owner = String(getQuery(event).owner || '').trim()
  if (!owner) return { code: 400, message: 'owner 必填', data: null }
  return { code: 0, message: 'success', data: { count: centralizedProjectRepository.unreadCountForOwner(owner) } }
})
```

新建 `server/api/centralized-projects/mark-seen.post.ts`：

```ts
import { centralizedProjectRepository } from '../../database/centralized-project-repository'

/** POST /api/centralized-projects/mark-seen?owner= → { updated } */
export default defineEventHandler((event) => {
  const owner = String(getQuery(event).owner || '').trim()
  if (!owner) return { code: 400, message: 'owner 必填', data: null }
  return { code: 0, message: 'success', data: { updated: centralizedProjectRepository.markSeenForOwner(owner) } }
})
```

- [ ] **Step 6: 运行确认通过**

Run: `cd /root/data/projects/data-asset-manage && npx vitest run tests/centralized-set-template-unread.test.ts`
Expected: PASS（set-template + 未读 全部）

- [ ] **Step 7: 提交**

```bash
cd /root/data/projects/data-asset-manage
git add server/database/index.ts server/database/centralized-project-repository.ts server/api/centralized-projects/unread-count.get.ts server/api/centralized-projects/mark-seen.post.ts tests/centralized-set-template-unread.test.ts
git commit -m "feat(manage): 集中立项负责人未读列 + unread-count/mark-seen 接口"
```

---

## Task 3: scan — 3 个代理接口

**Files:**
- Modify: `internal/httpd/centralized_projects.go`（路由 + 3 handler）
- Test: `internal/httpd/centralized_projects_p1_test.go`（新建）

代理实现参考既有 `pushCentralizedProjectToManage`：从 `repository.NewSystemConfigRepository(...).GetValue(repository.KeyManageEndpoint)` 取 endpoint，用 `net/http` 转发，回包 `{success,data}`。

- [ ] **Step 1: 写失败测试**

新建 `internal/httpd/centralized_projects_p1_test.go`：

```go
package httpd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 三个代理接口：转发到 manage，注入 operator/owner。
func TestHTTP_CentralizedProjects_P1Proxies(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	var gotPaths []string
	var gotOwner, gotBody string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPaths = append(gotPaths, req.URL.Path)
		if q := req.URL.Query().Get("owner"); q != "" {
			gotOwner = q
		}
		if req.Method == "POST" && strings.Contains(req.URL.Path, "set-template") {
			buf := make([]byte, req.ContentLength)
			_, _ = req.Body.Read(buf)
			gotBody = string(buf)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"count":3,"updated":2,"id":1}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// unread-count
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/unread-count")
	successOk(t, status, resp)
	if dataMap(t, resp)["count"] == nil {
		t.Errorf("unread-count 应回 count: %+v", resp)
	}
	if gotOwner != "lead" {
		t.Errorf("应以 owner=lead 转发，实得 %q", gotOwner)
	}

	// mark-seen
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/mark-seen", map[string]interface{}{})
	successOk(t, status, resp)

	// set-template
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/set-template?id=1", map[string]interface{}{
		"template_id": 9, "template_code": "TPL-X", "template_version": "V1",
	})
	successOk(t, status, resp)
	if !strings.Contains(gotBody, "\"acceptor\":\"lead\"") {
		t.Errorf("set-template 应注入 acceptor=lead，body=%s", gotBody)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /root/data/projects/data-asset-scan && go test ./internal/httpd/ -run TestHTTP_CentralizedProjects_P1Proxies -v`
Expected: FAIL（路由 404）

- [ ] **Step 3: 注册路由**

在 `RegisterCentralizedProjectsRoutes` 中（`r.POST("", CreateCentralizedProject)` 一带）加：

```go
	r.POST("/set-template", SetTemplateProxy)
	r.GET("/unread-count", UnreadCountProxy)
	r.POST("/mark-seen", MarkSeenProxy)
```

- [ ] **Step 4: 实现 3 个 handler**

在 `centralized_projects.go` 末尾加（复用包内 `repository`、`currentOperator`、`gin`、`net/http`、`io`、`bytes`、`encoding/json`、`time`、`strings` 已 import）：

```go
// manageEndpoint 取已配置的 manage 地址（去尾斜杠）。
func manageEndpoint() (string, error) {
	cfg := repository.NewSystemConfigRepository(repository.GetDB())
	ep := strings.TrimRight(strings.TrimSpace(cfg.GetValue(repository.KeyManageEndpoint)), "/")
	if ep == "" {
		return "", fmt.Errorf("未配置 manage_endpoint")
	}
	return ep, nil
}

// proxyToManage 转发请求并把 manage 的 {code,message,data} 翻成 scan 的 {success,data,error}。
func proxyToManage(c *gin.Context, method, url string, body []byte) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, url, reqBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "解析 manage 响应失败"})
		return
	}
	if out.Code != 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": out.Message})
		return
	}
	var data any
	_ = json.Unmarshal(out.Data, &data)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// SetTemplateProxy POST /centralized-projects/set-template?id= —— 注入 acceptor=operator 转发 manage。
func SetTemplateProxy(c *gin.Context) {
	ep, err := manageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	if body == nil {
		body = map[string]any{}
	}
	body["acceptor"] = currentOperator(c)
	b, _ := json.Marshal(body)
	proxyToManage(c, "POST", fmt.Sprintf("%s/api/centralized-projects/set-template?id=%s", ep, c.Query("id")), b)
}

// UnreadCountProxy GET /centralized-projects/unread-count —— owner=operator。
func UnreadCountProxy(c *gin.Context) {
	ep, err := manageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "GET", fmt.Sprintf("%s/api/centralized-projects/unread-count?owner=%s", ep, currentOperator(c)), nil)
}

// MarkSeenProxy POST /centralized-projects/mark-seen —— owner=operator。
func MarkSeenProxy(c *gin.Context) {
	ep, err := manageEndpoint()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	proxyToManage(c, "POST", fmt.Sprintf("%s/api/centralized-projects/mark-seen?owner=%s", ep, currentOperator(c)), nil)
}
```

注：`currentOperator(c)` 用于 owner/acceptor，URL 里用户名一般为 ASCII；若担心特殊字符可后续加 `url.QueryEscape`，本期用户名为登录名（ASCII），从简。

- [ ] **Step 5: 运行确认通过**

Run: `cd /root/data/projects/data-asset-scan && go test ./internal/httpd/ -run 'CentralizedProjects' -v`
Expected: PASS（新代理 + 既有不回归）

- [ ] **Step 6: 提交**

```bash
cd /root/data/projects/data-asset-scan
git add internal/httpd/centralized_projects.go internal/httpd/centralized_projects_p1_test.go
git commit -m "feat(scan): 代理 set-template/unread-count/mark-seen 到 manage"
```

---

## Task 4: scan — 导航改名 +「工作事项分工」未读角标

**Files:**
- Modify: `frontend_real/App.vue`

- [ ] **Step 1: 导航改名**

把 `navItems` 中（约 173 行）的「项目承接」项 title 改为「工作事项分工」，hint 改为「作为负责人：选模版并为各工作环节指派负责人」，并加一个标记字段 `badge: 'worktask'`：

```ts
      { title: '工作事项分工', icon: 'mdi-handshake-outline', to: '/project-acceptance', badge: 'worktask', hint: '作为负责人：选模版并为各工作环节指派负责人' },
```

同组父级（约 167 行）hint 里「项目承接」改为「工作事项分工」。

- [ ] **Step 2: 脚本加未读拉取/清零**

在 `App.vue` `<script setup>` 中（已 import `ref`、`useRouter`/`router` 或 `useRoute`），加：

```ts
const worktaskUnread = ref(0)

async function loadWorktaskUnread() {
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/unread-count`)
    const j = await r.json()
    if (j.success) worktaskUnread.value = Number(j.data?.count) || 0
  } catch { /* 静默 */ }
}

async function clearWorktaskUnread() {
  try { await fetch(`${API_BASE}/centralized-projects/mark-seen`, { method: 'POST' }) } catch {}
  worktaskUnread.value = 0
}
```

确保 `API_BASE` 已 import（`import { API_BASE } from '@/services/api'`，若 App.vue 未引则加）。在 `onMounted` 里调用 `loadWorktaskUnread()`。用 `watch(() => route.path, ...)`（`route = useRoute()`）：当进入 `/project-acceptance` 时调 `clearWorktaskUnread()`；离开后重新 `loadWorktaskUnread()` 以便新指派再次出现。

```ts
watch(() => route.path, (p, old) => {
  if (p === '/project-acceptance') clearWorktaskUnread()
  else if (old === '/project-acceptance') loadWorktaskUnread()
})
```

（`watch`、`useRoute` 需在 import 中。）

- [ ] **Step 3: 模板加角标**

在导航渲染的子项 `v-list-item`（叶子项 `<v-list-item v-bind="props" :disabled=... :title=... :prepend-icon=... :to=...>`，约 282-289 行）上加一个 `#append` 插槽显示角标：

```vue
                <v-list-item
                  v-bind="props"
                  :disabled="item.disabled"
                  :title="item.title"
                  :prepend-icon="item.icon"
                  :to="item.to"
                >
                  <template #append v-if="item.badge === 'worktask' && worktaskUnread > 0">
                    <v-chip size="x-small" color="error" variant="flat">{{ worktaskUnread }}</v-chip>
                  </template>
                </v-list-item>
```

（注意：「工作事项分工」是某父级的 child，渲染处变量名可能是 `child` 而非 `item`——按实际 `v-for` 变量名套用 `child.badge === 'worktask'`。）

- [ ] **Step 4: 验证构建**

Run: `cd /root/data/projects/data-asset-scan && yarn build 2>&1 | tail -6`
Expected: 构建成功，无报错。

- [ ] **Step 5: 提交**

```bash
cd /root/data/projects/data-asset-scan
git add frontend_real/App.vue
git commit -m "feat(scan): 导航「项目承接」改名「工作事项分工」+ 未读角标"
```

---

## Task 5: scan — ProjectAcceptanceView 两步流程

**Files:**
- Modify: `frontend_real/views/ProjectAcceptanceView.vue`
- Test: `frontend_real/__tests__/ProjectAcceptanceView.test.ts`（新建或扩展）

把当前"一屏选模版+派环节"改为**两步**：行按钮 `选择模版 →（确定调 set-template）→ 分工 →（提交调 accept）→ 已分工`。复用现有的 `load()`(assigned)、模版列表加载、模版结构(stages)加载、`ownerOptions`、accept 调用逻辑——只是拆成两个对话框 + 接入 set-template + 按钮态。

行按钮态由数据推导（assigned 项目需带 `template_id`、`status`）：
- `status==='accepted'` → 文本「已分工」（只读）
- `template_id` 真值 且 `status==='approved'` → 按钮「分工」
- 否则 → 按钮「选择模版」

- [ ] **Step 1: 写失败测试**

新建/扩展 `frontend_real/__tests__/ProjectAcceptanceView.test.ts`：

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import ProjectAcceptanceView from '../views/ProjectAcceptanceView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })
function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}
function mountView() { return mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } }) }

describe('工作事项分工：按钮态 + 两步', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('按行状态推导按钮文案', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 1, project_name: 'A', owner_name: 'lead', status: 'approved', template_id: null, create_time: '' },
        { id: 2, project_name: 'B', owner_name: 'lead', status: 'approved', template_id: 9, create_time: '' },
        { id: 3, project_name: 'C', owner_name: 'lead', status: 'accepted', template_id: 9, create_time: '' },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.rowActionLabel(vm.items[0])).toBe('选择模版')
    expect(vm.rowActionLabel(vm.items[1])).toBe('分工')
    expect(vm.rowActionLabel(vm.items[2])).toBe('已分工')
  })

  it('确定调 set-template（POST，带 template_*）', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([{ id: 1, project_name: 'A', owner_name: 'lead', status: 'approved', template_id: null, create_time: '' }])
      if (url.includes('/set-template') && init?.method === 'POST') { posted.push({ url, body: JSON.parse(init.body as string) }); return ok({ id: 1, status: 'approved', template_id: 9 }) }
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    vm.openSelectTemplate(vm.items[0])
    vm.selectDialog.templateKey = 'L:9'
    vm.selectDialog.templates = [{ value: 'L:9', source: 'local', id: 9, template_code: 'TPL-X', template_name: 'X', template_version: 'V1', label: 'X' }]
    await vm.confirmTemplate()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].url).toContain('/set-template?id=1')
    expect(posted[0].body.template_code).toBe('TPL-X')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /root/data/projects/data-asset-scan && npm rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/ProjectAcceptanceView.test.ts`
Expected: FAIL（`rowActionLabel`/`openSelectTemplate`/`confirmTemplate`/`selectDialog` 未定义）

- [ ] **Step 3: 接口扩展 AssignedProject + 加 rowActionLabel**

确保 `interface AssignedProject` 含 `template_id: number | null`（若无则加）。在 `<script setup>` 加：

```ts
function rowActionLabel(it: AssignedProject): string {
  if (it.status === 'accepted') return '已分工'
  if (it.template_id) return '分工'
  return '选择模版'
}
```

- [ ] **Step 4: 加「选择模版」对话框状态与方法**

新增 `selectDialog` 状态 + 打开/确定方法（复用现有模版列表加载逻辑——把原 acceptDialog 里加载 templates 的函数抽出或复用）：

```ts
const selectDialog = ref({
  open: false, busy: false, applicationId: 0,
  projectName: '', sensitivity: '', dataOwner: '',
  templateKey: null as string | null,
  templates: [] as TemplateOption[], loadingTemplates: false,
})

async function openSelectTemplate(it: AssignedProject) {
  selectDialog.value = { open: true, busy: false, applicationId: it.id, projectName: it.project_name, sensitivity: (it as any).sensitivity_level || '', dataOwner: (it as any).data_owner || '', templateKey: null, templates: [], loadingTemplates: true }
  // 复用现有合并加载逻辑：拉本地 /templates/authoring + 远程 /templates/remote-list，填 selectDialog.templates
  await loadTemplatesInto(selectDialog.value) // ← 复用/抽取自原 acceptDialog 的模版加载
  selectDialog.value.loadingTemplates = false
}

async function confirmTemplate() {
  const d = selectDialog.value
  const opt = d.templates.find(t => t.value === d.templateKey)
  if (!opt) { snackbar.value = { show: true, text: '请选择模版', color: 'error' }; return }
  d.busy = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/set-template?id=${d.applicationId}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ template_id: opt.id, template_code: opt.template_code, template_version: opt.template_version }),
    })
    const j = await r.json()
    if (j.success) { d.open = false; snackbar.value = { show: true, text: '已选定模版，请继续分工', color: 'success' }; await load() }
    else snackbar.value = { show: true, text: '选模版失败：' + (j.error || ''), color: 'error' }
  } finally { d.busy = false }
}
```

> `loadTemplatesInto(target)` 把原 acceptDialog 中"拉本地+远程模版并去重成 TemplateOption[]"那段抽成一个可复用函数，写入 `target.templates`。原承接对话框的"分工"步骤继续复用既有的 stages 加载 + accept 提交逻辑，仅入口改为 `rowActionLabel==='分工'` 时打开。

- [ ] **Step 5: 模板按钮区接线**

把行操作列按钮改为按 `rowActionLabel` 分流：「选择模版」→ `openSelectTemplate(item)`；「分工」→ 打开既有承接(分工)对话框；「已分工」→ 纯文本。新增「选择模版」对话框模板（立项基本信息只读 + 模版下拉 + 确定）。保持既有"分工"对话框（各环节派负责人 + 提交 accept）不变。

- [ ] **Step 6: 运行确认通过**

Run: `cd /root/data/projects/data-asset-scan && npm rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/ProjectAcceptanceView.test.ts`
Expected: PASS

- [ ] **Step 7: 构建 + 提交**

```bash
cd /root/data/projects/data-asset-scan && yarn build 2>&1 | tail -5
git add frontend_real/views/ProjectAcceptanceView.vue frontend_real/__tests__/ProjectAcceptanceView.test.ts
git commit -m "feat(scan): 工作事项分工改为两步（选模版→确定→分工→提交）"
```

---

## 自查（Self-Review）

- **Spec 覆盖**：set-template(Task1) / 未读列+接口(Task2) / scan 代理(Task3) / 导航改名+角标(Task4) / 两步视图(Task5) —— 均有任务。
- **不在范围**：文件任务参与人、文件任务指派/受理、删我的工作事项、Tier2/3 角标 —— 与设计一致，未纳入（P2/P3）。
- **类型一致**：`set-template` body `{acceptor,template_id,template_code,template_version}` 三处一致（manage 接口/repo/scan 代理注入 acceptor）；`unread-count` 回 `{count}`、`mark-seen` 回 `{updated}` 前后端一致；`rowActionLabel` 依据 `status`+`template_id`，assigned 接口需带这两字段（manage `findAll` 用 `SELECT *` 已含 template_id；scan assigned 代理透传）。
- **占位符**：Task5 的 `loadTemplatesInto`/"分工对话框"复用现有代码，已明确指出抽取来源与复用点，非空泛 TODO；其余步骤均给完整代码。

## 风险与注意
- **assigned 需带 template_id/status**：scan 的 `/centralized-projects/assigned` 透传 manage `list`（`SELECT *`，含 template_id/status）。实现 Task5 时确认前端 `AssignedProject` 解析到 `template_id`。
- **manage 迁移**：`owner_viewed_at` 经 `addColumnIfMissing` 幂等加列，旧库安全。
- **set-template 后 accept**：accept 仍带 template_*（已选），与 set-template 写入一致，幂等无冲突。
- **角标渲染变量名**：App.vue 导航子项 `v-for` 变量名以实际为准（`child`/`item`）。
- **测试前置**：scan 前端测试前 `npm rebuild better-sqlite3`；manage 测试用 `npx vitest`（其仓库脚本）。
