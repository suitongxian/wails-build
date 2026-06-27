package httpd

import (
	"net/http"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// V2-7: 过户 happy path — owner 配 share 权限，可过户保管主体
func TestHTTP_Ledgers_HandoverSuccess(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	tplCode, tplVer := seedTestTemplateForHTTP(t, db)
	owner, custodian, security := seedTestSubjectsForHTTP(t, db)
	withProjectRoot(t, db)

	svc := repository.NewProjectInstantiationService(db)
	out, err := svc.Instantiate(repository.InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "过户成功测试",
		ObjectShortCode:    "HT-HV-OK",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []repository.MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "receive", "submit", "share", "archive", "close"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	withActiveUser(t, db, "OWNER-1")

	// 先上传一个 IN-001，让对应 ledger 进入 registered
	fvID := findFvIDByLocal(t, out.Stages, "MZ-SG", "IN-001")
	_, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)
	d := dataMap(t, resp)
	lid := int64(d["ledger"].(map[string]interface{})["id"].(float64))

	// 建一个新 subject 当过户目标
	now := "now"
	res, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('NEW-CUST', '新保管方', 'department', 'active', ?, ?, 0)`, now, now)
	newSubID, _ := res.LastInsertId()

	// 调用过户接口
	status, resp := jsonReq(t, r, "POST", "/ledgers/"+itoa(lid)+"/handover", map[string]interface{}{
		"subject_kind":  "custodian",
		"to_subject_id": newSubID,
		"reason":        "保管方变更",
		"approval_ref":  "OA-2026-001",
	})
	successOk(t, status, resp)

	// 验证返回的 ledger 已经更新了 custodian_subject_id
	updated := dataMap(t, resp)
	if int64(updated["custodian_subject_id"].(float64)) != newSubID {
		t.Errorf("custodian_subject_id 应为 %d, got %v", newSubID, updated["custodian_subject_id"])
	}

	// 应当生成一条 handover 事件
	var evCount int
	db.Get(&evCount, `SELECT COUNT(*) FROM lifecycle_events WHERE ledger_id = ? AND event_type = 'handover'`, lid)
	if evCount != 1 {
		t.Errorf("应当生成 1 条 handover 事件, got %d", evCount)
	}
}

// V2-7: subject_kind 非法 → 失败（前置：操作人有 share 权限通过中间件）
func TestHTTP_Ledgers_HandoverInvalidKind(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	tplCode, tplVer := seedTestTemplateForHTTP(t, db)
	owner, custodian, security := seedTestSubjectsForHTTP(t, db)
	withProjectRoot(t, db)

	svc := repository.NewProjectInstantiationService(db)
	out, err := svc.Instantiate(repository.InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "subject_kind 校验",
		ObjectShortCode:    "HT-HV-IK",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []repository.MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "share", "close"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	withActiveUser(t, db, "OWNER-1")

	fvID := findFvIDByLocal(t, out.Stages, "MZ-SG", "IN-001")
	_, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)
	d := dataMap(t, resp)
	lid := int64(d["ledger"].(map[string]interface{})["id"].(float64))

	status, resp := jsonReq(t, r, "POST", "/ledgers/"+itoa(lid)+"/handover", map[string]interface{}{
		"subject_kind":  "unknown",
		"to_subject_id": 1,
	})
	expectFailure(t, status, resp)
}

// V2-7: 无 share 权限 → 403
func TestHTTP_Ledgers_HandoverNeedSharePermission(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// 立项时配一个只有 read+write+close 的人（缺 share）
	tplCode, tplVer := seedTestTemplateForHTTP(t, db)
	owner, custodian, security := seedTestSubjectsForHTTP(t, db)
	now := "now"
	res, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('NO-SHARE', '无 share 权限', 'person', 'active', ?, ?, 0)`, now, now)
	noShareSubID, _ := res.LastInsertId()
	withProjectRoot(t, db)

	svc := repository.NewProjectInstantiationService(db)
	out, err := svc.Instantiate(repository.InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "过户权限测试",
		ObjectShortCode:    "HT-HV",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []repository.MemberInput{
			{SubjectID: noShareSubID, RoleCode: "EDITOR", PermissionActions: []string{"read", "write", "submit"}}, // 无 share
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "receive", "submit", "close"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	withActiveUser(t, db, "NO-SHARE")

	fvID := findFvIDByLocal(t, out.Stages, "MZ-SG", "IN-001")
	_, uploadResp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)
	d := dataMap(t, uploadResp)
	lid := int64(d["ledger"].(map[string]interface{})["id"].(float64))

	res2, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('TGT', 'TGT', 'department', 'active', ?, ?, 0)`, now, now)
	newSubID, _ := res2.LastInsertId()

	w := httpPost(t, r, "/ledgers/"+itoa(lid)+"/handover", map[string]interface{}{
		"subject_kind":  "custodian",
		"to_subject_id": newSubID,
		"reason":        "试图过户",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("NO-SHARE 应被拒 403, got %d body=%s", w.Code, w.Body.String())
	}
}
