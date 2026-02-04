.PHONY: lint test build run

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed; skipping lint"; exit 0; }
	@golangci-lint run ./...

test:
	@go test ./...

build:
	@go build ./...

run:
	@go run ./services/template/cmd/template
