<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { API_BASE } from '@/services/api'

interface MyTask {
  application_id: number
  stage_code: string
  stage_name: string | null
  task_code: string
  task_name: string
  status: string
  project_name: string
  project_code: string | null
  template_code: string | null
  template_version: string | null
  project_scope?: string | null      // 立项所选层级 person/department/unit（随 my-tasks 下发）
  sensitivity_level?: string | null  // 项目敏感级 core/important/general
  output_custody_scope?: string | null // 定稿保管层级 unit/department
  output_custody_note?: string | null  // 归档归属说明
}

interface DocItem { name: string; size: number; mod_time: string; is_dir: boolean; empty?: boolean }
// 文档标识属性（工作受理展示「该任务应交哪些文件、各自要求」）。
interface FileRuleAttr {
  file_rule_code: string
  file_name: string
  data_state: string // input/process/output
  required: number
  allowed_file_types: string
  naming_pattern: string | null
  summary_pattern: string | null
  sensitivity_level: string | null
  drafter: string | null
}
// 是否未填写的占位（后端 empty 标记；兜底用 size===0）。PDF 占位非 0 字节，必须看 empty。
function isPlaceholderDoc(d: DocItem): boolean { return d.empty ?? d.size === 0 }

const loading = ref(false)
const items = ref<MyTask[]>([])
const snackbar = ref({ show: false, text: '', color: 'success' })
const starting = ref<string | null>(null)
const completing = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/my-tasks`)
    const j = await r.json()
    if (j.success) items.value = (j.data || []) as MyTask[]
  } finally { loading.value = false }
}

// 一键归档：把我经手的项目目录按九宫格归档（个人→本地夹复制 / 部门、单位→上报云端 / 行业→跳过）。
// 文件真正落在工作受理这一步的本机，故归档入口放这里。按我工作事项里出现的项目逐个归档。
const archiving = ref(false)
async function onQuickArchive() {
  // 按项目去重：取每个项目第一条任务携带的项目上下文（编码/名称/层级/敏感级，随 my-tasks 下发）。
  const byApp = new Map<number, MyTask>()
  for (const t of items.value) {
    if (t.application_id && !byApp.has(t.application_id)) byApp.set(t.application_id, t)
  }
  if (!byApp.size) { snackbar.value = { show: true, text: '暂无可归档的项目', color: 'info' }; return }
  archiving.value = true
  let archived = 0, skipped = 0, errs = 0
  try {
    for (const [id, t] of byApp) {
      try {
        const r = await fetch(`${API_BASE}/centralized-projects/${id}/quick-archive`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            project_code: t.project_code || '',
            project_name: t.project_name || '',
            project_scope: t.project_scope || '',
            sensitivity_level: t.sensitivity_level || '',
            output_custody_scope: t.output_custody_scope || '',
            output_custody_note: t.output_custody_note || '',
          }),
        })
        const j = await r.json()
        if (j.success) {
          archived += j.data?.archived || 0
          skipped += j.data?.skipped || 0
          errs += (j.data?.errors || []).length
        } else { errs++ }
      } catch { errs++ }
    }
    snackbar.value = {
      show: true,
      text: `归档完成：新归档 ${archived} 个、跳过 ${skipped} 个` + (errs ? `，${errs} 个出错` : ''),
      color: errs ? 'warning' : 'success',
    }
  } finally { archiving.value = false }
}

const todoItems = computed(() => items.value.filter(t => t.status === 'pending'))
const doingItems = computed(() => items.value.filter(t => t.status === 'in_progress'))
const doneItems = computed(() => items.value.filter(t => t.status === 'completed'))

// 待办/进行中/已完成 改为 tab：点选指定 tab 才展示对应看板。
const tab = ref<'todo' | 'doing' | 'done'>('todo')

// 进入页面默认展示第一个非空 tab：待办 > 进行中 > 已完成（都空则停在待办）。
function pickDefaultTab() {
  if (todoItems.value.length) tab.value = 'todo'
  else if (doingItems.value.length) tab.value = 'doing'
  else if (doneItems.value.length) tab.value = 'done'
  else tab.value = 'todo'
}

async function startWork(it: MyTask) {
  starting.value = it.task_code
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/start-task`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ application_id: it.application_id, stage_code: it.stage_code, task_code: it.task_code, template_code: it.template_code, template_version: it.template_version, project_code: it.project_code }),
    })
    const j = await r.json()
    if (j.success) {
      snackbar.value = { show: true, text: `已开始工作，按模版预建 ${j.data?.scaffolded ?? 0} 个过程文档，点「在线编辑」填写`, color: 'success' }
      await load()
      // 开始后直接打开在线编辑
      const fresh = items.value.find(t => t.application_id === it.application_id && t.stage_code === it.stage_code && t.task_code === it.task_code)
      if (fresh) openEditor(fresh)
    } else {
      snackbar.value = { show: true, text: '开始失败：' + (j.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '开始失败：' + (e?.message || String(e)), color: 'error' }
  } finally { starting.value = null }
}

