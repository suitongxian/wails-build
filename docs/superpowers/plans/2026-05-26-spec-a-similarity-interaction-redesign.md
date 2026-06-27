# Spec A：相似度分析交互重设计 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把"相似度分析"从用户感知里抹去（扫描完自动跑），把"批量认领时是否带上家族"显式化为弹窗交互。

**Architecture:** 后端最小化（仅新增 2 个查询接口 + 扫描完成钩子），重心在前端：列表 chip、二次确认对话框、单文件/批量两种认领弹窗、系统设置卡片。所有家族成员决策由前端在弹窗内组装，最终调用现有 `POST /resources/claim` 完成认领。

**Tech Stack:** Go (Gin) + sqlx (SQLite via better-sqlite3 in tests) / Vue 3 + Vuetify 3 + Vitest / yarn

**Spec reference:** `docs/superpowers/specs/2026-05-26-spec-a-similarity-interaction-redesign.md`

**关键铁律（来自 CLAUDE.md）：**
- 每个任务都要有测试用例验证才能进入下一阶段
- 项目中采用 yarn 代替 npm
- 测试前需运行 `npm rebuild better-sqlite3`（go test 不需要，仅 frontend vitest 触发 sqlite 时需要）
- 严禁修改扫描文件的物理操作（本 plan 仅触发后端流程，不写盘改文件，天然合规）

---

## File Structure

### 后端新增/修改

| 文件 | 责任 |
|---|---|
| `internal/repository/system_config.go` | 新增两个常量 key 和默认值 seed |
| `internal/repository/family.go` | 新增 `BatchListFamilyMembersByContentSigns` 方法 |
| `internal/httpd/family.go` | 新增 `POST /family/batch-members` handler |
| `internal/httpd/similarity.go` | 新增 `POST /analyze/preview` handler；导出 `RunAnalysisAsync()` 入口给 scanner 调用 |
| `internal/httpd/router.go` | 注册新增路由 |
| `internal/scanner/atomic.go` | 在 `CompleteUpdate` emit 后追加调用 `httpd.RunAnalysisAsync()`（异步、互斥） |

### 前端新增/修改

| 文件 | 责任 |
|---|---|
| `frontend_real/services/api.ts` | 新增 `analyzePreview()`、`batchFamilyMembers()`；扩展 `SystemConfig` 类型 |
| `frontend_real/views/ClaimView.vue` | primary 行加 chip；按钮改名 + 二次确认对话框；接弹窗触发逻辑 |
| `frontend_real/components/ClaimFamilyDialogSingle.vue`（新建） | 单文件认领弹窗 |
| `frontend_real/components/ClaimFamilyDialogBatch.vue`（新建） | 批量认领弹窗（方案 B） |
| `frontend_real/components/RebuildSimilarityDialog.vue`（新建） | 「重建相似关系」二次确认对话框 |
| `frontend_real/views/SystemConfigView.vue` | 新增「相似认领默认行为」配置卡片 |
| `frontend_real/__tests__/*` | 各组件测试 |

把弹窗组件抽出来，避免 ClaimView.vue 进一步膨胀（已经超 800 行）。

---

## Task 1：DB 引导 system_config 默认值

**Files:**
- Modify: `internal/repository/system_config.go`
- Test: `internal/repository/system_config_test.go`

- [ ] **Step 1: 在 system_config.go 文件头部常量区追加 key 定义**

```go
// 追加到现有 KeyXxx 常量列表
const (
    // ...existing keys...
    KeyClaimFamilyDefaultPolicy = "claim_family_default_policy"
    KeyClaimFamilySkipDialog    = "claim_family_skip_dialog"
)

// 合法值（用作枚举校验）
const (
    ClaimFamilyPolicySameContentOnly = "same_content_only"
    ClaimFamilyPolicyAll             = "all"
    ClaimFamilyPolicyNone            = "none"
)
```

- [ ] **Step 2: 写失败测试**

```go
// internal/repository/system_config_test.go 追加
func TestSystemConfig_ClaimFamilyDefaults(t *testing.T) {
    db := setupTestDB(t)
    repo := NewSystemConfigRepository(db)

    // 默认 policy 应为 same_content_only
    policy, err := repo.Get(KeyClaimFamilyDefaultPolicy)
    if err != nil {
        t.Fatalf("get policy: %v", err)
    }
    if policy != ClaimFamilyPolicySameContentOnly {
        t.Errorf("default policy = %q, want %q", policy, ClaimFamilyPolicySameContentOnly)
    }

    // 默认跳过弹窗为 false
    skip, err := repo.Get(KeyClaimFamilySkipDialog)
    if err != nil {
        t.Fatalf("get skip: %v", err)
    }
    if skip != "false" {
        t.Errorf("default skip = %q, want \"false\"", skip)
    }
}
```

- [ ] **Step 3: 运行测试验证失败**

```bash
cd /root/data/projects/data-asset-scan
go test ./internal/repository/ -run TestSystemConfig_ClaimFamilyDefaults -v
# 期望：FAIL，policy = "" or err，因为 seed 未实现
```

- [ ] **Step 4: 在 InitDB 流程里 seed 默认值**

在 `internal/repository/db.go` 的 `runMigrations()` 末尾（其他 seed 之后）追加：

```go
// 引导相似度认领默认行为配置
if err := seedClaimFamilyDefaults(db); err != nil {
    return fmt.Errorf("seed claim family defaults: %w", err)
}
```

新建函数（放在 `db.go` 末尾或新建 `internal/repository/claim_family_seed.go`）：

```go
func seedClaimFamilyDefaults(db *sqlx.DB) error {
    seeds := map[string]string{
        KeyClaimFamilyDefaultPolicy: ClaimFamilyPolicySameContentOnly,
        KeyClaimFamilySkipDialog:    "false",
    }
    now := time.Now()
    for key, val := range seeds {
        // INSERT OR IGNORE：已存在时不覆盖（保留用户已设置的值）
        _, err := db.Exec(`INSERT OR IGNORE INTO system_config
            (key, type, value, create_time, update_time, disable)
            VALUES (?, 'string', ?, ?, ?, 0)`, key, val, now, now)
        if err != nil {
            return fmt.Errorf("seed %s: %w", key, err)
        }
    }
    return nil
}
```

- [ ] **Step 5: 跑测试验证通过**

```bash
go test ./internal/repository/ -run TestSystemConfig_ClaimFamilyDefaults -v
# 期望：PASS
```

- [ ] **Step 6: Commit**

```bash
git add internal/repository/system_config.go internal/repository/system_config_test.go internal/repository/db.go
git commit -m "feat(claim): seed claim_family_default_policy/skip_dialog to system_config"
```

---

## Task 2：后端新增 BatchListFamilyMembersByContentSigns

**Files:**
- Modify: `internal/repository/family.go`
- Test: `internal/repository/family_batch_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
// internal/repository/family_batch_test.go
package repository

import (
    "testing"
    "time"
)

func TestFamilyRepository_BatchListByContentSigns(t *testing.T) {
    db := setupTestDB(t)
    repo := NewFamilyRepository(db)

    // seed: 2 个 family，每个 family 1 primary + 2 同伴
    now := time.Now()
    _, _ = db.Exec(`INSERT INTO data_resources (
        content_sign, source_count, workspace_source_count, first_create_time,
        resources_name, claim_status, importance_level, family_id, family_relation,
        create_time, update_time, disable, data_origin
    ) VALUES
        ('CS_F1_P', 1, 0, ?, 'fam1-primary.pdf', 0, 0, 1, 'primary', ?, ?, 0, 'historical'),
        ('CS_F1_M1', 1, 0, ?, 'fam1-mem1.pdf',    0, 0, 1, 'same_content', ?, ?, 0, 'historical'),
        ('CS_F1_M2', 1, 0, ?, 'fam1-mem2.pdf',    0, 0, 1, 'process_version', ?, ?, 0, 'historical'),
        ('CS_F2_P', 1, 0, ?, 'fam2-primary.docx', 0, 0, 2, 'primary', ?, ?, 0, 'historical'),
        ('CS_F2_M1', 1, 0, ?, 'fam2-mem1.docx',   0, 0, 2, 'derived', ?, ?, 0, 'historical')`,
        now, now, now, now, now, now, now, now, now, now, now, now, now, now, now)

    got, err := repo.BatchListFamilyMembersByContentSigns([]string{"CS_F1_P", "CS_F2_P"})
    if err != nil {
        t.Fatalf("batch list: %v", err)
    }
    if len(got) != 2 {
        t.Fatalf("got %d families, want 2", len(got))
    }
    if len(got["CS_F1_P"]) != 3 {
        t.Errorf("F1 members = %d, want 3", len(got["CS_F1_P"]))
    }
    if len(got["CS_F2_P"]) != 2 {
        t.Errorf("F2 members = %d, want 2", len(got["CS_F2_P"]))
    }
}

func TestFamilyRepository_BatchListByContentSigns_NoFamily(t *testing.T) {
    db := setupTestDB(t)
    repo := NewFamilyRepository(db)

    now := time.Now()
    // family_id IS NULL 的资源
    _, _ = db.Exec(`INSERT INTO data_resources (
        content_sign, source_count, workspace_source_count, first_create_time,
        resources_name, claim_status, importance_level, family_id,
        create_time, update_time, disable, data_origin
    ) VALUES ('CS_SOLO', 1, 0, ?, 'solo.pdf', 0, 0, NULL, ?, ?, 0, 'historical')`,
        now, now, now)

    got, _ := repo.BatchListFamilyMembersByContentSigns([]string{"CS_SOLO"})
    if _, ok := got["CS_SOLO"]; ok {
        t.Errorf("solo content_sign should not appear in result map (no family)")
    }
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
go test ./internal/repository/ -run TestFamilyRepository_BatchListByContentSigns -v
# 期望：FAIL，方法未定义
```

- [ ] **Step 3: 实现 BatchListFamilyMembersByContentSigns**

在 `internal/repository/family.go` 末尾追加：

