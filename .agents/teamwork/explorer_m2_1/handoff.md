# Milestone 2 Exploration & Design: Batch Checkpointing, Resumability & Dead-Letter Isolation

## 1. Observation

### Codebase Inspection Findings

1. **Domain Models (`pkg/domain/models.go`)**:
   - Lines 54–68: `SupplierUpdate.Validate()` validates SKU non-empty (`ErrEmptySKU`), non-negative price (`ErrNegativePrice`), valid status (`ErrInvalidStatus`), and non-zero timestamp (`ErrZeroTimestamp`).
   - Lines 70–75: `Batch` struct contains `JobID string`, `BatchIndex int`, and `Records []SupplierUpdate`.
   - Lines 77–98: `ChangeSummary` has fields `Added`, `Updated`, `Withdrawn`, `Skipped`, `DeadLetter`, with `Add(other)` and `TotalProcessed() int`.
   - Lines 100–109: `JobStatus` defines constants `JobStatusPending ("PENDING")`, `JobStatusRunning ("RUNNING")`, `JobStatusInterrupted ("INTERRUPTED")`, `JobStatusCompleted ("COMPLETED")`, `JobStatusFailed ("FAILED")`.
   - Lines 111–121: `SyncJob` contains `JobID`, `Status`, `TotalBatches`, `CommittedBatchIndex` (initialized to -1 before first batch), `Summary`, `CreatedAt`, `UpdatedAt`, and `CompletedAt *time.Time`.
   - Lines 123–130: `BatchCheckpoint` contains `JobID`, `BatchIndex`, `ItemsCount`, `Summary`, and `CommittedAt time.Time`.
   - Lines 132–141: `DeadLetterRecord` contains `IssueID`, `JobID`, `BatchIndex`, `SKU`, `RawPayload`, `Reason`, and `CreatedAt time.Time`.

2. **Storage Implementation (`pkg/storage/sqlite.go`)**:
   - Lines 16–39: Interface `storage.Storage` defines:
     ```go
     CreateJob(ctx context.Context, job *domain.SyncJob) error
     GetJob(ctx context.Context, jobID string) (*domain.SyncJob, error)
     GetActiveJob(ctx context.Context) (*domain.SyncJob, error)
     UpdateJobStatus(ctx context.Context, jobID string, status domain.JobStatus) error
     RecordBatchCheckpointTx(ctx context.Context, tx *sql.Tx, cp *domain.BatchCheckpoint) error
     UpdateJobProgressTx(ctx context.Context, tx *sql.Tx, jobID string, batchIndex int, delta domain.ChangeSummary) error
     RecoverInterruptedJobs(ctx context.Context) ([]string, error)
     RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
     ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
     ```
   - Lines 107–163: DDL schema creates tables with strict constraints:
     - `products` (sku PRIMARY KEY, CHECK status IN ('ACTIVE', 'WITHDRAWN'))
     - `sync_jobs` (job_id PRIMARY KEY, CHECK status IN ('PENDING', 'RUNNING', 'INTERRUPTED', 'COMPLETED', 'FAILED'), committed_batch_index DEFAULT -1)
     - `batch_checkpoints` (PRIMARY KEY (job_id, batch_index), `FOREIGN KEY (job_id) REFERENCES sync_jobs(job_id) ON DELETE CASCADE`)
     - `dead_letter_issues` (issue_id PRIMARY KEY, job_id, batch_index, sku, raw_payload, reason, created_at)
   - Lines 51–56: SQLite connection configured with `_pragma=foreign_keys(ON)`, `_pragma=busy_timeout(5000)`, `_pragma=journal_mode(WAL)`, `_txlock=immediate`, and `db.SetMaxOpenConns(1)`.

