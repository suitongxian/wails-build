package httpd

import (
	"data-asset-scan-go/internal/models"
	"data-asset-scan-go/internal/repository"
	"encoding/json"
	"net/http"
	"testing"
)

// findFvIDByLocal 在指定 stage 找 local_code 对应的 fv id
func findFvIDByLocal(t *testing.T, stages []repository.FullStageInstance, stageCode, localCode string) int64 {
	t.Helper()
	for _, s := range stages {
		if s.StageCode != stageCode {
			continue
		}
		for _, fv := range s.FileVersions {
			if fv.LocalCode == localCode {
				return fv.ID
			}
		}
	}
	t.Fatalf("fv not found: %s/%s", stageCode, localCode)
	return 0
}

func stageIDByCodeForHTTP(t *testing.T, stages []repository.FullStageInstance, code string) int64 {
	t.Helper()
	for _, s := range stages {
		if s.StageCode == code {
			return s.ID
		}
	}
	t.Fatalf("stage not found: %s", code)
	return 0
}

func ptrStringForHTTP(s string) *string {
	return &s
}

// V1验证-1.20 上传 multipart → 文件版本切换 registered + 底账 registered
func TestHTTP_FileVersions_UploadMultipart(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1") // 走严格鉴权 + 有 write 权限

	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")

	status, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "客户原稿.pdf", []byte("manuscript-bytes"), nil)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	fv, _ := d["file_version"].(map[string]interface{})
	if fv["lifecycle_status"] != "registered" {
		t.Errorf("after upload, lifecycle_status should be registered, got %v", fv["lifecycle_status"])
	}
	if fv["checksum"] == nil {
		t.Error("checksum should be set")
	}
	ledger, _ := d["ledger"].(map[string]interface{})
	if ledger["lifecycle_status"] != "registered" {
		t.Errorf("after upload, ledger should be registered, got %v", ledger["lifecycle_status"])
	}
}

func TestHTTP_FileVersions_UploadUsesLoggedInSessionForPermission(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, stages := seedTemplateAndProject(t, db)

	userRepo := repository.NewUserRepository(db)
	bob, err := userRepo.Create(models.CreateUserInput{
		Username:    "bob",
		DisplayName: "无权限用户",
		CompanyName: "第一研究院",
		Department:  "档案处",
	})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	aliceID := seedUserAndProjectMember(t, db, "alice", pj.ID, []string{"write"})

	currentAuthSession.Lock()
	currentAuthSession.session = &authSession{
		Token: "token-bob",
		User: authUser{
			ID:             bob.ID,
			Username:       "bob",
			DisplayName:    "无权限用户",
			UserUnit:       "第一研究院",
			UserDepartment: "档案处",
			Status:         "active",
		},
	}
	currentAuthSession.Unlock()
	defer func() {
		currentAuthSession.Lock()
		currentAuthSession.session = nil
		currentAuthSession.Unlock()
	}()

	if bob.ID >= aliceID {
		t.Fatalf("test setup requires active-user fallback to pick alice, got bob=%d alice=%d", bob.ID, aliceID)
	}

	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")
	status, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "客户原稿.pdf", []byte("manuscript-bytes"), nil)

	if status != http.StatusForbidden || resp["code"] != "PERMISSION_DENIED" {
		t.Fatalf("upload should use logged-in bob and be denied, status=%d resp=%+v", status, resp)
	}
}

