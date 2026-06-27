package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// 集中立项虚拟项目（CPA-{应用id}）下的"按模版建占位"与"挑定稿拷到 output"：
// 规则取自真实模版，文件目录落到 CPA 项目码。

func TestScaffoldStageProcessDocsForProject_WritesToCPADir(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "登记", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "收稿登记表", DataState: "process", AllowedFileTypes: "docx"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	projectCode := "CPA-999"
	if err := ws.CreateProjectTree(projectCode, []string{st.StageCode}); err != nil {
		t.Fatal(err)
	}

	created, err := ScaffoldStageProcessDocsForProject(db, tpl.TemplateCode, projectCode, st.StageCode)
	if err != nil {
		t.Fatalf("建占位失败: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("应建 2 个占位（process + output），实得 %d: %v", len(created), created)
	}
	// 五层落盘：落在 CPA 项目目录该文件任务的 process 下，而非真实模版目录
	proc := filepath.Join(ws.TaskStateDir(projectCode, st.StageCode, tk.TaskCode, "process"), "收稿登记表.docx")
	if fi, err := os.Stat(proc); err != nil || fi.Size() == 0 || !isPlaceholderFile(proc, fi.Size()) {
		t.Fatalf("CPA 任务 process(docx) 占位应为非空有效文件且被识别为占位: err=%v", err)
	}
	// 真实模版目录下不应有文件
	if _, err := os.Stat(filepath.Join(ws.TaskStateDir(tpl.TemplateCode, st.StageCode, tk.TaskCode, "process"), "收稿登记表.docx")); err == nil {
		t.Fatal("不应写到真实模版目录")
	}
	// output 也预建占位（三态全建）；pdf 占位非空但被识别为占位
	outPath := filepath.Join(ws.TaskStateDir(projectCode, st.StageCode, tk.TaskCode, "output"), "登记定稿.pdf")
	if fi, err := os.Stat(outPath); err != nil || fi.Size() == 0 || !isPlaceholderFile(outPath, fi.Size()) {
		t.Fatalf("output(pdf) 占位应为非空且被识别为占位: err=%v", err)
	}
}

// 项目目录树只建 stages/，不再建空占位的 metadata/、archive/。
func TestCreateProjectTree_OnlyStages(t *testing.T) {
	root := t.TempDir()
	ws := NewProjectWorkspace(root)
	if err := ws.CreateProjectTree("CPA-1", []string{"S1"}); err != nil {
		t.Fatalf("建目录树失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "项目文件管理", "CPA-1", "stages", "S1", "process")); err != nil {
		t.Fatalf("stages 目录应存在: %v", err)
	}
	for _, d := range []string{"metadata", "archive"} {
		if _, err := os.Stat(filepath.Join(root, "项目文件管理", "CPA-1", d)); err == nil {
			t.Fatalf("不应再创建 %s 目录", d)
		}
	}
}

// 集中立项交付后应自动归档：CPA 项目目录下的产出文件按模版密级挂账到个人文件夹。
func TestAutoArchiveStageForProject_ArchivesFromCPADir(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "core"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "排版"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "排版加工", SensitivityLevel: "core"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "排版完成稿", DataState: "output", AllowedFileTypes: "PDF"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	projectCode := "CPA-777"
	if err := ws.CreateProjectTree(projectCode, []string{st.StageCode}); err != nil {
		t.Fatal(err)
	}
	outDir := ws.StageStateDir(projectCode, st.StageCode, "output")
	_ = os.MkdirAll(outDir, 0o755)
	if err := os.WriteFile(filepath.Join(outDir, "婚姻法-排版定稿.pdf"), []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := AutoArchiveStageForProject(db, tpl.TemplateCode, projectCode, st.StageCode)
	if err != nil {
		t.Fatalf("自动归档失败: %v", err)
	}
	if res.Archived != 1 {
		t.Fatalf("应归档 1 个文件，实得 %d（errors=%v）", res.Archived, res.Errors)
	}
	var pc string
	if err := db.Get(&pc, `SELECT p.project_code FROM file_versions fv
		JOIN data_projects p ON p.id = fv.project_id
		JOIN data_resources dr ON dr.content_sign = fv.checksum
		WHERE dr.resources_name = ? LIMIT 1`, "婚姻法-排版定稿.pdf"); err != nil {
		t.Fatalf("查归档落点失败: %v", err)
	}
	if pc != PersonalCoreProjectCode {
		t.Fatalf("核心级产出应挂到 %s，实得 %s", PersonalCoreProjectCode, pc)
	}
}

func TestSubmitStageFinalsToProject_CopiesProcessToCPAOutput(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "登记", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF"})
	var outRuleCode string
	_ = db.Get(&outRuleCode, `SELECT file_rule_code FROM template_file_rules WHERE template_stage_id = ? AND data_state='output' LIMIT 1`, st.ID)

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	projectCode := "CPA-1000"
	if err := ws.CreateProjectTree(projectCode, []string{st.StageCode}); err != nil {
		t.Fatal(err)
	}
	// 在 CPA process 目录放一个用户编辑好的过程文件
	procDir := ws.StageStateDir(projectCode, st.StageCode, "process")
	_ = os.MkdirAll(procDir, 0o755)
	srcName := "收稿登记表.docx"
	if err := os.WriteFile(filepath.Join(procDir, srcName), []byte("内容"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := SubmitStageFinalsToProject(db, tpl.TemplateCode, projectCode, st.StageCode, []FinalSelection{
		{FileRuleCode: outRuleCode, SourceFile: srcName},
	})
	if err != nil {
		t.Fatalf("定稿失败: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("应生成 1 个定稿，实得 %d", len(created))
	}
	// 定稿落在 CPA output 目录，规范名 + 沿用所挑过程文件的真实后缀(.docx)，
	// 不强制成标识声明的 PDF（避免内容是 docx 却命名 .pdf 的格式错配）。
	out := filepath.Join(ws.StageStateDir(projectCode, st.StageCode, "output"), "登记定稿.docx")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("CPA output 定稿应沿用源后缀 .docx: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.StageStateDir(projectCode, st.StageCode, "output"), "登记定稿.pdf")); err == nil {
		t.Fatal("不应强制成 .pdf")
	}
	// 源过程文件保留（不删改）
	if _, err := os.Stat(filepath.Join(procDir, srcName)); err != nil {
		t.Fatal("源过程文件应保留")
	}
}

// 五层落盘：带 task_code 的定稿——源取自该任务 process，定稿落到该任务 output。
func TestSubmitStageFinalsToProject_PerTask(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "登记", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF"})
	var outRuleCode string
	_ = db.Get(&outRuleCode, `SELECT file_rule_code FROM template_file_rules WHERE template_task_id = ? AND data_state='output' LIMIT 1`, tk.ID)

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	projectCode := "CPA-1001"
	if _, err := ws.CreateTaskDir(projectCode, st.StageCode, tk.TaskCode); err != nil {
		t.Fatal(err)
	}
	// 过程文件放在该任务自己的 process 目录
	procDir := ws.TaskStateDir(projectCode, st.StageCode, tk.TaskCode, "process")
	srcName := "收稿登记表.docx"
	if err := os.WriteFile(filepath.Join(procDir, srcName), []byte("内容"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := SubmitStageFinalsToProject(db, tpl.TemplateCode, projectCode, st.StageCode, []FinalSelection{
		{FileRuleCode: outRuleCode, SourceFile: srcName, TaskCode: tk.TaskCode},
	})
	if err != nil {
		t.Fatalf("定稿失败: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("应生成 1 个定稿，实得 %d", len(created))
	}
	// 定稿落到该任务的 output 目录
	out := filepath.Join(ws.TaskStateDir(projectCode, st.StageCode, tk.TaskCode, "output"), "登记定稿.docx")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("定稿应落到任务 output 目录: %v", err)
	}
	// 不应落到环节级 output（旧平铺）
	if _, err := os.Stat(filepath.Join(ws.StageStateDir(projectCode, st.StageCode, "output"), "登记定稿.docx")); err == nil {
		t.Fatal("不应落到环节级 output")
	}
}
