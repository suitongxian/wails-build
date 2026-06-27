package scanner

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	"data-asset-scan-go/internal/repository"
)

// openTestDBWithScannerTables opens an in-memory SQLite DB and runs all migrations.
func openTestDBWithScannerTables(t *testing.T) *sqlx.DB {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")

	sqlDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	db := sqlx.NewDb(sqlDB, "sqlite3")
	if err := repository.RunMigrationsForTest(db); err != nil {
		t.Fatalf("RunMigrationsForTest: %v", err)
	}
	return db
}

// 并发正确性核心测试：种 N 个已知内容文件，跑完 FULL_INVENTORY 后验证
// data_distributing 表里每行的 content_sign 跟我们本地算的 sha256 完全一致，
// 而且每个文件路径都被入库（无遗漏、无重复）。
func TestAtomicScanner_FullInventory_ParallelProducesIdenticalRecords(t *testing.T) {
	tmp := t.TempDir()
	type expected struct {
		path string
		hash string
	}
	var want []expected

	// 创建 50 个文件，每个内容不同，hash 也都不同
	for i := 0; i < 50; i++ {
		path := filepath.Join(tmp, fmt.Sprintf("f%03d.pdf", i))
		content := []byte(fmt.Sprintf("hello-%d-test-content", i))
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		want = append(want, expected{path: path, hash: strings.ToUpper(hex.EncodeToString(sum[:]))})
	}

	// 加几对内容相同的文件（验证 md5StatsMap source_count 聚合）
	dupContent := []byte("dup-content-for-aggregation")
	dupSum := sha256.Sum256(dupContent)
	dupHash := strings.ToUpper(hex.EncodeToString(dupSum[:]))
	for _, name := range []string{"dup1.pdf", "dup2.pdf", "dup3.pdf"} {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, dupContent, 0644); err != nil {
			t.Fatal(err)
		}
		want = append(want, expected{path: path, hash: dupHash})
	}

	db := openTestDBWithScannerTables(t)
	defer db.Close()

	s := NewAtomicScanner(db, 100)
	result := s.Scan(AtomicScanOptions{
		Directory:        tmp,
		Extensions:       []string{".pdf"},
		ScanMode:         FullInventory,
		MD5Concurrency:   4,
		BatchSize:        10,
		ProgressInterval: 10,
	})
	if !result.Success {
		t.Fatalf("scan failed: %s", result.ErrorMessage)
	}
	if result.ScannedFiles != len(want) {
		t.Errorf("ScannedFiles = %d, want %d", result.ScannedFiles, len(want))
	}

	// 查 data_distributing
	var rows []struct {
		Path        string `db:"path"`
		ContentSign string `db:"content_sign"`
	}
	if err := db.Select(&rows, `SELECT path, content_sign FROM data_distributing WHERE disable = 0 ORDER BY path`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(want) {
		t.Fatalf("data_distributing row count = %d, want %d", len(rows), len(want))
	}
	sort.Slice(want, func(i, j int) bool { return want[i].path < want[j].path })
	for i, r := range rows {
		if r.Path != want[i].path {
			t.Errorf("row[%d].path = %q, want %q", i, r.Path, want[i].path)
		}
		if r.ContentSign != want[i].hash {
			t.Errorf("row[%d].hash = %q, want %q", i, r.ContentSign, want[i].hash)
		}
	}

	// 验证 data_resources 聚合：3 个 dup 文件应该有 1 行 source_count=3
	var dupRow struct {
		SourceCount int `db:"source_count"`
	}
	if err := db.Get(&dupRow, `SELECT source_count FROM data_resources WHERE content_sign = ? AND disable = 0`, dupHash); err != nil {
		t.Fatalf("data_resources dup lookup: %v", err)
	}
	if dupRow.SourceCount != 3 {
		t.Errorf("dup source_count = %d, want 3", dupRow.SourceCount)
	}
}
