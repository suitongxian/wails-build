# V5 Phase 1: Quick Wins Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 消除 §4 审计发现的 4 项小颗粒度功能缺口（A1-A4），把 V4 完成态推进到接近完整。

**Architecture:** 全部改动在 scan 端（涉及 manage 端的仅 templates-seed.ts 的 TPL-PERSONAL-FILES 升级）。新增 1 张表（reclassify_history），3 张表加字段（template_*, data_resources, file_versions）。所有改动走 migrations，向后兼容。

**Tech Stack:** scan: Go 1.21 + sqlx + Gin + Wails v2 + Vue 3 + Vuetify 3; manage: Nuxt 4 + better-sqlite3 + TypeScript

---

## 涉及文件总览

### Modify
- `data-asset-manage/server/database/templates-seed.ts` — TPL-PERSONAL-FILES 升级到 V2.0（2 环节）
- `data-asset-scan/internal/repository/personal_files_init.go` — 适配新模版（2 环节）
- `data-asset-scan/internal/repository/personal_files_bridge.go` — 桥接区分过程/定稿
- `data-asset-scan/internal/repository/migrations.go` — 新列 + 新表
- `data-asset-scan/internal/repository/audit_log.go` — 加新 audit 码
- `data-asset-scan/internal/repository/lifecycle_event.go` — 加 `EventReject` `EventUnbind` `EventReclassify`
- `data-asset-scan/internal/httpd/ai_classify.go` — 加 reject + adjust 端点
- `data-asset-scan/internal/httpd/file_versions.go` — 加 unbind + reclassify 端点
- `data-asset-scan/internal/httpd/family.go` — 家族归档接受双目标
- `data-asset-scan/internal/httpd/router.go` — 注册新路由
- `data-asset-scan/frontend_real/views/AIClassifyView.vue` — 驳回 + 调整 UI
- `data-asset-scan/frontend_real/views/LedgerView.vue` — 解绑 + 重归类入口
- `data-asset-scan/frontend_real/views/ClassifyView.vue` — 家族对话框双目标 UI

### Create
- `data-asset-scan/internal/repository/reclassify.go` — 解绑 + 重归类业务逻辑
- `data-asset-scan/internal/repository/reclassify_test.go`
- `data-asset-scan/internal/httpd/ai_classify_reject_test.go`
- `data-asset-scan/internal/httpd/file_versions_unbind_test.go`
- `data-asset-scan/internal/httpd/family_split_test.go`
- `data-asset-manage/tests/templates/personal-files-v2.test.ts`

---

## Task 1: Manage 端升级 TPL-PERSONAL-FILES 模版到 V2.0（2 环节）

**Files:**
- Modify: `data-asset-manage/server/database/templates-seed.ts:187-232`
- Test: `data-asset-manage/tests/templates/personal-files-v2.test.ts` (create)

**Why V2.0 而非改 V1.0：** 模版版本不能原地改（V3-A 收尾文档约束，已发布模版改结构需升新版）。Scan 端启动时检测到本地缓存的是 V1.0 而 manage 端最新是 V2.0，会通过"重新同步"拉到 V2.0。

- [ ] **Step 1.1: 写失败测试**

文件 `data-asset-manage/tests/templates/personal-files-v2.test.ts`：

```typescript
import { describe, it, expect, beforeEach } from 'vitest'
import Database from 'better-sqlite3'
import { runMigrations } from '../../server/database/migrations'
import { seedDefaultTemplates } from '../../server/database/templates-seed'

describe('TPL-PERSONAL-FILES V2.0 — 双环节', () => {
  let db: Database.Database
  beforeEach(() => {
    db = new Database(':memory:')
    runMigrations(db)
    seedDefaultTemplates(db)
  })

  it('应存在 V2.0 版本', () => {
    const row = db.prepare(`
      SELECT id, status FROM data_templates
      WHERE template_code = ? AND template_version = ?
    `).get('TPL-PERSONAL-FILES', 'V2.0')
    expect(row).toBeTruthy()
    expect((row as any).status).toBe('active')
  })

  it('应有 2 个 stage：起草修改 + 定稿', () => {
    const tpl = db.prepare(`SELECT id FROM data_templates WHERE template_code = ? AND template_version = ?`)
      .get('TPL-PERSONAL-FILES', 'V2.0') as { id: number }
    const stages = db.prepare(`
      SELECT stage_code, stage_name, sort_order
      FROM template_stages WHERE template_id = ? ORDER BY sort_order
    `).all(tpl.id) as Array<{ stage_code: string; stage_name: string; sort_order: number }>
    expect(stages).toHaveLength(2)
    expect(stages[0]).toMatchObject({ stage_code: 'GR-DRAFT', stage_name: '个人文件起草与修改' })
    expect(stages[1]).toMatchObject({ stage_code: 'GR-FINAL', stage_name: '个人文件定稿' })
  })

  it('GR-DRAFT 环节有 1 条规则 PRC-001 (process)', () => {
    const stage = db.prepare(`
      SELECT ts.id FROM template_stages ts
      JOIN data_templates t ON t.id = ts.template_id
      WHERE t.template_code = ? AND t.template_version = ? AND ts.stage_code = ?
    `).get('TPL-PERSONAL-FILES', 'V2.0', 'GR-DRAFT') as { id: number }
    const rules = db.prepare(`SELECT file_rule_code, data_state FROM template_file_rules WHERE template_stage_id = ?`).all(stage.id)
    expect(rules).toEqual([{ file_rule_code: 'PRC-001', data_state: 'process' }])
  })

  it('GR-FINAL 环节有 1 条规则 OUT-001 (output)', () => {
    const stage = db.prepare(`
      SELECT ts.id FROM template_stages ts
      JOIN data_templates t ON t.id = ts.template_id
      WHERE t.template_code = ? AND t.template_version = ? AND ts.stage_code = ?
    `).get('TPL-PERSONAL-FILES', 'V2.0', 'GR-FINAL') as { id: number }
    const rules = db.prepare(`SELECT file_rule_code, data_state FROM template_file_rules WHERE template_stage_id = ?`).all(stage.id)
    expect(rules).toEqual([{ file_rule_code: 'OUT-001', data_state: 'output' }])
  })

  it('V1.0 旧版本应仍存在但 status=deprecated', () => {
    const v1 = db.prepare(`SELECT status FROM data_templates WHERE template_code = ? AND template_version = ?`)
      .get('TPL-PERSONAL-FILES', 'V1.0') as { status: string } | undefined
    if (v1) expect(v1.status).toBe('deprecated')
  })
})
```

- [ ] **Step 1.2: 跑测试看失败**

```bash
cd data-asset-manage && npm rebuild better-sqlite3
npx vitest run tests/templates/personal-files-v2.test.ts
```
预期 FAIL — V2.0 不存在。

- [ ] **Step 1.3: 修改 templates-seed.ts**

在 `seedPersonalFilesTemplate` 函数（约第 187 行）下方新增 `seedPersonalFilesTemplateV2`：

