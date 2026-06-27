import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import router from '../plugins/router'
import WorkspaceLedgerView from '../views/WorkspaceLedgerView.vue'
import FileDraftingView from '../views/FileDraftingView.vue'
import LocalArchiveView from '../views/LocalArchiveView.vue'
import ArchiveSyncView from '../views/ArchiveSyncView.vue'

const vuetify = createVuetify({ components, directives })
const mountOpts = { global: { plugins: [vuetify] } }

describe('工作空间管理演示页面', () => {
  it('工作文件台账总览渲染目录台账', () => {
    const w = mount(WorkspaceLedgerView, mountOpts)
    const txt = w.text()
    expect(txt).toContain('工作文件台账总览')
    expect(txt).toContain('工作文档')
    expect(txt).toContain('项目资料')
  })

  it('电子文件起草管理渲染表单与按钮', () => {
    const w = mount(FileDraftingView, mountOpts)
    const txt = w.text()
    expect(txt).toContain('电子文件起草管理')
    expect(txt).toContain('新建文件')
    expect(txt).toContain('保存归档')
  })

  it('自有文件本地归档展示 4 个系统隐藏目录且可新增/删除自定义目录', async () => {
    const w = mount(LocalArchiveView, mountOpts)
    const txt = w.text()
    expect(txt).toContain('个人保密夹')
    expect(txt).toContain('个人隐私文件夹')
    // 4 个系统目录初始呈现
    const vm = w.vm as unknown as { newDirName: string; addCustomDir: () => void; customDirs: unknown[]; deleteCustomDir: (n: string) => void }
    vm.newDirName = '项目专档'
    vm.addCustomDir()
    expect(vm.customDirs.length).toBe(1)
    vm.deleteCustomDir('项目专档')
    expect(vm.customDirs.length).toBe(0)
  })

  it('本地档案同步上传渲染进度条', () => {
    const w = mount(ArchiveSyncView, mountOpts)
    const txt = w.text()
    expect(txt).toContain('本地档案同步上传')
    expect(txt).toContain('开始同步')
  })

  it('4 个工作空间路由均已注册', () => {
    const paths = router.getRoutes().map(r => r.path)
    expect(paths).toContain('/workspace-ledger')
    expect(paths).toContain('/file-drafting')
    expect(paths).toContain('/local-archive')
    expect(paths).toContain('/archive-sync')
  })
})
