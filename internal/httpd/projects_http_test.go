package httpd

import (
	"data-asset-scan-go/internal/repository"
	"fmt"
	"strings"
	"testing"
)

// V1验证-1.10 立项接口快乐路径：模版 + 三主体 + member with close → 创建成功
func TestHTTP_Projects_CreateHappyPath(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	tplCode, tplVer := seedTestTemplateForHTTP(t, db)
	owner, custodian, security := seedTestSubjectsForHTTP(t, db)
	withProjectRoot(t, db)

	body := map[string]interface{}{
		"template_code":        tplCode,
		"template_version":     tplVer,
		"project_name":         "HTTP 测试立项",
		"object_short_code":    "HT-NSXS",
		"sensitivity_level":    "important",
		"owner_subject_id":     owner,
		"custodian_subject_id": custodian,
		"security_subject_id":  security,
		"activate":             true,
		"members": []map[string]interface{}{
			{"subject_id": owner, "role_code": "PM", "permission_actions": []string{"read", "write", "submit", "close"}},
		},
	}
	status, resp := jsonReq(t, r, "POST", "/projects", body)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	pj, _ := d["project"].(map[string]interface{})
	if pj == nil {
		t.Fatalf("project missing in response: %+v", d)
	}
	pcode, _ := pj["project_code"].(string)
	if !strings.Contains(pcode, "HT-NSXS-") {
		t.Errorf("project_code should contain HT-NSXS prefix, got %s", pcode)
	}
}

// V1验证-1.11 缺 close 权限成员 → 拒绝
func TestHTTP_Projects_RequireCloseAuthority(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	tplCode, tplVer := seedTestTemplateForHTTP(t, db)
	owner, custodian, security := seedTestSubjectsForHTTP(t, db)
	withProjectRoot(t, db)

	body := map[string]interface{}{
		"template_code":        tplCode,
		"template_version":     tplVer,
		"project_name":         "无 close 测试",
		"object_short_code":    "HT-NC",
		"sensitivity_level":    "important",
		"owner_subject_id":     owner,
		"custodian_subject_id": custodian,
		"security_subject_id":  security,
		"members": []map[string]interface{}{
			{"subject_id": owner, "role_code": "ED", "permission_actions": []string{"read", "write"}},
		},
	}
	status, resp := jsonReq(t, r, "POST", "/projects", body)
	expectFailure(t, status, resp)
	if errMsg, _ := resp["error"].(string); !strings.Contains(errMsg, "close") {
		t.Errorf("error should mention 'close', got %s", errMsg)
	}
}

// V1验证-1.12 模版未找到 → 拒绝
func TestHTTP_Projects_TemplateNotFound(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	owner, custodian, security := seedTestSubjectsForHTTP(t, db)
	withProjectRoot(t, db)

	status, resp := jsonReq(t, r, "POST", "/projects", map[string]interface{}{
		"template_code":        "NOT-EXIST",
		"template_version":     "V1.0",
		"project_name":         "x",
		"object_short_code":    "X",
		"sensitivity_level":    "important",
		"owner_subject_id":     owner,
		"custodian_subject_id": custodian,
		"security_subject_id":  security,
		"members": []map[string]interface{}{
			{"subject_id": owner, "permission_actions": []string{"close"}},
		},
	})
	expectFailure(t, status, resp)
}

// V1验证-1.13 立项+列表+详情+stages/file-versions/members 端点
func TestHTTP_Projects_ListAndSubresources(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, _ := seedTemplateAndProject(t, db)
	userID := seedUserAndProjectMember(t, db, "designer", pj.ID, []string{"read", "write"})
	if _, err := db.Exec(`UPDATE users SET display_name = '李四', department = '设计部', company_name = '第一研究院' WHERE id = ?`, userID); err != nil {
		t.Fatalf("update seeded user: %v", err)
	}

	// 列表
	status, resp := jsonReqNoBody(t, r, "GET", "/projects")
	successOk(t, status, resp)
	if len(dataList(t, resp)) != 1 {
		t.Errorf("expect 1 project")
	}

	// 详情
	status, resp = jsonReqNoBody(t, r, "GET", "/projects/1")
	successOk(t, status, resp)

	// stages
	status, resp = jsonReqNoBody(t, r, "GET", "/projects/1/stages")
	successOk(t, status, resp)
	if len(dataList(t, resp)) == 0 {
		t.Error("project should have stages")
	}

	// file-versions
	status, resp = jsonReqNoBody(t, r, "GET", "/projects/1/file-versions")
	successOk(t, status, resp)
	if len(dataList(t, resp)) == 0 {
		t.Error("project should have planned file_versions")
	}

	// members
	status, resp = jsonReqNoBody(t, r, "GET", "/projects/1/members")
	successOk(t, status, resp)
	members := dataList(t, resp)
	if len(members) == 0 {
		t.Fatal("project should have members")
	}
	var userMember map[string]interface{}
	for _, item := range members {
		m := item.(map[string]interface{})
		if m["user_id"] == float64(userID) {
			userMember = m
			break
		}
	}
	if userMember == nil {
		t.Fatalf("expected user member user_id=%d in %+v", userID, members)
	}
	if userMember["user_display_name"] != "李四" || userMember["user_department"] != "设计部" {
		t.Fatalf("member should include joined user display fields, got %+v", userMember)
	}

	// ledgers
	status, resp = jsonReqNoBody(t, r, "GET", "/projects/1/ledgers")
	successOk(t, status, resp)

	// events 一开始可能为空（仅 instantiate 没写事件）
	status, resp = jsonReqNoBody(t, r, "GET", "/projects/1/events")
	successOk(t, status, resp)

	// keyword 筛选
	status, resp = jsonReqNoBody(t, r, "GET", "/projects?keyword="+pj.ProjectCode[:4])
	successOk(t, status, resp)
}

