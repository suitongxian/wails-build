package ai

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestEnrichInputForResource_BasicFields(t *testing.T) {
	db := setupTestDBForEnricher(t)
	defer db.Close()

	now := time.Now()
	r, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable
	) VALUES ('SIGN001', 1, 1, ?, '报表.xlsx', 2, 0, ?, ?, 0)`, now, now, now)
	resID, _ := r.LastInsertId()
	db.Exec(`INSERT INTO data_distributing (
		scan_task_id, path, data_type, scan_found_count, content_sign, file_suffix, file_magic,
		file_create_time, file_update_time, file_size, ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (1, '/Users/x/work/报表.xlsx', 1, 1, 'SIGN001', 'xlsx',
		'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
		?, ?, 102400, '127.0.0.1', 'aa:bb:cc:dd:ee:ff', ?, ?, ?, 0)`,
		now, now, now, now, now)

	in, err := EnrichInputForResource(db, resID)
	if err != nil {
		t.Fatalf("enrich failed: %v", err)
	}
	if in.FileName != "报表.xlsx" {
		t.Errorf("FileName wrong: %s", in.FileName)
	}
	if in.Path != "/Users/x/work/报表.xlsx" {
		t.Errorf("Path wrong: %s", in.Path)
	}
	if in.Metadata["file_size"] != "102400" {
		t.Errorf("file_size metadata missing/wrong: %s", in.Metadata["file_size"])
	}
	if in.Metadata["mime"] != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("mime metadata wrong: %s", in.Metadata["mime"])
	}
	if in.Metadata["ext"] != "xlsx" {
		t.Errorf("ext metadata wrong: %s", in.Metadata["ext"])
	}
	if in.Metadata["parent_dir"] != "work" {
		t.Errorf("parent_dir should be 'work', got %s", in.Metadata["parent_dir"])
	}
	if in.Metadata["sibling_count"] == "" {
		t.Errorf("sibling_count should be set even if 0, got empty")
	}
}

func TestEnrichInputForResource_NoDistribution(t *testing.T) {
	db := setupTestDBForEnricher(t)
	defer db.Close()
	now := time.Now()
	r, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable
	) VALUES ('SIGN_NO_DIST', 1, 1, ?, '孤资源.pdf', 2, 0, ?, ?, 0)`, now, now, now)
	resID, _ := r.LastInsertId()

	in, err := EnrichInputForResource(db, resID)
	if err != nil {
		t.Fatalf("enrich failed: %v", err)
	}
	if in.FileName != "孤资源.pdf" {
		t.Errorf("FileName wrong: %s", in.FileName)
	}
	if in.Path != "" {
		t.Errorf("Path should be empty when no distribution, got %s", in.Path)
	}
	// Metadata 应该是空 map（已初始化）但没具体 keys
	if in.Metadata == nil {
		t.Errorf("Metadata should be initialized empty, not nil")
	}
}

func TestEnrichInputForResource_SiblingCount(t *testing.T) {
	db := setupTestDBForEnricher(t)
	defer db.Close()
	now := time.Now()

	// 主资源
	r, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable
	) VALUES ('SIB_MAIN', 1, 1, ?, '主.txt', 2, 0, ?, ?, 0)`, now, now, now)
	resID, _ := r.LastInsertId()
	db.Exec(`INSERT INTO data_distributing (
		scan_task_id, path, data_type, scan_found_count, content_sign, file_suffix, file_magic,
		file_size, ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (1, '/work/folder/主.txt', 1, 1, 'SIB_MAIN', 'txt', 'text/plain', 100, '127.0.0.1', 'aa', ?, ?, ?, 0)`, now, now, now)

	// 同目录的 3 个 sibling（不同 content_sign）
	for i := 1; i <= 3; i++ {
		sign := "SIB_OTHER_" + string(rune('0'+i))
		db.Exec(`INSERT INTO data_distributing (
			scan_task_id, path, data_type, scan_found_count, content_sign, file_suffix, file_magic,
			file_size, ip, mac_address, scan_time, create_time, update_time, disable
		) VALUES (1, ?, 1, 1, ?, 'txt', 'text/plain', 100, '127.0.0.1', 'aa', ?, ?, ?, 0)`,
			"/work/folder/sibling-"+string(rune('0'+i))+".txt", sign, now, now, now)
	}

	// 不同目录的文件（不应被算入 sibling）
	db.Exec(`INSERT INTO data_distributing (
		scan_task_id, path, data_type, scan_found_count, content_sign, file_suffix, file_magic,
		file_size, ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (1, '/other/dir/外.txt', 1, 1, 'OTHER_DIR', 'txt', 'text/plain', 100, '127.0.0.1', 'aa', ?, ?, ?, 0)`, now, now, now)

	in, err := EnrichInputForResource(db, resID)
	if err != nil {
		t.Fatal(err)
	}
	if in.Metadata["sibling_count"] != "3" {
		t.Errorf("sibling_count 应=3, got %s", in.Metadata["sibling_count"])
	}
}

// setupTestDBForEnricher 创建一个 in-memory SQLite，仅加载 data_resources + data_distributing schema
func setupTestDBForEnricher(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:?_journal_mode=WAL&_foreign_keys=1")
	if err != nil {
		t.Fatal(err)
	}
	schema := `
	CREATE TABLE data_resources (
		data_resources_id INTEGER PRIMARY KEY AUTOINCREMENT,
		content_sign TEXT NOT NULL,
		source_count INTEGER NOT NULL DEFAULT 1,
		workspace_source_count INTEGER NOT NULL DEFAULT 1,
		first_create_time DATETIME NOT NULL,
		resources_name TEXT,
		resources_desc TEXT,
		content_subject TEXT,
		claim_status INTEGER DEFAULT 0,
		importance_level INTEGER DEFAULT 0,
		create_time DATETIME NOT NULL,
		update_time DATETIME NOT NULL,
		disable INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE data_distributing (
		data_distribution_id INTEGER PRIMARY KEY AUTOINCREMENT,
		scan_task_id INTEGER,
		path TEXT NOT NULL,
		data_type INTEGER NOT NULL DEFAULT 1,
		scan_found_count INTEGER NOT NULL DEFAULT 1,
		content_sign TEXT NOT NULL,
		file_suffix TEXT,
		file_magic TEXT,
		file_create_time DATETIME,
		file_update_time DATETIME,
		file_read_time DATETIME,
		file_size INTEGER NOT NULL DEFAULT 0,
		file_hide INTEGER DEFAULT 0,
		upload_state INTEGER DEFAULT 0,
		ip TEXT NOT NULL DEFAULT '',
		mac_address TEXT NOT NULL DEFAULT '',
		parent_id INTEGER,
		scan_time DATETIME NOT NULL,
		create_time DATETIME NOT NULL,
		update_time DATETIME NOT NULL,
		disable INTEGER NOT NULL DEFAULT 0
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}
