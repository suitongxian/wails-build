<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FamilyMemberDetail } from '../services/api'

const props = defineProps<{
  modelValue: boolean
  primary: FamilyMemberDetail
  members: FamilyMemberDetail[]
  claimStatus: number
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm', payload: { ids: number[]; skipNextTime: boolean }): void
}>()

const sameContent = computed(() => props.members.filter(m =>
  m.family_relation === 'same_content' && m.data_resources_id !== props.primary.data_resources_id))
const processVersion = computed(() => props.members.filter(m => m.family_relation === 'process_version'))
const derived = computed(() => props.members.filter(m => m.family_relation === 'derived'))

const checkedIds = ref<Set<number>>(new Set())
const skipNextTime = ref(false)

const isAlreadyClaimed = (m: FamilyMemberDetail) => (m.claim_status ?? 0) !== 0

const resetDefaults = () => {
  const s = new Set<number>()
  sameContent.value.forEach(m => {
    if (!isAlreadyClaimed(m)) s.add(m.data_resources_id)
  })
  checkedIds.value = s
  skipNextTime.value = false
}

watch(() => [props.modelValue, props.members], ([open]) => {
  if (open) resetDefaults()
}, { immediate: true })

const toggle = (m: FamilyMemberDetail) => {
  if (isAlreadyClaimed(m)) return
  const s = new Set(checkedIds.value)
  if (s.has(m.data_resources_id)) s.delete(m.data_resources_id)
  else s.add(m.data_resources_id)
  checkedIds.value = s
}

const finalIds = computed(() => {
  const ids = [props.primary.data_resources_id]
  checkedIds.value.forEach(id => ids.push(id))
  return ids
})

const close = () => emit('update:modelValue', false)
const confirm = () => {
  emit('confirm', { ids: finalIds.value, skipNextTime: skipNextTime.value })
  close()
}
const claimOnlyPrimary = () => {
  emit('confirm', { ids: [props.primary.data_resources_id], skipNextTime: skipNextTime.value })
  close()
}

const fmtScore = (s: number | null | undefined) => s == null ? '' : `${(s * 100).toFixed(0)}%`
const totalRelations = computed(() => sameContent.value.length + processVersion.value.length + derived.value.length)
</script>

<template>
  <v-dialog :model-value="modelValue" @update:model-value="close" max-width="640">
    <v-card>
      <v-card-title>一并认领相似文件？</v-card-title>
      <v-card-text>
        <div class="mb-3">
          <div class="font-weight-medium">你正在认领：{{ primary.resources_name || '-' }}</div>
        </div>
        <div class="info-bar mb-3">
          🔗 关联 {{ totalRelations }} 个相似文件
        </div>

        <div v-if="sameContent.length > 0" class="mb-3">
          <div class="text-caption font-weight-medium mb-1">✓ 相同内容（{{ sameContent.length }} 个 · 默认勾选）</div>
          <div v-for="m in sameContent" :key="m.data_resources_id" class="member-row" data-test="member-row">
            <v-checkbox
              :model-value="checkedIds.has(m.data_resources_id)"
              :disabled="isAlreadyClaimed(m)"
              hide-details density="compact"
              @update:model-value="toggle(m)"
            />
            <span :class="{ 'text-disabled': isAlreadyClaimed(m) }">{{ m.resources_name }}</span>
            <span v-if="isAlreadyClaimed(m)" class="text-caption text-disabled ml-2">
              已认领（认领人：{{ m.claimant_name || '?' }}）
            </span>
            <span class="ml-auto text-success text-caption">{{ fmtScore(m.family_score) }}</span>
          </div>
        </div>

        <div v-if="processVersion.length > 0" class="mb-3">
          <div class="text-caption font-weight-medium mb-1">☐ 过程版本（{{ processVersion.length }} 个 · 按需勾选）</div>
          <div v-for="m in processVersion" :key="m.data_resources_id" class="member-row" data-test="member-row">
            <v-checkbox
              :model-value="checkedIds.has(m.data_resources_id)"
              :disabled="isAlreadyClaimed(m)"
              hide-details density="compact"
              @update:model-value="toggle(m)"
            />
            <span :class="{ 'text-disabled': isAlreadyClaimed(m) }">{{ m.resources_name }}</span>
            <span v-if="isAlreadyClaimed(m)" class="text-caption text-disabled ml-2">已认领</span>
            <span class="ml-auto text-warning text-caption">{{ fmtScore(m.family_score) }}</span>
          </div>
        </div>

        <div v-if="derived.length > 0" class="mb-3">
          <div class="text-caption font-weight-medium mb-1">☐ 衍生文件（{{ derived.length }} 个）</div>
          <div v-for="m in derived" :key="m.data_resources_id" class="member-row" data-test="member-row">
            <v-checkbox
              :model-value="checkedIds.has(m.data_resources_id)"
              :disabled="isAlreadyClaimed(m)"
              hide-details density="compact"
              @update:model-value="toggle(m)"
            />
            <span :class="{ 'text-disabled': isAlreadyClaimed(m) }">{{ m.resources_name }}</span>
            <span class="ml-auto text-info text-caption">{{ fmtScore(m.family_score) }}</span>
          </div>
        </div>

        <v-divider class="my-3" />
        <v-checkbox
          v-model="skipNextTime"
          label="以后总是按当前选择，不再询问"
          hide-details density="compact"
          data-test="skip-next-time"
        />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="claimOnlyPrimary" data-test="only-primary-btn">仅领此文件</v-btn>
        <v-btn color="primary" data-test="confirm-btn" @click="confirm">
          认领 {{ finalIds.length }} 个（1 主 + {{ finalIds.length - 1 }} 相似）
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.info-bar {
  background: #eef5ff;
  border-left: 3px solid #1976d2;
  padding: 10px 12px;
  border-radius: 4px;
  font-size: 13px;
  color: #1565c0;
}
.member-row {
  display: flex;
  align-items: center;
  padding: 4px 8px;
  font-size: 13px;
}
</style>
