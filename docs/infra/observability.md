# Observability

## Metrics
- Prometheus scraping all services.
- HTTP metrics (standard on all services):
  - `http_requests_total{method,path,status}`
  - `http_request_duration_seconds{method,path,status}`
- Key business metrics: orders/sec, match latency, settlement latency, Kafka lag, DB latency.
- Grafana dashboards per service and per market (described below).

## Logging
- Structured JSON logs.
- Correlation IDs propagated end-to-end (`X-Request-ID`).
- Optional `traceparent` logged when present.
- Central aggregation (e.g., Loki/ELK).

## Tracing
- OpenTelemetry instrumentation.
- OTLP/HTTP exporter (set `OTEL_EXPORTER_OTLP_ENDPOINT`).
- Trace propagation via headers and Kafka metadata.

## Alerting
- SLO-based alerts (latency, error rate, Kafka lag).
- DB replication lag and disk usage alerts.
- Security alerts for auth anomalies and AML triggers.

## Suggested Dashboards (Markdown Templates)
- **Service Overview**: requests/sec, error rate, p50/p95 latency, CPU/memory.
- **Auth**: login success/failure, rate-limit hits.
- **User**: request rates, 4xx/5xx counts.
- **Data**: DB connection counts, query latency, replication lag.

## Suggested Alerts (Baseline)
- High error rate: `5xx > 1% for 5m`
- Latency spike: `p95 > 300ms for 5m`
- Kafka lag: `> 10k messages for 10m`
- DB disk usage: `> 80%`

## How to Verify
- `curl http://localhost:8080/metrics` and confirm HTTP metrics exist.
- Trigger requests and confirm logs include `request_id` and `traceparent` if provided.
- Set `OTEL_EXPORTER_OTLP_ENDPOINT` and verify spans are exported.
