<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { api, type DataResource, type SystemConfig, type ResourcesStatistics, type FileItem,
         type SimilarityTask, type FamilyMembersResponse, type FamilyMemberDetail } from '@/services/api'
import { userInfoManager } from '@/services/UserInfoManager'
import RebuildSimilarityDialog from '../components/RebuildSimilarityDialog.vue'
import ClaimFamilyDialogSingle from '../components/ClaimFamilyDialogSingle.vue'
import ClaimFamilyDialogBatch from '../components/ClaimFamilyDialogBatch.vue'

// 选项卡类型
//   workspace        = 工作文件档案管理（工作空间内）
//   new_access       = 新数据登记管理（首次普查后新登记）
//   history_inventory= 历史数据专项治理（首次普查前）
// 与 FilesView 三个 tab 同源，由后端 businessTypeFilter 过滤 data_resources
type TabType = 'workspace' | 'new_access' | 'history_inventory'
type FamilyGroupKey = 'all' | 'primary' | 'same_content' | 'process_version' | 'derived'

// ClaimView 独立 tab 状态，避免与 FilesView 共用 TabStateManager 互相覆盖
const CLAIM_TAB_STORAGE_KEY = 'claim_view_active_tab'
const activeTab = ref<TabType>('workspace')

// 系统配置
const config = ref<SystemConfig | null>(null)

// 统计数据
const statistics = ref<ResourcesStatistics | null>(null)

// 状态
const loading = ref(false)
const resources = ref<DataResource[]>([])
const search = ref('')
const page = ref(1)
const pageSize = ref(50)
const total = ref(0)
const claimStatusFilter = ref(0) // -1 表示全部

// 选中项
const selectedIds = ref<number[]>([])

// 提交状态
const submitting = ref(false)

// 打开文件状态
const openingFile = ref(false)

// 副本列表对话框状态
const showCopiesDialog = ref(false)
const currentCopies = ref<FileItem[]>([])
const copiesLoading = ref(false)
const currentResourceName = ref('')
const currentDialogMode = ref<'copies' | 'family'>('copies')

// 家族成员对话框状态（当资源属于某家族时使用）
const currentFamily = ref<FamilyMembersResponse | null>(null)
const currentFamilyGroup = ref<FamilyGroupKey>('all')

// 相似度分析任务状态
const analysisTask = ref<SimilarityTask | null>(null)
const analysisStarting = ref(false)

// 重建相似关系对话框
const showRebuildDialog = ref(false)
let analysisPollTimer: ReturnType<typeof setInterval> | null = null

// 认领弹窗路由状态
const singleDialogOpen = ref(false)
const batchDialogOpen = ref(false)
const dialogPrimary = ref<FamilyMemberDetail | null>(null)
const dialogMembers = ref<FamilyMemberDetail[]>([])
const dialogBatchPrimaries = ref<FamilyMemberDetail[]>([])
const dialogBatchFamilyMap = ref<Record<string, FamilyMemberDetail[]>>({})
const dialogClaimStatus = ref(0)

// 认领分类选项
// 注意：value=4「已忽略」不放进认领下拉里（不让用户手动选「忽略」一份个人文件），
// 只通过顶部「一键忽略疑似非个人文件」入口产生。但 filterOptions 包含它，方便用户回看/恢复。
const claimStatusOptions = [
  { value: 0, text: '未分类', color: 'grey' },
  { value: 1, text: '个人隐私', color: 'error' },
  { value: 2, text: '个人工作', color: 'primary' },
  { value: 3, text: '非责任类', color: 'warning' },
]

// 已忽略：仅作为过滤项 + 行级展示用
const claimStatusIgnored = { value: 4, text: '已忽略', color: 'grey-darken-1' }

// 过滤选项（含全部 + 已忽略）
const filterOptions = [
  { value: -1, text: '全部' },
  ...claimStatusOptions,
  claimStatusIgnored,
]

// Snackbar状态
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

const familyGroupDefs: Array<{ key: FamilyGroupKey; title: string; shortTitle: string; color: string; icon: string }> = [
  { key: 'primary', title: '主资源', shortTitle: '主资源', color: 'primary', icon: 'mdi-star' },
  { key: 'same_content', title: '相同文件', shortTitle: '相同', color: 'success', icon: 'mdi-equal' },
  { key: 'process_version', title: '过程文件', shortTitle: '过程', color: 'warning', icon: 'mdi-file-document-multiple-outline' },
  { key: 'derived', title: '衍生文件', shortTitle: '衍生', color: 'info', icon: 'mdi-file-tree' },
]

