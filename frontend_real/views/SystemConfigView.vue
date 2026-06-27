<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, type SystemConfig, type UserInfo } from '@/services/api'
import { userInfoManager } from '@/services/UserInfoManager'

// 默认服务端地址：文件上传 / 服务端 / 归档上报都默认指向同一台
const DEFAULT_SERVER = 'http://47.95.233.47:19091'

// 状态
const loading = ref(false)
const saving = ref(false)
const syncing = ref(false)
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

// 机主信息（原 SettingsView 入口，现统一并入主设置页）
const userInfo = ref<UserInfo | null>(null)
const showUserInfoDialog = ref(false)
const savingUserInfo = ref(false)
const userInfoForm = ref({
  name: '',
  companyName: '',
  departmentName: '',
  phone: '',
  workAddress: '',
})
const formValid = ref(false)
const formRules = {
  required: (v: string) => !!v?.trim() || '此项为必填项',
}

// 表单数据
const workspace = ref('')
const dailyScanInterval = ref(15)
const controlType = ref('')
const scanAreaPath = ref('')
const scanExcludeDir = ref('')
// 服务端地址：三个 URL（文件上传 / 服务端 / 归档上报）合并为一个
const serverEndpoint = ref('')
const homeDir = ref('')

// 相似认领默认行为
const claimFamilyPolicy = ref<'same_content_only' | 'all' | 'none'>('same_content_only')
const claimFamilyAlwaysAsk = ref(true)

// 相似度阈值
const simSameContent = ref(0.95)
const simProcessVersion = ref(0.75)
const simDerived = ref(0.50)
const simImage = ref(0.84)
const simFilename = ref(0.70)
const simFeature = ref(0.60)

// 同步状态
const lastSyncTime = ref<string | null>(null)

// 数据业务模版 V1 配置（project_root 已与 workspace 合并；三个 URL 已合并为 serverEndpoint；manage_token 已废弃）

// 加载配置
const loadConfig = async () => {
  loading.value = true
  try {
    const config = await api.getConfig()
    workspace.value = config.workspace || ''
    dailyScanInterval.value = config.daily_scan_interval || 15
    controlType.value = config.control_type || '.doc,.docx,.ppt,.pptx,.xls,.xlsx,.pdf'
    scanAreaPath.value = config.scan_area_path || config.home_dir || ''
    scanExcludeDir.value = config.scan_exclude_dir || ''
    // 后端 GET /config 已返回 server_endpoint（也回显 upload_server_url 同值）
    serverEndpoint.value = (config as any).server_endpoint || config.upload_server_url || DEFAULT_SERVER
    homeDir.value = config.home_dir || ''
    lastSyncTime.value = config.last_sync_time
    if (config.similarity_same_content != null)    simSameContent.value = config.similarity_same_content
    if (config.similarity_process_version != null) simProcessVersion.value = config.similarity_process_version
    if (config.similarity_derived != null)         simDerived.value = config.similarity_derived
    if (config.similarity_image != null)           simImage.value = config.similarity_image
    if (config.similarity_filename != null)        simFilename.value = config.similarity_filename
    if (config.similarity_feature != null)         simFeature.value = config.similarity_feature
    // 相似认领默认行为
    if (config.claim_family_default_policy) {
      claimFamilyPolicy.value = config.claim_family_default_policy
    }
    claimFamilyAlwaysAsk.value = config.claim_family_skip_dialog !== 'true'
    // manage_endpoint / archive_endpoint / manage_token 已被 serverEndpoint 取代，不再单独维护
    // 并入机主信息（原 SettingsView 行为）
    userInfo.value = await userInfoManager.getUserInfo()
  } catch (error) {
    showSnackbar('加载配置失败', 'error')
    console.error('Failed to load config:', error)
  } finally {
    loading.value = false
  }
}

// 打开机主信息弹窗
const openUserInfoDialog = () => {
  if (userInfo.value) {
    userInfoForm.value = {
      name: userInfo.value.user_name,
      companyName: userInfo.value.company_name,
      departmentName: userInfo.value.department,
      phone: userInfo.value.phone || '',
      workAddress: userInfo.value.work_address || '',
    }
  } else {
    userInfoForm.value = {
      name: '',
      companyName: '',
      departmentName: '',
      phone: '',
      workAddress: '',
    }
  }
  showUserInfoDialog.value = true
}

