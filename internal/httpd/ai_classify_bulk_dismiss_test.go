package httpd

import (
	"testing"
	"time"
)

func TestHTTP_AIClassify_BulkDismiss_DismissesHistorical(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	var ids []int64
	for i := 0; i < 3; i++ {
		res, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, 0, 'historical')`,
			"BD_"+itoa(int64(i)), now, "h.pdf", now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}

	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": ids,
		"reason":       "首次普查存量，无需细分",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if v, _ := d["dismissed"].(float64); int(v) != 3 {
		t.Errorf("dismissed = %v, want 3", v)
	}

	var stillPending int
	db.Get(&stillPending,
		`SELECT COUNT(*) FROM data_resources
		  WHERE data_origin = 'historical' AND ai_classify_rejected_at IS NULL AND disable = 0`)
	if stillPending != 0 {
		t.Errorf("still pending after dismiss = %d, want 0", stillPending)
	}

	var auditCount int
	db.Get(&auditCount,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'ai_classify_reject' AND target_type = 'data_resource'`)
	if auditCount != 3 {
		t.Errorf("audit rows = %d, want 3", auditCount)
	}
}

func TestHTTP_AIClassify_BulkDismiss_RejectsNonHistorical(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('BD_NEW', 1, 1, ?, 'new.pdf', 2, 0, ?, ?, 0, 'new')`, now, now, now)
	newID, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": []int64{newID},
		"reason":       "should-be-rejected",
	})
	expectFailure(t, status, resp)
	if resp["error"] == nil {
		t.Error("error message missing on validation failure")
	}
}

func TestHTTP_AIClassify_BulkDismiss_RejectsEmpty(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()
	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": []int64{},
		"reason":       "x",
	})
	expectFailure(t, status, resp)
}

func TestHTTP_AIClassify_BulkDismiss_AllowsNewGeneral(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('BDNG_1', 1, 1, ?, 'general-new.pdf', 2, 3, ?, ?, 0, 'new')`,
		now, now, now)
	rid, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": []int64{rid},
		"reason":       "AI 没把握，跳过",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if v, _ := d["dismissed"].(float64); int(v) != 1 {
		t.Errorf("dismissed = %v, want 1", v)
	}
}

func TestHTTP_AIClassify_BulkDismiss_RejectsNewImportant(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('BDNI_1', 1, 1, ?, 'important-new.pdf', 2, 2, ?, ?, 0, 'new')`,
		now, now, now)
	rid, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/ai/classify/bulk-dismiss", map[string]interface{}{
		"resource_ids": []int64{rid},
		"reason":       "skip",
	})
	expectFailure(t, status, resp)
}
