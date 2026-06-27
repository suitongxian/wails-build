// Package ai 定义 AI 辅助模块的端口接口。
//
// 文档依据：
//   - 需求 §1.1.8 "为后续 AI 自动归目、特征提取、模版推荐和目录上报预留接口"
//   - 需求 §7.9 AI 辅助模块：4 类能力，输入输出列在表格里
//   - 设计 §5.9 AI 预留模块（端口命名）
//   - 需求 §17.7 "AI 输出只能作为建议，关键字段必须由用户确认"
//
// V3-7 MVP 范围：仅定义 Port 接口 + NoOp 默认实现（接口可调用但不返回结果）。
// 不接入任何具体模型——文档明示 "MVP 不强制调用模型"。
// 后续可由不同实现（HTTP 调用 / 本地模型 / 其他厂商 API）替换。
package ai

import "context"

// TemplateRecommendInput §7.9 模版推荐能力的输入
type TemplateRecommendInput struct {
	ProjectName  string `json:"project_name"`
	Industry     string `json:"industry"`
	TaskSummary  string `json:"task_summary"`
}

// TemplateRecommendation 单条模版推荐
type TemplateRecommendation struct {
	TemplateCode    string  `json:"template_code"`
	TemplateVersion string  `json:"template_version"`
	Score           float64 `json:"score"`   // 0~1
	Reason          string  `json:"reason"`  // 推荐理由（给用户看）
}

// AutoClassifyInput §7.9 自动归目能力的输入
type AutoClassifyInput struct {
	FileName  string            `json:"file_name"`
	Path      string            `json:"path,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Summary   string            `json:"summary,omitempty"`
}

// ClassificationSuggestion §7.9 推荐归到哪个项目/环节/文件规则
//
// 简版 §4.3 要求归目颗粒到 (项目, 工作环节, 文件版本规则, 数据态)。
// 同一 stage_code 在多个项目实例下可能并存（如多个 TPL-PRINT-BOOK 项目
// 都有 MZ-SG 环节），因此需要 ProjectID 明确指向具体项目实例。
type ClassificationSuggestion struct {
	ProjectID    int64   `json:"project_id"`
	ProjectCode  string  `json:"project_code"`
	StageCode    string  `json:"stage_code"`
	StageName    string  `json:"stage_name"`
	FileRuleCode string  `json:"file_rule_code"`
	FileName     string  `json:"file_name"`     // 模版规则定义的文件名（用户可见）
	DataState    string  `json:"data_state"`    // input / process / output
	Confidence   float64 `json:"confidence"`    // 0~1
	Reason       string  `json:"reason"`        // 命中原因（给用户看）
	MatchedRules []string `json:"matched_rules,omitempty"` // 命中的具体规则列表
}

// SummarySuggestInput §7.9 摘要建议输入
type SummarySuggestInput struct {
	FileName    string `json:"file_name"`
	BusinessCtx string `json:"business_ctx"` // 业务上下文（项目/环节/文件版本）
}

// SummarySuggestion §7.9 底账摘要建议
type SummarySuggestion struct {
	Summary    string  `json:"summary"`
	Confidence float64 `json:"confidence"`
}

// AnomalyDetectInput §7.9 异常识别输入
type AnomalyDetectInput struct {
	LedgerEvents     []map[string]any `json:"ledger_events,omitempty"`
	TransferRecords  []map[string]any `json:"transfer_records,omitempty"`
	PermissionLogs   []map[string]any `json:"permission_logs,omitempty"`
}

// AnomalyHint §7.9 异常提示
type AnomalyHint struct {
	Severity string `json:"severity"` // info / warning / critical
	Message  string `json:"message"`
	Subject  string `json:"subject,omitempty"`
}

// =============================================================================
// Port 接口（与 设计 §5.9 命名对齐）
// =============================================================================

// TemplateRecommendPort §7.9 + 设计 §5.9 模版推荐端口
type TemplateRecommendPort interface {
	Recommend(ctx context.Context, in TemplateRecommendInput) ([]TemplateRecommendation, error)
}

// AutoClassifyPort §7.9 + 设计 §5.9 自动归目端口
type AutoClassifyPort interface {
	Classify(ctx context.Context, in AutoClassifyInput) ([]ClassificationSuggestion, error)
}

// SummarySuggestPort §7.9 + 设计 §5.9 摘要建议端口
type SummarySuggestPort interface {
	Suggest(ctx context.Context, in SummarySuggestInput) (SummarySuggestion, error)
}

// AnomalyDetectPort §7.9 异常识别端口
type AnomalyDetectPort interface {
	Detect(ctx context.Context, in AnomalyDetectInput) ([]AnomalyHint, error)
}
