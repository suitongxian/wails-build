<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import {
  manageBusinessClassApi,
  localTemplateApi,
  manageUsersApi,
  SCOPE_LABELS,
  LOCAL_SCOPE_LABELS,
  SENSITIVITY_LABELS,
  type BusinessClass,
  type LocalTemplate,
  type ManageUser,
  type RemoteTemplate,
  type TemplateInput,
} from '@/services/templateAuthoringApi'

// 2026-06-02 合并：「数据项目模版」并入「模板库」。本页 = 本地模版完整管理(创作/编辑/发布/同步/删除)
// + 在线通用模版（中心下发，可另存为本地裁剪）。立项请走「立项」页。

const router = useRouter()

const loading = ref(false)
const error = ref('')
const items = ref<LocalTemplate[]>([])
const classes = ref<BusinessClass[]>([])
const managers = ref<ManageUser[]>([])
const managersError = ref('')
const filterClass = ref<string | null>(null)
const filterScope = ref<string | null>(null)
const snackbar = ref({ show: false, text: '', color: 'success' })

// 在线通用模版（原「manage 通用模版」）
const remote = ref<RemoteTemplate[]>([])
const busy = ref<string | null>(null)

const scopeOptions = Object.entries(LOCAL_SCOPE_LABELS).map(([value, title]) => ({ value, title }))
const filterScopeOptions = Object.entries(SCOPE_LABELS).map(([value, title]) => ({ value, title }))
const sensitivityOptions = Object.entries(SENSITIVITY_LABELS).map(([value, title]) => ({ value, title }))

const headers = [
  { title: '项目名称', key: 'template_name', minWidth: '300px' },
  { title: '代号', key: 'short_code', width: '110px' },
  { title: '行业', key: 'class_code', width: '120px' },
  { title: '归类', key: 'scope', width: '100px' },
  { title: '负责人', key: 'manager', width: '100px' },
  { title: '敏感级别', key: 'project_sensitivity_level', width: '100px' },
  { title: '发布', key: 'is_published', width: '90px' },
  { title: '同步', key: 'sync_status', width: '80px' },
  { title: '操作', key: 'actions', width: '160px', sortable: false, align: 'center' },
]

function classNameOf(code: string | null): string {
  if (!code) return '-'
  return classes.value.find((c) => c.code === code)?.name || code
}
function notify(text: string, color = 'success') {
  snackbar.value = { show: true, text, color }
}

async function loadClasses() {
  try { classes.value = await manageBusinessClassApi.list() } catch { classes.value = [] }
}
async function loadManagers() {
  managersError.value = ''
  try { managers.value = await manageUsersApi.list() } catch (e: any) {
    managersError.value = e?.message || String(e); managers.value = []
  }
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    items.value = await localTemplateApi.list({
      class_code: filterClass.value || undefined,
      scope: filterScope.value || undefined,
    })
  } catch (e: any) {
    error.value = e?.message || String(e)
  } finally {
    loading.value = false
  }
}
async function loadRemote() {
  try { remote.value = await localTemplateApi.remoteList() } catch (e: any) {
    error.value = '在线通用模版拉取失败：' + (e?.message || String(e))
    remote.value = []
  }
}

// ---------- 新建/编辑弹窗 ----------
const dialog = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const form = ref<TemplateInput>(emptyForm())
function emptyForm(): TemplateInput {
  return { class_code: undefined, scope: 'unit', template_name: '', short_code: '', manager: '', description: '', approval_basis: '', sensitivity_level: 'general', owner: '' }
}
function openCreate() {
  editingId.value = null
  form.value = emptyForm()
  if (filterClass.value) form.value.class_code = filterClass.value
  dialog.value = true
}
function openEdit(item: LocalTemplate) {
  editingId.value = item.id
  form.value = {
    class_code: item.class_code || undefined,
    scope: (item.scope as TemplateInput['scope']) === 'industry' ? 'unit' : ((item.scope as TemplateInput['scope']) || 'unit'),
    template_name: item.template_name,
    short_code: item.short_code || '',
    manager: item.manager || '',
    description: item.description || '',
    approval_basis: item.approval_basis || '',
    sensitivity_level: (item.project_sensitivity_level as TemplateInput['sensitivity_level']) || 'general',
    owner: item.owner || '',
  }
  dialog.value = true
}
async function save() {
  if (!form.value.template_name.trim()) { notify('请填写项目名称', 'error'); return }
  saving.value = true
  try {
    if (editingId.value == null) { await localTemplateApi.create(form.value); notify('已创建项目模版') }
    else { await localTemplateApi.update(editingId.value, form.value); notify('已保存') }
    dialog.value = false
    await load()
  } catch (e: any) {
    notify('保存失败：' + (e?.message || String(e)), 'error')
  } finally {
    saving.value = false
  }
}

