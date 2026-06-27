import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import AIClassifyView from '../views/AIClassifyView.vue'

const vuetify = createVuetify({ components, directives })

function pendingResp(items: any[], total: number) {
  return { ok: true, json: async () => ({ success: true, data: { items, total, page: 1, page_size: 20 } }) }
}

describe('AIClassifyView level sub-tabs', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('renders 4 level tabs (全部/核心/重要/一般) inside the new tab', async () => {
    global.fetch = vi.fn(async (input: RequestInfo) => {
      const url = String(input)
      if (url.includes('/ai/classify/pending')) {
        return pendingResp([
          { resource_id: 1, resource_name: 'a.pdf', suggestions: [{ project_id: 1, project_code: 'SYS-PERSONAL-CORE', stage_code: 'GR-DA', file_rule_code: 'OUT-001', confidence: 0.8 }] },
          { resource_id: 2, resource_name: 'b.pdf', suggestions: [{ project_id: 2, project_code: 'SYS-PERSONAL-GENERAL', stage_code: 'GR-DA', file_rule_code: 'OUT-001', confidence: 0.6 }] },
        ], 2) as any
      }
      return pendingResp([], 0) as any
    }) as any

    const wrapper = mount(AIClassifyView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const tabs = wrapper.findAll('.v-tab').map(t => t.text())
    expect(tabs.some(t => t.includes('核心'))).toBe(true)
    expect(tabs.some(t => t.includes('重要'))).toBe(true)
    expect(tabs.some(t => t.includes('一般'))).toBe(true)
  })

  it('renders the 一键 AI 归目 button only when 一般 tab is active', async () => {
    global.fetch = vi.fn(async () => pendingResp([
      { resource_id: 1, resource_name: 'a.pdf', suggestions: [{ project_id: 1, project_code: 'SYS-PERSONAL-GENERAL', stage_code: 'GR-DA', file_rule_code: 'OUT-001', confidence: 0.7 }] },
    ], 1)) as any
    const wrapper = mount(AIClassifyView, { global: { plugins: [vuetify] } })
    await flushPromises()
    // 默认 currentLevel='all'，一键按钮不应出现
    expect(wrapper.text().includes('一键 AI 归目')).toBe(false)
    // 切到一般
    const generalTab = wrapper.findAll('.v-tab').find(t => t.text().includes('一般'))
    expect(generalTab).toBeTruthy()
    await generalTab!.trigger('click')
    await flushPromises()
    expect(wrapper.text().includes('一键 AI 归目')).toBe(true)
  })
})
