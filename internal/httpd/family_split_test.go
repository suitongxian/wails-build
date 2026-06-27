package httpd

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/repository"
)

// seedPersonalProjectsV2ForFamilySplit V5-P1 Task9 测试用：
// seed TPL-PERSONAL-FILES V2.0 双环节（GR-DRAFT/PRC-001 process + GR-FINAL/OUT-001 output）。
// 配合 repository.EnsurePersonalContextForTest 让 3 个内置项目带这两 stage。
func seedPersonalProjectsV2ForFamilySplit(t *testing.T, db *sqlx.DB) {
	t.Helper()
	now := time.Now()
	res, err := db.Exec(`INSERT INTO data_templates (
		template_code, template_name, template_version, status, project_sensitivity_level,
		cached_at, create_time, update_time, disable
	) VALUES ('TPL-PERSONAL-FILES', '个人文件项目化管理模版', 'V2.0', 'active', 'general', ?, ?, ?, 0)`,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	tplID, _ := res.LastInsertId()
	draftRes, err := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-DRAFT', '个人文件起草与修改', 'process', 1, ?, ?, ?, 0)`, tplID, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	draftID, _ := draftRes.LastInsertId()
	finalRes, err := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
		VALUES (?, 'GR-FINAL', '个人文件定稿', 'record', 2, ?, ?, ?, 0)`, tplID, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	finalID, _ := finalRes.LastInsertId()
	if _, err := db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'PRC-001', '过程版本', 'process', 0, '["*"]', ?, ?, ?, 0)`, draftID, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
		VALUES (?, 'OUT-001', '归档定稿', 'output', 0, '["*"]', ?, ?, ?, 0)`, finalID, now, now, now); err != nil {
		t.Fatal(err)
	}
}

// seedFamilyWithDatedResources 在 httpd 包测试里建一个 family + N member。
// first_create_time 按 contentSigns 顺序递增，最后一个最新。
func seedFamilyWithDatedResources(t *testing.T, db *sqlx.DB, contentSigns []string) (familyID int64, resourceIDs []int64) {
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
		ctime := now.Add(time.Duration(i) * time.Hour)
		res, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, family_id, family_relation, family_score,
			create_time, update_time, disable
		) VALUES (?, 1, 1, ?, ?, 2, 2, ?, 'same_content', 0.95, ?, ?, 0)`,
			sign, ctime, "member-"+sign+".docx", familyID, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		resourceIDs = append(resourceIDs, id)
	}
	return familyID, resourceIDs
}

