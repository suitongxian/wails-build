import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import AIClassifyView from '../views/AIClassifyView.vue'

const vuetify = createVuetify({ components, directives })

type FetchResp = { ok: boolean; json: () => Promise<any> }

function pendingResp(items: any[], total: number): FetchResp {
  return {
    ok: true,
    json: async () => ({ success: true, data: { items, total, page: 1, page_size: 20 } }),
  }
}

function makeFetchSpy(handler: (url: string) => any) {
  return vi.fn(async (input: RequestInfo, init?: RequestInit) => {
    const url = String(input)
    const r = handler(url)
    if (r) return r as FetchResp
    return { ok: true, json: async () => ({ success: true, data: {} }) } as FetchResp
  })
}

describe('AIClassifyView tabs', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('展示新增数据 / 普查数据两个 Tab', async () => {
    global.fetch = makeFetchSpy((url) => {
      if (url.includes('/ai/classify/pending')) return pendingResp([], 7)
      return null
    }) as any
    const wrapper = mount(AIClassifyView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const tabTexts = wrapper.findAll('.v-tab').map(t => t.text())
    expect(tabTexts.some(t => t.includes('新增数据'))).toBe(true)
    expect(tabTexts.some(t => t.includes('普查数据'))).toBe(true)
  })

  it('切到普查数据 Tab 时拉 origin=historical 的 pending', async () => {
    const spy = makeFetchSpy((url) => {
      if (url.includes('/ai/classify/pending')) return pendingResp([], 0)
      return null
    })
    global.fetch = spy as any
    const wrapper = mount(AIClassifyView, { global: { plugins: [vuetify] } })
    await flushPromises()

    const histTab = wrapper.findAll('.v-tab').find(t => t.text().includes('普查数据'))
    expect(histTab).toBeTruthy()
    await histTab!.trigger('click')
    await flushPromises()

    const urls = spy.mock.calls.map(c => String(c[0]))
    expect(urls.some(u => u.includes('origin=historical'))).toBe(true)
  })
})
