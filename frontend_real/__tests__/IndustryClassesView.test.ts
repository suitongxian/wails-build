import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import IndustryClassesView from '../views/IndustryClassesView.vue'

const vuetify = createVuetify({ components, directives })

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/template-authoring', component: { template: '<div/>' } },
      { path: '/industry-classes', component: { template: '<div/>' } },
    ],
  })
}

const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })
const CLASSES = [
  { id: 1, code: 'IND-001', name: '出版印刷', type: 'industry', description: '图书/期刊印刷' },
  { id: 2, code: 'IND-002', name: '政务', type: 'industry', description: null },
]

async function mountWith(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
  const router = makeRouter()
  router.push('/industry-classes')
  await router.isReady()
  const wrapper = mount(IndustryClassesView, { global: { plugins: [vuetify, router] } })
  await flushPromises()
  return wrapper
}

describe('IndustryClassesView', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('加载并展示行业分类列表', async () => {
    const wrapper = await mountWith((url) => {
      if (url.includes('/business-classes')) return ok(CLASSES)
      return ok([])
    })
    const txt = wrapper.text()
    expect(txt).toContain('数据业务分类')
    expect(txt).toContain('出版印刷')
    expect(txt).toContain('IND-001')
    expect(txt).toContain('政务')
  })

  it('新增行业 → POST /business-classes', async () => {
    const posted: any[] = []
    const wrapper = await mountWith((url, init) => {
      if (url.endsWith('/business-classes') && init?.method === 'POST') {
        posted.push(JSON.parse(init.body as string))
        return ok({ id: 9, code: 'IND-003', name: '金融', type: 'industry', description: '' })
      }
      if (url.includes('/business-classes')) return ok(CLASSES)
      return ok([])
    })
    const vm: any = wrapper.vm
    vm.openCreate()
    vm.dialog.name = '金融'
    vm.dialog.description = '银行证券'
    await vm.save()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].name).toBe('金融')
  })

  it('删除行业 → DELETE /business-classes/:id', async () => {
    const deleted: string[] = []
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await mountWith((url, init) => {
      if (url.includes('/business-classes/1') && init?.method === 'DELETE') {
        deleted.push(url)
        return ok(null)
      }
      if (url.includes('/business-classes')) return ok(CLASSES)
      return ok([])
    })
    const vm: any = wrapper.vm
    await vm.remove(CLASSES[0])
    await flushPromises()
    expect(deleted.some((u) => u.includes('/business-classes/1'))).toBe(true)
  })
})
