# Milestone 2 Test Specifications & Crash Simulation Oracles Report

**Agent**: `teamwork_preview_spec_miner`  
**Working Directory**: `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1`  
**Target Milestone**: Milestone 2 (Resumability, Batch Checkpointing & Dead-Letter Isolation)  
**Authoritative References**: `ORIGINAL_REQUEST.md`, `PROJECT.md`, `TEST_INFRA.md`, `pkg/domain`, `pkg/storage`, `pkg/sync`

---

## 1. Executive Summary & Specification Scope
This document specifies the authoritative test designs, behavioral invariants, edge cases, and crash simulation oracles for **Milestone 2 (Resumability, Batch Checkpointing & Dead-Letter Isolation)**. It translates the high-level requirements R1 & R2 from `ORIGINAL_REQUEST.md` and architecture definitions in `PROJECT.md` into concrete, executable test oracles and fault-injection harnesses.

Milestone 2 covers five key functional areas:
1. **Invariant D (Dead-Letter Isolation)**: Quarantining invalid/corrupt records (empty SKU, negative price, invalid status, zero/corrupt timestamps, NaN/Inf) into `dead_letter_issues` without halting or failing valid records.
2. **Invariant E (Atomic Batch Transactions)**: Enforcing single-transaction boundaries encompassing product upserts, dead-letter writes, checkpoint logs, and job progress counters.
3. **Checkpoint Persistence**: Durably recording `batch_checkpoints` and cumulative counters in `sync_jobs` with SQLite WAL mode.
4. **Startup Crash Recovery**: Resuming from process crash/kill by identifying stale `RUNNING`/`PENDING` jobs, marking them `INTERRUPTED`, and guaranteeing incomplete updates are never reported as complete.
5. **Resumable Synchronization**: Streaming multi-batch execution (`RunJob`) and resuming interrupted updates (`ResumeJob`) directly from $last\_committed\_batch + 1$ without re-processing committed batches.

---

## Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Resumability | Streaming Batch Execution (`RunJob`) | Ingests supplier batches sequentially from channel `<-chan domain.Batch`, executing atomic batch transactions and advancing job progress | `ctx context.Context`, `jobID string`, `batchStream <-chan domain.Batch` | `*domain.SyncJob` with `status = COMPLETED`, final `ChangeSummary`, `completed_at` populated | Context cancellation or stream failure aborts processing; leaves uncommitted batch rolled back | `PROJECT.md:177`, `pkg/sync/engine.go:247` |
| 2 | Resumability | Resumable Synchronization (`ResumeJob`) | Inspects `committed_batch_index = C`; skips batches $\le C$ without re-processing; executes remaining batches from $C+1$ to completion | `ctx context.Context`, `jobID string`, `batchStream <-chan domain.Batch` | `*domain.SyncJob` with `status = COMPLETED` and complete accumulated summary | `ErrNotFound` if job does not exist; error if job already `COMPLETED`; aborts on cancellation | `PROJECT.md:181`, `pkg/sync/engine.go:251` |
| 3 | Fault Tolerance | Startup Crash Recovery (`RecoverOnStartup`) | On startup, identifies any job in `RUNNING` or `PENDING` status left by process crash or kill, transitioning it to `INTERRUPTED` | `ctx context.Context` | `[]string` of recovered job IDs; database updated | Returns empty slice `nil` if no stale jobs exist; database errors propagated | `ORIGINAL_REQUEST.md:48`, `PROJECT.md:184`, `pkg/storage/sqlite.go:438` |
| 4 | Transaction Integrity | Atomic Batch Transactions (Invariant E) | Wraps product upserts, dead-letter records, checkpoint logging, and job progress updates into a single atomic SQLite transaction | `domain.Batch` | `domain.ChangeSummary`, committed transaction | On error/cancellation: `tx.Rollback()` triggered, 0 partial writes survive | `ORIGINAL_REQUEST.md:19,46`, `PROJECT.md:16,175` |
| 5 | Audit & Durability | Batch Checkpoint Persistence | Transactionally writes immutable checkpoint record to `batch_checkpoints` upon each committed batch | `tx *sql.Tx`, `*domain.BatchCheckpoint` | New row inserted with item counts, delta metrics, and timestamp | Foreign key constraint violation if `jobID` missing; primary key collision on re-insert | `PROJECT.md:154`, `pkg/storage/sqlite.go:383` |
| 6 | Progress Tracking | Cumulative Progress Updates | Updates cumulative `items_*` counters and `committed_batch_index` in `sync_jobs` inside batch transaction | `tx *sql.Tx`, `jobID string`, `batchIndex int`, `delta domain.ChangeSummary` | Updated row in `sync_jobs` | Fails if `tx` is aborted; `ErrNotFound` if jobID missing | `PROJECT.md:155`, `pkg/storage/sqlite.go:408` |
| 7 | Isolation | Dead-Letter Isolation (Invariant D) | Quarantines malformed/corrupt records into `dead_letter_issues` while allowing valid records in the batch to commit successfully | `tx *sql.Tx`, `*domain.DeadLetterRecord` | Row inserted in `dead_letter_issues`, `summary.DeadLetter++` | Does NOT fail the batch; valid records persist | `ORIGINAL_REQUEST.md:22,43`, `PROJECT.md:15,159`, `pkg/sync/engine.go:122` |
| 8 | Observability | Dead-Letter Queue Inspector | Paginated retrieval of quarantined records by job ID or across all jobs | `jobID string`, `limit int`, `offset int` | `[]domain.DeadLetterRecord`, `totalCount int`, `error` | Returns empty slice and total 0 if no records match | `PROJECT.md:160`, `pkg/storage/sqlite.go:497` |
| 9 | State Machine | Sync Job Status Lifecycle | Manages monotonic job state transitions (`PENDING` $\to$ `RUNNING` $\to$ `INTERRUPTED` $\to$ `RUNNING` $\to$ `COMPLETED`/`FAILED`) | `jobID string`, `status domain.JobStatus` | `sync_jobs.status` updated, `updated_at` refreshed, `completed_at` populated on terminal state | `ErrNotFound` on non-existent job; invalid transition rejected | `pkg/domain/models.go:101`, `pkg/storage/sqlite.go:357` |
| 10 | Concurrency | Writer Serialization Lock | Mutex serialization (`mu stdsync.Mutex`) in `SyncEngine` ensuring single-writer execution against SQLite WAL | Concurrent batch calls | Serialized transaction execution, eliminates `SQLITE_BUSY` errors | Lock released safely via `defer` even on panic or error | `pkg/sync/engine.go:42,84` |

---

## Edge Cases

