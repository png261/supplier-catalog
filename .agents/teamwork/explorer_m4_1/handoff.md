# M4.1 Architecture & Technical Design: REST API, SSE Pub/Sub Broker, and Server Binary

## 1. Observation

Direct observations from examining the codebase, specifications, and project dependencies:

1. **Go Toolchain & Standard Library Routing**:
   - `go.mod:3`: `go 1.25.0` is configured.
   - Go 1.22+ standard library `net/http.ServeMux` natively supports HTTP method matching (e.g. `POST /api/jobs`, `GET /api/jobs/{id}`) and wildcard path parameters (`r.PathValue("id")`), eliminating any need for third-party routing frameworks (such as gorilla/mux, chi, gin, or fiber).
   - Only external runtime dependency is `modernc.org/sqlite v1.36.0` (`go.mod:5`).

2. **Existing Domain Models (`pkg/domain/models.go`)**:
   - `models.go:35-42`: `Product` with `SKU`, `Name`, `Price`, `Status`, `SupplierTimestamp`, `UpdatedAt`.
   - `models.go:77-84`: `ChangeSummary` with `Added`, `Updated`, `Withdrawn`, `Skipped`, `DeadLetter`.
   - `models.go:103-109`: `JobStatus` enums: `PENDING`, `RUNNING`, `INTERRUPTED`, `COMPLETED`, `FAILED`.
   - `models.go:112-121`: `SyncJob` with `JobID`, `Status`, `TotalBatches`, `CommittedBatchIndex`, `Summary`, `CreatedAt`, `UpdatedAt`, `CompletedAt`.
   - `models.go:124-130`: `BatchCheckpoint` with `JobID`, `BatchIndex`, `ItemsCount`, `Summary`, `CommittedAt`.
   - `models.go:133-141`: `DeadLetterRecord` with `IssueID`, `JobID`, `BatchIndex`, `SKU`, `RawPayload`, `Reason`, `CreatedAt`.

3. **Storage Repository Capabilities & Gaps (`pkg/storage/sqlite.go`)**:
   - `sqlite.go:17-39`: `Storage` interface currently defines:
     - `GetProduct`, `GetProductTx`, `UpsertProductTx`
     - `CreateJob`, `GetJob`, `GetActiveJob`, `UpdateJobStatus`
     - `RecordBatchCheckpointTx`, `UpdateJobProgressTx`, `RecoverInterruptedJobs`
     - `RecordDeadLetterTx`, `ListDeadLetterRecords`
   - **Gap**: `Storage` lacks `ListJobs(ctx, limit, offset)` for `GET /api/jobs`.
   - **Gap**: `Storage` lacks `ListProducts(ctx, status, search, limit, offset)` for `GET /api/products`.
   - **Gap**: `Storage` lacks `GetBatchCheckpoints(ctx, jobID)` for detailed inspection under `GET /api/jobs/{id}`.

4. **Synchronization Engine Interface & Hook Gap (`pkg/sync/engine.go`)**:
   - `engine.go:51-56`: `Engine` interface exposes:
     - `ExecuteBatch(ctx context.Context, batch domain.Batch) (domain.ChangeSummary, error)`
     - `RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)`
     - `ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)`
     - `RecoverOnStartup(ctx context.Context) ([]string, error)`
   - `engine.go:69-77`: `SyncEngine` already utilizes functional options (`type Option func(*SyncEngine)`).
   - `engine.go:252-310`: `processBatchTx` commits batch transactions and updates cumulative progress, but does not provide an external hook or listener callback to stream incremental progress deltas in real-time.

5. **Mock Data Generator Interface (`pkg/generator/`)**:
   - `config.go:11-25`: `Config` defines `Seed`, `TotalBatches`, `BatchSize`, `UpdateRatio`, `DuplicateRatio`, `OutOfOrderRatio`, `WithdrawnRatio`, `InvalidRatio`.
   - `config.go:28-43`: `DefaultConfig()` provides calibrated defaults.
   - `generator.go:15-19`: `Generator` interface provides `StreamBatches(ctx, jobID) <-chan domain.Batch`.
   - `reader.go:97-111`: `UnmarshalBatchJSON(data []byte) (domain.Batch, error)`.

6. **Server CLI & Process Lifecycle Conventions (`cmd/generator/main.go`)**:
   - `main.go:37-106`: `ParseFlags(args []string, stderr io.Writer) (*Options, error)` returns structured options and handles standard help/validation errors.
   - `main.go:275-303`: `runMain(args []string, stdout, stderr io.Writer) int` wraps execution with signal handling (`os.Interrupt`, `syscall.SIGTERM`) and clean exit status codes, ensuring deterministic unit/CLI testability.

---

## 2. Logic Chain

From the observations above, the following design decisions and architectural components are derived:

