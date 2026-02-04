#!/usr/bin/env bash
set -euo pipefail

docker compose -f deploy/docker-compose.yml up -d

echo "Local dev stack is starting. Use 'docker compose ps' to check status." 
echo "To stop: ./scripts/dev-down.sh"
