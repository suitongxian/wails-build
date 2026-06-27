<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authManager } from '@/services/AuthManager'
import { api, API_BASE } from '@/services/api'

const router = useRouter()
const route = useRoute()

const activeTab = ref<'login' | 'register'>('login')
const loading = ref(false)
const errorMessage = ref('')

// 服务端配置（首次进入终端的统一入口，不必再到「系统配置」里改）
// 2026-05-24：三个 URL（upload / manage / archive）合并为单一 server_endpoint
const serverDialog = ref({
  open: false,
  loading: false,
  saving: false,
  saved: false,
  server_endpoint: '',           // manage 地址：上报数据与文件
  template_server_endpoint: '',  // 模版服务器地址：同步远程模版
})

const currentServer = computed(() => serverDialog.value.server_endpoint || '尚未配置')
const currentTemplateServer = computed(() => serverDialog.value.template_server_endpoint || '尚未配置')

async function openServerDialog() {
  serverDialog.value.open = true
  serverDialog.value.loading = true
  serverDialog.value.saved = false
  try {
    const cfg = await api.getConfig()
    // 后端 GET /config 返回 server_endpoint（也兼容回显 upload_server_url 同值）
    serverDialog.value.server_endpoint = (cfg as any).server_endpoint || cfg.upload_server_url || ''
    serverDialog.value.template_server_endpoint = (cfg as any).template_server_endpoint || ''
  } finally {
    serverDialog.value.loading = false
  }
}

async function saveServerConfig() {
  serverDialog.value.saving = true
  try {
    await api.saveConfig({
      // server_endpoint：后端会同步刷三个老 key（上报数据/文件）
      server_endpoint: serverDialog.value.server_endpoint.trim().replace(/\/+$/, ''),
      // template_server_endpoint：模版同步专用，独立存储
      template_server_endpoint: serverDialog.value.template_server_endpoint.trim().replace(/\/+$/, ''),
    } as any)
    serverDialog.value.saved = true
    setTimeout(() => { serverDialog.value.open = false }, 800)
  } catch (e: any) {
    errorMessage.value = '保存服务端配置失败：' + (e?.message || String(e))
  } finally {
    serverDialog.value.saving = false
  }
}

// 进入登录页时静默拉一次当前配置，header 显示当前服务端地址
onMounted(async () => {
  try {
    const cfg = await api.getConfig()
    serverDialog.value.server_endpoint = (cfg as any).server_endpoint || cfg.upload_server_url || ''
    serverDialog.value.template_server_endpoint = (cfg as any).template_server_endpoint || ''
  } catch { /* 静默 */ }
  await loadLoginHistory()
})

const loginForm = ref({
  username: '',
  password: '',
})
const showLoginPassword = ref(false) // 登录密码"小眼睛"：默认掩码(星号)，点击可临时显示明文核对

// 快速登录：本机登录过的账号（含密码），账号框下拉可选，选中自动填充密码
interface LoginHistoryItem {
  username: string
  password: string
  display_name: string
  user_unit: string
  user_department: string
}
const loginHistory = ref<LoginHistoryItem[]>([])
// 账号下拉项：标题展示"显示名（账号）"，值为账号
const historyOptions = computed(() =>
  loginHistory.value.map((h) => ({
    title: h.display_name ? `${h.display_name}（${h.username}）` : h.username,
    value: h.username,
  })),
)

async function loadLoginHistory() {
  try {
    const r = await fetch(`${API_BASE}/auth/login-history`)
    const j = await r.json()
    if (j.success) loginHistory.value = j.data || []
  } catch { /* 静默：拿不到历史不影响手输登录 */ }
}

// 账号变化（选历史项或手输）：命中历史账号则自动填充密码
function onUsernameChange(val: string) {
  const name = (val || '').trim()
  loginForm.value.username = name
  const hit = loginHistory.value.find((h) => h.username === name)
  if (hit) loginForm.value.password = hit.password
}

async function removeHistory(username: string) {
  try {
    await fetch(`${API_BASE}/auth/login-history/delete`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username }),
    })
    loginHistory.value = loginHistory.value.filter((h) => h.username !== username)
  } catch { /* 静默 */ }
}

