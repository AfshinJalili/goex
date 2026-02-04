#!/usr/bin/env bash
set -euo pipefail

docker run --rm -v "$(pwd)/docs/api:/spec" redocly/cli lint /spec/openapi.yaml
