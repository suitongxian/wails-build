package repository

import "testing"

// 按桶定级：input 跳过；reference 用声明级；process=项目级；output=max(项目级,重要)。
func TestBucketArchiveLevel(t *testing.T) {
	type tc struct {
		bucket, projSens, refLevel string
		wantLevel                  string
		wantArchive                bool
	}
	cases := []tc{
		{"input", "core", "", "", false},                  // 工作依据不归档
		{"reference", "core", "general", "general", true}, // 参考按声明
		{"reference", "core", "", "general", true},        // 参考无声明默认一般
		{"process", "important", "", "important", true},   // 过程=项目级
		{"process", "general", "", "general", true},
		{"output", "general", "", "important", true}, // 定稿不低于重要
		{"output", "core", "", "core", true},         // 定稿=项目级(更高)
		{"output", "important", "", "important", true},
	}
	for _, c := range cases {
		lvl, arch := BucketArchiveLevel(c.bucket, c.projSens, c.refLevel)
		if arch != c.wantArchive || (arch && lvl != c.wantLevel) {
			t.Fatalf("桶=%s 项目=%s 声明=%s → 期望(级别=%s,归档=%v) 实得(级别=%s,归档=%v)",
				c.bucket, c.projSens, c.refLevel, c.wantLevel, c.wantArchive, lvl, arch)
		}
	}
}

// scope → 容器路由：个人本地、部门/单位上云、行业跳过。
func TestScopeRouting(t *testing.T) {
	cases := map[string]struct {
		route  ArchiveRoute
		prefix string
		suffix string
	}{
		"person":     {RouteLocal, "个人", "文件夹"},
		"department": {RouteCloud, "部门", "文件柜"},
		"unit":       {RouteCloud, "单位", "文件室"},
		"industry":   {RouteSkip, "", ""},
		"":           {RouteSkip, "", ""},
	}
	for scope, want := range cases {
		r, prefix, suffix := ScopeRoute(scope)
		if r != want.route || prefix != want.prefix || suffix != want.suffix {
			t.Fatalf("scope=%s → 期望(%v,%s,%s) 实得(%v,%s,%s)", scope, want.route, want.prefix, want.suffix, r, prefix, suffix)
		}
	}
}

// 九宫格目标文件夹名：前缀(个人/部门/单位)+级别(核心/重要/一般)+后缀(文件夹/文件柜/文件室)。
func TestNineGridFolder(t *testing.T) {
	cases := map[[2]string]string{
		{"person", "core"}:      "个人核心文件夹",
		{"person", "important"}: "个人重要文件夹",
		{"person", "general"}:   "个人一般文件夹",
		{"department", "core"}:  "部门核心文件柜",
		{"unit", "general"}:     "单位一般文件室",
	}
	for k, want := range cases {
		if got := NineGridFolder(k[0], k[1]); got != want {
			t.Fatalf("scope=%s level=%s → 期望 %s 实得 %s", k[0], k[1], want, got)
		}
	}
}
