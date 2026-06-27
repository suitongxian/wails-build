# 三级分工级联 · P3「文件任务受理 + 开始工作」设计

日期：2026-06-09
仓库：data-asset-scan（go-test-template）+ data-asset-manage（test-template）
范围：三级分工级联**第三/末阶段（P3）**——文件任务参与人受理任务、「开始工作」按模版建目录/空文件并接入现有在线编辑，含 Tier-3 未读角标；并下线「我的工作事项」导航。

## 1. 背景

P1（负责人选模版+派环节负责人）、P2（环节负责人派文件任务参与人，写 `centralized_project_task_assignments` status=pending）已完成。P3 让**文件任务参与人**受理分给自己的文件任务并开始工作。

现状（探明）：
- `centralized_project_task_assignments`（manage）：任务→参与人，带 status（pending/in_progress/completed，P2 加）。P3 加 `assignee_viewed_at`（Tier-3 未读）。
- 任务指派 manage-only，scan 代理读。scan 无本地任务表。
- scan 本地模版五层：`template_file_rules.template_task_id` 关联 `template_tasks.id`，可按 (stage_code, task_code) 定位某任务的文件标识。
- scan 既有 stage 级脚手架 `ScaffoldStageProcessDocsForProject(db, templateCode, virtualProjectCode, stageCode)`（读 `template_file_rules WHERE template_stage_id=? AND data_state='process'`，在 `CPA-{appid}/stages/{stage}/process/` 建空文件）。`ProjectWorkspace.CreateStageDir/StageStateDir` 跨平台（filepath.Join）。
- 现有工作台路由 `/stage-workbench?app_id=&stage_code=&stage_name=&project_name=&...`（文件浏览/在线编辑）。

## 2. 范围与边界

**做**：Tier-3 视图「文件任务受理」+ 新导航 + Tier-3 未读角标 + task 级「开始工作」(建目录/空文件) + 接现有工作台 + manage 的 my-tasks/start-task/未读接口 + 下线「我的工作事项」导航。

**安全**：「开始工作」只在工作空间 `CPA-{appid}/stages/{stage}/process/` **创建空文件**（幂等、已存在则跳过），**不删改任何扫描文件**（符合 [[feedback-scan-rules]]）；路径用 `ProjectWorkspace`（filepath.Join，跨平台，符合 [[feedback-cross-platform-mandatory]]）。

## 3. 关键设计决定

- **「开始工作」粒度=任务**：脚手架只建该任务的文件标识（`template_file_rules WHERE template_task_id=该任务 AND data_state='process'`），落在该环节 `process/` 目录。
- **接现有编辑**：开始工作成功后，前端导航到既有 `/stage-workbench?app_id=&stage_code=&...`（该环节文件浏览/在线编辑，含刚建的任务文件）。不新造编辑器。
- **状态**：task pending→in_progress（+started_at），仅任务参与人本人可开始。
- **Tier-3 未读**：task 级 `assignee_viewed_at`，进「文件任务受理」清零。
- **下线「我的工作事项」**：从导航移除（路由保留作回滚后路，不删 MyWorkItemsView）。三级级联现以 工作事项分工 / 文件任务指派 / 文件任务受理 取代它。

## 4. manage 端改动

### 4.1 schema（addColumnIfMissing，置于 task 表创建之后）
- `centralized_project_task_assignments` 加 `assignee_viewed_at DATETIME`。

### 4.2 repository 方法
- `myTasksForAssignee(assignee)`：`centralized_project_task_assignments t JOIN centralized_project_applications a ON a.id=t.application_id` WHERE `t.assignee_username=? AND t.disabled=0 AND a.disabled=0`，选 `t.application_id, t.stage_code, t.task_code, t.task_name, t.status, a.project_name, a.template_code, a.template_id`，并 LEFT JOIN `centralized_project_stage_assignments` 取 `stage_name`（按 application_id+stage_code）。ORDER BY application_id DESC, id。
- `startTask(applicationId, stageCode, taskCode, actor)`：校验存在 `centralized_project_task_assignments WHERE application_id=? AND stage_code=? AND task_code=? AND assignee_username=actor AND disabled=0`，否则抛「只有该文件任务参与人可以开始工作」；置 `status='in_progress', started_at=CURRENT_TIMESTAMP`（仅当当前非 completed）；返回该任务行（含 template_code 供 scan 脚手架）。
- `taskUnreadCountForAssignee(assignee)`：`COUNT(*) WHERE assignee_username=? AND assignee_viewed_at IS NULL AND disabled=0`。
- `markTasksSeenForAssignee(assignee)`：置 `assignee_viewed_at`，返回 changes。

