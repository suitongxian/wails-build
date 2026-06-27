# V3：MVP 查漏补缺（严格依据需求文档）

> 状态：v0.3（执行版）
> 用户口径："基于所有文档实现正规 MVP，功能点可简陋但禁止缺失，禁止糊弄"
> §18 七条待确认问题：保留，V4 处理
> §12 五项暂不实现：保留，V5 视需要

---

## 一、对账结论：V1+V2 完成 80%，V3 是查漏补缺

文档要求按 §2 模块清单 / §7 模块详细设计 / §11 审计要求 / §13 内置示例 / §8 API 五条主线逐项核对：

### A. 模版库管理（§7.1）

| 文档要求 | 状态 |
|---|---|
| 创建模版 | ✅ manage |
| 编辑草稿（仅 draft） | ✅ manage |
| 发布模版（校验） | ✅ manage |
| **废弃模版** (deprecated) | ✅ manage `/api/templates/deprecate` |
| **复制模版**（企业派生）| ❌ 缺失 |
| **导入导出**（JSON/YAML，含 securityDefaults + permissionDefaults）| ❌ 缺失 |
| **版本升级**（创建新版本不覆盖旧）| ⚠️ 部分（发布后拒绝原地改，但无显式"创建新版本"按钮）|

### B. 项目环节模块（§7.3）

| 文档要求 | 状态 |
|---|---|
| 查询环节 | ✅ |
| **调整环节状态: pending / running / completed / skipped** | ❌ 状态切换接口未实现 |
| 查看环节文件版本 | ✅ |
| 维护环节责任人 | ⚠️ V2 加 user_id 后基本可表达，但缺管理 UI |

### C. 权限动作（§7.7）— 9 个

| 动作 | 状态 |
|---|---|
| read / write / receive / submit / share / archive / close | ✅ V1+V2-5 严格化 |
| **upload**（独立于 write）| ⚠️ security_policy_seed 用了但 PermXxx 常量未定义 |
| **destroy** | ⚠️ 同上，常量未定义；销账流程也没用 |

### D. 审计日志（§11）

| 文档要求 | 状态 |
|---|---|
| **§11.2 audit_logs 表 8 字段** | ❌ 表不存在（V2-4 只补了 lifecycle_events.operator_user_id） |
| §11.1.1 模版发布/废弃/复制/导入 | ❌ 未审计 |
| §11.1.2 项目立项/激活/结项/取消 | ⚠️ 结项有走 lifecycle_events，其余无 |
| §11.1.3 三主体变更 | ✅ V2-7 handover |
| §11.1.4 文件上传/领取/提交/移动/删除 | ✅ lifecycle_events |
| §11.1.5 生命周期事件追加 | ✅ 表本身就是 |
| §11.1.6 **权限配置变更**（project_members 改）| ❌ 未审计 |
| §11.1.7 底账关键字段变更 | ⚠️ 部分（过户走 handover 事件）|
| §11.1.8 **导出底账或归档包** | ❌ 未审计 |

### E. 文件存储适配（§7.8 + §1.2 + §16）

| 文档要求 | 状态 |
|---|---|
| StorageAdapter 接口（8 方法）| ❌ 散落实现，无抽象 |
| createProjectDirectory / createStageDirectory | ✅ workspace.go 散落 |
| saveFile / calculateChecksum | ✅ file_operation.go 散落 |
| copyAsInput | ✅ bindPlannedAsReceive 散落 |
| sealArchive | ✅ project_close.go 散落 |
| **moveFile** | ❌ 未实现 |
| **deleteFile**（仅销账流程调用）| ❌ 未实现 |
| MVP 仅本地适配器 | OK，但需抽象出接口 |

### F. AI 辅助模块（§7.9 + §1.1.8）

| 文档要求 | 状态 |
|---|---|
| **4 个 Port 接口预留**（TemplateRecommend / AutoClassify / SummarySuggest / AnomalyDetect）| ❌ 完全未做 |
| MVP 不强制实现模型调用 | OK 用 NoOp |

### G. API 设计（§8）

