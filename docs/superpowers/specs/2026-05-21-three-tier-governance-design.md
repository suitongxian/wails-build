# Three-Tier Governance Design (核心 / 重要 / 一般)

## Background

刘老师 2026-05-21 在演示评审时定下了一条根本原则：**对个人文件三个级别的治理强度不能一视同仁**。

- 历史数据治理是「AI > 人工」，沙里淘金、宁可牺牲精度换治理率
- 新数据治理是「人工 > AI」，必须靠模板 / 工作项 / 版本目录 / 命名规范走严格准入
- 三个级别（核心 / 重要 / 一般）占比悬殊（约 1% / 中段 / 90%），处理方式必须分开

现实现状：
- AI 归目工具是一个单一列表入口，所有级别共用同一套 apply 流程
- `data_resources.importance_level` 与 `SYS-PERSONAL-CORE/IMPORTANT/GENERAL` 两套级别语义在 DB 里并存但不联动
- 个人台账三个 bucket 卡片对称、点击行为一样，看不到级别治理强度的差异
- 已有 `data_resource_family` / `family_id` 等相似性家族基础设施，但没有"权威源"概念，用户层不可见

这份设计把这套原则在 scan 终端里落地为：**6 个治理桶 + 3 条不同强度的人工/自动通道 + 1 个反映三级差异的台账主页**。

## Goals

- 把"AI 归目入口 / 数字机要专用通道 / 重要级权威源裁定 / 一般级一键 AI" 分别落到具体的 UI、API、数据模型上
- 用尽量少的新表 / 新列复用现有 `asset_ledgers` / `data_resource_family` / `audit_logs` / `lifecycle_event`
- 把 `data_resources.importance_level` 与 `project_code` 在 apply 时强同步，消除两套级别语义
- 演示阶段每条通道有可观察、可点的端到端动作，但**核心级仅实现"登记"一步，确认/回执/销账等 V2 再做**
- 不动后端 scanner / similarity 已有的核心引擎；本期只做"治理通道"上面那一层

## Non-Goals

- 不实现核心级的"确认 / 回执 / 销账"完整状态机；演示阶段只做"待登记 → 已登记"两态
- 不引入新的密码 / 签名体系；登记的二次签字复用当前登录密码的哈希比对
- 不实现重要级权威源的"批量裁定" UI，一族一族手动选
- 不动相似性家族（`data_resource_family`）的自动检测算法；只是给它加一列"权威源 id"
- 不处理隐私级（`importance_level=4`）资源；保持现状（不进 AI 归目流，旁路）
- manage 端不在本期改造；核心级回执 / 上报机制延到 V2

## Naming

| 中文 | 英文锚点 | 含义 |
|---|---|---|
| 核心级 | `SYS-PERSONAL-CORE` / `importance_level=1` | 必须人工登记的"数字机要"资料 |
| **数字机要** | `digital_memorandum` (字段前缀 `memorandum_*`) | 核心级资料的正式登记 |
| 重要级 | `SYS-PERSONAL-IMPORTANT` / `importance_level=2` | 半自动；多源时人工选权威源 |
| 一般级 | `SYS-PERSONAL-GENERAL` / `importance_level=3` | 一键 AI 自动归目 + 一键清空 |
| 权威源 | `data_resource_family.authoritative_resource_id` | 相似性家族里人工裁定的"主"成员 |
| 参考件 | 查询时由 `data_resources.family_id` 与 `family.authoritative_resource_id` 推导（**不**复用 `marking_method`，那是引用式/内嵌式/混合式的语义） | 非权威成员入账时的展示标记 |

注意："数字机要"中是「机」（机密 / 机要文件），不是「纪要」（meeting notes），全文 UI、菜单、变量内联翻译统一。

---

## 第一章 · 6 桶治理框架

### 1.1 框架

把 AI 归目工具的入口从"一个列表"改为"6 个治理桶"——`{普查 / 新增} × {核心 / 重要 / 一般}`。三级在每个 origin Tab 下用第二级标签呈现：

```
┌─ AI 归目工具 ─────────────────────────────────────────┐
│  普查数据 847   新增数据 12                            │
├────────────────────────────────────────────────────────┤
│  核心待登记 1 (red)   重要待裁定 5 (orange)   一般 6 (green)   │
├────────────────────────────────────────────────────────┤
│   核心：显示"已转入数字机要"占位，点跳菜单            │
│   重要：列表 + 权威源裁定徽标 + apply                  │
│   一般：阈值滑杆 + 一键 AI 归目 + 一键清空             │
└────────────────────────────────────────────────────────┘
```