### Step 1: Storage Layer Extensions
- **Observation Reference**: Section 1, Item 3.
- To fulfill `GET /api/jobs` and `GET /api/products`, `pkg/storage` requires two paginated query methods, plus an optional batch checkpoint query method:
  1. `ListJobs(ctx context.Context, limit, offset int) ([]domain.SyncJob, int, error)`
  2. `ListProducts(ctx context.Context, status string, search string, limit, offset int) ([]domain.Product, int, error)`
  3. `GetBatchCheckpoints(ctx context.Context, jobID string) ([]domain.BatchCheckpoint, error)`
- These methods use index-optimized SQL queries (`idx_products_status`, primary key on `sku`, `idx_issues_job_id`) and standard SQLite `LIMIT ? OFFSET ?`.

### Step 2: Non-Invasive Real-Time SyncEngine Hook
- **Observation Reference**: Section 1, Item 4.
- Rather than refactoring `SyncEngine`'s core loop or polling the database from the API layer (which wastes I/O and creates lag), `SyncEngine` should support an optional functional option:
  ```go
  type ProgressHook func(jobID string, batchIndex int, totalBatches int, delta domain.ChangeSummary, cumulative domain.ChangeSummary)

  func WithProgressHook(hook ProgressHook) Option {
      return func(e *SyncEngine) {
          e.progressHook = hook
      }
  }
  ```
- Calling `e.progressHook` at the end of `processBatchTx` guarantees that progress updates are emitted **only after** each batch transaction has committed successfully to SQLite WAL mode.
- Existing callers and tests are completely unaffected because `e.progressHook` defaults to `nil`.

### Step 3: Thread-Safe Pure Go SSE Pub/Sub Broker
- **Observation Reference**: Section 1, Item 1; Section 1, Item 4.
- In accordance with the zero-external-dependencies rule, the SSE broker is designed in pure Go:
  - **Subscriber Management**: A thread-safe registry of client channels (`chan Event`) protected by `sync.RWMutex` (or an actor channel loop).
  - **Backpressure & Slow Consumer Isolation**: Each client channel has a bounded capacity (default 64). During broadcasting, a non-blocking `select` with `default:` drops events for saturated clients, preventing a slow or stalled HTTP connection from blocking the synchronization engine thread.
  - **Connection Keepalive (Heartbeat)**: A background ticker broadcasts an SSE comment `: ping\n\n` every 15 seconds. Per the W3C SSE standard, lines prefixed with `:` are ignored by browser `EventSource` clients while keeping reverse proxies (NGINX, Cloudflare, AWS ALB) from closing idle connections.
  - **Event Formatting**: Serializes to standard W3C text/event-stream syntax:
    ```
    event: <event_type>\n
    data: <json_payload>\n\n
    ```
  - **Reconnection Semantics**: Sends `retry: 3000\n\n` immediately upon client handshake.

### Step 4: REST API Endpoints & Request/Response Contracts
- **Observation Reference**: Section 1, Items 1, 2, 5.
- Endpoints registered on `http.ServeMux`:
  1. `POST /api/jobs`: Trigger sync job with optional generator parameters or direct supplier updates. Validates that no job is currently `RUNNING`/`PENDING` (returns `409 Conflict` if busy). Returns `202 Accepted`.
  2. `POST /api/jobs/{id}/resume`: Resume interrupted job directly from $last\_committed\_batch + 1$. Validates job existence (404), active state (409 if running), and completion state (400 if already completed). Returns `202 Accepted`.
  3. `GET /api/jobs`: Paginated list of past and active sync jobs with progress and change summaries.
  4. `GET /api/jobs/{id}`: Inspect single job with full checkpoint breakdown.
  5. `GET /api/dead-letter`: Paginated list of quarantined corrupt records with error reasons and raw payloads, filterable by `job_id`.
  6. `GET /api/products`: Paginated list of catalog products, filterable by `status` (`ACTIVE`/`WITHDRAWN`) and text `search` (SKU or name).
  7. `GET /api/events`: SSE streaming endpoint (`text/event-stream`).

### Step 5: Standalone Server Binary (`cmd/server/main.go`)
- **Observation Reference**: Section 1, Item 6.
- Implements testable CLI entry point with `ParseFlags` and `runMain`.
- Supports CLI flags:
  - `--port`: HTTP listen port (default `8080`, fallback to `PORT` environment variable).
  - `--db`: SQLite database path (default `data/catalog.db`).
  - `--static-dir`: Optional directory to serve static assets from disk (bypassing embedded assets for rapid frontend development).
- Middleware pipeline: Panic Recovery -> Structured Logging -> CORS -> `http.ServeMux`.
- Server configuration: `WriteTimeout: 0` (required for continuous SSE streaming), `ReadTimeout: 15s`, `IdleTimeout: 60s`.
- Graceful shutdown: Listens for `SIGINT` and `SIGTERM`, drains active HTTP connections within 10s timeout, gracefully shuts down the SSE broker, closes the database, and releases all resources.

