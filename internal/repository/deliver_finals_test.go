package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSubmitStageFinals 验证提交定稿：从 process 挑文件 → 拷贝到 output 并按 output 标识规范名改名，
// 源文件保留（不删），随后 AutoArchiveStage 能把定稿归档。
func TestSubmitStageFinals(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	repo := NewTemplateAuthoringRepository(db)

	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "定稿测试", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "排版"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "排版加工", SensitivityLevel: "important"})
	outRule, _ := repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "排版完成稿", DataState: "output", AllowedFileTypes: "PDF,DOCX"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	procDir := ws.StageStateDir(tpl.TemplateCode, st.StageCode, "process")
	os.MkdirAll(procDir, 0o755)
	// 过程文件（用户自己命名的工作文件）
	srcName := "我的草稿_v3.docx"
	if err := os.WriteFile(filepath.Join(procDir, srcName), []byte("final content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 空占位不应出现在挑选列表
	os.WriteFile(filepath.Join(procDir, "占位.docx"), []byte{}, 0o644)

	// 列表只含非空文件
	files, err := ListStageProcessFiles(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != srcName {
		t.Fatalf("process 列表应只含非空的 %s，实得 %v", srcName, files)
	}

	// output 标识清单
	rules, err := ListStageOutputRules(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].FileRuleCode != outRule.FileRuleCode {
		t.Fatalf("应有 1 个 output 标识，实得 %v", rules)
	}

	// 提交定稿：把草稿作为该标识的定稿
	created, err := SubmitStageFinals(db, tpl.TemplateCode, st.StageCode, []FinalSelection{
		{FileRuleCode: outRule.FileRuleCode, SourceFile: srcName},
	})
	if err != nil {
		t.Fatalf("提交定稿失败: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("应产出 1 份定稿，实得 %v", created)
	}
	// 改名为规范名 + 沿用所挑过程文件真实后缀(.docx)，内容与源一致，源保留
	// （2026-06-02：定稿后缀跟随源文件，不强制成标识声明的首类型，避免格式错配）
	dst := filepath.Join(ws.StageStateDir(tpl.TemplateCode, st.StageCode, "output"), "排版完成稿.docx")
	if b, err := os.ReadFile(dst); err != nil || string(b) != "final content" {
		t.Fatalf("定稿应按规范名改名且内容一致: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(procDir, srcName)); err != nil {
		t.Fatalf("过程源文件应保留（copy 不删源）: %v", err)
	}

	// 归档：定稿与过程同内容 → 单资产
	res, err := AutoArchiveStage(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	if res.Archived != 1 {
		t.Fatalf("同内容应归档 1 个，实得 archived=%d skipped=%d", res.Archived, res.Skipped)
	}
}
