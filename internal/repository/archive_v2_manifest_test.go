package repository

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"data-asset-scan-go/internal/models"
)

// V2-收尾：归档 manifest 应该把 V2 audit user_id 字段一并打包传给 manage
//
// 否则归档移交之后 manage 侧拿不到 user_id，审计链断在 scan 端。
func TestArchiveManifest_IncludesV2UserIDFields(t *testing.T) {
	db, _, project, stages := setupProjectForFileOps(t)
	userRepo := NewUserRepository(db)
	u, _ := userRepo.Create(models.CreateUserInput{Username: "creator", DisplayName: "立项人"})

	// 立项人作为 project_member（带 user_id）
	uid := u.ID
	memberRepo := NewProjectMemberRepository(db)
	tx, _ := db.Beginx()
	memberRepo.Insert(tx, CreateProjectMemberInput{
		ProjectID:         project.ID,
		UserID:            &uid,
		SubjectID:         0,
		RoleCode:          "立项人",
		PermissionActions: `["read","write","close"]`,
	})
	tx.Commit()

	// 把 project.created_by_user_id 也填上
	db.Exec(`UPDATE data_projects SET created_by_user_id = ? WHERE id = ?`, uid, project.ID)

	// 上传所有 required 完成结项准备
	svc := NewFileOperationService(db)
	uploadAllRequired(t, svc, stages)

	// 给一个 fv 加 V2 审计字段（模拟 V2 路径写入）
	db.Exec(`UPDATE file_versions SET created_by_user_id = ?, submitted_by_user_id = ?
		WHERE id = (SELECT id FROM file_versions WHERE project_id = ? AND data_state = 'output' ORDER BY id LIMIT 1)`, uid, uid, project.ID)
	// 给一个 lifecycle_event 加 operator_user_id
	db.Exec(`UPDATE lifecycle_events SET operator_user_id = ?
		WHERE id = (SELECT id FROM lifecycle_events ORDER BY id DESC LIMIT 1)`, uid)

	closeSvc := NewProjectCloseService(db)
	out, err := closeSvc.Close(CloseInput{
		ProjectID:      project.ID,
		OperatorID:     "creator",
		OperatorUserID: uid,
		Force:          true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 读 manifest.json
	raw, err := os.ReadFile(out.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}

	// project.created_by_user_id
	proj := manifest["project"].(map[string]interface{})
	if proj["created_by_user_id"] == nil {
		t.Error("manifest.project.created_by_user_id 缺失")
	}

	// 至少一条 file_version 含 created_by_user_id 或 submitted_by_user_id
	fvs := manifest["file_versions"].([]interface{})
	hasFvUID := false
	for _, f := range fvs {
		m := f.(map[string]interface{})
		if m["created_by_user_id"] != nil || m["submitted_by_user_id"] != nil {
			hasFvUID = true
			break
		}
	}
	if !hasFvUID {
		t.Error("manifest.file_versions 中没有任何条带 V2 user_id 字段")
	}

	// 至少一条 member 含 user_id
	members := manifest["members"].([]interface{})
	hasMemberUID := false
	for _, m := range members {
		mm := m.(map[string]interface{})
		if mm["user_id"] != nil {
			hasMemberUID = true
			break
		}
	}
	if !hasMemberUID {
		t.Error("manifest.members 中没有任何条带 user_id")
	}

	// 至少一条 event 含 operator_user_id
	evs := manifest["lifecycle_events"].([]interface{})
	hasEvUID := false
	for _, e := range evs {
		ee := e.(map[string]interface{})
		if ee["operator_user_id"] != nil {
			hasEvUID = true
			break
		}
	}
	if !hasEvUID {
		t.Error("manifest.lifecycle_events 中没有任何条带 operator_user_id")
	}

	// JSON 整体不应包含 V1 字段名 "created_by_user_id" 解析失败之类
	if strings.Contains(string(raw), `"created_by_user_id":null,`) {
		// 这只是 sanity check：因为我们用 omitempty + *int64，0 值不会序列化
		t.Log("manifest 含 null 字段，按 omitempty 设计应该不出现，但不致命")
	}
}
