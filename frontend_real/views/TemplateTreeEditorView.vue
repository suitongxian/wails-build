<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  localTemplateApi,
  stageApi,
  taskApi,
  fileRuleApi,
  manageUsersApi,
  SENSITIVITY_LABELS,
  SCOPE_LABELS,
  type LocalTemplateTree,
  type StageNode,
  type TaskNode,
  type TemplateFileRule,
  type DataState,
  type Sensitivity,
  type ManageUser,
} from '@/services/templateAuthoringApi'

const route = useRoute()
const router = useRouter()
const templateId = computed(() => Number(route.params.id))

const loading = ref(false)
const error = ref('')
// 责任人/参与人/承办人 下拉数据：manage 已注册用户
const managers = ref<ManageUser[]>([])
const managerNames = computed(() => managers.value.map((m) => m.display_name))
async function loadManagers() {
  try {
    managers.value = await manageUsersApi.list()
  } catch {
    managers.value = []
  }
}
// username → display_name 解析（用已加载的 manage 用户）
function displayOf(username: string): string {
  return managers.value.find((m) => m.username === username)?.display_name || username
}
const tree = ref<LocalTemplateTree | null>(null)
const snackbar = ref({ show: false, text: '', color: 'success' })
const expanded = ref<Record<string, boolean>>({}) // 'stage-<id>' / 'task-<id>'

const DATA_STATE_LABELS: Record<DataState, string> = { input: '工作依据', process: '过程文件', output: '定稿' }
const STATE_COLOR: Record<DataState, string> = { input: 'blue', process: 'purple', output: 'green' }
const sensitivityOptions = Object.entries(SENSITIVITY_LABELS).map(([value, title]) => ({ value, title }))
const stateOptions = (Object.keys(DATA_STATE_LABELS) as DataState[]).map((v) => ({ value: v, title: DATA_STATE_LABELS[v] }))

function notify(text: string, color = 'success') {
  snackbar.value = { show: true, text, color }
}

// 应用内确认（替代原生 confirm —— Wails WebView 不支持 window.confirm，会"点了没反应"）。
const confirmBox = ref<{ show: boolean; text: string; resolve: null | ((v: boolean) => void) }>({ show: false, text: '', resolve: null })
function askConfirm(text: string): Promise<boolean> {
  return new Promise((resolve) => {
    confirmBox.value = { show: true, text, resolve }
  })
}
function confirmRespond(ok: boolean) {
  const r = confirmBox.value.resolve
  confirmBox.value.show = false
  confirmBox.value.resolve = null
  if (r) r(ok)
}

// ── 改项目信息（名称/负责人/归属/敏感级/立项依据）──
const projectDialog = ref({ show: false, name: '', manager: '', owner: '', approvalBasis: '', sensitivity: 'general' as Sensitivity, saving: false })
function openEditProject() {
  if (!tree.value) return
  const t = tree.value.template
  projectDialog.value = {
    show: true,
    name: t.template_name,
    manager: t.manager || '',
    owner: t.owner || '',
    approvalBasis: t.approval_basis || '',
    sensitivity: (t.project_sensitivity_level as Sensitivity) || 'general',
    saving: false,
  }
}
async function saveProject() {
  if (!tree.value) return
  const d = projectDialog.value
  if (!d.name.trim()) return notify('请填写项目名称', 'error')
  d.saving = true
  try {
    await localTemplateApi.update(templateId.value, {
      class_code: tree.value.template.class_code || undefined,
      scope: tree.value.template.scope as any,
      template_name: d.name.trim(),
      manager: d.manager,
      owner: d.owner,
      approval_basis: d.approvalBasis,
      sensitivity_level: d.sensitivity,
    })
    projectDialog.value.show = false
    await load()
    notify('项目信息已更新')
  } catch (e: any) {
    notify('更新失败：' + (e?.message || String(e)), 'error')
  } finally {
    d.saving = false
  }
}

// 2026-06-02 立项归一：模版编辑器不再「确认立项」。立项统一走「立项」(集中立项)页，
// 承接时选用已发布模版。此处只保留模版的编辑与「发布」。

