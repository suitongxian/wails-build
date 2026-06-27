package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// TestLocalTemplateHTTP 验证项目模版本地创作 CRUD 的 HTTP 闭环（片3）
func TestLocalTemplateHTTP(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 先建一个行业
	_, resp := jsonReq(t, r, "POST", "/business-classes", map[string]any{"name": "出版印刷"})
	classCode := dataMap(t, resp)["code"].(string)

	// 创建项目模版
	code, resp := jsonReq(t, r, "POST", "/templates", map[string]any{
		"class_code":        classCode,
		"scope":             "unit",
		"template_name":     "《明朝那些事儿》印刷计划",
		"short_code":        "MC-NSXS",
		"manager":           "刘老师",
		"sensitivity_level": "core",
		"owner":             "第一研究院",
	})
	if code != 200 || resp["success"] != true {
		t.Fatalf("创建模版失败: code=%d resp=%v", code, resp)
	}
	tpl := dataMap(t, resp)
	id := int64(tpl["id"].(float64))
	if tpl["origin"] != "local" {
		t.Fatalf("origin 应为 local: %v", tpl["origin"])
	}
	if tpl["template_code"] == "" || tpl["template_code"] == nil {
		t.Fatal("应返回自动生成的 template_code")
	}

	// 非法敏感级别被拒
	code, resp = jsonReq(t, r, "POST", "/templates", map[string]any{
		"template_name": "x", "sensitivity_level": "绝密",
	})
	if resp["success"] == true {
		t.Fatal("非法敏感级别应失败")
	}

	// authoring 列表含刚建的
	code, resp = jsonReqNoBody(t, r, "GET", "/templates/authoring")
	if code != 200 || resp["success"] != true {
		t.Fatalf("authoring 列表失败: code=%d resp=%v", code, resp)
	}
	list, _ := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("本地模版应有 1 个，实得 %d", len(list))
	}

	// 更新
	code, resp = jsonReq(t, r, "PUT", "/templates/"+itoa(id), map[string]any{
		"class_code": classCode, "scope": "department", "template_name": "改名了", "sensitivity_level": "important",
	})
	if code != 200 || resp["success"] != true {
		t.Fatalf("更新失败: code=%d resp=%v", code, resp)
	}

	// 删除
	code, resp = jsonReqNoBody(t, r, "DELETE", "/templates/"+itoa(id))
	if code != 200 || resp["success"] != true {
		t.Fatalf("删除失败: code=%d resp=%v", code, resp)
	}
	_, resp = jsonReqNoBody(t, r, "GET", "/templates/authoring")
	if rows, ok := resp["data"].([]interface{}); ok && len(rows) != 0 {
		t.Fatalf("删除后 authoring 列表应为空，实得 %d", len(rows))
	}
}

