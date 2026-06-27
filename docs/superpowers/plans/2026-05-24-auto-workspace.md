# 登录后自动创建工作空间目录 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用户登录成功后，若工作空间未配置则自动设为 `<OS home>/<scan登录名>/workspace` 并创建目录，前端顶栏加可见的当前工作空间指示器。

**Architecture:** 后端拆两层——`repository.ComputeDefaultWorkspace/ValidateUsername` 纯函数（无 IO，易测）+ `httpd.ensureDefaultWorkspaceForUser`（IO 层，封装 mkdir 与 SetWorkspace），在 `forwardAuth` 末尾调用，失败仅 log 不阻塞登录。前端在 App.vue 顶栏复用现有 `config.workspace` reactive 数据加一个 chip；SystemConfigView 输入框补 hint。

**Tech Stack:** Go 1.x + sqlx + Gin + better-sqlite3（测试前需 `npm rebuild better-sqlite3`）；Vue 3 + Vuetify 4

**关联 spec:** `docs/superpowers/specs/2026-05-24-auto-workspace-design.md`

---

## 文件结构

| 路径 | 类型 | 职责 |
|---|---|---|
| `internal/repository/workspace_default.go` | 新 | 纯函数 `ValidateUsername`、`ComputeDefaultWorkspace`——无 IO，易单测 |
| `internal/repository/workspace_default_test.go` | 新 | helper 单测：4 个用例覆盖正常 + 3 种危险字符 |
| `internal/httpd/auth_workspace.go` | 新 | `ensureDefaultWorkspaceForUser`——封装 mkdir + SetWorkspace；调用 repository helper |
| `internal/httpd/auth_workspace_test.go` | 新 | 单测：empty/customized/mkdir 失败/invalid username 4 个场景 |
| `internal/httpd/auth.go` | 改 | 在 `forwardAuth` 中 `mirrorAuthUser` 之后调用 `ensureDefaultWorkspaceForUser`，error 仅 log |
| `internal/httpd/auth_login_workspace_integration_test.go` | 新 | HTTP 端到端：POST /auth/login → KeyWorkspace 自动写入 + 目录存在；二次登录不覆盖；mkdir 失败仍登录成功 |
| `frontend_real/views/SystemConfigView.vue` | 改 | workspace 输入框加 hint 文案 |
| `frontend_real/App.vue` | 改 | 顶栏在 user-info chip 左侧新增 workspace chip |

---

## Task 1: repository helper（纯函数）

**Files:**
- Create: `internal/repository/workspace_default.go`
- Test: `internal/repository/workspace_default_test.go`

- [ ] **Step 1: 写失败测试**

文件 `internal/repository/workspace_default_test.go`：

```go
package repository

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeDefaultWorkspace_HappyPath(t *testing.T) {
	got, err := ComputeDefaultWorkspace("/Users/admin", "zhang")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("/Users/admin", "zhang", "workspace")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeDefaultWorkspace_RejectsSlash(t *testing.T) {
	_, err := ComputeDefaultWorkspace("/Users/admin", "a/b")
	if err == nil {
		t.Fatal("expected error for username containing slash")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Errorf("error should mention username, got: %v", err)
	}
}

func TestComputeDefaultWorkspace_RejectsBackslash(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("/Users/admin", "a\\b"); err == nil {
		t.Fatal("expected error for username containing backslash")
	}
}

func TestComputeDefaultWorkspace_RejectsDotDot(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("/Users/admin", ".."); err == nil {
		t.Fatal("expected error for username '..'")
	}
	if _, err := ComputeDefaultWorkspace("/Users/admin", "..foo"); err == nil {
		t.Fatal("expected error for username starting with '..'")
	}
}

func TestComputeDefaultWorkspace_RejectsNull(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("/Users/admin", "a\x00b"); err == nil {
		t.Fatal("expected error for username containing null byte")
	}
}

func TestComputeDefaultWorkspace_RejectsEmpty(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("/Users/admin", ""); err == nil {
		t.Fatal("expected error for empty username")
	}
	if _, err := ComputeDefaultWorkspace("/Users/admin", "   "); err == nil {
		t.Fatal("expected error for whitespace-only username")
	}
}

func TestComputeDefaultWorkspace_RejectsEmptyHomeDir(t *testing.T) {
	if _, err := ComputeDefaultWorkspace("", "zhang"); err == nil {
		t.Fatal("expected error for empty home dir")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /root/data/projects/data-asset-scan
go test ./internal/repository/ -run TestComputeDefaultWorkspace -v
```

