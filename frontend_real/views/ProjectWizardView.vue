<template>
  <div>
    <v-card flat>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-folder-plus</v-icon>
        新建项目立项
        <v-spacer />
        <v-btn variant="text" @click="$router.push('/projects')">
          <v-icon>mdi-arrow-left</v-icon> 返回列表
        </v-btn>
      </v-card-title>

      <v-card-text>
        <v-stepper v-model="step" :items="stepItems" hide-actions>
          <template #item.1>
            <v-card-text>
              <p class="text-body-2 text-medium-emphasis mb-3">选择已发布的数据业务模版作为本项目的卷宗结构来源。</p>
              <div class="d-flex gap-3 mb-3 align-center">
                <v-select
                  v-model="form.template_id"
                  :items="templateOptions"
                  label="数据业务模版 *"
                  variant="outlined"
                  density="compact"
                  hide-details
                  style="flex: 1"
                  @update:model-value="onTemplateSelect"
                />
                <v-btn variant="tonal" :loading="syncing" @click="onSyncFromManage" title="进入向导时已自动同步；如 manage 上模版有更新，点击此处重新拉取">
                  <v-icon>mdi-refresh</v-icon> 重新同步
                </v-btn>
              </div>

              <v-card v-if="selectedTemplate" variant="tonal" class="mt-4">
                <v-card-title class="text-subtitle-1">
                  <v-icon class="mr-2">mdi-information</v-icon>
                  {{ selectedTemplate.template_name }}
                  <v-chip class="ml-2" size="small">{{ selectedTemplate.template_version }}</v-chip>
                  <v-chip class="ml-1" size="small" :color="sensColor(selectedTemplate.project_sensitivity_level)" variant="tonal">
                    建议等级：{{ sensLabel(templateSensitivity) }}
                  </v-chip>
                </v-card-title>
                <v-card-text>
                  <div class="text-caption text-medium-emphasis">{{ selectedTemplate.scenario }}</div>
                  <div class="mt-2 text-body-2">{{ selectedTemplate.description }}</div>
                  <div class="mt-3" v-if="fullStages.length">
                    <div class="text-caption mb-1">将自动生成 {{ fullStages.length }} 个工作环节，{{ totalRules }} 条文件版本规则：</div>
                    <v-chip v-for="s in fullStages" :key="s.stage_code" size="x-small" class="mr-1 mb-1">
                      {{ s.stage_code }} {{ s.stage_name }}
                    </v-chip>
                  </div>
                </v-card-text>
              </v-card>
            </v-card-text>
          </template>

          <template #item.2>
            <v-card-text>
              <v-form v-model="step2Valid">
                <v-row>
                  <v-col cols="12" md="6">
                    <v-text-field
                      v-model="form.project_name"
                      label="项目名称 *"
                      placeholder="如《明朝那些事儿》印刷计划"
                      :rules="[required]"
                      variant="outlined"
                      density="compact"
                    />
                  </v-col>
                  <v-col cols="12" md="6">
                    <v-text-field
                      label="项目编码"
                      model-value="（提交后由系统自动生成）"
                      disabled
                      variant="outlined"
                      density="compact"
                      persistent-hint
                      hint="按规则生成：PROJ-{年份}-{流水号}"
                    />
                  </v-col>
                  <v-col cols="12">
                    <v-textarea
                      v-model="form.task_summary"
                      label="任务概述"
                      :rows="2"
                      variant="outlined"
                      density="compact"
                    />
                  </v-col>
                  <v-col cols="12" md="6">
                    <v-text-field
                      v-model="form.approval_basis"
                      label="审批立项依据"
                      placeholder="合同号 / 任务书 / 审批单号"
                      variant="outlined"
                      density="compact"
                    />
                  </v-col>
                  <v-col cols="12" md="3">
                    <v-text-field
                      v-model="form.planned_start_date"
                      type="date"
                      label="计划开始"
                      variant="outlined"
                      density="compact"
                    />
                  </v-col>
                  <v-col cols="12" md="3">
                    <v-text-field
                      v-model="form.planned_end_date"
                      type="date"
                      label="计划结束"
                      variant="outlined"
                      density="compact"
                    />
                  </v-col>
                </v-row>
              </v-form>
            </v-card-text>
          </template>

          <template #item.3>
            <v-card-text>
              <p class="text-body-2 text-medium-emphasis mb-3">三主体决定数据资产责任归属，对应底账中的归属/保管/安全主体。</p>
              <v-row>
                <v-col cols="12" md="4">
                  <v-select
                    v-model="form.owner_subject_id"
                    :items="subjectOptions"
                    label="归属主体 *"
                    hint="数据归谁所有"
                    persistent-hint
                    :rules="[requiredNum]"
                    variant="outlined"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12" md="4">
                  <v-select
                    v-model="form.custodian_subject_id"
                    :items="subjectOptions"
                    label="保管主体 *"
                    hint="实际保管使用"
                    persistent-hint
                    :rules="[requiredNum]"
                    variant="outlined"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12" md="4">
                  <v-select
                    v-model="form.security_subject_id"
                    :items="subjectOptions"
                    label="安全主体 *"
                    hint="安全合规责任"
                    persistent-hint
                    :rules="[requiredNum]"
                    variant="outlined"
                    density="compact"
                  />
                </v-col>
              </v-row>
              <div class="text-caption text-medium-emphasis">
                没找到合适主体？请先在 manage 端维护，回到本页会自动拉取最新主体。
              </div>
            </v-card-text>
          </template>

          <template #item.4>
            <v-card-text>
              <p class="text-body-2 text-medium-emphasis mb-3">敏感等级用"就高不就低"原则与模版预设比对，最终自动取较高等级。</p>
              <v-row>
                <v-col cols="12" md="6">
                  <v-select
                    v-model="form.sensitivity_level"
                    :items="sensOptions"
                    label="项目敏感等级 *"
                    :hint="`模版建议：${sensLabel(templateSensitivity)}`"
                    variant="outlined"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12" md="6">
                  <v-select
                    v-model="form.management_mode"
                    :items="modeOptions"
                    label="管理模式"
                    variant="outlined"
                    density="compact"
                  />
                </v-col>
              </v-row>

              <v-divider class="my-4" />

              <div class="d-flex align-center mb-2">
                <h3 class="text-h6">项目成员与权限</h3>
                <v-spacer />
                <v-btn size="small" prepend-icon="mdi-plus" @click="addMember">添加成员</v-btn>
              </div>

              <!-- V2-3: 立项人由后端自动登记为项目负责人 -->
              <v-alert v-if="currentUser" type="success" variant="tonal" density="compact" class="mb-3">
                <div class="d-flex align-center">
                  <v-icon class="mr-2">mdi-account-star</v-icon>
                  <div>
                    <strong>{{ currentUser.user_name }}</strong>（{{ currentUser.department }}）将自动登记为项目负责人，享有
                    查看、写入、领取、提交、共享、归档、结项全权限，无需在下方重复添加。
                  </div>
                </div>
              </v-alert>
              <v-alert v-else type="warning" variant="tonal" density="compact" class="mb-3">
                未检测到当前登录用户信息，请先在右上角填写机主信息后再立项，否则将无法自动分配项目负责人。
              </v-alert>

              <p class="text-caption text-medium-emphasis mb-2">下方可补充其他角色成员（如收稿员、排版员等）；立项后再补充也可以。</p>

              <v-card v-for="(m, i) in form.members" :key="i" variant="outlined" class="mb-2 pa-3">
                <v-row dense>
                  <v-col cols="12" md="3">
                    <v-select
                      v-model="m.user_id"
                      :items="userOptions"
                      label="成员 *"
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12" md="2">
                    <v-combobox
                      v-model="m.role_code"
                      :items="roleOptions"
                      label="角色 *"
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12" md="3">
                    <v-select
                      v-model="m.stage_codes"
                      :items="stageOptions"
                      label="可参与环节"
                      multiple
                      chips
                      closable-chips
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12" md="3">
                    <v-select
                      v-model="m.permission_actions"
                      :items="permOptions"
                      label="权限 *"
                      multiple
                      chips
                      closable-chips
                      density="compact"
                      hide-details
                    />
                  </v-col>
                  <v-col cols="12" md="1" class="d-flex align-center">
                    <v-btn icon="mdi-delete" size="small" variant="text" color="error" @click="form.members.splice(i, 1)" />
                  </v-col>
                </v-row>
              </v-card>

            </v-card-text>
          </template>

          <template #item.5>
            <v-card-text>
              <h3 class="text-h6 mb-3">立项确认</h3>
              <v-list density="compact">
                <v-list-item title="模版" :subtitle="`${selectedTemplate?.template_code} ${selectedTemplate?.template_version} - ${selectedTemplate?.template_name}`" />
                <v-list-item title="项目名称" :subtitle="form.project_name" />
                <v-list-item title="项目编码" subtitle="（提交后由系统按 PROJ-{年份}-{流水号} 自动生成）" />
                <v-list-item title="任务概述" :subtitle="form.task_summary || '-'" />
                <v-list-item title="审批依据" :subtitle="form.approval_basis || '-'" />
                <v-list-item title="计划时限" :subtitle="`${form.planned_start_date || '-'} ~ ${form.planned_end_date || '-'}`" />
                <v-list-item title="敏感等级（最终）" :subtitle="sensLabel(finalSensitivity)" />
                <v-list-item title="管理模式" :subtitle="modeLabel(form.management_mode)" />
                <v-list-item title="归属主体" :subtitle="subjectLabel(form.owner_subject_id)" />
                <v-list-item title="保管主体" :subtitle="subjectLabel(form.custodian_subject_id)" />
                <v-list-item title="安全主体" :subtitle="subjectLabel(form.security_subject_id)" />
                <v-list-item title="项目成员" :subtitle="`${form.members.length} 人`" />
              </v-list>

              <v-alert type="info" variant="tonal" class="mt-3">
                确认后将自动生成项目编码、{{ fullStages.length }} 个工作环节、{{ totalRules }} 条文件版本计划，并在本机创建项目目录树。
              </v-alert>
            </v-card-text>
          </template>
        </v-stepper>

        <v-divider class="my-3" />
        <div class="d-flex justify-space-between px-4 pb-4">
          <v-btn variant="text" :disabled="step <= 1" @click="step--">
            <v-icon>mdi-arrow-left</v-icon> 上一步
          </v-btn>
          <div>
            <v-btn v-if="step < 5" color="primary" :disabled="!canNext" @click="step++">
              下一步 <v-icon>mdi-arrow-right</v-icon>
            </v-btn>
            <v-btn
              v-if="step === 5"
              variant="outlined"
              class="mr-2"
              :loading="submitting && pendingMode === 'draft'"
              :disabled="!currentUser || submitting"
              @click="submit(false)"
            >
              <v-icon>mdi-content-save-outline</v-icon> 暂存草稿
            </v-btn>
            <v-btn
              v-if="step === 5"
              color="success"
              :loading="submitting && pendingMode === 'activate'"
              :disabled="!currentUser || submitting"
              @click="submit(true)"
            >
              <v-icon>mdi-check</v-icon> 创建并激活项目
            </v-btn>
          </div>
        </div>
      </v-card-text>
    </v-card>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="5000">
      {{ snackbar.text }}
    </v-snackbar>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  templatesApi,
  subjectsApi,
  usersApi,
  projectsApi,
  type DataTemplate,
  type FullTemplate,
  type ProjectUser,
  type Subject,
} from '@/services/projectsApi'
import { userInfoManager, type UserInfo } from '@/services/UserInfoManager'

