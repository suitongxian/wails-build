# 个人电子文件数字助理定位

## 背景

`SYS-PERSONAL-CORE`、`SYS-PERSONAL-IMPORTANT`、`SYS-PERSONAL-GENERAL` 最初以“个人文件项目”方式落库，是为了复用项目、环节、文件版本和底账能力。但它们不应被理解为用户日常工作的三个真实项目，也不应驱动本地 workspace 目录创建。

当前定位调整为：

```text
三个 SYS-PERSONAL-* 记录 = 个人电子文件三层级归目容器
```

它们承载个人文件的级别标识、文件版本编码、底账和生命周期事件，不直接承载实体文件迁移。

## 产品原则

个人电子文件侧的目标是“有账、懂规矩、手脚麻利”：

- 有账：扫描认领后的工作文件形成 `file_versions` 和 `asset_ledgers`。
- 懂规矩：区分核心、重要、一般，以及过程版本、标记定稿。
- 手脚麻利：默认只做归目挂账和来源记录，不要求用户搬文件、改文件名、建目录。

## 目录与实体文件边界

个人文件认领后默认只做：

```text
扫描文件
-> 识别为个人工作文件
-> 建议归目级别和过程/定稿
-> 人工确认
-> 写入个人归目容器的 file_version + ledger
-> ledger.source_ref 记录 data_resources 来源
```

默认不做：

```text
不在 workspace 下创建个人核心/重要/一般目录
不把过程版本复制到系统目录
不把 source_storage_uri 当成已接管文件
不把个人归目记录同步到 manage 部门柜
```

如果后续要对“最终版本”执行实体归档，应作为独立策略动作设计，例如 `archive_action = copy_later/copied`，并在界面中明确提示用户。不要在归目确认时隐式复制。

## 与业务项目的区别

个人归目容器：

```text
个人工作文件历史治理 / 数字助理
只归目、标识、挂账
实体文件保留原位
不进入 manage 部门柜/单位室
```

业务数据项目：

```text
正式立项卷宗
按模版实例化工作环节
output 提交后可同步 manage 部门柜登记
项目结项后可同步 manage 单位文件室/机要室登记
```

## 当前实现约束

- `ensurePersonalFilesContext` 创建三个 `SYS-PERSONAL-*` 归目容器，`project_root` 保持为空。
- `BridgeClassifyToPersonalProject*` 只创建 `file_versions`、`asset_ledgers` 和 `lifecycle_events`。
- 个人桥接不写 `file_versions.storage_uri`。
- 来源记录写入 `asset_ledgers.source_ref`，指向 `data_resources`。
- `ManagedArchiveReporter` 对 `SYS-PERSONAL-*` 保持 skipped，不上报 manage。
- `content_summary` 可写入 `工作事项/主题：xxx`，用于“我的文件归目”页面按真实工作事项聚合。
- 是否进入 manage 由 `archive_policy` 统一判断；个人容器恒为 `no_sync`。
