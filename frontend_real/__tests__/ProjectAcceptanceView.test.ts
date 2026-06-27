import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import ProjectAcceptanceView from '../views/ProjectAcceptanceView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })

function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}

describe('ProjectAcceptanceView 承接指派', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('模版下拉合并 本地 + 在线；选本地模版直接用本地 id（不走 sync）', async () => {
    const synced: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      if (url.includes('/templates/authoring')) return ok([
        { id: 9, template_code: 'TPL-LOCAL-001', template_name: '本地印刷', template_version: 'V1.0' },
      ])
      if (url.includes('/templates/remote-list')) return ok([
        { id: 5, template_code: 'TPL-X', template_name: '在线印刷', template_version: 'V1.0' },
      ])
      if (url.includes('/templates/sync')) { synced.push(1); return ok({ template: { id: 50 } }) }
      if (url.match(/\/templates\/9$/)) return ok({ stages: [{ id: 1, stage_code: 'S1', stage_name: '收稿', sort_order: 0, file_rules: [] }] })
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAcceptDialog({ id: 7, project_name: 'P' })
    await flushPromises()
    // 合并：本地 + 在线 各一条
    const sources = vm.acceptDialog.templates.map((t: any) => t.source)
    expect(sources).toContain('local')
    expect(sources).toContain('remote')
    expect(vm.acceptDialog.templates.find((t: any) => t.value === 'L:9')).toBeTruthy()
    expect(vm.acceptDialog.templates.find((t: any) => t.value === 'R:5')).toBeTruthy()
    // 选本地模版 → 直接用本地 id 拿结构，不调用 /templates/sync
    vm.acceptDialog.templateKey = 'L:9'
    await vm.onTemplateSelect()
    await flushPromises()
    expect(synced.length).toBe(0)
    expect(vm.acceptDialog.stages.length).toBe(1)
  })

  it('选在线模版 → 先 sync 再取结构；环节下拉互相独立(stage_code 空也不联动)', async () => {
    const accepted: any[] = []
    const synced: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'u1', display_name: '员工一', role: 'user' },
        { username: 'u2', display_name: '员工二', role: 'user' },
      ])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      if (url.includes('/templates/authoring')) return ok([])
      if (url.includes('/templates/remote-list')) return ok([
        { id: 5, template_code: 'TPL-X', template_name: '印刷', template_version: 'V1.0' },
      ])
      if (url.includes('/templates/sync') && init?.method === 'POST') { synced.push(1); return ok({ template: { id: 50 } }) }
      if (url.match(/\/templates\/50$/)) return ok({
        stages: [
          { id: 1, stage_code: '', stage_name: '收稿', sort_order: 0, file_rules: [] },
          { id: 2, stage_code: '', stage_name: '排版', sort_order: 1, file_rules: [] },
        ],
      })
      if (url.match(/\/centralized-projects\/7\/accept$/) && init?.method === 'POST') {
        accepted.push(JSON.parse(init.body as string))
        return ok({ id: 7, status: 'accepted' })
      }
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAcceptDialog({ id: 7, project_name: 'P' })
    await flushPromises()
    vm.acceptDialog.templateKey = 'R:5'
    await vm.onTemplateSelect()
    await flushPromises()
    expect(synced.length).toBe(1) // 在线模版走 sync
    const keys = vm.acceptDialog.stages.map((s: any) => s.key)
    expect(new Set(keys).size).toBe(2)
    vm.acceptDialog.assignments[keys[0]] = 'u1'
    vm.acceptDialog.assignments[keys[1]] = 'u2'
    expect(vm.canSubmitAccept).toBe(true)
    await vm.confirmAccept()
    await flushPromises()
    const sas = accepted[0].stage_assignments
    expect(sas[0].assignee_username).toBe('u1')
    expect(sas[1].assignee_username).toBe('u2')
    expect(accepted[0].template_code).toBe('TPL-X')
  })

  it('分工弹窗：工作事项只读、无「添加工作环节」按钮，也不再暴露 addInlineStage', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      if (url.includes('/templates/authoring')) return ok([{ id: 9, template_code: 'TPL-L', template_name: '本地', template_version: 'V1.0' }])
      if (url.includes('/templates/remote-list')) return ok([])
      if (url.match(/\/templates\/9$/)) return ok({ stages: [{ id: 1, stage_code: 'STG-001', stage_name: '收稿', sort_order: 0, file_rules: [] }] })
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAcceptDialog({ id: 7, project_name: 'P' })
    await flushPromises()
    vm.acceptDialog.templateKey = 'L:9'
    await vm.onTemplateSelect(); await flushPromises()
    // 环节名输入框只读（分工不可改工作事项）
    const stageInput = Array.from(document.querySelectorAll('input'))
      .find(el => (el as HTMLInputElement).value === '收稿') as HTMLInputElement | undefined
    expect(stageInput).toBeTruthy()
    expect(stageInput!.readOnly).toBe(true)
    // 无「添加工作环节」按钮；拉人按钮仍在
    const body = document.body.textContent || ''
    expect(body).not.toContain('添加工作环节')
    expect(body).toContain('团队核心人员调整')
    // 不再暴露内联加环节的能力
    expect(vm.addInlineStage).toBeUndefined()
  })

  it('分工弹窗：批量指派——勾选多个工作环节，一键指派负责人（逐项指派仍可用）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }, { username: 'u2', display_name: '员工二', role: 'user' }])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      if (url.includes('/templates/authoring')) return ok([{ id: 9, template_code: 'TPL-L', template_name: '本地', template_version: 'V1.0' }])
      if (url.includes('/templates/remote-list')) return ok([])
      if (url.match(/\/templates\/9$/)) return ok({ stages: [
        { id: 1, stage_code: 'STG-1', stage_name: '收稿', sort_order: 0, file_rules: [] },
        { id: 2, stage_code: 'STG-2', stage_name: '排版', sort_order: 1, file_rules: [] },
        { id: 3, stage_code: 'STG-3', stage_name: '校对', sort_order: 2, file_rules: [] },
      ] })
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAcceptDialog({ id: 7, project_name: 'P' })
    await flushPromises()
    vm.acceptDialog.templateKey = 'L:9'
    await vm.onTemplateSelect(); await flushPromises()
    const keys = vm.acceptDialog.stages.map((s: any) => s.key)
    // 全选 → batchSelected 含全部
    vm.allStagesSelected = true
    expect(vm.acceptDialog.batchSelected.length).toBe(3)
    // 批量指派 u1 → 三个环节都填 u1
    vm.acceptDialog.batchAssignee = 'u1'
    vm.applyBatchAssign()
    expect(keys.every((k: string) => vm.acceptDialog.assignments[k] === 'u1')).toBe(true)
    // 应用后清空勾选与选择框
    expect(vm.acceptDialog.batchSelected.length).toBe(0)
    expect(vm.acceptDialog.batchAssignee).toBe('')
    // 逐项指派仍可用：单独把第二个改成 u2
    vm.acceptDialog.assignments[keys[1]] = 'u2'
    expect(vm.canSubmitAccept).toBe(true)
  })

  it('分工提交前先弹预览二次确认（展示环节→负责人名称），确认后才 POST accept', async () => {
    const accepted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      if (url.includes('/templates/authoring')) return ok([{ id: 9, template_code: 'TPL-L', template_name: '本地', template_version: 'V1.0' }])
      if (url.includes('/templates/remote-list')) return ok([])
      if (url.match(/\/templates\/9$/)) return ok({ stages: [{ id: 1, stage_code: 'STG-1', stage_name: '收稿', sort_order: 0, file_rules: [] }] })
      if (url.match(/\/centralized-projects\/7\/accept$/) && init?.method === 'POST') { accepted.push(JSON.parse(init.body as string)); return ok({ id: 7, status: 'accepted' }) }
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAcceptDialog({ id: 7, project_name: 'P' })
    await flushPromises()
    vm.acceptDialog.templateKey = 'L:9'
    await vm.onTemplateSelect(); await flushPromises()
    vm.acceptDialog.assignments[vm.acceptDialog.stages[0].key] = 'u1'
    // 点提交 → 只开预览（展示负责人名称），未 POST
    vm.openAssignPreview()
    expect(vm.assignPreview.open).toBe(true)
    expect(vm.assignPreview.rows[0]).toMatchObject({ stage: '收稿', assignee: '员工一' })
    expect(accepted.length).toBe(0)
    // 确认 → POST
    await vm.confirmAccept(); await flushPromises()
    expect(accepted.length).toBe(1)
    expect(vm.assignPreview.open).toBe(false)
  })

  it('分工弹窗：仅指派负责人即可提交，下发模版原环节名/stage_code（不改工作事项）', async () => {
    const accepted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      if (url.includes('/templates/authoring')) return ok([{ id: 9, template_code: 'TPL-L', template_name: '本地', template_version: 'V1.0' }])
      if (url.includes('/templates/remote-list')) return ok([])
      if (url.match(/\/templates\/9$/)) return ok({ stages: [{ id: 1, stage_code: 'STG-001', stage_name: '收稿', sort_order: 0, file_rules: [] }] })
      if (url.match(/\/centralized-projects\/7\/accept$/) && init?.method === 'POST') { accepted.push(JSON.parse(init.body as string)); return ok({ id: 7, status: 'accepted' }) }
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAcceptDialog({ id: 7, project_name: 'P' })
    await flushPromises()
    vm.acceptDialog.templateKey = 'L:9'
    await vm.onTemplateSelect(); await flushPromises()
    // 未指派 → 不可提交
    expect(vm.canSubmitAccept).toBe(false)
    vm.acceptDialog.assignments[vm.acceptDialog.stages[0].key] = 'u1'
    expect(vm.canSubmitAccept).toBe(true)
    await vm.confirmAccept(); await flushPromises()
    // 下发的是模版原环节名与 stage_code（分工未改工作事项）
    expect(accepted[0].stage_assignments[0].stage_name).toBe('收稿')
    expect(accepted[0].stage_assignments[0].stage_code).toBe('STG-001')
  })
})

