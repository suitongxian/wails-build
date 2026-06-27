# Spec B：扫描期预计算文件特征

- 日期：2026-05-26
- 作者：与用户结对设计
- 状态：待实现
- 关联：Spec A（相似度分析的交互重设计）

## 背景与动机

`BuildFamilies` 当前流程中，`extractFeatures` 阶段（`internal/similarity/similarity.go:1534-1584`）占用大头耗时：每个 hash group 的代表文件都要 ① 打开文件 ② 按格式解 PDF/DOCX/PPTX 出文本 ③ 算 simhash ④ contentHash。

实测（典型 1000 文件办公电脑）：

| 阶段 | 耗时 |
|---|---|
| LoadInputs | 几秒 |
| **extractFeatures** | **30+ 分钟** |
| selectCandidates | 几秒 |
| worker pool 算 pair similarity | 2-5 分钟 |
| **总计** | **~35 分钟** |

这导致两个问题：
1. Spec A 想做的"扫描完后台自动跑 BuildFamilies → 用户无感"无法成立，30+ 分钟空窗期用户感知明显
2. 用户每次想重新调整阈值或人工修家族后重算，都要再等 30 分钟

本 spec 通过**把 features 计算挪到扫描阶段并持久化**，让 BuildFamilies 跳过 extract 阶段，整体提速到 ~5 分钟（cache 命中常态）。

## 范围与边界

**做：**
- `data_distributing` 表增 6 列存特征
- Scanner 在算完 SHA-256 后串联特征计算并持久化
- BuildFamilies 改造 `extractFeatures` 阶段：先读 DB cache + stat 比对失效，命中跳过现场重算
- Cache miss 时现场重算 + 回写 DB（修复缓存）
- `LoadInputs` 改 lazy load fullText，避免内存压力
- Migration：新列允许 NULL，老数据自动 fallback

**不做：**
- 不动 BuildFamilies 算法（selectCandidates / workerPool / Union-Find / persist 全部保留）
- 不动 textextract 包（直接复用现有 API）
- 不动 scanner 物理文件操作（继续遵守 CLAUDE.md「严禁修改扫描文件」）
- 不删除现场重算路径（作为 fallback 永久保留）

## 关键设计决策

### 失效检测：mtime + size

cache 有效性判断：

```
stat(file_path) → 当前 mtime, current_size
对比 DB 中 feature_mtime, feature_size
一致 → cache hit，用 DB features
不一致 → cache miss，现场重算并回写
```

边界 case：
- mtime 改但 size 不变（理论可能，比如 touch + 同字节数替换）→ 视为 miss，保守路径，无副作用
- size 改但 mtime 不变（极罕见）→ 视为 miss，同上
- stat 失败（文件被删/挪了）→ 视为 miss，fallback 也失败 → BuildFamilies 跳过该文件，warn 日志

### 不同的内存策略：fullText lazy load

fullText 体积大（每个文档几十 KB 到几 MB）。如果 `LoadInputs` 一次性把所有 fullText 读进内存：

| 文件量 | 平均 fullText | 内存占用 |
|---|---|---|
| 1 万 | 20 KB | 200 MB |
| 5 万 | 30 KB | 1.5 GB |

桌面 app 在 5 万级会撑爆。采用 **lazy load**：

```
LoadInputs：只读 metadata + simhash + contentHash + feature_mtime + feature_size
            （不读 extracted_text）

extractFeatures 阶段：cache hit 时 fullText 暂为空

worker pool pair similarity：算到某对时按需 query DB 拿这两个文件的 extracted_text
                              用完即弃（worker 局部变量自动释放）
```

worker 并发数 = `runtime.NumCPU()`（与当前一致）；峰值内存 = `worker_count × 2 × avg_fullText` ≈ 几 MB，与文件总数无关。

### 不截断 fullText

存原始全文，不做长度上限。理由：
- TF-IDF 在长文本下精度更高，截断会损失相似度判定准确度
- 超大文档（>1 MB 抽出文本）罕见
- SQLite TEXT 字段无大小硬限制（理论 1GB+）
- DB 体积影响：5 万文件 × 30 KB ≈ 1.5 GB；可接受（桌面端用户存储一般 100GB+）

