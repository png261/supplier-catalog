# Technical Survey & Architecture Handoff: Storage & Synchronization Engine

## 1. Observation

### 1.1 Environment & Toolchain
- **Operating System & Architecture**: Darwin arm64 (macOS).
- **Go Version**: `go version go1.25.6 darwin/arm64`.
- **Git State**: No git repository initialized yet (`fatal: not a git repository`).
- **Disk Availability**: Root filesystem `/System/Volumes/Data` has approximately 145–151 MiB of free disk space remaining (`df -h` returned `151Mi Avail, 99% Capacity`). Intermediate build caches must be managed carefully.

### 1.2 Embedded Database Candidate Empirical Testing
Empirical compilation, binary sizing, and race detector testing was performed directly on the toolchain:

#### Candidate A: `modernc.org/sqlite`
- **Driver & CGO**: Pure Go port of SQLite translated from C via `ccgo`. Tested with `CGO_ENABLED=0`.
- **Version Compatibility**:
  - `modernc.org/sqlite@latest` (v1.60.1) requires Go toolchain `>= 1.26.0`.
  - Version `modernc.org/sqlite@v1.36.0` (and `v1.34.5`) requires `go 1.21` in its `go.mod` (`go list -m -json modernc.org/sqlite@v1.36.0` verified `GoVersion: "1.21"`), compiling seamlessly under `go 1.25.6` without automatic toolchain downloads.
- **Compiled Binary Size**:
  `CGO_ENABLED=0 go build -ldflags="-s -w" -o app_pure` produced a standalone binary of **6.1 MB** (`-rwxr-xr-x 1 png staff 6.1M`).
- **Race Detector Test**:
  Executed `go test -race -count=5 -v` on a concurrent SQLite workload with 10 goroutines performing concurrent inserts and updates.
  Result:
  ```
  === RUN   TestSQLiteRace
  --- PASS: TestSQLiteRace (0.01s)
  === RUN   TestSQLiteRace
  --- PASS: TestSQLiteRace (0.00s)
  === RUN   TestSQLiteRace
  --- PASS: TestSQLiteRace (0.01s)
  === RUN   TestSQLiteRace
  --- PASS: TestSQLiteRace (0.01s)
  === RUN   TestSQLiteRace
  --- PASS: TestSQLiteRace (0.00s)
  PASS
  ok   test_race 1.450s
  ```
  Passed with **0 data races**.

#### Candidate B: `go.etcd.io/bbolt` (v1.3.11)
- **Driver & CGO**: Native pure Go transactional key-value store. Compiles with `CGO_ENABLED=0`.
- **Compiled Binary Size**:
  `CGO_ENABLED=0 go build -ldflags="-s -w" -o app_bbolt` produced a standalone binary of **1.7 MB** (`-rwxr-xr-x 1 png staff 1.7M`).
- **Storage Paradigm**: Key-value B+ tree buckets. Lacks native SQL queries, secondary indexes, aggregation functions, and typed columns.

#### Candidate C: `github.com/mattn/go-sqlite3`
- **Driver & CGO**: Requires CGO (`CGO_ENABLED=1`), C compiler toolchain (gcc/clang), and dynamic C runtime (`musl-dev` on Alpine Linux, `glibc` on Debian/Ubuntu).
- **Cross-Compilation**: Fails compilation when `CGO_ENABLED=0`. Severely complicates multi-stage Docker builds targeting minimal images (`scratch` or `alpine`) and cross-architecture deployment.

---

## 2. Logic Chain

### 2.1 Database Selection: `modernc.org/sqlite` is Optimal
1. **Container & Size Constraint (R5 Acceptance Criteria)**: The final Docker image must be $\le 25\text{MB}$.
   - `modernc.org/sqlite` yields a stripped executable of 6.1 MB. Embedded in an Alpine Linux base image (~5 MB) or Scratch/Distroless image (~2 MB), the resulting container is ~8–12 MB, well below the 25 MB limit.
