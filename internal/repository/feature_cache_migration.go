package repository

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// migrateDataDistributingFeatureCache creates the partial index on content_hash
// that supports fast duplicate-detection queries.
// The 6 feature columns themselves are added via the columnAdds slice in
// initDB so they follow the same idempotent PRAGMA-based pattern as all other
// ALTER TABLE migrations.
func migrateDataDistributingFeatureCache(db *sqlx.DB) error {
	alterSQL := `CREATE INDEX IF NOT EXISTS idx_data_distributing_content_hash
	 ON data_distributing(content_hash) WHERE content_hash IS NOT NULL`
	if _, err := db.Exec(alterSQL); err != nil {
		return fmt.Errorf("create content_hash index: %w", err)
	}
	return nil
}
