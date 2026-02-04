# Data Services (Helm)

This directory provides Helm wrapper charts for core data services in the `cex-data` namespace.

## Install Order (Staging)
```bash
kubectl create namespace cex-data

helm upgrade --install cex-postgres deploy/helm/data/postgres \
  --namespace cex-data --create-namespace \
  --values deploy/helm/data/postgres/values-staging.yaml

helm upgrade --install cex-redis deploy/helm/data/redis \
  --namespace cex-data --create-namespace \
  --values deploy/helm/data/redis/values-staging.yaml

helm upgrade --install cex-kafka deploy/helm/data/kafka \
  --namespace cex-data --create-namespace \
  --values deploy/helm/data/kafka/values-staging.yaml

helm upgrade --install cex-timescaledb deploy/helm/data/timescaledb \
  --namespace cex-data --create-namespace \
  --values deploy/helm/data/timescaledb/values-staging.yaml
```

## Connection Endpoints
Postgres:
- Service: `cex-postgres-postgresql`
- Port: `5432`

Redis:
- Service: `cex-redis-master`
- Port: `6379`

Kafka:
- Service: `cex-kafka`
- Port: `9092`

TimescaleDB:
- Service: `cex-timescaledb`
- Port: `5432`

## Credentials (Staging Example)
Postgres:
- user: `cex`
- password: `cex`
- database: `cex_core`

Redis:
- password: `cex_redis`

TimescaleDB:
- superuser: `cex_ts_super`
- replication: `cex_ts_repl`
- admin: `cex_ts_admin`

## Overriding Credentials
For production, create Kubernetes Secrets and override the chart values to reference existing secrets rather than inline passwords.

## Backup Notes
- Postgres: enable snapshots or PITR depending on your storage class.
- TimescaleDB: use the chart’s backup configuration (pgBackRest) and external object storage for archival.
