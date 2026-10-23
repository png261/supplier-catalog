package sync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

// computeCatalogChecksum computes a deterministic SHA-256 hash over all products in SQLite.
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

// computeTableCounts snapshots row counts across all 4 catalog versioning & audit tables.
func computeTableCounts(t *testing.T, store *storage.SQLiteStorage) (products, versions, deltas, deadLetters int) {
	t.Helper()
	db := store.DB()
	if err := db.QueryRow("SELECT COUNT(*) FROM products;").Scan(&products); err != nil {
		t.Fatalf("failed counting products: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM catalog_versions;").Scan(&versions); err != nil {
		t.Fatalf("failed counting catalog_versions: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM catalog_version_deltas;").Scan(&deltas); err != nil {
		t.Fatalf("failed counting catalog_version_deltas: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM dead_letter_issues;").Scan(&deadLetters); err != nil {
		t.Fatalf("failed counting dead_letter_issues: %v", err)
	}
	return
}

// -----------------------------------------------------------------------------
// PILLAR 1: IMMUTABILITY VERIFICATION
// -----------------------------------------------------------------------------

// TestPatch_Preview_Immutability_ZeroDatabaseMutations asserts that PreviewPatch
// performs ZERO database mutations: table row counts, record values, and SHA-256
// catalog checksums remain 100% bit-for-bit invariant before and after preview.
func TestPatch_Preview_Immutability_ZeroDatabaseMutations(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

	// 1. Seed initial catalog with 3 products
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	prods := []*domain.Product{
		{SKU: "SKU-001", Name: "Widget A", Price: 100.00, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now},
		{SKU: "SKU-002", Name: "Widget B", Price: 200.00, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now},
		{SKU: "SKU-003", Name: "Widget C", Price: 300.00, Status: domain.StatusWithdrawn, SupplierTimestamp: now, UpdatedAt: now},
	}
	for _, p := range prods {
		if err := store.UpsertProductTx(ctx, tx, p); err != nil {
			_ = tx.Rollback()
			t.Fatalf("UpsertProductTx %s failed: %v", p.SKU, err)
		}
	}
	ver1 := &domain.CatalogVersion{
		VersionID:          "ver-seed-001",
		VersionNumber:      1,
		Description:        "Seed catalog baseline",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypeInitial,
		TotalItemsAffected: 3,
		Summary:            domain.ChangeSummary{Added: 3},
		CreatedAt:          now,
	}
	deltas1 := []domain.ItemDelta{
		{VersionID: "ver-seed-001", SKU: "SKU-001", Action: domain.DeltaActionAdded, NewName: "Widget A", NewPrice: 100.00, NewStatus: domain.StatusActive, NewTimestamp: now},
		{VersionID: "ver-seed-001", SKU: "SKU-002", Action: domain.DeltaActionAdded, NewName: "Widget B", NewPrice: 200.00, NewStatus: domain.StatusActive, NewTimestamp: now},
		{VersionID: "ver-seed-001", SKU: "SKU-003", Action: domain.DeltaActionAdded, NewName: "Widget C", NewPrice: 300.00, NewStatus: domain.StatusWithdrawn, NewTimestamp: now},
	}
	if err := store.CreateVersionTx(ctx, tx, ver1, deltas1); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// 2. Snapshot state before PreviewPatch
	checksumBefore, countBefore := computeCatalogChecksum(t, store)
	pCntB, vCntB, dCntB, dlCntB := computeTableCounts(t, store)

	// 3. Construct aggressive patch touching all categories + invalid records
	future := now.Add(2 * time.Hour)
	patch := domain.CatalogPatch{
		PatchID:     "patch-preview-immutability",
		Description: "Simulated price increase and product withdrawal",
		Author:      "analyst@coderpush.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-001", Name: "Widget A Pro", Price: 150.00, Status: domain.StatusActive, Timestamp: &future}, // Update
			{SKU: "SKU-002", Name: "Widget B", Price: 200.00, Status: domain.StatusWithdrawn, Timestamp: &future},  // Withdrawn
			{SKU: "SKU-003", Name: "Widget C", Price: 350.00, Status: domain.StatusActive, Timestamp: &future},     // Tombstone resurrection violation!
			{SKU: "SKU-004", Name: "Widget D New", Price: 400.00, Status: domain.StatusActive, Timestamp: &future}, // Add
			{SKU: "SKU-005", Name: "Widget E Bad", Price: -9.99, Status: domain.StatusActive, Timestamp: &future},  // Invalid price violation!
		},
	}

	// 4. Execute PreviewPatch
	patchEngine := catalogSync.NewPatchEngine(store, catalogSync.WithPatchTimeFunc(func() time.Time { return future }))
	result, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed unexpectedly: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil PatchPreviewResult")
	}

	// 5. Snapshot state after PreviewPatch
	checksumAfter, countAfter := computeCatalogChecksum(t, store)
	pCntA, vCntA, dCntA, dlCntA := computeTableCounts(t, store)

	// 6. Assert 100% database immutability
	if checksumBefore != checksumAfter {
		t.Fatalf("CRITICAL IMMUTABILITY VIOLATION: Catalog checksum drifted after preview!\nBefore: %s\nAfter:  %s", checksumBefore, checksumAfter)
	}
	if countBefore != countAfter {
		t.Errorf("product row count drifted: before=%d, after=%d", countBefore, countAfter)
	}
	if pCntB != pCntA {
		t.Errorf("products table count modified: before=%d, after=%d", pCntB, pCntA)
	}
	if vCntB != vCntA {
		t.Errorf("catalog_versions table count modified: before=%d, after=%d", vCntB, vCntA)
	}
	if dCntB != dCntA {
		t.Errorf("catalog_version_deltas table count modified: before=%d, after=%d", dCntB, dCntA)
	}
	if dlCntB != dlCntA {
		t.Errorf("dead_letter_issues table count modified: before=%d, after=%d", dlCntB, dlCntA)
	}

	// 7. Verify point products in SQLite are untouched
	p1, err := store.GetProduct(ctx, "SKU-001")
	if err != nil || p1.Price != 100.00 || p1.Name != "Widget A" {
		t.Errorf("SKU-001 was mutated in SQLite: %+v", p1)
	}
	p3, err := store.GetProduct(ctx, "SKU-003")
	if err != nil || p3.Status != domain.StatusWithdrawn {
		t.Errorf("SKU-003 tombstone was mutated in SQLite: %+v", p3)
	}
	_, err = store.GetProduct(ctx, "SKU-004")
	if err == nil {
		t.Errorf("SKU-004 should NOT exist in SQLite before publication")
	}
}

