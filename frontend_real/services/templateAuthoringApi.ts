/**
 * 数据业务模版「本地创作」API（scan 端五层模版 CRUD）
 *
 * 2026-05-31 模版创作权从 manage 迁到 scan：行业分类 ▸ 数据项目模版 ▸ 工作事项 ▸
 * 文件任务 ▸ 文档标识。编码全自动，前端只填业务字段。
 */
import { API_BASE } from '@/services/api'

// ---------- 类型 ----------
export interface BusinessClass {
  id: number
  code: string
  name: string
  type: string
  description: string | null
}

export type TemplateScope = 'industry' | 'unit' | 'department' | 'person'
export type Sensitivity = 'core' | 'important' | 'general'
export type DataState = 'input' | 'process' | 'output'

export interface LocalTemplate {
  id: number
  template_code: string
  template_name: string
  template_version: string
  class_code: string | null
  status: string
  is_published: number // 0未发布/1已发布；只有已发布才能立项
  project_sensitivity_level: string
  origin: string
  scope: string
  short_code: string | null
  manager: string | null
  owner: string | null
  approval_basis: string | null
  description: string | null
  sync_status: string | null
  synced_at: string | null
  certified?: number // 1=项目认定模版（单位最高权威）
  certified_from?: string | null // 认定来源项目编码
}

export interface TemplateStage {
  id: number
  template_id: number
  stage_code: string
  stage_name: string
  stage_type: string
  sort_order: number
  description: string | null
  manager: string | null
  members: string | null
  manager_username: string | null
  members_usernames: string | null
}

export interface TemplateTask {
  id: number
  template_stage_id: number
  task_code: string
  task_name: string
  manager: string | null
  sensitivity_level: string | null
  sort_order: number
  description: string | null
}

export interface TemplateFileRule {
  id: number
  template_stage_id: number
  template_task_id: number | null
  file_rule_code: string
  file_name: string
  data_state: DataState
  required: number
  allowed_file_types: string
  naming_pattern: string | null
  summary_pattern: string | null
  sensitivity_level: string | null
  drafter: string | null
  sort_order: number
}

export interface TaskNode extends TemplateTask {
  file_rules: TemplateFileRule[]
}
export interface StageNode extends TemplateStage {
  tasks: TaskNode[]
}
export interface LocalTemplateTree {
  template: LocalTemplate
  stages: StageNode[]
}

// ---------- 请求封装 ----------
async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    cache: 'no-store',
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    ...init,
  })
  const json = await res.json()
  if (!json.success) throw new Error(json.error || '请求失败')
  return json.data as T
}
const body = (o: unknown) => JSON.stringify(o)

// ---------- 行业分类 ----------
export const businessClassApi = {
  list: () => req<BusinessClass[]>(`/business-classes`).then((d) => d || []),
  create: (input: { name: string; description?: string }) =>
    req<BusinessClass>(`/business-classes`, { method: 'POST', body: body(input) }),
  update: (id: number, input: { name: string; description?: string }) =>
    req<void>(`/business-classes/${id}`, { method: 'PUT', body: body(input) }),
  remove: (id: number) => req<void>(`/business-classes/${id}`, { method: 'DELETE' }),
}

// ---------- 数据项目模版 ----------
export interface TemplateInput {
  class_code?: string
  scope?: TemplateScope
  template_name: string
  short_code?: string
  manager?: string
  description?: string
  approval_basis?: string
  sensitivity_level?: Sensitivity
  owner?: string
}

export const localTemplateApi = {
  list: (params: { class_code?: string; scope?: string } = {}) => {
    const q = new URLSearchParams()
    if (params.class_code) q.set('class_code', params.class_code)
    if (params.scope) q.set('scope', params.scope)
    return req<LocalTemplate[]>(`/templates/authoring?${q.toString()}`).then((d) => d || [])
  },
  create: (input: TemplateInput) => req<LocalTemplate>(`/templates`, { method: 'POST', body: body(input) }),
  update: (id: number, input: TemplateInput) => req<void>(`/templates/${id}`, { method: 'PUT', body: body(input) }),
  remove: (id: number) => req<void>(`/templates/${id}`, { method: 'DELETE' }),
  tree: (id: number) => req<LocalTemplateTree>(`/templates/${id}/tree`),
  // 阶段二：把本地模版推送到 manage（反向同步）
  push: (id: number) => req<{ remote_id: number }>(`/templates/${id}/push`, { method: 'POST' }),
  // 立项：把本地模版作为项目推送到 manage（is_project=1，进进度跟踪）
  initiate: (id: number) => req<{ remote_id: number }>(`/templates/${id}/initiate`, { method: 'POST' }),
  // 发布/取消发布：只有已发布的本地模版才能立项
  setPublished: (id: number, published: boolean) =>
    req<{ is_published: boolean }>(`/templates/${id}/publish`, { method: 'POST', body: body({ published }) }),
  // 列 manage 已有模版 / 从某模版克隆到本地可编辑
  remoteList: () => req<RemoteTemplate[]>(`/templates/remote-list`).then((d) => d || []),
  cloneFromManage: (template_code: string) =>
    req<{ id: number }>(`/templates/clone-from-manage`, { method: 'POST', body: body({ template_code }) }),
  // 导入：上传/粘贴五层模版树 JSON（{ template, stages:[{tasks:[{file_rules:[]}]}] }），重建为本地可编辑模版
  importTree: (tree: unknown) =>
    req<{ id: number }>(`/templates/import`, { method: 'POST', body: body(tree) }),
}

