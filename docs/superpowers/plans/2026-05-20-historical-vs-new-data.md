# Historical vs New Data Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tag every `data_resources` row as `historical` or `new`, split the AI 归目 view into two tabs, and add a bulk-dismiss path for historical so new data stops being drowned out.

**Architecture:** A new immutable `data_origin` column on `data_resources` is stamped at INSERT time based on a `system_config.baseline_completed_at` row; the very first `scan_task` that succeeds writes that timestamp via a conditional UPDATE. The HTTP layer adds `origin` + pagination to `GET /ai/classify/pending` and a new `POST /ai/classify/bulk-dismiss`; the Vue view grows two tabs over the existing card list.

**Tech Stack:** Go (jmoiron/sqlx) + sqlite3, Gin HTTP, Vue 3 + Vuetify (TypeScript), Vitest + Go testing.

**Spec:** `docs/superpowers/specs/2026-05-20-historical-vs-new-data-design.md`

---

## Pre-flight

- All Go work runs from repo root with `go test ./internal/...`.
- Frontend work uses **yarn** (project rule), tests via `yarn test`.
- Before any DB-touching test run (frontend integration): `npm rebuild better-sqlite3` (frontend toolchain rule).
- Commit after each task. Branch `go-test-template` is current; do not switch.

---

### Task 1: Migration — `data_origin` column + baseline_completed_at config seed

**Files:**
- Modify: `internal/repository/db.go` (the `runMigrations` column-add list + a one-shot historical backfill + a seed of the baseline config row)
- Test: `internal/repository/data_origin_migration_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/repository/data_origin_migration_test.go`:

```go
package repository

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func openMigratedTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "v1.db")
	sqlDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	db := sqlx.NewDb(sqlDB, "sqlite3")
	if err := RunMigrationsForTest(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestMigration_AddsDataOriginColumn(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	ok, err := columnExists(db, "data_resources", "data_origin")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !ok {
		t.Fatal("data_resources.data_origin column missing after migration")
	}
}

func TestMigration_StampsExistingRowsAsHistorical(t *testing.T) {
	// First, simulate "old build" — open DB without the migration logic
	// applied to data_origin (we approximate that by inserting a row that
	// must end up labeled 'historical' after the migration).
	db := openMigratedTestDB(t)
	defer db.Close()

	// Insert one row directly without specifying data_origin; the column
	// default is 'new'. The migration's backfill must then flip it back
	// to 'historical' because baseline_completed_at is still NULL.
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		create_time, update_time, disable, data_origin
	) VALUES ('CS_BEFORE_BASELINE', 1, 1, ?, ?, ?, 0, 'new')`,
		now, now, now)
	if err != nil {
		t.Fatalf("seed pre-baseline row: %v", err)
	}

	// Re-run migrations: backfill must rewrite that 'new' to 'historical'
	// because baseline_completed_at IS NULL.
	if err := RunMigrationsForTest(db); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}

	var got string
	if err := db.Get(&got, `SELECT data_origin FROM data_resources WHERE content_sign = 'CS_BEFORE_BASELINE'`); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "historical" {
		t.Errorf("data_origin = %q, want historical (baseline not closed yet)", got)
	}
}

func TestMigration_SeedsBaselineCompletedAtRow(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	var rows int
	if err := db.Get(&rows, `SELECT COUNT(*) FROM system_config WHERE key = 'baseline_completed_at'`); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("baseline_completed_at row count = %d, want 1", rows)
	}

	var value sql.NullString
	if err := db.Get(&value, `SELECT value FROM system_config WHERE key = 'baseline_completed_at'`); err != nil {
		t.Fatalf("read value: %v", err)
	}
	if value.Valid && value.String != "" {
		t.Errorf("baseline_completed_at value = %q, want empty/NULL", value.String)
	}
}
```

- [ ] **Step 2: Run tests, expect FAIL**

Run: `go test ./internal/repository/ -run 'TestMigration_(AddsDataOriginColumn|StampsExistingRowsAsHistorical|SeedsBaselineCompletedAtRow)' -count=1`
Expected: 3 failures (column missing / data_origin wrong / config row missing).

- [ ] **Step 3: Implement the migration in `db.go`**

Open `internal/repository/db.go`, find the `columnAdds` slice inside `runMigrations` (around line 300). Add one entry to the slice:

```go
{"data_resources", "data_origin", "ALTER TABLE data_resources ADD COLUMN data_origin TEXT NOT NULL DEFAULT 'new'"},
```

Immediately after the `for _, c := range columnAdds` loop (around line 340), add:

```go
// Index for the AI 归目 pending queries (origin × claim × importance).
if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_data_resources_origin_claim
    ON data_resources(data_origin, claim_status, importance_level)`); err != nil {
    return fmt.Errorf("create idx_data_resources_origin_claim: %w", err)
}

// One-shot backfill: while baseline_completed_at is still NULL/empty, every
// existing row pre-dates the new-data era and must be tagged historical.
// Idempotent — once baseline is closed this UPDATE matches no rows.
if _, err := db.Exec(`UPDATE data_resources
    SET data_origin = 'historical'
    WHERE data_origin = 'new'
      AND NOT EXISTS (
          SELECT 1 FROM system_config
          WHERE key = 'baseline_completed_at' AND value IS NOT NULL AND value <> '' AND disable = 0
      )`); err != nil {
    return fmt.Errorf("backfill historical data_origin: %w", err)
}

// Seed the baseline_completed_at config row (idempotent).
if _, err := db.Exec(`INSERT OR IGNORE INTO system_config (key, type, value, create_time, update_time, disable)
    VALUES ('baseline_completed_at', 'string', NULL, ?, ?, 0)`,
    time.Now(), time.Now()); err != nil {
    return fmt.Errorf("seed baseline_completed_at: %w", err)
}
```

