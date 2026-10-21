package sync_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

func setupTestStorage(t *testing.T) *storage.SQLiteStorage {
	t.Helper()
	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to create in-memory storage: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

// TestEngine_InvariantA_BatchReplay verifies that ingesting the identical batch twice
// produces 0 adds, 0 updates, 0 withdrawals, and N skipped with unchanged catalog state.
func TestEngine_InvariantA_BatchReplay(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	batch := domain.Batch{
		JobID:      "job-replay-1",
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-001", Name: "Widget A", Price: 10.50, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-002", Name: "Widget B", Price: 20.00, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-003", Name: "Widget C", Price: 30.25, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-004", Name: "Widget D", Price: 40.00, Status: domain.StatusWithdrawn, Timestamp: baseTime},
			{SKU: "SKU-005", Name: "Widget E", Price: 50.75, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}

	// 1st Run: Expect all records added
	res1, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	expected1 := domain.ChangeSummary{Added: 5, Updated: 0, Withdrawn: 0, Skipped: 0, DeadLetter: 0}
	if res1 != expected1 {
		t.Errorf("run 1 mismatch: got %+v, want %+v", res1, expected1)
	}

	// Snapshot state of product 1
	p1Before, err := store.GetProduct(ctx, "SKU-001")
	if err != nil || p1Before == nil {
		t.Fatalf("failed to retrieve SKU-001: %v", err)
	}

	// 2nd Run: Replay exact same batch
	res2, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	expected2 := domain.ChangeSummary{Added: 0, Updated: 0, Withdrawn: 0, Skipped: 5, DeadLetter: 0}
	if res2 != expected2 {
		t.Errorf("run 2 mismatch: got %+v, want %+v", res2, expected2)
	}

	// Verify catalog state is unchanged
	p1After, err := store.GetProduct(ctx, "SKU-001")
	if err != nil || p1After == nil {
		t.Fatalf("failed to retrieve SKU-001 after replay: %v", err)
	}
	if p1Before.Price != p1After.Price || p1Before.Name != p1After.Name || !p1Before.SupplierTimestamp.Equal(p1After.SupplierTimestamp) {
		t.Errorf("product drifted on replay: before %+v, after %+v", p1Before, p1After)
	}
}

// TestEngine_InvariantA_PartialOverlap tests batch containing both new and duplicate items.
func TestEngine_InvariantA_PartialOverlap(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	batch1 := domain.Batch{
		JobID:      "job-overlap-1",
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-1", Name: "Item 1", Price: 10.0, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-2", Name: "Item 2", Price: 20.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}
	_, err := engine.ExecuteBatch(ctx, batch1)
	if err != nil {
		t.Fatalf("batch 1 failed: %v", err)
	}

	// Batch 2: SKU-2 is identical, SKU-3 is new
	batch2 := domain.Batch{
		JobID:      "job-overlap-2",
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-2", Name: "Item 2", Price: 20.0, Status: domain.StatusActive, Timestamp: baseTime},
			{SKU: "SKU-3", Name: "Item 3", Price: 30.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}
	res2, err := engine.ExecuteBatch(ctx, batch2)
	if err != nil {
		t.Fatalf("batch 2 failed: %v", err)
	}
	expected2 := domain.ChangeSummary{Added: 1, Updated: 0, Withdrawn: 0, Skipped: 1, DeadLetter: 0}
	if res2 != expected2 {
		t.Errorf("batch 2 mismatch: got %+v, want %+v", res2, expected2)
	}
}

// TestEngine_InvariantB_NewerTimestamp tests applying update with newer timestamp.
func TestEngine_InvariantB_NewerTimestamp(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)

	// Seed item
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B1", Name: "Old Name", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})

	// Update with T2 > T1
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B1", Name: "New Name", Price: 15.0, Status: domain.StatusActive, Timestamp: t2},
		},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if res.Updated != 1 || res.Skipped != 0 {
		t.Errorf("expected Updated=1, Skipped=0; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-B1")
	if p.Name != "New Name" || p.Price != 15.0 || !p.SupplierTimestamp.Equal(t2) {
		t.Errorf("catalog not updated properly: %+v", p)
	}
}

// TestEngine_InvariantB_OlderTimestamp tests that out-of-order older records are rejected and skipped.
func TestEngine_InvariantB_OlderTimestamp(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tOld := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	tCurrent := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Seed item with tCurrent
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B2", Name: "Current Name", Price: 100.0, Status: domain.StatusActive, Timestamp: tCurrent},
		},
	})

	// Ingest older record (tOld < tCurrent)
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B2", Name: "Stale Name", Price: 50.0, Status: domain.StatusActive, Timestamp: tOld},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("expected Skipped=1, Updated=0; got %+v", res)
	}

	// Catalog must retain current values
	p, _ := store.GetProduct(ctx, "SKU-B2")
	if p.Name != "Current Name" || p.Price != 100.0 || !p.SupplierTimestamp.Equal(tCurrent) {
		t.Errorf("catalog state corrupted by stale update: %+v", p)
	}
}

