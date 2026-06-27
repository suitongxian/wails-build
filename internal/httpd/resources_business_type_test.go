package httpd

import (
	"fmt"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/repository"
)

// seedResource 插一行 data_resources 用于业务来源 tab 过滤测试
func seedResource(t *testing.T, db *sqlx.DB, name, contentSign string, firstCreateTime time.Time, workspaceSourceCount int) {
	t.Helper()
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES (?, 1, ?, ?, ?, 0, 0, ?, ?, 0, 'historical')`,
		contentSign, workspaceSourceCount, firstCreateTime, name, now, now)
	if err != nil {
		t.Fatalf("insert data_resources: %v", err)
	}
}

func resourceNames(t *testing.T, d map[string]interface{}) []string {
	t.Helper()
	raw, ok := d["resources"].([]interface{})
	if !ok {
		t.Fatalf("data.resources not a list: %+v", d)
	}
	names := make([]string, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		names = append(names, fmt.Sprint(m["resources_name"]))
	}
	return names
}

// workspace tab：只返回 workspace_source_count > 0 的资源；不依赖 full_inventory_time
func TestHTTP_Resources_BusinessType_Workspace_NoInventoryDependency(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// 故意不设 full_inventory_time
	now := time.Now()
	seedResource(t, db, "ws-yes-1", "W1", now, 2)
	seedResource(t, db, "ws-yes-2", "W2", now, 1)
	seedResource(t, db, "ws-no", "W3", now, 0) // 不在工作空间

	status, resp := jsonReqNoBody(t, r, "GET", "/resources?businessTypeFilter=workspace&pageSize=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	names := resourceNames(t, d)
	if len(names) != 2 {
		t.Errorf("workspace tab should return 2 resources, got %d: %v", len(names), names)
	}
	for _, n := range names {
		if n == "ws-no" {
			t.Errorf("workspace tab should not include ws-no: %v", names)
		}
	}
}

// new_access tab：first_create_time > inventory_time
func TestHTTP_Resources_BusinessType_NewAccess(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetFullInventoryTime("2026-01-01T00:00:00Z")

	hist := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	seedResource(t, db, "old", "N1", hist, 1)
	seedResource(t, db, "fresh", "N2", newer, 1)

	status, resp := jsonReqNoBody(t, r, "GET", "/resources?businessTypeFilter=new_access&pageSize=100")
	successOk(t, status, resp)
	names := resourceNames(t, dataMap(t, resp))
	if len(names) != 1 || names[0] != "fresh" {
		t.Errorf("new_access should return only [fresh], got %v", names)
	}
}

// new_access tab + 未设 full_inventory_time：passthrough（与 FilesView 一致）
func TestHTTP_Resources_BusinessType_NewAccess_NoInventoryTime_Passthrough(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	seedResource(t, db, "a", "P1", now, 1)
	seedResource(t, db, "b", "P2", now, 0)

	status, resp := jsonReqNoBody(t, r, "GET", "/resources?businessTypeFilter=new_access&pageSize=100")
	successOk(t, status, resp)
	names := resourceNames(t, dataMap(t, resp))
	if len(names) != 2 {
		t.Errorf("new_access without inventory time should passthrough (2), got %d: %v", len(names), names)
	}
}

// history_inventory tab：first_create_time < inventory_time
func TestHTTP_Resources_BusinessType_History(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetFullInventoryTime("2026-01-01T00:00:00Z")

	hist := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	seedResource(t, db, "old", "H1", hist, 1)
	seedResource(t, db, "fresh", "H2", newer, 1)

	status, resp := jsonReqNoBody(t, r, "GET", "/resources?businessTypeFilter=history_inventory&pageSize=100")
	successOk(t, status, resp)
	names := resourceNames(t, dataMap(t, resp))
	if len(names) != 1 || names[0] != "old" {
		t.Errorf("history_inventory should return only [old], got %v", names)
	}
}

// history_inventory + 未设 full_inventory_time：返空（与 FilesView 一致）
func TestHTTP_Resources_BusinessType_History_NoInventoryTime_Empty(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	seedResource(t, db, "a", "Q1", now, 1)
	seedResource(t, db, "b", "Q2", now, 0)

	status, resp := jsonReqNoBody(t, r, "GET", "/resources?businessTypeFilter=history_inventory&pageSize=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	names := resourceNames(t, d)
	if len(names) != 0 {
		t.Errorf("history_inventory without inventory time should be empty, got %v", names)
	}
	if got := int(d["total"].(float64)); got != 0 {
		t.Errorf("history_inventory total without inventory time = %d, want 0", got)
	}
}
