#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="deploy/.env"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

: "${POSTGRES_HOST:=localhost}"
: "${POSTGRES_PORT:=5432}"
: "${POSTGRES_DB:=cex_core}"
: "${POSTGRES_USER:=cex}"
: "${POSTGRES_PASSWORD:=cex}"
: "${CEX_ENV:=dev}"
: "${CEX_JWT_SECRET:=dev-secret-change-in-production-min-32-chars}"
: "${CEX_RATE_LIMIT_REDIS_ADDR:=redis:6379}"
: "${GATEWAY_URL:=http://localhost:8000}"

export POSTGRES_HOST POSTGRES_PORT POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD CEX_ENV CEX_JWT_SECRET CEX_RATE_LIMIT_REDIS_ADDR GATEWAY_URL

compose_args=(-f deploy/docker-compose.yml)
if [[ -f "$ENV_FILE" ]]; then
  compose_args+=(--env-file "$ENV_FILE")
fi

docker compose "${compose_args[@]}" up -d --build

MIGRATE_BIN="$(command -v migrate || true)"
if [[ -z "$MIGRATE_BIN" ]]; then
  GOPATH_BIN="$(go env GOPATH 2>/dev/null)/bin/migrate"
  if [[ -x "$GOPATH_BIN" ]]; then
    MIGRATE_BIN="$GOPATH_BIN"
  fi
fi

if [[ -z "$MIGRATE_BIN" ]]; then
  echo "migrate CLI not found. Install: https://github.com/golang-migrate/migrate" >&2
  exit 1
fi

echo "Waiting for postgres to be ready..."
until docker compose "${compose_args[@]}" exec -T postgres pg_isready -U "$POSTGRES_USER" >/dev/null 2>&1; do
  sleep 1
done

echo "Running migrations..."
MIGRATE_BIN="$MIGRATE_BIN" ./scripts/migrate-up.sh

if [[ "$CEX_ENV" == "dev" || "$CEX_ENV" == "test" ]]; then
  echo "Seeding database..."
  ./scripts/seed.sh
else
  echo "Skipping seed for CEX_ENV=$CEX_ENV"
fi

echo "Restarting application services..."
docker compose "${compose_args[@]}" restart auth user fee ledger risk order-ingest matching

echo "Waiting for Kong gateway to be ready..."
until curl -fsS http://localhost:8001/status >/dev/null 2>&1; do
  sleep 1
done

wait_for() {
  local name="$1"
  local url="$2"
  local retries="${3:-30}"
  local delay="${4:-1}"

  for _ in $(seq 1 "$retries"); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$delay"
  done

  echo "Timed out waiting for $name at $url" >&2
  return 1
}

echo "Verifying service health endpoints..."
wait_for "auth" "http://localhost:8080/healthz"
wait_for "user" "http://localhost:8081/healthz"
wait_for "fee" "http://localhost:8082/healthz"
wait_for "ledger" "http://localhost:8083/healthz"
wait_for "risk" "http://localhost:8084/healthz"
wait_for "order-ingest" "http://localhost:8085/healthz"
wait_for "matching" "http://localhost:8086/healthz"
wait_for "kong" "http://localhost:8001/status"

echo "Local dev stack is ready."
echo "Auth service: http://localhost:8080"
echo "User service: http://localhost:8081"
echo "Fee service: http://localhost:8082"
echo "Ledger service: http://localhost:8083"
echo "Risk service: http://localhost:8084"
echo "Order ingest service: http://localhost:8085"
echo "Matching engine: http://localhost:8086"
echo "Gateway proxy: $GATEWAY_URL"
echo "Gateway admin: http://localhost:8001"
echo "JWT secret: $CEX_JWT_SECRET"
echo "Demo users: demo@example.com / demo123, trader@example.com / trader123"
echo "To stop: ./scripts/dev-down.sh"
