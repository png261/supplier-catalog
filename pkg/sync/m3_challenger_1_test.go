package sync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// ============================================================================
// Milestone 3 Empirical Challenger 1 Suite
// Mandate:
// 1. Immutability Stress Test: Run PreviewPatch with large batches of valid & invalid
//    items. Measure SQLite DB state before and after (row counts, checksums, SQLite
//    schema/WAL files) and verify bit-for-bit zero mutations.
// 2. Invariant Quarantine Attack: Send adversarial patch items attempting to resurrect
//    tombstoned items, backward timestamps, NaN/Inf/-1 prices, invalid statuses. Verify
//    that 100% of invalid items are quarantined into DeadLetterItems with appropriate
//    domain error reasons.
// ============================================================================

// fileChecksumAndSize computes SHA-256 and byte size of a file on disk.
func fileChecksumAndSize(t *testing.T, filePath string) (string, int64, bool) {
	t.Helper()
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, false
		}
		t.Fatalf("failed stat file %s: %v", filePath, err)
	}

	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("failed opening file %s: %v", filePath, err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		t.Fatalf("failed hashing file %s: %v", filePath, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), info.Size(), true
}

// -----------------------------------------------------------------------------
// MANDATE 1: IMMUTABILITY STRESS TEST (FILE-BACKED WAL + BIT-FOR-BIT VERIFICATION)
// -----------------------------------------------------------------------------

