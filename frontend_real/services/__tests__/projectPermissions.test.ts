import { describe, expect, it } from 'vitest'
import { filterProjectMembersByStage, projectMemberDisplayName } from '../projectPermissions'

describe('filterProjectMembersByStage', () => {
  it('keeps project-wide members and members assigned to the current stage', () => {
    const members = [
      { id: 1, role_code: '项目负责人', stage_ids: null },
      { id: 2, role_code: '本环节成员', stage_ids: '[11,22]' },
      { id: 3, role_code: '其他环节成员', stage_ids: '[33]' },
      { id: 4, role_code: '空环节成员', stage_ids: '' },
    ]

    expect(filterProjectMembersByStage(members, 22).map((m) => m.role_code)).toEqual([
      '项目负责人',
      '本环节成员',
      '空环节成员',
    ])
  })
})

describe('projectMemberDisplayName', () => {
  it('uses real user display fields before technical ids', () => {
    expect(projectMemberDisplayName({
      id: 1,
      user_id: 12,
      role_code: '设计师',
      permission_actions: '[]',
      user_display_name: '李四',
      user_department: '设计部',
    })).toBe('李四（设计部）')
  })

  it('falls back to user or subject ids when display fields are missing', () => {
    expect(projectMemberDisplayName({ id: 1, user_id: 12, role_code: '设计师', permission_actions: '[]' })).toBe('user#12')
    expect(projectMemberDisplayName({ id: 2, subject_id: 3, role_code: '旧成员', permission_actions: '[]' })).toBe('subject#3')
  })
})
