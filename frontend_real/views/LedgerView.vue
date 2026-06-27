<template>
  <div>
    <v-card flat>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-book-open-variant</v-icon>
        数据资产标识底账
        <v-spacer />
        <v-chip size="small" variant="tonal" color="primary">显示 {{ displayList.length }} 条</v-chip>
        <v-chip v-if="!includeDrafts && draftCount > 0" size="small" variant="text" class="ml-1">
          已隐藏 {{ draftCount }} 条草稿
        </v-chip>
      </v-card-title>
      <v-card-subtitle>一件一号 · 一件一账 · 一件一链 · 一件一责 · 一件一处置</v-card-subtitle>

      <v-card-text>
        <v-row dense>
          <v-col cols="12" md="3">
            <v-select
              v-model="filters.project_code"
              :items="projectOptions"
              label="项目"
              item-title="title"
              item-value="value"
              density="compact"
              hide-details
              clearable
              @update:model-value="loadList"
            />
          </v-col>
          <v-col cols="12" md="2">
            <v-select
              v-model="filters.lifecycle_status"
              :items="statusOptions"
              label="生命周期状态"
              density="compact"
              hide-details
              clearable
              @update:model-value="loadList"
            />
          </v-col>
          <v-col cols="12" md="2">
            <v-select
              v-model="filters.sensitivity_level"
              :items="sensitivityOptions"
              label="敏感等级"
              density="compact"
              hide-details
              clearable
              @update:model-value="loadList"
            />
          </v-col>
          <v-col cols="12" md="2">
            <v-text-field
              v-model="filters.stage_code"
              label="工作环节编码"
              density="compact"
              hide-details
              clearable
              @keyup.enter="loadList"
            />
          </v-col>
          <v-col cols="12" md="3">
            <v-text-field
              v-model="filters.keyword"
              label="关键词（资产名/底账编号/文件版本编码）"
              density="compact"
              hide-details
              clearable
              @keyup.enter="loadList"
            />
          </v-col>
        </v-row>

        <div class="d-flex align-center mt-2">
          <v-switch
            v-model="includeDrafts"
            color="primary"
            density="compact"
            hide-details
            label="包含草稿"
          />
          <v-tooltip location="top" max-width="320">
            <template #activator="{ props }">
              <v-icon v-bind="props" size="small" class="ml-1 text-medium-emphasis">mdi-help-circle-outline</v-icon>
            </template>
            <div>
              草稿 = 立项时为每条文件规则建的"应当存在的资产"占位，状态为 planned。
              文件还没上传 / 派生 / 领取，所以列表默认隐藏，避免视图混乱。
              做盘点或审计时打开开关查全量底账。
            </div>
          </v-tooltip>
          <v-spacer />
          <v-btn variant="text" size="small" @click="loadList">
            <v-icon>mdi-refresh</v-icon> 刷新
          </v-btn>
          <v-btn variant="text" size="small" color="primary" @click="exportXlsx">
            <v-icon>mdi-microsoft-excel</v-icon> 导出 Excel
          </v-btn>
          <!-- V3-8 §7.5 文档要求底账支持 CSV / JSON 导出 -->
          <v-btn variant="text" size="small" @click="exportFormat('csv')">
            <v-icon>mdi-file-delimited-outline</v-icon> 导出 CSV
          </v-btn>
          <v-btn variant="text" size="small" @click="exportFormat('json')">
            <v-icon>mdi-code-json</v-icon> 导出 JSON
          </v-btn>
        </div>

        <v-data-table
          :headers="headers"
          :items="displayList"
          :loading="loading"
          density="compact"
          items-per-page="20"
          class="mt-2"
        >
          <template v-slot:item.ledger_code="{ item }">
            <span class="font-monospace text-primary">{{ item.ledger_code }}</span>
          </template>
          <template v-slot:item.file_version_code="{ item }">
            <span class="font-monospace text-caption">{{ item.file_version_code }}</span>
          </template>
          <template v-slot:item.sensitivity_level="{ item }">
            <v-chip :color="sensColor(item.sensitivity_level)" size="x-small" variant="tonal">
              {{ sensLabel(item.sensitivity_level) }}
            </v-chip>
          </template>
          <template v-slot:item.lifecycle_status="{ item }">
            <v-chip :color="statusColor(item.lifecycle_status)" size="x-small" variant="tonal">
              {{ statusLabel(item.lifecycle_status) }}
            </v-chip>
          </template>
          <template v-slot:item.actions="{ item }">
            <v-btn size="x-small" variant="text" color="primary" @click="openDetail(item)">
              <v-icon>mdi-eye</v-icon> 详情
            </v-btn>
          </template>
        </v-data-table>

        <div v-if="!loading && list.length === 0" class="text-center text-medium-emphasis py-12">
          暂无底账记录
        </div>
      </v-card-text>
    </v-card>

    <!-- 详情 + 状态切换 + 事件流 -->
    <v-dialog v-model="detailOpen" max-width="780">
      <v-card v-if="detail">
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-book-open-variant</v-icon>
          {{ detail.ledger_code }} · {{ detail.asset_name }}
          <v-spacer />
          <v-btn icon variant="text" @click="detailOpen = false"><v-icon>mdi-close</v-icon></v-btn>
        </v-card-title>
        <v-divider />
        <v-card-text>
          <v-row dense>
            <v-col cols="6">
              <div class="text-caption text-medium-emphasis">所属项目</div>
              <div class="font-monospace">{{ detail.project_code }}</div>
            </v-col>
            <v-col cols="6">
              <div class="text-caption text-medium-emphasis">环节</div>
              <div class="font-monospace">{{ detail.stage_code }}</div>
            </v-col>
            <v-col cols="6">
              <div class="text-caption text-medium-emphasis">文件版本</div>
              <div class="font-monospace text-caption">{{ detail.file_version_code }}</div>
            </v-col>
            <v-col cols="6">
              <div class="text-caption text-medium-emphasis">敏感等级</div>
              <v-chip :color="sensColor(detail.sensitivity_level)" size="x-small" variant="tonal">
                {{ sensLabel(detail.sensitivity_level) }}
              </v-chip>
            </v-col>
            <v-col cols="6">
              <div class="text-caption text-medium-emphasis">生命周期状态</div>
              <v-chip :color="statusColor(detail.lifecycle_status)" size="x-small" variant="tonal">
                {{ statusLabel(detail.lifecycle_status) }}
              </v-chip>
            </v-col>
            <v-col cols="6">
              <div class="text-caption text-medium-emphasis">标识方式</div>
              <div>{{ markingLabel(detail.marking_method) }}</div>
            </v-col>
            <v-col cols="12">
              <div class="text-caption text-medium-emphasis">当前存储位置</div>
              <div class="font-monospace text-caption text-break">{{ detail.current_storage_uri || '-' }}</div>
            </v-col>
            <v-col cols="12" v-if="detail.source_ref">
              <div class="text-caption text-medium-emphasis">来源说明</div>
              <div v-if="parsedSourceRef" class="mt-1">
                <div class="text-body-2">
                  <v-chip size="x-small" color="info" variant="tonal" class="mr-1">
                    {{ sourceRefKindLabel(parsedSourceRef.received_via || parsedSourceRef.derive_kind) }}
                  </v-chip>
                  <span v-if="parsedSourceRef.upstream_file_version_code" class="font-monospace text-caption">
                    {{ parsedSourceRef.upstream_file_version_code }}
                  </span>
                </div>
                <v-expansion-panels variant="accordion" class="mt-1" density="compact">
                  <v-expansion-panel>
                    <v-expansion-panel-title class="text-caption">查看原始 JSON（开发/审计）</v-expansion-panel-title>
                    <v-expansion-panel-text>
                      <pre class="text-caption" style="white-space: pre-wrap; word-break: break-all">{{ detail.source_ref }}</pre>
                    </v-expansion-panel-text>
                  </v-expansion-panel>
                </v-expansion-panels>
              </div>
              <pre v-else class="text-caption" style="white-space: pre-wrap; word-break: break-all">{{ detail.source_ref }}</pre>
            </v-col>
          </v-row>

          <v-divider class="my-3" />

          <div class="d-flex align-center mb-2">
            <strong>合法状态切换</strong>
          </div>
          <!-- 项目归档后底账只读 -->
          <v-alert
            v-if="detailProjectReadOnly"
            type="info"
            density="compact"
            variant="tonal"
            icon="mdi-lock-outline"
            class="mb-2"
          >
            项目{{ detailProjectStatusLabel }}，本底账已封存，状态不可手动切换。
          </v-alert>
          <!-- 当前状态显式标出，再展示"可去向" -->
          <div v-else>
            <div class="d-flex align-center mb-2 text-caption">
              <span class="text-medium-emphasis mr-1">当前：</span>
              <v-chip :color="statusColor(detail.lifecycle_status)" size="small" variant="tonal" class="mr-2">
                {{ statusLabel(detail.lifecycle_status) }}
              </v-chip>
              <span class="text-medium-emphasis">→ 可切换至：</span>
            </div>
            <div class="d-flex flex-wrap gap-2">
              <v-btn
                v-for="next in allowedTransitions(detail.lifecycle_status)"
                :key="next"
                size="small"
                variant="tonal"
                :color="statusColor(next)"
                prepend-icon="mdi-arrow-right-bold"
                @click="confirmTransition(next)"
              >
                {{ statusLabel(next) }}
              </v-btn>
              <span v-if="allowedTransitions(detail.lifecycle_status).length === 0" class="text-caption text-medium-emphasis">
                终态，无后续状态
              </span>
            </div>
          </div>

          <v-divider class="my-3" />

          <!-- V5-P1 Task 8: 解绑 + 重新归类 -->
          <div v-if="!detailProjectReadOnly && canUnbind(detail)">
            <div class="d-flex align-center mb-2">
              <strong>归目调整</strong>
            </div>
            <div class="d-flex flex-wrap gap-2">
              <v-btn
                size="small"
                variant="tonal"
                color="warning"
                prepend-icon="mdi-link-off"
                @click="openUnbind(detail)"
              >解绑</v-btn>
              <v-btn
                size="small"
                variant="tonal"
                color="secondary"
                prepend-icon="mdi-folder-swap-outline"
                @click="openReclassify(detail)"
              >重新归类</v-btn>
            </div>
            <v-divider class="my-3" />
          </div>

          <!-- V2-7: 三主体过户 -->
          <div v-if="!detailProjectReadOnly && canHandover(detail.lifecycle_status)">
            <div class="d-flex align-center mb-2">
              <strong>三主体过户</strong>
            </div>
            <div class="d-flex flex-wrap gap-2">
              <v-btn size="small" variant="tonal" color="warning" prepend-icon="mdi-account-switch" @click="openHandover('owner')">
                过户归属主体
              </v-btn>
              <v-btn size="small" variant="tonal" color="warning" prepend-icon="mdi-account-switch" @click="openHandover('custodian')">
                过户保管主体
              </v-btn>
              <v-btn size="small" variant="tonal" color="warning" prepend-icon="mdi-account-switch" @click="openHandover('security')">
                过户安全主体
              </v-btn>
            </div>

            <v-divider class="my-3" />
          </div>

          <div class="d-flex align-center mb-2">
            <strong>生命周期事件流</strong>
            <v-spacer />
            <v-chip size="x-small" variant="tonal">{{ detailEvents.length }} 条</v-chip>
          </div>
          <v-timeline density="compact" side="end" v-if="detailEvents.length">
            <v-timeline-item
              v-for="ev in detailEvents"
              :key="ev.id"
              :dot-color="eventColor(ev.event_type)"
              size="x-small"
            >
              <div class="d-flex align-center">
                <strong class="mr-2">{{ ev.event_name }}</strong>
                <v-chip size="x-small" variant="tonal" :color="eventColor(ev.event_type)">{{ ev.event_type }}</v-chip>
              </div>
              <div class="text-caption text-medium-emphasis">
                {{ formatTime(ev.create_time) }} · {{ ev.operator_id || '-' }}
              </div>
              <div v-if="ev.reason" class="text-caption">{{ ev.reason }}</div>
              <div v-if="ev.approval_ref" class="text-caption">审批：{{ ev.approval_ref }}</div>
            </v-timeline-item>
          </v-timeline>
          <div v-else class="text-center text-medium-emphasis py-4">暂无事件</div>
        </v-card-text>
      </v-card>
    </v-dialog>

    <!-- 状态切换确认 -->
    <v-dialog v-model="transitionOpen" max-width="480">
      <v-card>
        <v-card-title>切换至 {{ statusLabel(transitionTarget) }}</v-card-title>
        <v-card-text>
          <v-textarea v-model="transitionReason" label="变更原因（必填）" rows="3" density="compact" />
          <v-text-field v-model="transitionApproval" label="审批/凭证编号（可选）" density="compact" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="transitionOpen = false">取消</v-btn>
          <v-btn color="primary" :disabled="!transitionReason.trim()" :loading="submittingTransition" @click="doTransition">确认</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- V2-7: 三主体过户 -->
    <v-dialog v-model="handoverOpen" max-width="520">
      <v-card>
        <v-card-title>
          <v-icon class="mr-2" color="warning">mdi-account-switch</v-icon>
          {{ handoverKindLabel(handoverKind) }}
        </v-card-title>
        <v-card-text>
          <div class="text-caption text-medium-emphasis mb-2">
            当前 {{ handoverKindLabel(handoverKind).replace('过户', '') }}：
            <strong>{{ handoverCurrentSubjectLabel }}</strong>
          </div>
          <v-select
            v-model="handoverToSubjectID"
            :items="handoverTargetOptions"
            label="过户至 *"
            density="compact"
            variant="outlined"
            hide-details
            class="mb-3"
          />
          <v-textarea v-model="handoverReason" label="过户原因（必填）" rows="3" density="compact" variant="outlined" />
          <v-text-field v-model="handoverApproval" label="审批/凭证编号（可选）" density="compact" variant="outlined" class="mt-2" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="handoverOpen = false">取消</v-btn>
          <v-btn
            color="warning"
            :disabled="!handoverToSubjectID || !handoverReason.trim()"
            :loading="submittingHandover"
            @click="doHandover"
          >
            确认过户
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 导出成功提示弹窗（替代纯 toast，让用户能看到并复制完整路径） -->
    <v-dialog v-model="exportResult.show" max-width="540">
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2" color="success">mdi-microsoft-excel</v-icon>
          导出成功
        </v-card-title>
        <v-card-text>
          <div class="mb-2">
            已导出 <strong>{{ exportResult.count }}</strong> 条底账记录到下载目录：
          </div>
          <v-card variant="tonal" class="pa-2">
            <div class="d-flex align-start">
              <v-icon class="mr-2 mt-1" size="small">mdi-file-excel-outline</v-icon>
              <div class="flex-grow-1" style="min-width: 0">
                <div class="font-weight-medium text-body-2">{{ exportResult.filename }}</div>
                <div class="text-caption font-monospace text-medium-emphasis text-break" style="word-break: break-all">
                  {{ exportResult.path }}
                </div>
                <div class="text-caption mt-1">
                  {{ (exportResult.size / 1024).toFixed(1) }} KB
                </div>
              </div>
            </div>
          </v-card>
          <div class="text-caption text-medium-emphasis mt-2">
            可以在 Finder / 文件管理器中打开此路径，或用 Excel / Numbers 直接打开。
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" prepend-icon="mdi-content-copy" @click="copyExportPath">复制路径</v-btn>
          <v-btn variant="text" @click="exportResult.show = false">关闭</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- V5-P1 Task 8: 解绑对话框 -->
    <v-dialog v-model="unbindDialog" max-width="500">
      <v-card>
        <v-card-title>
          <v-icon class="mr-2" color="warning">mdi-link-off</v-icon>
          解除绑定
        </v-card-title>
        <v-card-text>
          <div class="text-caption text-medium-emphasis mb-2">
            底账：<span class="font-monospace">{{ unbindItem?.file_version_code }}</span>
            <span v-if="unbindItem"> · 级别：{{ sensLabel(unbindItem.sensitivity_level) }}</span>
          </div>
          <v-alert type="warning" variant="tonal" density="compact" class="mb-2">
            解绑后该底账被标记 cancelled，可在审计中追溯，但不再参与统计。
          </v-alert>
          <v-textarea
            v-model="unbindReason"
            label="解绑原因（必填）"
            rows="3"
            density="compact"
            variant="outlined"
            autofocus
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="unbinding" @click="unbindDialog = false">取消</v-btn>
          <v-btn
            color="warning"
            variant="elevated"
            :loading="unbinding"
            :disabled="!unbindReason.trim()"
            @click="onUnbind"
          >确认解绑</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- V5-P1 Task 8: 重新归类对话框 -->
    <v-dialog v-model="reclassifyDialog" max-width="600">
      <v-card>
        <v-card-title>
          <v-icon class="mr-2" color="secondary">mdi-folder-swap-outline</v-icon>
          重新归类
        </v-card-title>
        <v-card-text>
          <div class="text-caption text-medium-emphasis mb-2">
            底账：<span class="font-monospace">{{ reclassifyItem?.file_version_code }}</span>
          </div>
          <v-alert type="info" variant="tonal" density="compact" class="mb-2">
            将解绑原 fv 并把同一资源桥接到新目标，原 fv 标记为 cancelled。
          </v-alert>
          <v-select
            v-model="reclassifyForm.project_id"
            :items="projectOptionsForReclassify"
            item-title="label"
            item-value="value"
            label="新项目"
            density="compact"
            variant="outlined"
            class="mb-2"
            :loading="loadingProjectsForReclassify"
            @update:modelValue="onReclassifyProjectChange"
          />
          <v-select
            v-model="reclassifyForm.stage_code"
            :items="reclassifyStageOptions"
            item-title="label"
            item-value="value"
            label="新环节"
            density="compact"
            variant="outlined"
            :disabled="!reclassifyForm.project_id"
            class="mb-2"
          />
          <v-select
            v-model="reclassifyForm.file_rule_code"
            :items="reclassifyRuleOptions"
            item-title="label"
            item-value="value"
            label="新文件规则"
            density="compact"
            variant="outlined"
            :disabled="!reclassifyForm.stage_code"
            class="mb-2"
          />
          <v-textarea
            v-model="reclassifyReason"
            label="重新归类原因（必填）"
            rows="2"
            density="compact"
            variant="outlined"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="reclassifying" @click="reclassifyDialog = false">取消</v-btn>
          <v-btn
            color="primary"
            variant="elevated"
            :loading="reclassifying"
            :disabled="!canReclassify"
            @click="onReclassify"
          >确认重新归类</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="3000">
      {{ snackbar.text }}
    </v-snackbar>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ledgersApi, projectsApi, subjectsApi, type AssetLedger, type LifecycleEvent, type DataProject, type Subject } from '@/services/projectsApi'

