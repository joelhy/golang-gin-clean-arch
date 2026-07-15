# Error Codes

Every HTTP response uses:

```json
{"code":0,"data":{}}
```

or:

```json
{"code":10001,"message":"request validation failed","data":[{"field":"email","reason":"invalid_email"}]}
```

`code=0` means success. Nonzero codes are stable failure categories.

| Code | HTTP status | Message | Meaning |
|---:|---|---|---|
| 10000 | 400, 413 | `request body must be valid JSON`, `request body too large`, or related malformed-body text | JSON syntax, unknown field, multiple body values, empty body, type mismatch, or body-size failure |
| 10001 | 400 | `request validation failed` | Domain or query validation failed; field details may be present in `data` |
| 20000 | 401, 429 | `authentication failed` or `too many login attempts` | Missing/invalid credentials, disabled account, invalid access/refresh token, or login throttling |
| 20001 | 403 | `permission denied` or `origin not allowed` | RBAC failure, last-admin protection, forbidden domain action, or rejected CORS origin |
| 20002 | 401 | `session expired` | Expired/revoked session or refresh-token reuse |
| 30000 | 404 | `user not found` | User or role lookup failed |
| 30001 | 409 | `email already exists` | Registration or bootstrap-admin email conflict |
| 40000 | 404 | `product not found` | Product lookup failed |
| 40001 | 409 | `insufficient stock` | Product or order stock check failed |
| 50000 | 404 | `order not found` | Order lookup failed |
| 50001 | 409 | `invalid order transition` | Requested order status transition is not allowed |
| 50002 | 409 | `conflict` | Optimistic version conflict or idempotency-key conflict |
| 90000 | 404, 405, 499, 500, 503 | `route not found`, `method not allowed`, `request canceled`, `internal server error`, or `service unavailable` | Transport fallback, cancellation, readiness failure, or unexpected internal error |

Validation reasons include `invalid_email`, `invalid_name`, `weak_password`, `invalid_sku`, `invalid_price`, `invalid_currency`, `invalid_stock`, `invalid_status`, `invalid_stock_adjustment`, `invalid_order`, `invalid_money`, and `invalid_filter`.
