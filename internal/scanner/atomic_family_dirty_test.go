package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 扫描成功后必须把 similarity_family_dirty 置 "1"，
// 否则前端「重建相似关系」按钮永远不会被点亮，相当于把家族归并逻辑废了。
func TestAtomicScan_FullInventory_MarksFamilyDirty(t *testing.T) {
	tmp := t.TempDir()
	txt := filepath.Join(tmp, "dirty_marker.txt")
	if err := os.WriteFile(txt, []byte("any content"), 0644); err != nil {
		t.Fatal(err)
	}

	db := openTestDBWithScannerTables(t)
	defer db.Close()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyFamilyDirty, "0") // 模拟「之前刚跑完分析」

	s := NewAtomicScanner(db, 100)
	result := s.Scan(AtomicScanOptions{
		Directory:      tmp,
		Extensions:     []string{".txt"},
		ScanMode:       FullInventory,
		MD5Concurrency: 2,
		BatchSize:      10,
	})
	if !result.Success {
		t.Fatalf("scan failed: %s", result.ErrorMessage)
	}

	if got := cfg.GetValue(repository.KeyFamilyDirty); got != "1" {
		t.Errorf("after FullInventory scan: family_dirty = %q, want %q", got, "1")
	}
}

func TestAtomicScan_DailyCheck_MarksFamilyDirty(t *testing.T) {
	tmp := t.TempDir()
	txt := filepath.Join(tmp, "survival.txt")
	if err := os.WriteFile(txt, []byte("survives"), 0644); err != nil {
		t.Fatal(err)
	}

	db := openTestDBWithScannerTables(t)
	defer db.Close()

	// 先做一次全量扫描，让存活扫描有"对照基线"
	s := NewAtomicScanner(db, 100)
	full := s.Scan(AtomicScanOptions{
		Directory:      tmp,
		Extensions:     []string{".txt"},
		ScanMode:       FullInventory,
		MD5Concurrency: 2,
		BatchSize:      10,
	})
	if !full.Success {
		t.Fatalf("seed scan failed: %s", full.ErrorMessage)
	}

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyFamilyDirty, "0")

	// 再触发一次存活扫描（DailyCheck 走 survival 路径）
	survival := s.Scan(AtomicScanOptions{
		Directory:      tmp,
		Extensions:     []string{".txt"},
		ScanMode:       DailyCheck,
		MD5Concurrency: 2,
		BatchSize:      10,
	})
	if !survival.Success {
		t.Fatalf("survival scan failed: %s", survival.ErrorMessage)
	}

	if got := cfg.GetValue(repository.KeyFamilyDirty); got != "1" {
		t.Errorf("after DailyCheck: family_dirty = %q, want %q", got, "1")
	}
}
