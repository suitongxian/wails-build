# scan「新建立项书」对话框 + 草稿态 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 scan「项目立项」页的内联表单改为「新建立项书」对话框（五板块、纯文本字段），并引入「存草稿（只存本地不推 manage、可重开编辑）/ 发布（推 manage）」两态。

**Architecture:** 复用现有 `centralized_project_applications` 表，新增 4 列（project_code/department/approval_basis/description），`status` 新增取值 `draft`。后端在现有 `POST /centralized-projects` 加 `save_as_draft` 分支，并新增 `PUT /centralized-projects/draft` 编辑草稿。前端 `CentralizedProjectView.vue` 用一个 `v-dialog` 承载五板块表单，列表对草稿行提供「编辑」入口。立项阶段**不关联模版**（保持现状）。

**Tech Stack:** Go + Gin + sqlx（modernc/mattn sqlite），Vue3 + Vuetify 4 + vitest。后端测试用 `setupTestServer` harness + httptest mock manage；前端测试前需 `yarn rebuild better-sqlite3`。

**关键约束（来自 CLAUDE.md）：** 每个任务用例通过再进下一个；yarn 代替 npm；严禁出现删除/修改扫描文件的代码；测试前 `yarn rebuild better-sqlite3`。

---

## 文件结构

**后端（Go）**
- 修改 `internal/repository/db.go`：迁移列表新增 4 条 ALTER（约 375 行 data_owner 之后）。
- 修改 `internal/httpd/centralized_projects.go`：扩展请求结构体、create 分支、新增 update-draft handler、路由注册、扩展 row 结构体与 SELECT。
- 新增/扩展测试 `internal/httpd/centralized_projects_draft_test.go`：草稿/发布/编辑/校验。

**前端（Vue）**
- 修改 `frontend_real/views/CentralizedProjectView.vue`：删内联表单、加对话框、加草稿编辑、扩展 ownerOptions/状态映射。
- 修改 `frontend_real/__tests__/CentralizedProjectView.test.ts`：适配对话框；新增草稿/发布/部门自动填测试。

---

## Task 1: 后端迁移——新增 4 列

**Files:**
- Modify: `internal/repository/db.go:375`（紧跟 data_owner 那条之后）
- Test: `internal/httpd/centralized_projects_draft_test.go`（新建）

- [ ] **Step 1: 写失败测试（确认 4 列存在）**

新建文件 `internal/httpd/centralized_projects_draft_test.go`：

```go
package httpd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 迁移后 centralized_project_applications 应含 4 个新列。
func TestMigration_CentralizedProjectNewColumns(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()

	rows, err := db.Query(`PRAGMA table_info(centralized_project_applications)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	for _, want := range []string{"project_code", "department", "approval_basis", "description"} {
		if !cols[want] {
			t.Errorf("缺少列 %s", want)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /root/data/projects/data-asset-scan && go test ./internal/httpd/ -run TestMigration_CentralizedProjectNewColumns -v`
Expected: FAIL（缺少列 project_code 等）

- [ ] **Step 3: 加迁移**

在 `internal/repository/db.go` 中 `data_owner` 那条（约 375 行）之后插入：

```go
		// 2026-06-08 立项书：基本信息/责任主体/立项依据/项目简介 4 列
		{"centralized_project_applications", "project_code", "ALTER TABLE centralized_project_applications ADD COLUMN project_code TEXT"},
		{"centralized_project_applications", "department", "ALTER TABLE centralized_project_applications ADD COLUMN department TEXT"},
		{"centralized_project_applications", "approval_basis", "ALTER TABLE centralized_project_applications ADD COLUMN approval_basis TEXT"},
		{"centralized_project_applications", "description", "ALTER TABLE centralized_project_applications ADD COLUMN description TEXT"},
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/httpd/ -run TestMigration_CentralizedProjectNewColumns -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/repository/db.go internal/httpd/centralized_projects_draft_test.go
git commit -m "feat(scan): 立项表新增 project_code/department/approval_basis/description 4 列"
```

---

## Task 2: 后端 create——save_as_draft 分支 + 新字段落库

**Files:**
- Modify: `internal/httpd/centralized_projects.go:98-176`（请求结构体 + CreateCentralizedProject）
- Test: `internal/httpd/centralized_projects_draft_test.go`

- [ ] **Step 1: 写失败测试（草稿不推、发布推送）**

追加到 `centralized_projects_draft_test.go`：

```go
// 存草稿：status=draft，不推 manage（mock 命中 0 次）。
func TestHTTP_CentralizedProjects_SaveAsDraft(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	hits := 0
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":777}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 草稿允许只填项目名称（不填负责人/敏感级也能存）
	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name":  "半成品立项",
		"save_as_draft": true,
	})
	successOk(t, status, resp)
	if dataMap(t, resp)["status"] != "draft" {
		t.Errorf("status=%v want draft", dataMap(t, resp)["status"])
	}
	if hits != 0 {
		t.Errorf("草稿不应推送 manage，hits=%d", hits)
	}
}

