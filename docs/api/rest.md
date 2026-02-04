# REST API Specification (v1)

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
### POST `/api-keys`
Request:
```json
{ "label": "trading-bot", "scopes": ["trade", "read"], "ip_whitelist": ["1.2.3.4"] }
```

### DELETE `/api-keys/{key_id}`

## User & Account
### GET `/me`
### GET `/accounts`
### GET `/balances`

## Orders
### POST `/orders`
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
  "received_at": "2026-02-04T12:00:00Z"
}
```

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
