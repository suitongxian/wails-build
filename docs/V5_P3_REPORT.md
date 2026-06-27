# V5 Phase 3 完成报告

> AI 归目深化：§4.3-1 项目分组展示 + §4.3-5 属性抽取扩展

**完成日期**：2026-05-13
**分支**：`go-test-template`
**起点**：V5-P1.1 完成态（commit `0914345`）
**终点**：本期最终 commit
**Tag**：`v5-p3-ai-classify-deepening`

---

## 一、需求映射

| 编号 | §4 章节 | 要求 | 实现 |
|---|---|---|---|
| **B4** | §4.3-1 | "显示个人关联项目列表（正在进行中和最新项目优先），集中展示全部版本文件的完成情况、最新进展、待处理事项和异常提示" | 轻版：AIClassifyView 加 viewMode toggle (平铺 / 按项目分组)，分组视图按 TOP 建议项目聚合，按平均置信度排序，显示总数 + 平均置信度。**完整版（聚合统计/异常提示）**待后续 |
| **B5** | §4.3-5 | "从文件名、正文内容、元数据和上传上下文中抽取项目、工作环节、文件版本、责任人、日期、敏感等级和数据态等属性信息" | ✅ 元数据（mime/size/ext/timestamps）+ 路径上下文（parent_dir / sibling_count）+ 正文片段（首 200 字）全维度抽取 + 评分函数纳入新维度 |

## 二、6 个 Task 进度

| # | Task | Status | Commit |
|---|---|---|---|
| 1 | 抽出 textextract 包 | ✅ | `e10cba8` |
| 2 | metadata_enricher 注入元数据 | ✅ | `77c8d53` |
| 3 | rule_classifier 加 3 评分维度 | ✅ | `65ddc3c` |
| 4 | HTTP 端点接入 enricher + 正文片段 | ✅ | `4501156` |
| 5 | AIClassifyView 按项目分组视图 | ✅ | `2af6719` |
| 6 | 端到端验收 + 完成报告 + tag | ✅ | （本文件 + 后续 commit）|

## 三、评分维度对照

| # | 维度 | 加分 | V5-P3 前 | V5-P3 后 |
|---|---|---|---|---|
| 1 | 扩展名匹配 allowed_file_types | +0.35 | ✅ | ✅ |
| 2 | 文件名含 rule.file_name 关键词 | +0.30 | ✅ | ✅ |
| 3 | 文件名/路径含 stage_name 关键词 | +0.20 | ✅ | ✅ |
| 4 | 文件名/路径含 project_code/name | +0.20 | ✅ | ✅ |
| 5 | 路径精确含 project_code | +0.15 | ✅ | ✅ |
| 6 | summary/file_name 命中 naming_pattern | +0.10 | ✅ | ✅ |
| **7** | **MIME 精确匹配 allowed_file_types** | **+0.08** | ❌ | ✅ |
| **8** | **sibling_count ≥ 5 且规则 `*` 类型** | **+0.05** | ❌ | ✅ |
| **9** | **正文片段含 stage_name** | **+0.10** | ❌ | ✅ |
| **10** | **正文片段含 rule.file_name** | **+0.10** | ❌ | ✅ |

Cap 0.95 保持。

## 四、新增/修改文件清单

### Create
- `internal/textextract/textextract.go` — 文本提取包（从 similarity 搬出）
- `internal/textextract/textextract_test.go`
- `internal/ai/metadata_enricher.go` — EnrichInputForResource
- `internal/ai/metadata_enricher_test.go`
- `internal/ai/v5_p3_acceptance_test.go` — 端到端验收
- `docs/V5_P3_REPORT.md` — 本报告

### Modify
- `internal/similarity/similarity.go` — extractText 改 thin wrapper（-946 行）
- `internal/ai/rule_classifier.go` — 加 3 个评分维度（mime/sibling/body）
- `internal/ai/rule_classifier_test.go` — 3 个新测试
- `internal/httpd/ai_classify.go` — GetClassifySuggestions / ListPendingForClassify 接入 enricher + body
- `internal/httpd/ai_classify_test.go` — 2 个新测试
- `frontend_real/views/AIClassifyView.vue` — viewMode toggle + 分组视图

## 五、测试覆盖

- **新增**：
  - `TestExtractText_PlainText` / `_NonExistent` / `_UnknownBinary` / `TestExtractTextWithTimeout_Fast`
  - `TestEnrichInputForResource_BasicFields` / `_NoDistribution` / `_SiblingCount`
  - `TestRuleClassifier_MimeMatch` / `_SiblingCountMatch` / `_BodyKeywordMatch`
  - `TestHTTP_AIClassify_BodyEnrichment` / `_MetadataEnrichment`
  - `TestV5P3_EndToEnd`

- **整体状态**：`go test ./... -count=1` 全 PASS

## 六、已知限制 / 后续计划

1. **B4 §4.3-1 完整版未做**：分组视图当前是按 TOP 建议聚合，不含"完成情况/最新进展/异常提示"聚合统计。完整版需要后端新加聚合端点（per-project：fv 总数 / 已挂账数 / 待归目数 / 最近 7 天活动 / 异常项），UI 增加项目卡片元素。可在 V5 Phase 3.x 或 P2 期间补。
2. **mime 映射兜底**：MIME 命中通过 `mimeToExt` 映射 + 字符串包含双重判断，覆盖 docx/xlsx/pptx/pdf 等主流。罕见格式仍可能漏判。
3. **正文片段限制 200 rune**：足够命中"环节/规则名"关键词，不足以做 NLP-level 语义抽取（V5 后续若上模型可放宽）。

## 七、下一步

按依赖图：
- **P4**（manage 模版能力增强）— 跟 P3 独立，可并行或紧接着做
- **P2**（立项体验提升）— 依赖 P4 完成
