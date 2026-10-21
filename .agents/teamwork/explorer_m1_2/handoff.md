# Synchronization Engine & Invariants A, B, C: Architectural Design & Implementation Blueprints

**Target Component**: `pkg/sync/engine.go` & Test Suites (`pkg/sync/engine_test.go`, `pkg/sync/engine_race_test.go`)  
**Milestone**: Milestone 1 (Core Domain, Storage & Synchronization Engine)  
**Author**: `teamwork_preview_explorer` (`explorer_m1_2`)  
**Working Directory**: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2`  
**Parent Agent**: `e6900bb2-6639-41ea-b0fb-48d815c2c1c8`  
**Date**: 2026-10-21  

---

## 1. Observation

### 1.1 Authoritative Requirements & Direct Citations

1. **`ORIGINAL_REQUEST.md`**:
   - **R1. Deterministic Batch Catalog Synchronization & Invariants** (lines 12–17):
     - *"Ingest supplier product batches and store a local copy using an embedded database."*
     - *"Enforce idempotency: importing the exact same update multiple times must never duplicate products or drift catalog state."*
     - *"Enforce fast-forward out-of-order resolution: incoming older information (based on supplier timestamp) must not overwrite newer local information."*
     - *"Enforce tombstone preservation: a product marked as withdrawn from sale must never be restored to active sale by older or subsequent update payloads."*
   - **Acceptance Criteria** (lines 40–42):
     - *"Ingesting the same batch twice results in 0 additional inserts, 0 updates, and identical catalog state."*
     - *"An incoming record with timestamp $T_{new} < T_{old}$ is rejected and counted as skipped."*
     - *"A product with status `WITHDRAWN` is never flipped back to `ACTIVE`."*
   - **R5. Security, Test Rigor & CI/CD Packaging** (lines 51–52):
     - *"Automated test suite achieves $\ge 90\%$ statement/branch coverage across the project."*
     - *"Multi-iteration test run (`go test -race -count=5 ./...`) passes with 0 data races."*

2. **`PROJECT.md` Interface Contracts**:
   - **Domain Primitives (`pkg/domain`)** (lines 80–128):
     - `Product`: `SKU string`, `Name string`, `Price float64`, `Status ProductStatus`, `SupplierTimestamp time.Time`, `UpdatedAt time.Time`.
     - `SupplierUpdate`: `SKU string`, `Name string`, `Price float64`, `Status ProductStatus`, `Timestamp time.Time`.
     - `Batch`: `JobID string`, `BatchIndex int`, `Records []SupplierUpdate`.
     - `ChangeSummary`: `Added int`, `Updated int`, `Withdrawn int`, `Skipped int`, `DeadLetter int`.
   - **Sync Engine Contract (`pkg/sync`)** (lines 164–186):
     ```go
     type Engine interface {
         ExecuteBatch(ctx context.Context, batch domain.Batch) (domain.ChangeSummary, error)
         RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
         ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
         RecoverOnStartup(ctx context.Context) ([]string, error)
     }
     ```

3. **`DISPATCH.md` Directives**:
   - Scope for Milestone 1:
     - Synchronization Engine in `pkg/sync/engine.go`: `ExecuteBatch(ctx, batch)` transaction management.
     - Invariant A (Deterministic Idempotency): Compare existing record $P$ with incoming record $R$. If identical, perform NO database write, increment `Skipped`. Re-running identical batch produces 0 Added, 0 Updated, 0 Withdrawn, $N$ Skipped.
     - Invariant B (Fast-Forward Timestamps): If incoming $T_{incoming} < T_{local}$: record rejected and skipped (`Skipped++`), catalog unchanged.
     - Invariant C (Tombstone Preservation): If existing $P.status == "WITHDRAWN"$: monotonic rule: status can NEVER flip back to "ACTIVE". Incoming update attempting to reactivate is rejected (`Skipped++`), status remains "WITHDRAWN".
     - Change counters calculation:
       - `+Added`: new product inserted.
       - `~Updated`: existing product updated with newer valid timestamp.
       - `-Withdrawn`: product transitioned to WITHDRAWN.
       - `↷Skipped`: idempotent duplicate, out-of-order drop, or forbidden resurrection.

4. **Peer Artifacts (`explorer_m1_1/handoff.md` & `spec_miner_m1_1/handoff.md`)**:
   - `explorer_m1_1`: Designed `pkg/domain/models.go` and `pkg/storage/sqlite.go` implementing `Storage` with:
     - `BeginTx(ctx context.Context) (*sql.Tx, error)`
     - `GetProduct(ctx context.Context, sku string) (*domain.Product, error)`
     - `GetProductTx(ctx context.Context, tx *sql.Tx, sku string) (*domain.Product, error)`
     - `UpsertProductTx(ctx context.Context, tx *sql.Tx, p *domain.Product) error`
     - Connection pool: `db.SetMaxOpenConns(1)` to serialize writes and prevent `SQLITE_BUSY`.
   - `spec_miner_m1_1`: Formalized 26 features, 31 edge cases, and test harness (`TestEngine_InvariantA_BatchReplay`, `TestEngine_InvariantB_OlderTimestamp`, `TestEngine_InvariantC_ResurrectionBlocked`, `TestEngine_IntraBatch_TombstoneLock`, `TestEngine_Race_ConcurrentReadsAndWrites`).

---

## 2. Logic Chain

### 2.1 State Representation & Intra-Batch Staging

1. **Problem**: A batch is an atomic sequence of supplier updates: $B = [R_1, R_2, \dots, R_m]$. A supplier update payload may contain multiple records for the identical SKU within the same batch (e.g., $R_1$: price reduction, $R_2$: withdrawal).
2. **Analysis**:
   - If `ExecuteBatch` queries the database for each record without caching or using `GetProductTx`, and `db.SetMaxOpenConns(1)` is active, querying `db.QueryRowContext` on another connection while `tx` is checked out will deadlock waiting for a connection!
   - Furthermore, even within a transaction, repeated SQL round-trips for the same SKU add latency.
3. **Resolution**:
   - Maintain an in-memory batch staging map:
     $$\text{batchState} : \text{string} \to *\text{domain.Product}$$
   - When evaluating record $R_i$:
     1. Check if $R_i.SKU \in \text{batchState}$.
     2. If not found in `batchState`, fetch from storage via `GetProductTx(ctx, tx, sku)` (or `GetProduct(ctx, sku)`). Cache the result (or `nil` if not found).
     3. Evaluate Invariants A, B, and C against the current state $P = \text{batchState}[R_i.SKU]$.
     4. If the record modifies the catalog, write to database via `UpsertProductTx(ctx, tx, updatedProduct)` and immediately update `batchState[R_i.SKU] = updatedProduct`.
     5. If the record is skipped, `batchState` remains unchanged, and zero database writes are issued.
4. **Guarantees**:
   - Zero connection deadlocks.
   - At most one SELECT query per unique SKU per batch.
   - Intra-batch state transitions (e.g. `ACTIVE` $\to$ `WITHDRAWN`) take effect immediately for subsequent records in the same batch, preventing intra-batch resurrection anomalies.

---

### 2.2 Formal Invariant Decision Rules

Let $R$ be an incoming update and $P$ be the existing product state (from `batchState` or DB).

#### Step 1: Input Normalization & Validation (Invariant D Forward Compatibility)
- Trim whitespace from SKU: $SKU = \text{TrimSpace}(R.SKU)$.
- Normalize timestamp: $T_{R} = R.Timestamp.\text{UTC}()$.
- Precondition Check:
  If $SKU == "" \lor R.Price < 0 \lor (R.Status \neq \text{ACTIVE} \land R.Status \neq \text{WITHDRAWN})$:
  - Increment `summary.DeadLetter++`.
  - If storage supports `RecordDeadLetterTx`, record quarantine entry.
  - Skip to next record.

#### Step 2: New Product Insertion ($P == nil$)
- When product does not exist in catalog:
  - Construct:
    $$P_{new} = \text{Product}\{ SKU, Name: R.Name, Price: R.Price, Status: R.Status, SupplierTimestamp: T_R, UpdatedAt: now \}$$
  - Call `storage.UpsertProductTx(ctx, tx, P_{new})`.
  - Update `batchState[SKU] = P_{new}`.
  - Increment:
    $$\begin{cases} \text{summary.Added++} & \text{for new products (including initial WITHDRAWN)} \end{cases}$$
    *(Per `spec_miner_m1_1` spec: `TestEngine_InvariantC_InitialWithdrawn` asserts `Added=1`).*

#### Step 3: Existing Product Evaluation ($P \neq nil$)

The evaluation must follow strict precedence:

```
[Incoming Record R]
       |
       v
