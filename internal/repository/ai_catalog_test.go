package repository

import (
	"context"
	"testing"
)

// V4-Q1-a DBCatalogProvider 端到端：建好项目 → 模版 → 规则后能正确返回候选
func TestDBCatalogProvider_ReturnsActiveProjectCandidates(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	if err := ensurePersonalFilesContext(db); err != nil {
		t.Fatal(err)
	}

	provider := NewDBCatalogProvider(db)
	candidates, err := provider.GetCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 3 个内置项目 × 1 环节 × 3 规则 = 9 个候选
	if len(candidates) != 9 {
		t.Errorf("应有 9 个候选 (3 项目 × 3 规则), got %d", len(candidates))
	}

	// 验证每条候选字段完整
	for _, c := range candidates {
		if c.ProjectID == 0 || c.ProjectCode == "" {
			t.Errorf("候选缺项目信息: %+v", c)
		}
		if c.StageCode == "" || c.FileRuleCode == "" {
			t.Errorf("候选缺环节/规则: %+v", c)
		}
		if c.DataState == "" {
			t.Errorf("候选缺 data_state: %+v", c)
		}
		// 个人文件模版 allowed_file_types = ["*"]
		if len(c.AllowedFileTypes) == 0 {
			t.Errorf("候选缺 allowed_file_types: %+v", c)
		}
	}
}

// V4-Q1-a archived/cancelled 项目不返回
func TestDBCatalogProvider_ExcludesArchivedAndCancelled(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	ensurePersonalFilesContext(db)

	// 把核心级项目改为 archived
	db.Exec(`UPDATE data_projects SET status = 'archived' WHERE project_code = ?`, PersonalCoreProjectCode)
	// 把重要级改为 cancelled
	db.Exec(`UPDATE data_projects SET status = 'cancelled' WHERE project_code = ?`, PersonalImportantProjectCode)

	provider := NewDBCatalogProvider(db)
	candidates, _ := provider.GetCandidates(context.Background())

	// 只剩 general 级项目的 3 个候选
	if len(candidates) != 3 {
		t.Errorf("archived/cancelled 应排除，期望 3 个，got %d", len(candidates))
	}
	for _, c := range candidates {
		if c.ProjectCode != PersonalGeneralProjectCode {
			t.Errorf("不应出现 %s（已 archived/cancelled）", c.ProjectCode)
		}
	}
}

// V4-Q1-a 无任何项目时返回空
func TestDBCatalogProvider_EmptyDB(t *testing.T) {
	db := openTestDB(t)
	provider := NewDBCatalogProvider(db)
	candidates, err := provider.GetCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Errorf("空 DB 应返回 0 候选, got %d", len(candidates))
	}
}

// V4-Q1-a allowed_file_types JSON 解析
func TestDBCatalogProvider_ParsesAllowedTypes(t *testing.T) {
	db := openTestDB(t)
	seedMockPersonalFilesTemplate(t, db)
	ensurePersonalFilesContext(db)
	provider := NewDBCatalogProvider(db)
	candidates, _ := provider.GetCandidates(context.Background())
	if len(candidates) == 0 {
		t.Skip("no candidates")
	}
	// 个人文件模版用 ["*"]
	if candidates[0].AllowedFileTypes[0] != "*" {
		t.Errorf("应解析出 '*' 通配符, got %v", candidates[0].AllowedFileTypes)
	}
}
