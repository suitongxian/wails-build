# V5 Phase 1 用户视角验证手册

> 4 项功能：A1 双环节 / A2 AI 归目驳回调整 / A3 解绑重归类 / A4 家族归档分流

---

## 准备工作

### 0.1 代码状态

两边都拉最新：
```bash
cd /path/to/data-asset-manage && git checkout test-template && git pull
cd /path/to/data-asset-scan   && git checkout go-test-template && git pull
```

### 0.2 启动 manage

```bash
cd /path/to/data-asset-manage
npm rebuild better-sqlite3
yarn dev
```

启动后**验证 V2.0 模版到位**：
```bash
node -e "const D=require('better-sqlite3'); const db=new D('.runtime/data.db',{readonly:true}); console.log(db.prepare(\"SELECT template_code, template_version, status FROM data_templates WHERE template_code='TPL-PERSONAL-FILES'\").all())"
```
预期：V1.0 deprecated + V2.0 active 两行。

### 0.3 启动 scan

按你日常方式启动 Wails 客户端。

### 0.4 关键步骤：scan 同步模版 + 重启

1. 进 **"数据业务项目 → 新建项目 → 重新同步"** → 拉到 TPL-PRINT-BOOK V2.1 + TPL-PERSONAL-FILES V2.0
2. **关掉 scan 再开一次**（`ensurePersonalFilesContext` 在启动时跑一次，必须重启才会重建 3 个项目）

### 0.5 状态检查清单

进 **"数据业务项目"** 列表，确认：
- ✅ 看到 3 个 `SYS-PERSONAL-*` 项目（核心 / 重要 / 一般）
- ✅ 进任一项目工作台，看到 **2 个环节** GR-DRAFT + GR-FINAL

如果只看到 1 个环节 GR-DA，说明你 manage 同步的是旧 V1.0——回到 0.2 检查。

---

## A1 — 个人文件 2 工作环节

**所属需求**：§4.2-6 "只包括个人文件起草与修改、个人文件定稿两个工作环节和对应的版本文件"

### 验证 A1-1：项目结构

1. 进 `SYS-PERSONAL-CORE 个人核心级文件管理` 工作台
2. ✅ 左侧环节列表显示 **2 个环节**：
   - `GR-DRAFT 个人文件起草与修改`
   - `GR-FINAL 个人文件定稿`
3. ✅ GR-DRAFT 下规则：`PRC-001 过程版本`（data_state=process）
4. ✅ GR-FINAL 下规则：`OUT-001 归档定稿`（data_state=output）

### 验证 A1-2：桥接默认归 GR-FINAL

1. 进 **"管控文件扫描盘点"** → 扫几个文件
2. 进 **"扫描结果责任认领"** → 认领若干
3. 进 **"认领文件归档保护"** → 给某个文件分类为"重要"等级
4. 进 `SYS-PERSONAL-IMPORTANT` 工作台
5. ✅ 该 fv 应出现在 **GR-FINAL 环节**（不是 GR-DRAFT）
6. ✅ data_state = output

---

## A2 — AI 归目"驳回 + 调整"

**所属需求**：§4.3-2 "支持人工确认、调整、驳回和批量处理"

**前置**：确保有未归档的扫描资源（importance_level 未设）。

### 验证 A2-1：驳回

1. 进 **"AI 归目工具"**（左侧 `mdi-auto-fix` 图标，路由 `/ai-classify`）
2. 任一待归目条目展开 → 点 **"驳回"**
3. ✅ 弹出对话框，要求填写原因
4. ✅ 原因留空时"确认驳回"按钮 disabled
5. 填原因 → 确认
6. ✅ snackbar 显示"已驳回"
7. ✅ 资源从列表消失
8. 点"刷新" → ✅ 驳回的资源**不再出现**（pending 自动过滤）

### 验证 A2-2：调整（手动改目标）

1. 另一条目 → 点 **"调整"**
2. ✅ 弹出对话框，3 级下拉：项目 / 环节 / 文件规则
3. ✅ 选项目后，环节下拉自动加载
4. ✅ 选环节后，文件规则下拉自动过滤
5. ✅ 三项都选齐"应用"按钮才 enabled
6. 选 `SYS-PERSONAL-CORE` → `GR-FINAL` → `OUT-001` → 应用
7. ✅ snackbar 显示"调整归目成功"
8. 进 `SYS-PERSONAL-CORE` 工作台 → GR-FINAL → ✅ 看到刚归的 fv

