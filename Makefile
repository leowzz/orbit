.DEFAULT_GOAL := dev

GO ?= go
TOOLS_DIR := $(CURDIR)/.tools/bin
BUF := $(TOOLS_DIR)/buf
PROTOC_GEN_GO := $(TOOLS_DIR)/protoc-gen-go
NODE_DIR := nodes/display/models/oled-128x32/variants/yd-esp32-s3
AGENT_CONFIG ?= configs/agent.local.yaml
CORE_CONFIG ?= configs/core.local.yaml
WEB_CONFIG ?= configs/web.local.yaml
AGENT_LAUNCHD_LABEL ?= com.leo.orbit.agent.dev
CORE_LAUNCHD_LABEL ?= com.leo.orbit.core.dev
WEB_LAUNCHD_LABEL ?= com.leo.orbit.web.dev

.PHONY: dev dev-agent dev-core dev-web kill kill-agent kill-core kill-web \
	build build-go build-node test test-go test-node lint fmt fmt-check \
	proto-lint generate verify

dev:
	$(MAKE) -j2 dev-agent dev-core

dev-agent:
	$(GO) run ./cmd/orbit-agent -config "$(AGENT_CONFIG)"

dev-core:
	$(GO) run ./cmd/orbit-core -config "$(CORE_CONFIG)"

dev-web:
	$(GO) run ./cmd/orbit-web -config "$(WEB_CONFIG)"

kill: kill-web kill-agent kill-core

kill-agent:
	@launchctl remove "$(AGENT_LAUNCHD_LABEL)" 2>/dev/null || true
	@pkill -TERM -f '[/][o]rbit-agent([[:space:]]|$$)' 2>/dev/null || true

kill-core:
	@launchctl remove "$(CORE_LAUNCHD_LABEL)" 2>/dev/null || true
	@pkill -TERM -f '[/][o]rbit-core([[:space:]]|$$)' 2>/dev/null || true

kill-web:
	@launchctl remove "$(WEB_LAUNCHD_LABEL)" 2>/dev/null || true
	@pkill -TERM -f '[/][o]rbit-web([[:space:]]|$$)' 2>/dev/null || true

build: build-go

build-go:
	$(GO) build ./...

build-node:
	$(MAKE) -C "$(NODE_DIR)" build CONFIG=config.example.yaml

test: test-go

test-go:
	$(GO) test ./...

test-node:
	$(MAKE) -C "$(NODE_DIR)" check

lint:
	$(GO) vet ./...

fmt:
	gofmt -w $$(find cmd internal proto -name '*.go' -type f)

fmt-check:
	@test -z "$$(gofmt -l $$(find cmd internal proto -name '*.go' -type f))"

proto-lint: $(BUF)
	$(BUF) lint

generate: $(BUF) $(PROTOC_GEN_GO)
	PATH="$(TOOLS_DIR):$$PATH" $(BUF) generate

verify: fmt-check lint test-go proto-lint test-node build-go build-node

$(BUF):
	@mkdir -p "$(TOOLS_DIR)"
	GOBIN="$(TOOLS_DIR)" $(GO) install github.com/bufbuild/buf/cmd/buf@v1.72.0

$(PROTOC_GEN_GO):
	@mkdir -p "$(TOOLS_DIR)"
	GOBIN="$(TOOLS_DIR)" $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
