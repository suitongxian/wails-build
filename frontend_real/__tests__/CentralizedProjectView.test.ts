import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import CentralizedProjectView from '../views/CentralizedProjectView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })

function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}

function mountView() {
  return mount(CentralizedProjectView, { global: { plugins: [vuetify] } })
}

describe('CentralizedProjectView 立项（含定数权 / 去审核）', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('负责人下拉排除系统管理员（system_admin）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'admin', display_name: '管理员', role: 'system_admin', status: 'active' },
        { username: 'zhangsan', display_name: '张三', role: 'user', status: 'active' },
      ])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.includes('/centralized-projects')) return ok({ items: [], total: 0 })
      return ok([])
    })
    const wrapper = mount(CentralizedProjectView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    const names = vm.ownerOptions.map((o: any) => o.username)
    expect(names).toContain('zhangsan')
    expect(names).not.toContain('admin') // 管理员被排除
  })

  it('选负责人后自动填部门，且只读', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([{ username: 'zhangsan', display_name: '张三', user_department: '第一研究院', role: 'user' }])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.includes('/centralized-projects')) return ok({ items: [], total: 0 })
      return ok([])
    })
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    vm.openCreate()
    vm.dform.owner_name = 'zhangsan'
    vm.onOwnerSelected('zhangsan')
    expect(vm.dform.department).toBe('第一研究院')
  })

  it('立项书不再含「项目过程文件管理模式」录入（已移到承接环节）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'zhangsan', display_name: '张三', user_unit: '数可信研究院', user_department: '财务处', role: 'user' },
      ])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.includes('/centralized-projects')) return ok({ items: [], total: 0 })
      return ok([])
    })
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    vm.openCreate()
    vm.dform.project_scope = 'unit'
    await flushPromises()
    // 立项书弹窗(.lxs)里不应再出现 custody 录入相关文案/控件
    const lxs = document.querySelector('.lxs')
    expect(lxs?.textContent || '').not.toContain('项目过程文件管理模式')
    expect(lxs?.textContent || '').not.toContain('归档归属说明')
    expect(vm.custodyRadioOptions).toBeUndefined()
  })

  it('存草稿走 save_as_draft=true（POST），仅需项目名称', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.endsWith('/centralized-projects') && init?.method === 'POST') {
        posted.push(JSON.parse(init.body as string)); return ok({ id: 1, status: 'draft' })
      }
      if (url.includes('/centralized-projects')) return ok({ items: [], total: 0 })
      return ok([])
    })
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    vm.openCreate()
    vm.dform.project_name = '半成品'
    expect(vm.canDraft).toBe(true)
    await vm.persist(true)
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].save_as_draft).toBe(true)
    expect(posted[0].project_name).toBe('半成品')
  })

  it('发布需全部必填：缺负责人/部门时 canPublish=false', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.includes('/centralized-projects')) return ok({ items: [], total: 0 })
      return ok([])
    })
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    vm.openCreate()
    vm.dform.project_name = '正式'
    expect(vm.canPublish).toBe(false)
    vm.dform.owner_name = 'zhangsan'
    vm.dform.department = '第一研究院'
    vm.dform.sensitivity_level = 'general'
    expect(vm.canPublish).toBe(true)
  })

  it('编辑草稿走 PUT /draft 带 id', async () => {
    const puts: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.endsWith('/centralized-projects/draft') && init?.method === 'PUT') {
        puts.push(JSON.parse(init.body as string)); return ok({ id: 9, status: 'draft' })
      }
      if (url.includes('/centralized-projects')) return ok({ items: [], total: 0 })
      return ok([])
    })
    const wrapper = mountView()
    await flushPromises()
    const vm: any = wrapper.vm
    vm.openEdit({ id: 9, project_name: '草稿', project_code: null, owner_name: '', department: null, sensitivity_level: 'general', data_owner: null, approval_basis: null, description: null })
    await vm.persist(true)
    await flushPromises()
    expect(puts.length).toBe(1)
    expect(puts[0].id).toBe(9)
  })

  it('负责人列展示姓名而非 username', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([{ username: 'zhangsan', display_name: '张三', role: 'user' }])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.includes('/centralized-projects?')) return ok({ items: [
        { id: 1, project_name: 'P', owner_name: 'zhangsan', data_owner: '院', status: 'approved', sensitivity_level: 'general', manage_remote_id: null, sync_status: 'synced', create_time: '2026-06-02T10:00:00Z' },
      ], total: 1 })
      return ok([])
    })
    const wrapper = mount(CentralizedProjectView, { global: { plugins: [vuetify] } })
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.ownerDisplay('zhangsan')).toBe('张三')
    expect(wrapper.text()).toContain('张三')
  })

  it('结项已迁出立项页：不再有结项列/按钮，也不再拉环节进度', async () => {
    const calledStages: string[] = []
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.match(/\/stages$/)) { calledStages.push(url); return ok([{ status: 'completed' }]) }
      if (url.includes('/centralized-projects?')) return ok({ items: [
        { id: 1, project_name: '已交付待收尾项目', owner_name: 'lead', data_owner: '院', status: 'accepted', sensitivity_level: 'general', manage_remote_id: 77, sync_status: 'synced', create_time: '2026-06-02T10:00:00Z' },
      ], total: 1 })
      return ok([])
    })
    const wrapper = mount(CentralizedProjectView, { global: { plugins: [vuetify] } })
    await flushPromises()
    // 立项页（立项人视角）不再承担结项：无结项按钮、表头无「结项」、不再为每行拉环节进度
    expect(wrapper.text()).not.toContain('结项')
    expect(calledStages.length).toBe(0)
  })

  it('草稿行展示「草稿」状态与「编辑」按钮', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.includes('/centralized-projects?')) return ok({ items: [
        { id: 5, project_name: '草稿X', owner_name: '', data_owner: null, status: 'draft', sensitivity_level: 'general', manage_remote_id: null, sync_status: 'pending', sync_error: null, reject_reason: null, reviewed_at: null, create_time: '2026-06-08T10:00:00Z', submitted_by: 'u1', project_code: null, department: null, approval_basis: null, description: null },
      ], total: 1 })
      if (url.includes('/centralized-projects')) return ok({ items: [], total: 0 })
      return ok([])
    })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('草稿')
    expect(wrapper.text()).toContain('编辑')
    expect(wrapper.text()).not.toContain('待推送') // 草稿不显示同步状态
  })
})