// -----------------------------------------------------------------------------
// PILLAR 2: INVARIANT QUARANTINE TESTS
// -----------------------------------------------------------------------------

// TestPatch_Preview_InvariantA_TombstoneResurrectionQuarantine asserts that
// attempting to reactivate a WITHDRAWN item to ACTIVE places it in DeadLetterItems
// with ErrResurrectionRestricted reason, while metadata updates on WITHDRAWN items succeed.
func TestPatch_Preview_InvariantA_TombstoneResurrectionQuarantine(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)

	// Seed catalog with a tombstoned product (WITHDRAWN)
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{
		SKU:               "SKU-TOMB-001",
		Name:              "Discontinued Model X",
		Price:             49.99,
		Status:            domain.StatusWithdrawn,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	})
	_ = tx.Commit()

	patchEngine := catalogSync.NewPatchEngine(store)

	// Case 1: Illegal resurrection attempt (WITHDRAWN -> ACTIVE)
	illegalPatch := domain.CatalogPatch{
		Description: "Resurrect discontinued item",
		Author:      "staff@coderpush.com",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-TOMB-001",
				Name:      "Resurrected Model X",
				Price:     59.99,
				Status:    domain.StatusActive, // VIOLATION: Invariant A
				Timestamp: &future,
			},
		},
	}

	res1, err := patchEngine.PreviewPatch(ctx, illegalPatch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}
	if res1.Summary.DeadLetter != 1 || res1.DeadLetterCount != 1 {
		t.Errorf("expected Summary.DeadLetter == 1, got summary=%d, count=%d", res1.Summary.DeadLetter, res1.DeadLetterCount)
	}
	if res1.Summary.Added != 0 || res1.Summary.Updated != 0 {
		t.Errorf("expected 0 added and 0 updated, got %+v", res1.Summary)
	}
	if len(res1.Deltas) != 0 {
		t.Errorf("expected 0 deltas for quarantined item, got %d", len(res1.Deltas))
	}
	if res1.CanCommit {
		t.Errorf("expected CanCommit == false when dead-letter records exist")
	}

	deadLetters := res1.InvalidRecords
	if len(deadLetters) != 1 {
		t.Fatalf("expected 1 dead letter record, got %d", len(deadLetters))
	}
	dl := deadLetters[0]
	if dl.SKU != "SKU-TOMB-001" {
		t.Errorf("expected SKU-TOMB-001 in dead letters, got %s", dl.SKU)
	}
	if !strings.Contains(dl.Reason, "tombstone") && !strings.Contains(dl.Reason, "WITHDRAWN") {
		t.Errorf("expected tombstone violation reason, got %q", dl.Reason)
	}

	// Also verify DeadLetterItems slice
	if len(res1.DeadLetterItems) != 1 || res1.DeadLetterItems[0].SKU != "SKU-TOMB-001" {
		t.Errorf("expected SKU-TOMB-001 in DeadLetterItems, got %+v", res1.DeadLetterItems)
	}

	// Case 2: Legal metadata update on WITHDRAWN item (Status remains WITHDRAWN)
	legalPatch := domain.CatalogPatch{
		Description: "Update historical price of withdrawn item",
		Author:      "staff@coderpush.com",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-TOMB-001",
				Name:      "Discontinued Model X (Archival Revision)",
				Price:     45.00,
				Status:    domain.StatusWithdrawn, // Legal: remains WITHDRAWN
				Timestamp: &future,
			},
		},
	}

	res2, err := patchEngine.PreviewPatch(ctx, legalPatch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}
	if res2.Summary.DeadLetter != 0 || res2.Summary.Updated != 1 {
		t.Errorf("expected Summary.Updated == 1, DeadLetter == 0, got %+v", res2.Summary)
	}
	if len(res2.Deltas) != 1 {
		t.Fatalf("expected 1 delta for legal update, got %d", len(res2.Deltas))
	}
	if res2.Deltas[0].NewStatus != domain.StatusWithdrawn {
		t.Errorf("expected NewStatus == WITHDRAWN, got %s", res2.Deltas[0].NewStatus)
	}
	if !res2.CanCommit {
		t.Errorf("expected CanCommit == true for clean patch")
	}
}

