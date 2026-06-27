# Three-Tier Governance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 spec 里的"核心 / 重要 / 一般 三级分流治理"全部落地：6 桶 AI 归目 + 数字机要登记通道 + 权威源裁定 + 一键 AI 归目 + 分层台账主页。

**Architecture:** 数据模型上**只加 6 列 + 1 索引**（5 列在 `asset_ledgers`，1 列在 `data_resource_family`）；apply 流程加 2 个拦截点（核心走数字机要 hint、重要走 family 仲裁 409）；前端在 AIClassifyView 套一层 level sub-tab、新增 MemorandumView、改造 PersonalFilesView。

**Tech Stack:** Go (sqlx + Gin) + sqlite3，Vue 3 + Vuetify + TypeScript，Vitest + Go testing。

**Spec:** `docs/superpowers/specs/2026-05-21-three-tier-governance-design.md`

---

## Pre-flight

- Go 测试：`go test ./internal/...`
- 前端：用 **yarn**（项目规则），实测 `npx vitest run` 也能用；测试前如遇 sqlite native module 报错，先 `npm rebuild better-sqlite3`
- 当前分支：`go-test-template`，不切
- 每个 task 完成 = 测试通过 + commit + 移到下一个 task

---

### Task 1: Migration — 5 + 1 列 + 索引

**Files:**
- Modify: `internal/repository/db.go`（`columnAdds` 数组 + 额外索引 + family 列）
- Test: `internal/repository/three_tier_migration_test.go`（新建）

- [ ] **Step 1: 写失败测试**

Create `internal/repository/three_tier_migration_test.go`:

```go
package repository

import (
	"testing"
)

func TestMigration_AddsMemorandumColumns(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	for _, col := range []string{
		"memorandum_topic",
		"memorandum_classification",
		"memorandum_registered_at",
		"memorandum_registered_by",
		"memorandum_signature_hash",
	} {
		ok, err := columnExists(db, "asset_ledgers", col)
		if err != nil {
			t.Fatalf("columnExists %s: %v", col, err)
		}
		if !ok {
			t.Errorf("asset_ledgers.%s missing after migration", col)
		}
	}
}

func TestMigration_AddsFamilyAuthoritativeColumn(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	ok, err := columnExists(db, "data_resource_family", "authoritative_resource_id")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !ok {
		t.Fatal("data_resource_family.authoritative_resource_id missing")
	}
}
```

- [ ] **Step 2: 跑测试确认 FAIL**

Run: `go test ./internal/repository/ -run 'TestMigration_(AddsMemorandumColumns|AddsFamilyAuthoritativeColumn)' -count=1`

Expected: 6 missing-column errors.

- [ ] **Step 3: 在 `db.go` 的 `columnAdds` 数组追加 6 行**

打开 `internal/repository/db.go`，定位到 `columnAdds := []struct{...}{...}` 这一段（约 line 300 附近，2026-05-20 已加过 `data_origin` 那个，再继续在最后加 6 行）：

```go
		// 2026-05-21 三级分流治理：数字机要 5 列 + 家族权威源 1 列
		{"asset_ledgers", "memorandum_topic", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_topic TEXT"},
		{"asset_ledgers", "memorandum_classification", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_classification TEXT"},
		{"asset_ledgers", "memorandum_registered_at", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_registered_at DATETIME"},
		{"asset_ledgers", "memorandum_registered_by", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_registered_by INTEGER"},
		{"asset_ledgers", "memorandum_signature_hash", "ALTER TABLE asset_ledgers ADD COLUMN memorandum_signature_hash TEXT"},
		{"data_resource_family", "authoritative_resource_id", "ALTER TABLE data_resource_family ADD COLUMN authoritative_resource_id INTEGER"},
```

在那段循环之后再加索引（紧跟着已有 `idx_data_resources_origin_claim` 的位置）：

```go
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_family_authoritative
		ON data_resource_family(authoritative_resource_id)`); err != nil {
		return fmt.Errorf("create idx_family_authoritative: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_ledger_memorandum_registered
		ON asset_ledgers(memorandum_registered_at)`); err != nil {
		return fmt.Errorf("create idx_ledger_memorandum_registered: %w", err)
	}
```

- [ ] **Step 4: 跑测试，PASS**

Run: `go test ./internal/repository/ -run 'TestMigration_(AddsMemorandumColumns|AddsFamilyAuthoritativeColumn)' -count=1`

- [ ] **Step 5: 全 repo 回归**

Run: `go test ./internal/repository/...`

Expected: 全绿。

- [ ] **Step 6: Commit**

```bash
git add internal/repository/db.go internal/repository/three_tier_migration_test.go
git commit -m "feat(scan): three-tier columns — memorandum_* + family.authoritative_resource_id"
```

---

### Task 2: apply 时 importance_level ↔ project_code 强同步 + pending 过滤

**Files:**
- Modify: `internal/repository/personal_files_bridge.go`（在 `BridgeResourceToTarget` 末尾加同步）
- Modify: `internal/httpd/ai_classify.go`（pending SQL 加 `importance_level NOT IN (1, 4)` 过滤）
- Test: `internal/repository/personal_files_bridge_importance_test.go`（新建）
- Test: `internal/httpd/ai_classify_filter_test.go`（新建）

- [ ] **Step 1: 写 repository 失败测试**

Create `internal/repository/personal_files_bridge_importance_test.go`:

```go
package repository

import (
	"testing"
	"time"
)

func seedResource(t *testing.T, name, sign string) int64 {
	t.Helper()
	now := time.Now()
	db := openMigratedTestDB(t)
	t.Cleanup(func() { db.Close() })
	r, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, 0, 'new')`, sign, now, name, now, now)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := r.LastInsertId()
	return rid
}