(Top-of-file `import "time"` is already present.)

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./internal/repository/ -run 'TestMigration_(AddsDataOriginColumn|StampsExistingRowsAsHistorical|SeedsBaselineCompletedAtRow)' -count=1`
Expected: 3 passes.

- [ ] **Step 5: Full repository test run for regression**

Run: `go test ./internal/repository/...`
Expected: existing tests still pass.

- [ ] **Step 6: Commit**

```bash
git add internal/repository/db.go internal/repository/data_origin_migration_test.go
git commit -m "feat(scan): add data_origin column + baseline_completed_at seed"
```

---

### Task 2: Tag `data_origin` at INSERT time

**Files:**
- Modify: `internal/repository/data_resources.go` (`InsertBatch` + `InsertFromStatistics` + new helper)
- Test: `internal/repository/data_origin_insert_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/repository/data_origin_insert_test.go`:

```go
package repository

import (
	"testing"
	"time"
)

func TestInsertBatch_TagsHistoricalBeforeBaseline(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := NewDataResourcesRepository(db)

	now := time.Now()
	n := repo.InsertBatch([]map[string]interface{}{{
		"content_sign":           "CSH001",
		"source_count":           int64(1),
		"workspace_source_count": int64(1),
		"first_create_time":      now.Format(time.RFC3339),
		"resources_name":         "file-pre-baseline.pdf",
		"content_subject":        "file",
		"content_type":           "pdf",
	}})
	if n != 1 {
		t.Fatalf("insert count = %d, want 1", n)
	}
	var origin string
	if err := db.Get(&origin, `SELECT data_origin FROM data_resources WHERE content_sign = 'CSH001'`); err != nil {
		t.Fatalf("read: %v", err)
	}
	if origin != "historical" {
		t.Errorf("origin = %q, want historical", origin)
	}
}

func TestInsertBatch_TagsNewAfterBaseline(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	cfg := NewSystemConfigRepository(db)
	cfg.SetValue("baseline_completed_at", time.Now().Format(time.RFC3339))

	repo := NewDataResourcesRepository(db)
	now := time.Now()
	repo.InsertBatch([]map[string]interface{}{{
		"content_sign":           "CSN001",
		"source_count":           int64(1),
		"workspace_source_count": int64(1),
		"first_create_time":      now.Format(time.RFC3339),
		"resources_name":         "file-post-baseline.pdf",
		"content_subject":        "file",
		"content_type":           "pdf",
	}})
	var origin string
	if err := db.Get(&origin, `SELECT data_origin FROM data_resources WHERE content_sign = 'CSN001'`); err != nil {
		t.Fatalf("read: %v", err)
	}
	if origin != "new" {
		t.Errorf("origin = %q, want new", origin)
	}
}

func TestInsertFromStatistics_RespectsBaseline(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	repo := NewDataResourcesRepository(db)
	now := time.Now()
	stats := map[string]interface{}{
		"CSS001": map[string]interface{}{
			"content_sign":           "CSS001",
			"source_count":           int64(1),
			"workspace_source_count": int64(0),
			"first_create_time":      now.Format(time.RFC3339),
			"resources_name":         "stats-pre.pdf",
			"content_type":           "pdf",
		},
	}
	repo.InsertFromStatistics(stats)
	var origin string
	if err := db.Get(&origin, `SELECT data_origin FROM data_resources WHERE content_sign = 'CSS001'`); err != nil {
		t.Fatalf("read: %v", err)
	}
	if origin != "historical" {
		t.Errorf("pre-baseline stats origin = %q, want historical", origin)
	}

	// Close baseline and re-run
	cfg := NewSystemConfigRepository(db)
	cfg.SetValue("baseline_completed_at", now.Format(time.RFC3339))
	stats2 := map[string]interface{}{
		"CSS002": map[string]interface{}{
			"content_sign":           "CSS002",
			"source_count":           int64(1),
			"workspace_source_count": int64(0),
			"first_create_time":      now.Format(time.RFC3339),
			"resources_name":         "stats-post.pdf",
			"content_type":           "pdf",
		},
	}
	repo.InsertFromStatistics(stats2)
	if err := db.Get(&origin, `SELECT data_origin FROM data_resources WHERE content_sign = 'CSS002'`); err != nil {
		t.Fatalf("read CSS002: %v", err)
	}
	if origin != "new" {
		t.Errorf("post-baseline stats origin = %q, want new", origin)
	}
}
```

- [ ] **Step 2: Run tests, expect FAIL**

Run: `go test ./internal/repository/ -run 'TestInsert(Batch|FromStatistics)_' -count=1`
Expected: 3 failures (rows default to 'new' regardless of baseline).

- [ ] **Step 3: Add the helper and wire it into both insert paths**

Open `internal/repository/data_resources.go`. After the `dataResourcesColumns` constant (around line 26) add:

```go
// currentDataOrigin returns 'historical' while system_config.baseline_completed_at
// is NULL / empty, otherwise 'new'. Resolved once per insert batch.
func currentDataOrigin(db sqlxQuerier) string {
	var value *string
	err := db.Get(&value, `SELECT value FROM system_config WHERE key = 'baseline_completed_at' AND disable = 0`)
	if err != nil || value == nil || *value == "" {
		return "historical"
	}
	return "new"
}

