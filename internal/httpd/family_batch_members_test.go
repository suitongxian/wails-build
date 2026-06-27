package httpd

import (
	"testing"
	"time"
)

func TestHTTP_FamilyBatchMembers(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id, family_relation,
		create_time, update_time, disable, data_origin
	) VALUES
		('CS_P1', 1, 0, ?, 'p1.pdf', 0, 0, 10, 'primary',      ?, ?, 0, 'historical'),
		('CS_M1', 1, 0, ?, 'm1.pdf', 0, 0, 10, 'same_content', ?, ?, 0, 'historical'),
		('CS_P2', 1, 0, ?, 'p2.pdf', 0, 0, 20, 'primary',      ?, ?, 0, 'historical')`,
		now, now, now,
		now, now, now,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReq(t, r, "POST", "/family/batch-members", map[string]interface{}{
		"content_signs": []string{"CS_P1", "CS_P2"},
	})
	successOk(t, status, resp)

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response.data is not a map: %+v", resp)
	}
	cs1, ok := data["CS_P1"].([]interface{})
	if !ok {
		t.Fatalf("CS_P1 not found or not a list in data: %+v", data)
	}
	if len(cs1) != 2 {
		t.Errorf("CS_P1 members = %d, want 2", len(cs1))
	}
	cs2, ok := data["CS_P2"].([]interface{})
	if !ok {
		t.Fatalf("CS_P2 not found or not a list in data: %+v", data)
	}
	if len(cs2) != 1 {
		t.Errorf("CS_P2 members = %d, want 1", len(cs2))
	}
}

func TestHTTP_FamilyBatchMembers_EmptyInput(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	status, resp := jsonReq(t, r, "POST", "/family/batch-members", map[string]interface{}{
		"content_signs": []string{},
	})
	if status != 200 {
		t.Errorf("status = %d, want 200; body=%+v", status, resp)
		return
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response.data is not a map: %+v", resp)
	}
	if len(data) != 0 {
		t.Errorf("expected empty data map, got %d entries", len(data))
	}
}