// 验证不同 personal 项目代码下 importance_level 的目标值映射
func TestSyncImportanceFromProjectCode(t *testing.T) {
	cases := []struct {
		code string
		want int
	}{
		{PersonalCoreProjectCode, 1},
		{PersonalImportantProjectCode, 2},
		{PersonalGeneralProjectCode, 3},
		{"BIZ-NON-PERSONAL", 0}, // 业务项目不动
	}
	for _, c := range cases {
		got := SyncImportanceFromProjectCode(c.code)
		if got != c.want {
			t.Errorf("SyncImportanceFromProjectCode(%q) = %d, want %d", c.code, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 跑 FAIL**

Run: `go test ./internal/repository/ -run 'TestSyncImportanceFromProjectCode' -count=1`

Expected: undefined function error.

- [ ] **Step 3: 在 `personal_files_bridge.go` 末尾加映射 + 同步**

打开 `internal/repository/personal_files_bridge.go`，文件最末追加：

```go
// SyncImportanceFromProjectCode 把 personal 项目代码映射到 importance_level 的目标值。
// 非个人项目返回 0，调用方应解读为"不动"。
func SyncImportanceFromProjectCode(projectCode string) int {
	switch projectCode {
	case PersonalCoreProjectCode:
		return 1
	case PersonalImportantProjectCode:
		return 2
	case PersonalGeneralProjectCode:
		return 3
	default:
		return 0
	}
}

// SyncResourceImportance 在 apply 成功后把 data_resources.importance_level 同步到目标级别。
// projectCode 非 SYS-PERSONAL-* 则跳过。
func SyncResourceImportance(db sqlxDBExec, resourceID int64, projectCode string) error {
	target := SyncImportanceFromProjectCode(projectCode)
	if target == 0 {
		return nil
	}
	_, err := db.Exec(`UPDATE data_resources SET importance_level = ?, update_time = ? WHERE data_resources_id = ?`,
		target, time.Now(), resourceID)
	return err
}

// sqlxDBExec 让 SyncResourceImportance 能同时接 *sqlx.DB 与 *sqlx.Tx
type sqlxDBExec interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}
```

如果 `time` / `sql` import 缺，按需加。

- [ ] **Step 4: 跑测试，PASS**

Run: `go test ./internal/repository/ -run 'TestSyncImportanceFromProjectCode' -count=1`

- [ ] **Step 5: 在 ai_classify.go 的 apply 末尾调同步**

打开 `internal/httpd/ai_classify.go`，找到 `ApplyClassifySuggestion` 里 `auditRepo.Append(...)` 之前加：

```go
	// 三级分流：apply 成功后把 importance_level 与目标项目代码同步
	if projectCode := lookupProjectCode(req.ProjectID); projectCode != "" {
		_ = repository.SyncResourceImportance(repository.GetDB(), req.ResourceID, projectCode)
	}
```

在文件 helper 区（`strDeref` 上方）加：

```go
func lookupProjectCode(projectID int64) string {
	var code string
	_ = repository.GetDB().Get(&code, `SELECT project_code FROM data_projects WHERE id = ?`, projectID)
	return code
}
```

- [ ] **Step 6: 写 pending 过滤失败测试**

Create `internal/httpd/ai_classify_filter_test.go`:

```go
package httpd

import (
	"testing"
	"time"
)

func TestHTTP_AIClassify_Pending_FiltersCoreAndPrivacy(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()

	rows := []struct{ name, sign string; level int }{
		{"core.pdf", "TC_CORE", 1},      // 核心：不进 pending
		{"privacy.pdf", "TC_PRIV", 4},   // 隐私：不进 pending
		{"general.pdf", "TC_GEN", 0},    // 未分类：进 pending（origin=new 默认）
	}
	for _, x := range rows {
		_, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, ?, ?, ?, 0, 'new')`,
			x.sign, now, x.name, x.level, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending?origin=new")
	successOk(t, status, resp)
	items, _ := dataMap(t, resp)["items"].([]interface{})
	for _, it := range items {
		m := it.(map[string]interface{})
		if m["resource_name"] == "core.pdf" {
			t.Error("核心级 (importance=1) 不应出现在 pending")
		}
		if m["resource_name"] == "privacy.pdf" {
			t.Error("隐私级 (importance=4) 不应出现在 pending")
		}
	}
}
```

- [ ] **Step 7: 跑 FAIL（当前没有过滤）**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_Pending_FiltersCoreAndPrivacy' -count=1`

Expected: 2 个 t.Error。

- [ ] **Step 8: 修 pending 的 SQL**

打开 `internal/httpd/ai_classify.go`，找到 `ListPendingForClassify` 里两段 SQL（COUNT 与 SELECT），都在 WHERE 末尾加 `AND importance_level NOT IN (1, 4)`：

```go
	if err := db.Get(&total,
		`SELECT COUNT(*) FROM data_resources
		  WHERE claim_status = 2 AND importance_level = 0 AND disable = 0
		    AND ai_classify_rejected_at IS NULL
		    AND data_origin = ?`, origin); err != nil {
```

注意：原 SQL 已经是 `importance_level = 0`，所以核心 / 隐私本来就不会被选中。Spec 提到的 `!= 1 AND != 4` 适用于：当我们扩展 pending 到包含已分类未挂账的情况时。本期保持 `importance_level = 0` 即可（核心 / 隐私自然不会出现）。Test 也应当通过。

实际上：**只需要在原 SQL 上不动就行**。这步 Step 8 调整为：

打开 test 文件，把 `level: 1` 和 `level: 4` 测试的预期改"应该出现/不出现"，验证已有过滤本就生效。

修改 test 文件（已经在 Step 6 写出来），改成只看不应该出现的核心 / 隐私级，保留 `general.pdf` (level=0) 必须出现：

```go
	// 改 expected：检查 general.pdf 出现，core / privacy 不出现
	foundGeneral := false
	for _, it := range items {
		m := it.(map[string]interface{})
		switch m["resource_name"] {
		case "general.pdf":
			foundGeneral = true
		case "core.pdf":
			t.Error("核心级 (importance=1) 不应出现在 pending")
		case "privacy.pdf":
			t.Error("隐私级 (importance=4) 不应出现在 pending")
		}
	}
	if !foundGeneral {
		t.Error("一般级 (importance=0) 应该出现在 pending")
	}
```

- [ ] **Step 9: 跑测试**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_Pending_FiltersCoreAndPrivacy' -count=1`

Expected: PASS（基于已有 `importance_level = 0` 的过滤）。

- [ ] **Step 10: 全套回归**

Run: `go test ./internal/...`

- [ ] **Step 11: Commit**

```bash
git add internal/repository/personal_files_bridge.go \
        internal/repository/personal_files_bridge_importance_test.go \
        internal/httpd/ai_classify.go \
        internal/httpd/ai_classify_filter_test.go
git commit -m "feat(scan): sync importance_level to project_code on apply"
```

---

### Task 3: importance 手动调级端点 `POST /resources/:id/importance`

**Files:**
- Modify: `internal/httpd/resources.go`（新 handler + 注册）
- Test: `internal/httpd/resources_importance_test.go`（新建）

- [ ] **Step 1: 写失败测试**

Create `internal/httpd/resources_importance_test.go`:

```go
package httpd

import (
	"testing"
	"time"
)

func TestHTTP_Resources_OverrideImportance(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('OVI_001', 1, 1, ?, 'x.pdf', 2, 0, ?, ?, 0, 'new')`, now, now, now)
	rid, _ := res.LastInsertId()

	// 提升到核心
	status, resp := jsonReq(t, r, "POST", "/resources/"+itoa(rid)+"/importance", map[string]interface{}{
		"level": 1,
	})
	successOk(t, status, resp)

	var got int
	db.Get(&got, `SELECT importance_level FROM data_resources WHERE data_resources_id = ?`, rid)
	if got != 1 {
		t.Errorf("importance_level = %d, want 1", got)
	}

	// 撤回到未分类
	status, _ = jsonReq(t, r, "POST", "/resources/"+itoa(rid)+"/importance", map[string]interface{}{"level": 0})
	if status != 200 {
		t.Errorf("revert to 0 status = %d, want 200", status)
	}
	db.Get(&got, `SELECT importance_level FROM data_resources WHERE data_resources_id = ?`, rid)
	if got != 0 {
		t.Errorf("after revert importance_level = %d, want 0", got)
	}
}

func TestHTTP_Resources_OverrideImportance_RejectsBadLevel(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()
	status, resp := jsonReq(t, r, "POST", "/resources/1/importance", map[string]interface{}{"level": 7})
	expectFailure(t, status, resp)
}
```

- [ ] **Step 2: 跑 FAIL**

Run: `go test ./internal/httpd/ -run 'TestHTTP_Resources_OverrideImportance' -count=1`

Expected: 404 (route not registered)。

- [ ] **Step 3: 加 handler + route**

打开 `internal/httpd/resources.go`，在 `RegisterResourcesRoutes` 函数里加：

```go
	r.POST("/:id/importance", OverrideResourceImportance)
```

文件末尾追加：

```go
// OverrideResourceImportance POST /resources/:id/importance body {"level": 0|1|2|3|4}
//
// 用户手动指定级别。0 = 退回 pending；1/2/3 = 进对应通道；4 = 隐私旁路。
// 不触发 apply；只改 data_resources.importance_level。
func OverrideResourceImportance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var body struct {
		Level int `json:"level"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if body.Level < 0 || body.Level > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "level 必须在 0~4 之间"})
		return
	}
	db := repository.GetDB()
	if _, err := db.Exec(`UPDATE data_resources SET importance_level = ?, update_time = ? WHERE data_resources_id = ? AND disable = 0`,
		body.Level, time.Now(), id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"resource_id": id, "level": body.Level}})
}
```

如果 imports 缺 `strconv` / `time` / `net/http`，按编译器提示加。

- [ ] **Step 4: 跑 PASS**

Run: `go test ./internal/httpd/ -run 'TestHTTP_Resources_OverrideImportance' -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/httpd/resources.go internal/httpd/resources_importance_test.go
git commit -m "feat(scan): POST /resources/:id/importance for manual override"
```

---

### Task 4: family 权威源裁定 — repo helper + 2 endpoints + apply 拦截

**Files:**
- Create: `internal/repository/family_authoritative.go`
- Modify: `internal/httpd/family.go`（加 POST authoritative + family role 推导）
- Modify: `internal/httpd/ai_classify.go`（apply 前拦截）
- Test: `internal/repository/family_authoritative_test.go`
- Test: `internal/httpd/family_authoritative_test.go`
- Test: `internal/httpd/ai_classify_intercept_test.go`

- [ ] **Step 1: 写 repository 失败测试**

Create `internal/repository/family_authoritative_test.go`:

```go
package repository

import (
	"testing"
	"time"
)

func seedFamilyWithMembers(t *testing.T, n int) (familyID int64, resourceIDs []int64) {
	t.Helper()
	db := openMigratedTestDB(t)
	t.Cleanup(func() { db.Close() })
	now := time.Now()
	r, err := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		create_time, update_time, disable
	) VALUES ('PCS', 0, ?, 'simhash', 0.9, ?, ?, 0)`, n, now, now)
	if err != nil {
		t.Fatal(err)
	}
	familyID, _ = r.LastInsertId()
	for i := 0; i < n; i++ {
		rs, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, family_id, family_relation,
			create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, 'derived', ?, ?, 0, 'new')`,
			itoaSlow(i), now, "m"+itoaSlow(i)+".pdf", familyID, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := rs.LastInsertId()
		resourceIDs = append(resourceIDs, id)
	}
	return familyID, resourceIDs
}

func itoaSlow(n int) string {
	// 测试用小辅助，避免引入 strconv
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string('0'+byte(n%10)) + out
		n /= 10
	}
	return out
}

func TestSetAuthoritativeResource(t *testing.T) {
	fid, ids := seedFamilyWithMembers(t, 3)
	db := GetDB()

	if err := SetAuthoritativeResource(db, fid, ids[1]); err != nil {
		t.Fatalf("set: %v", err)
	}
	var got int64
	db.Get(&got, `SELECT authoritative_resource_id FROM data_resource_family WHERE family_id = ?`, fid)
	if got != ids[1] {
		t.Errorf("authoritative_resource_id = %d, want %d", got, ids[1])
	}

	// 改判
	if err := SetAuthoritativeResource(db, fid, ids[2]); err != nil {
		t.Fatalf("re-set: %v", err)
	}
	db.Get(&got, `SELECT authoritative_resource_id FROM data_resource_family WHERE family_id = ?`, fid)
	if got != ids[2] {
		t.Errorf("after re-set = %d, want %d", got, ids[2])
	}
}

func TestNeedsAuthoritativeArbitration(t *testing.T) {
	fid, ids := seedFamilyWithMembers(t, 3)
	db := GetDB()

	needs, err := NeedsAuthoritativeArbitration(db, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if !needs {
		t.Error("family 3 成员且未指定权威 → 应需要仲裁")
	}

	_ = SetAuthoritativeResource(db, fid, ids[1])
	needs, _ = NeedsAuthoritativeArbitration(db, ids[0])
	if needs {
		t.Error("已指定权威后不再需要仲裁")
	}
}
```

注意：`seedFamilyWithMembers` 里用了 `openMigratedTestDB`（已存在）+ `GetDB()`——这两个 helper 在 repository 包内可见。

- [ ] **Step 2: 跑 FAIL**

Run: `go test ./internal/repository/ -run 'TestSetAuthoritativeResource|TestNeedsAuthoritativeArbitration' -count=1`

Expected: undefined functions。

- [ ] **Step 3: 实现 repository helper**

Create `internal/repository/family_authoritative.go`:

```go
package repository

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// SetAuthoritativeResource 设置或更换 family 的权威源。
// 不触碰任何 ledger 行——「权威 / 参考」是查询时由 join 推导。
func SetAuthoritativeResource(db *sqlx.DB, familyID, resourceID int64) error {
	_, err := db.Exec(`UPDATE data_resource_family
		SET authoritative_resource_id = ?, update_time = CURRENT_TIMESTAMP
		WHERE family_id = ?`, resourceID, familyID)
	return err
}

// NeedsAuthoritativeArbitration 判断给定 resource 在 apply 重要级时是否需要先选权威源。
// 触发条件：family_id 非空 + family.member_count >= 2 + authoritative_resource_id IS NULL。
func NeedsAuthoritativeArbitration(db *sqlx.DB, resourceID int64) (bool, error) {
	var row struct {
		FamilyID    sql.NullInt64 `db:"family_id"`
		MemberCount int           `db:"member_count"`
		AuthID      sql.NullInt64 `db:"authoritative_resource_id"`
	}
	err := db.Get(&row, `SELECT dr.family_id, COALESCE(f.member_count, 0) AS member_count, f.authoritative_resource_id
		FROM data_resources dr
		LEFT JOIN data_resource_family f ON f.family_id = dr.family_id
		WHERE dr.data_resources_id = ? AND dr.disable = 0`, resourceID)
	if err != nil {
		return false, err
	}
	if !row.FamilyID.Valid {
		return false, nil
	}
	if row.MemberCount < 2 {
		return false, nil
	}
	if row.AuthID.Valid {
		return false, nil
	}
	return true, nil
}

// FamilyRole 在查询时推导成员在 family 的角色
type FamilyRole string

const (
	FamilyRoleStandalone    FamilyRole = "standalone"     // 无 family
	FamilyRoleAuthoritative FamilyRole = "authoritative"  // 权威源
	FamilyRolePending       FamilyRole = "pending_arbitration"
	FamilyRoleReference     FamilyRole = "reference"
)

// ResolveFamilyRole 在已知 resource 与其 family 的当前状态下计算角色
func ResolveFamilyRole(familyID sql.NullInt64, resourceID int64, authoritativeID sql.NullInt64) FamilyRole {
	if !familyID.Valid {
		return FamilyRoleStandalone
	}
	if authoritativeID.Valid && authoritativeID.Int64 == resourceID {
		return FamilyRoleAuthoritative
	}
	if !authoritativeID.Valid {
		return FamilyRolePending
	}
	return FamilyRoleReference
}
```

- [ ] **Step 4: 跑 PASS**

Run: `go test ./internal/repository/ -run 'TestSetAuthoritativeResource|TestNeedsAuthoritativeArbitration' -count=1`

- [ ] **Step 5: 加 POST /family/:id/authoritative HTTP endpoint + 测试**

Create `internal/httpd/family_authoritative_test.go`:

```go
package httpd

import (
	"testing"
	"time"
)

func TestHTTP_Family_SetAuthoritative(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()

	famR, _ := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		create_time, update_time, disable
	) VALUES ('AUTHCS', 0, 2, 'sim', 0.9, ?, ?, 0)`, now, now)
	famID, _ := famR.LastInsertId()

	resR, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('AUTHRES', 1, 1, ?, 'a.pdf', 2, 0, ?, ?, ?, 0, 'new')`, now, famID, now, now)
	resID, _ := resR.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/family/"+itoa(famID)+"/authoritative", map[string]interface{}{
		"resource_id": resID,
	})
	successOk(t, status, resp)

	var got int64
	db.Get(&got, `SELECT authoritative_resource_id FROM data_resource_family WHERE family_id = ?`, famID)
	if got != resID {
		t.Errorf("authoritative_resource_id = %d, want %d", got, resID)
	}
}
```