### 1.2 级别语义统一

apply 端点新增强同步：

```sql
-- 在 BridgeResourceToTarget 成功后立即执行
UPDATE data_resources
   SET importance_level = ?,        -- 1 / 2 / 3 由目标 project_code 推出
       update_time = CURRENT_TIMESTAMP
 WHERE data_resources_id = ?;
```

映射表：

| project_code | importance_level |
|---|---|
| `SYS-PERSONAL-CORE` | 1 |
| `SYS-PERSONAL-IMPORTANT` | 2 |
| `SYS-PERSONAL-GENERAL` | 3 |
| 其它（业务项目挂账） | 不动 |

> **隐私级 `importance_level=4`** 在本期不进 AI 归目流；pending 查询也过滤掉。

### 1.3 桶分流如何计算

pending 列表生成时按 top1 推荐 project_code 把每条资源分入三个 sub-list：

```text
for each pending resource:
    sugs = AI.classify(resource)   # 仅 origin=new 时跑（origin=historical 跳过；详见 V1 历史/新数据 spec）
    top1 = sugs[0] if sugs else null
    bucket = bucket_of(top1.project_code)  # core / important / general / unknown
    sub_lists[bucket].append((resource, top1, sugs))
```

`origin=historical` 时不跑 AI，整个 batch 默认进 `general` 桶；用户在桶内点"展开 AI 推荐"才动 LLM。

用户也可手动调整某条到别的级别（"我觉得这是核心" → 转到核心通道）。手动调级走 `POST /resources/{id}/importance body {level: 1|2|3}` 端点，**两件事一起做**：

1. 把 `data_resources.importance_level` 改成目标值（脱离 pending 池——pending 过滤是 `importance_level=0`）
2. 触发与 apply 等价的"分流到对应通道"——核心 → 进数字机要待登记；重要 → 进重要级待裁定（若家族多源未确权也会被拦）；一般 → 留在一般级桶等下次一键

这样手动调级不是"暂存预判"，而是**已决断**：用户认为级别已经定了，从 pending 池移出，进入相应通道。如果之后想撤回，再调一次 `/resources/{id}/importance body {level: 0}` 重新回 pending。

---

## 第二章 · 核心级 · 数字机要（简化版）

### 2.1 设计意图

演示阶段只做两件事：
1. **核心级一律不被 AI 自动归目**——拦住等用户亲手登记
2. **登记必须用严肃动作**——填工作主题 / 密级 + 输登录密码做二次签字

完整 4 态机（登记/确认/回执/销账）延到 V2。

### 2.2 数据模型

不新建表。在 `asset_ledgers` 加 5 列：

```sql
ALTER TABLE asset_ledgers ADD COLUMN memorandum_topic TEXT;
ALTER TABLE asset_ledgers ADD COLUMN memorandum_classification TEXT;  -- 内部 / 秘密 / 机密
ALTER TABLE asset_ledgers ADD COLUMN memorandum_registered_at DATETIME;
ALTER TABLE asset_ledgers ADD COLUMN memorandum_registered_by INTEGER; -- users.id
ALTER TABLE asset_ledgers ADD COLUMN memorandum_signature_hash TEXT;
```

判定"已登记"：`memorandum_registered_at IS NOT NULL`。终态不可逆。

`memorandum_signature_hash` 计算公式：`SHA256(register_user_id || ":" || ISO_TIMESTAMP || ":" || users.password_md5)`。校验密码失败则 register 端点直接 401。

审计落 `audit_logs`，`action='core_memorandum_register'`，`target_type='asset_ledger'`，`target_id=ledger.id`。

### 2.3 状态机

```
        importance_level=1 (核心) + memorandum_registered_at IS NULL
                          │
                  ┌───────┴───────┐
                  ▼               ▼
            待登记 (列表)     [登记] ─填表+二次密码→ 已登记 (终态)
```

### 2.4 与 AI 归目的连接点

apply 端点流程改动：

```text
def apply(resource_id, project_id, stage_code, file_rule_code):
    project_code = lookup(project_id)
    if project_code == 'SYS-PERSONAL-IMPORTANT':
        if needs_authoritative_arbitration(resource_id):
            return 409 { family_id, members, error: "需要先选权威源" }
    ledger = BridgeResourceToTarget(...)
    sync_importance_level(resource_id, project_code)
    if project_code == 'SYS-PERSONAL-CORE':
        # ledger.memorandum_registered_at 留 NULL；
        # 提示前端"已转入数字机要待登记"，但 ledger 已实际建出
        return { ledger_id, hint: "transferred_to_memorandum_pending" }
    return { ledger_id }
```

