# Remediation Investigation Report: Dead-Letter Queue Reason Filter

**Target Deliverables**: `pkg/storage/sqlite.go`, `pkg/api/handlers.go`, `pkg/sync/engine.go`, `PROJECT.md`  
**Milestone**: M4.2 Remediation Track  
**Investigator**: `teamwork_preview_explorer` (Explorer M4 Remediation 2)  
**Parent Conversation ID**: `e6900bb2-6639-41ea-b0fb-48d815c2c1c8`  
**Status**: COMPLETE (Actionable Blueprint Ready)

---

## 1. Observation

### Observation 1.1: Empirical Test Failure in Adversarial DLQ Test
Running the adversarial test suite against the existing Milestone 4 codebase fails with an explicit specification violation:
```bash
go test -v -run=TestAdversarial_DeadLetterQueue_PaginationAndFiltering ./pkg/api/...
```
Verbatim execution output:
```
=== RUN   TestAdversarial_DeadLetterQueue_PaginationAndFiltering
=== RUN   TestAdversarial_DeadLetterQueue_PaginationAndFiltering/PaginationBoundaries
=== RUN   TestAdversarial_DeadLetterQueue_PaginationAndFiltering/InvariantD_PayloadAndReasonAccuracy
=== RUN   TestAdversarial_DeadLetterQueue_PaginationAndFiltering/FilterByReason
    adversarial_test.go:480: GET /api/dead-letter?reason=negative price returned Total=65, Count=20
    adversarial_test.go:484: SPECIFICATION VIOLATION / DEFECT: Expected 25 records filtered by reason 'negative price', but got Total=65. Reason query parameter is ignored by API handler.
--- FAIL: TestAdversarial_DeadLetterQueue_PaginationAndFiltering (0.01s)
    --- PASS: TestAdversarial_DeadLetterQueue_PaginationAndFiltering/PaginationBoundaries (0.00s)
    --- PASS: TestAdversarial_DeadLetterQueue_PaginationAndFiltering/InvariantD_PayloadAndReasonAccuracy (0.00s)
    --- FAIL: TestAdversarial_DeadLetterQueue_PaginationAndFiltering/FilterByReason (0.00s)
FAIL
FAIL	coderpush/pkg/api	0.518s
FAIL
```

### Observation 1.2: Code Defect in `pkg/api/handlers.go` (`handleListDeadLetter`)
In `pkg/api/handlers.go:586-627`:
```go
586: func (h *Handler) handleListDeadLetter(w http.ResponseWriter, r *http.Request) {
587: 	ctx := r.Context()
588: 	q := r.URL.Query()
...
610: 	jobID := q.Get("job_id")
611: 
612: 	issues, total, err := h.storage.ListDeadLetterRecords(ctx, jobID, limit, offset)
613: 	if err != nil {
614: 		h.writeError(w, http.StatusInternalServerError, "failed to list dead-letter records: "+err.Error(), ErrCodeInternalError)
615: 		return
616: 	}
```
**Finding**: `r.URL.Query().Get("reason")` is never read or extracted. Any incoming `?reason=...` query parameter is completely ignored by the HTTP handler layer.

### Observation 1.3: Interface and Storage Defect in `pkg/storage/sqlite.go`
In `pkg/storage/sqlite.go:36-39`:
```go
36: 	// Dead Letter
37: 	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
38: 	ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
```
And in `pkg/storage/sqlite.go:501-526`:
```go
501: // ListDeadLetterRecords queries paginated dead-letter records for a specific job (or all jobs if jobID is empty).
502: func (s *SQLiteStorage) ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error) {
503: 	var countQuery string
504: 	var selectQuery string
505: 	var countArgs []any
506: 	var selectArgs []any
507: 
508: 	if jobID != "" {
509: 		countQuery = `SELECT count(*) FROM dead_letter_issues WHERE job_id = ?;`
510: 		countArgs = []any{jobID}
511: 
512: 		selectQuery = `
513: 		SELECT issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
514: 		FROM dead_letter_issues WHERE job_id = ?
515: 		ORDER BY created_at DESC LIMIT ? OFFSET ?;
516: 		`
517: 		selectArgs = []any{jobID, limit, offset}
518: 	} else {
519: 		countQuery = `SELECT count(*) FROM dead_letter_issues;`
520: 		selectQuery = `
521: 		SELECT issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
522: 		FROM dead_letter_issues
523: 		ORDER BY created_at DESC LIMIT ? OFFSET ?;
524: 		`
525: 		selectArgs = []any{limit, offset}
526: 	}
```
**Finding**:
1. `Storage.ListDeadLetterRecords` does not accept any reason argument.
2. The SQLite queries only branch on `jobID != ""`. They cannot filter by reason substring or keyword.

