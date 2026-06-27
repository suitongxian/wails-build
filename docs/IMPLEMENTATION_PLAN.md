# data-asset-scan Go 重构实现计划

> **状态：已完成（保留作历史参考）**。当前主分支已是 Go/Wails，此文档
> 描述的是迁移计划与决策过程，可用于回溯架构选型、了解模块对应关系。

## 一、目标

将 `data-asset-scan-go` 从 TypeScript/Electron 重构为 Go/Wails，功能、效果、API 一比一还原，不改变任何业务逻辑。

---

## 二、技术选型

| 模块 | 选型 | 说明 |
|------|------|------|
| 桌面框架 | Wails v2 | 跨平台 Windows/macOS/Linux |
| Web 框架 | Gin | 路由/Gin/v2 |
| 数据库 | go-sqlite3 + sqlx | 直接迁移 database.sql |
| 前端 | Vue 3 + Vuetify 3 | **一行不改**，Wails bundling |
| 状态机 | 手写 Go 版 | 9 阶段完整还原 |
| SSE | net/http 原生 | text/event-stream |
| 文件哈希 | Go 标准库 md5 | partial hash 逻辑精确复刻 |

---

## 三、项目结构

```
data-asset-scan-go/
├── wails.json                    # Wails 项目配置
├── main.go                       # Wails 入口
├── frontend/                     # Vue 前端（从 src/ 软链接或复制）
│   └── src/                      # 不变
├── internal/
│   ├── config/                   # ConfigService（YAML 解析）
│   ├── models/                   # 数据库模型（与 SQL 表结构一一对应）
│   ├── repository/               # 6 个 Repository（DatabaseService 拆分）
│   │   ├── user_info.go
│   │   ├── scan_task.go
│   │   ├── data_distribution.go
│   │   ├── file_distribution.go
│   │   ├── data_archive.go
│   │   └── scan_statistics.go
│   ├── scanner/                  # 核心扫描逻辑
│   │   ├── hash.go              # FileHashUtil（partial hash）
│   │   ├── walker.go            # StreamingFileScannerService（目录遍历）
│   │   ├── atomic.go            # AtomicScanService（状态机 + 扫描核心）
│   │   └── progress.go          # SSE 进度推送
│   ├── httpd/                    # HTTP 路由层
│   │   ├── router.go             # Gin 路由注册
│   │   ├── user.go               # /user_info/*
│   │   ├── scan.go               # /scan/*
│   │   ├── tasks.go              # /tasks/*
│   │   ├── files.go              # /files/*
│   │   ├── distribution.go       # /data_distribution/*
│   │   ├── archive.go            # /archive/*
│   │   └── statistics.go         # /statistics/*
│   ├── workspace.go              # WorkspaceService（快捷方式、符号链接）
│   ├── opener.go                 # FileOpenerService（打开文件）
│   └── uploader.go               # HttpUploadService（远程归档上传）
├── verification/                  # 交叉验证工具（重构前先写）
│   └── main.go                   # partial hash 对比工具
└── database.sql                  # 直接迁移，不改
```

---

## 四、数据库迁移

**直接复制原 `database.sql`，逐行迁移到 Go**。

原文件位置：`electron/database.sql`

迁移原则：
- 建表 SQL 一字不改
- Repository 层的 Go 结构体字段与 SQL 列名一一对应
- 使用 `sqlx` 的 `Scan` 机制，字段映射逐个核对

---

## 五、模块映射表

| 原 TypeScript Service | Go 实现 | 难度 |
|----------------------|---------|------|
| `ConfigService` | `internal/config/config.go` | ⭐ |
| `UserInfoService` | `internal/repository/user_info.go` | ⭐ |
| `ScanTaskService` | `internal/repository/scan_task.go` | ⭐ |
| `DataDistributionService` | `internal/repository/data_distribution.go` | ⭐⭐ |
| `FileDistributionService` | `internal/repository/file_distribution.go` | ⭐⭐ |
| `DataArchiveService` | `internal/repository/data_archive.go` | ⭐⭐ |
| `ScanStatisticsService` | `internal/repository/scan_statistics.go` | ⭐⭐ |
| `DatabaseService` | 拆分为上面 6 个 repository | ⭐ |
| `FileHashUtil` | `internal/scanner/hash.go` | ⭐⭐⭐ |
| `StreamingFileScannerService` | `internal/scanner/walker.go` | ⭐⭐ |
| `AtomicScanService` | `internal/scanner/atomic.go` | ⭐⭐⭐ |
| `WorkspaceService` | `internal/workspace.go` | ⭐⭐ |
| `FileOpenerService` | `internal/opener.go` | ⭐ |
| `HttpUploadService` | `internal/uploader.go` | ⭐⭐ |

