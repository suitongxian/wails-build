# V3 真实环境联调清单

按顺序执行 10 步，对应 V3-1 到 V3-10。每步含"做什么 / 看什么 / SQL 探测"。

## 启动

```bash
# 1. 启动 manage 服务（test-template 分支）
cd /Users/suitongxian/.../data-asset-manage
git checkout test-template && git pull
yarn dev   # 默认 :3000

# 2. 启动 scan（go-test-template 分支）
cd /Users/suitongxian/.../data-asset-scan
git checkout go-test-template && git pull
wails dev  # 桌面端会自动起，HTTP 在 :3001

# 路径
SCAN_DB=~/.local/share/data-asset-scan/data.db
MANAGE_DB=<wherever-manage-stores-data.db>
```

如果想完全干净测试：`bash verification/v1/reset_projects.sh`（清项目数据，保留模版/用户/主体）。

---

## V3-1：模版复制 / 导入 / 导出（§7.1，manage 端）

**做（manage UI）**：
1. 浏览器打开 manage `http://localhost:3000/templates`
2. 列表里找 `TPL-PRINT-BOOK V2.1`：
   - ✅ 行尾应有 3 个按钮：**详情 / 导出 / 复制**
   - ✅ 顶部应有 **导入** 按钮
3. 点 **导出** → 下载 `TPL-PRINT-BOOK-V2.1.json`
4. 用文本编辑器打开，确认包含：`template / stages / fileRules / securityDefaults / permissionDefaults`
5. 点 **复制** → 弹窗：
   - 新编码：`TPL-PRINT-BOOK-CUSTOM`
   - 新版本：`V1.0`
   - 新名称：`定制版`
   - 确认 → 提示"复制成功"，列表新增一行 status=draft
6. 点 **导入** → 选刚才下载的 JSON：
   - ✅ 预览：`TPL-PRINT-BOOK V2.1 · 10 环节 · 22 文件规则`
   - 修改下载的 JSON：把 `template.template_code` 改成 `TPL-IMPORT-TEST` 保存重新选
   - 确认导入 → 列表新增 `TPL-IMPORT-TEST V2.1` status=draft

**SQL 探测（manage DB）**：

```sql
SELECT template_code, template_version, status, source_template_id
FROM data_templates
WHERE template_code IN ('TPL-PRINT-BOOK-CUSTOM', 'TPL-IMPORT-TEST')
ORDER BY id DESC;
-- 期望两行：
-- TPL-PRINT-BOOK-CUSTOM | V1.0 | draft | <源模版id>
-- TPL-IMPORT-TEST      | V2.1 | draft | NULL
```

✅ 通过：3 个 API + UI 全部可用。

---

## V3-2：模版版本升级（§7.1 + §17.3，manage 端）

**做**：
1. 进 `TPL-PRINT-BOOK V2.1` 详情页
2. 顶部应多一个 **升级版本** 按钮（仅 active/deprecated 状态显示）
3. 点击 → 弹窗，默认建议 `V2.2`
4. 确认 → 跳转到新 draft 详情页

**SQL 探测**：

```sql
SELECT template_code, template_version, status, source_template_id
FROM data_templates
WHERE template_code = 'TPL-PRINT-BOOK'
ORDER BY id;
-- 期望：
-- TPL-PRINT-BOOK | V2.1 | active     | NULL
-- TPL-PRINT-BOOK | V2.2 | draft      | <V2.1 的 id>
```

老 V2.1 不动；新 V2.2 是 draft 副本，环节 + 规则结构完整继承。

✅ 通过：§17.3"发布后必须新版本"机制落地。

---

## V3-3：项目环节状态机（§7.3，scan 端）

**做（scan UI）**：
1. 进任意一个 active 项目工作台
2. 左侧环节列表，每行下方应有两个 chip：
   - **官方状态**（待办/进行中/已完成/已跳过）
   - 可能的**派生进度**（V1 旧逻辑，仅当不同时显示）
