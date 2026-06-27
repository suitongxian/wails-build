package httpd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// Bug1：集中立项「项目列表」原本只读本机表，换台电脑（本机表为空）就看不到自己立的项目。
// 修复：在保留本地结果的前提下，合并从 manage 按 submitted_by 拉到的项目（本地缺失的补进来）。
func TestHTTP_ListCentralizedProjects_MergesManageSubmissions(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "zhang")

	// 本机表为空（模拟 B 电脑），但 manage 上有张主任提交过的项目。
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/centralized-projects/list" && req.URL.Query().Get("submitted_by") == "zhang" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
				{"id":7,"project_name":"二季度核算项目","project_code":"XM-2026-0001","owner_name":"wang","department":"财务处","data_owner":"财务处","submitted_by":"zhang","status":"approved","sensitivity_level":"important","project_scope":"unit"}
			]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects?page=1&page_size=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	found := false
	for _, it := range items {
		m, _ := it.(map[string]interface{})
		if m["project_code"] == "XM-2026-0001" && m["project_name"] == "二季度核算项目" {
			found = true
			// 应带上 manage 的 remote id，供"查看环节/结项"动作使用
			if fmt.Sprint(m["manage_remote_id"]) != "7" {
				t.Errorf("manage 项目应带 manage_remote_id=7，实得 %v", m["manage_remote_id"])
			}
		}
	}
	if !found {
		t.Fatalf("应合并出 manage 上张主任提交的项目 XM-2026-0001，实得 %v", items)
	}
}

// 本地已存在的项目（已同步、带 manage_remote_id）不应因为合并而重复出现。
func TestHTTP_ListCentralizedProjects_NoDuplicateWhenLocalHasIt(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "zhang")

	// 本机已有一条（manage_remote_id=7, project_code=XM-2026-0001）
	_, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, project_code, owner_name, department, data_owner, submitted_by, status, sync_status,
		 sensitivity_level, project_scope, output_custody_scope, output_custody_note, manage_remote_id, create_time, update_time, disable)
		VALUES ('二季度核算项目','XM-2026-0001','wang','财务处','财务处','zhang','approved','synced','important','unit','unit','',7,'now','now',0)`)
	if err != nil {
		t.Fatal(err)
	}

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/centralized-projects/list" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[
				{"id":7,"project_name":"二季度核算项目","project_code":"XM-2026-0001","owner_name":"wang","submitted_by":"zhang","status":"approved","sensitivity_level":"important","project_scope":"unit"}
			]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects?page=1&page_size=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	n := 0
	for _, it := range items {
		m, _ := it.(map[string]interface{})
		if m["project_code"] == "XM-2026-0001" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("项目 XM-2026-0001 应只出现 1 次（去重），实得 %d 次", n)
	}
}

// 未配置 manage 时，列表仍按本地正常返回（零回归）。
func TestHTTP_ListCentralizedProjects_NoEndpointStillLocal(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "zhang")
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, "")
	_, _ = db.Exec(`INSERT INTO centralized_project_applications
		(project_name, project_code, owner_name, submitted_by, status, sync_status, sensitivity_level, project_scope, output_custody_scope, output_custody_note, create_time, update_time, disable)
		VALUES ('本地项目','XM-LOCAL','wang','zhang','approved','pending','general','unit','unit','','now','now',0)`)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects?page=1&page_size=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("无 endpoint 时应只返回本地 1 条，实得 %d", len(items))
	}
}
