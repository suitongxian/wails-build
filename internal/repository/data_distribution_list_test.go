package repository

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// seedDist inserts one row into data_distributing and returns its PK.
func seedDist(t *testing.T, db *sqlx.DB, path, contentSign string, ctime *time.Time, scanFoundCount int) int64 {
	t.Helper()
	now := time.Now()
	res, err := db.Exec(`
		INSERT INTO data_distributing
			(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address,
			 scan_time, create_time, update_time, disable, file_create_time)
		VALUES (?, 1, ?, ?, 0, '', '', ?, ?, ?, 0, ?)`,
		path, scanFoundCount, contentSign, now, now, now, ctime,
	)
	if err != nil {
		t.Fatalf("seedDist: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedResource inserts one row into data_resources with the given content_sign and source_count.
func seedResource(t *testing.T, db *sqlx.DB, contentSign string, sourceCount int) {
	t.Helper()
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO data_resources
			(content_sign, source_count, workspace_source_count, first_create_time,
			 resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin)
		VALUES (?, ?, 1, ?, 'f', 0, 0, ?, ?, 0, 'scan')`,
		contentSign, sourceCount, now, now, now,
	)
	if err != nil {
		t.Fatalf("seedResource: %v", err)
	}
}

func newRepo(db *sqlx.DB) *DataDistributingRepository {
	return &DataDistributingRepository{DB: db}
}

// ---------- tests ----------

func TestListFilesWithFilters_BasicPagination(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/a/1.pdf", "s1", nil, 1)
	seedDist(t, db, "/a/2.pdf", "s2", nil, 1)
	seedDist(t, db, "/a/3.pdf", "s3", nil, 1)

	rows, total, err := repo.ListFilesWithFilters(FilesListOptions{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(rows) != 2 {
		t.Errorf("len(rows) = %d, want 2", len(rows))
	}

	rows2, total2, err := repo.ListFilesWithFilters(FilesListOptions{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("unexpected err page2: %v", err)
	}
	if total2 != 3 {
		t.Errorf("total2 = %d, want 3", total2)
	}
	if len(rows2) != 1 {
		t.Errorf("len(rows2) = %d, want 1", len(rows2))
	}
}

func TestListFilesWithFilters_SortByCreateTimeDescNullsLast(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	oldest := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	seedDist(t, db, "/old.pdf", "so", &oldest, 1)
	seedDist(t, db, "/new.pdf", "sn", &newest, 1)
	seedDist(t, db, "/null.pdf", "sx", nil, 1) // null ctime — should come last

	rows, _, err := repo.ListFilesWithFilters(FilesListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	// order: newest → oldest → null
	if rows[0].Path != "/new.pdf" {
		t.Errorf("rows[0].Path = %q, want /new.pdf", rows[0].Path)
	}
	if rows[1].Path != "/old.pdf" {
		t.Errorf("rows[1].Path = %q, want /old.pdf", rows[1].Path)
	}
	if rows[2].Path != "/null.pdf" {
		t.Errorf("rows[2].Path = %q, want /null.pdf", rows[2].Path)
	}
}

func TestListFilesWithFilters_WorkspaceFilterInside(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/ws/a.pdf", "s1", nil, 1)
	seedDist(t, db, "/other/b.pdf", "s2", nil, 1)
	seedDist(t, db, "/ws_other/c.pdf", "s3", nil, 1) // should NOT match /ws prefix

	rows, total, err := repo.ListFilesWithFilters(FilesListOptions{
		WorkspacePath:   "/ws",
		WorkspaceFilter: "inside",
		Page:            1,
		PageSize:        10,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(rows) != 1 || rows[0].Path != "/ws/a.pdf" {
		t.Errorf("got %v, want [/ws/a.pdf]", rows)
	}
}

func TestListFilesWithFilters_WorkspaceFilterOutside(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/ws/a.pdf", "s1", nil, 1)
	seedDist(t, db, "/other/b.pdf", "s2", nil, 1)
	seedDist(t, db, "/ws_other/c.pdf", "s3", nil, 1)

	rows, total, err := repo.ListFilesWithFilters(FilesListOptions{
		WorkspacePath:   "/ws",
		WorkspaceFilter: "outside",
		Page:            1,
		PageSize:        10,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	paths := map[string]bool{}
	for _, r := range rows {
		paths[r.Path] = true
	}
	if paths["/ws/a.pdf"] {
		t.Error("/ws/a.pdf should NOT be in outside results")
	}
	if !paths["/other/b.pdf"] || !paths["/ws_other/c.pdf"] {
		t.Errorf("missing expected outside paths, got %v", paths)
	}
}

func TestListFilesWithFilters_SurvivalFilters(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/deleted.pdf", "sd", nil, 0) // scan_found_count=0 → deleted
	seedDist(t, db, "/new.pdf", "sn", nil, 1)     // scan_found_count=1 → new
	seedDist(t, db, "/normal.pdf", "so", nil, 2)  // scan_found_count=2 → normal

	check := func(filter string, wantTotal int64) {
		t.Helper()
		_, total, err := repo.ListFilesWithFilters(FilesListOptions{
			SurvivalFilter: filter,
			Page:           1,
			PageSize:       10,
		})
		if err != nil {
			t.Fatalf("SurvivalFilter=%q err: %v", filter, err)
		}
		if total != wantTotal {
			t.Errorf("SurvivalFilter=%q total=%d, want %d", filter, total, wantTotal)
		}
	}

	check("new", 1)
	check("deleted", 1)
	check("normal", 2) // scan_found_count > 0 → both new(1) and normal(2)
	check("all", 3)
	check("", 3)
}

func TestListFilesWithFilters_AccessTimeFilterNew(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	inventoryTime := "2026-01-01T00:00:00Z"
	historyTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	seedDist(t, db, "/history.pdf", "sh", &historyTime, 1)
	seedDist(t, db, "/new.pdf", "sn", &newTime, 1)
	seedDist(t, db, "/null.pdf", "sx", nil, 1)

	rows, total, err := repo.ListFilesWithFilters(FilesListOptions{
		AccessTimeFilter:  "new",
		FullInventoryTime: inventoryTime,
		Page:              1,
		PageSize:          10,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(rows) != 1 || rows[0].Path != "/new.pdf" {
		t.Errorf("got %v, want [/new.pdf]", rows)
	}
}

func TestListFilesWithFilters_AccessTimeFilterHistoryEmptyWhenNoInventoryTime(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/a.pdf", "sa", nil, 1)

	_, total, err := repo.ListFilesWithFilters(FilesListOptions{
		AccessTimeFilter:  "history",
		FullInventoryTime: "",
		Page:              1,
		PageSize:          10,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0 (no inventory time → history returns empty)", total)
	}
}

func TestListFilesWithFilters_AccessTimeFilterNewPassthroughWhenNoInventoryTime(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/a.pdf", "sa", nil, 1)
	seedDist(t, db, "/b.pdf", "sb", nil, 1)

	_, total, err := repo.ListFilesWithFilters(FilesListOptions{
		AccessTimeFilter:  "new",
		FullInventoryTime: "",
		Page:              1,
		PageSize:          10,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2 (no inventory time → new is passthrough)", total)
	}
}

func TestListFilesWithFilters_SearchCaseInsensitive(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/a/REPORT.pdf", "sr", nil, 1)
	seedDist(t, db, "/a/normal.pdf", "sn", nil, 1)

	rows, total, err := repo.ListFilesWithFilters(FilesListOptions{
		Search:   "report",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(rows) != 1 || rows[0].Path != "/a/REPORT.pdf" {
		t.Errorf("got %v, want [/a/REPORT.pdf]", rows)
	}
}

func TestListFilesWithFilters_CopyCountFromDataResources(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/x.pdf", "signX", nil, 1)
	seedResource(t, db, "signX", 5)

	rows, _, err := repo.ListFilesWithFilters(FilesListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].CopyCount != 5 {
		t.Errorf("CopyCount = %d, want 5", rows[0].CopyCount)
	}
}

func TestListFilesWithFilters_CopyCountZeroWhenNoMatchingResource(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	seedDist(t, db, "/y.pdf", "signY", nil, 1)
	// no data_resources row for signY

	rows, _, err := repo.ListFilesWithFilters(FilesListOptions{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].CopyCount != 0 {
		t.Errorf("CopyCount = %d, want 0", rows[0].CopyCount)
	}
}

func TestListFilesWithFilters_WorkspaceFilterEscapesUnderscoreInPath(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := newRepo(db)

	// workspace = /a/user_name/work；同时插 /a/userXname/work/foo.pdf（_ 误当通配符就会命中）
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_distributing (
		path, data_type, scan_found_count, content_sign, file_size,
		ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (?, 1, 1, ?, 0, '127.0.0.1', '00:00:00:00:00:00', ?, ?, ?, 0)`,
		"/a/userXname/work/foo.pdf", "H1", now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO data_distributing (
		path, data_type, scan_found_count, content_sign, file_size,
		ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (?, 1, 1, ?, 0, '127.0.0.1', '00:00:00:00:00:00', ?, ?, ?, 0)`,
		"/a/user_name/work/legit.pdf", "H2", now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	rows, total, err := repo.ListFilesWithFilters(FilesListOptions{
		WorkspacePath:   "/a/user_name/work",
		WorkspaceFilter: "inside",
		Page:            1,
		PageSize:        100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("expected only legit.pdf (1 file), total=%d", total)
	}
	if len(rows) != 1 || rows[0].Path != "/a/user_name/work/legit.pdf" {
		t.Errorf("got rows: %v", rows)
	}
}