// ── 发布/取消发布：只有已发布的模版才能立项 ──
const publishing = ref(false)
const isPublished = computed(() => tree.value?.template.is_published === 1)
async function togglePublish() {
  if (!tree.value) return
  publishing.value = true
  const next = !isPublished.value
  try {
    await localTemplateApi.setPublished(templateId.value, next)
    notify(next ? '已发布，现在可以立项' : '已取消发布')
    await load()
  } catch (e: any) {
    notify((next ? '发布' : '取消发布') + '失败：' + (e?.message || String(e)), 'error')
  } finally {
    publishing.value = false
  }
}

function toggle(key: string) {
  // 默认（undefined）视为展开（与模板 `!== false` 判断一致）；
  // 基于"当前显示状态"翻转，避免首次 undefined→true 不产生视觉变化（需点两下）。
  const expandedNow = expanded.value[key] !== false
  expanded.value[key] = !expandedNow
}
// 当前是否展开（供模板复用，语义集中）
function isOpen(key: string): boolean {
  return expanded.value[key] !== false
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    tree.value = await localTemplateApi.tree(templateId.value)
  } catch (e: any) {
    error.value = e?.message || String(e)
  } finally {
    loading.value = false
  }
}

// ---------- 工作事项 ----------
// 责任人/参与人以 username 选择（防重名）；显示名在保存时由 username 解析后一并存
const stageDialog = ref({
  show: false,
  editingId: null as number | null,
  name: '',
  managerUsername: '',
  membersUsernames: [] as string[],
  desc: '',
})
function openCreateStage() {
  stageDialog.value = { show: true, editingId: null, name: '', managerUsername: '', membersUsernames: [], desc: '' }
}
function openEditStage(s: StageNode) {
  stageDialog.value = {
    show: true,
    editingId: s.id,
    name: s.stage_name,
    managerUsername: s.manager_username || '',
    membersUsernames: s.members_usernames ? s.members_usernames.split(',').map((x) => x.trim()).filter(Boolean) : [],
    desc: s.description || '',
  }
}
async function saveStage() {
  const d = stageDialog.value
  if (!d.name.trim()) return notify('请填写事项名称', 'error')
  const payload = {
    name: d.name,
    manager: d.managerUsername ? displayOf(d.managerUsername) : '',
    manager_username: d.managerUsername,
    members: d.membersUsernames.map(displayOf).join(','),
    members_usernames: d.membersUsernames.join(','),
    desc: d.desc,
  }
  try {
    if (d.editingId == null) {
      await stageApi.create({ template_id: templateId.value, ...payload })
    } else {
      await stageApi.update(d.editingId, payload)
    }
    stageDialog.value.show = false
    await load()
    notify('已保存事项')
  } catch (e: any) {
    notify('保存失败：' + (e?.message || String(e)), 'error')
  }
}
async function removeStage(s: StageNode) {
  if (!(await askConfirm(`删除事项「${s.stage_name}」？其下任务与标识将一并删除。`))) return
  try {
    await stageApi.remove(s.id)
    await load()
    notify('已删除事项')
  } catch (e: any) {
    notify('删除失败：' + (e?.message || String(e)), 'error')
  }
}

// ---------- 文件任务 ----------
const taskDialog = ref({ show: false, editingId: null as number | null, stageId: 0, name: '', manager: '', sensitivity_level: 'general' as Sensitivity, desc: '' })
function openCreateTask(stageId: number) {
  taskDialog.value = { show: true, editingId: null, stageId, name: '', manager: '', sensitivity_level: 'general', desc: '' }
}
function openEditTask(t: TaskNode) {
  taskDialog.value = { show: true, editingId: t.id, stageId: t.template_stage_id, name: t.task_name, manager: t.manager || '', sensitivity_level: (t.sensitivity_level as Sensitivity) || 'general', desc: t.description || '' }
}
async function saveTask() {
  const d = taskDialog.value
  if (!d.name.trim()) return notify('请填写任务名称', 'error')
  try {
    if (d.editingId == null) {
      await taskApi.create({ stage_id: d.stageId, name: d.name, manager: d.manager, sensitivity_level: d.sensitivity_level, desc: d.desc })
    } else {
      await taskApi.update(d.editingId, { name: d.name, manager: d.manager, sensitivity_level: d.sensitivity_level, desc: d.desc })
    }
    taskDialog.value.show = false
    await load()
    notify('已保存任务')
  } catch (e: any) {
    notify('保存失败：' + (e?.message || String(e)), 'error')
  }
}
async function removeTask(t: TaskNode) {
  if (!(await askConfirm(`删除任务「${t.task_name}」？其下标识将一并删除。`))) return
  try {
    await taskApi.remove(t.id)
    await load()
    notify('已删除任务')
  } catch (e: any) {
    notify('删除失败：' + (e?.message || String(e)), 'error')
  }
}

