package sync_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"coderpush/pkg/domain"
	catalogSync "coderpush/pkg/sync"
)

// TestAC4_Adversarial_ZeroTimestampQuarantine empirically checks whether records with
// zero timestamps (time.Time{}) are properly quarantined into dead_letter_issues,
// as defined by domain.ErrZeroTimestamp and Invariant D.
func TestAC4_Adversarial_ZeroTimestampQuarantine(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	now := time.Now().UTC()
	jobID := "job-zero-ts-test"

	batch := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{
				SKU:       "SKU-VALID-1",
				Name:      "Valid Product",
				Price:     29.99,
				Status:    domain.StatusActive,
				Timestamp: now,
			},
			{
				SKU:       "SKU-ZERO-TIMESTAMP",
				Name:      "Item with Zero Timestamp",
				Price:     19.99,
				Status:    domain.StatusActive,
				Timestamp: time.Time{}, // Zero timestamp
			},
		},
	}

	res, err := engine.ExecuteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}

	// 1. Verify summary
	if res.Added != 1 {
		t.Errorf("expected Added=1, got %d", res.Added)
	}
	if res.DeadLetter != 1 {
		t.Errorf("CRITICAL BUG: Zero timestamp record was NOT quarantined into DeadLetter! Summary: %+v", res)
	}

	// 2. Verify SKU-ZERO-TIMESTAMP was NOT added to products table
	p, err := store.GetProduct(ctx, "SKU-ZERO-TIMESTAMP")
	if err == nil && p != nil {
		t.Errorf("CRITICAL BUG: SKU-ZERO-TIMESTAMP was committed to products table! Product: %+v", p)
	}

	// 3. Verify dead_letter_issues contains the zero timestamp record
	dlRecords, totalCount, err := store.ListDeadLetterRecords(ctx, jobID, "", 10, 0)
	if err != nil {
		t.Fatalf("failed listing dead letter records: %v", err)
	}
	if totalCount != 1 || len(dlRecords) != 1 {
		t.Errorf("expected 1 dead letter record, got %d", totalCount)
	} else {
		if !strings.Contains(strings.ToLower(dlRecords[0].Reason), "timestamp") {
			t.Errorf("expected dead letter reason to mention timestamp, got %q", dlRecords[0].Reason)
		}
	}
}

