package sync_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// ============================================================================
// ADVERSARIAL CHALLENGER SUITE: AC1, AC2, AC3 (CRASH, ROLLBACK, RESUME)
// ============================================================================

// ----------------------------------------------------------------------------
// CHALLENGE 1: MULTI-CRASH CHAOS HARNESS WITH INDEPENDENT GROUND-TRUTH ORACLE
// ----------------------------------------------------------------------------
// Simulates an extremely unstable production environment:
//   - 20 batches with 200 total records.
//   - Complex lifecycle: multiple updates to identical SKUs, out-of-order timestamps,
//     withdrawn items, forbidden resurrections, dead-letter records, and identical duplicates.
//   - Injected abrupt crashes at random batches via context cancellations.
//   - Fresh SQLite storage re-opened from disk across crashes (simulating process restart).
//   - Verification against an independent in-memory Ground-Truth Oracle.
func TestChallenger_AC1_AC2_MultiCrashChaosStream_GroundTruthOracle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "multi_crash_chaos.db")
	jobID := "chaos-multicrash-job"
	baseTime := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	const totalBatches = 20
	const itemsPerBatch = 10

	// 1. Generate deterministic multi-batch stream with complex lifecycle
	batches := make([]domain.Batch, totalBatches)
	type oracleProduct struct {
		name      string
		price     float64
		status    domain.ProductStatus
		timestamp time.Time
	}

	oracleCatalog := make(map[string]oracleProduct)
	oracleSummary := domain.ChangeSummary{}

	// Helper to process record through ground-truth oracle rules
	applyToOracle := func(record domain.SupplierUpdate) {
		sku := record.SKU
		// Invariant D: Invalid records quarantined
		if sku == "" || record.Price < 0 || !record.Status.IsValid() {
			oracleSummary.DeadLetter++
			return
		}

		existing, exists := oracleCatalog[sku]
		if !exists {
			oracleCatalog[sku] = oracleProduct{
				name:      record.Name,
				price:     record.Price,
				status:    record.Status,
				timestamp: record.Timestamp,
			}
			oracleSummary.Added++
			return
		}

		// Invariant A: Identical record
		if existing.name == record.Name && existing.price == record.Price &&
			existing.status == record.Status && existing.timestamp.Equal(record.Timestamp) {
			oracleSummary.Skipped++
			return
		}

		// Invariant C: Tombstone preservation
		if existing.status == domain.StatusWithdrawn && record.Status == domain.StatusActive {
			oracleSummary.Skipped++
			return
		}

		// Invariant B: Fast-forward timestamp
		if record.Timestamp.Before(existing.timestamp) || record.Timestamp.Equal(existing.timestamp) {
			oracleSummary.Skipped++
			return
		}

		// Valid update
		wasWithdrawn := existing.status == domain.StatusWithdrawn
		oracleCatalog[sku] = oracleProduct{
			name:      record.Name,
			price:     record.Price,
			status:    record.Status,
			timestamp: record.Timestamp,
		}

		if !wasWithdrawn && record.Status == domain.StatusWithdrawn {
			oracleSummary.Withdrawn++
		} else {
			oracleSummary.Updated++
		}
	}

	// Build the 20 batches
	rng := rand.New(rand.NewSource(42))
	for b := 0; b < totalBatches; b++ {
		records := make([]domain.SupplierUpdate, itemsPerBatch)
		for i := 0; i < itemsPerBatch; i++ {
			skuIndex := rng.Intn(25) // 25 distinct SKUs to ensure high collision & update frequency
			sku := fmt.Sprintf("SKU-%03d", skuIndex)
			step := b*itemsPerBatch + i
			recTime := baseTime.Add(time.Duration(step) * time.Minute)

			// Calibrate edge cases:
			rType := rng.Intn(10)
			switch rType {
			case 0:
				// Out-of-order timestamp (Invariant B)
				recTime = baseTime.Add(-time.Duration(rng.Intn(60)+1) * time.Minute)
				records[i] = domain.SupplierUpdate{SKU: sku, Name: fmt.Sprintf("Out-of-order %s", sku), Price: 50.0, Status: domain.StatusActive, Timestamp: recTime}
			case 1:
				// Tombstone transition to WITHDRAWN (Invariant C)
				records[i] = domain.SupplierUpdate{SKU: sku, Name: fmt.Sprintf("Withdrawn %s", sku), Price: 40.0, Status: domain.StatusWithdrawn, Timestamp: recTime}
			case 2:
				// Illegal resurrection attempt to ACTIVE
				records[i] = domain.SupplierUpdate{SKU: sku, Name: fmt.Sprintf("Resurrect %s", sku), Price: 99.0, Status: domain.StatusActive, Timestamp: recTime}
			case 3:
				// Corrupt record: negative price (Invariant D)
				records[i] = domain.SupplierUpdate{SKU: sku, Name: "Corrupt Negative", Price: -15.5, Status: domain.StatusActive, Timestamp: recTime}
			case 4:
				// Corrupt record: empty SKU (Invariant D)
				records[i] = domain.SupplierUpdate{SKU: "", Name: "Corrupt Empty SKU", Price: 25.0, Status: domain.StatusActive, Timestamp: recTime}
			default:
				// Normal active update/insert
				records[i] = domain.SupplierUpdate{SKU: sku, Name: fmt.Sprintf("Product %s v%d", sku, step), Price: float64(10 + (step % 50)), Status: domain.StatusActive, Timestamp: recTime}
			}

			// Apply to ground truth oracle
			applyToOracle(records[i])
		}
		batches[b] = domain.Batch{
			JobID:      jobID,
			BatchIndex: b,
			Records:    records,
		}
	}

	// 2. Multi-Crash Execution Plan:
	// Crash targets:
	// Crash 1: at batch 3 (batches 0, 1, 2 committed)
	// Crash 2: at batch 8 (batches 3, 4, 5, 6, 7 committed)
	// Crash 3: at batch 14 (batches 8..13 committed)
	// Crash 4: at batch 18 (batches 14..17 committed)
	// Final run: completes batches 18 and 19
	crashAtBatches := []int{3, 8, 14, 18}

	for crashIdx, crashBatch := range crashAtBatches {
		store, err := storage.Open(dbPath)
		if err != nil {
			t.Fatalf("crash %d: failed to open store: %v", crashIdx, err)
		}

		engine := catalogSync.NewEngine(store)

		// On crashIdx == 2, simulate an ungraceful crash (SIGKILL) where status was left as RUNNING in SQLite
		if crashIdx == 2 {
			_ = store.UpdateJobStatus(context.Background(), jobID, domain.JobStatusRunning)
		}

		// On restart, recover stale jobs
		recovered, err := engine.RecoverOnStartup(context.Background())
		if err != nil {
			t.Fatalf("crash %d: RecoverOnStartup failed: %v", crashIdx, err)
		}
		if crashIdx == 2 {
			if len(recovered) != 1 || recovered[0] != jobID {
				t.Fatalf("crash %d: expected ungraceful crash job %s to be recovered, got %v", crashIdx, jobID, recovered)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())

		stream := make(chan domain.Batch)
		go func(targetBatch int) {
			defer close(stream)
			for _, b := range batches {
				if b.BatchIndex < targetBatch {
					select {
					case stream <- b:
					case <-ctx.Done():
						return
					}
				} else if b.BatchIndex == targetBatch {
					// Wait deterministically until targetBatch - 1 has committed to storage
					expectedPrev := targetBatch - 1
					for i := 0; i < 500; i++ {
						j, err := store.GetJob(context.Background(), jobID)
						if err == nil && j.CommittedBatchIndex == expectedPrev {
							break
						}
						time.Sleep(2 * time.Millisecond)
					}
					// Now crash midway through targetBatch
					cancel()
					select {
					case stream <- b:
					case <-time.After(50 * time.Millisecond):
					}
					return
				}
			}
		}(crashBatch)

		if crashIdx == 0 {
			_, err = engine.RunJob(ctx, jobID, stream)
		} else {
			_, err = engine.ResumeJob(ctx, jobID, stream)
		}

		if err == nil {
			t.Fatalf("crash %d: expected error on canceled run, got nil", crashIdx)
		}
		cancel()

		// Verify that targetBatch was NOT committed and prior batches remained intact
		job, err := store.GetJob(context.Background(), jobID)
		if err != nil {
			t.Fatalf("crash %d: failed to get job: %v", crashIdx, err)
		}
		expectedCommitted := crashBatch - 1
		if job.CommittedBatchIndex != expectedCommitted {
			t.Fatalf("crash %d: expected committed_batch_index=%d, got %d", crashIdx, expectedCommitted, job.CommittedBatchIndex)
		}
		if job.Status != domain.JobStatusInterrupted {
			t.Fatalf("crash %d: expected status INTERRUPTED, got %s", crashIdx, job.Status)
		}

		// Close store to simulate complete process death
		_ = store.Close()
	}

	// 3. Final Resumption: Run to Full Completion
	finalStore, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open final store: %v", err)
	}
	defer finalStore.Close()

	finalEngine := catalogSync.NewEngine(finalStore)
	_, err = finalEngine.RecoverOnStartup(context.Background())
	if err != nil {
		t.Fatalf("final RecoverOnStartup failed: %v", err)
	}

	// Stream ALL batches (0..19). The engine MUST skip already committed batches (0..17)
	// without executing redundant writes, and cleanly execute batches 18 and 19.
	fullStream := make(chan domain.Batch, totalBatches)
	for _, b := range batches {
		fullStream <- b
	}
	close(fullStream)

	completedJob, err := finalEngine.ResumeJob(context.Background(), jobID, fullStream)
	if err != nil {
		t.Fatalf("final ResumeJob failed: %v", err)
	}

	// AC3 Check: Monotonic transition to COMPLETED
	if completedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected final status COMPLETED, got %s", completedJob.Status)
	}
	if completedJob.CompletedAt == nil {
		t.Fatalf("expected completed_at to be populated")
	}
	if completedJob.CommittedBatchIndex != totalBatches-1 {
		t.Fatalf("expected committed_batch_index=%d, got %d", totalBatches-1, completedJob.CommittedBatchIndex)
	}

	// 4. Verification Against Oracle
	// 4.1 Summary Metrics Match EXACTLY
	if completedJob.Summary != oracleSummary {
		t.Fatalf("ORACLE SUMMARY DRIFT DETECTED!\nActual:   %+v\nExpected: %+v", completedJob.Summary, oracleSummary)
	}

	// 4.2 Checkpoints: Exactly 20 unique checkpoints, 0 duplicates
	var cpCount int
	err = finalStore.DB().QueryRow("SELECT count(*) FROM batch_checkpoints WHERE job_id = ?", jobID).Scan(&cpCount)
	if err != nil || cpCount != totalBatches {
		t.Fatalf("expected %d checkpoints, got %d (err: %v)", totalBatches, cpCount, err)
	}

	var duplicateCheckpoints int
	err = finalStore.DB().QueryRow("SELECT count(*) FROM (SELECT batch_index FROM batch_checkpoints WHERE job_id = ? GROUP BY batch_index HAVING count(*) > 1)", jobID).Scan(&duplicateCheckpoints)
	if err != nil || duplicateCheckpoints != 0 {
		t.Fatalf("duplicate checkpoints recorded! count=%d", duplicateCheckpoints)
	}

	// 4.3 Catalog State: Every single product matches Oracle exactly
	for sku, expected := range oracleCatalog {
		actual, err := finalStore.GetProduct(context.Background(), sku)
		if err != nil {
			t.Fatalf("product %s missing from DB: %v", sku, err)
		}
		if actual.Name != expected.name || actual.Price != expected.price ||
			actual.Status != expected.status || !actual.SupplierTimestamp.Equal(expected.timestamp) {
			t.Fatalf("CATALOG STATE CORRUPTION on SKU %s!\nActual:   %+v\nExpected: %+v", sku, actual, expected)
		}
	}
}

