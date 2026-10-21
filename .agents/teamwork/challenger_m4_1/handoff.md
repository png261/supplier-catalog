# Challenger M4.1 Handoff Report: REST Control Plane & DLQ Adversarial Stress Test

## Verdict: CONFIRM_CORRECTNESS_FAILED

---

## 1. Observation

### Observation 1.1: Concurrency Failure on `POST /api/jobs` (Multiple Simultaneous Sync Jobs)
- **Command Executed**:
  ```bash
  go test -v -race -run=TestAdversarial_JobConcurrency_Race ./pkg/api/...
  ```
- **Verbatim Output**:
  ```
  === RUN   TestAdversarial_JobConcurrency_Race
      adversarial_test.go:123: Adversarial Concurrency Result: Accepted=7, Conflict=13, Other=0
      adversarial_test.go:128: CONCURRENCY VULNERABILITY DETECTED: Expected exactly 1 accepted job (202), but got 7 accepted jobs! Responses: [409 409 409 202 409 409 409 202 409 202 409 409 409 202 202 409 202 409 409 202]
      adversarial_test.go:133: Expected 19 conflicts (409), but got 13
      adversarial_test.go:138: Active job in DB after concurrent burst: activeJob=&{JobID:job-1792591840397330000 Status:RUNNING TotalBatches:2 CommittedBatchIndex:-1 Summary:{Added:0 Updated:0 Withdrawn:0 Skipped:0 DeadLetter:0} CreatedAt:2026-10-21 14:10:40.397337 +0000 UTC UpdatedAt:2026-10-21 14:10:40.397337 +0000 UTC CompletedAt:<nil>}, err=<nil>
  --- FAIL: TestAdversarial_JobConcurrency_Race (0.11s)
  ```
- **Code Reference**: `pkg/api/handlers.go:88-230`:
  ```go
  // 1. Check if a job is already actively running
  active, err := h.storage.GetActiveJob(ctx)
  if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
      h.writeError(w, http.StatusInternalServerError, "failed to check active jobs: "+err.Error(), ErrCodeInternalError)
      return
  }
  if active != nil {
      h.writeError(w, http.StatusConflict, fmt.Sprintf("a synchronization job is already running: %s", active.JobID), ErrCodeJobConflict)
      return
  }

  // 2. Parse request body ...

  // 3. Create initial job state in storage
  now := time.Now().UTC()
  job := &domain.SyncJob{
      JobID:               jobID,
      Status:              domain.JobStatusRunning,
      TotalBatches:        totalBatches,
      CommittedBatchIndex: -1,
      Summary:             domain.ChangeSummary{},
      CreatedAt:           now,
      UpdatedAt:           now,
  }

  if err := h.storage.CreateJob(ctx, job); err != nil {
      h.writeError(w, http.StatusInternalServerError, "failed to create sync job: "+err.Error(), ErrCodeInternalError)
      return
  }
  ```
  Notice: Steps 1 through 3 are executed completely unsynchronized without mutex or atomic transaction locking. In SQLite (`pkg/storage/sqlite.go:126-139`), the `sync_jobs` table has no unique constraint or trigger on `status = 'RUNNING'`.

---

### Observation 1.2: Concurrency Failure on `POST /api/jobs/{id}/resume` (Multiple Resumptions of Same Job)
- **Command Executed**:
  ```bash
  go test -v -race -run=TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace ./pkg/api/...
  ```
- **Verbatim Output**:
  ```
  === RUN   TestAdversarial_InvalidResumptionScenarios
  === RUN   TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace
      adversarial_test.go:274: Concurrent Resume Race: Accepted=5, Conflict=5
      adversarial_test.go:276: CONCURRENCY VULNERABILITY IN RESUME: Expected exactly 1 accepted resume (202), but got 5 accepted resumes!
  --- FAIL: TestAdversarial_InvalidResumptionScenarios (0.08s)
      --- FAIL: TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace (0.03s)
  ```
  *(Note: In an earlier test run, Accepted was 10 out of 10).*
- **Code Reference**: `pkg/api/handlers.go:334-428`:
  `handleResumeJob` only queries the database for active jobs (`GetActiveJob`) and the target job (`GetJob`). It does not update the database status to `RUNNING` synchronously before responding with HTTP 202; the status update is deferred to the asynchronous background goroutine calling `h.engine.ResumeJob`.

---

### Observation 1.3: Dead-Letter Queue `reason` Filter Ignored on `GET /api/dead-letter`
- **Command Executed**:
  ```bash
  go test -v -race -run=TestAdversarial_DeadLetterQueue_PaginationAndFiltering/FilterByReason ./pkg/api/...
  ```
