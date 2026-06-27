<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { API_BASE } from '@/services/api'

interface RemoteTemplate {
  id: number
  template_code: string
  template_name: string
  template_version: string
  status: string
}

const loading = ref(false)
const items = ref<RemoteTemplate[]>([])
const error = ref('')

const headers = [
  { title: '编码', key: 'template_code', width: '200px' },
  { title: '名称', key: 'template_name' },
  { title: '版本', key: 'template_version', width: '100px' },
  { title: '状态', key: 'status', width: '100px' },
]

const snackbar = ref({ show: false, text: '', color: 'success' })

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r = await fetch(`${API_BASE}/templates/remote-list`)
    const j = await r.json()
    if (j.success) {
      items.value = j.data || []
    } else {
      error.value = j.error || '加载模板列表失败'
    }
  } catch (e: any) {
    error.value = e?.message || String(e)
  } finally {
    loading.value = false
  }
}

async function syncOne(item: RemoteTemplate) {
  try {
    // 总览列表来自模版管理平台(remote-list)：按 code 回到同一台服务器同步，
    // 不能用 item.id（平台 id）去 manage 查。
    const r = await fetch(`${API_BASE}/templates/sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ source: 'template-server', code: item.template_code, version: item.template_version }),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: `已同步：${item.template_code} ${item.template_version}`, color: 'success' }
    } else {
      snackbar.value = { show: true, text: '同步失败：' + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '同步失败：' + (e?.message || String(e)), color: 'error' }
  }
}

onMounted(load)
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-file-document-multiple-outline</v-icon>
      数据业务模版总览
      <v-spacer />
      <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
    </v-card-title>
    <v-card-subtitle>从 manage 端拉取的 status=active 模版列表。可选择"同步到本地"以便在立项向导里使用。</v-card-subtitle>

    <v-alert v-if="error" type="error" variant="tonal" density="compact" class="ma-3">
      {{ error }}
      <div class="mt-1 text-caption">请检查 Settings 里的 manage_endpoint 配置是否正确，以及 manage 服务是否可达。</div>
    </v-alert>

    <v-data-table
      :headers="headers"
      :items="items"
      :loading="loading"
      item-value="id"
      :items-per-page="50"
      hide-default-footer
    >
      <template #item.status="{ item }">
        <v-chip size="x-small" color="success" variant="tonal">{{ item.status }}</v-chip>
      </template>
      <template #item.template_code="{ item }">
        <code class="text-body-2">{{ item.template_code }}</code>
      </template>
      <template v-slot:no-data>
        <div class="text-center py-8">
          <v-icon size="64" color="grey-lighten-1">mdi-cloud-off-outline</v-icon>
          <div class="mt-4 text-grey">暂无模板，或 manage 端不可达</div>
        </div>
      </template>
      <template #item.actions="{ item }">
        <v-btn size="x-small" variant="text" color="primary" @click="syncOne(item)">同步到本地</v-btn>
      </template>
    </v-data-table>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>
