# 三级分工级联 · P2「文件任务指派」设计

日期：2026-06-09
仓库：data-asset-scan（go-test-template）+ data-asset-manage（test-template）
范围：三级分工级联**第二阶段（P2）**——工作环节负责人为每个文件任务指派工作参与人，含 Tier-2 未读角标。

## 1. 背景

P1 已完成：项目负责人选模版 + 为每个工作环节派负责人（写 `centralized_project_stage_assignments`，status pending）。P2 让**工作环节负责人**进一步把该环节下的每个**文件任务(task)**派给**工作参与人**。

manage 现状（已存在，P1 探明）：
- `centralized_project_stage_assignments`：环节→负责人，带 status。P2 在此加 `assignee_viewed_at`（Tier-2 未读）。
- `centralized_project_task_assignments`：任务→参与人，**已存在但缺 status**。P2 加 `status/started_at/completed_at`（status 驱动 P3「开始工作」）。
- `template_tasks`：某环节下的文件任务定义（`template_stage_id` 关联 `template_stages`），P2 据此枚举任务。
- `auth_users` + `/manage-users`：参与人候选。

## 2. 范围与边界

**做**：Tier-2 视图「文件任务指派」+ 新导航 + Tier-2 未读角标 + manage 的 task 指派列/接口。

**不做（P3）**：文件任务参与人的「文件任务受理」视图、task 级「开始工作」建目录/空文件、删除「我的工作事项」。P2 期间「我的工作事项」原样保留。

## 3. 关键设计决定（已确认）

- **每个文件任务一个参与人**（对齐现有 `centralized_project_task_assignments` 一行一 assignee）。
- **Tier-2 未读角标本阶段就做**，与 P1 体验一致——在 stage-assignment 级加 `assignee_viewed_at`。
- 「文件任务指派」导航放「工作事项分工」之后。
- 指派状态归 manage（方案 A）。

## 4. manage 端改动（data-asset-manage）

### 4.1 schema（idempotent addColumnIfMissing）
- `centralized_project_stage_assignments` 加 `assignee_viewed_at DATETIME`（Tier-2 未读，NULL=未读）。
- `centralized_project_task_assignments` 加：
  - `status VARCHAR(40) NOT NULL DEFAULT 'pending'`（pending/in_progress/completed）
  - `started_at DATETIME`、`completed_at DATETIME`（P3「开始工作」用）

### 4.2 repository 方法
- `stageTasksForAssignment(applicationId, stageCode)`：由 `application.template_id` → `template_stages(template_id, stage_code)` → `template_tasks(template_stage_id)` 取任务；左连现有 `centralized_project_task_assignments(application_id, stage_code, task_code, disabled=0)` 回填 `assignee_username`。返回 `[{task_code, task_name, sort_order, assignee_username|null}]`（按 sort_order）。
- `assignTasks(applicationId, stageCode, actor, assignments)`：
  - 校验 actor 是该环节负责人：`centralized_project_stage_assignments` 存在 `application_id=? AND stage_code=? AND assignee_username=actor AND disabled=0`，否则抛错「只有该环节负责人可以分工」。
  - 校验每个 `assignee_username` 已注册 active（复用 `assertOwnerRegistered`）。
  - 整体替换该环节任务指派（事务）：`UPDATE ... SET disabled=1 WHERE application_id=? AND stage_code=?`；再插入新行 `(application_id, stage_code, task_code, task_name, assignee_username, sort_order, status='pending')`；跳过空 assignee。
- `stageUnreadCountForAssignee(assignee)`：`COUNT(*) FROM centralized_project_stage_assignments WHERE assignee_username=? AND assignee_viewed_at IS NULL AND disabled=0`。
- `markStagesSeenForAssignee(assignee)`：`UPDATE ... SET assignee_viewed_at=CURRENT_TIMESTAMP WHERE assignee_username=? AND assignee_viewed_at IS NULL AND disabled=0`，返回 changes。

