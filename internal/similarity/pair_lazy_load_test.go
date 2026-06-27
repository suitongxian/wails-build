package similarity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPairSimilarity_LazyLoadFromDB verifies pair similarity uses lazy-loaded
// text from DB when cache hit stored fullText="" (Task 6 behavior).
// extractTextHook must not be called because DB has extracted_text populated.
func TestPairSimilarity_LazyLoadFromDB(t *testing.T) {
	tmp := t.TempDir()
	fileA := filepath.Join(tmp, "a.txt")
	fileB := filepath.Join(tmp, "b.txt")
	contentA := "shared content for similarity testing of lazy load behavior"
	contentB := "shared content for similarity testing of different content" // partial overlap
	os.WriteFile(fileA, []byte(contentA), 0644)
	os.WriteFile(fileB, []byte(contentB), 0644)
	infoA, _ := os.Stat(fileA)
	infoB, _ := os.Stat(fileB)

	db := openE2EDB(t)
	SetDB(db)
	defer SetDB(nil)

	// Seed DB with both rows: features cached + extracted_text populated.
	now := time.Now()
	insert := `INSERT INTO data_distributing
		(path, content_sign, file_create_time, file_size,
		 simhash, content_hash, extracted_text, feature_mtime, feature_size,
		 data_type, scan_found_count, ip, mac_address, scan_time,
		 create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?,
		        'FILE', 1, '', '', ?,
		        ?, ?, 0)`
	if _, err := db.Exec(insert,
		fileA, "CS_A", now, infoA.Size(),
		int64(simhash(contentA)), contentHashFromText(contentA), contentA,
		infoA.ModTime(), infoA.Size(),
		now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insert,
		fileB, "CS_B", now, infoB.Size(),
		int64(simhash(contentB)), contentHashFromText(contentB), contentB,
		infoB.ModTime(), infoB.Size(),
		now, now, now); err != nil {
		t.Fatal(err)
	}

	// Install spy hook: extractText should never be called (cache hit + lazy load).
	callCount := 0
	origExtract := extractTextHook
	extractTextHook = func(path string, timeout time.Duration) string {
		callCount++
		return ""
	}
	defer func() { extractTextHook = origExtract }()

	inputs := []FileInput{
		{ContentSign: "CS_A", Path: fileA, Size: infoA.Size(), ModTime: infoA.ModTime()},
		{ContentSign: "CS_B", Path: fileB, Size: infoB.Size(), ModTime: infoB.ModTime()},
	}

	_, err := BuildFamilies(context.Background(), inputs, defaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	if callCount != 0 {
		t.Errorf("extractTextHook called %d times, want 0 (cache hit + lazy load should avoid re-reads)", callCount)
	}
}

// TestPairSimilarity_LazyLoadEmptyFallsBackToFile verifies that when
// extracted_text in DB is NULL/empty (partial cache state), the worker falls
// back to computeSimilarity (file re-read). This documents the safety net.
func TestPairSimilarity_LazyLoadEmptyFallsBackToFile(t *testing.T) {
	tmp := t.TempDir()
	fileA := filepath.Join(tmp, "fa.txt")
	fileB := filepath.Join(tmp, "fb.txt")
	os.WriteFile(fileA, []byte("alpha content text"), 0644)
	os.WriteFile(fileB, []byte("alpha content text"), 0644)
	infoA, _ := os.Stat(fileA)
	infoB, _ := os.Stat(fileB)

	db := openE2EDB(t)
	SetDB(db)
	defer SetDB(nil)

	// Seed DB with cached simhash + content_hash but NO extracted_text (NULL).
	now := time.Now()
	insert := `INSERT INTO data_distributing
		(path, content_sign, file_create_time, file_size,
		 simhash, content_hash, feature_mtime, feature_size,
		 data_type, scan_found_count, ip, mac_address, scan_time,
		 create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?,
		        'FILE', 1, '', '', ?,
		        ?, ?, 0)`
	if _, err := db.Exec(insert,
		fileA, "CS_FA", now, infoA.Size(),
		int64(simhash("alpha content text")), contentHashFromText("alpha content text"),
		infoA.ModTime(), infoA.Size(),
		now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insert,
		fileB, "CS_FB", now, infoB.Size(),
		int64(simhash("alpha content text")), contentHashFromText("alpha content text"),
		infoB.ModTime(), infoB.Size(),
		now, now, now); err != nil {
		t.Fatal(err)
	}

	inputs := []FileInput{
		{ContentSign: "CS_FA", Path: fileA, Size: infoA.Size(), ModTime: infoA.ModTime()},
		{ContentSign: "CS_FB", Path: fileB, Size: infoB.Size(), ModTime: infoB.ModTime()},
	}

	// Just ensure BuildFamilies completes without error (fallback works).
	_, err := BuildFamilies(context.Background(), inputs, defaultConfig())
	if err != nil {
		t.Fatalf("BuildFamilies failed with NULL extracted_text fallback: %v", err)
	}
}