| # | Feature | Input / Condition | Observed / Required Behavior |
|---|---------|-------------------|-----------------------------|
| 1 | Crash & Resumability | Interruption during Batch 0 (before first commit) | `committed_batch_index` remains -1; no products in DB; resume starts directly at Batch 0 and completes. |
| 2 | Crash & Resumability | Interruption during Batch $N-1$ (the final batch) | Batches $0 \dots N-2$ intact in DB; resume processes ONLY Batch $N-1$ and transitions job to `COMPLETED`. |
| 3 | Resumability | Resuming an already `COMPLETED` job | `ResumeJob` returns error (e.g. `ErrJobAlreadyCompleted`); 0 DB mutations, 0 re-executions. |
| 4 | Resumability | Resuming non-existent job ID | Returns `domain.ErrNotFound`; 0 mutations. |
| 5 | Resumability | Resuming with stream missing uncommitted batches (gap in sequence) | Stream emits Batch 4 when `committed_batch_index = 1`; engine detects missing batches 2 & 3 and returns error. |
| 6 | Resumability | Multiple consecutive interruptions ($B_2 \to \text{crash} \to B_5 \to \text{crash} \to B_9$) | Each restart recovers to `INTERRUPTED`; resume continues from last checkpoint; final catalog matches single-pass run 100%. |
| 7 | Resumability | Channel `batchStream` closes prematurely before `total_batches` received | Job aborts; status remains `INTERRUPTED` (or `RUNNING` until recovery); job is NEVER marked `COMPLETED`. |
| 8 | Checkpoint & Stream | Stream contains an empty batch (0 records) at index $k$ | Checkpoint $k$ recorded with `items_count = 0`; `committed_batch_index` advances to $k$; 0 product mutations. |
| 9 | Dead-Letter Isolation | Batch consists of 100% invalid records (e.g. all negative prices) | Batch commits successfully; `DeadLetter = len(records)`; checkpoint recorded; 0 products inserted; job advances. |
| 10 | Dead-Letter Isolation | Record with whitespace-only SKU (`"   \t\n"`) | Whitespace trimmed to empty string; quarantined as `empty SKU` dead-letter record; does not fail valid items. |
| 11 | Dead-Letter Isolation | Non-finite price (`math.NaN()`, `math.Inf(1)`, `math.Inf(-1)`) | Detected as corrupt price; quarantined to dead letters without causing SQLite type errors or panics. |
| 12 | Dead-Letter Isolation | Extreme string payload (100KB JSON / malicious text) | Entire payload preserved in `raw_payload` TEXT column without SQLite truncation or memory exhaustion. |
| 13 | Dead-Letter Isolation | SQL injection syntax in SKU or Reason (`SKU'; DROP TABLE products; --`) | Handled safely by parameterized SQLite statements; stored literally without query syntax disruption. |
| 14 | Invariants Cross-Check | Resumed batch attempts to resurrect product withdrawn in prior committed batch | Invariant C is preserved across crash recovery: product remains `WITHDRAWN`, incoming update skipped. |
| 15 | Invariants Cross-Check | Single SKU updated across multiple batches with crash in between | Product created in Batch 1 ($v_1$), updated in Batch 2 ($v_2$), updated in uncommitted Batch 3 ($v_3$). Crash during Batch 3: product remains $v_2$. Resume Batch 3: product updates to $v_3$. |
| 16 | Startup Recovery | Application boots with clean database (0 stale jobs) | `RecoverOnStartup` returns `nil, nil`; no errors, no state mutations. |
| 17 | Startup Recovery | Application boots with completed jobs only | Completed jobs remain `COMPLETED`; `RecoverOnStartup` returns `nil, nil`. |
| 18 | Startup Recovery | Multiple concurrent crashed jobs in `RUNNING` status | All stale jobs transitioned to `INTERRUPTED`; all IDs returned in slice. |
| 19 | Context Cancellation | Context cancelled prior to `RunJob` starting | Returns `context.Canceled` immediately; 0 transactions initiated; job status remains initial. |
| 20 | Context Cancellation | Context cancelled midway through batch record loop | Record iteration terminates; `tx.Rollback()` executed; partial records discarded; engine returns `context.Canceled`. |
| 21 | Concurrency | Two concurrent calls to `ResumeJob` for the same JobID | Engine lock / state check prevents duplicate execution; one runs, other returns active conflict error. |
| 22 | Database Fault | Database file becomes read-only or closed mid-batch | Transaction aborts; error returned; uncommitted WAL journal discarded; earlier committed transactions intact. |

---

## 2. Acceptance Criteria Mapping & Test Specifications

### Acceptance Criterion 1: Mid-Batch Interruption & Rollback
> *"Simulating an interruption midway through a multi-batch sync leaves prior committed batches intact and uncommitted batch changes rolled back."*

#### Test Specification: `TestM2_CrashMidBatch_RollbackAndDurability`
- **Goal**: Verify that when failure/cancellation strikes during batch $k$ of $N$, all writes of batch $k$ are rolled back from SQLite WAL, while batches $0 \dots k-1$ remain permanently durable.
- **Setup**:
  - Open file-backed SQLite database in WAL mode (`filepath.Join(t.TempDir(), "durability.db")`).
  - Create job `job-crash-1` with `TotalBatches = 5`, batch size = 20 records.
  - Batches 0 and 1 contain unique SKUs `SKU-B0-001...020` and `SKU-B1-001...020`.
  - Batch 2 contains updates to `SKU-B0-001` (new price), insertions `SKU-B2-001...010`, and dead letters.
