package sync_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coderpush/pkg/domain"
	catalogSync "coderpush/pkg/sync"
)

// ============================================================================
// PART 1: INVARIANT C (TOMBSTONE IMMUTABILITY) ADVERSARIAL CHALLENGES
// ============================================================================

// TestAdversarial_InvariantC_DistantFutureResurrections attempts repeated aggressive
// resurrections of a WITHDRAWN product with timestamps ranging from seconds to centuries
// in the future (up to year 9999), varying prices, names, and payloads.
func TestAdversarial_InvariantC_DistantFutureResurrections(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tBase := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	sku := "SKU-TOMB-AGGRESSIVE"

	// Step 1: Pre-seed item as WITHDRAWN
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID:      "seed-withdrawn",
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{
				SKU:       sku,
				Name:      "Original Decommissioned Item",
				Price:     19.99,
				Status:    domain.StatusWithdrawn,
				Timestamp: tBase,
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to seed withdrawn item: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("expected Added=1 for initial withdrawn item; got %+v", res)
	}

	// Step 2: 50 aggressive resurrection attempts with future timestamps up to year 9999
	futureDates := []time.Time{
		tBase.Add(1 * time.Second),
		tBase.Add(1 * time.Minute),
		tBase.Add(1 * time.Hour),
		tBase.Add(24 * time.Hour),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2030, 6, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2050, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2099, 12, 31, 23, 59, 59, 999999999, time.UTC),
		time.Date(2500, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
	}

	// Add more generated timestamps
	for y := 2028; y <= 2065; y++ {
		futureDates = append(futureDates, time.Date(y, time.Month((y%12)+1), (y%28)+1, y%24, y%60, 0, 0, time.UTC))
	}

	for idx, futureTime := range futureDates {
		price := float64(idx+1) * 100.50
		res, err := engine.ExecuteBatch(ctx, domain.Batch{
			JobID:      fmt.Sprintf("resurrect-attempt-%d", idx),
			BatchIndex: idx + 1,
			Records: []domain.SupplierUpdate{
				{
					SKU:       sku,
					Name:      fmt.Sprintf("Reborn Phoenix Attempt %d", idx),
					Price:     price,
					Status:    domain.StatusActive, // Illegal resurrection attempt
					Timestamp: futureTime,
				},
			},
		})
		if err != nil {
			t.Fatalf("resurrection batch %d returned unexpected error: %v", idx, err)
		}

		// Must be counted as skipped: zero adds, zero updates, zero withdrawals
		if res.Skipped != 1 || res.Added != 0 || res.Updated != 0 || res.Withdrawn != 0 {
			t.Errorf("iteration %d: expected Skipped=1 on resurrection attempt at %v; got %+v", idx, futureTime, res)
		}

		// Invariant C Check: Database state must remain WITHDRAWN, uncorrupted
		p, err := store.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("iteration %d: failed to fetch product: %v", idx, err)
		}
		if p.Status != domain.StatusWithdrawn {
			t.Fatalf("INVARIANT C VIOLATION! Product %s flipped from WITHDRAWN to %s at iteration %d (timestamp %v)", sku, p.Status, idx, futureTime)
		}
		if p.Name != "Original Decommissioned Item" || p.Price != 19.99 {
			t.Fatalf("INVARIANT C VIOLATION! Metadata mutated during blocked resurrection: name=%s, price=%f", p.Name, p.Price)
		}
		if !p.SupplierTimestamp.Equal(tBase) {
			t.Fatalf("INVARIANT C VIOLATION! SupplierTimestamp advanced on blocked resurrection: got %v, want %v", p.SupplierTimestamp, tBase)
		}
	}
}

