package httpd

import (
	"testing"
	"time"
)

func TestHTTP_AIClassify_Pending_DefaultIsNew(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()

	rows := []struct{ name, sign, origin string }{
		{"hist.pdf", "HIST001", "historical"},
		{"newA.pdf", "NEWA001", "new"},
		{"newB.pdf", "NEWB001", "new"},
	}
	for _, x := range rows {
		_, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, 0, ?)`,
			x.sign, now, x.name, now, now, x.origin)
		if err != nil {
			t.Fatalf("seed %s: %v", x.sign, err)
		}
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	if len(items) != 2 {
		t.Errorf("default pending = %d items, want 2 (new only)", len(items))
	}
	total, _ := d["total"].(float64)
	if int(total) != 2 {
		t.Errorf("total = %v, want 2", total)
	}
}

func TestHTTP_AIClassify_Pending_HistoricalSkipsSuggestions(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()
	_, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
	) VALUES ('HIST_NO_SUG', 1, 1, ?, '历史报表.xlsx', 2, 0, ?, ?, 0, 'historical')`,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending?origin=historical")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("historical pending items = %d, want 1", len(items))
	}
	first, _ := items[0].(map[string]interface{})
	sugs, _ := first["suggestions"].([]interface{})
	if len(sugs) != 0 {
		t.Errorf("historical suggestions length = %d, want 0", len(sugs))
	}
}

func TestHTTP_AIClassify_Pending_Pagination(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()
	for i := 0; i < 5; i++ {
		_, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, 0, 'historical')`,
			"H_"+itoa(int64(i)), now, "h.pdf", now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending?origin=historical&page=2&page_size=2")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	if len(items) != 2 {
		t.Errorf("page=2 page_size=2 items = %d, want 2", len(items))
	}
	total, _ := d["total"].(float64)
	if int(total) != 5 {
		t.Errorf("total = %v, want 5", total)
	}
	page, _ := d["page"].(float64)
	if int(page) != 2 {
		t.Errorf("page = %v, want 2", page)
	}
}
