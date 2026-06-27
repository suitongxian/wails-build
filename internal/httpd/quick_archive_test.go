package httpd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 柜室文件代理：透传 scope 给 manage，并把 manage 的 {code,data} 转成 {success,data}。
func TestHTTP_QuickArchiveCabinetFilesProxy(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	var gotScope string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotScope = req.URL.Query().Get("scope")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":1,"file_name":"过程.txt","project_name":"甲","scope":"department","sensitivity_level":"important","storage_location":"部门重要项目档案柜"}]}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/quick-archive-files?scope=department")
	successOk(t, status, resp)
	if gotScope != "department" {
		t.Fatalf("应透传 scope=department，实得 %s", gotScope)
	}
	arr, _ := resp["data"].([]interface{})
	if len(arr) != 1 {
		t.Fatalf("应返回 1 条柜室文件，实得 %v", resp["data"])
	}
	if arr[0].(map[string]interface{})["storage_location"] != "部门重要项目档案柜" {
		t.Fatalf("storage_location 透传错误：%v", arr[0])
	}
}

// 单项目一键归档：person 范围 → 复制到本地个人夹。
func TestHTTP_QuickArchiveProject_PersonLocal(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, _ := seedTemplateAndProject(t, db) // 默认 active，OWNER-1 有 close 权限
	withActiveUser(t, db, "OWNER-1")
	// 模版改为个人范围（seed 默认无 scope）
	if _, err := db.Exec(`UPDATE data_templates SET scope='person' WHERE template_code='TPL-PRINT-BOOK'`); err != nil {
		t.Fatal(err)
	}

	root := repository.NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := repository.NewProjectWorkspace(root)
	if _, err := ws.CreateTaskDir(pj.ProjectCode, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(pj.ProjectCode, "S1", "TK1", "process"), "稿.txt"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReq(t, r, "POST", "/projects/"+itoa(pj.ID)+"/quick-archive", nil)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if int(d["archived"].(float64)) != 1 {
		t.Fatalf("应归档 1，实得 %v", d["archived"])
	}
	// project 敏感级 important → 过程入档案夹（个人夹默认就在工作空间下）
	if _, err := os.Stat(filepath.Join(root, "个人重要文件夹", "HTTP 测试项目", "稿.txt")); err != nil {
		t.Fatalf("过程(重要)应入个人重要文件夹：%v", err)
	}
}

// 集中立项项目一键归档：scope 取自专属模版 TPL-PRJ-<remote_id>，sensitivity 取自项目行。
func TestHTTP_QuickArchiveCentralized_PersonLocal(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "alice")

	// 一条已发布的集中立项项目（remote_id=7，重要级，立项选「个人级」→ 本地归档）
	if _, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, owner_name, submitted_by, status, sync_status, manage_remote_id, project_code, sensitivity_level, project_scope, create_time, update_time, disable)
		VALUES ('甲项目','owner-a','alice','accepted','synced',7,'XM-2026-0001','important','person',datetime('now'),datetime('now'),0)`); err != nil {
		t.Fatal(err)
	}

	root := withProjectRoot(t, db)
	ws := repository.NewProjectWorkspace(root)
	const dir = "甲项目-XM-2026-0001"
	if _, err := ws.CreateTaskDir(dir, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(dir, "S1", "TK1", "process"), "稿.txt"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/7/quick-archive", nil)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if int(d["archived"].(float64)) != 1 {
		t.Fatalf("应归档 1，实得 %v（%v）", d["archived"], d)
	}
	if _, err := os.Stat(filepath.Join(root, "个人重要文件夹", "甲项目", "稿.txt")); err != nil {
		t.Fatalf("重要级过程应入个人重要文件夹：%v", err)
	}
}

// 参与人侧：本机无 cpa 行，scope/敏感级/编码随请求体（来自 my-tasks）带入，仍能正确路由归档。
func TestHTTP_QuickArchiveCentralized_ParticipantViaBody(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "worker") // 参与人，非立项者；本机无 centralized_project_applications 行

	root := withProjectRoot(t, db)
	ws := repository.NewProjectWorkspace(root)
	const dir = "乙项目-XM-2026-0002" // = centralizedDirCode 无本地行时 = project_code（无名前缀）
	// 注意：无 cpa 行时 centralizedDirCode 退回 project_code，故目录用 project_code 命名
	const dirByCode = "XM-2026-0002"
	if _, err := ws.CreateTaskDir(dirByCode, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(dirByCode, "S1", "TK1", "process"), "稿.txt"), []byte("Y"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = dir

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/8/quick-archive", map[string]interface{}{
		"project_code": "XM-2026-0002", "project_name": "乙项目",
		"project_scope": "person", "sensitivity_level": "core",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if int(d["archived"].(float64)) != 1 {
		t.Fatalf("应归档 1，实得 %v（%v）", d["archived"], d)
	}
	// 核心级 → 个人核心文件夹（工作空间下）
	if _, err := os.Stat(filepath.Join(root, "个人核心文件夹", "乙项目", "稿.txt")); err != nil {
		t.Fatalf("核心级过程应入个人核心文件夹：%v", err)
	}
}

// 端到端：工作受理导入参考文件(声明重要级) → 一键归档 → 进个人「档案夹」(重要=档案)。
func TestHTTP_ImportReferenceThenQuickArchive_Important(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "worker")
	root := withProjectRoot(t, db)
	ws := repository.NewProjectWorkspace(root)
	const code = "XM-T-9001"
	if _, err := ws.CreateTaskDir(code, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}

	// 1) 导入参考文件并声明「重要」级
	st, resp := uploadReq(t, r, "/centralized-projects/workbench/import-reference", "file", "外部规范.txt",
		[]byte("REF"), map[string]string{"app_id": "9", "stage_code": "S1", "task_code": "TK1", "project_code": code, "category": "external", "sensitivity_level": "important"})
	successOk(t, st, resp)

	// 参考文件确实落到 reference 桶
	refPath := filepath.Join(ws.TaskStateDir(code, "S1", "TK1", "reference"), "外部规范.txt")
	if _, err := os.Stat(refPath); err != nil {
		t.Fatalf("参考文件未导入：%v", err)
	}

	// 2) 一键归档（参与人侧，带项目上下文）
	st, resp = jsonReq(t, r, "POST", "/centralized-projects/9/quick-archive", map[string]interface{}{
		"project_code": code, "project_name": "甲项目", "project_scope": "unit", "sensitivity_level": "general",
	})
	successOk(t, st, resp)
	d := dataMap(t, resp)
	if int(d["archived"].(float64)) < 1 {
		t.Fatalf("参考文件应被归档，archived=%v（%v）", d["archived"], d)
	}

	// 3) 重要级参考 → 个人重要文件夹（档案=重要）
	if _, err := os.Stat(filepath.Join(root, "个人重要文件夹", "甲项目", "外部规范.txt")); err != nil {
		t.Fatalf("重要级参考应入个人重要文件夹：%v", err)
	}
}

// 全局一键归档：归档 person 项目，跳过 SYS-PERSONAL 内置容器。
func TestHTTP_QuickArchiveAll_SkipsPersonalSystem(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, _ := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")
	if _, err := db.Exec(`UPDATE data_templates SET scope='person' WHERE template_code='TPL-PRINT-BOOK'`); err != nil {
		t.Fatal(err)
	}
	// 插一个内置个人容器项目，应被排除
	if _, err := repository.NewDataProjectRepository(db).Insert(db, repository.CreateDataProjectInput{
		ProjectCode: "SYS-PERSONAL-CORE", ProjectName: "个人核心", Status: "active", SensitivityLevel: "core",
	}); err != nil {
		t.Fatal(err)
	}

	root := repository.NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := repository.NewProjectWorkspace(root)
	if _, err := ws.CreateTaskDir(pj.ProjectCode, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(pj.ProjectCode, "S1", "TK1", "process"), "稿.txt"), []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReq(t, r, "POST", "/projects/quick-archive-all", nil)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if int(d["total_archived"].(float64)) < 1 {
		t.Fatalf("应至少归档 1，实得 %v", d["total_archived"])
	}
	projects, _ := d["projects"].([]interface{})
	for _, p := range projects {
		pm := p.(map[string]interface{})
		if pm["project_code"] == "SYS-PERSONAL-CORE" {
			t.Fatal("SYS-PERSONAL 容器不应出现在归档结果中")
		}
	}
}
