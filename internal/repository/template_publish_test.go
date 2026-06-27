package repository

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 模版「另存为本地裁剪」相关规则（2026-06-02）：
//   - 本地模版不允许保存为「行业模版」（scope=industry），只能 单位/部门/个人；
//   - 新增「是否发布」状态（is_published），新建/另存为默认未发布；
//   - 只有已发布的模版才能用来立项（PushTemplateToManage 在联网前拦截）。

// TestLocalTemplateScopeForbidsIndustry 本地模版不能是行业模版；留空默认单位。
func TestLocalTemplateScopeForbidsIndustry(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	// 显式选行业 → 拒绝
	if _, err := repo.CreateLocalTemplate(CreateTemplateInput{TemplateName: "行业", Scope: "industry"}); err == nil {
		t.Fatal("本地模版不应允许保存为行业模版（scope=industry）")
	}
	// 留空 → 默认单位
	tpl, err := repo.CreateLocalTemplate(CreateTemplateInput{TemplateName: "默认归类"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if tpl.Scope != "unit" {
		t.Fatalf("空 scope 应默认 unit，实得 %q", tpl.Scope)
	}
	// 部门/个人 → ok
	if _, err := repo.CreateLocalTemplate(CreateTemplateInput{TemplateName: "部门", Scope: "department"}); err != nil {
		t.Fatalf("部门模版应允许: %v", err)
	}
	if _, err := repo.CreateLocalTemplate(CreateTemplateInput{TemplateName: "个人", Scope: "person"}); err != nil {
		t.Fatalf("个人模版应允许: %v", err)
	}
	// 更新为行业 → 同样拒绝
	if err := repo.UpdateLocalTemplate(tpl.ID, CreateTemplateInput{TemplateName: "改行业", Scope: "industry"}); err == nil {
		t.Fatal("更新也不应允许 industry")
	}
}

// TestSetTemplatePublished 发布/取消发布切换。
func TestSetTemplatePublished(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	tpl, err := repo.CreateLocalTemplate(CreateTemplateInput{TemplateName: "待发布", Scope: "unit"})
	if err != nil {
		t.Fatal(err)
	}
	if tpl.IsPublished != 0 {
		t.Fatalf("新建应默认未发布(0)，实得 %d", tpl.IsPublished)
	}

	if err := repo.SetTemplatePublished(tpl.ID, true); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	got, _ := repo.GetLocalTemplate(tpl.ID)
	if got.IsPublished != 1 {
		t.Fatalf("发布后应为 1，实得 %d", got.IsPublished)
	}

	if err := repo.SetTemplatePublished(tpl.ID, false); err != nil {
		t.Fatalf("取消发布失败: %v", err)
	}
	got, _ = repo.GetLocalTemplate(tpl.ID)
	if got.IsPublished != 0 {
		t.Fatalf("取消发布后应为 0，实得 %d", got.IsPublished)
	}
}

// TestInitiateRequiresPublished 未发布的模版不能立项（联网前即拦截）。
func TestInitiateRequiresPublished(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{TemplateName: "未发布立项", Scope: "unit"})
	// endpoint 给个不可达地址：若未被发布闸门拦截会走到网络；用例期望在联网前就报「未发布」
	_, err := repo.PushTemplateToManage(nil, "http://manage.invalid.local", tpl.ID, true, "operator")
	if err == nil {
		t.Fatal("未发布不应允许立项")
	}
	if !strings.Contains(err.Error(), "未发布") {
		t.Fatalf("错误应提示未发布，实得 %v", err)
	}

	// 发布后再立项：闸门通过（之后才会因不可达 endpoint 失败，不应再是「未发布」）
	_ = repo.SetTemplatePublished(tpl.ID, true)
	_, err2 := repo.PushTemplateToManage(nil, "http://manage.invalid.local", tpl.ID, true, "operator")
	if err2 != nil && strings.Contains(err2.Error(), "未发布") {
		t.Fatalf("已发布不应再报未发布: %v", err2)
	}
}

// TestCloneRemapsIndustryScopeToUnit 从服务端「行业模版」另存为本地时，归类降级为单位且未发布。
func TestCloneRemapsIndustryScopeToUnit(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":0,"message":"","data":{"template":{"template_name":"通用印刷模版","scope":"industry","project_sensitivity_level":"general"},"stages":[]}}`)
	}))
	defer srv.Close()

	id, err := repo.CloneManageTemplateToLocal(srv.Client(), srv.URL, "TPL-IND-001")
	if err != nil {
		t.Fatalf("另存为失败: %v", err)
	}
	got, _ := repo.GetLocalTemplate(id)
	if got.Scope == "industry" {
		t.Fatal("另存为本地后不应仍是行业模版")
	}
	if got.Scope != "unit" {
		t.Fatalf("行业模版另存为应降为 unit，实得 %q", got.Scope)
	}
	if got.IsPublished != 0 {
		t.Fatalf("另存为后应为未发布(0)，实得 %d", got.IsPublished)
	}
	if got.Origin != "local" {
		t.Fatalf("另存为应为本地模版(origin=local)，实得 %q", got.Origin)
	}
}
