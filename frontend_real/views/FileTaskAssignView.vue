<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { API_BASE } from '@/services/api'
import TeamMemberPicker from '@/components/TeamMemberPicker.vue'
import ProjectTemplateEditor from '@/components/ProjectTemplateEditor.vue'

interface MyStage {
  id: number
  application_id: number
  stage_code: string
  stage_name: string
  assignee_username: string
  status: string
  project_name: string
  project_code?: string | null // 项目编号（分组头展示）
  create_time?: string // 指派时间（环节分工记录创建时间）
  team_count?: number // 环节团队人数（manage 带回）；0 表示尚未组队，不能分工
  assigned_count?: number // 本环节已分工（文件任务指派）数；0 表示尚未分工
}
interface TaskRow { task_code: string; task_name: string; assignee_username: string | null }
interface OwnerOption {
  username: string
  display_name: string
  user_unit?: string | null
  user_department?: string | null
  role?: string | null
  label: string
}

// 环节团队成员（manage 带回角色）：stage_lead 环节负责人 / participant 参与人
interface TeamMemberFull {
  username: string
  display_name: string | null
  roles?: string[]
}
const STAGE_ROLE_LABEL: Record<string, string> = { stage_lead: '环节负责人', participant: '参与人' }
function stageRoleLabels(roles: string[] | undefined): string[] {
  return (roles || []).map(r => STAGE_ROLE_LABEL[r] || r)
}

const loading = ref(false)
const items = ref<MyStage[]>([])
const snackbar = ref({ show: false, text: '', color: 'success' })
const ownerOptions = ref<OwnerOption[]>([])

const assignDialog = ref({
  open: false, busy: false, applicationId: 0, stageCode: '', stageName: '', projectName: '',
  loading: false, tasks: [] as TaskRow[],
  stage: null as MyStage | null,
  team: [] as string[], // 本环节团队成员 username（指派时唯一人池）
  batchAssignee: '' as string,    // 批量指派：目标参与人
  batchSelected: [] as string[],  // 批量指派：勾选的文件任务 task_code
})

// ── 工作团队（环节级）：环节被指派时即成队（含环节负责人），可继续拉人后再指派文件任务 ──
const teamDialog = ref({
  open: false, busy: false, loading: false,
  locked: false, // true=从分工弹窗「临时拉人入队」打开，环节固定、保存后回填分工人池
  stage: null as MyStage | null,
  members: [] as string[],
  roleMap: {} as Record<string, string[]>, // username → 已本地化角色文案
  leadUsername: '', // 环节负责人 username（锁定不可移除）
})
const canFormStageTeam = computed(() => items.value.length > 0)