// TestAdversarial_InvariantC_IntraBatchResurrectionBarrage tests a single atomic batch
// containing 500 interleaved records for the same SKU, switching rapidly between
// ACTIVE and WITHDRAWN with escalating timestamps.
func TestAdversarial_InvariantC_IntraBatchResurrectionBarrage(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tBase := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	sku := "SKU-BARRAGE-001"

	const numRecords = 500
	var records []domain.SupplierUpdate

	// Record 0: Born WITHDRAWN
	records = append(records, domain.SupplierUpdate{
		SKU:       sku,
		Name:      "Born As Withdrawn",
		Price:     10.0,
		Status:    domain.StatusWithdrawn,
		Timestamp: tBase,
	})

	expectedSkipped := 0
	expectedUpdated := 0

	var lastValidPrice float64 = 10.0
	var lastValidName string = "Born As Withdrawn"
	var lastValidTimestamp time.Time = tBase

	for i := 1; i < numRecords; i++ {
		recordTime := tBase.Add(time.Duration(i) * time.Minute)
		if i%2 == 1 {
			// Odd: Attempt resurrection to ACTIVE -> MUST BE DROPPED
			records = append(records, domain.SupplierUpdate{
				SKU:       sku,
				Name:      fmt.Sprintf("Phoenix Attempt %d", i),
				Price:     1000.0 + float64(i),
				Status:    domain.StatusActive,
				Timestamp: recordTime,
			})
			expectedSkipped++
		} else {
			// Even: Valid metadata update on WITHDRAWN item -> MUST BE APPLIED
			lastValidPrice = 20.0 + float64(i)
			lastValidName = fmt.Sprintf("Withdrawn Update %d", i)
			lastValidTimestamp = recordTime
			records = append(records, domain.SupplierUpdate{
				SKU:       sku,
				Name:      lastValidName,
				Price:     lastValidPrice,
				Status:    domain.StatusWithdrawn,
				Timestamp: recordTime,
			})
			expectedUpdated++
		}
	}

	summary, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID:      "job-barrage",
		BatchIndex: 0,
		Records:    records,
	})
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}

	// Record 0 is Added=1.
	// Even records are Updated.
	// Odd records are Skipped.
	if summary.Added != 1 {
		t.Errorf("expected Added=1; got %d", summary.Added)
	}
	if summary.Updated != expectedUpdated {
		t.Errorf("expected Updated=%d; got %d", expectedUpdated, summary.Updated)
	}
	if summary.Skipped != expectedSkipped {
		t.Errorf("expected Skipped=%d; got %d", expectedSkipped, summary.Skipped)
	}
	if summary.Withdrawn != 0 {
		t.Errorf("expected Withdrawn=0; got %d", summary.Withdrawn)
	}

	// Verify final product in database
	p, err := store.GetProduct(ctx, sku)
	if err != nil {
		t.Fatalf("failed to query product: %v", err)
	}
	if p.Status != domain.StatusWithdrawn {
		t.Fatalf("INVARIANT C VIOLATION: final product status is %s, expected WITHDRAWN", p.Status)
	}
	if p.Price != lastValidPrice {
		t.Errorf("expected final price %f, got %f", lastValidPrice, p.Price)
	}
	if p.Name != lastValidName {
		t.Errorf("expected final name %q, got %q", lastValidName, p.Name)
	}
	if !p.SupplierTimestamp.Equal(lastValidTimestamp) {
		t.Errorf("expected final timestamp %v, got %v", lastValidTimestamp, p.SupplierTimestamp)
	}
}