// TestChallenger1_Immutability_FileBackedWAL_BitForBitZeroMutation runs PreviewPatch
// against a real file-backed SQLite database operating in WAL mode.
// It verifies:
// 1. Bit-for-bit SHA-256 and byte-size identity of the .db file.
// 2. Bit-for-bit SHA-256 and byte-size identity of the .db-wal file.
// 3. PRAGMA data_version and PRAGMA schema_version stability.
// 4. Zero mutation across all SQLite table row counts.
// 5. Zero mutation of the full database SHA-256 catalog checksum.
func TestChallenger1_Immutability_FileBackedWAL_BitForBitZeroMutation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/file_immutability.db"
	walPath := dbPath + "-wal"

	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening file storage: %v", err)
	}
	defer store.Close()

	seedTime := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

	// 1. Seed 500 initial products into SQLite
	const seedCount = 500
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	seedDeltas := make([]domain.ItemDelta, 0, seedCount)
	for i := 0; i < seedCount; i++ {
		sku := fmt.Sprintf("SKU-SEED-%04d", i)
		status := domain.StatusActive
		if i%5 == 0 {
			status = domain.StatusWithdrawn // 20% seeded as tombstoned
		}
		p := &domain.Product{
			SKU:               sku,
			Name:              fmt.Sprintf("Original Seed Item %04d", i),
			Price:             10.00 + float64(i)*0.5,
			Status:            status,
			SupplierTimestamp: seedTime,
			UpdatedAt:         seedTime,
		}
		if err := store.UpsertProductTx(ctx, tx, p); err != nil {
			_ = tx.Rollback()
			t.Fatalf("UpsertProductTx failed on %s: %v", sku, err)
		}
		seedDeltas = append(seedDeltas, domain.ItemDelta{
			VersionID:    "ver-seed-immutability",
			SKU:          sku,
			Action:       domain.DeltaActionAdded,
			NewName:      p.Name,
			NewPrice:     p.Price,
			NewStatus:    p.Status,
			NewTimestamp: seedTime,
		})
	}

	seedVersion := &domain.CatalogVersion{
		VersionID:          "ver-seed-immutability",
		VersionNumber:      1,
		Description:        "Initial seed version for immutability challenge",
		Author:             "challenger@coderpush.com",
		Type:               domain.VersionTypeInitial,
		TotalItemsAffected: seedCount,
		Summary:            domain.ChangeSummary{Added: seedCount},
		CreatedAt:          seedTime,
	}
	if err := store.CreateVersionTx(ctx, tx, seedVersion, seedDeltas); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit seed tx failed: %v", err)
	}

	// 2. Force WAL checkpoint to flush all pending pages into the main .db file
	if _, err := store.DB().ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		t.Fatalf("wal_checkpoint failed: %v", err)
	}

	// 3. Snapshot BEFORE state: file hash, size, WAL hash, pragma data_version, schema_version, table counts
	dbHashBefore, dbSizeBefore, dbExists := fileChecksumAndSize(t, dbPath)
	if !dbExists {
		t.Fatalf("main database file missing: %s", dbPath)
	}
	walHashBefore, walSizeBefore, _ := fileChecksumAndSize(t, walPath)

	var dataVerBefore, schemaVerBefore int
	if err := store.DB().QueryRowContext(ctx, "PRAGMA data_version;").Scan(&dataVerBefore); err != nil {
		t.Fatalf("failed reading data_version: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, "PRAGMA schema_version;").Scan(&schemaVerBefore); err != nil {
		t.Fatalf("failed reading schema_version: %v", err)
	}

	catalogChecksumBefore, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
	if err != nil {
		t.Fatalf("failed computing initial catalog checksum: %v", err)
	}
	pCntB, vCntB, dCntB, dlCntB := computeTableCounts(t, store)

	// 4. Construct large adversarial patch of 2,500 items mixing valid and invalid operations
	futureTime := seedTime.Add(2 * time.Hour)
	pastTime := seedTime.Add(-2 * time.Hour)

	patchItems := make([]domain.PatchItem, 0, 2500)

	// Category 1: 500 valid updates on existing active items
	for i := 1; i < 500; i += 2 {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-SEED-%04d", i),
			Name:      fmt.Sprintf("Revised Item %04d", i),
			Price:     99.99,
			Status:    domain.StatusActive,
			Timestamp: &futureTime,
		})
	}

	// Category 2: 500 brand new additions
	for i := 0; i < 500; i++ {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-NEW-%04d", i),
			Name:      fmt.Sprintf("Brand New Product %04d", i),
			Price:     49.95,
			Status:    domain.StatusActive,
			Timestamp: &futureTime,
		})
	}

	// Category 3: 100 valid withdrawals (from active to withdrawn)
	for i := 2; i < 102; i += 2 {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-SEED-%04d", i),
			Name:      fmt.Sprintf("Original Seed Item %04d", i),
			Price:     10.00 + float64(i)*0.5,
			Status:    domain.StatusWithdrawn,
			Timestamp: &futureTime,
		})
	}

	// Category 4: 100 skipped identical updates
	for i := 102; i < 202; i += 2 {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-SEED-%04d", i),
			Name:      fmt.Sprintf("Original Seed Item %04d", i),
			Price:     10.00 + float64(i)*0.5,
			Status:    domain.StatusActive,
			Timestamp: &seedTime, // Identical timestamp and values
		})
	}

	// Category 5: 100 adversarial tombstone resurrection attacks (seeded as WITHDRAWN, attempting ACTIVE)
	for i := 0; i < 500; i += 5 {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-SEED-%04d", i),
			Name:      fmt.Sprintf("Resurrected Item %04d", i),
			Price:     150.00,
			Status:    domain.StatusActive, // VIOLATION: Invariant A
			Timestamp: &futureTime,
		})
	}

	// Category 6: 100 backward / stale timestamp attacks
	for i := 1; i < 101; i++ {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-SEED-%04d", i),
			Name:      fmt.Sprintf("Stale Item %04d", i),
			Price:     1.99,
			Status:    domain.StatusActive,
			Timestamp: &pastTime, // VIOLATION: Invariant B
		})
	}

	// Category 7: 100 extreme invalid prices (NaN, +Inf, -Inf, -1.0)
	for i := 0; i < 100; i++ {
		var badPrice float64
		switch i % 4 {
		case 0:
			badPrice = math.NaN()
		case 1:
			badPrice = math.Inf(1)
		case 2:
			badPrice = math.Inf(-1)
		case 3:
			badPrice = -1.00 - float64(i)
		}
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-BAD-PRICE-%04d", i),
			Name:      "Bad Price Item",
			Price:     badPrice, // VIOLATION: Invariant C
			Status:    domain.StatusActive,
			Timestamp: &futureTime,
		})
	}

	// Category 8: 100 invalid statuses
	for i := 0; i < 100; i++ {
		patchItems = append(patchItems, domain.PatchItem{
			SKU:       fmt.Sprintf("SKU-BAD-STAT-%04d", i),
			Name:      "Bad Status Item",
			Price:     25.00,
			Status:    domain.ProductStatus(fmt.Sprintf("INVALID_STATUS_%d", i)), // VIOLATION: Invariant D
			Timestamp: &futureTime,
		})
	}

	adversarialPatch := domain.CatalogPatch{
		PatchID:     "patch-immutability-stress-large",
		Description: "Large mixed valid and adversarial patch",
		Author:      "stress-tester@coderpush.com",
		Items:       patchItems,
	}

	// 5. Execute PreviewPatch 5 times consecutively
	engine := catalogSync.NewPatchEngine(store, catalogSync.WithPatchTimeFunc(func() time.Time { return futureTime }))

	for iter := 1; iter <= 5; iter++ {
		res, err := engine.PreviewPatch(ctx, adversarialPatch)
		if err != nil {
			t.Fatalf("iter %d: PreviewPatch failed: %v", iter, err)
		}
		if res == nil {
			t.Fatalf("iter %d: expected non-nil result", iter)
		}
		if res.CanCommit {
			t.Errorf("iter %d: expected CanCommit == false due to adversarial items", iter)
		}
		if res.DeadLetterCount == 0 {
			t.Errorf("iter %d: expected non-zero DeadLetterCount", iter)
		}

		// 6. Assert Bit-For-Bit Zero Database Mutations after preview
		dbHashAfter, dbSizeAfter, _ := fileChecksumAndSize(t, dbPath)
		walHashAfter, walSizeAfter, _ := fileChecksumAndSize(t, walPath)

		if dbHashBefore != dbHashAfter {
			t.Fatalf("iter %d: CRITICAL IMMUTABILITY VIOLATION: .db file SHA-256 mutated!\nBefore: %s\nAfter:  %s",
				iter, dbHashBefore, dbHashAfter)
		}
		if dbSizeBefore != dbSizeAfter {
			t.Fatalf("iter %d: CRITICAL IMMUTABILITY VIOLATION: .db file size changed from %d to %d bytes",
				iter, dbSizeBefore, dbSizeAfter)
		}
		if walSizeBefore != walSizeAfter || walHashBefore != walHashAfter {
			t.Fatalf("iter %d: CRITICAL IMMUTABILITY VIOLATION: .db-wal file mutated!\nBefore: %s (size %d)\nAfter:  %s (size %d)",
				iter, walHashBefore, walSizeBefore, walHashAfter, walSizeAfter)
		}

		var dataVerAfter, schemaVerAfter int
		_ = store.DB().QueryRowContext(ctx, "PRAGMA data_version;").Scan(&dataVerAfter)
		_ = store.DB().QueryRowContext(ctx, "PRAGMA schema_version;").Scan(&schemaVerAfter)

		if dataVerBefore != dataVerAfter {
			t.Fatalf("iter %d: PRAGMA data_version drifted: before=%d, after=%d", iter, dataVerBefore, dataVerAfter)
		}
		if schemaVerBefore != schemaVerAfter {
			t.Fatalf("iter %d: PRAGMA schema_version drifted: before=%d, after=%d", iter, schemaVerBefore, schemaVerAfter)
		}

		pCntA, vCntA, dCntA, dlCntA := computeTableCounts(t, store)
		if pCntB != pCntA || vCntB != vCntA || dCntB != dCntA || dlCntB != dlCntA {
			t.Fatalf("iter %d: table counts drifted: products (%d->%d), versions (%d->%d), deltas (%d->%d), deadLetters (%d->%d)",
				iter, pCntB, pCntA, vCntB, vCntA, dCntB, dCntA, dlCntB, dlCntA)
		}

		catalogChecksumAfter, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
		if err != nil {
			t.Fatalf("iter %d: failed computing post-preview checksum: %v", iter, err)
		}
		if catalogChecksumBefore != catalogChecksumAfter {
			t.Fatalf("iter %d: catalog SHA-256 checksum drifted!\nBefore: %s\nAfter:  %s",
				iter, catalogChecksumBefore, catalogChecksumAfter)
		}
	}
}

