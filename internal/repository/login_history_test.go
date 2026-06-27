package repository

import "testing"

// 登录历史：按 username 去重保存(含密码，供本机快速登录自动填充)，按最近登录倒序列出。
func TestLoginHistory_UpsertAndList(t *testing.T) {
	db := openTestDB(t)
	repo := NewLoginHistoryRepository(db)

	if err := repo.Upsert(LoginHistoryEntry{
		Username: "wangsz", Password: "pw1", DisplayName: "王司长",
		UserUnit: "第一研究院", UserDepartment: "编辑部", ManageEndpoint: "http://m", LastLoginAt: "2026-01-01 09:00:00",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(LoginHistoryEntry{
		Username: "zhang", Password: "pw2", DisplayName: "张主任", LastLoginAt: "2026-02-01 09:00:00",
	}); err != nil {
		t.Fatal(err)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("应有 2 条历史，实得 %d", len(list))
	}
	// 最近登录(zhang 2026-02)在前
	if list[0].Username != "zhang" || list[1].Username != "wangsz" {
		t.Fatalf("应按最近登录倒序，实得 %s,%s", list[0].Username, list[1].Username)
	}
	if list[1].Password != "pw1" || list[1].DisplayName != "王司长" {
		t.Fatalf("应保存密码与显示名，实得 %+v", list[1])
	}

	// 同账号再次登录 → 不新增、更新密码与最近时间
	if err := repo.Upsert(LoginHistoryEntry{Username: "wangsz", Password: "pw1-new", DisplayName: "王司长", LastLoginAt: "2026-03-01 09:00:00"}); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.List()
	if len(list) != 2 {
		t.Fatalf("同账号应去重，仍 2 条，实得 %d", len(list))
	}
	if list[0].Username != "wangsz" || list[0].Password != "pw1-new" {
		t.Fatalf("同账号应更新密码并置顶，实得 %+v", list[0])
	}
}

// 空 LastLoginAt 时自动用当前时间，不报错。
func TestLoginHistory_UpsertDefaultsTime(t *testing.T) {
	db := openTestDB(t)
	repo := NewLoginHistoryRepository(db)
	if err := repo.Upsert(LoginHistoryEntry{Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.List()
	if len(list) != 1 || list[0].LastLoginAt == "" {
		t.Fatalf("应自动填充 last_login_at，实得 %+v", list)
	}
}
