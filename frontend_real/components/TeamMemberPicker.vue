<script setup lang="ts">
import { ref, computed } from 'vue'

// 组建团队双栏选人体：左=候选人（全体注册用户，可搜索加入），右=当前团队全貌（可移除）。
// 每行展示 姓名 / username / 单位 / 部门 / 角色。v-model 为已选 username 数组。
interface PickUser {
  username: string
  display_name: string
  user_unit?: string | null
  user_department?: string | null
  role?: string | null
}

const props = defineProps<{
  users: PickUser[]
  modelValue: string[]
  // 项目/环节角色标签：username → 已本地化的角色文案数组（兜底用，优先级最低）
  roleTags?: Record<string, string[]>
  // 锁定成员（如项目负责人/环节负责人）：始终在队、不可移除
  lockedMembers?: string[]
  // 本次拉人添加的目标角色文案（如「核心成员」/「项目参与成员」）：候选区标题 + 非负责人成员的标签
  addLabel?: string
  // 负责人 username 与其角色文案（如「项目负责人」/「环节负责人」）：该成员显示此标签
  leadUser?: string
  leadLabel?: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: string[]): void }>()

function isLocked(username: string): boolean {
  return (props.lockedMembers || []).includes(username)
}
// 标签按位置实时推导（不依赖后端派生角色，新拉入的人也立即有标签）：
// 负责人 → leadLabel；其余团队成员 → addLabel（核心成员 / 项目参与成员）；都没配则回退 roleTags。
function tagsFor(username: string): string[] {
  if (props.leadUser && username === props.leadUser) {
    return props.leadLabel ? [props.leadLabel] : ((props.roleTags || {})[username] || [])
  }
  if (props.addLabel) return [props.addLabel]
  return (props.roleTags || {})[username] || []
}

const candSearch = ref('')
const teamSearch = ref('')

// 仅对管理员类角色展示标签；「普通用户」不再展示（无信息量、徒增噪声）。
const roleLabel: Record<string, string> = {
  system_admin: '系统管理员',
  unit_admin: '单位管理员',
}
function rl(role: string | null | undefined): string {
  if (!role) return ''
  return roleLabel[role] || ''
}

function inTeam(username: string): boolean {
  return props.modelValue.includes(username)
}
function match(u: PickUser, q: string): boolean {
  if (!q.trim()) return true
  const k = (u.display_name + u.username + (u.user_unit || '') + (u.user_department || '')).toLowerCase()
  return k.includes(q.trim().toLowerCase())
}

const candidates = computed(() => props.users.filter(u => match(u, candSearch.value)))
// 「当前团队」以 modelValue 为准（含后端合成的负责人）：能在 users 里找到则用完整资料，
// 找不到（如负责人不在候选名单/未加载）也回退用 username 展示，保证锁定成员始终可见。
const teamUsers = computed(() => {
  const byName = new Map(props.users.map(u => [u.username, u]))
  return props.modelValue
    .map(username => byName.get(username) || { username, display_name: username })
    .filter(u => match(u, teamSearch.value))
})

function add(username: string) {
  if (inTeam(username)) return
  emit('update:modelValue', [...props.modelValue, username])
}
function remove(username: string) {
  emit('update:modelValue', props.modelValue.filter(u => u !== username))
}
</script>