// 保存机主信息
const saveUserInfoHandler = async () => {
  if (!formValid.value) return
  savingUserInfo.value = true
  try {
    const savedInfo = await userInfoManager.saveUserInfo({
      user_name: userInfoForm.value.name.trim(),
      company_name: userInfoForm.value.companyName.trim(),
      department: userInfoForm.value.departmentName.trim(),
      phone: userInfoForm.value.phone.trim() || null,
      work_address: userInfoForm.value.workAddress.trim() || null,
    })
    userInfo.value = savedInfo
    showUserInfoDialog.value = false
    showSnackbar('机主信息已保存', 'success')
  } catch (error) {
    showSnackbar('保存机主信息失败', 'error')
    console.error('Failed to save user info:', error)
  } finally {
    savingUserInfo.value = false
  }
}

// 保存配置
const saveConfig = async () => {
  saving.value = true
  try {
    await api.saveConfig({
      workspace: workspace.value,
      daily_scan_interval: dailyScanInterval.value,
      control_type: controlType.value,
      scan_area_path: scanAreaPath.value,
      scan_exclude_dir: scanExcludeDir.value,
      similarity_same_content: simSameContent.value,
      similarity_process_version: simProcessVersion.value,
      similarity_derived: simDerived.value,
      similarity_image: simImage.value,
      similarity_filename: simFilename.value,
      similarity_feature: simFeature.value,
      // 三个 URL（upload / manage / archive）合并为一个 server_endpoint，后端会同步写入三个老 key
      server_endpoint: serverEndpoint.value,
      claim_family_default_policy: claimFamilyPolicy.value,
      claim_family_skip_dialog: claimFamilyAlwaysAsk.value ? 'false' : 'true',
    } as any)
    showSnackbar('配置已保存', 'success')
  } catch (error) {
    showSnackbar('保存配置失败', 'error')
    console.error('Failed to save config:', error)
  } finally {
    saving.value = false
  }
}

