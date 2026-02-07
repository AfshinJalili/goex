#!/usr/bin/env bash
set -euo pipefail

: "${POSTGRES_HOST:=localhost}"
: "${POSTGRES_PORT:=5432}"
: "${POSTGRES_DB:=cex_core}"
: "${POSTGRES_USER:=cex}"
: "${POSTGRES_PASSWORD:=cex}"

DB_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"

MIGRATE_BIN="${MIGRATE_BIN:-$(command -v migrate || true)}"

if [[ -z "$MIGRATE_BIN" ]]; then
  echo "migrate CLI not found. Install: https://github.com/golang-migrate/migrate" >&2
  exit 1
fi

"$MIGRATE_BIN" -path migrations -database "$DB_URL" up
