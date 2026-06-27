<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { API_BASE } from '@/services/api'

interface InvolvedProject {
  id: number
  project_name: string
  project_code: string | null
  status: string
  owner_name: string
  roles: string[]
}
interface MemberStage { stage_code: string; stage_name: string; status: string }
interface MemberTask { stage_code: string; stage_name: string; task_code: string; task_name: string; status: string }
interface WorkGroup {
  application_id: number
  project_name: string
  project_code: string | null
  status: string
  lead: { username: string; display_name: string; user_unit: string | null; user_department: string | null }
  core_members: Array<{ username: string; display_name: string; stages: MemberStage[] }>
  participants: Array<{ username: string; display_name: string; tasks: MemberTask[] }>
}

const loading = ref(false)
const projects = ref<InvolvedProject[]>([])
const selectedId = ref<number | null>(null)
const wg = ref<WorkGroup | null>(null)
const wgLoading = ref(false)
const snackbar = ref({ show: false, text: '', color: 'success' })

const roleLabel: Record<string, string> = { lead: '组长', core: '核心成员', participant: '参与人' }
function projStatusLabel(s: string): string {
  return ({ approved: '待承接', taken: '已承接', accepted: '已分工', closed: '已结项' } as Record<string, string>)[s] || s
}
function stageStatusColor(s: string): string {
  return ({ pending: 'grey', in_progress: 'primary', completed: 'success' } as Record<string, string>)[s] || 'grey'
}
function stageStatusLabel(s: string): string {
  return ({ pending: '待开始', in_progress: '进行中', completed: '已完成' } as Record<string, string>)[s] || s
}

async function loadProjects() {
  loading.value = true
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/involved`)
    const j = await r.json()
    if (j.success) {
      projects.value = (j.data || []) as InvolvedProject[]
      if (projects.value.length && selectedId.value == null) selectProject(projects.value[0].id)
    } else snackbar.value = { show: true, text: '加载失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载失败：' + (e?.message || String(e)), color: 'error' }
  } finally { loading.value = false }
}

async function selectProject(id: number) {
  selectedId.value = id
  wgLoading.value = true
  wg.value = null
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/work-group?application_id=${id}`)
    const j = await r.json()
    if (j.success) wg.value = j.data as WorkGroup
    else snackbar.value = { show: true, text: '加载工作组失败：' + (j.error || ''), color: 'error' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载工作组失败：' + (e?.message || String(e)), color: 'error' }
  } finally { wgLoading.value = false }
}

onMounted(loadProjects)
</script>

