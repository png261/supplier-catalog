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

func TestE2E_SyncJobVersioningDiffAndRollback(t *testing.T) {
	ts := createE2EServer(t)
	client := &http.Client{}

	// 1. Run Sync Job 1 (Baseline v1)
	job1Payload := `{
		"mode": "batches",
		"job_id": "job-sync-v1",
		"batches": [{
			"batch_index": 0,
			"records": [
				{"sku": "SKU-V1", "name": "Desk Lamp V1", "price": 50.0, "status": "ACTIVE", "timestamp": "2026-01-01T10:00:00Z"},
				{"sku": "SKU-V2", "name": "Desk Mat V1", "price": 25.0, "status": "ACTIVE", "timestamp": "2026-01-01T10:00:00Z"}
			]
		}]
	}`
	res1, err := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewBufferString(job1Payload))
	if err != nil || res1.StatusCode != http.StatusAccepted {
		t.Fatalf("failed starting job-sync-v1: %v", err)
	}
	res1.Body.Close()
	time.Sleep(150 * time.Millisecond)

	// 2. Run Sync Job 2 (Price update + Withdraw SKU-V2 + Add SKU-V3)
	job2Payload := `{
		"mode": "batches",
		"job_id": "job-sync-v2",
		"batches": [{
			"batch_index": 0,
			"records": [
				{"sku": "SKU-V1", "name": "Desk Lamp V2 Pro", "price": 75.0, "status": "ACTIVE", "timestamp": "2026-01-02T10:00:00Z"},
				{"sku": "SKU-V2", "name": "Desk Mat V1", "price": 25.0, "status": "WITHDRAWN", "timestamp": "2026-01-02T10:00:00Z"},
				{"sku": "SKU-V3", "name": "Monitor Arm V2", "price": 99.0, "status": "ACTIVE", "timestamp": "2026-01-02T10:00:00Z"}
			]
		}]
	}`
	res2, err := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewBufferString(job2Payload))
	if err != nil || res2.StatusCode != http.StatusAccepted {
		t.Fatalf("failed starting job-sync-v2: %v", err)
	}
	res2.Body.Close()
	time.Sleep(150 * time.Millisecond)

	// 3. Verify GET /api/versions has 2 SYNC versions
	verResp, err := http.Get(ts.URL + "/api/versions")
	if err != nil {
		t.Fatalf("failed to list versions: %v", err)
	}
	defer verResp.Body.Close()
	var verList api.VersionListResponse
	if err := json.NewDecoder(verResp.Body).Decode(&verList); err != nil {
		t.Fatalf("failed decoding versions: %v", err)
	}
	if verList.Total != 2 || len(verList.Versions) != 2 {
		t.Fatalf("expected 2 SYNC versions, got total=%d len=%d", verList.Total, len(verList.Versions))
	}
	if verList.Versions[0].Type != domain.VersionTypeSync || verList.Versions[1].Type != domain.VersionTypeSync {
		t.Errorf("expected version type SYNC, got %s and %s", verList.Versions[0].Type, verList.Versions[1].Type)
	}

	// 4. Verify GET /api/versions/job-sync-v2/diff returns 3 deltas
	diffResp, err := http.Get(ts.URL + "/api/versions/job-sync-v2/diff")
	if err != nil {
		t.Fatalf("failed getting diff: %v", err)
	}
	defer diffResp.Body.Close()
	var deltas []domain.ItemDelta
	if err := json.NewDecoder(diffResp.Body).Decode(&deltas); err != nil {
		t.Fatalf("failed decoding deltas: %v", err)
	}
	if len(deltas) != 3 {
		t.Fatalf("expected 3 deltas for job-sync-v2, got %d", len(deltas))
	}

	// 5. Rollback to job-sync-v1 using "operator" field (UI payload format)
	rollReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/versions/job-sync-v1/rollback", bytes.NewBufferString(`{"operator":"qa-playwright-bot","description":"Revert v2"}`))
	rollReq.Header.Set("Content-Type", "application/json")
	rollResp, err := client.Do(rollReq)
	if err != nil {
		t.Fatalf("rollback request failed: %v", err)
	}
	defer rollResp.Body.Close()
	if rollResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(rollResp.Body)
		t.Fatalf("expected 200 OK on rollback, got %d: %s", rollResp.StatusCode, body)
	}
	var rollData api.RollbackResponse
	_ = json.NewDecoder(rollResp.Body).Decode(&rollData)
	if rollData.Version.Author != "qa-playwright-bot" {
		t.Errorf("expected rollback author 'qa-playwright-bot', got %q", rollData.Version.Author)
	}
	if rollData.Version.Type != domain.VersionTypeRollback {
		t.Errorf("expected ROLLBACK version type, got %s", rollData.Version.Type)
	}

	// 6. Verify Tombstone Preservation & Baseline Restoration in live catalog
	prodResp, _ := http.Get(ts.URL + "/api/products?limit=20")
	var prodList api.ProductListResponse
	_ = json.NewDecoder(prodResp.Body).Decode(&prodList)
	prodResp.Body.Close()

	bySKU := make(map[string]domain.Product)
	for _, p := range prodList.Products {
		bySKU[p.SKU] = p
	}
	if bySKU["SKU-V1"].Price != 50.0 || bySKU["SKU-V1"].Name != "Desk Lamp V1" {
		t.Errorf("expected SKU-V1 reverted to 50.0 / Desk Lamp V1, got %+v", bySKU["SKU-V1"])
	}
	if bySKU["SKU-V2"].Status != domain.StatusWithdrawn {
		t.Errorf("expected Tombstone Preservation on SKU-V2 (remain WITHDRAWN), got %s", bySKU["SKU-V2"].Status)
	}
	if bySKU["SKU-V3"].Status != domain.StatusWithdrawn {
		t.Errorf("expected post-baseline SKU-V3 to be WITHDRAWN after rollback, got %s", bySKU["SKU-V3"].Status)
	}
}

