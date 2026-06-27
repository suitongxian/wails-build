package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAutoArchiveStage 验证主路径自动归档：环节产出目录文件 → 按规则定级 → 挂账到个人容器，幂等。
func TestAutoArchiveStage(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	repo := NewTemplateAuthoringRepository(db)

	// 本地模版（核心级项目）+ 一个环节 + 一个 output 核心标识
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷计划", SensitivityLevel: "core"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "排版"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "排版加工", SensitivityLevel: "core"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "排版完成稿", DataState: "output", AllowedFileTypes: "PDF"})

	// 工作空间 + 在该环节 output 目录放一个产出文件
	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	outDir := filepath.Join(root, "项目文件管理", tpl.TemplateCode, "stages", st.StageCode, "output")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "婚姻法-排版定稿-V1.pdf"), []byte("dummy content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 自动归档
	res, err := AutoArchiveStage(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatalf("自动归档失败: %v", err)
	}
	if res.Archived != 1 {
		t.Fatalf("应归档 1 个文件，实得 %d（errors=%v）", res.Archived, res.Errors)
	}

	// 该文件应挂在「个人-核心」容器下，且有底账
	var projectCode string
	if err := db.Get(&projectCode, `SELECT p.project_code FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id
		JOIN data_resources dr ON dr.content_sign = fv.checksum
		WHERE dr.resources_name = ? LIMIT 1`, "婚姻法-排版定稿-V1.pdf"); err != nil {
		t.Fatalf("查归档落点失败: %v", err)
	}
	if projectCode != PersonalCoreProjectCode {
		t.Fatalf("核心级定稿应挂到 %s，实得 %s", PersonalCoreProjectCode, projectCode)
	}
	var ledgerN int
	db.Get(&ledgerN, `SELECT COUNT(*) FROM asset_ledgers al
		JOIN file_versions fv ON fv.id = al.file_version_id
		JOIN data_resources dr ON dr.content_sign = fv.checksum WHERE dr.resources_name = ?`, "婚姻法-排版定稿-V1.pdf")
	if ledgerN < 1 {
		t.Fatalf("应生成底账，实得 %d", ledgerN)
	}

	// 幂等：再次归档同文件 → 跳过，不重复
	res2, err := AutoArchiveStage(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatalf("二次归档失败: %v", err)
	}
	if res2.Archived != 0 || res2.Skipped != 1 {
		t.Fatalf("二次应跳过（archived=0 skipped=1），实得 archived=%d skipped=%d", res2.Archived, res2.Skipped)
	}
}

// TestAutoArchiveStage_StageMaxLevel 验证环节级就高不就低：定稿(过程文件拷贝,同内容)
// 与过程源同 content_sign → 单资产、落在环节最高密级容器，不被低级压低。
func TestAutoArchiveStage_StageMaxLevel(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	repo := NewTemplateAuthoringRepository(db)

	// 项目一般级；环节内有 output 核心标识 + process 一般标识 → 环节最高=核心
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "就高测试", SensitivityLevel: "general"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "加工"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "加工任务", SensitivityLevel: "general"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "成品", DataState: "output", AllowedFileTypes: "PDF", SensitivityLevel: "core"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "草稿", DataState: "process", AllowedFileTypes: "PDF", SensitivityLevel: "general"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	procDir := ws.StageStateDir(tpl.TemplateCode, st.StageCode, "process")
	outDir := ws.StageStateDir(tpl.TemplateCode, st.StageCode, "output")
	os.MkdirAll(procDir, 0o755)
	os.MkdirAll(outDir, 0o755)
	// 同一份内容既在 process(草稿) 又在 output(定稿=拷贝)
	content := []byte("identical deliverable bytes")
	if err := os.WriteFile(filepath.Join(procDir, "草稿-V1.pdf"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "成品.pdf"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := AutoArchiveStage(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatalf("自动归档失败: %v", err)
	}
	// 同内容 → 单资产：一个归档、一个幂等跳过
	if res.Archived != 1 || res.Skipped != 1 {
		t.Fatalf("同内容应 archived=1 skipped=1，实得 archived=%d skipped=%d", res.Archived, res.Skipped)
	}
	// 落点：环节最高=核心 → 个人-核心容器
	var projectCode string
	if err := db.Get(&projectCode, `SELECT p.project_code FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id
		JOIN data_resources dr ON dr.content_sign = fv.checksum
		WHERE dr.resources_name IN ('草稿-V1.pdf','成品.pdf') LIMIT 1`); err != nil {
		t.Fatalf("查落点失败: %v", err)
	}
	if projectCode != PersonalCoreProjectCode {
		t.Fatalf("环节含核心标识，过程草稿也应就高到 %s，实得 %s", PersonalCoreProjectCode, projectCode)
	}
}

// TestAutoArchiveAllWorkspace 验证工作空间巡检：不依赖「完成」，遍历整个工作空间把各环节产出自动入账。
// 这是消除"用户不点完成→文件游离管外"盲区的关键。
func TestAutoArchiveAllWorkspace(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	repo := NewTemplateAuthoringRepository(db)

	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "巡检测试", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "加工"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "加工任务", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "加工稿", DataState: "output", AllowedFileTypes: "PDF"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	outDir := filepath.Join(root, "项目文件管理", tpl.TemplateCode, "stages", st.StageCode, "output")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "巡检产出-V1.pdf"), []byte("sweep content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 残留一个无对应模版的目录，巡检应跳过不报错
	if err := os.MkdirAll(filepath.Join(root, "项目文件管理", "STALE-PROJ", "stages", "STG-X", "output"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 未点「完成」，仅巡检即应入账
	res, err := AutoArchiveAllWorkspace(db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if res.Archived != 1 {
		t.Fatalf("巡检应归档 1 个文件，实得 %d（errors=%v）", res.Archived, res.Errors)
	}

	// 落点：重要级 → 个人-重要容器
	var projectCode string
	if err := db.Get(&projectCode, `SELECT p.project_code FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id
		JOIN data_resources dr ON dr.content_sign = fv.checksum
		WHERE dr.resources_name = ? LIMIT 1`, "巡检产出-V1.pdf"); err != nil {
		t.Fatalf("查归档落点失败: %v", err)
	}
	if projectCode != PersonalImportantProjectCode {
		t.Fatalf("重要级产出应挂到 %s，实得 %s", PersonalImportantProjectCode, projectCode)
	}

	// 幂等：再巡检 → 跳过
	res2, _ := AutoArchiveAllWorkspace(db)
	if res2.Archived != 0 || res2.Skipped != 1 {
		t.Fatalf("二次巡检应跳过（archived=0 skipped=1），实得 archived=%d skipped=%d", res2.Archived, res2.Skipped)
	}
}