async function completeWork(it: MyTask) {
  completing.value = it.task_code
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/complete-task`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ application_id: it.application_id, stage_code: it.stage_code, task_code: it.task_code }),
    })
    const j = await r.json()
    if (j.success) { snackbar.value = { show: true, text: '任务已完成', color: 'success' }; await load() }
    else snackbar.value = { show: true, text: '完成失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '完成失败：' + (e?.message || String(e)), color: 'error' }
  } finally { completing.value = null }
}

// ── 任务级定稿：谁编辑谁挑。完成前按本任务 output 标识从本任务过程文件挑定稿 ──
interface OutputRule { file_rule_code: string; file_name: string; allowed_file_types: string }
const finalsDialog = ref(false)
const finalsTarget = ref<MyTask | null>(null)
const finalsRules = ref<OutputRule[]>([])
const finalsFiles = ref<string[]>([])
const finalsPick = ref<Record<string, string>>({}) // file_rule_code → 选中的过程文件名
const finalsGenericPick = ref('') // 通用定稿（无定稿标识时）选中的文件
const finalsLoading = ref(false)
const finalsSubmitting = ref(false)

// 无定稿标识（通用模式）：让用户从过程文件里挑一个作定稿。
const isGenericFinals = computed(() => finalsRules.value.length === 0)

// 点「完成」：取本任务定稿候选并弹窗——有定稿标识按标识逐个挑，无标识则从过程文件挑一个作定稿。
async function startComplete(it: MyTask) {
  completing.value = it.task_code
  try {
    const qs = `app_id=${it.application_id}&stage_code=${encodeURIComponent(it.stage_code)}&task_code=${encodeURIComponent(it.task_code)}&template_code=${encodeURIComponent(it.template_code || '')}${it.project_code ? `&project_code=${encodeURIComponent(it.project_code)}` : ''}`
    const r = await fetch(`${API_BASE}/centralized-projects/task-finals-candidates?${qs}`)
    const j = await r.json()
    if (!j.success) { snackbar.value = { show: true, text: '取定稿候选失败：' + (j.error || ''), color: 'error' }; return }
    finalsTarget.value = it
    finalsRules.value = j.data?.output_rules || []
    finalsFiles.value = j.data?.process_files || []
    finalsPick.value = {}
    finalsGenericPick.value = ''
    finalsDialog.value = true // 总是弹窗让用户选定稿
  } catch (e: any) {
    snackbar.value = { show: true, text: '取定稿候选失败：' + (e?.message || String(e)), color: 'error' }
  } finally { completing.value = null }
}

const canSubmitFinals = computed(() => {
  if (isGenericFinals.value) return !!finalsGenericPick.value
  return finalsFiles.value.length > 0 && finalsRules.value.every(r => !!finalsPick.value[r.file_rule_code])
})

// 无定稿可挑（过程文件全空/不存在）时，允许不留定稿直接完成。
async function completeWithoutFinals() {
  const it = finalsTarget.value
  if (!it) return
  finalsDialog.value = false
  await completeWork(it)
}

// 提交定稿 → 成功后再完成任务。
async function submitFinalsAndComplete() {
  const it = finalsTarget.value
  if (!it || !canSubmitFinals.value) return
  finalsSubmitting.value = true
  try {
    const selections = isGenericFinals.value
      ? [{ file_rule_code: '', source_file: finalsGenericPick.value }]
      : finalsRules.value.map(r => ({ file_rule_code: r.file_rule_code, source_file: finalsPick.value[r.file_rule_code] }))
    const r = await fetch(`${API_BASE}/centralized-projects/submit-task-finals`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ app_id: it.application_id, stage_code: it.stage_code, task_code: it.task_code, template_code: it.template_code || '', project_code: it.project_code || '', selections }),
    })
    const j = await r.json()
    if (!j.success) { snackbar.value = { show: true, text: '提交定稿失败：' + (j.error || ''), color: 'error' }; return }
    finalsDialog.value = false
    await completeWork(it)
  } catch (e: any) {
    snackbar.value = { show: true, text: '提交定稿失败：' + (e?.message || String(e)), color: 'error' }
  } finally { finalsSubmitting.value = false }
}

// ── 文件任务窗口：列出工作依据(input) + 过程(process)，点文件→本机打开 ──
type WBucket = 'input' | 'reference' | 'process' | 'output'
const editorDialog = ref(false)
const editorTarget = ref<MyTask | null>(null)
const inputDocs = ref<DocItem[]>([])
const referenceDocs = ref<DocItem[]>([]) // 参考文件（外部导入）
const processDocs = ref<DocItem[]>([])
const outputDocs = ref<DocItem[]>([])    // 结果文件（定稿/产出）
// 该任务的文档标识属性清单（应交文件及要求）
const fileRules = ref<FileRuleAttr[]>([])
// 本机打开失败时记录该文件，窗口里浮出「在线编辑/查看」兜底按钮（点了才弹在线编辑窗口）。
const openFailed = ref<{ bucket: WBucket; name: string } | null>(null)
// 参考文件导入
const importing = ref(false)
const refFileInput = ref<HTMLInputElement | null>(null)

// ── 在线编辑窗口（二级弹窗，兜底）：input 只读查看 / process 可编辑 ──
const docDialog = ref(false)
const currentDoc = ref('')      // 正在编辑的过程文档名
const viewingInput = ref('')    // 正在只读查看的工作依据名（非空时只读）
const content = ref('')
const editorLoading = ref(false)
const savingDoc = ref(false)
const savedPath = ref('')

// 五层落盘：workbench 调用需带 project_code（目录名）+ task_code（定位到文件任务目录）。
function pcParam(it: MyTask): string {
  let s = it.project_code ? `&project_code=${encodeURIComponent(it.project_code)}` : ''
  if (it.task_code) s += `&task_code=${encodeURIComponent(it.task_code)}`
  return s
}

async function fetchDocs(it: MyTask) {
  const r = await fetch(`${API_BASE}/centralized-projects/workbench/files?app_id=${it.application_id}&stage_code=${encodeURIComponent(it.stage_code)}${pcParam(it)}`)
  const j = await r.json()
  if (j.success) {
    inputDocs.value = (j.data?.buckets?.input || []).filter((f: DocItem) => !f.is_dir)
    referenceDocs.value = (j.data?.buckets?.reference || []).filter((f: DocItem) => !f.is_dir)
    processDocs.value = (j.data?.buckets?.process || []).filter((f: DocItem) => !f.is_dir)
    outputDocs.value = (j.data?.buckets?.output || []).filter((f: DocItem) => !f.is_dir)
  } else {
    inputDocs.value = []; referenceDocs.value = []; processDocs.value = []; outputDocs.value = []
    snackbar.value = { show: true, text: '加载文档失败：' + (j.error || ''), color: 'error' }
  }
}

// 本机打开（优先）：用终端默认 Office/WPS 等打开文件；过程文档本机编辑保存即生效，无需回传。
// 跨平台（windows/darwin/linux）由后端 internal.FileOpenerService 处理。
// 成功→清掉兜底提示；失败→记录该文件，窗口里浮出「在线编辑/查看」按钮（点了才弹在线编辑窗口）。
async function openLocal(bucket: WBucket, name: string) {
  const it = editorTarget.value
  if (!it) return
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/workbench/open?app_id=${it.application_id}&stage_code=${encodeURIComponent(it.stage_code)}&bucket=${bucket}&name=${encodeURIComponent(name)}${pcParam(it)}`)
    const j = await r.json()
    if (j.success) { openFailed.value = null; snackbar.value = { show: true, text: `已用本机程序打开「${name}」`, color: 'success' } }
    else { openFailed.value = { bucket, name }; snackbar.value = { show: true, text: '本机打开失败：' + (j.error || ''), color: 'warning' } }
  } catch (e: any) {
    openFailed.value = { bucket, name }
    snackbar.value = { show: true, text: '本机打开失败：' + (e?.message || String(e)), color: 'error' }
  }
}

