// Package textextract provides plain-text extraction from common document formats:
// docx/dotx, pdf, ole (doc/xls/ppt 旧格式), rtf, html, mht, xml (word), plain text.
//
// 唯一真理源 —— similarity 包和 ai 包共同使用，避免逻辑分叉。
package textextract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"data-asset-scan-go/internal/sysproc"

	code_sajari_docconv "code.sajari.com/docconv/v2"
	"github.com/gabriel-vasile/mimetype"
	"github.com/ledongthuc/pdf"
	"github.com/richardlehane/mscfb"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ============================================================
// 内部常量与配置 —— 从 similarity 包搬迁，逻辑保持完全一致
// ============================================================

const (
	defaultMaxReadBytes = 500 * 1024 * 1024 // 500 MB - 读取文件内容的最大限制
	maxZipSize          = 500 * 1024 * 1024 // 500 MB - ZIP 解压最大限制
	maxXMLTokens        = 1000000           // XML 解析最大 token 数
)

// verbose 控制日志输出，与 similarity 包内同名变量保持默认 false。
var verbose bool

var plainTextExts = map[string]bool{
	"txt": true, "md": true, "py": true, "go": true, "java": true,
	"js": true, "ts": true, "c": true, "cpp": true, "h": true, "cs": true, "rb": true,
}

// oleDocExts 是基于 OLE Compound File Binary 格式的文档扩展名（.doc/.dot/.wps/.wpt 等）
var oleDocExts = map[string]bool{
	"doc": true, "dot": true, "wps": true, "wpt": true,
}

// HTML 相关的正则表达式（搬自 similarity 包，提取文本时使用）
var (
	reHTMLTag     = regexp.MustCompile(`<[^>]*>`)
	reHTMLCharset = regexp.MustCompile(`(?i)charset\s*=\s*["']?\s*([^"'\s;>]+)`)
	reHTMLStyle   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reHTMLScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reHTMLBody    = regexp.MustCompile(`(?is)<body[^>]*>(.*?)</body>`)
)

// ============================================================
// 公开 API
// ============================================================

// ExtractText 从给定文件路径读取并提取纯文本。
// 不支持的格式或读取失败返回 ""。
//
// 源头 panic 防御：第三方解析库（ledongthuc/pdf 等）可能因损坏文件 panic，
// 任何 goroutine panic 没被 recover 会杀整个进程。这里在 ExtractText 内部
// defer recover，让所有调用方（同步、goroutine、未来新增 caller）都自动安全。
// 命名返回 text 保证 recover 路径返回零值 ""。
func ExtractText(path string) (text string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("warn: text extraction panicked for %s: %v", path, r)
			text = ""
		}
	}()

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))

	// 检测真实 MIME 类型，优先按实际格式路由，避免扩展名与内容不匹配的问题
	// （例如加密的 .docx 实际是 OLE 格式）
	mtype, mErr := mimetype.DetectFile(path)
	mime := ""
	if mErr == nil {
		mime = mtype.String()
		// 剥离 MIME 参数（如 "; charset=UTF-8"），只保留主类型
		if idx := strings.Index(mime, ";"); idx >= 0 {
			mime = strings.TrimSpace(mime[:idx])
		}
	}

	if plainTextExts[ext] && !isOLEMime(mime) {
		f, err := os.Open(path)
		if err != nil {
			if verbose {
				log.Printf("warn: cannot open %s: %v", path, err)
			}
			return ""
		}
		defer f.Close()
		// 只读取前 defaultMaxReadBytes 字节
		lr := &io.LimitedReader{R: f, N: defaultMaxReadBytes}
		data, err := io.ReadAll(lr)
		if err != nil {
			if verbose {
				log.Printf("warn: cannot read %s: %v", path, err)
			}
			return ""
		}
		return string(data)
	}

	// OLE 格式优先判断（覆盖加密 docx、伪装扩展名等场景）
	if isOLEMime(mime) {
		return extractOLEDocText(path)
	}
	if ext == "docx" || ext == "dotx" || isZipOfficeMime(mime) {
		return extractDocxText(path)
	}
	if ext == "pdf" || mime == "application/pdf" {
		return extractPDFText(path)
	}
	if oleDocExts[ext] {
		return extractOLEDocText(path)
	}
	if ext == "rtf" || mime == "text/rtf" || mime == "application/rtf" {
		return extractRTFText(path)
	}
	if ext == "htm" || ext == "html" {
		return extractHTMLText(path)
	}
	if ext == "xml" {
		return extractWordXMLText(path)
	}
	if ext == "mht" || ext == "mhtml" {
		return extractMHTText(path)
	}
	return ""
}

