package repository

import (
	"strconv"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// seedFamilyWithMembers 测试用：建一个 family 和 N 个 member resources
func seedFamilyWithMembers(t *testing.T, db *sqlx.DB, memberCount int) (familyID int64, resourceIDs []int64) {
	t.Helper()
	now := time.Now()

	famRes, err := db.Exec(`INSERT INTO data_resource_family
		(primary_content_sign, member_count, algorithm, highest_score, create_time, update_time, disable)
		VALUES (?, ?, 'test', 1.0, ?, ?, 0)`,
		"FAM-PRIMARY-SIGN", memberCount, now, now)
	if err != nil {
		t.Fatal(err)
	}
	familyID, _ = famRes.LastInsertId()

	for i := 0; i < memberCount; i++ {
		sign := "FAM-MEMBER-" + string(rune('A'+i))
		res, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, family_id, family_relation, family_score,
			create_time, update_time, disable
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, ?, ?, ?, 0)`,
			sign, now, "member-"+string(rune('A'+i))+".docx",
			familyID, "same_content", 0.95, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		resourceIDs = append(resourceIDs, id)
	}
	return familyID, resourceIDs
}

// seedSimpleProject 测试用：建一个最小可用项目 + 1 个 stage + 1 个 rule
func seedSimpleProjectWithRule(t *testing.T, db *sqlx.DB, projCode, stageCode, ruleCode string) (projectID, stageID int64) {
	t.Helper()
	now := time.Now()

	// subjects (复用)
	var subjID int64
	if err := db.Get(&subjID, `SELECT id FROM subjects WHERE code = ?`, "TEST-SUBJ"); err != nil {
		r, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
			VALUES ('TEST-SUBJ', '测试主体', 'department', 'active', ?, ?, 0)`, now, now)
		subjID, _ = r.LastInsertId()
	}

	// 模版 + stage + rule（test_template 不带 family 桥接的全部 schema，这里裸 SQL 建）
	var tplID int64
	if err := db.Get(&tplID, `SELECT id FROM data_templates WHERE template_code = ?`, "TPL-TEST-FAM"); err != nil {
		r, _ := db.Exec(`INSERT INTO data_templates (
			template_code, template_name, template_version, status, project_sensitivity_level,
			cached_at, create_time, update_time, disable
		) VALUES ('TPL-TEST-FAM', '家族测试模版', 'V1.0', 'active', 'general', ?, ?, ?, 0)`, now, now, now)
		tplID, _ = r.LastInsertId()
	}
	var tplStageID int64
	if err := db.Get(&tplStageID, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ?`, tplID, stageCode); err != nil {
		r, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
			VALUES (?, ?, '测试环节', 'process', 1, ?, ?, ?, 0)`, tplID, stageCode, now, now, now)
		tplStageID, _ = r.LastInsertId()
	}
	db.Exec(`INSERT OR IGNORE INTO template_file_rules (
		template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types,
		cached_at, create_time, update_time, disable
	) VALUES (?, ?, '测试规则', 'output', 0, '["*"]', ?, ?, ?, 0)`, tplStageID, ruleCode, now, now, now)

	// 项目
	r, err := db.Exec(`INSERT INTO data_projects (
		project_code, project_name, template_code, template_version, sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id, status, create_time, update_time, disable
	) VALUES (?, '家族测试项目', 'TPL-TEST-FAM', 'V1.0', 'general', 'independent', ?, ?, ?, 'active', ?, ?, 0)`,
		projCode, subjID, subjID, subjID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ = r.LastInsertId()
	r2, _ := db.Exec(`INSERT INTO project_stages (
		project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order, status, create_time, update_time, disable
	) VALUES (?, ?, ?, '测试环节', 'process', 1, 'pending', ?, ?, 0)`, projectID, tplStageID, stageCode, now, now)
	stageID, _ = r2.LastInsertId()
	return
}

