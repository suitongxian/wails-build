# 数据业务模版 V1 — UI 手动验证清单

> 形式：每条用例编号 + 操作步骤 + 预期 UI 现象 + 预期 DB 状态 + 校验 SQL。
>
> 数据库默认路径（wails dev）：`~/.config/data-asset-scan/data.db`
> 校验 SQL 推荐用 `sqlite3 ~/.config/data-asset-scan/data.db "..."` 直接跑

---

## 0 验证前置准备

### 0.1 manage 端
- [ ] 启动：`cd data-asset-manage && yarn dev`，浏览器打开 `http://localhost:3000`
- [ ] 模版页 `/templates`：能看到 `TPL-PRINT-BOOK V2.1`，状态 `active`，含 10 个环节、30+ 文件规则。
- [ ] 校验 SQL（manage 库）：

```bash
sqlite3 data-asset-manage/data.db "
SELECT template_code, template_version, status,
  (SELECT COUNT(*) FROM template_stages WHERE template_id = dt.id) AS stages,
  (SELECT COUNT(*) FROM template_file_rules tfr JOIN template_stages ts ON ts.id=tfr.template_stage_id WHERE ts.template_id = dt.id) AS rules
FROM data_templates dt WHERE template_code='TPL-PRINT-BOOK';"
```
预期：`status=active, stages=10, rules≥30`

### 0.2 scan 端
- [ ] 启动：`cd data-asset-scan && wails dev`
- [ ] 设置 → 系统配置：
  - 项目根目录：`/tmp/scan-root`（或任意可写路径）
  - manage_endpoint：`http://localhost:3000`
- [ ] 校验 SQL：
```bash
SCAN_DB=~/.config/data-asset-scan/data.db
sqlite3 $SCAN_DB "SELECT key, value FROM system_config WHERE key IN ('project_root','manage_endpoint');"
```
预期：两条记录均非空。

---

## 1 三主体管理（subjects）

### 1.1 创建三种类型
- [ ] 点击左侧"三主体管理" → 右上"添加"，分别创建：
  - person：code=`OWNER-1`, name=`张三`
  - department：code=`DEPT-1`, name=`印刷部`
  - organization：code=`ORG-1`, name=`印刷院`

**预期 UI**：列表立即显示 3 条记录，类型 chip 颜色不同。

**校验 SQL**：
```bash
sqlite3 $SCAN_DB "SELECT code, name, type, status FROM subjects WHERE disable=0 ORDER BY id;"
```
预期：3 条记录，type 分别为 `person/department/organization`。

### 1.2 重复 code 应被拒绝
- [ ] 再创建一个 code=`OWNER-1` 的记录。
**预期 UI**：弹错误提示，不进列表。
**校验 SQL**：上述 count 仍为 3。

### 1.3 编辑与删除
- [ ] 编辑 OWNER-1 的姓名为 `张三-改`。
**预期 UI**：列表名字立刻刷新。
**校验 SQL**：`SELECT name FROM subjects WHERE code='OWNER-1';` → `张三-改`

- [ ] 删除 ORG-1（暂留 OWNER-1 和 DEPT-1，立项要用）。
**预期 UI**：列表少一行。
**校验 SQL**：`SELECT disable FROM subjects WHERE code='ORG-1';` → `1`

> **重要**：删除后，**重新创建一条** ORG-1，否则下面立项找不到 security 主体。

---

## 2 模版同步与查看

### 2.1 拉取模版
> 当前 V1 暂没有"模版库"独立 UI；走立项向导第一步会自动 list 本地缓存。如果第一次为空，先用 curl 触发同步：

```bash
curl -X POST http://127.0.0.1:3001/templates/sync \
  -H "Content-Type: application/json" \
  -d '{"code":"TPL-PRINT-BOOK","version":"V2.1"}'
```

**预期返回**：`{"success":true, "data":{...}}`

**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT dt.template_code, dt.template_version, dt.status,
  (SELECT COUNT(*) FROM template_stages WHERE template_id=dt.id) AS stages,
  (SELECT COUNT(*) FROM template_file_rules tfr JOIN template_stages ts ON ts.id=tfr.template_stage_id WHERE ts.template_id=dt.id) AS rules