// TestEngine_InvariantB_IdenticalTimestamp tests that non-advancing equal timestamp with different payload is skipped.
func TestEngine_InvariantB_IdenticalTimestamp(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tSame := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Seed
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B3", Name: "Original", Price: 10.0, Status: domain.StatusActive, Timestamp: tSame},
		},
	})

	// Same timestamp, different price
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-B3", Name: "Changed Price", Price: 20.0, Status: domain.StatusActive, Timestamp: tSame},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("expected Skipped=1, Updated=0; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-B3")
	if p.Price != 10.0 {
		t.Errorf("price should not have changed: got %f", p.Price)
	}
}

// TestEngine_InvariantB_TimezoneEquivalence verifies UTC normalization for different timezone offsets.
func TestEngine_InvariantB_TimezoneEquivalence(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tUTC := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	locBKK := time.FixedZone("UTC+7", 7*3600)
	tBKK := time.Date(2026, 10, 21, 15, 0, 0, 0, locBKK) // Exactly 08:00:00 UTC

	// Seed with UTC
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TZ", Name: "Item TZ", Price: 10.0, Status: domain.StatusActive, Timestamp: tUTC},
		},
	})

	// Replay with +07:00 equivalent
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TZ", Name: "Item TZ", Price: 10.0, Status: domain.StatusActive, Timestamp: tBKK},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("expected Skipped=1 due to timezone equivalence; got %+v", res)
	}
}

// TestEngine_InvariantB_MicrosecondPrecision verifies sub-millisecond precision comparison.
func TestEngine_InvariantB_MicrosecondPrecision(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 1000, time.UTC) // 1 microsecond
	t2 := time.Date(2026, 10, 21, 10, 0, 0, 2000, time.UTC) // 2 microseconds

	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-MICRO", Name: "V1", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})

	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-MICRO", Name: "V2", Price: 12.0, Status: domain.StatusActive, Timestamp: t2},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Updated != 1 {
		t.Errorf("expected Updated=1 with microsecond advance; got %+v", res)
	}
}

// TestEngine_InvariantC_ActiveToWithdrawn verifies valid transition from ACTIVE to WITHDRAWN.
func TestEngine_InvariantC_ActiveToWithdrawn(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)

	// Seed ACTIVE
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-C1", Name: "Active Item", Price: 25.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})

	// Transition to WITHDRAWN
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-C1", Name: "Active Item", Price: 25.0, Status: domain.StatusWithdrawn, Timestamp: t2},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Withdrawn != 1 || res.Updated != 0 || res.Skipped != 0 {
		t.Errorf("expected Withdrawn=1; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-C1")
	if p.Status != domain.StatusWithdrawn {
		t.Errorf("expected status WITHDRAWN, got %s", p.Status)
	}
}

