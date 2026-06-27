import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

const vuetify = createVuetify({ components, directives })

beforeAll(() => {
  if (typeof window !== 'undefined' && !('visualViewport' in window)) {
    Object.defineProperty(window, 'visualViewport', {
      value: {
        width: 1024, height: 768, scale: 1, offsetLeft: 0, offsetTop: 0,
        pageLeft: 0, pageTop: 0, addEventListener: () => {}, removeEventListener: () => {},
      },
      configurable: true,
    })
  }
})

const batchClaimMock = vi.fn(async () => ({ updatedCount: 1, success: true }))
const batchFamilyMembersMock = vi.fn(async () => ({}))
const getConfigMock = vi.fn(async () => ({}))
const saveConfigMock = vi.fn(async () => ({ success: true }))

vi.mock('../services/api', () => ({
  api: {
    getConfig: getConfigMock,
    saveConfig: saveConfigMock,
    getResourcesStatistics: vi.fn(async () => ({})),
    getResources: vi.fn(async () => ({ resources: [], total: 0, page: 1, pageSize: 50 })),
    getLatestSimilarityTask: vi.fn(async () => null),
    analyzePreview: vi.fn(async () => ({ cache_miss_count: 0, last_run_at: null, last_run_duration_sec: 0 })),
    batchFamilyMembers: batchFamilyMembersMock,
    batchClaim: batchClaimMock,
  },
}))

vi.mock('../services/UserInfoManager', () => ({
  userInfoManager: {
    getUserInfo: vi.fn(async () => ({ user_name: 'tester', company_name: 'Co' })),
  },
}))

const mkRes = (id: number, name: string, contentSign: string, familyMemberCount = 0) => ({
  data_resources_id: id, content_sign: contentSign, resources_name: name,
  source_count: 1, claim_status: 0, family_id: familyMemberCount > 0 ? 10 : null,
  family_member_count: familyMemberCount, family_relation: 'primary',
  family_same_content_count: 0, family_process_version_count: 0, family_derived_count: 0,
  first_create_time: '2026-01-01T00:00:00Z', primary_path: '/x/' + name,
})

describe('ClaimView 弹窗触发', () => {
  afterEach(() => {
    vi.clearAllMocks()
    document.body.innerHTML = ''
  })

  it('selecting 1 row with family → opens single dialog', async () => {
    const resources = [mkRes(1, 'p.docx', 'CS_1', 3)]
    ;(await import('../services/api')).api.getResources = vi.fn(async () => ({
      resources, total: 1, page: 1, pageSize: 50,
    }))
    batchFamilyMembersMock.mockResolvedValueOnce({
      'CS_1': [
        { data_resources_id: 1, family_relation: 'primary', content_sign: 'CS_1', resources_name: 'p.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
        { data_resources_id: 11, family_relation: 'same_content', content_sign: 'CS_11', resources_name: 'pp.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
      ],
    })

    const { default: ClaimView } = await import('../views/ClaimView.vue')
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] }, attachTo: document.body })
    await flushPromises()
    ;(wrapper.vm as any).selectedIds = [1]
    await wrapper.vm.$nextTick()
    await (wrapper.vm as any).handleClaim(2)
    await flushPromises()

    expect(batchFamilyMembersMock).toHaveBeenCalled()
    expect(wrapper.findComponent({ name: 'ClaimFamilyDialogSingle' }).exists()).toBe(true)
    expect((wrapper.vm as any).singleDialogOpen).toBe(true)
    wrapper.unmount()
  })

  it('selecting 2+ rows → opens batch dialog', async () => {
    const resources = [mkRes(1, 'a.docx', 'CS_A', 2), mkRes(2, 'b.docx', 'CS_B', 1)]
    ;(await import('../services/api')).api.getResources = vi.fn(async () => ({
      resources, total: 2, page: 1, pageSize: 50,
    }))
    batchFamilyMembersMock.mockResolvedValueOnce({
      'CS_A': [{ data_resources_id: 1, family_relation: 'primary', content_sign: 'CS_A', resources_name: 'a.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null }],
    })

    const { default: ClaimView } = await import('../views/ClaimView.vue')
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] }, attachTo: document.body })
    await flushPromises()
    ;(wrapper.vm as any).selectedIds = [1, 2]
    await wrapper.vm.$nextTick()
    await (wrapper.vm as any).handleClaim(2)
    await flushPromises()

    expect((wrapper.vm as any).batchDialogOpen).toBe(true)
    expect(wrapper.findComponent({ name: 'ClaimFamilyDialogBatch' }).exists()).toBe(true)
    wrapper.unmount()
  })

  it('all selected rows have no family → bypasses dialog, calls batchClaim directly', async () => {
    const resources = [mkRes(1, 'solo.docx', 'CS_S', 0)]
    ;(await import('../services/api')).api.getResources = vi.fn(async () => ({
      resources, total: 1, page: 1, pageSize: 50,
    }))
    batchFamilyMembersMock.mockResolvedValueOnce({})  // empty → no family

    const { default: ClaimView } = await import('../views/ClaimView.vue')
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] }, attachTo: document.body })
    await flushPromises()
    ;(wrapper.vm as any).selectedIds = [1]
    await (wrapper.vm as any).handleClaim(2)
    await flushPromises()

    expect(batchClaimMock).toHaveBeenCalledWith(expect.objectContaining({ ids: [1] }))
    expect((wrapper.vm as any).singleDialogOpen).toBe(false)
    wrapper.unmount()
  })

  it('skip_dialog=true → bypasses dialog, computes IDs by policy', async () => {
    const resources = [mkRes(1, 'p.docx', 'CS_1', 3)]
    getConfigMock.mockResolvedValueOnce({
      claim_family_skip_dialog: 'true',
      claim_family_default_policy: 'same_content_only',
    })
    ;(await import('../services/api')).api.getResources = vi.fn(async () => ({
      resources, total: 1, page: 1, pageSize: 50,
    }))
    batchFamilyMembersMock.mockResolvedValueOnce({
      'CS_1': [
        { data_resources_id: 1, family_relation: 'primary', content_sign: 'CS_1', resources_name: 'p.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
        { data_resources_id: 11, family_relation: 'same_content', content_sign: 'CS_11', resources_name: 'pp.docx', source_count: 1, claim_status: 0, family_score: 1.0, family_id: 10, claimant_name: null, claim_time: null },
      ],
    })

    const { default: ClaimView } = await import('../views/ClaimView.vue')
    const wrapper = mount(ClaimView, { global: { plugins: [vuetify] }, attachTo: document.body })
    await flushPromises()
    ;(wrapper.vm as any).selectedIds = [1]
    await (wrapper.vm as any).handleClaim(2)
    await flushPromises()

    expect(batchClaimMock).toHaveBeenCalled()
    const calledIds = batchClaimMock.mock.calls[batchClaimMock.mock.calls.length - 1][0].ids
    expect(calledIds).toContain(1)
    expect(calledIds).toContain(11)
    expect((wrapper.vm as any).singleDialogOpen).toBe(false)
    wrapper.unmount()
  })
})
