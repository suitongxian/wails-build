package repository

import (
	"strings"
	"testing"
	"time"
)

// TestProjectAuth_SystemBypass system 身份直接放行
func TestProjectAuth_SystemBypass(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	authSvc := NewProjectAuthService(db)
	if err := authSvc.CheckProjectAction("system", project.ID, "close"); err != nil {
		t.Fatalf("system should be allowed: %v", err)
	}
}

// TestProjectAuth_StrictMatchByCode operator 匹配 subject.code 走严格模式
func TestProjectAuth_StrictMatchByCode(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	authSvc := NewProjectAuthService(db)

	// 测试 setup 创建了 owner subject 且作为 member 拥有 [read,write,submit,close]
	var ownerCode string
	if err := db.Get(&ownerCode, `SELECT code FROM subjects WHERE id = ?`, project.OwnerSubjectID); err != nil {
		t.Fatalf("read owner code: %v", err)
	}

	if err := authSvc.CheckProjectAction(ownerCode, project.ID, "write"); err != nil {
		t.Errorf("owner should have write: %v", err)
	}
	if err := authSvc.CheckProjectAction(ownerCode, project.ID, "close"); err != nil {
		t.Errorf("owner should have close: %v", err)
	}
}

// TestProjectAuth_StrictMatchDeny 已知 subject 但无对应权限 → 拒绝
func TestProjectAuth_StrictMatchDeny(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	authSvc := NewProjectAuthService(db)

	var ownerCode string
	_ = db.Get(&ownerCode, `SELECT code FROM subjects WHERE id = ?`, project.OwnerSubjectID)

	// owner 默认 perm 集是 [read,write,submit,close]，没有 destroy
	err := authSvc.CheckProjectAction(ownerCode, project.ID, "destroy")
	if err == nil {
		t.Fatal("expected permission denied for destroy")
	}
	if !IsPermissionDenied(err) {
		t.Errorf("expected PermissionDeniedError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "destroy") {
		t.Errorf("error should mention 'destroy', got: %v", err)
	}
}

// V2-5: operator 不在 users / subjects 任何一张表 → 一律拒绝（取消宽松回退）
//
// 旧 V1 行为是"项目里有人有该权限就放行"，但这让立项向导里的权限选择形同虚设：
// 任何陌生身份只要项目里某成员有 write 都能写，等于绕开了权限矩阵。
func TestProjectAuth_StrictNoLooseFallback(t *testing.T) {
	db, _, project, _ := setupProjectForFileOps(t)
	authSvc := NewProjectAuthService(db)

	// "anonymous" 既不在 users 也不在 subjects → 必须拒绝
	err := authSvc.CheckProjectAction("anonymous", project.ID, "write")
	if err == nil {
		t.Fatal("V2-5 严格模式：未注册操作人必须被拒，旧的宽松回退已取消")
	}
	if !IsPermissionDenied(err) {
		t.Errorf("expected PermissionDeniedError, got %T: %v", err, err)
	}

	// 即使是项目里所有人都没有的动作（destroy），也照样拒绝
	if err := authSvc.CheckProjectAction("anonymous", project.ID, "destroy"); err == nil {
		t.Fatal("unknown operator + missing action must be denied")
	}
}

// TestParsePermissionActions JSON 与 CSV 双兼容
func TestParsePermissionActions(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{`["read","write"]`, []string{"read", "write"}},
		{`read,write,submit`, []string{"read", "write", "submit"}},
		{` read , write `, []string{"read", "write"}},
		{``, nil},
	}
	for _, c := range cases {
		got, err := parsePermissionActions(c.raw)
		if err != nil {
			t.Errorf("parse %q: %v", c.raw, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("parse %q: got %v, want %v", c.raw, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parse %q[%d]: got %s, want %s", c.raw, i, got[i], c.want[i])
			}
		}
	}
}

// V5-P1: SYS-PERSONAL-* 项目无 project_members 设计，
// 任意 active user 都应被授权（单终端单用户的个人文件管理模型）。
func TestCheckProjectAction_PersonalProject_AllowsActiveUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}

	// Create an active user with no project membership
	res, err := db.Exec(`INSERT INTO users (username, display_name, status, create_time, update_time, disable) VALUES ('alice', 'Alice', 'active', ?, ?, 0)`,
		time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = res

	var coreID int64
	db.Get(&coreID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalCoreProjectCode)

	svc := NewProjectAuthService(db)
	if err := svc.CheckProjectAction("alice", coreID, "write"); err != nil {
		t.Errorf("alice should be authorized on personal project, got: %v", err)
	}
}

// V5-P1: 个人项目对未注册用户仍然拒绝
func TestCheckProjectAction_PersonalProject_DenyUnknownUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	seedPersonalFilesTemplateV2InTest(t, db)
	if err := EnsurePersonalContextForTest(db); err != nil {
		t.Fatal(err)
	}
	var coreID int64
	db.Get(&coreID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalCoreProjectCode)
	svc := NewProjectAuthService(db)
	if err := svc.CheckProjectAction("ghost", coreID, "write"); err == nil {
		t.Error("unknown user should not be authorized")
	}
}

// V5-P1: 常规项目（非 SYS-PERSONAL-*）仍然必须查 project_members
func TestCheckProjectAction_RegularProject_StillEnforcesMembership(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	// Build a regular (non-personal) project with no members
	now := time.Now()
	_, _ = db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES ('REG-OWNER', 'reg-owner', 'person', 'active', ?, ?, 0)`, now, now)
	var subjID int64
	db.Get(&subjID, `SELECT id FROM subjects WHERE code = 'REG-OWNER'`)
	res, _ := db.Exec(`INSERT INTO data_projects (
		project_code, project_name, template_id, template_code, template_version,
		sensitivity_level, management_mode, owner_subject_id, custodian_subject_id, security_subject_id,
		status, created_by, create_time, update_time, disable
	) VALUES ('REG-001', '常规项目', 0, 'TPL-X', 'V1.0', 'general', 'independent', ?, ?, ?, 'active', 'system', ?, ?, 0)`,
		subjID, subjID, subjID, now, now)
	regID, _ := res.LastInsertId()

	db.Exec(`INSERT INTO users (username, display_name, status, create_time, update_time, disable) VALUES ('bob', 'Bob', 'active', ?, ?, 0)`,
		time.Now(), time.Now())

	svc := NewProjectAuthService(db)
	if err := svc.CheckProjectAction("bob", regID, "write"); err == nil {
		t.Error("regular project requires membership; bob should be denied")
	}
}
