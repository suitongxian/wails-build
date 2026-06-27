import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import TemplateAuthoringView from '../views/TemplateAuthoringView.vue'

const vuetify = createVuetify({ components, directives })

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' } },
      { path: '/template-authoring', component: { template: '<div/>' } },
      { path: '/template-authoring/:id', component: { template: '<div>editor</div>' } },
    ],
  })
}

const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })

function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}

const CLASSES = [{ id: 1, code: 'IND-001', name: '出版印刷', type: 'industry', description: null }]
const TEMPLATES = [
  {
    id: 10,
    template_code: 'TPL-LOCAL-001',
    template_name: '《明朝那些事儿》印刷计划',
    template_version: 'V1.0',
    class_code: 'IND-001',
    status: 'draft',
    is_published: 0,
    project_sensitivity_level: 'core',
    origin: 'local',
    scope: 'unit',
    short_code: 'MC-NSXS',
    manager: '刘老师',
    owner: null,
    approval_basis: null,
    description: null,
    sync_status: null,
    synced_at: null,
  },
]

describe('TemplateAuthoringView', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('加载并展示本地项目模版列表', async () => {
    mockFetch((url) => {
      if (url.includes('business-classes')) return ok(CLASSES)
      if (url.includes('/templates/authoring')) return ok(TEMPLATES)
      return ok([])
    })
    const router = makeRouter()
    router.push('/template-authoring')
    await router.isReady()
    const wrapper = mount(TemplateAuthoringView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    const txt = wrapper.text()
    expect(txt).toContain('数据项目模版')
    expect(txt).toContain('《明朝那些事儿》印刷计划')
    expect(txt).toContain('出版印刷') // class_code 解析成行业名
    expect(txt).toContain('核心级') // 敏感级别中文标签
  })

  it('点「新建项目模版」打开表单并 POST 创建', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('business-classes')) return ok(CLASSES)
      if (url.includes('/templates/authoring')) return ok([])
      if (url.endsWith('/templates') && init?.method === 'POST') {
        posted.push(JSON.parse(init.body as string))
        return ok({ id: 99, ...TEMPLATES[0] })
      }
      return ok([])
    })
    const router = makeRouter()
    router.push('/template-authoring')
    await router.isReady()
    const wrapper = mount(TemplateAuthoringView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    // 「新建项目模版」按钮存在（实际打开弹窗会触发 Vuetify overlay 的 jsdom 限制，
    // 此处不渲染弹窗，直接驱动组件保存逻辑验证 POST 负载）
    const newBtn = wrapper.findAll('button').find((b) => b.text().includes('新建项目模版'))
    expect(newBtn).toBeTruthy()

    const vm: any = wrapper.vm
    vm.form.template_name = '测试模版'
    vm.form.sensitivity_level = 'general'
    await vm.save()
    await flushPromises()

    expect(posted.length).toBe(1)
    expect(posted[0].template_name).toBe('测试模版')
  })

  it('点「推送manage」调用 push 接口', async () => {
    const pushed: string[] = []
    mockFetch((url, init) => {
      if (url.includes('business-classes')) return ok(CLASSES)
      if (url.includes('/templates/authoring')) return ok(TEMPLATES)
      if (url.match(/\/templates\/\d+\/push$/) && init?.method === 'POST') {
        pushed.push(url)
        return ok({ remote_id: 888 })
      }
      return ok([])
    })
    const router = makeRouter()
    router.push('/template-authoring')
    await router.isReady()
    const wrapper = mount(TemplateAuthoringView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    const vm: any = wrapper.vm
    await vm.pushToManage(TEMPLATES[0])
    await flushPromises()
    expect(pushed.some((u) => u.includes('/templates/10/push'))).toBe(true)
  })

  it('表单「模版归类」选项不含「通用行业」，默认 unit', async () => {
    mockFetch((url) => {
      if (url.includes('business-classes')) return ok(CLASSES)
      if (url.includes('/templates/authoring')) return ok([])
      return ok([])
    })
    const router = makeRouter()
    router.push('/template-authoring')
    await router.isReady()
    const wrapper = mount(TemplateAuthoringView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm: any = wrapper.vm
    const values = vm.scopeOptions.map((o: any) => o.value)
    expect(values).not.toContain('industry')
    expect(values).toEqual(expect.arrayContaining(['unit', 'department', 'person']))
    vm.openCreate()
    expect(vm.form.scope).toBe('unit')
  })

  it('点「发布」调用 /templates/:id/publish 并带 published=true', async () => {
    const published: any[] = []
    mockFetch((url, init) => {
      if (url.includes('business-classes')) return ok(CLASSES)
      if (url.includes('/templates/authoring')) return ok(TEMPLATES)
      if (url.match(/\/templates\/\d+\/publish$/) && init?.method === 'POST') {
        published.push({ url, body: JSON.parse(init.body as string) })
        return ok({ is_published: true })
      }
      return ok([])
    })
    const router = makeRouter()
    router.push('/template-authoring')
    await router.isReady()
    const wrapper = mount(TemplateAuthoringView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('未发布') // 列表展示发布状态
    const vm: any = wrapper.vm
    await vm.togglePublish(TEMPLATES[0])
    await flushPromises()
    expect(published.length).toBe(1)
    expect(published[0].url).toContain('/templates/10/publish')
    expect(published[0].body).toEqual({ published: true })
  })

  it('项目负责人下拉数据从 /manage-users 加载', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users'))
        return ok([
          { username: 'liu', display_name: '刘老师', user_unit: '院', user_department: '档案处', status: 'active' },
          { username: 'wang', display_name: '王老师', user_unit: '院', user_department: '编辑部', status: 'active' },
        ])
      if (url.includes('business-classes')) return ok(CLASSES)
      if (url.includes('/templates/authoring')) return ok([])
      return ok([])
    })
    const router = makeRouter()
    router.push('/template-authoring')
    await router.isReady()
    const wrapper = mount(TemplateAuthoringView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    const vm: any = wrapper.vm
    expect(vm.managers.length).toBe(2)
    expect(vm.managers.map((m: any) => m.display_name)).toContain('刘老师')
  })
})
