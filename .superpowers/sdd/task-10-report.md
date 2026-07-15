# Task 10 Report: Uniform HTTP envelope, strict decoding, and middleware

## Status

DONE

## What I implemented

Created the `web` package foundation required by Task 10:

- `web/response.go`
  - Uniform success envelope with JSON `code` and optional `data`
  - Uniform failure envelope with nonzero `code`, nonempty `message`, optional `data`
  - No `success`, `meta`, `details`, or `request_id` body fields
- `web/error.go`
  - Central `problemFromError(error) (status int, problem Problem)` mapper
  - Domain sentinel mapping for `user`, `product`, and `order`
  - Unknown errors sanitized to HTTP 500 + `CodeInternal`
- `web/decode.go`
  - Strict JSON decoding with `http.MaxBytesReader`
  - `json.Decoder.DisallowUnknownFields()`
  - Exactly one JSON value required
  - Field-specific validation data for unknown fields and type mismatches
- `web/pagination.go`
  - Pagination envelope helper with `data.pagination`
  - Query decoding for `limit`, `offset`, `sort`, `direction`, `start_at`, `end_at`
  - Rejection for repeated singleton params, invalid integers, limit outside `1-100`, negative offset, unknown sort/direction, malformed UTC timestamps, inverted ranges
- `web/middleware.go`
  - Request ID propagation via `X-Request-ID`
  - Panic recovery with stack traces logged only
  - Security headers
  - CORS allow/deny/preflight handling
  - Trusted proxies support through `UseStandard`
  - Max body size enforcement
  - Login rate limiting by IP and account using `x/time/rate`
  - Cleanup goroutine that stops on context cancellation
  - Access logs with normalized path and no credential/header leakage
  - Context cancellation middleware returning HTTP `499`

Also added the matching tests:

- `web/response_test.go`
- `web/error_test.go`
- `web/decode_test.go`
- `web/pagination_test.go`
- `web/middleware_test.go`

## TDD evidence

### RED

Command:

```bash
go test ./web -run 'TestWrite|TestDecode|TestMiddleware|TestCORS|TestRate' -v
```

Observed failure excerpt before production code existed:

```text
# clean-arch-gin/web [clean-arch-gin/web.test]
web/pagination_test.go:134:54: undefined: Problem
web/decode_test.go:22:16: undefined: decodeJSON
web/decode_test.go:35:13: undefined: decodeJSON
web/decode_test.go:39:21: undefined: CodeValidation
...
FAIL    clean-arch-gin/web [build failed]
```

This is the expected red phase: the contract tests existed first and failed because the `web` package surface had not been implemented yet.

### GREEN

Command:

```bash
gofmt -w web
go test ./web -run 'TestWrite|TestDecode|TestMiddleware|TestCORS|TestRate' -v
```

Result:

```text
PASS
ok      clean-arch-gin/web  0.013s
```

## Verification commands and outputs

### 1. Focused foundation tests

Command:

```bash
go test ./web -run 'TestWrite|TestDecode|TestMiddleware|TestCORS|TestRate' -v
```

Result:

```text
PASS
ok      clean-arch-gin/web  0.013s
```

### 2. Vet

Command:

```bash
go vet ./web
```

Result: exit code `0`, no findings.

### 3. Full web package tests

Command:

```bash
go test ./web -v
```

Result:

```text
PASS
ok      clean-arch-gin/web  0.009s
```

## Files changed

- `web/response.go`
- `web/response_test.go`
- `web/error.go`
- `web/error_test.go`
- `web/decode.go`
- `web/decode_test.go`
- `web/pagination.go`
- `web/pagination_test.go`
- `web/middleware.go`
- `web/middleware_test.go`
- `.superpowers/sdd/task-10-report.md`

## Self-review findings

- The response envelope contract matches the task brief exactly for success and failure bodies.
- Pagination is nested under `data.pagination` through `PageData`.
- Error translation is centralized and transport-safe.
- Non-obvious security, request-body, recovery, and rate-limit logic includes concise maintenance comments per `AGENTS.md`.
- The login limiter restores the request body after account extraction so later strict decoders can still consume the original payload.

## Concerns

- There is no dedicated rate-limit application code in the Task 10 brief constant set, so the login throttle currently uses HTTP `429` with `CodeAuthentication` and message `too many login attempts`. This behavior is covered by tests, but if Task 11/12 want a distinct application code, that will need a follow-up change.

## Task 10 review fix update

### What changed

