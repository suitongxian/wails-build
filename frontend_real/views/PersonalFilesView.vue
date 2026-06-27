<template>
  <div class="personal-ledger-page">
    <div class="d-flex align-center mb-4">
      <div>
        <div class="text-h6 d-flex align-center">
          <v-icon class="mr-2">mdi-account-file-text</v-icon>
          个人文件台账
        </div>
        <div class="text-body-2 text-medium-emphasis">
          个人工作文件先入账、分级、分主题；不默认迁移实体文件，正式项目归档另走立项环节。
        </div>
      </div>
      <v-spacer />
      <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="loadData">刷新</v-btn>
      <v-btn color="secondary" variant="tonal" prepend-icon="mdi-auto-fix" @click="goAIClassify">
        归目推荐
      </v-btn>
    </div>

    <v-alert
      v-if="missingBuckets.length > 0"
      type="warning"
      variant="tonal"
      density="compact"
      class="mb-4"
    >
      个人文件模版尚未完整初始化：{{ missingBuckets.map(item => item.title).join('、') }}。请确认已同步个人文件模版后重启终端。
    </v-alert>

    <v-row dense class="mb-5">
      <v-col v-for="stat in summaryStats" :key="stat.label" cols="6" md="2">
        <v-card variant="outlined" class="summary-card">
          <v-card-text>
            <div class="d-flex align-center justify-space-between mb-2">
              <v-icon :color="stat.color">{{ stat.icon }}</v-icon>
              <v-chip size="x-small" :color="stat.color" variant="tonal">{{ stat.scope }}</v-chip>
            </div>
            <div class="summary-value">{{ stat.value }}</div>
            <div class="summary-label">{{ stat.label }}</div>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>

    <!-- 2026-05-21 三级分流：需人工干预的红条提示 -->
    <v-alert
      v-if="needsActionTotal > 0"
      type="warning"
      variant="tonal"
      density="compact"
      class="mb-3"
      prepend-icon="mdi-alert"
    >
      <div class="d-flex align-center flex-wrap ga-3">
        <div v-if="memoPendingCount > 0">
          ⏳ 核心登记待办 <strong>{{ memoPendingCount }}</strong>
          <v-btn variant="text" size="x-small" color="primary" @click="$router.push('/memorandum')">→ 核心登记</v-btn>
        </div>
        <div v-if="importantUnsettledCount > 0">
          ⚖ 重要级多源待裁定 <strong>{{ importantUnsettledCount }}</strong>
          <v-btn variant="text" size="x-small" color="primary" @click="$router.push('/ai-classify')">→ 归目推荐</v-btn>
        </div>
      </div>
    </v-alert>

    <section class="mb-5">
      <div class="section-heading">
        <div>
          <div class="text-subtitle-1 d-flex align-center">
            <v-icon class="mr-2">mdi-shape-outline</v-icon>
            工作事项 / 主题
          </div>
          <div class="text-caption text-medium-emphasis">按文件主题聚合最近 30 日内有活动的工作事项；早于此窗口的旧主题不在此处展示。</div>
        </div>
        <v-chip size="small" variant="tonal" color="primary">{{ workItems.length }} 个</v-chip>
      </div>

      <v-row v-if="workItems.length > 0" dense>
        <v-col v-for="item in workItems" :key="item.name" cols="12" md="4">
          <v-card variant="outlined" class="topic-card">
            <v-card-title class="text-subtitle-2 d-flex align-center">
              <span
                v-if="item.latest && (item.latest as any).project_code"
                class="d-inline-block mr-2"
                :style="{ width: '10px', height: '10px', borderRadius: '50%', backgroundColor: tierDotColor((item.latest as any).project_code) }"
              />
              <v-icon size="small" class="mr-2">mdi-folder-text-outline</v-icon>
              <span class="text-truncate">{{ item.name }}</span>
            </v-card-title>
            <v-card-text class="pt-0">
              <div class="d-flex align-center ga-2 mb-2 flex-wrap">
                <v-chip size="x-small" color="primary" variant="tonal">{{ item.count }} 条</v-chip>
                <v-chip size="x-small" color="info" variant="tonal">{{ item.finalCount }} 定稿</v-chip>
                <v-chip size="x-small" color="grey" variant="tonal">{{ item.processCount }} 过程</v-chip>
              </div>
              <div class="text-caption text-medium-emphasis text-truncate">
                最近：{{ item.latest?.asset_name || '-' }}
              </div>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>
      <v-sheet v-else border rounded class="empty-state">
        近 30 日内暂无工作事项。新认领归目后会按文件主题汇总在此处。
      </v-sheet>
    </section>

    <section class="mb-5">
      <div class="section-heading">
        <div>
          <div class="text-subtitle-1 d-flex align-center">
            <v-icon class="mr-2">mdi-tune-variant</v-icon>
            级别分布
          </div>
          <div class="text-caption text-medium-emphasis">核心、重要、一般作为个人文件标识级别，用于筛选和统计。</div>
        </div>
        <v-btn size="small" variant="text" color="primary" @click="openAllLedgers">
          <v-icon>mdi-book-open-variant</v-icon>
          查看全部底账
        </v-btn>
      </div>

      <v-chip-group v-model="selectedLevel" mandatory class="mb-3">
        <v-chip value="all" filter variant="tonal">全部 {{ personalLedgers.length }}</v-chip>
        <v-chip
          v-for="bucket in buckets"
          :key="bucket.code"
          :value="bucket.code"
          filter
          variant="tonal"
          :color="bucket.color"
        >
          {{ bucket.title }} {{ bucket.ledgerCount }}
        </v-chip>
      </v-chip-group>

      <v-row dense>
        <v-col v-for="bucket in buckets" :key="bucket.code" cols="12" md="4">
          <v-card variant="outlined" class="level-card">
            <v-card-title class="d-flex align-center">
              <v-icon :color="bucket.color" class="mr-2">{{ bucket.icon }}</v-icon>
              {{ bucket.title }}
              <v-spacer />
              <v-chip :color="bucket.color" size="small" variant="tonal">{{ bucket.ledgerCount }} 条</v-chip>
            </v-card-title>
            <v-card-text>
              <div class="d-flex align-center">
                <div class="metric">
                  <div class="metric-value">{{ bucket.processCount }}</div>
                  <div class="metric-label">过程</div>
                </div>
                <v-divider vertical class="mx-3" />
                <div class="metric">
                  <div class="metric-value">{{ bucket.finalCount }}</div>
                  <div class="metric-label">定稿</div>
                </div>
                <v-divider vertical class="mx-3" />
                <div class="metric">
                  <div class="metric-value">{{ bucket.recentCount }}</div>
                  <div class="metric-label">近 7 日</div>
                </div>
              </div>
            </v-card-text>
            <v-card-actions>
              <!-- 2026-05-21 三级分流：bucket 卡片差异化 -->
              <template v-if="bucket.code === 'SYS-PERSONAL-CORE'">
                <v-spacer />
                <v-btn size="small" variant="tonal" color="error" @click="$router.push('/memorandum')">
                  <v-icon>mdi-shield-lock</v-icon>&nbsp;核心登记
                </v-btn>
              </template>
              <template v-else>
                <v-btn size="small" variant="text" @click="selectedLevel = bucket.code">
                  <v-icon>mdi-filter-outline</v-icon>
                  筛选
                </v-btn>
                <v-spacer />
                <v-btn size="small" variant="text" color="primary" @click="filterLedgers(bucket.code)">
                  <v-icon>mdi-book-open-variant</v-icon>
                  查看底账
                </v-btn>
                <v-btn
                  v-if="bucket.code === 'SYS-PERSONAL-IMPORTANT'"
                  size="small" variant="text" color="primary"
                  @click="$router.push('/ai-classify')"
                >→ 多源裁定</v-btn>
                <v-btn
                  v-if="bucket.code === 'SYS-PERSONAL-GENERAL'"
                  size="small" variant="text" color="primary"
                  @click="$router.push('/ai-classify')"
                >→ AI 一键</v-btn>
              </template>
            </v-card-actions>
          </v-card>
        </v-col>
      </v-row>
    </section>

    <section>
      <div class="section-heading">
        <div>
          <div class="text-subtitle-1 d-flex align-center">
            <v-icon class="mr-2">mdi-format-list-bulleted</v-icon>
            个人文件明细
          </div>
          <div class="text-caption text-medium-emphasis">只展示已挂账的个人工作文件记录。</div>
        </div>
        <v-chip size="small" variant="tonal" color="primary">{{ filteredPersonalLedgers.length }} 条</v-chip>
      </div>

      <v-data-table
        :headers="headers"
        :items="filteredPersonalLedgers"
        :loading="loading"
        density="compact"
        items-per-page="12"
      >
        <template #item.tier_state="{ item }">
          <template v-if="item.project_code === 'SYS-PERSONAL-CORE'">
            <v-chip v-if="(item as any).memorandum_registered_at" size="x-small" color="success" variant="tonal">
              已机要 {{ formatTierDate((item as any).memorandum_registered_at) }}
            </v-chip>
            <v-chip v-else size="x-small" color="error" variant="tonal">待登记</v-chip>
          </template>
          <template v-else-if="item.project_code === 'SYS-PERSONAL-IMPORTANT'">
            <v-chip size="x-small" color="warning" variant="tonal">重要</v-chip>
          </template>
          <template v-else>
            <v-chip size="x-small" variant="tonal">一般</v-chip>
          </template>
        </template>
        <template #item.project_code="{ item }">
          <v-chip :color="bucketColor(item.project_code)" size="x-small" variant="tonal">
            {{ bucketTitle(item.project_code) }}
          </v-chip>
        </template>
        <template #item.stage_code="{ item }">
          {{ stageLabel(item.stage_code) }}
        </template>
        <template #item.content_summary="{ item }">
          <span class="text-caption">{{ workItemLabel(item) }}</span>
        </template>
        <template #item.lifecycle_status="{ item }">
          <v-chip :color="statusColor(item.lifecycle_status)" size="x-small" variant="tonal">
            {{ statusLabel(item.lifecycle_status) }}
          </v-chip>
        </template>
        <template #item.open="{ item }">
          <v-btn size="x-small" variant="text" color="primary" prepend-icon="mdi-folder-open" @click="openLedgerFile(item.id)">
            打开
          </v-btn>
        </template>
        <template #item.create_time="{ item }">
          <span class="text-caption">{{ formatTime(item.create_time) }}</span>
        </template>
      </v-data-table>
    </section>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
      {{ snackbar.text }}
    </v-snackbar>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ledgersApi, projectsApi, type AssetLedger, type DataProject } from '@/services/projectsApi'
