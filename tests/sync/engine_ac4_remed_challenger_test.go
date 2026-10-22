package sync_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	catalogSync "coderpush/pkg/sync"
)

// ============================================================================
// EMPIRICAL CHALLENGER ADVERSARIAL SUITE FOR AC4 (DEAD-LETTER ISOLATION)
// Target: Zero-Timestamp records, Corrupt payloads, Catalog Invariant Safety
// ============================================================================

// TestChallenger_AC4_ZeroTimestampNewSKU_QuarantinedAndZeroAdded verifies that
// any new product with a zero timestamp (either time.Time{} or UTC Jan 1 0001 00:00:00)
// is strictly quarantined to dead_letter_issues with the expected diagnostic reason,
// produces Added == 0, and is NEVER inserted into the products table.
func TestChallenger_AC4_ZeroTimestampNewSKU_QuarantinedAndZeroAdded(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "challenger-zero-ts-new"

	zeroVariations := []struct {
		name      string
		sku       string
		timestamp time.Time
	}{
		{
			name:      "time.Time{} literal",
			sku:       "SKU-ZERO-LITERAL",
			timestamp: time.Time{},
		},
		{
			name:      "UTC Date Jan 1 0001 00:00:00",
			sku:       "SKU-ZERO-DATE-UTC",
			timestamp: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "time.Time{}.In(Local)",
			sku:       "SKU-ZERO-IN-LOCAL",
			timestamp: time.Time{}.In(time.Local),
		},
		{
			name:      "time.Time{}.In(FixedZone +07)",
			sku:       "SKU-ZERO-IN-ZONE",
			timestamp: time.Time{}.In(time.FixedZone("ICT", 7*3600)),
		},
	}

	for idx, tc := range zeroVariations {
		t.Run(tc.name, func(t *testing.T) {
			batch := domain.Batch{
				JobID:      jobID,
				BatchIndex: idx,
				Records: []domain.SupplierUpdate{
					{
						SKU:       tc.sku,
						Name:      fmt.Sprintf("Item %s", tc.name),
						Price:     49.99,
						Status:    domain.StatusActive,
						Timestamp: tc.timestamp,
					},
				},
			}

			summary, err := engine.ExecuteBatch(ctx, batch)
			if err != nil {
				t.Fatalf("ExecuteBatch failed on %s: %v", tc.name, err)
			}

			// 1. Summary verification: exactly 1 DeadLetter, 0 across all other counters
			expected := domain.ChangeSummary{DeadLetter: 1}
			if summary != expected {
				t.Fatalf("Summary mismatch on %s: got %+v, want %+v", tc.name, summary, expected)
			}

			// 2. Catalog verification: must NOT be in products table
			prod, err := store.GetProduct(ctx, tc.sku)
			if err == nil && prod != nil {
				t.Fatalf("INVARIANT VIOLATION: Zero timestamp record %s was committed to products table: %+v", tc.sku, prod)
			}
			if !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("Unexpected error querying product %s: %v", tc.sku, err)
			}

			// 3. Dead letter table verification
			dlRecords, total, err := store.ListDeadLetterRecords(ctx, jobID, "", 10, 0)
			if err != nil {
				t.Fatalf("Failed listing dead letter records: %v", err)
			}
			if total < 1 {
				t.Fatalf("Expected at least 1 dead letter record, got %d", total)
			}

			// Verify reason contains "zero or uninitialized timestamp"
			foundRecord := false
			for _, dl := range dlRecords {
				if dl.SKU == tc.sku {
					foundRecord = true
					if !strings.Contains(strings.ToLower(dl.Reason), "zero or uninitialized timestamp") {
						t.Errorf("Dead letter reason for %s does not contain 'zero or uninitialized timestamp': got %q", tc.sku, dl.Reason)
					}
					break
				}
			}
			if !foundRecord {
				t.Errorf("Did not find dead letter record for SKU %s", tc.sku)
			}
		})
	}
}

