# Task 7 Report

## What changed

- Added [mysqlstore/product_store.go](/data/workspace/open-source/golang-gin-clean-arch/mysqlstore/product_store.go) implementing `product.Store` on MySQL/GORM Gen.
- Added [mysqlstore/product_store_test.go](/data/workspace/open-source/golang-gin-clean-arch/mysqlstore/product_store_test.go) integration coverage for:
  - unique SKU conflict mapping with redacted duplicate values
  - active-only lookup/list behavior
  - stable sorting with ID tie-breaks
  - ordinary update preserving persisted stock
  - optimistic version conflict and missing-row mapping
  - stock adjustment ledger writes, overflow/insufficient stock handling, and concurrent decrements

## RED evidence

Command:

```bash
timeout 300s go test -count=1 -tags=integration ./mysqlstore -run TestProductStore -v
```

Output:

```text
# clean-arch-gin/mysqlstore [clean-arch-gin/mysqlstore.test]
mysqlstore/product_store_test.go:371:51: undefined: ProductStore
mysqlstore/product_store_test.go:373:16: undefined: NewProductStore
mysqlstore/product_store_test.go:380:47: undefined: ProductStore
FAIL	clean-arch-gin/mysqlstore [build failed]
FAIL
```

Why expected:

- `ProductStore` and `NewProductStore` did not exist yet, so the new integration tests failed before implementation as intended by TDD.

## GREEN evidence

`gofmt -w mysqlstore/product_store.go mysqlstore/product_store_test.go`

- exit 0

`go test -count=1 ./...`

```text
ok  	clean-arch-gin/mysqlstore	0.012s
?   	clean-arch-gin/mysqlstore/generate	[no test files]
?   	clean-arch-gin/mysqlstore/model	[no test files]
?   	clean-arch-gin/mysqlstore/query	[no test files]
ok  	clean-arch-gin/product	0.008s
ok  	clean-arch-gin/security	0.370s
ok  	clean-arch-gin/user	0.014s
```

`go vet ./mysqlstore/...`

- exit 0

## Task 7 final review follow-up

### What changed

- Updated [mysqlstore/product_store_test.go](/data/workspace/open-source/golang-gin-clean-arch/mysqlstore/product_store_test.go) so each `TestProductStore` subtest creates its own `newTestDB` and `ProductStore`, preventing shared database state from hiding ordering bugs.
- Tightened `ListStableSorting` to assert only against the three rows it seeds itself instead of expecting rows created by `CreateLookupListAndUpdate`.

### Verification

`timeout 300s go test -count=1 -tags=integration ./mysqlstore -run 'TestProductStore/ListStableSorting' -v`

```text
=== RUN   TestProductStore
=== RUN   TestProductStore/ListStableSorting
    product_store_test.go:240: List(price desc) returned 3 items, want at least 4
--- FAIL: TestProductStore (21.26s)
    --- FAIL: TestProductStore/ListStableSorting (0.03s)
FAIL
FAIL	clean-arch-gin/mysqlstore	21.354s
FAIL
```

`timeout 300s go test -count=1 -tags=integration ./mysqlstore -run TestProductStore -v`

```text
=== RUN   TestProductStore
=== RUN   TestProductStore/CreateLookupListAndUpdate
=== RUN   TestProductStore/ListStableSorting
=== RUN   TestProductStore/UpdateAndAdjustStockErrorMapping
=== RUN   TestProductStore/ConcurrentStockAdjustment
--- PASS: TestProductStore (76.13s)
    --- PASS: TestProductStore/CreateLookupListAndUpdate (20.31s)
    --- PASS: TestProductStore/ListStableSorting (20.22s)
    --- PASS: TestProductStore/UpdateAndAdjustStockErrorMapping (17.66s)
    --- PASS: TestProductStore/ConcurrentStockAdjustment (17.93s)
PASS
ok  	clean-arch-gin/mysqlstore	76.188s
```

`go test -count=1 ./...`

