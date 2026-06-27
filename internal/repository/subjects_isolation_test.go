package repository

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/models"
)

// V2-6: subjects 表保持独立，禁止被 V2 身份体系污染
//
// 设计上 subjects 表表示"三主体"——业务责任主体（归属/保管/安全），
// 可以是部门、组织、个人，是数据资产的责任归属。它与登录身份（users）
// 完全独立。V2-1/V2-2/V2-3 迁移只允许"读 subjects 反查到 users"，
// 严禁反向往 subjects 表注入 user_id 或修改 subject 数据。
//
// 这些 assertion 保护未来重构不要意外把两者耦合。

// V2-6: subjects 表 schema 不应含 user_id 列
func TestSubjectsIsolation_NoUserIDColumn(t *testing.T) {
	db := openTestDB(t)
	exists, err := columnExists(db, "subjects", "user_id")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("subjects 表禁止出现 user_id 列 —— V2 设计上身份与责任主体必须解耦")
	}
}

// V2-6: 项目立项时三主体写入 data_projects，subjects 表本身不被修改
func TestSubjectsIsolation_InstantiateDoesNotModifySubjects(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)

	// 快照 subjects 表
	type snapshot struct {
		ID         int64     `db:"id"`
		Code       string    `db:"code"`
		Name       string    `db:"name"`
		Type       string    `db:"type"`
		Status     string    `db:"status"`
		UpdateTime time.Time `db:"update_time"`
	}
	var before []snapshot
	if err := db.Select(&before, `SELECT id, code, name, type, status, update_time FROM subjects ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(before) != 3 {
		t.Fatalf("expected 3 seeded subjects, got %d", len(before))
	}

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "u1", DisplayName: "U1"})

	svc := NewProjectInstantiationService(db)
	if _, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "三主体隔离测试",
		ObjectShortCode:    "ISO",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		CreatedByUserID:    u.ID,
		Activate:           true,
	}); err != nil {
		t.Fatal(err)
	}

	// 再次快照，应当与 before 完全一致（subjects 没动）
	var after []snapshot
	if err := db.Select(&after, `SELECT id, code, name, type, status, update_time FROM subjects ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("subjects 行数变化: %d → %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("subject id=%d 被修改: %+v → %+v", before[i].ID, before[i], after[i])
		}
	}
}

// V2-6: 三主体字段在 data_projects 与 asset_ledgers 中都仍是 subject_id（不是 user_id）
func TestSubjectsIsolation_ThreeSubjectFieldsAreSubjectIDs(t *testing.T) {
	db := openTestDB(t)
	// data_projects 三主体列
	for _, col := range []string{"owner_subject_id", "custodian_subject_id", "security_subject_id"} {
		ok, err := columnExists(db, "data_projects", col)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("data_projects.%s 缺失（三主体责任字段必须保留）", col)
		}
	}
	// asset_ledgers 同上
	for _, col := range []string{"owner_subject_id", "custodian_subject_id", "security_subject_id"} {
		ok, err := columnExists(db, "asset_ledgers", col)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("asset_ledgers.%s 缺失", col)
		}
	}

	// 反过来：data_projects / asset_ledgers 不应出现 *_user_id 形式的"主体"字段
	// （审计字段如 created_by_user_id 是允许的；这里检查的是不该有 owner_user_id 之类）
	for _, badCol := range []string{"owner_user_id", "custodian_user_id", "security_user_id"} {
		ok, _ := columnExists(db, "data_projects", badCol)
		if ok {
			t.Errorf("data_projects 不应出现 %s 列：三主体是责任归属，不是登录身份", badCol)
		}
		ok2, _ := columnExists(db, "asset_ledgers", badCol)
		if ok2 {
			t.Errorf("asset_ledgers 不应出现 %s 列", badCol)
		}
	}
}
