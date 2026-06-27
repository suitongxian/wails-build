import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

const vuetify = createVuetify({ components, directives })

beforeAll(() => {
  if (typeof window !== 'undefined' && !('visualViewport' in window)) {
    Object.defineProperty(window, 'visualViewport', {
      value: {
        width: 1024, height: 768, scale: 1, offsetLeft: 0, offsetTop: 0,
        pageLeft: 0, pageTop: 0, addEventListener: () => {}, removeEventListener: () => {},
      },
      configurable: true,
    })
  }
})

const { getPersonalMock, getCabinetMock } = vi.hoisted(() => ({
  getPersonalMock: vi.fn(async () => ([
    { file_name: '采购合同.pdf', project_name: '甲项目', sensitivity_level: 'core', folder: '个人核心文件夹', file_size: 2048, archived_at: '2026-06-21 09:30:00' },
  ])),
  getCabinetMock: vi.fn(async () => ([])),
}))

vi.mock('../services/api', () => ({
  api: {
    getArchiveFiles: vi.fn(async () => ({ list: [], total: 0, page: 1, pageSize: 1000 })),
    getPersonalArchiveFiles: getPersonalMock,
    getQuickArchiveCabinetFiles: getCabinetMock,
    getResources: vi.fn(async () => ({ resources: [], total: 0, page: 1, pageSize: 50 })),
    borrowDownload: vi.fn(async () => new Blob()),
  },
}))

vi.mock('../services/UserInfoManager', () => ({
  userInfoManager: {
    getUserInfo: vi.fn(async () => ({ user_name: '张三', company_name: '某单位', department: '信息部' })),
  },
}))

import BorrowView from '../views/BorrowView.vue'

const mountOpts = { global: { plugins: [vuetify] } }

describe('BorrowView 个人（夹）', () => {
  afterEach(() => vi.clearAllMocks())

  it('一级 tab 含个人/部门/单位，默认进入个人并加载本机个人文件夹', async () => {
    const w = mount(BorrowView, mountOpts)
    await flushPromises()
    const txt = w.text()
    expect(txt).toContain('个人')
    expect(txt).toContain('部门')
    expect(txt).toContain('单位')
    // 默认 personal scope → 调用 getPersonalArchiveFiles（本机一键归档落点），并展示文件
    expect(getPersonalMock).toHaveBeenCalled()
    expect(txt).toContain('采购合同.pdf')
    expect(txt).toContain('甲项目')
  })

  it('个人下二级 tab 按级别命名（核心/重要/一般 文件夹）', async () => {
    const w = mount(BorrowView, mountOpts)
    await flushPromises()
    const txt = w.text()
    expect(txt).toContain('核心文件夹')
    expect(txt).toContain('重要文件夹')
    expect(txt).toContain('一般文件夹')
    // 个人为默认 scope，不应出现柜/室
    expect(txt).not.toContain('核心文件柜')
    expect(txt).not.toContain('核心文件室')
  })
})
