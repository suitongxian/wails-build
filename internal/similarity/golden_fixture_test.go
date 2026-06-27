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

// TestGoldenFixture_BuildFamilies_Snapshot 是「锁定当前算法行为」的基线测试。
//
// 作用：
//   - 在重构 buildFamilies 之前先把当前输出固化为 testdata/golden_fixture.json
//   - 后续每一步重构都跑这个测试，任何家族成员集合 / relation / score 差异都会
//     被检出，逼我们回滚那次改动
//
// 用法：
//   - 首次 capture：UPDATE_GOLDEN=1 go test -run TestGoldenFixture_BuildFamilies_Snapshot
//     会把当前输出写到 testdata/golden_fixture.json
//   - 平时验证：直接 go test，会跟 testdata/golden_fixture.json 比对

func TestGoldenFixture_BuildFamilies_Snapshot(t *testing.T) {
	dir := t.TempDir()
	inputs := buildSyntheticFixture(t, dir)

	fams, err := BuildFamilies(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("BuildFamilies: %v", err)
	}

	snap := normalizeForGolden(FamiliesToSnapshot(fams), dir)
	goldenPath := "testdata/golden_fixture.json"

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := SaveSnapshot(goldenPath, snap); err != nil {
			t.Fatalf("save golden: %v", err)
		}
		t.Logf("Wrote golden snapshot: %s (families=%d)", goldenPath, len(snap.Families))
		return
	}

	want, err := LoadSnapshot(goldenPath)
	if err != nil {
		t.Fatalf("load golden (run with UPDATE_GOLDEN=1 first): %v", err)
	}

	if diff := CompareSnapshots(want, snap); diff != "" {
		t.Fatalf("golden snapshot diff:\n%s", diff)
	}
}

// normalizeForGolden 把 tempdir 路径从 snapshot 里抹掉，因为 t.TempDir() 路径随机变。
// 只保留文件名部分供 debug 阅读。
func normalizeForGolden(s FamilySnapshot, dir string) FamilySnapshot {
	for i := range s.Families {
		s.Families[i].PrimaryPath = stripDir(s.Families[i].PrimaryPath, dir)
		for j := range s.Families[i].Members {
			s.Families[i].Members[j].Path = stripDir(s.Families[i].Members[j].Path, dir)
		}
	}
	return s
}

func stripDir(p, dir string) string {
	if p == "" {
		return ""
	}
	if rel, err := filepath.Rel(dir, p); err == nil {
		return rel
	}
	return filepath.Base(p)
}

// buildSyntheticFixture 写出一组覆盖典型场景的合成文件：
//   1. 完全相同的两个 txt （exact dup → content_hash 归并）
//   2. 内容极度相似的两个 txt （near-dup → simhash/tfidf 归并）
//   3. 一份完全无关的 txt （独立文件，不应被纳入任何家族）
//   4. 同内容跨格式：同一段 markdown 同时存为 .txt 和 .md
//   5. 同目录下名称相似（report_v1.txt / report_v2.txt 拼写微差 + 内容很像）
//   6. 大小过滤边界（短和长两个 doc，应被尺寸过滤剔除）
//
// 共 9 个 file inputs。简单覆盖但覆盖到决策树主要分支。
func buildSyntheticFixture(t testing.TB, dir string) []FileInput {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	const longText = "客户合同正本：甲方为示例公司，乙方为另一示例公司。本合同自双方签字之日起生效，有效期为一年。" +
		"双方就服务范围、付款方式、违约责任达成一致。任何一方变更条款须经书面同意。本协议受所在地法律管辖。"

	type f struct {
		name    string
		content string
	}
	files := []f{
		// 1. 完全相同的两份（exact dup）
		{"exact_a.txt", "完全相同的内容A：" + longText},
		{"exact_b.txt", "完全相同的内容A：" + longText},

		// 2. 内容极度相似（near-dup）：只改了几个字
		{"near_a.txt", "近似副本X：" + longText + " 备注：版本一"},
		{"near_b.txt", "近似副本X：" + longText + " 备注：版本二"},

		// 3. 完全无关
		{"unrelated.txt", "这是一份完全无关的笔记，主要讲述今天的天气和午饭的菜单。" +
			"上午开了一个无聊的会议，下午组织了一次团建活动。"},

		// 4. 同目录下名称相似 + 内容相似（应被归并）
		{"report_v1.txt", "周报：本周完成扫描器开发。计划：下周联调。" +
			"负责人甲。详情：" + longText},
		{"report_v2.txt", "周报：本周完成扫描器开发。计划：下周联调。" +
			"负责人乙。详情：" + longText + " 备注：审查后版本。"},

		// 5. 大小差异巨大（一长一短同主题，应被尺寸过滤拒绝）
		{"long.txt", longText + longText + longText + longText},
		{"short.txt", "短文档"},
	}

	inputs := make([]FileInput, 0, len(files))
	for _, file := range files {
		full := filepath.Join(dir, file.name)
		if err := os.WriteFile(full, []byte(file.content), 0644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
		sum := sha256.Sum256([]byte(file.content))
		cs := strings.ToUpper(hex.EncodeToString(sum[:]))
		size := int64(len(file.content))
		inputs = append(inputs, FileInput{
			UniqueID:    file.name, // 名字稳定，便于 debug
			Path:        full,
			ContentSign: cs,
			Size:        size,
			ModTime:     now,
		})
	}
	return inputs
}
