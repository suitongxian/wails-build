package httpd

import (
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 本机打开:校验路径解析 + 非法/缺失/不存在的错误分支,成功路径用注入的 opener 桩验证(不真正拉起程序)。
func TestHTTP_WorkbenchOpen_LocalApp(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	root := t.TempDir()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyProjectRoot, root)

	ws := repository.NewProjectWorkspace(root)
	stageDir, err := ws.CreateStageDir("CPA-7", "S1")
	if err != nil {
		t.Fatalf("建环节目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "process", "方案.docx"), []byte("x"), 0o644); err != nil {
		t.Fatalf("写过程文件失败: %v", err)
	}

	// 注入 opener 桩:记录被打开的路径,不真正拉起本机程序
	var opened string
	orig := openLocalFileFn
	openLocalFileFn = func(p string) error { opened = p; return nil }
	defer func() { openLocalFileFn = orig }()

	// 成功:用本机程序打开 process 文件
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/open?app_id=7&stage_code=S1&bucket=process&name=方案.docx")
	successOk(t, status, resp)
	if opened != filepath.Join(stageDir, "process", "方案.docx") {
		t.Errorf("opener 应收到正确路径,实得 %q", opened)
	}

	// 非法 bucket → 400
	status, _ = jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/open?app_id=7&stage_code=S1&bucket=xxx&name=方案.docx")
	if status != 400 {
		t.Errorf("非法 bucket 应 400,实得 %d", status)
	}

	// 文件不存在 → 404
	status, _ = jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/open?app_id=7&stage_code=S1&bucket=process&name=不存在.docx")
	if status != 404 {
		t.Errorf("文件不存在应 404,实得 %d", status)
	}

	// 缺 name → 400
	status, _ = jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/open?app_id=7&stage_code=S1&bucket=process")
	if status != 400 {
		t.Errorf("缺 name 应 400,实得 %d", status)
	}
}

// 五层落盘：workbench 带 task_code 时按文件任务目录定位，任务之间互相隔离。
func TestHTTP_Workbench_TaskScoped(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	root := t.TempDir()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyProjectRoot, root)
	ws := repository.NewProjectWorkspace(root)
	if _, err := ws.CreateTaskDir("CPA-9", "S1", "TK-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateTaskDir("CPA-9", "S1", "TK-2"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir("CPA-9", "S1", "TK-1", "process"), "甲.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir("CPA-9", "S1", "TK-2", "process"), "乙.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}

	// files?task_code=TK-1 只列 TK-1 的过程文件（不串到 TK-2）
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/files?app_id=9&stage_code=S1&task_code=TK-1")
	successOk(t, status, resp)
	buckets := dataMap(t, resp)["buckets"].(map[string]interface{})
	proc := buckets["process"].([]interface{})
	if len(proc) != 1 || proc[0].(map[string]interface{})["name"] != "甲.txt" {
		t.Fatalf("TK-1 process 应只含甲.txt, got %v", proc)
	}

	// 读 TK-1 过程文档
	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/doc?app_id=9&stage_code=S1&task_code=TK-1&bucket=process&name=甲.txt")
	successOk(t, status, resp)
	if dataMap(t, resp)["content"] != "A" {
		t.Fatalf("TK-1 内容应为 A，got %v", dataMap(t, resp)["content"])
	}

	// 保存落到 TK-1 任务目录，TK-2 不受影响
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/workbench/doc", map[string]interface{}{
		"app_id": "9", "stage_code": "S1", "task_code": "TK-1", "bucket": "process", "name": "甲.txt", "content": "A2",
	})
	successOk(t, status, resp)
	got, _ := os.ReadFile(filepath.Join(ws.TaskStateDir("CPA-9", "S1", "TK-1", "process"), "甲.txt"))
	if string(got) != "A2" {
		t.Fatalf("保存应落到 TK-1 任务目录，got %q", string(got))
	}
	got2, _ := os.ReadFile(filepath.Join(ws.TaskStateDir("CPA-9", "S1", "TK-2", "process"), "乙.txt"))
	if string(got2) != "B" {
		t.Fatalf("TK-2 文件不应被改动，got %q", string(got2))
	}
}
