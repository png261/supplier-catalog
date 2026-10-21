# ==============================================================================
# Makefile - Resilient Catalog Synchronization Engine
# ==============================================================================
SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

# ------------------------------------------------------------------------------
# Go & Build Configuration
# ------------------------------------------------------------------------------
GO ?= go
GOFLAGS ?=
CGO_ENABLED ?= 0

BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/server
GENERATOR_BIN := $(BIN_DIR)/generator

# ------------------------------------------------------------------------------
# Runtime Parameters (Configurable via CLI arguments or environment)
# ------------------------------------------------------------------------------
# Server parameters
PORT ?= 8080
DB_PATH ?= catalog.db
STATIC_DIR ?=
SERVER_FLAGS ?=

# Generator parameters
SEED ?= 42
BATCHES ?= 10
BATCH_SIZE ?= 100
OUT ?= stdout
FORMAT ?= json-batches
RATIO_UPDATE ?= 0.20
RATIO_DUPLICATE ?= 0.05
RATIO_OUT_OF_ORDER ?= 0.10
RATIO_WITHDRAWN ?= 0.05
RATIO_CORRUPT ?= 0.05
JOB_ID ?=
GEN_FLAGS ?=

# Coverage settings
COVERAGE_FILE ?= coverage.out
COVERAGE_HTML ?= coverage.html
COVERAGE_THRESHOLD ?= 90.0

# Docker settings
IMAGE_NAME ?= coderpush-catalog-engine
IMAGE_TAG ?= latest
DOCKER ?= docker
COMPOSE ?= docker compose

# ------------------------------------------------------------------------------
# Targets
# ------------------------------------------------------------------------------

##@ Help
.PHONY: help
help: ## Display this self-documenting help menu
	@echo "=================================================================="
	@echo " Resilient Catalog Synchronization Engine - Developer Tooling"
	@echo "=================================================================="
	@echo "Usage:"
	@echo "  make <target> [VARIABLE=value]"
	@echo ""
	@awk 'BEGIN {FS = ":.*##"; printf ""} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@echo ""

##@ Build & Execution
.PHONY: build build-server build-generator
build: build-server build-generator ## Compile both server and generator standalone binaries into bin/

build-server: ## Compile standalone HTTP server binary (bin/server)
	@echo "==> Building server binary (CGO_ENABLED=$(CGO_ENABLED))..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -trimpath -ldflags="-s -w" -o $(SERVER_BIN) ./cmd/server
	@echo "✅ Compiled: $(SERVER_BIN)"

build-generator: ## Compile standalone mock generator binary (bin/generator)
	@echo "==> Building generator binary (CGO_ENABLED=$(CGO_ENABLED))..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -trimpath -ldflags="-s -w" -o $(GENERATOR_BIN) ./cmd/generator
	@echo "✅ Compiled: $(GENERATOR_BIN)"

.PHONY: run-server run-generator
run-server: ## Build and run HTTP catalog server with defaults (cmd/server)
	@echo "==> Starting catalog server on :$(PORT) (database: $(DB_PATH))..."
	$(GO) run ./cmd/server -port $(PORT) -db $(DB_PATH) $(if $(STATIC_DIR),-static-dir $(STATIC_DIR),) $(SERVER_FLAGS)

run-generator: ## Run CLI mock data generator with configurable flags (cmd/generator)
	@echo "==> Running mock catalog generator (seed=$(SEED), batches=$(BATCHES), size=$(BATCH_SIZE))..."
	$(GO) run ./cmd/generator \
		-seed $(SEED) \
		-batches $(BATCHES) \
		-batch-size $(BATCH_SIZE) \
		-out $(OUT) \
		-format $(FORMAT) \
		-ratio-update $(RATIO_UPDATE) \
		-ratio-duplicate $(RATIO_DUPLICATE) \
		-ratio-out-of-order $(RATIO_OUT_OF_ORDER) \
		-ratio-withdrawn $(RATIO_WITHDRAWN) \
		-ratio-corrupt $(RATIO_CORRUPT) \
		$(if $(JOB_ID),-job-id $(JOB_ID),) \
		$(GEN_FLAGS)