// ---------- 文档标识 ----------
function emptyRule() {
  return {
    show: false,
    editingId: null as number | null,
    taskId: 0,
    file_name: '',
    data_state: 'process' as DataState,
    required: false,
    allowed_file_types: '',
    naming_pattern: '',
    drafter: '',
    sensitivity_level: 'general' as Sensitivity,
    category: null as string | null,
    security_requirement: null as string | null,
    diffusion_requirement: null as string | null,
    archive_requirement: null as string | null,
    retention_period_days: null as number | null,
    destruction_rule: '',
  }
}
const L6_CATEGORY = ['未识别文档', '个人文档', '工作文档', '非责任文档']
const L6_SECURITY = ['明文存储', '加密存储']
const L6_DIFFUSION = ['孤本模式', '双孤本模式']
const L6_ARCHIVE_REQ = ['个人文件夹', '部门文件柜', '单位文件室']
const ruleDialog = ref(emptyRule())
function openCreateRule(taskId: number) {
  ruleDialog.value = { ...emptyRule(), show: true, taskId }
}
function openEditRule(fr: TemplateFileRule) {
  ruleDialog.value = {
    show: true,
    editingId: fr.id,
    taskId: fr.template_task_id || 0,
    file_name: fr.file_name,
    data_state: fr.data_state,
    required: fr.required === 1,
    allowed_file_types: fr.allowed_file_types,
    naming_pattern: fr.naming_pattern || '',
    drafter: fr.drafter || '',
    sensitivity_level: (fr.sensitivity_level as Sensitivity) || 'general',
    category: (fr as any).category ?? null,
    security_requirement: (fr as any).security_requirement ?? null,
    diffusion_requirement: (fr as any).diffusion_requirement ?? null,
    archive_requirement: (fr as any).archive_requirement ?? null,
    retention_period_days: (fr as any).retention_period_days ?? null,
    destruction_rule: (fr as any).destruction_rule ?? '',
  }
}
async function saveRule() {
  const d = ruleDialog.value
  if (!d.file_name.trim()) return notify('请填写文档实际名称', 'error')
  const payload = {
    file_name: d.file_name,
    data_state: 'process', // 模版层一律过程文件
    required: d.required,
    allowed_file_types: d.allowed_file_types,
    naming_pattern: d.naming_pattern,
    drafter: d.drafter,
    sensitivity_level: d.sensitivity_level,
    category: d.category ?? '',
    security_requirement: d.security_requirement ?? '',
    diffusion_requirement: d.diffusion_requirement ?? '',
    archive_requirement: d.archive_requirement ?? '',
    retention_period_days: d.retention_period_days,
    destruction_rule: d.destruction_rule,
  }
  try {
    if (d.editingId == null) {
      await fileRuleApi.create({ task_id: d.taskId, ...payload })
    } else {
      await fileRuleApi.update(d.editingId, payload)
    }
    ruleDialog.value.show = false
    await load()
    notify('已保存标识')
  } catch (e: any) {
    notify('保存失败：' + (e?.message || String(e)), 'error')
  }
}
async function removeRule(fr: TemplateFileRule) {
  if (!(await askConfirm(`删除文档标识「${fr.file_name}」？`))) return
  try {
    await fileRuleApi.remove(fr.id)
    await load()
    notify('已删除标识')
  } catch (e: any) {
    notify('删除失败：' + (e?.message || String(e)), 'error')
  }
}

