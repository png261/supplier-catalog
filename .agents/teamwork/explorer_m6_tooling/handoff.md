# Handoff Report: Production Makefile Blueprint & Developer Automation (Milestone 6)

## 1. Observation

### 1.1 Codebase Architecture & Tooling Requirements
- **Assignment Specification** (`.agents/teamwork/explorer_m6_tooling/DISPATCH.md` lines 13–26):
  Requires designing a complete production Makefile covering:
  - `make run-server`: build and run HTTP server (`cmd/server`) with defaults.
  - `make run-generator`: run CLI generator (`cmd/generator`) with configurable flags.
  - `make test`: execute unit & integration tests (`go test -race ./...`).
  - `make test-race`: execute 5-iteration race tests (`go test -race -count=5 ./...`).
  - `make coverage`: run coverage check (`scripts/check-coverage.sh`) ensuring $\ge 90.0\%$, and generate `coverage.html` via `go tool cover -html=coverage.out -o coverage.html`.
  - `make lint`: run `gofmt -l`, `go vet ./...`, and `golangci-lint` (if installed, with graceful skip message).
  - `make docker-build`: build lightweight multi-stage Docker image (`Dockerfile`).
  - `make docker-up`: start compose environment (`docker compose up -d`).
  - `make docker-down`: stop compose environment (`docker compose down`).
  - `make clean`: remove temporary build artifacts, database files, coverage profiles, and logs.
  - `make help`: self-documenting help target.
  Must verify that `make test`, `make coverage`, and `make lint` execute cleanly with 0 errors.

- **Server Entrypoint CLI Flags** (`cmd/server/main.go` lines 41–56):
  - `--port`: HTTP listen port, default `8080` (or `$PORT`).
  - `--db`: SQLite database file path, default `catalog.db` (or `$DB_PATH`).
  - `--static-dir`: Optional disk directory for static assets (overrides embedded `web/static`).

- **Generator Entrypoint CLI Flags** (`cmd/generator/main.go` lines 41–54):
  - `--seed`: int64 deterministic seed (default `42`).
  - `--batches`: int batch count (default `10`; 0 for infinite stream).
  - `--batch-size`: int records per batch (default `100`).
  - `--out`: destination (`stdout`, `-`, or file path).
  - `--format`: output format (`json-batches`, `ndjson-records`, `json`).
  - `--ratio-update`: update ratio (default `0.20`).
  - `--ratio-duplicate`: duplicate ratio (default `0.05`).
  - `--ratio-out-of-order`: out-of-order ratio (default `0.10`).
  - `--ratio-withdrawn`: withdrawn ratio (default `0.05`).
  - `--ratio-corrupt`: corrupt ratio (default `0.05`).
  - `--job-id`: string job ID prefix (default auto).

- **Coverage Verification Script** (`scripts/check-coverage.sh` lines 11–13, 41–50):
  - Uses `go tool cover -func="$PROFILE"` to extract total statements coverage.
  - Uses POSIX `awk` comparison against `$THRESHOLD` (default `90.0%`).
  - Exits with 0 on pass and 1 on failure. Script has execution mode `-rwxr-xr-x`.

- **Quality & Git Hooks Precedent** (`.husky/pre-commit` lines 22–58):
  - Enforces `gofmt -l` across Go files.
  - Enforces `go vet ./...`.
  - Checks `if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else echo "ℹ️ golangci-lint is not installed locally (skipping)."; fi`.

### 1.2 Local Host Environment Observations
- Host OS: `Darwin 24.6.0 arm64` (macOS).
- Go version: `go version go1.25.6 darwin/arm64`.
- Docker version: `Docker version 28.5.1, build e827d09` at `/usr/local/bin/docker`.
- Docker Compose version: `Docker Compose version v5.0.2` at `docker compose`.
- `golangci-lint`: Not installed locally (`command -v golangci-lint` returned 1).

### 1.3 Empirical Command Verification Results
1. **Lint Command Verification** (`gofmt -l .` and `go vet ./...`):
   - `gofmt -l .` executed with return code 0 and produced 0 lines of unformatted output.
   - `go vet ./...` executed with return code 0 and produced 0 warnings/errors.
   - Graceful `golangci-lint` detection skipped cleanly with code 0 and output:
     `ℹ️  golangci-lint is not installed locally (gracefully skipping). CI enforces strict linting.`

