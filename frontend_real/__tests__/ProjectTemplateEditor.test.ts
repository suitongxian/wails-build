import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import ProjectTemplateEditor from '../components/ProjectTemplateEditor.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })
function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}
function tree() {
  return {
    template_id: 50,
    template_code: 'TPL-PRJ-42',
    // 与后端 GetLocalTemplateTree 一致的「扁平 + 小写键」结构
    tree: {
      template: { template_code: 'TPL-PRJ-42' },
      stages: [
        { id: 1, stage_code: 'STG-001', stage_name: '收稿', description: null, manager: '张三', manager_username: 'zhangsan', members: '李四', members_usernames: 'lisi',
          tasks: [{ id: 11, task_code: 'TK-001', task_name: '录入', description: null, sensitivity_level: 'general', manager: '王五', file_rules: [{ id: 101, file_rule_code: 'FR-1', file_name: '登记表', data_state: 'process', required: 1, allowed_file_types: 'DOCX', naming_pattern: '登记表', summary_pattern: null, default_retention_policy: null, sensitivity_level: 'general', drafter: null, category: null, security_requirement: null, diffusion_requirement: null, archive_requirement: null, retention_period_days: null, destruction_rule: null }] }] },
        { id: 2, stage_code: 'STG-002', stage_name: '空环节', description: null, manager: null, manager_username: null, members: null, members_usernames: null, tasks: [] },
      ],
    },
  }
}
function mountEditor(props: any) {
  return mount(ProjectTemplateEditor, { props: { modelValue: false, applicationId: 42, mode: 'stage', ...props }, global: { plugins: [vuetify] } })
}

