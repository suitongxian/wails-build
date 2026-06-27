package repository

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// UserSnapshot 解绑/重归类操作的用户上下文（可选）
type UserSnapshot struct {
	UserID *int64
	Name   string
}

// ReclassifyInput 重新归类入参
type ReclassifyInput struct {
	OriginalFvID    int64
	NewProjectID    int64
	NewStageCode    string
	NewFileRuleCode string
	Reason          string
	OperatorUser    *UserSnapshot
}

// UnbindFileVersion 解除绑定一个 file_version
//
// 把 fv + ledger lifecycle 置 cancelled，写 reclassify_history (action=unbind) +
// lifecycle_event (event_type=unbind)。不删数据，保留审计链。
// 调用方（HTTP 层）负责事后写 audit_logs。
func UnbindFileVersion(db *sqlx.DB, fvID int64, reason string, operator *UserSnapshot) error {
	if fvID <= 0 {
		return fmt.Errorf("invalid fv id")
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("reason 必填")
	}

	now := time.Now()
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var curStatus string
	if err := tx.Get(&curStatus, `SELECT lifecycle_status FROM file_versions WHERE id = ? AND disable = 0`, fvID); err != nil {
		return fmt.Errorf("fv 不存在: %w", err)
	}
	if curStatus == "cancelled" {
		return fmt.Errorf("fv 已解除绑定，无需重复操作")
	}

	if _, err := tx.Exec(`UPDATE file_versions
		SET lifecycle_status = 'cancelled', unbind_time = ?, unbind_reason = ?, update_time = ?
		WHERE id = ?`, now, reason, now, fvID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE asset_ledgers
		SET lifecycle_status = 'cancelled', update_time = ?
		WHERE file_version_id = ?`, now, fvID); err != nil {
		return err
	}

	var opID *int64
	var opName string
	if operator != nil {
		opID = operator.UserID
		opName = operator.Name
	}
	if _, err := tx.Exec(`INSERT INTO reclassify_history (
		original_fv_id, action, reason, operator_user_id, operator_name, create_time
	) VALUES (?, 'unbind', ?, ?, ?, ?)`, fvID, reason, opID, opName, now); err != nil {
		return err
	}

	opStr := "system"
	if opName != "" {
		opStr = opName
	}
	if _, err := tx.Exec(`INSERT INTO lifecycle_events (
		file_version_id, event_type, event_name, operator_id, reason, create_time
	) VALUES (?, ?, '解除绑定', ?, ?, ?)`,
		fvID, EventUnbind, opStr, reason, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ReclassifyFileVersion 把原 fv 解绑 + 新建到新目标。返回新 fv id。
//
// 非原子性说明：Unbind 与 Bridge 不在同一事务（事务内只覆盖 Unbind）。
// 若 Bridge 失败，原 fv 已置 cancelled 但无新 fv 替代——调用方应感知此状态：
// 错误返回时原 fv 一定 cancelled。HTTP 层可记录补偿事件供用户人工重试。
func ReclassifyFileVersion(db *sqlx.DB, in ReclassifyInput) (int64, error) {
	if in.OriginalFvID <= 0 {
		return 0, fmt.Errorf("invalid original fv id")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return 0, fmt.Errorf("reason 必填")
	}
	if in.NewProjectID <= 0 || in.NewStageCode == "" || in.NewFileRuleCode == "" {
		return 0, fmt.Errorf("new project/stage/rule 必填")
	}

	var sourceRef string
	if err := db.Get(&sourceRef, `SELECT COALESCE(source_ref, '') FROM asset_ledgers WHERE file_version_id = ? AND disable = 0`, in.OriginalFvID); err != nil {
		return 0, fmt.Errorf("查 ledger source_ref: %w", err)
	}
	resourceID := jsonExtractInt(sourceRef, "resource_id")
	if resourceID == 0 {
		return 0, fmt.Errorf("原 fv 无 resource source_ref，无法重新桥接")
	}

	if err := UnbindFileVersion(db, in.OriginalFvID, "因重新归类而解绑："+in.Reason, in.OperatorUser); err != nil {
		return 0, fmt.Errorf("解绑原 fv: %w", err)
	}

	item, err := BridgeResourceToTarget(db, resourceID, in.NewProjectID, in.NewStageCode, in.NewFileRuleCode)
	if err != nil {
		return 0, fmt.Errorf("桥接到新目标: %w", err)
	}
	if item.Status == "error" {
		return 0, fmt.Errorf("%s", item.ErrorMsg)
	}
	newFvID := item.FileVersionID

	now := time.Now()
	if _, err := db.Exec(`UPDATE file_versions SET reclassified_from_fv_id = ?, update_time = ? WHERE id = ?`,
		in.OriginalFvID, now, newFvID); err != nil {
		return newFvID, fmt.Errorf("更新 reclassified_from_fv_id: %w", err)
	}

	var opID *int64
	var opName string
	if in.OperatorUser != nil {
		opID = in.OperatorUser.UserID
		opName = in.OperatorUser.Name
	}
	if _, err := db.Exec(`INSERT INTO reclassify_history (
		original_fv_id, new_fv_id, action, reason, operator_user_id, operator_name, create_time
	) VALUES (?, ?, 'reclassify', ?, ?, ?, ?)`,
		in.OriginalFvID, newFvID, in.Reason, opID, opName, now); err != nil {
		log.Printf("[ReclassifyFileVersion] write history failed for orig_fv=%d new_fv=%d: %v", in.OriginalFvID, newFvID, err)
	}

	opStr := "system"
	if opName != "" {
		opStr = opName
	}
	if _, err := db.Exec(`INSERT INTO lifecycle_events (
		file_version_id, event_type, event_name, operator_id, reason, create_time
	) VALUES (?, ?, '重新归类', ?, ?, ?)`,
		newFvID, EventReclassify, opStr,
		fmt.Sprintf("从 fv(%d) 重新归类：%s", in.OriginalFvID, in.Reason), now); err != nil {
		log.Printf("[ReclassifyFileVersion] write lifecycle_event failed for new_fv=%d: %v", newFvID, err)
	}

	return newFvID, nil
}

// jsonExtractInt 极简 JSON 整型字段抽取器。
//
// 仅支持 {"key":N,...} 形态：key 紧接冒号无空格、N 为非负整数无引号。
// 不处理空格、负数、引号包裹的数字、嵌套。返回 0 表示未找到或格式不符。
//
// 设计目的：仅用于解析 asset_ledgers.source_ref，其格式由 personal_files_bridge.go
// 内部写入，固定为 {"bridge_from":"...","resource_id":N}。不要在其他场景使用——
// 如需通用 JSON 解析请用 encoding/json。
func jsonExtractInt(s, key string) int64 {
	needle := `"` + key + `":`
	i := strings.Index(s, needle)
	if i < 0 {
		return 0
	}
	j := i + len(needle)
	end := j
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == j {
		return 0
	}
	var n int64
	_, _ = fmt.Sscanf(s[j:end], "%d", &n)
	return n
}
