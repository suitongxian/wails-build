package repository

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// 权限动作常量
//
// 与文档 §7.7 列出的 9 个动作严格对齐：
//
//	read / write / receive / upload / submit / share / archive / close / destroy
//
// upload 与 write 在文档里独立列出（write = 创建或修改过程文件；upload = 上传实体文件）；
// 实际权限校验中 V1 把两者合并到 write 路径，但 security_policy 默认 perm 集仍引用 upload，
// 所以这里把两者都列出以保证常量对齐文档语义。
const (
	PermRead    = "read"
	PermWrite   = "write"
	PermReceive = "receive"
	PermUpload  = "upload"
	PermSubmit  = "submit"
	PermShare   = "share"
	PermArchive = "archive"
	PermClose   = "close"
	PermDestroy = "destroy"
)

// AllPermActions 返回文档 §7.7 列出的全部 9 个权限动作，用于 UI 选项 / 校验白名单。
func AllPermActions() []string {
	return []string{
		PermRead, PermWrite, PermReceive, PermUpload,
		PermSubmit, PermShare, PermArchive, PermClose, PermDestroy,
	}
}

// IsValidPermAction 判断字符串是否是文档定义的合法权限动作
func IsValidPermAction(action string) bool {
	for _, a := range AllPermActions() {
		if a == action {
			return true
		}
	}
	return false
}

// ProjectAuthService 项目权限校验服务
//
// V1 简化策略：
//   - 通过当前活跃用户名（user_info.user_name）查 subjects（按 code 或 name 匹配）
//   - 在 project_members 表里找该 subject 在项目内的成员记录
//   - 检查 permission_actions JSON 是否包含目标动作
//   - 无成员记录或动作不在列表 → 拒绝
//
// 系统级身份（operator == "system"）一律放行，用于服务自身/后台任务。
type ProjectAuthService struct {
	DB         *sqlx.DB
	memberRepo *ProjectMemberRepository
}

func NewProjectAuthService(db *sqlx.DB) *ProjectAuthService {
	return &ProjectAuthService{
		DB:         db,
		memberRepo: NewProjectMemberRepository(db),
	}
}

// PermissionDeniedError 权限拒绝
type PermissionDeniedError struct {
	Reason string
}

func (e *PermissionDeniedError) Error() string {
	return e.Reason
}

// IsPermissionDenied 判断是否权限拒绝
func IsPermissionDenied(err error) bool {
	_, ok := err.(*PermissionDeniedError)
	return ok
}

