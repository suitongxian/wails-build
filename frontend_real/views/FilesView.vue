<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type FileItem, type SystemConfig, type ScanProgressEvent, type FileStatisticsComparison, type StatisticsGrowth, type ResourcesStatistics } from '@/services/api'
import { saveTabState, loadTabState } from '@/services/TabStateManager'
import { Doughnut, Bar } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
  ArcElement,
} from 'chart.js'

// 注册 Chart.js 组件（方案A 概览图谱：分级环形图 + 范围对比柱状图）
ChartJS.register(CategoryScale, LinearScale, BarElement, Title, Tooltip, Legend, ArcElement)

const route = useRoute()
const router = useRouter()

// 选项卡类型
type TabType = 'workspace' | 'new_access' | 'history_inventory'

// 状态
const loading = ref(false)
const files = ref<FileItem[]>([])
const config = ref<SystemConfig | null>(null)
const search = ref('')
const survivalFilter = ref<'all' | 'new' | 'deleted' | 'normal'>('all')
const activeTab = ref<TabType>('workspace')

// 分页状态
const page = ref(1)
const pageSize = ref(20)
const totalItems = ref(0)

// 计算总页数
const totalPages = computed(() => {
  return Math.ceil(totalItems.value / pageSize.value)
})

// 扫描相关状态
const isScanning = ref(false)
const scanProgress = ref<ScanProgressEvent | null>(null)
const scanError = ref<string | null>(null)
// 当前轮询的扫描任务及定时器
let activeTaskId = 0
let taskPollTimer: number | null = null
const POLL_INTERVAL_MS = 2000

// 对话框状态
const showFirstScanDialog = ref(false)
const showCopiesDialog = ref(false)
const workspaceInput = ref('')
const selectedCopies = ref<FileItem[]>([])

// 详情侧边栏状态
const showDetailDrawer = ref(false)
const selectedFile = ref<FileItem | null>(null)

// 文件统计对比数据
const statisticsComparison = ref<FileStatisticsComparison | null>(null)

// 概览图谱（方案A）：全机分级 / 范围 / 治理统计
const resourcesStatistics = ref<ResourcesStatistics | null>(null)

// 概览 KPI 卡片（全机口径，不随 tab 变化）
const overviewKpis = computed(() => {
  const s = resourcesStatistics.value
  const c = statisticsComparison.value
  // 总数增长率：用历史+非历史的本次/上次合计推算
  let totalRate = 0
  if (c) {
    const last = (c.historyStatistics?.lastCount || 0) + (c.nonHistoryStatistics?.lastCount || 0)
    const cur = (c.historyStatistics?.currentCount || 0) + (c.nonHistoryStatistics?.currentCount || 0)
    if (last > 0) totalRate = Math.round(((cur - last) / last) * 1000) / 10
  }
  // 未做首次普查时，后端把 history/nonHistory 相关计数置为哨兵 -1（表示不适用），
  // 直接相加会得到 -2。求和时把负值当 0。
  const nz = (n?: number) => Math.max(0, n || 0)
  const pending = s
    ? nz(s.workspacePendingClassifyCount) + nz(s.historyPendingClassifyCount) + nz(s.nonHistoryPendingClassifyCount)
    : 0
  return {
    total: s?.totalFileCount ?? 0,
    totalRate,
    workspace: s?.workspaceTotalCount ?? 0,
    pending,
    unclassified: s?.unclassifiedCount ?? 0,
  }
})

// 分级分布环形图数据（核心/重要/开放/隐私/未分类）
const gradeChartData = computed(() => {
  const s = resourcesStatistics.value
  return {
    labels: ['核心', '重要', '开放', '隐私', '未分类'],
    datasets: [{
      data: [s?.coreCount ?? 0, s?.importantCount ?? 0, s?.openCount ?? 0, s?.privacyCount ?? 0, s?.unclassifiedCount ?? 0],
      backgroundColor: ['#d32f2f', '#f57c00', '#1976d2', '#7b1fa2', '#9e9e9e'],
      borderColor: '#fff',
      borderWidth: 2,
    }],
  }
})
const gradeChartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  cutout: '60%',
  plugins: { legend: { position: 'right' as const, labels: { boxWidth: 12, padding: 10 } } },
}