const API_BASE = 'http://127.0.0.1:3001'
const route = useRoute()

const list = ref<AssetLedger[]>([])
const loading = ref(false)
const projects = ref<DataProject[]>([])
const filters = ref<{
  project_code: string
  lifecycle_status: string
  sensitivity_level: string
  stage_code: string
  keyword: string
}>({ project_code: '', lifecycle_status: '', sensitivity_level: '', stage_code: '', keyword: '' })

// 默认隐藏 planned 草稿（立项时为每条规则建的"应当存在的资产"占位）
// 切到"包含草稿"后能看到全量底账，便于审计/盘点
const includeDrafts = ref(false)

// 导出结果展示用
const exportResult = ref<{
  show: boolean
  path: string
  filename: string
  size: number
  count: number
}>({ show: false, path: '', filename: '', size: 0, count: 0 })
const draftCount = computed(() => list.value.filter(l => l.lifecycle_status === 'planned').length)

// 实际渲染列表 = 后端返回的 list 经过"是否含草稿"开关过滤
// 用户主动选了 lifecycle_status=planned 时，开关失效（用户意图明确）
const displayList = computed(() => {
  if (includeDrafts.value || filters.value.lifecycle_status === 'planned') {
    return list.value
  }
  return list.value.filter(l => l.lifecycle_status !== 'planned')
})

