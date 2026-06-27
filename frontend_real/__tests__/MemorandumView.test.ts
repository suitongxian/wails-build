import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import MemorandumView from '../views/MemorandumView.vue'

const vuetify = createVuetify({ components, directives })

function listResp(items: any[], total: number) {
  return { ok: true, json: async () => ({ success: true, data: { items, total, page: 1, page_size: 20 } }) }
}

describe('MemorandumView', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('renders pending list with 登记 buttons', async () => {
    global.fetch = vi.fn(async (input: RequestInfo) => {
      const url = String(input)
      if (url.includes('/memorandum/pending')) {
        return listResp([{ ledger_id: 1, asset_name: '机密.docx', file_version_code: 'OUT-001', create_time: '2026-05-21' }], 1) as any
      }
      return listResp([], 0) as any
    }) as any
    const wrapper = mount(MemorandumView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const txt = wrapper.text()
    expect(txt).toContain('机密.docx')
    expect(txt.includes('登记')).toBe(true)
  })

  it('shows both 待登记 and 已登记 tabs', async () => {
    global.fetch = vi.fn(async () => listResp([], 0)) as any
    const wrapper = mount(MemorandumView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const txt = wrapper.text()
    expect(txt).toContain('待登记')
    expect(txt).toContain('已登记')
  })
})