---

## 3. Caveats

1. **SSE Streaming & HTTP WriteTimeout**: Standard Go `http.Server.WriteTimeout` terminates connections after the specified duration. For persistent SSE streams, `WriteTimeout` on `http.Server` must either be set to `0` (disabled) or per-request timeouts must be used via `http.ResponseController` (Go 1.20+). We specify `WriteTimeout = 0` with `ReadHeaderTimeout = 5s` and `IdleTimeout = 60s` to prevent slowloris attacks while allowing infinite SSE streaming.
2. **Deterministic Resumption with Mock Generator**: When resuming a generator-driven job via `POST /api/jobs/{id}/resume`, the generator must be re-instantiated with the exact same seed and parameters as the original job. The `SyncEngine` already safely skips batches with `batch.BatchIndex <= job.CommittedBatchIndex`. The API layer stores generator configuration parameters associated with the job ID so the resumed stream exactly matches the uncommitted sequence.
3. **Embedded Assets Coordination**: `web/embed.go` is designed concurrently by Explorer M4.2 (`explorer_m4_2`). The server binary uses a clean adapter function `web.AssetHandler(staticDirOverride string)` that serves from `embed.FS` when `staticDirOverride == ""` and from local disk when specified.

---

## 4. Conclusion & Complete Technical Specification

### 4.1 Storage Layer Proposed Extensions (`pkg/storage/sqlite.go`)

```go
// Add to Storage interface:
type Storage interface {
    // ... existing methods ...

    // Jobs Listing
    ListJobs(ctx context.Context, limit, offset int) ([]domain.SyncJob, int, error)

    // Products Listing & Search
    ListProducts(ctx context.Context, status string, search string, limit, offset int) ([]domain.Product, int, error)

    // Checkpoints Audit
    GetBatchCheckpoints(ctx context.Context, jobID string) ([]domain.BatchCheckpoint, error)
}
```

