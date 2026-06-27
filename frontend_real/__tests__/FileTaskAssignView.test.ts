import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import FileTaskAssignView from '../views/FileTaskAssignView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })
function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}
function mountView() { return mount(FileTaskAssignView, { global: { plugins: [vuetify] } }) }

describe('文件任务指派', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('页面右上角不再有全局「工作团队」按钮（仅保留刷新）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([]) // 空列表：无行内按钮干扰
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const title = wrapper.element.querySelector('.v-card-title')
    expect(title).toBeTruthy()
    const titleBtns = Array.from(title!.querySelectorAll('button')).map(b => (b.textContent || '').trim())
    expect(titleBtns.some(t => t.includes('工作团队'))).toBe(false)
    expect(titleBtns.some(t => t.includes('刷新'))).toBe(true)
  })

  it('三 tab 分桶：未分工→待指派 / 已分工(含状态仍 pending)→实施中 / 完成→已结束', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'A', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: 'P', assigned_count: 0 },
        { id: 2, application_id: 7, stage_code: 'B', stage_name: '排版', assignee_username: 'lead', status: 'pending', project_name: 'P', assigned_count: 3 }, // 已分工但任务未开始 → 实施中
        { id: 3, application_id: 7, stage_code: 'C', stage_name: '校对', assignee_username: 'lead', status: 'in_progress', project_name: 'P', assigned_count: 2 },
        { id: 4, application_id: 7, stage_code: 'D', stage_name: '归档', assignee_username: 'lead', status: 'completed', project_name: 'P', assigned_count: 1 },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.pendingStages.map((s: any) => s.id)).toEqual([1]) // 未分工
    expect(vm.inProgressStages.map((s: any) => s.id)).toEqual([2, 3]) // 已分工未完成（含 status=pending 的 2）
    expect(vm.completedStages.map((s: any) => s.id)).toEqual([4])
    expect(vm.tab).toBe('pending')
    expect(vm.tabItems.map((s: any) => s.id)).toEqual([1])
    vm.tab = 'in_progress'
    expect(vm.tabItems.map((s: any) => s.id)).toEqual([2, 3])
    vm.tab = 'completed'
    expect(vm.tabItems.map((s: any) => s.id)).toEqual([4])
  })

  it('同一项目的工作事项按项目分组，可折叠', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'A', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目', project_code: 'XM-2026-0007', assigned_count: 0 },
        { id: 2, application_id: 7, stage_code: 'B', stage_name: '排版', assignee_username: 'lead', status: 'pending', project_name: '甲项目', project_code: 'XM-2026-0007', assigned_count: 0 },
        { id: 3, application_id: 8, stage_code: 'C', stage_name: '初审', assignee_username: 'lead', status: 'pending', project_name: '乙项目', project_code: 'XM-2026-0008', assigned_count: 0 },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    // 分组：甲项目 2 个、乙项目 1 个（待指派 tab）
    const groups = vm.groupedTabItems
    expect(groups.map((g: any) => g.project)).toEqual(['甲项目', '乙项目'])
    expect(groups.find((g: any) => g.project === '甲项目').stages.length).toBe(2)
    // 分组头带项目编号
    expect(groups.find((g: any) => g.project === '甲项目').code).toBe('XM-2026-0007')
    expect(wrapper.text()).toContain('XM-2026-0007')
    // 折叠/展开
    expect(vm.isCollapsed('甲项目')).toBe(false)
    vm.toggleProject('甲项目')
    expect(vm.isCollapsed('甲项目')).toBe(true)
    vm.toggleProject('甲项目')
    expect(vm.isCollapsed('甲项目')).toBe(false)
  })

  it('环节已完成 → 文件任务/分工 锁定：openTaskEditor / openAssign 均被拦截', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'completed', project_name: '甲项目' },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    const it = vm.items[0]
    expect(vm.stageLocked(it)).toBe(true)
    // 文件任务编辑被拦
    vm.openTaskEditor(it)
    expect(vm.taskEditor.open).toBe(false)
    expect(vm.snackbar.color).toBe('warning')
    // 分工被拦
    vm.snackbar.show = false
    await vm.openAssign(it)
    await flushPromises()
    expect(vm.assignDialog.open).toBe(false)
    expect(vm.snackbar.color).toBe('warning')
    // 工作团队也被拦（按钮置灰 + 函数级拦截）
    vm.snackbar.show = false
    await vm.openTeamForStage(it)
    await flushPromises()
    expect(vm.teamDialog.open).toBe(false)
    expect(vm.snackbar.color).toBe('warning')
    // 不再展示「已完成，不可编辑」文案
    expect(wrapper.text()).not.toContain('已完成，不可编辑')
  })

  it('分工弹窗：批量指派——勾选多个文件任务，一键指派参与人（逐项仍可用）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }, { username: 'u2', display_name: '员工二', role: 'user' }])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'in_progress', project_name: '甲项目' },
      ])
      if (url.includes('/stage-tasks')) return ok([
        { task_code: 'T1', task_name: '录入', assignee_username: null },
        { task_code: 'T2', task_name: '校对', assignee_username: null },
        { task_code: 'T3', task_name: '归档', assignee_username: null },
      ])
      if (url.includes('/stage-team')) return ok([])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAssign(vm.items[0]); await flushPromises()
    expect(vm.assignDialog.tasks.length).toBe(3)
    // 全选 → 三个任务的 task_code 都勾上
    vm.allTasksSelected = true
    expect(vm.assignDialog.batchSelected.length).toBe(3)
    // 批量指派 u1
    vm.assignDialog.batchAssignee = 'u1'
    vm.applyBatchAssignTasks()
    expect(vm.assignDialog.tasks.every((t: any) => t.assignee_username === 'u1')).toBe(true)
    expect(vm.assignDialog.batchSelected.length).toBe(0)
    expect(vm.assignDialog.batchAssignee).toBe('')
    // 逐项仍可用：单独改一个为 u2
    vm.assignDialog.tasks[2].assignee_username = 'u2'
    expect(vm.assignDialog.tasks[2].assignee_username).toBe('u2')
  })

  it('未分工(待指派)可编辑；已分工→三按钮锁定(rowLocked)', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 2, application_id: 7, stage_code: 'STG-2', stage_name: '排版', assignee_username: 'lead', status: 'pending', project_name: '甲项目', assigned_count: 0 },
        { id: 3, application_id: 7, stage_code: 'STG-3', stage_name: '校对', assignee_username: 'lead', status: 'pending', project_name: '甲项目', assigned_count: 2 }, // 已分工
      ])
      if (url.includes('/stage-tasks')) return ok([])
      if (url.includes('/stage-team')) return ok([])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    const notAssigned = vm.items[0]
    const assigned = vm.items[1]
    // 未分工 → 可编辑
    expect(vm.rowLocked(notAssigned)).toBe(false)
    vm.openTaskEditor(notAssigned)
    expect(vm.taskEditor.open).toBe(true)
    // 已分工 → 锁定：三入口均拦截
    expect(vm.rowLocked(assigned)).toBe(true)
    vm.taskEditor.open = false
    vm.openTaskEditor(assigned)
    expect(vm.taskEditor.open).toBe(false)
    vm.snackbar.show = false; await vm.openAssign(assigned); await flushPromises()
    expect(vm.assignDialog.open).toBe(false)
    vm.snackbar.show = false; await vm.openTeamForStage(assigned); await flushPromises()
    expect(vm.teamDialog.open).toBe(false)
  })

  it('分工弹窗按钮：取消 / 暂存(占位) / 确认指派', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲', assigned_count: 0 },
      ])
      if (url.includes('/stage-tasks')) return ok([{ task_code: 'T1', task_name: '录入', assignee_username: null }])
      if (url.includes('/stage-team')) return ok([])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAssign(vm.items[0]); await flushPromises()
    const dialogs = Array.from(document.querySelectorAll('.v-card')).filter(c => (c.textContent || '').includes('文件任务分工'))
    const txt = dialogs[dialogs.length - 1]?.textContent || ''
    expect(txt).toContain('确认指派')
    expect(txt).toContain('暂存')
    expect(txt).not.toContain('提交')
    // 暂存仅占位：调用只提示，不发请求/不关弹窗
    vm.stashAssign()
    expect(vm.snackbar.color).toBe('info')
    expect(vm.assignDialog.open).toBe(true)
  })

  it('分工提交前先弹预览二次确认，确认后才 POST assign-tasks', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲', assigned_count: 0 },
      ])
      if (url.includes('/stage-tasks')) return ok([{ task_code: 'T1', task_name: '录入', assignee_username: null }])
      if (url.includes('/stage-team')) return ok([])
      if (url.includes('/assign-tasks') && init?.method === 'POST') { posted.push(JSON.parse(init.body as string)); return ok({ ok: true }) }
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAssign(vm.items[0]); await flushPromises()
    vm.assignDialog.tasks[0].assignee_username = 'u1'
    // 点提交 → 只开预览，尚未 POST
    vm.openAssignPreview()
    expect(vm.assignPreview.open).toBe(true)
    expect(vm.assignPreview.rows[0].assignee).toBe('员工一')
    expect(posted.length).toBe(0)
    // 确认 → POST
    await vm.submitAssign(); await flushPromises()
    expect(posted.length).toBe(1)
    expect(vm.assignPreview.open).toBe(false)
    expect(vm.assignDialog.open).toBe(false)
  })

  it('列出我的工作环节（含指派时间列）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目', create_time: '2026-06-21 09:30:15' },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('收稿')
    expect(wrapper.text()).toContain('甲项目')
    // 列名：工作事项 / 派发时间（原「工作环节」「指派时间」）
    expect(wrapper.text()).toContain('工作事项')
    expect(wrapper.text()).toContain('派发时间')
    expect(wrapper.text()).not.toContain('工作环节')
    expect(wrapper.text()).not.toContain('指派时间')
    // 派发时间截到分钟、转中国时区（UTC 09:30 → +08:00 17:30）
    expect(wrapper.text()).toContain('2026-06-21 17:30')
    expect(wrapper.text()).not.toContain('2026-06-21 09:30:15') // 秒被截掉
  })

  it('环节被指派即成队：每行均显示「工作团队 + 分工」，不再有「组建团队」空态', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲' },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('工作团队')
    expect(text).toContain('分工')
    expect(text).not.toContain('组建团队')
  })

  it('工作团队：环节负责人始终在队(stage_lead 角色)并锁定不可移除', async () => {
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'lead', display_name: '环节负责人', role: 'user' },
        { username: 'u1', display_name: '员工一', role: 'user' },
      ])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      // 环节团队 GET：manage 端保证环节负责人在列且带角色
      if (url.includes('/stage-team') && (!init || init.method === 'GET')) return ok([
        { username: 'lead', display_name: '环节负责人', roles: ['stage_lead'] },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openTeamForStage(vm.items[0]); await flushPromises()
    expect(vm.teamDialog.members).toEqual(['lead'])
    expect(vm.teamDialog.leadUsername).toBe('lead')
    expect(vm.teamDialog.roleMap['lead']).toContain('环节负责人')
  })

  it('选择项目参与成员弹窗：负责人标签「项目事项责任人」、成员标签「项目工作人员」', async () => {
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'lead', display_name: '老张', role: 'user' }])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      if (url.includes('/stage-team') && (!init || init.method === 'GET')) return ok([
        { username: 'lead', display_name: '老张', roles: ['stage_lead'] },
      ])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openTeamForStage(vm.items[0]); await flushPromises()
    const body = document.body.textContent || ''
    expect(body).toContain('项目事项责任人') // 负责人标签（原「环节负责人」）
    expect(body).toContain('项目工作人员')   // 成员标签（原「项目参与成员」）
  })

  it('分工弹窗列任务、提交调 assign-tasks 带 assignments', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      if (url.includes('/stage-tasks')) return ok([
        { task_code: 'TK-1', task_name: '录入', assignee_username: null },
        { task_code: 'TK-2', task_name: '校对', assignee_username: 'u1' },
      ])
      if (url.includes('/assign-tasks') && init?.method === 'POST') { posted.push({ url, body: JSON.parse(init.body as string) }); return ok({ count: 1 }) }
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAssign(vm.items[0])
    await flushPromises()
    expect(vm.assignDialog.tasks.length).toBe(2)
    expect(vm.assignDialog.tasks[1].assignee_username).toBe('u1')
    vm.assignDialog.tasks[0].assignee_username = 'u1'
    await vm.submitAssign()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].url).toContain('application_id=7')
    expect(posted[0].url).toContain('stage_code=STG-1')
    expect(posted[0].body.assignments.length).toBe(2)
  })

  it('分工弹窗不再有「增加文件任务」按钮，也不暴露 openTaskEditorFromAssign（分工不可改文件任务）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      if (url.includes('/stage-tasks')) return ok([{ task_code: 'T1', task_name: '扫描归档' }])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAssign(vm.items[0]); await flushPromises()
    // 分工弹窗已载入本环节文件任务
    expect(vm.assignDialog.tasks.map((t: any) => t.task_name)).toContain('扫描归档')
    // 当前分工弹窗内无「增加文件任务」按钮（取最后一个匹配卡，避开 teleport 残留）
    const dialogs = Array.from(document.querySelectorAll('.v-card'))
      .filter(c => (c.textContent || '').includes('文件任务分工'))
    const dialog = dialogs[dialogs.length - 1]
    expect(dialog).toBeTruthy()
    expect(dialog.textContent || '').not.toContain('增加文件任务')
    // 不再暴露从分工弹窗加任务的能力
    expect(vm.openTaskEditorFromAssign).toBeUndefined()
  })

  it('工作团队：保存 POST /stage-team 带 application_id+stage_code+members', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      if (url.includes('/stage-team') && (!init || init.method === 'GET')) return ok([])
      if (url.includes('/stage-team') && init?.method === 'POST') { posted.push({ url, body: JSON.parse(init.body as string) }); return ok([{ username: 'u1' }]) }
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.canFormStageTeam).toBe(true)
    await vm.openTeamDialog()
    await flushPromises()
    expect(vm.teamDialog.stage.stage_code).toBe('STG-1')
    vm.teamDialog.members = ['u1']
    await vm.saveStageTeam()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].url).toContain('application_id=7')
    expect(posted[0].url).toContain('stage_code=STG-1')
    expect(posted[0].body.members).toEqual([{ username: 'u1', display_name: '员工一' }])
  })

  it('指派候选只含环节团队成员（团队=实际班底，非团队不出现）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'u1', display_name: '员工一', user_department: '收集科', role: 'user' },
        { username: 'u2', display_name: '员工二', user_department: '审核科', role: 'user' },
      ])
      if (url.includes('/centralized-projects/my-stages')) return ok([])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    const items = vm.assigneeItems(['u2'])
    // 只有团队成员 u2，非团队 u1 不出现
    expect(items.length).toBe(1)
    expect(items[0].username).toBe('u2')
    expect(items[0].label).toContain('员工二')
    expect(items[0].label).toContain('审核科')
  })

  it('临时拉人入队：保存后回填分工人池，新人立即可选', async () => {
    mockFetch((url, init) => {
      if (url.includes('/manage-users')) return ok([
        { username: 'u1', display_name: '员工一', user_department: '收集科', role: 'user' },
        { username: 'u2', display_name: '员工二', user_department: '审核科', role: 'user' },
      ])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      if (url.includes('/stage-tasks')) return ok([{ task_code: 'TK-1', task_name: '录入', assignee_username: null }])
      if (url.includes('/stage-team') && (!init || init.method === 'GET')) return ok([])
      if (url.includes('/stage-team') && init?.method === 'POST') return ok([{ username: 'u1' }])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAssign(vm.items[0]); await flushPromises()
    // 初始团队空 → 人池空
    expect(vm.assigneeItems(vm.assignDialog.team).length).toBe(0)
    // 打开临时拉人入队（环节锁定为当前分工环节）
    await vm.openTeamForAssign(); await flushPromises()
    expect(vm.teamDialog.locked).toBe(true)
    expect(vm.teamDialog.stage.stage_code).toBe('STG-1')
    // 拉入 u1 并保存
    vm.teamDialog.members = ['u1']
    await vm.saveStageTeam(); await flushPromises()
    // 分工人池已回填 u1，立即可选
    expect(vm.assignDialog.team).toEqual(['u1'])
    const items = vm.assigneeItems(vm.assignDialog.team)
    expect(items.length).toBe(1)
    expect(items[0].username).toBe('u1')
  })

  it('文件任务指派的拉人按钮文案为「团队参与人员调整」（区别于分工的「团队核心人员调整」）', async () => {
    mockFetch((url) => {
      if (url.includes('/manage-users')) return ok([{ username: 'u1', display_name: '员工一', role: 'user' }])
      if (url.includes('/centralized-projects/my-stages')) return ok([
        { id: 1, application_id: 7, stage_code: 'STG-1', stage_name: '收稿', assignee_username: 'lead', status: 'pending', project_name: '甲项目' },
      ])
      if (url.includes('/stage-tasks')) return ok([{ task_code: 'TK-1', task_name: '录入', assignee_username: null }])
      if (url.includes('/stage-team')) return ok([])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openAssign(vm.items[0]); await flushPromises()
    const body = document.body.textContent || ''
    expect(body).toContain('团队参与人员调整')
    expect(body).not.toContain('团队核心人员调整')
  })
})