// V4-Q5 family 批量归档：所有 member 挂账，每个生成独立 fv + ledger
func TestBridgeFamilyToProject_BulkArchive(t *testing.T) {
	db := openTestDB(t)
	familyID, resourceIDs := seedFamilyWithMembers(t, db, 3)
	projectID, _ := seedSimpleProjectWithRule(t, db, "P-FAM-TEST", "ST1", "OUT-FAM")

	result, err := BridgeFamilyToProject(db, familyID, projectID, "ST1", "OUT-FAM")
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 {
		t.Errorf("total = %d, want 3", result.Total)
	}
	if result.Archived != 3 {
		t.Errorf("archived = %d, want 3", result.Archived)
	}
	if result.SkippedAlready != 0 || result.Errors != 0 {
		t.Errorf("应当 0 跳过 0 错误，got skipped=%d errors=%d", result.SkippedAlready, result.Errors)
	}

	// 每个 member 应有 fv + ledger + register 事件
	for _, rid := range resourceIDs {
		fvCode := "P-FAM-TEST-ST1-OUT-FAM-DR" + strconv.FormatInt(rid, 10)
		var fvID int64
		if err := db.Get(&fvID, `SELECT id FROM file_versions WHERE file_version_code = ?`, fvCode); err != nil {
			t.Errorf("resource %d 应有 fv %s: %v", rid, fvCode, err)
			continue
		}
		var lc, ec int
		db.Get(&lc, `SELECT COUNT(*) FROM asset_ledgers WHERE file_version_id = ?`, fvID)
		db.Get(&ec, `SELECT COUNT(*) FROM lifecycle_events WHERE file_version_id = ? AND event_type = 'register'`, fvID)
		if lc != 1 || ec != 1 {
			t.Errorf("resource %d: ledger=%d events=%d (期望 1/1)", rid, lc, ec)
		}
	}
}

// V4-Q5 部分 member 已挂账时跳过 already
func TestBridgeFamilyToProject_PartialAlreadyArchived(t *testing.T) {
	db := openTestDB(t)
	familyID, resourceIDs := seedFamilyWithMembers(t, db, 3)
	projectID, _ := seedSimpleProjectWithRule(t, db, "P-FAM-PARTIAL", "ST1", "OUT-FAM")

	// 第一次：全归
	first, err := BridgeFamilyToProject(db, familyID, projectID, "ST1", "OUT-FAM")
	if err != nil {
		t.Fatal(err)
	}
	if first.Archived != 3 {
		t.Fatalf("第一次应归档 3 个, got %d", first.Archived)
	}

	// 第二次：应当全跳
	second, err := BridgeFamilyToProject(db, familyID, projectID, "ST1", "OUT-FAM")
	if err != nil {
		t.Fatal(err)
	}
	if second.Archived != 0 {
		t.Errorf("第二次不应有新归档, got archived=%d", second.Archived)
	}
	if second.SkippedAlready != 3 {
		t.Errorf("第二次应全部跳过 3, got %d", second.SkippedAlready)
	}

	// member 数量不变
	for _, rid := range resourceIDs {
		fvCode := "P-FAM-PARTIAL-ST1-OUT-FAM-DR" + strconv.FormatInt(rid, 10)
		var n int
		db.Get(&n, `SELECT COUNT(*) FROM file_versions WHERE file_version_code = ?`, fvCode)
		if n != 1 {
			t.Errorf("resource %d 应只有 1 个 fv, got %d", rid, n)
		}
	}
}

// V4-Q5 空 family / 不存在 family 报错
func TestBridgeFamilyToProject_FamilyMissing(t *testing.T) {
	db := openTestDB(t)
	projectID, _ := seedSimpleProjectWithRule(t, db, "P-FAM-EMPTY", "ST1", "OUT-FAM")
	_, err := BridgeFamilyToProject(db, 99999, projectID, "ST1", "OUT-FAM")
	if err == nil {
		t.Error("不存在的 family 应报错")
	}
}

// V4-Q5 项目不存在报错
func TestBridgeFamilyToProject_ProjectMissing(t *testing.T) {
	db := openTestDB(t)
	familyID, _ := seedFamilyWithMembers(t, db, 1)
	_, err := BridgeFamilyToProject(db, familyID, 99999, "ST1", "OUT-FAM")
	if err == nil {
		t.Error("不存在的 project 应报错")
	}
}

