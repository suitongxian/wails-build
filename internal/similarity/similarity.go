package similarity

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"hash"
	"hash/fnv"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"data-asset-scan-go/internal/repository"
	"data-asset-scan-go/internal/textextract"

	"github.com/corona10/goimagehash"
	"github.com/disintegration/imaging"
	"github.com/gabriel-vasile/mimetype"
	"github.com/jmoiron/sqlx"
)

var (
	verbose            bool
	fileProcessTimeout = 3 * time.Second // 单个文件处理超时 (再缩短到3秒)
	maxFiles           = 200000          // 最大文件数量
	maxCandidatePairs  = 100000          // 最大候选对数量 (再降到5万)
	workerCount        = 8               // 并发worker数，利用多核CPU
)

// injectedDB is set by cmd/main.go at startup via SetDB. Tests may also call it.
// nil-safe: cache lookups fall through to live extraction when DB is not injected.
var injectedDB *sqlx.DB

// SetDB injects the DB connection used by BuildFamilies for feature cache lookups.
// Called once from main.go at startup; tests may also call it.
func SetDB(db *sqlx.DB) { injectedDB = db }

func getSimilarityDB() *sqlx.DB { return injectedDB }

// featurePrecomputeEnabled mirrors scanner.featurePrecomputeEnabled — checks system_config.
// When false, BuildFamilies skips DB cache lookup entirely (forces live extraction).
func featurePrecomputeEnabled() bool {
	db := getSimilarityDB()
	if db == nil {
		return true
	}
	repo := repository.NewSystemConfigRepository(db)
	v, err := repo.Get(repository.KeyFeaturePrecomputeEnabled)
	if err != nil {
		return true
	}
	return v != "false"
}

// extractTextHook lets tests intercept text extraction calls.
// Defaults to the real implementation.
var extractTextHook = extractTextWithTimeout

// ============================================================
// Config
// ============================================================

const (
	defaultMaxReadBytes = 500 * 1024 * 1024 // 500 MB - 读取文件内容的最大限制
	defaultSampleSize   = 32 * 1024 * 1024  // 32 MB - 大文件采样大小
	maxImageFileSize    = 50 * 1024 * 1024  // 50 MB - 图片文件最大大小（恢复50MB）
	maxImageDimension   = 10000             // 图片最大宽高（恢复10000）
)

type Config struct {
	ScanRootPath             string
	HashAlgorithm            string
	MaxFileSizeGB            float64
	ExcludeSystemDirs        []string
	ExcludeSystemExts        []string
	WorkFileExts             []string
	BackupKeywords           []string
	SizeFloatThreshold       float64
	FileNameSimilarityThresh float64
	FeatureSimilarityThresh  float64
	ModifyTimeIntervalDay    int
	SameContentThreshold     float64
	ProcessVersionThreshold  float64
	DerivedFileThreshold     float64
	ImageSimilarityThreshold float64
	OutputDir                string
	OutputFormat             string

	excludeSystemExtSet map[string]bool
	workFileExtSet      map[string]bool
}

func (c *Config) IsSystemExt(ext string) bool { return c.excludeSystemExtSet[strings.ToLower(ext)] }
func (c *Config) IsWorkExt(ext string) bool   { return c.workFileExtSet[strings.ToLower(ext)] }

func defaultConfig() *Config {
	cfg := &Config{
		HashAlgorithm: "sha256", // 文件哈希算法，可选 sha256 或 md5
		MaxFileSizeGB: 2.0,      // 跳过超过此大小（GB）的文件

		// 排除的系统目录，扫描时自动跳过
		ExcludeSystemDirs: []string{
			"/System", "/usr", "/bin", "/sbin", "/lib", "/proc", // macOS/Linux 系统目录
			"C:/Windows", "C:/Program Files", "C:/Program Files (x86)", // Windows 系统目录
			"$Recycle.Bin", // Windows 回收站
		},
		// 排除的系统文件扩展名，扫描时自动跳过
		ExcludeSystemExts: []string{
			"dll", "sys", "so", "dylib", // 动态链接库
			"exe", "bin", // 可执行文件
			"dat", "msi", "pkg", // 数据文件和安装包
		},
		// 需要分析的工作文件扩展名
		WorkFileExts: []string{
			"docx", "doc", "pdf", "xlsx", "xls", "pptx", "ppt", // 办公文档
			"txt", "md", // 纯文本
			"jpg", "jpeg", "png", "gif", "bmp", "tiff", // 图片
			"java", "go", "py", "js", "ts", "c", "cpp", "h", // 代码文件
		},
		// 备份文件关键词，文件名包含这些词会被标记为备份文件
		BackupKeywords: []string{
			"副本", "backup", "bak", "temp", "tmp", // 常见备份/临时标记
			"初稿", "终稿", "草稿", "修改版", // 中文版本标记
			"V1", "V2", "V3", // 版本号标记
			"copy", "old", "orig", "archive", // 英文备份标记
		},

		SizeFloatThreshold:       0.15, // 文件大小浮动阈值，差异超过 15% 则不作为候选对
		FileNameSimilarityThresh: 0.70, // 文件名相似度阈值，低于 0.70 则不作为候选对
		FeatureSimilarityThresh:  0.50, // 特征相似度阈值，低于 0.50 则不作为候选对
		ModifyTimeIntervalDay:    90,   // 修改时间间隔（天），超过此天数的文件不作为候选对

		SameContentThreshold:     0.95, // 相似度 ≥ 0.95 判定为"内容相同"
		ProcessVersionThreshold:  0.75, // 相似度 ≥ 0.75 判定为"过程版本"（不同版本/草稿）
		DerivedFileThreshold:     0.50, // 相似度 ≥ 0.50 判定为"衍生文件"（部分相似）
		ImageSimilarityThreshold: 0.84, // 图片感知哈希相似度阈值（汉明距离 ≤ 10/64）

		OutputDir:    "",     // 输出目录，默认按时间戳生成 output-YYYY-MM-DD-hh-mm-ss
		OutputFormat: "both", // 输出格式：json、xlsx 或 both（同时输出）
	}
	cfg.excludeSystemExtSet = toLowerSet(cfg.ExcludeSystemExts)
	cfg.workFileExtSet = toLowerSet(cfg.WorkFileExts)
	return cfg
}

func toLowerSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[strings.ToLower(s)] = true
	}
	return m
}

// ============================================================
// Scanner
// ============================================================

const chunkSize = 8 * 1024 * 1024 // 8 MB

type FileRecord struct {
	FileUniqueID    string
	FileFullPath    string
	FileName        string
	FileMainName    string
	FileExt         string
	FileSizeByte    int64
	FileCreateTime  time.Time
	FileModifyTime  time.Time
	FileDirPath     string
	FileHash        string
	FileHashGroupID string
	FileMIME        string
	FileCategory    string // system | work | backup | other
	FileLabel       string // 系统文件 | 疑似个人文件 | 个人文件
}

func (r *FileRecord) Recommendation(primaryIDs map[string]bool) string {
	if primaryIDs[r.FileUniqueID] {
		return "keep_primary"
	}
	return "review_for_deletion"
}

