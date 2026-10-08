.DEFAULT_GOAL := dev
PNPM ?= pnpm
.PHONY: dev build check install
install:
	$(PNPM) install --frozen-lockfile
dev: install
	$(PNPM) dev
build: install
	$(PNPM) build
check: install
	$(PNPM) check
	$(PNPM) format:check
