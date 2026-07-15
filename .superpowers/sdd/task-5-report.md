Recovered Task 5 report

Original implementer report was not present in this checkout when SDD resumed.
This file records the recoverable claims from docs/superpowers/status/2026-07-14-refactor-progress.md so the reviewer can read a report file while treating the diff as authoritative.

Implemented:
- Login, transparent password rehash, access token authentication, and live permission checks.
- Refresh token atomic rotation, old-token reuse detection, and session-family revocation.
- UserStore role/permission loading, filtering, pagination, and optimistic version updates.
- Last-admin status and role changes using the shared admin guard lock ordering.
- SessionStore returns ErrRefreshReuse only after committing reuse revocation so the security state is not rolled back with the error.

Tests reported in status:
- Non-container baseline tests were intended before resuming.
- Integration tests requiring Docker were blocked by the unavailable Docker provider.

Files changed by Task 5 range:
- user/auth.go
- user/auth_test.go
- mysqlstore/user_store.go
- mysqlstore/session_store.go
- mysqlstore/user_store_test.go
- mysqlstore/session_store_test.go
- mysqlstore/testmysql_test.go
- related generated/query or model use if shown by the review package.

Concerns:
- Status document says Task 5 passed spec review but final independent quality review was not completed.

## Task 5 fix follow-up (2026-07-14)

### What changed
- Added `ErrInvalidAccessToken` and made `Authenticate` wrap parser failures and malformed identities with a stable `errors.Is` sentinel while still redacting bearer material.
- Hardened `Refresh` to reload the account after `sessions.Rotate`; if the account is missing or inactive after rotation, the service now revokes the persisted session family before returning `ErrSessionRevoked` or `ErrDisabled`.
- Made post-persistence access-token issuance errors report the correct operation prefix for both login and refresh paths.
- Added auth tests covering invalid access token wrapping and post-rotation missing/disabled account revocation.

### RED evidence
- Command: `go test -count=1 ./user -run 'TestAuth' -v`
- Relevant failing output:
  ```text
  # clean-arch-gin/user [clean-arch-gin/user.test]
  user/auth_test.go:406:23: undefined: ErrInvalidAccessToken
  FAIL    clean-arch-gin/user [build failed]
  FAIL
  ```
- Why expected: the new test suite referenced the required domain sentinel before production code defined it, proving the auth surface did not yet support `errors.Is(..., ErrInvalidAccessToken)`.

### GREEN evidence
- Command: `go test -count=1 ./user -run 'TestAuth' -v`
- Passing output:
  ```text
  PASS
  ok      clean-arch-gin/user    0.013s
  ```
- Command: `go test -count=1 ./user -v`
- Passing output:
  ```text
  PASS
  ok      clean-arch-gin/user    0.440s
  ```

### Files changed
- `user/auth.go`
- `user/auth_test.go`
- `user/errors.go`
- `.superpowers/sdd/task-5-report.md`

### Self-review and concerns
- The refresh fix keeps the rotation-first persistence contract intact and revokes the family before returning whenever a post-rotation account check fails.
- Error handling still avoids leaking access or refresh bearer material in the new paths.
- No additional concerns from the scoped `./user` verification run.

## Task 5 mysqlstore re-review fix follow-up (2026-07-14)

### What changed
- Routed `UserStore.IsLastActiveAdmin` through the same transaction and `lockAdminGuardAndUser` path used by status and role writes, then performed the active-admin recount inside that transaction while keeping inactive and non-admin users on the existing `false` path.
- Tightened `mapRefreshWriteError` so only duplicate-key violations on `uk_refresh_tokens_digest` or `refresh_tokens.uk_refresh_tokens_digest` collapse to `user.ErrInvalidRefresh`.
- Added unit coverage for refresh-token duplicate-key classification and an integration assertion for `IsLastActiveAdmin` status/role semantics when the MySQL harness is able to run.

### RED evidence
- Command: `go test -count=1 ./mysqlstore -run 'TestMapRefreshWriteError' -v`
- Relevant failing output:
  ```text
  === RUN   TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint/primary_key
      session_store_unit_test.go:108: errors.Is(mapRefreshWriteError(), ErrInvalidRefresh) = true, want false; error rotate session: insert replacement: invalid refresh token
  === RUN   TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint/replacement_key
      session_store_unit_test.go:108: errors.Is(mapRefreshWriteError(), ErrInvalidRefresh) = true, want false; error rotate session: insert replacement: invalid refresh token
  === RUN   TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint/other_unique_key
      session_store_unit_test.go:108: errors.Is(mapRefreshWriteError(), ErrInvalidRefresh) = true, want false; error rotate session: insert replacement: invalid refresh token
  FAIL
  ```