func scanFiles(ctx context.Context, root string, cfg *Config) ([]*FileRecord, error) {
	maxBytes := int64(cfg.MaxFileSizeGB * 1024 * 1024 * 1024)
	var records []*FileRecord
	hashToGroup := make(map[string]string)
	recordCount := 0
	stopped := false

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if stopped {
			return filepath.SkipDir
		}

		if err != nil {
			if verbose {
				log.Printf("warn: cannot access %s: %v", path, err)
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Size() > maxBytes {
			return nil
		}

		// 过滤Office临时文件 (~开头的)
		baseName := info.Name()
		if strings.HasPrefix(baseName, "~$") || strings.HasPrefix(baseName, ".~") {
			return nil
		}

		recordCount++
		if recordCount > maxFiles {
			log.Printf("warn: reached max file limit (%d), stopping scan", maxFiles)
			stopped = true
			return nil
		}

		h, err := hashFile(path, cfg.HashAlgorithm)
		if err != nil {
			if verbose {
				log.Printf("warn: cannot hash %s: %v", path, err)
			}
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		stem := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
		mtype, mErr := mimetype.DetectFile(path)
		mimeStr := "application/octet-stream"
		if mErr == nil {
			mimeStr = mtype.String()
			// 剥离 MIME 参数（如 "; charset=UTF-8"），只保留主类型
			if idx := strings.Index(mimeStr, ";"); idx >= 0 {
				mimeStr = strings.TrimSpace(mimeStr[:idx])
			}
		}
		rec := &FileRecord{
			FileUniqueID:   newUUID(),
			FileFullPath:   path,
			FileName:       info.Name(),
			FileMainName:   stem,
			FileExt:        ext,
			FileSizeByte:   info.Size(),
			FileCreateTime: getFileCreateTime(path, info),
			FileModifyTime: info.ModTime(),
			FileDirPath:    filepath.Dir(path),
			FileHash:       h,
			FileMIME:       mimeStr,
			FileCategory:   "other",
		}
		records = append(records, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, r := range records {
		if _, ok := hashToGroup[r.FileHash]; !ok {
			hashToGroup[r.FileHash] = newUUID()
		}
		r.FileHashGroupID = hashToGroup[r.FileHash]
	}
	return records, nil
}

func isExcludedDir(path string, excludes []string) bool {
	norm := filepath.ToSlash(path)
	for _, excl := range excludes {
		if strings.HasPrefix(norm, filepath.ToSlash(excl)) {
			return true
		}
	}
	return false
}

func hashFile(path, algorithm string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var h hash.Hash
	switch strings.ToLower(algorithm) {
	case "md5":
		h = md5.New()
	default:
		h = sha256.New()
	}
	buf := make([]byte, chunkSize)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x-0000-4000-8000-000000000000", len(b))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ============================================================
// Classifier
// ============================================================

const (
	CatSystem = "system"
	CatWork   = "work"
	CatBackup = "backup"
	CatOther  = "other"
)

// ---- 文件分类（五维评分） ----

type ScoreResult struct {
	Total         int
	PathScore     int
	OwnerScore    int
	MIMEScore     int
	FilenameScore int
	TracesScore   int
	Label         string // "系统文件" / "疑似个人文件" / "个人文件"
}

func scoreLabel(total int) string {
	switch {
	case total <= 3:
		return "系统文件"
	case total <= 7:
		return "疑似个人文件"
	default:
		return "个人文件"
	}
}

var (
	personalExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true, ".heic": true,
		".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
		".mp3": true, ".flac": true, ".aac": true, ".wav": true,
		".pdf": true,
		".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
		".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true,
	}
	classifierSystemExts = map[string]bool{
		".so": true, ".dll": true, ".dylib": true, ".sys": true, ".ko": true, ".o": true,
	}
	reDatePattern = regexp.MustCompile(`\d{4}[-_]\d{2}[-_]\d{2}|\d{8}`)
	reSystemName  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

func checkFilename(path string) int {
	base := filepath.Base(path)
	baseLower := strings.ToLower(base)
	ext := strings.ToLower(filepath.Ext(baseLower))
	score := 1
	if classifierSystemExts[ext] {
		score -= 2
	}
	if strings.HasPrefix(baseLower, "lib") {
		score--
	}
	if ext == "" && reSystemName.MatchString(baseLower) {
		score--
	}
	if personalExts[ext] {
		score++
	}
	for _, r := range base {
		if unicode.Is(unicode.Han, r) {
			score++
			break
		}
	}
	if reDatePattern.MatchString(base) {
		score++
	}
	if score < 0 {
		return 0
	}
	if score > 2 {
		return 2
	}
	return score
}

func classifierMIMEScore(mimeStr string) int {
	switch {
	case strings.HasPrefix(mimeStr, "application/x-executable"),
		strings.HasPrefix(mimeStr, "application/x-sharedlib"),
		strings.HasPrefix(mimeStr, "application/x-object"),
		strings.HasPrefix(mimeStr, "application/x-mach-binary"),
		strings.HasPrefix(mimeStr, "application/x-elf"):
		return 0
	case strings.HasPrefix(mimeStr, "image/"),
		strings.HasPrefix(mimeStr, "video/"),
		strings.HasPrefix(mimeStr, "audio/"),
		mimeStr == "application/pdf",
		strings.HasPrefix(mimeStr, "application/vnd.openxmlformats-"),
		mimeStr == "application/msword",
		mimeStr == "application/vnd.ms-excel",
		mimeStr == "application/vnd.ms-powerpoint":
		return 2
	default:
		return 1
	}
}

func checkOwner(_ string, info os.FileInfo) int {
	sys := info.Sys()
	if sys == nil {
		return 1
	}
	v := reflect.ValueOf(sys)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	uidField := v.FieldByName("Uid")
	if !uidField.IsValid() {
		return 1
	}
	fileUID := uint32(uidField.Uint())
	if fileUID == 0 {
		return 0
	}
	if int(fileUID) == os.Getuid() {
		return 2
	}
	return 1
}

type pathClassifier struct {
	systemPaths []string
	userPaths   []string
	systemDrive string
	goos        string
}

func newPathClassifier() (*pathClassifier, error) {
	pc := &pathClassifier{goos: runtime.GOOS}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("UserHomeDir: %w", err)
	}
	userSubdirs := func(names ...string) []string {
		dirs := []string{homeDir}
		for _, name := range names {
			dirs = append(dirs, filepath.Join(homeDir, name))
		}
		return dirs
	}
	switch pc.goos {
	case "windows":
		sd := strings.ToLower(os.Getenv("SystemDrive"))
		if sd == "" {
			sd = "c:"
		}
		pc.systemDrive = sd
		for _, p := range []string{os.Getenv("SystemRoot"), os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramData")} {
			if p != "" {
				pc.systemPaths = append(pc.systemPaths, strings.ToLower(p))
			}
		}
		pc.userPaths = userSubdirs("Documents", "Desktop", "Downloads", "Pictures", "Videos", "Music")
	case "darwin":
		pc.systemPaths = []string{"/System", "/Library", "/Applications", "/private",
			"/usr", "/bin", "/sbin", "/opt", "/.Spotlight-V100", "/.fseventsd"}
		pc.userPaths = userSubdirs("Documents", "Desktop", "Downloads", "Pictures", "Movies", "Music")
	case "linux":
		pc.systemPaths = []string{"/etc", "/bin", "/sbin", "/usr", "/lib", "/lib64",
			"/boot", "/proc", "/sys", "/dev", "/var"}
		if homeDir != "/root" {
			pc.systemPaths = append(pc.systemPaths, "/root")
		}
		pc.userPaths = userSubdirs("Documents", "Desktop", "Downloads", "Pictures", "Videos", "Music")
	default:
		return nil, fmt.Errorf("unsupported OS: %s", pc.goos)
	}
	return pc, nil
}

func (pc *pathClassifier) norm(path string) string {
	if pc.goos == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func (pc *pathClassifier) isNonSystemPartition(abs string) bool {
	switch pc.goos {
	case "darwin":
		return strings.HasPrefix(abs, "/Volumes/")
	case "linux":
		return strings.HasPrefix(abs, "/mnt/") || strings.HasPrefix(abs, "/media/")
	case "windows":
		drive := strings.ToLower(filepath.VolumeName(abs))
		return drive != "" && drive != pc.systemDrive
	}
	return false
}

func hasPathPrefix(path string, prefixes []string) bool {
	sep := string(filepath.Separator)
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+sep) {
			return true
		}
	}
	return false
}

func checkPathWhitelist(absPath string) int {
	pc, err := newPathClassifier()
	if err != nil {
		return 1
	}
	norm := pc.norm(absPath)
	if hasPathPrefix(norm, pc.systemPaths) {
		return 0
	}
	if hasPathPrefix(norm, pc.userPaths) {
		return 3
	}
	if pc.isNonSystemPartition(norm) {
		return 2
	}
	return 1
}

func checkUserTraces(absPath string, info os.FileInfo) int {
	if time.Since(info.ModTime()) <= 30*24*time.Hour {
		return 1
	}
	if inShellHistory(absPath) {
		return 1
	}
	if inRecentFiles(absPath) {
		return 1
	}
	return 0
}

func inShellHistory(absPath string) bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	var histFiles []string
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			histFiles = []string{filepath.Join(appData, "Microsoft", "Windows",
				"PowerShell", "PSReadLine", "ConsoleHost_history.txt")}
		}
	default:
		histFiles = []string{
			filepath.Join(homeDir, ".bash_history"),
			filepath.Join(homeDir, ".zsh_history"),
		}
	}
	for _, hf := range histFiles {
		if searchInFile(hf, absPath) {
			return true
		}
	}
	return false
}