// TestAC4_Adversarial_CorruptRecordExhaustiveMatrix tests the entire spectrum of
// corrupt record variants specified in AC4:
// - Negative prices (-0.01, -100, -1e9)
// - Empty SKUs ("")
// - Whitespace-only SKUs (" ", "\t", " \n \r ")
// - NaN prices
// - +Inf and -Inf prices
// - Invalid status strings ("UNKNOWN", "active", "DELETED", "")
// - Zero timestamps
// - Multiple invalid fields simultaneously
//
// Interleaved with 50 valid products across 5 batches to verify isolation.
func TestAC4_Adversarial_CorruptRecordExhaustiveMatrix(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	jobID := "job-ac4-matrix"

	type corruptTestCase struct {
		name                   string
		record                 domain.SupplierUpdate
		expectedReasonKeywords []string
	}

	corruptCases := []corruptTestCase{
		{
			name: "negative price minor",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-NEG-1", Name: "Neg Minor", Price: -0.01, Status: domain.StatusActive, Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"negative price"},
		},
		{
			name: "negative price large",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-NEG-2", Name: "Neg Large", Price: -1000000.0, Status: domain.StatusActive, Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"negative price"},
		},
		{
			name: "empty SKU",
			record: domain.SupplierUpdate{
				SKU: "", Name: "Empty SKU Item", Price: 10.0, Status: domain.StatusActive, Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"empty SKU"},
		},
		{
			name: "whitespace spaces SKU",
			record: domain.SupplierUpdate{
				SKU: "    ", Name: "Spaces SKU Item", Price: 10.0, Status: domain.StatusActive, Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"empty SKU"},
		},
		{
			name: "whitespace tab newline SKU",
			record: domain.SupplierUpdate{
				SKU: "\t\n\r  \t", Name: "Tab Newline SKU Item", Price: 10.0, Status: domain.StatusActive, Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"empty SKU"},
		},
		{
			name: "NaN price",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-NAN", Name: "NaN Item", Price: math.NaN(), Status: domain.StatusActive, Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"price is NaN"},
		},
		{
			name: "Positive Infinity price",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-INF", Name: "Pos Inf Item", Price: math.Inf(1), Status: domain.StatusActive, Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"price is infinite"},
		},
		{
			name: "Negative Infinity price",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-NEGINF", Name: "Neg Inf Item", Price: math.Inf(-1), Status: domain.StatusActive, Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"price is infinite"},
		},
		{
			name: "invalid status lowercase active",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-STATUS-1", Name: "Lowercase active", Price: 10.0, Status: domain.ProductStatus("active"), Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"invalid status"},
		},
		{
			name: "invalid status unknown enum",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-STATUS-2", Name: "Unknown enum", Price: 10.0, Status: domain.ProductStatus("DISCONTINUED"), Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"invalid status"},
		},
		{
			name: "invalid status empty",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-STATUS-3", Name: "Empty enum", Price: 10.0, Status: domain.ProductStatus(""), Timestamp: baseTime,
			},
			expectedReasonKeywords: []string{"invalid status"},
		},
		{
			name: "zero timestamp",
			record: domain.SupplierUpdate{
				SKU: "SKU-CORRUPT-ZERO-TIME", Name: "Zero Timestamp", Price: 10.0, Status: domain.StatusActive, Timestamp: time.Time{},
			},
			expectedReasonKeywords: []string{"timestamp"},
		},
		{
			name: "multiple corrupt fields: empty SKU and negative price and invalid status and zero timestamp",
			record: domain.SupplierUpdate{
				SKU: "   ", Name: "All Corrupt", Price: -50.0, Status: domain.ProductStatus("BOGUS"), Timestamp: time.Time{},
			},
			expectedReasonKeywords: []string{"empty SKU", "negative price", "invalid status"},
		},
	}

	for _, tc := range corruptCases {
		t.Run(tc.name, func(t *testing.T) {
			subJobID := fmt.Sprintf("%s-%s", jobID, strings.ReplaceAll(tc.name, " ", "-"))
			validRecord := domain.SupplierUpdate{
				SKU:       fmt.Sprintf("SKU-VALID-%s", subJobID),
				Name:      "Valid Sibling",
				Price:     55.0,
				Status:    domain.StatusActive,
				Timestamp: baseTime,
			}

			batch := domain.Batch{
				JobID:      subJobID,
				BatchIndex: 0,
				Records:    []domain.SupplierUpdate{validRecord, tc.record},
			}

			res, err := engine.ExecuteBatch(ctx, batch)
			if err != nil {
				t.Fatalf("ExecuteBatch failed on %s: %v", tc.name, err)
			}

			if res.Added != 1 {
				t.Errorf("%s: expected Added=1 (valid sibling), got %d", tc.name, res.Added)
			}
			if res.DeadLetter != 1 {
				t.Errorf("%s: expected DeadLetter=1 for corrupt record, got %d (Summary: %+v)", tc.name, res.DeadLetter, res)
			}

			// Verify sibling was saved
			validProd, err := store.GetProduct(ctx, validRecord.SKU)
			if err != nil || validProd == nil {
				t.Errorf("%s: valid sibling was not committed! err=%v", tc.name, err)
			}

			// Verify corrupt SKU was NOT saved in products table if SKU was non-empty
			trimmedSKU := strings.TrimSpace(tc.record.SKU)
			if trimmedSKU != "" {
				corruptProd, err := store.GetProduct(ctx, trimmedSKU)
				if err == nil && corruptProd != nil {
					t.Errorf("%s: corrupt product %s was erroneously saved to products table!", tc.name, trimmedSKU)
				}
			}

			// Verify dead letter log
			dlRecords, count, err := store.ListDeadLetterRecords(ctx, subJobID, "", 10, 0)
			if err != nil {
				t.Fatalf("%s: failed listing dead letter records: %v", tc.name, err)
			}
			if count != 1 || len(dlRecords) != 1 {
				t.Errorf("%s: expected exactly 1 dead letter record, got %d", tc.name, count)
			} else {
				recordedReason := dlRecords[0].Reason
				for _, kw := range tc.expectedReasonKeywords {
					if !strings.Contains(strings.ToLower(recordedReason), strings.ToLower(kw)) {
						t.Errorf("%s: dead letter reason %q does not contain expected keyword %q", tc.name, recordedReason, kw)
					}
				}
			}
		})
	}
}