// 参考文件导入：先选文件，再由导入者做「归类定级声明」（类别决定默认级别、可手动改级），确认后拷贝到 reference 桶。
const refImportDialog = ref(false)
const pendingRefFile = ref<File | null>(null)
const refCategoryOptions = [
  { title: '内部资料（默认重要级）', value: 'internal' },
  { title: '外部资料（默认一般级）', value: 'external' },
  { title: '公开资料（默认一般级）', value: 'public' },
]
const refLevelOptions = [
  { title: '核心级（保密）', value: 'core' },
  { title: '重要级（档案）', value: 'important' },
  { title: '一般（开放）级（资料）', value: 'general' },
]
const refForm = reactive<{ category: string; sensitivity_level: string }>({ category: 'internal', sensitivity_level: 'important' })
function defaultLevelForCategory(cat: string) { return cat === 'internal' ? 'important' : 'general' }
// 切换类别时，把级别重置为该类别的默认级（导入者仍可再手动改）。
function onRefCategoryChange(cat: string) { refForm.category = cat; refForm.sensitivity_level = defaultLevelForCategory(cat) }

function triggerImportReference() { refFileInput.value?.click() }
function onRefFilePicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  const it = editorTarget.value
  if (input) input.value = ''
  if (!file || !it) return
  pendingRefFile.value = file
  refForm.category = 'internal'
  refForm.sensitivity_level = 'important'
  refImportDialog.value = true
}
async function confirmImportReference() {
  const file = pendingRefFile.value
  const it = editorTarget.value
  if (!file || !it) { refImportDialog.value = false; return }
  importing.value = true
  try {
    const fd = new FormData()
    fd.append('app_id', String(it.application_id))
    fd.append('stage_code', it.stage_code)
    fd.append('task_code', it.task_code || '')
    fd.append('project_code', it.project_code || '')
    fd.append('category', refForm.category)
    fd.append('sensitivity_level', refForm.sensitivity_level)
    fd.append('file', file)
    const r = await fetch(`${API_BASE}/centralized-projects/workbench/import-reference`, { method: 'POST', body: fd })
    const j = await r.json()
    if (j.success) { snackbar.value = { show: true, text: `已导入参考文件「${j.data?.name || file.name}」`, color: 'success' }; refImportDialog.value = false; pendingRefFile.value = null; await fetchDocs(it) }
    else snackbar.value = { show: true, text: '导入失败：' + (j.error || ''), color: 'error' }
  } catch (err: any) {
    snackbar.value = { show: true, text: '导入失败：' + (err?.message || String(err)), color: 'error' }
  } finally { importing.value = false }
}

