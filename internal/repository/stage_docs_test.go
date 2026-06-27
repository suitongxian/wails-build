package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStageDocsReadWriteList 验证在线编辑过程文档：保存路径由模版目录自动决定，
// 读写往返一致，列表含空占位，且文件名防目录穿越。
func TestStageDocsReadWriteList(t *testing.T) {
	db := openTestDB(t)
	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)

	const tc, sc = "TPL-DOC", "STG-001"

	// 尚未开始工作（目录不存在）→ 列表为空、读取为空
	if docs, err := ListStageProcessDocs(db, tc, sc); err != nil || len(docs) != 0 {
		t.Fatalf("无目录应空列表: docs=%v err=%v", docs, err)
	}
	if s, err := ReadStageProcessDoc(db, tc, sc, "草稿.txt"); err != nil || s != "" {
		t.Fatalf("不存在应空内容: %q err=%v", s, err)
	}

	// 保存 → 自动落到 process/ 目录
	path, err := WriteStageProcessDoc(db, tc, sc, "草稿.txt", "你好，世界")
	if err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	wantDir := NewProjectWorkspace(root).StageStateDir(tc, sc, "process")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("应存到环节 process 目录\n got %s\nwant 在 %s 下", path, wantDir)
	}

	// 读回一致
	if s, err := ReadStageProcessDoc(db, tc, sc, "草稿.txt"); err != nil || s != "你好，世界" {
		t.Fatalf("读写不一致: %q err=%v", s, err)
	}

	// 覆盖保存
	if _, err := WriteStageProcessDoc(db, tc, sc, "草稿.txt", "改过了"); err != nil {
		t.Fatal(err)
	}
	if s, _ := ReadStageProcessDoc(db, tc, sc, "草稿.txt"); s != "改过了" {
		t.Fatalf("覆盖保存未生效: %q", s)
	}

	// 列表含该文档（非空）
	docs, err := ListStageProcessDocs(db, tc, sc)
	if err != nil || len(docs) != 1 || docs[0].Name != "草稿.txt" || docs[0].Empty {
		t.Fatalf("列表应含 1 个非空文档: %v err=%v", docs, err)
	}

	// 空占位也应被列出（Empty=true）
	if err := os.WriteFile(filepath.Join(wantDir, "占位.txt"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	docs, _ = ListStageProcessDocs(db, tc, sc)
	if len(docs) != 2 {
		t.Fatalf("应列出 2 个文档（含空占位），实得 %v", docs)
	}

	// 防目录穿越：带路径分隔的文件名应被拒绝
	for _, bad := range []string{"../escape.txt", "sub/dir.txt", ""} {
		if _, err := WriteStageProcessDoc(db, tc, sc, bad, "x"); err == nil {
			t.Fatalf("非法文件名应被拒绝: %q", bad)
		}
		if _, err := ReadStageProcessDoc(db, tc, sc, bad); err == nil {
			t.Fatalf("非法文件名读取应被拒绝: %q", bad)
		}
	}
}
