# WebSocket API Specification (v1)

## Endpoint
- URL: `/ws`
- Auth: JWT passed in initial message or query string for private channels.

## Connection Flow
Client sends:
```json
{ "type": "auth", "token": "jwt" }
```
Server replies:
```json
{ "type": "auth.ok" }
```

## Subscribe
```json
{ "type": "subscribe", "channels": ["orderbook", "trades"], "symbol": "BTC-USD" }
```

## Channels
### Public
- `orderbook`: depth updates
- `trades`: recent trades
- `ticker`: best bid/ask, last price
- `candles`: OHLCV

### Private
- `orders`: user order updates
- `balances`: balance updates
- `fills`: user trade fills

## Message Examples
Order book update:
```json
{ "type": "orderbook", "symbol": "BTC-USD", "bids": [["45000", "0.5"]], "asks": [["45010", "0.3"]], "ts": "2026-02-04T12:00:01Z" }
```

User order update:
```json
{ "type": "orders", "order_id": "ord_123", "status": "filled", "filled_qty": "0.10" }
```

## Error
```json
{ "type": "error", "code": "UNAUTHORIZED", "message": "invalid token" }
```
