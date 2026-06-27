package similarity

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// 本文件提供 golden snapshot 工具：把 BuildFamilies 的输出归一化为稳定 JSON，
// 用于跨重构验证算法行为不变。
//
// 设计原则：
//   - 不依赖 family_id（自增可变） — 用 primary_content_sign 作为家族稳定 ID
//   - 不依赖 map 迭代顺序 — 全部排序输出
//   - 文件路径与名称仅供 debug 阅读，比对时只看 content_sign / score / relation / is_primary
//   - 浮点 score 不四舍五入，保持原值；若重构引入末位差异即视为破坏（应该不会发生）

// FamilySnapshot 是稳定可比对的家族列表序列化结构。
type FamilySnapshot struct {
	Families []FamilySnap `json:"families"`
}

type FamilySnap struct {
	PrimaryContentSign string       `json:"primary_content_sign"`
	PrimaryPath        string       `json:"primary_path,omitempty"` // debug only
	Algorithm          string       `json:"algorithm"`
	HighestScore       float64      `json:"highest_score"`
	Members            []MemberSnap `json:"members"`
}

type MemberSnap struct {
	ContentSign string  `json:"content_sign"`
	Path        string  `json:"path,omitempty"` // debug only
	Relation    string  `json:"relation"`
	Score       float64 `json:"score"`
	IsPrimary   bool    `json:"is_primary"`
}

// FamiliesToSnapshot 把 BuildFamilies 的输出归一化为可稳定序列化的快照。
func FamiliesToSnapshot(fams []Family) FamilySnapshot {
	out := FamilySnapshot{Families: make([]FamilySnap, 0, len(fams))}

	for _, fam := range fams {
		var primaryCS, primaryPath string
		for _, m := range fam.Members {
			if m.UniqueID == fam.PrimaryID {
				primaryCS = m.ContentSign
				primaryPath = m.Path
				break
			}
		}

		members := make([]MemberSnap, 0, len(fam.Members))
		for _, m := range fam.Members {
			members = append(members, MemberSnap{
				ContentSign: m.ContentSign,
				Path:        m.Path,
				Relation:    m.Relation,
				Score:       m.Score,
				IsPrimary:   m.UniqueID == fam.PrimaryID,
			})
		}
		sort.Slice(members, func(i, j int) bool {
			return members[i].ContentSign < members[j].ContentSign
		})

		out.Families = append(out.Families, FamilySnap{
			PrimaryContentSign: primaryCS,
			PrimaryPath:        primaryPath,
			Algorithm:          fam.Algorithm,
			HighestScore:       fam.HighestScore,
			Members:            members,
		})
	}

	sort.Slice(out.Families, func(i, j int) bool {
		return out.Families[i].PrimaryContentSign < out.Families[j].PrimaryContentSign
	})

	return out
}

// MarshalSnapshot 把 snapshot 序列化为 indent=2 的 JSON（人类可读 + diff 友好）。
func MarshalSnapshot(s FamilySnapshot) ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// SaveSnapshot 把 snapshot 写到 path（用于首次 capture 基线）。
func SaveSnapshot(path string, s FamilySnapshot) error {
	data, err := MarshalSnapshot(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadSnapshot 从 path 读 snapshot（用于 verify）。
func LoadSnapshot(path string) (FamilySnapshot, error) {
	var s FamilySnapshot
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parse snapshot: %w", err)
	}
	return s, nil
}

// CompareSnapshots 对比两个 snapshot。返回空字符串表示相同；
// 否则返回多行人类可读 diff 描述，定位到具体家族 / 成员差异。
//
// 比对忽略 PrimaryPath 与 MemberSnap.Path（仅 debug 用，文件路径变化不该算回归）。
func CompareSnapshots(want, got FamilySnapshot) string {
	var diffs []string

	if len(want.Families) != len(got.Families) {
		diffs = append(diffs, fmt.Sprintf("family count: want=%d got=%d",
			len(want.Families), len(got.Families)))
	}

	wantMap := make(map[string]FamilySnap, len(want.Families))
	for _, f := range want.Families {
		wantMap[f.PrimaryContentSign] = f
	}
	gotMap := make(map[string]FamilySnap, len(got.Families))
	for _, f := range got.Families {
		gotMap[f.PrimaryContentSign] = f
	}

	allKeys := make(map[string]struct{})
	for k := range wantMap {
		allKeys[k] = struct{}{}
	}
	for k := range gotMap {
		allKeys[k] = struct{}{}
	}
	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, k := range sortedKeys {
		w, wOK := wantMap[k]
		g, gOK := gotMap[k]
		switch {
		case !wOK:
			diffs = append(diffs, fmt.Sprintf("family %s: EXTRA in got (not in want)", k))
		case !gOK:
			diffs = append(diffs, fmt.Sprintf("family %s: MISSING in got (in want)", k))
		default:
			diffs = append(diffs, diffFamily(k, w, g)...)
		}
	}

	if len(diffs) == 0 {
		return ""
	}
	return joinLines(diffs)
}

func diffFamily(key string, w, g FamilySnap) []string {
	var diffs []string
	if w.Algorithm != g.Algorithm {
		diffs = append(diffs, fmt.Sprintf("family %s: algorithm want=%q got=%q",
			key, w.Algorithm, g.Algorithm))
	}
	if w.HighestScore != g.HighestScore {
		diffs = append(diffs, fmt.Sprintf("family %s: highest_score want=%.10f got=%.10f",
			key, w.HighestScore, g.HighestScore))
	}
	if len(w.Members) != len(g.Members) {
		diffs = append(diffs, fmt.Sprintf("family %s: member count want=%d got=%d",
			key, len(w.Members), len(g.Members)))
	}

	wMembers := make(map[string]MemberSnap, len(w.Members))
	for _, m := range w.Members {
		wMembers[m.ContentSign] = m
	}
	gMembers := make(map[string]MemberSnap, len(g.Members))
	for _, m := range g.Members {
		gMembers[m.ContentSign] = m
	}
	allCS := make(map[string]struct{})
	for cs := range wMembers {
		allCS[cs] = struct{}{}
	}
	for cs := range gMembers {
		allCS[cs] = struct{}{}
	}
	sortedCS := make([]string, 0, len(allCS))
	for cs := range allCS {
		sortedCS = append(sortedCS, cs)
	}
	sort.Strings(sortedCS)

	for _, cs := range sortedCS {
		wm, wOK := wMembers[cs]
		gm, gOK := gMembers[cs]
		switch {
		case !wOK:
			diffs = append(diffs, fmt.Sprintf("family %s member %s: EXTRA", key, cs))
		case !gOK:
			diffs = append(diffs, fmt.Sprintf("family %s member %s: MISSING", key, cs))
		default:
			if wm.IsPrimary != gm.IsPrimary {
				diffs = append(diffs, fmt.Sprintf("family %s member %s: is_primary want=%v got=%v",
					key, cs, wm.IsPrimary, gm.IsPrimary))
			}
			if wm.Relation != gm.Relation {
				diffs = append(diffs, fmt.Sprintf("family %s member %s: relation want=%q got=%q",
					key, cs, wm.Relation, gm.Relation))
			}
			if wm.Score != gm.Score {
				diffs = append(diffs, fmt.Sprintf("family %s member %s: score want=%.10f got=%.10f",
					key, cs, wm.Score, gm.Score))
			}
		}
	}
	return diffs
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
