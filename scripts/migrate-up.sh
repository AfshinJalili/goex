#!/usr/bin/env bash
set -euo pipefail

: "${POSTGRES_HOST:=localhost}"
: "${POSTGRES_PORT:=5432}"
: "${POSTGRES_DB:=cex_core}"
: "${POSTGRES_USER:=cex}"
: "${POSTGRES_PASSWORD:=cex}"

DB_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"

if ! command -v migrate >/dev/null 2>&1; then
  echo "migrate CLI not found. Install: https://github.com/golang-migrate/migrate" >&2
  exit 1
fi

migrate -path migrations -database "$DB_URL" up
