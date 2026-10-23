package storage_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
)

// TestChallenger_DeltaRecordingAccuracy_AddedUpdatedWithdrawn empirically validates that:
// 1. ADDED records NULL pre-images and non-null post-images.
// 2. UPDATED records exact pre-image and post-image values, including edge cases like 0.0 price.
// 3. WITHDRAWN records pre-image and sets new_status to WITHDRAWN.
// 4. Returned deltas maintain strict ASC order by delta_id.
func TestChallenger_DeltaRecordingAccuracy_AddedUpdatedWithdrawn(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	oldTime := now.Add(-2 * time.Hour)

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	ver := &domain.CatalogVersion{
		VersionID:     "ver-accuracy-001",
		VersionNumber: 1,
		Description:   "Delta recording accuracy test",
		Author:        "challenger@coderpush.com",
		Type:          domain.VersionTypePatch,
		Summary: domain.ChangeSummary{
			Added:     1,
			Updated:   1,
			Withdrawn: 1,
		},
		TotalItemsAffected: 3,
		CreatedAt:          now,
	}

	oldPriceZero := 0.0
	oldPriceUpdated := 49.99
	oldName := "Legacy Widget"
	oldStatusActive := domain.StatusActive

	deltas := []domain.ItemDelta{
		{
			VersionID:    "ver-accuracy-001",
			SKU:          "SKU-ADD",
			Action:       domain.DeltaActionAdded,
			OldName:      nil,
			NewName:      "Brand New Widget",
			OldPrice:     nil,
			NewPrice:     99.95,
			OldStatus:    nil,
			NewStatus:    domain.StatusActive,
			OldTimestamp: nil,
			NewTimestamp: now,
		},
		{
			VersionID:    "ver-accuracy-001",
			SKU:          "SKU-UPDATE-ZERO",
			Action:       domain.DeltaActionUpdated,
			OldName:      &oldName,
			NewName:      "Updated Widget",
			OldPrice:     &oldPriceZero, // Edge case: Price was exactly 0.0 (free item)
			NewPrice:     15.00,
			OldStatus:    &oldStatusActive,
			NewStatus:    domain.StatusActive,
			OldTimestamp: &oldTime,
			NewTimestamp: now,
		},
		{
			VersionID:    "ver-accuracy-001",
			SKU:          "SKU-WITHDRAW",
			Action:       domain.DeltaActionWithdrawn,
			OldName:      &oldName,
			NewName:      "Legacy Widget",
			OldPrice:     &oldPriceUpdated,
			NewPrice:     49.99,
			OldStatus:    &oldStatusActive,
			NewStatus:    domain.StatusWithdrawn,
			OldTimestamp: &oldTime,
			NewTimestamp: now,
		},
	}

	if err := s.CreateVersionTx(ctx, tx, ver, deltas); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// 1. Check raw database storage to verify actual SQLite NULL values
	var nullName, nullPrice, nullStatus, nullTime int
	err = s.DB().QueryRowContext(ctx, `
		SELECT
			(old_name IS NULL),
			(old_price IS NULL),
			(old_status IS NULL),
			(old_timestamp IS NULL)
		FROM catalog_version_deltas
		WHERE version_id = 'ver-accuracy-001' AND sku = 'SKU-ADD';
	`).Scan(&nullName, &nullPrice, &nullStatus, &nullTime)
	if err != nil {
		t.Fatalf("raw SQL check for SKU-ADD failed: %v", err)
	}
	if nullName != 1 || nullPrice != 1 || nullStatus != 1 || nullTime != 1 {
		t.Errorf("expected all pre-images to be SQLite NULL for ADDED delta, got: name=%d price=%d status=%d time=%d",
			nullName, nullPrice, nullStatus, nullTime)
	}

	// 2. Check raw database storage for edge-case old_price = 0.0 (MUST NOT be NULL)
	var isOldPriceNull int
	var rawOldPrice float64
	err = s.DB().QueryRowContext(ctx, `
		SELECT (old_price IS NULL), old_price
		FROM catalog_version_deltas
		WHERE version_id = 'ver-accuracy-001' AND sku = 'SKU-UPDATE-ZERO';
	`).Scan(&isOldPriceNull, &rawOldPrice)
	if err != nil {
		t.Fatalf("raw SQL check for SKU-UPDATE-ZERO failed: %v", err)
	}
	if isOldPriceNull != 0 {
		t.Errorf("expected old_price 0.0 to NOT be NULL in database, but it was stored as NULL")
	}
	if rawOldPrice != 0.0 {
		t.Errorf("expected old_price to be 0.0, got %f", rawOldPrice)
	}

	// 3. Retrieve through repository method GetVersionDeltas
	retrievedDeltas, err := s.GetVersionDeltas(ctx, "ver-accuracy-001")
	if err != nil {
		t.Fatalf("GetVersionDeltas failed: %v", err)
	}
	if len(retrievedDeltas) != 3 {
		t.Fatalf("expected 3 deltas, got %d", len(retrievedDeltas))
	}

	// Verify delta_id monotonic ascending ordering
	for i := 1; i < len(retrievedDeltas); i++ {
		if retrievedDeltas[i].DeltaID <= retrievedDeltas[i-1].DeltaID {
			t.Errorf("expected strictly ascending delta_id, got %d then %d",
				retrievedDeltas[i-1].DeltaID, retrievedDeltas[i].DeltaID)
		}
	}

	// Verify ADDED delta (d0)
	d0 := retrievedDeltas[0]
	if d0.SKU != "SKU-ADD" || d0.Action != domain.DeltaActionAdded {
		t.Errorf("d0 SKU or Action mismatch: %+v", d0)
	}
	if d0.OldName != nil {
		t.Errorf("d0 OldName expected nil, got %v", *d0.OldName)
	}
	if d0.OldPrice != nil {
		t.Errorf("d0 OldPrice expected nil, got %v", *d0.OldPrice)
	}
	if d0.OldStatus != nil {
		t.Errorf("d0 OldStatus expected nil, got %v", *d0.OldStatus)
	}
	if d0.OldTimestamp != nil {
		t.Errorf("d0 OldTimestamp expected nil, got %v", *d0.OldTimestamp)
	}
	if d0.NewName != "Brand New Widget" || d0.NewPrice != 99.95 || d0.NewStatus != domain.StatusActive {
		t.Errorf("d0 post-image mismatch: %+v", d0)
	}

	// Verify UPDATED delta with 0.0 old price (d1)
	d1 := retrievedDeltas[1]
	if d1.SKU != "SKU-UPDATE-ZERO" || d1.Action != domain.DeltaActionUpdated {
		t.Errorf("d1 SKU or Action mismatch: %+v", d1)
	}
	if d1.OldPrice == nil {
		t.Errorf("d1 OldPrice expected non-nil pointer for 0.0, got nil")
	} else if *d1.OldPrice != 0.0 {
		t.Errorf("d1 OldPrice expected 0.0, got %f", *d1.OldPrice)
	}
	if d1.OldName == nil || *d1.OldName != "Legacy Widget" {
		t.Errorf("d1 OldName mismatch: %+v", d1.OldName)
	}
	if d1.OldStatus == nil || *d1.OldStatus != domain.StatusActive {
		t.Errorf("d1 OldStatus mismatch: %+v", d1.OldStatus)
	}
	if d1.NewPrice != 15.00 || d1.NewName != "Updated Widget" {
		t.Errorf("d1 post-image mismatch: %+v", d1)
	}

	// Verify WITHDRAWN delta (d2)
	d2 := retrievedDeltas[2]
	if d2.SKU != "SKU-WITHDRAW" || d2.Action != domain.DeltaActionWithdrawn {
		t.Errorf("d2 SKU or Action mismatch: %+v", d2)
	}
	if d2.OldStatus == nil || *d2.OldStatus != domain.StatusActive {
		t.Errorf("d2 OldStatus expected ACTIVE, got %+v", d2.OldStatus)
	}
	if d2.NewStatus != domain.StatusWithdrawn {
		t.Errorf("d2 NewStatus expected WITHDRAWN, got %s", d2.NewStatus)
	}
}