const router = useRouter()

const step = ref(1)
const stepItems = [
  { title: '选择模版', value: 1 },
  { title: '项目信息', value: 2 },
  { title: '三主体', value: 3 },
  { title: '安全与成员', value: 4 },
  { title: '确认创建', value: 5 },
]

const templates = ref<DataTemplate[]>([])
const subjects = ref<Subject[]>([])
const users = ref<ProjectUser[]>([])
const fullTemplate = ref<FullTemplate | null>(null)
const fullStages = computed(() => fullTemplate.value?.stages || [])
const totalRules = computed(() =>
  fullStages.value.reduce((acc, s) => acc + (s.file_rules?.length || 0), 0)
)
const sensRank: Record<string, number> = { general: 1, important: 2, core_secret: 3 }
const selectedTemplate = computed(() => templates.value.find((t) => t.id === form.value.template_id) || null)
const templateSensitivity = computed(() =>
  normalizeSensitivity(fullTemplate.value?.template?.project_sensitivity_level || selectedTemplate.value?.project_sensitivity_level || 'general')
)

const syncing = ref(false)
const submitting = ref(false)
const snackbar = ref({ show: false, text: '', color: 'success' })
const step2Valid = ref(false)
const currentUser = ref<UserInfo | null>(null)

const form = ref({
  template_id: 0,
  project_name: '',
  task_summary: '',
  approval_basis: '',
  planned_start_date: '',
  planned_end_date: '',
  sensitivity_level: 'general',
  management_mode: 'independent',
  owner_subject_id: 0,
  custodian_subject_id: 0,
  security_subject_id: 0,
  // V2-3: 立项人由后端 currentUserID 自动注册为项目负责人（带 close 等全权限），
  // 这里默认不预置成员；用户可按需补充其他角色（如收稿员、排版员）。
  members: [] as Array<{ user_id: number; role_code: string; stage_codes: string[]; permission_actions: string[] }>,
})

