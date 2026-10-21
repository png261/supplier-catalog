package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coderpush/pkg/domain"
)

// -----------------------------------------------------------------------------
// Empirical Challenge 1: POST /api/jobs Concurrency (20 concurrent requests)
// -----------------------------------------------------------------------------

func TestChallenger_JobCreation_20ConcurrentRequests(t *testing.T) {
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
	statuses := make([]int, numGoroutines)
	bodies := make([]string, numGoroutines)

	// Direct batches payload that requires slight processing
	payload := JobStartRequest{
		Mode: "batches",
		Batches: []BatchInput{
			{
				BatchIndex: 0,
				Records: []BatchInputRecord{
					{
						SKU:       "CHALLENGE-001",
						Name:      "Challenge Product 1",
						Price:     49.99,
						Status:    domain.StatusActive,
						Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
					},
				},
			},
			{
				BatchIndex: 1,
				Records: []BatchInputRecord{
					{
						SKU:       "CHALLENGE-002",
						Name:      "Challenge Product 2",
						Price:     99.99,
						Status:    domain.StatusActive,
						Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
					},
				},
			},
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	for i := 0; i < numGoroutines; i++ {
		idx := i
		go func() {
			defer wg.Done()
			<-startBarrier // Barrier release: all goroutines fire simultaneously

			req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			statuses[idx] = rec.Code
			bodies[idx] = rec.Body.String()

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

	// Release burst
	close(startBarrier)
	wg.Wait()

	t.Logf("20-Burst Result: Accepted=%d, Conflict=%d, Other=%d", acceptedCount, conflictCount, otherCount)

	// Verification 1: Exactly 1 accepted (HTTP 202)
	if acceptedCount != 1 {
		t.Fatalf("CONCURRENCY FAILURE: Expected exactly 1 accepted job (202), got %d. Responses: %v",
			acceptedCount, statuses)
	}

	// Verification 2: Exactly 19 conflicts (HTTP 409)
	if conflictCount != int64(numGoroutines-1) {
		t.Fatalf("CONCURRENCY FAILURE: Expected %d conflicts (409), got %d", numGoroutines-1, conflictCount)
	}

	if otherCount != 0 {
		t.Fatalf("UNEXPECTED RESPONSE: Expected 0 other responses, got %d", otherCount)
	}

	// Verification 3: Verify the 409 error bodies contain code JOB_CONFLICT
	for idx, code := range statuses {
		if code == http.StatusConflict {
			var errResp ErrorResponse
			if err := json.Unmarshal([]byte(bodies[idx]), &errResp); err != nil {
				t.Errorf("Goroutine %d: failed to parse 409 body: %v", idx, err)
			} else if errResp.Code != ErrCodeJobConflict {
				t.Errorf("Goroutine %d: expected code JOB_CONFLICT, got %s", idx, errResp.Code)
			}
		} else if code == http.StatusAccepted {
			var jobResp SyncJobResponse
			if err := json.Unmarshal([]byte(bodies[idx]), &jobResp); err != nil {
				t.Errorf("Goroutine %d: failed to parse 202 body: %v", idx, err)
			} else if jobResp.Status != domain.JobStatusRunning {
				t.Errorf("Goroutine %d: expected status RUNNING, got %s", idx, jobResp.Status)
			}
		}
	}

	// Verification 4: Storage state check — exactly 1 active job in SQLite
	activeJob, err := store.GetActiveJob(context.Background())
	if err != nil {
		t.Fatalf("failed to query active job in storage: %v", err)
	}
	if activeJob == nil {
		t.Fatalf("expected 1 active job in storage, but found nil")
	}
	t.Logf("Verified exactly 1 active job in storage: %s", activeJob.JobID)
}

// -----------------------------------------------------------------------------
// Empirical Challenge 2: POST /api/jobs/{id}/resume Concurrency (10 concurrent requests)
// -----------------------------------------------------------------------------

func TestChallenger_JobResume_10ConcurrentRequests(t *testing.T) {
	now := time.Now().UTC()

	t.Run("InterruptedJob_10ConcurrentResumes", func(t *testing.T) {
		store, _, _, handler := setupAdversarialEnv(t)
		defer handler.CancelActiveJob()
		router := handler.Routes()
		ctx := context.Background()

		jobID := "job-interrupted-challenge-1"
		interruptedJob := &domain.SyncJob{
			JobID:               jobID,
			Status:              domain.JobStatusInterrupted,
			TotalBatches:        10,
			CommittedBatchIndex: 3,
			Summary: domain.ChangeSummary{
				Added:   15,
				Updated: 5,
			},
			CreatedAt: now.Add(-10 * time.Minute),
			UpdatedAt: now.Add(-5 * time.Minute),
		}
		if err := store.CreateJob(ctx, interruptedJob); err != nil {
			t.Fatalf("failed to seed interrupted job: %v", err)
		}

		const numResumeGoroutines = 10
		var startBarrier = make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(numResumeGoroutines)

		var acceptedCount int64
		var conflictCount int64
		var otherCount int64
		statuses := make([]int, numResumeGoroutines)
		bodies := make([]string, numResumeGoroutines)

		for i := 0; i < numResumeGoroutines; i++ {
			idx := i
			go func() {
				defer wg.Done()
				<-startBarrier

				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/jobs/%s/resume", jobID), nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				statuses[idx] = rec.Code
				bodies[idx] = rec.Body.String()

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

		// Release all 10 simultaneously
		close(startBarrier)
		wg.Wait()

		t.Logf("10-Resume Burst Result: Accepted=%d, Conflict=%d, Other=%d", acceptedCount, conflictCount, otherCount)

		// Verification 1: Exactly 1 accepted (HTTP 202)
		if acceptedCount != 1 {
			t.Fatalf("RESUME CONCURRENCY FAILURE: Expected exactly 1 accepted resume (202), got %d. Responses: %v",
				acceptedCount, statuses)
		}

		// Verification 2: Exactly 9 conflicts (HTTP 409)
		if conflictCount != int64(numResumeGoroutines-1) {
			t.Fatalf("RESUME CONCURRENCY FAILURE: Expected %d conflicts (409), got %d", numResumeGoroutines-1, conflictCount)
		}

		if otherCount != 0 {
			t.Fatalf("UNEXPECTED RESPONSE: Expected 0 other responses, got %d", otherCount)
		}

		// Verification 3: Verify the 409 error bodies contain code JOB_CONFLICT
		for idx, code := range statuses {
			if code == http.StatusConflict {
				var errResp ErrorResponse
				if err := json.Unmarshal([]byte(bodies[idx]), &errResp); err != nil {
					t.Errorf("Resume Goroutine %d: failed to parse 409 body: %v", idx, err)
				} else if errResp.Code != ErrCodeJobConflict {
					t.Errorf("Resume Goroutine %d: expected code JOB_CONFLICT, got %s", idx, errResp.Code)
				}
			} else if code == http.StatusAccepted {
				var jobResp SyncJobResponse
				if err := json.Unmarshal([]byte(bodies[idx]), &jobResp); err != nil {
					t.Errorf("Resume Goroutine %d: failed to parse 202 body: %v", idx, err)
				} else if jobResp.Status != domain.JobStatusRunning {
					t.Errorf("Resume Goroutine %d: expected status RUNNING, got %s", idx, jobResp.Status)
				}
			}
		}

		// Verification 4: Storage job is now RUNNING
		j, err := store.GetJob(ctx, jobID)
		if err != nil {
			t.Fatalf("failed to query job in storage: %v", err)
		}
		if j.Status != domain.JobStatusRunning {
			t.Errorf("expected job status to be RUNNING in storage, got %s", j.Status)
		}
	})

	t.Run("NonExistentJob_10ConcurrentResumes", func(t *testing.T) {
		_, _, _, handler := setupAdversarialEnv(t)
		router := handler.Routes()

		const numGoroutines = 10
		var startBarrier = make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		var notFoundCount int64
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				<-startBarrier

				req := httptest.NewRequest(http.MethodPost, "/api/jobs/job-does-not-exist-xyz/resume", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				if rec.Code == http.StatusNotFound {
					atomic.AddInt64(&notFoundCount, 1)
				}
			}()
		}

		close(startBarrier)
		wg.Wait()

		if notFoundCount != int64(numGoroutines) {
			t.Fatalf("expected all %d requests to return 404 NotFound, got %d", numGoroutines, notFoundCount)
		}
	})

	t.Run("CompletedJob_10ConcurrentResumes", func(t *testing.T) {
		store, _, _, handler := setupAdversarialEnv(t)
		router := handler.Routes()
		ctx := context.Background()

		completedJobID := "job-completed-challenge"
		completedJob := &domain.SyncJob{
			JobID:               completedJobID,
			Status:              domain.JobStatusCompleted,
			TotalBatches:        3,
			CommittedBatchIndex: 2,
			CreatedAt:           now.Add(-20 * time.Minute),
			UpdatedAt:           now.Add(-10 * time.Minute),
		}
		if err := store.CreateJob(ctx, completedJob); err != nil {
			t.Fatalf("failed to seed completed job: %v", err)
		}

		const numGoroutines = 10
		var startBarrier = make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		var conflictCount int64
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				<-startBarrier

				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/jobs/%s/resume", completedJobID), nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				if rec.Code == http.StatusConflict {
					atomic.AddInt64(&conflictCount, 1)
				}
			}()
		}

		close(startBarrier)
		wg.Wait()

		if conflictCount != int64(numGoroutines) {
			t.Fatalf("expected all %d requests to return 409 Conflict for completed job, got %d", numGoroutines, conflictCount)
		}
	})
}

// -----------------------------------------------------------------------------
// Empirical Challenge 3: Dead-Letter Queue Reason Filtering & Security
// -----------------------------------------------------------------------------

func TestChallenger_DeadLetter_ReasonFilteringAccuracyAndSecurity(t *testing.T) {
	_, engine, _, handler := setupAdversarialEnv(t)
	router := handler.Routes()
	ctx := context.Background()
	now := time.Now().UTC()

	// Batch 1 on job-dlq-1
	batch1 := domain.Batch{
		JobID:      "job-dlq-1",
		BatchIndex: 0,
		Records:    make([]domain.SupplierUpdate, 0),
	}
	// 10 negative price
	for i := 0; i < 10; i++ {
		batch1.Records = append(batch1.Records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("NEG-PRICE-%03d", i+1),
			Name:      "Product Negative",
			Price:     -10.50,
			Status:    domain.StatusActive,
			Timestamp: now,
		})
	}
	// 8 empty SKU
	for i := 0; i < 8; i++ {
		batch1.Records = append(batch1.Records, domain.SupplierUpdate{
			SKU:       "   ",
			Name:      "Product Empty SKU",
			Price:     15.00,
			Status:    domain.StatusActive,
			Timestamp: now,
		})
	}
	// 4 negative price discount
	for i := 0; i < 4; i++ {
		batch1.Records = append(batch1.Records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("DISC-PRICE-%03d", i+1),
			Name:      "Product Discount",
			Price:     -5.00,
			Status:    domain.StatusActive,
			Timestamp: now,
		})
	}

	stream1 := make(chan domain.Batch, 1)
	stream1 <- batch1
	close(stream1)
	if _, err := engine.RunJob(ctx, "job-dlq-1", stream1); err != nil {
		t.Fatalf("engine failed running job-dlq-1: %v", err)
	}

	// Batch 2 on job-dlq-2
	batch2 := domain.Batch{
		JobID:      "job-dlq-2",
		BatchIndex: 0,
		Records:    make([]domain.SupplierUpdate, 0),
	}
	// 5 corrupt with empty sku
	for i := 0; i < 5; i++ {
		batch2.Records = append(batch2.Records, domain.SupplierUpdate{
			SKU:       "",
			Name:      "Product Corrupt",
			Price:     20.00,
			Status:    domain.StatusActive,
			Timestamp: now,
		})
	}
	// 7 with negative price
	for i := 0; i < 7; i++ {
		batch2.Records = append(batch2.Records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("STATUS-ERR-%03d", i+1),
			Name:      "Product Status Error",
			Price:     -1.00,
			Status:    domain.StatusActive,
			Timestamp: now,
		})
	}

	stream2 := make(chan domain.Batch, 1)
	stream2 <- batch2
	close(stream2)
	if _, err := engine.RunJob(ctx, "job-dlq-2", stream2); err != nil {
		t.Fatalf("engine failed running job-dlq-2: %v", err)
	}

	// Total dead-letter issues recorded:
	// job-dlq-1: 10 (price negative) + 8 (sku empty) + 4 (price negative) = 22 issues
	// job-dlq-2: 5 (sku empty) + 7 (price negative) = 12 issues
	// Total overall = 34 issues
	// Breakdown:
	// "price" / "negative": 10 + 4 + 7 = 21 issues
	// "sku" / "empty": 8 + 5 = 13 issues

	type testCase struct {
		name          string
		query         string
		expectedTotal int
		expectedCount int
	}

	testCases := []testCase{
		{
			name:          "NoFilter_AllRecords",
			query:         "/api/dead-letter",
			expectedTotal: 34,
			expectedCount: 20, // default limit 20
		},
		{
			name:          "EmptyReasonFilter_AllRecords",
			query:         "/api/dead-letter?reason=",
			expectedTotal: 34,
			expectedCount: 20,
		},
		{
			name:          "WhitespaceReasonFilter_AllRecords",
			query:         "/api/dead-letter?reason=%20%20%20",
			expectedTotal: 34,
			expectedCount: 20,
		},
		{
			name:          "Reason_Price_SubstringExact",
			query:         "/api/dead-letter?reason=price",
			expectedTotal: 21,
			expectedCount: 20, // limit 20
		},
		{
			name:          "Reason_Price_CaseUppercase",
			query:         "/api/dead-letter?reason=PRICE",
			expectedTotal: 21,
			expectedCount: 20,
		},
		{
			name:          "Reason_Negative_CaseMixed",
			query:         "/api/dead-letter?reason=NeGaTiVe",
			expectedTotal: 21,
			expectedCount: 20,
		},
		{
			name:          "Reason_SKU_Empty",
			query:         "/api/dead-letter?reason=empty",
			expectedTotal: 13,
			expectedCount: 13,
		},
		{
			name:          "Reason_NonExistent",
			query:         "/api/dead-letter?reason=nonexistent_error_xyz_12345",
			expectedTotal: 0,
			expectedCount: 0,
		},
		{
			name:          "Reason_And_JobID_Conjunction_Job1",
			query:         "/api/dead-letter?job_id=job-dlq-1&reason=empty",
			expectedTotal: 8,
			expectedCount: 8,
		},
		{
			name:          "Reason_And_JobID_Conjunction_Job2",
			query:         "/api/dead-letter?job_id=job-dlq-2&reason=empty",
			expectedTotal: 5,
			expectedCount: 5,
		},
		{
			name:          "Pagination_Reason_Price_Offset0_Limit10",
			query:         "/api/dead-letter?reason=price&limit=10&offset=0",
			expectedTotal: 21,
			expectedCount: 10,
		},
		{
			name:          "Pagination_Reason_Price_Offset10_Limit10",
			query:         "/api/dead-letter?reason=price&limit=10&offset=10",
			expectedTotal: 21,
			expectedCount: 10,
		},
		{
			name:          "Pagination_Reason_Price_Offset20_Limit10",
			query:         "/api/dead-letter?reason=price&limit=10&offset=20",
			expectedTotal: 21,
			expectedCount: 1,
		},
		{
			name:          "Pagination_Reason_Price_OffsetBeyond_Limit10",
			query:         "/api/dead-letter?reason=price&limit=10&offset=30",
			expectedTotal: 21,
			expectedCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.query, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d, body: %s", rec.Code, rec.Body.String())
			}

			var resp DeadLetterListResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode JSON response: %v", err)
			}

			if resp.Total != tc.expectedTotal {
				t.Errorf("[%s] Expected Total=%d, got Total=%d", tc.name, tc.expectedTotal, resp.Total)
			}
			if len(resp.Issues) != tc.expectedCount {
				t.Errorf("[%s] Expected len(Issues)=%d, got %d", tc.name, tc.expectedCount, len(resp.Issues))
			}
			// Verify non-nil slice serialization for empty lists
			if tc.expectedCount == 0 && resp.Issues == nil {
				t.Errorf("[%s] Expected empty slice [] for Issues, got nil", tc.name)
			}
		})
	}

	// SQL Injection Resistance Stress Test
	sqlInjectionPayloads := []string{
		"' OR '1'='1",
		"'; DROP TABLE dead_letter_issues; --",
		"' UNION SELECT 1, 'job', 0, 'sku', '{}', 'injected', datetime('now') --",
		"1' AND (SELECT count(*) FROM sync_jobs) > 0 --",
		"' OR 1=1 --",
		"admin'--",
		"\" OR \"\"=\"",
	}

	for i, payload := range sqlInjectionPayloads {
		t.Run(fmt.Sprintf("SQLInjection_Payload_%d", i), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/dead-letter?reason="+url.QueryEscape(payload), nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// Must not crash or return HTTP 500
			if rec.Code != http.StatusOK {
				t.Fatalf("SQL Injection attack caused error HTTP %d: %s", rec.Code, rec.Body.String())
			}

			var resp DeadLetterListResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to parse response: %v", err)
			}

			// Since no reason literally matches the injected payload, total must be 0
			if resp.Total != 0 {
				t.Errorf("SQL INJECTION VULNERABILITY DETECTED! Payload '%s' matched %d records instead of 0",
					payload, resp.Total)
			}
		})
	}

	// Verify table integrity after all injection payloads: all 34 records must still exist
	reqFinal := httptest.NewRequest(http.MethodGet, "/api/dead-letter?limit=100", nil)
	recFinal := httptest.NewRecorder()
	router.ServeHTTP(recFinal, reqFinal)

	var respFinal DeadLetterListResponse
	_ = json.NewDecoder(recFinal.Body).Decode(&respFinal)
	if respFinal.Total != 34 {
		t.Fatalf("DATA CORRUPTION DETECTED: expected 34 records remaining after SQL injection tests, got %d", respFinal.Total)
	}
	t.Logf("SQL injection resistance verified: 0 vulnerabilities, 34/34 records intact.")
}
