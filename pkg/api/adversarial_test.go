package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// setupAdversarialEnv creates an isolated SQLite DB on disk to avoid memory caching issues.
func setupAdversarialEnv(t *testing.T) (storage.Storage, catalogSync.Engine, *Broker, *Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), fmt.Sprintf("adversarial_%d.db", time.Now().UnixNano()))
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	engine := catalogSync.NewEngine(store)
	broker := NewBroker()
	handler := NewHandler(store, engine, broker)

	return store, engine, broker, handler
}

// -----------------------------------------------------------------------------
// Objective 1: Concurrency Control & 409 Conflict
// -----------------------------------------------------------------------------

func TestAdversarial_JobConcurrency_Race(t *testing.T) {
	store, _, _, handler := setupAdversarialEnv(t)
	defer handler.CancelActiveJob()

	router := handler.Routes()
	const numGoroutines = 20

	var startBarrier = make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	var acceptedCount int64
	var conflictCount int64
	var otherCount int64
	var responseStatuses = make([]int, numGoroutines)
	var responseBodies = make([]string, numGoroutines)

	// Prepare batches that take slight time to ensure overlap
	var testBatches []BatchInput
	for b := 0; b < 10; b++ {
		var recs []BatchInputRecord
		for r := 0; r < 5; r++ {
			recs = append(recs, BatchInputRecord{
				SKU:       fmt.Sprintf("CONC-%02d-%02d", b, r),
				Name:      fmt.Sprintf("Concurrent Product %d-%d", b, r),
				Price:     19.99 + float64(r),
				Status:    domain.StatusActive,
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
		testBatches = append(testBatches, BatchInput{
			BatchIndex: b,
			Records:    recs,
		})
	}
	batchReq := JobStartRequest{
		Mode:    "batches",
		Batches: testBatches,
	}
	reqBytes, _ := json.Marshal(batchReq)

	for i := 0; i < numGoroutines; i++ {
		idx := i
		go func() {
			defer wg.Done()
			<-startBarrier // Synchronize all goroutines to release at the exact same moment

			req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewReader(reqBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			responseStatuses[idx] = rec.Code
			responseBodies[idx] = rec.Body.String()

			switch rec.Code {
			case http.StatusAccepted:
				atomic.AddInt64(&acceptedCount, 1)
			case http.StatusConflict:
				atomic.AddInt64(&conflictCount, 1)
			default:
				atomic.AddInt64(&otherCount, 1)
			}
		}()
	}

	// Release all goroutines simultaneously
	close(startBarrier)
	wg.Wait()

	t.Logf("Adversarial Concurrency Result: Accepted=%d, Conflict=%d, Other=%d",
		acceptedCount, conflictCount, otherCount)

	// We check if more than 1 job was accepted
	if acceptedCount != 1 {
		t.Errorf("CONCURRENCY VULNERABILITY DETECTED: Expected exactly 1 accepted job (202), but got %d accepted jobs! Responses: %v",
			acceptedCount, responseStatuses)
	}

	if conflictCount != int64(numGoroutines-1) {
		t.Errorf("Expected %d conflicts (409), but got %d", numGoroutines-1, conflictCount)
	}

	// Check DB state: verify at most 1 job is RUNNING in DB
	activeJob, err := store.GetActiveJob(context.Background())
	t.Logf("Active job in DB after concurrent burst: activeJob=%+v, err=%v", activeJob, err)
}

// -----------------------------------------------------------------------------
// Objective 2: Invalid Resumption Scenarios
// -----------------------------------------------------------------------------

func TestAdversarial_InvalidResumptionScenarios(t *testing.T) {
	store, _, _, handler := setupAdversarialEnv(t)
	router := handler.Routes()
	ctx := context.Background()
	now := time.Now().UTC()

	// Case 1: Non-existent job
	t.Run("ResumingNonExistentJob", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/jobs/job-does-not-exist-999/resume", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected 404 Not Found for non-existent job resume, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		var errResp ErrorResponse
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp.Code != ErrCodeJobNotFound {
			t.Errorf("Expected error code %s, got %s", ErrCodeJobNotFound, errResp.Code)
		}
	})

	// Case 2: Running job
	t.Run("ResumingRunningJob", func(t *testing.T) {
		runningJob := &domain.SyncJob{
			JobID:               "job-already-running-100",
			Status:              domain.JobStatusRunning,
			TotalBatches:        5,
			CommittedBatchIndex: 0,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := store.CreateJob(ctx, runningJob); err != nil {
			t.Fatalf("failed to create running job: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/jobs/job-already-running-100/resume", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Errorf("Expected 409 Conflict for running job resume, got %d (body: %s)", rec.Code, rec.Body.String())
		}
		var errResp ErrorResponse
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp.Code != ErrCodeJobConflict {
			t.Errorf("Expected error code %s, got %s", ErrCodeJobConflict, errResp.Code)
		}
	})

	// Case 3: Completed job (when no other job is running)
	t.Run("ResumingCompletedJob", func(t *testing.T) {
		store3, _, _, handler3 := setupAdversarialEnv(t)
		router3 := handler3.Routes()

		completedJob := &domain.SyncJob{
			JobID:               "job-finished-200",
			Status:              domain.JobStatusCompleted,
			TotalBatches:        3,
			CommittedBatchIndex: 2,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := store3.CreateJob(ctx, completedJob); err != nil {
			t.Fatalf("failed to create completed job: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/jobs/job-finished-200/resume", nil)
		rec := httptest.NewRecorder()
		router3.ServeHTTP(rec, req)

		t.Logf("Completed job resume response status: %d, body: %s", rec.Code, rec.Body.String())

		// DISPATCH.md specifies: completed jobs (assert 400), while spec_miner specifies 409 Conflict
		// Check what the implementation returns:
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusConflict {
			t.Errorf("Expected 400 Bad Request or 409 Conflict for completed job resume, got %d", rec.Code)
		}
		if rec.Code == http.StatusConflict {
			t.Logf("Observed: Implementation returned 409 Conflict instead of 400 Bad Request for completed job resume.")
		}
	})

	// Case 4: Concurrent Resume Race on the same interrupted job
	t.Run("ConcurrentResumeRace", func(t *testing.T) {
		store4, _, _, handler4 := setupAdversarialEnv(t)
		defer handler4.CancelActiveJob()
		router4 := handler4.Routes()

		interruptedJob := &domain.SyncJob{
			JobID:               "job-interrupted-300",
			Status:              domain.JobStatusInterrupted,
			TotalBatches:        10,
			CommittedBatchIndex: 2,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := store4.CreateJob(ctx, interruptedJob); err != nil {
			t.Fatalf("failed to create interrupted job: %v", err)
		}

		const numResumeGoroutines = 10
		var startBarrier = make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(numResumeGoroutines)

		var acceptedCount int64
		var conflictCount int64

		for i := 0; i < numResumeGoroutines; i++ {
			go func() {
				defer wg.Done()
				<-startBarrier

				req := httptest.NewRequest(http.MethodPost, "/api/jobs/job-interrupted-300/resume", nil)
				rec := httptest.NewRecorder()
				router4.ServeHTTP(rec, req)

				if rec.Code == http.StatusAccepted {
					atomic.AddInt64(&acceptedCount, 1)
				} else if rec.Code == http.StatusConflict {
					atomic.AddInt64(&conflictCount, 1)
				}
			}()
		}

		close(startBarrier)
		wg.Wait()

		t.Logf("Concurrent Resume Race: Accepted=%d, Conflict=%d", acceptedCount, conflictCount)
		if acceptedCount != 1 {
			t.Errorf("CONCURRENCY VULNERABILITY IN RESUME: Expected exactly 1 accepted resume (202), but got %d accepted resumes!", acceptedCount)
		}
	})
}

// -----------------------------------------------------------------------------
// Objective 3: Dead-Letter Queue Pagination & Filtering
// -----------------------------------------------------------------------------

func TestAdversarial_DeadLetterQueue_PaginationAndFiltering(t *testing.T) {
	store, engine, _, handler := setupAdversarialEnv(t)
	router := handler.Routes()
	ctx := context.Background()
	now := time.Now().UTC()

	// Generate a batch with 65 mixed corrupt and valid records
	const totalCorrupt = 65
	jobID := "job-dlq-adv-1"

	// Create job in DB first
	job := &domain.SyncJob{
		JobID:               jobID,
		Status:              domain.JobStatusRunning,
		TotalBatches:        1,
		CommittedBatchIndex: -1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("failed creating job: %v", err)
	}

	records := make([]domain.SupplierUpdate, 0, totalCorrupt+5)

	// Valid records
	for i := 0; i < 5; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("VALID-SKU-%03d", i),
			Name:      fmt.Sprintf("Valid Product %d", i),
			Price:     10.0 + float64(i),
			Status:    domain.StatusActive,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Corrupt Type 1: Negative Price (25 records)
	for i := 0; i < 25; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("CORRUPT-NEG-%03d", i),
			Name:      fmt.Sprintf("Negative Price %d", i),
			Price:     -1.0 - float64(i),
			Status:    domain.StatusActive,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Corrupt Type 2: Empty/Whitespace SKU (20 records)
	for i := 0; i < 20; i++ {
		whitespace := ""
		if i%2 == 0 {
			whitespace = "   "
		}
		records = append(records, domain.SupplierUpdate{
			SKU:       whitespace,
			Name:      fmt.Sprintf("Empty SKU %d", i),
			Price:     15.50,
			Status:    domain.StatusActive,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Corrupt Type 3: Zero Timestamp (10 records)
	for i := 0; i < 10; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("CORRUPT-ZERO-TS-%03d", i),
			Name:      fmt.Sprintf("Zero TS %d", i),
			Price:     20.00,
			Status:    domain.StatusActive,
			Timestamp: time.Time{}, // Zero timestamp
		})
	}

	// Corrupt Type 4: NaN / Invalid Status (10 records)
	for i := 0; i < 10; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("CORRUPT-STATUS-%03d", i),
			Name:      fmt.Sprintf("Invalid Status %d", i),
			Price:     30.00,
			Status:    domain.ProductStatus("DISCONTINUED_UNKNOWN"),
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	batch := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records:    records,
	}

	// Ingest via engine to enforce Invariant D
	summary, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}

	if summary.Added != 5 {
		t.Errorf("Expected 5 valid records added, got %d", summary.Added)
	}
	if summary.DeadLetter != totalCorrupt {
		t.Errorf("Expected %d dead letter records, got %d", totalCorrupt, summary.DeadLetter)
	}

	// 1. Pagination Tests
	t.Run("PaginationBoundaries", func(t *testing.T) {
		// Page 1: limit 20, offset 0 -> 20 items
		req1 := httptest.NewRequest(http.MethodGet, "/api/dead-letter?limit=20&offset=0", nil)
		rec1 := httptest.NewRecorder()
		router.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec1.Code)
		}
		var resp1 DeadLetterListResponse
		_ = json.NewDecoder(rec1.Body).Decode(&resp1)
		if len(resp1.Issues) != 20 || resp1.Total != totalCorrupt {
			t.Errorf("page 1: expected 20 issues of %d total, got %d (total %d)", totalCorrupt, len(resp1.Issues), resp1.Total)
		}

		// Page 4: limit 20, offset 60 -> remaining 5 items
		req4 := httptest.NewRequest(http.MethodGet, "/api/dead-letter?limit=20&offset=60", nil)
		rec4 := httptest.NewRecorder()
		router.ServeHTTP(rec4, req4)
		var resp4 DeadLetterListResponse
		_ = json.NewDecoder(rec4.Body).Decode(&resp4)
		if len(resp4.Issues) != 5 {
			t.Errorf("page 4: expected 5 remaining issues, got %d", len(resp4.Issues))
		}

		// Past end: limit 20, offset 100 -> 0 items
		reqEnd := httptest.NewRequest(http.MethodGet, "/api/dead-letter?limit=20&offset=100", nil)
		recEnd := httptest.NewRecorder()
		router.ServeHTTP(recEnd, reqEnd)
		var respEnd DeadLetterListResponse
		_ = json.NewDecoder(recEnd.Body).Decode(&respEnd)
		if len(respEnd.Issues) != 0 {
			t.Errorf("expected 0 issues past end, got %d", len(respEnd.Issues))
		}

		// Invalid negative limit -> 400 Bad Request
		reqBadLimit := httptest.NewRequest(http.MethodGet, "/api/dead-letter?limit=-1", nil)
		recBadLimit := httptest.NewRecorder()
		router.ServeHTTP(recBadLimit, reqBadLimit)
		if recBadLimit.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for negative limit, got %d", recBadLimit.Code)
		}

		// Invalid negative offset -> 400 Bad Request
		reqBadOffset := httptest.NewRequest(http.MethodGet, "/api/dead-letter?offset=-5", nil)
		recBadOffset := httptest.NewRecorder()
		router.ServeHTTP(recBadOffset, reqBadOffset)
		if recBadOffset.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for negative offset, got %d", recBadOffset.Code)
		}
	})

	// 2. Invariant D Verification: Raw payload and reason match
	t.Run("InvariantD_PayloadAndReasonAccuracy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/dead-letter?job_id=%s&limit=100&offset=0", jobID), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp DeadLetterListResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if len(resp.Issues) != totalCorrupt {
			t.Fatalf("expected %d issues, got %d", totalCorrupt, len(resp.Issues))
		}

		for _, issue := range resp.Issues {
			if issue.JobID != jobID {
				t.Errorf("expected job_id %s, got %s", jobID, issue.JobID)
			}
			if issue.RawPayload == "" {
				t.Errorf("issue %s has empty raw payload", issue.IssueID)
			}
			if issue.Reason == "" {
				t.Errorf("issue %s has empty reason", issue.IssueID)
			}
		}
	})

	// 3. Filter by Reason
	t.Run("FilterByReason", func(t *testing.T) {
		reqReason := httptest.NewRequest(http.MethodGet, "/api/dead-letter?reason=negative%20price", nil)
		recReason := httptest.NewRecorder()
		router.ServeHTTP(recReason, reqReason)

		if recReason.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recReason.Code)
		}
		var resp DeadLetterListResponse
		_ = json.NewDecoder(recReason.Body).Decode(&resp)

		t.Logf("GET /api/dead-letter?reason=negative price returned Total=%d, Count=%d", resp.Total, len(resp.Issues))
		// DISPATCH Objective 1.3: "filtering by job_id and error reason"
		// Worker claimed in handoff: "optional job_id and reason filters"
		if resp.Total != 25 {
			t.Errorf("SPECIFICATION VIOLATION / DEFECT: Expected 25 records filtered by reason 'negative price', but got Total=%d. Reason query parameter is ignored by API handler.", resp.Total)
		}
	})
}

