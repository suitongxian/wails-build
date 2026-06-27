# 三级分工级联 · P1「工作事项分工」设计

日期：2026-06-09
仓库：data-asset-scan（branch go-test-template）+ data-asset-manage
范围：三级分工级联的**第一阶段（P1）**——项目负责人的"选模版 + 派环节负责人"，含未读角标基建。

## 1. 背景与总体愿景（仅作上下文）

把现有「立项 → 承接 → 工作」从**两级**重构为**三级分工级联**，覆盖 5 层模版（行业 ▸ 业务/项目 ▸ 工作事项/stage ▸ 文件任务/task ▸ 文档标识/file_rule）：

1. **项目负责人**：在「工作事项分工」选模版 → 为每个**工作环节**派负责人。
2. **工作环节负责人**：在「文件任务指派」为每个**文件任务**派工作参与人。
3. **文件任务参与人**：在「文件任务受理」点「开始工作」，按模版建目录/空文件，接入在线编辑。

指派状态归 **manage**（沿用现有架构，方案 A）。manage 端已有 `centralized_project_stage_assignments`（环节级，带状态）、`centralized_project_task_assignments`（任务级，目前仅记录、缺 status）、`template_tasks`/`template_file_rules`（5 层结构，`template_file_rules.template_task_id` 关联到任务）。

### 分阶段交付

| 阶段 | 内容 | 仓库 |
|---|---|---|
| **P1**（本文档） | 工作事项分工：选模版→确定→分工(派环节负责人)→提交；未读角标基建 | scan + manage |
| P2 | 文件任务指派：环节负责人给文件任务派参与人（task 加 status + 接口） | scan + manage |
| P3 | 文件任务受理：参与人「开始工作」按 task 建目录/空文件 → 接在线编辑；届时删「我的工作事项」 | scan + manage |

逐阶段 设计→计划→实现，验证通过再下一阶段。

## 2. P1 范围与边界

**做**：Tier 1 视图（改写 ProjectAcceptanceView）+ 「项目承接」改名「工作事项分工」+ 该导航未读角标 + manage 的 `set-template`/未读接口。

**不做（留给 P2/P3）**：
- 不删、不改「我的工作事项」（P1 期间原样保留，保证环节负责人现有的"开始工作"不断）。
- 不加「文件任务指派/受理」导航。
- 不动 task/file_rule 级指派的执行流程。
- Tier 2/3 的未读角标随各自阶段做（P1 只做 Tier 1 角标）。

## 3. 关键设计决定（已与用户确认）

- **「确定」与「分工」解耦**：「确定」(选模版) 把模版关联存到 manage、**状态保持 approved**；「分工→提交」才写环节负责人并把状态推到 `accepted`。按钮态可断点续做。
- **按钮态从数据推导**，不新增子状态：
  - `template_id` 为空 → 「选择模版」
  - `template_id` 有 且 `status=approved` → 「分工」
  - `status=accepted` → 「已分工」（只读）
- **未读语义**：未读 = 我名下、`status=approved`、`owner_viewed_at` 为空 的项目数；进入「工作事项分工」页时整列清零（置 `owner_viewed_at`）。后续新指派的项目 `owner_viewed_at` 仍为空 → 角标重现。
- **角标范围**：三个导航最终都加未读角标，但 P1 只实现 Tier 1 的。

## 4. manage 端改动（data-asset-manage）

### 4.1 schema
`centralized_project_applications` 新增列（按 index.ts 现有迁移风格、幂等加列）：
- `owner_viewed_at DATETIME`（负责人最近一次打开分工页的时间；NULL = 未读）

### 4.2 新接口
- `POST /api/centralized-projects/set-template?id=<application_id>`
  - body：`{ acceptor: string, template_id: number, template_code: string, template_version: string }`
  - 校验：项目存在且 `status='approved'`；`acceptor === owner_name`（与 accept 同款校验）。
  - 写：`template_id/template_code/template_version`，**status 保持 approved**，更新 `update_time`。
  - 返回更新后的项目。
  - 仓储方法：`centralizedProjectRepository.setTemplate(id, {...})`。
- `GET /api/centralized-projects/unread-count?owner=<username>`
  - 返回 `{ count }`：`SELECT COUNT(*) ... WHERE owner_name=? AND status='approved' AND owner_viewed_at IS NULL AND disabled=0`。
- `POST /api/centralized-projects/mark-seen?owner=<username>`
  - `UPDATE centralized_project_applications SET owner_viewed_at=CURRENT_TIMESTAMP WHERE owner_name=? AND status='approved' AND owner_viewed_at IS NULL AND disabled=0`，返回 `{ updated }`。

### 4.3 复用（不改）
- `GET /api/centralized-projects/list?status=approved|accepted&owner_name=`（scan `assigned` 代理用）。
- `POST /api/centralized-projects/accept?id=`（分工提交用；template 已由 set-template 写过，accept 仍带 template_* + stage_assignments；以 accept 内写入为准，保持幂等）。