```go
// BatchListFamilyMembersByContentSigns 给定一组 content_sign，返回每个 sign 对应 family
// 的全部成员清单（map 的 key 是入参 content_sign，value 是该 family 所有成员）。
// 入参中不属于任何 family 的 content_sign 不会出现在结果 map 里。
// 避免前端批量场景按 family_id 多次往返产生 N+1。
func (r *FamilyRepository) BatchListFamilyMembersByContentSigns(
    contentSigns []string,
) (map[string][]FamilyMemberDetail, error) {
    if len(contentSigns) == 0 {
        return map[string][]FamilyMemberDetail{}, nil
    }

    // 第一步：找 content_sign → family_id 映射
    placeholders := make([]string, len(contentSigns))
    args := make([]interface{}, len(contentSigns))
    for i, cs := range contentSigns {
        placeholders[i] = "?"
        args[i] = cs
    }
    query := `SELECT content_sign, family_id FROM data_resources
        WHERE content_sign IN (` + strings.Join(placeholders, ",") + `)
          AND family_id IS NOT NULL AND disable = 0`

    type row struct {
        ContentSign string `db:"content_sign"`
        FamilyID    int64  `db:"family_id"`
    }
    var mappings []row
    if err := r.DB.Select(&mappings, query, args...); err != nil {
        return nil, fmt.Errorf("query content_sign -> family_id: %w", err)
    }

    // 第二步：按 family_id 拉 members（一次查多个 family）
    familyIDs := make(map[int64]bool)
    for _, m := range mappings {
        familyIDs[m.FamilyID] = true
    }
    if len(familyIDs) == 0 {
        return map[string][]FamilyMemberDetail{}, nil
    }

    famIDList := make([]int64, 0, len(familyIDs))
    famPH := make([]string, 0, len(familyIDs))
    famArgs := make([]interface{}, 0, len(familyIDs))
    for fid := range familyIDs {
        famIDList = append(famIDList, fid)
        famPH = append(famPH, "?")
        famArgs = append(famArgs, fid)
    }

    membersQuery := `SELECT data_resources_id, family_id, family_relation, family_score,
        content_sign, resources_name, source_count, claim_status, claimant_name, claim_time
        FROM data_resources
        WHERE family_id IN (` + strings.Join(famPH, ",") + `) AND disable = 0
        ORDER BY family_id, family_relation`

    var allMembers []FamilyMemberDetail
    if err := r.DB.Select(&allMembers, membersQuery, famArgs...); err != nil {
        return nil, fmt.Errorf("query members: %w", err)
    }

    // 第三步：按 family_id 分组，再按 content_sign 映射回去
    membersByFamily := make(map[int64][]FamilyMemberDetail)
    for _, m := range allMembers {
        if m.FamilyID != nil {
            membersByFamily[*m.FamilyID] = append(membersByFamily[*m.FamilyID], m)
        }
    }

    result := make(map[string][]FamilyMemberDetail, len(mappings))
    for _, mp := range mappings {
        result[mp.ContentSign] = membersByFamily[mp.FamilyID]
    }
    return result, nil
}
```

如果 `FamilyMemberDetail` struct 缺少 `ClaimStatus / ClaimantName / ClaimTime` 字段，按需扩展（查 `family.go` 现有定义对齐）。

- [ ] **Step 4: 跑测试验证通过**

```bash
go test ./internal/repository/ -run TestFamilyRepository_BatchListByContentSigns -v
# 期望：PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/repository/family.go internal/repository/family_batch_test.go
git commit -m "feat(family): add BatchListFamilyMembersByContentSigns for batch claim dialog"
```

---

## Task 3：后端新增 POST /family/batch-members handler

**Files:**
- Modify: `internal/httpd/family.go`, `internal/httpd/router.go`
- Test: `internal/httpd/family_batch_members_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
// internal/httpd/family_batch_members_test.go
package httpd

import (
    "encoding/json"
    "strings"
    "testing"
    "time"
)

func TestHTTP_FamilyBatchMembers(t *testing.T) {
    r, db, cleanup := setupTestServer(t)
    defer cleanup()

    now := time.Now()
    _, _ = db.Exec(`INSERT INTO data_resources (
        content_sign, source_count, workspace_source_count, first_create_time,
        resources_name, claim_status, importance_level, family_id, family_relation,
        create_time, update_time, disable, data_origin
    ) VALUES
        ('CS_P1', 1, 0, ?, 'p1.pdf', 0, 0, 10, 'primary',      ?, ?, 0, 'historical'),
        ('CS_M1', 1, 0, ?, 'm1.pdf', 0, 0, 10, 'same_content', ?, ?, 0, 'historical'),
        ('CS_P2', 1, 0, ?, 'p2.pdf', 0, 0, 20, 'primary',      ?, ?, 0, 'historical')`,
        now, now, now, now, now, now, now, now, now)

    body := `{"content_signs":["CS_P1","CS_P2"]}`
    status, resp := jsonReq(t, r, "POST", "/family/batch-members", body)
    successOk(t, status, resp)

    var d struct {
        Data map[string][]map[string]interface{} `json:"data"`
    }
    if err := json.Unmarshal([]byte(resp), &d); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if len(d.Data["CS_P1"]) != 2 {
        t.Errorf("CS_P1 members = %d, want 2", len(d.Data["CS_P1"]))
    }
    if len(d.Data["CS_P2"]) != 1 {
        t.Errorf("CS_P2 members = %d, want 1", len(d.Data["CS_P2"]))
    }
}

func TestHTTP_FamilyBatchMembers_EmptyInput(t *testing.T) {
    r, _, cleanup := setupTestServer(t)
    defer cleanup()

    status, resp := jsonReq(t, r, "POST", "/family/batch-members", `{"content_signs":[]}`)
    if status != 200 {
        t.Errorf("status = %d, want 200; body=%s", status, resp)
    }
    if !strings.Contains(resp, `"data":{}`) {
        t.Errorf("expected empty data map, got %s", resp)
    }
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
go test ./internal/httpd/ -run TestHTTP_FamilyBatchMembers -v
# 期望：FAIL，路由 404
```

- [ ] **Step 3: 实现 handler 并注册路由**

`internal/httpd/family.go` 追加：

```go
// BatchFamilyMembersRequest is the POST /family/batch-members body.
type BatchFamilyMembersRequest struct {
    ContentSigns []string `json:"content_signs"`
}

// GetBatchFamilyMembers POST /family/batch-members
// 给前端批量场景一次性拉取多个 content_sign 对应的 family 成员，
// 避免 N+1。无 family 的 content_sign 不会出现在返回 map 里。
func GetBatchFamilyMembers(c *gin.Context) {
    var req BatchFamilyMembersRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
        return
    }
    repo := repository.NewFamilyRepository(repository.GetDB())
    result, err := repo.BatchListFamilyMembersByContentSigns(req.ContentSigns)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
```

`internal/httpd/router.go` 修改 `RegisterFamilyRoutes`：

```go
func RegisterFamilyRoutes(r *gin.RouterGroup) {
    r.GET("/needs-arbitration", GetFamilyNeedsArbitrationCount)
    r.GET("/:id", GetFamily)
    r.GET("/:id/members", GetFamilyMembers)
    r.POST("/:id/authoritative", SetFamilyAuthoritative)
    r.POST("/batch-members", GetBatchFamilyMembers) // 新增
}
```

- [ ] **Step 4: 跑测试验证通过**

```bash
go test ./internal/httpd/ -run TestHTTP_FamilyBatchMembers -v
# 期望：PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/httpd/family.go internal/httpd/router.go internal/httpd/family_batch_members_test.go
git commit -m "feat(httpd): POST /family/batch-members for batch claim dialog"
```

---

## Task 4：后端新增 POST /analyze/preview handler

**Files:**
- Modify: `internal/httpd/similarity.go`, `internal/httpd/router.go`
- Test: `internal/httpd/similarity_preview_test.go`（新建）

> 注意：本任务的 `cache_miss_count` 依赖 Spec B 的 mtime/size 字段；如果 Spec B 尚未实现，preview 接口仍可返回 `cache_miss_count: <total_files>`（视为全部需重算），后续 Spec B 实施后自动得到精确值。本任务实现的就是这个"先粗后精"版本。

- [ ] **Step 1: 写失败测试**

```go
// internal/httpd/similarity_preview_test.go
package httpd

import (
    "testing"
    "time"
)

func TestHTTP_AnalyzePreview_NoTasksYet(t *testing.T) {
    r, db, cleanup := setupTestServer(t)
    defer cleanup()

    // seed 3 个 data_distributing
    now := time.Now()
    for i, p := range []string{"/a.pdf", "/b.pdf", "/c.pdf"} {
        _, _ = db.Exec(`INSERT INTO data_distributing
            (path, content_sign, file_create_time, file_size, create_time, update_time, disable, data_origin)
            VALUES (?, ?, ?, 100, ?, ?, 0, 'historical')`,
            p, []string{"H1","H2","H3"}[i], now, now, now)
    }

    status, resp := jsonReqNoBody(t, r, "POST", "/analyze/preview")
    successOk(t, status, resp)
    d := dataMap(t, resp)

    if n, _ := d["cache_miss_count"].(float64); int(n) != 3 {
        t.Errorf("cache_miss_count = %v, want 3 (no features yet, all miss)", d["cache_miss_count"])
    }
    if _, exists := d["last_run_at"]; exists && d["last_run_at"] != nil {
        t.Errorf("last_run_at should be nil when no tasks have run")
    }
}

func TestHTTP_AnalyzePreview_WithLastRun(t *testing.T) {
    r, db, cleanup := setupTestServer(t)
    defer cleanup()

    now := time.Now()
    // 模拟一次成功跑过的任务
    _, _ = db.Exec(`INSERT INTO similarity_task
        (task_state, start_time, end_time, input_count, family_count, member_count)
        VALUES ('succeed', ?, ?, 50, 12, 80)`,
        now.Add(-1*time.Hour), now.Add(-50*time.Minute))

    status, resp := jsonReqNoBody(t, r, "POST", "/analyze/preview")
    successOk(t, status, resp)
    d := dataMap(t, resp)

    if d["last_run_at"] == nil {
        t.Errorf("last_run_at should be set; got %v", d)
    }
    if n, _ := d["last_run_duration_sec"].(float64); n != 600 {
        t.Errorf("last_run_duration_sec = %v, want 600 (10 minutes)", n)
    }
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
go test ./internal/httpd/ -run TestHTTP_AnalyzePreview -v
# 期望：FAIL，路由 404
```

- [ ] **Step 3: 实现 handler 并注册路由**

`internal/httpd/similarity.go` 追加：

```go
// PreviewAnalyze POST /analyze/preview
// 给"重建相似关系"二次确认对话框估算耗时和待重算文件数。
// 当 Spec B 实施后，cache_miss_count 来自 mtime/size 失效检测；
// 当前版本（Spec B 未上线）退化为返回全部文件数。
func PreviewAnalyze(c *gin.Context) {
    db := repository.GetDB()

    // 文件总数（distinct content_sign in data_distributing）
    var total int
    _ = db.Get(&total, `SELECT COUNT(DISTINCT content_sign) FROM data_distributing WHERE disable = 0`)

    // Spec B 实施后此处替换为 mtime/size 失效统计；当前版本视为全部 miss。
    cacheMissCount := total

    // 上次任务
    taskRepo := repository.NewSimilarityTaskRepository(db)
    var lastRunAt *time.Time
    var lastRunDurationSec int64
    if latest, err := taskRepo.LatestSucceeded(); err == nil && latest != nil {
        lastRunAt = &latest.EndTime
        lastRunDurationSec = int64(latest.EndTime.Sub(latest.StartTime).Seconds())
    }

    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data": gin.H{
            "cache_miss_count":      cacheMissCount,
            "last_run_at":           lastRunAt,
            "last_run_duration_sec": lastRunDurationSec,
        },
    })
}
```

