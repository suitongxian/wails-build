package repository

import "testing"

// TestListTaskFileRules_AllStatesWithAttrs 验证「工作受理」用的任务文档标识查询：
// 返回 input/process/output 三态全部标识，按 input→process→output 排序，且属性齐全。
func TestListTaskFileRules_AllStatesWithAttrs(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "登记", SensitivityLevel: "important"})
	// 故意乱序创建，验证查询会按数据态归一排序
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF", Required: true, SensitivityLevel: "important", Drafter: "张三", NamingPattern: "登记-{date}"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "客户原稿", DataState: "input", AllowedFileTypes: "PDF,DOCX"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "登记草稿", DataState: "process", AllowedFileTypes: "docx"})

	rules, err := ListTaskFileRules(db, tpl.TemplateCode, st.StageCode, tk.TaskCode)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("应返回 3 个文档标识，实得 %d", len(rules))
	}
	// 顺序：input → process → output
	wantOrder := []string{"input", "process", "output"}
	for i, w := range wantOrder {
		if rules[i].DataState != w {
			t.Fatalf("第 %d 个应为 %s，实得 %s", i, w, rules[i].DataState)
		}
	}
	// output 标识的属性应完整带出
	out := rules[2]
	if out.FileName != "登记定稿" {
		t.Fatalf("output 文件名错: %s", out.FileName)
	}
	if out.Required != 1 {
		t.Fatalf("output 应为必需(required=1)，实得 %d", out.Required)
	}
	if out.AllowedFileTypes == "" {
		t.Fatal("应带出 allowed_file_types")
	}
	if out.SensitivityLevel == nil || *out.SensitivityLevel != "important" {
		t.Fatalf("应带出敏感级 important，实得 %v", out.SensitivityLevel)
	}
	if out.Drafter == nil || *out.Drafter != "张三" {
		t.Fatalf("应带出起草人 张三，实得 %v", out.Drafter)
	}
	if out.NamingPattern == nil || *out.NamingPattern == "" {
		t.Fatal("应带出命名规则")
	}
}
