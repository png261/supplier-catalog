# Specification Mining & Test Architecture Report: Milestone 1
**Target**: Milestone 1 (Core Domain, Storage Layer & Synchronization Engine)  
**Author**: `teamwork_preview_spec_miner` (`spec_miner_m1_1`)  
**Parent Agent**: `e6900bb2-6639-41ea-b0fb-48d815c2c1c8`  
**Date**: 2026-10-21  

---

## 1. Observation

### 1.1 Specification Sources & Direct Citations
1. **`ORIGINAL_REQUEST.md`**:
   - **R1. Deterministic Batch Catalog Synchronization & Invariants**:
     - Line 13: *"Ingest supplier product batches and store a local copy using an embedded database."*
     - Line 14: *"Enforce idempotency: importing the exact same update multiple times must never duplicate products or drift catalog state."*
     - Line 15: *"Enforce fast-forward out-of-order resolution: incoming older information (based on supplier timestamp) must not overwrite newer local information."*
     - Line 16: *"Enforce tombstone preservation: a product marked as withdrawn from sale must never be restored to active sale by older or subsequent update payloads."*
     - Line 40: *"Ingesting the same batch twice results in 0 additional inserts, 0 updates, and identical catalog state."*
     - Line 41: *"An incoming record with timestamp $T_{new} < T_{old}$ is rejected and counted as skipped."*
     - Line 42: *"A product with status `WITHDRAWN` is never flipped back to `ACTIVE`."*
   - **R5. Security, Test Rigor & CI/CD Packaging**:
     - Line 51: *"Automated test suite achieves $\ge 90\%$ statement/branch coverage across the project."*
     - Line 52: *"Multi-iteration test run (`go test -race -count=5 ./...`) passes with 0 data races."*

2. **`PROJECT.md`**:
   - Lines 4-17: Embedded SQLite (`modernc.org/sqlite@v1.36.0`) in WAL mode (`PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;`).
   - Lines 81-128: Canonical Domain Entities:
     - `Product`: `SKU`, `Name`, `Price`, `Status` (`ACTIVE` | `WITHDRAWN`), `SupplierTimestamp` (`time.Time`), `UpdatedAt` (`time.Time`).
     - `SupplierUpdate`: `SKU`, `Name`, `Price`, `Status`, `Timestamp`.
     - `Batch`: `JobID`, `BatchIndex`, `Records []SupplierUpdate`.
     - `ChangeSummary`: `Added int`, `Updated int`, `Withdrawn int`, `Skipped int`, `DeadLetter int`.
   - Lines 130-186: Interface Contracts:
     - `Storage`: `GetProduct(ctx, sku)`, `UpsertProductTx(ctx, tx, p)`, `Close() error`, `DB() *sql.DB`.
     - `Engine`: `ExecuteBatch(ctx, batch) (ChangeSummary, error)`.

3. **`DISPATCH.md` (`spec_miner_m1_1`)**:
   - Unit tests for Invariant A: batch replay producing 0 adds, 0 updates, 0 deletes, exact catalog state preservation.
   - Unit tests for Invariant B: strict timestamp ordering ($T_{new} < T_{old}$ dropped, $T_{new} == T_{old}$ deduplicated, $T_{new} > T_{old}$ applied).
   - Unit tests for Invariant C: tombstone immutability (`ACTIVE` $\rightarrow$ `WITHDRAWN` allowed; `WITHDRAWN` $\rightarrow$ `ACTIVE` strictly prohibited even with future timestamps).
   - Intra-batch duplicate handling (multiple records for same SKU in a single batch).
   - Concurrency test specifications for `go test -race -count=5 ./...`.

### 1.2 Toolchain & Empirical Constraints
- **Host System**: macOS Darwin arm64 (`go version go1.25.6 darwin/arm64`).
- **Disk Availability**: Primary volume has ~145–151 MiB of free space. Disk writes must be scoped and bounded; test databases must utilize `:memory:` or temporary files removed during `t.Cleanup()`.
- **Database Driver**: `modernc.org/sqlite@v1.36.0` (pure Go, `CGO_ENABLED=0`).
  - Empirical verification in technical survey confirmed clean compilation and 0 data races under `go test -race -count=5`.
  - SQLite WAL mode permits concurrent readers with a single writer. Single-writer synchronization must be enforced via connection pooling (`SetMaxOpenConns(1)`) or engine-level mutex to avoid `SQLITE_BUSY` errors.

---

## 2. Features Discovered