func TestHTTP_FileVersions_UploadRequiresPermissionInFileVersionStage(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, stages := seedTemplateAndProject(t, db)

	userRepo := repository.NewUserRepository(db)
	lisi, err := userRepo.Create(models.CreateUserInput{
		Username:    "lisi",
		DisplayName: "李四",
		CompanyName: "第一研究院",
		Department:  "排版部",
	})
	if err != nil {
		t.Fatalf("create lisi: %v", err)
	}

	pbStageID := stageIDByCodeForHTTP(t, stages, "MZ-PB")
	stageIDs, _ := json.Marshal([]int64{pbStageID})
	memberRepo := repository.NewProjectMemberRepository(db)
	tx := db.MustBegin()
	if _, err := memberRepo.Insert(tx, repository.CreateProjectMemberInput{
		ProjectID:         pj.ID,
		UserID:            &lisi.ID,
		RoleCode:          "排版员",
		StageIDs:          ptrStringForHTTP(string(stageIDs)),
		PermissionActions: `["write"]`,
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert lisi member: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit member: %v", err)
	}

	currentAuthSession.Lock()
	currentAuthSession.session = &authSession{
		Token: "token-lisi",
		User: authUser{
			ID:             lisi.ID,
			Username:       "lisi",
			DisplayName:    "李四",
			UserUnit:       "第一研究院",
			UserDepartment: "排版部",
			Status:         "active",
		},
	}
	currentAuthSession.Unlock()
	defer func() {
		currentAuthSession.Lock()
		currentAuthSession.session = nil
		currentAuthSession.Unlock()
	}()

	sgFvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")
	status, resp := uploadReq(t, r, "/file-versions/"+itoa(sgFvID)+"/upload",
		"file", "客户原稿.pdf", []byte("manuscript-bytes"), nil)

	if status != http.StatusForbidden || resp["code"] != "PERMISSION_DENIED" {
		t.Fatalf("upload should require write permission in MZ-SG stage, status=%d resp=%+v", status, resp)
	}
}

// V1验证-1.21 文件类型不匹配 → 失败
func TestHTTP_FileVersions_UploadWrongType(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001") // IN-001 只允许 PDF

	status, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "wrong.docx", []byte("docx"), nil)
	expectFailure(t, status, resp)
}

// V1验证-1.22 已 registered 不可再上传
func TestHTTP_FileVersions_DuplicateUploadRejected(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")

	// 第一次：成功
	uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)

	// 第二次：失败
	status, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("y"), nil)
	expectFailure(t, status, resp)
}

// V1验证-1.23 输入文件创建新版本应被拒绝（输入只读）
func TestHTTP_FileVersions_NewVersionOnInputRejected(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")

	// 先上传一份
	uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)

	// 再 new-version → 应当因为输入只读而失败
	status, resp := uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/new-version",
		"file", "case.pdf", []byte("y"), nil)
	expectFailure(t, status, resp)
}

// V1验证-1.24 file-version 详情 / events / chain / ledger
func TestHTTP_FileVersions_DetailEndpoints(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	fvID := findFvIDByLocal(t, stages, "MZ-SG", "IN-001")
	uploadReq(t, r, "/file-versions/"+itoa(fvID)+"/upload",
		"file", "case.pdf", []byte("x"), nil)

	// 详情
	status, resp := jsonReqNoBody(t, r, "GET", "/file-versions/"+itoa(fvID))
	successOk(t, status, resp)

	// 事件流 — 上传后至少 1 条 register 事件
	status, resp = jsonReqNoBody(t, r, "GET", "/file-versions/"+itoa(fvID)+"/events")
	successOk(t, status, resp)
	if len(dataList(t, resp)) == 0 {
		t.Error("expected register event after upload")
	}

	// chain
	status, resp = jsonReqNoBody(t, r, "GET", "/file-versions/"+itoa(fvID)+"/chain")
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["current"] == nil {
		t.Error("chain should include current")
	}

	// ledger
	status, resp = jsonReqNoBody(t, r, "GET", "/file-versions/"+itoa(fvID)+"/ledger")
	successOk(t, status, resp)
}

