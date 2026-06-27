package repository

import (
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// AssetLedgerRepository asset_ledgers 表
type AssetLedgerRepository struct {
	DB *sqlx.DB
}

func NewAssetLedgerRepository(db *sqlx.DB) *AssetLedgerRepository {
	return &AssetLedgerRepository{DB: db}
}

// CreateLedgerInput 创建底账入参
type CreateLedgerInput struct {
	LedgerCode         string
	FileVersionID      int64
	ClassCode          *string
	ProjectCode        string
	StageCode          string
	FileVersionCode    string
	AssetName          string
	ContentSummary     *string
	OwnerSubjectID     int64
	CustodianSubjectID int64
	SecuritySubjectID  int64
	SensitivityLevel   string
	MarkingMethod      string // reference / embedded / hybrid
	SourceRef          *string
	CurrentStorageURI  *string
	LifecycleStatus    string // planned / registered / ...
}

// Insert 在事务内插入底账
func (r *AssetLedgerRepository) Insert(tx sqlx.Ext, in CreateLedgerInput) (int64, error) {
	now := time.Now()
	res, err := tx.Exec(`INSERT INTO asset_ledgers (
		ledger_code, file_version_id, class_code, project_code, stage_code, file_version_code,
		asset_name, content_summary, owner_subject_id, custodian_subject_id, security_subject_id,
		sensitivity_level, marking_method, source_ref, current_storage_uri, lifecycle_status,
		create_time, update_time, disable
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		in.LedgerCode, in.FileVersionID, in.ClassCode, in.ProjectCode, in.StageCode, in.FileVersionCode,
		in.AssetName, in.ContentSummary, in.OwnerSubjectID, in.CustodianSubjectID, in.SecuritySubjectID,
		in.SensitivityLevel, in.MarkingMethod, in.SourceRef, in.CurrentStorageURI, in.LifecycleStatus,
		now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindByID 按本地 id 查找
func (r *AssetLedgerRepository) FindByID(id int64) (*models.AssetLedger, error) {
	var l models.AssetLedger
	if err := r.DB.Get(&l, `SELECT * FROM asset_ledgers WHERE id = ? AND disable = 0`, id); err != nil {
		return nil, err
	}
	return &l, nil
}

// FindByCode 按底账编号查找
func (r *AssetLedgerRepository) FindByCode(code string) (*models.AssetLedger, error) {
	var l models.AssetLedger
	if err := r.DB.Get(&l, `SELECT * FROM asset_ledgers WHERE ledger_code = ? AND disable = 0`, code); err != nil {
		return nil, err
	}
	return &l, nil
}

// FindByFileVersion 按文件版本查找底账
func (r *AssetLedgerRepository) FindByFileVersion(fileVersionID int64) (*models.AssetLedger, error) {
	var l models.AssetLedger
	if err := r.DB.Get(&l, `SELECT * FROM asset_ledgers WHERE file_version_id = ? AND disable = 0`, fileVersionID); err != nil {
		return nil, err
	}
	return &l, nil
}

// SearchInput 底账查询入参
type LedgerSearchInput struct {
	ProjectCode      string
	StageCode        string
	SensitivityLevel string
	OwnerSubjectID   int64
	LifecycleStatus  string
	Keyword          string
}

// Search 多维度筛选底账
func (r *AssetLedgerRepository) Search(in LedgerSearchInput) ([]models.AssetLedger, error) {
	q := `SELECT * FROM asset_ledgers WHERE disable = 0`
	args := []interface{}{}
	if in.ProjectCode != "" {
		q += ` AND project_code = ?`
		args = append(args, in.ProjectCode)
	}
	if in.StageCode != "" {
		q += ` AND stage_code = ?`
		args = append(args, in.StageCode)
	}
	if in.SensitivityLevel != "" {
		q += ` AND sensitivity_level = ?`
		args = append(args, in.SensitivityLevel)
	}
	if in.OwnerSubjectID != 0 {
		q += ` AND owner_subject_id = ?`
		args = append(args, in.OwnerSubjectID)
	}
	if in.LifecycleStatus != "" {
		q += ` AND lifecycle_status = ?`
		args = append(args, in.LifecycleStatus)
	}
	if in.Keyword != "" {
		q += ` AND (asset_name LIKE ? OR ledger_code LIKE ? OR file_version_code LIKE ?)`
		kw := "%" + in.Keyword + "%"
		args = append(args, kw, kw, kw)
	}
	q += ` ORDER BY update_time DESC LIMIT 1000`
	var list []models.AssetLedger
	if err := r.DB.Select(&list, q, args...); err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateBinding 文件正式入账后同步底账（绑定 storage_uri 和切换状态）
func (r *AssetLedgerRepository) UpdateBinding(id int64, storageURI string, lifecycleStatus string) error {
	_, err := r.DB.Exec(`UPDATE asset_ledgers SET current_storage_uri = ?, lifecycle_status = ?, update_time = ? WHERE id = ?`,
		storageURI, lifecycleStatus, time.Now(), id)
	return err
}

// UpdateLifecycleStatus 更新生命周期状态
func (r *AssetLedgerRepository) UpdateLifecycleStatus(tx sqlx.Ext, id int64, status string) error {
	_, err := tx.Exec(`UPDATE asset_ledgers SET lifecycle_status = ?, update_time = ? WHERE id = ?`, status, time.Now(), id)
	return err
}

// GenerateLedgerCode 生成底账编号
//
// 格式：LDG-{YYYYMM}-{NNNNNN}
//
// 在事务内调用确保编号原子性。
func GenerateLedgerCode(tx sqlx.Ext, refTime time.Time) (string, error) {
	prefix := refTime.Format("LDG-200601-")
	row := tx.QueryRowx(`SELECT ledger_code FROM asset_ledgers WHERE ledger_code LIKE ? AND disable = 0 ORDER BY ledger_code DESC LIMIT 1`, prefix+"%")
	var last string
	maxSeq := 0
	if err := row.Scan(&last); err == nil && last != "" {
		// 解析最后一段
		seqStr := last[len(prefix):]
		var n int
		_, _ = fmtSscanf(seqStr, &n)
		maxSeq = n
	}
	return prefix + zeroPad(maxSeq+1, 6), nil
}

// fmtSscanf 解析无前导零的整数（避免引入 fmt 在 hot path）
func fmtSscanf(s string, out *int) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return n, nil
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return n, nil
}

// zeroPad 0-pad an int to width
func zeroPad(n, width int) string {
	s := []byte{}
	if n == 0 {
		s = []byte{'0'}
	} else {
		for n > 0 {
			s = append([]byte{'0' + byte(n%10)}, s...)
			n /= 10
		}
	}
	for len(s) < width {
		s = append([]byte{'0'}, s...)
	}
	return string(s)
}