export interface RemoteTemplate {
  id: number
  template_code: string
  template_name: string
  template_version: string
  status: string
}

// ---------- 工作事项 ----------
export interface StageInput {
  template_id?: number
  name: string
  manager?: string
  manager_username?: string
  members?: string
  members_usernames?: string
  desc?: string
}
export const stageApi = {
  list: (templateId: number) => req<TemplateStage[]>(`/template-stages?template_id=${templateId}`).then((d) => d || []),
  create: (input: StageInput) => req<TemplateStage>(`/template-stages`, { method: 'POST', body: body(input) }),
  update: (id: number, input: StageInput) => req<void>(`/template-stages/${id}`, { method: 'PUT', body: body(input) }),
  remove: (id: number) => req<void>(`/template-stages/${id}`, { method: 'DELETE' }),
}

// ---------- 文件任务 ----------
export interface TaskInput {
  stage_id?: number
  name: string
  manager?: string
  sensitivity_level?: Sensitivity
  desc?: string
}
export const taskApi = {
  list: (stageId: number) => req<TemplateTask[]>(`/template-tasks?stage_id=${stageId}`).then((d) => d || []),
  create: (input: TaskInput) => req<TemplateTask>(`/template-tasks`, { method: 'POST', body: body(input) }),
  update: (id: number, input: TaskInput) => req<void>(`/template-tasks/${id}`, { method: 'PUT', body: body(input) }),
  remove: (id: number) => req<void>(`/template-tasks/${id}`, { method: 'DELETE' }),
}

// ---------- 文档标识 ----------
export interface FileRuleInput {
  task_id?: number
  file_name: string
  data_state: DataState
  required?: boolean
  allowed_file_types?: string
  naming_pattern?: string
  summary_pattern?: string
  drafter?: string
  sensitivity_level?: Sensitivity
  retention_policy?: string
  // L6 文档标识管控类字段
  category?: string
  security_requirement?: string
  diffusion_requirement?: string
  archive_requirement?: string
  retention_period_days?: number | null
  destruction_rule?: string
}
export const fileRuleApi = {
  list: (taskId: number) => req<TemplateFileRule[]>(`/template-file-rules?task_id=${taskId}`).then((d) => d || []),
  create: (input: FileRuleInput) => req<TemplateFileRule>(`/template-file-rules`, { method: 'POST', body: body(input) }),
  update: (id: number, input: FileRuleInput) =>
    req<void>(`/template-file-rules/${id}`, { method: 'PUT', body: body(input) }),
  remove: (id: number) => req<void>(`/template-file-rules/${id}`, { method: 'DELETE' }),
}

// ---------- manage 已注册用户（供「项目负责人」等下拉）----------
export interface ManageUser {
  username: string
  display_name: string
  user_unit: string
  user_department: string
  status: string
}
export const manageUsersApi = {
  list: () => req<ManageUser[]>(`/manage-users`).then((d) => d || []),
}

// 数据业务分类已归口 manage 管理；scan 创作模版时从 manage 拉取分类供下拉选择
export const manageBusinessClassApi = {
  list: () => req<BusinessClass[]>(`/manage-business-classes`).then((d) => d || []),
}

