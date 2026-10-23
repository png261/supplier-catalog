package storage_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
)

// ============================================================================
// Milestone 2 Challenger Suite: Non-Destructive Rollback Engine
// ============================================================================

// snapshotVersionAudit captures all records from catalog_versions and catalog_version_deltas
// to verify historical audit immutability (zero deletions / zero modifications).
type auditSnapshot struct {
	versions map[string]domain.CatalogVersion
	deltas   map[string][]domain.ItemDelta
}

func takeAuditSnapshot(t *testing.T, s *storage.SQLiteStorage) auditSnapshot {
	ctx := context.Background()
	vers, _, err := s.ListVersions(ctx, 1000, 0)
	if err != nil {
		t.Fatalf("takeAuditSnapshot failed listing versions: %v", err)
	}

	snap := auditSnapshot{
		versions: make(map[string]domain.CatalogVersion),
		deltas:   make(map[string][]domain.ItemDelta),
	}

	for _, v := range vers {
		snap.versions[v.VersionID] = v
		delts, err := s.GetVersionDeltas(ctx, v.VersionID)
		if err != nil {
			t.Fatalf("takeAuditSnapshot failed getting deltas for %s: %v", v.VersionID, err)
		}
		snap.deltas[v.VersionID] = delts
	}

	return snap
}

func verifyAuditPreservation(t *testing.T, s *storage.SQLiteStorage, priorSnap auditSnapshot) {
	ctx := context.Background()
	for vID, expectedVer := range priorSnap.versions {
		actualVer, err := s.GetVersion(ctx, vID)
		if err != nil {
			t.Fatalf("AUDIT VIOLATION: historical version %s was deleted or inaccessible: %v", vID, err)
		}
		if actualVer.VersionNumber != expectedVer.VersionNumber ||
			actualVer.Type != expectedVer.Type ||
			actualVer.Author != expectedVer.Author ||
			actualVer.TotalItemsAffected != expectedVer.TotalItemsAffected {
			t.Fatalf("AUDIT VIOLATION: historical version %s was modified! Expected %+v, got %+v", vID, expectedVer, actualVer)
		}

		actualDeltas, err := s.GetVersionDeltas(ctx, vID)
		if err != nil {
			t.Fatalf("AUDIT VIOLATION: deltas for historical version %s deleted: %v", vID, err)
		}
		expectedDeltas := priorSnap.deltas[vID]
		if len(actualDeltas) != len(expectedDeltas) {
			t.Fatalf("AUDIT VIOLATION: delta count changed for historical version %s: expected %d, got %d", vID, len(expectedDeltas), len(actualDeltas))
		}
		for i := range expectedDeltas {
			if actualDeltas[i].SKU != expectedDeltas[i].SKU ||
				actualDeltas[i].Action != expectedDeltas[i].Action ||
				actualDeltas[i].NewStatus != expectedDeltas[i].NewStatus ||
				actualDeltas[i].NewPrice != expectedDeltas[i].NewPrice {
				t.Fatalf("AUDIT VIOLATION: historical delta for version %s modified at index %d", vID, i)
			}
		}
	}
}

