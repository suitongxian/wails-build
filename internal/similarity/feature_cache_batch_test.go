package similarity

import (
	"testing"
	"time"
)

// 批量预加载应该等价于多次单条读取的合并结果。
func TestBatchReadCachedFeaturesWithText_BasicLoad(t *testing.T) {
	db := openE2EDB(t)
	now := time.Now()

	// 写两行带完整 feature + extracted_text
	for _, cs := range []string{"CS-A", "CS-B"} {
		mtime := now
		size := int64(1234)
		simhash := int64(0xABCDEF)
		ch := "ch-" + cs
		text := "text body for " + cs
		_, err := db.Exec(`INSERT INTO data_distributing
			(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time,
			 simhash, content_hash, extracted_text, feature_mtime, feature_size,
			 create_time, update_time, disable)
			VALUES (?, 0, 1, ?, ?, '', '', ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			"/p/"+cs, cs, size, now, simhash, ch, text, mtime, size, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	// 再写一个 feature_mtime=NULL 的行，模拟未缓存
	_, err := db.Exec(`INSERT INTO data_distributing
		(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time,
		 create_time, update_time, disable)
		VALUES ('/p/CS-C', 0, 1, 'CS-C', 100, '', '', ?, ?, ?, 0)`, now, now, now)
	if err != nil {
		t.Fatal(err)
	}

	got, err := BatchReadCachedFeaturesWithText(db, []string{"CS-A", "CS-B", "CS-C", "CS-D"})
	if err != nil {
		t.Fatalf("batch load: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 cached entries, got %d (keys=%v)", len(got), keys(got))
	}
	if _, ok := got["CS-C"]; ok {
		t.Errorf("CS-C should be absent (no feature_mtime/size)")
	}
	if _, ok := got["CS-D"]; ok {
		t.Errorf("CS-D should be absent (no row in DB)")
	}
	if a := got["CS-A"]; a != nil {
		if a.Simhash != 0xABCDEF {
			t.Errorf("CS-A simhash mismatch: %x", a.Simhash)
		}
		if a.ContentHash != "ch-CS-A" {
			t.Errorf("CS-A content_hash: %q", a.ContentHash)
		}
		if a.ExtractedText != "text body for CS-A" {
			t.Errorf("CS-A extracted_text: %q", a.ExtractedText)
		}
	}
}

func TestBatchReadCachedFeaturesWithText_EmptyInputReturnsEmpty(t *testing.T) {
	db := openE2EDB(t)
	got, err := BatchReadCachedFeaturesWithText(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %d", len(got))
	}
}

func TestBatchReadCachedFeaturesWithText_HandlesDuplicateContentSigns(t *testing.T) {
	db := openE2EDB(t)
	now := time.Now()
	// 两条相同 content_sign 不同 path 的行（多副本场景）
	for i := 0; i < 2; i++ {
		simhash := int64(0xAAAA + int64(i))
		_, err := db.Exec(`INSERT INTO data_distributing
			(path, data_type, scan_found_count, content_sign, file_size, ip, mac_address, scan_time,
			 simhash, feature_mtime, feature_size,
			 create_time, update_time, disable)
			VALUES (?, 0, 1, 'CS-X', 100, '', '', ?, ?, ?, ?, ?, ?, 0)`,
			"/p/dup_"+string(rune('a'+i)), now, simhash, now, int64(100), now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := BatchReadCachedFeaturesWithText(db, []string{"CS-X"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 entry after dedup, got %d", len(got))
	}
	// simhash 值只要是其中一个，不报错（与原 LIMIT 1 任意性一致）
	if v := got["CS-X"]; v == nil || (v.Simhash != 0xAAAA && v.Simhash != 0xAAAB) {
		t.Errorf("CS-X unexpected: %+v", v)
	}
}

func keys(m map[string]*CachedFeaturesBulk) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