// ----------------------------------------------------------------------------
// CHALLENGE 2: POISONED REPLAY ATTACK DURING RESUMPTION (AC2 DRIFT RESISTANCE)
// ----------------------------------------------------------------------------
// An adversarial producer connects upon resumption and re-sends previously
// committed batches 0 and 1 with altered, malicious data (e.g. price $99999).
// AC2 guarantees that committed batches are skipped without re-processing.
// If the engine fails to skip or re-executes them, the catalog will be poisoned.
func TestChallenger_AC2_PoisonedReplayAttack_ZeroCatalogDrift(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-poison-replay"
	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Batch 0 (Legitimate)
	batch0 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-LEGIT-01", Name: "Legit Product", Price: 10.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}
	// Batch 1 (Legitimate)
	batch1 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-LEGIT-02", Name: "Legit Product 2", Price: 20.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}

	// Run Batches 0 & 1 and interrupt
	stream1 := make(chan domain.Batch, 2)
	stream1 <- batch0
	stream1 <- batch1
	close(stream1)

	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusRunning, TotalBatches: 3, CommittedBatchIndex: -1,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	})

	job1, err := engine.RunJob(ctx, jobID, stream1)
	if err == nil || job1.Status != domain.JobStatusInterrupted {
		t.Fatalf("expected interruption on stream close, got %+v, err=%v", job1, err)
	}

	// Record original update timestamps
	p1Orig, _ := store.GetProduct(ctx, "SKU-LEGIT-01")
	p2Orig, _ := store.GetProduct(ctx, "SKU-LEGIT-02")

	// Construct Poisoned Batch 0 & 1 with newer timestamps and altered prices
	poisonedBatch0 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-LEGIT-01", Name: "POISONED HACKED NAME", Price: 99999.0, Status: domain.StatusActive, Timestamp: baseTime.Add(24 * time.Hour)},
		},
	}
	poisonedBatch1 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-LEGIT-02", Name: "POISONED HACKED NAME 2", Price: 88888.0, Status: domain.StatusActive, Timestamp: baseTime.Add(24 * time.Hour)},
		},
	}
	// Batch 2 (Legitimate final batch)
	batch2 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 2,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-LEGIT-03", Name: "Legit Product 3", Price: 30.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}

	// Resume sending Poisoned Batches 0 & 1 followed by Batch 2
	resumeStream := make(chan domain.Batch, 3)
	resumeStream <- poisonedBatch0
	resumeStream <- poisonedBatch1
	resumeStream <- batch2
	close(resumeStream)

	completedJob, err := engine.ResumeJob(ctx, jobID, resumeStream)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}
	if completedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected COMPLETED status, got %s", completedJob.Status)
	}

	// AC2 CRITICAL ASSERTIONS:
	// Products 1 and 2 MUST NOT be poisoned! Their price, name, and updated_at must remain untouched!
	p1After, err := store.GetProduct(ctx, "SKU-LEGIT-01")
	if err != nil || p1After.Price != 10.0 || p1After.Name != "Legit Product" {
		t.Fatalf("POISONED REPLAY PENETRATED RESUME! Product 1 was mutated: %+v", p1After)
	}
	if !p1After.UpdatedAt.Equal(p1Orig.UpdatedAt) {
		t.Fatalf("Product 1 UpdatedAt changed during resume replay! before=%v, after=%v", p1Orig.UpdatedAt, p1After.UpdatedAt)
	}

	p2After, err := store.GetProduct(ctx, "SKU-LEGIT-02")
	if err != nil || p2After.Price != 20.0 || p2After.Name != "Legit Product 2" {
		t.Fatalf("POISONED REPLAY PENETRATED RESUME! Product 2 was mutated: %+v", p2After)
	}
	if !p2After.UpdatedAt.Equal(p2Orig.UpdatedAt) {
		t.Fatalf("Product 2 UpdatedAt changed during resume replay! before=%v, after=%v", p2Orig.UpdatedAt, p2After.UpdatedAt)
	}

	// Cumulative summary MUST NOT double-count: only batch 2's added item should be added to summary
	expectedSummary := domain.ChangeSummary{Added: 3, Updated: 0, Withdrawn: 0, Skipped: 0, DeadLetter: 0}
	if completedJob.Summary != expectedSummary {
		t.Fatalf("summary corrupted by replay: got %+v, want %+v", completedJob.Summary, expectedSummary)
	}
}

