package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

var ErrCabinetSyncSkipped = errors.New("cabinet sync skipped")

// ManagedArchiveReporter 上报工作端产出到 manage 端归档目标。
//
// 只传结构化 JSON，不上传实体文件；柜/室真实承接边界在 manage 端。
type ManagedArchiveReporter struct {
	DB         *sqlx.DB
	configRepo *SystemConfigRepository
}

type ManagedArchiveReportResult struct {
	Status          string `json:"status"`
	Endpoint        string `json:"endpoint,omitempty"`
	Reply           string `json:"reply,omitempty"`
	StorageTier     string `json:"storage_tier,omitempty"`
	StorageLocation string `json:"storage_location,omitempty"`
	Error           string `json:"error,omitempty"`
}

func NewManagedArchiveReporter(db *sqlx.DB) *ManagedArchiveReporter {
	return &ManagedArchiveReporter{DB: db, configRepo: NewSystemConfigRepository(db)}
}

func (r *ManagedArchiveReporter) ReportFileVersionToCabinet(ctx context.Context, fvID int64) (*ManagedArchiveReportResult, error) {
	payload, err := r.buildFileArchivePayload(fvID)
	if err != nil {
		if errors.Is(err, ErrCabinetSyncSkipped) {
			msg := err.Error()
			_ = r.markCabinetSync(fvID, "skipped", msg, false)
			return &ManagedArchiveReportResult{Status: "skipped", Error: msg}, nil
		}
		_ = r.markCabinetSync(fvID, "failed", err.Error(), false)
		return &ManagedArchiveReportResult{Status: "failed", Error: err.Error()}, err
	}

	endpointBase := strings.TrimSpace(r.configRepo.GetValue(KeyArchiveEndpoint))
	if endpointBase == "" {
		endpointBase = strings.TrimSpace(r.configRepo.GetValue(KeyManageEndpoint))
	}
	if endpointBase == "" {
		msg := "未配置 manage 上报端点（system_configs key=manage_endpoint 或 archive_endpoint）"
		_ = r.markCabinetSync(fvID, "skipped", msg, false)
		return &ManagedArchiveReportResult{Status: "skipped", Error: msg}, nil
	}

	endpoint := strings.TrimRight(endpointBase, "/") + "/api/sync/file-archive"
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" {
		msg := fmt.Sprintf("非法的上报端点：%s", endpoint)
		_ = r.markCabinetSync(fvID, "failed", msg, false)
		return &ManagedArchiveReportResult{Status: "failed", Endpoint: endpoint, Error: msg}, fmt.Errorf("%s", msg)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		_ = r.markCabinetSync(fvID, "failed", err.Error(), false)
		return &ManagedArchiveReportResult{Status: "failed", Endpoint: endpoint, Error: err.Error()}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		_ = r.markCabinetSync(fvID, "failed", err.Error(), false)
		return &ManagedArchiveReportResult{Status: "failed", Endpoint: endpoint, Error: err.Error()}, err
	}
	req.Header.Set("Content-Type", "application/json")
	// manage_token 已废弃，仅用 KeySyncToken
	if token := strings.TrimSpace(r.configRepo.GetValue(KeySyncToken)); token != "" {
		req.Header.Set("X-Sync-Token", token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		msg := fmt.Sprintf("请求失败：%v", err)
		_ = r.markCabinetSync(fvID, "failed", msg, false)
		return &ManagedArchiveReportResult{Status: "failed", Endpoint: endpoint, Error: msg}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("manage 返回 %d: %s", resp.StatusCode, string(respBody))
		_ = r.markCabinetSync(fvID, "failed", msg, false)
		return &ManagedArchiveReportResult{Status: "failed", Endpoint: endpoint, Error: msg}, fmt.Errorf("%s", msg)
	}

	var rsp struct {
		Code    *int   `json:"code"`
		Success bool   `json:"success"`
		Message string `json:"message"`
		Error   string `json:"error"`
		Data    struct {
			StorageTier     string `json:"storage_tier"`
			StorageLocation string `json:"storage_location"`
		} `json:"data"`
	}
	_ = json.Unmarshal(respBody, &rsp)
	ok := rsp.Success || (rsp.Code != nil && *rsp.Code == 0)
	if !ok {
		msg := rsp.Error
		if msg == "" {
			msg = rsp.Message
		}
		if msg == "" {
			msg = string(respBody)
		}
		_ = r.markCabinetSync(fvID, "failed", "manage 拒绝："+msg, false)
		return &ManagedArchiveReportResult{Status: "failed", Endpoint: endpoint, Error: msg}, fmt.Errorf("%s", msg)
	}

	msg := "manage 部门柜已接收"
	if rsp.Message != "" {
		msg = rsp.Message
	}
	_ = r.markCabinetSync(fvID, "success", msg, true)
	return &ManagedArchiveReportResult{
		Status:          "success",
		Endpoint:        endpoint,
		Reply:           msg,
		StorageTier:     rsp.Data.StorageTier,
		StorageLocation: rsp.Data.StorageLocation,
	}, nil
}

