<template>
  <div>
    <v-card flat>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-auto-fix</v-icon>
        归目推荐
        <v-chip size="x-small" class="ml-2" color="info" variant="tonal">规则匹配版 MVP</v-chip>
        <v-spacer />
        <v-btn variant="text" size="small" @click="loadPending">
          <v-icon>mdi-refresh</v-icon> 刷新
        </v-btn>
      </v-card-title>

      <v-tabs v-model="currentTab" color="primary" class="px-4">
        <v-tab value="new">
          新增数据
          <v-badge inline :content="newTotal" :model-value="newTotal > 0" class="ml-2" />
        </v-tab>
        <v-tab value="historical">
          普查数据
          <v-badge inline :content="historicalTotal" :model-value="historicalTotal > 0" class="ml-2" />
        </v-tab>
      </v-tabs>

      <v-window v-model="currentTab">
        <v-window-item value="new">

      <v-tabs v-model="currentLevel" density="compact" color="primary" class="px-2">
        <v-tab value="all">全部 {{ levelCounts.all }}</v-tab>
        <v-tab value="core">核心 {{ levelCounts.core }}</v-tab>
        <v-tab value="important">重要 {{ levelCounts.important }}</v-tab>
        <v-tab value="general">一般 {{ levelCounts.general }}</v-tab>
      </v-tabs>

      <v-card-text>
        <!-- 一般级专用：一键 AI + 清空余下 -->
        <div v-if="currentLevel === 'general'" class="d-flex align-center ga-3 mb-3 flex-wrap pa-2 bg-grey-lighten-5 rounded">
          <v-icon size="small" color="primary">mdi-auto-fix</v-icon>
          <span class="text-caption text-medium-emphasis">阈值 {{ Math.round(autoApplyThreshold * 100) }}%</span>
          <div style="flex: 1; max-width: 240px">
            <v-slider v-model="autoApplyThreshold" :min="0.1" :max="0.95" :step="0.05" hide-details density="compact" />
          </div>
          <v-btn color="primary" variant="tonal" size="small" :disabled="generalAutoApplyableCount === 0" :loading="autoApplying" @click="onAutoApply">
            一键 AI 归目（{{ generalAutoApplyableCount }}）
          </v-btn>
          <v-btn color="warning" variant="tonal" size="small" :disabled="generalSkippableCount === 0" @click="onBulkSkipGeneral">
            清空余下（{{ generalSkippableCount }}）
          </v-btn>
        </div>

        <!-- 顶部说明 + 阈值控制 -->
        <v-alert type="info" variant="tonal" density="compact" class="mb-3">
          <div class="text-body-2">
            扫描的文件根据
            <strong>文件名 / 路径 / 扩展名 / 归目目标和环节关键词</strong>
            匹配出最合适的归目目标。置信度越高表示匹配越精确。
          </div>
        </v-alert>

        <div class="d-flex align-center gap-3 mb-3 flex-wrap">
          <div style="flex: 1; max-width: 300px">
            <div class="text-caption mb-1">置信度过滤：≥ {{ Math.round(minConfidence * 100) }}%</div>
            <v-slider
              v-model="minConfidence"
              :min="0" :max="0.95" :step="0.05"
              hide-details density="compact"
              @end="loadPending"
            />
          </div>
          <v-text-field
            v-model.number="limit"
            label="数量上限"
            type="number"
            density="compact"
            variant="outlined"
            hide-details
            style="max-width: 120px"
            @keyup.enter="loadPending"
          />
          <v-btn
            color="success"
            variant="tonal"
            prepend-icon="mdi-flash-auto"
            :disabled="autoApplyDisabled"
            :loading="autoApplying"
            @click="onAutoApply"
          >
            自动归目（≥ {{ Math.round(autoApplyThreshold * 100) }}%）
          </v-btn>
          <v-text-field
            v-model.number="autoApplyThreshold"
            label="自动应用阈值"
            type="number"
            min="0.5" max="0.95" step="0.05"
            density="compact"
            variant="outlined"
            hide-details
            style="max-width: 140px"
            suffix="（0-1）"
          />
        </div>

        <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-2" />

        <div v-if="!loading && pending.length === 0" class="text-center text-medium-emphasis py-12">
          <v-icon size="64" color="grey-lighten-1">mdi-check-decagram-outline</v-icon>
          <div class="mt-2">暂无待归目文件，或已被全部认领归目</div>
        </div>

        <!-- 待归目文件列表 -->
        <v-expansion-panels v-model="expanded" multiple density="compact">
          <v-expansion-panel
            v-for="item in pending"
            :key="item.resource_id"
            :value="item.resource_id"
          >
            <v-expansion-panel-title>
              <div class="d-flex align-center flex-wrap" style="gap: 8px; flex: 1">
                <v-icon size="small">mdi-file-document-outline</v-icon>
                <strong>{{ item.resource_name || '(无名)' }}</strong>
                <v-chip
                  v-if="item.suggestions.length > 0"
                  size="x-small"
                  :color="confidenceColor(item.suggestions[0].confidence)"
                  variant="tonal"
                >
                  TOP {{ Math.round(item.suggestions[0].confidence * 100) }}%
                  · {{ item.suggestions[0].project_code }} / {{ item.suggestions[0].stage_code }}
                </v-chip>
                <v-chip v-else size="x-small" color="grey" variant="tonal">
                  无建议
                </v-chip>
                <v-spacer />
                <v-btn
                  v-if="item.suggestions.length > 0"
                  size="x-small"
                  color="primary"
                  variant="tonal"
                  :loading="applyingId === item.resource_id"
                  @click.stop="applyTopSuggestion(item)"
                >
                  应用 TOP 建议
                </v-btn>
                <v-btn
                  size="x-small"
                  color="secondary"
                  variant="text"
                  @click.stop="openAdjust(item)"
                >调整</v-btn>
              </div>
            </v-expansion-panel-title>
            <v-expansion-panel-text>
              <div v-if="item.suggestions.length === 0" class="text-caption text-medium-emphasis">
                没有匹配的归目建议，建议手动归到合适目标。
              </div>
              <div v-else>
                <div class="text-caption text-medium-emphasis mb-2">
                  全部 {{ item.suggestions.length }} 条建议（按置信度降序）：
                </div>
                <v-card
                  v-for="(s, i) in item.suggestions"
                  :key="i"
                  variant="outlined"
                  class="mb-2 pa-3"
                >
                  <div class="d-flex align-center mb-1">
                    <v-chip size="x-small" :color="confidenceColor(s.confidence)" variant="tonal" class="mr-2">
                      {{ Math.round(s.confidence * 100) }}%
                    </v-chip>
                    <strong class="text-body-2">
                      {{ s.project_code }} → {{ s.stage_code }} {{ s.stage_name }} → {{ s.file_rule_code }} {{ s.file_name }}
                    </strong>
                    <v-chip size="x-small" class="ml-2" variant="text">{{ stateLabel(s.data_state) }}</v-chip>
                    <v-spacer />
                    <v-btn
                      size="x-small"
                      color="primary"
                      variant="tonal"
                      :loading="applyingId === item.resource_id && applyingTargetIdx === i"
                      @click="applySuggestion(item, s, i)"
                    >
                      应用
                    </v-btn>
                  </div>
                  <div class="text-caption text-medium-emphasis">
                    命中规则：{{ s.reason || '-' }}
                  </div>
                </v-card>
              </div>
            </v-expansion-panel-text>
          </v-expansion-panel>
        </v-expansion-panels>
      </v-card-text>

        </v-window-item>

        <v-window-item value="historical">
          <v-card-text>
            <v-alert type="info" variant="tonal" density="compact" class="mb-3">
              <div class="text-body-2">
                普查数据 = 终端首次普查产生的存量，主要起辅助治理作用。可对选中条目<strong>批量按 AI 推荐归目</strong>，
                或点「展开归目建议」对单条做精细归目。确无价值的条目可走兜底「批量标已治理」从待办清掉。
              </div>
            </v-alert>

            <div v-if="pending.length > 0" class="d-flex align-center mb-3">
              <v-checkbox
                :model-value="selectedHistoricalIds.size === pending.length && pending.length > 0"
                :indeterminate="selectedHistoricalIds.size > 0 && selectedHistoricalIds.size < pending.length"
                density="compact"
                hide-details
                @update:model-value="(v: any) => toggleAllHistorical(!!v)"
              />
              <span class="mx-2 text-medium-emphasis">全选</span>
              <v-btn
                color="primary"
                variant="elevated"
                size="small"
                prepend-icon="mdi-auto-fix"
                :loading="bulkApplying"
                :disabled="selectedHistoricalIds.size === 0"
                @click="onBulkApplyHistorical"
              >
                批量按 AI 推荐归目 ({{ selectedHistoricalIds.size }})
              </v-btn>
              <v-btn
                class="ml-2"
                color="warning"
                variant="text"
                size="small"
                prepend-icon="mdi-archive-check-outline"
                :disabled="selectedHistoricalIds.size === 0 || bulkApplying"
                @click="onBulkDismissHistorical"
              >
                批量标已治理（兜底跳过）
              </v-btn>
            </div>

            <v-progress-linear v-if="loading" indeterminate color="primary" class="mb-2" />

            <div v-if="!loading && pending.length === 0" class="text-center text-medium-emphasis py-12">
              <v-icon size="64" color="grey-lighten-1">mdi-history</v-icon>
              <div class="mt-2">暂无普查数据</div>
            </div>

            <v-list density="compact" v-if="pending.length > 0">
              <template v-for="item in pending" :key="item.resource_id">
                <v-list-item lines="two">
                  <template #prepend>
                    <v-checkbox
                      :model-value="selectedHistoricalIds.has(item.resource_id)"
                      density="compact"
                      hide-details
                      @update:model-value="(v: any) => toggleHistoricalChecked(item.resource_id, !!v)"
                    />
                  </template>
                  <v-list-item-title>{{ item.resource_name }}</v-list-item-title>
                  <template #append>
                    <v-btn
                      variant="text"
                      size="small"
                      :prepend-icon="expandedHistoricalSuggestions[item.resource_id] ? 'mdi-chevron-up' : 'mdi-chevron-down'"
                      @click="expandHistoricalSuggestions(item.resource_id)"
                    >
                      {{ expandedHistoricalSuggestions[item.resource_id] ? '收起' : '展开归目建议' }}
                    </v-btn>
                  </template>
                </v-list-item>

                <div
                  v-if="expandedHistoricalSuggestions[item.resource_id]"
                  class="px-4 pb-3"
                >
                  <div
                    v-if="(expandedHistoricalSuggestions[item.resource_id] || []).length === 0"
                    class="text-caption text-medium-emphasis"
                  >
                    暂无可用建议
                  </div>
                  <v-card
                    v-for="(s, i) in expandedHistoricalSuggestions[item.resource_id] || []"
                    :key="i"
                    variant="outlined"
                    class="mb-2 pa-2"
                  >
                    <div class="d-flex align-center justify-space-between">
                      <div>
                        <v-chip size="x-small" :color="confidenceColor(s.confidence)" variant="tonal" class="mr-1">
                          {{ Math.round(s.confidence * 100) }}%
                        </v-chip>
                        <span class="text-body-2">
                          {{ s.project_code || s.project_name }} / {{ s.stage_name || s.stage_code }} / {{ s.file_rule_name || s.file_rule_code }}
                        </span>
                      </div>
                      <v-btn
                        size="x-small"
                        color="primary"
                        variant="tonal"
                        :loading="applyingId === item.resource_id && applyingTargetIdx === i"
                        @click="applySuggestion(item, s, i)"
                      >
                        应用
                      </v-btn>
                    </div>
                  </v-card>
                </div>
              </template>
            </v-list>

            <v-pagination
              v-if="historicalTotal > historicalPageSize"
              v-model="historicalPage"
              :length="Math.ceil(historicalTotal / historicalPageSize)"
              class="mt-3"
              density="compact"
              @update:model-value="loadPending"
            />
          </v-card-text>
        </v-window-item>
      </v-window>
    </v-card>

    <!-- 2026-05-21 三级分流：重要级 multi-source 拦截弹窗 -->
    <v-dialog v-model="authoritativeDialog.open" max-width="640">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-scale-balance</v-icon>
          选择权威源
        </v-card-title>
        <v-card-text>
          <div class="text-body-2 mb-3 text-medium-emphasis">
            同一资源在以下位置被检测为相似 / 同源。请选定一份作为"权威"——其余将以参考件形式入账。
          </div>
          <v-radio-group v-model="selectedAuthoritative" density="compact">
            <v-radio
              v-for="m in authoritativeDialog.members"
              :key="m.data_resources_id"
              :value="m.data_resources_id"
            >
              <template #label>
                <div>
                  <div class="text-body-2">{{ m.resources_name || '(无文件名)' }}</div>
                  <div class="text-caption text-medium-emphasis">{{ m.family_relation || '-' }}</div>
                </div>
              </template>
            </v-radio>
          </v-radio-group>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="authoritativeDialog.open = false">取消</v-btn>
          <v-btn color="primary" :disabled="!selectedAuthoritative" @click="confirmAuthoritativeAndRetry">确认并应用</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 一般级"清空余下"二次确认 -->
    <v-dialog v-model="generalSkipDialog.open" max-width="480">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-broom</v-icon>
          清空余下
        </v-card-title>
        <v-card-text>
          <div class="text-body-2">
            将把 <strong>{{ generalSkipDialog.pendingIds.length }}</strong> 条 AI 未达阈值的一般级数据标为已治理，不再出现。审计可追溯。
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="generalSkipDialog.busy" @click="generalSkipDialog.open = false">取消</v-btn>
          <v-btn color="warning" variant="tonal" :loading="generalSkipDialog.busy" @click="confirmBulkSkipGeneral">确认</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 普查数据批量标已治理：输入治理说明 -->
    <v-dialog v-model="bulkDismissDialog.open" max-width="520">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-archive-check-outline</v-icon>
          批量标已治理
        </v-card-title>
        <v-card-text>
          <div class="text-body-2 text-medium-emphasis mb-3">
            将把 <strong>{{ selectedHistoricalIds.size }}</strong> 条普查数据标记为已治理，不再出现在待办列表。审计可追溯，但本动作不可撤销。
          </div>
          <v-text-field
            v-model="bulkDismissDialog.reason"
            label="治理说明 *"
            variant="outlined"
            density="compact"
            autofocus
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="bulkDismissDialog.busy" @click="bulkDismissDialog.open = false">取消</v-btn>
          <v-btn
            color="warning"
            variant="tonal"
            :loading="bulkDismissDialog.busy"
            :disabled="!bulkDismissDialog.reason.trim()"
            @click="confirmBulkDismissHistorical"
          >确认</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
      {{ snackbar.text }}
    </v-snackbar>

    <!-- 调整对话框 -->
    <v-dialog v-model="adjustDialog" max-width="600">
      <v-card>
        <v-card-title>调整归目目标</v-card-title>
        <v-card-text>
          <div class="text-caption mb-2 text-medium-emphasis">资源：{{ adjustItem?.resource_name || '—' }}</div>
          <v-alert
            v-if="!loadingProjects && projectOptions.length === 0"
            type="warning"
            variant="tonal"
            density="compact"
            class="mb-3"
          >
            暂无可选归目目标。请先在「数据业务项目」立项，或确认个人文件模版已同步并重启终端。
            <div class="mt-2">
              <v-btn size="x-small" variant="text" color="primary" @click="loadProjectOptions">重试</v-btn>
            </div>
          </v-alert>
          <v-select
            v-model="adjustForm.project_id"
            :items="projectOptions"
            item-title="label"
            item-value="value"
            label="归目目标"
            density="compact"
            variant="outlined"
            class="mb-2"
            :loading="loadingProjects"
            :disabled="loadingProjects || projectOptions.length === 0"
            @update:modelValue="onAdjustProjectChange"
          />
          <v-select
            v-model="adjustForm.stage_code"
            :items="adjustStageOptions"
            item-title="label"
            item-value="value"
            label="环节"
            density="compact"
            variant="outlined"
            :disabled="!adjustForm.project_id"
            class="mb-2"
          />
          <v-select
            v-model="adjustForm.file_rule_code"
            :items="adjustRuleOptions"
            item-title="label"
            item-value="value"
            label="文件规则"
            density="compact"
            variant="outlined"
            :disabled="!adjustForm.stage_code"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="adjustDialog = false">取消</v-btn>
          <v-btn
            color="primary"
            variant="elevated"
            :loading="adjustApplying"
            :disabled="!canAdjustApply"
            @click="onAdjustApply"
          >应用</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'