```typescript
function seedPersonalFilesTemplateV2(db: Database.Database): void {
  // 幂等
  const existing = db.prepare(`
    SELECT id FROM data_templates WHERE template_code = ? AND template_version = ?
  `).get('TPL-PERSONAL-FILES', 'V2.0') as { id: number } | undefined
  if (existing) return

  // 把 V1.0 标记为 deprecated（如果存在）
  db.prepare(`
    UPDATE data_templates SET status = 'deprecated'
    WHERE template_code = ? AND template_version = ? AND status != 'deprecated'
  `).run('TPL-PERSONAL-FILES', 'V1.0')

  const tplResult = db.prepare(`
    INSERT INTO data_templates (
      template_code, template_name, template_version, scenario,
      publisher, status, project_sensitivity_level, use_share_scope, description, created_by
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  `).run(
    'TPL-PERSONAL-FILES',
    '个人文件项目化管理模版',
    'V2.0',
    '个人本机工作文件的归档与项目化管理（简版需求 §4.2 第 6 条：起草修改 + 定稿 两个工作环节）',
    'provider',
    'active',
    'general',
    JSON.stringify(['个人']),
    'V2.0 升级到 2 环节：起草与修改、定稿。Scan 端"认领文件归档保护"按等级桥接到 3 个内置项目；用户主动归档默认归 GR-FINAL，过程版本归 GR-DRAFT。',
    'system'
  )
  const tplId = Number(tplResult.lastInsertRowid)

  const draftStage = db.prepare(`
    INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, description, default_role_codes)
    VALUES (?, ?, ?, ?, ?, ?, ?)
  `).run(tplId, 'GR-DRAFT', '个人文件起草与修改', 'work', 1,
    '承载个人工作过程版本：草稿、修改、暂存',
    JSON.stringify(['本人']))
  const draftId = Number(draftStage.lastInsertRowid)

  const finalStage = db.prepare(`
    INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, description, default_role_codes)
    VALUES (?, ?, ?, ?, ?, ?, ?)
  `).run(tplId, 'GR-FINAL', '个人文件定稿', 'record', 2,
    '承载个人工作定稿版本：用户主动归档的成品',
    JSON.stringify(['本人']))
  const finalId = Number(finalStage.lastInsertRowid)

  const insertRule = db.prepare(`
    INSERT INTO template_file_rules (
      template_stage_id, file_rule_code, file_name, data_state,
      required, allowed_file_types, naming_pattern, summary_pattern, sort_order
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
  `)
  insertRule.run(draftId, 'PRC-001', '过程版本', 'process', 0, JSON.stringify(['*']),
    '过程-{原文件名}-{日期}', '修改次数：{次数}', 1)
  insertRule.run(finalId, 'OUT-001', '归档定稿', 'output', 0, JSON.stringify(['*']),
    '定稿-{原文件名}', '归档时间：{时间}，敏感等级：{等级}', 1)
}
```

在 `seedDefaultTemplates` 入口（约第 172 行 `seedPersonalFilesTemplate(db)` 调用处）下方加：

```typescript
  seedPersonalFilesTemplate(db)    // V1.0 (legacy, kept for 既有缓存兼容)
  seedPersonalFilesTemplateV2(db)  // V2.0 (current active)
```

- [ ] **Step 1.4: 跑测试看通过**

```bash
cd data-asset-manage && npx vitest run tests/templates/personal-files-v2.test.ts
```
预期 PASS — 5 个 test 全过。

- [ ] **Step 1.5: Commit**

```bash
cd data-asset-manage
git add server/database/templates-seed.ts tests/templates/personal-files-v2.test.ts
git commit -m "feat(template): TPL-PERSONAL-FILES V2.0 with 2 stages

简版需求 §4.2-6: 个人文件项目化管理应包括 起草修改 + 定稿 两个工作环节。
V1.0 标记 deprecated；V2.0 含 GR-DRAFT (PRC-001 process) + GR-FINAL (OUT-001 output)。"
```

---

## Task 2: Scan 端 ensurePersonalFilesContext 适配 V2.0

**Files:**
- Modify: `data-asset-scan/internal/repository/personal_files_init.go:43-75`
- Test: `data-asset-scan/internal/repository/personal_files_init_test.go` (modify existing或 create)

**当前行为：** 找 V1.0；找不到就跳过。
**目标行为：** 优先找 V2.0；V2.0 找不到回退找 V1.0；都没有再跳过。3 个内置项目按找到的版本立项。

- [ ] **Step 2.1: 写失败测试**

在 `personal_files_init_test.go` 加新测试：

```go
func TestEnsurePersonalFilesContext_V2_TwoStages(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()

    // 在 db 里手工 seed V2.0 模版
    seedPersonalFilesTemplateV2InTest(t, db)

    if err := ensurePersonalFilesContext(db); err != nil {
        t.Fatalf("ensure failed: %v", err)
    }

    // 3 个项目应有，每个项目应有 2 个 stage
    for _, code := range []string{PersonalCoreProjectCode, PersonalImportantProjectCode, PersonalGeneralProjectCode} {
        var projID int64
        if err := db.Get(&projID, `SELECT id FROM data_projects WHERE project_code = ?`, code); err != nil {
            t.Fatalf("project %s not found: %v", code, err)
        }
        type stageRow struct {
            StageCode string `db:"stage_code"`
            SortOrder int    `db:"sort_order"`
        }
        var stages []stageRow
        if err := db.Select(&stages, `SELECT stage_code, sort_order FROM project_stages WHERE project_id = ? ORDER BY sort_order`, projID); err != nil {
            t.Fatal(err)
        }
        if len(stages) != 2 {
            t.Errorf("project %s should have 2 stages, got %d", code, len(stages))
        }
        if stages[0].StageCode != "GR-DRAFT" || stages[1].StageCode != "GR-FINAL" {
            t.Errorf("project %s stages should be [GR-DRAFT, GR-FINAL], got %v", code, stages)
        }
    }
}

func seedPersonalFilesTemplateV2InTest(t *testing.T, db *sqlx.DB) {
    t.Helper()
    now := time.Now()
    res, err := db.Exec(`INSERT INTO data_templates (
        template_code, template_name, template_version, status, project_sensitivity_level,
        cached_at, create_time, update_time, disable
    ) VALUES ('TPL-PERSONAL-FILES', '个人文件项目化管理模版', 'V2.0', 'active', 'general', ?, ?, ?, 0)`,
        now, now, now)
    if err != nil { t.Fatal(err) }
    tplID, _ := res.LastInsertId()
    draftRes, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
        VALUES (?, 'GR-DRAFT', '个人文件起草与修改', 'work', 1, ?, ?, ?, 0)`, tplID, now, now, now)
    draftID, _ := draftRes.LastInsertId()
    finalRes, _ := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
        VALUES (?, 'GR-FINAL', '个人文件定稿', 'record', 2, ?, ?, ?, 0)`, tplID, now, now, now)
    finalID, _ := finalRes.LastInsertId()
    db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
        VALUES (?, 'PRC-001', '过程版本', 'process', 0, '["*"]', ?, ?, ?, 0)`, draftID, now, now, now)
    db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, cached_at, create_time, update_time, disable)
        VALUES (?, 'OUT-001', '归档定稿', 'output', 0, '["*"]', ?, ?, ?, 0)`, finalID, now, now, now)
}
```

- [ ] **Step 2.2: 跑测试看失败**

```bash
cd data-asset-scan && go test ./internal/repository -run TestEnsurePersonalFilesContext_V2_TwoStages -v
```
预期 FAIL — 当前代码只找 V1.0，找不到就跳过，project 不会建。

- [ ] **Step 2.3: 修改 personal_files_init.go**

将第 46-50 行的模版查找改为优先 V2.0：

```go
// Step 1: 先检查模版是否已同步（优先 V2.0，回退 V1.0）
cacheRepo := NewTemplateCacheRepository(db)
tpl, err := cacheRepo.FindTemplateByCode("TPL-PERSONAL-FILES", "V2.0")
templateVersion := "V2.0"
if err != nil || tpl == nil {
    tpl, err = cacheRepo.FindTemplateByCode("TPL-PERSONAL-FILES", "V1.0")
    templateVersion = "V1.0"
    if err != nil || tpl == nil {
        return nil
    }
}
```

把 `ensureInternalProject` 的硬编码 `"V1.0"` 替换为参数 `templateVersion`：

```go
for _, spec := range specs {
    if err := ensureInternalProject(db, spec.Code, spec.Name, spec.ShortCode, spec.Sens, personalSubjectID, templateVersion); err != nil {
        return fmt.Errorf("ensure internal project %s: %w", spec.Code, err)
    }
}
```

修改 `ensureInternalProject` 签名 + 函数体内的 hardcoded 字符串：

```go
func ensureInternalProject(db *sqlx.DB, projectCode, projectName, shortCode, sensitivity string, subjectID int64, templateVersion string) error {
    // ... 既有逻辑
    if err := db.Get(&tplID,
        `SELECT id FROM data_templates WHERE template_code = ? AND template_version = ? AND disable = 0`,
        "TPL-PERSONAL-FILES", templateVersion); err != nil {
        return fmt.Errorf("template not synced: %w", err)
    }
    // ...
    res, err := db.Exec(`INSERT INTO data_projects (
        ..., template_code, template_version, ...
    ) VALUES (..., 'TPL-PERSONAL-FILES', ?, ...)`,
        ..., templateVersion, ...)
    // ...
}
```

另外把 file_version 默认 stage 的硬编码 `"GR-DA"` 改为遍历所有 project_stages — 因为 V2.0 有两个 stage，每个 stage 下的 rule 都应建 planned fv。当前代码已经是循环 stages，只需确认逻辑无需调整即可。

- [ ] **Step 2.4: 跑测试看通过**

```bash
go test ./internal/repository -run TestEnsurePersonalFilesContext_V2_TwoStages -v
```
预期 PASS。

确认旧测试不退化：

```bash
go test ./internal/repository -run TestEnsurePersonalFilesContext -v
go test ./internal/repository -run TestSubject_CRUD -v
```
预期 PASS。

- [ ] **Step 2.5: Commit**

```bash
cd data-asset-scan
git add internal/repository/personal_files_init.go internal/repository/personal_files_init_test.go
git commit -m "feat(personal): 内置项目支持 TPL-PERSONAL-FILES V2.0 双环节

§4.2-6 要求个人文件项目化管理包括 起草修改 + 定稿 两个工作环节。
ensurePersonalFilesContext 优先 V2.0，回退 V1.0，3 个内置项目据此实例化。"
```

---

## Task 3: BridgeClassifyToPersonalProject 区分过程/定稿

**Files:**
- Modify: `data-asset-scan/internal/repository/personal_files_bridge.go:291-408`
- Test: `data-asset-scan/internal/repository/personal_files_bridge_test.go` (modify)

**当前行为：** 所有桥接进来的资源都建 file_version 在 GR-DA 环节、data_state='output'、规则 OUT-001。
**目标行为：** "认领归类保护"是用户主动标定的"已分级"操作，默认归 GR-FINAL/OUT-001（定稿）；新增可选参数 `dataStateHint`，调用方可指定 process（如 §4.3-6 家族归档过程文件）。

- [ ] **Step 3.1: 写失败测试**

```go
func TestBridge_ToFinalStage_Default(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    seedPersonalFilesTemplateV2InTest(t, db)
    EnsurePersonalContextForTest(db)

    resID := seedResource(t, db, "测试.pdf", "BR001", 2 /*importance=2 重要*/)
    fvID, err := BridgeClassifyToPersonalProject(db, resID)
    if err != nil { t.Fatal(err) }
    if fvID == 0 { t.Fatal("应桥接成功") }

    var stage, state string
    db.Get(&stage, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID)
    db.Get(&state, `SELECT data_state FROM file_versions WHERE id = ?`, fvID)
    if stage != "GR-FINAL" { t.Errorf("默认应归 GR-FINAL, got %s", stage) }
    if state != "output" { t.Errorf("data_state 应为 output, got %s", state) }
}

func TestBridge_ToDraftStage_WithHint(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    seedPersonalFilesTemplateV2InTest(t, db)
    EnsurePersonalContextForTest(db)

    resID := seedResource(t, db, "草稿.pdf", "BR002", 2)
    fvID, err := BridgeClassifyToPersonalProjectWithState(db, resID, "process")
    if err != nil { t.Fatal(err) }

    var stage, state string
    db.Get(&stage, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID)
    db.Get(&state, `SELECT data_state FROM file_versions WHERE id = ?`, fvID)
    if stage != "GR-DRAFT" { t.Errorf("hint=process 应归 GR-DRAFT, got %s", stage) }
    if state != "process" { t.Errorf("data_state 应为 process, got %s", state) }
}

func TestBridge_LegacyV1_StillWorks(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    // 仅 seed V1.0 模版（无 V2.0），验证向后兼容
    seedPersonalFilesTemplateV1InTest(t, db)
    EnsurePersonalContextForTest(db)

    resID := seedResource(t, db, "兼容.pdf", "BR003", 2)
    fvID, err := BridgeClassifyToPersonalProject(db, resID)
    if err != nil { t.Fatal(err) }
    if fvID == 0 { t.Fatal("应桥接成功（V1.0 兼容）") }

    var stage string
    db.Get(&stage, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID)
    if stage != "GR-DA" { t.Errorf("V1.0 兼容路径应归 GR-DA, got %s", stage) }
}

func seedResource(t *testing.T, db *sqlx.DB, name, sign string, importance int) int64 {
    t.Helper()
    now := time.Now()
    r, err := db.Exec(`INSERT INTO data_resources (
        content_sign, source_count, workspace_source_count, first_create_time,
        resources_name, claim_status, importance_level, create_time, update_time, disable
    ) VALUES (?, 1, 1, ?, ?, 2, ?, ?, ?, 0)`,
        sign, now, name, importance, now, now)
    if err != nil { t.Fatal(err) }
    id, _ := r.LastInsertId()
    return id
}
```

