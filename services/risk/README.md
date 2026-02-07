# Risk Service

The Risk service performs pre-trade validation for incoming orders. It validates account/user status, market availability, and balance sufficiency by querying Postgres and calling the Ledger service.

## gRPC API
```
service Risk {
  rpc PreTradeCheck(PreTradeCheckRequest) returns (PreTradeCheckResponse);
}
```

**PreTradeCheckRequest**
- `account_id`
- `symbol`
- `side` (buy/sell)
- `order_type` (limit/market)
- `quantity`
- `price`

**PreTradeCheckResponse**
- `allowed` (bool)
- `reasons` (list of denial reasons)
- `details` (optional metadata)

Denial reasons include:
- `account_inactive`
- `kyc_insufficient`
- `market_not_found`
- `market_inactive`
- `insufficient_balance`

## Configuration
See `config.yaml` for defaults. Environment variables:
- `POSTGRES_*`
- `CEX_GRPC_HOST`, `CEX_GRPC_PORT`
- `LEDGER_SERVICE_ADDR`
- `CEX_CACHE_REFRESH_INTERVAL`

## Local Development
```
go run ./services/risk/cmd/risk
```

## Docker
```
docker compose -f deploy/docker-compose.yml up risk
```

## Testing
```
go test ./services/risk/...
RUN_DB_INTEGRATION=1 go test ./services/risk/...
```

## Metrics
- `risk_pre_trade_checks_total`
- `risk_pre_trade_check_duration_seconds`
- `risk_cache_hits_total`
- `risk_cache_refresh_duration_seconds`
- `risk_cache_size`

## Cache Behavior
The market cache is loaded on startup and refreshed at the configured interval.