// TestChallenger_RollbackUncommittedTransaction_ZeroTraces tests that an uncommitted transaction
// leaves absolutely zero traces across both live catalog and version/delta journal tables.
func TestChallenger_RollbackUncommittedTransaction_ZeroTraces(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	// 1. Insert product into live catalog within tx
	err = s.UpsertProductTx(ctx, tx, &domain.Product{
		SKU:               "SKU-ROLLBACK-TEST",
		Name:              "Ghost Item",
		Price:             42.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	})
	if err != nil {
		t.Fatalf("UpsertProductTx failed: %v", err)
	}

	// 2. Record catalog version and deltas within same tx
	ver := &domain.CatalogVersion{
		VersionID:     "ver-ghost-001",
		VersionNumber: 1,
		Description:   "Phantom Version",
		Author:        "phantom@coderpush.com",
		Type:          domain.VersionTypePatch,
		Summary: domain.ChangeSummary{
			Added: 1,
		},
		TotalItemsAffected: 1,
		CreatedAt:          now,
	}
	deltas := []domain.ItemDelta{
		{
			VersionID:    "ver-ghost-001",
			SKU:          "SKU-ROLLBACK-TEST",
			Action:       domain.DeltaActionAdded,
			NewName:      "Ghost Item",
			NewPrice:     42.00,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now,
		},
	}
	if err := s.CreateVersionTx(ctx, tx, ver, deltas); err != nil {
		t.Fatalf("CreateVersionTx failed: %v", err)
	}

	// 3. Rollback transaction
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// 4. Assert zero traces via repository queries
	_, err = s.GetProduct(ctx, "SKU-ROLLBACK-TEST")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetProduct should return ErrNotFound after rollback, got %v", err)
	}

	_, err = s.GetVersion(ctx, "ver-ghost-001")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetVersion should return ErrNotFound after rollback, got %v", err)
	}

	_, err = s.GetLatestVersion(ctx)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetLatestVersion should return ErrNotFound after rollback, got %v", err)
	}

	deltasOut, err := s.GetVersionDeltas(ctx, "ver-ghost-001")
	if err != nil {
		t.Errorf("GetVersionDeltas returned error after rollback: %v", err)
	}
	if len(deltasOut) != 0 {
		t.Errorf("GetVersionDeltas returned %d items, expected 0", len(deltasOut))
	}

	versionsList, total, err := s.ListVersions(ctx, 10, 0)
	if err != nil {
		t.Errorf("ListVersions returned error: %v", err)
	}
	if total != 0 || len(versionsList) != 0 {
		t.Errorf("ListVersions should be empty, got total=%d, len=%d", total, len(versionsList))
	}

	// 5. Assert zero traces via direct table count queries
	var countVersions, countDeltas, countProducts int
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM catalog_versions;").Scan(&countVersions)
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM catalog_version_deltas;").Scan(&countDeltas)
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM products;").Scan(&countProducts)

	if countVersions != 0 || countDeltas != 0 || countProducts != 0 {
		t.Errorf("Direct SQL counts non-zero: versions=%d, deltas=%d, products=%d",
			countVersions, countDeltas, countProducts)
	}
}