- **Verbatim Output**:
  ```
  === RUN   TestAdversarial_DeadLetterQueue_PaginationAndFiltering
  === RUN   TestAdversarial_DeadLetterQueue_PaginationAndFiltering/FilterByReason
      adversarial_test.go:480: GET /api/dead-letter?reason=negative price returned Total=65, Count=20
      adversarial_test.go:484: SPECIFICATION VIOLATION / DEFECT: Expected 25 records filtered by reason 'negative price', but got Total=65. Reason query parameter is ignored by API handler.
  --- FAIL: TestAdversarial_DeadLetterQueue_PaginationAndFiltering (0.08s)
      --- FAIL: TestAdversarial_DeadLetterQueue_PaginationAndFiltering/FilterByReason (0.01s)
  ```
- **Worker Claim** (`worker_m4/handoff.md:28`):
  > "- `GET /api/dead-letter`: paginated list of unprocessable records with optional `job_id` and `reason` filters."
- **Code Reference**: `pkg/api/handlers.go:586-627`:
  ```go
  // handleListDeadLetter handles GET /api/dead-letter.
  func (h *Handler) handleListDeadLetter(w http.ResponseWriter, r *http.Request) {
      ctx := r.Context()
      q := r.URL.Query()
      ...
      jobID := q.Get("job_id")

      issues, total, err := h.storage.ListDeadLetterRecords(ctx, jobID, limit, offset)
  ```
  Notice: `reason` query parameter is never extracted (`q.Get("reason")` does not exist).
  `pkg/storage/sqlite.go:502-559`: `ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int)` does not accept or filter by reason.

---

### Observation 1.4: Resumption Status Code on Completed Jobs
- **Command Executed**:
  ```bash
  go test -v -race -run=TestAdversarial_InvalidResumptionScenarios/ResumingCompletedJob ./pkg/api/...
  ```
- **Verbatim Output**:
  ```
  === RUN   TestAdversarial_InvalidResumptionScenarios
  === RUN   TestAdversarial_InvalidResumptionScenarios/ResumingCompletedJob
      adversarial_test.go:216: Completed job resume response status: 409, body: {"error":"job is already completed and cannot be resumed","code":"JOB_CONFLICT"}
      adversarial_test.go:224: Observed: Implementation returned 409 Conflict instead of 400 Bad Request for completed job resume.
  --- PASS: TestAdversarial_InvalidResumptionScenarios (0.04s)
  ```
- **Code Reference**: `pkg/api/handlers.go:357`:
  ```go
  if job.Status == domain.JobStatusCompleted {
      h.writeError(w, http.StatusConflict, "job is already completed and cannot be resumed", ErrCodeJobConflict)
      return
  }
  ```
  DISPATCH.md specified: `completed jobs (assert 400)`, whereas the implementation and `spec_miner_m4_1` specify `409 Conflict`.

---

### Observation 1.5: Passing Capabilities
- Non-existent job resume correctly returns `404 Not Found` (`JOB_NOT_FOUND`).
- Actively running job resume correctly returns `409 Conflict` (`JOB_CONFLICT`).
- Invariant D isolation: 65 corrupt records quarantined into `dead_letter_issues` with valid JSON raw payloads and explicit reasons (`"negative price: ..."`, `"empty SKU"`, `"zero or uninitialized timestamp"`, `"invalid status: ..."`), while 5 valid records in the same batch were cleanly committed to catalog.
- Catalog Browser filtering:
  - `GET /api/products?status=ACTIVE` filters exclusively active products.
  - `GET /api/products?status=WITHDRAWN` filters exclusively withdrawn products.
  - `GET /api/products?status=INVALID` returns `400 Bad Request`.
  - `GET /api/products?search=Apple` performs case-insensitive substring search matching SKU and Name.
  - Combined `status` + `search` + pagination (`limit`, `offset`) correctly updates total count and returns matched slice.

---

## 2. Logic Chain

1. **TOCTOU Vulnerability in `POST /api/jobs`**:
   - In `handleStartJob`, the check for existing running jobs (`h.storage.GetActiveJob`) and the insertion of the new running job (`h.storage.CreateJob`) are distinct, non-atomic database operations without in-memory synchronization (e.g. `h.mu.Lock()`).
   - When 20 goroutines burst `POST /api/jobs` simultaneously, multiple goroutines query the database before any goroutine finishes inserting its job. All of them see `active == nil`.
   - Consequently, multiple goroutines call `CreateJob` and respond with `202 Accepted` (observed 6 to 9 accepted jobs per burst).
   - Each accepted request spawns an independent background goroutine invoking `h.engine.RunJob`, violating the single-job concurrency guarantee and risking data corruption from concurrent SQLite writers.

