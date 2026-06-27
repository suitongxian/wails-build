package repository

import (
	"path/filepath"
	"testing"
)

// 参考文件导入时由导入者做「归类定级声明」：类别决定默认级别，可手动改级。
func TestDefaultLevelForCategory(t *testing.T) {
	cases := map[string]string{
		"internal": "important", // 内部资料默认重要级
		"external": "general",   // 外部资料默认一般级
		"public":   "general",   // 公开资料默认一般级
		"":         "general",   // 未声明默认一般级
		"乱填的":      "general",
	}
	for cat, want := range cases {
		if got := DefaultLevelForCategory(cat); got != want {
			t.Fatalf("类别 %q 默认级别应为 %q，实得 %q", cat, want, got)
		}
	}
}

// sidecar 落盘 + 回读：按文件名记录每个参考文件的类别与级别。
func TestRefGradeWriteRead(t *testing.T) {
	dir := t.TempDir()

	// 写入两条
	if err := WriteRefGrade(dir, "外部白皮书.docx", "external", "general"); err != nil {
		t.Fatal(err)
	}
	if err := WriteRefGrade(dir, "内部纪要.docx", "internal", "important"); err != nil {
		t.Fatal(err)
	}

	g, ok := ReadRefGrade(dir, "内部纪要.docx")
	if !ok {
		t.Fatal("应能读回内部纪要的定级")
	}
	if g.Category != "internal" || g.SensitivityLevel != "important" {
		t.Fatalf("读回定级不符：%+v", g)
	}

	// 不存在的文件 → 读不到
	if _, ok := ReadRefGrade(dir, "查无此文件.docx"); ok {
		t.Fatal("不存在的文件不应读到定级")
	}

	// sidecar 是隐藏 json，落在该目录下
	if _, ok := ReadRefGrade(dir, "外部白皮书.docx"); !ok {
		t.Fatal("第一条也应仍在（写第二条不能覆盖第一条）")
	}
	if filepath.Base(RefGradeSidecarPath(dir)) != refGradeFileName {
		t.Fatalf("sidecar 文件名应为 %s", refGradeFileName)
	}
}

// 级别归一 + 级别→九宫格分区（保密/档案/资料）。
func TestSensitivityToArchiveZone(t *testing.T) {
	cases := map[string]string{
		"core":        "保密",
		"core_secret": "保密",
		"important":   "档案",
		"general":     "资料",
		"":            "资料",
	}
	for lvl, want := range cases {
		if got := SensitivityToArchiveZone(lvl); got != want {
			t.Fatalf("级别 %q 应落 %q 区，实得 %q", lvl, want, got)
		}
	}
}
