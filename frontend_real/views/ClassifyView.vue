<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { api, type DataResource, type SystemConfig, type SingleClassifyParams, type ResourcesStatistics, type FileItem, type FamilyMembersResponse } from '@/services/api'
import { saveTabState, loadTabState } from '@/services/TabStateManager'
import { projectsApi, templatesApi, type DataProject, type FullTemplate } from '@/services/projectsApi'

// 选项卡类型
type TabType = 'workspace' | 'new_access' | 'history_inventory'

// 当前选中的选项卡
const activeTab = ref<TabType>('workspace')

// 系统配置
const config = ref<SystemConfig | null>(null)

// 归类保护统计数据
const classifyStats = ref<ResourcesStatistics | null>(null)

// 状态
const loading = ref(false)
const resources = ref<DataResource[]>([])
const search = ref('')
const page = ref(1)
const pageSize = ref(50)
const total = ref(0)
const importanceLevelFilter = ref(0) // -1 表示全部

// 提交状态
const submitting = ref(false)

// 归类保护弹窗状态
const classifyDialog = ref(false)
const classifyData = ref<SingleClassifyParams | null>(null)
const classifyLoading = ref(false)
const contentSignForOpenFile = ref<string | null>(null)

// 副本列表对话框状态
const showCopiesDialog = ref(false)
const currentCopies = ref<FileItem[]>([])

// V4-Q5 §4.3.5 家族批量归目对话框状态
// V5-P1 Task 10: 扩展为双目标（过程目标 + 可选定稿目标），若两者齐全则后端按"最新→定稿，其余→过程"分流
const familyArchiveDialog = ref<{
  open: boolean
  busy: boolean
  projectId: number | null
  stageCode: string         // 过程目标 stage
  fileRuleCode: string      // 过程目标 rule
  finalStageCode: string    // V5-P1 Task 10: 定稿目标 stage（可选）
  finalFileRuleCode: string // V5-P1 Task 10: 定稿目标 rule（可选）
  result: {
    total: number
    archived: number
    skipped_already: number
    errors: number
  } | null
}>({
  open: false, busy: false,
  projectId: null, stageCode: '', fileRuleCode: '',
  finalStageCode: '', finalFileRuleCode: '',
  result: null,
})

// 选项数据
const allProjects = ref<DataProject[]>([])
const currentTemplate = ref<FullTemplate | null>(null)
const archiveProjectOptions = computed(() =>
  allProjects.value
    .filter(p => p.status === 'active' || p.status === 'draft')
    .map(p => ({ title: `${p.project_code} ${p.project_name}`, value: p.id }))
)
const archiveStageOptions = computed(() =>
  (currentTemplate.value?.stages || []).map(s => ({
    title: `${s.stage_code} ${s.stage_name}`, value: s.stage_code,
  }))
)
const archiveRuleOptions = computed(() => {
  const stage = (currentTemplate.value?.stages || []).find(s => s.stage_code === familyArchiveDialog.value.stageCode)
  return (stage?.file_rules || []).map(r => ({
    title: `${r.file_rule_code} ${r.file_name} (${r.data_state})`,
    value: r.file_rule_code,
  }))
})
// V5-P1 Task 10: 定稿目标的规则选项（基于 finalStageCode 过滤）
const finalRuleOptions = computed(() => {
  const stage = (currentTemplate.value?.stages || []).find(s => s.stage_code === familyArchiveDialog.value.finalStageCode)
  return (stage?.file_rules || []).map(r => ({
    title: `${r.file_rule_code} ${r.file_name} (${r.data_state})`,
    value: r.file_rule_code,
  }))
})

async function openFamilyArchiveDialog() {
  familyArchiveDialog.value = {
    open: true, busy: false,
    projectId: null, stageCode: '', fileRuleCode: '',
    finalStageCode: '', finalFileRuleCode: '',
    result: null,
  }
  // 拉所有 active 归目目标/项目
  try {
    allProjects.value = await projectsApi.list()
  } catch (e: any) {
    showSnackbar('加载归目目标列表失败：' + e.message, 'error')
  }
}