## 5. scan 端改动（data-asset-scan）

### 5.1 代理接口（internal/httpd/centralized_projects.go）
- `POST /centralized-projects/set-template`：取 `?id=`，body 注入 `acceptor=currentOperator(c)`，转发 manage `set-template`。
- `GET /centralized-projects/unread-count`：以 `owner=currentOperator(c)` 转发 manage，回 `{count}`。
- `POST /centralized-projects/mark-seen`：以 `owner=currentOperator(c)` 转发 manage，回 `{updated}`。
- 路由在 `RegisterCentralizedProjectsRoutes` 注册（静态路径，避开 `:remote_id` 冲突）。

### 5.2 导航与角标（App.vue）
- 导航项「项目承接」label 改为「工作事项分工」（route `/project-acceptance` 不变）。
- 给该导航项加 `v-badge`（`:content="unreadCount"` `:model-value="unreadCount > 0"` color error dot/number）。
- `unreadCount` 在 App 挂载时 `GET /centralized-projects/unread-count` 拉取；路由进入 `/project-acceptance` 时 `POST /centralized-projects/mark-seen` 后将 `unreadCount` 置 0。
- 提供一个轻量刷新（如切换路由回来时重拉），不引入轮询。

### 5.3 视图重写（ProjectAcceptanceView.vue）
列表：`GET /centralized-projects/assigned`（我作为 owner 的 approved/accepted 项目）。每行按 §3 按钮态：
- **「选择模版」弹窗**：上部立项基本信息（project_name/project_code/owner/sensitivity 等，只读）；下部「关联模版」下拉（本地 `GET /templates/authoring` + 远程已发布 `GET /templates/remote-list` 合并）；右下「确定」→ `POST /centralized-projects/set-template?id=`。
- **「分工」弹窗**：取所选模版结构（复用 scan 本地 `GET /templates/{localId}` 拿 stages）；逐个工作环节一个负责人下拉（候选 `GET /manage-users`，排除 system_admin）；右下「提交」→ `POST /centralized-projects/{remote_id}/accept`，body `{ acceptor, template_id, template_code, template_version, stage_assignments:[{stage_code,stage_name,assignee_username,sort_order}] }`。
- 提交成功后该行状态变 `accepted`，显示「已分工」。

> 现有 ProjectAcceptanceView 已经一屏完成"选模版+派环节"，本次拆成两步按钮流并接 set-template；其余调用（assigned/template-list/template-tree/accept）复用。

## 6. 数据流

```
项目负责人登录 scan
  → App 拉 unread-count → 导航「工作事项分工」红点
  → 进入页面 → mark-seen（红点清零）→ 列我的项目
  → 行「选择模版」→ set-template（manage 写 template_*，仍 approved）→ 行变「分工」
  → 行「分工」→ accept（manage 写 stage_assignments，status→accepted）→ 行变「已分工」
  → （环节负责人在「我的工作事项」照旧可见自己的环节——P1 不动）
```

## 7. 边界与错误处理
- `set-template` 仅 `status=approved` 可调；已 accepted/closed 拒绝（"项目状态不允许重选模版"）。
- 重选模版（提交分工前）允许覆盖 `template_*`。
- `acceptor !== owner_name` → 拒绝（沿用 accept 校验文案）。
- 「分工」要求模版已选 且 每个环节都派了负责人（沿用 accept 校验）。
- unread-count / mark-seen 仅作用于本人（owner=currentOperator），多人共用终端互不影响。
- manage 不可达：角标拉取/清零失败静默（不阻塞页面）；set-template/accept 失败给出错误提示。

## 8. 测试

### manage（vitest，沿用 tests/centralized-accept-assignments.test.ts 套路）
1. `set-template`：写入 template_* 且 `status` 仍为 approved；非 approved 项目拒绝；acceptor≠owner 拒绝。
2. `unread-count`：仅计 owner 名下 approved 且 owner_viewed_at 为空者。
3. `mark-seen`：把本人未读置已读，`unread-count` 随后归零；不影响他人。
4. 迁移：`owner_viewed_at` 列存在、默认 NULL，旧库幂等加列。

### scan
- Go（httpd，mock manage）：三个代理接口转发正确、注入 operator/owner、回包结构正确。
- vitest（ProjectAcceptanceView）：按钮态机（无模版→选择模版；选模版后→分工；提交后→已分工）；选择模版弹窗调 set-template；分工弹窗提交调 accept 带 stage_assignments；导航角标随 unread-count 显示、进入页面后清零。

## 9. 不在本阶段范围
- 文件任务参与人指派与「文件任务指派/受理」视图（P2/P3）。
- task 级 status、task 级「开始工作」与目录/空文件创建（P3）。
- 删除「我的工作事项」（P3 替换后再删）。
- Tier 2/3 未读角标（随 P2/P3）。