// ----------------------------------------------------------------------------
// 1. Invariant A Stress Test
// Construct adversarial sequences of versions where items transition between
// ACTIVE and WITHDRAWN, and execute multiple rollbacks to earlier versions where
// items were ACTIVE. Verify empirically that under NO circumstances is a
// WITHDRAWN item resurrected to ACTIVE.
// ----------------------------------------------------------------------------
func TestChallengerM2_InvariantA_AdversarialSequences(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Step 1: V1 (Initial Baseline) — 10 items (SKU-01 through SKU-10), ALL ACTIVE.
	tx1, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx1 failed: %v", err)
	}

	deltas1 := make([]domain.ItemDelta, 0, 10)
	for i := 1; i <= 10; i++ {
		sku := fmt.Sprintf("SKU-%02d", i)
		p := &domain.Product{
			SKU:               sku,
			Name:              fmt.Sprintf("Item %s V1", sku),
			Price:             float64(i * 10),
			Status:            domain.StatusActive,
			SupplierTimestamp: now.Add(-4 * time.Hour),
			UpdatedAt:         now.Add(-4 * time.Hour),
		}
		if err := s.UpsertProductTx(ctx, tx1, p); err != nil {
			t.Fatalf("UpsertProductTx tx1 failed for %s: %v", sku, err)
		}
		deltas1 = append(deltas1, domain.ItemDelta{
			VersionID:    "ver-inv-001",
			SKU:          sku,
			Action:       domain.DeltaActionAdded,
			NewName:      p.Name,
			NewPrice:     p.Price,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now.Add(-4 * time.Hour),
		})
	}
	ver1 := &domain.CatalogVersion{
		VersionID:          "ver-inv-001",
		VersionNumber:      1,
		Description:        "V1 Baseline: All 10 active",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypeInitial,
		TotalItemsAffected: 10,
		Summary:            domain.ChangeSummary{Added: 10},
		CreatedAt:          now.Add(-4 * time.Hour),
	}
	if err := s.CreateVersionTx(ctx, tx1, ver1, deltas1); err != nil {
		t.Fatalf("CreateVersionTx tx1 failed: %v", err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatalf("Commit tx1 failed: %v", err)
	}

	// Step 2: V2 (Patch) — Withdraw odd items (SKU-01, 03, 05, 07, 09).
	// Even items remain ACTIVE with updated price.
	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx2 failed: %v", err)
	}

	deltas2 := make([]domain.ItemDelta, 0, 10)
	summary2 := domain.ChangeSummary{}
	for i := 1; i <= 10; i++ {
		sku := fmt.Sprintf("SKU-%02d", i)
		oldPrice := float64(i * 10)
		oldName := fmt.Sprintf("Item %s V1", sku)
		oldStatus := domain.StatusActive
		oldTime := now.Add(-4 * time.Hour)

		var newStatus domain.ProductStatus
		var newName string
		var newPrice float64
		var action domain.DeltaAction

		if i%2 == 1 {
			// Odd items: WITHDRAWN
			newStatus = domain.StatusWithdrawn
			newName = fmt.Sprintf("Item %s V2 (Withdrawn)", sku)
			newPrice = oldPrice
			action = domain.DeltaActionWithdrawn
			summary2.Withdrawn++
		} else {
			// Even items: UPDATED
			newStatus = domain.StatusActive
			newName = fmt.Sprintf("Item %s V2", sku)
			newPrice = oldPrice + 5.0
			action = domain.DeltaActionUpdated
			summary2.Updated++
		}

		p := &domain.Product{
			SKU:               sku,
			Name:              newName,
			Price:             newPrice,
			Status:            newStatus,
			SupplierTimestamp: now.Add(-3 * time.Hour),
			UpdatedAt:         now.Add(-3 * time.Hour),
		}
		if err := s.UpsertProductTx(ctx, tx2, p); err != nil {
			t.Fatalf("UpsertProductTx tx2 failed for %s: %v", sku, err)
		}
		deltas2 = append(deltas2, domain.ItemDelta{
			VersionID:    "ver-inv-002",
			SKU:          sku,
			Action:       action,
			OldName:      &oldName,
			NewName:      newName,
			OldPrice:     &oldPrice,
			NewPrice:     newPrice,
			OldStatus:    &oldStatus,
			NewStatus:    newStatus,
			OldTimestamp: &oldTime,
			NewTimestamp: now.Add(-3 * time.Hour),
		})
	}
	ver2 := &domain.CatalogVersion{
		VersionID:          "ver-inv-002",
		VersionNumber:      2,
		Description:        "V2: Withdraw odd items, update even items",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypePatch,
		TotalItemsAffected: 10,
		Summary:            summary2,
		CreatedAt:          now.Add(-3 * time.Hour),
	}
	if err := s.CreateVersionTx(ctx, tx2, ver2, deltas2); err != nil {
		t.Fatalf("CreateVersionTx tx2 failed: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit tx2 failed: %v", err)
	}

	// Step 3: V3 (Patch) — Withdraw SKU-02 and SKU-04 as well.
	// Modify price of previously withdrawn SKU-01 while keeping it WITHDRAWN.
	p02Live, _ := s.GetProduct(ctx, "SKU-02")
	oldName02 := p02Live.Name
	oldPrice02 := p02Live.Price
	oldStatus02 := p02Live.Status
	oldTime02 := p02Live.SupplierTimestamp

	p04Live, _ := s.GetProduct(ctx, "SKU-04")
	oldName04 := p04Live.Name
	oldPrice04 := p04Live.Price
	oldStatus04 := p04Live.Status
	oldTime04 := p04Live.SupplierTimestamp

	p01Live, _ := s.GetProduct(ctx, "SKU-01")
	oldName01 := p01Live.Name
	oldPrice01 := p01Live.Price
	oldStatus01 := p01Live.Status
	oldTime01 := p01Live.SupplierTimestamp

	tx3, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx3 failed: %v", err)
	}

	_ = s.UpsertProductTx(ctx, tx3, &domain.Product{
		SKU: "SKU-02", Name: "Item SKU-02 V3 (Withdrawn)", Price: oldPrice02, Status: domain.StatusWithdrawn,
		SupplierTimestamp: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	})
	_ = s.UpsertProductTx(ctx, tx3, &domain.Product{
		SKU: "SKU-04", Name: "Item SKU-04 V3 (Withdrawn)", Price: oldPrice04, Status: domain.StatusWithdrawn,
		SupplierTimestamp: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	})
	_ = s.UpsertProductTx(ctx, tx3, &domain.Product{
		SKU: "SKU-01", Name: "Item SKU-01 V3 Withdrawn Discounted", Price: 5.0, Status: domain.StatusWithdrawn,
		SupplierTimestamp: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	})

	ver3 := &domain.CatalogVersion{
		VersionID:          "ver-inv-003",
		VersionNumber:      3,
		Description:        "V3: Withdraw 02, 04, update price of withdrawn 01",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypePatch,
		TotalItemsAffected: 3,
		Summary:            domain.ChangeSummary{Updated: 1, Withdrawn: 2},
		CreatedAt:          now.Add(-2 * time.Hour),
	}
	deltas3 := []domain.ItemDelta{
		{
			VersionID: "ver-inv-003", SKU: "SKU-02", Action: domain.DeltaActionWithdrawn,
			OldName: &oldName02, NewName: "Item SKU-02 V3 (Withdrawn)",
			OldPrice: &oldPrice02, NewPrice: oldPrice02,
			OldStatus: &oldStatus02, NewStatus: domain.StatusWithdrawn,
			OldTimestamp: &oldTime02, NewTimestamp: now.Add(-2 * time.Hour),
		},
		{
			VersionID: "ver-inv-003", SKU: "SKU-04", Action: domain.DeltaActionWithdrawn,
			OldName: &oldName04, NewName: "Item SKU-04 V3 (Withdrawn)",
			OldPrice: &oldPrice04, NewPrice: oldPrice04,
			OldStatus: &oldStatus04, NewStatus: domain.StatusWithdrawn,
			OldTimestamp: &oldTime04, NewTimestamp: now.Add(-2 * time.Hour),
		},
		{
			VersionID: "ver-inv-003", SKU: "SKU-01", Action: domain.DeltaActionUpdated,
			OldName: &oldName01, NewName: "Item SKU-01 V3 Withdrawn Discounted",
			OldPrice: &oldPrice01, NewPrice: 5.0,
			OldStatus: &oldStatus01, NewStatus: domain.StatusWithdrawn,
			OldTimestamp: &oldTime01, NewTimestamp: now.Add(-2 * time.Hour),
		},
	}
	if err := s.CreateVersionTx(ctx, tx3, ver3, deltas3); err != nil {
		t.Fatalf("CreateVersionTx tx3 failed: %v", err)
	}
	if err := tx3.Commit(); err != nil {
		t.Fatalf("Commit tx3 failed: %v", err)
	}

	// At this point (before rollback):
	// WITHDRAWN items: SKU-01, 03, 05, 07, 09 (from V2), SKU-02, 04 (from V3) -> 7 withdrawn items!
	// ACTIVE items: SKU-06, 08, 10 -> 3 active items.
	withdrawnSKUs := map[string]bool{
		"SKU-01": true, "SKU-02": true, "SKU-03": true, "SKU-04": true,
		"SKU-05": true, "SKU-07": true, "SKU-09": true,
	}

	// ------------------------------------------------------------------------
	// Challenge Step 4: ROLLBACK TO V1 (Creates V4)
	// In V1, ALL 10 ITEMS WERE ACTIVE!
	// Under Invariant A: NOT A SINGLE ONE OF THE 7 WITHDRAWN ITEMS MAY BE RESURRECTED!
	// ------------------------------------------------------------------------
	v4, deltasV4, err := s.RollbackVersion(ctx, "ver-inv-001", "challenger@coderpush.com", "Adversarial rollback to V1 where all items were active")
	if err != nil {
		t.Fatalf("RollbackVersion to V1 failed: %v", err)
	}
	if v4.VersionNumber != 4 {
		t.Errorf("expected version number 4, got %d", v4.VersionNumber)
	}

	// Verify live products table directly for all 10 items
	for sku := range withdrawnSKUs {
		prod, err := s.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("GetProduct %s failed: %v", sku, err)
		}
		if prod.Status != domain.StatusWithdrawn {
			t.Fatalf("INVARIANT A VIOLATION: Item %s was RESURRECTED to status %s upon rollback to V1!", sku, prod.Status)
		}
	}

	// Check that active product query returns ONLY the 3 items that were never withdrawn
	activeList, totalActive, err := s.ListProducts(ctx, "ACTIVE", "", 100, 0)
	if err != nil {
		t.Fatalf("ListProducts ACTIVE failed: %v", err)
	}
	if totalActive != 3 || len(activeList) != 3 {
		t.Fatalf("expected exactly 3 active products, got total=%d, len=%d", totalActive, len(activeList))
	}
	for _, p := range activeList {
		if withdrawnSKUs[p.SKU] {
			t.Fatalf("INVARIANT A VIOLATION: Withdrawn item %s appeared in active products list!", p.SKU)
		}
	}

	// Verify that deltas recorded in V4 never set NewStatus to ACTIVE for any previously withdrawn item
	for _, d := range deltasV4 {
		if withdrawnSKUs[d.SKU] && d.NewStatus == domain.StatusActive {
			t.Fatalf("INVARIANT A VIOLATION: Delta for %s has NewStatus=ACTIVE in rollback version %s!", d.SKU, v4.VersionID)
		}
	}

	// ------------------------------------------------------------------------
	// Challenge Step 5: V5 (Patch) — Withdraw SKU-06.
	// Now 8 items are withdrawn: SKU-01, 02, 03, 04, 05, 06, 07, 09.
	// ------------------------------------------------------------------------
	withdrawnSKUs["SKU-06"] = true
	p06Live, _ := s.GetProduct(ctx, "SKU-06")
	oldName06 := p06Live.Name
	oldPrice06 := p06Live.Price
	oldStatus06 := p06Live.Status
	oldTime06 := p06Live.SupplierTimestamp

	tx5, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx5 failed: %v", err)
	}
	_ = s.UpsertProductTx(ctx, tx5, &domain.Product{
		SKU: "SKU-06", Name: "Item SKU-06 V5 (Withdrawn)", Price: oldPrice06, Status: domain.StatusWithdrawn,
		SupplierTimestamp: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour),
	})
	ver5 := &domain.CatalogVersion{
		VersionID:          "ver-inv-005",
		VersionNumber:      5,
		Description:        "V5: Withdraw SKU-06",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypePatch,
		TotalItemsAffected: 1,
		Summary:            domain.ChangeSummary{Withdrawn: 1},
		CreatedAt:          now.Add(-1 * time.Hour),
	}
	deltas5 := []domain.ItemDelta{
		{
			VersionID: "ver-inv-005", SKU: "SKU-06", Action: domain.DeltaActionWithdrawn,
			OldName: &oldName06, NewName: "Item SKU-06 V5 (Withdrawn)",
			OldPrice: &oldPrice06, NewPrice: oldPrice06,
			OldStatus: &oldStatus06, NewStatus: domain.StatusWithdrawn,
			OldTimestamp: &oldTime06, NewTimestamp: now.Add(-1 * time.Hour),
		},
	}
	if err := s.CreateVersionTx(ctx, tx5, ver5, deltas5); err != nil {
		t.Fatalf("CreateVersionTx tx5 failed: %v", err)
	}
	if err := tx5.Commit(); err != nil {
		t.Fatalf("Commit tx5 failed: %v", err)
	}

	// ------------------------------------------------------------------------
	// Challenge Step 6: ROLLBACK TO V2 (Creates V6)
	// In V2, SKU-02, SKU-04, and SKU-06 WERE ACTIVE!
	// Live state has SKU-02, 04, 06 WITHDRAWN.
	// Rollback to V2 MUST NOT resurrect SKU-02, 04, 06 to ACTIVE!
	// ------------------------------------------------------------------------
	v6, deltasV6, err := s.RollbackVersion(ctx, "ver-inv-002", "challenger@coderpush.com", "Adversarial rollback to V2 where 02, 04, 06 were active")
	if err != nil {
		t.Fatalf("RollbackVersion to V2 failed: %v", err)
	}
	if v6.VersionNumber != 6 {
		t.Errorf("expected version number 6, got %d", v6.VersionNumber)
	}

	// Assert that all 8 withdrawn items remain WITHDRAWN
	for sku := range withdrawnSKUs {
		prod, err := s.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("GetProduct %s failed: %v", sku, err)
		}
		if prod.Status != domain.StatusWithdrawn {
			t.Fatalf("INVARIANT A VIOLATION: Item %s was RESURRECTED to status %s upon rollback to V2!", sku, prod.Status)
		}
	}

	// Check active list contains only SKU-08 and SKU-10
	activeList6, totalActive6, err := s.ListProducts(ctx, "ACTIVE", "", 100, 0)
	if err != nil {
		t.Fatalf("ListProducts ACTIVE failed: %v", err)
	}
	if totalActive6 != 2 || len(activeList6) != 2 {
		t.Fatalf("expected exactly 2 active products, got total=%d, len=%d", totalActive6, len(activeList6))
	}
	for _, p := range activeList6 {
		if p.SKU != "SKU-08" && p.SKU != "SKU-10" {
			t.Fatalf("INVARIANT A VIOLATION: Unexpected active product %s after rollback to V2!", p.SKU)
		}
	}

	// Verify deltas in V6: zero resurrection deltas
	for _, d := range deltasV6 {
		if withdrawnSKUs[d.SKU] && d.NewStatus == domain.StatusActive {
			t.Fatalf("INVARIANT A VIOLATION: Delta for %s set NewStatus=ACTIVE in rollback V6!", d.SKU)
		}
	}

	// ------------------------------------------------------------------------
	// Challenge Step 7: ROLLBACK TO V4 (Target is a ROLLBACK version!) (Creates V7)
	// Target V4 was already a rollback.
	// Verify that Invariant A still strictly holds!
	// ------------------------------------------------------------------------
	v7, _, err := s.RollbackVersion(ctx, v4.VersionID, "challenger@coderpush.com", "Rollback to rollback version V4")
	if err != nil {
		t.Fatalf("RollbackVersion to V4 failed: %v", err)
	}
	if v7.VersionNumber != 7 {
		t.Errorf("expected version number 7, got %d", v7.VersionNumber)
	}

	for sku := range withdrawnSKUs {
		prod, err := s.GetProduct(ctx, sku)
		if err != nil {
			t.Fatalf("GetProduct %s failed: %v", sku, err)
		}
		if prod.Status != domain.StatusWithdrawn {
			t.Fatalf("INVARIANT A VIOLATION: Item %s was RESURRECTED upon rollback to rollback version V4!", sku)
		}
	}
}

