# Milestone 2 Remediation: Regression Analysis on Invariant D Zero-Timestamp Records

## 1. Observation

### Observation 1.1: Current Test Failure in `pkg/sync`
Running `go test ./...` in `/Users/png/01. Project/coderpush`:
```
ok  	coderpush/pkg/domain	0.367s
ok  	coderpush/pkg/storage	0.363s
--- FAIL: TestAC4_Adversarial_ZeroTimestampQuarantine (0.00s)
    engine_ac4_stress_test.go:54: expected Added=1, got 2
    engine_ac4_stress_test.go:57: CRITICAL BUG: Zero timestamp record was NOT quarantined into DeadLetter! Summary: {Added:2 Updated:0 Withdrawn:0 Skipped:0 DeadLetter:0}
    engine_ac4_stress_test.go:63: CRITICAL BUG: SKU-ZERO-TIMESTAMP was committed to products table! Product: &{SKU:SKU-ZERO-TIMESTAMP Name:Item with Zero Timestamp Price:19.99 Status:ACTIVE SupplierTimestamp:0001-01-01 00:00:00 +0000 UTC UpdatedAt:2026-10-21 11:47:41.462675 +0000 UTC}
    engine_ac4_stress_test.go:72: expected 1 dead letter record, got 0
--- FAIL: TestAC4_Adversarial_CorruptRecordExhaustiveMatrix (0.00s)
    --- FAIL: TestAC4_Adversarial_CorruptRecordExhaustiveMatrix/zero_timestamp (0.00s)
        engine_ac4_stress_test.go:223: zero timestamp: expected Added=1 (valid sibling), got 2
        engine_ac4_stress_test.go:226: zero timestamp: expected DeadLetter=1 for corrupt record, got 0 (Summary: {Added:2 Updated:0 Withdrawn:0 Skipped:0 DeadLetter:0})
        engine_ac4_stress_test.go:240: zero timestamp: corrupt product SKU-CORRUPT-ZERO-TIME was erroneously saved to products table!
        engine_ac4_stress_test.go:250: zero timestamp: expected exactly 1 dead letter record, got 0
--- FAIL: TestAC4_Adversarial_ZeroTimestampExistingProduct (0.00s)
    engine_ac4_stress_test.go:389: CRITICAL BUG: Zero timestamp update on existing product was NOT sent to DeadLetter! Got summary: {Added:0 Updated:0 Withdrawn:0 Skipped:1 DeadLetter:0}
    engine_ac4_stress_test.go:392: CRITICAL BUG: Zero timestamp was erroneously treated as Skipped instead of DeadLetter! Got Skipped=1
FAIL
FAIL	coderpush/pkg/sync	0.897s
FAIL
```
All failures are exclusively localized to zero-timestamp isolation checks introduced in `pkg/sync/engine_ac4_stress_test.go`.

### Observation 1.2: Root Cause in `pkg/sync/engine.go`
In `pkg/sync/engine.go` (lines 558–587):
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
`isInvalidRecord` does not check `r.Timestamp.IsZero()`. As a result, records with `time.Time{}` pass validation and are treated as valid records.

### Observation 1.3: AST Inventory of `SupplierUpdate` and `Product` in All Test Files
Using an AST scanner across all 9 test files in the project (`pkg/domain/models_test.go`, `pkg/storage/sqlite_test.go`, `pkg/sync/engine_test.go`, `pkg/sync/resumability_test.go`, `pkg/sync/engine_adversarial_test.go`, `pkg/sync/engine_challenger_test.go`, `pkg/sync/engine_race_test.go`, `pkg/sync/engine_resumability_challenger_test.go`, `pkg/sync/engine_ac4_stress_test.go`):
- `[MISSING TIMESTAMP]` in `domain.SupplierUpdate`: **0 occurrences**.
- `[PRODUCT MISSING SUPPLIER_TIMESTAMP]` in `domain.Product`: **0 occurrences**.
- `[PRODUCT ZERO SUPPLIER_TIMESTAMP]` in `domain.Product`: **0 occurrences**.
- `[ZERO TIMESTAMP LITERAL]` (`Timestamp: time.Time{}`): **5 occurrences total**:
  1. `/Users/png/01. Project/coderpush/pkg/domain/models_test.go:95` (domain unit test validating `ErrZeroTimestamp`)
  2. `/Users/png/01. Project/coderpush/pkg/sync/engine_ac4_stress_test.go:37` (`TestAC4_Adversarial_ZeroTimestampQuarantine`)
  3. `/Users/png/01. Project/coderpush/pkg/sync/engine_ac4_stress_test.go:186` (`TestAC4_Adversarial_CorruptRecordExhaustiveMatrix` - zero timestamp case)
  4. `/Users/png/01. Project/coderpush/pkg/sync/engine_ac4_stress_test.go:193` (`TestAC4_Adversarial_CorruptRecordExhaustiveMatrix` - multiple corrupt fields case)
  5. `/Users/png/01. Project/coderpush/pkg/sync/engine_ac4_stress_test.go:381` (`TestAC4_Adversarial_ZeroTimestampExistingProduct`)

