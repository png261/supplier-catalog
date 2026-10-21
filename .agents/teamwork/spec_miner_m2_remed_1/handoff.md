# Milestone 2 Remediation: Zero-Timestamp Test Specifications & Feature Mining

## Executive Summary
This report defines formal test specifications and mined behavioral requirements for zero-timestamp records within the catalog synchronization engine (`pkg/sync`), resolving the `CONFIRM_CORRECTNESS_FAILED` verdict reported in `challenger_m2_2/handoff.md`.

---

## Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Invariant D: Dead-Letter Isolation | Zero-Timestamp Quarantine (New SKU) | Incoming record with zero timestamp (`time.Time{}`) for a new SKU is quarantined to `dead_letter_issues` and never inserted into `products`. | `SupplierUpdate{SKU: "SKU-NEW", Timestamp: time.Time{}}` | `ChangeSummary{DeadLetter: 1, Added: 0}`, 1 record in `dead_letter_issues` | Must not halt batch transaction; returns nil error; product omitted from catalog | `pkg/domain/models.go:64`, `challenger_m2_2/handoff.md` |
| 2 | Invariant D: Dead-Letter Isolation | Zero-Timestamp Quarantine (Existing Product) | Incoming record with zero timestamp for an existing catalog product must be quarantined to `dead_letter_issues` and must NOT be marked as `Skipped`. | Existing product in catalog; `SupplierUpdate{SKU: "SKU-EXISTING", Timestamp: time.Time{}}` | `ChangeSummary{DeadLetter: 1, Skipped: 0}`, 1 record in `dead_letter_issues` | Must not update product row in SQLite; must not increment Skipped; returns nil error | `engine_ac4_stress_test.go:376-394`, `pkg/sync/engine.go:147` |
| 3 | Invariant D: Dead-Letter Isolation | Dead-Letter Reason Formatting | Quarantined zero-timestamp record logs `"zero or uninitialized timestamp"` in `dead_letter_issues.reason`. | `SupplierUpdate{Timestamp: time.Time{}}` | `DeadLetterRecord.Reason = "zero or uninitialized timestamp"` | Raw payload preserved as JSON string in `raw_payload` | `DISPATCH.md`, `pkg/domain/models.go:18`, `engine.go:571` |
| 4 | Invariant D: Dead-Letter Isolation | Multi-Defect Composite Reason Logging | Record with zero timestamp plus other corrupt fields (empty SKU, negative price, invalid status) logs all reasons separated by `"; "`. | `SupplierUpdate{SKU: "", Price: -10, Status: "BOGUS", Timestamp: time.Time{}}` | `DeadLetterRecord.Reason` containing `"empty SKU"`, `"negative price"`, `"invalid status"`, and `"zero or uninitialized timestamp"` | `DeadLetter: 1`; non-empty reasons joined cleanly; no panic | `engine.go:571-587`, `engine_ac4_stress_test.go:192-197` |
| 5 | Invariant D: Dead-Letter Isolation | Zero Time Representation Parity | Both uninitialized `time.Time{}` and explicit `time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)` trigger `IsZero() == true` quarantine. | `time.Time{}` vs `time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)` | Both result in `DeadLetter: 1` | Same quarantine behavior regardless of timezone wrapper | Go `time.Time.IsZero()` specification |
| 6 | Invariant E: Atomic Transactions | Mixed Batch Atomic Commit & Isolation | Batch with $M$ valid records and $N$ zero-timestamp records commits $M$ products and quarantines $N$ records in a single atomic transaction. | `Batch{Records: [M valid, N zero-ts]}` | `ChangeSummary{Added: M, DeadLetter: N}`, transaction committed | If batch fails due to external DB error, entire batch rolls back; otherwise valid records commit and invalid records are logged | `ORIGINAL_REQUEST.md:22`, `PROJECT.md:15-16` |
| 7 | Invariant E: Atomic Transactions | 100% Zero-Timestamp Batch Execution | Batch containing $N$ zero-timestamp records commits 0 products to the catalog and logs $N$ dead letters without failing the batch. | `Batch{Records: [N zero-ts]}` | `ChangeSummary{Added: 0, Updated: 0, Withdrawn: 0, Skipped: 0, DeadLetter: N}` | Batch transaction commits successfully (returns nil error); catalog has 0 drift | `DISPATCH.md:17`, `PROJECT.md:15` |
| 8 | Invariant A: Deterministic Idempotency | Zero-Timestamp Batch Replay Drift Immunity | Replaying a batch containing zero-timestamp records produces 0 catalog drift and does not insert duplicate products. | Ingesting same zero-timestamp batch twice | Catalog checksum remains invariant; 0 products inserted; 0 updates | No SQLite unique constraint violations on `products` table | `DISPATCH.md:18`, `PROJECT.md:43`, `ORIGINAL_REQUEST.md:14` |
| 9 | Invariant D & Ordering | Intra-Batch Sibling Independence | An invalid zero-timestamp record in a batch does not block prior or subsequent valid records in the same batch. | `[Valid-1, Zero-TS-1, Valid-2]` | `ChangeSummary{Added: 2, DeadLetter: 1}`; Valid-1 and Valid-2 exist in DB | Both valid records committed; zero-ts recorded in dead-letter | `pkg/sync/engine.go:138-175` |
| 10 | Invariant D & Lifecycle | Intra-Batch Lifecycle Collision Resolution | A valid record and a zero-timestamp record for the SAME SKU in the same batch: valid record modifies catalog; zero-timestamp record is dead-lettered. | `[SKU-A valid at T1, SKU-A zero-ts, SKU-A valid at T2]` | `ChangeSummary{Added: 1, Updated: 1, DeadLetter: 1}`; catalog has SKU-A at T2 | Dead letter logged for invalid update; valid lifecycle preserved | `pkg/sync/engine.go:136-246` |
| 11 | Invariant E & Checkpointing | Multi-Batch Job Progression with Dead Letters | When using `RunJob` or `ResumeJob`, batches containing zero-timestamp records advance `committed_batch_index` and accumulate in `SyncJob.Summary`. | Stream of batches with zero-timestamp items processed via `RunJob` | `SyncJob{Status: COMPLETED, CommittedBatchIndex: total-1, Summary: {...}}` | Checkpoints persist in `batch_checkpoints`; job completes normally | `pkg/sync/resumability_test.go:481-496` |
| 12 | Storage & Inspection API | Dead Letter Pagination & Inspection | Quarantined zero-timestamp records can be queried via `ListDeadLetterRecords(jobID, limit, offset)`. | `jobID`, `limit`, `offset` | `[]DeadLetterRecord`, totalCount, nil error | Returns full metadata: `IssueID`, `JobID`, `BatchIndex`, `SKU`, `RawPayload`, `Reason`, `CreatedAt` | `pkg/storage/sqlite.go:497-550` |
| 13 | Domain Contract | Domain Model Validation Consistency | `SupplierUpdate.Validate()` and `isInvalidRecord(r)` produce identical validation verdicts for zero timestamps. | Any `SupplierUpdate` struct | `Validate() == ErrZeroTimestamp` $\iff$ `isInvalidRecord() == true` | Uniform invariant validation across domain and sync engine layers | `pkg/domain/models.go:54-68`, `pkg/sync/engine.go:558-569` |

