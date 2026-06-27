<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { useTheme } from 'vuetify'
import { useRouter, useRoute } from 'vue-router'
import { api, API_BASE, type SystemConfig } from '@/services/api'
import { userInfoManager, type UserInfo } from '@/services/UserInfoManager'
import { authManager } from '@/services/AuthManager'

const theme = useTheme()
const router = useRouter()
const route = useRoute()
const drawer = ref(true)
const config = ref<SystemConfig | null>(null)
const currentUserInfo = ref<UserInfo | null>(null)

const isShelllessPage = computed(() => {
  return route.name === 'PdfViewer' || route.name === 'Login'
})

// 用户信息弹窗相关
const showUserInfoDialog = ref(false)
const savingUserInfo = ref(false)
const userInfoForm = ref({
  name: '',
  companyName: '',
  departmentName: '',
  phone: '',
  workAddress: ''
})
const formValid = ref(false)
const formRules = {
  required: (v: string) => !!v?.trim() || '此项为必填项'
}

const toggleTheme = () => {
  theme.global.name.value = theme.global.current.value.dark ? 'light' : 'dark'
}

// 检查用户信息
const checkUserInfo = async () => {
  const userInfo = await userInfoManager.getUserInfo()
  currentUserInfo.value = userInfo
}

// 打开用户信息编辑弹窗
const openUserInfoDialog = () => {
  if (currentUserInfo.value) {
    userInfoForm.value = {
      name: currentUserInfo.value.user_name,
      companyName: currentUserInfo.value.company_name,
      departmentName: currentUserInfo.value.department,
      phone: currentUserInfo.value.phone || '',
      workAddress: currentUserInfo.value.work_address || ''
    }
  }
  showUserInfoDialog.value = true
}

// 保存用户信息并关闭弹窗
const saveUserInfo = async () => {
  if (!formValid.value) {
    return
  }

  savingUserInfo.value = true
  try {
    const savedInfo = await userInfoManager.saveUserInfo({
      user_name: userInfoForm.value.name.trim(),
      company_name: userInfoForm.value.companyName.trim(),
      department: userInfoForm.value.departmentName.trim(),
      phone: userInfoForm.value.phone.trim() || null,
      work_address: userInfoForm.value.workAddress.trim() || null
    })
    currentUserInfo.value = savedInfo
    showUserInfoDialog.value = false
  } catch (error) {
    console.error('Failed to save user info:', error)
  } finally {
    savingUserInfo.value = false
  }
}

const logout = async () => {
  try {
    await authManager.logout()
  } finally {
    userInfoManager.clearCache()
    currentUserInfo.value = null
    await router.push('/login')
  }
}

// 加载配置
const loadConfig = async () => {
  try {
    config.value = await api.getConfig()
  } catch (error) {
    console.error('Failed to load config:', error)
  }
}

// 是否已完成首次普查
const hasCompletedFirstScan = () => {
  return config.value?.full_inventory_time != null && config.value.full_inventory_time !== ''
}

// 点击首次普查按钮
const handleFirstScan = () => {
  router.push({ path: '/', query: { action: 'firstScan' } })
}

// 点击日常盘点按钮
const handleDailyScan = () => {
  router.push({ path: '/', query: { action: 'dailyScan' } })
}

// 三类导航待办红点 = 该角色"待处理且未读"的数量：看过(进入对应页)即清零，新待办再出现。
//  环节分工=我名下待承接/待分工的项目；任务指派=我名下待分工的环节；工作受理=我名下未完成的文件任务。
const worktaskUnread = ref(0)
const filetaskUnread = ref(0)
const recvtaskUnread = ref(0)

async function loadCount(path: string): Promise<number> {
  try {
    const r = await fetch(`${API_BASE}/centralized-projects/${path}`)
    const j = await r.json()
    if (j.success) return Number(j.data?.count) || 0
  } catch { /* 静默：manage 不可达时保持上次值 */ }
  return 0
}
async function markSeen(path: string) {
  try { await fetch(`${API_BASE}/centralized-projects/${path}`, { method: 'POST' }) } catch { /* 静默 */ }
}

// 刷新红点。跳过"当前正在查看的页面"——其红点已在进入时清零，避免刚清又被刷出来。
async function refreshBadges() {
  if (isShelllessPage.value) return
  if (route.path !== '/project-acceptance') worktaskUnread.value = await loadCount('unread-count')
  if (route.path !== '/file-task-assign') filetaskUnread.value = await loadCount('stage-unread-count')
  if (route.path !== '/file-task-receive') recvtaskUnread.value = await loadCount('task-unread-count')
}

