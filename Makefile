.DEFAULT_GOAL := help
GO ?= go
IMAGE ?= kaspersky-go-test:local
API_WAIT_TIMEOUT ?= 30

.PHONY: help fmt vet test check build run docker-build docker-run
help:
	@echo "Targets: fmt vet test check build run docker-build docker-run db-up db-down db-shell migrate-up migrate-down migrate-version test-db test-repository test-integration up down logs test-runtime generate generate-sql generate-http lint"

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

check:
	$(GO) fmt ./...
	$(GO) vet ./...
	$(GO) test ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/api ./cmd/api

run:
	$(GO) run ./cmd/api

docker-build:
	docker build -t $(IMAGE) .

docker-run: up

# Override COMPOSE to use a separate project or env file.
COMPOSE ?= docker compose
MIGRATE = $(COMPOSE) run --rm migrate -path /migrations -database 'postgres://postgres:5432/?sslmode=disable'

.PHONY: db-up db-down db-shell migrate-up migrate-down migrate-version test-db test-repository test-integration

db-up:
	$(COMPOSE) up -d --wait postgres

db-down:
	$(COMPOSE) down

db-shell:
	$(COMPOSE) exec postgres sh -c 'exec psql -X -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

migrate-up:
	$(MIGRATE) up

# Destructive: removes one migration, including its data. Run explicitly only.
migrate-down:
	$(MIGRATE) down 1

migrate-version:
	$(MIGRATE) version

test-db:
	$(GO) test -race -tags=integration -count=1 -v ./tests/database

# Requires Docker; Testcontainers creates and removes an isolated test DB.
test-repository: test-integration

test-integration:
	$(GO) test -race -tags=integration -count=1 -v ./tests/integration

.PHONY: up down logs test-runtime

# Keep migrations explicit, ordered, and outside application startup.
up:
	$(MAKE) db-up
	$(MAKE) migrate-up
	$(COMPOSE) up --build --wait --wait-timeout $(API_WAIT_TIMEOUT) api

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f api

test-runtime:
	$(GO) test -race -tags=e2e -count=1 -timeout=15m -v ./tests/runtime ./tests/compose

SQLC_VERSION := v1.31.1
OAPI_CODEGEN_VERSION := v2.8.2
GOLANGCI_LINT_VERSION := v2.15.0

.PHONY: generate generate-sql generate-http lint
generate: generate-sql generate-http

generate-sql:
	$(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

generate-http:
	$(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -config oapi-codegen.yaml docs/backend/openapi.yaml

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run
