#!/usr/bin/env bash
set -euo pipefail

echo "Running unit tests..."
go test ./...

echo "Running DB integration tests..."
RUN_DB_INTEGRATION=1 make test-db

echo "Running end-to-end integration tests..."
./scripts/test-integration.sh