// 进入这三个页面之一 = 看过：标记已读并清零该页红点。
async function clearBadgeFor(path: string) {
  if (isShelllessPage.value) return
  if (path === '/project-acceptance') { worktaskUnread.value = 0; await markSeen('mark-seen') }
  else if (path === '/file-task-assign') { filetaskUnread.value = 0; await markSeen('mark-stages-seen') }
  else if (path === '/file-task-receive') { recvtaskUnread.value = 0; await markSeen('mark-tasks-seen') }
}

let badgeTimer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  if (!isShelllessPage.value) {
    loadConfig()
    checkUserInfo()
  }
  refreshBadges()
  clearBadgeFor(route.path) // 直接进入这三个页面之一时即清零
  badgeTimer = setInterval(refreshBadges, 30000) // 30s 轮询：新待办自动冒红点
})

onUnmounted(() => { if (badgeTimer) clearInterval(badgeTimer) })

watch(isShelllessPage, (shellless) => {
  if (!shellless) {
    loadConfig()
    checkUserInfo()
    refreshBadges() // 登录进入主界面后立即加载红点
  }
})

// 切换路由：刷新其它页红点；进入这三个页面则清零（看过即消失）。
watch(() => route.path, (p) => {
  if (isShelllessPage.value) return
  refreshBadges()
  clearBadgeFor(p)
})