// V1验证-1.14 close precheck 在必填未传时返回 error issues
func TestHTTP_Projects_ClosePrecheckRequiredMissing(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedTemplateAndProject(t, db)

	status, resp := jsonReqNoBody(t, r, "GET", "/projects/1/close/precheck")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if ok, _ := d["ok"].(bool); ok {
		t.Error("precheck should NOT be ok when required files are still planned")
	}
	issues, _ := d["issues"].([]interface{})
	if len(issues) == 0 {
		t.Error("expected at least one error issue")
	}
}

// V1验证-1.15 必填未传时 close 直接失败
func TestHTTP_Projects_CloseBeforeUploadShouldFail(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1") // 已注册为 subject，权限校验通过

	status, resp := jsonReq(t, r, "POST", "/projects/1/close", map[string]interface{}{
		"reason": "测试", "force": true,
	})
	expectFailure(t, status, resp)
}

// V5-P1 Task 5 GET /projects/:id/stages-with-rules 返回 stages 嵌入 rules
func TestHTTP_Projects_StagesWithRules(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	seedPersonalProjectsForAI(t, db)
	if err := repository.EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	withActiveUser(t, db, "u1")

	var projID int64
	db.Get(&projID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalGeneralProjectCode)

	status, resp := jsonReqNoBody(t, r, "GET", fmt.Sprintf("/projects/%d/stages-with-rules", projID))
	successOk(t, status, resp)
	d := dataMap(t, resp)
	stages, ok := d["stages"].([]interface{})
	if !ok || len(stages) == 0 {
		t.Fatalf("expected stages in response, got %+v", d)
	}
	// 每个 stage 必须有 rules 数组
	for _, s := range stages {
		m := s.(map[string]interface{})
		if _, ok := m["stage_code"]; !ok {
			t.Errorf("stage missing stage_code")
		}
		rules, ok := m["rules"].([]interface{})
		if !ok {
			t.Errorf("stage missing rules array")
		}
		if len(rules) > 0 {
			r0 := rules[0].(map[string]interface{})
			if _, ok := r0["file_rule_code"]; !ok {
				t.Errorf("rule missing file_rule_code")
			}
			if _, ok := r0["data_state"]; !ok {
				t.Errorf("rule missing data_state")
			}
		}
	}

	// Assert specific V2.0 stage codes appear (regression guard)
	codes := map[string]bool{}
	for _, s := range stages {
		m := s.(map[string]interface{})
		codes[m["stage_code"].(string)] = true
	}
	// seedPersonalProjectsForAI seeds a TPL-PERSONAL-FILES template (V1-style single GR-DA stage),
	// so the personal-general project has 1 stage: GR-DA.
	if !codes["GR-DA"] {
		t.Errorf("expected stage GR-DA in stages, got %v", codes)
	}
}

// V1验证-1.16 一个完整快乐路径：上传所有 required → close → manifest 生成
func TestHTTP_Projects_CloseHappyPath(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	// 通过 service 把 required 都上传，避免重写 multipart upload 的逻辑
	svc := repository.NewFileOperationService(db)
	for _, s := range stages {
		for _, fv := range s.FileVersions {
			if fv.Required != 1 {
				continue
			}
			// 需要知道 allowed file types — 找规则
			var ext string
			err := db.Get(&ext, `SELECT lower(json_extract(allowed_file_types, '$[0]')) FROM template_file_rules WHERE id = ?`, *fv.TemplateFileRuleID)
			if err != nil || ext == "" {
				ext = "pdf"
			}
			src := dummyFile(t, "x."+ext, "x")
			if _, err := svc.UploadOrBind(fv.ID, repository.UploadInput{
				SourcePath: src, OriginalFileName: "x." + ext, OperatorID: "OWNER-1",
			}); err != nil {
				t.Fatalf("upload required %s: %v", fv.LocalCode, err)
			}
		}
	}

	// 现在 close 应该成功（仍可能 warning，需要 force）
	status, resp := jsonReq(t, r, "POST", "/projects/1/close", map[string]interface{}{
		"reason": "测试结项", "force": true,
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["manifest_path"] == nil {
		t.Error("response should contain manifest_path")
	}
	if d["manifest_sha256"] == nil {
		t.Error("response should contain manifest_sha256")
	}

	// 项目状态应为 archived
	_, p := jsonReqNoBody(t, r, "GET", "/projects/1")
	if pp := dataMap(t, p); pp["status"] != "archived" {
		t.Errorf("project status should be archived, got %v", pp["status"])
	}
}
