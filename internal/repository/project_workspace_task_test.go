package repository

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// 文件任务层目录原语：路径在环节与三态之间插入 {task} 一层；建目录幂等；ListTaskCodes 只回任务目录。
func TestProjectWorkspace_TaskLayer(t *testing.T) {
	root := t.TempDir()
	ws := NewProjectWorkspace(root)

	// 路径形状：{root}/CPA-7/stages/S1/TK-1/{input,process,output}
	wantTaskDir := filepath.Join(root, "项目文件管理", "CPA-7", "stages", "S1", "TK-1")
	if got := ws.TaskDir("CPA-7", "S1", "TK-1"); got != wantTaskDir {
		t.Fatalf("TaskDir=%q want %q", got, wantTaskDir)
	}
	if got := ws.TaskStateDir("CPA-7", "S1", "TK-1", "process"); got != filepath.Join(wantTaskDir, "process") {
		t.Fatalf("TaskStateDir=%q", got)
	}

	// 建目录：三态齐全
	if _, err := ws.CreateTaskDir("CPA-7", "S1", "TK-1"); err != nil {
		t.Fatalf("CreateTaskDir: %v", err)
	}
	for _, st := range []string{"input", "process", "output"} {
		if fi, err := os.Stat(ws.TaskStateDir("CPA-7", "S1", "TK-1", st)); err != nil || !fi.IsDir() {
			t.Fatalf("任务三态目录 %s 应存在: %v", st, err)
		}
	}
	// 幂等：再建一次不报错
	if _, err := ws.CreateTaskDir("CPA-7", "S1", "TK-1"); err != nil {
		t.Fatalf("CreateTaskDir 幂等失败: %v", err)
	}

	// 同环节再建一个任务
	if _, err := ws.CreateTaskDir("CPA-7", "S1", "TK-2"); err != nil {
		t.Fatalf("CreateTaskDir TK-2: %v", err)
	}

	// ListTaskCodes：只回任务目录，且能列出两个任务
	codes, err := ws.ListTaskCodes("CPA-7", "S1")
	if err != nil {
		t.Fatalf("ListTaskCodes: %v", err)
	}
	sort.Strings(codes)
	if len(codes) != 2 || codes[0] != "TK-1" || codes[1] != "TK-2" {
		t.Fatalf("ListTaskCodes=%v want [TK-1 TK-2]", codes)
	}

	// 混入一个遗留的环节级 process/ 桶，应被 ListTaskCodes 排除
	if err := os.MkdirAll(filepath.Join(ws.StageDir("CPA-7", "S1"), "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	codes, _ = ws.ListTaskCodes("CPA-7", "S1")
	for _, c := range codes {
		if c == "process" {
			t.Fatalf("ListTaskCodes 不应包含遗留桶目录 process: %v", codes)
		}
	}

	// 环节目录不存在 → 空列表，非错误
	empty, err := ws.ListTaskCodes("CPA-7", "S404")
	if err != nil || len(empty) != 0 {
		t.Fatalf("不存在环节应回空列表, got %v err %v", empty, err)
	}
}