// TestPatch_Preview_InvariantB_StaleTimestampQuarantine asserts that incoming patch
// items with SupplierTimestamp <= liveProduct.SupplierTimestamp and changed values
// are quarantined with ErrOutdatedTimestamp reason.
func TestPatch_Preview_InvariantB_StaleTimestampQuarantine(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	tBaseline := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	tOlder := tBaseline.Add(-1 * time.Hour)
	tEqual := tBaseline

	// Seed live product at tBaseline
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{
		SKU:               "SKU-STALE-001",
		Name:              "Current Gadget",
		Price:             25.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: tBaseline,
		UpdatedAt:         tBaseline,
	})
	_ = tx.Commit()

	patchEngine := catalogSync.NewPatchEngine(store)

	// Case 1: Strictly older timestamp
	olderPatch := domain.CatalogPatch{
		Description: "Stale patch with older timestamp",
		Author:      "staff@coderpush.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-STALE-001", Name: "Older Gadget", Price: 20.00, Status: domain.StatusActive, Timestamp: &tOlder},
		},
	}
	res1, err := patchEngine.PreviewPatch(ctx, olderPatch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}
	if res1.Summary.DeadLetter != 1 || len(res1.InvalidRecords) != 1 {
		t.Fatalf("expected older timestamp to be quarantined, got %+v", res1.Summary)
	}
	if !strings.Contains(res1.InvalidRecords[0].Reason, "timestamp") && !strings.Contains(res1.InvalidRecords[0].Reason, "older") {
		t.Errorf("expected outdated timestamp reason, got %q", res1.InvalidRecords[0].Reason)
	}

	// Case 2: Equal timestamp with differing content
	equalPatchDiff := domain.CatalogPatch{
		Description: "Equal timestamp with changed price",
		Author:      "staff@coderpush.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-STALE-001", Name: "Current Gadget", Price: 30.00, Status: domain.StatusActive, Timestamp: &tEqual},
		},
	}
	res2, err := patchEngine.PreviewPatch(ctx, equalPatchDiff)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}
	if res2.Summary.DeadLetter != 1 {
		t.Errorf("expected equal timestamp with differing content to be quarantined, got %+v", res2.Summary)
	}

	// Case 3: Equal timestamp with identical content (Idempotency skip)
	equalPatchIdentical := domain.CatalogPatch{
		Description: "Equal timestamp identical values",
		Author:      "staff@coderpush.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-STALE-001", Name: "Current Gadget", Price: 25.00, Status: domain.StatusActive, Timestamp: &tEqual},
		},
	}
	res3, err := patchEngine.PreviewPatch(ctx, equalPatchIdentical)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}
	if res3.Summary.Skipped != 1 || res3.Summary.DeadLetter != 0 {
		t.Errorf("expected identical record to be SKIPPED, got %+v", res3.Summary)
	}
}

