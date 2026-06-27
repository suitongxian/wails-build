# scan 立项页改造：「新建立项书」对话框 + 草稿态

日期：2026-06-08
分支：go-test-template
范围：data-asset-scan 的「项目立项」页（`/centralized-projects` → `CentralizedProjectView.vue`）及其后端

## 1. 背景与现状

当前「项目立项」页顶部是一个**内联极简表单**（项目名称、定数权、负责人、敏感级），填完直接点「项目立项」：

- 后端 `POST /centralized-projects`，请求体仅 `{project_name, owner_name, data_owner, sensitivity_level}`。
- 创建即 `status='approved'` 并**立即推送 manage**（`pushCentralizedProjectToManage`，best-effort）。
- **没有草稿态**：不存在"存了但未提交"的中间状态。
- 立项阶段**不关联模版**（模版由项目负责人在「项目承接」环节、给各工作环节分配责任人时才选）。

本次改造把内联表单换成「新建立项书」对话框，扩充字段，并引入「存草稿 / 发布」两态。**立项阶段仍不关联模版**（保持现有架构，模版关联是第二步）。

## 2. 目标

1. 顶部内联表单替换为 **「+ 新建立项书」按钮** → 弹出五板块对话框。
2. 新增字段：项目代号、所属部门、立项依据、项目简介。
3. 引入 **草稿态**：存草稿只存本地、不推 manage，可重开编辑；发布才推 manage。
4. 列表对草稿行提供「编辑」入口与「草稿」状态标识。

## 3. 对话框字段规格

| 板块 | 字段 | 必填 | 控件 | 数据来源 / 落库字段 |
|---|---|---|---|---|
| ① 基本信息 | 项目名称 | 是 | 文本 | `project_name`（已有） |
| | 项目代号 | 否 | 文本（手填，不校验唯一） | `project_code`（**新增**） |
| ② 责任主体 | 项目负责人 | 是 | 下拉（autocomplete） | `/manage-users`，排除 `role=system_admin`；存 `owner_name`（username） |
| | 所属部门 | 是 | **只读**自动填（不可手改） | 选负责人后取其 `user_department`；存 `department`（**新增**） |
| ③ 安全定级 | 敏感级别 | 是 | 单选 `v-radio-group`（核心/重要/一般） | `sensitivity_level`（已有；core/important/general） |
| | 数据权属 | 否 | 文本 | **复用** `data_owner`（原"定数权"，本次改名"数据权属"并改为选填） |
| ④ 立项依据 | （单字段） | 否 | 多行文本框 `v-textarea`（纯文本，非富文本） | `approval_basis`（**新增**） |
| ⑤ 项目简介 | （单字段） | 否 | 多行文本框 `v-textarea`（纯文本） | `description`（**新增**） |

按钮：**发布 / 存草稿 / 取消**

## 4. 状态与生命周期

`centralized_project_applications.status` 新增取值 `draft`，与现有 `approved/accepted/closed/rejected` 并列。

- **存草稿**：`status='draft'`，仅写 scan 本地，**不推 manage**（`sync_status` 保持未同步）。校验仅要求"项目名称"非空（允许半成品）。
- **发布**：`status='approved'` + 推送 manage（等同现有立项行为）。校验全部必填：项目名称、负责人、所属部门、敏感级别。
- **取消**：关闭弹窗，不落库。
- **草稿可重开**：从列表点「编辑」回填对话框 → 再次「存草稿」或「发布」。从草稿发布后，该记录由 `draft` 变 `approved` 并推送。

约束：仅 `status='draft'` 的记录可被编辑/更新；非草稿记录不走更新接口。

## 5. 后端改动

### 5.1 表结构
`centralized_project_applications` 幂等 `ALTER` 新增 4 列（忽略 duplicate column）：
- `project_code TEXT`
- `department TEXT`
- `approval_basis TEXT`
- `description TEXT`

（`data_owner` 已存在，复用。）

### 5.2 接口
- `POST /centralized-projects`：请求体新增 `project_code, department, approval_basis, description, save_as_draft(bool)`。
  - `save_as_draft=true` → `status='draft'`，不推 manage。
  - `save_as_draft=false`（默认） → `status='approved'`，推 manage（现有逻辑）。
- `PUT /centralized-projects/:id`：编辑草稿，**仅当当前 `status='draft'`** 才允许；同样带 `save_as_draft`：
  - `true` → 保持 `draft`。
  - `false` → 从草稿发布：置 `approved` 并推 manage。
  - 对非草稿记录返回业务错误（如"仅草稿可编辑"）。

### 5.3 推送 manage 的字段
部门/代号/立项依据/简介**先保证 scan 本地存储**。推送 manage 时，按 manage 建项接口实际能接收的字段一并带上；manage 不接收的字段保持 scan 本地（实现时核对 `pushCentralizedProjectToManage` 与 manage 端建项 API，不为此阻塞本地功能）。

## 6. 前端改动（`CentralizedProjectView.vue`）

- 移除顶部内联表单（约 258–329 行）。
- 新增「+ 新建立项书」按钮 + `v-dialog` 五板块表单组件。
- `loadOwnerOptions` 扩展：候选项携带 `user_department`，供选负责人后自动填部门。
- 选负责人 `@update` → `form.department = 选中用户.user_department`（只读展示）。
- 两套校验：`canSaveDraft`（仅项目名称非空）、`canPublish`（全部必填）。
- 「存草稿」「发布」分别带 `save_as_draft` 调 POST（新建）或 PUT（编辑草稿）。
- 列表「状态」列增加 `draft → 草稿`（灰色 chip）映射；草稿行增加「编辑」按钮（回填对话框，记录 `editingId`）。

## 7. 测试

### 后端（Go，沿用 `centralized_projects_test.go` 套路）
1. `save_as_draft=true` 创建 → `status='draft'`，**未触发**推送 manage。
2. `save_as_draft=false` 创建 → `status='approved'`，触发推送（mock manage）。
3. 草稿 `PUT` 更新字段 → 仍 `draft`，字段已变。
4. 草稿 `PUT save_as_draft=false` → `approved` 并推送。
5. 对非草稿记录 `PUT` → 业务错误。
6. 发布校验：缺必填（如无负责人）→ 拒绝。
7. 新增 4 列读写往返正确。

### 前端（vitest）
1. 点「新建立项书」弹出对话框，五板块渲染。
2. 选负责人 → 所属部门自动填且只读。
3. 「存草稿」走 `save_as_draft=true`；「发布」走 `save_as_draft=false`。
4. 缺必填时「发布」禁用，「存草稿」可用（项目名称已填）。
5. 列表草稿行展示「草稿」标识与「编辑」入口。

## 8. 不在本次范围

- 「项目承接」环节及模版关联（第二步，保持不变）。
- 富文本编辑（明确改为纯文本框）。
- 项目代号唯一性校验 / 自动生成（本次手填、不校验）。
- manage 端字段扩展（仅在其现有接口可接收时顺带带上）。
