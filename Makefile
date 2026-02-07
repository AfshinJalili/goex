.PHONY: lint test test-unit test-integration test-db test-all test-coverage build run docs docs-validate seed dev-reset dev-start dev-stop dev-verify dev-test dev-logs dev-restart

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed; skipping lint"; exit 0; }
	@golangci-lint run ./...

test:
	@go test ./...

test-unit:
	@go test ./... -short

test-db:
	@RUN_DB_INTEGRATION=1 go test ./services/auth/... ./services/user/...

test-integration:
	@RUN_INTEGRATION=1 go test ./services/integration/...

test-all: test-unit test-db test-integration

test-coverage:
	@go test ./... -coverprofile=coverage.out
	@go tool cover -html=coverage.out -o coverage.html

build:
	@go build ./...

run:
	@go run ./services/template/cmd/template

# Documentation
docs:
	@./scripts/openapi-serve.sh

docs-validate:
	@./scripts/openapi-validate.sh

# Seed
seed:
	@./scripts/seed.sh

# Dev reset
dev-reset:
	@docker compose -f deploy/docker-compose.yml down -v
	@./scripts/dev-up.sh

dev-start:
	@./scripts/dev-up.sh

dev-stop:
	@./scripts/dev-down.sh

dev-verify:
	@./scripts/verify-services.sh

dev-test:
	@./scripts/test-integration.sh

dev-logs:
	@docker compose -f deploy/docker-compose.yml logs -f

dev-restart: dev-stop dev-start

proto-fee:
	@cd services/fee && ./generate.sh

build-fee:
	@go build ./services/fee/cmd/fee

test-fee:
	@go test ./services/fee/...