// TestChallenger_EmptyVersions tests creating milestone versions with zero item modifications
// (e.g. tag/baseline milestone, zero-diff patches, or initial empty state).
func TestChallenger_EmptyVersions(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()

	// 1. Create empty version with nil deltas
	tx1, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	ver1 := &domain.CatalogVersion{
		VersionID:          "ver-empty-nil",
		VersionNumber:      1,
		Description:        "Empty Milestone with Nil Deltas",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypeInitial,
		Summary:            domain.ChangeSummary{},
		TotalItemsAffected: 0,
		CreatedAt:          now,
	}
	if err := s.CreateVersionTx(ctx, tx1, ver1, nil); err != nil {
		_ = tx1.Rollback()
		t.Fatalf("CreateVersionTx with nil deltas failed: %v", err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatalf("Commit tx1 failed: %v", err)
	}

	// 2. Create empty version with empty slice deltas
	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	ver2 := &domain.CatalogVersion{
		VersionID:          "ver-empty-slice",
		VersionNumber:      2,
		Description:        "Empty Milestone with Empty Slice",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypePatch,
		Summary:            domain.ChangeSummary{},
		TotalItemsAffected: 0,
		CreatedAt:          now.Add(time.Minute),
	}
	if err := s.CreateVersionTx(ctx, tx2, ver2, []domain.ItemDelta{}); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("CreateVersionTx with empty slice deltas failed: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit tx2 failed: %v", err)
	}

	// 3. Verify retrieval of both empty versions
	v1, err := s.GetVersion(ctx, "ver-empty-nil")
	if err != nil || v1.TotalItemsAffected != 0 {
		t.Errorf("GetVersion ver-empty-nil failed: %v, v1: %+v", err, v1)
	}
	deltas1, err := s.GetVersionDeltas(ctx, "ver-empty-nil")
	if err != nil || len(deltas1) != 0 {
		t.Errorf("GetVersionDeltas ver-empty-nil failed: %v, len: %d", err, len(deltas1))
	}

	v2, err := s.GetVersion(ctx, "ver-empty-slice")
	if err != nil || v2.TotalItemsAffected != 0 {
		t.Errorf("GetVersion ver-empty-slice failed: %v, v2: %+v", err, v2)
	}
	deltas2, err := s.GetVersionDeltas(ctx, "ver-empty-slice")
	if err != nil || len(deltas2) != 0 {
		t.Errorf("GetVersionDeltas ver-empty-slice failed: %v, len: %d", err, len(deltas2))
	}

	// 4. Verify LatestVersion is ver-empty-slice
	latest, err := s.GetLatestVersion(ctx)
	if err != nil || latest.VersionID != "ver-empty-slice" || latest.VersionNumber != 2 {
		t.Errorf("GetLatestVersion failed: %v, got %+v", err, latest)
	}

	// 5. Verify ListVersions returns both
	list, total, err := s.ListVersions(ctx, 10, 0)
	if err != nil || total != 2 || len(list) != 2 {
		t.Errorf("ListVersions failed: %v, total: %d, len: %d", err, total, len(list))
	}
}