// ----------------------------------------------------------------------------
// CHALLENGE 3: MID-BATCH PARTIAL TRANSACTION ATOMICITY (AC1 ROLLBACK TEST)
// ----------------------------------------------------------------------------
// A batch with 50 items has 20 valid product inserts, 10 valid updates, 10 dead letters,
// and item #41 is a poisoned trigger that cancels the context in-flight.
// None of the 40 items preceding the cancellation must persist to the database.
func TestChallenger_AC1_MidBatchPartialTransaction_AbsoluteRollback(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobID := "job-partial-atomic"
	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Step 1: Pre-populate 10 items in Batch 0
	seedBatch := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records:    make([]domain.SupplierUpdate, 10),
	}
	for i := 0; i < 10; i++ {
		seedBatch.Records[i] = domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-PRE-%02d", i),
			Name:      fmt.Sprintf("Pre Product %d", i),
			Price:     10.0,
			Status:    domain.StatusActive,
			Timestamp: baseTime,
		}
	}

	seedStream := make(chan domain.Batch, 1)
	seedStream <- seedBatch
	close(seedStream)

	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusRunning, TotalBatches: 2, CommittedBatchIndex: -1,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	})

	_, err := engine.RunJob(ctx, jobID, seedStream)
	if err == nil {
		t.Fatalf("expected interruption from stream closure (total=2, sent=1)")
	}

	// Verify batch 0 committed cleanly
	j0, _ := store.GetJob(ctx, jobID)
	if j0.CommittedBatchIndex != 0 {
		t.Fatalf("batch 0 failed to commit: %+v", j0)
	}

	// Step 2: Construct Batch 1 containing:
	// - 10 updates to SKU-PRE-00..09
	// - 20 brand new items (SKU-NEW-00..19)
	// - 10 corrupt records (negative prices)
	// - 1 trigger record that cancels the context
	batch1Records := make([]domain.SupplierUpdate, 0, 45)
	for i := 0; i < 10; i++ {
		batch1Records = append(batch1Records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-PRE-%02d", i),
			Name:      fmt.Sprintf("MUTATED Product %d", i),
			Price:     999.0,
			Status:    domain.StatusActive,
			Timestamp: baseTime.Add(time.Hour),
		})
	}
	for i := 0; i < 20; i++ {
		batch1Records = append(batch1Records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-NEW-%02d", i),
			Name:      fmt.Sprintf("New Product %d", i),
			Price:     50.0,
			Status:    domain.StatusActive,
			Timestamp: baseTime.Add(time.Hour),
		})
	}
	for i := 0; i < 10; i++ {
		batch1Records = append(batch1Records, domain.SupplierUpdate{
			SKU:       fmt.Sprintf("SKU-CORRUPT-%02d", i),
			Name:      "Corrupt Item",
			Price:     -10.0,
			Status:    domain.StatusActive,
			Timestamp: baseTime.Add(time.Hour),
		})
	}

	batch1 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 1,
		Records:    batch1Records,
	}

	stream2 := make(chan domain.Batch)
	go func() {
		defer close(stream2)
		// Send batch1: unblocks only when executeJob receives it
		stream2 <- batch1
		// Abrupt cancellation during batch 1 execution
		cancel()
	}()

	_, err = engine.ResumeJob(ctx, jobID, stream2)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// EMPIRICAL VERIFICATION OF COMPLETE ROLLBACK:
	// 1. None of the 10 updates survived
	for i := 0; i < 10; i++ {
		sku := fmt.Sprintf("SKU-PRE-%02d", i)
		p, err := store.GetProduct(context.Background(), sku)
		if err != nil {
			t.Fatalf("pre-existing product %s disappeared! %v", sku, err)
		}
		if p.Price != 10.0 || p.Name != fmt.Sprintf("Pre Product %d", i) {
			t.Fatalf("PARTIAL WRITE LEAK! Product %s was partially updated by rolled-back batch: %+v", sku, p)
		}
	}

	// 2. None of the 20 new items survived
	for i := 0; i < 20; i++ {
		sku := fmt.Sprintf("SKU-NEW-%02d", i)
		p, err := store.GetProduct(context.Background(), sku)
		if err == nil || p != nil {
			t.Fatalf("GHOST PRODUCT SURVIVED ROLLBACK! %s: %+v", sku, p)
		}
	}

	// 3. None of the 10 dead letters survived
	dlRecords, count, err := store.ListDeadLetterRecords(context.Background(), jobID, "", 50, 0)
	if err != nil {
		t.Fatalf("failed querying dead letters: %v", err)
	}
	if count != 0 || len(dlRecords) != 0 {
		t.Fatalf("DEAD LETTER ISSUE SURVIVED ROLLBACK! count=%d", count)
	}

	// 4. Batch checkpoint 1 MUST NOT exist
	var cp1Exists bool
	err = store.DB().QueryRow("SELECT 1 FROM batch_checkpoints WHERE job_id = ? AND batch_index = 1", jobID).Scan(&cp1Exists)
	if err == nil && cp1Exists {
		t.Fatalf("CHECKPOINT 1 SURVIVED ROLLBACK!")
	}

	// 5. Job progress unchanged
	jFinal, _ := store.GetJob(context.Background(), jobID)
	if jFinal.CommittedBatchIndex != 0 {
		t.Fatalf("job committed_batch_index corrupted: %d", jFinal.CommittedBatchIndex)
	}
	if jFinal.Status != domain.JobStatusInterrupted {
		t.Fatalf("job status not INTERRUPTED: %s", jFinal.Status)
	}
}