2. **CGO Independence & Portability**: Pure Go compilation (`CGO_ENABLED=0`) allows single-command cross-compilation for `linux/amd64` and `linux/arm64` without cross-compiler toolchains. `mattn/go-sqlite3` violates this requirement.
3. **Relational Querying & Staff Dashboard (R4)**: The Staff UI requires real-time change counts (+Added, ~Updated, -Withdrawn, ↷Skipped), paginated issues list with filtering, and checkpoint status inspection.
   - In SQLite, this is accomplished via standard SQL queries: `SELECT count(*) FROM products WHERE status = 'WITHDRAWN'`, `SELECT * FROM dead_letter_issues WHERE job_id = ? ORDER BY id DESC LIMIT 50`.
   - In `bbolt`, this would require serializing/deserializing JSON records, implementing manual in-memory indexing, and writing custom bucket iteration loops, introducing significant maintenance burden and potential data-structure bugs.
4. **ACID Transactions & Crash Resumability (R1, R2)**: SQLite provides true ACID batch transactions with Write-Ahead Logging (`WAL` mode). A batch transaction encapsulates catalog mutations, dead-letter entries, and checkpoint index updates in an atomic unit. If interrupted, SQLite's recovery automatically rolls back uncommitted batch writes.

### 2.2 Storage Schema & Indexing Architecture
Based on the domain model in R1, R2, and R4, the SQLite schema is designed as follows:

```sql
-- Pragmas for WAL mode, high concurrency, and data integrity
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;

-- 1. Products Catalog Table
CREATE TABLE IF NOT EXISTS products (
    sku TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    price REAL NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('ACTIVE', 'WITHDRAWN')),
    supplier_timestamp TEXT NOT NULL, -- UTC ISO-8601 / RFC3339 format
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_products_status ON products(status);

-- 2. Sync Jobs Table
CREATE TABLE IF NOT EXISTS sync_jobs (
    job_id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK(status IN ('PENDING', 'RUNNING', 'INTERRUPTED', 'COMPLETED', 'FAILED')),
    total_batches INTEGER NOT NULL DEFAULT 0,
    committed_batch_index INTEGER NOT NULL DEFAULT -1, -- 0-indexed; -1 means no batch committed yet
    items_added INTEGER NOT NULL DEFAULT 0,
    items_updated INTEGER NOT NULL DEFAULT 0,
    items_withdrawn INTEGER NOT NULL DEFAULT 0,
    items_skipped INTEGER NOT NULL DEFAULT 0,
    items_dead_letter INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT
);

-- 3. Batch Checkpoints Audit Table
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

-- 4. Dead-Letter Issues Table
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
```

### 2.3 Synchronization Engine & Invariant Enforcement Logic
For every batch $B_i$ in job $J$:
1. **Transaction Boundary**: The engine executes batch $B_i$ inside a single atomic database transaction: `tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelImmediate})`.
2. **Per-Record Invariant Decision Pipeline**:
   ```
   [Incoming Record R]
            │
            ▼
   ┌────────────────────────────────┐
   │ Invariant D: Validation Check │
   │ - SKU is non-empty?            │
   │ - Price >= 0.00?               │  No  ──► Insert into dead_letter_issues
   │ - Status in (ACTIVE,WITHDRAWN)?│          Increment batch.DeadLetterCount
   │ - Timestamp parseable?         │          Continue to next record (No batch failure)
   └────────────────────────────────┘
            │ Yes
            ▼
   ┌────────────────────────────────┐
   │ Fetch Local Product P by SKU   │
   └────────────────────────────────┘
            │
      ┌─────┴────────────────────────────────┐
      ▼ (Product Not Found)                  ▼ (Product Exists)
   ┌───────────────────────┐   ┌──────────────────────────────────────┐
   │ Insert New Product    │   │ Invariant B: Fast-Forward Check      │
   │ - status == ACTIVE:   │   │ - If T_incoming < T_local:           │
   │   AddedCount++        │   │   REJECT & SKIP (SkippedCount++)     │
   │ - status == WITHDRAWN:│   └──────────────────────────────────────┘
   │   WithdrawnCount++    │                  │
   └───────────────────────┘                  │ T_incoming >= T_local
                                              ▼
                               ┌──────────────────────────────────────┐
                               │ Invariant C: Tombstone Preservation  │
                               │ - If P.status == 'WITHDRAWN':        │
                               │   - If R.status == 'ACTIVE':         │
                               │     PREVENT REACTIVATION.            │
                               │     Keep status WITHDRAWN.           │
                               │     (Update metadata or SKIP)        │
                               └──────────────────────────────────────┘
                                              │
                                              ▼
                               ┌──────────────────────────────────────┐
                               │ Invariant A: Idempotency Check       │
                               │ - If R is identical to P:            │
                               │   No SQL UPDATE performed.           │
                               │   SkippedCount++                     │
                               │ - If R has newer valid updates:      │
                               │   Execute UPDATE; UpdatedCount++     │
                               └──────────────────────────────────────┘
   ```
