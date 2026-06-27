package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTemplateFetcher_FetchByCode(t *testing.T) {
	db := openTestDB(t)
	cacheRepo := NewTemplateCacheRepository(db)
	configRepo := NewSystemConfigRepository(db)

	// 启动 mock manage 服务器
	mockResp := ManageFullResponse{
		Code:    0,
		Message: "success",
		Data: &ManageFullStructure{
			Template: &ManageTemplate{
				ID:                      1,
				TemplateCode:            "TPL-PRINT-BOOK",
				TemplateName:            "书目印刷",
				TemplateVersion:         "V2.1",
				Publisher:               "provider",
				Status:                  "active",
				ProjectSensitivityLevel: "important",
			},
			BusinessClass: &ManageBusinessClass{
				ID:   2,
				Code: "C23",
				Name: "出版印刷",
				Type: "industry",
			},
			Stages: []ManageStage{
				{
					ID:        10,
					StageCode: "MZ-SG",
					StageName: "收稿登记",
					StageType: "process",
					SortOrder: 1,
					FileRules: []ManageFileRule{
						{ID: 100, FileRuleCode: "IN-001", FileName: "客户原稿", DataState: "input", Required: 1, AllowedFileTypes: `["PDF"]`, SortOrder: 1},
						{ID: 101, FileRuleCode: "OUT-001", FileName: "收稿凭证", DataState: "output", Required: 1, AllowedFileTypes: `["PDF"]`, SortOrder: 2},
					},
				},
				{
					ID:        11,
					StageCode: "MZ-PB",
					StageName: "排版",
					StageType: "process",
					SortOrder: 2,
					FileRules: []ManageFileRule{
						{ID: 200, FileRuleCode: "OUT-001", FileName: "排版完成稿", DataState: "output", Required: 1, AllowedFileTypes: `["PDF"]`, SortOrder: 1},
					},
				},
			},
		},
	}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/full" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("code") != "TPL-PRINT-BOOK" || r.URL.Query().Get("version") != "V2.1" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockResp)
	}))
	defer mockServer.Close()

	configRepo.SetValue(KeyManageEndpoint, mockServer.URL)

	fetcher := NewTemplateFetcher(cacheRepo, configRepo)
	localID, err := fetcher.FetchByCode("TPL-PRINT-BOOK", "V2.1")
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if localID == 0 {
		t.Fatal("expected non-zero local id")
	}

	// 验证本地缓存
	full, err := cacheRepo.GetFullTemplate(localID)
	if err != nil {
		t.Fatalf("get full failed: %v", err)
	}
	if full.Template.TemplateCode != "TPL-PRINT-BOOK" {
		t.Fatalf("wrong template_code: %s", full.Template.TemplateCode)
	}
	if len(full.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(full.Stages))
	}
	if len(full.Stages[0].FileRules) != 2 {
		t.Fatalf("stage 0 expected 2 rules, got %d", len(full.Stages[0].FileRules))
	}
	// 验证 source_endpoint 写入
	if full.Template.SourceEndpoint == nil || *full.Template.SourceEndpoint == "" {
		t.Fatal("source_endpoint should be set")
	}
	// 二次拉取（更新场景）— 模版数据变更后再拉，应当 upsert
	mockResp.Data.Template.TemplateName = "书目印刷-V2.1.1"
	localID2, err := fetcher.FetchByCode("TPL-PRINT-BOOK", "V2.1")
	if err != nil {
		t.Fatalf("re-fetch failed: %v", err)
	}
	if localID2 != localID {
		t.Fatalf("upsert should return same id, got %d vs %d", localID2, localID)
	}
	updatedTpl, _ := cacheRepo.FindTemplateByID(localID)
	if updatedTpl.TemplateName != "书目印刷-V2.1.1" {
		t.Fatalf("upsert did not update name: %s", updatedTpl.TemplateName)
	}
}

func TestTemplateFetcher_NoEndpoint(t *testing.T) {
	db := openTestDB(t)
	cacheRepo := NewTemplateCacheRepository(db)
	configRepo := NewSystemConfigRepository(db)

	fetcher := NewTemplateFetcher(cacheRepo, configRepo)
	if _, err := fetcher.FetchByCode("X", "V1.0"); err == nil {
		t.Fatal("expected error when endpoint missing")
	}
}

func TestTemplateFetcher_ManageError(t *testing.T) {
	db := openTestDB(t)
	cacheRepo := NewTemplateCacheRepository(db)
	configRepo := NewSystemConfigRepository(db)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ManageFullResponse{Code: 404, Message: "模版不存在", Data: nil})
	}))
	defer mockServer.Close()
	configRepo.SetValue(KeyManageEndpoint, mockServer.URL)

	fetcher := NewTemplateFetcher(cacheRepo, configRepo)
	if _, err := fetcher.FetchByCode("X", "V1.0"); err == nil {
		t.Fatal("expected error for non-zero code response")
	}
}