- Why expected: the new focused test proves the old mapper treated every MySQL 1062 as an invalid refresh token instead of preserving non-digest duplicate-key failures as storage errors.

### GREEN evidence
- Command: `go test -count=1 ./mysqlstore -run 'TestMapRefreshWriteError' -v`
- Passing output:
  ```text
  --- PASS: TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint (0.00s)
  PASS
  ok      clean-arch-gin/mysqlstore   0.009s
  ```
- Command: `go test -count=1 ./mysqlstore -v`
- Passing output:
  ```text
  PASS
  ok      clean-arch-gin/mysqlstore   0.016s
  ```
- Command: `go test -count=1 ./user ./mysqlstore -v`
- Passing output:
  ```text
  PASS
  ok      clean-arch-gin/user         0.020s
  ok      clean-arch-gin/mysqlstore   0.015s
  ```

### Files changed
- `mysqlstore/user_store.go`
- `mysqlstore/session_store.go`
- `mysqlstore/session_store_unit_test.go`
- `mysqlstore/user_store_test.go`
- `.superpowers/sdd/task-5-report.md`

### Self-review and concerns
- `IsLastActiveAdmin` now shares the same lock ordering and recount path as the authority-reducing writes, which removes the independent-read gap called out by the re-review.
- The refresh duplicate-key mapper still redacts credential-derived digest collisions, but unrelated 1062 failures now preserve `errors.As(..., *mysql.MySQLError)` for callers and logs.
- Docker was available (`docker version` reported `29.5.3`), but `go test -count=1 -tags=integration ./mysqlstore -run 'TestUserStore|TestSessionStore' -v` stalled for several minutes while `mysql:8.4` was still being pulled and had to be interrupted. The added integration assertion was written, but I do not have a completed integration result to claim.

## Task 5 mysqlstore redaction gap follow-up (2026-07-14)

### What changed
- Strengthened `TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint` so non-digest duplicate-key cases also carry a sensitive duplicate value and fail if `err.Error()` exposes it.
- Updated `mapRefreshWriteError` to map only the `uk_refresh_tokens_digest` duplicate constraint to `user.ErrInvalidRefresh`.
- Routed all other MySQL 1062 refresh-write failures through `redactedStorageError` so the public error text stays redacted while `errors.As(..., *mysql.MySQLError)` still works.

### RED evidence
- Command: `go test -count=1 ./mysqlstore -run 'TestMapRefreshWriteError' -v`
- Relevant failing output:
  ```text
  === RUN   TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint/primary_key
      session_store_unit_test.go:111: mapRefreshWriteError() exposed duplicate value
  === RUN   TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint/replacement_key
      session_store_unit_test.go:111: mapRefreshWriteError() exposed duplicate value
  === RUN   TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint/other_unique_key
      session_store_unit_test.go:111: mapRefreshWriteError() exposed duplicate value
  FAIL
  ```
- Why expected: the previous wrapper returned `fmt.Errorf("%s: %w", operation, err)` for non-digest duplicate keys, so the driver message still leaked the duplicate entry value.

### GREEN evidence
- Command: `go test -count=1 ./mysqlstore -run 'TestMapRefreshWriteError' -v`
- Passing output:
  ```text
  --- PASS: TestMapRefreshWriteErrorOnlyMapsRefreshDigestConstraint (0.00s)
  PASS
  ok      clean-arch-gin/mysqlstore   0.008s
  ```
- Command: `go test -count=1 ./mysqlstore -v`
- Passing output:
  ```text
  PASS
  ok      clean-arch-gin/mysqlstore   0.020s
  ```
- Command: `go test -count=1 ./user ./mysqlstore -v`
- Passing output:
  ```text
  PASS
  ok      clean-arch-gin/user         0.012s
  ok      clean-arch-gin/mysqlstore   0.013s
  ```
- Command: `timeout 300s go test -count=1 -tags=integration ./mysqlstore -run 'TestUserStore|TestSessionStore' -v`
- Blocker:
  ```text
  === RUN   TestSessionStoreLifecycleAndReuseRevocation
  2026/07/14 20:08:41 github.com/testcontainers/testcontainers-go - Connected to docker:
  ...
  2026/07/14 20:08:41 No image auth found for https://index.docker.io/v1/. Setting empty credentials for the image: mysql:8.4.
  signal: interrupt
  FAIL    clean-arch-gin/mysqlstore   270.087s
  ```
- Interpretation: Docker was reachable, but the integration run made no further progress after Testcontainers began preparing `mysql:8.4`, so I stopped it once the practical timeout window had clearly been exceeded.
