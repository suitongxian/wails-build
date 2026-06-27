package ai

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// V4-Q1 §4.3 项目版本文件 AI 归目工具 — 规则匹配版（不依赖外部模型）
//
// 简版 §1.8 AI 准确率分阶段路径：
//   人工为主 60-70% → AI 自动+复核 80-90% → AI 为主+异常处理 95%+
// 本实现属于第二阶段，纯规则匹配（不上 LLM），目标准确率 80%+。
// 后续如有模型需求，替换 AutoClassifyPort 实现即可（V3-7 留下的接口契约）。

// RuleBasedClassifyAdapter 实现 AutoClassifyPort 的规则匹配版
//
// 评分逻辑（各项独立加分，最终按总分排名，归一化到 0-0.95）：
//   - file_name 扩展名匹配 allowed_file_types：+0.35（基础信号最强）
//   - file_name 包含 file_rule.file_name 关键词：+0.30
//   - file_name 或 path 包含 stage_name 关键词：+0.20
//   - file_name 或 path 包含 project_code 或 project_name 关键词：+0.20
//   - path 含 project_root（精准匹配）：+0.15
//   - metadata.summary 或 file_name 命中 naming_pattern 变量：+0.10
//
// 设置上限 0.95，留 5% 空间避免假阳性"绝对自信"。
type RuleBasedClassifyAdapter struct {
	catalog CatalogProvider
	topN    int // 返回前 N 条建议，默认 5
}

func NewRuleBasedClassifyAdapter(catalog CatalogProvider) *RuleBasedClassifyAdapter {
	return &RuleBasedClassifyAdapter{catalog: catalog, topN: 5}
}

// SetTopN 设置返回建议条数（仅用于测试 / 配置）
func (a *RuleBasedClassifyAdapter) SetTopN(n int) {
	if n > 0 {
		a.topN = n
	}
}

// scoredSuggestion 内部排名结构
type scoredSuggestion struct {
	candidate ProjectStageRuleSnapshot
	score     float64
	matched   []string
}

