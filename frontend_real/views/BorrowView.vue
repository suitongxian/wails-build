<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, type ArchiveFile, type QuickArchiveCabinetFile, type PersonalArchiveFile } from '@/services/api'
import { userInfoManager } from '@/services/UserInfoManager'

const router = useRouter()

// 状态
const loading = ref(false)
const search = ref('')
// 两级 tab：一级在 scope（个人 / 部门 / 单位），二级在 classification（核心 / 重要 / 一般）
// 个人（夹）= 原「本机归档文件浏览」，业务功能保持不变；部门（柜）/ 单位（室）= 档案借阅。
const activeScope = ref<'personal' | 'department' | 'unit'>('personal')
const activeLevel = ref<'core' | 'important' | 'general'>('core')

// ===== 个人（夹）：本机「个人{级别}文件夹」里的一键归档文件，按级别分区展示 =====
const personalFiles = ref<PersonalArchiveFile[]>([])
const personalLoading = ref(false)

const personalHeaders = [
  { title: '文件名称', key: 'file_name', sortable: true },
  { title: '所属项目', key: 'project_name', sortable: true, width: '200px' },
  { title: '归档位置', key: 'folder', sortable: true, width: '160px' },
  { title: '大小', key: 'file_size', sortable: true, width: '120px' },
  { title: '归档时间', key: 'archived_at', sortable: true, width: '180px' },
]

// 加载本机个人归档文件（按当前级别小 Tab）
const loadPersonalResources = async () => {
  personalLoading.value = true
  try {
    personalFiles.value = await api.getPersonalArchiveFiles(activeLevel.value)
  } catch (error) {
    console.error('Failed to load personal archive files:', error)
    personalFiles.value = []
  } finally {
    personalLoading.value = false
  }
}

