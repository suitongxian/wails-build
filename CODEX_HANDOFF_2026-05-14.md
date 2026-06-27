# Codex Handoff - 2026-05-14

本文档用于在另一台电脑上恢复本轮 Codex 协作上下文。它不是原始聊天全文，而是可执行的工程交接记录，覆盖需求背景、业务边界、已实现内容、测试方式和恢复步骤。

## 仓库信息

- scan 仓库：`git@gitee.com:jy-development/data-asset-scan.git`
- 当前本地目录：`/Users/suitongxian/Code/xueba/data-asset-scan-go`
- 当前分支：`go-test-template`
- 配套 manage 仓库：`git@gitee.com:jy-development/data-asset-manage.git`
- manage 当前分支：`test-template`

## 需求背景

本轮围绕“scan 上报、manage 入柜入室”的结构化归档闭环展开。核心边界如下：

```text
scan 本地项目文件提交 output
→ 上报 manage：部门柜登记

scan 项目结项 close
→ 上报 manage：单位文件室 / 单位机要室登记
```

第一版只做结构化 JSON 上报和结构化登记，不传真实文件包，不移动文件实体。`source_storage_uri` 只作为来源证据，不代表 manage 端已经拿到或可直接访问该文件。

## 业务边界

### 个人项目

scan 端内置三个个人项目：

- `SYS-PERSONAL-CORE`：个人核心级文件管理
- `SYS-PERSONAL-IMPORTANT`：个人重要级文件管理
- `SYS-PERSONAL-GENERAL`：个人一般级文件管理

这些项目来自“个人文件项目化管理”的需求设计：负责人、事项责任人都是本人，事项简化为起草/定稿/本地归档。个人数据原则上本地归档，不进入 manage 的部门柜/单位室流程。

代码依据：

- `internal/repository/personal_files_init.go`
- `internal/repository/project_auth.go`
- `internal/repository/managed_archive_reporter.go`

### 工作数据 / 数据项目

当前实现中，“工作数据/数据项目”指非 `SYS-PERSONAL-*` 的业务项目，即通过业务模板/立项流程形成、有项目卷宗语义、可提交 output、可结项、可上报 manage 的项目。

这些项目的 output 提交后，scan 会尝试上报 manage 的 `/api/sync/file-archive`，形成部门柜记录。项目结项后，原有 `ArchiveUploader` 会继续上报 `/api/sync/project-archive`，manage 拆解成单位室/机要室结构化底账。

## 本轮 scan 端实现

### 1. 文件版本增加部门柜同步状态

新增字段：

- `cabinet_sync_status`：`pending / success / failed / skipped`
- `cabinet_sync_message`
- `cabinet_synced_at`

涉及文件：

- `internal/repository/template_project_schema.go`
- `internal/repository/db.go`
- `internal/models/template_project.go`

### 2. 新增 ManagedArchiveReporter

新增文件：

- `internal/repository/managed_archive_reporter.go`
- `internal/repository/managed_archive_reporter_test.go`

职责：

- 读取 `manage_endpoint` 或 `archive_endpoint`
- 读取 `sync_token` 或 `manage_token`
- 构造 `data-asset-scan/file-archive-v1` payload
- 上报到 `{endpoint}/api/sync/file-archive`
- 成功后写 `cabinet_sync_status = success`
- 失败后写 `cabinet_sync_status = failed`
- 未配置 endpoint 或个人项目时写 `skipped`
- 不上传实体文件内容

重要规则：

- 仅 `data_state = output`
- 仅 `lifecycle_status = registered`
- 仅已经 `submitted_at` 的 output 文件
- `SYS-PERSONAL-*` 个人项目跳过上报

### 3. output 提交后自动触发部门柜上报

修改文件：

- `internal/repository/file_operation.go`

在 `SubmitOutput` 成功后调用：

```go
_, _ = NewManagedArchiveReporter(s.DB).ReportFileVersionToCabinet(context.Background(), fvID)
```

上报失败不回滚本地提交，只记录同步状态和失败消息。