// Classify §7.9 AutoClassifyPort 实现
func (a *RuleBasedClassifyAdapter) Classify(ctx context.Context, in AutoClassifyInput) ([]ClassificationSuggestion, error) {
	if in.FileName == "" {
		return nil, nil
	}
	candidates, err := a.catalog.GetCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载归目候选: %w", err)
	}

	lowerFileName := strings.ToLower(in.FileName)
	lowerPath := strings.ToLower(in.Path)
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(in.FileName), "."))

	scored := make([]scoredSuggestion, 0, len(candidates))
	for _, c := range candidates {
		score := 0.0
		matched := []string{}

		// 1) 扩展名匹配 allowed_file_types
		if ext != "" {
			for _, allowed := range c.AllowedFileTypes {
				upper := strings.ToUpper(allowed)
				if upper == "*" || upper == ext {
					score += 0.35
					matched = append(matched, "扩展名匹配 "+upper)
					break
				}
			}
		}

		// 2) file_name 包含规则定义的文件名关键词
		ruleKw := tokenizeChinese(c.FileName)
		if hitsAny(lowerFileName, ruleKw) {
			score += 0.30
			matched = append(matched, "文件名含规则关键词 "+c.FileName)
		}

		// 3) file_name 或 path 含 stage_name 关键词
		stageKw := tokenizeChinese(c.StageName)
		if hitsAny(lowerFileName, stageKw) || hitsAny(lowerPath, stageKw) {
			score += 0.20
			matched = append(matched, "命中环节关键词 "+c.StageName)
		}

		// 4) file_name 或 path 含 project_code / project_name 关键词
		projectKw := append(tokenizeChinese(c.ProjectName), strings.ToLower(c.ProjectCode))
		if hitsAny(lowerFileName, projectKw) || hitsAny(lowerPath, projectKw) {
			score += 0.20
			matched = append(matched, "命中项目关键词 "+c.ProjectCode)
		}

		// 5) path 精确含项目编码
		if c.ProjectCode != "" && strings.Contains(lowerPath, strings.ToLower(c.ProjectCode)) {
			score += 0.15
			matched = append(matched, "路径含项目编码")
		}

		// 6) summary / metadata 命中 naming_pattern 变量名（简化：拆 {} 内容）
		patternVars := extractPatternVars(c.NamingPattern)
		if len(patternVars) > 0 {
			searchText := strings.ToLower(in.Summary + " " + in.FileName)
			for _, v := range patternVars {
				if strings.Contains(searchText, strings.ToLower(v)) {
					score += 0.10
					matched = append(matched, "命名规则变量 "+v)
					break
				}
			}
		}

		// 7) MIME 精确匹配（已有 ext 匹配的基础上，给精确 mime 多加分）
		//    支持两种命中方式：
		//      a) MIME 字符串直接包含扩展名（如 "image/png" 含 "png"）
		//      b) MIME 通过 mimeToExt 映射到扩展名（如 xlsx 的 MIME ↔ xlsx）
		if mime, ok := in.Metadata["mime"]; ok && mime != "" {
			lowerMime := strings.ToLower(mime)
			mappedExt := mimeToExt(lowerMime)
			for _, allowed := range c.AllowedFileTypes {
				if allowed == "*" {
					continue
				}
				lowerAllowed := strings.ToLower(allowed)
				if strings.Contains(lowerMime, lowerAllowed) || (mappedExt != "" && mappedExt == lowerAllowed) {
					score += 0.08
					matched = append(matched, "MIME 匹配 "+allowed)
					break
				}
			}
		}

		// 8) 同目录 sibling_count 高 + 规则接受 * 类型 → 像聚合目录
		if scStr, ok := in.Metadata["sibling_count"]; ok && scStr != "" {
			if sc, err := strconv.Atoi(scStr); err == nil && sc >= 5 {
				acceptsAll := false
				for _, t := range c.AllowedFileTypes {
					if t == "*" {
						acceptsAll = true
						break
					}
				}
				if acceptsAll {
					score += 0.05
					matched = append(matched, fmt.Sprintf("聚合目录(%d 同类)", sc))
				}
			}
		}

		// 9) 正文片段（首 200 字）含 stage_name 或 rule.file_name
		if in.Summary != "" {
			body := in.Summary
			// 按 rune 截 200 字（中文 3 byte/字，按 byte 截会乱码）
			runes := []rune(body)
			if len(runes) > 200 {
				body = string(runes[:200])
			}
			lowerBody := strings.ToLower(body)
			if c.StageName != "" && strings.Contains(lowerBody, strings.ToLower(c.StageName)) {
				score += 0.10
				matched = append(matched, "正文含环节关键词 "+c.StageName)
			}
			if c.FileName != "" && strings.Contains(lowerBody, strings.ToLower(c.FileName)) {
				score += 0.10
				matched = append(matched, "正文含文件名关键词 "+c.FileName)
			}
		}

		// 上限 0.95
		if score > 0.95 {
			score = 0.95
		}

		if score > 0 {
			scored = append(scored, scoredSuggestion{candidate: c, score: score, matched: matched})
		}
	}

	// 按分数降序
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// 截断 topN
	if len(scored) > a.topN {
		scored = scored[:a.topN]
	}

	out := make([]ClassificationSuggestion, 0, len(scored))
	for _, s := range scored {
		out = append(out, ClassificationSuggestion{
			ProjectID:    s.candidate.ProjectID,
			ProjectCode:  s.candidate.ProjectCode,
			StageCode:    s.candidate.StageCode,
			StageName:    s.candidate.StageName,
			FileRuleCode: s.candidate.FileRuleCode,
			FileName:     s.candidate.FileName,
			DataState:    s.candidate.DataState,
			Confidence:   s.score,
			Reason:       strings.Join(s.matched, "；"),
			MatchedRules: s.matched,
		})
	}
	return out, nil
}

