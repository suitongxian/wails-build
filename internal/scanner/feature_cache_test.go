package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFeaturesForCache_DocFile(t *testing.T) {
	tmp := t.TempDir()
	txtPath := filepath.Join(tmp, "test.txt")
	content := "This is a test document for feature extraction. It contains enough text to produce a meaningful simhash."
	if err := os.WriteFile(txtPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(txtPath)

	feat, err := ExtractFeaturesForCache(txtPath, "text/plain", info)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if feat.Simhash == 0 {
		t.Errorf("simhash should be non-zero for non-empty text")
	}
	if feat.ContentHash == "" {
		t.Errorf("content_hash should be non-empty for sufficient text")
	}
	if feat.ExtractedText == "" {
		t.Errorf("extracted_text should be non-empty")
	}
	if feat.Mtime.IsZero() {
		t.Errorf("mtime should be set")
	}
	if feat.Size != info.Size() {
		t.Errorf("size = %d, want %d", feat.Size, info.Size())
	}
}

func TestExtractFeaturesForCache_CorruptedFile_DoesNotPanic(t *testing.T) {
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "corrupted.pdf")
	os.WriteFile(bad, []byte{0x25, 0x50, 0x44, 0x46, 0xff, 0xff, 0xff}, 0644)
	info, _ := os.Stat(bad)

	feat, err := ExtractFeaturesForCache(bad, "application/pdf", info)
	// should not panic; either err != nil or feat.ExtractedText is empty
	if err == nil && feat.ExtractedText != "" {
		t.Errorf("corrupted PDF should fail or return empty text, got %d chars", len(feat.ExtractedText))
	}
}

func TestExtractFeaturesForCache_UnsupportedMIME_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "data.bin")
	os.WriteFile(bin, []byte{1, 2, 3, 4, 5}, 0644)
	info, _ := os.Stat(bin)

	feat, err := ExtractFeaturesForCache(bin, "application/octet-stream", info)
	if err != nil {
		t.Errorf("unsupported MIME should not error: %v", err)
	}
	if feat.ExtractedText != "" {
		t.Errorf("unsupported MIME should produce no text")
	}
}