### Observation 1.4: Mirror Interface in `pkg/sync/engine.go`
In `pkg/sync/engine.go:45-48`:
```go
45: 	// Dead Letter
46: 	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
47: 	ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
```
**Finding**: `pkg/sync/engine.go` declares its own internal `Storage` interface that mirrors `storage.Storage`. Both must remain synchronized to avoid compilation and mocking issues.

### Observation 1.5: Complete Call Site Matrix Across Repository
A comprehensive scan identified all references to `ListDeadLetterRecords`:

| # | File Path | Line(s) | Context |
|---|-----------|---------|---------|
| 1 | `pkg/storage/sqlite.go` | 38 | `Storage` interface definition |
| 2 | `pkg/storage/sqlite.go` | 502 | `*SQLiteStorage` method implementation |
| 3 | `pkg/sync/engine.go` | 47 | `sync.Storage` interface definition |
| 4 | `PROJECT.md` | 160 | Architecture specification contract |
| 5 | `pkg/api/handlers.go` | 612 | Handler caller in `handleListDeadLetter` |
| 6 | `pkg/storage/sqlite_test.go` | 361, 370, 376 | Storage unit tests |
| 7 | `pkg/sync/engine_test.go` | 618 | Engine unit test |
| 8 | `pkg/sync/resumability_test.go` | 175, 520 | Resumability & rollback verification |
| 9 | `pkg/sync/engine_ac4_stress_test.go` | 67, 245, 344 | Corrupt record stress tests |
| 10 | `pkg/sync/engine_deep_adversarial_test.go` | 490, 537, 700, 732 | Fault injection & retry tests |
| 11 | `pkg/sync/engine_ac4_remed_challenger_test.go` | 98, 234, 301, 564 | Challenger edge-case suite |
| 12 | `pkg/sync/engine_resumability_challenger_test.go` | 559, 906 | Resumption challenger suite |
| 13 | `pkg/generator/generator_test.go` | 602 | Generator conservation test |
| 14 | `pkg/generator/generator_challenger_test.go` | 388 | Generator scale invariant test |

---

## 2. Logic Chain

1. **Failure Etiology**:
   - In `TestAdversarial_DeadLetterQueue_PaginationAndFiltering/FilterByReason` (`pkg/api/adversarial_test.go:470-486`), the test generates a seeded batch with 65 corrupt items: 25 negative price, 20 empty/whitespace SKU, 10 zero timestamp, and 10 invalid status.
   - When calling `GET /api/dead-letter?reason=negative%20price`, the expected response is `Total: 25` items matching the negative price reason.
   - Because `handleListDeadLetter` does not parse `q.Get("reason")`, and `storage.Storage.ListDeadLetterRecords` does not accept or filter by reason, the query returns all 65 rows from `dead_letter_issues`.
   - The test asserts `resp.Total == 25`, triggering a fatal assertion failure.

2. **Interface Design Decision (Positional vs Options Struct)**:
   - In `pkg/storage/sqlite.go:42`, `ListProducts` is defined as:
     `ListProducts(ctx context.Context, status string, search string, limit, offset int) ([]domain.Product, int, error)`
   - Matching this established repository pattern, extending `ListDeadLetterRecords` to:
     `ListDeadLetterRecords(ctx context.Context, jobID string, reason string, limit, offset int) ([]domain.DeadLetterRecord, int, error)`
     provides maximal consistency, zero additional struct overhead, and intuitive parameter order (`ctx`, filters: `jobID, reason`, pagination: `limit, offset`).