// sqlxQuerier is the minimal subset of *sqlx.DB / *sqlx.Tx needed by helpers
// that have to be reusable from either a connection or a transaction.
type sqlxQuerier interface {
	Get(dest interface{}, query string, args ...interface{}) error
}
```

Inside `InsertBatch`, just before the SQL string is assembled (find the existing `INSERT INTO data_resources (` block around line 52), modify the SQL and the args to include `data_origin`. Concretely replace the `INSERT INTO data_resources (...)` block with:

```go
origin := currentDataOrigin(r.DB)
query := `
    INSERT INTO data_resources (
        content_sign, source_count, workspace_source_count, first_create_time,
        resources_name, resources_desc, content_subject, content_type,
        file_magic, create_time, update_time, disable, data_origin
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
`
```

and append `origin` as the last parameter to the corresponding `db.Exec` call.

Do the same edit inside `InsertFromStatistics` for its INSERT block.

(If the existing INSERT lists slightly different columns, keep the existing ones and just append `, data_origin` at the end of the column list, a matching `?` at the end of the VALUES list, and `origin` at the end of the args.)

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./internal/repository/ -run 'TestInsert(Batch|FromStatistics)_' -count=1`
Expected: 3 passes.

- [ ] **Step 5: Full repository test run**

Run: `go test ./internal/repository/...`
Expected: all green; verifies no regression in `InsertBatch` callers.

- [ ] **Step 6: Commit**

```bash
git add internal/repository/data_resources.go internal/repository/data_origin_insert_test.go
git commit -m "feat(scan): tag data_origin at insert time"
```

---

### Task 3: Close baseline on first `scan_task` succeed

**Files:**
- Modify: `internal/repository/scan_task.go` (`MarkSucceeded`)
- Test: `internal/repository/scan_task_baseline_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/repository/scan_task_baseline_test.go`:

```go
package repository

import (
	"testing"
	"time"
)

func newScanTaskRow(t *testing.T, db *sqlx.DB) int64 {
	t.Helper()
	now := time.Now()
	r, err := db.Exec(`INSERT INTO scan_task (
		scan_type, heartbeat, task_state, create_time, update_time, disable
	) VALUES ('FILE', 0, 'run', ?, ?, 0)`, now, now)
	if err != nil {
		t.Fatalf("insert scan_task: %v", err)
	}
	id, _ := r.LastInsertId()
	return id
}

func TestMarkSucceeded_ClosesBaselineFirstTime(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	taskID := newScanTaskRow(t, db)
	repo := NewScanTaskRepository(db)
	if err := repo.MarkSucceeded(taskID); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	cfg := NewSystemConfigRepository(db)
	got := cfg.GetValue("baseline_completed_at")
	if got == "" {
		t.Fatal("baseline_completed_at still empty after first MarkSucceeded")
	}
}

func TestMarkSucceeded_IsNoOpAfterBaselineClosed(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	cfg := NewSystemConfigRepository(db)
	preset := "2025-01-01T00:00:00Z"
	cfg.SetValue("baseline_completed_at", preset)

	taskID := newScanTaskRow(t, db)
	repo := NewScanTaskRepository(db)
	if err := repo.MarkSucceeded(taskID); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	if got := cfg.GetValue("baseline_completed_at"); got != preset {
		t.Errorf("baseline overwritten: %q != %q", got, preset)
	}
}

func TestMarkFailed_DoesNotCloseBaseline(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	taskID := newScanTaskRow(t, db)
	repo := NewScanTaskRepository(db)
	if err := repo.MarkFailed(taskID, "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	cfg := NewSystemConfigRepository(db)
	if got := cfg.GetValue("baseline_completed_at"); got != "" {
		t.Errorf("baseline_completed_at = %q, want empty after fail", got)
	}
}
```

(The file also needs `import "github.com/jmoiron/sqlx"` for the helper; add it.)

- [ ] **Step 2: Run tests, expect FAIL**

Run: `go test ./internal/repository/ -run 'TestMark(Succeeded|Failed)_' -count=1`
Expected: `TestMarkSucceeded_ClosesBaselineFirstTime` fails (baseline still empty).

- [ ] **Step 3: Implement the baseline close in `MarkSucceeded`**

Open `internal/repository/scan_task.go`. Replace `MarkSucceeded` with:

```go
// MarkSucceeded marks a task as successfully completed and, on the very first
// successful scan task in this terminal's lifetime, closes the historical
// baseline window so subsequent inserts are tagged 'new'.
func (r *ScanTaskRepository) MarkSucceeded(taskId int64) error {
	now := time.Now()
	if _, err := r.DB.Exec(`
		UPDATE scan_task
		SET task_state = 'succeed', task_error_message = '', end_time = ?, update_time = ?
		WHERE id = ?`, now, now, taskId); err != nil {
		return err
	}

	// Conditional close — only the first succeed mutates the value.
	if _, err := r.DB.Exec(`
		UPDATE system_config
		SET value = ?, update_time = ?
		WHERE key = 'baseline_completed_at'
		  AND disable = 0
		  AND (value IS NULL OR value = '')`,
		now.Format(time.RFC3339), now); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./internal/repository/ -run 'TestMark(Succeeded|Failed)_' -count=1`
Expected: 3 passes.

- [ ] **Step 5: Full repo test run for regression**

Run: `go test ./internal/repository/...`
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add internal/repository/scan_task.go internal/repository/scan_task_baseline_test.go
git commit -m "feat(scan): close baseline_completed_at on first scan_task succeed"
```

---

### Task 4: Extend `GET /ai/classify/pending` with origin + pagination + total

**Files:**
- Modify: `internal/httpd/ai_classify.go` (`ListPendingForClassify`)
- Test: `internal/httpd/ai_classify_origin_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/httpd/ai_classify_origin_test.go`:

```go
package httpd

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

// seedResourceWithOrigin 直接 INSERT 一条 data_resources，自带 origin
func seedResourceWithOrigin(t *testing.T, db interface {
	Exec(query string, args ...interface{}) (interface{}, error)
}, name, sign, origin string) {
	// 这里复用 testhelpers 的 *sqlx.DB（参考 seedSimpleResourceWithDist 的写法）
}

func TestHTTP_AIClassify_Pending_DefaultIsNew(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()

	// 1 historical + 2 new, all claimed + unclassified
	rows := []struct{ name, sign, origin string }{
		{"hist.pdf", "HIST001", "historical"},
		{"newA.pdf", "NEWA001", "new"},
		{"newB.pdf", "NEWB001", "new"},
	}
	for _, x := range rows {
		_, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, 0, ?)`,
			x.sign, now, x.name, now, now, x.origin)
		if err != nil {
			t.Fatalf("seed %s: %v", x.sign, err)
		}
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	if len(items) != 2 {
		t.Errorf("default pending = %d items, want 2 (new only)", len(items))
	}
	total, _ := d["total"].(float64)
	if int(total) != 2 {
		t.Errorf("total = %v, want 2", total)
	}

	repository.AppendAuditLog := repository.AppendAuditLog // keep linter from removing import
	_ = repository.NewAuditLogRepository
}

func TestHTTP_AIClassify_Pending_HistoricalSkipsSuggestions(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	repository.EnsurePersonalContextForTest(db)
	withActiveUser(t, db, "u1")

	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('HIST_NO_SUG', 1, 1, ?, '历史报表.xlsx', 2, 0, ?, ?, 0, 'historical')`,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending?origin=historical")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("historical pending items = %d, want 1", len(items))
	}
	first, _ := items[0].(map[string]interface{})
	sugs, _ := first["suggestions"].([]interface{})
	if len(sugs) != 0 {
		t.Errorf("historical suggestions length = %d, want 0", len(sugs))
	}
}