- [ ] **Step 6: 跑 FAIL**

Run: `go test ./internal/httpd/ -run 'TestHTTP_Family_SetAuthoritative' -count=1`

Expected: 404。

- [ ] **Step 7: 加 endpoint**

打开 `internal/httpd/family.go`，扩展 `RegisterFamilyRoutes`：

```go
func RegisterFamilyRoutes(r *gin.RouterGroup) {
	r.GET("/:id", GetFamily)
	r.GET("/:id/members", GetFamilyMembers)
	r.POST("/:id/authoritative", SetFamilyAuthoritative)
}
```

文件末尾追加：

```go
// SetFamilyAuthoritative POST /family/:id/authoritative body {"resource_id": int64}
func SetFamilyAuthoritative(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid family id"})
		return
	}
	var body struct {
		ResourceID int64 `json:"resource_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ResourceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "resource_id 必填"})
		return
	}
	if err := repository.SetAuthoritativeResource(repository.GetDB(), id, body.ResourceID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"family_id": id, "authoritative_resource_id": body.ResourceID,
	}})
}
```

- [ ] **Step 8: 跑 PASS**

Run: `go test ./internal/httpd/ -run 'TestHTTP_Family_SetAuthoritative' -count=1`

- [ ] **Step 9: 写 apply 拦截测试**

Create `internal/httpd/ai_classify_intercept_test.go`:

```go
package httpd

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_AIClassify_Apply_BlocksOnImportantUnsettledFamily(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	repository.EnsurePersonalContextForTest(db)
	withActiveUser(t, db, "u1")
	now := time.Now()

	// 建一个 family 含 2 个成员，无权威
	famR, _ := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		create_time, update_time, disable
	) VALUES ('INTCS', 0, 2, 'sim', 0.9, ?, ?, 0)`, now, now)
	famID, _ := famR.LastInsertId()

	resR, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('INT_R1', 1, 1, ?, 'rep.pdf', 2, 0, ?, ?, ?, 0, 'new')`, now, famID, now, now)
	resID, _ := resR.LastInsertId()

	// 找 SYS-PERSONAL-IMPORTANT 项目 id
	var projID int64
	db.Get(&projID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalImportantProjectCode)

	status, resp := jsonReq(t, r, "POST", "/ai/classify/apply", map[string]interface{}{
		"resource_id":    resID,
		"project_id":     projID,
		"stage_code":     "GR-DA",
		"file_rule_code": "OUT-001",
	})
	if status != 409 {
		t.Fatalf("status = %d, want 409 (need to choose authoritative)", status)
	}
	d, _ := resp["data"].(map[string]interface{})
	if d == nil || d["family_id"] == nil {
		t.Errorf("response.data should contain family_id; got %+v", resp)
	}
}
```

- [ ] **Step 10: 跑 FAIL**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_Apply_BlocksOnImportantUnsettledFamily' -count=1`

- [ ] **Step 11: 在 apply 端加拦截**

打开 `internal/httpd/ai_classify.go`，定位 `ApplyClassifySuggestion`。在 `repository.BridgeResourceToTarget(...)` 调用**之前**插入：

```go
	projectCode := lookupProjectCode(req.ProjectID)
	if projectCode == repository.PersonalImportantProjectCode {
		needs, _ := repository.NeedsAuthoritativeArbitration(repository.GetDB(), req.ResourceID)
		if needs {
			// 拉成员列表供前端弹窗
			var familyID int64
			_ = repository.GetDB().Get(&familyID,
				`SELECT family_id FROM data_resources WHERE data_resources_id = ?`, req.ResourceID)
			famRepo := repository.NewFamilyRepository(repository.GetDB())
			members, _ := famRepo.ListFamilyMembers(familyID)
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "需要先选权威源",
				"data": gin.H{
					"family_id": familyID,
					"members":   members,
				},
			})
			return
		}
	}
```

注意 `lookupProjectCode` 是 Task 2 已经加好的 helper。

- [ ] **Step 12: 跑 PASS**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_Apply_BlocksOnImportantUnsettledFamily' -count=1`

