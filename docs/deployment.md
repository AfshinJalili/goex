# Deployment Guide

## Prerequisites
- Docker Engine 24+
- Docker Compose v2
- PostgreSQL 16 (if running outside Docker)
- Kafka 3.7 (if running outside Docker)

## Environment
Set required secrets and connection details:
- `CEX_JWT_SECRET`
- `POSTGRES_*`
- `KAFKA_*`

## Deploy with Docker Compose
1. `docker compose -f deploy/docker-compose.yml pull`
2. `docker compose -f deploy/docker-compose.yml up -d --build`
3. `./scripts/verify-services.sh`

## Migrations
Run migrations after the database is reachable:
```bash
./scripts/migrate-up.sh
```

## Rollback
```bash
./scripts/migrate-down.sh
```

## Health Checks
Each service exposes:
- `/healthz` for liveness
- `/readyz` for readiness
