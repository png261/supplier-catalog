package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

func TestGenerator_ConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(c *Config)
		wantError bool
		errMsg    string
	}{
		{
			name:      "default config is valid",
			modify:    func(c *Config) {},
			wantError: false,
		},
		{
			name: "batch_size zero",
			modify: func(c *Config) {
				c.BatchSize = 0
			},
			wantError: true,
			errMsg:    "batch_size must be greater than 0",
		},
		{
			name: "batch_size negative",
			modify: func(c *Config) {
				c.BatchSize = -5
			},
			wantError: true,
			errMsg:    "batch_size must be greater than 0",
		},
		{
			name: "reservoir_capacity zero",
			modify: func(c *Config) {
				c.ReservoirCapacity = 0
			},
			wantError: true,
			errMsg:    "reservoir_capacity must be greater than 0",
		},
		{
			name: "total_batches negative",
			modify: func(c *Config) {
				c.TotalBatches = -1
			},
			wantError: true,
			errMsg:    "total_batches cannot be negative",
		},
		{
			name: "update_ratio negative",
			modify: func(c *Config) {
				c.UpdateRatio = -0.1
			},
			wantError: true,
			errMsg:    "must be between 0.0 and 1.0",
		},
		{
			name: "duplicate_ratio greater than 1",
			modify: func(c *Config) {
				c.DuplicateRatio = 1.2
			},
			wantError: true,
			errMsg:    "must be between 0.0 and 1.0",
		},
		{
			name: "out_of_order_ratio NaN",
			modify: func(c *Config) {
				c.OutOfOrderRatio = math.NaN()
			},
			wantError: true,
			errMsg:    "must be between 0.0 and 1.0",
		},
		{
			name: "withdrawn_ratio Inf",
			modify: func(c *Config) {
				c.WithdrawnRatio = math.Inf(1)
			},
			wantError: true,
			errMsg:    "must be between 0.0 and 1.0",
		},
		{
			name: "sum of ratios greater than 1.0",
			modify: func(c *Config) {
				c.UpdateRatio = 0.50
				c.DuplicateRatio = 0.30
				c.OutOfOrderRatio = 0.30
			},
			wantError: true,
			errMsg:    "sum of anomaly and update ratios cannot exceed 1.0",
		},
		{
			name: "corrupt_ratio alias populates invalid_ratio",
			modify: func(c *Config) {
				c.InvalidRatio = 0.0
				c.CorruptRatio = 0.08
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestRingBuffer_Operations(t *testing.T) {
	rb := NewRingBuffer(3)
	if rb.Cap() != 3 {
		t.Fatalf("expected cap 3, got %d", rb.Cap())
	}
	if rb.Len() != 0 {
		t.Fatalf("expected len 0, got %d", rb.Len())
	}

	rng := rand.New(rand.NewPCG(1, 2))
	_, _, ok := rb.Sample(rng)
	if ok {
		t.Fatalf("expected sample on empty ring buffer to return ok=false")
	}

	now := time.Now()
	rb.Push(ReservoirItem{SKU: "SKU-1", Price: 10, Status: domain.StatusActive, Timestamp: now})
	rb.Push(ReservoirItem{SKU: "SKU-2", Price: 20, Status: domain.StatusActive, Timestamp: now})
	if rb.Len() != 2 {
		t.Fatalf("expected len 2, got %d", rb.Len())
	}

	item, idx, ok := rb.Sample(rng)
	if !ok || idx < 0 || idx >= 2 {
		t.Fatalf("expected valid sample, got item=%+v, idx=%d, ok=%v", item, idx, ok)
	}

	// Update at index
	rb.UpdateAt(idx, ReservoirItem{SKU: item.SKU, Price: 99, Status: domain.StatusWithdrawn, Timestamp: now})
	if rb.items[idx].Price != 99 || rb.items[idx].Status != domain.StatusWithdrawn {
		t.Fatalf("expected updated item at idx %d", idx)
	}

	// Update out of bounds should be a no-op
	rb.UpdateAt(-1, ReservoirItem{})
	rb.UpdateAt(99, ReservoirItem{})

	// Push 3rd item (full)
	rb.Push(ReservoirItem{SKU: "SKU-3", Price: 30, Status: domain.StatusActive, Timestamp: now})
	if rb.Len() != 3 {
		t.Fatalf("expected len 3, got %d", rb.Len())
	}

	// Push 4th item (wrap-around)
	rb.Push(ReservoirItem{SKU: "SKU-4", Price: 40, Status: domain.StatusActive, Timestamp: now})
	if rb.Len() != 3 {
		t.Fatalf("expected len 3 after wrap-around, got %d", rb.Len())
	}

	// Default capacity on <=0
	rbDefault := NewRingBuffer(0)
	if rbDefault.Cap() != 1000 {
		t.Fatalf("expected default cap 1000, got %d", rbDefault.Cap())
	}
}

func TestGenerator_SeedDeterminism_ByteForByteAcross100Batches(t *testing.T) {
	cfg := Config{
		Seed:              133742,
		TotalBatches:      100,
		BatchSize:         100, // 10,000 total items
		UpdateRatio:       0.20,
		DuplicateRatio:    0.05,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		InvalidRatio:      0.05,
		ReservoirCapacity: 1000,
	}

	gen1, err := New(cfg)
	if err != nil {
		t.Fatalf("New gen1 failed: %v", err)
	}
	gen2, err := New(cfg)
	if err != nil {
		t.Fatalf("New gen2 failed: %v", err)
	}

	if gen1.Config().Seed != cfg.Seed {
		t.Fatalf("Config().Seed mismatch")
	}

	for b := 0; b < cfg.TotalBatches; b++ {
		batch1 := gen1.GenerateBatch("job-det", b)
		batch2 := gen2.GenerateBatch("job-det", b)

		if len(batch1.Records) != len(batch2.Records) {
			t.Fatalf("batch %d record count mismatch: %d vs %d", b, len(batch1.Records), len(batch2.Records))
		}

		bytes1, err1 := MarshalBatchJSON(batch1)
		if err1 != nil {
			t.Fatalf("marshal error: %v", err1)
		}
		bytes2, err2 := MarshalBatchJSON(batch2)
		if err2 != nil {
			t.Fatalf("marshal error: %v", err2)
		}

		if !bytes.Equal(bytes1, bytes2) {
			t.Fatalf("batch %d diverged between identical seeds:\nRun1: %s\nRun2: %s", b, string(bytes1[:100]), string(bytes2[:100]))
		}
	}
}

func TestGenerator_SeedDivergence_ImmediateDifference(t *testing.T) {
	cfg1 := Config{Seed: 42, TotalBatches: 1, BatchSize: 50, ReservoirCapacity: 100}
	cfg2 := Config{Seed: 43, TotalBatches: 1, BatchSize: 50, ReservoirCapacity: 100}

	gen1, _ := New(cfg1)
	gen2, _ := New(cfg2)

	batch1 := gen1.GenerateBatch("job-div", 0)
	batch2 := gen2.GenerateBatch("job-div", 0)

	bytes1, _ := json.Marshal(batch1.Records)
	bytes2, _ := json.Marshal(batch2.Records)

	if bytes.Equal(bytes1, bytes2) {
		t.Fatalf("generators with seeds 42 and 43 produced identical batch 0!")
	}

	// Verify immediate divergence at record 0
	rec1, _ := json.Marshal(batch1.Records[0])
	rec2, _ := json.Marshal(batch2.Records[0])
	if bytes.Equal(rec1, rec2) {
		t.Fatalf("record 0 did not diverge between seed 42 and 43: %s vs %s", string(rec1), string(rec2))
	}
}

func TestGenerator_BoundedMemory_O1(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory growth test in short mode")
	}

	cfg := Config{
		Seed:              42,
		TotalBatches:      1000,
		BatchSize:         100, // 100,000 total items
		UpdateRatio:       0.20,
		DuplicateRatio:    0.05,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		InvalidRatio:      0.05,
		ReservoirCapacity: 1000,
	}

	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New generator failed: %v", err)
	}

	ctx := context.Background()
	stream := gen.StreamBatches(ctx, "job-mem-test")

	var mStart, mMid, mEnd runtime.MemStats

	itemsCount := 0
	for batch := range stream {
		itemsCount += len(batch.Records)

		if itemsCount == 5000 {
			runtime.GC()
			runtime.ReadMemStats(&mStart)
		} else if itemsCount == 50000 {
			runtime.GC()
			runtime.ReadMemStats(&mMid)
		} else if itemsCount == 100000 {
			runtime.GC()
			runtime.ReadMemStats(&mEnd)
		}
	}

	deltaEnd := int64(mEnd.HeapAlloc) - int64(mStart.HeapAlloc)
	const maxAllowedDelta = 2 * 1024 * 1024 // 2 MB ceiling
	if deltaEnd > maxAllowedDelta {
		t.Fatalf("O(1) memory invariant violated! HeapAlloc grew by %d bytes (limit %d bytes). Start: %d, End: %d",
			deltaEnd, maxAllowedDelta, mStart.HeapAlloc, mEnd.HeapAlloc)
	}
}

func TestGenerator_StatisticalRatios_3Sigma(t *testing.T) {
	const (
		totalBatches = 200
		batchSize    = 100
		N            = totalBatches * batchSize // 20,000 items
	)

	cfg := Config{
		Seed:              987654321,
		TotalBatches:      totalBatches,
		BatchSize:         batchSize,
		UpdateRatio:       0.20,
		DuplicateRatio:    0.05,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		InvalidRatio:      0.05,
		ReservoirCapacity: 1000,
	}

	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New generator failed: %v", err)
	}

	var (
		seenSKUs         = make(map[string]domain.SupplierUpdate)
		dupCount         int
		oooCount         int
		withdrawnCount   int
		invalidCount     int
		updateCount      int
		corruptEmptySKU  int
		corruptNegPrice  int
		corruptNaNPrice  int
		corruptInfPrice  int
		corruptBadStatus int
		corruptZeroTime  int
	)

	for b := 0; b < totalBatches; b++ {
		batch := gen.GenerateBatch("job-stat", b)
		for _, rec := range batch.Records {
			// Check if Invariant D invalid
			isInvalid := strings.TrimSpace(rec.SKU) == "" ||
				math.IsNaN(rec.Price) ||
				math.IsInf(rec.Price, 0) ||
				rec.Price < 0 ||
				!rec.Status.IsValid() ||
				rec.Timestamp.IsZero()

			if isInvalid {
				invalidCount++
				if strings.TrimSpace(rec.SKU) == "" {
					corruptEmptySKU++
				}
				if math.IsNaN(rec.Price) {
					corruptNaNPrice++
				}
				if math.IsInf(rec.Price, 0) {
					corruptInfPrice++
				}
				if rec.Price < 0 {
					corruptNegPrice++
				}
				if !rec.Status.IsValid() {
					corruptBadStatus++
				}
				if rec.Timestamp.IsZero() {
					corruptZeroTime++
				}
				continue
			}

			prev, exists := seenSKUs[rec.SKU]
			if exists {
				// Duplicate check
				if prev.Name == rec.Name && math.Abs(prev.Price-rec.Price) < 1e-6 &&
					prev.Status == rec.Status && prev.Timestamp.Equal(rec.Timestamp) {
					dupCount++
				} else if rec.Timestamp.Before(prev.Timestamp) {
					// Out of order check
					oooCount++
				} else if rec.Status == domain.StatusWithdrawn {
					// Withdrawn transition
					withdrawnCount++
					seenSKUs[rec.SKU] = rec
				} else if rec.Timestamp.After(prev.Timestamp) {
					// Forward update check
					updateCount++
					seenSKUs[rec.SKU] = rec
				}
			} else {
				if rec.Status == domain.StatusWithdrawn {
					withdrawnCount++
				}
				seenSKUs[rec.SKU] = rec
			}
		}
	}

	assert3Sigma := func(name string, count int, p float64) {
		mu := float64(N) * p
		sigma := math.Sqrt(float64(N) * p * (1.0 - p))
		lower := int(math.Floor(mu - 3*sigma))
		upper := int(math.Ceil(mu + 3*sigma))
		if count < lower || count > upper {
			t.Errorf("ratio calibration failed for %s: observed %d, expected [%d, %d] (mu=%.1f, 3sigma=%.1f)",
				name, count, lower, upper, mu, 3*sigma)
		}
	}

	assert3Sigma("DuplicateRatio", dupCount, cfg.DuplicateRatio)
	assert3Sigma("OutOfOrderRatio", oooCount, cfg.OutOfOrderRatio)
	assert3Sigma("WithdrawnRatio", withdrawnCount, cfg.WithdrawnRatio)
	assert3Sigma("InvalidRatio", invalidCount, cfg.InvalidRatio)
	assert3Sigma("UpdateRatio", updateCount, cfg.UpdateRatio)

	// Verify all 6 corruption modes were exercised
	if corruptEmptySKU == 0 || corruptNegPrice == 0 || corruptNaNPrice == 0 ||
		corruptInfPrice == 0 || corruptBadStatus == 0 || corruptZeroTime == 0 {
		t.Errorf("incomplete Invariant D corruption coverage: empty=%d, neg=%d, nan=%d, inf=%d, badStatus=%d, zeroTime=%d",
			corruptEmptySKU, corruptNegPrice, corruptNaNPrice, corruptInfPrice, corruptBadStatus, corruptZeroTime)
	}
}