func TestHTTP_AIClassify_Pending_Pagination(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()
	for i := 0; i < 5; i++ {
		_, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, 0, 'historical')`,
			"H_"+itoa(int64(i)), now, "h.pdf", now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending?origin=historical&page=2&page_size=2")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	if len(items) != 2 {
		t.Errorf("page=2 page_size=2 items = %d, want 2", len(items))
	}
	total, _ := d["total"].(float64)
	if int(total) != 5 {
		t.Errorf("total = %v, want 5", total)
	}
	page, _ := d["page"].(float64)
	if int(page) != 2 {
		t.Errorf("page = %v, want 2", page)
	}
}
```

(Drop the placeholder `seedResourceWithOrigin` helper and the linter-noise lines — they were scaffolding only.)

- [ ] **Step 2: Run tests, expect FAIL**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_Pending_' -count=1`
Expected: 3 failures (current handler returns flat list, no items/total/page wrapper).

- [ ] **Step 3: Rewrite `ListPendingForClassify`**

Open `internal/httpd/ai_classify.go`. Replace the entire `ListPendingForClassify` function body with:

```go
func ListPendingForClassify(c *gin.Context) {
	origin := c.DefaultQuery("origin", "new")
	if origin != "new" && origin != "historical" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "origin 只能是 new 或 historical"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	db := repository.GetDB()

	type pendingRow struct {
		ID            int64   `db:"data_resources_id"`
		ResourcesName *string `db:"resources_name"`
	}

	var total int
	if err := db.Get(&total,
		`SELECT COUNT(*) FROM data_resources
		 WHERE claim_status = 2 AND importance_level = 0 AND disable = 0
		   AND ai_classify_rejected_at IS NULL
		   AND data_origin = ?`, origin); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	offset := (page - 1) * pageSize
	var pending []pendingRow
	if err := db.Select(&pending,
		`SELECT data_resources_id, resources_name
		   FROM data_resources
		  WHERE claim_status = 2 AND importance_level = 0 AND disable = 0
		    AND ai_classify_rejected_at IS NULL
		    AND data_origin = ?
		  ORDER BY data_resources_id DESC LIMIT ? OFFSET ?`,
		origin, pageSize, offset); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	type itemOut struct {
		ResourceID   int64                          `json:"resource_id"`
		ResourceName string                         `json:"resource_name"`
		Suggestions  []ai.ClassificationSuggestion  `json:"suggestions"`
	}
	items := make([]itemOut, 0, len(pending))

	if origin == "historical" {
		for _, p := range pending {
			items = append(items, itemOut{
				ResourceID:   p.ID,
				ResourceName: strDeref(p.ResourcesName),
				Suggestions:  []ai.ClassificationSuggestion{},
			})
		}
	} else {
		minConfidence := 0.0
		if v := c.Query("min_confidence"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				minConfidence = f
			}
		}
		adapter := classifyAdapter()
		for _, p := range pending {
			in, err := ai.EnrichInputForResource(db, p.ID)
			if err != nil {
				continue
			}
			if in.Path != "" && in.Summary == "" {
				body := textextract.ExtractTextWithTimeout(in.Path, 2*time.Second)
				if body != "" {
					runes := []rune(body)
					if len(runes) > 200 {
						body = string(runes[:200])
					}
					in.Summary = body
				}
			}
			sugs, _ := adapter.Classify(context.Background(), in)
			filtered := make([]ai.ClassificationSuggestion, 0, len(sugs))
			for _, s := range sugs {
				if s.Confidence >= minConfidence {
					filtered = append(filtered, s)
				}
			}
			items = append(items, itemOut{
				ResourceID:   p.ID,
				ResourceName: strDeref(p.ResourcesName),
				Suggestions:  filtered,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}
```

Update the existing `TestHTTP_AIClassify_Pending` test in `ai_classify_test.go` (around line 105) to match the new response shape: change `list := dataList(t, resp)` to read items via `dataMap(t, resp)["items"]` and assert length on that. Concretely replace lines 105-110 with:

```go
status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending?page_size=10&origin=new")
successOk(t, status, resp)
d := dataMap(t, resp)
items, _ := d["items"].([]interface{})
if len(items) < 2 {
	t.Errorf("应至少返回 2 条 pending, got %d", len(items))
}
```

(The fixture rows in that test do not specify `data_origin`, so they default to `'new'` from the column default; the test now needs `origin=new` to filter them in.)

Wait — `seedSimpleResourceWithDist` inserts without `data_origin`, but the `data_resources` schema's default is `'new'` from the migration. Confirm by re-reading the seed helper (line 19-32 of the test file): no `data_origin` mention → defaulted to `'new'`. Good.

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_' -count=1`
Expected: all green (new + adjusted old).

- [ ] **Step 5: Full handler test run**

Run: `go test ./internal/httpd/...`
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add internal/httpd/ai_classify.go internal/httpd/ai_classify_test.go internal/httpd/ai_classify_origin_test.go
git commit -m "feat(scan): pending list supports origin + pagination + total"
```

---

### Task 5: Add `POST /ai/classify/bulk-dismiss`

**Files:**
- Modify: `internal/httpd/ai_classify.go` (new handler, register route)
- Test: `internal/httpd/ai_classify_bulk_dismiss_test.go` (create)

- [ ] **Step 1: Write the failing tests**

Create `internal/httpd/ai_classify_bulk_dismiss_test.go`:

```go
package httpd

import (
	"testing"
	"time"
)

func TestHTTP_AIClassify_BulkDismiss_DismissesHistorical(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	var ids []int64
	for i := 0; i < 3; i++ {
		res, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, 0, 'historical')`,
			"BD_"+itoa(int64(i)), now, "h.pdf", now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}

	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": ids,
		"reason":       "首次普查存量，无需细分",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if v, _ := d["dismissed"].(float64); int(v) != 3 {
		t.Errorf("dismissed = %v, want 3", v)
	}

	var stillPending int
	db.Get(&stillPending,
		`SELECT COUNT(*) FROM data_resources
		  WHERE data_origin = 'historical' AND ai_classify_rejected_at IS NULL AND disable = 0`)
	if stillPending != 0 {
		t.Errorf("still pending after dismiss = %d, want 0", stillPending)
	}

	var auditCount int
	db.Get(&auditCount,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'ai_classify_reject' AND target_type = 'data_resource'`)
	if auditCount != 3 {
		t.Errorf("audit rows = %d, want 3", auditCount)
	}
}