onMounted(async () => {
  await loadManagers()
  await load()
})
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-btn variant="text" icon="mdi-arrow-left" @click="router.push('/template-authoring')" />
      <v-icon class="mr-2">mdi-file-tree</v-icon>
      <span>{{ tree?.template.template_name || '模版结构编辑' }}</span>
      <v-chip
        v-if="tree"
        size="small"
        class="ml-3"
        :color="tree.template.project_sensitivity_level === 'core' ? 'error' : tree.template.project_sensitivity_level === 'important' ? 'warning' : 'success'"
        variant="tonal"
      >
        {{ SENSITIVITY_LABELS[tree.template.project_sensitivity_level] || tree.template.project_sensitivity_level }}
      </v-chip>
      <v-chip v-if="tree" size="small" class="ml-2" :color="isPublished ? 'success' : 'grey'" variant="tonal">
        {{ isPublished ? '已发布' : '未发布' }}
      </v-chip>
      <v-spacer />
      <v-btn variant="text" prepend-icon="mdi-pencil" :disabled="!tree" @click="openEditProject">改项目信息</v-btn>
      <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
      <v-btn
        :color="isPublished ? 'warning' : 'success'"
        variant="tonal"
        :prepend-icon="isPublished ? 'mdi-publish-off' : 'mdi-publish'"
        :disabled="!tree"
        :loading="publishing"
        @click="togglePublish"
      >{{ isPublished ? '取消发布' : '发布' }}</v-btn>
    </v-card-title>
    <v-card-subtitle v-if="tree">
      {{ tree.template.template_code }} ·
      {{ SCOPE_LABELS[tree.template.scope] || tree.template.scope }}
      <span v-if="tree.template.manager"> · 负责人 {{ tree.template.manager }}</span>
    </v-card-subtitle>

    <v-alert v-if="error" type="error" variant="tonal" density="compact" class="ma-3">{{ error }}</v-alert>

    <v-card-text>
      <div class="d-flex align-center mb-2">
        <span class="text-subtitle-2">工作事项树</span>
        <v-spacer />
        <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-plus" @click="openCreateStage">新建工作事项</v-btn>
      </div>

      <div v-if="tree && tree.stages.length === 0" class="text-grey text-center py-6">
        暂无工作事项，点「新建工作事项」开始搭建结构
      </div>

      <!-- 事项 -->
      <div v-for="stage in tree?.stages || []" :key="stage.id" class="tree-stage">
        <div class="tree-row stage-row" data-test="stage-row">
          <v-btn :icon="isOpen(`stage-${stage.id}`) ? 'mdi-chevron-down' : 'mdi-chevron-right'" size="x-small" variant="text" @click="toggle(`stage-${stage.id}`)" />
          <v-icon size="small" class="mr-1">mdi-clipboard-text-outline</v-icon>
          <span class="font-weight-medium">{{ stage.stage_name }}</span>
          <v-chip size="x-small" variant="outlined" class="ml-2">{{ stage.stage_code }}</v-chip>
          <span v-if="stage.manager" class="text-caption text-grey ml-2">责任人 {{ stage.manager }}</span>
          <v-spacer />
          <v-btn size="x-small" variant="text" color="primary" @click="openCreateTask(stage.id)">+ 文件任务</v-btn>
          <v-btn size="x-small" variant="text" @click="openEditStage(stage)">编辑</v-btn>
          <v-btn size="x-small" variant="text" color="error" @click="removeStage(stage)">删除</v-btn>
        </div>

        <div v-show="isOpen(`stage-${stage.id}`)" class="tree-children">
          <div v-if="stage.tasks.length === 0" class="text-caption text-grey pl-8 py-1">（无文件任务）</div>
          <!-- 任务 -->
          <div v-for="task in stage.tasks" :key="task.id" class="tree-task">
            <div class="tree-row task-row" data-test="task-row">
              <v-btn :icon="isOpen(`task-${task.id}`) ? 'mdi-chevron-down' : 'mdi-chevron-right'" size="x-small" variant="text" @click="toggle(`task-${task.id}`)" />
              <v-icon size="small" class="mr-1">mdi-file-document-outline</v-icon>
              <span>{{ task.task_name }}</span>
              <v-chip v-if="task.sensitivity_level" size="x-small" variant="tonal" class="ml-2"
                :color="task.sensitivity_level === 'core' ? 'error' : task.sensitivity_level === 'important' ? 'warning' : 'success'">
                {{ SENSITIVITY_LABELS[task.sensitivity_level] || task.sensitivity_level }}
              </v-chip>
              <span v-if="task.manager" class="text-caption text-grey ml-2">承办 {{ task.manager }}</span>
              <v-spacer />
              <v-btn size="x-small" variant="text" color="primary" @click="openCreateRule(task.id)">+ 文档标识</v-btn>
              <v-btn size="x-small" variant="text" @click="openEditTask(task)">编辑</v-btn>
              <v-btn size="x-small" variant="text" color="error" @click="removeTask(task)">删除</v-btn>
            </div>

            <div v-show="isOpen(`task-${task.id}`)" class="tree-children">
              <div v-if="task.file_rules.length === 0" class="text-caption text-grey pl-12 py-1">（无文档标识）</div>
              <!-- 标识（叶子） -->
              <div v-for="fr in task.file_rules" :key="fr.id" class="tree-row rule-row" data-test="rule-row">
                <v-icon size="small" class="mr-1 ml-6">mdi-file-outline</v-icon>
                <span>{{ fr.file_name }}</span>
                <v-chip size="x-small" variant="outlined" class="ml-1">{{ fr.allowed_file_types }}</v-chip>
                <v-chip v-if="fr.required === 1" size="x-small" color="error" variant="text" class="ml-1">必填</v-chip>
                <span v-if="fr.naming_pattern" class="text-caption text-grey ml-2">命名 {{ fr.naming_pattern }}</span>
                <v-spacer />
                <v-btn size="x-small" variant="text" @click="openEditRule(fr)">编辑</v-btn>
                <v-btn size="x-small" variant="text" color="error" @click="removeRule(fr)">删除</v-btn>
              </div>
            </div>
          </div>
        </div>
      </div>
    </v-card-text>

    <!-- 事项弹窗 -->
    <v-dialog v-model="stageDialog.show" max-width="520" persistent>
      <v-card>
        <v-card-title>{{ stageDialog.editingId == null ? '新建工作事项' : '编辑工作事项' }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="stageDialog.name" label="事项名称 *" density="compact" variant="outlined" />
          <div class="d-flex ga-3">
            <v-select
              v-model="stageDialog.managerUsername"
              :items="managers"
              item-title="display_name"
              item-value="username"
              label="责任人"
              density="compact"
              variant="outlined"
              clearable
              hint="从 manage 已注册用户中选择（按账号唯一标识，防重名）"
              persistent-hint
            />
            <v-select
              v-model="stageDialog.membersUsernames"
              :items="managers"
              item-title="display_name"
              item-value="username"
              label="参与人（可多选）"
              density="compact"
              variant="outlined"
              multiple
              chips
              closable-chips
              hint="多选已注册用户"
              persistent-hint
            />
          </div>
          <v-textarea v-model="stageDialog.desc" label="内容描述" rows="2" density="compact" variant="outlined" />
        </v-card-text>
        <v-card-actions>
          <v-spacer /><v-btn variant="text" @click="stageDialog.show = false">取消</v-btn>
          <v-btn color="primary" @click="saveStage">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 任务弹窗 -->
    <v-dialog v-model="taskDialog.show" max-width="520" persistent>
      <v-card>
        <v-card-title>{{ taskDialog.editingId == null ? '新建文件任务' : '编辑文件任务' }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="taskDialog.name" label="任务名称 *" density="compact" variant="outlined" />
          <div class="d-flex ga-3">
            <v-combobox
              v-model="taskDialog.manager"
              :items="managerNames"
              label="承办人"
              density="compact"
              variant="outlined"
              hint="从 manage 已注册用户中选择，也可手填"
              persistent-hint
            />
            <v-select v-model="taskDialog.sensitivity_level" :items="sensitivityOptions" label="敏感级别" density="compact" variant="outlined" />
          </div>
          <v-textarea v-model="taskDialog.desc" label="任务说明" rows="2" density="compact" variant="outlined" />
        </v-card-text>
        <v-card-actions>
          <v-spacer /><v-btn variant="text" @click="taskDialog.show = false">取消</v-btn>
          <v-btn color="primary" @click="saveTask">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 标识弹窗 -->
    <v-dialog v-model="ruleDialog.show" max-width="560" persistent>
      <v-card>
        <v-card-title>{{ ruleDialog.editingId == null ? '新建文档标识' : '编辑文档标识' }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="ruleDialog.file_name" label="文档实际名称 *" density="compact" variant="outlined" />
          <div class="d-flex ga-3">
            <v-text-field v-model="ruleDialog.allowed_file_types" label="允许文件类型" placeholder="单个类型，如 DOCX（不可用 PDF）" density="compact" variant="outlined" />
          </div>
          <div class="d-flex ga-3 align-center">
            <v-text-field v-model="ruleDialog.naming_pattern" label="命名模式" density="compact" variant="outlined" />
            <v-select v-model="ruleDialog.sensitivity_level" :items="sensitivityOptions" label="敏感级别" density="compact" variant="outlined" />
          </div>
          <div class="d-flex ga-3 align-center">
            <v-combobox
              v-model="ruleDialog.drafter"
              :items="managerNames"
              label="起草人"
              density="compact"
              variant="outlined"
              hint="从 manage 已注册用户中选择，也可手填"
              persistent-hint
            />
            <v-checkbox v-model="ruleDialog.required" label="必填" density="compact" hide-details />
          </div>
          <v-divider class="my-2" /><div class="text-caption text-grey mb-1">文档管控属性（L6）</div>
          <div class="d-flex ga-3">
            <v-select v-model="ruleDialog.category" :items="L6_CATEGORY" label="文档类别" density="compact" variant="outlined" clearable />
            <v-select v-model="ruleDialog.security_requirement" :items="L6_SECURITY" label="安全要求" density="compact" variant="outlined" clearable />
          </div>
          <div class="d-flex ga-3">
            <v-select v-model="ruleDialog.diffusion_requirement" :items="L6_DIFFUSION" label="防扩散要求" density="compact" variant="outlined" clearable />
            <v-select v-model="ruleDialog.archive_requirement" :items="L6_ARCHIVE_REQ" label="归档要求" density="compact" variant="outlined" clearable />
          </div>
          <div class="d-flex ga-3 align-center">
            <v-text-field v-model.number="ruleDialog.retention_period_days" type="number" label="保留期(天, -1 永久)" density="compact" variant="outlined" />
          </div>
          <v-textarea v-model="ruleDialog.destruction_rule" label="销毁规则" rows="2" density="compact" variant="outlined" />
        </v-card-text>
        <v-card-actions>
          <v-spacer /><v-btn variant="text" @click="ruleDialog.show = false">取消</v-btn>
          <v-btn color="primary" @click="saveRule">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 改项目信息 -->
    <v-dialog v-model="projectDialog.show" max-width="520" persistent>
      <v-card>
        <v-card-title>改项目信息</v-card-title>
        <v-card-text class="d-flex flex-column ga-3">
          <v-text-field v-model="projectDialog.name" label="项目名称" density="compact" hide-details />
          <v-text-field v-model="projectDialog.manager" label="负责人" density="compact" hide-details />
          <v-text-field v-model="projectDialog.owner" label="数据所有权归属" density="compact" hide-details />
          <v-select v-model="projectDialog.sensitivity" :items="sensitivityOptions" label="敏感级别" density="compact" hide-details />
          <v-textarea v-model="projectDialog.approvalBasis" label="立项依据" :rows="2" density="compact" hide-details />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="projectDialog.show = false">取消</v-btn>
          <v-btn color="primary" variant="flat" :loading="projectDialog.saving" @click="saveProject">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 通用确认（删除等，替代原生 confirm） -->
    <v-dialog v-model="confirmBox.show" max-width="420">
      <v-card>
        <v-card-text class="pt-5">{{ confirmBox.text }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirmRespond(false)">取消</v-btn>
          <v-btn color="error" variant="flat" @click="confirmRespond(true)">确定</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>

<style scoped>
.tree-row { display: flex; align-items: center; padding: 4px 6px; border-radius: 6px; }
.tree-row:hover { background: rgba(0,0,0,.03); }
.stage-row { font-size: 14px; }
.task-row { padding-left: 24px; }
.rule-row { padding-left: 48px; font-size: 13px; }
.tree-children { border-left: 1px dashed rgba(0,0,0,.1); margin-left: 14px; }
</style>
