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
