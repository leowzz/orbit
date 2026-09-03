.DEFAULT_GOAL := dev

GO ?= go
BUF ?= buf

.PHONY: dev dev-agent dev-core build test lint fmt proto-lint generate

dev: dev-agent

dev-agent:
	$(GO) run ./cmd/orbit-agent

dev-core:
	$(GO) run ./cmd/orbit-core

build:
	$(GO) build ./...

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

proto-lint:
	$(BUF) lint

generate:
	$(BUF) generate