describe('项目专属模版编辑器', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('stage 模式：打开载入项目专属模版并展示工作事项', async () => {
    let loadUrl = ''
    mockFetch((url) => {
      if (url.includes('/project-template?')) { loadUrl = url; return ok(tree()) }
      return ok({})
    })
    const wrapper = mountEditor({ mode: 'stage' })
    await wrapper.setProps({ modelValue: true }); await flushPromises()
    const vm: any = wrapper.vm
    expect(loadUrl).toContain('application_id=42')
    expect(vm.templateId).toBe(50)
    expect(vm.stages.length).toBe(2)
    const body = document.body.textContent || ''
    expect(body).toContain('编辑工作事项')
    expect(body).toContain('收稿')
    expect(body).toContain('空环节')
    // 完整框架：环节下的文件任务、以及加文件任务/加标识入口都应呈现（不只是名称+描述）
    expect(body).toContain('录入')        // 收稿环节下的文件任务
    expect(body).toContain('加文件任务')
    expect(body).toContain('加标识')
    expect(body).toContain('文档标识')
  })

  it('task 模式：聚焦空环节时提示补齐文件任务', async () => {
    mockFetch((url) => url.includes('/project-template?') ? ok(tree()) : ok({}))
    const wrapper = mountEditor({ mode: 'task', stageCode: 'STG-002', stageName: '空环节' })
    await wrapper.setProps({ modelValue: true }); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.focusStage?.stage_code).toBe('STG-002')
    expect(vm.focusStage?.tasks.length).toBe(0)
    expect(document.body.textContent || '').toContain('请补齐')
  })

  it('编辑工作事项：PUT 回传原有责任人/参与人，不被清空', async () => {
    let putBody: any = null
    mockFetch((url, init) => {
      if (url.includes('/project-template?')) return ok(tree())
      if (url.includes('/template-stages/') && init?.method === 'PUT') { putBody = JSON.parse(init.body as string); return ok({}) }
      return ok({})
    })
    const wrapper = mountEditor({ mode: 'stage' })
    await wrapper.setProps({ modelValue: true }); await flushPromises()
    const vm: any = wrapper.vm
    vm.openStageEdit(vm.stages[0])      // 编辑"收稿"
    vm.stageDialog.name = '收稿改名'
    await vm.saveStage(); await flushPromises()
    expect(putBody.name).toBe('收稿改名')
    // 关键：原有责任人/参与人随 PUT 回传，避免被 UpdateStage 清空
    expect(putBody.manager).toBe('张三')
    expect(putBody.manager_username).toBe('zhangsan')
    expect(putBody.members_usernames).toBe('lisi')
  })

  it('新建文档标识：POST 携带 L6 管控字段', async () => {
    let postBody: any = null
    mockFetch((url, init) => {
      if (url.includes('/project-template?')) return ok(tree())
      if (url.includes('/template-file-rules') && init?.method === 'POST') { postBody = JSON.parse(init.body as string); return ok({}) }
      return ok({})
    })
    const wrapper = mountEditor({ mode: 'task', stageCode: 'STG-001', stageName: '收稿' })
    await wrapper.setProps({ modelValue: true }); await flushPromises()
    const vm: any = wrapper.vm
    vm.openRuleCreate(vm.focusStage.tasks[0])
    Object.assign(vm.ruleDialog, {
      file_name: '登记定稿', data_state: 'output', category: '工作文档', security: '加密存储',
      diffusion: '双孤本模式', archiveReq: '部门文件柜', retentionDays: 1825, destruction: '满期销毁',
    })
    await vm.saveRule(); await flushPromises()
    expect(postBody.category).toBe('工作文档')
    expect(postBody.security_requirement).toBe('加密存储')
    expect(postBody.diffusion_requirement).toBe('双孤本模式')
    expect(postBody.archive_requirement).toBe('部门文件柜')
    expect(postBody.retention_period_days).toBe(1825)
    expect(postBody.destruction_rule).toBe('满期销毁')
  })

  it('过程文件不允许 PDF：填 PDF 时拦截，不发 POST', async () => {
    let posted = false
    mockFetch((url, init) => {
      if (url.includes('/project-template?')) return ok(tree())
      if (url.includes('/template-file-rules') && init?.method === 'POST') { posted = true; return ok({}) }
      return ok({})
    })
    const wrapper = mountEditor({ mode: 'task', stageCode: 'STG-001', stageName: '收稿' })
    await wrapper.setProps({ modelValue: true }); await flushPromises()
    const vm: any = wrapper.vm
    vm.openRuleCreate(vm.focusStage.tasks[0])
    Object.assign(vm.ruleDialog, { file_name: '草稿', allowed: 'pdf' }) // 大小写不敏感
    await vm.saveRule(); await flushPromises()
    expect(posted).toBe(false)
    expect(vm.snack.text).toContain('过程文件不允许使用 PDF')
  })

  it('文件任务没有文件标识时，保存并同步被拦截（不能只加空任务）', async () => {
    let posted = false
    const emptyTree = () => ({
      template_id: 50, template_code: 'TPL-PRJ-42',
      tree: { template: { template_code: 'TPL-PRJ-42' }, stages: [
        { id: 1, stage_code: 'STG-001', stage_name: '收稿', tasks: [
          { id: 11, task_code: 'TK-001', task_name: '录入', sensitivity_level: 'general', file_rules: [] },
        ] },
      ] },
    })
    mockFetch((url, init) => {
      if (url.includes('/project-template?')) return ok(emptyTree())
      if (url.includes('/save-project-template')) { posted = true; return ok({ template_id: 50 }) }
      return ok({})
    })
    const wrapper = mountEditor({ mode: 'task', stageCode: 'STG-001', stageName: '收稿' })
    await wrapper.setProps({ modelValue: true }); await flushPromises()
    const vm: any = wrapper.vm
    await vm.saveAndSync(); await flushPromises()
    expect(posted).toBe(false)
    expect(vm.snack.text).toContain('还没有文件标识')
  })

  it('保存并同步：POST save-project-template 后关闭并触发 saved', async () => {
    let saveUrl = ''
    mockFetch((url, init) => {
      if (url.includes('/project-template?')) return ok(tree())
      if (url.includes('/save-project-template')) { saveUrl = url; return ok({ template_id: 50 }) }
      return ok({})
    })
    const wrapper = mountEditor({ mode: 'stage' })
    await wrapper.setProps({ modelValue: true }); await flushPromises()
    const vm: any = wrapper.vm
    await vm.saveAndSync(); await flushPromises()
    expect(saveUrl).toContain('application_id=42')
    expect(wrapper.emitted('saved')).toBeTruthy()
    expect(wrapper.emitted('update:modelValue')?.some((e: any) => e[0] === false)).toBe(true)
  })
})