3. **Current Sync Engine (`pkg/sync/engine.go`)**:
   - Lines 17–29: Local `Storage` interface in `engine.go` only includes `DB()`, `Close()`, `BeginTx()`, `GetProduct()`, `GetProductTx()`, `UpsertProductTx()`, `RecoverInterruptedJobs()`, and `RecordDeadLetterTx()`. It is missing the job and checkpoint methods: `CreateJob`, `GetJob`, `GetActiveJob`, `UpdateJobStatus`, `RecordBatchCheckpointTx`, `UpdateJobProgressTx`.
   - Lines 72–243: `ExecuteBatch` executes batch records inside an atomic transaction, handling intra-batch dedup, Invariant D dead letters, Invariants A (Idempotency), B (Fast-forward timestamps), and C (Tombstone preservation).
   - Lines 247–254:
     ```go
     // RunJob processes all batches from a batch reader/generator. Scheduled for Milestone 2.
     func (e *SyncEngine) RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
         return nil, errors.New("RunJob is scheduled for Milestone 2")
     }

     // ResumeJob restarts an interrupted job from its last committed batch checkpoint. Scheduled for Milestone 2.
     func (e *SyncEngine) ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
         return nil, errors.New("ResumeJob is scheduled for Milestone 2")
     }
     ```
   - Lines 257–262: `RecoverOnStartup` delegates directly to `e.storage.RecoverInterruptedJobs(ctx)`.
   - Lines 290–301: `isInvalidRecord(sku, price, status)` checks empty SKU, price < 0, and invalid status, but does not yet check `record.Timestamp.IsZero()`.

4. **Test Suite Status**:
   - `go test -v ./...` executed cleanly: 100% PASS across `pkg/domain`, `pkg/storage`, and `pkg/sync`. Zero race conditions detected.

---

## 2. Logic Chain

1. **Storage Readiness (Observations 1 & 2)**:
   The database tables (`sync_jobs`, `batch_checkpoints`, `dead_letter_issues`, `products`) and all SQL queries needed for Milestone 2 already exist in `pkg/storage/sqlite.go` and are verified by unit tests. No DDL migrations or SQL modifications are required in `pkg/storage`.

2. **Interface Synchronization (Observation 3)**:
   In `pkg/sync/engine.go`, the consumer `Storage` interface must be updated to include `CreateJob`, `GetJob`, `GetActiveJob`, `UpdateJobStatus`, `RecordBatchCheckpointTx`, and `UpdateJobProgressTx`. Because `storage.SQLiteStorage` already implements all these methods, expanding `sync.Storage` will compile with zero friction.

3. **Foreign Key Invariant (Observation 2)**:
   Because SQLite enforces foreign keys (`PRAGMA foreign_keys = ON;`), inserting into `batch_checkpoints` requires `job_id` to exist in `sync_jobs`. Therefore:
   - `RunJob(ctx, jobID, stream)` must verify whether `jobID` exists in `sync_jobs`. If not found, it must insert a new record with status `RUNNING` and `committed_batch_index = -1`. If found in `PENDING`, it updates status to `RUNNING`. If already `COMPLETED`, it rejects with an error.
   - `ResumeJob(ctx, jobID, stream)` must load the existing job. If not found, it returns `domain.ErrNotFound`. If already `COMPLETED`, it rejects. If `INTERRUPTED`, it updates status to `RUNNING`.

4. **Atomic Batch Transaction Boundary (Invariant E)**:
   To satisfy Invariant E, every batch processed by `RunJob` or `ResumeJob` must commit all of the following inside a single `*sql.Tx`:
   a. Product mutations (`UpsertProductTx`)
   b. Dead-letter issues (`RecordDeadLetterTx`)
   c. Batch checkpoint entry (`RecordBatchCheckpointTx`)
   d. Cumulative progress counters in `sync_jobs` (`UpdateJobProgressTx`)
   If any operation fails, `tx.Rollback()` reverts all changes from that batch. The database remains exactly at `last_committed_batch`.

5. **Checkpoint Skip Logic for Resumption (Invariant E & R2)**:
   When reading from `batchStream <-chan domain.Batch`:
   - If `batch.BatchIndex <= job.CommittedBatchIndex`: The batch was already committed in a previous run. The engine discards it without touching the database or running invariant logic.
   - If `batch.BatchIndex > job.CommittedBatchIndex`: The batch is executed inside an atomic transaction. Upon commit, `job.CommittedBatchIndex` and `job.Summary` are updated.

