# Agentic Control Plane — developer Makefile
# Run `make` or `make help` to list targets.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

.PHONY: help all build install clean fmt verify-fmt vet test test-coverage check ci

.DEFAULT_GOAL := help

# ---- Primary targets ----------------------------------------------------------

help: ## Show available targets
	@echo "Usage: make [<target>]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

all: fmt vet test build ## Format, vet, test, and build (local pre-push)

build: ## Build agentctl into bin/agentctl
	mkdir -p bin
	go build -trimpath -o bin/agentctl ./cmd/agentctl

install: ## Install agentctl with go install (honours GOBIN / GOPATH/bin)
	go install -trimpath ./cmd/agentctl

clean: ## Remove bin/ and coverage.out
	rm -rf bin/
	rm -f coverage.out

# ---- Go tooling ---------------------------------------------------------------

fmt: ## Run go fmt on all packages
	go fmt ./...

verify-fmt: ## Fail if any file needs gofmt (matches CI check)
	@out="$$(gofmt -l .)"; \
	if [[ -n "$$out" ]]; then \
		echo "Run \"make fmt\" — gofmt would change:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet: ## Run go vet
	go vet ./...

test: ## Run tests with race detector
	go test ./... -race

test-coverage: ## Run tests and write coverage.out
	go test ./... -race -coverprofile=coverage.out
	@echo "Summary:"
	@go tool cover -func=coverage.out | tail -n1

check: vet test ## Vet + test (no formatting changes)

ci: verify-fmt vet test ## CI-style gate: formatting, vet, tests (no build)