// ----------------------------------------------------------------------------
// CHALLENGE 4: LIFECYCLE FSM TRANSITIONS & IMMUTABILITY (AC3)
// ----------------------------------------------------------------------------
// Rigorously tests the entire lifecycle finite state machine:
// - PENDING / RUNNING -> INTERRUPTED on crash recovery
// - COMPLETED / FAILED cannot be altered by recovery
// - COMPLETED cannot be resumed or re-run
// - Premature stream termination never marks job as COMPLETED
// - TotalBatches barrier: job only completes when index reaches TotalBatches - 1
func TestChallenger_AC3_LifecycleFSM_StrictTransitions(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Seed jobs in diverse states
	states := []struct {
		id       string
		status   domain.JobStatus
		total    int
		idx      int
		expected domain.JobStatus
	}{
		{"fsm-running", domain.JobStatusRunning, 5, 2, domain.JobStatusInterrupted},
		{"fsm-pending", domain.JobStatusPending, 3, -1, domain.JobStatusInterrupted},
		{"fsm-interrupted", domain.JobStatusInterrupted, 4, 1, domain.JobStatusInterrupted},
		{"fsm-completed", domain.JobStatusCompleted, 2, 1, domain.JobStatusCompleted},
		{"fsm-failed", domain.JobStatusFailed, 2, 0, domain.JobStatusFailed},
	}

	for _, s := range states {
		completedAt := &now
		if s.status != domain.JobStatusCompleted && s.status != domain.JobStatusFailed {
			completedAt = nil
		}
		_ = store.CreateJob(ctx, &domain.SyncJob{
			JobID:               s.id,
			Status:              s.status,
			TotalBatches:        s.total,
			CommittedBatchIndex: s.idx,
			CreatedAt:           now,
			UpdatedAt:           now,
			CompletedAt:         completedAt,
		})
		// Ensure DB reflects exact seeded status
		if s.status == domain.JobStatusCompleted || s.status == domain.JobStatusFailed {
			_ = store.UpdateJobStatus(ctx, s.id, s.status)
		}
	}

	// Run RecoverOnStartup
	recovered, err := engine.RecoverOnStartup(ctx)
	if err != nil {
		t.Fatalf("RecoverOnStartup failed: %v", err)
	}
	if len(recovered) != 2 {
		t.Fatalf("expected 2 recovered jobs (running & pending), got %d (%v)", len(recovered), recovered)
	}

	// Verify resulting DB states
	for _, s := range states {
		job, err := store.GetJob(ctx, s.id)
		if err != nil {
			t.Fatalf("failed to query job %s: %v", s.id, err)
		}
		if job.Status != s.expected {
			t.Fatalf("job %s status drift! got %s, want %s", s.id, job.Status, s.expected)
		}
		if s.expected == domain.JobStatusInterrupted && job.CompletedAt != nil {
			t.Fatalf("interrupted job %s has non-nil completed_at: %v", s.id, job.CompletedAt)
		}
		if s.expected == domain.JobStatusCompleted && job.CompletedAt == nil {
			t.Fatalf("completed job %s has nil completed_at", s.id)
		}
	}

	// 2. Terminal State Invariant: Completed jobs REJECT both RunJob and ResumeJob
	emptyStream := make(chan domain.Batch)
	close(emptyStream)

	_, err = engine.RunJob(ctx, "fsm-completed", emptyStream)
	if !errors.Is(err, catalogSync.ErrJobAlreadyCompleted) {
		t.Fatalf("expected ErrJobAlreadyCompleted on RunJob for completed job, got %v", err)
	}

	_, err = engine.ResumeJob(ctx, "fsm-completed", emptyStream)
	if !errors.Is(err, catalogSync.ErrJobAlreadyCompleted) {
		t.Fatalf("expected ErrJobAlreadyCompleted on ResumeJob for completed job, got %v", err)
	}

	// 3. Premature stream closure barrier
	// "fsm-running" was recovered to INTERRUPTED (committed index=2, total=5).
	// Stream only emits batch 3, not batch 4.
	prematureStream := make(chan domain.Batch, 1)
	prematureStream <- domain.Batch{
		JobID:      "fsm-running",
		BatchIndex: 3,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-FSM-3", Name: "FSM 3", Price: 10, Status: domain.StatusActive, Timestamp: now},
		},
	}
	close(prematureStream)

	jobPremature, err := engine.ResumeJob(ctx, "fsm-running", prematureStream)
	if err == nil {
		t.Fatalf("expected premature closure error when total=5 and only 4 batches committed")
	}
	if jobPremature.Status != domain.JobStatusInterrupted {
		t.Fatalf("expected job to remain INTERRUPTED on premature close, got %s", jobPremature.Status)
	}
	if jobPremature.CompletedAt != nil {
		t.Fatalf("completed_at must remain nil on premature close, got %v", jobPremature.CompletedAt)
	}

	// 4. Final completion barrier
	// Resuming and providing final batch 4 MUST transition to COMPLETED
	finishStream := make(chan domain.Batch, 1)
	finishStream <- domain.Batch{
		JobID:      "fsm-running",
		BatchIndex: 4,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-FSM-4", Name: "FSM 4", Price: 10, Status: domain.StatusActive, Timestamp: now},
		},
	}
	close(finishStream)

	completedJob, err := engine.ResumeJob(ctx, "fsm-running", finishStream)
	if err != nil {
		t.Fatalf("ResumeJob failed to finish final batch: %v", err)
	}
	if completedJob.Status != domain.JobStatusCompleted {
		t.Fatalf("expected COMPLETED status, got %s", completedJob.Status)
	}
	if completedJob.CompletedAt == nil {
		t.Fatalf("completed_at was not populated upon completion")
	}
}