// TestStageTaskFileRuleHTTP 验证 事项/任务/标识 三层 CRUD 的 HTTP 闭环（片4）
func TestStageTaskFileRuleHTTP(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 建行业 + 项目模版
	_, resp := jsonReq(t, r, "POST", "/business-classes", map[string]any{"name": "出版印刷"})
	classCode := dataMap(t, resp)["code"].(string)
	_, resp = jsonReq(t, r, "POST", "/templates", map[string]any{
		"class_code": classCode, "scope": "unit", "template_name": "印刷计划", "sensitivity_level": "core",
	})
	tplID := int64(dataMap(t, resp)["id"].(float64))

	// 建事项
	code, resp := jsonReq(t, r, "POST", "/template-stages", map[string]any{
		"template_id": tplID, "name": "收稿登记", "manager": "刘老师", "members": "王老师,赵编辑",
	})
	if code != 200 || resp["success"] != true {
		t.Fatalf("建事项失败: code=%d resp=%v", code, resp)
	}
	stageID := int64(dataMap(t, resp)["id"].(float64))

	// 事项列表
	_, resp = jsonReqNoBody(t, r, "GET", "/template-stages?template_id="+itoa(tplID))
	if list, _ := resp["data"].([]interface{}); len(list) != 1 {
		t.Fatalf("事项列表应有 1 个，实得 %v", resp["data"])
	}

	// 建任务
	code, resp = jsonReq(t, r, "POST", "/template-tasks", map[string]any{
		"stage_id": stageID, "name": "客户原稿处理", "manager": "刘老师", "sensitivity_level": "core",
	})
	if code != 200 || resp["success"] != true {
		t.Fatalf("建任务失败: code=%d resp=%v", code, resp)
	}
	taskID := int64(dataMap(t, resp)["id"].(float64))

	// 建标识（带约束字段）
	code, resp = jsonReq(t, r, "POST", "/template-file-rules", map[string]any{
		"task_id": taskID, "file_name": "客户原稿", "data_state": "input", "required": true,
		"allowed_file_types": "PDF,DOC", "naming_pattern": "{书名}-原稿", "drafter": "刘老师", "sensitivity_level": "core",
	})
	if code != 200 || resp["success"] != true {
		t.Fatalf("建标识失败: code=%d resp=%v", code, resp)
	}
	fr := dataMap(t, resp)
	if fr["file_rule_code"] != "IN-001" {
		t.Fatalf("input 标识编码应为 IN-001，实得 %v", fr["file_rule_code"])
	}
	frID := int64(fr["id"].(float64))

	// 标识列表
	_, resp = jsonReqNoBody(t, r, "GET", "/template-file-rules?task_id="+itoa(taskID))
	if list, _ := resp["data"].([]interface{}); len(list) != 1 {
		t.Fatalf("标识列表应有 1 个，实得 %v", resp["data"])
	}

	// 非法 data_state 被拒
	_, resp = jsonReq(t, r, "POST", "/template-file-rules", map[string]any{
		"task_id": taskID, "file_name": "x", "data_state": "weird", "allowed_file_types": "PDF",
	})
	if resp["success"] == true {
		t.Fatal("非法 data_state 应失败")
	}

	// 更新标识
	code, resp = jsonReq(t, r, "PUT", "/template-file-rules/"+itoa(frID), map[string]any{
		"file_name": "客户原稿(改)", "data_state": "input", "allowed_file_types": "PDF", "required": true,
	})
	if code != 200 || resp["success"] != true {
		t.Fatalf("更新标识失败: %v", resp)
	}

	// 删事项 → 级联：任务、标识都没了
	code, resp = jsonReqNoBody(t, r, "DELETE", "/template-stages/"+itoa(stageID))
	if code != 200 || resp["success"] != true {
		t.Fatalf("删事项失败: %v", resp)
	}
	_, resp = jsonReqNoBody(t, r, "GET", "/template-tasks?stage_id="+itoa(stageID))
	if list, ok := resp["data"].([]interface{}); ok && len(list) != 0 {
		t.Fatalf("级联后任务应清空，实得 %d", len(list))
	}
	_, resp = jsonReqNoBody(t, r, "GET", "/template-file-rules?task_id="+itoa(taskID))
	if list, ok := resp["data"].([]interface{}); ok && len(list) != 0 {
		t.Fatalf("级联后标识应清空，实得 %d", len(list))
	}
}