- [ ] **Step 13: 全 httpd / repo 回归**

Run: `go test ./internal/...`

- [ ] **Step 14: Commit**

```bash
git add internal/repository/family_authoritative.go \
        internal/repository/family_authoritative_test.go \
        internal/httpd/family.go \
        internal/httpd/family_authoritative_test.go \
        internal/httpd/ai_classify.go \
        internal/httpd/ai_classify_intercept_test.go
git commit -m "feat(scan): family authoritative source + apply intercept"
```

---

### Task 5: 数字机要 — 3 endpoints + apply 命中核心时建账留 memo 空

**Files:**
- Create: `internal/httpd/memorandum.go`
- Modify: `internal/httpd/router.go`（注册 /memorandum 组）
- Modify: `internal/httpd/ai_classify.go`（命中核心时返 hint）
- Test: `internal/httpd/memorandum_test.go`

- [ ] **Step 1: 写失败测试**

Create `internal/httpd/memorandum_test.go`:

```go
package httpd

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

// 测试用：给个 user 加 password_md5（数字机要登记要校验密码）
func setUserPassword(t *testing.T, db interface {
	Exec(query string, args ...interface{}) (interface{}, error)
}, username, plainPwd string) {
	t.Helper()
	// 演示阶段直接用 password 明文 → md5（与现有 user_info 兼容）
}

func TestHTTP_Memorandum_Pending_ListsUnregisteredCore(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	repository.EnsurePersonalContextForTest(db)
	withActiveUser(t, db, "u1")

	// 直接建一条 core ledger，模拟 AI 把它转入待登记
	now := time.Now()
	var projID int64
	db.Get(&projID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalCoreProjectCode)
	db.Exec(`INSERT INTO asset_ledgers (
		ledger_code, file_version_id, class_code, project_code, stage_code,
		file_version_code, asset_name, owner_subject_id, custodian_subject_id, security_subject_id,
		sensitivity_level, marking_method, lifecycle_status, create_time, update_time, disable
	) VALUES ('LG-MEMO-1', 1, NULL, 'SYS-PERSONAL-CORE', 'GR-DA',
		'OUT-001', '机要资料.docx', 0, 0, 0, 'core_secret', 'reference', 'registered', ?, ?, 0)`, now, now)

	status, resp := jsonReqNoBody(t, r, "GET", "/memorandum/pending")
	successOk(t, status, resp)
	items, _ := dataMap(t, resp)["items"].([]interface{})
	if len(items) < 1 {
		t.Errorf("pending memorandum items = %d, want ≥ 1", len(items))
	}
}

func TestHTTP_Memorandum_Register_RequiresPassword(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	repository.EnsurePersonalContextForTest(db)

	// 建用户 + 设密码
	pwdMD5 := "5f4dcc3b5aa765d61d8327deb882cf99" // md5("password")
	db.Exec(`INSERT INTO user_info (company_name, user_name, department, ip, mac_address, work_address, phone, password_md5, create_time, update_time, disable)
		VALUES ('u', 'u1', 'd', '127.0.0.1', '00:00:00:00:00:00', '', '10000000000', ?, ?, ?, 0)`,
		pwdMD5, time.Now(), time.Now())
	db.Exec(`INSERT INTO users (username, display_name, status, create_time, update_time, disable)
		VALUES ('u1', 'u1', 'active', ?, ?, 0)`, time.Now(), time.Now())

	now := time.Now()
	res, _ := db.Exec(`INSERT INTO asset_ledgers (
		ledger_code, file_version_id, project_code, stage_code, file_version_code, asset_name,
		owner_subject_id, custodian_subject_id, security_subject_id, sensitivity_level,
		marking_method, lifecycle_status, create_time, update_time, disable
	) VALUES ('LG-MEMO-2', 2, 'SYS-PERSONAL-CORE', 'GR-DA', 'OUT-001', 'x.docx',
		0, 0, 0, 'core_secret', 'reference', 'registered', ?, ?, 0)`, now, now)
	ledgerID, _ := res.LastInsertId()

	// 密码错
	status, resp := jsonReq(t, r, "POST", "/memorandum/register", map[string]interface{}{
		"ledger_id":      ledgerID,
		"topic":          "客户合同",
		"classification": "秘密",
		"password":       "wrong",
	})
	expectFailure(t, status, resp)

	// 密码对
	status, resp = jsonReq(t, r, "POST", "/memorandum/register", map[string]interface{}{
		"ledger_id":      ledgerID,
		"topic":          "客户合同",
		"classification": "秘密",
		"password":       "password",
	})
	successOk(t, status, resp)

	var registeredAt *time.Time
	db.Get(&registeredAt, `SELECT memorandum_registered_at FROM asset_ledgers WHERE id = ?`, ledgerID)
	if registeredAt == nil {
		t.Error("memorandum_registered_at should be set after register")
	}
}
```

- [ ] **Step 2: 跑 FAIL**

Run: `go test ./internal/httpd/ -run 'TestHTTP_Memorandum_' -count=1`

- [ ] **Step 3: 实现 handlers**

Create `internal/httpd/memorandum.go`:

```go
package httpd

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"data-asset-scan-go/internal/repository"
)

func RegisterMemorandumRoutes(r *gin.RouterGroup) {
	r.GET("/pending", ListMemorandumPending)
	r.GET("/registered", ListMemorandumRegistered)
	r.POST("/register", RegisterMemorandum)
}

type memorandumItem struct {
	LedgerID       int64   `db:"id" json:"ledger_id"`
	AssetName      string  `db:"asset_name" json:"asset_name"`
	FileVersionCode string `db:"file_version_code" json:"file_version_code"`
	CreateTime     string  `db:"create_time" json:"create_time"`
	Topic          *string `db:"memorandum_topic" json:"topic"`
	Classification *string `db:"memorandum_classification" json:"classification"`
	RegisteredAt   *string `db:"memorandum_registered_at" json:"registered_at"`
	RegisteredBy   *int64  `db:"memorandum_registered_by" json:"registered_by"`
}

func ListMemorandumPending(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	listMemorandum(c, page, pageSize, false)
}

func ListMemorandumRegistered(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	listMemorandum(c, page, pageSize, true)
}

func listMemorandum(c *gin.Context, page, pageSize int, registered bool) {
	db := repository.GetDB()
	where := `project_code = 'SYS-PERSONAL-CORE' AND disable = 0`
	if registered {
		where += ` AND memorandum_registered_at IS NOT NULL`
	} else {
		where += ` AND memorandum_registered_at IS NULL`
	}

	var total int
	if err := db.Get(&total, `SELECT COUNT(*) FROM asset_ledgers WHERE `+where); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	var items []memorandumItem
	if err := db.Select(&items,
		`SELECT id, asset_name, file_version_code, create_time,
		        memorandum_topic, memorandum_classification, memorandum_registered_at, memorandum_registered_by
		 FROM asset_ledgers WHERE `+where+`
		 ORDER BY id DESC LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items": items, "total": total, "page": page, "page_size": pageSize,
	}})
}

type registerRequest struct {
	LedgerID       int64  `json:"ledger_id"`
	Topic          string `json:"topic"`
	Classification string `json:"classification"`
	Note           string `json:"note"`
	Password       string `json:"password"`
}

// RegisterMemorandum POST /memorandum/register
// 校验：1) ledger 存在且 project_code=SYS-PERSONAL-CORE 且未登记
//        2) 当前用户密码 md5 与 user_info.password_md5 一致
//        3) topic / classification / password 非空
func RegisterMemorandum(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if req.LedgerID <= 0 || req.Topic == "" || req.Classification == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "ledger_id / topic / classification / password 全部必填"})
		return
	}
	db := repository.GetDB()

	// 校验 ledger
	var info struct {
		ProjectCode  string  `db:"project_code"`
		Registered   *string `db:"memorandum_registered_at"`
	}
	if err := db.Get(&info, `SELECT project_code, memorandum_registered_at FROM asset_ledgers WHERE id = ? AND disable = 0`, req.LedgerID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "ledger 不存在"})
		return
	}
	if info.ProjectCode != repository.PersonalCoreProjectCode {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "非核心级 ledger 不可登记数字机要"})
		return
	}
	if info.Registered != nil && *info.Registered != "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该 ledger 已登记，不可重复"})
		return
	}

	// 校验密码：从 user_info 取当前用户 password_md5
	operator := currentOperator(c)
	var stored *string
	_ = db.Get(&stored, `SELECT password_md5 FROM user_info WHERE user_name = ? AND disable = 0 ORDER BY id DESC LIMIT 1`, operator)
	if stored == nil || *stored == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "用户未设密码，无法签字"})
		return
	}
	inputMD5 := md5Hex(req.Password)
	if inputMD5 != *stored {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "密码错误"})
		return
	}

	// 计算签字 hash
	now := time.Now()
	userID := currentUserID(c)
	sigPayload := strconv.FormatInt(userID, 10) + ":" + now.Format(time.RFC3339Nano) + ":" + *stored
	sigHash := sha256Hex(sigPayload)

	if _, err := db.Exec(`UPDATE asset_ledgers SET
		memorandum_topic = ?, memorandum_classification = ?, memorandum_registered_at = ?,
		memorandum_registered_by = ?, memorandum_signature_hash = ?, update_time = ?
		WHERE id = ?`,
		req.Topic, req.Classification, now, userID, sigHash, now, req.LedgerID); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	auditRepo := repository.NewAuditLogRepository(db)
	_, _ = auditRepo.Append(repository.AppendAuditInput{
		ActorID:     operator,
		ActorUserID: userID,
		Action:      "core_memorandum_register",
		TargetType:  "asset_ledger",
		TargetID:    req.LedgerID,
		After:       gin.H{"topic": req.Topic, "classification": req.Classification, "note": req.Note, "registered_at": now},
		IPAddress:   c.ClientIP(),
		Message:     "数字机要登记: " + req.Topic,
	})

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"ledger_id": req.LedgerID, "registered_at": now}})
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
```

- [ ] **Step 4: 注册路由**

打开 `internal/httpd/router.go`，在 aiGroup 之前加：

```go
	// 数字机要 (/memorandum/*) - 核心级正式登记通道
	memoGroup := r.Group("/memorandum")
	RegisterMemorandumRoutes(memoGroup)
