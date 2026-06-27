/**
 * 前端 API 服务层
 * 封装与后端 HTTP API 的通信
 */

export const API_BASE = 'http://127.0.0.1:3001'

export interface FileItem {
  data_distribution_id: number
  path: string
  data_type: number
  scan_found_count: number
  content_sign: string
  file_suffix: string | null
  file_magic: string | null
  file_create_time: string | null
  file_update_time: string | null
  file_read_time: string | null
  file_size: number
  file_hide: number
  upload_state: number  // 0.未上传 1.已上传 2.副本上传 3.上传失败 4.无需归档
  ip: string
  mac_address: string
  scan_time: string
  create_time: string
  update_time: string
  copy_count: number
}

/**
 * 归档管理文件项（包含重要级别）
 */
export interface ArchiveManagementFileItem extends FileItem {
  importance_level?: number  // 0=未分类 1=核心 2=重要 3=开放 4=隐私
}

export interface SystemConfig {
  workspace: string | null
  full_inventory_time: string | null
  daily_scan_interval: number
  last_scan_time: string | null
  control_type: string | null
  scan_area_path: string | null
  scan_exclude_dir: string | null
  upload_server_url: string | null
  last_sync_time: string | null
  home_dir: string
  similarity_same_content?: number
  similarity_process_version?: number
  similarity_derived?: number
  similarity_image?: number
  similarity_filename?: number
  similarity_feature?: number
  // 数据业务模版 V1 配置
  project_root?: string | null
  manage_endpoint?: string | null
  manage_token?: string | null
  archive_endpoint?: string | null
  // 统一服务端地址（上报数据/文件）+ 模版管理平台地址（同步远程模版）
  server_endpoint?: string | null
  template_server_endpoint?: string | null
  // 认领家族策略
  claim_family_default_policy?: 'same_content_only' | 'all' | 'none'
  claim_family_skip_dialog?: 'true' | 'false'
  // 特征预计算开关
  feature_precompute_enabled?: 'true' | 'false'
}

// 归档申请表
export interface ArchiveApplication {
  applicant_unit: string          // 申请人单位
  applicant_department: string    // 申请人部门
  applicant_name: string          // 申请人姓名
  applicant_contact: string       // 联系方式
  archive_file_name: string       // 归档文件名称
  archive_file_category: string   // 归档文件类别
  archive_file_hash: string       // 文件特征值
  application_time: string        // 申请时间
  content_title: string           // 内容标题
  content_summary: string         // 内容摘要
  data_classification: '核心' | '重要' | '一般' | '公开'  // 数据定级
  protection_method: 1 | 2        // 保护方式
  share_range: string             // 共享范围
}

export interface ArchiveResult {
  success: boolean
  message?: string
  data?: {
    id: number
    filePath: string
    fileExists: boolean
  }
}

export interface FileQueryParams {
  search?: string
  workspaceFilter?: 'inside' | 'outside' | 'all'
  survivalFilter?: 'new' | 'deleted' | 'normal' | 'all'
  // accessTimeFilter 以 full_inventory_time 为界做后端过滤：
  //   new=普查后新登记的文件；history=普查前历史文件；all=不过滤
  accessTimeFilter?: 'new' | 'history' | 'all'
  page?: number
  pageSize?: number
  noPagination?: boolean
}

/**
 * 归档管理查询参数
 */
export interface ArchiveManagementQueryParams {
  search?: string
  archiveType?: 'pending' | 'core' | 'important' | 'open'  // 归档类型
  importanceLevelFilter?: number  // 重要程度过滤: 1=核心 2=重要 3=开放 (仅对 pending 类型有效)
  page?: number
  pageSize?: number
}

export interface ScanProgressEvent {
  type: 'progress' | 'complete' | 'error'
  taskId?: number
  phase?: string
  scannedCount: number
  totalCount: number
  currentFile?: string
  elapsedMs: number
  success?: boolean
  errorMessage?: string
}

export interface StatisticsGrowth {
  lastCount: number
  currentCount: number
  growthCount: number
  growthRate: number  // 增涨率，百分比形式，如 5.5 表示 5.5%
}

