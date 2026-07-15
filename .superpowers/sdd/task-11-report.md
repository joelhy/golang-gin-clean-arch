# Task 11 Report

## Status

DONE

## Requirements source

- `/data/workspace/open-source/golang-gin-clean-arch/.superpowers/sdd/task-11-brief.md`

## Implemented

### `web/auth_handler.go`

- Added narrow web-layer interfaces for registration and auth flows.
- Implemented:
  - `Register`
  - `Login`
  - `Refresh`
  - `Logout`
  - `Authenticated()` middleware
  - `RequirePermission()` middleware
- Enforced Task 11 auth transport rules:
  - exactly one `Authorization` header value
  - exact `Bearer <token>` parsing
  - fail closed for missing, malformed, or duplicate auth headers
  - typed identity stored under a private web context key
  - permission middleware delegates to `Authorize`
- Added token response DTO mapping with exact snake_case fields:
  - `token_type`
  - `access_token`
  - `refresh_token`
  - `expires_in`
  - `refresh_expires_in`

### `web/user_handler.go`

- Added narrow interface-backed user HTTP handler.
- Implemented:
  - `Me`
  - `UpdateMe`
  - `List`
  - `SetStatus`
  - `ReplaceRoles`
  - `Permissions`
- Mapped user domain models to transport DTOs that avoid sensitive fields.
- Used UTC `RFC3339Nano` formatting for timestamps.
- Used standard pagination envelopes under `data.pagination`.

### `web/product_handler.go`

- Added narrow interface-backed product HTTP handler.
- Implemented:
  - `ListPublic`
  - `GetPublic`
  - `Create`
  - `GetAdmin`
  - `Update`
  - `ListAdmin`
  - `Publish`
  - `Unpublish`
  - `AdjustStock`
- Mapped money values as `{amount,currency}`.
- Ensured public endpoints use public service methods and do not trust public `status` query flags.

## TDD evidence

### RED

Wrote failing handler tests first in:

- `web/auth_handler_test.go`
- `web/user_handler_test.go`
- `web/product_handler_test.go`

Ran:

```bash
go test ./web -run 'TestAuthHandler|TestUserHandler|TestProductHandler' -v
```

Observed RED/build failure because handlers did not exist yet:

```text
# clean-arch-gin/web [clean-arch-gin/web.test]
web/auth_handler_test.go:64:13: undefined: NewAuthHandler
web/auth_handler_test.go:102:13: undefined: NewAuthHandler
web/auth_handler_test.go:124:13: undefined: NewAuthHandler
web/auth_handler_test.go:139:13: undefined: NewAuthHandler
web/auth_handler_test.go:173:13: undefined: NewAuthHandler
web/auth_handler_test.go:196:13: undefined: NewAuthHandler
web/auth_handler_test.go:228:13: undefined: NewAuthHandler
web/auth_handler_test.go:261:13: undefined: NewAuthHandler
web/auth_handler_test.go:298:13: undefined: NewAuthHandler
web/auth_handler_test.go:325:13: undefined: NewAuthHandler
web/auth_handler_test.go:325:13: too many errors
FAIL    clean-arch-gin/web [build failed]
FAIL
```

### GREEN

Implemented the minimum production code to satisfy the tests, then ran:

```bash
gofmt -w web
go test ./web -run 'TestAuthHandler|TestUserHandler|TestProductHandler' -v
go vet ./web
go test ./web -v
```

Results:

- Focused handler tests: PASS
- `go vet ./web`: PASS
- Full `go test ./web -v`: PASS

## Files changed

- `web/auth_handler.go`
- `web/auth_handler_test.go`
- `web/user_handler.go`
- `web/user_handler_test.go`
- `web/product_handler.go`
- `web/product_handler_test.go`

## Verification summary

- Auth tests cover:
  - register 201
  - duplicate email 409
  - malformed JSON 400
  - login token DTO shape
  - invalid credentials 401
  - refresh rotation
  - logout
  - missing/malformed/duplicate bearer failure
  - revoked session failure
  - permission denial
- User tests cover:
  - current profile
  - profile update
  - admin list filters and pagination
  - invalid query rejection
  - status update
  - role replacement
  - permissions endpoint
  - sensitive field absence
- Product tests cover:
  - public list/get using public service methods only
  - admin create/get/update/list
  - publish/unpublish
  - stock adjustment
  - money DTO mapping
  - pagination envelope

## Minimal design choices

- Kept handler dependencies as web-local interfaces instead of concrete services.
- Reused existing `decodeJSON`, `DecodePaginationQuery`, `problemFromError`, and envelope writers from prior tasks.
- Stored granted permissions in request context after middleware authorization so admin handlers can build the domain `Actor` expected by the user service without widening handler dependencies.

## Supporting changes outside Task 11 scope

- None.

## Self-review findings

- No blocking issues found after `go test` and `go vet`.
- No existing web foundation file required changes.
- DTOs do not expose password hashes or other sensitive auth persistence fields.
- Public product handlers call public service methods and ignore public `status` query flags.

## Git

- Branch: `feat/task-11-web-handlers`
- Commit: `feat(web): 实现认证用户与商品处理器`（当前 HEAD）
- Push: `origin/feat/task-11-web-handlers`
- PR helper URL from push output:
  - `https://github.com/joelhy/golang-gin-clean-arch/pull/new/feat/task-11-web-handlers`

## Concerns

- None.