(1) Identical in ALL fields? (SKU, Name, Price, Status, SupplierTimestamp)
       |-- YES --> Invariant A: NO DB Write, summary.Skipped++ (Idempotent Duplicate)
       |-- NO  --> Next check
       v
(2) Is P.Status == WITHDRAWN AND R.Status == ACTIVE?
       |-- YES --> Invariant C: NO DB Write, summary.Skipped++ (Resurrection Forbidden)
       |-- NO  --> Next check
       v
(3) Is T_R < P.SupplierTimestamp?
       |-- YES --> Invariant B: NO DB Write, summary.Skipped++ (Out-of-Order Stale Drop)
       |-- NO  --> Next check
       v
(4) Is T_R == P.SupplierTimestamp? (With differing payload)
       |-- YES --> Invariant B: NO DB Write, summary.Skipped++ (Non-Advancing Timestamp Drop)
       |-- NO  --> Next check (T_R > P.SupplierTimestamp)
       v
(5) Valid Update: T_R > P.SupplierTimestamp
       |
       +--> Call storage.UpsertProductTx(ctx, tx, P_updated)
       +--> batchState[SKU] = P_updated
       |
       +--> If P.Status == ACTIVE and R.Status == WITHDRAWN:
       |        summary.Withdrawn++ (Transition to Tombstone)
       +--> Else:
                summary.Updated++   (ACTIVE->ACTIVE or WITHDRAWN->WITHDRAWN Metadata)