func TestE2E_DeadLetterReasonCodeFilteringAndCORS(t *testing.T) {
	ts := createE2EServer(t)
	client := &http.Client{}

	// 1. Verify CORS preflight allows X-Shop-Id
	optReq, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/jobs", nil)
	optResp, err := client.Do(optReq)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	optResp.Body.Close()
	allowHeaders := optResp.Header.Get("Access-Control-Allow-Headers")
	if !bytes.Contains([]byte(allowHeaders), []byte("X-Shop-Id")) {
		t.Errorf("expected Access-Control-Allow-Headers to contain X-Shop-Id, got %q", allowHeaders)
	}

	// 2. Ingest batch with all 4 defect categories
	batchPayload := `{
		"mode": "batches",
		"job_id": "job-dlq-reasons",
		"batches": [{
			"batch_index": 0,
			"records": [
				{"sku": "", "name": "Missing SKU Item", "price": 19.99, "status": "ACTIVE", "timestamp": "2026-01-01T10:00:00Z"},
				{"sku": "SKU-NEG", "name": "Negative Price Item", "price": -5.00, "status": "ACTIVE", "timestamp": "2026-01-01T10:00:00Z"},
				{"sku": "SKU-BAD-STATUS", "name": "Invalid Status Item", "price": 15.00, "status": "BROKEN_STATUS", "timestamp": "2026-01-01T10:00:00Z"},
				{"sku": "SKU-ZERO-TS", "name": "Zero Timestamp Item", "price": 25.00, "status": "ACTIVE", "timestamp": "0001-01-01T00:00:00Z"}
			]
		}]
	}`
	res, err := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewBufferString(batchPayload))
	if err != nil || res.StatusCode != http.StatusAccepted {
		t.Fatalf("failed starting job-dlq-reasons: %v", err)
	}
	res.Body.Close()
	time.Sleep(150 * time.Millisecond)

	// 3. Test each UI reason filter code
	for _, code := range []string{"EMPTY_SKU", "NEGATIVE_OR_ZERO_PRICE", "CORRUPTED_JSON", "ZERO_OR_MISSING_TIMESTAMP"} {
		resp, err := http.Get(ts.URL + "/api/dead-letter?reason=" + code)
		if err != nil {
			t.Fatalf("GET /api/dead-letter?reason=%s failed: %v", code, err)
		}
		var list api.DeadLetterListResponse
		_ = json.NewDecoder(resp.Body).Decode(&list)
		resp.Body.Close()
		if list.Total != 1 || len(list.Issues) != 1 {
			t.Errorf("expected 1 issue for reason=%s, got total=%d len=%d", code, list.Total, len(list.Issues))
		}
	}

	// 4. Test generator with base_time advancement across consecutive runs
	gen1 := `{"mode":"generator","job_id":"gen-basetime-1","seed":42,"total_batches":2,"batch_size":10,"base_time":"2026-02-01T00:00:00Z"}`
	rGen1, _ := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewBufferString(gen1))
	rGen1.Body.Close()
	time.Sleep(150 * time.Millisecond)

	// Second generator run with earlier base_time should auto-advance past max_ts so updates are applied
	gen2 := `{"mode":"generator","job_id":"gen-basetime-2","seed":42,"total_batches":2,"batch_size":10,"base_time":"2026-01-01T00:00:00Z","update_ratio":0.5}`
	rGen2, _ := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewBufferString(gen2))
	rGen2.Body.Close()
	time.Sleep(150 * time.Millisecond)

	job2Resp, _ := http.Get(ts.URL + "/api/jobs/gen-basetime-2")
	var job2Detail api.JobDetailResponse
	_ = json.NewDecoder(job2Resp.Body).Decode(&job2Detail)
	job2Resp.Body.Close()
	if job2Detail.Job == nil || job2Detail.Job.Status != domain.JobStatusCompleted {
		t.Fatalf("expected gen-basetime-2 completed, got %+v", job2Detail.Job)
	}
	if job2Detail.Job.Summary.Added+job2Detail.Job.Summary.Updated+job2Detail.Job.Summary.Withdrawn == 0 {
		t.Errorf("expected gen-basetime-2 to apply changes with advanced base_time, got summary %+v", job2Detail.Job.Summary)
	}
}
