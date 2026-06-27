<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { api, type FileItem, type SystemConfig, type ArchiveApplication, type ArchiveManagementFileItem } from '@/services/api'
import { userInfoManager } from '@/services/UserInfoManager'

// 归档类型选项卡
const tabs = [
  { value: 'pending', label: '集中上传管理', icon: 'mdi-file-upload', count: 0 },
  { value: 'core', label: '保密室上传记录', icon: 'mdi-lock', count: 0 },
  { value: 'important', label: '档案室上传记录', icon: 'mdi-folder-lock', count: 0 },
  { value: 'open', label: '资料室上传记录', icon: 'mdi-folder-open', count: 0 }
]

// 所有选项卡的总数缓存
const allTabsCount = ref<Record<string, number>>({
  pending: 0,
  core: 0,
  important: 0,
  open: 0
})

// 状态
const loading = ref(false)
const activeTab = ref('pending')
const files = ref<ArchiveManagementFileItem[]>([])
const config = ref<SystemConfig | null>(null)
const search = ref('')
const page = ref(1)
const pageSize = ref(50)
const total = ref(0)

// 重要程度过滤（仅对 pending tab 有效）
const importanceLevel = ref<number | undefined>(undefined)
const importanceLevelOptions = [
  { value: 1, text: '核心' },
  { value: 2, text: '重要' },
  { value: 3, text: '开放' }
]

// 表格多选
const selectedItems = ref<ArchiveManagementFileItem[]>([])

// 上传状态
const uploading = ref<Record<number, boolean>>({})
const uploadResults = ref<Record<number, { success: boolean, message?: string }>>({})

// 对话框状态
const dialogVisible = ref(false)
const currentFile = ref<FileItem | null>(null)
const formRef = ref<any>(null)
const submitting = ref(false)
const userInfoLoaded = ref(false) // 标记用户信息是否已自动填入

// 归档申请表表单
const archiveForm = ref<ArchiveApplication>({
  applicant_unit: '',
  applicant_department: '',
  applicant_name: '',
  applicant_contact: '',
  archive_file_name: '',
  archive_file_category: '',
  archive_file_hash: '',
  application_time: '',
  content_title: '',
  content_summary: '',
  data_classification: '一般',
  protection_method: 1,
  share_range: ''
})

// 表单验证规则
const formRules = {
  applicant_unit: [{ required: true, message: '请输入申请人单位', trigger: 'blur' }],
  applicant_department: [{ required: true, message: '请输入申请人部门', trigger: 'blur' }],
  applicant_name: [{ required: true, message: '请输入申请人姓名', trigger: 'blur' }],
  applicant_contact: [{ required: true, message: '请输入联系方式', trigger: 'blur' }],
  archive_file_name: [{ required: true, message: '请输入归档文件名称', trigger: 'blur' }],
  archive_file_category: [{ required: true, message: '请输入归档文件类别', trigger: 'blur' }],
  content_title: [{ required: true, message: '请输入内容标题', trigger: 'blur' }],
  data_classification: [{ required: true, message: '请选择数据定级', trigger: 'change' }],
  protection_method: [{ required: true, message: '请选择保护方式', trigger: 'change' }]
}

// 数据定级选项
const classificationOptions = ['核心', '重要', '一般', '公开']

// 保护方式选项
const protectionOptions = [
  { value: 1, text: '全网孤本纸质备份' },
  { value: 2, text: '全网本机双孤本互为备份' }
]

// Snackbar状态
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

// 表格列配置
const headers = computed(() => {
  const baseHeaders = [
    { title: '文件名', key: 'path', sortable: true },
    { title: '文件后缀', key: 'file_suffix', sortable: true, width: '100px' },
    { title: '文件大小', key: 'file_size', sortable: true, width: '120px' },
    { title: '创建时间', key: 'file_create_time', sortable: true, width: '180px' },
  ]

  // 添加重要程度列
  const importanceHeader = { title: '重要程度', key: 'importance_level', sortable: true, width: '100px' }

  if (activeTab.value === 'pending') {
    return [
      ...baseHeaders,
      importanceHeader,
      { title: '上传状态', key: 'upload_state', sortable: true, width: '120px' },
      { title: '操作', key: 'actions', sortable: false, width: '120px' },
    ]
  } else {
    return [
      ...baseHeaders,
      importanceHeader,
      { title: '上传状态', key: 'upload_state', sortable: true, width: '120px' },
    ]
  }
})

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

// 格式化时间为申请时间格式
const formatApplicationTime = () => {
  const now = new Date()
  return now.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  }).replace(/\//g, '-')
}

