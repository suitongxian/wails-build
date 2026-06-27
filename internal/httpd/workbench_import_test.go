package httpd

import (
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 参考文件导入：把本地所选文件拷贝进任务的 reference 桶；files 接口能列出 reference/output 桶。
func TestHTTP_Workbench_ImportReference(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	root := t.TempDir()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyProjectRoot, root)
	ws := repository.NewProjectWorkspace(root)
	if _, err := ws.CreateTaskDir("CPA-9", "S1", "TK-1"); err != nil {
		t.Fatal(err)
	}

	// 导入一个外部文件到 reference 桶
	status, resp := uploadReq(t, r, "/centralized-projects/workbench/import-reference", "file", "外部资料.txt",
		[]byte("REF-CONTENT"), map[string]string{"app_id": "9", "stage_code": "S1", "task_code": "TK-1"})
	successOk(t, status, resp)

	// 文件应已被拷贝到 reference 桶（项目目录在「项目文件管理」下）
	dst := filepath.Join(ws.TaskStateDir("CPA-9", "S1", "TK-1", "reference"), "外部资料.txt")
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("参考文件未导入: %v", err)
	}
	if string(b) != "REF-CONTENT" {
		t.Fatalf("内容应为 REF-CONTENT, got %s", string(b))
	}

	// files 接口应能列出 reference 桶里的文件
	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/workbench/files?app_id=9&stage_code=S1&task_code=TK-1")
	successOk(t, status, resp)
	buckets := dataMap(t, resp)["buckets"].(map[string]interface{})
	if _, ok := buckets["reference"]; !ok {
		t.Fatal("files 应返回 reference 桶")
	}
	if _, ok := buckets["output"]; !ok {
		t.Fatal("files 应返回 output 桶")
	}
	ref := buckets["reference"].([]interface{})
	if len(ref) != 1 || ref[0].(map[string]interface{})["name"] != "外部资料.txt" {
		t.Fatalf("reference 应含 外部资料.txt, got %v", ref)
	}
}

// 参考文件导入时的「归类定级声明」：类别+级别随上传一并落 sidecar；未给级别按类别取默认。
func TestHTTP_Workbench_ImportReference_Grading(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	root := t.TempDir()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyProjectRoot, root)
	ws := repository.NewProjectWorkspace(root)
	if _, err := ws.CreateTaskDir("CPA-9", "S1", "TK-1"); err != nil {
		t.Fatal(err)
	}
	refDir := ws.TaskStateDir("CPA-9", "S1", "TK-1", "reference")

	// 内部资料、未显式给级别 → 默认重要级
	status, resp := uploadReq(t, r, "/centralized-projects/workbench/import-reference", "file", "内部纪要.txt",
		[]byte("A"), map[string]string{"app_id": "9", "stage_code": "S1", "task_code": "TK-1", "category": "internal"})
	successOk(t, status, resp)
	if d := dataMap(t, resp); d["sensitivity_level"] != "important" || d["category"] != "internal" {
		t.Fatalf("内部资料应默认重要级, got %v", d)
	}
	if g, ok := repository.ReadRefGrade(refDir, "内部纪要.txt"); !ok || g.SensitivityLevel != "important" {
		t.Fatalf("sidecar 未记录内部纪要定级: %+v ok=%v", g, ok)
	}

	// 外部资料、显式核心级 → 以导入者声明为准
	status, resp = uploadReq(t, r, "/centralized-projects/workbench/import-reference", "file", "外部图.txt",
		[]byte("B"), map[string]string{"app_id": "9", "stage_code": "S1", "task_code": "TK-1", "category": "external", "sensitivity_level": "core"})
	successOk(t, status, resp)
	if g, ok := repository.ReadRefGrade(refDir, "外部图.txt"); !ok || g.SensitivityLevel != "core" || g.Category != "external" {
		t.Fatalf("外部图应记核心级: %+v ok=%v", g, ok)
	}
}