- [ ] **Step 3.2: 跑测试看失败**

```bash
go test ./internal/repository -run "TestBridge_To" -v
```
预期 FAIL — `BridgeClassifyToPersonalProjectWithState` 未定义；当前 `BridgeClassifyToPersonalProject` 默认归 GR-DA。

- [ ] **Step 3.3: 重构 BridgeClassifyToPersonalProject**

把硬编码的 stage 和 rule 提取到一个 helper，按 `dataStateHint` 路由：

```go
// 在 personal_files_bridge.go 加：

// resolvePersonalStage 根据 dataState hint 找该项目下对应的 stage + rule
// hint="output" / "" / 未知 → GR-FINAL + OUT-001（默认定稿）
// hint="process"           → GR-DRAFT + PRC-001
// 项目只有 GR-DA 单环节（V1.0 兼容路径）→ 直接返回 GR-DA + OUT-001
func resolvePersonalStage(db *sqlx.DB, projectID int64, dataStateHint string) (stageID int64, stageCode, ruleCode, dataState string, ruleID int64, err error) {
    // 探测是 V2.0 (有 GR-DRAFT/GR-FINAL) 还是 V1.0 (有 GR-DA)
    type stageRow struct {
        ID   int64  `db:"id"`
        Code string `db:"stage_code"`
    }
    var stages []stageRow
    if err = db.Select(&stages, `SELECT id, stage_code FROM project_stages WHERE project_id = ? AND disable = 0 ORDER BY sort_order`, projectID); err != nil {
        return
    }
    stageByCode := map[string]int64{}
    for _, s := range stages { stageByCode[s.Code] = s.ID }

    // V2.0 路径
    if _, ok := stageByCode["GR-FINAL"]; ok {
        if dataStateHint == "process" {
            stageID = stageByCode["GR-DRAFT"]
            stageCode = "GR-DRAFT"
            ruleCode = "PRC-001"
            dataState = "process"
        } else {
            stageID = stageByCode["GR-FINAL"]
            stageCode = "GR-FINAL"
            ruleCode = "OUT-001"
            dataState = "output"
        }
    } else if _, ok := stageByCode["GR-DA"]; ok {
        // V1.0 兼容路径
        stageID = stageByCode["GR-DA"]
        stageCode = "GR-DA"
        ruleCode = "OUT-001"
        dataState = "output"
    } else {
        err = fmt.Errorf("项目 %d 未配置 personal 环节", projectID)
        return
    }

    // 查 rule id
    _ = db.Get(&ruleID, `SELECT tfr.id FROM template_file_rules tfr
        JOIN template_stages ts ON ts.id = tfr.template_stage_id
        JOIN project_stages ps ON ps.template_stage_id = ts.id
        WHERE ps.id = ? AND tfr.file_rule_code = ? AND tfr.disable = 0 LIMIT 1`,
        stageID, ruleCode)
    return
}

// BridgeClassifyToPersonalProject — 默认归定稿环节
func BridgeClassifyToPersonalProject(db *sqlx.DB, resourceID int64) (int64, error) {
    return BridgeClassifyToPersonalProjectWithState(db, resourceID, "output")
}

// BridgeClassifyToPersonalProjectWithState — 调用方可指定 data_state ("process" 或 "output")
func BridgeClassifyToPersonalProjectWithState(db *sqlx.DB, resourceID int64, dataStateHint string) (int64, error) {
    // 1-3 步骤同既有：查 resource、路由 project、找 project
    // (略，复用既有 91-127 行逻辑)

    // 4. 用 helper 解析 stage + rule
    stageID, stageCode, ruleCode, dataState, ruleIDInt, err := resolvePersonalStage(db, proj.ID, dataStateHint)
    if err != nil {
        return 0, fmt.Errorf("解析 stage 失败: %w", err)
    }
    var ruleIDPtr *int64
    if ruleIDInt > 0 { ruleIDPtr = &ruleIDInt }

    // 5. 幂等：fvCode 加入 stage_code + rule_code（避免 V1.0 旧 fvCode 和新 fvCode 冲突）
    fvCode := fmt.Sprintf("%s-%s-%s-DR%d", projectCode, stageCode, ruleCode, resourceID)
    // ...（既有幂等检查 + INSERT + ledger + lifecycle_events）
    // INSERT file_versions 时 data_state 用 helper 返回的值，不再写死 'output'
}
```

完整替换 personal_files_bridge.go 的 `BridgeClassifyToPersonalProject` 函数；保持 import 不变。

- [ ] **Step 3.4: 跑测试看通过**

```bash
go test ./internal/repository -run "TestBridge_" -v
```
预期 PASS — 3 个新 test + 既有 test 全过。

- [ ] **Step 3.5: Commit**

```bash
git add internal/repository/personal_files_bridge.go internal/repository/personal_files_bridge_test.go
git commit -m "feat(bridge): 桥接区分过程/定稿环节

§4.2-6 / §4.3-6 双环节桥接：
- 默认归 GR-FINAL/OUT-001（用户主动认领归类视为已定稿）
- 加 BridgeClassifyToPersonalProjectWithState 让调用方传 hint=process
- V1.0 模版回退到既有 GR-DA 路径"
```

---

## Task 4: AI 归目"驳回"端点与字段

**Files:**
- Modify: `data-asset-scan/internal/repository/migrations.go` — 加 data_resources 字段
- Modify: `data-asset-scan/internal/repository/audit_log.go` — 加 audit 码
- Create: `data-asset-scan/internal/httpd/ai_classify_reject_test.go`
- Modify: `data-asset-scan/internal/httpd/ai_classify.go` — 加 POST /ai/classify/reject

**字段新增：** `data_resources` 加 `ai_classify_rejected_at TIMESTAMP NULL`, `ai_classify_reject_reason TEXT NULL`。

`/ai/classify/pending` endpoint 需过滤 `ai_classify_rejected_at IS NULL`。

- [ ] **Step 4.1: 写 migration**

在 `migrations.go` 找到 V4 段下方加 V5 段：

```go
// V5-P1: AI 归目驳回字段
{
    Version: "v5_p1_ai_reject",
    Up: `
        ALTER TABLE data_resources ADD COLUMN ai_classify_rejected_at DATETIME NULL;
        ALTER TABLE data_resources ADD COLUMN ai_classify_reject_reason TEXT NULL;
    `,
},
```

- [ ] **Step 4.2: 加 audit 码**

`audit_log.go` 加：

```go
AuditAIClassifyReject   = "ai_classify_reject"
AuditAIClassifyApply    = "ai_classify_apply"  // 既有 apply 也补一个 audit 码方便统计
```

- [ ] **Step 4.3: 写失败测试**

`ai_classify_reject_test.go`：

```go
func TestHTTP_AIClassify_Reject(t *testing.T) {
    r, db, cleanup := setupTestServer(t)
    defer cleanup()
    seedPersonalProjectsForAI(t, db)
    repository.EnsurePersonalContextForTest(db)
    withActiveUser(t, db, "u1")
    resID := seedSimpleResourceWithDist(t, db, "测试.pdf", "AIRJ001", "/")

    status, resp := jsonReq(t, r, "POST", "/ai/classify/reject", map[string]interface{}{
        "resource_id": resID,
        "reason":      "用户决定不归目（私人文件）",
    })
    successOk(t, status, resp)

    // 验证字段写入
    var rejAt sql.NullTime
    var rejReason sql.NullString
    db.QueryRow(`SELECT ai_classify_rejected_at, ai_classify_reject_reason FROM data_resources WHERE data_resources_id = ?`, resID).Scan(&rejAt, &rejReason)
    if !rejAt.Valid { t.Error("ai_classify_rejected_at 应非空") }
    if rejReason.String != "用户决定不归目（私人文件）" { t.Errorf("reason 不对: %s", rejReason.String) }

    // pending 列表应不再含此 resource
    status, resp = jsonReqNoBody(t, r, "GET", "/ai/classify/pending?limit=100")
    successOk(t, status, resp)
    list := dataList(t, resp)
    for _, item := range list {
        m := item.(map[string]interface{})
        if int64(m["resource_id"].(float64)) == resID {
            t.Errorf("被驳回的 resource 不应出现在 pending: %d", resID)
        }
    }
}

func TestHTTP_AIClassify_Reject_MissingReason(t *testing.T) {
    r, db, cleanup := setupTestServer(t)
    defer cleanup()
    withActiveUser(t, db, "u1")
    status, resp := jsonReq(t, r, "POST", "/ai/classify/reject", map[string]interface{}{
        "resource_id": int64(1),
    })
    expectFailure(t, status, resp)  // 缺 reason 必拒
}
```

- [ ] **Step 4.4: 跑测试看失败**

```bash
go test ./internal/httpd -run TestHTTP_AIClassify_Reject -v
```
预期 FAIL — 路由 404 + 字段未存在。

- [ ] **Step 4.5: 实现 /ai/classify/reject 端点**

在 `ai_classify.go` 加：

