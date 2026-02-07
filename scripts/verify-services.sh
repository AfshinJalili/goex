#!/usr/bin/env bash
set -euo pipefail

POSTGRES_USER=${POSTGRES_USER:-cex}
TIMESCALE_USER=${TIMESCALE_USER:-cex_ts}

COMPOSE_FILE=${COMPOSE_FILE:-deploy/docker-compose.yml}
ENV_FILE=${ENV_FILE:-deploy/.env}

compose_args=(-f "$COMPOSE_FILE")
if [[ -f "$ENV_FILE" ]]; then
  compose_args+=(--env-file "$ENV_FILE")
fi

OK="✓"
FAIL="✗"
FAILED=0

print_row() {
  printf "%-20s %s\n" "$1" "$2"
}

compose_exec() {
  docker compose "${compose_args[@]}" exec -T "$@"
}

check_compose_cmd() {
  local name="$1"
  shift
  if compose_exec "$@" >/dev/null 2>&1; then
    print_row "$name" "$OK"
  else
    print_row "$name" "$FAIL"
    FAILED=1
  fi
}

printf "%-20s %s\n" "Service" "Status"
printf "%-20s %s\n" "-------" "------"

check_compose_cmd "Postgres" postgres pg_isready -U "$POSTGRES_USER"
check_compose_cmd "TimescaleDB" timescaledb pg_isready -U "$TIMESCALE_USER"
check_compose_cmd "Redis" redis redis-cli ping
check_compose_cmd "Kafka" kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list
check_compose_cmd "Auth" auth curl -fsS http://localhost:8080/healthz
check_compose_cmd "User" user curl -fsS http://localhost:8081/healthz
check_compose_cmd "Fee" fee curl -fsS http://localhost:8080/healthz
check_compose_cmd "Ledger" ledger curl -fsS http://localhost:8080/healthz
check_compose_cmd "Risk" risk curl -fsS http://localhost:8080/healthz
check_compose_cmd "OrderIngest" order-ingest curl -fsS http://localhost:8083/healthz
check_compose_cmd "Matching" matching curl -fsS http://localhost:8080/healthz
check_compose_cmd "Kong" kong kong health

if [[ $FAILED -ne 0 ]]; then
  exit 1
fi
