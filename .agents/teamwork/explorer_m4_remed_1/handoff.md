# Handoff Report: Concurrency TOCTOU Remediation for Job Initiation & Resumption

## Executive Summary
This report presents the forensic investigation and verified fix strategy for the critical Time-of-Check to Time-of-Use (TOCTOU) concurrency vulnerability on `POST /api/jobs` and `POST /api/jobs/{id}/resume` in `pkg/api/handlers.go`. Under concurrent bursts, un-serialized database queries allowed multiple jobs to start concurrently (violating the single-job constraint) and multiple resumptions of the same interrupted job to be accepted.

A machine-applicable patch has been generated and validated at `.agents/teamwork/explorer_m4_remed_1/handlers_concurrency_fix.patch`. When tested under `-race`, the fix achieved 100% compliance: exactly 1 accepted job and 19 conflicts on job start bursts, and exactly 1 accepted resume and 9 conflicts on resume bursts.

---

## 1. Observation

### Observation 1.1: Empirical Concurrency Failure on `POST /api/jobs`
Executing the adversarial concurrency test against the baseline codebase:
```bash
go test -v -race -run=TestAdversarial_JobConcurrency_Race ./pkg/api/...
```
Verbatim failure output:
```
=== RUN   TestAdversarial_JobConcurrency_Race
    adversarial_test.go:123: Adversarial Concurrency Result: Accepted=5, Conflict=15, Other=0
    adversarial_test.go:128: CONCURRENCY VULNERABILITY DETECTED: Expected exactly 1 accepted job (202), but got 5 accepted jobs! Responses: [409 409 409 202 409 409 409 409 409 202 409 409 202 202 202 409 409 409 409 409]
    adversarial_test.go:133: Expected 19 conflicts (409), but got 15
    adversarial_test.go:138: Active job in DB after concurrent burst: activeJob=&{JobID:job-1792592565955509000 Status:RUNNING TotalBatches:2 CommittedBatchIndex:-1 Summary:{Added:0 Updated:0 Withdrawn:0 Skipped:0 DeadLetter:0} CreatedAt:2026-10-21 14:22:45.955518 +0000 UTC UpdatedAt:2026-10-21 14:22:45.955518 +0000 UTC CompletedAt:<nil>}, err=<nil>
--- FAIL: TestAdversarial_JobConcurrency_Race (0.05s)
FAIL
```
*Direct Code Reference*: `pkg/api/handlers.go:88-97` and `pkg/api/handlers.go:213-258`:
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
// ... [Parsing request body, validating configuration, generating job ID] ...
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
if err := h.storage.CreateJob(ctx, job); err != nil { ... }
// 4. Return 202 Accepted
h.writeJSON(w, http.StatusAccepted, SyncJobResponse{ ... })
// 6. Spawn background job execution
bgCtx, cancel := context.WithCancel(context.Background())
h.mu.Lock()
h.activeJobID = jobID
h.activeCancel = cancel
h.mu.Unlock()
```

### Observation 1.2: Empirical Concurrency Failure on `POST /api/jobs/{id}/resume`
Executing the adversarial resume concurrency test against baseline codebase:
```bash
go test -v -race -run=TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace ./pkg/api/...
```
Verbatim failure output:
```
=== RUN   TestAdversarial_InvalidResumptionScenarios
=== RUN   TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace
    adversarial_test.go:274: Concurrent Resume Race: Accepted=6, Conflict=4
    adversarial_test.go:276: CONCURRENCY VULNERABILITY IN RESUME: Expected exactly 1 accepted resume (202), but got 6 accepted resumes!
--- FAIL: TestAdversarial_InvalidResumptionScenarios (0.13s)
    --- FAIL: TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace (0.11s)
FAIL
```
*Direct Code Reference*: `pkg/api/handlers.go:333-363` and `pkg/api/handlers.go:397-427`:
```go
// 1. Check if another job is already actively running
active, err := h.storage.GetActiveJob(ctx)
if err != nil && ... { ... }
if active != nil { ... }

// 2. Fetch target job
job, err := h.storage.GetJob(ctx, jobID)
if err != nil { ... }

