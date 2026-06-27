package httpd

import (
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 在线编辑回归：process 文档可读可写；input 只读、保存被拒。
func TestHTTP_WorkbenchDoc_EditProcessReadonlyInput(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	root := t.TempDir()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyProjectRoot, root)

	// 造出 CPA-5/stages/S1/{input,process,output}
	ws := repository.NewProjectWorkspace(root)
	stageDir, err := ws.CreateStageDir("CPA-5", "S1")
	if err != nil {
		t.Fatalf("建环节目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "process", "草稿.txt"), []byte("初始内容"), 0o644); err != nil {
		t.Fatalf("写过程文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "input", "来料.txt"), []byte("上游来料"), 0o644); err != nil {
		t.Fatalf("写来料文件失败: %v", err)
	}

	// 读 process：可编辑 + 内容正确
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/doc?app_id=5&stage_code=S1&bucket=process&name=草稿.txt")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["content"] != "初始内容" {
		t.Errorf("process 内容应为「初始内容」，实得 %v", d["content"])
	}
	if d["editable"] != true {
		t.Errorf("process 应可编辑")
	}

	// 写 process：保存成功并落盘
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/workbench/doc", map[string]interface{}{
		"app_id": "5", "stage_code": "S1", "bucket": "process", "name": "草稿.txt", "content": "改过的内容",
	})
	successOk(t, status, resp)
	got, _ := os.ReadFile(filepath.Join(stageDir, "process", "草稿.txt"))
	if string(got) != "改过的内容" {
		t.Errorf("保存后磁盘内容应为「改过的内容」，实得 %q", string(got))
	}

	// 读 input：只读
	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/doc?app_id=5&stage_code=S1&bucket=input&name=来料.txt")
	successOk(t, status, resp)
	if dataMap(t, resp)["editable"] != false {
		t.Errorf("input 应为只读")
	}

	// 写 input：被拒
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/workbench/doc", map[string]interface{}{
		"app_id": "5", "stage_code": "S1", "bucket": "input", "name": "来料.txt", "content": "x",
	})
	if resp["success"] == true {
		t.Errorf("input 桶不应允许在线保存，resp=%v", resp)
	}
	// 来料文件未被改动
	got, _ = os.ReadFile(filepath.Join(stageDir, "input", "来料.txt"))
	if string(got) != "上游来料" {
		t.Errorf("input 文件不应被改动，实得 %q", string(got))
	}
}