import {
  buildRecentWorkItems,
  isFinalLedger,
  isProcessLedger,
  workItemLabel,
} from '@/services/personalWorkItems'
import { API_BASE } from '@/services/api'

const WORK_ITEMS_WINDOW_DAYS = 30

// 2026-05-21 三级分流：需人工干预计数
const memoPendingCount = ref(0)
const importantUnsettledCount = ref(0)
const needsActionTotal = computed(() => memoPendingCount.value + importantUnsettledCount.value)

async function loadActionCounters() {
  try {
    const r = await fetch(`${API_BASE}/memorandum/pending?page=1&page_size=1`)
    const j = await r.json()
    if (j.success) memoPendingCount.value = j.data?.total || 0
  } catch {}
  try {
    const r = await fetch(`${API_BASE}/family/needs-arbitration`)
    const j = await r.json()
    if (j.success) importantUnsettledCount.value = j.data?.count || 0
  } catch {
    importantUnsettledCount.value = 0
  }
}

function tierDotColor(projectCode: string | undefined): string {
  if (projectCode === 'SYS-PERSONAL-CORE') return '#d32f2f'
  if (projectCode === 'SYS-PERSONAL-IMPORTANT') return '#f57c00'
  return '#43a047'
}

function formatTierDate(s: string | null | undefined): string {
  if (!s) return ''
  return String(s).substring(0, 10)
}

