<template>
  <div>
    <v-card flat>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-folder-multiple</v-icon>
        数据业务项目
        <v-spacer />
        <v-btn color="success" variant="tonal" prepend-icon="mdi-archive-arrow-down" class="mr-2" :loading="archivingAll" @click="onQuickArchiveAll">一键归档全部</v-btn>
        <v-btn color="primary" prepend-icon="mdi-plus" @click="$router.push('/projects/new')">新建项目立项</v-btn>
      </v-card-title>
      <v-card-subtitle>选用模版生成项目卷宗，纳入统一编码、目录、底账与生命周期管理</v-card-subtitle>

      <v-card-text>
        <div class="d-flex gap-3 mb-4 align-center">
          <v-select
            v-model="filterStatus"
            :items="statusOptions"
            label="状态"
            density="compact"
            style="max-width: 220px"
            hide-details
            @update:model-value="loadList"
          />
          <v-text-field
            v-model="keyword"
            label="项目编码或名称"
            density="compact"
            hide-details
            style="max-width: 280px"
            @keyup.enter="loadList"
          />
          <v-btn variant="text" @click="loadList">
            <v-icon>mdi-refresh</v-icon> 刷新
          </v-btn>
        </div>

        <v-data-table
          :headers="headers"
          :items="visibleList"
          :loading="loading"
          density="compact"
          items-per-page="20"
        >
          <template v-slot:item.project_code="{ item }">
            <span class="font-monospace text-primary">{{ item.project_code }}</span>
          </template>
          <template v-slot:item.sensitivity_level="{ item }">
            <v-chip :color="sensColor(item.sensitivity_level)" size="small" variant="tonal">
              {{ sensLabel(item.sensitivity_level) }}
            </v-chip>
          </template>
          <template v-slot:item.status="{ item }">
            <v-chip :color="statusColor(item.status)" size="small" variant="tonal">
              {{ statusLabel(item.status) }}
            </v-chip>
          </template>
          <template v-slot:item.update_time="{ item }">
            <span class="text-caption">{{ formatTime(item.update_time) }}</span>
          </template>
          <template v-slot:item.actions="{ item }">
            <v-btn size="x-small" variant="text" color="primary" @click="$router.push(`/projects/${item.id}`)">
              <v-icon>mdi-eye</v-icon> 工作台
            </v-btn>
            <!-- V3-8 §8.2: 项目激活 / 取消的 UI 入口 -->
            <v-btn
              v-if="item.status === 'draft'"
              size="x-small"
              variant="text"
              color="success"
              :loading="busyId === item.id"
              @click="onActivate(item)"
            >
              <v-icon>mdi-power</v-icon> 激活
            </v-btn>
            <v-btn
              v-if="item.status === 'draft' || item.status === 'active'"
              size="x-small"
              variant="text"
              color="error"
              :loading="busyId === item.id"
              @click="onCancel(item)"
            >
              <v-icon>mdi-close-circle-outline</v-icon> 取消
            </v-btn>
          </template>
        </v-data-table>

        <div v-if="!loading && visibleList.length === 0" class="text-center text-medium-emphasis py-12">
          暂无项目，点击右上角"新建项目立项"开始
        </div>
      </v-card-text>
    </v-card>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
      {{ snackbar.text }}
    </v-snackbar>

    <!-- V3-A 激活确认 -->
    <v-dialog v-model="activateDialog.show" max-width="480">
      <v-card v-if="activateDialog.item">
        <v-card-title>
          <v-icon class="mr-2" color="success">mdi-power</v-icon>
          确认激活项目
        </v-card-title>
        <v-card-text>
          确定激活项目「<strong>{{ activateDialog.item.project_name }}</strong>」吗？
          <div class="text-caption text-medium-emphasis mt-2">
            激活后项目从草稿（draft）变为执行中（active），可正常上传文件、提交产出。
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="busyId !== 0" @click="activateDialog.show = false">取消</v-btn>
          <v-btn color="success" :loading="busyId === activateDialog.item.id" @click="doActivate">确认激活</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- V3-A 取消确认 + 收原因 -->
    <v-dialog v-model="cancelDialog.show" max-width="480">
      <v-card v-if="cancelDialog.item">
        <v-card-title>
          <v-icon class="mr-2" color="error">mdi-close-circle-outline</v-icon>
          取消项目
        </v-card-title>
        <v-card-text>
          确定取消项目「<strong>{{ cancelDialog.item.project_name }}</strong>」吗？
          <v-alert type="warning" variant="tonal" density="compact" class="my-2">
            已取消的项目不可恢复，但可在列表筛选状态为"已取消"中查到。
          </v-alert>
          <v-textarea
            v-model="cancelDialog.reason"
            label="取消原因（可选）"
            rows="3"
            density="compact"
            variant="outlined"
            placeholder="如：需求变更、模版选错、合同终止 ..."
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="busyId !== 0" @click="cancelDialog.show = false">关闭</v-btn>
          <v-btn color="error" :loading="busyId === cancelDialog.item.id" @click="doCancel">确认取消</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { projectsApi, type DataProject } from '@/services/projectsApi'