// -----------------------------------------------------------------------------
// MANDATE 2: INVARIANT QUARANTINE ATTACK (TOMBSTONES, TIMESTAMPS, PRICES, STATUSES)
// -----------------------------------------------------------------------------

// TestChallenger1_InvariantQuarantine_TombstoneResurrectionAttacks rigorously tests
// Invariant A across pre-existing DB tombstones, intra-patch tombstones, chained
// resurrections, and legal metadata updates on WITHDRAWN items.
func TestChallenger1_InvariantQuarantine_TombstoneResurrectionAttacks(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	t1 := now.Add(1 * time.Hour)
	t2 := now.Add(2 * time.Hour)
	t3 := now.Add(3 * time.Hour)
	t4 := now.Add(4 * time.Hour)

	// Seed 3 products: 2 active, 1 tombstoned
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-TOMB-DB-001", Name: "Seeded Tombstone 1", Price: 10.0, Status: domain.StatusWithdrawn, SupplierTimestamp: now, UpdatedAt: now})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-TOMB-DB-002", Name: "Seeded Tombstone 2", Price: 20.0, Status: domain.StatusWithdrawn, SupplierTimestamp: now, UpdatedAt: now})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-ACT-001", Name: "Seeded Active 1", Price: 30.0, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now})
	_ = tx.Commit()

	engine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Multi-vector tombstone attack",
		Author:      "adversary@coderpush.com",
		Items: []domain.PatchItem{
			// Attack Vector 1: Attempt to resurrect seeded tombstone 1 (DB WITHDRAWN -> ACTIVE)
			{SKU: "SKU-TOMB-DB-001", Name: "Resurrect Attempt", Price: 15.0, Status: domain.StatusActive, Timestamp: &t1},

			// Attack Vector 2: Attempt to resurrect seeded tombstone 2 twice in same patch
			{SKU: "SKU-TOMB-DB-002", Name: "Resurrect Attempt A", Price: 25.0, Status: domain.StatusActive, Timestamp: &t1},
			{SKU: "SKU-TOMB-DB-002", Name: "Resurrect Attempt B", Price: 28.0, Status: domain.StatusActive, Timestamp: &t2},

			// Attack Vector 3: Intra-patch creation as WITHDRAWN, followed by resurrection attempt
			{SKU: "SKU-INTRA-INIT-WITHDRAWN", Name: "Born Dead", Price: 50.0, Status: domain.StatusWithdrawn, Timestamp: &t1},
			{SKU: "SKU-INTRA-INIT-WITHDRAWN", Name: "Resurrect Born Dead", Price: 55.0, Status: domain.StatusActive, Timestamp: &t2},

			// Attack Vector 4: Active item withdrawn intra-patch, then resurrected intra-patch
			{SKU: "SKU-ACT-001", Name: "Seeded Active 1", Price: 30.0, Status: domain.StatusWithdrawn, Timestamp: &t1},
			{SKU: "SKU-ACT-001", Name: "Resurrect Active 1", Price: 35.0, Status: domain.StatusActive, Timestamp: &t2},

			// Control Vector 5: LEGAL metadata update on WITHDRAWN item (status remains WITHDRAWN)
			{SKU: "SKU-TOMB-DB-001", Name: "Historical Name Fix", Price: 12.0, Status: domain.StatusWithdrawn, Timestamp: &t3},

			// Control Vector 6: Intra-patch newly created item withdrawn legally, then metadata updated while remaining WITHDRAWN
			{SKU: "SKU-INTRA-LEG-WITHDRAWN", Name: "Item A", Price: 40.0, Status: domain.StatusActive, Timestamp: &t1},
			{SKU: "SKU-INTRA-LEG-WITHDRAWN", Name: "Item A", Price: 40.0, Status: domain.StatusWithdrawn, Timestamp: &t2},
			{SKU: "SKU-INTRA-LEG-WITHDRAWN", Name: "Item A Renamed", Price: 42.0, Status: domain.StatusWithdrawn, Timestamp: &t4},
		},
	}

	res, err := engine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// 5 illegal resurrection items must be quarantined:
	// - SKU-TOMB-DB-001 (item 0)
	// - SKU-TOMB-DB-002 (item 1)
	// - SKU-TOMB-DB-002 (item 2)
	// - SKU-INTRA-INIT-WITHDRAWN (item 4)
	// - SKU-ACT-001 (item 6)
	if res.Summary.DeadLetter != 5 || res.DeadLetterCount != 5 {
		t.Errorf("expected 5 dead letters for resurrection violations, got summary=%d, count=%d",
			res.Summary.DeadLetter, res.DeadLetterCount)
	}
	if res.CanCommit {
		t.Errorf("expected CanCommit == false")
	}

	// Verify all quarantined items have Invariant A domain error reason
	for _, dl := range res.DeadLetterItems {
		if !strings.Contains(dl.Reason, "tombstone") && !strings.Contains(dl.Reason, "WITHDRAWN") {
			t.Errorf("unexpected dead-letter reason for %s: %q (expected tombstone invariant reason)", dl.SKU, dl.Reason)
		}
	}

	// Verify that legal metadata updates on WITHDRAWN products succeeded
	// Deltas should contain:
	// 1. SKU-INTRA-INIT-WITHDRAWN (Added, WITHDRAWN)
	// 2. SKU-ACT-001 (Withdrawn)
	// 3. SKU-TOMB-DB-001 (Updated, status WITHDRAWN, price 12.0)
	// 4. SKU-INTRA-LEG-WITHDRAWN (Added)
	// 5. SKU-INTRA-LEG-WITHDRAWN (Withdrawn)
	// 6. SKU-INTRA-LEG-WITHDRAWN (Updated, status WITHDRAWN)
	var foundLegalTombUpdate bool
	for _, d := range res.Deltas {
		if d.SKU == "SKU-TOMB-DB-001" && d.Action == domain.DeltaActionUpdated && d.NewStatus == domain.StatusWithdrawn && d.NewPrice == 12.0 {
			foundLegalTombUpdate = true
			break
		}
	}
	if !foundLegalTombUpdate {
		t.Errorf("expected legal metadata update on SKU-TOMB-DB-001 to produce delta with status WITHDRAWN")
	}
}

