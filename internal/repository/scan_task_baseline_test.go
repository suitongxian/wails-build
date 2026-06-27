package repository

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func newScanTaskRow(t *testing.T, db *sqlx.DB) int64 {
	t.Helper()
	now := time.Now()
	r, err := db.Exec(`INSERT INTO scan_task (
		scan_type, heartbeat, task_state, create_time, update_time, disable
	) VALUES ('FILE', 0, 'run', ?, ?, 0)`, now, now)
	if err != nil {
		t.Fatalf("insert scan_task: %v", err)
	}
	id, _ := r.LastInsertId()
	return id
}

func TestMarkSucceeded_ClosesBaselineFirstTime(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	taskID := newScanTaskRow(t, db)
	repo := NewScanTaskRepository(db)
	if err := repo.MarkSucceeded(taskID); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	cfg := NewSystemConfigRepository(db)
	got := cfg.GetValue("baseline_completed_at")
	if got == "" {
		t.Fatal("baseline_completed_at still empty after first MarkSucceeded")
	}
}

func TestMarkSucceeded_IsNoOpAfterBaselineClosed(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	cfg := NewSystemConfigRepository(db)
	preset := "2025-01-01T00:00:00Z"
	cfg.SetValue("baseline_completed_at", preset)

	taskID := newScanTaskRow(t, db)
	repo := NewScanTaskRepository(db)
	if err := repo.MarkSucceeded(taskID); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	if got := cfg.GetValue("baseline_completed_at"); got != preset {
		t.Errorf("baseline overwritten: %q != %q", got, preset)
	}
}

func TestMarkFailed_DoesNotCloseBaseline(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	taskID := newScanTaskRow(t, db)
	repo := NewScanTaskRepository(db)
	if err := repo.MarkFailed(taskID, "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	cfg := NewSystemConfigRepository(db)
	if got := cfg.GetValue("baseline_completed_at"); got != "" {
		t.Errorf("baseline_completed_at = %q, want empty after fail", got)
	}
}
