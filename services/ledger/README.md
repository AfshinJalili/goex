# Ledger Service

The ledger service applies settlements for executed trades and exposes balances over gRPC. It consumes `trades.executed` events from Kafka, applies double-entry accounting, and publishes `ledger.entries` and `balances.updated` events.

## gRPC API

`Ledger`
- `GetBalance` – retrieve available/locked balances for an account + asset.
- `ApplySettlement` – apply trade settlement (used internally and for tests).

## Configuration

Config file: `services/ledger/config.yaml`

Environment variables:
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_SSLMODE`
- `CEX_GRPC_HOST`, `CEX_GRPC_PORT`
- `CEX_HTTP_PORT` (metrics/health)
- `KAFKA_BROKERS`, `KAFKA_CONSUMER_GROUP`, `KAFKA_TRADES_TOPIC`
- `FEE_SERVICE_ADDR`

## Local Development

Run directly:
```bash
CEX_CONFIG=services/ledger/config.yaml go run ./services/ledger/cmd/ledger
```

Docker:
```bash
docker compose -f deploy/docker-compose.yml up ledger
```

## Testing

```bash
go test ./services/ledger/...
```

DB integration tests:
```bash
RUN_DB_INTEGRATION=1 go test ./services/ledger/...
```

## Events

Consumes:
- `trades.executed`

Publishes:
- `ledger.entries`
- `balances.updated`

## Metrics

Exposed on `/metrics` (default HTTP port 8080). Includes:
- `ledger_balance_lookups_total`
- `ledger_settlements_total`
- `ledger_settlement_errors_total`
- `ledger_settlement_duration_seconds`
- `kafka_publish_total`
- `kafka_publish_latency_seconds`
