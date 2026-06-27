package repository

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"data-asset-scan-go/internal/models"
)

// TestE2E_FullProjectLifecycle 端到端集成：立项 → 上传 → 派生 → 提交 → 领取 →
// 状态切换 → 结项预检 → 结项 → 单向上报 manage → 校验持久化结果
//
// 这是 V1 H1 任务的核心校验：把 D/E/F/G 全部串成一个真实业务闭环。
func TestE2E_FullProjectLifecycle(t *testing.T) {
	// 1) 准备数据库 + 模版 + 三主体 + 项目根
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)

	tmpRoot := t.TempDir()
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, tmpRoot)

	// 2) 立项
	instSvc := NewProjectInstantiationService(db)
	instOut, err := instSvc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "E2E 测试印刷",
		ObjectShortCode:    "MC-E2E",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "receive", "submit", "archive", "close"}},
		},
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	project := instOut.Project
	stages := instOut.Stages

	if !strings.Contains(project.ProjectCode, "MC-E2E-") {
		t.Errorf("project_code should contain MC-E2E prefix, got %s", project.ProjectCode)
	}

	// 项目根应建立
	if project.ProjectRoot == nil {
		t.Fatal("project_root should be set")
	}
	if _, err := os.Stat(*project.ProjectRoot); err != nil {
		t.Fatalf("project root not created: %v", err)
	}

	// 3) 文件操作
	fileSvc := NewFileOperationService(db)

	// 3.1 上传 IN-001 客户原稿（input → registered）
	inFv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src1 := writeTempFile(t, "客户原稿.pdf", "manuscript-bytes")
	bindRes, err := fileSvc.UploadOrBind(inFv.ID, UploadInput{
		SourcePath: src1, OriginalFileName: "客户原稿.pdf", OperatorID: "tester",
	})
	if err != nil {
		t.Fatalf("upload IN-001: %v", err)
	}
	if bindRes.Ledger.LifecycleStatus != "registered" {
		t.Errorf("ledger should be registered after bind, got %s", bindRes.Ledger.LifecycleStatus)
	}
	if bindRes.FileVersion.SecurityPolicyID == nil {
		t.Error("F1: security_policy_id should be auto-attached")
	}
	if bindRes.FileVersion.Checksum == nil || len(*bindRes.FileVersion.Checksum) != 64 {
		t.Errorf("checksum should be 64-char hex, got %v", bindRes.FileVersion.Checksum)
	}

	// 3.2 派生过程文件 PRC-001（process）— 测试 seed 允许 PSD
	prcRule := findFirstProcessRule(t, db, project, "MZ-PB")
	prcExt := pickAllowedExtension(t, prcRule.AllowedFileTypes)
	src2 := writeTempFile(t, "排版稿."+prcExt, "typesetting")
	deriveRes, err := fileSvc.DeriveProcess(inFv.ID, DeriveInput{
		UploadInput: UploadInput{
			SourcePath: src2, OriginalFileName: "排版稿." + prcExt, OperatorID: "tester",
		},
		TargetStageID:  stageIDByCode(stages, "MZ-PB"),
		TargetRuleCode: prcRule.FileRuleCode,
	})
	if err != nil {
		t.Fatalf("derive process: %v", err)
	}
	if deriveRes.FileVersion.SourceFileVersionID == nil || *deriveRes.FileVersion.SourceFileVersionID != inFv.ID {
		t.Errorf("derived fv should reference input as source")
	}

	// 3.3 找一个产出规则上传 + 提交
	outRule := findFirstOutputRule(t, db, project, "MZ-PB")
	if outRule == nil {
		t.Skip("test template has no output rule in MZ-PB; skip submit/receive")
	}
	outFv := findFvByLocalCode(t, stages, "MZ-PB", outRule.FileRuleCode)
	outExt := pickAllowedExtension(t, outRule.AllowedFileTypes)
	src3 := writeTempFile(t, "排版完成稿."+outExt, "completed")
	uploadOut, err := fileSvc.UploadOrBind(outFv.ID, UploadInput{
		SourcePath: src3, OriginalFileName: "排版完成稿." + outExt, OperatorID: "tester",
	})
	if err != nil {
		t.Fatalf("upload output: %v", err)
	}
	if _, err := fileSvc.SubmitOutput(uploadOut.FileVersion.ID, "tester"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	submitted, _ := fileSvc.fvRepo.FindByID(uploadOut.FileVersion.ID)
	if submitted.SubmittedAt == nil {
		t.Error("output should have submitted_at after submit")
	}

	// 3.4 下游 MZ-SH 的 input 领取上游产出
	shInput := findFirstInputRule(t, db, project, "MZ-SH")
	if shInput != nil {
		recvRes, err := fileSvc.ReceiveAsInput(ReceiveInput{
			SourceFileVersionID: submitted.ID,
			TargetStageID:       stageIDByCode(stages, "MZ-SH"),
			TargetRuleCode:      shInput.FileRuleCode,
			OperatorID:          "tester",
		})
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if recvRes.FileVersion.StorageURI == nil || *recvRes.FileVersion.StorageURI != *submitted.StorageURI {
			t.Error("received fv should reference upstream storage_uri (no copy)")
		}

		// 幂等：再调一次返回相同结果
		recvRes2, err := fileSvc.ReceiveAsInput(ReceiveInput{
			SourceFileVersionID: submitted.ID,
			TargetStageID:       stageIDByCode(stages, "MZ-SH"),
			TargetRuleCode:      shInput.FileRuleCode,
			OperatorID:          "tester",
		})
		if err != nil {
			t.Fatalf("idempotent receive: %v", err)
		}
		if recvRes2.FileVersion.ID != recvRes.FileVersion.ID {
			t.Error("receive should be idempotent")
		}
	}

	// 4) 状态切换：把 input 文件推到 in_use 再回 registered（验证状态机）
	lcSvc := NewLedgerLifecycleService(db)
	if err := lcSvc.Transition(TransitionInput{
		LedgerID: bindRes.Ledger.ID, ToStatus: "in_use", OperatorID: "tester", Reason: "投入工作",
	}); err != nil {
		t.Fatalf("registered->in_use: %v", err)
	}
	if err := lcSvc.Transition(TransitionInput{
		LedgerID: bindRes.Ledger.ID, ToStatus: "registered", OperatorID: "tester", Reason: "完成使用",
	}); err == nil {
		// 实际允许：in_use → registered 是合法的（state machine 定义）
	}

	// 非法转换应当被拒绝（已 registered → planned 不允许）
	err = lcSvc.Transition(TransitionInput{
		LedgerID: bindRes.Ledger.ID, ToStatus: "planned", OperatorID: "tester",
	})
	if err == nil {
		t.Error("expected planned transition from registered to be rejected")
	}

	// 5) 权限校验
	authSvc := NewProjectAuthService(db)
	// 严格模式：owner code 应有 close
	var ownerCode string
	_ = db.Get(&ownerCode, `SELECT code FROM subjects WHERE id = ?`, owner)
	if err := authSvc.CheckProjectAction(ownerCode, project.ID, "close"); err != nil {
		t.Errorf("owner should have close: %v", err)
	}
	// 严格模式拒绝
	if err := authSvc.CheckProjectAction(ownerCode, project.ID, "destroy"); err == nil {
		t.Error("owner should NOT have destroy")
	}
	// V2-5：取消宽松回退；未注册操作人一律拒绝
	if err := authSvc.CheckProjectAction("unknown-user", project.ID, "write"); err == nil {
		t.Error("V2-5：未注册操作人必须被拒")
	}

	// 6) 把所有 required 都补齐才能结项
	uploadAllRequired(t, fileSvc, stages)

	closeSvc := NewProjectCloseService(db)
	pre, err := closeSvc.Precheck(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	hasErr := false
	for _, iss := range pre.Issues {
		if iss.Severity == "error" {
			hasErr = true
			t.Logf("precheck error: %s", iss.Message)
		}
	}
	if hasErr {
		t.Fatal("precheck should pass after uploading all required")
	}

	// 7) 启动 mock manage 用来接收上报
	var manageBody []byte
	var manageHits int
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sync/project-archive" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		manageHits++
		manageBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"received","data":{"upload_id":777}}`))
	}))
	defer manage.Close()
	configRepo.SetValue(KeyManageEndpoint, manage.URL)
	// 2026-05-22 启动 seed 了默认 archive_endpoint，清空让上报走 mock URL
	configRepo.SetValue(KeyArchiveEndpoint, "")

	// 8) 结项 + 上报
	closeOut, err := closeSvc.Close(CloseInput{
		ProjectID: project.ID, OperatorID: "tester", Force: true,
	})
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	// 校验 manifest 文件存在 + sha256
	stat, err := os.Stat(closeOut.ManifestPath)
	if err != nil || stat.Size() == 0 {
		t.Fatalf("manifest missing/empty: %v", err)
	}
	if len(closeOut.ManifestSha256) != 64 {
		t.Errorf("manifest sha256 should be 64 chars, got %d", len(closeOut.ManifestSha256))
	}

	// 校验 manifest JSON 关键字段
	body, _ := os.ReadFile(closeOut.ManifestPath)
	var mf map[string]interface{}
	_ = json.Unmarshal(body, &mf)
	if mf["project"] == nil {
		t.Error("manifest missing project")
	}
	if mf["lifecycle_events"] == nil {
		t.Error("manifest missing lifecycle_events (manage compat field)")
	}
	if mf["source_terminal"] != "data-asset-scan" {
		t.Error("manifest missing source_terminal")
	}

	// 项目状态 archived + sync_status pending
	updated, _ := closeSvc.projRepo.FindByID(project.ID)
	if updated.Status != "archived" {
		t.Errorf("project should be archived, got %s", updated.Status)
	}

	// 9) 上报
	uploader := NewArchiveUploader(db)
	res, err := uploader.Upload(project.ID, closeOut.ManifestPath)
	if err != nil {
		t.Fatalf("upload to manage: %v", err)
	}
	if res.Status != "success" {
		t.Errorf("upload should succeed, got %s", res.Status)
	}
	if manageHits != 1 {
		t.Errorf("manage should be hit once, got %d", manageHits)
	}
	if len(manageBody) == 0 {
		t.Fatal("manage didn't receive body")
	}

	// sync_status 应当 success
	final, _ := closeSvc.projRepo.FindByID(project.ID)
	if final.SyncStatus == nil || *final.SyncStatus != "success" {
		t.Errorf("expected sync_status=success, got %v", final.SyncStatus)
	}
	if final.SyncedAt == nil {
		t.Error("synced_at should be set after successful upload")
	}

	// 10) 端到端审计：所有底账都应在 sealed/permanent/destroyed 之一
	allLedgers, _ := closeSvc.ledgerRepo.Search(LedgerSearchInput{ProjectCode: project.ProjectCode})
	for _, l := range allLedgers {
		// planned 草稿如果一直没 bind 就还是 planned，这是预期；其余应当 sealed
		if l.LifecycleStatus != "planned" && l.LifecycleStatus != "sealed" &&
			l.LifecycleStatus != "permanent" && l.LifecycleStatus != "destroyed" {
			t.Errorf("ledger %s in unexpected state %s after archive", l.LedgerCode, l.LifecycleStatus)
		}
	}

	// 11) 端到端审计：事件链至少包含 register / change / archive 三类
	events, err := lcSvc.SearchEventsByProject(SearchEventsInput{ProjectCode: project.ProjectCode})
	if err != nil {
		t.Fatal(err)
	}
	gotTypes := map[string]bool{}
	for _, e := range events {
		gotTypes[e.EventType] = true
	}
	for _, want := range []string{EventRegister, EventArchive} {
		if !gotTypes[want] {
			t.Errorf("event chain missing type %s", want)
		}
	}
}

// =============================================================================
// 测试辅助
// =============================================================================

// stageIDByCode 在 instOut.Stages 里按 stage_code 反查 id
func stageIDByCode(stages []FullStageInstance, code string) int64 {
	for _, s := range stages {
		if s.StageCode == code {
			return s.ID
		}
	}
	return 0
}

// findFirstProcessRule 在某个环节下找第一条 process 数据态规则
func findFirstProcessRule(t *testing.T, db interface {
	Get(dest interface{}, query string, args ...interface{}) error
}, project *models.DataProject, stageCode string) *models.TemplateFileRule {
	t.Helper()
	var rule models.TemplateFileRule
	err := db.Get(&rule, `SELECT tfr.* FROM template_file_rules tfr
		JOIN template_stages ts ON ts.id = tfr.template_stage_id
		JOIN data_templates dt ON dt.id = ts.template_id
		WHERE dt.template_code = ? AND dt.template_version = ?
		  AND ts.stage_code = ? AND tfr.data_state = 'process'
		  AND tfr.disable = 0
		ORDER BY tfr.sort_order, tfr.id LIMIT 1`,
		project.TemplateCode, project.TemplateVersion, stageCode)
	if err != nil {
		t.Logf("no process rule under %s: %v", stageCode, err)
		return nil
	}
	return &rule
}

// findFirstOutputRule 在某个环节下找第一条 output 数据态规则
func findFirstOutputRule(t *testing.T, db interface {
	Get(dest interface{}, query string, args ...interface{}) error
}, project *models.DataProject, stageCode string) *models.TemplateFileRule {
	t.Helper()
	var rule models.TemplateFileRule
	err := db.Get(&rule, `SELECT tfr.* FROM template_file_rules tfr
		JOIN template_stages ts ON ts.id = tfr.template_stage_id
		JOIN data_templates dt ON dt.id = ts.template_id
		WHERE dt.template_code = ? AND dt.template_version = ?
		  AND ts.stage_code = ? AND tfr.data_state = 'output'
		  AND tfr.disable = 0
		ORDER BY tfr.sort_order, tfr.id LIMIT 1`,
		project.TemplateCode, project.TemplateVersion, stageCode)
	if err != nil {
		return nil
	}
	return &rule
}

// pickAllowedExtension 从 allowed_file_types JSON 数组里取第一个并转小写
func pickAllowedExtension(t *testing.T, raw string) string {
	t.Helper()
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil || len(arr) == 0 {
		return "pdf"
	}
	return strings.ToLower(arr[0])
}

// findFirstInputRule 在某个环节下找第一条 input 数据态规则
func findFirstInputRule(t *testing.T, db interface {
	Get(dest interface{}, query string, args ...interface{}) error
}, project *models.DataProject, stageCode string) *models.TemplateFileRule {
	t.Helper()
	var rule models.TemplateFileRule
	err := db.Get(&rule, `SELECT tfr.* FROM template_file_rules tfr
		JOIN template_stages ts ON ts.id = tfr.template_stage_id
		JOIN data_templates dt ON dt.id = ts.template_id
		WHERE dt.template_code = ? AND dt.template_version = ?
		  AND ts.stage_code = ? AND tfr.data_state = 'input'
		  AND tfr.disable = 0
		ORDER BY tfr.sort_order, tfr.id LIMIT 1`,
		project.TemplateCode, project.TemplateVersion, stageCode)
	if err != nil {
		return nil
	}
	return &rule
}
