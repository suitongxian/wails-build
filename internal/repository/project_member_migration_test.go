package repository

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/models"
)

// V2-2: project_members.user_id 列在 fresh DB 上存在
func TestProjectMember_UserIDColumnExists(t *testing.T) {
	db := openTestDB(t)
	exists, err := columnExists(db, "project_members", "user_id")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("project_members.user_id column should exist after migrations")
	}
}

// V2-2: 新代码用 UserID 创建 member
func TestProjectMember_InsertWithUserID(t *testing.T) {
	db := openTestDB(t)

	// 先建 user
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "u1", DisplayName: "用户一"})

	// project_members 不依赖 data_projects 外键约束（V1 设计），可以直接 insert
	memberRepo := NewProjectMemberRepository(db)
	tx, _ := db.Beginx()
	userID := u.ID
	id, err := memberRepo.Insert(tx, CreateProjectMemberInput{
		ProjectID:         999, // 测试场景：不需要真实 project，只验插入
		UserID:            &userID,
		SubjectID:         0,
		RoleCode:          "PM",
		PermissionActions: `["read","write","close"]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	// 用新的 FindByUserInProject 应当找到
	list, err := memberRepo.FindByUserInProject(999, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Errorf("FindByUserInProject should return 1 member, got %d", len(list))
	}
	if list[0].UserID == nil || *list[0].UserID != u.ID {
		t.Errorf("member.UserID mismatch: %v", list[0].UserID)
	}
}

// V2-2: 迁移把现有 subject_id 反查映射到 user_id
func TestProjectMember_MigrateSubjectToUser(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	// 建 1 个 person 类型 subject "张三"
	res, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('S-1', '张三', 'person', 'active', ?, ?, 0)`, now, now)
	subjID, _ := res.LastInsertId()

	// 1 个 department 类型 subject "印刷部"
	res2, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('S-2', '印刷部', 'department', 'active', ?, ?, 0)`, now, now)
	deptSubjID, _ := res2.LastInsertId()

	// 建对应的 user，display_name='张三'
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "zs", DisplayName: "张三"})

	// 直接插 2 条 project_members：一条对应 person subject，一条对应 dept subject
	db.Exec(`INSERT INTO project_members (project_id, user_id, subject_id, role_code, stage_ids, permission_actions, create_time, update_time, disable)
		VALUES (1, NULL, ?, 'PM', '[]', '["read"]', ?, ?, 0)`, subjID, now, now)
	db.Exec(`INSERT INTO project_members (project_id, user_id, subject_id, role_code, stage_ids, permission_actions, create_time, update_time, disable)
		VALUES (1, NULL, ?, 'OWNER', '[]', '["read"]', ?, ?, 0)`, deptSubjID, now, now)

	// 跑迁移
	if err := migrateProjectMembersUserRef(db); err != nil {
		t.Fatal(err)
	}

	// 验证：person subject 那条 user_id 应回填为 u.ID；dept 那条仍 NULL
	var personUserID *int64
	var deptUserID *int64
	db.Get(&personUserID, `SELECT user_id FROM project_members WHERE subject_id = ?`, subjID)
	db.Get(&deptUserID, `SELECT user_id FROM project_members WHERE subject_id = ?`, deptSubjID)
	if personUserID == nil || *personUserID != u.ID {
		t.Errorf("person subject member should map to user %d, got %v", u.ID, personUserID)
	}
	if deptUserID != nil {
		t.Errorf("dept subject member should NOT map (type != person), got user_id=%v", *deptUserID)
	}
}

// V2-2: 迁移幂等
func TestProjectMember_MigrateIdempotent(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	res, _ := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('S-X', '李四', 'person', 'active', ?, ?, 0)`, now, now)
	subjID, _ := res.LastInsertId()
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "ls", DisplayName: "李四"})
	db.Exec(`INSERT INTO project_members (project_id, user_id, subject_id, role_code, stage_ids, permission_actions, create_time, update_time, disable)
		VALUES (1, NULL, ?, 'PM', '[]', '["read"]', ?, ?, 0)`, subjID, now, now)

	migrateProjectMembersUserRef(db)
	migrateProjectMembersUserRef(db) // 再来一次

	// 仍然只有 1 条，且 user_id 正确
	var n int
	db.Get(&n, `SELECT COUNT(*) FROM project_members WHERE subject_id = ?`, subjID)
	if n != 1 {
		t.Errorf("expected 1 member, got %d", n)
	}
	var uid *int64
	db.Get(&uid, `SELECT user_id FROM project_members WHERE subject_id = ?`, subjID)
	if uid == nil || *uid != u.ID {
		t.Errorf("user_id should remain %d, got %v", u.ID, uid)
	}
}

// V2-2: V1 SubjectID 入参兼容（保证 setupProjectForFileOps 旧路径不挂）
func TestProjectMember_LegacySubjectIDStillWorks(t *testing.T) {
	db := openTestDB(t)
	memberRepo := NewProjectMemberRepository(db)
	tx, _ := db.Beginx()
	_, err := memberRepo.Insert(tx, CreateProjectMemberInput{
		ProjectID:         100,
		SubjectID:         50,
		RoleCode:          "X",
		PermissionActions: `["read"]`,
	})
	if err != nil {
		t.Fatalf("legacy SubjectID-only insert should still work: %v", err)
	}
	tx.Commit()

	// FindBySubjectInProject 仍能找
	list, _ := memberRepo.FindBySubjectInProject(100, 50)
	if len(list) != 1 {
		t.Errorf("FindBySubjectInProject should work, got %d", len(list))
	}
}