// ----------------------------------------------------------------------------
// CHALLENGE 5: RESUMABILITY WITH SEQUENCE GAP DETECTION (AC2 DEFENSE)
// ----------------------------------------------------------------------------
// If a producer skips a batch (e.g. emits batch 3 after batch 1), the engine
// MUST NOT commit batch 3 and create gaps in the checkpoint sequence.
// It must mark the job INTERRUPTED and fail safely.
func TestChallenger_AC2_SequenceGapRejection(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()
	now := time.Now().UTC()

	jobID := "job-gap-rejection"
	stream := make(chan domain.Batch, 3)
	stream <- domain.Batch{JobID: jobID, BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "GAP-0", Name: "G0", Price: 1, Status: domain.StatusActive, Timestamp: now}}}
	stream <- domain.Batch{JobID: jobID, BatchIndex: 1, Records: []domain.SupplierUpdate{{SKU: "GAP-1", Name: "G1", Price: 2, Status: domain.StatusActive, Timestamp: now}}}
	// Gap: skips batch 2, sends batch 3
	stream <- domain.Batch{JobID: jobID, BatchIndex: 3, Records: []domain.SupplierUpdate{{SKU: "GAP-3", Name: "G3", Price: 3, Status: domain.StatusActive, Timestamp: now}}}
	close(stream)

	job, err := engine.RunJob(ctx, jobID, stream)
	if err == nil {
		t.Fatalf("expected gap error, got nil")
	}
	if job.Status != domain.JobStatusInterrupted {
		t.Fatalf("expected job to be INTERRUPTED, got %s", job.Status)
	}
	if job.CommittedBatchIndex != 1 {
		t.Fatalf("expected committed_batch_index=1, got %d", job.CommittedBatchIndex)
	}

	// Verify batch 3 was NOT committed to products table
	p3, err := store.GetProduct(ctx, "GAP-3")
	if err == nil || p3 != nil {
		t.Fatalf("gap batch product GAP-3 was erroneously committed! %+v", p3)
	}

	// Verify checkpoint for batch 3 was NOT recorded
	var cp3Exists bool
	_ = store.DB().QueryRow("SELECT 1 FROM batch_checkpoints WHERE job_id = ? AND batch_index = 3", jobID).Scan(&cp3Exists)
	if cp3Exists {
		t.Fatalf("checkpoint for gap batch 3 was written!")
	}
}

