# Remediation Strategy Report: Zero-Timestamp Dead-Letter Validation (AC4 / Invariant D)

## Executive Summary
Challenger M2.2 reported `CONFIRM_CORRECTNESS_FAILED` against `pkg/sync/engine.go` due to omitted zero-timestamp checking in `isInvalidRecord` and `getInvalidReason`. This causes corrupt supplier records with uninitialized timestamps (`time.Time{}`) to bypass Invariant D dead-letter quarantine: inserting corrupt products into the database on new SKUs or erroneously masquerading as stale updates (`Skipped`) on existing SKUs. This report provides the root-cause evidence chain and exact remediation code blueprints.

---

## 1. Observation

### Observation 1.1: Domain Validation Contract for Invariant D
In `/Users/png/01. Project/coderpush/pkg/domain/models.go` (lines 15, 25, 53–68):
```go
15: 	ErrZeroTimestamp          = errors.New("timestamp must not be zero")
...
53: // Validate enforces Invariant D: verifies SKU non-empty, price non-negative, valid status, non-zero timestamp.
54: func (u *SupplierUpdate) Validate() error {
55: 	if strings.TrimSpace(u.SKU) == "" {
56: 		return ErrEmptySKU
57: 	}
58: 	if u.Price < 0 {
59: 		return ErrNegativePrice
60: 	}
61: 		if !u.Status.IsValid() {
62: 		return ErrInvalidStatus
63: 	}
64: 	if u.Timestamp.IsZero() {
65: 		return ErrZeroTimestamp
66: 	}
67: 	return nil
68: }
```

### Observation 1.2: Current Engine Validation Helpers
In `/Users/png/01. Project/coderpush/pkg/sync/engine.go` (lines 558–587):
```go
558: func isInvalidRecord(r domain.SupplierUpdate) bool {
559: 	if strings.TrimSpace(r.SKU) == "" {
560: 		return true
561: 	}
562: 	if math.IsNaN(r.Price) || math.IsInf(r.Price, 0) || r.Price < 0 {
563: 		return true
564: 	}
565: 	if !r.Status.IsValid() {
566: 		return true
567: 	}
568: 	return false
569: }
570: 
571: func getInvalidReason(r domain.SupplierUpdate) string {
572: 	var reasons []string
573: 	if strings.TrimSpace(r.SKU) == "" {
574: 		reasons = append(reasons, "empty SKU")
575: 	}
576: 	if math.IsNaN(r.Price) {
577: 		reasons = append(reasons, "price is NaN")
578: 	} else if math.IsInf(r.Price, 0) {
579: 		reasons = append(reasons, "price is infinite")
580: 	} else if r.Price < 0 {
581: 		reasons = append(reasons, fmt.Sprintf("negative price: %f", r.Price))
582: 	}
583: 	if !r.Status.IsValid() {
584: 		reasons = append(reasons, fmt.Sprintf("invalid status: %s", r.Status))
585: 	}
586: 	return strings.Join(reasons, "; ")
587: }
```
`isInvalidRecord` does not evaluate `r.Timestamp.IsZero()` nor does it call `r.Validate()`. `getInvalidReason` contains no case for `r.Timestamp.IsZero()`.

### Observation 1.3: Batch Pipeline Execution Flow in `executeRecordsTx`
In `/Users/png/01. Project/coderpush/pkg/sync/engine.go` (lines 143–222):
- Line 144: `recordTimestamp := record.Timestamp.UTC()`
- Line 147: `if isInvalidRecord(record)` evaluates whether the record should be quarantined to `dead_letter_issues`.
- Lines 193–208: If `existingProduct == nil`, a new product is inserted into the `products` table with `SupplierTimestamp: recordTimestamp` and `summary.Added++`.
- Lines 219–222: If `existingProduct != nil` and `recordTimestamp.Before(existingProduct.SupplierTimestamp)`, the engine treats the incoming update as outdated (Invariant B) and executes `summary.Skipped++` and `continue`.