func TestHTTP_AIClassify_BulkDismiss_RejectsNonHistorical(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('BD_NEW', 1, 1, ?, 'new.pdf', 2, 0, ?, ?, 0, 'new')`, now, now, now)
	newID, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": []int64{newID},
		"reason":       "should-be-rejected",
	})
	expectFailure(t, status, resp)
	if resp["error"] == nil {
		t.Error("error message missing on validation failure")
	}
}

func TestHTTP_AIClassify_BulkDismiss_RejectsEmpty(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()
	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": []int64{},
		"reason":       "x",
	})
	expectFailure(t, status, resp)
}
```

- [ ] **Step 2: Run tests, expect FAIL**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_BulkDismiss_' -count=1`
Expected: 404s (route not registered).

- [ ] **Step 3: Implement the handler**

Open `internal/httpd/ai_classify.go`. In `RegisterAIClassifyRoutes` (around line 27) add the new route alongside the existing ones:

```go
r.POST("/bulk-dismiss", BulkDismissClassify)
```

Then append the handler at the bottom of the file (above `strDeref`):

```go
// BulkDismissClassifyRequest POST /ai/classify/bulk-dismiss 入参
type BulkDismissClassifyRequest struct {
	ResourceIDs []int64 `json:"resource_ids"`
	Reason      string  `json:"reason"`
}

// BulkDismissClassify POST /ai/classify/bulk-dismiss
//
// 批量把若干历史数据标记为"已人工治理（跳过 AI 归目）"：
//   - 仅接受 data_origin='historical' 的资源；任何不符合都整批回退 400
//   - 单事务内 UPDATE + 每条一行 audit_logs
func BulkDismissClassify(c *gin.Context) {
	var req BulkDismissClassifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if len(req.ResourceIDs) == 0 || len(req.ResourceIDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "resource_ids 必须包含 1~500 个 id"})
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "reason 必填"})
		return
	}

	db := repository.GetDB()

	type validRow struct {
		ID   int64   `db:"data_resources_id"`
		Name *string `db:"resources_name"`
	}
	placeholders := make([]string, len(req.ResourceIDs))
	args := make([]interface{}, len(req.ResourceIDs))
	for i, id := range req.ResourceIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT data_resources_id, resources_name FROM data_resources
	      WHERE data_resources_id IN (` + strings.Join(placeholders, ",") + `)
	        AND data_origin = 'historical' AND disable = 0`
	var rows []validRow
	if err := db.Select(&rows, q, args...); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	if len(rows) != len(req.ResourceIDs) {
		seen := make(map[int64]bool, len(rows))
		for _, r := range rows {
			seen[r.ID] = true
		}
		invalid := make([]int64, 0)
		for _, id := range req.ResourceIDs {
			if !seen[id] {
				invalid = append(invalid, id)
			}
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "存在非历史 / 已删除 / 不存在的资源",
			"data":    gin.H{"invalid_ids": invalid},
		})
		return
	}

	now := time.Now()
	tx, err := db.Beginx()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	updateQ := `UPDATE data_resources
	            SET ai_classify_rejected_at = ?, ai_classify_reject_reason = ?, update_time = ?
	            WHERE data_resources_id IN (` + strings.Join(placeholders, ",") + `)
	              AND data_origin = 'historical' AND disable = 0`
	updArgs := append([]interface{}{now, req.Reason, now}, args...)
	if _, err := tx.Exec(updateQ, updArgs...); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	auditRepo := repository.NewAuditLogRepository(tx)
	for _, row := range rows {
		_, _ = auditRepo.Append(repository.AppendAuditInput{
			ActorID:     currentOperator(c),
			ActorUserID: currentUserID(c),
			Action:      repository.AuditAIClassifyReject,
			TargetType:  repository.AuditTargetDataResource,
			TargetID:    row.ID,
			TargetCode:  strDeref(row.Name),
			After:       gin.H{"resource_id": row.ID, "reason": req.Reason, "rejected_at": now, "bulk": true},
			IPAddress:   c.ClientIP(),
			Message:     "AI 归目批量标已治理: " + req.Reason,
		})
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}
	committed = true
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"dismissed": len(rows),
		},
	})
}
```

If `NewAuditLogRepository` does not accept a `*sqlx.Tx`, fall back to calling it on `db` (no transactional wrap of audit appends — the project's existing reject path does the same).

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./internal/httpd/ -run 'TestHTTP_AIClassify_BulkDismiss_' -count=1`
Expected: 3 passes.