The following features constitute the complete observable specification for Milestone 1 across `pkg/storage` and `pkg/sync`:

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Storage | SQLite Database Initialization | Initialize SQLite storage with WAL mode, busy timeout (5000ms), and schema setup | Connection string / path (e.g. `:memory:` or file path) | Initialized `*sql.DB` with tables created | Invalid path or read-only directory returns error | `PROJECT.md` §1, `ORIGINAL_REQUEST.md` R1 |
| 2 | Storage | Pragmas Configuration | Enforce `PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON;` | Executed upon DB connection | Pragmas successfully set | Unsupported pragma or SQL syntax error returns error | `PROJECT.md` §1, Survey handoff |
| 3 | Storage | Product Schema & Table Creation | Create `products` table with primary key `sku` and status check constraint `CHECK(status IN ('ACTIVE','WITHDRAWN'))` | DDL execution | Table ready for queries | Corrupted schema or syntax error returns error | `PROJECT.md` §1, Survey handoff |
| 4 | Storage | Product Retrieval (`GetProduct`) | Retrieve single product by primary key SKU | `ctx context.Context`, `sku string` | `(*domain.Product, nil)` | Non-existent SKU returns `(nil, ErrNotFound)` (or `nil, nil`); cancelled ctx returns error | `PROJECT.md` §Interface Contracts |
| 5 | Storage | Transactional Upsert (`UpsertProductTx`) | Insert or replace product within an open `*sql.Tx` | `ctx context.Context`, `tx *sql.Tx`, `*domain.Product` | `nil` on success | Nil transaction, closed tx, or invalid status constraint returns error | `PROJECT.md` §Interface Contracts |
| 6 | Storage | Storage Lifecycle & Cleanup (`Close`) | Gracefully close underlying SQLite connection pool and release file locks | None | `error` (nil if closed cleanly) | Closing already closed storage returns error or nil | `PROJECT.md` §Interface Contracts |
| 7 | Sync Engine | Atomic Batch Transaction Lifecycle | Execute entire batch inside an atomic `BEGIN IMMEDIATE` transaction; commit on success, rollback on failure | `ctx context.Context`, `batch domain.Batch` | `(domain.ChangeSummary, error)` | Panic or error during batch triggers full transaction rollback; returns error | `PROJECT.md` §2, `ORIGINAL_REQUEST.md` R1 |
| 8 | Sync Engine | Invariant A: Full Batch Replay Idempotency | Ingesting the identical batch twice produces 0 adds, 0 updates, 0 withdrawals, and unchanged catalog state | Batch $B$ ingested, followed immediately by identical Batch $B$ | 1st Run: `Added=N, Updated=0, Skipped=0`. 2nd Run: `Added=0, Updated=0, Skipped=N` | None (valid idempotent flow) | `ORIGINAL_REQUEST.md` R1 & AC |
| 9 | Sync Engine | Invariant A: Partial Batch Replay | Batch containing 50% new items and 50% replayed identical items | Batch $B_2$ with overlapping SKUs and identical fields | `Added=len(new)`, `Skipped=len(overlap)`, `Updated=0` | None | `ORIGINAL_REQUEST.md` R1 |
| 10 | Sync Engine | Invariant B: Monotonic Fast-Forwarding | Incoming record with $T_{incoming} > T_{local}$ updates existing product | Existing product ($T_1$), Incoming update ($T_2 > T_1$) | Catalog updated to $T_2$; `Updated++` (or `Withdrawn++`) | None | `ORIGINAL_REQUEST.md` R1 & AC, `PROJECT.md` |
| 11 | Sync Engine | Invariant B: Out-of-Order Timestamp Rejection | Incoming record with $T_{incoming} < T_{local}$ is rejected without modifying catalog | Existing product ($T_2$), Incoming update ($T_1 < T_2$) | Catalog stays at $T_2$; `Skipped++` | None | `ORIGINAL_REQUEST.md` R1 & AC |
| 12 | Sync Engine | Invariant B: Identical Timestamp Deduplication | Incoming record with $T_{incoming} == T_{local}$ is deduplicated without modifying catalog | Existing product ($T_1$), Incoming update ($T_1$) | Catalog stays at $T_1$; `Skipped++` | None | `DISPATCH.md` line 11 |
| 13 | Sync Engine | Invariant B: UTC Normalization | Timestamps with different timezone offsets (e.g. `+07:00` vs `Z`) comparing the exact same instant | Incoming timestamp $T_{local} = \text{15:00+07:00}$, Incoming $T_{in} = \text{08:00Z}$ | Identified as $T_{in} == T_{local}$; `Skipped++` | None | Discovered via UTC spec analysis |
| 14 | Sync Engine | Invariant B: Sub-Millisecond Precision | Timestamps differing only by microseconds or nanoseconds | Existing $T_1 = \text{...000001Z}$, Incoming $T_2 = \text{...000002Z}$ | Properly recognized as $T_2 > T_1$; `Updated++` | None | Discovered via storage schema analysis |
| 15 | Sync Engine | Invariant C: Tombstone Transition (ACTIVE $\rightarrow$ WITHDRAWN) | An active product is withdrawn when incoming record has status `WITHDRAWN` and $T_{in} > T_{local}$ | Existing `ACTIVE`, Incoming `WITHDRAWN` ($T_{in} > T_{local}$) | Product status becomes `WITHDRAWN`; `Withdrawn++` | None | `ORIGINAL_REQUEST.md` R1, `DISPATCH.md` |
| 16 | Sync Engine | Invariant C: Tombstone Resurrection Block | A withdrawn product is NEVER restored to active sale, even with distant future timestamps | Existing `WITHDRAWN` ($T_1$), Incoming `ACTIVE` ($T_2 \gg T_1$) | Status remains `WITHDRAWN`; incoming update rejected; `Skipped++` | None | `ORIGINAL_REQUEST.md` R1 & AC |
| 17 | Sync Engine | Invariant C: Tombstone Metadata Update | An incoming record for a withdrawn product that maintains status `WITHDRAWN` with $T_{in} > T_{local}$ | Existing `WITHDRAWN` ($T_1$), Incoming `WITHDRAWN` ($T_2 > T_1$) with changed price | Status stays `WITHDRAWN`; price updated; `Updated++` (or skipped per tombstone lock) | None | Discovered via Invariant C edge analysis |
| 18 | Sync Engine | Invariant C: Direct Initial Withdrawn Ingestion | Ingesting a brand new product that arrives with status `WITHDRAWN` | Non-existent SKU, Incoming `WITHDRAWN` | Inserted as `WITHDRAWN`; `Added++` | None | Discovered via domain analysis |
| 19 | Sync Engine | Intra-Batch Duplicate: Exact Match | Multiple identical records for the same SKU in the same batch | Batch with `[SKU-1, T1]`, `[SKU-1, T1]` | Record 1 inserted (`Added++`), Record 2 dropped (`Skipped++`) | None | `DISPATCH.md` line 13 |
| 20 | Sync Engine | Intra-Batch Duplicate: Ascending Timestamps | Multiple records for the same SKU in ascending timestamp order within same batch | Batch with `[SKU-1, T1]`, `[SKU-1, T2]`, `[SKU-1, T3]` ($T_1 < T_2 < T_3$) | Record 1 added (`Added++`), Records 2 & 3 updated (`Updated+=2`); final state has $T_3$ | None | `DISPATCH.md` line 13 |
| 21 | Sync Engine | Intra-Batch Duplicate: Descending (Out-of-Order) | Multiple records for the same SKU in descending order within same batch | Batch with `[SKU-1, T3]`, `[SKU-1, T2]`, `[SKU-1, T1]` ($T_3 > T_2 > T_1$) | Record 1 added (`Added++`), Records 2 & 3 skipped (`Skipped+=2`); final state has $T_3$ | None | `DISPATCH.md` line 13 |
| 22 | Sync Engine | Intra-Batch Duplicate: Tombstone Lock | Ingesting `ACTIVE`, then `WITHDRAWN`, then `ACTIVE` in the exact same batch | Batch with `[SKU-1, ACTIVE, T1]`, `[SKU-1, WITHDRAWN, T2]`, `[SKU-1, ACTIVE, T3]` | Record 1 Added, Record 2 Withdrawn, Record 3 Skipped (Resurrection blocked); Final status `WITHDRAWN` | None | `DISPATCH.md` line 12-13 |
| 23 | Sync Engine | Empty Batch Handling | Processing a batch with zero records | `batch.Records = []` | `ChangeSummary{}` (all zeros); DB unmodified; no error | None | Boundary value analysis |
| 24 | Sync Engine | Context Cancellation & Transaction Rollback | Cancelling `context.Context` during or prior to batch processing | Canceled or timed-out `ctx` | Transaction aborted and rolled back; returns `context.Canceled` | `context.Canceled` or `context.DeadlineExceeded` | ACID verification |
| 25 | Concurrency | Concurrent Product Readers | Multiple goroutines reading products concurrently during active batch sync | 50 concurrent reader goroutines calling `GetProduct` | Consistent snapshot data returned without blocking writer | None; 0 data races under `-race` | `ORIGINAL_REQUEST.md` R5, `TEST_INFRA.md` |
| 26 | Concurrency | Serialized Concurrent Batch Ingestion | Multiple goroutines invoking `ExecuteBatch` simultaneously | 10 concurrent goroutines executing distinct batches | Batches serialized safely (via mutex or SQLite lock wait); all batches committed | No `SQLITE_BUSY` crash; 0 data races under `-race -count=5` | `ORIGINAL_REQUEST.md` R5, `TEST_INFRA.md` |