### 4. 新增部门柜重试接口

修改文件：

- `internal/httpd/file_versions.go`

新增接口：

```text
POST /file-versions/:id/sync-cabinet
```

用途：

- 对已经提交的 output 文件版本手动重试上报 manage
- 个人项目或未配置 endpoint 会返回成功但说明 skipped

### 5. 项目结项 manifest 补充归档元信息

修改文件：

- `internal/repository/project_close.go`

manifest 增加：

```json
{
  "archive_phase": "unit_release",
  "storage_authority": "manage",
  "transfer_mode": "structured_manifest"
}
```

同时在结项 manifest 的文件版本和底账中补充本地 id，便于 manage 侧形成结构化快照。

## scan → manage 文件上报 payload

接口：

```text
POST /api/sync/file-archive
```

schema：

```json
{
  "schema": "data-asset-scan/file-archive-v1",
  "archive_phase": "department_cabinet",
  "source_terminal": "data-asset-scan",
  "generated_at": "RFC3339",
  "project": {},
  "stage": {},
  "file_version": {},
  "ledger": {},
  "lifecycle_event": {}
}
```

## 配置说明

scan 从 `system_config` 读取：

- `manage_endpoint`
- `archive_endpoint`
- `manage_token`
- `sync_token`

本机用户当前说明 manage 端口就是 `3002`。新电脑恢复时，如果 manage 仍跑 `3002`，scan 系统设置里保持：

```text
manage_endpoint = http://localhost:3002
archive_endpoint = http://localhost:3002
```

## 用户视角体验链路

个人项目链路：

```text
个人核心/重要/一般项目 output 提交
→ scan 本地提交成功
→ cabinet_sync_status = skipped
→ manage 不出现数据
```

工作项目链路：

```text
创建非 SYS-PERSONAL-* 的业务项目
→ 在项目工作台上传/产出 output
→ 点击 output 提交
→ scan 自动上报 manage file-archive
→ manage “文件管理 / 柜室归档”出现部门柜记录
→ scan 对项目执行结项
→ manage “柜室归档”补充单位室/机要室记录
```

## 测试命令

本轮已经通过的测试：

```bash
cd ~/Code/xueba/data-asset-scan-go
go test ./internal/repository ./internal/httpd
```

重点覆盖：

- 未配置 manage endpoint 时 output 提交不阻塞
- mock manage 成功接收 `file-archive-v1`
- manage 返回错误时本地提交不回滚，同步状态记录失败
- `POST /file-versions/:id/sync-cabinet` 可重试

## 新电脑恢复步骤

```bash
git clone git@gitee.com:jy-development/data-asset-scan.git
cd data-asset-scan
git checkout go-test-template
go mod download
```

然后根据项目原有启动方式启动 scan。若需要前端：

```bash
npm install
```

注意：scan 的本地 SQLite 数据默认不在 Git 中。当前 macOS 运行库路径为：

```text
~/.local/share/data-asset-scan/data.db
```

如果要恢复旧电脑上的演示数据，需要手动复制该数据库文件到新电脑同一路径。若不复制，项目可运行，但需要重新同步模板、创建/操作项目并重新产生数据。

## 与 manage 仓库的配合

本仓库只负责 scan 工作端：

- 提交 output
- 记录本地 file_versions / asset_ledgers
- 上报结构化 JSON
- 结项上报 manifest

manage 仓库负责：

- 接收 `/api/sync/file-archive`
- 接收 `/api/sync/project-archive`
- 写入 `managed_archive_*` 结构化表
- 提供“柜室归档”页面

## 已知注意事项

- 个人项目不上报 manage，这是当前业务边界，不是 bug。
- scan 本轮不创建“柜/室”本地目录，不伪装实体迁移。
- manage 页面如果没有数据，优先检查当前操作的项目是不是 `SYS-PERSONAL-*`。
- 若需要“个人核心定稿也进入 manage”，需要修改业务边界和 `ManagedArchiveReporter` 的跳过逻辑。