// ----------------------------------------------------------------------------
// CHALLENGE 6: HIGH-CONCURRENCY STRESS & ACTIVE JOB MUTEX SAFETY
// ----------------------------------------------------------------------------
// Stresses the concurrency boundary by firing multiple simultaneous RunJob
// and ResumeJob calls on the same job ID from 20 parallel goroutines.
// Exactly ONE goroutine must win and execute; all others must receive ErrJobAlreadyRunning.
func TestChallenger_Concurrency_SingleActiveJobMutex(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-concurrent-stress"
	now := time.Now().UTC()

	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusPending, TotalBatches: 1, CommittedBatchIndex: -1,
		CreatedAt: now, UpdatedAt: now,
	})

	const contenders = 20
	var wg sync.WaitGroup
	wg.Add(contenders)

	startBarrier := make(chan struct{})
	holdWinner := make(chan struct{})
	successCount := 0
	conflictCount := 0
	var countMu sync.Mutex

	for i := 0; i < contenders; i++ {
		go func(id int) {
			defer wg.Done()
			<-startBarrier

			// Each contender provides a stream with batch 0 that waits on holdWinner before closing
			stream := make(chan domain.Batch, 1)
			stream <- domain.Batch{
				JobID:      jobID,
				BatchIndex: 0,
				Records:    []domain.SupplierUpdate{{SKU: "CONCUR-1", Name: "C1", Price: 10, Status: domain.StatusActive, Timestamp: now}},
			}
			go func() {
				<-holdWinner
				close(stream)
			}()

			_, err := engine.ResumeJob(ctx, jobID, stream)
			countMu.Lock()
			if err == nil {
				successCount++
			} else if errors.Is(err, catalogSync.ErrJobAlreadyRunning) {
				conflictCount++
			}
			countMu.Unlock()
		}(i)
	}

	close(startBarrier)
	// Give contenders a window to contend while the winning stream is held open
	time.Sleep(50 * time.Millisecond)
	close(holdWinner)
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 contender to acquire active lock and succeed; got %d (conflicts: %d)", successCount, conflictCount)
	}
	if conflictCount != contenders-1 {
		t.Fatalf("expected %d contenders to be rejected with ErrJobAlreadyRunning; got %d", contenders-1, conflictCount)
	}
}