// 范围对比柱状图数据（工作空间/新增登记/历史治理，本次 vs 上次）
const scopeChartData = computed(() => {
  const c = statisticsComparison.value
  const last = [c?.workspaceStatistics?.lastCount ?? 0, c?.nonHistoryStatistics?.lastCount ?? 0, c?.historyStatistics?.lastCount ?? 0]
  const cur = [c?.workspaceStatistics?.currentCount ?? 0, c?.nonHistoryStatistics?.currentCount ?? 0, c?.historyStatistics?.currentCount ?? 0]
  return {
    labels: ['工作空间', '新增登记', '历史治理'],
    datasets: [
      { label: '上次', data: last, backgroundColor: '#bcd4f0' },
      { label: '本次', data: cur, backgroundColor: '#1976d2' },
    ],
  }
})
const scopeChartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  scales: { y: { beginAtZero: true } },
  plugins: { legend: { position: 'top' as const, labels: { boxWidth: 12 } } },
}

// 表格列配置
const headers = [
  { title: '文件名', key: 'path', sortable: true },
  { title: '标签', key: 'status', sortable: false, width: '80px' },
  { title: '创建时间', key: 'file_create_time', sortable: true, width: '180px' },
  { title: '最后修改时间', key: 'file_update_time', sortable: true, width: '180px' },
]

// 从文件路径中提取文件名
const getFileName = (path: string) => {
  return path.split('/').pop() || path
}

// 格式化时间
const formatDate = (dateStr: string | null) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// 格式化时间为 yyyy-MM-dd HH:mm:ss 格式
const formatDateTime = (dateStr: string | null) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hour = String(date.getHours()).padStart(2, '0')
  const minute = String(date.getMinutes()).padStart(2, '0')
  const second = String(date.getSeconds()).padStart(2, '0')
  return `${year}-${month}-${day} ${hour}:${minute}:${second}`
}

// 计算进度百分比
const progressPercent = computed(() => {
  if (!scanProgress.value || !scanProgress.value.totalCount) return 0
  return Math.round((scanProgress.value.scannedCount / scanProgress.value.totalCount) * 100)
})

// counting / aggregating / 还没拿到 totalCount 时进度条 indeterminate
const progressIndeterminate = computed(() => {
  if (!isScanning.value) return false
  const phase = scanProgress.value?.phase
  if (phase === 'counting' || phase === 'aggregating' || phase === 'initializing') return true
  return (scanProgress.value?.totalCount ?? 0) <= 0
})

// tab → 后端 accessTimeFilter 映射
//   workspace        → 不传（workspaceFilter=inside 已经把范围限定到工作空间）
//   new_access       → 'new'（>= full_inventory_time）
//   history_inventory→ 'history'（< full_inventory_time）
// 注意：过滤已经下沉到后端，前端不再二次过滤——否则分页会和列表对不上
const accessTimeFilter = computed<'new' | 'history' | undefined>(() => {
  switch (activeTab.value) {
    case 'new_access': return 'new'
    case 'history_inventory': return 'history'
    default: return undefined
  }
})

// 获取阶段文本
const phaseText = computed(() => {
  const phase = scanProgress.value?.phase
  switch (phase) {
    case 'counting': return '正在统计文件数量...'
    case 'scanning': return '正在扫描文件...'
    case 'aggregating': return '正在聚合数据...'
    case 'completed': return '扫描完成'
    default: return '准备中...'
  }
})

// 获取当前选项卡对应的统计数据
const currentStatistics = computed((): StatisticsGrowth | null => {
  if (!statisticsComparison.value) return null

  switch (activeTab.value) {
    case 'workspace':
      return statisticsComparison.value.workspaceStatistics
    case 'new_access':
      return statisticsComparison.value.nonHistoryStatistics
    case 'history_inventory':
      return statisticsComparison.value.historyStatistics
    default:
      return null
  }
})

// 加载配置
const loadConfig = async () => {
  try {
    config.value = await api.getConfig()
  } catch (error) {
    console.error('Failed to load config:', error)
  }
}

// 加载文件统计数据（对比统计 + 全机概览统计）
const loadStatistics = async () => {
  try {
    const [comparison, resources] = await Promise.all([
      api.getStatistics(),
      api.getResourcesStatistics(),
    ])
    statisticsComparison.value = comparison
    resourcesStatistics.value = resources
  } catch (error) {
    console.error('Failed to load statistics:', error)
  }
}