// TestLocalTemplateTreeHTTP 验证五层树读取的 HTTP 闭环（片5）
func TestLocalTemplateTreeHTTP(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	_, resp := jsonReq(t, r, "POST", "/business-classes", map[string]any{"name": "出版印刷"})
	classCode := dataMap(t, resp)["code"].(string)
	_, resp = jsonReq(t, r, "POST", "/templates", map[string]any{
		"class_code": classCode, "scope": "unit", "template_name": "印刷计划", "sensitivity_level": "core",
	})
	tplID := int64(dataMap(t, resp)["id"].(float64))
	_, resp = jsonReq(t, r, "POST", "/template-stages", map[string]any{"template_id": tplID, "name": "收稿登记"})
	stageID := int64(dataMap(t, resp)["id"].(float64))
	_, resp = jsonReq(t, r, "POST", "/template-tasks", map[string]any{"stage_id": stageID, "name": "客户原稿处理"})
	taskID := int64(dataMap(t, resp)["id"].(float64))
	jsonReq(t, r, "POST", "/template-file-rules", map[string]any{
		"task_id": taskID, "file_name": "客户原稿", "data_state": "input", "allowed_file_types": "PDF",
	})

	code, resp := jsonReqNoBody(t, r, "GET", "/templates/"+itoa(tplID)+"/tree")
	if code != 200 || resp["success"] != true {
		t.Fatalf("读取树失败: code=%d resp=%v", code, resp)
	}
	tree := dataMap(t, resp)
	if dataMap(t, map[string]interface{}{"data": tree["template"]})["id"] == nil {
		t.Fatal("树应含 template 根")
	}
	stages, _ := tree["stages"].([]interface{})
	if len(stages) != 1 {
		t.Fatalf("应有 1 个事项，实得 %v", tree["stages"])
	}
	stage0 := stages[0].(map[string]interface{})
	tasks, _ := stage0["tasks"].([]interface{})
	if len(tasks) != 1 {
		t.Fatalf("应有 1 个任务，实得 %v", stage0["tasks"])
	}
	task0 := tasks[0].(map[string]interface{})
	rules, _ := task0["file_rules"].([]interface{})
	if len(rules) != 1 {
		t.Fatalf("应有 1 个标识，实得 %v", task0["file_rules"])
	}
}

// TestImportLocalTemplateHTTP 验证「导入模版（粘贴/上传 JSON）」HTTP 闭环：
// POST /templates/import 整棵五层树 → 本地重建为 origin=local 模版 → GET tree 校验五层齐全。
func TestImportLocalTemplateHTTP(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	tree := map[string]any{
		"template": map[string]any{
			"template_name":             "导入的印刷计划",
			"project_sensitivity_level": "core",
			"scope":                     "unit",
			"short_code":                "IMP-001",
			"manager":                   "刘老师",
			"owner":                     "第一研究院",
			"description":               "由 JSON 导入",
		},
		"stages": []any{
			map[string]any{
				"stage_name": "收稿登记",
				"manager":    "刘老师",
				"members":    "王老师,赵编辑",
				"tasks": []any{
					map[string]any{
						"task_name":         "客户原稿处理",
						"manager":           "刘老师",
						"sensitivity_level": "core",
						"file_rules": []any{
							map[string]any{
								"file_name":          "客户原稿",
								"data_state":         "input",
								"required":           1,
								"allowed_file_types": "PDF,DOC",
								"naming_pattern":     "{书名}-原稿",
							},
							map[string]any{
								"file_name":          "验证清单",
								"data_state":         "output",
								"required":           1,
								"allowed_file_types": "XLSX", // 与 GET tree 导出一致：逗号串
							},
						},
					},
				},
			},
		},
	}

	code, resp := jsonReq(t, r, "POST", "/templates/import", tree)
	if code != 200 || resp["success"] != true {
		t.Fatalf("导入失败: code=%d resp=%v", code, resp)
	}
	id := int64(dataMap(t, resp)["id"].(float64))
	if id == 0 {
		t.Fatal("应返回新本地模版 id")
	}

	// 列表里应是 origin=local 的一条
	_, resp = jsonReqNoBody(t, r, "GET", "/templates/authoring")
	list, _ := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("导入后本地模版应有 1 个，实得 %d", len(list))
	}
	if list[0].(map[string]interface{})["origin"] != "local" {
		t.Fatalf("导入模版 origin 应为 local: %v", list[0])
	}

	// 五层树校验：1 环节 → 1 任务 → 2 标识
	code, resp = jsonReqNoBody(t, r, "GET", "/templates/"+itoa(id)+"/tree")
	if code != 200 || resp["success"] != true {
		t.Fatalf("读取导入后树失败: code=%d resp=%v", code, resp)
	}
	tree2 := dataMap(t, resp)
	stages, _ := tree2["stages"].([]interface{})
	if len(stages) != 1 {
		t.Fatalf("应有 1 个环节，实得 %v", tree2["stages"])
	}
	tasks, _ := stages[0].(map[string]interface{})["tasks"].([]interface{})
	if len(tasks) != 1 {
		t.Fatalf("应有 1 个任务，实得 %v", stages[0])
	}
	rules, _ := tasks[0].(map[string]interface{})["file_rules"].([]interface{})
	if len(rules) != 2 {
		t.Fatalf("应有 2 个标识，实得 %v", tasks[0])
	}

	// 缺 template_name 应被拒
	_, resp = jsonReq(t, r, "POST", "/templates/import", map[string]any{
		"template": map[string]any{"project_sensitivity_level": "general"},
		"stages":   []any{map[string]any{"stage_name": "x"}},
	})
	if resp["success"] == true {
		t.Fatal("缺模版名称应导入失败")
	}

	// 无环节应被拒
	_, resp = jsonReq(t, r, "POST", "/templates/import", map[string]any{
		"template": map[string]any{"template_name": "无环节", "project_sensitivity_level": "general"},
		"stages":   []any{},
	})
	if resp["success"] == true {
		t.Fatal("无环节应导入失败")
	}
}

