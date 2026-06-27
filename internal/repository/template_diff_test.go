package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDiffSnap 纯逻辑：三级（环节/任务/标识）的 新增/删除/改名 都能识别，匹配按 code。
func TestDiffSnap(t *testing.T) {
	base := snapTree{Stages: []snapStage{
		{Code: "S1", Name: "收稿", Tasks: []snapTask{
			{Code: "T1", Name: "录入", Rules: []snapFileRule{
				{Code: "R1", Name: "原稿"},
				{Code: "R2", Name: "废稿"}, // 将被删除
			}},
			{Code: "T2", Name: "旧任务"}, // 将被删除
		}},
		{Code: "S2", Name: "排版"}, // 将被删除
	}}
	final := snapTree{Stages: []snapStage{
		{Code: "S1", Name: "收件", Tasks: []snapTask{ // 改名 收稿→收件
			{Code: "T1", Name: "登记", Rules: []snapFileRule{ // 改名 录入→登记
				{Code: "R1", Name: "原始稿件"},  // 改名 原稿→原始稿件
				{Code: "R3", Name: "扫描件"}, // 新增标识
			}},
			{Code: "T3", Name: "校对"}, // 新增任务
		}},
		{Code: "S3", Name: "印刷"}, // 新增环节
	}}

	changes := diffSnap(base, final)

	want := map[string]bool{
		"stage|renamed|收件":      false, // 收稿→收件
		"stage|removed|排版":      false,
		"stage|added|印刷":        false,
		"task|renamed|登记":       false, // 录入→登记
		"task|removed|旧任务":      false,
		"task|added|校对":         false,
		"file_rule|renamed|原始稿件": false, // 原稿→原始稿件
		"file_rule|removed|废稿":   false,
		"file_rule|added|扫描件":    false,
	}
	for _, c := range changes {
		key := c.Level + "|" + c.Type + "|" + c.Name
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("缺少预期变更: %s（实得 %d 条: %+v）", k, len(changes), changes)
		}
	}
	// 未改动的不应产出噪声：S1 改名计 1 条 stage 级，不应有 S1 的 "unchanged"
	for _, c := range changes {
		if c.Type != "added" && c.Type != "removed" && c.Type != "renamed" {
			t.Errorf("出现非预期变更类型: %+v", c)
		}
	}
}

// TestBaselineSavedOnClone 关联即克隆时写入基线快照，内容为源模版结构（按 code）。
func TestBaselineSavedOnClone(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	src, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(src.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "录入"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "原稿", DataState: "input", AllowedFileTypes: "PDF"})

	if _, err := repo.CloneLocalTemplateForApplication(src.ID, "7"); err != nil {
		t.Fatalf("克隆失败: %v", err)
	}

	base, ok := repo.getProjectTemplateBaseline("7")
	if !ok {
		t.Fatal("克隆后应写入基线快照")
	}
	if len(base.Stages) != 1 || base.Stages[0].Name != "收稿" || base.Stages[0].Code != st.StageCode {
		t.Fatalf("基线环节不符: %+v", base.Stages)
	}
	if len(base.Stages[0].Tasks) != 1 || base.Stages[0].Tasks[0].Name != "录入" {
		t.Fatalf("基线任务不符: %+v", base.Stages[0].Tasks)
	}
	if len(base.Stages[0].Tasks[0].Rules) != 1 || base.Stages[0].Tasks[0].Rules[0].Name != "原稿" {
		t.Fatalf("基线标识不符: %+v", base.Stages[0].Tasks[0].Rules)
	}

	// 复用克隆不覆盖基线（即便此后源有变）
	repo.CreateStage(src.ID, StageInput{Name: "源后加环节"})
	repo.CloneLocalTemplateForApplication(src.ID, "7")
	base2, _ := repo.getProjectTemplateBaseline("7")
	if len(base2.Stages) != 1 {
		t.Fatalf("基线应保持首次快照（1 环节），实得 %d", len(base2.Stages))
	}
}