// 发布：status=approved，推 manage（mock 命中 1 次），新字段落库可在列表读到。
func TestHTTP_CentralizedProjects_PublishWithNewFields(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredUser(t, db, "张三")

	hits := 0
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":777}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name":      "正式立项",
		"project_code":      "PRJ-001",
		"owner_name":        "张三",
		"department":        "第一研究院",
		"sensitivity_level": "important",
		"data_owner":        "甲方",
		"approval_basis":    "上级批文",
		"description":       "项目简介内容",
		"save_as_draft":     false,
	})
	successOk(t, status, resp)
	if dataMap(t, resp)["status"] != "approved" {
		t.Errorf("status=%v want approved", dataMap(t, resp)["status"])
	}
	if hits != 1 {
		t.Errorf("发布应推送 manage 一次，hits=%d", hits)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/httpd/ -run 'SaveAsDraft|PublishWithNewFields' -v`
Expected: FAIL（草稿当前仍校验负责人/推送；新字段未存）

- [ ] **Step 3: 扩展请求结构体**

`internal/httpd/centralized_projects.go` 替换 `CreateCentralizedProjectRequest`（98-103 行）：

```go
// CreateCentralizedProjectRequest POST /centralized-projects 入参
type CreateCentralizedProjectRequest struct {
	ProjectName      string `json:"project_name"`
	ProjectCode      string `json:"project_code"`
	OwnerName        string `json:"owner_name"`
	Department       string `json:"department"`
	DataOwner        string `json:"data_owner"`        // 数据权属（原"定数权"，选填）
	SensitivityLevel string `json:"sensitivity_level"` // core / important / general
	ApprovalBasis    string `json:"approval_basis"`    // 立项依据（选填，纯文本）
	Description      string `json:"description"`       // 项目简介（选填，纯文本）
	SaveAsDraft      bool   `json:"save_as_draft"`     // true=存草稿(不推manage)
}
```

- [ ] **Step 4: 改写 CreateCentralizedProject**

替换函数体（105-176 行）为：

```go
// CreateCentralizedProject POST /centralized-projects
//
// save_as_draft=true：status='draft'，只存本地、不推 manage，仅校验项目名称非空。
// save_as_draft=false：status='approved' + 推送 manage（校验负责人已注册、敏感级合法）。
func CreateCentralizedProject(c *gin.Context) {
	var req CreateCentralizedProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	projectName := strings.TrimSpace(req.ProjectName)
	ownerName := strings.TrimSpace(req.OwnerName)
	department := strings.TrimSpace(req.Department)
	dataOwner := strings.TrimSpace(req.DataOwner)
	projectCode := strings.TrimSpace(req.ProjectCode)
	approvalBasis := strings.TrimSpace(req.ApprovalBasis)
	description := strings.TrimSpace(req.Description)
	sensitivity := strings.ToLower(strings.TrimSpace(req.SensitivityLevel))
	draft := req.SaveAsDraft

	if projectName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "项目名称必填"})
		return
	}
	if !draft {
		if ownerName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "项目名称和负责人都必填"})
			return
		}
		if sensitivity != "core" && sensitivity != "important" && sensitivity != "general" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": "项目敏感等级必填，且仅支持 core / important / general"})
			return
		}
		// 负责人必须是 manage 已注册 active 用户
		_ = syncUsersFromManage(c)
		userRepo := repository.NewUserRepository(repository.GetDB())
		if u, err := userRepo.FindByUsername(ownerName); err != nil || u == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": fmt.Sprintf("负责人「%s」未在系统中注册（或已禁用），请到管理端用户列表建账号后再立项", ownerName)})
			return
		}
	}
	if sensitivity != "core" && sensitivity != "important" && sensitivity != "general" {
		sensitivity = "general" // 草稿未填敏感级时落默认值
	}

	status := "approved"
	if draft {
		status = "draft"
	}
	now := time.Now()
	operator := currentOperator(c)
	db := repository.GetDB()
	res, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, project_code, owner_name, department, data_owner, approval_basis, description,
		 submitted_by, status, sync_status, sensitivity_level, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, 0)`,
		projectName, projectCode, ownerName, department, dataOwner, approvalBasis, description,
		operator, status, sensitivity, now, now)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()

	syncStatus := "pending"
	if !draft {
		remoteID, pushErr := pushCentralizedProjectToManage(db, id, projectName, ownerName, dataOwner, operator, sensitivity)
		if pushErr != nil {
			_, _ = db.Exec(`UPDATE centralized_project_applications
			                  SET sync_status='failed', sync_error=?, update_time=? WHERE id=?`,
				pushErr.Error(), time.Now(), id)
			syncStatus = "failed"
		} else {
			_, _ = db.Exec(`UPDATE centralized_project_applications
			                  SET sync_status='synced', sync_error=NULL, manage_remote_id=?, update_time=? WHERE id=?`,
				remoteID, time.Now(), id)
			syncStatus = "synced"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"id": id, "status": status, "sync_status": syncStatus},
	})
}
```

