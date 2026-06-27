package repository

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockTemplateServer 模拟「模版管理平台」(template-manage) 的 /api/local-templates/{list,tree}。
// tree 的 allowed_file_types 故意用 JSON 数组字符串，验证克隆时会转成逗号串。
func mockTemplateServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/local-templates/list":
			// 模拟服务端 status 过滤：scan 始终带 ?status=active，仅返回已发布模版。
			active := []string{
				`{"id":7,"template_code":"TPL-LOCAL-001","template_name":"书目印刷计划","template_version":"V1.0","status":"active"}`,
				`{"id":8,"template_code":"TPL-PERSONAL-FILES","template_name":"个人文件","template_version":"V2.0","status":"active"}`,
			}
			draft := `{"id":9,"template_code":"TPL-LOCAL-DRAFT","template_name":"审订中模版","template_version":"V1.0","status":"review"}`
			items := active
			if r.URL.Query().Get("status") != "active" {
				items = append(append([]string{}, active...), draft) // 不带过滤时返回全部（含草稿）
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[` + strings.Join(items, ",") + `]}`))
		case r.URL.Path == "/api/local-templates/tree" && r.URL.Query().Get("id") == "7":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{
				"template":{"template_name":"书目印刷计划","project_sensitivity_level":"core_secret","scope":"industry","short_code":"MC","manager":"刘老师","owner":"第一研究院","approval_basis":"出版计划批文","description":"示例","class_code":"IND-001"},
				"stages":[
					{"stage_name":"收稿登记","manager":"王老师","manager_username":"","members":"","members_usernames":"","description":"环节说明",
						"tasks":[{"task_name":"材料接收","manager":"钱七","sensitivity_level":"core_secret","description":"",
							"file_rules":[{"file_name":"客户原稿","data_state":"output","required":1,"allowed_file_types":"[\"PDF\",\"DOCX\"]","naming_pattern":"{书名}-原稿","drafter":"赵六","sensitivity_level":"core_secret"}]}]},
					{"stage_name":"排版","manager":"赵编辑","members":"","tasks":[]}
				]}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}

// 克隆：先用 code 在 list 里定位 id，再按 id 拉 tree 重建五层；
// 校验 origin=local、core_secret→core、allowed_file_types JSON→逗号→本地落库。
func TestCloneTemplateServerToLocal(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	srv := mockTemplateServer(t)
	defer srv.Close()

	id, err := repo.CloneTemplateServerToLocal(srv.Client(), srv.URL, "TPL-LOCAL-001")
	if err != nil {
		t.Fatalf("克隆失败: %v", err)
	}

	var got struct {
		Origin string `db:"origin"`
		Name   string `db:"template_name"`
		Sens   string `db:"project_sensitivity_level"`
		Scope  string `db:"scope"`
	}
	if err := db.Get(&got, `SELECT origin, template_name, project_sensitivity_level, scope FROM data_templates WHERE id = ?`, id); err != nil {
		t.Fatalf("查本地模版失败: %v", err)
	}
	if got.Origin != "local" {
		t.Fatalf("应 origin=local，实得 %s", got.Origin)
	}
	if got.Name != "书目印刷计划" {
		t.Fatalf("名称应保留，实得 %s", got.Name)
	}
	if got.Sens != "core" {
		t.Fatalf("core_secret 应映射回 core，实得 %s", got.Sens)
	}
	if got.Scope == "industry" {
		t.Fatalf("行业模版另存应降级为非 industry，实得 %s", got.Scope)
	}

	// 结构：2 环节
	var stageN int
	if err := db.Get(&stageN, `SELECT COUNT(*) FROM template_stages WHERE template_id = ? AND disable = 0`, id); err != nil {
		t.Fatal(err)
	}
	if stageN != 2 {
		t.Fatalf("应克隆 2 个环节，实得 %d", stageN)
	}

	// 文件标识：allowed_file_types 只保留单个类型（克隆时从 JSON 数组取第一个 → PDF）
	var types string
	err = db.Get(&types, `SELECT fr.allowed_file_types FROM template_file_rules fr
		JOIN template_stages st ON st.id = fr.template_stage_id
		WHERE st.template_id = ? AND fr.disable = 0 LIMIT 1`, id)
	if err != nil {
		t.Fatalf("查文件标识失败: %v", err)
	}
	if types != "PDF" {
		t.Fatalf("allowed_file_types 应只保留单个类型 PDF，实得 %s", types)
	}
}

// code 不存在时应报错，不应静默建空模版
func TestCloneTemplateServerToLocal_CodeNotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	srv := mockTemplateServer(t)
	defer srv.Close()

	if _, err := repo.CloneTemplateServerToLocal(srv.Client(), srv.URL, "TPL-NOPE"); err == nil {
		t.Fatal("code 不存在时应返回错误")
	}
}

// 未发布(审订中)的模版不可同步：scan 带 status=active，草稿不在列表内 → 报错而非静默克隆
func TestCloneTemplateServerToLocal_DraftRefused(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)
	srv := mockTemplateServer(t)
	defer srv.Close()

	if _, err := repo.CloneTemplateServerToLocal(srv.Client(), srv.URL, "TPL-LOCAL-DRAFT"); err == nil {
		t.Fatal("未发布模版应拒绝同步")
	}
}

// 列表：走模版服务器地址，过滤掉 TPL-PERSONAL-FILES 基础设施模版，且仅含已发布
func TestListTemplateServerActive(t *testing.T) {
	db := openTestDB(t)
	srv := mockTemplateServer(t)
	defer srv.Close()

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyTemplateServerEndpoint, srv.URL)
	cacheRepo := NewTemplateCacheRepository(db)
	fetcher := NewTemplateFetcher(cacheRepo, configRepo)

	list, err := fetcher.ListTemplateServerActive()
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应过滤掉 TPL-PERSONAL-FILES 后剩 1 条，实得 %d", len(list))
	}
	if list[0].TemplateCode != "TPL-LOCAL-001" {
		t.Fatalf("列表内容不对: %+v", list[0])
	}
}
