package sync_test

import (
	"context"
	"math"
	"testing"
	"time"

	"coderpush/pkg/domain"
	catalogSync "coderpush/pkg/sync"
)

// TestChallenger_ScenarioA_CreateWithdrawnThenResurrect asserts that creating an item
// directly as WITHDRAWN and then attempting to resurrect it to ACTIVE in the same patch
// correctly triggers Invariant A quarantine.
func TestChallenger_ScenarioA_CreateWithdrawnThenResurrect(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	t1 := now.Add(time.Hour)
	t2 := now.Add(2 * time.Hour)

	patch := domain.CatalogPatch{
		Description: "Create withdrawn item then attempt resurrection",
		Author:      "adversary@test.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-INIT-WITHDRAWN", Name: "Item 1", Price: 10.00, Status: domain.StatusWithdrawn, Timestamp: &t1},
			{SKU: "SKU-INIT-WITHDRAWN", Name: "Item 1 Resurrected", Price: 15.00, Status: domain.StatusActive, Timestamp: &t2},
		},
	}

	engine := catalogSync.NewPatchEngine(store)
	res, err := engine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	if res.Summary.Added != 1 {
		t.Errorf("expected 1 added item, got %d", res.Summary.Added)
	}
	if res.Summary.DeadLetter != 1 {
		t.Errorf("expected 1 dead letter for resurrection, got %d", res.Summary.DeadLetter)
	}
	if res.CanCommit {
		t.Errorf("expected CanCommit == false")
	}
	if len(res.DeadLetterItems) != 1 || res.DeadLetterItems[0].SKU != "SKU-INIT-WITHDRAWN" {
		t.Errorf("expected SKU-INIT-WITHDRAWN quarantined, got %+v", res.DeadLetterItems)
	}
}

// TestChallenger_ScenarioB_FloatingPointExtremes tests extreme floats: NaN, +/-Inf, subnormal, -0.0
func TestChallenger_ScenarioB_FloatingPointExtremes(t *testing.T) {
	ctx := context.Background()
	store := setupTestStorage(t)

	now := time.Now().UTC()
	patch := domain.CatalogPatch{
		Description: "Extreme float values",
		Author:      "adversary@test.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-NAN", Name: "NaN", Price: math.NaN(), Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-INF-POS", Name: "+Inf", Price: math.Inf(1), Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-INF-NEG", Name: "-Inf", Price: math.Inf(-1), Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-NEG-ZERO", Name: "-0.0", Price: math.Copysign(0.0, -1.0), Status: domain.StatusActive, Timestamp: &now},
			{SKU: "SKU-SUBNORMAL", Name: "Subnormal", Price: math.SmallestNonzeroFloat64, Status: domain.StatusActive, Timestamp: &now},
		},
	}

	engine := catalogSync.NewPatchEngine(store)
	res, err := engine.PreviewPatch(ctx, patch)
	if err != nil {
		t.Fatalf("PreviewPatch failed: %v", err)
	}

	// NaN, +Inf, -Inf must be dead letters (3 items).
	// -0.0 is equal to 0.0 (valid price >= 0).
	// Subnormal is > 0 (valid price).
	if res.Summary.DeadLetter != 3 {
		t.Errorf("expected 3 dead letters for NaN/+Inf/-Inf, got %d", res.Summary.DeadLetter)
	}
	if res.Summary.Added != 2 {
		t.Errorf("expected 2 added items for -0.0 and Subnormal, got %d", res.Summary.Added)
	}
}

// TestChallenger_ScenarioC_ContextCancellationMidway tests context cancellation
func TestChallenger_ScenarioC_ContextCancellationMidway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	store := setupTestStorage(t)
	engine := catalogSync.NewPatchEngine(store)

	patch := domain.CatalogPatch{
		Description: "Cancelled context patch",
		Author:      "tester@test.com",
		Items: []domain.PatchItem{
			{SKU: "SKU-1", Name: "Item", Price: 10.00, Status: domain.StatusActive},
		},
	}

	_, err := engine.PreviewPatch(ctx, patch)
	if err == nil {
		t.Errorf("expected context cancellation error, got nil")
	}
}