<template>
  <v-card flat>
    <v-card-title class="d-flex align-center">
      <v-icon class="mr-2">mdi-account-group</v-icon>
      工作组
      <v-spacer />
      <v-btn variant="text" prepend-icon="mdi-refresh" :loading="loading" @click="loadProjects">刷新</v-btn>
    </v-card-title>
    <v-card-subtitle>查看你参与的项目的工作组构成：组长（项目负责人）、核心成员（环节责任人）、参与人员（文件任务参与人）。</v-card-subtitle>

    <v-card-text>
      <v-row no-gutters>
        <!-- 左：参与项目列表 -->
        <v-col cols="12" md="4" class="pr-md-3">
          <v-list density="compact" border rounded class="proj-list">
            <v-list-item v-for="p in projects" :key="p.id" :active="selectedId === p.id" @click="selectProject(p.id)">
              <v-list-item-title class="text-body-2">{{ p.project_name }}</v-list-item-title>
              <v-list-item-subtitle class="text-caption">
                <span v-if="p.project_code" class="code">{{ p.project_code }}</span>
                <v-chip size="x-small" variant="tonal" class="ml-1">{{ projStatusLabel(p.status) }}</v-chip>
              </v-list-item-subtitle>
              <template #append>
                <v-chip v-for="role in p.roles" :key="role" size="x-small" color="primary" variant="tonal" class="ml-1">{{ roleLabel[role] || role }}</v-chip>
              </template>
            </v-list-item>
            <div v-if="!loading && projects.length === 0" class="pa-4 text-caption text-grey text-center">暂无参与的项目</div>
          </v-list>
        </v-col>

        <!-- 右：工作组详情 -->
        <v-col cols="12" md="8">
          <v-progress-linear v-if="wgLoading" indeterminate color="primary" class="mb-3" />
          <template v-else-if="wg">
            <div class="text-subtitle-1 font-weight-medium mb-1">{{ wg.project_name }}
              <span v-if="wg.project_code" class="code ml-1">{{ wg.project_code }}</span>
              <v-chip size="x-small" variant="tonal" class="ml-1">{{ projStatusLabel(wg.status) }}</v-chip>
            </div>

            <!-- 组长 -->
            <v-card variant="tonal" color="indigo" class="mb-3">
              <v-card-text class="py-3">
                <div class="role-head"><v-icon size="18" class="mr-1">mdi-account-star</v-icon>组长（项目负责人）</div>
                <div class="d-flex align-center mt-1">
                  <v-avatar size="28" color="indigo" class="mr-2"><span class="text-caption">{{ (wg.lead.display_name || '?').slice(0,1) }}</span></v-avatar>
                  <div>
                    <span class="member-name">{{ wg.lead.display_name }}</span>
                    <span class="text-caption text-grey ml-1">({{ wg.lead.username }})</span>
                    <span v-if="wg.lead.user_department" class="text-caption text-grey ml-2">{{ wg.lead.user_unit }} · {{ wg.lead.user_department }}</span>
                  </div>
                </div>
              </v-card-text>
            </v-card>

            <!-- 核心成员（环节责任人） -->
            <v-card variant="outlined" class="mb-3">
              <v-card-text class="py-3">
                <div class="role-head"><v-icon size="18" class="mr-1" color="primary">mdi-account-tie</v-icon>核心成员（环节责任人）· {{ wg.core_members.length }} 人</div>
                <div v-if="wg.core_members.length === 0" class="text-caption text-grey mt-1">尚未分工</div>
                <div v-for="m in wg.core_members" :key="m.username" class="member-row">
                  <span class="member-name">{{ m.display_name }}</span>
                  <span class="text-caption text-grey ml-1">({{ m.username }})</span>
                  <div class="mt-1">
                    <v-chip v-for="s in m.stages" :key="s.stage_code" size="x-small" :color="stageStatusColor(s.status)" variant="tonal" class="mr-1 mb-1">
                      {{ s.stage_name }} · {{ stageStatusLabel(s.status) }}
                    </v-chip>
                  </div>
                </div>
              </v-card-text>
            </v-card>

            <!-- 参与人员（文件任务参与人） -->
            <v-card variant="outlined">
              <v-card-text class="py-3">
                <div class="role-head"><v-icon size="18" class="mr-1" color="success">mdi-account-multiple</v-icon>参与人员（文件任务参与人）· {{ wg.participants.length }} 人</div>
                <div v-if="wg.participants.length === 0" class="text-caption text-grey mt-1">尚未指派文件任务</div>
                <div v-for="m in wg.participants" :key="m.username" class="member-row">
                  <span class="member-name">{{ m.display_name }}</span>
                  <span class="text-caption text-grey ml-1">({{ m.username }})</span>
                  <div class="mt-1">
                    <v-chip v-for="t in m.tasks" :key="t.task_code" size="x-small" :color="stageStatusColor(t.status)" variant="tonal" class="mr-1 mb-1">
                      {{ t.stage_name }} / {{ t.task_name }} · {{ stageStatusLabel(t.status) }}
                    </v-chip>
                  </div>
                </div>
              </v-card-text>
            </v-card>
          </template>
          <div v-else class="text-center py-10 text-grey">
            <v-icon size="56" color="grey-lighten-1">mdi-account-group-outline</v-icon>
            <div class="mt-3">选择左侧项目查看工作组</div>
          </div>
        </v-col>
      </v-row>
    </v-card-text>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">{{ snackbar.text }}</v-snackbar>
  </v-card>
</template>

<style scoped>
.proj-list { max-height: 70vh; overflow-y: auto; }
.code { display: inline-block; background: #eef2f7; color: #1b3a5b; border: 1px solid #d6deea; border-radius: 4px; padding: 0 5px; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 11px; }
.role-head { font-size: 13.5px; font-weight: 600; display: flex; align-items: center; }
.member-row { padding: 8px 0; border-top: 1px solid rgba(0,0,0,.06); margin-top: 8px; }
.member-name { font-size: 14px; font-weight: 600; }
</style>
