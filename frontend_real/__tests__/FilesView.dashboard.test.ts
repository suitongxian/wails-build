import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

const vuetify = createVuetify({ components, directives })

beforeAll(() => {
  if (typeof window !== 'undefined' && !('visualViewport' in window)) {
    Object.defineProperty(window, 'visualViewport', {
      value: { width: 1024, height: 768, scale: 1, offsetLeft: 0, offsetTop: 0, pageLeft: 0, pageTop: 0, addEventListener: () => {}, removeEventListener: () => {} },
      configurable: true,
    })
  }
})

const RES_STATS = vi.hoisted(() => ({
  totalFileCount: 12840, workspaceTotalCount: 8420, historyFileCount: 3880, nonHistoryFileCount: 5040,
  workspaceClaimedCount: 0, historyClaimedCount: 0, nonHistoryClaimedCount: 0,
  workspacePendingClassifyCount: 40, historyPendingClassifyCount: 20, nonHistoryPendingClassifyCount: 7,
  unclassifiedCount: 2725, coreCount: 180, importantCount: 640, openCount: 9200, privacyCount: 95,
}))
const COMPARISON = vi.hoisted(() => ({
  workspaceStatistics: { lastCount: 8100, currentCount: 8420, growthCount: 320, growthRate: 4 },
  nonHistoryStatistics: { lastCount: 180, currentCount: 320, growthCount: 140, growthRate: 77.8 },
  historyStatistics: { lastCount: 3900, currentCount: 3880, growthCount: -20, growthRate: -0.5 },
  hasComparison: true,
}))

vi.mock('../services/api', () => ({
  api: {
    getConfig: vi.fn(async () => ({ workspace: '/ws', full_inventory_time: '2026-05-01T09:00:00Z' })),
    getFiles: vi.fn(async () => ({ files: [], total: 0 })),
    getStatistics: vi.fn(async () => COMPARISON),
    getResourcesStatistics: vi.fn(async () => RES_STATS),
    getRunningScanTask: vi.fn(async () => null),
  },
}))
vi.mock('../services/TabStateManager', () => ({
  saveTabState: vi.fn(),
  loadTabState: vi.fn(() => null),
}))
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {}, path: '/' }),
  useRouter: () => ({ replace: vi.fn(), resolve: () => ({ href: '#' }), push: vi.fn() }),
}))

import FilesView from '../views/FilesView.vue'

const mountOpts = {
  global: {
    plugins: [vuetify],
    stubs: { Doughnut: true, Bar: true, VNavigationDrawer: true }, // 避免 jsdom 无 canvas / 无 layout 上下文
  },
}

describe('FilesView 概览图谱（方案A）', () => {
  afterEach(() => vi.clearAllMocks())

  it('渲染 KPI 卡片与真实统计值', async () => {
    const w = mount(FilesView, mountOpts)
    await flushPromises()
    const txt = w.text()
    expect(txt).toContain('总文件数')
    expect(txt).toContain('12,840')          // totalFileCount.toLocaleString()
    expect(txt).toContain('工作空间文件')
    expect(txt).toContain('8,420')
    expect(txt).toContain('待归类保护')
    expect(txt).toContain('67')              // 40+20+7
    expect(txt).toContain('未分类')
    expect(txt).toContain('2,725')           // unclassifiedCount
  })

  it('渲染分级分布与范围对比两块图表区', async () => {
    const w = mount(FilesView, mountOpts)
    await flushPromises()
    const txt = w.text()
    expect(txt).toContain('分级分布')
    expect(txt).toContain('范围对比')
    expect(txt).toContain('本次 vs 上次')
    // 两块图表区域已挂载（真实 canvas 或被 stub 占位）
    const html = w.html().toLowerCase()
    expect(html.includes('canvas') || html.includes('stub')).toBe(true)
  })

  it('待归类保护：未做首次普查时后端哨兵 -1 被钳为 0（不再显示 -2）', async () => {
    const saved = {
      w: RES_STATS.workspacePendingClassifyCount,
      h: RES_STATS.historyPendingClassifyCount,
      n: RES_STATS.nonHistoryPendingClassifyCount,
    }
    RES_STATS.workspacePendingClassifyCount = 0
    RES_STATS.historyPendingClassifyCount = -1
    RES_STATS.nonHistoryPendingClassifyCount = -1
    try {
      const w = mount(FilesView, mountOpts)
      await flushPromises()
      const vm: any = w.vm
      expect(vm.overviewKpis.pending).toBe(0)
    } finally {
      RES_STATS.workspacePendingClassifyCount = saved.w
      RES_STATS.historyPendingClassifyCount = saved.h
      RES_STATS.nonHistoryPendingClassifyCount = saved.n
    }
  })

  it('保留原 3 个文件分类 tab', async () => {
    const w = mount(FilesView, mountOpts)
    await flushPromises()
    const txt = w.text()
    expect(txt).toContain('工作文件档案管理')
    expect(txt).toContain('新数据登记管理')
    expect(txt).toContain('历史数据专项治理')
  })
})