// TestChallenger_AC4_ZeroTimestampExistingCatalogItem_PreservesState verifies that
// updating an existing catalog product with a zero timestamp update is quarantined to
// DeadLetter, does NOT increment Skipped or Updated or Added, and leaves the existing
// catalog product completely unmodified in the database.
func TestChallenger_AC4_ZeroTimestampExistingCatalogItem_PreservesState(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	tBase := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	jobSeed := "job-seed-existing"

	// Pre-seed 3 items:
	// 1. ACTIVE item
	// 2. WITHDRAWN item
	// 3. ACTIVE item with custom price & name
	seedBatch := domain.Batch{
		JobID:      jobSeed,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-EXIST-ACTIVE", Name: "Original Active", Price: 100.0, Status: domain.StatusActive, Timestamp: tBase},
			{SKU: "SKU-EXIST-WITHDRAWN", Name: "Original Withdrawn", Price: 50.0, Status: domain.StatusWithdrawn, Timestamp: tBase},
			{SKU: "SKU-EXIST-EXPENSIVE", Name: "Original Expensive", Price: 999.99, Status: domain.StatusActive, Timestamp: tBase},
		},
	}

	seedRes, err := engine.ExecuteBatch(ctx, seedBatch)
	if err != nil {
		t.Fatalf("Failed seeding existing items: %v", err)
	}
	if seedRes.Added != 3 {
		t.Fatalf("Expected Added=3, got %+v", seedRes)
	}

	// Attack: Send updates with zero timestamps attempting to mutate each product:
	// - Alter price
	// - Alter name
	// - Try resurrecting WITHDRAWN item
	// - Try withdrawing ACTIVE item
	attackJobID := "job-zero-ts-attack"
	attackBatch := domain.Batch{
		JobID:      attackJobID,
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{
				SKU:       "SKU-EXIST-ACTIVE",
				Name:      "MUTATED ACTIVE NAME",
				Price:     1.0,
				Status:    domain.StatusWithdrawn,
				Timestamp: time.Time{}, // Zero timestamp
			},
			{
				SKU:       "SKU-EXIST-WITHDRAWN",
				Name:      "MUTATED RESURRECTED NAME",
				Price:     500.0,
				Status:    domain.StatusActive,
				Timestamp: time.Time{}, // Zero timestamp
			},
			{
				SKU:       "SKU-EXIST-EXPENSIVE",
				Name:      "MUTATED PRICE ZERO",
				Price:     0.0,
				Status:    domain.StatusActive,
				Timestamp: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), // Zero timestamp
			},
		},
	}

	attackRes, err := engine.ExecuteBatch(ctx, attackBatch)
	if err != nil {
		t.Fatalf("ExecuteBatch failed on attack batch: %v", err)
	}

	// Crucial AC4 Check:
	// DeadLetter must be 3.
	// Skipped must be 0 (must NOT be treated as Invariant B skipped).
	// Updated must be 0.
	// Added must be 0.
	// Withdrawn must be 0.
	expectedAttackSummary := domain.ChangeSummary{DeadLetter: 3}
	if attackRes != expectedAttackSummary {
		t.Fatalf("Summary mismatch on attack: got %+v, want %+v", attackRes, expectedAttackSummary)
	}

	// Verify catalog products were completely unaltered
	p1, err := store.GetProduct(ctx, "SKU-EXIST-ACTIVE")
	if err != nil || p1 == nil {
		t.Fatalf("Failed to fetch SKU-EXIST-ACTIVE: %v", err)
	}
	if p1.Name != "Original Active" || p1.Price != 100.0 || p1.Status != domain.StatusActive || !p1.SupplierTimestamp.Equal(tBase) {
		t.Fatalf("CATALOG MUTATION BUG: SKU-EXIST-ACTIVE was modified: %+v", p1)
	}

	p2, err := store.GetProduct(ctx, "SKU-EXIST-WITHDRAWN")
	if err != nil || p2 == nil {
		t.Fatalf("Failed to fetch SKU-EXIST-WITHDRAWN: %v", err)
	}
	if p2.Name != "Original Withdrawn" || p2.Price != 50.0 || p2.Status != domain.StatusWithdrawn || !p2.SupplierTimestamp.Equal(tBase) {
		t.Fatalf("CATALOG MUTATION BUG: SKU-EXIST-WITHDRAWN was modified: %+v", p2)
	}

	p3, err := store.GetProduct(ctx, "SKU-EXIST-EXPENSIVE")
	if err != nil || p3 == nil {
		t.Fatalf("Failed to fetch SKU-EXIST-EXPENSIVE: %v", err)
	}
	if p3.Name != "Original Expensive" || p3.Price != 999.99 || p3.Status != domain.StatusActive || !p3.SupplierTimestamp.Equal(tBase) {
		t.Fatalf("CATALOG MUTATION BUG: SKU-EXIST-EXPENSIVE was modified: %+v", p3)
	}

	// Verify dead letters in storage
	dlList, totalDL, err := store.ListDeadLetterRecords(ctx, attackJobID, "", 10, 0)
	if err != nil {
		t.Fatalf("Failed listing dead letter records: %v", err)
	}
	if totalDL != 3 || len(dlList) != 3 {
		t.Fatalf("Expected 3 dead letter records, got %d", totalDL)
	}
	for _, dl := range dlList {
		if !strings.Contains(strings.ToLower(dl.Reason), "zero or uninitialized timestamp") {
			t.Errorf("Unexpected dead letter reason: %q", dl.Reason)
		}
	}
}

