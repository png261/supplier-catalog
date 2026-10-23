package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

func setupMultiTenantTest(t *testing.T) (http.Handler, string, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "coderpush-mt-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	mainDBPath := filepath.Join(tempDir, "main.db")
	store, err := storage.Open(mainDBPath)
	if err != nil {
		t.Fatalf("failed to open main storage: %v", err)
	}

	broker := NewBroker(WithPingInterval(100 * time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	engine := catalogSync.NewEngine(store, catalogSync.WithProgressHook(NewProgressHook(broker)))

	server := NewServer(store, engine, broker, "", WithDataDir(tempDir))

	cleanup := func() {
		cancel()
		broker.Close()
		_ = store.Close()
		_ = os.RemoveAll(tempDir)
	}

	return server, tempDir, cleanup
}

func TestMultiTenantDataIsolation(t *testing.T) {
	server, _, cleanup := setupMultiTenantTest(t)
	defer cleanup()

	// 1. Post a batch of products to Shop A ("shop-nike")
	jobA := JobStartRequest{
		JobID: "job-nike-1",
		Mode:  "batches",
		Batches: []BatchInput{
			{
				BatchIndex: 0,
				Records: []BatchInputRecord{
					{
						SKU:       "NIKE-AIR-1",
						Name:      "Nike Air Max",
						Price:     150.0,
						Status:    "ACTIVE",
						Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
					},
				},
			},
		},
	}
	bodyA, _ := json.Marshal(jobA)
	reqA := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(string(bodyA)))
	reqA.Header.Set("Content-Type", "application/json")
	reqA.Header.Set("X-Shop-Id", "shop-nike")
	recA := httptest.NewRecorder()
	server.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for shop-nike, got %d: %s", recA.Code, recA.Body.String())
	}

	// 2. Post a batch of products to Shop B ("shop-adidas")
	jobB := JobStartRequest{
		JobID: "job-adidas-1",
		Mode:  "batches",
		Batches: []BatchInput{
			{
				BatchIndex: 0,
				Records: []BatchInputRecord{
					{
						SKU:       "ADIDAS-UB-1",
						Name:      "Adidas Ultraboost",
						Price:     180.0,
						Status:    "ACTIVE",
						Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
					},
				},
			},
		},
	}
	bodyB, _ := json.Marshal(jobB)
	reqB := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(string(bodyB)))
	reqB.Header.Set("Content-Type", "application/json")
	reqB.Header.Set("X-Shop-Id", "shop-adidas")
	recB := httptest.NewRecorder()
	server.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for shop-adidas, got %d: %s", recB.Code, recB.Body.String())
	}

	// Wait for background jobs to commit
	time.Sleep(200 * time.Millisecond)

	// 3. Query products from Shop A
	reqListA := httptest.NewRequest("GET", "/api/products", nil)
	reqListA.Header.Set("X-Shop-Id", "shop-nike")
	recListA := httptest.NewRecorder()
	server.ServeHTTP(recListA, reqListA)
	if recListA.Code != http.StatusOK {
		t.Fatalf("expected 200 for shop-nike products, got %d", recListA.Code)
	}
	var resA ProductListResponse
	_ = json.Unmarshal(recListA.Body.Bytes(), &resA)
	if resA.Total != 1 || resA.Products[0].SKU != "NIKE-AIR-1" {
		t.Fatalf("shop-nike should have exactly 1 product NIKE-AIR-1, got %+v", resA.Products)
	}

	// 4. Query products from Shop B
	reqListB := httptest.NewRequest("GET", "/api/products", nil)
	reqListB.Header.Set("X-Shop-Id", "shop-adidas")
	recListB := httptest.NewRecorder()
	server.ServeHTTP(recListB, reqListB)
	if recListB.Code != http.StatusOK {
		t.Fatalf("expected 200 for shop-adidas products, got %d", recListB.Code)
	}
	var resB ProductListResponse
	_ = json.Unmarshal(recListB.Body.Bytes(), &resB)
	if resB.Total != 1 || resB.Products[0].SKU != "ADIDAS-UB-1" {
		t.Fatalf("shop-adidas should have exactly 1 product ADIDAS-UB-1, got %+v", resB.Products)
	}

	// 5. Query default shop (should have 0 products)
	reqListDef := httptest.NewRequest("GET", "/api/products", nil)
	recListDef := httptest.NewRecorder()
	server.ServeHTTP(recListDef, reqListDef)
	var resDef ProductListResponse
	_ = json.Unmarshal(recListDef.Body.Bytes(), &resDef)
	if resDef.Total != 0 {
		t.Fatalf("default shop should be empty, got total=%d", resDef.Total)
	}
}

