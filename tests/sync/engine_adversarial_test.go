package sync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// computeCatalogChecksum computes a deterministic SHA-256 hash over all products in SQLite.
// Includes sku, name, price (formatted to 8 decimals), status, supplier_timestamp, and updated_at.
func computeCatalogChecksum(t *testing.T, store *storage.SQLiteStorage) (string, int) {
	t.Helper()
	rows, err := store.DB().Query(`
		SELECT sku, name, printf('%.8f', price), status, supplier_timestamp, updated_at
		FROM products ORDER BY sku ASC;
	`)
	if err != nil {
		t.Fatalf("failed querying products for checksum: %v", err)
	}
	defer rows.Close()

	hasher := sha256.New()
	count := 0
	for rows.Next() {
		count++
		var sku, name, priceStr, status, suppTime, updTime string
		if err := rows.Scan(&sku, &name, &priceStr, &status, &suppTime, &updTime); err != nil {
			t.Fatalf("failed scanning product row: %v", err)
		}
		rowStr := fmt.Sprintf("%s|%s|%s|%s|%s|%s\n", sku, name, priceStr, status, suppTime, updTime)
		hasher.Write([]byte(rowStr))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), count
}

// -----------------------------------------------------------------------------
// INVARIANT A: DETERMINISTIC IDEMPOTENCY
// -----------------------------------------------------------------------------

// TestAdversarial_InvariantA_ComplexBatchReplayZeroMutations tests that replaying
// a complex batch (with intra-batch duplicates and multiple revisions) produces
// exactly 0 mutations (0 Added, 0 Updated, 0 Withdrawn, 100% Skipped) and identical
// catalog checksums.
func TestAdversarial_InvariantA_ComplexBatchReplayZeroMutations(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	baseTime := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	var records []domain.SupplierUpdate

	// 1. 200 distinct unique items
	for i := 0; i < 200; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-IDEMP-%04d", i),
			Name:      fmt.Sprintf("Product %04d", i),
			Price:     float64(i)*1.5 + 0.99,
			Status:    domain.StatusActive,
			Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
		})
	}

	// 2. 100 intra-batch duplicates (exact identical records re-appearing)
	for i := 0; i < 100; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-IDEMP-%04d", i),
			Name:      fmt.Sprintf("Product %04d", i),
			Price:     float64(i)*1.5 + 0.99,
			Status:    domain.StatusActive,
			Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
		})
	}

	// 3. 50 intra-batch progressive revisions with newer timestamps
	for i := 100; i < 150; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-IDEMP-%04d", i),
			Name:      fmt.Sprintf("Product %04d Rev2", i),
			Price:     float64(i)*1.5 + 9.99,
			Status:    domain.StatusActive,
			Timestamp: baseTime.Add(time.Duration(i)*time.Minute + 10*time.Hour),
		})
	}

	// 4. 50 items marked WITHDRAWN
	for i := 150; i < 200; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-IDEMP-%04d", i),
			Name:      fmt.Sprintf("Product %04d", i),
			Price:     float64(i)*1.5 + 0.99,
			Status:    domain.StatusWithdrawn,
			Timestamp: baseTime.Add(time.Duration(i)*time.Minute + 20*time.Hour),
		})
	}

	batch := domain.Batch{
		JobID:      "job-idemp-1",
		BatchIndex: 0,
		Records:    records,
	}

	// First execution
	res1, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("first ExecuteBatch failed: %v", err)
	}
	if res1.TotalProcessed() != len(records) {
		t.Fatalf("run 1 total processed mismatch: got %d, want %d", res1.TotalProcessed(), len(records))
	}

	// Snapshot catalog state checksum and row count
	checksumBeforeReplay, countBeforeReplay := computeCatalogChecksum(t, store)
	if countBeforeReplay != 200 {
		t.Fatalf("expected 200 unique products in catalog, got %d", countBeforeReplay)
	}

	// Replay exact same batch
	res2, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("replayed ExecuteBatch failed: %v", err)
	}

	// Assert Invariant A: 0 Added, 0 Updated, 0 Withdrawn, 100% Skipped
	if res2.Added != 0 || res2.Updated != 0 || res2.Withdrawn != 0 || res2.DeadLetter != 0 {
		t.Errorf("replay violated Invariant A with non-zero mutations: %+v", res2)
	}
	if res2.Skipped != len(records) {
		t.Errorf("replay expected Skipped=%d, got %d", len(records), res2.Skipped)
	}

	// Verify catalog checksum is 100% identical bit-for-bit (including updated_at)
	checksumAfterReplay, countAfterReplay := computeCatalogChecksum(t, store)
	if countBeforeReplay != countAfterReplay {
		t.Errorf("product count drifted: before=%d, after=%d", countBeforeReplay, countAfterReplay)
	}
	if checksumBeforeReplay != checksumAfterReplay {
		t.Errorf("catalog state drifted upon identical batch replay!\nBefore: %s\nAfter:  %s", checksumBeforeReplay, checksumAfterReplay)
	}
}

