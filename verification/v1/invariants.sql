-- ============================================================================
-- 数据业务模版 V1 — 数据一致性 SQL 校验脚本
--
-- 用法：
--   sqlite3 /path/to/scan.db < verification/v1/invariants.sql
--
-- 每条断言以 `-- EXPECT: <预期>` 注释开头。
-- 行为：执行 SELECT，跟着 EXPECT 注释比对。脚本不强制 fail，
-- 但任何不为 0 的返回值（在 EXPECT: 0 行的查询）都需要人工排查。
-- 推荐配合 `verification/v1/run_invariants.sh` 自动比对。
-- ============================================================================

.headers on
.mode column
.print
.print ============================================================
.print 1) 模版子结构完整性
.print ============================================================

-- EXPECT: 0  — 每个 active 模版都至少有 1 个环节
SELECT 'TPL_NO_STAGE' AS check_name, COUNT(*) AS bad
FROM data_templates dt
WHERE dt.status = 'active' AND dt.disable = 0
  AND NOT EXISTS (
    SELECT 1 FROM template_stages ts
    WHERE ts.template_id = dt.id AND ts.disable = 0
  );

-- EXPECT: 0  — 每个 stage 都至少有 1 个 file_rule
SELECT 'STAGE_NO_RULE' AS check_name, COUNT(*) AS bad
FROM template_stages ts
WHERE ts.disable = 0
  AND NOT EXISTS (
    SELECT 1 FROM template_file_rules tfr
    WHERE tfr.template_stage_id = ts.id AND tfr.disable = 0
  );

-- EXPECT: 0  — file_rule_code 前缀必须匹配 data_state（IN-=input/PRC-=process/OUT-=output）
SELECT 'RULE_CODE_PREFIX_MISMATCH' AS check_name, COUNT(*) AS bad
FROM template_file_rules tfr
WHERE tfr.disable = 0
  AND (
    (tfr.data_state = 'input'   AND tfr.file_rule_code NOT LIKE 'IN-%')
 OR (tfr.data_state = 'process' AND tfr.file_rule_code NOT LIKE 'PRC-%')
 OR (tfr.data_state = 'output'  AND tfr.file_rule_code NOT LIKE 'OUT-%')
  );

.print
.print ============================================================
.print 2) 项目编码与立项完整性
.print ============================================================

-- EXPECT: 0  — 每个项目都有 owner/custodian/security 三主体
SELECT 'PROJECT_MISSING_SUBJECT' AS check_name, COUNT(*) AS bad
FROM data_projects dp
WHERE dp.disable = 0
  AND (dp.owner_subject_id = 0 OR dp.custodian_subject_id = 0 OR dp.security_subject_id = 0);

-- EXPECT: 0  — 每个项目至少有一个成员有 close 权限
SELECT 'PROJECT_NO_CLOSE_MEMBER' AS check_name, COUNT(*) AS bad
FROM data_projects dp
WHERE dp.disable = 0
  AND NOT EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = dp.id AND pm.disable = 0
      AND pm.permission_actions LIKE '%"close"%'
  );

-- EXPECT: 0  — 项目敏感等级满足"就高不就低"（≥ 模版基线）
SELECT 'PROJECT_BELOW_TEMPLATE_LEVEL' AS check_name, COUNT(*) AS bad
FROM data_projects dp
JOIN data_templates dt ON dt.template_code = dp.template_code AND dt.template_version = dp.template_version
WHERE dp.disable = 0
  AND (
    CASE dp.sensitivity_level
      WHEN 'general' THEN 1 WHEN 'important' THEN 2 WHEN 'core_secret' THEN 3 ELSE 0 END
    <
    CASE dt.project_sensitivity_level
      WHEN 'general' THEN 1 WHEN 'important' THEN 2 WHEN 'core_secret' THEN 3 ELSE 0 END
  );

-- EXPECT: 0  — 项目编码格式 {SHORT}-{YYYY}-{NNN}（最少 10 字符 + 包含两个 -）
SELECT 'PROJECT_CODE_BAD_FORMAT' AS check_name, COUNT(*) AS bad
FROM data_projects dp
WHERE dp.disable = 0
  AND (
    LENGTH(dp.project_code) < 10
 OR (LENGTH(dp.project_code) - LENGTH(REPLACE(dp.project_code, '-', ''))) < 2
  );

