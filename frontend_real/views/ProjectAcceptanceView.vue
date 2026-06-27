<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { API_BASE } from '@/services/api'
import TeamMemberPicker from '@/components/TeamMemberPicker.vue'
import ProjectTemplateEditor from '@/components/ProjectTemplateEditor.vue'

interface AssignedProject {
  id: number
  project_name: string
  owner_name: string
  submitted_by: string | null
  status: string
  reviewed_at: string | null
  accepted_by: string | null
  accepted_at: string | null
  create_time: string
  template_id: number | null
  template_code?: string | null
  template_version?: string | null
  sensitivity_level?: string | null
  project_scope?: string | null // 项目层级：单位级结项时可把定稿归卷到单位室
  data_owner?: string | null
  team_count?: number // 项目团队人数（manage 带回）；0 表示尚未组队，不能关联模版
  initiation_done?: boolean // 立项过程是否结束（所有文件任务都已被受理）
  project_template_edited?: boolean // 本项目专属模版是否被编辑过（提取认定模版的门禁）
  // 完整立项书内容（2026-06-11 端到端补齐，供负责人关联模版前了解项目）
  project_code?: string | null
  department?: string | null
  approval_basis?: string | null
  description?: string | null
}

// 承接可选模版（合并 本地 + 在线/远程）
interface TemplateOption {
  value: string // 唯一键：'L:'+id（本地）/ 'R:'+id（在线）
  source: 'local' | 'remote'
  id: number // 本地为本地模版 id；在线为 manage 远端 id
  template_code: string
  template_name: string
  template_version: string
  label: string
  certified?: boolean // 项目认定模版（单位最高权威）→ 置顶+标记推荐
}

interface TemplateFileRuleLite {
  key: string // 对话框内唯一键（防止 file_rule_code 为空/重复导致下拉联动）
  file_rule_code: string
  file_name: string
  task_code?: string
}
interface TemplateStage {
  key: string // 对话框内唯一键（防止 stage_code 为空/重复导致下拉联动）
  id: number
  stage_code: string
  stage_name: string
  sort_order?: number
  file_rules?: TemplateFileRuleLite[]
}

interface OwnerOption {
  username: string
  display_name: string
  user_unit?: string | null
  user_department?: string | null
  role?: string | null
  label: string
}

// 项目团队成员（manage 带回角色）：lead 项目负责人 / core 核心成员 / participant 项目参与人
interface TeamMemberFull {
  username: string
  display_name: string | null
  roles?: string[]
}
const PROJECT_ROLE_LABEL: Record<string, string> = { lead: '项目负责人', core: '项目核心成员', participant: '项目参与人' }
function projectRoleLabels(roles: string[] | undefined): string[] {
  return (roles || []).map(r => PROJECT_ROLE_LABEL[r] || r)
}

interface StageDetail {
  id: number
  stage_code: string
  stage_name: string
  assignee_username: string
  status: string
  sort_order: number
  started_at: string | null
  completed_at: string | null
  create_time: string
}

const loading = ref(false)
const items = ref<AssignedProject[]>([])
const loginName = ref('') // 后端回带的"当前识别登录名"，用于空列表时的身份诊断
const loadError = ref('') // 加载失败信息（如"未识别登录用户"）
// html：可选富文本（如需局部加粗）。设置后优先按 HTML 渲染；其余消息仍走纯文本（安全）。
const snackbar = ref<{ show: boolean; text: string; color: string; html?: string }>({ show: false, text: '', color: 'success' })

const detailsDialog = ref({
  open: false,
  loading: false,
  project: null as AssignedProject | null,
  stages: [] as StageDetail[],
})

const headers = [
  { title: '项目名称', key: 'project_name' },
  { title: '提交人', key: 'submitted_by', width: '120px' },
  { title: '状态', key: 'status', width: '110px' },
  { title: '立项时间', key: 'create_time', width: '170px' },
  { title: '承接时间', key: 'accepted_at', width: '170px' },
  { title: '操作', key: 'actions', width: '220px', sortable: false },
]

// 承接弹窗状态
const acceptDialog = ref({
  open: false,
  busy: false,
  applicationId: 0,
  projectName: '',
  templateKey: null as string | null,
  templates: [] as TemplateOption[],
  loadingTemplates: false,
  stages: [] as TemplateStage[],
  loadingStages: false,
  assignments: {} as Record<string, string>, // stage_code → assignee_username
  fileAssignments: {} as Record<string, string>, // file_rule_code → assignee_username（文件标识级，可选，仅记录）
  team: [] as string[], // 本项目团队成员 username（分工时置顶候选）
  batchAssignee: '' as string,     // 批量指派：目标负责人
  batchSelected: [] as string[],   // 批量指派：勾选的环节 key
})

// 第一步：选择模版弹窗状态
const selectDialog = ref({
  open: false,
  busy: false,
  applicationId: 0,
  projectName: '',
  sensitivity: '' as string | null,
  dataOwner: '' as string | null,
  // 完整立项书只读信息
  projectCode: '' as string | null,
  ownerName: '' as string | null,
  department: '' as string | null,
  approvalBasis: '' as string | null,
  description: '' as string | null,
  createTime: '' as string | null,
  templateKey: null as string | null,
  templates: [] as TemplateOption[],
  loadingTemplates: false,
})

const ownerOptions = ref<OwnerOption[]>([])

// ── 项目团队（项目级）：立项指定负责人即成队（含负责人），承接后可继续拉人；关联模版前的人池 ──
const teamDialog = ref({
  open: false,
  busy: false,
  loading: false,
  locked: false, // true=从分工弹窗「临时拉人入队」打开，项目固定、保存后回填分工人池
  applicationId: 0,
  projectName: '',
  members: [] as string[], // 选中的 username 列表
  roleMap: {} as Record<string, string[]>, // username → 已本地化角色文案
  leadUsername: '', // 项目负责人 username（锁定不可移除）
})
// 可组队的项目：已承接(taken) 或 分工中(assigning)，即未完成分工(未到 accepted)
const eligibleTeamProjects = computed(() => items.value.filter(it => it.status === 'taken' || it.status === 'assigning'))
const canFormTeam = computed(() => eligibleTeamProjects.value.length > 0)