// TestChallenger1_InvariantQuarantine_BackwardAndStaleTimestamps verifies that
// 100% of incoming items with timestamp <= existing.SupplierTimestamp (and changed values)
// are quarantined into DeadLetterItems with ErrOutdatedTimestamp.
func TestChallenger1_InvariantQuarantine_BackwardAndStaleTimestamps(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	tBase := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	tOlder1Sec := tBase.Add(-1 * time.Second)
	tOlder1Hour := tBase.Add(-1 * time.Hour)
	tOlder1Year := tBase.Add(-365 * 24 * time.Hour)
	tEqual := tBase
	tNewer := tBase.Add(1 * time.Hour)

	// Seed catalog
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-TIME-001", Name: "Item 1", Price: 100.0, Status: domain.StatusActive, SupplierTimestamp: tBase, UpdatedAt: tBase})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-TIME-002", Name: "Item 2", Price: 200.0, Status: domain.StatusActive, SupplierTimestamp: tBase, UpdatedAt: tBase})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-TIME-003", Name: "Item 3", Price: 300.0, Status: domain.StatusActive, SupplierTimestamp: tBase, UpdatedAt: tBase})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-TIME-004", Name: "Item 4", Price: 400.0, Status: domain.StatusActive, SupplierTimestamp: tBase, UpdatedAt: tBase})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-TIME-005", Name: "Item 5", Price: 500.0, Status: domain.StatusActive, SupplierTimestamp: tBase, UpdatedAt: tBase})
	_ = tx.Commit()

	engine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Timestamp attack suite",
		Author:      "adversary@coderpush.com",
		Items: []domain.PatchItem{
			// Vector 1: 1 second backward
			{SKU: "SKU-TIME-001", Name: "Item 1 Stale", Price: 105.0, Status: domain.StatusActive, Timestamp: &tOlder1Sec},
			// Vector 2: 1 hour backward
			{SKU: "SKU-TIME-002", Name: "Item 2 Stale", Price: 205.0, Status: domain.StatusActive, Timestamp: &tOlder1Hour},
			// Vector 3: 1 year backward
			{SKU: "SKU-TIME-003", Name: "Item 3 Stale", Price: 305.0, Status: domain.StatusActive, Timestamp: &tOlder1Year},
			// Vector 4: Equal timestamp, changed price
			{SKU: "SKU-TIME-004", Name: "Item 4 Equal Ts", Price: 405.0, Status: domain.StatusActive, Timestamp: &tEqual},
			// Vector 5: Equal timestamp, identical values -> MUST BE SKIPPED (not quarantined)
			{SKU: "SKU-TIME-005", Name: "Item 5", Price: 500.0, Status: domain.StatusActive, Timestamp: &tEqual},
			// Vector 6: Intra-patch stale timestamp (Item A at tNewer, followed by Item A at tBase)
			{SKU: "SKU-TIME-NEW", Name: "New Item V1", Price: 50.0, Status: domain.StatusActive, Timestamp: &tNewer},
			{SKU: "SKU-TIME-NEW", Name: "New Item V2 Older", Price: 55.0, Status: domain.StatusActive, Timestamp: &tBase},
		},
	}

	res, err := engine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// Vectors 1, 2, 3, 4, 6 must be quarantined (5 items)
	// Vector 5 must be skipped (1 item)
	// Vector 6 item 1 must be added (1 item)
	if res.Summary.DeadLetter != 5 || res.DeadLetterCount != 5 {
		t.Errorf("expected 5 dead letters for stale timestamps, got summary=%d, count=%d",
			res.Summary.DeadLetter, res.DeadLetterCount)
	}
	if res.Summary.Skipped != 1 || res.SkippedCount != 1 {
		t.Errorf("expected 1 skipped item for identical record, got summary=%d, count=%d",
			res.Summary.Skipped, res.SkippedCount)
	}
	if res.Summary.Added != 1 || res.AddedCount != 1 {
		t.Errorf("expected 1 added item for initial intra-patch item, got summary=%d, count=%d",
			res.Summary.Added, res.AddedCount)
	}

	// Verify all quarantined items have ErrOutdatedTimestamp reason
	expectedReason := domain.ErrOutdatedTimestamp.Error()
	for _, dl := range res.DeadLetterItems {
		if dl.Reason != expectedReason {
			t.Errorf("expected reason %q, got %q for SKU %s", expectedReason, dl.Reason, dl.SKU)
		}
	}
}

