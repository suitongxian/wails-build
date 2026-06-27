package httpd

import (
	"fmt"
	"testing"
	"time"

	"data-asset-scan-go/internal/repository"
)

// V5-P1 Q3: 桥接 fv 没 storage_uri 但有 checksum，前端通过 source-distribution 端点反查物理路径。
// 测试链路：seed resource + distribution → bridge 出 fv（无 storage_uri 有 checksum）→ 调端点 → 收到 path。
func TestHTTP_FileVersion_SourceDistribution_FromBridge(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	seedPersonalProjectsForAI(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	// 注：现有 seedSimpleResourceWithDist 因列名漂移导致 distribution 行实际未入库。
	// 这里手动插入一行匹配真实 schema，确保反查能命中。
	resID := seedSimpleResourceWithDist(t, db, "测试.pdf", "SRCQ3001", "/Users/x/files/")
	now := time.Now()
	if _, err := db.Exec(`INSERT INTO data_distributing (
		scan_task_id, path, data_type, scan_found_count, content_sign,
		file_suffix, file_size, ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (NULL, ?, 1, 1, ?, ?, ?, '127.0.0.1', '00:00:00:00:00:00', ?, ?, ?, 0)`,
		"/Users/x/files/测试.pdf", "SRCQ3001", ".pdf", int64(2048), now, now, now); err != nil {
		t.Fatalf("insert data_distributing: %v", err)
	}

	// importance_level=2 让桥接走通
	if _, err := db.Exec(`UPDATE data_resources SET importance_level = 2 WHERE data_resources_id = ?`, resID); err != nil {
		t.Fatal(err)
	}

	fvID, err := repository.BridgeClassifyToPersonalProject(db, resID)
	if err != nil || fvID == 0 {
		t.Fatalf("bridge failed: fvID=%d err=%v", fvID, err)
	}

	// 桥接 fv 的 storage_uri 应为空（前置条件）
	var storage *string
	if err := db.Get(&storage, `SELECT storage_uri FROM file_versions WHERE id = ?`, fvID); err != nil {
		t.Fatal(err)
	}
	if storage != nil && *storage != "" {
		t.Fatalf("桥接 fv storage_uri 应为空，得到 %q", *storage)
	}

	status, resp := jsonReqNoBody(t, r, "GET", fmt.Sprintf("/file-versions/%d/source-distribution", fvID))
	successOk(t, status, resp)

	d := dataMap(t, resp)
	if d["checksum"] != "SRCQ3001" {
		t.Errorf("checksum 应为 SRCQ3001, got %v", d["checksum"])
	}
	if d["path"] != "/Users/x/files/测试.pdf" {
		t.Errorf("path 应为 /Users/x/files/测试.pdf, got %v", d["path"])
	}
	if sz, _ := d["file_size"].(float64); int64(sz) != 2048 {
		t.Errorf("file_size 应为 2048, got %v", d["file_size"])
	}
}

// fv 不存在 → 404 + failure
func TestHTTP_FileVersion_SourceDistribution_NotFound(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()
	status, resp := jsonReqNoBody(t, r, "GET", "/file-versions/999999/source-distribution")
	expectFailure(t, status, resp)
}
