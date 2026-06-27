package repository

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBusinessClassCRUD 验证行业分类的本地增删改查（片2）
//
// 2026-05-31 模版创作迁到 scan：行业分类(business_classes) 在 scan 本地可创作。
// 编码全自动生成（IND-NNN 递增），用户只填名称/描述。删除为软删除（disable=1）。
func TestBusinessClassCRUD(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	// 创建：自动生成编码
	a, err := repo.CreateBusinessClass("出版印刷", "图书/期刊印刷数据业务")
	if err != nil {
		t.Fatalf("创建行业失败: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("创建后应有 id")
	}
	if a.Code == "" {
		t.Fatal("应自动生成 code")
	}
	if a.Type != "industry" {
		t.Fatalf("行业分类 type 应为 industry，实得 %q", a.Type)
	}

	// 第二个：编码应不同且递增
	b, err := repo.CreateBusinessClass("政务", "政务数据管理")
	if err != nil {
		t.Fatalf("创建第二个行业失败: %v", err)
	}
	if b.Code == a.Code {
		t.Fatalf("两次创建编码不应相同：%q == %q", a.Code, b.Code)
	}

	// 列表：含刚建的两个
	list, err := repo.ListBusinessClasses()
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	found := map[string]bool{}
	for _, x := range list {
		found[x.Code] = true
	}
	if !found[a.Code] || !found[b.Code] {
		t.Fatalf("列表应含新建的两个行业，实得 %d 条", len(list))
	}

	// 更新
	if err := repo.UpdateBusinessClass(a.ID, "出版印刷业", "更新后的描述"); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	got, err := repo.GetBusinessClass(a.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Name != "出版印刷业" {
		t.Fatalf("更新后名称应为 出版印刷业，实得 %q", got.Name)
	}
	if got.Description == nil || *got.Description != "更新后的描述" {
		t.Fatalf("更新后描述不符: %v", got.Description)
	}

	// 软删除：删除后列表不再含它，但行仍在（disable=1）
	if err := repo.DeleteBusinessClass(b.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	list2, err := repo.ListBusinessClasses()
	if err != nil {
		t.Fatalf("删除后列表失败: %v", err)
	}
	for _, x := range list2 {
		if x.ID == b.ID {
			t.Fatal("软删除后列表不应再含该行业")
		}
	}
	var disabled int
	if err := db.Get(&disabled, "SELECT disable FROM business_classes WHERE id=?", b.ID); err != nil {
		t.Fatalf("查 disable 失败: %v", err)
	}
	if disabled != 1 {
		t.Fatalf("软删除应置 disable=1，实得 %d", disabled)
	}
}

// TestLocalTemplateCRUD 验证「数据项目模版」本地创作的增删改查（片3）
//
// 项目模版即五层树的根：origin=local，template_code 自动生成，status 默认 draft。
func TestLocalTemplateCRUD(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	ind, err := repo.CreateBusinessClass("出版印刷", "")
	if err != nil {
		t.Fatalf("建行业失败: %v", err)
	}

	tpl, err := repo.CreateLocalTemplate(CreateTemplateInput{
		ClassCode:        ind.Code,
		Scope:            "unit",
		TemplateName:     "《明朝那些事儿》印刷计划",
		ShortCode:        "MC-NSXS",
		Manager:          "刘老师",
		Description:      "示例项目模版",
		ApprovalBasis:    "出版计划批文",
		SensitivityLevel: "core",
		Owner:            "第一研究院",
	})
	if err != nil {
		t.Fatalf("创建项目模版失败: %v", err)
	}
	if tpl.Origin != "local" {
		t.Fatalf("本地创作 origin 应为 local，实得 %q", tpl.Origin)
	}
	if tpl.TemplateCode == "" {
		t.Fatal("应自动生成 template_code")
	}
	if tpl.Status != "draft" {
		t.Fatalf("新建模版 status 应为 draft，实得 %q", tpl.Status)
	}
	if tpl.ProjectSensitivityLevel != "core" {
		t.Fatalf("敏感级别应为 core，实得 %q", tpl.ProjectSensitivityLevel)
	}

	// 非法敏感级别应被拒
	if _, err := repo.CreateLocalTemplate(CreateTemplateInput{
		ClassCode: ind.Code, Scope: "unit", TemplateName: "x", SensitivityLevel: "绝密",
	}); err == nil {
		t.Fatal("非法敏感级别应报错")
	}

	// 第二个模版（不同行业），验证列表按行业过滤
	ind2, _ := repo.CreateBusinessClass("政务", "")
	if _, err := repo.CreateLocalTemplate(CreateTemplateInput{
		ClassCode: ind2.Code, Scope: "unit", TemplateName: "政务归档模版", SensitivityLevel: "general",
	}); err != nil {
		t.Fatalf("建第二个模版失败: %v", err)
	}

	all, err := repo.ListLocalTemplates("", "")
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("本地模版应有 2 个，实得 %d", len(all))
	}
	byClass, err := repo.ListLocalTemplates(ind.Code, "")
	if err != nil {
		t.Fatalf("按行业列表失败: %v", err)
	}
	if len(byClass) != 1 || byClass[0].ID != tpl.ID {
		t.Fatalf("按行业过滤应只剩 1 个，实得 %d", len(byClass))
	}

	// 更新
	if err := repo.UpdateLocalTemplate(tpl.ID, CreateTemplateInput{
		ClassCode: ind.Code, Scope: "department", TemplateName: "改名了", Manager: "王老师", SensitivityLevel: "important",
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	got, _ := repo.GetLocalTemplate(tpl.ID)
	if got.TemplateName != "改名了" || got.Scope != "department" || got.ProjectSensitivityLevel != "important" {
		t.Fatalf("更新未生效: name=%q scope=%q sens=%q", got.TemplateName, got.Scope, got.ProjectSensitivityLevel)
	}

	// 软删除
	if err := repo.DeleteLocalTemplate(tpl.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	after, _ := repo.ListLocalTemplates("", "")
	if len(after) != 1 {
		t.Fatalf("删除后应剩 1 个，实得 %d", len(after))
	}
}

// TestStageTaskFileRuleCRUD 验证 事项/任务/标识 三层本地 CRUD（片4）
//
// 编码全自动：事项 STG-NNN（按模版作用域）、任务 TK-NNN（按事项）、
// 标识按数据态 IN/PRC/OUT-NNN（按事项作用域，沿用既有 UNIQUE(stage,code) 约束）。
func TestStageTaskFileRuleCRUD(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	ind, _ := repo.CreateBusinessClass("出版印刷", "")
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{ClassCode: ind.Code, Scope: "unit", TemplateName: "印刷计划", SensitivityLevel: "core"})

	// ---- 工作事项(stage) ----
	st1, err := repo.CreateStage(tpl.ID, StageInput{Name: "收稿登记", Manager: "刘老师", Members: "王老师,赵编辑"})
	if err != nil {
		t.Fatalf("建事项失败: %v", err)
	}
	if st1.StageCode == "" {
		t.Fatal("事项应自动生成 stage_code")
	}
	if st1.SortOrder != 1 {
		t.Fatalf("首个事项 sort_order 应为 1，实得 %d", st1.SortOrder)
	}
	st2, _ := repo.CreateStage(tpl.ID, StageInput{Name: "排版"})
	if st2.StageCode == st1.StageCode {
		t.Fatal("同模版下事项编码应不同")
	}
	if st2.SortOrder != 2 {
		t.Fatalf("第二个事项 sort_order 应为 2，实得 %d", st2.SortOrder)
	}
	stages, _ := repo.ListStages(tpl.ID)
	if len(stages) != 2 {
		t.Fatalf("应有 2 个事项，实得 %d", len(stages))
	}

	// ---- 文件任务(task) ----
	tk1, err := repo.CreateTask(st1.ID, TaskInput{Name: "客户原稿处理", Manager: "刘老师", SensitivityLevel: "core"})
	if err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
	if tk1.TaskCode == "" {
		t.Fatal("任务应自动生成 task_code")
	}
	// 不同事项下任务编码可重复（作用域隔离）
	tkOther, _ := repo.CreateTask(st2.ID, TaskInput{Name: "排版定稿"})
	if tkOther.TaskCode != tk1.TaskCode {
		t.Fatalf("不同事项的首个任务编码应相同（各自作用域），got %q vs %q", tkOther.TaskCode, tk1.TaskCode)
	}
	// 非法敏感级别被拒
	if _, err := repo.CreateTask(st1.ID, TaskInput{Name: "x", SensitivityLevel: "绝密"}); err == nil {
		t.Fatal("任务非法敏感级别应报错")
	}
	tasks, _ := repo.ListTasks(st1.ID)
	if len(tasks) != 1 {
		t.Fatalf("st1 下应有 1 个任务，实得 %d", len(tasks))
	}

	// ---- 文档标识(file_rule) ----
	fr1, err := repo.CreateFileRule(tk1.ID, FileRuleInput{
		FileName: "客户原稿", DataState: "input", Required: true,
		AllowedFileTypes: "PDF,DOC", NamingPattern: "{书名}-原稿", Drafter: "刘老师", SensitivityLevel: "core",
	})
	if err != nil {
		t.Fatalf("建标识失败: %v", err)
	}
	if fr1.FileRuleCode != "IN-001" {
		t.Fatalf("input 标识编码应为 IN-001，实得 %q", fr1.FileRuleCode)
	}
	if fr1.TemplateTaskID == nil || *fr1.TemplateTaskID != tk1.ID {
		t.Fatalf("标识应挂在任务 %d 上，实得 %v", tk1.ID, fr1.TemplateTaskID)
	}
	if fr1.TemplateStageID != st1.ID {
		t.Fatalf("标识 stage_id 应回填为任务所属事项 %d，实得 %d", st1.ID, fr1.TemplateStageID)
	}
	// 第二个 input → IN-002（按事项作用域递增）
	fr2, _ := repo.CreateFileRule(tk1.ID, FileRuleInput{FileName: "委托书", DataState: "input", AllowedFileTypes: "PDF"})
	if fr2.FileRuleCode != "IN-002" {
		t.Fatalf("第二个 input 应为 IN-002，实得 %q", fr2.FileRuleCode)
	}
	// output → OUT-001
	fr3, _ := repo.CreateFileRule(tk1.ID, FileRuleInput{FileName: "收稿凭证", DataState: "output", AllowedFileTypes: "PDF"})
	if fr3.FileRuleCode != "OUT-001" {
		t.Fatalf("output 标识应为 OUT-001，实得 %q", fr3.FileRuleCode)
	}
	// 非法 data_state 被拒
	if _, err := repo.CreateFileRule(tk1.ID, FileRuleInput{FileName: "x", DataState: "weird", AllowedFileTypes: "PDF"}); err == nil {
		t.Fatal("非法 data_state 应报错")
	}
	rules, _ := repo.ListFileRules(tk1.ID)
	if len(rules) != 3 {
		t.Fatalf("tk1 下应有 3 个标识，实得 %d", len(rules))
	}

	// ---- 更新 / 删除 ----
	if err := repo.UpdateFileRule(fr1.ID, FileRuleInput{FileName: "客户原稿(改)", DataState: "input", AllowedFileTypes: "PDF", Required: true}); err != nil {
		t.Fatalf("更新标识失败: %v", err)
	}
	if err := repo.DeleteFileRule(fr2.ID); err != nil {
		t.Fatalf("删除标识失败: %v", err)
	}
	if rules2, _ := repo.ListFileRules(tk1.ID); len(rules2) != 2 {
		t.Fatalf("删除后 tk1 应剩 2 个标识，实得 %d", len(rules2))
	}

	// ---- 级联软删：删事项应连带其下任务与标识 ----
	if err := repo.DeleteStage(st1.ID); err != nil {
		t.Fatalf("删事项失败: %v", err)
	}
	if tks, _ := repo.ListTasks(st1.ID); len(tks) != 0 {
		t.Fatalf("级联后事项下不应再有任务，实得 %d", len(tks))
	}
	if rs, _ := repo.ListFileRules(tk1.ID); len(rs) != 0 {
		t.Fatalf("级联后任务下不应再有标识，实得 %d", len(rs))
	}
}

// TestStageUsername 验证事项责任人/参与人的 username 存储与 ingest 透传（P1）
func TestStageUsername(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	ind, _ := repo.CreateBusinessClass("出版印刷", "")
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{ClassCode: ind.Code, Scope: "unit", TemplateName: "印刷计划", SensitivityLevel: "core"})
	st, err := repo.CreateStage(tpl.ID, StageInput{
		Name: "排版", Manager: "赵编辑", ManagerUsername: "zhao",
		Members: "王老师,刘老师", MembersUsernames: "wang,liu",
	})
	if err != nil {
		t.Fatalf("建事项失败: %v", err)
	}
	// 存储
	got := st
	if got.ManagerUsername == nil || *got.ManagerUsername != "zhao" {
		t.Fatalf("manager_username 应为 zhao，实得 %v", got.ManagerUsername)
	}
	if got.MembersUsernames == nil || *got.MembersUsernames != "wang,liu" {
		t.Fatalf("members_usernames 应为 wang,liu，实得 %v", got.MembersUsernames)
	}
	// 树里也带着
	tree, _ := repo.GetLocalTemplateTree(tpl.ID)
	if *tree.Stages[0].ManagerUsername != "zhao" {
		t.Fatal("树中事项应带 manager_username")
	}
	// ingest 负载透传
	payload, _ := repo.BuildIngestPayload(tpl.ID)
	if payload.Stages[0].ManagerUsername == nil || *payload.Stages[0].ManagerUsername != "zhao" {
		t.Fatalf("ingest 负载应带 manager_username，实得 %v", payload.Stages[0].ManagerUsername)
	}
	if payload.Stages[0].MembersUsernames == nil || *payload.Stages[0].MembersUsernames != "wang,liu" {
		t.Fatalf("ingest 负载应带 members_usernames")
	}
	// 更新
	if err := repo.UpdateStage(st.ID, StageInput{Name: "排版", Manager: "钱专家", ManagerUsername: "qian"}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	tree2, _ := repo.GetLocalTemplateTree(tpl.ID)
	if *tree2.Stages[0].ManagerUsername != "qian" {
		t.Fatalf("更新后 manager_username 应为 qian")
	}
}

// TestGetLocalTemplateTree 验证五层树读取（片5）
func TestGetLocalTemplateTree(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	ind, _ := repo.CreateBusinessClass("出版印刷", "")
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{ClassCode: ind.Code, Scope: "unit", TemplateName: "印刷计划", SensitivityLevel: "core"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿登记"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "客户原稿处理"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "客户原稿", DataState: "input", AllowedFileTypes: "PDF"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "收稿凭证", DataState: "output", AllowedFileTypes: "PDF"})
	// 第二个空事项（无任务），验证空集合渲染为 []
	repo.CreateStage(tpl.ID, StageInput{Name: "排版"})

	tree, err := repo.GetLocalTemplateTree(tpl.ID)
	if err != nil {
		t.Fatalf("读取树失败: %v", err)
	}
	if tree.Template.ID != tpl.ID {
		t.Fatalf("树根应为该模版，实得 %d", tree.Template.ID)
	}
	if len(tree.Stages) != 2 {
		t.Fatalf("应有 2 个事项，实得 %d", len(tree.Stages))
	}
	if len(tree.Stages[0].Tasks) != 1 {
		t.Fatalf("第一个事项应有 1 个任务，实得 %d", len(tree.Stages[0].Tasks))
	}
	if len(tree.Stages[0].Tasks[0].FileRules) != 2 {
		t.Fatalf("该任务应有 2 个标识，实得 %d", len(tree.Stages[0].Tasks[0].FileRules))
	}
	if tree.Stages[1].Tasks == nil {
		t.Fatal("空事项的 Tasks 应为非 nil 空切片，便于前端渲染")
	}
}

// TestPushTemplateToManage 验证反向同步：组装负载 + POST ingest + 回填 remote_id（片10）
func TestPushTemplateToManage(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	ind, _ := repo.CreateBusinessClass("出版印刷", "图书/期刊")
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{ClassCode: ind.Code, Scope: "unit", TemplateName: "印刷计划", SensitivityLevel: "core"})
	st, _ := repo.CreateStage(tpl.ID, StageInput{Name: "收稿登记", Manager: "刘老师"})
	tk, _ := repo.CreateTask(st.ID, TaskInput{Name: "客户原稿处理", SensitivityLevel: "core"})
	repo.CreateFileRule(tk.ID, FileRuleInput{FileName: "客户原稿", DataState: "input", Required: true, AllowedFileTypes: "PDF,DOC", NamingPattern: "{书名}-原稿"})

	// 先验证负载组装
	payload, err := repo.BuildIngestPayload(tpl.ID)
	if err != nil {
		t.Fatalf("组装负载失败: %v", err)
	}
	if payload.Template.TemplateCode != tpl.TemplateCode {
		t.Fatalf("负载 template_code 不符: %s", payload.Template.TemplateCode)
	}
	if payload.BusinessClass == nil || payload.BusinessClass.Code != ind.Code {
		t.Fatal("负载应含行业 code")
	}
	if len(payload.Stages) != 1 || len(payload.Stages[0].Tasks) != 1 || len(payload.Stages[0].Tasks[0].FileRules) != 1 {
		t.Fatal("负载五层结构不完整")
	}
	if !payload.Stages[0].Tasks[0].FileRules[0].Required {
		t.Fatal("required 应为 true")
	}

	// mock manage ingest 端点
	var received IngestPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/ingest" || r.Method != "POST" {
			w.WriteHeader(404)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &received)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"template_id":777}}`))
	}))
	defer srv.Close()

	remoteID, err := repo.PushTemplateToManage(srv.Client(), srv.URL, tpl.ID, false, "")
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	if remoteID != 777 {
		t.Fatalf("应回填 manage 返回的 remote_id=777，实得 %d", remoteID)
	}
	if received.Template.TemplateName != "印刷计划" {
		t.Fatalf("manage 收到的负载不符: %s", received.Template.TemplateName)
	}

	// 回填校验
	got, _ := repo.GetLocalTemplate(tpl.ID)
	if got.RemoteID == nil || *got.RemoteID != 777 {
		t.Fatalf("remote_id 未回填: %v", got.RemoteID)
	}
	if got.SyncStatus == nil || *got.SyncStatus != "synced" {
		t.Fatalf("sync_status 应为 synced，实得 %v", got.SyncStatus)
	}
	if got.SyncedAt == nil {
		t.Fatal("synced_at 应被记录")
	}
}

// TestInitiateGeneratesNewProjectCode 验证立项(isProject=true)：每次都生成全新项目编码、is_project=1，
// 且不复用模版编码——模版不动、再次立项是独立项目，不会更新同一个项目。
func TestInitiateGeneratesNewProjectCode(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "立项蓝本", SensitivityLevel: "general"})
	// 立项要求模版「已发布」
	if err := repo.SetTemplatePublished(tpl.ID, true); err != nil {
		t.Fatalf("发布失败: %v", err)
	}

	var got []IngestPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var p IngestPayload
		_ = json.Unmarshal(b, &p)
		got = append(got, p)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"template_id":1}}`))
	}))
	defer srv.Close()

	if _, err := repo.PushTemplateToManage(srv.Client(), srv.URL, tpl.ID, true, "alice"); err != nil {
		t.Fatalf("立项1失败: %v", err)
	}
	if _, err := repo.PushTemplateToManage(srv.Client(), srv.URL, tpl.ID, true, "alice"); err != nil {
		t.Fatalf("立项2失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应收到 2 次立项推送，实得 %d", len(got))
	}
	for i, p := range got {
		if p.Template.IsProject != 1 {
			t.Fatalf("第%d次立项 is_project 应为 1", i+1)
		}
		if p.Template.TemplateCode == tpl.TemplateCode {
			t.Fatalf("立项不应复用模版编码 %s（应生成新项目编码）", tpl.TemplateCode)
		}
	}
	if got[0].Template.TemplateCode == got[1].Template.TemplateCode {
		t.Fatalf("两次立项应生成不同的项目编码，实得相同 %s", got[0].Template.TemplateCode)
	}
}