// TestChallenger1_InvariantQuarantine_ExtremePrices tests NaN, +Inf, -Inf,
// negative prices, zero prices, subnormal floats, and negative zero.
func TestChallenger1_InvariantQuarantine_ExtremePrices(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	engine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Extreme price validation",
		Author:      "adversary@coderpush.com",
		Items: []domain.PatchItem{
			// Quarantined Items:
			{SKU: "SKU-NAN", Name: "NaN Price", Price: math.NaN(), Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-POS-INF", Name: "Pos Inf Price", Price: math.Inf(1), Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-NEG-INF", Name: "Neg Inf Price", Price: math.Inf(-1), Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-NEG-1", Name: "Minus One", Price: -1.0, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-NEG-TINY", Name: "Micro Negative", Price: -0.00000001, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-NEG-LARGE", Name: "Mega Negative", Price: -1000000.0, Status: domain.StatusActive, Timestamp: &now},

			// Valid Items:
			{SKU: "SKU-FREE-SAMPLE", Name: "Free Promo Item", Price: 0.00, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-NEG-ZERO", Name: "Negative Zero Float", Price: math.Copysign(0.0, -1.0), Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-SUBNORMAL", Name: "Subnormal Float", Price: math.SmallestNonzeroFloat64, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-NORMAL", Name: "Regular Product", Price: 19.99, Status: domain.StatusActive, Timestamp: &now},
		},
	}

	res, err := engine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// 6 invalid prices must be quarantined
	if res.Summary.DeadLetter != 6 || res.DeadLetterCount != 6 {
		t.Errorf("expected 6 dead letters for invalid prices, got summary=%d, count=%d",
			res.Summary.DeadLetter, res.DeadLetterCount)
	}

	// 4 valid prices must be added
	if res.Summary.Added != 4 || res.AddedCount != 4 {
		t.Errorf("expected 4 added items, got summary=%d, count=%d",
			res.Summary.Added, res.AddedCount)
	}

	// Verify error reasons
	expectedReason := domain.ErrNegativePrice.Error()
	for _, dl := range res.DeadLetterItems {
		if dl.Reason != expectedReason {
			t.Errorf("expected reason %q, got %q for SKU %s", expectedReason, dl.Reason, dl.SKU)
		}
	}
}

// TestChallenger1_InvariantQuarantine_InvalidStatusesAndEmptySKUs tests
// unrecognized statuses, whitespace SKUs, and zero timestamps.
func TestChallenger1_InvariantQuarantine_InvalidStatusesAndEmptySKUs(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	zeroTime := time.Time{}
	engine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Invalid statuses and empty SKUs",
		Author:      "adversary@coderpush.com",
		Items: []domain.PatchItem{
			// Invalid Statuses (must be ACTIVE or WITHDRAWN)
			{SKU: "SKU-STAT-1", Name: "Item", Price: 10.0, Status: domain.ProductStatus("PENDING"), Timestamp: &now},
			{SKU: "SKU-STAT-2", Name: "Item", Price: 10.0, Status: domain.ProductStatus("DELETED"), Timestamp: &now},
			{SKU: "SKU-STAT-3", Name: "Item", Price: 10.0, Status: domain.ProductStatus("ARCHIVED"), Timestamp: &now},
			{SKU: "SKU-STAT-4", Name: "Item", Price: 10.0, Status: domain.ProductStatus(""), Timestamp: &now},
			{SKU: "SKU-STAT-5", Name: "Item", Price: 10.0, Status: domain.ProductStatus("active"), Timestamp: &now}, // case sensitive
			{SKU: "SKU-STAT-6", Name: "Item", Price: 10.0, Status: domain.ProductStatus("withdrawn"), Timestamp: &now},

			// Empty or Whitespace SKUs
			{SKU: "", Name: "Item Empty SKU", Price: 10.0, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "   ", Name: "Item Spaces SKU", Price: 10.0, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "\t\r\n", Name: "Item Newlines SKU", Price: 10.0, Status: domain.StatusActive, Timestamp: &now},

			// Explicit Zero Timestamp
			{SKU: "SKU-ZERO-TIME", Name: "Zero Timestamp", Price: 10.0, Status: domain.StatusActive, Timestamp: &zeroTime},

			// Control: Valid item
			{SKU: "SKU-VALID-OK", Name: "Valid Item", Price: 10.0, Status: domain.StatusActive, Timestamp: &now},
		},
	}

	res, err := engine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// 6 invalid status + 3 empty SKU + 1 zero timestamp = 10 dead letters
	if res.Summary.DeadLetter != 10 || res.DeadLetterCount != 10 {
		t.Errorf("expected 10 dead letters, got summary=%d, count=%d",
			res.Summary.DeadLetter, res.DeadLetterCount)
	}
	if res.Summary.Added != 1 {
		t.Errorf("expected 1 added item, got %d", res.Summary.Added)
	}

	// Verify reasons
	statusErrCount := 0
	skuErrCount := 0
	zeroTimeErrCount := 0

	for _, dl := range res.DeadLetterItems {
		switch dl.Reason {
		case domain.ErrInvalidStatus.Error():
			statusErrCount++
		case domain.ErrEmptySKU.Error():
			skuErrCount++
		case domain.ErrZeroTimestamp.Error():
			zeroTimeErrCount++
		default:
			t.Errorf("unexpected dead letter reason: %s", dl.Reason)
		}
	}

	if statusErrCount != 6 {
		t.Errorf("expected 6 ErrInvalidStatus, got %d", statusErrCount)
	}
	if skuErrCount != 3 {
		t.Errorf("expected 3 ErrEmptySKU, got %d", skuErrCount)
	}
	if zeroTimeErrCount != 1 {
		t.Errorf("expected 1 ErrZeroTimestamp, got %d", zeroTimeErrCount)
	}
}

// -----------------------------------------------------------------------------
// MANDATE 3: INTRA-PATCH STATE ISOLATION & COMMIT GATING
// -----------------------------------------------------------------------------

// TestChallenger1_Adversarial_IntraPatch_StatePollutionAndIsolation verifies that
// invalid items in a patch DO NOT corrupt the projection or delta calculation of
// adjacent valid items.
func TestChallenger1_Adversarial_IntraPatch_StatePollutionAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	t1 := now.Add(1 * time.Hour)
	t2 := now.Add(2 * time.Hour)
	t3 := now.Add(3 * time.Hour)

	// Seed existing product
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-EXISTING", Name: "Existing Product", Price: 50.0, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now})
	_ = tx.Commit()

	engine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Interleaved valid and adversarial items",
		Author:      "interleave@coderpush.com",
		Items: []domain.PatchItem{
			// Item 1: Valid addition of SKU-ALPHA
			{SKU: "SKU-ALPHA", Name: "Alpha V1", Price: 10.0, Status: domain.StatusActive, Timestamp: &t1},

			// Item 2: Toxic corrupt item (negative price) -> quarantined!
			{SKU: "SKU-ALPHA", Name: "Alpha Corrupt", Price: -99.0, Status: domain.StatusActive, Timestamp: &t2},

			// Item 3: Valid chained update to SKU-ALPHA (should evaluate against Item 1, ignoring Item 2)
			{SKU: "SKU-ALPHA", Name: "Alpha V2", Price: 15.0, Status: domain.StatusActive, Timestamp: &t3},

			// Item 4: Toxic corrupt status on SKU-EXISTING -> quarantined!
			{SKU: "SKU-EXISTING", Name: "Existing Bad", Price: 50.0, Status: domain.ProductStatus("BOGUS"), Timestamp: &t1},

			// Item 5: Valid price update on SKU-EXISTING -> should evaluate cleanly against DB state
			{SKU: "SKU-EXISTING", Name: "Existing Updated", Price: 60.0, Status: domain.StatusActive, Timestamp: &t2},
		},
	}

	res, err := engine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// Dead letters: 2 (Item 2 and Item 4)
	if res.Summary.DeadLetter != 2 || res.DeadLetterCount != 2 {
		t.Errorf("expected 2 dead letters, got summary=%d, count=%d", res.Summary.DeadLetter, res.DeadLetterCount)
	}

	// Added: 1 (SKU-ALPHA initial creation)
	if res.Summary.Added != 1 || res.AddedCount != 1 {
		t.Errorf("expected 1 added item (SKU-ALPHA), got %d", res.Summary.Added)
	}

	// Updated: 2 (SKU-ALPHA updated to V2, SKU-EXISTING updated to 60.0)
	if res.Summary.Updated != 2 || res.UpdatedCount != 2 {
		t.Errorf("expected 2 updated items, got %d", res.Summary.Updated)
	}

	// Total deltas should be exactly 3 (1 added + 2 updated)
	if len(res.Deltas) != 3 {
		t.Fatalf("expected 3 deltas, got %d", len(res.Deltas))
	}

	// Verify that SKU-ALPHA V2 update delta correctly captured old_price = 10.0 (from Item 1)
	var foundAlphaUpdate bool
	for _, d := range res.Deltas {
		if d.SKU == "SKU-ALPHA" && d.Action == domain.DeltaActionUpdated {
			foundAlphaUpdate = true
			if d.OldPrice == nil || *d.OldPrice != 10.0 {
				t.Errorf("expected old_price 10.0 for SKU-ALPHA, got %v", d.OldPrice)
			}
			if d.NewPrice != 15.0 {
				t.Errorf("expected new_price 15.0 for SKU-ALPHA, got %f", d.NewPrice)
			}
		}
	}
	if !foundAlphaUpdate {
		t.Errorf("missing DeltaActionUpdated for SKU-ALPHA")
	}

	// CanCommit must be false because 2 dead letters occurred
	if res.CanCommit {
		t.Errorf("expected CanCommit == false")
	}
}

