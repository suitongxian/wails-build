# 个人归目与正式归档闭环设计

## 一句话定位

scan 是个人电子文件数字助理和正式项目工作端；manage 是单位管理平台和归档登记端。

因此系统必须区分两件事：

- 归目：个人或工作文件被识别、标识、挂账、保留来源线索。
- 归档：正式项目或被确认的最终版本形成归档事件，并向 manage 登记。

## 核心闭环

### 个人文件闭环

```text
扫描发现
-> 用户认领
-> AI/规则建议级别与状态
-> 用户确认核心/重要/一般 + 过程/定稿
-> 写入 SYS-PERSONAL-* 归目容器
-> asset_ledgers.source_ref 保留 data_resources 来源
-> 本地按级别、状态、工作事项/主题查询
```

约束：

- 不默认创建 workspace 目录。
- 不默认复制、移动或重命名实体文件。
- 不默认同步 manage。
- 过程稿和定稿都先是个人底账记录，后续如需单位归档，必须另行形成显式归档动作。

### 正式项目闭环

```text
立项
-> 模版实例化项目环节与文件规则
-> 上传/派生/提交 output
-> 按 archive_policy 判断部门柜/保密室目标
-> 结构化上报 manage
-> 项目结项
-> 生成 manifest
-> 按 archive_policy 判断单位档案室/保密室目标
-> 结构化上报 manage
```

约束：

- manage 接收结构化 JSON，不代表 scan 端已搬迁实体文件。
- `source_storage_uri` 和 manifest 中的路径是来源证据或本地位置引用。
- 是否实体接管由后续独立策略决定，不在“归目确认”或“结构化上报”中隐式完成。

## 关键字段

### sensitivity_level

表示文件或项目的保护等级：

- `general`：一般
- `important`：重要
- `core_secret`：核心 / 涉密 / 核心要件

### file_state

表示文件版本成熟状态：

- `personal_process`：个人过程稿
- `personal_final`：个人定稿
- `dept_stage`：部门环节版本
- `dept_final`：部门项目定稿
- `unit_release`：单位最终发布版本

### 两者组合

`sensitivity_level` 不决定文件是否已归档，`file_state` 也不决定保密等级。归档目标必须看组合。

当前 `archive_policy` 的基线：

| sensitivity_level | file_state | action | target |
| --- | --- | --- | --- |
| 任意个人容器 | 任意 | no_sync | 本地归目 |
| general | dept_final | sync | department_cabinet |
| important | dept_final | sync | department_cabinet |
| core_secret | dept_final | sync | secure_room |
| general | unit_release | sync | unit_archive |
| important | unit_release | sync | unit_archive |
| core_secret | unit_release | sync | secure_room |
| personal_process/personal_final/dept_stage | - | no_sync | 尚未形成归档事件 |

## 视觉模型

“我的文件归目”不是三个真实目录，而是两个视角叠加：

- 级别视角：核心、重要、一般三宫格。
- 事项视角：论文 A、市场调研、会议材料等真实工作主题。

底账中 `content_summary` 可以记录 `工作事项/主题：xxx`，界面按该字段聚合。没有显式主题时，前端会从文件名做弱推断；这只是辅助视图，不改变底账事实。

## 当前落地点

- `archive_policy.go` 是归档目标判断的唯一入口。
- `security_policies` 仍保存安全基线，seed 会幂等 upsert，保证规则修正能落到既有库。
- `ManagedArchiveReporter` 用策略结果生成 file archive payload。
- 项目结项 manifest 写入 `archive_action`、`archive_target`、`archive_file_state` 和 `storage_label`。
- 个人归目桥接只写 `file_versions`、`asset_ledgers`、`lifecycle_events`，不写 `storage_uri`。
