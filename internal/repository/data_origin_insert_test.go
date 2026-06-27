package repository

import (
	"testing"
	"time"
)

func TestInsertBatch_TagsHistoricalBeforeBaseline(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	repo := NewDataResourcesRepository(db, 100)

	now := time.Now()
	n := repo.InsertBatch([]map[string]interface{}{{
		"content_sign":           "CSH001",
		"source_count":           int64(1),
		"workspace_source_count": int64(1),
		"first_create_time":      now.Format(time.RFC3339),
		"resources_name":         "file-pre-baseline.pdf",
		"content_subject":        "file",
		"content_type":           "pdf",
	}})
	if n != 1 {
		t.Fatalf("insert count = %d, want 1", n)
	}
	var origin string
	if err := db.Get(&origin, `SELECT data_origin FROM data_resources WHERE content_sign = 'CSH001'`); err != nil {
		t.Fatalf("read: %v", err)
	}
	if origin != "historical" {
		t.Errorf("origin = %q, want historical", origin)
	}
}

func TestInsertBatch_TagsNewAfterBaseline(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	cfg := NewSystemConfigRepository(db)
	cfg.SetValue("baseline_completed_at", time.Now().Format(time.RFC3339))

	repo := NewDataResourcesRepository(db, 100)
	now := time.Now()
	repo.InsertBatch([]map[string]interface{}{{
		"content_sign":           "CSN001",
		"source_count":           int64(1),
		"workspace_source_count": int64(1),
		"first_create_time":      now.Format(time.RFC3339),
		"resources_name":         "file-post-baseline.pdf",
		"content_subject":        "file",
		"content_type":           "pdf",
	}})
	var origin string
	if err := db.Get(&origin, `SELECT data_origin FROM data_resources WHERE content_sign = 'CSN001'`); err != nil {
		t.Fatalf("read: %v", err)
	}
	if origin != "new" {
		t.Errorf("origin = %q, want new", origin)
	}
}

func TestInsertFromStatistics_RespectsBaseline(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	repo := NewDataResourcesRepository(db, 100)
	now := time.Now().Format(time.RFC3339)
	stats := map[string]interface{}{
		"CSS001": &MD5Stats{
			ContentSign:     "CSS001",
			SourceCount:     1,
			FirstCreateTime: now,
			ShortFileName:   "stats-pre.pdf",
			FirstFileName:   "stats-pre.pdf",
		},
	}
	repo.InsertFromStatistics(stats)
	var origin string
	if err := db.Get(&origin, `SELECT data_origin FROM data_resources WHERE content_sign = 'CSS001'`); err != nil {
		t.Fatalf("read: %v", err)
	}
	if origin != "historical" {
		t.Errorf("pre-baseline stats origin = %q, want historical", origin)
	}

	cfg := NewSystemConfigRepository(db)
	cfg.SetValue("baseline_completed_at", now)
	stats2 := map[string]interface{}{
		"CSS002": &MD5Stats{
			ContentSign:     "CSS002",
			SourceCount:     1,
			FirstCreateTime: now,
			ShortFileName:   "stats-post.pdf",
			FirstFileName:   "stats-post.pdf",
		},
	}
	repo.InsertFromStatistics(stats2)
	if err := db.Get(&origin, `SELECT data_origin FROM data_resources WHERE content_sign = 'CSS002'`); err != nil {
		t.Fatalf("read CSS002: %v", err)
	}
	if origin != "new" {
		t.Errorf("post-baseline stats origin = %q, want new", origin)
	}
}
