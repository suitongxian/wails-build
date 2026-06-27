package repository

import (
	"testing"
	"time"
)

// TestListFamilyMembers_ExpandsPhysicalCopies verifies that ListFamilyMembers
// returns one row per data_distributing entry, not one per data_resources row.
// Two physical copies sharing the same content_sign must appear as two separate
// rows, each with its own Path and IP.
func TestListFamilyMembers_ExpandsPhysicalCopies(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	insertRes := func(cs string) {
		_, err := db.Exec(`INSERT INTO data_resources
			(content_sign, source_count, workspace_source_count, first_create_time, create_time, update_time)
			VALUES (?, 2, 0, ?, ?, ?)`, cs, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertDist := func(cs, path, ip string) {
		_, err := db.Exec(`INSERT INTO data_distributing
			(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time, create_time, update_time)
			VALUES (?, 1, 1, ?, 100, ?, 'AA:BB', ?, ?, ?)`,
			path, cs, ip, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	// CS-A has 2 physical copies; CS-B has 1.
	insertRes("CS-A")
	insertRes("CS-B")
	insertDist("CS-A", "/data/a1.pdf", "10.0.0.1")
	insertDist("CS-A", "/data/a2.pdf", "10.0.0.2")
	insertDist("CS-B", "/data/b1.pdf", "10.0.0.3")

	famRepo := NewFamilyRepository(db)
	famID, err := famRepo.InsertFamilyWithMembers(FamilyInsert{
		PrimaryContentSign: "CS-A",
		Algorithm:          "tfidf",
		HighestScore:       0.91,
		Members: []FamilyMemberAssignment{
			{ContentSign: "CS-A", Relation: "primary", Score: 1.0, IsPrimary: true},
			{ContentSign: "CS-B", Relation: "same_content", Score: 0.97},
		},
	})
	if err != nil {
		t.Fatalf("InsertFamilyWithMembers: %v", err)
	}

	members, err := famRepo.ListFamilyMembers(famID)
	if err != nil {
		t.Fatalf("ListFamilyMembers: %v", err)
	}

	// Expect 3 rows: 2 for CS-A, 1 for CS-B.
	if len(members) != 3 {
		t.Fatalf("expected 3 members (expanded physical copies), got %d", len(members))
	}

	paths := map[string]bool{}
	for _, m := range members {
		if m.Path == nil {
			t.Error("expected Path to be set, got nil")
			continue
		}
		paths[*m.Path] = true
	}
	for _, want := range []string{"/data/a1.pdf", "/data/a2.pdf", "/data/b1.pdf"} {
		if !paths[want] {
			t.Errorf("missing path %q in members", want)
		}
	}
}

// TestFamily_InsertAndIDsInFamily seeds two data_resources rows, inserts a
// family that links them, and verifies IDsInFamily returns both.
func TestFamily_InsertAndIDsInFamily(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	insertRes := func(cs string) int64 {
		res, err := db.Exec(`INSERT INTO data_resources
			(content_sign, source_count, workspace_source_count, first_create_time, create_time, update_time)
			VALUES (?, 1, 0, ?, ?, ?)`, cs, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	idA := insertRes("CS-A")
	idB := insertRes("CS-B")

	famRepo := NewFamilyRepository(db)
	famID, err := famRepo.InsertFamilyWithMembers(FamilyInsert{
		PrimaryContentSign: "CS-A",
		Algorithm:          "tfidf",
		HighestScore:       0.91,
		Members: []FamilyMemberAssignment{
			{ContentSign: "CS-A", Relation: "primary", Score: 1.0, IsPrimary: true},
			{ContentSign: "CS-B", Relation: "process_version", Score: 0.83},
		},
	})
	if err != nil {
		t.Fatalf("InsertFamilyWithMembers: %v", err)
	}
	if famID == 0 {
		t.Fatal("expected non-zero family id")
	}

	ids, err := famRepo.IDsInFamily(famID)
	if err != nil {
		t.Fatalf("IDsInFamily: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids in family, got %d", len(ids))
	}
	got := map[int64]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[idA] || !got[idB] {
		t.Errorf("missing id: got=%v want %d,%d", ids, idA, idB)
	}

	// Verify family_relation roundtrip
	var rel string
	if err := db.Get(&rel, `SELECT family_relation FROM data_resources WHERE data_resources_id=?`, idA); err != nil {
		t.Fatal(err)
	}
	if rel != "primary" {
		t.Errorf("primary row relation: got %q want primary", rel)
	}
}

// TestFamily_ResetClearsAssignments verifies ResetFamilies wipes both the
// family table and the family_* columns on data_resources.
func TestFamily_ResetClearsAssignments(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	res, err := db.Exec(`INSERT INTO data_resources
		(content_sign, source_count, workspace_source_count, first_create_time, family_id, family_relation, family_score, create_time, update_time)
		VALUES ('X', 1, 0, ?, 99, 'primary', 1.0, ?, ?)`, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := res.LastInsertId()

	if _, err := db.Exec(`INSERT INTO data_resource_family
		(primary_content_sign, member_count, algorithm, highest_score, create_time, update_time)
		VALUES ('X', 1, 'tfidf', 1.0, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	famRepo := NewFamilyRepository(db)
	if err := famRepo.ResetFamilies(); err != nil {
		t.Fatalf("ResetFamilies: %v", err)
	}

	var famID *int64
	if err := db.Get(&famID, `SELECT family_id FROM data_resources WHERE data_resources_id=?`, rid); err != nil {
		t.Fatal(err)
	}
	if famID != nil {
		t.Errorf("expected family_id=NULL after reset, got %v", *famID)
	}

	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM data_resource_family`); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected family table empty after reset, got %d rows", n)
	}
}