- [ ] **Step 5: 运行测试确认通过（含旧用例不回归）**

Run: `go test ./internal/httpd/ -run 'CentralizedProjects' -v`
Expected: PASS（新草稿/发布用例 + 旧 CreateAndList/RejectsEmpty 等全过）

- [ ] **Step 6: 提交**

```bash
git add internal/httpd/centralized_projects.go internal/httpd/centralized_projects_draft_test.go
git commit -m "feat(scan): 立项 create 支持 save_as_draft 草稿态与新字段落库"
```

---

## Task 3: 后端 update-draft 接口

**Files:**
- Modify: `internal/httpd/centralized_projects.go`（路由注册 21-38 行；文件末尾新增 handler）
- Test: `internal/httpd/centralized_projects_draft_test.go`

- [ ] **Step 1: 写失败测试（编辑草稿 / 从草稿发布 / 非草稿拒绝）**

追加到 `centralized_projects_draft_test.go`：

```go
// 编辑草稿仍为 draft；带 save_as_draft=false 则发布为 approved 并推送；非草稿拒绝编辑。
func TestHTTP_CentralizedProjects_UpdateDraft(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredUser(t, db, "张三")

	hits := 0
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":777}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 先建草稿
	_, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "草稿A", "save_as_draft": true,
	})
	id := resp["data"].(map[string]interface{})["id"]

	// 编辑草稿（仍存草稿）
	status, resp := jsonReq(t, r, "PUT", "/centralized-projects/draft", map[string]interface{}{
		"id": id, "project_name": "草稿A改名", "save_as_draft": true,
	})
	successOk(t, status, resp)
	if dataMap(t, resp)["status"] != "draft" || hits != 0 {
		t.Errorf("编辑草稿应仍为 draft 且不推送：status=%v hits=%d", dataMap(t, resp)["status"], hits)
	}

	// 从草稿发布
	status, resp = jsonReq(t, r, "PUT", "/centralized-projects/draft", map[string]interface{}{
		"id": id, "project_name": "草稿A改名", "owner_name": "张三",
		"department": "第一研究院", "sensitivity_level": "general", "save_as_draft": false,
	})
	successOk(t, status, resp)
	if dataMap(t, resp)["status"] != "approved" || hits != 1 {
		t.Errorf("从草稿发布应 approved 且推送一次：status=%v hits=%d", dataMap(t, resp)["status"], hits)
	}

	// 再次编辑（已是 approved，非草稿）应被拒
	status, resp = jsonReq(t, r, "PUT", "/centralized-projects/draft", map[string]interface{}{
		"id": id, "project_name": "x", "save_as_draft": true,
	})
	expectFailure(t, status, resp)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/httpd/ -run TestHTTP_CentralizedProjects_UpdateDraft -v`
Expected: FAIL（404/路由不存在）

- [ ] **Step 3: 注册路由**

在 `RegisterCentralizedProjectsRoutes`（约 23 行 `r.POST("", CreateCentralizedProject)` 之后）加：

```go
	// 2026-06-08 编辑草稿 / 从草稿发布（id 在 body，避开与 :remote_id 的路由参数冲突）
	r.PUT("/draft", UpdateCentralizedProjectDraft)
```

- [ ] **Step 4: 实现 handler**

在文件末尾追加：

