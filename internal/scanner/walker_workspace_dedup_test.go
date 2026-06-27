package scanner

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// 验证：当 atomic.go 把 workspaceStats 提到上层算后传下来时，
// CountFilesWithExtensions 和 ScanWithCallback 不再各自调 CollectWorkspaceSuffixes。
// 通过把 workspace 路径设成不存在但 PrecomputedWorkspaceStats 给非 nil 值来验证。
// 如果还有"忘了用 precomputed"的代码路径，会去 walk 不存在的目录然后行为变化（虽不报错但会打 log）。
func TestStreamingScanner_RespectsPrecomputedWorkspaceStats(t *testing.T) {
	tmp := t.TempDir()
	// scan_area_path 有 1 个 .pdf
	if err := os.WriteFile(filepath.Join(tmp, "a.pdf"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// fake workspace path that does NOT exist on disk
	bogusWorkspace := "/definitely/does/not/exist/zzz"
	// 但我们传 precomputed，应当被采用
	precomputed := &WorkspaceStats{
		WorkspacePath:      bogusWorkspace,
		WorkspaceFileCount: 0,
		WorkspaceSuffixes:  []string{".docx"},
		SuffixCounts:       map[string]int{".docx": 0},
	}

	s := NewStreamingFileScanner()
	res, err := s.CountFilesWithExtensions(StreamingScanOptions{
		Directory:                 tmp,
		Extensions:                []string{".pdf"},
		Workspace:                 bogusWorkspace,
		PrecomputedWorkspaceStats: precomputed,
	})
	if err != nil {
		t.Fatalf("CountFilesWithExtensions err: %v", err)
	}
	// finalExtensions 应当包含 control_type ∪ precomputed.WorkspaceSuffixes
	wantExts := map[string]bool{".pdf": true, ".docx": true}
	for _, ext := range res.UsedExtensions {
		if !wantExts[ext] {
			t.Errorf("unexpected ext in usedExtensions: %s", ext)
		}
	}
	if len(res.UsedExtensions) != len(wantExts) {
		t.Errorf("UsedExtensions = %v, want %v", res.UsedExtensions, wantExts)
	}
	// workspaceStats 应当指向 precomputed 同一实例
	if res.WorkspaceStats != precomputed {
		t.Errorf("expected WorkspaceStats to be the precomputed pointer, got different")
	}

	// 同样验证 ScanWithCallback
	var seen int32
	_, err = s.ScanWithCallback(
		StreamingScanOptions{
			Directory:                 tmp,
			Extensions:                []string{".pdf"},
			Workspace:                 bogusWorkspace,
			PrecomputedWorkspaceStats: precomputed,
		},
		func(path string) error {
			atomic.AddInt32(&seen, 1)
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ScanWithCallback err: %v", err)
	}
	if atomic.LoadInt32(&seen) != 1 {
		t.Errorf("expected to see 1 file (a.pdf), saw %d", seen)
	}
}

// 回归：不传 PrecomputedWorkspaceStats 时，仍然内部调 CollectWorkspaceSuffixes
// （兼容性测试）
func TestStreamingScanner_FallsBackToInternalCollect(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.pdf"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// 真实存在的 workspace 子目录，里面有 .md
	ws := filepath.Join(tmp, "ws")
	if err := os.MkdirAll(ws, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "note.md"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewStreamingFileScanner()
	// 不传 PrecomputedWorkspaceStats
	res, err := s.CountFilesWithExtensions(StreamingScanOptions{
		Directory:  tmp,
		Extensions: []string{".pdf"},
		Workspace:  ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	// .md 应当被自动发现并合并进 finalExtensions
	found := false
	for _, ext := range res.UsedExtensions {
		if ext == ".md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf(".md should be auto-discovered from workspace, UsedExtensions=%v", res.UsedExtensions)
	}
}