如果 `LatestSucceeded()` 在 `SimilarityTaskRepository` 中不存在，新增：

```go
// internal/repository/similarity_task.go
func (r *SimilarityTaskRepository) LatestSucceeded() (*models.SimilarityTask, error) {
    var t models.SimilarityTask
    err := r.DB.Get(&t, `SELECT * FROM similarity_task
        WHERE task_state = 'succeed' ORDER BY end_time DESC LIMIT 1`)
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return &t, nil
}
```

`router.go` 修改 `RegisterSimilarityRoutes`：

```go
func RegisterSimilarityRoutes(r *gin.RouterGroup) {
    r.POST("/analyze", StartSimilarityAnalysis)
    r.POST("/analyze/preview", PreviewAnalyze) // 新增
    r.GET("/task/:id", GetSimilarityTask)
    r.GET("/task/latest", GetLatestSimilarityTask)
}
```

- [ ] **Step 4: 跑测试验证通过**

```bash
go test ./internal/httpd/ -run TestHTTP_AnalyzePreview -v
# 期望：PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/httpd/similarity.go internal/httpd/router.go internal/httpd/similarity_preview_test.go internal/repository/similarity_task.go
git commit -m "feat(httpd): POST /analyze/preview for rebuild similarity dialog estimate"
```

---

## Task 5：扫描完成自动触发 BuildFamilies

**Files:**
- Modify: `internal/scanner/atomic.go`, `internal/httpd/similarity.go`
- Test: `internal/scanner/atomic_auto_analyze_test.go`（新建）

- [ ] **Step 1: 在 similarity.go 导出无锁竞争的入口**

`internal/httpd/similarity.go` 追加：

```go
// RunAnalysisAsync 给非 HTTP 调用方（例如 scanner 完成钩子）使用。
// 已有任务在跑时直接返回（不阻塞、不排队），返回值表示是否成功 kick 了一次。
func RunAnalysisAsync() bool {
    taskRepo := repository.NewSimilarityTaskRepository(repository.GetDB())

    running, err := taskRepo.HasRunning()
    if err != nil {
        log.Printf("[similarity] auto-trigger: HasRunning check failed: %v", err)
        return false
    }
    if running {
        log.Printf("[similarity] auto-trigger: skipped, another task already running")
        return false
    }

    taskID, err := taskRepo.Create()
    if err != nil {
        log.Printf("[similarity] auto-trigger: create task failed: %v", err)
        return false
    }

    go runAnalysis(taskID)
    log.Printf("[similarity] auto-trigger: kicked task %d", taskID)
    return true
}
```

- [ ] **Step 2: 写失败测试（验证 scanner 调用 hook）**

由于 scanner 不应直接 import httpd 包（会出循环），引入 callback 注入模式。

```go
// internal/scanner/atomic_auto_analyze_test.go
package scanner

import (
    "sync/atomic"
    "testing"
)

func TestScanner_AutoTriggerAnalyzeOnComplete(t *testing.T) {
    var hookCalled atomic.Int32
    OnScanCompleteHook = func() {
        hookCalled.Add(1)
    }
    defer func() { OnScanCompleteHook = nil }()

    // 利用现有 helper 跑一次最小化扫描（假设已有 setupScanForTest helper）
    runMinimalScanForTest(t)

    if hookCalled.Load() != 1 {
        t.Errorf("hook called %d times, want 1", hookCalled.Load())
    }
}
```

如果 `runMinimalScanForTest` helper 不存在，先复用现有 `TestAtomicScanner_Basic` 或类似测试的 setup 代码，断言钩子被触发即可。

- [ ] **Step 3: 跑测试验证失败**

```bash
go test ./internal/scanner/ -run TestScanner_AutoTriggerAnalyzeOnComplete -v
# 期望：FAIL，OnScanCompleteHook 变量未定义
```

- [ ] **Step 4: 实现 hook 注入点**

`internal/scanner/atomic.go` 文件头追加：

```go
// OnScanCompleteHook 是扫描完成后被调用的钩子。
// 由 cmd/main.go 在启动时注入：cmd 同时引用 scanner 和 httpd，
// 把 httpd.RunAnalysisAsync 绑给这个钩子，避免 scanner 直接依赖 httpd。
var OnScanCompleteHook func()
```

在 scan 完成 emit `CompleteUpdate` 之后追加（atomic.go:1042 附近）：

```go
// Emit complete progress
s.emitProgress(ProgressUpdate{...})

// 触发自动相似度分析（如果注入了钩子）
if OnScanCompleteHook != nil {
    OnScanCompleteHook()
}
```

`cmd/main.go`（或 wails 启动入口）增加：

```go
// 注入扫描完成 → 相似度分析的钩子
scanner.OnScanCompleteHook = func() {
    httpd.RunAnalysisAsync()
}
```

- [ ] **Step 5: 跑测试验证通过**

```bash
go test ./internal/scanner/ -run TestScanner_AutoTriggerAnalyzeOnComplete -v
# 期望：PASS
```

- [ ] **Step 6: 集成测试验证完整链路**

```go
// internal/httpd/scanner_auto_trigger_integration_test.go
func TestIntegration_ScanCompleteTriggersAnalysis(t *testing.T) {
    // setup full server + scanner
    // 跑一次扫描
    // poll similarity_task 表，断言新增一条任务
}
```

- [ ] **Step 7: Commit**

```bash
git add internal/scanner/atomic.go internal/scanner/atomic_auto_analyze_test.go \
        internal/httpd/similarity.go cmd/main.go
git commit -m "feat(scan): auto-trigger BuildFamilies on scan completion via injected hook"
```

---

## Task 6：前端 services/api.ts 扩展类型与方法

**Files:**
- Modify: `frontend_real/services/api.ts`

- [ ] **Step 1: 扩展类型与方法**

```typescript
// SystemConfig 接口已存在，扩展两个字段
export interface SystemConfig {
  // ...existing fields...
  claim_family_default_policy?: 'same_content_only' | 'all' | 'none'
  claim_family_skip_dialog?: 'true' | 'false'
}

// 新增类型
export interface AnalyzePreview {
  cache_miss_count: number
  last_run_at: string | null
  last_run_duration_sec: number
}

export interface FamilyMemberDetail {
  data_resources_id: number
  family_id: number
  family_relation: string
  family_score: number | null
  content_sign: string
  resources_name: string | null
  source_count: number
  claim_status: number
  claimant_name: string | null
  claim_time: string | null
}

// api 对象追加：
export const api = {
  // ...existing methods...

  async analyzePreview(): Promise<AnalyzePreview> {
    const res = await fetch('/analyze/preview', { method: 'POST' })
    const j = await res.json()
    if (!j.success) throw new Error(j.error || 'preview failed')
    return j.data
  },

  async batchFamilyMembers(contentSigns: string[]): Promise<Record<string, FamilyMemberDetail[]>> {
    const res = await fetch('/family/batch-members', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content_signs: contentSigns }),
    })
    const j = await res.json()
    if (!j.success) throw new Error(j.error || 'batch members failed')
    return j.data || {}
  },
}
```

- [ ] **Step 2: 写测试验证类型契约**

```typescript
// frontend_real/__tests__/api.claimFamily.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { api } from '@/services/api'

describe('api.analyzePreview', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        success: true,
        data: {
          cache_miss_count: 12,
          last_run_at: '2026-05-26T10:00:00Z',
          last_run_duration_sec: 252,
        },
      }),
    })))
  })

  it('returns AnalyzePreview shape', async () => {
    const r = await api.analyzePreview()
    expect(r.cache_miss_count).toBe(12)
    expect(r.last_run_duration_sec).toBe(252)
  })
})

describe('api.batchFamilyMembers', () => {
  it('returns map keyed by content_sign', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        success: true,
        data: {
          'CS_1': [{ data_resources_id: 1, family_id: 10, family_relation: 'primary',
                     content_sign: 'CS_1', resources_name: 'a.pdf', source_count: 1,
                     claim_status: 0, family_score: 1.0, claimant_name: null, claim_time: null }],
        },
      }),
    })))

    const r = await api.batchFamilyMembers(['CS_1'])
    expect(r['CS_1']).toHaveLength(1)
    expect(r['CS_1'][0].family_relation).toBe('primary')
  })
})
```

- [ ] **Step 3: 跑测试**

```bash
cd /root/data/projects/data-asset-scan/frontend_real
yarn test --run __tests__/api.claimFamily.test.ts
# 期望：PASS
```

- [ ] **Step 4: Commit**

```bash
git add frontend_real/services/api.ts frontend_real/__tests__/api.claimFamily.test.ts
git commit -m "feat(api): add analyzePreview + batchFamilyMembers + claim family types"
```

---

## Task 7：前端「重建相似关系」二次确认对话框组件

**Files:**
- Create: `frontend_real/components/RebuildSimilarityDialog.vue`
- Test: `frontend_real/__tests__/RebuildSimilarityDialog.test.ts`

- [ ] **Step 1: 写失败测试**

```typescript
// frontend_real/__tests__/RebuildSimilarityDialog.test.ts
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import RebuildSimilarityDialog from '@/components/RebuildSimilarityDialog.vue'

const vuetify = createVuetify({ components })

describe('RebuildSimilarityDialog', () => {
  it('disables 开始重建 when cache_miss_count is 0', async () => {
    vi.mock('@/services/api', () => ({
      api: {
        analyzePreview: vi.fn(async () => ({
          cache_miss_count: 0,
          last_run_at: '2026-05-26T10:00:00Z',
          last_run_duration_sec: 252,
        })),
      },
    }))

    const wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
    })
    await new Promise(r => setTimeout(r, 50))

    const btn = wrapper.find('[data-test="rebuild-confirm-btn"]')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('无需重算')
  })

  it('shows N and M when cache_miss_count > 0', async () => {
    vi.mocked((await import('@/services/api')).api.analyzePreview).mockResolvedValue({
      cache_miss_count: 30,
      last_run_at: '2026-05-26T10:00:00Z',
      last_run_duration_sec: 252,
    })

    const wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
    })
    await new Promise(r => setTimeout(r, 50))

    expect(wrapper.text()).toContain('30')
    expect(wrapper.find('[data-test="rebuild-confirm-btn"]').attributes('disabled')).toBeUndefined()
  })
})
```

- [ ] **Step 2: 跑测试验证失败**

```bash
yarn test --run __tests__/RebuildSimilarityDialog.test.ts
# 期望：FAIL，组件不存在
```

- [ ] **Step 3: 实现组件**