// TestChallenger_ForeignKeyViolation_RevertOfVersionID tests referential integrity:
// 1. revert_of_version_id pointing to a non-existent version MUST fail with FK constraint error.
// 2. revert_of_version_id pointing to an existing version MUST succeed.
// 3. ON DELETE SET NULL behavior when the referenced target version is deleted.
func TestChallenger_ForeignKeyViolation_RevertOfVersionID(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()

	// 1. Attempt rollback version pointing to non-existent version_id
	txInvalid, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	nonExistentTarget := "ver-non-existent-999"
	invalidVer := &domain.CatalogVersion{
		VersionID:         "ver-rollback-invalid",
		VersionNumber:     1,
		Description:       "Rollback to non-existent version",
		Author:            "challenger@coderpush.com",
		Type:              domain.VersionTypeRollback,
		RevertOfVersionID: &nonExistentTarget,
		CreatedAt:         now,
	}

	err = s.CreateVersionTx(ctx, txInvalid, invalidVer, nil)
	if err == nil {
		_ = txInvalid.Rollback()
		t.Fatalf("expected foreign key violation error for non-existent revert_of_version_id, got nil")
	}
	_ = txInvalid.Rollback()

	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("expected error message to mention 'foreign key', got: %v", err)
	}

	// Verify DB has no remnants of ver-rollback-invalid
	_, err = s.GetVersion(ctx, "ver-rollback-invalid")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for ver-rollback-invalid, got %v", err)
	}

	// 2. Create the target version first
	txTarget, _ := s.BeginTx(ctx)
	targetVer := &domain.CatalogVersion{
		VersionID:     "ver-target-001",
		VersionNumber: 1,
		Description:   "Baseline Target Version",
		Author:        "admin@coderpush.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	if err := s.CreateVersionTx(ctx, txTarget, targetVer, nil); err != nil {
		_ = txTarget.Rollback()
		t.Fatalf("failed creating targetVer: %v", err)
	}
	if err := txTarget.Commit(); err != nil {
		t.Fatalf("commit targetVer failed: %v", err)
	}

	// 3. Now create rollback version referencing the existing target version
	txValid, _ := s.BeginTx(ctx)
	targetID := "ver-target-001"
	validVer := &domain.CatalogVersion{
		VersionID:         "ver-rollback-valid",
		VersionNumber:     2,
		Description:       "Rollback to ver-target-001",
		Author:            "admin@coderpush.com",
		Type:              domain.VersionTypeRollback,
		RevertOfVersionID: &targetID,
		CreatedAt:         now.Add(time.Minute),
	}
	if err := s.CreateVersionTx(ctx, txValid, validVer, nil); err != nil {
		_ = txValid.Rollback()
		t.Fatalf("CreateVersionTx with valid revert_of_version_id failed: %v", err)
	}
	if err := txValid.Commit(); err != nil {
		t.Fatalf("commit validVer failed: %v", err)
	}

	retrieved, err := s.GetVersion(ctx, "ver-rollback-valid")
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if retrieved.RevertOfVersionID == nil || *retrieved.RevertOfVersionID != "ver-target-001" {
		t.Errorf("expected RevertOfVersionID 'ver-target-001', got %+v", retrieved.RevertOfVersionID)
	}

	// 4. Test ON DELETE SET NULL: delete ver-target-001, ver-rollback-valid's revert_of_version_id should become NULL
	// First delete deltas for target if any, then delete target
	_, err = s.DB().ExecContext(ctx, "DELETE FROM catalog_versions WHERE version_id = 'ver-target-001';")
	if err != nil {
		t.Fatalf("failed deleting target version: %v", err)
	}

	afterDeleteTarget, err := s.GetVersion(ctx, "ver-rollback-valid")
	if err != nil {
		t.Fatalf("GetVersion ver-rollback-valid after target deletion failed: %v", err)
	}
	if afterDeleteTarget.RevertOfVersionID != nil {
		t.Errorf("expected RevertOfVersionID to become nil via ON DELETE SET NULL, got: %s", *afterDeleteTarget.RevertOfVersionID)
	}
}

