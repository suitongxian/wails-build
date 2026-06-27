import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import FileTaskReceiveView from '../views/FileTaskReceiveView.vue'

const vuetify = createVuetify({ components, directives })
const ok = (data: any) => ({ ok: true, json: async () => ({ success: true, data }) })
function mockFetch(handler: (url: string, init?: RequestInit) => any) {
  global.fetch = vi.fn(async (input: any, init?: any) => handler(String(input), init)) as any
}
function mountView() {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: { template: '<div/>' } },
  ] })
  return { wrapper: mount(FileTaskReceiveView, { global: { plugins: [vuetify, router] } }), router }
}

describe('文件任务受理', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('列出我的文件任务', async () => {
    mockFetch((url) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'pending', project_name: '甲项目', template_code: 'TPL-X' },
      ])
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    expect(wrapper.text()).toContain('录入')
    expect(wrapper.text()).toContain('甲项目')
    expect(wrapper.text()).toContain('开始工作')
  })

  it('待办/进行中/已完成改为 tab：默认显示待办，切到指定 tab 才展示对应任务', async () => {
    mockFetch((url) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '待办任务甲', status: 'pending', project_name: '甲项目', template_code: 'TPL-X' },
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-2', task_name: '进行任务乙', status: 'in_progress', project_name: '甲项目', template_code: 'TPL-X' },
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-3', task_name: '完成任务丙', status: 'completed', project_name: '甲项目', template_code: 'TPL-X' },
      ])
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    // 默认 tab=todo：仅待办任务可见，进行中/已完成尚未渲染
    expect(vm.tab).toBe('todo')
    expect(wrapper.text()).toContain('待办任务甲')
    expect(wrapper.text()).not.toContain('进行任务乙')
    expect(wrapper.text()).not.toContain('完成任务丙')
    // 切到进行中 tab
    vm.tab = 'doing'; await flushPromises()
    expect(wrapper.text()).toContain('进行任务乙')
    // 切到已完成 tab
    vm.tab = 'done'; await flushPromises()
    expect(wrapper.text()).toContain('完成任务丙')
  })

  it('默认 tab：待办为空、进行中有 → 默认进行中', async () => {
    mockFetch((url) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-2', task_name: '进行任务乙', status: 'in_progress', project_name: '甲项目', template_code: 'TPL-X' },
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-3', task_name: '完成任务丙', status: 'completed', project_name: '甲项目', template_code: 'TPL-X' },
      ])
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.tab).toBe('doing')
    expect(wrapper.text()).toContain('进行任务乙')
  })

  it('默认 tab：待办+进行中为空、已完成有 → 默认已完成', async () => {
    mockFetch((url) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-3', task_name: '完成任务丙', status: 'completed', project_name: '甲项目', template_code: 'TPL-X' },
      ])
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.tab).toBe('done')
    expect(wrapper.text()).toContain('完成任务丙')
  })

  it('开始工作调 start-task 后内联打开在线编辑（不跳旧工作台）', async () => {
    const posted: any[] = []
    let filesFetched = false
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'pending', project_name: '甲项目', project_code: 'XM-2026-0007', template_code: 'TPL-X', template_version: 'V1.0' },
      ])
      if (url.includes('/start-task') && init?.method === 'POST') { posted.push(JSON.parse(init.body as string)); return ok({ scaffolded: 1, app_id: 7, stage_code: 'STG-1' }) }
      if (url.includes('/workbench/files')) { filesFetched = true; return ok({ buckets: { input: [], process: [{ name: 'a.txt', size: 0, mod_time: '', is_dir: false }], output: [] } }) }
      if (url.includes('/workbench/doc')) return ok({ name: 'a.txt', content: '', editable: true })
      return ok([])
    })
    const { wrapper, router } = mountView(); await flushPromises()
    const push = vi.spyOn(router, 'push')
    const vm: any = wrapper.vm
    await vm.startWork(vm.items[0])
    await flushPromises()
    expect(posted[0].application_id).toBe(7)
    expect(posted[0].task_code).toBe('TK-1')
    expect(posted[0].template_code).toBe('TPL-X')
    expect(posted[0].template_version).toBe('V1.0')
    // 立项编号随 start-task 下发，后端据此命名目录
    expect(posted[0].project_code).toBe('XM-2026-0007')
    // 不再跳 /stage-workbench；改为内联打开在线编辑（拉 workbench/files）
    expect(push).not.toHaveBeenCalled()
    expect(filesFetched).toBe(true)
    expect(vm.editorDialog).toBe(true)
  })

  it('在线编辑保存调 workbench/doc(process)', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', template_code: 'TPL-X', template_version: 'V1.0' },
      ])
      if (url.includes('/workbench/files')) return ok({ buckets: { input: [], process: [{ name: 'a.txt', size: 0, mod_time: '', is_dir: false }], output: [] } })
      if (url.includes('/workbench/doc') && init?.method === 'POST') { posted.push(JSON.parse(init.body as string)); return ok({ path: '/x/process/a.txt' }) }
      if (url.includes('/workbench/doc')) return ok({ name: 'a.txt', content: '老内容', editable: true })
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openEditor(vm.items[0])
    await flushPromises()
    // 本机打开优先,不再自动灌入在线编辑框;用户点「在线编辑」才加载(兜底)
    await vm.selectDoc('a.txt')
    await flushPromises()
    vm.content = '新内容'
    await vm.saveDoc()
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].bucket).toBe('process')
    expect(posted[0].name).toBe('a.txt')
    expect(posted[0].content).toBe('新内容')
  })

  it('点击文件优先调本机打开 workbench/open', async () => {
    const opened: string[] = []
    mockFetch((url) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', project_code: 'XM-2026-0007', template_code: 'TPL-X', template_version: 'V1.0' },
      ])
      if (url.includes('/workbench/files')) return ok({ buckets: { input: [], process: [{ name: '方案.docx', size: 0, mod_time: '', is_dir: false }], output: [] } })
      if (url.includes('/workbench/open')) { opened.push(url); return ok({ path: '/x/process/方案.docx' }) }
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openEditor(vm.items[0])
    await flushPromises()
    await vm.openLocal('process', '方案.docx')
    await flushPromises()
    expect(opened.length).toBe(1)
    expect(opened[0]).toContain('/workbench/open')
    expect(opened[0]).toContain('bucket=process')
    expect(opened[0]).toContain(encodeURIComponent('方案.docx'))
    // 立项编号随之带上,后端据此定位目录
    expect(opened[0]).toContain('project_code=XM-2026-0007')
    // 五层落盘:task_code 随之带上,后端定位到文件任务目录
    expect(opened[0]).toContain('task_code=TK-1')
  })

  it('打开文档时拉取并展示该任务的文档属性（应交文件及要求）', async () => {
    let ruleUrl = ''
    mockFetch((url) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', project_code: 'XM-2026-0007', template_code: 'TPL-X', template_version: 'V1.0' },
      ])
      if (url.includes('/workbench/files')) return ok({ buckets: { input: [], process: [], output: [] } })
      if (url.includes('/task-file-rules')) {
        ruleUrl = url
        return ok([
          { file_rule_code: 'IN-001', file_name: '客户原稿', data_state: 'input', required: 0, allowed_file_types: 'PDF', naming_pattern: null, summary_pattern: '客户提供的原始书稿', sensitivity_level: 'general', drafter: null },
          { file_rule_code: 'OUT-001', file_name: '登记定稿', data_state: 'output', required: 1, allowed_file_types: 'PDF', naming_pattern: '登记-{date}', summary_pattern: null, sensitivity_level: 'important', drafter: '张三' },
        ])
      }
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openEditor(vm.items[0])
    await flushPromises()
    // 按 stage/task/template 拉取
    expect(ruleUrl).toContain('stage_code=STG-1')
    expect(ruleUrl).toContain('task_code=TK-1')
    expect(ruleUrl).toContain('template_code=TPL-X')
    expect(vm.fileRules.length).toBe(2)
    // 标签转换正确
    expect(vm.dataStateLabel('input')).toBe('工作依据')
    expect(vm.dataStateLabel('output')).toBe('定稿')
    expect(vm.sensLabel('important')).toBe('重要')
    // 文档属性渲染：弹窗 teleport 到 body，断言 body 文本
    const body = document.body.textContent || ''
    expect(body).toContain('文档属性')
    expect(body).toContain('客户原稿')
    expect(body).toContain('登记定稿')
    expect(body).toContain('内容要求')              // 列标题
    expect(body).toContain('客户提供的原始书稿')      // summary_pattern 内容
    expect(body).toContain('重要')     // important 密级
    expect(body).toContain('张三')
    expect(body).toContain('登记-{date}')
  })

  it('工作受理窗口含 工作依据/参考文件/过程文档/结果文件 四区，参考文件可导入', async () => {
    let importInit: any = null
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', project_code: 'XM-2026-0007', template_code: 'TPL-X', template_version: 'V1.0' },
      ])
      if (url.includes('/workbench/files')) return ok({ buckets: {
        input: [{ name: '上游来料.pdf', size: 1, mod_time: '', is_dir: false }],
        reference: [{ name: '外部规范.pdf', size: 1, mod_time: '', is_dir: false }],
        process: [{ name: 'a.txt', size: 0, mod_time: '', is_dir: false }],
        output: [{ name: '定稿.pdf', size: 1, mod_time: '', is_dir: false }],
      } })
      if (url.includes('/workbench/import-reference')) { importInit = init; return ok({ name: '新导入.pdf', path: '/x/新导入.pdf' }) }
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openEditor(vm.items[0])
    await flushPromises()
    // 四区都在；参考/结果文件名渲染
    const body = document.body.textContent || ''
    expect(body).toContain('工作依据')
    expect(body).toContain('参考文件')
    expect(body).toContain('结果文件')
    expect(body).toContain('外部规范.pdf')
    expect(body).toContain('定稿.pdf')
    expect(vm.referenceDocs.length).toBe(1)
    expect(vm.outputDocs.length).toBe(1)
    // 导入：选中文件 → 先弹「归类定级」窗 → 确认后 POST multipart 到 import-reference
    const file = new File([new Uint8Array([1, 2, 3])], '新导入.pdf', { type: 'application/pdf' })
    await vm.onRefFilePicked({ target: { files: [file], value: '' } } as any)
    await flushPromises()
    expect(vm.refImportDialog).toBe(true)
    // 切到外部资料 → 级别默认改为一般
    vm.onRefCategoryChange('external')
    expect(vm.refForm.sensitivity_level).toBe('general')
    await vm.confirmImportReference()
    await flushPromises()
    expect(importInit?.method).toBe('POST')
    expect(importInit?.body instanceof FormData).toBe(true)
    // 携带导入者声明的类别与级别
    expect(importInit?.body.get('category')).toBe('external')
    expect(importInit?.body.get('sensitivity_level')).toBe('general')
  })

  it('本机打开失败才浮出在线编辑按钮，点按钮弹出在线编辑窗口', async () => {
    mockFetch((url) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', project_code: 'XM-2026-0007', template_code: 'TPL-X', template_version: 'V1.0' },
      ])
      if (url.includes('/workbench/files')) return ok({ buckets: { input: [], process: [{ name: 'a.txt', size: 0, mod_time: '', is_dir: false }], output: [] } })
      // 本机打开返回失败（无关联程序）
      if (url.includes('/workbench/open')) return { ok: true, json: async () => ({ success: false, error: '无关联程序' }) }
      if (url.includes('/workbench/doc')) return ok({ name: 'a.txt', content: '占位', editable: true })
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.openEditor(vm.items[0])
    await flushPromises()
    // 打开窗口时无兜底按钮、在线编辑窗口未弹出
    expect(vm.openFailed).toBe(null)
    expect(vm.docDialog).toBe(false)
    // 点文件→本机打开失败→记录该文件，浮出兜底按钮
    await vm.openLocal('process', 'a.txt')
    await flushPromises()
    expect(vm.openFailed).toEqual({ bucket: 'process', name: 'a.txt' })
    expect(vm.docDialog).toBe(false)
    // 点「在线编辑」按钮→才弹出在线编辑窗口并载入内容
    vm.openInlineEditor('process', 'a.txt')
    await flushPromises()
    expect(vm.docDialog).toBe(true)
    expect(vm.currentDoc).toBe('a.txt')
    expect(vm.content).toBe('占位')
  })

  it('一键归档：对我经手的去重项目逐个调 quick-archive 并汇总', async () => {
    const posted: Array<{ url: string; body: any }> = []
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', project_code: 'XM-1', project_scope: 'person', sensitivity_level: 'core' },
        { application_id: 7, stage_code: 'STG-2', stage_name: '排版', task_code: 'TK-2', task_name: '排版', status: 'completed', project_name: '甲项目', project_code: 'XM-1', project_scope: 'person', sensitivity_level: 'core' },
        { application_id: 8, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-9', task_name: '录入', status: 'pending', project_name: '乙项目', project_code: 'XM-2', project_scope: 'unit', sensitivity_level: 'important' },
      ])
      if (url.includes('/quick-archive') && init?.method === 'POST') {
        posted.push({ url, body: JSON.parse(init.body as string) })
        return ok({ route: '本地个人夹', archived: 1, skipped: 0 })
      }
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.onQuickArchive(); await flushPromises()
    // 两个去重项目各调一次（7 出现两次只调一次）
    expect(posted.length).toBe(2)
    const p7 = posted.find(p => p.url.includes('/centralized-projects/7/quick-archive'))
    const p8 = posted.find(p => p.url.includes('/centralized-projects/8/quick-archive'))
    expect(p7).toBeTruthy()
    expect(p8).toBeTruthy()
    // 立项层级 + 敏感级随请求体带入（参与人本机无 cpa 行也能正确路由）
    expect(p7!.body.project_scope).toBe('person')
    expect(p7!.body.sensitivity_level).toBe('core')
    expect(p7!.body.project_code).toBe('XM-1')
    expect(p8!.body.project_scope).toBe('unit')
    expect(vm.snack?.show ?? vm.snackbar.show).toBe(true)
    expect((vm.snackbar.text as string)).toContain('新归档 2')
  })

  it('完成有定稿标识的任务：先挑定稿→submit-task-finals→complete-task', async () => {
    const finalsPosted: any[] = []
    const completePosted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', project_code: 'XM-2026-0007', template_code: 'TPL-X', template_version: 'V1.0' },
      ])
      if (url.includes('/task-finals-candidates')) return ok({ output_rules: [{ file_rule_code: 'OUT-1', file_name: '登记定稿', allowed_file_types: 'docx' }], process_files: ['登记表.docx'] })
      if (url.includes('/submit-task-finals') && init?.method === 'POST') { finalsPosted.push(JSON.parse(init.body as string)); return ok({ finals: ['/x/output/登记定稿.docx'] }) }
      if (url.includes('/complete-task') && init?.method === 'POST') { completePosted.push(JSON.parse(init.body as string)); return ok({ status: 'completed' }) }
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.startComplete(vm.items[0])
    await flushPromises()
    // 有 output 标识 → 弹定稿框，未直接完成
    expect(vm.finalsDialog).toBe(true)
    expect(completePosted.length).toBe(0)
    expect(vm.canSubmitFinals).toBe(false) // 还没挑
    // 挑一个过程文件作定稿
    vm.finalsPick['OUT-1'] = '登记表.docx'
    await flushPromises()
    expect(vm.canSubmitFinals).toBe(true)
    await vm.submitFinalsAndComplete()
    await flushPromises()
    // 先提交定稿（带 task_code + selection），再完成任务
    expect(finalsPosted.length).toBe(1)
    expect(finalsPosted[0].task_code).toBe('TK-1')
    expect(finalsPosted[0].selections[0]).toEqual({ file_rule_code: 'OUT-1', source_file: '登记表.docx' })
    expect(completePosted.length).toBe(1)
    expect(completePosted[0].task_code).toBe('TK-1')
  })

  it('无定稿标识但有过程文件：通用模式弹窗→选一个作定稿→submit-task-finals(file_rule_code 空)', async () => {
    const finalsPosted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', project_code: 'XM-7', template_code: 'TPL-X' },
      ])
      if (url.includes('/task-finals-candidates')) return ok({ output_rules: [], process_files: ['登记表.docx'] })
      if (url.includes('/submit-task-finals') && init?.method === 'POST') { finalsPosted.push(JSON.parse(init.body as string)); return ok({ finals: ['/x/output/登记表.docx'] }) }
      if (url.includes('/complete-task') && init?.method === 'POST') return ok({ status: 'completed' })
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.startComplete(vm.items[0])
    await flushPromises()
    // 总是弹窗；无定稿标识 → 通用模式
    expect(vm.finalsDialog).toBe(true)
    expect(vm.isGenericFinals).toBe(true)
    expect(vm.canSubmitFinals).toBe(false) // 还没挑
    vm.finalsGenericPick = '登记表.docx'
    await flushPromises()
    expect(vm.canSubmitFinals).toBe(true)
    await vm.submitFinalsAndComplete()
    await flushPromises()
    expect(finalsPosted.length).toBe(1)
    expect(finalsPosted[0].selections[0]).toEqual({ file_rule_code: '', source_file: '登记表.docx' })
  })

  it('无定稿标识且过程文件全空：弹窗给「不留定稿直接完成」', async () => {
    const completePosted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', template_code: 'TPL-X' },
      ])
      if (url.includes('/task-finals-candidates')) return ok({ output_rules: [], process_files: [] })
      if (url.includes('/complete-task') && init?.method === 'POST') { completePosted.push(JSON.parse(init.body as string)); return ok({ status: 'completed' }) }
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.startComplete(vm.items[0])
    await flushPromises()
    // 现在总会弹窗（不再静默完成）
    expect(vm.finalsDialog).toBe(true)
    expect(completePosted.length).toBe(0)
    // 无文件可挑 → 走「不留定稿直接完成」
    await vm.completeWithoutFinals()
    await flushPromises()
    expect(completePosted.length).toBe(1)
  })

  it('完成调 complete-task', async () => {
    const posted: any[] = []
    mockFetch((url, init) => {
      if (url.includes('/my-tasks')) return ok([
        { application_id: 7, stage_code: 'STG-1', stage_name: '收稿', task_code: 'TK-1', task_name: '录入', status: 'in_progress', project_name: '甲项目', template_code: 'TPL-X', template_version: 'V1.0' },
      ])
      if (url.includes('/complete-task') && init?.method === 'POST') { posted.push(JSON.parse(init.body as string)); return ok({ status: 'completed' }) }
      return ok([])
    })
    const { wrapper } = mountView(); await flushPromises()
    const vm: any = wrapper.vm
    await vm.completeWork(vm.items[0])
    await flushPromises()
    expect(posted.length).toBe(1)
    expect(posted[0].application_id).toBe(7)
    expect(posted[0].task_code).toBe('TK-1')
  })
})
