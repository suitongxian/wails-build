package scanner

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestExcludeDirs_SkipsWorkspaceByName 验证按目录名排除：工作空间目录(project_root)下的文件不被通用扫描扫到。
// 这是主路径(完成→自动归档)与兜底扫描分域的关键，避免抢文件。
func TestExcludeDirs_SkipsWorkspaceByName(t *testing.T) {
	tmp := t.TempDir()
	// 工作空间目录（模拟 project_root），里面有产出文件
	managed := filepath.Join(tmp, "data-asset-projects", "TPL-X", "stages", "STG-001", "output")
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managed, "managed.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 工作空间外的散落文件
	if err := os.WriteFile(filepath.Join(tmp, "scattered.pdf"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStreamingFileScanner()
	var mu sync.Mutex
	var seen []string
	_, err := s.ScanWithCallback(
		StreamingScanOptions{
			Directory:   tmp,
			Extensions:  []string{".pdf"},
			ExcludeDirs: []string{"data-asset-projects"}, // 排除工作空间目录名
		},
		func(path string) error {
			mu.Lock()
			seen = append(seen, filepath.Base(path))
			mu.Unlock()
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("scan err: %v", err)
	}
	for _, name := range seen {
		if name == "managed.pdf" {
			t.Fatalf("工作空间里的文件不应被通用扫描扫到，实得 %v", seen)
		}
	}
	found := false
	for _, name := range seen {
		if name == "scattered.pdf" {
			found = true
		}
	}
	if !found {
		t.Fatalf("散落文件应被扫到，实得 %v", seen)
	}
}

// TestExcludePaths_OnlyExcludesProjectSubtree 验证按全路径前缀排除：
// 只排除"项目目录"子树(主路径管辖)，工作空间内项目目录之外的散文件照常扫到(走认领归档)，
// 且不会因目录重名误伤(比通用 ExcludeDirs 按名匹配更精确)。
func TestExcludePaths_OnlyExcludesProjectSubtree(t *testing.T) {
	root := t.TempDir() // 模拟工作空间 /root/data
	// 项目目录(应排除)：root/TPL-0001/stages/STG-001/output/managed.pdf
	managed := filepath.Join(root, "TPL-0001", "stages", "STG-001", "output")
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managed, "managed.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 工作空间根目录散文件(不在项目目录下，应被扫到 → 走认领)
	if err := os.WriteFile(filepath.Join(root, "loose.pdf"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 非项目子目录里的文件(也应被扫到)
	other := filepath.Join(root, "misc")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "note.pdf"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStreamingFileScanner()
	var mu sync.Mutex
	var seen []string
	_, err := s.ScanWithCallback(
		StreamingScanOptions{
			Directory:    root,
			Extensions:   []string{".pdf"},
			ExcludePaths: []string{filepath.Join(root, "TPL-0001")}, // 只排除项目目录子树
		},
		func(path string) error {
			mu.Lock()
			seen = append(seen, filepath.Base(path))
			mu.Unlock()
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("scan err: %v", err)
	}
	has := func(n string) bool {
		for _, s := range seen {
			if s == n {
				return true
			}
		}
		return false
	}
	if has("managed.pdf") {
		t.Fatalf("项目目录内文件应被排除(归主路径)，实得 %v", seen)
	}
	if !has("loose.pdf") {
		t.Fatalf("根目录散文件应被扫到(走认领归档)，实得 %v", seen)
	}
	if !has("note.pdf") {
		t.Fatalf("非项目目录文件应被扫到，实得 %v", seen)
	}
}
