#!/usr/bin/env bash
set -euo pipefail

: "${POSTGRES_HOST:=localhost}"
: "${POSTGRES_PORT:=5432}"
: "${POSTGRES_DB:=cex_core}"
: "${POSTGRES_USER:=cex}"
: "${POSTGRES_PASSWORD:=cex}"
: "${CEX_ENV:=dev}"

if [[ "$CEX_ENV" != "dev" && "$CEX_ENV" != "test" ]]; then
  echo "Refusing to seed: CEX_ENV must be 'dev' or 'test' (got '$CEX_ENV')" >&2
  exit 1
fi

export POSTGRES_HOST POSTGRES_PORT POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD CEX_ENV

go run ./cmd/seed
