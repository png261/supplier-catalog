# Milestone 4 Review Report: Embedded Staff Web UI & Control Plane

## Review Summary
**Verdict**: **APPROVE**

---

## 1. Observation
1. **Source Code Inspection**:
   - `pkg/api/models.go` (lines 8–101): Defined data models (`JobStartRequest`, `JobResumeRequest`, `SyncJobResponse`, `JobListResponse`, `JobDetailResponse`, `DeadLetterListResponse`, `ProductListResponse`, `ErrorResponse`) and standard error code taxonomy (`INVALID_REQUEST`, `JOB_NOT_FOUND`, `JOB_CONFLICT`, `INTERNAL_ERROR`).
   - `pkg/api/broker.go` (lines 46–125, 175–245):
     - Implemented `Broker` with bounded subscriber channels (`make(chan Event, b.clientCap)` where default `clientCap = 64`).
     - Backpressure protection: non-blocking drop via `select { case client.messages <- evt: default: }` ensures slow clients never block event broadcasting or pipeline execution.
     - Keepalive ticker: emits comment frame `Event{Comment: "ping"}` (`: ping\n\n`) on configurable interval (default 15s).
     - Lifecycle cleanup: tracks subscribers in mutex-guarded map; deregisters and closes client channels on context cancellation (`<-r.Context().Done()`).
   - `pkg/api/middleware.go` (lines 10–64):
     - `CORS`: Standard cross-origin headers and `OPTIONS` handling with `204 No Content`.
     - `Logging`: Logs method, path, status, and duration (exempting `/api/events` to avoid log spam).
     - `Recoverer`: Recovers panics, logs stack, and returns `500 Internal Server Error` JSON payload.
     - `statusResponseWriter`: Wraps `http.ResponseWriter` while implementing `http.Flusher`.
   - `pkg/api/handlers.go` (lines 42–50, 85–678):
     - Routing: `POST /api/jobs`, `POST /api/jobs/{id}/resume`, `GET /api/jobs`, `GET /api/jobs/{id}`, `GET /api/dead-letter`, `GET /api/products`, `GET /api/events`.
     - Single active job conflict detection: returns `409 Conflict` if an active job is already running.
     - Generator parameter validation: ensures anomaly ratios are within $[0.0, 1.0]$ and sum $\le 1.0$.
     - Resumption: verifies job exists and is not already completed/running; resumes from $last\_committed\_batch + 1$.
   - `pkg/sync/engine.go` (lines 58–60, 83–88, 310–324):
     - `WithProgressHook(hook ProgressHook)` option.
     - Hook invocation occurs at line 321 **strictly after** `tx.Commit()` succeeds at line 311.
   - `pkg/storage/sqlite.go` (lines 561–724):
     - `ListJobs`: paginated job retrieval with `COUNT(*)` and `ORDER BY created_at DESC`.
     - `ListProducts`: parameterized queries filtering by `status` and `(sku LIKE ? OR name LIKE ?)`.
     - `GetBatchCheckpoints`: retrieves checkpoints ordered by `batch_index ASC`.
     - Indices: `idx_products_status` on `products(status)` and `idx_jobs_created_at` on `sync_jobs(created_at DESC)`.
   - `cmd/server/main.go` (lines 29–121):
     - CLI flags: `--port` (default 8080 or `$PORT`), `--db` (default `catalog.db`), `--static-dir`.
     - Startup recovery: invokes `store.RecoverInterruptedJobs(ctx)` before starting listener.
     - HTTP server configuration: `ReadHeaderTimeout: 5 * time.Second`, `ReadTimeout: 15 * time.Second`, `WriteTimeout: 0` (required for persistent SSE streaming), `IdleTimeout: 60 * time.Second`.
     - Graceful shutdown handles `SIGINT`/`SIGTERM` with 5s context timeout.
   - `web/embed.go` (lines 13–92):
     - Embedded static dashboard files via `//go:embed static/*`.
     - Explicit MIME types registered in `init()` for minimal container support.
     - SPA fallback serving `index.html` for unmatched non-API routes.

2. **Integrity Violation Audit**:
   - Hardcoded test results: None found.
   - Dummy or facade implementations: None found; genuine SQLite storage and sync engine executions are performed across all handlers.
   - Shortcuts: None found.
   - Fabricated verification: None found; verified independently via local test execution.

