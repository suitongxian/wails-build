# Manage-Centered Login And Registration Design

## Background

The current scan project treats "login" as local machine-owner information. The frontend reads and writes `/user-info`, which persists to scan's local SQLite `user_info` table and mirrors into scan's local `users` table. That is useful for a single-terminal demo, but it is not a real identity boundary:

- User identity is not managed by the management platform.
- Registration and login do not call a backend identity API.
- Different terminals can create inconsistent local identities.
- Project stage ownership and audit fields cannot reliably identify a cross-terminal person.

The product direction is that manage is the management platform and should be the authoritative source for user accounts. Scan should act as the terminal client: it logs in against manage, registers users through manage, and keeps only the current session and a local compatibility mirror.

## Goals

- Let scan users register themselves from the scan login page.
- Store registered account information in manage, not only in scan local SQLite.
- Let scan login by calling manage.
- Return a token and user profile from manage so scan can identify the current operator.
- Keep existing scan pages working with minimal churn by mirroring the logged-in user into scan's local `user_info` and `users` tables.
- Redesign the scan login/register page using the selected "workbench-style login" visual direction.

## Non-Goals

- No administrator approval workflow in the first version. Self-registration is immediately active.
- No full role-management UI in this pass. New self-registered users default to the ordinary `user` role.
- No replacement of all project permission checks in this pass. Existing code may continue to use current local helpers while receiving the logged-in user mirror.
- No single sign-on integration, password reset, or multi-factor authentication.

## User Experience

Scan adds a dedicated `/login` page. All normal scan routes require an authenticated session, except `/login` and the PDF viewer.

The page uses a two-panel workbench design:

- Left panel: product identity and trust cues, emphasizing "数据可信终端", "个人电子文件数字助理", unified identity, terminal registration, and audit traceability.
- Right panel: a compact form with two tabs, "登录" and "注册".

Login fields:

- Management backend address, defaulting from local system config `manage_endpoint` or `http://127.0.0.1:3002`.
- Username.
- Password.

Registration fields:

- Management backend address.
- Username.
- Display name.
- Unit.
- Department.
- Phone, optional.
- Password.
- Confirm password.

After successful registration, scan automatically treats the response as a login session and enters the main app.

## Manage Data Model

Manage keeps `terminal_user_registry` as a terminal registration and login-trace table. It is not reused as the account table.

Add an account table, named `auth_users`:

| Column | Purpose |
| --- | --- |
| `id` | Stable internal user ID |
| `username` | Unique login account |
| `display_name` | Human-readable name |
| `password_hash` | Hashed password, never plaintext |
| `user_unit` | Unit / organization |
| `user_department` | Department |
| `phone` | Optional phone |
| `role` | `user`, future-compatible with admin roles |
| `status` | `active`, future-compatible with pending/disabled flows |
| `last_login_time` | Last successful login timestamp |
| `create_time` | Created timestamp |
| `update_time` | Updated timestamp |
| `disabled` | Soft-delete marker |

Password hashing should use a standard one-way hash with salt. Because manage is a Node/Nuxt project, the initial implementation may use `crypto.scryptSync` with a per-user random salt stored inside the encoded hash string, for example `scrypt$N$r$p$salt$hash`. Plain MD5 is not acceptable for account passwords.

## Manage API

Add these endpoints:

### `POST /api/auth/register`

Request:

```json
{
  "username": "zhangsan",
  "password": "secret",
  "display_name": "张三",
  "user_unit": "某单位",
  "user_department": "办公室",
  "phone": "13800000000",
  "terminal_app_version": "0.1.6",
  "computer_ip": "192.168.1.8",
  "computer_mac": "AA:BB:CC:DD:EE:FF"
}
```

Behavior:

- Validate required fields.
- Reject duplicate active usernames.
- Create an active ordinary user.
- Create or update `terminal_user_registry` using IP + MAC.
- Return a session token and the user profile.

### `POST /api/auth/login`

Request:

```json
{
  "username": "zhangsan",
  "password": "secret",
  "terminal_app_version": "0.1.6",
  "computer_ip": "192.168.1.8",
  "computer_mac": "AA:BB:CC:DD:EE:FF"
}
```

Behavior:

- Validate username and password.
- Reject disabled or inactive accounts.
- Update `auth_users.last_login_time`.
- Create or update `terminal_user_registry`.
- Return a session token and the user profile.

### `GET /api/auth/me`

Behavior:

- Reads `Authorization: Bearer <token>`.
- Returns the current user profile if the token is valid.
- Returns unauthorized if missing or invalid.

### Token

First version can use signed JWT tokens with a server-side secret. The token must include the user ID and username, not the password hash. The existing `jsonwebtoken` dependency in manage can be used.

## Scan Backend

Scan adds auth proxy endpoints that call manage:

- `POST /auth/register`
- `POST /auth/login`
- `GET /auth/me`
- `POST /auth/logout`

These endpoints:

- Read or accept the manage backend address.
- Forward credentials and terminal metadata to manage.
- Store the returned token in the scan process session and optionally in a local config table for app restart convenience.
- Mirror the returned profile into local `user_info` and local `users`.
- Return a normalized scan auth session to the frontend.

The mirror keeps old code working:

- `user_info.user_name` receives `display_name` when present, otherwise `username`.
- `user_info.company_name` receives `user_unit`.
- `user_info.department` receives `user_department`.
- `users.username` receives `username`.
- `users.display_name` receives `display_name`.

Scan should not allow arbitrary local editing of the current user's identity after login. Existing "机主信息" editing entry points should either be hidden or become profile display only in this pass.

## Scan Frontend

Add:

- `frontend_real/views/LoginView.vue`
- `frontend_real/services/AuthManager.ts`
- Auth API methods in `frontend_real/services/api.ts`
- Router guard in `frontend_real/plugins/router.ts`

Update:

- `App.vue` should not show the old first-use machine-owner dialog.
- The right top user chip should show the authenticated profile and a logout action.
- `UserInfoManager.getUserInfo()` should return the authenticated user profile first, falling back to the local mirror only for compatibility.

Routes:

- `/login` is public.
- Normal routes redirect to `/login` when unauthenticated.
- After login or registration, redirect to the original target or `/`.

## Error Handling

- Manage connection failure: scan login page shows that the management backend is unreachable and keeps the user on the page.
- Duplicate username: registration form shows a field-level or form-level error.
- Wrong password: login form shows a generic authentication failure.
- Invalid token: scan clears the current session and redirects to login.
- Missing IP/MAC: scan may still login, but terminal registration should use best-effort values and clearly avoid crashing.

## Testing

Manage tests:

- Register creates `auth_users` and terminal registry record.
- Register rejects duplicate username.
- Login accepts correct password and rejects wrong password.
- Login updates `last_login_time` and terminal registry.
- `/api/auth/me` returns current user with a valid token and rejects missing/invalid token.

Scan backend tests:

- `/auth/login` forwards to manage and mirrors user info locally.
- `/auth/register` forwards to manage and mirrors user info locally.
- `/auth/me` returns the cached/current user when token is valid.
- Logout clears the scan session.

Scan frontend tests:

- Login page renders login/register tabs and the selected workbench-style structure.
- Successful login redirects.
- Successful registration redirects.
- Route guard redirects anonymous users to `/login`.
- Existing pages that consume `UserInfoManager.getUserInfo()` receive the authenticated profile.

## Rollout Notes

This is a foundation change. The first implementation should be deliberately small:

1. Build manage account APIs.
2. Build scan auth proxy and local mirror.
3. Build scan login/register UI and route guard.
4. Keep existing current-user consumers working through compatibility.

Future work can add admin approval, user management UI, role assignment, password reset, and stronger token storage.

## Spec Review

- No placeholder sections remain.
- The account source of truth is manage throughout the document.
- Self-registration is explicitly immediate-active, matching the approved product direction.
- Existing local scan identity tables are kept only as compatibility mirrors.
- The selected A visual direction is captured without over-specifying pixel-level implementation.