### Observation 1.4: Existing Dead-Letter Unit Test Fixtures
1. In `pkg/sync/engine_test.go` (`TestEngine_DeadLetter_InvalidRecords`, lines 587–592):
   ```go
   		Records: []domain.SupplierUpdate{
   			{SKU: "SKU-VALID", Name: "Valid Item", Price: 10.0, Status: domain.StatusActive, Timestamp: now},
   			{SKU: "   ", Name: "Whitespace SKU", Price: 15.0, Status: domain.StatusActive, Timestamp: now},
   			{SKU: "SKU-NEG", Name: "Negative Price", Price: -5.0, Status: domain.StatusActive, Timestamp: now},
   			{SKU: "SKU-BAD-STATUS", Name: "Bad Status", Price: 20.0, Status: domain.ProductStatus("UNKNOWN"), Timestamp: now},
   		},
   ```
   All 4 records set `Timestamp: now`. It tests whitespace SKU, negative price, and bad status, but currently omits a zero-timestamp corrupt record.

2. In `pkg/sync/resumability_test.go` (`TestAC4_DeadLetter_IsolationAndNonHalting`, lines 453–474):
   ```go
   		Records: []domain.SupplierUpdate{
   			// 1. Valid Active item
   			{SKU: "SKU-VALID-1", Name: "Valid Product 1", Price: 19.99, Status: domain.StatusActive, Timestamp: now},
   			// 2. Corrupt: Negative price
   			{SKU: "SKU-INVALID-NEG", Name: "Negative Price Item", Price: -5.00, Status: domain.StatusActive, Timestamp: now},
   			// 3. Corrupt: Empty SKU
   			{SKU: "", Name: "Missing SKU Item", Price: 25.00, Status: domain.StatusActive, Timestamp: now},
   			// 4. Corrupt: Whitespace-only SKU
   			{SKU: "   \t\n", Name: "Whitespace SKU Item", Price: 30.00, Status: domain.StatusActive, Timestamp: now},
   			// 5. Valid Withdrawn item
   			{SKU: "SKU-VALID-2", Name: "Valid Withdrawn Product", Price: 49.99, Status: domain.StatusWithdrawn, Timestamp: now},
   			// 6. Corrupt: IEEE 754 NaN price
   			{SKU: "SKU-INVALID-NAN", Name: "NaN Price", Price: math.NaN(), Status: domain.StatusActive, Timestamp: now},
   			// 7. Corrupt: Positive Infinity price
   			{SKU: "SKU-INVALID-INF", Name: "Inf Price", Price: math.Inf(1), Status: domain.StatusActive, Timestamp: now},
   			// 8. Corrupt: Negative Infinity price
   			{SKU: "SKU-INVALID-NEGINF", Name: "-Inf Price", Price: math.Inf(-1), Status: domain.StatusActive, Timestamp: now},
   			// 9. Corrupt: Invalid Status string
   			{SKU: "SKU-INVALID-STATUS", Name: "Bad Status", Price: 10.00, Status: domain.ProductStatus("DISCONTINUED"), Timestamp: now},
   			// 10. Valid Active item
   			{SKU: "SKU-VALID-3", Name: "Valid Product 3", Price: 99.99, Status: domain.StatusActive, Timestamp: now},
   		},
   ```
   All 10 records explicitly set `Timestamp: now`. Zero timestamp is currently omitted from the fixture.

---

## 2. Logic Chain

1. **Safety from Inadvertent Reliance**:
   - The AST analysis proves that zero existing unit tests across `pkg/sync/`, `pkg/storage/`, and `pkg/domain/` rely on zero or default timestamps for valid records (Observation 1.3).
   - All tests initialize records with explicit non-zero times (`baseTime`, `now`, `t1`, etc.).
   - Therefore, enforcing `r.Timestamp.IsZero()` rejection will not break any existing passing tests.

2. **Impact on Engine Implementation**:
   - Adding `if r.Timestamp.IsZero() { return true }` to `isInvalidRecord` and `if r.Timestamp.IsZero() { reasons = append(reasons, "timestamp must not be zero") }` to `getInvalidReason` in `pkg/sync/engine.go` directly addresses the root cause (Observation 1.2).
   - This ensures records with `time.Time{}` are quarantined to `dead_letter_issues` before any insertion into `products` or before checking stale update rules.

