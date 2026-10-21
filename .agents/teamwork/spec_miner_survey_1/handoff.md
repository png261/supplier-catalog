# Specification Mining & Domain Invariant Report: Resilient Catalog Synchronization Engine

## 1. Observation

### 1.1 Source Documents & Environment
- **Authoritative Source**: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md` (Lines 1–59)
- **Dispatch Assignment**: `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_survey_1/DISPATCH.md` (Lines 1–20)
- **Host Runtime Inspection**:
  - Operating System: macOS Darwin (arm64)
  - Go Toolchain: `go version go1.25.6 darwin/arm64`
  - Secret Scanning: `/opt/homebrew/bin/gitleaks`
  - Container Runtime: `Docker version 29.2.0, build 0b9d198`
  - Repository Status: Clean workspace (initial `.agents` structure present, git repo not yet initialized).

### 1.2 Verbatim Requirements Quoted from ORIGINAL_REQUEST.md
```markdown
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
```

### 1.3 Verbatim Acceptance Criteria Quoted from ORIGINAL_REQUEST.md
```markdown
### Invariant & Functional Correctness
- [ ] Ingesting the same batch twice results in 0 additional inserts, 0 updates, and identical catalog state.
- [ ] An incoming record with timestamp T_{new} < T_{old} is rejected and counted as skipped.
- [ ] A product with status WITHDRAWN is never flipped back to ACTIVE.
- [ ] Records with negative prices or empty SKUs are logged to the issues table without halting the batch.

### Crash & Resumability
- [ ] Simulating an interruption midway through a multi-batch sync leaves prior committed batches intact and uncommitted batch changes rolled back.
- [ ] Resuming an interrupted sync continues directly from the uncommitted batch to completion without re-processing completed batches.
- [ ] Interrupted jobs are marked INTERRUPTED upon restart and only transition to COMPLETED when all batches finish.

### Test Coverage & Security
- [ ] Automated test suite achieves >= 90% statement/branch coverage across the project.
- [ ] Multi-iteration test run (go test -race -count=5 ./...) passes with 0 data races.
- [ ] Gitleaks scans pass with 0 detected credentials on staged files.