const registerForm = ref({
  username: '',
  password: '',
  confirmPassword: '',
  displayName: '',
  userUnit: '',
  userDepartment: '',
  phone: '',
})

const redirectTarget = computed(() => {
  const redirect = route.query.redirect
  return typeof redirect === 'string' && redirect.startsWith('/') ? redirect : '/'
})

const required = (value: string) => Boolean(value?.trim()) || '此项为必填项'

async function submitLogin() {
  errorMessage.value = ''
  loading.value = true
  try {
    await authManager.login({
      username: loginForm.value.username.trim(),
      password: loginForm.value.password,
    })
    await router.push(redirectTarget.value)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '登录失败'
  } finally {
    loading.value = false
  }
}

async function submitRegister() {
  errorMessage.value = ''
  if (registerForm.value.password !== registerForm.value.confirmPassword) {
    errorMessage.value = '两次输入的密码不一致'
    return
  }

  loading.value = true
  try {
    await authManager.register({
      username: registerForm.value.username.trim(),
      password: registerForm.value.password,
      display_name: registerForm.value.displayName.trim(),
      user_unit: registerForm.value.userUnit.trim(),
      user_department: registerForm.value.userDepartment.trim(),
      phone: registerForm.value.phone.trim() || null,
    })
    await router.push(redirectTarget.value)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '注册失败'
  } finally {
    loading.value = false
  }
}

defineExpose({
  loginForm,
  registerForm,
  submitLogin,
  submitRegister,
  loginHistory,
  historyOptions,
  loadLoginHistory,
  onUsernameChange,
  removeHistory,
  showLoginPassword,
})
</script>

