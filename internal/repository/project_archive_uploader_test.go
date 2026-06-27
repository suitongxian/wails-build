package repository

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestArchiveUpload_HappyPath_NuxtStyle 上报成功，manage 返回 {code:0,...}
func TestArchiveUpload_HappyPath_NuxtStyle(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)
	uploadAllRequired(t, svc, stages)

	closeSvc := NewProjectCloseService(db)
	out, err := closeSvc.Close(CloseInput{ProjectID: project.ID, OperatorID: "tester", Force: true})
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	// 启 httptest 模拟 manage 端
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sync/project-archive" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"upload_id":42}}`))
	}))
	defer srv.Close()

	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyManageEndpoint, srv.URL)
	configRepo.SetValue(KeyArchiveEndpoint, "")
	configRepo.SetValue(KeyManageToken, "test-token")

	uploader := NewArchiveUploader(db)
	res, err := uploader.Upload(project.ID, out.ManifestPath)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if res.Status != "success" {
		t.Errorf("expected status=success, got %s", res.Status)
	}

	// 项目 sync_status 应当 success
	pRepo := NewDataProjectRepository(db)
	updated, _ := pRepo.FindByID(project.ID)
	if updated.SyncStatus == nil || *updated.SyncStatus != "success" {
		t.Errorf("expected sync_status=success, got %v", updated.SyncStatus)
	}

	// manage 端确实收到了 manifest
	if len(receivedBody) == 0 {
		t.Fatal("manage didn't receive any body")
	}
	var body map[string]interface{}
	if err := json.Unmarshal(receivedBody, &body); err != nil {
		t.Fatal(err)
	}
	if body["project"] == nil {
		t.Error("manifest body missing project")
	}
	if body["lifecycle_events"] == nil {
		t.Error("manifest body should expose 'lifecycle_events' field for manage compat")
	}
	manifest := body["manifest"].(map[string]interface{})
	if manifest["archive_target"] != StorageTierUnitArchive {
		t.Fatalf("important project close should target unit_archive, got %+v", manifest)
	}
}

// TestArchiveUpload_GenericSuccessShape manage 返回 {success:true} 也兼容
func TestArchiveUpload_GenericSuccessShape(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)
	uploadAllRequired(t, svc, stages)
	out, _ := NewProjectCloseService(db).Close(CloseInput{ProjectID: project.ID, OperatorID: "tester", Force: true})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"ok"}`))
	}))
	defer srv.Close()

	NewSystemConfigRepository(db).SetValue(KeyManageEndpoint, srv.URL)
	NewSystemConfigRepository(db).SetValue(KeyArchiveEndpoint, "")
	res, err := NewArchiveUploader(db).Upload(project.ID, out.ManifestPath)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if res.Status != "success" {
		t.Errorf("expected success, got %s", res.Status)
	}
}

// TestArchiveUpload_ManageRejection manage 返回错误
func TestArchiveUpload_ManageRejection(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)
	uploadAllRequired(t, svc, stages)
	out, _ := NewProjectCloseService(db).Close(CloseInput{ProjectID: project.ID, OperatorID: "tester", Force: true})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"message":"missing project_code"}`))
	}))
	defer srv.Close()

	NewSystemConfigRepository(db).SetValue(KeyManageEndpoint, srv.URL)
	NewSystemConfigRepository(db).SetValue(KeyArchiveEndpoint, "")
	res, err := NewArchiveUploader(db).Upload(project.ID, out.ManifestPath)
	if err == nil {
		t.Fatal("expected error from manage rejection")
	}
	if res == nil || res.Status != "failed" {
		t.Errorf("expected failed result, got %+v", res)
	}

	pRepo := NewDataProjectRepository(db)
	updated, _ := pRepo.FindByID(project.ID)
	if updated.SyncStatus == nil || *updated.SyncStatus != "failed" {
		t.Errorf("expected sync_status=failed after rejection, got %v", updated.SyncStatus)
	}
}

// V1 验证：成功移交后不允许再次移交（防止重复入库）
func TestArchiveUpload_RejectRepeatAfterSuccess(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)
	uploadAllRequired(t, svc, stages)
	out, _ := NewProjectCloseService(db).Close(CloseInput{ProjectID: project.ID, OperatorID: "tester", Force: true})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"message":"ok"}`))
	}))
	defer srv.Close()
	NewSystemConfigRepository(db).SetValue(KeyManageEndpoint, srv.URL)
	NewSystemConfigRepository(db).SetValue(KeyArchiveEndpoint, "")

	uploader := NewArchiveUploader(db)

	// 第一次：成功
	res, err := uploader.Upload(project.ID, out.ManifestPath)
	if err != nil {
		t.Fatalf("first upload should succeed: %v", err)
	}
	if res.Status != "success" {
		t.Fatalf("first upload status should be success, got %s", res.Status)
	}

	// 第二次：拒绝
	_, err = uploader.Upload(project.ID, out.ManifestPath)
	if err == nil {
		t.Fatal("second upload should be rejected after success")
	}
	if !strings.Contains(err.Error(), "已成功移交") {
		t.Errorf("expected '已成功移交' in error, got: %v", err)
	}
}

// TestArchiveUpload_NoEndpointConfigured 未配置 manage 端点
func TestArchiveUpload_NoEndpointConfigured(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)
	uploadAllRequired(t, svc, stages)
	out, _ := NewProjectCloseService(db).Close(CloseInput{ProjectID: project.ID, OperatorID: "tester", Force: true})

	// 故意不设置 manage_endpoint
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyManageEndpoint, "")
	configRepo.SetValue(KeyArchiveEndpoint, "")

	res, err := NewArchiveUploader(db).Upload(project.ID, out.ManifestPath)
	if err == nil {
		t.Fatal("expected error when endpoint not configured")
	}
	if res == nil || !strings.Contains(res.Error, "manage 上报端点") {
		t.Errorf("expected error mentioning manage 上报端点, got %+v", res)
	}
}