```vue
<!-- frontend_real/components/RebuildSimilarityDialog.vue -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, type AnalyzePreview } from '@/services/api'

const props = defineProps<{
  modelValue: boolean
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm'): void
}>()

const preview = ref<AnalyzePreview | null>(null)
const loading = ref(false)

const fmtDuration = (sec: number): string => {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return m > 0 ? `${m} 分 ${s} 秒` : `${s} 秒`
}

const fmtDate = (iso: string | null): string => {
  if (!iso) return ''
  return new Date(iso).toLocaleString('zh-CN')
}

const estimatedMinutes = (miss: number, lastDurSec: number): number => {
  // 每文件平均耗时 = lastDur / lastMissCount。
  // 简化：用 2 秒/文件兜底（实际有了上次记录后可以更精确，但 preview 接口没回 last_miss_count）。
  const perFile = lastDurSec > 0 ? Math.max(2, lastDurSec / Math.max(miss, 1)) : 2
  return Math.max(1, Math.ceil((miss * perFile) / 60))
}

watch(() => props.modelValue, async (open) => {
  if (!open) return
  loading.value = true
  try {
    preview.value = await api.analyzePreview()
  } finally {
    loading.value = false
  }
}, { immediate: true })

const close = () => emit('update:modelValue', false)
const confirm = () => {
  emit('confirm')
  close()
}
</script>

<template>
  <v-dialog :model-value="modelValue" @update:model-value="close" max-width="520">
    <v-card>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-refresh</v-icon>
        重建相似关系
      </v-card-title>
      <v-card-text>
        <v-progress-circular v-if="loading" indeterminate size="24" />
        <template v-else-if="preview">
          <div v-if="preview.cache_miss_count === 0" class="text-success mb-2">
            无需重算，家族关系已是最新
          </div>
          <template v-else>
            <div class="mb-2">
              约 <strong>{{ preview.cache_miss_count }}</strong> 个文件需要重新计算特征
            </div>
            <div class="mb-2">
              预计耗时 ~<strong>{{ estimatedMinutes(preview.cache_miss_count, preview.last_run_duration_sec) }}</strong> 分钟
            </div>
          </template>
          <div v-if="preview.last_run_at" class="text-caption text-grey">
            上次重建：{{ fmtDate(preview.last_run_at) }}，耗时 {{ fmtDuration(preview.last_run_duration_sec) }}
          </div>
        </template>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="close">取消</v-btn>
        <v-btn
          color="primary"
          data-test="rebuild-confirm-btn"
          :disabled="loading || preview?.cache_miss_count === 0"
          @click="confirm"
        >
          开始重建
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
```

- [ ] **Step 4: 跑测试验证通过**

```bash
yarn test --run __tests__/RebuildSimilarityDialog.test.ts
# 期望：PASS
```

- [ ] **Step 5: Commit**

```bash
git add frontend_real/components/RebuildSimilarityDialog.vue \
        frontend_real/__tests__/RebuildSimilarityDialog.test.ts
git commit -m "feat(ui): RebuildSimilarityDialog with N/M estimate from /analyze/preview"
```

---

## Task 8：前端列表 primary 行加「关联 N ▾」chip

**Files:**
- Modify: `frontend_real/views/ClaimView.vue`
- Test: `frontend_real/__tests__/ClaimView.familyChip.test.ts`（新建）

- [ ] **Step 1: 写失败测试**

```typescript
// frontend_real/__tests__/ClaimView.familyChip.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import ClaimView from '@/views/ClaimView.vue'

const vuetify = createVuetify({ components })

const mkResource = (id: number, name: string, familyMemberCount: number) => ({
  data_resources_id: id, content_sign: `CS_${id}`, resources_name: name,
  source_count: 1, first_create_time: '2026-01-01', claim_status: 0,
  family_id: familyMemberCount > 0 ? 10 : null,
  family_member_count: familyMemberCount, family_relation: 'primary',
  family_same_content_count: 0, family_process_version_count: 0, family_derived_count: 0,
  primary_path: '/x/' + name,
})

describe('ClaimView primary row 关联 chip', () => {
  beforeEach(() => {
    vi.doMock('@/services/api', () => ({
      api: {
        getConfig: vi.fn(async () => ({})),
        getResourcesStatistics: vi.fn(async () => ({})),
        getResources: vi.fn(async () => ({
          resources: [mkResource(1, 'big-fam.pdf', 5), mkResource(2, 'solo.pdf', 1)],
          total: 2, page: 1, pageSize: 50,
        })),
        getLatestSimilarityTask: vi.fn(async () => null),
      },
    }))
  })

  it('shows 关联 N chip with count = family_member_count - 1', async () => {
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] } })
    await flushPromises()
    expect(wrapper.text()).toMatch(/关联.*4/)  // 5 - 1 = 4
  })

  it('hides 关联 chip when family_member_count <= 1', async () => {
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const rows = wrapper.findAll('tr')
    const soloRow = rows.find(r => r.text().includes('solo.pdf'))
    expect(soloRow?.text() ?? '').not.toMatch(/关联/)
  })
})
```

- [ ] **Step 2: 跑测试验证失败**

```bash
yarn test --run __tests__/ClaimView.familyChip.test.ts
# 期望：FAIL
```

- [ ] **Step 3: 在 ClaimView.vue 列表 primary 行 template 中追加 chip**

定位 `frontend_real/views/ClaimView.vue` 的 `template v-slot:item.resources_name` 部分（line ~600），在现有「N 副本」chip 旁边追加：

```vue
<!-- 现有 N 副本 chip 之后追加 -->
<v-tooltip location="top" open-delay="100" v-if="item.family_member_count > 1">
  <template v-slot:activator="{ props }">
    <v-chip
      v-bind="props"
      size="small"
      variant="tonal"
      color="primary"
      class="ml-2"
      style="cursor: pointer;"
      @click.stop="handleViewFamilyGroup(item, 'all')"
    >
      关联 {{ item.family_member_count - 1 }} ▾
    </v-chip>
  </template>
  <span>查看相似家族成员</span>
</v-tooltip>
```

注意：复用现有 `handleViewFamilyGroup` 方法，不新增 handler。chip 显示数字 = `family_member_count - 1`（除掉 primary 自己）。

- [ ] **Step 4: 跑测试验证通过**

```bash
yarn test --run __tests__/ClaimView.familyChip.test.ts
# 期望：PASS
```

- [ ] **Step 5: Commit**

```bash
git add frontend_real/views/ClaimView.vue frontend_real/__tests__/ClaimView.familyChip.test.ts
git commit -m "feat(claim): primary 行紧贴文件名加「关联 N ▾」chip"
```

---

## Task 9：前端按钮改名 + 接入重建对话框

**Files:**
- Modify: `frontend_real/views/ClaimView.vue`
- Test: `frontend_real/__tests__/ClaimView.rebuildBtn.test.ts`（新建）

- [ ] **Step 1: 写失败测试**

```typescript
// frontend_real/__tests__/ClaimView.rebuildBtn.test.ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import ClaimView from '@/views/ClaimView.vue'

const vuetify = createVuetify({ components })

describe('ClaimView 重建按钮', () => {
  it('renders 重建相似关系 button (renamed from 相似度分析)', () => {
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] } })
    expect(wrapper.text()).toContain('重建相似关系')
    expect(wrapper.text()).not.toContain('相似度分析')
  })

  it('opens RebuildSimilarityDialog on click', async () => {
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] } })
    const btn = wrapper.find('[data-test="rebuild-similarity-btn"]')
    await btn.trigger('click')
    expect(wrapper.find('.v-dialog').exists()).toBe(true)
    expect(wrapper.text()).toContain('重建相似关系')  // dialog title
  })
})
```

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 修改 ClaimView.vue 按钮 + 接入对话框**

```vue
<script setup lang="ts">
// 顶部 import 追加
import RebuildSimilarityDialog from '@/components/RebuildSimilarityDialog.vue'

// 顶部 state 追加
const showRebuildDialog = ref(false)
</script>

<template>
  <!-- 按钮改名 + 改回调 + 加 data-test -->
  <v-btn
    color="primary"
    variant="tonal"
    size="small"
    data-test="rebuild-similarity-btn"
    :disabled="isAnalysisRunning"
    @click="showRebuildDialog = true"
  >
    <v-icon start>mdi-refresh</v-icon>
    重建相似关系
  </v-btn>

  <!-- 移除按钮下方旧的"分析中..."小字 v-if 块 -->
  <!-- 移除 -->

  <!-- 加对话框组件 -->
  <RebuildSimilarityDialog
    v-model="showRebuildDialog"
    @confirm="startAnalysis"
  />
</template>
```

- [ ] **Step 4: 跑测试**

- [ ] **Step 5: Commit**

```bash
git add frontend_real/views/ClaimView.vue frontend_real/__tests__/ClaimView.rebuildBtn.test.ts
git commit -m "feat(claim): rename 相似度分析→重建相似关系 + wire RebuildSimilarityDialog"
```

---

## Task 10：前端 SystemConfigView 加「相似认领默认行为」卡片

**Files:**
- Modify: `frontend_real/views/SystemConfigView.vue`
- Test: `frontend_real/__tests__/SystemConfigView.claimFamily.test.ts`（新建）

- [ ] **Step 1: 写失败测试**

```typescript
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import SystemConfigView from '@/views/SystemConfigView.vue'

const vuetify = createVuetify({ components })

describe('SystemConfigView 相似认领默认行为', () => {
  it('renders 3 radio options + 总是弹窗确认 checkbox', async () => {
    const wrapper = mount(SystemConfigView, { global: { plugins: [vuetify] } })
    await new Promise(r => setTimeout(r, 50))

    expect(wrapper.text()).toContain('相似认领默认行为')
    expect(wrapper.text()).toContain('仅认领相同内容')
    expect(wrapper.text()).toContain('认领整个家族')
    expect(wrapper.text()).toContain('不带家族')
    expect(wrapper.text()).toContain('总是弹窗确认')
  })

  it('save policy change calls api.saveConfig with claim_family_default_policy', async () => {
    const saveMock = vi.fn(async () => ({ success: true }))
    vi.doMock('@/services/api', () => ({ api: { saveConfig: saveMock, getConfig: vi.fn(async () => ({})) } }))

    const wrapper = mount(SystemConfigView, { global: { plugins: [vuetify] } })
    await new Promise(r => setTimeout(r, 50))

    const radio = wrapper.find('[data-test="claim-family-policy-all"]')
    await radio.trigger('click')
    await wrapper.find('[data-test="save-claim-family-btn"]').trigger('click')

    expect(saveMock).toHaveBeenCalledWith(expect.objectContaining({
      claim_family_default_policy: 'all',
    }))
  })
})
```

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 在 SystemConfigView.vue 追加卡片**

具体位置参考现有"其他设置"卡片样式，在末尾追加：