// TestAdversarial_InvariantA_MultiBatchStreamReplay tests replaying a stream of 10 batches
// in original sequence, reverse sequence, and shuffled sequence, verifying zero drift.
func TestAdversarial_InvariantA_MultiBatchStreamReplay(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	numBatches := 10
	batchSize := 50
	baseTime := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

	// Generate 10 batches across 100 SKUs with evolving revisions
	batches := make([]domain.Batch, numBatches)
	for b := 0; b < numBatches; b++ {
		var recs []domain.SupplierUpdate
		for i := 0; i < batchSize; i++ {
			skuIdx := (b*13 + i*7) % 100
			recs = append(recs, domain.SupplierUpdate{
				SKU:       fmt.Sprintf("SKU-STREAM-%03d", skuIdx),
				Name:      fmt.Sprintf("Item %03d Rev %d", skuIdx, b),
				Price:     float64(skuIdx) + float64(b)*0.5,
				Status:    domain.StatusActive,
				Timestamp: baseTime.Add(time.Duration(b)*time.Hour + time.Duration(i)*time.Minute),
			})
		}
		batches[b] = domain.Batch{
			JobID:      "job-stream",
			BatchIndex: b,
			Records:    recs,
		}
	}

	// Ingest all 10 batches
	for b := 0; b < numBatches; b++ {
		_, err := engine.ExecuteBatch(ctx, batches[b])
		if err != nil {
			t.Fatalf("initial ingestion failed at batch %d: %v", b, err)
		}
	}

	checksumOriginal, countOriginal := computeCatalogChecksum(t, store)
	if countOriginal != 100 {
		t.Fatalf("expected 100 unique products in catalog, got %d", countOriginal)
	}

	// 1. Replay in forward order (0 to 9)
	for b := 0; b < numBatches; b++ {
		res, err := engine.ExecuteBatch(ctx, batches[b])
		if err != nil {
			t.Fatalf("forward replay failed at batch %d: %v", b, err)
		}
		if res.Added != 0 || res.Updated != 0 || res.Withdrawn != 0 {
			t.Fatalf("forward replay mutation at batch %d: %+v", b, res)
		}
		if res.Skipped != batchSize {
			t.Fatalf("forward replay skipped mismatch at batch %d: got %d, want %d", b, res.Skipped, batchSize)
		}
	}
	checksumForward, _ := computeCatalogChecksum(t, store)
	if checksumOriginal != checksumForward {
		t.Fatalf("catalog drifted after forward replay")
	}

	// 2. Replay in reverse order (9 down to 0)
	for b := numBatches - 1; b >= 0; b-- {
		res, err := engine.ExecuteBatch(ctx, batches[b])
		if err != nil {
			t.Fatalf("reverse replay failed at batch %d: %v", b, err)
		}
		if res.Added != 0 || res.Updated != 0 || res.Withdrawn != 0 {
			t.Fatalf("reverse replay mutation at batch %d: %+v", b, res)
		}
		if res.Skipped != batchSize {
			t.Fatalf("reverse replay skipped mismatch at batch %d: got %d, want %d", b, res.Skipped, batchSize)
		}
	}
	checksumReverse, _ := computeCatalogChecksum(t, store)
	if checksumOriginal != checksumReverse {
		t.Fatalf("catalog drifted after reverse replay")
	}

	// 3. Replay in arbitrary re-chunked batch sizes (e.g. 1 record per batch)
	for b := 0; b < numBatches; b++ {
		for _, rec := range batches[b].Records {
			res, err := engine.ExecuteBatch(ctx, domain.Batch{
				JobID:      "single-record-replay",
				BatchIndex: 0,
				Records:    []domain.SupplierUpdate{rec},
			})
			if err != nil {
				t.Fatalf("single record replay failed: %v", err)
			}
			if res.Added != 0 || res.Updated != 0 || res.Withdrawn != 0 || res.Skipped != 1 {
				t.Fatalf("single record replay produced unexpected result: %+v", res)
			}
		}
	}
	checksumSingle, _ := computeCatalogChecksum(t, store)
	if checksumOriginal != checksumSingle {
		t.Fatalf("catalog drifted after single-record replay")
	}
}

