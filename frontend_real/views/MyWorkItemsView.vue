<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { API_BASE } from '@/services/api'
import { workItemsApi, type OutputRule, type StageDoc } from '@/services/templateAuthoringApi'

// 2026-06-02 统一收件箱：集中立项承接后下发给我的环节任务，三列看板。
// 开始工作 → 后端建 CPA-{应用id} 目录树 + 按模版过程标识预建占位；
// 在线编辑只展示「工作依据(input)」+「过程(process)」，不展示定稿(output)；
// 干完点「交付」→ 按 output 标识挑定稿（和改动前一致）→ 自动拷到 output → 下游就绪。

interface StageTask {
  id: number
  application_id: number
  stage_code: string
  stage_name: string
  status: string // pending / in_progress / completed
  project_name: string
  template_code: string | null
  template_version: string | null
  owner_name: string
  assignee_username: string
}

const loading = ref(false)
const busy = ref<number>(0)
const items = ref<StageTask[]>([])
const snackbar = ref({ show: false, text: '', color: 'success' })

function notify(text: string, color = 'success') {
  snackbar.value = { show: true, text, color }
}
// 集中立项虚拟项目码：在线编辑/过程文件都按它定位目录
function cpaCode(item: StageTask) {
  return `CPA-${item.application_id}`
}

const allStagesCache = ref<Record<number, Array<{ stage_code: string; stage_name: string; status: string; sort_order: number }>>>({})
async function ensureAllStages(appId: number) {
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/${appId}/stages`)
    const j = await r.json()
    if (j.success) allStagesCache.value = { ...allStagesCache.value, [appId]: j.data || [] }
  } catch { /* ignore */ }
}

async function load() {
  loading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/my-stages`)
    const j = await r.json()
    if (j.success) {
      items.value = (j.data || []) as StageTask[]
      const appIds = Array.from(new Set(items.value.map(s => s.application_id)))
      allStagesCache.value = {}
      await Promise.all(appIds.map(id => ensureAllStages(id)))
    } else {
      notify('加载失败：' + (j.error || ''), 'error')
    }
  } catch (e: any) {
    notify('加载失败：' + (e?.message || String(e)), 'error')
  } finally {
    loading.value = false
  }
}

const todoItems = computed(() => items.value.filter(s => s.status === 'pending'))
const doingItems = computed(() => items.value.filter(s => s.status === 'in_progress'))
const doneItems = computed(() => items.value.filter(s => s.status === 'completed'))

function siblingStageCodes(item: StageTask): string[] {
  const all = allStagesCache.value[item.application_id]
  if (all && all.length > 0) return all.map(s => s.stage_code)
  return items.value.filter(s => s.application_id === item.application_id).map(s => s.stage_code)
}
function isBlocked(item: StageTask): boolean {
  if (item.status !== 'pending') return false
  const all = allStagesCache.value[item.application_id]
  if (!all) return false
  const cur = all.find(s => s.stage_code === item.stage_code)
  if (!cur) return false
  return all.some(s => s.sort_order < cur.sort_order && s.status !== 'completed')
}
function blockHint(item: StageTask): string {
  const all = allStagesCache.value[item.application_id]
  if (!all) return ''
  const cur = all.find(s => s.stage_code === item.stage_code)
  if (!cur) return ''
  const blockers = all.filter(s => s.sort_order < cur.sort_order && s.status !== 'completed').map(s => `${s.stage_code} ${s.stage_name}`)
  return blockers.length > 0 ? `待上游：${blockers.join(' / ')}` : ''
}

async function startWork(item: StageTask) {
  busy.value = item.id
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/stages/${item.id}/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        application_id: item.application_id,
        stage_code: item.stage_code,
        template_code: item.template_code || '',
        all_stage_codes: siblingStageCodes(item),
      }),
    })
    const j = await r.json()
    if (j.success) {
      const n = j.data?.scaffolded || 0
      notify(n > 0 ? `已开始，按模版预建 ${n} 个过程文档，点「在线编辑」填写` : '已开始，点「在线编辑」开始工作')
      await load()
      // 开始后直接打开在线编辑
      const fresh = items.value.find(s => s.id === item.id)
      if (fresh) openEditor(fresh)
    } else {
      notify('开始工作失败：' + (j.error || ''), 'error')
    }
  } catch (e: any) {
    notify('开始工作失败：' + (e?.message || String(e)), 'error')
  } finally {
    busy.value = 0
  }
}

