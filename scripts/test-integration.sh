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

go test -v ./services/integration/...