### Packaging & Execution
- [ ] Application builds into a standalone Go executable and a multi-stage Docker image (<= 25MB).
- [ ] docker compose up starts the application with persistent volume mounting for the database.
- [ ] Native GitHub Actions workflow compiles the application, runs tests, and publishes the Docker image to GHCR.
```

---

## 2. Logic Chain

### 2.1 Derivation of Core Invariants (A through E)
1. **Invariant A (Deterministic Idempotency)**:
   - *Premise*: R1 states "importing the exact same update multiple times must never duplicate products or drift catalog state." Acceptance criterion: "Ingesting the same batch twice results in 0 additional inserts, 0 updates, and identical catalog state."
   - *Deduction*: Let $f(S, B)$ be the state transition function for catalog state $S$ applied to batch $B$. Then $f(f(S, B), B) = f(S, B)$. Re-processing must compute change deltas $(\Delta A = 0, \Delta U = 0, \Delta W = 0, \Delta S = |B_{valid}|, \Delta D = |B_{invalid}|)$. The primary key on SKU prevents duplicates, and comparison against current hash/timestamp/content yields zero modifications.

2. **Invariant B (Fast-Forward Out-of-Order Resolution)**:
   - *Premise*: R1 states "incoming older information (based on supplier timestamp) must not overwrite newer local information." Acceptance criterion: "An incoming record with timestamp $T_{new} < T_{old}$ is rejected and counted as skipped."
   - *Deduction*: Every incoming record $R$ contains supplier timestamp $T_{incoming}$. Existing record $P$ stores $T_{local} = P.supplier\_updated\_at$.
     - If $T_{incoming} < T_{local}$: reject payload, keep existing $P$, increment `skipped` counter.
     - If $T_{incoming} == T_{local}$: if payload identical, idempotent no-op (increment `skipped`); if payload differs, deterministic tie-breaking rejects or skips to avoid non-deterministic drift.
     - If $T_{incoming} > T_{local}$: accept update, modify attributes, set $P.supplier\_updated\_at = T_{incoming}$, subject to Invariant C.

3. **Invariant C (Tombstone Preservation)**:
   - *Premise*: R1 states "a product marked as withdrawn from sale must never be restored to active sale by older or subsequent update payloads." Acceptance criterion: "A product with status WITHDRAWN is never flipped back to ACTIVE."
   - *Deduction*: State transition for status is monotonically non-decreasing over partial order: $\text{UNSEEN} \prec \text{ACTIVE} \prec \text{WITHDRAWN}$. Once $P.status = \text{WITHDRAWN}$, it is an absorbing state. Any incoming record $R$ for that SKU with $R.status = \text{ACTIVE}$ is forbidden from resurrecting $P$, regardless of timestamp ($T_{incoming} > T_{local}$ or $T_{incoming} < T_{local}$). The status remains `WITHDRAWN` and the resurrection attempt is recorded as `skipped`.

4. **Invariant D (Dead-Letter Isolation)**:
   - *Premise*: R2 states "Isolate corrupt or invalid product records (e.g. negative price, missing SKU) into a dead-letter queue for staff attention without failing valid records in the batch." Acceptance criterion: "Records with negative prices or empty SKUs are logged to the issues table without halting the batch."
   - *Deduction*: Record-level validation must precede mutation. A validation predicate $V(R)$ checks: $R.sku \ne "" \land \text{trim}(R.sku) \ne "" \land R.price \ge 0.0 \land R.timestamp \text{ is valid RFC3339} \land R.status \in \{\text{ACTIVE}, \text{WITHDRAWN}\}$. If $V(R)$ fails, $R$ is written to `dead_letter_records` with error details, $R$ is not applied to `products`, and the batch continues processing all remaining valid records. The containing database transaction commits both valid product mutations and dead-letter insertions together.

5. **Invariant E (Atomic Batch Transactions & Crash Resumability)**:
   - *Premise*: R2 states "transactions must be atomic per batch... persist execution checkpoints so staff can resume... interrupted or partially completed update must never be reported as complete." Acceptance criteria: "Interruption midway through leaves prior committed batches intact and uncommitted rolled back... Resuming continues directly from uncommitted batch... Interrupted jobs marked INTERRUPTED upon restart."
   - *Deduction*:
     - Database must support ACID transactions (embedded SQLite in WAL mode).
     - Each batch $i \in [1, N]$ executes in a single transaction $\tau_i$.
     - $\tau_i$ contains: product upserts for batch $i$, dead-letter insertions for batch $i$, and checkpoint record update setting $last\_committed\_batch = i$ and updated cumulative metrics.
     - If power/process terminates during $\tau_k$, SQLite WAL rollbacks uncommitted changes upon reconnection. Batches $1 \dots k-1$ remain durably on disk.
     - Engine startup routine executes recovery scan: any job with status `IN_PROGRESS` is transitioned to `INTERRUPTED`.
     - Resumption queries $last\_committed\_batch$ and streams batches starting from $k = last\_committed\_batch + 1$. Completed batches $1 \dots k-1$ are bypassed.

### 2.2 Derivation of Domain Entities and Relational Schemas
From the requirements and dispatch instructions, 7 core domain models are derived:
1. `Product`: Canonical stored catalog item.
2. `SupplierUpdate`: Raw incoming product record from supplier batch.
3. `Batch`: Discrete collection of supplier updates processed in one atomic transaction.
4. `Job`: Orchestration execution instance tracking a full synchronization run.
5. `Checkpoint`: Persisted progress marker recording the last successfully committed batch.
6. `DeadLetterRecord`: Quarantined invalid record with failure diagnostics.
7. `ChangeSummary`: Structured delta metrics (+Added, ~Updated, -Withdrawn, ↷Skipped, Dead-Lettered).

### 2.3 Derivation of Generator Architecture (R3)
- Standalone CLI generator (`cmd/generator` or subcommand) capable of streaming batches to `stdout` or file in NDJSON format.
- Parameterized by `--seed` (reproducibility) and `--batches`, `--batch-size`.
- Explicit mutation injection flags: `--dup-ratio`, `--out-of-order-ratio`, `--withdrawn-ratio`, `--invalid-ratio`, `--resurrect-ratio`.
- Memory bounded via streaming iterator to allow infinite scale ($10^6+$ records without OOM).

### 2.4 Derivation of Embedded Staff Web UI (R4)
- Embedded assets via Go `//go:embed` directive (`web/dist` or `web/static`).
- Zero external runtime npm dependency in production container.
- Clean dashboard interface displaying:
  - Synchronizer control: Trigger sync (from mock or stream), Pause/Resume.
  - Live progress: Active job status, progress bar, batch $i$ of $N$, throughput.
  - Cumulative counters: `+Added`, `~Updated`, `-Withdrawn`, `↷Skipped`, `⚠Dead-Lettered`.
  - Issue viewer: Paginated table of dead-letter records with SKU, reason, batch index, timestamp, and raw payload inspect drawer.

### 2.5 Derivation of Security, CI/CD, and Container Packaging (R5)
- Docker: Multi-stage Dockerfile (`golang:1.25-alpine` builder $\to$ `alpine:3.20` or `distroless` runner). Total compressed image size $\le 25\text{MB}$.
- Persistence: Docker Compose file (`docker-compose.yml`) mounting a named volume to SQLite storage path (e.g. `/data/catalog.db`).
- Secret scanning: Pre-commit/pre-push hooks via Husky running Gitleaks.
- CI/CD: `.github/workflows/ci.yml` running lint, gitleaks, `go test -race -count=5 ./...`, test coverage enforcement $\ge 90\%$, Docker build, and GHCR publishing.

---

## 3. Caveats

