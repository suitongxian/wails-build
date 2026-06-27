import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import ProjectInitiationView from '../views/ProjectInitiationView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })

const LOCALS = [{
  id: 7, template_code: 'TPL-LOC', template_name: '本地模版甲', template_version: 'V1.0',
  class_code: 'IND-001', status: 'draft', is_published: 0, project_sensitivity_level: 'general',
  origin: 'local', scope: 'unit', short_code: '', manager: '刘老师', owner: null,
  approval_basis: null, description: null, sync_status: null, synced_at: null,
}]
const REMOTE = [{ id: 1, template_code: 'TPL-SRC', template_name: '婚姻法立法', template_version: 'V1.0', status: 'active' }]

function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}
function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' } },
      { path: '/project-initiation', component: ProjectInitiationView },
      { path: '/template-authoring/:id', component: { template: '<div/>' } },
    ],
  })
}
async function mountView(handler: (url: string, init?: RequestInit) => any) {
  mockFetch(handler)
  const router = makeRouter()
  router.push('/project-initiation')
  await router.isReady()
  const wrapper = mount(ProjectInitiationView, { global: { plugins: [vuetify, router] } })
  await flushPromises()
  return { wrapper, router }
}

const baseHandler = (url: string) => {
  if (url.includes('/manage-business-classes')) return ok([{ id: 1, code: 'IND-001', name: '出版印刷', type: 'industry', description: null }])
  if (url.includes('/manage-users')) return ok([{ username: 'liu', display_name: '刘老师', role: 'user' }])
  if (url.includes('/templates/authoring')) return ok(LOCALS)
  if (url.includes('/templates/remote-list')) return ok(REMOTE)
  return ok([])
}

describe('模板库（合并数据项目模版）', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('展示本地模版完整管理 + 在线通用模版；无「我立项的项目」', async () => {
    const { wrapper } = await mountView(baseHandler)
    const txt = wrapper.text()
    expect(txt).toContain('模板库')
    expect(txt).toContain('本地模版甲')
    expect(txt).toContain('在线通用模版')        // 原「manage 通用模版」改名
    expect(txt).not.toContain('manage 通用模版')
    expect(txt).toContain('婚姻法立法')          // 在线通用模版条目
    expect(txt).toContain('另存为本地模版')
    expect(txt).not.toContain('我立项的项目')    // 已去掉
    // 本地模版迁入的管理动作
    expect(txt).toContain('编辑结构')
    expect(txt).toContain('发布')
    expect(txt).toContain('同步管理平台')
  })

  it('在线通用模版「另存为本地模版」→ clone → 进编辑器', async () => {
    const cloned: any[] = []
    const { router } = await mountView((url, init) => {
      if (url.includes('/templates/clone-from-manage') && init?.method === 'POST') {
        cloned.push(JSON.parse(init.body as string))
        return ok({ id: 42 })
      }
      return baseHandler(url)
    })
    const pushSpy = vi.spyOn(router, 'push')
    // 直接驱动（按钮在表格内）
    const vm: any = mount(ProjectInitiationView, { global: { plugins: [vuetify, router] } }).vm
    await flushPromises()
    await vm.saveAsLocal(REMOTE[0])
    expect(cloned[0]).toEqual({ template_code: 'TPL-SRC' })
    expect(pushSpy).toHaveBeenCalledWith('/template-authoring/42')
  })

  it('导入模版：粘贴 JSON → POST /templates/import → 进编辑器', async () => {
    const imported: any[] = []
    const { wrapper, router } = await mountView((url, init) => {
      if (url.endsWith('/templates/import') && init?.method === 'POST') {
        imported.push(JSON.parse(init.body as string))
        return ok({ id: 55 })
      }
      return baseHandler(url)
    })
    expect(wrapper.text()).toContain('导入模版')
    const pushSpy = vi.spyOn(router, 'push')
    const vm: any = wrapper.vm
    vm.openImport()
    vm.importText = JSON.stringify({
      template: { template_name: '导入甲', project_sensitivity_level: 'core' },
      stages: [{ stage_name: '收稿', tasks: [{ task_name: '录入', file_rules: [{ file_name: '原稿', data_state: 'input', allowed_file_types: 'PDF' }] }] }],
    })
    await vm.doImport()
    await flushPromises()
    expect(imported[0].template.template_name).toBe('导入甲')
    expect(imported[0].stages[0].stage_name).toBe('收稿')
    expect(pushSpy).toHaveBeenCalledWith('/template-authoring/55')
  })

  it('导入模版：非法 JSON 不发请求', async () => {
    const calls: string[] = []
    const { wrapper } = await mountView((url, init) => {
      if (init?.method === 'POST') calls.push(url)
      return baseHandler(url)
    })
    const vm: any = wrapper.vm
    vm.openImport()
    vm.importText = '这不是 JSON'
    await vm.doImport()
    await flushPromises()
    expect(calls.some((u) => u.endsWith('/templates/import'))).toBe(false)
  })

  it('删除：先弹应用内确认（不依赖 window.confirm），确认后 DELETE /templates/:id 并刷新', async () => {
    const deleted: string[] = []
    let loads = 0
    const { wrapper } = await mountView((url, init) => {
      if (url.includes('/templates/authoring')) { loads++; return ok(LOCALS) }
      if (url.match(/\/templates\/7$/) && init?.method === 'DELETE') { deleted.push(url); return ok({}) }
      return baseHandler(url)
    })
    const vm: any = wrapper.vm
    const loadsBefore = loads
    // 点删除：仅打开确认弹窗，尚未发删除请求
    vm.askRemove(LOCALS[0])
    await flushPromises()
    expect(vm.removeDialog.open).toBe(true)
    expect(vm.removeDialog.target.id).toBe(7)
    expect(deleted.length).toBe(0)
    // 确认删除 → 发 DELETE 并刷新列表
    await vm.confirmRemove()
    await flushPromises()
    expect(deleted.length).toBe(1)
    expect(deleted[0]).toMatch(/\/templates\/7$/)
    expect(vm.removeDialog.open).toBe(false)
    expect(loads).toBeGreaterThan(loadsBefore)
  })

  it('新建模版 → POST /templates；发布 → POST publish', async () => {
    const posted: any[] = []
    const published: any[] = []
    const { wrapper } = await mountView((url, init) => {
      if (url.endsWith('/templates') && init?.method === 'POST') { posted.push(JSON.parse(init.body as string)); return ok({ id: 99 }) }
      if (url.match(/\/templates\/7\/publish$/) && init?.method === 'POST') { published.push(JSON.parse(init.body as string)); return ok({ is_published: true }) }
      return baseHandler(url)
    })
    const vm: any = wrapper.vm
    vm.openCreate()
    vm.form.template_name = '新模版'
    vm.form.sensitivity_level = 'general'
    await vm.save()
    await flushPromises()
    expect(posted[0].template_name).toBe('新模版')
    await vm.togglePublish(LOCALS[0])
    expect(published[0]).toEqual({ published: true })
  })
})
