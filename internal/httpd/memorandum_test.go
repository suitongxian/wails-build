package httpd

import (
	"testing"
	"time"
)

func TestHTTP_Memorandum_Pending_ListsUnregisteredCore(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	_, err := db.Exec(`INSERT INTO asset_ledgers (
		ledger_code, file_version_id, class_code, project_code, stage_code,
		file_version_code, asset_name, owner_subject_id, custodian_subject_id, security_subject_id,
		sensitivity_level, marking_method, lifecycle_status, create_time, update_time, disable
	) VALUES ('LG-MEMO-1', 1, NULL, 'SYS-PERSONAL-CORE', 'GR-DA',
		'OUT-001', '机要资料.docx', 0, 0, 0, 'core_secret', 'reference', 'registered', ?, ?, 0)`, now, now)
	if err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/memorandum/pending")
	successOk(t, status, resp)
	items, _ := dataMap(t, resp)["items"].([]interface{})
	if len(items) < 1 {
		t.Errorf("pending memorandum items = %d, want ≥ 1", len(items))
	}
}

func TestHTTP_Memorandum_Register_RequiresPasswordAndFlipsState(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// 建用户 + 设密码 md5
	pwdMD5 := "5f4dcc3b5aa765d61d8327deb882cf99" // md5("password")
	db.Exec(`INSERT INTO user_info (company_name, user_name, department, ip, mac_address, work_address, phone, password_md5, create_time, update_time, disable)
		VALUES ('u', 'u1', 'd', '127.0.0.1', '00:00:00:00:00:00', '', '10000000000', ?, ?, ?, 0)`,
		pwdMD5, time.Now(), time.Now())
	db.Exec(`INSERT INTO users (username, display_name, status, create_time, update_time, disable)
		VALUES ('u1', 'u1', 'active', ?, ?, 0)`, time.Now(), time.Now())

	now := time.Now()
	res, _ := db.Exec(`INSERT INTO asset_ledgers (
		ledger_code, file_version_id, project_code, stage_code, file_version_code, asset_name,
		owner_subject_id, custodian_subject_id, security_subject_id, sensitivity_level,
		marking_method, lifecycle_status, create_time, update_time, disable
	) VALUES ('LG-MEMO-2', 2, 'SYS-PERSONAL-CORE', 'GR-DA', 'OUT-001', 'x.docx',
		0, 0, 0, 'core_secret', 'reference', 'registered', ?, ?, 0)`, now, now)
	ledgerID, _ := res.LastInsertId()

	// 密码错 → 401
	status, resp := jsonReq(t, r, "POST", "/memorandum/register", map[string]interface{}{
		"ledger_id":      ledgerID,
		"topic":          "客户合同",
		"classification": "秘密",
		"password":       "wrong",
	})
	expectFailure(t, status, resp)

	// 密码对 → 成功
	status, resp = jsonReq(t, r, "POST", "/memorandum/register", map[string]interface{}{
		"ledger_id":      ledgerID,
		"topic":          "客户合同",
		"classification": "秘密",
		"password":       "password",
	})
	successOk(t, status, resp)

	var registeredAt *time.Time
	db.Get(&registeredAt, `SELECT memorandum_registered_at FROM asset_ledgers WHERE id = ?`, ledgerID)
	if registeredAt == nil {
		t.Error("memorandum_registered_at should be set after register")
	}

	// 重复登记 → 400
	status, resp = jsonReq(t, r, "POST", "/memorandum/register", map[string]interface{}{
		"ledger_id":      ledgerID,
		"topic":          "重复",
		"classification": "秘密",
		"password":       "password",
	})
	expectFailure(t, status, resp)
}