interface Suggestion {
  project_id: number
  project_code: string
  stage_code: string
  stage_name: string
  file_rule_code: string
  file_name: string
  data_state: string
  confidence: number
  reason: string
}

interface PendingItem {
  resource_id: number
  resource_name: string
  suggestions: Suggestion[]
}

import {
  API_BASE,
  fetchClassifyPending,
  fetchClassifySuggestions,
  bulkDismissHistorical,
  fetchFamilyMembers,
  setFamilyAuthoritative,
  type ClassifyOrigin,
} from '../services/api'

const pending = ref<PendingItem[]>([])
const loading = ref(false)
const expanded = ref<number[]>([])

// 2026-05-20 历史/新数据分流：Tab + 历史紧凑列表 + 按需展开
const currentTab = ref<ClassifyOrigin>('new')
const newTotal = ref(0)
const historicalTotal = ref(0)
const historicalPage = ref(1)
const historicalPageSize = 20
const selectedHistoricalIds = ref<Set<number>>(new Set())
const expandedHistoricalSuggestions = ref<Record<number, Suggestion[]>>({})
const bulkDismissDialog = ref({
  open: false,
  reason: '首次普查存量批量跳过',
  busy: false,
})
const generalSkipDialog = ref({
  open: false,
  busy: false,
  pendingIds: [] as number[],
})

