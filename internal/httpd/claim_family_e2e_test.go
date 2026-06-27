package httpd

import (
	"testing"
	"time"
)

// End-to-end: batch claim with family members.
// Verifies the contract between frontend dialog flow and existing BatchClaim API:
// frontend computes IDs (primary + same_content), backend updates only those rows.
func TestE2E_BatchClaimWithFamilyMembers(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	// Seed: family with primary + same_content + derived; plus a solo resource
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id, family_relation,
		create_time, update_time, disable, data_origin
	) VALUES
		('CS_F_P',  1, 0, ?, 'fam-p.docx', 0, 0, 100, 'primary',      ?, ?, 0, 'historical'),
		('CS_F_M1', 1, 0, ?, 'fam-m1.docx',0, 0, 100, 'same_content', ?, ?, 0, 'historical'),
		('CS_F_M2', 1, 0, ?, 'fam-m2.docx',0, 0, 100, 'derived',      ?, ?, 0, 'historical'),
		('CS_SOLO', 1, 0, ?, 'solo.docx',  0, 0, NULL, NULL,           ?, ?, 0, 'historical')`,
		now, now, now,
		now, now, now,
		now, now, now,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: preview (sanity check)
	s1, b1 := jsonReqNoBody(t, r, "POST", "/similarity/analyze/preview")
	successOk(t, s1, b1)

	// Step 2: batch members — frontend resolves family membership
	s2, b2 := jsonReq(t, r, "POST", "/family/batch-members", map[string]interface{}{
		"content_signs": []string{"CS_F_P", "CS_SOLO"},
	})
	successOk(t, s2, b2)

	data, ok := b2["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response.data is not a map: %+v", b2)
	}
	fam, ok := data["CS_F_P"].([]interface{})
	if !ok {
		t.Fatalf("CS_F_P not in data map: %+v", data)
	}
	if len(fam) != 3 {
		t.Errorf("CS_F_P members = %d, want 3", len(fam))
	}
	if _, ok := data["CS_SOLO"]; ok {
		t.Errorf("solo should not appear in family-batch result")
	}

	// Step 3: frontend computed IDs = primary (CS_F_P) + same_content (CS_F_M1)
	// (derived CS_F_M2 excluded per default same_content_only policy)
	var rows []struct {
		ID int64  `db:"data_resources_id"`
		CS string `db:"content_sign"`
	}
	if err := db.Select(&rows,
		`SELECT data_resources_id, content_sign FROM data_resources
		 WHERE content_sign IN ('CS_F_P','CS_F_M1') ORDER BY content_sign`); err != nil {
		t.Fatalf("query ids: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("seed broken: expected 2 rows, got %d", len(rows))
	}

	// rows sorted by content_sign: CS_F_M1 < CS_F_P
	claimBody := map[string]interface{}{
		"ids":           []int64{rows[0].ID, rows[1].ID},
		"is_claimed":    1,
		"claim_status":  2,
		"claimant_name": "E2E",
		"claimant_unit": "Test",
	}
	s3, b3 := jsonReq(t, r, "POST", "/resources/claim", claimBody)
	successOk(t, s3, b3)

	// Step 4: verify CS_F_P and CS_F_M1 claim_status = 2; CS_F_M2 (derived) still 0
	type result struct {
		CS     string `db:"content_sign"`
		Status int    `db:"claim_status"`
	}
	var got []result
	if err := db.Select(&got,
		`SELECT content_sign, claim_status FROM data_resources
		 WHERE content_sign IN ('CS_F_P','CS_F_M1','CS_F_M2') ORDER BY content_sign`); err != nil {
		t.Fatalf("verify query: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("verify count = %d, want 3", len(got))
	}
	expected := map[string]int{"CS_F_M1": 2, "CS_F_M2": 0, "CS_F_P": 2}
	for _, g := range got {
		if expected[g.CS] != g.Status {
			t.Errorf("%s claim_status = %d, want %d", g.CS, g.Status, expected[g.CS])
		}
	}
}

// Verify the "无家族" bypass path: frontend skips family lookup entirely
// (this is the fast-path that doesn't even call batch-members API).
func TestE2E_ClaimResourceWithoutFamily(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('CS_LONE', 1, 0, ?, 'lone.docx', 0, 0, NULL, ?, ?, 0, 'historical')`,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	// Frontend would skip batch-members for no-family rows and call claim directly.
	var id int64
	if err := db.Get(&id, `SELECT data_resources_id FROM data_resources WHERE content_sign='CS_LONE'`); err != nil {
		t.Fatalf("get id: %v", err)
	}

	claimBody := map[string]interface{}{
		"ids":           []int64{id},
		"is_claimed":    1,
		"claim_status":  1,
		"claimant_name": "E2E",
		"claimant_unit": "T",
	}
	s, b := jsonReq(t, r, "POST", "/resources/claim", claimBody)
	successOk(t, s, b)

	var status int
	if err := db.Get(&status, `SELECT claim_status FROM data_resources WHERE content_sign='CS_LONE'`); err != nil {
		t.Fatalf("get status: %v", err)
	}
	if status != 1 {
		t.Errorf("CS_LONE claim_status = %d, want 1", status)
	}
}
