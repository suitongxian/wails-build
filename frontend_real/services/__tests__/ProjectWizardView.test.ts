import { describe, it, expect, beforeEach, vi, beforeAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import ProjectWizardView from '@/views/ProjectWizardView.vue'

// Mock APIs ProjectWizardView 实际会调用
vi.mock('@/services/projectsApi', () => ({
  templatesApi: {
    list: vi.fn().mockResolvedValue([]),
    syncAll: vi.fn().mockResolvedValue({ total_remote: 0, synced: 0, errors: [] }),
    sync: vi.fn().mockResolvedValue(undefined),
    get: vi.fn().mockResolvedValue({ template: {}, stages: [] }),
  },
  subjectsApi: {
    list: vi.fn().mockResolvedValue([]),
  },
  usersApi: {
    list: vi.fn().mockResolvedValue([]),
  },
  projectsApi: {
    create: vi.fn(),
  },
}))

vi.mock('@/services/UserInfoManager', () => ({
  userInfoManager: {
    getUserInfo: vi.fn(),
  },
}))

import { userInfoManager } from '@/services/UserInfoManager'
import { projectsApi, subjectsApi, templatesApi, usersApi } from '@/services/projectsApi'

beforeAll(() => {
  Object.defineProperty(window, 'visualViewport', {
    value: {
      width: 1024, height: 768, offsetLeft: 0, offsetTop: 0,
      addEventListener: vi.fn(), removeEventListener: vi.fn(),
    },
    writable: true,
  })
})

const vuetify = createVuetify({ components, directives })
const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/projects', component: { template: '<div/>' } }],
})