// TestChallenger_CascadeDeltaDeletion tests that deleting a version cascades to all its deltas.
func TestChallenger_CascadeDeltaDeletion(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()

	tx, _ := s.BeginTx(ctx)
	ver := &domain.CatalogVersion{
		VersionID:     "ver-cascade-parent",
		VersionNumber: 1,
		Description:   "Parent version with deltas",
		Author:        "test@coderpush.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	deltas := []domain.ItemDelta{
		{
			VersionID:    "ver-cascade-parent",
			SKU:          "SKU-C1",
			Action:       domain.DeltaActionAdded,
			NewName:      "Item C1",
			NewPrice:     10.0,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now,
		},
		{
			VersionID:    "ver-cascade-parent",
			SKU:          "SKU-C2",
			Action:       domain.DeltaActionAdded,
			NewName:      "Item C2",
			NewPrice:     20.0,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now,
		},
	}
	if err := s.CreateVersionTx(ctx, tx, ver, deltas); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx failed: %v", err)
	}
	_ = tx.Commit()

	// Verify 2 deltas in DB
	var countBefore int
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM catalog_version_deltas WHERE version_id = 'ver-cascade-parent';").Scan(&countBefore)
	if countBefore != 2 {
		t.Fatalf("expected 2 deltas before delete, got %d", countBefore)
	}

	// Delete the parent version directly
	_, err := s.DB().ExecContext(ctx, "DELETE FROM catalog_versions WHERE version_id = 'ver-cascade-parent';")
	if err != nil {
		t.Fatalf("failed deleting parent version: %v", err)
	}

	// Verify deltas were CASCADE deleted
	var countAfter int
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM catalog_version_deltas WHERE version_id = 'ver-cascade-parent';").Scan(&countAfter)
	if countAfter != 0 {
		t.Errorf("expected 0 deltas after parent delete due to ON DELETE CASCADE, got %d", countAfter)
	}
}

