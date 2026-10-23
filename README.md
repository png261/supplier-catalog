# Resilient Catalog Synchronization Engine in Go

[![Live Demo](https://img.shields.io/badge/Live%20Demo-Cloudflare%20Pages%20%2B%20Workers-orange?style=flat&logo=cloudflare)](https://supplier-catalog-frontend.pages.dev)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat&logo=go)](https://go.dev/)
[![CI/CD Pipeline](https://github.com/png261/supplier-catalog/actions/workflows/ci-cd.yml/badge.svg)](https://github.com/png261/supplier-catalog/actions/workflows/ci-cd.yml)
[![Statement Coverage](https://img.shields.io/badge/Coverage-93.5%25-brightgreen?style=flat&logo=codecov)](scripts/check-coverage.sh)
[![Docker Image Size](https://img.shields.io/badge/Docker%20Size-21.3MB-blue?style=flat&logo=docker)](Dockerfile)
[![Security Gates](https://img.shields.io/badge/Gitleaks-Passed%20(0%20leaks)-success?style=flat&logo=shield)](.gitleaks.toml)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A resilient, minimalist catalog synchronization engine built in pure Go (`CGO_ENABLED=0`) utilizing embedded SQLite in Write-Ahead Logging (WAL) mode. Designed to ingest high-volume supplier catalog streams with mathematical idempotency, deterministic out-of-order resolution, permanent tombstone preservation, crash resumability, dead-letter quarantine, and zero external runtime dependencies.

Includes an embedded, zero-node real-time staff web dashboard via `//go:embed` with live Server-Sent Events (SSE), row-by-row diff previews, per-product modification history, dead-letter remediation, and independent update rollbacks.

---

## Live Deployment, API & Documentation Links

| Resource | Live Cloud URL (Cloudflare Edge) | Local / Docker Container URL |
|---|---|---|
| **Frontend Web Dashboard** | **[https://supplier-catalog-frontend.pages.dev](https://supplier-catalog-frontend.pages.dev)** | **[http://localhost:8080](http://localhost:8080)** |
| **Multi-Tenant Direct Link** | **[https://supplier-catalog-frontend.pages.dev/?shop=store-flagship](https://supplier-catalog-frontend.pages.dev/?shop=store-flagship)** | **[http://localhost:8080/?shop=store-flagship](http://localhost:8080/?shop=store-flagship)** |
| **Backend REST API** | **[https://catalog-sync-worker.ezcert.workers.dev](https://catalog-sync-worker.ezcert.workers.dev)** | **[http://localhost:8080/api/jobs](http://localhost:8080/api/jobs)** |
| **API Docs (Swagger UI)** | **[https://catalog-sync-worker.ezcert.workers.dev/swagger/index.html](https://catalog-sync-worker.ezcert.workers.dev/swagger/index.html)** | **[http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)** |
| **OpenAPI 2.0 Spec (JSON)** | **[https://catalog-sync-worker.ezcert.workers.dev/swagger/doc.json](https://catalog-sync-worker.ezcert.workers.dev/swagger/doc.json)** | **[http://localhost:8080/swagger/doc.json](http://localhost:8080/swagger/doc.json)** |
| **Full HD Video Demo (1080p)** | **[https://supplier-catalog-frontend.pages.dev/demo_walkthrough.mp4](https://supplier-catalog-frontend.pages.dev/demo_walkthrough.mp4)** | **[`./demo_walkthrough.mp4`](./demo_walkthrough.mp4)** *(1920x1080 Full HD, clear natural-speed voiceover)* |
| **GitHub Repository** | **[https://github.com/png261/supplier-catalog](https://github.com/png261/supplier-catalog)** | `git clone https://github.com/png261/supplier-catalog.git` |

---

## Table of Contents

1. [Live Deployment, API & Documentation Links](#live-deployment-api--documentation-links)
2. [Executive Overview & Design Philosophy](#executive-overview--design-philosophy)
3. [Architecture & The 5 Core Invariants](#architecture--the-5-core-invariants)
4. [1-Click Quickstart & Execution Guide (Native Go, Docker Container & Cloud)](#1-click-quickstart--execution-guide)
5. [Out-of-the-Box Demo with Sample Fixtures](#out-of-the-box-demo-with-sample-fixtures)
6. [Embedded Staff Web Dashboard & Real-Time SSE](#embedded-staff-web-dashboard--real-time-sse)
7. [Developer Tooling & Verification Guide](#developer-tooling--verification-guide)
8. [REST API & SSE Event Reference](#rest-api--sse-event-reference)
9. [Assessment 1: Mandatory Requirements](#assessment-1-mandatory-requirements)
   - [Assumptions & Architectural Trade-offs](#assumptions--architectural-trade-offs)
   - [Unfinished Work & Production Roadmap](#unfinished-work--production-roadmap)
   - [Transparent AI Tool Disclosure](#transparent-ai-tool-disclosure)

---

## Executive Overview & Design Philosophy

### The Problem
Retailers and e-commerce stores depend on external supplier data feeds to maintain their product catalogs. In practice, supplier feeds are notoriously unreliable:
- **Network Re-deliveries & Duplication**: Network retries cause identical batches or duplicate records to arrive repeatedly.
- **Out-of-Order Delivery**: Distributed supplier infrastructure emits updates that arrive out of chronological order ($T_{new} < T_{old}$).
- **Accidental Resurrection**: Previously discontinued or legally withdrawn products are inadvertently marked active in older feed replays.
- **Corrupt & Malformed Records**: Payloads with negative prices, whitespace/empty SKUs, or missing timestamps cause bulk ingestion jobs to fail abruptly.
- **Process Termination & Crashes**: Network timeouts, OOM kills, or power outages midway through a sync leave catalog databases in an undefined, corrupted, or unresumable state.

### The Resilient Minimalist Solution
Instead of requiring heavy external infrastructure (such as Kafka, Redis, or dedicated PostgreSQL clusters), this engine delivers **enterprise-grade ACID resilience within a self-contained, lightweight Go executable**:
- **Pure Go SQLite (`modernc.org/sqlite`)**: Embedded relational storage operating with WAL mode, immediate write locking, and single-writer concurrency. Zero CGo dependencies.
- **Strict Invariant State Machine**: A mathematical evaluation engine enforcing Idempotency, Monotonic Timestamps, Tombstone Preservation, and Dead-Letter Isolation.
- **Per-Batch Atomic Transactions**: Guaranteed all-or-nothing execution per batch ($T_{B_i}$). An interruption midway through a multi-batch sync leaves prior committed batches intact and the uncommitted batch rolled back.
- **Crash Recovery & Checkpoint Resumability**: On startup, interrupted jobs are identified and can resume execution directly from $last\_committed\_batch + 1$.
- **Zero-Node Single Binary SPA**: The complete responsive web interface is compiled directly into the binary via Go's native `embed.FS`. No Node.js runtime, npm, or webpack needed in production.

---

## Architecture & The 5 Core Invariants

```
                      ┌──────────────────────────────────────────────┐
                      │            Supplier Batch Stream             │
                      │  (Generator CLI, REST API, JSON Fixtures)    │
                      └──────────────────────┬───────────────────────┘
                                             │
                                             ▼
                      ┌──────────────────────────────────────────────┐
                      │           pkg/sync: SyncEngine               │
                      │  ┌────────────────────────────────────────┐  │
                      │  │ Invariant D: Dead-Letter Validation    │  │
                      │  └───────────────────┬────────────────────┘  │
                      │                      │                       │
                      │       [Valid]        │     [Malformed]       │
                      │          ▼           │          ▼            │
                      │  ┌──────────────┐    │   ┌────────────────┐  │
                      │  │ Invariant A: │    │   │ dead_letter_   │  │
                      │  │ Idempotency  │    │   │ issues table   │  │
                      │  └──────┬───────┘    │   └────────────────┘  │
                      │         ▼            │                       │
                      │  ┌──────────────┐    │                       │
                      │  │ Invariant C: │    │                       │
                      │  │ Tombstones   │    │                       │
                      │  └──────┬───────┘    │                       │
                      │         ▼            │                       │
                      │  ┌──────────────┐    │                       │
                      │  │ Invariant B: │    │                       │
                      │  │ Timestamps   │    │                       │
                      │  └──────┬───────┘    │                       │
                      │         ▼            │                       │
                      │  ┌──────────────┐    │                       │
                      │  │ Apply Upsert │    │                       │
                      │  └──────┬───────┘    │                       │
                      │         │            │                       │
                      │  ┌──────┴────────────┴────────────────────┐  │
                      │  │ Invariant E: Atomic Batch Commit       │  │
                      │  │ (Products + Checkpoint + Progress)     │  │
                      │  └───────────────────┬────────────────────┘  │
                      └──────────────────────┼───────────────────────┘
                                             │
                      ┌──────────────────────┴───────────────────────┐
                      │       Embedded SQLite Storage (WAL Mode)     │
                      │   products | sync_jobs | batch_checkpoints   │
                      └──────────────────────┬───────────────────────┘
                                             │
                                             ▼
                      ┌──────────────────────────────────────────────┐
                      │      Real-Time SSE Broker (/api/events)      │
                      │   Live Progress Bar, Metric Cards, Events    │
                      └──────────────────────────────────────────────┘
```

### Invariant A: Deterministic Idempotency
- **Formal Rule**: Re-ingesting an identical record payload must produce zero catalog state mutations and zero drift.
  $$\forall R \in \text{Batch}, \quad \text{if } P_{local} \equiv R \implies \Delta(\text{Catalog}) = \emptyset, \quad \text{Skipped} \gets \text{Skipped} + 1$$
- **Verification**: Ingesting the identical batch multiple times results in exactly `0 Added`, `0 Updated`, `0 Withdrawn`, and `N Skipped`. Catalog checksum remains byte-for-byte identical.

### Invariant B: Fast-Forward Timestamps (Temporal Monotonicity)
- **Formal Rule**: Incoming records with timestamps older than or equal to local state are rejected.
  $$\text{If } R.Timestamp \le P_{local}.SupplierTimestamp \implies \text{Reject update}, \quad \text{Skipped} \gets \text{Skipped} + 1$$
- **Verification**: An incoming record with timestamp $T_{new} < T_{old}$ does not overwrite the existing record. The local product retains its newer timestamp and data.

### Invariant C: Tombstone Preservation (Monotonic Non-Decreasing Status)
- **Formal Rule**: Product lifecycle is strictly monotonic. Once marked `WITHDRAWN`, a product can **never** be reactivated to `ACTIVE` by subsequent or out-of-order updates.
  $$P_{local}.Status = \text{WITHDRAWN} \implies \forall R, \quad R.Status \gets \text{WITHDRAWN}$$
- **Verification**: Submitting a payload with status `ACTIVE` for a previously withdrawn SKU is skipped, and the catalog retains `WITHDRAWN`.

### Invariant D: Dead-Letter Isolation (Non-Halting Quarantine)
- **Formal Rule**: Records violating schema integrity (empty/whitespace SKU, negative price, invalid status enum, zero timestamp) are quarantined into the `dead_letter_issues` table without aborting the batch transaction.
  $$\text{Invalid}(R) \implies \text{Quarantine}(R) \land \text{DeadLetter} \gets \text{DeadLetter} + 1$$
- **Verification**: A batch containing corrupt records successfully commits all valid records while saving detailed failure diagnostics for staff inspection.

### Invariant E: Atomic Batch Transactions & Resumability
- **Formal Rule**: Each batch $B_i$ executes inside an isolated SQLite transaction boundary committing products, dead-letter records, checkpoints, and cumulative job progress simultaneously.
  $$\mathcal{T}(B_i) = \{ \text{UpsertProducts}, \text{RecordDeadLetters}, \text{WriteCheckpoint}(i), \text{UpdateProgress}(i) \}$$
- **Verification**: Simulating a crash at batch $K$ rolls back uncommitted changes in batch $K$. Resuming the job continues seamlessly from batch $K$ without re-executing batches $0 \dots K-1$.

---

## 1-Click Quickstart & Execution Guide

### Prerequisites
- **Go**: Version 1.25 or higher (for native compilation)
- **Docker & Docker Compose**: (Optional, for containerized execution)
- **Make**: (Optional, for developer task automation)

---

### Option 1: Native Go Execution (Fastest for Local Development)

1. **Clone the repository**:
   ```bash
   git clone https://github.com/png261/supplier-catalog.git
   cd supplier-catalog
   ```

2. **Start the server with Makefile**:
   ```bash
   make run-server
   ```
   *Or execute natively via Go*:
   ```bash
   go run ./cmd/server -port 8080 -db ./catalog.db
   ```

3. **Access the Staff Web Dashboard & API Docs**:
   - **Frontend Dashboard**: [http://localhost:8080](http://localhost:8080)
   - **REST API**: [http://localhost:8080/api/jobs](http://localhost:8080/api/jobs)
   - **Swagger API Docs**: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html) (or [http://localhost:8080/docs](http://localhost:8080/docs))

---

### Option 2: Docker Compose (Recommended Turnkey Container Execution)

Docker Compose builds the static Alpine image ($\le 25\text{MB}$) and mounts a persistent host volume (`./data:/app/data`) ensuring catalog and audit logs persist across container restarts.

1. **Build and start the container in detached mode**:
   ```bash
   make docker-up
   # Or directly via Docker Compose:
   mkdir -p data && chmod 777 data
   docker compose up -d --build
   ```

2. **Inspect container health and logs**:
   ```bash
   docker compose ps
   docker compose logs -f
   ```

3. **Access the containerized application**:
   - **Frontend UI**: [http://localhost:8080](http://localhost:8080)
   - **Backend API**: [http://localhost:8080/api/jobs](http://localhost:8080/api/jobs)
   - **Swagger Docs**: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)

4. **Verify volume persistence across restarts**:
   ```bash
   docker compose restart
   ```

5. **Stop the service**:
   ```bash
   make docker-down
   # Or: docker compose down
   ```

---

### Option 3: GitHub Container Registry (GHCR) Package

Run the pre-built, hardened production image directly from GitHub Container Registry without installing Go or compilers:

```bash
# Pull and run the latest image with persistent volume
mkdir -p data && chmod 777 data
docker run -d \
  --name catalog-engine \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  --restart unless-stopped \
  ghcr.io/png261/supplier-catalog:latest
```

Open [http://localhost:8080](http://localhost:8080).

---

### Option 4: Cloudflare Fullstack Deployment (Pages + Worker + D1 Database)

Deploy the entire stack serverlessly to Cloudflare with zero servers to manage:
- **Frontend SPA**: Hosted globally on **Cloudflare Pages** (Vite + React)
- **Backend API**: Running at Edge on **Cloudflare Workers** (Hono + TypeScript)
- **Database**: Native serverless SQLite on **Cloudflare D1** with multi-tenant data isolation

```bash
# 1. Run worker locally with local D1 emulator
make cf-dev

# 2. Run automated test suite verifying all 5 invariants
make cf-test

# 3. Deploy fullstack to Cloudflare (D1 migrations + Worker + Pages)
make deploy-cloudflare
```

Test multi-tenancy on the live deployment:
- `https://supplier-catalog-frontend.pages.dev/?shop=shop-nike-1`
- `https://supplier-catalog-frontend.pages.dev/?shop=shop-adidas-2`

---

### Running the Standalone Mock Data Generator CLI

The standalone generator streams simulated supplier batches with seed determinism and calibrated anomaly ratios:

```bash
# Generate 10 batches of 100 records each to stdout (JSON batches format)
go run ./cmd/generator -batches 10 -batch-size 100 -seed 42

# Export 5 batches with calibrated edge cases to a file
go run ./cmd/generator \
  -batches 5 \
  -batch-size 50 \
  -seed 12345 \
  -ratio-update 0.20 \
  -ratio-duplicate 0.05 \
  -ratio-out-of-order 0.10 \
  -ratio-withdrawn 0.05 \
  -ratio-corrupt 0.05 \
  -out ./simulated_feed.json

# Stream infinite NDJSON records to stdout (for pipeline piping)
go run ./cmd/generator -batches 0 -format ndjson-records
```

---

## Out-of-the-Box Demo with Sample Fixtures

The repository includes pre-built fixture batches in `mock_data/` demonstrating all 5 Core Invariants using a progressive coffee catalog scenario.

### Automated Demo Walkthrough (1-Click)

While the server is running (`make run-server`), open a new terminal and execute:
```bash
make ingest-demo
# Or run directly: ./scripts/ingest-fixtures.sh
```

### Manual Step-by-Step Walkthrough via curl

#### Step 1: Ingest Initial Catalog Baseline (Batch 1)
Inserts 10 baseline coffee bean and equipment products with status `ACTIVE`:
```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "batches",
    "job_id": "demo-step-1",
    "batches": ['"$(cat mock_data/batch_001_initial.json)"']
  }'
```
*Expected Result*: `Added: 10, Updated: 0, Withdrawn: 0, Skipped: 0, DeadLetter: 0`.

#### Step 2: Ingest Updates, Idempotency & Tombstones (Batch 2)
Tests **Invariant A** (replay of identical items), price updates, and **Invariant C** (marking 2 items `WITHDRAWN`):
```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "batches",
    "job_id": "demo-step-2",
    "batches": ['"$(cat mock_data/batch_002_updates_and_idempotency.json)"']
  }'
```
*Expected Result*: `Added: 0, Updated: 5, Withdrawn: 2, Skipped: 3, DeadLetter: 0`.

#### Step 3: Ingest Stale Timestamps & Malformed Payloads (Batch 3)
Tests **Invariant B** (rejecting $T_{new} < T_{old}$), **Invariant D** (quarantining negative prices and empty SKUs to DLQ), and verifies that valid sibling records still commit:
```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "batches",
    "job_id": "demo-step-3",
    "batches": ['"$(cat mock_data/batch_003_anomalies_and_deadletter.json)"']
  }'
```
*Expected Result*: `Added: 1, Updated: 1, Withdrawn: 0, Skipped: 2, DeadLetter: 3`.

#### Step 4: Attempt Tombstone Reactivation (Batch 4)
Tests **Invariant C** by attempting to flip `WITHDRAWN` items back to `ACTIVE` with a newer timestamp:
```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "batches",
    "job_id": "demo-step-4",
    "batches": ['"$(cat mock_data/batch_004_tombstone_reactivation_attempt.json)"']
  }'
```
*Expected Result*: `Added: 0, Updated: 1, Withdrawn: 0, Skipped: 2, DeadLetter: 0`. The withdrawn items remain strictly `WITHDRAWN`.

#### Step 5: Inspect Quarantined Issues via API
```bash
curl -s "http://localhost:8080/api/dead-letter?limit=10" | jq .
```

---

## Embedded Staff Web Dashboard & Real-Time SSE

The engine features a modern React 19 + Tailwind CSS + shadcn/ui single-page dashboard compiled directly into the Go executable via `//go:embed static/*` in `web/embed.go`.

### Key Dashboard Capabilities
1. **Independent Catalog Updates & Pre-Sync / Pre-Revert Diff Preview**:
   - **5 Independent Supplier Update Scenarios**: *Baseline Catalog Feed*, *Price & Inventory Adjustment*, *Supplier Anomaly Feed (DLQ)*, *High-Volume Restock Stream*, and *Mid-Feed Crash & Checkpoint Recovery*.
   - **Pre-Sync Dry-Run Diff Preview**: Clicking **Sync** on any update first opens a row-by-row **Preview Catalog Update Diff Modal** (`POST /api/jobs/preview`) showing exact `+ ADDED`, `~ UPDATED` (`Before -> After` unit prices), `- WITHDRAWN`, and `QUARANTINED` records before committing.
   - **Independent Single-Update Revert with Diff Preview & Live Progress**: Clicking **Revert** on any synced update first opens a **Preview Revert Modal** showing the exact rollback diff, then executes a live batch revert (`Reverting...` progress bar) that restores pre-update state while preserving `WITHDRAWN` tombstones and leaving other independent updates intact.
   - **Resume Interrupted Jobs**: 1-click resumption of any interrupted sync run directly from its last committed SQLite WAL batch checkpoint (`Batch 6/10 -> 10/10`).
2. **Real-Time Visual Metric Cards & Drilldown Modals (Server-Sent Events)**:
   - Dynamic progress bar with live percentage, batch index, items/sec throughput, and ETA.
   - Interactive KPI cards (**Products Added**, **Price / Name Updated**, **Withdrawn Products**, **Duplicates Skipped**, and **Quarantined (DLQ)**) — clicking any card opens a SKU-level audit drilldown modal.
   - Analytics time-range filters (`Today`, `Yesterday`, `Last 7 Days`, `All Time`, `Custom`) and interactive chart zoom controls.
3. **Quarantine (Dead-Letter Queue) Inspector & Staff Remediation**:
   - Searchable, paginated audit table displaying quarantined supplier payloads needing attention.
   - Filter by defect reason (`NEGATIVE_OR_ZERO_PRICE`, `EMPTY_SKU`, `INVALID_STATUS`).
   - **Review & Fix Remediation Modal**: Inspect verbatim raw defective supplier JSON, correct product attributes/price, and atomically import the remediated SKU into the live catalog (`POST /api/dead-letter/{id}/resolve`).
4. **Live Product Catalog Explorer & Per-Product Modification History**:
   - Instant full-text search across product SKUs and names with status filters (`All Statuses`, `ACTIVE only`, `WITHDRAWN only`).
   - **Product Modification History Modal**: Clicking **History** (or any product row) opens a chronological audit log (`GET /api/products/{sku}/history`) showing every creation, price/name change (`Before -> After`), and sync version that modified that SKU.
5. **Unified Sync History & Catalog Version Control**:
   - Tracks every `SYNC`, `PATCH`, and `ROLLBACK` version milestone with row-by-row `Before -> After` **Version Diff Inspection** (`GET /api/versions/{id}/diff`).
6. **Multi-Tenant Store Isolation**:
   - Gatekeeper validation and live sidebar **Switch Tenant** switcher (`X-Shop-Id` / `?shop=store-flagship`), isolating each shop in its own dedicated SQLite database.

---

## Developer Tooling & Verification Guide

### Automated Makefile Targets

| Target | Description | Underlying Command |
|---|---|---|
| `make help` | Show self-documenting help menu | `awk` target parser |
| `make build` | Build React frontend + Go `server` & `generator` binaries | `make build-frontend build-server build-generator` |
| `make run-server` | Build and run HTTP server on :8080 | `go run ./cmd/server -port 8080 -db ./catalog.db` |
| `make run-generator` | Run CLI mock data generator | `go run ./cmd/generator $(ARGS)` |
| `make ingest-demo` | Ingest all 4 narrative demo fixture batches into running server | `bash scripts/ingest-fixtures.sh` |
| `make test` | Execute unit & integration tests with race detector | `go test -race ./...` |
| `make test-race` | Execute 5-iteration race condition test | `go test -race -count=5 ./...` |
| `make test-playwright` | Execute full Playwright API & UI E2E test suite | `npx playwright test` |
| `make cf-test` | Execute Cloudflare Worker & D1 unit/integration test suite | `npm --prefix worker test` |
| `make coverage` | Verify statement coverage $\ge 90.0\%$ & generate HTML | `./scripts/check-coverage.sh coverage.out 90.0` |
| `make lint` | Run code formatting, go vet, and golangci-lint | `gofmt`, `go vet`, `golangci-lint run ./...` |
| `make docker-build` | Build static multi-stage Docker container ($\le 25\text{MB}$) | `docker build -t coderpush-catalog-engine .` |
| `make docker-up` | Build & start local Docker Compose environment | `docker compose up -d --build` |
| `make docker-down` | Stop local Docker Compose environment | `docker compose down` |
| `make clean` | Remove temporary binaries, DBs, and coverage reports | `rm -rf bin/ *.db* coverage.*` |

---

### Security Gates & Git Hooks

The project employs **Husky v9** and **Gitleaks** to protect code quality and prevent secret leakage:

1. **Pre-Commit Hook (`.husky/pre-commit`)**:
   - Executes `gitleaks protect --staged` to detect leaked credentials or API keys.
   - Verifies Go code formatting with `gofmt -l` across staged files.
   - Executes compiler static analysis (`go vet ./...`).
   - Runs `golangci-lint` (with graceful fallback if uninstalled locally).
2. **Pre-Push Hook (`.husky/pre-push`)**:
   - Executes full repository secret scan with `gitleaks detect`.
   - Runs 5-iteration race testing (`go test -race -count=5 ./...`).
   - Enforces the project-wide **$\ge 90.0\%$ statement coverage gate** via `scripts/check-coverage.sh`.

---

### CI/CD Pipeline Architecture (`.github/workflows/ci-cd.yml`)

The repository includes a GitHub Actions pipeline executed on every push and pull request to `main`:

```
┌─────────────────────────────────┐       ┌─────────────────────────────────┐
│     Job 1: Security & Lint      │       │    Job 2: Test & Coverage Gate  │
│  - Gitleaks full repo scan      │       │  - 5-iteration race tests       │
│  - gofmt strict formatting gate │       │  - Statement coverage >= 90.0%  │
│  - go vet static analysis       │       │  - Upload coverage HTML report  │
└────────────────┬────────────────┘       └────────────────┬────────────────┘
                 │                                         │
                 └───────────────────┬─────────────────────┘
                                     ▼
                  ┌─────────────────────────────────────┐
                  │    Job 3: Docker Build & Publish    │
                  │  - Multi-stage static build         │
                  │  - Assert image size <= 25MB        │
                  │  - Publish container image to GHCR  │
                  └──────────────────┬──────────────────┘
                                     ▼
                  ┌─────────────────────────────────────┐
                  │  Job 4: Cloudflare Edge Deployment  │
                  │  - D1 SQLite schema migrations      │
                  │  - Deploy Worker API & Pages SPA    │
                  └─────────────────────────────────────┘
```

---

## REST API & SSE Event Reference

Interactive Swagger 2.0 / OpenAPI documentation is served directly by both the Go server and Cloudflare Worker:
- **Cloud Swagger UI**: [https://catalog-sync-worker.ezcert.workers.dev/swagger/index.html](https://catalog-sync-worker.ezcert.workers.dev/swagger/index.html) (or `/docs`)
- **Local / Docker Swagger UI**: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html) (or `/docs`)

### Core Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/jobs` | Start a new sync job (supports `generator` or `batches` mode) |
| `POST` | `/api/jobs/preview` | Dry-run preview of a sync job returning row-by-row `Before -> After` diffs without mutating DB |
| `POST` | `/api/jobs/{id}/resume` | Resume an interrupted sync job from its last committed batch checkpoint |
| `POST` | `/api/jobs/{id}/cancel` | Gracefully halt an active sync job at the current batch boundary |
| `GET` | `/api/jobs` | List recent sync jobs with status and change summaries |
| `GET` | `/api/jobs/{id}` | Retrieve details and checkpoint progress for a specific job |
| `GET` | `/api/products` | Query local catalog products (with search and status filters) |
| `GET` | `/api/products/{sku}/history` | Retrieve chronological modification history (`Before -> After` audit log) for a SKU |
| `GET` | `/api/dead-letter` | Query quarantined invalid records (filterable by `reason` and `job_id`) |
| `POST` | `/api/dead-letter/{id}/resolve` | Remediate a quarantined record and import the corrected product into the active catalog |
| `GET` | `/api/versions` | List immutable catalog version milestones (`SYNC`, `PATCH`, `ROLLBACK`) |
| `GET` | `/api/versions/{id}/diff` | Retrieve row-by-row `Before -> After` SKU diffs for a specific version |
| `POST` | `/api/versions/{id}/rollback` | Revert a specific catalog update (`revert_update_id`) or rollback to a version while preserving tombstones |
| `GET` | `/api/metrics` | Query aggregated catalog KPIs, status distribution, and time-series sync activity |
| `POST` | `/api/database/reset` | Reset the active shop tenant's catalog database to a clean state |
| `GET` | `/api/events` | Server-Sent Events (SSE) live progress and version event stream |
| `GET` | `/swagger/index.html` | Interactive Swagger UI API documentation (`/docs` redirects here) |

---

## Assessment 1: Mandatory Requirements

### Assumptions & Architectural Trade-offs

1. **Single-Writer Concurrency via SQLite WAL Mode**:
   - *Decision*: Configured SQLite in WAL mode (`journal_mode=WAL`) with immediate transactions (`_txlock=immediate`) and a single connection write lock (`db.SetMaxOpenConns(1)`).
   - *Rationale*: Catalog ingestion is inherently sequential per supplier store. By dedicating a single serialized writer and utilizing SQLite's WAL ring buffer, the system eliminates write deadlocks (`SQLITE_BUSY`) while providing non-blocking concurrent reads for the web UI and REST queries.
   - *Trade-off*: Write throughput is bound to single-node disk I/O (typically 3,000–5,000 records/sec). This is more than sufficient for retail store catalog synchronization, avoiding the operational overhead of a standalone database server.

2. **Embedded Static Web UI via `//go:embed`**:
   - *Decision*: Frontend assets (`web/static/*`) are compiled directly into the Go binary without requiring Node.js at runtime.
   - *Rationale*: Eliminates external runtime dependencies, keeps the compressed multi-stage Docker image under $25\text{MB}$, and guarantees the UI is always in lockstep with the API version.
   - *Trade-off*: Changes to UI templates require recompilation of the Go executable (though a `-static-dir` flag is provided for rapid local frontend iteration).

3. **Batch-Level Transaction Boundaries & Independent Update Tracking**:
   - *Decision*: Invariant E defines atomic transactions at the batch level ($T_{B_i}$), not the individual record level, and records per-SKU `Before -> After` deltas in `catalog_version_changes`.
   - *Rationale*: Committing each record individually causes massive disk sync overhead. Committing an entire job in a single transaction risks memory exhaustion and prevents resumability. Batch-level boundaries provide optimal ACID durability, instant checkpoint resumption, and allow independent single-update revert operations without losing unrelated updates or resurrecting `WITHDRAWN` tombstones.
   - *Trade-off*: An interrupted batch rolls back completely; upon resumption, the engine restarts at the uncommitted batch index.

4. **Deterministic PRNG Reservoir for Fictional Supplier Workloads**:
   - *Decision*: The mock generator uses deterministic PRNG seeds with disjoint SKU namespaces per update scenario (`seed=42, 101, 999, 777, 555`) and a bounded ring buffer to emit duplicate, out-of-order, withdrawn, and corrupt records.
   - *Rationale*: Enables reproducible dry-run diff previews (`POST /api/jobs/preview`), independent update syncing/reverting, and $O(1)$ memory streaming without requiring a live external supplier account.

---

### Unfinished Work & Production Roadmap

The current implementation satisfies 100% of the resilient catalog synchronization, quarantine remediation, diff inspection, and checkpoint recovery requirements. For multi-region enterprise retail deployment, the following enhancements are planned:

1. **Distributed Active-Passive High Availability (Leader Election)**:
   - Implement Raft consensus (e.g. `hashicorp/raft`) or Litestream/Turso continuous WAL replication to support multi-node failover. While the primary node performs catalog ingestion, replica nodes serve read-only queries and assume leadership if the primary fails.
2. **Object Storage Cold Archival (S3 / R2 Blob Storage)**:
   - Stream raw supplier JSON batch payloads to cold object storage (Cloudflare R2 or Amazon S3) with 90-day retention for compliance and historical replay.
3. **Control Plane Authentication & Role-Based Access Control (RBAC)**:
   - Integrate OAuth2/OIDC and JWT middleware protecting write endpoints (`POST /api/jobs`, `POST /api/dead-letter/{id}/resolve`, `POST /api/versions/{id}/rollback`), separating read-only staff viewers from catalog operators.
4. **Cloud-Native Observability & OpenTelemetry**:
   - Expose a Prometheus `/metrics` scrape endpoint (tracking batch commit latency histograms, skip ratios, and dead-letter counters) and OpenTelemetry trace spans across batch streams.
5. **Webhook & Downstream POS Notifications**:
   - Emit signed outbound webhooks when sync jobs complete or when dead-letter thresholds are exceeded so downstream point-of-sale (POS) terminals and staff Slack channels are notified immediately.

---

### Transparent AI Tool Disclosure

In compliance with Assessment 1 transparency guidelines, the design, development, and testing of this application were assisted by AI tooling:

1. **Architecture & Code Implementation**:
   - Domain interfaces, SQLite WAL transaction boundaries, SSE event streaming, and React dashboard components (`VersionDiffModal`, `ProductHistoryModal`, `UpdatesList`, `RemediationModal`) were developed and refined using AI-assisted pair programming (Google Antigravity / Gemini / Claude).
2. **Adversarial Testing & Verification Harness**:
   - AI tools assisted in generating comprehensive unit, integration, and Playwright E2E test suites, including concurrent race condition tests (`go test -race -count=5`), multi-tenant isolation tests, and edge-case inputs (empty/whitespace SKUs, negative prices, out-of-order timestamps, and tombstone reactivation attempts).
3. **Documentation & Demo Automation**:
   - Architectural diagrams, OpenAPI/Swagger annotations, and Playwright UI capture scripts for the walkthrough demo were structured with AI assistance.
4. **Human Verification & Quality Gates**:
   - All code was compiled with `CGO_ENABLED=0`, formatted and linted via `gofmt` and `go vet`, verified against the $\ge 90.0\%$ statement coverage gate (`scripts/check-coverage.sh`), and validated across both the native Go SQLite engine and the Cloudflare Worker + D1 edge runtime.
