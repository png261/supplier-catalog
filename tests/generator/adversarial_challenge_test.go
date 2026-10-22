package generator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sync"
	"testing"

	"coderpush/pkg/generator"
)

// TestAdversarial_SeedDeterminism_100Batches verifies byte-for-byte identity across 100 batches (10,000 records)
// between two independently constructed generator instances initialized with the exact same seed (seed=1337).
func TestAdversarial_SeedDeterminism_100Batches(t *testing.T) {
	const (
		seed         = uint64(1337)
		totalBatches = 100
		batchSize    = 100
	)

	cfg := generator.Config{
		Seed:              seed,
		TotalBatches:      totalBatches,
		BatchSize:         batchSize,
		UpdateRatio:       0.20,
		DuplicateRatio:    0.05,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		InvalidRatio:      0.05,
		ReservoirCapacity: 1000,
	}

	gen1, err := generator.New(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize gen1: %v", err)
	}

	gen2, err := generator.New(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize gen2: %v", err)
	}

	ctx := context.Background()
	stream1 := gen1.StreamBatches(ctx, "job-det-stream-1")
	stream2 := gen2.StreamBatches(ctx, "job-det-stream-1") // identical jobID

	totalRecordsCompared := 0

	for b := 0; b < totalBatches; b++ {
		batch1, ok1 := <-stream1
		batch2, ok2 := <-stream2

		if !ok1 || !ok2 {
			t.Fatalf("Stream closed prematurely at batch %d: ok1=%v, ok2=%v", b, ok1, ok2)
		}

		if batch1.BatchIndex != b || batch2.BatchIndex != b {
			t.Fatalf("Batch index mismatch at %d: batch1=%d, batch2=%d", b, batch1.BatchIndex, batch2.BatchIndex)
		}

		if len(batch1.Records) != len(batch2.Records) {
			t.Fatalf("Batch %d record length mismatch: %d vs %d", b, len(batch1.Records), len(batch2.Records))
		}

		for r := 0; r < len(batch1.Records); r++ {
			rec1 := batch1.Records[r]
			rec2 := batch2.Records[r]

			// Assert field-by-field equality: SKU, Name, Price, Status, Timestamp
			if rec1.SKU != rec2.SKU {
				t.Fatalf("Batch %d Record %d SKU mismatch: %q vs %q", b, r, rec1.SKU, rec2.SKU)
			}
			if rec1.Name != rec2.Name {
				t.Fatalf("Batch %d Record %d Name mismatch: %q vs %q", b, r, rec1.Name, rec2.Name)
			}
			if rec1.Status != rec2.Status {
				t.Fatalf("Batch %d Record %d Status mismatch: %v vs %v", b, r, rec1.Status, rec2.Status)
			}
			if !rec1.Timestamp.Equal(rec2.Timestamp) {
				t.Fatalf("Batch %d Record %d Timestamp mismatch: %v vs %v", b, r, rec1.Timestamp, rec2.Timestamp)
			}

			// For Price, check NaN or Inf equality or exact bitwise float64 equality
			price1Bits := math.Float64bits(rec1.Price)
			price2Bits := math.Float64bits(rec2.Price)
			if price1Bits != price2Bits {
				if !(math.IsNaN(rec1.Price) && math.IsNaN(rec2.Price)) {
					t.Fatalf("Batch %d Record %d Price mismatch: %v vs %v", b, r, rec1.Price, rec2.Price)
				}
			}

			totalRecordsCompared++
		}

		// Also verify byte-for-byte JSON marshaled representation
		json1, err1 := generator.MarshalBatchJSON(batch1)
		json2, err2 := generator.MarshalBatchJSON(batch2)
		if err1 != nil || err2 != nil {
			t.Fatalf("JSON marshal error: %v, %v", err1, err2)
		}
		if !bytes.Equal(json1, json2) {
			t.Fatalf("Batch %d JSON bytes diverged between identical seeds!", b)
		}
	}

	if totalRecordsCompared != totalBatches*batchSize {
		t.Fatalf("Expected %d records compared, got %d", totalBatches*batchSize, totalRecordsCompared)
	}
}

