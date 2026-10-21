package generator_test

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/generator"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// TestChallenger_Binomial3Sigma_N25000 verifies binomial 3-sigma ratio calibration
// over N = 25,000 records with configured ratios:
// UpdateRatio: 0.15, DuplicateRatio: 0.10, OutOfOrderRatio: 0.10, WithdrawnRatio: 0.05, InvalidRatio: 0.05
func TestChallenger_Binomial3Sigma_N25000(t *testing.T) {
	const (
		totalBatches = 250
		batchSize    = 100
		N            = totalBatches * batchSize // 25,000 records
	)

	seeds := []uint64{42, 20261021, 999999999, 12345678, 7777777}

	for _, seed := range seeds {
		t.Run(fmt.Sprintf("Seed-%d", seed), func(t *testing.T) {
			cfg := generator.Config{
				Seed:              seed,
				TotalBatches:      totalBatches,
				BatchSize:         batchSize,
				UpdateRatio:       0.15,
				DuplicateRatio:    0.10,
				OutOfOrderRatio:   0.10,
				WithdrawnRatio:    0.05,
				InvalidRatio:      0.05,
				ReservoirCapacity: 1000,
			}

			gen, err := generator.New(cfg)
			if err != nil {
				t.Fatalf("Failed to initialize generator: %v", err)
			}

			// Map from SKU to all previously seen records for that SKU
			// For empty SKU, we key by empty string or pointer
			skuHistory := make(map[string][]domain.SupplierUpdate)

			var (
				observedInvalid   int
				observedDuplicate int
				observedOOO       int
				observedWithdrawn int
				observedUpdate    int
				observedNew       int

				defectEmptySKU int
				defectNegPrice int
				defectNaNPrice int
				defectInfPrice int
				defectBadStat  int
				defectZeroTime int
			)

			totalRecords := 0

			for b := 0; b < totalBatches; b++ {
				batch := gen.GenerateBatch("challenger-job", b)
				if len(batch.Records) != batchSize {
					t.Fatalf("Batch %d had %d records, expected %d", b, len(batch.Records), batchSize)
				}

				for _, rec := range batch.Records {
					totalRecords++

					sku := rec.SKU
					prevList := skuHistory[sku]

					// Check if this record is an exact duplicate of ANY previous record for this SKU
					isDup := false
					for _, prev := range prevList {
						// For NaN prices, both are NaN
						priceMatches := (math.IsNaN(prev.Price) && math.IsNaN(rec.Price)) ||
							(math.IsInf(prev.Price, 1) && math.IsInf(rec.Price, 1)) ||
							(math.IsInf(prev.Price, -1) && math.IsInf(rec.Price, -1)) ||
							math.Abs(prev.Price-rec.Price) < 1e-6

						if prev.Name == rec.Name && priceMatches &&
							prev.Status == rec.Status && prev.Timestamp.Equal(rec.Timestamp) {
							isDup = true
							break
						}
					}

					if isDup {
						observedDuplicate++
						continue
					}

					// If not an exact duplicate, check Invariant D Invalid Record
					isInv := strings.TrimSpace(rec.SKU) == "" ||
						math.IsNaN(rec.Price) ||
						math.IsInf(rec.Price, 0) ||
						rec.Price < 0 ||
						!rec.Status.IsValid() ||
						rec.Timestamp.IsZero()

					if isInv {
						observedInvalid++
						if strings.TrimSpace(rec.SKU) == "" {
							defectEmptySKU++
						}
						if math.IsNaN(rec.Price) {
							defectNaNPrice++
						}
						if math.IsInf(rec.Price, 0) {
							defectInfPrice++
						}
						if rec.Price < 0 {
							defectNegPrice++
						}
						if !rec.Status.IsValid() {
							defectBadStat++
						}
						if rec.Timestamp.IsZero() {
							defectZeroTime++
						}
						skuHistory[sku] = append(skuHistory[sku], rec)
						continue
					}

					// Valid record: find maximum timestamp previously seen for this SKU
					if len(prevList) == 0 {
						if rec.Status == domain.StatusWithdrawn {
							observedWithdrawn++
						} else {
							observedNew++
						}
					} else {
						// Find max timestamp among valid records for this SKU
						var maxTime time.Time
						hasValidPrev := false
						for _, p := range prevList {
							if !p.Timestamp.IsZero() && (maxTime.IsZero() || p.Timestamp.After(maxTime)) {
								maxTime = p.Timestamp
								hasValidPrev = true
							}
						}

						if hasValidPrev && rec.Timestamp.Before(maxTime) {
							observedOOO++
						} else if rec.Status == domain.StatusWithdrawn {
							observedWithdrawn++
						} else {
							observedUpdate++
						}
					}

					skuHistory[sku] = append(skuHistory[sku], rec)
				}
			}

			if totalRecords != N {
				t.Fatalf("Total records generated %d != expected %d", totalRecords, N)
			}

			// Validate 3-sigma bounds for each configured ratio
			check3Sigma := func(label string, observed int, ratio float64) {
				mu := float64(N) * ratio
				sigma := math.Sqrt(float64(N) * ratio * (1.0 - ratio))
				lower := int(math.Floor(mu - 3.0*sigma))
				upper := int(math.Ceil(mu + 3.0*sigma))
				t.Logf("[%s] Ratio: %.2f | Observed: %d | Expected: %.1f | StdDev: %.2f | 3-Sigma: [%d, %d]",
					label, ratio, observed, mu, sigma, lower, upper)
				if observed < lower || observed > upper {
					t.Errorf("Binomial 3-sigma calibration FAIL for %s: observed %d, outside [%d, %d]",
						label, observed, lower, upper)
				}
			}

			check3Sigma("UpdateRatio", observedUpdate, cfg.UpdateRatio)
			check3Sigma("DuplicateRatio", observedDuplicate, cfg.DuplicateRatio)
			check3Sigma("OutOfOrderRatio", observedOOO, cfg.OutOfOrderRatio)
			check3Sigma("WithdrawnRatio", observedWithdrawn, cfg.WithdrawnRatio)
			check3Sigma("InvalidRatio", observedInvalid, cfg.InvalidRatio)

			// Verify all 6 Invariant D defects were observed
			t.Logf("Invariant D Defect breakdown: EmptySKU=%d, NegPrice=%d, NaNPrice=%d, InfPrice=%d, BadStatus=%d, ZeroTime=%d (Total=%d)",
				defectEmptySKU, defectNegPrice, defectNaNPrice, defectInfPrice, defectBadStat, defectZeroTime, observedInvalid)

			if defectEmptySKU == 0 || defectNegPrice == 0 || defectNaNPrice == 0 ||
				defectInfPrice == 0 || defectBadStat == 0 || defectZeroTime == 0 {
				t.Errorf("Invariant D 6-defect coverage incomplete: empty=%d, neg=%d, nan=%d, inf=%d, badStatus=%d, zeroTime=%d",
					defectEmptySKU, defectNegPrice, defectNaNPrice, defectInfPrice, defectBadStat, defectZeroTime)
			}
		})
	}
}