---

## 六、核心难点详细方案

### 6.1 Partial Hash（最关键）

**原版逻辑**（`FileHashUtil.ts`，函数 `calculateFileMd5Hash`）：

```typescript
// 文件 < 5MB：直接读全部，算 MD5
// 文件 >= 5MB：读前 4096 字节 + 末尾 4096 字节，拼在一起算 MD5
// 当文件 < 8KB 时，head 和 tail 数据会重叠（原版这样用，不做特殊处理）
// 最终结果统一转大写 32 位 hex
```

**Go 实现**：

```go
func PartialMD5(path string) (string, error) {
    fi, err := os.Stat(path)
    if err != nil { return "", err }

    const threshold int64 = 5 * 1024 * 1024 // 5MB

    if fi.Size() <= threshold {
        data, err := os.ReadFile(path)
        if err != nil { return "", err }
        sum := md5.Sum(data)
        return strings.ToUpper(hex.EncodeToString(sum[:])), nil
    }

    const sampleSize = 4096
    f, err := os.Open(path)
    if err != nil { return "", err }
    defer f.Close()

    head := make([]byte, sampleSize)
    tail := make([]byte, sampleSize)

    f.ReadAt(head, 0)
    f.ReadAt(tail, fi.Size()-sampleSize)

    combined := append(head, tail...)
    sum := md5.Sum(combined)
    return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}
```

**验证方法**（见第八节）

---

### 6.2 状态机（9 阶段）

**原版流程**（`AtomicScanService.ts` 的 `executeScanProcess` 方法）：

#### FULL_INVENTORY 模式
```
counting → scanning → aggregating → completed
```

#### DAILY_CHECK 模式
```
counting → scanning → aggregating
  → collecting → loading_existing → classifying
  → checking_modifications → marking_deleted
  → processing_new → completed
```

**Go 实现核心结构**：

```go
type ScanPhase string

const (
    PhaseCounting            ScanPhase = "counting"
    PhaseScanning            ScanPhase = "scanning"
    PhaseAggregating         ScanPhase = "aggregating"
    PhaseCollecting          ScanPhase = "collecting"
    PhaseLoadingExisting     ScanPhase = "loading_existing"
    PhaseClassifying         ScanPhase = "classifying"
    PhaseCheckingModifications ScanPhase = "checking_modifications"
    PhaseMarkingDeleted      ScanPhase = "marking_deleted"
    PhaseProcessingNew       ScanPhase = "processing_new"
    PhaseCompleted           ScanPhase = "completed"
)

type ScanState struct {
    Phase    ScanPhase
    Mu       sync.RWMutex
    Progress ProgressUpdate
}

type ProgressUpdate struct {
    Type         string `json:"type"` // "progress" | "complete"
    ScannedCount int    `json:"scannedCount"`
    TotalCount   int    `json:"totalCount"`
    CurrentFile  string `json:"currentFile,omitempty"`
    ElapsedMs    int64  `json:"elapsedMs"`
    Success      bool   `json:"success"`
    // DAILY_CHECK 专用
    NewFiles        int `json:"newFiles,omitempty"`
    NormalFiles     int `json:"normalFiles,omitempty"`
    DeletedFiles    int `json:"deletedFiles,omitempty"`
    ModifiedFiles    int `json:"modifiedFiles,omitempty"`
    // 不确定阶段（scanning 之前）专用
    Indeterminate bool `json:"indeterminate,omitempty"`
}
```

**进度推送通过 SSE channel**：

```go
func (s *AtomicScanner) run(ctx context.Context, sseCh chan<- ProgressUpdate) {
    for phase := s.nextPhase(); phase != PhaseCompleted; phase = s.nextPhase() {
        s.state.Phase = phase
        if err := s.executePhase(ctx, phase, sseCh); err != nil {
            sseCh <- ProgressUpdate{Type: "complete", Success: false}
            return
        }
    }
    sseCh <- ProgressUpdate{Type: "complete", Success: true}
}
```

---

### 6.3 SSE 流式推送

**Go 精确实现**（逐字段对齐原版）：