// 2026-05-21 三级分流：level sub-tab + 权威源弹窗
const currentLevel = ref<'all' | 'core' | 'important' | 'general'>('all')
const levelCounts = computed(() => {
  const out = { all: pending.value.length, core: 0, important: 0, general: 0 }
  for (const p of pending.value) {
    const top = p.suggestions?.[0]
    if (!top) continue
    if ((top as any).project_code === 'SYS-PERSONAL-CORE') out.core++
    else if ((top as any).project_code === 'SYS-PERSONAL-IMPORTANT') out.important++
    else if ((top as any).project_code === 'SYS-PERSONAL-GENERAL') out.general++
  }
  return out
})
const filteredPending = computed(() => {
  if (currentLevel.value === 'all') return pending.value
  const code = ({ core: 'SYS-PERSONAL-CORE', important: 'SYS-PERSONAL-IMPORTANT', general: 'SYS-PERSONAL-GENERAL' } as any)[currentLevel.value]
  return pending.value.filter(p => (p.suggestions?.[0] as any)?.project_code === code)
})
const generalAutoApplyableCount = computed(() =>
  pending.value.filter(p =>
    (p.suggestions?.[0] as any)?.project_code === 'SYS-PERSONAL-GENERAL' &&
    (p.suggestions?.[0]?.confidence || 0) >= autoApplyThreshold.value
  ).length
)
const generalSkippableCount = computed(() =>
  pending.value.filter(p =>
    (p.suggestions?.[0] as any)?.project_code === 'SYS-PERSONAL-GENERAL' &&
    (p.suggestions?.[0]?.confidence || 0) < autoApplyThreshold.value
  ).length
)
const authoritativeDialog = ref<{
  open: boolean
  familyId: number
  members: Array<{ data_resources_id: number; resources_name?: string | null; family_relation?: string | null }>
  pendingItem: PendingItem | null
  pendingSuggestion: Suggestion | null
  pendingIdx: number
}>({
  open: false, familyId: 0, members: [], pendingItem: null, pendingSuggestion: null, pendingIdx: -1,
})
const selectedAuthoritative = ref<number | null>(null)

