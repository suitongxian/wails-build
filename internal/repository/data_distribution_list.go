package repository

import (
	"fmt"
	"strings"

	"data-asset-scan-go/internal/models"
)

// FilesListOptions 描述 ListFilesWithFilters 接受的查询参数。
type FilesListOptions struct {
	Search            string // 路径子串模糊匹配，空字符串 = 不过滤
	WorkspacePath     string // 工作空间根路径（已 trim trailing slash）
	WorkspaceFilter   string // inside | outside | all (空字符串视作 all)
	SurvivalFilter    string // new | deleted | normal | all
	AccessTimeFilter  string // new | history | all
	FullInventoryTime string // RFC3339 字符串；空表示未做过普查
	Page              int    // 从 1 开始
	PageSize          int    // 每页条数
}

// FileRow data_distributing 一行 + 联表算出的 copy_count
type FileRow struct {
	models.DataDistribution `db:",inline"`
	CopyCount               int `db:"copy_count"`
}

// ListFilesWithFilters 返回符合过滤条件的 data_distributing 行（含 copy_count）+ 总数
// (post-filter, pre-pagination)。失败返回 err。
func (r *DataDistributingRepository) ListFilesWithFilters(opts FilesListOptions) ([]FileRow, int64, error) {
	whereClauses := []string{"d.disable = 0"}
	args := []interface{}{}

	// search: case-insensitive substring 匹配
	if s := strings.TrimSpace(opts.Search); s != "" {
		whereClauses = append(whereClauses, "LOWER(d.path) LIKE LOWER(?) ESCAPE '\\'")
		args = append(args, "%"+escapeLikePattern(s)+"%")
	}

	// workspace filter（inside/outside）按 path 前缀匹配
	if opts.WorkspacePath != "" {
		ws := strings.TrimRight(opts.WorkspacePath, "/\\")
		switch opts.WorkspaceFilter {
		case "inside":
			whereClauses = append(whereClauses, "(d.path = ? OR d.path LIKE ? ESCAPE '\\')")
			args = append(args, ws, escapeLikePattern(ws)+"/%")
		case "outside":
			whereClauses = append(whereClauses, "NOT (d.path = ? OR d.path LIKE ? ESCAPE '\\')")
			args = append(args, ws, escapeLikePattern(ws)+"/%")
		}
	}

	// survival filter
	switch opts.SurvivalFilter {
	case "new":
		whereClauses = append(whereClauses, "d.scan_found_count = 1")
	case "deleted":
		whereClauses = append(whereClauses, "d.scan_found_count = 0")
	case "normal":
		whereClauses = append(whereClauses, "d.scan_found_count > 0")
	}

	// access time filter
	// SQLite 时间列存储格式为 "2026-01-01 00:00:00+00:00"，与 RFC3339 "2026-01-01T00:00:00Z"
	// 直接做字符串比较会因格式差异（空格 vs T、+00:00 vs Z）导致结果错误。
	// 用 datetime() 函数统一转换后再比较，避免格式问题。
	if opts.AccessTimeFilter == "new" {
		if opts.FullInventoryTime != "" {
			whereClauses = append(whereClauses, "d.file_create_time IS NOT NULL AND datetime(d.file_create_time) >= datetime(?)")
			args = append(args, opts.FullInventoryTime)
		}
		// 无普查时间：passthrough，与前端旧行为对齐
	} else if opts.AccessTimeFilter == "history" {
		if opts.FullInventoryTime != "" {
			whereClauses = append(whereClauses, "(d.file_create_time IS NULL OR datetime(d.file_create_time) < datetime(?))")
			args = append(args, opts.FullInventoryTime)
		} else {
			// 无普查时间：返空
			whereClauses = append(whereClauses, "1 = 0")
		}
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	// total
	var total int64
	countQuery := "SELECT COUNT(*) FROM data_distributing d WHERE " + whereSQL
	if err := r.DB.Get(&total, countQuery, args...); err != nil {
		return nil, 0, fmt.Errorf("count files: %w", err)
	}

	// page rows + copy_count via LEFT JOIN data_resources
	// NULLS LAST 通过 (file_create_time IS NULL) 排序键实现，避免 SQLite 老版本不支持 NULLS LAST
	limit := opts.PageSize
	if limit <= 0 {
		limit = 50
	}
	page := opts.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	listQuery := `
		SELECT d.*, COALESCE(r.source_count, 0) AS copy_count
		FROM data_distributing d
		LEFT JOIN data_resources r ON d.content_sign = r.content_sign AND r.disable = 0
		WHERE ` + whereSQL + `
		ORDER BY (d.file_create_time IS NULL), d.file_create_time DESC, d.data_distribution_id DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append([]interface{}{}, args...)
	listArgs = append(listArgs, limit, offset)

	rows := []FileRow{}
	if err := r.DB.Select(&rows, listQuery, listArgs...); err != nil {
		return nil, 0, fmt.Errorf("select files: %w", err)
	}
	return rows, total, nil
}

// escapeLikePattern 给 LIKE 用，转义 \ % _。
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