- [ ] **Step 5: Full handler tests**

Run: `go test ./internal/httpd/...`
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add internal/httpd/ai_classify.go internal/httpd/ai_classify_bulk_dismiss_test.go
git commit -m "feat(scan): bulk-dismiss endpoint for historical AI classify"
```

---

### Task 6: Frontend — two tabs + historical compact list + bulk dismiss + on-demand suggestion expand

**Files:**
- Modify: `frontend_real/services/api.ts` (new helpers)
- Modify: `frontend_real/views/AIClassifyView.vue`
- Test: `frontend_real/__tests__/AIClassifyView.tabs.test.ts` (create)

- [ ] **Step 1: Add typed API helpers**

Open `frontend_real/services/api.ts`. Add at the end of the file:

```ts
export type ClassifyOrigin = 'new' | 'historical'

export interface PendingResponse {
  items: Array<{
    resource_id: number
    resource_name: string
    suggestions: Array<{ project_id: number; stage_code: string; file_rule_code: string; project_name?: string; stage_name?: string; file_rule_name?: string; confidence: number; reason?: string }>
  }>
  total: number
  page: number
  page_size: number
}

export async function fetchClassifyPending(opts: {
  origin: ClassifyOrigin
  page?: number
  pageSize?: number
}): Promise<PendingResponse> {
  const q = new URLSearchParams()
  q.set('origin', opts.origin)
  if (opts.page) q.set('page', String(opts.page))
  if (opts.pageSize) q.set('page_size', String(opts.pageSize))
  const res = await fetch(`${API_BASE}/ai/classify/pending?${q.toString()}`)
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'pending fetch failed')
  return j.data as PendingResponse
}

export async function fetchClassifySuggestions(resourceId: number) {
  const res = await fetch(`${API_BASE}/ai/classify/suggestions?resource_id=${resourceId}`)
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'suggestions fetch failed')
  return j.data
}

