# Spec B：扫描期预计算文件特征 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 BuildFamilies 中耗时大头的 `extractFeatures` 阶段挪到扫描期一次性算好并持久化，让重建相似度从 ~35 分钟降到 ~5 分钟（cache 命中常态）。

**Architecture:** `data_distributing` 表新增 6 列存特征（simhash/content_hash/phash/extracted_text/feature_mtime/feature_size）。Scanner 在算完 SHA-256 后串联特征计算，失败不阻塞 hash。BuildFamilies 改读 DB cache + stat 失效检测，cache miss 时 fallback 现场重算并回写。算法本身完全不动。

**Tech Stack:** Go + sqlx (SQLite) / textextract 包 / goimagehash phash

**Spec reference:** `docs/superpowers/specs/2026-05-26-spec-b-feature-precompute-on-scan.md`

**关键铁律（来自 CLAUDE.md）：**
- 每个任务都要有测试用例验证才能进入下一阶段
- 项目中采用 yarn 代替 npm
- 测试前需运行 `npm rebuild better-sqlite3`（go test 不需要，仅 frontend vitest 需要；本 plan 主要是 go 端）
- 严禁修改扫描发现的物理文件（仅读、计算特征、写 DB，天然合规）
- 不删现场重算路径（永久作为 fallback 保留）

---

## File Structure

### 新增/修改

| 文件 | 责任 |
|---|---|
| `internal/repository/db.go` | runMigrations 中追加 6 列 ADD COLUMN + 部分索引（idempotent） |
| `internal/models/data_distribution.go` | DataDistribution struct 加 6 个新字段（指针/可选） |
| `internal/scanner/feature_cache.go`（新建） | `ExtractFeaturesForCache(path, mime)` 独立函数 |
| `internal/scanner/feature_cache_test.go`（新建） | 单元测试 |
| `internal/scanner/atomic.go` | hash worker 写入流程串联 feature 计算 |
| `internal/similarity/feature_cache.go`（新建） | `ReadCachedFeatures` + `IsCacheValid` + `WriteBackFeatures` 工具函数 |
| `internal/similarity/feature_cache_test.go`（新建） | 单元测试 |
| `internal/similarity/analyzer.go` | LoadInputs 不读 extracted_text |
| `internal/similarity/similarity.go` | extractFeatures 阶段改读 cache；worker pool lazy load extracted_text |
| `internal/repository/system_config.go` | 新增 feature flag key `feature_precompute_enabled` |

把 cache 工具抽出到独立文件，避免 similarity.go / atomic.go 进一步膨胀（前者已超 1800 行）。

---

## Task 1：DB schema 增 6 列 + content_hash 索引

**Files:**
- Modify: `internal/repository/db.go`
- Test: `internal/repository/feature_cache_migration_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
// internal/repository/feature_cache_migration_test.go
package repository

import (
    "testing"
)

func TestMigration_DataDistributingFeatureCacheColumns(t *testing.T) {
    db := setupTestDB(t)

    // 验证 6 个新列存在
    cols := []string{"simhash", "content_hash", "phash", "extracted_text", "feature_mtime", "feature_size"}
    for _, col := range cols {
        var present int
        err := db.Get(&present, `SELECT COUNT(*) FROM pragma_table_info('data_distributing') WHERE name = ?`, col)
        if err != nil {
            t.Fatalf("query column %s: %v", col, err)
        }
        if present != 1 {
            t.Errorf("column %s missing in data_distributing", col)
        }
    }

    // 验证索引
    var idxCount int
    db.Get(&idxCount, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_data_distributing_content_hash'`)
    if idxCount != 1 {
        t.Errorf("index idx_data_distributing_content_hash missing")
    }
}