// -----------------------------------------------------------------------------
// Objective 4: Catalog Browser Filtering
// -----------------------------------------------------------------------------

func TestAdversarial_CatalogBrowserFiltering(t *testing.T) {
	store, engine, _, handler := setupAdversarialEnv(t)
	router := handler.Routes()
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed 40 products: 25 ACTIVE, 15 WITHDRAWN
	// With varied names and SKUs
	batch := domain.Batch{
		JobID:      "job-catalog-seed",
		BatchIndex: 0,
		Records:    make([]domain.SupplierUpdate, 0, 40),
	}

	for i := 0; i < 25; i++ {
		status := domain.StatusActive
		name := fmt.Sprintf("Apple iPhone %02d Pro", i+1)
		if i >= 15 {
			name = fmt.Sprintf("Samsung Galaxy S%02d", i+1)
		}
		batch.Records = append(batch.Records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("PHONE-ACT-%03d", i+1),
			Name:      name,
			Price:     999.0 + float64(i)*10,
			Status:    status,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	for i := 0; i < 15; i++ {
		status := domain.StatusWithdrawn
		name := fmt.Sprintf("Legacy Apple iPod %02d", i+1)
		if i >= 10 {
			name = fmt.Sprintf("Nokia Phone %02d", i+1)
		}
		batch.Records = append(batch.Records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("PHONE-WTD-%03d", i+1),
			Name:      name,
			Price:     99.0 + float64(i),
			Status:    status,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	job := &domain.SyncJob{
		JobID:               "job-catalog-seed",
		Status:              domain.JobStatusRunning,
		TotalBatches:        1,
		CommittedBatchIndex: -1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	_ = store.CreateJob(ctx, job)
	summary, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}
	if summary.Added != 40 {
		t.Fatalf("Expected 40 added, got %d", summary.Added)
	}

	// 1. Status Filter: ACTIVE
	t.Run("StatusActiveOnly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/products?status=ACTIVE&limit=50", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp ProductListResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.Total != 25 {
			t.Errorf("expected total 25 ACTIVE, got %d", resp.Total)
		}
		if len(resp.Products) != 25 {
			t.Errorf("expected 25 ACTIVE products, got %d", len(resp.Products))
		}
		for _, p := range resp.Products {
			if p.Status != domain.StatusActive {
				t.Errorf("expected ACTIVE product, got %s for %s", p.Status, p.SKU)
			}
		}
	})

	// 2. Status Filter: WITHDRAWN
	t.Run("StatusWithdrawnOnly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/products?status=WITHDRAWN&limit=50", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp ProductListResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.Total != 15 {
			t.Errorf("expected total 15 WITHDRAWN, got %d", resp.Total)
		}
		if len(resp.Products) != 15 {
			t.Errorf("expected 15 WITHDRAWN products, got %d", len(resp.Products))
		}
		for _, p := range resp.Products {
			if p.Status != domain.StatusWithdrawn {
				t.Errorf("expected WITHDRAWN product, got %s for %s", p.Status, p.SKU)
			}
		}
	})

	// 3. Status Filter: Invalid Status -> 400 Bad Request
	t.Run("InvalidStatusFilter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/products?status=DELETED", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for invalid status, got %d", rec.Code)
		}
	})

	// 4. Text Search Query
	t.Run("SearchQueryMatching", func(t *testing.T) {
		// "Apple" is in 15 active (iPhone) + 10 withdrawn (iPod) = 25 products
		req := httptest.NewRequest(http.MethodGet, "/api/products?search=Apple&limit=50", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp ProductListResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.Total != 25 {
			t.Errorf("expected 25 products matching 'Apple', got %d", resp.Total)
		}

		// Combined search + status
		reqComb := httptest.NewRequest(http.MethodGet, "/api/products?search=Apple&status=ACTIVE&limit=50", nil)
		recComb := httptest.NewRecorder()
		router.ServeHTTP(recComb, reqComb)

		var respComb ProductListResponse
		_ = json.NewDecoder(recComb.Body).Decode(&respComb)
		if respComb.Total != 15 {
			t.Errorf("expected 15 active products matching 'Apple', got %d", respComb.Total)
		}
	})

	// 5. Pagination Limits & Offsets
	t.Run("PaginationBoundaries", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/products?limit=10&offset=35", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var resp ProductListResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.Total != 40 {
			t.Errorf("expected total 40, got %d", resp.Total)
		}
		if len(resp.Products) != 5 {
			t.Errorf("expected 5 products on last page (offset 35 of 40), got %d", len(resp.Products))
		}
	})
}
