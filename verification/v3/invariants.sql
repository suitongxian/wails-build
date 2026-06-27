-- ============================================================================
-- 数据业务模版 V3 — MVP 完整性 SQL 校验脚本
--
-- 用法：
--   sqlite3 ~/.local/share/data-asset-scan/data.db < verification/v3/invariants.sql
-- 一键执行：
--   bash verification/v3/run_invariants.sh
--
-- 每条断言以 `-- EXPECT: 0` 注释；非 0 表示文档不变式被破坏。
-- ============================================================================

.headers on
.mode column
.print
.print ============================================================
.print V3-3 §7.3 项目环节状态机
.print ============================================================

-- EXPECT: 0 — project_stages.status 必须是文档 §7.3 列出的 4 个值之一
SELECT 'STAGE_INVALID_STATUS' AS check_name, COUNT(*) AS bad
FROM project_stages
WHERE disable = 0
  AND status NOT IN ('pending', 'running', 'completed', 'skipped');

.print
.print ============================================================
.print V3-4 §7.7 权限动作
.print ============================================================

-- EXPECT: 0 — project_members.permission_actions 解析后每个动作都应在文档 9 个之内
-- （V3 不强制 schema 约束，但通过这个查询能定位污染数据）
SELECT 'MEMBER_HAS_UNKNOWN_PERM' AS check_name, COUNT(*) AS bad
FROM project_members pm
WHERE pm.disable = 0
  AND pm.permission_actions IS NOT NULL
  AND (
    pm.permission_actions LIKE '%"delete"%'    -- 旧 V1 可能用过 delete 而不是 destroy
    OR pm.permission_actions LIKE '%"create"%'
    OR pm.permission_actions LIKE '%"modify"%'
    OR pm.permission_actions LIKE '%"execute"%'
  );

.print
.print ============================================================
.print V3-5 §11 audit_logs 完整性
.print ============================================================

-- EXPECT: 0 — audit_logs 必填字段 actor_id / action / target_type 非空
SELECT 'AUDIT_MISSING_REQUIRED' AS check_name, COUNT(*) AS bad
FROM audit_logs
WHERE actor_id IS NULL OR actor_id = ''
   OR action IS NULL OR action = ''
   OR target_type IS NULL OR target_type = '';

-- EXPECT: 0 — actor_user_id 非空时应指向真实 users 行
SELECT 'AUDIT_ACTOR_USER_FK_BROKEN' AS check_name, COUNT(*) AS bad
FROM audit_logs al
WHERE al.actor_user_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = al.actor_user_id);

.print
.print ============================================================
.print V3-5 §11.1 必须审计的操作覆盖度
.print ============================================================

-- 有 data_projects 但没 project_create / project_activate 审计 → 漏审计
SELECT 'PROJECT_CREATE_NOT_AUDITED' AS check_name, COUNT(*) AS bad
FROM data_projects dp
WHERE dp.disable = 0
  AND dp.create_time > datetime('now', '-30 days')  -- 只看最近一个月的
  AND NOT EXISTS (
    SELECT 1 FROM audit_logs al
    WHERE al.target_type = 'project'
      AND al.target_id = dp.id
      AND al.action IN ('project_create', 'project_activate')
  );

.print
.print ============================================================
.print V3-8 §8.2 项目状态机
.print ============================================================

-- EXPECT: 0 — data_projects.status 必须是已定义枚举
SELECT 'PROJECT_INVALID_STATUS' AS check_name, COUNT(*) AS bad
FROM data_projects
WHERE disable = 0
  AND status NOT IN ('draft', 'active', 'archived', 'cancelled');

.print
.print ============================================================
.print §15.3 七条 MVP 验收标准
.print ============================================================

-- 1. 一件一号：file_version_code 唯一
SELECT 'ONE_CODE' AS check_name,
  (SELECT COUNT(*) FROM file_versions WHERE disable = 0)
  - (SELECT COUNT(DISTINCT file_version_code) FROM file_versions WHERE disable = 0)
  AS bad;

-- 2. 一件一账：registered fv 必有底账
SELECT 'ONE_LEDGER' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0 AND fv.lifecycle_status = 'registered'
  AND NOT EXISTS (SELECT 1 FROM asset_ledgers al WHERE al.file_version_id = fv.id);

-- 3. 一件一责：底账三主体非空
SELECT 'ONE_RESPONSIBILITY' AS check_name, COUNT(*) AS bad
FROM asset_ledgers
WHERE disable = 0
  AND (owner_subject_id IS NULL OR custodian_subject_id IS NULL OR security_subject_id IS NULL);

-- 4. 版本锁定：data_projects.template_version 必填
SELECT 'TEMPLATE_VERSION_LOCKED' AS check_name, COUNT(*) AS bad
FROM data_projects
WHERE disable = 0
  AND (template_version IS NULL OR template_version = '');

.print
.print ============================================================
.print 整体完整性
.print ============================================================

-- §17.5 生命周期事件不可物理删除：检查 ledger 状态变化但没事件
SELECT 'LEDGER_WITHOUT_EVENT' AS check_name, COUNT(*) AS bad
FROM asset_ledgers al
WHERE al.disable = 0 AND al.lifecycle_status != 'planned'
  AND NOT EXISTS (SELECT 1 FROM lifecycle_events le WHERE le.ledger_id = al.id);

.print
.print ============================================================
.print Done. 所有 bad 列都应为 0。
.print ============================================================
