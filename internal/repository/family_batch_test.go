package repository

import (
	"testing"
	"time"
)

func TestFamilyRepository_BatchListByContentSigns(t *testing.T) {
	db := openTestDB(t)
	repo := NewFamilyRepository(db)

	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id, family_relation,
		create_time, update_time, disable, data_origin
	) VALUES
		('CS_F1_P',  1, 0, ?, 'fam1-primary.pdf',       0, 0, 1, 'primary',          ?, ?, 0, 'historical'),
		('CS_F1_M1', 1, 0, ?, 'fam1-mem1.pdf',           0, 0, 1, 'same_content',     ?, ?, 0, 'historical'),
		('CS_F1_M2', 1, 0, ?, 'fam1-mem2.pdf',           0, 0, 1, 'process_version',  ?, ?, 0, 'historical'),
		('CS_F2_P',  1, 0, ?, 'fam2-primary.docx',       0, 0, 2, 'primary',          ?, ?, 0, 'historical'),
		('CS_F2_M1', 1, 0, ?, 'fam2-mem1.docx',          0, 0, 2, 'derived',          ?, ?, 0, 'historical')`,
		now, now, now,
		now, now, now,
		now, now, now,
		now, now, now,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.BatchListFamilyMembersByContentSigns([]string{"CS_F1_P", "CS_F2_P"})
	if err != nil {
		t.Fatalf("batch list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d families, want 2", len(got))
	}
	if len(got["CS_F1_P"]) != 3 {
		t.Errorf("F1 members = %d, want 3", len(got["CS_F1_P"]))
	}
	if len(got["CS_F2_P"]) != 2 {
		t.Errorf("F2 members = %d, want 2", len(got["CS_F2_P"]))
	}
}

func TestFamilyRepository_BatchListByContentSigns_NoFamily(t *testing.T) {
	db := openTestDB(t)
	repo := NewFamilyRepository(db)

	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('CS_SOLO', 1, 0, ?, 'solo.pdf', 0, 0, NULL, ?, ?, 0, 'historical')`,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.BatchListFamilyMembersByContentSigns([]string{"CS_SOLO"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["CS_SOLO"]; ok {
		t.Errorf("solo content_sign should not appear in result map (no family)")
	}
}

func TestFamilyRepository_BatchListByContentSigns_EmptyInput(t *testing.T) {
	db := openTestDB(t)
	repo := NewFamilyRepository(db)
	got, err := repo.BatchListFamilyMembersByContentSigns([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty input should return empty map, got %d entries", len(got))
	}
}