func TestMigration_OldDataRowsAreNullSafe(t *testing.T) {
    db := setupTestDB(t)

    // 写一行不含新字段的数据（模拟升级前的老数据）
    _, err := db.Exec(`INSERT INTO data_distributing
        (path, content_sign, file_create_time, file_size, create_time, update_time, disable, data_origin)
        VALUES ('/old/file.pdf', 'OLD_HASH', '2026-01-01', 100, '2026-01-01', '2026-01-01', 0, 'historical')`)
    if err != nil {
        t.Fatalf("insert old row: %v", err)
    }

    // 查 features 应该全 NULL
    var simhash, contentHash, extractedText *string
    err = db.QueryRow(`SELECT simhash, content_hash, extracted_text FROM data_distributing WHERE content_sign='OLD_HASH'`).
        Scan(&simhash, &contentHash, &extractedText)
    if err != nil {
        t.Fatalf("query old row: %v", err)
    }
    if simhash != nil || contentHash != nil || extractedText != nil {
        t.Errorf("old row features should be NULL, got %v %v %v", simhash, contentHash, extractedText)
    }
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
cd /root/data/projects/data-asset-scan
go test ./internal/repository/ -run TestMigration_DataDistributingFeatureCacheColumns -v
# 期望：FAIL，列不存在
```

- [ ] **Step 3: 在 runMigrations() 添加幂等 ALTER**

`internal/repository/db.go` 的 `runMigrations` 函数末尾追加：

```go
// 引用 Spec B：扫描期预计算文件特征
if err := migrateDataDistributingFeatureCache(db); err != nil {
    return fmt.Errorf("migrate data_distributing feature cache: %w", err)
}
```

新增函数（同文件末尾或新建 `internal/repository/feature_cache_migration.go`）：

```go
func migrateDataDistributingFeatureCache(db *sqlx.DB) error {
    columns := []struct {
        name string
        ddl  string
    }{
        {"simhash",        "INTEGER"},
        {"content_hash",   "TEXT"},
        {"phash",          "TEXT"},
        {"extracted_text", "TEXT"},
        {"feature_mtime",  "DATETIME"},
        {"feature_size",   "INTEGER"},
    }
    for _, c := range columns {
        // 检查列是否已存在，避免重复 ALTER 报错
        var exists int
        if err := db.Get(&exists,
            `SELECT COUNT(*) FROM pragma_table_info('data_distributing') WHERE name = ?`,
            c.name); err != nil {
            return fmt.Errorf("check column %s: %w", c.name, err)
        }
        if exists > 0 {
            continue
        }
        sql := fmt.Sprintf(`ALTER TABLE data_distributing ADD COLUMN %s %s`, c.name, c.ddl)
        if _, err := db.Exec(sql); err != nil {
            return fmt.Errorf("add column %s: %w", c.name, err)
        }
    }

    // content_hash 索引（partial：仅对非 NULL 行）
    if _, err := db.Exec(
        `CREATE INDEX IF NOT EXISTS idx_data_distributing_content_hash
         ON data_distributing(content_hash) WHERE content_hash IS NOT NULL`); err != nil {
        return fmt.Errorf("create content_hash index: %w", err)
    }
    return nil
}
```

- [ ] **Step 4: 跑测试验证通过**

```bash
go test ./internal/repository/ -run TestMigration_DataDistributingFeatureCacheColumns -v
go test ./internal/repository/ -run TestMigration_OldDataRowsAreNullSafe -v
# 期望：两个 PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/repository/db.go internal/repository/feature_cache_migration.go internal/repository/feature_cache_migration_test.go
git commit -m "feat(db): add feature cache columns + content_hash index to data_distributing"
```

---

## Task 2：DataDistribution model 加 6 个字段

**Files:**
- Modify: `internal/models/data_distribution.go`
- Test: `internal/repository/data_distribution_feature_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
// internal/repository/data_distribution_feature_test.go
package repository

import (
    "testing"
    "time"
)

func TestDataDistribution_WriteAndReadFeatures(t *testing.T) {
    db := setupTestDB(t)
    now := time.Now()

    // 写入带特征的行
    simhash := int64(0xDEADBEEF)
    contentHash := "abc123"
    extractedText := "hello world"

    _, err := db.Exec(`INSERT INTO data_distributing
        (path, content_sign, file_create_time, file_size,
         simhash, content_hash, extracted_text, feature_mtime, feature_size,
         create_time, update_time, disable, data_origin)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 'historical')`,
        "/test/feat.pdf", "FEAT1", now, int64(100),
        simhash, contentHash, extractedText, now, int64(100),
        now, now)
    if err != nil {
        t.Fatalf("insert with features: %v", err)
    }

    // 读回来
    var got struct {
        Simhash       *int64     `db:"simhash"`
        ContentHash   *string    `db:"content_hash"`
        ExtractedText *string    `db:"extracted_text"`
        FeatureMtime  *time.Time `db:"feature_mtime"`
        FeatureSize   *int64     `db:"feature_size"`
    }
    err = db.Get(&got,
        `SELECT simhash, content_hash, extracted_text, feature_mtime, feature_size
         FROM data_distributing WHERE content_sign = ?`, "FEAT1")
    if err != nil {
        t.Fatalf("select features: %v", err)
    }
    if got.Simhash == nil || *got.Simhash != simhash {
        t.Errorf("simhash = %v, want %v", got.Simhash, simhash)
    }
    if got.ContentHash == nil || *got.ContentHash != contentHash {
        t.Errorf("contentHash mismatch")
    }
    if got.ExtractedText == nil || *got.ExtractedText != extractedText {
        t.Errorf("extractedText mismatch")
    }
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
go test ./internal/repository/ -run TestDataDistribution_WriteAndReadFeatures -v
# 期望：PASS（schema 已加，但 model struct 还没字段；纯 SQL 操作可能跑过；如果用 sqlx.Select 直接到 struct 才会失败）
```

> 注：本 step 测试只验证 schema 可写可读。Task 3+ 会用 sqlx.Select 到完整 DataDistribution struct，那时 struct 必须有字段才能 select 成功。

- [ ] **Step 3: 在 model 加字段**

`internal/models/data_distribution.go` 的 `DataDistribution` struct 追加：

```go
type DataDistribution struct {
    // ...existing fields...

    // Spec B：扫描期预计算特征缓存（可空）
    Simhash       *int64     `db:"simhash"        json:"-"`
    ContentHash   *string    `db:"content_hash"   json:"-"`
    Phash         *string    `db:"phash"          json:"-"`
    ExtractedText *string    `db:"extracted_text" json:"-"`  // 体积大，不暴露给 API
    FeatureMtime  *time.Time `db:"feature_mtime"  json:"-"`
    FeatureSize   *int64     `db:"feature_size"   json:"-"`
}
```

`json:"-"` 保证这些字段不出现在任何 API 响应里（extracted_text 尤其不能暴露，体积过大）。

- [ ] **Step 4: 跑现有 data_distribution 相关测试，验证不破坏**

```bash
go test ./internal/repository/ -run TestDataDistribution -v
go test ./internal/repository/ -run TestListFilesWithFilters -v  # ListFilesWithFilters 用 DataDistribution embedding
# 期望：全部 PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/models/data_distribution.go internal/repository/data_distribution_feature_test.go
git commit -m "feat(models): DataDistribution add 6 nullable feature cache fields"
```

---

## Task 3：scanner.ExtractFeaturesForCache 独立函数

**Files:**
- Create: `internal/scanner/feature_cache.go`
- Create: `internal/scanner/feature_cache_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/scanner/feature_cache_test.go
package scanner

import (
    "os"
    "path/filepath"
    "testing"
)

func TestExtractFeaturesForCache_DocFile(t *testing.T) {
    tmp := t.TempDir()
    txtPath := filepath.Join(tmp, "test.txt")
    content := "This is a test document for feature extraction. It contains enough text to produce a meaningful simhash."
    if err := os.WriteFile(txtPath, []byte(content), 0644); err != nil {
        t.Fatal(err)
    }
    info, _ := os.Stat(txtPath)

    feat, err := ExtractFeaturesForCache(txtPath, "text/plain", info)
    if err != nil {
        t.Fatalf("extract: %v", err)
    }
    if feat.Simhash == 0 {
        t.Errorf("simhash should be non-zero for non-empty text")
    }
    if feat.ContentHash == "" {
        t.Errorf("content_hash should be non-empty for sufficient text")
    }
    if feat.ExtractedText == "" {
        t.Errorf("extracted_text should be non-empty")
    }
    if feat.Mtime.IsZero() {
        t.Errorf("mtime should be set")
    }
    if feat.Size != info.Size() {
        t.Errorf("size = %d, want %d", feat.Size, info.Size())
    }
}

func TestExtractFeaturesForCache_CorruptedFile_DoesNotPanic(t *testing.T) {
    tmp := t.TempDir()
    bad := filepath.Join(tmp, "corrupted.pdf")
    // 写一些假装是 PDF 但实际损坏的字节
    os.WriteFile(bad, []byte{0x25, 0x50, 0x44, 0x46, 0xff, 0xff, 0xff}, 0644)
    info, _ := os.Stat(bad)

    feat, err := ExtractFeaturesForCache(bad, "application/pdf", info)
    // 应该返回 err 或空 features，但不能 panic
    if err == nil && feat.ExtractedText != "" {
        t.Errorf("corrupted PDF should fail or return empty text, got %d chars", len(feat.ExtractedText))
    }
}

func TestExtractFeaturesForCache_UnsupportedMIME_ReturnsEmpty(t *testing.T) {
    tmp := t.TempDir()
    bin := filepath.Join(tmp, "data.bin")
    os.WriteFile(bin, []byte{1, 2, 3, 4, 5}, 0644)
    info, _ := os.Stat(bin)

    feat, err := ExtractFeaturesForCache(bin, "application/octet-stream", info)
    if err != nil {
        t.Errorf("unsupported MIME should not error: %v", err)
    }
    if feat.ExtractedText != "" {
        t.Errorf("unsupported MIME should produce no text")
    }
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
go test ./internal/scanner/ -run TestExtractFeaturesForCache -v
# 期望：FAIL，函数未定义
```

- [ ] **Step 3: 实现函数**

```go
// internal/scanner/feature_cache.go
package scanner

import (
    "crypto/sha256"
    "fmt"
    "hash/fnv"
    "math"
    "os"
    "strings"
    "time"
    "unicode"

    "data-asset-scan-go/internal/textextract"
)

// CachedFeatures 是扫描期算好准备写入 data_distributing 的特征集合。
type CachedFeatures struct {
    Simhash       int64
    ContentHash   string // 大小写不敏感，仅含字母数字的全文 SHA-256 hex；空字符串表示无内容
    Phash         string // 图像感知哈希；当前实现暂留空（接入 pHashFile 待 phash 子任务）
    ExtractedText string // 完整抽出的纯文本（lazy load 用）；空表示无文本
    Mtime         time.Time
    Size          int64
}

// ExtractFeaturesForCache 单文件特征计算入口。
// 与 internal/similarity/similarity.go:extractFeature 等价的特征集合，
// 但去掉了图片 phash（pHashFile 在 similarity 包内，本函数只做 doc/code 文本类）。
// img 桶的 phash 在 Task 4 单独追加（保持本任务最小变更）。
//
// 失败语义：返回 err 时调用方应记 warn 但不阻塞 hash 写入；features 字段会是零值。
func ExtractFeaturesForCache(path, mime string, info os.FileInfo) (CachedFeatures, error) {
    bucket := classifyBucket(mime)

    feat := CachedFeatures{
        Mtime: info.ModTime(),
        Size:  info.Size(),
    }

    if bucket == "doc" || bucket == "code" {
        text := textextract.ExtractTextWithTimeout(path, 10*time.Second)
        if text == "" {
            // 抽不出文本，但不算错（PDF 扫描图片版很常见）
            return feat, nil
        }
        feat.ExtractedText = text
        feat.Simhash = int64(simhashFromText(text))
        feat.ContentHash = contentHashFromText(text)
    }
    // 其他桶（img / other）本任务跳过，feat 仅含 mtime+size

    return feat, nil
}

// classifyBucket 与 similarity.mimeBucket 同源；
// 此处独立实现避免 scanner → similarity 包循环。
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

// simhashFromText 与 similarity.simhash 同算法；
// 截断到 3000 字符（与 simhashCharLimit 一致）。
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
    _ = math.MaxInt // 避免 math import 未使用警告（如有）
    return tokens
}
```

> 注意：本 task 不实现 img phash，避免 scanner → goimagehash 依赖在本步引入。img phash 在 Task 4 单独追加（如果后续 plan 决定支持），可以延后或不做（与 BuildFamilies 现有 fallback 兼容）。

- [ ] **Step 4: 跑测试验证通过**

```bash
go test ./internal/scanner/ -run TestExtractFeaturesForCache -v
# 期望：3 个 PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/scanner/feature_cache.go internal/scanner/feature_cache_test.go
git commit -m "feat(scan): ExtractFeaturesForCache for doc/code (simhash + contentHash + text)"
```

---

## Task 4：scanner atomic.go 集成特征计算到 hash worker

**Files:**
- Modify: `internal/scanner/atomic.go`
- Modify: `internal/repository/data_distribution.go`（如需新增写入字段）
- Test: `internal/scanner/atomic_feature_integration_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
// internal/scanner/atomic_feature_integration_test.go
package scanner

import (
    "os"
    "path/filepath"
    "testing"
)

func TestAtomicScan_WritesFeatureCache(t *testing.T) {
    // setup: 临时目录 + 1 个 txt 文件
    tmp := t.TempDir()
    txt := filepath.Join(tmp, "doc.txt")
    os.WriteFile(txt, []byte("hello world test document for feature extraction"), 0644)

    db := setupAtomicScanTestDB(t)
    // 跑一次扫描（沿用 atomic_parallel_test.go 的 setup helper）
    runAtomicScanForTest(t, db, tmp)

    var row struct {
        ContentHash   *string `db:"content_hash"`
        ExtractedText *string `db:"extracted_text"`
        FeatureSize   *int64  `db:"feature_size"`
    }
    err := db.Get(&row,
        `SELECT content_hash, extracted_text, feature_size FROM data_distributing WHERE path = ?`, txt)
    if err != nil {
        t.Fatalf("query: %v", err)
    }
    if row.ContentHash == nil || *row.ContentHash == "" {
        t.Errorf("content_hash should be populated")
    }
    if row.ExtractedText == nil || *row.ExtractedText == "" {
        t.Errorf("extracted_text should be populated")
    }
    if row.FeatureSize == nil {
        t.Errorf("feature_size should be set")
    }
}

func TestAtomicScan_CorruptedFileStillHashes(t *testing.T) {
    tmp := t.TempDir()
    bad := filepath.Join(tmp, "bad.pdf")
    os.WriteFile(bad, []byte{0x25, 0x50, 0x44, 0x46, 0xff, 0xff}, 0644)

    db := setupAtomicScanTestDB(t)
    runAtomicScanForTest(t, db, tmp)

    var hash string
    err := db.Get(&hash, `SELECT content_sign FROM data_distributing WHERE path = ?`, bad)
    if err != nil {
        t.Fatalf("query: %v", err)
    }
    if hash == "" {
        t.Errorf("hash should still be written for corrupted file (extract failure must not block hash)")
    }
}
```

如果 `runAtomicScanForTest` helper 不存在，参考 `atomic_parallel_test.go` 的 setup 模式自建。

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 修改 atomic.go 的 hash worker 写入流程**

定位 atomic.go 中 hash worker 的 record 构建处（写 data_distributing 那行），在 record 字段填好之后、写入之前追加：

```go
// 串联特征计算（Spec B）
// 启用条件：feature_precompute_enabled 配置为 true（Task 7 接入 flag）
if featurePrecomputeEnabled() {
    info, statErr := os.Stat(path)
    if statErr == nil {
        feat, err := ExtractFeaturesForCache(path, mimeStr, info)
        if err != nil {
            log.Printf("[scan] feature extract failed for %s: %v", path, err)
        } else {
            rec.Simhash = &feat.Simhash
            rec.ContentHash = &feat.ContentHash
            rec.ExtractedText = &feat.ExtractedText
            rec.FeatureMtime = &feat.Mtime
            rec.FeatureSize = &feat.Size
            // phash 留待后续追加
        }
    }
}
```

`featurePrecomputeEnabled()` 临时占位（Task 7 替换为读 system_config）：

```go
func featurePrecomputeEnabled() bool {
    return true  // Task 7 改为读 system_config
}
```

然后 `repository/data_distribution.go` 中 `Create` / `BulkInsert` 等写入语句的 INSERT SQL 增加 6 个新列（如果使用 `INSERT INTO ... SELECT *` 类自动展开则无需改；如果是手写列名 INSERT，必须加）。

- [ ] **Step 4: 跑测试验证通过**

```bash
go test ./internal/scanner/ -run TestAtomicScan_WritesFeatureCache -v
go test ./internal/scanner/ -run TestAtomicScan_CorruptedFileStillHashes -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/scanner/atomic.go internal/repository/data_distribution.go \
        internal/scanner/atomic_feature_integration_test.go
git commit -m "feat(scan): integrate feature cache into hash worker (skip on error)"
```

---

## Task 5：similarity 包新增 feature cache 工具

**Files:**
- Create: `internal/similarity/feature_cache.go`
- Create: `internal/similarity/feature_cache_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/similarity/feature_cache_test.go
package similarity

import (
    "os"
    "path/filepath"
    "testing"
    "time"

    "data-asset-scan-go/internal/repository"
)

func TestReadCachedFeatures_ReturnsNilWhenAllNULL(t *testing.T) {
    db := repository.SetupTestDB(t)
    now := time.Now()
    _, _ = db.Exec(`INSERT INTO data_distributing
        (path, content_sign, file_create_time, file_size, create_time, update_time, disable, data_origin)
        VALUES ('/a', 'CS_NULL', ?, 1, ?, ?, 0, 'historical')`, now, now, now)

    cached, err := ReadCachedFeatures(db, "CS_NULL")
    if err != nil {
        t.Fatalf("read: %v", err)
    }
    if cached != nil {
        t.Errorf("expected nil for all-NULL row, got %+v", cached)
    }
}

func TestReadCachedFeatures_ReturnsValueWhenPopulated(t *testing.T) {
    db := repository.SetupTestDB(t)
    now := time.Now()
    _, _ = db.Exec(`INSERT INTO data_distributing
        (path, content_sign, file_create_time, file_size,
         simhash, content_hash, feature_mtime, feature_size,
         create_time, update_time, disable, data_origin)
        VALUES ('/b', 'CS_HIT', ?, 1, 12345, 'abc', ?, 100, ?, ?, 0, 'historical')`,
        now, now, now, now)

    cached, err := ReadCachedFeatures(db, "CS_HIT")
    if err != nil { t.Fatal(err) }
    if cached == nil { t.Fatal("expected non-nil") }
    if cached.Simhash != 12345 { t.Errorf("simhash = %d", cached.Simhash) }
    if cached.ContentHash != "abc" { t.Errorf("content_hash = %s", cached.ContentHash) }
    if cached.FeatureSize != 100 { t.Errorf("size = %d", cached.FeatureSize) }
}

func TestIsCacheValid_MtimeAndSizeMatch(t *testing.T) {
    tmp := t.TempDir()
    file := filepath.Join(tmp, "t.txt")
    os.WriteFile(file, []byte("hi"), 0644)
    info, _ := os.Stat(file)

    cached := &CachedFeaturesDB{
        FeatureMtime: info.ModTime(),
        FeatureSize:  info.Size(),
    }
    if !IsCacheValid(file, cached) {
        t.Errorf("should be valid when mtime+size match")
    }

    // mtime drift
    cached.FeatureMtime = info.ModTime().Add(-time.Hour)
    if IsCacheValid(file, cached) {
        t.Errorf("should be invalid when mtime differs")
    }

    // size drift
    cached.FeatureMtime = info.ModTime()
    cached.FeatureSize = info.Size() + 1
    if IsCacheValid(file, cached) {
        t.Errorf("should be invalid when size differs")
    }
}

func TestIsCacheValid_StatFail_ReturnsFalse(t *testing.T) {
    cached := &CachedFeaturesDB{FeatureMtime: time.Now(), FeatureSize: 100}
    if IsCacheValid("/nonexistent/path", cached) {
        t.Errorf("should be invalid when stat fails")
    }
}

func TestWriteBackFeatures_UpdatesRow(t *testing.T) {
    db := repository.SetupTestDB(t)
    now := time.Now()
    _, _ = db.Exec(`INSERT INTO data_distributing
        (path, content_sign, file_create_time, file_size, create_time, update_time, disable, data_origin)
        VALUES ('/c', 'CS_WB', ?, 1, ?, ?, 0, 'historical')`, now, now, now)

    err := WriteBackFeatures(db, "CS_WB", CachedFeaturesDB{
        Simhash: 99, ContentHash: "xyz", FeatureMtime: now, FeatureSize: 42,
    })
    if err != nil { t.Fatal(err) }

    var simhash int64
    db.Get(&simhash, `SELECT simhash FROM data_distributing WHERE content_sign='CS_WB'`)
    if simhash != 99 { t.Errorf("simhash after write = %d, want 99", simhash) }
}
```

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 实现工具**

```go
// internal/similarity/feature_cache.go
package similarity

import (
    "database/sql"
    "os"
    "time"

    "github.com/jmoiron/sqlx"
)

// CachedFeaturesDB 是从 data_distributing 读出的特征缓存。
// 不含 ExtractedText 字段：lazy load 时用 QueryExtractedText 单独按需查。
type CachedFeaturesDB struct {
    Simhash      uint64
    ContentHash  string
    Phash        string
    FeatureMtime time.Time
    FeatureSize  int64
}

// ReadCachedFeatures 读 data_distributing 一行的特征。
// 返回 nil 表示特征未持久化（视为 cache miss）。
func ReadCachedFeatures(db *sqlx.DB, contentSign string) (*CachedFeaturesDB, error) {
    var row struct {
        Simhash      *int64     `db:"simhash"`
        ContentHash  *string    `db:"content_hash"`
        Phash        *string    `db:"phash"`
        FeatureMtime *time.Time `db:"feature_mtime"`
        FeatureSize  *int64     `db:"feature_size"`
    }
    err := db.Get(&row,
        `SELECT simhash, content_hash, phash, feature_mtime, feature_size
         FROM data_distributing WHERE content_sign = ? AND disable = 0 LIMIT 1`, contentSign)
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    // 任一关键字段 NULL 视为未缓存（mtime+size 必须都有才能做失效检测）
    if row.FeatureMtime == nil || row.FeatureSize == nil {
        return nil, nil
    }
    c := &CachedFeaturesDB{
        FeatureMtime: *row.FeatureMtime,
        FeatureSize:  *row.FeatureSize,
    }
    if row.Simhash != nil {
        c.Simhash = uint64(*row.Simhash)
    }
    if row.ContentHash != nil {
        c.ContentHash = *row.ContentHash
    }
    if row.Phash != nil {
        c.Phash = *row.Phash
    }
    return c, nil
}

// IsCacheValid 通过 stat 当前文件，对比 mtime+size 判断缓存是否仍有效。
// stat 失败 / mtime 不一致 / size 不一致 → 视为失效。
func IsCacheValid(filePath string, cached *CachedFeaturesDB) bool {
    if cached == nil {
        return false
    }
    info, err := os.Stat(filePath)
    if err != nil {
        return false
    }
    if info.Size() != cached.FeatureSize {
        return false
    }
    if !info.ModTime().Equal(cached.FeatureMtime) {
        return false
    }
    return true
}

// WriteBackFeatures cache miss 现场重算后回写到 DB，修复缓存。
func WriteBackFeatures(db *sqlx.DB, contentSign string, feat CachedFeaturesDB) error {
    _, err := db.Exec(
        `UPDATE data_distributing
         SET simhash = ?, content_hash = ?, phash = ?, feature_mtime = ?, feature_size = ?,
             update_time = ?
         WHERE content_sign = ? AND disable = 0`,
        int64(feat.Simhash), feat.ContentHash, feat.Phash,
        feat.FeatureMtime, feat.FeatureSize, time.Now(), contentSign)
    return err
}

// WriteBackExtractedText cache miss 重算时同时回写文本（独立函数，
// 因为重算时通常一并算出所有 features + text）。
func WriteBackExtractedText(db *sqlx.DB, contentSign, text string) error {
    _, err := db.Exec(
        `UPDATE data_distributing SET extracted_text = ?, update_time = ?
         WHERE content_sign = ? AND disable = 0`,
        text, time.Now(), contentSign)
    return err
}

// QueryExtractedText lazy load 单条 extracted_text（pair similarity 阶段用）。
// 返回空字符串表示未缓存（调用方应 fallback 现场 extractText）。
func QueryExtractedText(db *sqlx.DB, contentSign string) string {
    var text *string
    err := db.Get(&text,
        `SELECT extracted_text FROM data_distributing
         WHERE content_sign = ? AND disable = 0 LIMIT 1`, contentSign)
    if err != nil || text == nil {
        return ""
    }
    return *text
}
```

注意：`repository.SetupTestDB(t)` 如果不存在则用项目内现有 helper 替换。

- [ ] **Step 4: 跑测试验证通过**

```bash
go test ./internal/similarity/ -run TestReadCachedFeatures -v
go test ./internal/similarity/ -run TestIsCacheValid -v
go test ./internal/similarity/ -run TestWriteBackFeatures -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/similarity/feature_cache.go internal/similarity/feature_cache_test.go
git commit -m "feat(similarity): feature cache utils (Read/IsValid/WriteBack/QueryText)"
```

---

## Task 6：BuildFamilies extractFeatures 改造 - cache 优先

**Files:**
- Modify: `internal/similarity/similarity.go`
- Test: `internal/similarity/build_families_cache_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
// internal/similarity/build_families_cache_test.go
package similarity

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"
)

// 验证 cache hit 时 ExtractTextWithTimeout 不被调用。
// 通过 spy global pattern：用一个 var 包住函数，测试时替换。
func TestBuildFamilies_UsesCacheWhenAvailable(t *testing.T) {
    // setup: 1 个文件，在 DB 里已经有 features
    tmp := t.TempDir()
    file := filepath.Join(tmp, "doc.txt")
    os.WriteFile(file, []byte("hello world content"), 0644)

    db := setupTestDBWithCachedRow(t, file, "CS_CACHED")

    callCount := 0
    origExtract := extractTextHook
    extractTextHook = func(path string, timeout time.Duration) string {
        callCount++
        return ""
    }
    defer func() { extractTextHook = origExtract }()

    cfg := defaultConfig()
    inputs := loaderFromDB(db).MustLoad(t)
    _, err := BuildFamilies(context.Background(), inputs, cfg)
    if err != nil { t.Fatal(err) }

    if callCount != 0 {
        t.Errorf("extractTextHook called %d times, want 0 (should hit cache)", callCount)
    }
}

func TestBuildFamilies_CacheMissTriggersFallbackAndWriteback(t *testing.T) {
    tmp := t.TempDir()
    file := filepath.Join(tmp, "fresh.txt")
    os.WriteFile(file, []byte("new content"), 0644)

    db := setupTestDBNoCachedRow(t, file, "CS_FRESH")

    cfg := defaultConfig()
    inputs := loaderFromDB(db).MustLoad(t)
    _, _ = BuildFamilies(context.Background(), inputs, cfg)

    // 验证 features 被回写
    var contentHash *string
    db.Get(&contentHash, `SELECT content_hash FROM data_distributing WHERE content_sign='CS_FRESH'`)
    if contentHash == nil || *contentHash == "" {
        t.Errorf("expected content_hash written back after cache miss, got NULL")
    }
}
```

helper 函数 `setupTestDBWithCachedRow`、`setupTestDBNoCachedRow`、`loaderFromDB` 按测试需要编写。

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 修改 similarity.go**

在 `internal/similarity/similarity.go` 文件头部追加 spy hook：

```go
// extractTextHook 用于测试时替换 textextract 调用。
// 默认走真实的 extractTextWithTimeout。
var extractTextHook = extractTextWithTimeout
```

修改 `buildFamilies` 中 `extractFeatures` 阶段（line ~1534-1584）。原循环：

```go
for i, gid := range sortedGIDs {
    rep := byGroup[gid]
    // ...原代码 extractTextWithTimeout(rep.FileFullPath, 10s)...
}
```

改为：

```go
db := getSimilarityDB()  // 通过 package-level injector 拿到 *sqlx.DB，避免硬依赖

for i, gid := range sortedGIDs {
    rep := byGroup[gid]
    select { case <-ctx.Done(): return nil; default: }

    if (i+1)%100 == 0 {
        log.Printf("Extracted features %d/%d", i+1, len(byGroup))
    }
    bucket := mimeBucket(rep.FileMIME)

    // Spec B: 优先读 cache
    cached, _ := ReadCachedFeatures(db, rep.ContentSign)
    if cached != nil && IsCacheValid(rep.FileFullPath, cached) {
        cache[gid] = featureCache{
            simhash:     cached.Simhash,
            contentHash: cached.ContentHash,
            bucket:      bucket,
            rep:         rep,
            fullText:    "",  // lazy load by worker pool
        }
        continue
    }

    // Cache miss → 走现场重算（原有路径）
    var sh uint64
    var ch, fullText string
    if bucket == "doc" || bucket == "code" {
        fullText = extractTextHook(rep.FileFullPath, 10*time.Second)
        truncatedForSimhash := fullText
        if runes := []rune(fullText); len(runes) > simhashCharLimit {
            truncatedForSimhash = string(runes[:simhashCharLimit])
        }
        if truncatedForSimhash != "" {
            sh = simhash(truncatedForSimhash)
        }
        ch = contentHashFromText(fullText)

        // 回写 cache
        info, statErr := os.Stat(rep.FileFullPath)
        if statErr == nil {
            _ = WriteBackFeatures(db, rep.ContentSign, CachedFeaturesDB{
                Simhash: sh, ContentHash: ch,
                FeatureMtime: info.ModTime(), FeatureSize: info.Size(),
            })
            if fullText != "" {
                _ = WriteBackExtractedText(db, rep.ContentSign, fullText)
            }
        }
    } else {
        feat := extractFeature(rep.FileFullPath)
        sh = feat.SimhashValue
    }

    cache[gid] = featureCache{
        simhash:     sh,
        contentHash: ch,
        bucket:      bucket,
        rep:         rep,
        fullText:    fullText,
    }
}
```

`getSimilarityDB` 通过 package var 注入：

```go
// internal/similarity/similarity.go
var injectedDB *sqlx.DB

// SetDB 由 cmd/main.go 在启动时注入，给 BuildFamilies 用。
// 测试中也可以通过 testSetDB 注入 mock DB。
func SetDB(db *sqlx.DB) { injectedDB = db }
func getSimilarityDB() *sqlx.DB { return injectedDB }
```

`cmd/main.go` 启动时调一次：

```go
similarity.SetDB(repository.GetDB())
```

- [ ] **Step 4: 跑测试验证通过**

```bash
go test ./internal/similarity/ -run TestBuildFamilies_UsesCacheWhenAvailable -v
go test ./internal/similarity/ -run TestBuildFamilies_CacheMissTriggersFallbackAndWriteback -v
```

- [ ] **Step 5: 跑全部 similarity 测试，确认回归**

```bash
go test ./internal/similarity/ -count=1 -v 2>&1 | tail -30
# 期望：原有测试全 PASS + 新测试 PASS
```

- [ ] **Step 6: Commit**

```bash
git add internal/similarity/similarity.go internal/similarity/build_families_cache_test.go cmd/main.go
git commit -m "feat(similarity): BuildFamilies prefer DB feature cache, fallback to live extract"
```

---

## Task 7：worker pool lazy load extracted_text

**Files:**
- Modify: `internal/similarity/similarity.go`
- Test: `internal/similarity/pair_lazy_load_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
// internal/similarity/pair_lazy_load_test.go
package similarity

import "testing"

func TestPairSimilarity_LazyLoadFromDB(t *testing.T) {
    // setup: 两个相似文档，DB 里 extracted_text 已存
    // BuildFamilies 跑完后，pair similarity 阶段应该通过 QueryExtractedText 读 DB
    // 验证 docSimilarityFromText 被调用（hook spy）
    // 而 computeSimilarity 不被调用（不重读文件）
    t.Skip("Spec B Task 7：需要 BuildFamilies 与 worker pool 完整链路；待 Task 6 完成后落地")
}
```

> 此测试形态较重，先用 Skip 标记并在 Step 3 实现后补全；建议实施时 Step 1 跳过、Step 3 实现后再回写测试体。

- [ ] **Step 2: 跑测试，确认 skip 状态**

```bash
go test ./internal/similarity/ -run TestPairSimilarity_LazyLoadFromDB -v
# 期望：SKIP
```

- [ ] **Step 3: 改 worker pool 中 pair similarity 逻辑**

定位 `internal/similarity/similarity.go:1681` 附近的 pair similarity 计算：

原代码：
```go
if (bucket == "doc" || bucket == "code") && cacheA.fullText != "" && cacheB.fullText != "" {
    result = docSimilarityFromText(cacheA.fullText, cacheB.fullText)
} else {
    result = computeSimilarity(cacheA.rep.FileFullPath, cacheB.rep.FileFullPath, bucket)
}
```

改为：

```go
if bucket == "doc" || bucket == "code" {
    textA := cacheA.fullText
    if textA == "" {
        textA = QueryExtractedText(getSimilarityDB(), cacheA.rep.ContentSign)
    }
    textB := cacheB.fullText
    if textB == "" {
        textB = QueryExtractedText(getSimilarityDB(), cacheB.rep.ContentSign)
    }
    if textA != "" && textB != "" {
        result = docSimilarityFromText(textA, textB)
    } else {
        result = computeSimilarity(cacheA.rep.FileFullPath, cacheB.rep.FileFullPath, bucket)
    }
} else {
    result = computeSimilarity(cacheA.rep.FileFullPath, cacheB.rep.FileFullPath, bucket)
}
```

- [ ] **Step 4: 补全 Step 1 跳过的测试**

```go
// 替换 Skip
func TestPairSimilarity_LazyLoadFromDB(t *testing.T) {
    // setup 2 files with cached extracted_text in DB
    // run BuildFamilies
    // assert pair similarity used DB text, not file re-read
    // 这里可以用 hook 替换 computeSimilarity，断言其零调用
}
```

- [ ] **Step 5: 跑测试验证通过**

- [ ] **Step 6: Commit**

```bash
git add internal/similarity/similarity.go internal/similarity/pair_lazy_load_test.go
git commit -m "feat(similarity): lazy-load extracted_text from DB in pair worker"
```

---

## Task 8：feature flag 接入

**Files:**
- Modify: `internal/repository/system_config.go`, `internal/repository/db.go`
- Modify: `internal/scanner/feature_cache.go` 或 `atomic.go`（替换 featurePrecomputeEnabled）
- Modify: `internal/similarity/similarity.go`（cache 读取分支可被 flag 关闭）
- Test: `internal/scanner/feature_flag_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
func TestFeatureFlag_DisablesPrecompute(t *testing.T) {
    db := setupAtomicScanTestDB(t)
    cfgRepo := repository.NewSystemConfigRepository(db)
    cfgRepo.Set(repository.KeyFeaturePrecomputeEnabled, "false")

    // 扫描一个文件
    tmp := t.TempDir()
    f := filepath.Join(tmp, "x.txt")
    os.WriteFile(f, []byte("test"), 0644)
    runAtomicScanForTest(t, db, tmp)

    var ext *string
    db.Get(&ext, `SELECT extracted_text FROM data_distributing WHERE path=?`, f)
    if ext != nil && *ext != "" {
        t.Errorf("flag off → extracted_text should be NULL, got %q", *ext)
    }
}
```

- [ ] **Step 2: 跑测试验证失败**

- [ ] **Step 3: 注册 key + 默认值**

`internal/repository/system_config.go`：

```go
const KeyFeaturePrecomputeEnabled = "feature_precompute_enabled"
```

`internal/repository/db.go` 的 `runMigrations` 末尾 seed：

```go
_, _ = db.Exec(`INSERT OR IGNORE INTO system_config (key, type, value, create_time, update_time, disable)
    VALUES ('feature_precompute_enabled', 'string', 'true', ?, ?, 0)`, time.Now(), time.Now())
```

- [ ] **Step 4: 修改 scanner.featurePrecomputeEnabled**

```go
// internal/scanner/feature_cache.go 或 atomic.go
func featurePrecomputeEnabled() bool {
    repo := repository.NewSystemConfigRepository(repository.GetDB())
    v, err := repo.Get(repository.KeyFeaturePrecomputeEnabled)
    if err != nil { return true }  // 默认开启
    return v != "false"
}
```

> 注意：scanner 引用 `repository` 包；如果会出循环依赖，把 flag check 抽到调用方（atomic.go scan 入口）。

similarity 端类似：BuildFamilies 入口处检查 flag，flag off 则跳过 cache 优先路径，全部走现场重算。

- [ ] **Step 5: 跑测试验证通过**

- [ ] **Step 6: Commit**

```bash
git add internal/repository/system_config.go internal/repository/db.go \
        internal/scanner/feature_cache.go internal/similarity/similarity.go \
        internal/scanner/feature_flag_test.go
git commit -m "feat(flag): feature_precompute_enabled gating for safe rollout/rollback"
```

---

## Task 9：pin determinism 集成测试

**Files:**
- Test: `internal/similarity/build_families_determinism_test.go`（新建）

- [ ] **Step 1: 写测试 - 新旧实现结果一致**

```go
// internal/similarity/build_families_determinism_test.go
package similarity

import (
    "context"
    "encoding/json"
    "fmt"
    "sort"
    "testing"
)

// 跑两次 BuildFamilies：第一次 cache miss（全部现场算），第二次 cache hit（全部走 DB）。
// 断言两次产生的 family 结构完全一致（按 primary content_sign + member content_sign 排序后比对）。
func TestBuildFamilies_DeterministicAcrossCacheState(t *testing.T) {
    // setup: 用 internal/scanner 现有 53 文件 fixture（atomic_parallel_test 用过的）
    fixtureFiles, fixtureDB := setupFixture53Files(t)
    SetDB(fixtureDB)

    inputs := loaderFromDB(fixtureDB).MustLoad(t)
    cfg := defaultConfig()

    // 第一次：清空所有 features 走全 miss
    fixtureDB.Exec(`UPDATE data_distributing SET simhash=NULL, content_hash=NULL,
        extracted_text=NULL, feature_mtime=NULL, feature_size=NULL`)
    fams1, err := BuildFamilies(context.Background(), inputs, cfg)
    if err != nil { t.Fatal(err) }
    snap1 := snapshotFamilies(fams1)

    // 第二次：现在 DB 里 features 已被 cache miss 写回 → 全 hit
    fams2, err := BuildFamilies(context.Background(), inputs, cfg)
    if err != nil { t.Fatal(err) }
    snap2 := snapshotFamilies(fams2)

    if snap1 != snap2 {
        t.Errorf("BuildFamilies non-deterministic across cache state:\nmiss:\n%s\nhit:\n%s",
            snap1, snap2)
    }
    _ = fixtureFiles
}

func snapshotFamilies(fams []SameSourceFamily) string {
    type famSnap struct {
        Primary string   `json:"primary"`
        Members []string `json:"members"`
        Score   float64  `json:"score"`
    }
    snaps := make([]famSnap, 0, len(fams))
    for _, f := range fams {
        memberSigns := []string{}
        for _, m := range f.MemberScores {
            _ = m  // 实际取 content_sign，要从 FamilyMember 拿
        }
        sort.Strings(memberSigns)
        snaps = append(snaps, famSnap{Primary: f.PrimaryFileID, Members: memberSigns, Score: f.Score})
    }
    sort.Slice(snaps, func(i, j int) bool { return snaps[i].Primary < snaps[j].Primary })
    b, _ := json.MarshalIndent(snaps, "", "  ")
    return fmt.Sprintf("%s", b)
}
```

> 复用 `setupFixture53Files` helper（参考 `internal/scanner/atomic_parallel_test.go` 中的 53 文件 fixture 构造代码）。

- [ ] **Step 2: 跑测试验证通过**

```bash
go test ./internal/similarity/ -run TestBuildFamilies_DeterministicAcrossCacheState -v
# 期望：PASS（如果失败：说明 cache hit 路径与现场算路径产出不同，必须 debug）
```

- [ ] **Step 3: race detector 跑一遍 scanner + similarity**

```bash
go test -race ./internal/scanner/... ./internal/similarity/... -count=1 2>&1 | tail -20
# 期望：无 race detected
```

- [ ] **Step 4: Commit**

```bash
git add internal/similarity/build_families_determinism_test.go
git commit -m "test(similarity): pin BuildFamilies determinism across cache miss/hit states"
```

---

## Task 10：性能基准测试

**Files:**
- Test: `internal/similarity/build_families_bench_test.go`（新建）

- [ ] **Step 1: 写 benchmark**

```go
// internal/similarity/build_families_bench_test.go
package similarity

import (
    "context"
    "testing"
)

func BenchmarkBuildFamilies_CacheMiss(b *testing.B) {
    _, db := setupFixture53Files(&testing.T{})
    SetDB(db)
    inputs := loaderFromDB(db).MustLoad(&testing.T{})
    cfg := defaultConfig()

    for i := 0; i < b.N; i++ {
        db.Exec(`UPDATE data_distributing SET simhash=NULL, content_hash=NULL,
            extracted_text=NULL, feature_mtime=NULL, feature_size=NULL`)
        _, _ = BuildFamilies(context.Background(), inputs, cfg)
    }
}

func BenchmarkBuildFamilies_CacheHit(b *testing.B) {
    _, db := setupFixture53Files(&testing.T{})
    SetDB(db)
    inputs := loaderFromDB(db).MustLoad(&testing.T{})
    cfg := defaultConfig()

    // 先跑一次让 cache 写满
    _, _ = BuildFamilies(context.Background(), inputs, cfg)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = BuildFamilies(context.Background(), inputs, cfg)
    }
}
```

- [ ] **Step 2: 跑 benchmark**

```bash
go test ./internal/similarity/ -bench BenchmarkBuildFamilies -benchtime=1x -v 2>&1 | grep -E "Benchmark|ns/op"
# 期望：cache hit 比 cache miss 至少快 5x（53 文件场景可能小，更明显的差距要更大数据集）
```

- [ ] **Step 3: 记录基准数字到 commit message**

```bash
git add internal/similarity/build_families_bench_test.go
git commit -m "test(similarity): benchmark BuildFamilies cache hit vs miss