// TestDiffProjectTemplate_EndToEnd 基线(源) vs manage 最终结构 → 改动清单。
func TestDiffProjectTemplate_EndToEnd(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	src, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "印刷", SensitivityLevel: "important"})
	st, _ := repo.CreateStage(src.ID, StageInput{Name: "收稿"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "录入"})
	fr, _ := repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "原稿", DataState: "input", AllowedFileTypes: "PDF"})
	repo.CloneLocalTemplateForApplication(src.ID, "7")

	// manage 最终结构：环节改名(收稿→收件)、原标识改名(原稿→原始稿件)、新增一个标识(同任务码不变)
	final := `{"code":0,"message":"ok","data":{
		"template":{"template_code":"TPL-PRJ-7","template_version":"V1.0","template_name":"印刷","project_sensitivity_level":"important","scope":"unit"},
		"stages":[{"stage_code":"` + st.StageCode + `","stage_name":"收件","tasks":[
			{"task_code":"` + tk.TaskCode + `","task_name":"录入","file_rules":[
				{"file_rule_code":"` + fr.FileRuleCode + `","file_name":"原始稿件","data_state":"input","required":1,"allowed_file_types":"PDF"},
				{"file_rule_code":"NEW-1","file_name":"扫描件","data_state":"input","required":0,"allowed_file_types":"PDF"}
			]}
		]}]}}`
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.URL.Path, "authoring-tree") {
			_, _ = w.Write([]byte(final))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer manage.Close()

	changes, hasBaseline, err := repo.DiffProjectTemplate(nil, manage.URL, "7")
	if err != nil {
		t.Fatalf("diff 失败: %v", err)
	}
	if !hasBaseline {
		t.Fatal("应有基线")
	}
	got := map[string]string{}
	for _, c := range changes {
		got[c.Level+"|"+c.Name] = c.Type
	}
	if got["stage|收件"] != "renamed" {
		t.Errorf("应识别环节改名 收稿→收件，实得 %+v", changes)
	}
	if got["file_rule|原始稿件"] != "renamed" {
		t.Errorf("应识别标识改名 原稿→原始稿件，实得 %+v", changes)
	}
	if got["file_rule|扫描件"] != "added" {
		t.Errorf("应识别新增标识 扫描件，实得 %+v", changes)
	}
}

// TestDiffProjectTemplate_UsesManageBaseline 无本地基线时，用 manage 云端基线对比（多端可提取）。
func TestDiffProjectTemplate_UsesManageBaseline(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	// 注意：本测试不在本地克隆，故本地无基线——只能走 manage 云端基线。

	baseline := `{"stages":[{"code":"S1","name":"收稿","tasks":[]}]}`
	final := `{"code":0,"message":"ok","data":{
		"template":{"template_code":"TPL-PRJ-9","template_version":"V1.0","template_name":"印刷","project_sensitivity_level":"important","scope":"unit"},
		"stages":[{"stage_code":"S1","stage_name":"收件","tasks":[]}]}}`
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.URL.Path, "authoring-tree"):
			_, _ = w.Write([]byte(final))
		case strings.Contains(req.URL.Path, "template-baseline"):
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"source_code":"TPL-X","source_version":"V1.0","baseline_json":` + jsonString(baseline) + `}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0}`))
		}
	}))
	defer manage.Close()

	changes, hasBaseline, err := repo.DiffProjectTemplate(nil, manage.URL, "9")
	if err != nil {
		t.Fatalf("diff 失败: %v", err)
	}
	if !hasBaseline {
		t.Fatal("应使用 manage 云端基线")
	}
	if len(changes) != 1 || changes[0].Type != "renamed" || changes[0].Name != "收件" {
		t.Fatalf("应识别环节改名 收稿→收件（来自云端基线），实得 %+v", changes)
	}
}

// jsonString 把字符串编码为 JSON 字符串字面量（含引号），用于内嵌到响应体。
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