const minConfidence = ref(0.3)
const limit = ref(30)
const autoApplyThreshold = ref(0.5)
const autoApplying = ref(false)
const applyingId = ref<number>(0)
const applyingTargetIdx = ref<number>(-1)

const snackbar = ref({ show: false, text: '', color: 'success' })

const autoApplyDisabled = computed(() =>
  pending.value.length === 0 ||
  !pending.value.some(p => p.suggestions.some(s => s.confidence >= autoApplyThreshold.value))
)

function confidenceColor(c: number): string {
  if (c >= 0.8) return 'success'
  if (c >= 0.5) return 'primary'
  if (c >= 0.3) return 'warning'
  return 'grey'
}

function stateLabel(s: string): string {
  return ({ input: '输入', process: '过程', output: '产出' } as Record<string, string>)[s] || s
}

async function loadPending() {
  loading.value = true
  try {
    if (currentTab.value === 'new') {
      const data = await fetchClassifyPending({
        origin: 'new',
        page: 1,
        pageSize: limit.value || 30,
        minConfidence: minConfidence.value,
      })
      pending.value = data.items as PendingItem[]
      newTotal.value = data.total
    } else {
      const data = await fetchClassifyPending({
        origin: 'historical',
        page: historicalPage.value,
        pageSize: historicalPageSize,
      })
      pending.value = data.items as PendingItem[]
      historicalTotal.value = data.total
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载失败：' + e.message, color: 'error' }
  } finally {
    loading.value = false
  }
}

// 进入页面后另一个 Tab 的徽标计数：用 page_size=1 的廉价拉取
async function warmInactiveBadge() {
  try {
    const other: ClassifyOrigin = currentTab.value === 'new' ? 'historical' : 'new'
    const data = await fetchClassifyPending({ origin: other, page: 1, pageSize: 1 })
    if (other === 'new') newTotal.value = data.total
    else historicalTotal.value = data.total
  } catch {
    // 静默：徽标只是 nice-to-have
  }
}

watch(currentTab, () => {
  historicalPage.value = 1
  selectedHistoricalIds.value = new Set()
  expandedHistoricalSuggestions.value = {}
  loadPending()
})

function toggleHistoricalChecked(id: number, on: boolean) {
  const next = new Set(selectedHistoricalIds.value)
  if (on) next.add(id)
  else next.delete(id)
  selectedHistoricalIds.value = next
}

function toggleAllHistorical(on: boolean) {
  selectedHistoricalIds.value = on
    ? new Set(pending.value.map(p => p.resource_id))
    : new Set()
}

function onBulkDismissHistorical() {
  if (selectedHistoricalIds.value.size === 0) return
  bulkDismissDialog.value = { open: true, reason: '首次普查存量批量跳过', busy: false }
}

// 批量按 AI TOP 推荐归目：对每个选中条目，应用 suggestions[0]。
// 无推荐 / 未展开 AI 的条目记为 skipped（不影响其它条目）。
const bulkApplying = ref(false)
async function onBulkApplyHistorical() {
  const ids = Array.from(selectedHistoricalIds.value)
  if (ids.length === 0) return
  bulkApplying.value = true
  let applied = 0
  let skipped = 0
  let failed = 0
  try {
    for (const id of ids) {
      const item = pending.value.find(p => p.resource_id === id)
      if (!item) { skipped++; continue }
      let suggestion = item.suggestions[0] as any
      // 若该条目还没拉过推荐，先按需拉一次（不展开 UI）
      if (!suggestion) {
        try {
          const d = await fetchClassifySuggestions(id)
          suggestion = d.suggestions?.[0]
        } catch { /* 拉不到就当 skipped */ }
      }
      if (!suggestion) { skipped++; continue }
      try {
        const res = await fetch(`${API_BASE}/ai/classify/apply`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            resource_id: id,
            project_id: suggestion.project_id,
            stage_code: suggestion.stage_code,
            file_rule_code: suggestion.file_rule_code,
          }),
        })
        const json = await res.json()
        if (json.success) applied++
        else failed++
      } catch {
        failed++
      }
    }
    snackbar.value = {
      show: true,
      text: `批量归目：成功 ${applied} 条 / 跳过 ${skipped} 条（无推荐）/ 失败 ${failed} 条`,
      color: failed > 0 ? 'warning' : 'success',
    }
    selectedHistoricalIds.value = new Set()
    await loadPending()
  } finally {
    bulkApplying.value = false
  }
}

