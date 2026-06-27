# 集中立项流程重构（立项→承接→环节任务）设计

日期：2026-06-02
涉及仓库：scan（Wails+Go+Vue）、manage（Nuxt+Nitro）

## 目标流程

1. **立项人**在独立「立项」页填：项目名称、定数权（数据所有权归属）、负责人 → 点立项 → 同步 manage（**不选模版**）。
2. **被指派负责人**登录 scan → 看到"待我承接"的项目 → 选（已发布）模版 → 为每个 环节/文件任务/文件标识 指派责任人 → 提交 → 同步 manage。
3. **各环节责任人**登录 scan → 看到环节任务 → 开始工作 →（后续同现状）。

## 已确认决策

- **A 去审核**：立项即可承接，移除审核/驳回（manage submit 直接落 `status='approved'`；移除 review/reject 接口与管理端审核界面）。
- **B 文件层只记录**：文件任务/文件标识的指派只做"定责到人"的记录，真正"开始工作/交付"仍按环节走现有流程，不做文件级任务收件箱。
- **C 立项归一**：立项统一走本流程；`/project-initiation` 去掉「确认立项」，退化为模版库（另存为/编辑/发布）。承接选模版只列已发布模版（复用 is_published 闸门）。

## 复用现状（基本不动）

- scan：`CentralizedProjectView`(/centralized-projects)、`ProjectAcceptanceView`(/project-acceptance)、`StageTasksView`(/stage-tasks)，后端 `internal/httpd/centralized_projects.go`，本地表 `centralized_project_applications` / 同步到 manage `/api/centralized-projects/submit`。
- manage：`centralized-project-repository.ts`、`server/api/centralized-projects/*`、表 `centralized_project_applications`、`centralized_project_stage_assignments`。
- 环节责任人执行：`my-stages` → `start-stage` → 接回现有「我的工作事项 / 开始工作」。

## 分阶段实现（每阶段 TDD，用例通过再进下一阶段）

### Phase 1 — 立项归一 + 定数权 + 去审核 + 导航接回
- scan：App.vue 恢复导航「立项」(/centralized-projects，由"数据业务集中立项"改名)、「项目承接」(/project-acceptance)、「我的环节任务」(/stage-tasks)。
- scan：`CentralizedProjectView` 立项表单新增「定数权（数据所有权归属）」`data_owner` 字段。
- scan 后端：`CreateCentralizedProjectRequest` + 本地表加列 `data_owner`，push payload 带上。
- manage：`/submit` 接收 `data_owner`；表加列 `data_owner`；提交后 `status='approved'`（去审核）。
- manage：移除审核/驳回接口（review/reject）与管理端审核 UI；scan 端"拉审核结果"退化为只读同步（不再有 pending/rejected 态阻断承接）。

### Phase 2 — 承接三层指派（记录）
- manage：新增表 `centralized_project_task_assignments`、`centralized_project_file_rule_assignments`；`/accept` 入参扩展 `task_assignments` / `file_rule_assignments`；仓储写入。
- scan：`ProjectAcceptanceView` 承接对话框 环节 ▸ 文件任务 ▸ 文件标识 逐层展开指派；`/accept` 透传三层。
- scan 后端：`AcceptCentralizedProject` 透传三层到 manage。
- 注：B 决策——这两层只落库记录，不衍生独立任务收件箱。

### Phase 3 — 退化 /project-initiation + 收件箱合并（2026-06-02 追加）
- scan：移除 `ProjectInitiationView` 的「确认立项」「编辑并立项」入口，页面退化为模版库（另存为/编辑/发布跳转编辑器）；移除 `TemplateTreeEditorView` 的「确认立项」（保留发布）。
- **收件箱合并**：环节责任人统一在 **「我的工作事项」(/my-work-items)** 看任务、开工、交付——把集中立项的 `my-stages`/`start-stage`/`deliver` 接入这块看板；移除独立的「我的环节任务」(/stage-tasks) 导航（路由保留回滚）。
  - 看板三列直接由 stage 状态驱动：pending→待办、in_progress→进行中、completed→已结束。
  - 开始工作 → 集中立项 `start-stage`（建 `CPA-{application_id}` 目录树）；交付 → 集中立项 `deliver`（output→下游 input）。
  - 在线编辑复用现有 stage 过程文档编辑：以虚拟项目码 `CPA-{application_id}` 作为 template_code 调 `work-items/process-docs|doc`（路径 `{root}/CPA-{app_id}/stages/{stage_code}/process`）。
- 保留 /project-initiation、/stage-tasks 路由作回滚后路。

## 测试要点
- scan Go：立项创建/同步带 data_owner；（Phase2）承接三层指派透传；去审核后立项即 approved。
- manage：submit 带 data_owner 且直接 approved；（Phase2）accept 写三层指派表。
- 前端：立项表单含定数权字段、导航出现三入口；承接对话框三层展开（Phase2）。

## 风险/回滚
- 改动触及上周刚上线流程；每阶段独立提交，/project-initiation 路由保留可回滚。
- 去审核为不可逆流程语义变更，已用户确认。