// 格式化文件大小
const formatFileSize = (bytes: number | null | undefined) => {
  if (bytes === null || bytes === undefined) return '-'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(2) + ' ' + units[i]
}

// 是否可以上传
const canUpload = computed(() => {
  return config.value?.upload_server_url != null && config.value.upload_server_url !== ''
})

// 计算总页数
const totalPages = computed(() => {
  return Math.ceil(total.value / pageSize.value)
})

// 是否有选中项
const hasSelection = computed(() => {
  return selectedItems.value.length > 0
})

// 当前选项卡是否可以执行无需归档操作
const canDoNoArchive = computed(() => {
  return activeTab.value === 'pending' && hasSelection.value
})

// 加载配置
const loadConfig = async () => {
  try {
    config.value = await api.getConfig()
  } catch (error) {
    console.error('Failed to load config:', error)
  }
}

// 加载文件列表
const loadFiles = async () => {
  loading.value = true
  try {
    const result = await api.getArchiveManagementFiles({
      search: search.value || undefined,
      archiveType: activeTab.value as 'pending' | 'core' | 'important' | 'open',
      importanceLevelFilter: activeTab.value === 'pending' ? importanceLevel.value : undefined,
      page: page.value,
      pageSize: pageSize.value,
    })
    files.value = result.files
    total.value = result.total
    // 更新当前选项卡的计数（注意：计数不受 importanceLevelFilter 影响）
    allTabsCount.value[activeTab.value] = result.total
  } catch (error) {
    console.error('Failed to load files:', error)
    files.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

// 加载所有选项卡的总数
const loadAllTabsCount = async () => {
  const archiveTypes: Array<'pending' | 'core' | 'important' | 'open'> = ['pending', 'core', 'important', 'open']
  for (const type of archiveTypes) {
    try {
      const result = await api.getArchiveManagementFiles({
        archiveType: type,
        page: 1,
        pageSize: 1, // 只需要获取总数，不需要文件详情
      })
      allTabsCount.value[type] = result.total
    } catch (error) {
      console.error(`Failed to load count for ${type}:`, error)
      allTabsCount.value[type] = 0
    }
  }
}

// 打开归档申请表对话框
const openArchiveDialog = async (file: FileItem) => {
  if (!canUpload.value) {
    showSnackbar('请先在设置中配置文件上传服务器地址', 'warning')
    return
  }

  currentFile.value = file
  userInfoLoaded.value = false

  // 获取用户信息
  const userInfo = await userInfoManager.getUserInfo()

  // 初始化表单数据
  archiveForm.value = {
    applicant_unit: userInfo?.company_name || '',
    applicant_department: userInfo?.department || '',
    applicant_name: userInfo?.user_name || '',
    applicant_contact: userInfo?.phone || '',
    archive_file_name: getFileName(file.path),
    archive_file_category: getCategoryFromSuffix(file.file_suffix),
    archive_file_hash: file.content_sign || '',
    application_time: formatApplicationTime(),
    content_title: getFileName(file.path).replace(/\.[^/.]+$/, ''),
    content_summary: '',
    data_classification: '一般',
    protection_method: 1,
    share_range: ''
  }

  // 如果用户信息存在，标记为已加载（用于设置只读）
  userInfoLoaded.value = !!userInfo

  if (!userInfo) {
    showSnackbar('请先在用户信息页面填写用户信息', 'warning')
  }

  dialogVisible.value = true
}

// 根据文件后缀获取类别
const getCategoryFromSuffix = (suffix: string | null): string => {
  if (!suffix) return '其他'
  const suffixLower = suffix.toLowerCase()
  if (['.doc', '.docx'].includes(suffixLower)) return 'Word文档'
  if (['.xls', '.xlsx'].includes(suffixLower)) return 'Excel表格'
  if (['.ppt', '.pptx'].includes(suffixLower)) return 'PPT演示'
  if (['.pdf'].includes(suffixLower)) return 'PDF文档'
  if (['.jpg', '.jpeg', '.png', '.gif', '.bmp', '.webp'].includes(suffixLower)) return '图片'
  if (['.mp4', '.avi', '.mov', '.wmv', '.mkv'].includes(suffixLower)) return '视频'
  if (['.mp3', '.wav', '.flac', '.aac'].includes(suffixLower)) return '音频'
  if (['.txt'].includes(suffixLower)) return '文本文件'
  if (['.zip', '.rar', '.7z', '.tar', '.gz'].includes(suffixLower)) return '压缩包'
  return '其他'
}

// 提交归档申请
const submitArchive = async () => {
  if (!currentFile.value) return

  // 验证表单
  const isValid = await validateForm()
  if (!isValid) return

  submitting.value = true
  const fileId = currentFile.value.data_distribution_id
  uploading.value[fileId] = true
  uploadResults.value[fileId] = { success: false }

  try {
    const result = await api.archiveFile(currentFile.value.path, archiveForm.value)
    uploadResults.value[fileId] = result

    if (result.success) {
      showSnackbar(result.message || '文件上报成功', 'success')
      dialogVisible.value = false
      // 刷新所有选项卡的计数
      await loadAllTabsCount()
      // 刷新文件列表以更新上传状态
      await loadFiles()
    } else {
      showSnackbar(result.message || '文件上报失败', 'error')
      // 上传失败也需要刷新列表以显示失败状态
      await loadFiles()
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : '上报失败'
    uploadResults.value[fileId] = { success: false, message }
    showSnackbar(message, 'error')
  } finally {
    submitting.value = false
    uploading.value[fileId] = false
  }
}

// 表单验证
const validateForm = async (): Promise<boolean> => {
  const form = archiveForm.value
  if (!form.applicant_unit.trim()) {
    showSnackbar('请输入申请人单位', 'warning')
    return false
  }
  if (!form.applicant_department.trim()) {
    showSnackbar('请输入申请人部门', 'warning')
    return false
  }
  if (!form.applicant_name.trim()) {
    showSnackbar('请输入申请人姓名', 'warning')
    return false
  }
  if (!form.applicant_contact.trim()) {
    showSnackbar('请输入联系方式', 'warning')
    return false
  }
  if (!form.archive_file_name.trim()) {
    showSnackbar('请输入归档文件名称', 'warning')
    return false
  }
  if (!form.archive_file_category.trim()) {
    showSnackbar('请输入归档文件类别', 'warning')
    return false
  }
  if (!form.content_title.trim()) {
    showSnackbar('请输入内容标题', 'warning')
    return false
  }
  return true
}

// 关闭对话框
const closeDialog = () => {
  if (!submitting.value) {
    dialogVisible.value = false
    currentFile.value = null
  }
}

// 显示提示
const showSnackbar = (text: string, color: string) => {
  snackbarText.value = text
  snackbarColor.value = color
  snackbar.value = true
}

// 获取上传状态图标
const getUploadStatusIcon = (file: FileItem) => {
  const result = uploadResults.value[file.data_distribution_id]
  if (!result) return null
  return result.success ? 'mdi-check-circle' : 'mdi-alert-circle'
}

// 获取上传状态颜色
const getUploadStatusColor = (file: FileItem) => {
  const result = uploadResults.value[file.data_distribution_id]
  if (!result) return ''
  return result.success ? 'success' : 'error'
}

// 获取文件上传状态文本
const getUploadStateText = (uploadState: number) => {
  switch (uploadState) {
    case 0: return '未上传'
    case 1: return '已上传'
    case 2: return '主数据已上传'
    case 3: return '上传失败'
    case 4: return '无需归档'
    default: return '未知'
  }
}

// 获取文件上传状态颜色
const getUploadStateColor = (uploadState: number) => {
  switch (uploadState) {
    case 0: return 'grey'
    case 1: return 'success'
    case 2: return 'info'
    case 3: return 'error'
    case 4: return 'info'
    default: return 'grey'
  }
}

// 获取重要程度文本
const getImportanceLevelText = (level: number | undefined) => {
  switch (level) {
    case 1: return '核心'
    case 2: return '重要'
    case 3: return '开放'
    case 4: return '隐私'
    default: return '-'
  }
}

// 获取重要程度颜色
const getImportanceLevelColor = (level: number | undefined) => {
  switch (level) {
    case 1: return 'error'
    case 2: return 'warning'
    case 3: return 'info'
    case 4: return 'grey'
    default: return 'grey'
  }
}

// 判断文件是否可以上传（0未上传 或 3上传失败 可以上传）
const canUploadFile = (file: FileItem) => {
  return file.upload_state === 0 || file.upload_state === 3
}

// 批量无需归档
const handleBatchNoArchive = async () => {
  const ids = selectedItems.value.map(item => item.data_distribution_id)
  if (ids.length === 0) {
    showSnackbar('请先选择需要操作的文件', 'warning')
    return
  }

  try {
    const result = await api.batchUpdateToNoArchive(ids)
    showSnackbar(`成功将 ${result.updatedCount} 条记录设置为无需归档`, 'success')
    // 刷新所有选项卡的计数
    await loadAllTabsCount()
    // 刷新列表
    await loadFiles()
    // 清空选择
    selectedItems.value = []
  } catch (error) {
    const message = error instanceof Error ? error.message : '操作失败'
    showSnackbar(message, 'error')
  }
}

// 监听选项卡变化
watch(activeTab, () => {
  page.value = 1
  // 切换 tab 时重置重要程度过滤
  importanceLevel.value = undefined
  loadFiles()
})

// 监听重要程度变化
watch(importanceLevel, () => {
  page.value = 1
  loadFiles()
})

// 监听搜索条件变化
watch([search], () => {
  page.value = 1
  loadFiles()
})

// 监听分页变化
watch([page, pageSize], () => {
  loadFiles()
})

// 组件挂载时加载数据
onMounted(async () => {
  await loadConfig()
  // 并行加载所有选项卡计数和当前选项卡的文件列表
  await Promise.all([
    loadAllTabsCount(),
    loadFiles()
  ])
})
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-archive</v-icon>
        工作文件归档管理
      </v-card-title>
      <v-card-text>
        <v-tabs
          v-model="activeTab"
          color="primary"
          align-tabs="start"
        >
          <v-tab
            v-for="tab in tabs"
            :key="tab.value"
            :value="tab.value"
          >
            <v-icon start class="mr-1">{{ tab.icon }}</v-icon>
            {{ tab.label }}
            <v-chip
              size="x-small"
              class="ml-2"
              color="primary"
              variant="tonal"
            >
              {{ allTabsCount[tab.value] }}
            </v-chip>
          </v-tab>
        </v-tabs>
      </v-card-text>
    </v-card>

    <!-- 搜索栏和操作按钮 -->
    <v-card class="mb-4" elevation="1">
      <v-card-text>
        <v-row align="center">
          <v-col cols="12" md="3">
            <v-text-field
              v-model="search"
              prepend-inner-icon="mdi-magnify"
              label="搜索文件"
              variant="outlined"
              density="compact"
              hide-details
              clearable
            />
          </v-col>
          <v-col cols="12" md="3" v-if="activeTab === 'pending'">
            <v-select
              v-model="importanceLevel"
              :items="importanceLevelOptions"
              item-value="value"
              item-title="text"
              label="重要程度"
              variant="outlined"
              density="compact"
              hide-details
              clearable
              placeholder="全部"
            />
          </v-col>
          <v-col cols="12" md="6" class="d-flex justify-end align-center">
            <v-btn
              v-if="activeTab === 'pending'"
              color="warning"
              variant="tonal"
              :disabled="!canDoNoArchive"
              @click="handleBatchNoArchive"
              class="mr-2"
            >
              <v-icon start>mdi-close-circle</v-icon>
              无需归档 ({{ selectedItems.length }})
            </v-btn>
            <span class="text-body-2 text-grey">
              共 {{ total }} 条记录
            </span>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <!-- 文件表格 -->
    <v-card elevation="1">
      <v-data-table
        :headers="headers"
        :items="files"
        :loading="loading"
        item-value="data_distribution_id"
        :items-per-page="pageSize"
        hide-default-footer
        :show-select="activeTab === 'pending'"
        v-model="selectedItems"
        return-object
      >
        <template v-slot:item.path="{ item }">
          <div class="d-flex align-center">
            <v-icon size="small" class="mr-2" color="grey">
              mdi-file-document-outline
            </v-icon>
            <div>
              <div class="text-truncate" style="max-width: 350px" :title="item.path">
                {{ getFileName(item.path) }}
              </div>
              <div class="text-caption text-grey text-truncate" style="max-width: 350px" :title="item.path">
                {{ item.path }}
              </div>
            </div>
          </div>
        </template>

        <template v-slot:item.file_suffix="{ item }">
          <v-chip size="small" variant="tonal" color="info">
            {{ item.file_suffix || '-' }}
          </v-chip>
        </template>

        <template v-slot:item.file_size="{ item }">
          {{ formatFileSize(item.file_size) }}
        </template>

        <template v-slot:item.file_create_time="{ item }">
          {{ formatDate(item.file_create_time) }}
        </template>

        <template v-slot:item.importance_level="{ item }">
          <v-chip
            size="small"
            variant="tonal"
            :color="getImportanceLevelColor(item.importance_level)"
          >
            {{ getImportanceLevelText(item.importance_level) }}
          </v-chip>
        </template>

        <template v-slot:item.upload_state="{ item }">
          <v-chip
            size="small"
            variant="tonal"
            :color="getUploadStateColor(item.upload_state)"
          >
            {{ getUploadStateText(item.upload_state) }}
          </v-chip>
        </template>

        <template v-slot:item.actions="{ item }">
          <div class="d-flex align-center">
            <v-btn
              size="small"
              color="primary"
              variant="tonal"
              :loading="uploading[item.data_distribution_id]"
              :disabled="!canUpload || !canUploadFile(item)"
              @click="openArchiveDialog(item)"
            >
              <v-icon start>mdi-upload</v-icon>
              上报归档
            </v-btn>
            <v-icon
              v-if="getUploadStatusIcon(item)"
              :color="getUploadStatusColor(item)"
              class="ml-2"
              size="small"
            >
              {{ getUploadStatusIcon(item) }}
            </v-icon>
          </div>
        </template>

        <template v-slot:no-data>
          <div class="text-center py-8">
            <v-icon size="64" color="grey-lighten-1">mdi-folder-open-outline</v-icon>
            <div class="mt-4 text-grey">暂无文件数据</div>
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

    <!-- 归档申请表对话框 -->
    <v-dialog v-model="dialogVisible" max-width="600" persistent>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-file-document-edit</v-icon>
          文件上报申请表
        </v-card-title>
        <v-card-subtitle v-if="currentFile" class="pb-0">
          {{ getFileName(currentFile.path) }}
        </v-card-subtitle>
        <v-card-text class="py-2">
          <v-form ref="formRef">
            <v-row dense class="mb-n2">
              <v-col cols="6">
                <v-text-field
                  v-model="archiveForm.applicant_unit"
                  label="申请人单位 *"
                  variant="outlined"
                  density="compact"
                  :readonly="userInfoLoaded"
                  :disabled="submitting"
                  :bg-color="userInfoLoaded ? 'grey-lighten-4' : undefined"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model="archiveForm.applicant_department"
                  label="申请人部门 *"
                  variant="outlined"
                  density="compact"
                  :readonly="userInfoLoaded"
                  :disabled="submitting"
                  :bg-color="userInfoLoaded ? 'grey-lighten-4' : undefined"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model="archiveForm.applicant_name"
                  label="申请人姓名 *"
                  variant="outlined"
                  density="compact"
                  :readonly="userInfoLoaded"
                  :disabled="submitting"
                  :bg-color="userInfoLoaded ? 'grey-lighten-4' : undefined"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model="archiveForm.applicant_contact"
                  label="联系方式 *"
                  variant="outlined"
                  density="compact"
                  :readonly="userInfoLoaded"
                  :disabled="submitting"
                  :bg-color="userInfoLoaded ? 'grey-lighten-4' : undefined"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model="archiveForm.archive_file_name"
                  label="归档文件名称 *"
                  variant="outlined"
                  density="compact"
                  :disabled="submitting"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model="archiveForm.archive_file_category"
                  label="归档文件类别 *"
                  variant="outlined"
                  density="compact"
                  :disabled="submitting"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model="archiveForm.content_title"
                  label="内容标题 *"
                  variant="outlined"
                  density="compact"
                  :disabled="submitting"
                  hide-details
                />
              </v-col>
              <v-col cols="6">
                <v-text-field
                  v-model="archiveForm.application_time"
                  label="申请时间"
                  variant="outlined"
                  density="compact"
                  disabled
                  hide-details
                />
              </v-col>
              <v-col cols="12">
                <v-textarea
                  v-model="archiveForm.content_summary"
                  label="内容摘要"
                  variant="outlined"
                  density="compact"
                  :disabled="submitting"
                  rows="2"
                  hide-details
                  placeholder="请输入内容摘要"
                />
              </v-col>
              <v-col cols="4">
                <v-select
                  v-model="archiveForm.data_classification"
                  :items="classificationOptions"
                  label="数据定级 *"
                  variant="outlined"
                  density="compact"
                  :disabled="submitting"
                  hide-details
                />
              </v-col>
              <v-col cols="4">
                <v-select
                  v-model="archiveForm.protection_method"
                  :items="protectionOptions"
                  item-value="value"
                  item-title="text"
                  label="保护方式 *"
                  variant="outlined"
                  density="compact"
                  :disabled="submitting"
                  hide-details
                />
              </v-col>
              <v-col cols="4">
                <v-text-field
                  v-model="archiveForm.share_range"
                  label="共享范围"
                  variant="outlined"
                  density="compact"
                  placeholder="请输入"
                  :disabled="submitting"
                  hide-details
                />
              </v-col>
            </v-row>
          </v-form>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn
            variant="text"
            @click="closeDialog"
            :disabled="submitting"
          >
            取消
          </v-btn>
          <v-btn
            color="primary"
            variant="tonal"
            @click="submitArchive"
            :loading="submitting"
          >
            <v-icon start>mdi-upload</v-icon>
            提交上报
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 提示消息 -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" :timeout="3000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
