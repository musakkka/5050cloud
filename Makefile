# SPDX-License-Identifier: AGPL-3.0-only
#
# 5050cloud build targets (docs/SPEC.md 17.1). `make help` lists them.
# Every tool is pinned here; `make tools` installs the Go ones into bin/tools.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

GO ?= go
NPM ?= npm
MODULE := github.com/musakkka/5050cloud
TOOLS := $(CURDIR)/bin/tools

# Pinned tool versions (each at least two weeks old when pinned).
GOLANGCI_LINT_VERSION := v2.14.0
TYGO_VERSION := v0.2.21
GITLEAKS_VERSION := v8.30.1
GOVULNCHECK_VERSION := v1.8.0
SHELLCHECK_IMAGE := koalaman/shellcheck:v0.11.0
GOLANG_IMAGE := golang:1.27.1-bookworm

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/pkg/version.Version=$(VERSION) \
	-X $(MODULE)/pkg/version.Commit=$(COMMIT) \
	-X $(MODULE)/pkg/version.Date=$(DATE)

BINARIES := 5050d 5050-agent 5050
# Go packages of this module. web/node_modules can hold stray Go packages
# from npm dependencies, so ./... is filtered.
GO_PKGS = $(shell $(GO) list ./... | grep -v /node_modules/)
SHELL_SCRIPTS = $(wildcard install/*.sh e2e/*.sh scripts/*.sh)

.PHONY: help
help: ## List targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ---------------------------------------------------------------- tools

$(TOOLS)/.golangci-lint-$(GOLANGCI_LINT_VERSION):
	@mkdir -p $(TOOLS)
	GOBIN=$(TOOLS) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@touch $@
$(TOOLS)/.tygo-$(TYGO_VERSION):
	@mkdir -p $(TOOLS)
	GOBIN=$(TOOLS) $(GO) install github.com/gzuidhof/tygo@$(TYGO_VERSION)
	@touch $@
$(TOOLS)/.gitleaks-$(GITLEAKS_VERSION):
	@mkdir -p $(TOOLS)
	GOBIN=$(TOOLS) $(GO) install github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION)
	@touch $@
$(TOOLS)/.govulncheck-$(GOVULNCHECK_VERSION):
	@mkdir -p $(TOOLS)
	GOBIN=$(TOOLS) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@touch $@

GOLANGCI_LINT := $(TOOLS)/.golangci-lint-$(GOLANGCI_LINT_VERSION)
TYGO := $(TOOLS)/.tygo-$(TYGO_VERSION)
GITLEAKS := $(TOOLS)/.gitleaks-$(GITLEAKS_VERSION)
GOVULNCHECK := $(TOOLS)/.govulncheck-$(GOVULNCHECK_VERSION)

.PHONY: tools
tools: $(GOLANGCI_LINT) $(TYGO) $(GITLEAKS) $(GOVULNCHECK) ## Install the pinned Go tools into bin/tools

web/node_modules/.package-lock.json: web/package-lock.json
	cd web && $(NPM) ci --no-audit --no-fund
	@touch $@
WEB_DEPS := web/node_modules/.package-lock.json

# ---------------------------------------------------------------- build

.PHONY: deps
deps: ## Download Go modules and install npm packages for web/ (and site/ when it exists)
	$(GO) mod download
	cd web && $(NPM) ci --no-audit --no-fund
	@if [ -f site/package-lock.json ]; then cd site && $(NPM) ci --no-audit --no-fund; fi

.PHONY: dist-placeholder
dist-placeholder: ## Create web/dist/index.html if missing so Go builds without Node
	@mkdir -p web/dist; if [ ! -f web/dist/index.html ]; then \
		printf '<!doctype html><meta charset="utf-8"><title>5050cloud</title><p>The console was not built. Run make web.</p>\n' > web/dist/index.html; \
		echo "Wrote placeholder web/dist/index.html"; \
	fi

.PHONY: web
web: $(WEB_DEPS) ## Build the console into web/dist and check the P10 size budget
	cd web && $(NPM) run build
	sh scripts/web-size.sh

.PHONY: build
build: dist-placeholder ## Build bin/5050d, bin/5050-agent and bin/5050 for this OS and arch
	@mkdir -p bin
	@for b in $(BINARIES); do \
		if [ -d cmd/$$b ]; then \
			echo "go build -o bin/$$b ./cmd/$$b"; \
			CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/$$b ./cmd/$$b; \
		else \
			echo "skip  cmd/$$b does not exist yet"; \
		fi; \
	done
	$(GO) build $(GO_PKGS)

.PHONY: cross
cross: dist-placeholder ## Cross-build linux amd64/arm64 for all binaries; darwin and windows for the CLI
	@mkdir -p bin
	@for b in $(BINARIES); do \
		[ -d cmd/$$b ] || { echo "skip  cmd/$$b does not exist yet"; continue; }; \
		for arch in amd64 arm64; do \
			echo "linux/$$arch $$b"; \
			GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/linux_$$arch/$$b ./cmd/$$b; \
		done; \
	done
	@if [ -d cmd/5050 ]; then \
		for t in darwin/amd64 darwin/arm64 windows/amd64; do \
			os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
			echo "$$t 5050"; \
			GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/$${os}_$$arch/5050$$ext ./cmd/5050; \
		done; \
	fi

# ---------------------------------------------------------------- test

.PHONY: test
test: dist-placeholder $(WEB_DEPS) ## Go tests with the race detector, plus npm test in web/
	$(GO) test -race -count=1 $(GO_PKGS)
	cd web && $(NPM) test

TEST_LINUX_PKGS ?= ./internal/agent/...
.PHONY: test-linux
test-linux: dist-placeholder ## Integration tests (tag integration) in a privileged golang container on local Docker
	docker run --rm --privileged \
		-v $(CURDIR):/src -w /src \
		-v 5050-gomod:/go/pkg/mod -v 5050-gocache:/root/.cache/go-build \
		-v /var/run/docker.sock:/var/run/docker.sock \
		$(GOLANG_IMAGE) \
		go test -tags integration -race -count=1 $(TEST_LINUX_PKGS)

.PHONY: cover
cover: dist-placeholder ## Coverage per package; fails under the floors of docs/SPEC.md 18.1
	sh scripts/cover.sh

# ---------------------------------------------------------------- lint

.PHONY: lint
lint: lint-go lint-web lint-sh ## gofmt, go vet, golangci-lint (with staticcheck), tsc, eslint, shellcheck

.PHONY: lint-go
lint-go: dist-placeholder $(GOLANGCI_LINT)
	@out=$$(gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go' 2>/dev/null || find . -name '*.go' -not -path './web/node_modules/*')); \
	if [ -n "$$out" ]; then echo "gofmt would change these files:"; echo "$$out"; exit 1; fi
	$(GO) vet $(GO_PKGS)
	$(TOOLS)/golangci-lint run ./...

.PHONY: lint-web
lint-web: $(WEB_DEPS)
	cd web && $(NPM) run typecheck
	cd web && $(NPM) run lint

.PHONY: lint-sh
lint-sh:
	@scripts="$(SHELL_SCRIPTS)"; \
	if [ -z "$$scripts" ]; then echo "no shell scripts"; exit 0; fi; \
	if command -v shellcheck >/dev/null 2>&1; then shellcheck $$scripts; \
	elif command -v docker >/dev/null 2>&1; then docker run --rm -v $(CURDIR):/mnt -w /mnt $(SHELLCHECK_IMAGE) $$scripts; \
	else echo "shellcheck is not installed and Docker is not available; install one of them to lint shell scripts." >&2; exit 1; fi

.PHONY: vulncheck
vulncheck: dist-placeholder $(GOVULNCHECK) ## govulncheck over every package
	$(TOOLS)/govulncheck $(GO_PKGS)

# ---------------------------------------------------------------- generated files

.PHONY: gen-ts
gen-ts: $(TYGO) ## Regenerate web/src/api/types.gen.ts and the MSW mocks from pkg/api
	$(TOOLS)/tygo generate
	$(GO) run ./pkg/api/internal/mockgen -out web/src/api/mocks/handlers.gen.ts

.PHONY: check-gen
check-gen: gen-ts ## Fail when generated files are stale (CI)
	@git diff --exit-code -- web/src/api/types.gen.ts web/src/api/mocks/handlers.gen.ts || \
		{ echo "Generated API files are stale. Run make gen-ts and commit the result." >&2; exit 1; }

.PHONY: labels
labels: ## Regenerate web/e2e/labels.json and e2e/v1-checklist.json from docs/SPEC.md (planner only)
	$(GO) run scripts/gen-labels.go

.PHONY: contracts
contracts: ## Rewrite docs/contracts.sha256 (planner only, with a spec change)
	sh scripts/contracts.sh update

.PHONY: check-public
check-public: $(GITLEAKS) ## Secrets, deny list, private IPs, em dashes, banned words, SPDX, license boundary, stubs, contract hashes
	GITLEAKS=$(TOOLS)/gitleaks DENYLIST=$(DENYLIST) bash scripts/check-public.sh

# ---------------------------------------------------------------- run

.PHONY: dev
dev: dist-placeholder $(WEB_DEPS) ## Run 5050d --dev and Vite (the console alone against MSW mocks until cmd/5050d exists)
	@if [ -d cmd/5050d ]; then \
		mkdir -p .dev; \
		trap 'kill 0' EXIT INT TERM; \
		$(GO) run ./cmd/5050d serve --dev --data ./.dev & \
		if [ "$$(uname -s)" = Linux ] && [ -d cmd/5050-agent ]; then $(GO) run ./cmd/5050-agent --dev --control http://127.0.0.1:5050 --data ./.dev/agent & fi; \
		cd web && FIFTY_API=http://127.0.0.1:5050 $(NPM) run dev; \
	else \
		echo "cmd/5050d does not exist yet, so the console runs against the generated MSW mocks."; \
		cd web && $(NPM) run dev:mocks; \
	fi

.PHONY: site
site: ## Build the docs site
	@if [ -d site ]; then cd site && $(NPM) run build; else echo "site/ does not exist yet (WP9)." >&2; exit 1; fi

.PHONY: release-snapshot
release-snapshot: ## goreleaser snapshot into dist/ (WP6)
	@if [ -f .goreleaser.yaml ]; then goreleaser release --snapshot --clean; else echo ".goreleaser.yaml does not exist yet (WP6)." >&2; exit 1; fi

.PHONY: e2e
e2e: ## VM end-to-end suite: make e2e HOST=user@ip [HOST2=user@ip] SUITE=single (WP6)
	@if [ -x e2e/run.sh ]; then e2e/run.sh --host "$(HOST)" $(if $(HOST2),--host2 "$(HOST2)") --suite "$(or $(SUITE),single)"; \
	else echo "e2e/run.sh does not exist yet (WP6)." >&2; exit 1; fi

.PHONY: e2e-ui
e2e-ui: $(WEB_DEPS) ## Playwright suite against E2E_CONSOLE_URL (WP8a)
	@if [ -f web/playwright.config.ts ]; then cd web && npx playwright test; else echo "web/playwright.config.ts does not exist yet (WP8a)." >&2; exit 1; fi

.PHONY: loadtest
loadtest: ## S1 with fake agents against a local 5050d (WP6)
	@if [ -d e2e/loadtest ]; then $(GO) run ./e2e/loadtest; else echo "e2e/loadtest does not exist yet (WP6)." >&2; exit 1; fi

.PHONY: clean
clean: ## Remove build outputs (keeps bin/tools)
	@find bin -mindepth 1 -maxdepth 1 -not -name tools -exec rm -rf {} + 2>/dev/null || true
	rm -rf dist .dev
	@find web/dist -mindepth 1 -not -name .gitkeep -delete 2>/dev/null || true
