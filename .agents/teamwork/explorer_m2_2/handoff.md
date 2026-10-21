# Milestone 2 Exploration Report: Startup Crash Recovery & Dead-Letter Queue Isolation

**Author**: `teamwork_preview_explorer` (Worker 2)  
**Working Directory**: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2`  
**Milestone**: Milestone 2 (Resumability, Batch Checkpointing & Dead-Letter Isolation)  
**Authoritative References**: `ORIGINAL_REQUEST.md`, `PROJECT.md`, `pkg/domain`, `pkg/storage`, `pkg/sync`

---

## 1. Observation

Direct code observations from the current Milestone 1 codebase:

### 1.1 Existing Startup Recovery Implementation
- In `/Users/png/01. Project/coderpush/pkg/storage/sqlite.go`, lines 438–473:
```go
// RecoverInterruptedJobs transitions stale RUNNING or PENDING jobs to INTERRUPTED on application startup.
func (s *SQLiteStorage) RecoverInterruptedJobs(ctx context.Context) ([]string, error) {
	selectQuery := `SELECT job_id FROM sync_jobs WHERE status IN ('RUNNING', 'PENDING');`
	rows, err := s.db.QueryContext(ctx, selectQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale jobs: %w", err)
	}
	defer rows.Close()

	var recoveredIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			recoveredIDs = append(recoveredIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating stale jobs: %w", err)
	}

	if len(recoveredIDs) == 0 {
		return nil, nil
	}

	updateQuery := `
	UPDATE sync_jobs SET status = 'INTERRUPTED', updated_at = ?
	WHERE status IN ('RUNNING', 'PENDING');
	`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, updateQuery, now)
	if err != nil {
		return nil, fmt.Errorf("failed to mark jobs interrupted: %w", err)
	}

	return recoveredIDs, nil
}
```
- In `/Users/png/01. Project/coderpush/pkg/sync/engine.go`, lines 256–262:
```go
// RecoverOnStartup checks for running jobs and transitions them to INTERRUPTED.
func (e *SyncEngine) RecoverOnStartup(ctx context.Context) ([]string, error) {
	if e.storage == nil {
		return nil, errors.New("storage is nil")
	}
	return e.storage.RecoverInterruptedJobs(ctx)
}
```
- In `/Users/png/01. Project/coderpush/pkg/storage/sqlite.go`, lines 315–323:
```go
// GetActiveJob returns the currently RUNNING or PENDING job, or ErrNotFound if idle.
func (s *SQLiteStorage) GetActiveJob(ctx context.Context) (*domain.SyncJob, error) {
	query := `
	SELECT job_id, status, total_batches, committed_batch_index,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       created_at, updated_at, completed_at
	FROM sync_jobs WHERE status IN ('RUNNING', 'PENDING')
	ORDER BY created_at DESC LIMIT 1;
	`
...
```

### 1.2 Existing Dead-Letter Queue & Validation Implementation
- In `/Users/png/01. Project/coderpush/pkg/sync/engine.go`, lines 121–141:
```go
		// --- Validation & Invariant D: Dead-Letter Isolation (Forward Compatibility) ---
		if isInvalidRecord(sku, record.Price, record.Status) {
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
				Reason:    getInvalidReason(sku, record.Price, record.Status),
				CreatedAt: e.now(),
			}
			// Attempt to persist dead letter if storage supports it
			_ = e.storage.RecordDeadLetterTx(ctx, tx, dlRecord)
			continue
		}
```
- In `/Users/png/01. Project/coderpush/pkg/sync/engine.go`, lines 290–315:
```go
func isInvalidRecord(sku string, price float64, status domain.ProductStatus) bool {
	if sku == "" {
		return true
	}
	if price < 0 {
		return true
	}
	if status != domain.StatusActive && status != domain.StatusWithdrawn {
		return true
	}
	return false
}