#### SQL Implementation Details:
```go
func (s *SQLiteStorage) ListJobs(ctx context.Context, limit, offset int) ([]domain.SyncJob, int, error) {
    if limit <= 0 { limit = 20 }
    if limit > 100 { limit = 100 }
    if offset < 0 { offset = 0 }

    var total int
    countQuery := `SELECT count(*) FROM sync_jobs;`
    if err := s.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
        return nil, 0, fmt.Errorf("failed to count sync jobs: %w", err)
    }

    query := `
    SELECT job_id, status, total_batches, committed_batch_index,
           items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
           created_at, updated_at, completed_at
    FROM sync_jobs
    ORDER BY created_at DESC
    LIMIT ? OFFSET ?;
    `
    rows, err := s.db.QueryContext(ctx, query, limit, offset)
    if err != nil {
        return nil, 0, fmt.Errorf("failed to query sync jobs: %w", err)
    }
    defer rows.Close()

    var jobs []domain.SyncJob
    for rows.Next() {
        var j domain.SyncJob
        var statusStr, createdStr, updatedStr string
        var completedStr sql.NullString

        if err := rows.Scan(
            &j.JobID, &statusStr, &j.TotalBatches, &j.CommittedBatchIndex,
            &j.Summary.Added, &j.Summary.Updated, &j.Summary.Withdrawn,
            &j.Summary.Skipped, &j.Summary.DeadLetter,
            &createdStr, &updatedStr, &completedStr,
        ); err != nil {
            return nil, 0, fmt.Errorf("failed to scan sync job row: %w", err)
        }
        j.Status = domain.JobStatus(statusStr)
        j.CreatedAt, _ = ParseTime(createdStr)
        j.UpdatedAt, _ = ParseTime(updatedStr)
        if completedStr.Valid && completedStr.String != "" {
            ct, err := ParseTime(completedStr.String)
            if err == nil { j.CompletedAt = &ct }
        }
        jobs = append(jobs, j)
    }
    return jobs, total, rows.Err()
}

func (s *SQLiteStorage) ListProducts(ctx context.Context, status string, search string, limit, offset int) ([]domain.Product, int, error) {
    if limit <= 0 { limit = 50 }
    if limit > 100 { limit = 100 }
    if offset < 0 { offset = 0 }

    var conditions []string
    var args []any

    if status != "" {
        conditions = append(conditions, "status = ?")
        args = append(args, status)
    }
    if search != "" {
        conditions = append(conditions, "(sku LIKE ? OR name LIKE ?)")
        searchPattern := "%" + search + "%"
        args = append(args, searchPattern, searchPattern)
    }

    whereClause := ""
    if len(conditions) > 0 {
        whereClause = "WHERE " + strings.Join(conditions, " AND ")
    }

    countQuery := fmt.Sprintf("SELECT count(*) FROM products %s;", whereClause)
    var total int
    if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
        return nil, 0, fmt.Errorf("failed to count products: %w", err)
    }

    selectQuery := fmt.Sprintf(`
    SELECT sku, name, price, status, supplier_timestamp, updated_at
    FROM products %s
    ORDER BY sku ASC
    LIMIT ? OFFSET ?;`, whereClause)

    selectArgs := append(append([]any{}, args...), limit, offset)
    rows, err := s.db.QueryContext(ctx, selectQuery, selectArgs...)
    if err != nil {
        return nil, 0, fmt.Errorf("failed to list products: %w", err)
    }
    defer rows.Close()

    var products []domain.Product
    for rows.Next() {
        var p domain.Product
        var statusStr, supTimeStr, upTimeStr string
        if err := rows.Scan(&p.SKU, &p.Name, &p.Price, &statusStr, &supTimeStr, &upTimeStr); err != nil {
            return nil, 0, fmt.Errorf("failed scanning product row: %w", err)
        }
        p.Status = domain.ProductStatus(statusStr)
        p.SupplierTimestamp, _ = ParseTime(supTimeStr)
        p.UpdatedAt, _ = ParseTime(upTimeStr)
        products = append(products, p)
    }
    return products, total, rows.Err()
}

func (s *SQLiteStorage) GetBatchCheckpoints(ctx context.Context, jobID string) ([]domain.BatchCheckpoint, error) {
    query := `
    SELECT job_id, batch_index, items_count, added_count, updated_count, withdrawn_count, skipped_count, dead_letter_count, committed_at
    FROM batch_checkpoints
    WHERE job_id = ?
    ORDER BY batch_index ASC;
    `
    rows, err := s.db.QueryContext(ctx, query, jobID)
    if err != nil {
        return nil, fmt.Errorf("failed to query batch checkpoints: %w", err)
    }
    defer rows.Close()

    var checkpoints []domain.BatchCheckpoint
    for rows.Next() {
        var cp domain.BatchCheckpoint
        var timeStr string
        if err := rows.Scan(
            &cp.JobID, &cp.BatchIndex, &cp.ItemsCount,
            &cp.Summary.Added, &cp.Summary.Updated, &cp.Summary.Withdrawn,
            &cp.Summary.Skipped, &cp.Summary.DeadLetter,
            &timeStr,
        ); err != nil {
            return nil, fmt.Errorf("failed scanning checkpoint: %w", err)
        }
        cp.CommittedAt, _ = ParseTime(timeStr)
        checkpoints = append(checkpoints, cp)
    }
    return checkpoints, rows.Err()
}
```

---

### 4.2 SyncEngine Functional Option (`pkg/sync/engine.go`)

```go
// ProgressHook receives incremental and cumulative change metrics after each batch commits.
type ProgressHook func(jobID string, batchIndex int, totalBatches int, delta domain.ChangeSummary, cumulative domain.ChangeSummary)

// Add progressHook field to SyncEngine:
type SyncEngine struct {
    storage      Storage
    mu           stdsync.Mutex
    now          func() time.Time
    activeJobs   map[string]bool
    progressHook ProgressHook // nil by default
}

// WithProgressHook attaches an event callback to SyncEngine.
func WithProgressHook(hook ProgressHook) Option {
    return func(e *SyncEngine) {
        e.progressHook = hook
    }
}
```
In `processBatchTx` (after `tx.Commit()`):
```go
    // 5. Update in-memory job state only after successful commit
    job.CommittedBatchIndex = batch.BatchIndex
    job.Summary.Add(summary)
    job.UpdatedAt = e.now()

    if e.progressHook != nil {
        e.progressHook(job.JobID, batch.BatchIndex, job.TotalBatches, summary, job.Summary)
    }
```

---