### 4.3 接口（envelope `{code,message,data}`）
- `GET /api/centralized-projects/stage-tasks?application_id=&stage_code=` → `data:[{task_code,task_name,assignee_username}]`。
- `POST /api/centralized-projects/assign-tasks?application_id=&stage_code=` body `{actor, assignments:[{task_code,task_name,assignee_username}]}` → `data:{stage_code, count}`；try/catch→400。
- `GET /api/centralized-projects/stage-unread-count?assignee=` → `data:{count}`。
- `POST /api/centralized-projects/mark-stages-seen` body `{assignee}` → `data:{updated}`。

## 5. scan 端改动（data-asset-scan）

### 5.1 代理接口（internal/httpd/centralized_projects.go）
- `GET /centralized-projects/stage-tasks`：透传 `application_id`、`stage_code` query。
- `POST /centralized-projects/assign-tasks`：注入 `actor=currentOperator(c)` 进 body，透传 `application_id`、`stage_code` query。
- `GET /centralized-projects/stage-unread-count`：`assignee=currentOperator(c)` query。
- `POST /centralized-projects/mark-stages-seen`：`{assignee: operator}` body。
- 复用 P1 的 `getManageEndpoint`/`proxyToManage`。

### 5.2 导航 + 角标（App.vue）
- 新导航项「文件任务指派」`to:'/file-task-assign'`，`badge:'filetask'`，放「工作事项分工」之后。
- `filetaskUnread` ref；`loadFiletaskUnread()`（GET stage-unread-count）；`clearFiletaskUnread()`（POST mark-stages-seen 成功后清零）。onMounted 拉取；route watch：进入 `/file-task-assign` 清零、离开重拉（带 shellless 守卫）。
- 列表项 `#append` 角标条件 `child.badge==='filetask' && filetaskUnread>0`。

### 5.3 路由 + 视图
- `plugins/router.ts` 加 `/file-task-assign` → 新视图 `FileTaskAssignView.vue`。
- `FileTaskAssignView.vue`：
  - 列表 = 我的工作环节（`GET /centralized-projects/my-stages`，返回 stage-assignment + project 信息）。每行展示 项目名/环节名/状态，末尾「分工」按钮。
  - 「分工」→ 弹窗:`GET /centralized-projects/stage-tasks?application_id=&stage_code=` 列出任务,每任务一个参与人下拉(候选 `/manage-users` 排除 system_admin,回填已有 assignee)→「提交」→ `POST /centralized-projects/assign-tasks`（带 assignments）→ 成功关弹窗 + 刷新。

## 6. 数据流
```
环节负责人登录 → App 拉 stage-unread-count → 「文件任务指派」红点
  → 进入页面 → mark-stages-seen 清零 → 列我的环节
  → 行「分工」→ stage-tasks 列任务 → 逐任务选参与人 → assign-tasks（写 task 指派 status=pending）
  → （P3：参与人在「文件任务受理」开始工作）
```

## 7. 边界与错误
- assign-tasks：actor 非该环节负责人 → 拒绝;参与人未注册 → 拒绝;整体替换(可重复分工覆盖)。
- 任务列表为空(模版该环节无 task)→ 弹窗提示「该环节无文件任务」。
- stage-unread/mark-stages-seen 仅作用本人(assignee=operator)。
- manage 不可达：角标静默;分工提交失败给提示。
- 仅 status 字段加默认 'pending'：既有 task 指派行(P1 accept 可能写过的"仅记录")升级后 status 默认 pending,符合预期。

## 8. 测试
### manage（vitest，复用 in-memory DDL 套路；新 DDL 含新列）
1. 迁移：task 表有 status/started_at/completed_at，stage 表有 assignee_viewed_at。
2. `stageTasksForAssignment`：返回模版该环节任务 + 回填已有参与人。
3. `assignTasks`：actor=环节负责人通过、非负责人拒绝;参与人未注册拒绝;整体替换 + status=pending。
4. `stageUnreadCountForAssignee`/`markStagesSeenForAssignee`：仅本人、置已读归零、不影响他人。

### scan
- Go（httpd，mock manage）：四个代理转发正确、注入 actor/assignee、query/body 位置正确。
- vitest（FileTaskAssignView）：列环节;「分工」弹窗列任务 + 选参与人;提交调 assign-tasks 带 assignments;空任务提示。

## 9. 不在本阶段范围
- 文件任务受理视图 + task 级「开始工作」建目录/空文件（P3）。
- 删除「我的工作事项」（P3）。
