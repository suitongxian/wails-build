package repository

import (
	"database/sql"
	"log"
	"strings"
	"time"

	"data-asset-scan-go/internal/models"
	"github.com/jmoiron/sqlx"
)

// dataResourcesColumns 是 data_resources 表中模型映射的列清单
//
// 显式列出而不是 SELECT *，避免 DB 实际有 struct 没有的列时触发
// sqlx 严格模式 'missing destination name xxx' 错误。
// 顺序与 models.DataResources 字段一致。
const dataResourcesColumns = `data_resources_id, content_sign, source_count, workspace_source_count,
	first_create_time, resources_name, resources_desc, content_subject, content_type,
	is_claimed, claim_status, importance_level, claim_time, claimant_name, claimant_unit,
	data_level, data_share, file_magic, family_id, family_relation, family_score,
	COALESCE((SELECT COUNT(*) FROM data_resources fam WHERE fam.family_id = data_resources.family_id AND fam.disable = 0), 0) AS family_member_count,
	COALESCE((SELECT COUNT(*) FROM data_resources fam WHERE fam.family_id = data_resources.family_id AND fam.family_relation = 'same_content' AND fam.disable = 0), 0) AS family_same_content_count,
	COALESCE((SELECT COUNT(*) FROM data_resources fam WHERE fam.family_id = data_resources.family_id AND fam.family_relation = 'process_version' AND fam.disable = 0), 0) AS family_process_version_count,
	COALESCE((SELECT COUNT(*) FROM data_resources fam WHERE fam.family_id = data_resources.family_id AND fam.family_relation = 'derived' AND fam.disable = 0), 0) AS family_derived_count,
	create_time, update_time, disable`

// DataResourcesRepository handles database operations for data_resources table
type DataResourcesRepository struct {
	DB        *sqlx.DB
	BatchSize int
}

// NewDataResourcesRepository creates a new DataResourcesRepository
func NewDataResourcesRepository(db *sqlx.DB, batchSize int) *DataResourcesRepository {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &DataResourcesRepository{DB: db, BatchSize: batchSize}
}

// currentDataOrigin 返回 INSERT 时应填的 data_origin 值。
// baseline_completed_at 为空 → 'historical'（首次普查仍在进行）；否则 'new'。
// 单次 INSERT 批次内只查一次。
func currentDataOrigin(db *sqlx.DB) string {
	var value *string
	err := db.Get(&value, `SELECT value FROM system_config WHERE key = 'baseline_completed_at' AND disable = 0`)
	if err != nil || value == nil || *value == "" {
		return "historical"
	}
	return "new"
}

// InsertBatch inserts multiple records in a batch
func (r *DataResourcesRepository) InsertBatch(records []map[string]interface{}) int {
	if len(records) == 0 {
		return 0
	}

	now := time.Now()
	origin := currentDataOrigin(r.DB)
	tx := r.DB.MustBegin()

	stmt := `
		INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, resources_desc, content_subject, content_type, file_magic,
			create_time, update_time, disable, data_origin
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
	`

	for _, record := range records {
		contentSign := getStringFromMapR(record, "content_sign", "")
		sourceCount := getIntFromMapR(record, "source_count", 1)
		workspaceSourceCount := getIntFromMapR(record, "workspace_source_count", 0)
		firstCreateTime := getStringFromMapR(record, "first_create_time", now.Format(time.RFC3339))
		resourcesName := getStringPtrFromMapR(record, "resources_name")
		resourcesDesc := getStringPtrFromMapR(record, "resources_desc")
		contentSubject := getStringPtrFromMapR(record, "content_subject")
		contentType := getStringPtrFromMapR(record, "content_type")
		fileMagic := getStringPtrFromMapR(record, "file_magic")

		tx.MustExec(stmt,
			contentSign, sourceCount, workspaceSourceCount, firstCreateTime,
			resourcesName, resourcesDesc, contentSubject, contentType, fileMagic,
			now, now, origin,
		)
	}

	err := tx.Commit()
	if err != nil {
		return 0
	}
	return len(records)
}

