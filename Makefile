.DEFAULT_GOAL := help
GO ?= go
IMAGE ?= kasperski-go-test:local

.PHONY: help fmt vet test check build run docker-build docker-run
help:
	@echo "Targets: fmt vet test check build run docker-build docker-run"

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