// TestPatch_Preview_InvariantC_NegativePriceQuarantine asserts that negative
// and non-finite prices are quarantined, while price = 0.00 is allowed.
func TestPatch_Preview_InvariantC_NegativePriceQuarantine(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	patchEngine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Price invariant checks",
		Author:      "qa@coderpush.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-NEG-001", Name: "Negative Price", Price: -15.50, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-NEG-002", Name: "Micro Negative", Price: -0.01, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-ZERO-001", Name: "Free Promotional Sample", Price: 0.00, Status: domain.StatusActive, Timestamp: &now},
		},
	}

	res, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	if res.Summary.DeadLetter != 2 {
		t.Errorf("expected 2 dead-letter records for negative prices, got %d", res.Summary.DeadLetter)
	}
	if res.Summary.Added != 1 {
		t.Errorf("expected 1 added record for zero-price item, got %d", res.Summary.Added)
	}

	for _, dl := range res.InvalidRecords {
		if !strings.Contains(dl.Reason, "negative") && !strings.Contains(dl.Reason, "price") {
			t.Errorf("expected negative price reason for %s, got %q", dl.SKU, dl.Reason)
		}
	}
}

// TestPatch_Preview_InvariantD_InvalidStatusQuarantine asserts that unrecognized
// statuses are quarantined with ErrInvalidStatus reason.
func TestPatch_Preview_InvariantD_InvalidStatusQuarantine(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	patchEngine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Status invariant checks",
		Author:      "qa@coderpush.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-STAT-001", Name: "Item 1", Price: 10.00, Status: domain.ProductStatus("PENDING"), Timestamp: &now},
			{SKU: "SKU-STAT-002", Name: "Item 2", Price: 10.00, Status: domain.ProductStatus("DELETED"), Timestamp: &now},
			{SKU: "SKU-STAT-003", Name: "Item 3", Price: 10.00, Status: domain.ProductStatus("ARCHIVED"), Timestamp: &now},
			{SKU: "SKU-STAT-004", Name: "Item 4", Price: 10.00, Status: domain.ProductStatus(""), Timestamp: &now},
		},
	}

	res, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	if res.Summary.DeadLetter != 4 {
		t.Errorf("expected 4 dead-letter records, got %d", res.Summary.DeadLetter)
	}
	for _, dl := range res.InvalidRecords {
		if !strings.Contains(dl.Reason, "status") {
			t.Errorf("expected invalid status reason for %s, got %q", dl.SKU, dl.Reason)
		}
	}
}

// TestPatch_Preview_InvariantE_EmptySKUQuarantine asserts that empty or whitespace
// SKU is quarantined with ErrEmptySKU reason.
func TestPatch_Preview_InvariantE_EmptySKUQuarantine(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	patchEngine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Empty SKU check",
		Author:      "qa@coderpush.com",
		Items: []domain.PatchItem{
			{SKU: "", Name: "No SKU", Price: 10.00, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "   ", Name: "Whitespace SKU", Price: 10.00, Status: domain.StatusActive, Timestamp: &now},
		},
	}

	res, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	if res.Summary.DeadLetter != 2 {
		t.Errorf("expected 2 dead letters for empty SKUs, got %d", res.Summary.DeadLetter)
	}
	for _, dl := range res.InvalidRecords {
		if !strings.Contains(dl.Reason, "sku") && !strings.Contains(dl.Reason, "empty") {
			t.Errorf("expected empty SKU reason, got %q", dl.Reason)
		}
	}
}

// -----------------------------------------------------------------------------
// PILLAR 3: CLASSIFICATION ACCURACY
// -----------------------------------------------------------------------------