// InsertFromStatistics inserts records from MD5 statistics map
func (r *DataResourcesRepository) InsertFromStatistics(statsMap map[string]interface{}) int {
	if len(statsMap) == 0 {
		return 0
	}

	now := time.Now()
	origin := currentDataOrigin(r.DB)
	tx := r.DB.MustBegin()

	stmt := `
		INSERT INTO data_resources (
			content_sign, source_count, workspace_source_count, first_create_time,
			resources_name, content_subject, content_type, file_magic,
			create_time, update_time, disable, data_origin
		) VALUES (?, ?, ?, ?, ?, 'file', ?, ?, ?, ?, 0, ?)
	`

	for contentSign, statsVal := range statsMap {
		stats, ok := statsVal.(*MD5Stats)
		if !ok {
			continue
		}

		contentType := ""
		if stats.FirstFileName != "" {
			if extIdx := strings.LastIndex(stats.FirstFileName, "."); extIdx > 0 && extIdx < len(stats.FirstFileName)-1 {
				contentType = strings.ToLower(stats.FirstFileName[extIdx+1:])
			}
		}

		tx.MustExec(stmt,
			contentSign, stats.SourceCount, stats.WorkspaceSourceCount, stats.FirstCreateTime,
			stats.ShortFileName, contentType, stats.FileMagic,
			now, now, origin,
		)
	}

	err := tx.Commit()
	if err != nil {
		return 0
	}
	return len(statsMap)
}

// Truncate deletes all records (for full inventory reset)
func (r *DataResourcesRepository) Truncate() int {
	result, err := r.DB.Exec("DELETE FROM data_resources")
	if err != nil {
		return 0
	}
	changes, _ := result.RowsAffected()
	return int(changes)
}