// ── 在线编辑：工作依据(只读) + 过程(可编辑)；不展示定稿 ──
const editorDialog = ref(false)
const editorTarget = ref<StageTask | null>(null)
const inputDocs = ref<StageDoc[]>([])
const docs = ref<StageDoc[]>([])
const currentDoc = ref('')
const viewingInput = ref('') // 正在只读查看的工作依据文件名（非空时编辑器只读）
const content = ref('')
const editorLoading = ref(false)
const savingDoc = ref(false)
const savedPath = ref('')

async function openEditor(item: StageTask) {
  editorTarget.value = item
  editorDialog.value = true
  editorLoading.value = true
  currentDoc.value = ''
  viewingInput.value = ''
  content.value = ''
  savedPath.value = ''
  inputDocs.value = []
  docs.value = []
  const cpa = cpaCode(item)
  try {
    ;[inputDocs.value, docs.value] = await Promise.all([
      workItemsApi.inputDocs(cpa, item.stage_code),
      workItemsApi.processDocs(cpa, item.stage_code),
    ])
    if (docs.value.length > 0) await selectDoc(docs.value[0].name)
  } catch (e: any) {
    notify('加载文档失败：' + (e?.message || String(e)), 'error')
  } finally {
    editorLoading.value = false
  }
}
async function selectDoc(name: string) {
  if (!editorTarget.value) return
  viewingInput.value = ''
  currentDoc.value = name
  editorLoading.value = true
  try {
    const res = await workItemsApi.readDoc(cpaCode(editorTarget.value), editorTarget.value.stage_code, name)
    content.value = res?.content ?? ''
  } catch (e: any) {
    notify('读取文档失败：' + (e?.message || String(e)), 'error')
  } finally {
    editorLoading.value = false
  }
}

// 只读查看「工作依据」(input) 文件内容
async function viewInputDoc(name: string) {
  if (!editorTarget.value) return
  currentDoc.value = ''
  viewingInput.value = name
  editorLoading.value = true
  try {
    const res = await workItemsApi.readInputDoc(cpaCode(editorTarget.value), editorTarget.value.stage_code, name)
    content.value = res?.content ?? ''
  } catch (e: any) {
    notify('读取工作依据失败：' + (e?.message || String(e)), 'error')
  } finally {
    editorLoading.value = false
  }
}
async function saveDoc() {
  const w = editorTarget.value
  if (!w || !currentDoc.value) return
  savingDoc.value = true
  try {
    const res = await workItemsApi.saveDoc(cpaCode(w), w.stage_code, currentDoc.value, content.value)
    savedPath.value = res?.path || ''
    notify('已保存到本环节过程目录')
    docs.value = await workItemsApi.processDocs(cpaCode(w), w.stage_code)
  } catch (e: any) {
    notify('保存失败：' + (e?.message || String(e)), 'error')
  } finally {
    savingDoc.value = false
  }
}
const SAVE_AS_FORMATS = ['txt', 'md', 'html', 'csv', 'json']
const saveAsDialog = ref(false)
const saveAsFormat = ref('txt')
const saveAsName = computed(() => `${currentDoc.value.replace(/\.[^.]+$/, '') || '未命名'}.${saveAsFormat.value}`)
function openSaveAs() {
  if (!currentDoc.value) return
  saveAsFormat.value = 'txt'
  saveAsDialog.value = true
}
async function doSaveAs() {
  const w = editorTarget.value
  if (!w) return
  savingDoc.value = true
  try {
    const name = saveAsName.value
    const res = await workItemsApi.saveDoc(cpaCode(w), w.stage_code, name, content.value)
    savedPath.value = res?.path || ''
    docs.value = await workItemsApi.processDocs(cpaCode(w), w.stage_code)
    currentDoc.value = name
    saveAsDialog.value = false
    notify(`已另存为 ${name}`)
  } catch (e: any) {
    notify('另存为失败：' + (e?.message || String(e)), 'error')
  } finally {
    savingDoc.value = false
  }
}
async function copyContent() {
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(content.value)
    } else {
      const ta = document.createElement('textarea')
      ta.value = content.value
      document.body.appendChild(ta); ta.select(); document.execCommand('copy'); document.body.removeChild(ta)
    }
    notify('已复制全文到剪贴板')
  } catch (e: any) {
    notify('复制失败：' + (e?.message || String(e)), 'error')
  }
}