// TestAdversarial_InvariantA_FloatingPointPrecision verifies price equality with epsilon boundaries.
func TestAdversarial_InvariantA_FloatingPointPrecision(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	// Seed with 10.0000001
	_, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-FLOAT-1", Name: "Widget", Price: 10.0000001, Status: domain.StatusActive, Timestamp: t1},
		},
	})
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Replay with identical timestamp and price difference 1e-7 (< 1e-6 epsilon)
	// Must be treated as identical -> Skipped!
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-FLOAT-1", Name: "Widget", Price: 10.0000002, Status: domain.StatusActive, Timestamp: t1},
		},
	})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if res.Skipped != 1 || res.Updated != 0 {
		t.Errorf("expected Skipped=1 for sub-epsilon price difference, got %+v", res)
	}

	// Zero price: free product (Price = 0.0)
	_, err = engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-ZERO-PRICE", Name: "Free Sample", Price: 0.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})
	if err != nil {
		t.Fatalf("zero price insert failed: %v", err)
	}

	// Replay zero price
	resZero, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-ZERO-PRICE", Name: "Free Sample", Price: 0.0, Status: domain.StatusActive, Timestamp: t1},
		},
	})
	if err != nil {
		t.Fatalf("zero price replay failed: %v", err)
	}
	if resZero.Skipped != 1 || resZero.Added != 0 || resZero.Updated != 0 {
		t.Errorf("zero price replay failed: %+v", resZero)
	}
}

// -----------------------------------------------------------------------------
// INVARIANT B: FAST-FORWARD TIMESTAMPS & OUT-OF-ORDER STRESS
// -----------------------------------------------------------------------------

