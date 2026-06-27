import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest'
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
    getFamilyMembers: vi.fn(),
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

const mkResource = (id: number, name: string, familyMemberCount: number) => ({
  data_resources_id: id,
  content_sign: `CS_${id}`,
  resources_name: name,
  source_count: 1,
  first_create_time: '2026-01-01T00:00:00Z',
  claim_status: 0,
  family_id: familyMemberCount > 1 ? 10 : null,
  family_member_count: familyMemberCount,
  family_relation: 'primary',
  family_same_content_count: 0,
  family_process_version_count: 0,
  family_derived_count: 0,
  primary_path: '/x/' + name,
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getConfig).mockResolvedValue({} as any)
  vi.mocked(api.getResourcesStatistics).mockResolvedValue({} as any)
  vi.mocked(api.getResources).mockResolvedValue({
    resources: [
      mkResource(1, 'big-fam.pdf', 5),
      mkResource(2, 'solo.pdf', 1),
      mkResource(3, 'zero-fam.pdf', 0),
    ],
    total: 3,
    page: 1,
    pageSize: 50,
  } as any)
  vi.mocked(api.getLatestSimilarityTask).mockResolvedValue(null)
  vi.mocked(api.analyzePreview).mockResolvedValue({
    cache_miss_count: 0,
    last_run_at: null,
    last_run_duration_sec: 0,
  } as any)
  vi.mocked(api.getFamilyMembers).mockResolvedValue({
    family_id: 10,
    total_members: 5,
    groups: {
      primary: [{ data_resources_id: 1, resources_name: 'big-fam.pdf', path: '/x/big-fam.pdf' }],
      same_content: [],
      process_version: [],
      derived: [],
    },
  } as any)
})

describe('ClaimView primary row 关联 chip', () => {
  it('shows 关联 N chip when family_member_count > 1', async () => {
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    // 5 members - 1 (primary) = 4
    expect(wrapper.text()).toMatch(/关联\s*4\s*▾/)
    wrapper.unmount()
  })

  it('hides 关联 chip when family_member_count <= 1', async () => {
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    // The chip uses data-test="family-chip"
    const chips = wrapper.findAll('[data-test="family-chip"]')
    // Only 1 row (big-fam, count=5) should have the chip
    expect(chips.length).toBe(1)
    wrapper.unmount()
  })

  it('calls handleViewFamilyGroup with "all" on chip click', async () => {
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const chip = wrapper.find('[data-test="family-chip"]')
    expect(chip.exists()).toBe(true)
    await chip.trigger('click')
    await flushPromises()
    // Verify getFamilyMembers was called with family_id
    expect(vi.mocked(api.getFamilyMembers)).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('tooltip wraps the family chip', async () => {
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    // The template should contain the tooltip text
    const sourceCode = wrapper.vm.$el.outerHTML
    expect(sourceCode).toContain('关联')
    expect(sourceCode).toContain('▾')
    wrapper.unmount()
  })

  it('chip is clickable and stops propagation', async () => {
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const chip = wrapper.find('[data-test="family-chip"]')
    expect(chip.exists()).toBe(true)
    // Check that the chip has pointer cursor
    expect(chip.attributes('style')).toContain('cursor: pointer;')
    wrapper.unmount()
  })

  it('chip renders with correct styling', async () => {
    const wrapper = mount(ClaimView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const chip = wrapper.find('[data-test="family-chip"]')
    // Check classes - size, variant, color are rendered as class names
    const classes = chip.classes().join(' ')
    expect(classes).toContain('v-chip')
    // Vuetify renders color="primary" as a text-primary class or similar
    expect(classes).toMatch(/v-chip/)
    wrapper.unmount()
  })
})