const list = ref<DataProject[]>([])
const visibleList = computed(() => list.value.filter(p => !p.project_code.startsWith('SYS-PERSONAL-')))
const loading = ref(false)
const filterStatus = ref('')
const keyword = ref('')
const snackbar = ref({ show: false, text: '', color: 'success' })
const busyId = ref<number>(0) // 行内激活/取消按钮防双击
const archivingAll = ref(false)

// 一键归档全部：按九宫格分流（个人→本地夹 / 部门、单位→上报云端 / 行业→跳过）。
async function onQuickArchiveAll() {
  archivingAll.value = true
  try {
    const r = await projectsApi.quickArchiveAll()
    const errs = (r.projects || []).flatMap(p => p.errors || [])
    const tip = `归档完成：新归档 ${r.total_archived} 个、跳过 ${r.total_skipped} 个` + (errs.length ? `，${errs.length} 个错误` : '')
    snackbar.value = { show: true, text: tip, color: errs.length ? 'warning' : 'success' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '一键归档失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    archivingAll.value = false
  }
}

// V3-A 激活/取消用 Vuetify 对话框（Wails WebView 不支持 window.confirm / prompt）
const activateDialog = ref<{ show: boolean; item: DataProject | null }>({ show: false, item: null })
const cancelDialog = ref<{ show: boolean; item: DataProject | null; reason: string }>({
  show: false, item: null, reason: '',
})

function onActivate(item: DataProject) {
  activateDialog.value = { show: true, item }
}

async function doActivate() {
  const item = activateDialog.value.item
  if (!item) return
  busyId.value = item.id
  try {
    await projectsApi.activate(item.id)
    snackbar.value = { show: true, text: '激活成功', color: 'success' }
    activateDialog.value.show = false
    await loadList()
  } catch (e: any) {
    snackbar.value = { show: true, text: '激活失败：' + e.message, color: 'error' }
  } finally {
    busyId.value = 0
  }
}

function onCancel(item: DataProject) {
  cancelDialog.value = { show: true, item, reason: '' }
}

async function doCancel() {
  const item = cancelDialog.value.item
  if (!item) return
  busyId.value = item.id
  try {
    await projectsApi.cancel(item.id, cancelDialog.value.reason || undefined)
    snackbar.value = { show: true, text: '已取消', color: 'success' }
    cancelDialog.value.show = false
    await loadList()
  } catch (e: any) {
    snackbar.value = { show: true, text: '取消失败：' + e.message, color: 'error' }
  } finally {
    busyId.value = 0
  }
}

const headers = [
  { title: '项目编码', key: 'project_code' },
  { title: '项目名称', key: 'project_name' },
  { title: '模版', key: 'template_code' },
  { title: '版本', key: 'template_version' },
  { title: '敏感等级', key: 'sensitivity_level' },
  { title: '状态', key: 'status' },
  { title: '更新时间', key: 'update_time' },
  { title: '操作', key: 'actions', sortable: false, width: 260 },
]

const statusOptions = [
  { title: '全部', value: '' },
  { title: '草稿', value: 'draft' },
  { title: '执行中', value: 'active' },
  { title: '结项中', value: 'closing' },
  { title: '已归档', value: 'archived' },
  { title: '已取消', value: 'cancelled' },
]

function sensColor(s: string): string {
  return ({ general: 'default', important: 'warning', core_secret: 'error' } as Record<string, string>)[s] || 'default'
}
function sensLabel(s: string): string {
  return ({ general: '一般', important: '重要', core_secret: '核心(涉密)' } as Record<string, string>)[s] || s
}
function statusColor(s: string): string {
  return ({ draft: 'default', active: 'success', closing: 'warning', archived: 'info', cancelled: 'error' } as Record<string, string>)[s] || 'default'
}
function statusLabel(s: string): string {
  return ({ draft: '草稿', active: '执行中', closing: '结项中', archived: '已归档', cancelled: '已取消' } as Record<string, string>)[s] || s
}
function formatTime(t: string | null): string {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

async function loadList() {
  loading.value = true
  try {
    list.value = await projectsApi.list({ status: filterStatus.value || undefined, keyword: keyword.value || undefined })
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载失败：' + e.message, color: 'error' }
  } finally {
    loading.value = false
  }
}

onMounted(loadList)
</script>
