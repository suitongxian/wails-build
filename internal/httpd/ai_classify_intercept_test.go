package httpd

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_AIClassify_Apply_BlocksOnImportantUnsettledFamily(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	repository.EnsurePersonalContextForTest(db)
	withActiveUser(t, db, "u1")
	now := time.Now()

	// 建一个 family 含 2 个成员，未指定权威
	famR, _ := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		create_time, update_time, disable
	) VALUES ('INTCS', 0, 2, 'sim', 0.9, ?, ?, 0)`, now, now)
	famID, _ := famR.LastInsertId()

	resR, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, family_id,
		create_time, update_time, disable, data_origin
	) VALUES ('INT_R1', 1, 1, ?, 'rep.pdf', 2, 0, ?, ?, ?, 0, 'new')`, now, famID, now, now)
	resID, _ := resR.LastInsertId()

	// 找 SYS-PERSONAL-IMPORTANT 项目 id
	var projID int64
	db.Get(&projID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalImportantProjectCode)

	status, resp := jsonReq(t, r, "POST", "/ai/classify/apply", map[string]interface{}{
		"resource_id":    resID,
		"project_id":     projID,
		"stage_code":     "GR-DA",
		"file_rule_code": "OUT-001",
	})
	if status != 409 {
		t.Fatalf("status = %d, want 409 (need to choose authoritative); body=%+v", status, resp)
	}
	d, _ := resp["data"].(map[string]interface{})
	if d == nil || d["family_id"] == nil {
		t.Errorf("response.data should contain family_id; got %+v", resp)
	}
}