```go
// UpdateCentralizedProjectDraftRequest PUT /centralized-projects/draft 入参（id 在 body）
type UpdateCentralizedProjectDraftRequest struct {
	ID int64 `json:"id"`
	CreateCentralizedProjectRequest
}

// UpdateCentralizedProjectDraft PUT /centralized-projects/draft
//
// 仅 status='draft' 且属于当前提交者的记录可改。save_as_draft=false 即"从草稿发布"。
func UpdateCentralizedProjectDraft(c *gin.Context) {
	var req UpdateCentralizedProjectDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	db := repository.GetDB()
	operator := currentOperator(c)

	var cur struct {
		ID     int64  `db:"id"`
		Status string `db:"status"`
	}
	if err := db.Get(&cur, `SELECT id, status FROM centralized_project_applications
	                          WHERE id=? AND submitted_by=? AND disable=0`, req.ID, operator); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "草稿不存在或无权编辑"})
		return
	}
	if cur.Status != "draft" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "仅草稿可编辑"})
		return
	}

	projectName := strings.TrimSpace(req.ProjectName)
	ownerName := strings.TrimSpace(req.OwnerName)
	department := strings.TrimSpace(req.Department)
	dataOwner := strings.TrimSpace(req.DataOwner)
	projectCode := strings.TrimSpace(req.ProjectCode)
	approvalBasis := strings.TrimSpace(req.ApprovalBasis)
	description := strings.TrimSpace(req.Description)
	sensitivity := strings.ToLower(strings.TrimSpace(req.SensitivityLevel))
	draft := req.SaveAsDraft

	if projectName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "项目名称必填"})
		return
	}
	if !draft {
		if ownerName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "项目名称和负责人都必填"})
			return
		}
		if sensitivity != "core" && sensitivity != "important" && sensitivity != "general" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": "项目敏感等级必填，且仅支持 core / important / general"})
			return
		}
		_ = syncUsersFromManage(c)
		userRepo := repository.NewUserRepository(repository.GetDB())
		if u, err := userRepo.FindByUsername(ownerName); err != nil || u == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": fmt.Sprintf("负责人「%s」未在系统中注册（或已禁用），请到管理端用户列表建账号后再立项", ownerName)})
			return
		}
	}
	if sensitivity != "core" && sensitivity != "important" && sensitivity != "general" {
		sensitivity = "general"
	}

	status := "draft"
	if !draft {
		status = "approved"
	}
	now := time.Now()
	if _, err := db.Exec(`UPDATE centralized_project_applications
		SET project_name=?, project_code=?, owner_name=?, department=?, data_owner=?,
		    approval_basis=?, description=?, sensitivity_level=?, status=?, update_time=?
		WHERE id=?`,
		projectName, projectCode, ownerName, department, dataOwner,
		approvalBasis, description, sensitivity, status, now, req.ID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	syncStatus := "pending"
	if !draft {
		remoteID, pushErr := pushCentralizedProjectToManage(db, req.ID, projectName, ownerName, dataOwner, operator, sensitivity)
		if pushErr != nil {
			_, _ = db.Exec(`UPDATE centralized_project_applications
			                  SET sync_status='failed', sync_error=?, update_time=? WHERE id=?`,
				pushErr.Error(), time.Now(), req.ID)
			syncStatus = "failed"
		} else {
			_, _ = db.Exec(`UPDATE centralized_project_applications
			                  SET sync_status='synced', sync_error=NULL, manage_remote_id=?, update_time=? WHERE id=?`,
				remoteID, time.Now(), req.ID)
			syncStatus = "synced"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"id": req.ID, "status": status, "sync_status": syncStatus},
	})
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/httpd/ -run 'CentralizedProjects' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/httpd/centralized_projects.go internal/httpd/centralized_projects_draft_test.go
git commit -m "feat(scan): 新增 PUT /centralized-projects/draft 编辑草稿/从草稿发布"
```

---

## Task 4: 后端列表返回新字段（供草稿回填）

**Files:**
- Modify: `internal/httpd/centralized_projects.go:40-55`（row 结构体）、`:80-90`（SELECT）
- Test: `internal/httpd/centralized_projects_draft_test.go`

- [ ] **Step 1: 写失败测试（列表能读到新字段）**

追加：

```go
// 列表应返回 project_code/department/approval_basis/description，供草稿回填。
func TestHTTP_CentralizedProjects_ListReturnsNewFields(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	_, _ = jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "草稿B", "project_code": "PC-9",
		"approval_basis": "依据X", "description": "简介Y", "save_as_draft": true,
	})

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects")
	successOk(t, status, resp)
	items, _ := dataMap(t, resp)["items"].([]interface{})
	first := items[0].(map[string]interface{})
	if first["project_code"] != "PC-9" || first["approval_basis"] != "依据X" || first["description"] != "简介Y" {
		t.Errorf("列表缺新字段: %+v", first)
	}
	if first["status"] != "draft" {
		t.Errorf("status=%v want draft", first["status"])
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/httpd/ -run TestHTTP_CentralizedProjects_ListReturnsNewFields -v`
Expected: FAIL（字段为 nil）

