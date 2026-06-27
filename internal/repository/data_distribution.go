package repository

import (
	"database/sql"
	"strings"
	"time"

	"data-asset-scan-go/internal/models"
	"github.com/jmoiron/sqlx"
)

// DataDistributingRepository handles database operations for data_distributing table
type DataDistributingRepository struct {
	DB        *sqlx.DB
	BatchSize int
}

// NewDataDistributingRepository creates a new DataDistributingRepository
func NewDataDistributingRepository(db *sqlx.DB, batchSize int) *DataDistributingRepository {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &DataDistributingRepository{DB: db, BatchSize: batchSize}
}

// InsertBatch inserts multiple records in a batch
func (r *DataDistributingRepository) InsertBatch(records []map[string]interface{}) int {
	if len(records) == 0 {
		return 0
	}

	now := time.Now()
	tx := r.DB.MustBegin()

	stmt := `
		INSERT INTO data_distributing (
			scan_task_id, path, data_type, scan_found_count, content_sign, file_suffix,
			file_magic, file_create_time, file_update_time, file_read_time,
			file_size, file_hide, ip, mac_address, parent_id, scan_time,
			create_time, update_time, disable,
			simhash, content_hash, extracted_text, feature_mtime, feature_size, phash,
			suspect_non_personal
		) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?)
	`

	for _, record := range records {
		scanTaskID := getInt64PtrFromMap(record, "scan_task_id")
		path := getStringFromMap(record, "path", "")
		dataType := getIntFromMap(record, "data_type", 1)
		contentSign := getStringFromMap(record, "content_sign", "")
		fileSuffix := getStringPtrFromMap(record, "file_suffix")
		fileMagic := getStringPtrFromMap(record, "file_magic")
		fileCreateTime := getTimePtrFromMap(record, "file_create_time")
		fileUpdateTime := getTimePtrFromMap(record, "file_update_time")
		fileReadTime := getTimePtrFromMap(record, "file_read_time")
		fileSize := getInt64FromMap(record, "file_size", 0)
		fileHide := getIntFromMap(record, "file_hide", 0)
		ip := getStringFromMap(record, "ip", "")
		macAddress := getStringFromMap(record, "mac_address", "")
		parentID := getInt64PtrFromMap(record, "parent_id")
		scanTime := getTimePtrFromMap(record, "scan_time")
		simhash := getInt64PtrFromMap(record, "simhash")
		contentHash := getStringPtrFromMap(record, "content_hash")
		extractedText := getStringPtrFromMap(record, "extracted_text")
		featureMtime := getTimePtrFromMap(record, "feature_mtime")
		featureSize := getInt64PtrFromMap(record, "feature_size")
		phash := getStringPtrFromMap(record, "phash")
		suspect := getIntFromMap(record, "suspect_non_personal", 0)

		tx.MustExec(stmt,
			scanTaskID, path, dataType, contentSign, fileSuffix,
			fileMagic, fileCreateTime, fileUpdateTime, fileReadTime,
			fileSize, fileHide, ip, macAddress, parentID, scanTime,
			now, now,
			simhash, contentHash, extractedText, featureMtime, featureSize, phash,
			suspect,
		)
	}

	err := tx.Commit()
	if err != nil {
		return 0
	}
	return len(records)
}

// Truncate deletes all records (for full inventory reset)
func (r *DataDistributingRepository) Truncate() int {
	result, err := r.DB.Exec("DELETE FROM data_distributing")
	if err != nil {
		return 0
	}
	changes, _ := result.RowsAffected()
	return int(changes)
}