---

## A3 — 解绑 + 重新归类 + 原因登记

**所属需求**：§4.3-4 "手动关联、解除绑定、重新归类和原因登记"

**前置**：至少有一个已挂账的 fv。

### 验证 A3-1：底账解绑

1. 进 **"资产标识底账"**（左侧 `mdi-book-open-variant` 图标）
2. 列表中点任一条 `lifecycle=registered` 的底账，看右侧详情
3. ✅ 详情面板 **"归目调整"** 段（在三主体过户上方）应有 **"解绑"** + **"重新归类"** 按钮
4. ✅ 只在 lifecycle ∈ {registered, in_use, sealed} 时显示
5. 点 **"解绑"** → 填原因 → 确认
6. ✅ snackbar 显示"已解绑"
7. ✅ 该底账 lifecycle 变 `cancelled`

### 验证 A3-2：重新归类

1. 选另一条 registered 底账 → 点 **"重新归类"**
2. ✅ 对话框含 3 级下拉 + 原因 textarea
3. ✅ 所有项都填齐才能提交
4. 选新目标 + 填原因 → 提交
5. ✅ snackbar 显示"已重新归类"
6. ✅ 原 fv lifecycle 变 cancelled
7. ✅ 新位置出现一条新 fv（lifecycle=registered）
8. 进新 fv 详情看 lifecycle_events → ✅ 应有 reclassify 事件

### 验证 A3-3：边界（可选）

- 对 `lifecycle=cancelled` 的 fv 再点解绑 → ✅ 应失败"fv 已解除绑定"

---

## A4 — 家族归档过程/定稿分流

**所属需求**：§4.3-6 "过程文件和最新文件分别归入过程版本文件和定稿文件关联"

**前置**：有一个文件家族（≥2 成员）。如没有，扫描有副本/重名的目录会自动生成。

### 验证 A4-1：单目标兼容

1. 进 **"认领文件归档保护"** → 找带"家族 #N · M 成员"标识的行
2. 点进去看家族成员对话框 → 点"家族批量归档"
3. ✅ 对话框打开，看到 **"过程目标 *"**（项目/环节/规则）+ **"定稿目标（可选）"** 两段
4. 只填上面 3 个，下面留空
5. ✅ 按钮文字是 **"确认批量归档"**（单目标模式）
6. ✅ 提示框显示"不填即按单目标归档"
7. 提交 → ✅ 所有成员都归到指定环节

### 验证 A4-2：双目标分流（核心功能）

1. 再选一个家族 → 打开归档对话框
2. 填齐全部 5 项：
   - 过程目标：`SYS-PERSONAL-IMPORTANT` / `GR-DRAFT` / `PRC-001`
   - 定稿目标：（同项目）`GR-FINAL` / `OUT-001`
3. ✅ 按钮文字变成 **"确认分流归档"**（split 模式）
4. 提交
5. ✅ 结果显示 archived=N（总数）
6. 进 `SYS-PERSONAL-IMPORTANT` 工作台：
   - ✅ 家族里**最新**的 1 个文件 → GR-FINAL 环节
   - ✅ 其他文件 → GR-DRAFT 环节

---

## 整体回归（可选）

如果以上都过了，跑后端测试套件：

```bash
cd /path/to/data-asset-scan
go test ./... -count=1
```
预期：全包 PASS。

---

## 反馈格式

遇到任何"实际表现 ≠ 预期"，贴：

1. **哪一项**：A1-1 / A2-2 / A3-3 等等
2. **操作步骤**：复现路径
3. **实际结果 vs 预期**
4. UI 问题截图 / API 问题贴 DevTools Network 里失败请求的 response

---

## 后续

完整通过后可以选：
1. 真实环境继续用一段时间（最稳）
2. 进 V5 Phase 4（manage 端模版能力增强 C1-C4）
3. 暂停整体 merge / 发布
