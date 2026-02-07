# Ticket: 2026-02-04-014 - Order Ingest Service

## Objective
Build REST/Kafka order ingest service for order lifecycle management.

## Scope
- POST/GET/DELETE `/orders` endpoints
- Kafka event publishing/consumption
- Risk validation
- Idempotency

## Dependencies
- Risk service (gRPC)
- Ledger service (gRPC for balance checks)
- Kafka topics (`orders.accepted`, `orders.rejected`, `orders.cancelled`, `trades.executed`)

## Database
Uses existing `orders` table from migration `0010_create_orders.up.sql`.

## Success Criteria
- Orders can be submitted, validated, persisted, and published to Kafka.
- Status updates from trade events are applied.
- Proper error handling and audit logging.