func inRecentFiles(absPath string) bool {
	switch runtime.GOOS {
	case "linux":
		return inRecentFilesLinux(absPath)
	case "windows":
		return inRecentFilesWindows(absPath)
	}
	return false
}

func inRecentFilesLinux(absPath string) bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".local", "share", "recently-used.xbel"))
	if err != nil {
		return false
	}
	type bookmark struct {
		Href string `xml:"href,attr"`
	}
	type xbel struct {
		Bookmarks []bookmark `xml:"bookmark"`
	}
	var doc xbel
	if err := xml.Unmarshal(data, &doc); err != nil {
		return false
	}
	needle := "file://" + absPath
	for _, b := range doc.Bookmarks {
		if b.Href == needle || strings.EqualFold(b.Href, needle) {
			return true
		}
	}
	return false
}

func inRecentFilesWindows(absPath string) bool {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(appData, "Microsoft", "Windows", "Recent"))
	if err != nil {
		return false
	}
	targetBase := strings.ToLower(filepath.Base(absPath))
	for _, e := range entries {
		if strings.ToLower(filepath.Ext(e.Name())) != ".lnk" {
			continue
		}
		if strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))) == targetBase {
			return true
		}
	}
	return false
}

func searchInFile(filePath, needle string) bool {
	f, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), needle) {
			return true
		}
	}
	return false
}

func safeCheck(fn func() int, fallback int) (result int) {
	defer func() {
		if r := recover(); r != nil {
			result = fallback
		}
	}()
	return fn()
}

func Classify(path string) (ScoreResult, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	info, err := os.Lstat(real)
	if err != nil {
		return ScoreResult{}, fmt.Errorf("stat %s: %w", real, err)
	}
	if info.IsDir() {
		return ScoreResult{}, fmt.Errorf("%s is a directory", real)
	}
	abs, err := filepath.Abs(real)
	if err != nil {
		return ScoreResult{}, err
	}
	r := ScoreResult{}
	r.PathScore = safeCheck(func() int { return checkPathWhitelist(abs) }, 1)
	r.OwnerScore = safeCheck(func() int { return checkOwner(abs, info) }, 1)
	r.MIMEScore = safeCheck(func() int {
		mt, e := mimetype.DetectFile(abs)
		if e != nil {
			return 1
		}
		return classifierMIMEScore(mt.String())
	}, 1)
	r.FilenameScore = safeCheck(func() int { return checkFilename(abs) }, 1)
	r.TracesScore = safeCheck(func() int { return checkUserTraces(abs, info) }, 0)
	raw := r.PathScore + r.OwnerScore + r.MIMEScore + r.FilenameScore + r.TracesScore
	if raw < 1 {
		raw = 1
	}
	if raw > 10 {
		raw = 10
	}
	r.Total = raw
	r.Label = scoreLabel(r.Total)
	return r, nil
}

// markBackups 仅检测备份关键词，供 choosePrimary 在族群构建前使用
func markBackups(records []*FileRecord, cfg *Config) {
	for _, r := range records {
		nameLower := strings.ToLower(r.FileMainName)
		for _, kw := range cfg.BackupKeywords {
			if strings.Contains(nameLower, strings.ToLower(kw)) {
				r.FileCategory = CatBackup
				break
			}
		}
	}
}

// classifyAll 在族群构建完成后调用，对每个文件进行系统/个人判定并写入 FileLabel
func classifyAll(records []*FileRecord, cfg *Config) {
	for _, r := range records {
		result, err := Classify(r.FileFullPath)
		if err != nil {
			r.FileLabel = "疑似个人文件"
			// 保留已有的 CatBackup，否则标记为 other
			if r.FileCategory != CatBackup {
				r.FileCategory = CatOther
			}
			continue
		}
		r.FileLabel = result.Label
		switch result.Label {
		case "系统文件":
			r.FileCategory = CatSystem
		case "个人文件":
			if r.FileCategory != CatBackup {
				r.FileCategory = CatWork
			}
		default: // 疑似个人文件
			if r.FileCategory != CatBackup {
				r.FileCategory = CatOther
			}
		}
	}
}

// ---- MIME 桶分类 ----

func mimeBucket(mimeStr string) string {
	// 剥离 MIME 参数（如 "; charset=UTF-8"），只保留主类型用于匹配
	if idx := strings.Index(mimeStr, ";"); idx >= 0 {
		mimeStr = strings.TrimSpace(mimeStr[:idx])
	}
	switch {
	case strings.HasPrefix(mimeStr, "image/"):
		return "img"
	case strings.HasPrefix(mimeStr, "video/") || strings.HasPrefix(mimeStr, "audio/"):
		return "media"
	case mimeStr == "application/pdf",
		strings.HasPrefix(mimeStr, "application/vnd.openxmlformats-"),
		mimeStr == "application/msword",
		mimeStr == "application/vnd.ms-excel",
		mimeStr == "application/vnd.ms-powerpoint",
		mimeStr == "application/vnd.ms-works",  // WPS Office / MS Works (.wps)
		mimeStr == "application/x-ole-storage", // OLE Compound File（.doc/.dot/.xls/.ppt 等旧格式）
		mimeStr == "text/rtf",                  // RTF 文档
		mimeStr == "text/xml",                  // Word 另存为 XML
		mimeStr == "application/xml",           // Word 另存为 XML（部分系统）
		mimeStr == "message/rfc822",            // MHT/MHTML 网页存档（Word 另存为）
		mimeStr == "multipart/related",         // MHT/MHTML 的另一种 MIME 检测结果
		strings.HasPrefix(mimeStr, "text/plain"),
		strings.HasPrefix(mimeStr, "text/html"),
		mimeStr == "text/markdown":
		return "doc"
	case strings.HasPrefix(mimeStr, "text/x-"),
		strings.HasPrefix(mimeStr, "text/"),
		mimeStr == "application/javascript",
		mimeStr == "text/javascript":
		return "code"
	default:
		return "other"
	}
}

// ============================================================
// Selector
// ============================================================

var docExts = map[string]bool{
	"docx": true, "doc": true, "pdf": true, "xlsx": true, "xls": true,
	"pptx": true, "ppt": true, "txt": true, "md": true, "odt": true, "rtf": true,
	"xml": true, "mht": true, "mhtml": true,
}
var imgExts = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true,
	"bmp": true, "tiff": true, "webp": true,
}
var codeExts = map[string]bool{
	"py": true, "java": true, "go": true, "js": true, "ts": true,
	"c": true, "cpp": true, "h": true, "cs": true, "rb": true,
}

func typeBucket(ext string) string {
	e := strings.ToLower(ext)
	if docExts[e] {
		return "doc"
	}
	if imgExts[e] {
		return "img"
	}
	if codeExts[e] {
		return "code"
	}
	return "other"
}

type CandidatePair struct {
	GroupIDA string
	GroupIDB string
	Reason   string // same_dir | name_similar | recent_mtime | size_match
}