func (r *ManagedArchiveReporter) markCabinetSync(fvID int64, status, message string, synced bool) error {
	now := time.Now()
	if synced {
		_, err := r.DB.Exec(`UPDATE file_versions
			SET cabinet_sync_status = ?, cabinet_sync_message = ?, cabinet_synced_at = ?, update_time = ?
			WHERE id = ?`, status, message, now, now, fvID)
		return err
	}
	_, err := r.DB.Exec(`UPDATE file_versions
		SET cabinet_sync_status = ?, cabinet_sync_message = ?, update_time = ?
		WHERE id = ?`, status, message, now, fvID)
	return err
}

func (r *ManagedArchiveReporter) buildFileArchivePayload(fvID int64) (map[string]interface{}, error) {
	fvRepo := NewFileVersionRepository(r.DB)
	projRepo := NewDataProjectRepository(r.DB)
	stageRepo := NewProjectStageRepository(r.DB)
	ledgerRepo := NewAssetLedgerRepository(r.DB)
	eventRepo := NewLifecycleEventRepository(r.DB)

	fv, err := fvRepo.FindByID(fvID)
	if err != nil {
		return nil, err
	}
	if fv.DataState != "output" {
		return nil, fmt.Errorf("部门柜上报仅支持 output 文件版本，当前 %s", fv.DataState)
	}
	if fv.LifecycleStatus != "registered" {
		return nil, fmt.Errorf("部门柜上报仅支持 registered 文件版本，当前 %s", fv.LifecycleStatus)
	}
	if fv.SubmittedAt == nil {
		return nil, fmt.Errorf("部门柜上报仅支持已提交的 output 文件版本")
	}

	project, err := projRepo.FindByID(fv.ProjectID)
	if err != nil {
		return nil, err
	}
	decision := DecideFileVersionArchiveTarget(project, fv)
	if decision.Action == ArchiveActionNoSync {
		return nil, fmt.Errorf("%w: %s", ErrCabinetSyncSkipped, decision.Reason)
	}
	if decision.Action != ArchiveActionSync {
		return nil, fmt.Errorf("归档目标需人工确认：%s", decision.Reason)
	}
	stage, err := stageRepo.FindByID(fv.ProjectStageID)
	if err != nil {
		return nil, err
	}
	ledger, err := ledgerRepo.FindByFileVersion(fv.ID)
	if err != nil {
		return nil, err
	}
	events, _ := eventRepo.ListByFileVersion(fv.ID)
	var latestEvent map[string]interface{}
	if len(events) > 0 {
		e := events[len(events)-1]
		latestEvent = map[string]interface{}{
			"event_type":       e.EventType,
			"event_name":       e.EventName,
			"file_version_id":  e.FileVersionID,
			"operator_id":      ptrString(e.OperatorID),
			"operator_user_id": ptrInt64(e.OperatorUserID),
			"reason":           ptrString(e.Reason),
			"create_time":      e.CreateTime.Format(time.RFC3339),
		}
		if e.LedgerID != nil {
			latestEvent["ledger_id"] = *e.LedgerID
		}
	}

	fileVersion := map[string]interface{}{
		"file_version_code":  fv.FileVersionCode,
		"local_code":         fv.LocalCode,
		"display_name":       fv.DisplayName,
		"stage_code":         stage.StageCode,
		"data_state":         fv.DataState,
		"version_no":         fv.VersionNo,
		"required":           fv.Required,
		"lifecycle_status":   fv.LifecycleStatus,
		"submitted_at":       fv.SubmittedAt.Format(time.RFC3339),
		"submitted_by":       ptrString(fv.SubmittedBy),
		"source_storage_uri": ptrString(fv.StorageURI),
	}
	if fv.FileType != nil {
		fileVersion["file_type"] = *fv.FileType
	}
	if fv.Checksum != nil {
		fileVersion["checksum"] = *fv.Checksum
	}
	if fv.FileSize != nil {
		fileVersion["file_size"] = *fv.FileSize
	}
	if fv.OriginalFileName != nil {
		fileVersion["original_file_name"] = *fv.OriginalFileName
	}
	if fv.SubmittedByUserID != nil {
		fileVersion["submitted_by_user_id"] = *fv.SubmittedByUserID
	}

	projectSubjects := r.subjectCodes(project.OwnerSubjectID, project.CustodianSubjectID, project.SecuritySubjectID)

	return map[string]interface{}{
		"schema":          "data-asset-scan/file-archive-v1",
		"archive_phase":   decision.ArchivePhase,
		"source_terminal": "data-asset-scan",
		"generated_at":    time.Now().Format(time.RFC3339),
		"archive_decision": map[string]interface{}{
			"action":        decision.Action,
			"target_tier":   decision.TargetTier,
			"file_state":    decision.FileState,
			"storage_label": decision.StorageLabel,
		},
		"project": map[string]interface{}{
			"project_code":           project.ProjectCode,
			"project_name":           project.ProjectName,
			"template_code":          project.TemplateCode,
			"template_version":       project.TemplateVersion,
			"sensitivity_level":      project.SensitivityLevel,
			"management_mode":        project.ManagementMode,
			"owner_subject_id":       project.OwnerSubjectID,
			"custodian_subject_id":   project.CustodianSubjectID,
			"security_subject_id":    project.SecuritySubjectID,
			"owner_subject_code":     projectSubjects[project.OwnerSubjectID],
			"custodian_subject_code": projectSubjects[project.CustodianSubjectID],
			"security_subject_code":  projectSubjects[project.SecuritySubjectID],
			"project_root":           ptrString(project.ProjectRoot),
		},
		"stage": map[string]interface{}{
			"stage_code": stage.StageCode,
			"stage_name": stage.StageName,
			"stage_type": stage.StageType,
			"sort_order": stage.SortOrder,
			"status":     stage.Status,
		},
		"file_version":    fileVersion,
		"ledger":          r.ledgerToArchiveMap(ledger),
		"lifecycle_event": latestEvent,
	}, nil
}

