#!/bin/bash
# V3 不变式批量校验 — 跑完 ui_checklist 后用这个一键验
#
# 用法：
#   bash verification/v3/run_invariants.sh [db_path]
#
# 默认 db_path = ~/.local/share/data-asset-scan/data.db (mac/linux)
#
# 输出格式：
#   ✅ CHECK_NAME (bad=0)        通过
#   ❌ CHECK_NAME (bad=N>0)      不通过，按 check_name 去 invariants.sql 查规则
#
# 退出码：所有通过=0；任一不过=1

set -u

DB_PATH="${1:-$HOME/.local/share/data-asset-scan/data.db}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SQL="$SCRIPT_DIR/invariants.sql"

if [ ! -f "$DB_PATH" ]; then
  echo "❌ 数据库不存在: $DB_PATH"
  exit 2
fi
if [ ! -f "$SQL" ]; then
  echo "❌ invariants.sql 找不到: $SQL"
  exit 2
fi

echo "🔍 校验数据库: $DB_PATH"
echo "📜 不变式脚本: $SQL"
echo

# 跑 SQL，捕获每行结果（check_name | bad），用 awk 比对
RAW=$(sqlite3 -separator '|' -noheader "$DB_PATH" < "$SQL" 2>&1)

# 处理 .print 标题与空行
FAIL_COUNT=0
PASS_COUNT=0
echo "$RAW" | while IFS='|' read -r name bad; do
  # 跳过 .print 标题、空行
  if [ -z "$name" ] || [ -z "$bad" ]; then
    if [ -n "$name" ] && [ -z "$bad" ]; then
      # 是 .print 输出的标题
      echo "$name"
    fi
    continue
  fi
  if [ "$bad" = "0" ]; then
    echo "  ✅ $name (bad=$bad)"
  else
    echo "  ❌ $name (bad=$bad)  ← 不通过，按 check_name 去 invariants.sql 查规则"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
done

# 由于上面在 subshell 里数 FAIL，外面拿不到；重新数一次
FAILS=$(echo "$RAW" | awk -F'|' 'NF==2 && $2 != "" && $2 != "0" && $1 !~ /^=/ && $1 !~ /^$/ {print $1}')
if [ -n "$FAILS" ]; then
  echo
  echo "❌ 共有 $(echo "$FAILS" | wc -l | tr -d ' ') 条不变式失败"
  exit 1
fi
echo
echo "✅ 全部通过"
exit 0