// TestAdversarial_InvariantC_ComplexInterleavedLifecycle simulates a real-world messy
// lifecycle across 10 sequential batches:
// Creation (ACTIVE) -> Updates -> Withdrawal -> Multiple Resurrection attempts ->
// Permitted Metadata Updates -> Stale Updates -> Identical Replays.
func TestAdversarial_InvariantC_ComplexInterleavedLifecycle(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	t4 := time.Date(2026, 10, 21, 13, 0, 0, 0, time.UTC)
	t5 := time.Date(2026, 10, 21, 14, 0, 0, 0, time.UTC)
	t6 := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	tOld := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	tFuture := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

	sku := "SKU-LIFECYCLE-101"

	type testStep struct {
		name            string
		batch           domain.Batch
		expectedSummary domain.ChangeSummary
		expectedStatus  domain.ProductStatus
		expectedPrice   float64
	}

	steps := []testStep{
		{
			name: "1. Create initial ACTIVE product",
			batch: domain.Batch{
				JobID: "b1", BatchIndex: 1,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Initial", Price: 100.0, Status: domain.StatusActive, Timestamp: t1},
				},
			},
			expectedSummary: domain.ChangeSummary{Added: 1},
			expectedStatus:  domain.StatusActive,
			expectedPrice:   100.0,
		},
		{
			name: "2. Legitimate update while ACTIVE",
			batch: domain.Batch{
				JobID: "b2", BatchIndex: 2,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Rev 2", Price: 120.0, Status: domain.StatusActive, Timestamp: t2},
				},
			},
			expectedSummary: domain.ChangeSummary{Updated: 1},
			expectedStatus:  domain.StatusActive,
			expectedPrice:   120.0,
		},
		{
			name: "3. Legitimate transition to WITHDRAWN",
			batch: domain.Batch{
				JobID: "b3", BatchIndex: 3,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Rev 2", Price: 120.0, Status: domain.StatusWithdrawn, Timestamp: t3},
				},
			},
			expectedSummary: domain.ChangeSummary{Withdrawn: 1},
			expectedStatus:  domain.StatusWithdrawn,
			expectedPrice:   120.0,
		},
		{
			name: "4. Forbidden resurrection attempt to ACTIVE with advancing timestamp",
			batch: domain.Batch{
				JobID: "b4", BatchIndex: 4,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Reopened", Price: 200.0, Status: domain.StatusActive, Timestamp: t4},
				},
			},
			expectedSummary: domain.ChangeSummary{Skipped: 1},
			expectedStatus:  domain.StatusWithdrawn,
			expectedPrice:   120.0,
		},
		{
			name: "5. Permitted metadata update on WITHDRAWN item",
			batch: domain.Batch{
				JobID: "b5", BatchIndex: 5,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Clearance Final", Price: 50.0, Status: domain.StatusWithdrawn, Timestamp: t5},
				},
			},
			expectedSummary: domain.ChangeSummary{Updated: 1},
			expectedStatus:  domain.StatusWithdrawn,
			expectedPrice:   50.0,
		},
		{
			name: "6. Forbidden resurrection attempt with extreme future timestamp (Year 2099)",
			batch: domain.Batch{
				JobID: "b6", BatchIndex: 6,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Year 2099", Price: 5000.0, Status: domain.StatusActive, Timestamp: tFuture},
				},
			},
			expectedSummary: domain.ChangeSummary{Skipped: 1},
			expectedStatus:  domain.StatusWithdrawn,
			expectedPrice:   50.0,
		},
		{
			name: "7. Out-of-order resurrection attempt with timestamp older than original creation",
			batch: domain.Batch{
				JobID: "b7", BatchIndex: 7,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Past", Price: 10.0, Status: domain.StatusActive, Timestamp: tOld},
				},
			},
			expectedSummary: domain.ChangeSummary{Skipped: 1},
			expectedStatus:  domain.StatusWithdrawn,
			expectedPrice:   50.0,
		},
		{
			name: "8. Out-of-order WITHDRAWN update with older timestamp (Invariant B)",
			batch: domain.Batch{
				JobID: "b8", BatchIndex: 8,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Past Withdrawn", Price: 15.0, Status: domain.StatusWithdrawn, Timestamp: tOld},
				},
			},
			expectedSummary: domain.ChangeSummary{Skipped: 1},
			expectedStatus:  domain.StatusWithdrawn,
			expectedPrice:   50.0,
		},
		{
			name: "9. Identical replay of batch 5 (Invariant A)",
			batch: domain.Batch{
				JobID: "b9", BatchIndex: 9,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Clearance Final", Price: 50.0, Status: domain.StatusWithdrawn, Timestamp: t5},
				},
			},
			expectedSummary: domain.ChangeSummary{Skipped: 1},
			expectedStatus:  domain.StatusWithdrawn,
			expectedPrice:   50.0,
		},
		{
			name: "10. Permitted further metadata update while remaining WITHDRAWN",
			batch: domain.Batch{
				JobID: "b10", BatchIndex: 10,
				Records: []domain.SupplierUpdate{
					{SKU: sku, Name: "Item Archived Permanently", Price: 0.0, Status: domain.StatusWithdrawn, Timestamp: t6},
				},
			},
			expectedSummary: domain.ChangeSummary{Updated: 1},
			expectedStatus:  domain.StatusWithdrawn,
			expectedPrice:   0.0,
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			res, err := engine.ExecuteBatch(ctx, step.batch)
			if err != nil {
				t.Fatalf("ExecuteBatch failed: %v", err)
			}
			if res != step.expectedSummary {
				t.Fatalf("summary mismatch: got %+v, want %+v", res, step.expectedSummary)
			}
			p, err := store.GetProduct(ctx, sku)
			if err != nil {
				t.Fatalf("GetProduct failed: %v", err)
			}
			if p.Status != step.expectedStatus {
				t.Fatalf("status mismatch: got %s, want %s", p.Status, step.expectedStatus)
			}
			if p.Price != step.expectedPrice {
				t.Fatalf("price mismatch: got %f, want %f", p.Price, step.expectedPrice)
			}
		})
	}
}

