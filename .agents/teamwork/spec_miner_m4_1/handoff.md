# Handoff Report: Milestone 4 Control Plane Specification & Test Matrix

**Author**: `teamwork_preview_spec_miner` (spec_miner_m4_1)  
**Recipient**: Parent Orchestrator (`e6900bb2-6639-41ea-b0fb-48d815c2c1c8`)  
**Target Milestone**: Milestone 4 — Embedded Staff Web UI & Control Plane (`pkg/api`, `cmd/server`, `web`)  
**Working Directory**: `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1`  

---

## 1. Observation

Authoritative project specifications and existing codebase implementations were examined directly:

1. **`ORIGINAL_REQUEST.md` (lines 28–31)**:
   > "R4. Minimalist Staff Interface & Status Reporting: Provide a clean, minimalist single-page dashboard embedded directly into the Go binary. Allow staff to start an update, view real-time completion progress, inspect a summary of changes (+Added, ~Updated, -Withdrawn, ↷Skipped), view issues needing attention, and resume interrupted updates."
2. **`PROJECT.md` (lines 22–25, 54–57, 74)**:
   > - Feature 15: Embedded Staff Web UI via `//go:embed` serving single-page dashboard.
   > - Feature 16: Real-Time Sync Status & SSE API (`/api/events`) streaming progress and change counters.
   > - Feature 17: Job Control & Resume API (endpoints to start new sync jobs or resume interrupted jobs).
   > - Feature 18: Dead-Letter Queue Viewer API (paginated query endpoint for quarantined records and reasons).
3. **`DISPATCH.md` (lines 14–32)**:
   > - Formalize REST API contracts with JSON schemas, HTTP status codes (200, 201, 202, 400, 404, 409 Conflict when job already running, 500), and error format `{"error": "message", "code": "CODE"}`.
   > - Formalize SSE protocol: `job_started`, `progress`, `job_completed`, `job_interrupted`, `dead_letter`, `retry: 3000\n`, ping keepalive every 15s.
   > - Design test matrix targeting statement coverage $\ge 90\%$ and zero data races under `-race -count=5`.
4. **`pkg/domain/models.go` (lines 78–142)**:
   > - `ChangeSummary` has counters: `Added`, `Updated`, `Withdrawn`, `Skipped`, `DeadLetter`.
   > - `SyncJob` statuses: `PENDING`, `RUNNING`, `INTERRUPTED`, `COMPLETED`, `FAILED`. Fields include `JobID`, `TotalBatches`, `CommittedBatchIndex` (-1 before first batch), `Summary`, `CreatedAt`, `UpdatedAt`, `CompletedAt`.
   > - `BatchCheckpoint` audits `JobID`, `BatchIndex`, `ItemsCount`, `Summary`, `CommittedAt`.
   > - `DeadLetterRecord` contains `IssueID`, `JobID`, `BatchIndex`, `SKU`, `RawPayload`, `Reason`, `CreatedAt`.
5. **`pkg/storage/sqlite.go` (lines 315–354, 497–554)**:
   > - `GetActiveJob(ctx)` returns any job with `status IN ('RUNNING', 'PENDING') ORDER BY created_at DESC LIMIT 1`.
   > - `ListDeadLetterRecords(ctx, jobID, limit, offset)` performs paginated SQL query filtered by `job_id` or returning all records when `jobID == ""`, returning `[]DeadLetterRecord, totalCount, error`.
6. **`pkg/sync/engine.go` (lines 416–428, 452–460, 481–513)**:
   > - `activeJobs map[string]bool` guarded by `mu stdsync.Mutex`.
   > - Attempting to run an active job returns `ErrJobAlreadyRunning`.
   > - Attempting to run or resume a completed job returns `ErrJobAlreadyCompleted`.
   > - Resuming non-existent job returns `domain.ErrNotFound`.

---