- [ ] **Step 3: 扩展 row 结构体**

`centralizedProjectRow`（40-55 行）在 `DataOwner` 之后加 4 字段：

```go
	ProjectCode      *string `db:"project_code" json:"project_code"`
	Department       *string `db:"department" json:"department"`
	ApprovalBasis    *string `db:"approval_basis" json:"approval_basis"`
	Description      *string `db:"description" json:"description"`
```

- [ ] **Step 4: 扩展 SELECT**

`ListCentralizedProjects` 的 SELECT（82-87 行）列清单加上新列：

```go
		`SELECT id, project_name, project_code, owner_name, department, data_owner, submitted_by,
		        status, sensitivity_level, approval_basis, description,
		        manage_remote_id, sync_status, sync_error, reject_reason, reviewed_at,
		        create_time, update_time
		   FROM centralized_project_applications
		  WHERE disable = 0 AND submitted_by = ?
		  ORDER BY id DESC LIMIT ? OFFSET ?`, operator, pageSize, (page-1)*pageSize); err != nil {
```

- [ ] **Step 5: 运行确认通过**

Run: `go test ./internal/httpd/ -run 'CentralizedProjects' -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/httpd/centralized_projects.go internal/httpd/centralized_projects_draft_test.go
git commit -m "feat(scan): 立项列表返回 project_code/department/approval_basis/description"
```

---

## Task 5: 前端——ownerOptions 带部门 + 草稿状态映射

**Files:**
- Modify: `frontend_real/views/CentralizedProjectView.vue`（OwnerOption 27-32、loadOwnerOptions 34-51、statusColor 209-211、statusLabel 213-215、CentralizedProject 接口 5-19）

- [ ] **Step 1: 扩展 CentralizedProject 接口**

在 interface（5-19 行）`data_owner` 之后加：

```ts
  project_code: string | null
  department: string | null
  approval_basis: string | null
  description: string | null
```

- [ ] **Step 2: OwnerOption 带部门**

替换 OwnerOption（27-32 行）与 loadOwnerOptions 的 map（42-46 行）：

```ts
interface OwnerOption {
  username: string
  display_name: string
  user_department: string
  label: string
}
```

map 部分：

```ts
        .map((u: any) => ({
          username: u.username,
          display_name: u.display_name || u.username,
          user_department: u.user_department || '',
          label: `${u.display_name || u.username} (${u.username})`,
        }))
```

- [ ] **Step 3: 草稿状态映射**

替换 statusColor/statusLabel（209-215 行）：

```ts
function statusColor(s: string): string {
  return ({ draft: 'grey', pending: 'warning', approved: 'success', rejected: 'error', cancelled: 'grey' } as Record<string, string>)[s] || 'grey'
}

function statusLabel(s: string): string {
  return ({ draft: '草稿', pending: '处理中', approved: '已立项', rejected: '已驳回', accepted: '已承接', closed: '已结项', cancelled: '已取消' } as Record<string, string>)[s] || s
}
```

- [ ] **Step 4: 构建确认无类型错误**

Run: `cd /root/data/projects/data-asset-scan && yarn vue-tsc --noEmit -p tsconfig.json 2>&1 | grep CentralizedProjectView || echo "no errors in view"`
Expected: `no errors in view`（若项目无 vue-tsc 脚本，改用 `yarn build` 或跳过，靠 Task 6 测试兜底）

- [ ] **Step 5: 提交**

```bash
git add frontend_real/views/CentralizedProjectView.vue
git commit -m "feat(scan): 立项视图 ownerOptions 带部门 + 草稿状态映射"
```

---

## Task 6: 前端——「新建立项书」对话框 + 发布/存草稿

**Files:**
- Modify: `frontend_real/views/CentralizedProjectView.vue`（删旧表单状态 53-75、submit 175-202；删模板内联表单 258-335；新增对话框状态与模板）
- Test: `frontend_real/__tests__/CentralizedProjectView.test.ts`

- [ ] **Step 1: 写失败测试（对话框 + 部门自动填 + 草稿/发布请求）**

替换 `CentralizedProjectView.test.ts` 中第一个用例（"表单含「定数权」字段，立项提交携带 data_owner"），新增以下用例（保留其余结项相关用例不动）。新测试：