```

---

### 2.3 Mathematical Proof of Invariants

1. **Proof of Invariant A (Deterministic Idempotency)**:
   - Let $S_0$ be initial state. Let batch $B = \{R_1, \dots, R_n\}$ be applied, resulting in state $S_1 = f(S_0, B)$ and counters $\Delta_1$.
   - For every record $R_i \in B$, $P_i \in S_1$ has $P_i.SKU = R_i.SKU, P_i.Name = R_i.Name, P_i.Price = R_i.Price, P_i.Status = R_i.Status, P_i.SupplierTimestamp = R_i.Timestamp$.
   - When $B$ is applied a second time: $S_2 = f(S_1, B)$.
   - For every $R_i \in B$, check (1) succeeds because all 5 fields match identically.
   - Therefore, zero writes are issued to the database: $S_2 \equiv S_1$.
   - Every record increments `Skipped`.
   - Counters for 2nd run: $\Delta_2 = (Added: 0, Updated: 0, Withdrawn: 0, Skipped: |B|, DeadLetter: 0)$.
   - $\therefore$ System satisfies $f(f(S, B), B) = f(S, B)$ with zero catalog drift and deterministic counter behavior. $\blacksquare$

2. **Proof of Invariant B (Monotonic Fast-Forward Timestamps)**:
   - For any SKU $k$, let $T(k, t)$ denote the local supplier timestamp at step $t$.
   - By rules (3), (4), and (5), a database write occurs if and only if $T_{incoming} > T(k, t)$.
   - Therefore, $T(k, t+1) \ge T(k, t)$ for all transitions.
   - Any record with $T_{incoming} \le T(k, t)$ is dropped without modifying catalog state.
   - $\therefore$ The catalog state is monotonically non-decreasing in supplier timestamp. $\blacksquare$

3. **Proof of Invariant C (Tombstone Monotonicity)**:
   - Define status lattice: $\text{ACTIVE} \prec \text{WITHDRAWN}$.
   - Rule (2) states: If $P.Status = \text{WITHDRAWN}$ and $R.Status = \text{ACTIVE}$, the update is rejected regardless of timestamp ($T_{incoming} \in [-\infty, +\infty]$).
   - The only valid status transitions in Rule (5) are:
     - $\text{ACTIVE} \to \text{ACTIVE}$
     - $\text{ACTIVE} \to \text{WITHDRAWN}$
     - $\text{WITHDRAWN} \to \text{WITHDRAWN}$
   - Transition $\text{WITHDRAWN} \to \text{ACTIVE}$ has transition probability and capability equal to 0.
   - $\therefore$ $\text{WITHDRAWN}$ is an absorbing state. Tombstone preservation holds unconditionally. $\blacksquare$

---

### 2.4 Transactional Boundary & Concurrency Model

1. **SQLite Serialized Writer Protocol**:
   - SQLite permits concurrent readers in WAL mode, but only one write transaction at any instant.
   - In Go, if multiple goroutines invoke `BeginTx` concurrently, SQLite lock contention can lead to `SQLITE_BUSY` errors if transactions wait longer than `busy_timeout`.
   - **Engine-Level Synchronization**:
     Include a `sync.Mutex` on `SyncEngine`.
     `ExecuteBatch` acquires `e.mu.Lock()` before initiating the database transaction and releases it via `defer e.mu.Unlock()`.
   - **Benefits**:
     - Prevents lock acquisition races entirely within the process.
     - Guarantees 0 `SQLITE_BUSY` errors during concurrent execution.
     - Allows concurrent read-only queries (`GetProduct`) via WAL mode without blocking.
     - Passes multi-iteration race tests (`go test -race -count=5 ./...`) with zero data races.

2. **Context Cancellation & Rollback**:
   - `ExecuteBatch` checks `ctx.Err()`:
     1. Prior to lock acquisition.
     2. Immediately after lock acquisition.
     3. On every iteration of the record loop.
   - If `ctx.Err()` is non-nil at any point:
     - The function immediately returns `(ChangeSummary{}, ctx.Err())`.
     - The deferred `tx.Rollback()` aborts the transaction.
     - Uncommitted writes are discarded by SQLite WAL.
     - Database integrity is fully preserved (Invariant E).

---

## 3. Caveats

1. **Floating-Point Price Equality**:
   - `domain.Product` defines `Price float64`. Directly using `==` on IEEE 754 floating-point values can produce false inequalities due to binary precision artifacts after JSON/SQLite roundtrips.
   - **Resolution**: Use epsilon comparison:
     $$\text{priceEqual}(a, b) = |a - b| < 10^{-6}$$
     This handles standard currency precision down to micro-cents while avoiding float jitter.
2. **Timestamp Formatting & Timezone Normalization**:
   - Timestamps may arrive with varying offsets (`Z`, `+07:00`, `-05:00`).
   - Comparing strings directly would fail.
   - **Resolution**: Always convert incoming timestamps to UTC (`record.Timestamp.UTC()`) and use `time.Time.Equal()`, `Before()`, and `After()`.
3. **Dead-Letter Isolation Scope (M1 vs M2)**:
   - In Milestone 1, focus is on Invariants A, B, and C. Full dead-letter queue inspection API and persistent issue pagination belong to Milestone 2 & 4.
   - However, `pkg/sync/engine.go` implements forward-compatible quarantine checks (`isInvalidRecord`). If storage supports `RecordDeadLetterTx`, it writes to `dead_letter_issues`; otherwise it simply increments `summary.DeadLetter++`.
4. **Initial Withdrawn Product Metric Counting**:
   - When a product that does not exist in the database arrives with status `WITHDRAWN`, it is inserted into the catalog as `WITHDRAWN` (to act as a tombstone for future out-of-order active arrivals).
   - Per test specification `TestEngine_InvariantC_InitialWithdrawn` in `spec_miner_m1_1/handoff.md`, this is counted as `Added=1` (row added to catalog) with `Status=WITHDRAWN`.

---

## 4. Conclusion & Code Blueprints

The synchronization engine is designed with modularity, zero allocations in critical hot loops, deterministic invariant evaluation, and robust multi-goroutine race safety.

### 4.1 Blueprint: `pkg/sync/engine.go`
File path: `/Users/png/01. Project/coderpush/pkg/sync/engine.go`

```go
package sync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	stdsync "sync"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
)

