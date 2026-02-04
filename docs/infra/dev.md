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

## Kafka
- Bootstrap servers: `localhost:9092`
- Advertised listeners default to `PLAINTEXT://localhost:9092`
 - Image: `apache/kafka:3.7.0` (chosen for local KRaft compatibility)

## Notes
- Update `deploy/.env.example` and provide a `.env` if you want custom credentials.
- TimescaleDB is exposed on port `5433` to avoid clashing with Postgres.