6. **Context Cancellation vs Crash Recovery**:
   - **Cancellation/Timeout**: When `ctx.Done()` fires during streaming or record execution:
     The engine aborts the active batch transaction (triggering `tx.Rollback()`).
     To update `sync_jobs` status to `INTERRUPTED`, the engine CANNOT use `ctx` because it is already cancelled; it must use an uncancelled detached context with a short timeout (`context.WithTimeout(context.Background(), 5*time.Second)`).
   - **Abrupt Process Crash**: When the process dies abruptly (SIGKILL, hardware outage), transactions uncommitted at the moment of death are automatically rolled back by SQLite WAL recovery. On the next application startup, `RecoverOnStartup(ctx)` invokes `s.storage.RecoverInterruptedJobs(ctx)`, transitioning stale `RUNNING` or `PENDING` jobs to `INTERRUPTED`.

7. **Completion Guarantees (DISPATCH & ORIGINAL_REQUEST)**:
   The job transitions to `COMPLETED` and sets `completed_at` strictly when `batchStream` closes (`ok == false` from channel receive) without any abortive error or context cancellation.

8. **Dead-Letter Isolation Hardening (Invariant D)**:
   Enhance `isInvalidRecord` to check `record.Timestamp.IsZero()` in addition to empty SKU, negative price, and invalid status. Ensure `RecordDeadLetterTx` errors are checked and returned so any storage failure triggers a clean rollback rather than silent data omission.

---

## 3. Caveats

1. **Batch Stream Closure Requirement**:
   `batchStream <-chan domain.Batch` must be closed (`close(ch)`) by the sender when generation completes to signal EOF. If a stream hangs open indefinitely without sending or closing, `RunJob` will wait until the supplied `ctx` is cancelled.
2. **Channel Buffering**:
   The engine works identically with unbuffered and buffered channels. Batches are consumed sequentially one at a time.
3. **Out-of-Order Batches**:
   In standard streaming, `batch.BatchIndex` increments monotonically (0, 1, 2, ...). If batches arrive out of order, checkpoint filtering checks `batch.BatchIndex <= job.CommittedBatchIndex`. The generator in Milestone 3 should emit batches in monotonically increasing index order.
4. **SQLite Single-Writer Concurrency**:
   `SyncEngine` uses `e.mu.Lock()` per batch execution to serialize writes. This prevents SQLite lock contention (`SQLITE_BUSY`) when concurrent goroutines or tests invoke engine methods.

---

## 4. Conclusion & Concrete Go Blueprints

### Blueprint 1: Expanded `Storage` Interface in `pkg/sync/engine.go`