// TestPatch_Preview_ClassificationAccuracy_AllCategories asserts accurate projection
// of Added, Updated, Withdrawn, and Skipped items alongside pre/post-image deltas.
func TestPatch_Preview_ClassificationAccuracy_AllCategories(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	t1 := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Hour)

	// Seed existing items
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-EXIST-UPD", Name: "Existing Alpha", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: t1, UpdatedAt: t1})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-EXIST-WIT", Name: "Existing Beta", Price: 20.00, Status: domain.StatusActive, SupplierTimestamp: t1, UpdatedAt: t1})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-EXIST-SKP", Name: "Existing Gamma", Price: 30.00, Status: domain.StatusActive, SupplierTimestamp: t1, UpdatedAt: t1})
	_ = tx.Commit()

	patch := domain.CatalogPatch{
		Description: "Multi-category classification patch",
		Author:      "merchandiser@coderpush.com",
		Items: []domain.PatchItem{
			// 1. ADDED: SKU does not exist
			{SKU: "SKU-BRAND-NEW", Name: "Brand New Item", Price: 50.00, Status: domain.StatusActive, Timestamp: &t2},
			// 2. UPDATED: Price increased from 10.00 -> 12.50
			{SKU: "SKU-EXIST-UPD", Name: "Existing Alpha Pro", Price: 12.50, Status: domain.StatusActive, Timestamp: &t2},
			// 3. WITHDRAWN: Status changed from ACTIVE -> WITHDRAWN
			{SKU: "SKU-EXIST-WIT", Name: "Existing Beta", Price: 20.00, Status: domain.StatusWithdrawn, Timestamp: &t2},
			// 4. SKIPPED: Completely identical values and timestamp
			{SKU: "SKU-EXIST-SKP", Name: "Existing Gamma", Price: 30.00, Status: domain.StatusActive, Timestamp: &t1},
			// 5. DEAD-LETTER: Negative price
			{SKU: "SKU-DEAD-LET", Name: "Corrupt Item", Price: -5.00, Status: domain.StatusActive, Timestamp: &t2},
		},
	}

	patchEngine := catalogSync.NewPatchEngine(store)
	res, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// Verify Summary counts and direct count fields
	if res.Summary.Added != 1 || res.AddedCount != 1 {
		t.Errorf("expected Added == 1, got summary=%d, count=%d", res.Summary.Added, res.AddedCount)
	}
	if res.Summary.Updated != 1 || res.UpdatedCount != 1 {
		t.Errorf("expected Updated == 1, got summary=%d, count=%d", res.Summary.Updated, res.UpdatedCount)
	}
	if res.Summary.Withdrawn != 1 || res.WithdrawnCount != 1 {
		t.Errorf("expected Withdrawn == 1, got summary=%d, count=%d", res.Summary.Withdrawn, res.WithdrawnCount)
	}
	if res.Summary.Skipped != 1 || res.SkippedCount != 1 {
		t.Errorf("expected Skipped == 1, got summary=%d, count=%d", res.Summary.Skipped, res.SkippedCount)
	}
	if res.Summary.DeadLetter != 1 || res.DeadLetterCount != 1 {
		t.Errorf("expected DeadLetter == 1, got summary=%d, count=%d", res.Summary.DeadLetter, res.DeadLetterCount)
	}
	if res.TotalItems != 5 {
		t.Errorf("expected TotalItems == 5, got %d", res.TotalItems)
	}

	// Verify Deltas (Skipped and DeadLetter items do NOT produce deltas)
	if len(res.Deltas) != 3 {
		t.Fatalf("expected exactly 3 deltas (Added, Updated, Withdrawn), got %d", len(res.Deltas))
	}

	deltaMap := make(map[string]domain.ItemDelta)
	for _, d := range res.Deltas {
		deltaMap[d.SKU] = d
	}

	// Verify ADDED delta
	dAdd, ok := deltaMap["SKU-BRAND-NEW"]
	if !ok || dAdd.Action != domain.DeltaActionAdded || dAdd.OldPrice != nil || dAdd.NewPrice != 50.00 || dAdd.NewStatus != domain.StatusActive {
		t.Errorf("invalid ADDED delta: %+v", dAdd)
	}

	// Verify UPDATED delta with full pre-image
	dUpd, ok := deltaMap["SKU-EXIST-UPD"]
	if !ok || dUpd.Action != domain.DeltaActionUpdated {
		t.Errorf("invalid UPDATED delta: %+v", dUpd)
	}
	if dUpd.OldPrice == nil || *dUpd.OldPrice != 10.00 || dUpd.NewPrice != 12.50 {
		t.Errorf("expected price change 10.00 -> 12.50, got %v -> %f", dUpd.OldPrice, dUpd.NewPrice)
	}
	if dUpd.OldName == nil || *dUpd.OldName != "Existing Alpha" || dUpd.NewName != "Existing Alpha Pro" {
		t.Errorf("expected name change 'Existing Alpha' -> 'Existing Alpha Pro', got %v -> %s", dUpd.OldName, dUpd.NewName)
	}

	// Verify WITHDRAWN delta
	dWit, ok := deltaMap["SKU-EXIST-WIT"]
	if !ok || dWit.NewStatus != domain.StatusWithdrawn {
		t.Errorf("invalid WITHDRAWN delta: %+v", dWit)
	}
}

// -----------------------------------------------------------------------------
// PILLAR 4: DETERMINISTIC CHECKSUM VERIFICATION
// -----------------------------------------------------------------------------