##@ Testing & Quality Assurance
.PHONY: test test-race coverage lint fmt vet
test: ## Execute unit and integration tests with race detector
	@echo "==> Executing test suite with race detector (go test -race ./...)..."
	$(GO) test -race ./...

test-race: ## Execute 5-iteration race stress tests (go test -race -count=5 ./...)
	@echo "==> Executing 5-iteration race stress tests (go test -race -count=5 ./...)..."
	$(GO) test -race -count=5 ./...

coverage: ## Execute coverage gate (scripts/check-coverage.sh >= 90%) and generate HTML report
	@echo "==> Generating coverage profile (mode=atomic)..."
	$(GO) test -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	@echo "==> Verifying statement coverage threshold (>= $(COVERAGE_THRESHOLD)%)..."
	@if [ -x "./scripts/check-coverage.sh" ]; then \
		./scripts/check-coverage.sh $(COVERAGE_FILE) $(COVERAGE_THRESHOLD); \
	else \
		bash scripts/check-coverage.sh $(COVERAGE_FILE) $(COVERAGE_THRESHOLD); \
	fi
	@echo "==> Generating HTML coverage report ($(COVERAGE_HTML))..."
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "✅ Coverage report successfully generated: $(COVERAGE_HTML)"

lint: ## Run gofmt -l, go vet ./..., and golangci-lint (with graceful fallback)
	@echo "==> [1/3] Verifying Go code formatting (gofmt)..."
	@UNFORMATTED=$$($(GO)fmt -l . | grep -v '^\.agents' || true); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "❌ ERROR: Unformatted Go files detected:" >&2; \
		echo "$$UNFORMATTED" >&2; \
		echo "Run 'make fmt' to automatically format files." >&2; \
		exit 1; \
	fi; \
	echo "✅ gofmt: all files formatted properly."
	@echo "==> [2/3] Running static analysis (go vet ./...)..."
	@$(GO) vet ./...
	@echo "✅ go vet: 0 static issues detected."
	@echo "==> [3/3] Checking advanced linter (golangci-lint)..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
		echo "✅ golangci-lint: passed with 0 issues."; \
	else \
		echo "ℹ️  golangci-lint is not installed locally (gracefully skipping). CI enforces strict linting."; \
	fi

fmt: ## Format all Go source files according to Go standard formatting
	@echo "==> Formatting Go source files..."
	gofmt -s -w .
	@echo "✅ gofmt formatting complete."

vet: ## Run go vet static analyzer across all packages
	@echo "==> Running go vet ./... analysis..."
	$(GO) vet ./...

##@ Container & Docker
.PHONY: docker-build docker-up docker-down docker-logs
docker-build: ## Build lightweight multi-stage Docker image (<= 25MB)
	@echo "==> Building Docker image $(IMAGE_NAME):$(IMAGE_TAG)..."
	$(DOCKER) build -t $(IMAGE_NAME):$(IMAGE_TAG) -f Dockerfile .
	@echo "✅ Docker build complete: $(IMAGE_NAME):$(IMAGE_TAG)"

docker-up: ## Start application container in background via docker compose
	@echo "==> Starting containers via Docker Compose (detached)..."
	$(COMPOSE) up -d

docker-down: ## Stop and remove docker compose containers and networks
	@echo "==> Stopping Docker Compose containers..."
	$(COMPOSE) down

docker-logs: ## Tail docker compose container logs
	$(COMPOSE) logs -f

##@ Housekeeping
.PHONY: clean
clean: ## Remove temporary build binaries, databases, coverage profiles, and logs
	@echo "==> Cleaning temporary build artifacts, databases, coverage files, and logs..."
	@rm -rf $(BIN_DIR)
	@rm -f server generator
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML) coverage_*.out server_coverage.out *.coverprofile
	@rm -f catalog.db catalog.db-wal catalog.db-shm
	@rm -rf data/*.db data/*.db-wal data/*.db-shm
	@rm -f *.log
	@rm -rf -- -h --help
	@echo "✅ Clean complete."
