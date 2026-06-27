# V2 真实环境联调清单

按顺序执行；每一步都有"做什么 / 看什么 / SQL 探测"三段。所有 SQL 在终端跑：

```bash
sqlite3 ~/.local/share/data-asset-scan/data.db
```

Mac 路径如上，Linux 也一样；Windows 在 `%APPDATA%\data-asset-scan\data.db`。

如果想从干净环境开始：`bash verification/v1/reset_projects.sh` 清掉已有项目数据（保留模版、用户、主体）。

---

## 0. 准备

- 启动 manage 服务：`cd data-asset-manage && yarn dev`（默认 3000）
- 启动 scan 桌面端：`wails dev` 或运行已构建好的可执行（如 `/tmp/scan-v2-bin`）
- scan 启动后进 "设置" 确认 manage 地址（典型 `http://127.0.0.1:3000`）

**首次进入应自动迁移**：V1 → V2 schema。验证 schema 已升：

```sql
.schema users
.schema project_members        -- 应当看到 user_id 列
.schema data_projects          -- 应当看到 created_by_user_id 列
.schema file_versions          -- 应当看到 created_by_user_id / submitted_by_user_id
.schema lifecycle_events       -- 应当看到 operator_user_id
```

---

## 1. V2-1：填机主信息后，users 表同步

**做**：右上角"机主信息"对话框，填好 user_name / company_name / department，保存。

**看**：

```sql
SELECT id, user_name FROM user_info WHERE disable = 0;       -- 拿到 user_info 行
SELECT id, username, display_name, department FROM users;    -- 应当也有一行，username == 上面 user_name
```

**改名复测**：再次打开对话框，改 department，保存。

```sql
SELECT id, username, department, datetime(update_time, 'localtime') FROM users;
```

`department` 应该是新值，`update_time` 应当跟随更新。

✅ 通过：users 表行随 user_info 同步增/改。

---

## 2. V2-3：立项时自动登记为项目负责人

**做**：进入"数据业务项目" → 新建项目 → 选模版（如 TPL-PRINT-BOOK）→ 填项目信息 → 选三主体 → **跳到第 4 步"安全与成员"时**：

**看 UI**：
- ✅ 顶部应有绿色提示框："**<your_user_name>**（部门）将自动登记为项目负责人..."
- ✅ 成员列表**默认是空的**（没有预置一行需要你填）
- ✅ 即使不添加任何成员，下一步 / 提交按钮可点
- ✅ 如果未填机主信息，提示框应变成黄色警告 + 提交按钮禁用

**做**：直接提交（不加成员）。

**SQL 探测**：

```sql
SELECT id, project_code, project_name, created_by, created_by_user_id
FROM data_projects ORDER BY id DESC LIMIT 1;
-- 期望：created_by = 你的 username；created_by_user_id = users.id（不为 NULL）

SELECT pm.id, pm.user_id, pm.subject_id, pm.role_code, pm.permission_actions
FROM project_members pm
WHERE pm.project_id = (SELECT MAX(id) FROM data_projects);
-- 期望：仅 1 条，user_id = 你的 users.id，subject_id = 0，role_code = '项目负责人'
-- permission_actions 含 close / share / archive
```

✅ 通过：立项后无需手填，自己就是项目负责人。

---

## 3. V2-4：操作审计字段落到 user_id

**做**：在新建的项目里：
1. 找到 MZ-SG（收稿登记）→ IN-001（客户原稿），点上传，选一个 PDF
2. 找到对应的 OUT-001（收稿凭证），上传一个 PDF
3. 点该 output 文件的"提交产出"

**SQL 探测**：

```sql
-- 上传后 file_versions 的审计字段
SELECT id, display_name, lifecycle_status, created_by, created_by_user_id, submitted_by, submitted_by_user_id
FROM file_versions
WHERE project_id = (SELECT MAX(id) FROM data_projects)
ORDER BY id;
-- 期望：上传的 input/output fv 都有 created_by_user_id（不为 NULL）
-- 提交过的 output fv 应有 submitted_by_user_id

-- 事件流
SELECT id, event_type, event_name, operator_id, operator_user_id
FROM lifecycle_events
WHERE file_version_id IN (
  SELECT id FROM file_versions WHERE project_id = (SELECT MAX(id) FROM data_projects)
)
ORDER BY id;
-- 期望：每条事件的 operator_user_id 都不为 NULL（与 operator_id 字符串并存）
```

✅ 通过：所有写事件都带 user_id。

---

## 4. V2-5：严格权限

V2-5 改的是"陌生身份不再放行"。直接在 UI 不容易构造，用 SQL 模拟一个 share 权限不足的成员，跑 HTTP 校验：

**做**：

