<template>
  <div>
    <v-card flat>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-clipboard-text-clock</v-icon>
        审计日志
        <v-spacer />
        <v-btn variant="text" size="small" @click="loadList">
          <v-icon>mdi-refresh</v-icon> 刷新
        </v-btn>
      </v-card-title>

      <v-card-text>
        <div class="d-flex gap-2 mb-3 flex-wrap align-end">
          <v-select
            v-model="filter.action"
            :items="actionOptions"
            label="动作类型"
            density="compact"
            variant="outlined"
            hide-details
            clearable
            style="max-width: 240px"
            @update:model-value="loadList"
          />
          <v-select
            v-model="filter.target_type"
            :items="targetTypeOptions"
            label="目标类型"
            density="compact"
            variant="outlined"
            hide-details
            clearable
            style="max-width: 180px"
            @update:model-value="loadList"
          />
          <v-text-field
            v-model="filter.actor_id"
            label="操作人"
            density="compact"
            variant="outlined"
            hide-details
            clearable
            style="max-width: 160px"
            @keyup.enter="loadList"
          />
          <v-text-field
            v-model.number="filter.limit"
            label="数量上限"
            type="number"
            density="compact"
            variant="outlined"
            hide-details
            style="max-width: 100px"
            @keyup.enter="loadList"
          />
          <v-btn color="primary" @click="loadList">查询</v-btn>
        </div>

        <v-data-table
          :headers="headers"
          :items="list"
          :loading="loading"
          density="compact"
          items-per-page="50"
        >
          <template #item.create_time="{ item }">
            <span class="text-caption font-monospace">{{ formatTime(item.create_time) }}</span>
          </template>
          <template #item.action="{ item }">
            <v-chip size="x-small" variant="tonal" :color="actionColor(item.action)">
              {{ actionLabel(item.action) }}
            </v-chip>
          </template>
          <template #item.target="{ item }">
            <span class="text-caption text-medium-emphasis">{{ targetTypeLabel(item.target_type) }}</span>
            <span class="font-monospace text-caption ml-1">#{{ item.target_id || '-' }}</span>
            <span v-if="item.target_code" class="ml-1 text-caption">{{ item.target_code }}</span>
          </template>
          <template #item.actor="{ item }">
            <span class="font-monospace">{{ item.actor_id }}</span>
            <span v-if="item.actor_user_id" class="text-caption text-medium-emphasis ml-1">
              (user#{{ item.actor_user_id }})
            </span>
          </template>
          <template #item.message="{ item }">
            <span class="text-caption">{{ item.message || '-' }}</span>
          </template>
          <template #item.actions="{ item }">
            <v-btn size="x-small" variant="text" @click="openDetail(item)">
              <v-icon>mdi-eye</v-icon>
            </v-btn>
          </template>
        </v-data-table>

        <div v-if="!loading && list.length === 0" class="text-center text-medium-emphasis py-12">
          暂无审计记录
        </div>
      </v-card-text>
    </v-card>

    <!-- 详情对话框：展示 before/after JSON -->
    <v-dialog v-model="detailOpen" max-width="720">
      <v-card v-if="detail">
        <v-card-title class="d-flex align-center">
          <v-chip :color="actionColor(detail.action)" size="small" variant="tonal" class="mr-2">
            {{ actionLabel(detail.action) }}
          </v-chip>
          {{ targetTypeLabel(detail.target_type) }} #{{ detail.target_id }}
          <v-spacer />
          <v-btn icon variant="text" @click="detailOpen = false"><v-icon>mdi-close</v-icon></v-btn>
        </v-card-title>
        <v-card-text>
          <div class="text-caption mb-1">操作人：<code>{{ detail.actor_id }}</code>
            <span v-if="detail.actor_user_id"> · users.id={{ detail.actor_user_id }}</span>
          </div>
          <div class="text-caption mb-1">时间：{{ formatTime(detail.create_time) }}</div>
          <div v-if="detail.ip_address" class="text-caption mb-1">IP：{{ detail.ip_address }}</div>
          <div v-if="detail.message" class="text-body-2 my-2">{{ detail.message }}</div>

          <template v-if="detail.before_json">
            <v-divider class="my-3" />
            <div class="text-caption text-medium-emphasis mb-1">变更前</div>
            <pre class="text-caption pa-2 bg-grey-lighten-4 rounded" style="white-space: pre-wrap; word-break: break-all">{{ prettyJson(detail.before_json) }}</pre>
          </template>
          <template v-if="detail.after_json">
            <v-divider class="my-3" />
            <div class="text-caption text-medium-emphasis mb-1">变更后</div>
            <pre class="text-caption pa-2 bg-grey-lighten-4 rounded" style="white-space: pre-wrap; word-break: break-all">{{ prettyJson(detail.after_json) }}</pre>
          </template>
        </v-card-text>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'

interface AuditLog {
  id: number
  actor_id: string
  actor_user_id: number | null
  action: string
  target_type: string
  target_id: number | null
  target_code: string | null
  before_json: string | null
  after_json: string | null
  ip_address: string | null
  message: string | null
  create_time: string
}

const API_BASE = 'http://127.0.0.1:3001'
const list = ref<AuditLog[]>([])
const loading = ref(false)
const filter = ref<{ action: string; target_type: string; actor_id: string; limit: number }>({
  action: '', target_type: '', actor_id: '', limit: 200,
})

// §11.1 八类操作 Action 选项
const actionOptions = [
  { title: '全部', value: '' },
  { title: '模版-发布', value: 'template_publish' },
  { title: '模版-废弃', value: 'template_deprecate' },
  { title: '模版-复制', value: 'template_copy' },
  { title: '模版-导入', value: 'template_import' },
  { title: '模版-升级版本', value: 'template_upgrade_version' },
  { title: '项目-立项', value: 'project_create' },
  { title: '项目-激活', value: 'project_activate' },
  { title: '项目-结项', value: 'project_close' },
  { title: '项目-取消', value: 'project_cancel' },
  { title: '成员-添加', value: 'member_add' },
  { title: '成员-修改', value: 'member_update' },
  { title: '成员-移除', value: 'member_remove' },
  { title: '过户', value: 'subject_handover' },
  { title: '导出底账', value: 'export_ledger' },
  { title: '导出归档包', value: 'export_archive' },
]
const targetTypeOptions = [
  { title: '全部', value: '' },
  { title: '模版', value: 'template' },
  { title: '项目', value: 'project' },
  { title: '项目成员', value: 'project_member' },
  { title: '底账', value: 'ledger' },
  { title: '文件版本', value: 'file_version' },
  { title: '导出', value: 'export' },
]

const headers = [
  { title: '时间', key: 'create_time', width: 160 },
  { title: '动作', key: 'action', width: 130 },
  { title: '目标', key: 'target', width: 260, sortable: false },
  { title: '操作人', key: 'actor', width: 160 },
  { title: '说明', key: 'message', sortable: false },
  { title: '', key: 'actions', sortable: false, width: 60 },
]

function actionLabel(a: string): string {
  return (actionOptions.find(o => o.value === a)?.title || a).replace(/^.*-/, '')
}
function actionColor(a: string): string {
  if (a.startsWith('template_')) return 'info'
  if (a.startsWith('project_')) return 'primary'
  if (a.startsWith('member_')) return 'warning'
  if (a.startsWith('export_')) return 'success'
  if (a.startsWith('subject_')) return 'error'
  return 'default'
}
function targetTypeLabel(t: string): string {
  return ({
    template: '模版',
    project: '项目',
    project_member: '项目成员',
    ledger: '底账',
    file_version: '文件版本',
    export: '导出',
  } as Record<string, string>)[t] || t
}
function formatTime(t: string): string {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}
function prettyJson(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}

async function loadList() {
  loading.value = true
  try {
    const q = new URLSearchParams()
    if (filter.value.action) q.set('action', filter.value.action)
    if (filter.value.target_type) q.set('target_type', filter.value.target_type)
    if (filter.value.actor_id) q.set('actor_id', filter.value.actor_id)
    if (filter.value.limit > 0) q.set('limit', String(filter.value.limit))
    const res = await fetch(`${API_BASE}/audit-logs?${q.toString()}`)
    const json = await res.json()
    list.value = json.success ? (json.data || []) : []
  } finally {
    loading.value = false
  }
}

const detailOpen = ref(false)
const detail = ref<AuditLog | null>(null)
function openDetail(item: AuditLog) {
  detail.value = item
  detailOpen.value = true
}

onMounted(loadList)
</script>