// TestChallenger_InvariantD_Uniformity verifies all 6 defect types are uniformly distributed
func TestChallenger_InvariantD_Uniformity(t *testing.T) {
	const (
		totalBatches = 100
		batchSize    = 100
		N            = totalBatches * batchSize // 10,000 records
	)

	// Set InvalidRatio to 0.60 to generate a large sample of ~6,000 invalid records
	cfg := generator.Config{
		Seed:              123456,
		TotalBatches:      totalBatches,
		BatchSize:         batchSize,
		InvalidRatio:      0.60,
		UpdateRatio:       0.10,
		DuplicateRatio:    0.10,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		ReservoirCapacity: 500,
	}

	gen, err := generator.New(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize generator: %v", err)
	}

	defectCounts := make(map[string]int)
	totalInvalid := 0

	for b := 0; b < totalBatches; b++ {
		batch := gen.GenerateBatch("job-inv-uniformity", b)
		for _, rec := range batch.Records {
			if strings.TrimSpace(rec.SKU) == "" {
				defectCounts["EmptySKU"]++
				totalInvalid++
			} else if math.IsNaN(rec.Price) {
				defectCounts["NaNPrice"]++
				totalInvalid++
			} else if math.IsInf(rec.Price, 0) {
				defectCounts["InfPrice"]++
				totalInvalid++
			} else if rec.Price < 0 {
				defectCounts["NegPrice"]++
				totalInvalid++
			} else if !rec.Status.IsValid() {
				defectCounts["BadStatus"]++
				totalInvalid++
			} else if rec.Timestamp.IsZero() {
				defectCounts["ZeroTime"]++
				totalInvalid++
			}
		}
	}

	t.Logf("Total invalid records analyzed: %d", totalInvalid)
	if totalInvalid < 5000 {
		t.Fatalf("Expected at least 5000 invalid records, got %d", totalInvalid)
	}

	// 6 defects, each has expected p = 1/6 of totalInvalid
	expectedPerDefect := float64(totalInvalid) / 6.0
	sigma := math.Sqrt(float64(totalInvalid) * (1.0 / 6.0) * (5.0 / 6.0))
	lower := int(math.Floor(expectedPerDefect - 3.5*sigma))
	upper := int(math.Ceil(expectedPerDefect + 3.5*sigma))

	for _, name := range []string{"EmptySKU", "NegPrice", "NaNPrice", "InfPrice", "BadStatus", "ZeroTime"} {
		count := defectCounts[name]
		t.Logf("Defect [%s]: count=%d, expected=%.1f, bounds=[%d, %d]", name, count, expectedPerDefect, lower, upper)
		if count < lower || count > upper {
			t.Errorf("Defect %s count %d outside uniform bounds [%d, %d]", name, count, lower, upper)
		}
	}
}

