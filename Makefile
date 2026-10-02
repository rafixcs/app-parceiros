.PHONY: cluster test lint build migrate

GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

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