// TestAdversarial_SeedDivergence_ImmediateDifference tests immediate divergence between adjacent seeds
// across multiple test pairs, confirming that divergence occurs at Batch 0, Record 0.
func TestAdversarial_SeedDivergence_ImmediateDifference(t *testing.T) {
	pairs := [][2]uint64{
		{100, 101},
		{0, 1},
		{1337, 1338},
		{42, 43},
		{999999, 1000000},
	}

	for _, pair := range pairs {
		t.Run(fmt.Sprintf("Seeds_%d_vs_%d", pair[0], pair[1]), func(t *testing.T) {
			cfg1 := generator.DefaultConfig()
			cfg1.Seed = pair[0]
			cfg1.TotalBatches = 1
			cfg1.BatchSize = 10

			cfg2 := generator.DefaultConfig()
			cfg2.Seed = pair[1]
			cfg2.TotalBatches = 1
			cfg2.BatchSize = 10

			gen1, err := generator.New(cfg1)
			if err != nil {
				t.Fatalf("Failed to initialize gen1: %v", err)
			}
			gen2, err := generator.New(cfg2)
			if err != nil {
				t.Fatalf("Failed to initialize gen2: %v", err)
			}

			batch1 := gen1.GenerateBatch("job-div", 0)
			batch2 := gen2.GenerateBatch("job-div", 0)

			rec1 := batch1.Records[0]
			rec2 := batch2.Records[0]

			// Verify divergence at record 0
			identical := rec1.SKU == rec2.SKU &&
				rec1.Name == rec2.Name &&
				rec1.Price == rec2.Price &&
				rec1.Status == rec2.Status &&
				rec1.Timestamp.Equal(rec2.Timestamp)

			if identical {
				t.Fatalf("Seeds %d and %d produced identical Record 0: %+v", pair[0], pair[1], rec1)
			}

			// Check JSON marshaled bytes difference
			j1, _ := json.Marshal(rec1)
			j2, _ := json.Marshal(rec2)
			if bytes.Equal(j1, j2) {
				t.Fatalf("Seeds %d and %d produced identical JSON representation at Record 0: %s", pair[0], pair[1], string(j1))
			}
		})
	}
}

// TestAdversarial_BoundedHeap_O1_100kPlus verifies strict O(1) heap allocation over 120,000+ records.
// It samples heap memory via runtime.ReadMemStats after forced GC at 5,000, 40,000, 80,000, and 120,000 records.
func TestAdversarial_BoundedHeap_O1_100kPlus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 120,000-item memory test in short mode")
	}

	const (
		totalBatches = 1200
		batchSize    = 100 // 120,000 records
	)

	cfg := generator.Config{
		Seed:              20261021,
		TotalBatches:      totalBatches,
		BatchSize:         batchSize,
		UpdateRatio:       0.20,
		DuplicateRatio:    0.05,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		InvalidRatio:      0.05,
		ReservoirCapacity: 1000,
	}

	gen, err := generator.New(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize generator: %v", err)
	}

	ctx := context.Background()
	stream := gen.StreamBatches(ctx, "job-o1-heap-stress")

	var (
		mWarmup  runtime.MemStats
		mMid1    runtime.MemStats
		mMid2    runtime.MemStats
		mEnd     runtime.MemStats
		totalRec int
	)

	// Sample heap after baseline warmup to ignore initial runtime init overhead
	for batch := range stream {
		totalRec += len(batch.Records)

		switch totalRec {
		case 5000:
			runtime.GC()
			runtime.ReadMemStats(&mWarmup)
		case 40000:
			runtime.GC()
			runtime.ReadMemStats(&mMid1)
		case 80000:
			runtime.GC()
			runtime.ReadMemStats(&mMid2)
		case 120000:
			runtime.GC()
			runtime.ReadMemStats(&mEnd)
		}
	}

	if totalRec != 120000 {
		t.Fatalf("Expected 120,000 records, got %d", totalRec)
	}

	deltaWarmupToEnd := int64(mEnd.HeapAlloc) - int64(mWarmup.HeapAlloc)
	deltaMid1ToEnd := int64(mEnd.HeapAlloc) - int64(mMid1.HeapAlloc)
	deltaMid2ToEnd := int64(mEnd.HeapAlloc) - int64(mMid2.HeapAlloc)

	const maxGrowthBytes = 2 * 1024 * 1024 // 2MB ceiling

	t.Logf("HeapAlloc: Warmup(5k)=%d B, Mid1(40k)=%d B, Mid2(80k)=%d B, End(120k)=%d B",
		mWarmup.HeapAlloc, mMid1.HeapAlloc, mMid2.HeapAlloc, mEnd.HeapAlloc)
	t.Logf("HeapAlloc Delta (Warmup to End): %d B (%.2f KB)", deltaWarmupToEnd, float64(deltaWarmupToEnd)/1024)
	t.Logf("HeapAlloc Delta (Mid1 to End): %d B (%.2f KB)", deltaMid1ToEnd, float64(deltaMid1ToEnd)/1024)
	t.Logf("HeapAlloc Delta (Mid2 to End): %d B (%.2f KB)", deltaMid2ToEnd, float64(deltaMid2ToEnd)/1024)

	if deltaWarmupToEnd > maxGrowthBytes {
		t.Fatalf("O(1) heap memory bound violated! Grew by %d bytes (ceiling %d bytes)",
			deltaWarmupToEnd, maxGrowthBytes)
	}

	// Also verify that growth between 40k and 120k is essentially flat (< 1MB)
	if deltaMid1ToEnd > 1*1024*1024 {
		t.Fatalf("Heap growth between 40k and 120k records exceeded 1MB: %d bytes", deltaMid1ToEnd)
	}
}

