# 登录后自动创建工作空间目录

**日期**：2026-05-24
**状态**：设计已确认，待实现
**触发原因**：用户首次使用终端时还需手动到「系统设置」里填写工作空间目录，这是一道额外的操作门槛，且容易填错路径

## 背景

当前流程：
- 用户必须先到「系统设置」→「工作空间目录」手填一个路径
- 工作空间承担多重职责：扫描的"工作区"范围、项目根目录（数据业务模版立项卷宗存储）、`accessTimeFilter=workspace` 过滤的依据
- 路径为空时多个下游功能会跳过或报错（首次普查无法启动、数据业务模版立项被拒）

目标：登录成功后，若工作空间目录为空，自动生成一个约定路径并创建目录，让用户开箱即用，但仍保留可编辑的入口供用户自定义。

## 设计

### 触发点：登录成功后

唯一时机选 `forwardAuth`（`internal/httpd/auth.go`），在 `mirrorAuthUser(session.User)` 成功之后、写 `currentAuthSession` 之前。

理由：
- 这是后端唯一明确知道「scan 登录用户名」的时刻
- "用户第一次打开终端"这个时刻还没登录，无从知道用谁的名字
- 登出再登录、或新用户首次登录都被这一个时机覆盖

### 路径约定

```
<os.UserHomeDir()> / <session.User.Username> / workspace
```

字段含义：
- `os.UserHomeDir()` 是 OS 进程用户的家目录，例如 `/Users/admin`、`/home/admin`、`/root`
- `session.User.Username` 是 scan 登录用户名（拼音/英文 ID，不是 DisplayName）
- 最终拼接示例：`/root/zhang/workspace`、`/Users/admin/alice/workspace`

为什么用 Username 而不是 DisplayName：
- DisplayName 含中文/空格/标点的概率高，做文件路径容易踩坑
- Username 是已注册的稳定 ID，路径稳定可预期

### 行为策略：仅当 KeyWorkspace 为空时设置

```
if KeyWorkspace == "" {
    path = computePath(homeDir, username)
    if err := validateUsername(username); err != nil {
        log + return
    }
    if err := os.MkdirAll(path, 0755); err != nil {
        log + return  // 不写库，下次登录再试
    }
    cfg.SetWorkspace(path)
}
// 已有值：不动
```

为什么"仅空时设"而不是"每次覆盖"：
- 用户在「系统设置」自定义后，下次登录不应被覆盖回约定路径
- 这一点正是「保留可编辑入口」语义的内涵

### 健壮性