- `web/middleware.go`
  - CORS disallowed preflight now writes the standard JSON failure envelope instead of a bare `403`.
  - Context cancellation now writes the standard JSON failure envelope instead of a bare `499`.
  - Existing status/code choices were preserved; only the response shape changed.
- `web/middleware_test.go`
  - Added envelope assertions for disallowed preflight, canceled context, max-body rejection, and rate limiting.
  - The helper checks `code`, `message`, and the absence of forbidden keys such as `success`, `meta`, `details`, and `request_id`.

### RED evidence

Command:

```bash
go test ./web -run 'TestMiddleware|TestCORS|TestRate' -v
```

Observed failures before the middleware fix:

```text
--- FAIL: TestCORSRejectsDisallowedPreflight
    middleware_test.go:148: unmarshal body: unexpected end of JSON input; body=""
--- FAIL: TestMiddlewareCanceledContextReturns499
    middleware_test.go:269: unmarshal body: unexpected end of JSON input; body=""
```

### GREEN evidence

Command:

```bash
gofmt -w web/middleware.go web/middleware_test.go && go test ./web -run 'TestMiddleware|TestCORS|TestRate' -v
```

Result:

```text
PASS
ok  	clean-arch-gin/web	0.017s
```

### Commands run

```bash
go test ./web -run 'TestMiddleware|TestCORS|TestRate' -v
go test ./web -v
go vet ./web
```

### Outputs

- Focused middleware tests: pass
- Full `web` package tests: pass
- `go vet ./web`: exit code `0`

### Files changed in this fix

- `web/middleware.go`
- `web/middleware_test.go`
- `.superpowers/sdd/task-10-report.md`

### Self-review

- The fix is minimal and local to middleware behavior.
- The response envelope contract is now enforced by tests for every middleware-generated failure path covered in Task 10.
- No new rate-limit application code was introduced.

### Concerns

- None beyond the existing task-level note about the current rate-limit application code choice.

## Task 10 second re-review fix update

### What changed

- `web/middleware.go`
  - Moved `AccessLog` earlier in `UseStandard` so it wraps later middleware and records middleware-generated failures after their final status is known.
  - Left `ContextCancellation` inside that logging wrapper so canceled requests are logged with the final `499` envelope instead of the default status.
  - Added tracked max-body reads plus a buffered response writer for unknown-length/chunked bodies so an ignored `*http.MaxBytesError` cannot escape as a success response.
- `web/middleware_test.go`
  - Added focused regressions for denied preflight logging with final `403`, canceled request logging with final `499`, and unknown-length oversized body rejection even when the handler ignores the read error.
  - Reused the Task 10 envelope assertions so each middleware-generated failure still has nonzero `code`, nonempty `message`, and no forbidden keys.

### RED evidence

Command:

```bash
go test ./web -run 'TestMiddleware|TestCORS|TestRate' -v
```

Observed failures before the production fix:

```text
--- FAIL: TestMiddlewareLogsDeniedPreflightWithFinal403
    middleware_test.go:175: logs = "", want substring "status=403"
--- FAIL: TestMiddlewareMaxBodyBytesUnknownLengthReturnsEnvelopeWhenHandlerIgnoresReadError
    middleware_test.go:234: status = 200, want 413
--- FAIL: TestMiddlewareLogsCanceledRequestWithFinal499
    middleware_test.go:340: logs = "... status=200 ...", want substring "status=499"
FAIL
```

### GREEN evidence

Commands:

```bash
gofmt -w web/middleware.go web/middleware_test.go
go test ./web -run 'TestMiddleware|TestCORS|TestRate' -v
go test ./web -v
go vet ./web
```

Results:

```text
PASS
ok  	clean-arch-gin/web	0.012s
```

```text
PASS
ok  	clean-arch-gin/web	0.014s
```

`go vet ./web`: exit code `0`, no findings.

### Files changed in this fix

- `web/middleware.go`
- `web/middleware_test.go`
- `.superpowers/sdd/task-10-report.md`

### Self-review

- `AccessLog` now observes middleware aborts and post-handler cancellation rewriting through the standard stack instead of logging an intermediate status.
- The streamed max-body guard is scoped to unknown-length requests, which keeps the fix local to the reviewer-identified gap and avoids changing the fixed-length fast path.
- Buffered response handling preserves headers already set by outer middleware, so request ID and security headers remain intact on the chunked overflow path.

### Concerns

- No unrelated workspace edits were present in the files this fix touched.