// TestEngine_InvariantC_ResurrectionBlocked verifies tombstone preservation (WITHDRAWN never restored to ACTIVE).
func TestEngine_InvariantC_ResurrectionBlocked(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	tFuture := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	// Seed WITHDRAWN
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TOMB", Name: "Discontinued", Price: 10.0, Status: domain.StatusWithdrawn, Timestamp: t1},
		},
	})

	// Attempt resurrection to ACTIVE with future timestamp
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TOMB", Name: "Reactivated Attempt", Price: 99.0, Status: domain.StatusActive, Timestamp: tFuture},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 || res.Withdrawn != 0 {
		t.Errorf("expected resurrection to be Skipped=1; got %+v", res)
	}

	// Status must remain WITHDRAWN
	p, _ := store.GetProduct(ctx, "SKU-TOMB")
	if p.Status != domain.StatusWithdrawn {
		t.Fatalf("tombstone violated: product became %s", p.Status)
	}
	if p.Name != "Discontinued" || p.Price != 10.0 {
		t.Errorf("metadata modified on forbidden resurrection: %+v", p)
	}
}

// TestEngine_InvariantC_TombstoneMetadataUpdate tests updating price/metadata on a WITHDRAWN item.
func TestEngine_InvariantC_TombstoneMetadataUpdate(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)

	// Seed WITHDRAWN
	_, _ = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TOMB-UPDATE", Name: "Discontinued", Price: 10.0, Status: domain.StatusWithdrawn, Timestamp: t1},
		},
	})

	// Update metadata with status remaining WITHDRAWN
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-TOMB-UPDATE", Name: "Discontinued Rev2", Price: 8.0, Status: domain.StatusWithdrawn, Timestamp: t2},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Updated != 1 || res.Skipped != 0 {
		t.Errorf("expected Updated=1 on tombstone metadata change; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-TOMB-UPDATE")
	if p.Price != 8.0 || p.Name != "Discontinued Rev2" || p.Status != domain.StatusWithdrawn {
		t.Errorf("unexpected updated product: %+v", p)
	}
}

// TestEngine_InvariantC_InitialWithdrawn verifies direct ingestion of a new withdrawn product.
func TestEngine_InvariantC_InitialWithdrawn(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-NEW-WITHDRAWN", Name: "Born Withdrawn", Price: 15.0, Status: domain.StatusWithdrawn, Timestamp: t1},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 {
		t.Errorf("expected Added=1; got %+v", res)
	}

	p, err := store.GetProduct(ctx, "SKU-NEW-WITHDRAWN")
	if err != nil || p == nil {
		t.Fatalf("product not found: %v", err)
	}
	if p.Status != domain.StatusWithdrawn {
		t.Errorf("expected status WITHDRAWN, got %s", p.Status)
	}
}

// TestEngine_IntraBatch_Duplicates tests multiple identical records in the same batch.
func TestEngine_IntraBatch_Duplicates(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	batch := domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-INTRA-1", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: "SKU-INTRA-1", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: "SKU-INTRA-1", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
		},
	}
	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 || res.Skipped != 2 {
		t.Errorf("expected Added=1, Skipped=2; got %+v", res)
	}
}

// TestEngine_IntraBatch_AscendingOrder tests multiple progressive updates in the same batch.
func TestEngine_IntraBatch_AscendingOrder(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 10, 5, 0, 0, time.UTC)
	t3 := time.Date(2026, 10, 21, 10, 10, 0, 0, time.UTC)

	batch := domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-INTRA-2", Name: "V1", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: "SKU-INTRA-2", Name: "V2", Price: 12.0, Status: domain.StatusActive, Timestamp: t2},
			{SKU: "SKU-INTRA-2", Name: "V3", Price: 15.0, Status: domain.StatusActive, Timestamp: t3},
		},
	}
	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 || res.Updated != 2 {
		t.Errorf("expected Added=1, Updated=2; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-INTRA-2")
	if p.Name != "V3" || p.Price != 15.0 || !p.SupplierTimestamp.Equal(t3) {
		t.Errorf("final state incorrect: %+v", p)
	}
}

// TestEngine_IntraBatch_DescendingOrder tests multiple out-of-order records in the same batch.
func TestEngine_IntraBatch_DescendingOrder(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 10, 5, 0, 0, time.UTC)

	// First record has T2, second has T1 < T2
	batch := domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-INTRA-3", Name: "Latest", Price: 20.0, Status: domain.StatusActive, Timestamp: t2},
			{SKU: "SKU-INTRA-3", Name: "Stale", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
		},
	}
	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 || res.Skipped != 1 {
		t.Errorf("expected Added=1, Skipped=1; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-INTRA-3")
	if p.Name != "Latest" || p.Price != 20.0 {
		t.Errorf("stale record overwrote newer record in same batch: %+v", p)
	}
}

