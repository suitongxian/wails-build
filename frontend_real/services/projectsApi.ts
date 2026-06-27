/**
 * 数据业务模版与项目卷宗相关 API
 */

const API_BASE = 'http://127.0.0.1:3001'

// 文件上传等长操作的超时（毫秒）。超时后 fetch 抛 AbortError，
// 调用方 catch 后能正常清理 loading 状态，不会"一直转圈"。
const UPLOAD_TIMEOUT_MS = 60_000

// fetchWithTimeout 给 fetch 加超时，避免长请求让 UI 永远 loading
async function fetchWithTimeout(url: string, init: RequestInit, timeoutMs = UPLOAD_TIMEOUT_MS): Promise<Response> {
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), timeoutMs)
  try {
    return await fetch(url, { ...init, signal: ctrl.signal })
  } catch (e: any) {
    if (e?.name === 'AbortError') {
      throw new Error(`请求超时（${timeoutMs / 1000}s）。请检查网络或后端服务，并在 wails 终端查看 [upload]/[derive] 日志定位卡点。`)
    }
    throw e
  } finally {
    clearTimeout(timer)
  }
}

// =============================================
// Subjects 三主体
// =============================================

export interface Subject {
  id: number
  code: string
  name: string
  type: 'person' | 'department' | 'organization'
  parent_id: number | null
  contact: string | null
  status: string
  create_time: string
  update_time: string
  disable: number
}

export const subjectsApi = {
  async list(params: { type?: string; keyword?: string } = {}): Promise<Subject[]> {
    const q = new URLSearchParams()
    if (params.type) q.set('type', params.type)
    if (params.keyword) q.set('keyword', params.keyword)
    const res = await fetch(`${API_BASE}/subjects?${q.toString()}`, { cache: 'no-store' })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },
}

// =============================================
// Users 真实用户
// =============================================

export interface ProjectUser {
  id: number
  username: string
  display_name: string
  company_name: string
  department: string
  status: string
}

export const usersApi = {
  async list(): Promise<ProjectUser[]> {
    const res = await fetch(`${API_BASE}/users`, { cache: 'no-store' })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },
}

// =============================================
// Templates 模版
// =============================================

export interface DataTemplate {
  id: number
  remote_id: number | null
  template_code: string
  template_name: string
  template_version: string
  class_code: string | null
  scenario: string | null
  publisher: string | null
  status: string
  project_sensitivity_level: string
  description: string | null
  source_endpoint: string | null
  cached_at: string
}

export interface TemplateStage {
  id: number
  template_id: number
  stage_code: string
  stage_name: string
  stage_type: string
  sort_order: number
  description: string | null
  default_role_codes: string | null
}

export interface TemplateFileRule {
  id: number
  template_stage_id: number
  file_rule_code: string
  file_name: string
  data_state: 'input' | 'process' | 'output'
  required: number
  allowed_file_types: string
  naming_pattern: string | null
  summary_pattern: string | null
  sort_order: number
}

export interface FullStageWithRules extends TemplateStage {
  file_rules: TemplateFileRule[]
}

export interface FullTemplate {
  template: DataTemplate
  stages: FullStageWithRules[]
}

