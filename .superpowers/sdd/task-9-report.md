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

## Task 9 review-finding fix: stock ledger actor attribution

### What changed

- Added `ActorUserID` to the consumer-owned `order.StockChange` value so stock movement audit identity crosses the domain port without leaking MySQL details into `order`.
- Updated checkout stock deductions in `order.Service.Create` to carry the customer user ID on each `DecreaseStock` call.
- Updated cancellation stock restoration in `order.Service.Cancel` to carry the cancelling actor’s user ID through `IncreaseStock`, including admin cancellation of another user’s order.
- Updated `mysqlstore/order_store.go` to persist actor attribution per pending ledger entry instead of inferring cancellation actor from the order owner during ledger flush.
- Extended unit and integration coverage so:
  - checkout deductions record the customer actor
  - owner cancellation records the owner actor
  - admin cancellation records the admin actor in `stock_adjustments.actor_user_id`

### RED evidence

I wrote the failing tests first, then ran the scoped RED commands before changing production code.

Command:

```text
go test ./order -run 'TestCreate|TestServiceCancel' -v
```

Output:

```text
# clean-arch-gin/order [clean-arch-gin/order.test]
order/service_test.go:252:29: unknown field ActorUserID in struct literal of type StockChange
order/service_test.go:253:29: unknown field ActorUserID in struct literal of type StockChange
order/service_test.go:451:29: unknown field ActorUserID in struct literal of type StockChange
order/service_test.go:452:29: unknown field ActorUserID in struct literal of type StockChange
order/service_test.go:483:29: unknown field ActorUserID in struct literal of type StockChange
order/service_test.go:484:29: unknown field ActorUserID in struct literal of type StockChange
FAIL    clean-arch-gin/order [build failed]
FAIL
```

Command:

```text
go test -tags=integration ./mysqlstore -run 'TestOrderStore/CancellationRestoresStock' -v
```

Output:

```text
--- FAIL: TestOrderStore (28.20s)
    --- FAIL: TestOrderStore/CancellationRestoresStock (28.20s)
        order_store_test.go:389: product 1 latest stock adjustment actor_user_id = 1, want 2
FAIL
FAIL    clean-arch-gin/mysqlstore    28.264s
FAIL
```

### GREEN verification

Command:

```text
go test ./order -run 'TestCreate|TestServiceCancel' -v
```

Output:

```text
=== RUN   TestServiceCancelEnforcesPermissionsAndRestoresInventory
=== RUN   TestServiceCancelEnforcesPermissionsAndRestoresInventory/owner_can_cancel_and_restore_stock
=== RUN   TestServiceCancelEnforcesPermissionsAndRestoresInventory/non_owner_is_forbidden
=== RUN   TestServiceCancelEnforcesPermissionsAndRestoresInventory/admin_can_cancel_any_order
--- PASS: TestServiceCancelEnforcesPermissionsAndRestoresInventory (0.00s)
    --- PASS: TestServiceCancelEnforcesPermissionsAndRestoresInventory/owner_can_cancel_and_restore_stock (0.00s)
    --- PASS: TestServiceCancelEnforcesPermissionsAndRestoresInventory/non_owner_is_forbidden (0.00s)
    --- PASS: TestServiceCancelEnforcesPermissionsAndRestoresInventory/admin_can_cancel_any_order (0.00s)
PASS
ok      clean-arch-gin/order    0.006s
```

Command:

```text
go test ./order -race
```

Output:

```text
ok      clean-arch-gin/order    1.030s
```

Command:

```text
go test -run '^$' -tags=integration ./mysqlstore
```

Output:

## Task 9 re-review finding 2 fix: cancellation lock path ignores active status

### What changed

- Kept checkout product locking on the active-only path.
- Added a cancellation-specific product lock path in `mysqlstore/order_store.go` that locks products by ID without filtering on product status, while preserving the existing deterministic ID ordering.
- Updated cancellation stock restoration to call the cancellation-specific lock path instead of reusing the checkout-only lock.
- Extended `mysqlstore/order_store_test.go` so `CancellationRestoresStock` now:
  - creates an order
  - marks the purchased products inactive after checkout
  - cancels the order successfully
  - verifies stock is restored and stock adjustment rows are written

### RED evidence

I updated the regression first, then ran the required focused test before changing production code.

Command:

```text
go test -tags=integration ./mysqlstore -run 'TestOrderStore/CancellationRestoresStock' -v
```

Output:

```text
--- FAIL: TestOrderStore (35.58s)
    --- FAIL: TestOrderStore/CancellationRestoresStock (35.58s)
        order_store_test.go:394: Cancel() error = cancel order 1: restore stock for product 1: not found
FAIL
FAIL    clean-arch-gin/mysqlstore  35.649s
FAIL
```

### GREEN evidence

After the adapter change, the same regression passed.

Command:

```text
go test -tags=integration ./mysqlstore -run 'TestOrderStore/CancellationRestoresStock' -v
```

Output:

```text
--- PASS: TestOrderStore (36.93s)
    --- PASS: TestOrderStore/CancellationRestoresStock (36.93s)
PASS
ok      clean-arch-gin/mysqlstore  36.993s
```

### Commands and results

- `go test -tags=integration ./mysqlstore -run 'TestOrderStore/CancellationRestoresStock' -v` - PASS
- `go test -run '^$' -tags=integration ./mysqlstore` - PASS
- `go vet ./mysqlstore/...` - PASS

### Files changed

- `mysqlstore/order_store.go`
- `mysqlstore/order_store_test.go`
- `.superpowers/sdd/task-9-report.md`

### Self-review

- Checkout still uses the active-only product lock path.
- Cancellation now uses a status-agnostic product lock path and keeps product IDs ordered before locking.
- The regression covers the inactive-product cancellation case and verifies ledger persistence.
- No changes were made outside the MySQL order adapter and its integration test.

### Concerns

- None.

```text
ok      clean-arch-gin/mysqlstore    0.056s [no tests to run]
```

Command:

```text
go test -tags=integration ./mysqlstore -run 'TestOrderStore/CancellationRestoresStock' -v
```

Output:

```text
--- PASS: TestOrderStore (23.55s)
    --- PASS: TestOrderStore/CancellationRestoresStock (23.55s)
PASS
ok      clean-arch-gin/mysqlstore    23.615s
```

Command:

```text
go vet ./order ./mysqlstore/...
```

Output:

```text
PASS
```

### Files changed

- `order/types.go`
- `order/service.go`
- `order/service_test.go`
- `mysqlstore/order_store.go`
- `mysqlstore/order_store_test.go`
- `.superpowers/sdd/task-9-report.md`

### Self-review

- The fix stays at the consumer-owned port boundary by extending `order.StockChange` instead of introducing storage-specific actor plumbing into the `order` package.
- `mysqlstore` now persists the actor that came with each stock movement, which removes the previous cancellation fallback to `order.UserID`.
- Comments were preserved; no new comments were added beyond the existing audit/transaction notes because the new data flow is direct in code.

### Concerns

- Minor follow-up only: retry behavior for MySQL deadlock `1213` and lock wait timeout `1205` still lacks focused coverage. I did not expand that here because this fix did not naturally create a low-cost seam, and the review finding was specifically about actor attribution.