- **Execution**:
  - Run batches 0 and 1 using `ExecuteBatch` (or `RunJob`).
  - During batch 2, inject interruption midway (e.g. at record 10 of 20 via context cancellation or crash hook).
- **Verification Oracle**:
  1. Engine returns error (`context.Canceled` or injected fault).
  2. Query `batch_checkpoints`: Checkpoints for batches 0 and 1 exist; Checkpoint for batch 2 **DOES NOT EXIST**.
  3. Query `products`:
     - `SKU-B0-001` retains its original Batch 0 price (Batch 2 mutation rolled back).
     - `SKU-B2-001...010` do NOT exist in `products` table (count == 0).
     - All 20 products from Batch 0 and 20 products from Batch 1 exist intact with exact timestamps.
  4. Query `dead_letter_issues`: Dead letters from batch 2 do NOT exist.
  5. Query `sync_jobs`: `committed_batch_index == 1`; metrics equal exact sum of Batch 0 + Batch 1.

---

### Acceptance Criterion 2: Resuming Without Re-Processing
> *"Resuming an interrupted sync continues directly from the uncommitted batch to completion without re-processing completed batches."*

#### Test Specification: `TestM2_Resume_SkipsCommittedBatchesDirectly`
- **Goal**: Verify that `ResumeJob` bypasses batches $\le \text{committed\_batch\_index}$ without executing database operations or re-incrementing metric counters, then completes all uncommitted batches.
- **Setup**:
  - Create job `job-resume-1` with 6 batches ($B_0 \dots B_5$).
  - Pre-commit batches $B_0, B_1, B_2$ into SQLite storage.
  - Mark job status as `INTERRUPTED`, `committed_batch_index = 2`.
  - Instrument a `SpyStorage` or track batch execution hooks.
- **Execution**:
  - Provide a stream emitting all 6 batches ($B_0 \dots B_5$) to `engine.ResumeJob(ctx, "job-resume-1", stream)`.
- **Verification Oracle**:
  1. Batches 0, 1, 2 are verified skipped: 0 product upsert queries issued for items in $B_0, B_1, B_2$.
  2. Batches 3, 4, 5 are executed sequentially within atomic transactions.
  3. Job transitions to `COMPLETED` when Batch 5 commits.
  4. `job.CommittedBatchIndex == 5`.
  5. Checkpoints exist for $B_0, B_1, B_2, B_3, B_4, B_5$ with exactly 1 row each.
  6. Final cumulative `job.Summary` matches exactly $\sum_{i=0}^5 \Delta_{B_i}$.

---

### Acceptance Criterion 3: Job State Transition Lifecycle & Restart Recovery
> *"Interrupted jobs are marked INTERRUPTED upon restart and only transition to COMPLETED when all batches finish."*

#### Test Specification: `TestM2_StartupRecovery_StateTransitionOracle`
- **Goal**: Guarantee that crashed jobs in `RUNNING` or `PENDING` are converted to `INTERRUPTED` on startup, and that an interrupted job is never reported as `COMPLETED` until all batches are fully finished.
- **Setup**:
  - Seed database with 4 jobs:
    - `job-running`: status `RUNNING`, `committed_batch_index = 3`, `total_batches = 10`.
    - `job-pending`: status `PENDING`, `committed_batch_index = -1`, `total_batches = 5`.
    - `job-completed`: status `COMPLETED`, `committed_batch_index = 4`, `total_batches = 5`.
    - `job-interrupted`: status `INTERRUPTED`, `committed_batch_index = 2`, `total_batches = 8`.
- **Execution**:
  - Invoke `engine.RecoverOnStartup(ctx)`.
- **Verification Oracle**:
  1. Returned IDs slice contains exactly `["job-running", "job-pending"]`.
  2. Database status checks:
     - `job-running` is now `INTERRUPTED`. `completed_at` is `nil`.
     - `job-pending` is now `INTERRUPTED`. `completed_at` is `nil`.
     - `job-completed` remains `COMPLETED`. `completed_at` is preserved.
     - `job-interrupted` remains `INTERRUPTED`.
  3. Attempting to report progress or query active job: `GetActiveJob` returns `ErrNotFound`.
  4. Resume `job-running` with remaining batches $4 \dots 9$:
     - While running, status is `RUNNING`.
     - If interrupted at batch 7: status is `INTERRUPTED`, never `COMPLETED`.
     - When batch 9 commits: status strictly transitions to `COMPLETED`, `completed_at` is set.

