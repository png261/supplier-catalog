# Milestone 1 Exploration Report: Domain & SQLite Storage Blueprints

## 1. Observation

### 1.1 Toolchain & Runtime Environment
- **Operating System & Arch**: macOS Darwin `arm64` (Apple Silicon).
- **Go Toolchain**: `go version go1.25.6 darwin/arm64`.
- **CGO Configuration**: Mandated pure Go (`CGO_ENABLED=0`) per `PROJECT.md` Architecture (line 4) and Docker Packaging specification (lines 29–30).
- **Disk Availability**: Primary APFS volume `/System/Volumes/Data` has approximately 687 MiB free disk space. Intermediate cache growth must be carefully controlled (`go clean -cache`).

### 1.2 Target Deliverables & Contract Constraints
Per `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_1/DISPATCH.md`:
1. `go.mod` initialization (`module coderpush`, Go 1.25 compatibility, dependency `modernc.org/sqlite@v1.36.0`).
2. Domain entities in `pkg/domain/models.go`:
   - `Product` (`SKU`, `Name`, `Price`, `Status`, `SupplierTimestamp`, `UpdatedAt`).
   - `SupplierUpdate` (`SKU`, `Name`, `Price`, `Status`, `Timestamp`).
   - `Batch` (`JobID`, `BatchIndex`, `Records`).
   - `ChangeSummary` (`Added`, `Updated`, `Withdrawn`, `Skipped`, `DeadLetter`).
   - Forward-compatible types for `JobStatus`, `SyncJob`, `BatchCheckpoint`, `DeadLetterRecord`.
3. SQLite storage engine in `pkg/storage/sqlite.go`:
   - DDL schema initialization for `products` (and audit tables).
   - WAL mode (`PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;`).
   - Connection pooling settings (`SetMaxOpenConns(1)` for single-writer write-safety).
   - `GetProduct`, `GetProductTx`, and `UpsertProductTx` transactional methods.
   - Comprehensive implementation of the `storage.Storage` interface contract from `PROJECT.md` (lines 131–162).

### 1.3 Empirical Driver Inspection (`modernc.org/sqlite@v1.36.0`)
Direct source inspection of `/Users/png/go/pkg/mod/modernc.org/sqlite@v1.36.0/sqlite.go` revealed:
- **Driver Name**: Registered as `"sqlite"` via `sql.Register("sqlite", &Driver{})`.
- **DSN Pragma Handling (lines 894–917)**: The driver parses `_pragma` query parameters and automatically runs `busy_timeout` first, followed by remaining pragmas in sorted order:
  `file:catalog.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)`
- **Transaction Locking Mode (lines 1990–1992)**: Driver supports `_txlock=immediate` which defaults all `tx, err := db.Begin()` / `db.BeginTx()` calls to SQLite `BEGIN IMMEDIATE`, reserving the write lock upfront and preventing lock upgrade deadlocks.
- **Pure Go Execution**: 100% CGO-free, verified with `CGO_ENABLED=0`, compiles cleanly to a static binary of ~6.1 MB.

---

## 2. Logic Chain

### 2.1 Dependency Specification (`go.mod`)
- *Observation*: Go version is `1.25.6`. `modernc.org/sqlite@latest` (v1.60.x) requires Go toolchain `>= 1.26.0`, whereas `v1.36.0` specifies `go 1.21` in its `go.mod` and compiles cleanly on Go 1.25.x without attempting toolchain toolchain auto-downloads.
- *Logic*: Explicitly pin `modernc.org/sqlite v1.36.0` in `go.mod` with Go directive `go 1.25.0`.

### 2.2 Domain Entities Architecture (`pkg/domain/models.go`)
- *Observation*: Invariants A (Idempotency), B (Fast-forward timestamps), C (Tombstone immutability), and D (Dead-letter isolation) require strict validation and comparison primitives.
- *Logic*:
  1. `ProductStatus`: Typed string enum (`ACTIVE`, `WITHDRAWN`). Monotonic transition rule dictates that `WITHDRAWN` is an absorbing terminal state.
  2. `SupplierUpdate.Validate()`: Self-validating method enforcing Invariant D preconditions (non-empty trimmed SKU, non-negative price, valid status enum, non-zero timestamp).
  3. `ChangeSummary`: Struct with `.Add(other ChangeSummary)` helper method to aggregate per-batch metrics into job cumulative counters with zero boilerplate.
  4. Forward-compatible structs (`SyncJob`, `BatchCheckpoint`, `DeadLetterRecord`) defined immediately to eliminate circular type dependencies between M1 storage interfaces and M2/M4 engines.

