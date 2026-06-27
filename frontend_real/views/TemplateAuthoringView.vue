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
  type TemplateInput,
} from '@/services/templateAuthoringApi'

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

// 表单里只能选 单位/部门/个人（不含通用行业）；筛选下拉仍可用全集
const scopeOptions = Object.entries(LOCAL_SCOPE_LABELS).map(([value, title]) => ({ value, title }))
const filterScopeOptions = Object.entries(SCOPE_LABELS).map(([value, title]) => ({ value, title }))
const sensitivityOptions = Object.entries(SENSITIVITY_LABELS).map(([value, title]) => ({ value, title }))

const headers = [
  { title: '项目名称', key: 'template_name', minWidth: '320px' },
  { title: '代号', key: 'short_code', width: '120px' },
  { title: '行业', key: 'class_code', width: '130px' },
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

// 数据业务分类已归口 manage：从 manage 拉取供筛选/表单下拉选择
async function loadClasses() {
  try {
    classes.value = await manageBusinessClassApi.list()
  } catch {
    classes.value = []
  }
}

// 项目负责人下拉数据：从 manage 拉取已注册用户（manage 不可达时降级为可手填）
async function loadManagers() {
  managersError.value = ''
  try {
    managers.value = await manageUsersApi.list()
  } catch (e: any) {
    managersError.value = e?.message || String(e)
    managers.value = []
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

// ---------- 新建/编辑弹窗 ----------
const dialog = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const form = ref<TemplateInput>(emptyForm())

function emptyForm(): TemplateInput {
  return {
    class_code: undefined,
    scope: 'unit',
    template_name: '',
    short_code: '',
    manager: '',
    description: '',
    approval_basis: '',
    sensitivity_level: 'general',
    owner: '',
  }
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
  if (!form.value.template_name.trim()) {
    notify('请填写项目名称', 'error')
    return
  }
  saving.value = true
  try {
    if (editingId.value == null) {
      await localTemplateApi.create(form.value)
      notify('已创建项目模版')
    } else {
      await localTemplateApi.update(editingId.value, form.value)
      notify('已保存')
    }
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
    notify('推送失败：' + (e?.message || String(e)), 'error')
    await load()
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

async function remove(item: LocalTemplate) {
  if (!confirm(`确认删除项目模版「${item.template_name}」？其下事项/任务/标识将一并删除。`)) return
  try {
    await localTemplateApi.remove(item.id)
    notify('已删除')
    await load()
  } catch (e: any) {
    notify('删除失败：' + (e?.message || String(e)), 'error')
  }
}

function editStructure(item: LocalTemplate) {
  router.push(`/template-authoring/${item.id}`)
}

onMounted(async () => {
  await loadClasses()
  await loadManagers()
  await load()
})
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-file-tree</v-icon>
      数据项目模版
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">新建项目模版</v-btn>
    </v-card-title>
    <v-card-subtitle>本地创作的五层模版（项目 ▸ 事项 ▸ 任务 ▸ 标识）。编码全自动生成。</v-card-subtitle>

    <div class="d-flex ga-3 px-4 pt-2" style="max-width: 640px">
      <v-select
        v-model="filterClass"
        :items="classes"
        item-title="name"
        item-value="code"
        label="按行业筛选"
        density="compact"
        variant="outlined"
        clearable
        hide-details
        @update:model-value="load"
      />
      <v-select
        v-model="filterScope"
        :items="filterScopeOptions"
        label="按归类筛选"
        density="compact"
        variant="outlined"
        clearable
        hide-details
        @update:model-value="load"
      />
    </div>

    <v-alert v-if="error" type="error" variant="tonal" density="compact" class="ma-3">{{ error }}</v-alert>

    <v-data-table
      :headers="headers"
      :items="items"
      :loading="loading"
      item-value="id"
      :items-per-page="50"
      hide-default-footer
      class="mt-2"
    >
      <template #item.short_code="{ item }">{{ item.short_code || '-' }}</template>
      <template #item.class_code="{ item }">{{ classNameOf(item.class_code) }}</template>
      <template #item.scope="{ item }">
        <v-chip v-if="item.certified === 1" size="x-small" color="amber-darken-2" variant="flat" prepend-icon="mdi-star">项目认定</v-chip>
        <v-chip v-else size="x-small" variant="tonal">{{ SCOPE_LABELS[item.scope] || item.scope }}</v-chip>
      </template>
      <template #item.manager="{ item }">{{ item.manager || '-' }}</template>
      <template #item.project_sensitivity_level="{ item }">
        <v-chip
          size="x-small"
          :color="item.project_sensitivity_level === 'core' ? 'error' : item.project_sensitivity_level === 'important' ? 'warning' : 'success'"
          variant="tonal"
        >
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
          <v-btn
            size="small"
            variant="tonal"
            :color="item.is_published === 1 ? 'warning' : 'success'"
            :prepend-icon="item.is_published === 1 ? 'mdi-publish-off' : 'mdi-publish'"
            :loading="publishingId === item.id"
            block
            @click="togglePublish(item)"
          >{{ item.is_published === 1 ? '取消发布' : '发布' }}</v-btn>
          <v-btn size="small" variant="tonal" color="info" prepend-icon="mdi-cloud-upload-outline" :loading="pushingId === item.id" block @click="pushToManage(item)">同步管理平台</v-btn>
          <v-btn size="small" variant="tonal" color="grey-darken-1" prepend-icon="mdi-pencil-outline" block @click="openEdit(item)">编辑</v-btn>
          <v-btn size="small" variant="tonal" color="error" prepend-icon="mdi-delete-outline" block @click="remove(item)">删除</v-btn>
        </div>
      </template>
      <template v-slot:no-data>
        <div class="text-center py-8">
          <v-icon size="64" color="grey-lighten-1">mdi-file-tree-outline</v-icon>
          <div class="mt-4 text-grey">暂无项目模版，点右上角「新建项目模版」开始</div>
        </div>
      </template>
    </v-data-table>

    <!-- 新建/编辑弹窗 -->
    <v-dialog v-model="dialog" max-width="640" persistent>
      <v-card>
        <v-card-title>{{ editingId == null ? '新建项目模版' : '编辑项目模版' }}</v-card-title>
        <v-card-text>
          <div class="d-flex ga-3">
            <v-select
              v-model="form.class_code"
              :items="classes"
              item-title="name"
              item-value="code"
              label="数据业务分类"
              density="compact"
              variant="outlined"
            />
            <v-select v-model="form.scope" :items="scopeOptions" label="模版归类" density="compact" variant="outlined" />
          </div>
          <v-text-field v-model="form.template_name" label="项目名称 *" density="compact" variant="outlined" />
          <div class="d-flex ga-3">
            <v-text-field v-model="form.short_code" label="代号/简称" density="compact" variant="outlined" />
            <v-combobox
              v-model="form.manager"
              :items="managers.map((m) => m.display_name)"
              label="项目负责人"
              density="compact"
              variant="outlined"
              :hint="managersError ? 'manage 用户拉取失败，可手动填写' : '从 manage 已注册用户中选择，也可手填'"
              persistent-hint
            />
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

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>
