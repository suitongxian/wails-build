<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { api, type DataResource } from '@/services/api'
import { saveTabState, loadTabState } from '@/services/TabStateManager'

// 选项卡类型（"privacy" 已下线为独立菜单 /privacy，此页不再展示）
type TabType = 'core' | 'important' | 'open'

// 当前选中的选项卡
const activeTab = ref<TabType>('core')

// 状态
const loading = ref(false)
const resources = ref<DataResource[]>([])
const search = ref('')
const page = ref(1)
const pageSize = ref(50)
const total = ref(0)

// Snackbar状态
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

// 表格列配置
const headers = [
  { title: '资源类目', key: 'content_subject', sortable: true, width: '120px' },
  { title: '资源名称', key: 'resources_name', sortable: true },
  { title: '资源摘要', key: 'resources_desc', sortable: true },
  { title: '资源筛选', key: 'importance_level', sortable: true, width: '140px' },
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

// 获取当前选项卡对应的重要程度值
const getImportanceLevelForTab = (tab: TabType): number => {
  switch (tab) {
    case 'core':
      return 1
    case 'important':
      return 2
    case 'open':
      return 3
    default:
      return 0
  }
}

// 打开文件
const handleOpenFile = async (contentSign: string) => {
  try {
    await api.openFile(contentSign)
    showSnackbar('文件已打开', 'success')
  } catch (error) {
    const message = error instanceof Error ? error.message : '打开文件失败'
    showSnackbar(message, 'error')
  }
}

// 加载资源列表
const loadResources = async () => {
  loading.value = true
  try {
    const importanceLevel = getImportanceLevelForTab(activeTab.value)
    const result = await api.getResources({
      search: search.value || undefined,
      page: page.value,
      pageSize: pageSize.value,
      importanceLevelFilter: importanceLevel,
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

// 监听搜索和选项卡变化
watch([search, activeTab], () => {
  page.value = 1
  loadResources()
})

// 监听分页变化
watch([page, pageSize], () => {
  loadResources()
})

// 组件挂载时加载数据
onMounted(async () => {
  // 加载选项卡状态
  const savedTab = loadTabState()
  if (savedTab && ['core', 'important', 'open'].includes(savedTab)) {
    activeTab.value = savedTab as TabType
  }
  await loadResources()
})
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-folder-eye</v-icon>
        本机归档文件浏览
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          按文件级别分类分区存储查询，点击资源名称可打开文件
        </div>
      </v-card-text>
    </v-card>

    <!-- 选项卡 -->
    <div class="d-flex align-center mb-4">
      <v-tabs v-model="activeTab" color="primary">
        <v-tab value="core">
          <v-icon start>mdi-lock</v-icon>
          保密柜（核心要件）
        </v-tab>
        <v-tab value="important">
          <v-icon start>mdi-archive</v-icon>
          档案柜（重要文件）
        </v-tab>
        <v-tab value="open">
          <v-icon start>mdi-folder</v-icon>
          资料柜（开放文件）
        </v-tab>
      </v-tabs>
      <v-spacer />
      <span class="text-body-2 text-grey">
        共 {{ total }} 条记录
      </span>
    </div>

    <!-- 搜索栏 -->
    <v-card class="mb-4" elevation="1">
      <v-card-text>
        <v-row align="center">
          <v-col cols="12" md="6">
            <v-text-field
              v-model="search"
              prepend-inner-icon="mdi-magnify"
              label="搜索资源名称或摘要"
              variant="outlined"
              density="compact"
              hide-details
              clearable
            />
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
      <template v-slot:item.content_subject="{ item }">
        <div class="d-flex align-center">
          <v-icon size="small" class="mr-2" color="primary">
            mdi-folder
          </v-icon>
          <span>{{ item.content_subject || '-' }}</span>
        </div>
      </template>
      <template v-slot:item.resources_name="{ item }">
        <div
          class="text-truncate resource-name-cell cursor-pointer"
          style="max-width: 350px"
          :title="item.resources_name || '-'"
          @click="handleOpenFile(item.content_sign)"
        >
        <v-icon size="small" class="mr-2" color="grey">
          mdi-file-document-outline
        </v-icon>
          <span class="text-primary text-decoration-underline">
            {{ item.resources_name || '-' }}
          </span>
        </div>
      </template>
      <template v-slot:item.resources_desc="{ item }">
        <div
          class="text-truncate"
          style="max-width: 500px"
          :title="item.resources_desc || '-'"
          >
        {{ item.resources_desc || '-' }}
        </div>
      </template>
      <template v-slot:no-data>
        <div class="text-center py-8">
          <v-icon size="64" color="grey-lighten-1">mdi-folder-open-outline</v-icon>
          <div class="mt-4 text-grey">暂无资源数据</div>
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