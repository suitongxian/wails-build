<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { api } from '@/services/api'
import type { ScanTask as ApiScanTask, ScanProgressEvent } from '@/services/api'

// 扫描任务详情（侧边栏展示）
interface ScanTaskDetail extends ApiScanTask {
  heartbeat: number
}

// 扫描进度信息（本地接口，用于显示）
interface ScanProgress {
  taskId: number
  taskPhase: string
  fileTotal: number
  fileScannedCount: number
}

// ============== 状态数据 ==============
const scanTasks = ref<ApiScanTask[]>([])
const selectedTask = ref<ScanTaskDetail | null>(null)
const showDetailDrawer = ref(false)
const isScanning = ref(false)
const showBackgroundScanOption = ref(false)
const hasCompletedFirstScan = ref(false)
const currentProgress = ref<ScanProgress | null>(null)

// 进度事件（沿用旧的 SSE 格式，前端组件直接消费）
const scanProgress = ref<ScanProgressEvent | null>(null)
const scanError = ref<string | null>(null)

const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const totalTasks = ref(0)

// 当前正在轮询的扫描任务 id（无任务为 0）；以及定时器
let activeTaskId = 0
let taskPollTimer: number | null = null
const POLL_INTERVAL_MS = 2000

// ============== 计算属性 ==============
// 从 FilesView 复制的计算属性：阶段文本
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

// 最近一次扫描任务
const lastScanTask = computed(() => {
  return scanTasks.value.length > 0 ? scanTasks.value[0] : null
})

// 计算总用时
const calcDuration = (startTime: string | null | undefined, endTime: string | null | undefined): string => {
  if (!startTime) return '-'
  try {
    const start = new Date(startTime)
    const end = endTime ? new Date(endTime) : new Date()
    const diffMs = end.getTime() - start.getTime()
    if (diffMs < 1000) return `${diffMs}ms`
    const diffSec = Math.floor(diffMs / 1000)
    if (diffSec < 60) return `${diffSec}秒`
    const diffMin = Math.floor(diffSec / 60)
    const remSec = diffSec % 60
    if (diffMin < 60) return `${diffMin}分${remSec}秒`
    const diffHour = Math.floor(diffMin / 60)
    const remMin = diffMin % 60
    return `${diffHour}小时${remMin}分`
  } catch {
    return '-'
  }
}

// 最近一次扫描总用时
const lastScanDuration = computed(() => {
  return calcDuration(lastScanTask.value?.create_time, lastScanTask.value?.end_time)
})
const taskStateMap: Record<string, { text: string; color: string; icon: string }> = {
  run: { text: '进行中', color: 'info', icon: 'mdi-loading mdi-spin' },
  succeed: { text: '成功', color: 'success', icon: 'mdi-check-circle' },
  fail: { text: '失败', color: 'error', icon: 'mdi-alert-circle' }
}

const scanTypeMap: Record<string, { text: string; icon: string }> = {
  FILE: { text: '文件扫描', icon: 'mdi-file-document-multiple' },
  DATABASE: { text: '数据库扫描', icon: 'mdi-database' }
}

// 进度百分比（兼容两种进度格式）
const progressPercent = computed(() => {
  // 优先使用 FilesView 格式的 scanProgress
  if (scanProgress.value && scanProgress.value.totalCount) {
    return Math.round((scanProgress.value.scannedCount / scanProgress.value.totalCount) * 100)
  }
  // 兼容原有 ScanProgress 格式
  if (currentProgress.value && currentProgress.value.fileTotal !== 0) {
    return Math.round((currentProgress.value.fileScannedCount / currentProgress.value.fileTotal) * 100)
  }
  return 0
})

// 进度条是否为 indeterminate（counting / aggregating 阶段或还没拿到 totalCount 时）
const progressIndeterminate = computed(() => {
  if (!isScanning.value) return false
  const phase = scanProgress.value?.phase
  if (phase === 'counting' || phase === 'aggregating' || phase === 'initializing') return true
  const total = scanProgress.value?.totalCount ?? currentProgress.value?.fileTotal ?? 0
  return total <= 0
})

// 格式化时间
const formatTime = (timeStr: string | null | undefined): string => {
  if (!timeStr) return '-'
  try {
    const date = new Date(timeStr)
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    })
  } catch {
    return timeStr
  }
}