// TestAdversarial_InvariantB_ShuffledStreamStressOracle generates an authoritative
// chronological history for 200 SKUs (10 updates per SKU), pools all 2,000 updates,
// randomly shuffles them completely out-of-order, and feeds them into the sync engine.
// It verifies against the oracle that EVERY SKU achieves strictly the state corresponding
// to its maximum timestamp, with 0 older timestamps overwriting newer state.
func TestAdversarial_InvariantB_ShuffledStreamStressOracle(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	numSKUs := 200
	updatesPerSKU := 10
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Ground truth oracle: maps SKU to its expected final state (highest timestamp)
	type OracleProduct struct {
		SKU               string
		Name              string
		Price             float64
		Status            domain.ProductStatus
		SupplierTimestamp time.Time
	}
	oracle := make(map[string]OracleProduct)

	var allUpdates []domain.SupplierUpdate

	for s := 0; s < numSKUs; s++ {
		sku := fmt.Sprintf("SKU-ORACLE-%04d", s)
		for u := 0; u < updatesPerSKU; u++ {
			ts := baseTime.Add(time.Duration(s)*24*time.Hour + time.Duration(u+1)*time.Hour)
			status := domain.StatusActive
			// For some SKUs, the final update is WITHDRAWN
			if s%4 == 0 && u == updatesPerSKU-1 {
				status = domain.StatusWithdrawn
			}

			update := domain.SupplierUpdate{
				SKU:       sku,
				Name:      fmt.Sprintf("Item %04d Version %d", s, u),
				Price:     10.0 + float64(s) + float64(u)*2.5,
				Status:    status,
				Timestamp: ts,
			}
			allUpdates = append(allUpdates, update)

			// The update with u == updatesPerSKU-1 has the highest timestamp
			if u == updatesPerSKU-1 {
				oracle[sku] = OracleProduct{
					SKU:               sku,
					Name:              update.Name,
					Price:             update.Price,
					Status:            update.Status,
					SupplierTimestamp: update.Timestamp,
				}
			}
		}
	}

	// Total updates: 200 * 10 = 2,000 updates
	if len(allUpdates) != numSKUs*updatesPerSKU {
		t.Fatalf("expected %d updates, generated %d", numSKUs*updatesPerSKU, len(allUpdates))
	}

	// Deterministic random shuffle of the entire update stream
	rng := rand.New(rand.NewPCG(42, 99))
	rng.Shuffle(len(allUpdates), func(i, j int) {
		allUpdates[i], allUpdates[j] = allUpdates[j], allUpdates[i]
	})

	// Partition into batches of 50 updates each
	batchSize := 50
	totalAdded := 0
	totalUpdated := 0
	totalWithdrawn := 0
	totalSkipped := 0

	for start := 0; start < len(allUpdates); start += batchSize {
		end := start + batchSize
		if end > len(allUpdates) {
			end = len(allUpdates)
		}
		batch := domain.Batch{
			JobID:      "job-shuffled-stream",
			BatchIndex: start / batchSize,
			Records:    allUpdates[start:end],
		}
		res, err := engine.ExecuteBatch(ctx, batch)
		if err != nil {
			t.Fatalf("ExecuteBatch failed on batch %d: %v", start/batchSize, err)
		}
		totalAdded += res.Added
		totalUpdated += res.Updated
		totalWithdrawn += res.Withdrawn
		totalSkipped += res.Skipped
	}

	// 1. Total added MUST equal the number of unique SKUs
	if totalAdded != numSKUs {
		t.Errorf("total added mismatch: got %d, want %d", totalAdded, numSKUs)
	}

	// 2. Total processed records must equal total inputs
	totalProcessed := totalAdded + totalUpdated + totalWithdrawn + totalSkipped
	if totalProcessed != len(allUpdates) {
		t.Errorf("total processed mismatch: got %d, want %d", totalProcessed, len(allUpdates))
	}

	// 3. Verify every SKU matches the Oracle's expected final state exactly!
	for sku, expected := range oracle {
		prod, err := store.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("failed retrieving SKU %s: %v", sku, err)
		}
		if prod == nil {
			t.Fatalf("SKU %s missing from catalog!", sku)
		}

		if !prod.SupplierTimestamp.Equal(expected.SupplierTimestamp) {
			t.Errorf("SKU %s timestamp mismatch: got %v, want %v (out-of-order overwrite detected!)",
				sku, prod.SupplierTimestamp, expected.SupplierTimestamp)
		}
		if prod.Name != expected.Name {
			t.Errorf("SKU %s name mismatch: got %q, want %q", sku, prod.Name, expected.Name)
		}
		if math.Abs(prod.Price-expected.Price) > 1e-6 {
			t.Errorf("SKU %s price mismatch: got %f, want %f", sku, prod.Price, expected.Price)
		}
		if prod.Status != expected.Status {
			t.Errorf("SKU %s status mismatch: got %s, want %s", sku, prod.Status, expected.Status)
		}
	}
}