// 搜索过滤
const personalFiltered = computed(() => {
  const kw = search.value.trim().toLowerCase()
  if (!kw) return personalFiles.value
  return personalFiles.value.filter(f =>
    (f.file_name || '').toLowerCase().includes(kw) || (f.project_name || '').toLowerCase().includes(kw))
})
function fmtSize(n: number | null): string {
  if (!n || n <= 0) return '-'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

// 二级 tab 的中文名 / 图标随一级 scope 切换：个人=文件夹 / 部门=文件柜 / 单位=文件室（按级别）
const levelLabels = computed(() => {
  if (activeScope.value === 'personal') return { core: '核心文件夹', important: '重要文件夹', general: '一般文件夹' }
  if (activeScope.value === 'department') return { core: '核心文件柜', important: '重要文件柜', general: '一般文件柜' }
  return { core: '核心文件室', important: '重要文件室', general: '一般文件室' }
})
const levelIcons = computed(() => {
  if (activeScope.value === 'personal') return { core: 'mdi-lock', important: 'mdi-archive', general: 'mdi-folder' }
  if (activeScope.value === 'department') return { core: 'mdi-shield-lock-outline', important: 'mdi-archive-outline', general: 'mdi-folder-outline' }
  return { core: 'mdi-safe', important: 'mdi-archive', general: 'mdi-folder-multiple' }
})
const activeTab = computed(() => {
  if (activeScope.value === 'department') {
    return ({ core: 'dept_core', important: 'dept_important', general: 'dept_general' } as const)[activeLevel.value]
  }
  return ({ core: 'core', important: 'important', general: 'general' } as const)[activeLevel.value]
})

// 对话框状态
const reasonDialog = ref(false)
const reasonText = ref('')
const currentAction = ref<{ type: 'view' | 'download', file: ArchiveFile | null }>({ type: 'view', file: null })

// Snackbar状态
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

// 表格列配置
const headers = [
  { title: '内容标题', key: 'content_title', sortable: true },
  { title: '摘要', key: 'content_summary', sortable: true, width: '300px' },
  { title: '文件类型', key: 'file_extension', sortable: true, width: '100px' },
  { title: '文件分类', key: 'archive_file_category', sortable: true, width: '120px' },
  { title: '操作', key: 'actions', sortable: false, width: '200px' },
]

// 获取文件后缀
const getFileExtension = (filename: string) => {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  return ext ? `.${ext}` : '-'
}

// Tab 配置：部门级（柜）+ 单位级（室）共 6 个。
// 演示阶段部门级数据未对接 manage 端 archive，暂时显示空列表。
const tabs = [
  { key: 'dept_core',      label: '部门保密柜', classification: '核心', scope: 'department', icon: 'mdi-shield-lock-outline' },
  { key: 'dept_important', label: '部门档案柜', classification: '重要', scope: 'department', icon: 'mdi-archive-outline' },
  { key: 'dept_general',   label: '部门资料柜', classification: '一般', scope: 'department', icon: 'mdi-folder-outline' },
  { key: 'core',           label: '单位保密室', classification: '核心', scope: 'unit',       icon: 'mdi-safe' },
  { key: 'important',      label: '单位档案室', classification: '重要', scope: 'unit',       icon: 'mdi-archive' },
  { key: 'general',        label: '单位资料室', classification: '一般', scope: 'unit',       icon: 'mdi-folder-multiple' },
]

// 单位级（室）核心/重要/一般 = 一键归档上报的 unit 柜室文件
const coreFiles = computed(() => cabinetCell('unit', '核心'))
const importantFiles = computed(() => cabinetCell('unit', '重要'))
const generalFiles = computed(() => cabinetCell('unit', '一般'))

// ===== 部门（柜）/ 单位（室）：一键归档上报到云端的柜室文件登记 =====
// 数据来自 manage 的 quick_archive_files（元数据登记，文件实体仍在原终端，故只列不借）。
const cabinetFiles = ref<QuickArchiveCabinetFile[]>([])
const loadCabinetFiles = async () => {
  try {
    cabinetFiles.value = await api.getQuickArchiveCabinetFiles() // 取全部（含部门+单位），客户端按 scope/级别分格
  } catch (e) {
    console.error('Failed to load cabinet files:', e)
    cabinetFiles.value = []
  }
}
const sensToClassification = (s: string): '核心' | '重要' | '一般' =>
  ({ core: '核心', core_secret: '核心', important: '重要', general: '一般' } as Record<string, '核心' | '重要' | '一般'>)[s] || '一般'

// 把柜室文件映射成表格用的 ArchiveFile 形状（带 _quick 标记 + scope 以便分格）。
const cabinetAsRows = computed(() => cabinetFiles.value.map((f) => ({
  id: f.id,
  application_name: '', applicant_unit: '', applicant_department: '', applicant_name: '', applicant_contact: '',
  archive_file_name: f.file_name,
  archive_file_category: f.storage_location || f.target_folder || '',
  archive_file_hash: f.checksum,
  application_time: f.archived_at,
  content_title: f.file_name,
  content_summary: `${f.project_name}　·　${f.storage_location || f.target_folder || ''}`,
  data_classification: sensToClassification(f.sensitivity_level),
  protection_method: 0,
  create_time: f.archived_at, update_time: f.archived_at,
  _quick: true, _scope: f.scope,
} as ArchiveFile & { _quick: boolean; _scope: string })))

// 按 scope + 级别 + 搜索词分格
const cabinetCell = (scope: 'department' | 'unit', cls: '核心' | '重要' | '一般') => {
  const kw = search.value.trim().toLowerCase()
  return cabinetAsRows.value.filter(f =>
    (f as any)._scope === scope && f.data_classification === cls &&
    (!kw || (f.content_title || '').toLowerCase().includes(kw) || (f.content_summary || '').toLowerCase().includes(kw)))
}
const deptCoreFiles = computed(() => cabinetCell('department', '核心'))
const deptImportantFiles = computed(() => cabinetCell('department', '重要'))
const deptGeneralFiles = computed(() => cabinetCell('department', '一般'))

// 当前tab显示的数据
const currentTabFiles = computed(() => {
  switch (activeTab.value) {
    case 'dept_core':      return deptCoreFiles.value
    case 'dept_important': return deptImportantFiles.value
    case 'dept_general':   return deptGeneralFiles.value
    case 'core':           return coreFiles.value
    case 'important':      return importantFiles.value
    case 'general':        return generalFiles.value
    default: return []
  }
})

// 当前 tab 的范围（部门 / 单位），用于空态文案
const currentScope = computed(() => {
  return tabs.find(t => t.key === activeTab.value)?.scope || 'unit'
})

// tab 上的计数 chip
function tabCount(key: string): number {
  switch (key) {
    case 'dept_core':      return deptCoreFiles.value.length
    case 'dept_important': return deptImportantFiles.value.length
    case 'dept_general':   return deptGeneralFiles.value.length
    case 'core':           return coreFiles.value.length
    case 'important':      return importantFiles.value.length
    case 'general':        return generalFiles.value.length
    default: return 0
  }
}

// 判断是否可以在线查看
const canView = (file: ArchiveFile) => {
  if ((file as any)._quick) return false // 云端登记（元数据），实体在原终端，暂不支持在线借阅
  // 只有核心数据且是PDF文件才能在线查看
  const isPdf = file.archive_file_name.toLowerCase().endsWith('.pdf')
  const isCore = file.data_classification === '核心'
  return isPdf && isCore
}

// 判断是否可以下载
const canDownload = (file: ArchiveFile) => {
  if ((file as any)._quick) return false // 云端登记（元数据），不支持下载
  // 核心文件不能下载
  // 重要、一般、公开数据都可以下载
  return file.data_classification !== '核心'
}

// 判断是否需要填写理由
const needReason = (file: ArchiveFile, action: 'view' | 'download') => {
  if (action === 'view') {
    // 在线查看需强制填写申请理由
    return true
  } else {
    // 下载时，重要数据需填写申请理由
    return file.data_classification === '重要'
  }
}

// 加载柜室登记列表
const loadArchiveFiles = async () => {
  loading.value = true
  try {
    await loadCabinetFiles()
  } finally {
    loading.value = false
  }
}

// 处理在线查看
const handleView = (file: ArchiveFile) => {
  if (!canView(file)) {
    showSnackbar('该文件不支持在线查看', 'warning')
    return
  }

  if (needReason(file, 'view')) {
    // 需要填写理由
    currentAction.value = { type: 'view', file }
    reasonText.value = ''
    reasonDialog.value = true
  } else {
    // 不需要理由，直接查看
    executeView(file, '')
  }
}

// 执行在线查看
const executeView = async (file: ArchiveFile, reason: string) => {
  const userInfo = await userInfoManager.getUserInfo()
  if (!userInfo) {
    showSnackbar('请先登录', 'warning')
    return
  }

  loading.value = true
  try {
    const blob = await api.borrowDownload({
      archive_id: file.id,
      borrower_name: userInfo.user_name,
      borrower_department: userInfo.company_name,
      borrow_reason: reason || undefined,
      borrow_method: 1  // 1=在线查看
    })

    // 将 blob 转换为 base64
    const arrayBuffer = await blob.arrayBuffer()
    const base64 = btoa(new Uint8Array(arrayBuffer).reduce((data, byte) => {
      return data + String.fromCharCode(byte)
    }, ''))

    // 在新窗口打开 PDF 查看器
    const url = router.resolve({
      name: 'PdfViewer',
      query: { data: base64 }
    }).href
    window.open(url, '_blank')

    showSnackbar('文件已在新窗口打开', 'success')
  } catch (error) {
    const message = error instanceof Error ? error.message : '在线查看失败'
    showSnackbar(message, 'error')
  } finally {
    loading.value = false
  }
}

// 处理下载
const handleDownload = (file: ArchiveFile) => {
  if (!canDownload(file)) {
    showSnackbar('核心文件不能下载', 'warning')
    return
  }

  if (needReason(file, 'download')) {
    // 需要填写理由
    currentAction.value = { type: 'download', file }
    reasonText.value = ''
    reasonDialog.value = true
  } else {
    // 不需要理由，直接下载
    executeDownload(file, '')
  }
}

// 执行下载
const executeDownload = async (file: ArchiveFile, reason: string) => {
  const userInfo = await userInfoManager.getUserInfo()
  if (!userInfo) {
    showSnackbar('请先登录', 'warning')
    return
  }

  loading.value = true
  try {
    // 一般文件下载时，如果没有填写理由，使用固定理由
    const finalReason = reason || (file.data_classification === '一般' ? '开放文件下载' : undefined)

    const blob = await api.borrowDownload({
      archive_id: file.id,
      borrower_name: userInfo.user_name,
      borrower_department: userInfo.company_name,
      borrow_reason: finalReason,
      borrow_method: 2  // 2=下载
    })

    // 创建下载链接
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = file.archive_file_name
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)

    showSnackbar('文件下载成功', 'success')
  } catch (error) {
    const message = error instanceof Error ? error.message : '文件下载失败'
    showSnackbar(message, 'error')
  } finally {
    loading.value = false
  }
}