// TestImportTemplateManageExportFormat 验证兼容「模版管理平台导出」格式：
// 外层 {version,type,template:{...,stages:[...]}}（stages 嵌套在 template 内）+ required 为布尔值。
func TestImportTemplateManageExportFormat(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	payload := map[string]any{
		"version":    "1.0",
		"type":       "data-business-template",
		"exportedAt": "2026-06-15T00:00:00Z",
		"template": map[string]any{
			"template_name":             "平台导出的模版",
			"project_sensitivity_level": "general",
			"scope":                     "unit",
			"stages": []any{ // stages 嵌套在 template 内
				map[string]any{
					"stage_name": "收稿",
					"tasks": []any{
						map[string]any{
							"task_name": "录入",
							"file_rules": []any{
								map[string]any{
									"file_name":          "原稿",
									"data_state":         "input",
									"required":           true, // 布尔，而非 0/1
									"allowed_file_types": "PDF,DOCX",
								},
							},
						},
					},
				},
			},
		},
	}

	code, resp := jsonReq(t, r, "POST", "/templates/import", payload)
	if code != 200 || resp["success"] != true {
		t.Fatalf("平台导出格式导入失败: code=%d resp=%v", code, resp)
	}
	id := int64(dataMap(t, resp)["id"].(float64))

	// 校验五层重建 + required 落为 1（true）
	_, resp = jsonReqNoBody(t, r, "GET", "/templates/"+itoa(id)+"/tree")
	tree := dataMap(t, resp)
	stages, _ := tree["stages"].([]interface{})
	if len(stages) != 1 {
		t.Fatalf("应有 1 个环节，实得 %v", tree["stages"])
	}
	tasks, _ := stages[0].(map[string]interface{})["tasks"].([]interface{})
	rules, _ := tasks[0].(map[string]interface{})["file_rules"].([]interface{})
	if len(rules) != 1 {
		t.Fatalf("应有 1 个标识，实得 %v", tasks[0])
	}
	if rq, _ := rules[0].(map[string]interface{})["required"].(float64); rq != 1 {
		t.Fatalf("required=true 应落为 1，实得 %v", rules[0].(map[string]interface{})["required"])
	}
}

// TestPushTemplateHTTP 验证「推送到 manage」HTTP 闭环（片10）：读配置端点 → POST ingest → 回填
func TestPushTemplateHTTP(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// mock manage ingest 端点
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/templates/ingest" {
			_, _ = io.ReadAll(req.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"template_id":888}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, srv.URL)

	// 建一个本地模版
	_, resp := jsonReq(t, r, "POST", "/business-classes", map[string]any{"name": "出版印刷"})
	classCode := dataMap(t, resp)["code"].(string)
	_, resp = jsonReq(t, r, "POST", "/templates", map[string]any{
		"class_code": classCode, "scope": "unit", "template_name": "印刷计划", "sensitivity_level": "core",
	})
	tplID := int64(dataMap(t, resp)["id"].(float64))

	// 推送
	code, resp := jsonReqNoBody(t, r, "POST", "/templates/"+itoa(tplID)+"/push")
	if code != 200 || resp["success"] != true {
		t.Fatalf("推送失败: code=%d resp=%v", code, resp)
	}
	if int64(dataMap(t, resp)["remote_id"].(float64)) != 888 {
		t.Fatalf("应返回 remote_id=888，实得 %v", dataMap(t, resp)["remote_id"])
	}
}