function mountViewBtn() { return mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } }) }

describe('环节分工：按钮态 + 两步', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('立项即成队：按行状态推导按钮（承接 / 项目团队+关联模版 / 项目团队+分工 / 已分工），不再有「组建团队」空态', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 1, project_name: 'A', owner_name: 'lead', status: 'approved', template_id: null, create_time: '' },
        { id: 2, project_name: 'B', owner_name: 'lead', status: 'taken', template_id: null, create_time: '' },
        { id: 3, project_name: 'C', owner_name: 'lead', status: 'taken', template_id: 9, create_time: '' },
        { id: 4, project_name: 'D', owner_name: 'lead', status: 'accepted', template_id: 9, create_time: '' },
      ])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.rowActions(vm.items[0])).toEqual(['承接'])
    expect(vm.rowActions(vm.items[1])).toEqual(['项目团队', '关联模版']) // taken 未关联模版
    expect(vm.rowActions(vm.items[2])).toEqual(['项目团队', '工作事项', '分工'])     // taken 已关联模版：可编辑工作事项
    expect(vm.rowActions(vm.items[3])).toEqual(['已分工'])
    // 任何 taken 都不会出现「组建团队」（立项即成队）
    expect(vm.rowActions(vm.items[1])).not.toContain('组建团队')
  })

  it('「提取项目模版」只在结项(closed)且改过模版时出现；过程中各状态都不再出现提取按钮', async () => {
    mockFetch(() => ok([]))
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    // 结项 + 改过 → 出现「提取项目模版」
    expect(vm.rowActions({ status: 'closed', project_template_edited: true })).toEqual(['提取项目模版'])
    // 结项但没改过 → 不出现
    expect(vm.rowActions({ status: 'closed', project_template_edited: false })).toEqual([])
    // 过程中（taken/assigning/accepted）即便改过也不再出现任何提取按钮
    expect(vm.rowActions({ status: 'taken', template_id: 9, project_template_edited: true })).not.toContain('提取项目模版')
    expect(vm.rowActions({ status: 'taken', template_id: 9, project_template_edited: true })).not.toContain('提取认定模版')
    expect(vm.rowActions({ status: 'assigning', project_template_edited: true })).not.toContain('提取项目模版')
    expect(vm.rowActions({ status: 'accepted', project_template_edited: true })).not.toContain('提取项目模版')
  })

  it('提取项目模版：点按钮先拉改动清单弹窗，确认后才 POST extract-template 并提示存入本地模版库', async () => {
    const posted: string[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      if (url.includes('/template-diff')) return ok({ has_baseline: true, changes: [
        { level: 'stage', type: 'renamed', stage: '收件', name: '收件', from: '收稿', to: '收件' },
        { level: 'file_rule', type: 'added', stage: '收件', task: '录入', name: '扫描件' },
      ] })
      if (url.includes('/extract-template') && init?.method === 'POST') { posted.push(url); return ok({ template_id: 88 }) }
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    // 点「提取项目模版」→ 只拉差异、开弹窗，尚未提取
    await vm.openExtractDialog({ id: 5, project_code: 'XM-5' })
    await flushPromises()
    expect(vm.extractDialog.open).toBe(true)
    expect(vm.extractDialog.changes.length).toBe(2)
    expect(posted.length).toBe(0)
    // 确认 → POST extract-template
    await vm.confirmExtract()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0]).toContain('/extract-template?application_id=5')
    expect(vm.extractDialog.open).toBe(false)
    expect(vm.snackbar.color).toBe('success')
    expect(vm.snackbar.html).toContain('<strong>本地模版库</strong>')
    expect(vm.snackbar.html).toContain('已提取为「项目认定模版」')
  })

  it('页面右上角不再有全局「项目团队」按钮（仅保留刷新）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([]) // 空列表：无行内按钮干扰
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    // 标题栏（v-card-title）内不应出现「项目团队」按钮（scope 到组件根，避开残留弹窗）
    const title = wrapper.element.querySelector('.v-card-title')
    expect(title).toBeTruthy()
    const titleBtns = Array.from(title!.querySelectorAll('button')).map(b => (b.textContent || '').trim())
    expect(titleBtns.some(t => t.includes('项目团队'))).toBe(false)
    expect(titleBtns.some(t => t.includes('刷新'))).toBe(true)
  })

  it('关联模版下拉：项目认定模版置顶并标记 ★项目认定', async () => {
    mockFetch((url) => {
      if (url.includes('/templates/authoring')) return ok([
        { id: 1, template_code: 'TPL-LOCAL-001', template_name: '普通本地', template_version: 'V1.0', certified: 0 },
        { id: 2, template_code: 'TPL-LOCAL-009', template_name: '认定模版', template_version: 'V1.0', certified: 1 },
      ])
      if (url.includes('/templates/remote-list')) return ok([])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    const { opts } = await vm.loadTemplateOptions()
    expect(opts[0].certified).toBe(true) // 认定模版置顶
    expect(opts[0].label).toContain('★项目认定')
  })

  it('承接：先弹立项书确认，确认后 POST accept-project?id= 并刷新', async () => {
    const posted: string[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 7, project_name: 'A', owner_name: 'lead', status: 'approved', template_id: null, create_time: '', project_code: 'XM-7', department: '法制部' },
      ])
      if (url.includes('/accept-project') && init?.method === 'POST') { posted.push(url); return ok({ id: 7, status: 'taken' }) }
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    // 点「承接」先打开立项书确认弹窗，尚未发请求
    vm.openAcceptConfirm(vm.items[0])
    await flushPromises()
    expect(vm.acceptConfirmDialog.open).toBe(true)
    expect(vm.acceptConfirmDialog.project.project_code).toBe('XM-7')
    expect(posted.length).toBe(0)
    // 确认承接 → 发请求
    await vm.acceptProject(vm.acceptConfirmDialog.project)
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0]).toContain('/accept-project?id=7')
  })

  it('承接弹窗不再有「项目过程文件管理模式」选项（已去掉，单位级定稿统一归部门柜）', async () => {
    const bodies: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 8, project_name: 'U', owner_name: 'lead', status: 'approved', template_id: null, create_time: '', project_code: 'XM-8', department: '财务处', project_scope: 'unit' },
      ])
      if (url.includes('/accept-project') && init?.method === 'POST') { bodies.push(JSON.parse(init.body)); return ok({ id: 8, status: 'taken' }) }
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    vm.openAcceptConfirm(vm.items[0])
    await flushPromises()
    // 单位级项目也不再出现保管层级选项
    const card = Array.from(document.querySelectorAll('.v-card'))
      .find(c => (c.textContent || '').includes('承接项目：U') && (c.textContent || '').includes('XM-8'))
    expect(card).toBeTruthy()
    expect(card!.textContent || '').not.toContain('项目过程文件管理模式')
    // 承接不再下发 output_custody_scope
    await vm.acceptProject(vm.acceptConfirmDialog.project)
    await flushPromises()
    expect(bodies[0].output_custody_scope).toBeUndefined()
  })

  it('承接时填的项目周期随 accept-project 一起提交', async () => {
    const bodies: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 12, project_name: 'P', owner_name: 'lead', status: 'approved', template_id: null, create_time: '', project_code: 'XM-12', department: '法制部' },
      ])
      if (url.includes('/accept-project') && init?.method === 'POST') { bodies.push(JSON.parse(init.body)); return ok({ id: 12, status: 'taken' }) }
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    vm.openAcceptConfirm(vm.items[0])
    await flushPromises()
    vm.acceptConfirmDialog.cycle_start = '2026-07-01'
    vm.acceptConfirmDialog.cycle_end = '2026-12-31'
    await vm.acceptProject(vm.acceptConfirmDialog.project)
    await flushPromises()
    expect(bodies[0].cycle_start).toBe('2026-07-01')
    expect(bodies[0].cycle_end).toBe('2026-12-31')
  })

  it('承接弹窗：项目周期起止默认今天，不改即按今天提交', async () => {
    const bodies: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 13, project_name: 'Q', owner_name: 'lead', status: 'approved', template_id: null, create_time: '', project_code: 'XM-13', department: '法制部' },
      ])
      if (url.includes('/accept-project') && init?.method === 'POST') { bodies.push(JSON.parse(init.body)); return ok({ id: 13, status: 'taken' }) }
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    vm.openAcceptConfirm(vm.items[0])
    await flushPromises()
    const today = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    const ymd = `${today.getFullYear()}-${pad(today.getMonth() + 1)}-${pad(today.getDate())}`
    // 默认值即今天
    expect(vm.acceptConfirmDialog.cycle_start).toBe(ymd)
    expect(vm.acceptConfirmDialog.cycle_end).toBe(ymd)
    // 不修改直接确认 → 按今天提交（两端都有，不会出现「?」）
    await vm.acceptProject(vm.acceptConfirmDialog.project)
    await flushPromises()
    expect(bodies[0].cycle_start).toBe(ymd)
    expect(bodies[0].cycle_end).toBe(ymd)
  })

  it('已分工详情：列名为工作事项/工作事项责任人；责任人展示用户名称；开始/结束时间渲染', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 9, project_name: 'D', owner_name: 'lead', status: 'accepted', template_id: 9, create_time: '' },
      ])
      if (url.match(/\/centralized-projects\/9\/stages$/)) return ok([
        { id: 1, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'u1', status: 'completed', sort_order: 0, started_at: '2026-06-01 09:00:00', completed_at: '2026-06-03 15:00:00', create_time: '' },
      ])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    // 责任人 username → 用户名称
    expect(vm.displayName('u1')).toBe('员工一')
    expect(vm.displayName('unknown')).toBe('unknown') // 查不到回退登录名
    await vm.openDetails(vm.items[0]); await flushPromises()
    expect(vm.detailsDialog.stages.length).toBe(1)
    // 取详情弹窗卡片（按标题定位最后一个，避开 teleport 残留）
    const cards = Array.from(document.querySelectorAll('.v-card')).filter(c => (c.textContent || '').includes('项目详情：D'))
    const card = cards[cards.length - 1]
    const txt = card?.textContent || ''
    expect(txt).toContain('责任人')
    expect(txt).not.toContain('工作环节') // 旧列名已改
    // 表头精确校验：第二列为「责任人」（非「工作事项责任人」）
    const ths = Array.from(card.querySelectorAll('th')).map(t => (t.textContent || '').trim())
    expect(ths).toContain('责任人')
    expect(ths).not.toContain('工作事项责任人')
    expect(txt).toContain('员工一')       // 展示名称而非 u1
    expect(txt).not.toContain('u1')       // 不再露出登录名
    expect(txt).toContain('2026-06-01')   // 开始时间已渲染
  })

  it('项目团队：负责人始终在队(lead 角色)并锁定不可移除，可继续拉人', async () => {
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'lead', display_name: '负责人', role: 'user' },
        { username: 'u1', display_name: '员工一', role: 'user' },
      ])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 2, project_name: 'B', owner_name: 'lead', status: 'taken', template_id: null, create_time: '' },
      ])
      // 团队 GET：manage 端保证负责人在列且带角色
      if (url.includes('/centralized-projects/team') && (!init || init.method === 'GET')) return ok([
        { username: 'lead', display_name: '负责人', roles: ['lead'] },
      ])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openTeamForProject(vm.items[0]); await flushPromises()
    expect(vm.teamDialog.members).toEqual(['lead'])
    expect(vm.teamDialog.leadUsername).toBe('lead')
    expect(vm.teamDialog.roleMap['lead']).toContain('项目负责人')
  })

  it('项目团队：后端团队返回空也兜底显示项目负责人(owner_name)', async () => {
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 9, project_name: 'C', owner_name: 'boss', status: 'taken', template_id: null, create_time: '' },
      ])
      // 模拟后端（如旧实例）未返回负责人：团队为空
      if (url.includes('/centralized-projects/team') && (!init || init.method === 'GET')) return ok([])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openTeamForProject(vm.items[0]); await flushPromises()
    // 前端用 owner_name 兜底补出负责人
    expect(vm.teamDialog.members).toEqual(['boss'])
    expect(vm.teamDialog.leadUsername).toBe('boss')
    expect(vm.teamDialog.roleMap['boss']).toContain('项目负责人')
  })

  it('项目团队：仅 taken 可组队，保存 POST /team 带 members', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'u1', display_name: '员工一', role: 'user' },
        { username: 'u2', display_name: '员工二', role: 'user' },
      ])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 1, project_name: 'A', owner_name: 'lead', status: 'approved', template_id: null, create_time: '' },
        { id: 2, project_name: 'B', owner_name: 'lead', status: 'taken', template_id: null, create_time: '' },
      ])
      if (url.includes('/centralized-projects/team') && (!init || init.method === 'GET')) return ok([])
      if (url.includes('/centralized-projects/team') && init?.method === 'POST') { posted.push({ url, body: JSON.parse(init.body as string) }); return ok([{ username: 'u1' }]) }
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    // 只有 taken 项目可组队
    expect(vm.canFormTeam).toBe(true)
    expect(vm.eligibleTeamProjects.map((p: any) => p.id)).toEqual([2])
    await vm.openTeamDialog()
    await flushPromises()
    expect(vm.teamDialog.applicationId).toBe(2)
    vm.teamDialog.members = ['u1', 'u2']
    await vm.saveTeam()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].url).toContain('application_id=2')
    expect(posted[0].body.members).toEqual([
      { username: 'u1', display_name: '员工一' },
      { username: 'u2', display_name: '员工二' },
    ])
  })

  it('门禁：taken 未关联模版→「项目团队 + 关联模版」；关联模版后→「项目团队 + 分工」', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.rowActions({ status: 'approved' })).toEqual(['承接'])
    // 立项即成队：team_count 不再参与门禁
    expect(vm.rowActions({ status: 'taken', template_id: null })).toEqual(['项目团队', '关联模版'])
    // 关联模版后：可编辑工作事项（项目专属模版）再分工
    expect(vm.rowActions({ status: 'taken', template_id: 9 })).toEqual(['项目团队', '工作事项', '分工'])
    expect(vm.rowActions({ status: 'accepted' })).toEqual(['已分工'])
  })

  it('新状态 assigning(分工中)：动作为「项目团队 + 工作事项 + 分工」', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.rowActions({ status: 'assigning' })).toEqual(['项目团队', '工作事项', '分工'])
    expect(vm.statusLabel('assigning')).toBe('分工中')
    expect(vm.statusLabel('closed')).toBe('已结项') // 结项后中文展示，不露英文
    // 核心成员角色标签 → 项目核心成员
    expect(vm.projectRoleLabels(['core'])).toContain('项目核心成员')
    expect(vm.projectRoleLabels(['core'])).not.toContain('核心成员')
  })

  it('分工候选只含项目团队成员；临时拉人入队保存后回填人池', async () => {
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'u1', display_name: '员工一', user_department: '收集科', role: 'user' },
        { username: 'u2', display_name: '员工二', user_department: '审核科', role: 'user' },
      ])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 2, project_name: 'B', owner_name: 'lead', status: 'taken', template_id: null, create_time: '' },
      ])
      if (url.includes('/centralized-projects/team') && (!init || init.method === 'GET')) return ok([])
      if (url.includes('/centralized-projects/team') && init?.method === 'POST') return ok([{ username: 'u1' }])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAcceptDialog(vm.items[0]); await flushPromises()
    // 团队空 → 分工人池空（非团队成员不出现）
    expect(vm.assigneeItems(vm.acceptDialog.team).length).toBe(0)
    // 临时拉人入队：项目锁定为当前分工项目
    await vm.openTeamForAssign(); await flushPromises()
    expect(vm.teamDialog.locked).toBe(true)
    expect(vm.teamDialog.applicationId).toBe(2)
    vm.teamDialog.members = ['u1']
    await vm.saveTeam(); await flushPromises()
    // 分工人池回填 u1，立即可选，且带部门标注
    expect(vm.acceptDialog.team).toEqual(['u1'])
    const items = vm.assigneeItems(vm.acceptDialog.team)
    expect(items.length).toBe(1)
    expect(items[0].username).toBe('u1')
    expect(items[0].label).toContain('收集科')
  })

  it('分工弹窗按 template_code 预选，本地/远程 id 撞号不串', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 1, project_name: 'A', owner_name: 'lead', status: 'approved', template_id: 5, template_code: 'TPL-REMOTE', template_version: 'V1', create_time: '' },
      ])
      if (url.includes('/templates/authoring')) return ok([{ id: 5, template_code: 'TPL-LOCAL', template_name: '本地撞号', template_version: 'V1' }])
      if (url.includes('/templates/remote-list')) return ok([{ id: 5, template_code: 'TPL-REMOTE', template_name: '远程', template_version: 'V1' }])
      if (url.match(/\/templates\/\d+($|\?)/)) return ok({ stages: [] })
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAcceptDialog(vm.items[0])
    await flushPromises()
    const sel = vm.acceptDialog.templates.find((t: any) => t.value === vm.acceptDialog.templateKey)
    expect(sel?.template_code).toBe('TPL-REMOTE')
  })

  it('确定调 set-template（POST，带 template_*）', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([{ id: 1, project_name: 'A', owner_name: 'lead', status: 'approved', template_id: null, create_time: '' }])
      if (url.includes('/set-template') && init?.method === 'POST') { posted.push({ url, body: JSON.parse(init.body as string) }); return ok({ id: 1, status: 'approved', template_id: 9 }) }
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    vm.openSelectTemplate(vm.items[0])
    vm.selectDialog.templateKey = 'L:9'
    vm.selectDialog.templates = [{ value: 'L:9', source: 'local', id: 9, template_code: 'TPL-X', template_name: 'X', template_version: 'V1', label: 'X' }]
    await vm.confirmTemplate()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].url).toContain('/set-template?id=1')
    expect(posted[0].body.template_code).toBe('TPL-X')
  })

  it('关联模版弹窗带出完整立项书（编号/部门/立项依据/项目简介）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([{
        id: 3, project_name: '婚姻法立法项目', owner_name: '张三', status: 'approved', template_id: null,
        create_time: '2026-06-11 10:00', project_code: 'XM-2026-0003', department: '法制工作部',
        sensitivity_level: 'core', data_owner: '第一研究院',
        approval_basis: '依据《立法法》第十条', description: '梳理婚姻家庭领域法规,形成修订草案',
      }])
      return ok([])
    })
    const wrapper = mountViewBtn(); await flushPromises()
    const vm: any = wrapper.vm
    vm.openSelectTemplate(vm.items[0])
    await flushPromises()
    expect(vm.selectDialog.projectCode).toBe('XM-2026-0003')
    expect(vm.selectDialog.ownerName).toBe('张三')
    expect(vm.selectDialog.department).toBe('法制工作部')
    expect(vm.selectDialog.approvalBasis).toBe('依据《立法法》第十条')
    expect(vm.selectDialog.description).toBe('梳理婚姻家庭领域法规,形成修订草案')
  })
})

