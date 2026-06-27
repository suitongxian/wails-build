package httpd

import (
	"testing"
	"time"
)

func TestHTTP_Family_SetAuthoritative(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()

	famR, _ := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		create_time, update_time, disable
	) VALUES ('AUTHCS', 0, 2, 'sim', 0.9, ?, ?, 0)`, now, now)
	famID, _ := famR.LastInsertId()

	resR, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('AUTHRES', 1, 1, ?, 'a.pdf', 2, 0, ?, ?, ?, 0, 'new')`, now, famID, now, now)
	resID, _ := resR.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/family/"+itoa(famID)+"/authoritative", map[string]interface{}{
		"resource_id": resID,
	})
	successOk(t, status, resp)

	var got int64
	db.Get(&got, `SELECT authoritative_resource_id FROM data_resource_family WHERE family_id = ?`, famID)
	if got != resID {
		t.Errorf("authoritative_resource_id = %d, want %d", got, resID)
	}
}
