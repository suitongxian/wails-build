package repository

import (
	"testing"
)

func TestMigration_AddsMemorandumColumns(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	for _, col := range []string{
		"memorandum_topic",
		"memorandum_classification",
		"memorandum_registered_at",
		"memorandum_registered_by",
		"memorandum_signature_hash",
	} {
		ok, err := columnExists(db, "asset_ledgers", col)
		if err != nil {
			t.Fatalf("columnExists %s: %v", col, err)
		}
		if !ok {
			t.Errorf("asset_ledgers.%s missing after migration", col)
		}
	}
}

func TestMigration_AddsFamilyAuthoritativeColumn(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()
	ok, err := columnExists(db, "data_resource_family", "authoritative_resource_id")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !ok {
		t.Fatal("data_resource_family.authoritative_resource_id missing")
	}
}