// TestAdversarial_InvariantB_SubMillisecondAndNanosecondResolution verifies that
// sub-millisecond, microsecond, and nanosecond timestamp differences are strictly preserved
// and accurately resolved, including persistence roundtrips through SQLite.
func TestAdversarial_InvariantB_SubMillisecondAndNanosecondResolution(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	// Base timestamp
	base := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	t1 := base.Add(1 * time.Nanosecond)
	t2 := base.Add(2 * time.Nanosecond)
	t3 := base.Add(500 * time.Nanosecond)
	t4 := base.Add(999999 * time.Nanosecond)  // Sub-millisecond boundary
	t5 := base.Add(1000000 * time.Nanosecond) // Exactly 1 millisecond

	sku := "SKU-NANO-TEST"

	// 1. Initial insert with t3 (500 ns)
	res1, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "V3_500ns", Price: 50.0, Status: domain.StatusActive, Timestamp: t3},
		},
	})
	if err != nil || res1.Added != 1 {
		t.Fatalf("initial insert failed: res=%+v, err=%v", res1, err)
	}

	// 2. Out-of-order older nanosecond records: t1 (1 ns) and t2 (2 ns)
	// Must be rejected and counted as Skipped
	res2, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "V1_1ns", Price: 10.0, Status: domain.StatusActive, Timestamp: t1},
			{SKU: sku, Name: "V2_2ns", Price: 20.0, Status: domain.StatusActive, Timestamp: t2},
		},
	})
	if err != nil {
		t.Fatalf("stale batch failed: %v", err)
	}
	if res2.Skipped != 2 || res2.Updated != 0 || res2.Added != 0 {
		t.Errorf("expected Skipped=2 for older nanosecond updates; got %+v", res2)
	}

	// 3. Verify SQLite state still has t3
	p, err := store.GetProduct(ctx, sku)
	if err != nil || p == nil {
		t.Fatalf("failed to retrieve product: %v", err)
	}
	if !p.SupplierTimestamp.Equal(t3) || p.SupplierTimestamp.Nanosecond() != 500 {
		t.Errorf("expected timestamp %v (500ns), got %v (%dns)", t3, p.SupplierTimestamp, p.SupplierTimestamp.Nanosecond())
	}
	if p.Name != "V3_500ns" {
		t.Errorf("name was overwritten by older nanosecond record: %s", p.Name)
	}

	// 4. Ingest newer t4 (999,999 ns) and t5 (1,000,000 ns) in ascending order
	res3, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "V4_999999ns", Price: 75.0, Status: domain.StatusActive, Timestamp: t4},
			{SKU: sku, Name: "V5_1ms", Price: 100.0, Status: domain.StatusActive, Timestamp: t5},
		},
	})
	if err != nil {
		t.Fatalf("newer batch failed: %v", err)
	}
	if res3.Updated != 2 {
		t.Errorf("expected Updated=2; got %+v", res3)
	}

	// 5. Final state must be t5 (1ms)
	pFinal, err := store.GetProduct(ctx, sku)
	if err != nil || pFinal == nil {
		t.Fatalf("failed retrieving final product: %v", err)
	}
	if !pFinal.SupplierTimestamp.Equal(t5) || pFinal.Name != "V5_1ms" {
		t.Errorf("final state incorrect: %+v", pFinal)
	}

	// 6. Intra-batch nanosecond re-ordering:
	// In a single batch, provide [t5 (same), t3 (stale), t5+1ns (newer), t2 (stale)]
	t6 := t5.Add(1 * time.Nanosecond)
	resIntra, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "Same as V5", Price: 100.0, Status: domain.StatusActive, Timestamp: t5},
			{SKU: sku, Name: "Stale T3", Price: 30.0, Status: domain.StatusActive, Timestamp: t3},
			{SKU: sku, Name: "V6_Newer", Price: 120.0, Status: domain.StatusActive, Timestamp: t6},
			{SKU: sku, Name: "Stale T2", Price: 20.0, Status: domain.StatusActive, Timestamp: t2},
		},
	})
	if err != nil {
		t.Fatalf("intra-batch nanosecond failed: %v", err)
	}
	if resIntra.Updated != 1 || resIntra.Skipped != 3 {
		t.Errorf("intra-batch nanosecond expected Updated=1, Skipped=3; got %+v", resIntra)
	}

	pFinal2, _ := store.GetProduct(ctx, sku)
	if !pFinal2.SupplierTimestamp.Equal(t6) || pFinal2.Name != "V6_Newer" {
		t.Errorf("intra-batch final state mismatch: %+v", pFinal2)
	}
}