---

## Edge Cases

| # | Feature | Input | Observed Behavior |
|---|---------|-------|-------------------|
| 1 | Single Record Quarantine (New SKU) | `SupplierUpdate{SKU: "SKU-Z1", Timestamp: time.Time{}}` in batch index 0 | **Bug**: Inserted into `products` with timestamp `0001-01-01 00:00:00 UTC`, `Added: 1`, `DeadLetter: 0`.<br>**Specified**: Quarantined to `dead_letter_issues` with reason `"zero or uninitialized timestamp"`, `Added: 0`, `DeadLetter: 1`, omitted from `products`. |
| 2 | Existing Active Product Update | Product exists with $T_1 = \text{2026-10-21}$. Update arrives with `Timestamp: time.Time{}`. | **Bug**: Treated as stale update because Year 0001 $< T_1$; `Skipped: 1`, `DeadLetter: 0`.<br>**Specified**: Quarantined to `dead_letter_issues`, `Skipped: 0`, `DeadLetter: 1`. Existing product in `products` table unmodified. |
| 3 | Existing Withdrawn Product Update | Product exists with `Status: WITHDRAWN`. Update arrives with `Status: ACTIVE, Timestamp: time.Time{}`. | **Bug**: Evaluated by Invariant C before dead-letter or skipped due to timestamp $< T_{withdrawn}$; `Skipped: 1`.<br>**Specified**: Invariant D takes precedence at batch intake; quarantined to `dead_letter_issues`, `DeadLetter: 1`, `Skipped: 0`. |
| 4 | Batch of 100% Zero-Timestamp Records | Batch of 10 items, all with `Timestamp: time.Time{}` and unique SKUs. | **Bug**: All 10 items inserted into `products` table, `Added: 10`, `DeadLetter: 0`.<br>**Specified**: 0 items in `products` table; 10 rows in `dead_letter_issues`; `Summary{DeadLetter: 10, Added: 0, Updated: 0, Withdrawn: 0, Skipped: 0}`. Transaction commits successfully. |
| 5 | Replay of 100% Zero-Timestamp Batch | Batch of 10 zero-timestamp items executed, then replayed a second time on the same storage instance. | **Bug**: Run 1 inserts 10 items. Run 2 reports `Skipped: 10` due to identical supplier timestamps (0001-01-01). Catalog permanently polluted.<br>**Specified**: Run 1 results in 0 catalog items, 10 dead letters. Run 2 results in 0 catalog items (0 drift), 10 dead letters. Catalog remains pristine with 0 products. |
| 6 | Interleaved Mixed Batch | 5 valid new items alternating with 5 zero-timestamp items in single batch. | **Bug**: All 10 items added to `products` table (`Added: 10`).<br>**Specified**: 5 valid items committed to `products` table (`Added: 5`); 5 zero-timestamp items isolated to `dead_letter_issues` (`DeadLetter: 5`). |
| 7 | Multi-Defect Record (All Fields Invalid) | `SKU: "   "`, `Price: -99.99`, `Status: "UNKNOWN"`, `Timestamp: time.Time{}` | Quarantined to `dead_letter_issues`. Reason must be composite: `"empty SKU; negative price: -99.990000; invalid status: UNKNOWN; zero or uninitialized timestamp"`. |
| 8 | Multi-Defect Record (Price NaN + Zero-TS) | `SKU: "SKU-NAN"`, `Price: math.NaN()`, `Status: ACTIVE`, `Timestamp: time.Time{}` | Quarantined to `dead_letter_issues`. Reason: `"price is NaN; zero or uninitialized timestamp"`. |
| 9 | Intra-Batch Collision: Valid then Zero-TS | Index 0: `SKU-A` valid at $T_1$; Index 1: `SKU-A` corrupt with `time.Time{}` in same batch. | Index 0 adds `SKU-A` to batch state (`Added: 1`). Index 1 dead-letters (`DeadLetter: 1`). Final catalog retains `SKU-A` with $T_1$. |
| 10 | Intra-Batch Collision: Zero-TS then Valid | Index 0: `SKU-A` corrupt with `time.Time{}`; Index 1: `SKU-A` valid at $T_1$ in same batch. | Index 0 dead-letters (`DeadLetter: 1`). Index 1 adds `SKU-A` to catalog (`Added: 1`). Final catalog retains `SKU-A` with $T_1$. |
| 11 | Explicit Zero Date Construction | `Timestamp: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)` | `IsZero()` returns `true`. Must be quarantined to `dead_letter_issues` identically to `time.Time{}`. |
| 12 | JSON Unmarshaling Zero Timestamp | JSON payload with missing `"timestamp"` field or `"timestamp": "0001-01-01T00:00:00Z"` | Deserializes into `SupplierUpdate` with `Timestamp.IsZero() == true`. Must be quarantined to `dead_letter_issues`. |
| 13 | Multi-Batch Stream Checkpointing | 3 batches: Batch 0 (valid), Batch 1 (100% zero-ts), Batch 2 (valid) via `RunJob`. | `committed_batch_index` reaches 2. `SyncJob.Summary` aggregates valid changes + dead letters. Job status transitions to `COMPLETED`. |