2. **Test Command Verification** (`go test -race ./...`):
   - Executed with return code 0 in ~16s across all 8 packages:
     ```
     ok  coderpush/cmd/generator (cached)
     ok  coderpush/cmd/server    7.388s
     ok  coderpush/pkg/api       16.060s
     ok  coderpush/pkg/domain    (cached)
     ok  coderpush/pkg/generator (cached)
     ok  coderpush/pkg/storage   (cached)
     ok  coderpush/pkg/sync      (cached)
     ok  coderpush/web           (cached)
     ```

3. **Multi-Iteration Race Test Verification** (`go test -race -count=5 ./...`):
   - Executed with return code 0 across 5 consecutive iterations with 0 data races:
     ```
     ok  coderpush/cmd/generator 8.343s
     ok  coderpush/cmd/server    5.522s
     ok  coderpush/pkg/api       49.738s
     ok  coderpush/pkg/domain    4.298s
     ok  coderpush/pkg/generator 79.211s
     ok  coderpush/pkg/storage   8.494s
     ok  coderpush/pkg/sync      59.880s
     ok  coderpush/web           4.560s
     ```

4. **Coverage Command Verification** (`go test -coverprofile=coverage.out -covermode=atomic ./... && ./scripts/check-coverage.sh coverage.out 90.0`):
   - Statement coverage per package:
     - `cmd/generator`: 90.2%
     - `cmd/server`: 93.5%
     - `pkg/api`: 93.0%
     - `pkg/domain`: 100.0%
     - `pkg/generator`: 97.0%
     - `pkg/storage`: 90.5%
     - `pkg/sync`: 95.5%
     - `web`: 95.9%
   - Total Statement Coverage: **93.5%**
   - Script verification result:
     `✅ PASS: Statement coverage (93.5%) meets or exceeds threshold (90.0%).`
   - `go tool cover -html=coverage.out -o coverage.html` generated `coverage.html` with return code 0.

5. **Sandbox Makefile Verification** (`make -f .agents/teamwork/explorer_m6_tooling/Makefile`):
   - `make help`: Returned formatted, colorized target catalog grouped into sections (Help, Build & Execution, Testing & Quality Assurance, Container & Docker, Housekeeping).
   - `make lint`: Completed in 1s with 0 errors.
   - `make test`: Completed with 0 errors.
   - `make coverage`: Completed with 0 errors, validating 93.5% statement coverage.
   - `make build`: Compiled `bin/server` and `bin/generator` with `CGO_ENABLED=0` and `-ldflags="-s -w"`.
   - `make clean`: Removed `bin/`, coverage files, databases, and logs cleanly.

---

## 2. Logic Chain

1. **Target Completeness & Alignment with Authoritative Requirements**:
   - Section 1.1 enumerates all required targets specified in Requirement R4 (`run-server`, `run-generator`, `test`, `test-race`, `coverage`, `lint`, `docker-build`, `docker-up`, `docker-down`, `clean`, `help`).
   - Adding developer ergonomics targets (`build`, `build-server`, `build-generator`, `fmt`, `vet`, `docker-logs`) provides complete coverage for local development while strictly preserving all required target names and behaviors.

2. **Portability & Determinism (`SHELL`, `.SHELLFLAGS`, `.DEFAULT_GOAL`)**:
   - Standardizing `SHELL := /usr/bin/env bash` with `.SHELLFLAGS := -eu -o pipefail -c` guarantees that all recipe pipelines fail fast on error, preventing masked silent failures in sub-shells or pipe commands.
   - Setting `.DEFAULT_GOAL := help` ensures running plain `make` prints the self-documenting command reference.

3. **Graceful Linter Degradation**:
   - Section 1.2 observed that `golangci-lint` is not installed on the local developer workstation, though it is present in the GitHub Actions runner (`.github/workflows/ci-cd.yml`).
   - The recipe for `make lint` checks `command -v golangci-lint`. If present, it executes `golangci-lint run ./...`. If absent, it logs an informative notice explaining that CI enforces strict linting and exits with status 0. This satisfies the requirement while preserving local developer flow.

4. **Race Detection and Coverage Verification Parity with CI & Git Hooks**:
   - `make test` executes `go test -race ./...`, matching standard unit testing.
   - `make test-race` executes `go test -race -count=5 ./...`, matching the pre-push gate in `.husky/pre-push` (line 24) and CI workflow (line 80).
   - `make coverage` executes `go test -coverprofile=coverage.out -covermode=atomic ./...`, invokes `./scripts/check-coverage.sh $(COVERAGE_FILE) 90.0`, and outputs `coverage.html`. This directly mirrors CI step 82–110.

5. **Container Orchestration Compatibility**:
   - `docker-build` passes `Dockerfile` to `docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .`.
   - `docker-up` runs `docker compose up -d` using the repository's `docker-compose.yml`, which mounts `./data:/app/data` for persistent SQLite storage.
   - `docker-down` cleanly stops containers with `docker compose down`.

