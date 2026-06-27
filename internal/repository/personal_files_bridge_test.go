package repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// seedDataResource 给 bridge 测试用：插一条 data_resources 模拟扫到的已分类文件
func seedDataResourceForBridge(t *testing.T, db *sqlx.DB, contentSign string, importance int, name string) int64 {
	t.Helper()
	now := time.Now()
	res, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable
	) VALUES (?, 1, 1, ?, ?, 2, ?, ?, ?, 0)`,
		contentSign, now, name, importance, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// V4-Q2 importance=1 → 桥接到 SYS-PERSONAL-CORE
func TestBridgeClassify_CoreLevel(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	resID := seedDataResourceForBridge(t, db, "AAAA111", 1, "核心文件.docx")

	fvID, err := BridgeClassifyToPersonalProject(db, resID)
	if err != nil {
		t.Fatal(err)
	}
	if fvID == 0 {
		t.Fatal("应当返回新建的 fv id")
	}

	// 验 fv 挂在 core 项目下
	var projectCode string
	db.Get(&projectCode, `SELECT p.project_code FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id WHERE fv.id = ?`, fvID)
	if projectCode != PersonalCoreProjectCode {
		t.Errorf("fv 应挂在 %s, got %s", PersonalCoreProjectCode, projectCode)
	}

	// 必有对应 ledger
	var ledgerCount int
	db.Get(&ledgerCount, `SELECT COUNT(*) FROM asset_ledgers WHERE file_version_id = ?`, fvID)
	if ledgerCount != 1 {
		t.Errorf("应当生成 1 条 ledger，got %d", ledgerCount)
	}

	// 必有 register 事件
	var eventCount int
	db.Get(&eventCount, `SELECT COUNT(*) FROM lifecycle_events WHERE file_version_id = ? AND event_type = 'register'`, fvID)
	if eventCount != 1 {
		t.Errorf("应当生成 1 条 register 事件，got %d", eventCount)
	}
}

// V4-Q2 importance=2 → important 项目
func TestBridgeClassify_ImportantLevel(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	ensurePersonalFilesContext(db)
	resID := seedDataResourceForBridge(t, db, "BBBB222", 2, "重要文件.pdf")

	fvID, _ := BridgeClassifyToPersonalProject(db, resID)
	var pc string
	db.Get(&pc, `SELECT p.project_code FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id WHERE fv.id = ?`, fvID)
	if pc != PersonalImportantProjectCode {
		t.Errorf("应挂 important 项目, got %s", pc)
	}
}

// V4-Q2 importance=3 → general 项目
func TestBridgeClassify_GeneralLevel(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	ensurePersonalFilesContext(db)
	resID := seedDataResourceForBridge(t, db, "CCCC333", 3, "一般文件.txt")

	fvID, _ := BridgeClassifyToPersonalProject(db, resID)
	var pc string
	db.Get(&pc, `SELECT p.project_code FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id WHERE fv.id = ?`, fvID)
	if pc != PersonalGeneralProjectCode {
		t.Errorf("应挂 general 项目, got %s", pc)
	}
}

// V4-Q2 importance=4 (隐私) → 静默跳过，不挂账
func TestBridgeClassify_PrivacyLevelSkipped(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	ensurePersonalFilesContext(db)
	resID := seedDataResourceForBridge(t, db, "DDDD444", 4, "隐私文件.docx")

	fvID, err := BridgeClassifyToPersonalProject(db, resID)
	if err != nil {
		t.Errorf("隐私级别应静默跳过，但有错: %v", err)
	}
	if fvID != 0 {
		t.Errorf("隐私级别不应挂账，但返回 fv id %d", fvID)
	}
}

// V4-Q2 重复调用幂等 — 同一 resource 重复 classify 不重复挂账
//
// V5-P1.1 撤掉 Q2 软阻止后行为：第二次返回 fvID1（fvCode 幂等命中）。
// 用户视角：无新 fv 产生，与之前等价。
func TestBridgeClassify_Idempotent(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	ensurePersonalFilesContext(db)
	resID := seedDataResourceForBridge(t, db, "EEEE555", 2, "可重复.pdf")

	fvID1, _ := BridgeClassifyToPersonalProject(db, resID)
	fvID2, _ := BridgeClassifyToPersonalProject(db, resID)
	if fvID1 == 0 {
		t.Fatal("第一次 classify 应桥接成功")
	}
	if fvID2 != fvID1 {
		t.Errorf("第二次 classify 应返回 fvID1（fvCode 幂等），got fv=%d, want=%d", fvID2, fvID1)
	}

	// 总归仍只有 1 个非 cancelled fv 指向同一资源
	var fvCount int
	db.Get(&fvCount, `SELECT COUNT(*) FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id
		WHERE p.project_code LIKE 'SYS-PERSONAL-%' AND fv.checksum = 'EEEE555' AND fv.lifecycle_status != 'cancelled'`)
	if fvCount != 1 {
		t.Errorf("应只有 1 条非 cancelled fv，got %d", fvCount)
	}

	// ledger / event 也不应翻倍
	var ledgerCount, eventCount int
	db.Get(&ledgerCount, `SELECT COUNT(*) FROM asset_ledgers WHERE file_version_id = ?`, fvID1)
	db.Get(&eventCount, `SELECT COUNT(*) FROM lifecycle_events WHERE file_version_id = ? AND event_type = 'register'`, fvID1)
	if ledgerCount != 1 || eventCount != 1 {
		t.Errorf("重复挂账：ledger=%d, events=%d (期望各 1)", ledgerCount, eventCount)
	}
}

// V4-Q2 内置项目未就绪（模版未同步） → 静默跳过
func TestBridgeClassify_SkipsWhenProjectsMissing(t *testing.T) {
	db := openTestDB(t)
	// 不 seed 模版 / 不调 ensurePersonalFilesContext
	resID := seedDataResourceForBridge(t, db, "FFFF666", 1, "孤儿文件.docx")

	fvID, err := BridgeClassifyToPersonalProject(db, resID)
	if err != nil {
		t.Errorf("内置项目未就绪应静默跳过，但有错: %v", err)
	}
	if fvID != 0 {
		t.Errorf("内置项目未就绪不应挂账, 返回 fv=%d", fvID)
	}
}

// V4-Q2 resource 不存在 → 报错
func TestBridgeClassify_ResourceNotFound(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	ensurePersonalFilesContext(db)
	_, err := BridgeClassifyToPersonalProject(db, 99999)
	if err == nil {
		t.Error("不存在的 resource_id 应报错")
	}
}

// V5-P1 Task3 §4.2-6 默认归 GR-FINAL/OUT-001/output（用户认领归类视为已定稿）
func TestBridge_ToFinalStage_Default(t *testing.T) {
	db := openTestDB(t)
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	resID := seedResourceForBridge(t, db, "测试.pdf", "BR001", 2 /*importance=2 重要*/)
	fvID, err := BridgeClassifyToPersonalProject(db, resID)
	if err != nil {
		t.Fatal(err)
	}
	if fvID == 0 {
		t.Fatal("应桥接成功")
	}

	var stage, state string
	if err := db.Get(&stage, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&state, `SELECT data_state FROM file_versions WHERE id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if stage != "GR-FINAL" {
		t.Errorf("默认应归 GR-FINAL, got %s", stage)
	}
	if state != "output" {
		t.Errorf("data_state 应为 output, got %s", state)
	}
}