FROM data_templates dt WHERE template_code='TPL-PRINT-BOOK' AND template_version='V2.1';"
```
预期：`status=active, stages=10, rules≥30`

---

## 3 立项向导（5 步）

### 3.1 快乐路径
- [ ] 左侧"数据业务项目" → 右上"新建项目立项"
- [ ] **Step 1 选模版**：选 `TPL-PRINT-BOOK V2.1`
**预期 UI**：右侧预览显示模版的环节列表与文件规则数。

- [ ] **Step 2 项目信息**：
  - project_name：`手动验证项目-001`
  - object_short_code：`MC-MAN`
  - 任务说明：`UI 手动验证`
- [ ] **Step 3 三主体 + 敏感等级**：
  - owner：OWNER-1
  - custodian：DEPT-1
  - security：ORG-1
  - 敏感等级：`important`（与模版一致）
- [ ] **Step 4 项目成员**：
  - 默认应当显示 1 行，subject=OWNER-1，权限默认 `read,write,receive,submit,archive,close`（必须含 close）
- [ ] **Step 5 确认**：勾选"立即激活" → 提交

**预期 UI**：跳转到项目列表，看到刚创建的项目，状态 chip 是 `active`，project_code 形如 `MC-MAN-2026-001`。

**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT project_code, project_name, status, sensitivity_level, owner_subject_id, template_code, template_version, project_root
FROM data_projects WHERE disable=0 ORDER BY id DESC LIMIT 1;"
```
预期：编码 `MC-MAN-{YYYY}-001`，status=active，sensitivity_level=important，project_root 非空。

```bash
sqlite3 $SCAN_DB "
SELECT COUNT(*) FROM project_stages WHERE project_id=(SELECT id FROM data_projects ORDER BY id DESC LIMIT 1);"
```
预期：`10`（10 个环节）。

```bash
sqlite3 $SCAN_DB "
SELECT data_state, COUNT(*) FROM file_versions
WHERE project_id=(SELECT id FROM data_projects ORDER BY id DESC LIMIT 1) AND lifecycle_status='planned'
GROUP BY data_state;"
```
预期：`input/process/output` 三种数据态都有 planned 行，总数 ≈ 30+。

### 3.2 错误用例：缺 close 权限
- [ ] 重复 3.1 步骤，但 Step 4 成员的权限去掉 `close`，提交。
**预期 UI**：错误提示"立项必须至少有一个成员具备 close 权限"。

### 3.3 错误用例：敏感等级低于模版
- [ ] 重复 3.1 步骤，Step 3 把敏感等级选为 `general`（模版基线 important）。
**预期 UI**：项目仍可创建，但实际 `sensitivity_level` 应当是 `important`（就高不就低）。
**校验 SQL**：`SELECT sensitivity_level FROM data_projects ORDER BY id DESC LIMIT 1;` → `important`

### 3.4 项目根目录已建立
- [ ] 检查文件系统：
```bash
ls -la /tmp/scan-root/MC-MAN-*/
```
预期：每个环节有一个 `stages/<stage_code>/{input,process,output}` 子目录树。

---

## 4 项目工作台（环节 + 三态 + 详情）

### 4.1 进入工作台
- [ ] 项目列表 → 点 3.1 创建的项目所在行 → 点"工作台"
**预期 UI**：
- 顶部 header：项目编码 + 名称 + 模版编码 + 敏感等级 chip + project_root
- 右上有"结项归档"按钮（warning 色）
- 左栏列出 10 个工作环节
- 中栏（选中环节时）按 `输入 / 过程 / 产出` 三态分组
- 右栏（选中文件时）显示详情

### 4.2 上传输入文件 IN-001
- [ ] 左栏点 `MZ-SG 收稿`
- [ ] 中栏 → 输入区 → `IN-001 客户原稿`卡片 → 右侧"上传"
- [ ] 选一个 PDF（IN-001 只允许 PDF）→ 提交
**预期 UI**：
- 卡片状态 chip 从 `planned` → `registered`
- 中栏点该卡片，右栏详情显示：
  - 实体文件路径（在 project_root/.../stages/MZ-SG/input/...）
  - SHA-256 checksum 前 16 字符
  - 文件大小
  - 底账编号 LDG-{YYYYMM}-...
  - 生命周期事件 timeline 至少 1 条 register 事件