// TestChallenger_AC4_PureZeroTimestampBatch_ZeroCatalogDrift sends a batch composed
// 100% of zero-timestamp records across 50 distinct SKUs.
// Verifies that:
// 1. Transaction commits cleanly without error.
// 2. Summary is exactly {DeadLetter: 50}.
// 3. 0 rows exist in the products table (0 catalog drift).
// 4. Exactly 50 rows exist in dead_letter_issues table.
func TestChallenger_AC4_PureZeroTimestampBatch_ZeroCatalogDrift(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	const count = 50
	jobID := "job-100-percent-zero-ts"

	var records []domain.SupplierUpdate
	for i := 0; i < count; i++ {
		records = append(records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-ALL-ZERO-%03d", i),
			Name:      fmt.Sprintf("Pure Zero Item %d", i),
			Price:     float64(i + 1),
			Status:    domain.StatusActive,
			Timestamp: time.Time{},
		})
	}

	batch := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records:    records,
	}

	summary, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("ExecuteBatch failed on 100%% zero-timestamp batch: %v", err)
	}

	expected := domain.ChangeSummary{DeadLetter: count}
	if summary != expected {
		t.Fatalf("Summary mismatch on pure zero-timestamp batch: got %+v, want %+v", summary, expected)
	}

	// Verify products table row count
	var productCount int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&productCount)
	if err != nil {
		t.Fatalf("Failed querying products count: %v", err)
	}
	if productCount != 0 {
		t.Fatalf("CATALOG DRIFT DETECTED: Expected 0 products in database, found %d", productCount)
	}

	// Verify dead_letter_issues row count
	_, totalDL, err := store.ListDeadLetterRecords(ctx, jobID, "", 1, 0)
	if err != nil {
		t.Fatalf("Failed querying dead letter count: %v", err)
	}
	if totalDL != count {
		t.Fatalf("Expected %d dead letter issues, found %d", count, totalDL)
	}
}

