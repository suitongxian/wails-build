package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// 个人项目一键归档：按桶定级复制到本地九宫格个人夹；input 跳过；幂等。
func TestArchiveProjectLocal(t *testing.T) {
	root := t.TempDir()
	ws := NewProjectWorkspace(root)
	const proj = "甲项目-XM-2026-0001"
	if _, err := ws.CreateTaskDir(proj, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	write := func(bucket, name, content string) {
		p := filepath.Join(ws.TaskStateDir(proj, "S1", "TK1", bucket), name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("input", "工作依据.txt", "IN")     // 不归档
	write("process", "过程稿.txt", "PROC")   // general → 资料
	write("output", "定稿.txt", "FINAL")     // 抬到重要 → 档案
	write("reference", "外部.txt", "REF")     // external/general → 资料
	if err := WriteRefGrade(ws.TaskStateDir(proj, "S1", "TK1", "reference"), "外部.txt", "external", "general"); err != nil {
		t.Fatal(err)
	}

	// 个人夹默认就放在工作空间根下（personalRoot = workspaceRoot）
	res, err := ArchiveProjectLocal(root, root, proj, "甲项目", "general")
	if err != nil {
		t.Fatal(err)
	}
	if res.Archived != 3 {
		t.Fatalf("应归档 3 个（过程/定稿/参考），实得 %d（errors=%v）", res.Archived, res.Errors)
	}

	mustExist := func(rel string) {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("应归档到 %s：%v", rel, err)
		}
	}
	mustExist(filepath.Join("个人一般文件夹", "甲项目", "过程稿.txt"))
	mustExist(filepath.Join("个人重要文件夹", "甲项目", "定稿.txt")) // 定稿不低于重要 → 档案
	mustExist(filepath.Join("个人一般文件夹", "甲项目", "外部.txt"))

	// 工作依据不应被归档到任何个人夹
	if _, err := os.Stat(filepath.Join(root, "个人一般文件夹", "甲项目", "工作依据.txt")); err == nil {
		t.Fatal("工作依据文件不应被归档")
	}

	// 幂等：再次归档 → 全部跳过
	res2, err := ArchiveProjectLocal(root, root, proj, "甲项目", "general")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Archived != 0 || res2.Skipped != 3 {
		t.Fatalf("二次应全跳过（archived=0 skipped=3），实得 archived=%d skipped=%d", res2.Archived, res2.Skipped)
	}
}

// 列出本机个人{级别}文件夹下的归档文件（供档案在线阅卷·个人展示）。
func TestListPersonalArchiveFiles(t *testing.T) {
	personalRoot := t.TempDir()
	// 模拟一键归档落点：个人重要文件夹/甲项目/外部规范.txt
	dir := filepath.Join(personalRoot, "个人重要文件夹", "甲项目")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "外部规范.txt"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 隐藏元数据文件不应列出
	if err := os.WriteFile(filepath.Join(dir, ".archive-grade.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	list := ListPersonalArchiveFiles(personalRoot, "important")
	if len(list) != 1 {
		t.Fatalf("应列出 1 个重要级文件，实得 %d", len(list))
	}
	if list[0].FileName != "外部规范.txt" || list[0].ProjectName != "甲项目" || list[0].Folder != "个人重要文件夹" {
		t.Fatalf("列出内容不符：%+v", list[0])
	}
	// 其他级别为空
	if len(ListPersonalArchiveFiles(personalRoot, "core")) != 0 {
		t.Fatal("核心级应为空")
	}
}

// 核心级项目：过程与定稿都进个人核心文件夹。
func TestArchiveProjectLocal_CoreToSecret(t *testing.T) {
	root := t.TempDir()
	ws := NewProjectWorkspace(root)
	const proj = "密项目-XM-2026-0002"
	if _, err := ws.CreateTaskDir(proj, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(proj, "S1", "TK1", "process"), "核心过程.txt"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ArchiveProjectLocal(root, root, proj, "密项目", "core")
	if err != nil {
		t.Fatal(err)
	}
	if res.Archived != 1 {
		t.Fatalf("应归档 1，实得 %d", res.Archived)
	}
	if _, err := os.Stat(filepath.Join(root, "个人核心文件夹", "密项目", "核心过程.txt")); err != nil {
		t.Fatalf("核心级过程应入个人核心文件夹：%v", err)
	}
}