**Username 危险字符校验**：含 `/`、`\`、`..`、`\x00` 拒绝。理论上从 manage 同步来的 Username 不会有这些，但兜底防御。

**错误处理契约**：`ensureDefaultWorkspaceForUser` 函数永远返回 `error`，调用方（`forwardAuth`）拿到 error 一律 **log + 继续**，不让 login 失败。具体场景：
- Username 校验失败 → 返 error，不 mkdir 不写库
- MkdirAll 失败（权限不足、磁盘满、同名非目录文件占位）→ 返 error，不写 KeyWorkspace，下次登录再试
- SetWorkspace 写库失败 → 返 error，下次登录再试

口诀：**自动化便利不能成为登录的硬阻塞**。

### 前端改动

**SystemConfigView（保留可编辑）**：
- 输入框保留
- hint 加一句：「首次登录会自动设为 `~/用户名/workspace`，你可以改成别的」
- 不加"重置到默认"按钮（按用户决定）

**App.vue 右上指示器**（新增）：
- 在现有 user-info chip 左侧插入一个 workspace chip
- 显示「工作空间: <path>」，太长用 text-truncate + tooltip 显示完整路径
- 图标 `mdi-folder-outline`
- 点击跳 `/settings`
- 数据来源：复用 `api.getConfig().workspace`，App.vue 已有的 onMounted 流程加一次拉取

布局示意：
```
[📁 工作空间: ~/zhang/workspace]  [👤 张三 | 部门 | 单位]  [退出]
```

## 模块划分

| 改动点 | 文件 | 内容 |
|---|---|---|
| Helper | `internal/repository/workspace_default.go`（新） | `ComputeDefaultWorkspace(homeDir, username) (string, error)` + `ValidateUsername(username) error` |
| Hook 入口 | `internal/httpd/auth.go` | `forwardAuth` 末尾调用，独立小函数 `ensureDefaultWorkspaceForUser(cfg, username)` 方便单测 |
| 前端 SystemConfigView | `frontend_real/views/SystemConfigView.vue` | 增 hint 文案 |
| 前端 App.vue | `frontend_real/App.vue` | 新增 workspace chip + workspace ref + 拉取 |

## 测试

按 CLAUDE.md 铁律，每个改动都有 case 验证。

### Go 单测：repository helper

`internal/repository/workspace_default_test.go`：
- `TestComputeDefaultWorkspace_HappyPath`：正常 username → 正确路径
- `TestComputeDefaultWorkspace_RejectsSlash`：username `a/b` → error
- `TestComputeDefaultWorkspace_RejectsDotDot`：username `..` → error
- `TestComputeDefaultWorkspace_RejectsBackslash`：username `a\\b` → error
- `TestComputeDefaultWorkspace_RejectsNull`：username 含 `\x00` → error

### Go 单测：auth helper（不涉及 HTTP）

`internal/httpd/auth_workspace_test.go`：
- `TestEnsureDefaultWorkspace_EmptyConfig_CreatesAndSets`：空 KeyWorkspace + 临时 home dir → 路径正确写入 + 目录被创建
- `TestEnsureDefaultWorkspace_PreservesUserCustom`：已有 KeyWorkspace = `/custom/path` → 不动，且不 mkdir 约定路径
- `TestEnsureDefaultWorkspace_MkdirFailureReturnsError`：mkdir 失败（路径前缀指向只读 / 不存在的卷）→ 函数返 error，KeyWorkspace 不被写入
- `TestEnsureDefaultWorkspace_InvalidUsernameReturnsError`：username 含 `..` → 函数返 error，不 mkdir 不写库

### Go 集成测：HTTP login（确认错误处理契约）

`internal/httpd/auth_login_workspace_integration_test.go`：
- mock manage 登录响应（用 httptest）→ POST /auth/login → 校验 KeyWorkspace 被自动写入预期路径 + 目录存在
- 第二次登录（已有 workspace 自定义）→ KeyWorkspace 不变
- helper 返 error 的场景下（用故意失败的 home dir）→ POST /auth/login **仍返 200**，登录成功，仅 log 一条警告

### 前端测试

- SystemConfigView 改动是文案 hint，无需新增测试
- App.vue 新增 chip 不强求自动化测试（手测 UI 即可）。可选：加 render snapshot 验证 chip 出现且包含 workspace 文本

### 测前必须

`npm rebuild better-sqlite3`（CLAUDE.md 铁律）。

## 非目标

- 不做老数据迁移（用户明确说过"不用考虑老的数据"）
- 不加"重置到默认"按钮（按用户决定）
- 不动 `data_projects.project_root` 字段（每个项目立项时的快照，与本特性正交）
- 不影响 `effectiveProjectRoot`（已与 workspace 合并，自动跟随）
- 不替换扫描区域 / 控制类型 / 排除目录等其他配置

## 验收标准

1. 全新数据库 + 首次登录 → 工作空间自动设为 `<HOME>/<username>/workspace`，目录已创建
2. 用户到「系统设置」把工作空间改成 `/data/projects` → 登出再登录 → 仍是 `/data/projects`
3. 任何分支下登录都不应因 mkdir 问题失败
4. 右上角看得到当前工作空间路径，点击能跳到设置页
5. 所有新测试用例通过；现有测试不被影响