// ===== helpers =====

// tokenizeChinese 把中文/英文混合字符串切成关键词集合
//
// 策略：
//   - 全转小写
//   - 按非字母数字 + 全角中文非汉字 切分
//   - 过滤长度 < 2 的 token（避免"的"、"a" 类噪声）
//   - 同时把整段当作一个 token 加进去（用于精确包含匹配）
//
// 这是 MVP 极简版；未来如要更准可上 jieba / pkuseg 中文分词。
func tokenizeChinese(s string) []string {
	if s == "" {
		return nil
	}
	lower := strings.ToLower(s)
	out := []string{lower}
	// 简单切分：用常见分隔符 + 全角符号
	seps := []string{" ", "_", "-", "/", "\\", ".", "(", ")", "（", "）", "【", "】", "·", "—", ",", "，"}
	tmp := lower
	for _, sep := range seps {
		tmp = strings.ReplaceAll(tmp, sep, " ")
	}
	for _, w := range strings.Fields(tmp) {
		if utf8Len(w) >= 2 {
			out = append(out, w)
		}
	}
	return dedup(out)
}

// hitsAny 文本含任意一个关键词
func hitsAny(text string, keywords []string) bool {
	if text == "" || len(keywords) == 0 {
		return false
	}
	for _, k := range keywords {
		if k != "" && strings.Contains(text, k) {
			return true
		}
	}
	return false
}

// extractPatternVars 从 "排版稿-{书名}-V{版本}" 抽出 ["书名", "版本"]
func extractPatternVars(pattern string) []string {
	if pattern == "" {
		return nil
	}
	out := []string{}
	depth := 0
	cur := strings.Builder{}
	for _, r := range pattern {
		if r == '{' {
			depth = 1
			cur.Reset()
		} else if r == '}' && depth == 1 {
			depth = 0
			v := cur.String()
			if v != "" {
				out = append(out, v)
			}
		} else if depth == 1 {
			cur.WriteRune(r)
		}
	}
	return out
}

// utf8Len 返回字符串的 rune 数（考虑 utf-8 多字节）
func utf8Len(s string) int {
	return len([]rune(s))
}

// mimeToExt 常见 MIME → 扩展名 映射（小写），MIME 字符串不含扩展名时兜底
//
// 仅覆盖业务高频类型；命中失败返回空串（不阻断主流程）。
func mimeToExt(lowerMime string) string {
	switch {
	case strings.Contains(lowerMime, "spreadsheetml.sheet"):
		return "xlsx"
	case strings.Contains(lowerMime, "ms-excel"):
		return "xls"
	case strings.Contains(lowerMime, "wordprocessingml.document"):
		return "docx"
	case strings.Contains(lowerMime, "msword"):
		return "doc"
	case strings.Contains(lowerMime, "presentationml.presentation"):
		return "pptx"
	case strings.Contains(lowerMime, "ms-powerpoint"):
		return "ppt"
	case strings.Contains(lowerMime, "application/pdf"):
		return "pdf"
	case strings.Contains(lowerMime, "text/plain"):
		return "txt"
	case strings.Contains(lowerMime, "text/markdown"):
		return "md"
	case strings.Contains(lowerMime, "image/jpeg"):
		return "jpg"
	case strings.Contains(lowerMime, "image/png"):
		return "png"
	case strings.Contains(lowerMime, "image/gif"):
		return "gif"
	case strings.Contains(lowerMime, "image/webp"):
		return "webp"
	case strings.Contains(lowerMime, "application/zip"):
		return "zip"
	}
	return ""
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// 编译期保证 RuleBasedClassifyAdapter 实现了 AutoClassifyPort
var _ AutoClassifyPort = (*RuleBasedClassifyAdapter)(nil)
