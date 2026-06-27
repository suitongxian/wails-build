package repository

import (
	"testing"
	"time"
)

var testUser = UserSnapshot{Name: "tester"}

func TestUnbindFileVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	resID := seedResourceForBridge(t, db, "解绑.pdf", "UB001", 2)
	fvID, _ := BridgeClassifyToPersonalProject(db, resID)
	if fvID == 0 {
		t.Fatal("expected fv to exist")
	}

	if err := UnbindFileVersion(db, fvID, "误归目，文件已重新归档别处", &testUser); err != nil {
		t.Fatalf("unbind failed: %v", err)
	}

	var status string
	db.Get(&status, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, fvID)
	if status != "cancelled" {
		t.Errorf("fv lifecycle 应 cancelled, got %s", status)
	}

	var ledgerStatus string
	db.Get(&ledgerStatus, `SELECT lifecycle_status FROM asset_ledgers WHERE file_version_id = ?`, fvID)
	if ledgerStatus != "cancelled" {
		t.Errorf("ledger lifecycle 应 cancelled, got %s", ledgerStatus)
	}

	var unbindReason string
	db.Get(&unbindReason, `SELECT unbind_reason FROM file_versions WHERE id = ?`, fvID)
	if unbindReason != "误归目，文件已重新归档别处" {
		t.Errorf("reason mismatch: %s", unbindReason)
	}

	var historyCount int
	db.Get(&historyCount, `SELECT COUNT(*) FROM reclassify_history WHERE original_fv_id = ? AND action = 'unbind'`, fvID)
	if historyCount != 1 {
		t.Errorf("应有 1 条 unbind 历史, got %d", historyCount)
	}

	var eventCount int
	db.Get(&eventCount, `SELECT COUNT(*) FROM lifecycle_events WHERE file_version_id = ? AND event_type = 'unbind'`, fvID)
	if eventCount != 1 {
		t.Errorf("应有 1 条 unbind event, got %d", eventCount)
	}
}

func TestUnbindFileVersion_EmptyReason(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if err := UnbindFileVersion(db, 1, "", nil); err == nil {
		t.Fatal("空 reason 应失败")
	}
}

func TestUnbindFileVersion_AlreadyCancelled(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	resID := seedResourceForBridge(t, db, "已取消.pdf", "UB002", 2)
	fvID, _ := BridgeClassifyToPersonalProject(db, resID)
	if err := UnbindFileVersion(db, fvID, "first time", &testUser); err != nil {
		t.Fatal(err)
	}
	if err := UnbindFileVersion(db, fvID, "second time", &testUser); err == nil {
		t.Fatal("二次解绑应失败")
	}
}

func TestReclassifyFileVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	resID := seedResourceForBridge(t, db, "重归.pdf", "RC001", 1) // importance=1 -> CORE
	origFvID, _ := BridgeClassifyToPersonalProject(db, resID)
	if origFvID == 0 {
		t.Fatal("expected orig fv")
	}

	var generalID int64
	db.Get(&generalID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalGeneralProjectCode)

	newFvID, err := ReclassifyFileVersion(db, ReclassifyInput{
		OriginalFvID:    origFvID,
		NewProjectID:    generalID,
		NewStageCode:    "GR-DRAFT",
		NewFileRuleCode: "PRC-001",
		Reason:          "应归一般级而非核心级",
		OperatorUser:    &testUser,
	})
	if err != nil {
		t.Fatalf("reclassify failed: %v", err)
	}
	if newFvID == 0 {
		t.Fatal("应返回 new fv id")
	}
	if newFvID == origFvID {
		t.Error("new fv id 应不同于 orig")
	}

	var origStatus string
	db.Get(&origStatus, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, origFvID)
	if origStatus != "cancelled" {
		t.Errorf("orig fv 应 cancelled, got %s", origStatus)
	}

	var newStatus string
	var fromFv int64
	db.Get(&newStatus, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, newFvID)
	db.Get(&fromFv, `SELECT reclassified_from_fv_id FROM file_versions WHERE id = ?`, newFvID)
	if newStatus != "registered" {
		t.Errorf("new fv 应 registered, got %s", newStatus)
	}
	if fromFv != origFvID {
		t.Errorf("reclassified_from_fv_id 应=%d, got %d", origFvID, fromFv)
	}

	var hist int
	db.Get(&hist, `SELECT COUNT(*) FROM reclassify_history WHERE original_fv_id = ?`, origFvID)
	if hist != 2 {
		t.Errorf("应有 2 条 history (unbind + reclassify), got %d", hist)
	}
}