// GetActive retrieves all active records
func (r *DataResourcesRepository) GetActive() ([]models.DataResources, error) {
	var records []models.DataResources
	query := `SELECT ` + dataResourcesColumns + ` FROM data_resources WHERE disable = 0`
	err := r.DB.Select(&records, query)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// GetActiveByContentSignMap returns all active records as a map keyed by content_sign
func (r *DataResourcesRepository) GetActiveByContentSignMap() map[string]models.DataResources {
	var records []models.DataResources
	query := `SELECT ` + dataResourcesColumns + ` FROM data_resources WHERE disable = 0`
	err := r.DB.Select(&records, query)
	if err != nil {
		return make(map[string]models.DataResources)
	}

	result := make(map[string]models.DataResources, len(records))
	for _, record := range records {
		result[record.ContentSign] = record
	}
	return result
}

// IncrementSourceCount increments the source_count for a content sign
func (r *DataResourcesRepository) IncrementSourceCount(contentSign string, increment int64) {
	now := time.Now()
	query := `UPDATE data_resources SET source_count = source_count + ?, update_time = ? WHERE content_sign = ? AND disable = 0`
	r.DB.Exec(query, increment, now, contentSign)
}

// IncrementWorkspaceSourceCount increments the workspace_source_count for a content sign
func (r *DataResourcesRepository) IncrementWorkspaceSourceCount(contentSign string, increment int64) {
	now := time.Now()
	query := `UPDATE data_resources SET workspace_source_count = workspace_source_count + ?, update_time = ? WHERE content_sign = ? AND disable = 0`
	r.DB.Exec(query, increment, now, contentSign)
}

// BatchUpdateForDeletedFiles updates resources for deleted files
func (r *DataResourcesRepository) BatchUpdateForDeletedFiles(updates []map[string]interface{}) {
	if len(updates) == 0 {
		return
	}

	now := time.Now()
	tx := r.DB.MustBegin()

	stmtSourceOnly := `
		UPDATE data_resources
		SET source_count = MAX(0, source_count - 1), update_time = ?
		WHERE content_sign = ? AND disable = 0
	`
	stmtBoth := `
		UPDATE data_resources
		SET source_count = MAX(0, source_count - 1),
		    workspace_source_count = MAX(0, workspace_source_count - 1),
		    update_time = ?
		WHERE content_sign = ? AND disable = 0
	`

	for _, update := range updates {
		contentSign := getStringFromMapR(update, "content_sign", "")
		isFromWorkspace := getBoolFromMapR(update, "is_from_workspace", false)

		if isFromWorkspace {
			tx.MustExec(stmtBoth, now, contentSign)
		} else {
			tx.MustExec(stmtSourceOnly, now, contentSign)
		}
	}

	tx.Commit()
}

// BatchUpdateForModifiedFiles updates resources for modified files
func (r *DataResourcesRepository) BatchUpdateForModifiedFiles(updates []map[string]interface{}, existingResources map[string]models.DataResources) {
	if len(updates) == 0 {
		return
	}

	now := time.Now()
	tx := r.DB.MustBegin()

	stmtDecrementSourceOnly := `
		UPDATE data_resources
		SET source_count = MAX(0, source_count - 1), update_time = ?
		WHERE content_sign = ? AND disable = 0
	`
	stmtDecrementBoth := `
		UPDATE data_resources
		SET source_count = MAX(0, source_count - 1),
		    workspace_source_count = MAX(0, workspace_source_count - 1),
		    update_time = ?
		WHERE content_sign = ? AND disable = 0
	`
	stmtIncrementSourceOnly := `
		UPDATE data_resources
		SET source_count = source_count + 1, update_time = ?
		WHERE content_sign = ? AND disable = 0
	`
	stmtIncrementBoth := `
		UPDATE data_resources
		SET source_count = source_count + 1,
		    workspace_source_count = workspace_source_count + 1,
		    update_time = ?
		WHERE content_sign = ? AND disable = 0
	`

	newResourcesMap := make(map[string]*MD5Stats)

	for _, update := range updates {
		oldContentSign := getStringFromMapR(update, "old_content_sign", "")
		newContentSign := getStringFromMapR(update, "new_content_sign", "")
		isFromWorkspace := getBoolFromMapR(update, "is_from_workspace", false)

		// Decrement old content sign
		if isFromWorkspace {
			tx.MustExec(stmtDecrementBoth, now, oldContentSign)
		} else {
			tx.MustExec(stmtDecrementSourceOnly, now, oldContentSign)
		}

		// Check if new content sign exists
		if existingRecord, ok := existingResources[newContentSign]; ok {
			// Increment new content sign
			if isFromWorkspace {
				tx.MustExec(stmtIncrementBoth, now, newContentSign)
			} else {
				tx.MustExec(stmtIncrementSourceOnly, now, newContentSign)
			}
			_ = existingRecord
		} else {
			// Collect for new resource creation
			fileCreateTime := getStringFromMapR(update, "file_create_time", now.Format(time.RFC3339))
			fileMagicVal := getStringFromMapR(update, "file_magic", "")
			fileName := getStringFromMapR(update, "file_name", "")

			if existing, ok := newResourcesMap[newContentSign]; ok {
				existing.SourceCount++
				if isFromWorkspace {
					existing.WorkspaceSourceCount++
				}
				if fileCreateTime < existing.FirstCreateTime {
					existing.FirstCreateTime = fileCreateTime
					existing.FirstFileName = fileName
				}
				if len(fileName) < len(existing.ShortFileName) {
					existing.ShortFileName = fileName
				}
			} else {
				newResourcesMap[newContentSign] = &MD5Stats{
					ContentSign:          newContentSign,
					SourceCount:          1,
					WorkspaceSourceCount: 0,
					FirstCreateTime:      fileCreateTime,
					FileMagic:            fileMagicVal,
					FirstFileName:        fileName,
					ShortFileName:        fileName,
				}
				if isFromWorkspace {
					newResourcesMap[newContentSign].WorkspaceSourceCount = 1
				}
			}
		}
	}

	tx.Commit()

	// Insert new resources
	if len(newResourcesMap) > 0 {
		r.InsertFromStatistics(convertMD5StatsMap(newResourcesMap))
	}
}

// MD5Stats represents MD5 statistics for internal use
type MD5Stats struct {
	ContentSign          string
	SourceCount          int
	WorkspaceSourceCount int
	FirstCreateTime      string
	FileMagic            string
	FirstFileName        string
	ShortFileName        string
}

func convertMD5StatsMap(m map[string]*MD5Stats) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		result[k] = v
	}
	return result
}

// GetByContentSign retrieves a resource by content sign
func (r *DataResourcesRepository) GetByContentSign(contentSign string) (*models.DataResources, error) {
	var record models.DataResources
	query := `SELECT ` + dataResourcesColumns + ` FROM data_resources WHERE content_sign = ? AND disable = 0`
	err := r.DB.Get(&record, query, contentSign)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// Count returns the total count of active records
func (r *DataResourcesRepository) Count() int {
	var result struct {
		Count int `db:"count"`
	}
	query := `SELECT COUNT(*) as count FROM data_resources WHERE disable = 0`
	err := r.DB.Get(&result, query)
	if err != nil {
		return 0
	}
	return result.Count
}

// Helper functions for map access
func getStringFromMapR(params map[string]interface{}, key, defaultVal string) string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getStringPtrFromMapR(params map[string]interface{}, key string) *string {
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return &s
		}
	}
	return nil
}