**校验 SQL**：
```bash
PID=$(sqlite3 $SCAN_DB "SELECT id FROM data_projects WHERE project_code LIKE 'MC-MAN-%' AND disable=0 ORDER BY id DESC LIMIT 1;")
sqlite3 $SCAN_DB "
SELECT fv.file_version_code, fv.lifecycle_status, fv.checksum, fv.file_size, al.lifecycle_status, al.ledger_code
FROM file_versions fv
LEFT JOIN asset_ledgers al ON al.file_version_id = fv.id
WHERE fv.project_id=$PID AND fv.local_code='IN-001' AND fv.data_state='input';"
```
预期：lifecycle=registered，checksum 64 chars，ledger lifecycle=registered。

### 4.3 错误用例：文件类型不匹配
- [ ] 在另一个 IN-002（如果允许 DOCX）那里上传一个 .pdf。
- [ ] 或者，再次去刚刚 4.2 已 registered 的 IN-001 卡片，点上传按钮（如果还在）。
**预期 UI**：
- 错误："文件类型 ... 不在允许列表" 或 "状态为 registered，不能再次上传"。

### 4.4 派生过程文件
- [ ] 中栏切到 MZ-PB 排版环节 → 输入区 → 在已 registered 的 IN-001 卡片上点"派生"
- [ ] 弹窗里：
  - 目标环节默认 `MZ-PB`
  - 目标规则选 `PRC-001`（process 数据态）
  - 上传 PSD 文件
**预期 UI**：
- 派生成功，过程区出现一个新卡片，状态 registered
- 右栏详情"来源链路"显示 `派生自 fv #{原 IN-001 的 id}`

**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT fv.file_version_code, fv.data_state, fv.source_file_version_id, src.file_version_code AS source_code
FROM file_versions fv LEFT JOIN file_versions src ON src.id = fv.source_file_version_id
WHERE fv.project_id=$PID AND fv.local_code='PRC-001';"
```
预期：source_code 是 `MC-MAN-...-MZ-SG-IN-001`。

### 4.5 上传产出 + 提交
- [ ] MZ-PB 输出区 → OUT-001 排版完成稿 → 上传 PDF
- [ ] 同卡片点"提交"（仅 output+registered 状态可见）
**预期 UI**：
- 提交按钮消失
- 详情面板事件 timeline 增加 `提交产出 change` 事件
- 右栏详情上多出"已提交于 ..."

**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT fv.file_version_code, fv.lifecycle_status, fv.submitted_at, fv.submitted_by
FROM file_versions fv WHERE fv.project_id=$PID AND fv.local_code='OUT-001';"
```
预期：submitted_at 非空。

### 4.6 下游领取
- [ ] 切到 MZ-SH 审核环节 → 输入区 → 点"领取上游产出"按钮
- [ ] 弹窗里下拉选择刚刚提交的 OUT-001 → 选目标规则 IN-001 → 确认
**预期 UI**：
- 输入区出现新卡片 `IN-001`，状态 registered
- 右栏详情：
  - storage_uri 与上游一致（不复制）
  - 来源链路指向 `MC-MAN-...-MZ-PB-OUT-001`

**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT fv.file_version_code, fv.storage_uri = src.storage_uri AS same_storage,
       fv.source_file_version_id, src.file_version_code AS upstream
FROM file_versions fv JOIN file_versions src ON src.id = fv.source_file_version_id
WHERE fv.project_id=$PID AND fv.local_code='IN-001' AND fv.project_stage_id =
  (SELECT id FROM project_stages WHERE project_id=$PID AND stage_code='MZ-SH');"
```
预期：same_storage=1，upstream 形如 OUT-001。

### 4.7 幂等领取
- [ ] 重复 4.6（同一上游 → 同一下游规则）
**预期 UI**：要么提示"已领取"，要么静默成功（返回相同 fv id）。
**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT COUNT(*) FROM file_versions WHERE project_id=$PID AND project_stage_id=
  (SELECT id FROM project_stages WHERE project_id=$PID AND stage_code='MZ-SH')
  AND local_code='IN-001';"
```
预期：`1`（仍只有一条）。

---

## 5 资产标识底账

### 5.1 进入底账页
- [ ] 左侧"资产标识底账"
**预期 UI**：表格列出当前项目所有 ledger，状态 chip 区分（registered=success 绿色 / planned=灰色 / 等）。

