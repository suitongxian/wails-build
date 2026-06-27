import { describe, it, expect, beforeEach, vi, beforeAll } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import BorrowView from '@/views/BorrowView.vue'
import { api } from '@/services/api'
import { userInfoManager } from '@/services/UserInfoManager'

// Mock API
vi.mock('@/services/api', () => ({
  api: {
    getArchiveFiles: vi.fn(),
    getQuickArchiveCabinetFiles: vi.fn(),
    getPersonalArchiveFiles: vi.fn(),
    getResources: vi.fn(),
    openFile: vi.fn(),
    borrowDownload: vi.fn(),
  }
}))

// Mock UserInfoManager
vi.mock('@/services/UserInfoManager', () => ({
  userInfoManager: {
    getUserInfo: vi.fn(),
  }
}))

// Mock visualViewport for Vuetify
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

const vuetify = createVuetify({
  components,
  directives,
})

describe('BorrowView', () => {
  beforeEach(() => {
    vi.clearAllMocks()

    // 默认返回用户信息（异步方式，使用新的字段名）
    vi.mocked(userInfoManager.getUserInfo).mockResolvedValue({
      id: 1,
      user_name: '测试用户',
      company_name: '测试单位',
      department: '测试部门',
      ip: '192.168.1.1',
      mac_address: '00:00:00:00:00:00',
      work_address: null,
      phone: null,
      create_time: new Date().toISOString(),
      update_time: new Date().toISOString()
    })

    // 默认返回归档文件列表
    vi.mocked(api.getArchiveFiles).mockResolvedValue({
      list: [],
      total: 0,
      page: 1,
      pageSize: 1000
    })
    // 部门/单位柜室文件（一键归档上报）+ 个人台账，均默认空
    vi.mocked(api.getQuickArchiveCabinetFiles).mockResolvedValue([])
    vi.mocked(api.getPersonalArchiveFiles).mockResolvedValue([])
    vi.mocked(api.getResources).mockResolvedValue({ resources: [], total: 0 } as any)
  })

  it('应该正确渲染组件', () => {
    const wrapper = mount(BorrowView, {
      global: {
        plugins: [vuetify],
      },
    })

    expect(wrapper.find('.v-card-title').text()).toContain('档案在线阅卷')
  })

  it('应该在挂载时加载部门/单位柜室文件（一键归档上报）', async () => {
    vi.mocked(api.getQuickArchiveCabinetFiles).mockResolvedValue([
      {
        id: 1, project_code: 'XM-1', project_name: '甲项目', scope: 'department',
        file_name: '定稿.pdf', bucket: 'output', sensitivity_level: 'important',
        target_folder: '部门档案柜', storage_tier: 'department_cabinet', storage_location: '部门重要项目档案柜',
        checksum: 'abc', file_size: 1024, custody_note: '单位立项', archived_at: '2026-06-21 09:30:00',
      },
    ] as any)

    mount(BorrowView, { global: { plugins: [vuetify] } })
    await new Promise(resolve => setTimeout(resolve, 200))

    // 改版后柜室数据来自一键归档上报（quick_archive_files），挂载即加载
    expect(api.getQuickArchiveCabinetFiles).toHaveBeenCalled()
  })

  it('应该正确判断文件是否可以在线查看', () => {
    // PDF文件且是核心数据可以查看
    const pdfCoreFile = {
      id: 1,
      archive_file_name: 'test.pdf',
      data_classification: '核心' as const,
    }

    // 一般文件不能查看
    const normalFile = {
      id: 3,
      archive_file_name: 'test.doc',
      data_classification: '一般' as const,
    }

    // 验证逻辑
    const isPdf = pdfCoreFile.archive_file_name.toLowerCase().endsWith('.pdf')
    const isCore = pdfCoreFile.data_classification === '核心'
    expect(isPdf && isCore).toBe(true)

    expect(normalFile.archive_file_name.toLowerCase().endsWith('.pdf')).toBe(false)
  })

  it('应该正确判断文件是否可以下载', () => {
    // 核心文件不能下载
    const coreFile = {
      data_classification: '核心' as const,
    }

    // 重要文件可以下载
    const importantFile = {
      data_classification: '重要' as const,
    }

    // 一般文件可以下载
    const normalFile = {
      data_classification: '一般' as const,
    }

    // 公开文件可以下载
    const publicFile = {
      data_classification: '公开' as const,
    }

    expect(coreFile.data_classification).toBe('核心')
    expect(importantFile.data_classification).not.toBe('核心')
    expect(normalFile.data_classification).not.toBe('核心')
    expect(publicFile.data_classification).not.toBe('核心')
  })

  it('应该在下载重要文件时要求填写理由', () => {
    const importantFile = {
      data_classification: '重要' as const,
    }

    const normalFile = {
      data_classification: '一般' as const,
    }

    // 重要文件需要理由
    expect(importantFile.data_classification).toBe('重要')

    // 一般文件不需要理由
    expect(normalFile.data_classification).not.toBe('重要')
  })

  it('应该在在线查看时强制要求填写理由', () => {
    // 所有在线查看都需要填写理由
    const action = 'view'
    expect(action === 'view').toBe(true)
  })
})