func TestReclassifyFileVersion_EmptyReason(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := ReclassifyFileVersion(db, ReclassifyInput{OriginalFvID: 1}); err == nil {
		t.Fatal("空 reason 应失败")
	}
}

func TestReclassifyFileVersion_MissingResourceRef(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	// 手工建一个没有 resource source_ref 的 fv（模拟非桥接来源）
	var coreID int64
	db.Get(&coreID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalCoreProjectCode)
	var stageID int64
	db.Get(&stageID, `SELECT id FROM project_stages WHERE project_id = ? LIMIT 1`, coreID)
	res, err := db.Exec(`INSERT INTO file_versions (
		project_id, project_stage_id, file_version_code, local_code,
		display_name, data_state, version_no, required, lifecycle_status,
		create_time, update_time, disable
	) VALUES (?, ?, ?, 'OUT-001', '手工.pdf', 'output', 'V1.0', 0, 'registered', ?, ?, 0)`,
		coreID, stageID, "MANUAL-FV-001", time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fvID, _ := res.LastInsertId()
	// ledger 不带 source_ref
	db.Exec(`INSERT INTO asset_ledgers (
		ledger_code, file_version_id, project_code, stage_code, file_version_code, asset_name,
		owner_subject_id, custodian_subject_id, security_subject_id,
		sensitivity_level, marking_method, lifecycle_status,
		create_time, update_time, disable
	) VALUES ('L-MANUAL-001', ?, 'SYS-PERSONAL-CORE', 'GR-FINAL', 'MANUAL-FV-001', '手工',
		1, 1, 1, 'core_secret', 'reference', 'registered', ?, ?, 0)`,
		fvID, time.Now(), time.Now())

	var generalID int64
	db.Get(&generalID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalGeneralProjectCode)

	_, err = ReclassifyFileVersion(db, ReclassifyInput{
		OriginalFvID:    fvID,
		NewProjectID:    generalID,
		NewStageCode:    "GR-DRAFT",
		NewFileRuleCode: "PRC-001",
		Reason:          "test missing ref",
		OperatorUser:    &testUser,
	})
	if err == nil {
		t.Fatal("缺 source_ref 应失败")
	}
}

func TestReclassifyFileVersion_InvalidTarget(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	resID := seedResourceForBridge(t, db, "无效目标.pdf", "RCIT001", 2)
	origFvID, _ := BridgeClassifyToPersonalProject(db, resID)

	_, err := ReclassifyFileVersion(db, ReclassifyInput{
		OriginalFvID:    origFvID,
		NewProjectID:    99999, // 不存在的项目
		NewStageCode:    "GR-DRAFT",
		NewFileRuleCode: "PRC-001",
		Reason:          "test invalid target",
		OperatorUser:    &testUser,
	})
	if err == nil {
		t.Fatal("无效目标项目应失败")
	}
}

func TestJsonExtractInt(t *testing.T) {
	cases := []struct {
		name  string
		input string
		key   string
		want  int64
	}{
		{"normal", `{"bridge_from":"data_resources","resource_id":42}`, "resource_id", 42},
		{"empty", ``, "resource_id", 0},
		{"missing key", `{"foo":"bar"}`, "resource_id", 0},
		{"large number", `{"resource_id":987654321}`, "resource_id", 987654321},
		{"first occurrence wins (single matches)", `{"resource_id":7}`, "resource_id", 7},
		{"whitespace breaks current impl by design", `{"resource_id": 5}`, "resource_id", 0},
		{"quoted number breaks by design", `{"resource_id":"5"}`, "resource_id", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := jsonExtractInt(c.input, c.key)
			if got != c.want {
				t.Errorf("jsonExtractInt(%q, %q) = %d, want %d", c.input, c.key, got, c.want)
			}
		})
	}
}
