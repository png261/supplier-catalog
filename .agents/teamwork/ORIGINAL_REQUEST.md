# Original User Request

## 2026-10-21T08:28:33Z

Build a resilient, minimalist catalog synchronization engine in Go that manages a store's local copy of a supplier's catalog with deterministic out-of-order handling, tombstone preservation, crash resumability, automated testing, and Docker packaging.

Working directory: /Users/png/01. Project/coderpush
Integrity mode: development

## Requirements

### R1. Deterministic Batch Catalog Synchronization & Invariants
- Ingest supplier product batches and store a local copy using an embedded database.
- Enforce idempotency: importing the exact same update multiple times must never duplicate products or drift catalog state.
- Enforce fast-forward out-of-order resolution: incoming older information (based on supplier timestamp) must not overwrite newer local information.
- Enforce tombstone preservation: a product marked as withdrawn from sale must never be restored to active sale by older or subsequent update payloads.

### R2. Fault-Tolerant Resumability & Dead-Letter Isolation
- Guarantee catalog safety under application crash, kill, or outage: transactions must be atomic per batch, ensuring an interruption never erases or corrupts existing data.
- Persist execution checkpoints so staff can resume an incomplete update without re-processing already committed batches.
- Accurately track execution state: an interrupted or partially completed update must never be reported as complete.
- Isolate corrupt or invalid product records (e.g. negative price, missing SKU) into a dead-letter queue for staff attention without failing valid records in the batch.

### R3. Programmatic Mock Data Generator (Infinite Scale)
- Implement a standalone Go generator to stream and export fictional supplier product batches with configurable volume and deterministic random seeds.
- Inject controlled edge-case ratios for duplicates, out-of-order timestamps, withdrawn items, and invalid/corrupt records to support comprehensive testing and stress testing.

### R4. Minimalist Staff Interface & Status Reporting
- Provide a clean, minimalist single-page dashboard embedded directly into the Go binary.
- Allow staff to start an update, view real-time completion progress, inspect a summary of changes (+Added, ~Updated, -Withdrawn, ↷Skipped), view issues needing attention, and resume interrupted updates.

### R5. Security, Test Rigor & CI/CD Packaging
- Configure Husky and Gitleaks to enforce secret leak detection, code formatting, and strict linting on pre-commit and pre-push.
- Package the application into a lightweight multi-stage Docker container with volume persistence.
- Provide a native GitHub Actions CI/CD workflow that runs tests, verifies code coverage, and publishes the container image to GitHub Container Registry (GHCR).

## Acceptance Criteria

### Invariant & Functional Correctness
- [ ] Ingesting the same batch twice results in 0 additional inserts, 0 updates, and identical catalog state.
- [ ] An incoming record with timestamp $T_{new} < T_{old}$ is rejected and counted as skipped.
- [ ] A product with status `WITHDRAWN` is never flipped back to `ACTIVE`.
- [ ] Records with negative prices or empty SKUs are logged to the issues table without halting the batch.

### Crash & Resumability
- [ ] Simulating an interruption midway through a multi-batch sync leaves prior committed batches intact and uncommitted batch changes rolled back.
- [ ] Resuming an interrupted sync continues directly from the uncommitted batch to completion without re-processing completed batches.
- [ ] Interrupted jobs are marked `INTERRUPTED` upon restart and only transition to `COMPLETED` when all batches finish.

### Test Coverage & Security
- [ ] Automated test suite achieves $\ge 90\%$ statement/branch coverage across the project.
- [ ] Multi-iteration test run (`go test -race -count=5 ./...`) passes with 0 data races.
- [ ] Gitleaks scans pass with 0 detected credentials on staged files.

### Packaging & Execution
- [ ] Application builds into a standalone Go executable and a multi-stage Docker image ($\le 25\text{MB}$).
- [ ] `docker compose up` starts the application with persistent volume mounting for the database.
- [ ] Native GitHub Actions workflow compiles the application, runs tests, and publishes the Docker image to GHCR.


## 2026-10-21T16:32:04Z

Continue the implementation of the resilient, minimalist catalog synchronization engine in Go located at `/Users/png/01. Project/coderpush`. 

Milestones 1–4 are fully implemented and verified with 93.4% statement coverage:
- `pkg/domain`, `pkg/storage` (SQLite WAL mode), `pkg/sync` (Invariants A, B, C, D, E, crash recovery).
- `pkg/generator`, `cmd/generator` (streaming mock data generator with seed determinism).
- `web/static/` (zero-node SPA dashboard via `//go:embed`), `pkg/api` (REST control plane, thread-safe SSE broker), `cmd/server`.

Working directory: /Users/png/01. Project/coderpush
Integrity mode: development

## Requirements

### R1. Security Gates & Git Hooks (Milestone 5)
- Configure Husky and Gitleaks (`.husky/pre-commit`, `.husky/pre-push`, `.gitleaks.toml`) to scan staged files and full history for secret leaks.
- Enforce strict `gofmt`, `go vet`, and `golangci-lint` on pre-commit.
- Enforce `go test -race -count=5 ./...` and project-wide statement coverage threshold ($\ge 90\%$) on pre-push.

### R2. Docker Packaging & Local Composition (Milestone 5)
- Provide a lightweight multi-stage `Dockerfile` ($\le 25\text{MB}$ Alpine) producing static Go binaries (`CGO_ENABLED=0`).
- Provide `docker-compose.yml` with a persistent named volume mount (`./data:/app/data`) for SQLite resilience across restarts.

### R3. Native GitHub Actions CI/CD (Milestone 5)
- Implement `.github/workflows/ci-cd.yml` using official native GitHub & Docker actions (`actions/setup-go`, `docker/setup-buildx-action`, `docker/login-action`, `docker/build-push-action`).
- Automatically execute Gitleaks secret scans, multi-iteration race tests, coverage verification ($\ge 90\%$), and publish the container package to GitHub Container Registry (GHCR: `ghcr.io`).

### R4. Project Tooling & Comprehensive Documentation (Milestone 6)
- Provide a `Makefile` with targets for running the server, generator, tests, coverage HTML reports, linter, and Docker commands.
- Provide a production-grade `README.md` including 1-click execution commands (Go, Docker, GHCR package), architecture & data invariants, assumptions, unfinished work, and transparent AI tool disclosure.
- Generate sample fixture batches in `mock_data/` for immediate out-of-the-box demonstration.

## Acceptance Criteria

### Security & Git Hooks
- [ ] Pre-commit hook prevents commits containing leaked credentials, unformatted Go code, or linter errors.
- [ ] Pre-push hook enforces race-free test execution and fails if total statement coverage drops below 90.0%.

### Packaging & CI/CD
- [ ] Multi-stage Docker image builds successfully and image size is $\le 25\text{MB}$.
- [ ] `docker compose up` starts the server and persists database state in the mounted volume across container restarts.
- [ ] GitHub Actions workflow syntax validates cleanly against GitHub schema.

### Documentation & Verification
- [ ] `make test`, `make coverage`, and `make lint` execute cleanly with 0 errors.
- [ ] `README.md` covers all Assessment 1 requirements (Instructions, Assumptions, Unfinished Work, AI disclosure).