async function onArchiveProjectChange() {
  familyArchiveDialog.value.stageCode = ''
  familyArchiveDialog.value.fileRuleCode = ''
  familyArchiveDialog.value.finalStageCode = ''
  familyArchiveDialog.value.finalFileRuleCode = ''
  currentTemplate.value = null
  if (!familyArchiveDialog.value.projectId) return
  const proj = allProjects.value.find(p => p.id === familyArchiveDialog.value.projectId)
  if (!proj) return
  // 用 templatesApi.list 找模版 id，再 get 完整结构
  try {
    const tpls = await templatesApi.list()
    const tpl = tpls.find(t =>
      t.template_code === proj.template_code && t.template_version === proj.template_version
    )
    if (tpl) {
      currentTemplate.value = await templatesApi.get(tpl.id)
    }
  } catch (e: any) {
    showSnackbar('加载模版结构失败：' + e.message, 'error')
  }
}

function onArchiveStageChange() {
  familyArchiveDialog.value.fileRuleCode = ''
}

// V5-P1 Task 10: 定稿环节变化时清空定稿规则
function onFamilyFinalStageChange() {
  familyArchiveDialog.value.finalFileRuleCode = ''
}

// V5-P1 Task 10: 是否进入分流模式（过程 + 定稿两个目标齐全）
const isSplitArchiveMode = computed(() =>
  !!(familyArchiveDialog.value.finalStageCode && familyArchiveDialog.value.finalFileRuleCode)
)

async function doFamilyArchive() {
  if (!currentFamily.value || !familyArchiveDialog.value.projectId) return
  familyArchiveDialog.value.busy = true
  try {
    const payload: Record<string, any> = {
      project_id: familyArchiveDialog.value.projectId,
      stage_code: familyArchiveDialog.value.stageCode,
      file_rule_code: familyArchiveDialog.value.fileRuleCode,
    }
    // V5-P1 Task 10: 只有两个字段都填齐才传定稿目标，触发后端 split 模式
    if (familyArchiveDialog.value.finalStageCode && familyArchiveDialog.value.finalFileRuleCode) {
      payload.final_stage_code = familyArchiveDialog.value.finalStageCode
      payload.final_file_rule_code = familyArchiveDialog.value.finalFileRuleCode
    }
    const res = await fetch(`http://127.0.0.1:3001/resources/families/${currentFamily.value.family_id}/batch-archive`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || '归目失败')
    familyArchiveDialog.value.result = json.data
    showSnackbar(
      `批量归目完成：新挂账 ${json.data.archived} · 跳过 ${json.data.skipped_already} · 错误 ${json.data.errors}`,
      json.data.errors > 0 ? 'warning' : 'success'
    )
  } catch (e: any) {
    showSnackbar('归目失败：' + e.message, 'error')
  } finally {
    familyArchiveDialog.value.busy = false
  }
}
const copiesLoading = ref(false)
const currentResourceName = ref('')
const currentFamily = ref<FamilyMembersResponse | null>(null)

// 认领分类选项（用于显示）
const claimStatusOptions = [
  { value: 0, text: '未认领', color: 'grey' },
  { value: 1, text: '个人隐私', color: 'error' },
  { value: 2, text: '个人工作', color: 'primary' },
  { value: 3, text: '非责任类', color: 'warning' }
]

// 工作事项历史记录相关
const HISTORY_STORAGE_KEY = 'classify_folder_history'
const folderHistory = ref<string[]>([])

// 读取历史记录
const loadFolderHistory = () => {
  try {
    const saved = localStorage.getItem(HISTORY_STORAGE_KEY)
    if (saved) {
      folderHistory.value = JSON.parse(saved)
    }
  } catch (error) {
    console.error('Failed to load folder history:', error)
    folderHistory.value = []
  }
}