// TestChallenger1_PublishPatch_CommitGating_StrictIntegrity asserts that
// PublishPatch strictly refuses to commit patches containing any invariant violation,
// leaving SQLite database state completely unaltered.
func TestChallenger1_PublishPatch_CommitGating_StrictIntegrity(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()

	// Seed catalog
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-GATE-001", Name: "Gate 1", Price: 10.0, Status: domain.StatusWithdrawn, SupplierTimestamp: now, UpdatedAt: now})
	_ = tx.Commit()

	countsBeforeProducts, countsBeforeVersions, countsBeforeDeltas, countsBeforeDL := computeTableCounts(t, store)
	checksumBefore, _ := computeCatalogChecksum(t, store)

	engine := catalogSync.NewPatchEngine(store)

	corruptPatch := domain.CatalogPatch{
		Description: "Corrupt patch attempting publish",
		Author:      "attacker@test.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-VALID-NEW", Name: "Valid", Price: 20.0, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-GATE-001", Name: "Illegal Resurrection", Price: 15.0, Status: domain.StatusActive, Timestamp: &now},
		},
	}

	ver, deltas, err := engine.PublishPatch(ctx, corruptPatch)
	if err == nil {
		t.Fatalf("expected PublishPatch to fail with ErrPatchContainsInvalidRecords, got ver=%v, deltas=%d", ver, len(deltas))
	}
	if !errors.Is(err, domain.ErrPatchContainsInvalidRecords) {
		t.Errorf("expected error wrapping ErrPatchContainsInvalidRecords, got: %v", err)
	}

	// Verify database was 100% untouched
	countsAfterProducts, countsAfterVersions, countsAfterDeltas, countsAfterDL := computeTableCounts(t, store)
	checksumAfter, _ := computeCatalogChecksum(t, store)

	if countsBeforeProducts != countsAfterProducts || countsBeforeVersions != countsAfterVersions ||
		countsBeforeDeltas != countsAfterDeltas || countsBeforeDL != countsAfterDL {
		t.Fatalf("database table counts mutated on failed PublishPatch!")
	}
	if checksumBefore != checksumAfter {
		t.Fatalf("database checksum mutated on failed PublishPatch!")
	}
}

// -----------------------------------------------------------------------------
// MANDATE 4: HIGH CONCURRENCY RACE & REPRODUCIBILITY STRESS TEST
// -----------------------------------------------------------------------------

// TestChallenger1_HighConcurrency_20Workers_PreviewImmutability tests 20 concurrent
// goroutines calling PreviewPatch with random adversarial and valid patches
// against the same store under -race, verifying zero data races and zero DB mutation.
func TestChallenger1_HighConcurrency_20Workers_PreviewImmutability(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()

	// Seed 50 items
	tx, _ := store.BeginTx(ctx)
	for i := 0; i < 50; i++ {
		status := domain.StatusActive
		if i%3 == 0 {
			status = domain.StatusWithdrawn
		}
		_ = store.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-CONC-%03d", i),
			Name:              fmt.Sprintf("Conc Product %d", i),
			Price:             float64(i + 1),
			Status:            status,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	_ = tx.Commit()

	checksumBefore, countBefore := computeCatalogChecksum(t, store)

	engine := catalogSync.NewPatchEngine(store)

	const numWorkers = 20
	const iterations = 15

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	errCh := make(chan error, numWorkers*iterations)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier

			r := rand.New(rand.NewSource(int64(workerID * 1000)))

			for it := 0; it < iterations; it++ {
				skuIdx := r.Intn(50)
				sku := fmt.Sprintf("SKU-CONC-%03d", skuIdx)
				ts := now.Add(time.Duration(it+1) * time.Hour)

				// Randomly generate valid or adversarial items
				items := []domain.PatchItem{
					{
						SKU:       sku,
						Name:      "Random Update",
						Price:     float64(r.Intn(100) + 1),
						Status:    domain.StatusActive, // May cause resurrection if sku was withdrawn!
						Timestamp: &ts,
					},
					{
						SKU:       fmt.Sprintf("SKU-WORKER-%d-%d", workerID, it),
						Name:      "Worker Item",
						Price:     float64(r.Intn(50) + 5),
						Status:    domain.StatusActive,
						Timestamp: &ts,
					},
				}

				patch := domain.CatalogPatch{
					Description: fmt.Sprintf("Concurrent patch worker %d iter %d", workerID, it),
					Author:      fmt.Sprintf("worker-%d", workerID),
					Items:       items,
				}

				res, err := engine.PreviewPatch(ctx, patch)
				if err != nil {
					errCh <- fmt.Errorf("worker %d iter %d: %w", workerID, it, err)
					return
				}
				if res == nil {
					errCh <- fmt.Errorf("worker %d iter %d: nil result", workerID, it)
					return
				}
			}
		}(w)
	}

	close(startBarrier)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrency error: %v", err)
	}

	checksumAfter, countAfter := computeCatalogChecksum(t, store)
	if checksumBefore != checksumAfter || countBefore != countAfter {
		t.Fatalf("database mutated during 20 concurrent PreviewPatch calls!\nBefore: %s (%d)\nAfter:  %s (%d)",
			checksumBefore, countBefore, checksumAfter, countAfter)
	}
}

