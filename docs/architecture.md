# Architecture

## Package Boundaries

The project is a modular monolith organized by domain and adapter boundary:

```text
cmd/server     HTTP server entry point, signals, listener, graceful shutdown, Wire runtime graph
cmd/admin      migration and bootstrap-admin CLI commands
config         strict APP_* environment loading and validation
user           accounts, credentials, sessions, RBAC, permissions
product        catalog, pricing, stock rules
order          order state machine, idempotency, inventory transaction policy
reporting      read-only admin statistics model
security       Argon2id, JWT, refresh token, random ID implementations
mysqlstore     GORM models, GORM Gen query code, MySQL persistence adapters
migrations     embedded versioned SQL migrations
web            Gin handlers, DTOs, middleware, response envelope, error mapping
```

Domain packages (`user`, `product`, `order`, `reporting`) do not import Gin, GORM, the MySQL driver, JWT, Cobra, Viper, or CLI packages. They define the ports they consume. Adapter packages implement those ports.

`cmd/server` and `cmd/admin` are composition boundaries. They wire concrete adapters and services together but do not contain business rules.

## Dependency Direction

Dependencies point inward:

```text
cmd -> web/mysqlstore/security/config/migrations
web -> user/product/order/reporting/config
mysqlstore -> user/product/order/reporting/config/migrations
security -> user
domain packages -> standard library only, plus narrow domain peers where explicitly modeled
```

Wire connects constructors and commits generated `wire_gen.go` files. `wire.go` files are build-tagged with `wireinject` and are not part of normal builds.

## Runtime Lifecycle

`cmd/server` loads configuration once, reserves the listener, opens the database pool, builds services and router, then serves through `http.Server`.

The server uses configured read-header, read, write, idle, and shutdown timeouts. On signal cancellation it creates a fresh shutdown context instead of reusing the canceled parent context, so in-flight requests get the configured drain window. Startup failures after the DB opens close the DB before returning; normal serving exits close runtime resources through `serverRuntime.Closer`.

## Transaction Flow

Order creation is coordinated by the `order` service and persisted by `mysqlstore` in one MySQL transaction:

1. Claim or validate the idempotency key.
2. Lock requested products in stable product ID order.
3. Validate active products, currency, quantity, and stock.
4. Calculate order items from server-side price snapshots.
5. Decrease stock and write stock-adjustment ledger rows.
6. Insert order and order items.
7. Complete the idempotency record with the created order ID.
8. Commit.

Order cancellation follows the same transaction boundary when stock must be restored. The adapter owns SQL locking and persistence; the domain service owns status and permission rules.

## Security Model

Access tokens carry identity and session IDs, not role snapshots. Protected routes authenticate the session and load current permissions before authorizing admin actions. Handler-level permission evidence fails closed if route wiring omits or misorders permission middleware.

Refresh tokens are one-time bearer secrets. The database stores only digests and revokes the session family on reuse detection.
