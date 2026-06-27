package httpd

import (
	"testing"
	"time"
)

func TestHTTP_Family_NeedsArbitrationCount(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()

	// family A: 2 成员、未确权、有重要级成员 → 计 1
	famAR, _ := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		create_time, update_time, disable
	) VALUES ('NAA', 0, 2, 'sim', 0.9, ?, ?, 0)`, now, now)
	famA, _ := famAR.LastInsertId()
	db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('NAA_R', 1, 1, ?, 'x.pdf', 2, 2, ?, ?, ?, 0, 'new')`, now, famA, now, now)

	// family B: 2 成员、已确权 → 不计
	famBR, _ := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		authoritative_resource_id,
		create_time, update_time, disable
	) VALUES ('NAB', 0, 2, 'sim', 0.9, 1, ?, ?, 0)`, now, now)
	famB, _ := famBR.LastInsertId()
	db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('NAB_R', 1, 1, ?, 'y.pdf', 2, 2, ?, ?, ?, 0, 'new')`, now, famB, now, now)

	// family C: 2 成员、未确权、但成员全是非重要级 → 不计
	famCR, _ := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		create_time, update_time, disable
	) VALUES ('NAC', 0, 2, 'sim', 0.9, ?, ?, 0)`, now, now)
	famC, _ := famCR.LastInsertId()
	db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('NAC_R', 1, 1, ?, 'z.pdf', 2, 3, ?, ?, ?, 0, 'new')`, now, famC, now, now)

	status, resp := jsonReqNoBody(t, r, "GET", "/family/needs-arbitration")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	got, _ := d["count"].(float64)
	if int(got) != 1 {
		t.Errorf("count = %v, want 1", got)
	}
}
