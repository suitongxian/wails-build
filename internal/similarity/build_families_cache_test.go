package similarity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBuildFamilies_UsesCacheWhenAvailable verifies that when the DB has valid
// cached features for a file, extractTextHook is never called.
func TestBuildFamilies_UsesCacheWhenAvailable(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "doc.txt")
	if err := os.WriteFile(file, []byte("hello world test content for buildfamilies cache"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	db := openE2EDB(t)
	SetDB(db)
	defer SetDB(nil)

	// Seed DB with valid cached features (matching the file's mtime+size).
	now := time.Now()
	_, err = db.Exec(`INSERT INTO data_distributing
		(path, content_sign, file_create_time, file_size,
		 simhash, content_hash, feature_mtime, feature_size,
		 data_type, scan_found_count, ip, mac_address, scan_time,
		 create_time, update_time, disable)
		VALUES (?, 'CS_CACHED', ?, ?, 555, 'cached_hash', ?, ?,
		        1, 1, '', '', ?,
		        ?, ?, 0)`,
		file, now, info.Size(), info.ModTime(), info.Size(), now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	// Install spy hook.
	callCount := 0
	origHook := extractTextHook
	extractTextHook = func(path string, timeout time.Duration) string {
		callCount++
		return ""
	}
	defer func() { extractTextHook = origHook }()

	inputs := []FileInput{{
		UniqueID:    "uid-cached",
		Path:        file,
		ContentSign: "CS_CACHED",
		Size:        info.Size(),
		ModTime:     info.ModTime(),
	}}

	_, err = BuildFamilies(context.Background(), inputs, defaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	if callCount != 0 {
		t.Errorf("extractTextHook called %d times, want 0 (should hit cache)", callCount)
	}
}

// TestBuildFamilies_CacheMissTriggersFallbackAndWriteback verifies that when
// the DB row has no cached features, live extraction runs and writes back.
func TestBuildFamilies_CacheMissTriggersFallbackAndWriteback(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "fresh.txt")
	if err := os.WriteFile(file, []byte("new content for cache miss writeback test"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}

	db := openE2EDB(t)
	SetDB(db)
	defer SetDB(nil)

	// Seed DB with row that has NO feature columns (cache miss).
	now := time.Now()
	_, err = db.Exec(`INSERT INTO data_distributing
		(path, content_sign, file_create_time, file_size,
		 data_type, scan_found_count, ip, mac_address, scan_time,
		 create_time, update_time, disable)
		VALUES (?, 'CS_FRESH', ?, ?,
		        1, 1, '', '', ?,
		        ?, ?, 0)`,
		file, now, info.Size(), now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	inputs := []FileInput{{
		UniqueID:    "uid-fresh",
		Path:        file,
		ContentSign: "CS_FRESH",
		Size:        info.Size(),
		ModTime:     info.ModTime(),
	}}

	_, err = BuildFamilies(context.Background(), inputs, defaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	// Verify writeback: content_hash should now be non-empty.
	var contentHash *string
	if dbErr := db.Get(&contentHash, `SELECT content_hash FROM data_distributing WHERE content_sign='CS_FRESH'`); dbErr != nil {
		t.Fatalf("query after writeback: %v", dbErr)
	}
	if contentHash == nil || *contentHash == "" {
		t.Errorf("expected content_hash written back after cache miss, got NULL/empty")
	}
}

// TestBuildFamilies_NilDB_NoRegression verifies that when no DB is injected
// (old-style tests), BuildFamilies still works as before.
func TestBuildFamilies_NilDB_NoRegression(t *testing.T) {
	SetDB(nil) // explicitly nil

	tmp := t.TempDir()
	file := filepath.Join(tmp, "nodeb.txt")
	if err := os.WriteFile(file, []byte("some content without db injection"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)

	inputs := []FileInput{{
		UniqueID:    "uid-nodeb",
		Path:        file,
		ContentSign: "CS_NODEB",
		Size:        info.Size(),
		ModTime:     info.ModTime(),
	}}

	_, err := BuildFamilies(context.Background(), inputs, defaultConfig())
	if err != nil {
		t.Errorf("BuildFamilies with nil DB should not error: %v", err)
	}
}
