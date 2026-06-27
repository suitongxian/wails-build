<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { API_BASE } from '@/services/api'

interface CentralizedProject {
  id: number
  project_name: string
  data_owner: string | null
  owner_name: string
  submitted_by: string
  status: string
  sensitivity_level: 'core' | 'important' | 'general'
  manage_remote_id: number | null
  sync_status: string
  sync_error: string | null
  reject_reason: string | null
  reviewed_at: string | null
  create_time: string
  project_code: string | null
  department: string | null
  approval_basis: string | null
  description: string | null
  project_scope?: string | null
  output_custody_scope?: string | null
  output_custody_note?: string | null
  cycle_start?: string | null   // 项目周期·计划起始（负责人承接时填，云端权威）
  cycle_end?: string | null     // 项目周期·计划结束
  completion_rate?: number | null // 整体完成率（0-100），无任务为 null
}

const loading = ref(false)
const refreshing = ref(false)
const items = ref<CentralizedProject[]>([])
const snackbar = ref({ show: false, text: '', color: 'success' })
// 只读查看：展示一条立项的全部信息
const viewDialog = ref<{ open: boolean; item: CentralizedProject | null }>({ open: false, item: null })
function viewItem(item: CentralizedProject) {
  viewDialog.value = { open: true, item }
}

interface OwnerOption {
  username: string
  display_name: string
  user_department: string
  label: string
}
const ownerOptions = ref<OwnerOption[]>([])

async function loadOwnerOptions() {
  try {
    // 用 /manage-users（带 role），并排除系统管理员——管理员不作为业务项目负责人候选。
    const r = await fetch(`${API_BASE}/manage-users`)
    const j = await r.json()
    if (j.success && Array.isArray(j.data)) {
      ownerOptions.value = j.data
        .filter((u: any) => u.role !== 'system_admin')
        .map((u: any) => ({
          username: u.username,
          display_name: u.display_name || u.username,
          user_department: u.user_department || '',
          label: `${u.display_name || u.username} (${u.username})`,
        }))
    }
  } catch {
    // ignore: 下拉为空，用户重试即可
  }
}

const dialog = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)

// 立项书抬头的拟制日期（展示用）
const today = (() => {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
})()

const blankForm = () => ({
  project_name: '', project_code: '', owner_name: '', department: '',
  sensitivity_level: 'general' as 'core' | 'important' | 'general',
  project_scope: 'unit' as 'person' | 'department' | 'unit',
  output_custody_scope: 'unit' as 'unit' | 'department', // 定稿保管层级（仅单位级可选）
  output_custody_note: '', // 归档归属说明（选填）
  data_owner: '', approval_basis: '', description: '',
})
const dform = ref(blankForm())

const sensitivityOptions = [
  { value: 'core',      label: '核心',  hint: '需重点保护、需登记 / 上报的高敏文件' },
  { value: 'important', label: '重要',  hint: '部门 / 单位内部权威源，多源时需裁定' },
  { value: 'general',   label: '一般',  hint: '低敏，可批量 AI 归目' },
]

// 项目层级：决定归档落「夹/柜/室」及本地还是上报云端。
const scopeOptions = [
  { value: 'person',     label: '个人级', hint: '本人工作项目，归档复制到本机个人夹' },
  { value: 'department', label: '部门级', hint: '部门项目，归档上报云端入部门柜' },
  { value: 'unit',       label: '单位级', hint: '单位项目，归档上报云端入单位室' },
]

function onOwnerSelected(username: string) {
  const o = ownerOptions.value.find(x => x.username === username)
  dform.value.department = o?.user_department || ''
}

const canPublish = computed(() =>
  dform.value.project_name.trim() !== '' &&
  dform.value.owner_name.trim() !== '' &&
  dform.value.department.trim() !== '' &&
  !!dform.value.sensitivity_level &&
  !!dform.value.project_scope)

const canDraft = computed(() => dform.value.project_name.trim() !== '')