// 拉取环节团队（带角色）。环节负责人始终在列（manage 端保证）。
async function loadStageTeam(applicationId: number, stageCode: string): Promise<TeamMemberFull[]> {
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/stage-team?application_id=${applicationId}&stage_code=${encodeURIComponent(stageCode)}`)
    const j = await r.json()
    if (j.success && Array.isArray(j.data)) return j.data as TeamMemberFull[]
  } catch {}
  return []
}
function buildRoleMap(members: TeamMemberFull[]): Record<string, string[]> {
  const m: Record<string, string[]> = {}
  for (const x of members) m[x.username] = stageRoleLabels(x.roles)
  return m
}
function leadOf(members: TeamMemberFull[]): string {
  return members.find(x => (x.roles || []).includes('stage_lead'))?.username || ''
}
// 应用环节团队。兜底：无论后端返回什么，都用环节负责人(assignee_username)补齐——
// 环节被指派即成队，环节负责人必在队、置顶、带「环节负责人」标签且锁定。
function applyTeam(members: TeamMemberFull[], stageLead?: string) {
  let list = members || []
  const lead = (stageLead || '').trim()
  if (lead && !list.some(m => m.username === lead)) {
    list = [{ username: lead, display_name: null, roles: ['stage_lead'] }, ...list]
  }
  teamDialog.value.members = list.map(m => m.username)
  teamDialog.value.roleMap = buildRoleMap(list)
  teamDialog.value.leadUsername = leadOf(list) || lead
}

async function openTeamDialog() {
  const first = items.value[0] || null
  teamDialog.value = { open: true, busy: false, loading: !!first, locked: false, stage: first, members: [], roleMap: {}, leadUsername: '' }
  if (first) applyTeam(await loadStageTeam(first.application_id, first.stage_code), first.assignee_username)
  teamDialog.value.loading = false
}

// 从分工弹窗内打开「临时拉人入队」：环节固定为正在分工的环节，保存后回填团队人池
async function openTeamForAssign() {
  const d = assignDialog.value
  if (!d.stage) return
  teamDialog.value = { open: true, busy: false, loading: true, locked: true, stage: d.stage, members: [], roleMap: {}, leadUsername: '' }
  applyTeam(await loadStageTeam(d.stage.application_id, d.stage.stage_code), d.stage.assignee_username)
  teamDialog.value.loading = false
}

// 行内「工作团队」：查看/扩充指定环节的团队（指派文件任务的前置）。环节固定。
async function openTeamForStage(it: MyStage) {
  if (rowLocked(it)) { snackbar.value = { show: true, text: '该环节已分工或已完成，不可再调整团队', color: 'warning' }; return }
  teamDialog.value = { open: true, busy: false, loading: true, locked: true, stage: it, members: [], roleMap: {}, leadUsername: '' }
  applyTeam(await loadStageTeam(it.application_id, it.stage_code), it.assignee_username)
  teamDialog.value.loading = false
}

async function onTeamStageChange() {
  const s = teamDialog.value.stage
  if (!s) { teamDialog.value.members = []; teamDialog.value.roleMap = {}; teamDialog.value.leadUsername = ''; return }
  teamDialog.value.loading = true
  applyTeam(await loadStageTeam(s.application_id, s.stage_code), s.assignee_username)
  teamDialog.value.loading = false
}

async function saveStageTeam() {
  const d = teamDialog.value
  if (!d.stage) { snackbar.value = { show: true, text: '请先选择工作环节', color: 'warning' }; return }
  d.busy = true
  try {
    const members = d.members.map(u => {
      const o = ownerOptions.value.find(x => x.username === u)
      return { username: u, display_name: o?.display_name || u }
    })
    const r = await fetch(`${API_BASE}/centralized-projects/stage-team?application_id=${d.stage.application_id}&stage_code=${encodeURIComponent(d.stage.stage_code)}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ members }),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: `环节团队已保存（${members.length} 人）`, color: 'success' }
      d.open = false
      // 若分工弹窗正开着同一环节，回填团队人池，临时拉的人立即可选
      if (assignDialog.value.open && d.stage
        && assignDialog.value.applicationId === d.stage.application_id
        && assignDialog.value.stageCode === d.stage.stage_code) {
        assignDialog.value.team = d.members.slice()
      }
      // 刷新环节列表，回填团队人数等
      await load()
    }
    else snackbar.value = { show: true, text: '保存环节团队失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '保存环节团队失败：' + (e?.message || String(e)), color: 'error' }
  } finally { d.busy = false }
}

// 指派候选：只从本环节团队中选人（团队 = 实际班底）。
// 人不够时用对话框内的「临时拉人入队」补员（永久入队）。
function assigneeItems(team: string[]): OwnerOption[] {
  const teamSet = new Set(team)
  return ownerOptions.value
    .filter(o => teamSet.has(o.username))
    .map(o => ({ ...o, label: `${o.display_name} (${o.username})${o.user_department ? ' · ' + o.user_department : ''}` }))
}