describe('CentralizedProjectView 列表增强：立项人列 / 文案 / 查看', () => {
  beforeEach(() => vi.restoreAllMocks())

  function mountEmpty() {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/refresh')) return ok({ updated: 0 })
      if (url.includes('/centralized-projects')) return ok({ items: [], total: 0 })
      return ok([])
    })
    return mount(CentralizedProjectView, { global: { plugins: [vuetify] } })
  }

  it('列调整：移除立项人/项目周期/数据权属列；提交时间→立项日期；负责人→项目负责人；状态→工程进展；保留整体完成率', async () => {
    const wrapper = mountEmpty()
    await flushPromises()
    const vm: any = wrapper.vm
    const keys = vm.headers.map((h: any) => h.key)
    // 已移除的列
    expect(keys).not.toContain('submitted_by')
    expect(keys).not.toContain('cycle')
    expect(keys).not.toContain('data_owner')
    expect(keys).not.toContain('sync_status')
    // 文案改名
    expect(vm.headers.find((h: any) => h.key === 'owner_name').title).toBe('项目负责人')
    expect(vm.headers.find((h: any) => h.key === 'status').title).toBe('工程进展')
    expect(vm.headers.find((h: any) => h.key === 'create_time').title).toBe('立项日期')
    // 保留整体完成率
    expect(keys).toContain('completion_rate')
  })

  it('工程进展文案：已立项/已承接/分工中/受理中/已结项', async () => {
    const wrapper = mountEmpty()
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.statusLabel('approved')).toBe('已立项')
    expect(vm.statusLabel('taken')).toBe('已承接')
    expect(vm.statusLabel('assigning')).toBe('分工中')
    expect(vm.statusLabel('accepted')).toBe('受理中')
    expect(vm.statusLabel('closed')).toBe('已结项')
  })

  it('已推送文案改为"已推送给项目负责人"', async () => {
    const wrapper = mountEmpty()
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.syncStatusLabel('synced')).toBe('已推送给项目负责人')
    expect(vm.syncStatusLabel('pending')).toBe('待推送')
  })

  it('查看按钮打开只读详情弹窗，含全部立项信息', async () => {
    const wrapper = mountEmpty()
    await flushPromises()
    const vm: any = wrapper.vm
    const item = { id: 9, project_name: '二季度核算', submitted_by: 'zhang', owner_name: 'wang', status: 'approved', sync_status: 'synced' }
    vm.viewItem(item)
    await flushPromises()
    expect(vm.viewDialog.open).toBe(true)
    expect(vm.viewDialog.item.id).toBe(9)
    // v-dialog 内容 teleport 到 body，wrapper.text() 抓不到；校验绑定数据 + body 渲染
    expect(vm.viewDialog.item.project_name).toBe('二季度核算')
    expect(document.body.textContent).toContain('二季度核算')
  })

  it('查看弹窗为立项人视角的项目跟踪面板（进展/完成率/周期/负责人），只读', async () => {
    const wrapper = mountEmpty()
    await flushPromises()
    const vm: any = wrapper.vm
    const item = {
      id: 9, project_code: 'XM-2026-0009', project_name: '二季度核算',
      submitted_by: 'zhang', owner_name: 'wang', department: '财务处',
      data_owner: '财务处', sensitivity_level: 'important',
      project_scope: 'unit', status: 'assigning', sync_status: 'synced',
      cycle_start: '2026-07-01', cycle_end: '2999-12-31', completion_rate: 40,
      approval_basis: '依据财务制度', description: '二季度账目核算',
      create_time: '2026-06-02T10:00:00Z',
    }
    vm.viewItem(item)
    await flushPromises()
    // 断言局限在查看面板 .pv 内；取最后一个（jsdom 不清理上一个用例 teleport 的残留节点）
    const pvs = document.querySelectorAll('.pv')
    const dialog = pvs[pvs.length - 1]
    expect(dialog).toBeTruthy()
    const body = dialog!.textContent || ''
    // 抬头：项目名 + 编号
    expect(body).toContain('二季度核算')
    expect(body).toContain('XM-2026-0009')
    // 关键指标
    expect(body).toContain('工程进展')
    expect(body).toContain('分工中')        // assigning 工程进展
    expect(body).toContain('整体完成率')
    expect(body).toContain('40')            // 完成率
    expect(body).toContain('项目周期')
    expect(body).toContain('2026-07-01 ~ 2999-12-31')
    expect(body).toContain('项目负责人')
    // 步骤条五档都在
    for (const s of ['已立项', '已承接', '分工中', '受理中', '已结项']) expect(body).toContain(s)
    // 依据/简介仍可看
    expect(body).toContain('依据财务制度')
    expect(body).toContain('二季度账目核算')
    // 面板本身不再是公文式立项书
    expect(body).not.toContain('项 目 立 项 书')
    // 只读：无输入框
    expect(dialog!.querySelectorAll('input, textarea').length).toBe(0)
  })

  it('面板派生指标：步骤序号 / 周期剩余·超期 / 完成率配色', async () => {
    const wrapper = mountEmpty()
    await flushPromises()
    const vm: any = wrapper.vm
    // 步骤序号
    expect(vm.statusStepIndex('approved')).toBe(0)
    expect(vm.statusStepIndex('assigning')).toBe(2)
    expect(vm.statusStepIndex('closed')).toBe(4)
    expect(vm.statusStepIndex('draft')).toBe(-1) // 非正常流不进步骤条
    // 周期：未设定 / 超期 / 剩余 / 已结项
    expect(vm.cycleInfo({ cycle_start: '', cycle_end: '', status: 'taken' }).range).toBe('未设定')
    // 只填一端不展示「?」：仅结束→「截止 Y」；仅起始→「X 起」
    expect(vm.cycleInfo({ cycle_start: '', cycle_end: '2026-08-29', status: 'accepted' }).range).toBe('截止 2026-08-29')
    expect(vm.cycleInfo({ cycle_start: '2026-07-01', cycle_end: '', status: 'accepted' }).range).toBe('2026-07-01 起')
    expect(vm.cycleInfo({ cycle_start: '', cycle_end: '2026-08-29', status: 'accepted' }).range).not.toContain('?')
    expect(vm.cycleInfo({ cycle_start: '2000-01-01', cycle_end: '2000-02-01', status: 'accepted' }).range).toBe('2000-01-01 ~ 2000-02-01')
    expect(vm.cycleInfo({ cycle_start: '2000-01-01', cycle_end: '2000-02-01', status: 'accepted' }).sub).toContain('已超期')
    const future = vm.cycleInfo({ cycle_start: '2999-01-01', cycle_end: '2999-12-31', status: 'accepted' })
    expect(future.sub).toContain('剩')
    expect(vm.cycleInfo({ cycle_start: '2000-01-01', cycle_end: '2000-02-01', status: 'closed' }).sub).toBe('已结项')
    // 完成率配色
    expect(vm.rateColor(null)).toBe('grey')
    expect(vm.rateColor(100)).toBe('success')
    expect(vm.rateColor(10)).toBe('warning')
  })
})