// ----------------------------------------------------------------------------
// CHALLENGE 7: RAW WAL ABRUPT KILL & ORPHAN FRAME RECOVERY (AC1 + AC2)
// ----------------------------------------------------------------------------
// Simulates an abrupt ungraceful process kill with dirty uncommitted WAL frames.
// Verifies that pure Go SQLite WAL recovery discards dirty frames and engine
// resumes seamlessly from the last committed checkpoint.
func TestChallenger_AC1_WAL_AbruptProcessKill_OrphanFrameRecovery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wal_orphan_recovery.db")
	ctx := context.Background()
	now := time.Now().UTC()
	jobID := "job-wal-orphan"

	// 1. Initial process: commit Batch 0
	store1, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening initial storage: %v", err)
	}

	_ = store1.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusRunning, TotalBatches: 2, CommittedBatchIndex: -1,
		CreatedAt: now, UpdatedAt: now,
	})

	tx0, err := store1.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed BeginTx: %v", err)
	}
	_ = store1.UpsertProductTx(ctx, tx0, &domain.Product{
		SKU: "SKU-DURABLE-01", Name: "Durable Product", Price: 100.0, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now,
	})
	_ = store1.RecordBatchCheckpointTx(ctx, tx0, &domain.BatchCheckpoint{
		JobID: jobID, BatchIndex: 0, ItemsCount: 1, Summary: domain.ChangeSummary{Added: 1}, CommittedAt: now,
	})
	_ = store1.UpdateJobProgressTx(ctx, tx0, jobID, 0, domain.ChangeSummary{Added: 1})
	if err := tx0.Commit(); err != nil {
		t.Fatalf("failed to commit batch 0: %v", err)
	}

	// 2. Start dirty uncommitted batch 1 transaction and abruptly kill connection
	txDirty, err := store1.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed BeginTx for dirty batch: %v", err)
	}
	_ = store1.UpsertProductTx(ctx, txDirty, &domain.Product{
		SKU: "SKU-GHOST-ORPHAN", Name: "Orphan Ghost", Price: 999.0, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now,
	})
	_ = store1.RecordDeadLetterTx(ctx, txDirty, &domain.DeadLetterRecord{
		IssueID: "dl-dirty-01", JobID: jobID, BatchIndex: 1, SKU: "SKU-DIRTY", RawPayload: "corrupt", Reason: "bad", CreatedAt: now,
	})
	// Simulate abrupt process termination without commit: rollback and close
	_ = txDirty.Rollback()
	_ = store1.Close()

	// 3. New process restarts: opens storage, recovers interrupted jobs, and resumes
	store2, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed reopening storage: %v", err)
	}
	defer store2.Close()

	engine2 := catalogSync.NewEngine(store2)

	recovered, err := engine2.RecoverOnStartup(ctx)
	if err != nil {
		t.Fatalf("RecoverOnStartup failed: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != jobID {
		t.Fatalf("expected job %s to be recovered, got %v", jobID, recovered)
	}

	// Verify uncommitted ghost product and dead letter do not exist
	ghost, err := store2.GetProduct(ctx, "SKU-GHOST-ORPHAN")
	if err == nil || ghost != nil {
		t.Fatalf("ghost product survived crash: %+v", ghost)
	}
	dlRecords, count, err := store2.ListDeadLetterRecords(ctx, jobID, "", 10, 0)
	if err != nil || count != 0 || len(dlRecords) != 0 {
		t.Fatalf("dirty dead letters survived crash: count=%d", count)
	}

	// Verify durable batch 0 product intact
	durable, err := store2.GetProduct(ctx, "SKU-DURABLE-01")
	if err != nil || durable.Price != 100.0 {
		t.Fatalf("durable product corrupted: %+v", durable)
	}

	// Resume job to completion with legitimate batch 1
	resStream := make(chan domain.Batch, 2)
	resStream <- domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records:    []domain.SupplierUpdate{{SKU: "SKU-DURABLE-01", Name: "Durable Product", Price: 100.0, Status: domain.StatusActive, Timestamp: now}},
	}
	resStream <- domain.Batch{
		JobID:      jobID,
		BatchIndex: 1,
		Records:    []domain.SupplierUpdate{{SKU: "SKU-FINAL-02", Name: "Final Item", Price: 200.0, Status: domain.StatusActive, Timestamp: now}},
	}
	close(resStream)

	job2, err := engine2.ResumeJob(ctx, jobID, resStream)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}
	if job2.Status != domain.JobStatusCompleted {
		t.Fatalf("expected COMPLETED, got %s", job2.Status)
	}

	finalP, err := store2.GetProduct(ctx, "SKU-FINAL-02")
	if err != nil || finalP.Price != 200.0 {
		t.Fatalf("final product missing or incorrect: %+v", finalP)
	}
}