// TestPushTemplateToManage_Error 验证 manage 返回错误时记录 sync_status=error
func TestPushTemplateToManage_Error(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{Scope: "unit", TemplateName: "x", SensitivityLevel: "general"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":500,"message":"boom","data":null}`))
	}))
	defer srv.Close()

	_, err := repo.PushTemplateToManage(srv.Client(), srv.URL, tpl.ID, false, "")
	if err == nil {
		t.Fatal("manage 报错时应返回 error")
	}
	got, _ := repo.GetLocalTemplate(tpl.ID)
	if got.SyncStatus == nil || *got.SyncStatus != "error" {
		t.Fatalf("失败应记 sync_status=error，实得 %v", got.SyncStatus)
	}
}

// TestBuildIngestPayload_ClassCodeWithoutLocalRow 验证：模版带 class_code 但 scan 本地无该分类行时，
// 负载仍带上 code（manage 按 code 命中自己的分类，使计数 +1）。
func TestBuildIngestPayload_ClassCodeWithoutLocalRow(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	// 直接用 IND-001（scan 本地不创建该分类，模拟分类归口 manage 的现状）
	tpl, _ := repo.CreateLocalTemplate(CreateTemplateInput{ClassCode: "IND-001", Scope: "unit", TemplateName: "印刷计划", SensitivityLevel: "core"})

	payload, err := repo.BuildIngestPayload(tpl.ID)
	if err != nil {
		t.Fatalf("组装负载失败: %v", err)
	}
	if payload.BusinessClass == nil || payload.BusinessClass.Code != "IND-001" {
		t.Fatalf("本地无分类行时也应带上 class code，实得 %+v", payload.BusinessClass)
	}
}
