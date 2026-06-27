package similarity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

// stubLoader returns a fixed list of FileInputs.
type stubLoader struct{ inputs []FileInput }

func (s *stubLoader) LoadInputs() ([]FileInput, error) { return s.inputs, nil }

// stubPersister captures SaveFamily calls so the test can inspect them.
type stubPersister struct {
	saved      []repository.FamilyInsert
	resetCalls int
}

func (s *stubPersister) ResetFamilies() error { s.resetCalls++; return nil }
func (s *stubPersister) SaveFamily(in repository.FamilyInsert) error {
	s.saved = append(s.saved, in)
	return nil
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func mkFile(t *testing.T, dir, name, content string) (string, string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return full, sha(content)
}

func TestAnalyzeFromDB_PersistsFamilyAndCallsReset(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	pa, ha := mkFile(t, dir, "doc.txt", "hello world from project alpha")
	pb, hb := mkFile(t, dir, "doc_copy.txt", "hello world from project alpha") // identical

	loader := &stubLoader{inputs: []FileInput{
		{UniqueID: "1", Path: pa, ContentSign: ha, Size: 30, ModTime: now},
		{UniqueID: "2", Path: pb, ContentSign: hb, Size: 30, ModTime: now},
	}}
	persister := &stubPersister{}

	res, err := AnalyzeFromDB(context.Background(), loader, persister,
		AnalyzerOptions{Reset: true})
	if err != nil {
		t.Fatalf("AnalyzeFromDB: %v", err)
	}

	if persister.resetCalls != 1 {
		t.Errorf("expected ResetFamilies called once, got %d", persister.resetCalls)
	}
	// Same content → same content_sign → dedup leaves only 1 assignment, so the
	// family should be skipped (singleton, < 2 distinct hashes). Verify.
	if len(persister.saved) != 0 {
		t.Errorf("expected 0 saved families (singleton dedup), got %d", len(persister.saved))
	}
	if res.InputCount != 2 {
		t.Errorf("InputCount: got %d", res.InputCount)
	}
}

func TestAnalyzeFromDB_DistinctHashesInOneFamily(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Two textually-similar but byte-different files → different hashes,
	// same family.
	base := "项目周报：本周完成扫描器开发，下周进行测试与上线。负责人：张三。"
	revised := "项目周报：本周完成扫描器开发，下周进行测试与上线。负责人：李四。备注：审查中。"
	pa, ha := mkFile(t, dir, "report_v1.txt", base)
	pb, hb := mkFile(t, dir, "report_v2.txt", revised)

	loader := &stubLoader{inputs: []FileInput{
		{UniqueID: "1", Path: pa, ContentSign: ha, Size: int64(len(base)), ModTime: now},
		{UniqueID: "2", Path: pb, ContentSign: hb, Size: int64(len(revised)), ModTime: now.Add(time.Minute)},
	}}
	persister := &stubPersister{}

	if _, err := AnalyzeFromDB(context.Background(), loader, persister,
		AnalyzerOptions{Reset: true}); err != nil {
		t.Fatalf("AnalyzeFromDB: %v", err)
	}

	if len(persister.saved) != 1 {
		t.Fatalf("expected exactly 1 saved family, got %d", len(persister.saved))
	}
	fam := persister.saved[0]
	if len(fam.Members) != 2 {
		t.Fatalf("expected 2 distinct-hash members, got %d", len(fam.Members))
	}
	primaryCount := 0
	for _, m := range fam.Members {
		if m.IsPrimary {
			primaryCount++
		}
	}
	if primaryCount != 1 {
		t.Errorf("expected exactly 1 primary, got %d", primaryCount)
	}
	if fam.PrimaryContentSign == "" {
		t.Error("PrimaryContentSign empty")
	}
}