// GetActive retrieves all active records
func (r *DataDistributingRepository) GetActive() ([]models.DataDistribution, error) {
	var records []models.DataDistribution
	query := `SELECT * FROM data_distributing WHERE disable = 0`
	err := r.DB.Select(&records, query)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// GetActiveByPathMap returns all active records as a map keyed by path
func (r *DataDistributingRepository) GetActiveByPathMap() map[string]models.DataDistribution {
	var records []models.DataDistribution
	query := `SELECT * FROM data_distributing WHERE disable = 0`
	err := r.DB.Select(&records, query)
	if err != nil {
		return make(map[string]models.DataDistribution)
	}

	result := make(map[string]models.DataDistribution, len(records))
	for _, record := range records {
		result[record.Path] = record
	}
	return result
}

// GetActiveByPathMapWithPrefix returns active records with path prefix as a map
func (r *DataDistributingRepository) GetActiveByPathMapWithPrefix(pathPrefix string) map[string]models.DataDistribution {
	separator := "/"
	if strings.Contains(pathPrefix, "\\") {
		separator = "\\"
	}
	normalizedPrefix := pathPrefix
	if !strings.HasSuffix(normalizedPrefix, separator) {
		normalizedPrefix += separator
	}

	query := `SELECT * FROM data_distributing WHERE disable = 0 AND (path LIKE ? OR path = ?)`
	var records []models.DataDistribution
	err := r.DB.Select(&records, query, normalizedPrefix+"%", strings.TrimSuffix(pathPrefix, "/\\"))
	if err != nil {
		return make(map[string]models.DataDistribution)
	}

	result := make(map[string]models.DataDistribution, len(records))
	for _, record := range records {
		result[record.Path] = record
	}
	return result
}

// BatchIncrementScanFoundCount increments scan_found_count for multiple records
func (r *DataDistributingRepository) BatchIncrementScanFoundCount(ids []int64) int {
	if len(ids) == 0 {
		return 0
	}

	now := time.Now()
	placeholders := make([]string, len(ids))
	values := make([]interface{}, len(ids)+1)
	values[0] = now
	for i, id := range ids {
		placeholders[i] = "?"
		values[i+1] = id
	}

	query := `UPDATE data_distributing SET scan_found_count = scan_found_count + 1, update_time = ? WHERE data_distribution_id IN (` + strings.Join(placeholders, ",") + `)`
	result, err := r.DB.Exec(query, values...)
	if err != nil {
		return 0
	}
	changes, _ := result.RowsAffected()
	return int(changes)
}

// BatchMarkAsDeleted marks records as deleted by setting scan_found_count = 0
func (r *DataDistributingRepository) BatchMarkAsDeleted(ids []int64) int {
	if len(ids) == 0 {
		return 0
	}

	now := time.Now()
	placeholders := make([]string, len(ids))
	values := make([]interface{}, len(ids)+1)
	values[0] = now
	for i, id := range ids {
		placeholders[i] = "?"
		values[i+1] = id
	}

	query := `UPDATE data_distributing SET scan_found_count = 0, update_time = ? WHERE data_distribution_id IN (` + strings.Join(placeholders, ",") + `)`
	result, err := r.DB.Exec(query, values...)
	if err != nil {
		return 0
	}
	changes, _ := result.RowsAffected()
	return int(changes)
}

// BatchUpdateModifiedFiles updates records for modified files
func (r *DataDistributingRepository) BatchUpdateModifiedFiles(updates []map[string]interface{}) int {
	if len(updates) == 0 {
		return 0
	}

	now := time.Now()
	tx := r.DB.MustBegin()

	query := `
		UPDATE data_distributing
		SET content_sign = ?, file_update_time = ?, file_read_time = ?, file_size = ?, file_magic = ?,
		    scan_found_count = 1, update_time = ?
		WHERE data_distribution_id = ?
	`

	for _, update := range updates {
		dataDistributionID := getInt64FromMap(update, "data_distribution_id", 0)
		contentSign := getStringFromMap(update, "content_sign", "")
		fileUpdateTime := getStringPtrFromMap(update, "file_update_time")
		fileReadTime := getStringPtrFromMap(update, "file_read_time")
		fileSize := getInt64FromMap(update, "file_size", 0)
		fileMagic := getStringPtrFromMap(update, "file_magic")

		tx.MustExec(query, contentSign, fileUpdateTime, fileReadTime, fileSize, fileMagic, now, dataDistributionID)
	}

	err := tx.Commit()
	if err != nil {
		return 0
	}
	return len(updates)
}

// GetByContentSign retrieves records by content sign
func (r *DataDistributingRepository) GetByContentSign(contentSign string) ([]models.DataDistribution, error) {
	var records []models.DataDistribution
	query := `SELECT * FROM data_distributing WHERE content_sign = ? AND disable = 0 ORDER BY update_time DESC`
	err := r.DB.Select(&records, query, contentSign)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// Count returns the total count of active records
func (r *DataDistributingRepository) Count() int {
	var result struct {
		Count int `db:"count"`
	}
	query := `SELECT COUNT(*) as count FROM data_distributing WHERE disable = 0`
	err := r.DB.Get(&result, query)
	if err != nil {
		return 0
	}
	return result.Count
}

// Helper functions for map access
func getStringFromMap(params map[string]interface{}, key, defaultVal string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getStringPtrFromMap(params map[string]interface{}, key string) *string {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case string:
			return &val
		case *string:
			return val
		}
	}
	return nil
}

func getIntFromMap(params map[string]interface{}, key string, defaultVal int) int {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		}
	}
	return defaultVal
}