// ----------------------------------------------------------------------------
// 2. Chained Rollbacks Test
// Test V1 -> V2 -> V3 -> Rollback(V1) -> Rollback(V2) -> Rollback(V3).
// Verify version monotonicity, audit preservation (no historical row deleted
// in catalog_versions or catalog_version_deltas), and exact metadata convergence.
// ----------------------------------------------------------------------------
func TestChallengerM2_ChainedRollbacks_MonotonicityAuditAndConvergence(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Setup V1: SKU-ALPHA ($10.00, "Alpha Alpha"), SKU-BETA ($20.00, "Beta Beta")
	tx1, _ := s.BeginTx(ctx)
	pA1 := &domain.Product{SKU: "SKU-ALPHA", Name: "Alpha Alpha", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)}
	pB1 := &domain.Product{SKU: "SKU-BETA", Name: "Beta Beta", Price: 20.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)}
	_ = s.UpsertProductTx(ctx, tx1, pA1)
	_ = s.UpsertProductTx(ctx, tx1, pB1)
	ver1 := &domain.CatalogVersion{
		VersionID: "ver-chain-001", VersionNumber: 1, Description: "V1 Base", Author: "alice",
		Type: domain.VersionTypeInitial, TotalItemsAffected: 2, Summary: domain.ChangeSummary{Added: 2}, CreatedAt: now.Add(-3 * time.Hour),
	}
	deltas1 := []domain.ItemDelta{
		{VersionID: "ver-chain-001", SKU: "SKU-ALPHA", Action: domain.DeltaActionAdded, NewName: "Alpha Alpha", NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now.Add(-3 * time.Hour)},
		{VersionID: "ver-chain-001", SKU: "SKU-BETA", Action: domain.DeltaActionAdded, NewName: "Beta Beta", NewPrice: 20.00, NewStatus: domain.StatusActive, NewTimestamp: now.Add(-3 * time.Hour)},
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	_ = tx1.Commit()

	// Setup V2: SKU-ALPHA ($15.00, "Alpha Updated"), SKU-BETA ($25.00, "Beta Updated")
	tx2, _ := s.BeginTx(ctx)
	pA2 := &domain.Product{SKU: "SKU-ALPHA", Name: "Alpha Updated", Price: 15.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)}
	pB2 := &domain.Product{SKU: "SKU-BETA", Name: "Beta Updated", Price: 25.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)}
	_ = s.UpsertProductTx(ctx, tx2, pA2)
	_ = s.UpsertProductTx(ctx, tx2, pB2)
	ver2 := &domain.CatalogVersion{
		VersionID: "ver-chain-002", VersionNumber: 2, Description: "V2 Mod", Author: "bob",
		Type: domain.VersionTypePatch, TotalItemsAffected: 2, Summary: domain.ChangeSummary{Updated: 2}, CreatedAt: now.Add(-2 * time.Hour),
	}
	oldNameA1 := "Alpha Alpha"
	oldPriceA1 := 10.00
	oldStatusA1 := domain.StatusActive
	oldTimeA1 := now.Add(-3 * time.Hour)
	oldNameB1 := "Beta Beta"
	oldPriceB1 := 20.00
	oldStatusB1 := domain.StatusActive
	oldTimeB1 := now.Add(-3 * time.Hour)
	deltas2 := []domain.ItemDelta{
		{VersionID: "ver-chain-002", SKU: "SKU-ALPHA", Action: domain.DeltaActionUpdated, OldName: &oldNameA1, NewName: "Alpha Updated", OldPrice: &oldPriceA1, NewPrice: 15.00, OldStatus: &oldStatusA1, NewStatus: domain.StatusActive, OldTimestamp: &oldTimeA1, NewTimestamp: now.Add(-2 * time.Hour)},
		{VersionID: "ver-chain-002", SKU: "SKU-BETA", Action: domain.DeltaActionUpdated, OldName: &oldNameB1, NewName: "Beta Updated", OldPrice: &oldPriceB1, NewPrice: 25.00, OldStatus: &oldStatusB1, NewStatus: domain.StatusActive, OldTimestamp: &oldTimeB1, NewTimestamp: now.Add(-2 * time.Hour)},
	}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltas2)
	_ = tx2.Commit()

	// Setup V3: SKU-ALPHA ($18.00, "Alpha Final"), SKU-BETA ($28.00, "Beta Final")
	tx3, _ := s.BeginTx(ctx)
	pA3 := &domain.Product{SKU: "SKU-ALPHA", Name: "Alpha Final", Price: 18.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)}
	pB3 := &domain.Product{SKU: "SKU-BETA", Name: "Beta Final", Price: 28.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)}
	_ = s.UpsertProductTx(ctx, tx3, pA3)
	_ = s.UpsertProductTx(ctx, tx3, pB3)
	ver3 := &domain.CatalogVersion{
		VersionID: "ver-chain-003", VersionNumber: 3, Description: "V3 Mod", Author: "charlie",
		Type: domain.VersionTypePatch, TotalItemsAffected: 2, Summary: domain.ChangeSummary{Updated: 2}, CreatedAt: now.Add(-1 * time.Hour),
	}
	oldNameA2 := "Alpha Updated"
	oldPriceA2 := 15.00
	oldStatusA2 := domain.StatusActive
	oldTimeA2 := now.Add(-2 * time.Hour)
	oldNameB2 := "Beta Updated"
	oldPriceB2 := 25.00
	oldStatusB2 := domain.StatusActive
	oldTimeB2 := now.Add(-2 * time.Hour)
	deltas3 := []domain.ItemDelta{
		{VersionID: "ver-chain-003", SKU: "SKU-ALPHA", Action: domain.DeltaActionUpdated, OldName: &oldNameA2, NewName: "Alpha Final", OldPrice: &oldPriceA2, NewPrice: 18.00, OldStatus: &oldStatusA2, NewStatus: domain.StatusActive, OldTimestamp: &oldTimeA2, NewTimestamp: now.Add(-1 * time.Hour)},
		{VersionID: "ver-chain-003", SKU: "SKU-BETA", Action: domain.DeltaActionUpdated, OldName: &oldNameB2, NewName: "Beta Final", OldPrice: &oldPriceB2, NewPrice: 28.00, OldStatus: &oldStatusB2, NewStatus: domain.StatusActive, OldTimestamp: &oldTimeB2, NewTimestamp: now.Add(-1 * time.Hour)},
	}
	_ = s.CreateVersionTx(ctx, tx3, ver3, deltas3)
	_ = tx3.Commit()

	// Snapshot audit state at V3
	snapV3 := takeAuditSnapshot(t, s)
	if len(snapV3.versions) != 3 {
		t.Fatalf("expected 3 versions in snapshot, got %d", len(snapV3.versions))
	}

	// ------------------------------------------------------------------------
	// Phase 1: Rollback(V1) -> Creates V4
	// ------------------------------------------------------------------------
	v4, _, err := s.RollbackVersion(ctx, "ver-chain-001", "revert-user", "Rollback to V1")
	if err != nil {
		t.Fatalf("Rollback to V1 failed: %v", err)
	}
	// Monotonicity check
	if v4.VersionNumber != 4 {
		t.Errorf("expected version number 4, got %d", v4.VersionNumber)
	}
	if v4.Type != domain.VersionTypeRollback {
		t.Errorf("expected VersionTypeRollback, got %s", v4.Type)
	}
	if v4.RevertOfVersionID == nil || *v4.RevertOfVersionID != "ver-chain-001" {
		t.Errorf("expected RevertOfVersionID ver-chain-001, got %v", v4.RevertOfVersionID)
	}
	// Exact metadata convergence check to V1
	pA_at_V4, _ := s.GetProduct(ctx, "SKU-ALPHA")
	pB_at_V4, _ := s.GetProduct(ctx, "SKU-BETA")
	if pA_at_V4.Price != 10.00 || pA_at_V4.Name != "Alpha Alpha" || pA_at_V4.Status != domain.StatusActive {
		t.Fatalf("METADATA CONVERGENCE FAILURE at V4 (Rollback V1): SKU-ALPHA got %+v, expected price 10.00, name 'Alpha Alpha'", pA_at_V4)
	}
	if pB_at_V4.Price != 20.00 || pB_at_V4.Name != "Beta Beta" || pB_at_V4.Status != domain.StatusActive {
		t.Fatalf("METADATA CONVERGENCE FAILURE at V4 (Rollback V1): SKU-BETA got %+v, expected price 20.00, name 'Beta Beta'", pB_at_V4)
	}
	// Audit preservation check: V1, V2, V3 records must be identical
	verifyAuditPreservation(t, s, snapV3)

	// Snapshot audit state at V4
	snapV4 := takeAuditSnapshot(t, s)

	// ------------------------------------------------------------------------
	// Phase 2: Rollback(V2) -> Creates V5
	// ------------------------------------------------------------------------
	v5, _, err := s.RollbackVersion(ctx, "ver-chain-002", "revert-user", "Rollback to V2")
	if err != nil {
		t.Fatalf("Rollback to V2 failed: %v", err)
	}
	// Monotonicity check
	if v5.VersionNumber != 5 {
		t.Errorf("expected version number 5, got %d", v5.VersionNumber)
	}
	if v5.Type != domain.VersionTypeRollback {
		t.Errorf("expected VersionTypeRollback, got %s", v5.Type)
	}
	if v5.RevertOfVersionID == nil || *v5.RevertOfVersionID != "ver-chain-002" {
		t.Errorf("expected RevertOfVersionID ver-chain-002, got %v", v5.RevertOfVersionID)
	}
	// Exact metadata convergence check to V2
	pA_at_V5, _ := s.GetProduct(ctx, "SKU-ALPHA")
	pB_at_V5, _ := s.GetProduct(ctx, "SKU-BETA")
	if pA_at_V5.Price != 15.00 || pA_at_V5.Name != "Alpha Updated" || pA_at_V5.Status != domain.StatusActive {
		t.Fatalf("METADATA CONVERGENCE FAILURE at V5 (Rollback V2): SKU-ALPHA got %+v, expected price 15.00, name 'Alpha Updated'", pA_at_V5)
	}
	if pB_at_V5.Price != 25.00 || pB_at_V5.Name != "Beta Updated" || pB_at_V5.Status != domain.StatusActive {
		t.Fatalf("METADATA CONVERGENCE FAILURE at V5 (Rollback V2): SKU-BETA got %+v, expected price 25.00, name 'Beta Updated'", pB_at_V5)
	}
	// Audit preservation check: V1..V4 records must be intact
	verifyAuditPreservation(t, s, snapV4)

	// Snapshot audit state at V5
	snapV5 := takeAuditSnapshot(t, s)

	// ------------------------------------------------------------------------
	// Phase 3: Rollback(V3) -> Creates V6
	// ------------------------------------------------------------------------
	v6, _, err := s.RollbackVersion(ctx, "ver-chain-003", "revert-user", "Rollback to V3")
	if err != nil {
		t.Fatalf("Rollback to V3 failed: %v", err)
	}
	// Monotonicity check
	if v6.VersionNumber != 6 {
		t.Errorf("expected version number 6, got %d", v6.VersionNumber)
	}
	if v6.Type != domain.VersionTypeRollback {
		t.Errorf("expected VersionTypeRollback, got %s", v6.Type)
	}
	if v6.RevertOfVersionID == nil || *v6.RevertOfVersionID != "ver-chain-003" {
		t.Errorf("expected RevertOfVersionID ver-chain-003, got %v", v6.RevertOfVersionID)
	}
	// Exact metadata convergence check to V3
	pA_at_V6, _ := s.GetProduct(ctx, "SKU-ALPHA")
	pB_at_V6, _ := s.GetProduct(ctx, "SKU-BETA")
	if pA_at_V6.Price != 18.00 || pA_at_V6.Name != "Alpha Final" || pA_at_V6.Status != domain.StatusActive {
		t.Fatalf("METADATA CONVERGENCE FAILURE at V6 (Rollback V3): SKU-ALPHA got %+v, expected price 18.00, name 'Alpha Final'", pA_at_V6)
	}
	if pB_at_V6.Price != 28.00 || pB_at_V6.Name != "Beta Final" || pB_at_V6.Status != domain.StatusActive {
		t.Fatalf("METADATA CONVERGENCE FAILURE at V6 (Rollback V3): SKU-BETA got %+v, expected price 28.00, name 'Beta Final'", pB_at_V6)
	}
	// Audit preservation check: V1..V5 records must be completely intact
	verifyAuditPreservation(t, s, snapV5)

	// Comprehensive version history audit check:
	vers, total, err := s.ListVersions(ctx, 100, 0)
	if err != nil || total != 6 || len(vers) != 6 {
		t.Fatalf("expected exactly 6 versions total, got total=%d, len=%d, err=%v", total, len(vers), err)
	}
	// Confirm strict monotonicity: 6, 5, 4, 3, 2, 1
	for idx, expectedNum := range []int{6, 5, 4, 3, 2, 1} {
		if vers[idx].VersionNumber != expectedNum {
			t.Errorf("version at index %d expected number %d, got %d", idx, expectedNum, vers[idx].VersionNumber)
		}
	}
}