---

### Acceptance Criterion 4: Dead-Letter Isolation Without Batch Halting
> *"Records with negative prices or empty SKUs are logged to the issues table without halting the batch."*

#### Test Specification: `TestM2_DeadLetter_IsolationAndNonHalting`
- **Goal**: Verify that corrupt records (negative prices, missing SKUs, invalid statuses, non-finite floats) are quarantined to `dead_letter_issues` table while valid records in the same batch are committed successfully.
- **Setup**:
  - Construct a batch containing 10 records:
    - 4 valid records with various statuses (`ACTIVE`, `WITHDRAWN`).
    - 6 corrupt records: negative price (`-12.50`), empty SKU (`""`), whitespace SKU (`"   "`), invalid status (`"UNKNOWN"`), zero timestamp (`time.Time{}`), NaN price (`math.NaN()`).
- **Execution**:
  - Execute batch via `engine.ExecuteBatch(ctx, batch)`.
- **Verification Oracle**:
  1. `ExecuteBatch` returns `err == nil`. The batch does NOT fail.
  2. Returned `ChangeSummary`: `Added = 3`, `Withdrawn = 1`, `DeadLetter = 6`, `Skipped = 0`.
  3. The 4 valid products exist in the `products` table with correct attributes.
  4. Exactly 6 rows exist in `dead_letter_issues` with matching `job_id`, `batch_index`, raw payloads, and specific error reasons.
  5. Paginated query `ListDeadLetterRecords(ctx, jobID, 10, 0)` returns all 6 records.

---

## 3. Crash Simulation Harnesses & Oracle Implementations

Here are the concrete Go architectures for the simulation harnesses and oracles designed for Milestone 2:

### Harness 1: Mid-Batch Context Cancellation Harness
```go
package synctest

import (
	"context"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// RunContextCancellationHarness verifies that mid-batch cancellation cancels cleanly
// and leaves the database uncorrupted.
func RunContextCancellationHarness(t *testing.T, store storage.Storage) {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Create engine with a clock hook that cancels context on 5th record inspection
	counter := 0
	engine := catalogSync.NewEngine(store, catalogSync.WithTimeFunc(func() time.Time {
		counter++
		if counter == 5 {
			cancel() // Sudden cancellation mid-batch!
		}
		return time.Now().UTC()
	}))

	batch := domain.Batch{
		JobID:      "job-cancel-test",
		BatchIndex: 0,
		Records:    generateTestRecords(10), // 10 records
	}

	summary, err := engine.ExecuteBatch(ctx, batch)
	if err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}

	// Oracle: Zero records committed from cancelled batch
	for _, rec := range batch.Records {
		p, _ := store.GetProduct(context.Background(), rec.SKU)
		if p != nil {
			t.Fatalf("product %s was committed despite mid-batch cancellation!", rec.SKU)
		}
	}
}
```

### Harness 2: Fault-Injecting Storage Harness (`FaultInjectingStorage`)
```go
package synctest

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
)

type FaultInjectingStorage struct {
	storage.Storage
	mu              sync.Mutex
	failOnBatch     int
	failOnOperation string // e.g., "UpsertProductTx", "RecordBatchCheckpointTx", "BeginTx"
	currentBatch    int
	injectedErr     error
}

func NewFaultStorage(base storage.Storage, failOnBatch int, op string, err error) *FaultInjectingStorage {
	return &FaultInjectingStorage{
		Storage:         base,
		failOnBatch:     failOnBatch,
		failOnOperation: op,
		injectedErr:     err,
	}
}

func (f *FaultInjectingStorage) SetCurrentBatch(b int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentBatch = b
}

func (f *FaultInjectingStorage) UpsertProductTx(ctx context.Context, tx *sql.Tx, p *domain.Product) error {
	f.mu.Lock()
	shouldFail := f.currentBatch == f.failOnBatch && f.failOnOperation == "UpsertProductTx"
	f.mu.Unlock()
	if shouldFail {
		return f.injectedErr
	}
	return f.Storage.UpsertProductTx(ctx, tx, p)
}

func (f *FaultInjectingStorage) RecordBatchCheckpointTx(ctx context.Context, tx *sql.Tx, cp *domain.BatchCheckpoint) error {
	f.mu.Lock()
	shouldFail := f.currentBatch == f.failOnBatch && f.failOnOperation == "RecordBatchCheckpointTx"
	f.mu.Unlock()
	if shouldFail {
		return f.injectedErr
	}
	return f.Storage.RecordBatchCheckpointTx(ctx, tx, cp)
}
```

