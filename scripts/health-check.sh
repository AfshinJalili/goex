#!/usr/bin/env bash
set -euo pipefail

INTERVAL=5
ONCE=0

if [[ "${1:-}" == "--once" ]]; then
  ONCE=1
fi

POSTGRES_USER=${POSTGRES_USER:-cex}
TIMESCALE_USER=${TIMESCALE_USER:-cex_ts}

COMPOSE_FILE=${COMPOSE_FILE:-deploy/docker-compose.yml}
ENV_FILE=${ENV_FILE:-deploy/.env}

compose_args=(-f "$COMPOSE_FILE")
if [[ -f "$ENV_FILE" ]]; then
  compose_args+=(--env-file "$ENV_FILE")
fi

GREEN="\033[0;32m"
YELLOW="\033[0;33m"
RED="\033[0;31m"
NC="\033[0m"

compose_exec() {
  docker compose "${compose_args[@]}" exec -T "$@" >/dev/null 2>&1
}

check_cmd() {
  local service="$1"
  shift
  compose_exec "$service" "$@"
}

status_line() {
  local name="$1"
  local result="$2"

  if [[ "$result" -eq 0 ]]; then
    printf "%-20s %b\n" "$name" "${GREEN}healthy${NC}"
    return
  fi

  if [[ "$ONCE" -eq 1 ]]; then
    printf "%-20s %b\n" "$name" "${RED}unhealthy${NC}"
    return
  fi

  printf "%-20s %b\n" "$name" "${YELLOW}starting${NC}"
}

while :; do
  printf "\n%s\n" "Health check @ $(date)"
  printf "%-20s %s\n" "Service" "Status"
  printf "%-20s %s\n" "-------" "------"

  check_cmd postgres pg_isready -U "$POSTGRES_USER"
  status_line "Postgres" $?

  check_cmd timescaledb pg_isready -U "$TIMESCALE_USER"
  status_line "TimescaleDB" $?

  check_cmd redis redis-cli ping
  status_line "Redis" $?

  check_cmd kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list
  status_line "Kafka" $?

  check_cmd auth curl -fsS http://localhost:8080/healthz
  status_line "Auth" $?

  check_cmd user curl -fsS http://localhost:8081/healthz
  status_line "User" $?

  check_cmd kong kong health
  status_line "Kong" $?

  if [[ "$ONCE" -eq 1 ]]; then
    exit 0
  fi

  sleep "$INTERVAL"
done
