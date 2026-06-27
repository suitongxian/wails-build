package httpd

import (
	"testing"

	"data-asset-scan-go/internal/repository"
)

// V3-8 §8.2 POST /projects/:id/activate: draft → active
func TestHTTP_Projects_Activate(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, _ := seedTemplateAndProject(t, db) // 默认 active 状态
	withActiveUser(t, db, "OWNER-1")

	// 把项目改回 draft 模拟分步立项场景
	db.Exec(`UPDATE data_projects SET status = 'draft' WHERE id = ?`, pj.ID)

	status, resp := jsonReq(t, r, "POST", "/projects/"+itoa(pj.ID)+"/activate", nil)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["status"] != "active" {
		t.Errorf("status 应为 active, got %v", d["status"])
	}
}

// V3-8 active 项目不允许重复 activate
func TestHTTP_Projects_Activate_RejectNonDraft(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, _ := seedTemplateAndProject(t, db) // active
	withActiveUser(t, db, "OWNER-1")

	status, resp := jsonReq(t, r, "POST", "/projects/"+itoa(pj.ID)+"/activate", nil)
	expectFailure(t, status, resp)
}

// V3-8 §8.2 POST /projects/:id/cancel: active → cancelled
func TestHTTP_Projects_Cancel(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, _ := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")

	status, resp := jsonReq(t, r, "POST", "/projects/"+itoa(pj.ID)+"/cancel", map[string]any{
		"reason": "需求变更",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["status"] != "cancelled" {
		t.Errorf("status 应为 cancelled, got %v", d["status"])
	}
}

func TestHTTP_Projects_Cancel_WithMirroredAdminUser(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, _ := seedTemplateAndProject(t, db)

	if err := repository.NewUserInfoRepository(db).MirrorManagedAuthUser(repository.ManagedAuthUser{
		Username:       "admin",
		DisplayName:    "",
		UserUnit:       "第一研究院",
		UserDepartment: "系统管理部",
	}); err != nil {
		t.Fatalf("mirror admin user: %v", err)
	}

	var adminID int64
	if err := db.Get(&adminID, `SELECT id FROM users WHERE username = 'admin' AND disable = 0`); err != nil || adminID == 0 {
		t.Fatalf("admin should be mirrored into users, id=%d err=%v", adminID, err)
	}
	if _, err := db.Exec(`INSERT INTO project_members (
		project_id, user_id, subject_id, role_code, permission_actions,
		create_time, update_time, disable
	) VALUES (?, ?, 0, '项目负责人', '["close"]', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 0)`,
		pj.ID, adminID); err != nil {
		t.Fatalf("insert admin project member: %v", err)
	}

	currentAuthSession.Lock()
	currentAuthSession.session = &authSession{
		Token: "token-admin",
		User: authUser{
			ID:             adminID,
			Username:       "admin",
			DisplayName:    "",
			UserUnit:       "第一研究院",
			UserDepartment: "系统管理部",
			Status:         "active",
		},
	}
	currentAuthSession.Unlock()
	defer func() {
		currentAuthSession.Lock()
		currentAuthSession.session = nil
		currentAuthSession.Unlock()
	}()

	status, resp := jsonReq(t, r, "POST", "/projects/"+itoa(pj.ID)+"/cancel", map[string]any{
		"reason": "演示数据清理",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["status"] != "cancelled" {
		t.Errorf("status 应为 cancelled, got %v", d["status"])
	}
}

// V3-8 archived 项目不能取消
func TestHTTP_Projects_Cancel_RejectArchived(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	pj, _ := seedTemplateAndProject(t, db)
	withActiveUser(t, db, "OWNER-1")
	db.Exec(`UPDATE data_projects SET status = 'archived' WHERE id = ?`, pj.ID)

	status, resp := jsonReq(t, r, "POST", "/projects/"+itoa(pj.ID)+"/cancel", nil)
	expectFailure(t, status, resp)
}
