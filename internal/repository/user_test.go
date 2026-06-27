package repository

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/models"
)

// V2-1: users 表 CRUD
func TestUser_CRUD(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserRepository(db)

	phone := "13800000000"
	created, err := repo.Create(models.CreateUserInput{
		Username:    "zhangsan",
		DisplayName: "张三",
		CompanyName: "测试公司",
		Department:  "技术部",
		Phone:       &phone,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("created.ID should be > 0")
	}
	if created.Username != "zhangsan" || created.DisplayName != "张三" {
		t.Errorf("created fields mismatch: %+v", created)
	}
	if created.Status != "active" {
		t.Errorf("default status should be active, got %s", created.Status)
	}

	// FindByID
	got, err := repo.FindByID(created.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v / %+v", err, got)
	}

	// FindByUsername
	byName, err := repo.FindByUsername("zhangsan")
	if err != nil || byName == nil || byName.ID != created.ID {
		t.Fatalf("FindByUsername: %v / %+v", err, byName)
	}

	// 不存在
	none, err := repo.FindByUsername("not-exist")
	if err != nil || none != nil {
		t.Errorf("expected nil, got err=%v, user=%v", err, none)
	}

	// Update display_name
	newName := "张三-更新"
	updated, err := repo.Update(created.ID, models.UpdateUserInput{DisplayName: &newName})
	if err != nil || updated.DisplayName != "张三-更新" {
		t.Errorf("update display_name: %v / %+v", err, updated)
	}

	// SoftDelete
	if err := repo.SoftDelete(created.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	again, _ := repo.FindByID(created.ID)
	if again != nil {
		t.Errorf("after soft delete, FindByID should return nil")
	}
}

// V2-1: username UNIQUE 约束
func TestUser_UsernameUnique(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserRepository(db)

	if _, err := repo.Create(models.CreateUserInput{Username: "dup", DisplayName: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(models.CreateUserInput{Username: "dup", DisplayName: "B"}); err == nil {
		t.Error("duplicate username should fail")
	}
}

// V2-1: GetActiveUser 返回最新一条
func TestUser_GetActiveUser(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserRepository(db)

	// 空表
	none, err := repo.GetActiveUser()
	if err != nil || none != nil {
		t.Errorf("empty table should return nil, got %v", none)
	}

	repo.Create(models.CreateUserInput{Username: "a", DisplayName: "A"})
	time.Sleep(10 * time.Millisecond)
	b, _ := repo.Create(models.CreateUserInput{Username: "b", DisplayName: "B"})

	active, err := repo.GetActiveUser()
	if err != nil || active == nil {
		t.Fatal("GetActiveUser empty")
	}
	if active.ID != b.ID {
		t.Errorf("GetActiveUser should return latest (b.id=%d), got %d", b.ID, active.ID)
	}
}

// V2-1: user_info → users 数据迁移幂等
func TestUser_MigrateFromUserInfo_Idempotent(t *testing.T) {
	db := openTestDB(t)

	// 直接往 user_info 插一条（模拟 V1 用户）
	now := time.Now()
	phone := "13900000000"
	if _, err := db.Exec(`INSERT INTO user_info
		(company_name, user_name, department, ip, mac_address, work_address, phone, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"V1 公司", "v1user", "V1 部门", "127.0.0.1", "00:00:00:00:00:00", "", phone, now, now); err != nil {
		t.Fatal(err)
	}

	// 跑迁移
	if err := migrateUserInfoToUsers(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewUserRepository(db)
	u, err := repo.FindByUsername("v1user")
	if err != nil || u == nil {
		t.Fatalf("after migrate, should find v1user: %v / %+v", err, u)
	}
	if u.DisplayName != "v1user" || u.CompanyName != "V1 公司" || u.Department != "V1 部门" {
		t.Errorf("migrated fields mismatch: %+v", u)
	}

	// 再跑一次：应当幂等，不报错也不重复插入
	if err := migrateUserInfoToUsers(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	all, _ := repo.List()
	count := 0
	for _, x := range all {
		if x.Username == "v1user" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("after 2nd migrate, v1user count should be 1, got %d", count)
	}
}

// V2-1: 必填字段校验
func TestUser_CreateRequiresFields(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserRepository(db)

	if _, err := repo.Create(models.CreateUserInput{Username: ""}); err == nil {
		t.Error("empty username should fail")
	}
	if _, err := repo.Create(models.CreateUserInput{Username: "x", DisplayName: ""}); err == nil {
		t.Error("empty display_name should fail")
	}
}
