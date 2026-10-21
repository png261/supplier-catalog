# Project: Resilient Catalog Synchronization Engine in Go

## Architecture
A resilient, minimalist catalog synchronization engine built in pure Go (`CGO_ENABLED=0`) utilizing embedded SQLite (`modernc.org/sqlite@v1.36.0`) in WAL mode for deterministic, transactional catalog state synchronization.

### System Components:
1. **Domain & Storage Layer (`pkg/domain`, `pkg/storage`)**:
   - Canonical `Product` data model with UTC timestamps and monotonic status (`ACTIVE`, `WITHDRAWN`).
   - SQLite relational storage with WAL mode, busy timeout, and ACID batch transaction guarantees.
   - Dedicated schemas for `products`, `sync_jobs`, `batch_checkpoints`, and `dead_letter_issues`.
2. **Synchronization Engine (`pkg/sync`)**:
   - Invariant A (Deterministic Idempotency): Zero duplicate products, zero catalog drift upon batch replay.
   - Invariant B (Fast-Forward Timestamps): $T_{new} < T_{old}$ updates rejected and counted as skipped.
   - Invariant C (Tombstone Preservation): Monotonic non-decreasing status; `WITHDRAWN` cannot be reactivated to `ACTIVE`.
   - Invariant D (Dead-Letter Isolation): Negative prices, empty/whitespace SKUs, and malformed records quarantined to `dead_letter_issues` without failing valid records.
   - Invariant E (Atomic Batch Transactions & Resumability): Atomic commits per batch including product upserts, dead-letter records, and checkpoint progression.
   - Startup crash recovery hook converting stale `RUNNING` jobs to `INTERRUPTED` and resuming from $last\_committed\_batch + 1$.
3. **Mock Data Generator (`pkg/generator`, `cmd/generator`)**:
   - Infinite streaming generator with $O(1)$ memory footprint using `math/rand/v2` PCG PRNG.
   - Seedable deterministic generation with configurable batch volume and sizes.
   - Calibrated anomaly injection: duplicates, out-of-order timestamps ($T_{new} < T_{old}$), withdrawn items, and corrupt records (negative prices, missing SKUs).
4. **Staff Dashboard & Control Plane (`pkg/api`, `pkg/server`, `web`)**:
   - Zero-node embedded frontend via `//go:embed` serving minimalist single-page dashboard.
   - Server-Sent Events (SSE) `/api/events` streaming real-time job progress and change metrics (+Added, ~Updated, -Withdrawn, ↷Skipped, ⚠Dead-Lettered).
   - REST control APIs for triggering sync runs, resuming interrupted jobs, and inspecting paginated dead-letter records.
5. **Security, Container & CI/CD (`.husky`, `.github/workflows`, `Dockerfile`, `docker-compose.yml`)**:
   - Husky v9 + Gitleaks staged secret scanning on pre-commit and pre-push.
   - Strict `gofmt` and `go vet` lint gates.
   - Multi-stage Docker container ($\le 25\text{MB}$) compiling static binary (`CGO_ENABLED=0`, `-ldflags="-s -w"`) on Alpine/Distroless.
   - Docker Compose with persistent named volume mount for SQLite database.
   - GitHub Actions CI running Gitleaks, `go test -race -count=5 ./...`, test coverage enforcement $\ge 90\%$, Docker build size gate, and GHCR publishing.

---

## Feature Inventory
Every feature identified during the Survey phase mapped to its assigned milestone:

| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Embedded SQLite Storage | Pure Go SQLite engine (`modernc.org/sqlite`) with WAL mode and schema initialization | M1 | Survey (Spec Miner & Explorer 1) |
| 2 | Product Catalog Repository | CRUD operations, SKU primary key lookup, and status tracking for catalog items | M1 | Survey (Spec Miner & Explorer 1) |
| 3 | Batch Ingestion Pipeline | Ingestion of supplier update batches within atomic database transaction boundaries | M1 | Survey (Spec Miner & Explorer 1) |
| 4 | Invariant A: Deterministic Idempotency | Re-ingesting identical batch results in 0 additional inserts, 0 updates, 0 drift | M1 | Survey (Spec Miner & Explorer 1) |
| 5 | Invariant B: Fast-Forward Timestamps | Rejection and skip counting for incoming records with $T_{new} < T_{old}$ | M1 | Survey (Spec Miner & Explorer 1) |
| 6 | Invariant C: Tombstone Preservation | Monotonic status enforcement; `WITHDRAWN` items can never be restored to `ACTIVE` | M1 | Survey (Spec Miner & Explorer 1) |
| 7 | Invariant D: Dead-Letter Isolation | Quarantine of corrupt records (negative price, empty/whitespace SKU) without failing batch | M2 | Survey (Spec Miner & Explorer 1) |
| 8 | Invariant E: Atomic Batch Transactions | Guaranteed atomic commit/rollback per batch including checkpoints and issues | M2 | Survey (Spec Miner & Explorer 1) |
| 9 | Checkpoint Persistence | Transactional recording of `committed_batch_index` and accumulated change metrics | M2 | Survey (Spec Miner & Explorer 1) |
| 10 | Startup Crash Recovery | Startup routine identifying incomplete `RUNNING` jobs and marking them `INTERRUPTED` | M2 | Survey (Spec Miner & Explorer 1) |
| 11 | Resumable Synchronization | Resuming an interrupted job from $last\_committed\_batch + 1$ to final completion | M2 | Survey (Spec Miner & Explorer 1) |
| 12 | Deterministic Data Generator | Standalone Go CLI & package generating supplier catalog batches with seedable PRNG | M3 | Survey (Spec Miner & Explorer 2) |
| 13 | Edge-Case Ratio Injection | Calibrated injection of duplicates, out-of-order timestamps, tombstones, and corrupt records | M3 | Survey (Spec Miner & Explorer 2) |
| 14 | Infinite Stream Architecture | Memory-bounded ($O(1)$) streaming generator supporting unlimited batches | M3 | Survey (Spec Miner & Explorer 2) |
| 15 | Embedded Staff Web UI | Single-page UI compiled directly into Go binary via `//go:embed` | M4 | Survey (Spec Miner & Explorer 2) |
| 16 | Real-Time Sync Status & SSE API | REST + Server-Sent Events streaming job progress and change counters | M4 | Survey (Spec Miner & Explorer 2) |
| 17 | Job Control & Resume API | Endpoints to start new sync jobs or resume interrupted jobs | M4 | Survey (Spec Miner & Explorer 2) |
| 18 | Dead-Letter Queue Viewer API | Paginated query endpoint for inspecting quarantined records and failure reasons | M4 | Survey (Spec Miner & Explorer 2) |
| 19 | Secret Leak Prevention (Gitleaks) | Pre-commit and pre-push hooks preventing credential leakage | M5 | Survey (Spec Miner & Explorer 2) |
| 20 | Git Hooks & Code Formatting | Husky v9 configuration enforcing `gofmt` and `go vet` | M5 | Survey (Spec Miner & Explorer 2) |
| 21 | Multi-Stage Docker Container | Lightweight static container image ($\le 25\text{MB}$) | M5 | Survey (Spec Miner & Explorer 2) |
| 22 | Docker Compose Volume Persistence | Docker Compose file mounting persistent volume for SQLite database | M5 | Survey (Spec Miner & Explorer 2) |
| 23 | GitHub Actions CI/CD Workflow | Workflow running race detection, coverage gate $\ge 90\%$, and publishing to GHCR | M5 | Survey (Spec Miner & Explorer 2) |
| 24 | Final E2E Integration & Acceptance | Passing 100% of E2E test suite (Tiers 1-4) and Tier 5 adversarial hardening | M6 | Survey & Project Pattern |

---

## Milestones

| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Core Domain, Storage & Synchronization Engine | Schema initialization, Product repository, Batch transaction handling, Invariants A (Idempotency), B (Fast-forward timestamps), C (Tombstone preservation) | none | DONE |
| M2 | Resumability, Batch Checkpointing & Dead-Letter Isolation | Invariant D (Dead-letter quarantine), Invariant E (Atomic batch commits), Checkpoint persistence, Startup crash recovery, Resumable execution from uncommitted batch | M1 | PLANNED |
| M3 | Programmatic Mock Data Generator | Standalone generator CLI & package (`cmd/generator`, `pkg/generator`), seedable PCG PRNG, bounded ring buffer for edge-case injection (duplicates, out-of-order, withdrawn, corrupt) | M1 | PLANNED |
| M4 | Embedded Staff Web UI & Control Plane | Embedded single-page dashboard (`//go:embed`), REST & SSE APIs (`pkg/api`), real-time metric cards, dead-letter inspector, trigger/resume controls | M1, M2 | PLANNED |
| M5 | Security, Docker Packaging & CI/CD | Husky v9 + Gitleaks git hooks, multi-stage Dockerfile ($\le 25\text{MB}$), `docker-compose.yml` with persistent volume, GitHub Actions CI/CD workflow | M1, M2, M3, M4 | PLANNED |
| M6 | Final Integration & E2E Acceptance Testing | Execution of E2E Test Suite (Tiers 1-4) published by E2E Testing Track, followed by Phase 2 Tier 5 Adversarial Coverage Hardening | M1, M2, M3, M4, M5, TEST_READY.md | PLANNED |

---

## Interface Contracts

### 1. `pkg/domain` ↔ `pkg/storage`
```go
package domain

import (
	"context"
	"time"
)

type ProductStatus string

const (
	StatusActive    ProductStatus = "ACTIVE"
	StatusWithdrawn ProductStatus = "WITHDRAWN"
)

type Product struct {
	SKU               string        `json:"sku"`
	Name              string        `json:"name"`
	Price             float64       `json:"price"`
	Status            ProductStatus `json:"status"`
	SupplierTimestamp time.Time     `json:"supplier_timestamp"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

