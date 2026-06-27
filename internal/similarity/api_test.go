package similarity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFile creates a file with given content and returns its absolute path
// plus its sha256 hex (used to populate FileInput.ContentSign).
func writeFile(t *testing.T, dir, name, content string) (string, string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	sum := sha256.Sum256([]byte(content))
	return full, hex.EncodeToString(sum[:])
}

// TestBuildFamilies_ExactDuplicates verifies the simplest case:
// two files with identical content end up in the same family with relation
// same_content (score 1.0).
func TestBuildFamilies_ExactDuplicates(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	pathA, hashA := writeFile(t, dir, "report.txt", "hello world\nthis is a report")
	pathB, hashB := writeFile(t, dir, "report_copy.txt", "hello world\nthis is a report")

	if hashA != hashB {
		t.Fatalf("expected identical hashes for identical content")
	}

	inputs := []FileInput{
		{UniqueID: "a", Path: pathA, ContentSign: hashA, Size: 28, ModTime: now},
		{UniqueID: "b", Path: pathB, ContentSign: hashB, Size: 28, ModTime: now},
	}

	fams, err := BuildFamilies(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("BuildFamilies: %v", err)
	}
	if len(fams) != 1 {
		t.Fatalf("expected 1 family, got %d", len(fams))
	}
	if fams[0].MemberCount != 2 {
		t.Fatalf("expected 2 members, got %d", fams[0].MemberCount)
	}
	for _, m := range fams[0].Members {
		if m.Relation != "same_content" {
			t.Errorf("member %s: expected same_content, got %s", m.UniqueID, m.Relation)
		}
		if m.Score < 0.99 {
			t.Errorf("member %s: expected score≈1.0, got %v", m.UniqueID, m.Score)
		}
	}
}

// TestBuildFamilies_IndependentFiles verifies that two unrelated files are
// NOT placed into the same family.
func TestBuildFamilies_IndependentFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	pathA, hashA := writeFile(t, dir, "alpha.txt", "alpha alpha alpha")
	pathB, hashB := writeFile(t, dir, "zzz_unrelated.bin", "completely different binary content here")

	inputs := []FileInput{
		{UniqueID: "a", Path: pathA, ContentSign: hashA, Size: 17, ModTime: now},
		{UniqueID: "b", Path: pathB, ContentSign: hashB, Size: 41, ModTime: now},
	}

	fams, err := BuildFamilies(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("BuildFamilies: %v", err)
	}
	// Two unrelated files should yield zero families (singletons aren't returned).
	if len(fams) != 0 {
		t.Fatalf("expected 0 families for unrelated files, got %d (members: %+v)", len(fams), fams)
	}
}

// TestBuildFamilies_TextSimilarity verifies that two text files with high
// content overlap but different bytes still cluster into one family with
// process_version or derived relation.
func TestBuildFamilies_TextSimilarity(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	base := "项目周报\n本周完成扫描器开发，下周进行测试与上线。\n负责人：张三\n"
	revised := "项目周报\n本周完成扫描器开发，下周进行测试与上线。\n负责人：李四\n备注：已通过初步审查。\n"

	pathA, hashA := writeFile(t, dir, "weekly_report.txt", base)
	pathB, hashB := writeFile(t, dir, "weekly_report_v2.txt", revised)

	inputs := []FileInput{
		{UniqueID: "a", Path: pathA, ContentSign: hashA, Size: int64(len(base)), ModTime: now},
		{UniqueID: "b", Path: pathB, ContentSign: hashB, Size: int64(len(revised)), ModTime: now.Add(time.Minute)},
	}

	fams, err := BuildFamilies(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("BuildFamilies: %v", err)
	}
	if len(fams) != 1 {
		t.Fatalf("expected 1 family for similar texts, got %d", len(fams))
	}
	if fams[0].MemberCount != 2 {
		t.Fatalf("expected 2 members in similar-text family, got %d", fams[0].MemberCount)
	}
	// Primary will have score 1.0; the non-primary should be one of the three relations.
	nonPrimaryRel := ""
	for _, m := range fams[0].Members {
		if m.UniqueID != fams[0].PrimaryID {
			nonPrimaryRel = m.Relation
		}
	}
	if nonPrimaryRel != "same_content" && nonPrimaryRel != "process_version" && nonPrimaryRel != "derived" {
		t.Errorf("non-primary member relation unexpected: %q", nonPrimaryRel)
	}
}
