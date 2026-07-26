.PHONY: help build run-server run-remote test test-unit test-integration demo mock mock-clean tidy fmt db-up db-down db-seed replication-status clean

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

db-up: ## Start the primary and its standby
	docker compose up -d --wait

db-down: ## Stop both servers and drop their volumes
	docker compose down -v

db-seed: ## Load the demo dataset into the primary (200k companies, 100k users)
	docker compose exec -T postgres psql -U postgres -d demo -q -v ON_ERROR_STOP=1 -f - < prefill_data_final.sql

replication-status: ## Show how far behind the standby is
	docker compose exec -T postgres psql -U postgres -d demo -c "\
		SELECT application_name, state, sync_state, \
		       pg_wal_lsn_diff(sent_lsn, replay_lsn) AS replay_lag_bytes \
		FROM pg_stat_replication;"

clean: ## Remove build artifacts
	rm -rf $(BIN)
	go clean