func getInt64FromMap(params map[string]interface{}, key string, defaultVal int64) int64 {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case int:
			return int64(val)
		case int64:
			return val
		}
	}
	return defaultVal
}

func getInt64PtrFromMap(params map[string]interface{}, key string) *int64 {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case int:
			v2 := int64(val)
			return &v2
		case int64:
			return &val
		case *int64:
			return val
		}
	}
	return nil
}

func getTimePtrFromMap(params map[string]interface{}, key string) *time.Time {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case *time.Time:
			return val
		case time.Time:
			return &val
		case string:
			if t, err := time.Parse(time.RFC3339, val); err == nil {
				return &t
			}
		}
	}
	return nil
}

// Fix sqlx import usage
var _ = sql.ErrNoRows

// ArchiveQueryOptions represents query options for archive files
type ArchiveQueryOptions struct {
	Page                  int
	PageSize              int
	Search                string
	ArchiveType           string // 'pending' | 'core' | 'important' | 'open'
	ImportanceLevelFilter *int
}

// FileWithCopyCount represents a file with its copy count
type FileWithCopyCount struct {
	models.DataDistribution `db:",inline"`
	CopyCount               int  `json:"copy_count" db:"copy_count"`
	ImportanceLevel         *int `json:"importance_level,omitempty" db:"importance_level"`
}

// ArchiveFileResult represents archive file query result
type ArchiveFileResult struct {
	Files []FileWithCopyCount `json:"files"`
	Total int                 `json:"total"`
}

// GetArchiveFiles retrieves paginated archive files
func (r *DataDistributingRepository) GetArchiveFiles(options ArchiveQueryOptions) ArchiveFileResult {
	page := options.Page
	if page <= 0 {
		page = 1
	}
	pageSize := options.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	conditions := []string{"d.disable = 0"}
	args := []interface{}{}

	if options.Search != "" {
		conditions = append(conditions, "d.path LIKE ?")
		args = append(args, "%"+options.Search+"%")
	}

	archiveType := options.ArchiveType
	if archiveType == "" {
		archiveType = "pending"
	}

	switch archiveType {
	case "pending":
		conditions = append(conditions, "d.upload_state = 0")
		if options.ImportanceLevelFilter != nil {
			conditions = append(conditions, "r.importance_level = ?")
			args = append(args, *options.ImportanceLevelFilter)
		} else {
			conditions = append(conditions, "r.importance_level IN (1, 2, 3)")
		}
	case "core":
		conditions = append(conditions, "d.upload_state IN (1, 2)")
		conditions = append(conditions, "r.importance_level = 1")
	case "important":
		conditions = append(conditions, "d.upload_state IN (1, 2)")
		conditions = append(conditions, "r.importance_level = 2")
	case "open":
		conditions = append(conditions, "d.upload_state IN (1, 2)")
		conditions = append(conditions, "r.importance_level = 3")
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	var total int
	countQuery := "SELECT COUNT(*) as count FROM data_distributing d LEFT JOIN data_resources r ON d.content_sign = r.content_sign WHERE " + whereClause
	r.DB.Get(&total, countQuery, args...)

	// Get page data
	dataQuery := `
		SELECT d.*, COALESCE(r.source_count, 0) as copy_count, r.importance_level
		FROM data_distributing d
		LEFT JOIN data_resources r ON d.content_sign = r.content_sign
		WHERE ` + whereClause + `
		ORDER BY d.update_time DESC
		LIMIT ? OFFSET ?`
	args = append(args, pageSize, offset)

	files := []FileWithCopyCount{}
	r.DB.Select(&files, dataQuery, args...)

	return ArchiveFileResult{Files: files, Total: total}
}

// BatchUpdateToNoArchive marks files as no archive needed
func (r *DataDistributingRepository) BatchUpdateToNoArchive(ids []int64) int {
	if len(ids) == 0 {
		return 0
	}

	now := time.Now()
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = now
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := "UPDATE data_distributing SET upload_state = 4, update_time = ? WHERE data_distribution_id IN (" + strings.Join(placeholders, ",") + ")"
	result, err := r.DB.Exec(query, args...)
	if err != nil {
		return 0
	}
	changes, _ := result.RowsAffected()
	return int(changes)
}