const pushingId = ref<number | null>(null)
async function pushToManage(item: LocalTemplate) {
  pushingId.value = item.id
  try {
    const res = await localTemplateApi.push(item.id)
    notify(`已推送到 manage（remote_id=${res.remote_id}）`)
    await load()
  } catch (e: any) {
    notify('推送失败：' + (e?.message || String(e)), 'error'); await load()
  } finally {
    pushingId.value = null
  }
}
const publishingId = ref<number | null>(null)
async function togglePublish(item: LocalTemplate) {
  publishingId.value = item.id
  const next = item.is_published !== 1
  try {
    await localTemplateApi.setPublished(item.id, next)
    notify(next ? '已发布，现在可用于立项' : '已取消发布')
    await load()
  } catch (e: any) {
    notify((next ? '发布' : '取消发布') + '失败：' + (e?.message || String(e)), 'error')
  } finally {
    publishingId.value = null
  }
}
// 删除确认：不用 window.confirm（Wails WebView 偶发不弹窗→点了没反应），改用应用内对话框。
const removeDialog = ref<{ open: boolean; busy: boolean; target: LocalTemplate | null }>({ open: false, busy: false, target: null })
function askRemove(item: LocalTemplate) {
  removeDialog.value = { open: true, busy: false, target: item }
}
async function confirmRemove() {
  const item = removeDialog.value.target
  if (!item) return
  removeDialog.value.busy = true
  try {
    await localTemplateApi.remove(item.id)
    removeDialog.value.open = false
    notify('已删除')
    await load()
  } catch (e: any) {
    notify('删除失败：' + (e?.message || String(e)), 'error')
  } finally {
    removeDialog.value.busy = false
  }
}
function editStructure(item: LocalTemplate) {
  router.push(`/template-authoring/${item.id}`)
}

// ---------- 导入模版（上传 .json / 粘贴 JSON）----------
const importDialog = ref(false)
const importing = ref(false)
const importText = ref('')
const importFileName = ref('')
function openImport() {
  importText.value = ''
  importFileName.value = ''
  importDialog.value = true
}
// 选择 .json 文件 → 读入文本框（跨平台：浏览器 FileReader，无本地路径依赖）
async function onImportFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files && input.files[0]
  if (!file) return
  importFileName.value = file.name
  try {
    importText.value = await file.text()
  } catch (err: any) {
    notify('读取文件失败：' + (err?.message || String(err)), 'error')
  }
  input.value = '' // 允许重复选择同一文件
}
async function doImport() {
  const raw = importText.value.trim()
  if (!raw) { notify('请粘贴或上传模版 JSON', 'error'); return }
  let tree: unknown
  try {
    tree = JSON.parse(raw)
  } catch (err: any) {
    notify('JSON 格式有误：' + (err?.message || String(err)), 'error')
    return
  }
  importing.value = true
  try {
    const res = await localTemplateApi.importTree(tree)
    notify('已导入到本地模板库')
    importDialog.value = false
    await load()
    router.push(`/template-authoring/${res.id}`)
  } catch (e: any) {
    notify('导入失败：' + (e?.message || String(e)), 'error')
  } finally {
    importing.value = false
  }
}

// 在线通用模版：另存为本地副本（归类降为单位），进编辑器裁剪、发布
async function saveAsLocal(t: RemoteTemplate) {
  busy.value = 'm:' + t.template_code
  try {
    const c = await localTemplateApi.cloneFromManage(t.template_code)
    notify('已另存为本地模版，请在编辑器裁剪、发布后供立项选用')
    router.push(`/template-authoring/${c.id}`)
  } catch (e: any) {
    notify('另存为失败：' + (e?.message || String(e)), 'error')
  } finally {
    busy.value = null
  }
}

