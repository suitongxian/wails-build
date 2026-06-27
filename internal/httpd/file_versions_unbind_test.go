package httpd

import (
	"fmt"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// V5-P1 Task 7 解绑端点
//
// 验证：调用成功后 fv lifecycle_status=cancelled；二次解绑必失败。
func TestHTTP_FileVersion_Unbind(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedPersonalProjectsForAI(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	resID := seedSimpleResourceWithDist(t, db, "解绑.pdf", "UBHT001", "/")
	// importance_level=2 重要 → 可桥接到内置项目
	if _, err := db.Exec(`UPDATE data_resources SET importance_level = 2 WHERE data_resources_id = ?`, resID); err != nil {
		t.Fatal(err)
	}
	fvID, err := repository.BridgeClassifyToPersonalProject(db, resID)
	if err != nil || fvID == 0 {
		t.Fatalf("bridge failed: fvID=%d err=%v", fvID, err)
	}

	// V5-P1 Task 7 fix: 解绑端点已加 RequireFileVersionProjectAction("write")，
	// 需要先给 u1 在该 fv 所属项目里登记 write 权限。
	var projectIDForFv int64
	if err := db.Get(&projectIDForFv, `SELECT project_id FROM file_versions WHERE id = ?`, fvID); err != nil {
		t.Fatalf("查 fv project_id: %v", err)
	}
	seedUserAndProjectMember(t, db, "u1", projectIDForFv, []string{"read", "write"})

	status, resp := jsonReq(t, r, "POST", fmt.Sprintf("/file-versions/%d/unbind", fvID), map[string]interface{}{
		"reason": "测试解绑",
	})
	successOk(t, status, resp)

	var lc string
	if err := db.Get(&lc, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if lc != "cancelled" {
		t.Errorf("应 cancelled, got %s", lc)
	}

	// 二次解绑应失败
	status, resp = jsonReq(t, r, "POST", fmt.Sprintf("/file-versions/%d/unbind", fvID), map[string]interface{}{
		"reason": "再来一次",
	})
	expectFailure(t, status, resp)
}

// V5-P1 Task 7 解绑端点 — reason 为空必失败（业务层 + HTTP 层双重防御）
func TestHTTP_FileVersion_Unbind_MissingReason(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	status, resp := jsonReq(t, r, "POST", "/file-versions/1/unbind", map[string]interface{}{
		"reason": "",
	})
	expectFailure(t, status, resp)
}

// V5-P1 Task 7 解绑端点 — id 非法必失败
func TestHTTP_FileVersion_Unbind_InvalidID(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	status, resp := jsonReq(t, r, "POST", "/file-versions/0/unbind", map[string]interface{}{
		"reason": "test",
	})
	expectFailure(t, status, resp)
}

// V5-P1 Task 7 重新归类端点
//
// importance=1 核心桥接 → 重归到一般级内置项目（同一 V1.0 模版下不同的 project_code）
// 验证：原 fv cancelled、新 fv registered、id 不同。
func TestHTTP_FileVersion_Reclassify(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedPersonalProjectsForAI(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	resID := seedSimpleResourceWithDist(t, db, "重归测试.pdf", "RCHT001", "/")
	// importance=1（核心）桥接
	if _, err := db.Exec(`UPDATE data_resources SET importance_level = 1 WHERE data_resources_id = ?`, resID); err != nil {
		t.Fatal(err)
	}
	fvID, err := repository.BridgeClassifyToPersonalProject(db, resID)
	if err != nil || fvID == 0 {
		t.Fatalf("bridge failed: %v", err)
	}

	// V5-P1 Task 7 fix: 重新归类端点已加 RequireFileVersionProjectAction("write")，
	// 需要先给 u1 在原 fv 所属项目（importance=1 → CORE）里登记 write 权限。
	var origProjectID int64
	if err := db.Get(&origProjectID, `SELECT project_id FROM file_versions WHERE id = ?`, fvID); err != nil {
		t.Fatalf("查 fv project_id: %v", err)
	}
	seedUserAndProjectMember(t, db, "u1", origProjectID, []string{"read", "write"})

	var generalID int64
	if err := db.Get(&generalID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalGeneralProjectCode); err != nil {
		t.Fatalf("查一般级项目: %v", err)
	}

	// 不假设 V1.0 vs V2.0，从 DB 反查 stage_code + file_rule_code
	var stageCode, ruleCode string
	if err := db.Get(&stageCode, `SELECT stage_code FROM project_stages WHERE project_id = ? AND disable = 0 LIMIT 1`, generalID); err != nil {
		t.Fatalf("查 stage: %v", err)
	}
	if err := db.Get(&ruleCode, `SELECT tfr.file_rule_code FROM template_file_rules tfr
		JOIN template_stages ts ON ts.id = tfr.template_stage_id
		JOIN project_stages ps ON ps.template_stage_id = ts.id
		WHERE ps.project_id = ? AND tfr.disable = 0 LIMIT 1`, generalID); err != nil {
		t.Fatalf("查 rule: %v", err)
	}

	status, resp := jsonReq(t, r, "POST", fmt.Sprintf("/file-versions/%d/reclassify", fvID), map[string]interface{}{
		"new_project_id":     generalID,
		"new_stage_code":     stageCode,
		"new_file_rule_code": ruleCode,
		"reason":             "应一般级",
	})
	successOk(t, status, resp)

	d := dataMap(t, resp)
	newFvIDFloat, ok := d["new_fv_id"].(float64)
	if !ok {
		t.Fatalf("缺 new_fv_id: %+v", d)
	}
	newFvID := int64(newFvIDFloat)
	if newFvID == fvID {
		t.Error("new fv id 应不同")
	}

	var orig, newSt string
	db.Get(&orig, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, fvID)
	db.Get(&newSt, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, newFvID)
	if orig != "cancelled" {
		t.Errorf("orig 应 cancelled, got %s", orig)
	}
	if newSt != "registered" {
		t.Errorf("new 应 registered, got %s", newSt)
	}
}

// V5-P1 Task 7 重新归类端点 — 必填字段缺失必失败
func TestHTTP_FileVersion_Reclassify_MissingFields(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	status, resp := jsonReq(t, r, "POST", "/file-versions/1/reclassify", map[string]interface{}{
		"reason": "no target",
	})
	expectFailure(t, status, resp)
}
