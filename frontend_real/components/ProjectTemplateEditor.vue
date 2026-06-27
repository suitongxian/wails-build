<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { API_BASE } from '@/services/api'

// 立项过程中编辑「项目专属模版」：
//  - mode='stage'：项目负责人增删改工作事项（允许只加空环节，不必带文件任务）
//  - mode='task' ：环节责任人为本环节补齐/增删改文件任务与文档标识
// 所有改动落在项目专属模版（TPL-PRJ-<application_id>）上，保存时整树回灌 manage。

interface FileRule {
  id: number; file_rule_code: string; file_name: string; data_state: string
  required: number; allowed_file_types: string; naming_pattern: string | null
  summary_pattern: string | null; default_retention_policy: string | null
  sensitivity_level: string | null; drafter: string | null
  category: string | null; security_requirement: string | null; diffusion_requirement: string | null
  archive_requirement: string | null; retention_period_days: number | null; destruction_rule: string | null
}
const CATEGORY_OPTS = ['未识别文档', '个人文档', '工作文档', '非责任文档']
const SECURITY_OPTS = ['明文存储', '加密存储']
const DIFFUSION_OPTS = ['孤本模式', '双孤本模式']
const ARCHIVE_REQ_OPTS = ['个人文件夹', '部门文件柜', '单位文件室']
// 后端 /project-template 返回的树为「扁平」结构（内嵌字段提升 + 小写键 stages/tasks/file_rules）
interface TaskNode {
  id: number; task_code: string; task_name: string; description: string | null
  sensitivity_level: string | null; manager: string | null
  file_rules: FileRule[]
}
interface StageNode {
  id: number; stage_code: string; stage_name: string; description: string | null
  manager: string | null; manager_username: string | null; members: string | null; members_usernames: string | null
  tasks: TaskNode[]
}

const props = defineProps<{
  modelValue: boolean
  applicationId: number | string
  mode: 'stage' | 'task'
  stageCode?: string
  stageName?: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void; (e: 'saved'): void }>()

const show = computed({ get: () => props.modelValue, set: v => emit('update:modelValue', v) })
const loading = ref(false)
const saving = ref(false)
const templateId = ref<number | null>(null)
const stages = ref<StageNode[]>([])
const snack = ref({ show: false, text: '', color: 'success' })
const notify = (text: string, color = 'success') => { snack.value = { show: true, text, color } }

// 当前 mode='task' 聚焦的那个环节
const focusStage = computed<StageNode | null>(() =>
  props.mode === 'task' ? (stages.value.find(s => s.stage_code === props.stageCode) || null) : null)
// 展示的环节：task 模式只看聚焦的那个环节；stage 模式看全部环节（项目负责人搭整套框架）。
const visibleStages = computed<StageNode[]>(() =>
  props.mode === 'task' ? (focusStage.value ? [focusStage.value] : []) : stages.value)

