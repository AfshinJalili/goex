# goex

Event-driven trading platform with gRPC services, Kafka event flow, and PostgreSQL persistence.

## Architecture
```mermaid
sequenceDiagram
    participant Client
    participant Gateway
    participant OrderIngest
    participant Risk
    participant Ledger
    participant Kafka
    participant Matching

    Client->>Gateway: POST /orders
    Gateway->>OrderIngest: Submit order
    OrderIngest->>Risk: PreTradeCheck (gRPC)
    Risk-->>OrderIngest: Allowed/Denied
    OrderIngest->>Ledger: ReserveBalance (gRPC)
    Ledger-->>OrderIngest: Reserved
    OrderIngest->>Kafka: orders.accepted
    Kafka->>Matching: orders.accepted
    Matching->>Kafka: trades.executed
    Kafka->>Ledger: trades.executed
    Ledger->>Kafka: ledger.entries, balances.updated
```

## Local Development
1. `make dev-start`
2. `make dev-verify`
3. `make dev-test`

## Tests
Run all tests:
```bash
./scripts/test-full.sh
```
