package repository

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/models"
)

// V2-4: 审计 user_id 列在 fresh DB 上存在
func TestAuditUserID_ColumnsExist(t *testing.T) {
	db := openTestDB(t)
	cases := []struct{ Table, Col string }{
		{"data_projects", "created_by_user_id"},
		{"file_versions", "created_by_user_id"},
		{"file_versions", "submitted_by_user_id"},
		{"lifecycle_events", "operator_user_id"},
	}
	for _, c := range cases {
		ok, err := columnExists(db, c.Table, c.Col)
		if err != nil {
			t.Fatalf("%s.%s: %v", c.Table, c.Col, err)
		}
		if !ok {
			t.Errorf("%s.%s should exist after migrations", c.Table, c.Col)
		}
	}
}

// V2-4: 写入路径同时落 V1 (TEXT) 与 V2 (user_id)
func TestAuditUserID_WriteBothColumns(t *testing.T) {
	db := openTestDB(t)
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "alice", DisplayName: "Alice"})

	// data_projects
	pRepo := NewDataProjectRepository(db)
	createdBy := "alice"
	uid := u.ID
	tx, _ := db.Beginx()
	pid, err := pRepo.Insert(tx, CreateDataProjectInput{
		ProjectCode:        "P-AUDIT-1",
		ProjectName:        "审计测试",
		TemplateCode:       "TPL",
		TemplateVersion:    "V1",
		SensitivityLevel:   "important",
		ManagementMode:     "independent",
		OwnerSubjectID:     1,
		CustodianSubjectID: 1,
		SecuritySubjectID:  1,
		Status:             "draft",
		CreatedBy:          &createdBy,
		CreatedByUserID:    &uid,
	})
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	var pUserID *int64
	var pUserName *string
	db.Get(&pUserID, `SELECT created_by_user_id FROM data_projects WHERE id = ?`, pid)
	db.Get(&pUserName, `SELECT created_by FROM data_projects WHERE id = ?`, pid)
	if pUserID == nil || *pUserID != u.ID {
		t.Errorf("data_projects.created_by_user_id should be %d, got %v", u.ID, pUserID)
	}
	if pUserName == nil || *pUserName != "alice" {
		t.Errorf("data_projects.created_by should be 'alice', got %v", pUserName)
	}

	// file_versions
	fvRepo := NewFileVersionRepository(db)
	tx2, _ := db.Beginx()
	fvID, err := fvRepo.Insert(tx2, CreateFileVersionInput{
		ProjectID:       pid,
		ProjectStageID:  1,
		FileVersionCode: "P-AUDIT-1-S1-F1",
		LocalCode:       "F1",
		DisplayName:     "f",
		DataState:       "input",
		VersionNo:       "V1.0",
		LifecycleStatus: "planned",
		CreatedBy:       &createdBy,
		CreatedByUserID: &uid,
	})
	if err != nil {
		t.Fatal(err)
	}
	tx2.Commit()
	var fvUID *int64
	db.Get(&fvUID, `SELECT created_by_user_id FROM file_versions WHERE id = ?`, fvID)
	if fvUID == nil || *fvUID != u.ID {
		t.Errorf("file_versions.created_by_user_id mismatch: %v", fvUID)
	}

	// lifecycle_events
	leRepo := NewLifecycleEventRepository(db)
	tx3, _ := db.Beginx()
	op := "alice"
	leID, err := leRepo.Append(tx3, AppendEventInput{
		FileVersionID:  fvID,
		EventType:      EventRegister,
		EventName:      "入账",
		OperatorID:     &op,
		OperatorUserID: &uid,
	})
	if err != nil {
		t.Fatal(err)
	}
	tx3.Commit()
	var leUID *int64
	db.Get(&leUID, `SELECT operator_user_id FROM lifecycle_events WHERE id = ?`, leID)
	if leUID == nil || *leUID != u.ID {
		t.Errorf("lifecycle_events.operator_user_id mismatch: %v", leUID)
	}
}

// V2-4: V1 路径（仅 TEXT 字段，user_id 留 NULL）仍工作
func TestAuditUserID_V1OnlyStillWorks(t *testing.T) {
	db := openTestDB(t)
	pRepo := NewDataProjectRepository(db)
	createdBy := "legacy_user"
	tx, _ := db.Beginx()
	pid, err := pRepo.Insert(tx, CreateDataProjectInput{
		ProjectCode:        "P-LEGACY-1",
		ProjectName:        "V1 路径",
		TemplateCode:       "TPL",
		TemplateVersion:    "V1",
		SensitivityLevel:   "important",
		ManagementMode:     "independent",
		OwnerSubjectID:     1,
		CustodianSubjectID: 1,
		SecuritySubjectID:  1,
		Status:             "draft",
		CreatedBy:          &createdBy,
		// CreatedByUserID 故意不传
	})
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	var uid *int64
	db.Get(&uid, `SELECT created_by_user_id FROM data_projects WHERE id = ?`, pid)
	if uid != nil {
		t.Errorf("V1 path should leave created_by_user_id NULL, got %v", *uid)
	}
}