53-file fixture:
- cache miss: <X> ms
- cache hit:  <Y> ms (Z× speedup)"
```

---

## 验收 checklist

- [ ] Task 1：DB 6 列 + content_hash 索引迁移到位
- [ ] Task 2：DataDistribution model 字段就绪
- [ ] Task 3：ExtractFeaturesForCache 函数 + 3 个单测通过
- [ ] Task 4：scanner 集成特征计算，损坏文件不阻塞 hash
- [ ] Task 5：similarity.feature_cache.go 工具 + 测试通过
- [ ] Task 6：BuildFamilies cache hit 跳过 extract + miss 回写
- [ ] Task 7：worker pool lazy load extracted_text
- [ ] Task 8：feature_precompute_enabled flag 可控制启停
- [ ] Task 9：pin determinism 测试通过（cache miss/hit 结果一致）
- [ ] Task 10：benchmark 显示明显提速
- [ ] race detector clean（`go test -race ./internal/scanner/... ./internal/similarity/...`）
- [ ] 全套回归测试通过（`go test ./... -count=1`）

---

## 风险与回滚

| 风险 | 缓解 |
|---|---|
| Scanner 改动引入 bug 影响现有扫描 | 严格 TDD；feature flag `feature_precompute_enabled=false` 立即回退；现场重算 fallback 永久保留 |
| BuildFamilies cache hit 路径有 bug 导致家族错乱 | Task 9 pin determinism 测试守护；feature flag 紧急可关 |
| 扫描时间从 5 分钟涨到 25 分钟 | spec 文档已记录此预期；后续若反馈强烈，把 ExtractFeaturesForCache 塞进现有 hash worker pool 并发（scanner 已有并行能力） |
| DB 体积膨胀（万文件 1-2GB） | 不在本 plan 范围；若实际反馈过大，加 truncate 是清晰的加法改动 |
| stat 失败 / NULL safety | ReadCachedFeatures 在任一关键字段 NULL 时返回 nil；IsCacheValid 在 stat 失败时返回 false → 均走 fallback，无副作用 |

回滚步骤（紧急）：

```bash
# 1. 通过 API 或直接 SQL 关 flag
UPDATE system_config SET value='false' WHERE key='feature_precompute_enabled';

# 2. （可选）revert 最近几个 commit
git revert <commit-range>
```

flag off 后行为：scanner 不算 features（保留 hash）、BuildFamilies 全走现场重算，等价于改造前。