### 4.3 接口
- `GET /api/centralized-projects/my-tasks?assignee=` → `data:[{application_id,stage_code,stage_name,task_code,task_name,status,project_name,template_code}]`。
- `POST /api/centralized-projects/start-task` body `{actor, application_id, stage_code, task_code}` → 置 in_progress，返回 `data:{...task,template_code}`；catch→400。
- `GET /api/centralized-projects/task-unread-count?assignee=` → `{count}`。
- `POST /api/centralized-projects/mark-tasks-seen` body `{assignee}` → `{updated}`。

## 5. scan 端改动

### 5.1 代理 / 处理接口（internal/httpd/centralized_projects.go）
- `GET /centralized-projects/my-tasks`：`assignee=operator` 代理。
- `GET /centralized-projects/task-unread-count`：`assignee=operator` 代理。
- `POST /centralized-projects/mark-tasks-seen`：`{assignee:operator}` body 代理。
- `POST /centralized-projects/start-task`（**非纯代理**）：body `{application_id, stage_code, task_code, template_code}`。
  1. 调 manage `start-task`（注入 actor=operator），失败则返回错误、不建文件。
  2. 成功后本地脚手架：`virtualProjectCode = CPA-{application_id}`；`ProjectWorkspace.CreateStageDir(vp, stage_code)`；新助手 `ScaffoldTaskDocsForProject(db, template_code, vp, stage_code, task_code)`。
  3. 返回 `data:{scaffolded, app_id, stage_code}`。

### 5.2 脚手架助手（internal/repository/stage_scaffold.go）
- `ScaffoldTaskDocsForProject(db, templateCode, virtualProjectCode, stageCode, taskCode) ([]string, error)`：模版→stage(stage_code)→task(template_tasks WHERE template_stage_id AND task_code)→`template_file_rules WHERE template_task_id=task.id AND data_state='process' AND disable=0`；在 `StageStateDir(vp, stageCode, 'process')` 建空文件（沿用 `sanitizeFileName`+`firstFileExt`，幂等跳过已存在）。返回创建路径。

### 5.3 视图 + 导航 + 路由
- 新视图 `FileTaskReceiveView.vue`：列「我的文件任务」(`GET /centralized-projects/my-tasks`)；每行展示 项目/环节/任务/状态；`status==='pending'` 显示「开始工作」按钮 → `POST start-task` → 成功后导航 `/stage-workbench?app_id={application_id}&stage_code={stage_code}&stage_name={stage_name}&project_name={project_name}`（接现有编辑）；非 pending 显示状态或「进入工作台」按钮直接进 workbench。
- 路由 `/file-task-receive` → FileTaskReceiveView。
- 导航「文件任务受理」放「文件任务指派」之后，`badge:'recvtask'`，Tier-3 角标（`task-unread-count`/`mark-tasks-seen`）。
- **下线「我的工作事项」**：从 navItems 注释掉该项（保留路由 /my-work-items 与 MyWorkItemsView 作回滚后路）。

## 6. 数据流
```
任务参与人登录 → App 拉 task-unread-count → 「文件任务受理」红点
  → 进入页面 → mark-tasks-seen 清零 → 列我的文件任务
  → 行「开始工作」→ start-task（manage 置 in_progress；scan 建 CPA 目录+任务空文件）
  → 导航 /stage-workbench（现有文件浏览/在线编辑，含刚建文件）→ 编辑/交付（沿用现有）
```

## 7. 边界与错误
- start-task：actor 非任务参与人 → manage 拒绝、scan 不建文件。
- 任务无 process 文件标识 → 脚手架建 0 文件，仍进工作台（不报错）。
- 模版本地缺失 → 脚手架返回错误提示「本地模版缺失，请先同步」。
- task-unread/mark-tasks-seen 仅本人。
- manage 不可达：角标静默；开始工作失败给提示。

## 8. 测试
### manage（vitest）
1. 迁移：task 表有 assignee_viewed_at。
2. `myTasksForAssignee`：返回我的任务 + 项目/环节/模版上下文。
3. `startTask`：仅参与人可开始（非参与人抛错），置 in_progress + started_at。
4. task 未读 count / markSeen。

### scan
- Go（repository）：`ScaffoldTaskDocsForProject` 按 task 建对应 process 空文件（种本地模版五层）。
- Go（httpd，mock manage + temp workspace）：start-task 成功路径建文件 + 置状态；actor 非参与人(manage 返回错误)时不建文件；4 代理 query/body 正确。
- vitest（FileTaskReceiveView）：列任务；pending 显示开始工作；开始工作调 start-task 后导航 workbench；导航不再含「我的工作事项」、含「文件任务受理」。

## 9. 完成后
三级分工级联全链路打通：立项 → 工作事项分工(P1) → 文件任务指派(P2) → 文件任务受理+开始工作(P3) → 现有在线编辑/交付/结项。
