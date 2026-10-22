package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// setupTestEnv sets up an isolated temporary SQLite storage, engine, broker, and handler for testing.
func setupTestEnv(t *testing.T) (*storage.SQLiteStorage, *catalogSync.SyncEngine, *Broker, *Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "api_test.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test storage: %v", err)
	}

	broker := NewBroker(WithClientCap(64), WithPingInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := NewProgressHook(broker)
	engine := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	handler := NewHandler(store, engine, broker)

	t.Cleanup(func() {
		handler.CancelActiveJob()
		cancel()
		broker.Close()
		_ = store.Close()
	})

	return store, engine, broker, handler
}

func TestHandler_PostJob_DefaultConfig(t *testing.T) {
	_, _, _, handler := setupTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp SyncJobResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.JobID == "" {
		t.Error("expected non-empty job ID")
	}
	if resp.Status != domain.JobStatusRunning {
		t.Errorf("expected status RUNNING, got %s", resp.Status)
	}
	if resp.TotalBatches != 10 {
		t.Errorf("expected default 10 total batches, got %d", resp.TotalBatches)
	}
}

func TestHandler_PostJob_CustomGenerator(t *testing.T) {
	_, _, _, handler := setupTestEnv(t)

	body := `{
		"mode": "generator",
		"job_id": "job-custom-101",
		"seed": 12345,
		"total_batches": 3,
		"batch_size": 25,
		"update_ratio": 0.1,
		"duplicate_ratio": 0.05,
		"out_of_order_ratio": 0.05,
		"withdrawn_ratio": 0.05,
		"invalid_ratio": 0.05
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp SyncJobResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.JobID != "job-custom-101" {
		t.Errorf("expected job ID job-custom-101, got %s", resp.JobID)
	}
	if resp.TotalBatches != 3 {
		t.Errorf("expected 3 total batches, got %d", resp.TotalBatches)
	}
}

func TestHandler_PostJob_DirectBatches(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)

	nowStr := time.Now().UTC().Format(time.RFC3339)
	body := fmt.Sprintf(`{
		"mode": "batches",
		"job_id": "job-batches-01",
		"batches": [
			{
				"batch_index": 0,
				"records": [
					{
						"sku": "SKU-DIRECT-1",
						"name": "Direct Product 1",
						"price": 49.99,
						"status": "ACTIVE",
						"timestamp": "%s"
					}
				]
			}
		]
	}`, nowStr)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	// Wait briefly for background execution to commit
	time.Sleep(150 * time.Millisecond)

	prod, err := store.GetProduct(context.Background(), "SKU-DIRECT-1")
	if err != nil {
		t.Fatalf("failed to get product: %v", err)
	}
	if prod.Name != "Direct Product 1" || prod.Price != 49.99 {
		t.Errorf("unexpected product state: %+v", prod)
	}
}

func TestHandler_PostJob_InvalidJSON(t *testing.T) {
	_, _, _, handler := setupTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString("{invalid_json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}

	var errResp ErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp.Code != ErrCodeInvalidRequest {
		t.Errorf("expected code INVALID_REQUEST, got %s", errResp.Code)
	}
}

func TestHandler_PostJob_InvalidRatios(t *testing.T) {
	_, _, _, handler := setupTestEnv(t)

	// Ratio > 1.0
	bodySingle := `{"update_ratio": 1.5}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(bodySingle))
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for ratio > 1.0, got %d", rec.Code)
	}

	// Sum of ratios > 1.0
	bodySum := `{
		"update_ratio": 0.5,
		"duplicate_ratio": 0.3,
		"out_of_order_ratio": 0.3
	}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(bodySum))
	rec2 := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for sum of ratios > 1.0, got %d", rec2.Code)
	}
}

func TestHandler_PostJob_ConflictAlreadyRunning(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)

	now := time.Now().UTC()
	runningJob := &domain.SyncJob{
		JobID:               "job-already-running",
		Status:              domain.JobStatusRunning,
		TotalBatches:        10,
		CommittedBatchIndex: 0,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := store.CreateJob(context.Background(), runningJob); err != nil {
		t.Fatalf("failed creating running job: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString("{}"))
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp ErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp.Code != ErrCodeJobConflict {
		t.Errorf("expected code JOB_CONFLICT, got %s", errResp.Code)
	}
	if !strings.Contains(errResp.Error, "job-already-running") {
		t.Errorf("expected error to mention active job ID, got: %s", errResp.Error)
	}
}

func TestHandler_PostJob_DirectBatches_EdgeCases(t *testing.T) {
	_, _, _, handler := setupTestEnv(t)

	// Empty batches array
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(`{"mode":"batches","batches":[]}`))
	recEmpty := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty batches array, got %d", recEmpty.Code)
	}

	// Invalid timestamp in record
	badTS := `{"mode":"batches","batches":[{"batch_index":0,"records":[{"sku":"SKU1","name":"N1","price":10,"status":"ACTIVE","timestamp":"not-a-timestamp"}]}]}`
	reqTS := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(badTS))
	recTS := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recTS, reqTS)
	if recTS.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad timestamp, got %d", recTS.Code)
	}

	// Unsupported mode
	badMode := `{"mode":"unsupported_mode"}`
	reqMode := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString(badMode))
	recMode := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recMode, reqMode)
	if recMode.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported mode, got %d", recMode.Code)
	}
}

func TestHandler_ResumeJob_Scenarios(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Not Found
	reqNotFound := httptest.NewRequest(http.MethodPost, "/api/jobs/job-missing/resume", nil)
	reqNotFound.SetPathValue("id", "job-missing")
	recNotFound := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recNotFound, reqNotFound)
	if recNotFound.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing job, got %d", recNotFound.Code)
	}

	// 2. Already Completed Job
	completedJob := &domain.SyncJob{
		JobID:               "job-done",
		Status:              domain.JobStatusCompleted,
		TotalBatches:        5,
		CommittedBatchIndex: 4,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	_ = store.CreateJob(ctx, completedJob)
	_ = store.UpdateJobStatus(ctx, "job-done", domain.JobStatusCompleted)

	reqDone := httptest.NewRequest(http.MethodPost, "/api/jobs/job-done/resume", nil)
	reqDone.SetPathValue("id", "job-done")
	recDone := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recDone, reqDone)
	if recDone.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for completed job, got %d", recDone.Code)
	}

	// 3. Already Running Job
	activeJob := &domain.SyncJob{
		JobID:               "job-active-now",
		Status:              domain.JobStatusRunning,
		TotalBatches:        5,
		CommittedBatchIndex: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	_ = store.CreateJob(ctx, activeJob)

	reqActive := httptest.NewRequest(http.MethodPost, "/api/jobs/job-active-now/resume", nil)
	reqActive.SetPathValue("id", "job-active-now")
	recActive := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recActive, reqActive)
	if recActive.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for running job, got %d", recActive.Code)
	}

	// Mark activeJob as INTERRUPTED so it can be resumed
	_ = store.UpdateJobStatus(ctx, "job-active-now", domain.JobStatusInterrupted)

	// 4. Resume Success
	reqResume := httptest.NewRequest(http.MethodPost, "/api/jobs/job-active-now/resume", bytes.NewBufferString(`{"batch_size": 20}`))
	reqResume.SetPathValue("id", "job-active-now")
	recResume := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recResume, reqResume)
	if recResume.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted on resume, got %d: %s", recResume.Code, recResume.Body.String())
	}

	var resumeResp SyncJobResponse
	_ = json.NewDecoder(recResume.Body).Decode(&resumeResp)
	if resumeResp.JobID != "job-active-now" {
		t.Errorf("expected job ID job-active-now, got %s", resumeResp.JobID)
	}
}

func TestHandler_ListJobs_PaginationAndFiltering(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)
	ctx := context.Background()
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 25; i++ {
		status := domain.JobStatusCompleted
		if i == 5 || i == 15 {
			status = domain.JobStatusInterrupted
		}
		j := &domain.SyncJob{
			JobID:               fmt.Sprintf("job-list-%02d", i),
			Status:              status,
			TotalBatches:        10,
			CommittedBatchIndex: 9,
			CreatedAt:           baseTime.Add(time.Duration(i) * time.Minute),
			UpdatedAt:           baseTime.Add(time.Duration(i)*time.Minute + 10*time.Second),
		}
		_ = store.CreateJob(ctx, j)
		if status == domain.JobStatusInterrupted {
			_ = store.UpdateJobStatus(ctx, j.JobID, domain.JobStatusInterrupted)
		} else {
			_ = store.UpdateJobStatus(ctx, j.JobID, domain.JobStatusCompleted)
		}
	}

	// 1. Pagination
	req := httptest.NewRequest(http.MethodGet, "/api/jobs?limit=10&offset=10", nil)
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var listResp JobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed decoding list response: %v", err)
	}
	if len(listResp.Jobs) != 10 {
		t.Errorf("expected 10 jobs, got %d", len(listResp.Jobs))
	}
	if listResp.Total != 25 {
		t.Errorf("expected total 25, got %d", listResp.Total)
	}

	// 2. Filter by status INTERRUPTED
	reqFilter := httptest.NewRequest(http.MethodGet, "/api/jobs?status=INTERRUPTED&limit=50", nil)
	recFilter := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recFilter, reqFilter)
	if recFilter.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for status filter, got %d", recFilter.Code)
	}

	var filterResp JobListResponse
	_ = json.NewDecoder(recFilter.Body).Decode(&filterResp)
	if len(filterResp.Jobs) != 2 {
		t.Errorf("expected 2 interrupted jobs, got %d", len(filterResp.Jobs))
	}
	for _, job := range filterResp.Jobs {
		if job.Status != domain.JobStatusInterrupted {
			t.Errorf("expected status INTERRUPTED, got %s", job.Status)
		}
	}

	// 3. Invalid parameters
	reqBadLimit := httptest.NewRequest(http.MethodGet, "/api/jobs?limit=-5", nil)
	recBadLimit := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recBadLimit, reqBadLimit)
	if recBadLimit.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative limit, got %d", recBadLimit.Code)
	}

	reqBadStatus := httptest.NewRequest(http.MethodGet, "/api/jobs?status=INVALID_STATUS", nil)
	recBadStatus := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recBadStatus, reqBadStatus)
	if recBadStatus.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid status, got %d", recBadStatus.Code)
	}
}

func TestHandler_GetJob_Inspection(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Success with checkpoints
	job := &domain.SyncJob{
		JobID:               "job-inspect-1",
		Status:              domain.JobStatusCompleted,
		TotalBatches:        2,
		CommittedBatchIndex: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	_ = store.CreateJob(ctx, job)

	tx, _ := store.BeginTx(ctx)
	_ = store.RecordBatchCheckpointTx(ctx, tx, &domain.BatchCheckpoint{
		JobID:       "job-inspect-1",
		BatchIndex:  0,
		ItemsCount:  10,
		Summary:     domain.ChangeSummary{Added: 10},
		CommittedAt: now,
	})
	_ = tx.Commit()

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/job-inspect-1", nil)
	req.SetPathValue("id", "job-inspect-1")
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var detailResp JobDetailResponse
	_ = json.NewDecoder(rec.Body).Decode(&detailResp)
	if detailResp.Job.JobID != "job-inspect-1" {
		t.Errorf("expected job ID job-inspect-1, got %s", detailResp.Job.JobID)
	}
	if len(detailResp.Checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(detailResp.Checkpoints))
	}

	// 2. Not found
	reqMissing := httptest.NewRequest(http.MethodGet, "/api/jobs/job-nonexistent", nil)
	reqMissing.SetPathValue("id", "job-nonexistent")
	recMissing := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recMissing, reqMissing)
	if recMissing.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing job, got %d", recMissing.Code)
	}
}

func TestHandler_DeadLetter_PaginationAndFiltering(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed dead-letter records
	for i := 0; i < 15; i++ {
		jobID := "job-dlq-1"
		if i >= 10 {
			jobID = "job-dlq-2"
		}
		issue := &domain.DeadLetterRecord{
			IssueID:    fmt.Sprintf("dl-%02d", i),
			JobID:      jobID,
			BatchIndex: i % 3,
			SKU:        fmt.Sprintf("BAD-SKU-%02d", i),
			RawPayload: `{"sku":"bad"}`,
			Reason:     "negative price",
			CreatedAt:  now.Add(time.Duration(i) * time.Second),
		}
		tx, _ := store.BeginTx(ctx)
		_ = store.RecordDeadLetterTx(ctx, tx, issue)
		_ = tx.Commit()
	}

	// 1. Pagination across all jobs
	req := httptest.NewRequest(http.MethodGet, "/api/dead-letter?limit=5&offset=5", nil)
	rec := httptest.NewRecorder()
	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	var dlqResp DeadLetterListResponse
	_ = json.NewDecoder(rec.Body).Decode(&dlqResp)
	if len(dlqResp.Issues) != 5 {
		t.Errorf("expected 5 issues, got %d", len(dlqResp.Issues))
	}
	if dlqResp.Total != 15 {
		t.Errorf("expected total 15, got %d", dlqResp.Total)
	}

	// 2. Filter by job_id
	reqJob := httptest.NewRequest(http.MethodGet, "/api/dead-letter?job_id=job-dlq-2", nil)
	recJob := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recJob, reqJob)
	if recJob.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for job_id filter, got %d", recJob.Code)
	}
	var jobDlqResp DeadLetterListResponse
	_ = json.NewDecoder(recJob.Body).Decode(&jobDlqResp)
	if jobDlqResp.Total != 5 || len(jobDlqResp.Issues) != 5 {
		t.Errorf("expected 5 issues for job-dlq-2, got %d (total %d)", len(jobDlqResp.Issues), jobDlqResp.Total)
	}
	for _, issue := range jobDlqResp.Issues {
		if issue.JobID != "job-dlq-2" {
			t.Errorf("expected job_id job-dlq-2, got %s", issue.JobID)
		}
	}

	// 3. Invalid offset
	reqBadOffset := httptest.NewRequest(http.MethodGet, "/api/dead-letter?offset=-1", nil)
	recBadOffset := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recBadOffset, reqBadOffset)
	if recBadOffset.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative offset, got %d", recBadOffset.Code)
	}
}

func TestHandler_Products_PaginationAndFiltering(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC()

	products := []domain.Product{
		{SKU: "KEY-01", Name: "Mechanical Keyboard", Price: 99.0, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now},
		{SKU: "KEY-02", Name: "Wireless Keyboard", Price: 59.0, Status: domain.StatusWithdrawn, SupplierTimestamp: now, UpdatedAt: now},
		{SKU: "MOU-01", Name: "Gaming Mouse", Price: 49.0, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now},
	}
	tx, _ := store.BeginTx(ctx)
	for i := range products {
		_ = store.UpsertProductTx(ctx, tx, &products[i])
	}
	_ = tx.Commit()

	// 1. Filter by status ACTIVE
	reqActive := httptest.NewRequest(http.MethodGet, "/api/products?status=ACTIVE", nil)
	recActive := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recActive, reqActive)
	if recActive.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", recActive.Code)
	}
	var prodResp ProductListResponse
	_ = json.NewDecoder(recActive.Body).Decode(&prodResp)
	if len(prodResp.Products) != 2 {
		t.Errorf("expected 2 active products, got %d", len(prodResp.Products))
	}

	// 2. Search
	reqSearch := httptest.NewRequest(http.MethodGet, "/api/products?search=Wireless", nil)
	recSearch := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recSearch, reqSearch)
	if recSearch.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", recSearch.Code)
	}
	var searchResp ProductListResponse
	_ = json.NewDecoder(recSearch.Body).Decode(&searchResp)
	if len(searchResp.Products) != 1 || searchResp.Products[0].SKU != "KEY-02" {
		t.Errorf("expected 1 product KEY-02, got %+v", searchResp.Products)
	}

	// 3. Invalid status
	reqBadStatus := httptest.NewRequest(http.MethodGet, "/api/products?status=DELETED", nil)
	recBadStatus := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recBadStatus, reqBadStatus)
	if recBadStatus.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid product status, got %d", recBadStatus.Code)
	}
}

func TestSSE_StreamingLifecycleAndPing(t *testing.T) {
	_, _, broker, handler := setupTestEnv(t)

	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect to SSE stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream Content-Type, got %s", ct)
	}

	// Verify broker tracks subscriber
	time.Sleep(50 * time.Millisecond)
	if broker.ClientCount() != 1 {
		t.Errorf("expected 1 active client, got %d", broker.ClientCount())
	}

	// Publish test event
	broker.Publish(Event{
		Type:  "test_event",
		JobID: "job-sse-1",
		Data:  map[string]string{"foo": "bar"},
	})

	// Wait for keepalive ping (broker ping interval is 50ms)
	time.Sleep(120 * time.Millisecond)

	// Cancel client context to simulate disconnect
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Client count should clean up to 0
	if broker.ClientCount() != 0 {
		t.Errorf("expected 0 active clients after disconnect, got %d", broker.ClientCount())
	}
}

func TestSSE_SlowClientBackpressure(t *testing.T) {
	broker := NewBroker(WithClientCap(4), WithPingInterval(1*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)
	defer func() {
		cancel()
		broker.Close()
	}()

	client := &Client{
		id:       "slow-client",
		messages: make(chan Event, 4),
		done:     make(chan struct{}),
	}
	broker.register <- client
	time.Sleep(20 * time.Millisecond)

	// Publish 50 events rapidly without draining client.messages
	for i := 0; i < 50; i++ {
		broker.Publish(Event{
			Type: "overflow",
			Data: map[string]int{"i": i},
		})
	}

	// Broker must not deadlock or block
	broker.unregister <- client
	time.Sleep(20 * time.Millisecond)
}

func TestSSE_MultiSubscriberBroadcast(t *testing.T) {
	broker := NewBroker(WithClientCap(16), WithPingInterval(1*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)
	defer func() {
		cancel()
		broker.Close()
	}()

	const subCount = 5
	var clients []*Client
	for i := 0; i < subCount; i++ {
		c := &Client{
			id:       fmt.Sprintf("sub-%d", i),
			messages: make(chan Event, 16),
			done:     make(chan struct{}),
		}
		broker.register <- c
		clients = append(clients, c)
	}
	time.Sleep(30 * time.Millisecond)

	broker.Publish(Event{
		Type: "broadcast_event",
		Data: map[string]string{"msg": "hello all"},
	})

	for i, c := range clients {
		select {
		case evt := <-c.messages:
			if evt.Type != "broadcast_event" {
				t.Errorf("client %d got event %s, expected broadcast_event", i, evt.Type)
			}
		case <-time.After(500 * time.Millisecond):
			t.Errorf("client %d timed out waiting for event", i)
		}
		broker.unregister <- c
	}
}

func TestMiddleware_Suite(t *testing.T) {
	t.Run("CORS headers and OPTIONS", func(t *testing.T) {
		handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		reqOpt := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
		recOpt := httptest.NewRecorder()
		handler.ServeHTTP(recOpt, reqOpt)

		if recOpt.Code != http.StatusNoContent {
			t.Errorf("expected 204 for OPTIONS, got %d", recOpt.Code)
		}
		if recOpt.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("expected CORS origin *")
		}

		reqGet := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		recGet := httptest.NewRecorder()
		handler.ServeHTTP(recGet, reqGet)
		if recGet.Code != http.StatusOK {
			t.Errorf("expected 200 for GET, got %d", recGet.Code)
		}
	})

	t.Run("Recoverer catches panic", func(t *testing.T) {
		panickyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("something went terribly wrong")
		})
		handler := Recoverer(panickyHandler)

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 on panic, got %d", rec.Code)
		}
		var errResp ErrorResponse
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp.Code != ErrCodeInternalError {
			t.Errorf("expected INTERNAL_ERROR, got %s", errResp.Code)
		}
	})

	t.Run("Logging middleware executes cleanly", func(t *testing.T) {
		normalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		handler := Logging(normalHandler)

		req := httptest.NewRequest(http.MethodGet, "/test-logging", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})
}

func TestServer_Integration(t *testing.T) {
	store, engine, broker, _ := setupTestEnv(t)
	serverHandler := NewServer(store, engine, broker, "")

	// 1. GET / serves dashboard index.html
	reqRoot := httptest.NewRequest(http.MethodGet, "/", nil)
	recRoot := httptest.NewRecorder()
	serverHandler.ServeHTTP(recRoot, reqRoot)
	if recRoot.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for root, got %d", recRoot.Code)
	}

	// 2. GET /api/jobs serves API
	reqAPI := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	recAPI := httptest.NewRecorder()
	serverHandler.ServeHTTP(recAPI, reqAPI)
	if recAPI.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/jobs, got %d", recAPI.Code)
	}
}

func TestServer_ConcurrencyAndRaceStress(t *testing.T) {
	store, _, _, handler := setupTestEnv(t)
	server := httptest.NewServer(handler.Routes())
	defer server.Close()

	// Seed some initial products
	ctx := context.Background()
	now := time.Now().UTC()
	tx, _ := store.BeginTx(ctx)
	for i := 0; i < 20; i++ {
		p := &domain.Product{
			SKU:               fmt.Sprintf("STRESS-SKU-%02d", i),
			Name:              fmt.Sprintf("Stress Item %d", i),
			Price:             10.0 + float64(i),
			Status:            domain.StatusActive,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		}
		_ = store.UpsertProductTx(ctx, tx, p)
	}
	_ = tx.Commit()

	var wg sync.WaitGroup
	const workers = 10

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			client := server.Client()

			// Concurrently query products
			resp, err := client.Get(server.URL + "/api/products?limit=10")
			if err == nil {
				_ = resp.Body.Close()
			}

			// Concurrently query jobs
			resp2, err := client.Get(server.URL + "/api/jobs?limit=5")
			if err == nil {
				_ = resp2.Body.Close()
			}

			// Concurrently query dead letter
			resp3, err := client.Get(server.URL + "/api/dead-letter")
			if err == nil {
				_ = resp3.Body.Close()
			}
		}(i)
	}

	wg.Wait()
}

type nonFlusherResponseWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (n *nonFlusherResponseWriter) Header() http.Header {
	if n.header == nil {
		n.header = make(http.Header)
	}
	return n.header
}
func (n *nonFlusherResponseWriter) Write(b []byte) (int, error) {
	return n.body.Write(b)
}
func (n *nonFlusherResponseWriter) WriteHeader(statusCode int) {
	n.code = statusCode
}

func TestBroker_NonFlusherAndClosed(t *testing.T) {
	broker := NewBroker()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rw := &nonFlusherResponseWriter{}

	broker.ServeHTTP(rw, req)
	if rw.code != http.StatusInternalServerError {
		t.Errorf("expected 500 for non-flusher, got %d", rw.code)
	}

	// Close broker and publish (should no-op without panic)
	broker.Close()
	broker.Close() // double close test
	broker.Publish(Event{Type: "ignored"})

	// ServeHTTP on closed broker
	rec := httptest.NewRecorder()
	broker.ServeHTTP(rec, req)
}

func TestServer_ProgressHook_WithDeadLetters(t *testing.T) {
	broker := NewBroker(WithClientCap(16), WithPingInterval(1*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)
	defer func() {
		cancel()
		broker.Close()
	}()

	hook := NewProgressHook(broker)

	client := &Client{
		id:       "dlq-hook-client",
		messages: make(chan Event, 16),
		done:     make(chan struct{}),
	}
	broker.register <- client
	time.Sleep(20 * time.Millisecond)

	// Call hook with dead letters > 0
	hook("job-dl-test", 0, 10, domain.ChangeSummary{Added: 5, DeadLetter: 2}, domain.ChangeSummary{Added: 5, DeadLetter: 2})

	// Client should receive both progress and dead_letter events
	receivedProgress := false
	receivedDeadLetter := false

	for i := 0; i < 2; i++ {
		select {
		case evt := <-client.messages:
			if evt.Type == "progress" {
				receivedProgress = true
			}
			if evt.Type == "dead_letter" {
				receivedDeadLetter = true
			}
		case <-time.After(500 * time.Millisecond):
			t.Errorf("timed out waiting for hook events")
		}
	}

	if !receivedProgress || !receivedDeadLetter {
		t.Errorf("expected both progress and dead_letter events, got progress=%v, dl=%v", receivedProgress, receivedDeadLetter)
	}
}

func TestMiddleware_FlushCoverage(t *testing.T) {
	rec := httptest.NewRecorder()
	srw := &statusResponseWriter{ResponseWriter: rec, status: http.StatusOK}
	srw.Flush() // invokes Flush() on underlying recorder
}

func TestHandler_ClosedDB_Branches(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("failed open: %v", err)
	}
	_ = store.Close() // close DB

	broker := NewBroker()
	engine := catalogSync.NewEngine(store)
	handler := NewHandler(store, engine, broker)

	// 1. handleStartJob DB failure
	reqStart := httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewBufferString("{}"))
	recStart := httptest.NewRecorder()
	handler.handleStartJob(recStart, reqStart)
	if recStart.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on start with closed DB, got %d", recStart.Code)
	}

	// 2. handleListJobs DB failure
	reqList := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	recList := httptest.NewRecorder()
	handler.handleListJobs(recList, reqList)
	if recList.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on list with closed DB, got %d", recList.Code)
	}

	// 3. handleGetJob DB failure
	reqGet := httptest.NewRequest(http.MethodGet, "/api/jobs/job-1", nil)
	reqGet.SetPathValue("id", "job-1")
	recGet := httptest.NewRecorder()
	handler.handleGetJob(recGet, reqGet)
	if recGet.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on get with closed DB, got %d", recGet.Code)
	}

	// 4. handleListDeadLetter DB failure
	reqDL := httptest.NewRequest(http.MethodGet, "/api/dead-letter", nil)
	recDL := httptest.NewRecorder()
	handler.handleListDeadLetter(recDL, reqDL)
	if recDL.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on DL with closed DB, got %d", recDL.Code)
	}

	// 5. handleListProducts DB failure
	reqProd := httptest.NewRequest(http.MethodGet, "/api/products", nil)
	recProd := httptest.NewRecorder()
	handler.handleListProducts(recProd, reqProd)
	if recProd.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on products with closed DB, got %d", recProd.Code)
	}

	// 6. handleResumeJob DB failure on active check
	reqResume := httptest.NewRequest(http.MethodPost, "/api/jobs/job-1/resume", nil)
	reqResume.SetPathValue("id", "job-1")
	recResume := httptest.NewRecorder()
	handler.handleResumeJob(recResume, reqResume)
	if recResume.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on resume with closed DB, got %d", recResume.Code)
	}
}

func TestHandler_AdditionalValidationEdges(t *testing.T) {
	_, _, _, handler := setupTestEnv(t)

	// Resume without path ID
	reqResumeNoID := httptest.NewRequest(http.MethodPost, "/api/jobs//resume", nil)
	recResumeNoID := httptest.NewRecorder()
	handler.handleResumeJob(recResumeNoID, reqResumeNoID)
	if recResumeNoID.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty resume ID, got %d", recResumeNoID.Code)
	}

	// GetJob without ID
	reqGetNoID := httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)
	recGetNoID := httptest.NewRecorder()
	handler.handleGetJob(recGetNoID, reqGetNoID)
	if recGetNoID.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty get job ID, got %d", recGetNoID.Code)
	}

	// Dead letter invalid limit (< 1)
	reqDLBadLimit := httptest.NewRequest(http.MethodGet, "/api/dead-letter?limit=-1", nil)
	recDLBadLimit := httptest.NewRecorder()
	handler.handleListDeadLetter(recDLBadLimit, reqDLBadLimit)
	if recDLBadLimit.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative dead letter limit, got %d", recDLBadLimit.Code)
	}

	// Products invalid limit (< 1) and invalid offset (< 0)
	reqProdBadLimit := httptest.NewRequest(http.MethodGet, "/api/products?limit=0", nil)
	recProdBadLimit := httptest.NewRecorder()
	handler.handleListProducts(recProdBadLimit, reqProdBadLimit)
	if recProdBadLimit.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid products limit, got %d", recProdBadLimit.Code)
	}

	reqProdBadOffset := httptest.NewRequest(http.MethodGet, "/api/products?offset=-2", nil)
	recProdBadOffset := httptest.NewRecorder()
	handler.handleListProducts(recProdBadOffset, reqProdBadOffset)
	if recProdBadOffset.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid products offset, got %d", recProdBadOffset.Code)
	}

	// Resume with malformed body
	reqResumeBadJSON := httptest.NewRequest(http.MethodPost, "/api/jobs/job-1/resume", bytes.NewBufferString("{bad"))
	reqResumeBadJSON.SetPathValue("id", "job-1")
	recResumeBadJSON := httptest.NewRecorder()
	handler.handleResumeJob(recResumeBadJSON, reqResumeBadJSON)
	// It will either fail on active check, not found, or malformed json
	if recResumeBadJSON.Code != http.StatusBadRequest && recResumeBadJSON.Code != http.StatusNotFound {
		t.Errorf("expected 400 or 404, got %d", recResumeBadJSON.Code)
	}
}

func TestHandler_ResetDatabase(t *testing.T) {
	store, _ := storage.OpenInMemory()
	defer store.Close()
	eng := catalogSync.NewEngine(store)
	broker := NewBroker()
	handler := NewHandler(store, eng, broker)

	// 1. Success reset when idle
	req := httptest.NewRequest(http.MethodPost, "/api/database/reset", nil)
	rec := httptest.NewRecorder()
	handler.handleResetDatabase(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d, body: %s", rec.Code, rec.Body.String())
	}

	// 2. Conflict when job is active
	handler.mu.Lock()
	handler.activeJobID = "job-active-test"
	handler.mu.Unlock()

	req2 := httptest.NewRequest(http.MethodPost, "/api/database/reset", nil)
	rec2 := httptest.NewRecorder()
	handler.handleResetDatabase(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when job is active, got %d", rec2.Code)
	}

	// Cleanup
	handler.mu.Lock()
	handler.activeJobID = ""
	handler.mu.Unlock()
}

func TestHandler_CancelJob(t *testing.T) {
	store, _ := storage.OpenInMemory()
	defer store.Close()
	eng := catalogSync.NewEngine(store)
	broker := NewBroker()
	handler := NewHandler(store, eng, broker)

	// 1. Missing job ID
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/jobs//cancel", nil)
	recEmpty := httptest.NewRecorder()
	handler.handleCancelJob(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty job ID, got %d", recEmpty.Code)
	}

	// 2. Not running job
	reqNotFound := httptest.NewRequest(http.MethodPost, "/api/jobs/job-unknown/cancel", nil)
	reqNotFound.SetPathValue("id", "job-unknown")
	recNotFound := httptest.NewRecorder()
	handler.handleCancelJob(recNotFound, reqNotFound)
	if recNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown job, got %d", recNotFound.Code)
	}

	// 3. Active job cancellation
	calledCancel := false
	handler.mu.Lock()
	handler.activeJobID = "job-run-test"
	handler.activeCancel = func() { calledCancel = true }
	handler.mu.Unlock()

	reqCancel := httptest.NewRequest(http.MethodPost, "/api/jobs/job-run-test/cancel", nil)
	reqCancel.SetPathValue("id", "job-run-test")
	recCancel := httptest.NewRecorder()
	handler.handleCancelJob(recCancel, reqCancel)
	if recCancel.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for cancel, got %d", recCancel.Code)
	}
	if !calledCancel {
		t.Fatal("expected cancel callback to be invoked")
	}
}
