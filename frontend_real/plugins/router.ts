import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'
import { authManager } from '@/services/AuthManager'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/LoginView.vue'),
  },
  {
    path: '/',
    name: 'Files',
    component: () => import('@/views/FilesView.vue'),
  },
  {
    path: '/scan',
    name: 'Scan',
    component: () => import('@/views/ScanView.vue'),
  },
  {
    path: '/classify',
    name: 'Classify',
    component: () => import('@/views/ClassifyView.vue'),
  },
  {
    path: '/personal-files',
    name: 'PersonalFiles',
    component: () => import('@/views/PersonalFilesView.vue'),
  },
  {
    path: '/classifySearch',
    name: 'ClassifySearch',
    component: () => import('@/views/ClassifySearchView.vue'),
  },
  {
    path: '/claim',
    name: 'Claim',
    component: () => import('@/views/ClaimView.vue'),
  },
  {
    path: '/user-info',
    name: 'UserInfo',
    component: () => import('@/views/UserInfoView.vue'),
  },
  {
    path: '/report',
    name: 'Report',
    component: () => import('@/views/ReportView.vue'),
  },
  {
    path: '/borrow',
    name: 'Borrow',
    component: () => import('@/views/BorrowView.vue'),
  },
  {
    path: '/pdf-viewer',
    name: 'PdfViewer',
    component: () => import('@/views/PdfViewer.vue'),
  },
  {
    path: '/settings',
    name: 'Settings',
    component: () => import('@/views/SystemConfigView.vue'),
  },
  {
    path: '/privacy',
    name: 'PrivacyProtection',
    component: () => import('@/views/PrivacyProtectionView.vue'),
  },
  {
    path: '/stats',
    name: 'Stats',
    component: () => import('@/views/StatsView.vue'),
  },
  {
    path: '/projects',
    name: 'Projects',
    component: () => import('@/views/ProjectsListView.vue'),
  },
  {
    path: '/projects/new',
    name: 'ProjectWizard',
    component: () => import('@/views/ProjectWizardView.vue'),
  },
  {
    path: '/projects/:id',
    name: 'ProjectWorkbench',
    component: () => import('@/views/ProjectWorkbenchView.vue'),
  },
  {
    path: '/ledgers',
    name: 'Ledgers',
    component: () => import('@/views/LedgerView.vue'),
  },
  {
    path: '/audit-logs',
    name: 'AuditLogs',
    component: () => import('@/views/AuditLogsView.vue'),
  },
  {
    path: '/ai-classify',
    name: 'AIClassify',
    component: () => import('@/views/AIClassifyView.vue'),
  },
  {
    path: '/memorandum',
    name: 'Memorandum',
    component: () => import('@/views/MemorandumView.vue'),
  },
  {
    path: '/template-overview',
    name: 'TemplateOverview',
    component: () => import('@/views/TemplateOverviewView.vue'),
  },
  {
    path: '/my-work-items',
    name: 'MyWorkItems',
    component: () => import('@/views/MyWorkItemsView.vue'),
  },
  {
    path: '/project-initiation',
    name: 'ProjectInitiation',
    component: () => import('@/views/ProjectInitiationView.vue'),
  },
  {
    path: '/template-authoring',
    name: 'TemplateAuthoring',
    component: () => import('@/views/TemplateAuthoringView.vue'),
  },
  {
    path: '/template-authoring/:id',
    name: 'TemplateTreeEditor',
    component: () => import('@/views/TemplateTreeEditorView.vue'),
  },
  {
    path: '/industry-classes',
    name: 'IndustryClasses',
    component: () => import('@/views/IndustryClassesView.vue'),
  },
  {
    path: '/centralized-projects',
    name: 'CentralizedProjects',
    component: () => import('@/views/CentralizedProjectView.vue'),
  },
  {
    path: '/project-acceptance',
    name: 'ProjectAcceptance',
    component: () => import('@/views/ProjectAcceptanceView.vue'),
  },
  {
    path: '/project-closure',
    name: 'ProjectClosure',
    component: () => import('@/views/ProjectClosureView.vue'),
  },
  // 工作空间管理（演示页面，仅展示用，无实际逻辑）
  {
    path: '/workspace-ledger',
    name: 'WorkspaceLedger',
    component: () => import('@/views/WorkspaceLedgerView.vue'),
  },
  {
    path: '/file-drafting',
    name: 'FileDrafting',
    component: () => import('@/views/FileDraftingView.vue'),
  },
  {
    path: '/local-archive',
    name: 'LocalArchive',
    component: () => import('@/views/LocalArchiveView.vue'),
  },
  {
    path: '/archive-sync',
    name: 'ArchiveSync',
    component: () => import('@/views/ArchiveSyncView.vue'),
  },
  {
    path: '/file-task-assign',
    name: 'FileTaskAssign',
    component: () => import('@/views/FileTaskAssignView.vue'),
  },
  {
    path: '/file-task-receive',
    name: 'FileTaskReceive',
    component: () => import('@/views/FileTaskReceiveView.vue'),
  },
  {
    path: '/work-group',
    name: 'WorkGroup',
    component: () => import('@/views/WorkGroupView.vue'),
  },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

const publicPaths = new Set(['/login', '/pdf-viewer'])

router.beforeEach(async (to) => {
  if (publicPaths.has(to.path)) {
    return true
  }

  const session = await authManager.getSession()
  if (!session?.token || !session.user) {
    return {
      path: '/login',
      query: {
        redirect: to.fullPath,
      },
    }
  }

  return true
})

export default router