3. **SQL Parameterization & Injection Safety**:
   - Using `AND reason LIKE '%' || ? || '%'` directly utilizes SQLite's string concatenation operator `||` with positional parameter binding `?`.
   - This guarantees complete SQL injection safety regardless of input characters, avoids manual string escapes, and adheres strictly to the dispatch instruction: `"with parameterization AND reason LIKE '%' || ? || %'"`.

4. **Dynamic Filter Composition Pattern**:
   - Adopting the clean slice-based clause accumulator used in `ListProducts`:
     ```go
     var conditions []string
     var args []any
     if jobID != "" {
         conditions = append(conditions, "job_id = ?")
         args = append(args, jobID)
     }
     if reason != "" {
         conditions = append(conditions, "reason LIKE '%' || ? || '%'")
         args = append(args, reason)
     }
     whereClause := ""
     if len(conditions) > 0 {
         whereClause = "WHERE " + strings.Join(conditions, " AND ")
     }
     ```
   - This dynamically handles all 4 query variations:
     1. `jobID` only: `WHERE job_id = ?`
     2. `reason` only: `WHERE reason LIKE '%' || ? || '%'`
     3. Both: `WHERE job_id = ? AND reason LIKE '%' || ? || '%'`
     4. Neither: (no WHERE clause, returning all records paginated)

5. **Nil Slice Safety in API Responses**:
   - Initializing `records := make([]domain.DeadLetterRecord, 0)` ensures that if 0 records match the query, JSON marshaling produces `{"issues": []}` rather than `{"issues": null}`, preventing client-side `.length` and `.map` runtime errors.

---

## 3. Caveats

1. **Update Scope Across Tests**:
   - Because Go does not support function overloading or default arguments, changing `ListDeadLetterRecords` requires passing `""` for the `reason` argument across 16 test call sites in `pkg/sync/`, `pkg/generator/`, and `pkg/storage/`.
   - All 16 call sites are cataloged in Section 4 below with exact line numbers.
2. **Case-Insensitivity Behavior in SQLite**:
   - By default, SQLite's `LIKE` operator is case-insensitive for ASCII characters. Substrings like `"negative price"`, `"Negative Price"`, and `"NEGATIVE PRICE"` will all match `"negative price: -1.000000"`. This matches the requirements of diagnostic reason filtering.
3. **Limit & Offset Preservation**:
   - Unlike `ListProducts` which caps `limit` to 100, `ListDeadLetterRecords` must preserve large limit values (such as `limit = 10000` in `pkg/generator/generator_test.go:602`) without arbitrary truncation. The API handler in `pkg/api/handlers.go` enforces HTTP-level bounds (`limit >= 1`, default 20; `offset >= 0`, default 0).
4. **Decoupling from Concurrent Race Remediations**:
   - This remediation specifically targets the DLQ reason filter defect. Separate concurrent race defects (Worker/Challenger observations 1.1, 1.2, 1.4 regarding `POST /api/jobs` and SSE broker client ID generation) are independent and orthogonal to this storage/API filter fix.

---

## 4. Conclusion & Actionable Implementation Blueprint

The Dead-Letter Queue reason filter defect is completely remediable with localized edits across `pkg/storage/sqlite.go`, `pkg/api/handlers.go`, `pkg/sync/engine.go`, and test calls.

### 4.1 Target File: `pkg/storage/sqlite.go`

#### Change A: Storage Interface (`pkg/storage/sqlite.go:36-39`)
```go
<<<< CURRENT
	// Dead Letter
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
	ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
====
	// Dead Letter
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
	ListDeadLetterRecords(ctx context.Context, jobID string, reason string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
>>>>
```