### 2.3 SQLite Storage Engine Architecture (`pkg/storage/sqlite.go`)
- *Observation*: SQLite WAL mode supports concurrent readers but strictly serializes writes to a single thread/connection. Concurrent write attempts can trigger `SQLITE_BUSY` (database is locked).
- *Logic*:
  1. **Connection Pooling**: Set `db.SetMaxOpenConns(1)` and `db.SetMaxIdleConns(1)`. This forces Go's connection pool to serialize all database operations through a single shared connection handle. This eliminates all `SQLITE_BUSY` contention in-process, guarantees safety across goroutines, and enables multi-iteration tests (`go test -race -count=5 ./...`) to pass with zero data races.
  2. **Pragmas**:
     - `PRAGMA journal_mode = WAL;` — Write-Ahead Logging allows concurrent reading without blocking.
     - `PRAGMA busy_timeout = 5000;` — 5-second wait before failing on locks.
     - `PRAGMA foreign_keys = ON;` — Cascading deletes for checkpoints/issues.
     - `PRAGMA synchronous = NORMAL;` — Optimal balance between durability and throughput in WAL mode.
     *(Note: In memory mode `file::memory:`, journal_mode stays `memory` without error).*
  3. **DDL Schema**:
     - `products`: Primary key `sku`, check constraint on `status IN ('ACTIVE', 'WITHDRAWN')`, ISO-8601/RFC3339 text timestamps stored in UTC.
     - `sync_jobs`, `batch_checkpoints`, `dead_letter_issues`: Initialized on startup so that M1, M2, and M4 operate on a uniform schema.
  4. **Transactional Operations (`GetProductTx` and `UpsertProductTx`)**:
     - Because each batch executes inside an atomic `*sql.Tx`, `GetProductTx(ctx, tx, sku)` is required to read uncommitted mutations within the same batch (handling intra-batch duplicate SKUs).
     - `UpsertProductTx` uses SQLite 3.24+ `INSERT INTO products (...) VALUES (...) ON CONFLICT(sku) DO UPDATE SET ...`, guaranteeing atomic upsert without race conditions.

---

## 3. Caveats

1. **In-Memory Testing vs File-Backed SQLite**:
   When using `:memory:` without `cache=shared`, each connection creates a separate ephemeral database. With `SetMaxOpenConns(1)`, `:memory:` persists for the lifetime of `*SQLiteStorage`. For multi-connection tests or isolated test suites, `file:<name>?mode=memory&cache=shared` or unique temp files must be used.
2. **Timestamp Precision & String Comparison**:
   SQLite stores timestamps as `TEXT`. All timestamps written to SQLite must be normalized to `.UTC().Format(time.RFC3339Nano)`. This guarantees exact nanosecond fidelity and lexicographical sortability in SQL queries (`ORDER BY created_at DESC`).
3. **Price Field Float vs Cents**:
   `domain.Product` specifies `Price float64`. Float values in SQLite `REAL` column must be verified to prevent NaN/Infinity. `SupplierUpdate.Validate()` enforces `Price >= 0.0` and rejects negative values to dead-letter queue.

---

## 4. Conclusion & Code Blueprints

### 4.1 Blueprint: `go.mod`
File path: `/Users/png/01. Project/coderpush/go.mod`

```go
module coderpush

go 1.25.0

require modernc.org/sqlite v1.36.0

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp v0.0.0-20230315142452-642cacee5cc0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	modernc.org/libc v1.61.13 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.8.2 // indirect
)
```

---

### 4.2 Blueprint: `pkg/domain/models.go`
File path: `/Users/png/01. Project/coderpush/pkg/domain/models.go`

