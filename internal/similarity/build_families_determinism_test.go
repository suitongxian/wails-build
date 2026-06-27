package similarity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// TestBuildFamilies_DeterministicAcrossCacheState verifies that BuildFamilies
// produces identical family structures whether features come from the DB cache
// (hit path) or live extraction (miss path). This is the critical safety net for
// Spec B: proves the cache path is functionally equivalent to live extraction.
func TestBuildFamilies_DeterministicAcrossCacheState(t *testing.T) {
	// Set up a small but realistic fixture: a few groups of similar files
	fixture := setupDeterminismFixture(t)

	SetDB(fixture.db)
	defer SetDB(nil)

	// 1. First run with all features cleared → cache miss → live extraction
	clearAllFeatures(t, fixture.db)
	fams1, err := BuildFamilies(context.Background(), fixture.inputs, defaultConfig())
	if err != nil {
		t.Fatalf("first BuildFamilies (miss): %v", err)
	}
	snap1 := snapshotFamilies(fams1)

	// 2. Second run: cache should be populated now (from writeback during miss run)
	//    → cache hit → DB-backed features
	fams2, err := BuildFamilies(context.Background(), fixture.inputs, defaultConfig())
	if err != nil {
		t.Fatalf("second BuildFamilies (hit): %v", err)
	}
	snap2 := snapshotFamilies(fams2)

	if snap1 != snap2 {
		t.Errorf("BuildFamilies non-deterministic across cache state:\n=== miss snapshot ===\n%s\n=== hit snapshot ===\n%s",
			snap1, snap2)
	}
}

type determinismFixture struct {
	db     *sqlx.DB
	tmp    string
	inputs []FileInput
}

func (f *determinismFixture) cleanup() {
	f.db.Close()
}

// setupDeterminismFixture creates a small but meaningful set of files:
// - 3 groups of 3 files each: identical content (each group), distinct from other groups
// - 2 standalone files
// = 11 files total, expected 3 same-content families
func setupDeterminismFixture(t *testing.T) *determinismFixture {
	t.Helper()
	tmp := t.TempDir()
	db := openE2EDB(t)

	var inputs []FileInput
	now := time.Now()

	seedFile := func(idx int, content string, contentSign string) {
		path := filepath.Join(tmp, fmt.Sprintf("file_%02d.txt", idx))
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`INSERT INTO data_distributing
			(path, content_sign, file_create_time, file_size,
			 data_type, scan_found_count, ip, mac_address, scan_time,
			 create_time, update_time, disable)
			VALUES (?, ?, ?, ?,
			        'FILE', 1, '', '', ?,
			        ?, ?, 0)`,
			path, contentSign, now, info.Size(), now, now, now)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, FileInput{
			ContentSign: contentSign,
			Path:        path,
			Size:        info.Size(),
			ModTime:     info.ModTime(),
		})
	}

	// Group 1: 3 files with identical "alpha" content
	for i := 0; i < 3; i++ {
		seedFile(i, "alpha content sufficient length for simhash and content hash computation", fmt.Sprintf("CS_ALPHA_%d", i))
	}
	// Group 2: 3 files with identical "beta" content
	for i := 0; i < 3; i++ {
		seedFile(i+3, "beta different content with enough text for hashing and feature extraction", fmt.Sprintf("CS_BETA_%d", i))
	}
	// Group 3: 3 files with identical "gamma" content
	for i := 0; i < 3; i++ {
		seedFile(i+6, "gamma yet another distinct content body with sufficient characters", fmt.Sprintf("CS_GAMMA_%d", i))
	}
	// Standalones
	seedFile(9, "solitary file alpha unique content has enough length", "CS_SOLO_1")
	seedFile(10, "another solitary file with totally separate content here", "CS_SOLO_2")

	return &determinismFixture{
		db:     db,
		tmp:    tmp,
		inputs: inputs,
	}
}

func clearAllFeatures(t *testing.T, db *sqlx.DB) {
	t.Helper()
	_, err := db.Exec(`UPDATE data_distributing SET
		simhash = NULL, content_hash = NULL, extracted_text = NULL,
		feature_mtime = NULL, feature_size = NULL`)
	if err != nil {
		t.Fatal(err)
	}
}

// snapshotFamilies returns a deterministic string representation of family
// structure for comparison. Uses ContentSign (stable across runs) to identify
// both the primary member and all members. Families are sorted by primary
// ContentSign so the snapshot is order-independent.
func snapshotFamilies(fams []Family) string {
	type famSnap struct {
		PrimarySign string   `json:"primary_sign"`
		Members     []string `json:"members"`
		Score       float64  `json:"score"`
	}
	snaps := make([]famSnap, 0, len(fams))
	for _, f := range fams {
		// Resolve the primary ContentSign by finding the member whose UniqueID
		// matches f.PrimaryID — this is stable unlike f.PrimaryID which is a
		// UUID regenerated each BuildFamilies call when FileInput.UniqueID is empty.
		primarySign := ""
		memberSigns := make([]string, 0, len(f.Members))
		for _, m := range f.Members {
			memberSigns = append(memberSigns, m.ContentSign)
			if m.UniqueID == f.PrimaryID {
				primarySign = m.ContentSign
			}
		}
		sort.Strings(memberSigns)
		snaps = append(snaps, famSnap{
			PrimarySign: primarySign,
			Members:     memberSigns,
			Score:       f.HighestScore,
		})
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].PrimarySign < snaps[j].PrimarySign })
	b, _ := json.MarshalIndent(snaps, "", "  ")
	return string(b)
}