#### Change B: SQLiteStorage Implementation (`pkg/storage/sqlite.go:501-559`)
```go
<<<< CURRENT
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
		r.CreatedAt, _ = ParseTime(timeStr)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating dead letter rows: %w", err)
	}

	return records, totalCount, nil
}
====
// ListDeadLetterRecords queries paginated dead-letter records with optional job_id and reason filters.
func (s *SQLiteStorage) ListDeadLetterRecords(ctx context.Context, jobID string, reason string, limit, offset int) ([]domain.DeadLetterRecord, int, error) {
	var conditions []string
	var args []any

	if jobID != "" {
		conditions = append(conditions, "job_id = ?")
		args = append(args, jobID)
	}
	if reason != "" {
		conditions = append(conditions, "reason LIKE '%' || ? || '%'")
		args = append(args, reason)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT count(*) FROM dead_letter_issues %s;", whereClause)
	var totalCount int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("failed to count dead letter issues: %w", err)
	}

	selectQuery := fmt.Sprintf(`
		SELECT issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
		FROM dead_letter_issues %s
		ORDER BY created_at DESC LIMIT ? OFFSET ?;`, whereClause)

	selectArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.QueryContext(ctx, selectQuery, selectArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query dead letter issues: %w", err)
	}
	defer rows.Close()

	records := make([]domain.DeadLetterRecord, 0)
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
		r.CreatedAt, _ = ParseTime(timeStr)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating dead letter rows: %w", err)
	}

	return records, totalCount, nil
}
>>>>
```

---

### 4.2 Target File: `pkg/api/handlers.go`

#### Change in `handleListDeadLetter` (`pkg/api/handlers.go:610-613`)
```go
<<<< CURRENT
	jobID := q.Get("job_id")

	issues, total, err := h.storage.ListDeadLetterRecords(ctx, jobID, limit, offset)
====
	jobID := strings.TrimSpace(q.Get("job_id"))
	reason := strings.TrimSpace(q.Get("reason"))

	issues, total, err := h.storage.ListDeadLetterRecords(ctx, jobID, reason, limit, offset)
>>>>
```

---

### 4.3 Target File: `pkg/sync/engine.go`

#### Change in `sync.Storage` Interface (`pkg/sync/engine.go:45-48`)
```go
<<<< CURRENT
	// Dead Letter
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
	ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
====
	// Dead Letter
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
	ListDeadLetterRecords(ctx context.Context, jobID string, reason string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
>>>>
```

---

### 4.4 Target File: `PROJECT.md`

#### Change in Interface Contract (`PROJECT.md:159-161`)
```markdown
<<<< CURRENT
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
	ListDeadLetterRecords(ctx context.Context, jobID string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
====
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
	ListDeadLetterRecords(ctx context.Context, jobID string, reason string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
>>>>
```

---

### 4.5 Target File: `pkg/storage/sqlite_test.go`