// ExtractTextWithTimeout 带超时的文本提取，防止损坏/加密文件卡住预提取阶段。
// 内部派生 goroutine 调 ExtractText；如果 ExtractText 或它调用的第三方解析器
// （ledongthuc/pdf 等）panic，会被 extractTextWithRecover 捕获，**不会**杀整个进程。
func ExtractTextWithTimeout(path string, timeout time.Duration) string {
	return extractTextWithRecover(path, ExtractText, timeout)
}

// extractTextWithRecover 是 ExtractTextWithTimeout 的可测内核。
// 把 extractor 作为参数注入，使得测试可以传入 panic-emitting 的桩函数验证 recover 行为。
// 任何 panic 都会被捕获并 log，返回空串（与文件不可读 / 不支持格式等失败路径一致）。
func extractTextWithRecover(path string, extractor func(string) string, timeout time.Duration) string {
	ch := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// 不 gate 在 verbose：用户必须知道是哪个文件让第三方库炸了，
				// 才能 issue / 隔离 / 自查损坏文件。
				log.Printf("warn: text extraction panicked for %s: %v", path, r)
				// 容量 1 的 buffered channel + 正常路径只发一次 → 这里发空串总能成功。
				ch <- ""
			}
		}()
		ch <- extractor(path)
	}()
	select {
	case text := <-ch:
		return text
	case <-time.After(timeout):
		if verbose {
			log.Printf("warn: text extraction timed out for %s", path)
		}
		return ""
	}
}

// ============================================================
// 内部 helper / 各种格式的提取实现
// ============================================================

// isOLEMime 判断 MIME 类型是否为 OLE 复合文档格式
func isOLEMime(mime string) bool {
	return mime == "application/x-ole-storage" ||
		mime == "application/msword" ||
		mime == "application/vnd.ms-excel" ||
		mime == "application/vnd.ms-powerpoint"
}

// isZipOfficeMime 判断 MIME 类型是否为 ZIP-based Office 格式（docx/xlsx/pptx 等）
func isZipOfficeMime(mime string) bool {
	return strings.HasPrefix(mime, "application/vnd.openxmlformats-officedocument.")
}

// pdftotextOnce 保证只检测一次 pdftotext 是否可用。
var (
	pdftotextOnce  sync.Once
	pdftotextAvail bool
	pdftotextBin   string
)

func hasPdftotext() bool {
	pdftotextOnce.Do(func() {
		if bin, err := exec.LookPath("pdftotext"); err == nil {
			pdftotextBin = bin
			pdftotextAvail = true
			if verbose {
				log.Printf("info: pdftotext found at %s, will use as primary PDF extractor", bin)
			}
		}
	})
	return pdftotextAvail
}

// extractPDFTextViaPdftotext 调用系统 pdftotext（poppler）提取文本。
// pdftotext 对 CJK 字体、CNKI 专有格式等的处理远优于纯 Go 库。
func extractPDFTextViaPdftotext(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// "-enc UTF-8" 强制 UTF-8 输出；"-" 表示输出到 stdout
	cmd := exec.CommandContext(ctx, pdftotextBin, "-enc", "UTF-8", "-nopgbrk", path, "-")
	sysproc.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		if verbose {
			log.Printf("warn: pdftotext failed for %s: %v", path, err)
		}
		return ""
	}
	result := string(out)
	if len(result) > defaultMaxReadBytes {
		result = result[:defaultMaxReadBytes]
	}
	return result
}

