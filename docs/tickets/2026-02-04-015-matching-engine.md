# Ticket: Matching Engine (Kafka-Driven)

## Objective
Build a Kafka-driven matching engine for spot trading with in-memory order books and price-time priority matching.

## Scope
- Consume `orders.accepted` and `orders.cancelled` from Kafka.
- Maintain in-memory order books per symbol (BTC-USD, ETH-USD, etc.).
- Implement price-time priority matching algorithm.
- Emit `trades.executed` events to Kafka.
- gRPC control plane: `LoadSnapshot`, `HealthCheck`.
- Metrics for matching latency, order book depth, trade volume.

## Dependencies
- Kafka topics: `orders.accepted`, `orders.cancelled` (input), `trades.executed` (output).
- Postgres `orders` and `trades` tables (for snapshot loading).
- No direct database writes (event-driven).

## Database
Read-only access to `orders` table for snapshot loading via gRPC.

## Success Criteria
- Orders matched correctly with price-time priority.
- Trade events published with maker/taker order IDs, price, quantity.
- Sub-50ms matching latency for typical order flow.
- Graceful handling of order cancellations.
- Metrics exposed for monitoring.