```go
package domain

import (
	"errors"
	"strings"
	"time"
)

// Common domain sentinel errors.
var (
	ErrNotFound               = errors.New("record not found")
	ErrEmptySKU               = errors.New("sku cannot be empty")
	ErrNegativePrice          = errors.New("price cannot be negative")
	ErrInvalidStatus          = errors.New("status must be ACTIVE or WITHDRAWN")
	ErrZeroTimestamp          = errors.New("timestamp must not be zero")
	ErrResurrectionRestricted = errors.New("tombstone invariant violation: cannot reactivate WITHDRAWN product")
	ErrOutdatedTimestamp      = errors.New("timestamp invariant violation: incoming record older than local state")
	ErrIdenticalUpdate        = errors.New("idempotency invariant: incoming record is identical to local state")
)

// ProductStatus defines the lifecycle status of a catalog product.
type ProductStatus string

const (
	StatusActive    ProductStatus = "ACTIVE"
	StatusWithdrawn ProductStatus = "WITHDRAWN"
)

// IsValid checks if the product status is one of the recognized constants.
func (s ProductStatus) IsValid() bool {
	return s == StatusActive || s == StatusWithdrawn
}

// Product represents a canonical item in the store's local catalog.
type Product struct {
	SKU               string        `json:"sku"`
	Name              string        `json:"name"`
	Price             float64       `json:"price"`
	Status            ProductStatus `json:"status"`
	SupplierTimestamp time.Time     `json:"supplier_timestamp"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

// SupplierUpdate represents an incoming product record from a supplier batch.
type SupplierUpdate struct {
	SKU       string        `json:"sku"`
	Name      string        `json:"name"`
	Price     float64       `json:"price"`
	Status    ProductStatus `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
}

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

// Batch represents a chunk of supplier updates processed inside an atomic transaction.
type Batch struct {
	JobID      string           `json:"job_id"`
	BatchIndex int              `json:"batch_index"`
	Records    []SupplierUpdate `json:"records"`
}

// ChangeSummary captures metric deltas for a batch or cumulative job run.
type ChangeSummary struct {
	Added      int `json:"added"`
	Updated    int `json:"updated"`
	Withdrawn  int `json:"withdrawn"`
	Skipped    int `json:"skipped"`
	DeadLetter int `json:"dead_letter"`
}

// Add combines another ChangeSummary into this summary.
func (c *ChangeSummary) Add(other ChangeSummary) {
	c.Added += other.Added
	c.Updated += other.Updated
	c.Withdrawn += other.Withdrawn
	c.Skipped += other.Skipped
	c.DeadLetter += other.DeadLetter
}

// TotalProcessed returns the total number of records accounted for.
func (c ChangeSummary) TotalProcessed() int {
	return c.Added + c.Updated + c.Withdrawn + c.Skipped + c.DeadLetter
}

// JobStatus represents the execution state of a catalog synchronization job.
type JobStatus string

const (
	JobStatusPending     JobStatus = "PENDING"
	JobStatusRunning     JobStatus = "RUNNING"
	JobStatusInterrupted JobStatus = "INTERRUPTED"
	JobStatusCompleted   JobStatus = "COMPLETED"
	JobStatusFailed      JobStatus = "FAILED"
)

// SyncJob tracks an end-to-end synchronization run.
type SyncJob struct {
	JobID               string        `json:"job_id"`
	Status              JobStatus     `json:"status"`
	TotalBatches        int           `json:"total_batches"`
	CommittedBatchIndex int           `json:"committed_batch_index"` // -1 before first batch
	Summary             ChangeSummary `json:"summary"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	CompletedAt         *time.Time    `json:"completed_at,omitempty"`
}

// BatchCheckpoint audits an individual committed batch.
type BatchCheckpoint struct {
	JobID       string        `json:"job_id"`
	BatchIndex  int           `json:"batch_index"`
	ItemsCount  int           `json:"items_count"`
	Summary     ChangeSummary `json:"summary"`
	CommittedAt time.Time     `json:"committed_at"`
}