// 保存历史记录
const saveFolderHistory = (folder: string) => {
  if (!folder || folder.trim() === '') return

  const trimmedFolder = folder.trim()
  // 移除重复项并添加到开头
  const newHistory = [trimmedFolder, ...folderHistory.value.filter(f => f !== trimmedFolder)]
  // 只保留最近10条
  folderHistory.value = newHistory.slice(0, 10)

  try {
    localStorage.setItem(HISTORY_STORAGE_KEY, JSON.stringify(folderHistory.value))
  } catch (error) {
    console.error('Failed to save folder history:', error)
  }
}

// 重要程度选项
const importanceLevelOptions = [
  { value: 0, text: '未保护', color: 'grey' },
  { value: 1, text: '核心级标识', color: 'error' },
  { value: 2, text: '重要级标识', color: 'warning' },
  { value: 3, text: '一般级标识', color: 'success' },
  { value: 5, text: '不予归目', color: 'grey' },
]

// 归类保护方式选项（用于弹窗选择）
const protectionMethodOptions = [
  { value: 1, label: '核心级', subtitle: '标识归目，不迁移文件', color: '#e53935' },
  { value: 2, label: '重要级', subtitle: '标识归目，不迁移文件', color: '#f57c00' },
  { value: 3, label: '一般级', subtitle: '标识归目，不迁移文件', color: '#43a047' },
  { value: 5, label: '不予归目', subtitle: '不纳入个人文件账', color: '#757575' },
]

// 过滤选项（含全部）
const filterOptions = [
  { value: -1, text: '全部' },
  ...importanceLevelOptions
]

// Snackbar状态
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

// 表格列配置
const headers = [
  { title: '资源名称', key: 'resources_name', sortable: true },
  { title: '最早创建时间', key: 'first_create_time', sortable: true, width: '180px' },
  { title: '分布数量', key: 'source_count', sortable: true, width: '100px' },
  { title: '认领状态', key: 'claim_status', sortable: true, width: '120px' },
  { title: '保护方式', key: 'importance_level', sortable: true, width: '180px' },
]

// 副本列表表格列配置
const copiesHeaders = [
  { title: '文件路径', key: 'path', sortable: false },
  { title: '文件大小', key: 'file_size', sortable: false, width: '100px' },
]

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

// 格式化文件大小
const formatFileSize = (bytes: number) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

// 获取认领状态文本
const getClaimStatusText = (status: number) => {
  const option = claimStatusOptions.find(o => o.value === status)
  return option?.text || '未知'
}

// 获取认领状态颜色
const getClaimStatusColor = (status: number) => {
  const option = claimStatusOptions.find(o => o.value === status)
  return option?.color || 'grey'
}

// 获取重要程度文本
const getImportanceLevelText = (level: number) => {
  const option = importanceLevelOptions.find(o => o.value === level)
  return option?.text || '未知'
}

// 获取重要程度颜色
const getImportanceLevelColor = (level: number) => {
  const option = importanceLevelOptions.find(o => o.value === level)
  return option?.color || 'grey'
}

// 计算总页数
const totalPages = computed(() => {
  return Math.ceil(total.value / pageSize.value)
})

// 归类保护统计数据
const workspacePendingClassifyCount = computed(() => classifyStats.value?.workspacePendingClassifyCount ?? 0)
const historyPendingClassifyCount = computed(() => classifyStats.value?.historyPendingClassifyCount ?? 0)
const nonHistoryPendingClassifyCount = computed(() => classifyStats.value?.nonHistoryPendingClassifyCount ?? 0)

// 打开归类保护弹窗
const openClassifyDialog = (item: DataResource) => {
  const currentFolder = item.content_subject || ''
  // 如果当前有工作事项值则使用当前值，否则使用最近的历史记录
  const defaultFolder = currentFolder || (folderHistory.value.length > 0 ? folderHistory.value[0] : '')
  classifyData.value = {
    data_resources_id: item.data_resources_id,
    importance_level: item.importance_level || 0,
    resources_name: item.resources_name || '',
    resources_desc: item.resources_desc || '',
    content_subject: defaultFolder
  }
  contentSignForOpenFile.value = item.content_sign
  classifyDialog.value = true
}