func selectCandidates(ctx context.Context, records []*FileRecord, cfg *Config) []CandidatePair {
	groups := make(map[string]*FileRecord)
	for _, r := range records {
		if _, ok := groups[r.FileHashGroupID]; !ok {
			groups[r.FileHashGroupID] = r
		}
	}

	// 先按 MIME 桶分组
	byBucket := make(map[string][]*FileRecord)
	for _, r := range groups {
		bucket := mimeBucket(r.FileMIME)
		byBucket[bucket] = append(byBucket[bucket], r)
	}

	// 排序保证不同运行产生相同的候选对顺序（map 迭代非确定性）
	for bucket := range byBucket {
		sort.Slice(byBucket[bucket], func(i, j int) bool {
			return byBucket[bucket][i].FileFullPath < byBucket[bucket][j].FileFullPath
		})
	}

	seen := make(map[[2]string]bool)
	var pairs []CandidatePair

	// 在每个 MIME 桶内进行配对
	for bucketName, list := range byBucket {
		for i := 0; i < len(list); i++ {
			select {
			case <-ctx.Done():
				return pairs
			default:
			}

			for j := i + 1; j < len(list); j++ {
				if len(pairs) >= maxCandidatePairs {
					log.Printf("warn: reached max candidate pairs limit (%d)", maxCandidatePairs)
					return pairs
				}

				a, b := list[i], list[j]
				if a.FileHashGroupID == b.FileHashGroupID {
					continue
				}
				crossFormat := a.FileExt != b.FileExt
				sizeMax := math.Max(float64(a.FileSizeByte), float64(b.FileSizeByte))
				if sizeMax == 0 {
					continue
				}
				// doc 桶的文件大小受嵌入资源（图片、OLE对象等）影响大，
				// 同一文档不同版本/来源体积差异可达 20-30%，
				// 因此对 doc 桶放宽大小过滤，依靠后续 simhash + TF-IDF 把关
				if !crossFormat {
					sizeThresh := cfg.SizeFloatThreshold
					if bucketName == "doc" {
						sizeThresh = 0.50
					}
					diff := math.Abs(float64(a.FileSizeByte-b.FileSizeByte)) / sizeMax
					if diff > sizeThresh {
						continue
					}
				} else {
					// 跨格式跳过比例过滤，但剔除大小相差 100 倍以上的极端情况
					sizeMin := math.Min(float64(a.FileSizeByte), float64(b.FileSizeByte))
					if sizeMin > 0 && sizeMax/sizeMin > 100 {
						continue
					}
				}
				key := sortedKey(a.FileHashGroupID, b.FileHashGroupID)
				if seen[key] {
					continue
				}
				seen[key] = true

				reason := "size_match"
				if a.FileDirPath == b.FileDirPath {
					reason = "same_dir"
				} else if nameSimilarity(a.FileMainName, b.FileMainName) >= cfg.FileNameSimilarityThresh {
					reason = "name_similar"
				} else {
					days := math.Abs(a.FileModifyTime.Sub(b.FileModifyTime).Hours() / 24)
					if days <= float64(cfg.ModifyTimeIntervalDay) {
						reason = "recent_mtime"
					}
				}
				// 跨格式配对必须有上下文依据（同目录/名称相似/时间接近），
				// 纯大小匹配不足以推断跨格式相同来源
				if crossFormat && reason == "size_match" {
					continue
				}
				pairs = append(pairs, CandidatePair{
					GroupIDA: a.FileHashGroupID,
					GroupIDB: b.FileHashGroupID,
					Reason:   reason,
				})
			}
		}
	}
	return pairs
}

func sortedKey(a, b string) [2]string {
	if a < b {
		return [2]string{a, b}
	}
	return [2]string{b, a}
}

func nameSimilarity(a, b string) float64 {
	ba, bb := strBigrams(strings.ToLower(a)), strBigrams(strings.ToLower(b))
	if len(ba) == 0 || len(bb) == 0 {
		return 0
	}
	intersect := 0
	for k := range ba {
		if bb[k] {
			intersect++
		}
	}
	return float64(intersect) / float64(len(ba)+len(bb)-intersect)
}

func strBigrams(s string) map[string]bool {
	runes := []rune(s)
	m := make(map[string]bool)
	for i := 0; i+1 < len(runes); i++ {
		m[string(runes[i:i+2])] = true
	}
	return m
}

// ============================================================
// Extractor
// ============================================================

const summaryCharLimit = 300  // 摘要展示用
const simhashCharLimit = 3000 // simhash 预过滤用（更大样本，减少误通过）

type FeatureResult struct {
	FilePath     string
	SummaryText  string
	SimhashValue uint64
}

func extractFeature(path string) FeatureResult {
	text := extractText(path)
	runes := []rune(text)
	if len(runes) > summaryCharLimit {
		text = string(runes[:summaryCharLimit])
	}
	res := FeatureResult{FilePath: path, SummaryText: text}
	if text != "" {
		res.SimhashValue = simhash(text)
	}
	return res
}

// computeContentHash extracts text from a document/code file and returns a
// SHA-256 hex of all letter/digit runes lowercased, with whitespace and
// punctuation stripped. This makes the hash format-agnostic: PDF libraries
// often inject spaces between Chinese characters while Word extractors do not,
// so normalising to bare characters eliminates that difference.
// Returns "" when no text can be extracted or fewer than 50 meaningful runes.
func computeContentHash(path string) string {
	text := extractText(path)
	return contentHashFromText(text)
}

// contentHashFromText is the pure normalise-and-hash step, shared between
// computeContentHash and the inline path in buildFamilies.
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

// extractText delegates to textextract.ExtractText (V5-P3 Task 1: 抽出独立包)
func extractText(path string) string {
	return textextract.ExtractText(path)
}

// extractTextWithTimeout delegates to textextract.ExtractTextWithTimeout
func extractTextWithTimeout(path string, timeout time.Duration) string {
	return textextract.ExtractTextWithTimeout(path, timeout)
}

func simhash(text string) uint64 {
	tokens := tokenizeText(text) // 与 charBigramTF 保持一致：中文字级、英文词级
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

// ============================================================
// Similarity
// ============================================================

type SimilarityResult struct {
	Score     float64
	Algorithm string
}

func computeSimilarity(pathA, pathB, bucket string) SimilarityResult {
	switch bucket {
	case "img":
		return imgSimilarity(pathA, pathB)
	case "doc", "code":
		return docSimilarity(pathA, pathB)
	default:
		return lshSimilarity(pathA, pathB)
	}
}

func docSimilarity(pathA, pathB string) SimilarityResult {
	type result struct {
		sr SimilarityResult
	}
	resChan := make(chan result, 1)

	go func() {
		// 防御性 recover：textextract.ExtractText 现已自带顶层 recover（textextract.go），
		// 此处再加一层防 future 路径上其它第三方依赖（goimagehash 等）panic 杀进程。
		// resChan 容量 1，panic 时尚未 send，这里 send 不会阻塞。
		defer func() {
			if r := recover(); r != nil {
				log.Printf("warn: docSimilarity panicked for %s & %s: %v", pathA, pathB, r)
				select {
				case resChan <- result{sr: SimilarityResult{Score: 0, Algorithm: "tfidf-cosine"}}:
				default:
				}
			}
		}()

		textA := readFileText(pathA)
		textB := readFileText(pathB)
		if textA == "" && textB == "" {
			// 无法提取文字（如扫描图片 PDF），回退到二进制 MinHash
			resChan <- result{sr: lshSimilarity(pathA, pathB)}
			return
		}
		if textA == "" || textB == "" {
			resChan <- result{sr: SimilarityResult{Score: 0.0, Algorithm: "tfidf-cosine"}}
			return
		}
		resChan <- result{sr: SimilarityResult{Score: tfidfCosine(textA, textB), Algorithm: "tfidf-cosine"}}
	}()

	select {
	case res := <-resChan:
		return res.sr
	case <-time.After(fileProcessTimeout):
		if verbose {
			log.Printf("warn: doc similarity timed out for %s & %s", pathA, pathB)
		}
		return SimilarityResult{Score: 0, Algorithm: "tfidf-cosine"}
	}
}

func readFileText(path string) string {
	return extractText(path)
}

// docSimilarityFromText computes TF-IDF cosine similarity from pre-extracted text,
// avoiding the need to re-read files from disk (and the associated timeout risk).
func docSimilarityFromText(textA, textB string) SimilarityResult {
	if textA == "" || textB == "" {
		return SimilarityResult{Score: 0.0, Algorithm: "tfidf-cosine"}
	}
	return SimilarityResult{Score: tfidfCosine(textA, textB), Algorithm: "tfidf-cosine"}
}

func tfidfCosine(a, b string) float64 {
	return tfidfCosineFromTF(charBigramTF(a), charBigramTF(b))
}

// tfidfCosineFromTF 给已经预算好 bigram TF 的调用方用的入口。
// 重构 #3：避免在每个候选对里都重算两侧的 charBigramTF。
// IDF 用本地 2-doc 语料计算（与原 tfidfCosine 等价），不能跨对共享。
func tfidfCosineFromTF(tfA, tfB map[string]float64) float64 {
	vocab := make(map[string]bool)
	for k := range tfA {
		vocab[k] = true
	}
	for k := range tfB {
		vocab[k] = true
	}

	idf := make(map[string]float64, len(vocab))
	for k := range vocab {
		df := 0
		if tfA[k] > 0 {
			df++
		}
		if tfB[k] > 0 {
			df++
		}
		idf[k] = math.Log(float64(2+1)/float64(df+1)) + 1.0
	}

	vecA := make(map[string]float64, len(tfA))
	vecB := make(map[string]float64, len(tfB))
	for k, tf := range tfA {
		vecA[k] = tf * idf[k]
	}
	for k, tf := range tfB {
		vecB[k] = tf * idf[k]
	}
	return cosine(vecA, vecB)
}

// isCJK 判断是否为 CJK 汉字（不含符号/标点，只含表意文字本身）。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK 统一表意文字
		(r >= 0x3400 && r <= 0x4DBF) || // CJK 扩展 A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK 扩展 B
		(r >= 0xF900 && r <= 0xFAFF) // CJK 兼容汉字
}