```text
?   	clean-arch-gin/cmd	[no test files]
ok  	clean-arch-gin/config	0.184s
?   	clean-arch-gin/internal/adapters/controllers	[no test files]
?   	clean-arch-gin/internal/adapters/middleware	[no test files]
?   	clean-arch-gin/internal/adapters/models	[no test files]
?   	clean-arch-gin/internal/adapters/repositories	[no test files]
?   	clean-arch-gin/internal/adapters/shared/models	[no test files]
?   	clean-arch-gin/internal/adapters/usecases	[no test files]
?   	clean-arch-gin/internal/adapters/user/controllers	[no test files]
?   	clean-arch-gin/internal/adapters/user/repositories	[no test files]
?   	clean-arch-gin/internal/adapters/user/usecases	[no test files]
?   	clean-arch-gin/internal/application/user/commands	[no test files]
?   	clean-arch-gin/internal/application/user/queries	[no test files]
?   	clean-arch-gin/internal/di	[no test files]
?   	clean-arch-gin/internal/domain/order/entities	[no test files]
?   	clean-arch-gin/internal/domain/shared/entities	[no test files]
?   	clean-arch-gin/internal/domain/user/entities	[no test files]
?   	clean-arch-gin/internal/domain/user/repositories	[no test files]
?   	clean-arch-gin/internal/domain/user/usecases	[no test files]
?   	clean-arch-gin/internal/infrastructure/config	[no test files]
?   	clean-arch-gin/internal/infrastructure/database	[no test files]
?   	clean-arch-gin/internal/infrastructure/database/query	[no test files]
?   	clean-arch-gin/internal/infrastructure/router	[no test files]
?   	clean-arch-gin/internal/infrastructure/router/user	[no test files]
?   	clean-arch-gin/internal/modules	[no test files]
?   	clean-arch-gin/internal/modules/order	[no test files]
?   	clean-arch-gin/internal/modules/user	[no test files]
ok  	clean-arch-gin/migrations	0.006s
ok  	clean-arch-gin/mysqlstore	0.015s
?   	clean-arch-gin/mysqlstore/generate	[no test files]
?   	clean-arch-gin/mysqlstore/model	[no test files]
?   	clean-arch-gin/mysqlstore/query	[no test files]
ok  	clean-arch-gin/product	0.008s
ok  	clean-arch-gin/security	0.326s
ok  	clean-arch-gin/user	0.010s
```

`go vet ./mysqlstore/...`

- exit 0

`timeout 300s go test -count=1 -tags=integration ./mysqlstore -run TestProductStore -v`

```text
=== RUN   TestProductStore
=== RUN   TestProductStore/CreateLookupListAndUpdate
=== RUN   TestProductStore/ListStableSorting
=== RUN   TestProductStore/UpdateAndAdjustStockErrorMapping
=== RUN   TestProductStore/ConcurrentStockAdjustment
--- PASS: TestProductStore (66.44s)
    --- PASS: TestProductStore/CreateLookupListAndUpdate (0.07s)
    --- PASS: TestProductStore/ListStableSorting (0.04s)
    --- PASS: TestProductStore/UpdateAndAdjustStockErrorMapping (0.06s)
    --- PASS: TestProductStore/ConcurrentStockAdjustment (0.05s)
PASS
ok  	clean-arch-gin/mysqlstore	66.503s
```

Skipped:

- `go test -count=1 ./mysqlstore -run 'TestNewProductStore|TestProduct' -v` was intentionally skipped because no non-integration tests were added for this task.

## Files changed

- [mysqlstore/product_store.go](/data/workspace/open-source/golang-gin-clean-arch/mysqlstore/product_store.go)
- [mysqlstore/product_store_test.go](/data/workspace/open-source/golang-gin-clean-arch/mysqlstore/product_store_test.go)
- [task-7-report.md](/data/workspace/open-source/golang-gin-clean-arch/.superpowers/sdd/task-7-report.md)

## Self-review and concerns

- `List` enforces `active` visibility whenever `activeOnly=true`, even if a caller passes another status filter, so public reads cannot leak draft/inactive products.
- `Update` deliberately excludes `stock` writes and documents why, keeping inventory mutations confined to the stock ledger transaction.
- `AdjustStock` locks the product row before computing the new stock and writes the product row plus ledger entry in one transaction, which prevents negative stock under concurrent decrements.
- No open functional concerns after the required verification commands.

## Task 7 review follow-up

### Additional coverage

- Added an `AdjustStock` integration assertion for a stale `Version` conflict where stock is still sufficient, verifying `product.ErrConflict` and that the failed attempt does not insert another `stock_adjustments` row.
- Added `List` integration assertions covering `activeOnly=false` with `StatusDraft`, plus `activeOnly=true` overriding conflicting `StatusDraft` and `StatusInactive` filters to return only active rows.

### TDD result

- Followed the required test-first flow for the review findings by adding the missing integration coverage and running the focused integration command before any production edits.
- The focused integration run passed immediately, so this was a coverage-only GREEN and `mysqlstore/product_store.go` did not require changes.

### Verification

`timeout 300s go test -count=1 -tags=integration ./mysqlstore -run TestProductStore -v`

```text
=== RUN   TestProductStore
=== RUN   TestProductStore/CreateLookupListAndUpdate
=== RUN   TestProductStore/ListStableSorting
=== RUN   TestProductStore/UpdateAndAdjustStockErrorMapping
=== RUN   TestProductStore/ConcurrentStockAdjustment
--- PASS: TestProductStore (22.89s)
    --- PASS: TestProductStore/CreateLookupListAndUpdate (0.07s)
    --- PASS: TestProductStore/ListStableSorting (0.03s)
    --- PASS: TestProductStore/UpdateAndAdjustStockErrorMapping (0.05s)
    --- PASS: TestProductStore/ConcurrentStockAdjustment (0.03s)
PASS
ok  	clean-arch-gin/mysqlstore	22.952s
```

`go test -count=1 ./...`

- exit 0

`go vet ./mysqlstore/...`

- exit 0