1. **Price Precision in Storage**:
   - Floating point (`float64`) in Go and `REAL` in SQLite can introduce IEEE-754 rounding artifacts (e.g. `$19.99` represented as `19.990000000000002`).
   - *Recommendation*: Store price as integer cents (`price_cents INTEGER`, e.g. `$19.99` $\to$ `1999`) or fixed-precision string/decimal in database and internal structs, while exposing human-readable decimal in JSON/UI.
2. **Intra-Batch Duplicate SKUs**:
   - A single batch may contain multiple records for the exact same SKU (e.g. Record 3 is SKU-001 at $T_1$, Record 8 is SKU-001 at $T_2$).
   - *Policy*: The engine must process intra-batch records sequentially or pre-sort/reduce them using the identical Invariants B and C rules, ensuring the final committed state inside the batch transaction matches chronological order.
3. **Withdrawn Item Attribute Updates**:
   - If an incoming record has $T_{incoming} > T_{local}$, status = `ACTIVE`, but local product is `WITHDRAWN`, Invariant C mandates status cannot flip to `ACTIVE`.
   - *Policy*: To guarantee complete tombstone safety and prevent partial zombie updates, resurrection attempts must be rejected in their entirety (counted as `↷Skipped`). If a supplier sends an update with status = `WITHDRAWN` and $T_{incoming} > T_{local}$, metadata (e.g. price/name) may update while status remains `WITHDRAWN`.
4. **SQLite Multi-Process Write Contention**:
   - SQLite supports multi-reader single-writer concurrency under WAL mode.
   - *Policy*: The Go synchronization service must serialize write transactions via an application-level mutex or single-worker channel to prevent `SQLITE_BUSY` or locked database errors.
5. **Job Resumption Source Consistency**:
   - Resuming an interrupted job assumes that the batch sequence provided to the engine on resume is identical to the original sequence (deterministic generator with identical seed, or immutable batch files).

---

## 4. Conclusion

