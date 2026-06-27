import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'
import TeamMemberPicker from '../components/TeamMemberPicker.vue'

const vuetify = createVuetify({ components, directives })

const USERS = [
  { username: 'zhangsan', display_name: '张三', user_unit: '一院', user_department: '档案处', role: 'unit_admin' },
  { username: 'lisi', display_name: '李四', user_unit: '一院', user_department: '收集科', role: 'user' },
  { username: 'wangwu', display_name: '王五', user_unit: '二院', user_department: '法务科', role: 'user' },
]

function mountPicker(modelValue: string[]) {
  return mount(TeamMemberPicker, {
    props: { users: USERS, modelValue },
    global: { plugins: [vuetify] },
  })
}

describe('TeamMemberPicker 双栏组队', () => {
  it('展示单位/部门/角色，并区分已在队', () => {
    const w = mountPicker(['zhangsan'])
    const text = w.text()
    expect(text).toContain('档案处')
    expect(text).toContain('收集科')
    expect(text).toContain('单位管理员') // 管理员角色仍展示
    expect(text).not.toContain('普通用户') // 「普通用户」标签已去掉
    expect(text).toContain('已在队') // zhangsan 已在团队
    // 右栏团队 1 人
    expect(text).toContain('当前团队')
  })

  it('点入队 → emit 追加 username', async () => {
    const w = mountPicker([])
    // 找到“+ 入队”按钮（候选区每行一个）
    const addBtn = w.findAll('button').find(b => b.text().includes('入队'))
    expect(addBtn).toBeTruthy()
    await addBtn!.trigger('click')
    const ev = w.emitted('update:modelValue')
    expect(ev).toBeTruthy()
    expect(ev![0][0]).toEqual(['zhangsan'])
  })

  it('点移除 → emit 去掉 username', async () => {
    const w = mountPicker(['zhangsan', 'lisi'])
    const rmBtn = w.findAll('button').find(b => b.text().includes('移除'))
    expect(rmBtn).toBeTruthy()
    await rmBtn!.trigger('click')
    const ev = w.emitted('update:modelValue')
    expect(ev).toBeTruthy()
    // 移除第一个团队成员 zhangsan
    expect(ev![0][0]).toEqual(['lisi'])
  })

  // 回归（item 2）：当前团队以 modelValue 为准——成员即使不在候选 users 列表也要显示，
  // 保证后端合成的项目负责人始终可见、不会出现"空团队"。
  it('成员不在候选名单也显示（负责人始终可见）+ 角色标签 + 必选锁定', () => {
    const w = mount(TeamMemberPicker, {
      props: {
        users: USERS, // 不含 boss
        modelValue: ['boss'],
        roleTags: { boss: ['项目负责人'] },
        lockedMembers: ['boss'],
      },
      global: { plugins: [vuetify] },
    })
    const text = w.text()
    expect(text).toContain('当前团队')
    expect(text).toContain('boss')          // 回退用 username 展示
    expect(text).toContain('项目负责人')     // 角色标签
    expect(text).toContain('必选')          // 锁定成员不可移除
    expect(w.findAll('button').some(b => b.text().includes('移除'))).toBe(false)
  })

  // item 3：候选区标题随「添加角色」变化
  it('addLabel 改变候选区标题', () => {
    const w = mount(TeamMemberPicker, {
      props: { users: USERS, modelValue: [], addLabel: '核心成员' },
      global: { plugins: [vuetify] },
    })
    expect(w.text()).toContain('添加为核心成员')
  })

  // 标签按位置推导：负责人→leadLabel，其余团队成员→addLabel（不依赖后端派生角色）
  it('负责人显示 leadLabel，其余成员显示 addLabel', () => {
    const w = mount(TeamMemberPicker, {
      props: { users: USERS, modelValue: ['zhangsan', 'lisi'], leadUser: 'zhangsan', leadLabel: '项目负责人', addLabel: '核心成员' },
      global: { plugins: [vuetify] },
    })
    // 团队区：张三=项目负责人，李四=核心成员
    const html = w.html()
    expect(html).toContain('项目负责人')
    expect(html).toContain('核心成员')
  })

  it('新拉入的成员立即获得 addLabel 标签（响应式）', async () => {
    const count = (s: string, sub: string) => s.split(sub).length - 1
    const w = mount(TeamMemberPicker, {
      props: { users: USERS, modelValue: ['zhangsan'], leadUser: 'zhangsan', leadLabel: '项目负责人', addLabel: '核心成员' },
      global: { plugins: [vuetify] },
    })
    // 初始：「核心成员」只出现在候选区标题（添加为核心成员），团队区无成员标签
    expect(count(w.html(), '核心成员')).toBe(1)
    // 模拟父组件把李四加入团队 → 团队区多出李四的「核心成员」标签
    await w.setProps({ modelValue: ['zhangsan', 'lisi'] })
    expect(count(w.html(), '核心成员')).toBe(2)
  })
})
