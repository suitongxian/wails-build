package similarity

import (
	"database/sql"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
)

// CachedFeaturesDB 是从 data_distributing 读出的特征缓存。
// 不含 ExtractedText 字段：lazy load 时用 QueryExtractedText 单独按需查。
type CachedFeaturesDB struct {
	Simhash      uint64
	ContentHash  string
	Phash        string
	FeatureMtime time.Time
	FeatureSize  int64
}

// ReadCachedFeatures 读 data_distributing 一行的特征。
// 返回 nil 表示特征未持久化（视为 cache miss）。
func ReadCachedFeatures(db *sqlx.DB, contentSign string) (*CachedFeaturesDB, error) {
	var row struct {
		Simhash      *int64     `db:"simhash"`
		ContentHash  *string    `db:"content_hash"`
		Phash        *string    `db:"phash"`
		FeatureMtime *time.Time `db:"feature_mtime"`
		FeatureSize  *int64     `db:"feature_size"`
	}
	err := db.Get(&row,
		`SELECT simhash, content_hash, phash, feature_mtime, feature_size
         FROM data_distributing WHERE content_sign = ? AND disable = 0 LIMIT 1`, contentSign)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.FeatureMtime == nil || row.FeatureSize == nil {
		return nil, nil
	}
	c := &CachedFeaturesDB{
		FeatureMtime: *row.FeatureMtime,
		FeatureSize:  *row.FeatureSize,
	}
	if row.Simhash != nil {
		c.Simhash = uint64(*row.Simhash)
	}
	if row.ContentHash != nil {
		c.ContentHash = *row.ContentHash
	}
	if row.Phash != nil {
		c.Phash = *row.Phash
	}
	return c, nil
}

// IsCacheValid 通过 stat 当前文件，对比 mtime+size 判断缓存是否仍有效。
// stat 失败 / mtime 不一致 / size 不一致 → 视为失效。
func IsCacheValid(filePath string, cached *CachedFeaturesDB) bool {
	if cached == nil {
		return false
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	if info.Size() != cached.FeatureSize {
		return false
	}
	if !info.ModTime().Equal(cached.FeatureMtime) {
		return false
	}
	return true
}

// WriteBackFeatures cache miss 现场重算后回写到 DB，修复缓存。
func WriteBackFeatures(db *sqlx.DB, contentSign string, feat CachedFeaturesDB) error {
	_, err := db.Exec(
		`UPDATE data_distributing
         SET simhash = ?, content_hash = ?, phash = ?, feature_mtime = ?, feature_size = ?,
             update_time = ?
         WHERE content_sign = ? AND disable = 0`,
		int64(feat.Simhash), feat.ContentHash, feat.Phash,
		feat.FeatureMtime, feat.FeatureSize, time.Now(), contentSign)
	return err
}

// WriteBackExtractedText cache miss 重算时同时回写文本（独立函数，
// 因为重算时通常一并算出所有 features + text）。
func WriteBackExtractedText(db *sqlx.DB, contentSign, text string) error {
	_, err := db.Exec(
		`UPDATE data_distributing SET extracted_text = ?, update_time = ?
         WHERE content_sign = ? AND disable = 0`,
		text, time.Now(), contentSign)
	return err
}

// QueryExtractedText lazy load 单条 extracted_text（pair similarity 阶段用）。
// 返回空字符串表示未缓存（调用方应 fallback 现场 extractText）。
//
// 注意：重构 #1 之后主循环改走 BatchReadCachedFeaturesWithText 一次性预加载，
// 这条函数仅保留给极少数仍走 lazy 路径的场景（向后兼容）。
func QueryExtractedText(db *sqlx.DB, contentSign string) string {
	var text *string
	err := db.Get(&text,
		`SELECT extracted_text FROM data_distributing
         WHERE content_sign = ? AND disable = 0 LIMIT 1`, contentSign)
	if err != nil || text == nil {
		return ""
	}
	return *text
}

// CachedFeaturesBulk 是批量预加载的单条记录（含 extracted_text）。
type CachedFeaturesBulk struct {
	CachedFeaturesDB
	ExtractedText string
}

// BatchReadCachedFeaturesWithText 把 contentSigns 列表对应的全部特征 + extracted_text
// 一次性拉到内存，返回按 content_sign 索引的 map。失败时已加载的部分仍返回（带 err）。
//
// 重构 #1：替代 buildFamilies 主循环里 N 次 ReadCachedFeatures + worker pool 里 K 次
// QueryExtractedText 的反复 SQL 往返。单连接 SQLite 下显著缩短端到端耗时。
//
// content_signs 同值多行会去重，保留任意一行（与原 LIMIT 1 行为一致）。
// 自动按批次 500 个 chunk，避开 SQLite IN 参数上限。
func BatchReadCachedFeaturesWithText(db *sqlx.DB, contentSigns []string) (map[string]*CachedFeaturesBulk, error) {
	out := make(map[string]*CachedFeaturesBulk, len(contentSigns))
	if len(contentSigns) == 0 {
		return out, nil
	}

	const chunkSize = 500
	dedupe := make(map[string]struct{}, len(contentSigns))
	uniq := make([]string, 0, len(contentSigns))
	for _, cs := range contentSigns {
		if cs == "" {
			continue
		}
		if _, ok := dedupe[cs]; ok {
			continue
		}
		dedupe[cs] = struct{}{}
		uniq = append(uniq, cs)
	}

	type row struct {
		ContentSign   string     `db:"content_sign"`
		Simhash       *int64     `db:"simhash"`
		ContentHash   *string    `db:"content_hash"`
		Phash         *string    `db:"phash"`
		ExtractedText *string    `db:"extracted_text"`
		FeatureMtime  *time.Time `db:"feature_mtime"`
		FeatureSize   *int64     `db:"feature_size"`
	}

	for start := 0; start < len(uniq); start += chunkSize {
		end := start + chunkSize
		if end > len(uniq) {
			end = len(uniq)
		}
		chunk := uniq[start:end]

		query, args, err := sqlx.In(
			`SELECT content_sign, simhash, content_hash, phash, extracted_text, feature_mtime, feature_size
             FROM data_distributing WHERE disable = 0 AND content_sign IN (?)`, chunk)
		if err != nil {
			return out, err
		}
		query = db.Rebind(query)

		var rows []row
		if err := db.Select(&rows, query, args...); err != nil {
			return out, err
		}
		for _, r := range rows {
			// content_sign 同值多行：保留先到者，后到者跳过
			if _, ok := out[r.ContentSign]; ok {
				continue
			}
			// feature_mtime/feature_size 缺一即视为未缓存
			if r.FeatureMtime == nil || r.FeatureSize == nil {
				continue
			}
			b := &CachedFeaturesBulk{
				CachedFeaturesDB: CachedFeaturesDB{
					FeatureMtime: *r.FeatureMtime,
					FeatureSize:  *r.FeatureSize,
				},
			}
			if r.Simhash != nil {
				b.Simhash = uint64(*r.Simhash)
			}
			if r.ContentHash != nil {
				b.ContentHash = *r.ContentHash
			}
			if r.Phash != nil {
				b.Phash = *r.Phash
			}
			if r.ExtractedText != nil {
				b.ExtractedText = *r.ExtractedText
			}
			out[r.ContentSign] = b
		}
	}
	return out, nil
}
