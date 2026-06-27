package similarity

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// openE2EDB spins up a fresh sqlite db, applies the embedded schema and
// migrations, and returns the sqlx handle.
func openE2EDB(t *testing.T) *sqlx.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "e2e.db")
	sqlDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db := sqlx.NewDb(sqlDB, "sqlite3")

	// Hand-execute the same statements InitDB would have; skip the singleton.
	if err := repository.RunMigrationsForTest(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return db
}

// E2E_BuildPipeline mirrors a realistic flow:
//   1. Write three text files: two near-identical reports + one unrelated note.
//   2. Insert them into data_distributing + seed data_resources rows for each
//      content_sign (mirroring what scanner does post-scan).
//   3. Run AnalyzeFromDB via DBLoader/DBPersister.
//   4. Assert: the two reports collapse into one family, with one row marked
//      primary; data_resources gets family_id/relation/score populated.
//   5. Assert: IDsInFamily on that family returns both report data_resources_id.
func TestE2E_BuildPipelineWithRealDB(t *testing.T) {
	db := openE2EDB(t)
	dir := t.TempDir()

	// Step 1: write files.
	type sample struct {
		name, content string
	}
	samples := []sample{
		{"report_v1.txt", "项目周报：本周完成扫描器开发，下周进行测试与上线。负责人：张三。"},
		{"report_v2.txt", "项目周报：本周完成扫描器开发，下周进行测试与上线。负责人：李四。备注：审查中。"},
		{"unrelated.txt", "这是一个完全不相关的文件，里面只有一些随便写的内容。"},
	}

	type seed struct {
		ddID, drID int64
		path, cs   string
	}
	seeds := make([]seed, 0, len(samples))
	now := time.Now()
	for _, s := range samples {
		full := filepath.Join(dir, s.name)
		if err := os.WriteFile(full, []byte(s.content), 0644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
		sum := md5.Sum([]byte(s.content))
		cs := strings.ToUpper(hex.EncodeToString(sum[:]))

		// Insert into data_distributing.
		ddRes, err := db.Exec(`INSERT INTO data_distributing
			(scan_task_id, path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time, create_time, update_time)
			VALUES (NULL, ?, 1, 1, ?, ?, '127.0.0.1', 'aa:bb', ?, ?, ?)`,
			full, cs, int64(len(s.content)), now, now, now)
		if err != nil {
			t.Fatalf("insert dd: %v", err)
		}
		ddID, _ := ddRes.LastInsertId()

		// Seed data_resources (one row per content_sign).
		drRes, err := db.Exec(`INSERT INTO data_resources
			(content_sign, source_count, workspace_source_count, first_create_time, resources_name, create_time, update_time)
			VALUES (?, 1, 0, ?, ?, ?, ?)`, cs, now, s.name, now, now)
		if err != nil {
			t.Fatalf("insert dr: %v", err)
		}
		drID, _ := drRes.LastInsertId()
		seeds = append(seeds, seed{ddID, drID, full, cs})
	}

	// Step 2/3: run the analyzer.
	loader := &DBLoader{Repo: repository.NewDataDistributingRepository(db, 100)}
	famRepo := repository.NewFamilyRepository(db)
	persister := &DBPersister{Repo: famRepo}

	res, err := AnalyzeFromDB(context.Background(), loader, persister, AnalyzerOptions{Reset: true})
	if err != nil {
		t.Fatalf("AnalyzeFromDB: %v", err)
	}
	if res.InputCount != 3 {
		t.Errorf("InputCount: got %d want 3", res.InputCount)
	}

	// Step 4: exactly one family persisted (the two reports). The unrelated
	// file should not form a family.
	var famCount int
	if err := db.Get(&famCount, `SELECT COUNT(*) FROM data_resource_family`); err != nil {
		t.Fatal(err)
	}
	if famCount != 1 {
		t.Fatalf("expected 1 family, got %d", famCount)
	}

	var famID int64
	if err := db.Get(&famID, `SELECT family_id FROM data_resource_family LIMIT 1`); err != nil {
		t.Fatal(err)
	}

	// Step 5: family expansion picks up both report data_resources_ids.
	ids, err := famRepo.IDsInFamily(famID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 members in family, got %d (ids=%v)", len(ids), ids)
	}

	// Verify exactly one is primary.
	var primaryCnt int
	if err := db.Get(&primaryCnt, `SELECT COUNT(*) FROM data_resources WHERE family_id = ? AND family_relation = 'primary'`, famID); err != nil {
		t.Fatal(err)
	}
	if primaryCnt != 1 {
		t.Errorf("expected 1 primary, got %d", primaryCnt)
	}

	// Verify the unrelated file did NOT get a family_id assigned.
	var unrelatedFam *int64
	if err := db.Get(&unrelatedFam, `SELECT family_id FROM data_resources WHERE resources_name = 'unrelated.txt'`); err != nil {
		t.Fatal(err)
	}
	if unrelatedFam != nil {
		t.Errorf("unrelated file should not be in any family, got family_id=%d", *unrelatedFam)
	}
}
