#!/usr/bin/env bash
set -euo pipefail

: "${POSTGRES_HOST:=localhost}"
: "${POSTGRES_PORT:=5432}"
: "${POSTGRES_DB:=cex_core}"
: "${POSTGRES_USER:=cex}"
: "${POSTGRES_PASSWORD:=cex}"
: "${CEX_ENV:=dev}"

export POSTGRES_HOST POSTGRES_PORT POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD CEX_ENV

docker compose -f deploy/docker-compose.yml up -d

if ! command -v migrate >/dev/null 2>&1; then
  echo "migrate CLI not found. Install: https://github.com/golang-migrate/migrate" >&2
  exit 1
fi

echo "Waiting for postgres to be ready..."
until docker compose -f deploy/docker-compose.yml exec -T postgres pg_isready -U "$POSTGRES_USER" >/dev/null 2>&1; do
  sleep 1
done

echo "Running migrations..."
./scripts/migrate-up.sh

if [[ "$CEX_ENV" == "dev" || "$CEX_ENV" == "test" ]]; then
  echo "Seeding database..."
  ./scripts/seed.sh
else
  echo "Skipping seed for CEX_ENV=$CEX_ENV"
fi

echo "Local dev stack is ready. Use 'docker compose ps' to check status." 
echo "To stop: ./scripts/dev-down.sh"