#### Update existing calls and add comprehensive reason filtering assertions (`pkg/storage/sqlite_test.go:361-380`)
```go
<<<< CURRENT
	// Query for job
	records, count, err := s.ListDeadLetterRecords(ctx, "job-dl-1", 10, 0)
	if err != nil {
		t.Fatalf("failed listing dead letter records: %v", err)
	}
	if count != 2 || len(records) != 2 {
		t.Errorf("expected 2 records, got count %d, len %d", count, len(records))
	}

	// Pagination: limit 1, offset 0
	p1, countP, err := s.ListDeadLetterRecords(ctx, "job-dl-1", 1, 0)
	if err != nil || len(p1) != 1 || countP != 2 {
		t.Errorf("expected 1 record from page 1, got %d, total %d", len(p1), countP)
	}

	// Query across all jobs (empty jobID)
	allRecords, allCount, err := s.ListDeadLetterRecords(ctx, "", 10, 0)
	if err != nil || allCount != 2 || len(allRecords) != 2 {
		t.Errorf("expected 2 records for all jobs, got %d, total %d", len(allRecords), allCount)
	}
====
	// Query for job
	records, count, err := s.ListDeadLetterRecords(ctx, "job-dl-1", "", 10, 0)
	if err != nil {
		t.Fatalf("failed listing dead letter records: %v", err)
	}
	if count != 2 || len(records) != 2 {
		t.Errorf("expected 2 records, got count %d, len %d", count, len(records))
	}

	// Pagination: limit 1, offset 0
	p1, countP, err := s.ListDeadLetterRecords(ctx, "job-dl-1", "", 1, 0)
	if err != nil || len(p1) != 1 || countP != 2 {
		t.Errorf("expected 1 record from page 1, got %d, total %d", len(p1), countP)
	}

	// Query across all jobs (empty jobID)
	allRecords, allCount, err := s.ListDeadLetterRecords(ctx, "", "", 10, 0)
	if err != nil || allCount != 2 || len(allRecords) != 2 {
		t.Errorf("expected 2 records for all jobs, got %d, total %d", len(allRecords), allCount)
	}

	// Filter by reason: exact match
	negRecords, negCount, err := s.ListDeadLetterRecords(ctx, "job-dl-1", "negative price", 10, 0)
	if err != nil || negCount != 1 || len(negRecords) != 1 {
		t.Errorf("expected 1 negative price record, got %d, total %d", len(negRecords), negCount)
	}
	if len(negRecords) > 0 && negRecords[0].Reason != "negative price" {
		t.Errorf("unexpected reason: %s", negRecords[0].Reason)
	}

	// Filter by reason: substring match
	priceRecords, priceCount, err := s.ListDeadLetterRecords(ctx, "", "price", 10, 0)
	if err != nil || priceCount != 1 || len(priceRecords) != 1 {
		t.Errorf("expected 1 price record by substring, got %d, total %d", len(priceRecords), priceCount)
	}

	// Filter by non-existent reason
	noRecords, noCount, err := s.ListDeadLetterRecords(ctx, "", "nonexistent", 10, 0)
	if err != nil || noCount != 0 || len(noRecords) != 0 {
		t.Errorf("expected 0 records for nonexistent reason, got %d, total %d", len(noRecords), noCount)
	}
>>>>
```

---

### 4.6 Target Test Files Across Packages

Update the following call sites to pass `""` for the `reason` argument:

1. **`pkg/sync/engine_test.go:618`**:
   `dlRecords, count, err := store.ListDeadLetterRecords(ctx, "job-dl-test", "", 10, 0)`
2. **`pkg/sync/resumability_test.go:175`**:
   `dlRecords, count, err := store.ListDeadLetterRecords(context.Background(), jobID, "", 10, 0)`
3. **`pkg/sync/resumability_test.go:520`**:
   `dlRecords, totalCount, err := store.ListDeadLetterRecords(ctx, jobID, "", 20, 0)`
4. **`pkg/sync/engine_ac4_stress_test.go:67`**:
   `dlRecords, totalCount, err := store.ListDeadLetterRecords(ctx, jobID, "", 10, 0)`
5. **`pkg/sync/engine_ac4_stress_test.go:245`**:
   `dlRecords, count, err := store.ListDeadLetterRecords(ctx, subJobID, "", 10, 0)`
6. **`pkg/sync/engine_ac4_stress_test.go:344`**:
   `_, dlTotal, err := store.ListDeadLetterRecords(ctx, jobID, "", 1, 0)`
7. **`pkg/sync/engine_deep_adversarial_test.go:490`**:
   `dlRecords, dlCount, err := fStore.ListDeadLetterRecords(ctx, jobID, "", 10, 0)`
8. **`pkg/sync/engine_deep_adversarial_test.go:537`**:
   `_, retryDlCount, _ := fStore.ListDeadLetterRecords(ctx, jobID, "", 10, 0)`
9. **`pkg/sync/engine_deep_adversarial_test.go:700`**:
   `dlRecords, dlCount, _ := store.ListDeadLetterRecords(context.Background(), jobID, "", 10, 0)`
10. **`pkg/sync/engine_deep_adversarial_test.go:732`**:
    `_, dlCount, _ := store.ListDeadLetterRecords(context.Background(), jobID, "", 10, 0)`
11. **`pkg/sync/engine_ac4_remed_challenger_test.go:98`**:
    `dlRecords, total, err := store.ListDeadLetterRecords(ctx, jobID, "", 10, 0)`