func extractPDFText(path string) string {
	stat, err := os.Stat(path)
	if err != nil {
		if verbose {
			log.Printf("warn: cannot stat %s: %v", path, err)
		}
		return ""
	}
	if stat.Size() > maxZipSize {
		if verbose {
			log.Printf("warn: skipping large pdf %s (%d bytes)", path, stat.Size())
		}
		return ""
	}

	// 优先使用 pdftotext（poppler），对 CJK 字体、CNKI 专有格式等处理更全面。
	if hasPdftotext() {
		if text := extractPDFTextViaPdftotext(path); text != "" {
			return text
		}
	}

	// 回退：使用 ledongthuc/pdf 进行位置推断提取。
	f, r, err := pdf.Open(path)
	if err != nil {
		// 部分 PDF（如 CNKI 知网下载的文件）会在标准 %%EOF 标记之后追加专有元数据，
		// 导致严格检查文件末尾的解析器报错。尝试截断到最后一个 %%EOF 位置再解析。
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			if idx := bytes.LastIndex(data, []byte("%%EOF")); idx >= 0 {
				truncated := data[:idx+5] // 5 = len("%%EOF")
				if r2, parseErr := pdf.NewReader(bytes.NewReader(truncated), int64(len(truncated))); parseErr == nil {
					r = r2
					goto extractPages
				}
			}
		}
		if verbose {
			log.Printf("warn: cannot open pdf %s: %v", path, err)
		}
		return ""
	}
	defer f.Close()
extractPages:

	var sb strings.Builder
	for pageNum := 1; pageNum <= r.NumPage(); pageNum++ {
		page := r.Page(pageNum)
		if page.V.IsNull() {
			continue
		}
		content := page.Content()
		texts := content.Text
		if len(texts) == 0 {
			continue
		}

		// 按 Y 坐标降序（上方先），同 Y 的元素保留内容流原始顺序。
		// PDF 坐标原点在左下角，Y 值大表示页面偏上。
		// 注意：不按 X 二次排序，因为部分中文 PDF 的字符 X 坐标随阅读方向递减，
		// 强制按 X 升序会翻转整行文字。内容流原始顺序通常即为阅读顺序。
		sort.SliceStable(texts, func(i, j int) bool {
			ti, tj := texts[i], texts[j]
			if math.Abs(ti.Y-tj.Y) > 2 {
				return ti.Y > tj.Y
			}
			return false
		})

		prev := texts[0]
		sb.WriteString(prev.S)
		for _, cur := range texts[1:] {
			dy := math.Abs(cur.Y - prev.Y)
			lineH := prev.FontSize
			if lineH <= 0 {
				lineH = 10 // 默认字号 10pt
			}
			if dy > lineH*0.5 {
				// Y 偏移超过半个行高 → 换行
				sb.WriteByte('\n')
			} else {
				// 同一行：判断水平间距是否足以插入空格。
				// 词间距通常约为字号的 20–40%；字符内部间距通常 < 5%。
				gap := cur.X - (prev.X + prev.W)
				if gap > lineH*0.15 {
					sb.WriteByte(' ')
				}
			}
			sb.WriteString(cur.S)
			prev = cur
		}
		sb.WriteByte('\n')
	}

	result := sb.String()
	if len(result) > defaultMaxReadBytes {
		result = result[:defaultMaxReadBytes]
	}
	return result
}

func extractDocxText(path string) string {
	// 先检查文件大小
	stat, err := os.Stat(path)
	if err != nil {
		if verbose {
			log.Printf("warn: cannot stat %s: %v", path, err)
		}
		return ""
	}
	if stat.Size() > maxZipSize {
		if verbose {
			log.Printf("warn: skipping large docx %s (%d bytes)", path, stat.Size())
		}
		return ""
	}

	r, err := zip.OpenReader(path)
	if err != nil {
		if verbose {
			log.Printf("warn: cannot open docx %s: %v", path, err)
		}
		return ""
	}
	defer r.Close()

	// 检查 ZIP 文件数量，防止炸弹
	if len(r.File) > 1000 {
		if verbose {
			log.Printf("warn: docx has too many files: %s", path)
		}
		return ""
	}

	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		// 检查单个文件大小
		if f.UncompressedSize64 > uint64(maxZipSize) {
			if verbose {
				log.Printf("warn: docx inner file too large: %s", path)
			}
			return ""
		}
		rc, err := f.Open()
		if err != nil {
			if verbose {
				log.Printf("warn: cannot read docx inner file %s: %v", path, err)
			}
			return ""
		}
		// 限制读取大小
		lr := &io.LimitedReader{R: rc, N: int64(maxZipSize)}
		result := parseWordXML(lr)
		rc.Close() // 手动关闭，不用 defer 在循环里
		return result
	}
	return ""
}

func parseWordXML(r io.Reader) string {
	var parts []string
	dec := xml.NewDecoder(r)
	tokenCount := 0

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}

		tokenCount++
		if tokenCount > maxXMLTokens {
			break
		}

		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "t" {
			continue
		}
		var s string
		if err := dec.DecodeElement(&s, &se); err == nil && s != "" {
			parts = append(parts, s)
		}

		// 限制总文本长度
		if len(parts) > 10000 {
			break
		}
	}
	return strings.Join(parts, " ")
}