```vue
<v-card class="mb-4" elevation="1">
  <v-card-title class="d-flex align-center">
    <v-icon class="mr-2">mdi-account-group</v-icon>
    相似认领默认行为
  </v-card-title>
  <v-card-text>
    <div class="text-body-2 mb-2">默认对相似家族的处理：</div>
    <v-radio-group v-model="claimFamilyPolicy" density="compact" hide-details>
      <v-radio
        value="same_content_only"
        label="仅认领相同内容（推荐）"
        data-test="claim-family-policy-same_content_only"
      />
      <v-radio
        value="all"
        label="认领整个家族（相同 + 过程 + 衍生）"
        data-test="claim-family-policy-all"
      />
      <v-radio
        value="none"
        label="不带家族（只认领选中文件）"
        data-test="claim-family-policy-none"
      />
    </v-radio-group>

    <v-checkbox
      v-model="claimFamilyAlwaysAsk"
      label="总是弹窗确认（即便已设默认）"
      density="compact"
      hide-details
      class="mt-2"
    />
    <div class="text-caption text-grey mt-1">
      ↑ 取消勾选此项相当于"下次不再问"
    </div>

    <v-btn
      class="mt-4"
      color="primary"
      :loading="savingClaimFamily"
      data-test="save-claim-family-btn"
      @click="saveClaimFamilyConfig"
    >
      保存
    </v-btn>
  </v-card-text>
</v-card>
```

`<script setup>` 顶部追加：

```typescript
const claimFamilyPolicy = ref<'same_content_only' | 'all' | 'none'>('same_content_only')
const claimFamilyAlwaysAsk = ref(true)  // skip_dialog = false 时此处为 true
const savingClaimFamily = ref(false)

// onMounted 里追加 load
onMounted(async () => {
  // ... existing loads ...
  const cfg = await api.getConfig()
  if (cfg.claim_family_default_policy) {
    claimFamilyPolicy.value = cfg.claim_family_default_policy
  }
  claimFamilyAlwaysAsk.value = cfg.claim_family_skip_dialog !== 'true'
})

async function saveClaimFamilyConfig() {
  savingClaimFamily.value = true
  try {
    await api.saveConfig({
      claim_family_default_policy: claimFamilyPolicy.value,
      claim_family_skip_dialog: claimFamilyAlwaysAsk.value ? 'false' : 'true',
    } as any)
    showSnackbar('已保存', 'success')
  } catch (e) {
    showSnackbar('保存失败：' + (e as Error).message, 'error')
  } finally {
    savingClaimFamily.value = false
  }
}
```

- [ ] **Step 4: 跑测试验证通过**

- [ ] **Step 5: Commit**

```bash
git add frontend_real/views/SystemConfigView.vue \
        frontend_real/__tests__/SystemConfigView.claimFamily.test.ts
git commit -m "feat(config): 系统设置加「相似认领默认行为」卡片"
```

---

## Task 11：前端单文件认领弹窗组件

**Files:**
- Create: `frontend_real/components/ClaimFamilyDialogSingle.vue`
- Test: `frontend_real/__tests__/ClaimFamilyDialogSingle.test.ts`

- [ ] **Step 1: 写失败测试**

```typescript
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import ClaimFamilyDialogSingle from '@/components/ClaimFamilyDialogSingle.vue'

const vuetify = createVuetify({ components })

const sampleMembers = [
  { data_resources_id: 1, family_relation: 'primary',        content_sign: 'CS_P', resources_name: 'main.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
  { data_resources_id: 2, family_relation: 'same_content',   content_sign: 'CS_1', resources_name: 'backup.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
  { data_resources_id: 3, family_relation: 'same_content',   content_sign: 'CS_2', resources_name: 'dup.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
  { data_resources_id: 4, family_relation: 'process_version', content_sign: 'CS_3', resources_name: 'v2.docx', source_count: 1, claim_status: 0, family_score: 0.87, family_id: 10, claimant_name: null, claim_time: null },
  { data_resources_id: 5, family_relation: 'derived',         content_sign: 'CS_4', resources_name: 'note.docx', source_count: 1, claim_status: 0, family_score: 0.74, family_id: 10, claimant_name: null, claim_time: null },
]

describe('ClaimFamilyDialogSingle', () => {
  it('defaults to checking same_content members only', () => {
    const wrapper = mount(ClaimFamilyDialogSingle, {
      props: {
        modelValue: true,
        primary: sampleMembers[0],
        members: sampleMembers,
        claimStatus: 2, // 个人工作
      },
      global: { plugins: [vuetify] },
    })
    // same_content 2 个默认勾选 → 主按钮文案"认领 3 个（1 主 + 2 相似）"
    expect(wrapper.text()).toContain('认领 3 个')
  })

  it('emits confirm with selected member IDs (primary + checked)', async () => {
    const wrapper = mount(ClaimFamilyDialogSingle, {
      props: { modelValue: true, primary: sampleMembers[0], members: sampleMembers, claimStatus: 2 },
      global: { plugins: [vuetify] },
    })
    await wrapper.find('[data-test="confirm-btn"]').trigger('click')
    expect(wrapper.emitted('confirm')?.[0]).toEqual([{
      ids: [1, 2, 3],
      skipNextTime: false,
    }])
  })

  it('grays out already-claimed members and excludes them from result', async () => {
    const withClaimed = [...sampleMembers]
    withClaimed[1] = { ...withClaimed[1], claim_status: 2, claimant_name: '张三' }

    const wrapper = mount(ClaimFamilyDialogSingle, {
      props: { modelValue: true, primary: sampleMembers[0], members: withClaimed, claimStatus: 2 },
      global: { plugins: [vuetify] },
    })
    expect(wrapper.text()).toContain('已认领（认领人：张三')
    await wrapper.find('[data-test="confirm-btn"]').trigger('click')
    // claim_status=2 的 ID 2 应被排除
    expect(wrapper.emitted('confirm')?.[0]?.[0].ids).not.toContain(2)
  })
})
```

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 实现组件**

```vue
<!-- frontend_real/components/ClaimFamilyDialogSingle.vue -->
<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FamilyMemberDetail } from '@/services/api'

const props = defineProps<{
  modelValue: boolean
  primary: FamilyMemberDetail
  members: FamilyMemberDetail[]
  claimStatus: number
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm', payload: { ids: number[]; skipNextTime: boolean }): void
}>()

// 分组
const sameContent = computed(() => props.members.filter(m => m.family_relation === 'same_content'))
const processVersion = computed(() => props.members.filter(m => m.family_relation === 'process_version'))
const derived = computed(() => props.members.filter(m => m.family_relation === 'derived'))

// 默认勾选：same_content 全勾，其他不勾
const checkedIds = ref<Set<number>>(new Set())
const skipNextTime = ref(false)

watch(() => [props.modelValue, props.members] as const, ([open]) => {
  if (open) {
    const s = new Set<number>()
    sameContent.value.forEach(m => {
      if (m.claim_status === 0) s.add(m.data_resources_id) // 未认领的默认勾上
    })
    checkedIds.value = s
  }
}, { immediate: true })

const isAlreadyClaimed = (m: FamilyMemberDetail) => m.claim_status !== 0
const toggle = (m: FamilyMemberDetail) => {
  if (isAlreadyClaimed(m)) return
  const s = new Set(checkedIds.value)
  if (s.has(m.data_resources_id)) s.delete(m.data_resources_id)
  else s.add(m.data_resources_id)
  checkedIds.value = s
}

const finalIds = computed(() => {
  const ids = [props.primary.data_resources_id]
  for (const id of checkedIds.value) ids.push(id)
  return ids
})

const close = () => emit('update:modelValue', false)
const confirm = () => {
  emit('confirm', { ids: finalIds.value, skipNextTime: skipNextTime.value })
  close()
}
const claimOnlyPrimary = () => {
  emit('confirm', { ids: [props.primary.data_resources_id], skipNextTime: skipNextTime.value })
  close()
}

const fmtScore = (s: number | null) => s == null ? '' : `${(s * 100).toFixed(0)}%`
</script>

<template>
  <v-dialog :model-value="modelValue" @update:model-value="close" max-width="640">
    <v-card>
      <v-card-title>一并认领相似文件？</v-card-title>
      <v-card-text>
        <div class="mb-3">
          <div class="font-weight-medium">你正在认领：{{ primary.resources_name || '-' }}</div>
        </div>
        <div class="info-bar mb-3">
          🔗 关联 {{ members.length - 1 }} 个相似文件
        </div>

        <div v-if="sameContent.length > 0" class="mb-3">
          <div class="text-caption font-weight-medium mb-1">✓ 相同内容（{{ sameContent.length }} 个 · 默认勾选）</div>
          <div v-for="m in sameContent" :key="m.data_resources_id" class="member-row">
            <v-checkbox
              :model-value="checkedIds.has(m.data_resources_id)"
              :disabled="isAlreadyClaimed(m)"
              hide-details density="compact"
              @update:model-value="toggle(m)"
            />
            <span :class="{ 'text-disabled': isAlreadyClaimed(m) }">{{ m.resources_name }}</span>
            <span v-if="isAlreadyClaimed(m)" class="text-caption text-disabled ml-2">
              已认领（认领人：{{ m.claimant_name || '?' }}）
            </span>
            <span class="ml-auto text-success text-caption">{{ fmtScore(m.family_score) }}</span>
          </div>
        </div>

        <div v-if="processVersion.length > 0" class="mb-3">
          <div class="text-caption font-weight-medium mb-1">☐ 过程版本（{{ processVersion.length }} 个 · 按需勾选）</div>
          <div v-for="m in processVersion" :key="m.data_resources_id" class="member-row">
            <v-checkbox
              :model-value="checkedIds.has(m.data_resources_id)"
              :disabled="isAlreadyClaimed(m)"
              hide-details density="compact"
              @update:model-value="toggle(m)"
            />
            <span :class="{ 'text-disabled': isAlreadyClaimed(m) }">{{ m.resources_name }}</span>
            <span v-if="isAlreadyClaimed(m)" class="text-caption text-disabled ml-2">
              已认领
            </span>
            <span class="ml-auto text-warning text-caption">{{ fmtScore(m.family_score) }}</span>
          </div>
        </div>

        <div v-if="derived.length > 0" class="mb-3">
          <div class="text-caption font-weight-medium mb-1">☐ 衍生文件（{{ derived.length }} 个）</div>
          <div v-for="m in derived" :key="m.data_resources_id" class="member-row">
            <v-checkbox
              :model-value="checkedIds.has(m.data_resources_id)"
              :disabled="isAlreadyClaimed(m)"
              hide-details density="compact"
              @update:model-value="toggle(m)"
            />
            <span :class="{ 'text-disabled': isAlreadyClaimed(m) }">{{ m.resources_name }}</span>
            <span class="ml-auto text-info text-caption">{{ fmtScore(m.family_score) }}</span>
          </div>
        </div>

        <v-divider class="my-3" />
        <v-checkbox
          v-model="skipNextTime"
          label="以后总是按当前选择，不再询问"
          hide-details density="compact"
        />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="claimOnlyPrimary">仅领此文件</v-btn>
        <v-btn color="primary" data-test="confirm-btn" @click="confirm">
          认领 {{ finalIds.length }} 个（1 主 + {{ finalIds.length - 1 }} 相似）
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.info-bar {
  background: #eef5ff;
  border-left: 3px solid #1976d2;
  padding: 10px 12px;
  border-radius: 4px;
  font-size: 13px;
  color: #1565c0;
}
.member-row {
  display: flex;
  align-items: center;
  padding: 4px 8px;
  font-size: 13px;
}
</style>
```

