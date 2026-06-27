package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFirstFileExt 验证后缀解析兼容 JSON 数组串与逗号分隔，且不产生 `.["docx"]` 脏后缀。
func TestFirstFileExt(t *testing.T) {
	cases := map[string]string{
		`["docx"]`:       ".docx", // 模版创作落库的 JSON 数组格式
		`["PDF","DOCX"]`: ".pdf",
		`PDF,DOCX`:       ".pdf", // 逗号分隔
		`docx`:           ".docx",
		`[" .Md "]`:      ".md",
		``:               "", // 未指定 → 无后缀
		`[]`:             "",
	}
	for in, want := range cases {
		if got := firstFileExt(in); got != want {
			t.Errorf("firstFileExt(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScaffoldStageFiles 验证开始工作预建占位：input + process + output 三态都按扩展名建空文件，
// 幂等不覆盖，且空占位不被自动归档。
func TestScaffoldStageFiles(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}
	repo := NewTemplateAuthoringRepository(db)

	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "占位测试", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "排版"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "排版加工", SensitivityLevel: "important"})
	// input + process + output 三态都应建占位
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "排版完成稿", DataState: "output", AllowedFileTypes: "PDF,DOCX"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "排版草稿", DataState: "process", AllowedFileTypes: "docx"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "原始书稿", DataState: "input", AllowedFileTypes: "PDF"})

	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	if _, err := NewProjectWorkspace(root).CreateStageDir(tpl.TemplateCode, st.StageCode); err != nil {
		t.Fatal(err)
	}

	created, err := ScaffoldStageFiles(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatalf("预建占位失败: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("应建 3 个占位（input + process + output），实得 %d: %v", len(created), created)
	}

	ws := NewProjectWorkspace(root)
	out := filepath.Join(ws.StageStateDir(tpl.TemplateCode, st.StageCode, "output"), "排版完成稿.pdf")
	proc := filepath.Join(ws.StageStateDir(tpl.TemplateCode, st.StageCode, "process"), "排版草稿.docx")
	inp := filepath.Join(ws.StageStateDir(tpl.TemplateCode, st.StageCode, "input"), "原始书稿.pdf")
	// docx 占位现为最小有效文件（非 0 字节），但仍被识别为未填写占位
	if fi, err := os.Stat(proc); err != nil || fi.Size() == 0 || !isPlaceholderFile(proc, fi.Size()) {
		t.Fatalf("process(docx) 占位应为非空有效文件且被识别为占位: err=%v", err)
	}
	// pdf 占位非空（最小可打开 PDF），但仍被识别为"未填写占位"
	for _, p := range []string{inp, out} {
		fi, err := os.Stat(p)
		if err != nil || fi.Size() == 0 {
			t.Fatalf("pdf 占位应为非空可打开文件: %s err=%v", p, err)
		}
		if !isPlaceholderFile(p, fi.Size()) {
			t.Fatalf("pdf 占位应被识别为未填写占位: %s", p)
		}
	}

	// 幂等：先往 process 占位写入内容，再建不覆盖、不新增
	if err := os.WriteFile(proc, []byte("real content"), 0o644); err != nil {
		t.Fatal(err)
	}
	created2, err := ScaffoldStageFiles(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatalf("二次预建失败: %v", err)
	}
	if len(created2) != 0 {
		t.Fatalf("二次预建不应新增，实得 %v", created2)
	}
	if b, _ := os.ReadFile(proc); string(b) != "real content" {
		t.Fatalf("已填内容被覆盖了")
	}

	// 自动归档：已填的 process 文件归档 1 个（空占位会被跳过，此处已填）
	res, err := AutoArchiveStage(db, tpl.TemplateCode, st.StageCode)
	if err != nil {
		t.Fatalf("自动归档失败: %v", err)
	}
	if res.Archived != 1 {
		t.Fatalf("应归档 1 个已填 process 文件，实得 archived=%d skipped=%d errors=%v", res.Archived, res.Skipped, res.Errors)
	}
}