```go
func (h *ScanHandler) handleAtomicScan(w http.ResponseWriter, r *http.Request) {
    // 设置 SSE 响应头
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no")

    flusher, ok := w.(http.Flusher)
    if !ok { return }

    // 通过 channel 接收扫描进度
    progCh := make(chan ProgressUpdate, 100)

    // 在 goroutine 中启动扫描
    go func() {
        h.scanner.Run(r.Context(), progCh)
        close(progCh)
    }()

    // SSE 循环
    encoder := json.NewEncoder(w)
    for p := range progCh {
        data, _ := json.Marshal(p)
        fmt.Fprintf(w, "data: %s\n\n", data)
        flusher.Flush()
    }
}
```

**原版 SSE 消息格式**（逐字段对照）：

| 字段 | 原版类型 | Go 类型 |
|------|---------|---------|
| `type` | string | string |
| `scannedCount` | number | int |
| `totalCount` | number | int |
| `currentFile` | string | string |
| `elapsedMs` | number | int64 |
| `indeterminate` | boolean（不确定阶段时出现） | bool |
| `success` | boolean（complete 时） | bool |
| `newFiles/normalFiles/deletedFiles/modifiedFiles` | number | int |

---

### 6.4 目录遍历（扩展名过滤）

**原版过滤逻辑**（`StreamingFileScannerService.ts`）：

```typescript
// 排除的目录名（正则）
/(^|[\/\\])\.|node_modules|\.git$|__pycache__|\.venv$/

// 文件扩展名过滤（includeExtensions 不为空时只保留这些）
// 示例：includeExtensions = ['.pdf', '.doc', '.docx']
```

**Go 实现**：

```go
var dirExcludePattern = regexp.MustCompile(`(^|[\/\\])\.|node_modules|\.git$|__pycache__|\.venv$`)

func shouldSkipDir(name string) bool {
    return dirExcludePattern.MatchString(name)
}

func matchExtensions(name string, include []string) bool {
    if len(include) == 0 { return true }
    ext := filepath.Ext(name)
    for _, e := range include {
        if ext == e { return true }
    }
    return false
}
```

---

## 七、API 路由对照表（全部 27 个接口）

| 方法 | 路径 | Go Handler | 说明 |
|------|------|-----------|------|
| GET | `/user_info` | `GetUserInfo` | 获取用户信息 |
| PUT | `/user_info` | `UpdateUserInfo` | 更新用户名 |
| GET | `/user_info/workspace` | `GetWorkspace` | 获取工作空间路径 |
| PUT | `/user_info/workspace` | `UpdateWorkspace` | 更新工作空间 |
| GET | `/user_info/workspace/exists` | `WorkspaceExists` | 检查工作空间是否存在 |
| POST | `/user_info/workspace` | `CreateWorkspace` | 创建工作空间（含符号链接） |
| DELETE | `/user_info/workspace` | `DeleteWorkspace` | 删除工作空间 |
| GET | `/user_info/workspace/.os/*` | `GetOSCompatiblePath` | 获取 OS 兼容路径 |
| GET | `/tasks` | `GetTasks` | 获取所有扫描任务 |
| POST | `/tasks` | `CreateTask` | 创建扫描任务 |
| DELETE | `/tasks/:id` | `DeleteTask` | 删除任务 |
| POST | `/tasks/:id/start` | `StartTask` | 启动扫描 |
| GET | `/tasks/:id/progress` | `GetTaskProgress` | 获取任务进度（SSE） |
| GET | `/scan/atomic` | `AtomicScan` | 全量原子扫描（SSE） |
| POST | `/scan/atomic/stop` | `StopAtomicScan` | 停止扫描 |
| GET | `/files` | `GetFiles` | 分页获取文件列表 |
| GET | `/files/:id` | `GetFile` | 获取单个文件 |
| DELETE | `/files/:id` | `DeleteFile` | 删除文件 |
| GET | `/distribution` | `GetDataDistribution` | 获取数据分布 |
| GET | `/distribution/file` | `GetFileDistribution` | 获取文件类型分布 |
| POST | `/archive` | `CreateArchive` | 创建归档（远程上传） |
| GET | `/archive` | `ListArchives` | 获取归档列表 |
| GET | `/archive/:id/download` | `DownloadArchive` | 下载归档 |
| DELETE | `/archive/:id` | `DeleteArchive` | 删除归档 |
| GET | `/statistics` | `GetStatistics` | 获取统计信息 |
| GET | `/statistics/growth` | `GetGrowthRate` | 获取增长率 |

---

## 八、验证方法

### 8.1 Partial Hash 交叉验证