// TestPatch_Preview_DeterministicChecksum_ProjectedMatchesPublished asserts
// that the projected catalog SHA-256 checksum calculated during PreviewPatch
// matches bit-for-bit with the actual SQLite catalog checksum after PublishPatch.
func TestPatch_Preview_DeterministicChecksum_ProjectedMatchesPublished(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	fixedTime := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)

	// Seed catalog with 3 products
	tx, _ := store.BeginTx(ctx)
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-CHK-001", Name: "Product 1", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: fixedTime, UpdatedAt: fixedTime})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-CHK-002", Name: "Product 2", Price: 20.00, Status: domain.StatusActive, SupplierTimestamp: fixedTime, UpdatedAt: fixedTime})
	_ = store.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-CHK-003", Name: "Product 3", Price: 30.00, Status: domain.StatusWithdrawn, SupplierTimestamp: fixedTime, UpdatedAt: fixedTime})
	_ = tx.Commit()

	patchTime := fixedTime.Add(time.Hour)
	patch := domain.CatalogPatch{
		Description: "Patch for deterministic checksum match",
		Author:      "checksum-audit@coderpush.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-CHK-001", Name: "Product 1 Rev", Price: 15.00, Status: domain.StatusActive, Timestamp: &patchTime}, // Update
			{SKU: "SKU-CHK-002", Name: "Product 2", Price: 20.00, Status: domain.StatusWithdrawn, Timestamp: &patchTime},  // Withdraw
			{SKU: "SKU-CHK-004", Name: "Product 4 New", Price: 40.00, Status: domain.StatusActive, Timestamp: &patchTime}, // Add
		},
	}

	// 1. Run PreviewPatch with locked time func
	patchEngine := catalogSync.NewPatchEngine(store, catalogSync.WithPatchTimeFunc(func() time.Time { return patchTime }))
	previewResult, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}
	if previewResult.CatalogChecksum == "" {
		t.Fatal("expected non-empty CatalogChecksum in PatchPreviewResult")
	}
	if len(previewResult.CatalogChecksum) != 64 {
		t.Fatalf("expected 64-char hex SHA-256 checksum, got %q (len %d)", previewResult.CatalogChecksum, len(previewResult.CatalogChecksum))
	}

	// 2. Publish the patch using the identical time func
	ver, deltas, err := patchEngine.PublishPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PublishPatch failed: %v", err)
	}
	if ver == nil || len(deltas) != 3 {
		t.Fatalf("expected 3 deltas published, got %d", len(deltas))
	}

	// 3. Compute actual SQLite catalog SHA-256 checksum
	actualPublishedChecksum, totalItems := computeCatalogChecksum(t, store)
	if totalItems != 4 {
		t.Fatalf("expected 4 products in published catalog, got %d", totalItems)
	}

	// 4. Critical assertion: Projected Checksum == Actual Published Checksum
	if previewResult.CatalogChecksum != actualPublishedChecksum {
		t.Fatalf("CHECKSUM MISMATCH!\nProjected (Preview): %s\nActual   (Published): %s",
			previewResult.CatalogChecksum, actualPublishedChecksum)
	}

	// 5. Verify published version summary matches preview summary
	if ver.Summary != previewResult.Summary {
		t.Errorf("version summary mismatch:\nPreview:   %+v\nPublished: %+v", previewResult.Summary, ver.Summary)
	}
}

// -----------------------------------------------------------------------------
// PILLAR 5: CONCURRENCY & RACE TESTING
// -----------------------------------------------------------------------------

