package httpd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 方案甲端到端：认领归档保护「归类」为重要级后，文件实体应被复制进本机
// 「个人重要文件夹/{工作事项}/」，使「档案在线阅卷·个人」可见。复制不删原件。
func TestHTTP_SingleClassify_CopiesToPersonalFolder(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	root := withProjectRoot(t, db) // GetEffectiveProjectRoot → personalRoot

	now := time.Now()
	// 真实原始文件
	srcDir := filepath.Join(root, "扫描源")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "历史台账.xlsx")
	if err := os.WriteFile(src, []byte("DATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	// distributing(指向真实文件) + resources(已认领为个人工作, 带工作事项)
	db.Exec(`INSERT INTO data_distributing
		(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address,
		 scan_time, create_time, update_time, disable, file_create_time)
		VALUES (?, 1, 1, 'CP_001', 4, '', '', ?, ?, ?, 0, ?)`, src, now, now, now, now)
	res, _ := db.Exec(`INSERT INTO data_resources
		(content_sign, source_count, workspace_source_count, first_create_time,
		 resources_name, content_subject, claim_status, importance_level,
		 create_time, update_time, disable, data_origin)
		VALUES ('CP_001', 1, 1, ?, '历史台账', '二季度核算', 2, 0, ?, ?, 0, 'new')`, now, now, now)
	rid, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/resources/classify/single", map[string]interface{}{
		"data_resources_id": rid,
		"importance_level":  2, // 重要级
	})
	successOk(t, status, resp)

	// 文件已复制进个人重要文件夹/二季度核算/
	dst := filepath.Join(root, "个人重要文件夹", "二季度核算", "历史台账.xlsx")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("应复制到 %s：%v", dst, err)
	}
	// 原件不动
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("原始文件不应被删除/移动：%v", err)
	}

	// 「档案在线阅卷·个人」重要级应能列出
	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/personal-archive-files?level=important")
	if status != 200 {
		t.Fatalf("列出个人归档文件 status=%d", status)
	}
	data, _ := resp["data"].([]interface{})
	found := false
	for _, it := range data {
		m, _ := it.(map[string]interface{})
		if m["file_name"] == "历史台账.xlsx" && m["project_name"] == "二季度核算" {
			found = true
		}
	}
	if !found {
		t.Fatalf("个人重要文件夹应列出归类后的文件，实得 %v", data)
	}
}

// 不予归目(5)不复制实体。
func TestHTTP_SingleClassify_NoCopyForNonArchive(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	root := withProjectRoot(t, db)

	now := time.Now()
	db.Exec(`INSERT INTO data_distributing
		(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address,
		 scan_time, create_time, update_time, disable, file_create_time)
		VALUES ('/nonexist/x.txt', 1, 1, 'CP_002', 1, '', '', ?, ?, ?, 0, ?)`, now, now, now, now)
	res, _ := db.Exec(`INSERT INTO data_resources
		(content_sign, source_count, workspace_source_count, first_create_time,
		 resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin)
		VALUES ('CP_002', 1, 1, ?, 'y', 2, 0, ?, ?, 0, 'new')`, now, now, now)
	rid, _ := res.LastInsertId()

	status, resp := jsonReq(t, r, "POST", "/resources/classify/single", map[string]interface{}{
		"data_resources_id": rid,
		"importance_level":  5, // 不予归目
	})
	successOk(t, status, resp)

	for _, lvl := range []string{"个人核心文件夹", "个人重要文件夹", "个人一般文件夹"} {
		if _, err := os.Stat(filepath.Join(root, lvl)); err == nil {
			t.Fatalf("不予归目不应创建/写入个人文件夹 %s", lvl)
		}
	}
}
