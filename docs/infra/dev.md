# Local Development Stack

This project ships a docker-compose stack for local development.

## Start
```bash
./scripts/dev-up.sh
```

## Stop
```bash
./scripts/dev-down.sh
```

## Full reset (remove volumes)
```bash
docker compose -f deploy/docker-compose.yml down -v
```

## Services and Ports
- Postgres: `localhost:5432`
- TimescaleDB: `localhost:5433`
- Redis: `localhost:6379`
- Kafka: `localhost:9092`

## Default Credentials
Postgres:
- user: `cex`
- password: `cex`
- database: `cex_core`

TimescaleDB:
- user: `cex_ts`
- password: `cex_ts`
- database: `cex_timeseries`

## Seed Data

The dev stack automatically seeds demo data after migrations when `CEX_ENV=dev` or `CEX_ENV=test`.
To seed manually:

```bash
CEX_ENV=dev ./scripts/seed.sh
```

**WARNING: This is dev/test-only data. Never use in production.**

### Demo Users

- **demo@example.com** / `demo123`
- **trader@example.com** / `trader123`

Both users have:
- Status: `active`
- KYC Level: `verified`
- MFA: disabled
- Spot accounts with sample balances
- API keys with `trade` and `read` scopes

Seeds are deterministic and idempotent.

### Test Fixtures (Optional)
Set `SEED_TESTDATA=1` to add extra test users (MFA/suspended):
```bash
CEX_ENV=dev SEED_TESTDATA=1 ./scripts/seed.sh
```

## Kafka
- Bootstrap servers: `localhost:9092`
- Advertised listeners default to `PLAINTEXT://localhost:9092`
 - Image: `apache/kafka:3.7.0` (chosen for local KRaft compatibility)

## Notes
- Update `deploy/.env.example` and provide a `.env` if you want custom credentials.
- TimescaleDB is exposed on port `5433` to avoid clashing with Postgres.