// 主导航菜单
const navItems = [
  // { title: '本机数据资源图谱', icon: 'mdi-chart-bar', to: '/stats', hint: '数据治理与归档统计图表', disabled: false },
  { title: '本机数据资源图谱', icon: 'mdi-file', to: '/' ,hint: "个人工作文件管理",disabled:false },
  {
    title: '历史数据治理',
    icon: 'mdi-database-cog-outline',
    hint: '管控文件扫描盘点、责任认领、归目保护、本机归档浏览、隐私保护与档案上报移交',
    disabled: false,
    children: [
      { title: '管控文件扫描盘点', icon: 'mdi-file-search', to: '/scan', hint: '本机文件目录资源普查与盘点' },
      { title: '扫描结果责任认领', icon: 'mdi-account-check', to: '/claim', hint: '本人私有与工作责任文件认领' },
      { title: '认领文件归档保护', icon: 'mdi-folder-lock', to: '/classify', hint: '兜底归档：处理未按模版规则、工作空间之外或历史数据治理的文件，手动选择归档目标与级别' },
      // 2026-06-01 「本机归档文件浏览」并入「档案在线阅卷」的“个人”一级 tab（保密夹/档案夹/资料夹）。路由 /classifySearch 与 ClassifySearchView.vue 暂保留作回滚后路。
      // { title: '本机归档文件浏览', icon: 'mdi-folder-eye', to: '/classifySearch', hint: '按文件级别（核心/重要/开放）分区浏览本机归档文件' },
      { title: '个人隐私保护', icon: 'mdi-shield-account', to: '/privacy', hint: '认领为个人隐私的文件清单，仅本机查看' },
      { title: '工作档案上报移交', icon: 'mdi-file-upload', to: '/report', hint: '自起草文件的上报移交' },
    ],
  },
  {
    title: '工作空间管理',
    icon: 'mdi-folder-cog-outline',
    hint: '工作文件台账、电子文件起草、自有文件本地归档与本地档案同步上传',
    disabled: false,
    children: [
      { title: '工作文件台账总览', icon: 'mdi-file-table-outline', to: '/workspace-ledger', hint: '个人工作空间全部文件台账，支持检索、统计、导出、归档' },
      { title: '电子文件起草管理', icon: 'mdi-file-edit-outline', to: '/file-drafting', hint: '新建、编辑、保存、归档电子文件，自动纳入管控体系' },
      { title: '自有文件本地归档', icon: 'mdi-folder-lock-outline', to: '/local-archive', hint: '4 个系统隐藏目录 + 自定义目录的本机自动归档' },
      { title: '本地档案同步上传', icon: 'mdi-cloud-upload-outline', to: '/archive-sync', hint: '将本地已归档文件安全上传至单位服务器，支持断点续传、校验' },
    ],
  },
  // 2026-05-27 隐藏「个人文件台账」菜单。路由 /personal-files 与 PersonalFilesView.vue 暂保留作回滚后路。
  // { title: '个人文件台账', icon: 'mdi-account-file-text', to: '/personal-files', hint: '个人工作文件入账、分级和主题总览', disabled: false },
  {
    title: '数据业务服务',
    icon: 'mdi-briefcase-outline',
    hint: '数据项目立项、项目工作分工、文件任务指派、文本工作受理、业务模版管理',
    disabled: false,
    children: [
      // 2026-06-02 立项归一：填项目信息(含定数权)+负责人→立项→同步 manage（不选模版，模版由负责人承接时选）
      { title: '数据项目立项', icon: 'mdi-clipboard-plus-outline', to: '/centralized-projects', hint: '填项目信息、定数权、负责人后立项；同步到 manage，指定负责人即可承接' },
      // 负责人承接：选模版 + 为各环节/文件标识指派责任人
      { title: '项目工作分工', icon: 'mdi-handshake-outline', to: '/project-acceptance', badge: 'worktask', hint: '作为负责人：选模版并为各工作环节指派负责人' },
      { title: '文件任务指派', icon: 'mdi-account-multiple-plus', to: '/file-task-assign', badge: 'filetask', hint: '作为工作环节负责人：为本环节每个文件任务指派参与人' },
      { title: '文本工作受理', icon: 'mdi-clipboard-check-outline', to: '/file-task-receive', badge: 'recvtask', hint: '作为文件任务参与人：待办/进行中/已完成看板，开始工作后在线编辑过程文档' },
      { title: '项目人员管理', icon: 'mdi-account-group', to: '/work-group', hint: '查看你参与项目的工作组：组长、核心成员（环节责任人）、参与人员（文件任务参与人）' },
      // 2026-06-09 三级分工级联：「我的工作事项」由 工作事项分工/文件任务指派/文件任务受理 取代，导航下线（路由保留作回滚）
      // { title: '我的工作事项', icon: 'mdi-clipboard-check-outline', to: '/my-work-items', hint: '承接后下发给我的工作环节：开始工作建目录并进工作台，干完在工作台交付' },
      { title: '业务模版管理', icon: 'mdi-file-document-multiple-outline', to: '/project-initiation', hint: '业务模版管理：新建/裁剪/编辑/发布本地模版 + 另存在线通用模版，供「数据项目立项」承接时选用（立项请到「数据项目立项」页）' },
      // 2026-06-02 暂时隐藏「归目推荐」入口（暂时用不着）。路由 /ai-classify 与 AIClassifyView.vue 保留作回滚后路。
      // { title: '归目推荐', icon: 'mdi-auto-fix', to: '/ai-classify', hint: '§4.3 规则匹配版 AI 归目（待归目文件智能挂账）' },
      // 2026-05-31 数据业务分类改由 manage 管理（scan 创作模版时下拉拉取选择）。路由 /industry-classes 与 IndustryClassesView.vue 暂保留作回滚后路。
      // { title: '数据业务分类', icon: 'mdi-tag-multiple-outline', to: '/industry-classes', hint: '行业/业务分类管理，模版的上层归类，编码自动 IND-NNN' },
      // 2026-06-02 「数据项目模版」并入「模板库」：入口关闭，功能(创作/编辑/发布/同步/删除)已迁到模板库。
      //            路由 /template-authoring（列表）与 /template-authoring/:id（树编辑器）保留——模板库「编辑结构」仍用后者。
      // { title: '数据项目模版', icon: 'mdi-file-tree', to: '/template-authoring', hint: '本地创作五层数据业务模版（项目▸事项▸任务▸标识），编码全自动' },
      // 2026-05-31 隐藏「数据业务模版总览」菜单。路由 /template-overview 与 TemplateOverviewView.vue 暂保留作回滚后路。
      // { title: '数据业务模版总览', icon: 'mdi-file-document-multiple-outline', to: '/template-overview', hint: '从 manage 拉取的可用业务模版' },
      // 2026-05-22 暂时屏蔽「数据业务项目」入口。路由 /projects / 页面 / API 保留，
      // 后续要恢复直接取消注释即可。
      // { title: '数据业务项目', icon: 'mdi-folder-multiple', to: '/projects', hint: '基于模版的项目立项与卷宗管理' },
      // 2026-05-31 隐藏「数据业务集中立项」菜单。路由 /centralized-projects 与 CentralizedProjectView.vue 暂保留作回滚后路。
      // { title: '数据业务集中立项', icon: 'mdi-clipboard-plus-outline', to: '/centralized-projects', hint: '简化的立项意向登记，与正式立项解耦' },
      // 2026-05-31 隐藏「数据项目在线承接」菜单。路由 /project-acceptance 与 ProjectAcceptanceView.vue 暂保留作回滚后路。
      // { title: '数据项目在线承接', icon: 'mdi-handshake-outline', to: '/project-acceptance', hint: '查看分配给自己的已通过项目并承接' },
      // 2026-06-09 「我的环节任务」(StageTasksView) 及其旧「文件桶工作台」(CentralizedStageWorkbenchView) 已删除——
      //            职责由 文件任务受理(看板+内联在线编辑) 取代，不再保留旧工作台代码。
      // 2026-05-31 隐藏「数据项目结项管理」菜单。路由 /project-closure 与 ProjectClosureView.vue 暂保留作回滚后路。
      // { title: '数据项目结项管理', icon: 'mdi-archive-arrow-down-outline', to: '/project-closure', hint: '本人提交的项目结项归档' },
    ],
  },
  // 2026-05-27 隐藏「资产标识底账」菜单。路由 /ledgers 与 LedgerView.vue 暂保留作回滚后路。
  // { title: '资产标识底账', icon: 'mdi-book-open-variant', to: '/ledgers', hint: '一件一号一账一链一责一处置全量底账', disabled: false },
  // 2026-05-27 隐藏「审计日志」菜单。路由 /audit-logs 与 AuditLogsView.vue 暂保留作回滚后路。
  // { title: '审计日志', icon: 'mdi-clipboard-text-clock', to: '/audit-logs', hint: '§11 模块级关键操作审计', disabled: false },
  // 2026-05-27 隐藏「核心登记」菜单。路由 /memorandum 与 MemorandumView.vue 暂保留作回滚后路。
  // { title: '核心登记', icon: 'mdi-shield-lock', to: '/memorandum', hint: '核心级资料人工登记通道', disabled: false },
  { title: '档案在线阅卷', icon: 'mdi-book-open-page-variant', to: '/borrow', hint: '部门级与单位级档案的借阅与安全管控',disabled:false},
]

