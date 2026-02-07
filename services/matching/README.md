# Matching Engine Service

## Overview
The matching engine consumes order events from Kafka, maintains in-memory order books per symbol, and emits `trades.executed` events using price-time priority matching. It is designed for low-latency, deterministic matching with a control-plane gRPC API for snapshots and health.

## Architecture
- **Input**: Kafka topics `orders.accepted`, `orders.cancelled`
- **Output**: Kafka topic `trades.executed`
- **State**: In-memory order books per symbol
- **Control Plane**: gRPC `LoadSnapshot`, `HealthCheck`
- **Read-only DB**: Snapshot loading from Postgres `orders` table

## gRPC API
```
service MatchingEngine {
  rpc LoadSnapshot(LoadSnapshotRequest) returns (LoadSnapshotResponse);
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
}
```

## Metrics
- `matching_orders_processed_total`
- `matching_trades_executed_total`
- `matching_latency_seconds`
- `matching_orderbook_depth`
- `matching_orderbook_spread`

## Configuration
- `config.yaml` supports Kafka, Postgres, gRPC and HTTP ports.
- Environment overrides use `CEX_` prefix (e.g. `CEX_KAFKA_BROKERS`).

## Running
```
make run-matching
```

Or via Docker Compose:
```
docker compose -f deploy/docker-compose.yml up -d matching
```

## Testing
```
make test-matching
```

Integration tests:
```
RUN_INTEGRATION=1 go test ./services/integration -run MatchingEngine
```