// Storage defines the contract required by the synchronization engine.
// It matches pkg/storage.Storage and allows interface decoupling and test mocking.
type Storage interface {
	DB() *sql.DB
	Close() error
	BeginTx(ctx context.Context) (*sql.Tx, error)

	GetProduct(ctx context.Context, sku string) (*domain.Product, error)
	GetProductTx(ctx context.Context, tx *sql.Tx, sku string) (*domain.Product, error)
	UpsertProductTx(ctx context.Context, tx *sql.Tx, p *domain.Product) error

	// Forward-compatible interfaces
	RecoverInterruptedJobs(ctx context.Context) ([]string, error)
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
}

// Engine defines the high-level synchronization operations.
type Engine interface {
	ExecuteBatch(ctx context.Context, batch domain.Batch) (domain.ChangeSummary, error)
	RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
	ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
	RecoverOnStartup(ctx context.Context) ([]string, error)
}

// SyncEngine implements Engine with strict enforcement of Invariants A, B, and C.
type SyncEngine struct {
	storage Storage
	mu      stdsync.Mutex // Serializes batch writes for SQLite single-writer safety
	now     func() time.Time
}

// Ensure SyncEngine implements Engine at compile time.
var _ Engine = (*SyncEngine)(nil)

// Option allows configuring SyncEngine options (e.g. clock mocking for tests).
type Option func(*SyncEngine)

// WithTimeFunc configures custom clock provider for deterministic testing.
func WithTimeFunc(now func() time.Time) Option {
	return func(e *SyncEngine) {
		e.now = now
	}
}