```ts
  it('选负责人后自动填部门，且只读', async () => {
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    vm.ownerOptions = [{ username: 'zhangsan', display_name: '张三', user_department: '第一研究院', label: '张三 (zhangsan)' }]
    vm.openCreate()
    vm.dform.owner_name = 'zhangsan'
    vm.onOwnerSelected('zhangsan')
    expect(vm.dform.department).toBe('第一研究院')
  })

  it('存草稿走 save_as_draft=true（POST），仅需项目名称', async () => {
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ success: true, data: { id: 1, status: 'draft' } }), { status: 200 }),
    )
    vm.openCreate()
    vm.dform.project_name = '半成品'
    await vm.persist(true)
    const call = spy.mock.calls.find(c => String(c[0]).endsWith('/centralized-projects'))
    expect(call).toBeTruthy()
    expect(JSON.parse((call![1] as any).body).save_as_draft).toBe(true)
    spy.mockRestore()
  })

  it('发布走 save_as_draft=false；缺必填时 canPublish=false', async () => {
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    vm.openCreate()
    vm.dform.project_name = '正式'
    expect(vm.canPublish).toBe(false) // 缺负责人/部门
    vm.dform.owner_name = 'zhangsan'
    vm.dform.department = '第一研究院'
    vm.dform.sensitivity_level = 'general'
    expect(vm.canPublish).toBe(true)
  })

  it('编辑草稿走 PUT /draft 带 id', async () => {
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ success: true, data: { id: 9, status: 'draft' } }), { status: 200 }),
    )
    vm.openEdit({ id: 9, project_name: '草稿', project_code: null, owner_name: '', department: null, sensitivity_level: 'general', data_owner: null, approval_basis: null, description: null })
    await vm.persist(true)
    const call = spy.mock.calls.find(c => String(c[0]).endsWith('/centralized-projects/draft'))
    expect(call).toBeTruthy()
    expect((call![1] as any).method).toBe('PUT')
    expect(JSON.parse((call![1] as any).body).id).toBe(9)
    spy.mockRestore()
  })
```

确保测试文件顶部 import 含 `flushPromises`、`vi`，并有一个 `mountView()` 帮助函数（沿用文件现有的 mount 方式；若现有用例用的是局部 mount，则抽出 `mountView`）。

- [ ] **Step 2: 运行确认失败**

Run: `cd /root/data/projects/data-asset-scan && yarn rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/CentralizedProjectView.test.ts`
Expected: FAIL（openCreate/persist/canPublish 未定义）

- [ ] **Step 3: 替换脚本——对话框状态与方法**

删除旧的 `form`/`sensitivityOptions`(保留)/`currentSensitivityHint`/`canSubmit`/`submit`（53-75、175-202 行中与内联表单相关的部分），改为：

```ts
const dialog = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)

const blankForm = () => ({
  project_name: '', project_code: '', owner_name: '', department: '',
  sensitivity_level: 'general' as 'core' | 'important' | 'general',
  data_owner: '', approval_basis: '', description: '',
})
const dform = ref(blankForm())

const sensitivityOptions = [
  { value: 'core',      label: '核心',  hint: '需重点保护、需登记 / 上报的高敏文件' },
  { value: 'important', label: '重要',  hint: '部门 / 单位内部权威源，多源时需裁定' },
  { value: 'general',   label: '一般',  hint: '低敏，可批量 AI 归目' },
]

function onOwnerSelected(username: string) {
  const o = ownerOptions.value.find(x => x.username === username)
  dform.value.department = o?.user_department || ''
}

const canPublish = computed(() =>
  dform.value.project_name.trim() !== '' &&
  dform.value.owner_name.trim() !== '' &&
  dform.value.department.trim() !== '' &&
  !!dform.value.sensitivity_level)

const canDraft = computed(() => dform.value.project_name.trim() !== '')

function openCreate() {
  editingId.value = null
  dform.value = blankForm()
  dialog.value = true
}

function openEdit(it: CentralizedProject) {
  editingId.value = it.id
  dform.value = {
    project_name: it.project_name || '',
    project_code: it.project_code || '',
    owner_name: it.owner_name || '',
    department: it.department || '',
    sensitivity_level: (it.sensitivity_level || 'general') as 'core' | 'important' | 'general',
    data_owner: it.data_owner || '',
    approval_basis: it.approval_basis || '',
    description: it.description || '',
  }
  dialog.value = true
}

async function persist(asDraft: boolean) {
  if (asDraft ? !canDraft.value : !canPublish.value) return
  saving.value = true
  try {
    const payload: Record<string, unknown> = {
      project_name: dform.value.project_name.trim(),
      project_code: dform.value.project_code.trim(),
      owner_name: dform.value.owner_name.trim(),
      department: dform.value.department.trim(),
      data_owner: dform.value.data_owner.trim(),
      sensitivity_level: dform.value.sensitivity_level,
      approval_basis: dform.value.approval_basis,
      description: dform.value.description,
      save_as_draft: asDraft,
    }
    let url = `${API_BASE}/centralized-projects`
    let method = 'POST'
    if (editingId.value != null) {
      url = `${API_BASE}/centralized-projects/draft`
      method = 'PUT'
      payload.id = editingId.value
    }
    const r = await fetch(url, {
      method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
    })
    const j = await r.json()
    if (j.success) {
      dialog.value = false
      snackbar.value = { show: true, text: asDraft ? '已存草稿' : '已发布', color: 'success' }
      await load()
    } else {
      snackbar.value = { show: true, text: (asDraft ? '存草稿失败：' : '发布失败：') + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '操作失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    saving.value = false
  }
}
```