- [ ] **Step 4: 跑测试验证通过**

- [ ] **Step 5: Commit**

```bash
git add frontend_real/components/ClaimFamilyDialogSingle.vue \
        frontend_real/__tests__/ClaimFamilyDialogSingle.test.ts
git commit -m "feat(claim): single-file claim dialog with same_content default + skip option"
```

---

## Task 12：前端批量认领弹窗组件（方案 B）

**Files:**
- Create: `frontend_real/components/ClaimFamilyDialogBatch.vue`
- Test: `frontend_real/__tests__/ClaimFamilyDialogBatch.test.ts`

> 这是 Spec A 最复杂的组件，行内展开 + 全局默认下拉 + 行级覆盖 + 已认领灰掉 + 抽屉（N>20）+ 跨 tab 标注。

- [ ] **Step 1: 写失败测试（核心场景 4 条）**

```typescript
// frontend_real/__tests__/ClaimFamilyDialogBatch.test.ts
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import ClaimFamilyDialogBatch from '@/components/ClaimFamilyDialogBatch.vue'

const vuetify = createVuetify({ components })

const mkMember = (id: number, rel: string, sign: string, name: string, claimStatus = 0) => ({
  data_resources_id: id, family_relation: rel, content_sign: sign,
  resources_name: name, source_count: 1, claim_status: claimStatus,
  family_score: 1.0, family_id: Math.floor(id / 10) || 1,
  claimant_name: claimStatus !== 0 ? '张三' : null, claim_time: null,
})

describe('ClaimFamilyDialogBatch', () => {
  it('renders one row per selected primary + 关联 chip with count', () => {
    const selected = [mkMember(1, 'primary', 'CS_1', 'p1.docx'), mkMember(2, 'primary', 'CS_2', 'p2.docx')]
    const familyMap = {
      'CS_1': [mkMember(1, 'primary', 'CS_1', 'p1.docx'),
               mkMember(11, 'same_content', 'CS_11', 'p1-bk.docx'),
               mkMember(12, 'same_content', 'CS_12', 'p1-cp.docx')],
      'CS_2': [mkMember(2, 'primary', 'CS_2', 'p2.docx')],
    }
    const wrapper = mount(ClaimFamilyDialogBatch, {
      props: { modelValue: true, selectedPrimaries: selected, familyMap, claimStatus: 2 },
      global: { plugins: [vuetify] },
    })
    expect(wrapper.findAll('[data-test^="batch-row-"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('关联 2')  // CS_1 has 2 non-primary
    expect(wrapper.text()).toContain('无关联')   // CS_2 has no extra members
  })

  it('global default change syncs all rows not yet user-modified', async () => {
    const selected = [mkMember(1, 'primary', 'CS_1', 'p1.docx'),
                      mkMember(2, 'primary', 'CS_2', 'p2.docx')]
    const familyMap = {
      'CS_1': [selected[0], mkMember(11, 'same_content', 'CS_11', 'm1.docx')],
      'CS_2': [selected[1], mkMember(21, 'process_version', 'CS_21', 'm2.docx')],
    }
    const wrapper = mount(ClaimFamilyDialogBatch, {
      props: { modelValue: true, selectedPrimaries: selected, familyMap, claimStatus: 2,
               defaultPolicy: 'same_content_only' },
      global: { plugins: [vuetify] },
    })
    // 默认 same_content_only：行 1 含 1 same → 应认领 2 个；行 2 无 same → 应只领 1 个
    expect(wrapper.text()).toMatch(/确认认领\s*3/)

    // 找到全局下拉并改为 all
    const globalSelect = wrapper.find('.global-bar .v-select')
    await globalSelect.find('input').setValue('all')
    await wrapper.vm.$nextTick()
    // 改为 all：两行都含全部成员（1 same + 1 process）→ 应认领 4 个
    expect(wrapper.text()).toMatch(/确认认领\s*4/)
  })

  it('confirm emits IDs combining primary + checked members', async () => {
    const selected = [mkMember(1, 'primary', 'CS_1', 'p1.docx')]
    const familyMap = {
      'CS_1': [selected[0], mkMember(11, 'same_content', 'CS_11', 'm1.docx')],
    }
    const wrapper = mount(ClaimFamilyDialogBatch, {
      props: { modelValue: true, selectedPrimaries: selected, familyMap, claimStatus: 2,
               defaultPolicy: 'same_content_only' },
      global: { plugins: [vuetify] },
    })
    await wrapper.find('[data-test="batch-confirm-btn"]').trigger('click')
    const payload = wrapper.emitted('confirm')?.[0]?.[0] as any
    expect(payload.ids).toEqual([1, 11])
  })

  it('已认领成员显示灰掉，不计入最终 IDs', async () => {
    const selected = [mkMember(1, 'primary', 'CS_1', 'p1.docx')]
    const familyMap = {
      'CS_1': [mkMember(1, 'primary', 'CS_1', 'p1.docx'),
               mkMember(11, 'same_content', 'CS_11', 'taken.docx', 2 /* 已认领 */)],
    }
    const wrapper = mount(ClaimFamilyDialogBatch, {
      props: { modelValue: true, selectedPrimaries: selected, familyMap, claimStatus: 2,
               defaultPolicy: 'same_content_only' },
      global: { plugins: [vuetify] },
    })
    await wrapper.find('[data-test="batch-confirm-btn"]').trigger('click')
    const ids = wrapper.emitted('confirm')?.[0]?.[0].ids as number[]
    expect(ids).toContain(1)    // primary
    expect(ids).not.toContain(11) // 已认领跳过
  })
})
```

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 实现组件（结构最小、行为正确）**

```vue
<!-- frontend_real/components/ClaimFamilyDialogBatch.vue -->
<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FamilyMemberDetail } from '@/services/api'

type Policy = 'same_content_only' | 'all' | 'none'

const props = defineProps<{
  modelValue: boolean
  selectedPrimaries: FamilyMemberDetail[]
  familyMap: Record<string, FamilyMemberDetail[]>  // content_sign → members
  claimStatus: number
  defaultPolicy?: Policy
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm', payload: { ids: number[]; skipNextTime: boolean }): void
}>()

const globalPolicy = ref<Policy>(props.defaultPolicy || 'same_content_only')
const skipNextTime = ref(false)

// 每行的独立 policy（用户改过的）；未在此 map 中的行 fallback 到 globalPolicy
const rowPolicies = ref<Record<string, Policy>>({})
const customizedRows = ref<Set<string>>(new Set())

const expandedRows = ref<Set<string>>(new Set())

const setRowPolicy = (cs: string, p: Policy) => {
  rowPolicies.value = { ...rowPolicies.value, [cs]: p }
  customizedRows.value = new Set([...customizedRows.value, cs])
}

const effectiveRowPolicy = (cs: string): Policy => {
  return customizedRows.value.has(cs) ? rowPolicies.value[cs] : globalPolicy.value
}

// 全局变更：覆盖所有未自定义的行
watch(globalPolicy, () => {
  // 未自定义的行天然走 globalPolicy（effectiveRowPolicy 计算属性逻辑），无需主动更新
})

const isAlreadyClaimed = (m: FamilyMemberDetail) => m.claim_status !== 0

const idsForPrimary = (primary: FamilyMemberDetail): number[] => {
  const cs = primary.content_sign
  const members = props.familyMap[cs] || [primary]
  const policy = effectiveRowPolicy(cs)
  const ids = [primary.data_resources_id]
  for (const m of members) {
    if (m.data_resources_id === primary.data_resources_id) continue
    if (isAlreadyClaimed(m)) continue
    if (policy === 'all') ids.push(m.data_resources_id)
    else if (policy === 'same_content_only' && m.family_relation === 'same_content') {
      ids.push(m.data_resources_id)
    }
  }
  return ids
}

const totalIds = computed(() => {
  const all: number[] = []
  for (const p of props.selectedPrimaries) {
    for (const id of idsForPrimary(p)) all.push(id)
  }
  return all
})

const totalSkipped = computed(() => {
  let n = 0
  for (const p of props.selectedPrimaries) {
    const ms = props.familyMap[p.content_sign] || []
    n += ms.filter(m => m.data_resources_id !== p.data_resources_id && isAlreadyClaimed(m)).length
  }
  return n
})

const familyMemberCount = (cs: string): number => {
  const ms = props.familyMap[cs]
  if (!ms || ms.length <= 1) return 0
  return ms.length - 1  // 除掉 primary
}

const close = () => emit('update:modelValue', false)
const confirm = () => {
  emit('confirm', { ids: totalIds.value, skipNextTime: skipNextTime.value })
  close()
}

const policyOptions: { value: Policy; label: string }[] = [
  { value: 'same_content_only', label: '仅带相同内容（推荐）' },
  { value: 'all',               label: '全部带（相同+过程+衍生）' },
  { value: 'none',              label: '都不带' },
]

const rowPolicyLabel = (cs: string): string => {
  const p = effectiveRowPolicy(cs)
  const ms = props.familyMap[cs] || []
  const sameCount = ms.filter(m => m.family_relation === 'same_content' && !isAlreadyClaimed(m)).length
  const allCount = ms.filter(m => m.family_relation !== 'primary' && !isAlreadyClaimed(m)).length
  if (p === 'same_content_only') return `仅同内容 (${sameCount})`
  if (p === 'all') return `全选 (${allCount})`
  return '不带'
}

const toggleExpand = (cs: string) => {
  const s = new Set(expandedRows.value)
  if (s.has(cs)) s.delete(cs)
  else s.add(cs)
  expandedRows.value = s
}

const shouldUseDrawer = (cs: string) => familyMemberCount(cs) > 20
</script>

<template>
  <v-dialog :model-value="modelValue" @update:model-value="close" max-width="880">
    <v-card>
      <v-card-title>认领 {{ selectedPrimaries.length }} 个文件</v-card-title>
      <v-card-text>
        <div class="global-bar mb-3">
          🔗 共识别到 {{ totalIds.length - selectedPrimaries.length }} 个相似文件。批量默认：
          <v-select
            v-model="globalPolicy"
            :items="policyOptions" item-value="value" item-title="label"
            density="compact" hide-details variant="outlined" style="width: 220px; display: inline-block;"
          />
        </div>

        <div
          v-for="primary in selectedPrimaries"
          :key="primary.data_resources_id"
          :data-test="`batch-row-${primary.data_resources_id}`"
          class="batch-row"
        >
          <div class="row-main">
            <div class="fname">
              {{ primary.resources_name || '-' }}
              <span v-if="customizedRows.has(primary.content_sign)" class="text-caption text-warning ml-2">已自定义</span>
            </div>
            <v-chip
              v-if="familyMemberCount(primary.content_sign) > 0"
              size="small" variant="tonal" color="info"
              @click="toggleExpand(primary.content_sign)"
            >
              关联 {{ familyMemberCount(primary.content_sign) }} ▾
            </v-chip>
            <span v-else class="text-caption text-disabled">无关联</span>

            <v-select
              v-if="familyMemberCount(primary.content_sign) > 0"
              :model-value="effectiveRowPolicy(primary.content_sign)"
              :items="policyOptions" item-value="value" item-title="label"
              density="compact" hide-details variant="outlined"
              style="width: 200px; margin-left: auto;"
              @update:model-value="setRowPolicy(primary.content_sign, $event)"
            />
          </div>

          <!-- 行内展开：family 成员明细 -->
          <div v-if="expandedRows.has(primary.content_sign) && !shouldUseDrawer(primary.content_sign)" class="row-expand">
            <div
              v-for="m in (familyMap[primary.content_sign] || []).filter(m => m.data_resources_id !== primary.data_resources_id)"
              :key="m.data_resources_id"
              class="member-detail-row"
              :class="{ 'text-disabled': isAlreadyClaimed(m) }"
            >
              <span>{{ m.resources_name }}</span>
              <span class="ml-2 text-caption">{{ m.family_relation }}</span>
              <span v-if="isAlreadyClaimed(m)" class="ml-2 text-caption">已认领（{{ m.claimant_name }}）</span>
            </div>
          </div>
        </div>

        <!-- 抽屉（N>20）暂用简化的全屏展示；正式实现可改 v-navigation-drawer -->
        <v-dialog v-for="primary in selectedPrimaries.filter(p => expandedRows.has(p.content_sign) && shouldUseDrawer(p.content_sign))"
                  :key="primary.data_resources_id" :model-value="true" max-width="800"
                  @update:model-value="toggleExpand(primary.content_sign)">
          <v-card>
            <v-card-title>{{ primary.resources_name }} · 关联成员 ({{ familyMemberCount(primary.content_sign) }})</v-card-title>
            <v-card-text style="max-height: 60vh; overflow-y: auto;">
              <div v-for="m in (familyMap[primary.content_sign] || [])"
                   :key="m.data_resources_id" class="member-detail-row">
                <span>{{ m.resources_name }}</span>
                <span class="ml-2 text-caption">{{ m.family_relation }}</span>
              </div>
            </v-card-text>
          </v-card>
        </v-dialog>

        <v-divider class="my-3" />

        <v-checkbox
          v-model="skipNextTime"
          label="以后总是按当前默认行为认领，不再询问"
          hide-details density="compact"
        />

        <div class="summary mt-2">
          汇总：{{ selectedPrimaries.length }} 主 + {{ totalIds.length - selectedPrimaries.length }} 相似
          <span v-if="totalSkipped > 0" class="text-warning ml-2">（{{ totalSkipped }} 个相似已被认领，将跳过）</span>
        </div>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="close">取消</v-btn>
        <v-btn color="primary" data-test="batch-confirm-btn" @click="confirm">
          确认认领 {{ totalIds.length }} 个
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.global-bar {
  background: #eef5ff; border: 1px solid #c5e1ff;
  padding: 10px 14px; border-radius: 6px;
  font-size: 13px; color: #1565c0;
  display: flex; align-items: center; gap: 12px;
}
.batch-row { border: 1px solid #e0e0e0; border-radius: 6px; margin-bottom: 6px; }
.row-main { display: flex; align-items: center; padding: 10px 12px; gap: 8px; }
.fname { flex: 1; font-size: 13px; }
.row-expand { background: #fafafa; padding: 8px 14px; border-top: 1px solid #e0e0e0; }
.member-detail-row { padding: 4px 0; font-size: 12px; display: flex; align-items: center; }
.summary { font-size: 12px; padding: 8px 10px; background: #f0f4ff; border-radius: 6px; }
</style>
```

