#!/usr/bin/env bash
# 跑 invariants.sql 并解析结果，把 bad > 0 的检查项列出来
#
# 用法：
#   verification/v1/run_invariants.sh [<scan.db 路径>]
#
# 默认 scan.db 路径：~/.config/data-asset-scan/data.db （wails 默认）
# 也可以从环境变量 SCAN_DB 读取。

set -e

DB="${1:-${SCAN_DB:-$HOME/.config/data-asset-scan/data.db}}"

if [[ ! -f "$DB" ]]; then
  echo "数据库文件不存在: $DB" >&2
  echo "用法: $0 [<scan.db 路径>]" >&2
  exit 1
fi

echo "==========================================="
echo "数据库: $DB"
echo "校验脚本: $(dirname "$0")/invariants.sql"
echo "==========================================="

# 跑 SQL，把所有形如 "<name>|<n>" 的"bad"行解析出来
output=$(sqlite3 -header -separator '|' "$DB" < "$(dirname "$0")/invariants.sql")
echo "$output"

echo
echo "==========================================="
echo "断言结果"
echo "==========================================="

# 重新跑一次 SELECT 收集 bad > 0 的（不输出 .print 装饰）
fail_count=0

# 用一个临时 SQL 抽取所有 SELECT 'NAME', COUNT(*) 形式的行
tmp_results=$(sqlite3 -separator '|' -bail "$DB" <<'EOF'
SELECT 'TPL_NO_STAGE', COUNT(*)
FROM data_templates dt
WHERE dt.status = 'active' AND dt.disable = 0
  AND NOT EXISTS (SELECT 1 FROM template_stages ts WHERE ts.template_id = dt.id AND ts.disable = 0);

SELECT 'STAGE_NO_RULE', COUNT(*)
FROM template_stages ts
WHERE ts.disable = 0
  AND NOT EXISTS (SELECT 1 FROM template_file_rules tfr WHERE tfr.template_stage_id = ts.id AND tfr.disable = 0);

SELECT 'RULE_CODE_PREFIX_MISMATCH', COUNT(*)
FROM template_file_rules tfr
WHERE tfr.disable = 0
  AND ((tfr.data_state = 'input' AND tfr.file_rule_code NOT LIKE 'IN-%')
    OR (tfr.data_state = 'process' AND tfr.file_rule_code NOT LIKE 'PRC-%')
    OR (tfr.data_state = 'output' AND tfr.file_rule_code NOT LIKE 'OUT-%'));

SELECT 'PROJECT_MISSING_SUBJECT', COUNT(*)
FROM data_projects dp
WHERE dp.disable = 0
  AND (dp.owner_subject_id = 0 OR dp.custodian_subject_id = 0 OR dp.security_subject_id = 0);

SELECT 'PROJECT_NO_CLOSE_MEMBER', COUNT(*)
FROM data_projects dp
WHERE dp.disable = 0
  AND NOT EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = dp.id AND pm.disable = 0 AND pm.permission_actions LIKE '%"close"%');

SELECT 'PROJECT_BELOW_TEMPLATE_LEVEL', COUNT(*)
FROM data_projects dp
JOIN data_templates dt ON dt.template_code = dp.template_code AND dt.template_version = dp.template_version
WHERE dp.disable = 0
  AND (CASE dp.sensitivity_level WHEN 'general' THEN 1 WHEN 'important' THEN 2 WHEN 'core_secret' THEN 3 ELSE 0 END
     < CASE dt.project_sensitivity_level WHEN 'general' THEN 1 WHEN 'important' THEN 2 WHEN 'core_secret' THEN 3 ELSE 0 END);

SELECT 'PROJECT_CODE_BAD_FORMAT', COUNT(*)
FROM data_projects dp
WHERE dp.disable = 0
  AND (LENGTH(dp.project_code) < 10
    OR (LENGTH(dp.project_code) - LENGTH(REPLACE(dp.project_code, '-', ''))) < 2);

SELECT 'FV_WITHOUT_LEDGER', COUNT(*)
FROM file_versions fv
WHERE fv.disable = 0
  AND NOT EXISTS (SELECT 1 FROM asset_ledgers al WHERE al.file_version_id = fv.id AND al.disable = 0);

SELECT 'LEDGER_WITHOUT_FV', COUNT(*)
FROM asset_ledgers al
WHERE al.disable = 0
  AND NOT EXISTS (SELECT 1 FROM file_versions fv WHERE fv.id = al.file_version_id AND fv.disable = 0);

SELECT 'LEDGER_FV_STATUS_DRIFT', COUNT(*)
FROM asset_ledgers al JOIN file_versions fv ON fv.id = al.file_version_id
WHERE al.disable = 0 AND fv.disable = 0 AND al.lifecycle_status != fv.lifecycle_status;

SELECT 'FV_CODE_DUPLICATE', (SELECT COUNT(*) FROM (SELECT file_version_code FROM file_versions WHERE disable = 0 GROUP BY file_version_code HAVING COUNT(*) > 1));

