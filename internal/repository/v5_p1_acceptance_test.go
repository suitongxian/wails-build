package repository

import (
	"testing"
)

// V5-P1 端到端验收：覆盖 §4 审计的 4 项缺口（A1-A4）
//
// 路线：
//
//	模版 V2.0 加载
//	  → 双环节项目初始化
//	  → 默认桥接到定稿环节
//	  → hint=process 桥接到过程环节
//	  → 解绑 (lifecycle cancelled + 链路完整)
//	  → 重新归类 (原 fv cancelled + 新 fv registered + reclassified_from_fv_id 链路)
//	  → 家族分流归档 (newest → final, others → process)
//
// 这是回归基线。新加 V5 功能后此测试若仍 PASS，可视为 P1 行为基线未破坏。
func TestV5P1_EndToEnd(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	// 1. seed V2.0 模版（模拟 manage 端同步）
	seedPersonalFilesTemplateV2InTest(t, db)

	// 2. 启动初始化：3 个项目 + 每项目 2 stages
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatalf("ensure failed: %v", err)
	}
	for _, code := range []string{PersonalCoreProjectCode, PersonalImportantProjectCode, PersonalGeneralProjectCode} {
		var stageCount int
		if err := db.Get(&stageCount, `SELECT COUNT(*) FROM project_stages ps
			JOIN data_projects p ON p.id = ps.project_id
			WHERE p.project_code = ? AND ps.disable = 0`, code); err != nil {
			t.Fatalf("[step 2] 查 %s stage 数失败: %v", code, err)
		}
		if stageCount != 2 {
			t.Errorf("[step 2] %s 应有 2 stage, got %d", code, stageCount)
		}
	}

	// 3. 桥接：默认归 GR-FINAL（importance=2 → IMPORTANT 项目）
	resID1 := seedResourceForBridge(t, db, "定稿测试.pdf", "E2E001", 2)
	fvID1, err := BridgeClassifyToPersonalProject(db, resID1)
	if err != nil {
		t.Fatalf("[step 3] bridge default failed: %v", err)
	}
	if fvID1 == 0 {
		t.Fatal("[step 3] 默认桥接应返回有效 fv id")
	}
	var stage1, state1 string
	if err := db.Get(&stage1, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID1); err != nil {
		t.Fatalf("[step 3] 查 stage 失败: %v", err)
	}
	if err := db.Get(&state1, `SELECT data_state FROM file_versions WHERE id = ?`, fvID1); err != nil {
		t.Fatalf("[step 3] 查 data_state 失败: %v", err)
	}
	if stage1 != "GR-FINAL" {
		t.Errorf("[step 3] 默认 stage 应 GR-FINAL, got %s", stage1)
	}
	if state1 != "output" {
		t.Errorf("[step 3] data_state 应 output, got %s", state1)
	}

	// 4. 桥接：hint=process 归 GR-DRAFT
	resID2 := seedResourceForBridge(t, db, "草稿测试.pdf", "E2E002", 2)
	fvID2, err := BridgeClassifyToPersonalProjectWithState(db, resID2, "process")
	if err != nil {
		t.Fatalf("[step 4] bridge process failed: %v", err)
	}
	if fvID2 == 0 {
		t.Fatal("[step 4] hint=process 桥接应返回有效 fv id")
	}
	var stage2, state2 string
	if err := db.Get(&stage2, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID2); err != nil {
		t.Fatalf("[step 4] 查 stage 失败: %v", err)
	}
	if err := db.Get(&state2, `SELECT data_state FROM file_versions WHERE id = ?`, fvID2); err != nil {
		t.Fatalf("[step 4] 查 data_state 失败: %v", err)
	}
	if stage2 != "GR-DRAFT" {
		t.Errorf("[step 4] hint=process stage 应 GR-DRAFT, got %s", stage2)
	}
	if state2 != "process" {
		t.Errorf("[step 4] data_state 应 process, got %s", state2)
	}

	// 5. 解绑 fvID1
	if err := UnbindFileVersion(db, fvID1, "e2e unbind", &testUser); err != nil {
		t.Fatalf("[step 5] unbind failed: %v", err)
	}
	var lc string
	if err := db.Get(&lc, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, fvID1); err != nil {
		t.Fatalf("[step 5] 查 fv lifecycle 失败: %v", err)
	}
	if lc != "cancelled" {
		t.Errorf("[step 5] unbind 后 fv 应 cancelled, got %s", lc)
	}
	var ledgerLc string
	if err := db.Get(&ledgerLc, `SELECT lifecycle_status FROM asset_ledgers WHERE file_version_id = ?`, fvID1); err != nil {
		t.Fatalf("[step 5] 查 ledger 失败: %v", err)
	}
	if ledgerLc != "cancelled" {
		t.Errorf("[step 5] unbind 后 ledger 应 cancelled, got %s", ledgerLc)
	}
	var hist1 int
	db.Get(&hist1, `SELECT COUNT(*) FROM reclassify_history WHERE original_fv_id = ? AND action = 'unbind'`, fvID1)
	if hist1 != 1 {
		t.Errorf("[step 5] 应有 1 条 unbind 历史, got %d", hist1)
	}
	var evCount int
	db.Get(&evCount, `SELECT COUNT(*) FROM lifecycle_events WHERE file_version_id = ? AND event_type = ?`, fvID1, EventUnbind)
	if evCount != 1 {
		t.Errorf("[step 5] 应有 1 条 unbind lifecycle_event, got %d", evCount)
	}

	// 6. 重新归类：importance=1 → CORE 项目 (origFv)，重归到 GENERAL 项目 GR-DRAFT
	resID3 := seedResourceForBridge(t, db, "重归测试.pdf", "E2E003", 1)
	origFv, err := BridgeClassifyToPersonalProject(db, resID3)
	if err != nil {
		t.Fatalf("[step 6] 预桥接 orig fv 失败: %v", err)
	}
	if origFv == 0 {
		t.Fatal("[step 6] 预桥接应返回有效 fv id")
	}
	var generalID int64
	if err := db.Get(&generalID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalGeneralProjectCode); err != nil {
		t.Fatalf("[step 6] 找 GENERAL 项目失败: %v", err)
	}
	newFv, err := ReclassifyFileVersion(db, ReclassifyInput{
		OriginalFvID:    origFv,
		NewProjectID:    generalID,
		NewStageCode:    "GR-DRAFT",
		NewFileRuleCode: "PRC-001",
		Reason:          "e2e reclassify",
		OperatorUser:    &testUser,
	})
	if err != nil {
		t.Fatalf("[step 6] reclassify failed: %v", err)
	}
	if newFv == origFv || newFv == 0 {
		t.Fatalf("[step 6] new fv 应区别于 orig, got new=%d orig=%d", newFv, origFv)
	}
	var origLc string
	db.Get(&origLc, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, origFv)
	if origLc != "cancelled" {
		t.Errorf("[step 6] orig fv 应 cancelled, got %s", origLc)
	}
	var newLc string
	var fromFv int64
	db.Get(&newLc, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, newFv)
	db.Get(&fromFv, `SELECT reclassified_from_fv_id FROM file_versions WHERE id = ?`, newFv)
	if newLc != "registered" {
		t.Errorf("[step 6] new fv 应 registered, got %s", newLc)
	}
	if fromFv != origFv {
		t.Errorf("[step 6] new fv 应链回 orig, got from=%d expect=%d", fromFv, origFv)
	}

	// 7. 家族分流归档：3 个 member 按 first_create_time 倒序，最新 → GR-FINAL，其余 → GR-DRAFT。
	// 复用 family_bridge_test.go 的 seedFamilyWithDatedMembers helper（已存在于同包内）。
	famID, _ := seedFamilyWithDatedMembers(t, db, []string{"E2EFAM1", "E2EFAM2", "E2EFAM3"})

	var importantID int64
	if err := db.Get(&importantID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalImportantProjectCode); err != nil {
		t.Fatalf("[step 7] 找 IMPORTANT 项目失败: %v", err)
	}

	splitResult, err := BridgeFamilyToProjectSplit(db, famID, importantID,
		"GR-DRAFT", "PRC-001", // process target
		"GR-FINAL", "OUT-001", // final target
	)
	if err != nil {
		t.Fatalf("[step 7] family split failed: %v", err)
	}
	if splitResult.Total != 3 {
		t.Errorf("[step 7] Total 应=3, got %d", splitResult.Total)
	}
	if splitResult.Archived != 3 {
		t.Errorf("[step 7] Archived 应=3, got %d (errors=%d)", splitResult.Archived, splitResult.Errors)
	}

	// 最新 (E2EFAM3) → GR-FINAL
	var finalCount int
	db.Get(&finalCount, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		JOIN data_projects p ON p.id = fv.project_id
		WHERE ps.stage_code = 'GR-FINAL' AND p.project_code = ? AND fv.checksum = 'E2EFAM3'`,
		PersonalImportantProjectCode)
	if finalCount != 1 {
		t.Errorf("[step 7] E2EFAM3 (最新) 应归 GR-FINAL, got %d", finalCount)
	}
	// 较旧 (E2EFAM1, E2EFAM2) → GR-DRAFT
	var draftCount int
	db.Get(&draftCount, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		JOIN data_projects p ON p.id = fv.project_id
		WHERE ps.stage_code = 'GR-DRAFT' AND p.project_code = ? AND fv.checksum IN ('E2EFAM1','E2EFAM2')`,
		PersonalImportantProjectCode)
	if draftCount != 2 {
		t.Errorf("[step 7] E2EFAM1+E2EFAM2 应归 GR-DRAFT, got %d", draftCount)
	}
}