AI 归目工具 pending 列表过滤 `importance_level != 1`（核心不在工具里展示，避免诱导）。

### 2.5 UI

**导航栏新增**：`数字机要`（紧跟 AI 归目工具下方，图标 `mdi-shield-lock`）。

页面两个 Tab：「待登记」/「已登记」。

待登记列表（每行）：

```
┌── 客户合同-2026Q1.pdf  importance=1 ──────────────────┐
│ 推荐主题：客户合同 · 推荐密级：秘密                    │
│ [登记]                                                  │
└─────────────────────────────────────────────────────────┘
```

「登记」弹窗字段：
- 资源名（只读，从 ledger.asset_name）
- 工作主题 ★（默认填 AI 推荐的工作事项）
- 密级 ★（下拉：内部 / 秘密 / 机密）
- 备注（可空）
- 登录密码 ★（用于二次签字）

保存：后端校密码 → 写 `memorandum_*` 5 列 + audit_log → 这条从待登记 Tab 消失，出现在已登记 Tab。

已登记 Tab 只读，展示登记快照（主题 / 密级 / 登记人 / 登记时间）。

### 2.6 API

| 端点 | 用途 |
|---|---|
| `GET /memorandum/pending?page=&page_size=` | 列待登记（`asset_ledgers join data_resources where importance_level=1 AND memorandum_registered_at IS NULL`），分页，返 `{items, total, page, page_size}` |
| `GET /memorandum/registered?page=&page_size=` | 列已登记，只读 |
| `POST /memorandum/register` body `{ledger_id, topic, classification, note, password}` | 校密码 → 写 5 列 → 落 audit |

manage 端本期不动。

---

## 第三章 · 重要级 · 权威源裁定

### 3.1 设计意图

刘老师"半自动"在重要级落地为：默认走 AI 推荐自动 apply，**只在检测到多源歧义时降级人工**。复用现有相似性家族基础设施（`data_resource_family`），只缺"权威源"这一格。

### 3.2 数据模型

**只加 1 列**：

```sql
ALTER TABLE data_resource_family ADD COLUMN authoritative_resource_id INTEGER;
CREATE INDEX idx_family_authoritative ON data_resource_family(authoritative_resource_id);
```

NULL = 该 family 还没人裁定权威源。

### 3.3 触发条件（apply 拦截）

apply 端在执行 BridgeResourceToTarget 前检查，三条**全部满足**才拦：

1. 目标 `project_code == 'SYS-PERSONAL-IMPORTANT'`
2. resource 有 `family_id IS NOT NULL` 且 family 的 `member_count >= 2`
3. family 的 `authoritative_resource_id IS NULL`

拦截响应：`409 { error: "需要先选权威源", data: { family_id, members: [{resource_id, name, path, size, update_time, relation}, ...] } }`

前端收到 409 + family 数据 → 弹"选权威源"对话框。

### 3.4 权威源已定的分支

| 当前 resource 在 family 的身份 | apply 行为 |
|---|---|
| `resource_id == authoritative_resource_id` | 正常 apply，ledger 落账 |
| 非权威成员 | apply 走通；查询台账时通过 join 推导出"参考件"标签（**不**写 `marking_method`，那是另一套语义） |

「权威 / 参考」纯查询时推导：

```sql
CASE
  WHEN dr.family_id IS NULL                            THEN 'standalone'
  WHEN dr.data_resources_id = f.authoritative_resource_id THEN 'authoritative'
  WHEN f.authoritative_resource_id IS NULL             THEN 'pending_arbitration'
  ELSE 'reference'
END AS family_role
```

用户可以**主动改判**：`POST /family/{id}/authoritative body {resource_id}` 改权威；改判后**无需修改任何 ledger 行**——下次查询自然得到新结果。

### 3.5 UI

入口在 AI 归目工具的"重要级"子桶（普查 / 新增 都适用）。列表行加多源徽标：

