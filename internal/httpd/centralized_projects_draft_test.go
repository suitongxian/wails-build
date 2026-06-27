package httpd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 迁移后 centralized_project_applications 应含 4 个新列。
func TestMigration_CentralizedProjectNewColumns(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()

	rows, err := db.Query(`PRAGMA table_info(centralized_project_applications)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	for _, want := range []string{"project_code", "department", "approval_basis", "description"} {
		if !cols[want] {
			t.Errorf("缺少列 %s", want)
		}
	}
}

// 存草稿：status=draft，不推 manage（mock 命中 0 次）。
func TestHTTP_CentralizedProjects_SaveAsDraft(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	hits := 0
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":777}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 草稿允许只填项目名称（不填负责人/敏感级也能存）
	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name":  "半成品立项",
		"save_as_draft": true,
	})
	successOk(t, status, resp)
	if dataMap(t, resp)["status"] != "draft" {
		t.Errorf("status=%v want draft", dataMap(t, resp)["status"])
	}
	if hits != 0 {
		t.Errorf("草稿不应推送 manage，hits=%d", hits)
	}
}

// 立项选「项目层级(person/department/unit)」：本地落库 + 随提交推送给 manage。
func TestHTTP_CentralizedProjects_StoresAndPushesProjectScope(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredUser(t, db, "张三")

	var pushedScope string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/submit") {
			var body map[string]interface{}
			_ = json.NewDecoder(req.Body).Decode(&body)
			if v, ok := body["project_scope"].(string); ok {
				pushedScope = v
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":55,"project_code":"XM-2026-0055"}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "部门项目", "owner_name": "张三",
		"sensitivity_level": "important", "project_scope": "department", "save_as_draft": false,
	})
	successOk(t, status, resp)

	var scope string
	if err := db.Get(&scope, `SELECT project_scope FROM centralized_project_applications ORDER BY id DESC LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if scope != "department" {
		t.Fatalf("本地应落 project_scope=department，实得 %s", scope)
	}
	if pushedScope != "department" {
		t.Fatalf("应推送 project_scope=department 给 manage，实得 %s", pushedScope)
	}
}

// 草稿不应被 refresh 卷入 manage 同步：其 id 不出现在 by-origins 请求的 origin_ids 中。
func TestHTTP_CentralizedProjects_RefreshSkipsDrafts(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredUser(t, db, "张三")

	var capturedOrigins string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "by-origins") {
			capturedOrigins = req.URL.Query().Get("origin_ids")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 一条草稿（不推送）
	_, dResp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "草稿C", "save_as_draft": true,
	})
	draftID := dResp["data"].(map[string]interface{})["id"]

	// 一条正式发布（会推送，manage_remote_id 落库），保证 by-origins 真正被调用
	_, _ = jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "正式D", "owner_name": "张三", "sensitivity_level": "general", "save_as_draft": false,
	})

	// refresh
	status, resp := jsonReq(t, r, "POST", "/centralized-projects/refresh", map[string]interface{}{})
	successOk(t, status, resp)

	// 草稿 id 不应出现在 origin_ids 中
	for _, idStr := range strings.Split(capturedOrigins, ",") {
		if idStr == fmt.Sprintf("%v", draftID) {
			t.Errorf("草稿 id %v 不应被 refresh 卷入：origin_ids=%s", draftID, capturedOrigins)
		}
	}
	// 草稿状态仍为 draft
	var st string
	_ = db.Get(&st, `SELECT status FROM centralized_project_applications WHERE id=?`, draftID)
	if st != "draft" {
		t.Errorf("草稿状态被 refresh 改了：%s", st)
	}
}