export const templatesApi = {
  async list(status?: string): Promise<DataTemplate[]> {
    const q = new URLSearchParams()
    if (status) q.set('status', status)
    const res = await fetch(`${API_BASE}/templates?${q.toString()}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },

  async get(id: number): Promise<FullTemplate> {
    const res = await fetch(`${API_BASE}/templates/${id}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async sync(input: { code?: string; version?: string; remote_id?: number }): Promise<FullTemplate> {
    const res = await fetch(`${API_BASE}/templates/sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  // 同步 manage 端所有 active 模版（"重新同步"按钮用）
  async syncAll(): Promise<{ total_remote: number; synced: number; errors: string[]; local_ids: number[] }> {
    const res = await fetch(`${API_BASE}/templates/sync-all`, { method: 'POST' })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },
}

// =============================================
// Projects 项目卷宗
// =============================================

export interface DataProject {
  id: number
  project_code: string
  project_name: string
  object_short_code: string | null
  template_code: string
  template_version: string
  task_summary: string | null
  approval_basis: string | null
  planned_start_date: string | null
  planned_end_date: string | null
  sensitivity_level: string
  management_mode: string
  owner_subject_id: number
  custodian_subject_id: number
  security_subject_id: number
  status: string
  project_root: string | null
  sync_status: string | null
  sync_message: string | null
  synced_at: string | null
  created_by: string | null
  create_time: string
  update_time: string
}

export interface MemberInput {
  user_id: number
  role_code: string
  stage_codes?: string[]
  permission_actions: string[]
}

export interface CreateProjectInput {
  template_code: string
  template_version: string
  project_name: string
  object_short_code: string
  task_summary?: string
  approval_basis?: string
  planned_start_date?: string
  planned_end_date?: string
  sensitivity_level: string
  management_mode?: string
  owner_subject_id: number
  custodian_subject_id: number
  security_subject_id: number
  members: MemberInput[]
  activate?: boolean
}

export const projectsApi = {
  async list(params: { status?: string; keyword?: string } = {}): Promise<DataProject[]> {
    const q = new URLSearchParams()
    if (params.status) q.set('status', params.status)
    if (params.keyword) q.set('keyword', params.keyword)
    const res = await fetch(`${API_BASE}/projects?${q.toString()}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },

  async get(id: number): Promise<DataProject> {
    const res = await fetch(`${API_BASE}/projects/${id}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async create(input: CreateProjectInput): Promise<any> {
    const res = await fetch(`${API_BASE}/projects`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async listStages(id: number): Promise<any[]> {
    const res = await fetch(`${API_BASE}/projects/${id}/stages`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },

  async listFileVersions(id: number): Promise<any[]> {
    const res = await fetch(`${API_BASE}/projects/${id}/file-versions`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },

  async listMembers(id: number): Promise<any[]> {
    const res = await fetch(`${API_BASE}/projects/${id}/members`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },

  async closePrecheck(id: number): Promise<{
    ok: boolean
    issues: { severity: string; code: string; message: string }[]
  }> {
    const res = await fetch(`${API_BASE}/projects/${id}/close/precheck`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async close(id: number, reason: string, force: boolean): Promise<{
    project: DataProject
    manifest_path: string
    manifest_sha256: string
    file_count: number
    ledger_count: number
    event_count: number
  }> {
    const res = await fetch(`${API_BASE}/projects/${id}/close`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason, force }),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async syncArchive(id: number): Promise<{ status: string; endpoint: string; reply?: string; error?: string }> {
    const res = await fetch(`${API_BASE}/projects/${id}/sync`, { method: 'POST' })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  // 一键归档（单项目）：按九宫格分流——个人→本地夹 / 部门、单位→上报云端 / 行业→跳过
  async quickArchive(id: number): Promise<{ route: string; archived: number; skipped: number; errors?: string[] }> {
    const res = await fetch(`${API_BASE}/projects/${id}/quick-archive`, { method: 'POST' })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  // 一键归档（全部）：巡检工作空间所有项目目录批量归档
  async quickArchiveAll(): Promise<{ total_archived: number; total_skipped: number; projects: Array<{ project_code: string; project_name: string; scope: string; route: string; archived: number; skipped: number; errors?: string[] }> }> {
    const res = await fetchWithTimeout(`${API_BASE}/projects/quick-archive-all`, { method: 'POST' }, 120_000)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  // V3-3 §7.3 切换环节状态：pending → running / skipped 等
  async updateStageStatus(projectId: number, stageId: number, toStatus: string): Promise<any> {
    const res = await fetch(`${API_BASE}/projects/${projectId}/stages/${stageId}/status`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ to_status: toStatus }),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  // V3-8 §8.2 项目激活 / 取消
  async activate(id: number): Promise<DataProject> {
    const res = await fetch(`${API_BASE}/projects/${id}/activate`, { method: 'POST' })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async cancel(id: number, reason?: string): Promise<DataProject> {
    const res = await fetch(`${API_BASE}/projects/${id}/cancel`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason }),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },
}

// =============================================
// File Versions 文件版本操作（D2-D5）
// =============================================

export interface FileVersion {
  id: number
  project_id: number
  project_stage_id: number
  template_file_rule_id: number | null
  file_version_code: string
  local_code: string
  display_name: string
  data_state: 'input' | 'process' | 'output'
  version_no: string
  required: number
  file_type: string | null
  storage_uri: string | null
  checksum: string | null
  file_size: number | null
  source_file_version_id: number | null
  security_policy_id: number | null
  lifecycle_status: string
  submitted_at: string | null
  submitted_by: string | null
  original_file_name: string | null
  created_by: string | null
  create_time: string
  update_time: string
}

export interface AssetLedger {
  id: number
  ledger_code: string
  file_version_id: number
  class_code: string | null
  project_code: string
  stage_code: string
  file_version_code: string
  asset_name: string
  content_summary: string | null
  owner_subject_id: number
  custodian_subject_id: number
  security_subject_id: number
  sensitivity_level: string
  marking_method: string
  source_ref: string | null
  current_storage_uri: string | null
  lifecycle_status: string
  create_time: string
  update_time: string
}

export interface LifecycleEvent {
  id: number
  file_version_id: number
  event_type: string
  event_name: string
  operator_id: string | null
  reason: string | null
  approval_ref: string | null
  create_time: string
}

export const fileVersionsApi = {
  async get(id: number): Promise<FileVersion> {
    const res = await fetch(`${API_BASE}/file-versions/${id}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async events(id: number): Promise<LifecycleEvent[]> {
    const res = await fetch(`${API_BASE}/file-versions/${id}/events`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },

  async chain(id: number): Promise<{ current: FileVersion; ancestors: FileVersion[]; descendants: any[] }> {
    const res = await fetch(`${API_BASE}/file-versions/${id}/chain`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async ledger(id: number): Promise<AssetLedger | null> {
    const res = await fetch(`${API_BASE}/file-versions/${id}/ledger`)
    if (res.status === 404) return null
    const json = await res.json()
    if (!json.success) return null
    return json.data
  },

  async upload(id: number, file: File, extras?: Record<string, string>): Promise<any> {
    const fd = new FormData()
    fd.append('file', file)
    if (extras) fd.append('extras', JSON.stringify(extras))
    console.info('[upload] start', { id, filename: file.name, size: file.size })
    const t0 = performance.now()
    const res = await fetchWithTimeout(`${API_BASE}/file-versions/${id}/upload`, { method: 'POST', body: fd })
    const json = await res.json()
    console.info('[upload] response', { id, status: res.status, ok: json.success, elapsed: `${(performance.now() - t0).toFixed(0)}ms` })
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async newVersion(id: number, file: File, extras?: Record<string, string>): Promise<any> {
    const fd = new FormData()
    fd.append('file', file)
    if (extras) fd.append('extras', JSON.stringify(extras))
    const res = await fetchWithTimeout(`${API_BASE}/file-versions/${id}/new-version`, { method: 'POST', body: fd })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async derive(
    sourceFvId: number,
    file: File,
    targetStageId: number,
    targetRuleCode: string,
    targetVersionNo?: string,
    extras?: Record<string, string>
  ): Promise<any> {
    const fd = new FormData()
    fd.append('file', file)
    fd.append('target_stage_id', String(targetStageId))
    fd.append('target_rule_code', targetRuleCode)
    if (targetVersionNo) fd.append('target_version_no', targetVersionNo)
    if (extras) fd.append('extras', JSON.stringify(extras))
    const res = await fetchWithTimeout(`${API_BASE}/file-versions/${sourceFvId}/derive`, { method: 'POST', body: fd })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async submit(id: number): Promise<FileVersion> {
    const res = await fetch(`${API_BASE}/file-versions/${id}/submit`, { method: 'POST' })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async receive(sourceFvId: number, targetStageId: number, targetRuleCode: string): Promise<any> {
    const res = await fetch(`${API_BASE}/file-versions/${sourceFvId}/receive`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target_stage_id: targetStageId, target_rule_code: targetRuleCode }),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async listSubmittable(projectId: number): Promise<FileVersion[]> {
    const res = await fetch(`${API_BASE}/file-versions/submittable?project_id=${projectId}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },
}

// =============================================
// Ledgers 底账（"账"）+ 生命周期事件
// =============================================

export interface ProjectEventContext {
  id: number
  file_version_id: number
  ledger_id: number | null
  event_type: string
  event_name: string
  operator_id: string | null
  reason: string | null
  approval_ref: string | null
  create_time: string
  project_code: string | null
  stage_code: string | null
  file_version_code: string | null
  asset_name: string | null
}

export interface LedgerSearchParams {
  project_code?: string
  stage_code?: string
  sensitivity_level?: string
  owner_subject_id?: number
  lifecycle_status?: string
  keyword?: string
}

export const ledgersApi = {
  async search(params: LedgerSearchParams = {}): Promise<AssetLedger[]> {
    const q = new URLSearchParams()
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
    })
    const res = await fetch(`${API_BASE}/ledgers?${q.toString()}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },

  async get(id: number): Promise<AssetLedger> {
    const res = await fetch(`${API_BASE}/ledgers/${id}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async events(id: number): Promise<LifecycleEvent[]> {
    const res = await fetch(`${API_BASE}/ledgers/${id}/events`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },

  async transition(id: number, toStatus: string, reason?: string, approvalRef?: string): Promise<AssetLedger> {
    const res = await fetch(`${API_BASE}/ledgers/${id}/transition`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ to_status: toStatus, reason, approval_ref: approvalRef }),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  // V2-7: 三主体过户。subjectKind 取 owner / custodian / security。
  async handover(
    id: number,
    subjectKind: 'owner' | 'custodian' | 'security',
    toSubjectID: number,
    reason?: string,
    approvalRef?: string,
  ): Promise<AssetLedger> {
    const res = await fetch(`${API_BASE}/ledgers/${id}/handover`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        subject_kind: subjectKind,
        to_subject_id: toSubjectID,
        reason,
        approval_ref: approvalRef,
      }),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data
  },

  async listProjectEvents(
    projectId: number,
    params: { event_type?: string; stage_code?: string; limit?: number } = {},
  ): Promise<ProjectEventContext[]> {
    const q = new URLSearchParams()
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
    })
    const res = await fetch(`${API_BASE}/projects/${projectId}/events?${q.toString()}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },


  async listProjectLedgers(
    projectId: number,
    params: { stage_code?: string; lifecycle_status?: string; keyword?: string } = {},
  ): Promise<AssetLedger[]> {
    const q = new URLSearchParams()
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
    })
    const res = await fetch(`${API_BASE}/projects/${projectId}/ledgers?${q.toString()}`)
    const json = await res.json()
    if (!json.success) throw new Error(json.error || 'failed')
    return json.data || []
  },
}