// TestEngine_IntraBatch_TombstoneLock tests ACTIVE -> WITHDRAWN -> ACTIVE in the same batch.
func TestEngine_IntraBatch_TombstoneLock(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 10, 5, 0, 0, time.UTC)
	t3 := time.Date(2026, 10, 21, 10, 10, 0, 0, time.UTC)

	batch := domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-INTRA-4", Name: "Born Active", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: "SKU-INTRA-4", Name: "Withdrawn Now", Price: 10.0, Status: domain.StatusWithdrawn, Timestamp: t2},
			{SKU: "SKU-INTRA-4", Name: "Illegal Reactivation", Price: 10.0, Status: domain.StatusActive, Timestamp: t3},
		},
	}
	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Added != 1 || res.Withdrawn != 1 || res.Skipped != 1 {
		t.Errorf("expected Added=1, Withdrawn=1, Skipped=1; got %+v", res)
	}

	p, _ := store.GetProduct(ctx, "SKU-INTRA-4")
	if p.Status != domain.StatusWithdrawn {
		t.Fatalf("tombstone not preserved intra-batch: got status %s", p.Status)
	}
}

// TestEngine_EmptyBatch verifies that empty batch does not touch database.
func TestEngine_EmptyBatch(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	res, err := engine.ExecuteBatch(ctx, domain.Batch{Records: []domain.SupplierUpdate{}})
	if err != nil {
		t.Fatalf("empty batch error: %v", err)
	}
	if res != (domain.ChangeSummary{}) {
		t.Errorf("expected zero summary, got %+v", res)
	}
}

// TestEngine_ContextCancellation verifies transactional rollback upon cancelled context.
func TestEngine_ContextCancellation(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context upfront

	_, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-CANCEL", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: time.Now().UTC()},
		},
	})
	if err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}

	// Verify nothing was written
	p, err := store.GetProduct(context.Background(), "SKU-CANCEL")
	if p != nil || (err != nil && !errors.Is(err, domain.ErrNotFound)) {
		t.Errorf("product was committed despite cancellation: %+v, err: %v", p, err)
	}
}

// TestEngine_DeadLetter_InvalidRecords tests quarantine of negative prices, empty SKUs, and invalid statuses.
func TestEngine_DeadLetter_InvalidRecords(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	now := time.Now().UTC()
	batch := domain.Batch{
		JobID:      "job-dl-test",
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-VALID", Name: "Valid Item", Price: 10.0, Status: domain.StatusActive, Timestamp: now},
			{SKU: "   ", Name: "Whitespace SKU", Price: 15.0, Status: domain.StatusActive, Timestamp: now},
			{SKU: "SKU-NEG", Name: "Negative Price", Price: -5.0, Status: domain.StatusActive, Timestamp: now},
			{SKU: "SKU-BAD-STATUS", Name: "Bad Status", Price: 20.0, Status: domain.ProductStatus("UNKNOWN"), Timestamp: now},
			{SKU: "SKU-ZERO-TIME", Name: "Zero Timestamp Item", Price: 15.0, Status: domain.StatusActive, Timestamp: time.Time{}},
		},
	}

	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	if res.Added != 1 || res.DeadLetter != 4 {
		t.Errorf("expected Added=1, DeadLetter=4; got %+v", res)
	}

	// Verify valid item is committed
	p, err := store.GetProduct(ctx, "SKU-VALID")
	if err != nil || p == nil {
		t.Errorf("valid product was not saved: %v", err)
	}

	// Verify zero-timestamp item was not committed
	pZero, errZero := store.GetProduct(ctx, "SKU-ZERO-TIME")
	if errZero == nil && pZero != nil {
		t.Errorf("corrupt zero-timestamp product was erroneously committed to products table: %+v", pZero)
	}

	// Verify dead letters in storage
	dlRecords, count, err := store.ListDeadLetterRecords(ctx, "job-dl-test", "", 10, 0)
	if err != nil {
		t.Fatalf("failed listing dead letter records: %v", err)
	}
	if count != 4 || len(dlRecords) != 4 {
		t.Errorf("expected 4 dead letters recorded, got %d", count)
	}

	var foundTimestampReason bool
	for _, dl := range dlRecords {
		if strings.Contains(strings.ToLower(dl.Reason), "timestamp") {
			foundTimestampReason = true
			break
		}
	}
	if !foundTimestampReason {
		t.Errorf("expected at least one dead letter record with timestamp reason")
	}
}