.print
.print ============================================================
.print 3) 卷宗结构对齐（项目 → stages → file_versions → ledgers）
.print ============================================================

-- EXPECT: 0  — 每个 file_version 都有对应底账（除了非常特殊的 receive 情况，应该都有）
SELECT 'FV_WITHOUT_LEDGER' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND NOT EXISTS (
    SELECT 1 FROM asset_ledgers al
    WHERE al.file_version_id = fv.id AND al.disable = 0
  );

-- EXPECT: 0  — 每个底账都有对应 file_version
SELECT 'LEDGER_WITHOUT_FV' AS check_name, COUNT(*) AS bad
FROM asset_ledgers al
WHERE al.disable = 0
  AND NOT EXISTS (
    SELECT 1 FROM file_versions fv
    WHERE fv.id = al.file_version_id AND fv.disable = 0
  );

-- EXPECT: 0  — 底账与 fv 的 lifecycle_status 应保持同步（除新建瞬间）
SELECT 'LEDGER_FV_STATUS_DRIFT' AS check_name, COUNT(*) AS bad
FROM asset_ledgers al
JOIN file_versions fv ON fv.id = al.file_version_id
WHERE al.disable = 0 AND fv.disable = 0
  AND al.lifecycle_status != fv.lifecycle_status;

-- EXPECT: 0  — file_version_code 唯一
SELECT 'FV_CODE_DUPLICATE' AS check_name, COUNT(*) AS bad
FROM (
  SELECT file_version_code FROM file_versions
  WHERE disable = 0
  GROUP BY file_version_code
  HAVING COUNT(*) > 1
);

-- EXPECT: 0  — ledger_code 唯一
SELECT 'LEDGER_CODE_DUPLICATE' AS check_name, COUNT(*) AS bad
FROM (
  SELECT ledger_code FROM asset_ledgers
  WHERE disable = 0
  GROUP BY ledger_code
  HAVING COUNT(*) > 1
);

.print
.print ============================================================
.print 4) 文件操作链路完整性（D2-D5）
.print ============================================================

-- EXPECT: 0  — registered/in_use/sealed 状态下必须有 storage_uri
SELECT 'FV_REGISTERED_NO_STORAGE' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.lifecycle_status IN ('registered', 'in_use', 'sealed', 'permanent')
  AND (fv.storage_uri IS NULL OR fv.storage_uri = '');

-- EXPECT: 0  — registered+ 状态下必须有 checksum（来自上游领取的 input 是从源拷贝过来的）
SELECT 'FV_REGISTERED_NO_CHECKSUM' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.lifecycle_status IN ('registered', 'in_use', 'sealed', 'permanent')
  AND (fv.checksum IS NULL OR fv.checksum = '');

-- EXPECT: 0  — checksum 应该是 64 字符 SHA-256 hex
SELECT 'FV_CHECKSUM_LENGTH_INVALID' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.checksum IS NOT NULL AND fv.checksum != ''
  AND LENGTH(fv.checksum) != 64;

-- EXPECT: 0  — 输入 fv 处于 registered 但 source_file_version_id 为空 → 应当是直接上传 input；正常
-- EXPECT: 0  — source_file_version_id 指向的 fv 应该存在
SELECT 'FV_SOURCE_DANGLING' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.source_file_version_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM file_versions src
    WHERE src.id = fv.source_file_version_id AND src.disable = 0
  );

-- EXPECT: 0  — submitted_at 必须在 output 数据态上才能设置
SELECT 'FV_NON_OUTPUT_SUBMITTED' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.submitted_at IS NOT NULL
  AND fv.data_state != 'output';

.print
.print ============================================================
.print 5) 生命周期事件流（仅追加 + 状态机）
.print ============================================================

-- EXPECT: 0  — 每条 lifecycle_event 都关联到一个存在的 fv
SELECT 'EVENT_FV_DANGLING' AS check_name, COUNT(*) AS bad
FROM lifecycle_events le
WHERE NOT EXISTS (
  SELECT 1 FROM file_versions fv
  WHERE fv.id = le.file_version_id AND fv.disable = 0
);

