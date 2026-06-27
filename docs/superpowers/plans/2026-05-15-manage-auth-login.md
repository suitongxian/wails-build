# Manage-Centered Login Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make scan register and login through manage, with manage as the authoritative account store and scan keeping only a current-session/local-compatibility mirror.

**Architecture:** Manage gets a small auth module with `auth_users`, password hashing, signed session tokens, and terminal registry updates during register/login. Scan gets auth proxy endpoints that call manage, mirror the returned user into local `user_info/users`, and expose a normalized session to the frontend. The scan UI gets a workbench-style login/register page and a router guard.

**Tech Stack:** Nuxt/Nitro + better-sqlite3 + Vitest in `data-asset-manage`; Go/Gin/sqlx + Vue/Vuetify/Vitest in `data-asset-scan`.

---

### Task 1: Manage Auth Repository And Passwords

**Files:**
- Modify: `/root/data/projects/data-asset-manage/server/database/database.sql`
- Modify: `/root/data/projects/data-asset-manage/server/database/index.ts`
- Modify: `/root/data/projects/data-asset-manage/server/database/types.ts`
- Create: `/root/data/projects/data-asset-manage/server/database/auth-repository.ts`
- Test: `/root/data/projects/data-asset-manage/tests/auth-repository.test.ts`

- [ ] **Step 1: Write failing repository tests**

Create `tests/auth-repository.test.ts` with tests for:

- `createUser` stores an active user and does not expose plaintext password.
- Duplicate username is rejected.
- `verifyUserPassword` accepts the correct password and rejects the wrong one.
- `recordSuccessfulLogin` updates `last_login_time`.

Run: `npm run test -- tests/auth-repository.test.ts`
Expected: FAIL because `auth-repository.ts` does not exist.

- [ ] **Step 2: Add manage database schema and migration**

Add `auth_users` DDL to `server/database/database.sql` and migration guards to `server/database/index.ts` so existing databases gain the table and indexes.

- [ ] **Step 3: Implement repository**

Create `server/database/auth-repository.ts` with:

- `hashPassword(password: string): string`
- `verifyPassword(password: string, encoded: string): boolean`
- `authUserRepository.create(input)`
- `authUserRepository.findByUsername(username)`
- `authUserRepository.findByID(id)`
- `authUserRepository.verifyUserPassword(username, password)`
- `authUserRepository.recordSuccessfulLogin(userID)`

Use Node `crypto.scryptSync`, per-user random salt, and `crypto.timingSafeEqual`.

- [ ] **Step 4: Run repository tests**

Run: `npm run test -- tests/auth-repository.test.ts`
Expected: PASS.

### Task 2: Manage Auth API

**Files:**
- Create: `/root/data/projects/data-asset-manage/server/utils/auth-token.ts`
- Create: `/root/data/projects/data-asset-manage/server/api/auth/register.post.ts`
- Create: `/root/data/projects/data-asset-manage/server/api/auth/login.post.ts`
- Create: `/root/data/projects/data-asset-manage/server/api/auth/me.get.ts`
- Test: `/root/data/projects/data-asset-manage/tests/auth-api.test.ts`

- [ ] **Step 1: Write failing API tests**

Create `tests/auth-api.test.ts` with tests that call the Nitro handlers directly or through existing test utilities:

- Register returns `code: 0`, `token`, and user profile.
- Register inserts/updates `terminal_user_registry`.
- Duplicate register returns an error.
- Login returns a token for correct password and rejects wrong password.
- `me` returns the user for a valid bearer token and rejects missing/invalid token.

Run: `npm run test -- tests/auth-api.test.ts`
Expected: FAIL because auth endpoints do not exist.

- [ ] **Step 2: Implement token utility**

Create `server/utils/auth-token.ts` using Node `crypto` HMAC signing:

- `signAuthToken(user)`
- `verifyAuthToken(token)`
- `authTokenFromEvent(event)`

Use `process.env.AUTH_JWT_SECRET || 'data-asset-manage-dev-secret'` for the first version.

- [ ] **Step 3: Implement register/login/me endpoints**

Implement:

- `POST /api/auth/register`
- `POST /api/auth/login`
- `GET /api/auth/me`

Return the existing manage response style: `{ code: 0, message, data }`.

Registration and login both call existing terminal registry repository logic using IP + MAC when provided. If IP/MAC are missing, do not fail auth; skip terminal registration.

- [ ] **Step 4: Run manage auth API tests**

Run: `npm run test -- tests/auth-api.test.ts`
Expected: PASS.

### Task 3: Scan Auth Backend Proxy And Local Mirror

**Files:**
- Create: `/root/data/projects/data-asset-scan/internal/httpd/auth.go`
- Modify: `/root/data/projects/data-asset-scan/internal/httpd/router.go`
- Modify: `/root/data/projects/data-asset-scan/internal/repository/user_info.go`
- Test: `/root/data/projects/data-asset-scan/internal/httpd/auth_test.go`

- [ ] **Step 1: Write failing Go HTTP tests**

Create `internal/httpd/auth_test.go` with tests for:

- `/auth/login` forwards credentials to a fake manage server and mirrors the returned profile into `user_info`.
- `/auth/register` forwards registration data and mirrors the returned profile.
- `/auth/session` returns authenticated user after login/register.
- `/auth/logout` clears session.

Run: `GOCACHE=/tmp/go-build-cache go test ./internal/httpd -run 'TestHTTP_Auth'`
Expected: FAIL because routes do not exist.

- [ ] **Step 2: Implement scan auth route registration**

Add `authGroup := r.Group("/auth")` and `RegisterAuthRoutes(authGroup)` in `internal/httpd/router.go`.