// DeadLetterRecord stores an isolated corrupt or invalid record for staff inspection.
type DeadLetterRecord struct {
	IssueID    string    `json:"issue_id"`
	JobID      string    `json:"job_id"`
	BatchIndex int       `json:"batch_index"`
	SKU        string    `json:"sku"`
	RawPayload string    `json:"raw_payload"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}
```

---

### 4.3 Blueprint: `pkg/storage/sqlite.go`
File path: `/Users/png/01. Project/coderpush/pkg/storage/sqlite.go`

```go
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"coderpush/pkg/domain"

	_ "modernc.org/sqlite"
)

// Storage defines the complete repository interface for products, sync jobs, and audit records.
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

// SQLiteStorage implements Storage backed by embedded SQLite in WAL mode.
type SQLiteStorage struct {
	db *sql.DB
}

// Ensure SQLiteStorage implements Storage at compile time.
var _ Storage = (*SQLiteStorage)(nil)

// Open initializes SQLiteStorage, configures WAL pragmas and connection limits, and executes DDL migrations.
func Open(dsn string) (*SQLiteStorage, error) {
	// Construct optimized DSN if raw file path is passed
	formattedDSN := dsn
	if !strings.Contains(dsn, "?") && dsn != ":memory:" {
		formattedDSN = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_txlock=immediate", dsn)
	}

	db, err := sql.Open("sqlite", formattedDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Single connection serializes all writes, preventing SQLITE_BUSY deadlocks and race conditions.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	s := &SQLiteStorage{db: db}
	if err := s.initSchema(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return s, nil
}

// OpenInMemory initializes an in-memory SQLiteStorage instance ideal for tests.
func OpenInMemory() (*SQLiteStorage, error) {
	dsn := "file::memory:?cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_txlock=immediate"
	return Open(dsn)
}

// DB returns the underlying sql.DB instance.
func (s *SQLiteStorage) DB() *sql.DB {
	return s.db
}

// Close gracefully terminates database connections.
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// BeginTx starts an atomic transaction with immediate locking behavior.
func (s *SQLiteStorage) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// initSchema applies the DDL schema for products, sync_jobs, batch_checkpoints, and dead_letter_issues.
func (s *SQLiteStorage) initSchema(ctx context.Context) error {
	ddl := `
	PRAGMA busy_timeout = 5000;
	PRAGMA foreign_keys = ON;

	CREATE TABLE IF NOT EXISTS products (
		sku TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		price REAL NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('ACTIVE', 'WITHDRAWN')),
		supplier_timestamp TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_products_status ON products(status);

	CREATE TABLE IF NOT EXISTS sync_jobs (
		job_id TEXT PRIMARY KEY,
		status TEXT NOT NULL CHECK(status IN ('PENDING', 'RUNNING', 'INTERRUPTED', 'COMPLETED', 'FAILED')),
		total_batches INTEGER NOT NULL DEFAULT 0,
		committed_batch_index INTEGER NOT NULL DEFAULT -1,
		items_added INTEGER NOT NULL DEFAULT 0,
		items_updated INTEGER NOT NULL DEFAULT 0,
		items_withdrawn INTEGER NOT NULL DEFAULT 0,
		items_skipped INTEGER NOT NULL DEFAULT 0,
		items_dead_letter INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		completed_at TEXT
	);

	CREATE TABLE IF NOT EXISTS batch_checkpoints (
		job_id TEXT NOT NULL,
		batch_index INTEGER NOT NULL,
		items_count INTEGER NOT NULL,
		added_count INTEGER NOT NULL,
		updated_count INTEGER NOT NULL,
		withdrawn_count INTEGER NOT NULL,
		skipped_count INTEGER NOT NULL,
		dead_letter_count INTEGER NOT NULL,
		committed_at TEXT NOT NULL,
		PRIMARY KEY (job_id, batch_index),
		FOREIGN KEY (job_id) REFERENCES sync_jobs(job_id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS dead_letter_issues (
		issue_id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL,
		batch_index INTEGER NOT NULL,
		sku TEXT,
		raw_payload TEXT NOT NULL,
		reason TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY (job_id) REFERENCES sync_jobs(job_id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_issues_job_id ON dead_letter_issues(job_id, batch_index);
	`
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

// rowQuerier abstracts *sql.DB and *sql.Tx for row queries.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// GetProduct retrieves a product by its SKU from the database.
func (s *SQLiteStorage) GetProduct(ctx context.Context, sku string) (*domain.Product, error) {
	return s.getProduct(ctx, s.db, sku)
}

// GetProductTx retrieves a product within an active transaction boundary.
func (s *SQLiteStorage) GetProductTx(ctx context.Context, tx *sql.Tx, sku string) (*domain.Product, error) {
	return s.getProduct(ctx, tx, sku)
}

func (s *SQLiteStorage) getProduct(ctx context.Context, q rowQuerier, sku string) (*domain.Product, error) {
	query := `SELECT sku, name, price, status, supplier_timestamp, updated_at FROM products WHERE sku = ?`
	row := q.QueryRowContext(ctx, query, sku)

	var p domain.Product
	var statusStr, supplierTimeStr, updatedTimeStr string

	err := row.Scan(&p.SKU, &p.Name, &p.Price, &statusStr, &supplierTimeStr, &updatedTimeStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan product %s: %w", sku, err)
	}

	p.Status = domain.ProductStatus(statusStr)

	st, err := parseTime(supplierTimeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid supplier_timestamp for %s: %w", sku, err)
	}
	p.SupplierTimestamp = st

	ut, err := parseTime(updatedTimeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid updated_at for %s: %w", sku, err)
	}
	p.UpdatedAt = ut

	return &p, nil
}

// UpsertProductTx writes or updates a product atomically inside a transaction.
func (s *SQLiteStorage) UpsertProductTx(ctx context.Context, tx *sql.Tx, p *domain.Product) error {
	query := `
	INSERT INTO products (sku, name, price, status, supplier_timestamp, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(sku) DO UPDATE SET
		name = excluded.name,
		price = excluded.price,
		status = excluded.status,
		supplier_timestamp = excluded.supplier_timestamp,
		updated_at = excluded.updated_at;
	`
	_, err := tx.ExecContext(ctx, query,
		p.SKU,
		p.Name,
		p.Price,
		string(p.Status),
		p.SupplierTimestamp.UTC().Format(time.RFC3339Nano),
		p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert product %s: %w", p.SKU, err)
	}
	return nil
}

// CreateJob persists a new sync job record.
func (s *SQLiteStorage) CreateJob(ctx context.Context, job *domain.SyncJob) error {
	query := `
	INSERT INTO sync_jobs (
		job_id, status, total_batches, committed_batch_index,
		items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err := s.db.ExecContext(ctx, query,
		job.JobID,
		string(job.Status),
		job.TotalBatches,
		job.CommittedBatchIndex,
		job.Summary.Added,
		job.Summary.Updated,
		job.Summary.Withdrawn,
		job.Summary.Skipped,
		job.Summary.DeadLetter,
		job.CreatedAt.UTC().Format(time.RFC3339Nano),
		job.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to create job %s: %w", job.JobID, err)
	}
	return nil
}

// GetJob loads a sync job by its JobID.
func (s *SQLiteStorage) GetJob(ctx context.Context, jobID string) (*domain.SyncJob, error) {
	query := `
	SELECT job_id, status, total_batches, committed_batch_index,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       created_at, updated_at, completed_at
	FROM sync_jobs WHERE job_id = ?;
	`
	row := s.db.QueryRowContext(ctx, query, jobID)

	var job domain.SyncJob
	var statusStr, createdStr, updatedStr string
	var completedStr sql.NullString

	err := row.Scan(
		&job.JobID,
		&statusStr,
		&job.TotalBatches,
		&job.CommittedBatchIndex,
		&job.Summary.Added,
		&job.Summary.Updated,
		&job.Summary.Withdrawn,
		&job.Summary.Skipped,
		&job.Summary.DeadLetter,
		&createdStr,
		&updatedStr,
		&completedStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan job %s: %w", jobID, err)
	}

	job.Status = domain.JobStatus(statusStr)
	job.CreatedAt, _ = parseTime(createdStr)
	job.UpdatedAt, _ = parseTime(updatedStr)
	if completedStr.Valid && completedStr.String != "" {
		ct, err := parseTime(completedStr.String)
		if err == nil {
			job.CompletedAt = &ct
		}
	}

	return &job, nil
}

// GetActiveJob returns the currently RUNNING or PENDING job, or ErrNotFound if idle.
func (s *SQLiteStorage) GetActiveJob(ctx context.Context) (*domain.SyncJob, error) {
	query := `
	SELECT job_id, status, total_batches, committed_batch_index,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       created_at, updated_at, completed_at
	FROM sync_jobs WHERE status IN ('RUNNING', 'PENDING')
	ORDER BY created_at DESC LIMIT 1;
	`
	row := s.db.QueryRowContext(ctx, query)

	var job domain.SyncJob
	var statusStr, createdStr, updatedStr string
	var completedStr sql.NullString

	err := row.Scan(
		&job.JobID,
		&statusStr,
		&job.TotalBatches,
		&job.CommittedBatchIndex,
		&job.Summary.Added,
		&job.Summary.Updated,
		&job.Summary.Withdrawn,
		&job.Summary.Skipped,
		&job.Summary.DeadLetter,
		&createdStr,
		&updatedStr,
		&completedStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to query active job: %w", err)
	}

	job.Status = domain.JobStatus(statusStr)
	job.CreatedAt, _ = parseTime(createdStr)
	job.UpdatedAt, _ = parseTime(updatedStr)

	return &job, nil
}

// UpdateJobStatus updates the status and timestamp of a sync job.
func (s *SQLiteStorage) UpdateJobStatus(ctx context.Context, jobID string, status domain.JobStatus) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var query string
	var args []any

	if status == domain.JobStatusCompleted || status == domain.JobStatusFailed {
		query = `UPDATE sync_jobs SET status = ?, updated_at = ?, completed_at = ? WHERE job_id = ?;`
		args = []any{string(status), now, now, jobID}
	} else {
		query = `UPDATE sync_jobs SET status = ?, updated_at = ? WHERE job_id = ?;`
		args = []any{string(status), now, jobID}
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update job status %s: %w", jobID, err)
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RecordBatchCheckpointTx records an immutable checkpoint audit row inside the batch transaction.
func (s *SQLiteStorage) RecordBatchCheckpointTx(ctx context.Context, tx *sql.Tx, cp *domain.BatchCheckpoint) error {
	query := `
	INSERT INTO batch_checkpoints (
		job_id, batch_index, items_count,
		added_count, updated_count, withdrawn_count, skipped_count, dead_letter_count,
		committed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err := tx.ExecContext(ctx, query,
		cp.JobID,
		cp.BatchIndex,
		cp.ItemsCount,
		cp.Summary.Added,
		cp.Summary.Updated,
		cp.Summary.Withdrawn,
		cp.Summary.Skipped,
		cp.Summary.DeadLetter,
		cp.CommittedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to record batch checkpoint %s[%d]: %w", cp.JobID, cp.BatchIndex, err)
	}
	return nil
}

// UpdateJobProgressTx updates cumulative progress counters in sync_jobs atomically inside the batch transaction.
func (s *SQLiteStorage) UpdateJobProgressTx(ctx context.Context, tx *sql.Tx, jobID string, batchIndex int, delta domain.ChangeSummary) error {
	query := `
	UPDATE sync_jobs SET
		committed_batch_index = ?,
		items_added = items_added + ?,
		items_updated = items_updated + ?,
		items_withdrawn = items_withdrawn + ?,
		items_skipped = items_skipped + ?,
		items_dead_letter = items_dead_letter + ?,
		updated_at = ?
	WHERE job_id = ?;
	`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, query,
		batchIndex,
		delta.Added,
		delta.Updated,
		delta.Withdrawn,
		delta.Skipped,
		delta.DeadLetter,
		now,
		jobID,
	)
	if err != nil {
		return fmt.Errorf("failed to update job progress for %s: %w", jobID, err)
	}
	return nil
}

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

// ListDeadLetterRecords queries paginated dead-letter records for a specific job (or all jobs if jobID is empty).
func (s *SQLiteStorage) ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error) {
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
		ORDER BY created_at DESC LIMIT ? OFFSET ?;
		`
		selectArgs = []any{jobID, limit, offset}
	} else {
		countQuery = `SELECT count(*) FROM dead_letter_issues;`
		selectQuery = `
		SELECT issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
		FROM dead_letter_issues
		ORDER BY created_at DESC LIMIT ? OFFSET ?;
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

	var records []domain.DeadLetterRecord
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
		r.CreatedAt, _ = parseTime(timeStr)
		records = append(records, r)
	}

	return records, totalCount, nil
}

// parseTime parses RFC3339Nano or RFC3339 formatted UTC timestamps.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
```

---

### 4.4 Blueprint: `pkg/storage/sqlite_test.go`
File path: `/Users/png/01. Project/coderpush/pkg/storage/sqlite_test.go`

```go
package storage_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
)

func TestStorage_InitAndPragmas(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory storage: %v", err)
	}
	defer s.Close()

	// Verify busy_timeout pragma
	var busyTimeout int
	err = s.DB().QueryRowContext(ctx, "PRAGMA busy_timeout;").Scan(&busyTimeout)
	if err != nil {
		t.Fatalf("failed querying busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("expected busy_timeout = 5000, got %d", busyTimeout)
	}

	// Verify foreign_keys pragma
	var foreignKeys int
	err = s.DB().QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&foreignKeys)
	if err != nil {
		t.Fatalf("failed querying foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("expected foreign_keys = 1, got %d", foreignKeys)
	}
}

func TestStorage_UpsertAndGetProduct(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory storage: %v", err)
	}
	defer s.Close()

	// 1. Query non-existent product
	_, err = s.GetProduct(ctx, "SKU-NON-EXISTENT")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. Insert new product via transaction
	now := time.Now().UTC().Truncate(time.Millisecond)
	p := &domain.Product{
		SKU:               "SKU-001",
		Name:              "Widget Pro",
		Price:             49.99,
		Status:            domain.StatusActive,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}

	// Verify GetProductTx inside transaction before insert
	_, err = s.GetProductTx(ctx, tx, "SKU-001")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound inside tx before insert, got %v", err)
	}

	if err := s.UpsertProductTx(ctx, tx, p); err != nil {
		tx.Rollback()
		t.Fatalf("failed upserting product: %v", err)
	}

	// Verify GetProductTx sees uncommitted write
	pTx, err := s.GetProductTx(ctx, tx, "SKU-001")
	if err != nil {
		tx.Rollback()
		t.Fatalf("expected to read uncommitted insert inside tx, got %v", err)
	}
	if pTx.Name != "Widget Pro" {
		t.Errorf("expected Name 'Widget Pro', got %s", pTx.Name)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("failed committing tx: %v", err)
	}

	// 3. Query committed product via GetProduct
	saved, err := s.GetProduct(ctx, "SKU-001")
	if err != nil {
		t.Fatalf("failed retrieving committed product: %v", err)
	}
	if saved.SKU != p.SKU || saved.Name != p.Name || saved.Price != p.Price || saved.Status != p.Status {
		t.Errorf("mismatch in stored product: %+v vs %+v", saved, p)
	}

	// 4. Update product via UpsertProductTx (ON CONFLICT DO UPDATE)
	later := now.Add(10 * time.Minute)
	p.Price = 39.99
	p.Status = domain.StatusWithdrawn
	p.SupplierTimestamp = later
	p.UpdatedAt = later

	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed beginning tx2: %v", err)
	}
	if err := s.UpsertProductTx(ctx, tx2, p); err != nil {
		tx2.Rollback()
		t.Fatalf("failed upserting updated product: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("failed committing tx2: %v", err)
	}

	updated, err := s.GetProduct(ctx, "SKU-001")
	if err != nil {
		t.Fatalf("failed retrieving updated product: %v", err)
	}
	if updated.Price != 39.99 || updated.Status != domain.StatusWithdrawn {
		t.Errorf("expected updated price 39.99 and WITHDRAWN status, got %+v", updated)
	}
}

func TestStorage_JobLifecycleAndRecovery(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	job := &domain.SyncJob{
		JobID:               "job-100",
		Status:              domain.JobStatusRunning,
		TotalBatches:        10,
		CommittedBatchIndex: -1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("failed creating job: %v", err)
	}

	active, err := s.GetActiveJob(ctx)
	if err != nil {
		t.Fatalf("failed retrieving active job: %v", err)
	}
	if active.JobID != "job-100" || active.Status != domain.JobStatusRunning {
		t.Errorf("unexpected active job: %+v", active)
	}

	// Simulate crash recovery: startup detects RUNNING job and marks INTERRUPTED
	recovered, err := s.RecoverInterruptedJobs(ctx)
	if err != nil {
		t.Fatalf("failed recovering jobs: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != "job-100" {
		t.Fatalf("expected recovered [job-100], got %v", recovered)
	}

	// Verify status is now INTERRUPTED
	recoveredJob, err := s.GetJob(ctx, "job-100")
	if err != nil {
		t.Fatalf("failed retrieving recovered job: %v", err)
	}
	if recoveredJob.Status != domain.JobStatusInterrupted {
		t.Errorf("expected status INTERRUPTED, got %s", recoveredJob.Status)
	}

	// Verify no active job remains
	_, err = s.GetActiveJob(ctx)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for active job, got %v", err)
	}
}

func TestStorage_ConcurrencyAndRaceSafety(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	const workers = 8
	const itemsPerWorker = 25
	var wg sync.WaitGroup

	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < itemsPerWorker; i++ {
				sku := fmt.Sprintf("SKU-W%d-I%d", workerID, i)
				now := time.Now().UTC()
				p := &domain.Product{
					SKU:               sku,
					Name:              fmt.Sprintf("Item %s", sku),
					Price:             float64(workerID*10 + i),
					Status:            domain.StatusActive,
					SupplierTimestamp: now,
					UpdatedAt:         now,
				}

				tx, err := s.BeginTx(ctx)
				if err != nil {
					t.Errorf("worker %d failed BeginTx: %v", workerID, err)
					return
				}
				if err := s.UpsertProductTx(ctx, tx, p); err != nil {
					tx.Rollback()
					t.Errorf("worker %d failed UpsertProductTx: %v", workerID, err)
					return
				}
				if err := tx.Commit(); err != nil {
					t.Errorf("worker %d failed Commit: %v", workerID, err)
					return
				}

				// Concurrent read
				_, err = s.GetProduct(ctx, sku)
				if err != nil {
					t.Errorf("worker %d failed reading %s: %v", workerID, sku, err)
				}
			}
		}(w)
	}

	wg.Wait()

	// Verify total count
	var totalProducts int
	err = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM products;").Scan(&totalProducts)
	if err != nil {
		t.Fatalf("failed querying products count: %v", err)
	}
	expected := workers * itemsPerWorker
	if totalProducts != expected {
		t.Errorf("expected %d products, got %d", expected, totalProducts)
	}
}
```

---

## 5. Verification Method

Once Milestone 1 implementation begins, verify the implementation via:

1. **Module & Dependency Verification**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go mod tidy
   go list -m all | grep sqlite
   # Expect: modernc.org/sqlite v1.36.0
   ```

2. **Compilation with Pure Go (`CGO_ENABLED=0`)**:
   ```bash
   CGO_ENABLED=0 go vet ./...
   CGO_ENABLED=0 go build -v ./...
   ```

3. **Storage Unit Tests & Data Race Verification**:
   ```bash
   go test -v -race -count=5 ./pkg/storage/...
   # Expect: 5 consecutive iterations passing with 0 data races.
   ```

4. **Schema & Pragmas Verification**:
   Inspect initialized database file:
   ```bash
   sqlite3 /tmp/test_catalog.db "PRAGMA journal_mode; PRAGMA busy_timeout; PRAGMA foreign_keys;"
   # Expect:
   # wal
   # 5000
   # 1
   ```

5. **Invalidation Conditions**:
   - `modernc.org/sqlite` upgraded to `>= v1.60.0` requiring Go `>= 1.26` (must keep pinned to `v1.36.0`).
   - SQLite `database is locked` / `SQLITE_BUSY` errors appearing under concurrency (invalidates connection pool configuration; verify `SetMaxOpenConns(1)`).
   - Non-UTC timestamps causing lexicographical ordering mismatch in SQL queries.