// extractWordXMLText 从 Word 另存为的 XML 文件中提取文本。
// Word 另存为 XML 的格式与 docx 内部的 word/document.xml 结构相同，
// 文本内容存储在 <w:t> 标签中，因此复用 parseWordXML 解析器。
func extractWordXMLText(path string) string {
	stat, err := os.Stat(path)
	if err != nil {
		if verbose {
			log.Printf("warn: cannot stat %s: %v", path, err)
		}
		return ""
	}
	if stat.Size() > maxZipSize {
		if verbose {
			log.Printf("warn: skipping large xml %s (%d bytes)", path, stat.Size())
		}
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		if verbose {
			log.Printf("warn: cannot open xml %s: %v", path, err)
		}
		return ""
	}
	defer f.Close()
	lr := &io.LimitedReader{R: f, N: int64(maxZipSize)}
	text := parseWordXML(lr)
	if text != "" {
		return text
	}
	// 如果没有 <w:t> 标签，回退为纯文本读取
	f.Seek(0, io.SeekStart)
	data, err := io.ReadAll(&io.LimitedReader{R: f, N: defaultMaxReadBytes})
	if err != nil {
		return ""
	}
	return string(data)
}

// extractOLEDocText 从 OLE Compound File Binary 文档（.doc/.dot/.wps）中提取文本。
// 原理：用 mscfb 打开 CFB 容器，找到 "WordDocument" 流，然后扫描其中的 UTF-16 LE 文字序列。
func extractOLEDocText(path string) string {
	stat, err := os.Stat(path)
	if err != nil || stat.Size() > maxZipSize {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	doc, err := mscfb.New(f)
	if err != nil {
		if verbose {
			log.Printf("warn: cannot open OLE file %s: %v", path, err)
		}
		return ""
	}

	// 依次遍历 CFB 目录项，找 WordDocument 流（.doc/.dot/.wps 均使用此流名）
	for {
		entry, err := doc.Next()
		if err != nil {
			break
		}
		if entry.Name != "WordDocument" {
			continue
		}
		lr := &io.LimitedReader{R: entry, N: defaultMaxReadBytes}
		data, err := io.ReadAll(lr)
		if err != nil {
			if verbose {
				log.Printf("warn: cannot read WordDocument stream %s: %v", path, err)
			}
			return ""
		}
		return scanUTF16LEText(data)
	}
	return ""
}

// scanUTF16LEText 扫描二进制数据中的文字序列。
// .doc 格式可能使用 ANSI（单字节）或 UTF-16 LE（双字节）存储文本。
// 优先尝试 ANSI 扫描（因为 true UTF-16LE 的 ASCII 区间每隔一个字节有 0x00，
// ANSI 扫描不会误匹配到长 run），如果 ANSI 不够则回退到 UTF-16LE。
func scanUTF16LEText(data []byte) string {
	ansi := scanANSIText(data)
	utf16 := scanUTF16LEOnly(data)
	// 优先选择 ANSI：如果提取到足够多的文本就用 ANSI
	ansiRunes := len([]rune(ansi))
	utf16Runes := len([]rune(utf16))
	if ansiRunes >= 50 && ansiRunes >= utf16Runes/3 {
		return ansi
	}
	if utf16Runes >= 50 {
		return utf16
	}
	if ansiRunes > utf16Runes {
		return ansi
	}
	return utf16
}

// scanUTF16LEOnly 按 UTF-16 LE 编码扫描文字序列。
func scanUTF16LEOnly(data []byte) string {
	var sb strings.Builder
	const minRunLen = 4
	i := 0
	for i+1 < len(data) {
		r := rune(data[i]) | rune(data[i+1])<<8
		if isOLETextRune(r) {
			var run []rune
			j := i
			for j+1 < len(data) {
				r2 := rune(data[j]) | rune(data[j+1])<<8
				if !isOLETextRune(r2) {
					break
				}
				run = append(run, r2)
				j += 2
			}
			if len(run) >= minRunLen {
				sb.WriteString(string(run))
				sb.WriteByte(' ')
			}
			i = j
		} else {
			i++
		}
	}
	return strings.TrimSpace(sb.String())
}

// scanANSIText 扫描二进制数据中的 ANSI（单字节）可读文字序列。
// 用于 .doc 文件中文本以 ANSI 编码存储的情况。
func scanANSIText(data []byte) string {
	var sb strings.Builder
	const minRunLen = 6
	i := 0
	for i < len(data) {
		b := data[i]
		if b >= 0x20 && b <= 0x7E {
			// 收集连续可打印 ASCII 字符
			j := i
			for j < len(data) && data[j] >= 0x20 && data[j] <= 0x7E {
				j++
			}
			if j-i >= minRunLen {
				sb.Write(data[i:j])
				sb.WriteByte(' ')
			}
			i = j
		} else {
			i++
		}
	}
	return strings.TrimSpace(sb.String())
}

// isOLETextRune 判断一个 Unicode 码点是否属于"有意义的可读字符"。
// 覆盖 ASCII 可打印字符、CJK 汉字、全角符号、常用标点等。
func isOLETextRune(r rune) bool {
	switch {
	case r >= 0x0020 && r <= 0x007E: // ASCII 可打印
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK 统一表意文字
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK 扩展 A
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK 兼容汉字
		return true
	case r >= 0x3000 && r <= 0x303F: // CJK 符号和标点
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // 全角/半角字符
		return true
	case r >= 0x2000 && r <= 0x206F: // 通用标点
		return true
	default:
		return false
	}
}

// hexNibble 把单个十六进制字符转为数值，非法字符返回 -1。
func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// extractRTFText 从 RTF 文件中提取纯文本。
// 优先使用 docconv（unrtf），若提取质量低（大量 ? 占位符）则回退到内置解析器。
func extractRTFText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		text, _, err := code_sajari_docconv.ConvertRTF(f)
		ch <- result{text, err}
	}()

	select {
	case <-time.After(30 * time.Second):
		return ""
	case res := <-ch:
		if res.err != nil {
			return extractRTFTextFallback(path)
		}
		text := strings.TrimSpace(res.text)
		// unrtf 处理 \uN Unicode 转义失败时会把中文全变成 ?，
		// 检测这个特征：文本中 ? 占比过高说明提取质量差，回退到内置解析器
		if isRTFExtractionPoor(text) {
			return extractRTFTextFallback(path)
		}
		return text
	}
}

