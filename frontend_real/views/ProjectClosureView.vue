<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { API_BASE } from '@/services/api'

interface MyProject {
  id: number
  project_name: string
  owner_name: string
  status: string
  template_code: string | null
  template_version: string | null
  accepted_at: string | null
  closed_at: string | null
  closed_by: string | null
  closure_summary: string | null
  reviewed_at: string | null
  reject_reason: string | null
  create_time: string
  initiation_done?: boolean // 立项过程是否结束（所有文件任务都已被受理）
}

interface StageRow {
  id: number
  stage_code: string
  stage_name: string
  assignee_username: string
  status: string
  sort_order: number
  started_at: string | null
  completed_at: string | null
}

interface ProjectWithProgress extends MyProject {
  stages: StageRow[]
  progress: number   // 0~100
  canClose: boolean
}

const tab = ref<'closable' | 'in_progress' | 'pending_review' | 'rejected' | 'closed'>('closable')
const loading = ref(false)
const items = ref<ProjectWithProgress[]>([])
const snackbar = ref({ show: false, text: '', color: 'success' })

const closeDialog = ref({
  open: false,
  busy: false,
  project: null as ProjectWithProgress | null,
  summary: '',
})

const detailsDialog = ref({
  open: false,
  project: null as ProjectWithProgress | null,
})

function classifyStatus(p: MyProject, stages: StageRow[]) {
  if (p.status === 'closed') return 'closed'
  if (p.status === 'rejected') return 'rejected'
  if (p.status === 'pending' || p.status === 'approved') return 'pending_review'
  // accepted
  const allDone = stages.length > 0 && stages.every(s => s.status === 'completed')
  return allDone ? 'closable' : 'in_progress'
}

function progressOf(stages: StageRow[]): number {
  if (stages.length === 0) return 0
  const done = stages.filter(s => s.status === 'completed').length
  return Math.round((done / stages.length) * 100)
}

async function load() {
  loading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/my-submissions`)
    const j = await r.json()
    if (!j.success) {
      snackbar.value = { show: true, text: '加载失败：' + (j.error || ''), color: 'error' }
      return
    }
    const list = (j.data || []) as MyProject[]
    // 对 accepted / closed 状态拉 stages 算进度；其它状态不需要
    const enriched: ProjectWithProgress[] = []
    for (const p of list) {
      let stages: StageRow[] = []
      if (p.status === 'accepted' || p.status === 'closed') {
        try {
          const sr = await fetch(`${API_BASE}/centralized-projects/${p.id}/stages`)
          const sj = await sr.json()
          if (sj.success && Array.isArray(sj.data)) {
            stages = (sj.data as StageRow[]).sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0))
          }
        } catch {}
      }
      const prog = progressOf(stages)
      enriched.push({
        ...p,
        stages,
        progress: prog,
        canClose: p.status === 'accepted' && stages.length > 0 && stages.every(s => s.status === 'completed'),
      })
    }
    items.value = enriched
  } finally {
    loading.value = false
  }
}

const buckets = computed(() => {
  const m: Record<string, ProjectWithProgress[]> = {
    closable: [], in_progress: [], pending_review: [], rejected: [], closed: [],
  }
  for (const p of items.value) {
    m[classifyStatus(p, p.stages)].push(p)
  }
  return m
})

const visibleItems = computed(() => buckets.value[tab.value])

const tabsConfig = [
  { value: 'closable',       label: '可结项',   icon: 'mdi-flag-checkered',     color: 'success' },
  { value: 'in_progress',    label: '进行中',   icon: 'mdi-progress-clock',     color: 'primary' },
  { value: 'pending_review', label: '审核中',   icon: 'mdi-clock-outline',      color: 'warning' },
  { value: 'closed',         label: '已结项',   icon: 'mdi-archive-check',      color: 'grey' },
  { value: 'rejected',       label: '已驳回',   icon: 'mdi-close-circle',       color: 'error' },
] as const

function openCloseDialog(p: ProjectWithProgress) {
  closeDialog.value = { open: true, busy: false, project: p, summary: '' }
}

async function confirmClose() {
  if (!closeDialog.value.project) return
  closeDialog.value.busy = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/${closeDialog.value.project.id}/close`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ closure_summary: closeDialog.value.summary.trim() }),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: '结项成功，项目已归档', color: 'success' }
      closeDialog.value.open = false
      await load()
    } else {
      snackbar.value = { show: true, text: '结项失败：' + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '结项失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    closeDialog.value.busy = false
  }
}

function openDetails(p: ProjectWithProgress) {
  detailsDialog.value = { open: true, project: p }
}

function statusLabel(s: string): string {
  return ({ pending: '待审核', approved: '已立项', taken: '已承接', accepted: '已分工', rejected: '已驳回', closed: '已结项' } as Record<string, string>)[s] || s
}

function stageStatusColor(s: string): string {
  return ({ pending: 'warning', in_progress: 'primary', completed: 'success' } as Record<string, string>)[s] || 'grey'
}

function stageStatusLabel(s: string): string {
  return ({ pending: '待开始', in_progress: '进行中', completed: '已完成' } as Record<string, string>)[s] || s
}

function formatTime(t: string | null): string {
  if (!t) return '-'
  return String(t).substring(0, 19).replace('T', ' ')
}

onMounted(load)
</script>

