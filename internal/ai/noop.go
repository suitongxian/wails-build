package ai

import "context"

// NoOpAdapter 是文档 §7.9 "MVP 阶段只做接口预留，不强制实现模型调用" 的默认实现。
//
// 所有方法返回空结果 + nil error，让上游业务代码可以无条件注入而不报错；
// 真正需要 AI 能力时换成具体实现（OpenAI / 本地模型 / 企业 AI 平台等）。
type NoOpAdapter struct{}

// NewNoOpAdapter 实例化（单例即可，零状态）
func NewNoOpAdapter() *NoOpAdapter { return &NoOpAdapter{} }

func (a *NoOpAdapter) Recommend(ctx context.Context, in TemplateRecommendInput) ([]TemplateRecommendation, error) {
	return nil, nil
}

func (a *NoOpAdapter) Classify(ctx context.Context, in AutoClassifyInput) ([]ClassificationSuggestion, error) {
	return nil, nil
}

func (a *NoOpAdapter) Suggest(ctx context.Context, in SummarySuggestInput) (SummarySuggestion, error) {
	return SummarySuggestion{}, nil
}

func (a *NoOpAdapter) Detect(ctx context.Context, in AnomalyDetectInput) ([]AnomalyHint, error) {
	return nil, nil
}

// 编译期保证 NoOpAdapter 实现了全部 4 个 Port
var (
	_ TemplateRecommendPort = (*NoOpAdapter)(nil)
	_ AutoClassifyPort      = (*NoOpAdapter)(nil)
	_ SummarySuggestPort    = (*NoOpAdapter)(nil)
	_ AnomalyDetectPort     = (*NoOpAdapter)(nil)
)