// V4-Q5 archived/cancelled 项目拒绝归档
func TestBridgeFamilyToProject_RejectClosedProject(t *testing.T) {
	db := openTestDB(t)
	familyID, _ := seedFamilyWithMembers(t, db, 2)
	projectID, _ := seedSimpleProjectWithRule(t, db, "P-FAM-CLOSED", "ST1", "OUT-FAM")
	db.Exec(`UPDATE data_projects SET status = 'archived' WHERE id = ?`, projectID)
	_, err := BridgeFamilyToProject(db, familyID, projectID, "ST1", "OUT-FAM")
	if err == nil {
		t.Error("archived 项目应当拒")
	}
}

// V4-Q5 stage / rule 不存在报错
func TestBridgeFamilyToProject_StageMissing(t *testing.T) {
	db := openTestDB(t)
	familyID, _ := seedFamilyWithMembers(t, db, 1)
	projectID, _ := seedSimpleProjectWithRule(t, db, "P-FAM-NOSTAGE", "ST1", "OUT-FAM")
	_, err := BridgeFamilyToProject(db, familyID, projectID, "NONEXISTENT", "OUT-FAM")
	if err == nil {
		t.Error("不存在的 stage 应报错")
	}
	_, err = BridgeFamilyToProject(db, familyID, projectID, "ST1", "NONEXISTENT")
	if err == nil {
		t.Error("不存在的 file_rule 应报错")
	}
}

// seedFamilyWithDatedMembers 测试用：建一个 family 和 N 个 member，按时间递增。
// 返回的 resourceIDs 与 contentSigns 按 first_create_time 升序（最后一个是最新）。
func seedFamilyWithDatedMembers(t *testing.T, db *sqlx.DB, contentSigns []string) (familyID int64, resourceIDs []int64) {
	t.Helper()
	now := time.Now()

	famRes, err := db.Exec(`INSERT INTO data_resource_family
		(primary_content_sign, member_count, algorithm, highest_score, create_time, update_time, disable)
		VALUES (?, ?, 'test', 1.0, ?, ?, 0)`,
		contentSigns[0], len(contentSigns), now, now)
	if err != nil {
		t.Fatal(err)
	}
	familyID, _ = famRes.LastInsertId()

	for i, sign := range contentSigns {
		ctime := now.Add(time.Duration(i) * time.Hour) // 严格递增，最后一个最新
		res, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, family_id, family_relation, family_score,
			create_time, update_time, disable
		) VALUES (?, 1, 1, ?, ?, 2, 2, ?, ?, ?, ?, ?, 0)`,
			sign, ctime, "member-"+sign+".docx",
			familyID, "same_content", 0.95, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		resourceIDs = append(resourceIDs, id)
	}
	return familyID, resourceIDs
}

// V5-P1 Task9: family 归档过程/定稿分流 —— 最新 → final，其余 → process
func TestBridgeFamily_SplitProcessAndFinal(t *testing.T) {
	db := openTestDB(t)

	// 建 1 个项目 + 2 个 stage + 2 个 rule（process / final）
	now := time.Now()
	var subjID int64
	r, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('TEST-SUBJ-SPLIT', '测试主体', 'department', 'active', ?, ?, 0)`, now, now)
	subjID, _ = r.LastInsertId()

	rt, _ := db.Exec(`INSERT INTO data_templates (
		template_code, template_name, template_version, status, project_sensitivity_level,
		cached_at, create_time, update_time, disable
	) VALUES ('TPL-SPLIT', '分流测试模版', 'V1.0', 'active', 'general', ?, ?, ?, 0)`, now, now, now)
	tplID, _ := rt.LastInsertId()
	rs1, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-DRAFT', '起草', 'process', 1, ?, ?, ?, 0)`, tplID, now, now, now)
	tplStage1, _ := rs1.LastInsertId()
	rs2, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-FINAL', '定稿', 'record', 2, ?, ?, ?, 0)`, tplID, now, now, now)
	tplStage2, _ := rs2.LastInsertId()
	db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'PRC-001', '过程版本', 'process', 0, '["*"]', ?, ?, ?, 0)`, tplStage1, now, now, now)
	db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'OUT-001', '归档定稿', 'output', 0, '["*"]', ?, ?, ?, 0)`, tplStage2, now, now, now)

	rp, _ := db.Exec(`INSERT INTO data_projects (
		project_code, project_name, template_code, template_version, sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id, status, create_time, update_time, disable
	) VALUES ('P-SPLIT-1', '分流测试项目', 'TPL-SPLIT', 'V1.0', 'general', 'independent', ?, ?, ?, 'active', ?, ?, 0)`,
		subjID, subjID, subjID, now, now)
	projectID, _ := rp.LastInsertId()
	db.Exec(`INSERT INTO project_stages (project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order, status, create_time, update_time, disable)
		VALUES (?, ?, 'GR-DRAFT', '起草', 'process', 1, 'pending', ?, ?, 0)`, projectID, tplStage1, now, now)
	db.Exec(`INSERT INTO project_stages (project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order, status, create_time, update_time, disable)
		VALUES (?, ?, 'GR-FINAL', '定稿', 'record', 2, 'pending', ?, ?, 0)`, projectID, tplStage2, now, now)

	// 3 个 family member，first_create_time 递增（FAM3 最新）
	familyID, ids := seedFamilyWithDatedMembers(t, db, []string{"FAM1", "FAM2", "FAM3"})
	_ = ids

	result, err := BridgeFamilyToProjectSplit(db, familyID, projectID,
		"GR-DRAFT", "PRC-001", // process target
		"GR-FINAL", "OUT-001", // final target
	)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("Total 应=3, got %d", result.Total)
	}
	if result.Archived != 3 {
		t.Errorf("Archived 应=3, got %d (errors=%d details=%+v)", result.Archived, result.Errors, result.Details)
	}

	// 最新（FAM3）应归 GR-FINAL
	var finalCount int
	db.Get(&finalCount, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		WHERE ps.stage_code = 'GR-FINAL' AND fv.checksum = 'FAM3'`)
	if finalCount != 1 {
		t.Errorf("FAM3 (最新) 应归 GR-FINAL, got %d", finalCount)
	}

	// 较旧（FAM1, FAM2）应归 GR-DRAFT
	var draftCount int
	db.Get(&draftCount, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		WHERE ps.stage_code = 'GR-DRAFT' AND fv.checksum IN ('FAM1','FAM2')`)
	if draftCount != 2 {
		t.Errorf("FAM1+FAM2 应归 GR-DRAFT, got %d", draftCount)
	}
}