### 5.2 五维筛选
- [ ] 项目筛选器选 `MC-MAN-{YYYY}-001`
**预期**：行数立即收窄到该项目所属。
- [ ] 状态筛选 `已入账`
**预期**：仅 registered 行显示。
- [ ] 关键词输入"客户原稿"
**预期**：仅资产名匹配的行。

### 5.3 详情 + 状态切换
- [ ] 点任意 registered 行的"详情"
**预期 UI**：弹窗显示资产编号、所属项目、环节、文件版本编码、敏感等级、当前存储、生命周期事件 timeline。
**预期"合法状态切换"按钮**：仅显示 `使用中` 和 `已封存`（registered 的合法去向）。

- [ ] 点"使用中" → 输入原因"投入工作"→ 确认
**预期 UI**：
- 状态 chip 变成 `in_use`
- timeline 多一条 `投入使用 use` 事件
- 按钮区现在显示 `已入账` 和 `已封存` 两个可选项（in_use 的合法去向）

**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT al.ledger_code, al.lifecycle_status, fv.lifecycle_status AS fv_status
FROM asset_ledgers al JOIN file_versions fv ON fv.id=al.file_version_id
WHERE al.project_code=(SELECT project_code FROM data_projects WHERE id=$PID)
  AND al.lifecycle_status='in_use';"
```
预期：底账 `in_use`，**且 fv 也是 `in_use`**（同步）。

### 5.4 错误用例：非法状态转换
- [ ] V1 中按钮已经过滤掉非法选项；如果直接发请求：

```bash
curl -X POST http://127.0.0.1:3001/ledgers/<id>/transition \
  -H "Content-Type: application/json" \
  -d '{"to_status":"planned"}'
```
**预期返回**：`{"success":false, "error":"不允许的状态转换：in_use → planned"}`

### 5.5 CSV 导出
- [ ] 右上角"导出 CSV"
**预期**：浏览器下载 `ledgers-{date}.csv`，UTF-8 BOM，Excel 打开正常显示中文。

---

## 6 安全策略与权限验证

### 6.1 安全策略已绑定
**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT fv.file_version_code, fv.data_state, fv.lifecycle_status, sp.policy_code, sp.sensitivity_level, sp.file_state
FROM file_versions fv LEFT JOIN security_policies sp ON sp.id = fv.security_policy_id
WHERE fv.project_id=$PID AND fv.lifecycle_status IN ('registered','in_use')
ORDER BY fv.file_version_code;"
```
预期：每行 `policy_code` 非空，`sensitivity_level` 为 important（项目级），`file_state` 是 personal_process / personal_final / dept_stage / dept_final 之一。

### 6.2 权限拒绝
> V1 在 wails 单用户模式下用宽松回退（项目内有人有该权限就放行），所以默认情况下所有操作都会通过。要触发拒绝需要：
> 1. 在 user_info 设置一个 user_name 与某 subject 严格匹配
> 2. 该 subject 在项目里没有目标权限

- [ ] 进 settings → 用户信息 → 把 user_name 改为 `OWNER-1`（已是 subject）
- [ ] 创建一个新项目，立项时把 OWNER-1 这个成员的权限只勾选 `read`，**另一**个 subject 给 close 权限（满足立项必填）
- [ ] 用 user_name=OWNER-1 去尝试上传文件
**预期 UI**：上传失败，错误提示"操作人 OWNER-1 在该项目内无 write 权限"。

---

## 7 项目结项与归档

### 7.1 预检失败：必填未传
- [ ] 创建一个新项目（不上传任何文件）
- [ ] 工作台点"结项归档"
**预期 UI**：弹窗显示
- error 类提示一堆 `REQUIRED_NOT_REGISTERED：必填文件版本 ... 尚未上传`
- "执行结项"按钮 disabled

### 7.2 预检通过（带 warning）
- [ ] 在 4.6 完成的项目（已上传 IN-001 / PRC-001 / OUT-001）回到工作台
- [ ] 把所有 required=1 的文件都依次上传（SQL 查 planned required=1 的 fv 列表）
- [ ] 点"结项归档"
**预期 UI**：弹窗显示"预检发现 N 个警告"（warnings 来自未提交的 output / 草稿 ledger）
- 勾选"强制结项" → "执行结项"按钮亮起

