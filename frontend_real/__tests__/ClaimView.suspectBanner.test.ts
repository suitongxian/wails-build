import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import ClaimView from '../views/ClaimView.vue'
import { api } from '../services/api'

vi.mock('../services/api', () => ({
  api: {
    getConfig: vi.fn(),
    getResourcesStatistics: vi.fn(),
    getResources: vi.fn(),
    getLatestSimilarityTask: vi.fn(),
    analyzePreview: vi.fn(),
    getFamilyMembers: vi.fn(),
    getSuspectSummary: vi.fn(),
    ignoreAllSuspect: vi.fn(),
  },
}))

vi.mock('../services/UserInfoManager', () => ({
  userInfoManager: { getUserInfo: vi.fn() },
}))

beforeAll(() => {
  Object.defineProperty(window, 'visualViewport', {
    value: {
      width: 1024, height: 768, offsetLeft: 0, offsetTop: 0,
      addEventListener: vi.fn(), removeEventListener: vi.fn(),
    },
    writable: true,
  })
})

const vuetify = createVuetify({ components, directives })

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getConfig).mockResolvedValue({ contact_name: 'alice', contact_unit: 'team-x' } as any)
  vi.mocked(api.getResourcesStatistics).mockResolvedValue({} as any)
  vi.mocked(api.getResources).mockResolvedValue({
    resources: [], total: 0, page: 1, pageSize: 50,
  } as any)
  vi.mocked(api.getLatestSimilarityTask).mockResolvedValue(null)
  vi.mocked(api.analyzePreview).mockResolvedValue({
    cache_miss_count: 0, family_stale: false,
    last_run_at: null, last_run_duration_sec: 0,
  } as any)
})

describe('ClaimView suspect banner', () => {
  it('count > 0 时显示横幅', async () => {
    vi.mocked(api.getSuspectSummary).mockResolvedValue({
      count: 12,
      sample_paths: ['/Users/u/Library/Caches/a.cache', '/path/to/lib.dll'],
    })
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const banner = document.querySelector('[data-test="suspect-banner"]')
    expect(banner).not.toBeNull()
    expect(banner!.textContent).toContain('12')
    wrapper.unmount()
  })

  it('首次进入(无切 tab)即按 full_inventory_time 拉疑似摘要 → 横幅可见', async () => {
    // 回归：onMounted 必须先 await loadConfig，否则疑似摘要拿不到 full_inventory_time，
    // 首次进入(非来回切 tab)时疑似数算成 0、黄色横幅不显示。
    localStorage.clear()
    vi.mocked(api.getConfig).mockResolvedValue({ contact_name: 'alice', contact_unit: 'team-x', full_inventory_time: '2026-05-01T09:00:00Z' } as any)
    vi.mocked(api.getSuspectSummary).mockResolvedValue({ count: 7, sample_paths: ['/a.dll'] })
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] }, attachTo: document.body })
    await flushPromises()

    expect(document.querySelector('[data-test="suspect-banner"]')).not.toBeNull()
    expect(api.getSuspectSummary).toHaveBeenCalled()
    const firstArg = vi.mocked(api.getSuspectSummary).mock.calls[0][0] as any
    expect(firstArg.fullInventoryTime).toBe('2026-05-01T09:00:00Z')
    wrapper.unmount()
  })

  it('count = 0 时不显示横幅', async () => {
    vi.mocked(api.getSuspectSummary).mockResolvedValue({ count: 0, sample_paths: [] })
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const banner = document.querySelector('[data-test="suspect-banner"]')
    expect(banner).toBeNull()
    wrapper.unmount()
  })

  it('点击一键忽略按钮后调 ignoreAllSuspect API', async () => {
    vi.mocked(api.getSuspectSummary).mockResolvedValue({
      count: 5,
      sample_paths: ['/a.dll', '/b.ttf'],
    })
    vi.mocked(api.ignoreAllSuspect).mockResolvedValue({ updatedCount: 5 })

    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    const ignoreBtn = document.querySelector('[data-test="suspect-ignore-btn"]') as HTMLElement
    expect(ignoreBtn).not.toBeNull()
    ignoreBtn.click()
    await flushPromises()

    // 确认对话框应当弹出且列出样本路径
    const dialog = document.querySelector('[data-test="suspect-confirm-ok"]') as HTMLElement
    expect(dialog).not.toBeNull()
    expect(document.body.textContent).toContain('/a.dll')
    expect(document.body.textContent).toContain('/b.ttf')

    dialog.click()
    await flushPromises()

    expect(api.ignoreAllSuspect).toHaveBeenCalled()
    const args = vi.mocked(api.ignoreAllSuspect).mock.calls[0][0]
    expect(args.claimant_name).toBe('alice')
    expect(args.claimant_unit).toBe('team-x')
    expect(args.businessType).toBe('workspace') // 默认 tab

    wrapper.unmount()
  })
})
