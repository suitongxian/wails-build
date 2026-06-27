package repository

import (
	"strconv"
	"strings"
	"time"

	"data-asset-scan-go/internal/models"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// SystemConfigRepository handles database operations for system_config table
type SystemConfigRepository struct {
	DB *sqlx.DB
}

// SystemConfigKey constants
const (
	KeyFullInventoryTime        = "FULL_INVENTORY_TIME"
	KeySaveCode                 = "save_code"
	KeyControlType              = "control_type"
	KeyDailyScanInterval        = "daily_scan_interval"
	KeyWorkspace                = "workspace"
	KeyLastScanTime             = "last_scan_time"
	KeyScanAreaPath             = "scan_area_path"
	KeyScanExcludeDir           = "scan_exclude_dir"
	KeyUploadServerURL          = "upload_server_url"
	KeyLastSyncTime             = "last_sync_time"
	KeyAllTerminalUsers         = "all_terminal_users"
	KeyClaimFamilyDefaultPolicy = "claim_family_default_policy"
	KeyClaimFamilySkipDialog    = "claim_family_skip_dialog"
	KeyFeaturePrecomputeEnabled = "feature_precompute_enabled"
	// KeyFamilyDirty 标记「家族表是否与当前文件集合不一致」。
	// 1 = 需要重建（扫描后置 1；未运行过分析也视为 1）
	// 0 = 与最近一次成功分析后无变化
	KeyFamilyDirty = "similarity_family_dirty"
	// KeyScanInstanceID 本终端稳定实例标识：首次生成 UUID 并持久化，作为向 manage 提交立项时的
	// scan_endpoint，使不同终端/重装互不串号（避免本地自增 id 重排撞上 manage 旧记录）。
	KeyScanInstanceID = "scan_instance_id"
	// KeyPersonalArchiveRoot 个人级一键归档落点根目录（个人保密/档案/资料夹建在其下）。
	// 默认就是工作空间根；如需改到别处可配置此项覆盖。
	KeyPersonalArchiveRoot = "personal_archive_root"
)

// ClaimFamily policy constants
const (
	ClaimFamilyPolicySameContentOnly = "same_content_only"
	ClaimFamilyPolicyAll             = "all"
	ClaimFamilyPolicyNone            = "none"
)

// NewSystemConfigRepository creates a new SystemConfigRepository
func NewSystemConfigRepository(db *sqlx.DB) *SystemConfigRepository {
	return &SystemConfigRepository{DB: db}
}

// GetByKey retrieves a config by key
func (r *SystemConfigRepository) GetByKey(key string) (*models.SystemConfig, error) {
	var config models.SystemConfig
	query := `SELECT * FROM system_config WHERE key = ? AND disable = 0`
	err := r.DB.Get(&config, query, key)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// GetValue retrieves a config value by key
func (r *SystemConfigRepository) GetValue(key string) string {
	config, err := r.GetByKey(key)
	if err != nil || config == nil || config.Value == nil || *config.Value == "" {
		return ""
	}
	return *config.Value
}

// EnsureScanInstanceID 返回本终端稳定实例标识（首次调用生成 UUID 并持久化）。
// 用作向 manage 提交立项的 scan_endpoint：不同终端/重装各自唯一，配合 scan_origin_id
// 让去重键全局稳定，避免本地 id 重排撞上 manage 旧记录。
func (r *SystemConfigRepository) EnsureScanInstanceID() string {
	if v := strings.TrimSpace(r.GetValue(KeyScanInstanceID)); v != "" {
		return v
	}
	id := "scan-" + uuid.NewString()
	r.SetValue(KeyScanInstanceID, id)
	return id
}

// SetValue sets or creates a config value
func (r *SystemConfigRepository) SetValue(key, value string) {
	now := time.Now()

	// Check if exists
	existing, _ := r.GetByKey(key)
	if existing != nil {
		query := `UPDATE system_config SET value = ?, update_time = ? WHERE key = ? AND disable = 0`
		r.DB.Exec(query, value, now, key)
	} else {
		query := `INSERT INTO system_config (key, type, value, create_time, update_time, disable) VALUES (?, 'string', ?, ?, ?, 0)`
		r.DB.Exec(query, key, value, now, now)
	}
}

// Exists checks if a config key exists
func (r *SystemConfigRepository) Exists(key string) bool {
	_, err := r.GetByKey(key)
	return err == nil
}

// GetFullInventoryTime returns the full inventory time
func (r *SystemConfigRepository) GetFullInventoryTime() string {
	return r.GetValue(KeyFullInventoryTime)
}

// SetFullInventoryTime sets the full inventory time
func (r *SystemConfigRepository) SetFullInventoryTime(timeStr string) {
	r.SetValue(KeyFullInventoryTime, timeStr)
}

// HasFullInventory checks if full inventory has been performed
func (r *SystemConfigRepository) HasFullInventory() bool {
	return r.Exists(KeyFullInventoryTime)
}

// GetSaveCode returns the save code
func (r *SystemConfigRepository) GetSaveCode() string {
	return r.GetValue(KeySaveCode)
}

// SetSaveCode sets the save code
func (r *SystemConfigRepository) SetSaveCode(code string) {
	r.SetValue(KeySaveCode, code)
}

// VerifySaveCode verifies if the provided code matches
func (r *SystemConfigRepository) VerifySaveCode(code string) bool {
	storedCode := r.GetSaveCode()
	return storedCode != "" && storedCode == code
}

// GetWorkspace returns the workspace path
func (r *SystemConfigRepository) GetWorkspace() string {
	return r.GetValue(KeyWorkspace)
}

// SetWorkspace sets the workspace path
func (r *SystemConfigRepository) SetWorkspace(path string) {
	r.SetValue(KeyWorkspace, path)
}

// GetEffectiveProjectRoot 数据业务模版的"项目根目录"统一取自 workspace —— 上层 UI
// 已合并为单一字段。仅当 workspace 未配置时退回到旧 KeyProjectRoot（向后兼容
// 老数据库 / 升级期），保留 data_projects 表上已立项项目快照路径不动。
func (r *SystemConfigRepository) GetEffectiveProjectRoot() string {
	if ws := strings.TrimSpace(r.GetWorkspace()); ws != "" {
		return ws
	}
	return r.GetValue(KeyProjectRoot)
}

// GetEffectivePersonalArchiveRoot 个人级一键归档落点根目录：
// 默认就放在工作空间下（个人保密/档案/资料夹建在工作空间根下）；
// 如显式配置 personal_archive_root 则用配置值（便于将来改到别处）。
func (r *SystemConfigRepository) GetEffectivePersonalArchiveRoot() string {
	if v := strings.TrimSpace(r.GetValue(KeyPersonalArchiveRoot)); v != "" {
		return v
	}
	return r.GetEffectiveProjectRoot()
}

// DefaultServerEndpoint 服务端默认地址（文件上传 / 服务端 / 归档上报合并后）
const DefaultServerEndpoint = "http://47.95.233.47:19091"

// GetEffectiveServerEndpoint 上层 UI 把「文件上传服务器地址」「服务端地址」「归档上报地址」
// 合并为单一「服务端地址」。三个旧 key 都仍可能存有值（升级期/老调用方），按优先级取第一个
// 非空：KeyManageEndpoint > KeyUploadServerURL > KeyArchiveEndpoint > DefaultServerEndpoint。
// SaveConfig 时会把同一个值写入三个 key，保证读哪个都得到一致值。
func (r *SystemConfigRepository) GetEffectiveServerEndpoint() string {
	for _, key := range []string{KeyManageEndpoint, KeyUploadServerURL, KeyArchiveEndpoint} {
		if v := strings.TrimSpace(r.GetValue(key)); v != "" {
			return v
		}
	}
	return DefaultServerEndpoint
}

// DefaultTemplateServerEndpoint 模版管理平台默认地址（template-manage，:19092），用于「同步远程模版」。
const DefaultTemplateServerEndpoint = "http://47.95.233.47:19092"

// GetEffectiveTemplateServerEndpoint 返回模版管理平台地址；未配置时回退默认值。
// 这是与「上报数据/文件」的 manage 地址（GetEffectiveServerEndpoint）分离的第二台服务器。
func (r *SystemConfigRepository) GetEffectiveTemplateServerEndpoint() string {
	if v := strings.TrimSpace(r.GetValue(KeyTemplateServerEndpoint)); v != "" {
		return v
	}
	return DefaultTemplateServerEndpoint
}

// GetLastScanTime returns the last scan time
func (r *SystemConfigRepository) GetLastScanTime() string {
	return r.GetValue(KeyLastScanTime)
}

// SetLastScanTime sets the last scan time
func (r *SystemConfigRepository) SetLastScanTime(timeStr string) {
	r.SetValue(KeyLastScanTime, timeStr)
}

// GetDailyScanInterval returns the daily scan interval in minutes
func (r *SystemConfigRepository) GetDailyScanInterval() int {
	value := r.GetValue(KeyDailyScanInterval)
	if value == "" {
		return 15 // Default 15 minutes
	}
	var result int
	for _, c := range value {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		}
	}
	if result == 0 {
		return 15
	}
	return result
}

// SetDailyScanInterval sets the daily scan interval in minutes
func (r *SystemConfigRepository) SetDailyScanInterval(minutes int) {
	r.SetValue(KeyDailyScanInterval, strconv.Itoa(minutes))
}

// GetScanAreaPath returns the scan area path
func (r *SystemConfigRepository) GetScanAreaPath() string {
	return r.GetValue(KeyScanAreaPath)
}

// SetScanAreaPath sets the scan area path
func (r *SystemConfigRepository) SetScanAreaPath(path string) {
	r.SetValue(KeyScanAreaPath, path)
}

// GetScanExcludeDir returns the scan exclude directories
func (r *SystemConfigRepository) GetScanExcludeDir() string {
	return r.GetValue(KeyScanExcludeDir)
}

// SetScanExcludeDir sets the scan exclude directories
func (r *SystemConfigRepository) SetScanExcludeDir(dirs string) {
	r.SetValue(KeyScanExcludeDir, dirs)
}

// GetControlType returns the control type
func (r *SystemConfigRepository) GetControlType() string {
	return r.GetValue(KeyControlType)
}

// SetControlType sets the control type
func (r *SystemConfigRepository) SetControlType(ctype string) {
	r.SetValue(KeyControlType, ctype)
}

// GetUploadServerURL returns the upload server URL
func (r *SystemConfigRepository) GetUploadServerURL() string {
	return r.GetValue(KeyUploadServerURL)
}

// SetUploadServerURL sets the upload server URL
func (r *SystemConfigRepository) SetUploadServerURL(url string) {
	r.SetValue(KeyUploadServerURL, url)
}

// GetLastSyncTime returns the last sync time
func (r *SystemConfigRepository) GetLastSyncTime() string {
	return r.GetValue(KeyLastSyncTime)
}

// SetLastSyncTime sets the last sync time
func (r *SystemConfigRepository) SetLastSyncTime(timeStr string) {
	r.SetValue(KeyLastSyncTime, timeStr)
}

// GetAllTerminalUsers returns all terminal users JSON
func (r *SystemConfigRepository) GetAllTerminalUsers() string {
	return r.GetValue(KeyAllTerminalUsers)
}

// SetAllTerminalUsers sets all terminal users JSON
func (r *SystemConfigRepository) SetAllTerminalUsers(jsonValue string) {
	r.SetValue(KeyAllTerminalUsers, jsonValue)
}

// Get retrieves a config value by key, returning (value, error).
// Returns ("", sql.ErrNoRows) when the key does not exist.
// Callers that only need a best-effort string should use GetValue instead.
func (r *SystemConfigRepository) Get(key string) (string, error) {
	cfg, err := r.GetByKey(key)
	if err != nil {
		return "", err
	}
	if cfg.Value == nil {
		return "", nil
	}
	return *cfg.Value, nil
}

// Set creates or updates a config value by key. Errors are swallowed (same as SetValue).
func (r *SystemConfigRepository) Set(key, value string) error {
	r.SetValue(key, value)
	return nil
}

// GetAll retrieves all config records
func (r *SystemConfigRepository) GetAll() ([]models.SystemConfig, error) {
	var configs []models.SystemConfig
	query := `SELECT * FROM system_config WHERE disable = 0`
	err := r.DB.Select(&configs, query)
	if err != nil {
		return nil, err
	}
	return configs, nil
}