// TestEngine_NilStorage verifies error when storage is nil.
func TestEngine_NilStorage(t *testing.T) {
	engine := catalogSync.NewEngine(nil)
	_, err := engine.ExecuteBatch(context.Background(), domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-1", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: time.Now().UTC()},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "storage is nil") {
		t.Errorf("expected nil storage error, got %v", err)
	}

	_, err = engine.RecoverOnStartup(context.Background())
	if err == nil {
		t.Errorf("expected error on RecoverOnStartup with nil storage")
	}
}

// TestEngine_Milestone2Validation verifies parameter validation for RunJob, ResumeJob, and RecoverOnStartup.
func TestEngine_Milestone2Validation(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	// Nil batch stream
	_, err := engine.RunJob(ctx, "job-val", nil)
	if err == nil || !errors.Is(err, catalogSync.ErrNilBatchStream) {
		t.Errorf("expected ErrNilBatchStream from RunJob, got %v", err)
	}

	_, err = engine.ResumeJob(ctx, "job-val", nil)
	if err == nil || !errors.Is(err, catalogSync.ErrNilBatchStream) {
		t.Errorf("expected ErrNilBatchStream from ResumeJob, got %v", err)
	}

	// Empty job ID
	stream := make(chan domain.Batch)
	close(stream)
	_, err = engine.RunJob(ctx, "  ", stream)
	if err == nil || !errors.Is(err, catalogSync.ErrEmptyJobID) {
		t.Errorf("expected ErrEmptyJobID from RunJob, got %v", err)
	}

	_, err = engine.ResumeJob(ctx, "  ", stream)
	if err == nil || !errors.Is(err, catalogSync.ErrEmptyJobID) {
		t.Errorf("expected ErrEmptyJobID from ResumeJob, got %v", err)
	}

	recovered, err := engine.RecoverOnStartup(ctx)
	if err != nil {
		t.Fatalf("RecoverOnStartup failed: %v", err)
	}
	if len(recovered) != 0 {
		t.Errorf("expected 0 recovered on clean startup, got %d", len(recovered))
	}
}

// TestEngine_WithTimeFunc verifies custom clock option.
func TestEngine_WithTimeFunc(t *testing.T) {
	store := setupTestStorage(t)
	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	engine := catalogSync.NewEngine(store, catalogSync.WithTimeFunc(func() time.Time {
		return fixedTime
	}))

	ctx := context.Background()
	_, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-FIXED-TIME", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: fixedTime},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	p, err := store.GetProduct(ctx, "SKU-FIXED-TIME")
	if err != nil {
		t.Fatalf("failed getting product: %v", err)
	}
	if !p.UpdatedAt.Equal(fixedTime) {
		t.Errorf("expected UpdatedAt %v, got %v", fixedTime, p.UpdatedAt)
	}
}

// TestEngine_IdenticalProduct_Variations covers branches where fields differ on same timestamp.
func TestEngine_IdenticalProduct_Variations(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	// Seed initial
	_, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-VAR", Name: "Original Name", Price: 100.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// 1. Same timestamp, different name -> not identical, but timestamp didn't advance -> skipped
	resName, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-VAR", Name: "Different Name", Price: 100.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})
	if err != nil || resName.Skipped != 1 {
		t.Errorf("expected Skipped=1 for different name with same timestamp; got %+v, err: %v", resName, err)
	}

	// 2. Same timestamp, different price -> not identical, but timestamp didn't advance -> skipped
	resPrice, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-VAR", Name: "Original Name", Price: 150.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})
	if err != nil || resPrice.Skipped != 1 {
		t.Errorf("expected Skipped=1 for different price with same timestamp; got %+v, err: %v", resPrice, err)
	}

	// 3. Same timestamp, different status -> not identical, but timestamp didn't advance -> skipped
	resStatus, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-VAR", Name: "Original Name", Price: 100.0, Status: domain.StatusWithdrawn, Timestamp: t1},
		},
	})
	if err != nil || resStatus.Skipped != 1 {
		t.Errorf("expected Skipped=1 for different status with same timestamp; got %+v, err: %v", resStatus, err)
	}
}