```
┌── 项目A-章程.docx  ⚠️ 多源(3) 待裁定 ──────────────────┐
│ 推荐：项目A / 资料 / 章程  [选权威源 & 应用]            │
└─────────────────────────────────────────────────────────┘
┌── 项目B-计划.docx  ✔ 权威 ─────────────────────────────┐
│ 推荐：项目B / 资料 / 计划  [应用]                       │
└─────────────────────────────────────────────────────────┘
┌── 项目C-报告.docx  · 参考（权威：~/Desk/...）─────────┐
│ 推荐：项目C / 资料 / 报告  [仍以参考件入账] [改判我为权威] │
└─────────────────────────────────────────────────────────┘
```

「选权威源」弹窗：family 全部成员 + RadioGroup 选权威 → 写 family.authoritative_resource_id → 弹窗关 → 继续 apply。

### 3.6 API

| 端点 | 用途 |
|---|---|
| `GET /family/{family_id}` | 返 family 主信息 + 成员列表（id / name / path / size / update_time / relation / is_authoritative） |
| `POST /family/{family_id}/authoritative` body `{resource_id}` | 设/改权威源 |
| `POST /ai/classify/apply` *(扩展)* | 命中 3.3 三条件 → 409 + family 数据 |

apply 端的拦截是 **fail-closed**——绕开前端直接调 apply，重要级 + 多源 + 未确权 → 一律 409。

---

## 第四章 · 一般级 · 一键 AI 归目

### 4.1 设计意图

一般级占 ~90%。主动作不是逐条选，而是**两键清场**：一键 AI 归目大头，一键把零碎扫掉。"AI > 人工"落地为：用户的默认体验 = 看一眼、按两次。

### 4.2 数据模型

**0 列改动**。完全复用现有：
- `importance_level=3` + `project_code='SYS-PERSONAL-GENERAL'`
- `ai_classify_rejected_at` + `ai_classify_reject_reason` 标已治理
- `BridgeResourceToTarget` 走 apply

### 4.3 UI

在 AI 归目工具的"一般级"子桶（普查 / 新增 都适用）：

