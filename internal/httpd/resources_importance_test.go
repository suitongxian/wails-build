package httpd

import (
	"testing"
	"time"
)

func TestHTTP_Resources_OverrideImportance(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('OVI_001', 1, 1, ?, 'x.pdf', 2, 0, ?, ?, 0, 'new')`, now, now, now)
	rid, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/resources/"+itoa(rid)+"/importance", map[string]interface{}{
		"level": 1,
	})
	successOk(t, status, resp)

	var got int
	db.Get(&got, `SELECT importance_level FROM data_resources WHERE data_resources_id = ?`, rid)
	if got != 1 {
		t.Errorf("importance_level = %d, want 1", got)
	}

	// 撤回到未分类
	status, _ = jsonReq(t, r, "POST", "/resources/"+itoa(rid)+"/importance", map[string]interface{}{"level": 0})
	if status != 200 {
		t.Errorf("revert status = %d, want 200", status)
	}
	db.Get(&got, `SELECT importance_level FROM data_resources WHERE data_resources_id = ?`, rid)
	if got != 0 {
		t.Errorf("after revert importance_level = %d, want 0", got)
	}
}

func TestHTTP_Resources_OverrideImportance_RejectsBadLevel(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()
	status, resp := jsonReq(t, r, "POST", "/resources/1/importance", map[string]interface{}{"level": 7})
	expectFailure(t, status, resp)
}
