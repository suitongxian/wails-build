package repository

import (
	"context"
	"encoding/json"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/ai"
)

// DBCatalogProvider V4-Q1-a 实现 ai.CatalogProvider — 从本地 SQLite 拉所有
// active/draft 项目下的 (项目+环节+模版规则) 候选三元组。
//
// SQL JOIN 一次拉全量（项目 × 环节 × 规则），单机数据量小（典型 < 1000 行）
// 不需要缓存。如果未来上百万行，可改为按 importance/extension 预过滤。
type DBCatalogProvider struct {
	DB *sqlx.DB
}

func NewDBCatalogProvider(db *sqlx.DB) *DBCatalogProvider {
	return &DBCatalogProvider{DB: db}
}

// rawRow JOIN 行（私有，AI 包外不见）
type rawRow struct {
	ProjectID        int64   `db:"project_id"`
	ProjectCode      string  `db:"project_code"`
	ProjectName      string  `db:"project_name"`
	TemplateCode     string  `db:"template_code"`
	StageID          int64   `db:"stage_id"`
	StageCode        string  `db:"stage_code"`
	StageName        string  `db:"stage_name"`
	FileRuleCode     string  `db:"file_rule_code"`
	FileName         string  `db:"file_name"`
	DataState        string  `db:"data_state"`
	AllowedFileTypes string  `db:"allowed_file_types"`
	NamingPattern    *string `db:"naming_pattern"`
}

// GetCandidates 实现 ai.CatalogProvider
func (p *DBCatalogProvider) GetCandidates(ctx context.Context) ([]ai.ProjectStageRuleSnapshot, error) {
	var rows []rawRow
	err := p.DB.SelectContext(ctx, &rows, `
		SELECT
			dp.id AS project_id,
			dp.project_code,
			dp.project_name,
			dp.template_code,
			ps.id AS stage_id,
			ps.stage_code,
			ps.stage_name,
			tfr.file_rule_code,
			tfr.file_name,
			tfr.data_state,
			tfr.allowed_file_types,
			tfr.naming_pattern
		FROM data_projects dp
		JOIN project_stages ps ON ps.project_id = dp.id AND ps.disable = 0
		JOIN template_file_rules tfr ON tfr.template_stage_id = ps.template_stage_id AND tfr.disable = 0
		WHERE dp.disable = 0
		  AND dp.status IN ('active', 'draft')
		ORDER BY dp.id, ps.sort_order, tfr.sort_order
	`)
	if err != nil {
		return nil, err
	}

	out := make([]ai.ProjectStageRuleSnapshot, 0, len(rows))
	for _, r := range rows {
		// 解 allowed_file_types JSON
		var types []string
		if r.AllowedFileTypes != "" {
			_ = json.Unmarshal([]byte(r.AllowedFileTypes), &types)
		}
		naming := ""
		if r.NamingPattern != nil {
			naming = *r.NamingPattern
		}
		out = append(out, ai.ProjectStageRuleSnapshot{
			ProjectID:        r.ProjectID,
			ProjectCode:      r.ProjectCode,
			ProjectName:      r.ProjectName,
			TemplateCode:     r.TemplateCode,
			StageID:          r.StageID,
			StageCode:        r.StageCode,
			StageName:        r.StageName,
			FileRuleCode:     r.FileRuleCode,
			FileName:         r.FileName,
			DataState:        r.DataState,
			AllowedFileTypes: types,
			NamingPattern:    naming,
		})
	}
	return out, nil
}

// 编译期断言实现 ai.CatalogProvider 接口
var _ ai.CatalogProvider = (*DBCatalogProvider)(nil)