### Observation 1.4: Direct Reproduction of Adversarial Test Failures
Executing `go test -v -run TestAC4_Adversarial ./pkg/sync` produced:
```
=== RUN   TestAC4_Adversarial_ZeroTimestampQuarantine
    engine_ac4_stress_test.go:54: expected Added=1, got 2
    engine_ac4_stress_test.go:57: CRITICAL BUG: Zero timestamp record was NOT quarantined into DeadLetter! Summary: {Added:2 Updated:0 Withdrawn:0 Skipped:0 DeadLetter:0}
    engine_ac4_stress_test.go:63: CRITICAL BUG: SKU-ZERO-TIMESTAMP was committed to products table! Product: &{SKU:SKU-ZERO-TIMESTAMP Name:Item with Zero Timestamp Price:19.99 Status:ACTIVE SupplierTimestamp:0001-01-01 00:00:00 +0000 UTC UpdatedAt:2026-10-21 11:48:10.096767 +0000 UTC}
    engine_ac4_stress_test.go:72: expected 1 dead letter record, got 0
--- FAIL: TestAC4_Adversarial_ZeroTimestampQuarantine (0.00s)
=== RUN   TestAC4_Adversarial_CorruptRecordExhaustiveMatrix
    --- FAIL: TestAC4_Adversarial_CorruptRecordExhaustiveMatrix/zero_timestamp (0.00s)
        engine_ac4_stress_test.go:223: zero timestamp: expected Added=1 (valid sibling), got 2
        engine_ac4_stress_test.go:226: zero timestamp: expected DeadLetter=1 for corrupt record, got 0 (Summary: {Added:2 Updated:0 Withdrawn:0 Skipped:0 DeadLetter:0})
        engine_ac4_stress_test.go:240: zero timestamp: corrupt product SKU-CORRUPT-ZERO-TIME was erroneously saved to products table!
        engine_ac4_stress_test.go:250: zero timestamp: expected exactly 1 dead letter record, got 0
=== RUN   TestAC4_Adversarial_ZeroTimestampExistingProduct
    engine_ac4_stress_test.go:389: CRITICAL BUG: Zero timestamp update on existing product was NOT sent to DeadLetter! Got summary: {Added:0 Updated:0 Withdrawn:0 Skipped:1 DeadLetter:0}
    engine_ac4_stress_test.go:392: CRITICAL BUG: Zero timestamp was erroneously treated as Skipped instead of DeadLetter! Got Skipped=1
--- FAIL: TestAC4_Adversarial_ZeroTimestampExistingProduct (0.00s)
FAIL
FAIL	coderpush/pkg/sync	0.145s
```

### Observation 1.5: Dead Letter Keyword Verification in Tests
In `/Users/png/01. Project/coderpush/pkg/sync/engine_ac4_stress_test.go` (lines 74–76, 185–190, 192–197):
- Line 74: `if !strings.Contains(strings.ToLower(dlRecords[0].Reason), "timestamp")`
- Line 189: `expectedReasonKeywords: []string{"timestamp"}`
- Line 196: `expectedReasonKeywords: []string{"empty SKU", "negative price", "invalid status"}`
In `/Users/png/01. Project/coderpush/pkg/sync/resumability_test.go` (line 532):
- `expectedSubstrings := []string{"negative price", "empty SKU", "price is NaN", "price is infinite", "invalid status"}`

---

## 2. Logic Chain

1. **Contract Definition**: According to Invariant D and Acceptance Criterion 4 (AC4) specified in `PROJECT.md` and `pkg/domain/models.go`, any record with a missing/empty SKU, negative price, invalid status, or zero timestamp (`time.Time{}`) is corrupt and must be quarantined into `dead_letter_issues` without halting valid records (Observation 1.1).
2. **Defect Mechanism**:
   - In `pkg/sync/engine.go:147`, the engine relies exclusively on `isInvalidRecord(record)` to route corrupt records into dead-letter storage before evaluating catalog mutations.
   - Because `isInvalidRecord` omits checking `r.Timestamp.IsZero()` and fails to call `r.Validate()` (Observation 1.2), a record with `Timestamp == time.Time{}` returns `false` (considered valid).
3. **Double Pipeline Failure Modes**:
   - *Failure Mode A (New Products)*: When a new SKU with a zero timestamp arrives, `isInvalidRecord` returns `false`. Execution proceeds to Step 2 (`existingProduct == nil`). The engine executes `UpsertProductTx` and commits the corrupt record with `0001-01-01 00:00:00` into SQLite `products`, incrementing `summary.Added`. `summary.DeadLetter` remains `0` (Observation 1.3, Observation 1.4).
   - *Failure Mode B (Existing Products)*: When an update for an existing SKU arrives with a zero timestamp, `isInvalidRecord` returns `false`. Execution proceeds to Step 3. Because Year 0001 is chronologically earlier than any valid local catalog timestamp, `recordTimestamp.Before(existingProduct.SupplierTimestamp)` evaluates to `true`. The engine erroneously increments `summary.Skipped++` under Invariant B instead of quarantining it under Invariant D (Observation 1.3, Observation 1.4).
