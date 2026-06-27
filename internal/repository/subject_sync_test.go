package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubjectSyncerFetchesManageSubjectsByCode(t *testing.T) {
	db := openTestDB(t)
	configRepo := NewSystemConfigRepository(db)

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/subjects/list" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "success",
			"data": []map[string]interface{}{
				{"code": "DEPT-MARKET", "name": "市场部", "type": "department", "status": "active", "contact": "market@example.test"},
				{"code": "ORG-SECURITY", "name": "安全办", "type": "organization", "status": "inactive"},
			},
		})
	}))
	defer mock.Close()
	configRepo.SetValue(KeyManageEndpoint, mock.URL)

	res, err := NewSubjectSyncer(db, configRepo).SyncAll()
	if err != nil {
		t.Fatalf("sync subjects: %v", err)
	}
	if res.TotalRemote != 2 || res.Synced != 2 {
		t.Fatalf("unexpected sync result: %+v", res)
	}

	var name, status string
	if err := db.QueryRow(`SELECT name, status FROM subjects WHERE code = ? AND disable = 0`, "ORG-SECURITY").Scan(&name, &status); err != nil {
		t.Fatalf("subject not cached: %v", err)
	}
	if name != "安全办" || status != "inactive" {
		t.Fatalf("unexpected cached subject: name=%s status=%s", name, status)
	}
}
