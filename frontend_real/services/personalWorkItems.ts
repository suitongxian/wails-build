/**
 * 个人文件台账 · 工作事项聚合
 *
 * 把同一个台账列表按"工作事项 / 主题"聚合成卡片。
 * 抽离出来主要是为了：
 *   1) 让"最近 N 天有活动"的时间窗口规则可被单测覆盖
 *   2) 让视图层只关心呈现，不再揉算法
 */

import type { AssetLedger } from './projectsApi'

export interface WorkItemGroup {
  name: string
  count: number
  finalCount: number
  processCount: number
  latest: AssetLedger | null
}

export interface BuildRecentWorkItemsOptions {
  /** 时间窗口（天），默认 30；只保留最近一次活动在此窗口内的事项 */
  windowDays?: number
  /** 卡片数量上限，默认 6 */
  limit?: number
  /** 当前时间，可注入用于测试 */
  now?: Date
}

/**
 * 工作事项主题：优先从 content_summary 的「工作事项/主题：」前缀解析，否则从 asset_name 推断
 */
export function workItemLabel(item: AssetLedger): string {
  const summary = item.content_summary || ''
  const prefix = '工作事项/主题：'
  if (summary.startsWith(prefix)) return summary.slice(prefix.length).trim() || '未归入具体主题'
  return inferWorkItemFromName(item.asset_name)
}

/**
 * 从文件名推断工作事项：取常见分隔符前的前缀
 */
export function inferWorkItemFromName(name: string): string {
  const stem = (name || '').replace(/\.[^.]+$/, '').trim()
  if (!stem) return '未归入具体主题'
  for (const sep of [' - ', '-', '_', '（', '(', '【', '[']) {
    const idx = stem.indexOf(sep)
    if (idx > 1) return stem.slice(0, idx).trim()
  }
  return stem
}

export function isProcessLedger(item: AssetLedger): boolean {
  return item.stage_code === 'GR-DRAFT' || item.file_version_code.includes('-PRC-')
}

export function isFinalLedger(item: AssetLedger): boolean {
  return item.stage_code === 'GR-FINAL' || item.file_version_code.includes('-OUT-')
}

/**
 * 构造"最近正在工作的事项"列表。
 * 规则：按主题分组 → 仅保留任一台账在 windowDays 内有活动（create_time / update_time）
 * 的分组 → 按数量倒序 → 取前 limit 个。
 */
export function buildRecentWorkItems(
  ledgers: AssetLedger[],
  opts: BuildRecentWorkItemsOptions = {},
): WorkItemGroup[] {
  const windowDays = opts.windowDays ?? 30
  const limit = opts.limit ?? 6
  const nowMs = (opts.now ?? new Date()).getTime()
  const cutoff = nowMs - windowDays * 24 * 60 * 60 * 1000

  const grouped = new Map<string, AssetLedger[]>()
  for (const ledger of ledgers) {
    const key = workItemLabel(ledger)
    const rows = grouped.get(key) || []
    rows.push(ledger)
    grouped.set(key, rows)
  }

  return Array.from(grouped.entries())
    .map(([name, rows]): WorkItemGroup => {
      const sorted = rows.slice().sort(
        (a, b) => latestActivityMs(b) - latestActivityMs(a),
      )
      return {
        name,
        count: rows.length,
        finalCount: rows.filter(isFinalLedger).length,
        processCount: rows.filter(isProcessLedger).length,
        latest: sorted[0] ?? null,
      }
    })
    .filter((group) => {
      if (!group.latest) return false
      return latestActivityMs(group.latest) >= cutoff
    })
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name, 'zh-CN'))
    .slice(0, limit)
}

function latestActivityMs(item: AssetLedger): number {
  const updated = parseMs(item.update_time)
  const created = parseMs(item.create_time)
  return Math.max(updated, created)
}

function parseMs(v: string | undefined | null): number {
  if (!v) return 0
  const t = new Date(v).getTime()
  return Number.isFinite(t) ? t : 0
}