// tokenizeText 将文本分割为语义 token：
//   - 中文汉字：每个字符作为独立 token（字符级 bigram 即相邻两字，语义明确）
//   - 英文/数字：整个词（空格/标点分隔）作为 token（避免字母级 bigram 丢失语义）
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

func charBigramTF(text string) map[string]float64 {
	// 语言感知分词：中文按字符、英文按词，在 token 序列上同时计算 unigram + bigram。
	// - 词级 unigram：对 PDF 文本提取噪声（乱序、缺空格、页眉页脚）鲁棒，
	//   保证同内容跨格式文件（PDF vs DOCX）的词汇重叠率仍足够高。
	// - 词级 bigram：区分同领域不同内容文档（相邻词对具有内容判别力）。
	// - 中文字符 bigram 保持原有语义（相邻两字 ≈ 复合词），不受影响。
	// 使用亚线性 TF 缩放（1 + log(count)）抑制高频 token 对相似度的虚高影响。
	tokens := tokenizeText(text)
	counts := make(map[string]int)
	// unigram：单个 token，对文本提取顺序差异鲁棒
	for _, t := range tokens {
		counts[t]++
	}
	// bigram：相邻 token 对，提供内容判别力
	for i := 0; i+1 < len(tokens); i++ {
		counts[tokens[i]+"\x00"+tokens[i+1]]++
	}
	if len(counts) == 0 {
		return map[string]float64{}
	}
	tf := make(map[string]float64, len(counts))
	for k, c := range counts {
		tf[k] = 1.0 + math.Log(float64(c))
	}
	return tf
}

// cosine 计算两个稀疏向量的余弦相似度。
// 关键点：用 map 迭代做浮点累加在 Go 里是非确定性的（map order 随机），
// 所以这里先把 key 排序再累加，确保同一组输入永远得到 bit-identical 结果。
func cosine(a, b map[string]float64) float64 {
	keysA := make([]string, 0, len(a))
	for k := range a {
		keysA = append(keysA, k)
	}
	sort.Strings(keysA)

	dot := 0.0
	for _, k := range keysA {
		if vb, ok := b[k]; ok {
			dot += a[k] * vb
		}
	}
	na, nb := vecNorm(a), vecNorm(b)
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (na * nb)
}

// vecNorm 计算 L2 范数。同样用排序累加保证确定性。
func vecNorm(v map[string]float64) float64 {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sum := 0.0
	for _, k := range keys {
		x := v[k]
		sum += x * x
	}
	return math.Sqrt(sum)
}

func imgSimilarity(pathA, pathB string) SimilarityResult {
	// 优化1: 先检查文件大小，如果差异太大直接返回0，不用算pHash
	statA, errA := os.Stat(pathA)
	statB, errB := os.Stat(pathB)
	if errA == nil && errB == nil {
		sizeMax := math.Max(float64(statA.Size()), float64(statB.Size()))
		if sizeMax > 0 {
			sizeDiff := math.Abs(float64(statA.Size()-statB.Size())) / sizeMax
			if sizeDiff > 0.3 { // 大小差异>30%，图片肯定不相似
				return SimilarityResult{Score: 0, Algorithm: "phash-size-fastpath"}
			}
		}
	}

	type result struct {
		sr SimilarityResult
	}
	resChan := make(chan result, 1)

	// 图片单独设置合理的超时：8秒
	imgTimeout := 8 * time.Second

	go func() {
		hashA, err := pHashFile(pathA)
		if err != nil {
			if verbose {
				log.Printf("warn: cannot phash %s: %v", pathA, err)
			}
			resChan <- result{sr: SimilarityResult{Score: 0, Algorithm: "phash"}}
			return
		}
		hashB, err := pHashFile(pathB)
		if err != nil {
			if verbose {
				log.Printf("warn: cannot phash %s: %v", pathB, err)
			}
			resChan <- result{sr: SimilarityResult{Score: 0, Algorithm: "phash"}}
			return
		}
		dist, err := hashA.Distance(hashB)
		if err != nil {
			if verbose {
				log.Printf("warn: cannot compute phash distance: %v", err)
			}
			resChan <- result{sr: SimilarityResult{Score: 0, Algorithm: "phash"}}
			return
		}
		score := 1.0 - float64(dist)/64.0
		if score < 0 {
			score = 0
		}
		resChan <- result{sr: SimilarityResult{Score: score, Algorithm: "phash"}}
	}()

	select {
	case res := <-resChan:
		return res.sr
	case <-time.After(imgTimeout):
		if verbose {
			log.Printf("warn: img similarity timed out for %s & %s", pathA, pathB)
		}
		return SimilarityResult{Score: 0, Algorithm: "phash"}
	}
}

func pHashFile(path string) (*goimagehash.ImageHash, error) {
	// 先检查文件大小
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if stat.Size() > maxImageFileSize {
		return nil, fmt.Errorf("file too large")
	}

	// 用 imaging 库打开图片（自动处理EXIF旋转）
	// 注意：我们不需要完整解码原图，直接让 imaging 内部优化
	img, err := imaging.Open(path, imaging.AutoOrientation(true))
	if err != nil {
		return nil, err
	}

	// 检查尺寸（用解码后的配置）
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width > maxImageDimension || height > maxImageDimension {
		return nil, fmt.Errorf("image too large")
	}

	// 优化：直接缩放到 64x64（比32x32稍大，保留更多细节）
	// goimagehash 内部会再缩放到32x32，但提前缩放能大大减少内存和时间
	thumb := imaging.Resize(img, 64, 64, imaging.Lanczos)

	return goimagehash.PerceptionHash(thumb)
}

const numPerm = 128

func lshSimilarity(pathA, pathB string) SimilarityResult {
	type result struct {
		sr SimilarityResult
	}
	resChan := make(chan result, 1)

	go func() {
		sigA := minHashSig(pathA)
		sigB := minHashSig(pathB)
		if sigA == nil || sigB == nil {
			resChan <- result{sr: SimilarityResult{Score: 0, Algorithm: "minhash"}}
			return
		}
		matches := 0
		for i := 0; i < numPerm; i++ {
			if sigA[i] == sigB[i] {
				matches++
			}
		}
		resChan <- result{sr: SimilarityResult{Score: float64(matches) / float64(numPerm), Algorithm: "minhash"}}
	}()

	select {
	case res := <-resChan:
		return res.sr
	case <-time.After(fileProcessTimeout):
		if verbose {
			log.Printf("warn: lsh similarity timed out for %s & %s", pathA, pathB)
		}
		return SimilarityResult{Score: 0, Algorithm: "minhash"}
	}
}

