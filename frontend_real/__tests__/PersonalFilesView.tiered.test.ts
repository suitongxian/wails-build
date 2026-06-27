import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createMemoryHistory, createRouter, type RouteRecordRaw } from 'vue-router'
import PersonalFilesView from '../views/PersonalFilesView.vue'

const vuetify = createVuetify({ components, directives })

const routes: RouteRecordRaw[] = [
  { path: '/', component: PersonalFilesView },
  { path: '/memorandum', component: { template: '<div/>' } },
  { path: '/ai-classify', component: { template: '<div/>' } },
]

describe('PersonalFilesView tiered buckets', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    global.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({ success: true, data: { items: [], total: 0, page: 1, page_size: 20 } }),
    })) as any
  })

  it('renders 核心登记 button in core bucket', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    const wrapper = mount(PersonalFilesView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const txt = wrapper.text()
    expect(txt).toContain('核心登记')
    expect(txt).toContain('核心级')
  })
})
