<script setup lang="ts">
import { ref, computed, onUnmounted } from 'vue'

interface ScanResult {
  files: string[]
  total: number
}

interface ApiResponse {
  success: boolean
  data?: ScanResult
  error?: string
}

interface ScanProgress {
  type: 'progress' | 'complete' | 'error'
  taskId?: number
  phase?: string
  scannedCount: number
  totalCount: number
  currentFile?: string
  elapsedMs: number
  success?: boolean
  errorMessage?: string
}

type ScanMode = 'normal' | 'stream' | 'atomic'

// 业务扫描模式
type BusinessScanMode = 'FULL_INVENTORY' | 'DAILY_CHECK' | 'TARGETED_SCAN' | ''

const directory = ref('')
const extensions = ref('.ts,.vue,.js')
const excludeDirs = ref('')
const workspace = ref('')
const scanMode = ref<ScanMode>('stream')
const businessScanMode = ref<BusinessScanMode>('')
const saveCode = ref('')
const scanning = ref(false)
const result = ref<ScanResult | null>(null)
const error = ref('')
const apiHost = ref('http://127.0.0.1:3001')

// 流式扫描进度状态
const progress = ref<ScanProgress | null>(null)
const eventSource = ref<EventSource | null>(null)

const hasResult = computed(() => result.value !== null || progress.value?.type === 'complete')
const progressPercent = computed(() => {
  if (!progress.value || progress.value.totalCount === 0) return 0
  return Math.round((progress.value.scannedCount / progress.value.totalCount) * 100)
})

// 是否需要显示安全操作码输入（重新首次普查时需要）
const showSaveCodeInput = computed(() => businessScanMode.value === 'FULL_INVENTORY')

// 是否需要 workspace（定点扫描必须）
const workspaceRequired = computed(() => businessScanMode.value === 'TARGETED_SCAN')

// 业务扫描模式是否启用（仅原子扫描模式支持）
const businessScanModeEnabled = computed(() => scanMode.value === 'atomic')

const elapsedTime = computed(() => {
  if (!progress.value) return '0s'
  const ms = progress.value.elapsedMs
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  const minutes = Math.floor(ms / 60000)
  const seconds = Math.floor((ms % 60000) / 1000)
  return `${minutes}m ${seconds}s`
})
const phaseText = computed(() => {
  if (!progress.value) return ''
  const phaseMap: Record<string, string> = {
    'counting': '正在统计文件数量...',
    'scanning': '正在扫描文件...',
    'completed': '扫描完成'
  }
  return phaseMap[progress.value.phase || ''] || progress.value.phase || ''
})

function closeEventSource() {
  if (eventSource.value) {
    eventSource.value.close()
    eventSource.value = null
  }
}

async function handleScan() {
  if (!directory.value.trim()) {
    error.value = '请输入扫描路径'
    return
  }

  if (!extensions.value.trim()) {
    error.value = '请输入文件后缀'
    return
  }

  // 定点扫描模式必须指定 workspace
  if (businessScanMode.value === 'TARGETED_SCAN' && !workspace.value.trim()) {
    error.value = '定点扫描模式必须指定工作空间目录'
    return
  }

  scanning.value = true
  error.value = ''
  result.value = null
  progress.value = null
  closeEventSource()

  if (scanMode.value === 'normal') {
    await handleNormalScan()
  } else {
    await handleStreamScan()
  }
}