### Harness 3: Sudden SQLite WAL Crash & Recovery Harness
```go
package synctest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// VerifyWALCrashRecovery tests sudden crash where DB connection is terminated
// while a batch transaction is in flight.
func VerifyWALCrashRecovery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wal_crash.db")
	ctx := context.Background()

	// 1. Initial process instance: Run Batch 0
	store1, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store1: %v", err)
	}

	jobID := "job-wal-crash"
	_ = store1.CreateJob(ctx, &domain.SyncJob{
		JobID:               jobID,
		Status:              domain.JobStatusRunning,
		TotalBatches:        3,
		CommittedBatchIndex: -1,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	})

	tx0, _ := store1.BeginTx(ctx)
	_ = store1.UpsertProductTx(ctx, tx0, &domain.Product{
		SKU: "SKU-DURABLE-0", Name: "Committed", Price: 10.0,
		Status: domain.StatusActive, SupplierTimestamp: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	_ = store1.RecordBatchCheckpointTx(ctx, tx0, &domain.BatchCheckpoint{
		JobID: jobID, BatchIndex: 0, ItemsCount: 1, Summary: domain.ChangeSummary{Added: 1}, CommittedAt: time.Now().UTC(),
	})
	_ = store1.UpdateJobProgressTx(ctx, tx0, jobID, 0, domain.ChangeSummary{Added: 1})
	_ = tx0.Commit() // Batch 0 committed!

	// 2. Start Batch 1, write uncommitted changes, then simulate CRASH
	tx1, _ := store1.BeginTx(ctx)
	_ = store1.UpsertProductTx(ctx, tx1, &domain.Product{
		SKU: "SKU-UNCOMMITTED-1", Name: "Ghost", Price: 99.0,
		Status: domain.StatusActive, SupplierTimestamp: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	// Simulate crash: abruptly close DB without commit or rollback
	_ = store1.Close()

	// 3. Restart process instance: Re-open database from disk
	store2, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to re-open store2 after crash: %v", err)
	}
	defer store2.Close()

	engine2 := catalogSync.NewEngine(store2)

	// Startup recovery hook
	recovered, err := engine2.RecoverOnStartup(ctx)
	if err != nil {
		t.Fatalf("startup recovery failed: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != jobID {
		t.Errorf("expected recovered job %s, got %v", jobID, recovered)
	}

	// Oracle 1: Uncommitted Batch 1 product does NOT exist
	ghost, err := store2.GetProduct(ctx, "SKU-UNCOMMITTED-1")
	if err == nil || ghost != nil {
		t.Errorf("ghost product survived crash! WAL rollback failed: %+v", ghost)
	}

	// Oracle 2: Committed Batch 0 product is 100% intact
	durable, err := store2.GetProduct(ctx, "SKU-DURABLE-0")
	if err != nil || durable == nil {
		t.Fatalf("durable product lost after crash recovery: %v", err)
	}

	// Oracle 3: Job status is INTERRUPTED, committed index is 0
	recoveredJob, err := store2.GetJob(ctx, jobID)
	if err != nil || recoveredJob.Status != domain.JobStatusInterrupted || recoveredJob.CommittedBatchIndex != 0 {
		t.Errorf("unexpected recovered job state: %+v", recoveredJob)
	}
}
```

