<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { api, type DataResource, type FamilyMembersResponse, type FamilyMemberDetail } from '@/services/api'

type FamilyGroupKey = 'primary' | 'same_content' | 'process_version' | 'derived'

// 状态
const loading = ref(false)
const resources = ref<DataResource[]>([])
const search = ref('')
const page = ref(1)
const pageSize = ref(50)
const total = ref(0)

// 副本 / 家族对话框（与 ClaimView 同款）
const showCopiesDialog = ref(false)
const copiesLoading = ref(false)
const currentResourceName = ref('')
const currentCopies = ref<Array<{ path: string; file_size?: number }>>([])
const currentFamily = ref<FamilyMembersResponse | null>(null)
const currentFamilyGroup = ref<FamilyGroupKey | 'all'>('all')
const currentDialogMode = ref<'copies' | 'family'>('copies')

// 打开文件状态
const openingFile = ref(false)

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

// 表格列配置（与 ClaimView 一致）
const headers = [
  { title: '资源名称', key: 'resources_name', sortable: true },
  { title: '最早创建时间', key: 'first_create_time', sortable: true, width: '180px' },
  { title: '相同文件', key: 'family_same_content_count', sortable: true, width: '100px' },
  { title: '过程文件', key: 'family_process_version_count', sortable: true, width: '100px' },
  { title: '衍生文件', key: 'family_derived_count', sortable: true, width: '100px' },
  { title: '分布数量', key: 'source_count', sortable: true, width: '100px' },
]

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

// 计算总页数
const totalPages = computed(() => {
  return Math.ceil(total.value / pageSize.value)
})

// 加载资源列表
const loadResources = async () => {
  loading.value = true
  try {
    const result = await api.getResources({
      search: search.value || undefined,
      page: page.value,
      pageSize: pageSize.value,
      claimStatusFilter: 1, // 1=个人隐私
      groupByFamily: true,
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

// 查看家族成员（按 chip 点击的分组过滤）
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

// 显示提示
const showSnackbar = (text: string, color: string) => {
  snackbarText.value = text
  snackbarColor.value = color
  snackbar.value = true
}

// 监听搜索条件变化
watch(search, () => {
  page.value = 1
  loadResources()
})

// 监听分页变化
watch([page, pageSize], () => {
  loadResources()
})

// 组件挂载时加载数据
onMounted(async () => {
  await loadResources()
})
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-shield-account</v-icon>
        个人隐私保护
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          查看和管理认领为「个人隐私」的资源。点击资源名称可在本机打开文件；若该资源属于族群，可点击"相同/过程/衍生"列查看族群成员。
        </div>
      </v-card-text>
    </v-card>

    <!-- 搜索栏 -->
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
          <v-col cols="12" md="8" class="d-flex align-center justify-end">
            <span class="text-body-2 text-grey">
              共 {{ total }} 条记录
            </span>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <!-- 资源表格（与 ClaimView 同款，附族群分组列） -->
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
            <div
              class="text-truncate resource-name-link"
              style="max-width: 350px"
              :title="item.resources_name || '-'"
              @click="handleOpenFile(item)"
            >
              {{ item.resources_name || '-' }}
            </div>
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

        <template v-slot:item.source_count="{ item }">
          <v-chip size="small" variant="tonal" color="grey">
            {{ item.source_count }}
          </v-chip>
        </template>

        <template v-slot:no-data>
          <div class="text-center py-8">
            <v-icon size="64" color="grey-lighten-1">mdi-shield-off-outline</v-icon>
            <div class="mt-4 text-grey">暂无隐私资源数据</div>
            <div class="mt-2 text-caption text-grey">在「扫描结果责任认领」中将文件认领为「个人隐私」后会出现在此处</div>
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

    <!-- 副本列表 / 家族成员对话框（与 ClaimView 同款） -->
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
              hide-default-footer
              density="compact"
            >
              <template #item.path="{ item }">
                <div>
                  <div class="text-body-2">{{ item.path || item.resources_name || '-' }}</div>
                </div>
              </template>
              <template #item.family_score="{ item }">
                <span v-if="item.family_score != null">{{ Math.round((item.family_score || 0) * 100) }}%</span>
                <span v-else class="text-caption text-medium-emphasis">-</span>
              </template>
            </v-data-table>
          </template>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="showCopiesDialog = false">关闭</v-btn>
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
</style>
