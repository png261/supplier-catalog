# Milestone 2 Adversarial Challenge Report: Dead-Letter Queue Isolation (AC4)

**Explicit Verdict**: `CONFIRM_CORRECTNESS_FAILED`

---

## 1. Observation

### Observation 1.1: Domain Validation Contract for Invariant D
In `/Users/png/01. Project/coderpush/pkg/domain/models.go` (lines 9–19 and 53–68):
```go
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
```

### Observation 1.2: Specification Requirements in DISPATCH.md
In `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m2_2/DISPATCH.md` (lines 8–12):
```markdown
Adversarial Objectives:
1. Empirically verify AC4:
   - Stress test batches heavily polluted with corrupt records: negative prices, empty SKUs, whitespace-only SKUs, NaN prices, infinite prices, invalid status enums, zero timestamps, and extreme prices.
   - Verify all invalid records are quarantined into `dead_letter_issues` table with correct error reasons.
   - Verify valid records in the same batches commit successfully without interruption or failure.
```

### Observation 1.3: Implementation Omission in Sync Engine
In `/Users/png/01. Project/coderpush/pkg/sync/engine.go` (lines 558–587):
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

func getInvalidReason(r domain.SupplierUpdate) string {
	var reasons []string
	if strings.TrimSpace(r.SKU) == "" {
		reasons = append(reasons, "empty SKU")
	}
	if math.IsNaN(r.Price) {
		reasons = append(reasons, "price is NaN")
	} else if math.IsInf(r.Price, 0) {
		reasons = append(reasons, "price is infinite")
	} else if r.Price < 0 {
		reasons = append(reasons, fmt.Sprintf("negative price: %f", r.Price))
	}
	if !r.Status.IsValid() {
		reasons = append(reasons, fmt.Sprintf("invalid status: %s", r.Status))
	}
	return strings.Join(reasons, "; ")
}
```
`isInvalidRecord` does not call `r.Validate()` and omits checking `r.Timestamp.IsZero()`. `getInvalidReason` does not generate any reason for zero timestamps.

### Observation 1.4: Empirical Test Failure Output
Executing `go test -v -run TestAC4_Adversarial ./pkg/sync` produced:
```
=== RUN   TestAC4_Adversarial_ZeroTimestampQuarantine
    engine_ac4_stress_test.go:54: expected Added=1, got 2
    engine_ac4_stress_test.go:57: CRITICAL BUG: Zero timestamp record was NOT quarantined into DeadLetter! Summary: {Added:2 Updated:0 Withdrawn:0 Skipped:0 DeadLetter:0}
    engine_ac4_stress_test.go:63: CRITICAL BUG: SKU-ZERO-TIMESTAMP was committed to products table! Product: &{SKU:SKU-ZERO-TIMESTAMP Name:Item with Zero Timestamp Price:19.99 Status:ACTIVE SupplierTimestamp:0001-01-01 00:00:00 +0000 UTC UpdatedAt:2026-10-21 11:28:24.395141 +0000 UTC}
    engine_ac4_stress_test.go:72: expected 1 dead letter record, got 0
--- FAIL: TestAC4_Adversarial_ZeroTimestampQuarantine (0.00s)
=== RUN   TestAC4_Adversarial_CorruptRecordExhaustiveMatrix
...
    --- FAIL: TestAC4_Adversarial_CorruptRecordExhaustiveMatrix/zero_timestamp (0.00s)
=== RUN   TestAC4_Adversarial_ZeroTimestampExistingProduct
    engine_ac4_stress_test.go:389: CRITICAL BUG: Zero timestamp update on existing product was NOT sent to DeadLetter! Got summary: {Added:0 Updated:0 Withdrawn:0 Skipped:1 DeadLetter:0}
    engine_ac4_stress_test.go:392: CRITICAL BUG: Zero timestamp was erroneously treated as Skipped instead of DeadLetter! Got Skipped=1