## 2. Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | REST API: Job Control | Trigger Sync Job | Starts a new catalog synchronization job using mock generator parameters or explicit batch payload | `POST /api/jobs` with JSON body (`mode`, `seed`, `total_batches`, `batch_size`, anomaly ratios, or `batches`) | `202 Accepted` with initial `SyncJob` JSON (`status: RUNNING`) | `400 Bad Request` on invalid config/JSON;<br>`409 Conflict` if another job is already running;<br>`500 Internal Error` on DB error | `ORIGINAL_REQUEST.md:30`, `DISPATCH.md:15-18` |
| 2 | REST API: Job Control | Resume Interrupted Job | Restarts an interrupted or failed job from its last committed batch checkpoint | `POST /api/jobs/{id}/resume` with optional generator override parameters | `202 Accepted` with resumed `SyncJob` JSON (`status: RUNNING`) | `400 Bad Request` if empty ID;<br>`404 Not Found` if job missing;<br>`409 Conflict` if already running or completed;<br>`500` on DB error | `ORIGINAL_REQUEST.md:30`, `PROJECT.md:56` |
| 3 | REST API: Job Query | List Sync Jobs | Lists historical and active sync jobs with progress and change metrics | `GET /api/jobs?limit=20&offset=0&status=RUNNING` | `200 OK` with JSON `{"jobs": [...], "total": N, "limit": 20, "offset": 0}` | `400 Bad Request` if `limit < 1`, `limit > 100`, `offset < 0`, or invalid `status` | `DISPATCH.md:16-17`, `PROJECT.md:55` |
| 4 | REST API: Job Query | Inspect Specific Job | Detailed inspection of a specific sync job including committed batch checkpoints | `GET /api/jobs/{id}` | `200 OK` with JSON `{"job": {...}, "checkpoints": [...]}` | `404 Not Found` with code `JOB_NOT_FOUND` if ID missing | `DISPATCH.md:17`, `pkg/domain/models.go:112` |
| 5 | REST API: Dead Letter | Paginated Issues Inspector | Paginated list of quarantined corrupt records with defect reasons and raw payloads | `GET /api/dead-letter?job_id=...&limit=20&offset=0` | `200 OK` with JSON `{"issues": [...], "total": N, "limit": 20, "offset": 0}` | `400 Bad Request` on invalid pagination params;<br>`500` on query error | `ORIGINAL_REQUEST.md:30`, `PROJECT.md:57` |
| 6 | REST API: Catalog | Product Catalog Explorer | Paginated list of local products with live pricing, status (`ACTIVE`/`WITHDRAWN`), and search | `GET /api/products?limit=20&offset=0&status=ACTIVE&search=PROD` | `200 OK` with JSON `{"products": [...], "total": N, "limit": 20, "offset": 0}` | `400 Bad Request` on invalid status or negative offset | `explorer_m4_1/DISPATCH.md:22`, `PROJECT.md:8` |
| 7 | Real-Time SSE | Event Stream Endpoint | Server-Sent Events stream delivering real-time progress, counters, and dead-letter notices | `GET /api/events` (HTTP `Accept: text/event-stream`) | Continuous stream: `retry: 3000\n\n`, `: ping\n\n`, `event: ...\ndata: {...}\n\n` | `500` if `http.Flusher` not supported | `PROJECT.md:24`, `DISPATCH.md:19-22` |
| 8 | SSE Event Broker | Thread-Safe Event Broker | In-memory pub/sub fanout broker managing client subscriptions, broadcasts, and backpressure | Channel subscribe/unsubscribe/publish actions | Thread-safe non-blocking event distribution | Drops slow client or drops event if client channel buffer full; zero deadlocks | `DISPATCH.md:25`, Go concurrency patterns |
| 9 | SSE Keepalive | Ping Keepalive Mechanism | Automatic periodic transmission of comment lines (`: ping\n\n`) every 15s to prevent timeouts | 15s timer ticker per broker | Comment line `: ping\n\n` | No-op if client disconnected | `DISPATCH.md:22` |
| 10 | Static Asset Serving | Embedded Single-Page Dashboard | Serves vanilla HTML, CSS, and JS embedded directly into Go binary via `embed.FS` | `GET /`, `GET /static/*` | HTML, CSS, JS with correct MIME types and cache headers | `404 Not Found` for missing static files; path traversal attempts blocked | `ORIGINAL_REQUEST.md:29`, `PROJECT.md:23` |
| 11 | Server CLI | Command-Line Execution | Standalone server executable supporting flags `--port`, `--db`, `--static-dir` | CLI invocation: `./server --port 8080 --db catalog.db` | HTTP listening server on configured port | `exit(1)` with descriptive error on invalid port or unreadable DB | `cmd/generator/main.go` pattern, `DISPATCH.md:30` |
| 12 | Server Lifecycle | Graceful Shutdown | Intercepts `SIGINT`/`SIGTERM`, aborts in-flight jobs into `INTERRUPTED`, closes DB cleanly | OS Signal `SIGINT` or `SIGTERM` | Clean shutdown logs, closes listeners within 5s timeout, exits code 0 | Force terminate if timeout exceeded | `DISPATCH.md:30`, `PROJECT.md:28` |
| 13 | Server Lifecycle | Startup Crash Recovery | Scans DB on boot for leftover `RUNNING` jobs and transitions them to `INTERRUPTED` | Server initialization hook | Database update; log of recovered job IDs | Returns error if database is locked or corrupted | `PROJECT.md:17`, `pkg/sync/engine.go:519` |
| 14 | API Standards | Unified Error Schema | Standardized JSON error response format across all API endpoints | Malformed requests or internal errors | `{"error": "message", "code": "CODE"}` | HTTP status matching semantic category (400, 404, 409, 500) | `DISPATCH.md:18` |