// 我的工作事项（多人协同 P3/P4/P5）
export type WorkItemStatus = 'pending' | 'ready' | 'delivered'
export interface WorkItem {
  template_code: string
  template_name: string
  stage_id: number
  stage_code: string
  stage_name: string
  sort_order: number
  manager: string
  members: string
  delivered: number
  status: WorkItemStatus
}
export interface OutputRule {
  file_rule_code: string
  file_name: string
  allowed_file_types: string
}
export interface FinalSelection {
  file_rule_code: string
  source_file: string
}
// 在线编辑：process/ 下的过程文档（含空占位）
export interface StageDoc {
  name: string
  size: number
  empty: boolean
}
export const workItemsApi = {
  listMine: () => req<WorkItem[]>(`/my-work-items`).then((d) => d || []),
  start: (project_code: string, stage_code: string) =>
    req<{ stage_dir: string; process_dir: string; scaffolded: string[] }>(`/work-items/start`, { method: 'POST', body: body({ project_code, stage_code }) }),
  // 待交付定稿清单（output 文档标识）
  outputRules: (template_code: string, stage_code: string) =>
    req<OutputRule[]>(`/work-items/output-rules?template_code=${encodeURIComponent(template_code)}&stage_code=${encodeURIComponent(stage_code)}`).then((d) => d || []),
  // 可挑选的过程文件（非空）
  processFiles: (template_code: string, stage_code: string) =>
    req<string[]>(`/work-items/process-files?template_code=${encodeURIComponent(template_code)}&stage_code=${encodeURIComponent(stage_code)}`).then((d) => d || []),
  deliver: (template_code: string, stage_code: string, selections: FinalSelection[]) =>
    req<void>(`/work-items/deliver`, { method: 'POST', body: body({ template_code, stage_code, selections }) }),
  // 工作依据：input/ 下上游交付来的文件（只读）
  inputDocs: (template_code: string, stage_code: string) =>
    req<StageDoc[]>(`/work-items/input-docs?template_code=${encodeURIComponent(template_code)}&stage_code=${encodeURIComponent(stage_code)}`).then((d) => d || []),
  readInputDoc: (template_code: string, stage_code: string, name: string) =>
    req<{ content: string }>(`/work-items/input-doc?template_code=${encodeURIComponent(template_code)}&stage_code=${encodeURIComponent(stage_code)}&name=${encodeURIComponent(name)}`),
  // 在线编辑：过程文档清单（含空占位） / 读取内容 / 保存（路径由模版自动决定）
  processDocs: (template_code: string, stage_code: string) =>
    req<StageDoc[]>(`/work-items/process-docs?template_code=${encodeURIComponent(template_code)}&stage_code=${encodeURIComponent(stage_code)}`).then((d) => d || []),
  readDoc: (template_code: string, stage_code: string, name: string) =>
    req<{ content: string }>(`/work-items/doc?template_code=${encodeURIComponent(template_code)}&stage_code=${encodeURIComponent(stage_code)}&name=${encodeURIComponent(name)}`),
  saveDoc: (template_code: string, stage_code: string, name: string, content: string) =>
    req<{ path: string }>(`/work-items/doc`, { method: 'POST', body: body({ template_code, stage_code, name, content }) }),
  // 我立项的项目 + 结项（仅立项人本人、全量交付后可结项，校验在 manage）
  myProjects: () => req<MyProject[]>(`/my-projects`).then((d) => d || []),
  closeProject: (template_code: string, reason: string) =>
    req<void>(`/projects/close`, { method: 'POST', body: body({ template_code, reason }) }),
}

export type ProjectOverall = 'not_started' | 'in_progress' | 'completed' | 'closed'
export interface MyProject {
  template_code: string
  template_name: string
  overall: ProjectOverall
  percent: number
  delivered_stages: number
  total_stages: number
  current_stage_name: string | null
  current_manager: string | null
  can_close: boolean
  closed_by: string | null
  closed_at: string | null
  close_reason: string | null
}
export const PROJECT_OVERALL_LABELS: Record<ProjectOverall, string> = {
  not_started: '未开始',
  in_progress: '进行中',
  completed: '已完成待结项',
  closed: '已结项',
}
export const WORK_ITEM_STATUS_LABELS: Record<string, string> = {
  pending: '待就绪',
  ready: '可开始',
  delivered: '已交付',
}

// 归类 / 敏感级别 中文标签
export const SCOPE_LABELS: Record<string, string> = {
  industry: '行业通用',
  unit: '单位专属',
  department: '部门专属',
  person: '个人专属',
}
// 本地模版可保存的归类：不含「通用行业」（行业模版由中心下发，用户只能另存为 单位/部门/个人）。
export const LOCAL_SCOPE_LABELS: Record<string, string> = {
  unit: '单位专属',
  department: '部门专属',
  person: '个人专属',
}
export const SENSITIVITY_LABELS: Record<string, string> = {
  core: '核心级',
  important: '重要级',
  general: '一般级',
}
