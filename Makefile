.PHONY: setup cluster test lint build migrate sqlc

GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1

setup: ## installs the local environment dependencies and creates the kind cluster
	./scripts/setup.sh

cluster: ## creates the local kind cluster used by Tilt
	kind create cluster --name parceiros

test:
	cd backend && go test ./...

lint:
	cd backend && go vet ./... && $(GOLANGCI) run ./...

build:
	cd backend && go build -o ../bin/parceiros ./cmd/parceiros

migrate: build ## applies the migrations to the DATABASE_URL database
	./bin/parceiros migrate

sqlc: ## generates the Go code of the queries in backend/db/queries
	cd backend && $(SQLC) generate