### 4.3 Pure Go Thread-Safe SSE Pub/Sub Broker (`pkg/api/broker.go`)

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event represents an SSE event frame or keepalive comment.
type Event struct {
	Type      string    `json:"type,omitempty"`
	JobID     string    `json:"job_id,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Data      any       `json:"data,omitempty"`
	Comment   string    `json:"-"` // Non-empty triggers comment frame: ": ping\n\n"
}

// Client represents an active HTTP SSE connection.
type Client struct {
	id       string
	messages chan Event
	done     chan struct{}
}

// Broker coordinates event fan-out to connected clients with backpressure handling.
type Broker struct {
	mu          sync.RWMutex
	clients     map[string]*Client
	broadcast   chan Event
	register    chan *Client
	unregister  chan *Client
	quit        chan struct{}
	pingTicker  *time.Ticker
	clientCap   int
}

// NewBroker initializes a thread-safe SSE broker.
func NewBroker() *Broker {
	return &Broker{
		clients:    make(map[string]*Client),
		broadcast:  make(chan Event, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		quit:       make(chan struct{}),
		pingTicker: time.NewTicker(15 * time.Second),
		clientCap:  64,
	}
}

// Start begins the central event dispatch loop.
func (b *Broker) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			b.Close()
			return
		case <-b.quit:
			return
		case client := <-b.register:
			b.mu.Lock()
			b.clients[client.id] = client
			b.mu.Unlock()
		case client := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[client.id]; ok {
				delete(b.clients, client.id)
				close(client.messages)
			}
			b.mu.Unlock()
		case evt := <-b.broadcast:
			b.mu.RLock()
			for _, client := range b.clients {
				select {
				case client.messages <- evt:
				default:
					// Backpressure: client buffer full -> drop frame to avoid blocking broker
				}
			}
			b.mu.RUnlock()
		case <-b.pingTicker.C:
			// Heartbeat comment frame
			ping := Event{Comment: "ping"}
			b.mu.RLock()
			for _, client := range b.clients {
				select {
				case client.messages <- ping:
				default:
				}
			}
			b.mu.RUnlock()
		}
	}
}

// Publish broadcasts an event to all connected clients.
func (b *Broker) Publish(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	select {
	case b.broadcast <- evt:
	case <-b.quit:
	}
}

// Close gracefully terminates the broker and disconnects clients.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.quit:
		return
	default:
		close(b.quit)
	}

	b.pingTicker.Stop()
	for id, client := range b.clients {
		close(client.messages)
		delete(b.clients, id)
	}
}

// ServeHTTP handles HTTP SSE streaming connections.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"Streaming unsupported","code":"STREAMING_UNSUPPORTED"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// W3C reconnect interval instruction: retry in 3000ms
	fmt.Fprintf(w, "retry: 3000\n\n")
	flusher.Flush()

	client := &Client{
		id:       fmt.Sprintf("client-%d", time.Now().UnixNano()),
		messages: make(chan Event, b.clientCap),
		done:     make(chan struct{}),
	}

	select {
	case b.register <- client:
	case <-b.quit:
		return
	}

	defer func() {
		select {
		case b.unregister <- client:
		case <-b.quit:
		}
	}()

	notify := r.Context().Done()

	for {
		select {
		case <-notify:
			return
		case <-b.quit:
			return
		case evt, ok := <-client.messages:
			if !ok {
				return
			}
			if evt.Comment != "" {
				fmt.Fprintf(w, ": %s\n\n", evt.Comment)
			} else {
				dataBytes, err := json.Marshal(evt.Data)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, string(dataBytes))
			}
			flusher.Flush()
		}
	}
}
```

---

### 4.4 REST API Endpoints Specification (`pkg/api/handler.go`)

#### 1. Route Table
| Method | Route | Description | Success Code | Error Codes |
|---|---|---|---|---|
| `POST` | `/api/jobs` | Trigger synchronization job (mock generator or direct batch payload) | `202 Accepted` | `400`, `409` |
| `POST` | `/api/jobs/{id}/resume` | Resume interrupted sync job from uncommitted batch | `202 Accepted` | `400`, `404`, `409` |
| `GET` | `/api/jobs` | Paginated list of sync jobs | `200 OK` | `500` |
| `GET` | `/api/jobs/{id}` | Detailed job inspection with checkpoints | `200 OK` | `404`, `500` |
| `GET` | `/api/dead-letter` | Paginated dead-letter corrupt records | `200 OK` | `500` |
| `GET` | `/api/products` | Paginated catalog products with search | `200 OK` | `500` |
| `GET` | `/api/events` | Server-Sent Events (SSE) live updates | `200 OK` | `500` |

#### 2. REST Handler Implementation Sketch
```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/generator"
	"coderpush/pkg/storage"
	"coderpush/pkg/sync"
)

type Storage interface {
	storage.Storage
	ListJobs(ctx context.Context, limit, offset int) ([]domain.SyncJob, int, error)
	ListProducts(ctx context.Context, status string, search string, limit, offset int) ([]domain.Product, int, error)
	GetBatchCheckpoints(ctx context.Context, jobID string) ([]domain.BatchCheckpoint, error)
}

type Engine interface {
	RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
	ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
}

type Handler struct {
	storage       Storage
	engine        Engine
	broker        *Broker
	jobMu         sync.Mutex
	activeJobID   string
	activeCancel  context.CancelFunc
	activeConfigs map[string]generator.Config // For deterministic resumption
}

func NewHandler(store Storage, eng Engine, broker *Broker) *Handler {
	return &Handler{
		storage:       store,
		engine:        eng,
		broker:        broker,
		activeConfigs: make(map[string]generator.Config),
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/jobs", h.handleStartJob)
	mux.HandleFunc("POST /api/jobs/{id}/resume", h.handleResumeJob)
	mux.HandleFunc("GET /api/jobs", h.handleListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", h.handleGetJob)
	mux.HandleFunc("GET /api/dead-letter", h.handleListDeadLetter)
	mux.HandleFunc("GET /api/products", h.handleListProducts)
	mux.HandleFunc("GET /api/events", h.broker.ServeHTTP)
	return mux
}
```

#### 3. Endpoint Specifications & Request/Response Payloads

##### A. `POST /api/jobs`
- **Request Body**:
  ```json
  {
    "type": "generator",
    "generator": {
      "seed": 42,
      "total_batches": 10,
      "batch_size": 100,
      "update_ratio": 0.20,
      "duplicate_ratio": 0.05,
      "out_of_order_ratio": 0.10,
      "withdrawn_ratio": 0.05,
      "invalid_ratio": 0.05
    }
  }
  ```
  *(Or empty body `{}` to trigger default generator)*
- **Response `202 Accepted`**:
  ```json
  {
    "job_id": "job-1729515900",
    "status": "RUNNING",
    "total_batches": 10,
    "created_at": "2026-10-21T13:00:00Z"
  }
  ```
- **Error `409 Conflict`**:
  ```json
  {
    "error": "A synchronization job is already running",
    "code": "JOB_ALREADY_RUNNING",
    "active_job_id": "job-1729515800"
  }
  ```

##### B. `POST /api/jobs/{id}/resume`
- **Request URL**: `/api/jobs/job-1729515900/resume`
- **Request Body**: (Optional generator configuration override)
- **Response `202 Accepted`**:
  ```json
  {
    "job_id": "job-1729515900",
    "status": "RUNNING",
    "resumed_from_batch": 4,
    "total_batches": 10
  }
  ```
- **Error `400 Bad Request`**: Job is already completed.
- **Error `404 Not Found`**: Job does not exist.
- **Error `409 Conflict`**: Another job is actively running.

##### C. `GET /api/jobs`
- **Query Parameters**: `limit` (default 20, max 100), `offset` (default 0).
- **Response `200 OK`**:
  ```json
  {
    "jobs": [
      {
        "job_id": "job-1729515900",
        "status": "COMPLETED",
        "total_batches": 10,
        "committed_batch_index": 9,
        "summary": {
          "added": 620,
          "updated": 190,
          "withdrawn": 45,
          "skipped": 145,
          "dead_letter": 50
        },
        "created_at": "2026-10-21T13:00:00Z",
        "updated_at": "2026-10-21T13:00:04Z",
        "completed_at": "2026-10-21T13:00:04Z"
      }
    ],
    "total": 1,
    "limit": 20,
    "offset": 0
  }
  ```

##### D. `GET /api/jobs/{id}`
- **Response `200 OK`**:
  ```json
  {
    "job": {
      "job_id": "job-1729515900",
      "status": "COMPLETED",
      "total_batches": 10,
      "committed_batch_index": 9,
      "summary": {
        "added": 620,
        "updated": 190,
        "withdrawn": 45,
        "skipped": 145,
        "dead_letter": 50
      },
      "created_at": "2026-10-21T13:00:00Z",
      "updated_at": "2026-10-21T13:00:04Z",
      "completed_at": "2026-10-21T13:00:04Z"
    },
    "checkpoints": [
      {
        "job_id": "job-1729515900",
        "batch_index": 0,
        "items_count": 100,
        "summary": {
          "added": 75,
          "updated": 10,
          "withdrawn": 5,
          "skipped": 5,
          "dead_letter": 5
        },
        "committed_at": "2026-10-21T13:00:01Z"
      }
    ]
  }
  ```

##### E. `GET /api/dead-letter`
- **Query Parameters**: `job_id` (optional), `limit` (default 50, max 100), `offset` (default 0).
- **Response `200 OK`**:
  ```json
  {
    "issues": [
      {
        "issue_id": "dl-job-1729515900-0-3-1729515901000",
        "job_id": "job-1729515900",
        "batch_index": 0,
        "sku": "SKU-INV-00042",
        "raw_payload": "{\"sku\":\"\",\"name\":\"Corrupt Product\",\"price\":-10,\"status\":\"ACTIVE\",\"timestamp\":\"2026-01-01T00:00:00Z\"}",
        "reason": "empty SKU; negative price: -10.000000",
        "created_at": "2026-10-21T13:00:01Z"
      }
    ],
    "total": 1,
    "limit": 50,
    "offset": 0
  }
  ```

##### F. `GET /api/products`
- **Query Parameters**: `status` (optional `ACTIVE`, `WITHDRAWN`), `search` (optional substring query), `limit` (default 50, max 100), `offset` (default 0).
- **Response `200 OK`**:
  ```json
  {
    "products": [
      {
        "sku": "SKU-00000001",
        "name": "Pro Keyboard",
        "price": 99.50,
        "status": "ACTIVE",
        "supplier_timestamp": "2026-01-01T00:00:01Z",
        "updated_at": "2026-10-21T13:00:01Z"
      }
    ],
    "total": 1,
    "limit": 50,
    "offset": 0
  }
  ```

---

### 4.5 Server-Sent Events (SSE) Protocol Details

1. **Stream Handshake**:
   - Client connects: `GET /api/events HTTP/1.1`
   - Server responds:
     ```http
     HTTP/1.1 200 OK
     Content-Type: text/event-stream
     Cache-Control: no-cache, no-transform
     Connection: keep-alive
     X-Accel-Buffering: no
     Access-Control-Allow-Origin: *

     retry: 3000\n\n
     ```
2. **Event Types & Payloads**:
   - `job_started`:
     ```
     event: job_started
     data: {"job_id":"job-1729515900","total_batches":10,"status":"RUNNING","timestamp":"2026-10-21T13:00:00Z"}
     ```
   - `progress`:
     ```
     event: progress
     data: {"job_id":"job-1729515900","batch_index":0,"total_batches":10,"percent":10.0,"batch_delta":{"added":75,"updated":10,"withdrawn":5,"skipped":5,"dead_letter":5},"cumulative_summary":{"added":75,"updated":10,"withdrawn":5,"skipped":5,"dead_letter":5}}
     ```
   - `dead_letter`:
     ```
     event: dead_letter
     data: {"job_id":"job-1729515900","batch_index":0,"new_issues":5,"total_dead_letter":5}
     ```
   - `job_completed`:
     ```
     event: job_completed
     data: {"job_id":"job-1729515900","status":"COMPLETED","total_batches":10,"summary":{"added":620,"updated":190,"withdrawn":45,"skipped":145,"dead_letter":50},"completed_at":"2026-10-21T13:00:04Z"}
     ```
   - `job_interrupted`:
     ```
     event: job_interrupted
     data: {"job_id":"job-1729515900","status":"INTERRUPTED","last_committed_batch":4,"error":"context canceled"}
     ```
   - `ping` (Heartbeat every 15s):
     ```
     : ping\n\n
     ```

---

### 4.6 Standalone Server Binary Architecture (`cmd/server/main.go`)

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"coderpush/pkg/api"
	"coderpush/pkg/storage"
	"coderpush/pkg/sync"
	"coderpush/web"
)

