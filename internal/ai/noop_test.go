package ai

import (
	"context"
	"testing"
)

// V3-7 NoOp 实现的 4 个端口都能调用不 panic、不 error，返回零值
func TestNoOpAdapter_AllPortsCallable(t *testing.T) {
	a := NewNoOpAdapter()
	ctx := context.Background()

	if out, err := a.Recommend(ctx, TemplateRecommendInput{ProjectName: "X"}); err != nil || out != nil {
		t.Errorf("Recommend NoOp 应返 nil/nil，got %v / %v", out, err)
	}

	if out, err := a.Classify(ctx, AutoClassifyInput{FileName: "x.pdf"}); err != nil || out != nil {
		t.Errorf("Classify NoOp 应返 nil/nil，got %v / %v", out, err)
	}

	if out, err := a.Suggest(ctx, SummarySuggestInput{FileName: "x.pdf"}); err != nil || out.Summary != "" {
		t.Errorf("Suggest NoOp 应返空 SummarySuggestion/nil，got %+v / %v", out, err)
	}

	if out, err := a.Detect(ctx, AnomalyDetectInput{}); err != nil || out != nil {
		t.Errorf("Detect NoOp 应返 nil/nil，got %v / %v", out, err)
	}
}

// V3-7 编译期类型保证（这里只是 runtime 的额外断言；如果 ports.go var 块出错编译就挂了）
func TestNoOpAdapter_ImplementsAllPorts(t *testing.T) {
	var _ TemplateRecommendPort = NewNoOpAdapter()
	var _ AutoClassifyPort = NewNoOpAdapter()
	var _ SummarySuggestPort = NewNoOpAdapter()
	var _ AnomalyDetectPort = NewNoOpAdapter()
}