const templateOptions = computed(() =>
  templates.value
    .filter((t) => t.status === 'active')
    .map((t) => ({ title: `${t.template_code} ${t.template_version} - ${t.template_name}`, value: t.id }))
)

const subjectOptions = computed(() =>
  subjects.value.map((s) => ({ title: `${s.code} ${s.name} (${typeLabel(s.type)})`, value: s.id }))
)

const userOptions = computed(() =>
  users.value.map((u) => ({ title: `${u.display_name}（${u.department || u.company_name || u.username}）`, value: u.id }))
)

const stageOptions = computed(() =>
  fullStages.value.map((s) => ({ title: `${s.stage_code} ${s.stage_name}`, value: s.stage_code }))
)

const sensOptions = [
  { title: '一般', value: 'general' },
  { title: '重要', value: 'important' },
  { title: '核心(涉密)', value: 'core_secret' },
]

const modeOptions = [
  { title: '独立管理（默认）', value: 'independent' },
  { title: '共享', value: 'shared' },
  { title: '混合', value: 'mixed' },
]

const fallbackRoleNames = ['项目经理', '收稿员', '排版员', '审校员', '设计师', '印刷工', '装订工', '交付员', '归档员']

const roleOptions = computed(() => {
  const templateRoles = uniqueRoleNames(fullStages.value.flatMap((stage) => parseRoleCodes(stage.default_role_codes)))
  const names = templateRoles.length > 0 ? templateRoles : fallbackRoleNames
  return names.map((name) => ({ title: name, value: name }))
})