12. **`pkg/sync/engine_ac4_remed_challenger_test.go:234`**:
    `dlList, totalDL, err := store.ListDeadLetterRecords(ctx, attackJobID, "", 10, 0)`
13. **`pkg/sync/engine_ac4_remed_challenger_test.go:301`**:
    `_, totalDL, err := store.ListDeadLetterRecords(ctx, jobID, "", 1, 0)`
14. **`pkg/sync/engine_ac4_remed_challenger_test.go:564`**:
    `_, dlTotal, err := store.ListDeadLetterRecords(ctx, jobID, "", 1, 0)`
15. **`pkg/sync/engine_resumability_challenger_test.go:559`**:
    `dlRecords, count, err := store.ListDeadLetterRecords(context.Background(), jobID, "", 50, 0)`
16. **`pkg/sync/engine_resumability_challenger_test.go:906`**:
    `dlRecords, count, err := store2.ListDeadLetterRecords(ctx, jobID, "", 10, 0)`
17. **`pkg/generator/generator_test.go:602`**:
    `dlRecords, totalDL, err := store.ListDeadLetterRecords(ctx, jobID, "", 10000, 0)`
18. **`pkg/generator/generator_challenger_test.go:388`**:
    `dlRecords, totalDL, err := store.ListDeadLetterRecords(ctx, jobID, "", totalExpectedRecords+100, 0)`

---

### 4.7 Optional Frontend Enhancement (Staff Dashboard DLQ Filter)

To expose reason filtering in the embedded staff UI:
- **`web/static/index.html:251-255`**:
  ```html
  <div class="filter-group">
    <label for="filter-dlq-job">Filter Job:</label>
    <input type="text" id="filter-dlq-job" class="form-input form-sm" placeholder="Enter Job ID or leave empty for all">
    <label for="filter-dlq-reason" style="margin-left: 0.5rem;">Reason:</label>
    <input type="text" id="filter-dlq-reason" class="form-input form-sm" placeholder="Filter by reason keyword...">
  </div>
  ```
- **`web/static/app.js:746-748`**:
  ```javascript
  const jobID = el.filterDlqJob ? el.filterDlqJob.value.trim() : '';
  const reason = el.filterDlqReason ? el.filterDlqReason.value.trim() : '';
  let url = `/api/dead-letter?limit=${state.dlq.limit}&offset=${state.dlq.offset}`;
  if (jobID) url += `&job_id=${encodeURIComponent(jobID)}`;
  if (reason) url += `&reason=${encodeURIComponent(reason)}`;
  ```

---

## 5. Verification Method

Once the changes are implemented by the remediation worker:

1. **Verify Adversarial DLQ Test Passes**:
   ```bash
   go test -v -race -run=TestAdversarial_DeadLetterQueue_PaginationAndFiltering ./pkg/api/...
   ```
   *Expected Result*: All 3 subtests (`PaginationBoundaries`, `InvariantD_PayloadAndReasonAccuracy`, `FilterByReason`) PASS. Specifically:
   `FilterByReason` returns `Total=25` for `GET /api/dead-letter?reason=negative price`.

2. **Verify Storage Unit Tests Pass**:
   ```bash
   go test -v -race -run=TestStorage_DeadLetterIssues ./pkg/storage/...
   ```
   *Expected Result*: All pagination, exact reason match, substring match, and empty reason queries PASS.

3. **Verify Full Repository Builds and Tests Pass**:
   ```bash
   go test -race -count=1 ./pkg/storage/... ./pkg/sync/... ./pkg/generator/...
   ```
   *Expected Result*: 100% buildability across all packages with zero signature mismatches.

4. **Invalidation Conditions**:
   - Any query to `GET /api/dead-letter?reason=...` returning records that do not contain the substring in their `reason` column.
   - Any compilation failure across `pkg/sync` or `pkg/generator` due to outdated positional parameter counts.
   - Any SQL syntax error or failure under modernc SQLite WAL mode.
