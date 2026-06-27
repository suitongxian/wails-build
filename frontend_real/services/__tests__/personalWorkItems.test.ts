import { describe, it, expect } from 'vitest'
import { buildRecentWorkItems, workItemLabel, inferWorkItemFromName } from '../personalWorkItems'
import type { AssetLedger } from '../projectsApi'

function ledger(over: Partial<AssetLedger>): AssetLedger {
  return {
    id: 0,
    ledger_code: 'LG-0',
    file_version_id: 0,
    class_code: null,
    project_code: 'SYS-PERSONAL-GENERAL',
    stage_code: 'GR-DRAFT',
    file_version_code: 'PRC-001',
    asset_name: 'untitled.docx',
    content_summary: null,
    owner_subject_id: 0,
    custodian_subject_id: 0,
    security_subject_id: 0,
    sensitivity_level: 'general',
    marking_method: '',
    source_ref: null,
    current_storage_uri: null,
    lifecycle_status: 'in_use',
    create_time: '2026-05-21T00:00:00Z',
    update_time: '2026-05-21T00:00:00Z',
    ...over,
  }
}

describe('workItemLabel', () => {
  it('uses content_summary prefix when present', () => {
    expect(
      workItemLabel(ledger({ content_summary: '工作事项/主题：调研报告' })),
    ).toBe('调研报告')
  })

  it('falls back to asset_name stem when no prefix', () => {
    expect(workItemLabel(ledger({ asset_name: '调研报告-初稿.docx' }))).toBe('调研报告')
  })

  it('returns 未归入 when both empty', () => {
    expect(workItemLabel(ledger({ asset_name: '', content_summary: null }))).toBe('未归入具体主题')
  })
})

describe('inferWorkItemFromName', () => {
  it('strips extension and takes prefix before common separators', () => {
    expect(inferWorkItemFromName('合同 - 初稿.pdf')).toBe('合同')
    expect(inferWorkItemFromName('合同_v1.pdf')).toBe('合同')
    expect(inferWorkItemFromName('合同（草案）.pdf')).toBe('合同')
    expect(inferWorkItemFromName('合同[终稿].pdf')).toBe('合同')
  })
})

describe('buildRecentWorkItems', () => {
  const now = new Date('2026-05-21T12:00:00Z')

  it('filters out groups whose latest activity is older than the window', () => {
    const items = [
      ledger({
        id: 1,
        asset_name: '老课题-说明.docx',
        update_time: '2026-01-01T00:00:00Z',
        create_time: '2026-01-01T00:00:00Z',
      }),
      ledger({
        id: 2,
        asset_name: '新课题-初稿.docx',
        update_time: '2026-05-15T00:00:00Z',
        create_time: '2026-05-15T00:00:00Z',
      }),
    ]
    const result = buildRecentWorkItems(items, { windowDays: 30, now })
    expect(result.map((g) => g.name)).toEqual(['新课题'])
  })

  it('uses update_time as activity when more recent than create_time', () => {
    const items = [
      ledger({
        id: 1,
        asset_name: '旧课题-初稿.docx',
        create_time: '2025-01-01T00:00:00Z',
        update_time: '2026-05-20T00:00:00Z',
      }),
    ]
    const result = buildRecentWorkItems(items, { windowDays: 30, now })
    expect(result).toHaveLength(1)
    expect(result[0].name).toBe('旧课题')
  })

  it('limits the number of returned groups', () => {
    const items: AssetLedger[] = []
    for (let i = 0; i < 10; i++) {
      items.push(
        ledger({
          id: i,
          asset_name: `主题${i}-x.docx`,
          create_time: '2026-05-20T00:00:00Z',
          update_time: '2026-05-20T00:00:00Z',
        }),
      )
    }
    const result = buildRecentWorkItems(items, { windowDays: 30, limit: 3, now })
    expect(result).toHaveLength(3)
  })

  it('aggregates count / final / process correctly', () => {
    const items = [
      ledger({
        id: 1,
        asset_name: '同一事项-第1版.docx',
        file_version_code: 'PRC-001',
        stage_code: 'GR-DRAFT',
        create_time: '2026-05-15T00:00:00Z',
        update_time: '2026-05-15T00:00:00Z',
      }),
      ledger({
        id: 2,
        asset_name: '同一事项-定稿.docx',
        file_version_code: 'OUT-001',
        stage_code: 'GR-FINAL',
        create_time: '2026-05-20T00:00:00Z',
        update_time: '2026-05-20T00:00:00Z',
      }),
    ]
    const [group] = buildRecentWorkItems(items, { windowDays: 30, now })
    expect(group.count).toBe(2)
    expect(group.processCount).toBe(1)
    expect(group.finalCount).toBe(1)
    expect(group.latest?.id).toBe(2)
  })

  it('returns empty when no ledgers are within window', () => {
    const items = [ledger({ create_time: '2025-01-01T00:00:00Z', update_time: '2025-01-01T00:00:00Z' })]
    const result = buildRecentWorkItems(items, { windowDays: 30, now })
    expect(result).toEqual([])
  })
})
