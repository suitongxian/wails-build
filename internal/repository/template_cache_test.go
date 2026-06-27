package repository

import (
	"testing"
)

func TestTemplateCache_UpsertFlow(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateCacheRepository(db)

	// 1. 业务分类 upsert
	desc := "印刷和记录媒介复制业"
	if err := repo.SaveBusinessClass(1, "C23", "出版印刷", "industry", &desc); err != nil {
		t.Fatalf("save business class: %v", err)
	}
	// 重复 upsert 不报错
	if err := repo.SaveBusinessClass(1, "C23", "出版印刷-改名", "industry", nil); err != nil {
		t.Fatalf("re-save business class: %v", err)
	}

	// 2. 模版主表 upsert
	classCode := "C23"
	endpoint := "http://manage.local"
	scenario := "图书印刷"
	tplID, err := repo.SaveTemplate(SaveTemplateInput{
		RemoteID:                42,
		TemplateCode:            "TPL-PRINT-BOOK",
		TemplateName:            "书目印刷数据业务模版",
		TemplateVersion:         "V2.1",
		ClassCode:               &classCode,
		Scenario:                &scenario,
		Status:                  "active",
		ProjectSensitivityLevel: "important",
		SourceEndpoint:          &endpoint,
	})
	if err != nil {
		t.Fatalf("save template: %v", err)
	}
	if tplID == 0 {
		t.Fatal("expected non-zero local id")
	}

	// 重复 upsert 应当返回相同 ID
	tplID2, err := repo.SaveTemplate(SaveTemplateInput{
		RemoteID:                42,
		TemplateCode:            "TPL-PRINT-BOOK",
		TemplateName:            "书目印刷数据业务模版（修订）",
		TemplateVersion:         "V2.1",
		Status:                  "active",
		ProjectSensitivityLevel: "important",
	})
	if err != nil {
		t.Fatalf("re-save template: %v", err)
	}
	if tplID != tplID2 {
		t.Fatalf("upsert should return same id: %d vs %d", tplID, tplID2)
	}

	// 3. 工作环节 upsert
	stageID, err := repo.SaveTemplateStage(SaveTemplateStageInput{
		RemoteID:   100,
		TemplateID: tplID,
		StageCode:  "MZ-SG",
		StageName:  "收稿登记",
		StageType:  "process",
		SortOrder:  1,
	})
	if err != nil {
		t.Fatalf("save stage: %v", err)
	}
	stageID2, err := repo.SaveTemplateStage(SaveTemplateStageInput{
		RemoteID:   100,
		TemplateID: tplID,
		StageCode:  "MZ-SG",
		StageName:  "收稿登记（更名）",
		StageType:  "process",
		SortOrder:  1,
	})
	if err != nil {
		t.Fatalf("re-save stage: %v", err)
	}
	if stageID != stageID2 {
		t.Fatal("upsert stage should return same id")
	}

	// 4. 文件规则 upsert
	rid, err := repo.SaveTemplateFileRule(SaveTemplateFileRuleInput{
		RemoteID:         200,
		TemplateStageID:  stageID,
		FileRuleCode:     "IN-001",
		FileName:         "客户原稿",
		DataState:        "input",
		Required:         1,
		AllowedFileTypes: `["PDF"]`,
		SortOrder:        1,
	})
	if err != nil {
		t.Fatalf("save rule: %v", err)
	}
	if rid == 0 {
		t.Fatal("expected non-zero rule id")
	}

	// 5. 完整结构查询
	full, err := repo.GetFullTemplate(tplID)
	if err != nil {
		t.Fatalf("get full: %v", err)
	}
	if full.Template.TemplateCode != "TPL-PRINT-BOOK" {
		t.Fatalf("wrong template: %+v", full.Template)
	}
	if len(full.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(full.Stages))
	}
	if len(full.Stages[0].FileRules) != 1 {
		t.Fatalf("expected 1 file rule, got %d", len(full.Stages[0].FileRules))
	}

	// 6. ListTemplates with status filter
	all, err := repo.ListTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 template, got %d", len(all))
	}
	active, err := repo.ListTemplates("active")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active, got %d", len(active))
	}
	deprecated, err := repo.ListTemplates("deprecated")
	if err != nil {
		t.Fatal(err)
	}
	if len(deprecated) != 0 {
		t.Fatalf("expected 0 deprecated, got %d", len(deprecated))
	}
}
