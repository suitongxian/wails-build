package repository

import "testing"

// TestCloneLocalTemplateForApplication 验证项目专属模版克隆：
// 深拷贝五层、用确定化编码 TPL-PRJ-<appKey>、改副本不影响源、再次调用幂等复用。
func TestCloneLocalTemplateForApplication(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	// 源模版：1 环节 / 1 任务 / 2 标识
	src, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(src.ID, StageInput{Name: "收稿", Desc: "环节说明"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "登记", SensitivityLevel: "important", Desc: "任务说明"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "客户原稿", DataState: "input", AllowedFileTypes: "PDF"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "登记定稿", DataState: "output", AllowedFileTypes: "PDF", Required: true, Drafter: "张三"})

	appKey := "777"
	cloneID, err := repo.CloneLocalTemplateForApplication(src.ID, appKey)
	if err != nil {
		t.Fatalf("克隆失败: %v", err)
	}
	if cloneID == src.ID {
		t.Fatal("克隆应是新模版，不能是源模版本身")
	}

	clone, err := repo.GetLocalTemplate(cloneID)
	if err != nil {
		t.Fatalf("读取克隆失败: %v", err)
	}
	if clone.TemplateCode != "TPL-PRJ-777" {
		t.Fatalf("项目专属模版编码应为 TPL-PRJ-777，实得 %s", clone.TemplateCode)
	}
	if clone.Origin != "local" {
		t.Fatalf("项目专属模版应为 origin=local，实得 %s", clone.Origin)
	}

	// 五层结构完整拷贝
	tree, _ := repo.GetLocalTemplateTree(cloneID)
	if len(tree.Stages) != 1 || len(tree.Stages[0].Tasks) != 1 || len(tree.Stages[0].Tasks[0].FileRules) != 2 {
		t.Fatalf("克隆树结构不完整: %+v", tree)
	}
	if deref(tree.Stages[0].TemplateStage.Description) != "环节说明" {
		t.Fatal("环节描述未拷贝")
	}
	// 编码必须保留（承接分工按 code 关联）：克隆环节/任务/标识码应与源一致
	if tree.Stages[0].TemplateStage.StageCode != st.StageCode {
		t.Fatalf("环节编码应保留 %s，实得 %s", st.StageCode, tree.Stages[0].TemplateStage.StageCode)
	}
	if tree.Stages[0].Tasks[0].TemplateTask.TaskCode != tk.TaskCode {
		t.Fatalf("任务编码应保留 %s，实得 %s", tk.TaskCode, tree.Stages[0].Tasks[0].TemplateTask.TaskCode)
	}

	// FindProjectTemplate 能命中
	if got, _ := repo.FindProjectTemplate(appKey); got != cloneID {
		t.Fatalf("FindProjectTemplate 应返回 %d，实得 %d", cloneID, got)
	}

	// 幂等：再次克隆复用同一份（不新建），且保留期间所做的编辑
	st2, _ := repo.CreateStage(cloneID, StageInput{Name: "新增空环节"})
	again, err := repo.CloneLocalTemplateForApplication(src.ID, appKey)
	if err != nil {
		t.Fatalf("二次克隆失败: %v", err)
	}
	if again != cloneID {
		t.Fatalf("二次克隆应复用 %d，实得 %d", cloneID, again)
	}
	tree2, _ := repo.GetLocalTemplateTree(cloneID)
	if len(tree2.Stages) != 2 {
		t.Fatalf("复用应保留新增的环节（应 2 个），实得 %d", len(tree2.Stages))
	}
	_ = st2

	// 改副本不影响源：源仍是 1 环节
	srcTree, _ := repo.GetLocalTemplateTree(src.ID)
	if len(srcTree.Stages) != 1 {
		t.Fatalf("编辑项目专属模版不应影响源模版，源应仍为 1 环节，实得 %d", len(srcTree.Stages))
	}
}