- [ ] **Step 4: 跑测试验证通过**

```bash
yarn test --run __tests__/ClaimFamilyDialogBatch.test.ts
# 期望：4 个测试全 PASS
```

- [ ] **Step 5: Commit**

```bash
git add frontend_real/components/ClaimFamilyDialogBatch.vue \
        frontend_real/__tests__/ClaimFamilyDialogBatch.test.ts
git commit -m "feat(claim): batch claim dialog with global default + per-row override (方案 B)"
```

---

## Task 13：前端 ClaimView 接入弹窗触发逻辑

**Files:**
- Modify: `frontend_real/views/ClaimView.vue`
- Test: `frontend_real/__tests__/ClaimView.dialogTrigger.test.ts`

- [ ] **Step 1: 写失败测试**

```typescript
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import ClaimView from '@/views/ClaimView.vue'

const vuetify = createVuetify({ components })

const baseMocks = (overrides: Partial<Record<string, any>> = {}) => ({
  api: {
    getConfig: vi.fn(async () => overrides.config ?? { claim_family_skip_dialog: 'false', claim_family_default_policy: 'same_content_only' }),
    getResourcesStatistics: vi.fn(async () => ({})),
    getResources: vi.fn(async () => overrides.resources ?? { resources: [], total: 0, page: 1, pageSize: 50 }),
    getLatestSimilarityTask: vi.fn(async () => null),
    batchFamilyMembers: vi.fn(async () => overrides.familyMap ?? {}),
    batchClaim: vi.fn(async () => ({ updatedCount: 1, success: true })),
    saveConfig: vi.fn(async () => ({ success: true })),
  },
  userInfoManager: { getUserInfo: vi.fn(async () => ({ user_name: 'tester', company_name: 'co' })) },
})

const mkRes = (id: number, name: string, contentSign: string, familyMemberCount = 0) => ({
  data_resources_id: id, content_sign: contentSign, resources_name: name,
  source_count: 1, claim_status: 0, family_id: familyMemberCount > 0 ? 10 : null,
  family_member_count: familyMemberCount, family_relation: 'primary',
  family_same_content_count: 0, family_process_version_count: 0, family_derived_count: 0,
  first_create_time: '2026-01-01', primary_path: '/x/' + name,
})

describe('ClaimView 弹窗触发', () => {
  it('selecting 1 row + 认领 opens single-file dialog', async () => {
    const resources = [mkRes(1, 'p.docx', 'CS_1', 3)]
    const familyMap = { 'CS_1': [
      { data_resources_id: 1, family_relation: 'primary',      content_sign: 'CS_1', resources_name: 'p.docx',  source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
      { data_resources_id: 11, family_relation: 'same_content', content_sign: 'CS_11', resources_name: 'pp.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
    ] }
    const mocks = baseMocks({ resources: { resources, total: 1, page: 1, pageSize: 50 }, familyMap })
    vi.doMock('@/services/api', () => ({ api: mocks.api }))
    vi.doMock('@/services/UserInfoManager', () => ({ userInfoManager: mocks.userInfoManager }))

    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] } })
    await flushPromises()
    // 模拟勾选第 1 行
    ;(wrapper.vm as any).selectedIds = [1]
    await wrapper.vm.$nextTick()
    // 点认领下拉中的"个人工作"
    await (wrapper.vm as any).handleClaim(2)
    await flushPromises()

    expect(mocks.api.batchFamilyMembers).toHaveBeenCalled()
    expect(wrapper.findComponent({ name: 'ClaimFamilyDialogSingle' }).exists()).toBe(true)
  })

  it('selecting 2+ rows + 认领 opens batch dialog', async () => {
    const resources = [mkRes(1, 'a.docx', 'CS_A', 2), mkRes(2, 'b.docx', 'CS_B', 1)]
    const familyMap = {
      'CS_A': [{ data_resources_id: 1, family_relation: 'primary', content_sign: 'CS_A', resources_name: 'a.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null }],
    }
    const mocks = baseMocks({ resources: { resources, total: 2, page: 1, pageSize: 50 }, familyMap })
    vi.doMock('@/services/api', () => ({ api: mocks.api }))
    vi.doMock('@/services/UserInfoManager', () => ({ userInfoManager: mocks.userInfoManager }))

    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] } })
    await flushPromises()
    ;(wrapper.vm as any).selectedIds = [1, 2]
    await wrapper.vm.$nextTick()
    await (wrapper.vm as any).handleClaim(2)
    await flushPromises()

    expect(wrapper.findComponent({ name: 'ClaimFamilyDialogBatch' }).exists()).toBe(true)
  })

  it('all selected rows have no family → bypasses dialog, calls batchClaim directly', async () => {
    const resources = [mkRes(1, 'solo.docx', 'CS_S', 0)]
    const mocks = baseMocks({ resources: { resources, total: 1, page: 1, pageSize: 50 }, familyMap: {} })
    vi.doMock('@/services/api', () => ({ api: mocks.api }))
    vi.doMock('@/services/UserInfoManager', () => ({ userInfoManager: mocks.userInfoManager }))

    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] } })
    await flushPromises()
    ;(wrapper.vm as any).selectedIds = [1]
    await (wrapper.vm as any).handleClaim(2)
    await flushPromises()

    expect(mocks.api.batchClaim).toHaveBeenCalledWith(expect.objectContaining({ ids: [1] }))
    expect(wrapper.findComponent({ name: 'ClaimFamilyDialogSingle' }).exists()).toBe(false)
  })

  it('skip_dialog=true → bypasses dialog, applies default policy', async () => {
    const resources = [mkRes(1, 'p.docx', 'CS_1', 3)]
    const familyMap = { 'CS_1': [
      { data_resources_id: 1, family_relation: 'primary', content_sign: 'CS_1', resources_name: 'p.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
      { data_resources_id: 11, family_relation: 'same_content', content_sign: 'CS_11', resources_name: 'pp.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
    ] }
    const mocks = baseMocks({
      config: { claim_family_skip_dialog: 'true', claim_family_default_policy: 'same_content_only' },
      resources: { resources, total: 1, page: 1, pageSize: 50 },
      familyMap,
    })
    vi.doMock('@/services/api', () => ({ api: mocks.api }))
    vi.doMock('@/services/UserInfoManager', () => ({ userInfoManager: mocks.userInfoManager }))

    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] } })
    await flushPromises()
    ;(wrapper.vm as any).selectedIds = [1]
    await (wrapper.vm as any).handleClaim(2)
    await flushPromises()

    // 直接调 batchClaim，不弹窗，IDs 含 primary + same_content
    expect(mocks.api.batchClaim).toHaveBeenCalledWith(expect.objectContaining({ ids: expect.arrayContaining([1, 11]) }))
    expect(wrapper.findComponent({ name: 'ClaimFamilyDialogSingle' }).exists()).toBe(false)
  })
})
```

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 改写 ClaimView.vue handleClaim**

