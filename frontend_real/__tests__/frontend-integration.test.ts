import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory, type RouteRecordRaw } from 'vue-router'
import App from '../App.vue'
import { authManager } from '@/services/AuthManager'
import { userInfoManager } from '@/services/UserInfoManager'

vi.mock('@/services/AuthManager', () => ({
  authManager: {
    logout: vi.fn(),
  },
}))

vi.mock('@/services/UserInfoManager', () => ({
  userInfoManager: {
    getUserInfo: vi.fn(),
    clearCache: vi.fn(),
  },
}))

// 创建测试用的 Vuetify 实例（包含所有组件）
const vuetify = createVuetify({
  components,
  directives,
})

// 创建测试用的路由（模拟新的路由结构）
const routes: RouteRecordRaw[] = [
  { path: '/', name: 'Files', component: { template: '<div>Files Page</div>' } },
  { path: '/login', name: 'Login', component: { template: '<div>Login Page</div>' } },
  { path: '/scan', name: 'Scan', component: { template: '<div>Scan Page</div>' } },
  { path: '/claim', name: 'Claim', component: { template: '<div>Claim Page</div>' } },
  { path: '/ai-classify', name: 'AiClassify', component: { template: '<div>AiClassify Page</div>' } },
  { path: '/classify', name: 'Classify', component: { template: '<div>Classify Page</div>' } },
  { path: '/classifySearch', name: 'ClassifySearch', component: { template: '<div>ClassifySearch Page</div>' } },
  { path: '/privacy', name: 'Privacy', component: { template: '<div>Privacy Page</div>' } },
  { path: '/report', name: 'Report', component: { template: '<div>Report Page</div>' } },
  { path: '/audit-logs', name: 'AuditLogs', component: { template: '<div>AuditLogs Page</div>' } },
  { path: '/settings', name: 'Settings', component: { template: '<div>Settings Page</div>' } },
  { path: '/my-work-items', name: 'MyWorkItems', component: { template: '<div>MyWorkItems Page</div>' } },
  { path: '/file-task-assign', name: 'FileTaskAssign', component: { template: '<div>FileTaskAssign Page</div>' } },
  { path: '/file-task-receive', name: 'FileTaskReceive', component: { template: '<div>FileTaskReceive Page</div>' } },
]

