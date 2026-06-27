package ai

import (
	"context"
	"strings"
	"testing"
)

// fakeCatalog 测试用 catalog provider
type fakeCatalog struct {
	items []ProjectStageRuleSnapshot
}

func (f *fakeCatalog) GetCandidates(ctx context.Context) ([]ProjectStageRuleSnapshot, error) {
	return f.items, nil
}

func makeCatalog(items ...ProjectStageRuleSnapshot) *fakeCatalog {
	return &fakeCatalog{items: items}
}

// V4-Q1-a 扩展名直接匹配
func TestRuleClassifier_ExtMatch(t *testing.T) {
	cat := makeCatalog(
		ProjectStageRuleSnapshot{
			ProjectID: 1, ProjectCode: "P1", ProjectName: "测试项目",
			StageCode: "ST1", StageName: "排版",
			FileRuleCode: "PRC-001", FileName: "排版临时文件", DataState: "process",
			AllowedFileTypes: []string{"PSD", "AI", "INDD"},
			NamingPattern:    "排版-{书名}-临时-{版本}",
		},
		ProjectStageRuleSnapshot{
			ProjectID: 1, ProjectCode: "P1", ProjectName: "测试项目",
			StageCode: "ST1", StageName: "排版",
			FileRuleCode: "OUT-001", FileName: "排版完成稿", DataState: "output",
			AllowedFileTypes: []string{"PDF"},
			NamingPattern:    "排版稿-{书名}-V{版本}",
		},
	)
	a := NewRuleBasedClassifyAdapter(cat)

	// 上传 a.psd —— 只 PRC-001 扩展名匹配
	out, err := a.Classify(context.Background(), AutoClassifyInput{
		FileName: "draft.psd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("应只有 1 条匹配（PRC-001 含 PSD），got %d", len(out))
	}
	if out[0].FileRuleCode != "PRC-001" {
		t.Errorf("应匹配 PRC-001, got %s", out[0].FileRuleCode)
	}
	if out[0].Confidence < 0.3 {
		t.Errorf("仅扩展名匹配，confidence 应 >= 0.3, got %f", out[0].Confidence)
	}
}

// V4-Q1-a 多信号叠加分数更高
func TestRuleClassifier_MultiSignalRanking(t *testing.T) {
	cat := makeCatalog(
		ProjectStageRuleSnapshot{
			ProjectID: 1, ProjectCode: "MC-NSXS-2024", ProjectName: "明朝那些事印刷计划",
			StageCode: "MZ-PB", StageName: "排版",
			FileRuleCode: "OUT-001", FileName: "排版完成稿", DataState: "output",
			AllowedFileTypes: []string{"PDF"},
			NamingPattern:    "排版稿-{书名}-V{版本}",
		},
		ProjectStageRuleSnapshot{
			ProjectID: 2, ProjectCode: "P-OTHER", ProjectName: "其他项目",
			StageCode: "ST1", StageName: "测试",
			FileRuleCode: "OUT-001", FileName: "排版完成稿", DataState: "output",
			AllowedFileTypes: []string{"PDF"},
			NamingPattern:    "排版稿-{书名}-V{版本}",
		},
	)
	a := NewRuleBasedClassifyAdapter(cat)

	// 文件名含明朝/排版字样 + .pdf
	out, _ := a.Classify(context.Background(), AutoClassifyInput{
		FileName: "排版稿-明朝-V2.pdf",
		Path:     "/Users/x/projects/MC-NSXS-2024/output/",
	})
	if len(out) < 2 {
		t.Fatalf("应当返回多条建议, got %d", len(out))
	}
	// 第一条应是 MC-NSXS-2024 项目（命中项目名/路径）
	if out[0].ProjectCode != "MC-NSXS-2024" {
		t.Errorf("最匹配应为 MC-NSXS-2024，got %s", out[0].ProjectCode)
	}
	// 第一条 confidence 应明显高于第二条
	if out[0].Confidence <= out[1].Confidence {
		t.Errorf("多信号项目分数应更高：%f vs %f", out[0].Confidence, out[1].Confidence)
	}
}

// V4-Q1-a 扩展名不匹配 → 0 分（不返回）
func TestRuleClassifier_NoMatchExt(t *testing.T) {
	cat := makeCatalog(
		ProjectStageRuleSnapshot{
			ProjectID: 1, FileRuleCode: "OUT-001", FileName: "完成稿",
			AllowedFileTypes: []string{"PDF"},
		},
	)
	a := NewRuleBasedClassifyAdapter(cat)
	out, _ := a.Classify(context.Background(), AutoClassifyInput{FileName: "x.docx"})
	if len(out) != 0 {
		t.Errorf("扩展名不匹配且无其他信号应返 0, got %d", len(out))
	}
}

// V4-Q1-a allowed_file_types = ['*'] 不限格式
func TestRuleClassifier_WildcardExt(t *testing.T) {
	cat := makeCatalog(
		ProjectStageRuleSnapshot{
			ProjectID: 1, ProjectCode: "SYS-PERSONAL-CORE", ProjectName: "个人核心级文件管理",
			StageCode: "GR-DA", StageName: "个人归档",
			FileRuleCode: "OUT-001", FileName: "归档定稿", DataState: "output",
			AllowedFileTypes: []string{"*"},
		},
	)
	a := NewRuleBasedClassifyAdapter(cat)
	out, _ := a.Classify(context.Background(), AutoClassifyInput{FileName: "任意文件.xlsx"})
	if len(out) != 1 {
		t.Fatalf("wildcard 应允许任意扩展名, got %d", len(out))
	}
}

// V4-Q1-a 上限 0.95
func TestRuleClassifier_ConfidenceCap(t *testing.T) {
	cat := makeCatalog(
		ProjectStageRuleSnapshot{
			ProjectID: 1, ProjectCode: "P-EXACT", ProjectName: "明朝那些事印刷",
			StageCode: "MZ-PB", StageName: "排版",
			FileRuleCode: "OUT-001", FileName: "排版完成稿", DataState: "output",
			AllowedFileTypes: []string{"PDF"},
			NamingPattern:    "排版稿-{书名}-V{版本}",
		},
	)
	a := NewRuleBasedClassifyAdapter(cat)
	out, _ := a.Classify(context.Background(), AutoClassifyInput{
		FileName: "排版稿-明朝-V2.pdf",
		Path:     "/Users/x/projects/P-EXACT/排版/output/",
		Summary:  "明朝 书名 版本 2",
	})
	if len(out) == 0 {
		t.Fatal("应有匹配")
	}
	if out[0].Confidence > 0.95 {
		t.Errorf("confidence 不应超过 0.95 (留 5%% 余量), got %f", out[0].Confidence)
	}
}

// V4-Q1-a topN 限制返回数量
func TestRuleClassifier_TopN(t *testing.T) {
	items := []ProjectStageRuleSnapshot{}
	for i := 0; i < 10; i++ {
		items = append(items, ProjectStageRuleSnapshot{
			ProjectID: int64(i), ProjectCode: "P", FileRuleCode: "OUT-001",
			AllowedFileTypes: []string{"PDF"},
		})
	}
	a := NewRuleBasedClassifyAdapter(&fakeCatalog{items: items})
	a.SetTopN(3)
	out, _ := a.Classify(context.Background(), AutoClassifyInput{FileName: "x.pdf"})
	if len(out) != 3 {
		t.Errorf("应只返回 3 条 (topN), got %d", len(out))
	}
}

// V4-Q1-a 空文件名 → 0 结果
func TestRuleClassifier_EmptyFileName(t *testing.T) {
	cat := makeCatalog(ProjectStageRuleSnapshot{ProjectID: 1, AllowedFileTypes: []string{"PDF"}})
	a := NewRuleBasedClassifyAdapter(cat)
	out, _ := a.Classify(context.Background(), AutoClassifyInput{FileName: ""})
	if len(out) != 0 {
		t.Errorf("空 file_name 应返 0, got %d", len(out))
	}
}

// V4-Q1-a tokenizeChinese 单元
func TestTokenizeChinese(t *testing.T) {
	out := tokenizeChinese("排版临时文件")
	if !containsStr(out, "排版临时文件") {
		t.Errorf("整段应被保留: %v", out)
	}
	out2 := tokenizeChinese("MC-NSXS-2024_明朝.pdf")
	if !containsStr(out2, "明朝") {
		t.Errorf("应切出 '明朝': %v", out2)
	}
}

// V4-Q1-a extractPatternVars
func TestExtractPatternVars(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"排版稿-{书名}-V{版本}", []string{"书名", "版本"}},
		{"plain text", nil},
		{"{a}{b}{c}", []string{"a", "b", "c"}},
		{"", nil},
	}
	for _, c := range cases {
		got := extractPatternVars(c.in)
		if !equalStrSlice(got, c.want) {
			t.Errorf("extractPatternVars(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRuleClassifier_MimeMatch(t *testing.T) {
	cat := makeCatalog(ProjectStageRuleSnapshot{
		ProjectID: 1, ProjectCode: "P", ProjectName: "测试项目",
		StageCode: "S1", StageName: "环节",
		FileRuleCode: "R", FileName: "数据报表", DataState: "output",
		AllowedFileTypes: []string{"xlsx"},
	})
	a := NewRuleBasedClassifyAdapter(cat)
	ctx := context.Background()

	in := AutoClassifyInput{
		FileName: "anything.xlsx",
		Metadata: map[string]string{
			"mime": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		},
	}
	sug, err := a.Classify(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(sug) == 0 {
		t.Fatal("expected suggestions")
	}

	inNoMime := AutoClassifyInput{FileName: "anything.xlsx"}
	sug2, _ := a.Classify(ctx, inNoMime)
	if len(sug2) == 0 {
		t.Fatal("expected suggestions for no-mime variant")
	}

	if sug[0].Confidence <= sug2[0].Confidence {
		t.Errorf("mime match should boost confidence: with=%.2f without=%.2f",
			sug[0].Confidence, sug2[0].Confidence)
	}
}

func TestRuleClassifier_SiblingCountMatch(t *testing.T) {
	cat := makeCatalog(ProjectStageRuleSnapshot{
		ProjectID: 1, ProjectCode: "BATCH", ProjectName: "批量归档",
		StageCode: "S", StageName: "环节",
		FileRuleCode: "R", FileName: "批量文件", DataState: "output",
		AllowedFileTypes: []string{"*"},
	})
	a := NewRuleBasedClassifyAdapter(cat)
	ctx := context.Background()

	inMany := AutoClassifyInput{
		FileName: "report.pdf",
		Metadata: map[string]string{"sibling_count": "10"},
	}
	sug, _ := a.Classify(ctx, inMany)
	if len(sug) == 0 {
		t.Fatal("expected suggestions")
	}

	inFew := AutoClassifyInput{
		FileName: "report.pdf",
		Metadata: map[string]string{"sibling_count": "0"},
	}
	sug2, _ := a.Classify(ctx, inFew)

	if sug[0].Confidence <= sug2[0].Confidence {
		t.Errorf("high sibling_count should boost confidence on '*' rules")
	}
}

func TestRuleClassifier_BodyKeywordMatch(t *testing.T) {
	cat := makeCatalog(ProjectStageRuleSnapshot{
		ProjectID: 1, ProjectCode: "P", ProjectName: "项目",
		StageCode: "S", StageName: "审校",
		FileRuleCode: "R", FileName: "审校意见", DataState: "output",
		AllowedFileTypes: []string{"*"},
	})
	a := NewRuleBasedClassifyAdapter(cat)
	ctx := context.Background()

	inWithBody := AutoClassifyInput{
		FileName: "random.txt",
		Summary:  "这份文件是审校意见的初稿，审校人是张三",
	}
	sug, _ := a.Classify(ctx, inWithBody)
	if len(sug) == 0 {
		t.Fatal("expected suggestions")
	}

	inNoBody := AutoClassifyInput{FileName: "random.txt"}
	sug2, _ := a.Classify(ctx, inNoBody)

	if sug[0].Confidence <= sug2[0].Confidence {
		t.Errorf("body keyword match should boost: with=%.2f without=%.2f",
			sug[0].Confidence, sug2[0].Confidence)
	}

	// reason 应包含正文关键词命中描述（"正文含"）
	if !strings.Contains(sug[0].Reason, "正文含") {
		t.Errorf("reason should mention 正文含, got: %s", sug[0].Reason)
	}
}