如果未来真出现 DB 体积问题，加截断是清晰的加法改动。

## 设计

### 1. DB schema 变更

新建 migration `internal/migrations/NNNN_add_feature_cache_to_data_distributing.up.sql`：

```sql
ALTER TABLE data_distributing ADD COLUMN simhash        INTEGER NULL;
ALTER TABLE data_distributing ADD COLUMN content_hash   TEXT    NULL;
ALTER TABLE data_distributing ADD COLUMN phash          TEXT    NULL;
ALTER TABLE data_distributing ADD COLUMN extracted_text TEXT    NULL;
ALTER TABLE data_distributing ADD COLUMN feature_mtime  DATETIME NULL;
ALTER TABLE data_distributing ADD COLUMN feature_size   INTEGER  NULL;

CREATE INDEX IF NOT EXISTS idx_data_distributing_content_hash
    ON data_distributing(content_hash) WHERE content_hash IS NOT NULL;
```

`content_hash` 加索引：BuildFamilies 步骤 1.5（跨格式同内容合并）需要按 content_hash 分组，加索引让这步秒级完成。

`extracted_text` 不加索引（不参与查询条件）。

升级路径：所有现有行 6 列保持 NULL → BuildFamilies 视为 cache miss → 走 fallback 现场重算并回写 → 第二次跑就是 cache hit。**零中断**。

### 2. Scanner 改动

`internal/scanner/atomic.go` 的写入流程（伪代码示意，实际嵌入现有 worker pool）：

```go
// 现状：扫描 worker 计算 SHA-256 后写 data_distributing
rec := computeHash(path)

// 新增：串联特征计算
if shouldExtractFeatures(rec.MIME) {
    features, err := extractFeaturesForCache(path, rec.MIME)
    if err == nil {
        rec.Simhash = features.Simhash
        rec.ContentHash = features.ContentHash
        rec.Phash = features.Phash
        rec.ExtractedText = features.ExtractedText
        rec.FeatureMtime = features.Mtime
        rec.FeatureSize = features.Size
    }
    // err != nil：features 字段留 NULL，hash 仍写入，warn 日志
}

writeDataDistributing(rec)
```

`extractFeaturesForCache` 按 MIME 分发（参照 BuildFamilies 现有逻辑）：

| MIME 桶 | 计算 |
|---|---|
| doc / code | `textextract.ExtractTextWithTimeout(path, 10s)` → `simhash(text)` + `contentHashFromText(text)` |
| img | `pHashFile(path)`（复用 similarity 包现有逻辑） |
| 其他 | 跳过（与 BuildFamilies "其他桶"语义一致） |

**关键约束**：
- extract 失败不阻塞 hash 写入（hash 写入是扫描器的首要契约）
- 失败 path 在 warn 日志可见（便于排查）
- 不修改扫描发现的物理文件（严守 CLAUDE.md 第三条铁律）

### 3. BuildFamilies 改动

`internal/similarity/analyzer.go` 中 `LoadInputs`：

```go
// 现状：SELECT * FROM data_distributing WHERE disable = 0
// 改为：SELECT 除 extracted_text 外的所有列
```

`internal/similarity/similarity.go:1534-1584` 的 `extractFeatures` 阶段：

```go
for _, gid := range sortedGIDs {
    rep := byGroup[gid]
    cached := readCachedFeatures(rep.ContentSign)
    if cached != nil && isCacheValid(rep.Path, cached) {
        cache[gid] = featureCache{
            simhash:     cached.Simhash,
            contentHash: cached.ContentHash,
            fullText:    "",  // lazy load，pair 阶段按需查
            bucket:      mimeBucket(rep.FileMIME),
            rep:         rep,
        }
        continue
    }
    // cache miss / 文件已改 / 老数据 NULL → 走现有现场提取路径
    // ... 现有代码不变 ...
    // 算完后回写 DB
    writeBackFeatures(rep.ContentSign, computedFeatures)
}
```

`internal/similarity/similarity.go:1681` worker pool 算 pair similarity：