func getIntFromMapR(params map[string]interface{}, key string, defaultVal int) int {
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

func getBoolFromMapR(params map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := params[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

// DataResourcesQueryParams represents query parameters for resources pagination
type DataResourcesQueryParams struct {
	Page                  int
	PageSize              int
	ClaimStatusFilter     *int
	ClaimStatusIn         []int
	ImportanceLevelFilter *int
	Search                *string
	BusinessTypeFilter    *string
	FullInventoryTime     *string
	GroupByFamily         bool // when true, fold each family into its primary row
}

// ResourcesPageResult represents paginated resources result
type ResourcesPageResult struct {
	Resources []models.DataResourcesWithPrimaryPath `json:"resources"`
	Total     int                                   `json:"total"`
	Page      int                                   `json:"page"`
	PageSize  int                                   `json:"pageSize"`
}

// BatchClaimParams represents batch claim parameters
type BatchClaimParams struct {
	IDs          []int64
	IsClaimed    int
	ClaimStatus  int
	ClaimantName string
	ClaimantUnit string
}

// GetResourcesWithPagination retrieves paginated resources
func (r *DataResourcesRepository) GetResourcesWithPagination(params DataResourcesQueryParams) ResourcesPageResult {
	page := params.Page
	if page <= 0 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	whereClause := "WHERE disable = 0"
	args := []interface{}{}

	if params.ClaimStatusFilter != nil && *params.ClaimStatusFilter >= 0 {
		whereClause += " AND claim_status = ?"
		args = append(args, *params.ClaimStatusFilter)
	}

	if params.ClaimStatusIn != nil && len(params.ClaimStatusIn) > 0 {
		placeholders := make([]string, len(params.ClaimStatusIn))
		for i, v := range params.ClaimStatusIn {
			placeholders[i] = "?"
			args = append(args, v)
		}
		whereClause += " AND claim_status IN (" + strings.Join(placeholders, ",") + ")"
	}

	if params.ImportanceLevelFilter != nil && *params.ImportanceLevelFilter >= 0 {
		whereClause += " AND importance_level = ?"
		args = append(args, *params.ImportanceLevelFilter)
	}

	if params.Search != nil && *params.Search != "" {
		whereClause += " AND resources_name LIKE ?"
		args = append(args, "%"+*params.Search+"%")
	}

	if params.GroupByFamily {
		// Fold each family into its primary row: keep rows with no family OR
		// rows that are explicitly marked primary.
		whereClause += " AND (family_id IS NULL OR family_relation = 'primary')"
	}

	// 业务来源过滤（语义与 FilesView/GetFiles 对齐）：
	//   workspace        = 工作空间内有副本，与是否设过 full_inventory_time 无关
	//   new_access       = 首次普查后新登记（first_create_time > full_inventory_time），
	//                      若未设普查时间则 passthrough（前端 tab 切了不能强制空）
	//   history_inventory= 首次普查前历史（first_create_time < full_inventory_time），
	//                      若未设普查时间则返空（"历史"没有意义）
	if params.BusinessTypeFilter != nil {
		switch *params.BusinessTypeFilter {
		case "workspace":
			whereClause += " AND workspace_source_count > 0"
		case "new_access":
			if params.FullInventoryTime != nil {
				whereClause += " AND first_create_time > ?"
				args = append(args, *params.FullInventoryTime)
			}
		case "history_inventory":
			if params.FullInventoryTime != nil {
				whereClause += " AND first_create_time < ?"
				args = append(args, *params.FullInventoryTime)
			} else {
				whereClause += " AND 1 = 0"
			}
		}
	}

	// Count total
	var total int
	if err := r.DB.Get(&total, "SELECT COUNT(*) as count FROM data_resources "+whereClause, args...); err != nil {
		log.Printf("[resources] count query failed: %v", err)
	}

	// Get page data
	//
	// !! 必须显式列名，不能 SELECT * !!
	// sqlx 默认严格模式：DB 多一列没在 struct 里也会让 Select 失败。
	// 历史上 data_resources 表可能被外部迁移/旧代码加过额外列（如 project_id），
	// 用 SELECT * 会触发 'missing destination name xxx' 错误。
	// primary_path 子查询：同 content_sign 在 data_distributing 里最早入库的那条路径。
	// 用作责任认领 UI 的"代表性物理路径"（hover tooltip 完整显示 + 副本弹窗剔除避免重复展示）。
	// suspect_non_personal：只要同 content_sign 任一条 distributing 行被打了 suspect，整个 resource 算 suspect。
	query := "SELECT " + dataResourcesColumns + ",\n" +
		"\t\t(SELECT path FROM data_distributing WHERE content_sign = data_resources.content_sign AND disable = 0 ORDER BY data_distribution_id ASC LIMIT 1) AS primary_path,\n" +
		"\t\tCOALESCE((SELECT MAX(suspect_non_personal) FROM data_distributing WHERE content_sign = data_resources.content_sign AND disable = 0), 0) AS suspect_non_personal\n" +
		"\tFROM data_resources " + whereClause + " ORDER BY first_create_time DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset)
	var resources []models.DataResourcesWithPrimaryPath
	if err := r.DB.Select(&resources, query, args...); err != nil {
		log.Printf("[resources] select query failed: %v\n  query: %s\n  args: %+v", err, query, args)
	}
	if resources == nil {
		resources = []models.DataResourcesWithPrimaryPath{}
	}

	return ResourcesPageResult{
		Resources: resources,
		Total:     total,
		Page:      page,
		PageSize:  pageSize,
	}
}

// ResourcesStatistics represents resources statistics
type ResourcesStatistics struct {
	TotalFileCount                 int `json:"totalFileCount"`
	WorkspaceTotalCount            int `json:"workspaceTotalCount"`
	HistoryFileCount               int `json:"historyFileCount"`
	NonHistoryFileCount            int `json:"nonHistoryFileCount"`
	WorkspaceClaimedCount          int `json:"workspaceClaimedCount"`
	HistoryClaimedCount            int `json:"historyClaimedCount"`
	NonHistoryClaimedCount         int `json:"nonHistoryClaimedCount"`
	WorkspacePendingClassifyCount  int `json:"workspacePendingClassifyCount"`
	HistoryPendingClassifyCount    int `json:"historyPendingClassifyCount"`
	NonHistoryPendingClassifyCount int `json:"nonHistoryPendingClassifyCount"`
	UnclassifiedCount              int `json:"unclassifiedCount"`
	CoreCount                      int `json:"coreCount"`
	ImportantCount                 int `json:"importantCount"`
	OpenCount                      int `json:"openCount"`
	PrivacyCount                   int `json:"privacyCount"`
}

// GetResourcesStatistics retrieves resources statistics
func (r *DataResourcesRepository) GetResourcesStatistics(fullInventoryTime *string) ResourcesStatistics {
	stats := ResourcesStatistics{}

	// Base stats
	row := r.DB.QueryRowx(`
		SELECT
			COALESCE(SUM(source_count), 0) as totalFileCount,
			COALESCE(SUM(workspace_source_count), 0) as workspaceTotalCount,
			COALESCE(SUM(CASE WHEN claim_status > 0 THEN workspace_source_count ELSE 0 END), 0) as workspaceClaimedCount,
			COALESCE(SUM(CASE WHEN importance_level = 0 THEN source_count ELSE 0 END), 0) as unclassifiedCount,
			COALESCE(SUM(CASE WHEN importance_level = 1 THEN source_count ELSE 0 END), 0) as coreCount,
			COALESCE(SUM(CASE WHEN importance_level = 2 THEN source_count ELSE 0 END), 0) as importantCount,
			COALESCE(SUM(CASE WHEN importance_level = 3 THEN source_count ELSE 0 END), 0) as openCount,
			COALESCE(SUM(CASE WHEN importance_level = 4 THEN source_count ELSE 0 END), 0) as privacyCount
		FROM data_resources WHERE disable = 0`)

	var totalFileCount, workspaceTotalCount, workspaceClaimedCount, unclassifiedCount, coreCount, importantCount, openCount, privacyCount int
	row.Scan(&totalFileCount, &workspaceTotalCount, &workspaceClaimedCount, &unclassifiedCount, &coreCount, &importantCount, &openCount, &privacyCount)

	stats.TotalFileCount = totalFileCount
	stats.WorkspaceTotalCount = workspaceTotalCount
	stats.WorkspaceClaimedCount = workspaceClaimedCount
	stats.UnclassifiedCount = unclassifiedCount
	stats.CoreCount = coreCount
	stats.ImportantCount = importantCount
	stats.OpenCount = openCount
	stats.PrivacyCount = privacyCount

	// Pending classify count
	var pendingCount int
	r.DB.Get(&pendingCount, `SELECT COUNT(*) FROM data_resources WHERE disable = 0 AND claim_status = 2 AND importance_level = 0 AND workspace_source_count > 0`)
	stats.WorkspacePendingClassifyCount = pendingCount

	if fullInventoryTime == nil || *fullInventoryTime == "" {
		stats.HistoryFileCount = -1
		stats.NonHistoryFileCount = -1
		stats.HistoryClaimedCount = -1
		stats.NonHistoryClaimedCount = -1
		stats.HistoryPendingClassifyCount = -1
		stats.NonHistoryPendingClassifyCount = -1
		return stats
	}

	// With full inventory time
	histRow := r.DB.QueryRowx(`
		SELECT
			COALESCE(SUM(CASE WHEN first_create_time < ? THEN source_count ELSE 0 END), 0) as historyFileCount,
			COALESCE(SUM(CASE WHEN first_create_time > ? THEN source_count ELSE 0 END), 0) as nonHistoryFileCount,
			COALESCE(SUM(CASE WHEN first_create_time < ? AND claim_status > 0 THEN source_count ELSE 0 END), 0) as historyClaimedCount,
			COALESCE(SUM(CASE WHEN first_create_time > ? AND claim_status > 0 THEN source_count ELSE 0 END), 0) as nonHistoryClaimedCount,
			COALESCE(COUNT(CASE WHEN first_create_time < ? AND claim_status = 2 AND importance_level = 0 THEN 1 END), 0) as historyPendingClassifyCount,
			COALESCE(COUNT(CASE WHEN first_create_time > ? AND claim_status = 2 AND importance_level = 0 THEN 1 END), 0) as nonHistoryPendingClassifyCount
		FROM data_resources WHERE disable = 0`,
		*fullInventoryTime, *fullInventoryTime, *fullInventoryTime, *fullInventoryTime, *fullInventoryTime, *fullInventoryTime)

	histRow.Scan(&stats.HistoryFileCount, &stats.NonHistoryFileCount, &stats.HistoryClaimedCount, &stats.NonHistoryClaimedCount, &stats.HistoryPendingClassifyCount, &stats.NonHistoryPendingClassifyCount)

	return stats
}

// BatchClaim batch updates claim status for resources
func (r *DataResourcesRepository) BatchClaim(params BatchClaimParams) int {
	if len(params.IDs) == 0 {
		return 0
	}

	now := time.Now()
	autoImportanceLevel := -1
	if params.ClaimStatus == 1 {
		autoImportanceLevel = 4 // privacy
	}

	tx := r.DB.MustBegin()
	defer tx.Rollback()

	var updated int
	for _, id := range params.IDs {
		var result sql.Result
		if autoImportanceLevel >= 0 {
			result, _ = tx.Exec(`
				UPDATE data_resources SET is_claimed = ?, claim_status = ?, claim_time = ?, claimant_name = ?, claimant_unit = ?, importance_level = ?, update_time = ?
				WHERE data_resources_id = ? AND disable = 0`,
				params.IsClaimed, params.ClaimStatus, now, params.ClaimantName, params.ClaimantUnit, autoImportanceLevel, now, id)
		} else {
			result, _ = tx.Exec(`
				UPDATE data_resources SET is_claimed = ?, claim_status = ?, claim_time = ?, claimant_name = ?, claimant_unit = ?, update_time = ?
				WHERE data_resources_id = ? AND disable = 0`,
				params.IsClaimed, params.ClaimStatus, now, params.ClaimantName, params.ClaimantUnit, now, id)
		}
		changes, _ := result.RowsAffected()
		updated += int(changes)
	}

	tx.Commit()
	return updated
}

// BatchClassify batch updates importance level for resources
func (r *DataResourcesRepository) BatchClassify(ids []int64, importanceLevel int) int {
	if len(ids) == 0 {
		return 0
	}

	now := time.Now()
	placeholders := make([]string, len(ids))
	args := []interface{}{importanceLevel, now}
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := "UPDATE data_resources SET importance_level = ?, update_time = ? WHERE data_resources_id IN (" + strings.Join(placeholders, ",") + ") AND disable = 0"
	result, err := r.DB.Exec(query, args...)
	if err != nil {
		return 0
	}
	changes, _ := result.RowsAffected()
	return int(changes)
}

// ClassifyResource updates a single resource with classification info
func (r *DataResourcesRepository) ClassifyResource(id int64, importanceLevel int, resourcesName, resourcesDesc, contentSubject *string) error {
	now := time.Now()
	query := `UPDATE data_resources SET importance_level = ?, update_time = ?`
	args := []interface{}{importanceLevel, now}

	if resourcesName != nil {
		query += `, resources_name = ?`
		args = append(args, *resourcesName)
	}
	if resourcesDesc != nil {
		query += `, resources_desc = ?`
		args = append(args, *resourcesDesc)
	}
	if contentSubject != nil {
		query += `, content_subject = ?`
		args = append(args, *contentSubject)
	}

	query += ` WHERE data_resources_id = ? AND disable = 0`
	args = append(args, id)

	_, err := r.DB.Exec(query, args...)
	return err
}

// ResourceArchiveInfo 认领归档保护复制到个人文件夹所需的资源信息。
type ResourceArchiveInfo struct {
	PrimaryPath    string `db:"primary_path"`
	ContentSubject string `db:"content_subject"`
	ResourcesName  string `db:"resources_name"`
}

// GetResourceArchiveInfo 取某资源的代表性文件路径（同 content_sign 最早入库那条）
// 及工作事项/资源名，供「认领归档保护」复制实体到个人{级别}文件夹时定位与分组。
func (r *DataResourcesRepository) GetResourceArchiveInfo(id int64) (ResourceArchiveInfo, error) {
	var info ResourceArchiveInfo
	err := r.DB.Get(&info, `
		SELECT
			COALESCE((
				SELECT path FROM data_distributing
				WHERE content_sign = dr.content_sign AND disable = 0
				ORDER BY data_distribution_id ASC LIMIT 1
			), '') AS primary_path,
			COALESCE(dr.content_subject, '') AS content_subject,
			COALESCE(dr.resources_name, '')  AS resources_name
		FROM data_resources dr
		WHERE dr.data_resources_id = ? AND dr.disable = 0`, id)
	return info, err
}

// SuspectFilter scopes the suspect query/update to a specific business tab
// (workspace / new_access / history_inventory)，与 GetResources 同源。
type SuspectFilter struct {
	BusinessType      *string // workspace / new_access / history_inventory (nil = no tab filter)
	FullInventoryTime *string // for new_access / history_inventory
}

// suspectWhereClause 构造 suspect 资源筛选公共 WHERE 部分。
// 命中条件：
//   - data_resources.disable=0
//   - claim_status=0（未认领；不覆盖用户手动决定）
//   - 至少一条 distributing 行 suspect_non_personal=1
//   - 可选 business tab 过滤（与 GetResources 同语义）
//
// 返回 (whereSQL, args)。
func suspectWhereClause(filter SuspectFilter) (string, []interface{}) {
	where := `WHERE dr.disable = 0
		AND dr.claim_status = 0
		AND EXISTS (
			SELECT 1 FROM data_distributing dd
			WHERE dd.content_sign = dr.content_sign
			  AND dd.disable = 0
			  AND dd.suspect_non_personal = 1
		)`
	args := []interface{}{}
	if filter.BusinessType != nil {
		switch *filter.BusinessType {
		case "workspace":
			where += " AND dr.workspace_source_count > 0"
		case "new_access":
			if filter.FullInventoryTime != nil {
				where += " AND dr.first_create_time > ?"
				args = append(args, *filter.FullInventoryTime)
			}
		case "history_inventory":
			if filter.FullInventoryTime != nil {
				where += " AND dr.first_create_time < ?"
				args = append(args, *filter.FullInventoryTime)
			} else {
				where += " AND 1 = 0"
			}
		}
	}
	return where, args
}

// SuspectSummary 返回 suspect 资源总数 + 前 N 个样本路径，给前端弹确认对话框用。
func (r *DataResourcesRepository) SuspectSummary(filter SuspectFilter, sampleSize int) (int, []string, error) {
	where, args := suspectWhereClause(filter)

	var count int
	if err := r.DB.Get(&count, "SELECT COUNT(*) FROM data_resources dr "+where, args...); err != nil {
		return 0, nil, err
	}
	if count == 0 || sampleSize <= 0 {
		return count, nil, nil
	}

	sampleQ := `SELECT (
			SELECT path FROM data_distributing
			WHERE content_sign = dr.content_sign AND disable = 0
			ORDER BY data_distribution_id ASC LIMIT 1
		) AS path
		FROM data_resources dr ` + where + ` ORDER BY dr.first_create_time DESC LIMIT ?`
	args2 := append([]interface{}{}, args...)
	args2 = append(args2, sampleSize)

	var samples []string
	if err := r.DB.Select(&samples, sampleQ, args2...); err != nil {
		return count, nil, err
	}
	// 过滤 NULL / 空串
	clean := samples[:0]
	for _, s := range samples {
		if s != "" {
			clean = append(clean, s)
		}
	}
	return count, clean, nil
}

// IgnoreAllSuspect 把 suspect 资源批量置 claim_status=4（已忽略）。
// 等价于用户挨个点「标为已忽略」。返回受影响行数。
func (r *DataResourcesRepository) IgnoreAllSuspect(filter SuspectFilter, claimantName, claimantUnit string) (int, error) {
	where, args := suspectWhereClause(filter)
	now := time.Now()

	// SQLite UPDATE ... WHERE ... 不直接支持别名 dr，
	// 用 IN 子查询绕开：先取出所有命中的 data_resources_id。
	idQ := "SELECT dr.data_resources_id FROM data_resources dr " + where
	var ids []int64
	if err := r.DB.Select(&ids, idQ, args...); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	tx := r.DB.MustBegin()
	defer tx.Rollback()
	var updated int
	for _, id := range ids {
		res, err := tx.Exec(`UPDATE data_resources
			SET is_claimed = 1, claim_status = 4, claim_time = ?,
			    claimant_name = ?, claimant_unit = ?, update_time = ?
			WHERE data_resources_id = ? AND claim_status = 0 AND disable = 0`,
			now, claimantName, claimantUnit, now, id)
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		updated += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return updated, nil
}

// PendingSyncRecord is the projection used by /sync/source.
// source_ip / source_mac are filled in at query time from the local host.
type PendingSyncRecord struct {
	DataResourcesID int64      `db:"data_resources_id"`
	ContentSign     string     `db:"content_sign"`
	SourceCount     int        `db:"source_count"`
	UpdateTime      time.Time  `db:"update_time"`
	FirstCreateTime time.Time  `db:"first_create_time"`
	ContentSubject  *string    `db:"content_subject"`
	ContentType     *string    `db:"content_type"`
	FileMagic       *string    `db:"file_magic"`
	ClaimStatus     int        `db:"claim_status"`
	ClaimTime       *time.Time `db:"claim_time"`
	ImportanceLevel int        `db:"importance_level"`
	DataShare       *string    `db:"data_share"`
}

// CountPendingSyncRecords returns the number of data_resources rows whose
// update_time is strictly after sinceTime (or all rows if sinceTime is empty).
func (r *DataResourcesRepository) CountPendingSyncRecords(sinceTime string) (int, error) {
	query := `SELECT COUNT(*) FROM data_resources WHERE disable = 0`
	args := []interface{}{}
	if sinceTime != "" {
		query += ` AND update_time > ?`
		args = append(args, sinceTime)
	}
	var n int
	err := r.DB.Get(&n, query, args...)
	return n, err
}

// GetPendingSyncRecords pages through data_resources rows that need syncing.
func (r *DataResourcesRepository) GetPendingSyncRecords(sinceTime string, limit, offset int) ([]PendingSyncRecord, error) {
	query := `
		SELECT
			data_resources_id, content_sign, source_count, update_time,
			first_create_time, content_subject, content_type, file_magic,
			claim_status, claim_time, importance_level, data_share
		FROM data_resources
		WHERE disable = 0`
	args := []interface{}{}
	if sinceTime != "" {
		query += ` AND update_time > ?`
		args = append(args, sinceTime)
	}
	query += ` ORDER BY update_time ASC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	var records []PendingSyncRecord
	err := r.DB.Select(&records, query, args...)
	return records, err
}