const permOptions = [
  { title: '查看', value: 'read' },
  { title: '写入', value: 'write' },
  { title: '领取', value: 'receive' },
  { title: '上传', value: 'upload' },
  { title: '提交', value: 'submit' },
  { title: '共享', value: 'share' },
  { title: '归档', value: 'archive' },
  { title: '结项', value: 'close' },
  { title: '销毁', value: 'destroy' },
]

function required(v: string) {
  return !!v?.trim() || '此项必填'
}
function requiredNum(v: number) {
  return v > 0 || '请选择'
}
function typeLabel(t: string) {
  return ({ person: '人员', department: '部门', organization: '组织' } as Record<string, string>)[t] || t
}
function normalizeSensitivity(s: string) {
  return sensRank[s] ? s : 'general'
}
function sensLabel(s: string) {
  return ({ general: '一般', important: '重要', core_secret: '核心(涉密)' } as Record<string, string>)[normalizeSensitivity(s)] || '一般'
}
function sensColor(s: string) {
  return ({ general: 'default', important: 'warning', core_secret: 'error' } as Record<string, string>)[normalizeSensitivity(s)] || 'default'
}
function modeLabel(s: string) {
  return ({ independent: '独立管理', shared: '共享', mixed: '混合' } as Record<string, string>)[s] || s
}
function subjectLabel(id: number) {
  const s = subjects.value.find((x) => x.id === id)
  return s ? `${s.code} ${s.name}` : '-'
}
function uniqueRoleNames(names: string[]) {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of names) {
    const name = raw.trim()
    if (!name || seen.has(name)) continue
    seen.add(name)
    out.push(name)
  }
  return out
}
function parseRoleCodes(raw: string | null | undefined): string[] {
  const value = (raw || '').trim()
  if (!value) return []
  try {
    const parsed = JSON.parse(value)
    if (Array.isArray(parsed)) return parsed.map((item) => String(item))
    if (typeof parsed === 'string') return [parsed]
  } catch {
    // 非 JSON 时按常见分隔符解析，兼容后台手填的逗号/顿号格式。
  }
  return value.split(/[,\n，、;；]+/).map((item) => item.trim()).filter(Boolean)
}

const finalSensitivity = computed(() => {
  const userLevel = normalizeSensitivity(form.value.sensitivity_level)
  const tplLevel = templateSensitivity.value
  return sensRank[userLevel] >= sensRank[tplLevel] ? userLevel : tplLevel
})

// V2-3: 立项人由后端自动注册为项目负责人（带 close 权限），前端不再要求"成员中必须有 close"
const canNext = computed(() => {
  if (step.value === 1) return form.value.template_id > 0
  if (step.value === 2) return step2Valid.value
  if (step.value === 3)
    return form.value.owner_subject_id > 0 && form.value.custodian_subject_id > 0 && form.value.security_subject_id > 0
  if (step.value === 4) return !!currentUser.value && form.value.members.every((m) => m.user_id > 0 && m.role_code)
  return true
})

async function loadTemplates() {
  try {
    templates.value = await templatesApi.list('active')
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载模版失败：' + e.message, color: 'error' }
  }
}

async function loadSubjects() {
  try {
    subjects.value = await subjectsApi.list()
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载主体失败：' + e.message, color: 'error' }
  }
}