// TestChallenger_E2E_PipelineFeedAndAccountingConservation pipes generator output into SyncEngine.RunJob
// and verifies accounting conservation, dead letter isolation, idempotency, and SQLite invariants.
func TestChallenger_E2E_PipelineFeedAndAccountingConservation(t *testing.T) {
	testCases := []struct {
		name         string
		totalBatches int
		batchSize    int
		seed         uint64
		cfg          generator.Config
	}{
		{
			name:         "Balanced-N5000",
			totalBatches: 50,
			batchSize:    100,
			seed:         424242,
			cfg: generator.Config{
				UpdateRatio:     0.15,
				DuplicateRatio:  0.10,
				OutOfOrderRatio: 0.10,
				WithdrawnRatio:  0.05,
				InvalidRatio:    0.05,
			},
		},
		{
			name:         "HighCorruption-N2000",
			totalBatches: 20,
			batchSize:    100,
			seed:         999888,
			cfg: generator.Config{
				UpdateRatio:     0.10,
				DuplicateRatio:  0.10,
				OutOfOrderRatio: 0.10,
				WithdrawnRatio:  0.05,
				InvalidRatio:    0.30,
			},
		},
		{
			name:         "AllCorrupt-100PercentInvalid",
			totalBatches: 10,
			batchSize:    50,
			seed:         111222,
			cfg: generator.Config{
				InvalidRatio: 1.0,
			},
		},
		{
			name:         "AllNewProducts-ZeroAnomalies",
			totalBatches: 10,
			batchSize:    50,
			seed:         333444,
			cfg: generator.Config{
				UpdateRatio:     0.0,
				DuplicateRatio:  0.0,
				OutOfOrderRatio: 0.0,
				WithdrawnRatio:  0.0,
				InvalidRatio:    0.0,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "pipeline_test.db")
			store, err := storage.Open(dbPath)
			if err != nil {
				t.Fatalf("NewSQLiteStorage failed: %v", err)
			}
			defer store.Close()

			engine := catalogSync.NewEngine(store)
			ctx := context.Background()

			cfg := tc.cfg
			cfg.Seed = tc.seed
			cfg.TotalBatches = tc.totalBatches
			cfg.BatchSize = tc.batchSize
			cfg.ReservoirCapacity = 500

			gen, err := generator.New(cfg)
			if err != nil {
				t.Fatalf("Failed to create generator: %v", err)
			}

			jobID := fmt.Sprintf("job-challenger-%s", tc.name)
			stream := gen.StreamBatches(ctx, jobID)

			completedJob, err := engine.RunJob(ctx, jobID, stream)
			if err != nil {
				t.Fatalf("engine.RunJob failed: %v", err)
			}

			if completedJob.Status != domain.JobStatusCompleted {
				t.Fatalf("Expected job status COMPLETED, got %s", completedJob.Status)
			}

			totalExpectedRecords := tc.totalBatches * tc.batchSize
			summary := completedJob.Summary

			t.Logf("[%s] Results: Added=%d, Updated=%d, Withdrawn=%d, Skipped=%d, DeadLetter=%d | Total=%d",
				tc.name, summary.Added, summary.Updated, summary.Withdrawn, summary.Skipped, summary.DeadLetter, summary.TotalProcessed())

			// 1. Accounting conservation: Added + Updated + Withdrawn + Skipped + DeadLetter == N_total
			if summary.TotalProcessed() != totalExpectedRecords {
				t.Fatalf("Accounting conservation violated! Expected %d, got %d (+Added:%d, ~Updated:%d, -Withdrawn:%d, ↷Skipped:%d, ⚠DeadLetter:%d)",
					totalExpectedRecords, summary.TotalProcessed(),
					summary.Added, summary.Updated, summary.Withdrawn, summary.Skipped, summary.DeadLetter)
			}

			// 2. Dead letter quarantine verification: row count in dead_letter_issues == summary.DeadLetter
			dlRecords, totalDL, err := store.ListDeadLetterRecords(ctx, jobID, "", totalExpectedRecords+100, 0)
			if err != nil {
				t.Fatalf("ListDeadLetterRecords failed: %v", err)
			}
			if totalDL != summary.DeadLetter || len(dlRecords) != summary.DeadLetter {
				t.Fatalf("Dead letter table count %d (total %d) does not match summary %d",
					len(dlRecords), totalDL, summary.DeadLetter)
			}

			// Verify all quarantined records have non-empty reason
			for idx, dl := range dlRecords {
				if strings.TrimSpace(dl.Reason) == "" {
					t.Fatalf("Dead letter record #%d has empty reason: %+v", idx, dl)
				}
				if dl.JobID != jobID {
					t.Fatalf("Dead letter record #%d has jobID %s, expected %s", idx, dl.JobID, jobID)
				}
			}

			// 3. Catalog database hygiene: NO dead letter record should EVER enter products table
			products, err := queryAllProducts(ctx, store)
			if err != nil {
				t.Fatalf("queryAllProducts failed: %v", err)
			}
			t.Logf("[%s] Products in catalog: %d", tc.name, len(products))

			for _, p := range products {
				if strings.TrimSpace(p.SKU) == "" {
					t.Fatalf("Catalog invariant violated: product has empty SKU! %+v", p)
				}
				if math.IsNaN(p.Price) || math.IsInf(p.Price, 0) || p.Price < 0 {
					t.Fatalf("Catalog invariant violated: product has corrupt price %f! %+v", p.Price, p)
				}
				if !p.Status.IsValid() {
					t.Fatalf("Catalog invariant violated: product has invalid status %s! %+v", p.Status, p)
				}
				if p.SupplierTimestamp.IsZero() {
					t.Fatalf("Catalog invariant violated: product has zero timestamp! %+v", p)
				}
			}

			// 4. Batch checkpoint completeness
			checkpoints, err := queryBatchCheckpoints(ctx, store, jobID)
			if err != nil {
				t.Fatalf("queryBatchCheckpoints failed: %v", err)
			}
			if len(checkpoints) != tc.totalBatches {
				t.Fatalf("Expected %d batch checkpoints, found %d", tc.totalBatches, len(checkpoints))
			}
			for i, cp := range checkpoints {
				if cp.BatchIndex != i {
					t.Fatalf("Checkpoint index gap: checkpoint #%d has BatchIndex %d", i, cp.BatchIndex)
				}
				if cp.ItemsCount != tc.batchSize {
					t.Fatalf("Checkpoint #%d itemsCount %d != expected %d", i, cp.ItemsCount, tc.batchSize)
				}
			}

			// 5. Special case assertions
			if tc.name == "AllCorrupt-100PercentInvalid" {
				if summary.DeadLetter != totalExpectedRecords {
					t.Fatalf("AllCorrupt: expected 100%% dead letter (%d), got %d", totalExpectedRecords, summary.DeadLetter)
				}
				if summary.Added != 0 || summary.Updated != 0 || summary.Withdrawn != 0 || summary.Skipped != 0 {
					t.Fatalf("AllCorrupt: expected 0 active records, got %+v", summary)
				}
				if len(products) != 0 {
					t.Fatalf("AllCorrupt: expected 0 products in database, got %d", len(products))
				}
			}

			if tc.name == "AllNewProducts-ZeroAnomalies" {
				if summary.Added != totalExpectedRecords {
					t.Fatalf("AllNewProducts: expected all %d records added, got %d", totalExpectedRecords, summary.Added)
				}
				if summary.DeadLetter != 0 || summary.Skipped != 0 || summary.Updated != 0 || summary.Withdrawn != 0 {
					t.Fatalf("AllNewProducts: expected 0 anomalies, got %+v", summary)
				}
				if len(products) != totalExpectedRecords {
					t.Fatalf("AllNewProducts: expected %d catalog products, got %d", totalExpectedRecords, len(products))
				}
			}
		})
	}
}