async function confirmBulkDismissHistorical() {
  const reason = bulkDismissDialog.value.reason.trim()
  if (!reason) return
  const ids = Array.from(selectedHistoricalIds.value)
  if (ids.length === 0) {
    bulkDismissDialog.value.open = false
    return
  }
  bulkDismissDialog.value.busy = true
  try {
    const r = await bulkDismissHistorical(ids, reason)
    snackbar.value = { show: true, text: `已批量标已治理 ${r.dismissed} 条`, color: 'success' }
    selectedHistoricalIds.value = new Set()
    bulkDismissDialog.value.open = false
    await loadPending()
  } catch (e: any) {
    snackbar.value = { show: true, text: '批量治理失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    bulkDismissDialog.value.busy = false
  }
}

async function expandHistoricalSuggestions(resourceId: number) {
  if (expandedHistoricalSuggestions.value[resourceId]) {
    const next = { ...expandedHistoricalSuggestions.value }
    delete next[resourceId]
    expandedHistoricalSuggestions.value = next
    return
  }
  try {
    const d = await fetchClassifySuggestions(resourceId)
    const next = { ...expandedHistoricalSuggestions.value }
    next[resourceId] = (d.suggestions || []) as Suggestion[]
    expandedHistoricalSuggestions.value = next
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载推荐失败：' + (e?.message || String(e)), color: 'error' }
  }
}

async function applySuggestion(item: PendingItem, s: Suggestion, idx: number) {
  applyingId.value = item.resource_id
  applyingTargetIdx.value = idx
  try {
    const res = await fetch(`${API_BASE}/ai/classify/apply`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        resource_id: item.resource_id,
        project_id: s.project_id,
        stage_code: s.stage_code,
        file_rule_code: s.file_rule_code,
      }),
    })
    // 2026-05-21 三级分流：409 = 重要级 + 多源未确权，拉成员列表让用户先选权威
    if (res.status === 409) {
      const j = await res.json()
      const fid = j.data?.family_id
      if (fid) {
        try {
          const r = await fetchFamilyMembers(fid)
          authoritativeDialog.value = {
            open: true,
            familyId: fid,
            members: r.members,
            pendingItem: item,
            pendingSuggestion: s,
            pendingIdx: idx,
          }
          selectedAuthoritative.value = null
        } catch (e: any) {
          snackbar.value = { show: true, text: '加载家族失败：' + e.message, color: 'error' }
        }
      }
      return
    }
    const json = await res.json()
    if (json.success) {
      const hint = json.hint
      const msg = hint === 'transferred_to_memorandum_pending'
        ? '该核心级资源已转入核心登记待办'
        : `已归目到 ${(s as any).project_code} / ${s.stage_code} / ${s.file_rule_code}`
      snackbar.value = { show: true, text: msg, color: 'success' }
      pending.value = pending.value.filter(p => p.resource_id !== item.resource_id)
    } else {
      snackbar.value = { show: true, text: '应用失败：' + json.error, color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '应用失败：' + e.message, color: 'error' }
  } finally {
    applyingId.value = 0
    applyingTargetIdx.value = -1
  }
}

