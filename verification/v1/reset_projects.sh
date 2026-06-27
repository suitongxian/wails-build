#!/usr/bin/env bash
# ============================================================================
# 清理项目相关数据，回到"刚同步完模版+建好三主体"的状态。
#
# 会清掉：
#   - 所有项目（data_projects）
#   - 所有项目环节实例（project_stages）
#   - 所有项目成员（project_members）
#   - 所有文件版本（file_versions）
#   - 所有底账（asset_ledgers）
#   - 所有生命周期事件（lifecycle_events）
#   - 项目归档上传记录（project_archive_uploads）
#   - 项目根目录下所有项目子目录（manifest.json + 文件实体）
#
# 保留：
#   - subjects（三主体）
#   - data_templates / template_stages / template_file_rules（模版缓存）
#   - security_policies（安全策略基线）
#   - system_config（系统配置）
#   - user_info（用户信息）
#
# 用法：
#   verification/v1/reset_projects.sh           # 默认 ~/.config/data-asset-scan/data.db
#   SCAN_DB=/path/to/data.db verification/v1/reset_projects.sh
# ============================================================================

set -e

DB="${SCAN_DB:-$HOME/.config/data-asset-scan/data.db}"
if [[ ! -f "$DB" ]]; then
  echo "数据库文件不存在: $DB" >&2
  exit 1
fi

# 先取项目根（用于删本地目录），然后再清表
PROJECT_ROOT=$(sqlite3 "$DB" "SELECT value FROM system_config WHERE key='project_root' AND disable=0 LIMIT 1;")

# 取所有项目编码（用于精准删除项目根下的子目录，不动其他文件）
PROJECT_CODES=$(sqlite3 "$DB" "SELECT project_code FROM data_projects WHERE disable=0;")

echo "==========================================="
echo "数据库:   $DB"
echo "项目根:   ${PROJECT_ROOT:-(未配置)}"
echo "==========================================="
echo
echo "将清理以下项目："
if [[ -z "$PROJECT_CODES" ]]; then
  echo "  （没有任何项目）"
else
  while IFS= read -r code; do
    [[ -z "$code" ]] && continue
    echo "  - $code"
  done <<< "$PROJECT_CODES"
fi

echo
read -p "确认继续？(yes/N) " confirm
if [[ "$confirm" != "yes" ]]; then
  echo "已取消"
  exit 0
fi

# 1) 清表（按外键依赖顺序）
sqlite3 "$DB" <<'EOF'
BEGIN;
DELETE FROM lifecycle_events;
DELETE FROM asset_ledgers;
DELETE FROM file_versions;
DELETE FROM project_members;
DELETE FROM project_stages;
DELETE FROM data_projects;
-- project_archive_uploads 在 manage 端，不在 scan 端；这里跳过
COMMIT;
VACUUM;
EOF

echo "✓ 数据库表已清空"

# 2) 删项目根下的项目子目录（按 project_code 精准删除）
if [[ -n "$PROJECT_ROOT" && -d "$PROJECT_ROOT" ]]; then
  while IFS= read -r code; do
    [[ -z "$code" ]] && continue
    target="$PROJECT_ROOT/$code"
    if [[ -d "$target" ]]; then
      rm -rf "$target"
      echo "✓ 已删除项目目录 $target"
    fi
  done <<< "$PROJECT_CODES"
else
  echo "（项目根目录不存在或未配置，跳过物理目录清理）"
fi

echo
echo "==========================================="
echo "✓ 清理完成。下次进 wails dev 立项即从 -001 重新开始。"
echo "==========================================="
