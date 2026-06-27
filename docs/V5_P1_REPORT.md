# V5 Phase 1 完成报告

> Quick Wins (A1-A4) — 消除 §4 主要产品与功能需求要点审计的 4 项小颗粒度缺口

**完成日期**：2026-05-13
**分支**：`go-test-template`
**起点 SHA**：`df80a33` (V5 计划文档)
**终点 SHA**：`24a42a8` (端到端验收测试)
**提交数**：16
**Tag**：`v5-p1-quick-wins`

---

## 一、需求映射

| 编号 | §4 章节 | 要求 | 实现状态 |
|---|---|---|---|
| A1 | §4.2-6 | 个人文件 2 工作环节（起草修改 + 定稿）| ✅ TPL-PERSONAL-FILES V2.0 |
| A2 | §4.3-2 | AI 归目"确认/调整/驳回/批量" | ✅ 全四项可用 |
| A3 | §4.3-4 | 手动归目"手动关联 + 解除绑定 + 重新归类 + 原因登记" | ✅ Unbind/Reclassify + Reason |
| A4 | §4.3-6 | 家族归档"过程文件和最新文件分别归入" | ✅ BridgeFamilyToProjectSplit |

---

## 二、12 个 Task 进度

| # | Task | Status | Commit |
|---|---|---|---|
| 1 | Manage 端 TPL-PERSONAL-FILES V2.0 双环节 | ✅ | `5d14ba9` (manage repo) |
| 2 | Scan ensurePersonalFilesContext 适配 V2.0 | ✅ | `bd089f4` |
| 3 | BridgeClassifyToPersonalProject 区分过程/定稿 | ✅ | `4d13333` |
| 4 | AI 归目"驳回"端点 | ✅ | `83ead4f` + `d1b01ac` (audit target fix) |
| 5 | AIClassifyView UI 加驳回+调整 | ✅ | `eb9fabf` + `fe5aa42` + `84a45dc` (polish) |
| 6 | 解绑+重新归类业务逻辑 (reclassify.go) | ✅ | `36b7390` + `ef12db2` (polish) |
| 7 | 解绑+重新归类 HTTP 端点 | ✅ | `8f70d43` + `c5b6b98` (perm middleware) + `d4d527d` (personal proj auth) |
| 8 | LedgerView 加解绑+重新归类入口 | ✅ | `5f62597` |
| 9 | 家族归档过程/定稿分流（后端）| ✅ | `bf9217c` |
| 10 | ClassifyView 家族对话框双目标 UI | ✅ | `de471ee` |
| 11 | V5-P1 端到端验收测试 | ✅ | `24a42a8` |
| 12 | V5-P1 完成报告 + tag | ✅ | （本文件）|

---

## 三、关键产物

### 3.1 数据库 schema 变化

**新加列**（`data_resources`）：
- `ai_classify_rejected_at DATETIME NULL`
- `ai_classify_reject_reason TEXT NULL`

**新加列**（`file_versions`）：
- `unbind_time DATETIME NULL`
- `unbind_reason TEXT NULL`
- `reclassified_from_fv_id INTEGER NULL`

**新加表**：
- `reclassify_history (id, original_fv_id, new_fv_id, action, reason, operator_user_id, operator_name, create_time)` + 索引 `idx_reclassify_history_orig(original_fv_id)`

**模版变化**（manage 端）：
- TPL-PERSONAL-FILES V1.0 → status='deprecated'（保留向后兼容）
- TPL-PERSONAL-FILES V2.0 → 2 环节 `GR-DRAFT`（PRC-001 process）+ `GR-FINAL`（OUT-001 output）

### 3.2 新增 audit/lifecycle 常量

```go
// audit_log.go
AuditAIClassifyApply    = "ai_classify_apply"  // 替代旧 AuditFileUpload 复用
AuditAIClassifyReject   = "ai_classify_reject"
AuditFvUnbind           = "fv_unbind"
AuditFvReclassify       = "fv_reclassify"
AuditTargetDataResource = "data_resource"     // 新 target 类型

// lifecycle_event.go
EventUnbind     = "unbind"
EventReclassify = "reclassify"
```

### 3.3 新增 HTTP 端点

| 端点 | 用途 | 中间件 |
|---|---|---|
| `POST /ai/classify/reject` | AI 归目驳回 | 无 |
| `GET /projects/:id/stages-with-rules` | 获取项目环节+规则三级结构 | 无 |
| `POST /file-versions/:id/unbind` | 解除绑定 fv | `RequireFileVersionProjectAction("write")` |
| `POST /file-versions/:id/reclassify` | 重新归类 fv | `RequireFileVersionProjectAction("write")` |
| `POST /resources/families/:id/batch-archive` | 加 `final_stage_code`/`final_file_rule_code` 可选参数 | 既有 |

### 3.4 新增 repository 函数