func minHashSig(path string) []uint32 {
	f, err := os.Open(path)
	if err != nil {
		if verbose {
			log.Printf("warn: cannot open %s: %v", path, err)
		}
		return nil
	}
	defer f.Close()

	// 获取文件大小
	stat, err := f.Stat()
	if err != nil {
		if verbose {
			log.Printf("warn: cannot stat %s: %v", path, err)
		}
		return nil
	}
	fileSize := stat.Size()

	var data []byte
	if fileSize <= defaultMaxReadBytes {
		// 小文件：全读
		data, err = io.ReadAll(f)
	} else {
		// 大文件：采样策略 - 读取头部 + 尾部
		data = make([]byte, 0, defaultSampleSize*2)

		// 读取头部
		headBuf := make([]byte, defaultSampleSize)
		n, err := f.ReadAt(headBuf, 0) // 用 ReadAt 从偏移 0 读取，不改变文件指针
		if err != nil && err != io.EOF {
			return nil
		}
		if n > 0 {
			data = append(data, headBuf[:n]...)
		}

		// 读取尾部（如果文件足够大）
		if fileSize > defaultSampleSize {
			tailBuf := make([]byte, defaultSampleSize)
			tailOffset := fileSize - defaultSampleSize
			if tailOffset < 0 {
				tailOffset = 0
			}
			n, err := f.ReadAt(tailBuf, tailOffset) // 用 ReadAt 读取尾部
			if err == nil || err == io.EOF {
				if n > 0 {
					data = append(data, tailBuf[:n]...)
				}
			}
		}
	}

	if len(data) == 0 {
		return nil
	}

	// 生成 shingles
	shingles := make(map[uint32]bool)
	for i := 0; i+4 <= len(data); i++ {
		h := fnv.New32a()
		h.Write(data[i : i+4])
		shingles[h.Sum32()] = true
	}
	if len(shingles) == 0 {
		return nil
	}
	shinList := make([]uint32, 0, len(shingles))
	for s := range shingles {
		shinList = append(shinList, s)
	}
	sort.Slice(shinList, func(i, j int) bool { return shinList[i] < shinList[j] })

	sig := make([]uint32, numPerm)
	for p := 0; p < numPerm; p++ {
		seed := uint32(p)
		minVal := uint32(math.MaxUint32)
		for _, s := range shinList {
			v := hashWithSeed(s, seed)
			if v < minVal {
				minVal = v
			}
		}
		sig[p] = minVal
	}
	return sig
}

func hashWithSeed(val, seed uint32) uint32 {
	h := fnv.New32a()
	buf := [8]byte{
		byte(seed), byte(seed >> 8), byte(seed >> 16), byte(seed >> 24),
		byte(val), byte(val >> 8), byte(val >> 16), byte(val >> 24),
	}
	h.Write(buf[:])
	return h.Sum32()
}

// ============================================================
// Family
// ============================================================

type RelationType string

const (
	RelSameContent    RelationType = "same_content"
	RelProcessVersion RelationType = "process_version"
	RelDerived        RelationType = "derived"
)

// MemberMeta holds per-member similarity info relative to the family's primary file.
type MemberMeta struct {
	Score     float64
	Relation  RelationType
	Algorithm string
}

type SameSourceFamily struct {
	FamilyID      string
	PrimaryFileID string
	MemberIDs     []string
	Relation      RelationType          // family-level: max relation across all members
	Score         float64               // family-level: max score across all members
	Algorithm     string                // family-level: algorithm of the max score pair
	MemberScores  map[string]MemberMeta // per file_unique_id score relative to primary
}

// unionFind 并查集，用于将传递相似的文件组合并到同一 family
type unionFind struct {
	parent map[string]string
}

func newUnionFind() *unionFind {
	return &unionFind{parent: make(map[string]string)}
}

func (u *unionFind) find(x string) string {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
		return x
	}
	// 迭代路径压缩
	root := x
	for u.parent[root] != root {
		root = u.parent[root]
	}
	for u.parent[x] != root {
		next := u.parent[x]
		u.parent[x] = root
		x = next
	}
	return root
}

func (u *unionFind) union(x, y string) {
	rx, ry := u.find(x), u.find(y)
	if rx != ry {
		u.parent[ry] = rx
	}
}

