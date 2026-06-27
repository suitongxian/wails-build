package ai

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// EnrichInputForResource 从 db 取 data_resource + data_distributing 信息，
// 组装成 AutoClassifyInput（含 FileName/Path/Metadata）。
//
// 同目录上下文：通过 LIKE 查同 parent_dir 下其他资源数量（不含本身）填入
// Metadata["sibling_count"]，给评分函数判断"是否属于一个聚合目录"提供信号。
//
// 元数据 keys:
//
//	"file_size"     — bytes (string)
//	"mime"          — file_magic from data_distributing
//	"ext"           — lower-case extension without dot
//	"create_time"   — file_create_time (raw string from DB)
//	"update_time"   — file_update_time (raw string from DB)
//	"parent_dir"    — parent directory basename
//	"sibling_count" — 同 parent dir 下其他资源数量（不同 content_sign）
//
// 资源无 distribution → 仅 FileName 被填充，Metadata 为空 map。
func EnrichInputForResource(db *sqlx.DB, resourceID int64) (AutoClassifyInput, error) {
	in := AutoClassifyInput{Metadata: map[string]string{}}

	type drRow struct {
		Name        *string `db:"resources_name"`
		Desc        *string `db:"resources_desc"`
		ContentSubj *string `db:"content_subject"`
		ContentSign string  `db:"content_sign"`
	}
	var dr drRow
	if err := db.Get(&dr, `SELECT resources_name, resources_desc, content_subject, content_sign
		FROM data_resources WHERE data_resources_id = ? AND disable = 0`, resourceID); err != nil {
		return in, fmt.Errorf("查 resource: %w", err)
	}
	if dr.Name != nil {
		in.FileName = *dr.Name
	}
	if dr.Desc != nil {
		in.Summary = *dr.Desc
	}
	if dr.ContentSubj != nil && in.Summary == "" {
		in.Summary = *dr.ContentSubj
	}

	type distRow struct {
		Path           string  `db:"path"`
		FileSize       int64   `db:"file_size"`
		FileSuffix     *string `db:"file_suffix"`
		FileMagic      *string `db:"file_magic"`
		FileCreateTime *string `db:"file_create_time"`
		FileUpdateTime *string `db:"file_update_time"`
	}
	var dist distRow
	err := db.Get(&dist, `SELECT path, file_size, file_suffix, file_magic, file_create_time, file_update_time
		FROM data_distributing
		WHERE content_sign = ? AND disable = 0
		ORDER BY data_distribution_id LIMIT 1`, dr.ContentSign)
	if err != nil {
		// 资源无分布信息：仅 FileName 可用，Metadata 保留为空 map
		return in, nil
	}

	in.Path = dist.Path
	in.Metadata["file_size"] = fmt.Sprintf("%d", dist.FileSize)
	if dist.FileMagic != nil {
		in.Metadata["mime"] = *dist.FileMagic
	}
	if dist.FileSuffix != nil && *dist.FileSuffix != "" {
		in.Metadata["ext"] = strings.ToLower(*dist.FileSuffix)
	} else {
		ext := strings.TrimPrefix(filepath.Ext(dist.Path), ".")
		in.Metadata["ext"] = strings.ToLower(ext)
	}
	if dist.FileCreateTime != nil {
		in.Metadata["create_time"] = *dist.FileCreateTime
	}
	if dist.FileUpdateTime != nil {
		in.Metadata["update_time"] = *dist.FileUpdateTime
	}

	parentDir := filepath.Dir(dist.Path)
	in.Metadata["parent_dir"] = filepath.Base(parentDir)

	// sibling_count: 同 parent dir 下其他不同 content_sign 的资源数量
	parentPrefix := parentDir
	if !strings.HasSuffix(parentPrefix, "/") {
		parentPrefix += "/"
	}
	var siblingCount int
	_ = db.Get(&siblingCount, `SELECT COUNT(DISTINCT content_sign) FROM data_distributing
		WHERE path LIKE ? AND content_sign != ? AND disable = 0`,
		parentPrefix+"%", dr.ContentSign)
	in.Metadata["sibling_count"] = fmt.Sprintf("%d", siblingCount)

	return in, nil
}
