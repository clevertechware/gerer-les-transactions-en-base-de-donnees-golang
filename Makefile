.PHONY: help build run-server run-remote test test-unit test-integration demo mock mock-clean tidy fmt db-up db-down clean

BIN := bin

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: ## Build both binaries
	go build -o $(BIN)/server ./cmd/server
	go build -o $(BIN)/remote ./cmd/remote

run-server: ## Run the API server
	go run ./cmd/server

run-remote: ## Run the slow third-party service
	go run ./cmd/remote

test: ## Run every test (requires Docker)
	go test ./...

test-unit: ## Run the fast tests only, no Docker needed
	go test -short -race ./...

test-integration: ## Run the testcontainers-backed tests
	go test -v -count=1 ./internal/postgres/... ./internal/migrate/... ./test/...

demo: ## Show the lock contention measurement on its own
	go test -v -count=1 -run 'TestVerify(Bad|Good)_Holds' ./test/integration/...

mock: ## Regenerate mocks
	mockery

mock-clean: ## Remove generated mocks
	find . -type d -name mocks -prune -exec rm -rf {} +

tidy: ## Tidy go.mod
	go mod tidy

fmt: ## Format the code
	gofmt -s -w .

db-up: ## Start PostgreSQL
	docker compose up -d --wait

db-down: ## Stop PostgreSQL and drop its volume
	docker compose down -v

clean: ## Remove build artifacts
	rm -rf $(BIN)
	go clean