// 表格列配置
const headers = computed(() => [
  { title: '资源名称', key: 'resources_name', sortable: true },
  { title: '最早创建时间', key: 'first_create_time', sortable: true, width: '180px' },
  { title: '相同文件', key: 'family_same_content_count', sortable: true, width: '100px' },
  { title: '过程文件', key: 'family_process_version_count', sortable: true, width: '100px' },
  { title: '衍生文件', key: 'family_derived_count', sortable: true, width: '100px' },
  { title: '认领状态', key: 'claim_status', sortable: false, width: '120px' },
])

// 副本列表表格列配置
const copiesHeaders = [
  { title: '文件路径', key: 'path', sortable: false },
  { title: '文件大小', key: 'file_size', sortable: false, width: '100px' },
]

const familyMemberHeaders = [
  { title: '资源 / 路径', key: 'path', sortable: false },
  { title: '来源 IP', key: 'ip', sortable: false, width: '140px' },
  { title: '相似度', key: 'family_score', sortable: true, width: '110px' },
  { title: '分布', key: 'source_count', sortable: true, width: '90px' },
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
  if (status === claimStatusIgnored.value) return claimStatusIgnored.text
  const option = claimStatusOptions.find(o => o.value === status)
  return option?.text || '未知'
}

// 获取认领状态颜色
const getClaimStatusColor = (status: number) => {
  if (status === claimStatusIgnored.value) return claimStatusIgnored.color
  const option = claimStatusOptions.find(o => o.value === status)
  return option?.color || 'grey'
}

// 计算总页数
const totalPages = computed(() => {
  return Math.ceil(total.value / pageSize.value)
})

// 是否可以执行认领操作
const canClaim = computed(() => {
  return selectedIds.value.length > 0
})

// 加载配置
const loadConfig = async () => {
  try {
    config.value = await api.getConfig()
  } catch (error) {
    console.error('Failed to load config:', error)
  }
}

