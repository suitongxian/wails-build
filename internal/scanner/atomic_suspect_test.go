package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

// FullInventory 扫描应当对疑似非个人文件打 suspect_non_personal=1
func TestAtomicScan_FullInventory_MarksSuspectNonPersonal(t *testing.T) {
	tmp := t.TempDir()
	// 1. 一个明显的二进制文件（.dll 后缀）→ 应当 suspect=1
	binPath := filepath.Join(tmp, "fakelib.dll")
	if err := os.WriteFile(binPath, []byte("MZ\x90\x00fake-dll-content"), 0644); err != nil {
		t.Fatal(err)
	}
	// 2. 一个普通的 .txt 文件 → suspect=0
	txtPath := filepath.Join(tmp, "note.txt")
	if err := os.WriteFile(txtPath, []byte("personal note content"), 0644); err != nil {
		t.Fatal(err)
	}

	db := openTestDBWithScannerTables(t)
	defer db.Close()

	s := NewAtomicScanner(db, 100)
	result := s.Scan(AtomicScanOptions{
		Directory:      tmp,
		Extensions:     []string{".dll", ".txt"},
		ScanMode:       FullInventory,
		MD5Concurrency: 2,
		BatchSize:      10,
	})
	if !result.Success {
		t.Fatalf("scan failed: %s", result.ErrorMessage)
	}

	if got := getSuspect(t, db, binPath); got != 1 {
		t.Errorf("%s: suspect = %d, want 1 (.dll should be flagged)", binPath, got)
	}
	if got := getSuspect(t, db, txtPath); got != 0 {
		t.Errorf("%s: suspect = %d, want 0 (normal txt should not be flagged)", txtPath, got)
	}
}

// 路径模式判定：模拟 macOS Library/Caches 这种**不以 . 开头**所以
// 不被 walker 默认排除的系统目录，应当被 suspect 打标。
// （以 . 开头的目录 walker 默认 skip，所以 .cache/.git/.idea 等根本扫不到，
//  对应的 suspect path 规则虽存在但不会触发；这一层是冗余防御）
func TestAtomicScan_FullInventory_MarksSuspectByPathPattern(t *testing.T) {
	tmp := t.TempDir()
	cachePath := filepath.Join(tmp, "Library", "Caches", "com.app", "data.txt")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("library cache content"), 0644); err != nil {
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
	if got := getSuspect(t, db, cachePath); got != 1 {
		t.Errorf("%s: suspect = %d, want 1 (path contains /library/)", cachePath, got)
	}
}

func getSuspect(t *testing.T, db *sqlx.DB, path string) int {
	t.Helper()
	var got int
	err := db.Get(&got, `SELECT suspect_non_personal FROM data_distributing WHERE path = ?`, path)
	if err != nil {
		t.Fatalf("query suspect for %s: %v", path, err)
	}
	return got
}
