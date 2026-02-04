# Kubernetes Deployment Topology

## Namespaces
- `cex-core`: core services (order, matching, ledger, user, market, wallet).
- `cex-data`: Postgres, Redis, Timescale, Kafka.
- `cex-ops`: monitoring, logging, admin tooling.

## Core Workloads
- Deployments: API gateway, user, order ingest, ledger, fee, market data, compliance, admin, risk.
- StatefulSets: Postgres, TimescaleDB, Kafka, Redis (clustered).
- Matching engine: dedicated deployment with node affinity for low-latency nodes.

## Networking
- Ingress controller (NGINX/Traefik) with TLS termination.
- Internal service-to-service traffic uses mTLS.
- Network policies to restrict data plane access.

## Config and Secrets
- ConfigMaps for service configs.
- Secrets for DB credentials, JWT signing keys, API keys.
- External secrets manager (optional) for production.

## Scaling
- HPA for stateless services.
- Kafka partition scaling by symbol.
- Market data service scaled by client connections.

## Health Checks
- Liveness and readiness probes for each service.
- Matching engine exposes health endpoints and snapshot API.

## CI/CD
- Build, test, security scan, deploy to staging.
- Canary rollout for critical services.

## Base K8s Manifests
See `deploy/k8s/README.md` for namespaces, network policies, and Kong Helm setup.

## Kong Gateway (DB-less)
See `deploy/kong/README.md` for the DB-less Kong declarative config and smoke tests.

## Data Services
See `deploy/helm/data/README.md` for Helm-based data service deployments.

## Local Development
For local docker-compose setup, see `docs/infra/dev.md`.
