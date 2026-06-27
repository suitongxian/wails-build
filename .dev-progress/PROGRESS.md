# 相似度家族集成（experiment → data-asset-scan）开发进度

> 最后更新：2026-04-30
> 维护方式：随开发推进增量更新，已完成阶段归档到"已完成"段

## 项目目标

把 `/root/data/experiment/experiment` 中验证过的"文件相似度家族"算法集成进 data-asset-scan 的"扫描结果责任认领"页面。当前 ClaimView 仅按 partial-MD5（`content_sign`）归并文件，二进制不完全相同就拆成不同资源；目标是引入"家族（family）"作为更高一层归并：哈希组属于家族，认领按家族折叠展示，副本对话框升级为"家族成员"分组视图（same_content / process_version / derived），批量认领自动联动家族所有成员。

## 当前状态

- **进行中：** 全部阶段完成 ✅
- **下一步：** 用户在真实 wails 应用里手动跑一遍：扫描入库 → 点"相似度分析" → 看认领页面是否按家族折叠
- **阻塞项：** 无（content_text_cache 缓存表已建，但实际接入 experiment 的 extractText 流水线推迟到性能瓶颈出现时再做）

## 里程碑

### 阶段一：后端骨架 — internal/similarity 包  `✅ 完成`
- [x] 创建 `internal/similarity/` 目录（2026-04-30）
- [x] 把 experiment/main.go 拷入并改造为 package similarity，去 main/flag/signal、去 xlsx/json/md 输出（2026-04-30，从 3260 行截到 2860 行）
- [x] 暴露公共入口 `BuildFamilies(ctx, inputs []FileInput, cfg *Config) ([]Family, error)`，新增 api.go（2026-04-30）
- [x] 合并 go.mod 依赖（mimetype 升级 1.4.3→1.4.13；新增 docconv/goimagehash/imaging/pdf/mscfb；golang.org/x/text 0.22→0.36）（2026-04-30）
- [x] 编写包级单元测试 api_test.go：完全重复/不相关/高相似文本三类用例全部通过（2026-04-30）

### 阶段二：数据层 + 任务执行  `🟡 基本完成`
- [x] migration：新增 `data_resource_family` 表，给 `data_resources` 加 `family_id / family_relation / family_score` 三列（2026-04-30，幂等迁移 + 三组测试覆盖：fresh/二次运行/老库）
- [x] migration：新增 `content_text_cache` 表（已建表，缓存注入到流水线见下方"未做"）
- [x] `repository/family.go`：FamilyRow / FamilyMemberDetail / Reset / InsertFamilyWithMembers / GetFamilyByID / ListFamilyMembers / IDsInFamily（2026-04-30）
- [x] 任务执行模块 `internal/similarity/analyzer.go`：抽 Loader/Persister 接口；提供 DBLoader（读 data_distributing.GetActive）+ DBPersister；含两组 mock 测试覆盖 reset 调用、单成员家族跳过、跨哈希成员归并（2026-04-30）
- [x] 阈值参数从 `system_config` 读取：`config_loader.go`，6 个 key + 容错（2026-04-30）
- [ ] **未做**：把 `content_text_cache` 实际接入 experiment 流水线的 `extractText` / `extractFeature`（需改 similarity.go 内部调用点，规模较大，下一回合做）
- [ ] **未做**：含 docx/pdf 样本的端到端用例（构造 docx 文件依赖较重，可在阶段五用真实样本目录验证）

### 阶段三：HTTP API 适配  `✅ 完成`
- [x] `similarity_task` 表迁移 + `repository/similarity_task.go`（Create/MarkRunning/UpdatePhase/MarkSucceeded/MarkFailed/Latest/HasRunning）
- [x] `httpd/similarity.go`：POST /similarity/analyze（异步 goroutine + 进程级互斥 + DB 级 HasRunning 检查）；GET /similarity/task/:id；GET /similarity/task/latest
- [x] `httpd/family.go`：GET /family/:id；GET /family/:id/members（按 relation 分组返回 + 主资源详情）
- [x] `GET /resources?groupByFamily=true` 默认开启，folded view（含 family_id IS NULL 兜底）
- [x] `BatchClaim` 在 handler 层 `expandFamilyIDs` 自动扩选家族成员（含 dedupe）
- [x] FamilyRepository 测试：Insert/IDsInFamily/Reset 三组用例通过

### 阶段四：前端  `✅ 完成`
- [x] api.ts 新增 SimilarityTask / FamilyMembersResponse / FamilyMemberDetail 类型 + 4 个方法（startSimilarityAnalysis、getSimilarityTask、getLatestSimilarityTask、getFamilyMembers）
- [x] DataResource / SystemConfig 类型补 family_* 与 similarity_* 字段
- [x] `ClaimView.vue`：顶部"相似度分析"按钮 + 任务进度文字（pending/running/succeed/failed 四态着色）；onMounted 时拉 latest task 续接进行中任务；onUnmounted 清理 timer
- [x] `ClaimView.vue` 副本对话框升级：当 item.family_id 存在时拉 /family/:id/members 并按 primary/same_content/process_version/derived 四段渲染（含 score 显示）；否则回退老的 getCopies 视图
- [x] `SystemConfigView.vue` 加 6 个阈值输入框
- [x] 后端 /config 接口同步暴露/接收 6 个 similarity_* 字段
- [x] 整个前端 `vue-tsc --noEmit` 通过

