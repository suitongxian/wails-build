package repository

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 从模版管理平台(template-server)按 code 拉取并写入缓存：
// 复现并验证「在线列表来自 :19092，但同步却去了 :19091」的修复——
// 现在按 code 回到同一台服务器，取到带各级 code 的五层结构并入缓存。
func TestTemplateFetcher_FetchFromTemplateServer(t *testing.T) {
	db := openTestDB(t)
	cacheRepo := NewTemplateCacheRepository(db)
	configRepo := NewSystemConfigRepository(db)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/local-templates/list":
			// 平台列表（id 属于平台库）
			fmt.Fprint(w, `{"code":0,"message":"ok","data":[
				{"id":7,"template_code":"SMYS-PRINT","template_name":"书目印刷计划","template_version":"V1.0","status":"active"}
			]}`)
		case "/api/local-templates/tree":
			if r.URL.Query().Get("id") != "7" {
				t.Errorf("tree 应按平台 id=7 取，实得 %s", r.URL.RawQuery)
			}
			resp := map[string]any{
				"code": 0, "message": "ok",
				"data": map[string]any{
					"template": map[string]any{
						"template_code": "SMYS-PRINT", "template_name": "书目印刷计划",
						"template_version": "V1.0", "project_sensitivity_level": "core_secret",
						"status": "active", "publisher": "中心",
					},
					"stages": []map[string]any{
						{"stage_code": "S1", "stage_name": "收稿登记", "stage_type": "process", "sort_order": 1,
							"tasks": []map[string]any{
								{"task_code": "TK-1", "task_name": "客户材料接收", "sort_order": 1,
									"file_rules": []map[string]any{
										{"file_rule_code": "IN-001", "file_name": "客户原稿", "data_state": "input", "required": 1, "allowed_file_types": "PDF", "sort_order": 1},
									}},
							}},
						{"stage_code": "S2", "stage_name": "排版", "stage_type": "process", "sort_order": 2,
							"tasks": []map[string]any{
								{"task_code": "TK-2", "task_name": "排版加工", "sort_order": 1,
									"file_rules": []map[string]any{
										{"file_rule_code": "OUT-001", "file_name": "排版完成稿", "data_state": "output", "required": 1, "allowed_file_types": "PDF", "sort_order": 1},
									}},
							}},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("非预期路径: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	configRepo.SetValue(KeyTemplateServerEndpoint, srv.URL)

	fetcher := NewTemplateFetcher(cacheRepo, configRepo)
	localID, err := fetcher.FetchFromTemplateServer("SMYS-PRINT", "V1.0")
	if err != nil {
		t.Fatalf("FetchFromTemplateServer 失败: %v", err)
	}
	if localID == 0 {
		t.Fatal("应返回非零本地 id")
	}

	full, err := cacheRepo.GetFullTemplate(localID)
	if err != nil {
		t.Fatalf("读取缓存失败: %v", err)
	}
	if full.Template.TemplateCode != "SMYS-PRINT" {
		t.Fatalf("template_code 不符: %s", full.Template.TemplateCode)
	}
	// core_secret → core 映射
	if full.Template.ProjectSensitivityLevel != "core" {
		t.Fatalf("敏感级应映射为 core，实得 %s", full.Template.ProjectSensitivityLevel)
	}
	if len(full.Stages) != 2 {
		t.Fatalf("应有 2 个环节，实得 %d", len(full.Stages))
	}
	// 文件规则带 task 关联
	var found bool
	for _, s := range full.Stages {
		if s.StageCode == "S1" {
			if len(s.FileRules) != 1 || s.FileRules[0].FileRuleCode != "IN-001" {
				t.Fatalf("S1 文件规则不符: %+v", s.FileRules)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("未找到环节 S1")
	}
}