3. 鼠标 hover 环节行末尾，看到 `...` 菜单
4. 点击 `...` → 应有"切换到「进行中」"和"切换到「已跳过」"两个选项
5. 选"切换到「进行中」"
6. 选"切换到「已完成」"

**看**：
- ✅ 每次切换后 chip 文案立刻更新
- ✅ 已完成状态下，`...` 菜单不再出现（终态）
- ✅ pending 状态下没有"切换到完成"（必须先 running）

**SQL 探测**：

```sql
SELECT id, stage_code, stage_name, status FROM project_stages WHERE project_id = <你的项目id>;
-- 期望某环节 status 从 'pending' 变成 'running' 再变成 'completed'
```

试错误转换（直接 SQL 测）：

```bash
# 模拟非法转换 completed → running
curl -X POST http://127.0.0.1:3001/projects/<id>/stages/<stage_id>/status \
  -H 'Content-Type: application/json' -d '{"to_status":"running"}'
# 期望：success=false，error 含"不允许从 completed 转换到 running"
```

✅ 通过：状态机受控，非法转换被拒。

---

## V3-4：9 权限常量（§7.7，scan 端）

**SQL 探测**：

```sql
-- 立项时随便保存的 permission_actions 应当只能用 9 个动作之一
SELECT DISTINCT permission_actions FROM project_members LIMIT 10;
-- 检查里面字符串都属于 {read, write, receive, upload, submit, share, archive, close, destroy}
```

**curl 验证**（V3-7 测过的同样路径）：

```bash
curl http://127.0.0.1:3001/projects/<id>/members | python3 -m json.tool | grep -i permission
```

✅ 通过：9 个常量齐备。

---

## V3-5：audit_logs 模块级审计（§11，scan 端）

**做（scan UI）**：
1. 立项一个新项目（→ 落 AuditProjectCreate / AuditProjectActivate）
2. 进项目工作台，点"结项"按钮（→ AuditProjectClose）
3. 进底账列表，点"导出 XLSX"按钮（→ AuditExportLedger）

**新端点验证**：

```bash
# 列举所有审计
curl 'http://127.0.0.1:3001/audit-logs?limit=20' | python3 -m json.tool | head -50

# 按 target 查
curl 'http://127.0.0.1:3001/audit-logs/target?target_type=project&target_id=<id>' \
  | python3 -m json.tool
```

**SQL 探测**：

```sql
SELECT id, action, target_type, target_code, actor_id, datetime(create_time, 'localtime') AS ts
FROM audit_logs
ORDER BY id DESC LIMIT 20;
-- 期望：
-- 立项后看到 action='project_create' 或 'project_activate'
-- 结项后看到 action='project_close'
-- 导出后看到 action='export_ledger'
```

✅ 通过：每个关键操作都有审计落地。

---

## V3-6：StorageAdapter 接口（§7.8）

V3-6 是纯结构性重构，**没有用户可见行为变化**——V1 的所有文件操作（项目目录创建 / 上传 / checksum / 归档）都正常即视为通过。如果你立项后看到项目目录结构正常生成、文件上传后能 SHA-256 校验，V3-6 就 OK。

**额外验证 moveFile / deleteFile**（新加 API 暴露在 storage 包里，HTTP 层未暴露，因为 §17.6 文档严禁直接对外）：

```bash
cd /Users/suitongxian/.../data-asset-scan
export PATH=$PATH:/usr/local/go/bin
go test ./internal/storage/ -v -count=1
# 期望：9/9 测试用例通过
```

✅ 通过：接口就绪 + 默认实现稳定。

---

## V3-7：AI 辅助 4 Port（§7.9）

V3-7 是接口预留 + NoOp 默认实现，**无 UI 可见行为**。

```bash
go test ./internal/ai/ -v -count=1
# 期望：TestNoOpAdapter_AllPortsCallable + TestNoOpAdapter_ImplementsAllPorts 都过
```

✅ 通过：未来加 AI 实现时不用动业务代码。