// TestChallenger_AC4_MixedBatch_AtomicityAndSelectiveIsolation tests a complex batch containing:
// - 20 valid new active items
// - 10 valid updates to existing active items
// - 5 valid transitions to withdrawn
// - 10 zero-timestamp records (5 new SKUs, 5 existing SKUs)
// - 5 negative price records
// - 5 empty/whitespace SKU records
// - 5 NaN/Inf price records
// - 5 invalid status enum records
// Total = 65 records in a single batch.
// Verifies that:
// 1. Transaction commits cleanly and atomically.
// 2. Summary: Added=20, Updated=10, Withdrawn=5, DeadLetter=30, Skipped=0.
// 3. All 35 valid products exist with accurate values in the database.
// 4. None of the 30 corrupt items leaked into the products table.
// 5. All 30 corrupt items are logged to dead_letter_issues.
func TestChallenger_AC4_MixedBatch_AtomicityAndSelectiveIsolation(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)

	// Step 1: Pre-seed 15 items (10 will be updated, 5 will be withdrawn)
	var seedRecords []domain.SupplierUpdate
	for i := 0; i < 10; i++ {
		seedRecords = append(seedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-UPDATE-%02d", i),
			Name:      fmt.Sprintf("Seed Item %d", i),
			Price:     20.0,
			Status:    domain.StatusActive,
			Timestamp: t1,
		})
	}
	for i := 0; i < 5; i++ {
		seedRecords = append(seedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-WITHDRAW-%02d", i),
			Name:      fmt.Sprintf("Seed Withdraw %d", i),
			Price:     30.0,
			Status:    domain.StatusActive,
			Timestamp: t1,
		})
	}
	// Also seed 5 items that will receive zero-timestamp attacks
	for i := 0; i < 5; i++ {
		seedRecords = append(seedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-ZERO-ATTACK-%02d", i),
			Name:      fmt.Sprintf("Seed Zero Attack %d", i),
			Price:     40.0,
			Status:    domain.StatusActive,
			Timestamp: t1,
		})
	}

	seedSummary, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID: "job-mix-seed", BatchIndex: 0, Records: seedRecords,
	})
	if err != nil {
		t.Fatalf("Failed seeding: %v", err)
	}
	if seedSummary.Added != 20 {
		t.Fatalf("Expected Added=20 for seed; got %+v", seedSummary)
	}

	// Step 2: Build the complex 65-record mixed batch
	jobID := "job-mix-complex-challenge"
	var mixedRecords []domain.SupplierUpdate

	// A. 20 valid new active items
	for i := 0; i < 20; i++ {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-NEW-%02d", i),
			Name:      fmt.Sprintf("Valid New Item %d", i),
			Price:     float64(50 + i),
			Status:    domain.StatusActive,
			Timestamp: t2,
		})
	}

	// B. 10 valid updates to existing active items
	for i := 0; i < 10; i++ {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-UPDATE-%02d", i),
			Name:      fmt.Sprintf("Updated Item %d", i),
			Price:     float64(100 + i),
			Status:    domain.StatusActive,
			Timestamp: t2,
		})
	}

	// C. 5 valid transitions to withdrawn
	for i := 0; i < 5; i++ {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-WITHDRAW-%02d", i),
			Name:      fmt.Sprintf("Withdrawn Item %d", i),
			Price:     30.0,
			Status:    domain.StatusWithdrawn,
			Timestamp: t2,
		})
	}

	// D. 10 zero-timestamp records (5 new, 5 targeting seeded items)
	for i := 0; i < 5; i++ {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-ZERO-NEW-%02d", i),
			Name:      fmt.Sprintf("Corrupt Zero New %d", i),
			Price:     25.0,
			Status:    domain.StatusActive,
			Timestamp: time.Time{},
		})
	}
	for i := 0; i < 5; i++ {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-ZERO-ATTACK-%02d", i),
			Name:      fmt.Sprintf("Corrupt Zero Attack %d", i),
			Price:     9999.0,
			Status:    domain.StatusActive,
			Timestamp: time.Time{},
		})
	}

	// E. 5 negative price records
	for i := 0; i < 5; i++ {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-NEG-%02d", i),
			Name:      fmt.Sprintf("Corrupt Neg %d", i),
			Price:     -float64(i + 1),
			Status:    domain.StatusActive,
			Timestamp: t2,
		})
	}

	// F. 5 whitespace / empty SKU records
	whitespaceSKUs := []string{"", " ", "   ", "\t", "\n\r"}
	for i, ws := range whitespaceSKUs {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       ws,
			Name:      fmt.Sprintf("Corrupt WS %d", i),
			Price:     10.0,
			Status:    domain.StatusActive,
			Timestamp: t2,
		})
	}

	// G. 5 NaN / Inf price records
	badPrices := []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.NaN(), math.Inf(1)}
	for i, bp := range badPrices {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-NANINF-%02d", i),
			Name:      fmt.Sprintf("Corrupt NaNInf %d", i),
			Price:     bp,
			Status:    domain.StatusActive,
			Timestamp: t2,
		})
	}

	// H. 5 invalid status enum records
	badStatuses := []domain.ProductStatus{"active", "WITHDRAW", "DELETED", "ARCHIVED", "INVALID"}
	for i, bs := range badStatuses {
		mixedRecords = append(mixedRecords, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-MIX-STATUS-%02d", i),
			Name:      fmt.Sprintf("Corrupt Status %d", i),
			Price:     15.0,
			Status:    bs,
			Timestamp: t2,
		})
	}

	if len(mixedRecords) != 65 {
		t.Fatalf("Expected 65 total records, got %d", len(mixedRecords))
	}

	// Step 3: Execute mixed batch
	mixedSummary, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID:      jobID,
		BatchIndex: 1,
		Records:    mixedRecords,
	})
	if err != nil {
		t.Fatalf("ExecuteBatch failed on complex mixed batch: %v", err)
	}

	expectedSummary := domain.ChangeSummary{
		Added:      20,
		Updated:    10,
		Withdrawn:  5,
		Skipped:    0,
		DeadLetter: 30,
	}

	if mixedSummary != expectedSummary {
		t.Fatalf("Mixed batch change summary mismatch:\nGot:  %+v\nWant: %+v", mixedSummary, expectedSummary)
	}

	// Step 4: Verify catalog state
	// Verify 20 new items were added
	for i := 0; i < 20; i++ {
		sku := fmt.Sprintf("SKU-MIX-NEW-%02d", i)
		p, err := store.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("Valid new item %s missing: %v", sku, err)
		}
		if p.Price != float64(50+i) || p.Status != domain.StatusActive {
			t.Fatalf("Valid new item %s data mismatch: %+v", sku, p)
		}
	}

	// Verify 10 items were updated
	for i := 0; i < 10; i++ {
		sku := fmt.Sprintf("SKU-MIX-UPDATE-%02d", i)
		p, err := store.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("Updated item %s missing: %v", sku, err)
		}
		if p.Price != float64(100+i) || p.Name != fmt.Sprintf("Updated Item %d", i) {
			t.Fatalf("Updated item %s data mismatch: %+v", sku, p)
		}
	}

	// Verify 5 items were withdrawn
	for i := 0; i < 5; i++ {
		sku := fmt.Sprintf("SKU-MIX-WITHDRAW-%02d", i)
		p, err := store.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("Withdrawn item %s missing: %v", sku, err)
		}
		if p.Status != domain.StatusWithdrawn {
			t.Fatalf("Item %s status mismatch: got %s, want WITHDRAWN", sku, p.Status)
		}
	}

	// Verify 5 attacked items retained original values
	for i := 0; i < 5; i++ {
		sku := fmt.Sprintf("SKU-MIX-ZERO-ATTACK-%02d", i)
		p, err := store.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("Attacked item %s missing: %v", sku, err)
		}
		if p.Price != 40.0 || p.Name != fmt.Sprintf("Seed Zero Attack %d", i) {
			t.Fatalf("INVARIANT VIOLATION: Zero timestamp attack mutated item %s: %+v", sku, p)
		}
	}

	// Verify none of the 5 new zero-timestamp SKUs exist in products
	for i := 0; i < 5; i++ {
		sku := fmt.Sprintf("SKU-MIX-ZERO-NEW-%02d", i)
		p, err := store.GetProduct(ctx, sku)
		if err == nil && p != nil {
			t.Fatalf("INVARIANT VIOLATION: Zero timestamp SKU %s was committed to products table: %+v", sku, p)
		}
	}

	// Verify dead letter issues total
	_, dlTotal, err := store.ListDeadLetterRecords(ctx, jobID, "", 1, 0)
	if err != nil {
		t.Fatalf("Failed querying dead letter records: %v", err)
	}
	if dlTotal != 30 {
		t.Fatalf("Expected 30 dead letter issues, found %d", dlTotal)
	}
}