// TestPatch_Preview_ConcurrencyAndRace_ZeroDataRaces verifies that multiple concurrent
// goroutines executing PreviewPatch over the same shared storage instance execute cleanly
// with zero data races under `go test -race` and zero lock contention.
func TestPatch_Preview_ConcurrencyAndRace_ZeroDataRaces(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()

	// 1. Seed 30 products into SQLite
	tx, _ := store.BeginTx(ctx)
	for i := 0; i < 30; i++ {
		sku := fmt.Sprintf("SKU-CONC-%03d", i)
		_ = store.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               sku,
			Name:              fmt.Sprintf("Concurrent Item %d", i),
			Price:             10.00 + float64(i),
			Status:            domain.StatusActive,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	_ = tx.Commit()

	initialChecksum, initialCount := computeCatalogChecksum(t, store)

	patchEngine := catalogSync.NewPatchEngine(store)

	const goroutines = 10
	const iterationsPerGoroutine = 20

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	errorsCh := make(chan error, goroutines*iterationsPerGoroutine)

	// Launch concurrent PreviewPatch workers
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier // Release all workers at the exact same instant

			for it := 0; it < iterationsPerGoroutine; it++ {
				patchFuture := now.Add(time.Duration(it+1) * time.Hour)
				patch := domain.CatalogPatch{
					Description: fmt.Sprintf("Concurrent preview worker %d iter %d", workerID, it),
					Author:      fmt.Sprintf("worker-%d", workerID),
					Items: []domain.PatchItem{
						{
							SKU:       fmt.Sprintf("SKU-CONC-%03d", (workerID+it)%30),
							Name:      "Updated Name",
							Price:     99.00,
							Status:    domain.StatusActive,
							Timestamp: &patchFuture,
						},
						{
							SKU:       fmt.Sprintf("SKU-CONC-NEW-%d-%d", workerID, it),
							Name:      "New Item",
							Price:     50.00,
							Status:    domain.StatusActive,
							Timestamp: &patchFuture,
						},
					},
				}

				res, err := patchEngine.PreviewPatch(ctx, patch)
				if err != nil {
					errorsCh <- fmt.Errorf("worker %d iter %d: %w", workerID, it, err)
					return
				}
				if res == nil || res.Summary.Added != 1 || res.Summary.Updated != 1 {
					errorsCh <- fmt.Errorf("worker %d iter %d unexpected summary: %+v", workerID, it, res)
					return
				}
			}
		}(g)
	}

	// Concurrently run background catalog reads
	const readers = 3
	wg.Add(readers)
	for r := 0; r < readers; r++ {
		go func(readerID int) {
			defer wg.Done()
			<-startBarrier
			for it := 0; it < iterationsPerGoroutine*2; it++ {
				sku := fmt.Sprintf("SKU-CONC-%03d", (readerID+it)%30)
				p, err := store.GetProduct(ctx, sku)
				if err != nil || p == nil {
					errorsCh <- fmt.Errorf("reader %d failed reading %s: %v", readerID, sku, err)
					return
				}
			}
		}(r)
	}

	// Release all workers simultaneously
	close(startBarrier)
	wg.Wait()
	close(errorsCh)

	// Collect any errors
	var errs []error
	for err := range errorsCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		t.Fatalf("encountered %d errors during concurrent preview execution: %v", len(errs), errs[0])
	}

	// Verify post-test database state is 100% unchanged
	finalChecksum, finalCount := computeCatalogChecksum(t, store)
	if initialChecksum != finalChecksum || initialCount != finalCount {
		t.Fatalf("database mutated during concurrent PreviewPatch calls!\nInitial: %s (%d)\nFinal:   %s (%d)",
			initialChecksum, initialCount, finalChecksum, finalCount)
	}
}

// -----------------------------------------------------------------------------
// SUPPLEMENTARY: INTRA-PATCH & COMMIT GATING
// -----------------------------------------------------------------------------

// TestPatch_Preview_IntraPatch_ChainedModifications verifies that multiple
// modifications to the same SKU within a single patch payload evaluate correctly
// against the in-memory simulated shadow state without writing to SQLite.
func TestPatch_Preview_IntraPatch_ChainedModifications(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	t1 := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t1.Add(2 * time.Hour)
	t4 := t1.Add(3 * time.Hour)

	// SKU-CHAIN does NOT exist initially in SQLite
	patch := domain.CatalogPatch{
		Description: "Intra-patch multi-revision test",
		Author:      "pipeline@coderpush.com",
		Items: []domain.PatchItem{
			// 1. Initial creation
			{SKU: "SKU-CHAIN", Name: "Chain V1", Price: 10.00, Status: domain.StatusActive, Timestamp: &t1},
			// 2. Immediate price revision
			{SKU: "SKU-CHAIN", Name: "Chain V2", Price: 15.00, Status: domain.StatusActive, Timestamp: &t2},
			// 3. Withdrawal within same patch
			{SKU: "SKU-CHAIN", Name: "Chain V2", Price: 15.00, Status: domain.StatusWithdrawn, Timestamp: &t3},
			// 4. Illegal resurrection within same patch!
			{SKU: "SKU-CHAIN", Name: "Chain V3 Resurrected", Price: 20.00, Status: domain.StatusActive, Timestamp: &t4},
		},
	}

	patchEngine := catalogSync.NewPatchEngine(store)
	res, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// Item 4 should trigger Invariant A tombstone violation against the simulated state!
	if res.Summary.DeadLetter != 1 {
		t.Errorf("expected 1 dead letter for intra-patch resurrection attempt, got %d", res.Summary.DeadLetter)
	}
	if len(res.InvalidRecords) != 1 || res.InvalidRecords[0].SKU != "SKU-CHAIN" {
		t.Errorf("expected SKU-CHAIN in dead letters, got %+v", res.InvalidRecords)
	}
}