```go
// personal_files_init.go
ensurePersonalFilesContext  // 改为优先 V2.0，回退 V1.0

// personal_files_bridge.go
resolvePersonalStage(db, projectID, dataStateHint)           // V2.0/V1.0 stage 选择
BridgeClassifyToPersonalProjectWithState(db, resID, hint)    // hint=process/output
BridgeClassifyToPersonalProject(db, resID)                   // wrapper, default = output
BridgeFamilyToProjectSplit(db, famID, projID, p_stage, p_rule, f_stage, f_rule)

// reclassify.go (新文件)
type UserSnapshot
type ReclassifyInput
UnbindFileVersion(db, fvID, reason, operator)
ReclassifyFileVersion(db, input)
jsonExtractInt(s, key)  // private helper for source_ref 解析

// project_auth.go
CheckProjectAction(...)             // 加 SYS-PERSONAL-* 放行
CheckProjectActionByUserID(...)     // 加 SYS-PERSONAL-* 放行
```

### 3.5 前端改造

- `AIClassifyView.vue`：驳回 + 调整对话框 + 3 级下拉
- `LedgerView.vue`：解绑 + 重新归类入口，按 lifecycle 显隐
- `ClassifyView.vue`：家族归档对话框加"定稿目标"可选段，自动分流

---

## 四、测试覆盖

### 4.1 单元/集成测试

- `TestEnsurePersonalFilesContext_V2_TwoStages`
- `TestBridge_ToFinalStage_Default` / `_ToDraftStage_WithHint` / `_LegacyV1_StillWorks`
- `TestHTTP_AIClassify_Reject` / `_MissingReason` / `_InvalidResourceID`
- `TestHTTP_Projects_StagesWithRules`
- `TestUnbindFileVersion` / `_EmptyReason` / `_AlreadyCancelled`
- `TestReclassifyFileVersion` / `_EmptyReason` / `_MissingResourceRef` / `_InvalidTarget`
- `TestJsonExtractInt`（7 子用例）
- `TestHTTP_FileVersion_Unbind` / `_MissingReason` / `_InvalidID` / `_Reclassify` / `_MissingFields`
- `TestCheckProjectAction_PersonalProject_AllowsActiveUser` / `_DenyUnknownUser` / `_RegularProject_StillEnforcesMembership`
- `TestBridgeFamily_SplitProcessAndFinal` / `_SplitIdempotent` / `_FamilyMissing`
- `TestHTTP_FamilyBatchArchive_SplitMode` / `_LegacySingleMode` / `_SplitInvalidFinalStage`
- **`TestV5P1_EndToEnd` —— 7 步骤回归基线**

### 4.2 整体状态

```
go test ./... -count=1  →  PASS 全包
go vet ./...            →  clean
```

---

## 五、后续 V5 Phase 2/3/4 计划

详见 `docs/V5_PLAN.md`。

| Phase | 内容 | 预计工作量 |
|---|---|---|
| P2 立项体验 | 项目认领 + 归类选型 + 样板层 | 3-4 天 |
| P3 AI 归目深化 | 按项目分组展示 + 属性抽取（正文/元数据）| 2-3 天 |
| P4 模版能力增强 | 模版九宫格 + 角色规范 + 评审 + 三级派生 | 3-4 天 |

> 建议顺序：P1 (本期) → P4 → P2 → P3。

---

## 六、已知偏差与待跟进

1. **personal_files_bridge.go 顶部 doc 注释**：仍提到 V1.0 单环节 `GR-DA` 格式——已被 Task 3 重构覆盖，下一次接触此文件时同步刷新。Code reviewer 标记为 "Minor — defer to follow-up"。
2. **family split 响应字段语义**：`StageCode = "split:GR-DRAFT→GR-FINAL"` 是合成字符串，未来可拆为独立的 ProcessStage / FinalStage 字段。
3. **`models.FileVersion` 因 SELECT \* 模式新增了 3 个字段**：与既有 `SubmittedBy`/`CreatedByUserID` 模式一致，但长期看应改为 explicit column list。
4. **个人项目权限**：`CheckProjectAction` 现在对 SYS-PERSONAL-* 项目放行任何 active user。此模型假设"单终端单用户"——若未来支持多用户共享终端，需重新设计。

---

## 七、Phase 1 完成判据 ✅

- [x] manage 端：TPL-PERSONAL-FILES V2.0 已 seed，V1.0 deprecated
- [x] scan 端：3 个 SYS-PERSONAL-* 项目每个有 2 个 stage（GR-DRAFT + GR-FINAL）
- [x] scan 端：归类保护页归档默认归 GR-FINAL
- [x] scan 端：AI 归目页可驳回（按钮 + 对话框 + 不再出现在 pending）
- [x] scan 端：AI 归目页可调整目标（三级下拉 → apply）
- [x] scan 端：底账可解绑（按钮 + 对话框 + lifecycle=cancelled）
- [x] scan 端：底账可重新归类（生成新 fv + 原 fv cancelled + reclassified_from_fv_id 串联）
- [x] scan 端：家族归档双目标 UI（过程目标 + 定稿目标）+ 后端自动分流
- [x] go test ./... 全部 PASS
- [x] manage 端 vitest 全部 PASS