// TestChallenger_E2E_DuplicateIdempotency verifies re-playing identical batches causes 0 catalog drift.
func TestChallenger_E2E_DuplicateIdempotency(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "idempotency_test.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open failed: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	cfg := generator.Config{
		Seed:              55555,
		TotalBatches:      10,
		BatchSize:         50,
		UpdateRatio:       0.10,
		DuplicateRatio:    0.10,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		InvalidRatio:      0.05,
		ReservoirCapacity: 200,
	}

	gen1, _ := generator.New(cfg)
	jobID1 := "job-idempotency-pass-1"
	stream1 := gen1.StreamBatches(ctx, jobID1)

	job1, err := engine.RunJob(ctx, jobID1, stream1)
	if err != nil {
		t.Fatalf("RunJob 1 failed: %v", err)
	}
	if job1.Status != domain.JobStatusCompleted {
		t.Fatalf("Job 1 did not complete: %s", job1.Status)
	}

	// Capture catalog snapshot
	productsAfterJob1, err := queryAllProducts(ctx, store)
	if err != nil {
		t.Fatalf("queryAllProducts 1 failed: %v", err)
	}

	// Generate exact same batches using identical seed into a second job
	gen2, _ := generator.New(cfg)
	jobID2 := "job-idempotency-pass-2"
	stream2 := gen2.StreamBatches(ctx, jobID2)

	job2, err := engine.RunJob(ctx, jobID2, stream2)
	if err != nil {
		t.Fatalf("RunJob 2 failed: %v", err)
	}
	if job2.Status != domain.JobStatusCompleted {
		t.Fatalf("Job 2 did not complete: %s", job2.Status)
	}

	// Catalog must NOT have any new items Added! Everything must be Skipped (or DeadLetter for corrupt items)
	if job2.Summary.Added != 0 {
		t.Fatalf("Idempotency violated: job 2 added %d new products!", job2.Summary.Added)
	}

	productsAfterJob2, err := queryAllProducts(ctx, store)
	if err != nil {
		t.Fatalf("queryAllProducts 2 failed: %v", err)
	}

	if len(productsAfterJob1) != len(productsAfterJob2) {
		t.Fatalf("Catalog count drifted! Job 1: %d, Job 2: %d", len(productsAfterJob1), len(productsAfterJob2))
	}

	// Verify byte-for-byte or field-for-field equality
	prodMap1 := make(map[string]domain.Product)
	for _, p := range productsAfterJob1 {
		prodMap1[p.SKU] = p
	}
	for _, p2 := range productsAfterJob2 {
		p1, ok := prodMap1[p2.SKU]
		if !ok {
			t.Fatalf("SKU %s found after Job 2 but was not in Job 1!", p2.SKU)
		}
		if p1.Price != p2.Price || p1.Status != p2.Status || !p1.SupplierTimestamp.Equal(p2.SupplierTimestamp) {
			t.Fatalf("Product drift for SKU %s: Job1=%+v, Job2=%+v", p2.SKU, p1, p2)
		}
	}
}