// 兜底：点「在线编辑/查看」按钮才弹出在线编辑窗口（process 可编辑、其它桶只读查看）。
function openInlineEditor(bucket: WBucket, name: string) {
  if (bucket === 'process') selectDoc(name)
  else viewDoc(bucket, name)
}

// 拉取该任务的文档标识属性清单（应交哪些文件、各自要求）。失败不阻断打开文档。
async function fetchFileRules(it: MyTask) {
  fileRules.value = []
  if (!it.template_code) return
  try {
    const qs = `stage_code=${encodeURIComponent(it.stage_code)}&task_code=${encodeURIComponent(it.task_code)}&template_code=${encodeURIComponent(it.template_code)}`
    const r = await fetch(`${API_BASE}/centralized-projects/task-file-rules?${qs}`)
    const j = await r.json()
    if (j.success) fileRules.value = (j.data || []) as FileRuleAttr[]
  } catch { /* 忽略，仅影响属性展示 */ }
}

const dataStateLabel = (s: string) => s === 'input' ? '工作依据' : s === 'output' ? '定稿' : s === 'process' ? '过程文件' : s
const dataStateColor = (s: string) => s === 'input' ? 'blue-grey' : s === 'output' ? 'success' : 'primary'
const sensLabel = (s: string | null) => s === 'core' ? '核心' : s === 'important' ? '重要' : s === 'general' ? '一般' : (s || '')

async function openEditor(it: MyTask) {
  editorTarget.value = it
  editorDialog.value = true
  editorLoading.value = true
  openFailed.value = null
  currentDoc.value = ''; viewingInput.value = ''; content.value = ''; savedPath.value = ''
  try {
    await Promise.all([fetchDocs(it), fetchFileRules(it)])
    // 本机打开优先：窗口只列文件，点文件→本机打开；不常驻在线编辑框（兜底才弹）。
  } finally { editorLoading.value = false }
}

async function readDoc(bucket: WBucket, name: string): Promise<string> {
  const it = editorTarget.value!
  const r = await fetch(`${API_BASE}/centralized-projects/workbench/doc?app_id=${it.application_id}&stage_code=${encodeURIComponent(it.stage_code)}&bucket=${bucket}&name=${encodeURIComponent(name)}${pcParam(it)}`)
  const j = await r.json()
  if (!j.success) throw new Error(j.error || '读取失败')
  return j.data?.content ?? ''
}

async function selectDoc(name: string) {
  currentDoc.value = name; viewingInput.value = ''; savedPath.value = ''
  docDialog.value = true
  editorLoading.value = true
  try { content.value = await readDoc('process', name) }
  catch (e: any) { snackbar.value = { show: true, text: '读取失败：' + (e?.message || String(e)), color: 'error' } }
  finally { editorLoading.value = false }
}