func buildFamilies(ctx context.Context, records []*FileRecord, pairs []CandidatePair, cfg *Config) []SameSourceFamily {
	byGroup := make(map[string]*FileRecord)
	groupMembers := make(map[string][]*FileRecord)
	for _, r := range records {
		groupMembers[r.FileHashGroupID] = append(groupMembers[r.FileHashGroupID], r)
		if _, ok := byGroup[r.FileHashGroupID]; !ok {
			byGroup[r.FileHashGroupID] = r
		}
	}

	// 预提取所有特征（只提取一次，缓存起来）
	// 排序后迭代保证相同输入产生相同输出（map 迭代顺序非确定性）
	log.Printf("Extracting features for %d groups...", len(byGroup))
	type featureCache struct {
		simhash     uint64
		bucket      string
		rep         *FileRecord
		contentHash string             // normalised text hash for cross-format deduplication
		fullText    string             // cached extracted text (doc/code) — avoids re-reading during similarity
		tf          map[string]float64 // 重构 #3：预算 bigram TF，避免每对都重算
	}
	cache := make(map[string]featureCache)
	sortedGIDs := make([]string, 0, len(byGroup))
	for gid := range byGroup {
		sortedGIDs = append(sortedGIDs, gid)
	}
	sort.Strings(sortedGIDs)
	db := getSimilarityDB() // may be nil in old tests; cache path is skipped when nil
	// 重构 #4：feature flag 一次性读入。原实现每个 gid + 每次 cache writeback 都重读
	// system_config，单次 analyze 多达数百次 SQL 查询。flag 在一次分析过程中不会被
	// 用户改动，预读一次足够；如真发生了，行为也只是这次分析按开始时的 flag 跑完。
	precomputeOn := db != nil && featurePrecomputeEnabled()

	// 重构 #1+#2：批量预加载 features + extracted_text，替代主循环 N 次 ReadCachedFeatures
	// 和 worker pool 里 K 次 QueryExtractedText 的反复 SQL 往返。单连接 SQLite 下提速明显。
	var bulkCache map[string]*CachedFeaturesBulk
	if precomputeOn {
		signs := make([]string, 0, len(sortedGIDs))
		for _, gid := range sortedGIDs {
			signs = append(signs, byGroup[gid].FileHash)
		}
		if loaded, err := BatchReadCachedFeaturesWithText(db, signs); err == nil {
			bulkCache = loaded
		} else {
			log.Printf("warn: batch feature cache load failed, falling back to per-row: %v", err)
		}
	}

	for i, gid := range sortedGIDs {
		rep := byGroup[gid]
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if (i+1)%100 == 0 {
			log.Printf("Extracted features %d/%d", i+1, len(byGroup))
		}
		bucket := mimeBucket(rep.FileMIME)

		// 重构 #1：cache 命中走 in-memory bulk 查表（替代每 gid 一次 SQL）
		if precomputeOn {
			var cached *CachedFeaturesDB
			var cachedText string
			if b, ok := bulkCache[rep.FileHash]; ok {
				cached = &b.CachedFeaturesDB
				cachedText = b.ExtractedText
			}
			if cached != nil && IsCacheValid(rep.FileFullPath, cached) {
				// 重构 #2/#3：fullText 在 batch 阶段已经一起拿了，
				// 顺手算 TF 入 cache，worker pool 不再 lazy load
				var tfMap map[string]float64
				if cachedText != "" && (bucket == "doc" || bucket == "code") {
					tfMap = charBigramTF(cachedText)
				}
				cache[gid] = featureCache{
					simhash:     cached.Simhash,
					contentHash: cached.ContentHash,
					bucket:      bucket,
					rep:         rep,
					fullText:    cachedText,
					tf:          tfMap,
				}
				continue
			}
		}

		// Cache miss → live extraction (original logic preserved)
		var sh uint64
		var ch, fullText string
		if bucket == "doc" || bucket == "code" {
			// 带超时提取文本，防止损坏文件卡住整个预提取阶段
			fullText = extractTextHook(rep.FileFullPath, 10*time.Second)
			// simhash 使用更大的采样窗口（3000字符），比摘要展示（300字符）更可靠
			truncatedForSimhash := fullText
			if runes := []rune(fullText); len(runes) > simhashCharLimit {
				truncatedForSimhash = string(runes[:simhashCharLimit])
			}
			if truncatedForSimhash != "" {
				sh = simhash(truncatedForSimhash)
			}
			ch = contentHashFromText(fullText)

			// Write back cache (best effort; failure logged warn-level, not fatal)
			// Gated by same feature flag as reads — no point writing if reads are disabled.
			if precomputeOn {
				if info, statErr := os.Stat(rep.FileFullPath); statErr == nil {
					if err := WriteBackFeatures(db, rep.FileHash, CachedFeaturesDB{
						Simhash:      sh,
						ContentHash:  ch,
						FeatureMtime: info.ModTime(),
						FeatureSize:  info.Size(),
					}); err != nil {
						log.Printf("warn: cache writeback failed for %s: %v", rep.FileHash, err)
					}
					if fullText != "" {
						if err := WriteBackExtractedText(db, rep.FileHash, fullText); err != nil {
							log.Printf("warn: extracted_text writeback failed for %s: %v", rep.FileHash, err)
						}
					}
				}
			}
		} else {
			// Image/other bucket: writeback deferred until image phash extraction is wired up
			// (Plan B follow-up). Currently no useful features to persist for this bucket.
			feat := extractFeature(rep.FileFullPath)
			sh = feat.SimhashValue
		}
		// 重构 #3：fullText 非空时顺手算 bigram TF，避免每个候选对都重算
		var tfMap map[string]float64
		if fullText != "" && (bucket == "doc" || bucket == "code") {
			tfMap = charBigramTF(fullText)
		}
		cache[gid] = featureCache{
			simhash:     sh,
			bucket:      bucket,
			rep:         rep,
			contentHash: ch,
			fullText:    fullText,
			tf:          tfMap,
		}
	}

	type qualifiedPair struct {
		groupIDA  string
		groupIDB  string
		score     float64
		algorithm string
	}

	uf := newUnionFind()
	var qualified []qualifiedPair

	// 聚合每个连通分量的最高分和对应算法
	type familyMeta struct {
		score     float64
		algorithm string
	}
	rootMeta := make(map[string]familyMeta)

	// 步骤1: 先处理同一 HashGroup 内有多个成员的情况（exact match）
	for gid, members := range groupMembers {
		if len(members) >= 2 {
			root := uf.find(gid)
			if 1.0 > rootMeta[root].score {
				rootMeta[root] = familyMeta{1.0, "exact_hash"}
			}
		}
	}

	// 步骤1.5: 按文本内容哈希归组（跨格式同内容检测）
	// 相同文本内容、不同格式的文件（如 .docx 与 .pdf）直接合并为 same_content family
	contentHashToGroups := make(map[string][]string)
	for gid, fc := range cache {
		if fc.contentHash == "" {
			continue
		}
		contentHashToGroups[fc.contentHash] = append(contentHashToGroups[fc.contentHash], gid)
	}
	crossFormatCount := 0
	for _, gids := range contentHashToGroups {
		if len(gids) < 2 {
			continue
		}
		for i := 1; i < len(gids); i++ {
			uf.union(gids[0], gids[i])
		}
		root := uf.find(gids[0])
		if 1.0 > rootMeta[root].score {
			rootMeta[root] = familyMeta{1.0, "content_hash"}
		}
		crossFormatCount++
	}
	log.Printf("Content hash grouping: %d cross-format content groups found", crossFormatCount)

	// 步骤2: 并发处理 candidate pairs
	// len(pairs)==0 时跳过整个 worker pool，避免 channel 容量为 0 导致死锁
	if len(pairs) > 0 {
		log.Printf("Starting to process %d candidate pairs with %d workers...", len(pairs), workerCount)

		// 创建工作通道和结果通道
		pairChan := make(chan CandidatePair, len(pairs))
		resultChan := make(chan qualifiedPair, len(pairs))
		progressChan := make(chan int, 100)

		// 启动worker pool
		for w := 0; w < workerCount; w++ {
			go func(workerID int) {
				for pair := range pairChan {
					select {
					case <-ctx.Done():
						return
					default:
					}

					cacheA, okA := cache[pair.GroupIDA]
					cacheB, okB := cache[pair.GroupIDB]
					if !okA || !okB {
						progressChan <- 1
						continue
					}

					// Simhash快速过滤（跨格式配对跳过此检查，因为不同格式的文本提取
					// 起始位置可能不同，导致 simhash 差异偏大产生误过滤）
					bucket := cacheA.bucket
					crossFormat := cacheA.rep.FileExt != cacheB.rep.FileExt
					if !crossFormat && (bucket == "doc" || bucket == "code") {
						if cacheA.simhash != 0 && cacheB.simhash != 0 {
							hamming := bits.OnesCount64(cacheA.simhash ^ cacheB.simhash)
							if hamming > 20 {
								progressChan <- 1
								continue
							}
						}
					}

					// 计算相似度：doc/code 优先使用预算的 TF（重构 #3），
					// 否则用主循环已经预加载的 fullText（重构 #1+#2，worker 不再 lazy load DB）。
					// 两者都不可得 → fallback 重读文件，与原行为等价。
					var result SimilarityResult
					if bucket == "doc" || bucket == "code" {
						tfA := cacheA.tf
						tfB := cacheB.tf
						switch {
						case tfA != nil && tfB != nil:
							result = SimilarityResult{
								Score:     tfidfCosineFromTF(tfA, tfB),
								Algorithm: "tfidf-cosine",
							}
						case cacheA.fullText != "" && cacheB.fullText != "":
							result = docSimilarityFromText(cacheA.fullText, cacheB.fullText)
						default:
							result = computeSimilarity(cacheA.rep.FileFullPath, cacheB.rep.FileFullPath, bucket)
						}
					} else {
						result = computeSimilarity(cacheA.rep.FileFullPath, cacheB.rep.FileFullPath, bucket)
					}
					threshold := cfg.DerivedFileThreshold
					if bucket == "img" {
						threshold = cfg.ImageSimilarityThreshold
					}
					if result.Score >= threshold {
						resultChan <- qualifiedPair{pair.GroupIDA, pair.GroupIDB, result.Score, result.Algorithm}
					}
					progressChan <- 1
				}
			}(w)
		}

		// 发送所有pair到工作通道
		go func() {
			for _, pair := range pairs {
				pairChan <- pair
			}
			close(pairChan)
		}()

		// 收集结果和进度
		processed := 0
		lastReport := 0
		doneWorkers := 0

		for doneWorkers < workerCount {
			select {
			case <-ctx.Done():
				return nil
			case qp, ok := <-resultChan:
				if ok {
					uf.union(qp.groupIDA, qp.groupIDB)
					qualified = append(qualified, qp)
				}
			case <-progressChan:
				processed++
				if processed-lastReport >= 500 || processed == len(pairs) {
					log.Printf("Processed %d/%d pairs (%.1f%%), found %d qualified",
						processed, len(pairs), float64(processed)*100/float64(len(pairs)), len(qualified))
					lastReport = processed
				}
				if processed == len(pairs) {
					doneWorkers = workerCount // 全部处理完
				}
			}
		}
		// Drain resultChan: the select loop may exit (via progressChan) while qualified
		// pairs are still buffered, causing them to be silently dropped.
		for draining := true; draining; {
			select {
			case qp := <-resultChan:
				uf.union(qp.groupIDA, qp.groupIDB)
				qualified = append(qualified, qp)
			default:
				draining = false
			}
		}
		close(resultChan)
		close(progressChan)
	} else {
		log.Printf("No candidate pairs, skipping similarity computation")
	}

	// 步骤3: 更新 rootMeta，合并后的 group 取最高分
	for _, qp := range qualified {
		root := uf.find(qp.groupIDA)
		if qp.score > rootMeta[root].score {
			rootMeta[root] = familyMeta{qp.score, qp.algorithm}
		}
	}

	// 步骤3.5: 不变量修复 — step 1/1.5 在 union 发生前以"自己是根"的假设写入 rootMeta，
	// 后续 union 把树根迁走后，那些写入会停留在非根 gid 上，最终在 step 4 按当前 root 取
	// 时被丢弃，导致 highest_score 凭运气波动（最严重的表现是 exact_hash=1.0 整族消失，
	// 退化为 worker pool 算出的非 1.0 doc 相似度）。
	//
	// 做法：遍历当前 rootMeta，把每个 entry 重新映射到当前 root，max 聚合。
	// 浮点 score 相等时取 algorithm 字典序较小的，保证 tie-break 也是确定性的。
	{
		// 先按旧 key 排序，确保非确定性 map 迭代不会影响并列时的算法选择
		oldKeys := make([]string, 0, len(rootMeta))
		for k := range rootMeta {
			oldKeys = append(oldKeys, k)
		}
		sort.Strings(oldKeys)

		remapped := make(map[string]familyMeta, len(rootMeta))
		for _, oldKey := range oldKeys {
			meta := rootMeta[oldKey]
			root := uf.find(oldKey)
			existing, ok := remapped[root]
			switch {
			case !ok:
				remapped[root] = meta
			case meta.score > existing.score:
				remapped[root] = meta
			case meta.score == existing.score && meta.algorithm < existing.algorithm:
				remapped[root] = meta
			}
		}
		rootMeta = remapped
	}

	// 步骤4: 收集所有在 rootMeta 中的 group
	rootGroups := make(map[string][]string)
	for gid := range uf.parent {
		root := uf.find(gid)
		rootGroups[root] = append(rootGroups[root], gid)
	}
	for gid := range rootMeta {
		if _, ok := rootGroups[gid]; !ok && uf.find(gid) == gid {
			// Only create a standalone entry for gids that are still their own
			// root; merged roots would otherwise produce spurious singleton families.
			rootGroups[gid] = []string{gid}
		}
	}

	// Sort each rootGroups slice for deterministic downstream processing
	for root := range rootGroups {
		sort.Strings(rootGroups[root])
	}

	// 排序保证输出确定性
	roots := make([]string, 0, len(rootGroups))
	for root := range rootGroups {
		roots = append(roots, root)
	}
	sort.Strings(roots)

	// 构建 fileID → groupID 的映射，用于查找每个成员所属的 group
	fileToGroup := make(map[string]string)
	for _, r := range records {
		fileToGroup[r.FileUniqueID] = r.FileHashGroupID
	}

	log.Printf("Building families: %d candidate root groups to evaluate", len(roots))
	familyBuildStart := time.Now()
	fallbackLiveExtractCount := 0

	var families []SameSourceFamily
	familySeq := 0
	for ri, root := range roots {
		if (ri+1)%50 == 0 || ri+1 == len(roots) {
			log.Printf("Building families: %d/%d processed", ri+1, len(roots))
		}
		meta := rootMeta[root]
		var allMembers []*FileRecord
		for _, gid := range rootGroups[root] {
			allMembers = append(allMembers, groupMembers[gid]...)
		}
		primary := choosePrimary(allMembers)
		primaryGID := fileToGroup[primary.FileUniqueID]
		primaryCache := cache[primaryGID]

		memberIDs := make([]string, 0, len(allMembers))
		memberScores := make(map[string]MemberMeta, len(allMembers))
		for _, m := range allMembers {
			memberIDs = append(memberIDs, m.FileUniqueID)

			mGID := fileToGroup[m.FileUniqueID]
			var ms MemberMeta

			if m.FileUniqueID == primary.FileUniqueID {
				// primary 自身：score=1.0
				ms = MemberMeta{Score: 1.0, Relation: RelSameContent, Algorithm: "primary"}
			} else if mGID == primaryGID {
				// 同一 hash group（二进制完全一致）
				ms = MemberMeta{Score: 1.0, Relation: RelSameContent, Algorithm: "exact_hash"}
			} else {
				mCache := cache[mGID]
				// 优先检查 content hash
				if primaryCache.contentHash != "" && mCache.contentHash != "" && primaryCache.contentHash == mCache.contentHash {
					ms = MemberMeta{Score: 1.0, Relation: RelSameContent, Algorithm: "content_hash"}
				} else {
					// 计算与 primary 的实际相似度
					// 重构 #3：优先用预算的 TF，等价于 docSimilarityFromText 但省 charBigramTF
					var sr SimilarityResult
					bucket := primaryCache.bucket
					if (bucket == "doc" || bucket == "code") && primaryCache.tf != nil && mCache.tf != nil {
						sr = SimilarityResult{
							Score:     tfidfCosineFromTF(primaryCache.tf, mCache.tf),
							Algorithm: "tfidf-cosine",
						}
					} else if (bucket == "doc" || bucket == "code") && primaryCache.fullText != "" && mCache.fullText != "" {
						sr = docSimilarityFromText(primaryCache.fullText, mCache.fullText)
					} else {
						// fallback 现场抽文本/算 phash，每次最多 fileProcessTimeout(3s)
						// 缓存命中失败 + 文本不可得才会走到这里，记一笔便于定位拖慢源
						fallbackLiveExtractCount++
						log.Printf("warn: family scoring fallback to live extract: %s vs %s (bucket=%s)",
							primary.FileFullPath, m.FileFullPath, bucket)
						sr = computeSimilarity(primary.FileFullPath, m.FileFullPath, bucket)
					}
					ms = MemberMeta{Score: sr.Score, Relation: scoreToRelation(sr.Score, cfg), Algorithm: sr.Algorithm}
				}
			}
			memberScores[m.FileUniqueID] = ms
		}

		familySeq++
		families = append(families, SameSourceFamily{
			FamilyID:      fmt.Sprintf("F%04d", familySeq),
			PrimaryFileID: primary.FileUniqueID,
			MemberIDs:     memberIDs,
			Relation:      scoreToRelation(meta.score, cfg),
			Score:         meta.score,
			Algorithm:     meta.algorithm,
			MemberScores:  memberScores,
		})
	}
	log.Printf("Built %d families in %v (live-extract fallbacks: %d)",
		len(families), time.Since(familyBuildStart), fallbackLiveExtractCount)
	return families
}