describe('ProjectAcceptanceView 身份诊断（Bug2）', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('空列表时不再展示"当前识别登录名"诊断提示', async () => {
    mockFetch((url) => {
      if (url.includes('/centralized-projects/assigned')) {
        return { ok: true, json: async () => ({ success: true, data: [], username: 'wang' }) }
      }
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.items.length).toBe(0)
    // 提示已去掉：空列表不再出现身份诊断文案
    expect(wrapper.text()).not.toContain('当前识别登录名')
    expect(wrapper.text()).not.toContain('云端按此账号查到 0 个被指派的项目')
  })

  it('身份未识别时显示后端错误', async () => {
    mockFetch((url) => {
      if (url.includes('/centralized-projects/assigned')) {
        return { ok: true, json: async () => ({ success: false, error: '未识别登录用户，请重新登录后再查看被指派的项目' }) }
      }
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.loadError).toContain('登录')
    expect(wrapper.text()).toContain('未识别登录用户')
  })

  // ── 结项（项目负责人在本页收尾）──
  it('accepted 且全环节完成 → 出现结项按钮；部门级结项 POST 不带 move_file_ids', async () => {
    const closed: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 10, project_name: '部门项目', owner_name: 'lead', status: 'accepted', project_scope: 'department', sensitivity_level: 'important', project_code: 'XM-2026-0010', create_time: '2026-06-02T10:00:00Z' },
      ])
      if (url.match(/\/centralized-projects\/10\/stages$/)) return ok([{ status: 'completed' }, { status: 'completed' }])
      if (url.match(/\/centralized-projects\/10\/close$/) && init?.method === 'POST') {
        closed.push(JSON.parse(init.body as string)); return ok({ id: 10, status: 'closed' })
      }
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.canClose(vm.items[0])).toBe(true)
    expect(vm.rowActions(vm.items[0])).toContain('结项')

    await vm.openCloseDialog(vm.items[0])
    await flushPromises()
    expect(vm.closeDialog.isUnit).toBe(false) // 部门级：无归卷清单
    vm.closeDialog.summary = '收尾'
    await vm.doClose()
    await flushPromises()
    expect(closed[0]).toEqual({ closure_summary: '收尾' }) // 不带 move_file_ids
  })

  it('accepted 但有环节未完成 → 无结项按钮', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 11, project_name: '在途项目', owner_name: 'lead', status: 'accepted', project_scope: 'unit', sensitivity_level: 'general', project_code: 'XM-1', create_time: '2026-06-02T10:00:00Z' },
      ])
      if (url.match(/\/centralized-projects\/11\/stages$/)) return ok([{ status: 'completed' }, { status: 'in_progress' }])
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.canClose(vm.items[0])).toBe(false)
    expect(vm.rowActions(vm.items[0])).not.toContain('结项')
  })

  it('单位级结项：拉部门柜定稿清单，勾选后 POST 带 move_file_ids', async () => {
    const closed: any[] = []
    let finalUrl = ''
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/assigned')) return ok([
        { id: 12, project_name: '单位项目', owner_name: 'lead', status: 'accepted', project_scope: 'unit', sensitivity_level: 'important', project_code: 'XM-2026-0012', create_time: '2026-06-02T10:00:00Z' },
      ])
      if (url.match(/\/centralized-projects\/12\/stages$/)) return ok([{ status: 'completed' }])
      if (url.includes('/centralized-projects/12/final-files')) {
        finalUrl = url
        return ok([
          { id: 101, file_name: '定稿A.txt', bucket: 'output', sensitivity_level: 'important', storage_location: '部门重要项目档案柜' },
          { id: 102, file_name: '定稿B.txt', bucket: 'output', sensitivity_level: 'important', storage_location: '部门重要项目档案柜' },
        ])
      }
      if (url.match(/\/centralized-projects\/12\/close$/) && init?.method === 'POST') {
        closed.push(JSON.parse(init.body as string)); return ok({ id: 12, status: 'closed', _moveResult: { moved: 1 } })
      }
      return ok([])
    })
    const wrapper = mount(ProjectAcceptanceView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm

    await vm.openCloseDialog(vm.items[0])
    await flushPromises()
    expect(vm.closeDialog.isUnit).toBe(true)
    expect(finalUrl).toContain('project_code=XM-2026-0012') // 带裸编号，后端据此还原归档键
    expect(vm.closeDialog.files.length).toBe(2)
    // 单位室目标房间按密级（重要→单位档案室）
    expect(vm.unitRoomLabel('important')).toBe('单位档案室')
    expect(vm.unitRoomLabel('core')).toBe('单位保密室')
    expect(vm.unitRoomLabel('general')).toBe('单位资料室')

    vm.closeDialog.selected = [101]
    await vm.doClose()
    await flushPromises()
    expect(closed[0]).toEqual({ closure_summary: '', move_file_ids: [101] })
  })
})