4. **Diagnostic Accumulation Requirement**:
   - In `getInvalidReason(r)`, multiple invalid attributes can coexist on a single corrupt record (Observation 1.5). `r.Validate()` returns only the first error encountered (`error`), whereas `getInvalidReason` accumulates all invalidities (`reasons = append(reasons, ...)`) joined by `"; "`.
   - Therefore, `getInvalidReason` must explicitly append `"zero or uninitialized timestamp"` when `r.Timestamp.IsZero()`. This satisfies the required substring `"timestamp"` in AC4 stress tests and preserves multi-attribute failure diagnostics without breaking existing substring tests (Observation 1.5).

---

## 3. Caveats

- **Scope Boundary**: This investigation is strictly read-only. Source code in `pkg/sync/engine.go` has not been modified.
- **Floating-point edge cases**: `domain.SupplierUpdate.Validate()` checks `u.Price < 0`, but IEEE-754 `math.NaN` returns `false` for `< 0`, and `+Inf` is positive. Therefore, if `isInvalidRecord` delegates to `r.Validate()`, it must also retain `math.IsNaN(r.Price) || math.IsInf(r.Price, 0)` checks.

---

## 4. Conclusion & Recommended Remediation Blueprint

The fix is localized strictly to lines 558–587 of `pkg/sync/engine.go`.

### Recommended Blueprint

#### 1. Function `isInvalidRecord` in `pkg/sync/engine.go`
Align `isInvalidRecord` with `domain.SupplierUpdate.Validate()` while retaining float non-finite checks:
```go
func isInvalidRecord(r domain.SupplierUpdate) bool {
	if err := r.Validate(); err != nil {
		return true
	}
	if math.IsNaN(r.Price) || math.IsInf(r.Price, 0) {
		return true
	}
	return false
}
```
*(Alternative equivalent: add `if r.Timestamp.IsZero() { return true }` directly to the explicit field checks).*

#### 2. Function `getInvalidReason` in `pkg/sync/engine.go`
Add the zero-timestamp check to the accumulated reason list:
```go
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
	if r.Timestamp.IsZero() {
		reasons = append(reasons, "zero or uninitialized timestamp")
	}
	return strings.Join(reasons, "; ")
}
```

### Proposed Patch (`proposed_engine_validation.patch`)

```patch
--- a/pkg/sync/engine.go
+++ b/pkg/sync/engine.go
@@ -558,9 +558,9 @@ func priceEqual(a, b float64) bool {
 func isInvalidRecord(r domain.SupplierUpdate) bool {
-	if strings.TrimSpace(r.SKU) == "" {
-		return true
-	}
-	if math.IsNaN(r.Price) || math.IsInf(r.Price, 0) || r.Price < 0 {
+	if err := r.Validate(); err != nil {
 		return true
 	}
-	if !r.Status.IsValid() {
+	if math.IsNaN(r.Price) || math.IsInf(r.Price, 0) {
 		return true
 	}
 	return false
@@ -583,5 +583,8 @@ func getInvalidReason(r domain.SupplierUpdate) string {
 	if !r.Status.IsValid() {
 		reasons = append(reasons, fmt.Sprintf("invalid status: %s", r.Status))
 	}
+	if r.Timestamp.IsZero() {
+		reasons = append(reasons, "zero or uninitialized timestamp")
+	}
 	return strings.Join(reasons, "; ")
 }
```

---

## 5. Verification Method

To independently verify the defect and the proposed fix:

1. **Run AC4 Adversarial Suite**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go test -v -run TestAC4_Adversarial ./pkg/sync
   ```
   *Expected Current Output*: Fails on `TestAC4_Adversarial_ZeroTimestampQuarantine`, `TestAC4_Adversarial_CorruptRecordExhaustiveMatrix/zero_timestamp`, and `TestAC4_Adversarial_ZeroTimestampExistingProduct`.
   *Expected Post-Fix Output*: 100% PASS with 0 failures.

2. **Run Full Sync Test Suite and Race Detector**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go test -v -race -count=1 ./pkg/sync
   ```
   *Expected Post-Fix Output*: PASS for all unit, adversarial, stress, and race tests.

3. **Run Project-Wide Test Suite**:
   ```bash
   cd "/Users/png/01. Project/coderpush"
   go test ./...
   ```
   *Expected Post-Fix Output*: `ok coderpush/pkg/domain`, `ok coderpush/pkg/storage`, `ok coderpush/pkg/sync`.

### Invalidation Conditions:
The challenger verdict `CONFIRM_CORRECTNESS_FAILED` will be invalidated when:
1. `isInvalidRecord` flags zero-timestamp records as invalid.
2. `getInvalidReason` records `"zero or uninitialized timestamp"` in `dead_letter_issues`.
3. `go test -v -run TestAC4_Adversarial ./pkg/sync` executes with 0 failures.
