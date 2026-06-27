import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import RebuildSimilarityDialog from '../components/RebuildSimilarityDialog.vue'
import { api } from '../services/api'

// Mock the api module (hoisted, works with static imports)
vi.mock('../services/api', () => ({
  api: {
    analyzePreview: vi.fn(),
    getSuspectSummary: vi.fn(),
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

// Helper to get all text from teleported dialog content (in document.body)
const bodyText = () => document.body.textContent || ''

const isDisabled = (btn: HTMLElement) =>
  btn.hasAttribute('disabled') || btn.getAttribute('aria-disabled') === 'true'

describe('RebuildSimilarityDialog', () => {
  let wrapper: ReturnType<typeof mount>

  beforeEach(() => {
    vi.clearAllMocks()
    // 默认无 suspect 文件，单独测试可覆盖
    vi.mocked(api.getSuspectSummary).mockResolvedValue({ count: 0, sample_paths: [] })
  })

  afterEach(() => {
    wrapper?.unmount()
  })

  it('disables 开始重建 when cache_miss=0 AND family_stale=false (真正无需重建)', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 0,
      family_stale: false,
      last_run_at: '2026-05-26T10:00:00Z',
      last_run_duration_sec: 252,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    expect(bodyText()).toContain('无需重算')
    const btn = document.querySelector('[data-test="rebuild-confirm-btn"]') as HTMLElement
    expect(btn).not.toBeNull()
    expect(isDisabled(btn)).toBe(true)
  })

  it('shows N when cache_miss_count > 0 and enables button', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 12,
      family_stale: false,
      last_run_at: '2026-05-26T10:00:00Z',
      last_run_duration_sec: 252,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    expect(bodyText()).toContain('12')  // cache_miss_count
    const btn = document.querySelector('[data-test="rebuild-confirm-btn"]') as HTMLElement
    expect(btn).not.toBeNull()
    expect(isDisabled(btn)).toBe(false)
  })

  it('shows 待构建 hint AND enables button when family_stale=true even if cache_miss=0', async () => {
    // 首次扫描完未跑分析的场景：特征值都新鲜（0 miss），但家族表是空的
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 0,
      family_stale: true,
      last_run_at: null,
      last_run_duration_sec: 0,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const text = bodyText()
    expect(text).toContain('待构建')
    // 不应同时显示「无需重算」误导
    expect(text).not.toContain('无需重算')
    const btn = document.querySelector('[data-test="rebuild-confirm-btn"]') as HTMLElement
    expect(btn).not.toBeNull()
    expect(isDisabled(btn)).toBe(false)
  })

  it('shows N AND 待构建 when both family_stale=true and cache_miss>0', async () => {
    // 用户改了文件 + 扫了一次新文件：既有特征值过期，也有新文件未入家族
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 5,
      family_stale: true,
      last_run_at: '2026-05-20T10:00:00Z',
      last_run_duration_sec: 100,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const text = bodyText()
    expect(text).toContain('5')
    expect(text).toContain('待构建')
    const btn = document.querySelector('[data-test="rebuild-confirm-btn"]') as HTMLElement
    expect(isDisabled(btn)).toBe(false)
  })

  const hasHeadsUpText = () => {
    const text = bodyText()
    return (
      text.includes('数分钟') || text.includes('请保持') || text.includes('请耐心') || text.includes('十几分钟')
    )
  }

  it('heads-up 文案：family_stale=true 时显示', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 0, family_stale: true, last_run_at: null, last_run_duration_sec: 0,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    expect(hasHeadsUpText()).toBe(true)
  })

  it('heads-up 文案：cache_miss>0 时显示', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 8, family_stale: false, last_run_at: null, last_run_duration_sec: 0,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    expect(hasHeadsUpText()).toBe(true)
  })

  it('heads-up 文案：无需重建状态下也显示（用户可能勾强制重建）', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 0, family_stale: false,
      last_run_at: '2026-05-27T10:00:00Z', last_run_duration_sec: 120,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    expect(hasHeadsUpText()).toBe(true)
  })

  // 情况 C：默认按钮置灰；勾选「强制重建」复选框后按钮变可点
  it('情况 C：勾选强制重建复选框后按钮变可点', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 0,
      family_stale: false,
      last_run_at: '2026-05-27T10:00:00Z',
      last_run_duration_sec: 120,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    const btn = document.querySelector('[data-test="rebuild-confirm-btn"]') as HTMLElement
    expect(isDisabled(btn)).toBe(true)

    // 找到强制重建复选框 input 并勾选
    const force = document.querySelector(
      '[data-test="rebuild-force-checkbox"] input[type="checkbox"]',
    ) as HTMLInputElement
    expect(force).not.toBeNull()
    force.click()
    await flushPromises()

    expect(isDisabled(btn)).toBe(false)
  })

  // 情况 A / B（family_stale 或 cache_miss>0）下不该显示强制重建复选框
  // 因为已经"需要重建"，没必要再让用户多点一下
  it('情况 A/B：不显示强制重建复选框', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 5,
      family_stale: true,
      last_run_at: null,
      last_run_duration_sec: 0,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    const force = document.querySelector('[data-test="rebuild-force-checkbox"]')
    expect(force).toBeNull()
  })

  // suspect 警告：未处理 suspect > 0 时显示警告 + 复选框 + 按钮 disabled
  it('suspect>0：显示警告 + 按钮 disabled，勾选后可点', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 5,
      family_stale: true,
      last_run_at: null,
      last_run_duration_sec: 0,
    })
    vi.mocked(api.getSuspectSummary).mockResolvedValue({
      count: 42,
      sample_paths: [],
    })

    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    // 警告条出现
    const warning = document.querySelector('[data-test="rebuild-suspect-warning"]')
    expect(warning).not.toBeNull()
    expect(warning!.textContent).toContain('42')

    // 即便 family_stale=true 本身要求重建，suspect 未勾仍然 disabled
    const btn = document.querySelector('[data-test="rebuild-confirm-btn"]') as HTMLElement
    expect(isDisabled(btn)).toBe(true)

    // 勾选「我知晓」
    const ack = document.querySelector(
      '[data-test="rebuild-suspect-ack-checkbox"] input[type="checkbox"]',
    ) as HTMLInputElement
    expect(ack).not.toBeNull()
    ack.click()
    await flushPromises()

    expect(isDisabled(btn)).toBe(false)
  })

  it('suspect=0：不显示 suspect 警告', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 5,
      family_stale: true,
      last_run_at: null,
      last_run_duration_sec: 0,
    })
    vi.mocked(api.getSuspectSummary).mockResolvedValue({ count: 0, sample_paths: [] })

    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    const warning = document.querySelector('[data-test="rebuild-suspect-warning"]')
    expect(warning).toBeNull()
  })

  // getSuspectSummary API 失败时不应阻止重建（容错）
  it('getSuspectSummary 失败时按 suspect=0 处理，不阻塞重建', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 5,
      family_stale: true,
      last_run_at: null,
      last_run_duration_sec: 0,
    })
    vi.mocked(api.getSuspectSummary).mockRejectedValue(new Error('network error'))

    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    const warning = document.querySelector('[data-test="rebuild-suspect-warning"]')
    expect(warning).toBeNull()
    const btn = document.querySelector('[data-test="rebuild-confirm-btn"]') as HTMLElement
    expect(isDisabled(btn)).toBe(false)
  })

  it('emits confirm when rebuild button clicked', async () => {
    vi.mocked(api.analyzePreview).mockResolvedValue({
      cache_miss_count: 12,
      family_stale: false,
      last_run_at: '2026-05-26T10:00:00Z',
      last_run_duration_sec: 252,
    })
    wrapper = mount(RebuildSimilarityDialog, {
      props: { modelValue: true },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const btn = document.querySelector('[data-test="rebuild-confirm-btn"]') as HTMLElement
    expect(btn).not.toBeNull()
    btn.click()
    await flushPromises()
    expect(wrapper.emitted('confirm')).toBeTruthy()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([false])
  })
})
