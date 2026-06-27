<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FamilyMemberDetail } from '../services/api'

type Policy = 'same_content_only' | 'all' | 'none'

const props = defineProps<{
  modelValue: boolean
  selectedPrimaries: FamilyMemberDetail[]
  familyMap: Record<string, FamilyMemberDetail[]>
  claimStatus: number
  defaultPolicy?: Policy
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm', payload: { ids: number[]; skipNextTime: boolean }): void
}>()

const globalPolicy = ref<Policy>(props.defaultPolicy || 'same_content_only')
const skipNextTime = ref(false)
const rowPolicies = ref<Record<string, Policy>>({})   // content_sign → user-set policy
const customizedRows = ref<Set<string>>(new Set())
const expandedRows = ref<Set<string>>(new Set())
const drawerForRow = ref<string | null>(null)

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      globalPolicy.value = props.defaultPolicy || 'same_content_only'
      rowPolicies.value = {}
      customizedRows.value = new Set()
      expandedRows.value = new Set()
      drawerForRow.value = null
      skipNextTime.value = false
    }
  },
  { immediate: true },
)

const setRowPolicy = (cs: string, p: Policy) => {
  rowPolicies.value = { ...rowPolicies.value, [cs]: p }
  customizedRows.value = new Set([...customizedRows.value, cs])
}

const effectivePolicy = (cs: string): Policy =>
  customizedRows.value.has(cs) ? (rowPolicies.value[cs] ?? globalPolicy.value) : globalPolicy.value

const isAlreadyClaimed = (m: FamilyMemberDetail) => (m.claim_status ?? 0) !== 0

/** All members in a family that are NOT the primary itself */
const nonPrimaryMembers = (cs: string, primary: FamilyMemberDetail): FamilyMemberDetail[] => {
  const members = props.familyMap[cs] || []
  return members.filter(m => m.data_resources_id !== primary.data_resources_id)
}

const familyMemberCount = (cs: string, primary: FamilyMemberDetail): number =>
  nonPrimaryMembers(cs, primary).length

const sameCountForRow = (cs: string, primary: FamilyMemberDetail): number =>
  nonPrimaryMembers(cs, primary).filter(m => m.family_relation === 'same_content' && !isAlreadyClaimed(m)).length

const allCountForRow = (cs: string, primary: FamilyMemberDetail): number =>
  nonPrimaryMembers(cs, primary).filter(m => !isAlreadyClaimed(m)).length

const idsForRow = (primary: FamilyMemberDetail): number[] => {
  const cs = primary.content_sign
  const policy = effectivePolicy(cs)
  const ids = [primary.data_resources_id]
  for (const m of nonPrimaryMembers(cs, primary)) {
    if (isAlreadyClaimed(m)) continue
    if (policy === 'all') {
      ids.push(m.data_resources_id)
    } else if (policy === 'same_content_only' && m.family_relation === 'same_content') {
      ids.push(m.data_resources_id)
    }
    // 'none' → only primary
  }
  return ids
}

const totalIds = computed<number[]>(() => {
  const seen = new Set<number>()
  const out: number[] = []
  for (const p of props.selectedPrimaries) {
    for (const id of idsForRow(p)) {
      if (!seen.has(id)) {
        seen.add(id)
        out.push(id)
      }
    }
  }
  return out
})

const totalRelations = computed(() => {
  let n = 0
  for (const p of props.selectedPrimaries) n += familyMemberCount(p.content_sign, p)
  return n
})

const totalSkipped = computed(() => {
  let n = 0
  for (const p of props.selectedPrimaries)
    n += nonPrimaryMembers(p.content_sign, p).filter(isAlreadyClaimed).length
  return n
})

const totalSelectedMembers = computed(() => totalIds.value.length - props.selectedPrimaries.length)

const policyOptions: { value: Policy; label: string }[] = [
  { value: 'same_content_only', label: '仅带相同内容（推荐）' },
  { value: 'all',               label: '全部带（相同 + 过程 + 衍生）' },
  { value: 'none',              label: '都不带' },
]

const rowPolicyOptions = (primary: FamilyMemberDetail) => {
  const cs = primary.content_sign
  return [
    { value: 'same_content_only', label: `仅同内容 (${sameCountForRow(cs, primary)})` },
    { value: 'all',               label: `全选 (${allCountForRow(cs, primary)})` },
    { value: 'none',              label: '不带' },
  ]
}

const toggleExpand = (cs: string, primary: FamilyMemberDetail) => {
  const count = familyMemberCount(cs, primary)
  if (count > 20) {
    drawerForRow.value = drawerForRow.value === cs ? null : cs
    return
  }
  const s = new Set(expandedRows.value)
  if (s.has(cs)) s.delete(cs)
  else s.add(cs)
  expandedRows.value = s
}

const drawerPrimary = computed(() =>
  drawerForRow.value ? props.selectedPrimaries.find(p => p.content_sign === drawerForRow.value) ?? null : null,
)

const close = () => emit('update:modelValue', false)
const confirm = () => {
  emit('confirm', { ids: totalIds.value, skipNextTime: skipNextTime.value })
  close()
}
</script>

