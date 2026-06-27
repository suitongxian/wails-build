package repository

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 部门项目一键归档（按桶分流）：参考/过程 → 本地个人夹；定稿 → 上报云端部门柜。
func TestArchiveProjectCloud(t *testing.T) {
	db := openTestDB(t)

	var got QuickArchiveCloudPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sync/quick-archive" {
			w.WriteHeader(404)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"message":"manage 已接收"}`))
	}))
	defer srv.Close()

	cfg := NewSystemConfigRepository(db)
	cfg.SetValue(KeyArchiveEndpoint, srv.URL) // archive_endpoint 优先，避免命中种子真实地址
	cfg.SetValue(KeyManageEndpoint, srv.URL)

	root := t.TempDir()
	ws := NewProjectWorkspace(root)
	const proj = "乙项目-XM-2026-0003"
	if _, err := ws.CreateTaskDir(proj, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(proj, "S1", "TK1", "process"), "过程.txt"), []byte("P"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(proj, "S1", "TK1", "output"), "定稿.txt"), []byte("F"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ArchiveProjectByScope(db, root, proj, "乙项目", "department", "important", "tester", "", "")
	if err != nil {
		t.Fatalf("归档失败：%v", err)
	}
	// 1 本地(过程) + 1 云端(定稿)
	if res.Archived != 2 {
		t.Fatalf("应归档 2（过程本地+定稿上云），实得 %d（errors=%v）", res.Archived, res.Errors)
	}

	// 过程文件 → 本地个人重要文件夹（重要）；定稿不在本地
	if _, err := os.Stat(filepath.Join(root, "个人重要文件夹", "乙项目", "过程.txt")); err != nil {
		t.Fatalf("过程(重要)应入本地个人重要文件夹：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "个人重要文件夹", "乙项目", "定稿.txt")); err == nil {
		t.Fatal("部门项目的定稿应上云，不应留在本地个人夹")
	}

	// 上报清单只含定稿，且目标为部门重要文件柜
	if got.Scope != "department" || got.Container != "部门文件柜" {
		t.Fatalf("容器应为部门柜，实得 scope=%s container=%s", got.Scope, got.Container)
	}
	if len(got.Files) != 1 || got.Files[0].Name != "定稿.txt" {
		t.Fatalf("上报清单应只含定稿，实得 %+v", got.Files)
	}
	if got.Files[0].TargetFolder != "部门重要文件柜" { // important → 档案
		t.Fatalf("定稿(重要)应入部门重要文件柜，实得 %s", got.Files[0].TargetFolder)
	}
}

// 单位级项目选「定稿保管=部门级」：定稿改投部门柜（而非单位室），并带归档归属说明。
// 单位级项目：定稿统一归「部门柜」——不再需要承接时选择保管层级，不传 custody 也进部门柜。
func TestArchiveProjectCloud_UnitGoesToDepartmentCabinet(t *testing.T) {
	db := openTestDB(t)
	var got QuickArchiveCloudPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"message":"ok"}`))
	}))
	defer srv.Close()
	cfg := NewSystemConfigRepository(db)
	cfg.SetValue(KeyArchiveEndpoint, srv.URL)
	cfg.SetValue(KeyManageEndpoint, srv.URL)

	root := t.TempDir()
	ws := NewProjectWorkspace(root)
	const proj = "丁项目-XM-2026-0005"
	if _, err := ws.CreateTaskDir(proj, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(proj, "S1", "TK1", "output"), "定稿.txt"), []byte("F"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 单位级项目，不传定稿保管层级（custody=""）：定稿仍统一归部门柜
	res, err := ArchiveProjectByScope(db, root, proj, "丁项目", "unit", "important", "tester", "", "单位立项")
	if err != nil {
		t.Fatal(err)
	}
	if res.Archived != 1 {
		t.Fatalf("应上报定稿 1，实得 %d（errors=%v）", res.Archived, res.Errors)
	}
	// 定稿归部门重要文件柜（不再是单位文件室）
	if got.Scope != "department" || got.Container != "部门文件柜" {
		t.Fatalf("单位级定稿应归部门柜，实得 scope=%s container=%s", got.Scope, got.Container)
	}
	if len(got.Files) != 1 || got.Files[0].TargetFolder != "部门重要文件柜" {
		t.Fatalf("定稿(重要)应入部门重要文件柜，实得 %+v", got.Files)
	}
	if got.CustodyNote != "单位立项" {
		t.Fatalf("应带归档归属说明=单位立项，实得 %q", got.CustodyNote)
	}
}

// 未配置 manage 端点时：定稿上云失败记入 errors（本地参考/过程不受影响）。
func TestArchiveProjectCloud_NoEndpoint(t *testing.T) {
	db := openTestDB(t)
	cfg := NewSystemConfigRepository(db)
	cfg.SetValue(KeyArchiveEndpoint, "")
	cfg.SetValue(KeyManageEndpoint, "")
	root := t.TempDir()
	ws := NewProjectWorkspace(root)
	const proj = "丙项目-XM-2026-0004"
	if _, err := ws.CreateTaskDir(proj, "S1", "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.TaskStateDir(proj, "S1", "TK1", "output"), "定稿.txt"), []byte("F"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ArchiveProjectByScope(db, root, proj, "丙项目", "unit", "core", "", "", "")
	if err != nil {
		t.Fatalf("不应返回顶层错误（云端失败应记入 errors）：%v", err)
	}
	if len(res.Errors) == 0 {
		t.Fatal("未配置端点时定稿上云应失败并记入 errors")
	}
	if res.Archived != 0 {
		t.Fatalf("仅定稿且上云失败 → 归档 0，实得 %d", res.Archived)
	}
}
