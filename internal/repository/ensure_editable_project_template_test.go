package repository

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 同机：项目专属模版已在本地 → EnsureEditableProjectTemplate 直接复用，不走网络。
func TestEnsureEditableProjectTemplate_LocalReuse(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	src, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	repo.CreateStage(src.ID, StageInput{Name: "收稿"})
	cloneID, err := repo.CloneLocalTemplateForApplication(src.ID, "5")
	if err != nil {
		t.Fatal(err)
	}
	// endpoint 给空，若走网络会报错；复用本地则不应触网
	got, err := repo.EnsureEditableProjectTemplate(nil, "", "5")
	if err != nil {
		t.Fatalf("应复用本地副本，不触网: %v", err)
	}
	if got != cloneID {
		t.Fatalf("应返回本地副本 %d，实得 %d", cloneID, got)
	}
}

// 跨机：本地没有 → 从 manage 拉取项目专属模版树，重建为可编辑副本，并保留 template_code 与各级编码。
func TestEnsureEditableProjectTemplate_PullPreservesCodes(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// authoring-tree 返回带编码的五层树（模拟 manage tree() 的 ...spread 行为）
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{
			"template":{"template_code":"TPL-PRJ-9","template_version":"V1.0","template_name":"书目印刷","project_sensitivity_level":"core_secret","scope":"unit"},
			"stages":[{"stage_code":"STG-001","stage_name":"收稿","tasks":[
				{"task_code":"TK-001","task_name":"录入","file_rules":[
					{"file_rule_code":"IN-001","file_name":"原稿","data_state":"input","required":1,"allowed_file_types":"PDF"}
				]}
			]}]
		}}`))
	}))
	defer manage.Close()

	id, err := repo.EnsureEditableProjectTemplate(nil, manage.URL, "9")
	if err != nil {
		t.Fatalf("拉取重建失败: %v", err)
	}
	tpl, _ := repo.GetLocalTemplate(id)
	if tpl.TemplateCode != "TPL-PRJ-9" {
		t.Fatalf("应保留 template_code=TPL-PRJ-9，实得 %s", tpl.TemplateCode)
	}
	if tpl.Origin != "local" {
		t.Fatalf("应为可编辑 origin=local，实得 %s", tpl.Origin)
	}
	tree, _ := repo.GetLocalTemplateTree(id)
	if len(tree.Stages) != 1 || tree.Stages[0].TemplateStage.StageCode != "STG-001" {
		t.Fatalf("应保留环节编码 STG-001，实得 %+v", tree.Stages)
	}
	if tree.Stages[0].Tasks[0].TemplateTask.TaskCode != "TK-001" {
		t.Fatalf("应保留任务编码 TK-001")
	}
	if tree.Stages[0].Tasks[0].FileRules[0].FileRuleCode != "IN-001" {
		t.Fatalf("应保留标识编码 IN-001")
	}
	// 敏感级映射 core_secret→core
	if tpl.ProjectSensitivityLevel != "core" {
		t.Fatalf("敏感级应映射为 core，实得 %s", tpl.ProjectSensitivityLevel)
	}

	// 幂等：再次 ensure 直接复用本地（不再新建）
	id2, err := repo.EnsureEditableProjectTemplate(nil, manage.URL, "9")
	if err != nil || id2 != id {
		t.Fatalf("二次 ensure 应复用 %d，实得 %d err=%v", id, id2, err)
	}
}