```go
// 现状：if (bucket == "doc" || bucket == "code") && cacheA.fullText != "" && cacheB.fullText != "" {
//          result = docSimilarityFromText(cacheA.fullText, cacheB.fullText)
//      } else {
//          result = computeSimilarity(cacheA.rep.FileFullPath, cacheB.rep.FileFullPath, bucket)
//      }

// 改为：
if bucket == "doc" || bucket == "code" {
    textA := cacheA.fullText
    if textA == "" {
        textA = queryExtractedText(cacheA.rep.ContentSign)  // lazy load 从 DB
    }
    textB := cacheB.fullText
    if textB == "" {
        textB = queryExtractedText(cacheB.rep.ContentSign)
    }
    if textA != "" && textB != "" {
        result = docSimilarityFromText(textA, textB)
    } else {
        result = computeSimilarity(cacheA.rep.FileFullPath, cacheB.rep.FileFullPath, bucket)
    }
}
```

`queryExtractedText`：
- 单条 `SELECT extracted_text FROM data_distributing WHERE content_sign = ? LIMIT 1`
- SQLite 本地查询微秒级，worker 并发查 SQLite 安全（启用 WAL 模式时多读单写）

### 4. 失败与降级矩阵

| 场景 | 行为 |
|---|---|
| extractText 失败（损坏 PDF / 加密 / 未知格式） | features 字段留 NULL；hash 仍写入；warn 日志 |
| 写 features 时 DB 错误 | 不阻塞 hash 写入（hash 在前）；features 留 NULL；error 日志 |
| BuildFamilies stat 失败 | 视为 cache miss，走 fallback；fallback 若也失败 → 跳过该文件，warn |
| lazy load extracted_text 查询失败 | fallback 走现场 extractText 一次性重算这对 |
| Migration 失败 | 新列允许 NULL，回滚不影响业务；BuildFamilies 读取端已天然处理 NULL（视为 cache miss 走 fallback） |

### 5. 增量扫描 / 重扫的语义

| 触发 | features 处理 |
|---|---|
| 首次普查（全新扫描） | 对每个文件都算 features 并入库 |
| 增量扫描（用户新增文件） | 只对新文件算 features 入库；已存在文件不动 |
| 用户改了文件后重扫（mtime 变化） | 视为"新版本"，按新 hash 写入新行（现有行为）；features 跟随新行算 |
| 用户手动点"重建相似关系" | 走 cache miss 路径只针对失效文件，不重算全部 |

## 测试矩阵

| 测试 | 内容 |
|---|---|
| Scanner 单测：features 字段写入正确性 | 喂 fixture pdf/docx/img/code/损坏文件，断言 DB 里 simhash/content_hash/phash/extracted_text 与期望一致 |
| Scanner 单测：extract 失败不阻塞 hash | 喂损坏 pdf，断言 hash 写入成功 + features 全部 NULL |
| Scanner 单测：feature_mtime + feature_size 写入 | 喂正常文件，断言这两个字段等于文件 stat 结果 |
| BuildFamilies 单测：cache hit 跳过 extract | seed DB 里 features 已存 + mtime 一致，spy extractTextWithTimeout 不被调用 |
| BuildFamilies 单测：cache miss 现场重算 + 回写 | seed mtime 不一致，断言 extractTextWithTimeout 被调用 + DB features 被更新 |
| BuildFamilies 单测：老数据 NULL fallback | seed 全部 features 为 NULL（升级场景），断言走 fallback，结果与现状一致 |
| BuildFamilies 单测：lazy load extracted_text | spy queryExtractedText 仅在 pair 算到 cache hit 文件对时被调用 |
| 集成测：扫描→自动 BuildFamilies→结果与现有逻辑一致 | pin determinism 测试（53 文件 fixture）：旧实现跑一次记录 family 结构，新实现跑断言一致 |
| 集成测：升级路径 | 旧 DB（features 全 NULL）跑 BuildFamilies，断言结果与现状完全一致 |
| 集成测：cache 命中率 100% 时整体耗时 | 跑两次：第一次 cache miss（记录耗时 T1），第二次 cache hit（记录耗时 T2），断言 T2 < T1 / 5 |
| 并发安全测：scanner + BuildFamilies 同时跑（极端边界） | spawn scanner write 与 BuildFamilies read 同时进行，断言无数据竞争（race detector clean） |