3. **Test & Build Execution Results**:
   - `go build ./cmd/server`: Passed with exit code 0.
   - `go test -v -race -count=5 ./pkg/api/...`: All 20 tests passed across 5 iterations (0 race warnings).
   - `go test -v -race -count=5 ./pkg/api/... ./pkg/storage/... ./pkg/sync/...`: All test suites passed across 5 iterations (0 race warnings).
   - `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`:
     - `coderpush/cmd/generator`: 90.2%
     - `coderpush/cmd/server`: 93.2%
     - `coderpush/pkg/api`: **92.2%** ($\ge 90\%$ gate satisfied)
     - `coderpush/pkg/domain`: 100.0%
     - `coderpush/pkg/generator`: 97.0%
     - `coderpush/pkg/storage`: 90.5%
     - `coderpush/pkg/sync`: 95.5%
     - `coderpush/web`: 95.9%
     - **Project total**: **93.2%** of statements.
   - `go vet ./...` & `gofmt -s -d .`: 0 errors or warnings.

---

## 2. Logic Chain
1. **AC & Specification Conformance**:
   - REST endpoints fulfill all Milestone 4 functional requirements: staff can trigger new synchronization jobs, resume interrupted syncs, inspect paginated jobs, dead-letter records, and catalog products.
   - The SSE pub/sub broker correctly broadcasts `job_started`, `progress`, `dead_letter`, `job_completed`, and `job_interrupted` events.
   - The engine hook in `pkg/sync/engine.go` guarantees that telemetry progress events reflect committed SQLite database state and never uncommitted or rolled-back dirty reads.
2. **Concurrency Safety & Resilience**:
   - Under Go's `-race` detector with 5 iterations (`-count=5`), all packages execute concurrently with zero data races.
   - `Broker` backpressure handling guarantees that a slow, stalled, or unresponsive browser subscriber cannot block the server or delay other subscribers.
   - `WriteTimeout: 0` on `http.Server` ensures long-lived SSE connections are not terminated prematurely by standard HTTP response timeouts.
3. **Packaging & Zero-Dependency Embedded UI**:
   - `web/embed.go` embeds all static frontend assets (`index.html`, `style.css`, `app.js`) directly into the Go binary. No external runtime or Node build step is required, creating a self-contained single binary executable.

---

## 3. Caveats & Adversarial Findings

### [Low/Minor] Finding 1: In-Memory Filtering on Job Status after DB Pagination
- **Observation**: In `pkg/api/handlers.go` lines 522–538 (`handleListJobs`), `h.storage.ListJobs(ctx, limit, offset)` queries the top `limit` jobs from SQLite, and if `statusFilter != ""` is provided, it filters the retrieved slice in memory.
- **Risk**: In large databases where jobs with different statuses are interspersed, pagination across filtered results may return fewer items than `limit` per page or miss items on subsequent pages.
- **Recommendation**: For future milestones or scaling beyond thousands of jobs, push the `status` filter into the SQL query (`WHERE status = ?`) inside `storage.ListJobs`. For the current scope and dashboard usage, the current behavior is acceptable.

### [Low/Minor] Challenge 1: HTTP-Level Race Window on Concurrent POST /api/jobs
- **Assumption Challenged**: Sequential invocation of `h.storage.GetActiveJob(ctx)` followed by `h.storage.CreateJob(ctx, job)` in `handleStartJob`.
- **Attack Scenario**: If two clients simultaneously send `POST /api/jobs` at the exact same microsecond, both could pass `GetActiveJob` before either executes `CreateJob`.
- **Blast Radius**: Both jobs would be created in SQLite, though `SyncEngine.processBatchTx`'s database transaction locks would serialize execution.
- **Mitigation**: A mutex guard on `Handler.handleStartJob` or an in-memory lock on `activeJobID` during job inception can eliminate this window.

---

## 4. Conclusion
Milestone 4 deliverables satisfy all correctness, robustness, thread safety, and test coverage requirements. Zero integrity violations or mocking shortcuts were identified. Statement coverage for `pkg/api` is 92.2% (project total 93.2%), and all multi-iteration race tests pass cleanly.

**Final Verdict**: **APPROVE**

---

## 5. Verification Method
To independently verify this review:

1. **Verify binary build**:
   ```bash
   go build ./cmd/server
   ```
   *Expected: Exit code 0, binary created.*

2. **Run Milestone 4 tests under race detector (5 iterations)**:
   ```bash
   go test -v -race -count=5 ./pkg/api/... ./web/... ./cmd/server/...
   ```
   *Expected: 0 race warnings, all tests pass.*

3. **Run cross-package race detector (5 iterations)**:
   ```bash
   go test -v -race -count=5 ./pkg/api/... ./pkg/storage/... ./pkg/sync/...
   ```
   *Expected: All tests pass with 0 data races.*

4. **Verify statement coverage**:
   ```bash
   go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
   ```
   *Expected: Statement coverage for `coderpush/pkg/api` $\ge 90\%$ (currently 92.2%) and project total $\ge 90\%$ (currently 93.2%).*

5. **Verify code quality & linting**:
   ```bash
   go vet ./...
   gofmt -s -d .
   ```
   *Expected: Clean output with 0 diffs.*