onMounted(async () => {
  await loadClasses()
  await loadManagers()
  await load()
  await loadRemote()
})
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-file-document-multiple-outline</v-icon>
      模板库
      <v-spacer />
      <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="() => { load(); loadRemote() }">刷新</v-btn>
      <v-btn variant="tonal" color="primary" prepend-icon="mdi-import" class="ml-2" @click="openImport">导入模版</v-btn>
      <v-btn color="primary" prepend-icon="mdi-plus" class="ml-2" @click="openCreate">新建项目模版</v-btn>
    </v-card-title>
    <v-card-subtitle>
      本地创作/裁剪的五层模版（项目 ▸ 事项 ▸ 任务 ▸ 标识）。发布后供「立项」承接时选用；立项不在此处。
    </v-card-subtitle>

    <div class="d-flex ga-3 px-4 pt-2" style="max-width: 640px">
      <v-select v-model="filterClass" :items="classes" item-title="name" item-value="code" label="按行业筛选" density="compact" variant="outlined" clearable hide-details @update:model-value="load" />
      <v-select v-model="filterScope" :items="filterScopeOptions" label="按归类筛选" density="compact" variant="outlined" clearable hide-details @update:model-value="load" />
    </div>

    <v-alert v-if="error" type="warning" variant="tonal" density="compact" class="ma-3">{{ error }}</v-alert>

    <v-data-table :headers="headers" :items="items" :loading="loading" item-value="id" :items-per-page="50" hide-default-footer class="mt-2">
      <template #item.short_code="{ item }">{{ item.short_code || '-' }}</template>
      <template #item.class_code="{ item }">{{ classNameOf(item.class_code) }}</template>
      <template #item.scope="{ item }">
        <v-chip size="x-small" variant="tonal">{{ SCOPE_LABELS[item.scope] || item.scope }}</v-chip>
      </template>
      <template #item.manager="{ item }">{{ item.manager || '-' }}</template>
      <template #item.project_sensitivity_level="{ item }">
        <v-chip size="x-small" :color="item.project_sensitivity_level === 'core' ? 'error' : item.project_sensitivity_level === 'important' ? 'warning' : 'success'" variant="tonal">
          {{ SENSITIVITY_LABELS[item.project_sensitivity_level] || item.project_sensitivity_level }}
        </v-chip>
      </template>
      <template #item.is_published="{ item }">
        <v-chip v-if="item.is_published === 1" size="x-small" color="success" variant="tonal">已发布</v-chip>
        <v-chip v-else size="x-small" color="grey" variant="tonal">未发布</v-chip>
      </template>
      <template #item.sync_status="{ item }">
        <v-chip v-if="item.sync_status === 'synced'" size="x-small" color="success" variant="tonal">已同步</v-chip>
        <v-chip v-else-if="item.sync_status === 'error'" size="x-small" color="error" variant="tonal">失败</v-chip>
        <span v-else class="text-grey text-caption">未同步</span>
      </template>
      <template #item.actions="{ item }">
        <div class="d-flex flex-column ga-1 py-2">
          <v-btn size="small" variant="tonal" color="primary" prepend-icon="mdi-file-tree-outline" block @click="editStructure(item)">编辑结构</v-btn>
          <v-btn size="small" variant="tonal" :color="item.is_published === 1 ? 'warning' : 'success'" :prepend-icon="item.is_published === 1 ? 'mdi-publish-off' : 'mdi-publish'" :loading="publishingId === item.id" block @click="togglePublish(item)">{{ item.is_published === 1 ? '取消发布' : '发布' }}</v-btn>
          <v-btn size="small" variant="tonal" color="info" prepend-icon="mdi-cloud-upload-outline" :loading="pushingId === item.id" block @click="pushToManage(item)">同步管理平台</v-btn>
          <v-btn size="small" variant="tonal" color="grey-darken-1" prepend-icon="mdi-pencil-outline" block @click="openEdit(item)">编辑</v-btn>
          <v-btn size="small" variant="tonal" color="error" prepend-icon="mdi-delete-outline" block @click="askRemove(item)">删除</v-btn>
        </div>
      </template>
      <template v-slot:no-data>
        <div class="text-center py-8">
          <v-icon size="64" color="grey-lighten-1">mdi-file-tree-outline</v-icon>
          <div class="mt-4 text-grey">暂无项目模版，点右上角「新建项目模版」或从下方「在线通用模版」另存为</div>
        </div>
      </template>
    </v-data-table>

    <!-- 在线通用模版（原 manage 通用模版） -->
    <v-card-text>
      <div class="text-subtitle-1 mb-1">在线通用模版</div>
      <div class="text-caption text-grey mb-2">中心下发的通用（行业）模版：点「另存为本地模版」复制一份到本地（归类自动降为「单位专属」）→ 在编辑器裁剪 → 发布后供「立项」选用（不改动在线原模版）。</div>
      <v-table v-if="remote.length > 0" density="comfortable">
        <thead>
          <tr><th>模版名称</th><th style="width:160px">编码</th><th style="width:90px">版本</th><th style="width:240px" class="text-right">操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="t in remote" :key="t.template_code">
            <td>{{ t.template_name }}</td>
            <td><v-chip size="small" variant="outlined">{{ t.template_code }}</v-chip></td>
            <td>{{ t.template_version }}</td>
            <td class="text-right">
              <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-content-save-edit-outline" :loading="busy === 'm:' + t.template_code" @click="saveAsLocal(t)">另存为本地模版</v-btn>
            </td>
          </tr>
        </tbody>
      </v-table>
      <v-sheet v-else-if="!loading" border rounded class="pa-4 text-center text-grey">在线暂无可用通用模版</v-sheet>
    </v-card-text>

    <!-- 新建/编辑弹窗 -->
    <v-dialog v-model="dialog" max-width="640" persistent>
      <v-card>
        <v-card-title>{{ editingId == null ? '新建项目模版' : '编辑项目模版' }}</v-card-title>
        <v-card-text>
          <div class="d-flex ga-3">
            <v-select v-model="form.class_code" :items="classes" item-title="name" item-value="code" label="数据业务分类" density="compact" variant="outlined" />
            <v-select v-model="form.scope" :items="scopeOptions" label="模版归类" density="compact" variant="outlined" />
          </div>
          <v-text-field v-model="form.template_name" label="项目名称 *" density="compact" variant="outlined" />
          <div class="d-flex ga-3">
            <v-text-field v-model="form.short_code" label="代号/简称" density="compact" variant="outlined" />
            <v-combobox v-model="form.manager" :items="managers.map((m) => m.display_name)" label="项目负责人" density="compact" variant="outlined" :hint="managersError ? 'manage 用户拉取失败，可手动填写' : '从 manage 已注册用户中选择，也可手填'" persistent-hint />
          </div>
          <div class="d-flex ga-3">
            <v-select v-model="form.sensitivity_level" :items="sensitivityOptions" label="敏感级别" density="compact" variant="outlined" />
            <v-text-field v-model="form.owner" label="数据所有权归属" density="compact" variant="outlined" />
          </div>
          <v-textarea v-model="form.description" label="项目简介" rows="2" density="compact" variant="outlined" />
          <v-textarea v-model="form.approval_basis" label="立项依据" rows="2" density="compact" variant="outlined" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="saving" @click="dialog = false">取消</v-btn>
          <v-btn color="primary" :loading="saving" @click="save">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 导入模版弹窗：上传 .json 或粘贴 JSON -->
    <v-dialog v-model="importDialog" max-width="680" persistent scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-import</v-icon>导入模版
        </v-card-title>
        <v-card-subtitle class="text-wrap">
          上传或粘贴一棵五层模版树 JSON（项目 ▸ 事项 ▸ 任务 ▸ 标识），导入后会在本地模板库新建一份可编辑模版。可用「编辑结构」处的导出 JSON 互相搬运。
        </v-card-subtitle>
        <v-card-text>
          <v-file-input
            label="上传 .json 文件"
            accept="application/json,.json"
            density="compact" variant="outlined"
            prepend-icon="mdi-file-upload-outline"
            hide-details class="mb-3"
            @change="onImportFile"
          />
          <div class="text-caption text-grey mb-1">或直接粘贴 JSON：</div>
          <v-textarea
            v-model="importText"
            placeholder='{ "template": { "template_name": "...", "project_sensitivity_level": "core" }, "stages": [ { "stage_name": "...", "tasks": [ { "task_name": "...", "file_rules": [ { "file_name": "...", "data_state": "input", "allowed_file_types": "PDF,DOCX" } ] } ] } ] }'
            rows="12" variant="outlined" density="compact" hide-details
            class="lxs-mono"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="importing" @click="importDialog = false">取消</v-btn>
          <v-btn color="primary" variant="elevated" :loading="importing" :disabled="!importText.trim()" @click="doImport">导入</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 删除确认（应用内对话框，避免 Wails WebView 下 window.confirm 不弹） -->
    <v-dialog v-model="removeDialog.open" max-width="460">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="error">mdi-delete-alert-outline</v-icon>删除模版
        </v-card-title>
        <v-card-text>
          确认删除项目模版「{{ removeDialog.target?.template_name }}」？其下事项 / 任务 / 标识将一并删除，且不可恢复。
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="removeDialog.busy" @click="removeDialog.open = false">取消</v-btn>
          <v-btn color="error" variant="elevated" :loading="removeDialog.busy" @click="confirmRemove">确认删除</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>

<style scoped>
.lxs-mono :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12.5px; line-height: 1.5; }
</style>
