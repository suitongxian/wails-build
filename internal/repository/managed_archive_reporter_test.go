package repository

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubmitOutput_CabinetSyncSkippedWithoutEndpoint(t *testing.T) {
	db, svc, _, stages := setupProjectForFileOps(t)
	// 2026-05-22 启动时已 seed 默认 archive_endpoint，本用例显式清除以测试"无 endpoint → skip"路径
	NewSystemConfigRepository(db).SetValue(KeyArchiveEndpoint, "")
	NewSystemConfigRepository(db).SetValue(KeyManageEndpoint, "")
	outFv := findFvByLocalCode(t, stages, "MZ-PB", "OUT-001")
	src := writeTempFile(t, "排版稿.pdf", "x")
	if _, err := svc.UploadOrBind(outFv.ID, UploadInput{SourcePath: src, OriginalFileName: "排版稿.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitOutput(outFv.ID, "u"); err != nil {
		t.Fatal(err)
	}

	var status, msg string
	if err := db.QueryRowx(`SELECT cabinet_sync_status, cabinet_sync_message FROM file_versions WHERE id = ?`, outFv.ID).Scan(&status, &msg); err != nil {
		t.Fatal(err)
	}
	if status != "skipped" {
		t.Fatalf("expected skipped, got %s", status)
	}
	if !strings.Contains(msg, "manage 上报端点") {
		t.Fatalf("expected endpoint message, got %s", msg)
	}
}

func TestSubmitOutput_CabinetSyncSuccess(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)
	outFv := findFvByLocalCode(t, stages, "MZ-PB", "OUT-001")
	src := writeTempFile(t, "排版稿.pdf", "x")
	if _, err := svc.UploadOrBind(outFv.ID, UploadInput{SourcePath: src, OriginalFileName: "排版稿.pdf", OperatorID: "u"}); err != nil {
		t.Fatal(err)
	}

	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sync/file-archive" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"storage_tier":"department_cabinet","storage_location":"部门重要项目档案柜"}}`))
	}))
	defer srv.Close()

	NewSystemConfigRepository(db).SetValue(KeyManageEndpoint, srv.URL)
	// 2026-05-22 启动时已 seed 默认 archive_endpoint，它会优先于 manage_endpoint；
	// 这里清空以让上报走到 mock 服务
	NewSystemConfigRepository(db).SetValue(KeyArchiveEndpoint, "")
	if _, err := svc.SubmitOutput(outFv.ID, "u"); err != nil {
		t.Fatal(err)
	}

	if received["schema"] != "data-asset-scan/file-archive-v1" {
		t.Fatalf("unexpected schema: %+v", received)
	}
	if received["archive_phase"] != "department_cabinet" {
		t.Fatalf("unexpected archive_phase: %+v", received["archive_phase"])
	}
	decision := received["archive_decision"].(map[string]interface{})
	if decision["target_tier"] != "department_cabinet" || decision["file_state"] != FileStateDeptFinal {
		t.Fatalf("unexpected archive_decision: %+v", decision)
	}
	proj := received["project"].(map[string]interface{})
	if proj["project_code"] != project.ProjectCode {
		t.Fatalf("project code mismatch: %v", proj["project_code"])
	}
	if proj["owner_subject_code"] == "" || proj["custodian_subject_code"] == "" || proj["security_subject_code"] == "" {
		t.Fatalf("project subject codes missing: %+v", proj)
	}
	ledger := received["ledger"].(map[string]interface{})
	if ledger["owner_subject_code"] == "" || ledger["custodian_subject_code"] == "" || ledger["security_subject_code"] == "" {
		t.Fatalf("ledger subject codes missing: %+v", ledger)
	}

	var status string
	if err := db.Get(&status, `SELECT cabinet_sync_status FROM file_versions WHERE id = ?`, outFv.ID); err != nil {
		t.Fatal(err)
	}
	if status != "success" {
		t.Fatalf("expected success, got %s", status)
	}
}