// TestAdversarial_InvariantB_DistantPastAndFutureBoundaries verifies timestamp comparisons
// across extreme calendar boundaries: Year 0001, Year 1970 (Epoch), Year 2038 (Unix 32-bit overflow),
// Year 2099, and Year 9999.
func TestAdversarial_InvariantB_DistantPastAndFutureBoundaries(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t0001 := time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC)
	t1970 := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	t2038 := time.Date(2038, 1, 19, 3, 14, 7, 0, time.UTC)
	t2099 := time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
	t9999 := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

	sku := "SKU-EXTREME-DATES"

	// 1. Seed with Year 2099
	res1, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "Future 2099", Price: 2099.0, Status: domain.StatusActive, Timestamp: t2099},
		},
	})
	if err != nil || res1.Added != 1 {
		t.Fatalf("insert 2099 failed: res=%+v, err=%v", res1, err)
	}

	// 2. Try to overwrite with past dates (Year 0001, 1970, 2038) -> all must be skipped!
	resPast, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "Year 0001", Price: 1.0, Status: domain.StatusActive, Timestamp: t0001},
			{SKU: sku, Name: "Year 1970", Price: 1970.0, Status: domain.StatusActive, Timestamp: t1970},
			{SKU: sku, Name: "Year 2038", Price: 2038.0, Status: domain.StatusActive, Timestamp: t2038},
		},
	})
	if err != nil {
		t.Fatalf("past batch failed: %v", err)
	}
	if resPast.Skipped != 3 || resPast.Updated != 0 {
		t.Errorf("past dates should be skipped against 2099; got %+v", resPast)
	}

	// 3. Advance to Year 9999 -> must update!
	resFuture, err := engine.ExecuteBatch(ctx, domain.Batch{
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "Year 9999", Price: 9999.0, Status: domain.StatusActive, Timestamp: t9999},
		},
	})
	if err != nil {
		t.Fatalf("future 9999 failed: %v", err)
	}
	if resFuture.Updated != 1 {
		t.Errorf("advancing to 9999 expected Updated=1; got %+v", resFuture)
	}

	// 4. Verify SQLite roundtrip
	p, err := store.GetProduct(ctx, sku)
	if err != nil || p == nil {
		t.Fatalf("failed reading product: %v", err)
	}
	if !p.SupplierTimestamp.Equal(t9999) || p.Name != "Year 9999" {
		t.Errorf("product did not advance to 9999: %+v", p)
	}
}