<template>
  <v-dialog :model-value="modelValue" @update:model-value="close" max-width="900">
    <v-card>
      <v-card-title>认领 {{ selectedPrimaries.length }} 个文件</v-card-title>

      <v-card-text>
        <!-- Global policy bar -->
        <div class="global-bar mb-3">
          🔗 共识别到 {{ totalRelations }} 个相似文件。批量默认：
          <v-select
            v-model="globalPolicy"
            :items="policyOptions"
            item-value="value"
            item-title="label"
            density="compact"
            hide-details
            variant="outlined"
            style="width: 260px;"
            data-test="global-policy-select"
          />
        </div>

        <div class="text-caption text-grey mb-2">选中文件（每行可单独调整）</div>

        <!-- One row per primary -->
        <div
          v-for="primary in selectedPrimaries"
          :key="primary.data_resources_id"
          :data-test="`batch-row-${primary.data_resources_id}`"
          class="batch-row"
        >
          <div class="row-main">
            <!-- File name -->
            <div class="fname">
              {{ primary.resources_name || '-' }}
              <span
                v-if="customizedRows.has(primary.content_sign)"
                class="text-caption text-warning ml-2"
              >已自定义</span>
            </div>

            <!-- Family chip / 无关联 -->
            <v-chip
              v-if="familyMemberCount(primary.content_sign, primary) > 0"
              size="small"
              variant="tonal"
              color="info"
              style="cursor: pointer; flex-shrink: 0;"
              :data-test="`row-chip-${primary.data_resources_id}`"
              @click="toggleExpand(primary.content_sign, primary)"
            >
              关联 {{ familyMemberCount(primary.content_sign, primary) }} ▾
            </v-chip>
            <span
              v-else
              class="text-caption text-disabled"
              :data-test="`row-no-family-${primary.data_resources_id}`"
            >无关联</span>

            <!-- Per-row policy dropdown (only when family exists) -->
            <v-select
              v-if="familyMemberCount(primary.content_sign, primary) > 0"
              :model-value="effectivePolicy(primary.content_sign)"
              :items="rowPolicyOptions(primary)"
              item-value="value"
              item-title="label"
              density="compact"
              hide-details
              variant="outlined"
              style="width: 230px; flex-shrink: 0;"
              :data-test="`row-policy-${primary.data_resources_id}`"
              @update:model-value="(v: any) => setRowPolicy(primary.content_sign, v)"
            />
          </div>

          <!-- Inline expansion (≤ 20 members) -->
          <div
            v-if="expandedRows.has(primary.content_sign) && familyMemberCount(primary.content_sign, primary) <= 20"
            class="row-expand"
          >
            <div
              v-for="m in nonPrimaryMembers(primary.content_sign, primary)"
              :key="m.data_resources_id"
              class="member-detail-row"
              :class="{ 'text-disabled': isAlreadyClaimed(m) }"
            >
              <span>{{ m.resources_name }}</span>
              <span class="ml-2 text-caption">{{ m.family_relation }}</span>
              <span v-if="isAlreadyClaimed(m)" class="ml-2 text-caption">
                已认领（{{ m.claimant_name || '?' }}）
              </span>
            </div>
          </div>
        </div>

        <v-divider class="my-3" />

        <!-- Skip next time -->
        <v-checkbox
          v-model="skipNextTime"
          label="以后总是按当前默认行为认领，不再询问"
          hide-details
          density="compact"
          data-test="skip-next-time"
        />

        <!-- Summary line -->
        <div class="summary mt-2">
          汇总：{{ selectedPrimaries.length }} 主 + {{ totalSelectedMembers }} 相似
          <span v-if="totalSkipped > 0" class="text-warning ml-2">
            （{{ totalSkipped }} 个相似已被认领，将跳过）
          </span>
        </div>
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn data-test="batch-cancel-btn" @click="close">取消</v-btn>
        <v-btn color="primary" data-test="batch-confirm-btn" @click="confirm">
          确认认领 {{ totalIds.length }} 个
        </v-btn>
      </v-card-actions>
    </v-card>

    <!-- Drawer-style dialog for large families (> 20 members) -->
    <v-dialog
      v-if="drawerForRow && drawerPrimary"
      :model-value="true"
      max-width="800"
      @update:model-value="drawerForRow = null"
    >
      <v-card>
        <v-card-title>
          {{ drawerPrimary.resources_name }} · 关联成员
        </v-card-title>
        <v-card-text style="max-height: 60vh; overflow-y: auto;">
          <div
            v-for="m in nonPrimaryMembers(drawerForRow!, drawerPrimary)"
            :key="m.data_resources_id"
            class="member-detail-row"
            :class="{ 'text-disabled': isAlreadyClaimed(m) }"
          >
            <span>{{ m.resources_name }}</span>
            <span class="ml-2 text-caption">{{ m.family_relation }}</span>
            <span v-if="isAlreadyClaimed(m)" class="ml-2 text-caption">
              已认领（{{ m.claimant_name || '?' }}）
            </span>
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="drawerForRow = null">关闭</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-dialog>
</template>

<style scoped>
.global-bar {
  background: #eef5ff;
  border: 1px solid #c5e1ff;
  padding: 10px 14px;
  border-radius: 6px;
  font-size: 13px;
  color: #1565c0;
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.batch-row {
  border: 1px solid #e0e0e0;
  border-radius: 6px;
  margin-bottom: 6px;
}
.row-main {
  display: flex;
  align-items: center;
  padding: 10px 12px;
  gap: 8px;
}
.fname {
  flex: 1;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.row-expand {
  background: #fafafa;
  padding: 8px 14px;
  border-top: 1px solid #e0e0e0;
}
.member-detail-row {
  padding: 4px 0;
  font-size: 12px;
  display: flex;
  align-items: center;
}
.summary {
  font-size: 12px;
  padding: 8px 10px;
  background: #f0f4ff;
  border-radius: 6px;
}
</style>
