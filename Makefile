.PHONY: setup cluster test lint build migrate sqlc

GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1

setup: ## instala as dependências do ambiente local e cria o cluster kind
	./scripts/setup.sh

cluster: ## cria o cluster local kind usado pelo Tilt
	kind create cluster --name parceiros

test:
	cd backend && go test ./...

lint:
	cd backend && go vet ./... && $(GOLANGCI) run ./...

build:
	cd backend && go build -o ../bin/parceiros ./cmd/parceiros

migrate: build ## aplica migrations no banco de DATABASE_URL
	./bin/parceiros migrate

sqlc: ## gera o código Go das queries em backend/db/queries
	cd backend && $(SQLC) generate
