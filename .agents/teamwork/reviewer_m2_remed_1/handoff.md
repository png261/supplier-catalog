# Milestone 2 Review & Adversarial Challenge Report

**Verdict**: **APPROVE**

---

## 1. Observation

### Observation 1.1: Atomic Batch Transactions (Invariant E) in `pkg/sync/engine.go`
In `pkg/sync/engine.go` (lines 251–310):
```go
func (e *SyncEngine) processBatchTx(ctx context.Context, job *domain.SyncJob, batch domain.Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if e.storage == nil {
		return ErrNilStorage
	}

	tx, err := e.storage.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 1. Process records and dead letters inside transaction
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
All four operations—product upserts, dead-letter records, checkpoint creation, and cumulative progress counter updates—execute within the same `*sql.Tx` and commit atomically. If any error or context cancellation occurs before `tx.Commit()`, `defer tx.Rollback()` aborts all uncommitted modifications. In-memory counters are updated strictly after `tx.Commit()` succeeds.

### Observation 1.2: Checkpoint Check and Crash Resumption in `executeJob`
In `pkg/sync/engine.go` (lines 326–350):
```go
			// Ensure batch associates with the active job ID
			batch.JobID = job.JobID

			// Checkpoint Check: Skip batches that have already been committed in previous runs
			if batch.BatchIndex <= job.CommittedBatchIndex {
				continue
			}

			// Gap detection: Next uncommitted batch MUST be exactly job.CommittedBatchIndex + 1
			if batch.BatchIndex > job.CommittedBatchIndex+1 {
				err := fmt.Errorf("gap detected in batch sequence: received batch index %d, expected %d", batch.BatchIndex, job.CommittedBatchIndex+1)
				e.markInterrupted(job)
				return job, err
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
```
In `ResumeJob` (lines 467–516), the engine retrieves `job` from SQLite storage (`e.storage.GetJob`), recovering `CommittedBatchIndex` and cumulative `Summary`. In `executeJob`, incoming batches with `BatchIndex <= job.CommittedBatchIndex` are skipped with zero database side-effects and zero counter mutations. In addition, sequence gaps (`batch.BatchIndex > job.CommittedBatchIndex+1`) are immediately detected, transitioning the job to `INTERRUPTED`.

### Observation 1.3: Startup Crash Recovery & Safe Cancellation State Handlers
In `pkg/sync/engine.go` (lines 379–399):
```go
func (e *SyncEngine) markInterrupted(job *domain.SyncJob) {
	if e.storage != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.storage.UpdateJobStatus(cleanupCtx, job.JobID, domain.JobStatusInterrupted)
	}
	job.Status = domain.JobStatusInterrupted
	job.UpdatedAt = e.now()
}
```
`markInterrupted` and `markFailed` execute against an isolated background context (`context.Background()` with 5-second timeout), ensuring status persistence even when the parent request context was canceled. In `RecoverOnStartup` (lines 519–530), any leftover `RUNNING` or `PENDING` jobs in SQLite are transitioned to `INTERRUPTED`.

### Observation 1.4: Multi-Iteration Concurrency & Race Detector Command
Command:
```bash
go test -v -race -count=5 ./...
```
Output:
```
PASS
ok  	coderpush/pkg/domain	1.018s
ok  	coderpush/pkg/storage	4.200s
ok  	coderpush/pkg/sync	58.364s
```
All unit, adversarial, stress, and race tests passed cleanly across 5 iterations with 0 data races.

### Observation 1.5: Project Statement Coverage
Command:
```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```
Output:
- `coderpush/pkg/domain`: 100.0% of statements
- `coderpush/pkg/storage`: 90.2% of statements
- `coderpush/pkg/sync`: 94.7% of statements
- Total across project: 93.1% of statements (exceeds the 90% project threshold).

### Observation 1.6: Build and Static Analysis
Commands:
- `go build ./...`: Succeeded with exit code 0.
- `go vet ./...`: Succeeded with exit code 0, 0 warnings.

### Observation 1.7: Integrity Check Verification
- Scanned `pkg/sync/engine.go`, `pkg/storage/sqlite.go`, and test fixtures for hardcoded outputs, fake branches, or test shortcuts.
- No `SKU-` prefixes or test-specific logic branches exist in source code.
- Calculations (`Added`, `Updated`, `Withdrawn`, `Skipped`, `DeadLetter`) are derived dynamically from database comparisons and schema constraints.
- No shortcuts or facade implementations detected.

---

## 2. Logic Chain

1. **Atomic Batch Transactions (Invariant E)**:
   - Observation 1.1 demonstrates that `processBatchTx` wraps record processing, dead-letter storage, batch checkpoint audit creation, and cumulative progress counter updates in a single `*sql.Tx`.
   - SQLite WAL mode ensures full ACID transaction properties (`_txlock=immediate` and connection limit 1 serialize writes).
   - Any crash, cancellation, or error triggers `tx.Rollback()`, leaving prior committed batches untouched and uncommitted mutations fully rolled back. This directly verifies R2 and Acceptance Criterion AC1.

2. **Checkpoint Persistence & Non-Drift Resumption**:
   - Observations 1.1 and 1.2 show that `batch_checkpoints` records immutable per-batch audit entries with primary key `(job_id, batch_index)`.
   - `sync_jobs` persists `committed_batch_index` and cumulative counters atomically with each batch.
   - When `ResumeJob` or `RunJob` restarts, it queries `sync_jobs` to initialize `job.CommittedBatchIndex`.
   - In `executeJob`, batches `b <= job.CommittedBatchIndex` are skipped immediately, preventing duplicate work, duplicate counters, and catalog drift. This directly satisfies R2 and Acceptance Criterion AC2.

3. **Crash Recovery & Lifecycle State Transitions**:
   - Observations 1.2 and 1.3 show that premature stream closure, sequence gaps, or context cancellations transition active jobs to `INTERRUPTED` without marking them complete.
   - Jobs only transition to `COMPLETED` when all batches are verified (`job.CommittedBatchIndex + 1 == job.TotalBatches`).
   - `RecoverOnStartup` recovers stale in-flight jobs after hard process terminations. This satisfies Acceptance Criterion AC3.

4. **Code Quality, Concurrency, and Coverage Compliance**:
   - Observation 1.4 confirms zero race conditions across repeated concurrent executions (`-count=5 -race`).
   - Observation 1.5 confirms total statement coverage is 93.1% (with `pkg/sync` reaching 94.7%), exceeding the >= 90% requirement.
   - Observation 1.6 confirms clean compilation and zero linter warnings.
   - Observation 1.7 confirms zero integrity violations.

---

## 3. Caveats

- SQLite in WAL mode requires a filesystem supporting posix locks and shared memory (shm/wal files). In containerized deployments (Milestone 5), named persistent volumes must be mounted to preserve the WAL files across container restarts.
- No other caveats.

---

## 4. Conclusion

**Verdict: APPROVE**

Milestone 2 implementation and remediation in `pkg/sync/engine.go`, `pkg/storage/sqlite.go`, and accompanying test suites completely satisfy all functional, architectural, and security invariants:
1. `RunJob` and `ResumeJob` enforce atomic batch transaction semantics (Invariant E).
2. Checkpoint records are persisted transactionally in SQLite after every committed batch.
3. Resumption accurately starts from $last\_committed\_batch + 1$, safely skipping already committed batches without catalog drift.
4. Concurrency safety, race-free execution, and statement coverage (93.1%) are rigorously verified.

---

## 5. Verification Method

To independently verify the implementation and test results:

1. **Compile and Lint Check**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go build ./...
   go vet ./...
   ```
   *Expected*: Exit code 0, 0 errors, 0 warnings.

2. **Concurrency & Race Detection (5 iterations)**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go test -v -race -count=5 ./...
   ```
   *Expected*: 100% PASS across all packages (`pkg/domain`, `pkg/storage`, `pkg/sync`) with zero data races.

3. **Statement Coverage Gate**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
   ```
   *Expected*: Total statement coverage >= 90% (observed: 93.1%).

4. **Adversarial Resumability & Chaos Harness**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go test -v -run "TestChallenger|TestAC" ./pkg/sync
   ```
   *Expected*: All multi-crash chaos, replay attack, and lifecycle FSM stress tests PASS.

### Invalidation Conditions:
The approval would be invalidated if:
- Any uncommitted batch modification persists in `products` or `dead_letter_issues` after mid-batch cancellation.
- Any batch with index $\le committed\_batch\_index$ is re-executed during resumption, altering catalog checksums or cumulative change counts.
- `go test -race -count=5 ./...` reports a data race or test failure.
- Statement coverage drops below 90%.