```
┌─ AI 归目工具 · 普查数据 · 一般级 ─────────────────────────────┐
│ ⓘ 一般级走 AI 自动化。可调阈值后一键应用；余下可一键清空。    │
│                                                                 │
│ 自动阈值: 50% ──────●─────── 47 条可自动应用 · 15 条低于阈值    │
│                                                                 │
│ [一键 AI 归目 (47)]   [清空余下 (15)]                           │
│                                                                 │
│ 进度：30/47 ━━━━━━━━━━━━━━━━━━━━░░░░░░ 失败 1                  │
│                                                                 │
│ ┌── 报告-2026Q1.pdf  ◐ 72% ──→ 项目A/资料/报告 [应用][跳过] ─┐│
│ │ 备注-XX.docx       ◐ 38% ──→ 项目B/资料/备注 [应用][跳过] ││
│ └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

- 阈值滑杆默认 **0.5**（远低于现行 onAutoApply 的 0.8）
- 「清空余下」必弹二次确认 dialog："将把 15 条标为已治理，不再出现，请确认。"
- 一键 AI 归目过程显示实时进度："已处理 N/M · 失败 K"
- 每行只展示 top1（不展开多 candidates），保持轻量
- [应用] / [跳过] 给"偶尔想干预"的场景

### 4.4 API

**不新增**。前端循环：
- 一键 AI 归目 → 遍历 top1.confidence ≥ 阈值 的条目，逐条 `POST /ai/classify/apply`
- 清空余下 → 收余下 id，一次 `POST /ai/classify/bulk-dismiss`

### 4.5 bulk-dismiss 校验放宽

第 V1 章 spec 里 bulk-dismiss 只允许 `data_origin='historical'`。本章扩展：

**允许的组合**：
- `(data_origin='historical', any importance_level)` — 历史数据可全部跳过
- `(data_origin='new', importance_level=3)` — 新数据中的一般级可跳过（AI 没把握的零碎）

其他组合（如 `(new, important=2)`、`(new, core=1)`）仍 400 拒绝。

### 4.6 入口约束

「一键 AI 归目」按钮**只在一般级桶**展示。核心 / 重要桶里不出现这个按钮——避免诱导用户用一键动作处理本来应该人工严管的内容。

---

## 第五章 · 个人台账 · 三级分层视图

### 5.1 设计意图

台账主页让"三级各管各的"一眼可见：
- 顶部红条把"需要人工干预的项"做硬提示
- 三个 bucket 卡片底部按钮 / metric 按级别走不同语义
- 列表行加级别专属副状态
- 工作事项卡片加级别色点

### 5.2 数据模型

**0 列改动**。完全复用前 4 章已加列。

### 5.3 顶部"需人工干预"红条

```
┌─ ⚠ 需人工干预 ──────────────────────────────────────────────┐
│  ⏳ 数字机要待登记 3       ⚖ 重要级多源待裁定 2             │
│  [→ 数字机要]              [→ AI 归目工具 · 重要级]          │
└──────────────────────────────────────────────────────────────┘
```

两个数字都为 0 时整条隐藏。

数据来源：
- 待登记 = `SELECT COUNT(*) FROM asset_ledgers WHERE memorandum_registered_at IS NULL AND ... join data_resources WHERE importance_level=1`
- 多源待裁定 = `SELECT COUNT(DISTINCT f.family_id) FROM data_resource_family f JOIN data_resources dr ON dr.family_id = f.family_id WHERE f.authoritative_resource_id IS NULL AND f.member_count >= 2 AND dr.importance_level=2 AND dr.claim_status=2 AND dr.disable=0 AND dr.ai_classify_rejected_at IS NULL`

### 5.4 三个 bucket 卡片差异化

```
┌─ 核心级 ─────────┐  ┌─ 重要级 ─────────┐  ┌─ 一般级 ─────────┐
│ [12 条]          │  │ [86 条]          │  │ [742 条]         │
│                  │  │                  │  │                  │
│ 已机要登记 9     │  │ 权威 70 · 参考 14│  │ AI 自动 680      │
│ 待登记 3 ⚠       │  │ 多源待裁定 2 ⚠   │  │ 手动 35 · 待 27  │
│                  │  │                  │  │                  │
│ [→ 数字机要]     │  │ [查看重要级台账] │  │ [查看一般级台账] │
│                  │  │ [→ 多源裁定]     │  │ [→ AI 一键归目]  │
└──────────────────┘  └──────────────────┘  └──────────────────┘
```

核心 bucket **不**给"查看底账"按钮——强约束"核心不在台账里编辑，只在数字机要菜单里操作"。

### 5.5 台账列表新增"副状态"列

紧跟"级别"列右侧。按行 importance_level 渲染：

| 级别 | 副状态列 chip |
|---|---|
| 核心 (1) | `已机要(YYYY-MM-DD)` 绿 / `待登记` 红 |
| 重要 (2) | `权威` 蓝 / `参考(→主)` 灰 / `多源待裁定` 橙 |
| 一般 (3) | `AI 应用` / `手动` / `待治理` 弱灰 chip |

操作列按级别条件渲染：
- 核心：[查看机要详情] → 跳 `/memorandum/registered` filter
- 重要：[查看 family]（仅 family_id 存在时显示）
- 一般：[详情]（现有）

### 5.6 工作事项卡片

不改 30 天活动窗口逻辑。**只加**：卡片右上角一个小色点（红 / 黄 / 绿），按事项内最高级别取色。

### 5.7 跨页跳转

| 起点 | 终点 | URL 形态 |
|---|---|---|
| 台账 → 数字机要 | 数字机要待登记 | `/memorandum?state=pending` |
| 台账 → AI 归目（重要） | AI 归目工具 重要桶 | `/ai-classify?origin=historical&level=important` |
| AI 归目 apply 命中核心 | toast + 跳数字机要 | `/memorandum?state=pending` |
| 台账列表行 → family 详情 | family 详情 | `/family/:id` |

AI 归目工具支持 query 参数 `?origin=&level=&page=` 深链。

---

## Cross-cutting Concerns

### Importance-level 与 project_code 强同步

`/ai/classify/apply` 成功后强制 SQL：

```sql
UPDATE data_resources
   SET importance_level = ?,   -- 由 project_code 映射
       update_time = CURRENT_TIMESTAMP
 WHERE data_resources_id = ?;