// TestAdversarial_InvariantC_MultiSKUMatrix tests 100 SKUs (50 ACTIVE, 50 WITHDRAWN).
// A subsequent batch attempts to transition all 100 to ACTIVE with future timestamps.
// The 50 ACTIVE items must update, while all 50 WITHDRAWN items must be blocked without cross-talk.
func TestAdversarial_InvariantC_MultiSKUMatrix(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)

	const totalSKUs = 100
	var batch1Records []domain.SupplierUpdate

	for i := 0; i < totalSKUs; i++ {
		sku := fmt.Sprintf("SKU-MATRIX-%03d", i)
		status := domain.StatusActive
		if i%2 == 1 {
			status = domain.StatusWithdrawn
		}
		batch1Records = append(batch1Records, domain.SupplierUpdate{
			SKU:       sku,
			Name:      fmt.Sprintf("Item %d", i),
			Price:     float64(i + 1),
			Status:    status,
			Timestamp: t1,
		})
	}

	res1, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID: "matrix-seed", BatchIndex: 0, Records: batch1Records,
	})
	if err != nil {
		t.Fatalf("batch 1 failed: %v", err)
	}
	if res1.Added != totalSKUs {
		t.Fatalf("expected 100 Added; got %+v", res1)
	}

	// Batch 2: Update ALL 100 SKUs to StatusActive at t2
	var batch2Records []domain.SupplierUpdate
	for i := 0; i < totalSKUs; i++ {
		sku := fmt.Sprintf("SKU-MATRIX-%03d", i)
		batch2Records = append(batch2Records, domain.SupplierUpdate{
			SKU:       sku,
			Name:      fmt.Sprintf("Item Updated %d", i),
			Price:     float64((i + 1) * 10),
			Status:    domain.StatusActive, // 50 are legitimate updates, 50 are illegal resurrections
			Timestamp: t2,
		})
	}

	res2, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID: "matrix-update-all-active", BatchIndex: 1, Records: batch2Records,
	})
	if err != nil {
		t.Fatalf("batch 2 failed: %v", err)
	}

	// 50 ACTIVE items -> Updated
	// 50 WITHDRAWN items -> Skipped
	if res2.Updated != 50 {
		t.Errorf("expected 50 Updated; got %d", res2.Updated)
	}
	if res2.Skipped != 50 {
		t.Errorf("expected 50 Skipped; got %d", res2.Skipped)
	}

	// Verify catalog state for all 100 items
	for i := 0; i < totalSKUs; i++ {
		sku := fmt.Sprintf("SKU-MATRIX-%03d", i)
		p, err := store.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("failed to get product %s: %v", sku, err)
		}
		if i%2 == 1 {
			// Odd items: MUST REMAIN WITHDRAWN, unmutated by batch 2
			if p.Status != domain.StatusWithdrawn {
				t.Fatalf("INVARIANT C VIOLATION: SKU %s resurrected to %s!", sku, p.Status)
			}
			if p.Price != float64(i+1) {
				t.Fatalf("INVARIANT C VIOLATION: SKU %s price changed to %f (expected %f)", sku, p.Price, float64(i+1))
			}
			if !p.SupplierTimestamp.Equal(t1) {
				t.Fatalf("INVARIANT C VIOLATION: SKU %s timestamp changed to %v (expected %v)", sku, p.SupplierTimestamp, t1)
			}
		} else {
			// Even items: MUST BE ACTIVE with new price
			if p.Status != domain.StatusActive {
				t.Fatalf("SKU %s status unexpected: got %s, want ACTIVE", sku, p.Status)
			}
			if p.Price != float64((i+1)*10) {
				t.Fatalf("SKU %s price unexpected: got %f, want %f", sku, p.Price, float64((i+1)*10))
			}
			if !p.SupplierTimestamp.Equal(t2) {
				t.Fatalf("SKU %s timestamp unexpected: got %v, want %v", sku, p.SupplierTimestamp, t2)
			}
		}
	}
}

