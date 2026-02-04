# Kafka Topics and Event Schemas

## Conventions
- All events include `event_id`, `event_type`, `event_version`, `timestamp`, `correlation_id`.
- Key by `symbol` for market data and by `account_id` for balances.
- Events must be idempotent; consumers store `event_id`.

## Topics
### Orders
- `orders.accepted`
- `orders.rejected`
- `orders.cancelled`

Schema (example):
```json
{
  "event_id": "evt_123",
  "event_type": "orders.accepted",
  "event_version": 1,
  "timestamp": "2026-02-04T12:00:00Z",
  "order_id": "ord_123",
  "account_id": "acct_1",
  "symbol": "BTC-USD",
  "side": "buy",
  "type": "limit",
  "price": "45000",
  "quantity": "0.10"
}
```

### Trades
- `trades.executed`
- `trades.settled`

### Ledger
- `ledger.entries`
- `balances.updated`

### Wallet
- `wallet.deposit.detected`
- `wallet.withdrawal.requested`
- `wallet.withdrawal.broadcast`
- `wallet.withdrawal.confirmed`

### Market Data
- `market.depth`
- `market.ticker`
- `market.candles`

## Partitioning Strategy
- Orders and trades: key by `symbol`.
- Balances and ledger: key by `account_id`.
- Wallet events: key by `wallet_id`.

## Versioning
- New fields are additive.
- Breaking changes require `event_version` bump and parallel topics.