// V1验证-1.25 鉴权：项目内没人有 receive → POST receive 403
func TestHTTP_FileVersions_ReceiveDeniedWithoutPermission(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	// 立项时只给 read+write（不给 receive）
	tplCode, tplVer := seedTestTemplateForHTTP(t, db)
	owner, custodian, security := seedTestSubjectsForHTTP(t, db)
	withProjectRoot(t, db)

	svc := repository.NewProjectInstantiationService(db)
	out, err := svc.Instantiate(repository.InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "限权测试",
		ObjectShortCode:    "HT-LIMIT",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []repository.MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "submit", "close"}}, // 故意不给 receive
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	withActiveUser(t, db, "OWNER-1") // 严格匹配到 owner

	// 找上游产出走完 upload + submit，然后下游领取应当 403
	prcFvID := findFvIDByLocal(t, out.Stages, "MZ-PB", "PRC-001")
	_ = prcFvID
	outFvID := findFvIDByLocal(t, out.Stages, "MZ-PB", "OUT-001")

	uploadReq(t, r, "/file-versions/"+itoa(outFvID)+"/upload",
		"file", "out.pdf", []byte("x"), nil)

	// submit
	status, resp := jsonReqNoBody(t, r, "POST", "/file-versions/"+itoa(outFvID)+"/submit")
	successOk(t, status, resp)

	// 下游领取应该 403
	w := httpPost(t, r, "/file-versions/"+itoa(outFvID)+"/receive", map[string]interface{}{
		"target_stage_id":  stageIDByName(t, out.Stages, "MZ-SH"),
		"target_rule_code": "IN-001",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// V1验证-1.26 鉴权放行：把 receive 加上后同样的 receive 应当成功
func TestHTTP_FileVersions_ReceiveAllowedWithPermission(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()

	_, stages := seedTemplateAndProject(t, db) // 默认成员有 receive 权限
	withActiveUser(t, db, "OWNER-1")

	outFvID := findFvIDByLocal(t, stages, "MZ-PB", "OUT-001")
	uploadReq(t, r, "/file-versions/"+itoa(outFvID)+"/upload",
		"file", "out.pdf", []byte("x"), nil)

	// submit
	status, resp := jsonReqNoBody(t, r, "POST", "/file-versions/"+itoa(outFvID)+"/submit")
	successOk(t, status, resp)

	// 领取
	status, resp = jsonReq(t, r, "POST", "/file-versions/"+itoa(outFvID)+"/receive", map[string]interface{}{
		"target_stage_id":  stageIDByName(t, stages, "MZ-SH"),
		"target_rule_code": "IN-001",
	})
	successOk(t, status, resp)
}

func TestHTTP_FileVersions_ReceiveChecksTargetStagePermission(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, stages := seedTemplateAndProject(t, db)

	outFvID := findFvIDByLocal(t, stages, "MZ-PB", "OUT-001")
	uploadReq(t, r, "/file-versions/"+itoa(outFvID)+"/upload",
		"file", "out.pdf", []byte("x"), nil)
	status, resp := jsonReqNoBody(t, r, "POST", "/file-versions/"+itoa(outFvID)+"/submit")
	successOk(t, status, resp)

	userRepo := repository.NewUserRepository(db)
	lisi, err := userRepo.Create(models.CreateUserInput{
		Username:    "lisi",
		DisplayName: "李四",
		CompanyName: "第一研究院",
		Department:  "审核部",
	})
	if err != nil {
		t.Fatalf("create lisi: %v", err)
	}

	targetStageID := stageIDByCodeForHTTP(t, stages, "MZ-SH")
	stageIDs, _ := json.Marshal([]int64{targetStageID})
	memberRepo := repository.NewProjectMemberRepository(db)
	tx := db.MustBegin()
	if _, err := memberRepo.Insert(tx, repository.CreateProjectMemberInput{
		ProjectID:         pj.ID,
		UserID:            &lisi.ID,
		RoleCode:          "审核员",
		StageIDs:          ptrStringForHTTP(string(stageIDs)),
		PermissionActions: `["receive"]`,
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert lisi member: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit member: %v", err)
	}

	currentAuthSession.Lock()
	currentAuthSession.session = &authSession{
		Token: "token-lisi",
		User: authUser{
			ID:             lisi.ID,
			Username:       "lisi",
			DisplayName:    "李四",
			UserUnit:       "第一研究院",
			UserDepartment: "审核部",
			Status:         "active",
		},
	}
	currentAuthSession.Unlock()
	defer func() {
		currentAuthSession.Lock()
		currentAuthSession.session = nil
		currentAuthSession.Unlock()
	}()

	status, resp = jsonReq(t, r, "POST", "/file-versions/"+itoa(outFvID)+"/receive", map[string]interface{}{
		"target_stage_id":  targetStageID,
		"target_rule_code": "IN-001",
	})
	successOk(t, status, resp)
}

// V1验证-1.27 submittable 列出已提交的产出
func TestHTTP_FileVersions_Submittable(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, stages := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	outFvID := findFvIDByLocal(t, stages, "MZ-PB", "OUT-001")
	uploadReq(t, r, "/file-versions/"+itoa(outFvID)+"/upload",
		"file", "out.pdf", []byte("x"), nil)
	jsonReqNoBody(t, r, "POST", "/file-versions/"+itoa(outFvID)+"/submit")

	status, resp := jsonReqNoBody(t, r, "GET", "/file-versions/submittable?project_id="+itoa(pj.ID))
	successOk(t, status, resp)
	if len(dataList(t, resp)) != 1 {
		t.Errorf("expected 1 submittable, got %d", len(dataList(t, resp)))
	}
}