// TestChallenger_AC4_IntraBatchCollisionWithZeroTimestamp tests scenarios where a valid
// update and a zero-timestamp update for the SAME SKU appear in the same batch.
func TestChallenger_AC4_IntraBatchCollisionWithZeroTimestamp(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Subtest 1: Valid record FIRST, then Zero-Timestamp record SECOND
	t.Run("ValidFirst_ZeroSecond", func(t *testing.T) {
		sku := "SKU-COLLISION-1"
		batch := domain.Batch{
			JobID:      "job-collision-1",
			BatchIndex: 0,
			Records: []domain.SupplierUpdate{
				{
					SKU:       sku,
					Name:      "Valid First Item",
					Price:     10.0,
					Status:    domain.StatusActive,
					Timestamp: t1,
				},
				{
					SKU:       sku,
					Name:      "Corrupt Zero Second Item",
					Price:     999.0,
					Status:    domain.StatusWithdrawn,
					Timestamp: time.Time{},
				},
			},
		}

		res, err := engine.ExecuteBatch(ctx, batch)
		if err != nil {
			t.Fatalf("ExecuteBatch failed: %v", err)
		}

		// Valid record was added, zero record was dead lettered
		if res.Added != 1 || res.DeadLetter != 1 || res.Updated != 0 || res.Skipped != 0 {
			t.Fatalf("Summary mismatch: got %+v, want Added=1, DeadLetter=1", res)
		}

		// Product must have valid first record's state, NOT mutated by second
		p, err := store.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("Failed fetching product: %v", err)
		}
		if p.Name != "Valid First Item" || p.Price != 10.0 || p.Status != domain.StatusActive {
			t.Fatalf("Product mutated by intra-batch zero timestamp record: %+v", p)
		}
	})

	// Subtest 2: Zero-Timestamp record FIRST, then Valid record SECOND
	t.Run("ZeroFirst_ValidSecond", func(t *testing.T) {
		sku := "SKU-COLLISION-2"
		batch := domain.Batch{
			JobID:      "job-collision-2",
			BatchIndex: 0,
			Records: []domain.SupplierUpdate{
				{
					SKU:       sku,
					Name:      "Corrupt Zero First Item",
					Price:     999.0,
					Status:    domain.StatusWithdrawn,
					Timestamp: time.Time{},
				},
				{
					SKU:       sku,
					Name:      "Valid Second Item",
					Price:     20.0,
					Status:    domain.StatusActive,
					Timestamp: t1,
				},
			},
		}

		res, err := engine.ExecuteBatch(ctx, batch)
		if err != nil {
			t.Fatalf("ExecuteBatch failed: %v", err)
		}

		// Zero record was dead lettered, valid record was added
		if res.Added != 1 || res.DeadLetter != 1 || res.Updated != 0 || res.Skipped != 0 {
			t.Fatalf("Summary mismatch: got %+v, want Added=1, DeadLetter=1", res)
		}

		// Product must be successfully created with valid second record's state
		p, err := store.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("Failed fetching product: %v", err)
		}
		if p.Name != "Valid Second Item" || p.Price != 20.0 || p.Status != domain.StatusActive {
			t.Fatalf("Product state mismatch: %+v", p)
		}
	})

	// Subtest 3: Multiple Zero-Timestamp records for same SKU
	t.Run("MultipleZeroSameSKU", func(t *testing.T) {
		sku := "SKU-COLLISION-3"
		batch := domain.Batch{
			JobID:      "job-collision-3",
			BatchIndex: 0,
			Records: []domain.SupplierUpdate{
				{SKU: sku, Name: "Zero A", Price: 10.0, Status: domain.StatusActive, Timestamp: time.Time{}},
				{SKU: sku, Name: "Zero B", Price: 20.0, Status: domain.StatusActive, Timestamp: time.Time{}},
				{SKU: sku, Name: "Zero C", Price: 30.0, Status: domain.StatusActive, Timestamp: time.Time{}},
			},
		}

		res, err := engine.ExecuteBatch(ctx, batch)
		if err != nil {
			t.Fatalf("ExecuteBatch failed: %v", err)
		}

		if res.DeadLetter != 3 || res.Added != 0 || res.Updated != 0 {
			t.Fatalf("Summary mismatch: got %+v, want DeadLetter=3", res)
		}

		p, err := store.GetProduct(ctx, sku)
		if err == nil && p != nil {
			t.Fatalf("Product %s was committed to products table: %+v", sku, p)
		}
	})
}