// TestChallenger_OutOfOrderRecords_AlwaysSkipped explicitly checks that when older timestamps arrive,
// SyncEngine skips them (accounting in Skipped) and retains the newer database timestamp.
func TestChallenger_OutOfOrderRecords_AlwaysSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ooo_test.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Batch 0: Insert product with timestamp T_current
	batch0 := domain.Batch{
		JobID:      "job-ooo-check",
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{
				SKU:       "SKU-OOO-01",
				Name:      "OOO Baseline",
				Price:     50.00,
				Status:    domain.StatusActive,
				Timestamp: now,
			},
		},
	}

	// Batch 1: Insert same SKU with T_old < T_current (out of order)
	batch1 := domain.Batch{
		JobID:      "job-ooo-check",
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{
				SKU:       "SKU-OOO-01",
				Name:      "OOO Mutation Attempt",
				Price:     30.00,
				Status:    domain.StatusActive,
				Timestamp: now.Add(-2 * time.Hour), // 2 hours in the past
			},
		},
	}

	ch := make(chan domain.Batch, 2)
	ch <- batch0
	ch <- batch1
	close(ch)

	completedJob, err := engine.RunJob(ctx, "job-ooo-check", ch)
	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if completedJob.Summary.Added != 1 {
		t.Fatalf("Expected 1 Added, got %d", completedJob.Summary.Added)
	}
	if completedJob.Summary.Skipped != 1 {
		t.Fatalf("Expected 1 Skipped, got %d", completedJob.Summary.Skipped)
	}
	if completedJob.Summary.Updated != 0 {
		t.Fatalf("Expected 0 Updated, got %d", completedJob.Summary.Updated)
	}

	// Check catalog state: price must STILL be 50.00, not 30.00
	prod, err := store.GetProduct(ctx, "SKU-OOO-01")
	if err != nil {
		t.Fatalf("GetProduct failed: %v", err)
	}
	if prod.Price != 50.00 {
		t.Fatalf("Catalog corrupted by out-of-order record! Expected 50.00, got %f", prod.Price)
	}
	if !prod.SupplierTimestamp.Equal(now) {
		t.Fatalf("Catalog timestamp overwritten! Expected %s, got %s", now, prod.SupplierTimestamp)
	}
}