### 4.1 Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | R1 Sync | Embedded SQLite Engine | Relational persistence with WAL mode for local catalog copy | File path (`data/catalog.db`), connection config | DB handle, initialized schema tables | Fails gracefully if disk full or path unwritable | ORIGINAL_REQUEST.md:13 |
| 2 | R1 Sync | Batch Ingestion | Ingest discrete batches of supplier updates atomically | Batch struct: `job_id`, `batch_index`, `[]SupplierUpdate` | Batch processing summary (A, U, W, S, D counts) | Rolls back transaction on fatal DB error; logs invalid records | ORIGINAL_REQUEST.md:13,19 |
| 3 | R1 Sync | Invariant A: Deterministic Idempotency | Re-ingesting an identical batch yields 0 inserts, 0 updates, 0 catalog drift | Repeated batch payload with identical SKUs & timestamps | 0 added, 0 updated, 0 withdrawn, $N$ skipped | Idempotent no-op; identical catalog checksum | ORIGINAL_REQUEST.md:14,40 |
| 4 | R1 Sync | Invariant B: Fast-Forward Timestamps | Drops updates older than existing catalog record timestamp ($T_{new} < T_{old}$) | Supplier record with $T_{new} < T_{local}$ | Record skipped, counter `skipped` incremented | Rejection logged; catalog record untouched | ORIGINAL_REQUEST.md:15,41 |
| 5 | R1 Sync | Invariant C: Tombstone Preservation | Prevents withdrawn products from ever being restored to active sale | Incoming record `status: ACTIVE` for SKU with local `WITHDRAWN` | Record skipped, status remains `WITHDRAWN`, `skipped` incremented | Resurrection rejected; tombstone preserved | ORIGINAL_REQUEST.md:16,42 |
| 6 | R2 Resilience | Invariant D: Dead-Letter Isolation | Diverts malformed records to dead-letter store without aborting batch | Record with `price < 0` or empty/whitespace SKU | Stored in `dead_letter_records` table, `dead_lettered` incremented | Batch transaction continues; valid records commit | ORIGINAL_REQUEST.md:22,43 |
| 7 | R2 Resilience | Invariant E: Atomic Batch Transactions | Atomic per-batch ACID transaction commit/rollback | Batch of product updates + dead-letter entries | Atomically committed DB state or 100% rolled back | If process aborted during batch, WAL cleans uncommitted state | ORIGINAL_REQUEST.md:19,46 |
| 8 | R2 Resilience | Checkpoint Tracking | Persists completed batch index and accumulated change metrics per job | `job_id`, `batch_index`, `ChangeSummary` | Checkpoint row updated in DB within batch transaction | If write fails, transaction rolls back | ORIGINAL_REQUEST.md:20,47 |
| 9 | R2 Resilience | Startup Crash Recovery | Scans on boot; converts incomplete `IN_PROGRESS` jobs to `INTERRUPTED` | Database startup hook | Updated job status (`IN_PROGRESS` $\to$ `INTERRUPTED`) | Logs detected crash and last committed batch | ORIGINAL_REQUEST.md:21,48 |
| 10 | R2 Resilience | Resumable Synchronization | Continues interrupted job from $last\_committed\_batch + 1$ | `job_id` | Resumed execution, transition to `COMPLETED` on finish | Cannot resume non-existent or already completed job | ORIGINAL_REQUEST.md:20,47 |
| 11 | R3 Generator | Deterministic Data Generator | CLI tool generating synthetic supplier catalogs with PRNG seed | `--seed`, `--batches`, `--batch-size` | NDJSON stream to stdout or file | Exits with code 1 on invalid arguments | ORIGINAL_REQUEST.md:25 |
| 12 | R3 Generator | Edge-Case Ratio Injection | Injects controlled percentage of anomalies into mock stream | `--dup-ratio`, `--out-of-order-ratio`, `--withdrawn-ratio`, `--invalid-ratio` | Stream with calibrated anomaly distribution | Validates ratios sum $\le 1.0$; flags out-of-range | ORIGINAL_REQUEST.md:26 |
| 13 | R3 Generator | Infinite Stream Generator | Generates unbounded batches in constant memory footprint | `--batches 0` (unlimited) or large $N$ | Streaming writer without memory accumulation | Clean SIGINT / SIGTERM graceful shutdown | ORIGINAL_REQUEST.md:25 |
| 14 | R4 UI | Embedded Staff Web UI | Single-page UI compiled directly into Go binary via `//go:embed` | HTTP GET `/` | HTML/JS/CSS single-page dashboard | Returns 404 / 500 cleanly if assets missing | ORIGINAL_REQUEST.md:29 |
| 15 | R4 UI | Real-Time Sync Status API | REST endpoint exposing job state, progress, and change counters | HTTP GET `/api/jobs/{id}` or `/api/jobs/active` | JSON payload: status, progress %, change summary | Returns 404 if no active job | ORIGINAL_REQUEST.md:30 |
| 16 | R4 UI | Job Dispatch & Resume API | REST endpoints to trigger new sync or resume interrupted job | HTTP POST `/api/jobs` or `/api/jobs/{id}/resume` | JSON response with job ID and initial status | Returns 409 Conflict if job already running | ORIGINAL_REQUEST.md:30 |
| 17 | R4 UI | Dead-Letter Queue Viewer API | Paginated query endpoint for inspecting quarantined corrupt records | HTTP GET `/api/dead-letter?job_id=...&limit=50&offset=0` | JSON array of `DeadLetterRecord` with error reasons | Returns empty list if no dead-letter records | ORIGINAL_REQUEST.md:30 |
| 18 | R5 DevOps | Multi-Stage Docker Build | Minimal Alpine/Distroless container under 25MB | Dockerfile, source code | Container image $\le 25\text{MB}$ | Build failure if image exceeds 25MB | ORIGINAL_REQUEST.md:34,56 |
| 19 | R5 DevOps | Docker Compose Volume Persistence | Persistent local directory mount for SQLite database | `docker-compose.yml`, `docker compose up` | Data survives container stop and recreate | Error on mount permission failure | ORIGINAL_REQUEST.md:34,57 |
| 20 | R5 DevOps | Secret Leak Prevention | Pre-commit/pre-push hooks via Husky running Gitleaks | Git commit / push triggers | Pass (code 0) or block (code 1) | Blocks commit with filename and line of detected secret | ORIGINAL_REQUEST.md:33,53 |
| 21 | R5 DevOps | High-Rigor Automated CI | GitHub Actions CI running tests, race detection, coverage $\ge 90\%$, and push to GHCR | Git push to `main` or pull request | CI execution logs, test coverage report, GHCR image | Workflow fails if coverage $< 90\%$ or race detected | ORIGINAL_REQUEST.md:35,51-58 |

---

### 4.2 Edge Cases Matrix

