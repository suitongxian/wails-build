package httpd

import (
	"strings"
	"testing"
)

// 回归测试：scan.go parseExtensions 必须保留前导点。
// 否则与 scanner walker.go 用 filepath.Ext() 返回的 ".pdf" 这种带点后缀不匹配，
// 扫描器会把所有文件都过滤掉，结果 0 个文件。
//
// 历史 bug：旧版本 parseExtensions 用 strings.TrimPrefix(ext, ".") 去掉前导点，
// 之前没暴露是因为 workspace 与 scan_area_path 重合时 CollectWorkspaceSuffixes
// 会从 workspace 收集带点后缀（".pdf"）合并进 extSet 救场。自动创建独立空 workspace
// 后救场消失，0 文件 bug 显形。
func TestParseExtensions_KeepsLeadingDot(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{
			input: ".doc,.docx,.pdf",
			want:  []string{".doc", ".docx", ".pdf"},
		},
		{
			// 用户写无点也要补
			input: "doc,docx,pdf",
			want:  []string{".doc", ".docx", ".pdf"},
		},
		{
			// 混合 + 空白
			input: " .pdf , docx ,.XLS",
			want:  []string{".pdf", ".docx", ".xls"},
		},
	}
	for _, c := range cases {
		got := parseExtensions(c.input)
		if len(got) != len(c.want) {
			t.Errorf("input %q: got %v, want %v", c.input, got, c.want)
			continue
		}
		for i, g := range got {
			if g != c.want[i] {
				t.Errorf("input %q index %d: got %q, want %q", c.input, i, g, c.want[i])
			}
		}
		// 每个都必须以 . 开头
		for _, g := range got {
			if !strings.HasPrefix(g, ".") {
				t.Errorf("input %q: ext %q missing leading dot", c.input, g)
			}
		}
	}
}

// 空字符串保持现状（scanner.ParseExtensions 返回 nil）
func TestParseExtensions_EmptyReturnsNil(t *testing.T) {
	if got := parseExtensions(""); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}