func TestMultiTenantResetIsolation(t *testing.T) {
	server, _, cleanup := setupMultiTenantTest(t)
	defer cleanup()

	// Seed shop A and shop B
	for _, shop := range []struct {
		id  string
		sku string
	}{
		{"shop-a", "SKU-A"},
		{"shop-b", "SKU-B"},
	} {
		job := JobStartRequest{
			JobID: "job-" + shop.id,
			Mode:  "batches",
			Batches: []BatchInput{
				{
					BatchIndex: 0,
					Records: []BatchInputRecord{
						{
							SKU:       shop.sku,
							Name:      "Item " + shop.sku,
							Price:     50.0,
							Status:    "ACTIVE",
							Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
						},
					},
				},
			},
		}
		b, _ := json.Marshal(job)
		req := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Shop-Id", shop.id)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
	}

	time.Sleep(200 * time.Millisecond)

	// Reset Shop A only
	reqReset := httptest.NewRequest("POST", "/api/database/reset", nil)
	reqReset.Header.Set("X-Shop-Id", "shop-a")
	recReset := httptest.NewRecorder()
	server.ServeHTTP(recReset, reqReset)
	if recReset.Code != http.StatusOK {
		t.Fatalf("expected 200 for reset, got %d: %s", recReset.Code, recReset.Body.String())
	}

	// Verify Shop A is empty
	reqA := httptest.NewRequest("GET", "/api/products", nil)
	reqA.Header.Set("X-Shop-Id", "shop-a")
	recA := httptest.NewRecorder()
	server.ServeHTTP(recA, reqA)
	var resA ProductListResponse
	_ = json.Unmarshal(recA.Body.Bytes(), &resA)
	if resA.Total != 0 {
		t.Fatalf("expected Shop A to have 0 products after reset, got %d", resA.Total)
	}

	// Verify Shop B is STILL intact!
	reqB := httptest.NewRequest("GET", "/api/products", nil)
	reqB.Header.Set("X-Shop-Id", "shop-b")
	recB := httptest.NewRecorder()
	server.ServeHTTP(recB, reqB)
	var resB ProductListResponse
	_ = json.Unmarshal(recB.Body.Bytes(), &resB)
	if resB.Total != 1 || resB.Products[0].SKU != "SKU-B" {
		t.Fatalf("expected Shop B to retain its product SKU-B, got %+v", resB.Products)
	}
}

func TestMultiTenantShopIDValidation(t *testing.T) {
	server, _, cleanup := setupMultiTenantTest(t)
	defer cleanup()

	invalidIDs := []string{
		"../traversal",
		"shop/subpath",
		"shop$special*",
		"shop name with spaces",
		strings.Repeat("a", 65), // exceeds 64 chars
	}

	for _, badID := range invalidIDs {
		req := httptest.NewRequest("GET", "/api/products", nil)
		req.Header.Set("X-Shop-Id", badID)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for invalid shop ID %q, got %d", badID, rec.Code)
		}
	}
}