func (r *ManagedArchiveReporter) ledgerToArchiveMap(ledger *models.AssetLedger) map[string]interface{} {
	subjects := r.subjectCodes(ledger.OwnerSubjectID, ledger.CustodianSubjectID, ledger.SecuritySubjectID)
	return map[string]interface{}{
		"ledger_code":            ledger.LedgerCode,
		"file_version_code":      ledger.FileVersionCode,
		"asset_name":             ledger.AssetName,
		"stage_code":             ledger.StageCode,
		"sensitivity_level":      ledger.SensitivityLevel,
		"marking_method":         ledger.MarkingMethod,
		"lifecycle_status":       ledger.LifecycleStatus,
		"owner_subject_id":       ledger.OwnerSubjectID,
		"custodian_subject_id":   ledger.CustodianSubjectID,
		"security_subject_id":    ledger.SecuritySubjectID,
		"owner_subject_code":     subjects[ledger.OwnerSubjectID],
		"custodian_subject_code": subjects[ledger.CustodianSubjectID],
		"security_subject_code":  subjects[ledger.SecuritySubjectID],
	}
}

func (r *ManagedArchiveReporter) subjectCodes(ids ...int64) map[int64]string {
	result := map[int64]string{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := result[id]; ok {
			continue
		}
		var code string
		if err := r.DB.Get(&code, `SELECT code FROM subjects WHERE id = ? AND disable = 0`, id); err == nil {
			result[id] = code
		}
	}
	return result
}

func ptrString(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func ptrInt64(i *int64) interface{} {
	if i == nil {
		return nil
	}
	return *i
}
