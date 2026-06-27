import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import ClaimFamilyDialogBatch from '../components/ClaimFamilyDialogBatch.vue'

const vuetify = createVuetify({ components, directives })

// Mock visualViewport for Vuetify overlays in happy-dom
beforeAll(() => {
  Object.defineProperty(window, 'visualViewport', {
    value: {
      width: 1024,
      height: 768,
      scale: 1,
      offsetLeft: 0,
      offsetTop: 0,
      pageLeft: 0,
      pageTop: 0,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
    writable: true,
  })
})

const mkMember = (id: number, rel: string, sign: string, name: string, claimStatus = 0) => ({
  data_resources_id: id,
  family_relation: rel,
  content_sign: sign,
  resources_name: name,
  source_count: 1,
  claim_status: claimStatus,
  family_score: 1.0,
  family_id: Math.floor(id / 10) || 1,
  claimant_name: claimStatus !== 0 ? '张三' : null,
  claim_time: null,
  data_distribution_id: null,
  path: null,
  ip: null,
})

describe('ClaimFamilyDialogBatch', () => {
  afterEach(() => { document.body.innerHTML = '' })

  it('renders one row per primary + 关联 chip with count', async () => {
    const selected = [mkMember(1, 'primary', 'CS_1', 'p1.docx'), mkMember(2, 'primary', 'CS_2', 'p2.docx')]
    const familyMap = {
      'CS_1': [selected[0], mkMember(11, 'same_content', 'CS_11', 'p1-bk.docx'), mkMember(12, 'same_content', 'CS_12', 'p1-cp.docx')],
      'CS_2': [selected[1]],
    }
    const wrapper = mount(ClaimFamilyDialogBatch, {
      props: { modelValue: true, selectedPrimaries: selected, familyMap, claimStatus: 2 },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const text = document.body.textContent || ''
    expect(text).toContain('关联 2')   // CS_1 has 2 non-primary members
    expect(text).toContain('无关联')   // CS_2 has no other members
    wrapper.unmount()
  })

  it('confirm emits IDs combining primary + checked same_content (default policy)', async () => {
    const selected = [mkMember(1, 'primary', 'CS_1', 'p1.docx')]
    const familyMap = {
      'CS_1': [selected[0], mkMember(11, 'same_content', 'CS_11', 'm1.docx')],
    }
    const wrapper = mount(ClaimFamilyDialogBatch, {
      props: { modelValue: true, selectedPrimaries: selected, familyMap, claimStatus: 2, defaultPolicy: 'same_content_only' },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const btn = document.body.querySelector('[data-test="batch-confirm-btn"]') as HTMLButtonElement
    btn.click()
    await flushPromises()
    const payload = wrapper.emitted('confirm')?.[0]?.[0] as any
    expect(payload.ids).toEqual([1, 11])
    wrapper.unmount()
  })

  it('already-claimed members are excluded from result IDs and counted in skipped', async () => {
    const selected = [mkMember(1, 'primary', 'CS_1', 'p1.docx')]
    const familyMap = {
      'CS_1': [selected[0], mkMember(11, 'same_content', 'CS_11', 'taken.docx', 2 /* already claimed */)],
    }
    const wrapper = mount(ClaimFamilyDialogBatch, {
      props: { modelValue: true, selectedPrimaries: selected, familyMap, claimStatus: 2, defaultPolicy: 'same_content_only' },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const btn = document.body.querySelector('[data-test="batch-confirm-btn"]') as HTMLButtonElement
    btn.click()
    await flushPromises()
    const payload = wrapper.emitted('confirm')?.[0]?.[0] as any
    expect(payload.ids).toContain(1)
    expect(payload.ids).not.toContain(11)
    // skipped count visible somewhere in dom
    expect(document.body.textContent).toContain('已被认领')
    wrapper.unmount()
  })

  it('global policy change updates row IDs (when row not customized)', async () => {
    const selected = [mkMember(1, 'primary', 'CS_1', 'p1.docx')]
    const familyMap = {
      'CS_1': [
        selected[0],
        mkMember(11, 'same_content', 'CS_11', 'm1.docx'),
        mkMember(12, 'process_version', 'CS_12', 'm2.docx'),
      ],
    }
    const wrapper = mount(ClaimFamilyDialogBatch, {
      props: { modelValue: true, selectedPrimaries: selected, familyMap, claimStatus: 2, defaultPolicy: 'same_content_only' },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    // Initial: default same_content_only → confirm button should show 2 IDs (primary + same_content)
    let btn = document.body.querySelector('[data-test="batch-confirm-btn"]') as HTMLButtonElement
    expect(btn.textContent).toMatch(/确认认领\s*2/)

    // Programmatically switch global to 'all'
    ;(wrapper.vm as any).globalPolicy = 'all'
    await flushPromises()
    btn = document.body.querySelector('[data-test="batch-confirm-btn"]') as HTMLButtonElement
    expect(btn.textContent).toMatch(/确认认领\s*3/)  // primary + same_content + process_version
    wrapper.unmount()
  })
})
