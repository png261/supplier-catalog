package sync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// computeIndependentDBCatalogChecksum calculates an independent SHA-256 hash over all products in SQLite
// using a raw SQL query, serving as an external oracle against CalculateCatalogChecksum and PreviewPatch.
func computeIndependentDBCatalogChecksum(t *testing.T, store *storage.SQLiteStorage) (string, int) {
	t.Helper()
	rows, err := store.DB().Query(`
		SELECT sku, name, printf('%.8f', price), status, supplier_timestamp, updated_at
		FROM products ORDER BY sku ASC;
	`)
	if err != nil {
		t.Fatalf("independent query failed: %v", err)
	}
	defer rows.Close()

	hasher := sha256.New()
	count := 0
	for rows.Next() {
		count++
		var sku, name, priceStr, status, suppTime, updTime string
		if err := rows.Scan(&sku, &name, &priceStr, &status, &suppTime, &updTime); err != nil {
			t.Fatalf("independent row scan failed: %v", err)
		}
		rowStr := fmt.Sprintf("%s|%s|%s|%s|%s|%s\n", sku, name, priceStr, status, suppTime, updTime)
		hasher.Write([]byte(rowStr))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("independent rows iteration error: %v", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), count
}

// -----------------------------------------------------------------------------
// CHALLENGE 1: CHECKSUM PARITY VERIFICATION ACROSS 500+ ITEMS
// -----------------------------------------------------------------------------

// TestChallenger_Patch_ChecksumParity_500PlusMixedItems tests checksum parity across a
// 600-item patch comprising mixed adds, updates, withdrawals, and skips against a pre-seeded
// catalog. It asserts that result.CatalogChecksum from PreviewPatch matches the actual
// database SHA-256 checksum after PublishPatch bit-for-bit.
func TestChallenger_Patch_ChecksumParity_500PlusMixedItems(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	baseTime := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	// 1. Pre-seed catalog with 500 baseline items
	// - 000..149: will be updated
	// - 150..299: will be withdrawn
	// - 300..449: will be skipped (identical in patch)
	// - 450..499: untouched (not in patch)
	const initialCatalogSize = 500
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	for i := 0; i < initialCatalogSize; i++ {
		sku := fmt.Sprintf("SKU-SEED-%04d", i)
		name := fmt.Sprintf("Baseline Product %04d", i)
		price := 10.00 + float64(i)*0.25
		status := domain.StatusActive
		prod := &domain.Product{
			SKU:               sku,
			Name:              name,
			Price:             price,
			Status:            status,
			SupplierTimestamp: baseTime,
			UpdatedAt:         baseTime,
		}
		if err := store.UpsertProductTx(ctx, tx, prod); err != nil {
			_ = tx.Rollback()
			t.Fatalf("failed seeding product %s: %v", sku, err)
		}
	}

	initVer := &domain.CatalogVersion{
		VersionID:          "ver-seed-baseline-500",
		VersionNumber:      1,
		Description:        "Seed baseline 500 items",
		Author:             "challenger@coderpush.com",
		Type:               domain.VersionTypeInitial,
		TotalItemsAffected: initialCatalogSize,
		Summary:            domain.ChangeSummary{Added: initialCatalogSize},
		CreatedAt:          baseTime,
	}
	var initDeltas []domain.ItemDelta
	for i := 0; i < initialCatalogSize; i++ {
		sku := fmt.Sprintf("SKU-SEED-%04d", i)
		initDeltas = append(initDeltas, domain.ItemDelta{
			VersionID:    "ver-seed-baseline-500",
			SKU:          sku,
			Action:       domain.DeltaActionAdded,
			NewName:      fmt.Sprintf("Baseline Product %04d", i),
			NewPrice:     10.00 + float64(i)*0.25,
			NewStatus:    domain.StatusActive,
			NewTimestamp: baseTime,
		})
	}
	if err := store.CreateVersionTx(ctx, tx, initVer, initDeltas); err != nil {
		_ = tx.Rollback()
		t.Fatalf("failed creating initial version: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed committing seed tx: %v", err)
	}

	initialChecksum, initialCount := computeIndependentDBCatalogChecksum(t, store)
	if initialCount != initialCatalogSize {
		t.Fatalf("expected %d initial items, got %d", initialCatalogSize, initialCount)
	}

	// 2. Construct 600-item patch (> 500 items):
	// - 150 Adds: SKU-ADD-0000 .. SKU-ADD-0149
	// - 150 Updates: SKU-SEED-0000 .. SKU-SEED-0149 (price change + name update)
	// - 150 Withdrawals: SKU-SEED-0150 .. SKU-SEED-0299 (status -> WITHDRAWN)
	// - 150 Skips: SKU-SEED-0300 .. SKU-SEED-0449 (identical name, price, status, timestamp)
	const (
		numAdds        = 150
		numUpdates     = 150
		numWithdrawals = 150
		numSkips       = 150
		totalPatchSize = numAdds + numUpdates + numWithdrawals + numSkips // 600 items
	)

	patchTime := baseTime.Add(2 * time.Hour)
	var patchItems []domain.PatchItem

	// 150 Adds
	for i := 0; i < numAdds; i++ {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-ADD-%04d", i),
			Name:      fmt.Sprintf("Added Item %04d", i),
			Price:     99.50 + float64(i)*0.10,
			Status:    domain.StatusActive,
			Timestamp: &patchTime,
		})
	}

	// 150 Updates
	for i := 0; i < numUpdates; i++ {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-SEED-%04d", i),
			Name:      fmt.Sprintf("Updated Baseline %04d", i),
			Price:     200.00 + float64(i)*1.50,
			Status:    domain.StatusActive,
			Timestamp: &patchTime,
		})
	}

	// 150 Withdrawals
	for i := 0; i < numWithdrawals; i++ {
		origIdx := 150 + i
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-SEED-%04d", origIdx),
			Name:      fmt.Sprintf("Baseline Product %04d", origIdx),
			Price:     10.00 + float64(origIdx)*0.25,
			Status:    domain.StatusWithdrawn,
			Timestamp: &patchTime,
		})
	}

	// 150 Skips (identical in name, price, status, and timestamp)
	for i := 0; i < numSkips; i++ {
		origIdx := 300 + i
		skipTime := baseTime
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-SEED-%04d", origIdx),
			Name:      fmt.Sprintf("Baseline Product %04d", origIdx),
			Price:     10.00 + float64(origIdx)*0.25,
			Status:    domain.StatusActive,
			Timestamp: &skipTime,
		})
	}

	if len(patchItems) != totalPatchSize {
		t.Fatalf("expected %d patch items, got %d", totalPatchSize, len(patchItems))
	}

	patch := domain.CatalogPatch{
		PatchID:     "patch-challenge-parity-600",
		Description: "Milestone 3 Empirical Checksum Parity Verification 600 items",
		Author:      "challenger-parity@coderpush.com",
		Items:       patchItems,
	}

	patchEngine := catalogSync.NewPatchEngine(store, catalogSync.WithPatchTimeFunc(func() time.Time { return patchTime }))

	// 3. Execute PreviewPatch
	previewResult, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// Verify preview invariants and classifications
	if previewResult == nil {
		t.Fatal("expected non-nil previewResult")
	}
	if !previewResult.CanCommit {
		t.Fatalf("expected CanCommit == true, but got false (dead letters: %d)", previewResult.DeadLetterCount)
	}
	if previewResult.TotalItems != totalPatchSize {
		t.Errorf("expected TotalItems %d, got %d", totalPatchSize, previewResult.TotalItems)
	}
	if previewResult.AddedCount != numAdds {
		t.Errorf("expected AddedCount %d, got %d", numAdds, previewResult.AddedCount)
	}
	if previewResult.UpdatedCount != numUpdates {
		t.Errorf("expected UpdatedCount %d, got %d", numUpdates, previewResult.UpdatedCount)
	}
	if previewResult.WithdrawnCount != numWithdrawals {
		t.Errorf("expected WithdrawnCount %d, got %d", numWithdrawals, previewResult.WithdrawnCount)
	}
	if previewResult.SkippedCount != numSkips {
		t.Errorf("expected SkippedCount %d, got %d", numSkips, previewResult.SkippedCount)
	}
	if previewResult.DeadLetterCount != 0 {
		t.Errorf("expected DeadLetterCount 0, got %d", previewResult.DeadLetterCount)
	}
	expectedDeltas := numAdds + numUpdates + numWithdrawals // 450 deltas (skips emit 0 deltas)
	if len(previewResult.Deltas) != expectedDeltas {
		t.Fatalf("expected %d deltas, got %d", expectedDeltas, len(previewResult.Deltas))
	}
	if len(previewResult.CatalogChecksum) != 64 {
		t.Fatalf("invalid CatalogChecksum length: %q", previewResult.CatalogChecksum)
	}

	// Verify DB was NOT mutated during preview
	dbChecksumDuringPreview, dbCountDuringPreview := computeIndependentDBCatalogChecksum(t, store)
	if dbChecksumDuringPreview != initialChecksum || dbCountDuringPreview != initialCount {
		t.Fatalf("IMMUTABILITY BREACH: DB mutated during PreviewPatch!\nBefore: %s\nDuring: %s",
			initialChecksum, dbChecksumDuringPreview)
	}

	// 4. Execute PublishPatch
	publishedVer, publishedDeltas, err := patchEngine.PublishPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PublishPatch failed: %v", err)
	}
	if publishedVer == nil {
		t.Fatal("expected non-nil published CatalogVersion")
	}
	if len(publishedDeltas) != expectedDeltas {
		t.Fatalf("expected %d published deltas, got %d", expectedDeltas, len(publishedDeltas))
	}
	if publishedVer.TotalItemsAffected != expectedDeltas {
		t.Errorf("expected TotalItemsAffected %d, got %d", expectedDeltas, publishedVer.TotalItemsAffected)
	}

	// 5. Query actual database SHA-256 checksum after PublishPatch
	// Verify both via storage query and via our independent raw SQL oracle
	actualDBChecksumFromHelper, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
	if err != nil {
		t.Fatalf("CalculateCatalogChecksumFromDB failed: %v", err)
	}
	actualDBChecksumIndependent, actualTotalProducts := computeIndependentDBCatalogChecksum(t, store)

	// Ensure the two DB calculation methods agree 100%
	if actualDBChecksumFromHelper != actualDBChecksumIndependent {
		t.Fatalf("DB checksum oracle mismatch:\nHelper:      %s\nIndependent: %s",
			actualDBChecksumFromHelper, actualDBChecksumIndependent)
	}

	// Expected total products in DB = 500 initial + 150 adds = 650
	expectedTotalProducts := initialCatalogSize + numAdds
	if actualTotalProducts != expectedTotalProducts {
		t.Fatalf("expected %d total products in DB, got %d", expectedTotalProducts, actualTotalProducts)
	}

	// 6. CRITICAL BIT-FOR-BIT ASSERTION:
	// Projected Checksum from PreviewPatch MUST MATCH Actual DB Checksum from PublishPatch
	if previewResult.CatalogChecksum != actualDBChecksumIndependent {
		t.Fatalf("EMPIRICAL FAILURE: Checksum parity mismatch!\nProjected (PreviewPatch): %s\nActual DB (PublishPatch): %s",
			previewResult.CatalogChecksum, actualDBChecksumIndependent)
	}

	t.Logf("SUCCESS: Checksum parity confirmed bit-for-bit across %d items!\nChecksum: %s\nTotal products in catalog: %d",
		totalPatchSize, previewResult.CatalogChecksum, actualTotalProducts)

	// 7. Verify multi-generation follow-up patch (2nd generation with 500 items)
	// Testing that checksum parity remains deterministic across sequential patch versions
	gen2Time := patchTime.Add(time.Hour)
	var gen2Items []domain.PatchItem
	for i := 0; i < 250; i++ {
		gen2Items = append(gen2Items, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-ADD-%04d", i),
			Name:      fmt.Sprintf("Added Item %04d Rev2", i),
			Price:     150.00 + float64(i),
			Status:    domain.StatusActive,
			Timestamp: &gen2Time,
		})
	}
	for i := 0; i < 250; i++ {
		gen2Items = append(gen2Items, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-GEN2-%04d", i),
			Name:      fmt.Sprintf("Gen2 Item %04d", i),
			Price:     300.00 + float64(i),
			Status:    domain.StatusActive,
			Timestamp: &gen2Time,
		})
	}

	gen2Patch := domain.CatalogPatch{
		PatchID:     "patch-challenge-parity-gen2-500",
		Description: "Generation 2 follow-up patch 500 items",
		Author:      "challenger-parity@coderpush.com",
		Items:       gen2Items,
	}

	gen2Engine := catalogSync.NewPatchEngine(store, catalogSync.WithPatchTimeFunc(func() time.Time { return gen2Time }))
	gen2Preview, err := gen2Engine.PreviewPatch(ctx, gen2Patch)
	if err != nil {
		t.Fatalf("Gen2 PreviewPatch failed: %v", err)
	}
	if !gen2Preview.CanCommit {
		t.Fatalf("Gen2 CanCommit == false: %d dead letters", gen2Preview.DeadLetterCount)
	}

	_, _, err = gen2Engine.PublishPatch(ctx, gen2Patch)
	if err != nil {
		t.Fatalf("Gen2 PublishPatch failed: %v", err)
	}

	actualGen2Checksum, actualGen2Count := computeIndependentDBCatalogChecksum(t, store)
	expectedGen2TotalProducts := expectedTotalProducts + 100 + 250 // 100 new SKU-ADD + 250 SKU-GEN2 = 1000
	if actualGen2Count != expectedGen2TotalProducts {
		t.Fatalf("expected %d products after gen2, got %d", expectedGen2TotalProducts, actualGen2Count)
	}

	if gen2Preview.CatalogChecksum != actualGen2Checksum {
		t.Fatalf("EMPIRICAL FAILURE: Gen2 Checksum parity mismatch!\nProjected (PreviewPatch): %s\nActual DB (PublishPatch): %s",
			gen2Preview.CatalogChecksum, actualGen2Checksum)
	}

	t.Logf("SUCCESS: Gen2 Checksum parity confirmed bit-for-bit: %s (Total items: %d)",
		actualGen2Checksum, actualGen2Count)
}