// V5-P1 Task3 §4.2-6 显式 hint=process → GR-DRAFT/PRC-001/process
func TestBridge_ToDraftStage_WithHint(t *testing.T) {
	db := openTestDB(t)
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	resID := seedResourceForBridge(t, db, "草稿.pdf", "BR002", 2)
	fvID, err := BridgeClassifyToPersonalProjectWithState(db, resID, "process")
	if err != nil {
		t.Fatal(err)
	}

	var stage, state string
	if err := db.Get(&stage, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&state, `SELECT data_state FROM file_versions WHERE id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if stage != "GR-DRAFT" {
		t.Errorf("hint=process 应归 GR-DRAFT, got %s", stage)
	}
	if state != "process" {
		t.Errorf("data_state 应为 process, got %s", state)
	}
}

func TestBridge_PersonalClassifyOnlyMarksLedgerWithoutMovingFile(t *testing.T) {
	db := openTestDB(t)
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	resID := seedResourceForBridge(t, db, "原位置文件.docx", "MARKONLY001", 3)
	fvID, err := BridgeClassifyToPersonalProjectWithState(db, resID, "process")
	if err != nil {
		t.Fatal(err)
	}
	if fvID == 0 {
		t.Fatal("应桥接成功")
	}

	var storage sql.NullString
	if err := db.Get(&storage, `SELECT storage_uri FROM file_versions WHERE id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if storage.Valid && storage.String != "" {
		t.Fatalf("personal classify should not copy/move file into managed storage, got storage_uri=%q", storage.String)
	}

	var sourceRef string
	if err := db.Get(&sourceRef, `SELECT source_ref FROM asset_ledgers WHERE file_version_id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sourceRef, `"bridge_from":"data_resources"`) || !strings.Contains(sourceRef, `"resource_id":`) {
		t.Fatalf("ledger should keep source_ref to scanned resource, got %s", sourceRef)
	}
}

// V5-P1 Task3 V1.0 模版仍只有 GR-DA 单环节，桥接应回退到该环节
func TestBridge_LegacyV1_StillWorks(t *testing.T) {
	db := openTestDB(t)
	// Seed only V1.0 (single GR-DA stage).
	seedMockPersonalFilesTemplate(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	resID := seedResourceForBridge(t, db, "兼容.pdf", "BR003", 2)
	fvID, err := BridgeClassifyToPersonalProject(db, resID)
	if err != nil {
		t.Fatal(err)
	}
	if fvID == 0 {
		t.Fatal("V1.0 兼容路径应桥接成功")
	}

	var stage string
	if err := db.Get(&stage, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if stage != "GR-DA" {
		t.Errorf("V1.0 兼容路径应归 GR-DA, got %s", stage)
	}
}

// seedResourceForBridge 给 Task3 新测试用：同 seedDataResourceForBridge 但参数顺序按
// 任务说明顺序排列（name, sign, importance）。
func seedResourceForBridge(t *testing.T, db *sqlx.DB, name, sign string, importance int) int64 {
	t.Helper()
	now := time.Now()
	r, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable
	) VALUES (?, 1, 1, ?, ?, 2, ?, ?, ?, 0)`,
		sign, now, name, importance, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

// V5-P1.1: 解绑后允许重新桥接（cancelled fv 不算挂账）
//
// 注意：撤掉 Q2 软阻止后，本测试只验 cancelled 后 fvCode 幂等放过 + -R<n> 后缀生效。
func TestBridge_Classify_AllowsAfterUnbind(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	resID := seedResourceForBridge(t, db, "解绑后再来.pdf", "SB002", 2)
	fv1, _ := BridgeClassifyToPersonalProject(db, resID)
	if fv1 == 0 {
		t.Fatal("第一次桥接应成功")
	}

	if err := UnbindFileVersion(db, fv1, "test unbind", &testUser); err != nil {
		t.Fatal(err)
	}

	fv2, err := BridgeClassifyToPersonalProject(db, resID)
	if err != nil {
		t.Fatalf("解绑后桥接应成功: %v", err)
	}
	if fv2 == 0 || fv2 == fv1 {
		t.Errorf("解绑后应产生新 fv, got fv1=%d fv2=%d", fv1, fv2)
	}
}

// V5-P1.1 §4.3-6 家族传播：单文件（无 family_id）→ 等价于单条桥接
func TestBridgeClassifyWithFamilyPropagation_NoFamily(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	resID := seedResourceForBridge(t, db, "单文件.pdf", "FAM_NONE001", 2)
	result, err := BridgeClassifyWithFamilyPropagation(db, resID)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasFamily {
		t.Error("应识别为无 family")
	}
	if result.BridgedCount != 1 {
		t.Errorf("应桥接 1 条, got %d", result.BridgedCount)
	}
	if result.LeadFvID == 0 {
		t.Error("应返回 lead fv id")
	}
	if result.PropagatedCount != 1 {
		t.Errorf("无 family 应记 propagated=1 (仅 lead), got %d", result.PropagatedCount)
	}
}

// V5-P1.1 §4.3-6 家族传播：classify 一个成员 → 全 family importance_level 同步 + 整族 split 归档
func TestBridgeClassifyWithFamilyPropagation_FamilyAllPropagated(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	// 建 family + 3 个 member，时间递增（FAMPROP3 最新），importance 初始 0
	famID, ids := seedFamilyWithDatedMembers(t, db, []string{"FAMPROP1", "FAMPROP2", "FAMPROP3"})
	_ = famID
	// seedFamilyWithDatedMembers 默认 importance=2，先全置 0 模拟 classify 前的态
	if _, err := db.Exec(`UPDATE data_resources SET importance_level = 0
		WHERE data_resources_id IN (?,?,?)`, ids[0], ids[1], ids[2]); err != nil {
		t.Fatal(err)
	}

	// 模拟 SingleClassifyResource: 先把主成员 importance 置 2，再调家族传播入口
	leadID := ids[0]
	if _, err := db.Exec(`UPDATE data_resources SET importance_level = 2 WHERE data_resources_id = ?`, leadID); err != nil {
		t.Fatal(err)
	}
	result, err := BridgeClassifyWithFamilyPropagation(db, leadID)
	if err != nil {
		t.Fatalf("家族传播失败: %v", err)
	}
	if !result.HasFamily {
		t.Error("应识别为有 family")
	}
	if result.PropagatedCount != 3 {
		t.Errorf("应传播 3 个成员, got %d", result.PropagatedCount)
	}
	if result.BridgedCount != 3 {
		t.Errorf("应桥接 3 条, got %d (errors=%d)", result.BridgedCount, result.ErrorCount)
	}

	// 验证所有 3 个 resource 的 importance_level 都被设为 2
	var count int
	db.Get(&count, `SELECT COUNT(*) FROM data_resources
		WHERE data_resources_id IN (?,?,?) AND importance_level = 2`, ids[0], ids[1], ids[2])
	if count != 3 {
		t.Errorf("应有 3 个成员 importance=2, got %d", count)
	}

	// 验证 split 模式：FAMPROP3（最新）→ GR-FINAL
	var finalCount int
	db.Get(&finalCount, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		WHERE ps.stage_code = 'GR-FINAL' AND fv.checksum = 'FAMPROP3'`)
	if finalCount != 1 {
		t.Errorf("FAMPROP3 应归 GR-FINAL, got %d", finalCount)
	}
	// FAMPROP1+FAMPROP2 → GR-DRAFT
	var draftCount int
	db.Get(&draftCount, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		WHERE ps.stage_code = 'GR-DRAFT' AND fv.checksum IN ('FAMPROP1','FAMPROP2')`)
	if draftCount != 2 {
		t.Errorf("FAMPROP1+FAMPROP2 应归 GR-DRAFT, got %d", draftCount)
	}
}

// V5-P1.1 §4.3-6 家族传播：importance=4 (隐私) → 不传播 不桥接
func TestBridgeClassifyWithFamilyPropagation_Privacy(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	resID := seedResourceForBridge(t, db, "隐私.pdf", "PRIV001", 4)
	result, err := BridgeClassifyWithFamilyPropagation(db, resID)
	if err != nil {
		t.Fatal(err)
	}
	if result.BridgedCount != 0 {
		t.Errorf("隐私 importance=4 不应桥接, got %d", result.BridgedCount)
	}
	if result.PropagatedCount != 0 {
		t.Errorf("隐私 importance=4 不应传播, got %d", result.PropagatedCount)
	}
	if result.HasFamily {
		t.Error("无 family，HasFamily 应 false")
	}
}

// V5-P1.1 §4.3-6 家族传播：classify 后家族其他成员的 importance 被联动改写
func TestBridgeClassifyWithFamilyPropagation_OverwritesExisting(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	// 建 family 3 成员，默认 importance=2
	famID, ids := seedFamilyWithDatedMembers(t, db, []string{"OVER1", "OVER2", "OVER3"})
	_ = famID

	// 用户把第二个成员重设为 importance=1（核心），触发家族传播
	if _, err := db.Exec(`UPDATE data_resources SET importance_level = 1 WHERE data_resources_id = ?`, ids[1]); err != nil {
		t.Fatal(err)
	}
	result, err := BridgeClassifyWithFamilyPropagation(db, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasFamily {
		t.Error("应识别为有 family")
	}

	// 全 family 都应被改写为 importance=1
	var c1 int
	db.Get(&c1, `SELECT COUNT(*) FROM data_resources
		WHERE data_resources_id IN (?,?,?) AND importance_level = 1`, ids[0], ids[1], ids[2])
	if c1 != 3 {
		t.Errorf("整族 importance 应改写为 1, got %d 条 ==1", c1)
	}

	// 归档应进入 CORE 项目
	var coreCount int
	db.Get(&coreCount, `SELECT COUNT(*) FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id
		WHERE p.project_code = ? AND fv.checksum IN ('OVER1','OVER2','OVER3')`,
		PersonalCoreProjectCode)
	if coreCount != 3 {
		t.Errorf("应在 CORE 项目下有 3 条 fv, got %d", coreCount)
	}
}

// importanceToProjectCode 单元
func TestImportanceToProjectCode(t *testing.T) {
	cases := []struct {
		level int
		want  string
	}{
		{1, PersonalCoreProjectCode},
		{2, PersonalImportantProjectCode},
		{3, PersonalGeneralProjectCode},
		{4, ""},
		{0, ""},
		{5, ""},
	}
	for _, c := range cases {
		got := importanceToProjectCode(c.level)
		if got != c.want {
			t.Errorf("importanceToProjectCode(%d) = %q, want %q", c.level, got, c.want)
		}
	}
}
