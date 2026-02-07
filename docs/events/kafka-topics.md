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

Schema (ledger.entries example):
```json
{
  "event_id": "evt_456",
  "event_type": "ledger.entries",
  "event_version": 1,
  "timestamp": "2026-02-07T12:00:00Z",
  "correlation_id": "evt_trade_1",
  "trade_id": "trade_123",
  "account_id": "acct_1",
  "entries": [
    {
      "entry_id": "entry_1",
      "asset": "USD",
      "entry_type": "debit",
      "amount": "100.00",
      "reference_id": "trade_123",
      "created_at": "2026-02-07T12:00:00Z"
    }
  ]
}
```

Schema (balances.updated example):
```json
{
  "event_id": "evt_457",
  "event_type": "balances.updated",
  "event_version": 1,
  "timestamp": "2026-02-07T12:00:00Z",
  "correlation_id": "evt_trade_1",
  "trade_id": "trade_123",
  "account_id": "acct_1",
  "balances": [
    {
      "asset": "USD",
      "available": "900.00",
      "locked": "0",
      "updated_at": "2026-02-07T12:00:00Z"
    }
  ]
}
```

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