| # | Feature | Input / Scenario | Expected / Observed Behavior |
|---|---------|------------------|------------------------------|
| 1 | Invariant D | Empty SKU string (`sku: ""`) | Quarantined to dead-letter queue (`REASON: EMPTY_SKU`). Batch continues. |
| 2 | Invariant D | Whitespace-only SKU (`sku: "   \t\n"`) | Quarantined to dead-letter queue (`REASON: EMPTY_SKU`). Batch continues. |
| 3 | Invariant D | Negative price (`price: -0.01` or `-500.00`) | Quarantined to dead-letter queue (`REASON: NEGATIVE_PRICE`). Batch continues. |
| 4 | Product Schema | Zero price (`price: 0.00`) | Valid record (free item / promotional gift). Stored successfully. |
| 5 | Product Schema | Large price (`price: 99999999.99`) | Stored successfully without integer/float overflow. |
| 6 | Invariant B | Out-of-order timestamp ($T_{incoming} < T_{local}$) | Incoming record dropped, existing record preserved, `skipped` incremented. |
| 7 | Invariant B | Exact identical timestamp ($T_{incoming} == T_{local}$, identical payload) | Idempotent no-op. 0 modifications. `skipped` incremented. |
| 8 | Invariant B | Identical timestamp, differing payload ($T_{incoming} == T_{local}$, new price) | Stale/tie-breaker skip. Local record preserved, `skipped` incremented. |
| 9 | Invariant B | Sub-millisecond / Nanosecond timestamp precision | RFC3339Nano comparison preserves exact temporal ordering. |
| 10 | Invariant C | Attempt to resurrect withdrawn product ($P.status = \text{WITHDRAWN}$, $R.status = \text{ACTIVE}$, $T_{new} > T_{old}$) | Rejection. Status remains `WITHDRAWN`. No resurrection. `skipped` incremented. |
| 11 | Invariant C | Out-of-order withdrawal ($P.status = \text{ACTIVE}$ at $T_2$, incoming WITHDRAWN with $T_1 < T_2$) | Rejected by Invariant B ($T_{new} < T_{old}$). Status remains `ACTIVE`. |
| 12 | Invariant C | Valid withdrawal ($P.status = \text{ACTIVE}$ at $T_1$, incoming WITHDRAWN with $T_2 > T_1$) | Status transitions to `WITHDRAWN`. `withdrawn` incremented. |
| 13 | Invariant A | Whole batch replay (Batch $B_1$ executed, then $B_1$ re-sent) | 0 Added, 0 Updated, 0 Withdrawn, $N$ Skipped. Catalog state hash identical. |
| 14 | Invariant D | Batch containing 100% invalid records | All records quarantined to dead-letter queue. 0 products modified. Batch transaction commits checkpoint. |
| 15 | Invariant D | Batch with alternating valid and corrupt records | Valid records inserted/updated. Invalid records quarantined. Batch succeeds. |
| 16 | Storage / Sync | Duplicate SKU within same batch ($R_1$: $T=10:00$, $R_2$: $T=09:00$) | Sequential intra-batch resolution: $R_1$ applied, $R_2$ skipped as stale. |
| 17 | Storage / Sync | Duplicate SKU within same batch ($R_1$: `ACTIVE`, $R_2$: `WITHDRAWN`, $T_2 > T_1$) | Sequential intra-batch resolution: Product ends in `WITHDRAWN`. |
| 18 | Storage / Sync | Duplicate SKU within same batch ($R_1$: `WITHDRAWN`, $R_2$: `ACTIVE`, $T_2 > T_1$) | Sequential intra-batch resolution: Invariant C prevents flip. Ends in `WITHDRAWN`. |
| 19 | Invariant E | Crash / SIGKILL midway through batch $K$ of $N$ | SQLite WAL rolls back batch $K$. Batches $1 \dots K-1$ intact. Checkpoint shows $K-1$. |
| 20 | Invariant E | Engine restart following crash | Startup scan detects Job in `IN_PROGRESS` with missing lock $\to$ marks `INTERRUPTED`. |
| 21 | Invariant E | Resume triggered on `INTERRUPTED` job | Engine reads checkpoint ($K-1$), skips batches $1 \dots K-1$, executes batches $K \dots N$. |
| 22 | Invariant E | Resume attempted on already `COMPLETED` job | Returns error or idempotent 200 OK with no-op message ("Job already completed"). |
| 23 | Invariant E | Empty batch ($N=0$ records) | Atomic transaction commits checkpoint index, 0 changes, advances cleanly. |
| 24 | R4 UI | Concurrent sync job request while job already running | Second request rejected with HTTP 409 Conflict ("Job already in progress"). |
| 25 | Invariant D | Malformed JSON in stream | Parser isolates raw line to dead-letter queue (`REASON: MALFORMED_JSON`), batch continues. |
| 26 | Security | Accidental secret in staged code (e.g. API key) | Husky pre-commit hook runs Gitleaks and blocks git commit immediately. |
| 27 | Docker | Container stop and restart (`docker compose down && docker compose up`) | Database preserved intact on mounted volume; catalog query returns all prior products. |

---

### 4.3 Domain Model Specifications

#### 4.3.1 Relational Database DDL (SQLite)
```sql
-- Products Table (Canonical Catalog)
CREATE TABLE IF NOT EXISTS products (
    sku TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    price_cents INTEGER NOT NULL CHECK (price_cents >= 0),
    status TEXT NOT NULL CHECK (status IN ('ACTIVE', 'WITHDRAWN')),
    supplier_updated_at TEXT NOT NULL, -- ISO-8601 / RFC3339 timestamp
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_products_status ON products(status);
CREATE INDEX IF NOT EXISTS idx_products_supplier_updated ON products(supplier_updated_at);

-- Sync Jobs Table
CREATE TABLE IF NOT EXISTS sync_jobs (
    job_id TEXT PRIMARY KEY NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'IN_PROGRESS', 'INTERRUPTED', 'COMPLETED', 'FAILED')),
    total_batches INTEGER NOT NULL DEFAULT 0,
    last_committed_batch INTEGER NOT NULL DEFAULT 0,
    added_count INTEGER NOT NULL DEFAULT 0,
    updated_count INTEGER NOT NULL DEFAULT 0,
    withdrawn_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    dead_letter_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_sync_jobs_status ON sync_jobs(status);

-- Dead-Letter Records Table (Issues Queue)
CREATE TABLE IF NOT EXISTS dead_letter_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    batch_index INTEGER NOT NULL,
    record_index INTEGER NOT NULL,
    sku TEXT,
    raw_payload TEXT NOT NULL,
    error_reason TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY(job_id) REFERENCES sync_jobs(job_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_dead_letter_job ON dead_letter_records(job_id);
CREATE INDEX IF NOT EXISTS idx_dead_letter_reason ON dead_letter_records(error_reason);
```

