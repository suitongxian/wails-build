import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import ClaimFamilyDialogSingle from '../components/ClaimFamilyDialogSingle.vue'

const vuetify = createVuetify({ components, directives })

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

const mkMember = (id: number, rel: string, name: string, claimStatus = 0, claimant: string | null = null) => ({
  data_resources_id: id, family_relation: rel, content_sign: `CS_${id}`,
  resources_name: name, source_count: 1, claim_status: claimStatus,
  family_score: 1.0, family_id: 10, claimant_name: claimant, claim_time: null,
  data_distribution_id: null, path: null, ip: null,
})

const sample = () => {
  const primary = mkMember(1, 'primary', 'main.docx')
  return {
    primary,
    members: [
      primary,
      mkMember(2, 'same_content', 'backup.docx'),
      mkMember(3, 'same_content', 'dup.docx'),
      mkMember(4, 'process_version', 'v2.docx'),
      mkMember(5, 'derived', 'note.docx'),
    ],
  }
}

describe('ClaimFamilyDialogSingle', () => {
  afterEach(() => { document.body.innerHTML = '' })

  it('defaults to checking same_content members only — confirm count = 1 primary + 2 same', async () => {
    const { primary, members } = sample()
    const wrapper = mount(ClaimFamilyDialogSingle, {
      props: { modelValue: true, primary, members, claimStatus: 2 },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const text = document.body.textContent || ''
    expect(text).toMatch(/认领\s*3\s*个.*1.*主.*2.*相似/)
    wrapper.unmount()
  })

  it('emits confirm with primary + checked same_content IDs (excludes process/derived by default)', async () => {
    const { primary, members } = sample()
    const wrapper = mount(ClaimFamilyDialogSingle, {
      props: { modelValue: true, primary, members, claimStatus: 2 },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const confirmBtn = document.body.querySelector('[data-test="confirm-btn"]') as HTMLButtonElement
    confirmBtn.click()
    await flushPromises()
    const payload = wrapper.emitted('confirm')?.[0]?.[0] as any
    expect(payload.ids).toEqual([1, 2, 3])
    expect(payload.skipNextTime).toBe(false)
    wrapper.unmount()
  })

  it('only primary mode: claim only the primary', async () => {
    const { primary, members } = sample()
    const wrapper = mount(ClaimFamilyDialogSingle, {
      props: { modelValue: true, primary, members, claimStatus: 2 },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()
    const onlyBtn = document.body.querySelector('[data-test="only-primary-btn"]') as HTMLButtonElement
    onlyBtn.click()
    await flushPromises()
    const payload = wrapper.emitted('confirm')?.[0]?.[0] as any
    expect(payload.ids).toEqual([1])
    wrapper.unmount()
  })

  it('already-claimed members are excluded from result IDs', async () => {
    const { primary } = sample()
    const members = [
      primary,
      mkMember(2, 'same_content', 'taken.docx', 2, '张三'),  // already claimed
      mkMember(3, 'same_content', 'free.docx'),
    ]
    const wrapper = mount(ClaimFamilyDialogSingle, {
      props: { modelValue: true, primary, members, claimStatus: 2 },
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    const text = document.body.textContent || ''
    expect(text).toContain('已认领（认领人：张三')

    const confirmBtn = document.body.querySelector('[data-test="confirm-btn"]') as HTMLButtonElement
    confirmBtn.click()
    await flushPromises()
    const payload = wrapper.emitted('confirm')?.[0]?.[0] as any
    expect(payload.ids).not.toContain(2)  // claimed, skipped
    expect(payload.ids).toContain(3)
    wrapper.unmount()
  })
})
