package httpd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 结项归卷：拉某项目部门柜定稿清单，按 centralizedDirCode(项目名-编号)+scope=department+bucket=output 透传 manage。
func TestHTTP_CentralizedFinalFilesProxy(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "owner-a")

	if _, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, owner_name, submitted_by, status, sync_status, manage_remote_id, project_code, sensitivity_level, project_scope, create_time, update_time, disable)
		VALUES ('甲项目','owner-a','alice','accepted','synced',7,'XM-2026-0001','important','unit',datetime('now'),datetime('now'),0)`); err != nil {
		t.Fatal(err)
	}

	var gotQuery map[string]string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		gotQuery = map[string]string{
			"project_code": q.Get("project_code"),
			"scope":        q.Get("scope"),
			"bucket":       q.Get("bucket"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":42,"file_name":"定稿.txt","bucket":"output","sensitivity_level":"important","storage_location":"部门重要项目档案柜"}]}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/7/final-files")
	successOk(t, status, resp)

	if gotQuery["project_code"] != "甲项目-XM-2026-0001" {
		t.Fatalf("应按 centralizedDirCode 透传 project_code=甲项目-XM-2026-0001，实得 %q", gotQuery["project_code"])
	}
	if gotQuery["scope"] != "department" || gotQuery["bucket"] != "output" {
		t.Fatalf("应透传 scope=department&bucket=output，实得 %+v", gotQuery)
	}
	arr, _ := resp["data"].([]interface{})
	if len(arr) != 1 {
		t.Fatalf("应返回 1 条定稿，实得 %v", resp["data"])
	}
	if arr[0].(map[string]interface{})["id"].(float64) != 42 {
		t.Fatalf("文件 id 透传错误：%v", arr[0])
	}
}

// 结项：把 closer(当前登录人) 与 move_file_ids 透传给 manage。
func TestHTTP_CloseCentralized_ForwardsCloserAndMoveIds(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "owner-a")

	var gotBody map[string]any
	var gotID string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotID = req.URL.Query().Get("id")
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"id":7,"status":"closed"}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/7/close", map[string]interface{}{
		"closure_summary": "收尾",
		"move_file_ids":   []int{42, 43},
	})
	successOk(t, status, resp)

	if gotID != "7" {
		t.Fatalf("应透传 id=7，实得 %q", gotID)
	}
	if gotBody["closer"] != "owner-a" {
		t.Fatalf("closer 应为当前登录人 owner-a，实得 %v", gotBody["closer"])
	}
	ids, _ := gotBody["move_file_ids"].([]interface{})
	if len(ids) != 2 || ids[0].(float64) != 42 || ids[1].(float64) != 43 {
		t.Fatalf("move_file_ids 透传错误：%v", gotBody["move_file_ids"])
	}
}