<template>
  <div class="tmp-grid">
    <!-- 左：候选人 -->
    <div class="tmp-col">
      <div class="tmp-head">
        <span class="t">{{ addLabel ? `候选人 · 添加为${addLabel}` : '候选人（全体注册用户）' }}</span>
        <span class="n">{{ users.length }} 人</span>
      </div>
      <v-text-field
        v-model="candSearch" density="compact" variant="outlined" hide-details
        placeholder="搜索 姓名 / username / 单位 / 部门" prepend-inner-icon="mdi-magnify" class="mb-2"
      />
      <div class="tmp-list">
        <div v-for="u in candidates" :key="u.username" class="tmp-row" :class="{ disabled: inTeam(u.username) }">
          <v-avatar size="30" color="indigo"><span class="text-caption">{{ (u.display_name || '?').slice(0, 1) }}</span></v-avatar>
          <div class="tmp-who">
            <div class="nm">{{ u.display_name }}<small>{{ u.username }}</small></div>
            <div class="meta">
              <v-chip v-if="u.user_unit" size="x-small" color="blue" variant="tonal">{{ u.user_unit }}</v-chip>
              <v-chip v-if="u.user_department" size="x-small" variant="tonal">{{ u.user_department }}</v-chip>
              <v-chip v-if="rl(u.role)" size="x-small" color="deep-purple" variant="tonal">{{ rl(u.role) }}</v-chip>
            </div>
          </div>
          <v-chip v-if="inTeam(u.username)" size="x-small" color="success" variant="tonal">已在队</v-chip>
          <v-btn v-else size="x-small" color="indigo" variant="tonal" @click="add(u.username)">+ 入队</v-btn>
        </div>
        <div v-if="candidates.length === 0" class="tmp-empty">无匹配用户</div>
      </div>
    </div>

    <!-- 右：当前团队 -->
    <div class="tmp-col">
      <div class="tmp-head">
        <span class="t">当前团队</span>
        <span class="n">{{ modelValue.length }} 人</span>
      </div>
      <v-text-field
        v-model="teamSearch" density="compact" variant="outlined" hide-details
        placeholder="在团队内查找" prepend-inner-icon="mdi-magnify" class="mb-2"
      />
      <div class="tmp-list">
        <div v-for="u in teamUsers" :key="u.username" class="tmp-row">
          <v-avatar size="30" color="deep-purple"><span class="text-caption">{{ (u.display_name || '?').slice(0, 1) }}</span></v-avatar>
          <div class="tmp-who">
            <div class="nm">{{ u.display_name }}<small>{{ u.username }}</small></div>
            <div class="meta">
              <v-chip v-for="t in tagsFor(u.username)" :key="t" size="x-small" color="primary" variant="flat">{{ t }}</v-chip>
              <v-chip v-if="u.user_unit" size="x-small" color="blue" variant="tonal">{{ u.user_unit }}</v-chip>
              <v-chip v-if="u.user_department" size="x-small" variant="tonal">{{ u.user_department }}</v-chip>
              <v-chip v-if="rl(u.role)" size="x-small" color="deep-purple" variant="tonal">{{ rl(u.role) }}</v-chip>
            </div>
          </div>
          <v-chip v-if="isLocked(u.username)" size="x-small" color="primary" variant="tonal">必选</v-chip>
          <v-btn v-else size="x-small" color="error" variant="text" @click="remove(u.username)">移除</v-btn>
        </div>
        <div v-if="teamUsers.length === 0" class="tmp-empty">团队还没有成员，从左侧添加</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tmp-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.tmp-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.tmp-head .t { font-weight: 600; font-size: 13.5px; }
.tmp-head .n { font-size: 12px; color: rgba(0,0,0,.55); }
.tmp-list { border: 1px solid rgba(0,0,0,.12); border-radius: 10px; max-height: 360px; overflow-y: auto; }
.tmp-row { display: flex; align-items: center; gap: 10px; padding: 9px 12px; border-bottom: 1px solid rgba(0,0,0,.05); }
.tmp-row:last-child { border-bottom: none; }
.tmp-row.disabled { opacity: .45; }
.tmp-who { flex: 1; min-width: 0; }
.tmp-who .nm { font-weight: 600; font-size: 13.5px; }
.tmp-who .nm small { font-weight: 400; color: rgba(0,0,0,.5); margin-left: 6px; }
.tmp-who .meta { margin-top: 3px; display: flex; gap: 5px; flex-wrap: wrap; }
.tmp-empty { padding: 36px 0; text-align: center; color: rgba(0,0,0,.4); font-size: 13px; }
</style>