```go
// Storage defines the storage contract required by the synchronization engine.
type Storage interface {
	DB() *sql.DB
	Close() error
	BeginTx(ctx context.Context) (*sql.Tx, error)

	// Products
	GetProduct(ctx context.Context, sku string) (*domain.Product, error)
	GetProductTx(ctx context.Context, tx *sql.Tx, sku string) (*domain.Product, error)
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

### Blueprint 2: Core Record Processing Helper (`executeRecordsTx`)

Refactor the inner record processing from `ExecuteBatch` into a reusable helper operating on an existing transaction `*sql.Tx`:

```go
// executeRecordsTx processes product records and dead-letters inside an existing transaction tx.
func (e *SyncEngine) executeRecordsTx(ctx context.Context, tx *sql.Tx, batch domain.Batch) (domain.ChangeSummary, error) {
	summary := domain.ChangeSummary{}
	batchState := make(map[string]*domain.Product)

	for i, record := range batch.Records {
		if err := ctx.Err(); err != nil {
			return domain.ChangeSummary{}, err
		}

		sku := strings.TrimSpace(record.SKU)
		recordTimestamp := record.Timestamp.UTC()

		// Invariant D: Dead-Letter Isolation
		if isInvalidRecord(sku, record.Price, record.Status, record.Timestamp) {
			summary.DeadLetter++
			issueID := fmt.Sprintf("dl-%s-%d-%d-%d", batch.JobID, batch.BatchIndex, i, e.now().UnixNano())
			if batch.JobID == "" {
				issueID = fmt.Sprintf("dl-%d-%d", i, e.now().UnixNano())
			}
			dlRecord := &domain.DeadLetterRecord{
				IssueID:    issueID,
				JobID:      batch.JobID,
				BatchIndex: batch.BatchIndex,
				SKU:        sku,
				RawPayload: fmt.Sprintf("SKU:%s, Name:%s, Price:%f, Status:%s, Timestamp:%s",
					record.SKU, record.Name, record.Price, record.Status, record.Timestamp.Format(time.RFC3339Nano)),
				Reason:    getInvalidReason(sku, record.Price, record.Status, record.Timestamp),
				CreatedAt: e.now(),
			}
			if err := e.storage.RecordDeadLetterTx(ctx, tx, dlRecord); err != nil {
				return domain.ChangeSummary{}, fmt.Errorf("failed to record dead letter issue: %w", err)
			}
			continue
		}

		// Step 1: Resolve existing product state
		existingProduct, foundInBatch := batchState[sku]
		if !foundInBatch {
			var err error
			existingProduct, err = e.storage.GetProductTx(ctx, tx, sku)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
					existingProduct = nil
				} else {
					return domain.ChangeSummary{}, fmt.Errorf("failed to query product %s at index %d: %w", sku, i, err)
				}
			}
			batchState[sku] = existingProduct
		}

		// Step 2: New insertion
		if existingProduct == nil {
			newProduct := &domain.Product{
				SKU:               sku,
				Name:              record.Name,
				Price:             record.Price,
				Status:            record.Status,
				SupplierTimestamp: recordTimestamp,
				UpdatedAt:         e.now(),
			}
			if err := e.storage.UpsertProductTx(ctx, tx, newProduct); err != nil {
				return domain.ChangeSummary{}, fmt.Errorf("failed to insert product %s: %w", sku, err)
			}
			batchState[sku] = newProduct
			summary.Added++
			continue
		}

		// Step 3: Evaluate Invariants A, C, B
		if isIdenticalProduct(existingProduct, record, recordTimestamp) {
			summary.Skipped++
			continue
		}
		if existingProduct.Status == domain.StatusWithdrawn && record.Status == domain.StatusActive {
			summary.Skipped++
			continue
		}
		if recordTimestamp.Before(existingProduct.SupplierTimestamp) || recordTimestamp.Equal(existingProduct.SupplierTimestamp) {
			summary.Skipped++
			continue
		}

		// Step 4: Valid update
		wasWithdrawn := existingProduct.Status == domain.StatusWithdrawn
		nowWithdrawn := record.Status == domain.StatusWithdrawn

		updatedProduct := &domain.Product{
			SKU:               sku,
			Name:              record.Name,
			Price:             record.Price,
			Status:            record.Status,
			SupplierTimestamp: recordTimestamp,
			UpdatedAt:         e.now(),
		}
		if err := e.storage.UpsertProductTx(ctx, tx, updatedProduct); err != nil {
			return domain.ChangeSummary{}, fmt.Errorf("failed to update product %s: %w", sku, err)
		}
		batchState[sku] = updatedProduct

		if !wasWithdrawn && nowWithdrawn {
			summary.Withdrawn++
		} else {
			summary.Updated++
		}
	}

	return summary, nil
}
```

### Blueprint 3: Atomic Batch Committer (`processBatchTx`)

Guarantees Invariant E: records, dead-letters, checkpoint, and job progress all commit in a single transaction:

```go
// processBatchTx executes a batch within an atomic database transaction including checkpoint and progress.
func (e *SyncEngine) processBatchTx(ctx context.Context, job *domain.SyncJob, batch domain.Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	tx, err := e.storage.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 1. Process records and dead-letters
	summary, err := e.executeRecordsTx(ctx, tx, batch)
	if err != nil {
		return err
	}

	// 2. Insert batch checkpoint
	checkpoint := &domain.BatchCheckpoint{
		JobID:       job.JobID,
		BatchIndex:  batch.BatchIndex,
		ItemsCount:  len(batch.Records),
		Summary:     summary,
		CommittedAt: e.now(),
	}
	if err := e.storage.RecordBatchCheckpointTx(ctx, tx, checkpoint); err != nil {
		return fmt.Errorf("failed to record batch checkpoint %s[%d]: %w", job.JobID, batch.BatchIndex, err)
	}

	// 3. Atomically update job cumulative progress
	if err := e.storage.UpdateJobProgressTx(ctx, tx, job.JobID, batch.BatchIndex, summary); err != nil {
		return fmt.Errorf("failed to update job progress for %s[%d]: %w", job.JobID, batch.BatchIndex, err)
	}

	// 4. Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch %s[%d]: %w", job.JobID, batch.BatchIndex, err)
	}

	// 5. Update in-memory job state only after successful commit
	job.CommittedBatchIndex = batch.BatchIndex
	job.Summary.Add(summary)
	job.UpdatedAt = e.now()

	return nil
}
```

### Blueprint 4: Unified Streaming Loop (`executeJob`)

Handles channel streaming, checkpoint skipping, context cancellation, and terminal state transitions:

```go
func (e *SyncEngine) executeJob(ctx context.Context, job *domain.SyncJob, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
	for {
		select {
		case <-ctx.Done():
			e.markInterrupted(job)
			return job, ctx.Err()

		case batch, ok := <-batchStream:
			if !ok {
				// Channel closed -> all batches finished!
				goto streamCompleted
			}

			// Ensure batch associates with the active job ID
			batch.JobID = job.JobID

			// Checkpoint Check: Skip batches that have already been committed in previous runs
			if batch.BatchIndex <= job.CommittedBatchIndex {
				continue
			}

			// Process and commit batch atomically
			if err := e.processBatchTx(ctx, job, batch); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					e.markInterrupted(job)
					return job, ctx.Err()
				}
				e.markFailed(job)
				return job, err
			}
		}
	}

