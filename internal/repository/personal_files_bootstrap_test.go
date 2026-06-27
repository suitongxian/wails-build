package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBootstrapPersonalProjects_PullsTemplateAndCreatesProjects 验证：
//  1. 本地无 TPL-PERSONAL-FILES、配了 manage_endpoint 时，BootstrapPersonalProjects
//     会主动从 manage 拉模板
//  2. 拉完后 3 个 SYS-PERSONAL-* 项目自动建出来
func TestBootstrapPersonalProjects_PullsTemplateAndCreatesProjects(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	// 启 mock manage：响应 /api/templates/full
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/full" {
			http.NotFound(w, r)
			return
		}
		code := r.URL.Query().Get("code")
		ver := r.URL.Query().Get("version")
		if code != "TPL-PERSONAL-FILES" {
			http.Error(w, "not this code", http.StatusNotFound)
			return
		}
		// V2.0 模板 — 含必需的环节 + 文件规则
		resp := map[string]interface{}{
			"code":    0,
			"message": "ok",
			"data": map[string]interface{}{
				"template": map[string]interface{}{
					"id":                        1,
					"template_code":             "TPL-PERSONAL-FILES",
					"template_name":             "个人文件项目化管理模版",
					"template_version":          ver,
					"status":                    "active",
					"project_sensitivity_level": "general",
					"publisher":                 "system",
				},
				"stages": []map[string]interface{}{
					{
						"id":         101,
						"stage_code": "GR-DA",
						"stage_name": "个人归档",
						"stage_type": "record",
						"sort_order": 1,
						"file_rules": []map[string]interface{}{
							{"id": 201, "file_rule_code": "IN-001", "file_name": "来源", "data_state": "input", "allowed_file_types": "[\"*\"]"},
							{"id": 202, "file_rule_code": "OUT-001", "file_name": "定稿", "data_state": "output", "allowed_file_types": "[\"*\"]"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := NewSystemConfigRepository(db)
	cfg.SetValue(KeyManageEndpoint, srv.URL)

	// 前置：3 个项目不应存在
	if personalProjectsAllExist(db) {
		t.Fatal("前置：3 个 personal 项目不应已存在")
	}

	BootstrapPersonalProjects(db)

	if !personalProjectsAllExist(db) {
		t.Error("BootstrapPersonalProjects 后 3 个 personal 项目应已建好")
	}
}

func TestBootstrapPersonalProjects_NoEndpoint_NoOp(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	// 2026-05-22 migrations 现在 seed 默认 manage_endpoint，显式清掉以测试"无 endpoint"路径
	NewSystemConfigRepository(db).SetValue(KeyManageEndpoint, "")
	BootstrapPersonalProjects(db)
	if personalProjectsAllExist(db) {
		t.Error("未配 endpoint 时不应建出 personal 项目")
	}
}

func TestBootstrapPersonalProjects_TemplateAlreadyCached_BuildsImmediately(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	// 直接在本地缓存放一个 TPL-PERSONAL-FILES V1.0（模拟之前同步过）
	cache := NewTemplateCacheRepository(db)
	tplID, err := cache.SaveTemplate(SaveTemplateInput{
		RemoteID:                42,
		TemplateCode:            "TPL-PERSONAL-FILES",
		TemplateName:            "个人文件项目化管理模版",
		TemplateVersion:         "V1.0",
		Status:                  "active",
		ProjectSensitivityLevel: "general",
	})
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}
	// 至少加一个 stage 才能让 ensurePersonalFilesContext 完整建项目
	_, err = cache.SaveTemplateStage(SaveTemplateStageInput{
		TemplateID: tplID,
		RemoteID:   1001,
		StageCode:  "GR-DA",
		StageName:  "个人归档",
		StageType:  "record",
		SortOrder:  1,
	})
	if err != nil {
		t.Fatalf("seed stage: %v", err)
	}

	BootstrapPersonalProjects(db)

	if !personalProjectsAllExist(db) {
		t.Error("本地已有模板时 BootstrapPersonalProjects 应当场建项目，未依赖 endpoint")
	}
}