---

## V3-8：activate / cancel / CSV / JSON 导出（§8 + §7.5）

### 8a. 项目激活（draft → active）

如果你以前从来没遇到过 draft 项目（V1 立项默认 activate=true 直接 active），用 SQL 制造一个：

```sql
-- 找一个 active 项目改回 draft 模拟
UPDATE data_projects SET status = 'draft' WHERE id = <最近一个项目id>;
```

```bash
curl -X POST http://127.0.0.1:3001/projects/<id>/activate | python3 -m json.tool
# 期望：status=active
```

### 8b. 项目取消

```bash
curl -X POST http://127.0.0.1:3001/projects/<id>/cancel \
  -H 'Content-Type: application/json' -d '{"reason":"业务变更"}' | python3 -m json.tool
# 期望：status=cancelled
```

### 8c. 底账 CSV 导出

```bash
curl -OJ 'http://127.0.0.1:3001/ledgers/export.csv?project_code=<your-code>'
# 期望：下载到 ledgers.csv，UTF-8 BOM + 9 列
cat ledgers.csv | head -3
```

### 8d. 底账 JSON 导出

```bash
curl -OJ 'http://127.0.0.1:3001/ledgers/export.json?project_code=<your-code>'
cat ledgers.json | python3 -m json.tool | head -20
# 期望：{"count": N, "ledgers": [...]}
```

**审计验证**：上述 8c/8d 应当在 audit_logs 里各落一条 `export_ledger` 记录：

```sql
SELECT action, target_code, message FROM audit_logs WHERE action = 'export_ledger' ORDER BY id DESC LIMIT 5;
```

✅ 通过：4 个端点全部可用 + 审计落库。

---

## V3-9：项目工作台权限/策略面板（§9.2，scan 端）

**做**：进任意项目工作台，选中一个文件版本（左→中→点击一个 fv）。

**看右侧面板**：自上而下应有 6 块：
1. ✅ 文件版本基本信息
2. ✅ 实体文件（storage_uri / SHA-256 / 大小）
3. ✅ 底账（编号 / 状态 / 标识方式）
4. ✅ 来源链路（派生自...）
5. ✅ **项目权限矩阵**（新加）：列出每个 member 的 role + 9 权限 chip
6. ✅ **安全策略**（新加）：项目敏感等级 + 文件版本 security_policy_id
7. ✅ 生命周期事件 timeline

如果第 5、6 块没出现，可能是 fv 没选中（点击文件版本一行）。

✅ 通过：§9.2 右侧面板 5 项内容齐备（底账 + 来源 + 权限 + 策略 + 事件）。

---

## V3-10：§15.3 七条验收测试（自动）

```bash
cd /Users/suitongxian/.../data-asset-scan
export PATH=$PATH:/usr/local/go/bin
go test ./internal/repository/ -run 'TestAcceptance' -v -count=1
# 期望：7+2 用例全过
#   TestAcceptance_TemplateReusable
#   TestAcceptance_VersionLocked
#   TestAcceptance_OneFileVersionOneCode
#   TestAcceptance_OneFileVersionOneLedger
#   TestAcceptance_OneFileVersionOneSourceChain
#   TestAcceptance_OneLedgerThreeSubjects
#   TestAcceptance_OneFileVersionOneDisposition
#   TestAcceptance_AuditLogsCoverV3Operations
#   TestAcceptance_TemplateAuditActionsExist
```

✅ 通过：自动覆盖文档 §15.3 全部 7 条验收标准。

---

## 11. 不变式校验（自动兜底）

跑完上面 10 步，最后用 SQL 脚本一键扫描数据完整性：

```bash
bash verification/v3/run_invariants.sh
# 期望：所有 ✅，无 ❌
```

---

## 失败时怎么报

任何一步对不上：

1. 该步骤的"看"清单里没通过的那条
2. 配套 SQL 输出
3. scan / manage 控制台日志相关片段

贴回来，按 commit hash 定位修复。
