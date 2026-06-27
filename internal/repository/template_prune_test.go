package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 远端把模版环节改少后，scan 再次同步应镜像远端：旧的孤儿环节/任务/文件规则被清理。
// 复现并修复「manage 种子改成 2 个环节，scan 分工仍显示一堆环节」。
func TestTemplateFetcher_PrunesRemovedStages(t *testing.T) {
	db := openTestDB(t)
	cacheRepo := NewTemplateCacheRepository(db)
	configRepo := NewSystemConfigRepository(db)

	// 初版：3 个环节，首环节含 1 任务 + 2 文件规则。
	resp := ManageFullResponse{
		Code: 0, Message: "ok",
		Data: &ManageFullStructure{
			Template: &ManageTemplate{
				ID: 1, TemplateCode: "TPL-PRINT-BOOK", TemplateName: "书目印刷计划",
				TemplateVersion: "V1.0", Publisher: "p", Status: "active",
			},
			Stages: []ManageStage{
				{ID: 10, StageCode: "S1", StageName: "收稿登记", StageType: "process", SortOrder: 1,
					FileRules: []ManageFileRule{
						{ID: 100, FileRuleCode: "IN-001", FileName: "原稿", DataState: "input", Required: 1, SortOrder: 1},
						{ID: 101, FileRuleCode: "OUT-001", FileName: "收稿凭证", DataState: "output", Required: 1, SortOrder: 2},
					}},
				{ID: 11, StageCode: "S2", StageName: "排版", StageType: "process", SortOrder: 2,
					FileRules: []ManageFileRule{{ID: 200, FileRuleCode: "OUT-001", FileName: "排版稿", DataState: "output", Required: 1, SortOrder: 1}}},
				{ID: 12, StageCode: "S3", StageName: "审校", StageType: "process", SortOrder: 3,
					FileRules: []ManageFileRule{{ID: 300, FileRuleCode: "OUT-001", FileName: "审校稿", DataState: "output", Required: 1, SortOrder: 1}}},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	configRepo.SetValue(KeyManageEndpoint, srv.URL)

	fetcher := NewTemplateFetcher(cacheRepo, configRepo)
	localID, err := fetcher.FetchByCode("TPL-PRINT-BOOK", "V1.0")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	full, _ := cacheRepo.GetFullTemplate(localID)
	if len(full.Stages) != 3 {
		t.Fatalf("初版应有 3 个环节，实得 %d", len(full.Stages))
	}

	// 远端改少：只剩 2 个环节，且 S1 的文件规则减为 1 个。
	resp.Data.Stages = resp.Data.Stages[:2]
	resp.Data.Stages[0].FileRules = resp.Data.Stages[0].FileRules[:1] // 删掉 OUT-001

	localID2, err := fetcher.FetchByCode("TPL-PRINT-BOOK", "V1.0")
	if err != nil {
		t.Fatalf("re-fetch: %v", err)
	}
	if localID2 != localID {
		t.Fatalf("应 upsert 同一模版，got %d vs %d", localID2, localID)
	}

	full2, _ := cacheRepo.GetFullTemplate(localID2)
	if len(full2.Stages) != 2 {
		t.Fatalf("镜像远端后应只剩 2 个环节，实得 %d（孤儿环节未清理）", len(full2.Stages))
	}
	for _, s := range full2.Stages {
		if s.StageCode == "S3" {
			t.Fatalf("已删除的环节 S3 仍残留")
		}
		if s.StageCode == "S1" && len(s.FileRules) != 1 {
			t.Fatalf("S1 文件规则应被裁剪为 1，实得 %d", len(s.FileRules))
		}
	}
}