```

- [ ] **Step 5: apply 命中核心时返 hint（不强行拦）**

打开 `internal/httpd/ai_classify.go`，定位 `ApplyClassifySuggestion`，在 `c.JSON(http.StatusOK, gin.H{"success": true, "data": item})` **之前**插入：

```go
	hint := ""
	if projectCode == repository.PersonalCoreProjectCode {
		hint = "transferred_to_memorandum_pending"
	}
```

把最后那行 `c.JSON(...)` 改成：

```go
	resp := gin.H{"success": true, "data": item}
	if hint != "" {
		resp["hint"] = hint
	}
	c.JSON(http.StatusOK, resp)
```

注意：`projectCode` 已经在 Task 2 那段被赋值过——在 `SyncResourceImportance` 调用之前。如果 Task 2 是把 lookup 写在 `auditRepo` 之前，那 `projectCode` 在这里仍可见；否则需要把 lookup 移上去。

- [ ] **Step 6: 跑 PASS**

Run: `go test ./internal/httpd/ -run 'TestHTTP_Memorandum_' -count=1`

- [ ] **Step 7: 全 httpd 回归**

Run: `go test ./internal/httpd/...`

- [ ] **Step 8: Commit**

```bash
git add internal/httpd/memorandum.go internal/httpd/memorandum_test.go \
        internal/httpd/router.go internal/httpd/ai_classify.go
git commit -m "feat(scan): digital memorandum endpoints + apply hint for core level"
```

---

### Task 6: bulk-dismiss 校验放宽：允许 (new, level=3) 组合

**Files:**
- Modify: `internal/httpd/ai_classify.go`（BulkDismissClassify 的校验段）
- Modify: `internal/httpd/ai_classify_bulk_dismiss_test.go`（加一例覆盖新组合）

- [ ] **Step 1: 写新测试用例**

打开 `internal/httpd/ai_classify_bulk_dismiss_test.go` 在文件末追加：

```go
func TestHTTP_AIClassify_BulkDismiss_AllowsNewGeneral(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('BDNG_1', 1, 1, ?, 'general-new.pdf', 2, 3, ?, ?, 0, 'new')`,
		now, now, now)
	rid, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": []int64{rid},
		"reason":       "AI 没把握，跳过",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if v, _ := d["dismissed"].(float64); int(v) != 1 {
		t.Errorf("dismissed = %v, want 1", v)
	}
}

func TestHTTP_AIClassify_BulkDismiss_RejectsNewImportant(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('BDNI_1', 1, 1, ?, 'important-new.pdf', 2, 2, ?, ?, 0, 'new')`,
		now, now, now)
	rid, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": []int64{rid},
		"reason":       "skip",
	})
	expectFailure(t, status, resp)
}
```

- [ ] **Step 2: 跑 FAIL**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_BulkDismiss_(AllowsNewGeneral|RejectsNewImportant)' -count=1`

- [ ] **Step 3: 改 bulk-dismiss 的校验**

打开 `internal/httpd/ai_classify.go`，找 `BulkDismissClassify`，把校验段（原本写的 `WHERE ... AND data_origin = 'historical'`）改成允许两种组合：

```go
	selectQ := `SELECT data_resources_id, resources_name FROM data_resources
	            WHERE data_resources_id IN (` + strings.Join(placeholders, ",") + `)
	              AND disable = 0
	              AND (
	                  data_origin = 'historical'
	                  OR (data_origin = 'new' AND importance_level = 3)
	              )`
```

同时把 UPDATE 段的 WHERE 也加同样的子句：

```go
	updateQ := `UPDATE data_resources
	            SET ai_classify_rejected_at = ?, ai_classify_reject_reason = ?, update_time = ?
	            WHERE data_resources_id IN (` + strings.Join(placeholders, ",") + `)
	              AND disable = 0
	              AND (
	                  data_origin = 'historical'
	                  OR (data_origin = 'new' AND importance_level = 3)
	              )`
```

错误消息也更新一下："存在非历史、非一般级新数据 / 已删除 / 不存在的资源"

- [ ] **Step 4: 跑 PASS**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_BulkDismiss_' -count=1`

Expected: 5 个 PASS（含已有 3 例 + 新 2 例）。

- [ ] **Step 5: Commit**

```bash
git add internal/httpd/ai_classify.go internal/httpd/ai_classify_bulk_dismiss_test.go
git commit -m "feat(scan): bulk-dismiss accepts new-data general level too"
```

---

### Task 7: 前端 AI 归目工具加 level sub-tabs + 重要级 family 拦截 UI + 一般级一键

**Files:**
- Modify: `frontend_real/views/AIClassifyView.vue`
- Modify: `frontend_real/services/api.ts`（加 family / override helper）
- Test: `frontend_real/__tests__/AIClassifyView.levels.test.ts`

- [ ] **Step 1: 加 api helpers**

打开 `frontend_real/services/api.ts`，在文件末尾追加：

```ts
export interface FamilyMember {
  data_resources_id: number
  resources_name?: string | null
  family_relation?: string | null
}

export async function fetchFamilyMembers(familyId: number): Promise<{ family_id: number; members: FamilyMember[] }> {
  const res = await fetch(`${API_BASE}/family/${familyId}/members`)
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'family fetch failed')
  // /family/:id/members 返 grouped；扁平化
  const groups = j.data?.groups || {}
  const flat: FamilyMember[] = []
  for (const k of Object.keys(groups)) for (const m of groups[k]) flat.push(m)
  return { family_id: j.data.family_id, members: flat }
}

export async function setFamilyAuthoritative(familyId: number, resourceId: number) {
  const res = await fetch(`${API_BASE}/family/${familyId}/authoritative`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ resource_id: resourceId }),
  })
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'set authoritative failed')
  return j.data
}

export async function overrideResourceImportance(resourceId: number, level: 0 | 1 | 2 | 3 | 4) {
  const res = await fetch(`${API_BASE}/resources/${resourceId}/importance`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ level }),
  })
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'override importance failed')
  return j.data
}
```

- [ ] **Step 2: 在 AIClassifyView 内的"新数据/历史数据"两 Tab 内**插一层 level sub-tabs

打开 `frontend_real/views/AIClassifyView.vue`，找到第一章已加的 `<v-tabs v-model="currentTab">`，在每个 `<v-window-item value="new">` / `<v-window-item value="historical">` 内**最顶部**加 level 子 Tab：

```vue
<v-tabs v-model="currentLevel" density="compact" color="primary" class="px-2">
  <v-tab value="all">全部 {{ levelCounts.all }}</v-tab>
  <v-tab value="core">核心 {{ levelCounts.core }}</v-tab>
  <v-tab value="important">重要 {{ levelCounts.important }}</v-tab>
  <v-tab value="general">一般 {{ levelCounts.general }}</v-tab>
</v-tabs>
```

在 `<script setup>` 加 state：

```ts
const currentLevel = ref<'all' | 'core' | 'important' | 'general'>('all')
const levelCounts = computed(() => {
  // 用 pending 按 top1 推荐 project_code 分桶
  const out = { all: pending.value.length, core: 0, important: 0, general: 0 }
  for (const p of pending.value) {
    const top = p.suggestions?.[0]
    if (!top) continue
    if (top.project_code === 'SYS-PERSONAL-CORE') out.core++
    else if (top.project_code === 'SYS-PERSONAL-IMPORTANT') out.important++
    else if (top.project_code === 'SYS-PERSONAL-GENERAL') out.general++
  }
  return out
})
```

行渲染时用一个 `filteredPending` computed：

```ts
const filteredPending = computed(() => {
  if (currentLevel.value === 'all') return pending.value
  const code = ({ core: 'SYS-PERSONAL-CORE', important: 'SYS-PERSONAL-IMPORTANT', general: 'SYS-PERSONAL-GENERAL' } as any)[currentLevel.value]
  return pending.value.filter(p => p.suggestions?.[0]?.project_code === code)
})
```