async function confirmAuthoritativeAndRetry() {
  if (!selectedAuthoritative.value) return
  try {
    await setFamilyAuthoritative(authoritativeDialog.value.familyId, selectedAuthoritative.value)
    const item = authoritativeDialog.value.pendingItem
    const s = authoritativeDialog.value.pendingSuggestion
    const idx = authoritativeDialog.value.pendingIdx
    authoritativeDialog.value.open = false
    if (item && s) await applySuggestion(item, s, idx)
  } catch (e: any) {
    snackbar.value = { show: true, text: '设置权威源失败：' + e.message, color: 'error' }
  }
}

function onBulkSkipGeneral() {
  const skippable = pending.value.filter(p =>
    (p.suggestions?.[0] as any)?.project_code === 'SYS-PERSONAL-GENERAL' &&
    (p.suggestions?.[0]?.confidence || 0) < autoApplyThreshold.value
  )
  if (skippable.length === 0) return
  generalSkipDialog.value = { open: true, busy: false, pendingIds: skippable.map(p => p.resource_id) }
}

async function confirmBulkSkipGeneral() {
  const ids = generalSkipDialog.value.pendingIds
  if (ids.length === 0) {
    generalSkipDialog.value.open = false
    return
  }
  generalSkipDialog.value.busy = true
  try {
    await bulkDismissHistorical(ids, '一般级 AI 未匹配，批量跳过')
    snackbar.value = { show: true, text: `已批量跳过 ${ids.length} 条`, color: 'success' }
    generalSkipDialog.value.open = false
    await loadPending()
  } catch (e: any) {
    snackbar.value = { show: true, text: '清空失败：' + (e?.message || String(e)), color: 'error' }
  } finally {
    generalSkipDialog.value.busy = false
  }
}

