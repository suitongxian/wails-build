package httpd

import (
	"testing"
	"time"
)

// 验证 GET /files 返回的 copy_count 来自 LEFT JOIN data_resources 的 source_count。
// 历史 bug：FileRecord.CopyCount 字段一直存在但 handler 从不填，前端「N 副本」chip 永远不显示。
// 重构到 ListFilesWithFilters 后由 SQL JOIN 拿到值；这条测试守住回归。
func TestHTTP_Files_CopyCount_FromDataResources(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	// 1. 一个 content_sign 在 data_resources 里 source_count=5
	seedDataDistribution(t, db, "/a/many-copies.pdf", "MC1", &now, 1)
	if _, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES (?, 5, 0, ?, ?, 0, 0, ?, ?, 0, 'historical')`,
		"MC1", now, "many-copies.pdf", now, now); err != nil {
		t.Fatal(err)
	}

	// 2. 另一个 content_sign 没对应 data_resources → copy_count 应为 0（omitempty 也算 0 语义）
	seedDataDistribution(t, db, "/a/no-resource.pdf", "NR1", &now, 1)

	status, resp := jsonReqNoBody(t, r, "GET", "/files?pageSize=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	raw, _ := d["files"].([]interface{})
	got := map[string]int{}
	for _, f := range raw {
		m := f.(map[string]interface{})
		path := m["path"].(string)
		// omitempty 时 0 值不在 map 里
		if v, ok := m["copy_count"].(float64); ok {
			got[path] = int(v)
		} else {
			got[path] = 0
		}
	}

	if got["/a/many-copies.pdf"] != 5 {
		t.Errorf("/a/many-copies.pdf copy_count = %d, want 5", got["/a/many-copies.pdf"])
	}
	if got["/a/no-resource.pdf"] != 0 {
		t.Errorf("/a/no-resource.pdf copy_count = %d, want 0", got["/a/no-resource.pdf"])
	}
}

// 验证 data_resources 已 disable=1 时不被 JOIN 进来（copy_count=0）
func TestHTTP_Files_CopyCount_IgnoresDisabledResources(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	seedDataDistribution(t, db, "/a/has-disabled-resource.pdf", "DR1", &now, 1)
	// data_resources 已 disable=1
	if _, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES (?, 9, 0, ?, ?, 0, 0, ?, ?, 1, 'historical')`,
		"DR1", now, "x.pdf", now, now); err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/files?pageSize=100")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	raw, _ := d["files"].([]interface{})
	for _, f := range raw {
		m := f.(map[string]interface{})
		if m["path"] == "/a/has-disabled-resource.pdf" {
			if v, ok := m["copy_count"].(float64); ok && int(v) != 0 {
				t.Errorf("disabled data_resources should not contribute copy_count, got %v", v)
			}
			return
		}
	}
	t.Fatal("seed row not found in response")
}