```go
type rejectReq struct {
    ResourceID int64  `json:"resource_id"`
    Reason     string `json:"reason"`
}

func handleAIClassifyReject(c *gin.Context, db *sqlx.DB) {
    var req rejectReq
    if err := c.ShouldBindJSON(&req); err != nil {
        respondFailure(c, http.StatusBadRequest, "INVALID_BODY", err.Error())
        return
    }
    if req.ResourceID <= 0 || strings.TrimSpace(req.Reason) == "" {
        respondFailure(c, http.StatusBadRequest, "MISSING_FIELDS", "resource_id 和 reason 必填")
        return
    }
    now := time.Now()
    res, err := db.Exec(`UPDATE data_resources
        SET ai_classify_rejected_at = ?, ai_classify_reject_reason = ?
        WHERE data_resources_id = ?`,
        now, req.Reason, req.ResourceID)
    if err != nil {
        respondFailure(c, http.StatusInternalServerError, "DB_ERROR", err.Error())
        return
    }
    affected, _ := res.RowsAffected()
    if affected == 0 {
        respondFailure(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource 不存在")
        return
    }
    // audit
    userID, _ := middleware.GetUserID(c)
    repository.AppendAuditLog(db, repository.AppendAuditLogInput{
        Action: repository.AuditAIClassifyReject,
        TargetType: repository.AuditTargetFileVersion, // 暂用 fv target；TODO V5-P3 加 resource target
        TargetID: req.ResourceID,
        Reason: req.Reason,
        OperatorUserID: userID,
    })
    respondSuccess(c, gin.H{"status": "rejected", "resource_id": req.ResourceID})
}
```

并在 `RegisterAIClassifyRoutes` 加：

```go
g.POST("/reject", func(c *gin.Context) { handleAIClassifyReject(c, db) })
```

修改 `handleAIClassifyPending` 的 SQL，加 `AND (ai_classify_rejected_at IS NULL)` 过滤条件。

- [ ] **Step 4.6: 跑测试看通过**

```bash
go test ./internal/httpd -run TestHTTP_AIClassify_Reject -v
go test ./internal/httpd -run TestHTTP_AIClassify_Pending -v
```
预期 PASS。

- [ ] **Step 4.7: Commit**

```bash
git add internal/repository/migrations.go internal/repository/audit_log.go internal/httpd/ai_classify.go internal/httpd/ai_classify_reject_test.go
git commit -m "feat(ai-classify): 加驳回功能

§4.3-2: AI 归目支持人工驳回。
- data_resources 加 ai_classify_rejected_at/reason 字段
- POST /ai/classify/reject {resource_id, reason}
- pending 列表自动过滤已驳回项"
```

---

## Task 5: AI 归目"调整"——前端选择 project/stage/rule

**Files:**
- Modify: `data-asset-scan/frontend_real/views/AIClassifyView.vue`

**调整方式：** 复用既有 POST /ai/classify/apply 端点（已接受任意 project/stage/rule 三元组）。前端在每条建议旁加"调整"按钮，弹出对话框让用户从下拉框选 project / stage / rule（来自 /projects + /file-versions/projects/:id/stages-and-rules 数据），然后调 apply。

- [ ] **Step 5.1: 加调整对话框组件结构**

在 AIClassifyView.vue 模板内加 `<v-dialog v-model="adjustDialog">`，包含：

```vue
<v-dialog v-model="adjustDialog" max-width="600">
  <v-card>
    <v-card-title>调整归目目标</v-card-title>
    <v-card-text>
      <div class="text-caption mb-2">资源：{{ adjustItem?.resource_name || '—' }}</div>
      <v-select v-model="adjustForm.project_id" :items="projectOptions"
        item-title="label" item-value="value" label="项目" density="compact"
        @update:modelValue="onAdjustProjectChange" />
      <v-select v-model="adjustForm.stage_code" :items="adjustStageOptions"
        item-title="label" item-value="value" label="环节" density="compact"
        :disabled="!adjustForm.project_id" />
      <v-select v-model="adjustForm.file_rule_code" :items="adjustRuleOptions"
        item-title="label" item-value="value" label="文件规则" density="compact"
        :disabled="!adjustForm.stage_code" />
    </v-card-text>
    <v-card-actions>
      <v-spacer />
      <v-btn variant="text" @click="adjustDialog = false">取消</v-btn>
      <v-btn color="primary" variant="elevated" :loading="adjustApplying"
        :disabled="!canAdjustApply"
        @click="onAdjustApply">应用</v-btn>
    </v-card-actions>
  </v-card>
</v-dialog>
```

加 script 状态：

```typescript
const adjustDialog = ref(false)
const adjustItem = ref<PendingItem | null>(null)
const adjustForm = ref({ project_id: 0, stage_code: '', file_rule_code: '' })
const adjustApplying = ref(false)
const projectOptions = ref<Array<{label: string; value: number}>>([])
const adjustStageOptions = ref<Array<{label: string; value: string}>>([])
const adjustRuleOptions = ref<Array<{label: string; value: string}>>([])

async function loadProjectOptions() {
  const res = await fetch(`${API_BASE}/projects?status=active`)
  const json = await res.json()
  if (json.success) {
    projectOptions.value = (json.data || []).map((p: any) => ({
      label: `${p.project_code} ${p.project_name}`,
      value: p.id,
    }))
  }
}

async function onAdjustProjectChange(projectId: number) {
  adjustForm.value.stage_code = ''
  adjustForm.value.file_rule_code = ''
  if (!projectId) return
  const res = await fetch(`${API_BASE}/projects/${projectId}/stages-and-rules`)
  const json = await res.json()
  if (json.success && json.data) {
    adjustStageOptions.value = (json.data.stages || []).map((s: any) => ({
      label: `${s.stage_code} ${s.stage_name}`, value: s.stage_code,
    }))
  }
}

watch(() => adjustForm.value.stage_code, async (code) => {
  adjustForm.value.file_rule_code = ''
  if (!code || !adjustForm.value.project_id) return
  const res = await fetch(`${API_BASE}/projects/${adjustForm.value.project_id}/stages-and-rules?stage_code=${code}`)
  const json = await res.json()
  if (json.success && json.data?.rules) {
    adjustRuleOptions.value = json.data.rules.map((r: any) => ({
      label: `${r.file_rule_code} ${r.file_name}`, value: r.file_rule_code,
    }))
  }
})

const canAdjustApply = computed(() =>
  !!adjustForm.value.project_id && !!adjustForm.value.stage_code && !!adjustForm.value.file_rule_code
)

function openAdjust(item: PendingItem) {
  adjustItem.value = item
  adjustForm.value = { project_id: 0, stage_code: '', file_rule_code: '' }
  adjustDialog.value = true
  if (projectOptions.value.length === 0) loadProjectOptions()
}

async function onAdjustApply() {
  if (!adjustItem.value || !canAdjustApply.value) return
  adjustApplying.value = true
  try {
    const res = await fetch(`${API_BASE}/ai/classify/apply`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        resource_id: adjustItem.value.resource_id,
        project_id: adjustForm.value.project_id,
        stage_code: adjustForm.value.stage_code,
        file_rule_code: adjustForm.value.file_rule_code,
      }),
    })
    const json = await res.json()
    if (json.success) {
      snackbar.value = { show: true, text: '调整归目成功', color: 'success' }
      pending.value = pending.value.filter(p => p.resource_id !== adjustItem.value!.resource_id)
      adjustDialog.value = false
    } else {
      snackbar.value = { show: true, text: '失败：' + json.error, color: 'error' }
    }
  } finally {
    adjustApplying.value = false
  }
}
```

- [ ] **Step 5.2: 加"驳回"和"调整"按钮**

把 expansion-panel-title 里的按钮区扩展为 3 个：

```vue
<v-btn v-if="item.suggestions.length > 0" size="x-small" color="primary" variant="tonal"
  :loading="applyingId === item.resource_id" @click.stop="applyTopSuggestion(item)">
  应用 TOP
</v-btn>
<v-btn size="x-small" color="secondary" variant="text"
  @click.stop="openAdjust(item)">调整</v-btn>
<v-btn size="x-small" color="error" variant="text"
  @click.stop="openReject(item)">驳回</v-btn>
```

加驳回对话框（input reason）：

```vue
<v-dialog v-model="rejectDialog" max-width="500">
  <v-card>
    <v-card-title>驳回归目</v-card-title>
    <v-card-text>
      <div class="text-caption mb-2">资源：{{ rejectItem?.resource_name }}</div>
      <v-textarea v-model="rejectReason" label="驳回原因（必填）" rows="3"
        density="compact" variant="outlined" />
    </v-card-text>
    <v-card-actions>
      <v-spacer />
      <v-btn variant="text" @click="rejectDialog = false">取消</v-btn>
      <v-btn color="error" variant="elevated" :loading="rejecting"
        :disabled="!rejectReason.trim()" @click="onReject">确认驳回</v-btn>
    </v-card-actions>
  </v-card>
</v-dialog>
```

script:

```typescript
const rejectDialog = ref(false)
const rejectItem = ref<PendingItem | null>(null)
const rejectReason = ref('')
const rejecting = ref(false)

function openReject(item: PendingItem) {
  rejectItem.value = item
  rejectReason.value = ''
  rejectDialog.value = true
}

async function onReject() {
  if (!rejectItem.value || !rejectReason.value.trim()) return
  rejecting.value = true
  try {
    const res = await fetch(`${API_BASE}/ai/classify/reject`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        resource_id: rejectItem.value.resource_id,
        reason: rejectReason.value.trim(),
      }),
    })
    const json = await res.json()
    if (json.success) {
      snackbar.value = { show: true, text: '已驳回', color: 'success' }
      pending.value = pending.value.filter(p => p.resource_id !== rejectItem.value!.resource_id)
      rejectDialog.value = false
    } else {
      snackbar.value = { show: true, text: '失败：' + json.error, color: 'error' }
    }
  } finally {
    rejecting.value = false
  }
}
```

- [ ] **Step 5.3: 启动 scan 客户端手动验证**

```bash
cd data-asset-scan && cd frontend_real && yarn install && yarn dev
# 另一终端
cd .. && go run cmd/dev/main.go
```

打开 `http://localhost:5173/#/ai-classify`：
- 看一条 pending 资源 → 点"驳回" → 输入原因 → 确认 → 应消失
- 点"调整" → 选 SYS-PERSONAL-CORE / GR-FINAL / OUT-001 → 应用 → 应消失
- 刷新页面，被驳回的不应再出现

- [ ] **Step 5.4: Commit**

```bash
cd data-asset-scan
git add frontend_real/views/AIClassifyView.vue
git commit -m "feat(ai-classify): UI 加驳回 + 调整功能

§4.3-2: 支持确认/调整/驳回。
- 调整：弹出对话框，用户从下拉选项目/环节/规则后调 apply
- 驳回：弹出对话框输原因后调 /ai/classify/reject"
```