---

## 5-Component Handoff Report

### 1. Observation
1. **Contract Inconsistency**:
   - In `/Users/png/01. Project/coderpush/pkg/domain/models.go` (line 15 and lines 64–66):
     ```go
     ErrZeroTimestamp = errors.New("timestamp must not be zero")
     // ...
     if u.Timestamp.IsZero() {
         return ErrZeroTimestamp
     }
     ```
   - In `/Users/png/01. Project/coderpush/pkg/sync/engine.go` (lines 558–587):
     ```go
     func isInvalidRecord(r domain.SupplierUpdate) bool {
         if strings.TrimSpace(r.SKU) == "" {
             return true
         }
         if math.IsNaN(r.Price) || math.IsInf(r.Price, 0) || r.Price < 0 {
             return true
         }
         if !r.Status.IsValid() {
             return true
         }
         return false
     }
     ```
     `isInvalidRecord` ignores `r.Timestamp.IsZero()`, bypassing Invariant D validation.
   - In `getInvalidReason(r domain.SupplierUpdate)` (lines 571–587):
     No branch exists for `r.Timestamp.IsZero()`, producing an empty or incomplete reason string when a record with a zero timestamp is evaluated.

2. **Empirical Test Failure**:
   Running `go test -v -run TestAC4_Adversarial ./pkg/sync` produced:
   ```
   === RUN   TestAC4_Adversarial_ZeroTimestampQuarantine
       engine_ac4_stress_test.go:54: expected Added=1, got 2
       engine_ac4_stress_test.go:57: CRITICAL BUG: Zero timestamp record was NOT quarantined into DeadLetter! Summary: {Added:2 Updated:0 Withdrawn:0 Skipped:0 DeadLetter:0}
       engine_ac4_stress_test.go:63: CRITICAL BUG: SKU-ZERO-TIMESTAMP was committed to products table! Product: &{SKU:SKU-ZERO-TIMESTAMP Name:Item with Zero Timestamp Price:19.99 Status:ACTIVE SupplierTimestamp:0001-01-01 00:00:00 +0000 UTC UpdatedAt:2026-10-21 11:48:10.167366 +0000 UTC}
   --- FAIL: TestAC4_Adversarial_ZeroTimestampQuarantine (0.00s)
   === RUN   TestAC4_Adversarial_ZeroTimestampExistingProduct
       engine_ac4_stress_test.go:389: CRITICAL BUG: Zero timestamp update on existing product was NOT sent to DeadLetter! Got summary: {Added:0 Updated:0 Withdrawn:0 Skipped:1 DeadLetter:0}
       engine_ac4_stress_test.go:392: CRITICAL BUG: Zero timestamp was erroneously treated as Skipped instead of DeadLetter! Got Skipped=1
   --- FAIL: TestAC4_Adversarial_ZeroTimestampExistingProduct (0.00s)
   ```

