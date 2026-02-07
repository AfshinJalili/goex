# Fee Service

The fee service provides fee tier lookup and fee calculation over gRPC. It loads fee tiers from Postgres, caches them in memory, and exposes RPCs to calculate maker/taker fees.

## gRPC API

`FeeService`
- `GetFeeTier` – retrieve the fee tier for an account or explicit volume.
- `CalculateFees` – calculate fees for an order using maker/taker bps.

## Configuration

Config file: `services/fee/config.yaml`

Environment variables:
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_SSLMODE`
- `CEX_GRPC_HOST`, `CEX_GRPC_PORT`
- `CEX_HTTP_PORT` (metrics/health)
- `CEX_CACHE_REFRESH_INTERVAL`

## Local Development

Run directly:
```bash
CEX_CONFIG=services/fee/config.yaml go run ./services/fee/cmd/fee
```

Docker:
```bash
docker compose -f deploy/docker-compose.yml up fee
```

## Testing

```bash
go test ./services/fee/...
```

DB integration tests:
```bash
RUN_DB_INTEGRATION=1 go test ./services/fee/...
```

## Metrics

Exposed on `/metrics` (default HTTP port 8080). Key metrics:
- `fee_tier_lookups_total`
- `fee_calculations_total`
- `fee_calculation_duration_seconds`
- `cache_refresh_duration_seconds`
- `cache_size`

## Cache Refresh

The fee tier cache refreshes on a fixed interval (default `5m`).
