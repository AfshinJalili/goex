#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

COMPOSE_FILE=${COMPOSE_FILE:-deploy/docker-compose.yml}
ENV_FILE=${ENV_FILE:-deploy/.env}

compose_args=(-f "$COMPOSE_FILE")
if [[ -f "$ENV_FILE" ]]; then
  compose_args+=(--env-file "$ENV_FILE")
fi

compose_exec() {
  docker compose "${compose_args[@]}" exec -T "$@"
}

if ! compose_exec kong kong health >/dev/null 2>&1; then
  echo "Kong health check failed. Is the stack running?" >&2
  exit 1
fi

if ! compose_exec auth curl -fsS http://localhost:8080/healthz >/dev/null 2>&1; then
  echo "Auth service health check failed. Is the stack running?" >&2
  exit 1
fi

if ! compose_exec user curl -fsS http://localhost:8081/healthz >/dev/null 2>&1; then
  echo "User service health check failed. Is the stack running?" >&2
  exit 1
fi

if ! "$SCRIPT_DIR/verify-services.sh"; then
  echo "Service verification failed. Fix issues before running integration tests." >&2
  exit 1
fi

export RUN_INTEGRATION=1
export GATEWAY_URL=${GATEWAY_URL:-http://localhost:8000}

REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_DIR="$(cd "$(dirname "$COMPOSE_FILE")" && pwd)"
PROJECT_NAME="$(basename "$COMPOSE_DIR")"
NETWORK_NAME="${PROJECT_NAME}_default"

docker run --rm \
  --network "$NETWORK_NAME" \
  -v "$REPO_ROOT":/workspace \
  -w /workspace \
  -e RUN_INTEGRATION=1 \
  -e GATEWAY_URL=http://kong:8000 \
  -e ORDER_INGEST_URL=http://order-ingest:8083 \
  -e MATCHING_URL=http://matching:8080 \
  -e KAFKA_BROKERS=kafka:9092 \
  -e POSTGRES_HOST=postgres \
  -e POSTGRES_PORT=5432 \
  -e POSTGRES_DB="${POSTGRES_DB:-cex_core}" \
  -e POSTGRES_USER="${POSTGRES_USER:-cex}" \
  -e POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-cex}" \
  -e POSTGRES_SSLMODE=disable \
  golang:1.24-alpine sh -c "apk add --no-cache git ca-certificates >/dev/null && go test -v ./services/integration/..."
