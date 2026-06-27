import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import LoginView from '../LoginView.vue'
import { authManager } from '@/services/AuthManager'

vi.mock('@/services/AuthManager', () => ({
  authManager: {
    login: vi.fn(),
    register: vi.fn(),
  },
}))

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

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>home</div>' } },
      { path: '/login', component: LoginView },
    ],
  })
}

describe('LoginView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders login tab fields', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    expect(wrapper.text()).not.toContain('管理平台地址')
    expect(wrapper.text()).not.toContain('manage')
    expect(wrapper.text()).toContain('登录账号')
    expect(wrapper.text()).toContain('登录密码')
    expect(wrapper.text()).toContain('进入工作台')
  })

  it('keeps login form fields visually separated from tabs', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    expect(wrapper.get('[data-test="login-form-window"]').classes()).toContain('login-form-window')
  })

  it('uses auto height for login/register tab content', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await flushPromises()

    expect(wrapper.get('[data-test="login-form-window"]').classes()).toContain('login-form-window--auto-height')
  })

  it('unmounts register form when switching back to login', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await wrapper.get('[data-test="register-tab"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="register-form"]').exists()).toBe(true)

    await wrapper.get('[data-test="login-tab"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="register-form"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="login-form"]').exists()).toBe(true)
  })

  it('renders register tab fields', async () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await wrapper.get('[data-test="register-tab"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('管理平台地址')
    expect(wrapper.text()).toContain('显示姓名')
    expect(wrapper.text()).toContain('所属单位')
    expect(wrapper.text()).toContain('所属部门')
    expect(wrapper.text()).toContain('联系电话')
    expect(wrapper.text()).toContain('确认密码')
  })

  it('submits login and redirects', async () => {
    vi.mocked(authManager.login).mockResolvedValue({ token: 'token', user: {} as any })
    const router = createTestRouter()
    const pushSpy = vi.spyOn(router, 'push')
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })

    const vm = wrapper.vm as any
    vm.loginForm.username = 'liulaoshi'
    vm.loginForm.password = 'secret123'
    await vm.submitLogin()

    expect(authManager.login).toHaveBeenCalledWith({
      username: 'liulaoshi',
      password: 'secret123',
    })
    expect(pushSpy).toHaveBeenCalledWith('/')
  })

  it('submits register and redirects', async () => {
    vi.mocked(authManager.register).mockResolvedValue({ token: 'token', user: {} as any })
    const router = createTestRouter()
    const pushSpy = vi.spyOn(router, 'push')
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })

    const vm = wrapper.vm as any
    vm.registerForm.username = 'zhangsan'
    vm.registerForm.password = 'secret123'
    vm.registerForm.confirmPassword = 'secret123'
    vm.registerForm.displayName = '张三'
    vm.registerForm.userUnit = '第一研究院'
    vm.registerForm.userDepartment = '综合处'
    vm.registerForm.phone = '13900000000'
    await vm.submitRegister()

    expect(authManager.register).toHaveBeenCalledWith({
      username: 'zhangsan',
      password: 'secret123',
      display_name: '张三',
      user_unit: '第一研究院',
      user_department: '综合处',
      phone: '13900000000',
    })
    expect(pushSpy).toHaveBeenCalledWith('/')
  })
})

describe('LoginView 快速登录', () => {
  beforeEach(() => vi.clearAllMocks())

  function mockFetchHistory(history: any[]) {
    global.fetch = vi.fn(async (input: any) => {
      const url = String(input)
      if (url.includes('/auth/login-history')) {
        return { ok: true, json: async () => ({ success: true, data: history }) } as any
      }
      return { ok: true, json: async () => ({ success: true, data: {} }) } as any
    }) as any
  }

  it('进入登录页加载本机登录历史，下拉项为"显示名（账号）"', async () => {
    mockFetchHistory([
      { username: 'liulaoshi', password: 'pw', display_name: '刘老师', user_unit: '院', user_department: '档案处' },
    ])
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.loginHistory.length).toBe(1)
    expect(vm.historyOptions[0]).toEqual({ title: '刘老师（liulaoshi）', value: 'liulaoshi' })
  })

  it('选中历史账号自动填充密码', async () => {
    mockFetchHistory([
      { username: 'liulaoshi', password: 'secret123', display_name: '刘老师', user_unit: '', user_department: '' },
    ])
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm: any = wrapper.vm
    vm.onUsernameChange('liulaoshi')
    expect(vm.loginForm.username).toBe('liulaoshi')
    expect(vm.loginForm.password).toBe('secret123')
  })

  it('手输未知账号不覆盖已填密码', async () => {
    mockFetchHistory([{ username: 'liulaoshi', password: 'secret123', display_name: '刘老师', user_unit: '', user_department: '' }])
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm: any = wrapper.vm
    vm.loginForm.password = '我自己输的'
    vm.onUsernameChange('someoneelse')
    expect(vm.loginForm.username).toBe('someoneelse')
    expect(vm.loginForm.password).toBe('我自己输的')
  })
})

describe('LoginView 密码显示切换', () => {
  beforeEach(() => vi.clearAllMocks())
  it('密码默认掩码(星号)，可切换显示明文', async () => {
    global.fetch = vi.fn(async (input: any) => {
      const url = String(input)
      if (url.includes('/auth/login-history')) return { ok: true, json: async () => ({ success: true, data: [] }) } as any
      return { ok: true, json: async () => ({ success: true, data: {} }) } as any
    }) as any
    const router = createTestRouter()
    const wrapper = mount(LoginView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm: any = wrapper.vm
    expect(vm.showLoginPassword).toBe(false) // 默认掩码
    const pwd = wrapper.find('[data-test="login-password"] input')
    expect(pwd.attributes('type')).toBe('password')
    vm.showLoginPassword = true
    await flushPromises()
    expect(wrapper.find('[data-test="login-password"] input').attributes('type')).toBe('text')
  })
})