#### 4.3.2 Go Domain Structs & Types
```go
package domain

import (
	"errors"
	"strings"
	"time"
)

type ProductStatus string

const (
	StatusActive    ProductStatus = "ACTIVE"
	StatusWithdrawn ProductStatus = "WITHDRAWN"
)

type JobStatus string

const (
	JobPending     JobStatus = "PENDING"
	JobInProgress  JobStatus = "IN_PROGRESS"
	JobInterrupted JobStatus = "INTERRUPTED"
	JobCompleted   JobStatus = "COMPLETED"
	JobFailed      JobStatus = "FAILED"
)

// Product represents a canonical catalog item stored in the local database.
type Product struct {
	SKU               string        `json:"sku"`
	Name              string        `json:"name"`
	PriceCents        int64         `json:"price_cents"`
	Status            ProductStatus `json:"status"`
	SupplierUpdatedAt time.Time     `json:"supplier_updated_at"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

// SupplierUpdate represents a raw record received from a supplier catalog payload.
type SupplierUpdate struct {
	SKU       string    `json:"sku"`
	Name      string    `json:"name"`
	Price     float64   `json:"price"` // decimal dollars/euros from supplier
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"` // supplier modification time
}

// Validate checks Invariant D requirements for a single record.
func (u *SupplierUpdate) Validate() error {
	if strings.TrimSpace(u.SKU) == "" {
		return errors.New("EMPTY_SKU: product sku is empty or whitespace")
	}
	if u.Price < 0.0 {
		return errors.New("NEGATIVE_PRICE: product price cannot be negative")
	}
	if u.Timestamp.IsZero() {
		return errors.New("INVALID_TIMESTAMP: supplier timestamp is zero or missing")
	}
	status := strings.ToUpper(strings.TrimSpace(u.Status))
	if status != string(StatusActive) && status != string(StatusWithdrawn) {
		return errors.New("INVALID_STATUS: status must be ACTIVE or WITHDRAWN")
	}
	return nil
}

// Batch represents a discrete unit of synchronization containing multiple records.
type Batch struct {
	JobID      string           `json:"job_id"`
	BatchIndex int              `json:"batch_index"`
	Records    []SupplierUpdate `json:"records"`
}

// ChangeSummary tracks synchronization delta counters.
type ChangeSummary struct {
	Added        int64 `json:"added"`         // +Added
	Updated      int64 `json:"updated"`       // ~Updated
	Withdrawn    int64 `json:"withdrawn"`     // -Withdrawn
	Skipped      int64 `json:"skipped"`       // ↷Skipped
	DeadLettered int64 `json:"dead_lettered"` // ⚠Dead-Lettered
}

func (s *ChangeSummary) Add(other ChangeSummary) {
	s.Added += other.Added
	s.Updated += other.Updated
	s.Withdrawn += other.Withdrawn
	s.Skipped += other.Skipped
	s.DeadLettered += other.DeadLettered
}

// DeadLetterRecord represents an isolated invalid record that failed validation.
type DeadLetterRecord struct {
	ID          int64     `json:"id"`
	JobID       string    `json:"job_id"`
	BatchIndex  int       `json:"batch_index"`
	RecordIndex int       `json:"record_index"`
	SKU         string    `json:"sku,omitempty"`
	RawPayload  string    `json:"raw_payload"`
	ErrorReason string    `json:"error_reason"`
	CreatedAt   time.Time `json:"created_at"`
}