async function loadTree() {
  loading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/project-template?application_id=${props.applicationId}`)
    const j = await r.json()
    if (!j.success) { notify('载入失败：' + (j.error || ''), 'error'); return }
    templateId.value = j.data.template_id
    stages.value = j.data.tree?.stages || []
  } catch (e: any) { notify('载入失败：' + (e?.message || String(e)), 'error') }
  finally { loading.value = false }
}

watch(show, v => { if (v) { templateId.value = null; stages.value = []; loadTree() } })

// ── 工作事项（stage）增删改 ──
// orig 保存原始记录：编辑时把未在本弹窗暴露的字段（责任人/参与人）原样回传，避免被后端覆盖清空。
const stageDialog = ref({ show: false, id: null as number | null, name: '', desc: '', orig: null as StageNode | null })
function openStageCreate() { stageDialog.value = { show: true, id: null, name: '', desc: '', orig: null } }
function openStageEdit(s: StageNode) { stageDialog.value = { show: true, id: s.id, name: s.stage_name, desc: s.description || '', orig: s } }
async function saveStage() {
  const d = stageDialog.value
  if (!d.name.trim()) return notify('请填写工作事项名称', 'error')
  const o = d.orig
  try {
    if (d.id == null) {
      await fetch(`${API_BASE}/template-stages`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ template_id: templateId.value, name: d.name, desc: d.desc }) })
    } else {
      // 回传原有责任人/参与人，避免 UpdateStage 把这些字段清空
      await fetch(`${API_BASE}/template-stages/${d.id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
        name: d.name, desc: d.desc,
        manager: o?.manager ?? '', manager_username: o?.manager_username ?? '',
        members: o?.members ?? '', members_usernames: o?.members_usernames ?? '',
      }) })
    }
    stageDialog.value.show = false
    await loadTree(); notify('已保存工作事项')
  } catch (e: any) { notify('保存失败：' + (e?.message || String(e)), 'error') }
}
// 删除确认：不用 window.confirm（Wails WebView 偶发不弹→点了没反应），改用 Vuetify 弹窗。
const confirmDel = ref({ show: false, text: '', run: null as null | (() => Promise<void>) })
function askDel(text: string, run: () => Promise<void>) { confirmDel.value = { show: true, text, run } }
async function doConfirmDel() {
  const run = confirmDel.value.run
  confirmDel.value.show = false
  if (run) { try { await run() } catch (e: any) { notify('删除失败：' + (e?.message || String(e)), 'error') } }
}
function delStage(s: StageNode) {
  askDel(`确定删除工作事项「${s.stage_name}」？其下文件任务与标识将一并删除。`, async () => {
    await fetch(`${API_BASE}/template-stages/${s.id}`, { method: 'DELETE' })
    await loadTree(); notify('已删除')
  })
}

// ── 文件任务（task）增删改 ──
const taskDialog = ref({ show: false, id: null as number | null, stageId: 0, name: '', sens: 'general', desc: '', orig: null as TaskNode | null })
function openTaskCreate(stage: StageNode) { taskDialog.value = { show: true, id: null, stageId: stage.id, name: '', sens: 'general', desc: '', orig: null } }
function openTaskEdit(t: TaskNode) { taskDialog.value = { show: true, id: t.id, stageId: 0, name: t.task_name, sens: t.sensitivity_level || 'general', desc: t.description || '', orig: t } }
async function saveTask() {
  const d = taskDialog.value
  if (!d.name.trim()) return notify('请填写文件任务名称', 'error')
  const o = d.orig
  try {
    if (d.id == null) {
      await fetch(`${API_BASE}/template-tasks`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ stage_id: d.stageId, name: d.name, sensitivity_level: d.sens, desc: d.desc }) })
    } else {
      // 回传原有承办人，避免 UpdateTask 把 manager 清空
      await fetch(`${API_BASE}/template-tasks/${d.id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: d.name, sensitivity_level: d.sens, desc: d.desc, manager: o?.manager ?? '' }) })
    }
    taskDialog.value.show = false
    await loadTree(); notify('已保存文件任务')
  } catch (e: any) { notify('保存失败：' + (e?.message || String(e)), 'error') }
}
function delTask(t: TaskNode) {
  askDel(`确定删除文件任务「${t.task_name}」？其下文档标识将一并删除。`, async () => {
    await fetch(`${API_BASE}/template-tasks/${t.id}`, { method: 'DELETE' })
    await loadTree(); notify('已删除')
  })
}

// ── 文档标识（file_rule）增删改 ──
const ruleDialog = ref({ show: false, id: null as number | null, taskId: 0, file_name: '', data_state: 'process', allowed: '', required: false, category: null as string | null, security: null as string | null, diffusion: null as string | null, archiveReq: null as string | null, retentionDays: null as number | null, destruction: '', orig: null as FileRule | null })
function openRuleCreate(t: TaskNode) { ruleDialog.value = { show: true, id: null, taskId: t.id, file_name: '', data_state: 'process', allowed: '', required: false, category: null, security: null, diffusion: null, archiveReq: null, retentionDays: null, destruction: '', orig: null } }
function openRuleEdit(r: FileRule) { ruleDialog.value = { show: true, id: r.id, taskId: 0, file_name: r.file_name, data_state: r.data_state, allowed: r.allowed_file_types, required: r.required === 1, category: r.category, security: r.security_requirement, diffusion: r.diffusion_requirement, archiveReq: r.archive_requirement, retentionDays: r.retention_period_days, destruction: r.destruction_rule || '', orig: r } }
async function saveRule() {
  const d = ruleDialog.value
  if (!d.file_name.trim()) return notify('请填写文档名称', 'error')
  // 过程文件不允许 PDF（PDF 是定稿/结果文件格式）
  if ((d.allowed || '').trim().toUpperCase() === 'PDF') return notify('过程文件不允许使用 PDF 格式，请改用可编辑格式（如 DOCX/XLSX）', 'error')
  const o = d.orig
  try {
    const l6 = {
      category: d.category ?? '', security_requirement: d.security ?? '', diffusion_requirement: d.diffusion ?? '',
      archive_requirement: d.archiveReq ?? '', retention_period_days: d.retentionDays, destruction_rule: d.destruction,
    }
    if (d.id == null) {
      await fetch(`${API_BASE}/template-file-rules`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ task_id: d.taskId, file_name: d.file_name, data_state: 'process', allowed_file_types: d.allowed, required: d.required, ...l6 }) })
    } else {
      // 回传原有命名规则/摘要/起草人/密级/归档策略，避免 UpdateFileRule 把这些字段清空
      await fetch(`${API_BASE}/template-file-rules/${d.id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
        file_name: d.file_name, data_state: 'process', allowed_file_types: d.allowed, required: d.required,
        naming_pattern: o?.naming_pattern ?? '', summary_pattern: o?.summary_pattern ?? '',
        drafter: o?.drafter ?? '', sensitivity_level: o?.sensitivity_level ?? '',
        retention_policy: o?.default_retention_policy ?? '',
        ...l6,
      }) })
    }
    ruleDialog.value.show = false
    await loadTree(); notify('已保存文档标识')
  } catch (e: any) { notify('保存失败：' + (e?.message || String(e)), 'error') }
}
function delRule(r: FileRule) {
  askDel(`确定删除文档标识「${r.file_name}」？`, async () => {
    await fetch(`${API_BASE}/template-file-rules/${r.id}`, { method: 'DELETE' })
    await loadTree(); notify('已删除')
  })
}