// TestAdversarial_InvariantB_ConcurrentChaosWritersSharedSKU verifies that multiple
// concurrent writers executing batches on shared SKUs serialize correctly and that
// the final state reflects the absolute highest timestamp submitted across ALL goroutines.
func TestAdversarial_InvariantB_ConcurrentChaosWritersSharedSKU(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	numGoroutines := 8
	numSKUs := 5
	updatesPerGoroutine := 15
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Thread-safe map tracking maximum timestamp generated for each SKU
	var maxMu sync.Mutex
	type WinningState struct {
		Timestamp time.Time
		Name      string
		Price     float64
	}
	globalMax := make(map[string]WinningState)

	var wg sync.WaitGroup

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(gid+1), uint64(gid*31+7)))

			for u := 0; u < updatesPerGoroutine; u++ {
				// Pick random SKU
				skuIdx := rng.IntN(numSKUs)
				sku := fmt.Sprintf("SKU-CHAOS-%02d", skuIdx)

				// Pick random minute offset between 1 and 5,000
				offsetMinutes := rng.IntN(5000) + 1
				ts := baseTime.Add(time.Duration(offsetMinutes) * time.Minute)
				name := fmt.Sprintf("Winner G%d U%d", gid, u)
				price := float64(offsetMinutes) * 0.1

				maxMu.Lock()
				currentMax, exists := globalMax[sku]
				if !exists || ts.After(currentMax.Timestamp) {
					globalMax[sku] = WinningState{
						Timestamp: ts,
						Name:      name,
						Price:     price,
					}
				}
				maxMu.Unlock()

				_, err := engine.ExecuteBatch(ctx, domain.Batch{
					JobID:      fmt.Sprintf("job-chaos-g%d", gid),
					BatchIndex: u,
					Records: []domain.SupplierUpdate{
						{
							SKU:       sku,
							Name:      name,
							Price:     price,
							Status:    domain.StatusActive,
							Timestamp: ts,
						},
					},
				})
				if err != nil {
					t.Errorf("concurrent ExecuteBatch error in g%d: %v", gid, err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify all SKUs in database match the global max timestamp and corresponding payload!
	for sku, expected := range globalMax {
		prod, err := store.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("failed to retrieve SKU %s: %v", sku, err)
		}
		if prod == nil {
			t.Fatalf("SKU %s not found in catalog", sku)
		}

		if !prod.SupplierTimestamp.Equal(expected.Timestamp) {
			t.Errorf("SKU %s final timestamp %v != expected maximum %v", sku, prod.SupplierTimestamp, expected.Timestamp)
		}
		if prod.Name != expected.Name {
			t.Errorf("SKU %s final name %q != expected %q", sku, prod.Name, expected.Name)
		}
		if math.Abs(prod.Price-expected.Price) > 1e-6 {
			t.Errorf("SKU %s final price %f != expected %f", sku, prod.Price, expected.Price)
		}
	}
}

// -----------------------------------------------------------------------------
// COMBINED STRESS: MASSIVE INTERLEAVED CHAOS STREAM WITH IN-MEMORY REFERENCE ORACLE
// -----------------------------------------------------------------------------

// TestAdversarial_Combined_IdempotencyAndFastForwardChaos tests 50 randomized batches
// against an in-memory ground-truth reference model. It verifies that the database
// state matches the reference model exactly, and that subsequent full-stream replay
// produces 100% Skipped and 0 mutations.
func TestAdversarial_Combined_IdempotencyAndFastForwardChaos(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	numBatches := 30
	recordsPerBatch := 40
	numUniqueSKUs := 50
	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Ground truth reference model simulating Invariants A, B, C
	type RefItem struct {
		SKU               string
		Name              string
		Price             float64
		Status            domain.ProductStatus
		SupplierTimestamp time.Time
	}
	refCatalog := make(map[string]RefItem)

	simulateUpdate := func(rec domain.SupplierUpdate) {
		sku := rec.SKU
		existing, found := refCatalog[sku]
		if !found {
			// Insert
			refCatalog[sku] = RefItem{
				SKU:               sku,
				Name:              rec.Name,
				Price:             rec.Price,
				Status:            rec.Status,
				SupplierTimestamp: rec.Timestamp,
			}
			return
		}

		// Invariant A: Identical -> skip
		if existing.Name == rec.Name &&
			math.Abs(existing.Price-rec.Price) < 1e-6 &&
			existing.Status == rec.Status &&
			existing.SupplierTimestamp.Equal(rec.Timestamp) {
			return
		}

		// Invariant C: Tombstone -> skip resurrection
		if existing.Status == domain.StatusWithdrawn && rec.Status == domain.StatusActive {
			return
		}

		// Invariant B: Fast-forward -> skip older or equal
		if rec.Timestamp.Before(existing.SupplierTimestamp) || rec.Timestamp.Equal(existing.SupplierTimestamp) {
			return
		}

		// Valid advance
		refCatalog[sku] = RefItem{
			SKU:               sku,
			Name:              rec.Name,
			Price:             rec.Price,
			Status:            rec.Status,
			SupplierTimestamp: rec.Timestamp,
		}
	}

	rng := rand.New(rand.NewPCG(12345, 67890))
	var recordedBatches []domain.Batch

	for b := 0; b < numBatches; b++ {
		var records []domain.SupplierUpdate
		for r := 0; r < recordsPerBatch; r++ {
			skuIdx := rng.IntN(numUniqueSKUs)
			sku := fmt.Sprintf("SKU-CHAOS-STREAM-%03d", skuIdx)

			// Generate random timestamp (either past, present, or future relative to base)
			minuteOffset := rng.IntN(500) - 100 // Can be negative (older)
			ts := baseTime.Add(time.Duration(minuteOffset) * time.Minute)

			status := domain.StatusActive
			if rng.Float64() < 0.15 {
				status = domain.StatusWithdrawn
			}

			price := float64(rng.IntN(10000)) / 100.0 // 0.00 to 100.00
			name := fmt.Sprintf("Product %03d Rev%d", skuIdx, rng.IntN(20))

			rec := domain.SupplierUpdate{
				SKU:       sku,
				Name:      name,
				Price:     price,
				Status:    status,
				Timestamp: ts,
			}
			records = append(records, rec)

			// Update ground-truth reference model
			simulateUpdate(rec)
		}

		batch := domain.Batch{
			JobID:      "job-combined-chaos",
			BatchIndex: b,
			Records:    records,
		}
		recordedBatches = append(recordedBatches, batch)

		_, err := engine.ExecuteBatch(ctx, batch)
		if err != nil {
			t.Fatalf("ExecuteBatch failed at batch %d: %v", b, err)
		}
	}

	// 1. Verify DB matches Reference Catalog 100% across all SKUs
	if len(refCatalog) == 0 {
		t.Fatalf("reference catalog is empty")
	}

	for sku, expected := range refCatalog {
		p, err := store.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("failed retrieving product %s: %v", sku, err)
		}
		if p == nil {
			t.Fatalf("product %s missing in DB!", sku)
		}

		if !p.SupplierTimestamp.Equal(expected.SupplierTimestamp) {
			t.Errorf("SKU %s timestamp mismatch: got %v, want %v", sku, p.SupplierTimestamp, expected.SupplierTimestamp)
		}
		if p.Name != expected.Name {
			t.Errorf("SKU %s name mismatch: got %q, want %q", sku, p.Name, expected.Name)
		}
		if math.Abs(p.Price-expected.Price) > 1e-6 {
			t.Errorf("SKU %s price mismatch: got %f, want %f", sku, p.Price, expected.Price)
		}
		if p.Status != expected.Status {
			t.Errorf("SKU %s status mismatch: got %s, want %s", sku, p.Status, expected.Status)
		}
	}

	// 2. Snapshot catalog state checksum
	checksumBeforeReplay, countBeforeReplay := computeCatalogChecksum(t, store)
	if countBeforeReplay != len(refCatalog) {
		t.Errorf("catalog row count %d != reference catalog %d", countBeforeReplay, len(refCatalog))
	}

	// 3. Replay all recorded batches in sequence
	for b, batch := range recordedBatches {
		res, err := engine.ExecuteBatch(ctx, batch)
		if err != nil {
			t.Fatalf("replay batch %d failed: %v", b, err)
		}
		if res.Added != 0 || res.Updated != 0 || res.Withdrawn != 0 {
			t.Fatalf("replay of batch %d produced mutation: %+v", b, res)
		}
		if res.Skipped != len(batch.Records) {
			t.Fatalf("replay of batch %d expected Skipped=%d, got %d", b, len(batch.Records), res.Skipped)
		}
	}

	// 4. Verify catalog checksum after replay is 100% invariant
	checksumAfterReplay, countAfterReplay := computeCatalogChecksum(t, store)
	if countBeforeReplay != countAfterReplay {
		t.Errorf("product count drifted on replay: before=%d, after=%d", countBeforeReplay, countAfterReplay)
	}
	if checksumBeforeReplay != checksumAfterReplay {
		t.Errorf("catalog state drifted on full-stream replay!\nBefore: %s\nAfter:  %s", checksumBeforeReplay, checksumAfterReplay)
	}
}

// Ensure sort import is referenced if unused
var _ = sort.Strings
