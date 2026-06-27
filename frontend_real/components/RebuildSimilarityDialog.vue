<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, type AnalyzePreview } from '../services/api'

const props = defineProps<{
  modelValue: boolean
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm'): void
}>()

const preview = ref<AnalyzePreview | null>(null)
const loading = ref(false)
const errorMsg = ref('')
// 「无需重建」状态下用户主动表态：我仍要强制重建
const forceRebuild = ref(false)
// 全库未处理 suspect 文件数；> 0 时显示警告且要求用户勾「我知晓」才能继续
const suspectCount = ref(0)
const suspectAcknowledged = ref(false)

const fmtDuration = (sec: number): string => {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return m > 0 ? `${m} 分 ${s} 秒` : `${s} 秒`
}

const fmtDate = (iso: string | null): string => {
  if (!iso) return ''
  return new Date(iso).toLocaleString('zh-CN')
}

const estimatedMinutes = (miss: number, lastDurSec: number): number => {
  // perFile = max(2s, lastRun's dur / lastRun's miss count).
  // Since preview API doesn't return last miss count, fall back to 2s/file baseline.
  const perFile = lastDurSec > 0 ? Math.max(2, lastDurSec / Math.max(miss, 1)) : 2
  return Math.max(1, Math.ceil((miss * perFile) / 60))
}

watch(() => props.modelValue, async (open) => {
  if (!open) return
  // 每次弹窗打开都重置勾选，避免上次状态泄漏
  forceRebuild.value = false
  suspectAcknowledged.value = false
  suspectCount.value = 0
  loading.value = true
  errorMsg.value = ''
  preview.value = null
  try {
    // 并行：analyze preview + 全库 suspect 摘要
    // suspect 不传 businessType → 后端返回全库未处理 suspect 数（重建是整库范围的）
    const [pv, sus] = await Promise.all([
      api.analyzePreview(),
      api.getSuspectSummary({}).catch(() => ({ count: 0, sample_paths: [] })),
    ])
    preview.value = pv
    suspectCount.value = sus.count
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}, { immediate: true })

const close = () => emit('update:modelValue', false)
const confirm = () => {
  emit('confirm')
  close()
}
</script>

<template>
  <v-dialog :model-value="modelValue" @update:model-value="close" max-width="520">
    <v-card>
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-refresh</v-icon>
        重建相似关系
      </v-card-title>
      <v-card-text>
        <div v-if="loading" class="d-flex align-center">
          <v-progress-circular indeterminate size="24" class="mr-2" />
          加载中…
        </div>
        <div v-else-if="errorMsg" class="text-error">{{ errorMsg }}</div>
        <template v-else-if="preview">
          <!-- 情况 A：家族表与当前文件集合不一致 → 必须重建（首次未跑过 / 扫描后还没跑） -->
          <div v-if="preview.family_stale" class="text-warning mb-2">
            ⚠ 家族关系待构建（扫描结果或文件集合有变化）
          </div>
          <!-- 情况 B：有特征值需要重算（往往与 family_stale 并存，但单独发生时也要提示） -->
          <template v-if="preview.cache_miss_count > 0">
            <div class="mb-2">
              约 <strong>{{ preview.cache_miss_count }}</strong> 个文件需要重新计算特征
            </div>
            <div class="mb-2">
              预计耗时 ~<strong>{{ estimatedMinutes(preview.cache_miss_count, preview.last_run_duration_sec) }}</strong> 分钟
            </div>
          </template>
          <!-- 情况 C：两者都假 → 真正无需重建 -->
          <div v-if="!preview.family_stale && preview.cache_miss_count === 0" class="text-success mb-2">
            ✓ 无需重算，家族关系已是最新
          </div>
          <div v-if="preview.last_run_at" class="text-caption text-grey mt-2">
            上次重建：{{ fmtDate(preview.last_run_at) }}，耗时 {{ fmtDuration(preview.last_run_duration_sec) }}
          </div>

          <!-- 耗时预防针：所有状态都显示一遍，让用户提前心里有数 -->
          <v-alert
            type="info"
            variant="tonal"
            density="compact"
            class="mt-3"
          >
            <div class="text-caption">
              ⏳ 重建期间扫描端会持续工作。文件较多或包含较多 PDF / Office
              文档时，可能需要数分钟到十几分钟。<strong>请保持「数可信终端」
              窗口运行</strong>，不要关闭或重启，跑完后界面会自动刷新家族关系。
            </div>
          </v-alert>

          <!-- suspect 警告：检测到未处理的疑似非个人文件 → 这些会被纳入重建大幅延长耗时 -->
          <v-alert
            v-if="suspectCount > 0"
            type="warning"
            variant="tonal"
            density="compact"
            class="mt-3"
            data-test="rebuild-suspect-warning"
          >
            <div class="text-caption">
              ⚠ 检测到 <strong>{{ suspectCount }}</strong> 个未处理的疑似非个人
              文件（系统目录 / 二进制 / 字体等）。它们会被纳入本次重建并显著
              拉长计算时间。<br>
              <strong>建议先取消</strong>，回到列表点顶部的「一键标为已忽略」，
              再回来重建。如确需现在重建，请勾选下面的确认项。
            </div>
            <v-checkbox
              v-model="suspectAcknowledged"
              data-test="rebuild-suspect-ack-checkbox"
              density="compact"
              hide-details
              class="mt-1"
            >
              <template #label>
                <span class="text-caption">
                  我知晓本次重建将包含 {{ suspectCount }} 个疑似非个人文件
                </span>
              </template>
            </v-checkbox>
          </v-alert>

          <!-- 情况 C 专属：强制重建复选框 -->
          <v-checkbox
            v-if="!preview.family_stale && preview.cache_miss_count === 0"
            v-model="forceRebuild"
            data-test="rebuild-force-checkbox"
            density="compact"
            hide-details
            class="mt-2"
          >
            <template #label>
              <span class="text-caption">
                我仍要强制重建（已确认家族关系已最新，但仍想重跑一次）
              </span>
            </template>
          </v-checkbox>
        </template>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="close" data-test="rebuild-cancel-btn">取消</v-btn>
        <v-btn
          color="primary"
          data-test="rebuild-confirm-btn"
          :disabled="
            loading
            || !!errorMsg
            || (!preview?.family_stale && preview?.cache_miss_count === 0 && !forceRebuild)
            || (suspectCount > 0 && !suspectAcknowledged)
          "
          @click="confirm"
        >
          开始重建
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