2. **TOCTOU Vulnerability in `POST /api/jobs/{id}/resume`**:
   - In `handleResumeJob`, the handler checks `GetActiveJob` and `GetJob`, then immediately returns `202 Accepted` without changing the job's database status to `RUNNING`.
   - When multiple concurrent resume requests are made for the same interrupted job, all requests read `status == INTERRUPTED` and return `202 Accepted` (observed 5 to 10 accepted resumes per burst).
   - Multiple background goroutines execute `h.engine.ResumeJob` simultaneously for the same job, creating conflicting batch transactions.

3. **Missing DLQ Reason Filter**:
   - Worker's handoff explicitly asserted that `GET /api/dead-letter` supports optional `job_id` and `reason` filters.
   - However, `handleListDeadLetter` in `pkg/api/handlers.go:612` ignores `reason`, and `storage.ListDeadLetterRecords` does not implement reason filtering in SQL.
   - An adversarial request with `?reason=negative%20price` returns all 65 records instead of the 25 negative price records.

4. **Verdict Determination**:
   - Because concurrent job initiation allows multiple concurrent sync jobs to run simultaneously, concurrent resume allows multiple workers to resume the same job, and dead-letter filtering by reason is not implemented despite specification requirements, the implementation fails empirical correctness verification.

---

## 3. Caveats

1. **Completed Job Status Code (409 vs 400)**:
   - Returning `409 Conflict` for resuming a completed job is RESTfully valid as a state conflict, matching `spec_miner_m4_1/handoff.md:429`, although DISPATCH.md requested asserting `400 Bad Request`. This is documented as an informational divergence rather than the primary blocker.
2. **Implementation Code Untouched**:
   - In strict compliance with the review-only constraint ("do NOT modify implementation code"), no changes were made to `pkg/api/handlers.go`, `pkg/storage/sqlite.go`, or `pkg/sync/engine.go`.

---

## 4. Conclusion

**Verdict: CONFIRM_CORRECTNESS_FAILED**

The M4 REST Control Plane fails adversarial stress testing due to:
1. **Critical Concurrency Defect**: `POST /api/jobs` lacks synchronization; burst traffic allows multiple concurrent sync jobs to transition to `RUNNING` simultaneously.
2. **Critical Concurrency Defect**: `POST /api/jobs/{id}/resume` lacks synchronization; burst traffic allows multiple concurrent resumption processes on the same interrupted job.
3. **Specification Omission**: `GET /api/dead-letter` ignores the `reason` filter parameter, returning un-filtered records and contradicting worker handoff claims.

### Recommended Fixes for Remediation:
1. In `pkg/api/handlers.go:handleStartJob`, serialize the check-then-create block using `h.mu.Lock()` or an atomic SQLite transaction with an `EXCLUDE` / `UNIQUE` constraint or status trigger on `RUNNING` jobs.
2. In `pkg/api/handlers.go:handleResumeJob`, synchronously update the job status from `INTERRUPTED` to `RUNNING` inside a serialized transaction before responding `202 Accepted`.
3. In `pkg/api/handlers.go:handleListDeadLetter` and `pkg/storage/sqlite.go:ListDeadLetterRecords`, parse `q.Get("reason")` and apply SQL `WHERE reason LIKE ?` filtering.

---

## 5. Verification Method

To independently reproduce the failures:

1. **Execute the adversarial challenge test suite**:
   ```bash
   go test -v -race -run="TestAdversarial_(JobConcurrency|InvalidResumption|DeadLetterQueue|CatalogBrowser)" ./pkg/api/...
   ```
   *Expected outcome*:
   - `TestAdversarial_JobConcurrency_Race`: FAILS with `CONCURRENCY VULNERABILITY DETECTED: Expected exactly 1 accepted job (202), but got N accepted jobs!`
   - `TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace`: FAILS with `CONCURRENCY VULNERABILITY IN RESUME: Expected exactly 1 accepted resume (202), but got N accepted resumes!`
   - `TestAdversarial_DeadLetterQueue_PaginationAndFiltering/FilterByReason`: FAILS with `SPECIFICATION VIOLATION / DEFECT: Expected 25 records filtered by reason 'negative price', but got Total=65`.
   - `TestAdversarial_CatalogBrowserFiltering`: PASSES.

2. **Inspect test implementation**:
   - File: `pkg/api/adversarial_test.go`