func TestMultiTenantConcurrentJobs(t *testing.T) {
	server, _, cleanup := setupMultiTenantTest(t)
	defer cleanup()

	// Both shops run generator jobs simultaneously
	totalBatches := 5
	batchSize := 20

	jobA := JobStartRequest{
		JobID:        "concurrent-job-a",
		Mode:         "generator",
		TotalBatches: &totalBatches,
		BatchSize:    &batchSize,
	}
	jobB := JobStartRequest{
		JobID:        "concurrent-job-b",
		Mode:         "generator",
		TotalBatches: &totalBatches,
		BatchSize:    &batchSize,
	}

	bodyA, _ := json.Marshal(jobA)
	bodyB, _ := json.Marshal(jobB)

	reqA := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(string(bodyA)))
	reqA.Header.Set("Content-Type", "application/json")
	reqA.Header.Set("X-Shop-Id", "shop-concurrent-a")

	reqB := httptest.NewRequest("POST", "/api/jobs", strings.NewReader(string(bodyB)))
	reqB.Header.Set("Content-Type", "application/json")
	reqB.Header.Set("X-Shop-Id", "shop-concurrent-b")

	recA := httptest.NewRecorder()
	recB := httptest.NewRecorder()

	server.ServeHTTP(recA, reqA)
	server.ServeHTTP(recB, reqB)

	if recA.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for shop A, got %d: %s", recA.Code, recA.Body.String())
	}
	if recB.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for shop B, got %d: %s", recB.Code, recB.Body.String())
	}

	// Wait for jobs to finish
	time.Sleep(300 * time.Millisecond)

	// Verify both completed
	reqListA := httptest.NewRequest("GET", "/api/jobs", nil)
	reqListA.Header.Set("X-Shop-Id", "shop-concurrent-a")
	recListA := httptest.NewRecorder()
	server.ServeHTTP(recListA, reqListA)

	reqListB := httptest.NewRequest("GET", "/api/jobs", nil)
	reqListB.Header.Set("X-Shop-Id", "shop-concurrent-b")
	recListB := httptest.NewRecorder()
	server.ServeHTTP(recListB, reqListB)

	var listA, listB JobListResponse
	_ = json.Unmarshal(recListA.Body.Bytes(), &listA)
	_ = json.Unmarshal(recListB.Body.Bytes(), &listB)

	if listA.Total != 1 || listA.Jobs[0].JobID != "concurrent-job-a" {
		t.Errorf("shop A expected job concurrent-job-a, got %+v", listA.Jobs)
	}
	if listB.Total != 1 || listB.Jobs[0].JobID != "concurrent-job-b" {
		t.Errorf("shop B expected job concurrent-job-b, got %+v", listB.Jobs)
	}
}

func TestMultiTenantCleanup(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "coderpush-cleanup-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, _ := storage.Open(filepath.Join(tempDir, "main.db"))
	defer store.Close()
	broker := NewBroker()
	defer broker.Close()
	eng := catalogSync.NewEngine(store)

	handler := NewHandler(store, eng, broker, WithHandlerDataDir(tempDir))
	tm := handler.tenantManager
	tm.maxIdleAge = 10 * time.Millisecond // very short for test

	tenant, err := tm.GetOrCreate("shop-expiring")
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	dbFile := filepath.Join(tempDir, "shops", "shop-expiring.db")
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("expected DB file %s to exist", dbFile)
	}

	// Manually age the tenant and file
	tenant.SetLastAccess(time.Now().Add(-2 * time.Hour))
	past := time.Now().Add(-10 * time.Hour)
	_ = os.Chtimes(dbFile, past, past)

	// PruneStaleTenants immediately evicts idle tenant and deletes DB files
	tm.PruneStaleTenants()

	if _, err := os.Stat(dbFile); !os.IsNotExist(err) {
		t.Errorf("expected DB file to be pruned, but stat returned err=%v", err)
	}
}

func TestMultiTenantConcurrentGetOrCreateRace(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "coderpush-race-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, _ := storage.Open(filepath.Join(tempDir, "main.db"))
	defer store.Close()
	broker := NewBroker()
	defer broker.Close()
	eng := catalogSync.NewEngine(store)

	handler := NewHandler(store, eng, broker, WithHandlerDataDir(tempDir))
	tm := handler.tenantManager

	const numGoroutines = 50
	const iterationsPerGoroutine = 100
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			for j := 0; j < iterationsPerGoroutine; j++ {
				tenant, err := tm.GetOrCreate("shared-race-shop")
				if err != nil {
					t.Errorf("GetOrCreate error: %v", err)
					return
				}
				if tenant == nil {
					t.Errorf("expected tenant not nil")
					return
				}
				_ = tenant.GetLastAccess()
			}
		}()
	}

	close(startBarrier)
	wg.Wait()
}