// 当前底账所属项目是否已归档/取消（决定是否隐藏状态切换按钮）
const detailProjectReadOnly = computed(() => {
  if (!detail.value) return false
  const p = projects.value.find(p => p.project_code === detail.value!.project_code)
  if (!p) return false
  return p.status === 'archived' || p.status === 'cancelled'
})
const detailProjectStatusLabel = computed(() => {
  if (!detail.value) return ''
  const p = projects.value.find(p => p.project_code === detail.value!.project_code)
  if (!p) return ''
  return ({ archived: '已结项归档', cancelled: '已取消' } as Record<string, string>)[p.status] || ''
})

// 解析 detail.source_ref（JSON 字符串）为对象，给 UI 友好渲染
const parsedSourceRef = computed<Record<string, any> | null>(() => {
  if (!detail.value?.source_ref) return null
  try {
    return JSON.parse(detail.value.source_ref)
  } catch {
    return null
  }
})

// 把 source_ref 里的 received_via / derive_kind 翻译成中文
function sourceRefKindLabel(kind: string | undefined): string {
  if (!kind) return '其他'
  return ({
    downstream_pickup: '下游领取上游产出',
    process: '派生为过程文件',
    output: '派生为产出文件',
    input: '派生为输入文件',
    new_version: '新版本',
  } as Record<string, string>)[kind] || kind
}
const snackbar = ref({ show: false, text: '', color: 'success' })

