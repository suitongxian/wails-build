package similarity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 历史 bug：worker pool 的 union 顺序非确定 → UF 树根 gid 随机 →
// step 1/1.5 在旧 root 上写入的 exact_hash 1.0 元数据丢失 → highest_score 凭运气波动。
// 重跑同一份合成 fixture 50 次，所有家族的 highest_score 应该 bit-equal。
func TestBuildFamilies_HighestScore_DeterministicAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// 触发场景：包含一对 exact dup（应当贡献 score=1.0 的 exact_hash 标记）
	// + 一批 doc 相似（贡献 < 1.0 的 worker pool 分数）。
	// 全部相互归并到同一家族，把 UF 树根的非确定性放大。
	const sharedLong = "客户合同正本：甲方为示例公司，乙方为另一示例公司。本合同自双方签字之日起生效，有效期为一年。" +
		"双方就服务范围、付款方式、违约责任达成一致。任何一方变更条款须经书面同意。"

	files := []struct {
		name, content string
	}{
		{"exact_a.txt", "完全相同的内容A：" + sharedLong},
		{"exact_b.txt", "完全相同的内容A：" + sharedLong},
		{"near_a.txt", "近似副本X：" + sharedLong + " 备注：版本一"},
		{"near_b.txt", "近似副本X：" + sharedLong + " 备注：版本二"},
		{"v1.txt", "周报：本周完成开发。负责人甲。详情：" + sharedLong},
		{"v2.txt", "周报：本周完成开发。负责人乙。详情：" + sharedLong + " 备注：审查后版本。"},
	}

	inputs := make([]FileInput, 0, len(files))
	for _, f := range files {
		full := filepath.Join(dir, f.name)
		if err := os.WriteFile(full, []byte(f.content), 0644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
		sum := sha256.Sum256([]byte(f.content))
		cs := strings.ToUpper(hex.EncodeToString(sum[:]))
		inputs = append(inputs, FileInput{
			UniqueID: f.name, Path: full, ContentSign: cs,
			Size: int64(len(f.content)), ModTime: now,
		})
	}

	first, err := BuildFamilies(context.Background(), inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstSnap := FamiliesToSnapshot(first)

	const iters = 50
	for i := 0; i < iters; i++ {
		fams, err := BuildFamilies(context.Background(), inputs, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := FamiliesToSnapshot(fams)
		if diff := CompareSnapshots(firstSnap, got); diff != "" {
			t.Fatalf("BuildFamilies non-deterministic at iter %d:\n%s", i, diff)
		}
	}
}