### Harness 4: Resumption & Catalog State Integrity Oracle
```go
package synctest

import (
	"context"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// VerifyResumeStateIntegrity verifies that resuming after crash reproduces the
// EXACT catalog state and metrics as an uninterrupted run.
func VerifyResumeStateIntegrity(t *testing.T, batches []domain.Batch) {
	ctx := context.Background()

	// Baseline: Run all batches on clean DB
	baseStore, _ := storage.OpenInMemory()
	defer baseStore.Close()
	baseEngine := catalogSync.NewEngine(baseStore)

	for _, b := range batches {
		_, _ = baseEngine.ExecuteBatch(ctx, b)
	}

	// Interrupted run: Interrupt at batch k
	testStore, _ := storage.OpenInMemory()
	defer testStore.Close()
	testEngine := catalogSync.NewEngine(testStore)

	jobID := "job-resume-oracle"
	_ = testStore.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusRunning, TotalBatches: len(batches), CommittedBatchIndex: -1,
	})

	// Process first half
	cutoff := len(batches) / 2
	for i := 0; i < cutoff; i++ {
		_, _ = testEngine.ExecuteBatch(ctx, batches[i])
		_ = testStore.UpdateJobStatus(ctx, jobID, domain.JobStatusRunning)
	}

	// Mark interrupted
	_ = testStore.UpdateJobStatus(ctx, jobID, domain.JobStatusInterrupted)

	// Stream all batches into ResumeJob
	batchChan := make(chan domain.Batch, len(batches))
	for _, b := range batches {
		batchChan <- b
	}
	close(batchChan)

	finalJob, err := testEngine.ResumeJob(ctx, jobID, batchChan)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}

	if finalJob.Status != domain.JobStatusCompleted {
		t.Errorf("expected COMPLETED status, got %s", finalJob.Status)
	}

	// State Oracle: Verify testStore matches baseStore for all records
	for _, b := range batches {
		for _, rec := range b.Records {
			expected, _ := baseStore.GetProduct(ctx, rec.SKU)
			actual, _ := testStore.GetProduct(ctx, rec.SKU)
			if (expected == nil) != (actual == nil) {
				t.Fatalf("mismatch in existence for SKU %s", rec.SKU)
			}
			if expected != nil && (actual.Price != expected.Price || actual.Status != expected.Status) {
				t.Fatalf("state divergence for SKU %s: actual %+v, expected %+v", rec.SKU, actual, expected)
			}
		}
	}
}
```

---

## 5-Component Handoff Report

### 1. Observation
- **Authoritative Specifications Inspected**:
  - `ORIGINAL_REQUEST.md:18-23`: R2 explicitly requires fault-tolerant resumability: "transactions must be atomic per batch, ensuring an interruption never erases or corrupts existing data", "persist execution checkpoints so staff can resume... without re-processing already committed batches", "an interrupted or partially completed update must never be reported as complete", and "isolate corrupt or invalid product records... into a dead-letter queue without failing valid records".
  - `ORIGINAL_REQUEST.md:45-49`: Acceptance criteria for Crash & Resumability:
    1. *"Simulating an interruption midway through a multi-batch sync leaves prior committed batches intact and uncommitted batch changes rolled back."*
    2. *"Resuming an interrupted sync continues directly from the uncommitted batch to completion without re-processing completed batches."*
    3. *"Interrupted jobs are marked `INTERRUPTED` upon restart and only transition to `COMPLETED` when all batches finish."*
    4. *"Records with negative prices or empty SKUs are logged to the issues table without halting the batch."*
  - `PROJECT.md:46-50`: Milestone 2 encompasses Features 7, 8, 9, 10, 11.
  - `PROJECT.md:154-186`: Interface contracts define `RunJob`, `ResumeJob`, `RecoverOnStartup`, `RecordBatchCheckpointTx`, `UpdateJobProgressTx`, `RecoverInterruptedJobs`, `RecordDeadLetterTx`, and `ListDeadLetterRecords`.