--- FAIL: TestAC4_Adversarial_ZeroTimestampExistingProduct (0.00s)
FAIL
FAIL	coderpush/pkg/sync	0.350s
```

### Observation 1.5: Concurrency and Race Detection Results
Executing `go test -v -race -count=5 ./...` passed with 0 data races:
```
PASS
ok  	coderpush/pkg/sync	34.172s
```

---

## 2. Logic Chain

1. **Premise**: Invariant D and Acceptance Criterion 4 require that all corrupt and invalid records (including empty/whitespace SKUs, negative prices, NaN prices, infinite prices, invalid status strings, and zero timestamps) must be isolated into the `dead_letter_issues` table without interrupting valid records in the batch (Observation 1.1, Observation 1.2).
2. **Contract Analysis**: In `pkg/domain/models.go`, the canonical domain definition states `ErrZeroTimestamp = errors.New("timestamp must not be zero")`, and `SupplierUpdate.Validate()` enforces that `Timestamp.IsZero()` is an invalid record violating Invariant D (Observation 1.1).
3. **Flaw Localization**: In `pkg/sync/engine.go`, the batch processing pipeline evaluates invalidity through the private helper `isInvalidRecord(record)` instead of `record.Validate()`. The function `isInvalidRecord` checks SKU emptiness, negative price, NaN, Inf, and status validity, but omits checking `r.Timestamp.IsZero()` (Observation 1.3).
4. **Empirical Defect Manifestation**:
   - When a new product arrives with a zero timestamp (`time.Time{}`), `isInvalidRecord` returns `false`. The engine treats the corrupt record as valid and executes `UpsertProductTx`, inserting it directly into the `products` table with supplier timestamp `0001-01-01 00:00:00 +0000 UTC` and incrementing `Added`. Zero entries are written to `dead_letter_issues` (Observation 1.4).
   - When an update for an existing product arrives with a zero timestamp, `isInvalidRecord` returns `false`. The engine compares `recordTimestamp` against the existing `supplier_timestamp`. Because Year 0001 is earlier than any valid timestamp, Invariant B triggers and the corrupt record is marked as `Skipped` rather than `DeadLetter`. Zero entries are written to `dead_letter_issues` (Observation 1.4).
5. **Divergence From Requirements**: Both paths violate AC4, which mandates that corrupt records with zero timestamps must be quarantined to `dead_letter_issues` with appropriate reasons (Observation 1.2).

---

## 3. Caveats

- All other tested invalid variations (negative prices, empty SKUs, whitespace-only SKUs, NaN prices, +Inf/-Inf prices, invalid status enums) are correctly isolated to `dead_letter_issues` with proper failure reasons and non-halting batch completion.
- Multi-iteration race detection (`go test -v -race -count=5 ./...`) passed cleanly across all packages with zero data races.
- The failure is isolated strictly to the absence of `r.Timestamp.IsZero()` in `isInvalidRecord` and `getInvalidReason` in `pkg/sync/engine.go`.

---

## 4. Conclusion

**Verdict**: `CONFIRM_CORRECTNESS_FAILED`

The synchronization engine in `pkg/sync/engine.go` fails Acceptance Criterion 4 (Dead-Letter Queue Isolation) for zero-timestamp records. Corrupt records with zero timestamps bypass dead-letter quarantine, directly polluting the canonical product catalog or erroneously masquerading as stale updates.

### Required Remediation (to be applied by implementer):
In `pkg/sync/engine.go`:
1. In `isInvalidRecord(r domain.SupplierUpdate)`:
   Add:
   ```go
   if r.Timestamp.IsZero() {
       return true
   }
   ```
2. In `getInvalidReason(r domain.SupplierUpdate)`:
   Add:
   ```go
   if r.Timestamp.IsZero() {
       reasons = append(reasons, "timestamp must not be zero")
   }
   ```
   (Alternatively, refactor both helpers to delegate directly to `r.Validate()` and domain errors).

---

## 5. Verification Method

To independently reproduce the failure:
```bash
cd "/Users/png/01. Project/coderpush"
go test -v -run TestAC4_Adversarial ./pkg/sync
```

### Invalidation Conditions:
The verdict `CONFIRM_CORRECTNESS_FAILED` remains valid until:
1. `isInvalidRecord` in `pkg/sync/engine.go` flags zero-timestamp records as invalid.
2. `getInvalidReason` logs "timestamp must not be zero" in `dead_letter_issues`.
3. Running `go test -v -run TestAC4_Adversarial ./pkg/sync` produces 100% PASS with 0 failures.