async function applyTopSuggestion(item: PendingItem) {
  if (item.suggestions.length === 0) return
  await applySuggestion(item, item.suggestions[0], 0)
}

async function onAutoApply() {
  const candidates = pending.value
    .filter(p => p.suggestions.length > 0 && p.suggestions[0].confidence >= autoApplyThreshold.value)
    .map(p => ({ item: p, top: p.suggestions[0] }))
  if (candidates.length === 0) {
    snackbar.value = { show: true, text: `当前没有 ≥ ${Math.round(autoApplyThreshold.value * 100)}% 的建议`, color: 'warning' }
    return
  }
  if (!confirm(`将自动归目 ${candidates.length} 个文件（置信度 ≥ ${Math.round(autoApplyThreshold.value * 100)}%）。继续？`)) {
    return
  }
  autoApplying.value = true
  let success = 0
  let failed = 0
  for (const { item, top } of candidates) {
    try {
      const res = await fetch(`${API_BASE}/ai/classify/apply`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          resource_id: item.resource_id,
          project_id: top.project_id,
          stage_code: top.stage_code,
          file_rule_code: top.file_rule_code,
        }),
      })
      const json = await res.json()
      if (json.success) success++
      else failed++
    } catch {
      failed++
    }
  }
  snackbar.value = {
    show: true,
    text: `自动归目完成：成功 ${success} · 失败 ${failed}`,
    color: failed > 0 ? 'warning' : 'success',
  }
  autoApplying.value = false
  await loadPending()
}

