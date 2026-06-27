package repository

import (
	"testing"
	"time"

	"data-asset-scan-go/internal/models"
)

// V2 收尾：UpsertFromUserInfo 命中已有 users 行 → UPDATE
func TestUpsertFromUserInfo_UpdateExisting(t *testing.T) {
	db := openTestDB(t)
	userRepo := NewUserRepository(db)

	// 先建一个 users 行
	u, err := userRepo.Create(models.CreateUserInput{
		Username:    "alice",
		DisplayName: "Alice",
		CompanyName: "OldCo",
		Department:  "OldDept",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 模拟用户改名换部门
	phone := "13900000000"
	updated, err := userRepo.UpsertFromUserInfo(&models.UserInfo{
		UserName:    "alice",
		CompanyName: "NewCo",
		Department:  "NewDept",
		IP:          "192.168.1.10",
		MacAddress:  "AA:BB:CC:DD:EE:FF",
		Phone:       &phone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != u.ID {
		t.Errorf("应命中同一行 id=%d，got %d", u.ID, updated.ID)
	}
	if updated.CompanyName != "NewCo" || updated.Department != "NewDept" {
		t.Errorf("公司/部门未同步: %+v", updated)
	}
	if updated.IP != "192.168.1.10" || updated.MacAddress != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("IP/MAC 未同步: %+v", updated)
	}
	if updated.Phone == nil || *updated.Phone != phone {
		t.Errorf("phone 未同步: %v", updated.Phone)
	}
}

// V2 收尾：UpsertFromUserInfo 未命中 → INSERT
func TestUpsertFromUserInfo_InsertNew(t *testing.T) {
	db := openTestDB(t)
	userRepo := NewUserRepository(db)

	u, err := userRepo.UpsertFromUserInfo(&models.UserInfo{
		UserName:    "newcomer",
		CompanyName: "Co",
		Department:  "Dept",
		IP:          "10.0.0.1",
		MacAddress:  "11:22:33:44:55:66",
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 {
		t.Error("应当 INSERT 新行并返回 id")
	}
	if u.Status != "active" || u.Disable != 0 {
		t.Errorf("默认应为 active/0，got status=%s disable=%d", u.Status, u.Disable)
	}
	if u.DisplayName != "newcomer" {
		t.Errorf("display_name 默认应回退到 username，got %s", u.DisplayName)
	}
}

// V2 收尾：UpsertFromUserInfo 入参校验
func TestUpsertFromUserInfo_Invalid(t *testing.T) {
	db := openTestDB(t)
	userRepo := NewUserRepository(db)

	if _, err := userRepo.UpsertFromUserInfo(nil); err == nil {
		t.Error("nil 入参应报错")
	}
	if _, err := userRepo.UpsertFromUserInfo(&models.UserInfo{UserName: ""}); err == nil {
		t.Error("缺 user_name 应报错")
	}
}

// V2 收尾：重复 Upsert 幂等（连续两次 UPDATE 不冲突）
func TestUpsertFromUserInfo_Idempotent(t *testing.T) {
	db := openTestDB(t)
	userRepo := NewUserRepository(db)
	ui := &models.UserInfo{UserName: "bob", CompanyName: "Co", Department: "D"}

	u1, err := userRepo.UpsertFromUserInfo(ui)
	if err != nil {
		t.Fatal(err)
	}
	// 等一毫秒，确保 update_time 至少有差
	time.Sleep(2 * time.Millisecond)
	u2, err := userRepo.UpsertFromUserInfo(ui)
	if err != nil {
		t.Fatal(err)
	}
	if u1.ID != u2.ID {
		t.Errorf("幂等性破坏：第一次 id=%d 第二次 id=%d", u1.ID, u2.ID)
	}
}

// 组建团队 v2：从 manage 同步的用户须把 role 落库（INSERT 与 UPDATE 两条路径）
func TestUpsertManagedAuthUser_PersistsRole(t *testing.T) {
	db := openTestDB(t)
	userRepo := NewUserRepository(db)

	// INSERT 路径
	created, err := userRepo.UpsertManagedAuthUser(ManagedAuthUser{
		Username:       "carol",
		DisplayName:    "Carol",
		UserUnit:       "一院",
		UserDepartment: "收集科",
		Role:           "管理员",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Role != "管理员" {
		t.Errorf("INSERT 后 role 应为 管理员，got %q", created.Role)
	}

	// UPDATE 路径（同 username 再次同步，角色变化）
	updated, err := userRepo.UpsertManagedAuthUser(ManagedAuthUser{
		Username:       "carol",
		DisplayName:    "Carol",
		UserUnit:       "一院",
		UserDepartment: "审核科",
		Role:           "普通成员",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID {
		t.Errorf("应命中同一行 id=%d，got %d", created.ID, updated.ID)
	}
	if updated.Role != "普通成员" {
		t.Errorf("UPDATE 后 role 应为 普通成员，got %q", updated.Role)
	}

	// List 也应带出 role
	list, err := userRepo.List()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, u := range list {
		if u.Username == "carol" {
			found = true
			if u.Role != "普通成员" {
				t.Errorf("List 中 carol.role 应为 普通成员，got %q", u.Role)
			}
		}
	}
	if !found {
		t.Error("List 未包含 carol")
	}
}