const router = useRouter()

const PERSONAL_BUCKETS = [
  {
    code: 'SYS-PERSONAL-CORE',
    title: '核心级',
    color: 'error',
    icon: 'mdi-lock',
  },
  {
    code: 'SYS-PERSONAL-IMPORTANT',
    title: '重要级',
    color: 'warning',
    icon: 'mdi-archive-outline',
  },
  {
    code: 'SYS-PERSONAL-GENERAL',
    title: '一般级',
    color: 'success',
    icon: 'mdi-folder-outline',
  },
]

const loading = ref(false)
const projects = ref<DataProject[]>([])
const ledgersByProject = ref<Record<string, AssetLedger[]>>({})
const selectedLevel = ref('all')
const snackbar = ref({ show: false, text: '', color: 'success' })

const personalLedgers = computed(() =>
  PERSONAL_BUCKETS
    .flatMap(bucket => activeLedgers(bucket.code))
    .sort((a, b) => new Date(b.create_time).getTime() - new Date(a.create_time).getTime())
)

const filteredPersonalLedgers = computed(() => {
  if (selectedLevel.value === 'all') return personalLedgers.value
  return personalLedgers.value.filter(item => item.project_code === selectedLevel.value)
})

const buckets = computed(() =>
  PERSONAL_BUCKETS.map(bucket => {
    const ledgers = activeLedgers(bucket.code)
    const projectList = Array.isArray(projects.value) ? projects.value : []
    const project = projectList.find(p => p.project_code === bucket.code) || null
    return {
      ...bucket,
      project,
      ledgerCount: ledgers.length,
      processCount: ledgers.filter(isProcessLedger).length,
      finalCount: ledgers.filter(isFinalLedger).length,
      recentCount: ledgers.filter(item => isRecent(item.create_time)).length,
    }
  })
)