// 加载文件列表
const loadFiles = async () => {
  if (isScanning.value) return

  loading.value = true
  try {
    // 根据选项卡设置工作空间过滤条件：workspace=工作空间内，其他=全部
    const wsFilter = activeTab.value === 'workspace' ? 'inside' : 'all'

    const result = await api.getFiles({
      search: search.value || undefined,
      workspaceFilter: wsFilter,
      survivalFilter: survivalFilter.value,
      accessTimeFilter: accessTimeFilter.value,
      page: page.value,
      pageSize: pageSize.value,
    })
    files.value = result.files
    totalItems.value = result.total
    // 同时加载统计数据
    await loadStatistics()
  } catch (error) {
    console.error('Failed to load files:', error)
    files.value = []
    totalItems.value = 0
  } finally {
    loading.value = false
  }
}

// 检查并触发首次普查
const checkFirstScan = async () => {
  await loadConfig()

  // 自动日常盘点已关闭；首次普查未完成时也只加载空列表，统一走 loadFiles
  loadFiles()
}

// 开始首次普查
const startFirstScan = async () => {
  if (!workspaceInput.value) return

  // 保存工作空间配置
  await api.saveConfig({ workspace: workspaceInput.value })
  await loadConfig()

  showFirstScanDialog.value = false
  startScan('FULL_INVENTORY')
}

// 开始日常盘点
const startDailyScan = () => {
  startScan('DAILY_CHECK')
}

// 手动刷新（触发日常盘点）
const handleRefresh = () => {
  if (isScanning.value) return
  startDailyScan()
}

const stopTaskPolling = () => {
  if (taskPollTimer !== null) {
    clearInterval(taskPollTimer)
    taskPollTimer = null
  }
  activeTaskId = 0
}

// 轮询任务状态直到非 run
const startTaskPolling = (taskId: number) => {
  if (!taskId) return
  if (taskPollTimer !== null) stopTaskPolling()

  activeTaskId = taskId
  isScanning.value = true

  const tick = async () => {
    try {
      const detail = await api.getScanTaskDetail(taskId)
      const phase = detail.task_phase || (detail.task_state === 'run' ? 'scanning' : 'completed')
      const total = detail.file_total ?? 0
      const scanned = detail.file_scanned_count ?? 0

      scanProgress.value = {
        type: detail.task_state === 'run' ? 'progress' : (detail.task_state === 'fail' ? 'error' : 'complete'),
        taskId: detail.id,
        scannedCount: scanned,
        totalCount: total,
        elapsedMs: 0,
        phase,
        success: detail.task_state === 'succeed',
        errorMessage: detail.task_error_message || undefined,
      }

      if (detail.task_state !== 'run') {
        stopTaskPolling()
        isScanning.value = false
        if (detail.task_state === 'fail') {
          scanError.value = detail.task_error_message || '扫描失败'
        } else {
          await loadFiles()
        }
      }
    } catch (error) {
      console.error('轮询扫描任务失败:', error)
    }
  }

  tick()
  taskPollTimer = window.setInterval(tick, POLL_INTERVAL_MS)
}

// 开始扫描
const startScan = async (mode: 'FULL_INVENTORY' | 'DAILY_CHECK' | 'TARGETED_SCAN') => {
  if (isScanning.value) return

  isScanning.value = true
  scanError.value = null
  scanProgress.value = {
    type: 'progress',
    scannedCount: 0,
    totalCount: 0,
    elapsedMs: 0,
    phase: 'initializing',
  }

  // 清空当前文件列表
  files.value = []

  try {
    const { taskId } = await api.triggerScan({ scanMode: mode })
    if (!taskId) {
      const running = await api.getRunningScanTask()
      if (running && running.id) {
        startTaskPolling(running.id)
      } else {
        isScanning.value = false
      }
      return
    }
    startTaskPolling(taskId)
  } catch (error) {
    scanError.value = error instanceof Error ? error.message : '扫描失败'
    isScanning.value = false
  }
}

// 显示副本
const showCopies = async (file: FileItem) => {
  try {
    const result = await api.getCopies(file.content_sign)
    selectedCopies.value = result.copies
    showCopiesDialog.value = true
  } catch (error) {
    console.error('Failed to load copies:', error)
  }
}