type ServerOptions struct {
	Port      int
	DBPath    string
	StaticDir string
}

func ParseFlags(args []string, stderr io.Writer) (*ServerOptions, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultPort := 8080
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			defaultPort = p
		}
	}

	opts := &ServerOptions{}
	fs.IntVar(&opts.Port, "port", defaultPort, "HTTP server listen port (default 8080 or $PORT)")
	fs.StringVar(&opts.DBPath, "db", "catalog.db", "SQLite database file path (or :memory:)")
	fs.StringVar(&opts.StaticDir, "static-dir", "", "Optional disk directory for static assets (overrides embedded files)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		return nil, fmt.Errorf("invalid --port: %d (must be 1-65535)", opts.Port)
	}
	return opts, nil
}

func runMain(args []string, stdout, stderr io.Writer) int {
	opts, err := ParseFlags(args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error parsing flags: %v\n", err)
		return 2
	}

	if opts.DBPath != ":memory:" {
		if dir := filepath.Dir(opts.DBPath); dir != "." && dir != "" {
			_ = os.MkdirAll(dir, 0755)
		}
	}

	store, err := storage.Open(opts.DBPath)
	if err != nil {
		fmt.Fprintf(stderr, "Failed to initialize storage: %v\n", err)
		return 1
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Startup Crash Recovery
	recovered, err := store.RecoverInterruptedJobs(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Startup recovery error: %v\n", err)
	} else if len(recovered) > 0 {
		fmt.Fprintf(stdout, "[server] startup recovery marked %d interrupted jobs: %v\n", len(recovered), recovered)
	}

	// Initialize SSE Broker
	broker := api.NewBroker()
	go broker.Start(ctx)
	defer broker.Close()

	// Progress Hook -> Broker
	hook := func(jobID string, batchIndex, totalBatches int, delta, cumulative domain.ChangeSummary) {
		pct := 0.0
		if totalBatches > 0 {
			pct = float64(batchIndex+1) / float64(totalBatches) * 100.0
		}
		broker.Publish(api.Event{
			Type:  "progress",
			JobID: jobID,
			Data: map[string]any{
				"job_id":             jobID,
				"batch_index":        batchIndex,
				"total_batches":      totalBatches,
				"percent":            pct,
				"batch_delta":        delta,
				"cumulative_summary": cumulative,
			},
		})
		if delta.DeadLetter > 0 {
			broker.Publish(api.Event{
				Type:  "dead_letter",
				JobID: jobID,
				Data: map[string]any{
					"job_id":            jobID,
					"batch_index":       batchIndex,
					"new_issues":        delta.DeadLetter,
					"total_dead_letter": cumulative.DeadLetter,
				},
			})
		}
	}

	engine := sync.NewEngine(store, sync.WithProgressHook(hook))
	apiHandler := api.NewHandler(store, engine, broker)

	// Root Router combining API and Frontend Assets
	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", apiHandler.Routes())
	rootMux.Handle("/", web.AssetHandler(opts.StaticDir))

	middlewareStack := api.Recoverer(api.Logging(api.CORS(rootMux)))

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", opts.Port),
		Handler:           middlewareStack,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0, // Required for persistent SSE connections!
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		fmt.Fprintf(stdout, "[server] listening on http://localhost:%d\n", opts.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		fmt.Fprintf(stderr, "HTTP server error: %v\n", err)
		return 1
	case <-ctx.Done():
		fmt.Fprintf(stdout, "\n[server] shutting down gracefully...\n")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(stderr, "Graceful shutdown failed: %v\n", err)
		return 1
	}

	return 0
}

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}
```

---

### 4.7 Middleware Suite (`pkg/api/middleware.go`)

```go
package api

