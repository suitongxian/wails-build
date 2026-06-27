package scanner

import (
	"crypto/sha256"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"time"
	"unicode"

	"data-asset-scan-go/internal/textextract"
)

// CachedFeatures 是扫描期算好准备写入 data_distributing 的特征集合。
type CachedFeatures struct {
	Simhash       int64
	ContentHash   string
	Phash         string
	ExtractedText string
	Mtime         time.Time
	Size          int64
}

// ExtractFeaturesForCache 单文件特征计算入口。
// 与 internal/similarity/similarity.go:extractFeature 等价的特征集合，
// 但去掉了图片 phash（图片走 similarity 包 pHashFile，独立任务再加）。
//
// 失败语义：返回 err 时调用方应记 warn 但不阻塞 hash 写入；features 字段会是零值。
// 当文件抽不出文本（如扫描版 PDF），返回 (zero features, nil) — 不算错。
func ExtractFeaturesForCache(path, mime string, info os.FileInfo) (CachedFeatures, error) {
	bucket := classifyBucket(mime)

	feat := CachedFeatures{
		Mtime: info.ModTime(),
		Size:  info.Size(),
	}

	if bucket == "doc" || bucket == "code" {
		text := textextract.ExtractTextWithTimeout(path, 10*time.Second)
		if text == "" {
			// 抽不出文本（扫描版 PDF / 损坏文件等）不算错
			return feat, nil
		}
		feat.ExtractedText = text
		feat.Simhash = int64(simhashFromText(text))
		feat.ContentHash = contentHashFromText(text)
	}

	return feat, nil
}

// classifyBucket 与 similarity.mimeBucket 同源；
// 此处独立实现避免 scanner → similarity 包循环依赖。
func classifyBucket(mimeStr string) string {
	if idx := strings.Index(mimeStr, ";"); idx >= 0 {
		mimeStr = strings.TrimSpace(mimeStr[:idx])
	}
	switch {
	case strings.HasPrefix(mimeStr, "image/"):
		return "img"
	case strings.HasPrefix(mimeStr, "video/"), strings.HasPrefix(mimeStr, "audio/"):
		return "media"
	case mimeStr == "application/pdf",
		strings.HasPrefix(mimeStr, "application/vnd.openxmlformats-"),
		mimeStr == "application/msword",
		mimeStr == "application/vnd.ms-excel",
		mimeStr == "application/vnd.ms-powerpoint",
		strings.HasPrefix(mimeStr, "text/plain"),
		strings.HasPrefix(mimeStr, "text/html"),
		mimeStr == "text/markdown":
		return "doc"
	case strings.HasPrefix(mimeStr, "text/"),
		mimeStr == "application/javascript",
		mimeStr == "text/javascript":
		return "code"
	}
	return "other"
}

const simhashCharLimit = 3000

func simhashFromText(text string) uint64 {
	runes := []rune(text)
	if len(runes) > simhashCharLimit {
		text = string(runes[:simhashCharLimit])
	}
	tokens := tokenizeText(text)
	var v [64]int
	for i := 0; i+1 < len(tokens); i++ {
		token := tokens[i] + "\x00" + tokens[i+1]
		h := fnv.New64a()
		h.Write([]byte(token))
		hv := h.Sum64()
		for bit := 0; bit < 64; bit++ {
			if (hv>>uint(bit))&1 == 1 {
				v[bit]++
			} else {
				v[bit]--
			}
		}
	}
	var result uint64
	for bit := 0; bit < 64; bit++ {
		if v[bit] > 0 {
			result |= 1 << uint(bit)
		}
	}
	return result
}

func contentHashFromText(text string) string {
	if text == "" {
		return ""
	}
	var buf strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(unicode.ToLower(r))
		}
	}
	normalized := buf.String()
	if len([]rune(normalized)) < 20 {
		return ""
	}
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h)
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0xF900 && r <= 0xFAFF)
}

func tokenizeText(text string) []string {
	var tokens []string
	var englishBuf strings.Builder

	flushEnglish := func() {
		w := englishBuf.String()
		if len([]rune(w)) >= 2 {
			tokens = append(tokens, w)
		}
		englishBuf.Reset()
	}

	for _, r := range text {
		if isCJK(r) {
			flushEnglish()
			tokens = append(tokens, string(r))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			englishBuf.WriteRune(unicode.ToLower(r))
		} else {
			flushEnglish()
		}
	}
	flushEnglish()
	return tokens
}
