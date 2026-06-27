import { describe, it, expect, vi, beforeEach, beforeAll, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

const vuetify = createVuetify({ components, directives })

const saveMock = vi.fn(async () => undefined)
const getConfigMock = vi.fn(async () => ({
  claim_family_default_policy: 'same_content_only' as const,
  claim_family_skip_dialog: 'false' as const,
  workspace: '',
  daily_scan_interval: 15,
  control_type: '',
  scan_area_path: '',
  scan_exclude_dir: '',
  upload_server_url: '',
  last_sync_time: null,
  home_dir: '',
  full_inventory_time: null,
  last_scan_time: null,
}))

vi.mock('../services/api', () => ({
  api: {
    getConfig: getConfigMock,
    saveConfig: saveMock,
    syncSource: vi.fn(async () => ({ message: 'ok', data: { lastSyncTime: null, syncedCount: 0, failedCount: 0, errors: [] } })),
  },
}))

vi.mock('../services/UserInfoManager', () => ({
  userInfoManager: {
    getUserInfo: vi.fn(async () => null),
  },
}))

// Mock visualViewport for Vuetify overlays in happy-dom
beforeAll(() => {
  Object.defineProperty(window, 'visualViewport', {
    value: {
      width: 1024,
      height: 768,
      offsetLeft: 0,
      offsetTop: 0,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
    writable: true,
  })
})

describe('SystemConfigView 相似认领默认行为', () => {
  beforeEach(() => {
    saveMock.mockClear()
    getConfigMock.mockClear()
  })
  afterEach(() => {
    vi.resetModules()
  })

  it('renders 3 radio options + 总是弹窗确认 checkbox', async () => {
    const { default: SystemConfigView } = await import('../views/SystemConfigView.vue')
    const wrapper = mount(SystemConfigView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    const text = document.body.textContent || ''
    expect(text).toContain('相似认领默认行为')
    expect(text).toContain('仅认领相同内容')
    expect(text).toContain('认领整个家族')
    expect(text).toContain('不带家族')
    expect(text).toContain('总是弹窗确认')

    wrapper.unmount()
  })

  it('save calls api.saveConfig with claim_family_default_policy + skip_dialog', async () => {
    const { default: SystemConfigView } = await import('../views/SystemConfigView.vue')
    const wrapper = mount(SystemConfigView, {
      global: { plugins: [vuetify] },
      attachTo: document.body,
    })
    await flushPromises()

    // Click 'all' radio
    const allRadio = document.body.querySelector('[data-test="claim-family-policy-all"] input') as HTMLInputElement
    if (allRadio) {
      allRadio.click()
      await flushPromises()
    }

    // Click the main save button (save-all pattern — one button at card bottom)
    const saveBtn = Array.from(document.body.querySelectorAll('button')).find(
      (btn) => btn.textContent?.trim() === '保存'
    ) as HTMLButtonElement | undefined
    if (saveBtn) {
      saveBtn.click()
      await flushPromises()
    }

    expect(saveMock).toHaveBeenCalled()
    const callArg = saveMock.mock.calls[saveMock.mock.calls.length - 1][0]
    expect(callArg.claim_family_default_policy).toBe('all')
    // claimFamilyAlwaysAsk stays true (skip_dialog=false means always ask), so skip_dialog = 'false'
    expect(callArg.claim_family_skip_dialog).toBe('false')

    wrapper.unmount()
  })
})