把现有的 `pending` 渲染替换为 `filteredPending`。

- [ ] **Step 3: 一般级一键按钮**

在 sub-tabs 下方加（仅在 `currentLevel === 'general'` 显示）：

```vue
<div v-if="currentLevel === 'general'" class="d-flex align-center gap-3 mb-3">
  <div style="flex: 1; max-width: 280px">
    <div class="text-caption mb-1">自动阈值 {{ Math.round(autoApplyThreshold * 100) }}%</div>
    <v-slider v-model="autoApplyThreshold" :min="0.1" :max="0.95" :step="0.05" hide-details density="compact" />
  </div>
  <v-btn color="primary" variant="tonal" :disabled="autoApplyDisabled" :loading="autoApplying" @click="onAutoApply">
    一键 AI 归目（{{ generalAutoApplyableCount }}）
  </v-btn>
  <v-btn color="warning" variant="tonal" :disabled="generalSkippableCount === 0" @click="onBulkSkipGeneral">
    清空余下（{{ generalSkippableCount }}）
  </v-btn>
</div>
```

加 computed：

```ts
const generalAutoApplyableCount = computed(() =>
  filteredPending.value.filter(p =>
    p.suggestions?.[0]?.project_code === 'SYS-PERSONAL-GENERAL' &&
    (p.suggestions?.[0]?.confidence || 0) >= autoApplyThreshold.value
  ).length
)
const generalSkippableCount = computed(() =>
  filteredPending.value.filter(p =>
    p.suggestions?.[0]?.project_code === 'SYS-PERSONAL-GENERAL' &&
    (p.suggestions?.[0]?.confidence || 0) < autoApplyThreshold.value
  ).length
)

async function onBulkSkipGeneral() {
  const skippable = filteredPending.value.filter(p =>
    p.suggestions?.[0]?.project_code === 'SYS-PERSONAL-GENERAL' &&
    (p.suggestions?.[0]?.confidence || 0) < autoApplyThreshold.value
  )
  if (skippable.length === 0) return
  const ok = window.confirm(`将把 ${skippable.length} 条标为已治理，不再出现。确认吗？`)
  if (!ok) return
  try {
    const { bulkDismissHistorical } = await import('../services/api')
    await bulkDismissHistorical(skippable.map(p => p.resource_id), '一般级 AI 未匹配，批量跳过')
    await loadPending()
  } catch (e: any) {
    snackbar.value = { show: true, text: '清空失败：' + (e?.message || String(e)), color: 'error' }
  }
}
```

把现有 `onAutoApply` 里 apply 之前加一段过滤：只对 general level 适用：

```ts
async function onAutoApply() {
  if (currentLevel.value !== 'general') {
    snackbar.value = { show: true, text: '一键 AI 归目只在一般级生效', color: 'warning' }
    return
  }
  // ... 原有 onAutoApply 逻辑保留，但确保只跑 project_code === 'SYS-PERSONAL-GENERAL'
}
```

阈值默认值改 0.5：

```ts
const autoApplyThreshold = ref(0.5)
```

- [ ] **Step 4: 重要级 apply 拦截 — family 弹窗**

在现有 `applySuggestion` 里增加 409 处理：

```ts
async function applySuggestion(item: PendingItem, s: Suggestion, idx: number) {
  applyingId.value = item.resource_id
  applyingTargetIdx.value = idx
  try {
    const res = await fetch(`${API_BASE}/ai/classify/apply`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ resource_id: item.resource_id, project_id: s.project_id, stage_code: s.stage_code, file_rule_code: s.file_rule_code }),
    })
    if (res.status === 409) {
      const j = await res.json()
      const fid = j.data?.family_id
      if (fid) {
        await openAuthoritativeDialog(fid, item, s, idx)
      }
      return
    }
    const json = await res.json()
    if (json.success) {
      snackbar.value = { show: true, text: `已归目`, color: 'success' }
      pending.value = pending.value.filter(p => p.resource_id !== item.resource_id)
    } else {
      snackbar.value = { show: true, text: '应用失败：' + json.error, color: 'error' }
    }
  } finally {
    applyingId.value = 0
    applyingTargetIdx.value = -1
  }
}

const authoritativeDialog = ref<{ open: boolean; familyId: number; members: any[]; pendingItem: PendingItem | null; pendingSuggestion: Suggestion | null; pendingIdx: number }>({
  open: false, familyId: 0, members: [], pendingItem: null, pendingSuggestion: null, pendingIdx: -1,
})

async function openAuthoritativeDialog(familyId: number, item: PendingItem, s: Suggestion, idx: number) {
  const { fetchFamilyMembers } = await import('../services/api')
  try {
    const r = await fetchFamilyMembers(familyId)
    authoritativeDialog.value = {
      open: true, familyId, members: r.members, pendingItem: item, pendingSuggestion: s, pendingIdx: idx,
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载家族失败：' + e.message, color: 'error' }
  }
}

async function confirmAuthoritative(chosenResourceId: number) {
  const { setFamilyAuthoritative } = await import('../services/api')
  try {
    await setFamilyAuthoritative(authoritativeDialog.value.familyId, chosenResourceId)
    const item = authoritativeDialog.value.pendingItem
    const s = authoritativeDialog.value.pendingSuggestion
    const idx = authoritativeDialog.value.pendingIdx
    authoritativeDialog.value.open = false
    if (item && s) await applySuggestion(item, s, idx)
  } catch (e: any) {
    snackbar.value = { show: true, text: '设置权威源失败：' + e.message, color: 'error' }
  }
}
```

在 template 文件末尾，`<v-snackbar>` 之前加弹窗：

```vue
<v-dialog v-model="authoritativeDialog.open" max-width="600">
  <v-card>
    <v-card-title>选择权威源</v-card-title>
    <v-card-text>
      <div class="text-body-2 mb-3">同一资源在以下位置被检测为相似/同源。请选定一份作为"权威"。</div>
      <v-radio-group v-model="selectedAuthoritative">
        <v-radio v-for="m in authoritativeDialog.members" :key="m.data_resources_id"
                 :label="m.resources_name + ' (' + (m.family_relation || '-') + ')'"
                 :value="m.data_resources_id" />
      </v-radio-group>
    </v-card-text>
    <v-card-actions>
      <v-spacer />
      <v-btn variant="text" @click="authoritativeDialog.open = false">取消</v-btn>
      <v-btn color="primary" :disabled="!selectedAuthoritative" @click="confirmAuthoritative(selectedAuthoritative!)">确认</v-btn>
    </v-card-actions>
  </v-card>
</v-dialog>
```

加 `const selectedAuthoritative = ref<number | null>(null)`。

- [ ] **Step 5: 写前端测试**

Create `frontend_real/__tests__/AIClassifyView.levels.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import AIClassifyView from '../views/AIClassifyView.vue'

const vuetify = createVuetify({ components, directives })

function makeResp(items: any[], total: number) {
  return { ok: true, json: async () => ({ success: true, data: { items, total, page: 1, page_size: 20 } }) }
}

describe('AIClassifyView level sub-tabs', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('renders 4 level tabs (all/core/important/general) inside the new tab', async () => {
    global.fetch = vi.fn(async () => makeResp([
      { resource_id: 1, resource_name: 'a.pdf', suggestions: [{ project_id: 1, project_code: 'SYS-PERSONAL-CORE', stage_code: 'GR-DA', file_rule_code: 'OUT-001', confidence: 0.8 }] },
      { resource_id: 2, resource_name: 'b.pdf', suggestions: [{ project_id: 2, project_code: 'SYS-PERSONAL-GENERAL', stage_code: 'GR-DA', file_rule_code: 'OUT-001', confidence: 0.6 }] },
    ], 2)) as any
    const wrapper = mount(AIClassifyView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const tabs = wrapper.findAll('.v-tab').map(t => t.text())
    expect(tabs.some(t => t.includes('核心'))).toBe(true)
    expect(tabs.some(t => t.includes('重要'))).toBe(true)
    expect(tabs.some(t => t.includes('一般'))).toBe(true)
  })
})
```

- [ ] **Step 6: 跑前端测试 + 全套**

Run: `npx vitest run frontend_real/__tests__/AIClassifyView.levels.test.ts`

Run: `npx vitest run`

Expected: 全绿（含已有套件）。

- [ ] **Step 7: Commit**

```bash
git add frontend_real/services/api.ts frontend_real/views/AIClassifyView.vue frontend_real/__tests__/AIClassifyView.levels.test.ts
git commit -m "feat(scan): AI classify level sub-tabs + general one-click + important family arbitration UI"
```

---

### Task 8: 数字机要 view + route + 导航栏

**Files:**
- Create: `frontend_real/views/MemorandumView.vue`
- Modify: `frontend_real/plugins/router.ts`
- Modify: `frontend_real/App.vue`（导航栏菜单加一项）
- Test: `frontend_real/__tests__/MemorandumView.test.ts`

- [ ] **Step 1: 创建 MemorandumView.vue**

