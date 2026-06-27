export interface ProjectPermissionMember {
  id: number
  user_id?: number | null
  subject_id?: number
  role_code: string
  stage_ids?: string | null
  permission_actions: string
  user_username?: string | null
  user_display_name?: string | null
  user_company_name?: string | null
  user_department?: string | null
}

function parseStageIDs(raw: string | null | undefined): number[] {
  if (!raw || !raw.trim()) return []
  try {
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) {
      return parsed.map((v) => Number(v)).filter((v) => Number.isFinite(v))
    }
  } catch {
    // Older data may be stored as a comma separated list.
  }
  return raw
    .split(',')
    .map((v) => Number(v.trim()))
    .filter((v) => Number.isFinite(v))
}

export function filterProjectMembersByStage<T extends ProjectPermissionMember>(
  members: T[],
  stageID: number | null | undefined
): T[] {
  if (!stageID) return members
  return members.filter((member) => {
    const stageIDs = parseStageIDs(member.stage_ids)
    return stageIDs.length === 0 || stageIDs.includes(stageID)
  })
}

export function projectMemberDisplayName(member: ProjectPermissionMember): string {
  const name = member.user_display_name || member.user_username || ''
  const org = member.user_department || member.user_company_name || ''
  if (name && org) return `${name}（${org}）`
  if (name) return name
  if (member.user_id) return `user#${member.user_id}`
  if (member.subject_id) return `subject#${member.subject_id}`
  return '未命名成员'
}
