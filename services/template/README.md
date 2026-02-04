# Template Service

This is the standard Go service template for the CEX backend. It includes config loading, JSON logging, Prometheus metrics, and health endpoints.

## Run
```bash
make run
```

Or directly:
```bash
go run ./services/template/cmd/template
```

## Configuration
The service loads `config.yaml` by default. You can override the path with `CEX_CONFIG`.

Example `config.yaml`:
```yaml
service_name: "template-service"
env: "dev"
log_level: "info"
metrics_path: "/metrics"
http:
  host: "0.0.0.0"
  port: 8080
  read_timeout: "5s"
  write_timeout: "10s"
  idle_timeout: "60s"
```

Environment variable overrides use the `CEX_` prefix:
- `CEX_SERVICE_NAME`
- `CEX_ENV`
- `CEX_LOG_LEVEL`
- `CEX_METRICS_PATH`
- `CEX_HTTP_HOST`
- `CEX_HTTP_PORT`
- `CEX_HTTP_READ_TIMEOUT`
- `CEX_HTTP_WRITE_TIMEOUT`
- `CEX_HTTP_IDLE_TIMEOUT`

## Endpoints
- `GET /healthz` liveness
- `GET /readyz` readiness
- `GET /metrics` Prometheus metrics