// TestChallenger_AC4_ConcurrentZeroTimestampRace runs multiple concurrent goroutines
// ingesting batches that combine zero-timestamp records and valid records under race detector.
func TestChallenger_AC4_ConcurrentZeroTimestampRace(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	const numWorkers = 10
	const batchesPerWorker = 5
	const recordsPerBatch = 10

	tBase := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	var errCount int

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for b := 0; b < batchesPerWorker; b++ {
				var records []domain.SupplierUpdate
				for r := 0; r < recordsPerBatch; r++ {
					sku := fmt.Sprintf("SKU-RACE-W%d-B%d-R%d", workerID, b, r)
					if r%2 == 0 {
						// Valid record
						records = append(records, domain.SupplierUpdate{
							SKU:       sku,
							Name:      "Valid Race Item",
							Price:     10.0,
							Status:    domain.StatusActive,
							Timestamp: tBase.Add(time.Duration(r) * time.Minute),
						})
					} else {
						// Zero timestamp record
						records = append(records, domain.SupplierUpdate{
							SKU:       sku,
							Name:      "Zero Race Item",
							Price:     10.0,
							Status:    domain.StatusActive,
							Timestamp: time.Time{},
						})
					}
				}

				summary, err := engine.ExecuteBatch(ctx, domain.Batch{
					JobID:      fmt.Sprintf("job-race-worker-%d", workerID),
					BatchIndex: b,
					Records:    records,
				})
				if err != nil {
					errCount++
					t.Errorf("worker %d batch %d failed: %v", workerID, b, err)
					return
				}

				if summary.Added != 5 || summary.DeadLetter != 5 {
					t.Errorf("worker %d batch %d unexpected summary: %+v", workerID, b, summary)
				}
			}
		}(w)
	}

	wg.Wait()

	if errCount > 0 {
		t.Fatalf("Encountered %d errors in concurrent zero-timestamp race", errCount)
	}
}