// CheckProjectAction 校验当前操作人是否可以在该项目内执行某个动作。
//
// V2 严格策略（取消"项目里任意人有就放行"的宽松回退）：
//
//  1. operator == "system" 直接放行（迁移/后台任务）。
//  2. V2 路径：用 operatorName 找 users.username → users.id，
//     再查 project_members.user_id 是否含 action（这是立项向导/V2-3 自动登记的主路径）。
//  3. V1 兼容路径：若 users 表查不到（或 user 在项目里无成员），
//     回退用 operatorName 匹配 subjects.code/name → project_members.subject_id 是否含 action。
//  4. 都查不到 → 拒绝。
//
// 不再有"匿名操作人只要项目里有人有该权限就放行"的回退，
// 因为这等于让立项向导里的权限选择形同虚设（V1 已观察到该问题）。
//
// 返回 nil 表示允许；*PermissionDeniedError 表示被拒；其他 error 表示数据问题。
func (s *ProjectAuthService) CheckProjectAction(operatorName string, projectID int64, action string) error {
	if operatorName == "" {
		return &PermissionDeniedError{Reason: "未识别操作人，无法授权"}
	}
	if operatorName == "system" {
		return nil
	}

	// V5-P1: personal projects (SYS-PERSONAL-*) have no project_members by design.
	// They are auto-created per scan terminal and implicitly owned by the active user.
	// Any active (registered) operator is authorized for any action on these projects.
	var projCode string
	if err := s.DB.Get(&projCode, `SELECT project_code FROM data_projects WHERE id = ? AND disable = 0`, projectID); err == nil {
		if strings.HasPrefix(projCode, "SYS-PERSONAL-") {
			// Confirm operator exists as a real user (not blank)
			var userID int64
			_ = s.DB.Get(&userID, `SELECT id FROM users WHERE username = ? AND disable = 0 LIMIT 1`, operatorName)
			if userID > 0 {
				return nil
			}
			// No matching user; fall through to standard path (will return permission denied)
		}
	}

	// V2 路径：username → users.id → project_members.user_id
	var userID int64
	err := s.DB.Get(&userID, `SELECT id FROM users WHERE username = ? AND disable = 0 LIMIT 1`, operatorName)
	if err == nil && userID != 0 {
		members, err := s.memberRepo.FindByUserInProject(projectID, userID)
		if err != nil {
			return err
		}
		if len(members) > 0 {
			if memberHasAction(members, action) {
				return nil
			}
			return &PermissionDeniedError{Reason: fmt.Sprintf("操作人 %s 在该项目内无 %s 权限", operatorName, action)}
		}
		// V2 找到 user 但该 user 在项目里无成员记录 → 继续尝试 V1 subject 路径
	}

	// V1 兼容路径：operator 匹配 subjects.code/name → project_members.subject_id
	var subjectID int64
	err = s.DB.Get(&subjectID, `SELECT id FROM subjects WHERE (code = ? OR name = ?) AND disable = 0 LIMIT 1`,
		operatorName, operatorName)
	if err == nil && subjectID != 0 {
		members, err := s.memberRepo.FindBySubjectInProject(projectID, subjectID)
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return &PermissionDeniedError{Reason: fmt.Sprintf("操作人 %s 不是该项目成员", operatorName)}
		}
		if memberHasAction(members, action) {
			return nil
		}
		return &PermissionDeniedError{Reason: fmt.Sprintf("操作人 %s 在该项目内无 %s 权限", operatorName, action)}
	}

	return &PermissionDeniedError{Reason: fmt.Sprintf("操作人 %s 未注册为用户或主体，无法授权", operatorName)}
}

// CheckProjectActionByUserID V2 接口：直接用 user_id 校验，比 username→user 查找更直接。
//
// 仅查 project_members.user_id 路径（不做 subject 回退），适合明确知道当前登录用户的场景。
// system 身份请走 CheckProjectAction("system", ...)。
func (s *ProjectAuthService) CheckProjectActionByUserID(userID, projectID int64, action string) error {
	if userID <= 0 {
		return &PermissionDeniedError{Reason: "未识别操作人，无法授权"}
	}

	// V5-P1: personal projects auto-authorize any active user (see CheckProjectAction)
	var projCode string
	if err := s.DB.Get(&projCode, `SELECT project_code FROM data_projects WHERE id = ? AND disable = 0`, projectID); err == nil {
		if strings.HasPrefix(projCode, "SYS-PERSONAL-") {
			var exists int
			_ = s.DB.Get(&exists, `SELECT COUNT(1) FROM users WHERE id = ? AND disable = 0`, userID)
			if exists > 0 {
				return nil
			}
		}
	}

	members, err := s.memberRepo.FindByUserInProject(projectID, userID)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return &PermissionDeniedError{Reason: fmt.Sprintf("用户 %d 不是该项目成员", userID)}
	}
	if memberHasAction(members, action) {
		return nil
	}
	return &PermissionDeniedError{Reason: fmt.Sprintf("用户 %d 在该项目内无 %s 权限", userID, action)}
}