// 同步数据资源
const syncResources = async () => {
  if (!serverEndpoint.value) {
    showSnackbar('请先配置服务端地址', 'error')
    return
  }

  syncing.value = true
  try {
    const result = await api.syncSource()
    lastSyncTime.value = result.data.lastSyncTime

    if (result.data.errors && result.data.errors.length > 0) {
      showSnackbar(
        `同步完成: 成功 ${result.data.syncedCount} 条，失败 ${result.data.failedCount} 条`,
        'warning'
      )
      console.warn('Sync errors:', result.data.errors)
    } else {
      showSnackbar(result.message, 'success')
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : '同步失败'
    showSnackbar(message, 'error')
    console.error('Failed to sync resources:', error)
  } finally {
    syncing.value = false
  }
}

// 格式化同步时间
const formatSyncTime = (time: string | null): string => {
  if (!time) return '从未同步'
  const date = new Date(time)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

// 显示提示
const showSnackbar = (text: string, color: string) => {
  snackbarText.value = text
  snackbarColor.value = color
  snackbar.value = true
}

// 组件挂载时加载配置
onMounted(() => {
  loadConfig()
})
</script>

<template>
  <div>
    <v-card elevation="1">
      <v-card-title>
        <v-icon class="mr-2">mdi-cog</v-icon>
        系统设置
      </v-card-title>

      <v-card-text>
        <v-skeleton-loader v-if="loading" type="article" />

        <v-form v-else>
          <v-row>
            <v-col cols="12">
              <v-text-field
                v-model="workspace"
                label="工作空间目录"
                placeholder="/Users/xxx/workspace"
                variant="outlined"
                hint="登录后会自动设为 ~/<用户名>/workspace；可改成别的，下次登录不会被覆盖"
                persistent-hint
              />
            </v-col>

            <v-col cols="12" md="6">
              <v-text-field
                v-model="scanAreaPath"
                label="扫描区域"
                :placeholder="`留空则扫描 ${homeDir}`"
                variant="outlined"
                :hint="`文件扫描的根目录，留空默认扫描 ${homeDir}`"
                persistent-hint
              />
            </v-col>

            <v-col cols="12" md="6">
              <v-text-field
                v-model.number="dailyScanInterval"
                label="日常盘点间隔（分钟）"
                type="number"
                min="1"
                variant="outlined"
                hint="超过此时间间隔后自动进行日常盘点"
                persistent-hint
              />
            </v-col>

            <v-col cols="12">
              <v-text-field
                v-model="controlType"
                label="管控文件类型"
                placeholder=".doc,.docx,.ppt,.pptx,.xls,.xlsx,.pdf"
                variant="outlined"
                hint="需要扫描的文件后缀，多个后缀用逗号分隔"
                persistent-hint
              />
            </v-col>

            <v-col cols="12">
              <v-text-field
                v-model="scanExcludeDir"
                label="排除目录"
                placeholder="node_modules,.git,__pycache__"
                variant="outlined"
                hint="扫描时排除的目录名称，多个目录用逗号分隔"
                persistent-hint
              />
            </v-col>

            <v-col cols="12">
              <v-text-field
                v-model="serverEndpoint"
                label="服务端地址"
                placeholder="http://47.95.233.47:19091"
                variant="outlined"
                hint="文件上传 / 模版同步 / 归档上报统一使用此地址"
                persistent-hint
              />
            </v-col>

            <!-- 相似度阈值区域 -->
            <v-col cols="12">
              <v-divider class="my-4" />
              <div class="text-subtitle-2 mb-3">
                相似度分析阈值
                <span class="text-caption text-grey ml-2">取值 0.0 ~ 1.0，调高会减少家族归并、调低会扩大归并范围</span>
              </div>
              <v-row dense>
                <v-col cols="12" sm="4">
                  <v-text-field v-model.number="simSameContent" type="number" step="0.01" min="0" max="1"
                    label="完全相同 (same_content)" hint="≥ 此值判定为同一内容（默认 0.95）" persistent-hint
                    variant="outlined" density="compact" />
                </v-col>
                <v-col cols="12" sm="4">
                  <v-text-field v-model.number="simProcessVersion" type="number" step="0.01" min="0" max="1"
                    label="流程版本 (process_version)" hint="≥ 此值判定为修订版本（默认 0.75）" persistent-hint
                    variant="outlined" density="compact" />
                </v-col>
                <v-col cols="12" sm="4">
                  <v-text-field v-model.number="simDerived" type="number" step="0.01" min="0" max="1"
                    label="衍生文件 (derived)" hint="≥ 此值判定为衍生（默认 0.50）" persistent-hint
                    variant="outlined" density="compact" />
                </v-col>
                <v-col cols="12" sm="4">
                  <v-text-field v-model.number="simImage" type="number" step="0.01" min="0" max="1"
                    label="图片相似度阈值" hint="图片家族判定门槛（默认 0.84）" persistent-hint
                    variant="outlined" density="compact" />
                </v-col>
                <v-col cols="12" sm="4">
                  <v-text-field v-model.number="simFilename" type="number" step="0.01" min="0" max="1"
                    label="文件名相似度阈值" hint="候选对筛选用（默认 0.70）" persistent-hint
                    variant="outlined" density="compact" />
                </v-col>
                <v-col cols="12" sm="4">
                  <v-text-field v-model.number="simFeature" type="number" step="0.01" min="0" max="1"
                    label="语义指纹预筛阈值" hint="过滤明显不相似的候选对（默认 0.60）" persistent-hint
                    variant="outlined" density="compact" />
                </v-col>
              </v-row>
            </v-col>

            <!-- 相似认领默认行为 -->
            <v-col cols="12">
              <v-divider class="my-4" />
              <div class="text-subtitle-2 mb-3">相似认领默认行为</div>
              <div class="text-body-2 mb-2">默认对相似家族的处理：</div>
              <v-radio-group v-model="claimFamilyPolicy" density="compact" hide-details>
                <v-radio
                  value="same_content_only"
                  label="仅认领相同内容（推荐）"
                  data-test="claim-family-policy-same_content_only"
                />
                <v-radio
                  value="all"
                  label="认领整个家族（相同 + 过程 + 衍生）"
                  data-test="claim-family-policy-all"
                />
                <v-radio
                  value="none"
                  label="不带家族（只认领选中文件）"
                  data-test="claim-family-policy-none"
                />
              </v-radio-group>
              <v-checkbox
                v-model="claimFamilyAlwaysAsk"
                label="总是弹窗确认（即便已设默认）"
                density="compact"
                hide-details
                class="mt-2"
                data-test="claim-family-always-ask"
              />
              <div class="text-caption text-grey mt-1">
                取消勾选此项相当于"下次不再问"
              </div>
            </v-col>

            <!-- 数据同步区域 -->
            <v-col cols="12">
              <v-divider class="my-4" />
              <div class="text-subtitle-2 mb-3">数据同步</div>

              <v-row align="center">
                <v-col cols="12" sm="8">
                  <div class="text-body-2">
                    <v-icon class="mr-1" size="small">mdi-clock-outline</v-icon>
                    最后同步时间: <span class="ml-1">{{ formatSyncTime(lastSyncTime) }}</span>
                  </div>
                </v-col>
                <v-col cols="12" sm="4" class="text-right">
                  <v-btn
                    color="primary"
                    @click="syncResources"
                    :loading="syncing"
                    :disabled="syncing || saving || loading"
                    variant="outlined"
                    prepend-icon="mdi-sync"
                  >
                    同步数据
                  </v-btn>
                </v-col>
              </v-row>
            </v-col>
          </v-row>
        </v-form>
      </v-card-text>

      <v-card-actions>
        <v-btn
          variant="outlined"
          size="small"
          prepend-icon="mdi-account-edit"
          @click="openUserInfoDialog"
          :disabled="loading"
        >
          机主信息
        </v-btn>
        <v-spacer />
        <v-btn
          variant="outlined"
          @click="loadConfig"
          :disabled="loading || saving || syncing"
        >
          重置
        </v-btn>
        <v-btn
          color="primary"
          @click="saveConfig"
          :loading="saving"
          :disabled="loading || syncing"
        >
          保存
        </v-btn>
      </v-card-actions>
    </v-card>

    <!-- 机主信息编辑弹窗（从原 SettingsView 迁入） -->
    <v-dialog v-model="showUserInfoDialog" max-width="500">
      <v-card>
        <v-card-title class="text-h5">
          <v-icon class="mr-2">mdi-account-edit</v-icon>
          {{ userInfo ? '修改机主信息' : '填写机主信息' }}
        </v-card-title>
        <v-card-subtitle class="mt-1">
          {{ userInfo ? '更新您的基本信息' : '请填写您的基本信息' }}
        </v-card-subtitle>

        <v-card-text>
          <v-form v-model="formValid">
            <v-text-field
              v-model="userInfoForm.name"
              label="姓名 *"
              placeholder="请输入您的姓名"
              variant="outlined"
              :rules="[formRules.required]"
              class="mb-2"
            />
            <v-text-field
              v-model="userInfoForm.companyName"
              label="单位名称 *"
              placeholder="请输入单位名称"
              variant="outlined"
              :rules="[formRules.required]"
              class="mb-2"
            />
            <v-text-field
              v-model="userInfoForm.departmentName"
              label="部门名称 *"
              placeholder="请输入部门名称"
              variant="outlined"
              :rules="[formRules.required]"
              class="mb-2"
            />
            <v-text-field
              v-model="userInfoForm.phone"
              label="联系方式"
              placeholder="请输入联系方式（选填）"
              variant="outlined"
              class="mb-2"
            />
            <v-text-field
              v-model="userInfoForm.workAddress"
              label="工作地点"
              placeholder="请输入工作地点（选填）"
              variant="outlined"
            />
          </v-form>
        </v-card-text>

        <v-card-actions>
          <v-spacer />
          <v-btn
            variant="text"
            @click="showUserInfoDialog = false"
            :disabled="savingUserInfo"
          >
            取消
          </v-btn>
          <v-btn
            color="primary"
            variant="elevated"
            :disabled="!formValid"
            :loading="savingUserInfo"
            @click="saveUserInfoHandler"
          >
            确认保存
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- 提示消息 -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" :timeout="3000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
