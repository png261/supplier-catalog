package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"coderpush/pkg/api"
	"coderpush/pkg/domain"
)

func TestE2E_FullJobLifecycleAndQuery(t *testing.T) {
	ts := createE2EServer(t)

	// 1. Reset Database
	resetResp, err := http.Post(ts.URL+"/api/database/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to reset database: %v", err)
	}
	if resetResp.StatusCode != http.StatusOK {
		t.Fatalf("expected reset 200, got %d", resetResp.StatusCode)
	}
	resetResp.Body.Close()

	// 2. Start a direct batch sync job
	batchPayload := api.JobStartRequest{
		Mode:  "batches",
		JobID: "job-e2e-01",
		Batches: []api.BatchInput{
			{
				BatchIndex: 0,
				Records: []api.BatchInputRecord{
					{
						SKU:       "SKU-E2E-1",
						Name:      "Wireless Ergonomic Mouse",
						Price:     49.99,
						Status:    domain.StatusActive,
						Timestamp: time.Now().UTC().Format(time.RFC3339),
					},
					{
						SKU:       "SKU-E2E-2",
						Name:      "Mechanical Keyboard",
						Price:     129.99,
						Status:    domain.StatusActive,
						Timestamp: time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	payloadBytes, _ := json.Marshal(batchPayload)
	startResp, err := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewReader(payloadBytes))
	if err != nil {
		t.Fatalf("failed to start job: %v", err)
	}
	if startResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(startResp.Body)
		t.Fatalf("expected 202 Accepted, got %d: %s", startResp.StatusCode, body)
	}
	var jobRes api.SyncJobResponse
	_ = json.NewDecoder(startResp.Body).Decode(&jobRes)
	startResp.Body.Close()

	if jobRes.JobID != "job-e2e-01" {
		t.Errorf("expected job_id job-e2e-01, got %s", jobRes.JobID)
	}

	// 3. Poll until job completes
	completed := false
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		getResp, err := http.Get(ts.URL + "/api/jobs/job-e2e-01")
		if err != nil {
			continue
		}
		var detail api.JobDetailResponse
		_ = json.NewDecoder(getResp.Body).Decode(&detail)
		getResp.Body.Close()

		if detail.Job != nil && detail.Job.Status == domain.JobStatusCompleted {
			completed = true
			break
		}
	}
	if !completed {
		t.Fatalf("job did not complete within timeout")
	}

	// 4. Query products
	prodResp, err := http.Get(ts.URL + "/api/products?search=Keyboard")
	if err != nil {
		t.Fatalf("failed to list products: %v", err)
	}
	defer prodResp.Body.Close()
	if prodResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", prodResp.StatusCode)
	}
	var prodList api.ProductListResponse
	_ = json.NewDecoder(prodResp.Body).Decode(&prodList)
	if prodList.Total != 1 || len(prodList.Products) != 1 {
		t.Errorf("expected 1 product matching Keyboard, got %d", prodList.Total)
	} else if prodList.Products[0].SKU != "SKU-E2E-2" {
		t.Errorf("expected SKU-E2E-2, got %s", prodList.Products[0].SKU)
	}

	// 5. Query metrics
	metricsResp, err := http.Get(ts.URL + "/api/metrics?time_range=all")
	if err != nil {
		t.Fatalf("failed to get metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", metricsResp.StatusCode)
	}
	var met api.MetricsResponse
	_ = json.NewDecoder(metricsResp.Body).Decode(&met)
	if met.Metrics.TotalProcessed != 2 {
		t.Errorf("expected total processed 2, got %d", met.Metrics.TotalProcessed)
	}
}

func TestE2E_DeadLetterQuarantineAndResolve(t *testing.T) {
	ts := createE2EServer(t)

	// Ingest a batch with corrupted record (negative price)
	batchPayload := api.JobStartRequest{
		Mode:  "batches",
		JobID: "job-dlq-01",
		Batches: []api.BatchInput{
			{
				BatchIndex: 0,
				Records: []api.BatchInputRecord{
					{
						SKU:       "BAD-SKU-99",
						Name:      "Corrupted Monitor",
						Price:     -10.0,
						Status:    domain.StatusActive,
						Timestamp: time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	b, _ := json.Marshal(batchPayload)
	res, err := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("start job failed: %v", err)
	}
	res.Body.Close()

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// List dead-letter records
	dlqResp, err := http.Get(ts.URL + "/api/dead-letter")
	if err != nil {
		t.Fatalf("failed to list dead-letter: %v", err)
	}
	defer dlqResp.Body.Close()
	var dlqList api.DeadLetterListResponse
	_ = json.NewDecoder(dlqResp.Body).Decode(&dlqList)

	if dlqList.Total == 0 || len(dlqList.Issues) == 0 {
		t.Fatalf("expected quarantined dead letter record, got 0")
	}
	issueID := dlqList.Issues[0].IssueID

	// Resolve the record
	resolvePayload := api.ResolveDeadLetterRequest{
		SKU:       "BAD-SKU-99",
		Name:      "Fixed Monitor 4K",
		Price:     299.99,
		Status:    "ACTIVE",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	rb, _ := json.Marshal(resolvePayload)
	resolveResp, err := http.Post(fmt.Sprintf("%s/api/dead-letter/%s/resolve", ts.URL, issueID), "application/json", bytes.NewReader(rb))
	if err != nil {
		t.Fatalf("failed to resolve dead-letter: %v", err)
	}
	defer resolveResp.Body.Close()
	if resolveResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resolveResp.StatusCode)
	}

	var resolved api.ResolveDeadLetterResponse
	_ = json.NewDecoder(resolveResp.Body).Decode(&resolved)
	if resolved.Status != "success" || resolved.Product.SKU != "BAD-SKU-99" {
		t.Errorf("unexpected resolution response: %+v", resolved)
	}
}

func TestE2E_MultiTenantIsolation(t *testing.T) {
	ts := createE2EServer(t)

	client := &http.Client{}

	// Post product to shop-alpha
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/jobs", bytes.NewReader([]byte(`{
		"mode": "batches",
		"job_id": "job-alpha",
		"batches": [{
			"batch_index": 0,
			"records": [{"sku": "ALPHA-1", "name": "Alpha Item", "price": 10.0, "status": "ACTIVE", "timestamp": "2026-10-22T10:00:00Z"}]
		}]
	}`)))
	req.Header.Set("X-Shop-Id", "shop-alpha")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed shop-alpha request: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	// Query shop-alpha
	reqAlpha, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/products", nil)
	reqAlpha.Header.Set("X-Shop-Id", "shop-alpha")
	respAlpha, err := client.Do(reqAlpha)
	if err != nil {
		t.Fatalf("failed shop-alpha query: %v", err)
	}
	defer respAlpha.Body.Close()
	var listAlpha api.ProductListResponse
	_ = json.NewDecoder(respAlpha.Body).Decode(&listAlpha)
	if listAlpha.Total != 1 {
		t.Errorf("expected 1 product in shop-alpha, got %d", listAlpha.Total)
	}

	// Query shop-beta (should be isolated and empty)
	reqBeta, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/products", nil)
	reqBeta.Header.Set("X-Shop-Id", "shop-beta")
	respBeta, err := client.Do(reqBeta)
	if err != nil {
		t.Fatalf("failed shop-beta query: %v", err)
	}
	defer respBeta.Body.Close()
	var listBeta api.ProductListResponse
	_ = json.NewDecoder(respBeta.Body).Decode(&listBeta)
	if listBeta.Total != 0 {
		t.Errorf("expected 0 products in shop-beta, got %d", listBeta.Total)
	}
}