3. **Batch Commit Atomicity**:
   Before committing `tx`, the engine:
   - Inserts the batch audit record into `batch_checkpoints (job_id, batch_index, added, updated, withdrawn, skipped, dead_letter)`.
   - Updates `sync_jobs` with new `committed_batch_index = i` and adds the batch's delta counts to cumulative metrics.
   - Executes `tx.Commit()`.

### 2.4 Crash Resumability & Recovery Protocol
1. **Application Crash Invariant**:
   - If process is killed while batch $k$ is running, uncommitted batch $k$ writes automatically roll back in SQLite WAL.
   - Batches $0 \dots k-1$ remain permanently committed.
   - `sync_jobs.committed_batch_index` strictly equals $k - 1$.
2. **Engine Startup Recovery**:
   - On engine initialization (`engine.RecoverOnStartup(ctx)`):
     ```sql
     SELECT job_id, committed_batch_index, total_batches 
     FROM sync_jobs 
     WHERE status IN ('RUNNING', 'PENDING');
     ```
   - For every active job detected from a prior crashed run:
     ```sql
     UPDATE sync_jobs 
     SET status = 'INTERRUPTED', updated_at = ? 
     WHERE job_id = ?;
     ```
     This satisfies Acceptance Criteria: *Interrupted jobs are marked `INTERRUPTED` upon restart and only transition to `COMPLETED` when all batches finish*.
3. **Resumption Protocol**:
   - When operator or API triggers `Resume(jobID)`:
     - Verify status is `INTERRUPTED`.
     - Update status to `RUNNING`.
     - Read `nextBatch = committed_batch_index + 1`.
     - Ingest starting from `nextBatch` up to `total_batches - 1`.
     - Prior batches ($0 \dots committed\_batch\_index$) are skipped without re-processing.
     - When last batch commits, transition status to `COMPLETED`.

### 2.5 Concurrency Model & Race Detector (`go test -race -count=5`)
1. **SQLite Write Serialization**: SQLite allows only one writer at a time. Concurrently calling `BEGIN IMMEDIATE` from multiple goroutines causes `SQLITE_BUSY`.
2. **Architecture**:
   - Connection string: `file:catalog.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)`
   - Database Connection Pool: `db.SetMaxOpenConns(max(4, runtime.NumCPU()))`.
   - Engine Mutex / Worker Pipeline:
     - Maintain an application-level write mutex (`sync.Mutex`) protecting write transaction execution, OR
     - Implement a single-worker background job pipeline receiving batch tasks through a Go channel (`chan BatchTask`).
     - HTTP Readers (`/api/catalog`, `/api/jobs/{id}`) read concurrently via standard `db.QueryContext` without blocking writers, leveraging SQLite WAL non-blocking readers.
     - This design completely eliminates SQLite write collisions, prevents data races in Go memory, and guarantees passing `go test -race -count=5 ./...`.

---

## 3. Caveats