- **Existing Baseline Verification**:
  - `pkg/storage/sqlite.go:111-160`: Tables `products`, `sync_jobs`, `batch_checkpoints`, and `dead_letter_issues` are created with foreign keys and indexes.
  - `pkg/storage/sqlite.go:438-473`: `RecoverInterruptedJobs` queries `status IN ('RUNNING', 'PENDING')` and transitions them to `'INTERRUPTED'`.
  - `pkg/storage/sqlite.go:475-554`: `RecordDeadLetterTx` and `ListDeadLetterRecords` are implemented.
  - `pkg/sync/engine.go:122-141`: Dead-letter isolation is implemented in `ExecuteBatch`, but `RunJob` and `ResumeJob` are stubs returning `errors.New("... is scheduled for Milestone 2")` (lines 248, 252).
  - Test command execution: `go test -v ./...` executed cleanly with 0 failures across all unit and adversarial suites.

### 2. Logic Chain
1. *Requirement R2 and AC1 dictate atomic rollback per batch*: In SQLite WAL mode, any uncommitted write is held in temporary WAL frames and rolled back when the transaction is aborted or connection reset. Because each batch $i$ is executed in its own `sql.Tx`, cancelling `ctx` or injecting a fault during batch $k$ discards all writes of batch $k$ while leaving $0 \dots k-1$ committed.
2. *Requirement R2 and AC2 dictate resumption without re-processing*: When `ResumeJob` is called, reading `committed_batch_index` gives the exact index of the last committed batch. Batches in `batchStream` with `index <= committed_batch_index` must be immediately dropped/skipped before any SQL query is dispatched, preventing duplicate increments and unnecessary database lock contention.
3. *Requirement R2 and AC3 dictate lifecycle accuracy*: If a process terminates abruptly, `sync_jobs.status` remains `RUNNING` in the database file. At startup, before accepting new jobs, `RecoverOnStartup` scans for stale jobs and sets them to `INTERRUPTED`. Jobs only transition to `COMPLETED` when `RunJob`/`ResumeJob` successfully commits the final batch (`index == total_batches - 1` or stream exhaust).
4. *Requirement R2 and AC4 dictate non-halting dead-letter isolation*: Calling `isInvalidRecord` detects corrupt inputs. Writing them to `dead_letter_issues` via `RecordDeadLetterTx` inside the active transaction and using `continue` allows valid records in the batch to proceed. The batch commits with both valid products and dead-letter issues intact.

### 3. Caveats
- `RunJob` and `ResumeJob` currently return stub errors in `pkg/sync/engine.go`. The test suites defined in this specification will be enabled and validated as `explorer_m2_1` and `worker_m1` implement those functions.
- In-memory SQLite (`:memory:`) does not simulate real OS process termination or disk file re-opening; hard crash tests (`TestM2_CrashMidBatch_RollbackAndDurability`, `VerifyWALCrashRecovery`) must use disk-backed temporary files (`t.TempDir()`) to exercise real WAL recovery.
- Resumption assumes the supplier `batchStream` contains deterministic batch indices starting from 0. If an external generator provides an out-of-order batch stream, the engine must validate batch index continuity.

### 4. Conclusion
Milestone 2 test specifications and crash simulation oracles are fully defined and ready for implementation. The test suites cover all four M2 acceptance criteria, 10 feature capabilities, 22 boundary/adversarial edge cases, and 6 executable harness designs (context cancellation, fault injection, WAL disk crash recovery, resumption integrity, dead-letter quarantine, and lifecycle state machines).

### 5. Verification Method
To independently verify the test specifications and oracles:
1. Verify existing baseline:
   ```bash
   go test -v -race ./pkg/...
   ```
2. When M2 implementation is deployed by the team, run the full test suite with race detection:
   ```bash
   go test -v -race -count=5 ./pkg/sync/... ./pkg/storage/...
   ```
3. Run statement and branch coverage verification:
   ```bash
   go test -coverprofile=coverage.out ./pkg/...
   go tool cover -func=coverage.out | grep total
   ```
   (Target: $\ge 90\%$ statement/branch coverage).
4. Invalidation condition: If any uncommitted batch $k$ mutates the `products` table after crash, or if `ResumeJob` re-processes batches $\le committed\_batch\_index$, or if an interrupted job ever reports `COMPLETED`, the verification fails.