func seedRegisteredFamilyUser(t *testing.T, db *sqlx.DB, username string) int64 {
	t.Helper()
	// 幂等：若 withActiveUser 已经 mirror 同名 user，直接复用现有 id
	var existing int64
	if err := db.Get(&existing, `SELECT id FROM users WHERE username = ? AND disable = 0`, username); err == nil && existing > 0 {
		return existing
	}
	now := time.Now()
	res, err := db.Exec(`INSERT INTO users (
		username, display_name, company_name, department, ip, mac_address,
		work_address, phone, status, create_time, update_time, disable
	) VALUES (?, ?, '测试单位', '测试部门', '', '', '', '', 'active', ?, ?, 0)`,
		username, username, now, now)
	if err != nil {
		t.Fatalf("insert registered user: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// V5-P1 Task 9: HTTP 端点 family 批量归档 - 分流模式
// (process_stage + final_stage 都给 → 最新走 final，其余走 process)
func TestHTTP_FamilyBatchArchive_SplitMode(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredFamilyUser(t, db, "u1")

	seedPersonalProjectsV2ForFamilySplit(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	familyID, _ := seedFamilyWithDatedResources(t, db, []string{"HTFAM1", "HTFAM2"})

	var importantID int64
	if err := db.Get(&importantID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalImportantProjectCode); err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReq(t, r, "POST", fmt.Sprintf("/resources/families/%d/batch-archive", familyID), map[string]interface{}{
		"project_id":           importantID,
		"stage_code":           "GR-DRAFT",
		"file_rule_code":       "PRC-001",
		"final_stage_code":     "GR-FINAL",
		"final_file_rule_code": "OUT-001",
	})
	successOk(t, status, resp)

	d := dataMap(t, resp)
	if int(d["total"].(float64)) != 2 {
		t.Errorf("total 应=2, got %v", d["total"])
	}
	if int(d["archived"].(float64)) != 2 {
		t.Errorf("archived 应=2, got %v", d["archived"])
	}

	// 最新（HTFAM2）应在 GR-FINAL；HTFAM1 在 GR-DRAFT
	var finalCnt, draftCnt int
	db.Get(&finalCnt, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		WHERE ps.stage_code = 'GR-FINAL' AND fv.checksum = 'HTFAM2'`)
	db.Get(&draftCnt, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		WHERE ps.stage_code = 'GR-DRAFT' AND fv.checksum = 'HTFAM1'`)
	if finalCnt != 1 {
		t.Errorf("HTFAM2 最新应归 GR-FINAL, got %d", finalCnt)
	}
	if draftCnt != 1 {
		t.Errorf("HTFAM1 较旧应归 GR-DRAFT, got %d", draftCnt)
	}
}

// V5-P1 Task 9: HTTP 端点 - 不传 final_*，仍走旧的单目标模式（兼容性回归）
func TestHTTP_FamilyBatchArchive_LegacySingleMode(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredFamilyUser(t, db, "u1")

	seedPersonalProjectsV2ForFamilySplit(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	familyID, _ := seedFamilyWithDatedResources(t, db, []string{"LEG1", "LEG2"})

	var importantID int64
	if err := db.Get(&importantID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalImportantProjectCode); err != nil {
		t.Fatal(err)
	}

	// 仅给 stage_code + file_rule_code，不带 final_*
	status, resp := jsonReq(t, r, "POST", fmt.Sprintf("/resources/families/%d/batch-archive", familyID), map[string]interface{}{
		"project_id":     importantID,
		"stage_code":     "GR-FINAL",
		"file_rule_code": "OUT-001",
	})
	successOk(t, status, resp)

	d := dataMap(t, resp)
	if int(d["archived"].(float64)) != 2 {
		t.Errorf("archived 应=2, got %v", d["archived"])
	}

	// 两个 member 都应在 GR-FINAL（单目标）
	var finalCnt int
	db.Get(&finalCnt, `SELECT COUNT(*) FROM file_versions fv
		JOIN project_stages ps ON ps.id = fv.project_stage_id
		WHERE ps.stage_code = 'GR-FINAL' AND fv.checksum IN ('LEG1','LEG2')`)
	if finalCnt != 2 {
		t.Errorf("legacy 模式 2 个 member 都应在 GR-FINAL, got %d", finalCnt)
	}
}

func TestHTTP_FamilyBatchArchive_RequiresTargetStagePermission(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, stages := seedTemplateAndProject(t, db)
	familyID, _ := seedFamilyWithDatedResources(t, db, []string{"DENY1", "DENY2"})

	now := time.Now()
	res, err := db.Exec(`INSERT INTO users (
		username, display_name, company_name, department, ip, mac_address,
		work_address, phone, status, create_time, update_time, disable
	) VALUES ('lisi', '李四', '第一研究院', '排版部', '', '', '', '', 'active', ?, ?, 0)`, now, now)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	lisiID, _ := res.LastInsertId()

	pbStageID := stageIDByCodeForHTTP(t, stages, "MZ-PB")
	if _, err := db.Exec(`INSERT INTO project_members (
		project_id, user_id, subject_id, role_code, stage_ids, permission_actions,
		create_time, update_time, disable
	) VALUES (?, ?, 0, '排版员', ?, '["write"]', ?, ?, 0)`,
		pj.ID, lisiID, fmt.Sprintf("[%d]", pbStageID), now, now); err != nil {
		t.Fatalf("insert project member: %v", err)
	}

	currentAuthSession.Lock()
	currentAuthSession.session = &authSession{
		Token: "token-lisi",
		User: authUser{
			ID:             lisiID,
			Username:       "lisi",
			DisplayName:    "李四",
			UserUnit:       "第一研究院",
			UserDepartment: "排版部",
			Status:         "active",
		},
	}
	currentAuthSession.Unlock()
	defer func() {
		currentAuthSession.Lock()
		currentAuthSession.session = nil
		currentAuthSession.Unlock()
	}()

	status, resp := jsonReq(t, r, "POST", fmt.Sprintf("/resources/families/%d/batch-archive", familyID), map[string]interface{}{
		"project_id":     pj.ID,
		"stage_code":     "MZ-SH",
		"file_rule_code": "IN-001",
	})
	if status != http.StatusForbidden || resp["code"] != "PERMISSION_DENIED" {
		t.Fatalf("batch archive should require write permission in target stage, status=%d resp=%+v", status, resp)
	}
}

// V5-P1 Task 9: HTTP 端点 - final_stage 不存在 → 业务报错（success=false）
func TestHTTP_FamilyBatchArchive_SplitInvalidFinalStage(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredFamilyUser(t, db, "u1")

	seedPersonalProjectsV2ForFamilySplit(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	familyID, _ := seedFamilyWithDatedResources(t, db, []string{"BAD1", "BAD2"})

	var importantID int64
	db.Get(&importantID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalImportantProjectCode)

	status, resp := jsonReq(t, r, "POST", fmt.Sprintf("/resources/families/%d/batch-archive", familyID), map[string]interface{}{
		"project_id":           importantID,
		"stage_code":           "GR-DRAFT",
		"file_rule_code":       "PRC-001",
		"final_stage_code":     "NONEXISTENT",
		"final_file_rule_code": "OUT-001",
	})
	expectFailure(t, status, resp)
}
