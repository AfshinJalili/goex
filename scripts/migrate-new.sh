#!/usr/bin/env bash
set -euo pipefail

if ! command -v migrate >/dev/null 2>&1; then
  echo "migrate CLI not found. Install: https://github.com/golang-migrate/migrate" >&2
  exit 1
fi

if [ $# -lt 1 ]; then
  echo "Usage: $0 <name>" >&2
  exit 1
fi

migrate create -ext sql -dir migrations "$1"