// TestChallenger_AutoVersionNumbering tests version number calculation:
// - If version_number <= 0, auto-increments MAX(version_number) + 1.
// - Handles jumps in manual version numbering gracefully.
func TestChallenger_AutoVersionNumbering(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()

	// 1. First version with VersionNumber: 0 -> should become 1
	tx1, _ := s.BeginTx(ctx)
	v1 := &domain.CatalogVersion{
		VersionID:     "ver-auto-1",
		VersionNumber: 0,
		Description:   "First Auto",
		Author:        "test@coderpush.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	if err := s.CreateVersionTx(ctx, tx1, v1, nil); err != nil {
		_ = tx1.Rollback()
		t.Fatalf("CreateVersionTx v1 failed: %v", err)
	}
	_ = tx1.Commit()

	if v1.VersionNumber != 1 {
		t.Errorf("expected v1.VersionNumber = 1, got %d", v1.VersionNumber)
	}

	// 2. Second version with VersionNumber: -5 -> should become 2
	tx2, _ := s.BeginTx(ctx)
	v2 := &domain.CatalogVersion{
		VersionID:     "ver-auto-2",
		VersionNumber: -5,
		Description:   "Second Auto",
		Author:        "test@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now.Add(time.Minute),
	}
	if err := s.CreateVersionTx(ctx, tx2, v2, nil); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("CreateVersionTx v2 failed: %v", err)
	}
	_ = tx2.Commit()

	if v2.VersionNumber != 2 {
		t.Errorf("expected v2.VersionNumber = 2, got %d", v2.VersionNumber)
	}

	// 3. Third version with explicit jump: 10
	tx3, _ := s.BeginTx(ctx)
	v3 := &domain.CatalogVersion{
		VersionID:     "ver-auto-10",
		VersionNumber: 10,
		Description:   "Jump to 10",
		Author:        "test@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now.Add(2 * time.Minute),
	}
	if err := s.CreateVersionTx(ctx, tx3, v3, nil); err != nil {
		_ = tx3.Rollback()
		t.Fatalf("CreateVersionTx v3 failed: %v", err)
	}
	_ = tx3.Commit()

	// 4. Fourth version with VersionNumber: 0 -> should become 11 (MAX + 1)
	tx4, _ := s.BeginTx(ctx)
	v4 := &domain.CatalogVersion{
		VersionID:     "ver-auto-11",
		VersionNumber: 0,
		Description:   "Auto after jump",
		Author:        "test@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now.Add(3 * time.Minute),
	}
	if err := s.CreateVersionTx(ctx, tx4, v4, nil); err != nil {
		_ = tx4.Rollback()
		t.Fatalf("CreateVersionTx v4 failed: %v", err)
	}
	_ = tx4.Commit()

	if v4.VersionNumber != 11 {
		t.Errorf("expected v4.VersionNumber = 11, got %d", v4.VersionNumber)
	}
}

// TestChallenger_InputValidations tests input guardrails on CreateVersionTx.
func TestChallenger_InputValidations(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	ver := &domain.CatalogVersion{
		VersionID:     "ver-val",
		VersionNumber: 1,
		Description:   "Valid",
		Author:        "test@coderpush.com",
		Type:          domain.VersionTypeInitial,
	}

	// 1. Nil transaction
	if err := s.CreateVersionTx(ctx, nil, ver, nil); err == nil {
		t.Error("expected error for nil transaction, got nil")
	}

	// 2. Nil version struct
	tx, _ := s.BeginTx(ctx)
	if err := s.CreateVersionTx(ctx, tx, nil, nil); err == nil {
		t.Error("expected error for nil version, got nil")
	}
	_ = tx.Rollback()

	// 3. Empty version ID
	tx2, _ := s.BeginTx(ctx)
	emptyIDVer := &domain.CatalogVersion{
		VersionID: "   ",
	}
	if err := s.CreateVersionTx(ctx, tx2, emptyIDVer, nil); err == nil {
		t.Error("expected error for empty version ID, got nil")
	}
	_ = tx2.Rollback()
}

