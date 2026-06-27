package httpd

import (
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 任务级定稿：候选接口列本任务 output 标识+过程文件；提交接口把过程文件拷成定稿落到本任务 output。
func TestHTTP_TaskFinals_PerTask(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	repo := repository.NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(repository.CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, repository.StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, repository.TaskInput{Name: "登记", SensitivityLevel: "important"})
	repo.CreateFileRule(tk.ID, repository.FileRuleInput{FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF"})

	root := t.TempDir()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyProjectRoot, root)
	ws := repository.NewProjectWorkspace(root)
	if _, err := ws.CreateTaskDir("CPA-42", st.StageCode, tk.TaskCode); err != nil {
		t.Fatal(err)
	}
	// 该任务 process 放一个用户编辑好的过程文件 + 一个空占位（空的不应作候选）
	procDir := ws.TaskStateDir("CPA-42", st.StageCode, tk.TaskCode, "process")
	if err := os.WriteFile(filepath.Join(procDir, "收稿登记表.docx"), []byte("内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "空占位.docx"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	// 候选：output 标识 1 个，过程文件只列非空的那个
	status, resp := jsonReqNoBody(t, r, "GET",
		"/centralized-projects/task-finals-candidates?app_id=42&stage_code="+st.StageCode+"&task_code="+tk.TaskCode+"&template_code="+tpl.TemplateCode)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	rules := d["output_rules"].([]interface{})
	if len(rules) != 1 {
		t.Fatalf("应有 1 个 output 标识, got %d", len(rules))
	}
	files := d["process_files"].([]interface{})
	if len(files) != 1 || files[0] != "收稿登记表.docx" {
		t.Fatalf("过程候选应只含非空文件, got %v", files)
	}
	ruleCode := rules[0].(map[string]interface{})["file_rule_code"].(string)

	// 提交定稿：拷到本任务 output
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/submit-task-finals", map[string]interface{}{
		"app_id": 42, "stage_code": st.StageCode, "task_code": tk.TaskCode, "template_code": tpl.TemplateCode,
		"selections": []map[string]interface{}{
			{"file_rule_code": ruleCode, "source_file": "收稿登记表.docx"},
		},
	})
	successOk(t, status, resp)

	// 定稿落到本任务 output（沿用源后缀 .docx），不落环节级
	out := filepath.Join(ws.TaskStateDir("CPA-42", st.StageCode, tk.TaskCode, "output"), "登记定稿.docx")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("定稿应落到任务 output 目录: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.StageStateDir("CPA-42", st.StageCode, "output"), "登记定稿.docx")); err == nil {
		t.Fatal("不应落到环节级 output")
	}
	// 源过程文件保留（守留痕）
	if _, err := os.Stat(filepath.Join(procDir, "收稿登记表.docx")); err != nil {
		t.Fatal("源过程文件应保留")
	}
}
