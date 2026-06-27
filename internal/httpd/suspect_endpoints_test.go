package httpd

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// 在 DB 中种 N 个 data_distributing + data_resources：
//   - good_*: suspect=0
//   - bad_*:  suspect=1
//   - already_claimed_bad: suspect=1 但 claim_status 已是 1（不该被一键覆盖）
func seedSuspectFixture(t *testing.T, db *sqlx.DB) (goodCount, badUnclaimedCount int) {
	t.Helper()
	now := time.Now()
	insertDD := func(path, cs string, suspect int) {
		_, err := db.Exec(`INSERT INTO data_distributing
			(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time,
			 suspect_non_personal, create_time, update_time, disable)
			VALUES (?, 0, 1, ?, 100, '', '', ?, ?, ?, ?, 0)`,
			path, cs, now, suspect, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertDR := func(name, cs string, claimStatus int) {
		_, err := db.Exec(`INSERT INTO data_resources
			(content_sign, source_count, workspace_source_count, first_create_time,
			 resources_name, claim_status, is_claimed, create_time, update_time, disable)
			VALUES (?, 1, 1, ?, ?, ?, ?, ?, ?, 0)`,
			cs, now, name, claimStatus, claimStatus > 0, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	insertDD("/Users/u/Library/Caches/x.cache", "CS_BAD_1", 1)
	insertDR("x.cache", "CS_BAD_1", 0)
	insertDD("/path/to/lib.dll", "CS_BAD_2", 1)
	insertDR("lib.dll", "CS_BAD_2", 0)
	insertDD("/path/to/font.ttf", "CS_BAD_3", 1)
	insertDR("font.ttf", "CS_BAD_3", 0)

	insertDD("/path/to/合同.pdf", "CS_OK_1", 0)
	insertDR("合同.pdf", "CS_OK_1", 0)
	insertDD("/path/to/报告.docx", "CS_OK_2", 0)
	insertDR("报告.docx", "CS_OK_2", 0)

	insertDD("/path/to/manually_claimed.dll", "CS_BAD_DONE", 1)
	insertDR("manually_claimed.dll", "CS_BAD_DONE", 1) // 已认领为隐私

	return 2, 3 // 2 good + 3 bad unclaimed
}

func TestHTTP_GetSuspectSummary(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	_, wantBad := seedSuspectFixture(t, db)

	status, resp := jsonReqNoBody(t, r, "GET", "/resources/suspect-summary")
	successOk(t, status, resp)
	d := dataMap(t, resp)

	if n, _ := d["count"].(float64); int(n) != wantBad {
		t.Errorf("count = %v, want %d (3 suspect unclaimed)", d["count"], wantBad)
	}

	samples, ok := d["sample_paths"].([]interface{})
	if !ok {
		t.Fatalf("sample_paths missing or wrong type: %T", d["sample_paths"])
	}
	if len(samples) != wantBad {
		t.Errorf("sample_paths len = %d, want %d", len(samples), wantBad)
	}
}

func TestHTTP_GetSuspectSummary_NoneSuspect_ReturnsZeroAndEmpty(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now()
	_, _ = db.Exec(`INSERT INTO data_distributing
		(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time,
		 suspect_non_personal, create_time, update_time, disable)
		VALUES ('/a.pdf', 0, 1, 'CS_A', 100, '', '', ?, 0, ?, ?, 0)`, now, now, now)
	_, _ = db.Exec(`INSERT INTO data_resources
		(content_sign, source_count, workspace_source_count, first_create_time,
		 resources_name, claim_status, create_time, update_time, disable)
		VALUES ('CS_A', 1, 1, ?, 'a.pdf', 0, ?, ?, 0)`, now, now, now)

	status, resp := jsonReqNoBody(t, r, "GET", "/resources/suspect-summary")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if n, _ := d["count"].(float64); int(n) != 0 {
		t.Errorf("count = %v, want 0", d["count"])
	}
}

func TestHTTP_IgnoreAllSuspect(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	_, wantUpdated := seedSuspectFixture(t, db)

	body := map[string]interface{}{
		"claimant_name": "alice",
		"claimant_unit": "test-team",
	}
	status, resp := jsonReq(t, r, "POST", "/resources/ignore-all-suspect", body)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if n, _ := d["updatedCount"].(float64); int(n) != wantUpdated {
		t.Errorf("updatedCount = %v, want %d", d["updatedCount"], wantUpdated)
	}

	// 校验：3 个 suspect 都变 claim_status=4；good 文件 + 已认领 bad 不动
	cases := []struct {
		cs   string
		want int
	}{
		{"CS_BAD_1", 4}, {"CS_BAD_2", 4}, {"CS_BAD_3", 4},
		{"CS_OK_1", 0}, {"CS_OK_2", 0},
		{"CS_BAD_DONE", 1}, // 之前已认领，不该被覆盖
	}
	for _, c := range cases {
		var got int
		if err := db.Get(&got, `SELECT claim_status FROM data_resources WHERE content_sign = ?`, c.cs); err != nil {
			t.Fatalf("query %s: %v", c.cs, err)
		}
		if got != c.want {
			t.Errorf("%s claim_status = %d, want %d", c.cs, got, c.want)
		}
	}
}

func TestHTTP_IgnoreAllSuspect_MissingClaimant(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := map[string]interface{}{"claimant_unit": "team"}
	status, _ := jsonReq(t, r, "POST", "/resources/ignore-all-suspect", body)
	if status != 400 {
		t.Errorf("status = %d, want 400 (missing claimant_name)", status)
	}
}
