package repository

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func openMigratedTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "v1.db")
	sqlDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	db := sqlx.NewDb(sqlDB, "sqlite3")
	if err := RunMigrationsForTest(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestMigration_AddsDataOriginColumn(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	ok, err := columnExists(db, "data_resources", "data_origin")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !ok {
		t.Fatal("data_resources.data_origin column missing after migration")
	}
}

func TestMigration_StampsExistingRowsAsHistorical(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		create_time, update_time, disable, data_origin
	) VALUES ('CS_BEFORE_BASELINE', 1, 1, ?, ?, ?, 0, 'new')`,
		now, now, now)
	if err != nil {
		t.Fatalf("seed pre-baseline row: %v", err)
	}

	if err := RunMigrationsForTest(db); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}

	var got string
	if err := db.Get(&got, `SELECT data_origin FROM data_resources WHERE content_sign = 'CS_BEFORE_BASELINE'`); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "historical" {
		t.Errorf("data_origin = %q, want historical (baseline not closed yet)", got)
	}
}

func TestMigration_SeedsBaselineCompletedAtRow(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	var rows int
	if err := db.Get(&rows, `SELECT COUNT(*) FROM system_config WHERE key = 'baseline_completed_at'`); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("baseline_completed_at row count = %d, want 1", rows)
	}

	var value sql.NullString
	if err := db.Get(&value, `SELECT value FROM system_config WHERE key = 'baseline_completed_at'`); err != nil {
		t.Fatalf("read value: %v", err)
	}
	if value.Valid && value.String != "" {
		t.Errorf("baseline_completed_at value = %q, want empty/NULL", value.String)
	}
}