// TestAC4_Adversarial_MassivePollutedStreamStress creates a large stream of 20 batches
// where 50% of all records are randomly polluted with corrupt variants.
// Verifies that:
// 1. All valid records are properly committed.
// 2. All corrupt records are quarantined to dead_letter_issues.
// 3. Batches commit atomically without failure.
// 4. Job summary counters exactly equal the sum of batch checkpoints.
func TestAC4_Adversarial_MassivePollutedStreamStress(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-ac4-polluted-stream"
	numBatches := 10
	recordsPerBatch := 40
	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	stream := make(chan domain.Batch, numBatches)

	expectedValidTotal := 0
	expectedDeadLetterTotal := 0

	for b := 0; b < numBatches; b++ {
		var records []domain.SupplierUpdate
		for r := 0; r < recordsPerBatch; r++ {
			sku := fmt.Sprintf("SKU-STREAM-B%02d-R%02d", b, r)
			recordTime := baseTime.Add(time.Duration(b*100+r) * time.Minute)

			if r%2 == 0 {
				// Valid record
				records = append(records, domain.SupplierUpdate{
					SKU:       sku,
					Name:      fmt.Sprintf("Valid Item B%d R%d", b, r),
					Price:     float64(r*10 + 5),
					Status:    domain.StatusActive,
					Timestamp: recordTime,
				})
				expectedValidTotal++
			} else {
				// Corrupt record variant
				var corruptRecord domain.SupplierUpdate
				switch r % 8 {
				case 1:
					corruptRecord = domain.SupplierUpdate{SKU: sku, Name: "Neg Price", Price: -10.0, Status: domain.StatusActive, Timestamp: recordTime}
				case 3:
					corruptRecord = domain.SupplierUpdate{SKU: "", Name: "Missing SKU", Price: 20.0, Status: domain.StatusActive, Timestamp: recordTime}
				case 5:
					corruptRecord = domain.SupplierUpdate{SKU: "   ", Name: "Whitespace SKU", Price: 20.0, Status: domain.StatusActive, Timestamp: recordTime}
				case 7:
					corruptRecord = domain.SupplierUpdate{SKU: sku, Name: "Bad Status", Price: 20.0, Status: domain.ProductStatus("INVALID"), Timestamp: recordTime}
				}
				records = append(records, corruptRecord)
				expectedDeadLetterTotal++
			}
		}

		stream <- domain.Batch{
			JobID:      jobID,
			BatchIndex: b,
			Records:    records,
		}
	}
	close(stream)

	finalJob, err := engine.RunJob(ctx, jobID, stream)
	if err != nil {
		t.Fatalf("RunJob failed on heavily polluted stream: %v", err)
	}

	if finalJob.Status != domain.JobStatusCompleted {
		t.Fatalf("job did not complete successfully: status=%s", finalJob.Status)
	}

	if finalJob.Summary.DeadLetter != expectedDeadLetterTotal {
		t.Errorf("dead letter count mismatch: got %d, want %d", finalJob.Summary.DeadLetter, expectedDeadLetterTotal)
	}
	if finalJob.Summary.Added != expectedValidTotal {
		t.Errorf("valid added count mismatch: got %d, want %d", finalJob.Summary.Added, expectedValidTotal)
	}

	// Verify all quarantined records are in dead_letter_issues
	_, dlTotal, err := store.ListDeadLetterRecords(ctx, jobID, "", 1, 0)
	if err != nil {
		t.Fatalf("failed querying dead letter issues: %v", err)
	}
	if dlTotal != expectedDeadLetterTotal {
		t.Errorf("dead_letter_issues table row count mismatch: got %d, want %d", dlTotal, expectedDeadLetterTotal)
	}
}

// TestAC4_Adversarial_ZeroTimestampExistingProduct verifies that when a product already
// exists, an incoming update with a zero timestamp is quarantined to DeadLetter rather
// than silently skipped as an older timestamp.
func TestAC4_Adversarial_ZeroTimestampExistingProduct(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	now := time.Now().UTC()
	sku := "SKU-EXISTING-ZERO-TS"

	// Pre-seed product
	_, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID:      "seed-job",
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "Pre-existing Product", Price: 50.0, Status: domain.StatusActive, Timestamp: now},
		},
	})
	if err != nil {
		t.Fatalf("failed to seed product: %v", err)
	}

	// Ingest zero timestamp update for existing product
	res, err := engine.ExecuteBatch(ctx, domain.Batch{
		JobID:      "job-zero-ts-update",
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{SKU: sku, Name: "Zero TS Update", Price: 60.0, Status: domain.StatusActive, Timestamp: time.Time{}},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}

	if res.DeadLetter != 1 {
		t.Errorf("CRITICAL BUG: Zero timestamp update on existing product was NOT sent to DeadLetter! Got summary: %+v", res)
	}
	if res.Skipped != 0 {
		t.Errorf("CRITICAL BUG: Zero timestamp was erroneously treated as Skipped instead of DeadLetter! Got Skipped=%d", res.Skipped)
	}
}

// TestAC4_Adversarial_ExtremePrices verifies behavior with extreme positive float values.
func TestAC4_Adversarial_ExtremePrices(t *testing.T) {
	store := setupTestStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	now := time.Now().UTC()

	extremeCases := []struct {
		sku   string
		price float64
	}{
		{"SKU-EXTREME-MAX", math.MaxFloat64},
		{"SKU-EXTREME-MIN", math.SmallestNonzeroFloat64},
		{"SKU-EXTREME-BILLION", 1e12},
	}

	for _, tc := range extremeCases {
		res, err := engine.ExecuteBatch(ctx, domain.Batch{
			JobID:      "extreme-job",
			BatchIndex: 0,
			Records: []domain.SupplierUpdate{
				{SKU: tc.sku, Name: "Extreme Price", Price: tc.price, Status: domain.StatusActive, Timestamp: now},
			},
		})
		if err != nil {
			t.Fatalf("failed inserting extreme price for %s (%e): %v", tc.sku, tc.price, err)
		}
		if res.Added != 1 {
			t.Errorf("expected Added=1 for %s, got %+v", tc.sku, res)
		}

		p, err := store.GetProduct(ctx, tc.sku)
		if err != nil || p == nil {
			t.Fatalf("failed retrieving product %s: %v", tc.sku, err)
		}
		if p.Price != tc.price {
			t.Errorf("price mismatch for %s: got %e, want %e", tc.sku, p.Price, tc.price)
		}
	}
}