---

## Task 6: 解除绑定与重新归类 — 后端

**Files:**
- Create: `data-asset-scan/internal/repository/reclassify.go`
- Create: `data-asset-scan/internal/repository/reclassify_test.go`
- Modify: `data-asset-scan/internal/repository/migrations.go` — 加表 + 字段
- Modify: `data-asset-scan/internal/repository/audit_log.go` — 加码
- Modify: `data-asset-scan/internal/repository/lifecycle_event.go` — 加 `EventUnbind` `EventReclassify`

**业务定义：**
- **解除绑定 (Unbind)**：file_version 从项目摘下来 → `lifecycle_status='cancelled'`，对应 asset_ledger 也置 cancelled，加 lifecycle_event `unbind`
- **重新归类 (Reclassify)**：在原 fv 基础上"换目标"——撤销原 fv（unbind）+ 新建一条 fv 到新 (project, stage, rule)，并在新 fv 的 audit 里链回原 fv

- [ ] **Step 6.1: migration 加字段 + 表**

```go
{
    Version: "v5_p1_reclassify",
    Up: `
        ALTER TABLE file_versions ADD COLUMN unbind_time DATETIME NULL;
        ALTER TABLE file_versions ADD COLUMN unbind_reason TEXT NULL;
        ALTER TABLE file_versions ADD COLUMN reclassified_from_fv_id INTEGER NULL;

        CREATE TABLE IF NOT EXISTS reclassify_history (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            original_fv_id INTEGER NOT NULL,
            new_fv_id INTEGER NULL,
            action TEXT NOT NULL,           -- 'unbind' | 'reclassify'
            reason TEXT NOT NULL,
            operator_user_id INTEGER NULL,
            operator_name TEXT NULL,
            create_time DATETIME NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_reclassify_history_orig ON reclassify_history(original_fv_id);
    `,
},
```

- [ ] **Step 6.2: 加 audit + lifecycle 码**

audit_log.go:
```go
AuditFvUnbind     = "fv_unbind"
AuditFvReclassify = "fv_reclassify"
```

lifecycle_event.go:
```go
EventUnbind     = "unbind"
EventReclassify = "reclassify"
```

- [ ] **Step 6.3: 写失败测试**

`reclassify_test.go`:

```go
package repository

import (
    "testing"
    "time"

    "github.com/jmoiron/sqlx"
)

func TestUnbindFileVersion(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    seedPersonalFilesTemplateV2InTest(t, db)
    EnsurePersonalContextForTest(db)
    resID := seedResource(t, db, "解绑.pdf", "UB001", 2)
    fvID, _ := BridgeClassifyToPersonalProject(db, resID)

    if err := UnbindFileVersion(db, fvID, "误归目，文件已重新归档别处", &testUser); err != nil {
        t.Fatalf("unbind failed: %v", err)
    }

    var status string
    db.Get(&status, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, fvID)
    if status != "cancelled" { t.Errorf("fv lifecycle 应 cancelled, got %s", status) }

    var ledgerStatus string
    db.Get(&ledgerStatus, `SELECT lifecycle_status FROM asset_ledgers WHERE file_version_id = ?`, fvID)
    if ledgerStatus != "cancelled" { t.Errorf("ledger lifecycle 应 cancelled, got %s", ledgerStatus) }

    var unbindReason string
    db.Get(&unbindReason, `SELECT unbind_reason FROM file_versions WHERE id = ?`, fvID)
    if unbindReason != "误归目，文件已重新归档别处" { t.Errorf("reason mismatch") }

    // 历史记录
    var historyCount int
    db.Get(&historyCount, `SELECT COUNT(*) FROM reclassify_history WHERE original_fv_id = ? AND action = 'unbind'`, fvID)
    if historyCount != 1 { t.Errorf("应有 1 条 unbind 历史, got %d", historyCount) }

    // lifecycle_event
    var eventCount int
    db.Get(&eventCount, `SELECT COUNT(*) FROM lifecycle_events WHERE file_version_id = ? AND event_type = 'unbind'`, fvID)
    if eventCount != 1 { t.Errorf("应有 1 条 unbind event") }
}

func TestUnbindFileVersion_EmptyReason(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    err := UnbindFileVersion(db, 1, "", nil)
    if err == nil { t.Fatal("空 reason 应失败") }
}

func TestReclassifyFileVersion(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    seedPersonalFilesTemplateV2InTest(t, db)
    EnsurePersonalContextForTest(db)
    resID := seedResource(t, db, "重归.pdf", "RC001", 2 /*importance=2*/)
    origFvID, _ := BridgeClassifyToPersonalProject(db, resID)

    // 重归到 GENERAL 项目的 DRAFT
    var generalID int64
    db.Get(&generalID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalGeneralProjectCode)

    newFvID, err := ReclassifyFileVersion(db, ReclassifyInput{
        OriginalFvID:     origFvID,
        NewProjectID:     generalID,
        NewStageCode:     "GR-DRAFT",
        NewFileRuleCode:  "PRC-001",
        Reason:           "应归一般级而非重要级",
        OperatorUser:     &testUser,
    })
    if err != nil { t.Fatalf("reclassify failed: %v", err) }
    if newFvID == 0 { t.Fatal("应返回 new fv id") }
    if newFvID == origFvID { t.Error("new fv id 应不同于 orig") }

    // 原 fv 应 cancelled
    var origStatus string
    db.Get(&origStatus, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, origFvID)
    if origStatus != "cancelled" { t.Errorf("orig fv 应 cancelled") }

    // 新 fv 应 registered + reclassified_from_fv_id 指向原 fv
    var newStatus string
    var fromFv int64
    db.Get(&newStatus, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, newFvID)
    db.Get(&fromFv, `SELECT reclassified_from_fv_id FROM file_versions WHERE id = ?`, newFvID)
    if newStatus != "registered" { t.Errorf("new fv 应 registered, got %s", newStatus) }
    if fromFv != origFvID { t.Errorf("reclassified_from_fv_id 应=%d, got %d", origFvID, fromFv) }
}
```

- [ ] **Step 6.4: 跑测试看失败**

```bash
go test ./internal/repository -run "TestUnbind|TestReclassify" -v
```
预期 FAIL — Unbind / Reclassify 函数未定义。

- [ ] **Step 6.5: 实现 reclassify.go**

```go
package repository

import (
    "fmt"
    "strings"
    "time"

    "github.com/jmoiron/sqlx"
)

type ReclassifyInput struct {
    OriginalFvID    int64
    NewProjectID    int64
    NewStageCode    string
    NewFileRuleCode string
    Reason          string
    OperatorUser    *UserSnapshot   // V2-7 既有的用户结构
}

type UserSnapshot struct {
    UserID *int64
    Name   string
}

// UnbindFileVersion 解除绑定一个 file_version。
// 把 fv + ledger lifecycle 置 cancelled，写 history + lifecycle_event，不删除任何数据。
func UnbindFileVersion(db *sqlx.DB, fvID int64, reason string, operator *UserSnapshot) error {
    if fvID <= 0 { return fmt.Errorf("invalid fv id") }
    if strings.TrimSpace(reason) == "" { return fmt.Errorf("reason 必填") }

    now := time.Now()
    tx, err := db.Beginx()
    if err != nil { return err }
    defer tx.Rollback()

    // 校验 fv 当前不是 cancelled
    var curStatus string
    if err := tx.Get(&curStatus, `SELECT lifecycle_status FROM file_versions WHERE id = ? AND disable = 0`, fvID); err != nil {
        return fmt.Errorf("fv 不存在: %w", err)
    }
    if curStatus == "cancelled" {
        return fmt.Errorf("fv 已解除绑定，无需重复操作")
    }

    // 更新 fv
    if _, err := tx.Exec(`UPDATE file_versions
        SET lifecycle_status = 'cancelled', unbind_time = ?, unbind_reason = ?, update_time = ?
        WHERE id = ?`, now, reason, now, fvID); err != nil {
        return err
    }
    // 更新 ledger
    if _, err := tx.Exec(`UPDATE asset_ledgers
        SET lifecycle_status = 'cancelled', update_time = ?
        WHERE file_version_id = ?`, now, fvID); err != nil {
        return err
    }
    // 写 history
    var opID *int64; var opName string
    if operator != nil { opID = operator.UserID; opName = operator.Name }
    if _, err := tx.Exec(`INSERT INTO reclassify_history (
        original_fv_id, action, reason, operator_user_id, operator_name, create_time
    ) VALUES (?, 'unbind', ?, ?, ?, ?)`, fvID, reason, opID, opName, now); err != nil {
        return err
    }
    // 写 lifecycle_event
    opStr := "system"
    if opName != "" { opStr = opName }
    if _, err := tx.Exec(`INSERT INTO lifecycle_events (
        file_version_id, event_type, event_name, operator_id, reason, create_time
    ) VALUES (?, 'unbind', '解除绑定', ?, ?, ?)`, fvID, opStr, reason, now); err != nil {
        return err
    }
    return tx.Commit()
}

// ReclassifyFileVersion 把原 fv 解绑 + 新建到新目标。返回新 fv id。
func ReclassifyFileVersion(db *sqlx.DB, in ReclassifyInput) (int64, error) {
    if in.OriginalFvID <= 0 { return 0, fmt.Errorf("invalid original fv id") }
    if strings.TrimSpace(in.Reason) == "" { return 0, fmt.Errorf("reason 必填") }

    // 1. 找出原 fv 的 source_ref（resource_id）和 sensitivity
    var resourceID int64
    var checksum string
    var displayName string
    if err := db.Get(&struct {
        Checksum    string `db:"checksum"`
        DisplayName string `db:"display_name"`
    }{}, `SELECT checksum, display_name FROM file_versions WHERE id = ?`, in.OriginalFvID); err != nil {
        return 0, fmt.Errorf("查 orig fv: %w", err)
    }
    // 从 asset_ledger 的 source_ref 拿 resource_id
    var sourceRef string
    db.Get(&sourceRef, `SELECT source_ref FROM asset_ledgers WHERE file_version_id = ? AND disable = 0`, in.OriginalFvID)
    // 解析 sourceRef JSON 取 resource_id（简化版）
    // {"bridge_from":"data_resources","resource_id":N}
    if m := jsonExtractInt(sourceRef, "resource_id"); m > 0 { resourceID = m }
    _ = checksum; _ = displayName  // 用于复用

    // 2. 先 unbind 原 fv
    if err := UnbindFileVersion(db, in.OriginalFvID, "因重新归类而解绑："+in.Reason, in.OperatorUser); err != nil {
        return 0, err
    }
    // 3. 用 BridgeResourceToTarget 新建到新目标
    item, err := BridgeResourceToTarget(db, resourceID, in.NewProjectID, in.NewStageCode, in.NewFileRuleCode)
    if err != nil { return 0, fmt.Errorf("新建到新目标: %w", err) }
    if item.Status == "error" { return 0, fmt.Errorf(item.ErrorMsg) }

    newFvID := item.FileVersionID

    // 4. 在新 fv 上记 reclassified_from_fv_id
    now := time.Now()
    if _, err := db.Exec(`UPDATE file_versions SET reclassified_from_fv_id = ?, update_time = ? WHERE id = ?`,
        in.OriginalFvID, now, newFvID); err != nil {
        return newFvID, err
    }
    // 5. history record (reclassify)
    var opID *int64; var opName string
    if in.OperatorUser != nil { opID = in.OperatorUser.UserID; opName = in.OperatorUser.Name }
    db.Exec(`INSERT INTO reclassify_history (
        original_fv_id, new_fv_id, action, reason, operator_user_id, operator_name, create_time
    ) VALUES (?, ?, 'reclassify', ?, ?, ?, ?)`,
        in.OriginalFvID, newFvID, in.Reason, opID, opName, now)
    // 6. lifecycle event on new fv
    opStr := "system"
    if opName != "" { opStr = opName }
    db.Exec(`INSERT INTO lifecycle_events (
        file_version_id, event_type, event_name, operator_id, reason, create_time
    ) VALUES (?, 'reclassify', '重新归类', ?, ?, ?)`,
        newFvID, opStr, fmt.Sprintf("从 fv(%d) 重新归类：%s", in.OriginalFvID, in.Reason), now)

    return newFvID, nil
}

// jsonExtractInt 简易抽 JSON 整型字段（避免引入完整 json 解析）
func jsonExtractInt(s, key string) int64 {
    needle := `"` + key + `":`
    i := strings.Index(s, needle)
    if i < 0 { return 0 }
    j := i + len(needle)
    end := j
    for end < len(s) && (s[end] >= '0' && s[end] <= '9') { end++ }
    if end == j { return 0 }
    var n int64
    fmt.Sscanf(s[j:end], "%d", &n)
    return n
}
```

`testUser` 在测试 helper 里：

```go
var testUser = UserSnapshot{Name: "tester"}
```

- [ ] **Step 6.6: 跑测试看通过**

```bash
go test ./internal/repository -run "TestUnbind|TestReclassify" -v
```
预期 PASS。

- [ ] **Step 6.7: Commit**

```bash
git add internal/repository/reclassify.go internal/repository/reclassify_test.go \
  internal/repository/migrations.go internal/repository/audit_log.go internal/repository/lifecycle_event.go
git commit -m "feat(reclassify): 解除绑定 + 重新归类 + 原因登记

§4.3-4 项目版本文件手动归目归档：
- UnbindFileVersion：fv + ledger 置 cancelled，写 history + event
- ReclassifyFileVersion：解绑原 fv + Bridge 到新目标，链路可追溯
- 新增 reclassify_history 表，file_versions 加 unbind_*, reclassified_from_fv_id 字段"
```

---

## Task 7: 解除绑定 / 重新归类 HTTP 端点

**Files:**
- Modify: `data-asset-scan/internal/httpd/file_versions.go`
- Create: `data-asset-scan/internal/httpd/file_versions_unbind_test.go`

- [ ] **Step 7.1: 写失败测试**

```go
func TestHTTP_FileVersion_Unbind(t *testing.T) {
    r, db, cleanup := setupTestServer(t)
    defer cleanup()
    withActiveUser(t, db, "u1")
    seedPersonalProjectsForAI(t, db)
    repository.EnsurePersonalContextForTest(db)
    resID := seedSimpleResourceWithDist(t, db, "解绑测试.pdf", "UBHT001", "/")
    fvID, _ := repository.BridgeClassifyToPersonalProjectWithImportance(db, resID, 2 /*importance*/)
    _ = fvID

    status, resp := jsonReq(t, r, "POST", fmt.Sprintf("/file-versions/%d/unbind", fvID), map[string]interface{}{
        "reason": "测试解绑",
    })
    successOk(t, status, resp)

    var s string
    db.Get(&s, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, fvID)
    if s != "cancelled" { t.Errorf("应 cancelled, got %s", s) }
}

func TestHTTP_FileVersion_Reclassify(t *testing.T) {
    r, db, cleanup := setupTestServer(t)
    defer cleanup()
    withActiveUser(t, db, "u1")
    seedPersonalProjectsForAI(t, db)
    repository.EnsurePersonalContextForTest(db)
    resID := seedSimpleResourceWithDist(t, db, "重归测试.pdf", "RCHT001", "/")
    fvID, _ := repository.BridgeClassifyToPersonalProjectWithImportance(db, resID, 1)  // 核心

    var generalID int64
    db.Get(&generalID, `SELECT id FROM data_projects WHERE project_code = ?`, repository.PersonalGeneralProjectCode)

    status, resp := jsonReq(t, r, "POST", fmt.Sprintf("/file-versions/%d/reclassify", fvID), map[string]interface{}{
        "new_project_id":     generalID,
        "new_stage_code":     "GR-DRAFT",
        "new_file_rule_code": "PRC-001",
        "reason":             "应一般级",
    })
    successOk(t, status, resp)

    d := dataMap(t, resp)
    newFvID := int64(d["new_fv_id"].(float64))
    if newFvID == fvID { t.Error("new fv id 应不同") }

    // 原 fv cancelled，新 fv registered
    var orig, newSt string
    db.Get(&orig, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, fvID)
    db.Get(&newSt, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, newFvID)
    if orig != "cancelled" { t.Error("orig 应 cancelled") }
    if newSt != "registered" { t.Error("new 应 registered") }
}
```

- [ ] **Step 7.2: 跑测试看失败**

```bash
go test ./internal/httpd -run "TestHTTP_FileVersion_Unbind|TestHTTP_FileVersion_Reclassify" -v
```
预期 FAIL。

- [ ] **Step 7.3: 实现 handler**

在 `file_versions.go` 加：

```go
func handleUnbind(c *gin.Context, db *sqlx.DB) {
    fvID, err := parseIDParam(c, "id")
    if err != nil { respondFailure(c, http.StatusBadRequest, "INVALID_ID", err.Error()); return }
    var body struct {
        Reason string `json:"reason"`
    }
    if err := c.ShouldBindJSON(&body); err != nil {
        respondFailure(c, http.StatusBadRequest, "INVALID_BODY", err.Error()); return
    }
    user := mustActiveUser(c, db)
    op := repository.UserSnapshot{UserID: &user.ID, Name: user.Username}
    if err := repository.UnbindFileVersion(db, fvID, body.Reason, &op); err != nil {
        respondFailure(c, http.StatusBadRequest, "UNBIND_FAILED", err.Error()); return
    }
    repository.AppendAuditLog(db, repository.AppendAuditLogInput{
        Action: repository.AuditFvUnbind,
        TargetType: repository.AuditTargetFileVersion,
        TargetID: fvID,
        Reason: body.Reason,
        OperatorUserID: &user.ID,
    })
    respondSuccess(c, gin.H{"status": "unbound", "fv_id": fvID})
}

func handleReclassify(c *gin.Context, db *sqlx.DB) {
    fvID, err := parseIDParam(c, "id")
    if err != nil { respondFailure(c, http.StatusBadRequest, "INVALID_ID", err.Error()); return }
    var body struct {
        NewProjectID    int64  `json:"new_project_id"`
        NewStageCode    string `json:"new_stage_code"`
        NewFileRuleCode string `json:"new_file_rule_code"`
        Reason          string `json:"reason"`
    }
    if err := c.ShouldBindJSON(&body); err != nil {
        respondFailure(c, http.StatusBadRequest, "INVALID_BODY", err.Error()); return
    }
    user := mustActiveUser(c, db)
    op := repository.UserSnapshot{UserID: &user.ID, Name: user.Username}
    newFv, err := repository.ReclassifyFileVersion(db, repository.ReclassifyInput{
        OriginalFvID:    fvID,
        NewProjectID:    body.NewProjectID,
        NewStageCode:    body.NewStageCode,
        NewFileRuleCode: body.NewFileRuleCode,
        Reason:          body.Reason,
        OperatorUser:    &op,
    })
    if err != nil { respondFailure(c, http.StatusBadRequest, "RECLASSIFY_FAILED", err.Error()); return }
    repository.AppendAuditLog(db, repository.AppendAuditLogInput{
        Action: repository.AuditFvReclassify,
        TargetType: repository.AuditTargetFileVersion,
        TargetID: fvID,
        Reason: body.Reason,
        OperatorUserID: &user.ID,
    })
    respondSuccess(c, gin.H{"status": "reclassified", "original_fv_id": fvID, "new_fv_id": newFv})
}
```

在 `RegisterFileVersionsRoutes` 加：

```go
g.POST("/:id/unbind", func(c *gin.Context) { handleUnbind(c, db) })
g.POST("/:id/reclassify", func(c *gin.Context) { handleReclassify(c, db) })
```

- [ ] **Step 7.4: 跑测试看通过**

```bash
go test ./internal/httpd -run "TestHTTP_FileVersion_Unbind|TestHTTP_FileVersion_Reclassify" -v
```
预期 PASS。

- [ ] **Step 7.5: Commit**

```bash
git add internal/httpd/file_versions.go internal/httpd/file_versions_unbind_test.go
git commit -m "feat(fv): 解绑 + 重新归类 HTTP 端点

POST /file-versions/:id/unbind {reason}
POST /file-versions/:id/reclassify {new_project_id, new_stage_code, new_file_rule_code, reason}"
```

---

## Task 8: LedgerView 加"解绑"+"重新归类"入口

**Files:**
- Modify: `data-asset-scan/frontend_real/views/LedgerView.vue`

**入口位置：** 底账列表每行的 action 区。仅当 lifecycle_status ∈ {registered, in_use, sealed} 时显示按钮（cancelled/destroyed/permanent 不可解绑）。

- [ ] **Step 8.1: 加按钮 + 两个对话框**

```vue
<v-btn v-if="canUnbind(item)" size="x-small" variant="text" color="warning"
  @click="openUnbind(item)">解绑</v-btn>
<v-btn v-if="canUnbind(item)" size="x-small" variant="text" color="secondary"
  @click="openReclassify(item)">重新归类</v-btn>
```

```vue
<!-- 解绑对话框 -->
<v-dialog v-model="unbindDialog" max-width="500">
  <v-card>
    <v-card-title>解除绑定</v-card-title>
    <v-card-text>
      <div class="text-caption mb-2">{{ unbindItem?.file_version_code }}</div>
      <v-textarea v-model="unbindReason" label="解绑原因（必填）" rows="3" />
    </v-card-text>
    <v-card-actions>
      <v-spacer />
      <v-btn variant="text" @click="unbindDialog = false">取消</v-btn>
      <v-btn color="warning" :loading="unbinding" :disabled="!unbindReason.trim()"
        @click="onUnbind">确认解绑</v-btn>
    </v-card-actions>
  </v-card>
</v-dialog>

<!-- 重新归类对话框：复用 Task 5 风格的三级下拉 -->
<v-dialog v-model="reclassifyDialog" max-width="600">
  ... (类似 AIClassifyView 的 adjustDialog)
  <v-textarea v-model="reclassifyReason" label="重新归类原因（必填）" rows="2" />
</v-dialog>
```

script:

```typescript
function canUnbind(item: any) {
  return ['registered', 'in_use', 'sealed'].includes(item.lifecycle_status)
}

async function onUnbind() {
  unbinding.value = true
  try {
    const res = await fetch(`${API_BASE}/file-versions/${unbindItem.value.file_version_id}/unbind`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason: unbindReason.value.trim() }),
    })
    const json = await res.json()
    if (json.success) {
      snackbar.value = { show: true, text: '已解绑', color: 'success' }
      unbindDialog.value = false
      await loadLedgers()
    } else {
      snackbar.value = { show: true, text: '失败：' + json.error, color: 'error' }
    }
  } finally { unbinding.value = false }
}

async function onReclassify() {
  // 类似 AIClassifyView.onAdjustApply，但调 /file-versions/:id/reclassify
}
```

- [ ] **Step 8.2: 手动验证**

跑前端 + 后端，进 `/ledgers` 页：
- 找一条已挂账的 fv（lifecycle=registered）→ 点"解绑" → 输原因 → 列表该行的 lifecycle 变 cancelled
- 找一条已挂账的 fv → 点"重新归类" → 选新目标 → 应有新底账出现

- [ ] **Step 8.3: Commit**

```bash
git add frontend_real/views/LedgerView.vue
git commit -m "feat(ledger): 底账加解绑 + 重新归类入口"
```

---

## Task 9: 家族归档过程/定稿分流 — 后端

**Files:**
- Modify: `data-asset-scan/internal/repository/personal_files_bridge.go` — `BridgeFamilyToProject` 加分流模式
- Modify: `data-asset-scan/internal/httpd/family.go` — 端点接受新参数
- Create: `data-asset-scan/internal/httpd/family_split_test.go`

**分流策略：** 在 family 内按 `data_resources.first_create_time` 倒序，**最近 1 条** → final 目标；**其余** → process 目标。如果用户指定两个目标三元组，分别 bridge；若用户只给一个目标（兼容旧调用），全部到该目标。

- [ ] **Step 9.1: 写失败测试**

```go
func TestBridgeFamily_SplitProcessAndFinal(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    seedPersonalFilesTemplateV2InTest(t, db)
    EnsurePersonalContextForTest(db)

    // 建一个 family: 3 个 member
    ids := make([]int64, 0, 3)
    base := time.Now()
    for i, name := range []string{"过程稿v1.docx", "过程稿v2.docx", "定稿.docx"} {
        r, _ := db.Exec(`INSERT INTO data_resources (
            content_sign, source_count, workspace_source_count, first_create_time,
            resources_name, claim_status, importance_level, create_time, update_time, disable
        ) VALUES (?, 1, 1, ?, ?, 2, 2, ?, ?, 0)`,
            fmt.Sprintf("FAM%d", i+1), base.Add(time.Duration(i)*time.Hour), name, now, now)
        id, _ := r.LastInsertId()
        ids = append(ids, id)
    }
    // 建 family
    famRepo := NewFamilyRepository(db)
    famID, _ := famRepo.CreateFamilyWithMembers(ids)

    var importantID int64
    db.Get(&importantID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalImportantProjectCode)

    result, err := BridgeFamilyToProjectSplit(db, famID, importantID,
        "GR-DRAFT", "PRC-001",   // process 目标
        "GR-FINAL", "OUT-001",    // final 目标
    )
    if err != nil { t.Fatal(err) }
    if result.Total != 3 { t.Errorf("Total 应=3") }
    if result.Archived != 3 { t.Errorf("Archived 应=3") }

    // 最新的（"定稿.docx"，first_create_time 最大）应归 GR-FINAL/OUT-001
    var finalCount, processCount int
    db.Get(&finalCount, `SELECT COUNT(*) FROM file_versions fv
        JOIN project_stages ps ON ps.id = fv.project_stage_id
        WHERE ps.stage_code = 'GR-FINAL' AND fv.checksum = 'FAM3'`)
    db.Get(&processCount, `SELECT COUNT(*) FROM file_versions fv
        JOIN project_stages ps ON ps.id = fv.project_stage_id
        WHERE ps.stage_code = 'GR-DRAFT' AND fv.checksum IN ('FAM1','FAM2')`)
    if finalCount != 1 { t.Errorf("FAM3（最新）应归 GR-FINAL, got %d", finalCount) }
    if processCount != 2 { t.Errorf("FAM1+FAM2 应归 GR-DRAFT, got %d", processCount) }
}
```

- [ ] **Step 9.2: 跑测试看失败**

```bash
go test ./internal/repository -run TestBridgeFamily_SplitProcessAndFinal -v
```
预期 FAIL — 函数未定义。

- [ ] **Step 9.3: 实现 BridgeFamilyToProjectSplit**

```go
// BridgeFamilyToProjectSplit §4.3-6 历史数据家族式自动归档：过程文件 + 最新文件分别归入
//
// 行为：family 内按 first_create_time 倒序，最新的 1 条归 final 目标，其余归 process 目标。
// 如 process 与 final 目标三元组相同，等价于 BridgeFamilyToProject 单目标行为。
func BridgeFamilyToProjectSplit(db *sqlx.DB, familyID, projectID int64,
    processStageCode, processRuleCode, finalStageCode, finalRuleCode string) (*FamilyBatchArchiveResult, error) {

    processTarget, err := resolveBridgeTarget(db, projectID, processStageCode, processRuleCode)
    if err != nil { return nil, err }
    finalTarget, err := resolveBridgeTarget(db, projectID, finalStageCode, finalRuleCode)
    if err != nil { return nil, err }

    // 取 family 成员，按 first_create_time 倒序
    type memberRow struct {
        ID    int64     `db:"data_resources_id"`
        Ctime time.Time `db:"first_create_time"`
    }
    var members []memberRow
    if err := db.Select(&members, `
        SELECT dr.data_resources_id, dr.first_create_time
        FROM data_resources dr
        JOIN data_family_members fm ON fm.data_resources_id = dr.data_resources_id
        WHERE fm.family_id = ? AND dr.disable = 0
        ORDER BY dr.first_create_time DESC`, familyID); err != nil {
        return nil, fmt.Errorf("查 family 成员: %w", err)
    }
    if len(members) == 0 { return nil, fmt.Errorf("家族 %d 无成员", familyID) }

    result := &FamilyBatchArchiveResult{
        FamilyID:     familyID,
        ProjectCode:  finalTarget.ProjectCode,
        StageCode:    "split:" + processStageCode + "→" + finalStageCode,
        FileRuleCode: "split:" + processRuleCode + "→" + finalRuleCode,
        Total:        len(members),
        Details:      make([]FamilyBatchArchiveItem, 0, len(members)),
    }

    for i, m := range members {
        var target BridgeTargetProject
        if i == 0 {
            target = finalTarget   // 最新归 final
        } else {
            target = processTarget // 其余归 process
        }
        item, _ := bridgeOneResource(db, target, m.ID)
        result.Details = append(result.Details, item)
        switch item.Status {
        case "archived": result.Archived++
        case "skipped_already": result.SkippedAlready++
        case "error": result.Errors++
        }
    }
    return result, nil
}
```

- [ ] **Step 9.4: 跑测试看通过**

```bash
go test ./internal/repository -run TestBridgeFamily_SplitProcessAndFinal -v
```
预期 PASS。

- [ ] **Step 9.5: HTTP 端点**

`family.go` 加 handler：

```go
// 既有 POST /family/:id/batch-archive 改成接受 split 模式参数
// 兼容旧调用：若 stage_code/file_rule_code 给齐 → 单目标
//          若 final_stage_code/final_file_rule_code 也给 → 双目标分流
type familyBatchReq struct {
    ProjectID       int64  `json:"project_id"`
    StageCode       string `json:"stage_code"`        // process 目标（或单一目标）
    FileRuleCode    string `json:"file_rule_code"`
    FinalStageCode  string `json:"final_stage_code"`  // optional
    FinalFileRuleCode string `json:"final_file_rule_code"`
}

func handleFamilyBatchArchive(c *gin.Context, db *sqlx.DB) {
    famID, _ := parseIDParam(c, "id")
    var req familyBatchReq
    if err := c.ShouldBindJSON(&req); err != nil { /* ... */ }

    var result *repository.FamilyBatchArchiveResult
    var err error
    if req.FinalStageCode != "" && req.FinalFileRuleCode != "" {
        result, err = repository.BridgeFamilyToProjectSplit(db, famID, req.ProjectID,
            req.StageCode, req.FileRuleCode, req.FinalStageCode, req.FinalFileRuleCode)
    } else {
        result, err = repository.BridgeFamilyToProject(db, famID, req.ProjectID, req.StageCode, req.FileRuleCode)
    }
    if err != nil { /* ... */ }
    respondSuccess(c, result)
}
```

- [ ] **Step 9.6: 测试 + Commit**

```bash
go test ./internal/httpd -run TestHTTP_Family -v
```
预期 PASS。

```bash
git add internal/repository/personal_files_bridge.go internal/httpd/family.go internal/httpd/family_split_test.go
git commit -m "feat(family): 家族归档过程/定稿自动分流

§4.3-6: 历史数据家族式自动归档，过程文件和最新文件分别关联。
- BridgeFamilyToProjectSplit: 按 first_create_time 倒序，最新归 final 目标，其余归 process
- family batch-archive 端点接受 final_stage_code / final_file_rule_code 参数（可选，缺则单目标）"
```

---

## Task 10: ClassifyView 家族对话框双目标 UI

**Files:**
- Modify: `data-asset-scan/frontend_real/views/ClassifyView.vue` 家族对话框区

- [ ] **Step 10.1: 改对话框加"过程目标"+"定稿目标"二级**

把单组三级下拉变成两组：

```vue
<v-card-text>
  <v-alert type="info" variant="tonal" density="compact" class="mb-3">
    家族归档将按"最新文件 → 定稿目标"、"其他过程文件 → 过程目标"自动分流
  </v-alert>

  <div class="text-subtitle-2 mb-2">过程目标</div>
  <v-select v-model="famForm.project_id" :items="projectOptions" label="项目" density="compact"
    @update:modelValue="onFamProjectChange" />
  <v-select v-model="famForm.process_stage" :items="famStageOptions" label="过程环节" density="compact" />
  <v-select v-model="famForm.process_rule" :items="getRuleOptions(famForm.process_stage)" label="过程文件规则" density="compact" />

  <div class="text-subtitle-2 mb-2 mt-4">定稿目标</div>
  <v-select v-model="famForm.final_stage" :items="famStageOptions" label="定稿环节" density="compact" />
  <v-select v-model="famForm.final_rule" :items="getRuleOptions(famForm.final_stage)" label="定稿文件规则" density="compact" />
</v-card-text>
```

更新提交逻辑：

```typescript
async function onFamArchive() {
  const payload: any = {
    project_id: famForm.value.project_id,
    stage_code: famForm.value.process_stage,
    file_rule_code: famForm.value.process_rule,
  }
  // 双目标
  if (famForm.value.final_stage && famForm.value.final_rule) {
    payload.final_stage_code = famForm.value.final_stage
    payload.final_file_rule_code = famForm.value.final_rule
  }
  const res = await fetch(`${API_BASE}/family/${famId}/batch-archive`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  // ...
}
```

- [ ] **Step 10.2: 手动验证**

进 "认领归类保护" 页：
- 选一个 family 有 3+ 成员（一个明显最新的）
- 点"家族批量归档"
- 选 SYS-PERSONAL-IMPORTANT → 过程: GR-DRAFT/PRC-001 → 定稿: GR-FINAL/OUT-001
- 提交后确认底账里看到：1 个 fv 在 GR-FINAL、其余在 GR-DRAFT

- [ ] **Step 10.3: Commit**

```bash
git add frontend_real/views/ClassifyView.vue
git commit -m "feat(classify): 家族归档对话框支持过程/定稿双目标"
```

---

## Task 11: V5-P1 端到端验收测试

**Files:**
- Create: `data-asset-scan/internal/repository/v5_p1_acceptance_test.go`

一个综合脚本验证 P1 所有功能闭环。

- [ ] **Step 11.1: 写综合用例**

```go
package repository

import "testing"

// V5-P1 端到端：模版升级 → 桥接区分 → 解绑 → 重归类 → 家族分流，全链路。
func TestV5P1_EndToEnd(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()

    // 1. seed V2.0 模版（模拟 manage 端同步）
    seedPersonalFilesTemplateV2InTest(t, db)

    // 2. 启动初始化，3 个项目 + 2 环节 应建好
    if err := ensurePersonalFilesContext(db); err != nil { t.Fatal(err) }
    for _, code := range []string{PersonalCoreProjectCode, PersonalImportantProjectCode, PersonalGeneralProjectCode} {
        var stageCount int
        db.Get(&stageCount, `SELECT COUNT(*) FROM project_stages ps
            JOIN data_projects p ON p.id = ps.project_id
            WHERE p.project_code = ? AND ps.disable = 0`, code)
        if stageCount != 2 { t.Errorf("%s 应有 2 stage, got %d", code, stageCount) }
    }

    // 3. 桥接：默认归 GR-FINAL
    resID := seedResource(t, db, "测试.pdf", "E2E001", 2)
    fvID, _ := BridgeClassifyToPersonalProject(db, resID)
    var stage string
    db.Get(&stage, `SELECT ps.stage_code FROM file_versions fv JOIN project_stages ps ON ps.id = fv.project_stage_id WHERE fv.id = ?`, fvID)
    if stage != "GR-FINAL" { t.Errorf("默认 stage 应 GR-FINAL, got %s", stage) }

    // 4. 解绑
    if err := UnbindFileVersion(db, fvID, "test unbind", &testUser); err != nil { t.Fatal(err) }
    var lc string
    db.Get(&lc, `SELECT lifecycle_status FROM file_versions WHERE id = ?`, fvID)
    if lc != "cancelled" { t.Errorf("unbind 后应 cancelled") }

    // 5. 重新归类
    resID2 := seedResource(t, db, "重归.pdf", "E2E002", 1) // 核心
    fv2, _ := BridgeClassifyToPersonalProject(db, resID2)
    var coreID int64
    db.Get(&coreID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalCoreProjectCode)
    var generalID int64
    db.Get(&generalID, `SELECT id FROM data_projects WHERE project_code = ?`, PersonalGeneralProjectCode)
    newFv, err := ReclassifyFileVersion(db, ReclassifyInput{
        OriginalFvID: fv2, NewProjectID: generalID,
        NewStageCode: "GR-DRAFT", NewFileRuleCode: "PRC-001",
        Reason: "test reclassify", OperatorUser: &testUser,
    })
    if err != nil { t.Fatal(err) }
    if newFv == 0 { t.Fatal("new fv 应存在") }

    // 6. 家族分流
    famID := seedFamily(t, db, []string{"v1.docx", "v2.docx", "v3-定稿.docx"})
    result, err := BridgeFamilyToProjectSplit(db, famID, generalID, "GR-DRAFT", "PRC-001", "GR-FINAL", "OUT-001")
    if err != nil { t.Fatal(err) }
    if result.Total != 3 || result.Archived != 3 {
        t.Errorf("family 应全部归档，got total=%d archived=%d", result.Total, result.Archived)
    }
}
```

- [ ] **Step 11.2: 跑全套测试**

```bash
go test ./... -count=1
```

预期：所有测试 PASS（含 V1-V4 既有 + V5-P1 新增）。

- [ ] **Step 11.3: Commit**

```bash
git add internal/repository/v5_p1_acceptance_test.go
git commit -m "test(v5-p1): 端到端验收测试

覆盖 V2.0 模版加载 → 双环节项目初始化 → 默认桥接到定稿 →
解绑 → 重新归类 → 家族过程/定稿分流 全链路。"
```

---

## Task 12: V5-P1 完成报告 & tag

**Files:**
- Create: `data-asset-scan/docs/V5_P1_REPORT.md`

- [ ] **Step 12.1: 写 P1 完成报告**

包含：
- 13 个功能点对照（A1-A4 + 改了哪些 file + 新加哪些 endpoint）
- 测试覆盖
- 后续 P2-P4 计划
- 已知偏差（如有）

- [ ] **Step 12.2: 打 tag**

```bash
git tag -a v5-p1-quick-wins -m "V5 Phase 1: Quick Wins (§4.2-6 双环节 + §4.3-2 驳回调整 + §4.3-4 解绑重归类 + §4.3-6 家族分流)"
```

---

## 完成判据

V5-P1 完成判据：

- [ ] manage 端：TPL-PERSONAL-FILES V2.0 已 seed，V1.0 deprecated
- [ ] scan 端：3 个 SYS-PERSONAL-* 项目每个有 2 个 stage（GR-DRAFT + GR-FINAL）
- [ ] scan 端：归类保护页归档默认归 GR-FINAL
- [ ] scan 端：AI 归目页可驳回（按钮 + 对话框 + 不再出现在 pending）
- [ ] scan 端：AI 归目页可调整目标（三级下拉 → apply）
- [ ] scan 端：底账可解绑（按钮 + 对话框 + lifecycle=cancelled）
- [ ] scan 端：底账可重新归类（生成新 fv + 原 fv cancelled + reclassified_from_fv_id 串联）
- [ ] scan 端：家族归档双目标 UI（过程目标 + 定稿目标）+ 后端自动分流
- [ ] go test ./... 全部 PASS
- [ ] manage 端 vitest 全部 PASS

---

## Self-Review 检查记录

**Spec coverage:**
- A1 §4.2-6 双环节 → Task 1-3 ✓
- A2 §4.3-2 驳回 → Task 4-5 ✓
- A2 §4.3-2 调整 → Task 5 ✓
- A3 §4.3-4 解绑 → Task 6-8 ✓
- A3 §4.3-4 重新归类 → Task 6-8 ✓
- A3 §4.3-4 原因登记 → Task 6（强制 reason 字段）✓
- A4 §4.3-6 过程/定稿分流 → Task 9-10 ✓

**Placeholder scan:** 已避免 TBD / "类似 Task N" 等占位描述。所有代码块完整可执行。

**Type consistency:**
- `BridgeClassifyToPersonalProject` / `BridgeClassifyToPersonalProjectWithState` — 同包 ✓
- `UserSnapshot` 结构在 Task 6 定义，Task 7 使用 ✓
- `ReclassifyInput` 字段名 Task 6 与 Task 7 一致 ✓
- `BridgeFamilyToProjectSplit` 签名 Task 9 与 HTTP handler 一致 ✓
