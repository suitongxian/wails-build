package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAtomicScan_WritesFeatureCache verifies that a text file scanned via FULL_INVENTORY
// gets content_hash, extracted_text, and feature_size populated in data_distributing.
func TestAtomicScan_WritesFeatureCache(t *testing.T) {
	tmp := t.TempDir()
	txt := filepath.Join(tmp, "doc.txt")
	// Write enough text to pass the 20-char normalized threshold in contentHashFromText
	content := "hello world test document for feature extraction and similarity computation"
	if err := os.WriteFile(txt, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	db := openTestDBWithScannerTables(t)
	defer db.Close()

	s := NewAtomicScanner(db, 100)
	result := s.Scan(AtomicScanOptions{
		Directory:      tmp,
		Extensions:     []string{".txt"},
		ScanMode:       FullInventory,
		MD5Concurrency: 2,
		BatchSize:      10,
	})
	if !result.Success {
		t.Fatalf("scan failed: %s", result.ErrorMessage)
	}

	var row struct {
		ContentHash   *string `db:"content_hash"`
		ExtractedText *string `db:"extracted_text"`
		FeatureSize   *int64  `db:"feature_size"`
		FeatureMtime  *string `db:"feature_mtime"`
	}
	err := db.Get(&row,
		`SELECT content_hash, extracted_text, feature_size, feature_mtime FROM data_distributing WHERE path = ?`, txt)
	if err != nil {
		t.Fatalf("query data_distributing: %v", err)
	}
	if row.ContentHash == nil || *row.ContentHash == "" {
		t.Errorf("content_hash should be populated for text file, got nil/empty")
	}
	if row.ExtractedText == nil || *row.ExtractedText == "" {
		t.Errorf("extracted_text should be populated for text file, got nil/empty")
	}
	if row.FeatureSize == nil {
		t.Errorf("feature_size should be set for text file")
	} else if *row.FeatureSize <= 0 {
		t.Errorf("feature_size should be > 0, got %d", *row.FeatureSize)
	}
	if row.FeatureMtime == nil {
		t.Errorf("feature_mtime should be set for text file")
	}
}

// TestAtomicScan_CorruptedFileStillHashes verifies that a corrupted PDF (invalid bytes)
// still gets its hash (content_sign) written even if feature extraction yields nothing.
func TestAtomicScan_CorruptedFileStillHashes(t *testing.T) {
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "bad.pdf")
	// PDF magic bytes followed by garbage
	if err := os.WriteFile(bad, []byte{0x25, 0x50, 0x44, 0x46, 0xff, 0xff, 0x00, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}

	db := openTestDBWithScannerTables(t)
	defer db.Close()

	s := NewAtomicScanner(db, 100)
	result := s.Scan(AtomicScanOptions{
		Directory:      tmp,
		Extensions:     []string{".pdf"},
		ScanMode:       FullInventory,
		MD5Concurrency: 2,
		BatchSize:      10,
	})
	if !result.Success {
		t.Fatalf("scan failed: %s", result.ErrorMessage)
	}

	var hash string
	err := db.Get(&hash,
		`SELECT content_sign FROM data_distributing WHERE path = ?`, bad)
	if err != nil {
		t.Fatalf("query data_distributing: %v", err)
	}
	if hash == "" {
		t.Errorf("content_sign should still be written for corrupted file")
	}
}