// V5-P1 Task9: 同 target 两次调用幂等
func TestBridgeFamily_SplitIdempotent(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	r, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('TEST-SUBJ-IDEM', '测试主体', 'department', 'active', ?, ?, 0)`, now, now)
	subjID, _ := r.LastInsertId()
	rt, _ := db.Exec(`INSERT INTO data_templates (
		template_code, template_name, template_version, status, project_sensitivity_level,
		cached_at, create_time, update_time, disable
	) VALUES ('TPL-SPLIT-IDEM', '幂等', 'V1.0', 'active', 'general', ?, ?, ?, 0)`, now, now, now)
	tplID, _ := rt.LastInsertId()
	rs1, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-DRAFT', '起草', 'process', 1, ?, ?, ?, 0)`, tplID, now, now, now)
	tplStage1, _ := rs1.LastInsertId()
	rs2, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-FINAL', '定稿', 'record', 2, ?, ?, ?, 0)`, tplID, now, now, now)
	tplStage2, _ := rs2.LastInsertId()
	db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'PRC-001', '过程', 'process', 0, '["*"]', ?, ?, ?, 0)`, tplStage1, now, now, now)
	db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'OUT-001', '定稿', 'output', 0, '["*"]', ?, ?, ?, 0)`, tplStage2, now, now, now)
	rp, _ := db.Exec(`INSERT INTO data_projects (
		project_code, project_name, template_code, template_version, sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id, status, create_time, update_time, disable
	) VALUES ('P-IDEM-1', '幂等测试', 'TPL-SPLIT-IDEM', 'V1.0', 'general', 'independent', ?, ?, ?, 'active', ?, ?, 0)`,
		subjID, subjID, subjID, now, now)
	projectID, _ := rp.LastInsertId()
	db.Exec(`INSERT INTO project_stages (project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order, status, create_time, update_time, disable)
		VALUES (?, ?, 'GR-DRAFT', '起草', 'process', 1, 'pending', ?, ?, 0)`, projectID, tplStage1, now, now)
	db.Exec(`INSERT INTO project_stages (project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order, status, create_time, update_time, disable)
		VALUES (?, ?, 'GR-FINAL', '定稿', 'record', 2, 'pending', ?, ?, 0)`, projectID, tplStage2, now, now)

	familyID, _ := seedFamilyWithDatedMembers(t, db, []string{"IDEM1", "IDEM2"})
	first, err := BridgeFamilyToProjectSplit(db, familyID, projectID, "GR-DRAFT", "PRC-001", "GR-FINAL", "OUT-001")
	if err != nil || first.Archived != 2 {
		t.Fatalf("第一次应归 2, got %+v err=%v", first, err)
	}
	second, err := BridgeFamilyToProjectSplit(db, familyID, projectID, "GR-DRAFT", "PRC-001", "GR-FINAL", "OUT-001")
	if err != nil {
		t.Fatal(err)
	}
	if second.SkippedAlready != 2 || second.Archived != 0 {
		t.Errorf("第二次应全跳, got archived=%d skipped=%d", second.Archived, second.SkippedAlready)
	}
}