// isRTFExtractionPoor 检测 RTF 提取质量是否过低。
// unrtf 无法处理 \uN Unicode 转义时会输出大量 ? 占位符，
// 无法处理 GBK 等编码时会输出非 UTF-8 的乱码，此时认为提取失败。
func isRTFExtractionPoor(text string) bool {
	if len(text) == 0 {
		return true
	}
	// 如果文本包含大量非 UTF-8 字节（unrtf 解码失败特征），认为是低质量
	if !utf8.ValidString(text) {
		return true
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return true
	}
	questionCount := 0
	for _, r := range runes {
		if r == '?' {
			questionCount++
		}
	}
	// 如果 ? 占总字符数超过 30%，认为是低质量提取
	return float64(questionCount)/float64(len(runes)) > 0.3
}

// extractRTFTextFallback 内置 GBK 解析器，已弃用，仅作保留。
// 当前使用 docconv（unrtf）替代。
func extractRTFTextFallback(path string) string {
	stat, err := os.Stat(path)
	if err != nil || stat.Size() > maxZipSize {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	lr := &io.LimitedReader{R: f, N: defaultMaxReadBytes}
	data, err := io.ReadAll(lr)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	var hexGroup []byte
	depth := 0
	skipGroup := false
	skipDepth := 0

	flushHexGroup := func() {
		if len(hexGroup) >= 2 {
			decoded, decErr := simplifiedchinese.GBK.NewDecoder().Bytes(hexGroup)
			if decErr == nil {
				sb.WriteString(string(decoded))
			}
		}
		hexGroup = hexGroup[:0]
	}

	isMetaDest := func(word string) bool {
		switch word {
		case "fonttbl", "colortbl", "stylesheet", "info", "pict", "object",
			"fldinst", "header", "footer", "headerl", "headerr", "headerf",
			"footerl", "footerr", "footerf", "footnote", "annotation",
			"atnid", "atnicn", "atnref", "atndate", "atnauthor",
			"listtable", "listoverridetable", "revtbl", "rsidtbl",
			"xmlnstbl", "mmathPr", "generator", "datastore",
			"themedata", "colorschememapping", "latentstyles",
			"datafield", "formfield", "panose", "falt",
			"shp", "shpinst", "sp", "sn", "sv",
			"shprslt", "shptxt",
			"picprop", "blipuid",
			"background", "pgdsctbl", "wgrffmtfilter",
			"pnseclvl",
			"mmathPict", "mmath":
			return true
		}
		return false
	}

	i := 0
	for i < len(data) {
		ch := data[i]

		switch {
		case ch == '{':
			depth++
			i++
		case ch == '}':
			flushHexGroup()
			if depth > 0 {
				depth--
			}
			if skipGroup && depth < skipDepth {
				skipGroup = false
			}
			i++
		case ch == '\\':
			i++
			if i >= len(data) {
				break
			}
			next := data[i]

			if next == '\'' && i+2 < len(data) {
				hi := hexNibble(data[i+1])
				lo := hexNibble(data[i+2])
				if hi >= 0 && lo >= 0 && !skipGroup {
					hexGroup = append(hexGroup, byte(hi<<4|lo))
				}
				i += 3
			} else if next == '\\' || next == '{' || next == '}' {
				if !skipGroup {
					flushHexGroup()
					sb.WriteByte(next)
				}
				i++
			} else if next == '~' {
				if !skipGroup {
					flushHexGroup()
					sb.WriteByte(' ')
				}
				i++
			} else if next == '\n' || next == '\r' {
				i++
			} else if next == 'u' {
				// \uN Unicode 转义：直接追加对应字符
				i++
				start := i
				for i < len(data) && data[i] >= '0' && data[i] <= '9' {
					i++
				}
				if i > start && !skipGroup {
					var u rune
					fmt.Fscanf(strings.NewReader(string(data[start:i])), "%d", &u)
					if u >= 0 {
						sb.WriteRune(u)
					}
				}
				// 跳过后续 \'N 替代字符（RTF 规范要求）
				for i+1 < len(data) && data[i] == '\'' {
					i++
					if i < len(data) && (data[i] == '-' || (data[i] >= '0' && data[i] <= '9')) {
						i++
						for i < len(data) && data[i] >= '0' && data[i] <= '9' {
							i++
						}
					}
				}
			} else {
				j := i
				for j < len(data) && ((data[j] >= 'a' && data[j] <= 'z') || (data[j] >= 'A' && data[j] <= 'Z')) {
					j++
				}
				word := string(data[i:j])
				for j < len(data) && ((data[j] >= '0' && data[j] <= '9') || data[j] == '-') {
					j++
				}
				if j < len(data) && data[j] == ' ' {
					j++
				}

				if word == "par" || word == "line" {
					if !skipGroup {
						flushHexGroup()
						sb.WriteByte('\n')
					}
				} else if word == "tab" {
					if !skipGroup {
						flushHexGroup()
						sb.WriteByte(' ')
					}
				}
				if isMetaDest(word) || word == "*" {
					if !skipGroup {
						skipGroup = true
						skipDepth = depth
					}
				}
				i = j
			}
		case ch == '\n' || ch == '\r':
			i++
		default:
			if !skipGroup && ch >= 0x20 && ch <= 0x7E {
				flushHexGroup()
				sb.WriteByte(ch)
			}
			i++
		}
	}
	flushHexGroup()

	result := strings.TrimSpace(sb.String())
	runes := []rune(result)
	contentStart := 0
	for contentStart < len(runes) {
		runStart := contentStart
		for runStart < len(runes) && !unicode.IsLetter(runes[runStart]) && !unicode.IsDigit(runes[runStart]) {
			runStart++
		}
		runEnd := runStart
		for runEnd < len(runes) && (unicode.IsLetter(runes[runEnd]) || unicode.IsDigit(runes[runEnd]) || runes[runEnd] == ' ') {
			runEnd++
		}
		if runEnd-runStart >= 20 {
			contentStart = runStart
			break
		}
		contentStart = runEnd
	}
	if contentStart > 0 && contentStart < len(runes) {
		result = string(runes[contentStart:])
	}
	return strings.TrimSpace(result)
}

// detectHTMLCharset 从 HTML 原始字节中提取 charset 声明（仅检查前 4KB）。
func detectHTMLCharset(raw []byte) string {
	search := raw
	if len(search) > 4096 {
		search = search[:4096]
	}
	m := reHTMLCharset.FindSubmatch(search)
	if len(m) >= 2 {
		return string(m[1])
	}
	return "utf-8"
}

// extractMHTText 从 MHT/MHTML 文件中提取纯文本。
// MHT 是 MIME 编码的网页存档格式（Word 另存为时生成），
// 核心内容在第一个 text/html 段中，提取后按 HTML 方式去标签。
func extractMHTText(path string) string {
	stat, err := os.Stat(path)
	if err != nil || stat.Size() > maxZipSize {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(raw)

	// 查找 boundary
	boundaryIdx := strings.Index(content, "boundary=\"")
	if boundaryIdx < 0 {
		// 没有 boundary，尝试整体按 HTML 处理（跳过 MIME 头部）
		if idx := strings.Index(content, "\r\n\r\n"); idx >= 0 {
			content = content[idx+4:]
		} else if idx := strings.Index(content, "\n\n"); idx >= 0 {
			content = content[idx+2:]
		}
		return extractHTMLFromString(content)
	}
	boundaryStart := boundaryIdx + len("boundary=\"")
	boundaryEnd := strings.Index(content[boundaryStart:], "\"")
	if boundaryEnd < 0 {
		return ""
	}
	boundary := content[boundaryStart : boundaryStart+boundaryEnd]

	// 按 boundary 分割，找第一个 Content-Type: text/html 的部分
	parts := strings.Split(content, "--"+boundary)
	for _, part := range parts {
		lower := strings.ToLower(part)
		if strings.Contains(lower, "content-type: text/html") {
			body := part
			if idx := strings.Index(part, "\r\n\r\n"); idx >= 0 {
				body = part[idx+4:]
			} else if idx := strings.Index(part, "\n\n"); idx >= 0 {
				body = part[idx+2:]
			}
			if strings.Contains(lower, "quoted-printable") {
				body = decodeMHTQuotedPrintable(body)
			}
			return extractHTMLFromString(body)
		}
	}
	return ""
}

// extractHTMLFromString 对内存中的 HTML 字符串进行去标签提取纯文本。
func extractHTMLFromString(html string) string {
	charset := detectHTMLCharset([]byte(html))
	var text string
	if strings.EqualFold(charset, "gb2312") || strings.EqualFold(charset, "gbk") {
		decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes([]byte(html))
		if err == nil {
			text = string(decoded)
		} else {
			text = html
		}
	} else {
		text = html
	}
	text = reHTMLStyle.ReplaceAllString(text, " ")
	text = reHTMLScript.ReplaceAllString(text, " ")
	if m := reHTMLBody.FindStringSubmatch(text); len(m) >= 2 {
		text = m[1]
	}
	text = reHTMLTag.ReplaceAllString(text, " ")
	return strings.Join(strings.Fields(text), " ")
}

// decodeMHTQuotedPrintable 解码 quoted-printable 编码的文本。
func decodeMHTQuotedPrintable(s string) string {
	var buf strings.Builder
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasSuffix(line, "=") {
			buf.WriteString(line[:len(line)-1])
		} else {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
	result := buf.String()
	var out strings.Builder
	for i := 0; i < len(result); i++ {
		if result[i] == '=' && i+2 < len(result) {
			hi := hexNibble(result[i+1])
			lo := hexNibble(result[i+2])
			if hi >= 0 && lo >= 0 {
				out.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		out.WriteByte(result[i])
	}
	return out.String()
}

// extractHTMLText 从 HTML 文件中提取纯文本。
// 自动检测 charset（支持 gb2312/gbk → UTF-8 转换），然后去除所有 HTML 标签。
func extractHTMLText(path string) string {
	stat, err := os.Stat(path)
	if err != nil || stat.Size() > maxZipSize {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	charset := detectHTMLCharset(raw)
	var text string
	if strings.EqualFold(charset, "gb2312") || strings.EqualFold(charset, "gbk") {
		decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw)
		if err == nil {
			text = string(decoded)
		} else {
			text = string(raw)
		}
	} else {
		text = string(raw)
	}

	// Remove style and script blocks (CSS rules, JS, metadata) before tag stripping,
	// otherwise their text content pollutes the document fingerprint.
	text = reHTMLStyle.ReplaceAllString(text, " ")
	text = reHTMLScript.ReplaceAllString(text, " ")
	// Extract only <body> content when present; this discards <head> metadata.
	if m := reHTMLBody.FindStringSubmatch(text); len(m) >= 2 {
		text = m[1]
	}
	text = reHTMLTag.ReplaceAllString(text, " ")
	return strings.Join(strings.Fields(text), " ")
}