// ============== 方法 ==============
// 加载扫描任务列表
const loadScanTasks = async () => {
  try {
    loading.value = true
    const result = await api.getScanTasks(currentPage.value, pageSize.value)
    scanTasks.value = result.tasks
    totalTasks.value = result.total
  } catch (error) {
    console.error('加载扫描任务失败:', error)
  } finally {
    loading.value = false
  }
}

// 加载扫描任务详情
const handleTaskClick = async (task: ApiScanTask) => {
  try {
    const detail = await api.getScanTaskDetail(task.id!)
    selectedTask.value = {
      ...detail,
      heartbeat: detail.heartbeat || 0
    }
    showDetailDrawer.value = true
  } catch (error) {
    console.error('加载扫描详情失败:', error)
  }
}

// 把后端 ScanTask 详情映射为前端进度展示
const applyTaskToProgress = (task: ApiScanTask) => {
  const phase = task.task_phase || (task.task_state === 'run' ? 'scanning' : 'completed')
  const total = task.file_total ?? 0
  const scanned = task.file_scanned_count ?? 0

  scanProgress.value = {
    type: task.task_state === 'run' ? 'progress' : (task.task_state === 'fail' ? 'error' : 'complete'),
    taskId: task.id,
    scannedCount: scanned,
    totalCount: total,
    elapsedMs: 0,
    phase,
    success: task.task_state === 'succeed',
    errorMessage: task.task_error_message || undefined,
  }
  currentProgress.value = {
    taskId: task.id || 0,
    taskPhase: phase,
    fileTotal: total,
    fileScannedCount: scanned,
  }
}

const stopTaskPolling = () => {
  if (taskPollTimer !== null) {
    clearInterval(taskPollTimer)
    taskPollTimer = null
  }
  activeTaskId = 0
}

// 轮询单个扫描任务直到 task_state 不再为 'run'
const startTaskPolling = (taskId: number) => {
  if (!taskId) return
  if (taskPollTimer !== null && activeTaskId === taskId) return

  // 如已有别的任务在轮询，先停掉
  if (taskPollTimer !== null) stopTaskPolling()

  activeTaskId = taskId
  isScanning.value = true

  const tick = async () => {
    try {
      const detail = await api.getScanTaskDetail(taskId)
      applyTaskToProgress(detail)

      if (detail.task_state !== 'run') {
        // 任务已结束
        stopTaskPolling()
        isScanning.value = false
        if (detail.task_state === 'fail') {
          scanError.value = detail.task_error_message || '扫描失败'
        } else {
          hasCompletedFirstScan.value = true
        }
        await loadScanTasks()
      }
    } catch (error) {
      console.error('轮询扫描任务失败:', error)
    }
  }

  // 立即拉一次再进入定时
  tick()
  taskPollTimer = window.setInterval(tick, POLL_INTERVAL_MS)
}

