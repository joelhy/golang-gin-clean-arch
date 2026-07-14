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