describe('V2-3 ProjectWizardView 立项人自动登记', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('已登录用户：成员列表默认为空（立项人由后端自动加）', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1, user_name: '张三', company_name: '测试单位', department: '研发部',
      ip: '127.0.0.1', mac_address: '00:00:00:00:00:00',
      work_address: '', phone: '',
      create_time: '', update_time: '',
    } as any)

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    // 内部 form 应当反映"立项人默认 enroll、Members 留空"
    const vm = wrapper.vm as any
    expect(vm.form.members).toEqual([])
    expect(vm.currentUser).not.toBeNull()
    expect(vm.currentUser.user_name).toBe('张三')
  })

  it('未登录用户：currentUser=null，submit 按钮禁用条件成立', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue(null)

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.currentUser).toBeNull()
    // canNext 在 step=4 时要求 currentUser 存在；前端兜底禁用提交（!currentUser）
  })

  it('canNext 不再依赖"成员有 close"——立项人由后端 enroll', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1, user_name: '张三', company_name: '测试', department: '研发',
      ip: '', mac_address: '', work_address: '', phone: '',
      create_time: '', update_time: '',
    } as any)

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    // 跳到 step 4，模拟最小数据
    vm.form.template_id = 1
    vm.form.project_name = 'X'
    vm.form.owner_subject_id = 1
    vm.form.custodian_subject_id = 1
    vm.form.security_subject_id = 1
    vm.step = 4

    await flushPromises()
    // 即使 form.members 为空、没有"close"权限的人，canNext 也应为 true
    expect(vm.canNext).toBe(true)
  })

  it('项目敏感等级不展示或提交旧 internal 值', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1, user_name: '张三', company_name: '测试', department: '研发',
      ip: '', mac_address: '', work_address: '', phone: '',
      create_time: '', update_time: '',
    } as any)
    vi.mocked(templatesApi.list).mockResolvedValue([
      {
        id: 1,
        template_code: 'TPL-LEGACY',
        template_version: 'V1.0',
        template_name: '旧模版',
        scenario: '',
        description: '',
        status: 'active',
        project_sensitivity_level: 'internal',
      } as any,
    ])
    vi.mocked(templatesApi.get).mockResolvedValue({
      template: { project_sensitivity_level: 'internal' },
      stages: [],
    } as any)

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    vm.form.template_id = 1
    await vm.onTemplateSelect()
    vm.step = 4
    await flushPromises()

    expect(vm.form.sensitivity_level).toBe('general')
    expect(vm.finalSensitivity).toBe('general')
    expect(wrapper.text()).not.toContain('internal')
    expect(wrapper.text()).not.toContain('项目最终敏感等级将被提升')
  })

  it('项目负责人自动授权提示使用中文权限名称', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1, user_name: '张三', company_name: '测试', department: '研发',
      ip: '', mac_address: '', work_address: '', phone: '',
      create_time: '', update_time: '',
    } as any)

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.step = 4
    await flushPromises()

    expect(wrapper.text()).toContain('查看、写入、领取、提交、共享、归档、结项')
    expect(wrapper.text()).not.toContain('read / write / receive / submit / share / archive / close')
  })

  it('角色下拉优先使用所选模版环节的默认角色，并支持保留通用兜底', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1, user_name: '张三', company_name: '测试', department: '研发',
      ip: '', mac_address: '', work_address: '', phone: '',
      create_time: '', update_time: '',
    } as any)
    vi.mocked(templatesApi.list).mockResolvedValue([
      {
        id: 1,
        template_code: 'TPL-SURVEY',
        template_version: 'V1.0',
        template_name: '调研模版',
        scenario: '',
        description: '',
        status: 'active',
        project_sensitivity_level: 'general',
      } as any,
    ])
    vi.mocked(templatesApi.get).mockResolvedValue({
      template: { project_sensitivity_level: 'general' },
      stages: [
        { stage_code: 'DY-XC', stage_name: '现场调研', default_role_codes: '["现场勘验员","材料整理员"]', file_rules: [] },
        { stage_code: 'DY-SH', stage_name: '复核', default_role_codes: '复核负责人,材料整理员', file_rules: [] },
      ],
    } as any)

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    vm.form.template_id = 1
    await vm.onTemplateSelect()
    await flushPromises()

    expect(vm.roleOptions).toEqual([
      { title: '现场勘验员', value: '现场勘验员' },
      { title: '材料整理员', value: '材料整理员' },
      { title: '复核负责人', value: '复核负责人' },
    ])

    vm.fullTemplate = { template: { project_sensitivity_level: 'general' }, stages: [] }
    await flushPromises()
    expect(vm.roleOptions.map((item: any) => item.value)).toContain('项目经理')
  })

  it('手动添加项目成员使用真实用户 user_id，而不是三主体 subject_id', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1, user_name: '张三', company_name: '测试', department: '研发',
      ip: '', mac_address: '', work_address: '', phone: '',
      create_time: '', update_time: '',
    } as any)
    vi.mocked(templatesApi.list).mockResolvedValue([
      {
        id: 1,
        template_code: 'TPL-USER',
        template_version: 'V1.0',
        template_name: '用户成员模版',
        scenario: '',
        description: '',
        status: 'active',
        project_sensitivity_level: 'general',
      } as any,
    ])
    vi.mocked(templatesApi.get).mockResolvedValue({
      template: { project_sensitivity_level: 'general' },
      stages: [],
    } as any)
    vi.mocked(usersApi.list).mockResolvedValue([
      { id: 2, username: 'lisi', display_name: '李四', company_name: '测试', department: '设计部', status: 'active' } as any,
    ])
    vi.mocked(projectsApi.create).mockResolvedValue({ project: { project_code: 'P-001' } })

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    expect(usersApi.list).toHaveBeenCalled()
    vm.form.template_id = 1
    await vm.onTemplateSelect()
    vm.form.project_name = 'X'
    vm.form.owner_subject_id = 10
    vm.form.custodian_subject_id = 11
    vm.form.security_subject_id = 12
    await vm.addMember()
    vm.form.members[0].user_id = 2
    vm.form.members[0].role_code = '设计师'
    vm.form.members[0].permission_actions = ['read', 'write']

    await vm.submit(false)

    expect(projectsApi.create).toHaveBeenCalledWith(expect.objectContaining({
      members: [
        expect.objectContaining({
          user_id: 2,
          role_code: '设计师',
          permission_actions: ['read', 'write'],
        }),
      ],
    }))
    expect(projectsApi.create).toHaveBeenCalledWith(expect.objectContaining({
      members: [
        expect.not.objectContaining({
          subject_id: expect.any(Number),
        }),
      ],
    }))
  })

  it('点击添加成员时会重新拉取真实用户列表', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1, user_name: '张三', company_name: '测试', department: '研发',
      ip: '', mac_address: '', work_address: '', phone: '',
      create_time: '', update_time: '',
    } as any)
    vi.mocked(usersApi.list).mockResolvedValue([
      { id: 2, username: 'lisi', display_name: '李四', company_name: '测试', department: '设计部', status: 'active' } as any,
    ])

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any
    vi.mocked(usersApi.list).mockClear()

    await vm.addMember()
    await flushPromises()

    expect(usersApi.list).toHaveBeenCalledTimes(1)
    expect(vm.form.members).toHaveLength(1)
  })

  it('切换到三主体和成员步骤时会重新拉取 manage 最新数据', async () => {
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1, user_name: '张三', company_name: '测试', department: '研发',
      ip: '', mac_address: '', work_address: '', phone: '',
      create_time: '', update_time: '',
    } as any)
    vi.mocked(subjectsApi.list).mockResolvedValue([
      { id: 10, code: 'DEPT-NEW', name: '新部门', type: 'department', parent_id: null, contact: null, status: 'active' } as any,
    ])
    vi.mocked(usersApi.list).mockResolvedValue([
      { id: 20, username: 'newuser', display_name: '新用户', company_name: '测试', department: '研发', status: 'active' } as any,
    ])

    const wrapper = mount(ProjectWizardView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any
    vi.mocked(subjectsApi.list).mockClear()
    vi.mocked(usersApi.list).mockClear()

    vm.step = 3
    await flushPromises()
    expect(subjectsApi.list).toHaveBeenCalledTimes(1)
    expect(usersApi.list).not.toHaveBeenCalled()

    vi.mocked(subjectsApi.list).mockClear()
    vi.mocked(usersApi.list).mockClear()
    vm.step = 4
    await flushPromises()
    expect(subjectsApi.list).toHaveBeenCalledTimes(1)
    expect(usersApi.list).toHaveBeenCalledTimes(1)
  })
})