### 7.3 执行结项
- [ ] 点"执行结项"
**预期 UI**：弹窗变绿色提示
- manifest 路径：`/tmp/scan-root/MC-MAN-.../manifest.json`
- SHA-256 前 32 字符
- 文件 N · 底账 M · 事件 K
- 顶部按钮变成"上报 manage"

**校验文件**：
```bash
ls -la /tmp/scan-root/MC-MAN-*/manifest.json
cat /tmp/scan-root/MC-MAN-*/manifest.json | python3 -c "
import sys, json
m = json.load(sys.stdin)
print('schema:', m.get('schema'))
print('source_terminal:', m.get('source_terminal'))
print('project_code:', m['project']['project_code'])
print('stages:', len(m.get('stages', [])))
print('file_versions:', len(m.get('file_versions', [])))
print('ledgers:', len(m.get('ledgers', [])))
print('lifecycle_events:', len(m.get('lifecycle_events', [])))
print('stats:', m.get('stats'))
"
```

**校验 SQL**：
```bash
sqlite3 $SCAN_DB "
SELECT status, sync_status, sync_message, synced_at FROM data_projects WHERE id=$PID;"
```
预期：status=archived，sync_status=pending，sync_message 含 manifest sha256。

```bash
sqlite3 $SCAN_DB "
SELECT lifecycle_status, COUNT(*) FROM asset_ledgers WHERE project_code='MC-MAN-...' GROUP BY lifecycle_status;"
```
预期：除 planned 草稿外，其他都已 sealed。

### 7.4 上报 manage
- [ ] 顶部点"上报 manage"
**预期 UI**：toast 显示"上报成功"，按钮变成"已上报"。

**校验 SQL（manage 库）**：
```bash
sqlite3 data-asset-manage/data.db "
SELECT id, project_code, source_terminal, process_status, datetime(create_time,'localtime') AS at
FROM project_archive_uploads ORDER BY id DESC LIMIT 1;"
```
预期：project_code 匹配，source_terminal=`data-asset-scan`，process_status=`pending`。

**校验 SQL（scan 库）**：
```bash
sqlite3 $SCAN_DB "
SELECT sync_status, datetime(synced_at,'localtime') AS synced_at FROM data_projects WHERE id=$PID;"
```
预期：sync_status=success，synced_at 是刚刚的时间。

### 7.5 重复结项被拒
- [ ] 已 archived 项目再点"结项归档"
**预期 UI**：错误"项目已归档，不可重复结项"。

### 7.6 错误用例：必填仍 planned 时强制结项
- [ ] 新建一个项目，至少有 1 个 required=1 必填文件没上传
- [ ] 工作台 → 结项归档 → 弹窗显示 error issues
**预期 UI**：
- "执行结项"按钮 disabled，无法点击。

---

## 8 数据一致性 SQL 校验

完成上述所有用例后，跑：

```bash
bash data-asset-scan/verification/v1/run_invariants.sh
```

**预期**：脚本最后输出 `✓ 全部数据一致性检查通过`。如果有任何 `✗` 项，按编号去排查（每条断言都注释了"为什么期望 0"）。

---

## 9 巡视检查表（用例之间的回归）

完成上述全部后，最后逐项确认：

- [ ] manage 模版仍 active，未被 scan 改动
- [ ] scan 项目列表至少 3 个项目（成功立项 + 错误用例 + 已归档）
- [ ] 已归档项目状态显示 archived，sync_status 显示 success
- [ ] 资产标识底账页能查到 archived 项目的 ledger，状态都已 sealed
- [ ] 工作台对 archived 项目仍能进入查看历史，但不能再上传 / 派生 / 提交（按钮全消失或 disabled）
- [ ] 三主体管理页的 OWNER-1 等不被允许删除（如果还在被项目引用）

---

## 附：V1 验证完成判定

满足以下全部条件视为 V1 验证通过：
1. 第 1-7 章用例全部 ✓ 完成
2. 第 8 章 SQL 校验脚本全部通过
3. 第 9 章回归检查全部 ✓
4. 自动化测试套件 `go test ./...` 全部通过
5. `verification/v1/smoke.sh` 端到端冒烟脚本全部通过