const missingBuckets = computed(() => buckets.value.filter(bucket => !bucket.project))

const summaryStats = computed(() => [
  {
    label: '全部文件',
    value: personalLedgers.value.length,
    scope: '台账',
    icon: 'mdi-file-document-multiple-outline',
    color: 'primary',
  },
  {
    label: '工作事项',
    value: workItems.value.length,
    scope: '主题',
    icon: 'mdi-shape-outline',
    color: 'secondary',
  },
  {
    label: '过程稿',
    value: personalLedgers.value.filter(isProcessLedger).length,
    scope: '版本',
    icon: 'mdi-file-edit-outline',
    color: 'grey',
  },
  {
    label: '定稿',
    value: personalLedgers.value.filter(isFinalLedger).length,
    scope: '版本',
    icon: 'mdi-file-check-outline',
    color: 'info',
  },
  {
    label: '近 7 日',
    value: personalLedgers.value.filter(item => isRecent(item.create_time)).length,
    scope: '新增',
    icon: 'mdi-clock-outline',
    color: 'success',
  },
])

const workItems = computed(() =>
  buildRecentWorkItems(personalLedgers.value, { windowDays: WORK_ITEMS_WINDOW_DAYS, limit: 6 }),
)

function activeLedgers(projectCode: string): AssetLedger[] {
  const list = ledgersByProject.value[projectCode]
  // 防御：异步加载中途该键可能非数组，旧写法 (x || []) 不挡非数组值 → .filter 抛未处理异常。
  return (Array.isArray(list) ? list : []).filter(item => item.lifecycle_status !== 'planned')
}

const headers = [
  { title: '级别', key: 'project_code', width: 100 },
  { title: '级别状态', key: 'tier_state', width: 150, sortable: false },
  { title: '资产名称', key: 'asset_name' },
  { title: '工作事项 / 主题', key: 'content_summary', width: 180 },
  { title: '版本状态', key: 'stage_code', width: 120 },
  { title: '文件版本编码', key: 'file_version_code' },
  { title: '生命周期', key: 'lifecycle_status', width: 120 },
  { title: '挂账时间', key: 'create_time', width: 170 },
  { title: '操作', key: 'open', width: 90, sortable: false },
]