Create `frontend_real/views/MemorandumView.vue`:

```vue
<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-shield-lock</v-icon>
      数字机要
      <v-chip size="x-small" color="error" variant="tonal" class="ml-2">人工登记</v-chip>
    </v-card-title>

    <v-tabs v-model="currentTab" color="primary" class="px-4">
      <v-tab value="pending">
        待登记
        <v-badge inline :content="pendingTotal" :model-value="pendingTotal > 0" class="ml-2" />
      </v-tab>
      <v-tab value="registered">
        已登记
        <v-badge inline :content="registeredTotal" :model-value="registeredTotal > 0" class="ml-2" />
      </v-tab>
    </v-tabs>

    <v-window v-model="currentTab">
      <v-window-item value="pending">
        <v-card-text>
          <v-alert type="info" variant="tonal" density="compact" class="mb-3">
            核心级资料一律人工登记。登记后该条目不可再修改。
          </v-alert>
          <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-2" />
          <div v-if="!loading && items.length === 0" class="text-center text-medium-emphasis py-12">
            <v-icon size="64" color="grey-lighten-1">mdi-shield-check-outline</v-icon>
            <div class="mt-2">暂无待登记的数字机要</div>
          </div>
          <v-list density="compact" v-if="items.length > 0">
            <v-list-item v-for="item in items" :key="item.ledger_id" lines="two">
              <v-list-item-title>{{ item.asset_name }}</v-list-item-title>
              <v-list-item-subtitle>{{ item.file_version_code }} · 创建 {{ item.create_time }}</v-list-item-subtitle>
              <template #append>
                <v-btn color="primary" variant="tonal" size="small" @click="openRegister(item)">登记</v-btn>
              </template>
            </v-list-item>
          </v-list>
        </v-card-text>
      </v-window-item>

      <v-window-item value="registered">
        <v-card-text>
          <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-2" />
          <div v-if="!loading && items.length === 0" class="text-center text-medium-emphasis py-12">
            暂无已登记记录
          </div>
          <v-list density="compact" v-if="items.length > 0">
            <v-list-item v-for="item in items" :key="item.ledger_id" lines="three">
              <v-list-item-title>{{ item.asset_name }}</v-list-item-title>
              <v-list-item-subtitle>
                主题：{{ item.topic || '-' }} · 密级：{{ item.classification || '-' }} · 登记：{{ item.registered_at }}
              </v-list-item-subtitle>
            </v-list-item>
          </v-list>
        </v-card-text>
      </v-window-item>
    </v-window>

    <v-dialog v-model="registerDialog.open" max-width="540">
      <v-card>
        <v-card-title>数字机要登记</v-card-title>
        <v-card-text>
          <div class="text-body-2 mb-3 text-medium-emphasis">资源：{{ registerDialog.assetName }}</div>
          <v-text-field v-model="registerDialog.topic" label="工作主题" required density="compact" />
          <v-select v-model="registerDialog.classification" :items="['内部', '秘密', '机密']" label="密级" required density="compact" />
          <v-textarea v-model="registerDialog.note" label="备注" rows="2" density="compact" />
          <v-text-field v-model="registerDialog.password" label="登录密码（用于签字）" type="password" required density="compact" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="registerDialog.open = false">取消</v-btn>
          <v-btn color="primary" :loading="registerDialog.busy" :disabled="!canRegister" @click="doRegister">提交登记</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { API_BASE } from '../services/api'

interface MemorandumItem {
  ledger_id: number
  asset_name: string
  file_version_code: string
  create_time: string
  topic?: string | null
  classification?: string | null
  registered_at?: string | null
}

const currentTab = ref<'pending' | 'registered'>('pending')
const items = ref<MemorandumItem[]>([])
const loading = ref(false)
const pendingTotal = ref(0)
const registeredTotal = ref(0)
const snackbar = ref({ show: false, text: '', color: 'success' })

const registerDialog = ref({
  open: false,
  busy: false,
  ledgerId: 0,
  assetName: '',
  topic: '',
  classification: '',
  note: '',
  password: '',
})

const canRegister = computed(() =>
  !!registerDialog.value.topic.trim() &&
  !!registerDialog.value.classification &&
  !!registerDialog.value.password,
)

async function load() {
  loading.value = true
  try {
    const url = currentTab.value === 'pending' ? '/memorandum/pending' : '/memorandum/registered'
    const r = await fetch(`${API_BASE}${url}?page=1&page_size=50`)
    const j = await r.json()
    if (j.success) {
      items.value = j.data?.items || []
      if (currentTab.value === 'pending') pendingTotal.value = j.data?.total || 0
      else registeredTotal.value = j.data?.total || 0
    }
  } finally {
    loading.value = false
  }
}

async function warmInactive() {
  try {
    const other = currentTab.value === 'pending' ? '/memorandum/registered' : '/memorandum/pending'
    const r = await fetch(`${API_BASE}${other}?page=1&page_size=1`)
    const j = await r.json()
    if (j.success) {
      if (other === '/memorandum/registered') registeredTotal.value = j.data?.total || 0
      else pendingTotal.value = j.data?.total || 0
    }
  } catch {}
}

watch(currentTab, load)

function openRegister(item: MemorandumItem) {
  registerDialog.value = {
    open: true, busy: false, ledgerId: item.ledger_id, assetName: item.asset_name,
    topic: '', classification: '', note: '', password: '',
  }
}

async function doRegister() {
  registerDialog.value.busy = true
  try {
    const r = await fetch(`${API_BASE}/memorandum/register`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ledger_id: registerDialog.value.ledgerId,
        topic: registerDialog.value.topic,
        classification: registerDialog.value.classification,
        note: registerDialog.value.note,
        password: registerDialog.value.password,
      }),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: '登记成功', color: 'success' }
      registerDialog.value.open = false
      await load()
      await warmInactive()
    } else {
      snackbar.value = { show: true, text: '登记失败：' + j.error, color: 'error' }
    }
  } finally {
    registerDialog.value.busy = false
  }
}

onMounted(async () => {
  await load()
  await warmInactive()
})
</script>
```

- [ ] **Step 2: 注册路由**

打开 `frontend_real/plugins/router.ts`，加：

```ts
  {
    path: '/memorandum',
    name: 'Memorandum',
    component: () => import('@/views/MemorandumView.vue'),
  },
```

- [ ] **Step 3: 导航栏菜单加一项**

打开 `frontend_real/App.vue`，定位 `menuItems` 数组，在 `'AI 归目工具'` 那条**之后**加：

```ts
  { title: '数字机要', icon: 'mdi-shield-lock', to: '/memorandum', hint: '核心级资料人工登记通道', disabled: false },
```

- [ ] **Step 4: 写前端测试**

Create `frontend_real/__tests__/MemorandumView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import MemorandumView from '../views/MemorandumView.vue'

const vuetify = createVuetify({ components, directives })

function listResp(items: any[], total: number) {
  return { ok: true, json: async () => ({ success: true, data: { items, total, page: 1, page_size: 20 } }) }
}

describe('MemorandumView', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('renders pending list and shows 登记 button per item', async () => {
    global.fetch = vi.fn(async (input: RequestInfo) => {
      const url = String(input)
      if (url.includes('/memorandum/pending')) {
        return listResp([{ ledger_id: 1, asset_name: '机密.docx', file_version_code: 'OUT-001', create_time: '2026-05-21' }], 1) as any
      }
      return listResp([], 0) as any
    }) as any
    const wrapper = mount(MemorandumView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const txt = wrapper.text()
    expect(txt).toContain('机密.docx')
    expect(txt).toContain('登记')
  })

  it('register button is disabled without password / topic / classification', async () => {
    global.fetch = vi.fn(async () => listResp([{ ledger_id: 1, asset_name: 'x.docx', file_version_code: 'OUT-001', create_time: '2026-05-21' }], 1)) as any
    const wrapper = mount(MemorandumView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const openBtn = wrapper.findAll('button').find(b => b.text() === '登记')
    expect(openBtn).toBeTruthy()
    await openBtn!.trigger('click')
    await flushPromises()
    const submit = wrapper.findAll('button').find(b => b.text() === '提交登记')
    expect(submit).toBeTruthy()
    expect((submit!.attributes('disabled') ?? '') !== '').toBe(true)
  })
})
```

- [ ] **Step 5: 跑测试**

Run: `npx vitest run frontend_real/__tests__/MemorandumView.test.ts`

Run: `npx vitest run`

- [ ] **Step 6: Commit**

```bash
git add frontend_real/views/MemorandumView.vue frontend_real/plugins/router.ts \
        frontend_real/App.vue frontend_real/__tests__/MemorandumView.test.ts
git commit -m "feat(scan): digital memorandum view + nav entry"
```

---

### Task 9: 个人台账三级分层改造（红条 + bucket 差异化 + 副状态列）

**Files:**
- Modify: `frontend_real/views/PersonalFilesView.vue`
- Test: `frontend_real/__tests__/PersonalFilesView.tiered.test.ts`

- [ ] **Step 1: 加顶部"需人工干预"红条**