// 确认理由对话框
const confirmReason = () => {
  if (!reasonText.value.trim()) {
    showSnackbar('请填写申请理由', 'warning')
    return
  }

  const { type, file } = currentAction.value
  if (!file) return

  reasonDialog.value = false

  if (type === 'view') {
    executeView(file, reasonText.value)
  } else {
    executeDownload(file, reasonText.value)
  }
}

// 显示提示
const showSnackbar = (text: string, color: string) => {
  snackbarText.value = text
  snackbarColor.value = color
  snackbar.value = true
}

// 级别/scope 切换：个人 → 读本机个人文件夹；部门/单位 → 刷新云端柜室登记
watch([activeScope, activeLevel], () => {
  if (activeScope.value === 'personal') loadPersonalResources()
  else loadCabinetFiles()
})

// 组件挂载时加载数据
onMounted(async () => {
  await loadArchiveFiles()
  await loadPersonalResources()
})
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-book-open-variant</v-icon>
        档案在线阅卷
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          按个人（夹）、部门级（柜）与单位级（室）分别浏览归档文件。个人为本机归档浏览，点击资源名称可打开文件；单位级核心 PDF 可在线查看，重要、一般、公开可下载。
        </div>
      </v-card-text>
    </v-card>

    <!-- 搜索栏 -->
    <v-card class="mb-4" elevation="1">
      <v-card-text>
        <v-text-field
          v-model="search"
          prepend-inner-icon="mdi-magnify"
          label="搜索标题或摘要"
          variant="outlined"
          density="compact"
          hide-details
          clearable
        />
      </v-card-text>
    </v-card>

    <!-- Tab页和文件表格 -->
    <v-card elevation="1">
      <!-- 一级：scope（个人 / 部门 / 单位） -->
      <v-tabs v-model="activeScope" color="primary" grow>
        <v-tab value="personal">
          <v-icon start>mdi-folder-account-outline</v-icon>
          个人
        </v-tab>
        <v-tab value="department">
          <v-icon start>mdi-office-building-outline</v-icon>
          部门
        </v-tab>
        <v-tab value="unit">
          <v-icon start>mdi-domain</v-icon>
          单位
        </v-tab>
      </v-tabs>

      <v-divider />

      <!-- 二级：classification（核心 / 重要 / 一般），名称随一级 scope 切换：个人=夹 / 部门=柜 / 单位=室 -->
      <v-tabs v-model="activeLevel" color="primary" density="compact" class="px-2">
        <v-tab value="core">
          <v-icon start>{{ levelIcons.core }}</v-icon>
          {{ levelLabels.core }}
          <v-chip v-if="activeScope !== 'personal'" size="x-small" class="ml-2" variant="tonal">
            {{ tabCount(activeScope === 'department' ? 'dept_core' : 'core') }}
          </v-chip>
        </v-tab>
        <v-tab value="important">
          <v-icon start>{{ levelIcons.important }}</v-icon>
          {{ levelLabels.important }}
          <v-chip v-if="activeScope !== 'personal'" size="x-small" class="ml-2" variant="tonal">
            {{ tabCount(activeScope === 'department' ? 'dept_important' : 'important') }}
          </v-chip>
        </v-tab>
        <v-tab value="general">
          <v-icon start>{{ levelIcons.general }}</v-icon>
          {{ levelLabels.general }}
          <v-chip v-if="activeScope !== 'personal'" size="x-small" class="ml-2" variant="tonal">
            {{ tabCount(activeScope === 'department' ? 'dept_general' : 'general') }}
          </v-chip>
        </v-tab>
      </v-tabs>

      <v-divider />

      <!-- 个人（夹）：本机「个人{级别}文件夹」里的一键归档文件 -->
      <template v-if="activeScope === 'personal'">
        <v-data-table
          :headers="personalHeaders"
          :items="personalFiltered"
          :loading="personalLoading"
          item-value="file_name"
          :items-per-page="-1"
          hide-default-footer
        >
          <template v-slot:item.file_name="{ item }">
            <div class="text-truncate" style="max-width: 360px" :title="item.file_name || '-'">
              <v-icon size="small" class="mr-2" color="grey">mdi-file-document-outline</v-icon>{{ item.file_name || '-' }}
            </div>
          </template>
          <template v-slot:item.file_size="{ item }">{{ fmtSize(item.file_size) }}</template>
          <template v-slot:no-data>
            <div class="text-center py-8">
              <v-icon size="64" color="grey-lighten-1">mdi-folder-open-outline</v-icon>
              <div class="mt-4 text-grey">暂无归档文件</div>
            </div>
          </template>
        </v-data-table>
        <div class="text-body-2 text-grey mt-3">共 {{ personalFiltered.length }} 个文件</div>
      </template>

      <!-- 部门（柜）/ 单位（室）：档案借阅 -->
      <v-data-table
        v-else
        :headers="headers"
        :items="currentTabFiles"
        :loading="loading"
        item-value="id"
        :items-per-page="-1"
        hide-default-footer
      >
        <template v-slot:item.content_title="{ item }">
          <div class="text-truncate" style="max-width: 300px" :title="item.content_title || '-'">
            {{ item.content_title || '-' }}
          </div>
        </template>

        <template v-slot:item.content_summary="{ item }">
          <div class="text-truncate" style="max-width: 280px" :title="item.content_summary || '-'">
            {{ item.content_summary || '-' }}
          </div>
        </template>

        <template v-slot:item.file_extension="{ item }">
          <v-chip size="small" variant="tonal" color="info">
            {{ getFileExtension(item.archive_file_name) }}
          </v-chip>
        </template>

        <template v-slot:item.actions="{ item }">
          <div v-if="(item as any)._quick" class="text-caption text-grey d-flex align-center">
            <v-icon size="small" class="mr-1">mdi-cloud-check-outline</v-icon>云端登记（在原终端借阅）
          </div>
          <div v-else class="d-flex gap-2">
            <v-btn
              size="small"
              variant="tonal"
              color="primary"
              :disabled="!canView(item)"
              @click="handleView(item)"
            >
              <v-icon start size="small">mdi-eye</v-icon>
              在线查看
            </v-btn>
            <v-btn
              size="small"
              variant="tonal"
              color="success"
              :disabled="!canDownload(item)"
              @click="handleDownload(item)"
            >
              <v-icon start size="small">mdi-download</v-icon>
              下载
            </v-btn>
          </div>
        </template>

        <template v-slot:no-data>
          <div class="text-center py-8">
            <v-icon size="64" color="grey-lighten-1">mdi-folder-open-outline</v-icon>
            <div class="mt-4 text-grey">暂无归档文件</div>
          </div>
        </template>
      </v-data-table>
    </v-card>

    <!-- 申请理由对话框 -->
    <v-dialog v-model="reasonDialog" max-width="500">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-text-box</v-icon>
          填写申请理由
        </v-card-title>
        <v-card-text>
          <v-textarea
            v-model="reasonText"
            label="申请理由"
            placeholder="请填写借阅申请理由"
            variant="outlined"
            rows="4"
            auto-grow
            :rules="[v => !!v || '申请理由不能为空']"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="reasonDialog = false">取消</v-btn>
          <v-btn color="primary" variant="tonal" @click="confirmReason">确认</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 提示消息 -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" :timeout="3000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>

<style scoped>
.cursor-pointer {
  cursor: pointer;
}
.resource-name-cell:hover {
  background-color: rgba(var(--v-theme-primary), 0.08);
  border-radius: 4px;
  padding: 4px 8px;
  margin: -4px -8px;
}
</style>