---

## 3. Edge Cases Matrix

The following matrix documents boundary values, extreme conditions, and observed/required behaviors across all M1 features:

| # | Feature | Input / Condition | Observed / Required Behavior |
|---|---------|-------------------|-----------------------------|
| E1 | Invariant A | Replay of empty batch (`Records: []`) | Returns `Added=0, Updated=0, Withdrawn=0, Skipped=0`; DB state unchanged. |
| E2 | Invariant A | Replay of batch containing 1,000 distinct products | 1st run: `Added=1000, Skipped=0`. 2nd run: `Added=0, Updated=0, Withdrawn=0, Skipped=1000`. Product table row count remains 1,000. |
| E3 | Invariant A | Replay of mixed batch (previously updated + newly added items) | Exact state preserved; zero rows modified in DB; all replayed records increment `Skipped`. |
| E4 | Invariant B | $T_{incoming}$ is 1 nanosecond older than $T_{local}$ ($T_{in} = T_{local} - 1\text{ns}$) | Rejected and skipped (`Skipped++`). Local record unchanged. |
| E5 | Invariant B | $T_{incoming}$ is 1 nanosecond newer than $T_{local}$ ($T_{in} = T_{local} + 1\text{ns}$) | Applied (`Updated++`). Local record updated to $T_{incoming}$. |
| E6 | Invariant B | $T_{incoming}$ equals Unix Epoch (`1970-01-01T00:00:00Z`) when local has $T_{local} = \text{2026-10-21}$ | Rejected and skipped (`Skipped++`). Local record unchanged. |
| E7 | Invariant B | $T_{incoming}$ equals Go zero-value (`0001-01-01T00:00:00Z`) | Rejected and skipped (`Skipped++`). Local record unchanged. |
| E8 | Invariant B | $T_{incoming}$ equals distant future (`2099-12-31T23:59:59Z`) | Applied as newest timestamp (`Updated++`). |
| E9 | Invariant B | $T_{incoming}$ has non-UTC timezone offset equivalent to $T_{local}$ (`2026-10-21T15:00:00+07:00` vs `2026-10-21T08:00:00Z`) | Normalized to UTC instant; recognized as $T_{in} == T_{local}$; dropped as duplicate (`Skipped++`). |
| E10 | Invariant B | $T_{incoming} == T_{local}$ but product name or price is different | Strict ordering drops non-advancing timestamps ($T_{new} == T_{old}$ deduplicated); record skipped (`Skipped++`), catalog retains existing price/name. |
| E11 | Invariant C | Local is `WITHDRAWN` at $T_1$; Incoming is `ACTIVE` at $T_2$ ($T_2 = T_1 + 10\text{ years}$) | Strict tombstone immutability: reactivation is rejected. Status remains `WITHDRAWN`, `Skipped++`. |
| E12 | Invariant C | Local is `WITHDRAWN` at $T_1$; Incoming is `ACTIVE` at $T_0 < T_1$ | Out-of-order AND resurrection: rejected, status remains `WITHDRAWN`, `Skipped++`. |
| E13 | Invariant C | Local is `ACTIVE` at $T_1$; Incoming is `WITHDRAWN` at $T_2 > T_1$ | Permitted transition: status transitions to `WITHDRAWN`, `Withdrawn++`. |
| E14 | Invariant C | Local is `WITHDRAWN` at $T_1$; Incoming is `WITHDRAWN` at $T_2 > T_1$ with updated price | Status remains `WITHDRAWN`; price updated; `Updated++`. |
| E15 | Invariant C | Local is `WITHDRAWN` at $T_1$; Incoming is `WITHDRAWN` at $T_1$ with identical data | Idempotent replay on tombstone: `Skipped++`, 0 DB writes. |
| E16 | Intra-Batch | Single batch contains 10 identical records for same SKU | 1st record creates product (`Added++`). Subsequent 9 records dropped as $T_{in} == T_{local}$ (`Skipped+=9`). Summary: Added=1, Skipped=9. |
| E17 | Intra-Batch | Single batch contains 5 records for same SKU in descending order: $T_5, T_4, T_3, T_2, T_1$ ($T_5 > \dots > T_1$) | 1st record ($T_5$) inserted (`Added++`). Subsequent 4 records have $T < T_5$, all dropped (`Skipped+=4`). Final state has $T_5$. |
| E18 | Intra-Batch | Single batch contains: `[ACTIVE, T1]`, `[WITHDRAWN, T2]`, `[ACTIVE, T3]` ($T_1 < T_2 < T_3$) | Record 1 inserts ACTIVE (`Added++`); Record 2 transitions to WITHDRAWN (`Withdrawn++`); Record 3 attempts resurrection, blocked (`Skipped++`). Final status `WITHDRAWN`. |
| E19 | Intra-Batch | DB already has `[ACTIVE, T2]`. Batch contains `[ACTIVE, T1]`, `[ACTIVE, T2]`, `[ACTIVE, T3]` ($T_1 < T_2 < T_3$) | Record 1 skipped ($T_1 < T_2$); Record 2 skipped ($T_2 == T_2$); Record 3 applied ($T_3 > T_2$, `Updated++`). Summary: Skipped=2, Updated=1. |
| E20 | Storage | SKU containing special characters: hyphens, slashes, colons (`SKU-PROD/123:ABC`) | Successfully stored and retrieved with exact character match. |
| E21 | Storage | SKU containing SQL injection payload (`SKU'; DROP TABLE products; --`) | Handled safely via parameter binding (`?`); stored literally; tables intact. |
| E22 | Storage | SKU containing Unicode & Emojis (`SKU-🔥-日本語-001`) | UTF-8 encoding safely stored and retrieved. |
| E23 | Storage | Product price is zero (`0.00`) | Stored as valid price $0.00 (free item allowed). |
| E24 | Storage | Product price is maximum float64 / large currency value (`999999999.99`) | Accurate IEEE 754 float representation stored and retrieved. |
| E25 | Storage | Product Name is empty string (`""`) | Stored successfully if schema permits, or tested against validation constraints. |
| E26 | Storage | Product Name is extremely long (10,000 characters) | Stored without truncation or buffer overflow. |
| E27 | Storage | Database path is `:memory:` vs file path on disk | Both operational; `:memory:` creates ephemeral instance, disk path persists across connection closures. |
| E28 | Sync Engine | Context cancelled prior to `ExecuteBatch` | Returns `context.Canceled` immediately without opening transaction. |
| E29 | Sync Engine | Context cancelled mid-batch during record iteration | Transaction rolls back; 0 records committed to catalog; error returned. |
| E30 | Concurrency | 50 goroutines executing `GetProduct` during a 500-product batch commit | Readers execute non-blocking under WAL; readers see either pre-batch or post-batch state; 0 dirty reads; 0 data races. |
| E31 | Concurrency | 10 goroutines calling `ExecuteBatch` concurrently on separate batches | Writes serialized without deadlocks; all 10 batches committed successfully; 0 data races under `go test -race -count=5`. |

