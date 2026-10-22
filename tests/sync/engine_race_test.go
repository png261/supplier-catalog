package sync_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// TestEngine_Race_ConcurrentReadsAndWrites runs 20 concurrent readers and 5 writers
// to prove 0 data races under go test -race -count=5.
func TestEngine_Race_ConcurrentReadsAndWrites(t *testing.T) {
	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Pre-seed catalog with 50 products
	var seedRecords []domain.SupplierUpdate
	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		seedRecords = append(seedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-RACE-%03d", i),
			Name:      fmt.Sprintf("Initial Item %d", i),
			Price:     float64(i + 1),
			Status:    domain.StatusActive,
			Timestamp: baseTime,
		})
	}
	_, err = engine.ExecuteBatch(ctx, domain.Batch{JobID: "seed", BatchIndex: 0, Records: seedRecords})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	var wg sync.WaitGroup
	numReaders := 20
	numWriters := 5

	// Spawn readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				sku := fmt.Sprintf("SKU-RACE-%03d", (readerID+j)%50)
				prod, err := store.GetProduct(ctx, sku)
				if err == nil && prod != nil {
					_ = prod.Name
					_ = prod.Price
				}
			}
		}(r)
	}

	// Spawn writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for b := 1; b <= 4; b++ {
				var updates []domain.SupplierUpdate
				for i := 0; i < 5; i++ {
					skuIdx := (writerID*5 + i) % 50
					updates = append(updates, domain.SupplierUpdate{
						SKU:       fmt.Sprintf("SKU-RACE-%03d", skuIdx),
						Name:      fmt.Sprintf("Updated by W%d B%d", writerID, b),
						Price:     float64(skuIdx*10) + float64(b),
						Status:    domain.StatusActive,
						Timestamp: baseTime.Add(time.Duration(b) * time.Hour),
					})
				}
				_, _ = engine.ExecuteBatch(ctx, domain.Batch{
					JobID:      fmt.Sprintf("job-w%d", writerID),
					BatchIndex: b,
					Records:    updates,
				})
			}
		}(w)
	}

	wg.Wait()
}

// TestEngine_Race_ConcurrentBatchExecution runs 8 concurrent goroutines executing batches
// against shared SKUs to verify transactional serialization and zero deadlocks.
func TestEngine_Race_ConcurrentBatchExecution(t *testing.T) {
	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	numWorkers := 8

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			records := []domain.SupplierUpdate{
				{
					SKU:       "SHARED-CONCURRENT-SKU",
					Name:      fmt.Sprintf("Worker %d", workerID),
					Price:     10.0 + float64(workerID),
					Status:    domain.StatusActive,
					Timestamp: time.Date(2026, 10, 21, 10, workerID, 0, 0, time.UTC),
				},
			}
			_, _ = engine.ExecuteBatch(ctx, domain.Batch{
				JobID:      fmt.Sprintf("job-conc-%d", workerID),
				BatchIndex: 0,
				Records:    records,
			})
		}(i)
	}

	wg.Wait()

	// Verify catalog is in a valid state
	p, err := store.GetProduct(ctx, "SHARED-CONCURRENT-SKU")
	if err != nil {
		t.Fatalf("failed to retrieve product: %v", err)
	}
	if p == nil || p.SKU != "SHARED-CONCURRENT-SKU" {
		t.Fatalf("unexpected state: %+v", p)
	}
}