// TestAdversarial_ConcurrencyStressTest spawns 10 concurrent goroutines calling GenerateBatch
// on the same Generator instance. Run under go test -race to assert 0 data races, 0 deadlocks,
// and consistent batch structures.
func TestAdversarial_ConcurrencyStressTest(t *testing.T) {
	const (
		numGoroutines     = 10
		batchesPerRoutine = 50
		batchSize         = 100
	)

	cfg := generator.Config{
		Seed:              999888777,
		TotalBatches:      0, // infinite so batch generation is unconstrained
		BatchSize:         batchSize,
		UpdateRatio:       0.20,
		DuplicateRatio:    0.05,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		InvalidRatio:      0.05,
		ReservoirCapacity: 1000,
	}

	gen, err := generator.New(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize generator: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*batchesPerRoutine)

	startGate := make(chan struct{})

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			<-startGate // synchronize start

			for b := 0; b < batchesPerRoutine; b++ {
				batchIdx := routineID*batchesPerRoutine + b
				batch := gen.GenerateBatch(fmt.Sprintf("job-conc-%d", routineID), batchIdx)

				if batch.BatchIndex != batchIdx {
					errCh <- fmt.Errorf("routine %d batch %d: expected batchIndex %d, got %d",
						routineID, b, batchIdx, batch.BatchIndex)
					return
				}

				if len(batch.Records) != batchSize {
					errCh <- fmt.Errorf("routine %d batch %d: expected %d records, got %d",
						routineID, b, batchSize, len(batch.Records))
					return
				}

				// Verify each record is either valid or one of the calibrated Invariant D defect types
				for _, rec := range batch.Records {
					if rec.SKU == "" && rec.Price < 0 {
						// Double defect unexpected
						errCh <- fmt.Errorf("routine %d: unexpected double defect in record: %+v", routineID, rec)
						return
					}
				}
			}
		}(g)
	}

	close(startGate) // unleash all goroutines simultaneously
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrency test error: %v", err)
	}
}