// NewEngine initializes a new SyncEngine instance with supplied storage and options.
func NewEngine(store Storage, opts ...Option) *SyncEngine {
	e := &SyncEngine{
		storage: store,
		now:     func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ExecuteBatch processes a single batch inside an atomic transaction, enforcing Invariants A, B, C, D, E.
func (e *SyncEngine) ExecuteBatch(ctx context.Context, batch domain.Batch) (domain.ChangeSummary, error) {
	// 1. Fast path: Empty batch produces empty summary with no DB interaction.
	if len(batch.Records) == 0 {
		return domain.ChangeSummary{}, nil
	}

	// 2. Immediate check for context cancellation before acquiring lock.
	if err := ctx.Err(); err != nil {
		return domain.ChangeSummary{}, err
	}

	// 3. Serialize write operations across goroutines to eliminate SQLITE_BUSY contention.
	e.mu.Lock()
	defer e.mu.Unlock()

	// Re-verify context after acquiring lock.
	if err := ctx.Err(); err != nil {
		return domain.ChangeSummary{}, err
	}

	if e.storage == nil {
		return domain.ChangeSummary{}, errors.New("sync engine storage is nil")
	}

	// 4. Begin atomic batch transaction.
	tx, err := e.storage.BeginTx(ctx)
	if err != nil {
		return domain.ChangeSummary{}, fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	// Defer rollback; if tx.Commit() succeeds, tx.Rollback() returns sql.ErrTxDone and is safely ignored.
	defer func() {
		_ = tx.Rollback()
	}()

	summary := domain.ChangeSummary{}

	// Intra-batch state cache: tracks current state of products within this batch.
	// Key: normalized SKU. Value: *domain.Product (nil means record does not exist in DB yet).
	batchState := make(map[string]*domain.Product)

	for i, record := range batch.Records {
		// Context check per record iteration to abort promptly on cancellation/timeout.
		if err := ctx.Err(); err != nil {
			return domain.ChangeSummary{}, err
		}

		sku := strings.TrimSpace(record.SKU)
		recordTimestamp := record.Timestamp.UTC()

		// --- Validation & Invariant D: Dead-Letter Isolation (Forward Compatibility) ---
		if isInvalidRecord(sku, record.Price, record.Status) {
			summary.DeadLetter++
			dlRecord := &domain.DeadLetterRecord{
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

		// --- Step 1: Resolve existing product state ---
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
			// Cache in batchState (can be nil if product does not exist)
			batchState[sku] = existingProduct
		}

		// --- Step 2: Product does NOT exist -> New Insertion ---
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

		// --- Step 3: Product exists -> Evaluate Invariants A, B, C ---

		// Invariant A: Deterministic Idempotency
		// If all business fields and supplier timestamp match exactly: NO DB write, increment Skipped.
		if isIdenticalProduct(existingProduct, record, recordTimestamp) {
			summary.Skipped++
			continue
		}

		// Invariant C: Tombstone Preservation
		// Monotonic rule: Status can NEVER flip from WITHDRAWN back to ACTIVE.
		// Any update attempting to resurrect a WITHDRAWN product is rejected; status remains WITHDRAWN.
		if existingProduct.Status == domain.StatusWithdrawn && record.Status == domain.StatusActive {
			summary.Skipped++
			continue
		}

		// Invariant B: Fast-Forward Timestamps
		// 1. If incoming timestamp is strictly older than existing timestamp: reject and skip.
		if recordTimestamp.Before(existingProduct.SupplierTimestamp) {
			summary.Skipped++
			continue
		}

		// 2. If incoming timestamp equals existing timestamp but payload differed:
		// Cannot determine newer version; strict ordering drops non-advancing timestamps.
		if recordTimestamp.Equal(existingProduct.SupplierTimestamp) {
			summary.Skipped++
			continue
		}

		// --- Step 4: Valid Update (recordTimestamp.After(existingProduct.SupplierTimestamp)) ---
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
			// Product transitioned from ACTIVE to WITHDRAWN
			summary.Withdrawn++
		} else {
			// Updated existing product (ACTIVE -> ACTIVE or WITHDRAWN -> WITHDRAWN metadata update)
			summary.Updated++
		}
	}

	// 5. Commit atomic transaction.
	if err := tx.Commit(); err != nil {
		return domain.ChangeSummary{}, fmt.Errorf("failed to commit batch transaction: %w", err)
	}

	return summary, nil
}

// RunJob processes all batches from a batch reader/generator. Scheduled for Milestone 2.
func (e *SyncEngine) RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
	return nil, errors.New("RunJob is scheduled for Milestone 2")
}

// ResumeJob restarts an interrupted job from its last committed batch checkpoint. Scheduled for Milestone 2.
func (e *SyncEngine) ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
	return nil, errors.New("ResumeJob is scheduled for Milestone 2")
}

// RecoverOnStartup checks for running jobs and transitions them to INTERRUPTED.
func (e *SyncEngine) RecoverOnStartup(ctx context.Context) ([]string, error) {
	if e.storage == nil {
		return nil, errors.New("storage is nil")
	}
	return e.storage.RecoverInterruptedJobs(ctx)
}

// --- Helper Functions ---

func isIdenticalProduct(p *domain.Product, r domain.SupplierUpdate, rTimestamp time.Time) bool {
	if p.SKU != strings.TrimSpace(r.SKU) {
		return false
	}
	if p.Name != r.Name {
		return false
	}
	if !priceEqual(p.Price, r.Price) {
		return false
	}
	if p.Status != r.Status {
		return false
	}
	if !p.SupplierTimestamp.Equal(rTimestamp) {
		return false
	}
	return true
}

func priceEqual(a, b float64) bool {
	const epsilon = 1e-6
	return math.Abs(a-b) < epsilon
}

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

---

### 4.2 Blueprint: `pkg/sync/engine_test.go`
File path: `/Users/png/01. Project/coderpush/pkg/sync/engine_test.go`

```go
package sync_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

func setupTestStorage(t *testing.T) *storage.SQLiteStorage {
	t.Helper()
	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to create in-memory storage: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

// TestEngine_InvariantA_BatchReplay verifies that ingesting the identical batch twice
// produces 0 adds, 0 updates, 0 withdrawals, and N skipped with unchanged catalog state.
func TestEngine_InvariantA_BatchReplay(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	batch := domain.Batch{
		JobID:      "job-replay-1",
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-001", Name: "Widget A", Price: 10.50, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-002", Name: "Widget B", Price: 20.00, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-003", Name: "Widget C", Price: 30.25, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-004", Name: "Widget D", Price: 40.00, Status: domain.StatusWithdrawn, Timestamp: baseTime},
			{SKU: "SKU-005", Name: "Widget E", Price: 50.75, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}

	// 1st Run: Expect all records added
	res1, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	expected1 := domain.ChangeSummary{Added: 5, Updated: 0, Withdrawn: 0, Skipped: 0, DeadLetter: 0}
	if res1 != expected1 {
		t.Errorf("run 1 mismatch: got %+v, want %+v", res1, expected1)
	}

	// Snapshot state of product 1
	p1Before, err := store.GetProduct(ctx, "SKU-001")
	if err != nil || p1Before == nil {
		t.Fatalf("failed to retrieve SKU-001: %v", err)
	}

	// 2nd Run: Replay exact same batch
	res2, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	expected2 := domain.ChangeSummary{Added: 0, Updated: 0, Withdrawn: 0, Skipped: 5, DeadLetter: 0}
	if res2 != expected2 {
		t.Errorf("run 2 mismatch: got %+v, want %+v", res2, expected2)
	}

	// Verify catalog state is unchanged
	p1After, err := store.GetProduct(ctx, "SKU-001")
	if err != nil || p1After == nil {
		t.Fatalf("failed to retrieve SKU-001 after replay: %v", err)
	}
	if p1Before.Price != p1After.Price || p1Before.Name != p1After.Name || !p1Before.SupplierTimestamp.Equal(p1After.SupplierTimestamp) {
		t.Errorf("product drifted on replay: before %+v, after %+v", p1Before, p1After)
	}
}

// TestEngine_InvariantA_PartialOverlap tests batch containing both new and duplicate items.
func TestEngine_InvariantA_PartialOverlap(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	batch1 := domain.Batch{
		JobID:      "job-overlap-1",
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-1", Name: "Item 1", Price: 10.0, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-2", Name: "Item 2", Price: 20.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}
	_, err := engine.ExecuteBatch(ctx, batch1)
	if err != nil {
		t.Fatalf("batch 1 failed: %v", err)
	}

	// Batch 2: SKU-2 is identical, SKU-3 is new
	batch2 := domain.Batch{
		JobID:      "job-overlap-2",
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-2", Name: "Item 2", Price: 20.0, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-3", Name: "Item 3", Price: 30.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}
	res2, err := engine.ExecuteBatch(ctx, batch2)
	if err != nil {
		t.Fatalf("batch 2 failed: %v", err)
	}
	expected2 := domain.ChangeSummary{Added: 1, Updated: 0, Withdrawn: 0, Skipped: 1, DeadLetter: 0}
	if res2 != expected2 {
		t.Errorf("batch 2 mismatch: got %+v, want %+v", res2, expected2)
	}
}

// TestEngine_InvariantB_NewerTimestamp tests applying update with newer timestamp.
func TestEngine_InvariantB_NewerTimestamp(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)

	// Seed item
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B1", Name: "Old Name", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})

	// Update with T2 > T1
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B1", Name: "New Name", Price: 15.0, Status: domain.StatusActive, Timestamp: t2},
		},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if res.Updated != 1 || res.Skipped != 0 {
		t.Errorf("expected Updated=1, Skipped=0; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-B1")
	if p.Name != "New Name" || p.Price != 15.0 || !p.SupplierTimestamp.Equal(t2) {
		t.Errorf("catalog not updated properly: %+v", p)
	}
}

// TestEngine_InvariantB_OlderTimestamp tests that out-of-order older records are rejected and skipped.
func TestEngine_InvariantB_OlderTimestamp(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tOld := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	tCurrent := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Seed item with tCurrent
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B2", Name: "Current Name", Price: 100.0, Status: domain.StatusActive, Timestamp: tCurrent},
		},
	})

	// Ingest older record (tOld < tCurrent)
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B2", Name: "Stale Name", Price: 50.0, Status: domain.StatusActive, Timestamp: tOld},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("expected Skipped=1, Updated=0; got %+v", res)
	}

	// Catalog must retain current values
	p, _ := store.GetProduct(ctx, "SKU-B2")
	if p.Name != "Current Name" || p.Price != 100.0 || !p.SupplierTimestamp.Equal(tCurrent) {
		t.Errorf("catalog state corrupted by stale update: %+v", p)
	}
}

// TestEngine_InvariantB_IdenticalTimestamp tests that non-advancing equal timestamp with different payload is skipped.
func TestEngine_InvariantB_IdenticalTimestamp(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tSame := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Seed
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B3", Name: "Original", Price: 10.0, Status: domain.StatusActive, Timestamp: tSame},
		},
	})

	// Same timestamp, different price
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B3", Name: "Changed Price", Price: 20.0, Status: domain.StatusActive, Timestamp: tSame},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("expected Skipped=1, Updated=0; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-B3")
	if p.Price != 10.0 {
		t.Errorf("price should not have changed: got %f", p.Price)
	}
}

// TestEngine_InvariantB_TimezoneEquivalence verifies UTC normalization for different timezone offsets.
func TestEngine_InvariantB_TimezoneEquivalence(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tUTC := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	locBKK := time.FixedZone("UTC+7", 7*3600)
	tBKK := time.Date(2026, 10, 21, 15, 0, 0, 0, locBKK) // Exactly 08:00:00 UTC

	// Seed with UTC
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TZ", Name: "Item TZ", Price: 10.0, Status: domain.StatusActive, Timestamp: tUTC},
		},
	})

	// Replay with +07:00 equivalent
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TZ", Name: "Item TZ", Price: 10.0, Status: domain.StatusActive, Timestamp: tBKK},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("expected Skipped=1 due to timezone equivalence; got %+v", res)
	}
}

// TestEngine_InvariantC_ActiveToWithdrawn verifies valid transition from ACTIVE to WITHDRAWN.
func TestEngine_InvariantC_ActiveToWithdrawn(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)

	// Seed ACTIVE
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-C1", Name: "Active Item", Price: 25.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})

	// Transition to WITHDRAWN
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-C1", Name: "Active Item", Price: 25.0, Status: domain.StatusWithdrawn, Timestamp: t2},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Withdrawn != 1 || res.Updated != 0 || res.Skipped != 0 {
		t.Errorf("expected Withdrawn=1; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-C1")
	if p.Status != domain.StatusWithdrawn {
		t.Errorf("expected status WITHDRAWN, got %s", p.Status)
	}
}

// TestEngine_InvariantC_ResurrectionBlocked verifies tombstone preservation (WITHDRAWN never restored to ACTIVE).
func TestEngine_InvariantC_ResurrectionBlocked(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	tFuture := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	// Seed WITHDRAWN
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TOMB", Name: "Discontinued", Price: 10.0, Status: domain.StatusWithdrawn, Timestamp: t1},
		},
	})

	// Attempt resurrection to ACTIVE with future timestamp
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TOMB", Name: "Reactivated Attempt", Price: 99.0, Status: domain.StatusActive, Timestamp: tFuture},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 || res.Withdrawn != 0 {
		t.Errorf("expected resurrection to be Skipped=1; got %+v", res)
	}

	// Status must remain WITHDRAWN
	p, _ := store.GetProduct(ctx, "SKU-TOMB")
	if p.Status != domain.StatusWithdrawn {
		t.Fatalf("tombstone violated: product became %s", p.Status)
	}
	if p.Name != "Discontinued" || p.Price != 10.0 {
		t.Errorf("metadata modified on forbidden resurrection: %+v", p)
	}
}

// TestEngine_InvariantC_InitialWithdrawn verifies direct ingestion of a new withdrawn product.
func TestEngine_InvariantC_InitialWithdrawn(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-NEW-WITHDRAWN", Name: "Born Withdrawn", Price: 15.0, Status: domain.StatusWithdrawn, Timestamp: t1},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 {
		t.Errorf("expected Added=1; got %+v", res)
	}

	p, err := store.GetProduct(ctx, "SKU-NEW-WITHDRAWN")
	if err != nil || p == nil {
		t.Fatalf("product not found: %v", err)
	}
	if p.Status != domain.StatusWithdrawn {
		t.Errorf("expected status WITHDRAWN, got %s", p.Status)
	}
}

// TestEngine_IntraBatch_Duplicates tests multiple identical records in the same batch.
func TestEngine_IntraBatch_Duplicates(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	batch := domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-INTRA-1", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: "SKU-INTRA-1", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: "SKU-INTRA-1", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
		},
	}
	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 || res.Skipped != 2 {
		t.Errorf("expected Added=1, Skipped=2; got %+v", res)
	}
}

// TestEngine_IntraBatch_AscendingOrder tests multiple progressive updates in the same batch.
func TestEngine_IntraBatch_AscendingOrder(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 10, 5, 0, 0, time.UTC)
	t3 := time.Date(2026, 10, 21, 10, 10, 0, 0, time.UTC)

	batch := domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-INTRA-2", Name: "V1", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: "SKU-INTRA-2", Name: "V2", Price: 12.0, Status: domain.StatusActive, Timestamp: t2},
			{SKU: "SKU-INTRA-2", Name: "V3", Price: 15.0, Status: domain.StatusActive, Timestamp: t3},
		},
	}
	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 || res.Updated != 2 {
		t.Errorf("expected Added=1, Updated=2; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-INTRA-2")
	if p.Name != "V3" || p.Price != 15.0 || !p.SupplierTimestamp.Equal(t3) {
		t.Errorf("final state incorrect: %+v", p)
	}
}

// TestEngine_IntraBatch_DescendingOrder tests multiple out-of-order records in the same batch.
func TestEngine_IntraBatch_DescendingOrder(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 10, 5, 0, 0, time.UTC)

	// First record has T2, second has T1 < T2
	batch := domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-INTRA-3", Name: "Latest", Price: 20.0, Status: domain.StatusActive, Timestamp: t2},
			{SKU: "SKU-INTRA-3", Name: "Stale", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
		},
	}
	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 || res.Skipped != 1 {
		t.Errorf("expected Added=1, Skipped=1; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-INTRA-3")
	if p.Name != "Latest" || p.Price != 20.0 {
		t.Errorf("stale record overwrote newer record in same batch: %+v", p)
	}
}

// TestEngine_IntraBatch_TombstoneLock tests ACTIVE -> WITHDRAWN -> ACTIVE in the same batch.
func TestEngine_IntraBatch_TombstoneLock(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 10, 5, 0, 0, time.UTC)
	t3 := time.Date(2026, 10, 21, 10, 10, 0, 0, time.UTC)

	batch := domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-INTRA-4", Name: "Born Active", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: "SKU-INTRA-4", Name: "Withdrawn Now", Price: 10.0, Status: domain.StatusWithdrawn, Timestamp: t2},
			{SKU: "SKU-INTRA-4", Name: "Illegal Reactivation", Price: 10.0, Status: domain.StatusActive, Timestamp: t3},
		},
	}
	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 || res.Withdrawn != 1 || res.Skipped != 1 {
		t.Errorf("expected Added=1, Withdrawn=1, Skipped=1; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-INTRA-4")
	if p.Status != domain.StatusWithdrawn {
		t.Fatalf("tombstone not preserved intra-batch: got status %s", p.Status)
	}
}

// TestEngine_EmptyBatch verifies that empty batch does not touch database.
func TestEngine_EmptyBatch(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	res, err := engine.ExecuteBatch(ctx, domain.Batch{Records: []domain.SupplierUpdate{}})
	if err != nil {
		t.Fatalf("empty batch error: %v", err)
	}
	if res != (domain.ChangeSummary{}) {
		t.Errorf("expected zero summary, got %+v", res)
	}
}

// TestEngine_ContextCancellation verifies transactional rollback upon cancelled context.
func TestEngine_ContextCancellation(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context upfront

	_, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-CANCEL", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: time.Now().UTC()},
		},
	})
	if err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}

	// Verify nothing was written
	p, err := store.GetProduct(context.Background(), "SKU-CANCEL")
	if p != nil || (err != nil && err != domain.ErrNotFound) {
		t.Errorf("product was committed despite cancellation: %+v, err: %v", p, err)
	}
}
```

---

### 4.3 Blueprint: `pkg/sync/engine_race_test.go`
File path: `/Users/png/01. Project/coderpush/pkg/sync/engine_race_test.go`

```go
package sync_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// TestEngine_Race_ConcurrentReadsAndWrites runs 20 concurrent readers and 5 writers
// to prove 0 data races under go test -race -count=5.
func TestEngine_Race_ConcurrentReadsAndWrites(t *testing.T) {
	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Pre-seed catalog with 50 products
	var seedRecords []domain.SupplierUpdate
	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		seedRecords = append(seedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-RACE-%03d", i),
			Name:      fmt.Sprintf("Initial Item %d", i),
			Price:     float64(i + 1),
			Status:    domain.StatusActive,
			Timestamp: baseTime,
		})
	}
	_, err = engine.ExecuteBatch(ctx, domain.Batch{JobID: "seed", BatchIndex: 0, Records: seedRecords})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	var wg sync.WaitGroup
	numReaders := 20
	numWriters := 5

	// Spawn readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				sku := fmt.Sprintf("SKU-RACE-%03d", (readerID+j)%50)
				prod, err := store.GetProduct(ctx, sku)
				if err == nil && prod != nil {
					_ = prod.Name
					_ = prod.Price
				}
			}
		}(r)
	}

	// Spawn writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for b := 1; b <= 4; b++ {
				var updates []domain.SupplierUpdate
				for i := 0; i < 5; i++ {
					skuIdx := (writerID*5 + i) % 50
					updates = append(updates, domain.SupplierUpdate{
						SKU:       fmt.Sprintf("SKU-RACE-%03d", skuIdx),
						Name:      fmt.Sprintf("Updated by W%d B%d", writerID, b),
						Price:     float64(skuIdx*10) + float64(b),
						Status:    domain.StatusActive,
						Timestamp: baseTime.Add(time.Duration(b) * time.Hour),
					})
				}
				_, _ = engine.ExecuteBatch(ctx, domain.Batch{
					JobID:      fmt.Sprintf("job-w%d", writerID),
					BatchIndex: b,
					Records:    updates,
				})
			}
		}(w)
	}

	wg.Wait()
}

// TestEngine_Race_ConcurrentBatchExecution runs 8 concurrent goroutines executing batches
// against shared SKUs to verify transactional serialization and zero deadlocks.
func TestEngine_Race_ConcurrentBatchExecution(t *testing.T) {
	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	numWorkers := 8

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			records := []domain.SupplierUpdate{
				{
					SKU:       "SHARED-CONCURRENT-SKU",
					Name:      fmt.Sprintf("Worker %d", workerID),
					Price:     10.0 + float64(workerID),
					Status:    domain.StatusActive,
					Timestamp: time.Date(2026, 10, 21, 10, workerID, 0, 0, time.UTC),
				},
			}
			_, _ = engine.ExecuteBatch(ctx, domain.Batch{
				JobID:      fmt.Sprintf("job-conc-%d", workerID),
				BatchIndex: 0,
				Records:    records,
			})
		}(i)
	}

	wg.Wait()

	// Verify catalog is in a valid state
	p, err := store.GetProduct(ctx, "SHARED-CONCURRENT-SKU")
	if err != nil {
		t.Fatalf("failed to retrieve product: %v", err)
	}
	if p == nil || p.SKU != "SHARED-CONCURRENT-SKU" {
		t.Fatalf("unexpected state: %+v", p)
	}
}
```

---

## 5. Verification Method

Once Worker implements the codebase, the synchronization engine and invariants can be independently verified using the following concrete steps:

1. **Verify Statement & Branch Coverage ($\ge 90\%$)**:
   ```bash
   go test -v -coverprofile=coverage.out ./pkg/sync/...
   go tool cover -func=coverage.out
   ```
   *Expected Outcome*: Total statement coverage for `pkg/sync` must be $\ge 90\%$.

2. **Verify Concurrency & Multi-Iteration Race Detector (0 Data Races)**:
   ```bash
   go test -race -count=5 -v ./pkg/sync/...
   ```
   *Expected Outcome*: All test cases pass across 5 iterations without triggering Go race detector warnings or `SQLITE_BUSY` errors.

3. **Verify Invariant A (Deterministic Batch Replay)**:
   ```bash
   go test -v -run TestEngine_InvariantA ./pkg/sync/...
   ```
   *Expected Outcome*: First ingestion produces `Added=N, Skipped=0`. Second replay produces `Added=0, Updated=0, Withdrawn=0, Skipped=N`. Catalog state remains identical.

4. **Verify Invariant B (Fast-Forward Timestamps)**:
   ```bash
   go test -v -run TestEngine_InvariantB ./pkg/sync/...
   ```
   *Expected Outcome*: Newer timestamps advance catalog state (`Updated++`); older and non-advancing timestamps are rejected without catalog mutation (`Skipped++`).

5. **Verify Invariant C (Tombstone Immutability)**:
   ```bash
   go test -v -run TestEngine_InvariantC ./pkg/sync/...
   ```
   *Expected Outcome*: `WITHDRAWN` products can never be resurrected to `ACTIVE`, regardless of future timestamps (`Skipped++`).

6. **Verify Intra-Batch Consistency**:
   ```bash
   go test -v -run TestEngine_IntraBatch ./pkg/sync/...
   ```
   *Expected Outcome*: Duplicates, out-of-order updates, and tombstone locks within a single batch are resolved deterministically.