// 3. Verify job state
if job.Status == domain.JobStatusCompleted { ... }
if job.Status == domain.JobStatusRunning { ... }

// ... [Parse optional resume config] ...
// 5. Respond 202 Accepted
h.writeJSON(w, http.StatusAccepted, SyncJobResponse{ ... })

// 7. Spawn resume in background
bgCtx, cancel := context.WithCancel(context.Background())
h.mu.Lock()
h.activeJobID = jobID
h.activeCancel = cancel
h.mu.Unlock()
```
Notice:
1. Target job in database has `status = 'INTERRUPTED'`.
2. `GetActiveJob` queries `status IN ('RUNNING', 'PENDING')` and returns `nil`.
3. `GetJob` loads target job with `status = 'INTERRUPTED'`.
4. The handler never updates `status` to `'RUNNING'` in SQLite before responding HTTP 202! The status update was deferred into the asynchronous background goroutine calling `h.engine.ResumeJob`.
5. Multiple concurrent requests read the exact same database state and all return 202 Accepted.

---

## 2. Logic Chain

1. **TOCTOU Gap in `handleStartJob`**:
   - Step 1 (`GetActiveJob`) checks whether a job is actively running in SQLite.
   - Step 3 (`CreateJob`) inserts a new record with `Status: JobStatusRunning`.
   - Between Step 1 and Step 3, the execution path is completely unsynchronized. In a multi-threaded web server (`net/http`), multiple HTTP request handler goroutines execute Step 1 concurrently before any goroutine reaches Step 3.
   - SQLite table `sync_jobs` has no database-level unique constraint on `status = 'RUNNING'` (and as observed in `pkg/sync/resumability_test.go:337-342`, test suites deliberately insert multiple crash states to test crash recovery).
   - Therefore, multiple requests succeed in inserting `RUNNING` jobs into SQLite and each returns HTTP 202 Accepted, spawning conflicting background engine runs.

2. **TOCTOU Gap in `handleResumeJob`**:
   - An interrupted job has `status = 'INTERRUPTED'`.
   - When multiple resume requests arrive for this job, each queries `GetActiveJob` (which returns `nil` because no job is currently `RUNNING`).
   - Each queries `GetJob`, finding `status == INTERRUPTED` (which is valid for resumption).
   - The handler immediately responds with `202 Accepted` and only dispatches the status update to `RUNNING` inside the asynchronous background goroutine (`h.engine.ResumeJob`).
   - All concurrent requests in the burst observe the job still in `INTERRUPTED` state and return 202 Accepted.

3. **Inadequacy of Current In-Memory Locking**:
   - Currently, `h.mu.Lock()` is only acquired to set `h.activeJobID = jobID` *after* HTTP 202 has already been written.
   - This in-memory write happens too late to prevent earlier concurrent requests from returning 202.

4. **Remediation Strategy**:
   - **Request parsing before lock**: Parse `r.Body`, unmarshal JSON, validate parameters, and validate generator configs outside `h.mu.Lock()`. If the request is malformed (HTTP 400), it rejects immediately without lock contention.
   - **Critical section synchronization**:
     1. Acquire `h.mu.Lock()`.
     2. Fast-path check: `if h.activeJobID != ""` -> reject with 409 Conflict.
     3. Storage check: `active, err := h.storage.GetActiveJob(ctx)` -> if found, update `h.activeJobID` and reject with 409 Conflict.
     4. In `handleStartJob`: Execute `h.storage.CreateJob(ctx, job)` while holding `h.mu.Lock()`.
     5. In `handleResumeJob`: Fetch `job`, verify status, and synchronously execute `h.storage.UpdateJobStatus(ctx, jobID, domain.JobStatusRunning)` while holding `h.mu.Lock()`.
     6. Register in-memory state: `h.activeJobID = jobID`, `h.activeCancel = cancel`, `h.activeConfigs[jobID] = cfg`.
     7. Release `h.mu.Unlock()`.
   - **Response and Execution outside lock**: Write HTTP 202 Accepted, broadcast SSE `job_started`, and launch background goroutine.
   - **Background cleanup**: The background goroutine deferred function cleanly locks `h.mu`, checks `if h.activeJobID == jobID`, and resets `h.activeJobID = ""` and `h.activeCancel = nil`.

5. **Empirical Validation**:
   - Applying this pattern to `pkg/api/handlers.go` completely eliminates the race window.
   - 20 concurrent `POST /api/jobs` requests: exactly 1 accepted (202), 19 rejected (409 Conflict).
   - 10 concurrent `POST /api/jobs/{id}/resume` requests: exactly 1 accepted (202), 9 rejected (409 Conflict).

---

## 3. Caveats

1. **Scope Boundaries**:
   - This remediation specifically targets the TOCTOU race conditions in `handleStartJob` and `handleResumeJob` within `pkg/api/handlers.go`.
   - The Dead-Letter Queue `reason` filter omission (`handleListDeadLetter` / `ListDeadLetterRecords`) is assigned to and being investigated by `explorer_m4_remed_2`.
   - The SSE Broker client ID collision (`time.Now().UnixNano()`) is assigned to and being investigated by `spec_miner_m4_remed_1`.
2. **Read-Only Investigation Compliance**:
   - In accordance with the Explorer archetype rules, the codebase has been left in its original untouched state.
   - The verified fix is delivered as a unified diff patch file at `.agents/teamwork/explorer_m4_remed_1/handlers_concurrency_fix.patch`.
3. **Database-Level Constraints**:
   - We investigated whether a partial unique index (`CREATE UNIQUE INDEX idx_single_running ON sync_jobs(status) WHERE status IN ('RUNNING', 'PENDING')`) should be added in SQLite. We determined this should NOT be done because existing test suites (`pkg/sync/resumability_test.go:337-342`) deliberately insert multiple crash states into the table to verify `RecoverOnStartup`. Mutex synchronization at the control plane layer (`Handler.mu`) is the correct architectural boundary.

---

## 4. Conclusion

The concurrency vulnerabilities on `POST /api/jobs` and `POST /api/jobs/{id}/resume` are caused by time-of-check to time-of-use race windows between SQLite status queries and state transitions.

The solution requires:
1. Moving payload parsing outside the mutex.
2. Serializing active job verification and synchronous state transition (`CreateJob` or `UpdateJobStatus` to `RUNNING`) under `h.mu.Lock()`.
3. Updating `h.activeJobID` synchronously before releasing `h.mu.Unlock()` and before returning HTTP 202.

### Concrete Patch Implementation
The complete patch file is saved at:
`/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/handlers_concurrency_fix.patch`

#### Patch Snippet: `handleStartJob`
```diff
--- a/pkg/api/handlers.go
+++ b/pkg/api/handlers.go
@@ -85,18 +85,7 @@
 func (h *Handler) handleStartJob(w http.ResponseWriter, r *http.Request) {
 	ctx := r.Context()
 
-	// 1. Check if a job is already actively running
-	active, err := h.storage.GetActiveJob(ctx)
-	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
-		h.writeError(w, http.StatusInternalServerError, "failed to check active jobs: "+err.Error(), ErrCodeInternalError)
-		return
-	}
-	if active != nil {
-		h.writeError(w, http.StatusConflict, fmt.Sprintf("a synchronization job is already running: %s", active.JobID), ErrCodeJobConflict)
-		return
-	}
-
-	// 2. Parse request body
+	// 1. Parse request body and validate inputs prior to locking
 	var req JobStartRequest
 	if r.Body != nil && r.ContentLength != 0 {
 		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
@@ -200,10 +189,6 @@
 		}
 
 		totalBatches = cfg.TotalBatches