3. **Impact on Challenger Tests**:
   - In `pkg/sync/engine_ac4_stress_test.go`:
     - `TestAC4_Adversarial_ZeroTimestampQuarantine` expects `dlRecords[0].Reason` to contain `"timestamp"`. The reason string `"timestamp must not be zero"` contains `"timestamp"`, satisfying this check.
     - `TestAC4_Adversarial_CorruptRecordExhaustiveMatrix` expects reason keyword `"timestamp"`. Satisfied.
     - `TestAC4_Adversarial_ZeroTimestampExistingProduct` checks that a zero-timestamp update on an existing product yields `DeadLetter=1` and `Skipped=0`. Because `isInvalidRecord` runs before existing product comparisons, the zero timestamp update will be dead-lettered immediately. Satisfied.
   - All 3 failing tests in Observation 1.1 will pass cleanly.

4. **Recommendation for Test Fixture Updates**:
   - While existing tests will pass without changes, `engine_test.go` and `resumability_test.go` should proactively include zero-timestamp records in their standard dead-letter test fixtures (`TestEngine_DeadLetter_InvalidRecords` and `TestAC4_DeadLetter_IsolationAndNonHalting`). This prevents regression in core unit test suites if adversarial test files are ever moved or separated.

---

## 3. Caveats

- **No Caveats on Backward Compatibility**: There are zero existing tests relying on uninitialized timestamps.
- **Reason String Formatting**: `engine_ac4_stress_test.go` performs case-insensitive substring search for `"timestamp"`. Using `"timestamp must not be zero"` satisfies both `strings.Contains(reason, "timestamp")` and aligns with `domain.ErrZeroTimestamp.Error()`.

---

## 4. Conclusion

1. **Regression Risk Assessment**: **ZERO REGRESSION RISK**. Enforcing non-zero timestamps in `isInvalidRecord` will not break any existing tests in `pkg/domain`, `pkg/storage`, or `pkg/sync`.
2. **Implementation Strategy for Worker Agent**:
   In `/Users/png/01. Project/coderpush/pkg/sync/engine.go`:
   - In `isInvalidRecord(r domain.SupplierUpdate) bool` (around line 567):
     ```go
     if r.Timestamp.IsZero() {
         return true
     }
     ```
   - In `getInvalidReason(r domain.SupplierUpdate) string` (around line 586):
     ```go
     if r.Timestamp.IsZero() {
         reasons = append(reasons, "timestamp must not be zero")
     }
     ```
3. **Recommended Test Fixture Updates**:
   - **`pkg/sync/engine_test.go` (`TestEngine_DeadLetter_InvalidRecords`)**:
     Add 5th record with zero timestamp:
     ```go
     {SKU: "SKU-ZERO-TIME", Name: "Zero Timestamp Item", Price: 15.0, Status: domain.StatusActive, Timestamp: time.Time{}},
     ```
     Update expectation from `res.DeadLetter != 3` to `res.DeadLetter != 4`.
   - **`pkg/sync/resumability_test.go` (`TestAC4_DeadLetter_IsolationAndNonHalting`)**:
     Add 11th record with zero timestamp:
     ```go
     // 11. Corrupt: Zero timestamp
     {SKU: "SKU-INVALID-ZEROTIME", Name: "Zero Time Item", Price: 15.00, Status: domain.StatusActive, Timestamp: time.Time{}},
     ```
     Update expectation from `DeadLetter: 7` to `DeadLetter: 8`, and add `"timestamp"` to `expectedSubstrings`.

---

## 5. Verification Method

### Step 1: Verify Zero Regressions on Existing Code
Run the full test suite before and after applying the fix:
```bash
cd "/Users/png/01. Project/coderpush"
go test -v ./...
```

### Step 2: Verify AC4 Adversarial Suite Passes
Run the specific adversarial AC4 test suite:
```bash
go test -v -run TestAC4_Adversarial ./pkg/sync
```
Expected output:
```
=== RUN   TestAC4_Adversarial_ZeroTimestampQuarantine
--- PASS: TestAC4_Adversarial_ZeroTimestampQuarantine (0.00s)
=== RUN   TestAC4_Adversarial_CorruptRecordExhaustiveMatrix
--- PASS: TestAC4_Adversarial_CorruptRecordExhaustiveMatrix (0.01s)
=== RUN   TestAC4_Adversarial_ZeroTimestampExistingProduct
--- PASS: TestAC4_Adversarial_ZeroTimestampExistingProduct (0.00s)
=== RUN   TestAC4_Adversarial_ExtremePrices
--- PASS: TestAC4_Adversarial_ExtremePrices (0.00s)
PASS
ok  	coderpush/pkg/sync	(cached)
```

### Step 3: Verify Multi-Iteration Race Detector
```bash
go test -v -race -count=5 ./...
```
Expected: 100% PASS with 0 data races.
