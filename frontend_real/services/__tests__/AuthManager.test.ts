import { beforeEach, describe, expect, it, vi } from 'vitest'
import { authManager } from '../AuthManager'

function mockFetch(data: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    json: () => Promise.resolve(data),
  }))
}

describe('AuthManager', () => {
  beforeEach(() => {
    authManager.clearCache()
    vi.unstubAllGlobals()
  })

  it('login stores the returned session and exposes current user', async () => {
    mockFetch({
      success: true,
      data: {
        token: 'scan-token',
        user: {
          id: 1,
          username: 'liulaoshi',
          display_name: '刘老师',
          user_unit: '第一研究院',
          user_department: '档案处',
          phone: '13800000000',
          role: 'user',
          status: 'active',
        },
      },
    })

    const session = await authManager.login({
      username: 'liulaoshi',
      password: 'secret123',
      manage_endpoint: 'http://manage.local',
    })

    expect(session.token).toBe('scan-token')
    expect(authManager.getCurrentUser()?.display_name).toBe('刘老师')
  })

  it('register stores the returned session', async () => {
    mockFetch({
      success: true,
      data: {
        token: 'register-token',
        user: {
          id: 2,
          username: 'zhangsan',
          display_name: '张三',
          user_unit: '第一研究院',
          user_department: '综合处',
          phone: null,
          role: 'user',
          status: 'active',
        },
      },
    })

    await authManager.register({
      username: 'zhangsan',
      password: 'secret123',
      display_name: '张三',
      user_unit: '第一研究院',
      user_department: '综合处',
    })

    expect(authManager.getCurrentUser()?.username).toBe('zhangsan')
  })

  it('logout clears the cached session', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce({
        json: () => Promise.resolve({
          success: true,
          data: {
            token: 'old-token',
            user: {
              id: 3,
              username: 'old',
              display_name: '旧用户',
              user_unit: '旧单位',
              user_department: '旧部门',
              phone: null,
              role: 'user',
              status: 'active',
            },
          },
        }),
      })
      .mockResolvedValueOnce({
        json: () => Promise.resolve({ success: true, data: { authenticated: false } }),
      }))

    await authManager.login({ username: 'old', password: 'secret123' })

    await authManager.logout()

    expect(authManager.getCurrentUser()).toBeNull()
  })
})