// V2-4: 迁移把 V1 TEXT 字段反查回填 user_id
func TestAuditUserID_MigrationBackfill(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	// 建对应 user
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "bob", DisplayName: "Bob"})

	// 直接 raw insert：created_by='bob'，created_by_user_id=NULL（模拟 V1 老数据）
	db.Exec(`INSERT INTO data_projects (
		project_code, project_name, template_code, template_version,
		sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id,
		status, created_by, create_time, update_time, disable
	) VALUES ('P-OLD-1', '老项目', 'TPL', 'V1', 'important', 'independent', 1, 1, 1, 'draft', 'bob', ?, ?, 0)`, now, now)

	// 验证回填前是 NULL
	var beforeUID *int64
	db.Get(&beforeUID, `SELECT created_by_user_id FROM data_projects WHERE project_code = 'P-OLD-1'`)
	if beforeUID != nil {
		t.Fatalf("created_by_user_id should be NULL before migration, got %v", *beforeUID)
	}

	// 跑迁移
	if err := migrateAuditUserRef(db); err != nil {
		t.Fatal(err)
	}

	var afterUID *int64
	db.Get(&afterUID, `SELECT created_by_user_id FROM data_projects WHERE project_code = 'P-OLD-1'`)
	if afterUID == nil || *afterUID != u.ID {
		t.Errorf("after backfill, created_by_user_id should be %d, got %v", u.ID, afterUID)
	}
}

// V2-4: 迁移幂等
func TestAuditUserID_MigrationIdempotent(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "carol", DisplayName: "Carol"})

	db.Exec(`INSERT INTO data_projects (
		project_code, project_name, template_code, template_version,
		sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id,
		status, created_by, create_time, update_time, disable
	) VALUES ('P-IDEM-1', '幂等', 'TPL', 'V1', 'important', 'independent', 1, 1, 1, 'draft', 'carol', ?, ?, 0)`, now, now)

	migrateAuditUserRef(db)
	migrateAuditUserRef(db) // 二次
	migrateAuditUserRef(db) // 三次

	var uid *int64
	db.Get(&uid, `SELECT created_by_user_id FROM data_projects WHERE project_code = 'P-IDEM-1'`)
	if uid == nil || *uid != u.ID {
		t.Errorf("idempotent migration should set created_by_user_id=%d, got %v", u.ID, uid)
	}
}

// V2-4: 找不到对应 user 时保留 NULL（不报错）
func TestAuditUserID_MigrationSkipsMissingUser(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	// created_by='ghost' 但 users 里没有 'ghost'
	db.Exec(`INSERT INTO data_projects (
		project_code, project_name, template_code, template_version,
		sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id,
		status, created_by, create_time, update_time, disable
	) VALUES ('P-GHOST-1', '幽灵', 'TPL', 'V1', 'important', 'independent', 1, 1, 1, 'draft', 'ghost', ?, ?, 0)`, now, now)

	if err := migrateAuditUserRef(db); err != nil {
		t.Fatal(err)
	}
	var uid *int64
	db.Get(&uid, `SELECT created_by_user_id FROM data_projects WHERE project_code = 'P-GHOST-1'`)
	if uid != nil {
		t.Errorf("missing user should leave NULL, got %v", *uid)
	}
}

// V2-4: SubmitOutput 提供 operatorUserID 时落 submitted_by_user_id
func TestAuditUserID_SubmitOutputV2(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "submitter", DisplayName: "Submitter"})

	svc := NewProjectInstantiationService(db)
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "提交审计",
		ObjectShortCode:    "SUB",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		CreatedByUserID:    u.ID,
		Activate:           true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 找一个 output fv，先 register（mock 上传），再 SubmitOutput V2
	var outFvID int64
	if err := db.Get(&outFvID, `SELECT id FROM file_versions
		WHERE project_id = ? AND data_state = 'output' AND lifecycle_status = 'planned' LIMIT 1`,
		out.Project.ID); err != nil {
		t.Skipf("test template has no output fv: %v", err)
	}
	db.Exec(`UPDATE file_versions SET lifecycle_status = 'registered' WHERE id = ?`, outFvID)

	fileSvc := NewFileOperationService(db)
	if _, err := fileSvc.SubmitOutput(outFvID, "submitter", u.ID); err != nil {
		t.Fatal(err)
	}

	var submittedUID *int64
	db.Get(&submittedUID, `SELECT submitted_by_user_id FROM file_versions WHERE id = ?`, outFvID)
	if submittedUID == nil || *submittedUID != u.ID {
		t.Errorf("submitted_by_user_id should be %d, got %v", u.ID, submittedUID)
	}

	// 对应 event 也应当带 operator_user_id
	var evUID *int64
	db.Get(&evUID, `SELECT operator_user_id FROM lifecycle_events
		WHERE file_version_id = ? AND event_name = '提交产出' ORDER BY id DESC LIMIT 1`, outFvID)
	if evUID == nil || *evUID != u.ID {
		t.Errorf("lifecycle event operator_user_id should be %d, got %v", u.ID, evUID)
	}
}
