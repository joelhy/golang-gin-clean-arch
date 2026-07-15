# Task 12 Report

## Summary

Implemented Task 12 end to end:

- Added `reporting` domain package with validated time windows and read-only snapshot service.
- Added MySQL reporting read model plus integration coverage.
- Added order and stats web handlers with fail-closed permission checks and stable DTOs.
- Built the final Gin router with the approved route inventory, health endpoints, JSON `NoRoute`/`NoMethod`, and login rate limiting.
- Added minimal admin user lookup support needed for `GET /api/v1/admin/users/:id`.

## RED Phase

### Command

```bash
go test ./reporting ./web -run 'TestReporting|TestOrderHandler|TestRouter|TestHealth' -v
```

### Observed failure

```text
clean-arch-gin/reporting: no non-test Go files in /data/workspace/open-source/golang-gin-clean-arch/reporting
# clean-arch-gin/reporting [clean-arch-gin/reporting.test]
reporting/service_test.go:11:35: undefined: Range
reporting/service_test.go:11:43: undefined: Snapshot
reporting/service_test.go:14:57: undefined: Range
reporting/service_test.go:14:65: undefined: Snapshot
reporting/service_test.go:22:11: undefined: Snapshot
reporting/service_test.go:23:15: undefined: Users
reporting/service_test.go:24:15: undefined: Inventory
reporting/service_test.go:25:15: undefined: Orders
reporting/service_test.go:29:19: undefined: NewService
```

This confirmed the RED state was caused by missing Task 12 production code.

## Implementation Notes

### Reporting

- Added `reporting.Range`, `Snapshot`, aggregate types, validation sentinel/errors, and `Service.Snapshot`.
- Enforced UTC-only `from`/`to`, strict `from < to`, and a maximum 366-day range.
- Added `mysqlstore.ReportingStore` with explicit aggregate SQL for:
  - all users / active users / new users in range
  - active inventory stock snapshot
  - order counts and gross amount within range
- Mixed currencies now fail with `reporting.ErrMixedCurrency` instead of adding unlike money.

### User admin lookup

- Added `user.Service.AdminByID` guarded by `users:read`.
- Added `web.userHandler.GetAdmin`.

### Order and stats handlers

- Added `web/order_handler.go` covering:
  - customer create/list/get/cancel
  - admin list/confirm/ship/deliver/cancel
  - `Idempotency-Key` validation as 1–128 visible ASCII bytes
  - stable DTO mapping with immutable item snapshots and money objects
- Added `web/stats_handler.go` for `GET /api/v1/admin/stats`.

### Router

- Added `web/router.go` with the approved route inventory only.
- Middleware order in `NewRouter` is:
  1. request ID
  2. recovery
  3. security headers
  4. CORS
  5. access log
  6. route-specific login rate limit
  7. authentication
  8. authorization
- Added `/health/live`, `/health/ready`, JSON `NoRoute`, and JSON `NoMethod`.

## Verification

### Formatting

```bash
gofmt -w reporting mysqlstore web user
```

### Tests

```bash
go test ./reporting ./web -v
go test ./user -run TestServiceAdminByID -v
go test -tags=integration ./mysqlstore -run TestReportingStore -v
```

Results:

- `go test ./reporting ./web -v` passed.
- `go test ./user -run TestServiceAdminByID -v` passed.
- `go test -tags=integration ./mysqlstore -run TestReportingStore -v` passed.
  - Total runtime: about `94s`
  - Docker/Testcontainers was available in this environment.

### Static verification

```bash
go vet ./reporting ./web ./mysqlstore/...
```

Result:

- `go vet` passed.

## Files Changed

- Added:
  - `reporting/types.go`
  - `reporting/service.go`
  - `reporting/service_test.go`
  - `mysqlstore/reporting_store.go`
  - `mysqlstore/reporting_store_test.go`
  - `web/order_handler.go`
  - `web/order_handler_test.go`
  - `web/stats_handler.go`
  - `web/stats_handler_test.go`
  - `web/router.go`
  - `web/router_test.go`
  - `.superpowers/sdd/task-12-report.md`
- Modified:
  - `user/rbac.go`
  - `user/rbac_test.go`
  - `web/error.go`
  - `web/user_handler.go`
  - `web/user_handler_test.go`

## Concerns

None beyond the intended route-contract constraint that admin product read/publish routes remain unregistered in `NewRouter`.