type SupplierUpdate struct {
	SKU       string        `json:"sku"`
	Name      string        `json:"name"`
	Price     float64       `json:"price"`
	Status    ProductStatus `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
}

type Batch struct {
	JobID      string           `json:"job_id"`
	BatchIndex int              `json:"batch_index"`
	Records    []SupplierUpdate `json:"records"`
}

type ChangeSummary struct {
	Added       int `json:"added"`
	Updated     int `json:"updated"`
	Withdrawn   int `json:"withdrawn"`
	Skipped     int `json:"skipped"`
	DeadLetter  int `json:"dead_letter"`
}
```

### 2. `pkg/storage` Interface
```go
package storage

import (
	"context"
	"database/sql"
	"time"
	"coderpush/pkg/domain"
)

type Storage interface {
	DB() *sql.DB
	Close() error
	
	// Products
	GetProduct(ctx context.Context, sku string) (*domain.Product, error)
	UpsertProductTx(ctx context.Context, tx *sql.Tx, p *domain.Product) error
	
	// Jobs & Checkpoints
	CreateJob(ctx context.Context, job *domain.SyncJob) error
	GetJob(ctx context.Context, jobID string) (*domain.SyncJob, error)
	GetActiveJob(ctx context.Context) (*domain.SyncJob, error)
	UpdateJobStatus(ctx context.Context, jobID string, status domain.JobStatus) error
	RecordBatchCheckpointTx(ctx context.Context, tx *sql.Tx, cp *domain.BatchCheckpoint) error
	UpdateJobProgressTx(ctx context.Context, tx *sql.Tx, jobID string, batchIndex int, delta domain.ChangeSummary) error
	RecoverInterruptedJobs(ctx context.Context) ([]string, error)
	
	// Dead Letter
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
	ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
}
```

### 3. `pkg/sync` Engine Interface
```go
package sync

import (
	"context"
	"coderpush/pkg/domain"
)

type Engine interface {
	// ExecuteBatch processes a single batch inside an atomic transaction, enforcing Invariants A, B, C, D, E.
	ExecuteBatch(ctx context.Context, batch domain.Batch) (domain.ChangeSummary, error)
	
	// RunJob processes all batches from a batch reader/generator, handling checkpoints and crash resumability.
	RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
	
	// ResumeJob restarts an interrupted job from its last committed batch checkpoint.
	ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
	
	// RecoverOnStartup checks for any running jobs and transitions them to INTERRUPTED.
	RecoverOnStartup(ctx context.Context) ([]string, error)
}
```

### 4. `pkg/generator` Interface
```go
package generator

import (
	"context"
	"coderpush/pkg/domain"
)

type Config struct {
	Seed              uint64  `json:"seed"`
	TotalBatches      int     `json:"total_batches"`
	BatchSize         int     `json:"batch_size"`
	DuplicateRatio    float64 `json:"duplicate_ratio"`
	OutOfOrderRatio   float64 `json:"out_of_order_ratio"`
	WithdrawnRatio    float64 `json:"withdrawn_ratio"`
	InvalidRatio      float64 `json:"invalid_ratio"`
}

type Generator interface {
	StreamBatches(ctx context.Context, jobID string) <-chan domain.Batch
	GenerateBatch(jobID string, batchIndex int) domain.Batch
}
```

---

## Code Layout
```
/Users/png/01. Project/coderpush/
├── .github/
│   └── workflows/
│       └── ci.yml               # GitHub Actions CI/CD workflow
├── .husky/
│   ├── pre-commit               # Gitleaks staged scan + gofmt + go vet
│   └── pre-push                 # Gitleaks repo scan + go test
├── cmd/
│   ├── server/
│   │   └── main.go              # Main application entry point (API + Embedded UI)
│   └── generator/
│       └── main.go              # Standalone CLI mock data generator
├── pkg/
│   ├── domain/                  # Core entity structs, statuses, invariant types
│   ├── storage/                 # SQLite storage implementation (modernc.org/sqlite)
│   ├── sync/                    # Batch synchronization engine & invariant logic
│   ├── generator/               # Infinite-scale mock data generator & ring buffer
│   └── api/                     # HTTP REST endpoints & SSE streaming handlers
├── web/
│   ├── embed.go                 # Go embed.FS declaration
│   └── static/                  # Minimalist single-page dashboard (index.html, app.js, style.css)
├── test/
│   ├── e2e/                     # Opaque-box E2E test suite (Tiers 1-4)
│   │   ├── runner_test.go       # E2E test harness & suite runner
│   │   ├── tier1_feature_test.go
│   │   ├── tier2_boundary_test.go
│   │   ├── tier3_combination_test.go
│   │   └── tier4_workload_test.go
│   └── testutil/                # Test fixtures and database helpers
├── Dockerfile                   # Multi-stage lightweight Dockerfile (<= 25MB)
├── docker-compose.yml           # Compose file with persistent volume mount
├── .gitleaks.toml               # Gitleaks configuration
├── package.json                 # Husky dependency for pre-commit hooks
├── go.mod                       # Go module definition
├── go.sum                       # Go checksums
├── PROJECT.md                   # Global project specification & milestone tracking
└── TEST_READY.md                # E2E test suite publication signal
```