<template>
  <div>
    <v-card class="mb-3" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2" color="primary">mdi-archive-arrow-down-outline</v-icon>
        数据项目结项管理
        <v-spacer />
        <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-medium-emphasis">
          仅显示您提交立项的项目。所有工作环节完成后可由您（立项者）结项归档；
          结项后该项目状态变为「已结项」不可再修改。
        </div>
      </v-card-text>
    </v-card>

    <v-card elevation="1">
      <v-tabs v-model="tab" align-tabs="start">
        <v-tab v-for="t in tabsConfig" :key="t.value" :value="t.value">
          <v-icon :color="t.color" class="mr-1">{{ t.icon }}</v-icon>
          {{ t.label }}
          <v-chip size="x-small" variant="tonal" class="ml-2">{{ buckets[t.value].length }}</v-chip>
        </v-tab>
      </v-tabs>
      <v-divider />

      <v-list density="comfortable" v-if="visibleItems.length > 0">
        <v-list-item
          v-for="p in visibleItems"
          :key="p.id"
          class="py-2"
        >
          <div class="d-flex align-center w-100">
            <div class="flex-grow-1">
              <div class="d-flex align-center mb-1">
                <span class="font-weight-medium text-body-1">{{ p.project_name }}</span>
                <v-chip size="x-small" class="ml-2" color="grey" variant="tonal">
                  负责人 {{ p.owner_name }}
                </v-chip>
                <v-chip v-if="p.template_code" size="x-small" class="ml-1" variant="tonal">
                  {{ p.template_code }} {{ p.template_version }}
                </v-chip>
                <v-chip v-if="p.initiation_done" size="x-small" class="ml-1" color="teal" variant="flat" prepend-icon="mdi-flag-checkered">
                  立项已结束
                </v-chip>
              </div>
              <div class="d-flex align-center text-caption text-medium-emphasis" style="gap: 12px">
                <span>提交：{{ formatTime(p.create_time) }}</span>
                <span v-if="p.accepted_at">承接：{{ formatTime(p.accepted_at) }}</span>
                <span v-if="p.closed_at">结项：{{ formatTime(p.closed_at) }}</span>
              </div>
              <!-- 进度条（仅 accepted / closed 有 stages 数据） -->
              <div v-if="p.stages.length > 0" class="d-flex align-center mt-2">
                <v-progress-linear :model-value="p.progress" :color="p.canClose ? 'success' : 'primary'" height="6" rounded class="flex-grow-1" />
                <span class="ml-2 text-caption">
                  {{ p.stages.filter(s => s.status === 'completed').length }} / {{ p.stages.length }}
                </span>
              </div>
              <v-alert
                v-if="p.status === 'rejected' && p.reject_reason"
                type="error"
                density="compact"
                variant="tonal"
                class="mt-2"
              >驳回原因：{{ p.reject_reason }}</v-alert>
              <v-alert
                v-if="p.status === 'closed' && p.closure_summary"
                type="success"
                density="compact"
                variant="tonal"
                class="mt-2"
              >结项说明：{{ p.closure_summary }}</v-alert>
            </div>
            <div class="ml-3 d-flex" style="gap: 8px">
              <v-btn
                v-if="p.stages.length > 0"
                size="small"
                variant="text"
                prepend-icon="mdi-eye-outline"
                @click="openDetails(p)"
              >查看详情</v-btn>
              <v-btn
                v-if="p.canClose"
                size="small"
                color="success"
                variant="elevated"
                prepend-icon="mdi-flag-checkered"
                @click="openCloseDialog(p)"
              >结项</v-btn>
              <v-chip
                v-else-if="p.status === 'closed'"
                size="small"
                color="grey"
                variant="tonal"
                prepend-icon="mdi-archive-check"
              >已结项</v-chip>
            </div>
          </div>
        </v-list-item>
      </v-list>

      <div v-else class="text-center py-8">
        <v-icon size="56" color="grey-lighten-1">mdi-clipboard-outline</v-icon>
        <div class="mt-2 text-grey">本分组下暂无项目</div>
      </div>
    </v-card>

    <!-- 结项确认弹窗 -->
    <v-dialog v-model="closeDialog.open" max-width="560" persistent>
      <v-card v-if="closeDialog.project">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="success">mdi-flag-checkered</v-icon>
          结项确认
        </v-card-title>
        <v-card-text>
          <p class="mb-3">项目「<strong>{{ closeDialog.project.project_name }}</strong>」的全部工作环节已完成。结项后项目状态变更为「已结项」且不可逆，所有环节数据将保留备查。</p>
          <v-textarea
            v-model="closeDialog.summary"
            label="结项说明（可选）"
            placeholder="例如：所有交付物已归档，关键产出已上报单位档案室"
            variant="outlined"
            density="compact"
            rows="3"
            hide-details
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="closeDialog.busy" @click="closeDialog.open = false">取消</v-btn>
          <v-btn color="success" :loading="closeDialog.busy" @click="confirmClose">确认结项</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 详情弹窗 -->
    <v-dialog v-model="detailsDialog.open" max-width="860">
      <v-card v-if="detailsDialog.project">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="primary">mdi-clipboard-text-search-outline</v-icon>
          项目详情：{{ detailsDialog.project.project_name }}
        </v-card-title>
        <v-card-subtitle>
          状态：{{ statusLabel(detailsDialog.project.status) }}
          · 项目负责人：{{ detailsDialog.project.owner_name }}
          · 模板：{{ detailsDialog.project.template_code }} {{ detailsDialog.project.template_version }}
        </v-card-subtitle>
        <v-divider />
        <v-card-text>
          <v-table density="comfortable">
            <thead>
              <tr><th>序</th><th>工作环节</th><th>负责人</th><th>状态</th><th>开始</th><th>完成</th></tr>
            </thead>
            <tbody>
              <tr v-for="(s, idx) in detailsDialog.project.stages" :key="s.id">
                <td>{{ idx + 1 }}</td>
                <td><code>{{ s.stage_code }}</code> {{ s.stage_name }}</td>
                <td>{{ s.assignee_username }}</td>
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
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="detailsDialog.open = false">关闭</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </div>
</template>