-
-		h.mu.Lock()
-		h.activeConfigs[jobID] = cfg
-		h.mu.Unlock()
 	} else {
 		h.writeError(w, http.StatusBadRequest, "unsupported mode: "+req.Mode, ErrCodeInvalidRequest)
 		return
@@ -210,5 +195,31 @@
+	// 2. Synchronized Active Job Verification, Storage Creation & State Transition
+	h.mu.Lock()
+
+	// Check 2a: In-memory active job (fast path)
+	if h.activeJobID != "" {
+		activeID := h.activeJobID
+		h.mu.Unlock()
+		h.writeError(w, http.StatusConflict, fmt.Sprintf("a synchronization job is already running: %s", activeID), ErrCodeJobConflict)
+		return
+	}
+
+	// Check 2b: Persistent storage active job
+	active, err := h.storage.GetActiveJob(ctx)
+	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
+		h.mu.Unlock()
+		h.writeError(w, http.StatusInternalServerError, "failed to check active jobs: "+err.Error(), ErrCodeInternalError)
+		return
+	}
+	if active != nil {
+		activeID := active.JobID
+		h.activeJobID = activeID
+		h.mu.Unlock()
+		h.writeError(w, http.StatusConflict, fmt.Sprintf("a synchronization job is already running: %s", activeID), ErrCodeJobConflict)
+		return
+	}
+
+	// 3. Atomically create initial job state in storage while holding lock
 	now := time.Now().UTC()
 	job := &domain.SyncJob{
 		JobID:               jobID,
@@ -220,6 +231,7 @@
 	}
 
 	if err := h.storage.CreateJob(ctx, job); err != nil {
+		h.mu.Unlock()
 		h.writeError(w, http.StatusInternalServerError, "failed to create sync job: "+err.Error(), ErrCodeInternalError)
 		return
 	}