// 拉取团队（带角色）。立项负责人始终在列（manage 端保证）。
async function loadTeam(applicationId: number): Promise<TeamMemberFull[]> {
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/team?application_id=${applicationId}`)
    const j = await r.json()
    if (j.success && Array.isArray(j.data)) return j.data as TeamMemberFull[]
  } catch {}
  return []
}
// 由团队成员构造角色文案表（username → 角色标签数组），供 TeamMemberPicker 展示
function buildRoleMap(members: TeamMemberFull[]): Record<string, string[]> {
  const m: Record<string, string[]> = {}
  for (const x of members) m[x.username] = projectRoleLabels(x.roles)
  return m
}
function leadOf(members: TeamMemberFull[]): string {
  return members.find(x => (x.roles || []).includes('lead'))?.username || ''
}

// 取某项目的负责人 username（前端兜底用）：立项即指定，items 里始终带 owner_name。
function ownerOf(applicationId: number): string {
  return (items.value.find(it => it.id === applicationId)?.owner_name || '').trim()
}

// 应用团队到弹窗。兜底：无论后端返回什么，都用项目负责人(owner_name)补齐——
// 立项即成队，负责人必在队、置顶、带「项目负责人」标签且锁定，避免出现"当前团队为空"。
function applyTeam(members: TeamMemberFull[], ownerName?: string) {
  let list = members || []
  const owner = (ownerName || '').trim()
  if (owner && !list.some(m => m.username === owner)) {
    list = [{ username: owner, display_name: null, roles: ['lead'] }, ...list]
  }
  teamDialog.value.members = list.map(m => m.username)
  teamDialog.value.roleMap = buildRoleMap(list)
  teamDialog.value.leadUsername = leadOf(list) || owner
}

async function openTeamDialog() {
  const first = eligibleTeamProjects.value[0]
  teamDialog.value = { open: true, busy: false, loading: true, locked: false, applicationId: first?.id || 0, projectName: first?.project_name || '', members: [], roleMap: {}, leadUsername: '' }
  if (first) applyTeam(await loadTeam(first.id), first.owner_name)
  teamDialog.value.loading = false
}

// 从分工弹窗内打开「临时拉人入队」：项目固定为正在分工的项目，保存后回填团队人池
async function openTeamForAssign() {
  const d = acceptDialog.value
  teamDialog.value = { open: true, busy: false, loading: true, locked: true, applicationId: d.applicationId, projectName: d.projectName, members: [], roleMap: {}, leadUsername: '' }
  applyTeam(await loadTeam(d.applicationId), ownerOf(d.applicationId))
  teamDialog.value.loading = false
}

// 行内「项目团队」：查看/扩充指定已承接项目的团队（关联模版的前置）。项目固定。
async function openTeamForProject(item: AssignedProject) {
  teamDialog.value = { open: true, busy: false, loading: true, locked: true, applicationId: item.id, projectName: item.project_name, members: [], roleMap: {}, leadUsername: '' }
  applyTeam(await loadTeam(item.id), item.owner_name)
  teamDialog.value.loading = false
}

// 切换关联项目 → 载入该项目已存团队
async function onTeamProjectChange() {
  if (!teamDialog.value.applicationId) { teamDialog.value.members = []; teamDialog.value.roleMap = {}; teamDialog.value.leadUsername = ''; return }
  const proj = eligibleTeamProjects.value.find(p => p.id === teamDialog.value.applicationId)
  teamDialog.value.projectName = proj?.project_name || ''
  teamDialog.value.loading = true
  applyTeam(await loadTeam(teamDialog.value.applicationId), proj?.owner_name)
  teamDialog.value.loading = false
}

async function saveTeam() {
  const d = teamDialog.value
  if (!d.applicationId) { snackbar.value = { show: true, text: '请先选择关联项目', color: 'warning' }; return }
  d.busy = true
  try {
    const members = d.members.map(u => {
      const o = ownerOptions.value.find(x => x.username === u)
      return { username: u, display_name: o?.display_name || u }
    })
    const r = await fetch(`${API_BASE}/centralized-projects/team?application_id=${d.applicationId}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ members }),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: `团队已保存（${members.length} 人）`, color: 'success' }
      d.open = false
      // 若分工弹窗正开着同一项目，回填其团队人池，临时拉的人立即可选
      if (acceptDialog.value.open && acceptDialog.value.applicationId === d.applicationId) {
        acceptDialog.value.team = d.members.slice()
      }
      // 刷新列表，回填团队人数等
      await load()
    }
    else snackbar.value = { show: true, text: '保存团队失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '保存团队失败：' + (e?.message || String(e)), color: 'error' }
  } finally { d.busy = false }
}

// 分工/指派时的候选项：只从本项目团队中选人（团队 = 实际班底）。
// 人不够时用对话框内的「临时拉人入队」补员（永久入队），不再摊开全量注册用户。
function assigneeItems(team: string[]): OwnerOption[] {
  const teamSet = new Set(team)
  return ownerOptions.value
    .filter(o => teamSet.has(o.username))
    .map(o => ({ ...o, label: `${o.display_name} (${o.username})${o.user_department ? ' · ' + o.user_department : ''}` }))
}

// 行操作按钮：立项指定负责人即成队（团队至少含负责人），故不再有「组建团队」空态。
// 工程进展：approved(已立项) →[承接]→ taken(已承接) →[关联模版]→ assigning(分工中) →[分工]→ accepted(受理中) →[结项]→ closed(已结项)
function rowActions(it: AssignedProject): string[] {
  if (it.status === 'approved') return ['承接']
  // taken：刚承接、待关联模版（兼容历史 taken 但已带 template_id 的数据，直接进编辑/分工）
  if (it.status === 'taken') return it.template_id ? ['项目团队', '工作事项', '分工'] : ['项目团队', '关联模版']
  // assigning：已关联模版、分工中
  if (it.status === 'assigning') return ['项目团队', '工作事项', '分工']
  if (it.status === 'accepted') return ['已分工', ...(canClose(it) ? ['结项'] : [])]
  // closed：结项后，若立项过程中改过模版结构，提供「提取项目模版」（提取为单位项目认定模版）
  if (it.status === 'closed') return it.project_template_edited ? ['提取项目模版'] : []
  return []
}

// ── 结项：项目负责人在本页对自己负责的项目做收尾（全环节交付完成后可结项）──
interface ProgressInfo { total: number; done: number; allDone: boolean }
const progressMap = ref<Record<number, ProgressInfo>>({})
// 仅对 accepted 项目按 manage 环节进度判断是否可结项（全部 completed）。
async function loadProgress() {
  const map: Record<number, ProgressInfo> = {}
  await Promise.all(items.value.filter(it => it.status === 'accepted').map(async (it) => {
    try {
      const r = await fetch(`${API_BASE}/centralized-projects/${it.id}/stages`)
      const j = await r.json()
      if (j.success) {
        const stages = (j.data || []) as Array<{ status: string }>
        const done = stages.filter(s => s.status === 'completed').length
        map[it.id] = { total: stages.length, done, allDone: stages.length > 0 && done === stages.length }
      }
    } catch { /* ignore */ }
  }))
  progressMap.value = map
}
function canClose(it: AssignedProject): boolean {
  return it.status === 'accepted' && !!progressMap.value[it.id]?.allDone
}

// 单位室目标房间（按密级）：一般→资料室 / 重要→档案室 / 核心→保密室
function unitRoomLabel(sens: string | null | undefined): string {
  switch ((sens || '').trim()) {
    case 'core':
    case 'core_secret': return '单位保密室'
    case 'important': return '单位档案室'
    default: return '单位资料室'
  }
}

interface FinalFile { id: number; file_name: string; sensitivity_level: string; storage_location: string }
const closeDialog = ref<{
  open: boolean; busy: boolean; project: AssignedProject | null; summary: string
  isUnit: boolean; loadingFiles: boolean; files: FinalFile[]; selected: number[]
}>({ open: false, busy: false, project: null, summary: '', isUnit: false, loadingFiles: false, files: [], selected: [] })

async function openCloseDialog(item: AssignedProject) {
  const isUnit = (item.project_scope || '') === 'unit'
  closeDialog.value = { open: true, busy: false, project: item, summary: '', isUnit, loadingFiles: isUnit, files: [], selected: [] }
  if (isUnit) {
    try {
      const r = await fetch(`${API_BASE}/centralized-projects/${item.id}/final-files?project_code=${encodeURIComponent(item.project_code || '')}`)
      const j = await r.json()
      if (j.success) closeDialog.value.files = (j.data || []).map((f: any) => ({
        id: f.id, file_name: f.file_name, sensitivity_level: f.sensitivity_level, storage_location: f.storage_location,
      }))
    } catch { /* ignore，部门柜为空或拉取失败则清单为空，可直接结项 */ }
    finally { closeDialog.value.loadingFiles = false }
  }
}

