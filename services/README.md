# Services Testing Guide

## Running Tests

### Unit Tests
Run unit tests (fast, no external dependencies):
```bash
make test-unit
```

### DB Integration Tests (Service-Level)
Integration tests require a running Postgres with seed data.

1. Start infrastructure and seed:
```bash
make dev-reset
```

2. Run DB integration tests:
```bash
make test-db
```

Or manually:
```bash
RUN_DB_INTEGRATION=1 go test ./services/auth/... ./services/user/...
```

### Gateway E2E Tests
Gateway tests require Kong + services running (e.g., via local k8s or docker compose + gateway).
Set `GATEWAY_URL` if not default `http://localhost:8000`.

```bash
RUN_INTEGRATION=1 GATEWAY_URL=http://localhost:8000 make test-integration
```

### All Tests
```bash
make test-all
```

### Coverage
```bash
make test-coverage
```

## Test Data

Seeded demo users:
- **demo@example.com** / `demo123`
- **trader@example.com** / `trader123`

Optional fixtures (enabled with `SEED_TESTDATA=1`):
- **mfa@example.com** / `mfa123` (MFA enabled)
- **suspended@example.com** / `suspended123`

API keys are deterministic in dev and printed by the seed tool when `CEX_ENV=dev`.

## Environment Variables
DB integration tests use these environment variables (defaults shown):
- `POSTGRES_HOST=localhost`
- `POSTGRES_PORT=5432`
- `POSTGRES_DB=cex_core`
- `POSTGRES_USER=cex`
- `POSTGRES_PASSWORD=cex`
- `POSTGRES_SSLMODE=disable`

Set `RUN_DB_INTEGRATION=1` to enable DB integration tests.
Set `RUN_INTEGRATION=1` to enable gateway E2E tests.
