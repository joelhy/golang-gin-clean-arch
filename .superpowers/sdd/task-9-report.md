# Task 9 Report: Transactional MySQL order adapter

## Status

DONE_WITH_CONCERNS

## Requirements source

- `/data/workspace/open-source/golang-gin-clean-arch/.superpowers/sdd/task-9-brief.md`

## What I implemented

### Production code

- Added `mysqlstore/order_store.go`.
- Implemented `order.Transactor`, `order.Tx`, and `order.Reader` in `OrderStore`.
- Implemented transactional `WithinTransaction` with whole-callback retry on MySQL deadlock `1213` and lock wait timeout `1205`, capped at three retries with bounded context-aware jitter.
- Implemented idempotency claim flow using insert-first semantics against `(user_id, operation, idempotency_key)`, with duplicate-key fallback to `SELECT ... FOR UPDATE`, request-hash comparison, and replay only when `order_id` is already present.
- Implemented deterministic product locking in sorted product-ID order with `FOR UPDATE`.
- Implemented stock decrement/increment updates with explicit `id + version + stock` guards.
- Implemented stock ledger persistence tied to the order row inside the same transaction.
- Implemented order creation, item insertion, idempotency completion, order lock/load, status update, `ByID`, and `List`.
- Implemented deterministic query mapping:
  - order items always loaded with `ORDER BY order_items.id ASC`
  - list sorting supports domain filter fields
  - non-`id` sorts add `orders.id ASC` as a stable secondary order
  - returned domain objects never expose persistence model pointers

### Test code

- Added `mysqlstore/order_store_test.go`.
- Wrote integration coverage for:
  - successful checkout
  - server-side price snapshot
  - unique order number conflict
  - insufficient stock rollback
  - idempotent replay
  - same-key different-payload conflict
  - concurrent same-key checkout
  - concurrent last-stock checkout
  - cancellation stock restoration
  - version conflict on transition
  - compound filters
  - deterministic order/item ordering

## TDD evidence

### RED

I wrote the integration tests first, then ran:

```text
go test -tags=integration ./mysqlstore -run TestOrderStore -v
```

Initial RED output:

```text
# clean-arch-gin/mysqlstore [clean-arch-gin/mysqlstore.test]
mysqlstore/order_store_test.go:555:49: undefined: OrderStore
mysqlstore/order_store_test.go:557:16: undefined: NewOrderStore
FAIL    clean-arch-gin/mysqlstore [build failed]
FAIL
```

This was the expected missing-adapter failure.

### GREEN

After implementing `OrderStore`, formatting, and fixing one test assumption around post-checkout product version, the required verification commands passed.

## Commands run and results

### Formatting

```text
gofmt -w mysqlstore
```

Result: PASS

### Order race tests

```text
go test ./order -race
```

Result:

```text
ok  	clean-arch-gin/order	1.028s
```

### Integration compile smoke

```text
go test -run '^$' -tags=integration ./mysqlstore
```

Result:

```text
ok  	clean-arch-gin/mysqlstore	0.085s [no tests to run]
```

### Full Task 9 integration suite

```text
go test -tags=integration ./mysqlstore -run TestOrderStore -v
```

Final GREEN result:

```text
--- PASS: TestOrderStore (248.68s)
    --- PASS: TestOrderStore/CheckoutPersistsServerSnapshotAndReplay (29.91s)
    --- PASS: TestOrderStore/UniqueOrderNumberConflict (29.41s)
    --- PASS: TestOrderStore/InsufficientStockRollsBack (32.69s)
    --- PASS: TestOrderStore/DifferentPayloadConflict (24.23s)
    --- PASS: TestOrderStore/ConcurrentSameKeyCreatesOneOrder (23.09s)
    --- PASS: TestOrderStore/ConcurrentLastStockOneWinner (26.39s)
    --- PASS: TestOrderStore/CancellationRestoresStock (38.54s)
    --- PASS: TestOrderStore/TransitionVersionConflict (19.61s)
    --- PASS: TestOrderStore/ListSupportsCompoundFiltersAndDeterministicOrdering (24.81s)
PASS
ok  	clean-arch-gin/mysqlstore	248.743s
```

### Vet

```text
go vet ./mysqlstore/...
```

Result: PASS

## Files changed

- `mysqlstore/order_store.go`
- `mysqlstore/order_store_test.go`
- `.superpowers/sdd/task-9-report.md`

## Notes on key design choices

- Transaction retry is intentionally narrow: only MySQL `1213` and `1205` cause a retry, and every retry re-enters the callback inside a fresh transaction.
- Duplicate idempotency claims lock the stored row before comparing request hashes so concurrent same-key callers cannot race between hash comparison and completion visibility.
- Cancellation locks the order first, then lazily locks all affected products in sorted product-ID order before any stock restoration to keep lock ordering deterministic.
- Stock ledger rows are emitted only after the order row exists so `stock_adjustments.order_id` can be populated consistently.

## Self-review findings

- No failing tests remain in the required command set.
- Comments were added around:
  - retry semantics
  - duplicate idempotency locking
  - stable list ordering
  - delayed ledger insertion after order creation
- I did not modify `order`, `web`, `cmd`, `docs`, or `internal`.

## Concerns / blockers

1. The current order port does not provide the cancelling actor identity to persistence during `Cancel`, only the locked order data. Because `stock_adjustments.actor_user_id` is required, cancellation ledger rows are currently attributed to the order owner (`order.UserID`) rather than the admin actor when an admin performs the cancellation.
2. `TestOrderStore` is accurate but expensive because each subtest provisions a fresh MySQL container via `newTestDB(t)`. This is correct for isolation and matched existing repo style, but the suite takes about 249 seconds.