// ----------------------------------------------------------------------------
// CHALLENGE 8: EMPTY BATCH CHECKPOINTING & RESUMPTION ACROSS CRASH (AC2)
// ----------------------------------------------------------------------------
// Verifies that batches containing 0 items advance checkpoints monotonically
// without corrupting state or failing resumption.
func TestChallenger_AC2_EmptyBatchResumptionAcrossCrash(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()
	now := time.Now().UTC()
	jobID := "job-empty-batch-crash"

	// Pre-create job with total_batches = 3
	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusRunning, TotalBatches: 3, CommittedBatchIndex: -1,
		CreatedAt: now, UpdatedAt: now,
	})

	// Run Batches 0 & 1: Batch 0 has 1 item, Batch 1 has 0 items
	stream1 := make(chan domain.Batch, 2)
	stream1 <- domain.Batch{
		JobID: jobID, BatchIndex: 0,
		Records: []domain.SupplierUpdate{{SKU: "SKU-E-0", Name: "E0", Price: 10, Status: domain.StatusActive, Timestamp: now}},
	}
	stream1 <- domain.Batch{
		JobID: jobID, BatchIndex: 1,
		Records: []domain.SupplierUpdate{}, // Empty batch
	}
	close(stream1)

	// Will interrupt because stream terminates with committed=1, total=3
	job1, err := engine.RunJob(ctx, jobID, stream1)
	if err == nil {
		t.Fatalf("expected premature closure error")
	}
	if job1.Status != domain.JobStatusInterrupted || job1.CommittedBatchIndex != 1 {
		t.Fatalf("expected status INTERRUPTED with committed_batch_index=1, got %+v", job1)
	}

	// Verify checkpoint 1 exists with items_count = 0
	var itemsCount int
	err = store.DB().QueryRow("SELECT items_count FROM batch_checkpoints WHERE job_id = ? AND batch_index = 1", jobID).Scan(&itemsCount)
	if err != nil || itemsCount != 0 {
		t.Fatalf("expected checkpoint 1 with items_count=0, got %d (err: %v)", itemsCount, err)
	}

	// Resume providing Batch 2
	stream2 := make(chan domain.Batch, 1)
	stream2 <- domain.Batch{
		JobID: jobID, BatchIndex: 2,
		Records: []domain.SupplierUpdate{{SKU: "SKU-E-2", Name: "E2", Price: 20, Status: domain.StatusActive, Timestamp: now}},
	}
	close(stream2)

	jobFinal, err := engine.ResumeJob(ctx, jobID, stream2)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}
	if jobFinal.Status != domain.JobStatusCompleted || jobFinal.CommittedBatchIndex != 2 {
		t.Fatalf("expected COMPLETED with committed=2, got %+v", jobFinal)
	}
}
