package httpd

import (
	"database/sql"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// V5-Phase1 §4.3-2 AI 归目驳回端点 — 正向链路：
//   - POST /ai/classify/reject 写 rejected_at + reject_reason
//   - 驳回后 GET /ai/classify/pending 不再返回该资源
func TestHTTP_AIClassify_Reject(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	withActiveUser(t, db, "u1")
	resID := seedSimpleResourceWithDist(t, db, "测试.pdf", "AIRJ001", "/")

	status, resp := jsonReq(t, r, "POST", "/ai/classify/reject", map[string]interface{}{
		"resource_id": resID,
		"reason":      "用户决定不归目（私人文件）",
	})
	successOk(t, status, resp)

	// 字段写入校验
	var rejAt sql.NullTime
	var rejReason sql.NullString
	if err := db.QueryRow(
		`SELECT ai_classify_rejected_at, ai_classify_reject_reason FROM data_resources WHERE data_resources_id = ?`,
		resID,
	).Scan(&rejAt, &rejReason); err != nil {
		t.Fatal(err)
	}
	if !rejAt.Valid {
		t.Error("ai_classify_rejected_at 应非空")
	}
	if rejReason.String != "用户决定不归目（私人文件）" {
		t.Errorf("reason 不对: %s", rejReason.String)
	}

	// pending 列表不应再包含被驳回的资源
	status, resp = jsonReqNoBody(t, r, "GET", "/ai/classify/pending?page_size=100&origin=new")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	for _, item := range items {
		m := item.(map[string]interface{})
		if int64(m["resource_id"].(float64)) == resID {
			t.Errorf("被驳回的 resource 不应出现在 pending: %d", resID)
		}
	}
}

// V5-Phase1 §4.3-2 reason 必填
func TestHTTP_AIClassify_Reject_MissingReason(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	status, resp := jsonReq(t, r, "POST", "/ai/classify/reject", map[string]interface{}{
		"resource_id": int64(1),
	})
	expectFailure(t, status, resp)
}

// V5-Phase1 §4.3-2 资源 id 必须存在
func TestHTTP_AIClassify_Reject_InvalidResourceID(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	status, resp := jsonReq(t, r, "POST", "/ai/classify/reject", map[string]interface{}{
		"resource_id": int64(99999),
		"reason":      "test",
	})
	expectFailure(t, status, resp)
}