// -----------------------------------------------------------------------------
// CHALLENGE 2: HIGH-CONTENTION RACE TEST (20 GOROUTINES UNDER GO TEST -RACE)
// -----------------------------------------------------------------------------

// TestChallenger_Patch_HighContentionRace_20Goroutines executes 20 concurrent
// goroutines calling PreviewPatch simultaneously using a synchronized barrier,
// along with concurrent background readers and checksum verifiers.
// Asserts 0 data races, 0 deadlocks, 0 memory corruption, and zero DB mutations.
func TestChallenger_Patch_HighContentionRace_20Goroutines(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	baseTime := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

	// Pre-seed catalog with 100 products
	const seedCount = 100
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	for i := 0; i < seedCount; i++ {
		sku := fmt.Sprintf("SKU-RACE-%03d", i)
		_ = store.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               sku,
			Name:              fmt.Sprintf("Race Item %03d", i),
			Price:             10.00 + float64(i),
			Status:            domain.StatusActive,
			SupplierTimestamp: baseTime,
			UpdatedAt:         baseTime,
		})
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	initialChecksum, initialCount := computeIndependentDBCatalogChecksum(t, store)
	if initialCount != seedCount {
		t.Fatalf("expected %d initial items, got %d", seedCount, initialCount)
	}

	patchEngine := catalogSync.NewPatchEngine(store)

	const (
		numGoroutines       = 20
		iterationsPerWorker = 30 // 20 * 30 = 600 concurrent PreviewPatch invocations
		numBackgroundRead   = 5
	)

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	errCh := make(chan error, numGoroutines*iterationsPerWorker+numBackgroundRead*iterationsPerWorker)
	var completedPreviewCalls int64

	// Launch 20 concurrent goroutines calling PreviewPatch
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier // Release all 20 goroutines at the exact same instant

			for it := 0; it < iterationsPerWorker; it++ {
				patchFuture := baseTime.Add(time.Duration(it+1) * time.Hour)

				// Construct worker-specific patch with mixed adds, updates, and skips
				items := []domain.PatchItem{
					// Update existing item
					{
						SKU:       fmt.Sprintf("SKU-RACE-%03d", (workerID*5+it)%seedCount),
						Name:      fmt.Sprintf("Worker %d Iter %d Update", workerID, it),
						Price:     75.50 + float64(workerID),
						Status:    domain.StatusActive,
						Timestamp: &patchFuture,
					},
					// Add worker-specific unique item
					{
						SKU:       fmt.Sprintf("SKU-RACE-ADD-W%d-IT%d", workerID, it),
						Name:      fmt.Sprintf("Worker %d Iter %d New", workerID, it),
						Price:     120.00,
						Status:    domain.StatusActive,
						Timestamp: &patchFuture,
					},
					// Skip existing item
					{
						SKU:       fmt.Sprintf("SKU-RACE-%03d", (workerID*3+it+50)%seedCount),
						Name:      fmt.Sprintf("Race Item %03d", (workerID*3+it+50)%seedCount),
						Price:     10.00 + float64((workerID*3+it+50)%seedCount),
						Status:    domain.StatusActive,
						Timestamp: &baseTime,
					},
				}

				patch := domain.CatalogPatch{
					PatchID:     fmt.Sprintf("patch-race-w%d-it%d", workerID, it),
					Description: fmt.Sprintf("High-contention race patch worker %d iter %d", workerID, it),
					Author:      fmt.Sprintf("worker-%d@test.com", workerID),
					Items:       items,
				}

				res, err := patchEngine.PreviewPatch(ctx, patch)
				if err != nil {
					errCh <- fmt.Errorf("worker %d iter %d: %w", workerID, it, err)
					return
				}
				if res == nil {
					errCh <- fmt.Errorf("worker %d iter %d: nil result", workerID, it)
					return
				}
				if len(res.CatalogChecksum) != 64 {
					errCh <- fmt.Errorf("worker %d iter %d: invalid checksum length: %q", workerID, it, res.CatalogChecksum)
					return
				}
				if res.AddedCount != 1 || res.UpdatedCount != 1 || res.SkippedCount != 1 {
					errCh <- fmt.Errorf("worker %d iter %d: unexpected summary counts: %+v", workerID, it, res.Summary)
					return
				}

				atomic.AddInt64(&completedPreviewCalls, 1)
			}
		}(g)
	}

	// Launch concurrent background readers querying SQLite simultaneously
	for r := 0; r < numBackgroundRead; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			<-startBarrier

			for it := 0; it < iterationsPerWorker; it++ {
				sku := fmt.Sprintf("SKU-RACE-%03d", (readerID*17+it)%seedCount)
				prod, err := store.GetProduct(ctx, sku)
				if err != nil {
					errCh <- fmt.Errorf("reader %d failed reading %s: %w", readerID, sku, err)
					return
				}
				if prod == nil {
					errCh <- fmt.Errorf("reader %d found nil product for %s", readerID, sku)
					return
				}
			}
		}(r)
	}

	// Trigger simultaneous execution across all 20 workers and background readers
	close(startBarrier)

	// Wait with timeout to detect any deadlock
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// Completed normally
	case <-time.After(15 * time.Second):
		t.Fatal("DEADLOCK DETECTED: High-contention race test timed out after 15 seconds")
	}

	close(errCh)
	var errorsList []error
	for err := range errCh {
		errorsList = append(errorsList, err)
	}
	if len(errorsList) > 0 {
		t.Fatalf("Encountered %d errors during 20-goroutine high-contention test. First error: %v",
			len(errorsList), errorsList[0])
	}

	totalCompleted := atomic.LoadInt64(&completedPreviewCalls)
	expectedTotalCompleted := int64(numGoroutines * iterationsPerWorker)
	if totalCompleted != expectedTotalCompleted {
		t.Fatalf("expected %d completed preview calls, got %d", expectedTotalCompleted, totalCompleted)
	}

	// Verify ZERO database mutations occurred during all 600 PreviewPatch operations
	finalChecksum, finalCount := computeIndependentDBCatalogChecksum(t, store)
	if initialChecksum != finalChecksum || initialCount != finalCount {
		t.Fatalf("DATABASE CORRUPTED / MUTATED during concurrent PreviewPatch calls!\nInitial: %s (count: %d)\nFinal:   %s (count: %d)",
			initialChecksum, initialCount, finalChecksum, finalCount)
	}

	t.Logf("SUCCESS: High-Contention Race Test passed: 20 goroutines, %d preview operations, 0 races, 0 deadlocks, 0 DB mutations",
		totalCompleted)
}