async function loadOwnerOptions() {
  try {
    const r = await fetch(`${API_BASE}/manage-users`)
    const j = await r.json()
    if (j.success && Array.isArray(j.data)) {
      ownerOptions.value = j.data.filter((u: any) => u.role !== 'system_admin')
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
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/my-stages`)
    const j = await r.json()
    if (j.success) items.value = (j.data || []) as MyStage[]
  } finally { loading.value = false }
}

// ── 文件任务（项目专属模版，task 级，聚焦本环节）：增改文件任务的唯一入口 ──
const taskEditor = ref({ open: false, applicationId: 0, stageCode: '', stageName: '' })
function openTaskEditor(it: MyStage) {
  if (rowLocked(it)) { snackbar.value = { show: true, text: '该环节已分工或已完成，文件任务不可再编辑', color: 'warning' }; return }
  taskEditor.value = { open: true, applicationId: it.application_id, stageCode: it.stage_code, stageName: it.stage_name }
}
// 模版编辑保存后：刷新环节列表；若分工弹窗开着，重载本环节文件任务（保留已选指派人）。
async function onTaskEditorSaved() {
  await load()
  const d = assignDialog.value
  if (d.open && d.applicationId && d.stageCode) {
    const prev: Record<string, string> = {}
    for (const t of d.tasks) if (t.assignee_username) prev[t.task_code] = t.assignee_username
    try {
      const r = await fetch(`${API_BASE}/centralized-projects/stage-tasks?application_id=${d.applicationId}&stage_code=${encodeURIComponent(d.stageCode)}`)
      const j = await r.json()
      const tasks = (j.success ? (j.data || []) : []) as TaskRow[]
      for (const t of tasks) if (prev[t.task_code]) t.assignee_username = prev[t.task_code]
      assignDialog.value.tasks = tasks
    } catch { /* 忽略，仅影响即时刷新 */ }
  }
}

async function openAssign(it: MyStage) {
  if (rowLocked(it)) { snackbar.value = { show: true, text: '该环节已分工或已完成，不可再分工', color: 'warning' }; return }
  assignDialog.value = { open: true, busy: false, applicationId: it.application_id, stageCode: it.stage_code, stageName: it.stage_name, projectName: it.project_name, loading: true, tasks: [], stage: it, team: [], batchAssignee: '', batchSelected: [] }
  // 载入本环节团队（指派时置顶候选）
  loadStageTeam(it.application_id, it.stage_code).then(t => { assignDialog.value.team = t.map(m => m.username) })
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/stage-tasks?application_id=${it.application_id}&stage_code=${encodeURIComponent(it.stage_code)}`)
    const j = await r.json()
    assignDialog.value.tasks = j.success ? (j.data || []) as TaskRow[] : []
    if (assignDialog.value.tasks.length === 0) snackbar.value = { show: true, text: '该环节暂无文件任务', color: 'warning' }
  } finally { assignDialog.value.loading = false }
}

// ── 批量指派（便捷能力，不替代逐项指派）：勾选多个文件任务 → 选一名参与人 → 一键填入 ──
const allTasksSelected = computed({
  get: () => {
    const d = assignDialog.value
    return d.tasks.length > 0 && d.batchSelected.length === d.tasks.length
  },
  set: (v: boolean) => {
    assignDialog.value.batchSelected = v ? assignDialog.value.tasks.map(t => t.task_code) : []
  },
})
function applyBatchAssignTasks() {
  const d = assignDialog.value
  const who = (d.batchAssignee || '').trim()
  if (!who || d.batchSelected.length === 0) return
  for (const t of d.tasks) if (d.batchSelected.includes(t.task_code)) t.assignee_username = who
  const n = d.batchSelected.length
  d.batchSelected = []
  d.batchAssignee = ''
  snackbar.value = { show: true, text: `已批量指派 ${n} 个文件任务`, color: 'success' }
}

// username → 用户名称（人池查不到回退登录名）。
function displayName(username: string | null | undefined): string {
  const u = (username || '').trim()
  if (!u) return ''
  return ownerOptions.value.find(x => x.username === u)?.display_name || u
}

// 提交前预览二次确认：列出每个文件任务的指派人。
const assignPreview = ref({ open: false, rows: [] as Array<{ task: string; assignee: string }> })
function openAssignPreview() {
  const d = assignDialog.value
  if (d.tasks.length === 0) return
  assignPreview.value = {
    open: true,
    rows: d.tasks.map(t => ({ task: t.task_name, assignee: t.assignee_username ? displayName(t.assignee_username) : '（未指派）' })),
  }
}

// 「暂存」：占位按钮，功能暂未开放（仅展示）。
function stashAssign() {
  snackbar.value = { show: true, text: '暂存功能即将开放', color: 'info' }
}

async function submitAssign() {
  const d = assignDialog.value
  d.busy = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/assign-tasks?application_id=${d.applicationId}&stage_code=${encodeURIComponent(d.stageCode)}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ assignments: d.tasks.map(t => ({ task_code: t.task_code, task_name: t.task_name, assignee_username: t.assignee_username || '' })) }),
    })
    const j = await r.json()
    if (j.success) { assignPreview.value.open = false; d.open = false; snackbar.value = { show: true, text: '已提交分工', color: 'success' }; await load() }
    else snackbar.value = { show: true, text: '分工失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '分工失败：' + (e?.message || String(e)), color: 'error' }
  } finally { d.busy = false }
}

function statusLabel(s: string): string {
  return ({ pending: '待开始', in_progress: '进行中', completed: '已完成' } as Record<string, string>)[s] || s
}

// 环节已完成（所有文件任务已交付）→ 锁定：不可再编辑文件任务、不可再分工。
function stageLocked(it: MyStage): boolean {
  return (it?.status || '') === 'completed'
}