func TestGenerator_StreamBatches_ContextCancellation(t *testing.T) {
	cfg := Config{
		Seed:              42,
		TotalBatches:      0, // infinite stream
		BatchSize:         10,
		ReservoirCapacity: 50,
	}

	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New generator failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := gen.StreamBatches(ctx, "job-cancel-test")

	// Read 3 batches
	for i := 0; i < 3; i++ {
		batch, ok := <-stream
		if !ok {
			t.Fatalf("stream closed prematurely at batch %d", i)
		}
		if batch.BatchIndex != i {
			t.Fatalf("expected batch index %d, got %d", i, batch.BatchIndex)
		}
	}

	cancel()

	// Ensure stream closes shortly
	done := make(chan struct{})
	go func() {
		for range stream {
		}
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatalf("stream did not close after context cancellation")
	}
}

func TestGenerator_ReadJSONBatches(t *testing.T) {
	// 1. Valid stream
	b1 := domain.Batch{JobID: "j1", BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "SKU-1", Price: 10, Status: domain.StatusActive, Timestamp: time.Now()}}}
	b2 := domain.Batch{JobID: "j1", BatchIndex: 1, Records: []domain.SupplierUpdate{{SKU: "SKU-2", Price: 20, Status: domain.StatusActive, Timestamp: time.Now()}}}

	d1, _ := json.Marshal(b1)
	d2, _ := json.Marshal(b2)
	payload := string(d1) + "\n\n" + string(d2) + "\n"

	ch, errCh := ReadJSONBatches(context.Background(), strings.NewReader(payload))
	var received []domain.Batch
	for b := range ch {
		received = append(received, b)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if len(received) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(received))
	}
	if received[0].BatchIndex != 0 || received[1].BatchIndex != 1 {
		t.Fatalf("unexpected batch indices: %v", received)
	}

	// 2. Corrupt JSON line
	corruptPayload := "{\"job_id\": \"j1\", invalid json}\n"
	chCorrupt, errChCorrupt := ReadJSONBatches(context.Background(), strings.NewReader(corruptPayload))
	for range chCorrupt {
	}
	err := <-errChCorrupt
	if err == nil || !strings.Contains(err.Error(), "failed to decode batch JSON") {
		t.Fatalf("expected JSON decode error, got %v", err)
	}

	// 3. Context cancellation mid-stream
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	chPipe, errChPipe := ReadJSONBatches(ctx, pr)

	// Write one line
	go func() {
		_, _ = pw.Write(append(d1, '\n'))
	}()
	<-chPipe
	cancel()
	_ = pw.Close()

	for range chPipe {
	}
	pipeErr := <-errChPipe
	if pipeErr != nil && !errors.Is(pipeErr, context.Canceled) {
		t.Fatalf("expected context.Canceled or nil, got %v", pipeErr)
	}
}