export interface FileStatisticsComparison {
  workspaceStatistics: StatisticsGrowth    // 工作空间文件数统计
  nonHistoryStatistics: StatisticsGrowth   // 非历史文件数统计
  historyStatistics: StatisticsGrowth      // 历史文件数统计
  hasComparison: boolean                   // 是否有对比数据（至少需要两条记录）
}

// 用户信息
export interface UserInfo {
  id: number
  company_name: string
  user_name: string
  department: string
  ip: string
  mac_address: string
  work_address: string | null
  phone: string | null
  create_time: string
  update_time: string
}

export interface SaveUserInfoParams {
  company_name: string
  user_name: string
  department: string
  phone?: string | null
  work_address?: string | null
}

export interface AuthUser {
  id: number
  username: string
  display_name: string
  user_unit: string
  user_department: string
  phone: string | null
  role: string
  status: string
  last_login_time?: string | null
}

export interface AuthSession {
  authenticated?: boolean
  token?: string
  user?: AuthUser
}

export interface LoginParams {
  username: string
  password: string
  manage_endpoint?: string | null
}

export interface RegisterParams {
  username: string
  password: string
  display_name: string
  user_unit: string
  user_department: string
  phone?: string | null
  manage_endpoint?: string | null
}

// 信息资源
export interface DataResource {
  data_resources_id: number
  content_sign: string
  source_count: number
  workspace_source_count: number
  first_create_time: string
  resources_name: string | null
  resources_desc: string | null
  content_subject: string | null
  content_type: string | null
  file_magic: string | null
  is_claimed: number
  claim_status: number  // 0=未分类 1=个人隐私 2=个人工作 3=非责任类 4=已忽略
  importance_level: number  // 0=未分类 1=核心 2=重要 3=开放 4=隐私
  claim_time: string | null
  claimant_name: string | null
  claimant_unit: string | null
  data_level: string | null
  data_share: string | null
  family_id: number | null
  family_relation: string | null
  family_score: number | null
  family_member_count: number
  family_same_content_count: number
  family_process_version_count: number
  family_derived_count: number
  create_time: string
  update_time: string
  disable: number
  // 代表性物理路径：同 content_sign 在 data_distributing 里最早入库的那条路径，
  // 用于资源列表 hover tooltip 显示完整路径 + 副本弹窗剔除避免重复展示
  primary_path?: string | null
  // 2026-05-27 任一关联 distributing 行 suspect_non_personal=1 即为 1，行级 ⚠ + 一键忽略
  suspect_non_personal?: number
}

export interface SimilarityTask {
  task_id: number
  task_state: 'pending' | 'running' | 'succeed' | 'failed'
  phase: string | null
  input_count: number
  family_count: number
  member_count: number
  error_message: string | null
  start_time: string
  end_time: string | null
}

export interface AnalyzePreview {
  cache_miss_count: number
  family_stale: boolean  // 家族表与当前文件集合不一致（首次未跑过 / 上次跑完又扫描了）
  last_run_at: string | null
  last_run_duration_sec: number
}

export interface FamilyMemberDetail {
  data_resources_id: number
  content_sign: string
  resources_name: string | null
  source_count: number
  family_id: number | null
  family_relation: string | null
  family_score: number | null
  data_distribution_id: number | null
  path: string | null
  ip: string | null
  // batch-members 扩展字段
  claim_status?: number
  claimant_name?: string | null
  claim_time?: string | null
}

export interface FamilyMembersResponse {
  family_id: number
  primary_resource: DataResource | null
  groups: {
    primary?: FamilyMemberDetail[]
    same_content?: FamilyMemberDetail[]
    process_version?: FamilyMemberDetail[]
    derived?: FamilyMemberDetail[]
    [key: string]: FamilyMemberDetail[] | undefined
  }
  total_members: number
}

export interface DataResourcesQueryParams {
  page?: number
  pageSize?: number
  claimStatusFilter?: number
  claimStatusIn?: number[]  // 多个认领状态过滤，如 [1, 2] 表示只查询个人隐私或个人工作
  importanceLevelFilter?: number
  search?: string
  businessTypeFilter?: 'workspace' | 'new_access' | 'history_inventory' | null
  groupByFamily?: boolean  // false 时不折叠相似度家族，展开看全部副本
}