import (
	"log"
	"net/http"
	"time"
)

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if r.URL.Path != "/api/events" { // Do not log continuous SSE streaming iterations
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
		}
	})
}

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[PANIC RECOVER] %v", rec)
				http.Error(w, `{"error":"Internal Server Error","code":"INTERNAL_ERROR"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *statusResponseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *statusResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
```

---

## 5. Verification Method

To independently verify this design during implementation:

1. **Storage Layer Extension Tests (`pkg/storage/sqlite_test.go`)**:
   - `TestSQLiteStorage_ListJobs_Pagination`: Insert 25 jobs, query with limit 10, offset 0; offset 10; offset 20. Verify total count = 25 and descending order.
   - `TestSQLiteStorage_ListProducts_FilterAndSearch`: Insert active, withdrawn, and various SKUs. Verify filtering by status (`ACTIVE`, `WITHDRAWN`) and text search matching SKU prefix or name.
   - `TestSQLiteStorage_GetBatchCheckpoints`: Verify checkpoints are returned in ascending `batch_index` order.

2. **SSE Broker Concurrency & Backpressure Tests (`pkg/api/broker_test.go`)**:
   - `TestBroker_ConnectDisconnect`: Connect 10 clients, disconnect 5, assert clean channel closure and map cleanup.
   - `TestBroker_Backpressure`: Fill client channel buffer, trigger 100 broadcasts, verify broker does not block and publishes without deadlock.
   - `TestBroker_HeartbeatPing`: Verify comment `: ping\n\n` is emitted on schedule.
   - `TestBroker_RaceDetection`: Execute 50 concurrent publishers and 50 concurrent subscribers under `go test -race -count=5 ./pkg/api`.

3. **REST Handlers Unit Tests (`pkg/api/handler_test.go`)**:
   - `TestHandler_StartJob_SuccessAndConflict`: Test `POST /api/jobs` returns 202; second immediate call returns 409 Conflict.
   - `TestHandler_ResumeJob_Scenarios`:
     - Unknown ID returns 404.
     - Completed job returns 400.
     - Interrupted job resumes and returns 202.
   - `TestHandler_ListJobs_And_Inspect`: Test `GET /api/jobs` and `GET /api/jobs/{id}`.
   - `TestHandler_DeadLetter_Pagination`: Test `GET /api/dead-letter` with `job_id` query parameter.
   - `TestHandler_Products_Pagination`: Test `GET /api/products` with `status=ACTIVE&search=Pro`.
   - `TestHandler_SSE_Stream`: Connect HTTP client, receive `retry: 3000\n\n`, trigger job, verify `job_started`, `progress`, and `job_completed` events.

4. **Server Binary CLI & Shutdown Tests (`cmd/server/main_test.go`)**:
   - `TestServer_ParseFlags`: Validate `--port`, `--db`, `--static-dir` parsing and rejection of invalid values.
   - `TestServer_GracefulShutdown`: Start server on ephemeral port (e.g. 0 or available port), send SIGTERM, verify exit code 0.

5. **Statement Coverage & Race Verification Command**:
   ```bash
   go test -race -v -coverprofile=coverage_m4.out ./pkg/api ./cmd/server
   go tool cover -func=coverage_m4.out | grep total
   # Target: total coverage >= 90.0%
   ```
