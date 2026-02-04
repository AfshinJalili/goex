#!/usr/bin/env bash
set -euo pipefail

docker compose -f deploy/docker-compose.yml down

echo "Local dev stack stopped."
echo "For a full reset (remove volumes): docker compose -f deploy/docker-compose.yml down -v"