```

映射不命中（即 project_code 不是 3 个 SYS-PERSONAL-* 之一）则不改 importance_level，保留原值。

### Pending 列表的 importance_level 过滤

`GET /ai/classify/pending` 在原过滤条件外加 `AND importance_level != 1 AND importance_level != 4`（核心走数字机要、隐私不进流）。

### 权威源已定但被改判的连锁更新

当 `POST /family/{id}/authoritative` 把权威源换到另一个 resource：
1. 改 `data_resource_family.authoritative_resource_id`
2. **不**触碰已落账 ledger 行——「权威 / 参考」是查询时推导，改一处即可
3. 落 audit_log `action='family_authoritative_change'`，记录 before/after `authoritative_resource_id`

### 一键 AI 归目过程中的错误

前端循环 apply 时每条独立处理，失败不阻塞其他。最终汇总 `{succeeded: N, failed: K, errors: [{resource_id, msg}, ...]}`。失败的条目保留在列表中等下次。

### 与现行 onAutoApply 的关系

现有 AIClassifyView 已有 `onAutoApply` 函数（默认阈值 0.8，全级别跑）。本期：
- 阈值默认值改 0.5
- 调用前先过滤 `top1.project_code === 'SYS-PERSONAL-GENERAL'`（其他级别即使 confidence 高也不参与"一键"）
- 按钮迁到一般级桶内显示，桶外不显示

## Migration / Rollout

### 一次性迁移

```sql
-- 第一章：importance_level ↔ project_code 强同步存量补齐
UPDATE data_resources SET importance_level = 1
 WHERE EXISTS (SELECT 1 FROM asset_ledgers al JOIN data_projects p ON al.project_code = p.project_code
               WHERE al.... AND p.project_code = 'SYS-PERSONAL-CORE')
   AND importance_level != 1;
-- 同理处理 IMPORTANT(2) / GENERAL(3)
```

实际实现时用 ledger 表反查更稳；细节由实施计划决定。

### 列添加（幂等）

通过现有 `runMigrations` 的 `columnAdds` 数组追加：
- `asset_ledgers.memorandum_topic / classification / registered_at / registered_by / signature_hash`
- `data_resource_family.authoritative_resource_id` + 索引

### 兼容性

- 已存在的 `SYS-PERSONAL-CORE` ledger 行在迁移后 `memorandum_registered_at IS NULL`，全部出现在"数字机要待登记"列表里——刘老师认可这种"立刻可见"的迁移效果
- 已存在的 family 不强制确权；只有当用户尝试 apply 重要级且家族 ≥2 成员时才会被拦
- 现有 AIClassifyView 单列表用户体验完全变样；要在版本发布说明里突出三级分流原则

## Testing Strategy

按项目铁律"每个任务都要有测试用例验证，只有用例通过才进入下一阶段开发"，每章对应：

### Backend (Go, `go test ./internal/...`)

| 文件 | 覆盖 |
|---|---|
| `repository/data_origin_*_test.go` | 第一章：apply 后 importance_level 同步、隐私级不进 pending |
| `repository/digital_memorandum_test.go` | 第二章：register 端点的密码校验 / 终态保护 / audit 落地 |
| `repository/family_authoritative_test.go` | 第三章：authoritative 设置 / 改判 / marking_method 自动联动 |
| `httpd/ai_classify_apply_intercept_test.go` | 第二/三章：apply 端的两类拦截（核心 hint / 重要 409） |
| `httpd/ai_classify_general_bulk_test.go` | 第四章：bulk-dismiss 放宽校验 + 进度统计 |

### Frontend (Vitest, `yarn test`)

| 文件 | 覆盖 |
|---|---|
| `__tests__/AIClassifyView.buckets.test.ts` | 6 桶结构渲染、Tab 切换深链 |
| `__tests__/MemorandumView.register.test.ts` | 数字机要登记表单 / 密码必填 / 已登记 Tab 切换 |
| `__tests__/PersonalFiles.tieredBuckets.test.ts` | 三个 bucket 卡片差异化按钮 / 顶部红条出现/隐藏 |
| `services/__tests__/familyAuthoritative.test.ts` | 选权威源后 apply 重试链路 |

### 前置

- Go：`go test ./internal/...`
- 前端：`yarn test`；如遇 better-sqlite3 native module 问题先 `npm rebuild better-sqlite3`

## Open Questions / Future

- **核心级完整四态机**（确认 / 回执 / 销账）—— V2，等业务跑通"登记"后再补
- **manage 端的"数字机要回执"通道** —— 需要管理后台同步设计，复用现有 cabinet_sync_*
- **批量权威源裁定 UI** —— 暂不做，演示阶段一族族手动
- **一般级 AI 学习反馈** —— 用户调一条手动后能不能强化 AI 推荐？属于 AI 层增量训练话题，本期不涉及
- **跨终端的权威源协调** —— 同一 family 在两个终端各自指定了不同权威，sync 到 manage 时怎么办？本期不处理（scan 现状本来就是单端落本地）
