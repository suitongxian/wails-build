package repository

import (
	"strings"
	"testing"

	"data-asset-scan-go/internal/models"
)

// V2-5: V2 路径 — username → users.id → project_members.user_id 含 action → 放行
func TestProjectAuthV2_ByUserPath(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)

	// 注册用户 alice
	userRepo := NewUserRepository(db)
	alice, _ := userRepo.Create(models.CreateUserInput{Username: "alice", DisplayName: "Alice"})

	// 加 alice 为项目成员（user_id 路径），权限 read/write
	memberRepo := NewProjectMemberRepository(db)
	tx, _ := db.Beginx()
	uid := alice.ID
	memberRepo.Insert(tx, CreateProjectMemberInput{
		ProjectID:         project.ID,
		UserID:            &uid,
		SubjectID:         0,
		RoleCode:          "PM",
		PermissionActions: `["read","write"]`,
	})
	tx.Commit()

	authSvc := NewProjectAuthService(db)

	// 用 username 走 V2 路径
	if err := authSvc.CheckProjectAction("alice", project.ID, "write"); err != nil {
		t.Errorf("alice 应允许 write: %v", err)
	}
	if err := authSvc.CheckProjectAction("alice", project.ID, "destroy"); err == nil {
		t.Error("alice 不应有 destroy")
	}
}

// V2-5: 直接用 user_id 校验（V2 接口）
func TestProjectAuthV2_ByUserID(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	userRepo := NewUserRepository(db)
	bob, _ := userRepo.Create(models.CreateUserInput{Username: "bob", DisplayName: "Bob"})
	memberRepo := NewProjectMemberRepository(db)
	tx, _ := db.Beginx()
	uid := bob.ID
	memberRepo.Insert(tx, CreateProjectMemberInput{
		ProjectID:         project.ID,
		UserID:            &uid,
		SubjectID:         0,
		RoleCode:          "PM",
		PermissionActions: `["read","submit","close"]`,
	})
	tx.Commit()

	authSvc := NewProjectAuthService(db)
	if err := authSvc.CheckProjectActionByUserID(bob.ID, project.ID, "close"); err != nil {
		t.Errorf("bob close 应允许: %v", err)
	}
	if err := authSvc.CheckProjectActionByUserID(bob.ID, project.ID, "write"); err == nil {
		t.Error("bob 不应有 write")
	}
	// 不是该项目成员的 user_id 必拒
	if err := authSvc.CheckProjectActionByUserID(99999, project.ID, "read"); err == nil {
		t.Error("非成员 user_id 必拒")
	}
	if err := authSvc.CheckProjectActionByUserID(0, project.ID, "read"); err == nil {
		t.Error("user_id=0 必拒")
	}
}

// V2-5: user 在 users 表存在但不是项目成员 → 回退到 subject 路径；都查不到 → 拒
func TestProjectAuthV2_UserExistsButNotMember(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	userRepo := NewUserRepository(db)
	_, _ = userRepo.Create(models.CreateUserInput{Username: "stranger", DisplayName: "Stranger"})

	authSvc := NewProjectAuthService(db)
	err := authSvc.CheckProjectAction("stranger", project.ID, "write")
	if err == nil {
		t.Fatal("V2 找到 user 但非项目成员，subject 路径也无映射 → 必拒")
	}
	if !IsPermissionDenied(err) {
		t.Errorf("expected PermissionDeniedError, got %T", err)
	}
	if !strings.Contains(err.Error(), "stranger") {
		t.Errorf("error should mention 'stranger', got: %v", err)
	}
}

// V2-5: V1 subject 路径仍然工作（兼容老立项数据：member 只有 subject_id）
func TestProjectAuthV2_LegacySubjectPathStillWorks(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	authSvc := NewProjectAuthService(db)

	// owner subject 在 setupProjectForFileOps 里已是成员，且默认 [read,write,submit,close]
	var ownerCode string
	db.Get(&ownerCode, `SELECT code FROM subjects WHERE id = ?`, project.OwnerSubjectID)

	if err := authSvc.CheckProjectAction(ownerCode, project.ID, "close"); err != nil {
		t.Errorf("V1 subject 路径应仍允许: %v", err)
	}
}