// V5-P1 Task9: family 不存在 → 报错
func TestBridgeFamily_Split_FamilyMissing(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	r, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('TEST-SUBJ-MISS', '测试主体', 'department', 'active', ?, ?, 0)`, now, now)
	subjID, _ := r.LastInsertId()
	rt, _ := db.Exec(`INSERT INTO data_templates (
		template_code, template_name, template_version, status, project_sensitivity_level,
		cached_at, create_time, update_time, disable
	) VALUES ('TPL-SPLIT-MISS', '缺', 'V1.0', 'active', 'general', ?, ?, ?, 0)`, now, now, now)
	tplID, _ := rt.LastInsertId()
	rs1, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-DRAFT', '起草', 'process', 1, ?, ?, ?, 0)`, tplID, now, now, now)
	tplStage1, _ := rs1.LastInsertId()
	rs2, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-FINAL', '定稿', 'record', 2, ?, ?, ?, 0)`, tplID, now, now, now)
	tplStage2, _ := rs2.LastInsertId()
	db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'PRC-001', '过程', 'process', 0, '["*"]', ?, ?, ?, 0)`, tplStage1, now, now, now)
	db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'OUT-001', '定稿', 'output', 0, '["*"]', ?, ?, ?, 0)`, tplStage2, now, now, now)
	rp, _ := db.Exec(`INSERT INTO data_projects (
		project_code, project_name, template_code, template_version, sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id, status, create_time, update_time, disable
	) VALUES ('P-MISS-1', '缺族测试', 'TPL-SPLIT-MISS', 'V1.0', 'general', 'independent', ?, ?, ?, 'active', ?, ?, 0)`,
		subjID, subjID, subjID, now, now)
	projectID, _ := rp.LastInsertId()
	db.Exec(`INSERT INTO project_stages (project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order, status, create_time, update_time, disable)
		VALUES (?, ?, 'GR-DRAFT', '起草', 'process', 1, 'pending', ?, ?, 0)`, projectID, tplStage1, now, now)
	db.Exec(`INSERT INTO project_stages (project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order, status, create_time, update_time, disable)
		VALUES (?, ?, 'GR-FINAL', '定稿', 'record', 2, 'pending', ?, ?, 0)`, projectID, tplStage2, now, now)

	if _, err := BridgeFamilyToProjectSplit(db, 999999, projectID, "GR-DRAFT", "PRC-001", "GR-FINAL", "OUT-001"); err == nil {
		t.Error("不存在的 family 应报错")
	}
}