---

## 4. Logic Chain

### 4.1 Invariant A (Deterministic Idempotency)
- **Premise**: In an e-commerce synchronization system, network timeouts or retries frequently result in duplicate batch deliveries. The catalog must act as a deterministic state machine.
- **Formal Definition**: Let $S$ denote the catalog state: $S = \{ (sku, name, price, status, T) \}$. Let $B$ denote a batch of updates.
  The transition function is $f: (S, B) \rightarrow S'$.
  Idempotency requires:
  $$f(f(S, B), B) = f(S, B)$$
- **Change Counter Invariant**:
  When evaluating delta metrics $\Delta = (Added, Updated, Withdrawn, Skipped, DeadLetter)$:
  $$\Delta(S', B) = (Added: 0, Updated: 0, Withdrawn: 0, Skipped: |B|, DeadLetter: 0)$$
- **Test Strategy**:
  1. Ingest batch $B_1$ into an empty database. Verify $Added = |B_1|$, $Skipped = 0$.
  2. Snapshot database rows (SKU, Name, Price, Status, SupplierTimestamp, UpdatedAt).
  3. Re-ingest batch $B_1$ immediately. Verify $Added = 0$, $Updated = 0$, $Withdrawn = 0$, $Skipped = |B_1|$.
  4. Compare snapshot with current database rows: verify exact bit-for-bit equality.

### 4.2 Invariant B (Strict Timestamp Ordering & Fast-Forwarding)
- **Premise**: Supplier updates arrive out of chronological order due to partitioned queues or asynchronous webhooks. Only newer updates must take effect.
- **Evaluation Rule**:
  For an incoming record $R$ with timestamp $T_{in}$ matching existing product $P$ with timestamp $T_{local}$:
  1. Normalize both $T_{in}$ and $T_{local}$ to UTC.
  2. Compare instants using `time.Time.Before()`, `After()`, and `Equal()`:
     - If $T_{in} < T_{local}$: **REJECT**. Local product unmodified. Delta metric: `Skipped++`.
     - If $T_{in} == T_{local}$: **DEDUPLICATE**. Local product unmodified. Delta metric: `Skipped++`.
     - If $T_{in} > T_{local}$: **PROCEED** to Invariant C evaluation. If accepted, local product updated to $R$, $T_{local} \leftarrow T_{in}$. Delta metric: `Updated++`.
- **Test Strategy**:
  1. Seed product with $T = 2026-10-21T10:00:00Z$.
  2. Send update with $T = 2026-10-21T09:00:00Z$. Assert: Skipped=1, DB timestamp remains 10:00:00Z.
  3. Send update with $T = 2026-10-21T10:00:00Z$. Assert: Skipped=1, DB timestamp remains 10:00:00Z.
  4. Send update with $T = 2026-10-21T11:00:00Z$. Assert: Updated=1, DB timestamp advances to 11:00:00Z.

### 4.3 Invariant C (Tombstone Immutability / Monotonic Status)
- **Premise**: When a product is withdrawn from sale (e.g. regulatory compliance, recall, discontinued line), it must NEVER be resurrected to `ACTIVE` by subsequent or out-of-order supplier feeds.
- **State Transition Monotonicity**:
  Let $Status \in \{\text{ACTIVE}, \text{WITHDRAWN}\}$. The partial order is:
  $$\text{ACTIVE} \prec \text{WITHDRAWN}$$
  Transitions must be monotonically non-decreasing:
  - $\text{ACTIVE} \rightarrow \text{WITHDRAWN}$: Allowed when $T_{in} > T_{local}$.
  - $\text{WITHDRAWN} \rightarrow \text{ACTIVE}$: **STRICTLY FORBIDDEN** $\forall T_{in} \in (-\infty, +\infty)$.
- **Test Strategy**:
  1. Seed product as `ACTIVE` with $T_1$.
  2. Ingest update with `status = "WITHDRAWN"` and $T_2 > T_1$. Assert: status in DB becomes `WITHDRAWN`, `Withdrawn++`.
  3. Ingest update with `status = "ACTIVE"` and $T_3 > T_2$ (future timestamp). Assert: status in DB remains `WITHDRAWN`, `Skipped++`.
  4. Ingest update with `status = "ACTIVE"` and $T_0 < T_2$ (past timestamp). Assert: status in DB remains `WITHDRAWN`, `Skipped++`.

### 4.4 Intra-Batch Duplicate Resolution
- **Premise**: A single batch payload from a supplier may contain multiple updates for the same SKU (e.g. price change followed by withdrawal).
- **Resolution Pipeline**:
  Processing within a batch transaction must apply updates sequentially against the intermediate uncommitted transaction state:
  - Record $R_1$: Applied or inserted.
  - Record $R_2$ (same SKU): Evaluated against the state left by $R_1$.
  - Invariants A, B, and C apply transitively between $R_1$ and $R_2$.
- **Test Strategy**:
  1. Test intra-batch duplicate with identical timestamps: verify 1 insert, 1 skip.
  2. Test intra-batch updates with ascending timestamps: verify 1 insert, 1 update.
  3. Test intra-batch updates with descending timestamps: verify 1 insert, 1 skip.
  4. Test intra-batch tombstone followed by reactivation attempt: verify 1 insert (ACTIVE), 1 withdrawal (WITHDRAWN), 1 skip (reactivation blocked); final status = WITHDRAWN.

### 4.5 Concurrency Model & Race Detector (`go test -race -count=5 ./...`)
- **Premise**: Acceptance criteria require zero data races over 5 iterations under `go test -race -count=5 ./...`.
- **Architectural Rules**:
  1. **SQLite Write Serialization**: SQLite does not support concurrent write transactions on the same database file without returning `SQLITE_BUSY`.
     - Pure Go driver `modernc.org/sqlite` handles WAL readers concurrently, but write transactions (`BEGIN IMMEDIATE`) must be synchronized.
     - The engine implementation must protect `ExecuteBatch` with an internal mutex (`sync.Mutex`) or configure `SetMaxOpenConns(1)` on the write pool.
  2. **Concurrent Read Isolation**: `GetProduct` must use `db.QueryContext` or read-only connection without locking writers.
  3. **Data Race Prevention**: Shared Go structs (`Engine`, counters, caches) must not perform unsynchronized field reads/writes across goroutines.

---

## 5. Concrete Test Specifications

### 5.1 Test Suite Layout
Following `PROJECT.md` code layout:
```
pkg/
├── storage/
│   ├── sqlite.go            # SQLite storage implementation
│   ├── sqlite_test.go       # Storage unit tests (CRUD, Pragmas, Transactions)
├── sync/
│   ├── engine.go            # Batch sync engine & invariant enforcement
│   ├── engine_test.go       # Sync engine unit tests (Invariants A, B, C, Intra-batch)
│   └── engine_race_test.go  # Concurrency & race detector tests (-race -count=5)
```

### 5.2 Storage Layer Test Suite Specification (`pkg/storage/sqlite_test.go`)

| Test Function | Target Feature | Scenario / Input | Expected Result |
|---|---|---|---|
| `TestStorage_Open_Memory` | SQLite Init | Open `:memory:` database | Returns active `Storage` without error; `DB().Ping()` succeeds. |
| `TestStorage_Open_Pragmas` | Pragmas | Query `PRAGMA journal_mode`, `PRAGMA busy_timeout` | Journal mode is `wal` (or `memory` for in-memory DB); busy timeout is 5000. |
| `TestStorage_GetProduct_NotFound` | Lookup | Query non-existent SKU `"SKU-MISSING"` | Returns `(nil, domain.ErrProductNotFound)` (or defined sentinel error). |
| `TestStorage_Upsert_Insert` | Upsert | Insert valid product `SKU-001`, Price 19.99, Status ACTIVE | Successfully written; `GetProduct("SKU-001")` returns matching fields. |
| `TestStorage_Upsert_Update` | Upsert | Insert `SKU-001`, then update price to 24.99 and name | `GetProduct("SKU-001")` returns updated price 24.99. |
| `TestStorage_Upsert_InvalidStatus` | Constraints | Upsert product with status `"INVALID_STATUS"` | Returns SQLite constraint error (`CHECK(status IN ('ACTIVE','WITHDRAWN'))`). |
| `TestStorage_Transaction_Rollback` | Transaction | Begin Tx, insert product, call `tx.Rollback()` | Product is NOT present in database upon subsequent `GetProduct`. |
| `TestStorage_Transaction_Commit` | Transaction | Begin Tx, insert product, call `tx.Commit()` | Product is durably present in database. |
| `TestStorage_Close` | Lifecycle | Call `storage.Close()` | Database connections closed; subsequent queries fail with `sql.ErrConnDone`. |

### 5.3 Sync Engine Test Suite Specification (`pkg/sync/engine_test.go`)

| Test Function | Invariant | Scenario / Input | Expected Metric & Catalog State |
|---|---|---|---|
| `TestEngine_InvariantA_BatchReplay` | Invariant A | Ingest 5 products in Batch 1. Re-ingest exact same Batch 1. | Run 1: `Added=5, Skipped=0`. Run 2: `Added=0, Updated=0, Withdrawn=0, Skipped=5`. Catalog state unchanged. |
| `TestEngine_InvariantA_PartialOverlap` | Invariant A | Ingest Batch 1 (SKU 1..3). Ingest Batch 2 (SKU 2..5, identical for 2 & 3). | Batch 2: `Added=2 (SKU 4,5)`, `Skipped=2 (SKU 2,3)`. |
| `TestEngine_InvariantB_NewerTimestamp` | Invariant B | Seed SKU with $T_1$. Ingest update with $T_2 > T_1$. | Metric: `Updated=1`. Catalog timestamp advances to $T_2$. |
| `TestEngine_InvariantB_OlderTimestamp` | Invariant B | Seed SKU with $T_2$. Ingest update with $T_1 < T_2$. | Metric: `Skipped=1`. Catalog timestamp remains $T_2$, price unchanged. |
| `TestEngine_InvariantB_IdenticalTimestamp` | Invariant B | Seed SKU with $T_1$. Ingest update with $T_1$. | Metric: `Skipped=1`. Catalog unchanged. |
| `TestEngine_InvariantB_TimezoneEquivalence` | Invariant B | Seed with `2026-10-21T08:00:00Z`. Ingest update with `2026-10-21T15:00:00+07:00`. | Parsed as identical instant. Metric: `Skipped=1`. |
| `TestEngine_InvariantB_MicrosecondPrecision` | Invariant B | Seed with `...000001Z`. Ingest update with `...000002Z`. | Metric: `Updated=1`. Stored timestamp updated to microsecond. |
| `TestEngine_InvariantC_ActiveToWithdrawn` | Invariant C | Seed ACTIVE at $T_1$. Update to WITHDRAWN at $T_2 > T_1$. | Metric: `Withdrawn=1`. Status in DB = `WITHDRAWN`. |
| `TestEngine_InvariantC_ResurrectionBlocked` | Invariant C | Seed WITHDRAWN at $T_1$. Update to ACTIVE at $T_2 > T_1$. | Metric: `Skipped=1`. Status in DB remains `WITHDRAWN`. |
| `TestEngine_InvariantC_InitialWithdrawn` | Invariant C | Non-existent SKU arriving as `WITHDRAWN`. | Metric: `Added=1`. Status in DB = `WITHDRAWN`. |
| `TestEngine_IntraBatch_Duplicates` | Intra-Batch | Single batch contains 3 identical records for `SKU-A`. | Metric: `Added=1, Skipped=2`. DB has 1 product. |
| `TestEngine_IntraBatch_AscendingOrder` | Intra-Batch | Single batch with `[SKU-A, T1]`, `[SKU-A, T2]` ($T_1 < T_2$). | Metric: `Added=1, Updated=1`. DB product has $T_2$ and latest price. |
| `TestEngine_IntraBatch_DescendingOrder` | Intra-Batch | Single batch with `[SKU-A, T2]`, `[SKU-A, T1]` ($T_2 > T_1$). | Metric: `Added=1, Skipped=1`. DB product retains $T_2$ values. |
| `TestEngine_IntraBatch_TombstoneLock` | Intra-Batch | Single batch: `[ACTIVE, T1]`, `[WITHDRAWN, T2]`, `[ACTIVE, T3]`. | Metric: `Added=1, Withdrawn=1, Skipped=1`. DB status = `WITHDRAWN`. |
| `TestEngine_EmptyBatch` | Edge Case | Batch with `Records = []`. | Metric: `Added=0, Updated=0, Withdrawn=0, Skipped=0`. No DB changes. |
| `TestEngine_ContextCancellation` | Transaction | Context cancelled mid-batch processing. | Returns `context.Canceled`. 0 records committed to database. |

### 5.4 Concurrency & Race Test Specification (`pkg/sync/engine_race_test.go`)

The concurrency test suite must be designed specifically to run under `go test -race -count=5`:

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

// TestEngine_Race_ConcurrentReadsAndWrites verifies 0 data races between
// concurrent product readers and batch writers.
func TestEngine_Race_ConcurrentReadsAndWrites(t *testing.T) {
	store, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to init storage: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Pre-seed catalog with 50 products
	var initialRecords []domain.SupplierUpdate
	for i := 0; i < 50; i++ {
		initialRecords = append(initialRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-%04d", i),
			Name:      fmt.Sprintf("Product %d", i),
			Price:     float64(i) * 1.5,
			Status:    domain.StatusActive,
			Timestamp: time.Now().UTC(),
		})
	}
	_, err = engine.ExecuteBatch(ctx, domain.Batch{JobID: "seed", BatchIndex: 0, Records: initialRecords})
	if err != nil {
		t.Fatalf("failed to seed: %v", err)
	}

	var wg sync.WaitGroup
	numReaders := 20
	numWriters := 5

	// Spawn readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				sku := fmt.Sprintf("SKU-%04d", (readerID+j)%50)
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
			for b := 0; b < 5; b++ {
				var records []domain.SupplierUpdate
				for i := 0; i < 10; i++ {
					records = append(records, domain.SupplierUpdate{
						SKU:       fmt.Sprintf("SKU-%04d", (writerID*10+i)%50),
						Name:      fmt.Sprintf("Updated by W%d B%d", writerID, b),
						Price:     float64(writerID*10+i) + 10.0,
						Status:    domain.StatusActive,
						Timestamp: time.Now().UTC().Add(time.Duration(b) * time.Minute),
					})
				}
				_, _ = engine.ExecuteBatch(ctx, domain.Batch{
					JobID:      fmt.Sprintf("job-w%d", writerID),
					BatchIndex: b,
					Records:    records,
				})
			}
		}(w)
	}

	wg.Wait()
}