// TestAdversarial_Concurrency_CleanStream tests 10 concurrent goroutines with InvalidRatio=0
// to assert that all records have non-empty SKUs and valid prices/statuses under race detection.
func TestAdversarial_Concurrency_CleanStream(t *testing.T) {
	const (
		numGoroutines     = 10
		batchesPerRoutine = 30
		batchSize         = 50
	)

	cfg := generator.Config{
		Seed:              123456789,
		TotalBatches:      0,
		BatchSize:         batchSize,
		UpdateRatio:       0.20,
		DuplicateRatio:    0.10,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.10,
		InvalidRatio:      0.0, // Zero invalid records
		ReservoirCapacity: 1000,
	}

	gen, err := generator.New(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize generator: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*batchesPerRoutine)
	startGate := make(chan struct{})

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			<-startGate

			for b := 0; b < batchesPerRoutine; b++ {
				batchIdx := routineID*batchesPerRoutine + b
				batch := gen.GenerateBatch(fmt.Sprintf("clean-job-%d", routineID), batchIdx)

				if len(batch.Records) != batchSize {
					errCh <- fmt.Errorf("routine %d batch %d: expected %d records, got %d", routineID, b, batchSize, len(batch.Records))
					return
				}

				for _, rec := range batch.Records {
					if rec.SKU == "" {
						errCh <- fmt.Errorf("routine %d: empty SKU generated when InvalidRatio=0", routineID)
						return
					}
					if rec.Price <= 0 || math.IsNaN(rec.Price) || math.IsInf(rec.Price, 0) {
						errCh <- fmt.Errorf("routine %d: invalid price %f generated when InvalidRatio=0", routineID, rec.Price)
						return
					}
					if !rec.Status.IsValid() {
						errCh <- fmt.Errorf("routine %d: invalid status %s generated when InvalidRatio=0", routineID, rec.Status)
						return
					}
					if rec.Timestamp.IsZero() {
						errCh <- fmt.Errorf("routine %d: zero timestamp generated when InvalidRatio=0", routineID)
						return
					}
				}
			}
		}(g)
	}

	close(startGate)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Clean concurrency error: %v", err)
	}
}

// TestAdversarial_ExtremeConfigurations tests boundary configurations:
// - 100% corrupt records (InvalidRatio = 1.0)
// - 100% updates (UpdateRatio = 1.0)
// - Small batch size (1 record per batch)
// - Small reservoir capacity (1 item)
func TestAdversarial_ExtremeConfigurations(t *testing.T) {
	// 1. 100% corrupt records
	t.Run("100PercentCorrupt", func(t *testing.T) {
		cfg := generator.Config{
			Seed:              777,
			TotalBatches:      5,
			BatchSize:         20,
			InvalidRatio:      1.0,
			ReservoirCapacity: 100,
		}
		gen, err := generator.New(cfg)
		if err != nil {
			t.Fatalf("Failed to initialize generator: %v", err)
		}

		for b := 0; b < 5; b++ {
			batch := gen.GenerateBatch("job-corrupt-all", b)
			for i, rec := range batch.Records {
				isInvalid := rec.Validate() != nil ||
					math.IsNaN(rec.Price) ||
					math.IsInf(rec.Price, 0)
				if !isInvalid {
					t.Fatalf("Batch %d Record %d was expected to be invalid under 100%% corrupt ratio: %+v", b, i, rec)
				}
			}
		}
	})

	// 2. 100% updates (reservoir warmup fallback handling)
	t.Run("100PercentUpdates", func(t *testing.T) {
		cfg := generator.Config{
			Seed:              888,
			TotalBatches:      5,
			BatchSize:         20,
			UpdateRatio:       1.0,
			ReservoirCapacity: 100,
		}
		gen, err := generator.New(cfg)
		if err != nil {
			t.Fatalf("Failed to initialize generator: %v", err)
		}

		// First record should fallback to new product because reservoir is empty
		batch0 := gen.GenerateBatch("job-update-all", 0)
		if len(batch0.Records) != 20 {
			t.Fatalf("Expected 20 records, got %d", len(batch0.Records))
		}
		// Subsequent batches should all be valid forward updates
		batch1 := gen.GenerateBatch("job-update-all", 1)
		for _, rec := range batch1.Records {
			if rec.SKU == "" || rec.Price <= 0 || !rec.Status.IsValid() {
				t.Fatalf("Invalid record generated under 100%% update ratio: %+v", rec)
			}
		}
	})

	// 3. BatchSize = 1 and ReservoirCapacity = 1
	t.Run("BatchSize1_Reservoir1", func(t *testing.T) {
		cfg := generator.Config{
			Seed:              999,
			TotalBatches:      10,
			BatchSize:         1,
			ReservoirCapacity: 1,
			UpdateRatio:       0.20,
			DuplicateRatio:    0.20,
		}
		gen, err := generator.New(cfg)
		if err != nil {
			t.Fatalf("Failed to initialize generator: %v", err)
		}

		for b := 0; b < 10; b++ {
			batch := gen.GenerateBatch("job-tiny", b)
			if len(batch.Records) != 1 {
				t.Fatalf("Expected 1 record in batch %d, got %d", b, len(batch.Records))
			}
		}
	})
}