-- EXPECT: 0  — 每条事件的 ledger_id（如有）也应存在
SELECT 'EVENT_LEDGER_DANGLING' AS check_name, COUNT(*) AS bad
FROM lifecycle_events le
WHERE le.ledger_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM asset_ledgers al
    WHERE al.id = le.ledger_id AND al.disable = 0
  );

-- EXPECT: 0  — event_type 在已知类型集合内
SELECT 'EVENT_TYPE_UNKNOWN' AS check_name, COUNT(*) AS bad
FROM lifecycle_events le
WHERE le.event_type NOT IN ('register','use','transfer','change','handover','archive','destroy','permanent');

-- 信息：每个项目的事件数 / 类型分布（不报警，仅查看）
.print
.print -- INFO: 项目事件汇总（人工查看）
SELECT al.project_code, le.event_type, COUNT(*) AS n
FROM lifecycle_events le
JOIN asset_ledgers al ON al.id = le.ledger_id
GROUP BY al.project_code, le.event_type
ORDER BY al.project_code, le.event_type;

.print
.print ============================================================
.print 6) 安全策略与等级
.print ============================================================

-- EXPECT: 0  — registered+ 的 fv 应当 attached security_policy_id
SELECT 'FV_REGISTERED_NO_POLICY' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.lifecycle_status IN ('registered','in_use','sealed','permanent')
  AND fv.security_policy_id IS NULL;

-- EXPECT: 0  — security_policy_id 指向的策略存在
SELECT 'FV_POLICY_DANGLING' AS check_name, COUNT(*) AS bad
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.security_policy_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM security_policies sp
    WHERE sp.id = fv.security_policy_id AND sp.disable = 0
  );

-- EXPECT: 20  — 安全策略基线应有 4 等级 × 5 状态 = 20 条
SELECT 'SECURITY_POLICY_COUNT' AS check_name, COUNT(*) AS bad
FROM security_policies WHERE disable = 0;

.print
.print ============================================================
.print 7) 项目结项与归档
.print ============================================================

-- EXPECT: 0  — archived 项目的所有 fv 应处于终态（sealed/permanent/destroyed/planned）
SELECT 'ARCHIVED_PROJECT_HAS_LIVE_FV' AS check_name, COUNT(*) AS bad
FROM data_projects dp
JOIN file_versions fv ON fv.project_id = dp.id
WHERE dp.disable = 0 AND fv.disable = 0
  AND dp.status = 'archived'
  AND fv.lifecycle_status IN ('registered','in_use');

-- EXPECT: 0  — 已上报成功（sync_status='success'）的项目必须有 synced_at
SELECT 'SYNCED_BUT_NO_TIMESTAMP' AS check_name, COUNT(*) AS bad
FROM data_projects dp
WHERE dp.disable = 0
  AND dp.sync_status = 'success'
  AND dp.synced_at IS NULL;

-- 信息：项目状态汇总
.print
.print -- INFO: 项目状态汇总
SELECT status, sync_status, COUNT(*) AS n FROM data_projects WHERE disable = 0 GROUP BY status, sync_status;

.print
.print ============================================================
.print 8) 三主体引用完整性
.print ============================================================

-- EXPECT: 0  — 项目引用的三主体必须存在
SELECT 'PROJECT_SUBJECT_DANGLING' AS check_name, COUNT(*) AS bad
FROM data_projects dp
WHERE dp.disable = 0
  AND (
    NOT EXISTS (SELECT 1 FROM subjects s WHERE s.id = dp.owner_subject_id AND s.disable = 0)
 OR NOT EXISTS (SELECT 1 FROM subjects s WHERE s.id = dp.custodian_subject_id AND s.disable = 0)
 OR NOT EXISTS (SELECT 1 FROM subjects s WHERE s.id = dp.security_subject_id AND s.disable = 0)
  );

-- EXPECT: 0  — 项目成员的 subject_id 也必须存在
SELECT 'MEMBER_SUBJECT_DANGLING' AS check_name, COUNT(*) AS bad
FROM project_members pm
WHERE pm.disable = 0
  AND NOT EXISTS (SELECT 1 FROM subjects s WHERE s.id = pm.subject_id AND s.disable = 0);

.print
.print ============================================================
.print 校验完毕。
.print 任何 'bad > 0' 的检查项都需要人工排查。
.print ============================================================
