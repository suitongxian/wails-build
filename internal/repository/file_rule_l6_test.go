package repository

import "testing"

// L6 文档标识管控类字段：创建/更新持久化，且项目专属模版克隆时一并带过去。
func TestFileRule_L6ControlFields(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "登记"})
	days := 1825
	fr, err := repo.CreateFileRule(tk.ID, FileRuleInput{
		FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF",
		Category: "工作文档", SecurityRequirement: "加密存储", DiffusionRequirement: "双孤本模式",
		ArchiveRequirement: "部门文件柜", RetentionPeriodDays: &days, DestructionRule: "满期销毁+审计",
	})
	if err != nil {
		t.Fatal(err)
	}
	if deref(fr.Category) != "工作文档" || deref(fr.SecurityRequirement) != "加密存储" ||
		deref(fr.DiffusionRequirement) != "双孤本模式" || deref(fr.ArchiveRequirement) != "部门文件柜" ||
		fr.RetentionPeriodDays == nil || *fr.RetentionPeriodDays != 1825 || deref(fr.DestructionRule) == "" {
		t.Fatalf("创建未持久化 L6 字段: %+v", fr)
	}

	// 更新：改安全要求 + 保留期永久
	d2 := -1
	if err := repo.UpdateFileRule(fr.ID, FileRuleInput{
		FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF",
		Category: "工作文档", SecurityRequirement: "明文存储", DiffusionRequirement: "孤本模式",
		ArchiveRequirement: "单位文件室", RetentionPeriodDays: &d2, DestructionRule: "永久保存",
	}); err != nil {
		t.Fatal(err)
	}
	rules, _ := repo.ListFileRules(tk.ID)
	if len(rules) != 1 || deref(rules[0].SecurityRequirement) != "明文存储" || *rules[0].RetentionPeriodDays != -1 {
		t.Fatalf("更新未生效: %+v", rules)
	}

	// 克隆为项目专属模版：L6 字段应一并复制
	cloneID, err := repo.CloneLocalTemplateForApplication(tpl.ID, "88")
	if err != nil {
		t.Fatal(err)
	}
	tree, _ := repo.GetLocalTemplateTree(cloneID)
	cfr := tree.Stages[0].Tasks[0].FileRules[0]
	if deref(cfr.ArchiveRequirement) != "单位文件室" || deref(cfr.SecurityRequirement) != "明文存储" {
		t.Fatalf("克隆未带 L6 字段: %+v", cfr)
	}
}