// TestEngine_Race_ConcurrentBatchExecution verifies that concurrent ExecuteBatch
// calls on identical or overlapping SKUs do not trigger panics, deadlocks, or races.
func TestEngine_Race_ConcurrentBatchExecution(t *testing.T) {
	store, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to init storage: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	var wg sync.WaitGroup
	numWorkers := 8

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			records := []domain.SupplierUpdate{
				{
					SKU:       "SHARED-SKU",
					Name:      fmt.Sprintf("Name from worker %d", workerID),
					Price:     10.0 + float64(workerID),
					Status:    domain.StatusActive,
					Timestamp: time.Now().UTC().Add(time.Duration(workerID) * time.Second),
				},
			}
			_, _ = engine.ExecuteBatch(ctx, domain.Batch{
				JobID:      fmt.Sprintf("race-job-%d", workerID),
				BatchIndex: 0,
				Records:    records,
			})
		}(i)
	}

	wg.Wait()

	// Verify catalog state is consistent
	prod, err := store.GetProduct(ctx, "SHARED-SKU")
	if err != nil {
		t.Fatalf("expected product to exist: %v", err)
	}
	if prod == nil || prod.SKU != "SHARED-SKU" {
		t.Fatalf("unexpected product state: %+v", prod)
	}
}
```

---

## 6. Caveats

1. **SQLite Concurrency & `modernc.org/sqlite`**:
   - In SQLite, WAL mode allows multiple concurrent readers, but only **one writer** may hold a write transaction simultaneously.
   - If multiple goroutines invoke `ExecuteBatch` without application-level synchronization, SQLite will wait up to `busy_timeout` (5000ms). If the lock is not released within that window, it returns `SQLITE_BUSY`.
   - **Recommendation**: In `pkg/sync/engine.go`, wrap `ExecuteBatch` transaction acquisition in an internal `sync.Mutex` (or single-writer channel) to ensure deterministic serialization and guarantee 0 `SQLITE_BUSY` errors during concurrent test runs.
2. **Timestamp Formatting & Microsecond Truncation**:
   - SQLite text storage formats timestamps as RFC3339 strings.
   - Sub-microsecond differences (nanoseconds) may be truncated depending on string formatting. The implementation must store timestamps in RFC3339Nano format (`2006-01-02T15:04:05.999999999Z07:00`) to preserve full precision.
3. **Local Host Disk Space Constraint**:
   - The developer machine currently has ~145–151 MiB of free disk space on `/System/Volumes/Data`.
   - Test suites must use SQLite in-memory databases (`:memory:`) or create small temporary databases in `t.TempDir()` that are cleaned up immediately via `t.Cleanup()`.
4. **Milestone Boundary (M1 vs M2)**:
   - Invariant D (Dead-Letter Isolation for negative prices and missing SKUs) and Invariant E (Crash Resumability & Checkpointing) are scheduled for **Milestone 2**.
   - In Milestone 1, `ChangeSummary` includes `DeadLetter: 0`. The unit tests for M1 must verify that Invariants A, B, and C function flawlessly on all valid product records.

---

## 7. Conclusion

Milestone 1's requirements and boundary behaviors have been completely mined and mapped:
1. **26 discrete features** discovered across Storage and Sync layers.
2. **31 rigorous edge case scenarios** formulated spanning boundary values, timestamp edge cases, and tombstone immutability.
3. **Comprehensive test suites** designed for `pkg/storage` (9 test cases) and `pkg/sync` (16 test cases + 2 concurrency race suites).
4. Concrete blueprints prepared for execution under `go test -race -count=5 ./...`, ensuring zero data races and meeting the $\ge 90\%$ test coverage requirement from day one.

---

## 8. Verification Method

Once Worker implements Milestone 1, the specifications detailed in this report can be verified independently using the following commands:

1. **Verify Statement & Branch Coverage ($\ge 90\%$)**:
   ```bash
   go test -v -coverprofile=coverage.out ./pkg/storage/... ./pkg/sync/...
   go tool cover -func=coverage.out
   ```
   *Expected*: Total coverage must meet or exceed 90.0%.

2. **Verify Multi-Iteration Race Detector (0 Data Races)**:
   ```bash
   go test -race -count=5 -v ./pkg/storage/... ./pkg/sync/...
   ```
   *Expected*: All tests pass cleanly over 5 consecutive iterations with 0 data race reports.

3. **Verify Invariant A (Batch Replay Idempotency)**:
   ```bash
   go test -v -run TestEngine_InvariantA ./pkg/sync/...
   ```
   *Expected*: `PASS`. Output confirms 2nd batch run yields `Added=0, Updated=0, Withdrawn=0, Skipped=N`.

4. **Verify Invariant B (Fast-Forward Ordering)**:
   ```bash
   go test -v -run TestEngine_InvariantB ./pkg/sync/...
   ```
   *Expected*: `PASS`. Older timestamps dropped, newer applied, identical deduplicated.

5. **Verify Invariant C (Tombstone Immutability)**:
   ```bash
   go test -v -run TestEngine_InvariantC ./pkg/sync/...
   ```
   *Expected*: `PASS`. Withdrawn products cannot be reactivated under any timestamp.