// -----------------------------------------------------------------------------
// CHALLENGE 3: ADVERSARIAL INVARIANT ATTACK TESTS DURING PREVIEW AND COMMIT GATING
// -----------------------------------------------------------------------------

// TestChallenger_Patch_InvariantAttacks_CommitGated asserts that when patches
// violate Invariant A (Tombstone resurrection), Invariant B (Stale timestamps),
// Invariant C (NaN/Inf prices), Invariant D (Invalid statuses), or Invariant E (Empty SKU),
// PreviewPatch quarantines them with CanCommit == false, and PublishPatch strictly rejects
// them, ensuring zero database corruption.
func TestChallenger_Patch_InvariantAttacks_CommitGated(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	fixedTime := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

	// Seed catalog with 1 active product and 1 withdrawn product
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{
		SKU:               "SKU-ACTIVE",
		Name:              "Active Product",
		Price:             50.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: fixedTime,
		UpdatedAt:         fixedTime,
	})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{
		SKU:               "SKU-TOMBSTONE",
		Name:              "Withdrawn Product",
		Price:             75.00,
		Status:            domain.StatusWithdrawn,
		SupplierTimestamp: fixedTime,
		UpdatedAt:         fixedTime,
	})
	_ = tx.Commit()

	initialChecksum, initialCount := computeIndependentDBCatalogChecksum(t, store)

	patchEngine := catalogSync.NewPatchEngine(store, catalogSync.WithPatchTimeFunc(func() time.Time { return fixedTime.Add(time.Hour) }))

	adversarialCases := []struct {
		name           string
		items          []domain.PatchItem
		expectedReason string
	}{
		{
			name: "Invariant A Attack: Resurrect withdrawn tombstone",
			items: []domain.PatchItem{
				{
					SKU:    "SKU-TOMBSTONE",
					Name:   "Resurrection Attempt",
					Price:  100.00,
					Status: domain.StatusActive, // Illegal resurrection!
				},
			},
			expectedReason: domain.ErrResurrectionRestricted.Error(),
		},
		{
			name: "Invariant B Attack: Stale timestamp earlier than existing",
			items: []domain.PatchItem{
				{
					SKU:       "SKU-ACTIVE",
					Name:      "Stale Update",
					Price:     60.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := fixedTime.Add(-time.Hour); return &t }(), // Stale!
				},
			},
			expectedReason: domain.ErrOutdatedTimestamp.Error(),
		},
		{
			name: "Invariant C Attack 1: Negative price",
			items: []domain.PatchItem{
				{
					SKU:    "SKU-NEW-NEG",
					Name:   "Negative Price",
					Price:  -10.00,
					Status: domain.StatusActive,
				},
			},
			expectedReason: domain.ErrNegativePrice.Error(),
		},
		{
			name: "Invariant C Attack 2: NaN price",
			items: []domain.PatchItem{
				{
					SKU:    "SKU-NEW-NAN",
					Name:   "NaN Price",
					Price:  math.NaN(),
					Status: domain.StatusActive,
				},
			},
			expectedReason: domain.ErrNegativePrice.Error(),
		},
		{
			name: "Invariant C Attack 3: Positive Inf price",
			items: []domain.PatchItem{
				{
					SKU:    "SKU-NEW-INF",
					Name:   "Inf Price",
					Price:  math.Inf(1),
					Status: domain.StatusActive,
				},
			},
			expectedReason: domain.ErrNegativePrice.Error(),
		},
		{
			name: "Invariant D Attack: Invalid status string",
			items: []domain.PatchItem{
				{
					SKU:    "SKU-NEW-STATUS",
					Name:   "Bad Status",
					Price:  25.00,
					Status: domain.ProductStatus("DISCONTINUED_UNKNOWN"),
				},
			},
			expectedReason: domain.ErrInvalidStatus.Error(),
		},
		{
			name: "Invariant E Attack: Empty or whitespace SKU",
			items: []domain.PatchItem{
				{
					SKU:    "   ",
					Name:   "Missing SKU",
					Price:  25.00,
					Status: domain.StatusActive,
				},
			},
			expectedReason: domain.ErrEmptySKU.Error(),
		},
	}

	for _, tc := range adversarialCases {
		t.Run(tc.name, func(t *testing.T) {
			patch := domain.CatalogPatch{
				PatchID:     "patch-attack-test",
				Description: tc.name,
				Author:      "adversary@test.com",
				Items:       tc.items,
			}

			// 1. PreviewPatch must quarantine the attack
			preview, err := patchEngine.PreviewPatch(ctx, patch)
			if err != nil {
				t.Fatalf("PreviewPatch returned unexpected error: %v", err)
			}
			if preview.CanCommit {
				t.Errorf("expected CanCommit == false for attack %q", tc.name)
			}
			if preview.DeadLetterCount == 0 {
				t.Errorf("expected DeadLetterCount > 0 for attack %q", tc.name)
			}
			if len(preview.DeadLetterItems) == 0 {
				t.Fatalf("expected non-empty DeadLetterItems for attack %q", tc.name)
			}
			if !strings.Contains(preview.DeadLetterItems[0].Reason, tc.expectedReason) {
				t.Errorf("expected reason containing %q, got %q", tc.expectedReason, preview.DeadLetterItems[0].Reason)
			}

			// 2. PublishPatch must strictly reject publishing corrupt patch
			ver, deltas, err := patchEngine.PublishPatch(ctx, patch)
			if err == nil {
				t.Errorf("expected PublishPatch to fail for attack %q, but succeeded: ver=%+v deltas=%+v", tc.name, ver, deltas)
			}

			// 3. Verify DB remained completely unmutated
			currentChecksum, currentCount := computeIndependentDBCatalogChecksum(t, store)
			if currentChecksum != initialChecksum || currentCount != initialCount {
				t.Fatalf("DATABASE MUTATED during attack %q!\nInitial: %s\nCurrent: %s",
					tc.name, initialChecksum, currentChecksum)
			}
		})
	}
}