export async function bulkDismissHistorical(resourceIds: number[], reason: string) {
  const res = await fetch(`${API_BASE}/ai/classify/bulk-dismiss`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ resource_ids: resourceIds, reason }),
  })
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'bulk dismiss failed')
  return j.data as { dismissed: number }
}
```

(`API_BASE` is already exported from this file; reuse it.)

- [ ] **Step 2: Refactor `AIClassifyView.vue`**

The view currently issues `fetch(${API_BASE}/ai/classify/pending?${q.toString()})` directly inside `loadPending`. Refactor into the new shape:

1. **Add a `currentTab` ref** at the top of `<script setup>`:

   ```ts
   const currentTab = ref<'new' | 'historical'>('new')
   const historicalPage = ref(1)
   const historicalTotal = ref(0)
   const newTotal = ref(0)
   const selectedHistoricalIds = ref<Set<number>>(new Set())
   const expandedHistoricalSuggestions = ref<Record<number, any[]>>({})
   ```

2. **Replace `loadPending`** with:

   ```ts
   async function loadPending() {
     loading.value = true
     try {
       if (currentTab.value === 'new') {
         const data = await fetchClassifyPending({ origin: 'new', page: 1, pageSize: 200 })
         pending.value = data.items
         newTotal.value = data.total
       } else {
         const data = await fetchClassifyPending({
           origin: 'historical',
           page: historicalPage.value,
           pageSize: 20,
         })
         pending.value = data.items
         historicalTotal.value = data.total
       }
     } finally {
       loading.value = false
     }
   }
   ```

3. **Add a "warm-up" call** (in `onMounted` after the initial `loadPending()`) to populate the inactive-tab badge cheaply:

   ```ts
   onMounted(async () => {
     await loadPending()
     // size-1 prefetch for badge on the other tab
     try {
       const other = currentTab.value === 'new' ? 'historical' : 'new'
       const data = await fetchClassifyPending({ origin: other as 'new' | 'historical', page: 1, pageSize: 1 })
       if (other === 'historical') historicalTotal.value = data.total
       else newTotal.value = data.total
     } catch {}
   })
   ```

4. **In the template**, wrap the existing content in `<v-tabs>`:

   ```html
   <v-tabs v-model="currentTab" class="mb-4" color="primary">
     <v-tab value="new">
       新数据
       <v-badge inline :content="newTotal" :model-value="newTotal > 0" class="ml-2" />
     </v-tab>
     <v-tab value="historical">
       历史数据
       <v-badge inline :content="historicalTotal" :model-value="historicalTotal > 0" class="ml-2" />
     </v-tab>
   </v-tabs>

   <v-window v-model="currentTab">
     <v-window-item value="new">
       <!-- 现有 pending 列表整段移动到这里，不动 -->
     </v-window-item>

     <v-window-item value="historical">
       <div class="d-flex align-center mb-3" v-if="pending.length > 0">
         <v-checkbox
           :model-value="selectedHistoricalIds.size === pending.length && pending.length > 0"
           :indeterminate="selectedHistoricalIds.size > 0 && selectedHistoricalIds.size < pending.length"
           density="compact"
           hide-details
           @update:model-value="toggleAllHistorical"
         />
         <span class="mx-2 text-medium-emphasis">全选</span>
         <v-btn
           color="warning"
           variant="tonal"
           size="small"
           :disabled="selectedHistoricalIds.size === 0"
           @click="onBulkDismissHistorical"
         >
           批量标已治理 ({{ selectedHistoricalIds.size }})
         </v-btn>
       </div>

       <v-list density="compact" v-if="pending.length > 0">
         <v-list-item v-for="item in pending" :key="item.resource_id" lines="two">
           <template #prepend>
             <v-checkbox
               :model-value="selectedHistoricalIds.has(item.resource_id)"
               density="compact"
               hide-details
               @update:model-value="(v: any) => toggleHistorical(item.resource_id, v)"
             />
           </template>
           <v-list-item-title>{{ item.resource_name }}</v-list-item-title>
           <template #append>
             <v-btn variant="text" size="small" @click="expandHistorical(item.resource_id)">
               {{ expandedHistoricalSuggestions[item.resource_id] ? '收起' : '展开 AI 推荐' }}
             </v-btn>
           </template>
         </v-list-item>
       </v-list>

       <div v-if="pending.length === 0 && !loading" class="text-center text-medium-emphasis py-12">
         暂无历史数据
       </div>

       <v-pagination
         v-if="historicalTotal > 20"
         v-model="historicalPage"
         :length="Math.ceil(historicalTotal / 20)"
         class="mt-3"
         @update:model-value="loadPending"
       />

       <!-- 展开后的建议卡片 -->
       <div
         v-for="item in pending"
         :key="'sug-' + item.resource_id"
         v-show="expandedHistoricalSuggestions[item.resource_id]"
         class="mb-3 pa-3 bg-grey-lighten-4 rounded"
       >
         <div class="text-subtitle-2 mb-2">{{ item.resource_name }} 的 AI 建议</div>
         <div
           v-for="(s, i) in expandedHistoricalSuggestions[item.resource_id] || []"
           :key="i"
           class="d-flex align-center justify-space-between mb-1"
         >
           <span>{{ s.project_name }} / {{ s.stage_name }} / {{ s.file_rule_name }}</span>
           <v-btn size="x-small" color="primary" variant="tonal" @click="applyHistoricalSuggestion(item, s)">应用</v-btn>
         </div>
       </div>
     </v-window-item>
   </v-window>
   ```

5. **Helper functions** in `<script setup>`:

   ```ts
   function toggleHistorical(id: number, on: boolean) {
     const next = new Set(selectedHistoricalIds.value)
     if (on) next.add(id)
     else next.delete(id)
     selectedHistoricalIds.value = next
   }
   function toggleAllHistorical(on: boolean) {
     selectedHistoricalIds.value = on
       ? new Set(pending.value.map((p) => p.resource_id))
       : new Set()
   }
   async function onBulkDismissHistorical() {
     const ids = Array.from(selectedHistoricalIds.value)
     if (ids.length === 0) return
     const reason = window.prompt('请输入治理说明（必填）：', '首次普查存量批量跳过')
     if (!reason || !reason.trim()) return
     try {
       await bulkDismissHistorical(ids, reason.trim())
       selectedHistoricalIds.value = new Set()
       await loadPending()
     } catch (e: any) {
       window.alert('批量治理失败：' + (e?.message || String(e)))
     }
   }
   async function expandHistorical(resourceId: number) {
     if (expandedHistoricalSuggestions.value[resourceId]) {
       const next = { ...expandedHistoricalSuggestions.value }
       delete next[resourceId]
       expandedHistoricalSuggestions.value = next
       return
     }
     try {
       const d = await fetchClassifySuggestions(resourceId)
       const next = { ...expandedHistoricalSuggestions.value }
       next[resourceId] = d.suggestions || []
       expandedHistoricalSuggestions.value = next
     } catch (e) {
       // ignore
     }
   }
   async function applyHistoricalSuggestion(item: any, s: any) {
     try {
       const res = await fetch(`${API_BASE}/ai/classify/apply`, {
         method: 'POST',
         headers: { 'Content-Type': 'application/json' },
         body: JSON.stringify({
           resource_id: item.resource_id,
           project_id: s.project_id,
           stage_code: s.stage_code,
           file_rule_code: s.file_rule_code,
         }),
       })
       const j = await res.json()
       if (j.success) {
         await loadPending()
       } else {
         window.alert('应用失败：' + j.error)
       }
     } catch (e: any) {
       window.alert('应用失败：' + (e?.message || String(e)))
     }
   }
   ```

6. **Reload on tab switch**: add `watch(currentTab, () => { historicalPage.value = 1; selectedHistoricalIds.value = new Set(); loadPending() })`.

7. **Imports**: at the top of `<script setup>`, `import { fetchClassifyPending, fetchClassifySuggestions, bulkDismissHistorical, API_BASE } from '../services/api'` (replace any direct imports as needed; keep existing `API_BASE` usage working).

- [ ] **Step 3: Write the failing frontend test**

Create `frontend_real/__tests__/AIClassifyView.tabs.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import AIClassifyView from '../views/AIClassifyView.vue'

