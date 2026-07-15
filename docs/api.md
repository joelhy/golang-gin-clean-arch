# API

## Envelope

All responses use a uniform envelope:

```json
{"code":0,"data":{}}
```

Failures include `message` and may include field-level `data`:

```json
{"code":10001,"message":"request validation failed","data":[{"field":"password","reason":"weak_password"}]}
```

Timestamps are UTC RFC3339/RFC3339Nano strings. Money uses integer minor units:

```json
{"amount":12345,"currency":"CNY"}
```

Paginated responses use:

```json
{"items":[],"pagination":{"limit":20,"offset":0}}
```

## Authentication

Use `Authorization: Bearer <access_token>` for authenticated routes. Order creation also requires exactly one visible-ASCII `Idempotency-Key` header, 1 to 128 bytes.

| Method | Path | Auth | Request | Success data |
|---|---|---|---|---|
| POST | `/api/v1/auth/register` | none | `{"email","name","password"}` | user object |
| POST | `/api/v1/auth/login` | none | `{"email","password"}` | token object |
| POST | `/api/v1/auth/refresh` | none | `{"refresh_token"}` | token object |
| POST | `/api/v1/auth/logout` | access token | none | omitted |

Token object:

```json
{
  "token_type": "Bearer",
  "access_token": "...",
  "refresh_token": "...",
  "expires_in": 900,
  "refresh_expires_in": 604800
}
```

User object:

```json
{
  "id": 1,
  "email": "user@example.com",
  "name": "User",
  "status": "active",
  "version": 1,
  "roles": [{"id": 1, "name": "customer"}],
  "created_at": "2026-07-15T00:00:00Z",
  "updated_at": "2026-07-15T00:00:00Z"
}
```

## Current User

| Method | Path | Auth | Request | Success data |
|---|---|---|---|---|
| GET | `/api/v1/users/me` | access token | none | user object |
| PATCH | `/api/v1/users/me` | access token | `{"name","version"}` | user object |

## Admin Users

Required permissions:

- `users:read` for list and detail.
- `users:write` for status changes.
- `users:roles` for role replacement.

| Method | Path | Permission | Request/query | Success data |
|---|---|---|---|---|
| GET | `/api/v1/admin/users` | `users:read` | `email`, `name`, `status`, `role`, `limit`, `offset`, `sort`, `direction` | paginated user objects |
| GET | `/api/v1/admin/users/:id` | `users:read` | none | user object |
| PATCH | `/api/v1/admin/users/:id/status` | `users:write` | `{"status":"active|disabled","version":1}` | omitted |
| PUT | `/api/v1/admin/users/:id/roles` | `users:roles` | `{"roles":["admin"],"version":1}` | omitted |

## Products

Product object:

```json
{
  "id": 1,
  "sku": "SKU-001",
  "name": "Product",
  "description": "Optional",
  "price": {"amount": 1000, "currency": "CNY"},
  "stock": 10,
  "status": "active",
  "version": 1,
  "created_at": "2026-07-15T00:00:00Z",
  "updated_at": "2026-07-15T00:00:00Z"
}
```

| Method | Path | Auth | Request/query | Success data |
|---|---|---|---|---|
| GET | `/api/v1/products` | none | `limit`, `offset`, `sort`, `direction` | paginated active product objects |
| GET | `/api/v1/products/:id` | none | none | active product object |

Admin product routes:

| Method | Path | Permission | Request/query | Success data |
|---|---|---|---|---|
| POST | `/api/v1/admin/products` | `products:write` | `{"sku","name","description","price":{"amount","currency"},"initial_stock":0}` | product object |
| PATCH | `/api/v1/admin/products/:id` | `products:write` | `{"sku","name","description","price":{"amount","currency"},"version":1}` | product object |
| POST | `/api/v1/admin/products/:id/stock-adjustments` | `products:stock` | `{"delta":5,"reason":"restock","version":1}` | product object |

The router intentionally does not expose admin product list/detail or publish/unpublish endpoints.

## Orders

Order object:

```json
{
  "id": 1,
  "user_id": 7,
  "number": "ORD-20260715-000001",
  "status": "pending",
  "total": {"amount": 1000, "currency": "CNY"},
  "items": [
    {
      "id": 1,
      "product_id": 1,
      "sku": "SKU-001",
      "name": "Product",
      "unit_price": {"amount": 1000, "currency": "CNY"},
      "subtotal": {"amount": 1000, "currency": "CNY"},
      "quantity": 1
    }
  ],
  "version": 1,
  "created_at": "2026-07-15T00:00:00Z",
  "updated_at": "2026-07-15T00:00:00Z"
}
```

Customer routes:

| Method | Path | Auth | Request/query | Success data |
|---|---|---|---|---|
| POST | `/api/v1/orders` | access token + `Idempotency-Key` | `{"items":[{"product_id":1,"quantity":1}]}` | order object |
| GET | `/api/v1/orders` | access token | `status`, `created_from`, `created_to`, `min_total_amount`, `max_total_amount`, `limit`, `offset`, `sort`, `direction` | paginated own orders |
| GET | `/api/v1/orders/:id` | access token | none | own order object |
| POST | `/api/v1/orders/:id/cancel` | access token | `{"version":1}` | order object |

Admin routes:

| Method | Path | Permission | Request/query | Success data |
|---|---|---|---|---|
| GET | `/api/v1/admin/orders` | `orders:read_all` | customer filters plus `user_id` | paginated order objects |
| POST | `/api/v1/admin/orders/:id/confirm` | `orders:manage` | `{"version":1}` | order object |
| POST | `/api/v1/admin/orders/:id/ship` | `orders:manage` | `{"version":1}` | order object |
| POST | `/api/v1/admin/orders/:id/deliver` | `orders:manage` | `{"version":1}` | order object |
| POST | `/api/v1/admin/orders/:id/cancel` | `orders:manage` | `{"version":1}` | order object |

## Admin Statistics

| Method | Path | Permission | Query | Success data |
|---|---|---|---|---|
| GET | `/api/v1/admin/stats` | `stats:read` | `from`, `to` UTC timestamps; range must be less than or equal to 366 days | stats snapshot |

Stats snapshot:

```json
{
  "users": {"total": 10, "active": 9, "new": 2},
  "inventory": {
    "active_products": 5,
    "units_in_stock": 100,
    "stock_value": {"amount": 12345, "currency": "CNY"},
    "currency": "CNY"
  },
  "orders": {
    "total": 3,
    "pending": 1,
    "confirmed": 1,
    "shipped": 0,
    "delivered": 1,
    "cancelled": 0,
    "gross_amount": {"amount": 3000, "currency": "CNY"},
    "currency": "CNY"
  }
}
```

## Health

| Method | Path | Auth | Success data |
|---|---|---|---|
| GET | `/health/live` | none | `{"status":"live"}` |
| GET | `/health/ready` | none | `{"status":"ready"}` or service unavailable envelope |