func getInvalidReason(sku string, price float64, status domain.ProductStatus) string {
	var reasons []string
	if sku == "" {
		reasons = append(reasons, "empty SKU")
	}
	if price < 0 {
		reasons = append(reasons, fmt.Sprintf("negative price: %f", price))
	}
	if status != domain.StatusActive && status != domain.StatusWithdrawn {
		reasons = append(reasons, fmt.Sprintf("invalid status: %s", status))
	}
	return strings.Join(reasons, "; ")
}
```
- In `/Users/png/01. Project/coderpush/pkg/domain/models.go`, lines 53–68:
```go
// Validate enforces Invariant D: verifies SKU non-empty, price non-negative, valid status, non-zero timestamp.
func (u *SupplierUpdate) Validate() error {
	if strings.TrimSpace(u.SKU) == "" {
		return ErrEmptySKU
	}
	if u.Price < 0 {
		return ErrNegativePrice
	}
	if !u.Status.IsValid() {
		return ErrInvalidStatus
	}
	if u.Timestamp.IsZero() {
		return ErrZeroTimestamp
	}
	return nil
}
```
- In `/Users/png/01. Project/coderpush/pkg/storage/sqlite.go`, lines 150–160 (Schema Definition):
```sql
	CREATE TABLE IF NOT EXISTS dead_letter_issues (
		issue_id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL,
		batch_index INTEGER NOT NULL,
		sku TEXT,
		raw_payload TEXT NOT NULL,
		reason TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_issues_job_id ON dead_letter_issues(job_id, batch_index);
```
- In `/Users/png/01. Project/coderpush/pkg/storage/sqlite.go`, lines 475–494:
```go
// RecordDeadLetterTx writes an isolated corrupt/invalid record to dead_letter_issues inside the batch transaction.
func (s *SQLiteStorage) RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error {
	query := `
	INSERT INTO dead_letter_issues (issue_id, job_id, batch_index, sku, raw_payload, reason, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	_, err := tx.ExecContext(ctx, query,
		issue.IssueID,
		issue.JobID,
		issue.BatchIndex,
		issue.SKU,
		issue.RawPayload,
		issue.Reason,
		issue.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to record dead letter issue %s: %w", issue.IssueID, err)
	}
	return nil
}
```
- In `/Users/png/01. Project/coderpush/pkg/storage/sqlite.go`, lines 496–554:
`ListDeadLetterRecords` queries paginated records by `job_id` or across all jobs if `jobID == ""`.

---

## 2. Logic Chain

### 2.1 Startup Crash Recovery Logic Chain

1. **Premise**: When an application process is killed (SIGKILL, power failure, OOM) during catalog synchronization:
   - In SQLite WAL mode, uncommitted batch transactions are automatically rolled back upon next open (`sqlite.Open()` + `PingContext()`).
   - However, the `sync_jobs` row was previously set to `RUNNING` (or `PENDING`) at job creation. The crash prevented any exit handler from updating it.
   - If left in `RUNNING`/`PENDING`, subsequent startup will incorrectly report an active job running (`s.GetActiveJob()`), blocking new sync runs or presenting false progress to staff.
2. **Transition Requirement**:
   - `RecoverOnStartup(ctx)` must be called during server/CLI bootstrap.
   - It invokes `s.RecoverInterruptedJobs(ctx)`.
   - The query transitions all jobs where `status IN ('RUNNING', 'PENDING')` to `status = 'INTERRUPTED'` and updates `updated_at = now`.
3. **Transaction Atomicity in SQLite**:
   - In `pkg/storage/sqlite.go:440-470`, the select and update statements are currently executed on `s.db` without an explicit transaction wrapper.
   - Wrapping the query and update within an explicit `tx, err := s.BeginTx(ctx)` guarantees that the IDs returned by `SELECT job_id` match exactly the rows transitioned by `UPDATE`, eliminating interleaving hazards.
4. **Audit and Structured Logging**:
   - When jobs are recovered, returning the list of recovered IDs allows the caller (`SyncEngine` and `cmd/server`) to log structured audit events detailing:
     `[RECOVERY] Recovered interrupted job {jobID}: previous status {status}, last committed batch {committedBatchIndex}/{totalBatches}`.
5. **Guaranteeing an Interrupted Job is NEVER Reported as COMPLETED**:
   - `ORIGINAL_REQUEST.md:21,48` specifies: *"An interrupted or partially completed update must never be reported as complete."* and *"Interrupted jobs are marked INTERRUPTED upon restart and only transition to COMPLETED when all batches finish."*
   - Proof of guarantee:
     a. **Storage State**: `sync_jobs.status` is explicitly set to `INTERRUPTED`, and `completed_at` remains `NULL`.
     b. **Read-Path Isolation**: `GetJob(jobID)` returns `status = "INTERRUPTED"`, `CompletedAt = nil`. `GetActiveJob()` filters exclusively on `status IN ('RUNNING', 'PENDING')`, returning `ErrNotFound`.
     c. **Engine-Path Gating**: In `RunJob` and `ResumeJob`, the transition to `COMPLETED` (`UpdateJobStatus(ctx, jobID, JobStatusCompleted)`) is strictly guarded: it is invoked ONLY after the batch stream has terminated without error AND all batches ($0 \dots \text{totalBatches}-1$) have been committed. If a stream closes prematurely or context is cancelled, the job transitions to `INTERRUPTED`.
     d. **Storage Transition Defense**: In `UpdateJobStatus`, transition to `COMPLETED` can verify that the job is not in an illegal incomplete state.

### 2.2 Dead-Letter Queue Isolation (Invariant D) Logic Chain

1. **Defects in Baseline Record Validation**:
   - Currently, `isInvalidRecord(sku, price, status)` (line 290) only checks `sku == ""`, `price < 0`, and status validity.
   - **Gaps Identified**:
     a. **Floating-Point NaN**: `math.NaN() < 0` evaluates to `false` in IEEE 754 float comparison! A record with `Price: math.NaN()` bypasses `price < 0` and enters the catalog database as corrupt data.
     b. **Floating-Point Infinity**: `math.IsInf(price, 0)` is not checked. Positive infinity `+Inf` bypasses `price < 0`.
     c. **Zero Timestamp**: `record.Timestamp.IsZero()` is not checked. Ingesting `time.Time{}` corrupts Invariant B (Fast-forward timestamps).
     d. **Whitespace-Only SKU**: `record.SKU = "   "` is trimmed to `""` in local variable `sku`, but in `dlRecord.RawPayload` the raw invalid string should be preserved for staff inspection.
2. **Quarantine Without Halting Valid Records**:
   - In `pkg/sync/engine.go:121-141`, when an invalid record is identified:
     a. `summary.DeadLetter++` is incremented.
     b. A unique `domain.DeadLetterRecord` is created.
     c. It is persisted inside the atomic transaction via `e.storage.RecordDeadLetterTx(ctx, tx, dlRecord)`.
     d. Crucially, the loop executes `continue`, skipping the product insert/update step.
     e. Subsequent valid records in the batch are processed and committed normally.
     f. The transaction commits atomically: valid records are committed to `products`, quarantined records are committed to `dead_letter_issues`, and metrics are recorded in `batch_checkpoints` and `sync_jobs`.
3. **Error Propagation**:
   - In `engine.go:139`, the code currently uses `_ = e.storage.RecordDeadLetterTx(ctx, tx, dlRecord)`.
   - Under Invariant E (Atomic Batch Transactions), every operation inside the batch must be atomic. If writing to `dead_letter_issues` fails (e.g. disk full, constraint violation), ignoring the error risks losing audit trails. The error MUST be checked and propagated so the batch transaction rolls back safely.
4. **Batch Replay Idempotency for Dead Letters**:
   - If an uncommitted batch crashes, WAL rollback erases the dead-letter records. When the batch is retried, inserting the dead letters must succeed.
   - In `RecordDeadLetterTx`, using `ON CONFLICT(issue_id) DO UPDATE SET raw_payload = excluded.raw_payload, reason = excluded.reason, created_at = excluded.created_at` prevents `UNIQUE constraint failed` errors if a batch is replayed or re-evaluated.
5. **JSON Raw Payload Representation**:
   - Storing `RawPayload` as formatted JSON (`json.Marshal(record)`) rather than custom string formatting ensures that the embedded web dashboard and REST API (`/api/dead-letter`) can parse and inspect the quarantined payload cleanly.

---

## 3. Concrete Go Architecture Blueprints

### Blueprint 1: Comprehensive Record Validation (`pkg/sync/validator.go` or `engine.go`)

```go
package sync

import (
	"fmt"
	"math"
	"strings"
	"time"

	"coderpush/pkg/domain"
)

// ValidationResult represents the evaluation of Invariant D on an incoming update.
type ValidationResult struct {
	IsInvalid bool
	Reasons   []string
}

// ReasonString formats all validation failures into a human-readable semicolon-separated string.
func (v ValidationResult) ReasonString() string {
	return strings.Join(v.Reasons, "; ")
}

// ValidateSupplierRecord enforces Invariant D: detects corrupt or invalid product records.
// Covers: empty/whitespace SKUs, negative prices, NaN prices, infinite prices, invalid statuses, zero timestamps.
func ValidateSupplierRecord(r domain.SupplierUpdate) ValidationResult {
	var reasons []string

	// 1. SKU validation
	if strings.TrimSpace(r.SKU) == "" {
		reasons = append(reasons, "empty or whitespace SKU")
	}

	// 2. Price validation (IEEE 754 floating point checks)
	if math.IsNaN(r.Price) {
		reasons = append(reasons, "price is NaN")
	} else if math.IsInf(r.Price, 0) {
		reasons = append(reasons, "price is infinite")
	} else if r.Price < 0 {
		reasons = append(reasons, fmt.Sprintf("negative price: %f", r.Price))
	}

	// 3. Status validation
	if !r.Status.IsValid() {
		reasons = append(reasons, fmt.Sprintf("invalid status: %q (must be ACTIVE or WITHDRAWN)", r.Status))
	}

	// 4. Timestamp validation
	if r.Timestamp.IsZero() {
		reasons = append(reasons, "zero or missing supplier timestamp")
	}

	if len(reasons) > 0 {
		return ValidationResult{IsInvalid: true, Reasons: reasons}
	}
	return ValidationResult{IsInvalid: false, Reasons: nil}
}
```

### Blueprint 2: Enhanced Dead-Letter Handling in `ExecuteBatch` (`pkg/sync/engine.go`)

```go
// Inside ExecuteBatch loop:
for i, record := range batch.Records {
	if err := ctx.Err(); err != nil {
		return domain.ChangeSummary{}, err
	}

	// --- Invariant D: Dead-Letter Isolation ---
	valRes := ValidateSupplierRecord(record)
	if valRes.IsInvalid {
		summary.DeadLetter++

		// Deterministic, unique IssueID
		issueID := fmt.Sprintf("dl-%s-%d-%d", batch.JobID, batch.BatchIndex, i)
		if batch.JobID == "" {
			issueID = fmt.Sprintf("dl-standalone-%d-%d-%d", batch.BatchIndex, i, e.now().UnixNano())
		}

		// JSON-serialized raw payload for rich staff inspection
		rawBytes, jsonErr := json.Marshal(record)
		rawPayload := string(rawBytes)
		if jsonErr != nil {
			rawPayload = fmt.Sprintf("SKU:%s, Name:%s, Price:%f, Status:%s, Timestamp:%s",
				record.SKU, record.Name, record.Price, record.Status, record.Timestamp.Format(time.RFC3339Nano))
		}

		dlRecord := &domain.DeadLetterRecord{
			IssueID:    issueID,
			JobID:      batch.JobID,
			BatchIndex: batch.BatchIndex,
			SKU:        strings.TrimSpace(record.SKU),
			RawPayload: rawPayload,
			Reason:     valRes.ReasonString(),
			CreatedAt:  e.now(),
		}

		// Invariant E: Atomically write dead letter inside transaction
		if err := e.storage.RecordDeadLetterTx(ctx, tx, dlRecord); err != nil {
			return domain.ChangeSummary{}, fmt.Errorf("failed to record dead letter issue %s: %w", issueID, err)
		}

		// Non-halting guarantee: skip to next record without failing the batch
		continue
	}

	// ... Process valid records (Invariant A, B, C) ...
}
```

### Blueprint 3: Enhanced Storage Methods (`pkg/storage/sqlite.go`)

```go
// RecordDeadLetterTx writes an isolated corrupt/invalid record to dead_letter_issues inside the batch transaction.
// Uses ON CONFLICT to guarantee idempotent replay safety.
func (s *SQLiteStorage) RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error {
	query := `
	INSERT INTO dead_letter_issues (issue_id, job_id, batch_index, sku, raw_payload, reason, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(issue_id) DO UPDATE SET
		job_id = excluded.job_id,
		batch_index = excluded.batch_index,
		sku = excluded.sku,
		raw_payload = excluded.raw_payload,
		reason = excluded.reason,
		created_at = excluded.created_at;
	`
	_, err := tx.ExecContext(ctx, query,
		issue.IssueID,
		issue.JobID,
		issue.BatchIndex,
		issue.SKU,
		issue.RawPayload,
		issue.Reason,
		issue.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to record dead letter issue %s: %w", issue.IssueID, err)
	}
	return nil
}

// ListDeadLetterRecords queries paginated dead-letter records for a specific job (or all jobs if jobID is empty).
func (s *SQLiteStorage) ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error) {
	// Sanitize pagination bounds
	if limit <= 0 {
		limit = 50
	} else if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}

	var countQuery string
	var selectQuery string
	var countArgs []any
	var selectArgs []any

	if jobID != "" {
		countQuery = `SELECT count(*) FROM dead_letter_issues WHERE job_id = ?;`
		countArgs = []any{jobID}

		selectQuery = `
		SELECT issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
		FROM dead_letter_issues WHERE job_id = ?
		ORDER BY created_at DESC, issue_id DESC LIMIT ? OFFSET ?;
		`
		selectArgs = []any{jobID, limit, offset}
	} else {
		countQuery = `SELECT count(*) FROM dead_letter_issues;`
		selectQuery = `
		SELECT issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
		FROM dead_letter_issues
		ORDER BY created_at DESC, issue_id DESC LIMIT ? OFFSET ?;
		`
		selectArgs = []any{limit, offset}
	}

	var totalCount int
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("failed to count dead letter issues: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, selectQuery, selectArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query dead letter issues: %w", err)
	}
	defer rows.Close()

	// Initialize empty slice (serializes to [] in JSON instead of null)
	records := make([]domain.DeadLetterRecord, 0)
	for rows.Next() {
		var r domain.DeadLetterRecord
		var timeStr string
		var skuStr sql.NullString

		if err := rows.Scan(&r.IssueID, &r.JobID, &r.BatchIndex, &skuStr, &r.RawPayload, &r.Reason, &timeStr); err != nil {
			return nil, 0, fmt.Errorf("failed scanning dead letter row: %w", err)
		}
		if skuStr.Valid {
			r.SKU = skuStr.String
		}
		r.CreatedAt, _ = ParseTime(timeStr)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating dead letter rows: %w", err)
	}

	return records, totalCount, nil
}
```

### Blueprint 4: Transactional Startup Recovery (`pkg/storage/sqlite.go`)

```go
// RecoverInterruptedJobs transitions stale RUNNING or PENDING jobs to INTERRUPTED inside an atomic transaction.
func (s *SQLiteStorage) RecoverInterruptedJobs(ctx context.Context) ([]string, error) {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction for job recovery: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	selectQuery := `SELECT job_id FROM sync_jobs WHERE status IN ('RUNNING', 'PENDING') ORDER BY created_at ASC;`
	rows, err := tx.QueryContext(ctx, selectQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale jobs: %w", err)
	}
	defer rows.Close()

	var recoveredIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan stale job id: %w", err)
		}
		recoveredIDs = append(recoveredIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating stale jobs: %w", err)
	}
	rows.Close()

	if len(recoveredIDs) == 0 {
		return nil, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	updateQuery := `
	UPDATE sync_jobs SET status = 'INTERRUPTED', updated_at = ?
	WHERE status IN ('RUNNING', 'PENDING');
	`
	if _, err := tx.ExecContext(ctx, updateQuery, now); err != nil {
		return nil, fmt.Errorf("failed to transition stale jobs to INTERRUPTED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit job recovery: %w", err)
	}

	return recoveredIDs, nil
}
```

### Blueprint 5: Engine Startup Recovery with Audit Logging (`pkg/sync/engine.go`)

```go
// RecoverOnStartup scans for stale RUNNING/PENDING jobs left after process crash/interruption,
// transitions them to INTERRUPTED, and records structured recovery audit entries.
func (e *SyncEngine) RecoverOnStartup(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.storage == nil {
		return nil, errors.New("sync engine storage is nil")
	}

	recoveredIDs, err := e.storage.RecoverInterruptedJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to recover interrupted jobs: %w", err)
	}

	// Audit logging for operators
	for _, id := range recoveredIDs {
		job, getErr := e.storage.GetJob(ctx, id)
		if getErr == nil && job != nil {
			log.Printf("[CRASH_RECOVERY] Stale job %q transitioned to INTERRUPTED (committed_batch_index=%d, total_batches=%d, progress=%+v)",
				job.JobID, job.CommittedBatchIndex, job.TotalBatches, job.Summary)
		}
	}

	return recoveredIDs, nil
}
```

---

## 4. Caveats

1. **Process-Level Lock vs Multi-Process SQLite Access**:
   SQLite is configured with single-writer serialization (`db.SetMaxOpenConns(1)`). If two separate OS processes run against the exact same SQLite database file simultaneously, SQLite's file lock (`_txlock=immediate` and `busy_timeout=5000`) will block the second process. `RecoverOnStartup` should run during singleton application initialization before serving traffic.
2. **Crash Before First Checkpoint**:
   If an application crashes during Batch 0, `committed_batch_index` remains `-1`, and cumulative metrics remain 0. On startup, `RecoverOnStartup` transitions the job to `INTERRUPTED`. Resuming this job will correctly start processing from Batch 0.
3. **Sub-Nanosecond Timestamp Parsing**:
   Timestamps stored in SQLite `created_at` and `supplier_timestamp` use `RFC3339Nano`. `ParseTime` must parse both standard `RFC3339` and `RFC3339Nano` to maintain backward compatibility.

---

## 5. Conclusion

1. **Crash Recovery Guarantee**:
   The combination of SQLite WAL automatic journal rollback upon re-open and `engine.RecoverOnStartup(ctx)` guarantees that:
   - Any half-written batch $k$ is completely erased.
   - All previously committed batches $0 \dots k-1$ remain durably intact with checkpoints and progress counters.
   - Stale jobs in `RUNNING` or `PENDING` transition immediately to `INTERRUPTED`.
   - **An interrupted job is NEVER reported as `COMPLETED`.** Its status is `INTERRUPTED` and `completed_at` is `nil`.
2. **Dead-Letter Queue Isolation (Invariant D)**:
   - Valid records are never failed, blocked, or skipped due to the presence of corrupt records in the same batch.
   - Validation covers all 6 corruption categories: negative prices, empty/whitespace SKUs, NaN prices, infinite prices, invalid statuses, and zero timestamps.
   - Quarantined records are written inside the batch transaction with deterministic IssueIDs and `ON CONFLICT` update rules for idempotent batch replays.

---

## 6. Verification Method

To verify these designs independently:

1. **Run Current Test Suite**:
   ```bash
   go test -v -race ./...
   ```
   *Expected*: Passes with 0 data races.

2. **Verify Storage Dead-Letter & Recovery**:
   ```bash
   go test -v ./pkg/storage -run="TestStorage_JobLifecycleAndRecovery|TestStorage_DeadLetterQueue"
   ```

3. **Verify Sync Engine Dead-Letter Isolation**:
   ```bash
   go test -v ./pkg/sync -run="TestEngine_DeadLetter_InvalidRecords|TestEngine_Milestone2Stubs"
   ```

4. **Verify Extended Milestone 2 Test Suite (spec_miner_m2_1 specifications)**:
   When `worker_m2` implements these designs, run:
   ```bash
   go test -v -race -count=5 ./...
   ```
   *Expected*: $\ge 90\%$ statement and branch coverage with zero data races.