1. **Local Disk Space**: The developer machine currently has ~145 MiB of available disk space on the primary volume. Build caches (`go build`, Docker builds) must avoid bloating temporary directories. Docker layers must be cleaned or pruned if Docker builds fail with `no space left on device`.
2. **Library Version Pinning**: `modernc.org/sqlite` latest (`v1.60.1`) enforces Go toolchain `>= 1.26.0`. To maintain compatibility with Go 1.25.6 and avoid unexpected toolchain switches, `modernc.org/sqlite` must be explicitly pinned to `v1.36.0` (or `v1.34.5`) in `go.mod`.
3. **Timestamp Precision & Format**: Supplier timestamps must be normalized to UTC RFC3339 strings (e.g. `2026-10-21T08:30:00.000000Z`) or parsed into `time.Time` prior to comparison. Floating-point or string comparisons without consistent UTC normalization could lead to incorrect ordering across daylight saving time transitions or differing offsets.
4. **Tombstone Status Semantics**: Invariant C dictates that `WITHDRAWN` is a terminal lifecycle state for sale. If an incoming update has $T_{incoming} > T_{local}$ but specifies `status = "ACTIVE"`, the status update must be ignored (preserving `WITHDRAWN`). Downstream implementers can either update non-status metadata (name, price) or drop the update as a rejected reactivation attempt. The recommended strategy is: preserve `WITHDRAWN`, log tombstone violation in audit counters, and ignore reactivation.

---

## 4. Conclusion

1. **Embedded Database**: Use `modernc.org/sqlite@v1.36.0`. It provides pure Go `CGO_ENABLED=0` cross-compilation, tiny binary footprint (6.1 MB, supporting $\le 25\text{MB}$ Docker containers), standard SQL querying for dashboard stats and dead-letter inspection, and verified zero data races under `go test -race -count=5`.
2. **Schema & Checkpoints**: Implement the 4-table schema (`products`, `sync_jobs`, `batch_checkpoints`, `dead_letter_issues`).
3. **Atomic Batch Transactions**: Wrap each batch in a `BEGIN IMMEDIATE` transaction committing products, dead-letter records, and checkpoint index in a single commit.
4. **Crash Recovery**: Implement engine startup detection to transition uncompleted `RUNNING` jobs to `INTERRUPTED`, and resume from `committed_batch_index + 1`.
5. **Concurrency**: Use WAL mode with an application write mutex/worker pipeline, allowing safe concurrent reads and serialized writes.

---

## 5. Verification Method

To independently verify these conclusions and architectural choices:

1. **Verify Binary Size & CGO_ENABLED=0**:
   ```bash
   CGO_ENABLED=0 go build -ldflags="-s -w" -o test_bin .
   ls -lh test_bin # Must be <= 10MB
   ```
2. **Verify Multi-Iteration Race Detector Compatibility**:
   ```bash
   go test -race -count=5 ./...
   ```
   Must pass with 0 data races.
3. **Verify Invariant A (Idempotency)**:
   Run sync engine with identical batch twice. Check:
   - `items_added == 0` on second run.
   - `items_updated == 0` on second run.
   - Total rows in `products` unchanged.
4. **Verify Invariant B (Fast-Forward Out-of-Order Rejection)**:
   Ingest product with timestamp $T_1 = 100$, then ingest same SKU with $T_2 = 50$. Check:
   - Record with $T_2$ is skipped (`items_skipped == 1`).
   - Catalog retains record with $T_1$.
5. **Verify Invariant C (Tombstone Preservation)**:
   Ingest product with `status = "WITHDRAWN"` at $T_1 = 100$. Ingest same SKU with `status = "ACTIVE"` at $T_2 = 200$. Check:
   - Local catalog status remains `WITHDRAWN`.
6. **Verify Invariant D (Dead-Letter Isolation)**:
   Ingest batch containing 1 valid item, 1 item with price `-5.00`, and 1 item with empty SKU `""`. Check:
   - Valid item is committed to `products`.
   - Batch does not fail.
   - 2 records inserted into `dead_letter_issues`.
7. **Verify Invariant E (Crash Resumability)**:
   Simulate panic/abort after batch 2 of a 5-batch job. Re-initialize engine. Check:
   - Job is automatically marked `INTERRUPTED`.
   - Batches 0 and 1 are intact.
   - Resuming job starts directly from batch 2 and completes through batch 4.
