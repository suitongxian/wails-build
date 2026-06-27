package repository

import (
	"testing"
)

// 新装库：suspect_non_personal 列存在 + 默认 0
func TestRunMigrations_AddsSuspectColumn(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	exists, err := columnExists(db, "data_distributing", "suspect_non_personal")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("data_distributing.suspect_non_personal not added by migration")
	}

	// 插一行不指定 suspect_non_personal，校验默认 0
	_, err = db.Exec(`INSERT INTO data_distributing
		(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time, create_time, update_time)
		VALUES ('/x.txt', 0, 1, 'CS', 100, '', '', datetime('now'), datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	var got int
	if err := db.Get(&got, `SELECT suspect_non_personal FROM data_distributing WHERE path = '/x.txt'`); err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("default suspect_non_personal = %d, want 0", got)
	}
}

// 既有库二次跑迁移：幂等，不报错，已有 suspect 值不被清零
func TestRunMigrations_SuspectColumnIdempotent(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	// 写一行 suspect=1
	_, err := db.Exec(`INSERT INTO data_distributing
		(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time,
		 suspect_non_personal, create_time, update_time)
		VALUES ('/sys.dll', 0, 1, 'CS', 100, '', '', datetime('now'), 1, datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}

	// 再跑一次迁移
	if err := runMigrations(db); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}

	var got int
	if err := db.Get(&got, `SELECT suspect_non_personal FROM data_distributing WHERE path = '/sys.dll'`); err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("after re-migrate: suspect = %d, want 1 (should not be reset)", got)
	}
}
