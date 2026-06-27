package httpd

import (
	"data-asset-scan-go/internal/repository"
	"net/http"
	"testing"
)

// V1验证-1.30 底账查询 + 详情 + 事件
func TestHTTP_Ledgers_SearchAndDetail(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	// 上传一个文件让底账升至 registered
	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")
	uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)

	// 查询全部
	status, resp := jsonReqNoBody(t, r, "GET", "/ledgers")
	successOk(t, status, resp)
	all := dataList(t, resp)
	if len(all) == 0 {
		t.Fatal("expected at least 1 ledger")
	}

	// 按项目筛
	status, resp = jsonReqNoBody(t, r, "GET", "/ledgers?project_code="+pj.ProjectCode)
	successOk(t, status, resp)
	if len(dataList(t, resp)) == 0 {
		t.Error("filter by project_code should match")
	}

	// 按状态筛
	status, resp = jsonReqNoBody(t, r, "GET", "/ledgers?lifecycle_status=registered")
	successOk(t, status, resp)
	for _, l := range dataList(t, resp) {
		if l.(map[string]interface{})["lifecycle_status"] != "registered" {
			t.Error("filter mismatch")
		}
	}

	// keyword
	status, resp = jsonReqNoBody(t, r, "GET", "/ledgers?keyword=客户原稿")
	successOk(t, status, resp)
	if len(dataList(t, resp)) == 0 {
		t.Error("keyword search should match asset_name")
	}

	// 取一个 ledger id
	first := dataList(t, resp)[0].(map[string]interface{})
	lid := int64(first["id"].(float64))

	// 详情
	status, resp = jsonReqNoBody(t, r, "GET", "/ledgers/"+itoa(lid))
	successOk(t, status, resp)

	// 事件
	status, resp = jsonReqNoBody(t, r, "GET", "/ledgers/"+itoa(lid)+"/events")
	successOk(t, status, resp)
	if len(dataList(t, resp)) == 0 {
		t.Error("expected register event")
	}
}

// V1验证-1.31 状态切换合法路径
func TestHTTP_Ledgers_TransitionLegal(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")
	_, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)
	d := dataMap(t, resp)
	ledger := d["ledger"].(map[string]interface{})
	lid := int64(ledger["id"].(float64))

	// registered → in_use 合法
	status, resp := jsonReq(t, r, "POST", "/ledgers/"+itoa(lid)+"/transition", map[string]interface{}{
		"to_status": "in_use", "reason": "投入工作",
	})
	successOk(t, status, resp)

	// in_use → sealed 合法
	status, resp = jsonReq(t, r, "POST", "/ledgers/"+itoa(lid)+"/transition", map[string]interface{}{
		"to_status": "sealed", "reason": "归档",
	})
	successOk(t, status, resp)
}

// V1验证-1.32 状态切换非法路径（registered → planned）应被拒绝
func TestHTTP_Ledgers_TransitionIllegalRejected(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")
	_, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)
	d := dataMap(t, resp)
	lid := int64(d["ledger"].(map[string]interface{})["id"].(float64))

	status, resp := jsonReq(t, r, "POST", "/ledgers/"+itoa(lid)+"/transition", map[string]interface{}{
		"to_status": "planned",
	})
	expectFailure(t, status, resp)
}

// V1验证-1.33 状态切换需要 close 权限：不在严格匹配且项目内无人有 close → 403
func TestHTTP_Ledgers_TransitionNeedClosePermission(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// 立项时只给 read+write+submit（去掉 close 不行 — 立项强制要求 close 至少一人 — 改为只给一人，仅有 close）
	tplCode, tplVer := seedTestTemplateForHTTP(t, db)
	owner, custodian, security := seedTestSubjectsForHTTP(t, db)
	// 多创一个 subject 不给 close
	now := "now"
	res, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('NO-CLOSE', '无 close 权限的人', 'person', 'active', ?, ?, 0)`, now, now)
	noCloseSubID, _ := res.LastInsertId()
	withProjectRoot(t, db)

	svc := repository.NewProjectInstantiationService(db)
	out, err := svc.Instantiate(repository.InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "权限测试",
		ObjectShortCode:    "HT-PERM",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []repository.MemberInput{
			{SubjectID: noCloseSubID, RoleCode: "EDITOR", PermissionActions: []string{"read", "write", "submit"}}, // 没 close
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "receive", "submit", "close"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 用没 close 的人 NO-CLOSE 当 active user
	withActiveUser(t, db, "NO-CLOSE")

	// 上传一个文件
	fvID := findFvIDByLocal(t, out.Stages, "MZ-SG", "IN-001")
	_, uploadResp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)
	d := dataMap(t, uploadResp)
	lid := int64(d["ledger"].(map[string]interface{})["id"].(float64))

	// 切换状态应当 403（NO-CLOSE 无 close 权限）
	w := httpPost(t, r, "/ledgers/"+itoa(lid)+"/transition", map[string]interface{}{
		"to_status": "in_use",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// V1验证-1.34 非法 id 返回 400
func TestHTTP_Ledgers_InvalidID(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	w := httptestRecord(t, r, "GET", "/ledgers/abc")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	w = httptestRecord(t, r, "GET", "/ledgers/9999")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