// ── 保存并同步到 manage（整树回灌）──
async function saveAndSync() {
  // 每个文件任务必须至少有一个文件标识：不允许只建空任务（文件标识的必填项 file_name 在其保存时已校验）。
  for (const s of visibleStages.value) {
    for (const t of s.tasks) {
      if (!t.file_rules || t.file_rules.length === 0) {
        notify(`文件任务「${t.task_name}」还没有文件标识，请先为它补齐文件标识再保存`, 'error')
        return
      }
    }
  }
  saving.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/save-project-template?application_id=${props.applicationId}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
    const j = await r.json()
    if (!j.success) { notify('同步失败：' + (j.error || ''), 'error'); return }
    notify('已保存并同步到 manage')
    emit('saved')
    show.value = false
  } catch (e: any) { notify('同步失败：' + (e?.message || String(e)), 'error') }
  finally { saving.value = false }
}
</script>

<template>
  <v-dialog v-model="show" max-width="720" persistent scrollable>
    <v-card>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-file-tree-outline</v-icon>
        {{ mode === 'stage' ? '编辑工作事项' : `补齐文件任务 — ${stageName || stageCode}` }}
      </v-card-title>
      <v-card-subtitle class="text-wrap">
        {{ mode === 'stage'
          ? '对本项目的工作事项增删改（可只加空环节，稍后由环节责任人补齐文件任务）。'
          : '为本环节补齐/增删改文件任务与文档标识。' }}
        改动保存后会同步记录到 manage。
      </v-card-subtitle>

      <v-card-text style="max-height:64vh;">
        <div v-if="loading" class="py-6 text-center"><v-progress-circular indeterminate /></div>

        <template v-else>
          <!-- 顶部：stage 模式可新增工作事项 -->
          <div v-if="mode === 'stage'" class="d-flex mb-2 align-center">
            <span class="text-caption text-grey">完整框架：工作事项 ▸ 文件任务 ▸ 文档标识，每层都可增删改</span>
            <v-spacer />
            <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-plus" @click="openStageCreate">新增工作事项</v-btn>
          </div>
          <v-alert v-if="mode === 'task' && focusStage && (focusStage.tasks || []).length === 0" type="warning" variant="tonal" density="compact" class="mb-3" border="start">
            本工作事项还没有文件任务，请补齐至少一个文件任务及其文档标识。
          </v-alert>

          <!-- 每个工作事项一张卡：环节信息 + 其下文件任务 + 每个任务的文档标识 -->
          <v-card v-for="s in visibleStages" :key="s.id" variant="outlined" class="mb-3">
            <v-card-text class="py-2">
              <!-- 工作事项行 -->
              <div class="d-flex align-center">
                <v-icon size="small" class="mr-1" color="indigo">mdi-folder-outline</v-icon>
                <b>{{ s.stage_name }}</b>
                <span class="text-caption text-grey ml-2">（{{ (s.tasks || []).length }} 个文件任务）</span>
                <v-spacer />
                <template v-if="mode === 'stage'">
                  <v-btn size="x-small" variant="tonal" color="primary" prepend-icon="mdi-pencil" class="mr-1" @click="openStageEdit(s)">编辑环节</v-btn>
                  <v-btn size="x-small" variant="tonal" color="error" prepend-icon="mdi-delete-outline" @click="delStage(s)">删除环节</v-btn>
                </template>
              </div>
              <div v-if="s.description" class="text-caption text-grey mt-1">{{ s.description }}</div>

              <!-- 文件任务 -->
              <div class="d-flex align-center mt-2">
                <span class="text-caption font-weight-medium">文件任务</span>
                <v-spacer />
                <v-btn size="x-small" color="primary" variant="text" prepend-icon="mdi-plus" @click="openTaskCreate(s)">加文件任务</v-btn>
              </div>
              <div v-if="(s.tasks || []).length === 0" class="pa-2 text-caption text-grey">暂无文件任务，点「加文件任务」开始。</div>

              <v-sheet v-for="t in (s.tasks || [])" :key="t.id" rounded border class="pa-2 mt-1">
                <div class="d-flex align-center">
                  <v-icon size="x-small" class="mr-1" color="blue-grey">mdi-file-document-outline</v-icon>
                  <span class="text-body-2 font-weight-medium">{{ t.task_name }}</span>
                  <v-spacer />
                  <v-btn size="x-small" variant="tonal" color="primary" prepend-icon="mdi-pencil" class="mr-1" @click="openTaskEdit(t)">编辑</v-btn>
                  <v-btn size="x-small" variant="tonal" color="error" prepend-icon="mdi-delete-outline" @click="delTask(t)">删除</v-btn>
                </div>
                <!-- 文档标识 -->
                <div class="d-flex align-center mt-1">
                  <span class="text-caption text-grey">文档标识</span>
                  <v-spacer />
                  <v-btn size="x-small" variant="text" prepend-icon="mdi-plus" @click="openRuleCreate(t)">加标识</v-btn>
                </div>
                <v-list density="compact" class="py-0">
                  <v-list-item v-for="rule in (t.file_rules || [])" :key="rule.id" class="px-1">
                    <v-list-item-title class="text-body-2">
                      {{ rule.file_name }}
                      <span class="text-caption text-grey">{{ rule.allowed_file_types }}</span>
                    </v-list-item-title>
                    <template #append>
                      <v-btn size="x-small" variant="tonal" color="primary" prepend-icon="mdi-pencil" class="mr-1" @click="openRuleEdit(rule)">编辑</v-btn>
                      <v-btn size="x-small" variant="tonal" color="error" prepend-icon="mdi-delete-outline" @click="delRule(rule)">删除</v-btn>
                    </template>
                  </v-list-item>
                  <div v-if="(t.file_rules || []).length === 0" class="pa-1 text-caption text-grey">暂无文档标识，点「加标识」</div>
                </v-list>
              </v-sheet>
            </v-card-text>
          </v-card>
          <div v-if="mode === 'stage' && visibleStages.length === 0" class="pa-4 text-caption text-grey">暂无工作事项，点「新增工作事项」开始。</div>
        </template>
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="show = false">关闭</v-btn>
        <v-btn color="primary" variant="flat" :loading="saving" prepend-icon="mdi-cloud-upload-outline" @click="saveAndSync">保存并同步</v-btn>
      </v-card-actions>
    </v-card>

    <!-- 工作事项 编辑弹窗 -->
    <v-dialog v-model="stageDialog.show" max-width="480">
      <v-card>
        <v-card-title>{{ stageDialog.id == null ? '新建工作事项' : '编辑工作事项' }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="stageDialog.name" label="工作事项名称 *" density="compact" />
          <v-textarea v-model="stageDialog.desc" label="描述（可选）" rows="2" density="compact" />
        </v-card-text>
        <v-card-actions><v-spacer /><v-btn variant="text" @click="stageDialog.show = false">取消</v-btn><v-btn color="primary" @click="saveStage">保存</v-btn></v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 文件任务 编辑弹窗 -->
    <v-dialog v-model="taskDialog.show" max-width="480">
      <v-card>
        <v-card-title>{{ taskDialog.id == null ? '新建文件任务' : '编辑文件任务' }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="taskDialog.name" label="文件任务名称 *" density="compact" />
          <v-select v-model="taskDialog.sens" :items="[{t:'一般',v:'general'},{t:'重要',v:'important'},{t:'核心',v:'core'}]" item-title="t" item-value="v" label="敏感级别" density="compact" />
          <v-textarea v-model="taskDialog.desc" label="描述（可选）" rows="2" density="compact" />
        </v-card-text>
        <v-card-actions><v-spacer /><v-btn variant="text" @click="taskDialog.show = false">取消</v-btn><v-btn color="primary" @click="saveTask">保存</v-btn></v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 文档标识 编辑弹窗 -->
    <v-dialog v-model="ruleDialog.show" max-width="480">
      <v-card>
        <v-card-title>{{ ruleDialog.id == null ? '新建文档标识' : '编辑文档标识' }}</v-card-title>
        <v-card-text>
          <v-text-field v-model="ruleDialog.file_name" label="文档名称 *" density="compact" />
          <v-text-field v-model="ruleDialog.allowed" label="允许文件类型" placeholder="单个类型，如 DOCX（不可用 PDF）" density="compact" />
          <div class="d-flex ga-2">
            <v-select v-model="ruleDialog.category" :items="CATEGORY_OPTS" label="文档类别" density="compact" clearable />
            <v-select v-model="ruleDialog.security" :items="SECURITY_OPTS" label="安全要求" density="compact" clearable />
          </div>
          <div class="d-flex ga-2">
            <v-select v-model="ruleDialog.diffusion" :items="DIFFUSION_OPTS" label="防扩散要求" density="compact" clearable />
            <v-select v-model="ruleDialog.archiveReq" :items="ARCHIVE_REQ_OPTS" label="归档要求" density="compact" clearable />
          </div>
          <v-text-field v-model.number="ruleDialog.retentionDays" type="number" label="保留期(天, -1 永久)" density="compact" />
          <v-textarea v-model="ruleDialog.destruction" label="销毁规则" rows="2" density="compact" />
          <v-checkbox v-model="ruleDialog.required" label="必填" density="compact" hide-details />
        </v-card-text>
        <v-card-actions><v-spacer /><v-btn variant="text" @click="ruleDialog.show = false">取消</v-btn><v-btn color="primary" @click="saveRule">保存</v-btn></v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 删除确认（替代不可靠的 window.confirm）-->
    <v-dialog v-model="confirmDel.show" max-width="420">
      <v-card>
        <v-card-title>确认删除</v-card-title>
        <v-card-text>{{ confirmDel.text }}</v-card-text>
        <v-card-actions><v-spacer /><v-btn variant="text" @click="confirmDel.show = false">取消</v-btn><v-btn color="error" variant="flat" @click="doConfirmDel">删除</v-btn></v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snack.show" :color="snack.color" timeout="2500">{{ snack.text }}</v-snackbar>
  </v-dialog>
</template>