| 文档建议路径 | scan 实际 | 状态 |
|---|---|---|
| `POST /api/projects/:id/activate` | 无（立项时 `activate:true` 内联）| ❌ 缺独立激活接口 |
| `GET /api/file-versions/:id/chain` | 无 | ⚠️ source_file_version_id 可遍历但无专用端点 |
| `POST /api/ledgers/:id/events`（通用追加）| 无（仅 /transition / /handover）| ⚠️ 通用追加缺 |
| `POST /api/ledgers/export`（CSV/JSON）| 仅 `/export.xlsx` | ⚠️ 缺 CSV / JSON 格式 |

### H. 前端页面（§9）

| 文档清单 | 当前页面 | 状态 |
|---|---|---|
| 模版列表 | manage TemplatesView | ✅ |
| 模版编辑 | manage | ✅ |
| 项目列表 | scan ProjectsListView | ✅ |
| 项目立项向导 | scan ProjectWizardView | ✅ V2-3 加强 |
| 项目工作台（§9.2 4 列布局：左环节、中文件三态、右底账+来源+权限+策略+事件）| scan ProjectWorkbenchView | ⚠️ 已有但布局未严格符合 §9.2 |
| 底账台账 | scan LedgerView | ✅ V2-7 加强 |
| **生命周期事件**（专属页面）| 当前散在底账详情里 | ⚠️ 无专属页 |
| 结项工作台 | 当前嵌在项目页 | ⚠️ 无专属页 |

### I. 内置示例数据（§13）

| 文档 | 状态 |
|---|---|
| TPL-PRINT-BOOK V2.1 | ✅ manage seed |
| **10 个工作环节**（MZ-SG 至 MZ-HJ）| ✅ manage seed 完整 |
| 文件规则示例（§13.3）| ✅ |

---

## 二、V3 子任务（10 个）

每个子任务必须**带文档章节引用 + 测试用例 + 不偏离自由发挥**。完成后才能进下一个。

| # | 子任务 | 文档章节 | 估算 |
|---|---|---|---|
| V3-1 | 模版复制 + 导入导出 | §7.1 | 4h |
| V3-2 | 模版版本升级流程（"基于此版本新建") | §7.1 + §17.3 | 2h |
| V3-3 | 项目环节状态机（pending → running / completed / skipped）| §7.3 + §5.2 | 3h |
| V3-4 | 权限常量补 upload + destroy | §6.x + §7.7 | 1h |
| V3-5 | audit_logs 表 + 中间件 + §11.1 八类操作接入 | §11 | 6h |
| V3-6 | StorageAdapter 接口抽象 + moveFile / deleteFile（仅销账调用）| §7.8 + §17.6 | 4h |
| V3-7 | AI 辅助 4 个 Port 预留（NoOp 实现）| §7.9 + 设计 §5.9 | 2h |
| V3-8 | API 补全：activate / fv chain / events 通用追加 / ledgers export CSV+JSON | §8.2 §8.3 §8.4 | 3h |
| V3-9 | 项目工作台 4 列布局 + 生命周期事件专属页 + 结项工作台专属页 | §9.1 §9.2 | 5h |
| V3-10 | 集成测试 §15.2 全闭环回归 + §15.3 验收 7 条断言 | §15 | 3h |

**总估算 ~33h**，按 5 个工作日推。

---

## 三、V3 后面是什么

| 阶段 | 范围 | 触发条件 |
|---|---|---|
| **V4** | §18 七条待确认问题决策后展开 | 用户给口径 |
| **V5** | §12 五项"暂不实现"按需选做（涉密对接 / 文件嵌入式打标 / 复杂 AI / 跨域监管 / 第三方审批集成）| 业务需要 |
| **V6** | §1.1.8 / §2 目录上报模块（§18.6 schema 定后才能做）| §18.6 答复 |
| **V7** | 持续性能优化、扩展存储适配器（依赖 §18.3 决策）| 性能问题 / 部署形态 |

V3 完成后系统在**功能维度**对齐文档 MVP 全集；V4-V6 是后续演进；V7 是工程优化。

---

## 四、执行顺序原则

1. **不可跳号**：V3-1 完成后才进 V3-2，每个都跑通测试再继续
2. **每子任务独立可回滚**：每个子任务一个 commit
3. **测试先行**：先写测试断言文档要求，再写实现
4. **不引申**：文档没写的不做（不发明）

---

立即开干 V3-1。
