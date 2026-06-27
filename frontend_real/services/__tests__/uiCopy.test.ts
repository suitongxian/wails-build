import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const view = (name: string) =>
  readFileSync(resolve(process.cwd(), 'frontend_real/views', name), 'utf8')

describe('页面提示文案', () => {
  it('我的文件归目不展示示例说明蓝色提示', () => {
    const source = view('PersonalFilesView.vue')
    const app = readFileSync(resolve(process.cwd(), 'frontend_real/App.vue'), 'utf8')

    expect(source).toContain('个人文件台账')
    expect(source).toContain('个人工作文件先入账、分级、分主题')
    expect(app).toContain("title: '个人文件台账'")
    expect(source).not.toContain('例如“五篇论文 / 论文A”或“市场调研 / 广东省”')
    expect(source).not.toContain('不是三套真实目录')
    expect(source).not.toContain('三级标识容器')
    expect(source).not.toContain('查看容器')
  })

  it('数据业务项目列表不展示个人容器说明蓝色提示', () => {
    const source = view('ProjectsListView.vue')

    expect(source).not.toContain('个人核心/重要/一般是系统级个人归目容器')
  })
})
