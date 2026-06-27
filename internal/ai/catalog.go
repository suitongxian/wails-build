package ai

import "context"

// ProjectStageRuleSnapshot 归目候选项 — 一条候选 = (项目 + 环节 + 文件规则)
//
// AutoClassifyPort 实现按此 snapshot 列表打分，挑出最匹配的若干条返回。
// 不直接依赖 DB 类型，方便测试时构造 fake snapshot。
type ProjectStageRuleSnapshot struct {
	ProjectID        int64
	ProjectCode      string
	ProjectName      string
	TemplateCode     string
	StageID          int64
	StageCode        string
	StageName        string
	FileRuleCode     string
	FileName         string
	DataState        string   // input / process / output
	AllowedFileTypes []string // 从 template_file_rules.allowed_file_types JSON 解析
	NamingPattern    string   // 模版命名规则，含 {书名}/{日期} 等变量
}

// CatalogProvider 提供归目候选项的来源
//
// 生产实现读 DB；测试用 fake。
type CatalogProvider interface {
	// GetCandidates 返回所有可作为归目目标的 (项目+环节+规则) 候选
	//
	// 调用方应保证只返回 active/draft 状态的项目。
	GetCandidates(ctx context.Context) ([]ProjectStageRuleSnapshot, error)
}