确保 `import { ref, computed, onMounted } from 'vue'` 不变（已含 computed）。

- [ ] **Step 4: 替换模板——删内联表单、加按钮与对话框**

把模板里旧的"立项表单"卡片（258-335 行整段 `<v-card class="mb-4">...</v-card>`）删除；在"已提交的立项列表"卡片标题区加「新建立项书」按钮——把 343 行附近的标题块改为：

```vue
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-format-list-checks</v-icon>
        已提交的立项申请
        <v-spacer />
        <v-btn color="primary" variant="flat" prepend-icon="mdi-plus" class="mr-2" @click="openCreate">新建立项书</v-btn>
        <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading || refreshing" @click="refreshAll">刷新</v-btn>
      </v-card-title>
```

在结项对话框（398-412 行）之前插入立项书对话框：

```vue
    <!-- 新建/编辑立项书 -->
    <v-dialog v-model="dialog" max-width="760" persistent scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-clipboard-plus-outline</v-icon>
          {{ editingId == null ? '新建立项书' : '编辑立项书（草稿）' }}
        </v-card-title>
        <v-card-text style="max-height:70vh;">
          <!-- 板块一 基本信息 -->
          <div class="text-subtitle-2 mb-2">一、基本信息</div>
          <v-row>
            <v-col cols="12" md="6">
              <v-text-field v-model="dform.project_name" label="项目名称 *" variant="outlined" density="compact" :disabled="saving" />
            </v-col>
            <v-col cols="12" md="6">
              <v-text-field v-model="dform.project_code" label="项目代号" variant="outlined" density="compact" :disabled="saving" />
            </v-col>
          </v-row>

          <!-- 板块二 责任主体 -->
          <div class="text-subtitle-2 mb-2 mt-2">二、责任主体</div>
          <v-row>
            <v-col cols="12" md="6">
              <v-autocomplete
                v-model="dform.owner_name"
                :items="ownerOptions"
                item-title="label"
                item-value="username"
                label="项目负责人 *"
                variant="outlined" density="compact" clearable :disabled="saving"
                :no-data-text="ownerOptions.length === 0 ? '加载中或暂无可选用户' : '无匹配项'"
                @update:model-value="onOwnerSelected"
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-text-field v-model="dform.department" label="所属部门 *（选负责人后自动带出）" variant="outlined" density="compact" readonly />
            </v-col>
          </v-row>

          <!-- 板块三 安全定级 -->
          <div class="text-subtitle-2 mb-2 mt-2">三、安全定级</div>
          <v-radio-group v-model="dform.sensitivity_level" inline density="compact" hide-details class="mb-2">
            <v-radio v-for="o in sensitivityOptions" :key="o.value" :label="o.label" :value="o.value" />
          </v-radio-group>
          <v-text-field v-model="dform.data_owner" label="数据权属（选填）" variant="outlined" density="compact" :disabled="saving" />

          <!-- 板块四 立项依据 -->
          <div class="text-subtitle-2 mb-2 mt-2">四、立项依据</div>
          <v-textarea v-model="dform.approval_basis" label="立项依据（选填）" :rows="3" variant="outlined" density="compact" :disabled="saving" />

          <!-- 板块五 项目简介 -->
          <div class="text-subtitle-2 mb-2 mt-2">五、项目简介</div>
          <v-textarea v-model="dform.description" label="项目简介（选填）" :rows="3" variant="outlined" density="compact" :disabled="saving" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="saving" @click="dialog = false">取消</v-btn>
          <v-btn variant="tonal" :loading="saving" :disabled="!canDraft" @click="persist(true)">存草稿</v-btn>
          <v-btn color="primary" variant="flat" :loading="saving" :disabled="!canPublish" @click="persist(false)">发布</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /root/data/projects/data-asset-scan && yarn rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/CentralizedProjectView.test.ts`