// 底部菜单
const bottomNavItems = [
  { title: '设置', icon: 'mdi-cog', to: '/settings' },
]
</script>

<template>
  <v-app>
    <v-navigation-drawer v-if="!isShelllessPage" v-model="drawer" permanent>
      <v-list-item
        title="数据业务治理系统"
        subtitle="电子文件帐目管理系统"
        prepend-icon="mdi-shield-lock"
      />

      <v-divider />

      <!-- 首次普查/日常盘点按钮 -->
<!--      <div class="pa-2">-->
<!--        <v-btn-->
<!--          v-if="!hasCompletedFirstScan()"-->
<!--          color="primary"-->
<!--          block-->
<!--          prepend-icon="mdi-clipboard-search"-->
<!--          @click="handleFirstScan"-->
<!--        >-->
<!--          首次普查-->
<!--        </v-btn>-->
<!--        <v-btn-->
<!--          v-else-->
<!--          color="primary"-->
<!--          variant="tonal"-->
<!--          block-->
<!--          prepend-icon="mdi-refresh"-->
<!--          @click="handleDailyScan"-->
<!--        >-->
<!--          日常盘点-->
<!--        </v-btn>-->
<!--      </div>-->

      <v-divider />

      <v-list density="compact" nav class="nav-list">
        <template v-for="item in navItems" :key="item.title">
          <!-- 分组：children 非空时渲染 v-list-group -->
          <v-list-group v-if="item.children && item.children.length > 0" :value="item.title">
            <template v-slot:activator="{ props }">
              <v-list-item
                v-bind="props"
                :title="item.title"
                :prepend-icon="item.icon"
              />
            </template>
            <v-tooltip
              v-for="child in item.children"
              :key="child.title"
              :text="child.hint"
              location="end"
            >
              <template v-slot:activator="{ props: tipProps }">
                <v-list-item
                  v-bind="tipProps"
                  :title="child.title"
                  :prepend-icon="child.icon"
                  :to="child.to"
                >
                  <template #append v-if="(child.badge === 'worktask' && worktaskUnread > 0) || (child.badge === 'filetask' && filetaskUnread > 0) || (child.badge === 'recvtask' && recvtaskUnread > 0)">
                    <span class="nav-unread">{{ child.badge === 'worktask' ? worktaskUnread : child.badge === 'filetask' ? filetaskUnread : recvtaskUnread }}</span>
                  </template>
                </v-list-item>
              </template>
            </v-tooltip>
          </v-list-group>
          <!-- 普通叶子项 -->
          <v-tooltip v-else :text="item.hint" location="end">
            <template v-slot:activator="{ props }">
              <v-list-item
                v-bind="props"
                :disabled="item.disabled"
                :title="item.title"
                :prepend-icon="item.icon"
                :to="item.to"
              />
            </template>
          </v-tooltip>
        </template>
      </v-list>

      <template v-slot:append>
        <v-divider />
        <v-list density="compact" nav>
          <v-list-item
            v-for="item in bottomNavItems"
            :key="item.title"
            :title="item.title"
            :prepend-icon="item.icon"
            :to="item.to"
          />
        </v-list>
      </template>
    </v-navigation-drawer>

    <!-- 右上角用户信息显示 -->
    <v-app-bar  v-if="!isShelllessPage" density="compact" flat color="transparent">
      <v-spacer />
      <v-chip
        v-if="config?.workspace"
        variant="text"
        class="mr-2"
        :title="config.workspace"
        @click="router.push('/settings')"
        style="cursor: pointer; max-width: 320px;"
      >
        <v-icon start size="small">mdi-folder-outline</v-icon>
        <span class="text-body-2 text-truncate">工作空间: {{ config.workspace }}</span>
      </v-chip>
      <v-chip
        v-if="currentUserInfo"
        variant="text"
        class="mr-2"
        @click="openUserInfoDialog"
        style="cursor: pointer;"
      >
        <v-icon start size="small">mdi-account</v-icon>
        <span class="text-body-2">
          {{ currentUserInfo.user_name }} | {{ currentUserInfo.department }} | {{ currentUserInfo.company_name }}
        </span>
        <v-icon end size="small">mdi-pencil</v-icon>
      </v-chip>
      <v-btn
        v-if="currentUserInfo"
        data-test="logout-button"
        variant="text"
        size="small"
        prepend-icon="mdi-logout"
        @click="logout"
      >
        退出
      </v-btn>
    </v-app-bar>

