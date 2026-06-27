package httpd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 立项列表：manage 云端权威值（工程进展/负责人/项目周期/整体完成率）叠加到本地行，
// 并把本地缺失的 manage 项目补进来。验证多端联动、不依赖本地存储。
func TestHTTP_ListCentralized_OverlaysManageAuthoritative(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "alice")

	// 本地一条「陈旧」行：状态还停在 approved、无周期/完成率
	if _, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, owner_name, submitted_by, status, sync_status, manage_remote_id, project_code, sensitivity_level, project_scope, output_custody_scope, output_custody_note, create_time, update_time, disable)
		VALUES ('甲项目','old-owner','alice','approved','synced',7,'XM-1','general','unit','unit','',datetime('now'),datetime('now'),0)`); err != nil {
		t.Fatal(err)
	}

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// /api/centralized-projects/list?submitted_by=alice
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":[
			{"id":7,"project_name":"甲项目","project_code":"XM-1","owner_name":"lead-new","submitted_by":"alice","status":"assigning","sensitivity_level":"general","project_scope":"unit","cycle_start":"2026-07-01","cycle_end":"2026-12-31","completion_rate":50,"create_time":"2026-06-01 00:00:00","update_time":"2026-06-02 00:00:00"},
			{"id":9,"project_name":"乙项目","project_code":"XM-2","owner_name":"lead-b","submitted_by":"alice","status":"accepted","sensitivity_level":"important","project_scope":"department","cycle_start":"","cycle_end":"","completion_rate":100,"create_time":"2026-06-03 00:00:00","update_time":"2026-06-03 00:00:00"}
		]}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects?page=1&page_size=50")
	successOk(t, status, resp)

	data, _ := resp["data"].(map[string]interface{})
	items, _ := data["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("应有 2 条（本地1 + manage新增1），实得 %d：%v", len(items), items)
	}
	byCode := map[string]map[string]interface{}{}
	for _, it := range items {
		m := it.(map[string]interface{})
		byCode[m["project_code"].(string)] = m
	}

	// 本地行被 manage 权威值覆盖
	a := byCode["XM-1"]
	if a["status"] != "assigning" {
		t.Fatalf("XM-1 工程进展应叠加为 assigning（分工中），实得 %v", a["status"])
	}
	if a["owner_name"] != "lead-new" {
		t.Fatalf("XM-1 负责人应叠加为 lead-new，实得 %v", a["owner_name"])
	}
	if a["cycle_start"] != "2026-07-01" || a["cycle_end"] != "2026-12-31" {
		t.Fatalf("XM-1 项目周期叠加错误：%v ~ %v", a["cycle_start"], a["cycle_end"])
	}
	if a["completion_rate"].(float64) != 50 {
		t.Fatalf("XM-1 整体完成率应为 50，实得 %v", a["completion_rate"])
	}

	// manage 独有项目被补进来
	b := byCode["XM-2"]
	if b == nil || b["status"] != "accepted" || b["completion_rate"].(float64) != 100 {
		t.Fatalf("manage 独有项目 XM-2 应被补入且带权威值，实得 %v", b)
	}
}