### 阶段五：联调与验证  `✅ 完成（后端层面）`
- [x] `internal/similarity/e2e_test.go`：开真 sqlite + 写真实文件 + 走完 InsertDD→InsertDR→AnalyzeFromDB→IDsInFamily 全链路；验证两个相似 txt 进入同一家族、不相关文件不入家族、有且仅有 1 个 primary
- [x] 全项目 `go build ./... && go test ./...` 全部通过；前端 `vue-tsc --noEmit` 通过
- [ ] 真实 wails 应用层面交互测试 留给用户：起 wails dev → 扫一个目录 → 点"相似度分析" → 在认领页看家族折叠 → 点击数量看家族成员对话框 → 批量认领联动

## 关键决策

- **2026-04-30** `集成形态`：把 experiment 作为内部 Go 包（`internal/similarity/`）而非外挂进程。原因：避免再启子进程、依赖外部二进制；输入直接从 SQLite 读，不重复扫盘。
- **2026-04-30** `数据模型`：新增 `data_resource_family` 表 + `data_resources` 加 `family_id/relation/score` 列，而不是改造 `data_resources` 主键。原因：不破坏现有"哈希组"语义，家族是其上一层归并，前端可切换两种视图。
- **2026-04-30** `触发时机`：用户在认领页点"相似度分析"按钮触发后台任务。原因：跨格式内容哈希要对所有文档做全文提取，分钟级耗时，不能塞进同步扫描流程拖死进度条。
- **2026-04-30** `跨格式内容哈希保留`：保留 experiment 的 Step 1.5（文本归一化 + SHA256），通过 `content_text_cache` 表缓存全文提取结果以加速增量。原因：这是用户场景里"同合同 docx + pdf"的核心识别路径，不能砍。
- **2026-04-30** `阈值可调`：0.95/0.75/0.50 写入 `system_config` 而非硬编码。原因：用户后续可能根据样本集微调，避免每次都改代码重打包。
- **2026-04-30** `批量认领联动`：勾家族主资源 → 自动扩选所有成员一起 claim。原因：家族折叠后用户只看到主资源一行，必须保证认领语义覆盖所有成员，否则会出现"折叠后认领不全"的逻辑漏洞。

## 已完成

### 阶段一：后端骨架 — internal/similarity 包（2026-04-30）
- experiment/main.go 整体迁入 `internal/similarity/similarity.go`，截掉 main + 报告写入器（excelize 依赖随之去掉），保留扫描/候选/特征/相似度/家族/分类全部算法
- 新建 `internal/similarity/api.go`：定义对外 `FileInput / Family / FamilyMember`，`BuildFamilies(ctx, inputs, cfg)` 把 DB 数据→内部 *FileRecord→跑流水线→映射出 Family
- 三个单元测试全部通过：完全重复→same_content；不相关→不归并；文本高相似→进入同家族并落档 process_version/derived
- 整个项目 `go build ./...` 通过；mimetype 升级未影响 scanner 包

## 变更日志

### 2026-04-30
- 完成两个项目逻辑梳理：data-asset-scan 用 partial-MD5 归并、experiment 用多算法家族聚合
- 与用户确认六个集成方案问题（聚合粒度/触发/阈值/批量联动/打包体积/跨格式保留）
- 初始化 `.dev-progress/PROGRESS.md`
- 阶段一完成：experiment 代码作为 internal/similarity 包入项目，公开 `BuildFamilies` API，3 个测试用例通过
- 阶段二主体完成：迁移幂等加表/列（3 测试通过）；FamilyRepository 落 CRUD；Analyzer 通过 Loader/Persister 接口解耦 DB 与算法（2 mock 测试通过）；system_config 阈值加载器到位
- 阶段三完成：similarity_task 表 + repository + 异步 HTTP handler（POST /similarity/analyze 等）；Family handler；/resources 加 groupByFamily；BatchClaim handler 内做家族扩选（FamilyRepository 用例 2 组通过）
- 阶段四完成：api.ts 新增 4 方法 + 多个类型；ClaimView 顶部"相似度分析"按钮 + 进度轮询 + 副本对话框升级为家族分组视图；SystemConfigView 加 6 个阈值输入；/config 接口同步收发 similarity_* 字段
- 阶段五（后端）完成：e2e_test 开真 sqlite + 真实文件走完整链路验证家族归并与 primary 唯一性
- 全项目 `go build ./...` 与 `go test ./...` 均通过；前端 `vue-tsc --noEmit` 通过

## 待办 / 待讨论

- mimetype 版本对齐：experiment 用 1.4.13，data-asset-scan 用 1.4.3。直接升到 1.4.13 应当向后兼容，需在阶段一合并依赖时验证 scanner 包不受影响。
- 增量更新策略 v2：当前 v1 全量重算。后续可加"仅对新/变更 content_sign 重算所属家族"，但需要谨慎处理家族边界变化（一个新文件可能合并两个旧家族）。
