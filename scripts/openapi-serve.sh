#!/usr/bin/env bash
set -euo pipefail

if ! docker info >/dev/null 2>&1; then
  echo "Error: Docker is not running. Please start Docker and try again."
  exit 1
fi

echo "OpenAPI docs server starting at http://localhost:8080"
echo "Press Ctrl+C to stop"

docker run --rm -p 8080:8080 -v "$(pwd)/docs/api:/usr/share/nginx/html/api" -e SWAGGER_JSON=/api/openapi.yaml swaggerapi/swagger-ui
