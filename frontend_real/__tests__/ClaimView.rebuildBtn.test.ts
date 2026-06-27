import { describe, it, expect, vi, beforeEach, beforeAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import ClaimView from '../views/ClaimView.vue'
import { api } from '../services/api'

// Mock the api module (hoisted)
vi.mock('../services/api', () => ({
  api: {
    getConfig: vi.fn(),
    getResourcesStatistics: vi.fn(),
    getResources: vi.fn(),
    getLatestSimilarityTask: vi.fn(),
    analyzePreview: vi.fn(),
    getSuspectSummary: vi.fn(),
    ignoreAllSuspect: vi.fn(),
  },
}))

// Mock UserInfoManager
vi.mock('../services/UserInfoManager', () => ({
  userInfoManager: {
    getUserInfo: vi.fn(),
  },
}))

// Mock visualViewport for Vuetify overlays in happy-dom
beforeAll(() => {
  Object.defineProperty(window, 'visualViewport', {
    value: {
      width: 1024,
      height: 768,
      offsetLeft: 0,
      offsetTop: 0,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
    writable: true,
  })
})

const vuetify = createVuetify({ components, directives })

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getConfig).mockResolvedValue({} as any)
  vi.mocked(api.getResourcesStatistics).mockResolvedValue({} as any)
  vi.mocked(api.getResources).mockResolvedValue({ resources: [], total: 0, page: 1, pageSize: 50 } as any)
  vi.mocked(api.getLatestSimilarityTask).mockResolvedValue(null)
  vi.mocked(api.analyzePreview).mockResolvedValue({
    cache_miss_count: 5, last_run_at: null, last_run_duration_sec: 0,
  } as any)
  vi.mocked(api.getSuspectSummary).mockResolvedValue({ count: 0, sample_paths: [] })
})

describe('ClaimView 重建按钮', () => {
  it('renders 重建相似关系 button (renamed from 相似度分析)', async () => {
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    expect(wrapper.text()).toContain('重建相似关系')
    expect(wrapper.text()).not.toContain('相似度分析')
    wrapper.unmount()
  })

  it('opens RebuildSimilarityDialog on click', async () => {
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const btn = wrapper.find('[data-test="rebuild-similarity-btn"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    await flushPromises()
    // Dialog content is teleported to document.body, so check body text
    // The dialog shows "约 N 个文件" (cache_miss_count=5), "加载中", or "无需重算"
    const bodyText = document.body.textContent || ''
    expect(bodyText).toMatch(/加载中|约.*个文件|无需重算/)
    wrapper.unmount()
  })
})
