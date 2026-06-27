package repository

import (
	"testing"
)

func TestMigration_DataDistributingFeatureCacheColumns(t *testing.T) {
	db := openTestDB(t)

	cols := []string{"simhash", "content_hash", "phash", "extracted_text", "feature_mtime", "feature_size"}
	for _, col := range cols {
		var present int
		err := db.Get(&present, `SELECT COUNT(*) FROM pragma_table_info('data_distributing') WHERE name = ?`, col)
		if err != nil {
			t.Fatalf("query column %s: %v", col, err)
		}
		if present != 1 {
			t.Errorf("column %s missing in data_distributing", col)
		}
	}

	var idxCount int
	if err := db.Get(&idxCount, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_data_distributing_content_hash'`); err != nil {
		t.Fatalf("query index presence: %v", err)
	}
	if idxCount != 1 {
		t.Errorf("index idx_data_distributing_content_hash missing")
	}
}

func TestMigration_OldDataRowsAreNullSafe(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Exec(`INSERT INTO data_distributing
		(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time, create_time, update_time, disable)
		VALUES ('/old/file.pdf', 0, 1, 'OLD_HASH', 100, '127.0.0.1', 'aa:bb:cc:dd:ee:ff', '2026-01-01', '2026-01-01', '2026-01-01', 0)`)
	if err != nil {
		t.Fatalf("insert old row: %v", err)
	}

	var simhash, contentHash, extractedText *string
	err = db.QueryRow(`SELECT simhash, content_hash, extracted_text FROM data_distributing WHERE content_sign='OLD_HASH'`).
		Scan(&simhash, &contentHash, &extractedText)
	if err != nil {
		t.Fatalf("query old row: %v", err)
	}
	if simhash != nil || contentHash != nil || extractedText != nil {
		t.Errorf("old row features should be NULL, got %v %v %v", simhash, contentHash, extractedText)
	}
}
