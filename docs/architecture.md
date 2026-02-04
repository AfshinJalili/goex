# Production-Grade CEX Backend Architecture

## Summary
This document defines a production-grade centralized exchange (CEX) backend architecture for spot trading. It provides service boundaries, data ownership, workflows, interfaces, and operational posture. The system is designed for startup scale (up to ~1k orders/sec, <50ms end-to-end) with a path to horizontal scaling.

## Goals
- Ultra-low-latency matching for spot markets.
- Correct, auditable double-entry ledger for all balance changes.
- Strong security posture with KYC/AML hooks, RBAC, MFA, and immutable audit trails.
- Modular microservices with Kafka event backbone and clear data ownership.
- Production-ready observability, rate limiting, and resilience.

## Non-Goals
- Margin, futures, or options trading in v1.
- Self-custodied wallets (external custody provider for v1).
- Sub-millisecond matching latencies at very high scale.

## Key Decisions (Locked)
- Language: Go for all services, matching engine optimized in Go.
- Architecture: Microservices with Kafka for async event streaming.
- Deployment: Kubernetes with standard gateway and ingress.
- Data: Postgres (core), Redis (cache/session/pubsub), TimescaleDB (time-series).
- Custody: External custody provider integration.
- Scope: Spot markets only.
- Compliance: Production-grade auditability and KYC/AML hooks.

## System Context
External actors:
- Retail and institutional clients (REST and WebSocket).
- External custody provider (wallet signing, broadcast, status webhooks).
- KYC/AML providers (verification and sanctions screening).
- Admin and compliance operators (internal tools).

Trust boundaries:
- Public edge: API Gateway + WAF + rate limits.
- Core services: internal Kubernetes network + mTLS (service-to-service).
- Data plane: Postgres, Redis, TimescaleDB, Kafka.
- External services: custody provider, KYC/AML providers.

## Service Topology and Responsibilities
Core services:
- API Gateway: auth, routing, rate limits, request validation, DDoS protection.
- User Service: users, auth sessions, KYC status, API keys, RBAC.
- Order Ingest Service: validates orders, applies risk rules, publishes to Kafka.
- Matching Engine: pure matching logic, price-time priority, emits fills.
- Ledger Service: double-entry ledger, balance snapshots, idempotent settlement.
- Fee Service: computes maker/taker fees, fee tiers, discounts.
- Market Data Service: order book depth, tickers, candles, WebSocket fan-out.
- Wallet Service (Adapter): custody provider integration for deposits/withdrawals.
- Compliance Service: AML rules, alerts, audit trail, suspicious activity.

Support services:
- Risk Service: order-level checks (limits, trading status, account flags).
- Admin Service: internal tooling, configuration, market controls.
- Notification Service: email/SMS alerts, withdrawal confirmations.

## Data Ownership
- User Service owns user identities, KYC flags, RBAC, API keys.
- Order Service owns order records and state transitions.
- Matching Engine owns in-memory order books and matching state.
- Ledger Service owns balances and immutable ledger entries.
- Wallet Service owns deposit/withdrawal lifecycle state.
- Market Data Service owns derived market data snapshots.

## Core Workflows
### Order Lifecycle
1. Client submits order via REST.
2. API Gateway authenticates and rate limits.
3. Order Ingest validates (format, balance, risk) and creates order record.
4. Order Ingest publishes `orders.accepted` to Kafka.
5. Matching Engine consumes order events, matches in memory, emits `trades.executed`.
6. Ledger Service consumes trade events, applies atomic settlement (buyer/seller + fees).
7. Market Data Service updates order book and emits WebSocket updates.

### Deposit Flow
1. Custody provider notifies a deposit webhook.
2. Wallet Service verifies and writes `wallet.deposit.detected` event.
3. Ledger Service credits user balance with idempotency key.
4. Compliance Service updates AML risk profile and audit log.

### Withdrawal Flow
1. Client requests withdrawal.
2. Order Ingest validates balance and risk.
3. Wallet Service creates custody provider withdrawal request.
4. Provider signs and broadcasts; status updates flow via webhook.
5. Ledger Service debits on `wallet.withdrawal.broadcast` and finalizes on `wallet.withdrawal.confirmed`.

## Data Model Overview
- Postgres: users, accounts, orders, trades, ledger entries, fees, audit logs.
- Redis: sessions, rate limits, hot market snapshots, WebSocket fan-out.
- TimescaleDB: ticks, candles, market metrics.

See detailed schemas:
- `docs/data/postgres-schema.md`
- `docs/data/timescale-schema.md`

## Events and Streams
Kafka topics include:
- `orders.accepted`, `orders.rejected`, `orders.cancelled`
- `trades.executed`, `trades.settled`
- `ledger.entries`, `balances.updated`
- `wallet.deposit.detected`, `wallet.withdrawal.requested`, `wallet.withdrawal.broadcast`, `wallet.withdrawal.confirmed`
- `market.depth`, `market.ticker`, `market.candles`

See `docs/events/kafka-topics.md`.

## Security and Compliance
- JWT/OAuth2 for auth, rotating API keys.
- RBAC for admin tools, MFA for withdrawals and configuration.
- KYC/AML hooks with provider integration and internal screening rules.
- Immutable audit logs for all critical actions.
- HSM or custody provider for key management and signing.

See `docs/infra/observability.md` and `docs/infra/k8s-deployment.md`.

## Observability
- Metrics: Prometheus + Grafana.
- Logs: structured JSON, central aggregation.
- Tracing: OpenTelemetry with end-to-end trace IDs.

## Scalability
- Market sharding across multiple matching engines.
- Kafka topic partitioning by symbol.
- Stateless services scaled horizontally in Kubernetes.
- Postgres read replicas and partitioning.

## Failure Handling
- Idempotent event processing with deduplication keys.
- Circuit breakers for external dependencies.
- Graceful degradation for market data services.
- DR plan with PITR for Postgres.

## Service Template
The repo includes a standard Go service template at `services/template/` with config loading, structured logging, metrics, and health endpoints. New services should start from this template to maintain consistent operational behavior.

## References
- Diagrams: `docs/diagrams/*.mmd`
- APIs: `docs/api/*.md`
- Events: `docs/events/kafka-topics.md`
- Data: `docs/data/*.md`
- Infra: `docs/infra/*.md`
- Roadmap: `docs/roadmap.md`