const detail = ref<AssetLedger | null>(null)
const detailOpen = ref(false)
const detailEvents = ref<LifecycleEvent[]>([])

const transitionOpen = ref(false)
const transitionTarget = ref<string>('')
const transitionReason = ref('')
const transitionApproval = ref('')
const submittingTransition = ref(false)

// V2-7: 过户对话框状态
const subjects = ref<Subject[]>([])
const handoverOpen = ref(false)
const handoverKind = ref<'owner' | 'custodian' | 'security'>('custodian')
const handoverToSubjectID = ref<number | null>(null)
const handoverReason = ref('')
const handoverApproval = ref('')
const submittingHandover = ref(false)

function canHandover(status: string): boolean {
  return status === 'registered' || status === 'in_use'
}
function handoverKindLabel(kind: string): string {
  return ({ owner: '过户归属主体', custodian: '过户保管主体', security: '过户安全主体' } as Record<string, string>)[kind] || '主体过户'
}
const handoverCurrentSubjectID = computed(() => {
  if (!detail.value) return 0
  return ({
    owner: detail.value.owner_subject_id,
    custodian: detail.value.custodian_subject_id,
    security: detail.value.security_subject_id,
  } as Record<string, number>)[handoverKind.value] || 0
})
const handoverCurrentSubjectLabel = computed(() => {
  const s = subjects.value.find(x => x.id === handoverCurrentSubjectID.value)
  return s ? `${s.code} ${s.name}` : '-'
})
const handoverTargetOptions = computed(() =>
  subjects.value
    .filter(s => s.id !== handoverCurrentSubjectID.value)
    .map(s => ({ title: `${s.code} ${s.name}`, value: s.id }))
)

