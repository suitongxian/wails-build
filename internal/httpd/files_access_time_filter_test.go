package httpd

import (
	"fmt"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/repository"
)

// seedDataDistribution 插一行 data_distributing 用于测试，file_create_time 可指定。
func seedDataDistribution(t *testing.T, db *sqlx.DB, path, contentSign string, fileCreateTime *time.Time, scanFoundCount int) {
	t.Helper()
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_distributing (
		scan_task_id, path, data_type, scan_found_count, content_sign,
		file_suffix, file_create_time, file_size,
		ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (NULL, ?, 1, ?, ?, ?, ?, 1024, '127.0.0.1', '00:00:00:00:00:00', ?, ?, ?, 0)`,
		path, scanFoundCount, contentSign, ".txt", fileCreateTime, now, now, now)
	if err != nil {
		t.Fatalf("insert data_distributing: %v", err)
	}
}

// fileNames 提取响应里的 path basename 列表，便于断言顺序无关。
func fileNames(t *testing.T, d map[string]interface{}) []string {
	t.Helper()
	raw, ok := d["files"].([]interface{})
	if !ok {
		t.Fatalf("data.files not a list: %+v", d)
	}
	names := make([]string, 0, len(raw))
	for _, f := range raw {
		m, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		names = append(names, fmt.Sprint(m["path"]))
	}
	return names
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// 设普查时间 = 2026-01-01；三类文件：history (2025)、boundary (2026-01-01 整点)、new (2026-02)
// new 模式：返回 boundary + new（>= 边界，与前端 `>=` 对齐）
func TestHTTP_Files_AccessTimeFilter_New(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetFullInventoryTime("2026-01-01T00:00:00Z")

	hist := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	boundary := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	seedDataDistribution(t, db, "/a/history.txt", "H1", &hist, 1)
	seedDataDistribution(t, db, "/a/boundary.txt", "B1", &boundary, 1)
	seedDataDistribution(t, db, "/a/new.txt", "N1", &newer, 1)
	seedDataDistribution(t, db, "/a/no-create-time.txt", "X1", nil, 1)

	status, resp := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=new&pageSize=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	if got := int(d["total"].(float64)); got != 2 {
		t.Errorf("new total = %d, want 2", got)
	}
	names := fileNames(t, d)
	if !containsPath(names, "/a/boundary.txt") {
		t.Errorf("new should include boundary file (>= inventory time): %v", names)
	}
	if !containsPath(names, "/a/new.txt") {
		t.Errorf("new should include /a/new.txt: %v", names)
	}
	if containsPath(names, "/a/history.txt") {
		t.Errorf("new should NOT include history: %v", names)
	}
	if containsPath(names, "/a/no-create-time.txt") {
		t.Errorf("new should NOT include null-create-time (unknown excluded): %v", names)
	}
}

// history 模式：返回 history + 无 create_time（无 create_time 视为历史，与前端一致）
func TestHTTP_Files_AccessTimeFilter_History(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetFullInventoryTime("2026-01-01T00:00:00Z")

	hist := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	boundary := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	seedDataDistribution(t, db, "/a/history.txt", "H1", &hist, 1)
	seedDataDistribution(t, db, "/a/boundary.txt", "B1", &boundary, 1)
	seedDataDistribution(t, db, "/a/new.txt", "N1", &newer, 1)
	seedDataDistribution(t, db, "/a/no-create-time.txt", "X1", nil, 1)

	status, resp := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=history&pageSize=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	if got := int(d["total"].(float64)); got != 2 {
		t.Errorf("history total = %d, want 2", got)
	}
	names := fileNames(t, d)
	if !containsPath(names, "/a/history.txt") {
		t.Errorf("history should include /a/history.txt: %v", names)
	}
	if !containsPath(names, "/a/no-create-time.txt") {
		t.Errorf("history should include null-create-time (treated as history): %v", names)
	}
	if containsPath(names, "/a/boundary.txt") {
		t.Errorf("history should NOT include boundary (>= inventory, not strict <): %v", names)
	}
	if containsPath(names, "/a/new.txt") {
		t.Errorf("history should NOT include /a/new.txt: %v", names)
	}
}

// 未设 full_inventory_time 时：history 必须返回空（无意义查询），new 退化为不过滤
func TestHTTP_Files_AccessTimeFilter_NoInventoryTime(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	hist := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	seedDataDistribution(t, db, "/a/history.txt", "H1", &hist, 1)
	seedDataDistribution(t, db, "/a/new.txt", "N1", &newer, 1)

	// history → 空
	status, resp := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=history&pageSize=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if got := int(d["total"].(float64)); got != 0 {
		t.Errorf("history total without inventory time = %d, want 0", got)
	}

	// new → 全量（与前端旧行为对齐）
	status2, resp2 := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=new&pageSize=100")
	successOk(t, status2, resp2)
	d2 := dataMap(t, resp2)
	if got := int(d2["total"].(float64)); got != 2 {
		t.Errorf("new total without inventory time = %d, want 2 (passthrough)", got)
	}
}

// 关键回归：accessTimeFilter 过滤须在分页之前，分页与表格行数一致
func TestHTTP_Files_AccessTimeFilter_PaginationConsistent(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetFullInventoryTime("2026-01-01T00:00:00Z")

	// 25 条历史，5 条新；按时间过滤 new=5，应只占 1 页
	hist := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		seedDataDistribution(t, db, fmt.Sprintf("/h/%d.txt", i), fmt.Sprintf("H%d", i), &hist, 1)
	}
	for i := 0; i < 5; i++ {
		seedDataDistribution(t, db, fmt.Sprintf("/n/%d.txt", i), fmt.Sprintf("N%d", i), &newer, 1)
	}

	// 老 bug：total=30、page2 返回 5 个但应该 0；
	// 修复后：total=5、page1 返回 5 个、page2 返回 0
	status, resp := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=new&page=1&pageSize=20")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if got := int(d["total"].(float64)); got != 5 {
		t.Errorf("filtered total = %d, want 5 (must reflect post-filter count, not raw)", got)
	}
	names := fileNames(t, d)
	if len(names) != 5 {
		t.Errorf("page1 returned %d files, want 5", len(names))
	}
	for _, n := range names {
		if containsPath([]string{n}, "/h/0.txt") || containsPath([]string{n}, "/h/10.txt") {
			t.Errorf("page1 leaked history file: %s", n)
		}
	}

	// page2 必须空
	status2, resp2 := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=new&page=2&pageSize=20")
	successOk(t, status2, resp2)
	d2 := dataMap(t, resp2)
	names2 := fileNames(t, d2)
	if len(names2) != 0 {
		t.Errorf("page2 should be empty, got %v", names2)
	}
}

// 关键回归：列表必须按 file_create_time DESC 排序，最新文件出现在第一页
// 旧 bug：GetActive() 没 ORDER BY，SQLite 按 rowid 升序返回，新文件 ID 大
// 被推到列表末尾 → 在「新数据登记管理」tab 里要翻到最后一页才能看到刚扫到的新文件
func TestHTTP_Files_AccessTimeFilter_NewestFirst(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetFullInventoryTime("2026-01-01T00:00:00Z")

	// 按插入顺序：oldest → newest，模拟首次普查 + 后续多次日常盘点累计
	oldest := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	middle := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	seedDataDistribution(t, db, "/a/oldest.txt", "O1", &oldest, 1)
	seedDataDistribution(t, db, "/a/middle.txt", "M1", &middle, 1)
	seedDataDistribution(t, db, "/a/newest.txt", "N1", &newest, 1) // 最大 ID

	status, resp := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=new&page=1&pageSize=2")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	names := fileNames(t, d)
	if len(names) != 2 {
		t.Fatalf("page1 should return 2 files, got %d: %v", len(names), names)
	}
	// page1 必须包含最新文件 newest.txt，且 newest 在 middle 之前
	if names[0] != "/a/newest.txt" {
		t.Errorf("page1[0] should be /a/newest.txt (latest file_create_time), got %s; full=%v", names[0], names)
	}
	if names[1] != "/a/middle.txt" {
		t.Errorf("page1[1] should be /a/middle.txt, got %s; full=%v", names[1], names)
	}

	// page2 应该只有 oldest
	_, resp2 := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=new&page=2&pageSize=2")
	names2 := fileNames(t, dataMap(t, resp2))
	if len(names2) != 1 || names2[0] != "/a/oldest.txt" {
		t.Errorf("page2 should be [/a/oldest.txt], got %v", names2)
	}
}

// 排序稳定性：file_create_time 为 nil 的文件应排到末尾（NULLS LAST）
func TestHTTP_Files_NullCreateTimeLast(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// 不设 full_inventory_time，accessTimeFilter 不参与，只测排序
	t1 := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	seedDataDistribution(t, db, "/a/null.txt", "X1", nil, 1)
	seedDataDistribution(t, db, "/a/has-time-old.txt", "X2", &t2, 1)
	seedDataDistribution(t, db, "/a/has-time-new.txt", "X3", &t1, 1)

	status, resp := jsonReqNoBody(t, r, "GET", "/files?pageSize=100")
	successOk(t, status, resp)
	names := fileNames(t, dataMap(t, resp))

	if len(names) != 3 {
		t.Fatalf("want 3 files, got %d: %v", len(names), names)
	}
	// 期望顺序：new（2026-05）→ old（2026-01）→ null
	if names[0] != "/a/has-time-new.txt" || names[1] != "/a/has-time-old.txt" || names[2] != "/a/null.txt" {
		t.Errorf("expected newest→oldest→null, got %v", names)
	}
}

// all（默认）+ 不传 accessTimeFilter：行为与现状一致，不丢任何文件
func TestHTTP_Files_AccessTimeFilter_AllPassthrough(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetFullInventoryTime("2026-01-01T00:00:00Z")

	hist := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	seedDataDistribution(t, db, "/a/history.txt", "H1", &hist, 1)
	seedDataDistribution(t, db, "/a/new.txt", "N1", &newer, 1)

	// 不传
	status, resp := jsonReqNoBody(t, r, "GET", "/files?pageSize=100")
	successOk(t, status, resp)
	if got := int(dataMap(t, resp)["total"].(float64)); got != 2 {
		t.Errorf("no filter total = %d, want 2", got)
	}

	// 显式 all
	status2, resp2 := jsonReqNoBody(t, r, "GET", "/files?accessTimeFilter=all&pageSize=100")
	successOk(t, status2, resp2)
	if got := int(dataMap(t, resp2)["total"].(float64)); got != 2 {
		t.Errorf("all total = %d, want 2", got)
	}
}