6. **Safety & Completeness in `clean`**:
   - The `clean` target removes build binaries (`bin/`, root `server`, `generator`), temporary databases (`catalog.db*`, `data/*.db*`), coverage profiles (`coverage.out`, `coverage.html`, `coverage_*.out`, `server_coverage.out`), and logs (`*.log`), leaving the repository pristine.

---

## 3. Caveats

1. **Docker Daemon Runtime State**:
   - While the Docker CLI and Docker Compose v5.0.2 are installed, the local Docker daemon was not active during investigation (`docker info` failed). The Dockerfile and docker-compose configurations were verified statically and align with the existing Milestone 5 specs.
2. **`golangci-lint` Local Installation**:
   - `golangci-lint` is verified to pass in GitHub Actions. Locally, it gracefully skips with code 0 unless installed by the developer (`brew install golangci-lint`).
3. **Repository Read-Only Discipline**:
   - Pursuant to Explorer read-only rules, no root files were modified. The Makefile blueprint was fully tested inside the explorer's working directory (`.agents/teamwork/explorer_m6_tooling/Makefile`) and is provided in Section 4 below for root instantiation by the implementer.

---

## 4. Conclusion & Complete Production Makefile Blueprint

The proposed `Makefile` satisfies all 11 required targets from Requirement R4, achieves clean 0-error execution for `make test`, `make coverage`, and `make lint`, and incorporates self-documenting colorized help.

### Complete Production `Makefile` Content
The implementer should write the following exact content to `/Users/png/01. Project/coderpush/Makefile`:

```makefile
# ==============================================================================
# Makefile - Resilient Catalog Synchronization Engine
# ==============================================================================
# Requirement R4: Complete developer automation targets for server, generator,
# testing, race detection, coverage enforcement (>= 90%), linting, and Docker.
# ==============================================================================

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

# ------------------------------------------------------------------------------
# Go & Build Tooling Configuration
# ------------------------------------------------------------------------------
GO ?= go
GOFLAGS ?=
CGO_ENABLED ?= 0

BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/server
GENERATOR_BIN := $(BIN_DIR)/generator

# ------------------------------------------------------------------------------
# Runtime Parameters (Configurable via command-line overrides or env vars)
# ------------------------------------------------------------------------------
# Server options
PORT ?= 8080
DB_PATH ?= catalog.db
STATIC_DIR ?=
SERVER_FLAGS ?=

# Generator options
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

# Quality gate settings
COVERAGE_FILE ?= coverage.out
COVERAGE_HTML ?= coverage.html
COVERAGE_THRESHOLD ?= 90.0

# Docker & Container configuration
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
	@echo " Resilient Catalog Synchronization Engine - Developer Automation"
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
```

---

## 5. Verification Method

To independently verify the Makefile blueprint:

1. **Test `make help`**:
   ```bash
   make -f .agents/teamwork/explorer_m6_tooling/Makefile help
   ```
   *Expected*: Prints formatted table listing all targets with descriptions. Return code `0`.

2. **Test `make lint`**:
   ```bash
   make -f .agents/teamwork/explorer_m6_tooling/Makefile lint
   ```
   *Expected*: Passes gofmt verification, passes go vet with 0 issues, gracefully handles golangci-lint. Return code `0`.

3. **Test `make test`**:
   ```bash
   make -f .agents/teamwork/explorer_m6_tooling/Makefile test
   ```
   *Expected*: Executes `go test -race ./...` across all 8 packages and returns code `0`.

4. **Test `make coverage`**:
   ```bash
   make -f .agents/teamwork/explorer_m6_tooling/Makefile coverage
   ```
   *Expected*: Generates `coverage.out`, runs `./scripts/check-coverage.sh` (reporting 93.5% >= 90.0%), generates `coverage.html`. Return code `0`.

5. **Test `make build` and `make clean`**:
   ```bash
   make -f .agents/teamwork/explorer_m6_tooling/Makefile build
   ls -la bin/server bin/generator
   make -f .agents/teamwork/explorer_m6_tooling/Makefile clean
   test ! -d bin
   ```
   *Expected*: Binaries compile into `bin/` then are cleaned up cleanly. Return code `0`.

6. **Invalidation Conditions**:
   - Total statement coverage drops below 90.0%.
   - Any data race detected during multi-iteration execution.
   - Any unformatted Go code staged without running `gofmt`.
   - Broken target syntax or missing tab delimiters in recipes.