<!--    <v-app-bar>-->
<!--      <v-app-bar-nav-icon @click="drawer = !drawer" />-->
<!--      <v-toolbar-title>数据资产保护系统</v-toolbar-title>-->
<!--      <v-spacer />-->
<!--      <v-btn-->
<!--        :icon="theme.global.current.value.dark ? 'mdi-weather-sunny' : 'mdi-weather-night'"-->
<!--        @click="toggleTheme"-->
<!--      />-->
<!--    </v-app-bar>-->

    <v-main>
      <v-container :class="{ 'pa-0': isShelllessPage }" fluid>
        <router-view />
      </v-container>
    </v-main>

    <!-- 用户信息填写弹窗 -->
    <v-dialog
      v-model="showUserInfoDialog"
      persistent
      max-width="500"
    >
      <v-card>
        <v-card-title class="text-h5">
          <v-icon class="mr-2">mdi-account-edit</v-icon>
          {{ currentUserInfo ? '修改机主信息' : '请填写机主信息' }}
        </v-card-title>
        <v-card-subtitle class="mt-1">
          {{ currentUserInfo ? '更新您的基本信息' : '首次使用，请填写您的基本信息' }}
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
            v-if="currentUserInfo"
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
            @click="saveUserInfo"
          >
            确认保存
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-app>
</template>

<style scoped>
/* 收窄二级导航缩进：Vuetify 默认分组子项缩进偏大，统一压到比父级图标略靠右即可 */
.nav-list :deep(.v-list-group__items .v-list-item) {
  padding-inline-start: 24px !important;
}

/* 导航未读红点：红圈 + 数字（待处理任务提醒，点击进入查看后清零） */
.nav-unread {
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: #e53935;
  color: #fff;
  font-size: 11px;
  line-height: 18px;
  font-weight: 700;
  text-align: center;
  display: inline-block;
  box-shadow: 0 0 0 2px rgba(229, 57, 53, .18);
}
</style>