streamCompleted:
	now := e.now()
	job.Status = domain.JobStatusCompleted
	job.CompletedAt = &now
	job.UpdatedAt = now
	if err := e.storage.UpdateJobStatus(ctx, job.JobID, domain.JobStatusCompleted); err != nil {
		return job, fmt.Errorf("failed to update job status to completed: %w", err)
	}
	return job, nil
}

// markInterrupted transitions job to INTERRUPTED using a background context with timeout.
func (e *SyncEngine) markInterrupted(job *domain.SyncJob) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = e.storage.UpdateJobStatus(cleanupCtx, job.JobID, domain.JobStatusInterrupted)
	job.Status = domain.JobStatusInterrupted
	job.UpdatedAt = e.now()
}

// markFailed transitions job to FAILED using a background context with timeout.
func (e *SyncEngine) markFailed(job *domain.SyncJob) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = e.storage.UpdateJobStatus(cleanupCtx, job.JobID, domain.JobStatusFailed)
	job.Status = domain.JobStatusFailed
	job.UpdatedAt = e.now()
}
```

### Blueprint 5: `RunJob` and `ResumeJob` Implementations

```go
// RunJob processes all batches from a batch reader/generator, handling checkpoints and crash resumability.
func (e *SyncEngine) RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
	if strings.TrimSpace(jobID) == "" {
		return nil, errors.New("job ID cannot be empty")
	}
	if batchStream == nil {
		return nil, errors.New("batch stream cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.storage == nil {
		return nil, errors.New("sync engine storage is nil")
	}

	job, err := e.storage.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Create new job
			now := e.now()
			job = &domain.SyncJob{
				JobID:               jobID,
				Status:              domain.JobStatusRunning,
				TotalBatches:        0,
				CommittedBatchIndex: -1,
				Summary:             domain.ChangeSummary{},
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			if err := e.storage.CreateJob(ctx, job); err != nil {
				return nil, fmt.Errorf("failed to create sync job %s: %w", jobID, err)
			}
		} else {
			return nil, fmt.Errorf("failed to lookup job %s: %w", jobID, err)
		}
	} else {
		// Existing job validation
		if job.Status == domain.JobStatusCompleted {
			return nil, fmt.Errorf("job %s is already completed", jobID)
		}
		if job.Status != domain.JobStatusRunning {
			if err := e.storage.UpdateJobStatus(ctx, jobID, domain.JobStatusRunning); err != nil {
				return nil, fmt.Errorf("failed to transition job %s to RUNNING: %w", jobID, err)
			}
			job.Status = domain.JobStatusRunning
		}
	}

	return e.executeJob(ctx, job, batchStream)
}

