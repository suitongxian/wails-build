<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { businessClassApi, type BusinessClass } from '@/services/templateAuthoringApi'

const loading = ref(false)
const error = ref('')
const items = ref<BusinessClass[]>([])
const snackbar = ref({ show: false, text: '', color: 'success' })

const headers = [
  { title: '编码', key: 'code', width: '140px' },
  { title: '行业分类名称', key: 'name' },
  { title: '数据业务描述', key: 'description' },
  { title: '操作', key: 'actions', width: '140px', sortable: false },
]

function notify(text: string, color = 'success') {
  snackbar.value = { show: true, text, color }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    items.value = await businessClassApi.list()
  } catch (e: any) {
    error.value = e?.message || String(e)
  } finally {
    loading.value = false
  }
}

const dialog = ref({ show: false, editingId: null as number | null, name: '', description: '' })
function openCreate() {
  dialog.value = { show: true, editingId: null, name: '', description: '' }
}
function openEdit(item: BusinessClass) {
  dialog.value = { show: true, editingId: item.id, name: item.name, description: item.description || '' }
}
const saving = ref(false)
async function save() {
  const d = dialog.value
  if (!d.name.trim()) return notify('请填写行业分类名称', 'error')
  saving.value = true
  try {
    if (d.editingId == null) {
      await businessClassApi.create({ name: d.name, description: d.description })
      notify('已创建行业分类')
    } else {
      await businessClassApi.update(d.editingId, { name: d.name, description: d.description })
      notify('已保存')
    }
    dialog.value.show = false
    await load()
  } catch (e: any) {
    notify('保存失败：' + (e?.message || String(e)), 'error')
  } finally {
    saving.value = false
  }
}
async function remove(item: BusinessClass) {
  if (!confirm(`确认删除行业分类「${item.name}」？`)) return
  try {
    await businessClassApi.remove(item.id)
    notify('已删除')
    await load()
  } catch (e: any) {
    notify('删除失败：' + (e?.message || String(e)), 'error')
  }
}

onMounted(load)
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-tag-multiple-outline</v-icon>
      数据业务分类
      <v-spacer />
      <v-btn color="primary" prepend-icon="mdi-plus" @click="openCreate">新增分类</v-btn>
    </v-card-title>
    <v-card-subtitle>行业分类是模版的上层归类（彼此平级）。编码自动生成 IND-NNN。</v-card-subtitle>

    <v-alert v-if="error" type="error" variant="tonal" density="compact" class="ma-3">{{ error }}</v-alert>

    <v-data-table
      :headers="headers"
      :items="items"
      :loading="loading"
      item-value="id"
      :items-per-page="100"
      hide-default-footer
    >
      <template #item.code="{ item }"><code class="text-body-2">{{ item.code }}</code></template>
      <template #item.description="{ item }">{{ item.description || '-' }}</template>
      <template #item.actions="{ item }">
        <v-btn size="x-small" variant="text" @click="openEdit(item)">编辑</v-btn>
        <v-btn size="x-small" variant="text" color="error" @click="remove(item)">删除</v-btn>
      </template>
      <template v-slot:no-data>
        <div class="text-center py-8">
          <v-icon size="64" color="grey-lighten-1">mdi-tag-off-outline</v-icon>
          <div class="mt-4 text-grey">暂无行业分类，点右上角「新增行业」</div>
        </div>
      </template>
    </v-data-table>

    <v-dialog v-model="dialog.show" max-width="480" persistent>
      <v-card>
        <v-card-title>{{ dialog.editingId == null ? '新增行业分类' : '编辑行业分类' }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="dialog.name" label="行业分类名称 *" density="compact" variant="outlined" />
          <v-textarea v-model="dialog.description" label="数据业务描述" rows="3" density="compact" variant="outlined" />
        </v-card-text>
        <v-card-actions>
          <v-spacer /><v-btn variant="text" :disabled="saving" @click="dialog.show = false">取消</v-btn>
          <v-btn color="primary" :loading="saving" @click="save">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>
