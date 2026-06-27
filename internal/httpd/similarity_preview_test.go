package httpd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_AnalyzePreview_NoTasksYet(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	for i, p := range []string{"/a.pdf", "/b.pdf", "/c.pdf"} {
		_, err := db.Exec(`INSERT INTO data_distributing
			(path, data_type, scan_found_count, content_sign, file_create_time, file_size,
			 ip, mac_address, scan_time, create_time, update_time, disable)
			VALUES (?, 0, 1, ?, ?, 100, '', '', ?, ?, ?, 0)`,
			p, []string{"H1", "H2", "H3"}[i], now, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	status, resp := jsonReqNoBody(t, r, "POST", "/similarity/analyze/preview")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	if n, _ := d["cache_miss_count"].(float64); int(n) != 3 {
		t.Errorf("cache_miss_count = %v, want 3 (no features yet, all miss)", d["cache_miss_count"])
	}
	if d["last_run_at"] != nil {
		t.Errorf("last_run_at should be nil when no tasks have run, got %v", d["last_run_at"])
	}
}

func TestHTTP_AnalyzePreview_AllCachedReturnsZeroMiss(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// Create real files in a temp dir
	tmp := t.TempDir()
	now := time.Now()
	for i, name := range []string{"a.pdf", "b.pdf"} {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte("content "+name), 0644); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(path)
		_, err := db.Exec(`INSERT INTO data_distributing
			(path, data_type, scan_found_count, content_sign, file_create_time, file_size,
			 ip, mac_address, scan_time,
			 simhash, content_hash, feature_mtime, feature_size,
			 create_time, update_time, disable)
			VALUES (?, 0, 1, ?, ?, ?, '', '', ?, 1, 'x', ?, ?, ?, ?, 0)`,
			path, fmt.Sprintf("CS_%d", i), now, info.Size(), now,
			info.ModTime(), info.Size(), now, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	status, resp := jsonReqNoBody(t, r, "POST", "/similarity/analyze/preview")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	if n, _ := d["cache_miss_count"].(float64); int(n) != 0 {
		t.Errorf("cache_miss_count = %v, want 0 (all features fresh)", d["cache_miss_count"])
	}
}

func TestHTTP_AnalyzePreview_StaleCacheCountsAsMiss(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "stale.pdf")
	if err := os.WriteFile(path, []byte("current content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Seed with stale mtime (1 hour ago) — current file mtime differs
	now := time.Now()
	staleMtime := now.Add(-1 * time.Hour)
	_, err := db.Exec(`INSERT INTO data_distributing
		(path, data_type, scan_found_count, content_sign, file_create_time, file_size,
		 ip, mac_address, scan_time,
		 simhash, content_hash, feature_mtime, feature_size,
		 create_time, update_time, disable)
		VALUES (?, 0, 1, 'CS_STALE', ?, 100, '', '', ?, 1, 'x', ?, 100, ?, ?, 0)`,
		path, now, now, staleMtime, now, now)
	if err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReqNoBody(t, r, "POST", "/similarity/analyze/preview")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	if n, _ := d["cache_miss_count"].(float64); int(n) != 1 {
		t.Errorf("cache_miss_count = %v, want 1 (stale mtime)", d["cache_miss_count"])
	}
}

// family_dirty=1（扫描后但还没跑分析）→ family_stale 应为 true
func TestHTTP_AnalyzePreview_ReturnsFamilyStale_WhenDirty(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyFamilyDirty, "1")

	status, resp := jsonReqNoBody(t, r, "POST", "/similarity/analyze/preview")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	stale, ok := d["family_stale"].(bool)
	if !ok {
		t.Fatalf("family_stale missing or not bool: %#v", d["family_stale"])
	}
	if !stale {
		t.Errorf("family_stale = false, want true (family_dirty=1)")
	}
}

// family_dirty=0（分析跑完且没有新扫描）→ family_stale 应为 false
func TestHTTP_AnalyzePreview_ReturnsFamilyStale_WhenClean(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyFamilyDirty, "0")

	status, resp := jsonReqNoBody(t, r, "POST", "/similarity/analyze/preview")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	stale, ok := d["family_stale"].(bool)
	if !ok {
		t.Fatalf("family_stale missing or not bool: %#v", d["family_stale"])
	}
	if stale {
		t.Errorf("family_stale = true, want false (family_dirty=0)")
	}
}

func TestHTTP_AnalyzePreview_WithLastRun(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	_, err := db.Exec(`INSERT INTO similarity_task
		(task_state, start_time, end_time, input_count, family_count, member_count, create_time, update_time)
		VALUES ('succeed', ?, ?, 50, 12, 80, ?, ?)`,
		now.Add(-1*time.Hour), now.Add(-50*time.Minute), now, now)
	if err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReqNoBody(t, r, "POST", "/similarity/analyze/preview")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	if d["last_run_at"] == nil {
		t.Errorf("last_run_at should be set; got %v", d)
	}
	if n, _ := d["last_run_duration_sec"].(float64); n != 600 {
		t.Errorf("last_run_duration_sec = %v, want 600 (10 minutes)", n)
	}
}