Expected: FAIL with "undefined: ComputeDefaultWorkspace"

- [ ] **Step 3: 写实现**

文件 `internal/repository/workspace_default.go`：

```go
package repository

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateUsername 拒绝可能导致路径遍历或破坏文件系统的字符。
// 理论上从 manage 同步来的 Username 不会有这些，仅作兜底防御。
func ValidateUsername(username string) error {
	u := strings.TrimSpace(username)
	if u == "" {
		return fmt.Errorf("username is empty")
	}
	if strings.ContainsAny(u, "/\\\x00") {
		return fmt.Errorf("username %q contains forbidden character (/ \\ \\0)", username)
	}
	if u == "." || u == ".." || strings.HasPrefix(u, "..") {
		return fmt.Errorf("username %q is reserved or starts with '..'", username)
	}
	return nil
}

// ComputeDefaultWorkspace 返回 <homeDir>/<username>/workspace 形式的约定路径。
// 不做 IO，仅做拼接 + 安全校验，方便单测。
func ComputeDefaultWorkspace(homeDir, username string) (string, error) {
	if strings.TrimSpace(homeDir) == "" {
		return "", fmt.Errorf("home dir is empty")
	}
	if err := ValidateUsername(username); err != nil {
		return "", err
	}
	return filepath.Join(homeDir, strings.TrimSpace(username), "workspace"), nil
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /root/data/projects/data-asset-scan
go test ./internal/repository/ -run TestComputeDefaultWorkspace -v
```

Expected: PASS（7 个 case 全过）

- [ ] **Step 5: Commit**

```bash
cd /root/data/projects/data-asset-scan
git add internal/repository/workspace_default.go internal/repository/workspace_default_test.go
git commit -m "$(cat <<'EOF'
feat(scan): 加 ComputeDefaultWorkspace / ValidateUsername helper

纯函数实现，拒绝 / \ .. \0 等危险字符。用于登录后自动创建工作空间目录的路径计算。

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: httpd 层 ensureDefaultWorkspaceForUser（IO 封装）

**Files:**
- Create: `internal/httpd/auth_workspace.go`
- Test: `internal/httpd/auth_workspace_test.go`

- [ ] **Step 1: 写失败测试**

文件 `internal/httpd/auth_workspace_test.go`：

```go
package httpd