// SyncJob tracks the execution, checkpoint, and outcome of a catalog synchronization run.
type SyncJob struct {
	JobID              string        `json:"job_id"`
	Status             JobStatus     `json:"status"`
	TotalBatches       int           `json:"total_batches"`
	LastCommittedBatch int           `json:"last_committed_batch"`
	Summary            ChangeSummary `json:"summary"`
	ErrorMessage       string        `json:"error_message,omitempty"`
	StartedAt          time.Time     `json:"started_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
	CompletedAt        *time.Time    `json:"completed_at,omitempty"`
}
```

---

### 4.4 State Machine Formalisms

#### 4.4.1 Product Status State Machine
```
   [ Incoming Record ]
           │
           ▼
    ┌─────────────┐
    │   UNSEEN    │ (Product not yet in catalog)
    └──────┬──────┘
           │
           ├──────────────────────────────┬──────────────────────────────┐
           │ incoming status = ACTIVE     │ incoming status = WITHDRAWN  │
           ▼                              ▼                              │
    ┌─────────────┐               ┌─────────────┐                        │
    │   ACTIVE    │               │  WITHDRAWN  │                        │
    └──────┬──────┘               │ (Tombstone) │                        │
           │                      └──────┬──────┘                        │
           │ incoming: WITHDRAWN         │                               │
           │ and T_new >= T_old          │                               │
           ▼                             │                               │
           └────────────────────────────►│                               │
                                         │ incoming: ACTIVE              │
                                         │ (any timestamp)               │
                                         ▼                               │
                                  [ REJECT FLIP ]                        │
                                 Invariant C Holds                       │
                                 Status stays WITHDRAWN ◄────────────────┘
```
**Transition Table**:
| Current Status | Incoming Status | Timestamp Condition | Resulting Status | Action / Metric | Invariant Rule |
|:--------------:|:---------------:|:-------------------:|:----------------:|:---------------:|:--------------:|
| *Unseen* | `ACTIVE` | Any valid $T$ | `ACTIVE` | Insert product, `+Added` | Normal initial insert |
| *Unseen* | `WITHDRAWN` | Any valid $T$ | `WITHDRAWN` | Insert product as tombstone, `-Withdrawn` | Pre-withdrawn item |
| `ACTIVE` | `ACTIVE` | $T_{new} > T_{old}$ | `ACTIVE` | Update product attributes, `~Updated` | Fast-forward update |
| `ACTIVE` | `ACTIVE` | $T_{new} == T_{old}$ (same payload) | `ACTIVE` | No-op, `↷Skipped` | Invariant A (Idempotency) |
| `ACTIVE` | `ACTIVE` | $T_{new} < T_{old}$ | `ACTIVE` | Reject update, `↷Skipped` | Invariant B (Out-of-order drop) |
| `ACTIVE` | `WITHDRAWN` | $T_{new} \ge T_{old}$ | `WITHDRAWN` | Transition to tombstone, `-Withdrawn` | Valid withdrawal |
| `ACTIVE` | `WITHDRAWN` | $T_{new} < T_{old}$ | `ACTIVE` | Reject update, `↷Skipped` | Invariant B (Stale withdrawal) |
| `WITHDRAWN` | `ACTIVE` | Any $T$ ($T_{new} \ge T_{old}$ or $<$) | `WITHDRAWN` | Reject resurrection, `↷Skipped` | **Invariant C (Tombstone immutable)** |
| `WITHDRAWN` | `WITHDRAWN` | Any $T$ | `WITHDRAWN` | No-op, `↷Skipped` | Idempotent tombstone |

#### 4.4.2 Sync Job State Machine
```
      ┌─────────────┐
      │   PENDING   │
      └──────┬──────┘
             │ Start execution
             ▼
      ┌─────────────┐      Interruption / SIGKILL / Crash
      │ IN_PROGRESS │ ──────────────────────────────────────┐
      └──────┬──────┘                                       │
             │                                              ▼
             │ All batches committed                ┌─────────────┐
             ├─────────────────────────────────────►│ INTERRUPTED │
             │                                      └──────┬──────┘
             ▼                                             │ Resume Trigger
      ┌─────────────┐                                      │ (From last committed batch + 1)
      │  COMPLETED  │ ◄────────────────────────────────────┘
      └─────────────┘
             ▲
             │ Fatal unrecoverable DB error
             │
      ┌─────────────┐
      │   FAILED    │
      └─────────────┘
```

---

### 4.5 Invariant Decision Algorithm (Pseudo-Code)

```go
func ProcessRecord(tx *sql.Tx, jobID string, batchIdx, recIdx int, r SupplierUpdate) (ChangeType, error) {
    // 1. Invariant D: Dead-Letter Validation
    if err := r.Validate(); err != nil {
        if err := InsertDeadLetter(tx, jobID, batchIdx, recIdx, r, err.Error()); err != nil {
            return "", err
        }
        return ChangeDeadLetter, nil
    }

    priceCents := int64(math.Round(r.Price * 100))

    // Query current state for SKU
    current, exists, err := GetProductForUpdate(tx, r.SKU)
    if err != nil {
        return "", err
    }

    // New Product Insertion
    if !exists {
        status := StatusActive
        if strings.ToUpper(r.Status) == string(StatusWithdrawn) {
            status = StatusWithdrawn
        }
        if err := InsertProduct(tx, r.SKU, r.Name, priceCents, status, r.Timestamp); err != nil {
            return "", err
        }
        if status == StatusWithdrawn {
            return ChangeWithdrawn, nil
        }
        return ChangeAdded, nil
    }

    // 2. Invariant C: Tombstone Preservation Check
    if current.Status == StatusWithdrawn {
        // Product is withdrawn. Any attempt to set to ACTIVE is rejected.
        // Even if T_new > T_old, tombstone is terminal.
        return ChangeSkipped, nil
    }

    // 3. Invariant B: Fast-Forward Out-of-Order Check
    if r.Timestamp.Before(current.SupplierUpdatedAt) {
        // T_new < T_old: stale update, drop and skip
        return ChangeSkipped, nil
    }

    if r.Timestamp.Equal(current.SupplierUpdatedAt) {
        // Identical timestamp: Invariant A idempotency check
        if current.Name == r.Name && current.PriceCents == priceCents && string(current.Status) == strings.ToUpper(r.Status) {
            return ChangeSkipped, nil
        }
        // Identical timestamp with differing payload: deterministic skip to avoid drift
        return ChangeSkipped, nil
    }

    // At this point: current.Status == ACTIVE and r.Timestamp > current.SupplierUpdatedAt
    newStatus := StatusActive
    if strings.ToUpper(r.Status) == string(StatusWithdrawn) {
        newStatus = StatusWithdrawn
    }

    if err := UpdateProduct(tx, r.SKU, r.Name, priceCents, newStatus, r.Timestamp); err != nil {
        return "", err
    }

    if newStatus == StatusWithdrawn {
        return ChangeWithdrawn, nil
    }
    return ChangeUpdated, nil
}
```

---

### 4.6 Verification Test Tier Matrix (Guided by Invariants)

| Test Tier | Scope & Focus | Verification Method & Target Acceptance Criteria |
|:---------:|:-------------|:-------------------------------------------------|
| **Tier 1: Unit Tests** | Pure domain logic & validation | - Test `r.Validate()` with negative prices, whitespace/empty SKUs, invalid timestamps, invalid status.<br>- Verify deterministic PRNG generator output given same seed.<br>- Invariant B timestamp comparison unit tests.<br>- Target: 100% logic coverage of domain package. |
| **Tier 2: Transaction & Storage Integration** | SQLite transactional boundaries & schema constraints | - Verify ACID atomic rollback when transaction errors out.<br>- Test SQLite WAL mode durability.<br>- Verify `dead_letter_records` insertion within same transaction as valid records.<br>- Verify unique constraint on SKU.<br>- Verify checkpoint row update on batch commit. |
| **Tier 3: Engine Synchronization** | Full batch pipeline execution & core invariants | - **AC1**: Ingest identical batch twice $\to$ Assert $\Delta A=0, \Delta U=0, \Delta W=0, \Delta S=N$, DB state hash unchanged.<br>- **AC2**: Ingest record with $T_{new} < T_{old}$ $\to$ Assert skipped, local record unchanged.<br>- **AC3**: Ingest record with `ACTIVE` for existing `WITHDRAWN` $\to$ Assert status remains `WITHDRAWN`.<br>- **AC4**: Ingest batch with mixed valid and corrupt records $\to$ Assert dead-letter isolation without failing valid records. |
| **Tier 4: Crash Resumability & Chaos E2E** | Hard interruption & recovery simulation | - **AC5**: Simulate crash midway through 10-batch sync (kill worker at batch 5). Verify batches 1–4 are committed, batch 5 uncommitted changes rolled back.<br>- **AC6**: Restart engine. Verify job status marked `INTERRUPTED`.<br>- **AC7**: Trigger resume. Verify sync continues from batch 5 to 10 without reprocessing 1–4, and status transitions to `COMPLETED`. |
| **Tier 5: System, Security & Packaging** | Docker packaging, Web UI, Gitleaks, CI | - Multi-stage Docker image built $\le 25\text{MB}$.<br>- `docker compose up` starts app and mounts volume for database; verify data survives restart.<br>- Gitleaks scan passes with 0 leaks.<br>- Embedded Web UI serves HTML dashboard at `/` and handles API requests cleanly. |

---

## 5. Verification Method

To independently verify the completeness, correctness, and fidelity of this specification report:

1. **Verify Requirements Traceability**:
   - Compare Section 1.2 with `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`. Ensure every requirement (R1 through R5) and every bullet point is mapped directly into Section 4.1 (`Features Discovered`) and Section 4.2 (`Edge Cases Matrix`).
2. **Verify Acceptance Criteria Coverage**:
   - Compare Section 1.3 with Section 4.6 (`Verification Test Tier Matrix`). Every acceptance criterion (AC1 through AC13) must be explicitly covered by a dedicated test tier and clear pass/fail assertions.
3. **Verify Formal Invariant Definitions**:
   - Review Section 2.1 and Section 4.5 against the mathematical definitions:
     - Invariant A: Deterministic idempotency ($f(f(S, B), B) = f(S, B)$).
     - Invariant B: Fast-forward out-of-order rejection ($T_{new} < T_{old}$).
     - Invariant C: Monotonic tombstone state ($\text{ACTIVE} \to \text{WITHDRAWN}$ terminal).
     - Invariant D: Dead-letter isolation ($V(R) = \text{Invalid} \implies \text{Quarantine}$, batch continues).
     - Invariant E: Atomic batch transactions & crash checkpoint resumability.
4. **Invalidation Conditions**:
   - If any requirement from `ORIGINAL_REQUEST.md` is omitted or altered.
   - If a proposed invariant contradicts SQLite transactional guarantees or ACID properties.
   - If any domain entity required by `DISPATCH.md` (Product, Supplier Update, Batch, Job, Checkpoint, Dead-Letter Record, Change Summary) is missing from the schemas.