// 关闭归类保护弹窗
const closeClassifyDialog = () => {
  classifyDialog.value = false
  classifyData.value = null
  contentSignForOpenFile.value = null
}

// 选择保护方式
const selectProtectionMethod = (value: number) => {
  if (classifyData.value) {
    classifyData.value.importance_level = value
  }
}

// 查看副本 / 家族成员列表
const handleViewCopies = async (item: DataResource) => {
  if (!item.content_sign) {
    showSnackbar('文件内容签名不存在', 'warning')
    return
  }

  copiesLoading.value = true
  currentResourceName.value = item.resources_name || '未命名资源'
  currentCopies.value = []
  currentFamily.value = null
  showCopiesDialog.value = true

  try {
    if (item.family_id) {
      currentFamily.value = await api.getFamilyMembers(item.family_id)
    } else {
      const result = await api.getCopies(item.content_sign)
      currentCopies.value = result.copies
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : '获取副本列表失败'
    showSnackbar(message, 'error')
    currentCopies.value = []
    currentFamily.value = null
  } finally {
    copiesLoading.value = false
  }
}

// 确认归类保护
const confirmClassify = async () => {
  if (!classifyData.value) return

  // 验证是否已选择保护方式（1,2,3,5 都是有效值）
  if (!classifyData.value.importance_level || classifyData.value.importance_level === 0) {
    showSnackbar('请选择保护方式', 'warning')
    return
  }

  // 不予归目时不需要工作事项
  if (classifyData.value.importance_level === 5) {
    classifyData.value.content_subject = ''
  }

  classifyLoading.value = true
  try {
    const result = await api.singleClassify(classifyData.value)
    showSnackbar(result.message || '归类保护成功', 'success')
    // 保存工作事项历史记录（仅限归目操作）
    if (classifyData.value.content_subject && classifyData.value.importance_level !== 5) {
      saveFolderHistory(classifyData.value.content_subject)
    }
    // 刷新列表
    await loadResources()
    // 刷新统计数据
    await loadClassifyStats()
    // 关闭弹窗
    closeClassifyDialog()
  } catch (error) {
    const message = error instanceof Error ? error.message : '归类失败'
    showSnackbar(message, 'error')
  } finally {
    classifyLoading.value = false
  }
}

// 查看文件
const handleViewFile = async () => {
  if (!contentSignForOpenFile.value) return

  try {
    await api.openFile(contentSignForOpenFile.value)
    showSnackbar('文件已打开', 'success')
  } catch (error) {
    const message = error instanceof Error ? error.message : '打开文件失败'
    showSnackbar(message, 'error')
  }
}

// 加载配置
const loadConfig = async () => {
  try {
    config.value = await api.getConfig()
  } catch (error) {
    console.error('Failed to load config:', error)
  }
}

// 加载归类保护统计数据
const loadClassifyStats = async () => {
  try {
    classifyStats.value = await api.getResourcesStatistics()
  } catch (error) {
    console.error('Failed to load classify stats:', error)
  }
}

// 加载资源列表
const loadResources = async () => {
  loading.value = true
  try {
    const result = await api.getResources({
      search: search.value || undefined,
      page: page.value,
      pageSize: pageSize.value,
      importanceLevelFilter: importanceLevelFilter.value,
      businessTypeFilter: activeTab.value,
      claimStatusIn: [2]  // 只显示个人工作数据 (claim_status=2)
    })
    resources.value = result.resources
    total.value = result.total
  } catch (error) {
    console.error('Failed to load resources:', error)
    resources.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

// 显示提示
const showSnackbar = (text: string, color: string) => {
  snackbarText.value = text
  snackbarColor.value = color
  snackbar.value = true
}

// 监听选项卡变化，保存状态
watch(activeTab, (newTab) => {
  saveTabState(newTab)
})

// 监听搜索和过滤条件变化
watch([search, importanceLevelFilter, activeTab], () => {
  page.value = 1
  loadResources()
})

// 监听分页变化
watch([page, pageSize], () => {
  loadResources()
})

// 组件挂载时加载数据
onMounted(async () => {
  // 加载工作事项历史记录
  loadFolderHistory()
  // 加载选项卡状态
  const savedTab = loadTabState()
  if (savedTab && ['workspace', 'new_access', 'history_inventory'].includes(savedTab)) {
    activeTab.value = savedTab as TabType
  }
  loadConfig()
  loadClassifyStats()
  await loadResources()
})
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-folder-lock</v-icon>
        归类保护
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          对已认领的个人工作信息资源进行分级保护
        </div>
      </v-card-text>
    </v-card>

    <!-- 选项卡 -->
    <div class="d-flex align-center mb-4">
      <v-tabs v-model="activeTab" color="primary">
        <v-tab value="workspace">
          工作文件档案管理
          <v-chip
            v-if="workspacePendingClassifyCount > 0"
            size="small"
            color="error"
            class="ml-2"
          >
            {{ workspacePendingClassifyCount }}
          </v-chip>
        </v-tab>
        <v-tab value="new_access">
          新数据登记管理
          <v-chip
            v-if="nonHistoryPendingClassifyCount > 0"
            size="small"
            color="error"
            class="ml-2"
          >
            {{ nonHistoryPendingClassifyCount }}
          </v-chip>
        </v-tab>
        <v-tab value="history_inventory">
          历史数据专项治理
          <v-chip
            v-if="historyPendingClassifyCount > 0"
            size="small"
            color="error"
            class="ml-2"
          >
            {{ historyPendingClassifyCount }}
          </v-chip>
        </v-tab>
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

    <!-- 搜索和过滤栏 -->
    <v-card class="mb-4" elevation="1">
      <v-card-text>
        <v-row align="center">
          <v-col cols="12" md="4">
            <v-text-field
              v-model="search"
              prepend-inner-icon="mdi-magnify"
              label="搜索资源名称"
              variant="outlined"
              density="compact"
              hide-details
              clearable
            />
          </v-col>
          <v-col cols="12" md="3">
            <v-select
              v-model="importanceLevelFilter"
              :items="filterOptions"
              item-value="value"
              item-title="text"
              label="重要程度过滤"
              variant="outlined"
              density="compact"
              hide-details
            />
          </v-col>
          <v-col cols="12" md="5" class="d-flex align-center justify-end">
            <span class="text-body-2 text-grey">
              共 {{ total }} 条记录
            </span>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <!-- 资源表格 -->
    <v-card elevation="1">
      <v-data-table
        :headers="headers"
        :items="resources"
        :loading="loading"
        item-value="data_resources_id"
        :items-per-page="pageSize"
        hide-default-footer
      >
        <template v-slot:item.resources_name="{ item }">
          <div class="d-flex align-center">
            <v-icon size="small" class="mr-2" color="grey">
              mdi-file-document-outline
            </v-icon>
            <div class="text-truncate" style="max-width: 300px" :title="item.resources_name || '-'">
              {{ item.resources_name || '-' }}
            </div>
          </div>
        </template>

        <template v-slot:item.first_create_time="{ item }">
          {{ formatDate(item.first_create_time) }}
        </template>

        <template v-slot:item.source_count="{ item }">
          <v-chip
            size="small"
            variant="tonal"
            color="info"
            class="cursor-pointer"
            @click="handleViewCopies(item)"
          >
            {{ item.source_count }}
          </v-chip>
        </template>

        <template v-slot:item.claim_status="{ item }">
          <v-chip
            size="small"
            variant="tonal"
            :color="getClaimStatusColor(item.claim_status)"
          >
            {{ getClaimStatusText(item.claim_status) }}
          </v-chip>
        </template>

        <template v-slot:item.importance_level="{ item }">
          <div class="d-flex align-center gap-2">
            <v-chip
              v-if="item.importance_level === 0"
              size="small"
              variant="tonal"
              color="grey"
            >
              未保护
            </v-chip>
            <v-chip
              v-else
              size="small"
              variant="tonal"
              :color="getImportanceLevelColor(item.importance_level)"
            >
              {{ getImportanceLevelText(item.importance_level) }}
            </v-chip>
            <v-btn
              size="small"
              color="primary"
              variant="text"
              density="compact"
              @click="openClassifyDialog(item)"
            >
              <v-icon size="small" class="mr-1">mdi-shield-check</v-icon>
              归类保护
            </v-btn>
          </div>
        </template>

        <template v-slot:no-data>
          <div class="text-center py-8">
            <v-icon size="64" color="grey-lighten-1">mdi-folder-open-outline</v-icon>
            <div class="mt-4 text-grey">暂无资源数据</div>
            <div class="mt-2 text-caption text-grey">请先进行责任认领</div>
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

    <!-- 归目保护弹窗 -->
    <v-dialog v-model="classifyDialog" max-width="600px" persistent>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-shield-check</v-icon>
          文件归目登记表(本地)
          <v-spacer />
          <v-btn icon="mdi-close" variant="text" @click="closeClassifyDialog" />
        </v-card-title>

        <v-card-text class="pt-4" v-if="classifyData">
          <!-- 保护方式选择 -->
          <div class="mb-6">
            <div class="text-subtitle-2 mb-3">归目级别</div>
            <v-row>
              <v-col v-for="option in protectionMethodOptions" :key="option.value" cols="6">
                <v-card
                  :class="[
                    'pa-3 cursor-pointer border',
                    classifyData?.importance_level === option.value ? 'selected-card' : 'unselected-card'
                  ]"
                  :style="{
                    borderColor: classifyData?.importance_level === option.value ? option.color : '#e0e0e0',
                    backgroundColor: classifyData?.importance_level === option.value ? `${option.color}15` : 'white'
                  }"
                  elevation="2"
                  @click="selectProtectionMethod(option.value)"
                >
                  <div class="d-flex flex-column align-center text-center">
                    <v-icon
                      size="32"
                      :color="classifyData?.importance_level === option.value ? option.color : 'grey'"
                      class="mb-2"
                    >
                      {{ option.value === 1 ? 'mdi-lock' : option.value === 2 ? 'mdi-archive' : option.value === 3 ? 'mdi-folder' : 'mdi-cancel' }}
                    </v-icon>
                    <div class="text-body-1 font-weight-medium">{{ option.label }}</div>
                    <div class="text-caption text-grey mt-1">{{ option.subtitle }}</div>
                  </div>
                </v-card>
              </v-col>
            </v-row>
          </div>

          <!-- 资源信息表单 -->
          <v-form>
            <v-text-field
              v-model="classifyData!.resources_name"
              label="资源标题"
              variant="outlined"
              density="compact"
              prepend-inner-icon="mdi-file-document"
              class="mb-2"
            />
            <v-textarea
              v-model="classifyData!.resources_desc"
              label="内容摘要"
              variant="outlined"
              density="compact"
              rows="3"
              prepend-inner-icon="mdi-text"
              class="mb-2"
            />
            <v-combobox
              v-model="classifyData!.content_subject"
              :items="folderHistory"
              label="工作事项 / 主题"
              chips
              variant="outlined"
              density="compact"
              prepend-inner-icon="mdi-tag"
              clearable
              hide-no-data
              :menu-props="{ maxHeight: 300 }"
            />
          </v-form>
        </v-card-text>

        <v-card-actions class="pt-0">
          <v-spacer />
          <v-btn
            color="grey"
            variant="text"
            prepend-icon="mdi-eye-outline"
            @click="handleViewFile"
            :disabled="!contentSignForOpenFile"
          >
            查看文件
          </v-btn>
          <v-btn
            color="primary"
            variant="flat"
            prepend-icon="mdi-check"
            :loading="classifyLoading"
            :disabled="classifyLoading"
            @click="confirmClassify"
          >
            确认归目
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 提示消息 -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" :timeout="3000">
      {{ snackbarText }}
    </v-snackbar>

    <!-- 副本 / 家族成员对话框 -->
    <v-dialog v-model="showCopiesDialog" max-width="900px">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">{{ currentFamily ? 'mdi-graph' : 'mdi-content-copy' }}</v-icon>
          {{ currentFamily ? '家族成员' : '副本列表' }}
          <v-chip v-if="currentFamily" class="ml-2" size="x-small" color="primary" variant="tonal">
            family #{{ currentFamily.family_id }} · {{ currentFamily.total_members }} 成员
          </v-chip>
        </v-card-title>
        <v-card-subtitle>资源：{{ currentResourceName }}</v-card-subtitle>
        <v-divider />
        <v-card-text class="pa-0" style="max-height: 60vh; overflow-y: auto;">
          <template v-if="currentFamily">
            <template v-for="group in [
              { key: 'primary', title: '主资源', color: 'primary', icon: 'mdi-star' },
              { key: 'same_content', title: '完全相同（same_content）', color: 'success', icon: 'mdi-equal' },
              { key: 'process_version', title: '流程版本（process_version）', color: 'warning', icon: 'mdi-file-document-multiple-outline' },
              { key: 'derived', title: '衍生文件（derived）', color: 'info', icon: 'mdi-file-tree' }
            ]" :key="group.key">
              <div v-if="(currentFamily.groups[group.key] || []).length > 0">
                <div class="px-4 py-2 d-flex align-center bg-grey-lighten-4">
                  <v-icon :color="group.color" size="small" class="mr-2">{{ group.icon }}</v-icon>
                  <span class="text-body-2 font-weight-medium">{{ group.title }}</span>
                  <v-chip size="x-small" :color="group.color" variant="tonal" class="ml-2">
                    {{ (currentFamily.groups[group.key] || []).length }}
                  </v-chip>
                </div>
                <v-list density="compact" class="py-0">
                  <v-list-item
                    v-for="m in (currentFamily.groups[group.key] || [])"
                    :key="m.data_resources_id"
                  >
                    <template v-slot:prepend>
                      <v-icon size="small" color="grey">mdi-file-document-outline</v-icon>
                    </template>
                    <v-list-item-title class="text-body-2">
                      {{ m.resources_name || '-' }}
                    </v-list-item-title>
                    <v-list-item-subtitle class="text-caption">
                      分布 {{ m.source_count }} 份
                      <span v-if="m.family_score != null" class="ml-3">
                        相似度 {{ (m.family_score * 100).toFixed(1) }}%
                      </span>
                    </v-list-item-subtitle>
                  </v-list-item>
                </v-list>
                <v-divider />
              </div>
            </template>
          </template>

          <v-data-table
            v-else
            :headers="copiesHeaders"
            :items="currentCopies"
            :loading="copiesLoading"
            item-value="data_distribution_id"
            hide-default-footer
            density="compact"
          >
            <template v-slot:item.path="{ item }">
              {{ item.path }}
            </template>

            <template v-slot:item.file_size="{ item }">
              {{ formatFileSize(item.file_size) }}
            </template>

            <template v-slot:no-data>
              <div class="text-center py-4 text-grey">
                暂无副本数据
              </div>
            </template>
          </v-data-table>
        </v-card-text>
        <v-divider />
        <v-card-actions>
          <!-- V4-Q5 family 批量归目入口 -->
          <v-btn
            v-if="currentFamily"
            color="success"
            variant="tonal"
            prepend-icon="mdi-folder-multiple-plus-outline"
            @click="openFamilyArchiveDialog"
          >
            批量归目挂账
          </v-btn>
          <v-spacer />
          <v-btn color="primary" variant="text" @click="showCopiesDialog = false">
            关闭
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- V4-Q5 §4.3.5 家族批量归目对话框 -->
    <v-dialog v-model="familyArchiveDialog.open" max-width="640">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="success">mdi-folder-multiple-plus-outline</v-icon>
          家族批量归目
          <v-spacer />
          <v-btn icon variant="text" @click="familyArchiveDialog.open = false"><v-icon>mdi-close</v-icon></v-btn>
        </v-card-title>
        <v-divider />
        <v-card-text>
          <div class="text-caption text-medium-emphasis mb-3">
            把整个家族（{{ currentFamily?.total_members || 0 }} 个成员）批量挂账到指定归目容器或项目的某个环节文件规则下。
            已挂账过的成员会自动跳过（幂等）。
          </div>

          <v-select
            v-model="familyArchiveDialog.projectId"
            :items="archiveProjectOptions"
            label="归目目标 *"
            variant="outlined"
            density="compact"
            hide-details
            class="mb-3"
            @update:model-value="onArchiveProjectChange"
          />

          <!-- 过程目标（必填，承接历史 stage/rule 字段语义） -->
          <div class="text-subtitle-2 mb-2">过程目标 *（家族其余成员归入此处）</div>

          <v-select
            v-model="familyArchiveDialog.stageCode"
            :items="archiveStageOptions"
            label="过程环节 *"
            variant="outlined"
            density="compact"
            hide-details
            class="mb-3"
            :disabled="!familyArchiveDialog.projectId"
            @update:model-value="onArchiveStageChange"
          />

          <v-select
            v-model="familyArchiveDialog.fileRuleCode"
            :items="archiveRuleOptions"
            label="过程文件规则 *"
            variant="outlined"
            density="compact"
            hide-details
            class="mb-3"
            :disabled="!familyArchiveDialog.stageCode"
          />

          <!-- V5-P1 Task 10: 定稿目标（可选；提供则触发分流） -->
          <v-divider class="my-3" />
          <div class="text-subtitle-2 mb-2 d-flex align-center">
            定稿目标（可选）
            <v-tooltip text="若提供：家族内最新的文件归入此目标，其余归入上方过程目标" location="top">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="x-small" class="ml-1">mdi-help-circle-outline</v-icon>
              </template>
            </v-tooltip>
          </div>
          <v-alert
            v-if="!familyArchiveDialog.finalStageCode && !familyArchiveDialog.finalFileRuleCode"
            type="info" variant="tonal" density="compact" class="mb-2"
          >
            不填即按"单目标"归目：所有成员归入上方过程目标。
          </v-alert>
          <v-select
            v-model="familyArchiveDialog.finalStageCode"
            :items="archiveStageOptions"
            label="定稿环节（最新成员）"
            variant="outlined"
            density="compact"
            hide-details
            class="mb-2"
            :disabled="!familyArchiveDialog.projectId"
            clearable
            @update:model-value="onFamilyFinalStageChange"
          />
          <v-select
            v-model="familyArchiveDialog.finalFileRuleCode"
            :items="finalRuleOptions"
            label="定稿文件规则"
            variant="outlined"
            density="compact"
            hide-details
            class="mb-2"
            :disabled="!familyArchiveDialog.finalStageCode"
            clearable
          />

          <v-alert
            v-if="familyArchiveDialog.result"
            :type="familyArchiveDialog.result.errors > 0 ? 'warning' : 'success'"
            variant="tonal"
            density="compact"
            class="mt-3"
          >
            <div class="text-body-2">
              结果：总计 {{ familyArchiveDialog.result.total }} · 新挂账
              <strong>{{ familyArchiveDialog.result.archived }}</strong> · 已存在跳过
              {{ familyArchiveDialog.result.skipped_already }} · 错误
              {{ familyArchiveDialog.result.errors }}
            </div>
          </v-alert>
        </v-card-text>
        <v-divider />
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="familyArchiveDialog.open = false">取消</v-btn>
          <v-btn
            color="success"
            :loading="familyArchiveDialog.busy"
            :disabled="!familyArchiveDialog.projectId || !familyArchiveDialog.stageCode || !familyArchiveDialog.fileRuleCode"
            @click="doFamilyArchive"
          >
            {{ isSplitArchiveMode ? '确认分流归目' : '确认批量归目' }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}

.selected-card {
  border: 2px solid !important;
}

.unselected-card {
  border: 1px solid #e0e0e0;
}

.unselected-card:hover {
  border-color: #bdbdbd;
}

.gap-2 {
  gap: 8px;
}

.cursor-pointer {
  cursor: pointer;
}

.cursor-pointer:hover {
  opacity: 0.8;
}
</style>
