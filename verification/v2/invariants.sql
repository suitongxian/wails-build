-- ============================================================================
-- 数据业务模版 V2 — 身份/权限/审计/过户数据一致性 SQL 校验脚本
--
-- 用法：
--   sqlite3 ~/.local/share/data-asset-scan/data.db < verification/v2/invariants.sql
--
-- 每条断言以 `-- EXPECT: <预期>` 注释开头。所有 EXPECT 0 的项目都应当返回 0；
-- 非 0 表示该 V2 不变式被破坏，按 check_name 定位问题。
-- ============================================================================

.headers on
.mode column
.print
.print ============================================================
.print 1) V2-1 users 表
.print ============================================================

-- EXPECT: 0 — users.username 唯一
SELECT 'USERS_USERNAME_DUP' AS check_name, COUNT(*) - COUNT(DISTINCT username) AS bad
FROM users WHERE disable = 0;

-- EXPECT: 0 — 每个 active user_info 都应该在 users 表里有对应行（V2-1 同步保证）
SELECT 'USER_INFO_NOT_SYNCED' AS check_name, COUNT(*) AS bad
FROM user_info ui
WHERE ui.disable = 0
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.username = ui.user_name AND u.disable = 0);

.print
.print ============================================================
.print 2) V2-2 project_members.user_id
.print ============================================================

-- EXPECT: 0 — project_members.user_id 若不为 NULL，必须指向存在且未删除的 users.id
SELECT 'PM_USER_FK_BROKEN' AS check_name, COUNT(*) AS bad
FROM project_members pm
WHERE pm.disable = 0 AND pm.user_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = pm.user_id AND u.disable = 0);

-- EXPECT: 0 — 同一项目内一个 user_id 不应有多条成员记录（同 role_code 时）
SELECT 'PM_DUPLICATE_USER_ROLE' AS check_name, COUNT(*) AS bad
FROM (
  SELECT project_id, user_id, role_code, COUNT(*) c
  FROM project_members
  WHERE disable = 0 AND user_id IS NOT NULL
  GROUP BY project_id, user_id, role_code
  HAVING c > 1
);

.print
.print ============================================================
.print 3) V2-3 立项人自动登记
.print ============================================================

-- EXPECT: 0 — V2 创建的 data_project（有 created_by_user_id）必须在 project_members 里有
--           一条对应 user_id 的成员且 permission_actions 含 'close'
SELECT 'INSTANTIATOR_NOT_ENROLLED' AS check_name, COUNT(*) AS bad
FROM data_projects dp
WHERE dp.disable = 0 AND dp.created_by_user_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = dp.id AND pm.user_id = dp.created_by_user_id
      AND pm.permission_actions LIKE '%"close"%'
  );

.print
.print ============================================================
.print 4) V2-4 审计 user_id 完整性
.print ============================================================

-- EXPECT: 0 — 已带 V1 created_by 字符串、且能在 users 表里找到的 data_project，
--           created_by_user_id 应当已被回填（V2-4 迁移负责）
SELECT 'PROJECT_AUDIT_UID_NOT_BACKFILLED' AS check_name, COUNT(*) AS bad
FROM data_projects dp
WHERE dp.disable = 0
  AND dp.created_by IS NOT NULL AND dp.created_by != ''
  AND dp.created_by_user_id IS NULL
  AND EXISTS (SELECT 1 FROM users u WHERE u.username = dp.created_by AND u.disable = 0);

-- EXPECT: 0 — file_versions 同上
SELECT 'FV_AUDIT_UID_NOT_BACKFILLED' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.created_by IS NOT NULL AND fv.created_by != ''
  AND fv.created_by_user_id IS NULL
  AND EXISTS (SELECT 1 FROM users u WHERE u.username = fv.created_by AND u.disable = 0);

-- EXPECT: 0 — submitted_by 同上
SELECT 'FV_SUBMITTED_UID_NOT_BACKFILLED' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.submitted_by IS NOT NULL AND fv.submitted_by != ''
  AND fv.submitted_by_user_id IS NULL
  AND EXISTS (SELECT 1 FROM users u WHERE u.username = fv.submitted_by AND u.disable = 0);

-- EXPECT: 0 — lifecycle_events 同上
SELECT 'LE_OPERATOR_UID_NOT_BACKFILLED' AS check_name, COUNT(*) AS bad
FROM lifecycle_events le
WHERE le.operator_id IS NOT NULL AND le.operator_id != ''
  AND le.operator_user_id IS NULL
  AND EXISTS (SELECT 1 FROM users u WHERE u.username = le.operator_id AND u.disable = 0);

.print
.print ============================================================
.print 5) V2-6 三主体独立性
.print ============================================================

-- EXPECT: 0 — subjects 表不应出现 user_id 列（schema 守护）
SELECT 'SUBJECTS_HAS_USER_ID' AS check_name, COUNT(*) AS bad
FROM pragma_table_info('subjects') WHERE name = 'user_id';

.print
.print ============================================================
.print 6) V2-7 过户事件
.print ============================================================

-- EXPECT: 0 — handover 事件必须含 from_subject_id 与 to_subject_id 且两者不同
SELECT 'HANDOVER_MALFORMED' AS check_name, COUNT(*) AS bad
FROM lifecycle_events
WHERE event_type = 'handover'
  AND (from_subject_id IS NULL OR to_subject_id IS NULL OR from_subject_id = to_subject_id);

.print
.print ============================================================
.print Done. 所有 bad 列都应为 0。
.print ============================================================
