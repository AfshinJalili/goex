# Observability

## Metrics
- Prometheus scraping all services.
- Key metrics: orders/sec, match latency, settlement latency, Kafka lag, DB latency.
- Grafana dashboards per service and per market.

## Logging
- Structured JSON logs.
- Correlation IDs propagated end-to-end.
- Central aggregation (e.g., Loki/ELK).

## Tracing
- OpenTelemetry instrumentation.
- Trace propagation via headers and Kafka metadata.

## Alerting
- SLO-based alerts (latency, error rate, Kafka lag).
- DB replication lag and disk usage alerts.
- Security alerts for auth anomalies and AML triggers.