func scoreToRelation(score float64, cfg *Config) RelationType {
	if score >= cfg.SameContentThreshold {
		return RelSameContent
	}
	if score >= cfg.ProcessVersionThreshold {
		return RelProcessVersion
	}
	return RelDerived
}

// formatQuality 定义文件格式的保留优先级：数值越高越优先保留。
// 可编辑的主流格式 > 归档格式 > 衍生格式 > 旧格式
var formatQuality = map[string]int{
	"docx": 100, "xlsx": 100, "pptx": 100,
	"doc": 80, "xls": 80, "ppt": 80,
	"pdf": 70,
	"md":  60, "txt": 50,
	"odt":  45,
	"rtf":  40,
	"html": 30, "htm": 30,
	"xml": 20, "mht": 10, "mhtml": 10,
}

func choosePrimary(all []*FileRecord) *FileRecord {
	// 过滤掉备份文件；若全部是备份则降级使用全集
	candidates := make([]*FileRecord, 0, len(all))
	for _, r := range all {
		if r.FileCategory != CatBackup {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		candidates = all
	}
	// 排序：格式质量高的优先，同格式则取修改时间最新的，最后用路径作稳定 tiebreaker
	sort.SliceStable(candidates, func(i, j int) bool {
		qi, qj := formatQuality[candidates[i].FileExt], formatQuality[candidates[j].FileExt]
		if qi != qj {
			return qi > qj
		}
		if !candidates[i].FileModifyTime.Equal(candidates[j].FileModifyTime) {
			return candidates[i].FileModifyTime.After(candidates[j].FileModifyTime)
		}
		// Stable tiebreaker: prefer lexicographically smaller path so that
		// selection is deterministic across runs given the same input set.
		return candidates[i].FileFullPath < candidates[j].FileFullPath
	})
	return candidates[0]
}
