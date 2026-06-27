import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import WorkGroupView from '../views/WorkGroupView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })
function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}
function mountView() { return mount(WorkGroupView, { global: { plugins: [vuetify] } }) }

const WG = {
  application_id: 1, project_name: '婚姻法项目', project_code: 'XM-2026-0001', status: 'accepted',
  lead: { username: 'lead', display_name: '组长', user_unit: '第一研究院', user_department: '档案处' },
  core_members: [
    { username: 'core1', display_name: '核心一', stages: [{ stage_code: 'S1', stage_name: '收稿', status: 'in_progress' }] },
  ],
  participants: [
    { username: 'p1', display_name: '参与一', tasks: [{ stage_code: 'S1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'completed' }] },
  ],
}

describe('工作组视图', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('载入参与项目并默认选中第一项，拉取工作组详情', async () => {
    const calls: string[] = []
    mockFetch((url) => {
      calls.push(url)
      if (url.includes('/centralized-projects/involved')) return ok([
        { id: 1, project_name: '婚姻法项目', project_code: 'XM-2026-0001', status: 'accepted', owner_name: 'lead', roles: ['lead'] },
      ])
      if (url.includes('/centralized-projects/work-group')) return ok(WG)
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.projects.length).toBe(1)
    expect(vm.selectedId).toBe(1)
    // 详情拉取且带 application_id
    expect(calls.some(u => u.includes('/work-group?application_id=1'))).toBe(true)
    expect(vm.wg.lead.display_name).toBe('组长')
    // 三个角色区都渲染
    const text = wrapper.text()
    expect(text).toContain('组长（项目负责人）')
    expect(text).toContain('核心成员（环节责任人）')
    expect(text).toContain('参与人员（文件任务参与人）')
    expect(text).toContain('核心一')
    expect(text).toContain('参与一')
    expect(text).toContain('收稿 / 录入 · 已完成')
  })

  it('无参与项目时提示', async () => {
    mockFetch((url) => {
      if (url.includes('/centralized-projects/involved')) return ok([])
      return ok([])
    })
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('暂无参与的项目')
  })
})
