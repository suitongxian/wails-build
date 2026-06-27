package httpd

import (
	"testing"
	"time"
)

func TestHTTP_AIClassify_Pending_FiltersByImportance(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now()

	rows := []struct {
		name, sign string
		level      int
	}{
		{"core.pdf", "TC_CORE", 1},       // 核心：importance_level=1，pending 已经过滤掉（默认只看 importance=0）
		{"privacy.pdf", "TC_PRIV", 4},    // 隐私：同上
		{"general.pdf", "TC_GEN", 0},     // 未分类：进 pending
		{"already_imp.pdf", "TC_IMP", 2}, // 重要：已分类，不在 pending
	}
	for _, x := range rows {
		_, err := db.Exec(`INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, claim_status, importance_level, create_time, update_time, disable, data_origin
		) VALUES (?, 1, 1, ?, ?, 2, ?, ?, ?, 0, 'new')`,
			x.sign, now, x.name, x.level, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending?origin=new")
	successOk(t, status, resp)
	items, _ := dataMap(t, resp)["items"].([]interface{})

	foundGeneral := false
	for _, it := range items {
		m := it.(map[string]interface{})
		switch m["resource_name"] {
		case "general.pdf":
			foundGeneral = true
		case "core.pdf":
			t.Error("核心级 (importance=1) 不应出现在 pending")
		case "privacy.pdf":
			t.Error("隐私级 (importance=4) 不应出现在 pending")
		case "already_imp.pdf":
			t.Error("已分类 (importance=2) 不应出现在 pending（默认过滤 importance=0）")
		}
	}
	if !foundGeneral {
		t.Error("一般级未分类 (importance=0) 应该出现在 pending")
	}
}
