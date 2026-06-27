package similarity

import (
	"strings"
	"testing"
)

// FamiliesToSnapshot 必须把内部 Family 转成稳定输出：
// - 家族按 primary_content_sign 排序
// - 成员按 content_sign 排序
// - 标记唯一一个 is_primary
func TestFamiliesToSnapshot_StableOrdering(t *testing.T) {
	fams := []Family{
		{
			FamilyID:     "FXXX",
			PrimaryID:    "u-b",
			Algorithm:    "tfidf-cosine",
			HighestScore: 0.87,
			Members: []FamilyMember{
				{UniqueID: "u-a", Path: "/p/a", ContentSign: "AAA", Relation: "derived", Score: 0.82},
				{UniqueID: "u-b", Path: "/p/b", ContentSign: "BBB", Relation: "primary", Score: 1.0},
				{UniqueID: "u-c", Path: "/p/c", ContentSign: "CCC", Relation: "derived", Score: 0.87},
			},
		},
		{
			FamilyID:  "FAAA",
			PrimaryID: "u-x",
			Algorithm: "content_hash",
			Members: []FamilyMember{
				{UniqueID: "u-x", Path: "/p/x", ContentSign: "ZZZ", Relation: "primary", Score: 1.0},
				{UniqueID: "u-y", Path: "/p/y", ContentSign: "YYY", Relation: "same_content", Score: 1.0},
			},
		},
	}

	snap := FamiliesToSnapshot(fams)

	if len(snap.Families) != 2 {
		t.Fatalf("expected 2 families, got %d", len(snap.Families))
	}

	// 家族按 primary_content_sign 排序：BBB 应在 ZZZ 前
	if snap.Families[0].PrimaryContentSign != "BBB" {
		t.Errorf("first family primary_content_sign = %q, want BBB",
			snap.Families[0].PrimaryContentSign)
	}
	if snap.Families[1].PrimaryContentSign != "ZZZ" {
		t.Errorf("second family primary_content_sign = %q, want ZZZ",
			snap.Families[1].PrimaryContentSign)
	}

	// 第一个家族成员按 content_sign 排序：AAA, BBB, CCC
	f0 := snap.Families[0]
	wantCS := []string{"AAA", "BBB", "CCC"}
	for i, m := range f0.Members {
		if m.ContentSign != wantCS[i] {
			t.Errorf("family[0].members[%d].content_sign = %q, want %q",
				i, m.ContentSign, wantCS[i])
		}
	}

	// is_primary 标记：只有 BBB 在第一个家族里是主
	primaryCount := 0
	for _, m := range f0.Members {
		if m.IsPrimary {
			primaryCount++
			if m.ContentSign != "BBB" {
				t.Errorf("primary in family[0] = %s, want BBB", m.ContentSign)
			}
		}
	}
	if primaryCount != 1 {
		t.Errorf("family[0] has %d primary members, want 1", primaryCount)
	}
}

// 完全相同的 snapshot 比对应该返回空字符串
func TestCompareSnapshots_IdenticalReturnsEmpty(t *testing.T) {
	s := FamilySnapshot{
		Families: []FamilySnap{
			{
				PrimaryContentSign: "X",
				Algorithm:          "tfidf-cosine",
				HighestScore:       0.9,
				Members: []MemberSnap{
					{ContentSign: "X", Relation: "primary", Score: 1.0, IsPrimary: true},
					{ContentSign: "Y", Relation: "derived", Score: 0.9, IsPrimary: false},
				},
			},
		},
	}
	if diff := CompareSnapshots(s, s); diff != "" {
		t.Errorf("identical compare returned diff: %s", diff)
	}
}

// 成员关系变化必须被检出
func TestCompareSnapshots_DetectsRelationChange(t *testing.T) {
	a := FamilySnapshot{Families: []FamilySnap{{
		PrimaryContentSign: "X", Algorithm: "tf",
		Members: []MemberSnap{
			{ContentSign: "X", Relation: "primary", IsPrimary: true},
			{ContentSign: "Y", Relation: "derived", Score: 0.85},
		},
	}}}
	b := FamilySnapshot{Families: []FamilySnap{{
		PrimaryContentSign: "X", Algorithm: "tf",
		Members: []MemberSnap{
			{ContentSign: "X", Relation: "primary", IsPrimary: true},
			{ContentSign: "Y", Relation: "same_content", Score: 0.85}, // changed
		},
	}}}
	diff := CompareSnapshots(a, b)
	if !strings.Contains(diff, "relation") || !strings.Contains(diff, "Y") {
		t.Errorf("expected diff to mention relation+Y, got: %s", diff)
	}
}

// 成员消失必须被检出
func TestCompareSnapshots_DetectsMissingMember(t *testing.T) {
	a := FamilySnapshot{Families: []FamilySnap{{
		PrimaryContentSign: "X",
		Members: []MemberSnap{
			{ContentSign: "X", IsPrimary: true},
			{ContentSign: "Y"},
			{ContentSign: "Z"},
		},
	}}}
	b := FamilySnapshot{Families: []FamilySnap{{
		PrimaryContentSign: "X",
		Members: []MemberSnap{
			{ContentSign: "X", IsPrimary: true},
			{ContentSign: "Y"},
		},
	}}}
	diff := CompareSnapshots(a, b)
	if !strings.Contains(diff, "MISSING") || !strings.Contains(diff, "Z") {
		t.Errorf("expected diff to mention Z MISSING, got: %s", diff)
	}
}

// 多了一个家族也要检出
func TestCompareSnapshots_DetectsExtraFamily(t *testing.T) {
	a := FamilySnapshot{Families: []FamilySnap{{PrimaryContentSign: "A"}}}
	b := FamilySnapshot{Families: []FamilySnap{
		{PrimaryContentSign: "A"},
		{PrimaryContentSign: "B"},
	}}
	diff := CompareSnapshots(a, b)
	if !strings.Contains(diff, "EXTRA") || !strings.Contains(diff, "B") {
		t.Errorf("expected diff to mention B EXTRA, got: %s", diff)
	}
}

// 分数变化即便很小也要检出（保证重构没引入末位差）
func TestCompareSnapshots_DetectsScoreChange(t *testing.T) {
	a := FamilySnapshot{Families: []FamilySnap{{
		PrimaryContentSign: "X",
		HighestScore:       0.876543210,
		Members:            []MemberSnap{{ContentSign: "X", Score: 0.876543210}},
	}}}
	b := FamilySnapshot{Families: []FamilySnap{{
		PrimaryContentSign: "X",
		HighestScore:       0.876543211, // 末位差 1
		Members:            []MemberSnap{{ContentSign: "X", Score: 0.876543211}},
	}}}
	diff := CompareSnapshots(a, b)
	if !strings.Contains(diff, "highest_score") {
		t.Errorf("expected diff to mention highest_score, got: %s", diff)
	}
}