// ============================================================================
// PART 2: CONCURRENCY & THREAD-SAFETY ADVERSARIAL STRESS CHALLENGES
// ============================================================================

// TestAdversarial_Concurrency_HeavyReadWriteContention launches 30 concurrent readers
// and 10 concurrent writers executing continuously across 100 SKUs.
func TestAdversarial_Concurrency_HeavyReadWriteContention(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const numSKUs = 100
	tBase := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Pre-seed catalog with 100 items
	var seedRecords []domain.SupplierUpdate
	for i := 0; i < numSKUs; i++ {
		seedRecords = append(seedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("CONC-SKU-%03d", i),
			Name:      fmt.Sprintf("Item %d", i),
			Price:     float64(i + 1),
			Status:    domain.StatusActive,
			Timestamp: tBase,
		})
	}
	_, err := engine.ExecuteBatch(ctx, domain.Batch{JobID: "seed", BatchIndex: 0, Records: seedRecords})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	var wg sync.WaitGroup
	var readErrors atomic.Int64
	var writeErrors atomic.Int64
	var totalReads atomic.Int64
	var totalWrites atomic.Int64

	const numReaders = 30
	const numWriters = 10
	const iterationsPerWorker = 25

	// Start readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for it := 0; it < iterationsPerWorker; it++ {
				skuIdx := (readerID*7 + it) % numSKUs
				sku := fmt.Sprintf("CONC-SKU-%03d", skuIdx)
				p, err := store.GetProduct(ctx, sku)
				if err != nil {
					readErrors.Add(1)
				} else if p != nil {
					_ = p.Name
					_ = p.Price
					_ = p.Status
				}
				totalReads.Add(1)
			}
		}(r)
	}

	// Start writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for it := 1; it <= iterationsPerWorker; it++ {
				var batchRecords []domain.SupplierUpdate
				for k := 0; k < 5; k++ {
					skuIdx := (writerID*10 + k + it) % numSKUs
					batchRecords = append(batchRecords, domain.SupplierUpdate{
						SKU:       fmt.Sprintf("CONC-SKU-%03d", skuIdx),
						Name:      fmt.Sprintf("Writer %d Iter %d", writerID, it),
						Price:     float64(skuIdx*10) + float64(it),
						Status:    domain.StatusActive,
						Timestamp: tBase.Add(time.Duration(writerID*100+it) * time.Second),
					})
				}
				_, err := engine.ExecuteBatch(ctx, domain.Batch{
					JobID:      fmt.Sprintf("job-w%d", writerID),
					BatchIndex: it,
					Records:    batchRecords,
				})
				if err != nil {
					writeErrors.Add(1)
				}
				totalWrites.Add(1)
			}
		}(w)
	}

	wg.Wait()

	if readErrors.Load() > 0 {
		t.Fatalf("encountered %d read errors during concurrency stress test", readErrors.Load())
	}
	if writeErrors.Load() > 0 {
		t.Fatalf("encountered %d write errors during concurrency stress test", writeErrors.Load())
	}
	if totalReads.Load() != numReaders*iterationsPerWorker {
		t.Errorf("read count mismatch: %d", totalReads.Load())
	}
	if totalWrites.Load() != numWriters*iterationsPerWorker {
		t.Errorf("write count mismatch: %d", totalWrites.Load())
	}
}