// ----------------------------------------------------------------------------
// 3. Post-Baseline Additions Test
// Verify items added in V2 are strictly WITHDRAWN (never deleted) upon rollback to V1.
// Tests multiple post-baseline additions, items added as ACTIVE and WITHDRAWN,
// and asserts zero database row deletions in products table.
// ----------------------------------------------------------------------------
func TestChallengerM2_PostBaselineAdditions_StrictlyWithdrawnNeverDeleted(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Setup V1: 2 baseline products
	tx1, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{SKU: "BASE-01", Name: "Base 1", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)})
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{SKU: "BASE-02", Name: "Base 2", Price: 20.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)})
	ver1 := &domain.CatalogVersion{
		VersionID: "ver-pba-001", VersionNumber: 1, Description: "Baseline with 2 products", Author: "ops",
		Type: domain.VersionTypeInitial, TotalItemsAffected: 2, Summary: domain.ChangeSummary{Added: 2}, CreatedAt: now.Add(-2 * time.Hour),
	}
	deltas1 := []domain.ItemDelta{
		{VersionID: "ver-pba-001", SKU: "BASE-01", Action: domain.DeltaActionAdded, NewName: "Base 1", NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now.Add(-2 * time.Hour)},
		{VersionID: "ver-pba-001", SKU: "BASE-02", Action: domain.DeltaActionAdded, NewName: "Base 2", NewPrice: 20.00, NewStatus: domain.StatusActive, NewTimestamp: now.Add(-2 * time.Hour)},
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	_ = tx1.Commit()

	// Setup V2: Add 5 new items (POST-01..03 ACTIVE, POST-04..05 WITHDRAWN)
	tx2, _ := s.BeginTx(ctx)
	postItems := []struct {
		sku    string
		name   string
		price  float64
		status domain.ProductStatus
	}{
		{"POST-01", "Post Item 1", 30.00, domain.StatusActive},
		{"POST-02", "Post Item 2", 40.00, domain.StatusActive},
		{"POST-03", "Post Item 3", 50.00, domain.StatusActive},
		{"POST-04", "Post Item 4", 60.00, domain.StatusWithdrawn},
		{"POST-05", "Post Item 5", 70.00, domain.StatusWithdrawn},
	}

	deltas2 := make([]domain.ItemDelta, 0, len(postItems))
	for _, it := range postItems {
		_ = s.UpsertProductTx(ctx, tx2, &domain.Product{
			SKU: it.sku, Name: it.name, Price: it.price, Status: it.status,
			SupplierTimestamp: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour),
		})
		action := domain.DeltaActionAdded
		if it.status == domain.StatusWithdrawn {
			action = domain.DeltaActionWithdrawn
		}
		deltas2 = append(deltas2, domain.ItemDelta{
			VersionID: "ver-pba-002", SKU: it.sku, Action: action,
			NewName: it.name, NewPrice: it.price, NewStatus: it.status,
			NewTimestamp: now.Add(-1 * time.Hour),
		})
	}
	ver2 := &domain.CatalogVersion{
		VersionID: "ver-pba-002", VersionNumber: 2, Description: "V2 adding 5 items", Author: "ops",
		Type: domain.VersionTypePatch, TotalItemsAffected: len(postItems),
		Summary: domain.ChangeSummary{Added: 3, Withdrawn: 2}, CreatedAt: now.Add(-1 * time.Hour),
	}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltas2)
	_ = tx2.Commit()

	// Verify total products before rollback = 2 baseline + 5 post = 7 products
	var countBefore int
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM products;").Scan(&countBefore)
	if countBefore != 7 {
		t.Fatalf("expected 7 products before rollback, got %d", countBefore)
	}

	// ------------------------------------------------------------------------
	// Action: Rollback to V1 -> Creates V3
	// ------------------------------------------------------------------------
	v3, rollbackDeltas, err := s.RollbackVersion(ctx, "ver-pba-001", "challenger", "Rollback to V1 before post additions")
	if err != nil {
		t.Fatalf("RollbackVersion failed: %v", err)
	}
	if v3 == nil {
		t.Fatal("expected non-nil v3")
	}

	// 1. STRICT NO-DELETION ASSERTION:
	// Verify table count is STILL EXACTLY 7! No row was deleted!
	var countAfter int
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM products;").Scan(&countAfter)
	if countAfter != 7 {
		t.Fatalf("DESTRUCTION AUDIT FAILURE: products table row count changed from %d to %d! Rows were deleted!", countBefore, countAfter)
	}

	// 2. STATUS ASSERTION:
	// All 5 post-baseline additions MUST be WITHDRAWN
	for _, it := range postItems {
		prod, err := s.GetProduct(ctx, it.sku)
		if err != nil {
			t.Fatalf("POST-BASELINE ITEM DELETED! Item %s not found in DB: %v", it.sku, err)
		}
		if prod.Status != domain.StatusWithdrawn {
			t.Fatalf("POST-BASELINE NOT WITHDRAWN: Item %s has status %s, expected WITHDRAWN", it.sku, prod.Status)
		}
		// Metadata must be preserved
		if prod.Price != it.price || prod.Name != it.name {
			t.Errorf("Metadata corrupted for post-baseline item %s: got %+v, expected price %f, name %s", it.sku, prod, it.price, it.name)
		}
	}

	// 3. ACTIVE CATALOG LISTING ASSERTION:
	// Only BASE-01 and BASE-02 must be returned in active listing
	activeList, totalActive, err := s.ListProducts(ctx, "ACTIVE", "", 100, 0)
	if err != nil {
		t.Fatalf("ListProducts ACTIVE failed: %v", err)
	}
	if totalActive != 2 || len(activeList) != 2 {
		t.Fatalf("expected 2 active products, got total=%d, len=%d", totalActive, len(activeList))
	}
	for _, p := range activeList {
		if p.SKU != "BASE-01" && p.SKU != "BASE-02" {
			t.Fatalf("INACTIVE ITEM LEAK: Found %s in active listing!", p.SKU)
		}
	}

	// 4. WITHDRAWN CATALOG LISTING ASSERTION:
	// Must contain all 5 post-baseline items
	withdrawnList, totalWithdrawn, err := s.ListProducts(ctx, "WITHDRAWN", "", 100, 0)
	if err != nil {
		t.Fatalf("ListProducts WITHDRAWN failed: %v", err)
	}
	if totalWithdrawn != 5 || len(withdrawnList) != 5 {
		t.Fatalf("expected 5 withdrawn products, got total=%d, len=%d", totalWithdrawn, len(withdrawnList))
	}

	// 5. DELTA ACTIONS CHECK:
	// Only POST-01, POST-02, POST-03 were ACTIVE at V2 and transitioned to WITHDRAWN in V3.
	// POST-04 and POST-05 were already WITHDRAWN in V2, so they required no transition.
	// Check delta actions in rollback:
	withdrawnDeltaMap := make(map[string]domain.ItemDelta)
	for _, d := range rollbackDeltas {
		withdrawnDeltaMap[d.SKU] = d
	}
	for _, sku := range []string{"POST-01", "POST-02", "POST-03"} {
		d, exists := withdrawnDeltaMap[sku]
		if !exists {
			t.Fatalf("expected delta for post-baseline item %s, but none was recorded", sku)
		}
		if d.Action != domain.DeltaActionWithdrawn {
			t.Errorf("expected delta action WITHDRAWN for %s, got %s", sku, d.Action)
		}
		if d.OldStatus == nil || *d.OldStatus != domain.StatusActive {
			t.Errorf("expected OldStatus ACTIVE for %s, got %v", sku, d.OldStatus)
		}
		if d.NewStatus != domain.StatusWithdrawn {
			t.Errorf("expected NewStatus WITHDRAWN for %s, got %s", sku, d.NewStatus)
		}
	}
}