function openHandover(kind: 'owner' | 'custodian' | 'security') {
  handoverKind.value = kind
  handoverToSubjectID.value = null
  handoverReason.value = ''
  handoverApproval.value = ''
  handoverOpen.value = true
}

async function doHandover() {
  if (!detail.value || !handoverToSubjectID.value || !handoverReason.value.trim()) return
  submittingHandover.value = true
  try {
    const updated = await ledgersApi.handover(
      detail.value.id,
      handoverKind.value,
      handoverToSubjectID.value,
      handoverReason.value.trim(),
      handoverApproval.value.trim() || undefined,
    )
    detail.value = updated
    detailEvents.value = await ledgersApi.events(detail.value.id)
    handoverOpen.value = false
    snackbar.value = { show: true, text: `${handoverKindLabel(handoverKind.value)}成功`, color: 'success' }
    await loadList()
  } catch (e: any) {
    snackbar.value = { show: true, text: '过户失败：' + e.message, color: 'error' }
  } finally {
    submittingHandover.value = false
  }
}

const headers = [
  { title: '底账编号', key: 'ledger_code' },
  { title: '资产名称', key: 'asset_name' },
  { title: '所属项目', key: 'project_code' },
  { title: '环节', key: 'stage_code' },
  { title: '文件版本', key: 'file_version_code' },
  { title: '敏感等级', key: 'sensitivity_level' },
  { title: '生命周期', key: 'lifecycle_status' },
  { title: '操作', key: 'actions', sortable: false, width: 100 },
]

const statusOptions = [
  { title: '草稿', value: 'planned' },
  { title: '已入账', value: 'registered' },
  { title: '使用中', value: 'in_use' },
  { title: '已封存', value: 'sealed' },
  { title: '已销账', value: 'destroyed' },
  { title: '永存', value: 'permanent' },
]
const sensitivityOptions = [
  { title: '一般', value: 'general' },
  { title: '重要', value: 'important' },
  { title: '核心(涉密)', value: 'core_secret' },
]
const projectOptions = ref<{ title: string; value: string }[]>([])