// TestChallenger_AC4_StreamingJob_ZeroTimestampBatchCheckpoints streams 10 consecutive batches
// through RunJob where 50% of all items have zero timestamps.
// Verifies that:
// 1. Job completes with JobStatusCompleted.
// 2. Summary in Job matches exactly: Added=100, DeadLetter=100.
// 3. Exactly 100 products exist in products table.
// 4. Exactly 100 dead letter issues exist in dead_letter_issues table.
func TestChallenger_AC4_StreamingJob_ZeroTimestampBatchCheckpoints(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-stream-zero-ts"
	const totalBatches = 10
	const itemsPerBatch = 20
	baseTime := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	stream := make(chan domain.Batch, totalBatches)

	for b := 0; b < totalBatches; b++ {
		var records []domain.SupplierUpdate
		for i := 0; i < itemsPerBatch; i++ {
			sku := fmt.Sprintf("SKU-STREAM-B%02d-I%02d", b, i)
			if i%2 == 0 {
				// Valid record
				records = append(records, domain.SupplierUpdate{
					SKU:       sku,
					Name:      fmt.Sprintf("Valid Stream Item B%d I%d", b, i),
					Price:     float64((b+1)*10 + i),
					Status:    domain.StatusActive,
					Timestamp: baseTime.Add(time.Duration(b*itemsPerBatch+i) * time.Minute),
				})
			} else {
				// Zero timestamp record
				records = append(records, domain.SupplierUpdate{
					SKU:       sku,
					Name:      fmt.Sprintf("Zero Timestamp Item B%d I%d", b, i),
					Price:     19.99,
					Status:    domain.StatusActive,
					Timestamp: time.Time{},
				})
			}
		}

		stream <- domain.Batch{
			JobID:      jobID,
			BatchIndex: b,
			Records:    records,
		}
	}
	close(stream)

	job, err := engine.RunJob(ctx, jobID, stream)
	if err != nil {
		t.Fatalf("RunJob failed on zero timestamp stream: %v", err)
	}

	if job.Status != domain.JobStatusCompleted {
		t.Fatalf("Job did not complete: status=%s", job.Status)
	}

	expectedSummary := domain.ChangeSummary{
		Added:      100,
		DeadLetter: 100,
	}
	if job.Summary != expectedSummary {
		t.Fatalf("Job summary mismatch: got %+v, want %+v", job.Summary, expectedSummary)
	}

	// Verify database rows count
	var prodCount int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&prodCount)
	if err != nil {
		t.Fatalf("Failed counting products: %v", err)
	}
	if prodCount != 100 {
		t.Fatalf("Expected 100 products in database, got %d", prodCount)
	}

	var dlCount int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM dead_letter_issues WHERE job_id = ?", jobID).Scan(&dlCount)
	if err != nil {
		t.Fatalf("Failed counting dead letters: %v", err)
	}
	if dlCount != 100 {
		t.Fatalf("Expected 100 dead letter issues for job %s, got %d", jobID, dlCount)
	}
}
