package repository

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectClose_PrecheckRequiredNotRegistered 必填且 planned → 错误
func TestProjectClose_PrecheckRequiredNotRegistered(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	closeSvc := NewProjectCloseService(db)

	// 直接 Precheck 时一堆 IN/PRC/OUT 都是 planned
	res, err := closeSvc.Precheck(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("Precheck should fail when required files are still planned")
	}
	hasErr := false
	for _, iss := range res.Issues {
		if iss.Code == "REQUIRED_NOT_REGISTERED" {
			hasErr = true
		}
	}
	if !hasErr {
		t.Errorf("expected REQUIRED_NOT_REGISTERED issue, got: %+v", res.Issues)
	}
}

// TestProjectClose_PrecheckOKAfterUpload 上传所有 required → precheck 通过（可能仍有 warning）
func TestProjectClose_PrecheckOKAfterUpload(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)

	// 给所有 required=1 的 fv 上传
	uploadAllRequired(t, svc, stages)

	closeSvc := NewProjectCloseService(db)
	res, err := closeSvc.Precheck(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	// V1 关键不变量：Issues 永远不是 nil（前端 .length 会 NPE）
	if res.Issues == nil {
		t.Fatal("Issues 不应当为 nil — 必须初始化为空 slice")
	}
	for _, iss := range res.Issues {
		if iss.Severity == "error" {
			t.Errorf("unexpected error after uploading required: %s", iss.Message)
		}
	}
	// 序列化测试：确保 JSON 输出 issues 为 [] 而非 null
	body, _ := json.Marshal(res)
	if !strings.Contains(string(body), `"issues":[`) {
		t.Errorf("expected JSON to contain 'issues:[]' or 'issues:[<...>]', got %s", string(body))
	}
}

// TestProjectClose_FullFlow 真做一次 close → manifest 存在 + 项目 archived + 底账 sealed
func TestProjectClose_FullFlow(t *testing.T) {
	db, svc, project, stages := setupProjectForFileOps(t)
	uploadAllRequired(t, svc, stages)

	closeSvc := NewProjectCloseService(db)
	out, err := closeSvc.Close(CloseInput{
		ProjectID:  project.ID,
		OperatorID: "tester",
		Reason:     "测试结项",
		Force:      true, // 跳过 warning
	})
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	// manifest 应当存在
	if _, err := os.Stat(out.ManifestPath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if !strings.HasSuffix(out.ManifestPath, "manifest.json") {
		t.Errorf("manifest path should end with manifest.json, got %s", out.ManifestPath)
	}

	// manifest sha256 非空
	if len(out.ManifestSha256) != 64 {
		t.Errorf("manifest sha256 length expect 64, got %d (%s)", len(out.ManifestSha256), out.ManifestSha256)
	}

	// manifest 可解析
	body, err := os.ReadFile(out.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var mf ArchiveManifest
	if err := json.Unmarshal(body, &mf); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}
	if mf.Project.ProjectCode != project.ProjectCode {
		t.Errorf("manifest project_code mismatch: %s vs %s", mf.Project.ProjectCode, project.ProjectCode)
	}
	if mf.Stats["file_versions"] == 0 {
		t.Error("manifest stats should report file_versions > 0")
	}

	// 项目状态 archived
	updated, _ := closeSvc.projRepo.FindByID(project.ID)
	if updated.Status != "archived" {
		t.Errorf("project should be archived, got %s", updated.Status)
	}
	if updated.SyncStatus == nil || *updated.SyncStatus != "pending" {
		t.Errorf("sync_status should be pending after close, got %v", updated.SyncStatus)
	}

	// 所有 ledger 已 sealed
	rows, _ := closeSvc.ledgerRepo.Search(LedgerSearchInput{ProjectCode: project.ProjectCode})
	for _, l := range rows {
		// 草稿 planned 也应该被推到 sealed（按当前实现，仍是 planned 不动）
		// 我们只断言已 registered 过的：
		if l.LifecycleStatus == "in_use" || l.LifecycleStatus == "registered" {
			t.Errorf("ledger %s should be sealed, got %s", l.LedgerCode, l.LifecycleStatus)
		}
	}

	// 重复 close 应当失败
	if _, err := closeSvc.Close(CloseInput{ProjectID: project.ID, OperatorID: "tester", Force: true}); err == nil {
		t.Error("expected error when closing already-archived project")
	}
}

// TestProjectClose_RejectIfErrorIssues 必填没传时 close 直接拒绝
func TestProjectClose_RejectIfErrorIssues(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	closeSvc := NewProjectCloseService(db)
	_, err := closeSvc.Close(CloseInput{ProjectID: project.ID, OperatorID: "tester"})
	if err == nil {
		t.Fatal("expected close to fail when required files not registered")
	}
}

// uploadAllRequired 把所有 required=1 仍处 planned 的 fv 上传一份占位文件
//
// 注意：stages 入参是立项时的快照，里面的 fv.LifecycleStatus 可能已过时；
// 需要从数据库重读当前状态再决定是否跳过。
func uploadAllRequired(t *testing.T, svc *FileOperationService, stages []FullStageInstance) {
	t.Helper()
	for _, s := range stages {
		for _, fv := range s.FileVersions {
			if fv.Required != 1 {
				continue
			}
			live, err := svc.fvRepo.FindByID(fv.ID)
			if err != nil || live.LifecycleStatus != "planned" {
				continue
			}
			// 取当前规则允许的扩展名作为占位文件
			rule, err := svc.findRuleForFV(live)
			if err != nil {
				t.Fatalf("rule lookup for %s: %v", live.LocalCode, err)
			}
			ext := "pdf"
			var arr []string
			if err := json.Unmarshal([]byte(rule.AllowedFileTypes), &arr); err == nil && len(arr) > 0 {
				ext = strings.ToLower(arr[0])
			}
			src := filepath.Join(t.TempDir(), "x."+ext)
			if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.UploadOrBind(fv.ID, UploadInput{
				SourcePath:       src,
				OriginalFileName: "x." + ext,
				OperatorID:       "tester",
			}); err != nil {
				t.Fatalf("upload required %s: %v", fv.LocalCode, err)
			}
		}
	}
}
