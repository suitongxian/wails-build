import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import TemplateTreeEditorView from '../views/TemplateTreeEditorView.vue'

const vuetify = createVuetify({ components, directives })

function makeRouter() {
  const r = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/template-authoring', component: { template: '<div/>' } },
      { path: '/template-authoring/:id', component: { template: '<div/>' } },
    ],
  })
  return r
}

const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })

const TREE = {
  template: {
    id: 10,
    template_code: 'TPL-LOCAL-001',
    template_name: '《明朝那些事儿》印刷计划',
    template_version: 'V1.0',
    class_code: 'IND-001',
    status: 'draft',
    is_published: 1,
    project_sensitivity_level: 'core',
    origin: 'local',
    scope: 'unit',
    short_code: 'MC-NSXS',
    manager: '刘老师',
    owner: null,
    approval_basis: null,
    description: null,
  },
  stages: [
    {
      id: 100,
      template_id: 10,
      stage_code: 'STG-001',
      stage_name: '收稿登记',
      stage_type: 'process',
      sort_order: 1,
      description: null,
      manager: '刘老师',
      members: null,
      tasks: [
        {
          id: 200,
          template_stage_id: 100,
          task_code: 'TK-001',
          task_name: '客户原稿处理',
          manager: '刘老师',
          sensitivity_level: 'core',
          sort_order: 1,
          description: null,
          file_rules: [
            {
              id: 300,
              template_stage_id: 100,
              template_task_id: 200,
              file_rule_code: 'IN-001',
              file_name: '客户原稿',
              data_state: 'input',
              required: 1,
              allowed_file_types: 'PDF,DOC',
              naming_pattern: '{书名}-原稿',
              summary_pattern: null,
              sensitivity_level: 'core',
              drafter: '刘老师',
              sort_order: 1,
            },
          ],
        },
      ],
    },
  ],
}

async function mountWith(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
  const router = makeRouter()
  router.push('/template-authoring/10')
  await router.isReady()
  const wrapper = mount(TemplateTreeEditorView, { global: { plugins: [vuetify, router] } })
  await flushPromises()
  return wrapper
}