function openCreate() {
  editingId.value = null
  dform.value = blankForm()
  dialog.value = true
}

function openEdit(it: CentralizedProject) {
  editingId.value = it.id
  dform.value = {
    project_name: it.project_name || '',
    project_code: it.project_code || '',
    owner_name: it.owner_name || '',
    department: it.department || '',
    sensitivity_level: (it.sensitivity_level || 'general') as 'core' | 'important' | 'general',
    project_scope: ((it as any).project_scope || 'unit') as 'person' | 'department' | 'unit',
    output_custody_scope: ((it as any).output_custody_scope || 'unit') as 'unit' | 'department',
    output_custody_note: (it as any).output_custody_note || '',
    data_owner: it.data_owner || '',
    approval_basis: it.approval_basis || '',
    description: it.description || '',
  }
  dialog.value = true
}

async function persist(asDraft: boolean) {
  if (asDraft ? !canDraft.value : !canPublish.value) return
  saving.value = true
  try {
    const payload: Record<string, unknown> = {
      project_name: dform.value.project_name.trim(),
      project_code: dform.value.project_code.trim(),
      owner_name: dform.value.owner_name.trim(),
      department: dform.value.department.trim(),
      data_owner: dform.value.data_owner.trim(),
      sensitivity_level: dform.value.sensitivity_level,
      project_scope: dform.value.project_scope,
      output_custody_scope: dform.value.project_scope === 'unit' ? dform.value.output_custody_scope : 'unit',
      output_custody_note: dform.value.project_scope === 'unit' ? dform.value.output_custody_note.trim() : '',
      approval_basis: dform.value.approval_basis.trim(),
      description: dform.value.description.trim(),
      save_as_draft: asDraft,
    }
    let url = `${API_BASE}/centralized-projects`
    let method = 'POST'
    if (editingId.value != null) {
      url = `${API_BASE}/centralized-projects/draft`
      method = 'PUT'
      payload.id = editingId.value
    }
    const r = await fetch(url, {
      method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
    })
    const j = await r.json()
    if (j.success) {
      dialog.value = false
      snackbar.value = { show: true, text: asDraft ? '已存草稿' : '已发布', color: 'success' }
      await load()
    } else {
      snackbar.value = { show: true, text: (asDraft ? '存草稿失败：' : '发布失败：') + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '操作失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    saving.value = false
  }
}

const headers = [
  { title: '编号', key: 'id', width: '80px' },
  { title: '项目名称', key: 'project_name' },
  { title: '敏感等级', key: 'sensitivity_level', width: '100px' },
  { title: '项目负责人', key: 'owner_name', width: '120px' },
  { title: '工程进展', key: 'status', width: '110px' },
  { title: '整体完成率', key: 'completion_rate', width: '140px' },
  { title: '立项日期', key: 'create_time', width: '170px' },
  { title: '操作', key: 'actions', width: '150px', sortable: false },
]

// 负责人列展示姓名而非 username（owner_name 存的是 username）
function ownerDisplay(username: string): string {
  const o = ownerOptions.value.find(x => x.username === username)
  return o ? o.display_name : username
}

function sensitivityLabel(s: string): string {
  return ({ core: '核心', important: '重要', general: '一般' } as Record<string, string>)[s] || s
}
function sensitivityColor(s: string): string {
  return ({ core: 'error', important: 'warning', general: 'grey' } as Record<string, string>)[s] || 'grey'
}
function scopeLabel(s: string | null | undefined): string {
  return ({ person: '个人', department: '部门', unit: '单位' } as Record<string, string>)[s || ''] || '—'
}

async function load() {
  loading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects?page=1&page_size=100`)
    const j = await r.json()
    if (j.success) {
      items.value = j.data?.items || []
    }
  } finally {
    loading.value = false
  }
}

// 结项已迁移到「项目工作分工」页（由项目负责人操作）；本「数据项目立项」页不再显示结项。

function formatTime(s: string): string {
  if (!s) return '-'
  const v = String(s).trim()
  // manage 端时间为 UTC（SQLite CURRENT_TIMESTAMP）；无时区标记按 UTC 解析并转中国时区展示。
  const hasTz = /[zZ]$/.test(v) || /[+-]\d{2}:?\d{2}$/.test(v)
  const d = new Date(v.replace(' ', 'T') + (hasTz ? '' : 'Z'))
  if (isNaN(d.getTime())) return v.substring(0, 19).replace('T', ' ')
  const p = new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai', hourCycle: 'h23',
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  }).formatToParts(d).reduce((m, x) => { m[x.type] = x.value; return m }, {} as Record<string, string>)
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second}`
}

function statusColor(s: string): string {
  return ({ draft: 'grey', pending: 'warning', approved: 'success', taken: 'info', assigning: 'teal', accepted: 'primary', rejected: 'error', closed: 'grey', cancelled: 'grey' } as Record<string, string>)[s] || 'grey'
}

// 工程进展：已立项→已承接→分工中→受理中→已结项
function statusLabel(s: string): string {
  return ({ draft: '草稿', pending: '处理中', approved: '已立项', taken: '已承接', assigning: '分工中', accepted: '受理中', rejected: '已驳回', closed: '已结项', cancelled: '已取消' } as Record<string, string>)[s] || s
}

// ── 立项人视角「查看」面板用的派生指标 ──
// 工程进展生命周期（步骤条）：已立项→已承接→分工中→受理中→已结项
const progressSteps = [
  { key: 'approved', label: '已立项' },
  { key: 'taken', label: '已承接' },
  { key: 'assigning', label: '分工中' },
  { key: 'accepted', label: '受理中' },
  { key: 'closed', label: '已结项' },
]
// 当前进展在步骤条中的序号；草稿/待审/驳回/取消 等非正常流返回 -1。
function statusStepIndex(s: string): number {
  return progressSteps.findIndex(x => x.key === s)
}
// 项目周期 + 进度提示（剩余/超期/今日到期），供立项人判断是否按期。
function cycleInfo(it: CentralizedProject): { range: string; sub: string; color: string } {
  const s = (it.cycle_start || '').trim()
  const e = (it.cycle_end || '').trim()
  if (!s && !e) return { range: '未设定', sub: '负责人承接时填写', color: 'grey' }
  // 只填了一端时不展示「?」：仅起始→「X 起」，仅结束→「截止 Y」；两端齐→「X ~ Y」。
  const range = s && e ? `${s} ~ ${e}` : (s ? `${s} 起` : `截止 ${e}`)
  if (it.status === 'closed') return { range, sub: '已结项', color: 'success' }
  if (!e) return { range, sub: '', color: 'info' }
  const end = new Date(e + 'T00:00:00')
  if (isNaN(end.getTime())) return { range, sub: '', color: 'info' }
  const now = new Date(); now.setHours(0, 0, 0, 0)
  const days = Math.round((end.getTime() - now.getTime()) / 86400000)
  if (days < 0) return { range, sub: `已超期 ${-days} 天`, color: 'error' }
  if (days === 0) return { range, sub: '今日到期', color: 'warning' }
  return { range, sub: `剩 ${days} 天`, color: days <= 7 ? 'warning' : 'success' }
}
// 整体完成率配色
function rateColor(r: number | null | undefined): string {
  if (r == null) return 'grey'
  if (r >= 100) return 'success'
  if (r >= 60) return 'primary'
  if (r >= 30) return 'info'
  return 'warning'
}

function syncStatusColor(s: string): string {
  return ({ synced: 'success', pending: 'warning', failed: 'error' } as Record<string, string>)[s] || 'grey'
}

function syncStatusLabel(s: string): string {
  return ({ synced: '已推送给项目负责人', pending: '待推送', failed: '推送失败' } as Record<string, string>)[s] || s
}

// 静默从 manage 拉一次审核结果（失败不打扰用户，等下次手动刷新或下次进页面再试）
async function silentRefreshFromManage(): Promise<number> {
  refreshing.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/refresh`, { method: 'POST' })
    const j = await r.json()
    if (j.success) return j.data?.updated ?? 0
    return 0
  } catch {
    return 0
  } finally {
    refreshing.value = false
  }
}

// 用户点「刷新」时：先拉 manage 审核结果 → 重新加载本地列表
async function refreshAll() {
  const updated = await silentRefreshFromManage()
  await load()
  if (updated > 0) {
    snackbar.value = { show: true, text: `已同步 ${updated} 条审核结果`, color: 'success' }
  }
}

onMounted(async () => {
  await loadOwnerOptions()
  await silentRefreshFromManage()
  await load()
})
</script>

<template>
  <div>
    <!-- 已提交的立项列表 -->
    <v-card elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-format-list-checks</v-icon>
        已提交的立项申请
        <v-spacer />
        <v-btn color="primary" variant="flat" prepend-icon="mdi-plus" class="mr-2" @click="openCreate">新建立项书</v-btn>
        <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading || refreshing" @click="refreshAll">刷新</v-btn>
      </v-card-title>
      <v-data-table
        :headers="headers"
        :items="items"
        :loading="loading"
        item-value="id"
        :items-per-page="50"
        hide-default-footer
      >
        <template #item.owner_name="{ item }">{{ ownerDisplay(item.owner_name) }}</template>
        <template #item.completion_rate="{ item }">
          <template v-if="item.completion_rate != null">
            <v-progress-linear :model-value="item.completion_rate" height="16" rounded color="primary" class="cp-rate">
              <span class="text-caption">{{ item.completion_rate }}%</span>
            </v-progress-linear>
          </template>
          <span v-else class="text-grey">—</span>
        </template>
        <template #item.sensitivity_level="{ item }">
          <v-chip size="x-small" :color="sensitivityColor(item.sensitivity_level)" variant="tonal">
            {{ sensitivityLabel(item.sensitivity_level) }}
          </v-chip>
        </template>
        <template #item.status="{ item }">
          <v-chip size="x-small" :color="statusColor(item.status)" variant="tonal">
            {{ statusLabel(item.status) }}
          </v-chip>
        </template>
        <template #item.create_time="{ item }">
          {{ formatTime(item.create_time) }}
        </template>
        <template #item.actions="{ item }">
          <v-btn size="small" variant="text" color="primary" prepend-icon="mdi-eye" @click="viewItem(item)">查看</v-btn>
          <v-btn v-if="item.status === 'draft'" size="small" variant="text" color="primary" prepend-icon="mdi-pencil" @click="openEdit(item)">编辑</v-btn>
        </template>
        <template v-slot:no-data>
          <div class="text-center py-8">
            <v-icon size="64" color="grey-lighten-1">mdi-clipboard-outline</v-icon>
            <div class="mt-4 text-grey">暂无集中立项申请</div>
          </div>
        </template>
      </v-data-table>
    </v-card>

    <!-- 查看：立项人视角的项目跟踪面板（重点看进展/完成率/周期/负责人，而非静态立项书） -->
    <v-dialog v-model="viewDialog.open" max-width="760" scrollable>
      <v-card v-if="viewDialog.item" class="pv">
        <!-- 抬头：项目名 + 编号 + 关键身份标签 -->
        <div class="pv-head">
          <div class="pv-head-main">
            <div class="pv-name">{{ viewDialog.item.project_name || '未命名项目' }}</div>
            <div class="pv-tags">
              <span class="pv-code">{{ viewDialog.item.project_code || '编号待生成' }}</span>
              <v-chip size="x-small" :color="sensitivityColor(viewDialog.item.sensitivity_level)" variant="flat">{{ sensitivityLabel(viewDialog.item.sensitivity_level) }}</v-chip>
              <v-chip size="x-small" color="blue-grey" variant="flat">{{ scopeLabel(viewDialog.item.project_scope) }}级</v-chip>
            </div>
          </div>
          <v-btn icon="mdi-close" variant="text" size="small" @click="viewDialog.open = false" />
        </div>

        <v-card-text class="pv-body" style="max-height:66vh;">
          <!-- 驳回提示（仅驳回时） -->
          <v-alert v-if="viewDialog.item.status === 'rejected'" type="error" variant="tonal" density="compact" class="mb-4">
            立项被驳回：{{ viewDialog.item.reject_reason || '未填写原因' }}
          </v-alert>
          <!-- 草稿提示 -->
          <v-alert v-else-if="viewDialog.item.status === 'draft'" type="info" variant="tonal" density="compact" class="mb-4">
            该立项仍是草稿，尚未发布给项目负责人。
          </v-alert>

          <!-- 工程进展步骤条 -->
          <div v-if="statusStepIndex(viewDialog.item.status) >= 0" class="pv-steps">
            <template v-for="(st, i) in progressSteps" :key="st.key">
              <div class="pv-step" :class="{ done: i < statusStepIndex(viewDialog.item.status), cur: i === statusStepIndex(viewDialog.item.status) }">
                <div class="pv-dot">
                  <v-icon v-if="i < statusStepIndex(viewDialog.item.status)" size="14">mdi-check</v-icon>
                  <span v-else>{{ i + 1 }}</span>
                </div>
                <div class="pv-step-label">{{ st.label }}</div>
              </div>
              <div v-if="i < progressSteps.length - 1" class="pv-line" :class="{ done: i < statusStepIndex(viewDialog.item.status) }" />
            </template>
          </div>

          <!-- 关键指标卡片 -->
          <div class="pv-metrics">
            <!-- 工程进展 -->
            <div class="pv-metric">
              <div class="pv-m-k">工程进展</div>
              <v-chip :color="statusColor(viewDialog.item.status)" variant="flat" size="small" class="mt-1">{{ statusLabel(viewDialog.item.status) }}</v-chip>
            </div>
            <!-- 整体完成率 -->
            <div class="pv-metric">
              <div class="pv-m-k">整体完成率</div>
              <div class="d-flex align-center" style="gap:10px;">
                <v-progress-circular :model-value="viewDialog.item.completion_rate ?? 0" :color="rateColor(viewDialog.item.completion_rate)" :size="44" :width="5">
                  <span class="pv-rate-num">{{ viewDialog.item.completion_rate != null ? viewDialog.item.completion_rate : '—' }}</span>
                </v-progress-circular>
                <span class="pv-m-sub">{{ viewDialog.item.completion_rate != null ? '已完成占比' : '暂无文件任务' }}</span>
              </div>
            </div>
            <!-- 项目周期 -->
            <div class="pv-metric">
              <div class="pv-m-k">项目周期</div>
              <div class="pv-m-v">{{ cycleInfo(viewDialog.item).range }}</div>
              <div v-if="cycleInfo(viewDialog.item).sub" class="pv-m-sub" :class="`text-${cycleInfo(viewDialog.item).color}`">{{ cycleInfo(viewDialog.item).sub }}</div>
            </div>
            <!-- 项目负责人 -->
            <div class="pv-metric">
              <div class="pv-m-k">项目负责人</div>
              <div class="pv-m-v">{{ ownerDisplay(viewDialog.item.owner_name) || '—' }}</div>
              <div class="pv-m-sub">{{ viewDialog.item.department || '所属部门未填' }}</div>
            </div>
          </div>

          <!-- 送达 / 立项信息 -->
          <div class="pv-info">
            <div class="pv-info-row">
              <span class="pv-i-k">推送状态</span>
              <span class="pv-i-v">
                <template v-if="viewDialog.item.status === 'draft'">—</template>
                <v-chip v-else size="x-small" :color="syncStatusColor(viewDialog.item.sync_status)" variant="tonal">{{ syncStatusLabel(viewDialog.item.sync_status) }}</v-chip>
              </span>
            </div>
            <div v-if="viewDialog.item.sync_error" class="pv-info-row">
              <span class="pv-i-k">推送错误</span><span class="pv-i-v text-error">{{ viewDialog.item.sync_error }}</span>
            </div>
            <div class="pv-info-row"><span class="pv-i-k">数据权属</span><span class="pv-i-v">{{ viewDialog.item.data_owner || '—' }}</span></div>
            <div class="pv-info-row"><span class="pv-i-k">立项日期</span><span class="pv-i-v">{{ formatTime(viewDialog.item.create_time) }}</span></div>
          </div>

          <!-- 立项依据 / 简介（折叠次要信息） -->
          <div class="pv-sec">
            <div class="pv-sec-h">立项依据</div>
            <div class="pv-para">{{ viewDialog.item.approval_basis || '—' }}</div>
          </div>
          <div class="pv-sec">
            <div class="pv-sec-h">项目简介</div>
            <div class="pv-para">{{ viewDialog.item.description || '—' }}</div>
          </div>
        </v-card-text>

        <div class="pv-foot">
          <v-spacer />
          <v-btn variant="flat" class="lxs-btn-pub" @click="viewDialog.open = false">关闭</v-btn>
        </div>
      </v-card>
    </v-dialog>

    <!-- 新建/编辑立项书（公文式） -->
    <v-dialog v-model="dialog" max-width="780" persistent scrollable>
      <v-card class="lxs">
        <!-- 文档抬头 -->
        <div class="lxs-head">
          <div class="lxs-title">项 目 立 项 书</div>
          <div class="lxs-sub">
            PROJECT&nbsp;INITIATION&nbsp;FORM
            <v-chip v-if="editingId != null" size="x-small" color="warning" variant="flat" class="ml-2">草稿</v-chip>
          </div>
        </div>
        <div class="lxs-meta">
          <span>编号：{{ dform.project_code || '立项后由系统自动生成' }}</span>
          <span>拟制日期：{{ today }}</span>
        </div>

        <v-card-text class="lxs-body" style="max-height:62vh;">
          <section class="lxs-sec">
            <h4 class="lxs-sec-h">一、基本信息</h4>
            <v-row>
              <v-col cols="12" md="6">
                <v-text-field v-model="dform.project_name" label="项目名称 *" variant="outlined" density="compact" hide-details="auto" :disabled="saving" />
              </v-col>
              <v-col cols="12" md="6">
                <v-text-field
                  :model-value="dform.project_code || '立项发布后由系统自动生成唯一编号'"
                  label="立项编号" variant="outlined" density="compact" hide-details="auto"
                  readonly disabled
                />
              </v-col>
            </v-row>
          </section>

          <section class="lxs-sec">
            <h4 class="lxs-sec-h">二、责任主体</h4>
            <v-row>
              <v-col cols="12" md="6">
                <v-autocomplete
                  v-model="dform.owner_name"
                  :items="ownerOptions"
                  item-title="label"
                  item-value="username"
                  label="项目负责人 *"
                  variant="outlined" density="compact" clearable hide-details="auto" :disabled="saving"
                  :no-data-text="ownerOptions.length === 0 ? '加载中或暂无可选用户' : '无匹配项'"
                  @update:model-value="onOwnerSelected"
                />
              </v-col>
              <v-col cols="12" md="6">
                <v-text-field v-model="dform.department" label="所属部门 *（选负责人后自动带出）" variant="outlined" density="compact" hide-details="auto" readonly />
              </v-col>
            </v-row>
          </section>

          <section class="lxs-sec">
            <h4 class="lxs-sec-h">三、安全定级</h4>
            <v-row>
              <v-col cols="12" md="6">
                <div class="lxs-flabel">敏感级别 <span class="lxs-req">*</span><span style="font-weight:400;color:#94a3b8;font-size:11px;">（决定归档落 保密/档案/资料）</span></div>
                <v-radio-group v-model="dform.sensitivity_level" inline density="compact" hide-details color="#1b3a5b">
                  <v-radio v-for="o in sensitivityOptions" :key="o.value" :label="o.label" :value="o.value" />
                </v-radio-group>
              </v-col>
              <v-col cols="12" md="6">
                <div class="lxs-flabel">项目层级 <span class="lxs-req">*</span><span style="font-weight:400;color:#94a3b8;font-size:11px;">（决定归档落 夹/柜/室·本地或上云）</span></div>
                <v-radio-group v-model="dform.project_scope" inline density="compact" hide-details color="#1b3a5b">
                  <v-radio v-for="o in scopeOptions" :key="o.value" :label="o.label" :value="o.value" />
                </v-radio-group>
              </v-col>
            </v-row>
            <!-- 「项目过程文件管理模式」已移到负责人「承接」环节选择（仅单位级项目可选） -->
            <v-row>
              <v-col cols="12" md="6">
                <v-text-field v-model="dform.data_owner" label="数据权属（选填）" variant="outlined" density="compact" hide-details="auto" :disabled="saving" />
              </v-col>
            </v-row>
          </section>

          <section class="lxs-sec">
            <h4 class="lxs-sec-h">四、立项依据</h4>
            <v-textarea v-model="dform.approval_basis" label="立项依据（选填）" :rows="3" variant="outlined" density="compact" hide-details="auto" :disabled="saving" />
          </section>

          <section class="lxs-sec">
            <h4 class="lxs-sec-h">五、项目简介</h4>
            <v-textarea v-model="dform.description" label="项目简介（选填）" :rows="3" variant="outlined" density="compact" hide-details="auto" :disabled="saving" />
          </section>
        </v-card-text>

        <div class="lxs-foot">
          <v-spacer />
          <v-btn variant="text" :disabled="saving" @click="dialog = false">取消</v-btn>
          <v-btn variant="outlined" class="lxs-btn-draft" :loading="saving" :disabled="!canDraft" @click="persist(true)">存草稿</v-btn>
          <v-btn variant="flat" class="lxs-btn-pub" :loading="saving" :disabled="!canPublish" @click="persist(false)">发布</v-btn>
        </div>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </div>
</template>

<style scoped>
/* 公文式立项书弹窗 */
.lxs-head{ text-align:center; padding:22px 32px 14px; border-bottom:2px solid #1b3a5b; }
.lxs-title{ font-size:24px; font-weight:800; letter-spacing:10px; color:#1b3a5b; line-height:1.3; }
.lxs-sub{ font-size:11px; letter-spacing:3px; color:#9aa4b2; margin-top:6px; }
.lxs-meta{ display:flex; justify-content:space-between; font-size:12px; color:#6b7686; padding:9px 32px; border-bottom:1px dashed #e3e8ef; }
.lxs-body{ padding:6px 32px 4px !important; }
.lxs-sec{ padding:14px 0 4px; border-bottom:1px solid #f0f3f7; }
.lxs-sec:last-child{ border-bottom:none; }
.lxs-sec-h{ display:flex; align-items:center; gap:9px; font-size:14px; font-weight:700; color:#1b3a5b; margin:0 0 12px; }
.lxs-sec-h::before{ content:""; width:4px; height:15px; background:#1b3a5b; border-radius:2px; }
.lxs-flabel{ font-size:12.5px; color:#6b7686; font-weight:600; margin-bottom:2px; }
.lxs-req{ color:#d4351c; font-weight:700; }
/* 缩小敏感级别单选框的选择圈 */
.lxs :deep(.v-radio .v-selection-control__input){ width:26px; height:26px; }
.lxs :deep(.v-radio .v-selection-control__input > .v-icon){ font-size:18px; }
.lxs-foot{ display:flex; align-items:center; gap:10px; padding:14px 24px; border-top:1px solid #e3e8ef; background:#eef3f8; }
.lxs-btn-pub{ background:#1b3a5b !important; color:#fff !important; }
.lxs-btn-draft{ color:#1b3a5b !important; border-color:#1b3a5b !important; }
/* 公文式查看（只读）：以纯文本字段呈现，不出现任何输入框 */
.lxs-grid{ display:grid; grid-template-columns:1fr 1fr; gap:10px 22px; }
.lxs-field{ display:flex; flex-direction:column; gap:2px; padding:6px 0; border-bottom:1px dashed #eef2f7; }
.lxs-k{ font-size:12px; color:#94a3b8; }
.lxs-v{ font-size:14px; color:#1f2937; font-weight:600; white-space:pre-wrap; word-break:break-all; }
.lxs-para{ font-size:14px; color:#1f2937; line-height:1.7; white-space:pre-wrap; padding:4px 2px; }
.cp-rate{ min-width:90px; }

/* 立项人视角项目跟踪面板 */
.pv-head{ display:flex; align-items:flex-start; gap:12px; padding:18px 22px 14px; border-bottom:1px solid #eef2f7; background:linear-gradient(180deg,#f7fafd,#fff); }
.pv-head-main{ flex:1; min-width:0; }
.pv-name{ font-size:19px; font-weight:800; color:#1b3a5b; line-height:1.35; word-break:break-all; }
.pv-tags{ display:flex; align-items:center; gap:8px; margin-top:8px; flex-wrap:wrap; }
.pv-code{ font-size:12px; color:#6b7686; font-family:ui-monospace,Menlo,monospace; }
.pv-body{ padding:18px 22px 8px !important; }
/* 步骤条 */
.pv-steps{ display:flex; align-items:center; margin:2px 2px 20px; }
.pv-step{ display:flex; flex-direction:column; align-items:center; gap:6px; flex:0 0 auto; }
.pv-dot{ width:26px; height:26px; border-radius:50%; display:flex; align-items:center; justify-content:center; font-size:12px; font-weight:700; background:#e7ecf3; color:#9aa7b8; transition:.2s; }
.pv-step.done .pv-dot{ background:#5b8def; color:#fff; }
.pv-step.cur .pv-dot{ background:#1b3a5b; color:#fff; box-shadow:0 0 0 4px rgba(27,58,91,.14); }
.pv-step-label{ font-size:11.5px; color:#94a3b8; white-space:nowrap; }
.pv-step.done .pv-step-label, .pv-step.cur .pv-step-label{ color:#1b3a5b; font-weight:700; }
.pv-line{ flex:1; height:2px; background:#e7ecf3; margin:0 4px; margin-bottom:22px; }
.pv-line.done{ background:#5b8def; }
/* 指标卡 */
.pv-metrics{ display:grid; grid-template-columns:1fr 1fr; gap:12px; }
.pv-metric{ border:1px solid #eef2f7; border-radius:10px; padding:12px 14px; background:#fbfdff; }
.pv-m-k{ font-size:12px; color:#94a3b8; font-weight:600; margin-bottom:4px; }
.pv-m-v{ font-size:15px; color:#1f2937; font-weight:700; word-break:break-all; }
.pv-m-sub{ font-size:11.5px; color:#94a3b8; margin-top:2px; }
.pv-rate-num{ font-size:13px; font-weight:800; }
/* 信息行 */
.pv-info{ margin-top:16px; border-top:1px dashed #e3e8ef; padding-top:12px; }
.pv-info-row{ display:flex; align-items:center; gap:10px; padding:5px 0; }
.pv-i-k{ flex:0 0 80px; font-size:12.5px; color:#94a3b8; }
.pv-i-v{ font-size:13.5px; color:#1f2937; font-weight:600; }
/* 次要段落 */
.pv-sec{ margin-top:14px; }
.pv-sec-h{ font-size:13px; font-weight:700; color:#1b3a5b; margin-bottom:5px; }
.pv-para{ font-size:13.5px; color:#374151; line-height:1.7; white-space:pre-wrap; }
.pv-foot{ display:flex; align-items:center; padding:12px 20px; border-top:1px solid #e3e8ef; background:#eef3f8; }
@media (max-width:560px){ .pv-metrics{ grid-template-columns:1fr; } }
</style>
