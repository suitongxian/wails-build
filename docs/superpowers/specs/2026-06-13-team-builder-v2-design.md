# 组建团队 v2 设计稿

日期：2026-06-13
范围：data-asset-scan（前端 + Go 后端）、data-asset-manage（无改动，仅复用）
原型：`docs/mockups/team-builder-v2.html`（已确认）

## 背景与目标

现状的「组建团队」只是一个"建议名单"——把人塞进一个下拉框，分工时仍可从全体注册用户里选。
流程上正确的顺序是 **先组队拉人 → 关联模版 → 分工**，所以团队应当升级为**实际班底**：
分工时默认只能从团队里选人，人不够时再显式补员。

两类组队同构：
- **项目负责人** 组建项目级团队 → 指派**环节责任人**时只从项目团队选。
- **环节责任人** 组建环节级团队 → 指派**文件任务参与人**时只从环节团队选。

## 核心决策（已与用户确认）

1. **团队 = 实际班底**：分工选人控件的数据源从"全体注册用户"收敛为"本团队成员"。
2. **临时拉人入队**：分工页提供按钮，搜全体注册用户补员；补进来的人**永久入队**（A），
   立即可选，后续其它环节/任务也能用。复用既有 `setProjectTeam/setStageTeam`（整表替换）。
3. **组队界面双栏**：左=候选人（全体注册用户，可搜索），右=当前团队全貌；
   每行显示 **姓名 / username / 单位 / 部门 / 角色**。
4. **角色 = 账号属性**（A）：组队阶段不贴意向角色标签；真正职责（组长/核心成员/参与人）
   由分工动作自然产生，已有「工作组」视图按分工结果反推呈现。两者互补。
5. **两层团队关系**：环节级团队不强制是项目级子集（沿用既定结论），各自从全体注册用户拉人。
6. 组队入口保持**弹窗**（够用、不打断流程）；临时拉人后给 snackbar 轻提示。

## 后端改动

### data-asset-manage
无改动。`GET /api/auth-users/list` 已经通过 `toAuthUserProfile` 返回 `role`；
`setProjectTeam/setStageTeam` 已是整表替换，临时拉人 = 前端把"现有团队 + 新人"整表回存。

### data-asset-scan（唯一新增：让 role 落库并带出）
1. `users` 表新增列：`role TEXT NOT NULL DEFAULT ''`（走 db.go 的 columnAdds PRAGMA 守卫，幂等）。
2. `models.User` 增 `Role string` 字段，并加入 `userColumns` 列清单。
3. `ManagedAuthUser` 增 `Role`；`UpsertManagedAuthUser` 的 UPDATE 与 INSERT 都写入 role。
4. `internal/httpd/users.go` 同步结构 `manageAuthUserProfile` 增 `Role string json:"role"`，
   透传给 `UpsertManagedAuthUser`。`GET /users` 随 `models.User` 自动带出 role。
5. team / stage-team 代理不变（已存在）。

## 前端改动（scan，frontend_real）

### 新组件
- `TeamBuilderDialog.vue`：双栏组队弹窗。props：项目/环节上下文 + 初始团队；
  内部拉 `/users`，左栏搜索加入、右栏移除，保存时回存 team 端点。展示单位/部门/角色。
- `TeamAddSheet.vue`：轻量「临时拉人入队」覆盖面板（= 双栏左栏）。搜全体注册用户→加入→
  整表回存对应 team 端点（永久入队）→ 关闭后通知父组件刷新人池。
- （二者共用一行用户展示标记 + `/users` 拉取逻辑，避免重复。）

### 改造
- `ProjectAcceptanceView.vue`：
  - 「组建团队」按钮改为打开 `TeamBuilderDialog`（项目级）。
  - 分工弹窗：环节责任人选人控件**数据源 = 项目团队**；旁加「+ 临时拉人入队」按钮 → `TeamAddSheet`。
- `FileTaskAssignView.vue`：
  - 「组建团队」按钮改为打开 `TeamBuilderDialog`（环节级）。
  - 分工弹窗：文件任务参与人选人控件**数据源 = 环节团队**；旁加「+ 临时拉人入队」→ `TeamAddSheet`。

### 团队全貌
- 组队期：`TeamBuilderDialog` 右栏即全貌。
- 分工后：沿用既有「工作组」视图（组长/核心成员/参与人）。

## 测试计划

- scan Go：
  - 用户同步带 role（`user_sync` 相关）：manage 返回含 role → 本地 users.role 落库 → `/users` 带出。
- scan 前端（vitest）：
  - `TeamBuilderDialog`：渲染单位/部门/角色；左栏加入→右栏出现；移除生效；保存 emit/回存。
  - 分工人池收敛：选人控件只含团队成员；点临时拉人→加入→人池立即出现该人。
- manage：现有 team / auth-users 测试已覆盖，必要时补一条断言 list 含 role。

## 追加：先组队再分工的顺序硬约束（2026-06-13）

流程上必须先组队、再分工，落实为前后端双层保障：

- **项目负责人**：承接(taken) → **必须先组建团队** → 才能「关联模版」。
- **环节责任人**：**必须先组建环节团队** → 才能「分工」(指派文件任务)。

实现：
- 后端硬约束(manage)：`setTemplate` 校验项目团队非空，否则报"请先组建团队，再关联模版"；
  `assignTasks` 校验环节团队非空，否则报"请先组建环节团队，再分工"。
- 前端引导(scan)：manage 的 `findAll`(/list→/assigned) 与 `findStageTasksByAssignee`(/my-stages)
  各带回 `team_count`；行内操作按钮据此变化——
  - 项目：taken 且 team_count=0 → 「组建团队」；team_count>0 且无模版 → 「选择模版」；有模版 → 「分工」。
  - 环节：team_count=0 → 「组建团队」；>0 → 「分工」。
  - 组队保存后刷新列表，按钮自动翻页到下一步。
- gate 条件 = 团队人数 ≥ 1（空团队视为未组建）。

## 非目标（YAGNI）

- 组队阶段贴意向角色标签。
- 把组队升级为独立向导页。
- 团队人数与模版环节数的硬校验/告警（仅靠"收敛人池 + 显式补员"表达）。