SELECT 'LEDGER_CODE_DUPLICATE', (SELECT COUNT(*) FROM (SELECT ledger_code FROM asset_ledgers WHERE disable = 0 GROUP BY ledger_code HAVING COUNT(*) > 1));

SELECT 'FV_REGISTERED_NO_STORAGE', COUNT(*)
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.lifecycle_status IN ('registered','in_use','sealed','permanent')
  AND (fv.storage_uri IS NULL OR fv.storage_uri = '');

SELECT 'FV_REGISTERED_NO_CHECKSUM', COUNT(*)
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.lifecycle_status IN ('registered','in_use','sealed','permanent')
  AND (fv.checksum IS NULL OR fv.checksum = '');

SELECT 'FV_CHECKSUM_LENGTH_INVALID', COUNT(*)
FROM file_versions fv
WHERE fv.disable = 0 AND fv.checksum IS NOT NULL AND fv.checksum != ''
  AND LENGTH(fv.checksum) != 64;

SELECT 'FV_SOURCE_DANGLING', COUNT(*)
FROM file_versions fv
WHERE fv.disable = 0 AND fv.source_file_version_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM file_versions src WHERE src.id = fv.source_file_version_id AND src.disable = 0);

SELECT 'FV_NON_OUTPUT_SUBMITTED', COUNT(*)
FROM file_versions fv
WHERE fv.disable = 0 AND fv.submitted_at IS NOT NULL AND fv.data_state != 'output';

SELECT 'EVENT_FV_DANGLING', COUNT(*)
FROM lifecycle_events le
WHERE NOT EXISTS (SELECT 1 FROM file_versions fv WHERE fv.id = le.file_version_id AND fv.disable = 0);

SELECT 'EVENT_LEDGER_DANGLING', COUNT(*)
FROM lifecycle_events le
WHERE le.ledger_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM asset_ledgers al WHERE al.id = le.ledger_id AND al.disable = 0);

SELECT 'EVENT_TYPE_UNKNOWN', COUNT(*)
FROM lifecycle_events le
WHERE le.event_type NOT IN ('register','use','transfer','change','handover','archive','destroy','permanent');

SELECT 'FV_REGISTERED_NO_POLICY', COUNT(*)
FROM file_versions fv
WHERE fv.disable = 0
  AND fv.lifecycle_status IN ('registered','in_use','sealed','permanent')
  AND fv.security_policy_id IS NULL;

SELECT 'FV_POLICY_DANGLING', COUNT(*)
FROM file_versions fv
WHERE fv.disable = 0 AND fv.security_policy_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM security_policies sp WHERE sp.id = fv.security_policy_id AND sp.disable = 0);

SELECT 'ARCHIVED_PROJECT_HAS_LIVE_FV', COUNT(*)
FROM data_projects dp JOIN file_versions fv ON fv.project_id = dp.id
WHERE dp.disable = 0 AND fv.disable = 0 AND dp.status = 'archived'
  AND fv.lifecycle_status IN ('registered','in_use');

SELECT 'SYNCED_BUT_NO_TIMESTAMP', COUNT(*)
FROM data_projects dp
WHERE dp.disable = 0 AND dp.sync_status = 'success' AND dp.synced_at IS NULL;

SELECT 'PROJECT_SUBJECT_DANGLING', COUNT(*)
FROM data_projects dp
WHERE dp.disable = 0
  AND (NOT EXISTS (SELECT 1 FROM subjects s WHERE s.id = dp.owner_subject_id AND s.disable = 0)
    OR NOT EXISTS (SELECT 1 FROM subjects s WHERE s.id = dp.custodian_subject_id AND s.disable = 0)
    OR NOT EXISTS (SELECT 1 FROM subjects s WHERE s.id = dp.security_subject_id AND s.disable = 0));

SELECT 'MEMBER_SUBJECT_DANGLING', COUNT(*)
FROM project_members pm
WHERE pm.disable = 0
  AND NOT EXISTS (SELECT 1 FROM subjects s WHERE s.id = pm.subject_id AND s.disable = 0);
EOF
)

# 处理结果
while IFS='|' read -r name n; do
  [[ -z "$name" ]] && continue
  if [[ "$name" == "SECURITY_POLICY_COUNT" ]]; then
    if [[ "$n" -ne 20 ]]; then
      echo "  ✗ $name = $n （期望 20）"
      fail_count=$((fail_count + 1))
    else
      echo "  ✓ $name = $n"
    fi
  else
    if [[ "$n" -gt 0 ]]; then
      echo "  ✗ $name = $n （期望 0）"
      fail_count=$((fail_count + 1))
    else
      echo "  ✓ $name = $n"
    fi
  fi
done <<< "$tmp_results"

echo
echo "==========================================="
if [[ "$fail_count" -eq 0 ]]; then
  echo "✓ 全部数据一致性检查通过"
  exit 0
else
  echo "✗ $fail_count 个检查项异常，请逐项排查"
  exit 1
fi