async function openLedgerFile(ledgerId: number) {
  try {
    const r = await fetch(`${API_BASE}/ledgers/${ledgerId}/open`, { method: 'POST' })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: '文件已在本机打开', color: 'success' }
    } else {
      snackbar.value = { show: true, text: '打开失败：' + (j.error || '未知错误'), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '打开失败：' + (e?.message || String(e)), color: 'error' }
  }
}

function isRecent(value: string): boolean {
  if (!value) return false
  const t = new Date(value).getTime()
  if (Number.isNaN(t)) return false
  return Date.now() - t <= 7 * 24 * 60 * 60 * 1000
}

function bucketMeta(code: string) {
  return PERSONAL_BUCKETS.find(item => item.code === code)
}

function bucketTitle(code: string): string {
  return bucketMeta(code)?.title || code
}

function bucketColor(code: string): string {
  return bucketMeta(code)?.color || 'default'
}

function stageLabel(code: string): string {
  if (code === 'GR-DRAFT') return '过程版本'
  if (code === 'GR-FINAL') return '标记定稿'
  if (code === 'GR-DA') return '归目记录'
  return code || '-'
}

function statusLabel(status: string): string {
  return ({
    planned: '待挂账',
    registered: '已挂账',
    in_use: '使用中',
    sealed: '已封存',
    destroyed: '已销账',
    permanent: '永存',
    cancelled: '已解绑',
  } as Record<string, string>)[status] || status
}

function statusColor(status: string): string {
  return ({
    planned: 'default',
    registered: 'success',
    in_use: 'primary',
    sealed: 'info',
    destroyed: 'error',
    permanent: 'purple',
    cancelled: 'grey',
  } as Record<string, string>)[status] || 'default'
}

function formatTime(value: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleString('zh-CN')
}

function goAIClassify() {
  router.push('/ai-classify')
}

function openAllLedgers() {
  router.push('/ledgers')
}

function filterLedgers(code: string) {
  router.push({ path: '/ledgers', query: { project_code: code } })
}

async function loadData() {
  loading.value = true
  try {
    const [projectList, ...ledgerGroups] = await Promise.all([
      projectsApi.list(),
      ...PERSONAL_BUCKETS.map(bucket => ledgersApi.search({ project_code: bucket.code })),
    ])
    projects.value = projectList
    ledgersByProject.value = PERSONAL_BUCKETS.reduce<Record<string, AssetLedger[]>>((acc, bucket, idx) => {
      acc[bucket.code] = ledgerGroups[idx] || []
      return acc
    }, {})
  } catch (e: any) {
    snackbar.value = { show: false, text: '', color: 'success' }
    snackbar.value = { show: true, text: '加载失败：' + (e?.message || e), color: 'error' }
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await loadData()
  await loadActionCounters()
})
</script>

<style scoped>
.personal-ledger-page {
  min-width: 0;
}

.section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}

.summary-card,
.level-card,
.topic-card {
  height: 100%;
  border-radius: 8px;
}

.summary-value {
  font-size: 1.65rem;
  line-height: 1.9rem;
  font-weight: 700;
}

.summary-label {
  font-size: 0.78rem;
  color: rgba(var(--v-theme-on-surface), 0.62);
}

.metric {
  min-width: 0;
  flex: 1;
  text-align: center;
}

.metric-value {
  font-size: 1.4rem;
  line-height: 1.7rem;
  font-weight: 700;
}

.metric-label {
  font-size: 0.75rem;
  color: rgba(var(--v-theme-on-surface), 0.62);
}

.empty-state {
  padding: 18px;
  color: rgba(var(--v-theme-on-surface), 0.62);
}

@media (max-width: 720px) {
  .section-heading {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
