package repository

import (
	"strings"
	"testing"
)

// 本终端稳定实例标识：首次生成并持久化，之后多次调用返回同一个（作 scan_endpoint 用，跨重启稳定）。
func TestEnsureScanInstanceID_StableAndPersisted(t *testing.T) {
	db := openTestDB(t)
	cfg := NewSystemConfigRepository(db)

	id1 := cfg.EnsureScanInstanceID()
	if !strings.HasPrefix(id1, "scan-") || len(id1) < 10 {
		t.Fatalf("实例标识格式应为 scan-<uuid>，实得 %q", id1)
	}
	// 再次调用返回同一个（不重新生成）
	if id2 := cfg.EnsureScanInstanceID(); id2 != id1 {
		t.Fatalf("应返回同一稳定标识：%q vs %q", id1, id2)
	}
	// 新建仓库实例（模拟重启后再读）仍是同一个（已持久化）
	if id3 := NewSystemConfigRepository(db).EnsureScanInstanceID(); id3 != id1 {
		t.Fatalf("持久化后应仍为同一标识：%q vs %q", id1, id3)
	}
	// 已落库
	if got := cfg.GetValue(KeyScanInstanceID); got != id1 {
		t.Fatalf("应已写入 system_config，实得 %q", got)
	}
}
