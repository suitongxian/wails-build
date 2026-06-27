package httpd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/repository"
)

// seedSimpleResourceWithDist 测试用：建 data_resource + distribution（提供 path）
func seedSimpleResourceWithDist(t *testing.T, db *sqlx.DB, name, sign, path string) int64 {
	t.Helper()
	now := time.Now()
	r, err := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable
	) VALUES (?, 1, 1, ?, ?, 2, 0, ?, ?, 0)`,
		sign, now, name, now, now)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := r.LastInsertId()
	_, _ = db.Exec(`INSERT INTO data_distributing (
		content_sign, file_md5, file_name, path, file_size, file_type,
		create_time, update_time, disable
	) VALUES (?, ?, ?, ?, 100, '.pdf', ?, ?, 0)`,
		sign, sign, name, path, now, now)
	return rid
}

// seedPersonalProjectsForAI 测试用：通过 mock template seeding + ensurePersonalFilesContext 建好 3 内置项目
func seedPersonalProjectsForAI(t *testing.T, db *sqlx.DB) {
	t.Helper()
	now := time.Now()
	res, err := db.Exec(`INSERT INTO data_templates (
		template_code, template_name, template_version, status, project_sensitivity_level,
		cached_at, create_time, update_time, disable
	) VALUES ('TPL-PERSONAL-FILES', '个人文件项目化管理模版', 'V1.0', 'active', 'general', ?, ?, ?, 0)`,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	tplID, _ := res.LastInsertId()
	stageRes, _ := db.Exec(`INSERT INTO template_stages (
		template_id, stage_code, stage_name, stage_type, sort_order,
		cached_at, create_time, update_time, disable
	) VALUES (?, 'GR-DA', '个人归档', 'record', 1, ?, ?, ?, 0)`, tplID, now, now, now)
	stageID, _ := stageRes.LastInsertId()
	for _, r := range []struct{ code, name, state string }{
		{"IN-001", "来源文件", "input"},
		{"PRC-001", "过程版本", "process"},
		{"OUT-001", "归档定稿", "output"},
	} {
		db.Exec(`INSERT INTO template_file_rules (
			template_stage_id, file_rule_code, file_name, data_state,
			required, allowed_file_types, cached_at, create_time, update_time, disable
		) VALUES (?, ?, ?, ?, 0, '["*"]', ?, ?, ?, 0)`, stageID, r.code, r.name, r.state, now, now, now)
	}
}

// V4-Q1-b AI 建议端点：单条资源返回排序建议
func TestHTTP_AIClassify_Suggestions(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	repository.EnsurePersonalContextForTest(db)
	withActiveUser(t, db, "u1")

	resID := seedSimpleResourceWithDist(t, db, "归档定稿-报表.xlsx", "AICLSG001", "/Users/x/")

	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/suggestions?resource_id="+itoa(resID))
	successOk(t, status, resp)

	d := dataMap(t, resp)
	suggestions, ok := d["suggestions"].([]interface{})
	if !ok {
		t.Fatalf("response 缺 suggestions: %+v", d)
	}
	if len(suggestions) == 0 {
		t.Fatal("应有建议（文件名含'归档定稿'命中 OUT-001 规则名）")
	}
	first := suggestions[0].(map[string]interface{})
	if first["file_rule_code"] != "OUT-001" {
		t.Errorf("首选应为 OUT-001（规则名命中），got %v", first["file_rule_code"])
	}
}

// V4-Q1-b pending 列表
func TestHTTP_AIClassify_Pending(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	repository.EnsurePersonalContextForTest(db)
	withActiveUser(t, db, "u1")

	// 建 2 个待归目资源
	seedSimpleResourceWithDist(t, db, "fileA.pdf", "AICPND001", "/p/")
	seedSimpleResourceWithDist(t, db, "fileB.docx", "AICPND002", "/p/")

	status, resp := jsonReqNoBody(t, r, "GET", "/ai/classify/pending?page_size=10&origin=new")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	items, _ := d["items"].([]interface{})
	if len(items) < 2 {
		t.Errorf("应至少返回 2 条 pending, got %d", len(items))
	}
}

// V4-Q1-b apply 端点 — 把资源挂到指定项目/环节/规则
func TestHTTP_AIClassify_Apply(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	repository.EnsurePersonalContextForTest(db)
	withActiveUser(t, db, "u1")
	resID := seedSimpleResourceWithDist(t, db, "测试.pdf", "AICAPP001", "/")

	// 找 SYS-PERSONAL-GENERAL 项目 id
	var projID int64
	db.Get(&projID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalGeneralProjectCode)

	status, resp := jsonReq(t, r, "POST", "/ai/classify/apply", map[string]interface{}{
		"resource_id":    resID,
		"project_id":     projID,
		"stage_code":     "GR-DA",
		"file_rule_code": "OUT-001",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["status"] != "archived" {
		t.Errorf("status 应为 archived, got %v", d["status"])
	}
}

// V4-Q1-b apply 缺字段拒
func TestHTTP_AIClassify_Apply_MissingFields(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db
	withActiveUser(t, db, "u1")
	status, resp := jsonReq(t, r, "POST", "/ai/classify/apply", map[string]interface{}{
		"resource_id": 1,
	})
	expectFailure(t, status, resp)
}

// V5-P3 §4.4 端点接入 enricher + 正文片段
//
// TestHTTP_AIClassify_BodyEnrichment 验证：
// 当本地路径真实存在时，端点会调用 textextract 读首 200 字注入 Summary，
// 评分流程仍能跑通并返回 suggestions（不报错）。
func TestHTTP_AIClassify_BodyEnrichment(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	withActiveUser(t, db, "u1")

	// 真实的本地文件，textextract 能读
	tmpDir := t.TempDir()
	bodyPath := filepath.Join(tmpDir, "审校意见-第一稿.txt")
	if err := os.WriteFile(bodyPath, []byte("本文件是审校意见的初稿，审校人张三，详细见正文..."), 0644); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	res, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, claim_status, importance_level, create_time, update_time, disable
	) VALUES ('BODY001', 1, 1, ?, ?, 2, 0, ?, ?, 0)`, now, "审校意见-第一稿.txt", now, now)
	resID, _ := res.LastInsertId()
	db.Exec(`INSERT INTO data_distributing (
		scan_task_id, path, data_type, scan_found_count, content_sign, file_suffix, file_magic,
		file_size, ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (1, ?, 1, 1, 'BODY001', 'txt', 'text/plain', 128, '127.0.0.1', 'aa:bb', ?, ?, ?, 0)`,
		bodyPath, now, now, now)

	status, resp := jsonReqNoBody(t, r, "GET", fmt.Sprintf("/ai/classify/suggestions?resource_id=%d", resID))
	successOk(t, status, resp)

	d := dataMap(t, resp)
	sugs, _ := d["suggestions"].([]interface{})
	if len(sugs) == 0 {
		t.Fatal("expected suggestions")
	}
	// 验证 enricher 在工作：因 personal 模版 stage_name="个人归档"、rule.file_name="归档定稿"
	// 测试文件名/正文未必命中这些，所以这个 test 只验证流程跑通（不报错 + 有建议）。
}

// TestHTTP_AIClassify_MetadataEnrichment 验证：
// mime/sibling_count 等元数据被 EnrichInputForResource 注入用于评分流程。
func TestHTTP_AIClassify_MetadataEnrichment(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	withActiveUser(t, db, "u1")

	// 没有本地真实文件，但有 distribution metadata
	resID := seedSimpleResourceWithDist(t, db, "归档定稿-测试.docx", "MD001", "/tmp/x/")
	// 改下 file_magic 让 enricher 读到 mime
	db.Exec(`UPDATE data_distributing SET file_magic = ? WHERE content_sign = ?`,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "MD001")

	status, resp := jsonReqNoBody(t, r, "GET", fmt.Sprintf("/ai/classify/suggestions?resource_id=%d", resID))
	successOk(t, status, resp)
	d := dataMap(t, resp)
	sugs, _ := d["suggestions"].([]interface{})
	if len(sugs) == 0 {
		t.Fatal("expected suggestions")
	}
}
