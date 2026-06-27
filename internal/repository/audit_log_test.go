package repository

import (
	"strings"
	"testing"
)

// V3-5 §11.2 audit_logs 表存在
func TestAuditLogs_TableExists(t *testing.T) {
	db := openTestDB(t)
	rows, err := db.Queryx(`SELECT name FROM sqlite_master WHERE type='table' AND name='audit_logs'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("audit_logs 表应当存在")
	}
}

// V3-5 §11.2 字段对齐文档
func TestAuditLogs_ColumnsAlignWithDoc(t *testing.T) {
	db := openTestDB(t)
	// 文档 §11.2 列出: id / actor_id / action / target_type / target_id / before / after / ip_address / created_at
	// 实现里 before/after 用 _json 后缀，created_at 用 create_time（与其他表一致），含义对齐。
	cols := []string{"id", "actor_id", "action", "target_type", "target_id", "before_json", "after_json", "ip_address", "create_time"}
	for _, c := range cols {
		ok, err := columnExists(db, "audit_logs", c)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("audit_logs 缺字段 %s（文档 §11.2 要求）", c)
		}
	}
}

// V3-5 Append + 取回
func TestAuditLog_AppendAndRetrieve(t *testing.T) {
	db := openTestDB(t)
	repo := NewAuditLogRepository(db)

	id, err := repo.Append(AppendAuditInput{
		ActorID:     "alice",
		ActorUserID: 42,
		Action:      AuditProjectCreate,
		TargetType:  AuditTargetProject,
		TargetID:    100,
		TargetCode:  "P-T1",
		After:       map[string]any{"name": "测试项目"},
		IPAddress:   "127.0.0.1",
		Message:     "立项测试",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("应当返回 id")
	}

	list, err := repo.ListByTarget(AuditTargetProject, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListByTarget 应返 1 条，got %d", len(list))
	}
	got := list[0]
	if got.ActorID != "alice" || got.Action != AuditProjectCreate {
		t.Errorf("字段不对: %+v", got)
	}
	if got.ActorUserID == nil || *got.ActorUserID != 42 {
		t.Errorf("actor_user_id 应为 42, got %v", got.ActorUserID)
	}
	if got.AfterJSON == nil || !strings.Contains(*got.AfterJSON, "测试项目") {
		t.Errorf("after_json 应当包含 '测试项目', got %v", got.AfterJSON)
	}
	if got.IPAddress == nil || *got.IPAddress != "127.0.0.1" {
		t.Errorf("ip_address 错: %v", got.IPAddress)
	}
}

// V3-5 Search 按 action 筛
func TestAuditLog_SearchByAction(t *testing.T) {
	db := openTestDB(t)
	repo := NewAuditLogRepository(db)
	repo.Append(AppendAuditInput{ActorID: "u", Action: AuditProjectCreate, TargetType: AuditTargetProject, TargetID: 1})
	repo.Append(AppendAuditInput{ActorID: "u", Action: AuditProjectClose, TargetType: AuditTargetProject, TargetID: 1})
	repo.Append(AppendAuditInput{ActorID: "u", Action: AuditExportLedger, TargetType: AuditTargetExport, TargetID: 0})

	list, err := repo.Search(AuditSearchInput{Action: AuditProjectClose})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Action != AuditProjectClose {
		t.Errorf("筛 close 应返 1 条，got %d", len(list))
	}
}

// V3-5 §11.1 8 类必须审计的操作，常量必须存在
func TestAuditLog_AllRequiredActionsDefined(t *testing.T) {
	// 模版相关
	checkConst(t, "AuditTemplatePublish", AuditTemplatePublish, "template_publish")
	checkConst(t, "AuditTemplateDeprecate", AuditTemplateDeprecate, "template_deprecate")
	checkConst(t, "AuditTemplateCopy", AuditTemplateCopy, "template_copy")
	checkConst(t, "AuditTemplateImport", AuditTemplateImport, "template_import")
	// 项目
	checkConst(t, "AuditProjectCreate", AuditProjectCreate, "project_create")
	checkConst(t, "AuditProjectActivate", AuditProjectActivate, "project_activate")
	checkConst(t, "AuditProjectClose", AuditProjectClose, "project_close")
	checkConst(t, "AuditProjectCancel", AuditProjectCancel, "project_cancel")
	// 三主体
	checkConst(t, "AuditSubjectHandover", AuditSubjectHandover, "subject_handover")
	// 文件
	checkConst(t, "AuditFileUpload", AuditFileUpload, "file_upload")
	checkConst(t, "AuditFileDelete", AuditFileDelete, "file_delete")
	// 权限
	checkConst(t, "AuditMemberAdd", AuditMemberAdd, "member_add")
	checkConst(t, "AuditMemberUpdate", AuditMemberUpdate, "member_update")
	checkConst(t, "AuditMemberRemove", AuditMemberRemove, "member_remove")
	// 导出
	checkConst(t, "AuditExportLedger", AuditExportLedger, "export_ledger")
	checkConst(t, "AuditExportArchive", AuditExportArchive, "export_archive")
}

func checkConst(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s 字面量应为 %q，got %q", name, want, got)
	}
}