// -----------------------------------------------------------------------------
// MANDATE 5: DETERMINISTIC FUZZ ORACLE WITH 1,000 RANDOM ADVERSARIAL ITEMS
// -----------------------------------------------------------------------------

// TestChallenger1_Fuzz_AdversarialOracle_1000Items generates 1,000 pseudo-random
// adversarial operations and compares PreviewPatch output against an independent
// verification oracle, verifying classification, dead-letter quarantine, and checksum parity.
func TestChallenger1_Fuzz_AdversarialOracle_1000Items(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	baseTime := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

	// 1. Seed 100 items into SQLite (50 active, 50 withdrawn)
	const seedCount = 100
	tx, _ := store.BeginTx(ctx)
	for i := 0; i < seedCount; i++ {
		status := domain.StatusActive
		if i%2 == 1 {
			status = domain.StatusWithdrawn
		}
		_ = store.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-FUZZ-%03d", i),
			Name:              fmt.Sprintf("Fuzz Product %d", i),
			Price:             10.0 + float64(i),
			Status:            status,
			SupplierTimestamp: baseTime,
			UpdatedAt:         baseTime,
		})
	}
	_ = tx.Commit()

	// 2. Build independent oracle shadow state
	type oracleProduct struct {
		name      string
		price     float64
		status    domain.ProductStatus
		timestamp time.Time
	}
	oracleMap := make(map[string]oracleProduct)
	for i := 0; i < seedCount; i++ {
		sku := fmt.Sprintf("SKU-FUZZ-%03d", i)
		st := domain.StatusActive
		if i%2 == 1 {
			st = domain.StatusWithdrawn
		}
		oracleMap[sku] = oracleProduct{
			name:      fmt.Sprintf("Fuzz Product %d", i),
			price:     10.0 + float64(i),
			status:    st,
			timestamp: baseTime,
		}
	}

	// 3. Generate 1,000 pseudo-random patch items with fixed PRNG seed for deterministic reproducibility
	const totalFuzzItems = 1000
	r := rand.New(rand.NewSource(424242))

	patchItems := make([]domain.PatchItem, totalFuzzItems)
	var expAdded, expUpdated, expWithdrawn, expSkipped, expDeadLetter int

	fixedRefTime := baseTime.Add(24 * time.Hour)

	for i := 0; i < totalFuzzItems; i++ {
		op := r.Intn(10) // 10 distinct operational variants
		var item domain.PatchItem

		switch op {
		case 0:
			// Add brand new item (valid)
			ts := baseTime.Add(time.Duration(i+1) * time.Minute)
			item = domain.PatchItem{
				SKU:       fmt.Sprintf("SKU-FUZZ-NEW-%04d", i),
				Name:      fmt.Sprintf("New Fuzz %d", i),
				Price:     25.0,
				Status:    domain.StatusActive,
				Timestamp: &ts,
			}
		case 1:
			// Update existing item with forward timestamp (valid update or withdrawal)
			skuIdx := r.Intn(seedCount)
			sku := fmt.Sprintf("SKU-FUZZ-%03d", skuIdx)
			curr := oracleMap[sku]
			ts := curr.timestamp.Add(time.Hour)

			newStatus := curr.status
			if curr.status == domain.StatusActive && r.Float64() < 0.3 {
				newStatus = domain.StatusWithdrawn
			}

			item = domain.PatchItem{
				SKU:       sku,
				Name:      fmt.Sprintf("%s Rev", curr.name),
				Price:     curr.price + 5.0,
				Status:    newStatus,
				Timestamp: &ts,
			}
		case 2:
			// Invariant A attack: Resurrect existing withdrawn item to active
			skuIdx := (r.Intn(seedCount/2) * 2) + 1 // Pick seeded withdrawn item
			sku := fmt.Sprintf("SKU-FUZZ-%03d", skuIdx)
			curr := oracleMap[sku]
			ts := curr.timestamp.Add(time.Hour)
			item = domain.PatchItem{
				SKU:       sku,
				Name:      "Resurrected Fuzz",
				Price:     99.0,
				Status:    domain.StatusActive, // Illegal resurrection!
				Timestamp: &ts,
			}
		case 3:
			// Invariant B attack: Backward timestamp
			skuIdx := r.Intn(seedCount)
			sku := fmt.Sprintf("SKU-FUZZ-%03d", skuIdx)
			curr := oracleMap[sku]
			staleTs := curr.timestamp.Add(-10 * time.Minute)
			item = domain.PatchItem{
				SKU:       sku,
				Name:      "Stale Fuzz",
				Price:     curr.price + 1.0,
				Status:    curr.status,
				Timestamp: &staleTs,
			}
		case 4:
			// Invariant C attack: NaN or negative price
			item = domain.PatchItem{
				SKU:       fmt.Sprintf("SKU-FUZZ-BAD-%04d", i),
				Name:      "Bad Float Fuzz",
				Price:     -10.0,
				Status:    domain.StatusActive,
				Timestamp: &fixedRefTime,
			}
		case 5:
			// Invariant D attack: Invalid status
			item = domain.PatchItem{
				SKU:       fmt.Sprintf("SKU-FUZZ-BAD-%04d", i),
				Name:      "Bad Status Fuzz",
				Price:     10.0,
				Status:    domain.ProductStatus("DISCARDED"),
				Timestamp: &fixedRefTime,
			}
		case 6:
			// Invariant E attack: Whitespace SKU
			item = domain.PatchItem{
				SKU:       "   ",
				Name:      "Blank SKU",
				Price:     10.0,
				Status:    domain.StatusActive,
				Timestamp: &fixedRefTime,
			}
		case 7:
			// Free promotional item: Price 0.00 (valid)
			ts := baseTime.Add(time.Duration(i+1) * time.Minute)
			item = domain.PatchItem{
				SKU:       fmt.Sprintf("SKU-FUZZ-FREE-%04d", i),
				Name:      "Free Sample",
				Price:     0.0,
				Status:    domain.StatusActive,
				Timestamp: &ts,
			}
		case 8:
			// Idempotent identical skip
			skuIdx := r.Intn(seedCount)
			sku := fmt.Sprintf("SKU-FUZZ-%03d", skuIdx)
			curr := oracleMap[sku]
			ts := curr.timestamp
			item = domain.PatchItem{
				SKU:       sku,
				Name:      curr.name,
				Price:     curr.price,
				Status:    curr.status,
				Timestamp: &ts,
			}
		default:
			// Legal metadata update on withdrawn item (Status remains WITHDRAWN)
			skuIdx := (r.Intn(seedCount/2) * 2) + 1
			sku := fmt.Sprintf("SKU-FUZZ-%03d", skuIdx)
			curr := oracleMap[sku]
			ts := curr.timestamp.Add(time.Hour)
			item = domain.PatchItem{
				SKU:       sku,
				Name:      fmt.Sprintf("%s Metadata Update", curr.name),
				Price:     curr.price + 2.0,
				Status:    domain.StatusWithdrawn,
				Timestamp: &ts,
			}
		}

		patchItems[i] = item

		// 4. Update oracle simulation
		sku := strings.TrimSpace(item.SKU)
		if sku == "" || math.IsNaN(item.Price) || math.IsInf(item.Price, 0) || item.Price < 0 || !item.Status.IsValid() {
			expDeadLetter++
			continue
		}

		itemTime := fixedRefTime
		if item.Timestamp != nil && !item.Timestamp.IsZero() {
			itemTime = item.Timestamp.UTC()
		}

		existing, exists := oracleMap[sku]
		if !exists {
			expAdded++
			oracleMap[sku] = oracleProduct{
				name:      item.Name,
				price:     item.Price,
				status:    item.Status,
				timestamp: itemTime,
			}
			continue
		}

		if existing.status == domain.StatusWithdrawn && item.Status == domain.StatusActive {
			expDeadLetter++
			continue
		}

		if existing.name == item.Name && math.Abs(existing.price-item.Price) < 1e-6 &&
			existing.status == item.Status && existing.timestamp.Equal(itemTime) {
			expSkipped++
			continue
		}

		if itemTime.Before(existing.timestamp) || itemTime.Equal(existing.timestamp) {
			expDeadLetter++
			continue
		}

		if existing.status != domain.StatusWithdrawn && item.Status == domain.StatusWithdrawn {
			expWithdrawn++
		} else {
			expUpdated++
		}

		oracleMap[sku] = oracleProduct{
			name:      item.Name,
			price:     item.Price,
			status:    item.Status,
			timestamp: itemTime,
		}
	}

	// 5. Run PreviewPatch with locked time func
	engine := catalogSync.NewPatchEngine(store, catalogSync.WithPatchTimeFunc(func() time.Time { return fixedRefTime }))
	patch := domain.CatalogPatch{
		PatchID:     "patch-fuzz-1000",
		Description: "Deterministic 1,000 item fuzz test",
		Author:      "oracle-challenger@coderpush.com",
		Items:       patchItems,
	}

	res, err := engine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// 6. Assert oracle parity
	if res.Summary.Added != expAdded || res.AddedCount != expAdded {
		t.Errorf("Added mismatch: got %d (count %d), expected %d", res.Summary.Added, res.AddedCount, expAdded)
	}
	if res.Summary.Updated != expUpdated || res.UpdatedCount != expUpdated {
		t.Errorf("Updated mismatch: got %d (count %d), expected %d", res.Summary.Updated, res.UpdatedCount, expUpdated)
	}
	if res.Summary.Withdrawn != expWithdrawn || res.WithdrawnCount != expWithdrawn {
		t.Errorf("Withdrawn mismatch: got %d (count %d), expected %d", res.Summary.Withdrawn, res.WithdrawnCount, expWithdrawn)
	}
	if res.Summary.Skipped != expSkipped || res.SkippedCount != expSkipped {
		t.Errorf("Skipped mismatch: got %d (count %d), expected %d", res.Summary.Skipped, res.SkippedCount, expSkipped)
	}
	if res.Summary.DeadLetter != expDeadLetter || res.DeadLetterCount != expDeadLetter {
		t.Errorf("DeadLetter mismatch: got %d (count %d), expected %d", res.Summary.DeadLetter, res.DeadLetterCount, expDeadLetter)
	}
	if len(res.DeadLetterItems) != expDeadLetter {
		t.Errorf("DeadLetterItems length mismatch: got %d, expected %d", len(res.DeadLetterItems), expDeadLetter)
	}
	if res.TotalItems != totalFuzzItems {
		t.Errorf("TotalItems mismatch: got %d, expected %d", res.TotalItems, totalFuzzItems)
	}
	if expDeadLetter > 0 && res.CanCommit {
		t.Errorf("expected CanCommit == false when dead letters exist")
	}

	// 7. Verify projected checksum against oracle products
	oracleProds := make([]domain.Product, 0, len(oracleMap))
	for sku, op := range oracleMap {
		oracleProds = append(oracleProds, domain.Product{
			SKU:               sku,
			Name:              op.name,
			Price:             op.price,
			Status:            op.status,
			SupplierTimestamp: op.timestamp,
			UpdatedAt:         op.timestamp,
		})
	}
	expectedChecksum := catalogSync.CalculateCatalogChecksum(oracleProds)

	if res.CatalogChecksum != expectedChecksum {
		t.Fatalf("FUZZ CHECKSUM DRIFT!\nGot:      %s\nExpected: %s", res.CatalogChecksum, expectedChecksum)
	}
}
