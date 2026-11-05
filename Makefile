.DEFAULT_GOAL := help
GO ?= go
IMAGE ?= kasperski-go-test:local

.PHONY: help fmt vet test check build run docker-build docker-run
help:
	@echo "Targets: fmt vet test check build run docker-build docker-run db-up db-down db-shell migrate-up migrate-down migrate-version test-db test-repository"

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

docker-run:
	docker run --rm -e LOG_LEVEL $(IMAGE)

# Override COMPOSE to use a separate project or env file.
COMPOSE ?= docker compose
MIGRATE = $(COMPOSE) run --rm migrate -path /migrations -database 'postgres://postgres:5432/?sslmode=disable'

.PHONY: db-up db-down db-shell migrate-up migrate-down migrate-version test-db test-repository

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
	python3 tests/integration/check_database.py

# Requires Docker Compose and Python 3; creates and removes an isolated test DB.
test-repository:
	python3 tests/integration/check_database.py --repository
