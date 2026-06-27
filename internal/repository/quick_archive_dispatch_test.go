package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// 归档上下文解析：从项目 id 拿到编码/名称/敏感级 + 模版 scope；并按 scope 分流。
func TestGetProjectArchiveContext_AndDispatch(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	// person 范围、核心级模版
	tpl, err := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "person", TemplateName: "个人计划", SensitivityLevel: "core"})
	if err != nil {
		t.Fatal(err)
	}
	projRepo := NewDataProjectRepository(db)
	pid, err := projRepo.Insert(db, CreateDataProjectInput{
		ProjectCode: "甲项目-XM-2026-0009", ProjectName: "甲项目",
		TemplateID: &tpl.ID, TemplateCode: tpl.TemplateCode, TemplateVersion: tpl.TemplateVersion,
		SensitivityLevel: "core", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, err := GetProjectArchiveContext(db, pid)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Scope != "person" || ctx.Sensitivity != "core" || ctx.ProjectName != "甲项目" {
		t.Fatalf("上下文不符：%+v", ctx)
	}

	// 工作空间放一个过程文件，按 scope=person 分流到工作空间下的个人核心文件夹（默认落点=工作空间）
	root := t.TempDir()
	ws := NewProjectWorkspace(root)
	if _, err := ws.CreateTaskDir(ctx.ProjectCode, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(ctx.ProjectCode, "S1", "TK1", "process"), "稿.txt"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ArchiveProjectByScope(db, root, ctx.ProjectCode, ctx.ProjectName, ctx.Scope, ctx.Sensitivity, "tester", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Route != RouteLocal || res.Archived != 1 {
		t.Fatalf("person 应本地归档 1，实得 route=%v archived=%d", res.Route, res.Archived)
	}
	if _, err := os.Stat(filepath.Join(root, "个人核心文件夹", "甲项目", "稿.txt")); err != nil {
		t.Fatalf("核心级过程应入个人核心文件夹：%v", err)
	}
}

// 行业级项目（无文件）：定稿无柜室被丢弃、参考/过程无文件 → 归档 0。
func TestArchiveProjectByScope_IndustrySkipped(t *testing.T) {
	res, err := ArchiveProjectByScope(nil, t.TempDir(), "X-1", "行业项目", "industry", "core", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Archived != 0 {
		t.Fatalf("行业级无文件应归档 0，实得 archived=%d", res.Archived)
	}
}