// 点击行显示详情
const handleRowClick = (_event: Event, row: { item: FileItem }) => {
  selectedFile.value = row.item
  showDetailDrawer.value = true
}

// 关闭详情侧边栏
const closeDetailDrawer = () => {
  showDetailDrawer.value = false
  selectedFile.value = null
}

// 格式化文件大小
const formatFileSize = (bytes: number | null | undefined) => {
  if (bytes === null || bytes === undefined) return '-'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(2) + ' ' + units[i]
}

// 获取数据类型文本
const getDataTypeText = (type: number) => {
  switch (type) {
    case 1: return '文件'
    case 2: return '数据库'
    default: return '未知'
  }
}

// 获取存续状态文本和颜色
const getSurvivalStatus = (count: number) => {
  if (count === 0) return { text: '已删除', color: 'error' }
  if (count === 1) return { text: '新文件', color: 'success' }
  return { text: '正常', color: 'info' }
}

// 监听选项卡变化，保存状态
watch(activeTab, (newTab) => {
  saveTabState(newTab)
})

// 监听过滤条件变化
watch([search, survivalFilter, activeTab], () => {
  if (!isScanning.value) {
    page.value = 1 // 过滤条件变化时重置到第一页
    loadFiles()
  }
})

// 监听分页变化
watch([page, pageSize], () => {
  loadFiles()
})

// 监听路由参数变化
watch(() => route.query.action, (newAction) => {
  if (newAction) {
    handleRouteAction()
  }
})

// 处理路由参数
const handleRouteAction = async () => {
  const action = route.query.action as string
  if (!action) return

  // 清除 query 参数，避免重复触发
  router.replace({ path: route.path, query: {} })

  if (action === 'firstScan') {
    // 触发首次普查对话框
    await loadConfig()
    workspaceInput.value = config.value?.workspace || ''
    showFirstScanDialog.value = true
  } else if (action === 'dailyScan') {
    // 触发日常盘点
    handleRefresh()
  }
}

// 组件挂载时检查首次普查
onMounted(() => {
  // 加载选项卡状态
  const savedTab = loadTabState()
  console.log("加载选项卡:"+savedTab)
  if (savedTab && ['workspace', 'new_access', 'history_inventory'].includes(savedTab)) {
    activeTab.value = savedTab as TabType
  }
  // 先检查路由参数
  if (route.query.action) {
    handleRouteAction()
  } else {
    checkFirstScan()
  }

  // 若有正在进行的扫描任务（其他页面/上次启动遗留），挂上轮询
  api.getRunningScanTask().then(running => {
    if (running && running.id) {
      startTaskPolling(running.id)
    }
  }).catch(e => console.error('查询运行中任务失败:', e))
})

onUnmounted(() => {
  stopTaskPolling()
})
</script>

