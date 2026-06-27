<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-shield-lock</v-icon>
      核心登记
      <v-chip size="x-small" color="error" variant="tonal" class="ml-2">人工登记</v-chip>
    </v-card-title>

    <v-tabs v-model="currentTab" color="primary" class="px-4">
      <v-tab value="pending">
        待登记
        <v-badge inline :content="pendingTotal" :model-value="pendingTotal > 0" class="ml-2" />
      </v-tab>
      <v-tab value="registered">
        已登记
        <v-badge inline :content="registeredTotal" :model-value="registeredTotal > 0" class="ml-2" />
      </v-tab>
    </v-tabs>

    <v-window v-model="currentTab">
      <v-window-item value="pending">
        <v-card-text>
          <v-alert type="info" variant="tonal" density="compact" class="mb-3">
            核心级资料一律人工登记。登记后该条目不可再修改。
          </v-alert>
          <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-2" />
          <div v-if="!loading && items.length === 0" class="text-center text-medium-emphasis py-12">
            <v-icon size="64" color="grey-lighten-1">mdi-shield-check-outline</v-icon>
            <div class="mt-2">暂无待登记的核心级资料</div>
          </div>
          <v-list density="compact" v-if="items.length > 0">
            <v-list-item v-for="item in items" :key="item.ledger_id" lines="two">
              <v-list-item-title>{{ item.asset_name }}</v-list-item-title>
              <v-list-item-subtitle>{{ item.file_version_code }} · 创建 {{ formatTime(item.create_time) }}</v-list-item-subtitle>
              <template #append>
                <v-btn color="primary" variant="tonal" size="small" @click="openRegister(item)">登记</v-btn>
              </template>
            </v-list-item>
          </v-list>
        </v-card-text>
      </v-window-item>

      <v-window-item value="registered">
        <v-card-text>
          <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-2" />
          <div v-if="!loading && items.length === 0" class="text-center text-medium-emphasis py-12">
            <v-icon size="64" color="grey-lighten-1">mdi-clipboard-text-outline</v-icon>
            <div class="mt-2">暂无已登记记录</div>
          </div>
          <v-list density="compact" v-if="items.length > 0">
            <v-list-item v-for="item in items" :key="item.ledger_id" lines="three">
              <v-list-item-title>{{ item.asset_name }}</v-list-item-title>
              <v-list-item-subtitle>
                主题：<strong>{{ item.topic || '-' }}</strong> · 密级：<strong>{{ item.classification || '-' }}</strong>
                · 登记：{{ formatTime(item.registered_at) }}
              </v-list-item-subtitle>
            </v-list-item>
          </v-list>
        </v-card-text>
      </v-window-item>
    </v-window>

    <v-dialog v-model="registerDialog.open" max-width="540">
      <v-card>
        <v-card-title>核心级资料登记</v-card-title>
        <v-card-text>
          <div class="text-body-2 mb-3 text-medium-emphasis">资源：{{ registerDialog.assetName }}</div>
          <v-text-field v-model="registerDialog.topic" label="工作主题 *" density="compact" />
          <v-select v-model="registerDialog.classification" :items="['内部', '秘密', '机密']" label="密级 *" density="compact" />
          <v-textarea v-model="registerDialog.note" label="备注" rows="2" density="compact" />
          <v-text-field v-model="registerDialog.password" label="登录密码（用于签字）*" type="password" density="compact" />
          <div class="text-caption text-medium-emphasis mt-2">登记后该记录不可再修改。</div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="registerDialog.open = false">取消</v-btn>
          <v-btn color="primary" :loading="registerDialog.busy" :disabled="!canRegister" @click="doRegister">提交登记</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { API_BASE } from '../services/api'

interface MemorandumItem {
  ledger_id: number
  asset_name: string
  file_version_code: string
  create_time: string
  topic?: string | null
  classification?: string | null
  registered_at?: string | null
}

const currentTab = ref<'pending' | 'registered'>('pending')
const items = ref<MemorandumItem[]>([])
const loading = ref(false)
const pendingTotal = ref(0)
const registeredTotal = ref(0)
const snackbar = ref({ show: false, text: '', color: 'success' })

const registerDialog = ref({
  open: false,
  busy: false,
  ledgerId: 0,
  assetName: '',
  topic: '',
  classification: '',
  note: '',
  password: '',
})

const canRegister = computed(() =>
  !!registerDialog.value.topic.trim() &&
  !!registerDialog.value.classification &&
  !!registerDialog.value.password,
)

function formatTime(s: string | null | undefined): string {
  if (!s) return '-'
  return String(s).substring(0, 19).replace('T', ' ')
}

async function load() {
  loading.value = true
  try {
    const url = currentTab.value === 'pending' ? '/memorandum/pending' : '/memorandum/registered'
    const r = await fetch(`${API_BASE}${url}?page=1&page_size=50`)
    const j = await r.json()
    if (j.success) {
      items.value = j.data?.items || []
      if (currentTab.value === 'pending') pendingTotal.value = j.data?.total || 0
      else registeredTotal.value = j.data?.total || 0
    }
  } finally {
    loading.value = false
  }
}

async function warmInactive() {
  try {
    const other = currentTab.value === 'pending' ? '/memorandum/registered' : '/memorandum/pending'
    const r = await fetch(`${API_BASE}${other}?page=1&page_size=1`)
    const j = await r.json()
    if (j.success) {
      if (other === '/memorandum/registered') registeredTotal.value = j.data?.total || 0
      else pendingTotal.value = j.data?.total || 0
    }
  } catch {}
}

watch(currentTab, load)

function openRegister(item: MemorandumItem) {
  registerDialog.value = {
    open: true, busy: false, ledgerId: item.ledger_id, assetName: item.asset_name,
    topic: '', classification: '', note: '', password: '',
  }
}

async function doRegister() {
  registerDialog.value.busy = true
  try {
    const r = await fetch(`${API_BASE}/memorandum/register`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ledger_id: registerDialog.value.ledgerId,
        topic: registerDialog.value.topic,
        classification: registerDialog.value.classification,
        note: registerDialog.value.note,
        password: registerDialog.value.password,
      }),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: '登记成功', color: 'success' }
      registerDialog.value.open = false
      await load()
      await warmInactive()
    } else {
      snackbar.value = { show: true, text: '登记失败：' + j.error, color: 'error' }
    }
  } finally {
    registerDialog.value.busy = false
  }
}

onMounted(async () => {
  await load()
  await warmInactive()
})
</script>