// TestPublishAndInitiateGateHTTP 验证「是否发布」HTTP 闭环：
// 未发布不能立项 → POST /publish 发布 → 立项放行。
func TestPublishAndInitiateGateHTTP(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/templates/ingest" {
			_, _ = io.ReadAll(req.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"template_id":777}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, srv.URL)

	// 建本地模版（默认未发布）
	_, resp := jsonReq(t, r, "POST", "/templates", map[string]any{
		"scope": "unit", "template_name": "待发布模版", "sensitivity_level": "general",
	})
	tpl := dataMap(t, resp)
	tplID := int64(tpl["id"].(float64))
	if int(tpl["is_published"].(float64)) != 0 {
		t.Fatalf("新建应默认未发布，实得 %v", tpl["is_published"])
	}

	// 未发布立项 → 应失败
	_, resp = jsonReqNoBody(t, r, "POST", "/templates/"+itoa(tplID)+"/initiate")
	if resp["success"] == true {
		t.Fatal("未发布不应允许立项")
	}

	// 发布
	code, resp := jsonReq(t, r, "POST", "/templates/"+itoa(tplID)+"/publish", map[string]any{"published": true})
	if code != 200 || resp["success"] != true {
		t.Fatalf("发布失败: code=%d resp=%v", code, resp)
	}

	// 发布后立项 → 放行
	code, resp = jsonReqNoBody(t, r, "POST", "/templates/"+itoa(tplID)+"/initiate")
	if code != 200 || resp["success"] != true {
		t.Fatalf("发布后立项应成功: code=%d resp=%v", code, resp)
	}
}

// TestListManageUsersHTTP 验证 /manage-users 代理拉取（片：负责人下拉）
func TestListManageUsersHTTP(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/auth-users/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":[{"username":"liu","display_name":"刘老师","user_unit":"院","user_department":"档案处","status":"active"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, srv.URL)

	code, resp := jsonReqNoBody(t, r, "GET", "/manage-users")
	if code != 200 || resp["success"] != true {
		t.Fatalf("拉取 manage 用户失败: code=%d resp=%v", code, resp)
	}
	list, _ := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("应返回 1 个用户，实得 %v", resp["data"])
	}
	u := list[0].(map[string]interface{})
	if u["display_name"] != "刘老师" {
		t.Fatalf("用户字段不符: %v", u)
	}
}

// TestListManageBusinessClassesFromTemplateServer 验证「数据业务分类」下拉以模版管理平台
// (template-server, :19092) 为准——即使另配了 manage(:19091)，也只读 template-server。
func TestListManageBusinessClassesFromTemplateServer(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// template-manage：返回行业分类
	tplSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/business-classes/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":[{"code":"IND-009","name":"平台自定义行业"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer tplSrv.Close()
	// data-asset-manage：若被误读则返回不同数据，便于断言来源
	manageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/business-classes/list" {
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":[{"code":"IND-001","name":"数据端行业"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer manageSrv.Close()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyTemplateServerEndpoint, tplSrv.URL)
	cfg.SetValue(repository.KeyManageEndpoint, manageSrv.URL)

	code, resp := jsonReqNoBody(t, r, "GET", "/manage-business-classes")
	if code != 200 || resp["success"] != true {
		t.Fatalf("拉取行业分类失败: code=%d resp=%v", code, resp)
	}
	list, _ := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("应返回 1 个行业，实得 %v", resp["data"])
	}
	bc := list[0].(map[string]interface{})
	if bc["code"] != "IND-009" {
		t.Fatalf("行业分类应来自 template-server(IND-009)，实得 %v", bc)
	}
}