Expected: PASS（含新对话框用例 + 结项相关旧用例）

- [ ] **Step 6: 提交**

```bash
git add frontend_real/views/CentralizedProjectView.vue frontend_real/__tests__/CentralizedProjectView.test.ts
git commit -m "feat(scan): 立项页改为「新建立项书」对话框，支持发布/存草稿"
```

---

## Task 7: 前端——列表草稿行「编辑」入口

**Files:**
- Modify: `frontend_real/views/CentralizedProjectView.vue`（headers 77-87、closure 列模板）
- Test: `frontend_real/__tests__/CentralizedProjectView.test.ts`

- [ ] **Step 1: 写失败测试（草稿行渲染「编辑」）**

追加：

```ts
  it('草稿行展示「草稿」状态与「编辑」按钮', async () => {
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    vm.items = [{ id: 5, project_name: '草稿X', status: 'draft', sensitivity_level: 'general', sync_status: 'pending', owner_name: '', project_code: null, department: null, data_owner: null, approval_basis: null, description: null, manage_remote_id: null, sync_error: null, reject_reason: null, reviewed_at: null, create_time: '', submitted_by: '' }]
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('草稿')
    expect(wrapper.text()).toContain('编辑')
  })
```

- [ ] **Step 2: 运行确认失败**

Run: `yarn vitest run frontend_real/__tests__/CentralizedProjectView.test.ts -t '草稿行'`
Expected: FAIL（无"编辑"）

- [ ] **Step 3: headers 加操作列**

`headers`（77-87 行）在 `closure` 之前插入：

```ts
  { title: '操作', key: 'actions', width: '90px', sortable: false },
```

- [ ] **Step 4: 模板加操作列渲染**

在 `<template #item.closure=...>` 之前加：

```vue
        <template #item.actions="{ item }">
          <v-btn v-if="item.status === 'draft'" size="small" variant="text" color="primary" prepend-icon="mdi-pencil" @click="openEdit(item)">编辑</v-btn>
          <span v-else class="text-caption text-grey">—</span>
        </template>
```

- [ ] **Step 5: 运行确认通过 + 全套前端测试**

Run: `cd /root/data/projects/data-asset-scan && yarn rebuild better-sqlite3 && yarn vitest run frontend_real/__tests__/CentralizedProjectView.test.ts frontend_real/__tests__/frontend-integration.test.ts`
Expected: PASS

- [ ] **Step 6: 整体构建 + 后端全测**

Run: `cd /root/data/projects/data-asset-scan && go test ./internal/httpd/ ./internal/repository/ && yarn build`
Expected: 后端 PASS；前端 build 成功

- [ ] **Step 7: 提交**

```bash
git add frontend_real/views/CentralizedProjectView.vue frontend_real/__tests__/CentralizedProjectView.test.ts
git commit -m "feat(scan): 立项列表草稿行提供「编辑」入口"
```

---

## 自查（Self-Review）

- **Spec 覆盖：** 板块一(Task6 模板) / 板块二+部门自动填(Task5/6) / 板块三单选+数据权属(Task6) / 板块四五文本框(Task6) / 三按钮(Task6) / 草稿态(Task2/3) / 列表草稿标识+编辑(Task5/7) / 后端 4 列(Task1) / 列表回填字段(Task4) — 均有任务覆盖。
- **不在范围：** 模版关联、富文本、项目代号唯一性、manage 端字段扩展 — 与设计文档一致，未纳入任务。
- **类型一致：** 后端 `save_as_draft`(snake) ↔ 前端 payload `save_as_draft`；row 新增 `*string` 指针列与 SELECT 列名一致；前端 `CentralizedProject` 接口含 project_code/department/approval_basis/description，`openEdit` 读取一致。
- **占位符：** 无 TODO/TBD；每个改动步骤均给出完整代码。

## 风险与注意

- **路由参数冲突：** update-draft 用静态路径 `PUT /draft`（id 在 body），避免与现有 `/:remote_id/...` 的 gin 参数名冲突。
- **manage 字段：** push 仍只带原 6 字段（manage 端建项接口未扩展）；部门/代号/依据/简介仅 scan 本地存储，符合设计"不为此阻塞本地功能"。
- **旧测试回归：** Task2 改了 create 校验分支与 INSERT，必须跑全 `-run CentralizedProjects` 确认 CreateAndList/RejectsEmpty/RejectsMissingSensitivity/RejectsUnregisteredOwner 不回归。
- **vitest 前置：** 每次跑前端测试前 `yarn rebuild better-sqlite3`（CLAUDE.md 硬性要求）。
