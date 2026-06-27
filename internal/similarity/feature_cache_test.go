package similarity

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// setupSimilarityTestDB reuses the openE2EDB helper defined in e2e_test.go,
// which is in the same package (similarity). Both files are package similarity,
// so the helper is available at test compile time.
func setupSimilarityTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	return openE2EDB(t)
}

func TestReadCachedFeatures_ReturnsNilWhenAllNULL(t *testing.T) {
	db := setupSimilarityTestDB(t)
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_distributing
        (path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time, file_create_time, create_time, update_time, disable)
        VALUES ('/a', 1, 1, 'CS_NULL', 1, '127.0.0.1', 'aa:bb', ?, ?, ?, ?, 0)`, now, now, now, now)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	cached, err := ReadCachedFeatures(db, "CS_NULL")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cached != nil {
		t.Errorf("expected nil for all-NULL row, got %+v", cached)
	}
}

func TestReadCachedFeatures_ReturnsNilWhenRowNotFound(t *testing.T) {
	db := setupSimilarityTestDB(t)
	cached, err := ReadCachedFeatures(db, "NONEXISTENT")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cached != nil {
		t.Errorf("expected nil for missing row, got %+v", cached)
	}
}

func TestReadCachedFeatures_ReturnsValueWhenPopulated(t *testing.T) {
	db := setupSimilarityTestDB(t)
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_distributing
        (path, data_type, scan_found_count, content_sign, file_size,
         simhash, content_hash, feature_mtime, feature_size,
         ip, mac_address, scan_time, create_time, update_time, disable)
        VALUES ('/b', 1, 1, 'CS_HIT', 1, 12345, 'abc', ?, 100, '127.0.0.1', 'aa:bb', ?, ?, ?, 0)`,
		now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	cached, err := ReadCachedFeatures(db, "CS_HIT")
	if err != nil {
		t.Fatal(err)
	}
	if cached == nil {
		t.Fatal("expected non-nil")
	}
	if cached.Simhash != 12345 {
		t.Errorf("simhash = %d", cached.Simhash)
	}
	if cached.ContentHash != "abc" {
		t.Errorf("content_hash = %s", cached.ContentHash)
	}
	if cached.FeatureSize != 100 {
		t.Errorf("size = %d", cached.FeatureSize)
	}
}

func TestIsCacheValid_MtimeAndSizeMatch(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "t.txt")
	os.WriteFile(file, []byte("hi"), 0644)
	info, _ := os.Stat(file)

	cached := &CachedFeaturesDB{
		FeatureMtime: info.ModTime(),
		FeatureSize:  info.Size(),
	}
	if !IsCacheValid(file, cached) {
		t.Errorf("should be valid when mtime+size match")
	}

	cached.FeatureMtime = info.ModTime().Add(-time.Hour)
	if IsCacheValid(file, cached) {
		t.Errorf("should be invalid when mtime differs")
	}

	cached.FeatureMtime = info.ModTime()
	cached.FeatureSize = info.Size() + 1
	if IsCacheValid(file, cached) {
		t.Errorf("should be invalid when size differs")
	}
}

func TestIsCacheValid_StatFailReturnsFalse(t *testing.T) {
	cached := &CachedFeaturesDB{FeatureMtime: time.Now(), FeatureSize: 100}
	if IsCacheValid("/nonexistent/path/that/does/not/exist", cached) {
		t.Errorf("should be invalid when stat fails")
	}
}

func TestIsCacheValid_NilReturnsFalse(t *testing.T) {
	if IsCacheValid("/whatever", nil) {
		t.Errorf("nil cached should always be invalid")
	}
}

func TestWriteBackFeatures_UpdatesRow(t *testing.T) {
	db := setupSimilarityTestDB(t)
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_distributing
        (path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time, create_time, update_time, disable)
        VALUES ('/c', 1, 1, 'CS_WB', 1, '127.0.0.1', 'aa:bb', ?, ?, ?, 0)`, now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	err = WriteBackFeatures(db, "CS_WB", CachedFeaturesDB{
		Simhash: 99, ContentHash: "xyz", FeatureMtime: now, FeatureSize: 42,
	})
	if err != nil {
		t.Fatal(err)
	}

	var simhash int64
	db.Get(&simhash, `SELECT simhash FROM data_distributing WHERE content_sign='CS_WB'`)
	if simhash != 99 {
		t.Errorf("simhash after write = %d, want 99", simhash)
	}
}

func TestQueryExtractedText_ReturnsEmptyWhenNULL(t *testing.T) {
	db := setupSimilarityTestDB(t)
	now := time.Now()
	_, _ = db.Exec(`INSERT INTO data_distributing
        (path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time, create_time, update_time, disable)
        VALUES ('/d', 1, 1, 'CS_NOTEXT', 1, '127.0.0.1', 'aa:bb', ?, ?, ?, 0)`, now, now, now)

	text := QueryExtractedText(db, "CS_NOTEXT")
	if text != "" {
		t.Errorf("expected empty, got %q", text)
	}
}

func TestQueryExtractedText_ReturnsValue(t *testing.T) {
	db := setupSimilarityTestDB(t)
	now := time.Now()
	_, _ = db.Exec(`INSERT INTO data_distributing
        (path, data_type, scan_found_count, content_sign, file_size, extracted_text, ip, mac_address, scan_time, create_time, update_time, disable)
        VALUES ('/e', 1, 1, 'CS_TEXT', 1, 'hello world', '127.0.0.1', 'aa:bb', ?, ?, ?, 0)`, now, now, now)

	text := QueryExtractedText(db, "CS_TEXT")
	if text != "hello world" {
		t.Errorf("got %q", text)
	}
}

// guard import unused
var _ = sql.ErrNoRows