import (
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func TestEnsureDefaultWorkspace_EmptyConfig_CreatesAndSets(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	// 用临时目录冒充 HOME，避免污染真实环境
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := ensureDefaultWorkspaceForUser(cfg, "zhang"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join(tmpHome, "zhang", "workspace")
	if got := cfg.GetWorkspace(); got != want {
		t.Errorf("KeyWorkspace = %q, want %q", got, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("workspace dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("workspace path exists but is not a directory")
	}
}

func TestEnsureDefaultWorkspace_PreservesUserCustom(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	cfg.SetWorkspace("/data/custom")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := ensureDefaultWorkspaceForUser(cfg, "zhang"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.GetWorkspace(); got != "/data/custom" {
		t.Errorf("KeyWorkspace should not be overwritten, got %q", got)
	}
	// 也不该创建约定目录
	conv := filepath.Join(tmpHome, "zhang", "workspace")
	if _, err := os.Stat(conv); !os.IsNotExist(err) {
		t.Errorf("convention dir should not exist, but stat err=%v", err)
	}
}

func TestEnsureDefaultWorkspace_MkdirFailureReturnsError(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	// 用一个文件作为 HOME，使后续 MkdirAll 失败
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpFile)

	err := ensureDefaultWorkspaceForUser(cfg, "zhang")
	if err == nil {
		t.Fatal("expected mkdir error")
	}
	if got := cfg.GetWorkspace(); got != "" {
		t.Errorf("KeyWorkspace should NOT be written on mkdir failure, got %q", got)
	}
}

func TestEnsureDefaultWorkspace_InvalidUsernameReturnsError(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := ensureDefaultWorkspaceForUser(cfg, ".."); err == nil {
		t.Fatal("expected error for invalid username")
	}
	if got := cfg.GetWorkspace(); got != "" {
		t.Errorf("KeyWorkspace should NOT be written, got %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /root/data/projects/data-asset-scan
npm rebuild better-sqlite3   # CLAUDE.md 铁律：测前必须
go test ./internal/httpd/ -run TestEnsureDefaultWorkspace -v
```

Expected: FAIL with "undefined: ensureDefaultWorkspaceForUser"

- [ ] **Step 3: 写实现**

文件 `internal/httpd/auth_workspace.go`：

```go
package httpd

import (
	"fmt"
	"os"

	"data-asset-scan-go/internal/repository"
)

// ensureDefaultWorkspaceForUser 在 KeyWorkspace 为空时按约定路径创建目录并写库。
// 已有配置则不动（保留用户自定义）。错误一律返回给调用方，由调用方决定是否阻塞登录
// （设计上：登录流程拿到 error 只 log，不让 login 失败）。
func ensureDefaultWorkspaceForUser(cfg *repository.SystemConfigRepository, username string) error {
	if existing := cfg.GetWorkspace(); existing != "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	path, err := repository.ComputeDefaultWorkspace(home, username)
	if err != nil {
		return fmt.Errorf("compute default workspace: %w", err)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("mkdir workspace %s: %w", path, err)
	}
	cfg.SetWorkspace(path)
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /root/data/projects/data-asset-scan
go test ./internal/httpd/ -run TestEnsureDefaultWorkspace -v
```

Expected: PASS（4 个 case 全过）

- [ ] **Step 5: Commit**

```bash
cd /root/data/projects/data-asset-scan
git add internal/httpd/auth_workspace.go internal/httpd/auth_workspace_test.go
git commit -m "$(cat <<'EOF'
feat(scan): 加 ensureDefaultWorkspaceForUser 封装

KeyWorkspace 为空时按 <home>/<username>/workspace 创建目录并写库；
已有配置不动；任何错误返回给调用方决定是否阻塞。

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: 在 forwardAuth 接入 + HTTP 集成测试

**Files:**
- Modify: `internal/httpd/auth.go`（在 `forwardAuth` 内 `mirrorAuthUser` 之后调用）
- Create: `internal/httpd/auth_login_workspace_integration_test.go`

- [ ] **Step 1: 写失败的集成测试**

文件 `internal/httpd/auth_login_workspace_integration_test.go`：

```go
package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 辅助：起一个 mock manage 服务，返回固定 session
func newMockManageForLogin(t *testing.T, username string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "登录成功",
			"data": map[string]interface{}{
				"token": "manage-token-" + username,
				"user": map[string]interface{}{
					"id":              42,
					"username":        username,
					"display_name":    username + "_display",
					"user_unit":       "第一研究院",
					"user_department": "档案处",
					"role":            "user",
					"status":          "active",
				},
			},
		})
	}))
}

func TestHTTP_AuthLogin_AutoCreatesWorkspaceWhenEmpty(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	successOk(t, status, resp)

	want := filepath.Join(tmpHome, "zhang", "workspace")
	if got := cfg.GetWorkspace(); got != want {
		t.Errorf("KeyWorkspace = %q, want %q", got, want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Errorf("workspace dir should exist, stat err=%v", err)
	}
}

func TestHTTP_AuthLogin_DoesNotOverwriteCustomWorkspace(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetWorkspace("/data/custom-keep")

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	successOk(t, status, resp)

	if got := cfg.GetWorkspace(); got != "/data/custom-keep" {
		t.Errorf("KeyWorkspace should be preserved, got %q", got)
	}
}

func TestHTTP_AuthLogin_StillSucceedsWhenWorkspaceMkdirFails(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	cfg := repository.NewSystemConfigRepository(db)

	manage := newMockManageForLogin(t, "zhang")
	defer manage.Close()
	cfg.SetValue(repository.KeyManageEndpoint, manage.URL)

	// 让 HOME 指向一个文件，触发 MkdirAll 失败
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpFile)

	status, resp := jsonReq(t, r, "POST", "/auth/login", map[string]interface{}{
		"username": "zhang",
		"password": "secret",
	})
	// 登录必须成功——自动化便利不能阻塞登录
	successOk(t, status, resp)

	// KeyWorkspace 应该仍为空（mkdir 失败时不写库）
	if got := cfg.GetWorkspace(); got != "" {
		t.Errorf("KeyWorkspace should remain empty after mkdir failure, got %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /root/data/projects/data-asset-scan
go test ./internal/httpd/ -run TestHTTP_AuthLogin_AutoCreates -v
```

Expected: FAIL（KeyWorkspace 仍为空，因为 forwardAuth 还没接入 hook）

- [ ] **Step 3: 在 forwardAuth 接入**

修改 `internal/httpd/auth.go` 第 114-142 行的 `forwardAuth` 函数，在 `mirrorAuthUser(session.User)` 之后、`currentAuthSession.Lock()` 之前插入调用。

找到这段（auth.go 约 124-136 行）：

```go
	session, err := callManageAuth(endpoint, path, req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := mirrorAuthUser(session.User); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "Failed to mirror authenticated user"})
		return
	}

	currentAuthSession.Lock()
	currentAuthSession.session = session
	currentAuthSession.Unlock()
```

改成：

```go
	session, err := callManageAuth(endpoint, path, req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := mirrorAuthUser(session.User); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "Failed to mirror authenticated user"})
		return
	}

	// 自动设置 / 创建工作空间目录。失败不阻塞登录，仅 log。
	if db := repository.GetDB(); db != nil {
		cfg := repository.NewSystemConfigRepository(db)
		if err := ensureDefaultWorkspaceForUser(cfg, session.User.Username); err != nil {
			log.Printf("[auth] ensureDefaultWorkspaceForUser(%s) failed: %v", session.User.Username, err)
		}
	}

	currentAuthSession.Lock()
	currentAuthSession.session = session
	currentAuthSession.Unlock()
```

如果 `log` 包还没 import 到 auth.go，在 import 块加上 `"log"`：

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"log"  // <- 新增（若已有则跳过）
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"data-asset-scan-go/internal/repository"
)
```

（看现有 import 决定是否新增 `log`）

- [ ] **Step 4: 跑测试确认通过 + 全量回归确认未破坏现有 auth 测试**

```bash
cd /root/data/projects/data-asset-scan
go test ./internal/httpd/ -run "TestHTTP_AuthLogin" -v
```

Expected: 三个新 case PASS + 现有 `TestHTTP_AuthLoginMirrorsManageUser` 等 PASS（注意现有测试也走 HOME，需确认它们不依赖 KeyWorkspace 为空——若失败需检查现有断言）

```bash
go test ./... 2>&1 | tail -20
```

Expected: 全包 ok

- [ ] **Step 5: Commit**

```bash
cd /root/data/projects/data-asset-scan
git add internal/httpd/auth.go internal/httpd/auth_login_workspace_integration_test.go
git commit -m "$(cat <<'EOF'
feat(scan): 登录成功后自动设置 + 创建工作空间目录

forwardAuth 在 mirrorAuthUser 之后调用 ensureDefaultWorkspaceForUser，
KeyWorkspace 为空时按 <HOME>/<scan登录名>/workspace 创建并写库；
已有配置不动；mkdir 失败仅 log 不阻塞登录。

集成测试覆盖：自动创建快乐路径、已有自定义不覆盖、mkdir 失败仍登录成功。

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: 前端 SystemConfigView 文案提示

**Files:**
- Modify: `frontend_real/views/SystemConfigView.vue`

- [ ] **Step 1: 找到 workspace 输入框**

`frontend_real/views/SystemConfigView.vue` 里搜「工作空间目录」，应找到形如：

```vue
<v-text-field
  v-model="workspace"
  label="工作空间目录"
  placeholder="/Users/xxx/workspace"
  variant="outlined"
  hint="需要重点监控的工作目录路径"
  persistent-hint
/>
```

- [ ] **Step 2: 修改 hint 文案**

把 `hint` 改成：

```vue
hint="登录后会自动设为 ~/<用户名>/workspace；可改成别的，下次登录不会被覆盖"
```

- [ ] **Step 3: 验证页面没破**

```bash
cd /root/data/projects/data-asset-scan
yarn test --run 2>&1 | tail -8
```

Expected: 15 文件 / 64 passed / 1 skipped（基线一致）

- [ ] **Step 4: Commit**

```bash
git add frontend_real/views/SystemConfigView.vue
git commit -m "$(cat <<'EOF'
feat(scan): workspace 输入框补「登录自动设置」hint

提示用户：登录后会自动设为 ~/<用户名>/workspace；可改成别的，
下次登录不会被覆盖（呼应后端 ensureDefaultWorkspaceForUser 的 preserve 行为）。

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: 前端 App.vue 顶栏 workspace chip

**Files:**
- Modify: `frontend_real/App.vue`

- [ ] **Step 1: 在 v-app-bar 内 user-info chip 左侧插入 workspace chip**

`frontend_real/App.vue` 第 269-294 行的 `<v-app-bar>` 块，找到现有 user-info chip：

```vue
<v-app-bar  v-if="!isShelllessPage" density="compact" flat color="transparent">
  <v-spacer />
  <v-chip
    v-if="currentUserInfo"
    variant="text"
    class="mr-2"
    @click="openUserInfoDialog"
    style="cursor: pointer;"
  >
    <v-icon start size="small">mdi-account</v-icon>
    <span class="text-body-2">
      {{ currentUserInfo.user_name }} | {{ currentUserInfo.department }} | {{ currentUserInfo.company_name }}
    </span>
    <v-icon end size="small">mdi-pencil</v-icon>
  </v-chip>
  ...
```

在 `<v-spacer />` 之后、user-info chip 之前插入 workspace chip：

```vue
<v-app-bar  v-if="!isShelllessPage" density="compact" flat color="transparent">
  <v-spacer />
  <v-chip
    v-if="config?.workspace"
    variant="text"
    class="mr-2"
    :title="config.workspace"
    @click="router.push('/settings')"
    style="cursor: pointer; max-width: 320px;"
  >
    <v-icon start size="small">mdi-folder-outline</v-icon>
    <span class="text-body-2 text-truncate">工作空间: {{ config.workspace }}</span>
  </v-chip>
  <v-chip
    v-if="currentUserInfo"
    variant="text"
    class="mr-2"
    @click="openUserInfoDialog"
    style="cursor: pointer;"
  >
    ...保持不变
  </v-chip>
  ...保持不变
```

- [ ] **Step 2: 确保 config 在登录后被刷新**

`config` ref 已经存在（第 13 行 `const config = ref<SystemConfig | null>(null)`）+ `loadConfig` 已经在 `onMounted` 调用（第 119 行），同时 `watch(isShelllessPage)` 在切回 shell 页面时刷新（第 124-129 行）。**无需改动加载逻辑**——登录成功后页面会从 `/login` 跳回 `/`，触发 `isShelllessPage` 从 true→false，自动 reload config。

- [ ] **Step 3: 验证前端测试未破**

```bash
cd /root/data/projects/data-asset-scan
yarn test --run 2>&1 | tail -8
```

Expected: 15 文件 / 64 passed / 1 skipped（基线一致）

- [ ] **Step 4: Commit**

```bash
git add frontend_real/App.vue
git commit -m "$(cat <<'EOF'
feat(scan): 顶栏新增工作空间路径指示器

在 user-info chip 左侧加一个 chip，显示当前 workspace 路径，
点击跳 /settings。复用现有 config reactive，无需新增 API 调用。
路径过长用 text-truncate + title tooltip。

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: 全量回归 + 推送

**Files:** 无（仅命令）

- [ ] **Step 1: Go 全量回归**

```bash
cd /root/data/projects/data-asset-scan
go test ./... 2>&1 | tail -15
```

Expected: 全包 ok，无 FAIL

- [ ] **Step 2: 前端测试**

```bash
yarn test --run 2>&1 | tail -8
```

Expected: 15/64/1 基线一致

- [ ] **Step 3: 推送**

```bash
git push origin go-test-template 2>&1 | tail -5
```

Expected: 输出 `<old-sha>..<new-sha>  go-test-template -> go-test-template`

---

## 验收对照（来自 spec）

1. ✓ 全新数据库 + 首次登录 → 工作空间自动设为 `<HOME>/<username>/workspace`，目录已创建 → Task 3 `TestHTTP_AuthLogin_AutoCreatesWorkspaceWhenEmpty` 覆盖
2. ✓ 自定义后登出再登录仍是自定义值 → Task 3 `TestHTTP_AuthLogin_DoesNotOverwriteCustomWorkspace` 覆盖
3. ✓ mkdir 失败不阻塞登录 → Task 3 `TestHTTP_AuthLogin_StillSucceedsWhenWorkspaceMkdirFails` 覆盖
4. ✓ 右上角看得到当前工作空间路径，点击能跳到设置页 → Task 5 实现（手测确认）
5. ✓ 现有测试不被影响 → Task 3 Step 4 全量回归 + Task 6 双层兜底

## 非目标

- 不做老数据迁移
- 不加"重置到默认"按钮
- 不动 `data_projects.project_root` 表字段
- 不改其他配置项

## 风险与注意

- **Username 含中文**：当前 `ValidateUsername` 没禁中文。中文做目录名在 Linux/Mac 一般 OK，但部分场景（备份/迁移）会出问题。如果 scan 用户 Username 实际上是拼音/英文 ID（看 mock manage 的 "liulaoshi" 这种格式），就没事。如果发现 Username 字段真存中文，要回头补一条 ASCII-only 校验。
- **测试环境的 HOME**：用 `t.Setenv("HOME", tmpDir)` 在 Linux/Mac 都能影响 `os.UserHomeDir()`。如果在 Windows 跑测试需要额外处理 `USERPROFILE`（项目不支持 Windows，可忽略）。
- **现有 auth_test.go 的影响**：现有 `TestHTTP_AuthLoginMirrorsManageUser` 不设 HOME，会使用真实 HOME，且现在它会在真实 HOME 下创建 `<real-home>/liulaoshi/workspace`。两种处理选择：
  - 不动现有测试（让它"漏"创建一个真实目录，每次跑测试会留垃圾）
  - 给所有走 `forwardAuth` 的现有测试加 `t.Setenv("HOME", t.TempDir())`
  - 第二种更干净。Task 3 Step 4 跑完看到现有 auth 测试通过即可，但留 follow-up TODO 给 PR review 决定要不要批量加 t.Setenv。