func TestGenerator_E2E_SyncEnginePipelineFeeding(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pipeline_e2e.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test storage: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	const (
		totalBatches = 50
		batchSize    = 100
		totalRecords = totalBatches * batchSize // 5,000 items
	)

	cfg := Config{
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

	gen, err := New(cfg)
	if err != nil {
		t.Fatalf("New generator failed: %v", err)
	}

	jobID := "pipeline-feed-job-001"
	stream := gen.StreamBatches(ctx, jobID)

	completedJob, err := engine.RunJob(ctx, jobID, stream)
	if err != nil {
		t.Fatalf("engine.RunJob failed: %v", err)
	}

	if completedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected job status COMPLETED, got %s", completedJob.Status)
	}
	if completedJob.CommittedBatchIndex != totalBatches-1 {
		t.Fatalf("expected committed batch index %d, got %d", totalBatches-1, completedJob.CommittedBatchIndex)
	}

	summary := completedJob.Summary
	// Accounting conservation invariant
	if summary.TotalProcessed() != totalRecords {
		t.Fatalf("accounting conservation violated: total processed %d != generated %d (+Added:%d, ~Updated:%d, -Withdrawn:%d, ↷Skipped:%d, ⚠DeadLetter:%d)",
			summary.TotalProcessed(), totalRecords, summary.Added, summary.Updated, summary.Withdrawn, summary.Skipped, summary.DeadLetter)
	}

	// Verify dead_letter_issues table row count matches summary.DeadLetter
	dlRecords, totalDL, err := store.ListDeadLetterRecords(ctx, jobID, "", 10000, 0)
	if err != nil {
		t.Fatalf("ListDeadLetterRecords failed: %v", err)
	}
	if totalDL != summary.DeadLetter || len(dlRecords) != summary.DeadLetter {
		t.Fatalf("dead_letter count mismatch: db %d vs summary %d", totalDL, summary.DeadLetter)
	}

	if summary.Added == 0 || summary.Updated == 0 || summary.Skipped == 0 || summary.DeadLetter == 0 {
		t.Fatalf("expected non-zero counters for Added, Updated, Skipped, DeadLetter: %+v", summary)
	}
}