describe('TemplateTreeEditorView', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('渲染整棵五层树：事项/任务/标识及约束字段', async () => {
    const wrapper = await mountWith((url) => {
      if (url.includes('/templates/10/tree')) return ok(TREE)
      return ok([])
    })
    const txt = wrapper.text()
    expect(txt).toContain('《明朝那些事儿》印刷计划')
    expect(txt).toContain('收稿登记') // 事项
    expect(txt).toContain('客户原稿处理') // 任务
    expect(txt).toContain('客户原稿') // 标识
    expect(txt).toContain('工作依据') // data_state 徽章显示中文（input→工作依据）
    expect(txt).not.toContain('input') // 不展示英文
    expect(txt).toContain('PDF,DOC') // 允许类型
    expect(txt).toContain('{书名}-原稿') // 命名模式
    // 三层行都渲染
    expect(wrapper.findAll('[data-test="stage-row"]').length).toBe(1)
    expect(wrapper.findAll('[data-test="task-row"]').length).toBe(1)
    expect(wrapper.findAll('[data-test="rule-row"]').length).toBe(1)
  })

  it('新建文档标识 → POST 带 task_id + data_state', async () => {
    const posted: any[] = []
    const wrapper = await mountWith((url, init) => {
      if (url.includes('/templates/10/tree')) return ok(TREE)
      if (url.endsWith('/template-file-rules') && init?.method === 'POST') {
        posted.push(JSON.parse(init.body as string))
        return ok({ id: 301 })
      }
      return ok([])
    })
    const vm: any = wrapper.vm
    vm.openCreateRule(200)
    vm.ruleDialog.file_name = '客户委托书'
    vm.ruleDialog.data_state = 'input'
    vm.ruleDialog.allowed_file_types = 'PDF'
    await vm.saveRule()
    await flushPromises()

    expect(posted.length).toBe(1)
    expect(posted[0].task_id).toBe(200)
    expect(posted[0].file_name).toBe('客户委托书')
    expect(posted[0].data_state).toBe('input')
  })

  it('责任人/参与人按 username 选择，保存时透传 username + 解析显示名（P1 防重名）', async () => {
    const posted: any[] = []
    const wrapper = await mountWith((url, init) => {
      if (url.includes('/manage-users'))
        return ok([
          { username: 'liu', display_name: '刘老师', user_unit: '院', user_department: '档案处', status: 'active' },
          { username: 'wang', display_name: '王老师', user_unit: '院', user_department: '编辑部', status: 'active' },
        ])
      if (url.endsWith('/template-stages') && init?.method === 'POST') {
        posted.push(JSON.parse(init.body as string))
        return ok({ id: 999 })
      }
      if (url.includes('/templates/10/tree')) return ok(TREE)
      return ok([])
    })
    const vm: any = wrapper.vm
    expect(vm.managers.length).toBe(2)

    vm.openCreateStage()
    vm.stageDialog.name = '排版'
    vm.stageDialog.managerUsername = 'liu'
    vm.stageDialog.membersUsernames = ['wang', 'liu']
    await vm.saveStage()

    expect(posted.length).toBe(1)
    expect(posted[0].manager_username).toBe('liu')
    expect(posted[0].members_usernames).toBe('wang,liu')
    // 显示名由 username 解析后一并存
    expect(posted[0].manager).toBe('刘老师')
    expect(posted[0].members).toBe('王老师,刘老师')
  })

  it('展开箭头一次点击即生效（默认展开→点一下收起）', async () => {
    const wrapper = await mountWith((url) => {
      if (url.includes('/templates/10/tree')) return ok(TREE)
      return ok([])
    })
    const vm: any = wrapper.vm
    // 默认（未记录）应视为展开
    expect(vm.isOpen('stage-100')).toBe(true)
    // 第一次点击应立即收起（修复前需点两下）
    vm.toggle('stage-100')
    expect(vm.isOpen('stage-100')).toBe(false)
    // 再点一次恢复展开
    vm.toggle('stage-100')
    expect(vm.isOpen('stage-100')).toBe(true)
  })

  it('改项目信息 → PUT /templates/:id（改名称等）', async () => {
    const put: any[] = []
    const wrapper = await mountWith((url, init) => {
      if (url.includes('/templates/10/tree')) return ok(TREE)
      if (url.endsWith('/templates/10') && init?.method === 'PUT') {
        put.push(JSON.parse(init.body as string))
        return ok(null)
      }
      return ok([])
    })
    const vm: any = wrapper.vm
    vm.openEditProject()
    expect(vm.projectDialog.name).toBe('《明朝那些事儿》印刷计划') // 预填当前名
    vm.projectDialog.name = '婚姻法立法项目'
    vm.projectDialog.manager = '王主任'
    await vm.saveProject()
    await flushPromises()
    expect(put.length).toBe(1)
    expect(put[0].template_name).toBe('婚姻法立法项目')
    expect(put[0].manager).toBe('王主任')
  })

  it('立项归一：模版编辑器不再有「确认立项」入口', async () => {
    const wrapper = await mountWith((url) => {
      if (url.includes('/templates/10/tree')) return ok(TREE)
      return ok([])
    })
    const initiateBtn = wrapper.findAll('button').find((b) => b.text().includes('确认立项'))
    expect(initiateBtn).toBeUndefined()
  })

  it('发布/取消发布 → POST /templates/:id/publish', async () => {
    const published: any[] = []
    const unpub = { ...TREE, template: { ...TREE.template, is_published: 0 } }
    const wrapper = await mountWith((url, init) => {
      if (url.includes('/templates/10/tree')) return ok(unpub)
      if (url.match(/\/templates\/10\/publish$/) && init?.method === 'POST') {
        published.push(JSON.parse(init.body as string))
        return ok({ is_published: true })
      }
      return ok([])
    })
    const vm: any = wrapper.vm
    expect(vm.isPublished).toBe(false)
    await vm.togglePublish()
    await flushPromises()
    expect(published.length).toBe(1)
    expect(published[0]).toEqual({ published: true })
  })

  it('删除事项 → 应用内确认后 DELETE /template-stages/:id', async () => {
    const deleted: string[] = []
    const wrapper = await mountWith((url, init) => {
      if (url.includes('/templates/10/tree')) return ok(TREE)
      if (url.includes('/template-stages/100') && init?.method === 'DELETE') {
        deleted.push(url)
        return ok(null)
      }
      return ok([])
    })
    const vm: any = wrapper.vm
    const p = vm.removeStage(TREE.stages[0]) // 阻塞在应用内确认对话框
    expect(vm.confirmBox.show).toBe(true)
    vm.confirmRespond(true) // 点"确定"
    await p
    await flushPromises()
    expect(deleted.some((u) => u.includes('/template-stages/100'))).toBe(true)
  })
})