describe('前端集成测试', () => {
  let router: ReturnType<typeof createRouter>

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1,
      user_name: '刘老师',
      company_name: '第一研究院',
      department: '档案处',
      ip: '',
      mac_address: '',
      work_address: null,
      phone: null,
      create_time: '',
      update_time: '',
    })
    router = createRouter({
      history: createMemoryHistory(),
      routes,
    })
  })

  describe('Vue Router 集成', () => {
    it('应该正确挂载 App 组件并包含路由视图', async () => {
      router.push('/')
      await router.isReady()

      const wrapper = mount(App, {
        global: {
          plugins: [vuetify, router],
        },
      })

      // 验证 App 组件包含导航元素
      expect(wrapper.text()).toContain('数据业务治理系统')
    })

    it('应该包含正确的导航项', async () => {
      router.push('/')
      await router.isReady()

      const wrapper = mount(App, {
        global: {
          plugins: [vuetify, router],
        },
      })

      // 验证导航项存在（与 App.vue 当前 navItems 文案对齐；
      // 选「数据业务服务」分组本身 + 几个稳定的顶级菜单做断言）
      expect(wrapper.text()).toContain('数据业务服务')
      // 文件任务受理（/file-task-receive）显示名为「文本工作受理」
      const drawerHtml = wrapper.find('.v-navigation-drawer').html()
      expect(drawerHtml).toContain('文本工作受理')
      expect(drawerHtml).toContain('/file-task-receive')
      expect(wrapper.text()).toContain('设置')
    })

    it('待处理任务在导航上显示红圈+数字角标（未读计数）', async () => {
      // mock 未读计数接口：环节分工=2、任务指派=3、工作受理=5
      const prevFetch = global.fetch
      global.fetch = vi.fn(async (input: any) => {
        const url = String(input)
        const count = url.includes('/task-unread-count') ? 5
          : url.includes('/stage-unread-count') ? 3
          : url.includes('/unread-count') ? 2 : 0
        return { ok: true, json: async () => ({ success: true, data: { count } }) } as any
      }) as any
      try {
        router.push('/')
        await router.isReady()
        const wrapper = mount(App, { global: { plugins: [vuetify, router] } })
        await flushPromises()
        const badges = wrapper.findAll('.nav-unread')
        expect(badges.length).toBe(3)
        const nums = badges.map((b) => b.text()).sort()
        expect(nums).toEqual(['2', '3', '5'])
      } finally {
        global.fetch = prevFetch
      }
    })

    it('「认领文件归档保护」入口已恢复（兜底归档），并指向 /classify', async () => {
      router.push('/')
      await router.isReady()

      const wrapper = mount(App, {
        global: { plugins: [vuetify, router] },
      })

      expect(wrapper.text()).toContain('认领文件归档保护')
      const drawerHtml = wrapper.find('.v-navigation-drawer').html()
      // 链接精确指向 /classify（避免被 /classifySearch 误命中）
      expect(drawerHtml).toMatch(/href="[^"]*\/classify(?!Search)/)
    })

    it('「本机归档文件浏览」已并入「档案在线阅卷」，不再出现在历史数据治理导航；/classifySearch 路由保留作回滚后路', async () => {
      router.push('/')
      await router.isReady()

      const wrapper = mount(App, {
        global: { plugins: [vuetify, router] },
      })

      const drawerHtml = wrapper.find('.v-navigation-drawer').html()
      // 导航中不再有「本机归档文件浏览」入口（已并入档案在线阅卷的“个人”一级 tab）
      expect(drawerHtml).not.toContain('本机归档文件浏览')
      // 「档案在线阅卷」入口仍在
      expect(drawerHtml).toContain('档案在线阅卷')
      expect(drawerHtml).toContain('/borrow')
      // /classifySearch 路由保留（回滚后路）
      expect(router.getRoutes().map((r) => r.path)).toContain('/classifySearch')
    })

    it('「历史数据治理」分组已建立，五个子入口归集到该一级菜单下并指向正确路由', async () => {
      router.push('/')
      await router.isReady()

      const wrapper = mount(App, {
        global: { plugins: [vuetify, router] },
      })

      const drawerHtml = wrapper.find('.v-navigation-drawer').html()
      // 一级分组标题
      expect(drawerHtml).toContain('历史数据治理')
      // 五个二级入口标题（「本机归档文件浏览」已并入档案在线阅卷）
      expect(drawerHtml).toContain('管控文件扫描盘点')
      expect(drawerHtml).toContain('扫描结果责任认领')
      expect(drawerHtml).toContain('认领文件归档保护')
      expect(drawerHtml).toContain('个人隐私保护')
      expect(drawerHtml).toContain('工作档案上报移交')
      // 「本机归档文件浏览」不再在导航中
      expect(drawerHtml).not.toContain('本机归档文件浏览')
      // 子入口路由正确
      expect(drawerHtml).toContain('/scan')
      expect(drawerHtml).toContain('/claim')
      expect(drawerHtml).toMatch(/href="[^"]*\/classify(?!Search)/)
      expect(drawerHtml).toContain('/privacy')
      expect(drawerHtml).toContain('/report')
    })

    it('「归目推荐」入口已从导航隐藏（路由 /ai-classify 保留作回滚后路）', async () => {
      router.push('/')
      await router.isReady()

      const wrapper = mount(App, {
        global: { plugins: [vuetify, router] },
      })

      const groups = wrapper.findAll('.v-list-group')
      const dataServiceGroup = groups.find((g) => g.text().includes('数据业务服务'))
      expect(dataServiceGroup).toBeTruthy()
      // 2026-06-02 归目推荐 暂时用不着 → 从导航隐藏（路由保留）
      expect(dataServiceGroup!.text()).not.toContain('归目推荐')
      expect(dataServiceGroup!.html()).not.toContain('/ai-classify')
      // 2026-06-02 「数据项目模版」并入模板库：入口关闭，模板库显示名为「业务模版管理」
      expect(dataServiceGroup!.text()).toContain('业务模版管理')
      expect(dataServiceGroup!.html()).toContain('/project-initiation')
      expect(dataServiceGroup!.text()).not.toContain('数据项目模版')
      // 数据业务分类已改由 manage 管理，scan 导航不再展示
      expect(dataServiceGroup!.text()).not.toContain('数据业务分类')
      expect(dataServiceGroup!.html()).not.toContain('/industry-classes')
      // 也不应出现在「历史数据治理」分组里
      const historyGroup = groups.find((g) => g.text().includes('历史数据治理'))
      expect(historyGroup!.text()).not.toContain('归目推荐')
    })

    it('2026-06-09 三级分工级联：导航含「数据项目立项」「项目工作分工」「文件任务指派」「文本工作受理」(=文件任务受理)', async () => {
      router.push('/')
      await router.isReady()

      const wrapper = mount(App, {
        global: { plugins: [vuetify, router] },
      })

      const drawerHtml = wrapper.find('.v-navigation-drawer').html()
      // 立项归一：集中立项页菜单名为「数据项目立项」并恢复入口
      expect(drawerHtml).toContain('/centralized-projects')
      expect(drawerHtml).toContain('数据项目立项')
      expect(drawerHtml).toContain('/project-acceptance')
      expect(drawerHtml).toContain('项目工作分工')
      // 文件任务指派：Tier-2 角标
      expect(drawerHtml).toContain('文件任务指派')
      expect(drawerHtml).toContain('/file-task-assign')
      // 文件任务受理（Tier-3，/file-task-receive）显示名为「文本工作受理」
      expect(drawerHtml).toContain('文本工作受理')
      expect(drawerHtml).toContain('/file-task-receive')
      // 「我的环节任务」(StageTasksView) 与旧工作台已删除：导航与路由都不应再出现
      expect(drawerHtml).not.toContain('我的环节任务')
      expect(drawerHtml).not.toContain('/stage-tasks')
      // 结项不单独成导航：结项按钮放在「立项」列表里（/project-closure 仅保留路由）
      expect(drawerHtml).not.toContain('项目结项')
      // 「数据业务模版总览」仍隐藏
      expect(drawerHtml).not.toContain('数据业务模版总览')
    })

    it('「审计日志」入口已从导航中移除', async () => {
      router.push('/')
      await router.isReady()

      const wrapper = mount(App, {
        global: { plugins: [vuetify, router] },
      })

      expect(wrapper.text()).not.toContain('审计日志')
      // 导航 DOM 中也不应再出现指向 /audit-logs 的链接
      // （只断言导航 drawer 区域，避免 router-view 子组件中的偶发命中）
      const drawerHtml = wrapper.find('.v-navigation-drawer').html()
      expect(drawerHtml).not.toContain('/audit-logs')
    })

    it('应该在右上角展示当前用户并支持退出登录', async () => {
      router.push('/')
      await router.isReady()
      const pushSpy = vi.spyOn(router, 'push')

      const wrapper = mount(App, {
        global: {
          plugins: [vuetify, router],
        },
      })
      await flushPromises()

      expect(wrapper.text()).toContain('刘老师 | 档案处 | 第一研究院')

      await wrapper.get('[data-test="logout-button"]').trigger('click')
      await flushPromises()

      expect(authManager.logout).toHaveBeenCalled()
      expect(userInfoManager.clearCache).toHaveBeenCalled()
      expect(pushSpy).toHaveBeenCalledWith('/login')
    })
  })

  describe('主题切换', () => {
    it.skip('应该包含主题切换按钮', async () => {
      // 主题切换按钮已在 App.vue 中注释掉，测试跳过
    })
  })
})