// TestAdversarial_Concurrency_ConcurrentResurrectionRace coordinates 20 concurrent
// goroutines attacking the SAME SKU simultaneously behind a barrier:
// 15 goroutines attempt to force an ACTIVE status, while 5 goroutines attempt to force WITHDRAWN.
// At the end, once WITHDRAWN is registered, it must remain strictly WITHDRAWN.
func TestAdversarial_Concurrency_ConcurrentResurrectionRace(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sku := "RACE-TOMB-TARGET"
	tBase := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Pre-seed product as WITHDRAWN at T0
	_, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID: "seed", BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "Target Tombstone", Price: 50.0, Status: domain.StatusWithdrawn, Timestamp: tBase},
		},
	})
	if err != nil {
		t.Fatalf("failed to seed tombstone: %v", err)
	}

	const numWorkers = 20
	barrier := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-barrier // Release all goroutines simultaneously

			status := domain.StatusActive
			if workerID%4 == 0 {
				status = domain.StatusWithdrawn
			}

			// Assign diverse timestamps in the future
			workerTime := tBase.Add(time.Duration(workerID+1) * time.Hour)

			_, _ = engine.ExecuteBatch(ctx, domain.Batch{
				JobID:      fmt.Sprintf("job-race-%d", workerID),
				BatchIndex: workerID,
				Records: []domain.SupplierUpdate{
					{
						SKU:       sku,
						Name:      fmt.Sprintf("Payload by Worker %d", workerID),
						Price:     float64(workerID * 10),
						Status:    status,
						Timestamp: workerTime,
					},
				},
			})
		}(i)
	}

	close(barrier) // Unleash race
	wg.Wait()

	// Invariant C Check: Under extreme concurrency, status must STILL be WITHDRAWN!
	p, err := store.GetProduct(ctx, sku)
	if err != nil {
		t.Fatalf("GetProduct failed: %v", err)
	}
	if p.Status != domain.StatusWithdrawn {
		t.Fatalf("CONCURRENCY INVARIANT C FAILURE: Target product flipped to %s under concurrent race!", p.Status)
	}
}