**在写任何业务代码之前，先写验证工具**：

```bash
# 编译验证工具
cd verification && go build -o hash_verifier main.go

# 准备测试文件集
mkdir -p test_files
# 放入：<4KB / 5MB / 10MB / 100MB / 1GB 各类文件

# 对比原版 TypeScript 输出 vs Go 输出
./hash_verifier ./test_files/ --output=compare_report.json

# 通过标准：diff_count == 0
```

**同时用 Node.js 跑原版对照**：

```bash
cd data-asset-scan
node -e "
const { calculateFileMd5Hash } = require('./electron/services/FileHashUtil');
const fs = require('fs');
const path = process.argv[2];
console.log(calculateFileMd5Hash(path));
" <file_path>
```

### 8.2 接口级对比验证

用 curl 逐个对比原版和 Go 版的响应：

```bash
# 原版（端口 3001）
curl -s http://localhost:3001/tasks | jq .

# Go 版（端口 3002）
curl -s http://localhost:3002/tasks | jq .

# diff 输出，必须完全一致
```

### 8.3 SSE 输出对比

同时对两个端口跑 curl，捕获 SSE 输出逐条对比：

```bash
curl -N http://localhost:3001/scan/atomic?scan_mode=FULL_INVENTORY --output /tmp/original_sse.txt
curl -N http://localhost:3002/scan/atomic?scan_mode=FULL_INVENTORY --output /tmp/go_sse.txt
diff /tmp/original_sse.txt /tmp/go_sse.txt
```

---

## 九、实施阶段

### 阶段 0：验证工具（1-2 天）

- [ ] 写 `verification/main.go`（partial hash 对比工具）
- [ ] 准备测试文件集（覆盖各尺寸、中文文件名）
- [ ] 跑对比，确认 Go 的 partial hash 与原版 100% 一致
- [ ] 确认后删除此工具，开始正式开发

### 阶段 1：项目骨架 + 数据库（2-3 天）

- [ ] 初始化 Wails v2 项目
- [ ] 迁移 `database.sql` 建表
- [ ] 用 sqlx 生成 Go 结构体（逐表对照）
- [ ] 实现 6 个 Repository（逐个与原版 SQL 对照）

### 阶段 2：Config + Workspace + Opener（1-2 天）

- [ ] `internal/config/config.go`（YAML 解析）
- [ ] `internal/workspace.go`（符号链接 + Windows 快捷方式）
- [ ] `internal/opener.go`（打开文件）

### 阶段 3：核心扫描（5-7 天，最核心）

- [ ] `internal/scanner/hash.go`（通过阶段 0 验证）
- [ ] `internal/scanner/walker.go`（目录遍历 + 扩展名过滤）
- [ ] `internal/scanner/atomic.go`（9 阶段状态机，逐阶段验证）
- [ ] `internal/scanner/progress.go`（SSE 推送，逐字段验证）

### 阶段 4：HTTP 路由层（2-3 天）

- [ ] `internal/httpd/router.go`（Gin 注册）
- [ ] 27 个接口逐个写，对比响应结构

### 阶段 5：前端集成 + 全量回归（2-3 天）

- [ ] Wails bundling 前端 Vue
- [ ] 全量功能测试（所有 UI 操作走一遍）
- [ ] 跨平台测试（Windows/macOS/Linux）

---

## 十、验收标准

每个阶段完成后，必须满足：

1. **阶段 0**：原版与 Go 版的 partial hash diff_count == 0
2. **阶段 1-2**：Gin 启动不报错，/user_info 接口返回结构一致
3. **阶段 3**：同一批文件扫描后，数据库里的 ScannedCount / totalCount 与原版一致
4. **阶段 4**：27 个接口逐个 curl 对比，响应结构和字段完全一致
5. **阶段 5**：前端所有操作（创建任务/启动扫描/查看文件/归档/统计）功能正常

---

## 十一、风险清单

| 风险 | 等级 | 应对 |
|------|------|------|
| Partial hash 不一致 | 🔴 最高 | 阶段 0 先验证，不通过不动手 |
| Windows 快捷方式 (`writeShortcutLink`) | 🟡 中 | 用 PowerShell 替代 |
| Go 和 Node.js SSE flush 时机不同 | 🟡 中 | 逐字段验证，只要数据对就行 |
| 中文/特殊字符路径 | 🟡 中 | 跨平台测试 |
| `filepath.Walk` 循环链接 | 🟡 中 | `WalkDir` + 跳过已访问 inode |
