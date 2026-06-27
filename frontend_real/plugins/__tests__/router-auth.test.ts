import { beforeEach, describe, expect, it, vi } from 'vitest'
import router from '../router'

describe('router auth guard', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  it('redirects anonymous users to login', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      json: () => Promise.resolve({
        success: true,
        data: { authenticated: false },
      }),
    }))

    await router.push('/scan')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('allows public login route', async () => {
    await router.push('/login')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/login')
  })
})