// 编辑草稿仍为 draft；带 save_as_draft=false 则发布为 approved 并推送；非草稿拒绝编辑。
func TestHTTP_CentralizedProjects_UpdateDraft(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredUser(t, db, "张三")

	hits := 0
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/api/centralized-projects/submit") {
			hits++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":777}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 先建草稿
	_, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "草稿A", "save_as_draft": true,
	})
	id := resp["data"].(map[string]interface{})["id"]

	// 编辑草稿（仍存草稿）
	status, resp := jsonReq(t, r, "PUT", "/centralized-projects/draft", map[string]interface{}{
		"id": id, "project_name": "草稿A改名", "save_as_draft": true,
	})
	successOk(t, status, resp)
	if dataMap(t, resp)["status"] != "draft" || hits != 0 {
		t.Errorf("编辑草稿应仍为 draft 且不推送：status=%v hits=%d", dataMap(t, resp)["status"], hits)
	}

	// 从草稿发布
	status, resp = jsonReq(t, r, "PUT", "/centralized-projects/draft", map[string]interface{}{
		"id": id, "project_name": "草稿A改名", "owner_name": "张三",
		"department": "第一研究院", "sensitivity_level": "general", "save_as_draft": false,
	})
	successOk(t, status, resp)
	if dataMap(t, resp)["status"] != "approved" || hits != 1 {
		t.Errorf("从草稿发布应 approved 且推送一次：status=%v hits=%d", dataMap(t, resp)["status"], hits)
	}

	// 再次编辑（已是 approved，非草稿）应被拒
	status, resp = jsonReq(t, r, "PUT", "/centralized-projects/draft", map[string]interface{}{
		"id": id, "project_name": "x", "save_as_draft": true,
	})
	expectFailure(t, status, resp)
}

// 跨用户：u2 不能编辑 u1 的草稿。
func TestHTTP_CentralizedProjects_UpdateDraft_RejectsOtherUsersDraft(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// u1 建草稿
	withActiveUser(t, db, "u1")
	_, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "u1草稿", "save_as_draft": true,
	})
	id := resp["data"].(map[string]interface{})["id"]

	// 切换活跃用户为 u2，尝试编辑 u1 的草稿 → 应被拒
	withActiveUser(t, db, "u2")
	status, resp := jsonReq(t, r, "PUT", "/centralized-projects/draft", map[string]interface{}{
		"id": id, "project_name": "篡改", "save_as_draft": true,
	})
	expectFailure(t, status, resp)

	// u1 的草稿内容未被篡改
	var name string
	_ = db.Get(&name, `SELECT project_name FROM centralized_project_applications WHERE id=?`, id)
	if name != "u1草稿" {
		t.Errorf("u1 草稿被 u2 篡改：name=%s", name)
	}
}

// 列表应返回 project_code/department/approval_basis/description，供草稿回填。
func TestHTTP_CentralizedProjects_ListReturnsNewFields(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	_, _ = jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "草稿B", "project_code": "PC-9",
		"approval_basis": "依据X", "description": "简介Y", "save_as_draft": true,
	})

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects")
	successOk(t, status, resp)
	items, _ := dataMap(t, resp)["items"].([]interface{})
	first := items[0].(map[string]interface{})
	if first["project_code"] != "PC-9" || first["approval_basis"] != "依据X" || first["description"] != "简介Y" {
		t.Errorf("列表缺新字段: %+v", first)
	}
	if first["status"] != "draft" {
		t.Errorf("status=%v want draft", first["status"])
	}
}

// 发布：status=approved，推 manage（mock 命中 1 次），新字段落库可在列表读到。
func TestHTTP_CentralizedProjects_PublishWithNewFields(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredUser(t, db, "张三")

	hits := 0
	var pushed map[string]interface{}
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/api/centralized-projects/submit" {
			hits++
			_ = json.NewDecoder(req.Body).Decode(&pushed)
			_, _ = w.Write([]byte(`{"code":0,"data":{"id":777}}`))
			return
		}
		// /api/auth-users/list — syncUsersFromManage 拉用户，返回空列表
		_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name":      "正式立项",
		"project_code":      "PRJ-001",
		"owner_name":        "张三",
		"department":        "第一研究院",
		"sensitivity_level": "important",
		"data_owner":        "甲方",
		"approval_basis":    "上级批文",
		"description":       "项目简介内容",
		"save_as_draft":     false,
	})
	successOk(t, status, resp)
	if dataMap(t, resp)["status"] != "approved" {
		t.Errorf("status=%v want approved", dataMap(t, resp)["status"])
	}
	if hits != 1 {
		t.Errorf("发布应推送 manage 一次，hits=%d", hits)
	}
	// 完整立项书三字段须随 submit 推送给 manage（供分工页负责人查看）
	if pushed["department"] != "第一研究院" {
		t.Errorf("推送 payload 缺 department：%v", pushed["department"])
	}
	if pushed["approval_basis"] != "上级批文" {
		t.Errorf("推送 payload 缺 approval_basis：%v", pushed["approval_basis"])
	}
	if pushed["description"] != "项目简介内容" {
		t.Errorf("推送 payload 缺 description：%v", pushed["description"])
	}
}
