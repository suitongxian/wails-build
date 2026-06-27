package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/repository"
)

// setupFeatureFlagTestDB opens a fresh test DB (with all migrations applied) and also
// registers it as the repository singleton so that featurePrecomputeEnabled() can read
// from it via repository.TryGetDB().
func setupFeatureFlagTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db := openTestDBWithScannerTables(t)
	restore := repository.SetTestDB(db)
	t.Cleanup(restore)
	return db
}

// runFeatureFlagScan runs a FULL_INVENTORY scan over dir using the provided DB.
func runFeatureFlagScan(t *testing.T, db *sqlx.DB, dir string) {
	t.Helper()
	s := NewAtomicScanner(db, 100)
	result := s.Scan(AtomicScanOptions{
		Directory:      dir,
		Extensions:     []string{".txt"},
		ScanMode:       FullInventory,
		MD5Concurrency: 2,
		BatchSize:      10,
	})
	if !result.Success {
		t.Fatalf("scan failed: %s", result.ErrorMessage)
	}
}

func TestFeatureFlag_OnByDefault(t *testing.T) {
	// Fresh DB → flag defaults to "true"
	db := setupFeatureFlagTestDB(t)
	repo := repository.NewSystemConfigRepository(db)
	v, err := repo.Get(repository.KeyFeaturePrecomputeEnabled)
	if err != nil {
		t.Fatalf("get flag: %v", err)
	}
	if v != "true" {
		t.Errorf("default flag = %q, want \"true\"", v)
	}
}

func TestFeatureFlag_DisablesPrecompute(t *testing.T) {
	db := setupFeatureFlagTestDB(t)
	repo := repository.NewSystemConfigRepository(db)
	if err := repo.Set(repository.KeyFeaturePrecomputeEnabled, "false"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	// Now scan a file
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	os.WriteFile(f, []byte("test content for flag off scenario"), 0644)

	runFeatureFlagScan(t, db, tmp)

	// Verify extracted_text is NULL (flag disabled the feature extraction)
	var ext *string
	db.Get(&ext, `SELECT extracted_text FROM data_distributing WHERE path=?`, f)
	if ext != nil && *ext != "" {
		t.Errorf("flag off → extracted_text should be NULL, got %q", *ext)
	}
}

func TestFeatureFlag_EnabledWritesFeatures(t *testing.T) {
	db := setupFeatureFlagTestDB(t)
	// Flag defaults to "true" — no explicit set needed

	tmp := t.TempDir()
	f := filepath.Join(tmp, "hello.txt")
	os.WriteFile(f, []byte("hello world test document for feature extraction and similarity computation"), 0644)

	runFeatureFlagScan(t, db, tmp)

	var ext *string
	db.Get(&ext, `SELECT extracted_text FROM data_distributing WHERE path=?`, f)
	if ext == nil || *ext == "" {
		t.Errorf("flag on → extracted_text should be populated, got nil/empty")
	}
}