测试前必须 `npm rebuild better-sqlite3`（CLAUDE.md 第五条）。

## 性能预期

| 指标 | 改造前 | 改造后（cache 100% hit） | 改造后（cache 0% hit） |
|---|---|---|---|
| 扫描时间（1000 文件） | ~5 分钟 | ~25 分钟 | 同上 |
| BuildFamilies 时间 | ~35 分钟 | ~5 分钟 | ~35 分钟（同改造前） |
| 扫描完到家族可用 | ~40 分钟 | ~30 分钟 | ~60 分钟（worst case） |
| **重复 BuildFamilies**（用户重建场景） | ~35 分钟 | **~5 分钟** | ~35 分钟 |
| 内存峰值（5 万文件） | <100 MB | ~10 MB（lazy load） | ~10 MB |

体验红利：
- 配合 Spec A 的"扫描完自动 BuildFamilies"，用户从扫描完成到能用家族功能的间隔从 35 分钟降到 5 分钟（cache 命中常态）
- 用户多次重算（调阈值、人工修家族后）每次省 30 分钟
- 内存稳定，不受文件总量影响

## 验收标准

- [ ] Migration 成功执行，老 DB 升级后无错误
- [ ] 扫描器写入 features 字段（喂 fixture 文件能查到非 NULL 值）
- [ ] 扫描器遇损坏文件不报错，hash 仍写入
- [ ] BuildFamilies cache hit 时跳过 extractText 调用
- [ ] BuildFamilies cache miss 时正确 fallback 现场重算并回写 DB
- [ ] LoadInputs 不读 extracted_text，pair 阶段按需 lazy load
- [ ] 1000 文件 fixture 在 cache 100% hit 时 BuildFamilies < 10 分钟
- [ ] race detector clean（`go test -race ./internal/similarity/... ./internal/scanner/...`）
- [ ] 集成测试断言新实现与旧实现 family 结构一致（pin determinism）
- [ ] 所有改动通过 TDD 流程（CLAUDE.md「每个任务都要测试用例验证」）

## 风险与回滚

| 风险 | 缓解 |
|---|---|
| 扫描时间从 5 分钟涨到 25 分钟，用户感知扫描"变慢"了 | 文案改造：进度条标识"扫描中（含特征提取）"；特征提取塞进现有 hash worker pool 与 hash 计算并发（scanner 已有并行 worker pool，参见此前 D-1/B 优化） |
| Migration 在大 DB 上慢 | ALTER TABLE ADD COLUMN 在 SQLite 是 metadata 级操作，瞬时；唯一索引创建可能慢但 content_hash 索引是 partial index 仅对非 NULL 行生效 |
| extracted_text 让 DB 体积膨胀 | 5 万文件预计 1.5 GB；可接受；如真有问题加截断 |
| Scanner 改动引入 bug 影响现有扫描功能 | 严格 TDD；不删除现场重算路径作为永久 fallback；feature flag `feature_precompute_enabled` 紧急可关 |
| BuildFamilies 改动 cache hit 路径有 bug 导致家族错乱 | pin determinism 集成测试守护；feature flag 紧急可退回纯现场重算 |

回滚策略：
- Feature flag `feature_precompute_enabled = false`：
  - 关闭后 scanner 不算 features（保留 hash 写入）
  - BuildFamilies 忽略 DB cache，全部走现场重算
  - 等价于改造前行为
- 新增列允许 NULL，关闭 flag 后老数据无害

## 执行顺序建议

1. Migration + DB schema（独立 PR，零风险）
2. Scanner 改动 + 单测（独立 PR，feature flag 默认 off）
3. BuildFamilies cache hit / miss 改造 + 单测（独立 PR，feature flag 默认 off）
4. LoadInputs lazy load 改造 + 单测
5. 集成测试 + pin determinism 验证
6. 性能基准测试（量化 cache hit 命中率与提速比）
7. 灰度开启 feature flag（先内部，确认无异常后打开）