func BenchmarkGenerator_StreamingMemory_Allocs(b *testing.B) {
	cfg := Config{
		Seed:              42,
		TotalBatches:      b.N,
		BatchSize:         100,
		ReservoirCapacity: 1000,
	}
	gen, err := New(cfg)
	if err != nil {
		b.Fatalf("New generator failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	ctx := context.Background()
	stream := gen.StreamBatches(ctx, "bench-job")
	for range stream {
	}
}

func TestReader_JSONConversions(t *testing.T) {
	now := time.Now()
	// Test MarshalRecordJSON with various prices
	recNormal := domain.SupplierUpdate{SKU: "SKU-N", Name: "Item", Price: 19.99, Status: domain.StatusActive, Timestamp: now}
	data, err := MarshalRecordJSON(recNormal)
	if err != nil {
		t.Fatalf("MarshalRecordJSON failed: %v", err)
	}
	if !strings.Contains(string(data), "19.99") {
		t.Fatalf("expected 19.99 in data: %s", string(data))
	}

	recNaN := domain.SupplierUpdate{SKU: "SKU-NAN", Name: "Item", Price: math.NaN(), Status: domain.StatusActive, Timestamp: now}
	dataNaN, err := MarshalRecordJSON(recNaN)
	if err != nil {
		t.Fatalf("MarshalRecordJSON NaN failed: %v", err)
	}
	if !strings.Contains(string(dataNaN), `"price":"NaN"`) {
		t.Fatalf("expected NaN in data: %s", string(dataNaN))
	}

	recInf := domain.SupplierUpdate{SKU: "SKU-INF", Name: "Item", Price: math.Inf(1), Status: domain.StatusActive, Timestamp: now}
	dataInf, err := MarshalRecordJSON(recInf)
	if err != nil {
		t.Fatalf("MarshalRecordJSON Inf failed: %v", err)
	}
	if !strings.Contains(string(dataInf), `"+Infinity"`) {
		t.Fatalf("expected +Infinity in data: %s", string(dataInf))
	}

	recNegInf := domain.SupplierUpdate{SKU: "SKU-NEGINF", Name: "Item", Price: math.Inf(-1), Status: domain.StatusActive, Timestamp: now}
	dataNegInf, err := MarshalRecordJSON(recNegInf)
	if err != nil {
		t.Fatalf("MarshalRecordJSON -Inf failed: %v", err)
	}
	if !strings.Contains(string(dataNegInf), `"-Infinity"`) {
		t.Fatalf("expected -Infinity in data: %s", string(dataNegInf))
	}

	// Test UnmarshalBatchJSON with various representations
	rawJSON := []byte(`{
		"job_id": "test-unmarshal",
		"batch_index": 0,
		"records": [
			{"sku": "SKU-1", "name": "N", "price": "+Infinity", "status": "ACTIVE", "timestamp": "2026-01-01T00:00:00Z"},
			{"sku": "SKU-2", "name": "N", "price": "-Infinity", "status": "ACTIVE", "timestamp": "2026-01-01T00:00:00Z"},
			{"sku": "SKU-3", "name": "N", "price": "NaN", "status": "ACTIVE", "timestamp": "2026-01-01T00:00:00Z"},
			{"sku": "SKU-4", "name": "N", "price": null, "status": "ACTIVE", "timestamp": "2026-01-01T00:00:00Z"},
			{"sku": "SKU-5", "name": "N", "price": "invalid_string", "status": "ACTIVE", "timestamp": "2026-01-01T00:00:00Z"},
			{"sku": "SKU-6", "name": "N", "price": 42.50, "status": "ACTIVE", "timestamp": "2026-01-01T00:00:00Z"}
		]
	}`)
	batch, err := UnmarshalBatchJSON(rawJSON)
	if err != nil {
		t.Fatalf("UnmarshalBatchJSON failed: %v", err)
	}
	if len(batch.Records) != 6 {
		t.Fatalf("expected 6 records, got %d", len(batch.Records))
	}
	if !math.IsInf(batch.Records[0].Price, 1) {
		t.Errorf("record 0 expected +Inf, got %f", batch.Records[0].Price)
	}
	if !math.IsInf(batch.Records[1].Price, -1) {
		t.Errorf("record 1 expected -Inf, got %f", batch.Records[1].Price)
	}
	if !math.IsNaN(batch.Records[2].Price) {
		t.Errorf("record 2 expected NaN, got %f", batch.Records[2].Price)
	}
	if !math.IsNaN(batch.Records[3].Price) {
		t.Errorf("record 3 expected NaN from null, got %f", batch.Records[3].Price)
	}
	if !math.IsNaN(batch.Records[4].Price) {
		t.Errorf("record 4 expected NaN from invalid string, got %f", batch.Records[4].Price)
	}
	if batch.Records[5].Price != 42.50 {
		t.Errorf("record 5 expected 42.50, got %f", batch.Records[5].Price)
	}
}

func TestGenerator_DeriveSeeds_And_NewDefaults(t *testing.T) {
	s1, s2 := deriveSeeds(42, 99)
	if s1 != 42 || s2 != 99 {
		t.Fatalf("expected (42, 99), got (%d, %d)", s1, s2)
	}

	// Empty BaseTime defaults to 2026-01-01
	gen, err := New(Config{
		BatchSize:         10,
		ReservoirCapacity: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gImpl, ok := gen.(*generator)
	if !ok {
		t.Fatalf("expected *generator concrete type")
	}
	if gImpl.currentTime.Year() != 2026 {
		t.Fatalf("expected year 2026, got %d", gImpl.currentTime.Year())
	}

	// Invalid config
	_, err = New(Config{BatchSize: -1})
	if err == nil {
		t.Fatalf("expected error on invalid config, got nil")
	}
}
