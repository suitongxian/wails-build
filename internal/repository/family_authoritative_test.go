package repository

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func seedAuthFamily(t *testing.T, db *sqlx.DB, n int) (familyID int64, resourceIDs []int64) {
	t.Helper()
	now := time.Now()
	r, err := db.Exec(`INSERT INTO data_resource_family (
		primary_content_sign, primary_resource_id, member_count, algorithm, highest_score,
		create_time, update_time, disable
	) VALUES ('PCS', 0, ?, 'simhash', 0.9, ?, ?, 0)`, n, now, now)
	if err != nil {
		t.Fatal(err)
	}
	familyID, _ = r.LastInsertId()
	for i := 0; i < n; i++ {
		rs, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, family_id, family_relation,
			create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, 'derived', ?, ?, 0, 'new')`,
			"CS_"+itoaSlow(i), now, "m"+itoaSlow(i)+".pdf", familyID, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := rs.LastInsertId()
		resourceIDs = append(resourceIDs, id)
	}
	return familyID, resourceIDs
}

func itoaSlow(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string('0'+byte(n%10)) + out
		n /= 10
	}
	return out
}

func TestSetAuthoritativeResource(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	fid, ids := seedAuthFamily(t, db, 3)

	if err := SetAuthoritativeResource(db, fid, ids[1]); err != nil {
		t.Fatalf("set: %v", err)
	}
	var got int64
	db.Get(&got, `SELECT authoritative_resource_id FROM data_resource_family WHERE family_id = ?`, fid)
	if got != ids[1] {
		t.Errorf("authoritative_resource_id = %d, want %d", got, ids[1])
	}

	// 改判
	if err := SetAuthoritativeResource(db, fid, ids[2]); err != nil {
		t.Fatalf("re-set: %v", err)
	}
	db.Get(&got, `SELECT authoritative_resource_id FROM data_resource_family WHERE family_id = ?`, fid)
	if got != ids[2] {
		t.Errorf("after re-set = %d, want %d", got, ids[2])
	}
}

func TestNeedsAuthoritativeArbitration(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	fid, ids := seedAuthFamily(t, db, 3)

	needs, err := NeedsAuthoritativeArbitration(db, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if !needs {
		t.Error("family 3 成员且未指定权威 → 应需要仲裁")
	}

	_ = SetAuthoritativeResource(db, fid, ids[1])
	needs, _ = NeedsAuthoritativeArbitration(db, ids[0])
	if needs {
		t.Error("已指定权威后不再需要仲裁")
	}
}

func TestNeedsAuthoritativeArbitration_StandaloneResource(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	now := time.Now()
	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('STANDALONE', 1, 1, ?, 'lone.pdf', 2, 0, ?, ?, 0, 'new')`, now, now, now)
	rid, _ := res.LastInsertId()

	needs, err := NeedsAuthoritativeArbitration(db, rid)
	if err != nil {
		t.Fatal(err)
	}
	if needs {
		t.Error("无 family 的资源不应需要仲裁")
	}
}
