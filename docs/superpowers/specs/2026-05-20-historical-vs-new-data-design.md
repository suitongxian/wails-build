# Historical vs New Data In The AI Classify Tool

## Background

The scan terminal performs successive file scans and persists every distinct file content as a row in `data_resources`, keyed by `content_sign`. Today the AI 归目 tool (`AIClassifyView`) shows every claimed-but-unclassified resource in one undifferentiated list. After a terminal has been in use for a while, that list is dominated by the **first census**: the bulk of files that already existed on the machine when scanning began.

Conceptually those two populations have very different governance value:

- **Historical data**: everything discovered during the initial census of an existing machine. It exists for visibility and coarse remediation. Forcing the user to walk every row through full AI classify is wasted effort and hides the signal.
- **New data**: anything ingested after the initial census has finished. This is the population the user really needs to govern carefully — it represents net-new assets being produced or received on the terminal.

Mixing the two in a single recommendation queue dilutes the new-data signal and creates work that the user does not want to do. The product direction is to keep both populations visible but treat them as first-class, distinct workstreams.

## Goals

- Persist a stable, immutable origin tag (`historical` or `new`) on every `data_resources` row.
- Define a deterministic boundary between the two populations: the **first successful scan task** closes the historical window for the entire terminal.
- Surface the two populations as separate tabs in the AI 归目 tool, with the new-data tab defaulting and keeping the existing recommendation UX intact.
- Give the historical tab a lighter-weight workflow geared at "I've seen it; move on": multi-select bulk dismiss, plus an on-demand expand to fall back to per-row AI recommendations when a particular historical file actually deserves it.
- Keep changes confined to the smallest set of code paths: a single insert chokepoint in the repository layer, one scan-task lifecycle hook, one HTTP query parameter, and one new endpoint.

## Non-Goals

- No retroactive re-classification of existing terminals: existing rows are bulk-stamped `historical` on upgrade and we do not try to backfill what "would have been new" from log history.
- No undo for bulk dismiss in v1. The action is recorded in `audit_logs`, but the user-facing path forward is to use the existing single-row apply flow if they change their mind.
- No per-workspace baselines. The first-census window is a single terminal-wide event.
- No background pre-fetching of AI suggestions in the historical tab. Suggestions are computed only when the user explicitly expands a row.
- No new statistics surfaces beyond the two badge counters on the tabs themselves. A richer "historical governance dashboard" is out of scope.

## Data Model

### `data_resources.data_origin`

Add a new column on `data_resources`:

```sql
ALTER TABLE data_resources
  ADD COLUMN data_origin TEXT NOT NULL DEFAULT 'new'
  CHECK (data_origin IN ('historical', 'new'));
```

Semantics:

- The value is **decided once at INSERT time** and never updated afterwards. `IncrementSourceCount`, `IncrementWorkspaceSourceCount`, `BatchUpdateForModifiedFiles`, and every other mutating path leave `data_origin` alone.
- Files whose content changes after baseline (same path, different `content_sign`) trigger a new INSERT for the new `content_sign`. That new row is independently tagged using the current baseline state, so a post-baseline content change naturally lands as `new`.
- `data_origin` participates in no foreign key and no UNIQUE constraint. It is purely a partition tag for query filtering.

An index helps the AI queue queries:

```sql
CREATE INDEX IF NOT EXISTS idx_data_resources_origin_claim
  ON data_resources(data_origin, claim_status, importance_level);
```

### `system_config.baseline_completed_at`

Store the baseline boundary as a single KV row in the existing `system_config` table:

- Key: `baseline_completed_at`
- Value: ISO8601 timestamp string, or NULL until the first successful scan task completes.

No new table is introduced.

### Migration

For terminals upgrading to this version, the migration runs once at startup:

1. If the `data_origin` column does not exist, add it (the `ALTER TABLE` above).
2. Set every existing `data_resources` row to `data_origin = 'historical'`. The justification is that pre-feature rows were collected without a baseline concept, so the safest interpretation is to treat them all as historical. This avoids accidentally classifying long-standing files as new just because they pre-date the feature.
3. Leave `baseline_completed_at` NULL. The first scan task the user runs after the upgrade will close the window naturally (and any newly inserted rows during that scan will also be tagged `historical`, matching the user's mental model that the upgrade itself does not start a new-data era — only completing a fresh census does).

Fresh installs run the same migration on an empty table, so no rows get touched and the column simply exists with its default.

## Baseline Lifecycle

The boundary is owned by exactly two pieces of code.

### Decision at insert time

Both `data_resources.InsertBatch` and `data_resources.InsertFromStatistics` consult `system_config.baseline_completed_at` once per call and stamp the resulting `data_origin` on every row in the batch:

- `baseline_completed_at IS NULL` → `'historical'`
- otherwise → `'new'`

A small helper `currentDataOrigin(db)` lives next to the repository and is the single place that maps baseline state to tag. Both insert paths call it. No other repository function touches `data_origin`.

### Closing the window

On any `scan_task` transitioning to `task_state = 'succeed'` (today this happens via `scan_task` repository writes), perform a conditional update:

```sql
UPDATE system_config
   SET value = ?,
       update_time = CURRENT_TIMESTAMP
 WHERE key = 'baseline_completed_at'
   AND (value IS NULL OR value = '');
```

This pattern is idempotent and concurrency-safe: only the first scan to succeed will set the timestamp; every subsequent succeed is a no-op. The conditional is performed at the SQL layer rather than in Go to avoid TOCTOU races.

The `INSERT INTO system_config (key, value) VALUES ('baseline_completed_at', NULL)` seed row is created as part of the migration so that the conditional UPDATE always has something to update.

### Implications

- A first scan that crashes partway through (state ends as `fail`) does **not** close the window. The user can rescan; the rows inserted during the crashed run remain `historical` because the baseline is still NULL.
- The very first scan that finishes successfully also has its rows stamped `historical` (they were inserted while baseline was still NULL). Only scans that start *after* baseline has been set produce `new` rows. This matches the natural reading of "everything found during the first census is historical".

## API Changes

### `GET /ai/classify/pending`

Add a query parameter `origin` accepting `new` (default) or `historical`, plus standard pagination parameters `page` (default 1) and `page_size` (default 20, max 200). The existing filter `claim_status = 2 AND importance_level = 0 AND ai_classify_rejected_at IS NULL AND disable = 0` continues to apply; `data_origin = ?` is appended.

Old callers that omit `origin` continue to work and now see only the new-data subset, which matches the desired UX (the AI tool was already intended to focus on what needs attention).

Response shape is extended to carry the unpaginated total so the frontend can render badge counts without a second round-trip:

```json
{
  "success": true,
  "data": {
    "items": [{"resource_id": ..., "resource_name": ..., "suggestions": [...]}, ...],
    "total": 847,
    "page": 1,
    "page_size": 20
  }
}
```

For `origin=historical`, every item's `suggestions` field is an **empty array**. This avoids paying LLM/extract cost for the bulk historical list. Per-row suggestions are computed on demand by a separate call (see below). For `origin=new`, suggestions are computed eagerly as today.

### Per-row suggestion fetch

The existing endpoint `GET /ai/classify/suggestions?resource_id=...` already returns sorted suggestions for a single resource (handler `GetClassifySuggestions` in `internal/httpd/ai_classify.go`). The historical tab reuses it as-is for its on-demand expand button — no new endpoint is added.

### `POST /ai/classify/bulk-dismiss`

A new endpoint:

- Request body: `{resource_ids: [int64], reason: string}`
- Behavior:
  1. Validate the request: `resource_ids` non-empty (cap at 500 per call) and `reason` non-blank.
  2. Look up every id; if **any** id is missing, soft-disabled, or has `data_origin != 'historical'`, the whole request is rejected with `400` and the response lists the offending ids. There is no silent skip — bulk dismiss is an explicit per-id intent and partial application would be confusing.
  3. In a single transaction, set `ai_classify_rejected_at = NOW()` and `ai_classify_rejection_reason = ?` on the validated rows.
  4. Append one `audit_logs` row per resource, action `ai_classify_dismiss`, with the reason payload.
- Response: `{success: true, data: {dismissed: N}}` on success, or `{success: false, error: "...", data: {invalid_ids: [...]}}` on validation failure.

Bulk dismiss is the historical tab's main verb. The existing `POST /ai/classify/apply` continues to work unchanged for the "I do want to AI-classify this one historical file" path.

## Frontend Design

The change is contained in `frontend_real/views/AIClassifyView.vue` plus a small extension to `services/api.ts`.

### Layout

A Vuetify `v-tabs` row sits directly under the page title, with two tabs:

- `新数据 N` (default active)
- `历史数据 M`

Counters come from the `total` field of each tab's most recent `pending` response. The frontend issues a tiny size-1 fetch (`page_size=1`) for the inactive tab on first mount to populate its badge without paying the cost of loading the full list, then refreshes the count whenever its own tab refreshes.

### `新数据` tab

The current `AIClassifyView` body is preserved verbatim: search box, refresh, auto-apply, the grouped-pending and ungrouped-pending lists with per-row apply / reject. No behavioral change.

### `历史数据` tab

A compact list view replaces the card-per-row layout:

- Toolbar: `[全选]` checkbox, `[批量标已治理 (N)]` action button, `[搜索资源...]` text input, `[刷新]`.
- Each row: row-level checkbox, resource name, `first_create_time` (formatted), and an `[展开 ▼]` button.
- Pagination at the bottom (page size 20 by default; the historical list can be hundreds or thousands of rows).
- Clicking `[展开 ▼]` calls `GET /ai/classify/suggest?resource_id=...`, then renders the same suggestion-card UI as the new tab inline for that row (so the per-row apply / reject path is identical to the new tab).
- Clicking `[批量标已治理]` sends a single `POST /ai/classify/bulk-dismiss` with the checked ids and a small reason prompt.

### State boundary

The two tabs keep their own state slices (`pendingNew`, `pendingHistorical`, `selectedHistoricalIds`, `expandedSuggestionsByResourceId`). Switching tabs preserves each side's state so a user can flip back and forth without losing their selection or expanded suggestions.

## Testing

Per project rule, every stage lands with passing tests before the next stage starts.

### Go (`internal/repository/`, `internal/httpd/`)

- **Migration**: a fresh in-memory DB seeded with pre-feature rows runs the migration and observes every row tagged `historical` with `baseline_completed_at` still NULL.
- **Insert tagging**: with `baseline_completed_at` NULL, `InsertBatch` and `InsertFromStatistics` both write `historical`. After setting the config row, the same inserts write `new`. Re-`IncrementSourceCount` on an existing content_sign does not change `data_origin`. A modified-file path (old content_sign → new content_sign) creates a fresh row tagged with the *current* baseline state.
- **Baseline close**: simulate a `scan_task` transition to `succeed`; verify the conditional UPDATE writes the timestamp once and is a no-op on the second succeed. A `fail` transition never writes the timestamp.
- **Handler `ListPendingForClassify`**: returns only `new` rows by default; `?origin=historical` returns only historical rows and with `suggestions == []`. Old callers (no param) match the new default. Response carries `items` / `total` / `page` / `page_size`.
- Existing `GET /ai/classify/suggestions` is verified to keep working unchanged (it already handles a single resource id regardless of origin).
- **Handler `BulkDismiss`** (`/ai/classify/bulk-dismiss`): dismisses N historical resources in one transaction, rejects the entire batch with `400` if any id is missing / disabled / non-historical (and lists the offending ids), writes one audit log per dismissed resource, sets `ai_classify_rejected_at`.

### Frontend (`frontend_real/__tests__/`)

- Snapshot / DOM test on `AIClassifyView` that asserts: two tabs render with badge counts, the `新数据` tab uses the existing card list, the `历史数据` tab shows the compact list with checkboxes.
- Behavior test for tab switching: hitting the historical tab fires a `GET /ai/classify/pending?origin=historical`, hitting the new tab fires `?origin=new`.
- Behavior test for `[展开 ▼]`: fires `/ai/classify/suggest` for that resource and renders the returned suggestion cards inline.
- Behavior test for `[批量标已治理]`: collects checked ids, fires `/ai/classify/bulk-dismiss` with them, removes those rows from the historical list on success.

### Test prerequisites

- Frontend tests follow project convention: `yarn test` (not npm).
- For tests that touch better-sqlite3 indirectly via shared toolchain, run `npm rebuild better-sqlite3` first (carried over from project CLAUDE.md).
- Go tests run with `go test ./internal/...` at repo root.

## Rollout

The feature is purely additive (one new column, one new config row, one new endpoint, two new query behaviors). It can ship in a single commit chain:

1. Migration + repository changes (column, helper, insert paths, baseline close).
2. New endpoints (`pending` query param, `suggest`, `bulk-dismiss`).
3. Frontend tabs + historical compact list.

Existing terminals upgrade by replaying the migration on next startup. No data loss, no destructive change.

## Open Questions

Defer to follow-ups if they come up in practice; none of these block v1:

- Should the historical tab eventually surface a "scanner sweep" mode that batch-classifies into a default low-importance bucket without dismissing? Out of scope for v1.
- Should bulk dismiss support an explicit category (e.g., "old contracts", "personal files") that lands as an importance level instead of just a rejection timestamp? Out of scope for v1; everything in historical is dismissed as un-actionable until the user explicitly expands a row.
- Should the baseline window be re-openable (admin action: "I just re-imaged this machine, start over")? Probably yes, but the trigger is a separate UI we have not specified; defer.