// 加载统计数据
const loadStatistics = async () => {
  try {
    statistics.value = await api.getResourcesStatistics()
  } catch (error) {
    console.error('Failed to load statistics:', error)
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
      claimStatusFilter: claimStatusFilter.value,
      businessTypeFilter: activeTab.value,
      groupByFamily: true,
    })
    resources.value = result.resources
    total.value = result.total
    // 清空选中项
    selectedIds.value = []
  } catch (error) {
    console.error('Failed to load resources:', error)
    resources.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

// 按策略计算认领 IDs（跳过弹窗时使用）
const buildIdsByPolicy = (
  primaries: any[],
  familyMap: Record<string, FamilyMemberDetail[]>,
  policy: string,
): number[] => {
  const seen = new Set<number>()
  const out: number[] = []
  for (const p of primaries) {
    if (!seen.has(p.data_resources_id)) { seen.add(p.data_resources_id); out.push(p.data_resources_id) }
    const members = familyMap[p.content_sign] || []
    for (const m of members) {
      if (m.data_resources_id === p.data_resources_id) continue
      if ((m.claim_status ?? 0) !== 0) continue  // skip already claimed
      const shouldInclude = policy === 'all' ||
        (policy === 'same_content_only' && m.family_relation === 'same_content')
      if (shouldInclude && !seen.has(m.data_resources_id)) {
        seen.add(m.data_resources_id)
        out.push(m.data_resources_id)
      }
    }
  }
  return out
}

// 执行认领操作（公共）
const doClaim = async (claimStatus: number, ids: number[], userInfo: any) => {
  submitting.value = true
  try {
    const result = await api.batchClaim({
      ids,
      is_claimed: 1,
      claim_status: claimStatus,
      claimant_name: userInfo.user_name,
      claimant_unit: userInfo.company_name,
    })
    showSnackbar(`成功认领 ${result.updatedCount} 条资源`, 'success')
    await loadResources()
    loadStatistics()
  } catch (e) {
    const message = e instanceof Error ? e.message : '认领失败'
    showSnackbar(message, 'error')
  } finally {
    submitting.value = false
  }
}

// 直接认领（选择分类后立即提交）
const handleClaim = async (claimStatus: number) => {
  if (!canClaim.value) {
    showSnackbar('请先选择要认领的资源', 'warning')
    return
  }

  const userInfo = await userInfoManager.getUserInfo()
  if (!userInfo) {
    showSnackbar('请先登录', 'warning')
    return
  }

  dialogClaimStatus.value = claimStatus

  const selectedResources = resources.value.filter(r => selectedIds.value.includes(r.data_resources_id))
  const contentSigns = selectedResources.map(r => r.content_sign).filter(Boolean) as string[]

  let familyMap: Record<string, FamilyMemberDetail[]> = {}
  try {
    familyMap = await api.batchFamilyMembers(contentSigns)
  } catch (e) {
    console.warn('batchFamilyMembers failed, proceeding without family info:', e)
  }

  // Bypass 1: no family members at all
  if (Object.keys(familyMap).length === 0) {
    await doClaim(claimStatus, selectedIds.value, userInfo)
    return
  }

  // Bypass 2: user set skip_dialog
  if (config.value?.claim_family_skip_dialog === 'true') {
    const policy = config.value.claim_family_default_policy || 'same_content_only'
    const ids = buildIdsByPolicy(selectedResources, familyMap, policy)
    await doClaim(claimStatus, ids, userInfo)
    return
  }

  // Route to dialog
  if (selectedResources.length === 1) {
    const primary = selectedResources[0] as unknown as FamilyMemberDetail
    dialogPrimary.value = primary
    dialogMembers.value = familyMap[primary.content_sign] || [primary]
    singleDialogOpen.value = true
  } else {
    dialogBatchPrimaries.value = selectedResources as unknown as FamilyMemberDetail[]
    dialogBatchFamilyMap.value = familyMap
    batchDialogOpen.value = true
  }
}

const onSingleConfirm = async (payload: { ids: number[]; skipNextTime: boolean }) => {
  const userInfo = await userInfoManager.getUserInfo()
  if (!userInfo) return
  await doClaim(dialogClaimStatus.value, payload.ids, userInfo)
  if (payload.skipNextTime) {
    await api.saveConfig({ claim_family_skip_dialog: 'true' } as any).catch(() => {})
  }
}

const onBatchConfirm = async (payload: { ids: number[]; skipNextTime: boolean }) => {
  const userInfo = await userInfoManager.getUserInfo()
  if (!userInfo) return
  await doClaim(dialogClaimStatus.value, payload.ids, userInfo)
  if (payload.skipNextTime) {
    await api.saveConfig({ claim_family_skip_dialog: 'true' } as any).catch(() => {})
  }
}

// 打开文件
const handleOpenFile = async (item: DataResource) => {
  if (!item.content_sign) {
    showSnackbar('文件内容签名不存在', 'warning')
    return
  }

  openingFile.value = true
  try {
    const result = await api.openFile(item.content_sign)
    if (result.success) {
      showSnackbar(result.message, 'success')
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : '打开文件失败'
    showSnackbar(message, 'error')
  } finally {
    openingFile.value = false
  }
}

// 查看副本列表 / 家族成员
const handleViewCopies = async (item: DataResource) => {
  if (!item.content_sign) {
    showSnackbar('文件内容签名不存在', 'warning')
    return
  }

  copiesLoading.value = true
  currentResourceName.value = item.resources_name || '未命名资源'
  currentCopies.value = []
  currentFamily.value = null
  currentFamilyGroup.value = 'all'
  currentDialogMode.value = 'copies'
  showCopiesDialog.value = true

  try {
    const result = await api.getCopies(item.content_sign)
    // 副本列表里剔除主路径（资源列表那一行已经代表它），只展示「其它」副本路径
    if (item.primary_path) {
      currentCopies.value = result.copies.filter(c => c.path !== item.primary_path)
    } else {
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

const handleViewFamilyGroup = async (item: DataResource, group: FamilyGroupKey) => {
  if (!item.family_id) return

  copiesLoading.value = true
  currentResourceName.value = item.resources_name || '未命名资源'
  currentCopies.value = []
  currentFamily.value = null
  currentFamilyGroup.value = group
  currentDialogMode.value = 'family'
  showCopiesDialog.value = true

  try {
    currentFamily.value = await api.getFamilyMembers(item.family_id)
  } catch (error) {
    const message = error instanceof Error ? error.message : '获取家族成员失败'
    showSnackbar(message, 'error')
    currentFamily.value = null
  } finally {
    copiesLoading.value = false
  }
}

const familyGroupCount = (item: DataResource, group: FamilyGroupKey): number => {
  if (!item.family_id) return 0
  if (group === 'same_content') return item.family_same_content_count || 0
  if (group === 'process_version') return item.family_process_version_count || 0
  if (group === 'derived') return item.family_derived_count || 0
  if (group === 'primary') return item.family_relation === 'primary' ? 1 : 0
  return item.family_member_count || 0
}

const currentFamilyRows = computed<FamilyMemberDetail[]>(() => {
  if (!currentFamily.value) return []
  if (currentFamilyGroup.value === 'all') {
    return familyGroupDefs.flatMap(group => currentFamily.value?.groups[group.key] || [])
  }
  return currentFamily.value.groups[currentFamilyGroup.value] || []
})

const currentFamilyGroupMeta = computed(() => {
  if (currentFamilyGroup.value === 'all') {
    return { title: '全部家族成员', color: 'primary', icon: 'mdi-graph' }
  }
  const found = familyGroupDefs.find(item => item.key === currentFamilyGroup.value)
  return found || { title: '家族成员', color: 'primary', icon: 'mdi-graph' }
})

// ============================================================
// 相似度分析
// ============================================================

const isAnalysisRunning = computed(() => {
  const s = analysisTask.value?.task_state
  return s === 'pending' || s === 'running'
})

const analysisProgressText = computed(() => {
  const t = analysisTask.value
  if (!t) return ''
  if (t.task_state === 'pending') return '排队中…'
  if (t.task_state === 'running') return `分析中：${t.phase || ''}`
  if (t.task_state === 'succeed') return `已完成：${t.family_count} 个家族 / ${t.member_count} 个成员`
  if (t.task_state === 'failed') return `失败：${t.error_message || '未知错误'}`
  return ''
})

const startAnalysis = async () => {
  analysisStarting.value = true
  try {
    const { task_id } = await api.startSimilarityAnalysis()
    analysisTask.value = await api.getSimilarityTask(task_id)
    startAnalysisPolling()
  } catch (error) {
    const message = error instanceof Error ? error.message : '启动相似度分析失败'
    showSnackbar(message, 'error')
  } finally {
    analysisStarting.value = false
  }
}

const startAnalysisPolling = () => {
  stopAnalysisPolling()
  analysisPollTimer = setInterval(async () => {
    if (!analysisTask.value) return
    try {
      const t = await api.getSimilarityTask(analysisTask.value.task_id)
      analysisTask.value = t
      if (t.task_state === 'succeed' || t.task_state === 'failed') {
        stopAnalysisPolling()
        if (t.task_state === 'succeed') {
          showSnackbar(`相似度分析完成：${t.family_count} 个家族`, 'success')
          await loadResources()
          loadStatistics()
        }
      }
    } catch (e) {
      // ignore one-off poll errors
    }
  }, 2000)
}

const stopAnalysisPolling = () => {
  if (analysisPollTimer) {
    clearInterval(analysisPollTimer)
    analysisPollTimer = null
  }
}

const loadLatestAnalysisTask = async () => {
  try {
    const t = await api.getLatestSimilarityTask()
    if (t) {
      analysisTask.value = t
      if (t.task_state === 'pending' || t.task_state === 'running') {
        startAnalysisPolling()
      }
    }
  } catch (e) {
    // no-op
  }
}

// 显示提示
const showSnackbar = (text: string, color: string) => {
  snackbarText.value = text
  snackbarColor.value = color
  snackbar.value = true
}

// 监听搜索和过滤条件变化
watch([search, claimStatusFilter], () => {
  page.value = 1
  loadResources()
})

// tab 切换：保存状态 + 清空跨 tab 的多选（防止把不同业务来源的资源一起认领）+ 重新加载
watch(activeTab, (newTab) => {
  try {
    localStorage.setItem(CLAIM_TAB_STORAGE_KEY, newTab)
  } catch (e) {
    console.error('Failed to save claim tab state:', e)
  }
  selectedIds.value = []
  page.value = 1
  loadResources()
})

// 监听分页变化
watch([page, pageSize], () => {
  loadResources()
})

// 组件挂载时加载数据
onMounted(async () => {
  // 先恢复 tab 状态，避免 loadResources 先用 default 拉一次再被 watch 触发第二次拉
  try {
    const saved = localStorage.getItem(CLAIM_TAB_STORAGE_KEY)
    if (saved === 'workspace' || saved === 'new_access' || saved === 'history_inventory') {
      activeTab.value = saved
    }
  } catch (e) {
    console.error('Failed to load claim tab state:', e)
  }
  // 先 await 配置：疑似横幅(loadSuspectSummary)依赖 config.full_inventory_time，
  // 否则首次进入(非切 tab)时 config 尚未就绪 → 疑似数算成 0 → 黄色横幅不显示。
  await loadConfig()
  loadStatistics()
  loadLatestAnalysisTask()
  loadSuspectSummary()
  await loadResources()
})

// ============================================================
// 疑似非个人文件：横幅 + 一键忽略
// ============================================================

const suspectCount = ref(0)
const suspectSamples = ref<string[]>([])
const showSuspectConfirm = ref(false)
const ignoringSuspect = ref(false)

const activeTabLabel = computed(() => {
  switch (activeTab.value) {
    case 'workspace': return '工作文件档案管理'
    case 'new_access': return '新数据登记管理'
    case 'history_inventory': return '历史数据专项治理'
    default: return ''
  }
})

// 给 suspect 行加底色淡化的 css class（Vuetify v-data-table 行级 props）
const rowPropsForResource = ({ item }: { item: DataResource }) => {
  if (item.suspect_non_personal === 1) {
    return { class: 'suspect-row' }
  }
  return {}
}

const loadSuspectSummary = async () => {
  try {
    const params: { businessType?: string; fullInventoryTime?: string } = {
      businessType: activeTab.value,
    }
    if (config.value?.full_inventory_time) {
      params.fullInventoryTime = config.value.full_inventory_time
    }
    const res = await api.getSuspectSummary(params)
    suspectCount.value = res.count
    suspectSamples.value = res.sample_paths
  } catch (e) {
    // 静默：摘要拉不到不影响主流程
    console.error('Failed to load suspect summary:', e)
  }
}

const openSuspectConfirm = () => {
  if (suspectCount.value === 0) return
  showSuspectConfirm.value = true
}

const ignoreAllSuspect = async () => {
  ignoringSuspect.value = true
  try {
    const claimant = config.value?.contact_name || '未登记'
    const claimantUnit = config.value?.contact_unit || '未登记'
    const params: {
      businessType?: string
      fullInventoryTime?: string
      claimant_name: string
      claimant_unit: string
    } = {
      businessType: activeTab.value,
      claimant_name: claimant,
      claimant_unit: claimantUnit,
    }
    if (config.value?.full_inventory_time) {
      params.fullInventoryTime = config.value.full_inventory_time
    }
    const res = await api.ignoreAllSuspect(params)
    showSuspectConfirm.value = false
    showSnackbar(`已忽略 ${res.updatedCount} 条疑似非个人文件`, 'success')
    await loadResources()
    await loadSuspectSummary()
    loadStatistics()
  } catch (e) {
    const msg = e instanceof Error ? e.message : '一键忽略失败'
    showSnackbar(msg, 'error')
  } finally {
    ignoringSuspect.value = false
  }
}

// tab 切换后重新拉取 suspect 数（不同 tab 范围不同）
watch(activeTab, () => {
  loadSuspectSummary()
})

onUnmounted(() => {
  stopAnalysisPolling()
})
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-account-check</v-icon>
        责任认领
        <v-spacer />
        <v-btn
          color="primary"
          variant="tonal"
          size="small"
          data-test="rebuild-similarity-btn"
          :disabled="isAnalysisRunning"
          @click="showRebuildDialog = true"
        >
          <v-icon start>mdi-refresh</v-icon>
          重建相似关系
        </v-btn>
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          对信息资源进行责任认领分类，批量选中后点击"认领"按钮进行认领操作。
          相同内容的文件已按"家族"折叠展示，认领主资源时同家族其他成员一并被认领。
        </div>
      </v-card-text>
    </v-card>

    <!-- 业务来源选项卡（与「本机数据资源图谱」三个 tab 同源） -->
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

    <!-- 疑似非个人文件横幅 -->
    <v-alert
      v-if="suspectCount > 0"
      type="warning"
      variant="tonal"
      class="mb-4"
      data-test="suspect-banner"
    >
      <div class="d-flex align-center">
        <div class="flex-grow-1">
          <strong>{{ suspectCount }}</strong> 个文件疑似非个人（系统目录 / 二进制 / 字体 / 临时文件等）。
          这些文件已在列表中标记为 ⚠ 并淡化显示。
        </div>
        <v-btn
          color="warning"
          variant="elevated"
          size="small"
          data-test="suspect-ignore-btn"
          @click="openSuspectConfirm"
        >
          <v-icon start>mdi-eye-off-outline</v-icon>
          一键标为已忽略
        </v-btn>
      </div>
    </v-alert>

    <!-- 一键忽略二次确认对话框 -->
    <v-dialog v-model="showSuspectConfirm" max-width="640">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-eye-off-outline</v-icon>
          一键忽略疑似非个人文件
        </v-card-title>
        <v-card-text>
          <div class="mb-3">
            将把当前 tab（<strong>{{ activeTabLabel }}</strong>）下
            <strong>{{ suspectCount }}</strong> 个疑似非个人文件标记为
            <v-chip size="x-small" color="grey-darken-1" variant="tonal">已忽略</v-chip>，
            它们默认不再出现在列表里。
          </div>
          <div class="mb-2 text-caption text-grey">
            示例路径（前 {{ suspectSamples.length }} 条）：
          </div>
          <v-list density="compact" max-height="240" class="border rounded">
            <v-list-item
              v-for="(p, i) in suspectSamples"
              :key="i"
              :title="p"
              class="text-caption"
            />
          </v-list>
          <v-alert type="info" density="compact" variant="tonal" class="mt-3">
            <span class="text-caption">
              如有误判，可在顶部认领状态过滤里选「已忽略」找回；这些文件不会被删除，
              也不会被上传到管理后台。
            </span>
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn
            data-test="suspect-confirm-cancel"
            @click="showSuspectConfirm = false"
          >取消</v-btn>
          <v-btn
            color="warning"
            variant="elevated"
            :loading="ignoringSuspect"
            data-test="suspect-confirm-ok"
            @click="ignoreAllSuspect"
          >
            <v-icon start>mdi-eye-off-outline</v-icon>
            确认忽略 {{ suspectCount }} 个文件
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

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
              v-model="claimStatusFilter"
              :items="filterOptions"
              item-value="value"
              item-title="text"
              label="认领状态过滤"
              variant="outlined"
              density="compact"
              hide-details
            />
          </v-col>
          <v-col cols="12" md="5" class="d-flex align-center justify-end">
            <span class="text-body-2 text-grey mr-4">
              已选 {{ selectedIds.length }} 条，共 {{ total }} 条记录
            </span>
            <v-menu>
              <template v-slot:activator="{ props }">
                <v-btn
                  color="primary"
                  variant="tonal"
                  :disabled="!canClaim || submitting"
                  :loading="submitting"
                  v-bind="props"
                >
                  <v-icon start>mdi-account-check</v-icon>
                  认领
                  <v-icon end>mdi-menu-down</v-icon>
                </v-btn>
              </template>
              <v-list density="compact">
                <v-list-item
                  v-for="option in claimStatusOptions.filter(o => o.value !== 0)"
                  :key="option.value"
                  @click="handleClaim(option.value)"
                >
                  <template v-slot:prepend>
                    <v-icon :color="option.color" size="small">mdi-circle</v-icon>
                  </template>
                  <v-list-item-title>{{ option.text }}</v-list-item-title>
                </v-list-item>
              </v-list>
            </v-menu>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <!-- 资源表格 -->
    <v-card elevation="1">
      <v-data-table
        v-model="selectedIds"
        :headers="headers"
        :items="resources"
        :loading="loading"
        item-value="data_resources_id"
        :items-per-page="pageSize"
        show-select
        hide-default-footer
        :row-props="rowPropsForResource"
      >
        <template v-slot:item.resources_name="{ item }">
          <div class="d-flex align-center" :class="{ 'text-disabled': item.suspect_non_personal === 1 }">
            <v-tooltip v-if="item.suspect_non_personal === 1" location="top" open-delay="100">
              <template v-slot:activator="{ props }">
                <v-icon v-bind="props" size="small" color="warning" class="mr-1" data-test="suspect-row-icon">
                  mdi-alert-circle-outline
                </v-icon>
              </template>
              <span>疑似非个人文件（系统目录 / 二进制 / 字体等）</span>
            </v-tooltip>
            <v-icon size="small" class="mr-2" color="grey">
              mdi-file-document-outline
            </v-icon>
            <v-tooltip location="top" open-delay="100">
              <template v-slot:activator="{ props }">
                <div
                  v-bind="props"
                  class="text-truncate resource-name-link"
                  style="max-width: 350px"
                  @click="handleOpenFile(item)"
                >
                  {{ item.resources_name || '-' }}
                </div>
              </template>
              <span>{{ item.primary_path || item.resources_name || '-' }}</span>
            </v-tooltip>
            <v-tooltip location="top" open-delay="100">
              <template v-slot:activator="{ props }">
                <v-chip
                  v-if="item.source_count > 1"
                  v-bind="props"
                  size="small"
                  variant="tonal"
                  color="info"
                  class="ml-2"
                  style="cursor: pointer;"
                  @click.stop="handleViewCopies(item)"
                >
                  {{ item.source_count - 1 }} 副本
                </v-chip>
              </template>
              <span>查看物理分布路径</span>
            </v-tooltip>
            <v-tooltip location="top" open-delay="100" v-if="(item.family_member_count ?? 0) > 1">
              <template v-slot:activator="{ props }">
                <v-chip
                  v-bind="props"
                  size="small"
                  variant="tonal"
                  color="primary"
                  class="ml-2"
                  style="cursor: pointer;"
                  data-test="family-chip"
                  @click.stop="handleViewFamilyGroup(item, 'all')"
                >
                  关联 {{ (item.family_member_count ?? 0) - 1 }} ▾
                </v-chip>
              </template>
              <span>查看相似家族成员</span>
            </v-tooltip>
          </div>
        </template>

        <template v-slot:item.first_create_time="{ item }">
          {{ formatDate(item.first_create_time) }}
        </template>

        <template v-slot:item.family_same_content_count="{ item }">
          <v-chip
            v-if="familyGroupCount(item, 'same_content') > 0"
            size="small"
            variant="tonal"
            color="success"
            class="cursor-pointer"
            @click="handleViewFamilyGroup(item, 'same_content')"
          >
            {{ familyGroupCount(item, 'same_content') }}
          </v-chip>
          <span v-else class="text-caption text-medium-emphasis">-</span>
        </template>

        <template v-slot:item.family_process_version_count="{ item }">
          <v-chip
            v-if="familyGroupCount(item, 'process_version') > 0"
            size="small"
            variant="tonal"
            color="warning"
            class="cursor-pointer"
            @click="handleViewFamilyGroup(item, 'process_version')"
          >
            {{ familyGroupCount(item, 'process_version') }}
          </v-chip>
          <span v-else class="text-caption text-medium-emphasis">-</span>
        </template>

        <template v-slot:item.family_derived_count="{ item }">
          <v-chip
            v-if="familyGroupCount(item, 'derived') > 0"
            size="small"
            variant="tonal"
            color="info"
            class="cursor-pointer"
            @click="handleViewFamilyGroup(item, 'derived')"
          >
            {{ familyGroupCount(item, 'derived') }}
          </v-chip>
          <span v-else class="text-caption text-medium-emphasis">-</span>
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

        <template v-slot:no-data>
          <div class="text-center py-8">
            <v-icon size="64" color="grey-lighten-1">mdi-folder-open-outline</v-icon>
            <div class="mt-4 text-grey">暂无资源数据</div>
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

    <!-- 提示消息 -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" :timeout="3000">
      {{ snackbarText }}
    </v-snackbar>

    <!-- 重建相似关系对话框 -->
    <RebuildSimilarityDialog
      v-model="showRebuildDialog"
      @confirm="startAnalysis"
    />

    <!-- 单文件认领弹窗 -->
    <ClaimFamilyDialogSingle
      v-if="dialogPrimary"
      v-model="singleDialogOpen"
      :primary="dialogPrimary"
      :members="dialogMembers"
      :claim-status="dialogClaimStatus"
      @confirm="onSingleConfirm"
    />

    <!-- 批量认领弹窗 -->
    <ClaimFamilyDialogBatch
      v-model="batchDialogOpen"
      :selected-primaries="dialogBatchPrimaries"
      :family-map="dialogBatchFamilyMap"
      :claim-status="dialogClaimStatus"
      :default-policy="(config?.claim_family_default_policy as any) || 'same_content_only'"
      @confirm="onBatchConfirm"
    />

    <!-- 副本列表 / 家族成员对话框 -->
    <v-dialog v-model="showCopiesDialog" max-width="1080px">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" :color="currentDialogMode === 'family' ? currentFamilyGroupMeta.color : undefined">
            {{ currentDialogMode === 'family' ? currentFamilyGroupMeta.icon : 'mdi-content-copy' }}
          </v-icon>
          {{ currentDialogMode === 'family' ? currentFamilyGroupMeta.title : '副本列表' }}
          <v-chip v-if="currentFamily" class="ml-2" size="x-small" color="primary" variant="tonal">
            family #{{ currentFamily.family_id }} · {{ currentFamily.total_members }} 成员
          </v-chip>
        </v-card-title>
        <v-card-subtitle>资源：{{ currentResourceName }}</v-card-subtitle>
        <v-divider />
        <v-card-text class="pa-0" style="max-height: 60vh; overflow-y: auto;">
          <!-- 家族成员视图：按当前点击的列过滤 -->
          <template v-if="currentDialogMode === 'family'">
            <div class="px-4 py-2 d-flex align-center bg-grey-lighten-4">
              <v-icon :color="currentFamilyGroupMeta.color" size="small" class="mr-2">
                {{ currentFamilyGroupMeta.icon }}
              </v-icon>
              <span class="text-body-2 font-weight-medium">{{ currentFamilyGroupMeta.title }}</span>
              <v-chip size="x-small" :color="currentFamilyGroupMeta.color" variant="tonal" class="ml-2">
                {{ currentFamilyRows.length }}
              </v-chip>
            </div>
            <v-data-table
              :headers="familyMemberHeaders"
              :items="currentFamilyRows"
              :loading="copiesLoading"
              item-value="data_distribution_id"
              :items-per-page="-1"
              hide-default-footer
              density="compact"
            >
              <template #item.path="{ item }">
                <div>
                  <div class="text-body-2">{{ item.path || item.resources_name || '-' }}</div>
                  <div class="text-caption text-medium-emphasis">
                    resource #{{ item.data_resources_id }}
                  </div>
                </div>
              </template>
              <template #item.ip="{ item }">
                <span class="text-caption">{{ item.ip || '-' }}</span>
              </template>
              <template #item.family_score="{ item }">
                <v-chip
                  v-if="item.family_score != null"
                  size="x-small"
                  :color="currentFamilyGroupMeta.color"
                  variant="tonal"
                >
                  {{ (item.family_score * 100).toFixed(1) }}%
                </v-chip>
                <span v-else class="text-caption text-medium-emphasis">-</span>
              </template>
              <template #item.source_count="{ item }">
                {{ item.source_count }}
              </template>
              <template #no-data>
                <div class="text-center py-4 text-grey">该分组暂无成员</div>
              </template>
            </v-data-table>
          </template>

          <!-- 老视图：纯哈希副本 -->
          <v-data-table
            v-else
            :headers="copiesHeaders"
            :items="currentCopies"
            :loading="copiesLoading"
            item-value="data_distribution_id"
            :items-per-page="-1"
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
              <div class="text-center py-4 text-grey">暂无副本数据</div>
            </template>
          </v-data-table>
        </v-card-text>
        <v-divider />
        <v-card-actions>
          <v-spacer />
          <v-btn color="primary" variant="text" @click="showCopiesDialog = false">
            关闭
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.resource-name-link {
  cursor: pointer;
  color: #1976d2;
  text-decoration: none;
  transition: color 0.2s;
}

.resource-name-link:hover {
  color: #1565c0;
  text-decoration: underline;
}

.cursor-pointer {
  cursor: pointer;
}

.cursor-pointer:hover {
  opacity: 0.8;
}

/* 疑似非个人文件行：底色淡化，提示用户这一行多半可忽略 */
:deep(.suspect-row) {
  background-color: rgba(255, 152, 0, 0.06);
}
:deep(.suspect-row:hover) {
  background-color: rgba(255, 152, 0, 0.12);
}
</style>