- [ ] **Step 3: Implement scan auth proxy**

Create `internal/httpd/auth.go` with:

- request/response structs for manage auth.
- in-process session state.
- `Login`, `Register`, `GetAuthSession`, `Logout`.
- manage URL resolution from request body, then `system_config.manage_endpoint`, then `http://127.0.0.1:3002`.
- terminal metadata using existing local IP/MAC helpers or safe best-effort values.
- local mirror into `user_info` and `users`.

- [ ] **Step 4: Run scan auth backend tests**

Run: `GOCACHE=/tmp/go-build-cache go test ./internal/httpd -run 'TestHTTP_Auth'`
Expected: PASS.

### Task 4: Scan Frontend Auth Service And Route Guard

**Files:**
- Modify: `/root/data/projects/data-asset-scan/frontend_real/services/api.ts`
- Create: `/root/data/projects/data-asset-scan/frontend_real/services/AuthManager.ts`
- Modify: `/root/data/projects/data-asset-scan/frontend_real/services/UserInfoManager.ts`
- Modify: `/root/data/projects/data-asset-scan/frontend_real/plugins/router.ts`
- Test: `/root/data/projects/data-asset-scan/frontend_real/services/__tests__/AuthManager.test.ts`
- Test: `/root/data/projects/data-asset-scan/frontend_real/plugins/__tests__/router-auth.test.ts`

- [ ] **Step 1: Write failing frontend service/router tests**

Add tests for:

- `AuthManager.login` stores the returned session and exposes current user.
- `AuthManager.register` stores the returned session.
- `AuthManager.logout` clears session.
- `UserInfoManager.getUserInfo` returns authenticated user before local fallback.
- Anonymous route navigation redirects to `/login`.

Run: `npm run test -- AuthManager router-auth`
Expected: FAIL because `AuthManager` and guard behavior do not exist.

- [ ] **Step 2: Add API types and methods**

Add to `frontend_real/services/api.ts`:

- `AuthUser`
- `AuthSession`
- `LoginParams`
- `RegisterParams`
- `login`
- `register`
- `getAuthSession`
- `logout`

- [ ] **Step 3: Implement AuthManager**

Create `frontend_real/services/AuthManager.ts` with cached session and methods:

- `getSession(force = false)`
- `login(params)`
- `register(params)`
- `logout()`
- `getCurrentUser()`
- `clearCache()`

- [ ] **Step 4: Update UserInfoManager compatibility**

Make `UserInfoManager.getUserInfo()` return authenticated user first. Fallback to existing `/user-info` behavior for compatibility.

- [ ] **Step 5: Add router guard**

Add `/login` route and a `beforeEach` guard. Public routes are `/login` and `/pdf-viewer`.

- [ ] **Step 6: Run frontend auth tests**

Run: `npm run test -- AuthManager router-auth`
Expected: PASS.

### Task 5: Scan Login/Register UI And App Shell

**Files:**
- Create: `/root/data/projects/data-asset-scan/frontend_real/views/LoginView.vue`
- Modify: `/root/data/projects/data-asset-scan/frontend_real/App.vue`
- Modify: `/root/data/projects/data-asset-scan/frontend_real/style.css`
- Test: `/root/data/projects/data-asset-scan/frontend_real/views/__tests__/LoginView.test.ts`

- [ ] **Step 1: Write failing LoginView tests**

Create tests for:

- Login tab renders manage URL, username, password, and submit button.
- Register tab renders manage URL, username, display name, unit, department, phone, password, confirm password.
- Successful login calls `authManager.login` and redirects.
- Successful register calls `authManager.register` and redirects.

Run: `npm run test -- LoginView`
Expected: FAIL because `LoginView.vue` does not exist.

- [ ] **Step 2: Implement LoginView**

Use the approved A workbench-style layout:

- Left dark identity panel.
- Right white form panel.
- Tabs for login/register.
- Clear error alerts.
- Manage endpoint defaults to config value or `http://127.0.0.1:3002`.

- [ ] **Step 3: Update App shell**

In `App.vue`:

- Hide drawer/app bar on login and PDF viewer.
- Remove first-use machine-owner dialog flow.
- Load auth session instead of local user info.
- Show user chip and logout action for authenticated users.

- [ ] **Step 4: Run LoginView/App tests**

Run: `npm run test -- LoginView`
Expected: PASS.

### Task 6: Integration Verification And Commits

**Files:**
- Verify both repositories.

- [ ] **Step 1: Run manage tests**

Run in manage: `npm run test -- auth`
Expected: PASS.

- [ ] **Step 2: Run scan backend tests**

Run in scan: `GOCACHE=/tmp/go-build-cache go test ./internal/httpd -run 'TestHTTP_Auth'`
Expected: PASS.

- [ ] **Step 3: Run scan frontend tests**

Run in scan: `npm run test -- AuthManager router-auth LoginView`
Expected: PASS.

- [ ] **Step 4: Run build checks**

Run in manage: `npm run build`
Expected: PASS.

Run in scan: `npm run build`
Expected: PASS.

- [ ] **Step 5: Commit and report**

Commit manage and scan separately with clear messages:

- manage: `feat(auth): add account registration and login`
- scan: `feat(auth): login through manage`

Report changed files, tests run, and any limitations.

## Plan Self-Review

- Spec coverage: manage account table, register/login/me APIs, scan auth proxy, local mirror, frontend login/register UI, router guard, and compatibility behavior are all covered.
- Placeholder scan: no TBD/TODO placeholders remain.
- Type consistency: `username`, `display_name`, `user_unit`, `user_department`, `token`, and `user` names are used consistently across tasks.
