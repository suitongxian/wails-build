import { describe, it, expect, beforeEach, vi, beforeAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import { createRouter, createMemoryHistory } from 'vue-router'
import LedgerView from '@/views/LedgerView.vue'

vi.mock('@/services/projectsApi', () => ({
  ledgersApi: {
    search: vi.fn().mockResolvedValue([]),
    get: vi.fn(),
    events: vi.fn().mockResolvedValue([]),
    transition: vi.fn(),
    handover: vi.fn(),
    listProjectEvents: vi.fn(),
    listProjectLedgers: vi.fn(),
  },
  projectsApi: {
    list: vi.fn().mockResolvedValue([]),
  },
  subjectsApi: {
    list: vi.fn().mockResolvedValue([
      { id: 10, code: 'S-1', name: '甲部门', type: 'department' },
      { id: 20, code: 'S-2', name: '乙部门', type: 'department' },
      { id: 30, code: 'S-3', name: '丙部门', type: 'department' },
    ]),
  },
}))

import { ledgersApi } from '@/services/projectsApi'

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
  routes: [{ path: '/', component: { template: '<div/>' } }],
})

describe('V2-7 LedgerView 三主体过户对话框', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('canHandover 仅 registered / in_use 允许', async () => {
    const wrapper = mount(LedgerView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    expect(vm.canHandover('registered')).toBe(true)
    expect(vm.canHandover('in_use')).toBe(true)
    expect(vm.canHandover('planned')).toBe(false)
    expect(vm.canHandover('sealed')).toBe(false)
    expect(vm.canHandover('destroyed')).toBe(false)
  })

  it('openHandover 初始化对话框状态', async () => {
    const wrapper = mount(LedgerView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    // 模拟有详情底账
    vm.detail = {
      id: 99, ledger_code: 'L-1', owner_subject_id: 10,
      custodian_subject_id: 20, security_subject_id: 30,
      lifecycle_status: 'registered',
    }
    await flushPromises()

    vm.openHandover('custodian')
    await flushPromises()

    expect(vm.handoverOpen).toBe(true)
    expect(vm.handoverKind).toBe('custodian')
    expect(vm.handoverToSubjectID).toBeNull()
    expect(vm.handoverReason).toBe('')
    expect(vm.handoverApproval).toBe('')
  })

  it('handoverTargetOptions 自动排除当前主体', async () => {
    const wrapper = mount(LedgerView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    vm.detail = {
      id: 99, owner_subject_id: 10, custodian_subject_id: 20, security_subject_id: 30,
      lifecycle_status: 'registered',
    }
    vm.handoverKind = 'custodian'
    await flushPromises()

    const opts = vm.handoverTargetOptions
    // 当前 custodian=20，应在备选中排除
    const ids = opts.map((o: any) => o.value)
    expect(ids).not.toContain(20)
    expect(ids).toContain(10)
    expect(ids).toContain(30)
  })

  it('handoverKindLabel 三类正确翻译', async () => {
    const wrapper = mount(LedgerView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    expect(vm.handoverKindLabel('owner')).toBe('过户归属主体')
    expect(vm.handoverKindLabel('custodian')).toBe('过户保管主体')
    expect(vm.handoverKindLabel('security')).toBe('过户安全主体')
  })

  it('doHandover：成功路径调用 API、刷新详情、关对话框', async () => {
    const updatedLedger = { id: 99, custodian_subject_id: 30, lifecycle_status: 'registered' }
    vi.mocked(ledgersApi.handover).mockResolvedValue(updatedLedger as any)
    vi.mocked(ledgersApi.events).mockResolvedValue([])

    const wrapper = mount(LedgerView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    vm.detail = { id: 99, custodian_subject_id: 20, lifecycle_status: 'registered' }
    vm.handoverOpen = true
    vm.handoverKind = 'custodian'
    vm.handoverToSubjectID = 30
    vm.handoverReason = '部门变更'
    vm.handoverApproval = 'OA-001'

    await vm.doHandover()
    await flushPromises()

    expect(ledgersApi.handover).toHaveBeenCalledWith(99, 'custodian', 30, '部门变更', 'OA-001')
    expect(vm.detail.custodian_subject_id).toBe(30)
    expect(vm.handoverOpen).toBe(false)
  })

  it('doHandover：缺必填时直接 return 不调 API', async () => {
    const wrapper = mount(LedgerView, { global: { plugins: [vuetify, router] } })
    await flushPromises()
    const vm = wrapper.vm as any

    vm.detail = { id: 99, custodian_subject_id: 20 }
    vm.handoverKind = 'custodian'
    vm.handoverToSubjectID = null // 没选目标
    vm.handoverReason = '原因'

    await vm.doHandover()
    expect(ledgersApi.handover).not.toHaveBeenCalled()

    vm.handoverToSubjectID = 30
    vm.handoverReason = '   ' // 空白
    await vm.doHandover()
    expect(ledgersApi.handover).not.toHaveBeenCalled()
  })
})