3. **Challenger Failure Context**:
   `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m2_2/handoff.md` issued `CONFIRM_CORRECTNESS_FAILED` due to unquarantined zero-timestamp records contaminating both new product insertions and existing product update streams.

---

### 2. Logic Chain
1. **Authoritative Specification**:
   - `ORIGINAL_REQUEST.md` (R2 & Acceptance Criteria): "Isolate corrupt or invalid product records into a dead-letter queue for staff attention without failing valid records in the batch."
   - `PROJECT.md` (Invariant D): "Negative prices, empty/whitespace SKUs, and malformed records quarantined to dead_letter_issues without failing valid records."
   - `pkg/domain/models.go`: Canonical definition enforces `Timestamp.IsZero()` as an invalid record violating Invariant D (`ErrZeroTimestamp`).
2. **Failure Analysis**:
   - In `pkg/sync/engine.go` (line 147), record validation delegates to `isInvalidRecord(record)`.
   - Because `isInvalidRecord` omitted `r.Timestamp.IsZero()`, zero-timestamp records were considered valid.
   - For a new SKU, the record proceeds to line 194, inserting a product with `SupplierTimestamp: 0001-01-01 00:00:00 +0000 UTC` and incrementing `Added`.
   - For an existing SKU, the record proceeds to line 219, where `recordTimestamp.Before(existingProduct.SupplierTimestamp)` evaluates to true (since year 0001 is older than any real timestamp), erroneously triggering Invariant B and incrementing `Skipped`.
3. **Remediation Specification**:
   - In `isInvalidRecord(r domain.SupplierUpdate)`: Must check `if r.Timestamp.IsZero() { return true }` (or `if r.Validate() != nil`).
   - In `getInvalidReason(r domain.SupplierUpdate)`: Must check `if r.Timestamp.IsZero() { reasons = append(reasons, "zero or uninitialized timestamp") }`.
   - Both new product ingestion and existing product updates must quarantine zero-timestamp records to `dead_letter_issues` with non-incremented `Added` and non-incremented `Skipped`.

---

### 3. Caveats
- **Dead-Letter Reason Wording**: DISPATCH.md explicitly mandates `"zero or uninitialized timestamp"`. The existing adversarial test `engine_ac4_stress_test.go` checks `strings.Contains(strings.ToLower(reason), "timestamp")`. Using `"zero or uninitialized timestamp"` satisfies both DISPATCH.md and all existing test assertions.
- **Timestamp Zero Value in Go**: In Go, `time.Time{}` and `time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)` both report `IsZero() == true`. Any timezone normalization (`.UTC()`) on `time.Time{}` maintains `IsZero() == true`.
- **Idempotency vs Audit Logging**: Replaying an invalid batch does not mutate or drift the catalog (`products` table), satisfying Invariant A. Dead-letter issues are audit records with unique `IssueID`s (`dl-...-[nano]`), ensuring audit traceability without affecting catalog state.