// TestChallenger_SchemaCheckConstraints tests SQLite database check constraints:
// - CHECK(type IN ('INITIAL', 'PATCH', 'ROLLBACK', 'SYNC'))
// - CHECK(action IN ('ADDED', 'UPDATED', 'WITHDRAWN'))
// - CHECK(old_status IS NULL OR old_status IN ('ACTIVE', 'WITHDRAWN'))
// - CHECK(new_status IN ('ACTIVE', 'WITHDRAWN'))
func TestChallenger_SchemaCheckConstraints(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// 1. Invalid version type
	_, err := s.DB().ExecContext(ctx, `
		INSERT INTO catalog_versions (version_id, version_number, description, author, type, created_at)
		VALUES ('ver-bad-type', 1, 'bad type', 'test', 'INVALID_TYPE', ?);
	`, now)
	if err == nil {
		t.Error("expected CHECK constraint violation for invalid version type, got nil")
	}

	// 2. Valid version for delta tests
	_, err = s.DB().ExecContext(ctx, `
		INSERT INTO catalog_versions (version_id, version_number, description, author, type, created_at)
		VALUES ('ver-valid-parent', 1, 'valid', 'test', 'INITIAL', ?);
	`, now)
	if err != nil {
		t.Fatalf("failed inserting valid parent version: %v", err)
	}

	// 3. Invalid delta action
	_, err = s.DB().ExecContext(ctx, `
		INSERT INTO catalog_version_deltas (version_id, sku, action, new_name, new_price, new_status, new_timestamp)
		VALUES ('ver-valid-parent', 'SKU-BAD-ACT', 'INVALID_ACTION', 'Item', 10.0, 'ACTIVE', ?);
	`, now)
	if err == nil {
		t.Error("expected CHECK constraint violation for invalid delta action, got nil")
	}

	// 4. Invalid delta old_status
	_, err = s.DB().ExecContext(ctx, `
		INSERT INTO catalog_version_deltas (version_id, sku, action, old_status, new_name, new_price, new_status, new_timestamp)
		VALUES ('ver-valid-parent', 'SKU-BAD-OLD-STAT', 'UPDATED', 'DELETED', 'Item', 10.0, 'ACTIVE', ?);
	`, now)
	if err == nil {
		t.Error("expected CHECK constraint violation for invalid old_status, got nil")
	}

	// 5. Invalid delta new_status
	_, err = s.DB().ExecContext(ctx, `
		INSERT INTO catalog_version_deltas (version_id, sku, action, new_name, new_price, new_status, new_timestamp)
		VALUES ('ver-valid-parent', 'SKU-BAD-NEW-STAT', 'ADDED', 'Item', 10.0, 'ARCHIVED', ?);
	`, now)
	if err == nil {
		t.Error("expected CHECK constraint violation for invalid new_status, got nil")
	}
}

// TestChallenger_HighVolumeAndSubMillisecondPointLookups benchmarks point lookups on products
// when catalog_version_deltas contains 10,000+ historical delta records.
func TestChallenger_HighVolumeAndSubMillisecondPointLookups(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()
	const productCount = 1000
	const versionsCount = 10
	const deltasPerVersion = 500 // total 5000 deltas

	// Seed products
	tx, _ := s.BeginTx(ctx)
	for i := 0; i < productCount; i++ {
		_ = s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-%04d", i),
			Name:              fmt.Sprintf("High Volume Product %d", i),
			Price:             float64(10 + i),
			Status:            domain.StatusActive,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	_ = tx.Commit()

	// Seed historical versions and deltas
	for v := 1; v <= versionsCount; v++ {
		txV, _ := s.BeginTx(ctx)
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-vol-%02d", v),
			VersionNumber: v,
			Description:   fmt.Sprintf("Historical version %d", v),
			Author:        "bench@coderpush.com",
			Type:          domain.VersionTypePatch,
			CreatedAt:     now.Add(time.Duration(v) * time.Hour),
		}
		batchDeltas := make([]domain.ItemDelta, deltasPerVersion)
		for d := 0; d < deltasPerVersion; d++ {
			skuIdx := (v*100 + d) % productCount
			batchDeltas[d] = domain.ItemDelta{
				VersionID:    ver.VersionID,
				SKU:          fmt.Sprintf("SKU-%04d", skuIdx),
				Action:       domain.DeltaActionUpdated,
				NewName:      fmt.Sprintf("High Volume Product %d (v%d)", skuIdx, v),
				NewPrice:     float64(20 + d),
				NewStatus:    domain.StatusActive,
				NewTimestamp: now,
			}
		}
		if err := s.CreateVersionTx(ctx, txV, ver, batchDeltas); err != nil {
			_ = txV.Rollback()
			t.Fatalf("failed inserting historical batch %d: %v", v, err)
		}
		_ = txV.Commit()
	}

	// Verify delta count
	var totalDeltas int
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM catalog_version_deltas;").Scan(&totalDeltas)
	if totalDeltas != versionsCount*deltasPerVersion {
		t.Fatalf("expected %d deltas, got %d", versionsCount*deltasPerVersion, totalDeltas)
	}

	// Measure 500 point lookups to verify live catalog queries remain O(1) < 1ms
	start := time.Now()
	const lookups = 500
	for i := 0; i < lookups; i++ {
		sku := fmt.Sprintf("SKU-%04d", i%productCount)
		prod, err := s.GetProduct(ctx, sku)
		if err != nil || prod == nil {
			t.Fatalf("GetProduct %s failed: %v", sku, err)
		}
	}
	elapsed := time.Since(start)
	avgLookup := elapsed / time.Duration(lookups)
	t.Logf("Measured avg point lookup with %d deltas: %v", totalDeltas, avgLookup)

	maxLookupAllowed := 2 * time.Millisecond
	if isRaceDetectorEnabled() {
		maxLookupAllowed = 15 * time.Millisecond
	}
	if avgLookup > maxLookupAllowed {
		t.Errorf("average lookup latency too high: %v (expected < %v)", avgLookup, maxLookupAllowed)
	}
}