// ============== 触发扫描 ==============
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

  try {
    const { taskId } = await api.triggerScan({ scanMode: mode })
    if (!taskId) {
      // 后端启动成功但还没拿到 id（极少见）；进入 mount-style 兜底
      const running = await api.getRunningScanTask()
      if (running) {
        startTaskPolling(running.id!)
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

async function handleFirstScan() {
  await startScan('FULL_INVENTORY')
}

async function handleDailyScan(background: boolean) {
  await startScan('DAILY_CHECK')
  if (background) {
    console.log('后台盘点已启动')
  }
}

const closeDetailDrawer = () => {
  showDetailDrawer.value = false
}

// ============== 生命周期 ==============
onMounted(async () => {
  // 先拉历史，方便用户立即看到旧记录
  await loadScanTasks()

  // 派生 hasCompletedFirstScan：以系统配置 last_scan_time 为准
  try {
    const config = await api.getConfig()
    hasCompletedFirstScan.value = !!config.last_scan_time
  } catch (e) {
    console.error('加载配置失败:', e)
  }

  // 若有正在进行的扫描，挂上轮询
  try {
    const running = await api.getRunningScanTask()
    if (running && running.id) {
      startTaskPolling(running.id)
    }
  } catch (e) {
    console.error('查询运行中任务失败:', e)
  }
})

onUnmounted(() => {
  stopTaskPolling()
})
</script>

<template>
  <div class="scan-view">
    <!-- 面包屑 -->
<!--    <v-row>-->
<!--      <v-col cols="12">-->
<!--        <v-breadcrumbs :items="[{ title: '首页', to: '/' }, { title: '文本扫描', disabled: true }]" />-->
<!--      </v-col>-->
<!--    </v-row>-->

    <!-- ============== 上部：历史扫描记录列表 ============== -->
    <v-row>
      <v-col cols="12">
        <v-card class="history-card">
          <v-card-item>
            <v-card-title>
              <v-icon start>mdi-history</v-icon>
              扫描历史记录
            </v-card-title>
            <v-card-subtitle>
              点击查看扫描详情
            </v-card-subtitle>
          </v-card-item>

          <v-card-text>
            <v-table density="compact">
              <thead>
                <tr>
                  <th class="text-left">任务ID</th>
                  <th class="text-left">扫描类型</th>
                  <th class="text-left">工作空间</th>
                  <th class="text-left">总文件数</th>
                  <th class="text-left">工作空间文件数</th>
                  <th class="text-left">状态</th>
                  <th class="text-left">扫描时间</th>
                  <th class="text-left">参数变更</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="task in scanTasks"
                  :key="task.id"
                  class="task-row"
                  @click="handleTaskClick(task)"
                >
                  <td class="text-primary">#{{ task.id }}</td>
                  <td>
                    <v-icon size="small" class="mr-1">
                      {{ scanTypeMap[task.scan_type].icon }}
                    </v-icon>
                    {{ scanTypeMap[task.scan_type].text }}
                  </td>
                  <td>
                    <v-tooltip :text="task.workspace_path || '-'">
                      <template v-slot:activator="{ props }">
                        <span v-bind="props" class="text-truncate">
                          {{ task.workspace_path || '-' }}
                        </span>
                      </template>
                    </v-tooltip>
                  </td>
                  <td>{{ task.file_total || 0 }}</td>
                  <td>{{ task.workspace_count || 0 }}</td>
                  <td>
                    <v-chip
                      :color="taskStateMap[task.task_state].color"
                      size="small"
                      variant="flat"
                    >
                      <v-icon size="small" start>
                        {{ taskStateMap[task.task_state].icon }}
                      </v-icon>
                      {{ taskStateMap[task.task_state].text }}
                    </v-chip>
                  </td>
                  <td class="text-caption">
                    {{ formatTime(task.create_time) }}
                  </td>
                  <td class="param-change-cell">
                    <!-- 工作空间路径变更标志 -->
                    <v-tooltip v-if="task.paramsChanged?.workspacePathChanged" location="top">
                      <template v-slot:activator="{ props }">
                        <v-icon
                          v-bind="props"
                          color="warning"
                          size="small"
                          class="mr-1"
                        >
                          mdi-folder-alert
                        </v-icon>
                      </template>
                      <span>工作空间变更</span>
                    </v-tooltip>
                    <!-- 扫描范围变更标志 -->
                    <v-tooltip v-if="task.paramsChanged?.scanAreaPathChanged" location="top">
                      <template v-slot:activator="{ props }">
                        <v-icon
                          v-bind="props"
                          color="warning"
                          size="small"
                          class="mr-1"
                        >
                          mdi-folder-alert
                        </v-icon>
                      </template>
                      <span>扫描范围变更</span>
                    </v-tooltip>
                    <!-- 管控类别变更标志 -->
                    <v-tooltip v-if="task.paramsChanged?.controlTypeChanged" location="top">
                      <template v-slot:activator="{ props }">
                        <v-icon
                          v-bind="props"
                          color="warning"
                          size="small"
                        >
                          mdi-shield-alert
                        </v-icon>
                      </template>
                      <span>管控类别变更</span>
                    </v-tooltip>
                  </td>
                </tr>
                <tr v-if="loading">
                  <td colspan="8" class="text-center py-8">
                    <v-progress-circular indeterminate color="primary" size="32" />
                    <div class="mt-2 text-caption">加载中...</div>
                  </td>
                </tr>
                <tr v-else-if="scanTasks.length === 0">
                  <td colspan="8" class="text-center text-grey py-8">
                    暂无扫描记录
                  </td>
                </tr>
              </tbody>
            </v-table>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>

    <!-- ============== 中部：扫描执行进度 ============== -->
    <v-row>
      <v-col cols="12">
        <v-card class="progress-card">
          <v-card-item>
            <v-card-title>
              <v-icon start>mdi-progress-clock</v-icon>
              扫描执行进度
            </v-card-title>
          </v-card-item>

          <v-card-text>
            <!-- 扫描中或有错误时显示进度 -->
            <div v-if="isScanning || scanError">
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
            </div>

            <!-- 未扫描状态：显示最近一次扫描报告详情 -->
            <div v-else-if="!isScanning && !currentProgress">
              <div v-if="lastScanTask" class="last-report">
                <div class="d-flex align-center justify-space-between mb-4">
                  <h4 class="text-h6">最近一次扫描报告</h4>
                  <v-chip
                    :color="taskStateMap[lastScanTask.task_state].color"
                    size="small"
                    variant="flat"
                  >
                    <v-icon size="small" start>
                      {{ taskStateMap[lastScanTask.task_state].icon }}
                    </v-icon>
                    {{ taskStateMap[lastScanTask.task_state].text }}
                  </v-chip>
                </div>
                <v-row dense>
                  <v-col cols="6" sm="4">
                    <div class="report-item">
                      <div class="report-label">开始时间</div>
                      <div class="report-value">{{ formatTime(lastScanTask.create_time) }}</div>
                    </div>
                  </v-col>
                  <v-col cols="6" sm="4">
                    <div class="report-item">
                      <div class="report-label">总用时</div>
                      <div class="report-value">{{ lastScanDuration }}</div>
                    </div>
                  </v-col>
                  <v-col cols="6" sm="4">
                    <div class="report-item">
                      <div class="report-label">扫描文件总数</div>
                      <div class="report-value">{{ lastScanTask.file_total || 0 }}</div>
                    </div>
                  </v-col>
                  <v-col cols="6" sm="4">
                    <div class="report-item">
                      <div class="report-label">工作空间文件数</div>
                      <div class="report-value">{{ lastScanTask.workspace_count || 0 }}</div>
                    </div>
                  </v-col>
                  <v-col cols="6" sm="4">
                    <div class="report-item">
                      <div class="report-label">扫描类型</div>
                      <div class="report-value">
                        <v-icon size="small" class="mr-1">
                          {{ scanTypeMap[lastScanTask.scan_type].icon }}
                        </v-icon>
                        {{ scanTypeMap[lastScanTask.scan_type].text }}
                      </div>
                    </div>
                  </v-col>
                  <v-col cols="6" sm="4">
                    <div class="report-item">
                      <div class="report-label">任务ID</div>
                      <div class="report-value text-primary">#{{ lastScanTask.id }}</div>
                    </div>
                  </v-col>
                </v-row>
                <!-- 异常信息 -->
                <v-alert
                  v-if="lastScanTask.task_error_message"
                  type="warning"
                  variant="tonal"
                  density="compact"
                  class="mt-3"
                >
                  <template v-slot:prepend>
                    <v-icon>mdi-alert-circle</v-icon>
                  </template>
                  <div class="text-subtitle-2 mb-1">异常信息</div>
                  <div class="text-body-2">{{ lastScanTask.task_error_message }}</div>
                </v-alert>
              </div>
              <!-- 无扫描记录 -->
              <div v-else class="idle-state">
                <v-icon size="48" color="grey-lighten-1">mdi-progress-question</v-icon>
                <p class="text-grey mt-4">当前没有扫描记录</p>
              </div>
            </div>

            <!-- 扫描进行中 -->
            <div v-else-if="currentProgress" class="scanning-state">
              <!-- 进度条 -->
              <div class="progress-section">
                <div class="progress-info mb-2">
                  <span class="text-subtitle-2">任务 #{{ currentProgress.taskId }} - {{ currentProgress.taskPhase }}</span>
                  <span class="text-primary font-weight-bold">{{ progressPercent }}%</span>
                </div>
                <v-progress-linear
                  :model-value="progressPercent"
                  color="primary"
                  height="24"
                  rounded
                >
                  <template v-slot:default="{ value }">
                    <strong>{{ Math.ceil(value) }}%</strong>
                  </template>
                </v-progress-linear>
              </div>
            </div>

            <!-- 扫描准备中 -->
            <div v-else class="preparing-state">
              <v-progress-linear
                indeterminate
                color="primary"
                height="8"
                rounded
              />
              <p class="text-subtitle-1 mt-4 text-center">正在准备扫描...</p>
            </div>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>

    <!-- ============== 下部：功能区 ============== -->
    <v-row>
      <v-col cols="12">
        <v-card class="action-card">
          <v-card-item>
            <v-card-title>
              <v-icon start>mdi-cog-play</v-icon>
              扫描操作
            </v-card-title>
          </v-card-item>

          <v-card-text>
            <v-row align="center">
              <!-- 首次普查按钮 -->
              <v-col cols="12" sm="6">
                <v-tooltip location="top" :disabled="!hasCompletedFirstScan">
                  <template v-slot:activator="{ props }">
                    <div v-bind="props">
                      <v-btn
                        :disabled="hasCompletedFirstScan || isScanning"
                        color="primary"
                        size="large"
                        block
                        prepend-icon="mdi-clipboard-search"
                        @click="handleFirstScan"
                      >
                        首次普查
                      </v-btn>
                      <div v-if="hasCompletedFirstScan" class="text-caption text-grey mt-2">
                        <v-icon size="x-small">mdi-check</v-icon>
                        已完成首次普查
                      </div>
                    </div>
                  </template>
                  <div class="tooltip-content">
                    <div>首次普查已完成，不可重复执行</div>
                    <div class="text-caption mt-1">如需重新开始普查，请在配置管理中"恢复出厂设置"</div>
                  </div>
                </v-tooltip>
              </v-col>

              <!-- 日常盘点按钮 -->
              <v-col cols="12" sm="6">
                <div class="daily-scan-section">
                  <v-tooltip location="top" :disabled="hasCompletedFirstScan">
                    <template v-slot:activator="{ props }">
                      <div v-bind="props">
                        <v-btn
                          :disabled="isScanning || !hasCompletedFirstScan"
                          color="success"
                          size="large"
                          block
                          prepend-icon="mdi-refresh"
                          @click="handleDailyScan(false)"
                        >
                          日常盘点
                        </v-btn>
                      </div>
                    </template>
                    <div>请先完成首次普查</div>
                  </v-tooltip>
                  <div v-if="hasCompletedFirstScan" class="text-caption text-grey mt-2">
                    <v-icon size="x-small">mdi-check</v-icon>
                    点击开始日常盘点
                  </div>
<!--                  <v-checkbox-->
<!--                    v-model="showBackgroundScanOption"-->
<!--                    label="后台盘点并退出"-->
<!--                    density="compact"-->
<!--                    class="mt-2"-->
<!--                    hide-details-->
<!--                  />-->
                </div>
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>

    <!-- ============== 右侧侧边栏：扫描详情 ============== -->
    <v-navigation-drawer
      v-model="showDetailDrawer"
      location="right"
      width="500"
      temporary
    >
      <template v-if="selectedTask">
        <!-- 头部 -->
        <div class="drawer-header pa-4">
          <div class="d-flex align-center justify-space-between mb-2">
            <h3 class="text-h6">扫描详情</h3>
            <v-btn icon="mdi-close" variant="text" @click="closeDetailDrawer" />
          </div>
          <v-chip :color="taskStateMap[selectedTask.task_state].color" size="small" variant="flat">
            <v-icon size="small" start>
              {{ taskStateMap[selectedTask.task_state].icon }}
            </v-icon>
            {{ taskStateMap[selectedTask.task_state].text }}
          </v-chip>
          <div class="text-caption text-grey mt-1">任务 ID: #{{ selectedTask.id }}</div>
        </div>

        <v-divider />

        <!-- 参数变更标签 -->
        <div class="pa-4">
          <div class="text-subtitle-2 mb-3">
            <v-icon size="small" start>mdi-compare</v-icon>
            参数变更检测
          </div>
          <v-row dense>
            <v-col cols="12">
              <v-card
                :variant="selectedTask.paramsChanged?.workspacePathChanged ? 'tonal' : 'outlined'"
                :color="selectedTask.paramsChanged?.workspacePathChanged ? 'warning' : 'default'"
                class="param-card"
              >
                <v-card-text class="pa-2 d-flex align-center">
                  <v-icon
                    :color="selectedTask.paramsChanged?.workspacePathChanged ? 'warning' : 'success'"
                    size="small"
                    class="mr-2"
                  >
                    {{ selectedTask.paramsChanged?.workspacePathChanged ? 'mdi-alert' : 'mdi-check' }}
                  </v-icon>
                  <div>
                    <div class="text-caption">工作空间路径</div>
                    <div class="text-body-2">{{ selectedTask.workspace_path || '未设置' }}</div>
                  </div>
                </v-card-text>
              </v-card>
            </v-col>
            <v-col cols="12">
              <v-card
                :variant="selectedTask.paramsChanged?.controlTypeChanged ? 'tonal' : 'outlined'"
                :color="selectedTask.paramsChanged?.controlTypeChanged ? 'warning' : 'default'"
                class="param-card"
              >
                <v-card-text class="pa-2 d-flex align-center">
                  <v-icon
                    :color="selectedTask.paramsChanged?.controlTypeChanged ? 'warning' : 'success'"
                    size="small"
                    class="mr-2"
                  >
                    {{ selectedTask.paramsChanged?.controlTypeChanged ? 'mdi-alert' : 'mdi-check' }}
                  </v-icon>
                  <div>
                    <div class="text-caption">管控类别</div>
                    <div class="text-body-2"></div>
                  </div>
                </v-card-text>
              </v-card>
            </v-col>
            <v-col cols="12">
              <v-card
                :variant="selectedTask.paramsChanged?.scanAreaPathChanged ? 'tonal' : 'outlined'"
                :color="selectedTask.paramsChanged?.scanAreaPathChanged ? 'warning' : 'default'"
                class="param-card"
              >
                <v-card-text class="pa-2 d-flex align-center">
                  <v-icon
                    :color="selectedTask.paramsChanged?.scanAreaPathChanged ? 'warning' : 'success'"
                    size="small"
                    class="mr-2"
                  >
                    {{ selectedTask.paramsChanged?.scanAreaPathChanged ? 'mdi-alert' : 'mdi-check' }}
                  </v-icon>
                  <div>
                    <div class="text-caption">扫描路径</div>
                    <div class="text-body-2">{{ selectedTask.file_scan_range || '未设置' }}</div>
                  </div>
                </v-card-text>
              </v-card>
            </v-col>
          </v-row>
        </div>

        <v-divider />

        <!-- 扫描统计 -->
        <div class="pa-4">
          <div class="text-subtitle-2 mb-3">
            <v-icon size="small" start>mdi-chart-bar</v-icon>
            扫描统计
          </div>
          <v-row dense>
            <v-col cols="6">
              <div class="stat-item">
                <div class="stat-label">扫描类型</div>
                <div class="stat-value">
                  <v-icon size="small" class="mr-1">
                    {{ scanTypeMap[selectedTask.scan_type].icon }}
                  </v-icon>
                  {{ scanTypeMap[selectedTask.scan_type].text }}
                </div>
              </div>
            </v-col>
            <v-col cols="6">
              <div class="stat-item">
                <div class="stat-label">任务阶段</div>
                <div class="stat-value">{{ selectedTask.task_phase || '-' }}</div>
              </div>
            </v-col>
            <v-col cols="6">
              <div class="stat-item">
                <div class="stat-label">扫描总数</div>
                <div class="stat-value">{{ selectedTask.file_total || 0 }}</div>
              </div>
            </v-col>
            <v-col cols="6">
              <div class="stat-item">
                <div class="stat-label">已扫描</div>
                <div class="stat-value">{{ selectedTask.file_scanned_count || 0 }}</div>
              </div>
            </v-col>
            <v-col cols="6">
              <div class="stat-item">
                <div class="stat-label">工作空间文件</div>
                <div class="stat-value">{{ selectedTask.workspace_count || 0 }}</div>
              </div>
            </v-col>
            <v-col cols="6">
              <div class="stat-item">
                <div class="stat-label">心跳次数</div>
                <div class="stat-value">{{ selectedTask.heartbeat }}</div>
              </div>
            </v-col>
          </v-row>
        </div>

        <v-divider />

        <!-- 时间信息 -->
        <div class="pa-4">
          <div class="text-subtitle-2 mb-3">
            <v-icon size="small" start>mdi-clock-outline</v-icon>
            时间信息
          </div>
          <v-row dense>
            <v-col cols="12">
              <div class="stat-item">
                <div class="stat-label">开始时间</div>
                <div class="stat-value">{{ formatTime(selectedTask.create_time) }}</div>
              </div>
            </v-col>
            <v-col cols="12">
              <div class="stat-item">
                <div class="stat-label">结束时间</div>
                <div class="stat-value">{{ formatTime(selectedTask.end_time) }}</div>
              </div>
            </v-col>
          </v-row>
        </div>

        <v-divider />

        <!-- 文件后缀统计 -->
<!--        <div class="pa-4">-->
<!--          <div class="text-subtitle-2 mb-3">-->
<!--            <v-icon size="small" start>mdi-file-multiple</v-icon>-->
<!--            文件后缀统计-->
<!--          </div>-->
<!--          <v-card variant="outlined" class="mb-2">-->
<!--            <v-card-title class="text-caption pa-2">本次扫描 ({{ selectedTask.file_all_suffix_count || 0 }} 种)</v-card-title>-->
<!--            <v-card-text class="pa-2 text-body-2">-->
<!--              {{ selectedTask.file_all_suffix_text || '-' }}-->
<!--            </v-card-text>-->
<!--          </v-card>-->
<!--          <v-card variant="outlined">-->
<!--            <v-card-title class="text-caption pa-2">工作空间 ({{ selectedTask.file_count_suffix_count || 0 }} 种)</v-card-title>-->
<!--            <v-card-text class="pa-2 text-body-2">-->
<!--              工作空间中的文件后缀种类总数-->
<!--            </v-card-text>-->
<!--          </v-card>-->
<!--        </div>-->

        <!-- 错误信息 -->
        <div v-if="selectedTask.task_error_message" class="pa-4">
          <div class="text-subtitle-2 mb-3">
            <v-icon size="small" start color="error">mdi-alert-circle</v-icon>
            错误信息
          </div>
          <v-alert type="error" variant="tonal" density="compact">
            {{ selectedTask.task_error_message }}
          </v-alert>
        </div>

        <!-- 扫描日志 -->
        <div v-if="selectedTask.scan_log" class="pa-4">
          <div class="text-subtitle-2 mb-3">
            <v-icon size="small" start>mdi-console</v-icon>
            扫描日志
          </div>
          <v-card variant="tonal">
            <v-card-text class="pa-2">
              <pre class="log-text">{{ selectedTask.scan_log }}</pre>
            </v-card-text>
          </v-card>
        </div>
      </template>
    </v-navigation-drawer>
  </div>
</template>

<style scoped>
.scan-view {
  padding: 16px;
}

/* 历史记录表格 */
.task-row {
  cursor: pointer;
  transition: background-color 0.2s;
}

.task-row:hover {
  background-color: rgba(0, 0, 0, 0.04);
}

.text-truncate {
  display: inline-block;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 参数变更单元格 */
.param-change-cell {
  white-space: nowrap;
}

/* 进度区域 */
.idle-state,
.preparing-state {
  text-align: center;
  padding: 40px 20px;
}

/* 报告显示区域 */
.last-report {
  padding: 8px 0;
}

.report-item {
  padding: 12px;
  background: #f5f5f5;
  border-radius: 8px;
  height: 100%;
}

.report-label {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.6);
  margin-bottom: 4px;
}

.report-value {
  font-size: 16px;
  font-weight: 500;
  color: rgba(0, 0, 0, 0.87);
}

/* 侧边栏 */
.drawer-header {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
}

.drawer-header .text-caption {
  color: rgba(255, 255, 255, 0.8);
}

.stat-item {
  padding: 8px 0;
}

.stat-item .stat-label {
  font-size: 12px;
  color: rgba(0, 0, 0, 0.6);
}

.stat-item .stat-value {
  font-size: 14px;
  font-weight: 500;
}

.param-card {
  transition: transform 0.2s;
}

.log-text {
  margin: 0;
  font-size: 11px;
  max-height: 200px;
  overflow-y: auto;
  background: #f5f5f5;
  padding: 8px;
  border-radius: 4px;
}

/* 提示框内容 */
.tooltip-content {
  max-width: 300px;
}

.daily-scan-section {
  display: flex;
  flex-direction: column;
}

.v-checkbox {
  justify-content: flex-start;
}
</style>
