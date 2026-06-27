# 数据业务模版 V1 验证套件

## 文件清单

| 文件 | 类型 | 用途 |
|---|---|---|
| `ui_checklist.md` | 手动测试清单 | 你自己点 UI，每条用例标"操作 → 预期 UI → 预期 DB → SQL 校验" |
| `smoke.sh` | 自动 curl 冒烟 | 起 manage+scan 后跑一遍业务全链路，每步断言响应 |
| `invariants.sql` | SQL 不变量 | 跑完业务后用 sqlite3 校验数据一致性（每条带 EXPECT 注释） |
| `run_invariants.sh` | 自动校验脚本 | 解析 invariants.sql 结果，把 `bad > 0` 的项列出来 |

## 推荐验证顺序

1. **跑 Go 单元测试**（已自带）：
   ```bash
   cd data-asset-scan
   npm rebuild better-sqlite3   # 按 CLAUDE.md 要求
   go test ./...
   ```
   预期：repository 90+ 测试 + httpd 23 测试全过。

2. **冒烟测试**（不依赖 UI，最快验证后端能跑通）：
   ```bash
   verification/v1/smoke.sh
   ```
   预期：所有 step 都 ✓；如失败，按 step 编号定位问题。

3. **数据一致性校验**：
   ```bash
   SCAN_DB=~/.config/data-asset-scan/data.db verification/v1/run_invariants.sh
   ```
   预期：`✓ 全部数据一致性检查通过`。

4. **手动 UI 验证**：
   按 `ui_checklist.md` 逐条勾选，覆盖快乐路径 + 关键错误用例。

## 自动化验证命令汇总

一次性跑全部自动化验证：
```bash
cd data-asset-scan
npm rebuild better-sqlite3
go test ./... 2>&1 | tail -20
verification/v1/smoke.sh
verification/v1/run_invariants.sh
```

## 常见问题

**Q：smoke.sh 在 step 5（同步模版）失败？**  
A：检查 manage 是否启动（`curl http://localhost:3000/api/templates`），TPL-PRINT-BOOK V2.1 是否 active。

**Q：smoke.sh 在 step 6（立项）失败"项目根目录不可写"？**  
A：smoke.sh 默认用 /tmp/scan-smoke-root；如系统不允许，改 `PROJECT_ROOT=/some/writable/path verification/v1/smoke.sh`。

**Q：invariants 报告 LEDGER_FV_STATUS_DRIFT > 0？**  
A：通常是 ledger_lifecycle 服务漏改了 fv 状态。手动查：
```sql
SELECT al.id, al.lifecycle_status AS al_status, fv.lifecycle_status AS fv_status
FROM asset_ledgers al JOIN file_versions fv ON fv.id=al.file_version_id
WHERE al.lifecycle_status != fv.lifecycle_status;
```

**Q：HTTP 测试要怎么单独跑？**  
A：`go test ./internal/httpd/ -v` — 23 个测试，每个 < 50ms。
