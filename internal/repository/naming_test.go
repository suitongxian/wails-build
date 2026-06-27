package repository

import (
	"strings"
	"testing"
	"time"
)

func TestRenderNamingPattern_SystemVars(t *testing.T) {
	ctx := NamingContext{
		ProjectCode: "MC-NSXS-2024-001",
		ProjectName: "明朝那些事儿印刷",
		StageCode:   "MZ-PB",
		StageName:   "排版",
		LocalCode:   "OUT-001",
		DisplayName: "排版完成稿",
		VersionNo:   "V2.0",
		UserName:    "张三",
		Date:        time.Date(2024, 5, 9, 18, 30, 45, 0, time.UTC),
	}

	cases := []struct {
		pattern  string
		expected string
	}{
		{"{project_code}-{stage_code}-{local_code}", "MC-NSXS-2024-001-MZ-PB-OUT-001"},
		{"{display_name}-V{version}", "排版完成稿-V2.0"},
		{"{stage_name}-{date}", "排版-20240509"},
		{"排版稿-{date}", "排版稿-20240509"},
		{"{project_name}-{user}", "明朝那些事儿印刷-张三"},
		{"{yyyymm}-{local_code}", "202405-OUT-001"},
	}

	for _, c := range cases {
		got := RenderNamingPattern(c.pattern, ctx)
		if got != c.expected {
			t.Errorf("pattern %q\n  got:      %q\n  expected: %q", c.pattern, got, c.expected)
		}
	}
}

func TestRenderNamingPattern_ChineseVars(t *testing.T) {
	ctx := NamingContext{
		ProjectCode: "MC-2024",
		StageCode:   "MZ-PB",
		LocalCode:   "OUT-001",
		DisplayName: "排版完成稿",
		VersionNo:   "V1.0",
		Date:        time.Date(2024, 5, 9, 0, 0, 0, 0, time.UTC),
	}
	got := RenderNamingPattern("排版稿-{书名}-V{版本}", NamingContext{
		ProjectCode: ctx.ProjectCode,
		LocalCode:   ctx.LocalCode,
		VersionNo:   ctx.VersionNo,
		Extras:      map[string]string{"书名": "明朝那些事儿"},
	})
	if got != "排版稿-明朝那些事儿-V1.0" {
		t.Fatalf("got %q", got)
	}
}

// TestRenderNamingPattern_UnknownVarsFallbackToDisplayName
// V1：未识别的业务变量回退到 display_name（避免文件名出现 `{书名}` 字面值）。
// 注：连续重复字符会被 sanitize 折叠（"原稿-原稿" → "原稿-原稿" 仍保留，但已无 {} 字面值）
func TestRenderNamingPattern_UnknownVarsFallbackToDisplayName(t *testing.T) {
	got := RenderNamingPattern("原稿-{书名}", NamingContext{
		DisplayName: "客户原稿",
	})
	if strings.Contains(got, "{") || strings.Contains(got, "}") {
		t.Fatalf("unknown var should not leave braces in filename, got %q", got)
	}
	if !strings.Contains(got, "客户原稿") {
		t.Fatalf("expected fallback to display_name, got %q", got)
	}
}

// 当显式提供 Extras 时，业务变量应当被精确替换，不走 display_name 回退
func TestRenderNamingPattern_ExtrasOverrideDisplayName(t *testing.T) {
	got := RenderNamingPattern("原稿-{书名}", NamingContext{
		DisplayName: "客户原稿",
		Extras:      map[string]string{"书名": "明朝那些事儿"},
	})
	if !strings.Contains(got, "明朝那些事儿") {
		t.Fatalf("extras should win over display_name fallback, got %q", got)
	}
	if strings.Contains(got, "客户原稿") {
		t.Fatalf("display_name should not appear when extras has value, got %q", got)
	}
}

func TestRenderNamingPattern_DefaultPattern(t *testing.T) {
	got := RenderNamingPattern("", NamingContext{
		DisplayName: "客户原稿",
		VersionNo:   "V1.0",
	})
	if got != "客户原稿-V1.0" {
		t.Fatalf("default pattern fallback wrong: %s", got)
	}
}

func TestRenderNamingPattern_Sanitization(t *testing.T) {
	got := RenderNamingPattern("file/name<>?:", NamingContext{})
	if strings.ContainsAny(got, `/\<>?:|*"`) {
		t.Fatalf("expected sanitized, got %q", got)
	}
}

func TestIsAllowedFileType(t *testing.T) {
	cases := []struct {
		ext      string
		allowed  string
		expected bool
	}{
		{"pdf", `["PDF"]`, true},
		{".pdf", `["PDF"]`, true},
		{"PDF", `["pdf"]`, true},
		{"docx", `["PDF","DOC","DOCX"]`, true},
		{"jpg", `["PDF","DOC"]`, false},
		{"", `["PDF"]`, false},
		{"pdf", `[]`, false},
		{"pdf", `not-json`, false},
	}
	for _, c := range cases {
		got := IsAllowedFileType(c.ext, c.allowed)
		if got != c.expected {
			t.Errorf("ext=%q allowed=%q got %v expected %v", c.ext, c.allowed, got, c.expected)
		}
	}
}