@@ -226,4 +238,13 @@
+	// 4. Mark job active in memory while holding lock
+	bgCtx, cancel := context.WithCancel(context.Background())
+	h.activeJobID = jobID
+	h.activeCancel = cancel
+	if req.Mode == "generator" {
+		h.activeConfigs[jobID] = cfg
+	}
+	h.mu.Unlock()
+
+	// 5. Return 202 Accepted
```

#### Patch Snippet: `handleResumeJob`
```diff
--- a/pkg/api/handlers.go
+++ b/pkg/api/handlers.go
@@ -346,9 +367,30 @@
 		return
 	}
 
-	// 1. Check if another job is already actively running
-	active, err := h.storage.GetActiveJob(ctx)
-	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
+	// 1. Parse optional resume config BEFORE locking
+	var resumeReq JobResumeRequest
+	if r.Body != nil && r.ContentLength != 0 {
+		if err := json.NewDecoder(r.Body).Decode(&resumeReq); err != nil {
+			h.writeError(w, http.StatusBadRequest, "malformed JSON request body", ErrCodeInvalidRequest)
+			return
+		}
+	}
+
+	// 2. Synchronized Active Job Verification, State Check & Status Transition
+	h.mu.Lock()
+
+	// Check 2a: In-memory active job (fast path)
+	if h.activeJobID != "" {
+		activeID := h.activeJobID
+		h.mu.Unlock()
+		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", activeID), ErrCodeJobConflict)
+		return
+	}
+
+	// Check 2b: Persistent storage active job
+	active, err := h.storage.GetActiveJob(ctx)
+	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, domain.ErrNotFound) {
+		h.mu.Unlock()
 		h.writeError(w, http.StatusInternalServerError, "failed checking active jobs: "+err.Error(), ErrCodeInternalError)
 		return
 	}
@@ -355,10 +397,14 @@
-		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", active.JobID), ErrCodeJobConflict)
-		return
-	}
-
-	// 2. Fetch target job
-	job, err := h.storage.GetJob(ctx, jobID)
-	if err != nil {
+		activeID := active.JobID
+		h.activeJobID = activeID
+		h.mu.Unlock()
+		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", activeID), ErrCodeJobConflict)
+		return
+	}
+
+	// Check 2c: Fetch target job
+	job, err := h.storage.GetJob(ctx, jobID)
+	if err != nil {
+		h.mu.Unlock()
 		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, domain.ErrNotFound) {
 			h.writeError(w, http.StatusNotFound, "sync job not found: "+jobID, ErrCodeJobNotFound)
 			return
@@ -366,8 +412,9 @@
 		return
 	}
 
-	// 3. Verify job state
+	// Check 2d: Verify target job state
 	if job.Status == domain.JobStatusCompleted {
+		h.mu.Unlock()
 		h.writeError(w, http.StatusConflict, "job is already completed and cannot be resumed", ErrCodeJobConflict)
 		return
 	}