---

### 4. Conclusion
Four formal test specifications are required to guarantee complete remediation of the zero-timestamp defect:

#### Test Specification 1: Zero-Timestamp Record Quarantine & Reason
- **Objective**: Verify that a zero-timestamp record is quarantined to `dead_letter_issues` with reason `"zero or uninitialized timestamp"`, whether the SKU is new or already exists in the catalog.
- **Fixture**:
  - Subtest 1A (New SKU): Ingest single record with `Timestamp: time.Time{}`. Verify `Added: 0`, `DeadLetter: 1`, `Skipped: 0`. Verify product does not exist in `products`. Verify 1 entry in `dead_letter_issues` with `Reason: "zero or uninitialized timestamp"`.
  - Subtest 1B (Existing SKU): Seed product `SKU-EXISTING` with valid timestamp $T_1$. Ingest update with `SKU-EXISTING` and `Timestamp: time.Time{}`. Verify `DeadLetter: 1`, `Skipped: 0`, `Updated: 0`. Verify product in `products` remains with timestamp $T_1$ and original price/status.

#### Test Specification 2: Mixed Batch Processing (Valid + Zero-Timestamp)
- **Objective**: Verify that a batch containing a mixture of valid records and zero-timestamp records commits all valid records atomically while isolating all zero-timestamp records to dead letters.
- **Fixture**:
  - Batch of 10 items: 5 valid products ($P_1 \dots P_5$) and 5 zero-timestamp records ($Z_1 \dots Z_5$) interleaved.
  - Assertions: `ExecuteBatch` returns `nil` error. `ChangeSummary{Added: 5, DeadLetter: 5, Updated: 0, Withdrawn: 0, Skipped: 0}`.
  - Query DB: All 5 valid products exist in `products`. 0 of the 5 zero-timestamp products exist in `products`. Exactly 5 records exist in `dead_letter_issues`.

#### Test Specification 3: 100% Zero-Timestamp Batch
- **Objective**: Verify that a batch composed entirely of zero-timestamp records commits 0 products to the catalog and $N$ dead-letter records, with successful batch transaction completion.
- **Fixture**:
  - Batch of $N=20$ records, each with unique SKU and `Timestamp: time.Time{}`.
  - Assertions: `ExecuteBatch` returns `nil` error. `ChangeSummary{Added: 0, Updated: 0, Withdrawn: 0, Skipped: 0, DeadLetter: 20}`.
  - Query DB: `products` table has 0 rows (count = 0). `dead_letter_issues` table has 20 rows.
  - Multi-batch streaming: When run as part of a `RunJob` stream, the batch checkpoint records `summary.DeadLetter = 20` and job status reaches `COMPLETED`.

#### Test Specification 4: Replay Idempotency & Zero Catalog Drift
- **Objective**: Verify that replaying a batch containing zero-timestamp records is idempotent with 0 catalog drift.
- **Fixture**:
  - Seed catalog with 5 existing valid products. Compute catalog checksum (SHA-256 over all product fields).
  - Execute a batch containing 3 valid updates and 3 zero-timestamp records.
  - Replay the exact same batch.
  - Assertions: Catalog state after replay is identical. Catalog checksum before and after replay matches expected state. 0 additional products inserted. 0 unexpected mutations.

---

### 5. Verification Method

To verify the test specifications and implementation:

1. **Adversarial AC4 Test Execution**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go test -v -run TestAC4_Adversarial ./pkg/sync
   ```
   **Expected Outcome**: 100% PASS with 0 failures across `TestAC4_Adversarial_ZeroTimestampQuarantine`, `TestAC4_Adversarial_CorruptRecordExhaustiveMatrix`, and `TestAC4_Adversarial_ZeroTimestampExistingProduct`.

2. **Project-Wide Multi-Iteration Race Test**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go test -v -race -count=5 ./...
   ```
   **Expected Outcome**: 100% PASS with 0 data races.

3. **Invalidation Conditions**:
   The specifications are violated if:
   - Any zero-timestamp record results in `Added > 0` or `Skipped > 0` for that record.
   - Any zero-timestamp record creates a row in the `products` table.
   - `dead_letter_issues.reason` fails to contain `"zero or uninitialized timestamp"`.
   - Replaying a batch containing zero-timestamp records mutates existing product state.
