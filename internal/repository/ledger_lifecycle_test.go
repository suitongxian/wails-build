package repository

import (
	"strings"
	"testing"
)

// TestLedgerLifecycle_TransitionHappyPath 验证 registered → in_use → sealed → permanent 完整路径
func TestLedgerLifecycle_TransitionHappyPath(t *testing.T) {
	_, svc, _, stages := setupProjectForFileOps(t)

	// 上传一个输入文件，触发底账 registered
	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	if fv == nil {
		t.Fatal("expected planned IN-001")
	}
	src := writeTempFile(t, "case.pdf", "x")
	res, err := svc.UploadOrBind(fv.ID, UploadInput{
		SourcePath: src, OriginalFileName: "case.pdf", OperatorID: "tester",
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	ledgerID := res.Ledger.ID
	if res.Ledger.LifecycleStatus != "registered" {
		t.Fatalf("after bind, ledger status should be registered, got %s", res.Ledger.LifecycleStatus)
	}

	lcSvc := NewLedgerLifecycleService(svc.DB)

	// registered → in_use
	if err := lcSvc.Transition(TransitionInput{LedgerID: ledgerID, ToStatus: "in_use", OperatorID: "tester", Reason: "投入工作"}); err != nil {
		t.Fatalf("registered->in_use: %v", err)
	}
	ledger, _ := svc.ledgerRepo.FindByID(ledgerID)
	if ledger.LifecycleStatus != "in_use" {
		t.Fatalf("ledger should be in_use, got %s", ledger.LifecycleStatus)
	}
	fvAfter, _ := svc.fvRepo.FindByID(res.FileVersion.ID)
	if fvAfter.LifecycleStatus != "in_use" {
		t.Fatalf("fv should also be in_use, got %s", fvAfter.LifecycleStatus)
	}

	// in_use → sealed
	if err := lcSvc.Transition(TransitionInput{LedgerID: ledgerID, ToStatus: "sealed", OperatorID: "tester", Reason: "归档"}); err != nil {
		t.Fatalf("in_use->sealed: %v", err)
	}
	// sealed → permanent
	if err := lcSvc.Transition(TransitionInput{LedgerID: ledgerID, ToStatus: "permanent", OperatorID: "tester"}); err != nil {
		t.Fatalf("sealed->permanent: %v", err)
	}

	// 事件流应包含 use / archive / permanent
	events, err := svc.eventRepo.ListByFileVersion(res.FileVersion.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, ev := range events {
		got[ev.EventType] = true
	}
	for _, want := range []string{EventRegister, EventUse, EventArchive, EventPermanent} {
		if !got[want] {
			t.Errorf("missing event type %s in lifecycle", want)
		}
	}
}

// TestLedgerLifecycle_RejectInvalidTransition planned → in_use 是不允许的
func TestLedgerLifecycle_RejectInvalidTransition(t *testing.T) {
	_, svc, project, _ := setupProjectForFileOps(t)
	_ = project

	// 找一个 planned 状态的底账（立项时创建的草稿）
	var ledgerID int64
	if err := svc.DB.Get(&ledgerID, `SELECT id FROM asset_ledgers WHERE lifecycle_status = 'planned' AND disable = 0 LIMIT 1`); err != nil {
		t.Fatalf("need a planned ledger for negative test: %v", err)
	}

	lcSvc := NewLedgerLifecycleService(svc.DB)
	err := lcSvc.Transition(TransitionInput{LedgerID: ledgerID, ToStatus: "in_use", OperatorID: "tester"})
	if err == nil {
		t.Fatal("expected error for planned->in_use")
	}
	if !strings.Contains(err.Error(), "不允许的状态转换") {
		t.Fatalf("expected '不允许的状态转换' error, got: %v", err)
	}
}

// TestLedgerLifecycle_SearchEventsByProject 项目级事件流应聚合所有 fv 的事件
func TestLedgerLifecycle_SearchEventsByProject(t *testing.T) {
	_, svc, project, stages := setupProjectForFileOps(t)

	// 在 MZ-SG 上传一个输入文件，触发若干事件
	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src := writeTempFile(t, "a.pdf", "x")
	if _, err := svc.UploadOrBind(fv.ID, UploadInput{SourcePath: src, OriginalFileName: "a.pdf", OperatorID: "tester"}); err != nil {
		t.Fatal(err)
	}

	lcSvc := NewLedgerLifecycleService(svc.DB)
	events, err := lcSvc.SearchEventsByProject(SearchEventsInput{ProjectCode: project.ProjectCode})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event for project")
	}
	for _, ev := range events {
		if ev.ProjectCode == nil || *ev.ProjectCode != project.ProjectCode {
			t.Errorf("event project_code mismatch: %v", ev.ProjectCode)
		}
	}
}

// TestLedgerLifecycle_FilterByEventType 限定 event_type 过滤
func TestLedgerLifecycle_FilterByEventType(t *testing.T) {
	_, svc, project, stages := setupProjectForFileOps(t)

	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src := writeTempFile(t, "b.pdf", "x")
	res, err := svc.UploadOrBind(fv.ID, UploadInput{SourcePath: src, OriginalFileName: "b.pdf", OperatorID: "tester"})
	if err != nil {
		t.Fatal(err)
	}

	lcSvc := NewLedgerLifecycleService(svc.DB)
	if err := lcSvc.Transition(TransitionInput{LedgerID: res.Ledger.ID, ToStatus: "in_use", OperatorID: "tester"}); err != nil {
		t.Fatal(err)
	}

	useEvents, err := lcSvc.SearchEventsByProject(SearchEventsInput{ProjectCode: project.ProjectCode, EventType: EventUse})
	if err != nil {
		t.Fatal(err)
	}
	if len(useEvents) == 0 {
		t.Fatal("expected at least one 'use' event")
	}
	for _, ev := range useEvents {
		if ev.EventType != EventUse {
			t.Errorf("expected event_type=use, got %s", ev.EventType)
		}
	}
}

// TestLedgerSearch_ByProjectAndKeyword 验证底账搜索基本筛选
func TestLedgerSearch_ByProjectAndKeyword(t *testing.T) {
	_, svc, project, stages := setupProjectForFileOps(t)

	fv := findFvByLocalCode(t, stages, "MZ-SG", "IN-001")
	src := writeTempFile(t, "客户原稿.pdf", "x")
	if _, err := svc.UploadOrBind(fv.ID, UploadInput{SourcePath: src, OriginalFileName: "客户原稿.pdf", OperatorID: "tester"}); err != nil {
		t.Fatal(err)
	}

	rows, err := svc.ledgerRepo.Search(LedgerSearchInput{ProjectCode: project.ProjectCode})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least one ledger for project")
	}

	// 确保都是该项目的
	for _, l := range rows {
		if l.ProjectCode != project.ProjectCode {
			t.Errorf("ledger.project_code = %s, want %s", l.ProjectCode, project.ProjectCode)
		}
	}
}