<template>
  <div class="login-page">
    <section class="login-identity">
      <div class="identity-mark">
        <v-icon size="34">mdi-shield-key</v-icon>
      </div>
      <div>
        <p class="identity-kicker">个人电子文件数字助理</p>
        <h1>数据业务治理系统</h1>
        <p class="identity-copy">
          账号由管理平台统一登记，终端只同步当前会话和本机操作人镜像。
        </p>
      </div>
      <div class="identity-grid">
        <div>
          <span>账号来源</span>
          <strong>管理平台</strong>
        </div>
        <div>
          <span>本机职责</span>
          <strong>扫描归目</strong>
        </div>
        <div>
          <span>数据闭环</span>
          <strong>项目档案</strong>
        </div>
      </div>
    </section>

    <section class="login-panel">
      <div class="login-panel-head">
        <div>
          <p class="panel-kicker">账户接入</p>
          <h2>登录或注册</h2>
        </div>
        <v-btn
          variant="text"
          size="small"
          prepend-icon="mdi-server-network"
          color="primary"
          @click="openServerDialog"
        >
          服务端配置
        </v-btn>
      </div>

      <!-- 当前服务端摘要（一目了然，避免输错地址都不知道） -->
      <div class="server-summary">
        <v-icon size="14" class="mr-1" color="grey">mdi-link-variant</v-icon>
        <span class="text-caption text-medium-emphasis">数据/文件上报：</span>
        <code class="text-caption ml-1">{{ currentServer }}</code>
      </div>
      <div class="server-summary">
        <v-icon size="14" class="mr-1" color="grey">mdi-file-tree</v-icon>
        <span class="text-caption text-medium-emphasis">模版同步：</span>
        <code class="text-caption ml-1">{{ currentTemplateServer }}</code>
      </div>

      <v-alert
        v-if="errorMessage"
        type="error"
        variant="tonal"
        density="compact"
        class="mb-4"
      >
        {{ errorMessage }}
      </v-alert>

      <v-tabs v-model="activeTab" color="primary" density="comfortable" class="mb-5">
        <v-tab value="login" data-test="login-tab">登录</v-tab>
        <v-tab value="register" data-test="register-tab">注册</v-tab>
      </v-tabs>

      <div
        data-test="login-form-window"
        class="login-form-window login-form-window--auto-height"
      >
        <v-form
          v-if="activeTab === 'login'"
          data-test="login-form"
          @submit.prevent="submitLogin"
        >
          <!-- 账号框：可手动输入，也可点开下拉选本机登录过的账号（选中自动填密码） -->
          <v-combobox
            :model-value="loginForm.username"
            @update:model-value="onUsernameChange"
            :items="historyOptions"
            item-title="title"
            item-value="value"
            :return-object="false"
            label="登录账号"
            placeholder="输入账号，或点开选择历史登录账号"
            prepend-inner-icon="mdi-account"
            variant="outlined"
            density="comfortable"
            :rules="[required]"
            class="mb-2"
            data-test="login-username"
          >
            <template #item="{ props, item }">
              <v-list-item v-bind="props" :title="item.title">
                <template #append>
                  <v-btn
                    icon="mdi-close" size="x-small" variant="text" color="grey"
                    data-test="history-remove"
                    @click.stop="removeHistory((item.raw as any).value)"
                  />
                </template>
              </v-list-item>
            </template>
          </v-combobox>
          <v-text-field
            v-model="loginForm.password"
            label="登录密码"
            prepend-inner-icon="mdi-lock"
            :type="showLoginPassword ? 'text' : 'password'"
            :append-inner-icon="showLoginPassword ? 'mdi-eye-off' : 'mdi-eye'"
            @click:append-inner="showLoginPassword = !showLoginPassword"
            variant="outlined"
            density="comfortable"
            :rules="[required]"
            class="mb-4"
            data-test="login-password"
          />
          <v-btn
            type="submit"
            color="primary"
            size="large"
            block
            :loading="loading"
            prepend-icon="mdi-login"
          >
            进入工作台
          </v-btn>
        </v-form>

        <v-form
          v-if="activeTab === 'register'"
          data-test="register-form"
          @submit.prevent="submitRegister"
        >
          <v-text-field
            v-model="registerForm.username"
            label="登录账号"
            prepend-inner-icon="mdi-account-plus"
            variant="outlined"
            density="comfortable"
            :rules="[required]"
            class="mb-2"
          />
          <v-text-field
            v-model="registerForm.displayName"
            label="显示姓名"
            prepend-inner-icon="mdi-card-account-details"
            variant="outlined"
            density="comfortable"
            :rules="[required]"
            class="mb-2"
          />
          <v-text-field
            v-model="registerForm.userUnit"
            label="所属单位"
            prepend-inner-icon="mdi-domain"
            variant="outlined"
            density="comfortable"
            :rules="[required]"
            class="mb-2"
          />
          <v-text-field
            v-model="registerForm.userDepartment"
            label="所属部门"
            prepend-inner-icon="mdi-office-building"
            variant="outlined"
            density="comfortable"
            :rules="[required]"
            class="mb-2"
          />
          <v-text-field
            v-model="registerForm.phone"
            label="联系电话"
            prepend-inner-icon="mdi-phone"
            variant="outlined"
            density="comfortable"
            class="mb-2"
          />
          <v-text-field
            v-model="registerForm.password"
            label="登录密码"
            prepend-inner-icon="mdi-lock"
            type="password"
            variant="outlined"
            density="comfortable"
            :rules="[required]"
            class="mb-2"
          />
          <v-text-field
            v-model="registerForm.confirmPassword"
            label="确认密码"
            prepend-inner-icon="mdi-lock-check"
            type="password"
            variant="outlined"
            density="comfortable"
            :rules="[required]"
            class="mb-4"
          />
          <v-btn
            type="submit"
            color="primary"
            size="large"
            block
            :loading="loading"
            prepend-icon="mdi-account-check"
          >
            创建并进入
          </v-btn>
        </v-form>
      </div>
    </section>

    <!-- 服务端配置对话框（登录前的统一入口） -->
    <v-dialog v-model="serverDialog.open" max-width="600" persistent>
      <v-card>
        <v-card-title class="d-flex align-center">
          <v-icon class="mr-2">mdi-server-network</v-icon>
          服务端配置
        </v-card-title>
        <v-card-subtitle>登录前先填好两个服务端地址：数据/文件上报走 manage，模版同步走模版管理平台。</v-card-subtitle>
        <v-divider />
        <v-card-text>
          <v-progress-linear v-if="serverDialog.loading" indeterminate color="primary" />
          <template v-else>
            <v-text-field
              v-model="serverDialog.server_endpoint"
              label="数据/文件上报地址（manage）*"
              placeholder="http://47.95.233.47:19091"
              variant="outlined"
              density="compact"
              prepend-inner-icon="mdi-cloud-upload-outline"
              hint="上报数据与文件到管理后台（文件上传 / 归档上报）"
              persistent-hint
            />
            <v-text-field
              v-model="serverDialog.template_server_endpoint"
              label="模版同步地址（模版管理平台）*"
              placeholder="http://47.95.233.47:19092"
              variant="outlined"
              density="compact"
              class="mt-4"
              prepend-inner-icon="mdi-file-tree"
              hint="同步远程数据业务模版（template-manage，端口通常为 19092）"
              persistent-hint
            />
            <v-alert
              v-if="serverDialog.saved"
              type="success"
              variant="tonal"
              density="compact"
              class="mt-3"
            >
              已保存
            </v-alert>
          </template>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" :disabled="serverDialog.saving" @click="serverDialog.open = false">取消</v-btn>
          <v-btn color="primary" :loading="serverDialog.saving" @click="saveServerConfig">保存</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.login-page {
  min-height: calc(100vh - 32px);
  display: grid;
  grid-template-columns: minmax(320px, 0.95fr) minmax(360px, 1.05fr);
  background: #f5f7fb;
}