async function doClose() {
  const d = closeDialog.value
  const t = d.project
  if (!t) return
  d.busy = true
  try {
    const body: any = { closure_summary: d.summary }
    if (d.isUnit && d.selected.length > 0) body.move_file_ids = d.selected
    const r = await fetch(`${API_BASE}/centralized-projects/${t.id}/close`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
    })
    const j = await r.json()
    if (j.success) {
      d.open = false
      const moved = Number(j.data?._moveResult?.moved || 0)
      snackbar.value = {
        show: true,
        text: moved > 0 ? `「${t.project_name}」已结项，${moved} 份定稿已归卷单位室` : `「${t.project_name}」已结项`,
        color: 'success',
      }
      await load()
    } else {
      snackbar.value = { show: true, text: '结项失败：' + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '结项失败：' + (e?.message || String(e)), color: 'error' }
  } finally { d.busy = false }
}

// 承接确认弹窗：点「承接」先看立项书详情，再确认接下项目（approved → taken）。
const acceptConfirmDialog = ref({
  open: false,
  busy: false,
  project: null as AssignedProject | null,
  cycle_start: '', // 项目周期·计划起始（承接时填，选填）
  cycle_end: '',   // 项目周期·计划结束
})
// 本地今日 YYYY-MM-DD（不用 toISOString 以免被 UTC 偏移成前一天）。
function todayLocal(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
function openAcceptConfirm(item: AssignedProject) {
  // 项目周期起止默认今天：负责人不改即按默认提交，无需强制选择。
  const today = todayLocal()
  acceptConfirmDialog.value = {
    open: true, busy: false, project: item, cycle_start: today, cycle_end: today,
  }
}

// ── 编辑工作事项（项目专属模版，stage 级）──
const stageEditor = ref({ open: false, applicationId: 0 })
function openStageEditor(item: AssignedProject) {
  stageEditor.value = { open: true, applicationId: item.id }
}
// 模版编辑保存后的统一回调：刷新项目列表；若「分工」弹窗开着，重载其工作环节（保留已选负责人）。
async function onEditorSaved() {
  await load()
  if (acceptDialog.value.open && acceptDialog.value.templateKey) {
    const prev: Record<string, string> = {}
    for (const s of acceptDialog.value.stages) {
      const a = acceptDialog.value.assignments[s.key]
      if (a) prev[s.stage_code] = a
    }
    await onTemplateSelect() // 重新拉环节（会重置 assignments）
    const next: Record<string, string> = {}
    for (const s of acceptDialog.value.stages) {
      if (prev[s.stage_code]) next[s.key] = prev[s.stage_code]
    }
    acceptDialog.value.assignments = next
  }
}

// ── 提取「项目模版」（结项后）：先弹窗展示本项目改了哪些工作事项/文件任务/文件标识，确认后提取为单位项目认定模版 ──
interface TemplateChange { level: string; type: string; stage: string; task?: string; name: string; from?: string; to?: string }
const extractDialog = ref({
  open: false,
  loadingId: null as number | null, // 正在拉取差异的行（按钮 loading）
  busy: false,                       // 确认提取中
  project: null as AssignedProject | null,
  changes: [] as TemplateChange[],
  hasBaseline: true,
})
async function openExtractDialog(item: AssignedProject) {
  extractDialog.value.loadingId = item.id
  try {
    const qs = `application_id=${item.id}${item.project_code ? `&project_code=${encodeURIComponent(item.project_code)}` : ''}`
    const r = await fetch(`${API_BASE}/centralized-projects/template-diff?${qs}`)
    const j = await r.json()
    if (!j.success) { snackbar.value = { show: true, text: '获取改动清单失败：' + (j.error || ''), color: 'error' }; return }
    extractDialog.value = {
      open: true, loadingId: null, busy: false, project: item,
      changes: (j.data?.changes || []) as TemplateChange[],
      hasBaseline: j.data?.has_baseline !== false,
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '获取改动清单失败：' + (e?.message || String(e)), color: 'error' }
  } finally { extractDialog.value.loadingId = null }
}
const changeTypeLabel = (t: string) => ({ added: '新增', removed: '删除', renamed: '改名' } as Record<string, string>)[t] || t
const changeTypeColor = (t: string) => ({ added: 'success', removed: 'error', renamed: 'warning' } as Record<string, string>)[t] || 'grey'
const levelLabel = (l: string) => ({ stage: '工作环节', task: '文件任务', file_rule: '文件标识' } as Record<string, string>)[l] || l
async function confirmExtract() {
  const item = extractDialog.value.project
  if (!item) return
  extractDialog.value.busy = true
  try {
    const qs = `application_id=${item.id}${item.project_code ? `&project_code=${encodeURIComponent(item.project_code)}` : ''}`
    const r = await fetch(`${API_BASE}/centralized-projects/extract-template?${qs}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
    const j = await r.json()
    if (j.success) {
      extractDialog.value.open = false
      snackbar.value = { show: true, text: '已提取为「项目认定模版」，已保存到本地模版库', html: '已提取为「项目认定模版」，已保存到<strong>本地模版库</strong>（业务模版管理可查看）', color: 'success' }
      await load()
    } else snackbar.value = { show: true, text: '提取失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '提取失败：' + (e?.message || String(e)), color: 'error' }
  } finally { extractDialog.value.busy = false }
}

// 承接：approved → taken（接下项目，之后才可继续拉人 / 关联模版 / 分工）
const acceptingId = ref(0)
async function acceptProject(item: AssignedProject) {
  acceptingId.value = item.id
  acceptConfirmDialog.value.busy = true
  try {
    // 承接时把项目周期(计划起止日期，选填)一并提交，回显给立项人。
    const payload = {
      cycle_start: (acceptConfirmDialog.value.cycle_start || '').trim(),
      cycle_end: (acceptConfirmDialog.value.cycle_end || '').trim(),
    }
    const r = await fetch(`${API_BASE}/centralized-projects/accept-project?id=${item.id}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: '已承接，可在「项目团队」继续拉人 / 关联模版 / 分工', color: 'success' }
      acceptConfirmDialog.value.open = false
      await load()
    }
    else snackbar.value = { show: true, text: '承接失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '承接失败：' + (e?.message || String(e)), color: 'error' }
  } finally { acceptingId.value = 0; acceptConfirmDialog.value.busy = false }
}

// 加载 本地模版(/templates/authoring) + 在线通用模版(/templates/remote-list) 合并为下拉项
async function loadTemplateOptions(): Promise<{ opts: TemplateOption[]; bothFailed: boolean }> {
  const [localR, remoteR] = await Promise.all([
    fetch(`${API_BASE}/templates/authoring`).then(r => r.json()).catch(() => ({ success: false })),
    fetch(`${API_BASE}/templates/remote-list`).then(r => r.json()).catch(() => ({ success: false })),
  ])
  const opts: TemplateOption[] = []
  if (localR.success && Array.isArray(localR.data)) {
    for (const t of localR.data) {
      const certified = t.certified === 1 || t.certified === true
      opts.push({
        value: `L:${t.id}`, source: 'local', id: t.id,
        template_code: t.template_code, template_name: t.template_name, template_version: t.template_version,
        certified,
        label: `${certified ? '★项目认定 · ' : '本地 · '}${t.template_code} ${t.template_version} — ${t.template_name}`,
      })
    }
  }
  if (remoteR.success && Array.isArray(remoteR.data)) {
    for (const t of remoteR.data) {
      opts.push({
        value: `R:${t.id}`, source: 'remote', id: t.id,
        template_code: t.template_code, template_name: t.template_name, template_version: t.template_version,
        label: `在线 · ${t.template_code} ${t.template_version} — ${t.template_name}`,
      })
    }
  }
  // 项目认定模版（单位最高权威）置顶，便于优先选用
  opts.sort((a, b) => Number(b.certified || false) - Number(a.certified || false))
  return { opts, bothFailed: !localR.success && !remoteR.success }
}

async function loadOwnerOptions() {
  try {
    // 用 /manage-users（带 role），排除系统管理员——管理员不作为环节责任人候选。
    const r = await fetch(`${API_BASE}/manage-users`)
    const j = await r.json()
    if (j.success && Array.isArray(j.data)) {
      ownerOptions.value = j.data
        .filter((u: any) => u.role !== 'system_admin')
        .map((u: any) => ({
          username: u.username,
          display_name: u.display_name || u.username,
          user_unit: u.user_unit || '',
          user_department: u.user_department || '',
          role: u.role || '',
          label: `${u.display_name || u.username} (${u.username})`,
        }))
    }
  } catch {}
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/assigned`)
    const j = await r.json()
    if (j.success) {
      items.value = j.data || []
      loginName.value = j.username || ''
      loadProgress()
    } else {
      loadError.value = j.error || '加载失败'
      snackbar.value = { show: true, text: '加载失败：' + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    loadError.value = e?.message || String(e)
    snackbar.value = { show: true, text: '加载失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    loading.value = false
  }
}

async function openAcceptDialog(item: AssignedProject) {
  acceptDialog.value = {
    open: true,
    busy: false,
    applicationId: item.id,
    projectName: item.project_name,
    templateKey: null,
    templates: [],
    loadingTemplates: true,
    stages: [],
    loadingStages: false,
    assignments: {},
    fileAssignments: {},
    team: [],
    batchAssignee: '',
    batchSelected: [],
  }
  // 载入本项目团队（分工时把团队成员置顶为候选）
  loadTeam(item.id).then(t => { acceptDialog.value.team = t.map(m => m.username) })
  // 合并 本地模版(/templates/authoring) + 在线通用模版(/templates/remote-list)
  try {
    const { opts, bothFailed } = await loadTemplateOptions()
    acceptDialog.value.templates = opts
    if (bothFailed) {
      snackbar.value = { show: true, text: '加载模版列表失败', color: 'error' }
    }
    // 分工步：模版已在第一步选定 → 预选并自动加载工作环节
    if (item.template_id) {
      // 按 template_code 优先匹配：本地(/templates/authoring) 与 在线(/templates/remote-list)
      // 是各自独立的 id 空间，会撞号；template_code 才是稳定、已持久化、跨源的键。
      const pre = (item.template_code ? acceptDialog.value.templates.find(t => t.template_code === item.template_code) : undefined)
        || acceptDialog.value.templates.find(t => t.id === item.template_id)
      if (pre) {
        acceptDialog.value.templateKey = pre.value
        await onTemplateSelect()
      } else {
        snackbar.value = { show: true, text: `未能匹配已选模版（${item.template_code || ''}），请确认模版仍存在`, color: 'warning' }
      }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载模版列表失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    acceptDialog.value.loadingTemplates = false
  }
}

// 第一步：打开「选择模版」对话框（立项基本信息只读 + 关联模版下拉）
async function openSelectTemplate(item: AssignedProject) {
  selectDialog.value = {
    open: true,
    busy: false,
    applicationId: item.id,
    projectName: item.project_name,
    sensitivity: item.sensitivity_level || '',
    dataOwner: item.data_owner || '',
    projectCode: item.project_code || '',
    ownerName: item.owner_name || '',
    department: item.department || '',
    approvalBasis: item.approval_basis || '',
    description: item.description || '',
    createTime: item.create_time || '',
    templateKey: null,
    templates: [],
    loadingTemplates: true,
  }
  try {
    const { opts, bothFailed } = await loadTemplateOptions()
    selectDialog.value.templates = opts
    if (bothFailed) {
      snackbar.value = { show: true, text: '加载模版列表失败', color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载模版列表失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    selectDialog.value.loadingTemplates = false
  }
}

// 第一步「确定」：把所选模版写回立项 → POST set-template?id=
async function confirmTemplate() {
  const d = selectDialog.value
  const tpl = d.templates.find(t => t.value === d.templateKey)
  if (!tpl) {
    snackbar.value = { show: true, text: '请先选择关联模版', color: 'warning' }
    return
  }
  d.busy = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/set-template?id=${d.applicationId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        template_id: tpl.id,
        template_code: tpl.template_code,
        template_version: tpl.template_version,
        // 让 scan 后端知道该模版来源：在线(remote)→template-server，本地→local。
        // 后端据此确保模版结构 ingest 进 manage，任务层才查得到文件任务。
        source: tpl.source === 'remote' ? 'template-server' : 'local',
      }),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: '已选定模版，可继续分工', color: 'success' }
      selectDialog.value.open = false
      await load()
    } else {
      snackbar.value = { show: true, text: '选定模版失败：' + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '选定模版失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    d.busy = false
  }
}

// 选模板 → 拉该模板的工作环节
async function onTemplateSelect() {
  const key = acceptDialog.value.templateKey
  acceptDialog.value.stages = []
  acceptDialog.value.assignments = {}
  acceptDialog.value.fileAssignments = {}
  acceptDialog.value.batchSelected = []
  acceptDialog.value.batchAssignee = ''
  if (!key) return
  const opt = acceptDialog.value.templates.find(t => t.value === key)
  if (!opt) return
  acceptDialog.value.loadingStages = true
  try {
    let localId: number | undefined
    if (opt.source === 'local') {
      // 本地模版：直接用本地 id 拿结构，无需同步
      localId = opt.id
    } else {
      // 在线模版来自模版管理平台(remote-list)：按 template_code 回到同一台服务器拉详情。
      // 注意不能用 opt.id（那是平台的 id）去 manage 查——两台是不同服务器。
      const syncR = await fetch(`${API_BASE}/templates/sync`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: 'template-server', code: opt.template_code, version: opt.template_version }),
      })
      const syncJ = await syncR.json()
      if (!syncJ.success) {
        snackbar.value = { show: true, text: '同步模板失败：' + (syncJ.error || ''), color: 'error' }
        return
      }
      localId = syncJ.data?.template?.id || syncJ.data?.id
    }
    if (!localId) {
      snackbar.value = { show: true, text: '取模版本地 id 失败', color: 'error' }
      return
    }
    const r = await fetch(`${API_BASE}/templates/${localId}`)
    const j = await r.json()
    if (j.success && j.data?.stages) {
      acceptDialog.value.stages = (j.data.stages as any[]).map((s, idx) => ({
        key: `s${idx}`,
        id: s.id,
        stage_code: s.stage_code,
        stage_name: s.stage_name,
        sort_order: s.sort_order,
        file_rules: (s.file_rules as any[] || []).map((fr, ridx) => ({
          key: `s${idx}f${ridx}`,
          file_rule_code: fr.file_rule_code,
          file_name: fr.file_name,
          task_code: fr.task_code || '',
        })),
      }))
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载工作环节失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    acceptDialog.value.loadingStages = false
  }
}

const canSubmitAccept = computed(() => {
  const d = acceptDialog.value
  if (!d.templateKey || d.stages.length === 0) return false
  // 每个环节都要指派负责人（环节名来自模版、只读，必然非空）。
  return d.stages.every((s: any) => !!d.assignments[s.key]?.trim() && !!s.stage_name?.trim())
})

// ── 批量指派（便捷能力，不替代逐项指派）：勾选多个工作环节 → 选一名负责人 → 一键填入 ──
const allStagesSelected = computed({
  get: () => {
    const d = acceptDialog.value
    return d.stages.length > 0 && d.batchSelected.length === d.stages.length
  },
  set: (v: boolean) => {
    acceptDialog.value.batchSelected = v ? acceptDialog.value.stages.map((s: any) => s.key) : []
  },
})
function applyBatchAssign() {
  const d = acceptDialog.value
  const who = (d.batchAssignee || '').trim()
  if (!who || d.batchSelected.length === 0) return
  for (const key of d.batchSelected) d.assignments[key] = who
  const n = d.batchSelected.length
  // 应用后清空勾选与批量选择框，方便下一轮；逐项仍可再微调。
  d.batchSelected = []
  d.batchAssignee = ''
  snackbar.value = { show: true, text: `已批量指派 ${n} 个工作环节`, color: 'success' }
}

// 提交前预览二次确认：列出每个工作环节的指派负责人。
const assignPreview = ref({ open: false, rows: [] as Array<{ stage: string; assignee: string }> })
function openAssignPreview() {
  if (!canSubmitAccept.value) return
  const d = acceptDialog.value
  assignPreview.value = {
    open: true,
    rows: d.stages.map((s: any) => ({ stage: s.stage_name, assignee: displayName(d.assignments[s.key]) })),
  }
}

async function confirmAccept() {
  if (!canSubmitAccept.value) return
  const d = acceptDialog.value
  const tpl = d.templates.find(t => t.value === d.templateKey)
  if (!tpl) return
  d.busy = true
  try {
    // 分工阶段不再增删/改名工作环节（工作事项已锁定，编辑走「工作事项」按钮）；
    // 这里只把模版既有环节与所选负责人下发。
    // 项目负责人这一步只指派工作环节负责人；文件任务/文件标识的指派由
    // 环节负责人在第二步「文件任务指派」完成，这里不再下发文件标识级指派。
    const payload = {
      template_id: tpl.id,
      template_code: tpl.template_code,
      template_version: tpl.template_version,
      // 让 scan 后端把 template_id 换成 manage 侧 id（与 set-template 一致），
      // 否则 accept 会用平台 id 覆盖掉，任务层就查不到文件任务。
      source: tpl.source === 'remote' ? 'template-server' : 'local',
      stage_assignments: d.stages.map((s, idx) => ({
        stage_code: s.stage_code,
        stage_name: s.stage_name,
        assignee_username: d.assignments[s.key],
        sort_order: s.sort_order ?? idx,
      })),
      file_rule_assignments: [],
    }
    const r = await fetch(`${API_BASE}/centralized-projects/${d.applicationId}/accept`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: '分工成功，已下发环节任务', color: 'success' }
      assignPreview.value.open = false
      acceptDialog.value.open = false
      await load()
    } else {
      snackbar.value = { show: true, text: '承接失败：' + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '承接失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    d.busy = false
  }
}

function statusColor(s: string): string {
  return ({ approved: 'success', taken: 'info', assigning: 'teal', accepted: 'primary', pending: 'warning', rejected: 'error', closed: 'grey' } as Record<string, string>)[s] || 'grey'
}

function statusLabel(s: string): string {
  return ({ approved: '待承接', taken: '已承接', assigning: '分工中', accepted: '已分工', pending: '待审核', rejected: '已驳回', closed: '已结项' } as Record<string, string>)[s] || s
}

function sensLabel(s: string | null | undefined): string {
  if (!s) return '-'
  return ({ core: '核心', important: '重要', general: '一般' } as Record<string, string>)[s] || s
}

function formatTime(t: string | null): string {
  if (!t) return '-'
  const s = String(t).trim()
  if (!s) return '-'
  // manage 端时间为 UTC（SQLite CURRENT_TIMESTAMP）；无时区标记时按 UTC 解析，再转中国时区展示。
  const hasTz = /[zZ]$/.test(s) || /[+-]\d{2}:?\d{2}$/.test(s)
  const d = new Date(s.replace(' ', 'T') + (hasTz ? '' : 'Z'))
  if (isNaN(d.getTime())) return s.substring(0, 19).replace('T', ' ')
  const p = new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai', hourCycle: 'h23',
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  }).formatToParts(d).reduce((m, x) => { m[x.type] = x.value; return m }, {} as Record<string, string>)
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second}`
}

function stageStatusColor(s: string): string {
  return ({ pending: 'warning', in_progress: 'primary', completed: 'success' } as Record<string, string>)[s] || 'grey'
}

function stageStatusLabel(s: string): string {
  return ({ pending: '待开始', in_progress: '进行中', completed: '已完成' } as Record<string, string>)[s] || s
}

async function openDetails(item: AssignedProject) {
  detailsDialog.value = { open: true, loading: true, project: item, stages: [] }
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/${item.id}/stages`)
    const j = await r.json()
    if (j.success) {
      const list = (j.data || []) as StageDetail[]
      detailsDialog.value.stages = list.sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0))
    } else {
      snackbar.value = { show: true, text: '加载详情失败：' + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载详情失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    detailsDialog.value.loading = false
  }
}

// username → 用户名称（显示名）。取自 manage-users 人池，查不到则回退展示登录名。
function displayName(username: string | null | undefined): string {
  const u = (username || '').trim()
  if (!u) return '—'
  return ownerOptions.value.find(x => x.username === u)?.display_name || u
}

const overallProgress = computed(() => {
  const total = detailsDialog.value.stages.length
  if (total === 0) return 0
  const completed = detailsDialog.value.stages.filter(s => s.status === 'completed').length
  return Math.round((completed / total) * 100)
})

onMounted(async () => {
  await loadOwnerOptions()
  await load()
})
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-handshake-outline</v-icon>
      环节分工
      <v-spacer />
      <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
    </v-card-title>
    <v-card-subtitle>点「承接」可先查看立项书再接下项目；项目立项时即已成队（含你这位项目负责人），在「项目团队」里继续拉人，再「关联模版」绑定数据业务模版、「分工」为每个工作环节指派负责人；环节负责人在自己 scan 的「任务指派」里再为本环节的文件任务派人。</v-card-subtitle>

    <!-- 身份诊断（增量）：识别不到登录用户时明确报错。 -->
    <v-alert v-if="loadError" type="error" variant="tonal" density="compact" class="mx-4 mb-2">
      {{ loadError }}
    </v-alert>

    <v-data-table
      :headers="headers"
      :items="items"
      :loading="loading"
      item-value="id"
      :items-per-page="50"
      hide-default-footer
    >
      <template #item.status="{ item }">
        <v-chip size="x-small" :color="statusColor(item.status)" variant="tonal">{{ statusLabel(item.status) }}</v-chip>
        <v-chip v-if="item.initiation_done" size="x-small" color="teal" variant="flat" class="ml-1" prepend-icon="mdi-flag-checkered">立项已结束</v-chip>
      </template>
      <template #item.create_time="{ item }">{{ formatTime(item.create_time) }}</template>
      <template #item.accepted_at="{ item }">{{ formatTime(item.accepted_at) }}</template>
      <template #item.actions="{ item }">
        <div class="d-flex align-center" style="gap: 6px;">
          <v-btn
            v-if="rowActions(item).includes('承接')"
            size="small" color="success" variant="tonal"
            prepend-icon="mdi-handshake-outline"
            :loading="acceptingId === item.id"
            @click="openAcceptConfirm(item)"
          >承接</v-btn>
          <v-btn
            v-if="rowActions(item).includes('项目团队')"
            size="small" color="indigo" variant="tonal"
            prepend-icon="mdi-account-group-outline"
            @click="openTeamForProject(item)"
          >项目团队</v-btn>
          <v-btn
            v-if="rowActions(item).includes('关联模版')"
            size="small" color="primary" variant="tonal"
            prepend-icon="mdi-file-document-outline"
            @click="openSelectTemplate(item)"
          >关联模版</v-btn>
          <v-btn
            v-if="rowActions(item).includes('工作事项')"
            size="small" color="indigo" variant="text"
            prepend-icon="mdi-file-tree-outline"
            @click="openStageEditor(item)"
          >工作事项</v-btn>
          <v-btn
            v-if="rowActions(item).includes('分工')"
            size="small" color="primary" variant="tonal"
            prepend-icon="mdi-account-multiple-plus-outline"
            @click="openAcceptDialog(item)"
          >分工</v-btn>
          <v-btn
            v-if="rowActions(item).includes('已分工')"
            size="small" color="primary" variant="text"
            prepend-icon="mdi-eye-outline"
            @click="openDetails(item)"
          >已分工</v-btn>
          <v-btn
            v-if="rowActions(item).includes('提取项目模版')"
            size="small" color="amber-darken-2" variant="tonal"
            prepend-icon="mdi-star-outline" :loading="extractDialog.loadingId === item.id"
            @click="openExtractDialog(item)"
          >提取项目模版</v-btn>
          <v-btn
            v-if="rowActions(item).includes('结项')"
            size="small" color="success" variant="flat"
            prepend-icon="mdi-flag-checkered"
            @click="openCloseDialog(item)"
          >结项</v-btn>
          <span v-if="rowActions(item).length === 0" class="text-caption text-medium-emphasis">-</span>
        </div>
      </template>
      <template v-slot:no-data>
        <div class="text-center py-8">
          <v-icon size="64" color="grey-lighten-1">mdi-clipboard-outline</v-icon>
          <div class="mt-4 text-grey">暂无分配给您的项目</div>
        </div>
      </template>
    </v-data-table>

    <!-- 提取项目模版：结项后展示改动清单，确认后提取为单位项目认定模版并存入本地模版库 -->
    <v-dialog v-model="extractDialog.open" max-width="720" scrollable>
      <v-card class="pv-extract">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="amber-darken-2">mdi-star-outline</v-icon>
          提取项目模版
        </v-card-title>
        <v-card-subtitle class="text-wrap">
          本项目立项过程中对模版结构所做的改动如下；确认后将提取为单位「项目认定模版」，保存到本地模版库（可在业务模版管理中查看、用于后续立项）。
        </v-card-subtitle>
        <v-card-text style="max-height:60vh;">
          <v-alert v-if="!extractDialog.hasBaseline" type="info" variant="tonal" density="compact" class="mb-3">
            未找到关联模版时的基线快照，无法逐条列出差异；仍可按当前最终结构提取。
          </v-alert>
          <div v-else-if="extractDialog.changes.length === 0" class="text-grey py-6 text-center">
            未检测到工作事项 / 文件任务 / 文件标识的结构改动。
          </div>
          <v-table v-else density="compact">
            <thead>
              <tr><th>类型</th><th>层级</th><th>位置</th><th>内容</th></tr>
            </thead>
            <tbody>
              <tr v-for="(c, i) in extractDialog.changes" :key="i">
                <td><v-chip size="x-small" :color="changeTypeColor(c.type)" variant="tonal">{{ changeTypeLabel(c.type) }}</v-chip></td>
                <td>{{ levelLabel(c.level) }}</td>
                <td class="text-caption text-medium-emphasis">
                  {{ c.stage }}<span v-if="c.task"> · {{ c.task }}</span>
                </td>
                <td>
                  <template v-if="c.type === 'renamed'">{{ c.from }} → {{ c.to }}</template>
                  <template v-else>{{ c.name }}</template>
                </td>
              </tr>
            </tbody>
          </v-table>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="extractDialog.busy" @click="extractDialog.open = false">取消</v-btn>
          <v-btn color="amber-darken-2" variant="elevated" :loading="extractDialog.busy" @click="confirmExtract">确认提取</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 承接对话框 -->
    <v-dialog v-model="acceptDialog.open" max-width="780" persistent>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-account-multiple-plus-outline</v-icon>
          分工：{{ acceptDialog.projectName }}
        </v-card-title>
        <v-card-text>
          <div class="text-body-2 text-medium-emphasis mb-3">
            模版已在上一步选定，为每个工作环节指派一名已注册的负责人。
          </div>
          <v-progress-linear v-if="acceptDialog.loadingTemplates" indeterminate color="primary" class="mb-3" />

          <div v-if="acceptDialog.templateKey" class="d-flex align-center mb-2 flex-wrap" style="gap:6px;">
            <span class="text-subtitle-2">工作环节负责人指派</span>
            <v-spacer />
            <v-btn size="small" variant="tonal" color="indigo" prepend-icon="mdi-account-plus-outline" @click="openTeamForAssign">团队核心人员调整</v-btn>
          </div>

          <!-- 批量指派（便捷）：勾选下方多个工作环节 → 选负责人 → 一键填入；逐项指派能力保留 -->
          <div v-if="acceptDialog.templateKey && acceptDialog.stages.length > 0" class="d-flex align-center mb-2 flex-wrap pa-2" style="gap:8px; background:rgba(0,0,0,.03); border-radius:6px;">
            <v-checkbox v-model="allStagesSelected" density="compact" hide-details label="全选" />
            <v-autocomplete
              v-model="acceptDialog.batchAssignee"
              :items="assigneeItems(acceptDialog.team)"
              item-title="label" item-value="username"
              label="批量指派负责人" density="compact" variant="outlined" hide-details clearable
              style="min-width:220px; max-width:320px;"
              :no-data-text="acceptDialog.team.length === 0 ? '团队暂无成员，请先「团队核心人员调整」' : '无匹配项'"
            />
            <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-account-check-outline"
              :disabled="!acceptDialog.batchAssignee || acceptDialog.batchSelected.length === 0"
              @click="applyBatchAssign">指派到勾选（{{ acceptDialog.batchSelected.length }}）</v-btn>
          </div>
          <v-progress-linear v-if="acceptDialog.loadingStages" indeterminate color="primary" class="mb-2" />
          <v-alert
            v-else-if="acceptDialog.templateKey && acceptDialog.stages.length === 0"
            type="warning"
            variant="tonal"
            density="compact"
          >
            该模板没有工作环节，无法承接。请换一个模板。
          </v-alert>

          <div v-for="(stage, idx) in acceptDialog.stages" :key="stage.key" class="mb-3 pb-2" style="border-bottom: 1px solid rgba(0,0,0,.06)">
            <v-row dense align="center" no-gutters>
              <v-col cols="1">
                <v-checkbox v-model="acceptDialog.batchSelected" :value="stage.key" density="compact" hide-details />
              </v-col>
              <v-col cols="5" class="pr-2">
                <!-- 分工阶段工作事项只读：环节名不可改、不可增删。
                     如需调整工作事项，请走「工作事项」按钮（项目专属模版编辑）。 -->
                <v-text-field
                  :model-value="stage.stage_name"
                  :label="`${idx + 1}. 工作环节名称`"
                  variant="outlined"
                  density="compact"
                  hide-details
                  readonly
                />
              </v-col>
              <v-col cols="6">
                <v-autocomplete
                  v-model="acceptDialog.assignments[stage.key]"
                  :items="assigneeItems(acceptDialog.team)"
                  item-title="label"
                  item-value="username"
                  label="环节负责人 *"
                  density="compact"
                  variant="outlined"
                  hide-details
                  clearable
                  :no-data-text="acceptDialog.team.length === 0 ? '团队暂无成员，请先「团队核心人员调整」' : '无匹配项'"
                />
              </v-col>
            </v-row>
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="acceptDialog.busy" @click="acceptDialog.open = false">取消</v-btn>
          <v-btn
            color="primary"
            variant="elevated"
            :loading="acceptDialog.busy"
            :disabled="!canSubmitAccept"
            @click="openAssignPreview"
          >
            提交
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 分工提交前预览 + 二次确认 -->
    <v-dialog v-model="assignPreview.open" max-width="520">
      <v-card>
        <v-card-title class="d-flex align-center"><v-icon class="mr-2" color="primary">mdi-clipboard-check-outline</v-icon>分工预览</v-card-title>
        <v-card-subtitle class="text-wrap">请确认以下工作环节的负责人指派；确认后将下发分工。</v-card-subtitle>
        <v-card-text style="max-height:55vh;">
          <v-table density="compact">
            <thead><tr><th>工作环节</th><th>负责人</th></tr></thead>
            <tbody>
              <tr v-for="(r, i) in assignPreview.rows" :key="i">
                <td>{{ r.stage }}</td>
                <td>{{ r.assignee }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="acceptDialog.busy" @click="assignPreview.open = false">返回修改</v-btn>
          <v-btn color="primary" variant="elevated" :loading="acceptDialog.busy" @click="confirmAccept">确认提交</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 第一步：选择模版对话框（完整立项书只读 + 关联模版下拉） -->
    <v-dialog v-model="selectDialog.open" max-width="640" persistent scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-file-document-outline</v-icon>
          关联模版：{{ selectDialog.projectName }}
        </v-card-title>
        <v-card-text style="max-height:70vh;">
          <div class="text-body-2 text-medium-emphasis mb-3">
            先了解本项目的立项书内容，再为其关联一个数据业务模版；确定后即可进入分工。
          </div>

          <!-- 立项书只读卡 -->
          <v-card variant="tonal" color="#1b3a5b" class="mb-4">
            <v-card-text class="py-3">
              <div class="text-subtitle-2 mb-2 d-flex align-center">
                <v-icon size="18" class="mr-1">mdi-clipboard-text-outline</v-icon>立项书
              </div>
              <v-row dense>
                <v-col cols="12" sm="6">
                  <div class="lxs-k">项目名称</div><div class="lxs-v">{{ selectDialog.projectName || '-' }}</div>
                </v-col>
                <v-col cols="12" sm="6">
                  <div class="lxs-k">立项编号</div><div class="lxs-v">{{ selectDialog.projectCode || '-' }}</div>
                </v-col>
                <v-col cols="12" sm="6">
                  <div class="lxs-k">项目负责人</div><div class="lxs-v">{{ selectDialog.ownerName || '-' }}</div>
                </v-col>
                <v-col cols="12" sm="6">
                  <div class="lxs-k">所属部门</div><div class="lxs-v">{{ selectDialog.department || '-' }}</div>
                </v-col>
                <v-col cols="12" sm="6">
                  <div class="lxs-k">敏感级别</div><div class="lxs-v">{{ sensLabel(selectDialog.sensitivity) || '-' }}</div>
                </v-col>
                <v-col cols="12" sm="6">
                  <div class="lxs-k">数据权属</div><div class="lxs-v">{{ selectDialog.dataOwner || '-' }}</div>
                </v-col>
                <v-col cols="12">
                  <div class="lxs-k">立项依据</div><div class="lxs-v lxs-pre">{{ selectDialog.approvalBasis || '—' }}</div>
                </v-col>
                <v-col cols="12">
                  <div class="lxs-k">项目简介</div><div class="lxs-v lxs-pre">{{ selectDialog.description || '—' }}</div>
                </v-col>
                <v-col cols="12">
                  <div class="lxs-k">立项时间</div><div class="lxs-v">{{ selectDialog.createTime || '-' }}</div>
                </v-col>
              </v-row>
            </v-card-text>
          </v-card>

          <v-autocomplete
            v-model="selectDialog.templateKey"
            :items="selectDialog.templates"
            item-title="label"
            item-value="value"
            label="关联模版 *（本地 + 在线）"
            density="compact"
            variant="outlined"
            :loading="selectDialog.loadingTemplates"
            :no-data-text="selectDialog.loadingTemplates ? '加载中…' : '暂无可用模板'"
            clearable
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="selectDialog.busy" @click="selectDialog.open = false">取消</v-btn>
          <v-btn
            color="primary"
            variant="elevated"
            :loading="selectDialog.busy"
            :disabled="!selectDialog.templateKey"
            @click="confirmTemplate"
          >
            确定
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 项目详情：每个环节的指派 + 工作进展 -->
    <v-dialog v-model="detailsDialog.open" max-width="860">
      <v-card v-if="detailsDialog.project">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="primary">mdi-clipboard-text-search-outline</v-icon>
          项目详情：{{ detailsDialog.project.project_name }}
        </v-card-title>
        <v-card-subtitle>
          项目负责人：{{ detailsDialog.project.owner_name }}
          · 承接时间：{{ formatTime(detailsDialog.project.accepted_at) }}
        </v-card-subtitle>
        <v-divider />
        <v-card-text>
          <div class="d-flex align-center mb-3">
            <span class="text-subtitle-2 mr-3">整体进度</span>
            <v-progress-linear :model-value="overallProgress" color="success" height="8" rounded class="flex-grow-1" />
            <span class="ml-3 text-body-2 text-medium-emphasis">{{ overallProgress }}%</span>
          </div>

          <v-progress-linear v-if="detailsDialog.loading" indeterminate color="primary" class="my-3" />

          <v-table v-else-if="detailsDialog.stages.length > 0" density="comfortable">
            <thead>
              <tr>
                <th>序</th>
                <th>工作事项</th>
                <th>责任人</th>
                <th>状态</th>
                <th>开始时间</th>
                <th>结束时间</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(s, idx) in detailsDialog.stages" :key="s.id">
                <td>{{ idx + 1 }}</td>
                <td>
                  <code class="text-body-2">{{ s.stage_code }}</code>
                  <span class="ml-1">{{ s.stage_name }}</span>
                </td>
                <td>{{ displayName(s.assignee_username) }}</td>
                <td>
                  <v-chip size="x-small" :color="stageStatusColor(s.status)" variant="tonal">
                    {{ stageStatusLabel(s.status) }}
                  </v-chip>
                </td>
                <td class="text-caption text-medium-emphasis">{{ formatTime(s.started_at) }}</td>
                <td class="text-caption text-medium-emphasis">{{ formatTime(s.completed_at) }}</td>
              </tr>
            </tbody>
          </v-table>

          <div v-else class="text-center py-6 text-grey">
            <v-icon size="48" color="grey-lighten-1">mdi-information-outline</v-icon>
            <div class="mt-2">没有环节指派记录</div>
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="detailsDialog.open = false">关闭</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 项目团队（项目级）：双栏选人 + 角色。项目负责人始终在队、不可移除。 -->
    <v-dialog v-model="teamDialog.open" max-width="900" persistent scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-account-group-outline</v-icon>
          选择核心成员
        </v-card-title>
        <v-card-subtitle class="text-wrap">
          立项时即已成队（含项目负责人本人）。这里把核心成员（环节责任人候选）拉进团队；可查看当前成员与角色（项目负责人 / 核心成员 / 项目参与人），此名单是分工时的人池。
        </v-card-subtitle>
        <v-card-text style="max-height:72vh;">
          <v-select
            v-model="teamDialog.applicationId"
            :items="eligibleTeamProjects"
            item-title="project_name"
            item-value="id"
            label="关联项目（已承接）"
            density="compact" variant="outlined" class="mb-3" hide-details
            :disabled="teamDialog.locked"
            @update:model-value="onTeamProjectChange"
          />
          <v-progress-linear v-if="teamDialog.loading" indeterminate color="primary" class="mb-2" />
          <TeamMemberPicker
            v-model="teamDialog.members"
            :users="ownerOptions"
            :locked-members="teamDialog.leadUsername ? [teamDialog.leadUsername] : []"
            :lead-user="teamDialog.leadUsername"
            lead-label="项目负责人"
            add-label="项目核心成员"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="teamDialog.busy" @click="teamDialog.open = false">取消</v-btn>
          <v-btn color="primary" variant="elevated" :loading="teamDialog.busy" :disabled="!teamDialog.applicationId" @click="saveTeam">保存团队</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 承接确认：先看立项书详情，再确认接下项目 -->
    <v-dialog v-model="acceptConfirmDialog.open" max-width="640" persistent scrollable>
      <v-card v-if="acceptConfirmDialog.project">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="success">mdi-handshake-outline</v-icon>
          承接项目：{{ acceptConfirmDialog.project.project_name }}
        </v-card-title>
        <v-card-text style="max-height:70vh;">
          <div class="text-body-2 text-medium-emphasis mb-3">
            请先确认立项书内容，承接后即成为项目负责人，可在「项目团队」继续拉人、关联模版、分工。
          </div>
          <v-card variant="tonal" color="#1b3a5b" class="mb-2">
            <v-card-text class="py-3">
              <div class="text-subtitle-2 mb-2 d-flex align-center">
                <v-icon size="18" class="mr-1">mdi-clipboard-text-outline</v-icon>立项书
              </div>
              <v-row dense>
                <v-col cols="12" sm="6"><div class="lxs-k">项目名称</div><div class="lxs-v">{{ acceptConfirmDialog.project.project_name || '-' }}</div></v-col>
                <v-col cols="12" sm="6"><div class="lxs-k">立项编号</div><div class="lxs-v">{{ acceptConfirmDialog.project.project_code || '-' }}</div></v-col>
                <v-col cols="12" sm="6"><div class="lxs-k">项目负责人</div><div class="lxs-v">{{ acceptConfirmDialog.project.owner_name || '-' }}</div></v-col>
                <v-col cols="12" sm="6"><div class="lxs-k">所属部门</div><div class="lxs-v">{{ acceptConfirmDialog.project.department || '-' }}</div></v-col>
                <v-col cols="12" sm="6"><div class="lxs-k">敏感级别</div><div class="lxs-v">{{ sensLabel(acceptConfirmDialog.project.sensitivity_level) }}</div></v-col>
                <v-col cols="12" sm="6"><div class="lxs-k">数据权属</div><div class="lxs-v">{{ acceptConfirmDialog.project.data_owner || '-' }}</div></v-col>
                <v-col cols="12"><div class="lxs-k">立项依据</div><div class="lxs-v lxs-pre">{{ acceptConfirmDialog.project.approval_basis || '—' }}</div></v-col>
                <v-col cols="12"><div class="lxs-k">项目简介</div><div class="lxs-v lxs-pre">{{ acceptConfirmDialog.project.description || '—' }}</div></v-col>
                <v-col cols="12"><div class="lxs-k">立项时间</div><div class="lxs-v">{{ formatTime(acceptConfirmDialog.project.create_time) }}</div></v-col>
              </v-row>
            </v-card-text>
          </v-card>

          <!-- 项目周期：负责人承接时填写（选填），承接后回显给立项人 -->
          <div class="text-subtitle-2 mb-1 d-flex align-center">
            <v-icon size="18" class="mr-1">mdi-calendar-range</v-icon>项目周期（选填）
          </div>
          <div class="text-caption text-medium-emphasis mb-2">填写本项目的计划起止日期，承接后会展示在立项人的项目列表中。</div>
          <v-row dense>
            <v-col cols="12" sm="6">
              <v-text-field v-model="acceptConfirmDialog.cycle_start" type="date" label="计划开始" variant="outlined" density="compact" hide-details :disabled="acceptConfirmDialog.busy" />
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field v-model="acceptConfirmDialog.cycle_end" type="date" label="计划结束" variant="outlined" density="compact" hide-details :disabled="acceptConfirmDialog.busy" />
            </v-col>
          </v-row>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="acceptConfirmDialog.busy" @click="acceptConfirmDialog.open = false">取消</v-btn>
          <v-btn color="success" variant="elevated" :loading="acceptConfirmDialog.busy" @click="acceptProject(acceptConfirmDialog.project)">确认承接</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <ProjectTemplateEditor v-model="stageEditor.open" :application-id="stageEditor.applicationId" mode="stage" @saved="onEditorSaved" />

    <!-- 结项弹窗：个人级/部门级仅确认；单位级可勾选定稿归卷到单位室（按密级落房间） -->
    <v-dialog v-model="closeDialog.open" max-width="640" scrollable>
      <v-card v-if="closeDialog.project">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="success">mdi-flag-checkered</v-icon>结项
        </v-card-title>
        <v-card-text>
          <div class="mb-3 text-body-2">
            确认对「<strong>{{ closeDialog.project.project_name }}</strong>」结项？结项后项目收尾、不可再交付。
          </div>

          <template v-if="closeDialog.isUnit">
            <v-divider class="mb-3" />
            <div class="text-subtitle-2 mb-1">归卷到单位室（可选）</div>
            <div class="text-caption text-medium-emphasis mb-2">
              勾选需要归卷的定稿，结项后将按项目密级移入单位室对应房间（一般→单位资料室 / 重要→单位档案室 / 核心→单位保密室），并从部门柜移出。不选则保留在部门柜。
            </div>
            <div v-if="closeDialog.loadingFiles" class="text-caption text-medium-emphasis py-3 d-flex align-center">
              <v-progress-circular indeterminate size="18" width="2" class="mr-2" />正在拉取部门柜定稿清单…
            </div>
            <div v-else-if="closeDialog.files.length === 0" class="text-caption text-grey py-3">
              部门柜暂无该项目的定稿文件（可能尚未一键归档），可直接结项。
            </div>
            <v-list v-else density="compact" class="py-0" style="border:1px solid rgba(0,0,0,.08); border-radius:6px; max-height:280px; overflow:auto;">
              <v-list-item v-for="f in closeDialog.files" :key="f.id" class="px-2">
                <template #prepend>
                  <v-checkbox-btn v-model="closeDialog.selected" :value="f.id" density="compact" />
                </template>
                <v-list-item-title class="text-body-2">{{ f.file_name }}</v-list-item-title>
                <v-list-item-subtitle class="text-caption">
                  {{ sensLabel(f.sensitivity_level) }} · 现在部门柜 → 归卷至 {{ unitRoomLabel(f.sensitivity_level) }}
                </v-list-item-subtitle>
              </v-list-item>
            </v-list>
          </template>

          <v-textarea
            v-model="closeDialog.summary" label="结项说明（选填）" :rows="2" density="compact"
            variant="outlined" hide-details class="mt-3" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="closeDialog.busy" @click="closeDialog.open = false">取消</v-btn>
          <v-btn color="success" variant="elevated" :loading="closeDialog.busy" @click="doClose">确认结项</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
      <span v-if="snackbar.html" v-html="snackbar.html"></span>
      <template v-else>{{ snackbar.text }}</template>
    </v-snackbar>
  </v-card>
</template>

<style scoped>
/* 立项书只读字段 */
.lxs-k { font-size: 12px; color: rgba(0, 0, 0, .55); margin-bottom: 1px; }
.lxs-v { font-size: 14px; color: rgba(0, 0, 0, .87); line-height: 1.5; margin-bottom: 6px; }
.lxs-pre { white-space: pre-wrap; word-break: break-word; }
</style>