// 指派时间展示：manage 存的是 "YYYY-MM-DD HH:MM:SS"（UTC），截到分钟即可；空值显示 —。
function fmtTime(s?: string): string {
  if (!s) return '—'
  const v = String(s).trim()
  // manage 端时间为 UTC（SQLite CURRENT_TIMESTAMP）；无时区标记按 UTC 解析并转中国时区（截到分钟）。
  const hasTz = /[zZ]$/.test(v) || /[+-]\d{2}:?\d{2}$/.test(v)
  const d = new Date(v.replace(' ', 'T') + (hasTz ? '' : 'Z'))
  if (isNaN(d.getTime())) {
    const m = v.replace('T', ' ').match(/^(\d{4}-\d{2}-\d{2} \d{2}:\d{2})/)
    return m ? m[1] : v
  }
  const p = new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai', hourCycle: 'h23',
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  }).formatToParts(d).reduce((m, x) => { m[x.type] = x.value; return m }, {} as Record<string, string>)
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}`
}

// 「项目」不再单列展示——按项目分组到折叠分组头里。
const headers = [
  { title: '工作事项', key: 'stage_name' },
  { title: '派发时间', key: 'create_time', width: '170px' },
  { title: '状态', key: 'status', width: '110px' },
  { title: '操作', key: 'actions', width: '220px', sortable: false },
]

// ── 三 tab：按「是否已分工 + 完成情况」分桶 ──
//   已结束 = 全部完成(completed)
//   实施中 = 已分工(有文件任务指派)但未全部完成
//   待指派 = 尚未分工（无文件任务指派）
const tab = ref<'pending' | 'in_progress' | 'completed'>('pending')
const isAssigned = (s: MyStage) => (s.assigned_count ?? 0) > 0
// 行锁定：已分工（提交过分工）或已完成 → 工作团队/文件任务/分工 三个按钮均不可再点。
function rowLocked(it: MyStage): boolean {
  return stageLocked(it) || isAssigned(it)
}
const pendingStages = computed(() => items.value.filter(s => s.status !== 'completed' && !isAssigned(s)))
const inProgressStages = computed(() => items.value.filter(s => s.status !== 'completed' && isAssigned(s)))
const completedStages = computed(() => items.value.filter(s => s.status === 'completed'))
const tabItems = computed(() => (
  tab.value === 'in_progress' ? inProgressStages.value
    : tab.value === 'completed' ? completedStages.value
      : pendingStages.value
))

// 同一项目的工作事项分组（保持出现顺序），可按项目折叠
const groupedTabItems = computed(() => {
  const map = new Map<string, MyStage[]>()
  for (const s of tabItems.value) {
    const k = s.project_name || '未命名项目'
    if (!map.has(k)) map.set(k, [])
    map.get(k)!.push(s)
  }
  return Array.from(map, ([project, stages]) => ({ project, code: (stages[0]?.project_code || '').trim(), stages }))
})
const collapsedProjects = ref<Record<string, boolean>>({})
const isCollapsed = (project: string) => !!collapsedProjects.value[project]
const toggleProject = (project: string) => { collapsedProjects.value[project] = !collapsedProjects.value[project] }

onMounted(async () => { await loadOwnerOptions(); await load() })
</script>

<template>
  <div>
    <v-card elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-account-multiple-plus</v-icon>
        任务指派
        <v-spacer />
        <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
      </v-card-title>
      <v-tabs v-model="tab" color="primary" class="mb-3">
        <v-tab value="pending"><span class="dot dot-todo mr-2"></span>待指派<span class="tab-count">{{ pendingStages.length }}</span></v-tab>
        <v-tab value="in_progress"><span class="dot dot-doing mr-2"></span>实施中<span class="tab-count">{{ inProgressStages.length }}</span></v-tab>
        <v-tab value="completed"><span class="dot dot-done mr-2"></span>已结束<span class="tab-count">{{ completedStages.length }}</span></v-tab>
      </v-tabs>
      <div v-if="tabItems.length === 0" class="text-center py-8 text-grey">暂无指派给我的工作环节</div>
      <!-- 同一项目的工作事项分组展示，可按项目折叠 -->
      <template v-for="g in groupedTabItems" :key="g.project">
        <div class="grp-header d-flex align-center px-3 py-2" @click="toggleProject(g.project)">
          <v-icon size="small" class="mr-1">{{ isCollapsed(g.project) ? 'mdi-chevron-right' : 'mdi-chevron-down' }}</v-icon>
          <v-icon size="small" class="mr-1" color="indigo">mdi-folder-outline</v-icon>
          <span class="font-weight-medium">{{ g.project }}</span>
          <span v-if="g.code" class="text-caption text-medium-emphasis ml-2">编号：{{ g.code }}</span>
          <v-chip size="x-small" class="ml-2" variant="tonal" color="indigo">{{ g.stages.length }} 个工作事项</v-chip>
        </div>
        <v-data-table v-show="!isCollapsed(g.project)" :headers="headers" :items="g.stages" :loading="loading"
          item-value="id" :items-per-page="-1" hide-default-footer density="comfortable">
          <template #item.create_time="{ item }">
            <span class="text-caption">{{ fmtTime(item.create_time) }}</span>
          </template>
          <template #item.status="{ item }">
            <v-chip size="x-small" variant="tonal">{{ statusLabel(item.status) }}</v-chip>
          </template>
          <template #item.actions="{ item }">
            <div class="d-flex align-center" style="gap: 6px;">
              <v-btn size="small" color="indigo" variant="tonal" prepend-icon="mdi-account-group-outline" :disabled="rowLocked(item)" @click="openTeamForStage(item)">工作团队</v-btn>
              <v-btn size="small" color="indigo" variant="text" prepend-icon="mdi-file-tree-outline" :disabled="rowLocked(item)" @click="openTaskEditor(item)">文件任务</v-btn>
              <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-account-multiple-plus" :disabled="rowLocked(item)" @click="openAssign(item)">分工</v-btn>
            </div>
          </template>
        </v-data-table>
      </template>
    </v-card>

    <v-dialog v-model="assignDialog.open" max-width="640" persistent scrollable>
      <v-card>
        <v-card-title>文件任务分工 · {{ assignDialog.stageName }}</v-card-title>
        <v-card-subtitle>{{ assignDialog.projectName }}</v-card-subtitle>
        <v-card-text style="max-height:60vh;">
          <div class="d-flex align-center mb-2">
            <span class="text-caption text-medium-emphasis">本环节文件任务（如需增改文件任务，请用「文件任务」按钮）</span>
          </div>
          <v-progress-linear v-if="assignDialog.loading" indeterminate class="mb-3" />
          <template v-else>
            <div v-if="assignDialog.tasks.length === 0" class="text-grey py-4 text-center">该环节暂无文件任务</div>
            <div v-else class="d-flex align-center mb-2">
              <span class="text-caption text-medium-emphasis">仅可从本环节团队（{{ assignDialog.team.length }} 人）中选择</span>
              <v-spacer />
              <v-btn size="small" variant="tonal" color="indigo" prepend-icon="mdi-account-plus-outline" @click="openTeamForAssign">团队参与人员调整</v-btn>
            </div>

            <!-- 批量指派（便捷）：勾选下方多个文件任务 → 选参与人 → 一键填入；逐项指派能力保留 -->
            <div v-if="assignDialog.tasks.length > 0" class="d-flex align-center mb-3 flex-wrap pa-2" style="gap:8px; background:rgba(0,0,0,.03); border-radius:6px;">
              <v-checkbox v-model="allTasksSelected" density="compact" hide-details label="全选" />
              <v-autocomplete v-model="assignDialog.batchAssignee" :items="assigneeItems(assignDialog.team)" item-title="label" item-value="username"
                label="批量指派参与人" density="compact" variant="outlined" hide-details clearable style="min-width:200px; max-width:300px;"
                :no-data-text="assignDialog.team.length === 0 ? '团队暂无成员，请先「团队参与人员调整」' : '无匹配项'" />
              <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-account-check-outline"
                :disabled="!assignDialog.batchAssignee || assignDialog.batchSelected.length === 0"
                @click="applyBatchAssignTasks">指派到勾选（{{ assignDialog.batchSelected.length }}）</v-btn>
            </div>

            <v-row v-for="(t, idx) in assignDialog.tasks" :key="idx" class="align-center" no-gutters>
              <v-col cols="1">
                <v-checkbox v-model="assignDialog.batchSelected" :value="t.task_code" density="compact" hide-details />
              </v-col>
              <v-col cols="5" class="pr-2">
                <div class="text-body-2">{{ t.task_name }}</div>
                <div class="text-caption text-grey">{{ t.task_code }}</div>
              </v-col>
              <v-col cols="6">
                <v-autocomplete v-model="t.assignee_username" :items="assigneeItems(assignDialog.team)" item-title="label" item-value="username"
                  label="工作参与人" variant="outlined" density="compact" clearable hide-details="auto" :disabled="assignDialog.busy"
                  :no-data-text="assignDialog.team.length === 0 ? '团队暂无成员，请先「团队参与人员调整」' : '无匹配项'" />
              </v-col>
            </v-row>
          </template>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="assignDialog.busy" @click="assignDialog.open = false">取消</v-btn>
          <v-btn variant="tonal" color="grey-darken-1" :disabled="assignDialog.busy" @click="stashAssign">暂存</v-btn>
          <v-btn color="primary" variant="flat" :loading="assignDialog.busy" :disabled="assignDialog.tasks.length === 0" @click="openAssignPreview">确认指派</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 分工提交前预览 + 二次确认 -->
    <v-dialog v-model="assignPreview.open" max-width="520">
      <v-card>
        <v-card-title class="d-flex align-center"><v-icon class="mr-2" color="primary">mdi-clipboard-check-outline</v-icon>分工预览</v-card-title>
        <v-card-subtitle class="text-wrap">请确认以下文件任务的指派；确认后将下发分工。</v-card-subtitle>
        <v-card-text style="max-height:55vh;">
          <v-table density="compact">
            <thead><tr><th>文件任务</th><th>指派给</th></tr></thead>
            <tbody>
              <tr v-for="(r, i) in assignPreview.rows" :key="i">
                <td>{{ r.task }}</td>
                <td :class="r.assignee === '（未指派）' ? 'text-medium-emphasis' : ''">{{ r.assignee }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="assignDialog.busy" @click="assignPreview.open = false">返回修改</v-btn>
          <v-btn color="primary" variant="elevated" :loading="assignDialog.busy" @click="submitAssign">确认提交</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 工作团队（环节级）：双栏选人 + 角色。环节负责人始终在队、不可移除。 -->
    <v-dialog v-model="teamDialog.open" max-width="900" persistent scrollable>
      <v-card>
        <v-card-title class="d-flex align-center"><v-icon class="mr-2">mdi-account-group-outline</v-icon>选择项目参与成员</v-card-title>
        <v-card-subtitle class="text-wrap">
          你被指派为环节负责人时该环节即已成队（含你本人）。这里把项目参与成员拉进团队；可查看成员与角色（环节负责人 / 参与人），此名单是指派文件任务时的人池。
        </v-card-subtitle>
        <v-card-text style="max-height:72vh;">
          <v-select
            v-model="teamDialog.stage"
            :items="items"
            :item-title="(s: any) => `${s.project_name} · ${s.stage_name}`"
            return-object
            label="工作环节"
            density="compact" variant="outlined" class="mb-3" hide-details
            :disabled="teamDialog.locked"
            @update:model-value="onTeamStageChange"
          />
          <v-progress-linear v-if="teamDialog.loading" indeterminate color="primary" class="mb-2" />
          <TeamMemberPicker
            v-model="teamDialog.members"
            :users="ownerOptions"
            :locked-members="teamDialog.leadUsername ? [teamDialog.leadUsername] : []"
            :lead-user="teamDialog.leadUsername"
            lead-label="项目事项责任人"
            add-label="项目工作人员"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="teamDialog.busy" @click="teamDialog.open = false">取消</v-btn>
          <v-btn color="primary" variant="elevated" :loading="teamDialog.busy" :disabled="!teamDialog.stage" @click="saveStageTeam">保存团队</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <ProjectTemplateEditor v-model="taskEditor.open" :application-id="taskEditor.applicationId" mode="task"
      :stage-code="taskEditor.stageCode" :stage-name="taskEditor.stageName" @saved="onTaskEditorSaved" />

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </div>
</template>

<style scoped>
/* tab 样式对齐「工作受理」页：彩色圆点 + 药丸计数 */
.dot { display: inline-block; width: 9px; height: 9px; border-radius: 50%; }
.dot-todo { background: #2f6feb; } .dot-doing { background: #e8910c; } .dot-done { background: #15a05a; }
.tab-count { margin-left: 7px; background: #f0f2f4; border: 1px solid #e6e8eb; border-radius: 999px; font-size: 12px; color: #646a73; padding: 1px 9px; }
/* 项目分组头：浅底色、可点击折叠 */
.grp-header { background: #f6f8fb; border-top: 1px solid #eceef1; cursor: pointer; user-select: none; }
.grp-header:hover { background: #eef2f7; }
</style>