async function handleNormalScan() {
  try {
    const params = new URLSearchParams({
      dir: directory.value.trim(),
      extensions: extensions.value.trim(),
      countOnly: 'false'
    })

    const response = await fetch(`${apiHost.value}/scan?${params}`)
    const data: ApiResponse = await response.json()

    if (data.success && data.data) {
      result.value = data.data
    } else {
      error.value = data.error ?? '扫描失败'
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '请求失败，请检查服务是否启动'
  } finally {
    scanning.value = false
  }
}

async function handleStreamScan() {
  const endpoint = scanMode.value === 'atomic' ? '/scan/atomic' : '/scan/stream'
  const params = new URLSearchParams({
    dir: directory.value.trim(),
    extensions: extensions.value.trim()
  })

  if (excludeDirs.value.trim()) {
    params.set('excludeDirs', excludeDirs.value.trim())
  }

  if (workspace.value.trim()) {
    params.set('workspace', workspace.value.trim())
  }

  // 原子扫描模式下传递业务扫描模式参数
  if (scanMode.value === 'atomic' && businessScanMode.value) {
    params.set('scan_mode', businessScanMode.value)

    // 首次普查需要传递安全操作码
    if (businessScanMode.value === 'FULL_INVENTORY' && saveCode.value.trim()) {
      params.set('save_code', saveCode.value.trim())
    }
  }

  const url = `${apiHost.value}${endpoint}?${params}`

  try {
    eventSource.value = new EventSource(url)

    eventSource.value.onmessage = (event) => {
      try {
        const data: ScanProgress = JSON.parse(event.data)
        progress.value = data

        if (data.type === 'complete') {
          scanning.value = false
          closeEventSource()
        } else if (data.type === 'error') {
          error.value = data.errorMessage || '扫描出错'
          scanning.value = false
          closeEventSource()
        }
      } catch (e) {
        console.error('解析 SSE 数据失败:', e)
      }
    }

    eventSource.value.onerror = () => {
      if (scanning.value) {
        error.value = '连接中断，请检查服务状态'
        scanning.value = false
      }
      closeEventSource()
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '请求失败，请检查服务是否启动'
    scanning.value = false
  }
}

function stopScan() {
  closeEventSource()
  scanning.value = false
}

function clearResult() {
  result.value = null
  progress.value = null
  error.value = ''
}

onUnmounted(() => {
  closeEventSource()
})
</script>

<template>
  <div class="scanner-container">
    <h1>文件扫描服务</h1>

    <div class="form-section">
      <div class="form-group">
        <label for="api-host">API 地址:</label>
        <input
          id="api-host"
          v-model="apiHost"
          type="text"
          placeholder="http://127.0.0.1:3001"
        />
      </div>

      <div class="form-group">
        <label for="directory">扫描路径:</label>
        <input
          id="directory"
          v-model="directory"
          type="text"
          placeholder="输入要扫描的目录路径，如 /Users/xxx/project"
          :disabled="scanning"
        />
      </div>

      <div class="form-group">
        <label for="extensions">文件后缀:</label>
        <input
          id="extensions"
          v-model="extensions"
          type="text"
          placeholder="逗号分隔，如 .ts,.vue,.js"
          :disabled="scanning"
        />
        <span class="hint">多个后缀用逗号分隔</span>
      </div>

      <div class="form-group">
        <label for="excludeDirs">排除目录:</label>
        <input
          id="excludeDirs"
          v-model="excludeDirs"
          type="text"
          placeholder="逗号分隔，如 dist,build,temp"
          :disabled="scanning"
        />
        <span class="hint">可选，多个目录用逗号分隔（仅流式扫描模式生效）</span>
      </div>

      <div class="form-group">
        <label for="workspace">
          工作空间目录:
          <span v-if="workspaceRequired" class="required-mark">*</span>
        </label>
        <input
          id="workspace"
          v-model="workspace"
          type="text"
          :placeholder="workspaceRequired ? '定点扫描必须指定工作空间目录' : '可选，工作空间目录路径'"
          :disabled="scanning"
          :class="{ 'required-field': workspaceRequired }"
        />
        <span class="hint">
          {{ workspaceRequired
            ? '定点扫描模式下必须指定工作空间目录，扫描将仅针对该目录进行'
            : '可选，工作空间目录的所有文件后缀将与扫描后缀合并（仅流式/原子扫描模式生效）'
          }}
        </span>
      </div>

      <div class="form-group">
        <label>扫描模式:</label>
        <div class="radio-group">
          <label class="radio-label">
            <input
              type="radio"
              v-model="scanMode"
              value="normal"
              :disabled="scanning"
            />
            <span>普通扫描</span>
          </label>
          <label class="radio-label">
            <input
              type="radio"
              v-model="scanMode"
              value="stream"
              :disabled="scanning"
            />
            <span>流式扫描</span>
          </label>
          <label class="radio-label">
            <input
              type="radio"
              v-model="scanMode"
              value="atomic"
              :disabled="scanning"
            />
            <span>原子扫描（入库）</span>
          </label>
        </div>
        <span class="hint">
          普通扫描：一次返回所有结果 | 流式扫描：实时显示进度 | 原子扫描：流式+数据入库
        </span>
      </div>

      <!-- 业务扫描模式（仅原子扫描模式可用） -->
      <div v-if="businessScanModeEnabled" class="form-group business-scan-mode">
        <label>业务扫描模式:</label>
        <div class="radio-group vertical">
          <label class="radio-label">
            <input
              type="radio"
              v-model="businessScanMode"
              value=""
              :disabled="scanning"
            />
            <span class="mode-option">
              <span class="mode-name">无</span>
              <span class="mode-desc">普通原子扫描，不启用特殊业务逻辑</span>
            </span>
          </label>
          <label class="radio-label">
            <input
              type="radio"
              v-model="businessScanMode"
              value="FULL_INVENTORY"
              :disabled="scanning"
            />
            <span class="mode-option">
              <span class="mode-name">首次普查</span>
              <span class="mode-desc">全面扫描并建立基线数据，重新普查需要安全操作码</span>
            </span>
          </label>
          <label class="radio-label">
            <input
              type="radio"
              v-model="businessScanMode"
              value="DAILY_CHECK"
              :disabled="scanning"
            />
            <span class="mode-option">
              <span class="mode-name">日常盘点</span>
              <span class="mode-desc">增量扫描并对比基线，需要先完成首次普查</span>
            </span>
          </label>
          <label class="radio-label">
            <input
              type="radio"
              v-model="businessScanMode"
              value="TARGETED_SCAN"
              :disabled="scanning"
            />
            <span class="mode-option">
              <span class="mode-name">定点扫描</span>
              <span class="mode-desc">仅扫描指定的工作空间目录（必须指定 workspace）</span>
            </span>
          </label>
        </div>
      </div>

      <!-- 安全操作码输入（首次普查时可选） -->
      <div v-if="showSaveCodeInput && businessScanModeEnabled" class="form-group">
        <label for="saveCode">安全操作码:</label>
        <input
          id="saveCode"
          v-model="saveCode"
          type="password"
          placeholder="重新首次普查时需要输入安全操作码"
          :disabled="scanning"
        />
        <span class="hint">如果是第一次进行首次普查，可以不填；重新首次普查时需要验证安全操作码</span>
      </div>

      <div class="button-group">
        <button
          class="scan-button"
          :disabled="scanning"
          @click="handleScan"
        >
          {{ scanning ? '扫描中...' : '开始扫描' }}
        </button>
        <button
          v-if="scanning"
          class="stop-button"
          @click="stopScan"
        >
          停止扫描
        </button>
        <button
          v-if="hasResult && !scanning"
          class="clear-button"
          @click="clearResult"
        >
          清除结果
        </button>
      </div>
    </div>

    <!-- 实时进度显示 -->
    <div v-if="progress && (scanMode === 'stream' || scanMode === 'atomic')" class="progress-section">
      <h2>扫描进度</h2>

      <div class="progress-stats">
        <div class="stat-item">
          <span class="stat-label">状态</span>
          <span class="stat-value" :class="{ 'status-complete': progress.type === 'complete' }">
            {{ phaseText }}
          </span>
        </div>
        <div class="stat-item">
          <span class="stat-label">已扫描文件</span>
          <span class="stat-value">{{ progress.scannedCount }} / {{ progress.totalCount }}</span>
        </div>
        <div class="stat-item">
          <span class="stat-label">已用时间</span>
          <span class="stat-value time-value">{{ elapsedTime }}</span>
        </div>
        <div v-if="progress.taskId" class="stat-item">
          <span class="stat-label">任务ID</span>
          <span class="stat-value">#{{ progress.taskId }}</span>
        </div>
      </div>

      <div class="progress-bar-container">
        <div class="progress-bar" :style="{ width: progressPercent + '%' }"></div>
        <span class="progress-text">{{ progressPercent }}%</span>
      </div>

      <div v-if="progress.currentFile" class="current-file">
        <span class="current-file-label">当前文件:</span>
        <span class="current-file-path">{{ progress.currentFile }}</span>
      </div>
    </div>

    <div v-if="error" class="error-message">
      {{ error }}
    </div>

    <!-- 普通扫描结果 -->
    <div v-if="result" class="result-section">
      <h2>扫描结果</h2>
      <div class="result-summary">
        共找到 <strong>{{ result.total }}</strong> 个文件
      </div>
      <ul v-if="result.files.length > 0" class="file-list">
        <li v-for="file in result.files" :key="file" class="file-item">
          {{ file }}
        </li>
      </ul>
      <div v-else-if="result.total > 0" class="count-only-notice">
        已启用仅统计数量模式，不返回文件列表
      </div>
    </div>

    <!-- 流式扫描完成结果 -->
    <div v-if="progress?.type === 'complete' && !result" class="result-section">
      <h2>扫描完成</h2>
      <div class="result-summary">
        共扫描 <strong>{{ progress.scannedCount }}</strong> 个文件，
        耗时 <strong>{{ elapsedTime }}</strong>
        <span v-if="progress.taskId">，任务ID: <strong>#{{ progress.taskId }}</strong></span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.scanner-container {
  max-width: 900px;
  margin: 0 auto;
  padding: 20px;
}

h1 {
  text-align: center;
  margin-bottom: 30px;
  color: #333;
}

h2 {
  margin-bottom: 15px;
  color: #444;
}

.form-section {
  background: #f9f9f9;
  padding: 20px;
  border-radius: 8px;
  margin-bottom: 20px;
}

.form-group {
  margin-bottom: 15px;
}

.form-group label {
  display: block;
  margin-bottom: 5px;
  font-weight: 500;
  color: #555;
}

.form-group input[type="text"] {
  width: 100%;
  padding: 10px;
  border: 1px solid #ddd;
  border-radius: 4px;
  font-size: 14px;
  box-sizing: border-box;
}

.form-group input:focus {
  outline: none;
  border-color: #4a9eff;
}

.form-group input:disabled {
  background: #eee;
}

.hint {
  display: block;
  margin-top: 5px;
  font-size: 12px;
  color: #888;
}

.radio-group {
  display: flex;
  gap: 20px;
  margin-top: 8px;
}

.radio-label {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  font-weight: normal;
}

.radio-label input[type="radio"] {
  width: 16px;
  height: 16px;
  cursor: pointer;
}

/* 业务扫描模式样式 */
.business-scan-mode {
  background: #f0f7ff;
  padding: 15px;
  border-radius: 6px;
  border: 1px solid #d0e5ff;
}

.radio-group.vertical {
  flex-direction: column;
  gap: 12px;
}

.radio-group.vertical .radio-label {
  align-items: flex-start;
}

.mode-option {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.mode-name {
  font-weight: 600;
  color: #333;
}

.mode-desc {
  font-size: 12px;
  color: #666;
}

.required-mark {
  color: #e74c3c;
  margin-left: 4px;
}

.required-field {
  border-color: #e74c3c !important;
}

.form-group input[type="password"] {
  width: 100%;
  padding: 10px;
  border: 1px solid #ddd;
  border-radius: 4px;
  font-size: 14px;
  box-sizing: border-box;
}

.form-group input[type="password"]:focus {
  outline: none;
  border-color: #4a9eff;
}

.form-group input[type="password"]:disabled {
  background: #eee;
}

.button-group {
  display: flex;
  gap: 10px;
  margin-top: 20px;
}

.scan-button {
  flex: 1;
  padding: 12px 20px;
  background: #4a9eff;
  color: white;
  border: none;
  border-radius: 4px;
  font-size: 16px;
  cursor: pointer;
  transition: background 0.2s;
}

.scan-button:hover:not(:disabled) {
  background: #3388ee;
}

.scan-button:disabled {
  background: #aaa;
  cursor: not-allowed;
}

.stop-button {
  padding: 12px 20px;
  background: #e74c3c;
  color: white;
  border: none;
  border-radius: 4px;
  font-size: 16px;
  cursor: pointer;
}

.stop-button:hover {
  background: #c0392b;
}

.clear-button {
  padding: 12px 20px;
  background: #666;
  color: white;
  border: none;
  border-radius: 4px;
  font-size: 16px;
  cursor: pointer;
}

.clear-button:hover {
  background: #555;
}

.error-message {
  background: #ffe0e0;
  color: #c00;
  padding: 12px;
  border-radius: 4px;
  margin-bottom: 20px;
}

/* 进度显示区域 */
.progress-section {
  background: #e8f4fd;
  padding: 20px;
  border-radius: 8px;
  margin-bottom: 20px;
  border: 1px solid #b3d7f5;
}

.progress-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 15px;
  margin-bottom: 20px;
}

.stat-item {
  background: white;
  padding: 12px;
  border-radius: 6px;
  text-align: center;
  box-shadow: 0 1px 3px rgba(0,0,0,0.1);
}

.stat-label {
  display: block;
  font-size: 12px;
  color: #666;
  margin-bottom: 4px;
}

.stat-value {
  font-size: 18px;
  font-weight: 600;
  color: #333;
}

.stat-value.status-complete {
  color: #27ae60;
}

.stat-value.time-value {
  font-family: 'Monaco', 'Menlo', monospace;
  color: #2980b9;
}

.progress-bar-container {
  position: relative;
  height: 24px;
  background: #ddd;
  border-radius: 12px;
  overflow: hidden;
  margin-bottom: 15px;
}

.progress-bar {
  height: 100%;
  background: linear-gradient(90deg, #4a9eff, #27ae60);
  transition: width 0.3s ease;
  border-radius: 12px;
}

.progress-text {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  font-size: 12px;
  font-weight: 600;
  color: #333;
}

.current-file {
  background: white;
  padding: 10px;
  border-radius: 4px;
  font-size: 13px;
  word-break: break-all;
}

.current-file-label {
  color: #666;
  margin-right: 8px;
}

.current-file-path {
  font-family: 'Monaco', 'Menlo', monospace;
  color: #555;
}

/* 结果区域 */
.result-section {
  background: #f0f8ff;
  padding: 20px;
  border-radius: 8px;
}

.result-summary {
  margin-bottom: 15px;
  font-size: 16px;
}

.file-list {
  list-style: none;
  padding: 0;
  margin: 0;
  max-height: 400px;
  overflow-y: auto;
  background: white;
  border-radius: 4px;
  border: 1px solid #ddd;
}

.file-item {
  padding: 8px 12px;
  border-bottom: 1px solid #eee;
  font-family: monospace;
  font-size: 13px;
  word-break: break-all;
}

.file-item:last-child {
  border-bottom: none;
}

.file-item:nth-child(even) {
  background: #fafafa;
}

.count-only-notice {
  padding: 12px;
  background: #fff8e6;
  border: 1px solid #ffe0a0;
  border-radius: 4px;
  color: #856404;
  font-size: 14px;
}
</style>