// TestAdversarial_Concurrency_MultiEngineSharedStorage runs 4 distinct SyncEngine instances
// concurrently accessing the same SQLiteStorage handle to verify driver-level serialization.
func TestAdversarial_Concurrency_MultiEngineSharedStorage(t *testing.T) {
	store := setupTestStorage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const numEngines = 4
	engines := make([]*catalogSync.SyncEngine, numEngines)
	for i := 0; i < numEngines; i++ {
		engines[i] = catalogSync.NewEngine(store)
	}

	var wg sync.WaitGroup
	tBase := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	for i := 0; i < numEngines; i++ {
		wg.Add(1)
		go func(engineID int) {
			defer wg.Done()
			e := engines[engineID]
			for b := 0; b < 10; b++ {
				sku := fmt.Sprintf("MULTI-ENG-SKU-%d", engineID)
				_, err := e.ExecuteBatch(ctx, domain.Batch{
					JobID:      fmt.Sprintf("job-eng-%d", engineID),
					BatchIndex: b,
					Records: []domain.SupplierUpdate{
						{
							SKU:       sku,
							Name:      fmt.Sprintf("Engine %d Batch %d", engineID, b),
							Price:     float64(b + 1),
							Status:    domain.StatusActive,
							Timestamp: tBase.Add(time.Duration(b) * time.Minute),
						},
					},
				})
				if err != nil {
					t.Errorf("engine %d batch %d failed: %v", engineID, b, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all 4 SKUs are present and intact
	for i := 0; i < numEngines; i++ {
		sku := fmt.Sprintf("MULTI-ENG-SKU-%d", i)
		p, err := store.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("failed to retrieve product %s: %v", sku, err)
		}
		if p.Status != domain.StatusActive {
			t.Errorf("unexpected status for %s: %s", sku, p.Status)
		}
	}
}

// TestAdversarial_Concurrency_ContextCancellationStress verifies that context cancellations
// (both pre-cancelled and mid-batch cancellations) cleanly abort the transaction,
// release the engine mutex, and leave the storage uncorrupted and ready for subsequent transactions.
func TestAdversarial_Concurrency_ContextCancellationStress(t *testing.T) {
	store := setupTestStorage(t)
	tBase := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Scenario 1: Repeated pre-cancelled context executions
	engine := catalogSync.NewEngine(store)
	for i := 0; i < 20; i++ {
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before invocation

		batch := domain.Batch{
			JobID:      fmt.Sprintf("pre-cancel-%d", i),
			BatchIndex: i,
			Records: []domain.SupplierUpdate{
				{SKU: fmt.Sprintf("PRE-CANCEL-%02d", i), Name: "Pre Canceled", Price: 10.0, Status: domain.StatusActive, Timestamp: tBase},
			},
		}
		_, err := engine.ExecuteBatch(canceledCtx, batch)
		if err == nil {
			t.Errorf("iteration %d: expected cancellation error, got nil", i)
		}
	}

	// Scenario 2: Context cancelled during batch iteration
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		count := 0
		iterEngine := catalogSync.NewEngine(store, catalogSync.WithTimeFunc(func() time.Time {
			count++
			if count == 1 {
				cancel() // Trigger cancellation on first time access during record processing
			}
			return tBase
		}))

		batch := domain.Batch{
			JobID:      fmt.Sprintf("mid-cancel-%d", i),
			BatchIndex: i,
			Records: []domain.SupplierUpdate{
				{SKU: fmt.Sprintf("MID-CANCEL-%02d-A", i), Name: "Mid Cancel A", Price: 10.0, Status: domain.StatusActive, Timestamp: tBase},
				{SKU: fmt.Sprintf("MID-CANCEL-%02d-B", i), Name: "Mid Cancel B", Price: 20.0, Status: domain.StatusActive, Timestamp: tBase.Add(time.Minute)},
			},
		}

		_, err := iterEngine.ExecuteBatch(ctx, batch)
		if err == nil {
			t.Errorf("iteration %d: expected mid-batch cancellation error, got nil", i)
		}

		// Verify neither record was committed (atomic transaction rollback)
		pA, _ := store.GetProduct(context.Background(), fmt.Sprintf("MID-CANCEL-%02d-A", i))
		pB, _ := store.GetProduct(context.Background(), fmt.Sprintf("MID-CANCEL-%02d-B", i))
		if pA != nil || pB != nil {
			t.Fatalf("iteration %d: atomic rollback violated on cancellation! pA=%+v, pB=%+v", i, pA, pB)
		}
	}

	// Scenario 3: Post-stress sanity check - Engine must remain fully operational
	validCtx := context.Background()
	res, err := engine.ExecuteBatch(validCtx, domain.Batch{
		JobID:      "post-cancel-healthy",
		BatchIndex: 999,
		Records: []domain.SupplierUpdate{
			{SKU: "SANITY-AFTER-CANCEL", Name: "Healthy Item", Price: 99.0, Status: domain.StatusActive, Timestamp: tBase.Add(10 * time.Hour)},
		},
	})
	if err != nil {
		t.Fatalf("engine failed after cancellation stress: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("expected healthy batch to add 1; got %+v", res)
	}

	p, err := store.GetProduct(validCtx, "SANITY-AFTER-CANCEL")
	if err != nil || p == nil {
		t.Fatalf("sanity product not retrieved: %v", err)
	}
}