// ----------------------------------------------------------------------------
// 4. Large-Scale Randomized Stress Harness (Tombstone Lock Guarantee)
// Generates 50 items across multiple lifecycle versions, randomly withdrawing
// items and executing rollbacks. Asserts that once an item enters the tombstone
// set, it can NEVER be resurrected across any number of subsequent rollbacks.
// ----------------------------------------------------------------------------
func TestChallengerM2_InvariantA_LargeScaleRandomHarness(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	rng := rand.New(rand.NewSource(42))

	const totalItems = 50
	tombstoneSet := make(map[string]bool)

	// V1: 50 items all ACTIVE
	tx1, _ := s.BeginTx(ctx)
	deltas1 := make([]domain.ItemDelta, 0, totalItems)
	for i := 1; i <= totalItems; i++ {
		sku := fmt.Sprintf("SKU-%03d", i)
		_ = s.UpsertProductTx(ctx, tx1, &domain.Product{
			SKU: sku, Name: fmt.Sprintf("Stress Item %s", sku), Price: float64(i * 5),
			Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now,
		})
		deltas1 = append(deltas1, domain.ItemDelta{
			VersionID: "ver-stress-001", SKU: sku, Action: domain.DeltaActionAdded,
			NewName: fmt.Sprintf("Stress Item %s", sku), NewPrice: float64(i * 5),
			NewStatus: domain.StatusActive, NewTimestamp: now,
		})
	}
	ver1 := &domain.CatalogVersion{
		VersionID: "ver-stress-001", VersionNumber: 1, Description: "V1 Base", Author: "stress",
		Type: domain.VersionTypeInitial, TotalItemsAffected: totalItems,
		Summary: domain.ChangeSummary{Added: totalItems}, CreatedAt: now,
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	_ = tx1.Commit()

	versionIDs := []string{"ver-stress-001"}

	// Maintain local state tracker to avoid querying DB while holding write transaction
	liveState := make(map[string]domain.Product)
	for i := 1; i <= totalItems; i++ {
		sku := fmt.Sprintf("SKU-%03d", i)
		liveState[sku] = domain.Product{
			SKU: sku, Name: fmt.Sprintf("Stress Item %s", sku), Price: float64(i * 5),
			Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now,
		}
	}

	// Create 5 patch versions with random status transitions & price modifications
	for step := 2; step <= 6; step++ {
		vID := fmt.Sprintf("ver-stress-%03d", step)
		deltas := make([]domain.ItemDelta, 0)
		summary := domain.ChangeSummary{}
		productsToUpdate := make([]domain.Product, 0)

		for i := 1; i <= totalItems; i++ {
			sku := fmt.Sprintf("SKU-%03d", i)
			live := liveState[sku]
			roll := rng.Float64()

			if live.Status == domain.StatusActive && roll < 0.25 {
				// 25% chance to withdraw an active item
				tombstoneSet[sku] = true
				oldName := live.Name
				oldPrice := live.Price
				oldStatus := live.Status
				oldTime := live.SupplierTimestamp
				updatedProd := domain.Product{
					SKU: sku, Name: live.Name, Price: live.Price, Status: domain.StatusWithdrawn,
					SupplierTimestamp: now.Add(time.Duration(step) * time.Hour),
					UpdatedAt:         now.Add(time.Duration(step) * time.Hour),
				}
				productsToUpdate = append(productsToUpdate, updatedProd)
				liveState[sku] = updatedProd
				deltas = append(deltas, domain.ItemDelta{
					VersionID: vID, SKU: sku, Action: domain.DeltaActionWithdrawn,
					OldName: &oldName, NewName: live.Name, OldPrice: &oldPrice, NewPrice: live.Price,
					OldStatus: &oldStatus, NewStatus: domain.StatusWithdrawn,
					OldTimestamp: &oldTime, NewTimestamp: now.Add(time.Duration(step) * time.Hour),
				})
				summary.Withdrawn++
			} else if roll < 0.50 {
				// 25% chance to update price
				newPrice := live.Price + float64(rng.Intn(10)+1)
				oldName := live.Name
				oldPrice := live.Price
				oldStatus := live.Status
				oldTime := live.SupplierTimestamp
				updatedProd := domain.Product{
					SKU: sku, Name: live.Name, Price: newPrice, Status: live.Status,
					SupplierTimestamp: now.Add(time.Duration(step) * time.Hour),
					UpdatedAt:         now.Add(time.Duration(step) * time.Hour),
				}
				productsToUpdate = append(productsToUpdate, updatedProd)
				liveState[sku] = updatedProd
				deltas = append(deltas, domain.ItemDelta{
					VersionID: vID, SKU: sku, Action: domain.DeltaActionUpdated,
					OldName: &oldName, NewName: live.Name, OldPrice: &oldPrice, NewPrice: newPrice,
					OldStatus: &oldStatus, NewStatus: live.Status,
					OldTimestamp: &oldTime, NewTimestamp: now.Add(time.Duration(step) * time.Hour),
				})
				summary.Updated++
			}
		}

		tx, err := s.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx failed at step %d: %v", step, err)
		}
		for _, p := range productsToUpdate {
			pCopy := p
			if err := s.UpsertProductTx(ctx, tx, &pCopy); err != nil {
				t.Fatalf("UpsertProductTx failed: %v", err)
			}
		}

		ver := &domain.CatalogVersion{
			VersionID: vID, VersionNumber: step, Description: fmt.Sprintf("V%d Patch", step),
			Author: "stress", Type: domain.VersionTypePatch, TotalItemsAffected: len(deltas),
			Summary: summary, CreatedAt: now.Add(time.Duration(step) * time.Hour),
		}
		if err := s.CreateVersionTx(ctx, tx, ver, deltas); err != nil {
			t.Fatalf("CreateVersionTx failed: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
		versionIDs = append(versionIDs, vID)
	}

	// Now perform 10 random rollbacks to earlier versions
	for rbIdx := 1; rbIdx <= 10; rbIdx++ {
		targetVersionID := versionIDs[rng.Intn(len(versionIDs))]
		rbVer, _, err := s.RollbackVersion(ctx, targetVersionID, "stress-bot", fmt.Sprintf("Random rollback %d", rbIdx))
		if err != nil {
			t.Fatalf("Rollback to %s failed at step %d: %v", targetVersionID, rbIdx, err)
		}
		versionIDs = append(versionIDs, rbVer.VersionID)

		// INVARIANT A HARNESS CHECK:
		// Check every tombstoned item in SQLite products table
		for tombSKU := range tombstoneSet {
			prod, err := s.GetProduct(ctx, tombSKU)
			if err != nil {
				t.Fatalf("GetProduct %s failed: %v", tombSKU, err)
			}
			if prod.Status != domain.StatusWithdrawn {
				t.Fatalf("INVARIANT A VIOLATION in stress test: Item %s was resurrected to %s after rollback to %s!", tombSKU, prod.Status, targetVersionID)
			}
		}

		// Also check SQL directly:
		var resurrectedCount int
		err = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM products WHERE status = 'ACTIVE' AND sku IN (SELECT sku FROM catalog_version_deltas WHERE new_status = 'WITHDRAWN');").Scan(&resurrectedCount)
		if err != nil {
			t.Fatalf("QueryRowContext failed: %v", err)
		}
		if resurrectedCount != 0 {
			t.Fatalf("INVARIANT A VIOLATION: Found %d resurrected items in products table!", resurrectedCount)
		}
	}
}

// ----------------------------------------------------------------------------
// 5. Edge Case: Rollback of a Rollback and Identical State Re-Rollback
// ----------------------------------------------------------------------------
func TestChallengerM2_RollbackOfRollbackAndIdenticalReRollback(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// V1: Product X ($10.00, ACTIVE)
	tx1, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{SKU: "SKU-X", Name: "Product X V1", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now})
	ver1 := &domain.CatalogVersion{VersionID: "ver-rb-001", VersionNumber: 1, Description: "V1", Author: "a", Type: domain.VersionTypeInitial, TotalItemsAffected: 1, Summary: domain.ChangeSummary{Added: 1}, CreatedAt: now}
	_ = s.CreateVersionTx(ctx, tx1, ver1, []domain.ItemDelta{{VersionID: "ver-rb-001", SKU: "SKU-X", Action: domain.DeltaActionAdded, NewName: "Product X V1", NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now}})
	_ = tx1.Commit()

	// V2: Product X ($20.00, ACTIVE)
	tx2, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx2, &domain.Product{SKU: "SKU-X", Name: "Product X V2", Price: 20.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour)})
	ver2 := &domain.CatalogVersion{VersionID: "ver-rb-002", VersionNumber: 2, Description: "V2", Author: "b", Type: domain.VersionTypePatch, TotalItemsAffected: 1, Summary: domain.ChangeSummary{Updated: 1}, CreatedAt: now.Add(time.Hour)}
	oldP := 10.00
	oldN := "Product X V1"
	oldS := domain.StatusActive
	_ = s.CreateVersionTx(ctx, tx2, ver2, []domain.ItemDelta{{VersionID: "ver-rb-002", SKU: "SKU-X", Action: domain.DeltaActionUpdated, OldName: &oldN, NewName: "Product X V2", OldPrice: &oldP, NewPrice: 20.00, OldStatus: &oldS, NewStatus: domain.StatusActive, NewTimestamp: now.Add(time.Hour)}})
	_ = tx2.Commit()

	// Rollback 1: Rollback to V1 -> Creates V3
	v3, _, err := s.RollbackVersion(ctx, "ver-rb-001", "c", "Rollback to V1")
	if err != nil {
		t.Fatalf("Rollback to V1 failed: %v", err)
	}

	// Immediately Rollback to V1 AGAIN from V3 -> Creates V4
	// Live catalog is ALREADY at V1 baseline state.
	// This should be a clean idempotent no-op rollback creating a forward version with 0 deltas.
	v4, deltas4, err := s.RollbackVersion(ctx, "ver-rb-001", "c", "Rollback to V1 again (no-op)")
	if err != nil {
		t.Fatalf("Re-rollback to V1 failed: %v", err)
	}
	if v4.VersionNumber != 4 {
		t.Errorf("expected version number 4, got %d", v4.VersionNumber)
	}
	if len(deltas4) != 0 || v4.TotalItemsAffected != 0 {
		t.Errorf("expected 0 deltas for idempotent re-rollback, got %d deltas, %d items affected", len(deltas4), v4.TotalItemsAffected)
	}

	// Now Rollback to V3 (which is itself a ROLLBACK version!) -> Creates V5
	v5, _, err := s.RollbackVersion(ctx, v3.VersionID, "d", "Rollback to V3 (rollback of rollback)")
	if err != nil {
		t.Fatalf("Rollback to rollback version V3 failed: %v", err)
	}
	if v5.VersionNumber != 5 {
		t.Errorf("expected version number 5, got %d", v5.VersionNumber)
	}

	// Now Rollback to V2 from V5 -> Creates V6
	v6, _, err := s.RollbackVersion(ctx, "ver-rb-002", "e", "Rollback to V2 from V5")
	if err != nil {
		t.Fatalf("Rollback to V2 failed: %v", err)
	}
	if v6.VersionNumber != 6 {
		t.Errorf("expected version number 6, got %d", v6.VersionNumber)
	}
	pX_at_V6, _ := s.GetProduct(ctx, "SKU-X")
	if pX_at_V6.Price != 20.00 || pX_at_V6.Name != "Product X V2" {
		t.Fatalf("expected Product X reverted to V2 ($20.00), got %+v", pX_at_V6)
	}
}