async function loadUsers() {
  try {
    users.value = await usersApi.list()
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载用户失败：' + e.message, color: 'error' }
  }
}

async function onTemplateSelect() {
  if (!form.value.template_id) {
    fullTemplate.value = null
    return
  }
  try {
    fullTemplate.value = await templatesApi.get(form.value.template_id)
    // 用模版预设等级初始化敏感等级
    if (fullTemplate.value?.template) {
      form.value.sensitivity_level = normalizeSensitivity(fullTemplate.value.template.project_sensitivity_level)
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载模版结构失败：' + e.message, color: 'error' }
  }
}

async function onSyncFromManage() {
  // 用户手动点"重新同步"：不静默错误，弹 toast
  // V3-A 修复：调 syncAll 拉 manage 所有 active 模版，而不是写死的 TPL-PRINT-BOOK V2.1
  syncing.value = true
  try {
    const r = await templatesApi.syncAll()
    await loadTemplates()
    let msg = `同步完成：远端 ${r.total_remote} 条，本地写入 ${r.synced} 条`
    if (r.errors && r.errors.length) {
      msg += `；${r.errors.length} 条失败`
      console.warn('[wizard] sync-all 部分失败:', r.errors)
    }
    snackbar.value = { show: true, text: msg, color: r.errors.length ? 'warning' : 'success' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '同步失败：' + e.message + '（请确认 Settings 中已配置 manage 服务地址）', color: 'error' }
  } finally {
    syncing.value = false
  }
}

// 自动同步：进入向导时尝试从 manage 拉最新模版
//
// 设计：
//   - 静默失败：网络不通时不弹错，只在控制台日志
//   - 仍调 loadTemplates() 加载本地缓存，让用户能用已有模版继续工作
//   - 用户可点"重新同步"按钮显式触发，那次失败会弹 toast
async function autoSyncOnMount() {
  syncing.value = true
  try {
    // V3-A 修复：同步所有 active 模版而不仅是写死的 TPL-PRINT-BOOK V2.1
    await templatesApi.syncAll()
  } catch (e: any) {
    // 静默：仅控制台日志，UI 不打扰；用户仍可用本地缓存
    console.warn('[wizard] 自动同步模版失败，回退到本地缓存：', e?.message || e)
  } finally {
    syncing.value = false
  }
  await loadTemplates()
}

async function addMember() {
  await loadUsers()
  form.value.members.push({ user_id: 0, role_code: '', stage_codes: [], permission_actions: ['read'] })
}

async function loadCurrentUser() {
  try {
    currentUser.value = await userInfoManager.getUserInfo()
  } catch (e) {
    currentUser.value = null
  }
}

const pendingMode = ref<'draft' | 'activate' | null>(null)

async function submit(activate: boolean) {
  if (!selectedTemplate.value) return
  submitting.value = true
  pendingMode.value = activate ? 'activate' : 'draft'
  try {
    const result = await projectsApi.create({
      template_code: selectedTemplate.value.template_code,
      template_version: selectedTemplate.value.template_version,
      project_name: form.value.project_name,
      task_summary: form.value.task_summary || undefined,
      approval_basis: form.value.approval_basis || undefined,
      planned_start_date: form.value.planned_start_date || undefined,
      planned_end_date: form.value.planned_end_date || undefined,
      sensitivity_level: form.value.sensitivity_level,
      management_mode: form.value.management_mode,
      owner_subject_id: form.value.owner_subject_id,
      custodian_subject_id: form.value.custodian_subject_id,
      security_subject_id: form.value.security_subject_id,
      members: form.value.members,
      activate,
    })
    const label = activate ? '创建并激活成功' : '已暂存为草稿，可在项目列表中点"激活"开工'
    snackbar.value = { show: true, text: `${label}：${result.project.project_code}`, color: 'success' }
    setTimeout(() => router.push('/projects'), 1500)
  } catch (e: any) {
    snackbar.value = { show: true, text: '创建失败：' + e.message, color: 'error' }
  } finally {
    submitting.value = false
    pendingMode.value = null
  }
}

onMounted(async () => {
  // 自动同步模版（静默失败回退本地）+ 加载主体/用户 + 加载当前登录用户（用于 V2-3 自动登记项目负责人）
  await Promise.all([autoSyncOnMount(), loadSubjects(), loadUsers(), loadCurrentUser()])
})

watch(step, async (currentStep, previousStep) => {
  if (currentStep === previousStep) return
  if (currentStep === 3) {
    await loadSubjects()
  }
  if (currentStep === 4) {
    await Promise.all([loadSubjects(), loadUsers()])
  }
})
</script>
