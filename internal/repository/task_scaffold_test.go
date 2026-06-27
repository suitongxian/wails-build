package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScaffoldTaskDocsForProject_AllThreeStates 验证任务级脚手架：input + process + output 三态
// 各按扩展名落到自己的桶目录建空占位。
func TestScaffoldTaskDocsForProject_AllThreeStates(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "录入", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "客户原稿", DataState: "input", AllowedFileTypes: "PDF"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "录入登记表", DataState: "process", AllowedFileTypes: "docx"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "登记定稿", DataState: "output", AllowedFileTypes: "pdf"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	vp := "CPA-777"
	if err := ws.CreateProjectTree(vp, []string{st.StageCode}); err != nil {
		t.Fatal(err)
	}

	created, err := ScaffoldTaskDocsForProject(db, tpl.TemplateCode, vp, st.StageCode, tk.TaskCode)
	if err != nil {
		t.Fatalf("脚手架失败: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("应建 input + process + output 共 3 个，实得 %d: %v", len(created), created)
	}
	inp := filepath.Join(ws.TaskStateDir(vp, st.StageCode, tk.TaskCode, "input"), "客户原稿.pdf")
	proc := filepath.Join(ws.TaskStateDir(vp, st.StageCode, tk.TaskCode, "process"), "录入登记表.docx")
	out := filepath.Join(ws.TaskStateDir(vp, st.StageCode, tk.TaskCode, "output"), "登记定稿.pdf")
	// docx 占位现为最小有效文件（非 0 字节），但仍被识别为占位
	if fi, err := os.Stat(proc); err != nil || fi.Size() == 0 || !isPlaceholderFile(proc, fi.Size()) {
		t.Fatalf("docx 占位应为非空有效文件且被识别为占位: err=%v", err)
	}
	// pdf 占位非空但仍被识别为占位
	for _, p := range []string{inp, out} {
		fi, err := os.Stat(p)
		if err != nil || fi.Size() == 0 || !isPlaceholderFile(p, fi.Size()) {
			t.Fatalf("pdf 占位应为非空且被识别为占位: %s err=%v", p, err)
		}
	}
}

func TestScaffoldTaskDocsForProject_OnlyThatTask(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk1, _ := repo.CreateTask(st.ID, TaskInput{Name: "录入", SensitivityLevel: "important"})
	tk2, _ := repo.CreateTask(st.ID, TaskInput{Name: "校对", SensitivityLevel: "important"})
	repo.CreateFileRule(tk1.ID, FileRuleInput{FileName: "录入登记表", DataState: "process", AllowedFileTypes: "docx"})
	repo.CreateFileRule(tk2.ID, FileRuleInput{FileName: "校对单", DataState: "process", AllowedFileTypes: "docx"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	vp := "CPA-555"
	if err := ws.CreateProjectTree(vp, []string{st.StageCode}); err != nil {
		t.Fatal(err)
	}

	created, err := ScaffoldTaskDocsForProject(db, tpl.TemplateCode, vp, st.StageCode, tk1.TaskCode)
	if err != nil {
		t.Fatalf("脚手架失败: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("应只建 TK-1 的 1 个文件，实得 %d: %v", len(created), created)
	}
	// 五层落盘：占位落到 tk1 自己的 process 目录，且不污染 tk2 的目录。
	proc1 := ws.TaskStateDir(vp, st.StageCode, tk1.TaskCode, "process")
	if _, err := os.Stat(filepath.Join(proc1, "录入登记表.docx")); err != nil {
		t.Fatalf("应建录入登记表.docx: %v", err)
	}
	proc2 := ws.TaskStateDir(vp, st.StageCode, tk2.TaskCode, "process")
	if _, err := os.Stat(filepath.Join(proc2, "校对单.docx")); err == nil {
		t.Fatal("不应建别的任务(校对)的文件")
	}
	// 不应再落到环节级 process（旧平铺路径）
	if _, err := os.Stat(filepath.Join(ws.StageStateDir(vp, st.StageCode, "process"), "录入登记表.docx")); err == nil {
		t.Fatal("不应落到环节级 process（应在任务目录下）")
	}
}