// ResumeJob restarts an interrupted job from its last committed batch checkpoint.
func (e *SyncEngine) ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
	if strings.TrimSpace(jobID) == "" {
		return nil, errors.New("job ID cannot be empty")
	}
	if batchStream == nil {
		return nil, errors.New("batch stream cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.storage == nil {
		return nil, errors.New("sync engine storage is nil")
	}

	job, err := e.storage.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("cannot resume non-existent job: %s", jobID)
		}
		return nil, fmt.Errorf("failed to fetch job %s for resume: %w", jobID, err)
	}

	if job.Status == domain.JobStatusCompleted {
		return nil, fmt.Errorf("job %s is already completed", jobID)
	}

	// Transition to RUNNING
	if job.Status != domain.JobStatusRunning {
		if err := e.storage.UpdateJobStatus(ctx, jobID, domain.JobStatusRunning); err != nil {
			return nil, fmt.Errorf("failed to transition job %s to RUNNING: %w", jobID, err)
		}
		job.Status = domain.JobStatusRunning
	}

	return e.executeJob(ctx, job, batchStream)
}
```

### Blueprint 6: Enhanced Invariant D Validation

```go
func isInvalidRecord(sku string, price float64, status domain.ProductStatus, timestamp time.Time) bool {
	if strings.TrimSpace(sku) == "" {
		return true
	}
	if price < 0 {
		return true
	}
	if !status.IsValid() {
		return true
	}
	if timestamp.IsZero() {
		return true
	}
	return false
}

func getInvalidReason(sku string, price float64, status domain.ProductStatus, timestamp time.Time) string {
	var reasons []string
	if strings.TrimSpace(sku) == "" {
		reasons = append(reasons, "empty SKU")
	}
	if price < 0 {
		reasons = append(reasons, fmt.Sprintf("negative price: %f", price))
	}
	if !status.IsValid() {
		reasons = append(reasons, fmt.Sprintf("invalid status: %s", status))
	}
	if timestamp.IsZero() {
		reasons = append(reasons, "zero timestamp")
	}
	return strings.Join(reasons, "; ")
}
```

---

## 5. Verification Method

### How to Independently Verify Milestone 2

1. **Unit & Race Test Execution**:
   Run all tests with race detection and multi-iteration coverage:
   ```bash
   go test -v -race -count=5 ./pkg/sync/...
   go test -v -race -coverprofile=coverage.out ./...
   go tool cover -func=coverage.out
   ```
   **Expected**: 0 data races, all tests pass, $\ge 90\%$ statement coverage.

2. **Milestone 2 Implementer Test Matrix**:
   The implementer should add the following dedicated tests in `pkg/sync/engine_m2_test.go`:
   - `TestEngine_RunJob_CompleteStreaming`: Verifies end-to-end streaming across $N$ batches, verifying all checkpoints in `batch_checkpoints`, cumulative progress in `sync_jobs`, and catalog contents.
   - `TestEngine_RunJob_ContextCancellation_AtomicRollback`: Cancels `ctx` during batch $K$, verifying that batches $< K$ remain committed, batch $K$ changes (products, dead letters, checkpoints) are rolled back, and job transitions to `INTERRUPTED`.
   - `TestEngine_ResumeJob_SkipCommittedBatches`: Resumes the interrupted job with a complete stream, verifying that batches $\le \text{committed\_batch\_index}$ are skipped without DB writes, batches $> \text{committed\_batch\_index}$ are processed, and job reaches `COMPLETED` with correct final metrics.
   - `TestEngine_ResumeJob_Validation`: Tests resuming non-existent jobs (`ErrNotFound`), already completed jobs (error), nil streams, and empty job IDs.
   - `TestEngine_DeadLetter_ComprehensiveQuarantine`: Ingests a batch containing empty SKU, negative price, invalid status, zero timestamp, and valid items. Verifies valid items are inserted, invalid items are recorded in `dead_letter_issues`, and the batch commits successfully.
   - `TestEngine_StartupCrashRecovery`: Sets up simulated stale jobs in `RUNNING` and `PENDING`, calls `RecoverOnStartup`, and verifies they are marked `INTERRUPTED` and can be resumed.

3. **Invalidation Conditions**:
   - If a batch failure leaves partial product inserts or dangling checkpoints: Invariant E is violated.
   - If resuming a job re-inserts or re-evaluates batches $\le \text{committed\_batch\_index}$: Checkpoint tracking is broken.
   - If an interrupted job is marked `COMPLETED`: Invariant R2 is violated.
   - If corrupt records cause the entire batch transaction to fail: Invariant D is violated.