function statusLabel(s: string): string {
  return ({
    planned: '草稿',
    registered: '已入账',
    in_use: '使用中',
    sealed: '已封存',
    destroyed: '已销账',
    permanent: '永存',
  } as Record<string, string>)[s] || s
}
function statusColor(s: string): string {
  return ({
    planned: 'default',
    registered: 'success',
    in_use: 'info',
    sealed: 'warning',
    destroyed: 'error',
    permanent: 'purple',
  } as Record<string, string>)[s] || 'default'
}
function sensLabel(s: string): string {
  return ({ general: '一般', important: '重要', core_secret: '核心(涉密)' } as Record<string, string>)[s] || s
}
function sensColor(s: string): string {
  return ({ general: 'default', important: 'warning', core_secret: 'error' } as Record<string, string>)[s] || 'default'
}
function markingLabel(m: string): string {
  return ({ reference: '引用式', embedded: '内嵌式', hybrid: '混合式' } as Record<string, string>)[m] || m
}
function eventColor(t: string): string {
  return ({
    register: 'success',
    use: 'info',
    transfer: 'primary',
    change: 'warning',
    handover: 'primary',
    archive: 'orange',
    destroy: 'error',
    permanent: 'purple',
  } as Record<string, string>)[t] || 'default'
}
function formatTime(t: string | null): string {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

// 客户端镜像后端 ValidStateTransition，避免试探后端报错
function allowedTransitions(from: string): string[] {
  return ({
    planned: ['registered'],
    registered: ['in_use', 'sealed'],
    in_use: ['registered', 'sealed'],
    sealed: ['destroyed', 'permanent'],
    destroyed: [],
    permanent: [],
  } as Record<string, string[]>)[from] || []
}

async function loadList() {
  loading.value = true
  try {
    list.value = await ledgersApi.search({
      project_code: filters.value.project_code || undefined,
      lifecycle_status: filters.value.lifecycle_status || undefined,
      sensitivity_level: filters.value.sensitivity_level || undefined,
      stage_code: filters.value.stage_code || undefined,
      keyword: filters.value.keyword || undefined,
    })
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载失败：' + e.message, color: 'error' }
  } finally {
    loading.value = false
  }
}

async function loadProjects() {
  try {
    projects.value = await projectsApi.list()
    projectOptions.value = projects.value.map(p => ({
      title: `${p.project_code} ${p.project_name}`,
      value: p.project_code,
    }))
  } catch {
    // ignore
  }
}

async function openDetail(item: AssetLedger) {
  detail.value = item
  detailOpen.value = true
  try {
    detailEvents.value = await ledgersApi.events(item.id)
  } catch (e: any) {
    detailEvents.value = []
    snackbar.value = { show: true, text: '事件流加载失败：' + e.message, color: 'warning' }
  }
}

function confirmTransition(to: string) {
  transitionTarget.value = to
  transitionReason.value = ''
  transitionApproval.value = ''
  transitionOpen.value = true
}

async function doTransition() {
  if (!detail.value) return
  if (!transitionReason.value.trim()) return
  submittingTransition.value = true
  try {
    const updated = await ledgersApi.transition(
      detail.value.id,
      transitionTarget.value,
      transitionReason.value.trim(),
      transitionApproval.value.trim() || undefined,
    )
    detail.value = updated
    detailEvents.value = await ledgersApi.events(detail.value.id)
    transitionOpen.value = false
    snackbar.value = { show: true, text: `已切换至 ${statusLabel(transitionTarget.value)}`, color: 'success' }
    // 同步刷新列表
    await loadList()
  } catch (e: any) {
    snackbar.value = { show: true, text: '切换失败：' + e.message, color: 'error' }
  } finally {
    submittingTransition.value = false
  }
}

// 导出 XLSX：让后端直接写到用户 ~/Downloads/，返回完整路径
//
// 之前用 fetch+blob+<a download> 在 Wails WebView 不能稳定弹保存对话框，
// 用户也不知道文件去哪了。改成"后端写盘 + 返回路径"模型，前端 toast 显示
// 完整路径，给"复制路径"和"打开文件夹"按钮。
async function exportXlsx() {
  if (displayList.value.length === 0) {
    snackbar.value = { show: true, text: '当前无数据可导出', color: 'warning' }
    return
  }
  const q = new URLSearchParams()
  if (filters.value.project_code) q.set('project_code', filters.value.project_code)
  if (filters.value.lifecycle_status) q.set('lifecycle_status', filters.value.lifecycle_status)
  if (filters.value.sensitivity_level) q.set('sensitivity_level', filters.value.sensitivity_level)
  if (filters.value.stage_code) q.set('stage_code', filters.value.stage_code)
  if (filters.value.keyword) q.set('keyword', filters.value.keyword)
  if (includeDrafts.value || filters.value.lifecycle_status === 'planned') {
    q.set('include_drafts', '1')
  }
  try {
    const res = await fetch(`http://127.0.0.1:3001/ledgers/export-to-downloads?${q.toString()}`, {
      method: 'POST',
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || '导出失败')
    exportResult.value = {
      show: true,
      path: json.data.path,
      filename: json.data.filename,
      size: json.data.size,
      count: json.data.count,
    }
  } catch (e: any) {
    console.error('[export] failed:', e)
    snackbar.value = { show: true, text: '导出失败：' + (e?.message || e), color: 'error' }
  }
}

// V3-8 §7.5 + §8.4 底账 CSV / JSON 导出
//
// 复用 XLSX 那条"后端写盘 + 返回完整路径"模型：通过
// /ledgers/export-to-downloads?format=csv|json 让后端写到用户 Downloads，
// 然后用同一个 exportResult 弹窗显示完整路径 + "复制路径"按钮。
// 与 XLSX 唯一区别就是 format 参数。
async function exportFormat(format: 'csv' | 'json') {
  if (displayList.value.length === 0) {
    snackbar.value = { show: true, text: '当前无数据可导出', color: 'warning' }
    return
  }
  const q = new URLSearchParams()
  q.set('format', format)
  if (filters.value.project_code) q.set('project_code', filters.value.project_code)
  if (filters.value.lifecycle_status) q.set('lifecycle_status', filters.value.lifecycle_status)
  if (filters.value.sensitivity_level) q.set('sensitivity_level', filters.value.sensitivity_level)
  if (filters.value.stage_code) q.set('stage_code', filters.value.stage_code)
  if (filters.value.keyword) q.set('keyword', filters.value.keyword)
  if (includeDrafts.value || filters.value.lifecycle_status === 'planned') {
    q.set('include_drafts', '1')
  }
  try {
    const res = await fetch(`http://127.0.0.1:3001/ledgers/export-to-downloads?${q.toString()}`, {
      method: 'POST',
    })
    const json = await res.json()
    if (!json.success) throw new Error(json.error || '导出失败')
    exportResult.value = {
      show: true,
      path: json.data.path,
      filename: json.data.filename,
      size: json.data.size,
      count: json.data.count,
    }
  } catch (e: any) {
    console.error('[export]', format, 'failed:', e)
    snackbar.value = { show: true, text: '导出失败：' + (e?.message || e), color: 'error' }
  }
}

async function copyExportPath() {
  if (!exportResult.value.path) return
  try {
    await navigator.clipboard.writeText(exportResult.value.path)
    snackbar.value = { show: true, text: '路径已复制到剪贴板', color: 'success' }
  } catch {
    const ta = document.createElement('textarea')
    ta.value = exportResult.value.path
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    snackbar.value = { show: true, text: '路径已复制到剪贴板', color: 'success' }
  }
}

// 旧 CSV 导出保留（万一 XLSX 在 wails 不工作时备用，但工具栏入口已改 XLSX）
// eslint-disable-next-line @typescript-eslint/no-unused-vars
function exportCsv() {
  // 用当前可见列表（已尊重"包含草稿"开关），用户看到什么就导什么
  const exportList = displayList.value
  if (exportList.length === 0) {
    snackbar.value = { show: true, text: '当前无数据可导出', color: 'warning' }
    return
  }
  const cols: { key: keyof AssetLedger; label: string }[] = [
    { key: 'ledger_code', label: '底账编号' },
    { key: 'asset_name', label: '资产名称' },
    { key: 'project_code', label: '项目编码' },
    { key: 'stage_code', label: '环节' },
    { key: 'file_version_code', label: '文件版本编码' },
    { key: 'sensitivity_level', label: '敏感等级' },
    { key: 'marking_method', label: '标识方式' },
    { key: 'lifecycle_status', label: '生命周期' },
    { key: 'current_storage_uri', label: '存储位置' },
  ]
  const escape = (v: any) => {
    if (v === null || v === undefined) return ''
    const s = String(v).replace(/"/g, '""')
    return `"${s}"`
  }
  const lines = [cols.map(c => c.label).join(',')]
  for (const r of exportList) {
    lines.push(cols.map(c => escape((r as any)[c.key])).join(','))
  }
  // 加 BOM 让 Excel 正确识别 UTF-8
  const blob = new Blob(['﻿' + lines.join('\n')], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `ledgers-${new Date().toISOString().slice(0, 10)}.csv`
  a.click()
  URL.revokeObjectURL(url)
}

async function loadSubjects() {
  try {
    subjects.value = await subjectsApi.list()
  } catch (e: any) {
    console.warn('[ledger] 加载主体列表失败：', e?.message || e)
  }
}

// V5-P1 Task 8: 解绑 + 重新归类
// §4.3-4 项目版本文件手动归目归档：仅 registered/in_use/sealed 可解绑/重归类
interface ProjectOpt { label: string; value: number }
interface StageOpt { label: string; value: string }
interface RuleOpt { label: string; value: string }

const unbindDialog = ref(false)
const unbindItem = ref<AssetLedger | null>(null)
const unbindReason = ref('')
const unbinding = ref(false)

const reclassifyDialog = ref(false)
const reclassifyItem = ref<AssetLedger | null>(null)
const reclassifyForm = ref({ project_id: 0, stage_code: '', file_rule_code: '' })
const reclassifyReason = ref('')
const reclassifying = ref(false)
const projectOptionsForReclassify = ref<ProjectOpt[]>([])
const loadingProjectsForReclassify = ref(false)
const reclassifyStagesData = ref<Array<{ stage_code: string; stage_name: string; rules: Array<{ file_rule_code: string; file_name: string; data_state: string }> }>>([])

const reclassifyStageOptions = computed<StageOpt[]>(() =>
  reclassifyStagesData.value.map(s => ({ label: `${s.stage_code} ${s.stage_name}`, value: s.stage_code }))
)
const reclassifyRuleOptions = computed<RuleOpt[]>(() => {
  const stage = reclassifyStagesData.value.find(s => s.stage_code === reclassifyForm.value.stage_code)
  if (!stage) return []
  return stage.rules.map(r => ({ label: `${r.file_rule_code} ${r.file_name}`, value: r.file_rule_code }))
})
const canReclassify = computed(() =>
  !!reclassifyForm.value.project_id &&
  !!reclassifyForm.value.stage_code &&
  !!reclassifyForm.value.file_rule_code &&
  !!reclassifyReason.value.trim()
)

// 仅这 3 个状态可解绑/重归类（cancelled/destroyed/permanent 不可）
function canUnbind(item: AssetLedger | null): boolean {
  return !!item && ['registered', 'in_use', 'sealed'].includes(item.lifecycle_status)
}

function openUnbind(item: AssetLedger) {
  unbindItem.value = item
  unbindReason.value = ''
  unbindDialog.value = true
}

async function onUnbind() {
  if (!unbindItem.value || !unbindReason.value.trim()) return
  unbinding.value = true
  try {
    const res = await fetch(`${API_BASE}/file-versions/${unbindItem.value.file_version_id}/unbind`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason: unbindReason.value.trim() }),
    })
    const json = await res.json()
    if (json.success) {
      snackbar.value = { show: true, text: '已解绑', color: 'success' }
      unbindDialog.value = false
      detailOpen.value = false
      await loadList()
    } else {
      snackbar.value = { show: true, text: '解绑失败：' + (json.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '解绑失败：' + e.message, color: 'error' }
  } finally {
    unbinding.value = false
  }
}

function openReclassify(item: AssetLedger) {
  reclassifyItem.value = item
  reclassifyForm.value = { project_id: 0, stage_code: '', file_rule_code: '' }
  reclassifyReason.value = ''
  reclassifyStagesData.value = []
  reclassifyDialog.value = true
  if (projectOptionsForReclassify.value.length === 0) loadProjectOptionsForReclassify()
}

async function loadProjectOptionsForReclassify() {
  loadingProjectsForReclassify.value = true
  try {
    const res = await fetch(`${API_BASE}/projects?status=active`)
    const json = await res.json()
    if (json.success) {
      const list = Array.isArray(json.data) ? json.data : (json.data?.items || [])
      projectOptionsForReclassify.value = list.map((p: any) => ({
        label: `${p.project_code} ${p.project_name}`,
        value: p.id,
      }))
    } else {
      snackbar.value = { show: true, text: '加载项目失败：' + (json.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载项目失败：' + e.message, color: 'error' }
  } finally {
    loadingProjectsForReclassify.value = false
  }
}

async function onReclassifyProjectChange(projectId: number) {
  reclassifyForm.value.stage_code = ''
  reclassifyForm.value.file_rule_code = ''
  reclassifyStagesData.value = []
  if (!projectId) return
  try {
    const res = await fetch(`${API_BASE}/projects/${projectId}/stages-with-rules`)
    const json = await res.json()
    if (json.success && json.data?.stages) {
      reclassifyStagesData.value = json.data.stages
    } else {
      snackbar.value = { show: true, text: '加载环节失败：' + (json.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '加载环节失败：' + e.message, color: 'error' }
  }
}

watch(() => reclassifyForm.value.stage_code, () => {
  reclassifyForm.value.file_rule_code = ''
})

async function onReclassify() {
  if (!reclassifyItem.value || !canReclassify.value) return
  reclassifying.value = true
  try {
    const res = await fetch(`${API_BASE}/file-versions/${reclassifyItem.value.file_version_id}/reclassify`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        new_project_id: reclassifyForm.value.project_id,
        new_stage_code: reclassifyForm.value.stage_code,
        new_file_rule_code: reclassifyForm.value.file_rule_code,
        reason: reclassifyReason.value.trim(),
      }),
    })
    const json = await res.json()
    if (json.success) {
      snackbar.value = { show: true, text: '已重新归类', color: 'success' }
      reclassifyDialog.value = false
      detailOpen.value = false
      await loadList()
    } else {
      snackbar.value = { show: true, text: '重新归类失败：' + (json.error || ''), color: 'error' }
    }
  } catch (e: any) {
    snackbar.value = { show: true, text: '重新归类失败：' + e.message, color: 'error' }
  } finally {
    reclassifying.value = false
  }
}

onMounted(async () => {
  const projectCode = route.query.project_code
  if (typeof projectCode === 'string') {
    filters.value.project_code = projectCode
  }
  await Promise.all([loadProjects(), loadList(), loadSubjects()])
})
</script>