export interface DataResourcesPageResult {
  resources: DataResource[]
  total: number
  page: number
  pageSize: number
}

export interface BatchClaimParams {
  ids: number[]
  is_claimed: number
  claim_status: number
  claimant_name: string
  claimant_unit: string
}

export interface BatchClassifyParams {
  ids: number[]
  importance_level: number  // 0=未分类 1=核心 2=重要 3=开放 4=隐私
}

export interface SingleClassifyParams {
  data_resources_id: number
  importance_level: number  // 1=保密柜(核心要件) 2=档案柜(重要文件) 3=资料柜(一般文件)
  resources_name?: string
  resources_desc?: string
  content_subject?: string
}

// 信息资源统计结果
export interface ResourcesStatistics {
  totalFileCount: number            // 总文件数
  workspaceTotalCount: number       // 工作空间总文件数
  historyFileCount: number          // 历史文件总数
  nonHistoryFileCount: number       // 非历史文件总数
  workspaceClaimedCount: number     // 工作空间文件认领数
  historyClaimedCount: number       // 历史文件认领数
  nonHistoryClaimedCount: number    // 非历史文件认领数
  workspacePendingClassifyCount: number      // 工作空间待归类保护数：claim_status==2 && importance_level==0 && workspace_source_count > 0
  historyPendingClassifyCount: number       // 历史文件待归类保护数：claim_status==2 && importance_level==0 && first_create_time < 历史封帐时间
  nonHistoryPendingClassifyCount: number     // 非历史文件待归类保护数：claim_status==2 && importance_level==0 && first_create_time > 历史封帐时间
  unclassifiedCount: number          // 未分类文件数
  coreCount: number                 // 核心文件数
  importantCount: number            // 重要文件数
  openCount: number                 // 开放文件数
  privacyCount: number              // 隐私文件数
}

// 归档文件
export interface ArchiveFile {
  id: number
  application_name: string
  applicant_unit: string
  applicant_department: string
  applicant_name: string
  applicant_contact: string
  archive_file_name: string
  archive_file_category: string
  archive_file_hash: string
  application_time: string
  content_title: string
  content_summary: string | null
  data_classification: '核心' | '重要' | '一般' | '公开'
  protection_method: number
  create_time: string
  update_time: string
}

// 一键归档上报到云端的部门/单位柜室文件（元数据登记，文件实体在原终端）
export interface QuickArchiveCabinetFile {
  id: number
  project_code: string
  project_name: string
  scope: 'department' | 'unit'
  file_name: string
  bucket: string | null
  sensitivity_level: 'core' | 'important' | 'general'
  target_folder: string | null
  storage_tier: string | null
  storage_location: string | null
  checksum: string
  file_size: number | null
  custody_note: string | null
  archived_at: string
}

// 本机个人{级别}文件夹下的一键归档文件
export interface PersonalArchiveFile {
  file_name: string
  project_name: string
  sensitivity_level: 'core' | 'important' | 'general'
  folder: string
  file_size: number
  archived_at: string
}

export interface ArchiveFileQueryParams {
  page?: number
  pageSize?: number
  applicant_name?: string
}

export interface ArchiveFilePageResult {
  list: ArchiveFile[]
  total: number
  page: number
  pageSize: number
}

// 借阅申请参数
export interface BorrowDownloadParams {
  archive_id: number
  borrower_name: string
  borrower_department: string
  borrow_reason?: string
  borrow_method: 1 | 2  // 1=在线查看 2=下载
}

class ApiService {
  /**
   * 获取文件列表
   */
  async getFiles(params: FileQueryParams = {}): Promise<{ files: FileItem[], total: number }> {
    const searchParams = new URLSearchParams()

    if (params.search) searchParams.set('search', params.search)
    if (params.workspaceFilter) searchParams.set('workspaceFilter', params.workspaceFilter)
    if (params.survivalFilter) searchParams.set('survivalFilter', params.survivalFilter)
    if (params.accessTimeFilter) searchParams.set('accessTimeFilter', params.accessTimeFilter)
    if (params.page) searchParams.set('page', String(params.page))
    if (params.pageSize) searchParams.set('pageSize', String(params.pageSize))
    if (params.noPagination) searchParams.set('noPagination', 'true')

    const response = await fetch(`${API_BASE}/files?${searchParams}`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch files')
    }

    return result.data
  }