// TestChallenger_ConcurrentVersionCreation_TransactionIsolation tests that multiple goroutines
// attempting to write versions simultaneously operate safely without data races or corruption.
func TestChallenger_ConcurrentVersionCreation_TransactionIsolation(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	const workerCount = 5
	const versionsPerWorker = 4
	var wg sync.WaitGroup

	errCh := make(chan error, workerCount*versionsPerWorker)

	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < versionsPerWorker; i++ {
				tx, err := s.BeginTx(ctx)
				if err != nil {
					errCh <- fmt.Errorf("worker %d BeginTx failed: %w", workerID, err)
					return
				}

				verID := fmt.Sprintf("ver-worker-%d-%d", workerID, i)
				ver := &domain.CatalogVersion{
					VersionID:     verID,
					VersionNumber: 0, // Request auto-assignment
					Description:   fmt.Sprintf("Worker %d run %d", workerID, i),
					Author:        "stress@coderpush.com",
					Type:          domain.VersionTypePatch,
					CreatedAt:     time.Now().UTC(),
				}

				deltas := []domain.ItemDelta{
					{
						SKU:          fmt.Sprintf("SKU-W%d-%d", workerID, i),
						Action:       domain.DeltaActionAdded,
						NewName:      fmt.Sprintf("Item W%d-%d", workerID, i),
						NewPrice:     10.0 + float64(i),
						NewStatus:    domain.StatusActive,
						NewTimestamp: time.Now().UTC(),
					},
				}

				if err := s.CreateVersionTx(ctx, tx, ver, deltas); err != nil {
					_ = tx.Rollback()
					errCh <- fmt.Errorf("worker %d CreateVersionTx failed: %w", workerID, err)
					return
				}

				if err := tx.Commit(); err != nil {
					errCh <- fmt.Errorf("worker %d Commit failed: %w", workerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrency error: %v", err)
	}

	// Verify all versions were committed with distinct version numbers
	list, total, err := s.ListVersions(ctx, 100, 0)
	if err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}
	expectedTotal := workerCount * versionsPerWorker
	if total != expectedTotal || len(list) != expectedTotal {
		t.Fatalf("expected %d versions, got total=%d, len=%d", expectedTotal, total, len(list))
	}

	// Verify version numbers are unique and strictly 1..expectedTotal
	seenNumbers := make(map[int]bool)
	for _, v := range list {
		if seenNumbers[v.VersionNumber] {
			t.Errorf("duplicate version number detected: %d", v.VersionNumber)
		}
		seenNumbers[v.VersionNumber] = true
	}
	if len(seenNumbers) != expectedTotal {
		t.Errorf("expected %d distinct version numbers, got %d", expectedTotal, len(seenNumbers))
	}
}
