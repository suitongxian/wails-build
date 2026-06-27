<template>
  <div v-if="project">
    <!-- Header -->
    <v-card flat class="mb-2">
      <v-card-text class="pa-3">
        <!-- 第一行：返回按钮 / 项目标题 / 右侧操作 -->
        <div class="d-flex align-center" style="min-height: 36px">
          <v-btn variant="text" size="small" @click="$router.push('/projects')" class="mr-2 px-2">
            <v-icon size="small">mdi-arrow-left</v-icon>
          </v-btn>
          <!-- 标题区：占满剩余空间，超长用 ellipsis -->
          <div class="flex-grow-1 d-flex align-center" style="min-width: 0">
            <h2
              class="text-h6 font-weight-medium text-truncate"
              :title="project.project_name"
              style="margin: 0; max-width: 100%"
            >
              {{ project.project_name }}
            </h2>
          </div>
          <!-- 右侧操作区：固定不被挤压 -->
          <div class="d-flex align-center flex-shrink-0 ml-3">
            <!-- 项目根目录：图标按钮 + tooltip + 点击复制 -->
            <v-tooltip v-if="project.project_root" location="bottom" max-width="600">
              <template #activator="{ props }">
                <v-btn
                  v-bind="props"
                  variant="text"
                  size="small"
                  icon
                  @click="copyProjectRoot"
                >
                  <v-icon>mdi-folder-outline</v-icon>
                </v-btn>
              </template>
              <div>
                <div class="text-caption mb-1">本项目根目录（点击复制）</div>
                <div class="font-monospace text-break" style="word-break: break-all">
                  {{ project.project_root }}
                </div>
              </div>
            </v-tooltip>
            <v-btn
              v-if="project.status !== 'cancelled'"
              color="success"
              variant="tonal"
              size="small"
              class="ml-1"
              prepend-icon="mdi-folder-arrow-down"
              :loading="quickArchiving"
              @click="onQuickArchive"
            >
              一键归档
            </v-btn>
            <v-btn
              v-if="project.status !== 'archived' && project.status !== 'cancelled'"
              color="warning"
              variant="tonal"
              size="small"
              class="ml-1"
              prepend-icon="mdi-archive-arrow-down"
              @click="openCloseDialog"
            >
              结项归档
            </v-btn>
            <!-- 归档移交按钮：按 sync_status 三态展示（未移交 / 失败可重试 / 已成功不可点） -->
            <v-tooltip location="bottom" max-width="380">
              <template #activator="{ props: tooltipProps }">
                <span v-bind="tooltipProps">
                  <v-btn
                    v-if="project.status === 'archived'"
                    :color="syncBtnColor"
                    variant="tonal"
                    size="small"
                    class="ml-1"
                    :prepend-icon="syncBtnIcon"
                    :loading="syncLoading"
                    :disabled="syncBtnDisabled"
                    @click="onSyncArchive"
                  >
                    {{ syncBtnLabel }}
                  </v-btn>
                </span>
              </template>
              <div>
                <div class="text-caption">{{ syncTooltip }}</div>
                <div v-if="project.synced_at" class="text-caption mt-1">
                  最后移交时间：{{ formatTime(project.synced_at) }}
                </div>
                <div v-if="project.sync_message" class="text-caption mt-1">
                  消息：{{ project.sync_message }}
                </div>
              </div>
            </v-tooltip>
          </div>
        </div>

        <!-- 第二行：元数据 chips（统一 small 字号，可换行） -->
        <div class="d-flex align-center flex-wrap mt-2" style="gap: 6px; padding-left: 40px">
          <span class="text-caption font-monospace text-primary">{{ project.project_code }}</span>
          <v-chip size="x-small" :color="statusColor(project.status)" variant="tonal">
            {{ statusLabel(project.status) }}
          </v-chip>
          <v-chip size="x-small" :color="sensColor(project.sensitivity_level)" variant="tonal">
            {{ sensLabel(project.sensitivity_level) }}
          </v-chip>
          <v-chip size="x-small" variant="text" prepend-icon="mdi-file-document-outline">
            {{ project.template_code }} {{ project.template_version }}
          </v-chip>
          <v-spacer />
          <!-- V5-P1：解绑/重分类后的 cancelled / destroyed fv 默认隐藏，
               审计场景可打开此 toggle 查看全部。 -->
          <v-switch
            v-model="includeCancelled"
            label="显示已取消"
            density="compact"
            hide-details
            color="warning"
            class="ml-3"
          />
        </div>
      </v-card-text>
    </v-card>

    <!-- 结项弹窗 -->
    <v-dialog v-model="closeDialog.show" max-width="860" persistent scrollable>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-archive-arrow-down</v-icon> 项目结项归档
          <v-spacer />
          <span class="text-caption text-medium-emphasis font-monospace">{{ project.project_code }}</span>
        </v-card-title>
        <v-card-subtitle>{{ project.project_name }}</v-card-subtitle>

        <v-card-text style="max-height: 70vh">
          <div v-if="closeDialog.loading" class="text-center py-6">
            <v-progress-circular indeterminate />
          </div>
          <div v-else>
            <div v-if="closeDialog.precheck">
              <!-- 顶部摘要 alert -->
              <v-alert
                v-if="closeDialog.precheck.ok && (closeDialog.precheck.issues || []).length === 0"
                type="success" density="compact" variant="tonal" class="mb-3"
              >预检通过，可以直接结项。</v-alert>
              <v-alert
                v-else-if="closeDialog.precheck.ok"
                type="warning" density="compact" variant="tonal" class="mb-3"
              >
                预检发现 {{ precheckErrorCount }} 个错误、{{ precheckWarningCount }} 个警告。
                错误数为 0，可勾选"强制结项"继续。
              </v-alert>
              <v-alert
                v-else
                type="error" density="compact" variant="tonal" class="mb-3"
              >
                预检未通过：{{ precheckErrorCount }} 个错误、{{ precheckWarningCount }} 个警告。请先处理错误项。
              </v-alert>

              <!-- 严重性筛选 chip -->
              <div v-if="(closeDialog.precheck.issues || []).length > 0" class="d-flex align-center mb-2">
                <span class="text-caption text-medium-emphasis mr-2">筛选：</span>
                <v-chip-group v-model="precheckSeverityFilter" mandatory selected-class="text-primary">
                  <v-chip size="small" value="all" variant="outlined">
                    全部 {{ (closeDialog.precheck.issues || []).length }}
                  </v-chip>
                  <v-chip v-if="precheckErrorCount > 0" size="small" value="error" color="error" variant="outlined">
                    错误 {{ precheckErrorCount }}
                  </v-chip>
                  <v-chip v-if="precheckWarningCount > 0" size="small" value="warning" color="warning" variant="outlined">
                    警告 {{ precheckWarningCount }}
                  </v-chip>
                </v-chip-group>
              </div>

              <!-- 按 code 折叠分组（同类问题往往很多，折叠可显著节省空间） -->
              <v-expansion-panels
                v-if="(closeDialog.precheck.issues || []).length > 0"
                v-model="precheckExpandedGroups"
                variant="accordion"
                multiple
                class="mb-3"
              >
                <v-expansion-panel
                  v-for="g in groupedPrecheckIssues"
                  :key="g.code"
                  :value="g.code"
                >
                  <v-expansion-panel-title>
                    <div class="d-flex align-center" style="width: 100%">
                      <v-icon
                        :color="g.severity === 'error' ? 'error' : 'warning'"
                        class="mr-2"
                        size="small"
                      >
                        {{ g.severity === 'error' ? 'mdi-alert-circle' : 'mdi-alert' }}
                      </v-icon>
                      <span class="text-body-2 font-weight-medium">{{ codeLabel(g.code) }}</span>
                      <v-chip size="x-small" class="ml-2" :color="g.severity === 'error' ? 'error' : 'warning'" variant="tonal">
                        {{ g.items.length }}
                      </v-chip>
                      <span class="text-caption text-medium-emphasis ml-2 font-monospace">{{ g.code }}</span>
                    </div>
                  </v-expansion-panel-title>
                  <v-expansion-panel-text>
                    <div
                      v-for="(it, i) in g.items"
                      :key="i"
                      class="text-body-2 py-1 px-2 my-1 rounded"
                      :style="{
                        backgroundColor: g.severity === 'error' ? 'rgba(244,67,54,0.05)' : 'rgba(255,152,0,0.05)',
                        wordBreak: 'break-all',
                        whiteSpace: 'normal'
                      }"
                    >
                      <v-tooltip location="top" max-width="600">
                        <template #activator="{ props }">
                          <span v-bind="props">{{ it.message }}</span>
                        </template>
                        <div style="word-break: break-all; white-space: normal">
                          <div class="text-caption mb-1">{{ codeLabel(it.code) }}（{{ it.code }}）</div>
                          <div>{{ it.message }}</div>
                        </div>
                      </v-tooltip>
                    </div>
                  </v-expansion-panel-text>
                </v-expansion-panel>
              </v-expansion-panels>

              <v-divider class="my-3" />
              <v-textarea v-model="closeDialog.reason" label="结项说明（写入 manifest）" rows="2" density="compact" />
              <v-checkbox
                v-if="closeDialog.precheck.ok && (closeDialog.precheck.issues || []).length > 0"
                v-model="closeDialog.force"
                label="强制结项（已知悉所有警告）"
                density="compact"
                hide-details
              />
            </div>
            <div v-if="closeDialog.error" class="text-error text-caption mt-2">{{ closeDialog.error }}</div>
            <div v-if="closeDialog.result" class="mt-2">
              <v-alert type="success" density="compact" variant="tonal">
                结项完成。manifest 已生成：
                <div class="font-monospace text-break">{{ closeDialog.result.manifest_path }}</div>
                <div class="text-caption mt-1">SHA-256 {{ closeDialog.result.manifest_sha256.substring(0, 32) }}…</div>
                <div class="text-caption">
                  文件 {{ closeDialog.result.file_count }} · 底账 {{ closeDialog.result.ledger_count }} · 事件 {{ closeDialog.result.event_count }}
                </div>
              </v-alert>
            </div>
          </div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="closeDialog.show = false">关闭</v-btn>
          <v-btn
            v-if="closeDialog.precheck && !closeDialog.result"
            color="warning"
            :disabled="!canSubmitClose"
            :loading="closeDialog.submitting"
            @click="onSubmitClose"
          >
            执行结项
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 只读模式 banner：项目已 archived/cancelled 时显式提示 -->
    <v-alert
      v-if="isReadOnly"
      type="info"
      variant="tonal"
      density="compact"
      class="mb-2"
      icon="mdi-lock-outline"
    >
      <div class="d-flex align-center">
        <div>
          <strong>只读模式</strong>：项目{{ readOnlyReason }}，所有文件版本与底账已封存。
          可以查看历史、导出 manifest，但不能再上传 / 派生 / 提交 / 领取 / 切换状态。
        </div>
      </div>
    </v-alert>

    <v-row no-gutters class="flex-nowrap" style="height: calc(100vh - 220px)">
      <!-- 左侧：工作环节 -->
      <v-col cols="2" class="border-r" style="min-width: 180px; max-width: 240px; overflow-y: auto">
        <v-list density="compact" nav>
          <v-list-subheader class="d-flex align-center">
            工作环节
            <v-tooltip location="right" max-width="280">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="x-small" class="ml-1 text-medium-emphasis">mdi-help-circle-outline</v-icon>
              </template>
              <div class="text-caption">
                <div class="mb-1"><strong>环节状态</strong>（按下属文件版本实时派生）</div>
                <div>· <strong>待办</strong>：未开始，所有文件仍是草稿</div>
                <div>· <strong>进行中</strong>：有文件已上传或派生入账</div>
                <div>· <strong>已交付</strong>：所有必填产出已提交</div>
                <div>· <strong>已封存</strong>：项目已结项归档</div>
              </div>
            </v-tooltip>
          </v-list-subheader>
          <v-list-item
            v-for="s in stages"
            :key="s.id"
            :active="selectedStageId === s.id"
            class="stage-list-item py-2"
            @click="selectStage(s.id)"
          >
            <v-list-item-title>
              {{ s.stage_name }}
            </v-list-item-title>
            <!-- V3-3 §7.3 官方状态机 + V1 派生展示状态分开显示
                 用 div + 自定义 class 替代 v-list-item-subtitle，避免 Vuetify
                 默认的 white-space:nowrap + overflow:hidden 把 chip 裁掉。
                 当官方状态已是终态（completed/skipped）时，不再叠加派生进度——
                 用户已显式 mark 完成/跳过，派生 chip 只会变成视觉噪音。 -->
            <div class="stage-status-row mt-1 d-flex flex-wrap align-center" style="gap: 4px;">
              <v-chip size="x-small" :color="stageProgressColor(s)" variant="tonal">
                {{ stageProgressLabel(s) }}
              </v-chip>
              <v-chip
                v-if="showOfficialStageStatus(s)"
                size="x-small"
                :color="officialStageColor(s.status)"
                variant="text"
              >
                流程 {{ officialStageLabel(s.status) }}
              </v-chip>
            </div>
            <template #append>
              <v-menu v-if="!isReadOnly && nextStageStatuses(s.status).length">
                <template #activator="{ props }">
                  <v-btn
                    v-bind="props"
                    size="x-small"
                    variant="tonal"
                    color="primary"
                    title="切换环节状态"
                    @click.stop
                  >
                    <v-icon>mdi-swap-horizontal</v-icon>
                    切换
                  </v-btn>
                </template>
                <v-list density="compact">
                  <v-list-item
                    v-for="ns in nextStageStatuses(s.status)"
                    :key="ns"
                    :title="transitionMenuLabel(s.status, ns)"
                    @click="updateStageStatus(s, ns)"
                  />
                </v-list>
              </v-menu>
            </template>
          </v-list-item>
        </v-list>
      </v-col>

      <!-- 中部：三态分组 -->
      <v-col cols="6" class="border-r" style="overflow-y: auto; min-width: 0">
        <!-- 顶部 header bar 与左侧"工作环节"对齐 -->
        <div class="column-header">
          <span class="text-caption text-medium-emphasis">
            <v-icon size="x-small" class="mr-1">mdi-file-tree</v-icon>
            <template v-if="selectedStage">
              {{ selectedStage.stage_name }}（按数据态分组）
            </template>
            <template v-else>文件版本</template>
          </span>
        </div>
        <div class="pa-3" v-if="!selectedStage">
          <div class="text-medium-emphasis text-center py-12">
            请选择左侧工作环节
          </div>
        </div>
        <div class="pa-3" v-else>
          <!-- V3-UI option C: 环节级只读提示 -->
          <v-alert
            v-if="isSelectedStageReadOnly && !isReadOnly"
            type="info"
            density="compact"
            variant="tonal"
            icon="mdi-lock-outline"
            class="mb-3"
          >
            本环节 <strong>{{ selectedStageReadOnlyReason }}</strong>，不可上传/派生/提交/领取。
          </v-alert>
          <div v-for="state in stateOrder" :key="state" class="mb-4">
            <div class="d-flex align-center mb-2">
              <v-icon :color="stateColor(state)" class="mr-2">{{ stateIcon(state) }}</v-icon>
              <span class="text-subtitle-1 font-weight-medium">{{ stateLabel(state) }}</span>
              <v-chip size="x-small" class="ml-2">{{ groupedFvs[state]?.length || 0 }}</v-chip>
              <v-spacer />
              <v-btn
                v-if="state === 'input' && !isWriteBlocked"
                size="x-small"
                variant="tonal"
                prepend-icon="mdi-download-circle"
                @click="openReceiveDialog"
              >
                领取上游产出
              </v-btn>
            </div>

            <div v-if="!groupedFvs[state] || groupedFvs[state].length === 0" class="text-caption text-medium-emphasis pa-2">
              无文件版本
            </div>

            <div v-for="group in groupByRule(groupedFvs[state] || [])" :key="group.localCode" class="mb-2">
              <v-card variant="outlined">
                <div class="px-3 py-2 d-flex align-center bg-grey-lighten-4">
                  <span class="text-caption font-monospace text-primary mr-2">{{ group.localCode }}</span>
                  <span class="text-body-2 font-weight-medium">{{ group.displayName }}</span>
                  <v-chip v-if="group.required" size="x-small" color="error" variant="tonal" class="ml-2">必填</v-chip>
                  <v-spacer />
                  <span class="text-caption text-medium-emphasis">{{ group.fvs.length }} 个版本</span>
                </div>
                <v-list density="compact" class="py-0">
                  <v-list-item
                    v-for="fv in group.fvs"
                    :key="fv.id"
                    :active="selectedFv?.id === fv.id"
                    @click="selectFv(fv)"
                  >
                    <v-list-item-title class="d-flex align-center">
                      <span>{{ fv.version_no }}</span>
                      <v-chip size="x-small" :color="lifecycleColor(fv.lifecycle_status)" variant="tonal" class="ml-2">
                        {{ lifecycleLabel(fv.lifecycle_status) }}
                      </v-chip>
                      <v-chip v-if="fv.submitted_at" size="x-small" color="success" variant="tonal" class="ml-1">
                        已提交
                      </v-chip>
                      <v-chip v-if="fv.source_file_version_id" size="x-small" variant="tonal" class="ml-1">
                        <v-icon size="x-small">mdi-call-split</v-icon> 派生
                      </v-chip>
                    </v-list-item-title>
                    <v-list-item-subtitle class="text-caption">
                      {{ fv.original_file_name || '未上传' }}
                      <span v-if="fv.file_size"> · {{ formatSize(fv.file_size) }}</span>
                    </v-list-item-subtitle>
                    <template #append>
                      <div class="d-flex gap-1">
                        <v-btn
                          v-if="!isWriteBlocked && fv.lifecycle_status === 'planned'"
                          size="x-small"
                          variant="tonal"
                          prepend-icon="mdi-upload"
                          @click.stop="openUploadDialog(fv, 'first')"
                        >
                          上传
                        </v-btn>
                        <v-btn
                          v-if="!isWriteBlocked && fv.lifecycle_status !== 'planned' && fv.data_state !== 'input'"
                          size="x-small"
                          variant="text"
                          icon="mdi-plus"
                          title="新版本"
                          @click.stop="openUploadDialog(fv, 'newVersion')"
                        />
                        <v-btn
                          v-if="!isWriteBlocked && fv.data_state === 'input' && fv.lifecycle_status !== 'planned'"
                          size="x-small"
                          variant="tonal"
                          prepend-icon="mdi-call-split"
                          @click.stop="openDeriveDialog(fv)"
                        >
                          派生
                        </v-btn>
                        <v-btn
                          v-if="!isWriteBlocked && fv.data_state === 'output' && fv.lifecycle_status === 'registered' && !fv.submitted_at"
                          size="x-small"
                          variant="tonal"
                          color="success"
                          prepend-icon="mdi-check-decagram"
                          @click.stop="onSubmit(fv)"
                        >
                          提交
                        </v-btn>
                      </div>
                    </template>
                  </v-list-item>
                </v-list>
              </v-card>
            </div>
          </div>
        </div>
      </v-col>

      <!-- 右侧：详情面板 -->
      <v-col cols="4" style="overflow-y: auto; min-width: 280px">
        <div class="column-header">
          <span class="text-caption text-medium-emphasis">
            <v-icon size="x-small" class="mr-1">mdi-information-outline</v-icon>
            文件版本详情
          </span>
        </div>
        <div class="pa-3" v-if="!selectedFv">
          <div class="text-medium-emphasis text-center py-12">
            选中文件版本查看详情
          </div>
        </div>
        <div class="pa-3" v-else>
          <h3 class="text-subtitle-1 font-weight-medium">{{ selectedFv.display_name }}</h3>
          <div class="d-flex flex-wrap gap-1 mt-1">
            <v-chip size="x-small" class="font-monospace">{{ selectedFv.file_version_code }}</v-chip>
            <v-chip size="x-small" :color="stateColor(selectedFv.data_state)" variant="tonal">
              {{ stateLabel(selectedFv.data_state) }}
            </v-chip>
            <v-chip size="x-small">{{ selectedFv.version_no }}</v-chip>
          </div>

          <v-divider class="my-3" />
          <div class="text-caption text-medium-emphasis mb-1">实体文件</div>
          <div v-if="selectedFv.storage_uri">
            <!-- 1) 直接绑定的 storage_uri（手动上传/绑定路径） -->
            <div class="text-caption font-monospace text-break">{{ selectedFv.storage_uri }}</div>
            <div v-if="selectedFv.checksum" class="text-caption text-medium-emphasis mt-1">
              <span class="font-weight-medium">SHA-256:</span> {{ selectedFv.checksum.substring(0, 16) }}...
            </div>
            <div v-if="selectedFv.file_size" class="text-caption">
              <span class="font-weight-medium">大小:</span> {{ formatSize(selectedFv.file_size) }}
            </div>
          </div>
          <div v-else-if="selectedFvSourceDist?.path">
            <!-- 2) 桥接 fv：经 source-distribution 端点反查到实际扫描路径 -->
            <v-chip size="x-small" color="info" variant="tonal" class="mb-1">已关联扫描资源</v-chip>
            <div class="text-caption font-monospace text-break">{{ selectedFvSourceDist.path }}</div>
            <div v-if="selectedFvSourceDist.checksum" class="text-caption text-medium-emphasis mt-1">
              <span class="font-weight-medium">SHA-256:</span> {{ selectedFvSourceDist.checksum.substring(0, 16) }}...
            </div>
            <div v-if="selectedFvSourceDist.file_size" class="text-caption">
              <span class="font-weight-medium">大小:</span> {{ formatSize(selectedFvSourceDist.file_size) }}
            </div>
          </div>
          <div v-else-if="selectedFv.checksum">
            <!-- 3) 有 checksum 但反查不到分布（如分布行已 disable） -->
            <v-chip size="x-small" color="info" variant="tonal" class="mb-1">已关联（路径不可见）</v-chip>
            <div class="text-caption text-medium-emphasis mt-1">
              <span class="font-weight-medium">SHA-256:</span> {{ selectedFv.checksum.substring(0, 16) }}...
            </div>
          </div>
          <div v-else class="text-caption text-medium-emphasis">尚未绑定实体文件</div>

          <v-divider class="my-3" />
          <div class="text-caption text-medium-emphasis mb-1">底账</div>
          <div v-if="selectedLedger">
            <div class="text-body-2">
              编号 <code>{{ selectedLedger.ledger_code }}</code>
            </div>
            <div class="text-caption">
              状态 {{ selectedLedger.lifecycle_status }}
              · 标识方式 {{ selectedLedger.marking_method }}
            </div>
          </div>
          <div v-else class="text-caption text-medium-emphasis">无底账记录</div>

          <v-divider class="my-3" />
          <div class="text-caption text-medium-emphasis mb-1">来源链路</div>
          <div v-if="sourceFv" class="text-body-2">
            派生自
            <div class="font-monospace text-caption text-primary">{{ sourceFv.file_version_code }}</div>
            <div class="text-caption">{{ sourceFv.display_name }}（{{ stateLabel(sourceFv.data_state) }} · {{ sourceFv.version_no }}）</div>
          </div>
          <div v-else-if="selectedFv.source_file_version_id" class="text-body-2 text-medium-emphasis">
            派生自 fv #{{ selectedFv.source_file_version_id }}（来源已删除或不可见）
          </div>
          <div v-else class="text-caption text-medium-emphasis">顶级（无上游来源）</div>

          <v-divider class="my-3" />
          <div class="text-caption text-medium-emphasis mb-1">当前环节权限 ({{ stageScopedProjectMembers.length }})</div>
          <div v-if="stageScopedProjectMembers.length === 0" class="text-caption text-medium-emphasis">
            未配置当前环节成员
          </div>
          <div v-else>
            <div v-for="m in stageScopedProjectMembers" :key="m.id" class="mb-1">
              <div class="text-caption">
                <strong>{{ m.role_code }}</strong>
                <span class="text-medium-emphasis ml-1">
                  · 成员 {{ projectMemberDisplayName(m) }}
                </span>
              </div>
              <div class="d-flex flex-wrap gap-1 mt-1">
                <v-chip
                  v-for="p in parsePerms(m.permission_actions)"
                  :key="p"
                  size="x-small"
                  variant="tonal"
                  color="primary"
                >
                  {{ permLabel(p) }}
                </v-chip>
              </div>
            </div>
          </div>

          <v-divider class="my-3" />
          <div class="text-caption text-medium-emphasis mb-1">
            安全策略
            <v-tooltip location="left" max-width="280">
              <template #activator="{ props }">
                <v-icon v-bind="props" size="x-small" class="ml-1 text-medium-emphasis">mdi-help-circle-outline</v-icon>
              </template>
              <div class="text-caption">
                依据《简版需求》§3.6 九宫格存储基线：项目敏感等级 ×
                文件版本状态 决定应处存储位置。当前 storage_tier 仅作字符串
                记账，不强制物理迁移。
              </div>
            </v-tooltip>
          </div>
          <div class="text-body-2">
            <div>项目敏感等级：
              <v-chip size="x-small" :color="sensColor(project?.sensitivity_level || '')" variant="tonal">
                {{ sensLabel(project?.sensitivity_level || '') }}
              </v-chip>
            </div>
            <div v-if="fvSecurity" class="mt-1">
              <div class="text-caption">
                <span class="text-medium-emphasis">文件状态：</span>
                <v-chip size="x-small" variant="tonal" color="info">{{ fvSecurity.file_state_label }}</v-chip>
              </div>
              <div class="text-caption mt-1">
                <span class="text-medium-emphasis">应处位置：</span>
                <v-chip size="x-small" variant="tonal" :color="storageTierColor(fvSecurity.storage_tier)">
                  {{ fvSecurity.storage_label }}
                </v-chip>
              </div>
              <div v-if="fvSecurity.policy_id" class="text-caption text-medium-emphasis mt-1">
                匹配策略 ID: {{ fvSecurity.policy_id }}
              </div>
            </div>
            <div v-else class="text-caption text-medium-emphasis mt-1">
              加载安全视图中...
            </div>
          </div>

          <v-divider class="my-3" />
          <div class="text-caption text-medium-emphasis mb-1">生命周期事件 ({{ events.length }})</div>
          <div v-if="events.length === 0" class="text-caption text-medium-emphasis">尚无事件</div>
          <v-timeline v-else density="compact" side="end" align="start">
            <v-timeline-item
              v-for="e in events"
              :key="e.id"
              :dot-color="eventColor(e.event_type)"
              size="x-small"
            >
              <div class="text-caption text-medium-emphasis">{{ formatTime(e.create_time) }}</div>
              <div class="text-body-2 font-weight-medium">{{ e.event_name }}</div>
              <div v-if="e.reason" class="text-caption">{{ e.reason }}</div>
            </v-timeline-item>
          </v-timeline>
        </div>
      </v-col>
    </v-row>

    <!-- 提交产出确认弹窗 -->
    <v-dialog v-model="submitConfirm.show" max-width="480">
      <v-card v-if="submitConfirm.fv">
        <v-card-title>
          <v-icon class="mr-2" color="primary">mdi-send-check</v-icon> 确认提交产出
        </v-card-title>
        <v-card-text>
          <div class="mb-2">
            提交后，<strong>下游环节可以领取此产出作为输入</strong>，且本文件版本将
            被标记"已提交"，不可再次提交。
          </div>
          <v-card variant="tonal" class="pa-3">
            <div class="font-weight-medium">{{ submitConfirm.fv.display_name }}</div>
            <div class="text-caption text-medium-emphasis font-monospace mt-1">
              {{ submitConfirm.fv.file_version_code }}
            </div>
            <div class="text-caption mt-1">版本 {{ submitConfirm.fv.version_no }}</div>
          </v-card>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="submitConfirm.loading" @click="submitConfirm.show = false">
            取消
          </v-btn>
          <v-btn color="primary" :loading="submitConfirm.loading" @click="doSubmit">
            确认提交
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 上传对话框 -->
    <v-dialog v-model="uploadDialog.show" max-width="500" persistent>
      <v-card>
        <v-card-title>
          <v-icon class="mr-2">mdi-upload</v-icon>
          {{ uploadDialog.mode === 'newVersion' ? '上传新版本' : '上传文件' }}
        </v-card-title>
        <v-card-subtitle>
          {{ uploadDialog.fv?.display_name }} · {{ uploadDialog.fv?.file_version_code }}
        </v-card-subtitle>
        <v-card-text>
          <v-file-input
            v-model="uploadDialog.file"
            label="选择文件"
            variant="outlined"
            density="compact"
            prepend-icon="mdi-paperclip"
            :accept="acceptHint(uploadDialog.fv)"
            show-size
          />
          <div class="text-caption text-medium-emphasis mt-2">
            允许文件类型：{{ allowedTypesHint(uploadDialog.fv) }}
          </div>
          <v-alert v-if="uploadDialog.error" type="error" variant="tonal" class="mt-2">
            {{ uploadDialog.error }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="uploadDialog.loading" @click="uploadDialog.show = false">取消</v-btn>
          <v-btn color="primary" :loading="uploadDialog.loading" :disabled="!uploadDialog.file" @click="onUpload">
            上传
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 派生过程文件对话框 -->
    <v-dialog v-model="deriveDialog.show" max-width="500" persistent>
      <v-card>
        <v-card-title>
          <v-icon class="mr-2">mdi-call-split</v-icon>
          派生过程文件
        </v-card-title>
        <v-card-subtitle>
          来源：{{ deriveDialog.source?.display_name }} ({{ deriveDialog.source?.file_version_code }})
        </v-card-subtitle>
        <v-card-text>
          <v-select
            v-model="deriveDialog.targetStageId"
            :items="stageOptions"
            label="目标环节"
            variant="outlined"
            density="compact"
          />
          <v-select
            v-model="deriveDialog.targetRuleCode"
            :items="processRuleOptions(deriveDialog.targetStageId)"
            label="目标过程文件规则 *"
            variant="outlined"
            density="compact"
          />
          <v-file-input
            v-model="deriveDialog.file"
            label="选择派生文件"
            variant="outlined"
            density="compact"
            prepend-icon="mdi-paperclip"
            show-size
          />
          <v-alert v-if="deriveDialog.error" type="error" variant="tonal" class="mt-2">{{ deriveDialog.error }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="deriveDialog.loading" @click="deriveDialog.show = false">取消</v-btn>
          <v-btn
            color="primary"
            :loading="deriveDialog.loading"
            :disabled="!deriveDialog.file || !deriveDialog.targetRuleCode"
            @click="onDerive"
          >
            派生
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 领取对话框 -->
    <v-dialog v-model="receiveDialog.show" max-width="720" persistent>
      <v-card>
        <v-card-title>
          <v-icon class="mr-2">mdi-download-circle</v-icon>
          领取上游产出为本环节输入
        </v-card-title>
        <v-card-subtitle class="pb-0">
          把其他环节已提交的产出文件，挂接到本环节模版预定义的输入槽位
        </v-card-subtitle>

        <v-card-text>
          <!-- 步骤 1：选要领的上游产出 -->
          <div class="text-subtitle-2 mb-1">
            <v-chip size="x-small" color="primary" class="mr-2">第 1 步</v-chip>
            选择要领取的上游产出
          </div>
          <div class="text-caption text-medium-emphasis mb-2">
            列表只显示当前项目已被显式"提交"的产出文件——未提交的不能被下游领取。
          </div>
          <v-card variant="outlined" class="pa-2 mb-4">
            <v-radio-group v-model="receiveDialog.sourceId" hide-details>
              <v-radio v-for="o in receiveDialog.outputs" :key="o.id" :value="o.id">
                <template #label>
                  <div>
                    <div class="text-body-2 font-weight-medium">{{ o.display_name }}</div>
                    <div class="text-caption font-monospace text-primary">{{ o.file_version_code }}</div>
                    <div class="text-caption text-medium-emphasis">
                      {{ o.version_no }} · {{ formatTime(o.submitted_at) }} · 提交人 {{ o.submitted_by || '-' }}
                    </div>
                  </div>
                </template>
              </v-radio>
            </v-radio-group>
            <div v-if="receiveDialog.outputs.length === 0" class="text-caption text-medium-emphasis py-3 text-center">
              当前项目尚无已提交的产出可供领取
            </div>
          </v-card>

          <!-- 步骤 2：选本环节的输入槽位 -->
          <div class="text-subtitle-2 mb-1">
            <v-chip size="x-small" color="primary" class="mr-2">第 2 步</v-chip>
            选择本环节的输入槽位
          </div>
          <div class="text-caption text-medium-emphasis mb-2">
            模版为本环节定义了若干输入槽位（input 规则），选择哪一个槽位用于"接收"上面这份上游产出。
            举例：把上游"排版完成稿"挂接到本环节的"排版完成稿"槽位。
          </div>
          <v-select
            v-model="receiveDialog.targetRuleCode"
            :items="inputRuleOptions(selectedStageId || 0)"
            label="目标输入槽位 *"
            placeholder="请选择本环节的某条输入规则"
            variant="outlined"
            density="compact"
            hide-details
          />

          <v-alert v-if="receiveDialog.error" type="error" variant="tonal" class="mt-3">{{ receiveDialog.error }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="receiveDialog.loading" @click="receiveDialog.show = false">取消</v-btn>
          <v-btn
            color="primary"
            :loading="receiveDialog.loading"
            :disabled="!receiveDialog.sourceId || !receiveDialog.targetRuleCode"
            @click="onReceive"
          >
            领取为输入
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar.show" :color="snackbar.color" :timeout="4000">
      {{ snackbar.text }}
    </v-snackbar>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  projectsApi,
  fileVersionsApi,
  templatesApi,
  type DataProject,
  type FileVersion,
  type AssetLedger,
  type LifecycleEvent,
  type FullTemplate,
} from '@/services/projectsApi'
import { filterProjectMembersByStage, projectMemberDisplayName, type ProjectPermissionMember } from '@/services/projectPermissions'

const route = useRoute()
const projectId = computed(() => Number(route.params.id))

const project = ref<DataProject | null>(null)
const stages = ref<any[]>([])
const fileVersions = ref<FileVersion[]>([])
const fullTemplate = ref<FullTemplate | null>(null)

// V5-P1：解绑 / 重分类 后 fv 会进入 lifecycle_status='cancelled'（或更早的 destroyed）
// 日常工作台不应让它们继续占据列表，但审计场景仍需可见。
// 默认隐藏，顶部 toggle 打开后可看到全部。
const includeCancelled = ref(false)
const visibleFileVersions = computed(() =>
  includeCancelled.value
    ? fileVersions.value
    : fileVersions.value.filter(
        (f) => f.lifecycle_status !== 'cancelled' && f.lifecycle_status !== 'destroyed'
      )
)

// V3-9 §9.2 右侧详情面板展示当前环节权限（成员/角色/动作）
const projectMembers = ref<ProjectPermissionMember[]>([])
// 解析每个成员的 permission_actions 为字符串数组（兼容 JSON / CSV）
function parsePerms(raw: string): string[] {
  if (!raw) return []
  try {
    const arr = JSON.parse(raw)
    if (Array.isArray(arr)) return arr
  } catch { /* ignore */ }
  return raw.split(',').map(s => s.trim()).filter(Boolean)
}
function permLabel(p: string): string {
  return ({
    read: '查看', write: '写入', receive: '领取', upload: '上传',
    submit: '提交', share: '共享', archive: '归档', close: '结项', destroy: '销毁',
  } as Record<string, string>)[p] || p
}

// 归档移交按钮的三态：未移交 / 失败可重试 / 成功不可点
const syncStatus = computed(() => project.value?.sync_status || 'pending')

const syncBtnLabel = computed(() => {
  switch (syncStatus.value) {
    case 'success': return '已移交'
    case 'failed':  return '重新移交'
    default:        return '归档移交'
  }
})
const syncBtnColor = computed(() => {
  switch (syncStatus.value) {
    case 'success': return 'success'
    case 'failed':  return 'error'
    default:        return 'primary'
  }
})
const syncBtnIcon = computed(() => {
  switch (syncStatus.value) {
    case 'success': return 'mdi-cloud-check-variant'
    case 'failed':  return 'mdi-cloud-alert-outline'
    default:        return 'mdi-cloud-upload-outline'
  }
})
const syncBtnDisabled = computed(() => syncStatus.value === 'success')
const syncTooltip = computed(() => {
  switch (syncStatus.value) {
    case 'success': return '本项目卷宗已成功移交至档案库（manage），不可重复移交'
    case 'failed':  return '上次移交失败，可点击重试'
    default:        return '把本项目卷宗的归档清单（manifest.json）移交至档案库（manage）'
  }
})

// V1：项目结项归档/取消后整个工作台进入只读模式
//
// 隐藏所有写入口（上传/派生/新版本/提交/领取/底账切换），
// 顶部加 alert banner 解释状态，避免操作员误以为按钮"消失了"。
const isReadOnly = computed(() => {
  const s = project.value?.status
  return s === 'archived' || s === 'cancelled'
})
const readOnlyReason = computed(() => {
  if (project.value?.status === 'archived') return '已结项归档'
  if (project.value?.status === 'cancelled') return '已取消'
  return ''
})

// V3-UI option C: 当前选中的环节是否处于"只读"（completed / skipped）
// 与项目级 isReadOnly 互补：项目只读管整个工作台，环节只读管这个环节内的写动作。
const isSelectedStageReadOnly = computed(() => {
  const s = selectedStage.value
  if (!s) return false
  return s.status === 'completed' || s.status === 'skipped'
})
const selectedStageReadOnlyReason = computed(() => {
  const s = selectedStage.value
  if (!s) return ''
  if (s.status === 'completed') return '已完成'
  if (s.status === 'skipped') return '已跳过（可点右上"..."→ 撤销跳过 重新开工）'
  return ''
})
// 写动作的统一守卫：项目只读 OR 当前环节只读
const isWriteBlocked = computed(() => isReadOnly.value || isSelectedStageReadOnly.value)

// 项目根目录在 header 上短显示（默认末段）+ tooltip 看全路径 + 点击复制
const projectRootShort = computed(() => {
  const p = project.value?.project_root
  if (!p) return ''
  // 同时兼容 / 和 \ 分隔符
  const parts = p.replace(/\\/g, '/').split('/').filter(s => s.length > 0)
  if (parts.length === 0) return p
  if (parts.length === 1) return parts[0]
  // 显示倒数两段，前面缀 …/ 提示有省略
  return `…/${parts.slice(-2).join('/')}`
})

async function copyProjectRoot() {
  const p = project.value?.project_root
  if (!p) return
  try {
    await navigator.clipboard.writeText(p)
    showMsg('项目根目录已复制到剪贴板')
  } catch {
    // 兜底：navigator.clipboard 在 Wails 某些版本可能不可用，用 fallback
    try {
      const ta = document.createElement('textarea')
      ta.value = p
      ta.style.position = 'fixed'
      ta.style.left = '-9999px'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
      showMsg('项目根目录已复制到剪贴板')
    } catch (e: any) {
      showMsg('复制失败：' + (e?.message || e), 'error')
    }
  }
}

const selectedStageId = ref<number | null>(null)
const selectedFv = ref<FileVersion | null>(null)
const selectedLedger = ref<AssetLedger | null>(null)
const sourceFv = ref<FileVersion | null>(null)  // 当 selectedFv 有 source_file_version_id 时，展示它的编码+名称
const events = ref<LifecycleEvent[]>([])
const activePermissionStageId = computed(() => selectedFv.value?.project_stage_id || selectedStageId.value)
const stageScopedProjectMembers = computed(() =>
  filterProjectMembersByStage(projectMembers.value, activePermissionStageId.value)
)

// V5-P1 Q3: 桥接 fv（BridgeClassifyToPersonalProject 等创建的）只填 checksum 不填 storage_uri，
// 物理文件路径在 data_distributing 表里。selectedFv 变化时主动调 source-distribution 端点
// 反查路径，UI 三档显示：直接绑定 / 已关联扫描资源（路径可见）/ 已关联（路径不可见）/ 尚未绑定。
const selectedFvSourceDist = ref<{
  path?: string
  file_size?: number
  file_suffix?: string | null
  file_create_time?: string | null
  checksum?: string
} | null>(null)

watch(selectedFv, async (fv) => {
  selectedFvSourceDist.value = null
  if (!fv) return
  // 已直接绑定 storage_uri → 没必要反查
  if (fv.storage_uri) return
  // 无 checksum → 没法反查（典型 planned fv）
  if (!fv.checksum) return
  try {
    const res = await fetch(`http://127.0.0.1:3001/file-versions/${fv.id}/source-distribution`)
    const json = await res.json()
    if (json.success && json.data) {
      selectedFvSourceDist.value = json.data
    }
  } catch {
    // 静默失败：网络问题时回退到 "尚未绑定" 默认显示
  }
})

const snackbar = ref({ show: false, text: '', color: 'success' })

const syncLoading = ref(false)

// 提交产出确认弹窗（替代原 window.confirm，Wails WebView 兼容性更好）
const submitConfirm = ref<{ show: boolean; fv: FileVersion | null; loading: boolean }>({
  show: false, fv: null, loading: false,
})

const closeDialog = ref<{
  show: boolean
  loading: boolean
  submitting: boolean
  precheck: { ok: boolean; issues: { severity: string; code: string; message: string }[] } | null
  reason: string
  force: boolean
  error: string
  result: any | null
}>({
  show: false,
  loading: false,
  submitting: false,
  precheck: null,
  reason: '',
  force: false,
  error: '',
  result: null,
})

const canSubmitClose = computed(() => {
  if (!closeDialog.value.precheck) return false
  if (!closeDialog.value.precheck.ok) return false
  if (closeDialog.value.precheck.issues.length > 0 && !closeDialog.value.force) return false
  return true
})

// 预检 issues 严重性筛选 + 分组展示状态
const precheckSeverityFilter = ref<'all' | 'error' | 'warning'>('all')
const precheckExpandedGroups = ref<string[]>([])

const precheckErrorCount = computed(() =>
  (closeDialog.value.precheck?.issues || []).filter(i => i.severity === 'error').length
)
const precheckWarningCount = computed(() =>
  (closeDialog.value.precheck?.issues || []).filter(i => i.severity === 'warning').length
)

interface PrecheckIssueGroup {
  code: string
  severity: string
  items: { severity: string; code: string; message: string }[]
}

const groupedPrecheckIssues = computed<PrecheckIssueGroup[]>(() => {
  const all = closeDialog.value.precheck?.issues || []
  const filtered = precheckSeverityFilter.value === 'all'
    ? all
    : all.filter(i => i.severity === precheckSeverityFilter.value)
  // 按 code 分组，error 在前 warning 在后，组内保持原顺序
  const map = new Map<string, PrecheckIssueGroup>()
  for (const it of filtered) {
    const g = map.get(it.code)
    if (g) {
      g.items.push(it)
    } else {
      map.set(it.code, { code: it.code, severity: it.severity, items: [it] })
    }
  }
  const groups = Array.from(map.values())
  groups.sort((a, b) => {
    if (a.severity === b.severity) return a.code.localeCompare(b.code)
    return a.severity === 'error' ? -1 : 1
  })
  return groups
})

// code 友好标签（业务可读）。后端 issue code 见 project_close.go
function codeLabel(code: string): string {
  return ({
    REQUIRED_NOT_REGISTERED: '必填文件未上传',
    OUTPUT_NOT_SUBMITTED: '产出已上传但未提交',
    LEDGER_PLANNED: '底账仍是草稿',
    ALREADY_ARCHIVED: '项目已归档',
    CANCELLED: '项目已取消',
  } as Record<string, string>)[code] || code
}

async function openCloseDialog() {
  closeDialog.value = {
    show: true,
    loading: true,
    submitting: false,
    precheck: null,
    reason: '',
    force: false,
    error: '',
    result: null,
  }
  try {
    const result = await projectsApi.closePrecheck(projectId.value)
    // 防御：后端任何路径都不能返回 issues=null（已加测试守护），但前端兜底也加
    if (!result.issues) {
      result.issues = []
    }
    closeDialog.value.precheck = result
    // 默认展开所有 error 分组（让用户一进来就能看到要处理的项）
    const errorCodes = new Set<string>()
    for (const i of result.issues) {
      if (i.severity === 'error') errorCodes.add(i.code)
    }
    precheckExpandedGroups.value = Array.from(errorCodes)
    precheckSeverityFilter.value = 'all'
  } catch (e: any) {
    console.error('[close] precheck failed:', e)
    closeDialog.value.error = e?.message || String(e)
  } finally {
    closeDialog.value.loading = false
  }
}

async function onSubmitClose() {
  closeDialog.value.submitting = true
  closeDialog.value.error = ''
  try {
    closeDialog.value.result = await projectsApi.close(
      projectId.value,
      closeDialog.value.reason,
      closeDialog.value.force,
    )
    showMsg('结项完成')
    await loadProject()
  } catch (e: any) {
    closeDialog.value.error = e.message
  } finally {
    closeDialog.value.submitting = false
  }
}

async function onSyncArchive() {
  // 双重防呆：computed 已禁用按钮，但万一某种情况漏了，再校验一下
  if (project.value?.sync_status === 'success') {
    showMsg('本项目已成功移交，不可重复移交', 'warning')
    return
  }
  syncLoading.value = true
  try {
    const res = await projectsApi.syncArchive(projectId.value)
    if (res.status === 'success') {
      showMsg('归档移交成功：' + (res.reply || '档案库已接收'), 'success')
    } else {
      showMsg('归档移交失败：' + (res.error || '未知错误'), 'error')
    }
    await loadProject()
  } catch (e: any) {
    showMsg('归档移交失败：' + (e?.message || e), 'error')
  } finally {
    syncLoading.value = false
  }
}

// 一键归档：按九宫格分流——个人→本地个人夹复制 / 部门、单位→上报云端 / 行业→跳过。
const quickArchiving = ref(false)
async function onQuickArchive() {
  quickArchiving.value = true
  try {
    const r = await projectsApi.quickArchive(projectId.value)
    const tail = (r.errors && r.errors.length) ? `，${r.errors.length} 个错误` : ''
    showMsg(`${r.route}：新归档 ${r.archived} 个、跳过 ${r.skipped} 个${tail}`, (r.errors && r.errors.length) ? 'warning' : 'success')
  } catch (e: any) {
    showMsg('一键归档失败：' + (e?.message || e), 'error')
  } finally {
    quickArchiving.value = false
  }
}

const uploadDialog = ref<{
  show: boolean
  loading: boolean
  fv: FileVersion | null
  mode: 'first' | 'newVersion'
  file: File | null
  error: string
}>({ show: false, loading: false, fv: null, mode: 'first', file: null, error: '' })

const deriveDialog = ref<{
  show: boolean
  loading: boolean
  source: FileVersion | null
  targetStageId: number
  targetRuleCode: string
  file: File | null
  error: string
}>({ show: false, loading: false, source: null, targetStageId: 0, targetRuleCode: '', file: null, error: '' })

const receiveDialog = ref<{
  show: boolean
  loading: boolean
  outputs: FileVersion[]
  sourceId: number | null
  targetRuleCode: string
  error: string
}>({ show: false, loading: false, outputs: [], sourceId: null, targetRuleCode: '', error: '' })

const stateOrder = ['input', 'process', 'output']

const selectedStage = computed(() => stages.value.find((s) => s.id === selectedStageId.value))
const groupedFvs = computed<Record<string, FileVersion[]>>(() => {
  const groups: Record<string, FileVersion[]> = { input: [], process: [], output: [] }
  if (!selectedStageId.value) return groups
  for (const fv of visibleFileVersions.value) {
    if (fv.project_stage_id === selectedStageId.value) {
      groups[fv.data_state]?.push(fv)
    }
  }
  return groups
})

const stageOptions = computed(() =>
  stages.value.map((s) => ({ title: `${s.stage_code} ${s.stage_name}`, value: s.id }))
)

function processRuleOptions(stageId: number) {
  if (!fullTemplate.value || !stageId) return []
  const stage = stages.value.find((s) => s.id === stageId)
  if (!stage) return []
  const tplStage = fullTemplate.value.stages.find((s) => s.stage_code === stage.stage_code)
  if (!tplStage) return []
  return tplStage.file_rules
    .filter((r) => r.data_state === 'process')
    .map((r) => ({ title: `${r.file_rule_code} ${r.file_name}`, value: r.file_rule_code }))
}

function inputRuleOptions(stageId: number) {
  if (!fullTemplate.value || !stageId) return []
  const stage = stages.value.find((s) => s.id === stageId)
  if (!stage) return []
  const tplStage = fullTemplate.value.stages.find((s) => s.stage_code === stage.stage_code)
  if (!tplStage) return []
  return tplStage.file_rules
    .filter((r) => r.data_state === 'input')
    .map((r) => ({ title: `${r.file_rule_code} ${r.file_name}`, value: r.file_rule_code }))
}

function groupByRule(fvs: FileVersion[]) {
  const map = new Map<string, { localCode: string; displayName: string; required: boolean; fvs: FileVersion[] }>()
  for (const fv of fvs) {
    if (!map.has(fv.local_code)) {
      map.set(fv.local_code, {
        localCode: fv.local_code,
        displayName: fv.display_name,
        required: fv.required === 1,
        fvs: [],
      })
    }
    map.get(fv.local_code)!.fvs.push(fv)
  }
  // 同一规则下按版本号倒序
  for (const g of map.values()) {
    g.fvs.sort((a, b) => b.version_no.localeCompare(a.version_no))
  }
  return Array.from(map.values()).sort((a, b) => a.localCode.localeCompare(b.localCode))
}

// =====================
// Labels & colors
// =====================
function statusLabel(s: string) {
  return ({ draft: '草稿', active: '执行中', closing: '结项中', archived: '已归档', cancelled: '已取消' } as Record<string, string>)[s] || s
}
function statusColor(s: string) {
  return ({ draft: 'default', active: 'success', closing: 'warning', archived: 'info', cancelled: 'error' } as Record<string, string>)[s] || 'default'
}
function sensColor(s: string) {
  return ({ general: 'default', important: 'warning', core_secret: 'error' } as Record<string, string>)[s] || 'default'
}
function sensLabel(s: string) {
  return ({ general: '一般', important: '重要', core_secret: '核心(涉密)' } as Record<string, string>)[s] || s
}
function stateColor(s: string) {
  return ({ input: 'info', process: 'warning', output: 'success' } as Record<string, string>)[s] || 'default'
}
function stateLabel(s: string) {
  return ({ input: '输入 IN', process: '过程 PRC', output: '产出 OUT' } as Record<string, string>)[s] || s
}
function stateIcon(s: string) {
  return ({ input: 'mdi-tray-arrow-down', process: 'mdi-cog', output: 'mdi-tray-arrow-up' } as Record<string, string>)[s] || 'mdi-file'
}
function stageStatusColor(s: string) {
  return ({
    pending: 'default',
    in_progress: 'info',
    submitted: 'success',
    closed: 'warning',
  } as Record<string, string>)[s] || 'default'
}

function stageStatusLabel(s: string) {
  return ({
    pending: '待办',
    in_progress: '进行中',
    submitted: '已交付',
    closed: '已封存',
  } as Record<string, string>)[s] || s
}

function stageProgressStatus(stageId: number): string {
  return deriveStageStatus(stageId)
}

function stageProgressLabel(stage: any): string {
  return stageStatusLabel(stageProgressStatus(stage.id))
}

function stageProgressColor(stage: any): string {
  return stageStatusColor(stageProgressStatus(stage.id))
}

function showOfficialStageStatus(stage: any): boolean {
  return stage.status !== 'completed' && stage.status !== 'skipped' && stage.status !== stageProgressStatus(stage.id)
}

// V3-3 §7.3 + §5.2 官方环节状态机（DB 字段 pending/running/completed/skipped）
function officialStageLabel(s: string): string {
  return ({
    pending: '待办',
    running: '进行中',
    completed: '已完成',
    skipped: '已跳过',
  } as Record<string, string>)[s] || s
}
function officialStageColor(s: string): string {
  return ({
    pending: 'default',
    running: 'info',
    completed: 'success',
    skipped: 'warning',
  } as Record<string, string>)[s] || 'default'
}
function nextStageStatuses(s: string): string[] {
  // V3-UI option C: skipped 可撤销回 pending，completed 是硬终态
  return ({
    pending: ['running', 'skipped'],
    running: ['completed', 'skipped'],
    completed: [],
    skipped: ['pending'],
  } as Record<string, string[]>)[s] || []
}
function transitionMenuLabel(fromStatus: string, toStatus: string): string {
  // V3-UI option C: 撤销跳过给一个更直观的文案
  if (fromStatus === 'skipped' && toStatus === 'pending') return '撤销跳过（回到待办）'
  return `切换到「${officialStageLabel(toStatus)}」`
}
async function updateStageStatus(stage: any, toStatus: string) {
  if (!project.value) return
  try {
    await projectsApi.updateStageStatus(project.value.id, stage.id, toStatus)
    stage.status = toStatus
    snackbar.value = { show: true, text: `已切换到 ${officialStageLabel(toStatus)}`, color: 'success' }
  } catch (e: any) {
    snackbar.value = { show: true, text: '切换失败：' + e.message, color: 'error' }
  }
}

// V1 实时派生 stage 状态（DB 字段没维护，前端按真实数据算）
//
// 规则：
//   - 项目 archived → closed
//   - stage 下所有 fv 都 planned → pending
//   - stage 下所有 required=1 的 output 都 submitted_at != null → submitted
//   - 其他 → in_progress（有任意 fv registered/in_use/sealed 等）
function deriveStageStatus(stageId: number): string {
  if (project.value?.status === 'archived') return 'closed'
  const fvs = visibleFileVersions.value.filter((f) => f.project_stage_id === stageId)
  if (fvs.length === 0) return 'pending'

  // 全 planned → pending
  const allPlanned = fvs.every((f) => f.lifecycle_status === 'planned')
  if (allPlanned) return 'pending'

  // 必填的 output 是否全 submitted
  const requiredOutputs = fvs.filter((f) => f.data_state === 'output' && f.required === 1)
  if (requiredOutputs.length > 0 && requiredOutputs.every((f) => !!f.submitted_at)) {
    return 'submitted'
  }
  return 'in_progress'
}
function lifecycleColor(s: string) {
  return ({ planned: 'default', registered: 'info', in_use: 'warning', sealed: 'success', destroyed: 'error', permanent: 'success' } as Record<string, string>)[s] || 'default'
}
function lifecycleLabel(s: string) {
  return ({ planned: '待上传', registered: '已入账', in_use: '使用中', sealed: '已归档', destroyed: '已销毁', permanent: '永存' } as Record<string, string>)[s] || s
}
function eventColor(t: string) {
  return ({ register: 'info', use: 'warning', transfer: 'warning', change: 'primary', handover: 'primary', archive: 'success', destroy: 'error', permanent: 'success' } as Record<string, string>)[t] || 'default'
}
function formatTime(t: string | null) {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}
function formatSize(b: number) {
  if (b < 1024) return `${b}B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)}KB`
  return `${(b / 1024 / 1024).toFixed(1)}MB`
}
function acceptHint(fv: FileVersion | null): string {
  if (!fv || !fullTemplate.value) return ''
  for (const stage of fullTemplate.value.stages) {
    for (const rule of stage.file_rules) {
      if (rule.id === fv.template_file_rule_id) {
        try {
          const arr = JSON.parse(rule.allowed_file_types) as string[]
          return arr.map((s) => '.' + s.toLowerCase()).join(',')
        } catch {
          return ''
        }
      }
    }
  }
  return ''
}
function allowedTypesHint(fv: FileVersion | null): string {
  if (!fv || !fullTemplate.value) return ''
  for (const stage of fullTemplate.value.stages) {
    for (const rule of stage.file_rules) {
      if (rule.id === fv.template_file_rule_id) {
        try {
          return (JSON.parse(rule.allowed_file_types) as string[]).join(', ')
        } catch {
          return rule.allowed_file_types
        }
      }
    }
  }
  return ''
}

// =====================
// Loading
// =====================
async function loadProject() {
  try {
    project.value = await projectsApi.get(projectId.value)
    await Promise.all([loadStages(), loadFileVersions(), loadTemplate(), loadMembersAndUser()])
    if (stages.value.length > 0 && !selectedStageId.value) {
      selectStage(stages.value[0].id)
    }
  } catch (e: any) {
    showMsg('加载失败：' + e.message, 'error')
  }
}

// V3-9 §9.2 右侧项目权限：加载项目成员列表
async function loadMembersAndUser() {
  try {
    projectMembers.value = await projectsApi.listMembers(projectId.value)
  } catch {
    projectMembers.value = []
  }
}

async function loadStages() {
  stages.value = await projectsApi.listStages(projectId.value)
}

async function loadFileVersions() {
  fileVersions.value = await projectsApi.listFileVersions(projectId.value)
}

async function loadTemplate() {
  if (!project.value) return
  // 找到本地缓存中的模版 id
  const list = await templatesApi.list()
  const tpl = list.find(
    (t) => t.template_code === project.value!.template_code && t.template_version === project.value!.template_version
  )
  if (tpl) {
    fullTemplate.value = await templatesApi.get(tpl.id)
  }
}

function selectStage(id: number) {
  selectedStageId.value = id
  selectedFv.value = null
  selectedLedger.value = null
  events.value = []
  if (deriveDialog.value.targetStageId === 0) {
    deriveDialog.value.targetStageId = id
  }
}

// V4-Q4 §3.6 九宫格安全视图（聚合：file_state + storage_tier + 中文 label）
const fvSecurity = ref<{
  file_state: string
  file_state_label: string
  storage_tier: string
  storage_label: string
  policy_id: number | null
} | null>(null)

function storageTierColor(tier: string): string {
  return ({
    personal_folder: 'default',
    department_cabinet: 'info',
    unit_archive: 'success',
    secure_room: 'error',
  } as Record<string, string>)[tier] || 'default'
}

async function selectFv(fv: FileVersion) {
  selectedFv.value = fv
  selectedLedger.value = null
  sourceFv.value = null
  events.value = []
  fvSecurity.value = null
  try {
    const [evList, lg] = await Promise.all([
      fileVersionsApi.events(fv.id),
      fileVersionsApi.ledger(fv.id),
    ])
    events.value = evList
    selectedLedger.value = lg
  } catch (e: any) {
    // 静默失败：UI 已对 null 做了缺省
  }
  // V4-Q4 拉九宫格安全视图
  try {
    const res = await fetch(`http://127.0.0.1:3001/file-versions/${fv.id}/security`)
    const json = await res.json()
    if (json.success) {
      fvSecurity.value = json.data
    }
  } catch {
    // 静默失败
  }
  // 加载来源 fv（用于"派生自 ..." 显示编码+名称而不是 #id）
  if (fv.source_file_version_id) {
    try {
      sourceFv.value = await fileVersionsApi.get(fv.source_file_version_id)
    } catch (e: any) {
      // 来源已删除或不可见，UI fallback 到 #id
    }
  }
}

// =====================
// Actions
// =====================
function openUploadDialog(fv: FileVersion, mode: 'first' | 'newVersion') {
  uploadDialog.value = { show: true, loading: false, fv, mode, file: null, error: '' }
}

async function onUpload() {
  if (!uploadDialog.value.fv || !uploadDialog.value.file) return
  uploadDialog.value.loading = true
  uploadDialog.value.error = ''
  try {
    if (uploadDialog.value.mode === 'first') {
      await fileVersionsApi.upload(uploadDialog.value.fv.id, uploadDialog.value.file)
    } else {
      await fileVersionsApi.newVersion(uploadDialog.value.fv.id, uploadDialog.value.file)
    }
    showMsg('上传成功')
    uploadDialog.value.show = false
    await loadFileVersions()
  } catch (e: any) {
    console.error('[upload] failed:', e)
    uploadDialog.value.error = e?.message || String(e)
    showMsg('上传失败：' + (e?.message || e), 'error')
  } finally {
    // 永远清掉 loading，避免请求卡死时弹窗一直转圈
    uploadDialog.value.loading = false
  }
}

function openDeriveDialog(fv: FileVersion) {
  deriveDialog.value = {
    show: true,
    loading: false,
    source: fv,
    targetStageId: fv.project_stage_id,
    targetRuleCode: '',
    file: null,
    error: '',
  }
}

async function onDerive() {
  if (!deriveDialog.value.source || !deriveDialog.value.file) return
  deriveDialog.value.loading = true
  deriveDialog.value.error = ''
  try {
    await fileVersionsApi.derive(
      deriveDialog.value.source.id,
      deriveDialog.value.file,
      deriveDialog.value.targetStageId,
      deriveDialog.value.targetRuleCode
    )
    showMsg('派生成功')
    deriveDialog.value.show = false
    await loadFileVersions()
  } catch (e: any) {
    deriveDialog.value.error = e.message
  } finally {
    deriveDialog.value.loading = false
  }
}

// 提交产出：弹 Vuetify 对话框确认
//   原来用 window.confirm()，Wails WebView 偶发不弹窗导致"点了没反应"
function onSubmit(fv: FileVersion) {
  submitConfirm.value = { show: true, fv, loading: false }
}

async function doSubmit() {
  if (!submitConfirm.value.fv) return
  submitConfirm.value.loading = true
  try {
    await fileVersionsApi.submit(submitConfirm.value.fv.id)
    showMsg('已提交，下游可领取')
    submitConfirm.value.show = false
    await loadFileVersions()
  } catch (e: any) {
    console.error('[submit] failed:', e)
    showMsg('提交失败：' + (e?.message || e), 'error')
  } finally {
    submitConfirm.value.loading = false
  }
}

async function openReceiveDialog() {
  receiveDialog.value = {
    show: true,
    loading: false,
    outputs: [],
    sourceId: null,
    targetRuleCode: '',
    error: '',
  }
  try {
    receiveDialog.value.outputs = await fileVersionsApi.listSubmittable(projectId.value)
  } catch (e: any) {
    receiveDialog.value.error = e.message
  }
}

async function onReceive() {
  if (!receiveDialog.value.sourceId || !selectedStageId.value) return
  receiveDialog.value.loading = true
  receiveDialog.value.error = ''
  try {
    await fileVersionsApi.receive(receiveDialog.value.sourceId, selectedStageId.value, receiveDialog.value.targetRuleCode)
    showMsg('领取成功')
    receiveDialog.value.show = false
    await loadFileVersions()
  } catch (e: any) {
    receiveDialog.value.error = e.message
  } finally {
    receiveDialog.value.loading = false
  }
}

function showMsg(text: string, color = 'success') {
  snackbar.value = { show: true, text, color }
}

watch(projectId, loadProject)
onMounted(loadProject)
</script>

<style scoped>
.border-r {
  border-right: 1px solid rgba(0, 0, 0, 0.12);
}
.text-break {
  word-break: break-all;
}
/* V3-UI: 环节列表行加高，让下方状态 chip 能完整展示 */
.stage-list-item {
  min-height: 64px;
}
.stage-list-item :deep(.v-list-item__content) {
  white-space: normal;
}
.stage-status-row {
  line-height: 1.4;
}

/* V3-UI: 三列工作台顶部 header 统一对齐
   - 左列 v-list 的 subheader 默认高度约 40px
   - 这里给中右两列加同样高度 + 底部分隔线，三列顶部水平线对齐 */
.column-header {
  height: 40px;
  display: flex;
  align-items: center;
  padding: 0 16px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.12);
  background: rgba(0, 0, 0, 0.02);
}
</style>