  /**
   * 获取指定内容签名的所有副本
   */
  async getCopies(contentSign: string): Promise<{ copies: FileItem[], count: number }> {
    const response = await fetch(`${API_BASE}/files/${encodeURIComponent(contentSign)}/copies`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch copies')
    }

    return result.data
  }

  /**
   * 获取系统配置
   */
  async getConfig(): Promise<SystemConfig> {
    const response = await fetch(`${API_BASE}/config`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch config')
    }

    return result.data
  }

  /**
   * 保存系统配置
   */
  async saveConfig(config: Partial<SystemConfig>): Promise<void> {
    const response = await fetch(`${API_BASE}/config`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(config),
    })
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to save config')
    }
  }

  /**
   * 触发扫描。后端立即创建任务并异步执行，返回新任务的 id；
   * 前端通过轮询 getScanTaskDetail(taskId) 获取实时进度与最终状态。
   */
  async triggerScan(options: {
    scanMode: 'FULL_INVENTORY' | 'DAILY_CHECK' | 'TARGETED_SCAN'
  }): Promise<{ taskId: number }> {
    const response = await fetch(`${API_BASE}/scan/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ scan_mode: options.scanMode }),
    })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to start scan')
    }
    return { taskId: result.data?.taskId || 0 }
  }

  /**
   * 查询当前正在运行的扫描任务（task_state='run'），无则返回 null。
   * 进入扫描页面时调一次，决定是否启动轮询。
   */
  async getRunningScanTask(): Promise<ScanTask | null> {
    const response = await fetch(`${API_BASE}/scan/current`)
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch running task')
    }
    return result.data || null
  }

  /**
   * 终止当前正在运行的扫描。
   */
  async stopScan(): Promise<void> {
    const response = await fetch(`${API_BASE}/scan/stop`, { method: 'POST' })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to stop scan')
    }
  }

  /**
   * 获取文件统计对比数据
   */
  async getStatistics(): Promise<FileStatisticsComparison> {
    const response = await fetch(`${API_BASE}/statistics`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch statistics')
    }

    return result.data
  }

  /**
   * 获取信息资源统计数据
   */
  async getResourcesStatistics(): Promise<ResourcesStatistics> {
    const response = await fetch(`${API_BASE}/resources/statistics`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch resources statistics')
    }

    return result.data
  }

  /**
   * 健康检查
   */
  async healthCheck(): Promise<boolean> {
    try {
      const response = await fetch(`${API_BASE}/health`)
      const result = await response.json()
      return result.success && result.status === 'healthy'
    } catch {
      return false
    }
  }

  /**
   * 分页获取文件列表（用于归类保护页面）
   */
  async getFilesPaginated(params: FileQueryParams = {}): Promise<{ files: FileItem[], total: number, page: number, pageSize: number }> {
    const searchParams = new URLSearchParams()

    if (params.search) searchParams.set('search', params.search)
    if (params.workspaceFilter) searchParams.set('workspaceFilter', params.workspaceFilter)
    if (params.survivalFilter) searchParams.set('survivalFilter', params.survivalFilter)
    if (params.accessTimeFilter) searchParams.set('accessTimeFilter', params.accessTimeFilter)
    searchParams.set('page', String(params.page || 1))
    searchParams.set('pageSize', String(params.pageSize || 50))

    const response = await fetch(`${API_BASE}/files?${searchParams}`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch files')
    }

    return result.data
  }

  /**
   * 归档文件上传
   * @param filePath 本地文件路径
   * @param archiveApplication 归档申请表
   */
  async archiveFile(filePath: string, archiveApplication: ArchiveApplication): Promise<ArchiveResult> {
    const response = await fetch(`${API_BASE}/archive`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        filePath,
        archiveApplication
      }),
    })

    const result = await response.json()
    return result
  }

  /**
   * 获取用户信息
   */
  async getUserInfo(): Promise<UserInfo | null> {
    const response = await fetch(`${API_BASE}/user-info`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch user info')
    }

    return result.data
  }

  /**
   * 保存用户信息
   */
  async saveUserInfo(params: SaveUserInfoParams): Promise<UserInfo> {
    const response = await fetch(`${API_BASE}/user-info`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to save user info')
    }

    return result.data
  }

  async login(params: LoginParams): Promise<AuthSession> {
    const response = await fetch(`${API_BASE}/auth/login`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to login')
    }
    return result.data
  }

  async register(params: RegisterParams): Promise<AuthSession> {
    const response = await fetch(`${API_BASE}/auth/register`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to register')
    }
    return result.data
  }

  async getAuthSession(): Promise<AuthSession> {
    const response = await fetch(`${API_BASE}/auth/session`)
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch auth session')
    }
    return result.data
  }

  async logout(): Promise<void> {
    const response = await fetch(`${API_BASE}/auth/logout`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
    })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to logout')
    }
  }

  /**
   * 获取信息资源列表（分页）
   */
  async getResources(params: DataResourcesQueryParams = {}): Promise<DataResourcesPageResult> {
    const searchParams = new URLSearchParams()

    if (params.page) searchParams.set('page', String(params.page))
    if (params.pageSize) searchParams.set('pageSize', String(params.pageSize))
    if (params.claimStatusFilter !== undefined) searchParams.set('claimStatusFilter', String(params.claimStatusFilter))
    if (params.claimStatusIn && params.claimStatusIn.length > 0) {
      searchParams.set('claimStatusIn', params.claimStatusIn.join(','))
    }
    if (params.importanceLevelFilter !== undefined) searchParams.set('importanceLevelFilter', String(params.importanceLevelFilter))
    if (params.businessTypeFilter) searchParams.set('businessTypeFilter', params.businessTypeFilter)
    if (params.search) searchParams.set('search', params.search)
    if (params.groupByFamily === false) searchParams.set('groupByFamily', 'false')

    const response = await fetch(`${API_BASE}/resources?${searchParams}`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch resources')
    }

    return result.data
  }

  /**
   * 批量认领资源
   */
  async batchClaim(params: BatchClaimParams): Promise<{ updatedCount: number }> {
    const response = await fetch(`${API_BASE}/resources/claim`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to claim resources')
    }

    return result.data
  }

  /**
   * 拉取当前 tab 范围内疑似非个人文件统计 + 前 10 条样本路径。
   * 给认领页顶部横幅 + 一键忽略确认对话框用。
   */
  async getSuspectSummary(params: {
    businessType?: string
    fullInventoryTime?: string
  } = {}): Promise<{ count: number; sample_paths: string[] }> {
    const q = new URLSearchParams()
    if (params.businessType) q.set('businessType', params.businessType)
    if (params.fullInventoryTime) q.set('fullInventoryTime', params.fullInventoryTime)
    const response = await fetch(`${API_BASE}/resources/suspect-summary?${q.toString()}`)
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to load suspect summary')
    }
    return {
      count: result.data?.count ?? 0,
      sample_paths: result.data?.sample_paths ?? [],
    }
  }

  /**
   * 一键把当前 tab 范围内所有 suspect=1 且未认领的资源批量置 claim_status=4（已忽略）。
   * 已认领的不动。
   */
  async ignoreAllSuspect(params: {
    businessType?: string
    fullInventoryTime?: string
    claimant_name: string
    claimant_unit: string
  }): Promise<{ updatedCount: number }> {
    const response = await fetch(`${API_BASE}/resources/ignore-all-suspect`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params),
    })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to ignore suspect resources')
    }
    return result.data
  }

  /**
   * 批量归类保护（更新重要程度）
   */
  async batchClassify(params: BatchClassifyParams): Promise<{ updatedCount: number }> {
    const response = await fetch(`${API_BASE}/resources/classify`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to classify resources')
    }

    return result.data
  }

  /**
   * 单条归类保护
   * @param params 归类保护参数
   */
  async singleClassify(params: SingleClassifyParams): Promise<{ success: boolean; message: string; data?: { id: number } }> {
    const response = await fetch(`${API_BASE}/resources/classify/single`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.message || result.error || 'Failed to classify resource')
    }

    return result
  }

  /**
   * 获取归档文件列表
   * @param params 查询参数
   */
  async getArchiveFiles(params: ArchiveFileQueryParams = {}): Promise<ArchiveFilePageResult> {
    const searchParams = new URLSearchParams()

    if (params.page) searchParams.set('page', String(params.page))
    if (params.pageSize) searchParams.set('pageSize', String(params.pageSize))
    if (params.applicant_name) searchParams.set('applicant_name', params.applicant_name)

    const response = await fetch(`${API_BASE}/archive/list?${searchParams}`)
    const result = await response.json()

    if (result.code !== 0) {
      throw new Error(result.message || 'Failed to fetch archive files')
    }

    return result.data
  }

  /**
   * 列出已一键归档上报到云端的部门/单位柜室文件（供「档案在线阅卷」部门/单位页展示）。
   * scope: 'department' | 'unit'
   */
  async getQuickArchiveCabinetFiles(scope?: 'department' | 'unit'): Promise<QuickArchiveCabinetFile[]> {
    const sp = new URLSearchParams()
    if (scope) sp.set('scope', scope)
    const response = await fetch(`${API_BASE}/centralized-projects/quick-archive-files?${sp}`)
    const result = await response.json()
    if (!result.success) throw new Error(result.error || '获取柜室文件失败')
    return result.data || []
  }

  /**
   * 列出本机「个人{级别}文件夹」下的一键归档文件（供「档案在线阅卷·个人」展示）。
   * level: 'core' | 'important' | 'general'
   */
  async getPersonalArchiveFiles(level: 'core' | 'important' | 'general'): Promise<PersonalArchiveFile[]> {
    const response = await fetch(`${API_BASE}/centralized-projects/personal-archive-files?level=${level}`)
    const result = await response.json()
    if (!result.success) throw new Error(result.error || '获取个人归档文件失败')
    return result.data || []
  }

  /**
   * 借阅下载文件
   * @param params 借阅参数
   */
  async borrowDownload(params: BorrowDownloadParams): Promise<Blob> {
    const response = await fetch(`${API_BASE}/archive/download`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })

    // 如果响应是二进制文件，直接返回
    if (response.headers.get('content-type')?.includes('application/octet-stream')) {
      return response.blob()
    }

    // 否则解析JSON错误响应
    const errorResult = await response.json().catch(() => ({}))
    if (errorResult.code !== 0) {
      throw new Error(errorResult.message || 'Failed to download file')
    }

    throw new Error('Unexpected response')
  }

  /**
   * 在线预览文件（返回预览URL）
   * @param params 借阅参数
   */
  async getPreviewUrl(params: BorrowDownloadParams): Promise<string> {
    const response = await fetch(`${API_BASE}/archive/download`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(params),
    })

    // 如果响应是二进制文件，创建临时URL
    if (response.headers.get('content-type')?.includes('application/octet-stream')) {
      const blob = await response.blob()
      return URL.createObjectURL(blob)
    }

    // 否则解析JSON响应
    const result = await response.json()
    if (result.code !== 0) {
      throw new Error(result.message || 'Failed to get preview URL')
    }

    return result.data.url || result.data.previewUrl
  }

  /**
   * 同步数据资源到服务端
   */
  async syncSource(): Promise<{
    success: boolean
    message: string
    data: {
      syncedCount: number
      failedCount: number
      totalCount: number
      lastSyncTime: string | null
      errors: string[]
    }
  }> {
    const response = await fetch(`${API_BASE}/sync/source`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
    })

    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || result.message || 'Failed to sync resources')
    }

    return result
  }

  /**
   * 获取扫描任务列表（分页）
   */
  async getScanTasks(page = 1, pageSize = 20): Promise<{
    tasks: ScanTask[]
    total: number
    page: number
    pageSize: number
  }> {
    const searchParams = new URLSearchParams()
    searchParams.set('page', String(page))
    searchParams.set('pageSize', String(pageSize))

    const response = await fetch(`${API_BASE}/scan-tasks?${searchParams}`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch scan tasks')
    }

    return result.data
  }

  /**
   * 获取扫描任务详情
   */
  async getScanTaskDetail(taskId: number): Promise<ScanTask> {
    const response = await fetch(`${API_BASE}/scan-tasks/${taskId}`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch scan task detail')
    }

    return result.data
  }

  /**
   * 根据 content_sign 打开文件
   * @param contentSign 文件内容签名
   */
  async openFile(contentSign: string): Promise<{ success: boolean; message: string; filePath?: string }> {
    const response = await fetch(`${API_BASE}/files/open`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ contentSign }),
    })
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || result.message || 'Failed to open file')
    }

    return {
      success: result.success,
      message: result.message,
      filePath: result.data?.filePath
    }
  }

  /**
   * 获取归档管理文件列表（本地待归档/已归档文件）
   * @param params 查询参数
   */
  async getArchiveManagementFiles(params: ArchiveManagementQueryParams = {}): Promise<{
    files: ArchiveManagementFileItem[]
    total: number
    page: number
    pageSize: number
  }> {
    const searchParams = new URLSearchParams()

    if (params.search) searchParams.set('search', params.search)
    if (params.archiveType) searchParams.set('archiveType', params.archiveType)
    if (params.importanceLevelFilter !== undefined && params.archiveType === 'pending') {
      searchParams.set('importanceLevelFilter', String(params.importanceLevelFilter))
    }
    searchParams.set('page', String(params.page || 1))
    searchParams.set('pageSize', String(params.pageSize || 50))

    const response = await fetch(`${API_BASE}/archive-management?${searchParams}`)
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || 'Failed to fetch archive files')
    }

    return result.data
  }

  /**
   * 批量设置为无需归档
   * @param ids 文件ID列表
   */
  async batchUpdateToNoArchive(ids: number[]): Promise<{ updatedCount: number }> {
    const response = await fetch(`${API_BASE}/archive-management/no-archive`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ ids }),
    })
    const result = await response.json()

    if (!result.success) {
      throw new Error(result.error || result.message || 'Failed to update files')
    }

    return result.data
  }

  // ============================================================
  // Similarity / Family
  // ============================================================

  async startSimilarityAnalysis(): Promise<{ task_id: number }> {
    const response = await fetch(`${API_BASE}/similarity/analyze`, { method: 'POST' })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to start analysis')
    }
    return result.data
  }

  async getSimilarityTask(id: number): Promise<SimilarityTask> {
    const response = await fetch(`${API_BASE}/similarity/task/${id}`)
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to load task')
    }
    return result.data
  }

  async getLatestSimilarityTask(): Promise<SimilarityTask | null> {
    const response = await fetch(`${API_BASE}/similarity/task/latest`)
    const result = await response.json()
    return result.data || null
  }

  async getFamilyMembers(familyId: number): Promise<FamilyMembersResponse> {
    const response = await fetch(`${API_BASE}/family/${familyId}/members`)
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'Failed to load family members')
    }
    return result.data
  }

  /**
   * 获取相似度分析预览（缓存缺失数 + 上次运行信息）
   */
  async analyzePreview(): Promise<AnalyzePreview> {
    const response = await fetch(`${API_BASE}/similarity/analyze/preview`, { method: 'POST' })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'preview failed')
    }
    return result.data
  }

  /**
   * 批量获取 content_sign 对应的家族成员列表
   */
  async batchFamilyMembers(contentSigns: string[]): Promise<Record<string, FamilyMemberDetail[]>> {
    const response = await fetch(`${API_BASE}/family/batch-members`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content_signs: contentSigns }),
    })
    const result = await response.json()
    if (!result.success) {
      throw new Error(result.error || 'batch members failed')
    }
    return result.data || {}
  }
}

// 扫描任务相关类型定义
export type ScanType = 'FILE' | 'DATABASE'
export type TaskState = 'run' | 'succeed' | 'fail'

export interface ParamsChanged {
  workspacePathChanged: boolean      // 工作空间变更
  scanAreaPathChanged: boolean       // 扫描范围变更
  controlTypeChanged: boolean        // 管控类型变更
}

export interface ScanTask {
  id?: number
  scan_type: ScanType
  file_scan_range?: string
  heartbeat: number
  workspace_path?: string
  task_state: TaskState
  task_phase?: string
  task_error_message?: string
  scan_args?: string
  file_total?: number
  file_scanned_count?: number
  file_all_suffix_text?: string      // 本次扫描用的所有文件后缀
  file_all_suffix_count?: number     // 本次扫描的所有后缀数量
  file_count_suffix_count?: number   // 工作空间中文件后缀种类数量
  workspace_count?: number
  end_time?: string
  scan_log?: string
  create_time: string
  update_time: string
  disable: number
  paramsChanged: ParamsChanged
}

export const api = new ApiService()

// ---------------------------------------------------------------------------
// 2026-05-20 历史/新数据区分：AI 归目相关的精简函数式 API
// ---------------------------------------------------------------------------

export type ClassifyOrigin = 'new' | 'historical'

export interface ClassifySuggestion {
  project_id: number
  project_code?: string
  project_name?: string
  stage_code: string
  stage_name?: string
  file_rule_code: string
  file_rule_name?: string
  confidence: number
  reason?: string
}

export interface PendingClassifyItem {
  resource_id: number
  resource_name: string
  suggestions: ClassifySuggestion[]
}

export interface PendingClassifyResponse {
  items: PendingClassifyItem[]
  total: number
  page: number
  page_size: number
}

export async function fetchClassifyPending(opts: {
  origin: ClassifyOrigin
  page?: number
  pageSize?: number
  minConfidence?: number
}): Promise<PendingClassifyResponse> {
  const q = new URLSearchParams()
  q.set('origin', opts.origin)
  if (opts.page) q.set('page', String(opts.page))
  if (opts.pageSize) q.set('page_size', String(opts.pageSize))
  if (opts.minConfidence != null) q.set('min_confidence', String(opts.minConfidence))
  const res = await fetch(`${API_BASE}/ai/classify/pending?${q.toString()}`)
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'pending fetch failed')
  return j.data as PendingClassifyResponse
}

export async function fetchClassifySuggestions(resourceId: number): Promise<{
  resource_id: number
  resource_name: string
  suggestions: ClassifySuggestion[]
}> {
  const res = await fetch(`${API_BASE}/ai/classify/suggestions?resource_id=${resourceId}`)
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'suggestions fetch failed')
  return j.data
}

export async function bulkDismissHistorical(resourceIds: number[], reason: string): Promise<{ dismissed: number }> {
  const res = await fetch(`${API_BASE}/ai/classify/bulk-dismiss`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ resource_ids: resourceIds, reason }),
  })
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'bulk dismiss failed')
  return j.data as { dismissed: number }
}

// ---------------------------------------------------------------------------
// 2026-05-21 三级分流：family 权威源 + importance 手动调级 + 核心登记
// ---------------------------------------------------------------------------

export interface FamilyMemberFlat {
  data_resources_id: number
  resources_name?: string | null
  family_relation?: string | null
  path?: string | null
}

export async function fetchFamilyMembers(familyId: number): Promise<{ family_id: number; members: FamilyMemberFlat[] }> {
  const res = await fetch(`${API_BASE}/family/${familyId}/members`)
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'family fetch failed')
  const groups = j.data?.groups || {}
  const flat: FamilyMemberFlat[] = []
  for (const k of Object.keys(groups)) for (const m of groups[k]) flat.push(m)
  return { family_id: j.data.family_id, members: flat }
}

export async function setFamilyAuthoritative(familyId: number, resourceId: number) {
  const res = await fetch(`${API_BASE}/family/${familyId}/authoritative`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ resource_id: resourceId }),
  })
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'set authoritative failed')
  return j.data
}

export async function overrideResourceImportance(resourceId: number, level: 0 | 1 | 2 | 3 | 4) {
  const res = await fetch(`${API_BASE}/resources/${resourceId}/importance`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ level }),
  })
  const j = await res.json()
  if (!j.success) throw new Error(j.error || 'override importance failed')
  return j.data
}