// 只读查看任意非过程桶（工作依据/参考文件/结果文件）的文本内容。
async function viewDoc(bucket: WBucket, name: string) {
  viewingInput.value = name; currentDoc.value = ''; savedPath.value = ''
  docDialog.value = true
  editorLoading.value = true
  try { content.value = await readDoc(bucket, name) }
  catch (e: any) { snackbar.value = { show: true, text: '读取失败：' + (e?.message || String(e)), color: 'error' } }
  finally { editorLoading.value = false }
}

async function saveDoc() {
  if (!currentDoc.value || !editorTarget.value) return
  const it = editorTarget.value
  savingDoc.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/workbench/doc`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ app_id: String(it.application_id), stage_code: it.stage_code, task_code: it.task_code || '', bucket: 'process', name: currentDoc.value, content: content.value, project_code: it.project_code || '' }),
    })
    const j = await r.json()
    if (j.success) { savedPath.value = j.data?.path || ''; snackbar.value = { show: true, text: '已保存', color: 'success' }; await fetchDocs(it) }
    else snackbar.value = { show: true, text: '保存失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '保存失败：' + (e?.message || String(e)), color: 'error' }
  } finally { savingDoc.value = false }
}

onMounted(async () => { await load(); pickDefaultTab() })
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-clipboard-check-outline</v-icon>
      工作受理
      <v-spacer />
      <v-btn color="success" variant="tonal" prepend-icon="mdi-archive-arrow-down" class="mr-2" :loading="archiving" @click="onQuickArchive">一键归档</v-btn>
      <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="load">刷新</v-btn>
    </v-card-title>
    <v-card-subtitle>环节负责人指派给我的文件任务：开始工作后在线编辑过程文档，完成后点「完成」。「一键归档」把我经手项目目录下的文件按九宫格归档（个人→本地夹 / 部门、单位→上报云端）。</v-card-subtitle>

    <v-card-text>
      <!-- 待办/进行中/已完成 改为 tab：点选才展示对应看板 -->
      <v-tabs v-model="tab" color="primary" class="mb-3">
        <v-tab value="todo"><span class="dot dot-todo mr-2"></span>待办<span class="tab-count">{{ todoItems.length }}</span></v-tab>
        <v-tab value="doing"><span class="dot dot-doing mr-2"></span>进行中<span class="tab-count">{{ doingItems.length }}</span></v-tab>
        <v-tab value="done"><span class="dot dot-done mr-2"></span>已完成<span class="tab-count">{{ doneItems.length }}</span></v-tab>
      </v-tabs>

      <v-window v-model="tab">
        <!-- 待办 -->
        <v-window-item value="todo">
          <v-card v-for="t in todoItems" :key="t.task_code" flat class="item-card bar-todo">
            <div class="proj"><span v-if="t.project_code" class="code">{{ t.project_code }}</span>{{ t.project_name }} · {{ t.stage_name || t.stage_code }}</div>
            <div class="stage">{{ t.task_name }}</div>
            <div class="acts">
              <v-btn size="small" color="primary" variant="tonal" :loading="starting === t.task_code" prepend-icon="mdi-play" @click="startWork(t)">开始工作</v-btn>
            </div>
          </v-card>
          <div v-if="!loading && todoItems.length === 0" class="col-empty">暂无待办</div>
        </v-window-item>

        <!-- 进行中 -->
        <v-window-item value="doing">
          <v-card v-for="t in doingItems" :key="t.task_code" flat class="item-card bar-doing">
            <div class="proj"><span v-if="t.project_code" class="code">{{ t.project_code }}</span>{{ t.project_name }} · {{ t.stage_name || t.stage_code }}</div>
            <div class="stage">{{ t.task_name }}</div>
            <div class="acts">
              <v-btn size="small" color="primary" variant="text" prepend-icon="mdi-folder-open-outline" @click="openEditor(t)">打开文档</v-btn>
              <v-btn size="small" color="success" variant="tonal" :loading="completing === t.task_code" prepend-icon="mdi-check" @click="startComplete(t)">完成</v-btn>
            </div>
          </v-card>
          <div v-if="!loading && doingItems.length === 0" class="col-empty">暂无进行中</div>
        </v-window-item>

        <!-- 已完成 -->
        <v-window-item value="done">
          <v-card v-for="t in doneItems" :key="t.task_code" flat class="item-card bar-done muted">
            <div class="proj"><span v-if="t.project_code" class="code">{{ t.project_code }}</span>{{ t.project_name }} · {{ t.stage_name || t.stage_code }}</div>
            <div class="stage">{{ t.task_name }}</div>
            <div class="acts">
              <v-btn size="small" color="primary" variant="text" prepend-icon="mdi-folder-open-outline" @click="openEditor(t)">打开文档</v-btn>
            </div>
          </v-card>
          <div v-if="!loading && doneItems.length === 0" class="col-empty">暂无已完成</div>
        </v-window-item>
      </v-window>

      <div v-if="!loading && items.length === 0" class="empty-all">
        <v-icon size="64" color="grey-lighten-1">mdi-clipboard-text-off-outline</v-icon>
        <div class="mt-4 text-grey">暂无指派给我的文件任务</div>
        <div class="text-caption text-grey-lighten-1 mt-1">需环节负责人在「任务指派」把任务派给当前账号</div>
      </div>
    </v-card-text>

    <!-- 文件任务窗口：列文件，点文件→本机打开；本机打不开才浮出在线编辑兜底按钮 -->
    <v-dialog v-model="editorDialog" max-width="640" persistent>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-folder-open-outline</v-icon>
          文件任务 — {{ editorTarget?.task_name }}
        </v-card-title>
        <v-card-subtitle class="text-wrap">点击文件用<b>本机 Office/WPS</b>打开（过程文档本机保存即生效）。若本机打不开，会出现「在线编辑」按钮兜底。</v-card-subtitle>
        <v-card-text>
          <!-- 文档属性：该任务应交哪些文件、各自要求（来自模版文档标识） -->
          <div v-if="fileRules.length" class="mb-3">
            <div class="text-caption text-grey mb-1">文档属性（本任务应交文件及要求）</div>
            <v-table density="compact" class="rule-table">
              <thead>
                <tr>
                  <th>文件</th><th>内容要求</th><th>允许格式</th><th>必填</th><th>密级</th><th>起草人</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="r in fileRules" :key="r.file_rule_code">
                  <td>
                    {{ r.file_name }}
                    <div v-if="r.naming_pattern" class="text-caption text-grey">命名：{{ r.naming_pattern }}</div>
                  </td>
                  <td class="text-caption" style="max-width:280px;white-space:normal;">{{ r.summary_pattern || '—' }}</td>
                  <td class="text-caption">{{ r.allowed_file_types || '—' }}</td>
                  <td><v-icon v-if="r.required" size="x-small" color="error">mdi-asterisk</v-icon><span v-else class="text-grey">—</span></td>
                  <td class="text-caption">{{ sensLabel(r.sensitivity_level) || '—' }}</td>
                  <td class="text-caption">{{ r.drafter || '—' }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>

          <!-- 本机打开失败 → 兜底：点按钮才弹在线编辑窗口 -->
          <v-alert v-if="openFailed" type="warning" variant="tonal" density="compact" class="mb-3" border="start">
            <div class="d-flex align-center flex-wrap" style="gap: 8px;">
              <span>「{{ openFailed.name }}」本机打不开（可能未装 Office/WPS 或无关联程序）。</span>
              <v-spacer />
              <v-btn size="small" color="primary" variant="flat"
                     :prepend-icon="openFailed.bucket === 'process' ? 'mdi-pencil-outline' : 'mdi-eye-outline'"
                     @click="openInlineEditor(openFailed.bucket, openFailed.name)">
                {{ openFailed.bucket === 'process' ? '在线编辑' : '在线查看' }}
              </v-btn>
            </div>
          </v-alert>

          <div class="text-caption text-grey mb-1">工作依据（上游交付，点击本机打开查看）</div>
          <v-list density="compact" border rounded class="doc-list mb-3">
            <v-list-item v-for="d in inputDocs" :key="d.name" @click="openLocal('input', d.name)">
              <template #prepend><v-icon size="small" color="info">mdi-tray-arrow-down</v-icon></template>
              <v-list-item-title class="text-body-2">{{ d.name }}</v-list-item-title>
              <template #append><v-icon size="small" color="grey">mdi-open-in-app</v-icon></template>
            </v-list-item>
            <div v-if="inputDocs.length === 0" class="pa-3 text-caption text-grey">暂无上游工作依据</div>
          </v-list>

          <div class="d-flex align-center mb-1">
            <div class="text-caption text-grey">参考文件（多来自外部，点「导入」从本地选择拷贝进来）</div>
            <v-spacer />
            <v-btn size="x-small" color="primary" variant="tonal" prepend-icon="mdi-upload" :loading="importing" @click="triggerImportReference">导入</v-btn>
            <input ref="refFileInput" type="file" class="d-none" @change="onRefFilePicked" />
          </div>
          <v-list density="compact" border rounded class="doc-list mb-3">
            <v-list-item v-for="d in referenceDocs" :key="d.name" @click="openLocal('reference', d.name)">
              <template #prepend><v-icon size="small" color="teal">mdi-file-link-outline</v-icon></template>
              <v-list-item-title class="text-body-2">{{ d.name }}</v-list-item-title>
              <template #append><v-icon size="small" color="grey">mdi-open-in-app</v-icon></template>
            </v-list-item>
            <div v-if="referenceDocs.length === 0" class="pa-3 text-caption text-grey">暂无参考文件，点「导入」添加</div>
          </v-list>

          <div class="text-caption text-grey mb-1">过程文档（点击本机打开编辑）</div>
          <v-list density="compact" border rounded class="doc-list mb-3">
            <v-list-item v-for="d in processDocs" :key="d.name" @click="openLocal('process', d.name)">
              <template #prepend>
                <v-icon size="small" :color="isPlaceholderDoc(d) ? 'grey' : 'success'">{{ isPlaceholderDoc(d) ? 'mdi-file-outline' : 'mdi-file-check-outline' }}</v-icon>
              </template>
              <v-list-item-title class="text-body-2">{{ d.name }}</v-list-item-title>
              <v-list-item-subtitle class="text-caption">{{ isPlaceholderDoc(d) ? '空（待填写）' : '已填写' }}</v-list-item-subtitle>
              <template #append><v-icon size="small" color="grey">mdi-open-in-app</v-icon></template>
            </v-list-item>
            <div v-if="processDocs.length === 0" class="pa-3 text-caption text-grey">暂无过程文档</div>
          </v-list>

          <div class="text-caption text-grey mb-1">结果文件（定稿/产出，点击本机打开查看）</div>
          <v-list density="compact" border rounded class="doc-list">
            <v-list-item v-for="d in outputDocs" :key="d.name" @click="openLocal('output', d.name)">
              <template #prepend><v-icon size="small" color="success">mdi-file-star-outline</v-icon></template>
              <v-list-item-title class="text-body-2">{{ d.name }}</v-list-item-title>
              <template #append><v-icon size="small" color="grey">mdi-open-in-app</v-icon></template>
            </v-list-item>
            <div v-if="outputDocs.length === 0" class="pa-3 text-caption text-grey">暂无结果文件</div>
          </v-list>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="editorDialog = false">关闭</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 在线编辑窗口（二级弹窗，兜底）：本机打不开时点按钮才弹出 -->
    <v-dialog v-model="docDialog" max-width="820" persistent scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">{{ viewingInput ? 'mdi-eye-outline' : 'mdi-pencil-outline' }}</v-icon>
          <template v-if="viewingInput">在线查看（只读）— {{ viewingInput }}</template>
          <template v-else>在线编辑（兜底）— {{ currentDoc }}</template>
        </v-card-title>
        <v-card-subtitle class="text-wrap">本机打不开时的兜底通道；文本类文档可在此{{ viewingInput ? '查看' : '编辑并保存到本环节过程目录' }}。</v-card-subtitle>
        <v-card-text>
          <v-textarea
            v-model="content"
            :loading="editorLoading"
            :readonly="!!viewingInput"
            :disabled="editorLoading"
            variant="outlined" rows="18" hide-details auto-grow
            class="lxs-mono"
            :placeholder="viewingInput ? '' : '在此输入文档内容，保存后存入本环节过程目录'"
          />
          <div v-if="savedPath" class="text-caption text-success mt-1"><v-icon size="x-small">mdi-check</v-icon> 已保存到：{{ savedPath }}</div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="docDialog = false">关闭</v-btn>
          <v-btn v-if="!viewingInput" color="primary" variant="flat" :loading="savingDoc" :disabled="!currentDoc" @click="saveDoc">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 任务级定稿：完成前，谁编辑谁挑——按本任务 output 标识从本任务过程文件挑定稿 -->
    <v-dialog v-model="finalsDialog" max-width="640" persistent>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-file-send-outline</v-icon>
          提交定稿 — {{ finalsTarget?.task_name }}
        </v-card-title>
        <v-card-subtitle class="text-wrap">
          {{ isGenericFinals ? '请从本任务的过程文件中选择一个作为定稿；提交后即完成本任务。' : '本文件任务需产出以下定稿；请为每个定稿标识，从你编辑过的文件（过程文件 / 定稿目录）中各挑一个。提交后即完成本任务。' }}
        </v-card-subtitle>
        <v-card-text>
          <v-alert v-if="finalsFiles.length === 0" type="warning" variant="tonal" density="compact" class="mb-2">
            过程文件都还是空的（或不存在），无法作为定稿。请先「打开文档」编辑并保存后再来；或下方「不留定稿直接完成」。
          </v-alert>

          <!-- 无定稿标识：从过程文件挑一个作定稿 -->
          <div v-if="isGenericFinals && finalsFiles.length > 0">
            <div class="text-body-2 mb-1"><v-icon size="small" color="primary">mdi-file-check-outline</v-icon> 选择定稿文件</div>
            <v-select
              v-model="finalsGenericPick"
              :items="finalsFiles" density="compact" variant="outlined" hide-details
              placeholder="从过程文件中选择一个作为定稿" />
          </div>

          <!-- 有定稿标识：逐个标识挑 -->
          <div v-for="r in finalsRules" :key="r.file_rule_code" class="mb-3">
            <div class="text-body-2 mb-1"><v-icon size="small" color="primary">mdi-file-check-outline</v-icon> {{ r.file_name }}<span class="text-caption text-grey ml-1">({{ r.allowed_file_types || '不限类型' }})</span></div>
            <v-select
              v-model="finalsPick[r.file_rule_code]"
              :items="finalsFiles" density="compact" variant="outlined" hide-details
              placeholder="选择作为该定稿的文件" :disabled="finalsFiles.length === 0" />
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="finalsDialog = false">取消</v-btn>
          <v-btn v-if="finalsFiles.length === 0" variant="text" color="warning" :loading="finalsSubmitting" @click="completeWithoutFinals">不留定稿直接完成</v-btn>
          <v-btn color="success" variant="flat" :loading="finalsSubmitting" :disabled="!canSubmitFinals" @click="submitFinalsAndComplete">提交定稿并完成</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 参考文件导入：归类定级声明 -->
    <v-dialog v-model="refImportDialog" max-width="460" persistent>
      <v-card>
        <v-card-title class="text-subtitle-1">导入参考文件 — 归类定级</v-card-title>
        <v-card-text>
          <div class="text-caption text-grey mb-3">
            文件「{{ pendingRefFile?.name }}」将拷贝到本任务的参考文件目录。请声明其类别与敏感级别——级别决定归档时落入九宫格的哪一格（核心→保密、重要→档案、一般→资料）。
          </div>
          <v-select
            v-model="refForm.category" :items="refCategoryOptions" label="资料类别"
            density="comfortable" variant="outlined" hide-details class="mb-3"
            @update:model-value="onRefCategoryChange" />
          <v-select
            v-model="refForm.sensitivity_level" :items="refLevelOptions" label="敏感级别（可手动调整）"
            density="comfortable" variant="outlined" hide-details />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="importing" @click="refImportDialog = false; pendingRefFile = null">取消</v-btn>
          <v-btn color="primary" variant="elevated" :loading="importing" @click="confirmImportReference">确认导入</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>

<style scoped>
.dot { display: inline-block; width: 9px; height: 9px; border-radius: 50%; }
.dot-todo { background: #2f6feb; } .dot-doing { background: #e8910c; } .dot-done { background: #15a05a; }
.tab-count { margin-left: 7px; background: #f0f2f4; border: 1px solid #e6e8eb; border-radius: 999px; font-size: 12px; color: #646a73; padding: 1px 9px; }
.item-card { border: 1px solid #e6e8eb !important; border-radius: 12px; padding: 12px 13px; margin-bottom: 11px; border-left-width: 3px !important; border-left-style: solid !important; }
.bar-todo { border-left-color: #2f6feb !important; } .bar-doing { border-left-color: #e8910c !important; }
.bar-done { border-left-color: #15a05a !important; }
.item-card.muted { opacity: 0.78; }
.proj { font-size: 12px; color: #646a73; margin-bottom: 3px; }
.proj .code { display: inline-block; background: #eef2f7; color: #1b3a5b; border: 1px solid #d6deea; border-radius: 4px; padding: 0 5px; margin-right: 6px; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 11px; }
.stage { font-size: 14.5px; font-weight: 600; margin-bottom: 10px; line-height: 1.35; }
.acts { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.col-empty { text-align: center; color: #9aa0a6; font-size: 12.5px; padding: 24px 0; }
.empty-all { text-align: center; padding: 32px 0; }
.doc-list { max-height: 200px; overflow-y: auto; }
.lxs-mono :deep(textarea) { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 13px; line-height: 1.6; }
</style>
