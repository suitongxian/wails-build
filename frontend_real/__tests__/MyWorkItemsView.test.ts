import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import MyWorkItemsView from '../views/MyWorkItemsView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })

const MY_STAGES = [
  { id: 1, application_id: 100, stage_code: 'STG-1', stage_name: '收稿登记', status: 'pending', project_name: '印刷计划', template_code: 'TPL-X', template_version: 'V1.0', owner_name: 'lead', assignee_username: 'lisi' },
  { id: 2, application_id: 100, stage_code: 'STG-2', stage_name: '排版', status: 'pending', project_name: '印刷计划', template_code: 'TPL-X', template_version: 'V1.0', owner_name: 'lead', assignee_username: 'lisi' },
]
const APP_STAGES = [
  { stage_code: 'STG-1', stage_name: '收稿登记', status: 'pending', sort_order: 0 },
  { stage_code: 'STG-2', stage_name: '排版', status: 'pending', sort_order: 1 },
]

function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}

async function mountView(handler: (url: string, init?: RequestInit) => any) {
  mockFetch(handler)
  const wrapper = mount(MyWorkItemsView, { global: { plugins: [vuetify] } })
  await flushPromises()
  return wrapper
}

describe('MyWorkItemsView 统一收件箱（集中立项环节看板 + 在线编辑 + 交付）', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('三列看板：前置未完成的待办置为「待就绪」', async () => {
    const wrapper = await mountView((url) => {
      if (url.includes('/centralized-projects/my-stages')) return ok(MY_STAGES)
      if (url.match(/\/centralized-projects\/100\/stages$/)) return ok(APP_STAGES)
      return ok([])
    })
    expect(wrapper.text()).toContain('我的待办')
    const vm: any = wrapper.vm
    expect(vm.todoItems.map((s: any) => s.stage_code)).toEqual(['STG-1', 'STG-2'])
    expect(vm.isBlocked(MY_STAGES[0])).toBe(false)
    expect(vm.isBlocked(MY_STAGES[1])).toBe(true)
  })

  it('开始工作 → start 带 template_code + all_stage_codes，并自动打开在线编辑', async () => {
    const started: any[] = []
    const wrapper = await mountView((url, init) => {
      if (url.includes('/centralized-projects/my-stages')) return ok([{ ...MY_STAGES[0] }])
      if (url.match(/\/centralized-projects\/100\/stages$/)) return ok([APP_STAGES[0]])
      if (url.match(/\/centralized-projects\/stages\/1\/start$/) && init?.method === 'POST') {
        started.push(JSON.parse(init.body as string))
        return ok({ virtual_project_code: 'CPA-100', scaffolded: 2 })
      }
      if (url.includes('/work-items/input-docs')) return ok([])
      if (url.includes('/work-items/process-docs')) return ok([{ name: '收稿登记表.docx', size: 0, empty: true }])
      if (url.includes('/work-items/doc')) return ok({ content: '' })
      return ok([])
    })
    const vm: any = wrapper.vm
    await vm.startWork({ ...MY_STAGES[0] })
    await flushPromises()
    expect(started[0].application_id).toBe(100)
    expect(started[0].template_code).toBe('TPL-X')
    expect(started[0].all_stage_codes).toEqual(['STG-1'])
    expect(vm.editorDialog).toBe(true) // 开始后直接进在线编辑
  })

  it('在线编辑：工作依据(只读)取 input-docs、过程取 process-docs，保存按 CPA 码落地', async () => {
    const saved: any[] = []
    const wrapper = await mountView((url, init) => {
      if (url.includes('/centralized-projects/my-stages')) return ok([{ ...MY_STAGES[0], status: 'in_progress' }])
      if (url.match(/\/centralized-projects\/100\/stages$/)) return ok(APP_STAGES)
      if (url.includes('/work-items/input-docs')) return ok([{ name: '上游交付稿.pdf', size: 10, empty: false }])
      if (url.includes('/work-items/process-docs')) return ok([{ name: '收稿登记表.docx', size: 0, empty: true }])
      if (url.includes('/work-items/doc') && (!init || init.method !== 'POST')) return ok({ content: '' })
      if (url.includes('/work-items/doc') && init?.method === 'POST') {
        saved.push(JSON.parse(init.body as string))
        return ok({ path: '/ws/CPA-100/stages/STG-1/process/收稿登记表.docx' })
      }
      return ok([])
    })
    const vm: any = wrapper.vm
    await vm.openEditor({ ...MY_STAGES[0], status: 'in_progress' })
    await flushPromises()
    expect(vm.inputDocs.map((d: any) => d.name)).toEqual(['上游交付稿.pdf']) // 工作依据只读
    expect(vm.currentDoc).toBe('收稿登记表.docx')
    vm.content = '已登记'
    await vm.saveDoc()
    expect(saved[0].template_code).toBe('CPA-100') // 按 CPA 虚拟码落地
    expect(saved[0].name).toBe('收稿登记表.docx')
  })

  it('交付：定稿已在各文件任务完成时提交，环节交付只汇总流转（不带 selections）', async () => {
    const delivered: any[] = []
    const wrapper = await mountView((url, init) => {
      if (url.includes('/centralized-projects/my-stages')) return ok([{ ...MY_STAGES[0], status: 'in_progress' }])
      if (url.match(/\/centralized-projects\/100\/stages$/)) return ok(APP_STAGES)
      if (url.match(/\/centralized-projects\/stages\/1\/deliver$/) && init?.method === 'POST') {
        delivered.push(JSON.parse(init.body as string))
        return ok({ is_last_stage: false, next_stage_code: 'STG-2' })
      }
      return ok([])
    })
    const vm: any = wrapper.vm
    // 打开交付即确认框，不再加载 output 标识/过程文件
    vm.openDeliver({ ...MY_STAGES[0], status: 'in_progress' })
    await flushPromises()
    expect(vm.deliverDialog).toBe(true)
    await vm.submitDeliver()
    // 交付只带 application_id/current_stage_code/template_code，无 selections（定稿是任务级的）
    expect(delivered[0]).toEqual({
      application_id: 100,
      current_stage_code: 'STG-1',
      template_code: 'TPL-X',
    })
    expect(delivered[0].selections).toBeUndefined()
  })
})