---

## 3. Edge Cases & Boundary Behaviors

| # | Feature | Input / Condition | Observed / Specified Behavior |
|---|---------|-------------------|-------------------------------|
| 1 | `POST /api/jobs` | Empty JSON `{}` or omitted request body | Applies default generator configuration (`Seed: 42`, `Batches: 10`, `BatchSize: 100`, standard anomaly ratios); starts job; returns `202 Accepted`. |
| 2 | `POST /api/jobs` | Invalid JSON syntax (e.g. `{bad:json`) | Returns `400 Bad Request` with body `{"error": "malformed JSON request body", "code": "INVALID_REQUEST"}`. |
| 3 | `POST /api/jobs` | Anomaly ratios sum to $> 1.0$ (e.g. `duplicate_ratio: 0.8`, `invalid_ratio: 0.3`) | Returns `400 Bad Request` with body `{"error": "sum of anomaly and update ratios cannot exceed 1.0", "code": "INVALID_REQUEST"}`. |
| 4 | `POST /api/jobs` | Another job is actively `RUNNING` in storage | Returns `409 Conflict` with body `{"error": "a synchronization job is already running: job-xyz", "code": "JOB_CONFLICT"}`. |
| 5 | `POST /api/jobs/{id}/resume` | Target job ID does not exist in storage | Returns `404 Not Found` with body `{"error": "sync job not found: job-nonexistent", "code": "JOB_NOT_FOUND"}`. |
| 6 | `POST /api/jobs/{id}/resume` | Target job has status `COMPLETED` | Returns `409 Conflict` with body `{"error": "job is already completed and cannot be resumed", "code": "JOB_CONFLICT"}`. |
| 7 | `POST /api/jobs/{id}/resume` | Target job is already `RUNNING` | Returns `409 Conflict` with body `{"error": "job job-123 is already running", "code": "JOB_CONFLICT"}`. |
| 8 | `GET /api/dead-letter` | Non-existent `job_id` filter | Returns `200 OK` with `{"issues": [], "total": 0, "limit": 20, "offset": 0}` (empty array, not null). |
| 9 | `GET /api/dead-letter` | Negative `offset` or `limit <= 0` | Returns `400 Bad Request` with code `INVALID_REQUEST` or automatically clamps to `offset=0`, `limit=20`. |
| 10 | `GET /api/events` | Client disconnects abruptly mid-stream | Request context `r.Context().Done()` fires; handler immediately unregisters client from broker, closes subscriber channel, and exits without goroutine leak. |
| 11 | `GET /api/events` | Slow / stalled client with full buffer (32 unconsumed events) | Broker performs non-blocking send with `select { case ch <- msg: default: /* drop or unregister */ }`; prevents slow client from blocking entire server or other subscribers. |
| 12 | `GET /api/events` | Idle stream (no sync jobs running) | Broker keepalive ticker fires every 15s, sending `: ping\n\n`; prevents intermediate proxy/browser HTTP connection drop. |
| 13 | `GET /` & `/static/*` | Requesting `/static/../../etc/passwd` | Go `http.FileServer` sanitizes path; returns `404 Not Found` or `400 Bad Request`; directory traversal is completely blocked. |
| 14 | `cmd/server` | Invalid port (e.g. `--port 75000` or `--port -1`) | `ParseFlags` validates port range `1 <= port <= 65535`; returns descriptive error; `main()` exits with status code 1. |
| 15 | `cmd/server` | SIGINT / SIGTERM received during active job run | Server initiates graceful shutdown: cancels engine context; engine rolls back uncommitted batch; engine updates job status to `INTERRUPTED`; HTTP server drains within 5s; DB closed; exits 0. |

---

## 4. Formal REST Schemas & Contracts

### 4.1 Unified Error Response Schema (`ErrorResponse`)
```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "ErrorResponse",
  "type": "object",
  "properties": {
    "error": { "type": "string", "description": "Human-readable explanation of error" },
    "code": {
      "type": "string",
      "enum": ["INVALID_REQUEST", "JOB_NOT_FOUND", "JOB_CONFLICT", "INTERNAL_ERROR"]
    }
  },
  "required": ["error", "code"]
}
```