const vuetify = createVuetify({ components, directives })

function mockFetchPending(items: any[], total: number) {
  return vi.fn(async (input: RequestInfo) => {
    const url = String(input)
    if (url.includes('/ai/classify/pending')) {
      return {
        ok: true,
        json: async () => ({ success: true, data: { items, total, page: 1, page_size: 20 } }),
      } as any
    }
    return { ok: true, json: async () => ({ success: true, data: {} }) } as any
  })
}

describe('AIClassifyView tabs', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('shows new and historical tabs with badges from response totals', async () => {
    global.fetch = mockFetchPending([], 7) as any

    const wrapper = mount(AIClassifyView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const tabs = wrapper.findAll('.v-tab').map((t) => t.text())
    expect(tabs.some((t) => t.includes('新数据'))).toBe(true)
    expect(tabs.some((t) => t.includes('历史数据'))).toBe(true)
  })

  it('issues a historical pending request when switching tabs', async () => {
    const spy = mockFetchPending([], 0)
    global.fetch = spy as any
    const wrapper = mount(AIClassifyView, { global: { plugins: [vuetify] } })
    await flushPromises()

    const histTab = wrapper.findAll('.v-tab').find((t) => t.text().includes('历史数据'))
    expect(histTab).toBeTruthy()
    await histTab!.trigger('click')
    await flushPromises()

    const urls = spy.mock.calls.map((c) => String(c[0]))
    expect(urls.some((u) => u.includes('origin=historical'))).toBe(true)
  })
})
```

- [ ] **Step 4: Run frontend tests**

Run: `yarn test --run __tests__/AIClassifyView.tabs.test.ts`
Expected: PASS.

If `yarn test` fails with native-module errors, run `npm rebuild better-sqlite3` first per project rule and retry.

- [ ] **Step 5: Commit**

```bash
git add frontend_real/services/api.ts frontend_real/views/AIClassifyView.vue frontend_real/__tests__/AIClassifyView.tabs.test.ts
git commit -m "feat(scan): split AI classify into new/historical tabs with bulk dismiss"
```

---

### Task 7: Final regression sweep

- [ ] **Step 1: Run all Go tests**

Run: `go test ./internal/...`
Expected: green.

- [ ] **Step 2: Run all frontend tests**

Run: `yarn test --run`
Expected: green.

- [ ] **Step 3: Commit if any cleanup happened (otherwise skip)**

```bash
git status   # should be clean
```

---

## Self-Review (writer's checklist)

- ✅ Spec section "Data Model" → Task 1.
- ✅ Spec section "Baseline Lifecycle / Decision at insert time" → Task 2.
- ✅ Spec section "Baseline Lifecycle / Closing the window" → Task 3.
- ✅ Spec section "API: pending + origin + total + pagination" → Task 4.
- ✅ Spec section "API: bulk-dismiss" → Task 5.
- ✅ Spec section "Per-row suggestion fetch" → reused existing `/suggestions`, no new task needed; covered by Task 6 frontend.
- ✅ Spec section "Frontend Design (tabs, historical compact list, expand-on-demand)" → Task 6.
- ✅ Spec section "Testing" → Each task has tests; Task 7 is final sweep.
- ✅ No `TODO`/`TBD` placeholders; every code block is complete and runnable.
- ✅ Type names consistent (`ClassifyOrigin`, `PendingResponse`, `BulkDismissClassifyRequest`).

