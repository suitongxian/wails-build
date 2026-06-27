package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// TestListTaskFinalCandidateFiles_ProcessAndOutput 候选定稿来源含 process/ + output/ 非空文件，空占位不列。
func TestListTaskFinalCandidateFiles_ProcessAndOutput(t *testing.T) {
	db := openTestDB(t)
	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	vp, sc, tc := "CPA-9", "STG-1", "TK-1"
	if _, err := ws.CreateTaskDir(vp, sc, tc); err != nil {
		t.Fatal(err)
	}
	procDir := ws.TaskStateDir(vp, sc, tc, "process")
	outDir := ws.TaskStateDir(vp, sc, tc, "output")
	_ = os.WriteFile(filepath.Join(procDir, "草稿.docx"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(procDir, "空占位.docx"), []byte{}, 0o644) // 空占位不列
	_ = os.WriteFile(filepath.Join(outDir, "定稿.pdf"), []byte("y"), 0o644)

	files, err := ListTaskFinalCandidateFiles(db, vp, sc, tc)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f] = true
	}
	if !got["草稿.docx"] || !got["定稿.pdf"] {
		t.Fatalf("应含 process 与 output 的非空文件，实得 %v", files)
	}
	if got["空占位.docx"] {
		t.Fatalf("空占位不应列出: %v", files)
	}
}

// TestSubmitStageFinals_GenericNoOutputRule 无定稿标识(FileRuleCode 为空)时，按所挑过程文件本名落定稿到 output/。
func TestSubmitStageFinals_GenericNoOutputRule(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "登记", SensitivityLevel: "important"})
	// 注意：本任务无 output 标识（模拟 AI 模版只含 input/process）

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	vp := "CPA-3000"
	if _, err := ws.CreateTaskDir(vp, st.StageCode, tk.TaskCode); err != nil {
		t.Fatal(err)
	}
	procDir := ws.TaskStateDir(vp, st.StageCode, tk.TaskCode, "process")
	if err := os.WriteFile(filepath.Join(procDir, "登记表.docx"), []byte("内容"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := SubmitStageFinalsToProject(db, tpl.TemplateCode, vp, st.StageCode, []FinalSelection{
		{FileRuleCode: "", SourceFile: "登记表.docx", TaskCode: tk.TaskCode}, // 通用定稿
	})
	if err != nil {
		t.Fatalf("通用定稿失败: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("应生成 1 个定稿，实得 %v", created)
	}
	// 定稿用源文件本名落到 output/
	out := filepath.Join(ws.TaskStateDir(vp, st.StageCode, tk.TaskCode, "output"), "登记表.docx")
	if b, _ := os.ReadFile(out); string(b) != "内容" {
		t.Fatalf("通用定稿应按本名落到 output/，内容应一致")
	}
}

// TestSubmitStageFinals_SourceFromOutput_SamePathNoClobber 用户直接在 output 填好定稿并挑它本身，
// 源==目标时不拷贝、内容不被清空。
func TestSubmitStageFinals_SourceFromOutput_SamePathNoClobber(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "登记", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF"})
	var outRuleCode string
	_ = db.Get(&outRuleCode, `SELECT file_rule_code FROM template_file_rules WHERE template_task_id=? AND data_state='output' LIMIT 1`, tk.ID)

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	vp := "CPA-2000"
	if _, err := ws.CreateTaskDir(vp, st.StageCode, tk.TaskCode); err != nil {
		t.Fatal(err)
	}
	outDir := ws.TaskStateDir(vp, st.StageCode, tk.TaskCode, "output")
	finalName := "登记定稿.pdf" // 规范名（用户直接在定稿目录填好）
	if err := os.WriteFile(filepath.Join(outDir, finalName), []byte("最终内容"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := SubmitStageFinalsToProject(db, tpl.TemplateCode, vp, st.StageCode, []FinalSelection{
		{FileRuleCode: outRuleCode, SourceFile: finalName, TaskCode: tk.TaskCode},
	})
	if err != nil {
		t.Fatalf("提交定稿失败: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("应生成 1 个定稿，实得 %v", created)
	}
	if b, _ := os.ReadFile(filepath.Join(outDir, finalName)); string(b) != "最终内容" {
		t.Fatalf("源==目标时定稿内容不应被破坏，实得 %q", string(b))
	}
}