### 4.2 Start Job Request & Response (`POST /api/jobs`)
- **Request Headers**: `Content-Type: application/json`
- **Request Body Schema (`JobStartRequest`)**:
```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "JobStartRequest",
  "type": "object",
  "properties": {
    "mode": { "type": "string", "enum": ["generator", "batches"], "default": "generator" },
    "job_id": { "type": "string", "description": "Optional custom job ID. If empty, server generates ID" },
    "seed": { "type": "integer", "minimum": 0, "default": 42 },
    "total_batches": { "type": "integer", "minimum": 0, "default": 10 },
    "batch_size": { "type": "integer", "minimum": 1, "default": 100 },
    "update_ratio": { "type": "number", "minimum": 0.0, "maximum": 1.0, "default": 0.20 },
    "duplicate_ratio": { "type": "number", "minimum": 0.0, "maximum": 1.0, "default": 0.05 },
    "out_of_order_ratio": { "type": "number", "minimum": 0.0, "maximum": 1.0, "default": 0.10 },
    "withdrawn_ratio": { "type": "number", "minimum": 0.0, "maximum": 1.0, "default": 0.05 },
    "invalid_ratio": { "type": "number", "minimum": 0.0, "maximum": 1.0, "default": 0.05 },
    "batches": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "batch_index": { "type": "integer", "minimum": 0 },
          "records": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "sku": { "type": "string" },
                "name": { "type": "string" },
                "price": { "type": "number" },
                "status": { "type": "string", "enum": ["ACTIVE", "WITHDRAWN"] },
                "timestamp": { "type": "string", "format": "date-time" }
              },
              "required": ["sku", "name", "price", "status", "timestamp"]
            }
          }
        },
        "required": ["batch_index", "records"]
      }
    }
  }
}
```
- **Response Status Codes**:
  - `202 Accepted`: Job accepted and running in background.
  - `400 Bad Request`: Validation failure (`INVALID_REQUEST`).
  - `409 Conflict`: Another job is already running (`JOB_CONFLICT`).
  - `500 Internal Server Error`: Storage failure (`INTERNAL_ERROR`).
- **Response Body (`SyncJobResponse`)**:
```json
{
  "job_id": "job-1771678800000",
  "status": "RUNNING",
  "total_batches": 10,
  "committed_batch_index": -1,
  "summary": { "added": 0, "updated": 0, "withdrawn": 0, "skipped": 0, "dead_letter": 0 },
  "created_at": "2026-10-21T13:00:00Z",
  "updated_at": "2026-10-21T13:00:00Z",
  "completed_at": null
}
```

### 4.3 Resume Job (`POST /api/jobs/{id}/resume`)
- **Request Body (Optional)**: `{"seed": 42, "total_batches": 10, "batch_size": 100}`
- **Response Status Codes**:
  - `202 Accepted`: Resume initiated.
  - `400 Bad Request`: Invalid parameters (`INVALID_REQUEST`).
  - `404 Not Found`: Job does not exist (`JOB_NOT_FOUND`).
  - `409 Conflict`: Job is already completed or actively running (`JOB_CONFLICT`).
  - `500 Internal Server Error`: Storage error (`INTERNAL_ERROR`).

### 4.4 List Jobs (`GET /api/jobs`)
- **Query Parameters**:
  - `limit`: `int` (default 20, max 100)
  - `offset`: `int` (default 0, min 0)
  - `status`: `string` (optional, one of `PENDING`, `RUNNING`, `INTERRUPTED`, `COMPLETED`, `FAILED`)