// 2026-05-21 驳回入口已移除——归目推荐是唯一归目入口，不再需要驳回路径。
// 若需要"跳过某条"，普查 Tab 用「批量标已治理」，一般级用「清空余下」。

// 调整 state
interface ProjectOpt { label: string; value: number }
interface StageOpt { label: string; value: string }
interface RuleOpt { label: string; value: string }

const adjustDialog = ref(false)
const adjustItem = ref<PendingItem | null>(null)
const adjustForm = ref({ project_id: 0, stage_code: '', file_rule_code: '' })
const adjustApplying = ref(false)
const projectOptions = ref<ProjectOpt[]>([])
const loadingProjects = ref(false)
const adjustStagesData = ref<Array<{ stage_code: string; stage_name: string; rules: Array<{ file_rule_code: string; file_name: string; data_state: string }> }>>([])

const adjustStageOptions = computed<StageOpt[]>(() =>
  adjustStagesData.value.map(s => ({ label: `${s.stage_code} ${s.stage_name}`, value: s.stage_code }))
)
const adjustRuleOptions = computed<RuleOpt[]>(() => {
  const stage = adjustStagesData.value.find(s => s.stage_code === adjustForm.value.stage_code)
  if (!stage) return []
  return stage.rules.map(r => ({ label: `${r.file_rule_code} ${r.file_name}`, value: r.file_rule_code }))
})
const canAdjustApply = computed(() =>
  !!adjustForm.value.project_id && !!adjustForm.value.stage_code && !!adjustForm.value.file_rule_code
)

async function loadProjectOptions() {
  loadingProjects.value = true
  try {
    const res = await fetch(`${API_BASE}/projects?status=active`)
    const json = await res.json()
    if (json.success) {
      const list = Array.isArray(json.data) ? json.data : (json.data?.items || [])
      projectOptions.value = list.map((p: any) => ({
        label: `${p.project_code} ${p.project_name}`,
        value: p.id,
      }))
    } else {
      snackbar.value = { show: true, text: '加载归目目标列表失败：' + (json.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载归目目标列表失败：' + e.message, color: 'error' }
  } finally {
    loadingProjects.value = false
  }
}

async function onAdjustProjectChange(projectId: number) {
  adjustForm.value.stage_code = ''
  adjustForm.value.file_rule_code = ''
  adjustStagesData.value = []
  if (!projectId) return
  try {
    const res = await fetch(`${API_BASE}/projects/${projectId}/stages-with-rules`)
    const json = await res.json()
    if (json.success && json.data?.stages) {
      adjustStagesData.value = json.data.stages
    } else {
      snackbar.value = { show: true, text: '加载环节列表失败：' + (json.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载环节列表失败：' + e.message, color: 'error' }
  }
}

// stage 变更时清空已选 rule
watch(() => adjustForm.value.stage_code, () => {
  adjustForm.value.file_rule_code = ''
})

function openAdjust(item: PendingItem) {
  adjustItem.value = item
  adjustForm.value = { project_id: 0, stage_code: '', file_rule_code: '' }
  adjustStagesData.value = []
  adjustDialog.value = true
  // 每次打开都重新拉取，避免上次失败 / 数据陈旧 / 个人项目刚被初始化等情况下项目下拉为空
  loadProjectOptions()
}

async function onAdjustApply() {
  if (!adjustItem.value || !canAdjustApply.value) return
  adjustApplying.value = true
  try {
    const res = await fetch(`${API_BASE}/ai/classify/apply`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        resource_id: adjustItem.value.resource_id,
        project_id: adjustForm.value.project_id,
        stage_code: adjustForm.value.stage_code,
        file_rule_code: adjustForm.value.file_rule_code,
      }),
    })
    const json = await res.json()
    if (json.success) {
      snackbar.value = { show: true, text: '调整归目成功', color: 'success' }
      pending.value = pending.value.filter(p => p.resource_id !== adjustItem.value!.resource_id)
      adjustDialog.value = false
    } else {
      snackbar.value = { show: true, text: '失败：' + (json.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '失败：' + e.message, color: 'error' }
  } finally {
    adjustApplying.value = false
  }
}

onMounted(async () => {
  await loadPending()
  await warmInactiveBadge()
})
</script>