```vue
<script setup lang="ts">
// 新增 import
import ClaimFamilyDialogSingle from '@/components/ClaimFamilyDialogSingle.vue'
import ClaimFamilyDialogBatch from '@/components/ClaimFamilyDialogBatch.vue'
import type { FamilyMemberDetail } from '@/services/api'

// 新增 state
const singleDialogOpen = ref(false)
const batchDialogOpen = ref(false)
const dialogPrimary = ref<FamilyMemberDetail | null>(null)
const dialogMembers = ref<FamilyMemberDetail[]>([])
const dialogBatchPrimaries = ref<FamilyMemberDetail[]>([])
const dialogBatchFamilyMap = ref<Record<string, FamilyMemberDetail[]>>({})
const dialogClaimStatus = ref(0)

// 重写 handleClaim
const handleClaim = async (claimStatus: number) => {
  if (!canClaim.value) {
    showSnackbar('请先选择要认领的资源', 'warning')
    return
  }

  const userInfo = await userInfoManager.getUserInfo()
  if (!userInfo) {
    showSnackbar('请先登录', 'warning')
    return
  }

  dialogClaimStatus.value = claimStatus

  // 获取被选中资源的 content_signs
  const selectedResources = resources.value.filter(r => selectedIds.value.includes(r.data_resources_id))
  const contentSigns = selectedResources.map(r => r.content_sign).filter(Boolean) as string[]

  // 批量查 family
  const familyMap = await api.batchFamilyMembers(contentSigns)

  // 全部无家族 → 走原流程
  if (Object.keys(familyMap).length === 0) {
    await doClaim(claimStatus, selectedIds.value, userInfo)
    return
  }

  // 配置：跳过弹窗
  if (config.value?.claim_family_skip_dialog === 'true') {
    const policy = config.value.claim_family_default_policy || 'same_content_only'
    const ids = buildIdsByPolicy(selectedResources, familyMap, policy)
    await doClaim(claimStatus, ids, userInfo)
    return
  }

  // 转入弹窗
  if (selectedResources.length === 1) {
    const primary = selectedResources[0] as unknown as FamilyMemberDetail
    dialogPrimary.value = primary
    dialogMembers.value = familyMap[primary.content_sign] || [primary]
    singleDialogOpen.value = true
  } else {
    dialogBatchPrimaries.value = selectedResources as unknown as FamilyMemberDetail[]
    dialogBatchFamilyMap.value = familyMap
    batchDialogOpen.value = true
  }
}

const buildIdsByPolicy = (
  primaries: any[], familyMap: Record<string, FamilyMemberDetail[]>, policy: string
): number[] => {
  const ids: number[] = []
  for (const p of primaries) {
    ids.push(p.data_resources_id)
    const members = familyMap[p.content_sign] || []
    for (const m of members) {
      if (m.data_resources_id === p.data_resources_id) continue
      if (m.claim_status !== 0) continue  // 跳过已认领
      if (policy === 'all') ids.push(m.data_resources_id)
      else if (policy === 'same_content_only' && m.family_relation === 'same_content') {
        ids.push(m.data_resources_id)
      }
    }
  }
  return [...new Set(ids)]  // 去重
}

const onSingleConfirm = async (payload: { ids: number[]; skipNextTime: boolean }) => {
  const userInfo = await userInfoManager.getUserInfo()
  if (!userInfo) return
  await doClaim(dialogClaimStatus.value, payload.ids, userInfo)
  if (payload.skipNextTime) await api.saveConfig({ claim_family_skip_dialog: 'true' } as any)
}

const onBatchConfirm = async (payload: { ids: number[]; skipNextTime: boolean }) => {
  const userInfo = await userInfoManager.getUserInfo()
  if (!userInfo) return
  await doClaim(dialogClaimStatus.value, payload.ids, userInfo)
  if (payload.skipNextTime) await api.saveConfig({ claim_family_skip_dialog: 'true' } as any)
}

const doClaim = async (claimStatus: number, ids: number[], userInfo: any) => {
  submitting.value = true
  try {
    const result = await api.batchClaim({
      ids,
      is_claimed: 1,
      claim_status: claimStatus,
      claimant_name: userInfo.user_name,
      claimant_unit: userInfo.company_name,
    })
    showSnackbar(`成功认领 ${result.updatedCount} 条资源`, 'success')
    await loadResources()
    loadStatistics()
  } catch (e) {
    showSnackbar('认领失败：' + (e as Error).message, 'error')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <!-- 现有 template ... -->

  <!-- 末尾追加弹窗 -->
  <ClaimFamilyDialogSingle
    v-if="dialogPrimary"
    v-model="singleDialogOpen"
    :primary="dialogPrimary"
    :members="dialogMembers"
    :claim-status="dialogClaimStatus"
    @confirm="onSingleConfirm"
  />
  <ClaimFamilyDialogBatch
    v-model="batchDialogOpen"
    :selected-primaries="dialogBatchPrimaries"
    :family-map="dialogBatchFamilyMap"
    :claim-status="dialogClaimStatus"
    :default-policy="(config?.claim_family_default_policy as any) || 'same_content_only'"
    @confirm="onBatchConfirm"
  />
</template>
```

- [ ] **Step 4: 跑测试验证通过**

- [ ] **Step 5: Commit**

```bash
git add frontend_real/views/ClaimView.vue frontend_real/__tests__/ClaimView.dialogTrigger.test.ts
git commit -m "feat(claim): wire single/batch claim dialogs into ClaimView handleClaim"
```

---

## Task 14：端到端集成测试

**Files:**
- Test: `internal/httpd/claim_family_e2e_test.go`

- [ ] **Step 1: 写端到端测试覆盖关键路径**

```go
// internal/httpd/claim_family_e2e_test.go
func TestE2E_BatchClaimWithFamilyMembers(t *testing.T) {
    r, db, cleanup := setupTestServer(t)
    defer cleanup()

    // seed: 1 family（1 primary + 1 same + 1 derived）+ 1 单独资源
    now := time.Now()
    _, _ = db.Exec(`INSERT INTO data_resources (
        content_sign, source_count, workspace_source_count, first_create_time,
        resources_name, claim_status, importance_level, family_id, family_relation,
        create_time, update_time, disable, data_origin
    ) VALUES
        ('CS_F_P',  1, 0, ?, 'fam-p.docx', 0, 0, 100, 'primary',      ?, ?, 0, 'historical'),
        ('CS_F_M1', 1, 0, ?, 'fam-m1.docx',0, 0, 100, 'same_content', ?, ?, 0, 'historical'),
        ('CS_F_M2', 1, 0, ?, 'fam-m2.docx',0, 0, 100, 'derived',      ?, ?, 0, 'historical'),
        ('CS_SOLO', 1, 0, ?, 'solo.docx',  0, 0, NULL, NULL,           ?, ?, 0, 'historical')`,
        now, now, now, now, now, now, now, now, now, now, now, now, now, now)

    // 1. preview
    s1, b1 := jsonReqNoBody(t, r, "POST", "/analyze/preview")
    successOk(t, s1, b1)

    // 2. batch members
    s2, b2 := jsonReq(t, r, "POST", "/family/batch-members",
        `{"content_signs":["CS_F_P","CS_SOLO"]}`)
    successOk(t, s2, b2)
    var bm struct { Data map[string][]map[string]interface{} `json:"data"` }
    json.Unmarshal([]byte(b2), &bm)
    if len(bm.Data["CS_F_P"]) != 3 {
        t.Errorf("F members = %d, want 3", len(bm.Data["CS_F_P"]))
    }
    if _, ok := bm.Data["CS_SOLO"]; ok {
        t.Errorf("solo should not appear")
    }

    // 3. claim primary + same_content member（模拟前端组装的 IDs）
    var rows []struct {
        ID int64 `db:"data_resources_id"`
        CS string `db:"content_sign"`
    }
    db.Select(&rows, "SELECT data_resources_id, content_sign FROM data_resources WHERE content_sign IN ('CS_F_P','CS_F_M1') ORDER BY content_sign")
    if len(rows) != 2 { t.Fatalf("seed broken: %d rows", len(rows)) }

    body := fmt.Sprintf(`{"ids":[%d,%d],"is_claimed":1,"claim_status":2,"claimant_name":"E2E","claimant_unit":"Test"}`,
        rows[0].ID, rows[1].ID)
    s3, b3 := jsonReq(t, r, "POST", "/resources/claim", body)
    successOk(t, s3, b3)

    // 4. 验证 CS_F_P + CS_F_M1 claim_status=2，CS_F_M2 仍为 0（derived 未带）
    var statuses []int
    db.Select(&statuses, "SELECT claim_status FROM data_resources WHERE content_sign IN ('CS_F_P','CS_F_M1','CS_F_M2') ORDER BY content_sign")
    if statuses[0] != 2 || statuses[1] != 2 || statuses[2] != 0 {
        t.Errorf("statuses = %v, want [2,2,0]", statuses)
    }
}
```

- [ ] **Step 2: 跑测试验证通过**

```bash
go test ./internal/httpd/ -run TestE2E_BatchClaimWithFamilyMembers -v
```

- [ ] **Step 3: 跑全部回归测试，确认无破坏**

```bash
go test ./... -count=1
# 期望：全部 PASS

cd frontend_real
yarn test --run
# 期望：原有 64/15 baseline + 本 spec 新增测试全 PASS
```

- [ ] **Step 4: Commit**

```bash
git add internal/httpd/claim_family_e2e_test.go
git commit -m "test(e2e): batch claim with family members end-to-end"
```

---

## 验收 checklist

- [ ] Task 1：system_config 默认值已 seed
- [ ] Task 2-3：`POST /family/batch-members` 接口工作
- [ ] Task 4：`POST /analyze/preview` 接口工作
- [ ] Task 5：扫描完成自动触发 BuildFamilies
- [ ] Task 6：前端 api.ts 类型与方法就绪
- [ ] Task 7：`RebuildSimilarityDialog` 组件 + 测试通过
- [ ] Task 8：列表 primary 行有"关联 N ▾" chip 紧贴文件名
- [ ] Task 9：按钮改名 + 接入二次确认对话框
- [ ] Task 10：系统设置卡片 + 配置持久化
- [ ] Task 11：单文件认领弹窗 + 测试通过
- [ ] Task 12：批量认领弹窗（方案 B）+ 测试通过
- [ ] Task 13：ClaimView 弹窗触发逻辑接入 + 4 个场景测试通过
- [ ] Task 14：端到端集成测试通过
- [ ] 全套回归测试通过（`go test ./...` + `yarn test --run`）

---

## 风险与回滚

- 每个 Task 单独 commit，出问题可单独 revert
- 紧急关闭：把 ClaimView.vue 的 `handleClaim` 改回直接调 `doClaim(claimStatus, selectedIds.value, userInfo)`，绕过弹窗 → 5 分钟回滚
- 后端新增接口、配置项均向后兼容，不影响存量数据