// ── 交付：定稿已在各文件任务完成时提交（任务级，谁编辑谁挑）。环节交付只做汇总流转：
// 把本环节各任务定稿汇总下发给下一环节每个任务的工作依据，并标记本环节完成。 ──
const deliverDialog = ref(false)
const deliverTarget = ref<StageTask | null>(null)
const deliverLoading = ref(false)

function openDeliver(item: StageTask) {
  deliverTarget.value = item
  deliverDialog.value = true
}
async function submitDeliver() {
  const w = deliverTarget.value
  if (!w) return
  deliverLoading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/stages/${w.id}/deliver`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        application_id: w.application_id,
        current_stage_code: w.stage_code,
        template_code: w.template_code || '',
      }),
    })
    const j = await r.json()
    if (j.success) {
      deliverDialog.value = false
      notify(j.data?.is_last_stage ? '已交付，本项目最后一个环节已完成' : '已交付，下游环节工作依据已就位')
      await load()
    } else {
      notify('交付失败：' + (j.error || ''), 'error')
    }
  } catch (e: any) {
    notify('交付失败：' + (e?.message || String(e)), 'error')
  } finally {
    deliverLoading.value = false
  }
}

onMounted(load)
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-clipboard-check-outline</v-icon>
      我的工作事项
      <v-spacer />
      <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
    </v-card-title>
    <v-card-subtitle>项目承接后下发给我的工作环节：开始工作后在线编辑过程文档，干完点「交付」挑选定稿，下游随之就绪。</v-card-subtitle>

    <v-card-text>
      <div class="board">
        <!-- 我的待办 -->
        <section class="col">
          <header class="col-head"><span class="dot dot-todo"></span><span class="col-title">我的待办</span><span class="count">{{ todoItems.length }}</span></header>
          <v-card v-for="w in todoItems" :key="w.id" flat class="item-card" :class="isBlocked(w) ? 'bar-wait locked' : 'bar-todo'">
            <div class="proj">{{ w.project_name }}</div>
            <div class="stage">{{ w.stage_name }}</div>
            <div class="meta">
              <span class="person">{{ w.assignee_username || '—' }}</span>
              <v-chip size="x-small" :color="isBlocked(w) ? 'grey' : 'primary'" variant="tonal">{{ isBlocked(w) ? '待就绪' : '可开始' }}</v-chip>
            </div>
            <div class="acts">
              <v-btn v-if="!isBlocked(w)" size="small" color="primary" variant="tonal" :loading="busy === w.id" @click="startWork(w)">开始工作</v-btn>
              <span v-else class="wait-hint">⏳ {{ blockHint(w) }}</span>
            </div>
          </v-card>
          <div v-if="!loading && todoItems.length === 0" class="col-empty">暂无待办</div>
        </section>

        <!-- 进行中 -->
        <section class="col">
          <header class="col-head"><span class="dot dot-doing"></span><span class="col-title">进行中</span><span class="count">{{ doingItems.length }}</span></header>
          <v-card v-for="w in doingItems" :key="w.id" flat class="item-card bar-doing">
            <div class="proj">{{ w.project_name }}</div>
            <div class="stage">{{ w.stage_name }}</div>
            <div class="meta">
              <span class="person">{{ w.assignee_username || '—' }}</span>
              <v-chip size="x-small" color="warning" variant="tonal">进行中</v-chip>
            </div>
            <div class="acts">
              <v-btn size="small" color="primary" variant="text" prepend-icon="mdi-pencil-outline" @click="openEditor(w)">在线编辑</v-btn>
              <v-btn size="small" color="success" variant="tonal" :loading="busy === w.id" @click="openDeliver(w)">交付</v-btn>
            </div>
          </v-card>
          <div v-if="!loading && doingItems.length === 0" class="col-empty">暂无进行中</div>
        </section>

        <!-- 已结束 -->
        <section class="col">
          <header class="col-head"><span class="dot dot-done"></span><span class="col-title">已结束</span><span class="count">{{ doneItems.length }}</span></header>
          <v-card v-for="w in doneItems" :key="w.id" flat class="item-card bar-done muted">
            <div class="proj">{{ w.project_name }}</div>
            <div class="stage">{{ w.stage_name }}</div>
            <div class="meta">
              <span class="person">{{ w.assignee_username || '—' }}</span>
              <v-chip size="x-small" color="success" variant="tonal">已交付</v-chip>
            </div>
          </v-card>
          <div v-if="!loading && doneItems.length === 0" class="col-empty">暂无已结束</div>
        </section>
      </div>

      <div v-if="!loading && items.length === 0" class="empty-all">
        <v-icon size="64" color="grey-lighten-1">mdi-clipboard-text-off-outline</v-icon>
        <div class="mt-4 text-grey">暂无分配给我的工作事项</div>
        <div class="text-caption text-grey-lighten-1 mt-1">需项目负责人在「项目承接」选模版并把环节指派给当前账号</div>
      </div>
    </v-card-text>

    <!-- 在线编辑：工作依据(只读) + 过程(可编辑) -->
    <v-dialog v-model="editorDialog" max-width="980" persistent>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-file-edit-outline</v-icon>
          在线编辑 — {{ editorTarget?.stage_name }}
        </v-card-title>
        <v-card-subtitle>左侧「工作依据」是上游交付来的资料（只读）；「过程」是你的工作文档，编辑后保存到本环节过程目录。</v-card-subtitle>
        <v-card-text>
          <v-row no-gutters>
            <v-col cols="4" class="pr-3">
              <div class="text-caption text-grey mb-1">工作依据（上游交付，点击查看）</div>
              <v-list density="compact" border rounded class="doc-list mb-3">
                <v-list-item v-for="d in inputDocs" :key="d.name" :active="viewingInput === d.name" @click="viewInputDoc(d.name)">
                  <template #prepend><v-icon size="small" color="info">mdi-tray-arrow-down</v-icon></template>
                  <v-list-item-title class="text-body-2">{{ d.name }}</v-list-item-title>
                </v-list-item>
                <div v-if="inputDocs.length === 0" class="pa-3 text-caption text-grey">暂无上游工作依据</div>
              </v-list>
              <div class="text-caption text-grey mb-1">过程文档</div>
              <v-list density="compact" border rounded class="doc-list">
                <v-list-item v-for="d in docs" :key="d.name" :active="currentDoc === d.name" @click="selectDoc(d.name)">
                  <template #prepend>
                    <v-icon size="small" :color="d.empty ? 'grey' : 'success'">{{ d.empty ? 'mdi-file-outline' : 'mdi-file-check-outline' }}</v-icon>
                  </template>
                  <v-list-item-title class="text-body-2">{{ d.name }}</v-list-item-title>
                  <v-list-item-subtitle class="text-caption">{{ d.empty ? '空（待填写）' : '已填写' }}</v-list-item-subtitle>
                </v-list-item>
                <div v-if="docs.length === 0" class="pa-3 text-caption text-grey">暂无过程文档</div>
              </v-list>
            </v-col>
            <v-col cols="8">
              <div class="text-caption text-grey mb-1">
                <template v-if="viewingInput">查看工作依据（只读）：<b>{{ viewingInput }}</b></template>
                <template v-else>正在编辑：<b>{{ currentDoc || '（未选择）' }}</b></template>
              </div>
              <v-textarea
                v-model="content"
                :loading="editorLoading"
                :readonly="!!viewingInput"
                :disabled="(!currentDoc && !viewingInput) || editorLoading"
                variant="outlined" rows="16" hide-details auto-grow
                :placeholder="viewingInput ? '' : '在此输入文档内容，保存后存入本环节过程目录'"
              />
              <div v-if="savedPath" class="text-caption text-success mt-1"><v-icon size="x-small">mdi-check</v-icon> 已保存到：{{ savedPath }}</div>
            </v-col>
          </v-row>
        </v-card-text>
        <v-card-actions>
          <v-btn variant="text" prepend-icon="mdi-content-copy" :disabled="!currentDoc" @click="copyContent">复制</v-btn>
          <v-btn variant="text" prepend-icon="mdi-content-save-edit-outline" :disabled="!currentDoc" @click="openSaveAs">另存为其他格式</v-btn>
          <v-spacer />
          <v-btn variant="text" @click="editorDialog = false">关闭</v-btn>
          <v-btn color="primary" variant="flat" :loading="savingDoc" :disabled="!currentDoc" @click="saveDoc">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 另存为其他格式 -->
    <v-dialog v-model="saveAsDialog" max-width="420">
      <v-card>
        <v-card-title class="d-flex align-center"><v-icon class="mr-2">mdi-content-save-edit-outline</v-icon>另存为其他格式</v-card-title>
        <v-card-text>
          <v-select v-model="saveAsFormat" :items="SAVE_AS_FORMATS" label="目标格式" variant="outlined" density="comfortable" hide-details />
          <div class="text-caption text-grey mt-3">将按当前内容另存为：<b>{{ saveAsName }}</b></div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="saveAsDialog = false">取消</v-btn>
          <v-btn color="primary" variant="flat" :loading="savingDoc" @click="doSaveAs">另存为</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 交付：定稿已由各文件任务在完成时提交；环节交付只做汇总流转 -->
    <v-dialog v-model="deliverDialog" max-width="560" persistent>
      <v-card>
        <v-card-title class="d-flex align-center"><v-icon class="mr-2">mdi-file-send-outline</v-icon>交付环节 — {{ deliverTarget?.stage_name }}</v-card-title>
        <v-card-subtitle class="text-wrap">本环节各文件任务的定稿已由对应参与人在「完成」时提交。确认交付后，系统会把这些定稿汇总下发为下一环节每个任务的工作依据，并标记本环节完成。</v-card-subtitle>
        <v-card-text>
          <v-alert type="info" variant="tonal" density="compact">请确认本环节所有文件任务均已完成并提交定稿，再交付下游。</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="deliverDialog = false">取消</v-btn>
          <v-btn color="success" variant="flat" :loading="deliverLoading" @click="submitDeliver">确认交付</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3500">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>

<style scoped>
.board { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; }
@media (max-width: 960px) { .board { grid-template-columns: 1fr; } }
.col { background: #eef0f2; border-radius: 14px; padding: 12px; min-height: 360px; }
.col-head { display: flex; align-items: center; gap: 8px; padding: 4px 6px 12px; }
.dot { width: 9px; height: 9px; border-radius: 50%; }
.dot-todo { background: #2f6feb; } .dot-doing { background: #e8910c; } .dot-done { background: #15a05a; }
.col-title { font-weight: 700; font-size: 14.5px; }
.count { margin-left: auto; background: #fff; border: 1px solid #e6e8eb; border-radius: 999px; font-size: 12px; color: #646a73; padding: 1px 9px; }
.item-card { border: 1px solid #e6e8eb !important; border-radius: 12px; padding: 12px 13px; margin-bottom: 11px; border-left-width: 3px !important; border-left-style: solid !important; }
.bar-todo { border-left-color: #2f6feb !important; } .bar-doing { border-left-color: #e8910c !important; }
.bar-done { border-left-color: #15a05a !important; } .bar-wait { border-left-color: #9aa0a6 !important; }
.item-card.locked { background: #fafbfc; border-style: dashed !important; }
.item-card.muted { opacity: 0.78; }
.proj { font-size: 12px; color: #646a73; margin-bottom: 3px; }
.stage { font-size: 14.5px; font-weight: 600; margin-bottom: 8px; line-height: 1.35; }
.locked .stage { color: #8a9099; }
.meta { display: flex; align-items: center; gap: 8px; font-size: 12px; color: #646a73; margin-bottom: 10px; }
.acts { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.wait-hint { font-size: 11.5px; color: #9aa0a6; }
.col-empty { text-align: center; color: #9aa0a6; font-size: 12.5px; padding: 24px 0; }
.empty-all { text-align: center; padding: 32px 0; }
.doc-list { max-height: 200px; overflow-y: auto; }
</style>
