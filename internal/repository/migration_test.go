package repository

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// openTestDB opens a fresh SQLite db at <tempDir>/test.db, applies the embedded
// schema and migrations, and returns the sqlx handle. It does NOT use the
// package-level singleton so multiple test cases can run independently.
func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	// 与生产 openDB 保持一致：单连接池。否则事务内的 r.DB.Get 死锁问题
	// 在测试里永远复现不了。
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	db := sqlx.NewDb(sqlDB, "sqlite3")
	if err := loadSchema(db); err != nil {
		t.Fatalf("loadSchema: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	return db
}

func TestMigrations_FreshDBHasNewTablesAndColumns(t *testing.T) {
	db := openTestDB(t)

	// data_resource_family table exists
	var n int
	if err := db.Get(&n, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='data_resource_family'"); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("data_resource_family table missing")
	}

	// content_text_cache table exists
	if err := db.Get(&n, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='content_text_cache'"); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("content_text_cache table missing")
	}

	// data_resources has family_id / family_relation / family_score
	for _, col := range []string{"family_id", "family_relation", "family_score"} {
		ok, err := columnExists(db, "data_resources", col)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("data_resources.%s missing", col)
		}
	}
}

func TestMigrations_IdempotentOnSecondRun(t *testing.T) {
	db := openTestDB(t)
	// Running again on the already-migrated DB must not error.
	if err := runMigrations(db); err != nil {
		t.Fatalf("second runMigrations failed: %v", err)
	}
	// Inserting and reading new columns works.
	_, err := db.Exec(`INSERT INTO data_resources
		(content_sign, source_count, workspace_source_count, first_create_time, family_id, family_relation, family_score, create_time, update_time)
		VALUES ('CS1', 1, 0, '2026-04-30', 7, 'same_content', 0.97, '2026-04-30', '2026-04-30')`)
	if err != nil {
		t.Fatalf("insert with new columns: %v", err)
	}
	var rel string
	var score float64
	if err := db.QueryRow(`SELECT family_relation, family_score FROM data_resources WHERE content_sign='CS1'`).Scan(&rel, &score); err != nil {
		t.Fatal(err)
	}
	if rel != "same_content" || score < 0.96 {
		t.Errorf("readback wrong: rel=%q score=%v", rel, score)
	}
}

func TestMigrations_OldDBWithoutNewColumns(t *testing.T) {
	// Simulate an old database that has the original data_resources schema
	// but none of the new family columns. runMigrations must add them.
	path := filepath.Join(t.TempDir(), "old.db")
	sqlDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db := sqlx.NewDb(sqlDB, "sqlite3")

	// Load only the original embedded schema (not migrations).
	for _, stmt := range splitSQLStatements(schemaSQL) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("load original schema: %v", err)
		}
	}

	// Confirm the new columns are NOT yet present.
	if ok, _ := columnExists(db, "data_resources", "family_id"); ok {
		t.Fatalf("precondition: family_id should not exist in old schema")
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations on old db: %v", err)
	}

	for _, col := range []string{"family_id", "family_relation", "family_score"} {
		if ok, _ := columnExists(db, "data_resources", col); !ok {
			t.Errorf("post-migration: data_resources.%s missing", col)
		}
	}
}