- **Response Body (`JobListResponse`)**:
```json
{
  "jobs": [
    {
      "job_id": "job-1",
      "status": "COMPLETED",
      "total_batches": 10,
      "committed_batch_index": 9,
      "summary": { "added": 800, "updated": 100, "withdrawn": 50, "skipped": 30, "dead_letter": 20 },
      "created_at": "2026-10-21T12:00:00Z",
      "updated_at": "2026-10-21T12:01:00Z",
      "completed_at": "2026-10-21T12:01:00Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

### 4.5 Inspect Job (`GET /api/jobs/{id}`)
- **Response Status**: `200 OK` or `404 Not Found`
- **Response Body**:
```json
{
  "job": {
    "job_id": "job-1",
    "status": "COMPLETED",
    "total_batches": 2,
    "committed_batch_index": 1,
    "summary": { "added": 160, "updated": 20, "withdrawn": 10, "skipped": 6, "dead_letter": 4 },
    "created_at": "2026-10-21T12:00:00Z",
    "updated_at": "2026-10-21T12:00:30Z",
    "completed_at": "2026-10-21T12:00:30Z"
  },
  "checkpoints": [
    {
      "job_id": "job-1",
      "batch_index": 0,
      "items_count": 100,
      "summary": { "added": 80, "updated": 10, "withdrawn": 5, "skipped": 3, "dead_letter": 2 },
      "committed_at": "2026-10-21T12:00:15Z"
    },
    {
      "job_id": "job-1",
      "batch_index": 1,
      "items_count": 100,
      "summary": { "added": 80, "updated": 10, "withdrawn": 5, "skipped": 3, "dead_letter": 2 },
      "committed_at": "2026-10-21T12:00:30Z"
    }
  ]
}
```

### 4.6 Dead-Letter Queue Viewer (`GET /api/dead-letter`)
- **Query Parameters**:
  - `job_id`: `string` (optional, filters issues by specific job)
  - `limit`: `int` (default 20, max 100)
  - `offset`: `int` (default 0)
- **Response Body (`DeadLetterListResponse`)**:
```json
{
  "issues": [
    {
      "issue_id": "dl-job-1-0-12-1771678801",
      "job_id": "job-1",
      "batch_index": 0,
      "sku": "BAD-PRICE-01",
      "raw_payload": "{\"sku\":\"BAD-PRICE-01\",\"name\":\"Bad Price Widget\",\"price\":-5.00,\"status\":\"ACTIVE\",\"timestamp\":\"2026-01-01T00:00:00Z\"}",
      "reason": "price cannot be negative",
      "created_at": "2026-10-21T12:00:01Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

### 4.7 Product Catalog Explorer (`GET /api/products`)
- **Query Parameters**:
  - `limit`: `int` (default 20, max 100)
  - `offset`: `int` (default 0)
  - `status`: `string` (optional: `ACTIVE`, `WITHDRAWN`)
  - `search`: `string` (optional: substring match on SKU or Name)
- **Response Body (`ProductListResponse`)**:
```json
{
  "products": [
    {
      "sku": "PROD-100",
      "name": "Mechanical Keyboard",
      "price": 129.99,
      "status": "ACTIVE",
      "supplier_timestamp": "2026-01-01T00:00:00Z",
      "updated_at": "2026-10-21T12:00:01Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

---

## 5. Formal Server-Sent Events (SSE) Protocol Specification

### 5.1 Connection Handshake & Headers
Upon connection to `GET /api/events`:
1. Server verifies support for `http.Flusher`. If unsupported, returns HTTP 500.
2. Server writes HTTP response headers:
   - `Content-Type: text/event-stream; charset=utf-8`
   - `Cache-Control: no-cache, no-transform`
   - `Connection: keep-alive`
   - `X-Accel-Buffering: no` (disables reverse-proxy buffering for Nginx)
3. Server immediately sends the reconnection retry parameter:
   ```
   retry: 3000\n\n
   ```
4. Server flushes writer and registers the client's event channel with the event broker.

### 5.2 SSE Event Wire Payloads

1. **`event: job_started`**: Emitted when a new sync or resume run starts.
   ```
   event: job_started
   data: {"job_id":"job-101","status":"RUNNING","total_batches":10,"batch_size":100,"created_at":"2026-10-21T13:00:00Z"}
   
   ```

2. **`event: progress`**: Emitted upon each committed batch checkpoint.
   ```
   event: progress
   data: {"job_id":"job-101","batch_index":0,"total_batches":10,"items_count":100,"percent":10.0,"summary":{"added":80,"updated":10,"withdrawn":5,"skipped":3,"dead_letter":2},"batch_delta":{"added":80,"updated":10,"withdrawn":5,"skipped":3,"dead_letter":2},"committed_at":"2026-10-21T13:00:01Z"}
   
   ```

3. **`event: dead_letter`**: Emitted when corrupt records are quarantined.
   ```
   event: dead_letter
   data: {"issue_id":"dl-job-101-0-5-1771678801","job_id":"job-101","batch_index":0,"sku":"EMPTY-SKU","reason":"sku cannot be empty","raw_payload":"{...}","created_at":"2026-10-21T13:00:01Z"}
   
   ```

4. **`event: job_completed`**: Emitted when all batches finish and commit.
   ```
   event: job_completed
   data: {"job_id":"job-101","status":"COMPLETED","total_batches":10,"summary":{"added":800,"updated":100,"withdrawn":50,"skipped":30,"dead_letter":20},"completed_at":"2026-10-21T13:00:10Z"}
   
   ```

5. **`event: job_interrupted`**: Emitted when a job is interrupted by signal or context cancel.
   ```
   event: job_interrupted
   data: {"job_id":"job-101","status":"INTERRUPTED","reason":"server shutdown","committed_batch_index":4,"total_batches":10,"summary":{"added":400,"updated":50,"withdrawn":25,"skipped":15,"dead_letter":10},"interrupted_at":"2026-10-21T13:00:05Z"}
   
   ```

6. **Keepalive Comment**: Emitted periodically every 15s if no events have been sent.
   ```
   : ping\n\n
   ```

### 5.3 Thread-Safe Pub/Sub Broker Architecture
- **Structure**:
  ```go
  type Event struct {
      Type string
      Data any
  }

  type Broker struct {
      clients     map[chan []byte]bool
      subscribe   chan chan []byte
      unsubscribe chan chan []byte
      broadcast   chan []byte
      mu          sync.RWMutex
      pingTicker  *time.Ticker
  }
  ```
- **Backpressure & Non-Blocking Broadcast**:
  Each subscriber channel has capacity 32 (`make(chan []byte, 32)`).
  When broadcasting:
  ```go
  select {
  case ch <- msg:
  default:
      // Drop slow client or drop message to prevent halting broker
  }
  ```
- **Lifecycle Cleanup**:
  When `r.Context().Done()` fires, client is immediately removed from `clients` map and channel is drained.

---

## 6. Comprehensive Automated Test Matrix for `pkg/api` & `cmd/server`

Target Coverage: $\ge 90\%$ statement coverage across `pkg/api` and `cmd/server`.  
Race Detection: `go test -race -count=5 ./pkg/api ./cmd/server` passing with 0 data races.

| Test # | Test File | Test Function | Purpose / Scenarios Tested | Assertions / Invalidation Conditions |
|---|---|---|---|---|
| **T01** | `pkg/api/handlers_test.go` | `TestHandler_PostJob_DefaultConfig` | Trigger sync job with empty JSON `{}` | Assert 202 Accepted, JSON contains valid `job_id`, `status == "RUNNING"`, `total_batches == 10`. |
| **T02** | `pkg/api/handlers_test.go` | `TestHandler_PostJob_CustomGenerator` | Trigger sync job with custom seed, batches=5, batch_size=50 | Assert 202 Accepted, storage creates job with 5 total batches. |
| **T03** | `pkg/api/handlers_test.go` | `TestHandler_PostJob_DirectBatches` | Trigger job by directly posting an array of batches | Assert 202 Accepted, batches are ingested sequentially, catalog updated. |
| **T04** | `pkg/api/handlers_test.go` | `TestHandler_PostJob_InvalidJSON` | POST malformed JSON `{invalid` | Assert 400 Bad Request, `code == "INVALID_REQUEST"`. |
| **T05** | `pkg/api/handlers_test.go` | `TestHandler_PostJob_InvalidRatios` | POST with `duplicate_ratio == 1.5` or sum $> 1.0$ | Assert 400 Bad Request, `code == "INVALID_REQUEST"`. |
| **T06** | `pkg/api/handlers_test.go` | `TestHandler_PostJob_ConflictAlreadyRunning` | POST while another job is actively RUNNING | Assert 409 Conflict, `code == "JOB_CONFLICT"`, body names active job ID. |
| **T07** | `pkg/api/handlers_test.go` | `TestHandler_PostJob_StorageFailure` | Simulate DB failure during job creation | Assert 500 Internal Server Error, `code == "INTERNAL_ERROR"`. |
| **T08** | `pkg/api/handlers_test.go` | `TestHandler_ResumeJob_Success` | Resume an `INTERRUPTED` job with committed index 2 of 5 | Assert 202 Accepted, remaining batches (3, 4) execute to completion (`COMPLETED`). |
| **T09** | `pkg/api/handlers_test.go` | `TestHandler_ResumeJob_NotFound` | Resume non-existent job ID | Assert 404 Not Found, `code == "JOB_NOT_FOUND"`. |
| **T10** | `pkg/api/handlers_test.go` | `TestHandler_ResumeJob_AlreadyCompleted` | Resume a job with `status == "COMPLETED"` | Assert 409 Conflict, `code == "JOB_CONFLICT"`. |
| **T11** | `pkg/api/handlers_test.go` | `TestHandler_ResumeJob_ConflictRunning` | Resume job while another job is active | Assert 409 Conflict, `code == "JOB_CONFLICT"`. |
| **T12** | `pkg/api/handlers_test.go` | `TestHandler_ListJobs_Pagination` | Seed 25 jobs; query `limit=10&offset=10` | Assert 200 OK, returns exactly 10 jobs, `total == 25`, `offset == 10`. |
| **T13** | `pkg/api/handlers_test.go` | `TestHandler_ListJobs_StatusFilter` | Query with `?status=INTERRUPTED` | Assert 200 OK, all returned jobs have `status == "INTERRUPTED"`. |
| **T14** | `pkg/api/handlers_test.go` | `TestHandler_ListJobs_InvalidParams` | Query with `limit=-5` or invalid status `status=UNKNOWN` | Assert 400 Bad Request, `code == "INVALID_REQUEST"`. |
| **T15** | `pkg/api/handlers_test.go` | `TestHandler_GetJob_Success` | Query existing job by ID with 3 committed checkpoints | Assert 200 OK, returns job entity and 3 checkpoints matching DB state. |
| **T16** | `pkg/api/handlers_test.go` | `TestHandler_GetJob_NotFound` | Query missing job ID | Assert 404 Not Found, `code == "JOB_NOT_FOUND"`. |
| **T17** | `pkg/api/handlers_test.go` | `TestHandler_DeadLetter_Pagination` | Seed 30 dead-letter issues; query `limit=15&offset=0` | Assert 200 OK, returns 15 issues, `total == 30`. |
| **T18** | `pkg/api/handlers_test.go` | `TestHandler_DeadLetter_FilterByJobID` | Query `?job_id=job-target` | Assert 200 OK, all issues belong to `job-target`; issues from other jobs excluded. |
| **T19** | `pkg/api/handlers_test.go` | `TestHandler_ListProducts_Pagination` | Seed 50 products; query `limit=20&offset=20` | Assert 200 OK, returns 20 products, `total == 50`. |
| **T20** | `pkg/api/handlers_test.go` | `TestHandler_ListProducts_StatusAndSearch` | Query `?status=WITHDRAWN&search=KEYBOARD` | Assert 200 OK, returns matching products; SQL injection characters sanitized. |
| **T21** | `pkg/api/sse_test.go` | `TestSSE_HeadersAndRetry` | Connect client to `/api/events` via `httptest` | Assert `Content-Type: text/event-stream`, `Connection: keep-alive`, initial payload `retry: 3000\n\n`. |
| **T22** | `pkg/api/sse_test.go` | `TestSSE_BroadcastAllEventTypes` | Publish `job_started`, `progress`, `dead_letter`, `job_completed` | Assert connected client receives all 4 events with matching `event:` and `data:` strings. |
| **T23** | `pkg/api/sse_test.go` | `TestSSE_KeepalivePing` | Connect client with 20ms ping interval; wait 60ms idle | Assert client receives `: ping\n\n` comments at least twice. |
| **T24** | `pkg/api/sse_test.go` | `TestSSE_ClientDisconnect_ZeroLeak` | Connect client, cancel client request context | Assert client removed from broker, subscriber count returns to 0, no goroutine leak. |
| **T25** | `pkg/api/sse_test.go` | `TestSSE_SlowClientBackpressure` | Client does not drain channel; broker sends 100 events | Assert broker does not block or deadlock; drops message or disconnects slow client. |
| **T26** | `pkg/api/sse_test.go` | `TestSSE_MultiSubscriberBroadcast` | 10 concurrent subscribers receive same stream | Assert all 10 clients receive identical sequence of progress events. |
| **T27** | `pkg/api/static_test.go` | `TestStatic_ServeIndexHtml` | GET `/` and GET `/index.html` | Assert 200 OK, `Content-Type: text/html; charset=utf-8`, contains HTML doc tags. |
| **T28** | `pkg/api/static_test.go` | `TestStatic_ServeCSS` | GET `/static/style.css` | Assert 200 OK, `Content-Type: text/css; charset=utf-8`. |
| **T29** | `pkg/api/static_test.go` | `TestStatic_ServeJS` | GET `/static/app.js` | Assert 200 OK, `Content-Type: application/javascript; charset=utf-8` (or `text/javascript`). |
| **T30** | `pkg/api/static_test.go` | `TestStatic_DirectoryTraversalBlocked` | GET `/static/../../etc/passwd` | Assert 404 or 400; path traversal forbidden. |
| **T31** | `cmd/server/main_test.go` | `TestServer_FlagParsing` | Parse valid and invalid flags (`--port`, `--db`, `--static-dir`) | Assert options parsed accurately; invalid ports (`-1`, `70000`) reject with error. |
| **T32** | `cmd/server/main_test.go` | `TestServer_StartupCrashRecovery` | Pre-populate DB with `RUNNING` job; boot server | Assert server invokes `RecoverOnStartup`, job status becomes `INTERRUPTED`. |
| **T33** | `cmd/server/main_test.go` | `TestServer_GracefulShutdown_SIGINT` | Start server, send `SIGINT` | Assert server drains connections, aborts active job cleanly, closes DB, exits 0. |
| **T34** | `cmd/server/main_test.go` | `TestServer_RaceDetector_Stress` | 20 goroutines trigger jobs, stream SSE, list dead letters | `go test -race -count=5 ./pkg/api ./cmd/server` passes with 0 data races. |

---

## 7. Logic Chain

1. **Requirement Mapping**: `ORIGINAL_REQUEST.md` (R4) demands an embedded single-page dashboard allowing staff to trigger updates, observe live progress and change counters, inspect dead-letter records, and resume interrupted runs.
2. **REST API Design**:
   - To trigger jobs, `POST /api/jobs` was specified to accept either generator parameters or direct batches.
   - To enforce the single active sync invariant, `storage.GetActiveJob` was leveraged: if an active job exists, `POST /api/jobs` returns `409 Conflict`.
   - To resume jobs, `POST /api/jobs/{id}/resume` was specified: non-existent jobs return `404 Not Found`, completed jobs return `409 Conflict`, while interrupted jobs resume execution from checkpoint $batch + 1$.
   - To allow inspection, `GET /api/jobs`, `GET /api/jobs/{id}`, `GET /api/dead-letter`, and `GET /api/products` provide paginated, filtered access.
3. **SSE Protocol Design**:
   - Standard browser `EventSource` requires `text/event-stream`, `Cache-Control: no-cache`, and automatic reconnect.
   - Sending `retry: 3000\n\n` immediately guarantees 3-second client reconnection behavior.
   - Sending `: ping\n\n` comments every 15s ensures HTTP proxies and cloud load balancers do not terminate idle SSE connections.
   - Five distinct event types (`job_started`, `progress`, `job_completed`, `job_interrupted`, `dead_letter`) mirror the exact domain lifecycle.
4. **Static Embedding & MIME Types**:
   - Go 1.16+ `//go:embed static/*` bundles the UI into the binary with 0 external dependencies.
   - Because minimal Alpine/scratch Docker containers often lack `/etc/mime.types`, the API package must explicitly call `mime.AddExtensionType` to guarantee valid CSS and JS MIME types.
5. **Test Matrix & Coverage**:
   - To satisfy $\ge 90\%$ test coverage and zero data races, 34 granular test cases were constructed spanning handler unit tests, pub/sub concurrency, static file delivery, and CLI graceful shutdown.

---

## 8. Caveats

1. **Single Writer Database**: `modernc.org/sqlite` with `MaxOpenConns(1)` serializes all writes. Concurrently triggered jobs cannot run simultaneously; `409 Conflict` prevents write contention.
2. **Minimal Container MIME Types**: Default Linux Docker containers may lack system MIME databases. Registering `.css` and `.js` via `mime.AddExtensionType` in `pkg/api/init.go` is mandatory to avoid browsers blocking stylesheet or script execution.
3. **SSE Client Buffering**: Slow network clients must not block the broker's publish loop. Non-blocking channel sends with buffer capacity 32 are required.

---

## 9. Conclusion

The specification for Milestone 4 (`pkg/api`, `cmd/server`, `web`) is fully mined, formalized, and verified against all domain invariants:
- Complete JSON schemas defined for all REST requests, responses, and errors.
- SSE wire format and pub/sub broker formalized with 5 event types, `retry: 3000\n\n`, and 15s keepalive ping.
- Comprehensive 34-scenario automated test matrix defined targeting $\ge 90\%$ statement coverage and zero data races under `-race -count=5`.
- Ready for implementation by Worker M4.

---

## 10. Verification Method

To verify the Milestone 4 specification and future implementation:

1. **Verify Existing Codebase Health**:
   ```bash
   go test -race -count=1 ./...
   ```
2. **Verify Milestone 4 Implementation (once implemented)**:
   ```bash
   # Run all API and Server tests with race detector and count=5
   go test -race -count=5 ./pkg/api/... ./cmd/server/...
   
   # Verify statement coverage >= 90%
   go test -coverprofile=coverage.out ./pkg/api/... ./cmd/server/...
   go tool cover -func=coverage.out
   ```
3. **Verify Static File Serving & MIME Types**:
   ```bash
   curl -I http://localhost:8080/
   curl -I http://localhost:8080/static/style.css
   curl -I http://localhost:8080/static/app.js
   ```
   Assert HTTP 200 and MIME headers `text/html`, `text/css`, `application/javascript`.
4. **Invalidation Conditions**:
   - Any API endpoint returning non-JSON or unstructured error without `{"error":..., "code":...}`.
   - Any race condition detected under `go test -race -count=5`.
   - Test coverage falling below $90\%$.
   - Server failing to transition active job to `INTERRUPTED` upon `SIGINT`/`SIGTERM`.