.login-identity {
  display: flex;
  min-height: 100%;
  flex-direction: column;
  justify-content: space-between;
  gap: 48px;
  padding: 56px;
  color: #f8fafc;
  background:
    linear-gradient(135deg, rgba(9, 28, 55, 0.94), rgba(15, 66, 84, 0.9)),
    url('https://images.unsplash.com/photo-1450101499163-c8848c66ca85?auto=format&fit=crop&w=1400&q=80');
  background-size: cover;
  background-position: center;
}

.identity-mark {
  width: 60px;
  height: 60px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.14);
  border: 1px solid rgba(255, 255, 255, 0.24);
}

.identity-kicker,
.panel-kicker {
  margin: 0 0 10px;
  font-size: 13px;
  letter-spacing: 0;
  opacity: 0.78;
}

.login-identity h1 {
  margin: 0;
  font-size: 40px;
  line-height: 1.15;
  letter-spacing: 0;
}

.identity-copy {
  max-width: 420px;
  margin: 18px 0 0;
  line-height: 1.8;
  color: rgba(248, 250, 252, 0.82);
}

.identity-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.identity-grid div {
  padding: 16px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.16);
}

.identity-grid span,
.identity-grid strong {
  display: block;
}

.identity-grid span {
  font-size: 12px;
  color: rgba(248, 250, 252, 0.72);
}

.identity-grid strong {
  margin-top: 6px;
  font-size: 16px;
}

.login-panel {
  align-self: center;
  width: min(480px, calc(100% - 48px));
  margin: 48px auto;
  padding: 32px;
  border-radius: 8px;
  background: #ffffff;
  border: 1px solid #dde4ee;
  box-shadow: 0 20px 60px rgba(15, 23, 42, 0.12);
}

.login-panel-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
}

.login-panel h2 {
  margin: 0;
  font-size: 28px;
  line-height: 1.25;
  letter-spacing: 0;
  color: #111827;
}

.login-form-window {
  padding-top: 8px;
  min-height: 0;
}

.server-summary {
  display: flex;
  align-items: center;
  margin-bottom: 16px;
  padding: 8px 12px;
  background: #f5f7fb;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
}

@media (max-width: 860px) {
  .login-page {
    grid-template-columns: 1fr;
  }

  .login-identity {
    min-height: auto;
    padding: 32px;
    gap: 28px;
  }

  .login-identity h1 {
    font-size: 32px;
  }

  .identity-grid {
    grid-template-columns: 1fr;
  }

  .login-panel {
    width: calc(100% - 32px);
    margin: 16px auto 32px;
    padding: 24px;
  }
}
</style>