<template>
  <div>
    <!-- 概览图谱（方案A）：KPI 卡片 + 分级环形 + 范围对比，全机口径 -->
    <template v-if="resourcesStatistics">
      <v-row class="mb-1" dense>
        <v-col cols="6" md="3">
          <v-card elevation="1" class="kpi-card">
            <div class="kpi-ic" style="background:#1976d2">
              <v-icon color="white">mdi-file-multiple-outline</v-icon>
            </div>
            <div>
              <div class="kpi-v">{{ overviewKpis.total.toLocaleString() }}</div>
              <div class="kpi-l">总文件数</div>
              <div class="kpi-d" :class="overviewKpis.totalRate > 0 ? 'text-success' : (overviewKpis.totalRate < 0 ? 'text-error' : 'text-grey')">
                {{ overviewKpis.totalRate > 0 ? '↑' : (overviewKpis.totalRate < 0 ? '↓' : '') }}
                {{ overviewKpis.totalRate !== 0 ? Math.abs(overviewKpis.totalRate) + '% 较上次' : '与上次持平' }}
              </div>
            </div>
          </v-card>
        </v-col>
        <v-col cols="6" md="3">
          <v-card elevation="1" class="kpi-card">
            <div class="kpi-ic" style="background:#2e7d32">
              <v-icon color="white">mdi-folder-account-outline</v-icon>
            </div>
            <div>
              <div class="kpi-v">{{ overviewKpis.workspace.toLocaleString() }}</div>
              <div class="kpi-l">工作空间文件</div>
              <div class="kpi-d text-grey">本机工作目录</div>
            </div>
          </v-card>
        </v-col>
        <v-col cols="6" md="3">
          <v-card elevation="1" class="kpi-card">
            <div class="kpi-ic" style="background:#f57c00">
              <v-icon color="white">mdi-alert-outline</v-icon>
            </div>
            <div>
              <div class="kpi-v">{{ overviewKpis.pending.toLocaleString() }}</div>
              <div class="kpi-l">待归类保护</div>
              <div class="kpi-d text-grey">需处理</div>
            </div>
          </v-card>
        </v-col>
        <v-col cols="6" md="3">
          <v-card elevation="1" class="kpi-card">
            <div class="kpi-ic" style="background:#9e9e9e">
              <v-icon color="white">mdi-help-circle-outline</v-icon>
            </div>
            <div>
              <div class="kpi-v">{{ overviewKpis.unclassified.toLocaleString() }}</div>
              <div class="kpi-l">未分类</div>
              <div class="kpi-d text-grey">待定级</div>
            </div>
          </v-card>
        </v-col>
      </v-row>

      <v-row class="mb-4" dense>
        <v-col cols="12" md="5">
          <v-card elevation="1" class="pa-4" height="100%">
            <div class="text-subtitle-2 font-weight-bold mb-1">📊 分级分布</div>
            <div class="text-caption text-grey mb-2">核心 / 重要 / 开放 / 隐私 / 未分类</div>
            <div style="height:240px"><Doughnut :data="gradeChartData" :options="gradeChartOptions" /></div>
          </v-card>
        </v-col>
        <v-col cols="12" md="7">
          <v-card elevation="1" class="pa-4" height="100%">
            <div class="text-subtitle-2 font-weight-bold mb-1">📈 范围对比 · 本次 vs 上次</div>
            <div class="text-caption text-grey mb-2">工作空间 / 新增登记 / 历史治理</div>
            <div style="height:240px"><Bar :data="scopeChartData" :options="scopeChartOptions" /></div>
          </v-card>
        </v-col>
      </v-row>
    </template>

    <!-- 选项卡 -->
    <div class="d-flex align-center mb-4">
      <v-tabs v-model="activeTab" color="primary">
        <v-tab value="workspace">工作文件档案管理</v-tab>
        <v-tab value="new_access">新数据登记管理</v-tab>
        <v-tab value="history_inventory">历史数据专项治理</v-tab>
      </v-tabs>
      <v-spacer />
      <v-chip
        v-if="activeTab !== 'workspace' && config?.full_inventory_time"
        color="info"
        variant="tonal"
        size="small"
      >
        历史封帐时间：{{ formatDateTime(config.full_inventory_time) }}
      </v-chip>
    </div>

    <!-- 操作栏 -->
    <v-card class="mb-4" elevation="1">
      <v-card-text>
        <v-row align="center">
          <v-col cols="12" md="4">
            <v-text-field
              v-model="search"
              prepend-inner-icon="mdi-magnify"
              label="搜索文件"
              variant="outlined"
              density="compact"
              hide-details
              clearable
              :disabled="isScanning"
            />
          </v-col>
          <v-col cols="6" md="2">
            <v-select
              v-model="survivalFilter"
              :items="[
                { title: '全部', value: 'all' },
                { title: '新文件', value: 'new' },
                { title: '已删除', value: 'deleted' },
                { title: '正常', value: 'normal' },
              ]"
              label="存续状态"
              variant="outlined"
              density="compact"
              hide-details
              :disabled="isScanning"
            />
          </v-col>
          <v-col cols="12" md="6">
            <v-row v-if="currentStatistics" align="center" justify="end" no-gutters>
              <v-col cols="auto" class="px-3 text-center">
                <div class="text-caption text-grey">上次数量</div>
                <div class="text-subtitle-1 font-weight-medium">{{ currentStatistics.lastCount }}</div>
              </v-col>
              <v-col cols="auto" class="px-3 text-center">
                <div class="text-caption text-grey">本次数量</div>
                <div class="text-subtitle-1 font-weight-medium">{{ currentStatistics.currentCount }}</div>
              </v-col>
              <v-col cols="auto" class="px-3 text-center">
                <div class="text-caption text-grey">增涨数量</div>
                <div class="text-subtitle-1 font-weight-medium" :class="currentStatistics.growthCount > 0 ? 'text-success' : (currentStatistics.growthCount < 0 ? 'text-error' : '')">
                  {{ currentStatistics.growthCount > 0 ? '+' : '' }}{{ currentStatistics.growthCount }}
                </div>
              </v-col>
              <v-col cols="auto" class="px-3 text-center">
                <div class="text-caption text-grey">增涨率</div>
                <div class="text-subtitle-1 font-weight-medium" :class="currentStatistics.growthRate > 0 ? 'text-success' : (currentStatistics.growthRate < 0 ? 'text-error' : '')">
                  {{ currentStatistics.growthRate > 0 ? '+' : '' }}{{ currentStatistics.growthRate }}%
                </div>
              </v-col>
            </v-row>
          </v-col>
        </v-row>