```sql
-- 在当前项目里加一个 share 权限不足的"陌生 user"
INSERT INTO users (username, display_name, status, create_time, update_time, disable)
VALUES ('NO_SHARE_USER', '无 share 权限', 'active', datetime('now'), datetime('now'), 0);

-- 把它加进项目，只给 read+write（不给 share）
INSERT INTO project_members (project_id, user_id, subject_id, role_code, stage_ids, permission_actions, create_time, update_time, disable)
SELECT MAX(id), (SELECT id FROM users WHERE username='NO_SHARE_USER'), 0, 'EDITOR', '[]', '["read","write"]', datetime('now'), datetime('now'), 0
FROM data_projects;

-- 把 user_info 切到这个新用户（模拟它登录）
UPDATE user_info SET disable = 1;
INSERT INTO user_info (company_name, user_name, department, ip, mac_address, create_time, update_time, disable)
VALUES ('测试单位', 'NO_SHARE_USER', '测试部门', '127.0.0.1', '00:00:00:00:00:00', datetime('now'), datetime('now'), 0);
```

**现在 scan 终端 UI 上你的身份变成 NO_SHARE_USER**（可能需要重启 scan 或者关闭再开机主信息对话框让缓存失效）。

**看**：进项目，找一个 registered 的底账，**底账详情应当看不见"过户"按钮**（前端有 share 检查）；如果它出现了，点"过户保管主体"应当被 manage / scan 拒。

**严格验证**（curl 直接打 API）：

```bash
# 找一个 ledger id
LID=$(sqlite3 ~/.local/share/data-asset-scan/data.db "SELECT id FROM asset_ledgers WHERE lifecycle_status='registered' LIMIT 1")

curl -s -X POST http://127.0.0.1:3001/ledgers/$LID/handover \
  -H 'Content-Type: application/json' \
  -d '{"subject_kind":"custodian","to_subject_id":1,"reason":"测试"}'
# 期望：HTTP 403, body 含 PERMISSION_DENIED + "NO_SHARE_USER 在该项目内无 share 权限"
```

**收尾恢复你自己**：

```sql
UPDATE user_info SET disable = 0 WHERE user_name != 'NO_SHARE_USER';
UPDATE user_info SET disable = 1 WHERE user_name = 'NO_SHARE_USER';
```

✅ 通过：陌生身份在严格模式下被准确拒。

---

## 5. V2-7：过户

**做**：进项目工作台 → 数据资产标识底账 → 点一条 registered 状态的底账 → "三主体过户"区块 → 选"过户保管主体"。

**看 UI**：
- ✅ 对话框显示"当前 保管主体：xxx"
- ✅ 下拉选项里自动排除当前主体
- ✅ 必填原因；不填时确认按钮禁用
- ✅ 选目标 → 填原因 → 确认 → 提示"过户保管主体成功"

**SQL 探测**：

```sql
-- 底账的 custodian_subject_id 应该被更新
SELECT id, custodian_subject_id, lifecycle_status FROM asset_ledgers WHERE id = <你刚过户的底账 id>;

-- 应当多出一条 handover 事件
SELECT id, event_type, event_name, from_subject_id, to_subject_id, operator_id, operator_user_id, reason
FROM lifecycle_events
WHERE ledger_id = <你刚过户的底账 id> AND event_type = 'handover';
-- 期望：恰好一条，from=旧主体，to=新主体，operator_user_id=你的 user.id
```

✅ 通过：过户落库 + 事件流可见。

---

## 6. 结项归档：V2 字段全部进 manifest

**做**：把所有 required 文件上传完，回项目工作台点"结项"，再点"归档移交"上报 manage。

**看 scan 端 manifest**：

```bash
PROJECT_ROOT=$(sqlite3 ~/.local/share/data-asset-scan/data.db "SELECT project_root FROM data_projects WHERE id = (SELECT MAX(id) FROM data_projects)")
cat "$PROJECT_ROOT/manifest.json" | python3 -m json.tool | grep -E "created_by_user_id|operator_user_id|submitted_by_user_id|user_id" | head -20
# 期望：能看到 V2 字段在 manifest 各处出现
```

**看 manage 端**：

```bash
# manage 库
sqlite3 <manage 的 data.db 路径> << 'EOF'
SELECT id, project_code, received_at, process_status,
  json_extract(payload, '$.project.created_by_user_id') AS created_uid,
  (SELECT COUNT(*) FROM json_each(json_extract(payload, '$.lifecycle_events'))
    WHERE json_extract(json_each.value, '$.operator_user_id') IS NOT NULL) AS ev_with_uid
FROM project_archive_uploads ORDER BY received_at DESC LIMIT 1;
EOF
# 期望：created_uid 非 NULL，ev_with_uid > 0
```

✅ 通过：归档全链路审计 user_id 都到 manage 了。

---

## 失败时怎么报

随便哪一步对不上 → 把：
1. 该步骤的"看"清单里没通过的那条
2. 配套 SQL 输出
3. scan/manage 控制台日志相关片段

贴回来，我看完给修。
