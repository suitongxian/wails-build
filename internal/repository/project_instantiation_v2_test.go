package repository

import (
	"strings"
	"testing"

	"data-asset-scan-go/internal/models"
)

// V2-3: CreatedByUserID 提供时，立项人自动加入 project_members 为项目负责人
func TestInstantiate_AutoEnrollCreatedByUser(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)

	// 建立项 user
	userRepo := NewUserRepository(db)
	user, _ := userRepo.Create(models.CreateUserInput{Username: "creator", DisplayName: "立项人"})

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "V2 自动入会测试",
		ObjectShortCode:    "V2-AUTO",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		CreatedByUserID:    user.ID,
		Activate:           true,
		// 故意不传 Members
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	// 立项人应当作为项目负责人自动加进 project_members
	memberRepo := NewProjectMemberRepository(db)
	list, _ := memberRepo.FindByUserInProject(out.Project.ID, user.ID)
	if len(list) != 1 {
		t.Fatalf("expected 1 auto-enrolled member, got %d", len(list))
	}
	m := list[0]
	if m.RoleCode != "项目负责人" {
		t.Errorf("role should be 项目负责人, got %s", m.RoleCode)
	}
	if !strings.Contains(m.PermissionActions, "close") {
		t.Errorf("auto-enrolled member should have close permission, got %s", m.PermissionActions)
	}
	for _, p := range []string{"read", "write", "submit", "receive", "archive"} {
		if !strings.Contains(m.PermissionActions, `"`+p+`"`) {
			t.Errorf("auto-enrolled member should have %s permission, got %s", p, m.PermissionActions)
		}
	}
}

// V2-3: CreatedByUserID 已在 Members 列表中时不重复 enroll
func TestInstantiate_NoDuplicateEnroll(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)
	userRepo := NewUserRepository(db)
	user, _ := userRepo.Create(models.CreateUserInput{Username: "u2", DisplayName: "立项人 2"})

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	uid := user.ID
	svc := NewProjectInstantiationService(db)
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "V2 不重复入会测试",
		ObjectShortCode:    "V2-DUP",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		CreatedByUserID:    user.ID,
		Members: []MemberInput{
			// 用户已显式作为成员（用 user_id）
			{UserID: &uid, RoleCode: "PM", PermissionActions: []string{"read", "write", "close"}},
		},
		Activate: true,
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	list, _ := NewProjectMemberRepository(db).FindByUserInProject(out.Project.ID, user.ID)
	if len(list) != 1 {
		t.Errorf("user already in Members list, should NOT be duplicated; got %d records", len(list))
	}
	// 用户已显式提供的应当被尊重（PM 角色 + 用户自己选的权限）
	if list[0].RoleCode != "PM" {
		t.Errorf("user-supplied role 'PM' should be preserved, got %s", list[0].RoleCode)
	}
}

func TestInstantiate_AllowsMultipleUserMembersWithSameRole(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)
	userRepo := NewUserRepository(db)
	creator, _ := userRepo.Create(models.CreateUserInput{Username: "creator-multi", DisplayName: "立项人"})
	u1, _ := userRepo.Create(models.CreateUserInput{Username: "designer-a", DisplayName: "设计师甲"})
	u2, _ := userRepo.Create(models.CreateUserInput{Username: "designer-b", DisplayName: "设计师乙"})

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	uid1 := u1.ID
	uid2 := u2.ID
	svc := NewProjectInstantiationService(db)
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "V2 多用户同角色测试",
		ObjectShortCode:    "V2-MULTI",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		CreatedByUserID:    creator.ID,
		Members: []MemberInput{
			{UserID: &uid1, RoleCode: "设计师", PermissionActions: []string{"read", "write"}},
			{UserID: &uid2, RoleCode: "设计师", PermissionActions: []string{"read", "write"}},
		},
		Activate: true,
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}

	for _, uid := range []int64{u1.ID, u2.ID} {
		list, _ := NewProjectMemberRepository(db).FindByUserInProject(out.Project.ID, uid)
		if len(list) != 1 {
			t.Fatalf("user %d should have 1 project member record, got %d", uid, len(list))
		}
		if list[0].RoleCode != "设计师" {
			t.Fatalf("user %d role = %s, want 设计师", uid, list[0].RoleCode)
		}
	}
}

// V2-3: V1 路径仍工作（CreatedByUserID=0，要求 Members 里有 close）
func TestInstantiate_V1PathStillWorks(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)

	// 不传 CreatedByUserID，但 Members 里有 close → 应当成功
	_, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "V1 路径测试",
		ObjectShortCode:    "V1-PATH",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Members: []MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "close"}},
		},
		Activate: true,
	})
	if err != nil {
		t.Fatalf("V1 path with subject_id + close should work: %v", err)
	}
}

// V2-3: 既无 CreatedByUserID 也无 close 成员 → 拒绝
func TestInstantiate_RejectNoCloseAuthority(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)
	_, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "无 close 测试",
		ObjectShortCode:    "NC",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Members: []MemberInput{
			{SubjectID: owner, RoleCode: "X", PermissionActions: []string{"read"}},
		},
		// CreatedByUserID=0，又没有 close → 拒
	})
	if err == nil {
		t.Fatal("should reject when neither CreatedByUserID nor close-authorized member is provided")
	}
	if !strings.Contains(err.Error(), "close") {
		t.Errorf("error should mention 'close', got: %v", err)
	}
}