func queryAllProducts(ctx context.Context, store storage.Storage) ([]domain.Product, error) {
	rows, err := store.DB().QueryContext(ctx, "SELECT sku, name, price, status, supplier_timestamp, updated_at FROM products")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []domain.Product
	for rows.Next() {
		var p domain.Product
		var tsStr, updatedStr string
		if err := rows.Scan(&p.SKU, &p.Name, &p.Price, &p.Status, &tsStr, &updatedStr); err != nil {
			return nil, err
		}
		p.SupplierTimestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
		p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
		products = append(products, p)
	}
	return products, rows.Err()
}

func queryBatchCheckpoints(ctx context.Context, store storage.Storage, jobID string) ([]domain.BatchCheckpoint, error) {
	rows, err := store.DB().QueryContext(ctx,
		"SELECT job_id, batch_index, items_count, added_count, updated_count, withdrawn_count, skipped_count, dead_letter_count, committed_at FROM batch_checkpoints WHERE job_id = ? ORDER BY batch_index ASC",
		jobID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checkpoints []domain.BatchCheckpoint
	for rows.Next() {
		var cp domain.BatchCheckpoint
		var committedStr string
		if err := rows.Scan(
			&cp.JobID, &cp.BatchIndex, &cp.ItemsCount,
			&cp.Summary.Added, &cp.Summary.Updated, &cp.Summary.Withdrawn, &cp.Summary.Skipped, &cp.Summary.DeadLetter,
			&committedStr,
		); err != nil {
			return nil, err
		}
		cp.CommittedAt, _ = time.Parse(time.RFC3339Nano, committedStr)
		checkpoints = append(checkpoints, cp)
	}
	return checkpoints, rows.Err()
}