// TestEngine_ContextCancellation_DuringIteration tests context being cancelled during batch loop.
func TestEngine_ContextCancellation_DuringIteration(t *testing.T) {
	store := setupTestStorage(t)
	ctx, cancel := context.WithCancel(context.Background())

	// We create a mock engine using a clock hook that cancels the context after 1st invocation
	count := 0
	engine := catalogSync.NewEngine(store, catalogSync.WithTimeFunc(func() time.Time {
		count++
		if count >= 1 {
			cancel()
		}
		return time.Now().UTC()
	}))

	now := time.Now().UTC()
	_, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-ITER-1", Name: "Item 1", Price: 10.0, Status: domain.StatusActive, Timestamp: now},
			{SKU: "SKU-ITER-2", Name: "Item 2", Price: 20.0, Status: domain.StatusActive, Timestamp: now},
		},
	})
	if err == nil {
		t.Errorf("expected context cancelled error, got nil")
	}
}

// TestEngine_StorageBeginTxError tests failure when beginning a transaction on closed storage.
func TestEngine_StorageBeginTxError(t *testing.T) {
	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed open: %v", err)
	}
	_ = store.Close() // Close underlying DB so BeginTx fails

	engine := catalogSync.NewEngine(store)
	_, err = engine.ExecuteBatch(context.Background(), domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-FAIL", Name: "Item", Price: 10.0, Status: domain.StatusActive, Timestamp: time.Now().UTC()},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "failed to begin batch transaction") {
		t.Errorf("expected begin batch transaction error, got %v", err)
	}
}

// TestEngine_WithProgressHook tests that the progress hook is invoked after each batch commits.
func TestEngine_WithProgressHook(t *testing.T) {
	store := setupTestStorage(t)
	ctx := context.Background()

	type progressReport struct {
		jobID        string
		batchIndex   int
		totalBatches int
		delta        domain.ChangeSummary
		cumulative   domain.ChangeSummary
	}

	var reports []progressReport
	hook := func(jobID string, batchIndex int, totalBatches int, delta domain.ChangeSummary, cumulative domain.ChangeSummary) {
		reports = append(reports, progressReport{
			jobID:        jobID,
			batchIndex:   batchIndex,
			totalBatches: totalBatches,
			delta:        delta,
			cumulative:   cumulative,
		})
	}

	engine := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))

	jobID := "job-hook-test"
	now := time.Now().UTC()
	batchStream := make(chan domain.Batch, 2)
	batchStream <- domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-H1", Name: "H1", Price: 10.0, Status: domain.StatusActive, Timestamp: now},
		},
	}
	batchStream <- domain.Batch{
		JobID:      jobID,
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-H2", Name: "H2", Price: 20.0, Status: domain.StatusActive, Timestamp: now},
		},
	}
	close(batchStream)

	job, err := engine.RunJob(ctx, jobID, batchStream)
	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}
	if job.Status != domain.JobStatusCompleted {
		t.Fatalf("expected completed job, got %s", job.Status)
	}

	if len(reports) != 2 {
		t.Fatalf("expected 2 progress reports, got %d", len(reports))
	}

	if reports[0].jobID != jobID || reports[0].batchIndex != 0 || reports[0].delta.Added != 1 || reports[0].cumulative.Added != 1 {
		t.Errorf("unexpected report 0: %+v", reports[0])
	}
	if reports[1].jobID != jobID || reports[1].batchIndex != 1 || reports[1].delta.Added != 1 || reports[1].cumulative.Added != 2 {
		t.Errorf("unexpected report 1: %+v", reports[1])
	}
}