// TestPatch_Publish_CommitGatingOnInvalidRecords verifies that if a patch contains
// dead-letter invariant violations, PublishPatch either safely quarantines dead letters
// into dead_letter_issues table or returns an error, preserving database integrity.
func TestPatch_Publish_CommitGatingOnInvalidRecords(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	patchEngine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Corrupt patch submission",
		Author:      "attacker@test.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-VALID-001", Name: "Valid Item", Price: 10.00, Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-CORRUPT-001", Name: "Bad Price", Price: -50.00, Status: domain.StatusActive, Timestamp: &now},
		},
	}

	preview, err := patchEngine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}
	if preview.CanCommit {
		t.Errorf("expected CanCommit == false when patch contains corrupt items")
	}

	// Attempting to publish patch with violations
	ver, deltas, err := patchEngine.PublishPatch(ctx, patch)
	if err != nil {
		// Valid design alternative 1: Strict rejection of entire patch
		t.Logf("PublishPatch strictly rejected corrupt patch: %v", err)
		return
	}

	// Valid design alternative 2: Atomically applies valid item and quarantines dead letter to DB
	if ver != nil {
		if ver.Summary.DeadLetter != 1 || ver.Summary.Added != 1 {
			t.Errorf("expected summary {Added: 1, DeadLetter: 1}, got %+v", ver.Summary)
		}
		if len(deltas) != 1 {
			t.Errorf("expected 1 valid delta applied, got %d", len(deltas))
		}
		// Verify dead letter was persisted in SQLite dead_letter_issues
		dlRows, _, err := store.ListDeadLetterRecords(ctx, "", "", 10, 0)
		if err != nil || len(dlRows) != 1 {
			t.Errorf("expected 1 persisted dead letter in SQLite, got %d (err: %v)", len(dlRows), err)
		}
	}
}

func TestPatch_EdgeCases_Coverage(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)
	fixedNow := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	// 1. WithPatchTimeFunc & custom PatchID publish
	pe := catalogSync.NewPatchEngine(store, catalogSync.WithPatchTimeFunc(func() time.Time { return fixedNow }))
	validPatch := domain.CatalogPatch{
		PatchID:     "ver-custom-001",
		Description: "Custom ID patch",
		Author:      "tester",
		Items: []domain.PatchItem{
			{SKU: "SKU-COV-1", Name: "Item 1", Price: 12.5, Status: domain.StatusActive},
		},
	}
	ver, deltas, err := pe.PublishPatch(ctx, validPatch)
	if err != nil {
		t.Fatalf("PublishPatch failed: %v", err)
	}
	if ver.VersionID != "ver-custom-001" || len(deltas) != 1 {
		t.Errorf("unexpected version: %+v", ver)
	}

	// 2. Nil storage errors
	nilPE := catalogSync.NewPatchEngine(nil)
	if _, err := nilPE.PreviewPatch(ctx, validPatch); err == nil {
		t.Error("expected error for nil storage PreviewPatch")
	}
	if _, _, err := nilPE.PublishPatch(ctx, validPatch); err == nil {
		t.Error("expected error for nil storage PublishPatch")
	}

	// 3. Canceled context errors
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := pe.PreviewPatch(cancelCtx, validPatch); err == nil {
		t.Error("expected error for canceled context PreviewPatch")
	}
	if _, _, err := pe.PublishPatch(cancelCtx, validPatch); err == nil {
		t.Error("expected error for canceled context PublishPatch")
	}

	// 4. Empty items / invalid patch
	if _, err := pe.PreviewPatch(ctx, domain.CatalogPatch{}); err == nil {
		t.Error("expected error for empty items PreviewPatch")
	}
	if _, _, err := pe.PublishPatch(ctx, domain.CatalogPatch{}); err == nil {
		t.Error("expected error for invalid patch PublishPatch")
	}

	// 5. CalculateCatalogChecksumFromDB edge cases
	if _, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, nil); err == nil {
		t.Error("expected error on nil DB")
	}
	sum, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
	if err != nil || sum == "" {
		t.Errorf("expected valid checksum from DB, got %q (%v)", sum, err)
	}

	// 6. Closed DB errors on PreviewPatch, PublishPatch, CalculateCatalogChecksumFromDB
	closedStore, _ := storage.OpenInMemory()
	_ = closedStore.Close()
	closedPE := catalogSync.NewPatchEngine(closedStore)
	if _, err := closedPE.PreviewPatch(ctx, validPatch); err == nil {
		t.Error("expected error on closed DB PreviewPatch")
	}
	if _, _, err := closedPE.PublishPatch(ctx, validPatch); err == nil {
		t.Error("expected error on closed DB PublishPatch")
	}
	if _, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, closedStore.DB()); err == nil {
		t.Error("expected error on closed DB CalculateCatalogChecksumFromDB")
	}
}