@@ -374,3 +421,4 @@
+		h.mu.Unlock()
 		h.writeError(w, http.StatusConflict, fmt.Sprintf("job %s is already running", jobID), ErrCodeJobConflict)
 		return
 	}
@@ -397,10 +445,9 @@
 	if resumeReq.BatchSize != nil {
 		cfg.BatchSize = *resumeReq.BatchSize
 	}
-	h.activeConfigs[jobID] = cfg
-	h.mu.Unlock()
 
 	if err := cfg.Validate(); err != nil {
+		h.mu.Unlock()
 		h.writeError(w, http.StatusBadRequest, "invalid resume config: "+err.Error(), ErrCodeInvalidRequest)
 		return
 	}
@@ -407,4 +454,19 @@
-	// 5. Respond 202 Accepted
+	// 4. CRITICAL: Atomically transition job status to RUNNING in storage while holding lock
+	if err := h.storage.UpdateJobStatus(ctx, jobID, domain.JobStatusRunning); err != nil {
+		h.mu.Unlock()
 		h.writeError(w, http.StatusInternalServerError, "failed to update job status: "+err.Error(), ErrCodeInternalError)
 		return
 	}
+
+	// 5. Mark job active in memory while holding lock
+	bgCtx, cancel := context.WithCancel(context.Background())
+	h.activeJobID = jobID
+	h.activeCancel = cancel
+	h.activeConfigs[jobID] = cfg
+	h.mu.Unlock()
+
+	// 6. Respond 202 Accepted
 	now := time.Now().UTC()
 	h.writeJSON(w, http.StatusAccepted, SyncJobResponse{
 		JobID:               job.JobID,
 		Status:              domain.JobStatusRunning,
@@ -411,10 +473,10 @@
 		CommittedBatchIndex: job.CommittedBatchIndex,
 		Summary:             job.Summary,
 		CreatedAt:           job.CreatedAt,
-		UpdatedAt:           time.Now().UTC(),
+		UpdatedAt:           now,
 	})
```

---

## 5. Verification Method

To independently verify this remediation strategy:

1. **Verify patch applies cleanly**:
   ```bash
   git apply --check .agents/teamwork/explorer_m4_remed_1/handlers_concurrency_fix.patch
   ```
   *Expected Outcome: Exits with code 0 (clean dry-run).*

2. **Execute adversarial concurrency race tests with patch applied**:
   ```bash
   git apply .agents/teamwork/explorer_m4_remed_1/handlers_concurrency_fix.patch
   go test -v -race -run="TestAdversarial_(JobConcurrency_Race|InvalidResumptionScenarios)" ./pkg/api/...
   git apply -R .agents/teamwork/explorer_m4_remed_1/handlers_concurrency_fix.patch
   ```
   *Observed Empirical Output*:
   ```
   === RUN   TestAdversarial_JobConcurrency_Race
       adversarial_test.go:123: Adversarial Concurrency Result: Accepted=1, Conflict=19, Other=0
   --- PASS: TestAdversarial_JobConcurrency_Race (0.07s)
   === RUN   TestAdversarial_InvalidResumptionScenarios
   === RUN   TestAdversarial_InvalidResumptionScenarios/ResumingNonExistentJob
   === RUN   TestAdversarial_InvalidResumptionScenarios/ResumingRunningJob
   === RUN   TestAdversarial_InvalidResumptionScenarios/ResumingCompletedJob
   === RUN   TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace
       adversarial_test.go:274: Concurrent Resume Race: Accepted=1, Conflict=9
   --- PASS: TestAdversarial_InvalidResumptionScenarios (0.08s)
   PASS
   ok  	coderpush/pkg/api	1.739s
   ```

3. **Verify non-regression of standard job control endpoints**:
   ```bash
   go test -v -race -run="TestHandler_(PostJob|ResumeJob|ListJobs|GetJob)" ./pkg/api/...
   ```
   *Expected Outcome: All 9 test suites pass cleanly with 0 race detector warnings.*