// CheckProjectStageAction 校验项目内某个具体环节的动作权限。
//
// stage_ids 为空表示项目级成员，适用于所有环节；非空时必须包含目标 stageID。
// file-version 类操作必须走这个方法，否则有同项目其他环节权限的用户会越权操作当前环节。
func (s *ProjectAuthService) CheckProjectStageAction(operatorName string, projectID, stageID int64, action string) error {
	if operatorName == "" {
		return &PermissionDeniedError{Reason: "未识别操作人，无法授权"}
	}
	if operatorName == "system" {
		return nil
	}
	if stageID <= 0 {
		return &PermissionDeniedError{Reason: "未识别目标环节，无法授权"}
	}

	var projCode string
	if err := s.DB.Get(&projCode, `SELECT project_code FROM data_projects WHERE id = ? AND disable = 0`, projectID); err == nil {
		if strings.HasPrefix(projCode, "SYS-PERSONAL-") {
			var userID int64
			_ = s.DB.Get(&userID, `SELECT id FROM users WHERE username = ? AND disable = 0 LIMIT 1`, operatorName)
			if userID > 0 {
				return nil
			}
		}
	}

	var userID int64
	err := s.DB.Get(&userID, `SELECT id FROM users WHERE username = ? AND disable = 0 LIMIT 1`, operatorName)
	if err == nil && userID != 0 {
		members, err := s.memberRepo.FindByUserInProject(projectID, userID)
		if err != nil {
			return err
		}
		if len(members) > 0 {
			if memberHasActionInStage(members, action, stageID) {
				return nil
			}
			return &PermissionDeniedError{Reason: fmt.Sprintf("操作人 %s 在当前环节无 %s 权限", operatorName, action)}
		}
	}

	var subjectID int64
	err = s.DB.Get(&subjectID, `SELECT id FROM subjects WHERE (code = ? OR name = ?) AND disable = 0 LIMIT 1`,
		operatorName, operatorName)
	if err == nil && subjectID != 0 {
		members, err := s.memberRepo.FindBySubjectInProject(projectID, subjectID)
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return &PermissionDeniedError{Reason: fmt.Sprintf("操作人 %s 不是该项目成员", operatorName)}
		}
		if memberHasActionInStage(members, action, stageID) {
			return nil
		}
		return &PermissionDeniedError{Reason: fmt.Sprintf("操作人 %s 在当前环节无 %s 权限", operatorName, action)}
	}

	return &PermissionDeniedError{Reason: fmt.Sprintf("操作人 %s 未注册为用户或主体，无法授权", operatorName)}
}

// memberHasAction 检查任一成员记录的 permission_actions 是否含 action
func memberHasAction(members []models.ProjectMember, action string) bool {
	for _, m := range members {
		actions, err := parsePermissionActions(m.PermissionActions)
		if err != nil {
			continue
		}
		for _, a := range actions {
			if a == action {
				return true
			}
		}
	}
	return false
}

func memberHasActionInStage(members []models.ProjectMember, action string, stageID int64) bool {
	for _, m := range members {
		if !memberCoversStage(m, stageID) {
			continue
		}
		actions, err := parsePermissionActions(m.PermissionActions)
		if err != nil {
			continue
		}
		for _, a := range actions {
			if a == action {
				return true
			}
		}
	}
	return false
}

func memberCoversStage(member models.ProjectMember, stageID int64) bool {
	stageIDs, err := parseMemberStageIDs(member.StageIDs)
	if err != nil {
		return false
	}
	if len(stageIDs) == 0 {
		return true
	}
	for _, id := range stageIDs {
		if id == stageID {
			return true
		}
	}
	return false
}

func parseMemberStageIDs(raw *string) ([]int64, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	s := strings.TrimSpace(*raw)
	if strings.HasPrefix(s, "[") {
		var arr []int64
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var id int64
		if _, err := fmt.Sscanf(p, "%d", &id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// parsePermissionActions 把 permission_actions 字段解析为字符串数组
//
// 兼容两种格式：标准 JSON 数组 ["read","write"]、CSV "read,write"。
func parsePermissionActions(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
