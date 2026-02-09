# REST API Specification (v1)

## OpenAPI Specification
A machine-readable OpenAPI 3.0 specification is available at `docs/api/openapi.yaml`

To view interactive documentation locally, run: `make docs` or `./scripts/openapi-serve.sh`

To validate the spec, run: `make docs-validate`

The docs server will be available at http://localhost:8080

## Conventions
- Base URL: `/api/v1`
- Auth: JWT for user sessions; HMAC API keys for trading endpoints.
- Idempotency: use `Idempotency-Key` header for order/withdrawal creation.
- Pagination: `limit` and `cursor` (opaque).
- Timestamps: ISO-8601 UTC.

## Authentication
### POST `/auth/login`
Request:
```json
{ "email": "user@example.com", "password": "...", "mfa_code": "123456" }
```
Response:
```json
{ "access_token": "...", "refresh_token": "...", "expires_in": 3600 }
```

### POST `/auth/refresh`
Request:
```json
{ "refresh_token": "..." }
```

### POST `/auth/logout`

## API Keys
### GET `/api-keys`
Response:
```json
{
  "keys": [
    {
      "id": "uuid",
      "prefix": "ab12cd34",
      "scopes": ["read", "trade"],
      "ip_whitelist": ["203.0.113.1"],
      "last_used_at": "2026-02-04T12:00:00Z",
      "revoked_at": null,
      "created_at": "2026-02-04T12:00:00Z"
    }
  ]
}
```
### POST `/api-keys`
Request (label is optional):
```json
{ "label": "trading-bot", "scopes": ["trade", "read"], "ip_whitelist": ["1.2.3.4"] }
```
Response:
```json
{
  "id": "uuid",
  "prefix": "ab12cd34",
  "secret": "ck_dev_ab12cd34.<secret>",
  "scopes": ["trade", "read"],
  "ip_whitelist": ["1.2.3.4"],
  "created_at": "2026-02-04T12:00:00Z"
}
```

### DELETE `/api-keys/{key_id}`
Response:
```json
{ "status": "revoked" }
```

## User & Account
### GET `/me`
Response:
```json
{
  "id": "uuid",
  "email": "user@example.com",
  "status": "active",
  "kyc_level": "basic",
  "mfa_enabled": false,
  "created_at": "2026-02-04T12:00:00Z"
}
```
### GET `/accounts`
Query params: `limit`, `cursor`

Response:
```json
{
  "accounts": [
    { "id": "uuid", "type": "spot", "status": "active", "created_at": "2026-02-04T12:00:00Z" }
  ],
  "next_cursor": "base64(ts|id)"
}
```
### GET `/balances`
Query params: `limit`, `cursor`

Response:
```json
{
  "balances": [
    { "account_id": "uuid", "asset": "BTC", "available": "0.5", "locked": "0.1", "updated_at": "2026-02-04T12:00:00Z" }
  ],
  "next_cursor": "base64(ts|id)"
}
```

## Orders
### POST `/orders`
Idempotency: supply `Idempotency-Key` header to dedupe order creation. Header value takes precedence over `client_order_id` in the body.
Request:
```json
{
  "symbol": "BTC-USD",
  "side": "buy",
  "type": "limit",
  "price": "45000.00",
  "quantity": "0.10",
  "time_in_force": "GTC"
}
```
Response:
```json
  {
    "order_id": "ord_123",
    "status": "accepted",
    "created_at": "2026-02-04T12:00:00Z"
  }
```
Notes:
- For `market` orders, `price` is optional and ignored by the matching engine.
- For market buys, the system reserves quote using a reference price plus a slippage cap; orders may be rejected if a reference price is unavailable or funds are insufficient.

### DELETE `/orders/{order_id}`
### GET `/orders`
Filters: `symbol`, `status`, `from`, `to`, `cursor`, `limit`

## Trades
### GET `/trades`
Filters: `symbol`, `from`, `to`, `cursor`, `limit`

## Markets
### GET `/markets`
### GET `/markets/{symbol}/orderbook`
### GET `/markets/{symbol}/ticker`
### GET `/markets/{symbol}/candles`

## Deposits / Withdrawals
### GET `/deposits`
### GET `/withdrawals`
### POST `/withdrawals`
Request:
```json
{ "asset": "BTC", "address": "bc1...", "amount": "0.01", "network": "bitcoin" }
```

## Fees
### GET `/fees`
Returns current maker/taker fees and tier info.

## Error Codes
- `INVALID_REQUEST`
- `UNAUTHORIZED`
- `FORBIDDEN`
- `RATE_LIMITED`
- `INSUFFICIENT_BALANCE`
- `ORDER_NOT_FOUND`
- `SYMBOL_HALTED`
- `INTERNAL_ERROR`

## Rate Limits
- Global: per IP and per API key.
- Trading: strict per key (orders/sec, cancels/sec).
