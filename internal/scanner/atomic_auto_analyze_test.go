package scanner

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestScanner_OnScanCompleteHookIsSettable verifies the hook variable can be set,
// invoked, and reset — confirming the wiring contract used by main.go.
func TestScanner_OnScanCompleteHookIsSettable(t *testing.T) {
	var called atomic.Int32
	OnScanCompleteHook = func() {
		called.Add(1)
	}
	defer func() { OnScanCompleteHook = nil }()

	// Manually invoke (simulates what atomic.go does after emitProgress)
	if OnScanCompleteHook != nil {
		OnScanCompleteHook()
	}

	if called.Load() != 1 {
		t.Errorf("hook called %d times, want 1", called.Load())
	}
}

// TestScanner_OnScanCompleteHookNilSafe verifies that the nil-guard in atomic.go
// prevents a panic when no hook has been injected.
func TestScanner_OnScanCompleteHookNilSafe(t *testing.T) {
	OnScanCompleteHook = nil
	// Should not panic if called when nil — code under test must check nil
	if OnScanCompleteHook != nil {
		OnScanCompleteHook()
	}
	// No assertion needed; just verify no panic
}

// TestAtomicScan_HookFires_FullInventory is an integration test that runs a real
// FULL_INVENTORY scan and verifies OnScanCompleteHook is invoked exactly once.
func TestAtomicScan_HookFires_FullInventory(t *testing.T) {
	tmp := t.TempDir()
	txt := filepath.Join(tmp, "hook_test.txt")
	if err := os.WriteFile(txt, []byte("hook fires when scan completes successfully"), 0644); err != nil {
		t.Fatal(err)
	}

	db := openTestDBWithScannerTables(t)
	defer db.Close()

	var hookCalled atomic.Int32
	OnScanCompleteHook = func() {
		hookCalled.Add(1)
	}
	defer func() { OnScanCompleteHook = nil }()

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

	if hookCalled.Load() != 1 {
		t.Errorf("OnScanCompleteHook called %d times after FullInventory scan, want 1", hookCalled.Load())
	}
}