<!--        <div v-if="statisticsComparison && !statisticsComparison.hasComparison" class="text-caption text-grey mt-2 text-right">-->
<!--          提示：需要至少进行两次扫描才能获得完整的对比数据-->
<!--        </div>-->
      </v-card-text>
    </v-card>

    <!-- 扫描进度 -->
    <v-card v-if="isScanning || scanError" class="mb-4" elevation="1">
      <v-card-text>
        <div v-if="isScanning">
          <div class="text-center mb-2">{{ phaseText }}</div>
          <v-progress-linear
            :model-value="progressIndeterminate ? undefined : progressPercent"
            :indeterminate="progressIndeterminate"
            height="24"
            color="primary"
            rounded
          >
            <template v-slot:default>
              <strong v-if="!progressIndeterminate">{{ progressPercent }}%</strong>
            </template>
          </v-progress-linear>
          <div class="mt-2 text-caption text-center">
            <template v-if="progressIndeterminate && (scanProgress?.totalCount ?? 0) === 0">
              {{ phaseText }}
            </template>
            <template v-else>
              已扫描: {{ scanProgress?.scannedCount || 0 }} / {{ scanProgress?.totalCount || 0 }}
            </template>
          </div>
          <div
            v-if="scanProgress?.currentFile"
            class="mt-1 text-caption text-truncate text-center text-grey"
          >
            {{ scanProgress.currentFile }}
          </div>
        </div>
        <v-alert v-if="scanError" type="error" variant="tonal" closable @click:close="scanError = null">
          {{ scanError }}
        </v-alert>
      </v-card-text>
    </v-card>

    <!-- 文件表格 -->
    <v-card elevation="1">
      <v-data-table
        :headers="headers"
        :items="files"
        :items-per-page="-1"
        :loading="loading"
        item-value="data_distribution_id"
        fixed-header
        hover
        hide-default-footer
        @click:row="handleRowClick"
      >
        <template v-slot:item.path="{ item }">
          <div class="d-flex align-center">
            <v-icon size="small" class="mr-2" color="grey">
              mdi-file-document-outline
            </v-icon>
            <span class="text-truncate" style="max-width: 400px" :title="item.path">
              {{ getFileName(item.path) }}
            </span>
            <v-chip
              v-if="item.copy_count > 1"
              size="small"
              color="info"
              variant="tonal"
              class="ml-2"
              @click="showCopies(item)"
            >
              {{ item.copy_count }} 副本
            </v-chip>
          </div>
        </template>

        <template v-slot:item.status="{ item }">
          <v-tooltip v-if="item.scan_found_count === 1" text="新文件">
            <template v-slot:activator="{ props }">
              <v-icon v-bind="props" color="success" size="small">mdi-file-plus</v-icon>
            </template>
          </v-tooltip>
          <v-tooltip v-else-if="item.scan_found_count === 0" text="已删除">
            <template v-slot:activator="{ props }">
              <v-icon v-bind="props" color="error" size="small">mdi-file-remove</v-icon>
            </template>
          </v-tooltip>
        </template>

        <template v-slot:item.file_create_time="{ item }">
          {{ formatDate(item.file_create_time) }}
        </template>

        <template v-slot:item.file_update_time="{ item }">
          {{ formatDate(item.file_update_time) }}
        </template>

        <template v-slot:no-data>
          <div class="text-center py-8">
            <v-icon size="64" color="grey-lighten-1">mdi-folder-open-outline</v-icon>
            <div class="mt-4 text-grey">暂无文件数据</div>
            <div class="mt-2 text-caption text-grey">请先进行首次普查或日常盘点</div>
          </div>
        </template>
      </v-data-table>

      <!-- 分页控件 -->
      <v-divider />
      <v-card-text class="py-2 d-flex align-center justify-space-between">
        <div class="d-flex align-center">
          <span class="text-body-2 text-grey mr-4">每页显示</span>
          <v-select
            v-model="pageSize"
            :items="[10, 20, 50, 100]"
            variant="outlined"
            density="compact"
            hide-details
            style="width: 100px"
          />
        </div>
        <v-pagination
          v-model="page"
          :length="totalPages"
          :total-visible="5"
          density="compact"
        />
      </v-card-text>
    </v-card>

    <!-- 首次普查对话框 -->
    <v-dialog v-model="showFirstScanDialog" persistent max-width="500">
      <v-card>
        <v-card-title>首次普查</v-card-title>
        <v-card-text>
          <p class="mb-4">系统检测到您尚未进行首次普查，请配置工作空间目录后开始扫描。</p>
          <v-text-field
            v-model="workspaceInput"
            label="工作空间目录"
            placeholder="/Users/xxx/workspace"
            variant="outlined"
            hint="请输入需要监控的工作目录路径"
            persistent-hint
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn
            variant="text"
            @click="showFirstScanDialog = false"
          >
            取消
          </v-btn>
          <v-btn
            color="primary"
            :disabled="!workspaceInput"
            @click="startFirstScan"
          >
            立即开始
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 副本详情对话框 -->
    <v-dialog v-model="showCopiesDialog" max-width="700">
      <v-card>
        <v-card-title>副本列表</v-card-title>
        <v-card-text>
          <v-list lines="two">
            <v-list-item
              v-for="copy in selectedCopies"
              :key="copy.data_distribution_id"
            >
              <v-list-item-title class="text-truncate">
                {{ copy.path }}
              </v-list-item-title>
              <v-list-item-subtitle>
                创建时间: {{ formatDate(copy.file_create_time) }}
                | 修改时间: {{ formatDate(copy.file_update_time) }}
              </v-list-item-subtitle>
            </v-list-item>
          </v-list>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="showCopiesDialog = false">关闭</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 文件详情侧边栏 -->
    <v-navigation-drawer
      v-model="showDetailDrawer"
      location="right"
      width="400"
    >
      <template v-if="selectedFile">
        <v-toolbar color="primary" density="compact">
          <v-toolbar-title class="text-body-1">文件详情</v-toolbar-title>
          <v-spacer />
          <v-btn icon="mdi-close" variant="text" @click="closeDetailDrawer" />
        </v-toolbar>

        <v-list density="compact">
          <!-- 文件路径 -->
          <v-list-item>
            <v-list-item-subtitle>完整路径</v-list-item-subtitle>
            <v-list-item-title class="text-wrap text-body-2">
              {{ selectedFile.path }}
            </v-list-item-title>
          </v-list-item>

          <v-divider />

          <!-- 基本信息 -->
          <v-list-subheader>基本信息</v-list-subheader>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-identifier</v-icon>
            </template>
            <v-list-item-subtitle>数据分布ID</v-list-item-subtitle>
            <v-list-item-title>{{ selectedFile.data_distribution_id }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-file-outline</v-icon>
            </template>
            <v-list-item-subtitle>数据类型</v-list-item-subtitle>
            <v-list-item-title>{{ getDataTypeText(selectedFile.data_type) }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-tag-outline</v-icon>
            </template>
            <v-list-item-subtitle>存续状态</v-list-item-subtitle>
            <v-list-item-title>
              <v-chip
                :color="getSurvivalStatus(selectedFile.scan_found_count).color"
                size="small"
                variant="tonal"
              >
                {{ getSurvivalStatus(selectedFile.scan_found_count).text }}
              </v-chip>
            </v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-content-copy</v-icon>
            </template>
            <v-list-item-subtitle>副本数量</v-list-item-subtitle>
            <v-list-item-title>
              <v-chip
                v-if="selectedFile.copy_count > 1"
                color="info"
                size="small"
                variant="tonal"
                @click="showCopies(selectedFile)"
              >
                {{ selectedFile.copy_count }} 副本
              </v-chip>
              <span v-else>1</span>
            </v-list-item-title>
          </v-list-item>

          <v-divider />

          <!-- 文件属性 -->
          <v-list-subheader>文件属性</v-list-subheader>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-file-code-outline</v-icon>
            </template>
            <v-list-item-subtitle>文件后缀</v-list-item-subtitle>
            <v-list-item-title>{{ selectedFile.file_suffix || '-' }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-database-outline</v-icon>
            </template>
            <v-list-item-subtitle>文件大小</v-list-item-subtitle>
            <v-list-item-title>{{ formatFileSize(selectedFile.file_size) }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-magic-staff</v-icon>
            </template>
            <v-list-item-subtitle>文件魔数</v-list-item-subtitle>
            <v-list-item-title class="text-truncate">{{ selectedFile.file_magic || '-' }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-eye-off-outline</v-icon>
            </template>
            <v-list-item-subtitle>是否隐藏</v-list-item-subtitle>
            <v-list-item-title>{{ selectedFile.file_hide ? '是' : '否' }}</v-list-item-title>
          </v-list-item>

          <v-divider />

          <!-- 签名信息 -->
          <v-list-subheader>签名信息</v-list-subheader>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-fingerprint</v-icon>
            </template>
            <v-list-item-subtitle>内容签名 (MD5)</v-list-item-subtitle>
            <v-list-item-title class="text-body-2" style="font-family: monospace;">
              {{ selectedFile.content_sign }}
            </v-list-item-title>
          </v-list-item>

          <v-divider />

          <!-- 时间信息 -->
          <v-list-subheader>时间信息</v-list-subheader>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-calendar-plus</v-icon>
            </template>
            <v-list-item-subtitle>文件创建时间</v-list-item-subtitle>
            <v-list-item-title>{{ formatDateTime(selectedFile.file_create_time) }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-calendar-edit</v-icon>
            </template>
            <v-list-item-subtitle>文件修改时间</v-list-item-subtitle>
            <v-list-item-title>{{ formatDateTime(selectedFile.file_update_time) }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-calendar-clock</v-icon>
            </template>
            <v-list-item-subtitle>文件读取时间</v-list-item-subtitle>
            <v-list-item-title>{{ formatDateTime(selectedFile.file_read_time) }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-radar</v-icon>
            </template>
            <v-list-item-subtitle>扫描发现时间</v-list-item-subtitle>
            <v-list-item-title>{{ formatDateTime(selectedFile.scan_time) }}</v-list-item-title>
          </v-list-item>

          <v-divider />

          <!-- 设备信息 -->
          <v-list-subheader>设备信息</v-list-subheader>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-ip-network</v-icon>
            </template>
            <v-list-item-subtitle>IP 地址</v-list-item-subtitle>
            <v-list-item-title>{{ selectedFile.ip || '-' }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-ethernet</v-icon>
            </template>
            <v-list-item-subtitle>MAC 地址</v-list-item-subtitle>
            <v-list-item-title style="font-family: monospace;">{{ selectedFile.mac_address || '-' }}</v-list-item-title>
          </v-list-item>

          <v-divider />

          <!-- 记录信息 -->
          <v-list-subheader>记录信息</v-list-subheader>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-counter</v-icon>
            </template>
            <v-list-item-subtitle>扫描发现次数</v-list-item-subtitle>
            <v-list-item-title>{{ selectedFile.scan_found_count }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-clock-plus-outline</v-icon>
            </template>
            <v-list-item-subtitle>记录创建时间</v-list-item-subtitle>
            <v-list-item-title>{{ formatDateTime(selectedFile.create_time) }}</v-list-item-title>
          </v-list-item>

          <v-list-item>
            <template v-slot:prepend>
              <v-icon size="small" color="grey">mdi-clock-edit-outline</v-icon>
            </template>
            <v-list-item-subtitle>记录更新时间</v-list-item-subtitle>
            <v-list-item-title>{{ formatDateTime(selectedFile.update_time) }}</v-list-item-title>
          </v-list-item>
        </v-list>
      </template>
    </v-navigation-drawer>
  </div>
</template>

<style scoped>
.kpi-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
}
.kpi-ic {
  width: 46px;
  height: 46px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.kpi-v {
  font-size: 24px;
  font-weight: 700;
  line-height: 1.1;
}
.kpi-l {
  font-size: 13px;
  color: #64748b;
  margin-top: 2px;
}
.kpi-d {
  font-size: 12px;
  margin-top: 3px;
}
</style>