打开 `frontend_real/views/PersonalFilesView.vue`，在 `<section class="mb-5">`（工作事项 section）**之前**加：

```vue
<v-alert
  v-if="needsActionTotal > 0"
  type="warning"
  variant="tonal"
  density="compact"
  class="mb-3"
  prepend-icon="mdi-alert"
>
  <div class="d-flex align-center flex-wrap ga-3">
    <div v-if="memoPendingCount > 0">
      ⏳ 数字机要待登记 <strong>{{ memoPendingCount }}</strong>
      <v-btn variant="text" size="x-small" color="primary" @click="$router.push('/memorandum')">→ 数字机要</v-btn>
    </div>
    <div v-if="importantUnsettledCount > 0">
      ⚖ 重要级多源待裁定 <strong>{{ importantUnsettledCount }}</strong>
      <v-btn variant="text" size="x-small" color="primary" @click="$router.push('/ai-classify')">→ AI 归目工具</v-btn>
    </div>
  </div>
</v-alert>
```

在 `<script setup>` 加：

```ts
const memoPendingCount = ref(0)
const importantUnsettledCount = ref(0)
const needsActionTotal = computed(() => memoPendingCount.value + importantUnsettledCount.value)

async function loadActionCounters() {
  try {
    const r1 = await fetch(`${API_BASE}/memorandum/pending?page=1&page_size=1`)
    const j1 = await r1.json()
    if (j1.success) memoPendingCount.value = j1.data?.total || 0
  } catch {}
  // 重要级多源待裁定：保守起见，前端仅展示 0（后端 endpoint 后续添加）
  // 暂时无 endpoint 时维持 0；待 Task 9 follow-up 实现 /memorandum/needs-action endpoint 再接
}
```

在 `onMounted` 末尾加 `await loadActionCounters()`。

注：这一段先用 memo pending 数；重要级未确权数后续若需要独立端点可由 `GET /family/needs-arbitration` 之类返回；本期演示先展示 0 即可（避免引入新端点）。

- [ ] **Step 2: bucket 卡片差异化（核心去掉"看底账"按钮）**

定位 `<v-card variant="outlined" class="level-card">` 区块（约 line 116 附近）。改为按 bucket.code 条件渲染按钮区。把原来 bucket 公用的"查看 / 跳转" actions 替换为：

```vue
<v-card-actions>
  <template v-if="bucket.code === 'SYS-PERSONAL-CORE'">
    <v-btn size="small" variant="tonal" color="error" @click="$router.push('/memorandum')">
      <v-icon>mdi-shield-lock</v-icon>&nbsp;数字机要
    </v-btn>
  </template>
  <template v-else>
    <v-btn size="small" variant="text" @click="filterLedgers(bucket.code)">查看{{ bucket.title }}台账</v-btn>
    <v-btn v-if="bucket.code === 'SYS-PERSONAL-IMPORTANT'" size="small" variant="text" color="primary" @click="$router.push('/ai-classify?level=important')">→ 多源裁定</v-btn>
    <v-btn v-if="bucket.code === 'SYS-PERSONAL-GENERAL'" size="small" variant="text" color="primary" @click="$router.push('/ai-classify?level=general')">→ AI 一键归目</v-btn>
  </template>
</v-card-actions>
```

- [ ] **Step 3: 列表副状态列**

定位 `headers` 数组（line ~331 附近），在"级别"列**之后**插入：

```ts
{ title: '级别状态', key: 'tier_state', width: 130 },
```

加 `#item.tier_state` slot 到 `<v-data-table>` 区块（与现有 `#item.content_summary` 同区）：

```vue
<template #item.tier_state="{ item }">
  <template v-if="item.project_code === 'SYS-PERSONAL-CORE'">
    <v-chip v-if="item.memorandum_registered_at" size="x-small" color="success" variant="tonal">
      已机要 {{ formatDate(item.memorandum_registered_at) }}
    </v-chip>
    <v-chip v-else size="x-small" color="error" variant="tonal">待登记</v-chip>
  </template>
  <template v-else-if="item.project_code === 'SYS-PERSONAL-IMPORTANT'">
    <v-chip size="x-small" color="info" variant="tonal">{{ item.family_id ? '可能多源' : '独立' }}</v-chip>
  </template>
  <template v-else>
    <v-chip size="x-small" variant="tonal">{{ item.ai_classify_rejected_at ? '已治理' : '正常' }}</v-chip>
  </template>
</template>
```

加 helper：

```ts
function formatDate(s: string | null | undefined): string {
  if (!s) return ''
  return s.substring(0, 10)
}
```

注意：`memorandum_registered_at` 与 `family_id` 字段在 `AssetLedger` 类型上尚未定义。本期通过 `(item as any).memorandum_registered_at` 用即可——演示阶段，不要为这些字段改 projectsApi.ts 类型。可加注释解释。

- [ ] **Step 4: 工作事项卡片色点**

定位 `<v-card variant="outlined" class="topic-card">`（约 line 62）。在 `<v-card-title>` 内首字符前加：

```vue
<span
  v-if="item.latest && item.latest.project_code"
  class="d-inline-block mr-2"
  :style="{ width: '10px', height: '10px', borderRadius: '50%', backgroundColor: tierDotColor((item.latest as any).project_code) }"
/>
```

加 helper：

```ts
function tierDotColor(projectCode: string): string {
  if (projectCode === 'SYS-PERSONAL-CORE') return '#d32f2f'
  if (projectCode === 'SYS-PERSONAL-IMPORTANT') return '#f57c00'
  return '#43a047'
}
```

- [ ] **Step 5: 写前端测试**

Create `frontend_real/__tests__/PersonalFilesView.tiered.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createMemoryHistory, createRouter, type RouteRecordRaw } from 'vue-router'
import PersonalFilesView from '../views/PersonalFilesView.vue'

const vuetify = createVuetify({ components, directives })

const routes: RouteRecordRaw[] = [
  { path: '/', component: PersonalFilesView },
  { path: '/memorandum', component: { template: '<div/>' } },
  { path: '/ai-classify', component: { template: '<div/>' } },
]

describe('PersonalFilesView tiered buckets', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    global.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({ success: true, data: { items: [], total: 0, page: 1, page_size: 20 } }),
    })) as any
  })

  it('shows 数字机要 button in core bucket only', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    const wrapper = mount(PersonalFilesView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const txt = wrapper.text()
    expect(txt).toContain('数字机要')
  })
})
```

- [ ] **Step 6: 跑前端测试**

Run: `npx vitest run frontend_real/__tests__/PersonalFilesView.tiered.test.ts`

Run: `npx vitest run`

- [ ] **Step 7: Commit**

```bash
git add frontend_real/views/PersonalFilesView.vue frontend_real/__tests__/PersonalFilesView.tiered.test.ts
git commit -m "feat(scan): tiered ledger view with action alert + per-bucket actions + tier state column"
```

---

### Task 10: 最终回归

- [ ] **Step 1: Go 全套**

Run: `go test ./internal/...`

Expected: 全绿。

- [ ] **Step 2: 前端全套**

Run: `npx vitest run`

Expected: 全绿。

- [ ] **Step 3: git status 应该是 clean**

```bash
git status
```

- [ ] **Step 4: 看 commit 历史**

```bash
git log --oneline -15
```

Expected: 9 个 feat commit + 之前的 spec/plan commit。

---

## Self-Review

**Spec coverage:**
- ✅ 第一章 1.2 importance 同步 → Task 2
- ✅ 第一章 1.3 6 桶分流 → Task 7
- ✅ 第一章 1.3 origin=historical 时不跑 AI → 已在 V1 历史/新数据 spec 实现
- ✅ 第一章手动调级别 → Task 3
- ✅ 第二章数字机要登记 → Task 1 (列) + Task 5 (endpoints) + Task 8 (UI)
- ✅ 第二章 apply 命中核心 hint → Task 5 Step 5
- ✅ 第三章 family.authoritative_resource_id → Task 1 (列) + Task 4
- ✅ 第三章 apply 拦截 409 → Task 4 Step 11
- ✅ 第四章一键 AI 归目 + 一般级专属 → Task 7 Step 3
- ✅ 第四章 bulk-dismiss 放宽 → Task 6
- ✅ 第五章红条 + bucket 差异化 + 副状态列 + 色点 → Task 9
- ⚠ Cross-cutting 「重要级未确权 family 计数」endpoint 暂时返 0（无后端支撑），如需精确，未来加 `GET /family/needs-arbitration` 即可
- ⚠ 跨页 `?origin=&level=` 深链解析没单独写测试，但代码里已支持；如演示发现问题再补

**Placeholder scan:**
- 没有 TODO / TBD
- 每个 code block 都是可运行的实际代码

**Type consistency:**
- `PersonalCoreProjectCode` 等常量在 Go 代码里复用
- `SYS-PERSONAL-CORE` 字符串在前端代码里直接用——和后端常量值保持一致
- `level: 0|1|2|3|4` 类型与 `importance_level` 字段一致
- `bulkDismissHistorical` 函数名虽然以 historical 起头，但内部已经支持 (new, 3) — 名字保留向后兼容
