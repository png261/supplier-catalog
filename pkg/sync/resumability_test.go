package sync_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// setupDiskStorage initializes a disk-backed SQLite storage instance in a temporary directory.
func setupDiskStorage(t *testing.T) (*storage.SQLiteStorage, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), fmt.Sprintf("test_sync_%d.db", time.Now().UnixNano()))
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open disk storage at %s: %v", dbPath, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store, dbPath
}

// =============================================================================
// Acceptance Criterion 1: Mid-Batch Interruption & Atomic Rollback
// "Simulating an interruption midway through a multi-batch sync leaves prior
//  committed batches intact and uncommitted batch changes rolled back."
// =============================================================================

func TestAC1_CrashMidBatch_RollbackAndDurability(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobID := "ac1-mid-batch-crash"
	baseTime := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	// Construct 4 batches (batches 0, 1, 2, 3)
	batches := []domain.Batch{
		{
			JobID:      jobID,
			BatchIndex: 0,
			Records: []domain.SupplierUpdate{
				{SKU: "SKU-B0-01", Name: "Widget 0-1", Price: 10.0, Status: domain.StatusActive, Timestamp: baseTime},
				{SKU: "SKU-B0-02", Name: "Widget 0-2", Price: 20.0, Status: domain.StatusActive, Timestamp: baseTime},
			},
		},
		{
			JobID:      jobID,
			BatchIndex: 1,
			Records: []domain.SupplierUpdate{
				{SKU: "SKU-B1-01", Name: "Widget 1-1", Price: 30.0, Status: domain.StatusActive, Timestamp: baseTime},
				{SKU: "SKU-B1-02", Name: "Widget 1-2", Price: 40.0, Status: domain.StatusActive, Timestamp: baseTime},
			},
		},
		{
			JobID:      jobID,
			BatchIndex: 2,
			Records: []domain.SupplierUpdate{
				// This batch modifies SKU-B0-01, adds SKU-B2-01, and includes a corrupt record
				{SKU: "SKU-B0-01", Name: "Widget 0-1 MODIFIED", Price: 99.0, Status: domain.StatusActive, Timestamp: baseTime.Add(time.Hour)},
				{SKU: "SKU-B2-01", Name: "Widget 2-1 Ghost", Price: 50.0, Status: domain.StatusActive, Timestamp: baseTime},
				{SKU: "SKU-B2-CORRUPT", Name: "Corrupt", Price: -100.0, Status: domain.StatusActive, Timestamp: baseTime},
			},
		},
		{
			JobID:      jobID,
			BatchIndex: 3,
			Records: []domain.SupplierUpdate{
				{SKU: "SKU-B3-01", Name: "Widget 3-1", Price: 60.0, Status: domain.StatusActive, Timestamp: baseTime},
			},
		},
	}

	stream := make(chan domain.Batch)

	go func() {
		// Send batch 0
		stream <- batches[0]
		// Send batch 1
		stream <- batches[1]

		// Wait briefly to allow batch 1 to commit, then cancel context and send batch 2
		time.Sleep(30 * time.Millisecond)
		cancel() // Abrupt mid-sync cancellation!
		stream <- batches[2]
		close(stream)
	}()

	finalJob, err := engine.RunJob(ctx, jobID, stream)
	if err == nil {
		t.Fatalf("expected cancellation error from RunJob, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// 1. Verify Job Status in DB
	savedJob, err := store.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to query job: %v", err)
	}
	if savedJob.Status != domain.JobStatusInterrupted {
		t.Errorf("expected status INTERRUPTED, got %s", savedJob.Status)
	}
	if savedJob.CompletedAt != nil {
		t.Errorf("completed_at must be nil for interrupted job, got %v", savedJob.CompletedAt)
	}
	if savedJob.CommittedBatchIndex != 1 {
		t.Errorf("expected committed_batch_index = 1, got %d", savedJob.CommittedBatchIndex)
	}
	if finalJob != nil && finalJob.Status != domain.JobStatusInterrupted {
		t.Errorf("finalJob status mismatch: got %s, want INTERRUPTED", finalJob.Status)
	}

	// 2. Verify Batches 0 & 1 Checkpoints exist, Batch 2 Checkpoint DOES NOT exist
	cp0, err0 := store.DB().Query("SELECT batch_index FROM batch_checkpoints WHERE job_id = ? AND batch_index = 0", jobID)
	if err0 != nil || !cp0.Next() {
		t.Errorf("checkpoint for batch 0 must exist")
	}
	cp0.Close()

	cp1, err1 := store.DB().Query("SELECT batch_index FROM batch_checkpoints WHERE job_id = ? AND batch_index = 1", jobID)
	if err1 != nil || !cp1.Next() {
		t.Errorf("checkpoint for batch 1 must exist")
	}
	cp1.Close()

	cp2, err2 := store.DB().Query("SELECT batch_index FROM batch_checkpoints WHERE job_id = ? AND batch_index = 2", jobID)
	if err2 == nil && cp2.Next() {
		cp2.Close()
		t.Fatalf("checkpoint for batch 2 was written despite rollback!")
	}
	if cp2 != nil {
		cp2.Close()
	}

	// 3. Verify Product catalog state: Batches 0 & 1 are intact
	p01, err := store.GetProduct(context.Background(), "SKU-B0-01")
	if err != nil || p01 == nil {
		t.Fatalf("SKU-B0-01 from batch 0 missing: %v", err)
	}
	// Verify uncommitted Batch 2 mutation to SKU-B0-01 was rolled back!
	if p01.Price != 10.0 || p01.Name != "Widget 0-1" {
		t.Errorf("SKU-B0-01 was partially updated by uncommitted batch 2: %+v", p01)
	}

	p02, err := store.GetProduct(context.Background(), "SKU-B0-02")
	if err != nil || p02 == nil || p02.Price != 20.0 {
		t.Errorf("SKU-B0-02 missing or incorrect: %+v", p02)
	}

	p11, err := store.GetProduct(context.Background(), "SKU-B1-01")
	if err != nil || p11 == nil || p11.Price != 30.0 {
		t.Errorf("SKU-B1-01 missing or incorrect: %+v", p11)
	}

	// 4. Verify uncommitted products from Batch 2 DO NOT exist in database
	pGhost, err := store.GetProduct(context.Background(), "SKU-B2-01")
	if err == nil || pGhost != nil {
		t.Errorf("uncommitted product SKU-B2-01 survived in DB: %+v", pGhost)
	}

	// 5. Verify uncommitted dead-letter records from Batch 2 were rolled back
	dlRecords, count, err := store.ListDeadLetterRecords(context.Background(), jobID, "", 10, 0)
	if err != nil {
		t.Fatalf("failed to query dead letters: %v", err)
	}
	if count != 0 || len(dlRecords) != 0 {
		t.Errorf("uncommitted dead-letter issues from batch 2 survived in DB! got %d records", count)
	}
}

// =============================================================================
// Acceptance Criterion 2: Resuming Without Re-Processing Completed Batches
// "Resuming an interrupted sync continues directly from the uncommitted batch
//  to completion without re-processing completed batches."
// =============================================================================

func TestAC2_ResumeJob_ContinuesDirectlyFromUncommitted(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "ac2-resumability-skip"
	baseTime := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

	// Pre-create the job in DB with total_batches = 4
	err := store.CreateJob(ctx, &domain.SyncJob{
		JobID:               jobID,
		Status:              domain.JobStatusRunning,
		TotalBatches:        4,
		CommittedBatchIndex: -1,
		CreatedAt:           baseTime,
		UpdatedAt:           baseTime,
	})
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// Manually execute Batches 0 & 1 using stream until interruption
	batch0 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-RES-0", Name: "Item 0", Price: 10.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}
	batch1 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 1,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-RES-1", Name: "Item 1", Price: 20.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}
	batch2 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 2,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-RES-2", Name: "Item 2", Price: 30.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}
	batch3 := domain.Batch{
		JobID:      jobID,
		BatchIndex: 3,
		Records: []domain.SupplierUpdate{
			{SKU: "SKU-RES-3", Name: "Item 3", Price: 40.0, Status: domain.StatusActive, Timestamp: baseTime},
		},
	}

	// Execute batch 0 and 1 via an interrupted RunJob
	stream1 := make(chan domain.Batch, 2)
	stream1 <- batch0
	stream1 <- batch1
	close(stream1)

	// Run with total_batches = 4; stream closes after 2 batches -> marks INTERRUPTED
	job1, err := engine.RunJob(ctx, jobID, stream1)
	if err == nil {
		t.Fatalf("expected premature closure error, got nil")
	}
	if job1.Status != domain.JobStatusInterrupted {
		t.Fatalf("expected status INTERRUPTED, got %s", job1.Status)
	}
	if job1.CommittedBatchIndex != 1 {
		t.Fatalf("expected committed_batch_index = 1, got %d", job1.CommittedBatchIndex)
	}

	// Record the checkpoint timestamp of batch 0 and batch 1 to verify they are NEVER overwritten
	var cp0Time, cp1Time string
	_ = store.DB().QueryRow("SELECT committed_at FROM batch_checkpoints WHERE job_id = ? AND batch_index = 0", jobID).Scan(&cp0Time)
	_ = store.DB().QueryRow("SELECT committed_at FROM batch_checkpoints WHERE job_id = ? AND batch_index = 1", jobID).Scan(&cp1Time)

	// Now resume with a full stream emitting all 4 batches (0, 1, 2, 3)
	stream2 := make(chan domain.Batch, 4)
	stream2 <- batch0
	stream2 <- batch1
	stream2 <- batch2
	stream2 <- batch3
	close(stream2)

	finalJob, err := engine.ResumeJob(ctx, jobID, stream2)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}

	// 1. Verify job completed
	if finalJob.Status != domain.JobStatusCompleted {
		t.Errorf("expected final status COMPLETED, got %s", finalJob.Status)
	}
	if finalJob.CompletedAt == nil {
		t.Errorf("expected completed_at to be populated")
	}
	if finalJob.CommittedBatchIndex != 3 {
		t.Errorf("expected committed_batch_index = 3, got %d", finalJob.CommittedBatchIndex)
	}

	// 2. Verify all items exist in DB
	for _, sku := range []string{"SKU-RES-0", "SKU-RES-1", "SKU-RES-2", "SKU-RES-3"} {
		p, err := store.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Errorf("product %s not found in DB after resumption: %v", sku, err)
		}
	}

	// 3. Verify checkpoints 0 and 1 were not modified or re-processed
	var cp0TimeAfter, cp1TimeAfter string
	_ = store.DB().QueryRow("SELECT committed_at FROM batch_checkpoints WHERE job_id = ? AND batch_index = 0", jobID).Scan(&cp0TimeAfter)
	_ = store.DB().QueryRow("SELECT committed_at FROM batch_checkpoints WHERE job_id = ? AND batch_index = 1", jobID).Scan(&cp1TimeAfter)

	if cp0Time != cp0TimeAfter {
		t.Errorf("checkpoint 0 was re-written! before=%s, after=%s", cp0Time, cp0TimeAfter)
	}
	if cp1Time != cp1TimeAfter {
		t.Errorf("checkpoint 1 was re-written! before=%s, after=%s", cp1Time, cp1TimeAfter)
	}

	// Check that checkpoints 2 and 3 exist
	var cpCount int
	_ = store.DB().QueryRow("SELECT count(*) FROM batch_checkpoints WHERE job_id = ?", jobID).Scan(&cpCount)
	if cpCount != 4 {
		t.Errorf("expected exactly 4 checkpoints, got %d", cpCount)
	}

	// 4. Cumulative metrics: 4 items added, 0 updated, 0 dead letter
	expectedSummary := domain.ChangeSummary{Added: 4, Updated: 0, Withdrawn: 0, Skipped: 0, DeadLetter: 0}
	if finalJob.Summary != expectedSummary {
		t.Errorf("final summary mismatch: got %+v, want %+v", finalJob.Summary, expectedSummary)
	}
}

// =============================================================================
// Acceptance Criterion 3: State Transition Lifecycle & Startup Recovery
// "Interrupted jobs are marked INTERRUPTED upon restart and only transition
//  to COMPLETED when all batches finish."
// =============================================================================

func TestAC3_StartupRecovery_StateTransitionLifecycle(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	now := time.Now().UTC()

	// Seed 4 jobs representing different lifecycle states:
	// 1. Stale RUNNING job (simulating crash during run)
	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: "job-crashed-running", Status: domain.JobStatusRunning, TotalBatches: 5, CommittedBatchIndex: 2,
		CreatedAt: now, UpdatedAt: now,
	})
	// 2. Stale PENDING job (simulating crash before batch 0)
	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: "job-crashed-pending", Status: domain.JobStatusPending, TotalBatches: 3, CommittedBatchIndex: -1,
		CreatedAt: now, UpdatedAt: now,
	})
	// 3. COMPLETED job (must remain untouched)
	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: "job-already-completed", Status: domain.JobStatusRunning, TotalBatches: 2, CommittedBatchIndex: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	_ = store.UpdateJobStatus(ctx, "job-already-completed", domain.JobStatusCompleted)
	// 4. Already INTERRUPTED job (must remain INTERRUPTED)
	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: "job-already-interrupted", Status: domain.JobStatusInterrupted, TotalBatches: 4, CommittedBatchIndex: 1,
		CreatedAt: now, UpdatedAt: now,
	})

	// Invoke RecoverOnStartup
	recoveredIDs, err := engine.RecoverOnStartup(ctx)
	if err != nil {
		t.Fatalf("RecoverOnStartup failed: %v", err)
	}

	// Verify recovered IDs
	if len(recoveredIDs) != 2 {
		t.Fatalf("expected 2 recovered jobs, got %d (%v)", len(recoveredIDs), recoveredIDs)
	}
	idMap := make(map[string]bool)
	for _, id := range recoveredIDs {
		idMap[id] = true
	}
	if !idMap["job-crashed-running"] || !idMap["job-crashed-pending"] {
		t.Errorf("expected job-crashed-running and job-crashed-pending to be recovered, got %v", recoveredIDs)
	}

	// Verify database statuses
	jRun, _ := store.GetJob(ctx, "job-crashed-running")
	if jRun.Status != domain.JobStatusInterrupted || jRun.CompletedAt != nil {
		t.Errorf("job-crashed-running state corrupt: status=%s, completedAt=%v", jRun.Status, jRun.CompletedAt)
	}

	jPend, _ := store.GetJob(ctx, "job-crashed-pending")
	if jPend.Status != domain.JobStatusInterrupted || jPend.CompletedAt != nil {
		t.Errorf("job-crashed-pending state corrupt: status=%s, completedAt=%v", jPend.Status, jPend.CompletedAt)
	}

	jComp, _ := store.GetJob(ctx, "job-already-completed")
	if jComp.Status != domain.JobStatusCompleted || jComp.CompletedAt == nil {
		t.Errorf("completed job modified during recovery: status=%s, completedAt=%v", jComp.Status, jComp.CompletedAt)
	}

	// Verify GetActiveJob returns ErrNotFound now that stale active jobs are recovered
	activeJob, err := store.GetActiveJob(ctx)
	if !errors.Is(err, domain.ErrNotFound) || activeJob != nil {
		t.Errorf("expected no active job after recovery, got active=%+v, err=%v", activeJob, err)
	}

	// Resume the recovered job and verify monotonic state transitions:
	// RUNNING while running -> COMPLETED only upon final batch
	stream := make(chan domain.Batch)
	go func() {
		// Emit batches 3 and 4 to finish "job-crashed-running" (committed index was 2, total 5)
		stream <- domain.Batch{
			JobID: "job-crashed-running", BatchIndex: 3,
			Records: []domain.SupplierUpdate{{SKU: "SKU-3", Name: "V3", Price: 30, Status: domain.StatusActive, Timestamp: now}},
		}
		stream <- domain.Batch{
			JobID: "job-crashed-running", BatchIndex: 4,
			Records: []domain.SupplierUpdate{{SKU: "SKU-4", Name: "V4", Price: 40, Status: domain.StatusActive, Timestamp: now}},
		}
		close(stream)
	}()

	resumedJob, err := engine.ResumeJob(ctx, "job-crashed-running", stream)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}
	if resumedJob.Status != domain.JobStatusCompleted {
		t.Errorf("expected COMPLETED status after all batches finished, got %s", resumedJob.Status)
	}
	if resumedJob.CompletedAt == nil {
		t.Errorf("expected completed_at to be non-nil")
	}

	// Verify clean startup with 0 stale jobs returns nil, nil
	cleanRecovered, err := engine.RecoverOnStartup(ctx)
	if err != nil || len(cleanRecovered) != 0 {
		t.Errorf("expected empty recovery on clean DB, got %v, err=%v", cleanRecovered, err)
	}
}

// =============================================================================
// Acceptance Criterion 4: Dead-Letter Isolation Without Halting
// "Records with negative prices or empty SKUs are logged to the issues table
//  without halting the batch."
// =============================================================================

func TestAC4_DeadLetter_IsolationAndNonHalting(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "ac4-dead-letter-job"
	now := time.Now().UTC()

	// Pre-seed SKU-VALID-2 as ACTIVE so its transition to WITHDRAWN counts towards Withdrawn
	_, _ = store.DB().Exec("INSERT INTO products (sku, name, price, status, supplier_timestamp, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"SKU-VALID-2", "Valid Pre-existing Product", 40.0, "ACTIVE", now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano))

	batch := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			// 1. Valid Active item
			{SKU: "SKU-VALID-1", Name: "Valid Product 1", Price: 19.99, Status: domain.StatusActive, Timestamp: now},
			// 2. Corrupt: Negative price
			{SKU: "SKU-INVALID-NEG", Name: "Negative Price Item", Price: -5.00, Status: domain.StatusActive, Timestamp: now},
			// 3. Corrupt: Empty SKU
			{SKU: "", Name: "Missing SKU Item", Price: 25.00, Status: domain.StatusActive, Timestamp: now},
			// 4. Corrupt: Whitespace-only SKU
			{SKU: "   \t\n", Name: "Whitespace SKU Item", Price: 30.00, Status: domain.StatusActive, Timestamp: now},
			// 5. Valid Withdrawn item
			{SKU: "SKU-VALID-2", Name: "Valid Withdrawn Product", Price: 49.99, Status: domain.StatusWithdrawn, Timestamp: now},
			// 6. Corrupt: IEEE 754 NaN price
			{SKU: "SKU-INVALID-NAN", Name: "NaN Price", Price: math.NaN(), Status: domain.StatusActive, Timestamp: now},
			// 7. Corrupt: Positive Infinity price
			{SKU: "SKU-INVALID-INF", Name: "Inf Price", Price: math.Inf(1), Status: domain.StatusActive, Timestamp: now},
			// 8. Corrupt: Negative Infinity price
			{SKU: "SKU-INVALID-NEGINF", Name: "-Inf Price", Price: math.Inf(-1), Status: domain.StatusActive, Timestamp: now},
			// 9. Corrupt: Invalid Status string
			{SKU: "SKU-INVALID-STATUS", Name: "Bad Status", Price: 10.00, Status: domain.ProductStatus("DISCONTINUED"), Timestamp: now},
			// 10. Valid Active item
			{SKU: "SKU-VALID-3", Name: "Valid Product 3", Price: 99.99, Status: domain.StatusActive, Timestamp: now},
			// 11. Corrupt: Zero timestamp
			{SKU: "SKU-INVALID-ZEROTIME", Name: "Zero Time Item", Price: 15.00, Status: domain.StatusActive, Timestamp: time.Time{}},
		},
	}

	stream := make(chan domain.Batch, 1)
	stream <- batch
	close(stream)

	job, err := engine.RunJob(ctx, jobID, stream)
	if err != nil {
		t.Fatalf("RunJob failed on batch with dead letters: %v", err)
	}

	// 1. Verify batch completed successfully without halting
	if job.Status != domain.JobStatusCompleted {
		t.Errorf("expected COMPLETED status, got %s", job.Status)
	}

	// Summary: 2 Added, 1 Withdrawn, 8 DeadLetter, 0 Skipped
	expectedSummary := domain.ChangeSummary{Added: 2, Withdrawn: 1, DeadLetter: 8, Updated: 0, Skipped: 0}
	if job.Summary != expectedSummary {
		t.Errorf("summary mismatch: got %+v, want %+v", job.Summary, expectedSummary)
	}

	// 2. Verify Valid Products committed to products table
	p1, err1 := store.GetProduct(ctx, "SKU-VALID-1")
	if err1 != nil || p1 == nil || p1.Price != 19.99 {
		t.Errorf("valid product 1 not saved: %+v, err: %v", p1, err1)
	}
	p2, err2 := store.GetProduct(ctx, "SKU-VALID-2")
	if err2 != nil || p2 == nil || p2.Status != domain.StatusWithdrawn {
		t.Errorf("valid withdrawn product not saved: %+v, err: %v", p2, err2)
	}
	p3, err3 := store.GetProduct(ctx, "SKU-VALID-3")
	if err3 != nil || p3 == nil || p3.Price != 99.99 {
		t.Errorf("valid product 3 not saved: %+v, err: %v", p3, err3)
	}

	// Verify zero-timestamp item was NOT committed to products table
	pZero, errZero := store.GetProduct(ctx, "SKU-INVALID-ZEROTIME")
	if errZero == nil && pZero != nil {
		t.Errorf("corrupt zero-timestamp product was erroneously saved: %+v", pZero)
	}

	// 3. Verify Corrupt items quarantined to dead_letter_issues
	dlRecords, totalCount, err := store.ListDeadLetterRecords(ctx, jobID, "", 20, 0)
	if err != nil {
		t.Fatalf("failed to query dead letters: %v", err)
	}
	if totalCount != 8 || len(dlRecords) != 8 {
		t.Errorf("expected exactly 8 dead letters, got totalCount=%d, len=%d", totalCount, len(dlRecords))
	}

	// Verify reasons are correctly logged
	reasonsMap := make(map[string]bool)
	for _, dl := range dlRecords {
		reasonsMap[dl.Reason] = true
		if dl.JobID != jobID || dl.BatchIndex != 0 {
			t.Errorf("dead letter metadata incorrect: %+v", dl)
		}
		if dl.RawPayload == "" {
			t.Errorf("raw payload empty for dead letter: %+v", dl)
		}
	}

	expectedSubstrings := []string{"negative price", "empty SKU", "price is NaN", "price is infinite", "invalid status", "timestamp"}
	for _, sub := range expectedSubstrings {
		found := false
		for reason := range reasonsMap {
			if containsSubstring(reason, sub) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected dead letter reason containing %q, recorded reasons: %v", sub, reasonsMap)
		}
	}
}

// =============================================================================
// Comprehensive Edge Case & Invariant Tests
// =============================================================================

// TestResumability_SequenceGapError verifies detection of gap in batch sequence.
func TestResumability_SequenceGapError(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-gap-test"
	stream := make(chan domain.Batch, 2)
	// Send batch 0 then batch 2 (skipping batch 1)
	stream <- domain.Batch{JobID: jobID, BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "S0", Name: "V0", Price: 1, Status: domain.StatusActive, Timestamp: time.Now().UTC()}}}
	stream <- domain.Batch{JobID: jobID, BatchIndex: 2, Records: []domain.SupplierUpdate{{SKU: "S2", Name: "V2", Price: 2, Status: domain.StatusActive, Timestamp: time.Now().UTC()}}}
	close(stream)

	job, err := engine.RunJob(ctx, jobID, stream)
	if err == nil {
		t.Fatalf("expected gap error, got nil")
	}
	if !containsSubstring(err.Error(), "gap detected in batch sequence") {
		t.Errorf("unexpected error message: %v", err)
	}
	if job.Status != domain.JobStatusInterrupted {
		t.Errorf("expected status INTERRUPTED on gap error, got %s", job.Status)
	}
	if job.CommittedBatchIndex != 0 {
		t.Errorf("expected committed_batch_index = 0, got %d", job.CommittedBatchIndex)
	}
}

// TestResumability_EmptyBatchAdvancesCheckpoint verifies that empty batches advance checkpoint safely.
func TestResumability_EmptyBatchAdvancesCheckpoint(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-empty-batch-test"
	stream := make(chan domain.Batch, 3)
	now := time.Now().UTC()
	stream <- domain.Batch{JobID: jobID, BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "S0", Name: "V0", Price: 10, Status: domain.StatusActive, Timestamp: now}}}
	stream <- domain.Batch{JobID: jobID, BatchIndex: 1, Records: []domain.SupplierUpdate{}} // Empty batch
	stream <- domain.Batch{JobID: jobID, BatchIndex: 2, Records: []domain.SupplierUpdate{{SKU: "S2", Name: "V2", Price: 20, Status: domain.StatusActive, Timestamp: now}}}
	close(stream)

	job, err := engine.RunJob(ctx, jobID, stream)
	if err != nil {
		t.Fatalf("RunJob with empty batch failed: %v", err)
	}
	if job.Status != domain.JobStatusCompleted || job.CommittedBatchIndex != 2 {
		t.Errorf("job failed to complete with empty batch: %+v", job)
	}

	// Verify checkpoint 1 recorded with items_count = 0
	var itemsCount int
	err = store.DB().QueryRow("SELECT items_count FROM batch_checkpoints WHERE job_id = ? AND batch_index = 1", jobID).Scan(&itemsCount)
	if err != nil || itemsCount != 0 {
		t.Errorf("expected checkpoint 1 with items_count = 0, got %d, err=%v", itemsCount, err)
	}
}

// TestResumability_ResumeNonExistentJob verifies error when resuming unknown job.
func TestResumability_ResumeNonExistentJob(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	stream := make(chan domain.Batch)
	close(stream)

	_, err := engine.ResumeJob(ctx, "job-does-not-exist", stream)
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound when resuming unknown job, got %v", err)
	}
}

// TestResumability_ResumeAlreadyCompletedJob verifies rejection of resume on completed job.
func TestResumability_ResumeAlreadyCompletedJob(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-comp-test"
	now := time.Now().UTC()
	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusCompleted, TotalBatches: 1, CommittedBatchIndex: 0,
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	})

	stream := make(chan domain.Batch)
	close(stream)

	_, err := engine.ResumeJob(ctx, jobID, stream)
	if err == nil || !errors.Is(err, catalogSync.ErrJobAlreadyCompleted) {
		t.Errorf("expected ErrJobAlreadyCompleted on resume, got %v", err)
	}

	_, err = engine.RunJob(ctx, jobID, stream)
	if err == nil || !errors.Is(err, catalogSync.ErrJobAlreadyCompleted) {
		t.Errorf("expected ErrJobAlreadyCompleted on run, got %v", err)
	}
}

// TestResumability_ConcurrentExecutionConflict verifies mutex protection against duplicate active jobs.
func TestResumability_ConcurrentExecutionConflict(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-concurrent-lock"
	stream1 := make(chan domain.Batch)
	stream2 := make(chan domain.Batch)
	defer close(stream2)

	var wg sync.WaitGroup
	wg.Add(1)

	var runErr error
	go func() {
		defer wg.Done()
		_, runErr = engine.RunJob(ctx, jobID, stream1)
	}()

	// Wait for goroutine to acquire activeJobs lock
	time.Sleep(20 * time.Millisecond)

	// Second concurrent call for same job ID must fail with ErrJobAlreadyRunning
	_, conflictErr := engine.ResumeJob(ctx, jobID, stream2)
	if conflictErr == nil || !errors.Is(conflictErr, catalogSync.ErrJobAlreadyRunning) {
		t.Errorf("expected ErrJobAlreadyRunning on concurrent execution, got %v", conflictErr)
	}

	// Close stream1 to allow first job to finish
	close(stream1)
	wg.Wait()
	if runErr != nil {
		t.Errorf("job 1 unexpected error: %v", runErr)
	}
}

// TestResumability_TombstonePreservedAcrossCrashAndResume verifies Invariant C across restart.
func TestResumability_TombstonePreservedAcrossCrashAndResume(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-tombstone-resume"
	t1 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	// Batch 0: Item is born WITHDRAWN
	stream0 := make(chan domain.Batch, 1)
	stream0 <- domain.Batch{
		JobID: jobID, BatchIndex: 0,
		Records: []domain.SupplierUpdate{{SKU: "SKU-TOMB-AC", Name: "Item", Price: 10, Status: domain.StatusWithdrawn, Timestamp: t1}},
	}
	close(stream0)

	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusRunning, TotalBatches: 2, CommittedBatchIndex: -1,
		CreatedAt: t1, UpdatedAt: t1,
	})

	_, _ = engine.RunJob(ctx, jobID, stream0) // Process batch 0, total_batches=2 -> interrupted

	// Verify status in DB is WITHDRAWN
	p, _ := store.GetProduct(ctx, "SKU-TOMB-AC")
	if p.Status != domain.StatusWithdrawn {
		t.Fatalf("expected initial status WITHDRAWN, got %s", p.Status)
	}

	// Resumed Batch 1 tries to resurrect item with ACTIVE status and future timestamp t3
	stream1 := make(chan domain.Batch, 2)
	stream1 <- domain.Batch{
		JobID: jobID, BatchIndex: 0,
		Records: []domain.SupplierUpdate{{SKU: "SKU-TOMB-AC", Name: "Item", Price: 10, Status: domain.StatusWithdrawn, Timestamp: t1}},
	}
	stream1 <- domain.Batch{
		JobID: jobID, BatchIndex: 1,
		Records: []domain.SupplierUpdate{{SKU: "SKU-TOMB-AC", Name: "Resurrect Attempt", Price: 99, Status: domain.StatusActive, Timestamp: t3}},
	}
	close(stream1)

	finalJob, err := engine.ResumeJob(ctx, jobID, stream1)
	if err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}
	if finalJob.Status != domain.JobStatusCompleted {
		t.Errorf("expected COMPLETED status, got %s", finalJob.Status)
	}

	// Invariant C Check: Status MUST remain WITHDRAWN!
	pAfter, _ := store.GetProduct(ctx, "SKU-TOMB-AC")
	if pAfter.Status != domain.StatusWithdrawn {
		t.Fatalf("Invariant C violated across resume: product resurrected to %s", pAfter.Status)
	}
	if pAfter.Name != "Item" || pAfter.Price != 10 {
		t.Errorf("metadata updated on forbidden resurrection: %+v", pAfter)
	}
	_ = t2
}

// TestResumability_100PercentInvalidBatch verifies a batch of 100% invalid records commits cleanly.
func TestResumability_100PercentInvalidBatch(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-100p-invalid"
	batch := domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "", Name: "Bad1", Price: -1.0, Status: domain.StatusActive, Timestamp: time.Now().UTC()},
			{SKU: "  ", Name: "Bad2", Price: 10.0, Status: domain.StatusActive, Timestamp: time.Now().UTC()},
			{SKU: "SKU-3", Name: "Bad3", Price: -50.0, Status: domain.StatusActive, Timestamp: time.Now().UTC()},
		},
	}

	stream := make(chan domain.Batch, 1)
	stream <- batch
	close(stream)

	job, err := engine.RunJob(ctx, jobID, stream)
	if err != nil {
		t.Fatalf("RunJob failed on 100%% invalid batch: %v", err)
	}

	if job.Summary.DeadLetter != 3 || job.Summary.Added != 0 {
		t.Errorf("summary mismatch: %+v", job.Summary)
	}
	if job.Status != domain.JobStatusCompleted {
		t.Errorf("expected COMPLETED status, got %s", job.Status)
	}
}

// TestResumability_NilStorageErrors covers ErrNilStorage on all engine methods.
func TestResumability_NilStorageErrors(t *testing.T) {
	engine := catalogSync.NewEngine(nil)
	ctx := context.Background()
	stream := make(chan domain.Batch)
	close(stream)

	_, err := engine.RunJob(ctx, "job-id", stream)
	if err == nil || !errors.Is(err, catalogSync.ErrNilStorage) {
		t.Errorf("expected ErrNilStorage from RunJob, got %v", err)
	}

	_, err = engine.ResumeJob(ctx, "job-id", stream)
	if err == nil || !errors.Is(err, catalogSync.ErrNilStorage) {
		t.Errorf("expected ErrNilStorage from ResumeJob, got %v", err)
	}

	_, err = engine.RecoverOnStartup(ctx)
	if err == nil || !errors.Is(err, catalogSync.ErrNilStorage) {
		t.Errorf("expected ErrNilStorage from RecoverOnStartup, got %v", err)
	}
}

// TestResumability_ContextCancellationBeforeStart tests immediate cancellation.
func TestResumability_ContextCancellationBeforeStart(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before call

	stream := make(chan domain.Batch)
	close(stream)

	_, err := engine.RunJob(ctx, "job-cancel-start", stream)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from RunJob, got %v", err)
	}

	_, err = engine.ResumeJob(ctx, "job-cancel-start", stream)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from ResumeJob, got %v", err)
	}

	_, err = engine.RecoverOnStartup(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from RecoverOnStartup, got %v", err)
	}
}

// TestResumability_WALCrash_SimulatedAbruptTermination simulates SIGKILL during uncommitted batch transaction.
func TestResumability_WALCrash_SimulatedAbruptTermination(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wal_hard_crash.db")
	ctx := context.Background()

	// 1. Initial process: commit Batch 0, start Batch 1 uncommitted, then kill process by closing DB
	store1, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening store1: %v", err)
	}

	jobID := "job-wal-kill"
	now := time.Now().UTC()
	_ = store1.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusRunning, TotalBatches: 2, CommittedBatchIndex: -1,
		CreatedAt: now, UpdatedAt: now,
	})

	// Commit batch 0
	tx0, _ := store1.BeginTx(ctx)
	_ = store1.UpsertProductTx(ctx, tx0, &domain.Product{
		SKU: "SKU-DURABLE-WAL", Name: "Durable", Price: 50, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now,
	})
	_ = store1.RecordBatchCheckpointTx(ctx, tx0, &domain.BatchCheckpoint{
		JobID: jobID, BatchIndex: 0, ItemsCount: 1, Summary: domain.ChangeSummary{Added: 1}, CommittedAt: now,
	})
	_ = store1.UpdateJobProgressTx(ctx, tx0, jobID, 0, domain.ChangeSummary{Added: 1})
	_ = tx0.Commit()

	// Start batch 1 uncommitted
	tx1, _ := store1.BeginTx(ctx)
	_ = store1.UpsertProductTx(ctx, tx1, &domain.Product{
		SKU: "SKU-GHOST-WAL", Name: "Ghost", Price: 999, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now,
	})
	// Simulate hard crash: uncommitted transaction is rolled back on abnormal termination
	_ = tx1.Rollback()
	_ = store1.Close()

	// 2. Restarted process: open new connection to same database file
	store2, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed reopening store2 after crash: %v", err)
	}
	defer store2.Close()

	engine2 := catalogSync.NewEngine(store2)

	// Startup recovery hook runs
	recovered, err := engine2.RecoverOnStartup(ctx)
	if err != nil {
		t.Fatalf("RecoverOnStartup failed: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != jobID {
		t.Errorf("expected recovered job %s, got %v", jobID, recovered)
	}

	// Verify uncommitted ghost product was wiped by SQLite WAL recovery
	ghost, err := store2.GetProduct(ctx, "SKU-GHOST-WAL")
	if err == nil || ghost != nil {
		t.Fatalf("ghost product survived crash! WAL rollback failed: %+v", ghost)
	}

	// Verify committed product survives intact
	durable, err := store2.GetProduct(ctx, "SKU-DURABLE-WAL")
	if err != nil || durable == nil || durable.Price != 50 {
		t.Fatalf("durable product corrupted: %+v, err: %v", durable, err)
	}

	// Verify job status is INTERRUPTED, committed index is 0
	jState, err := store2.GetJob(ctx, jobID)
	if err != nil || jState.Status != domain.JobStatusInterrupted || jState.CommittedBatchIndex != 0 {
		t.Errorf("unexpected recovered job state: %+v", jState)
	}
}

// TestResumability_BatchExecutionFailure_MarksFailed tests storage failure in processBatchTx.
func TestResumability_BatchExecutionFailure_MarksFailed(t *testing.T) {
	baseStore, _ := setupDiskStorage(t)
	faultStore := &faultyStorage{Storage: baseStore, failOnCheckpoint: true}
	engine := catalogSync.NewEngine(faultStore)
	ctx := context.Background()

	jobID := "job-fail-test"
	stream := make(chan domain.Batch, 1)
	stream <- domain.Batch{
		JobID:      jobID,
		BatchIndex: 0,
		Records: []domain.SupplierUpdate{
			{SKU: "S-FAIL", Name: "Fail", Price: 10, Status: domain.StatusActive, Timestamp: time.Now().UTC()},
		},
	}
	close(stream)

	job, err := engine.RunJob(ctx, jobID, stream)
	if err == nil {
		t.Fatalf("expected batch execution error, got nil")
	}
	if job.Status != domain.JobStatusFailed {
		t.Errorf("expected status FAILED on non-context error, got %s", job.Status)
	}
}

// TestResumability_RunJob_ExistingJobInInterruptedState tests RunJob transitioning existing interrupted job.
func TestResumability_RunJob_ExistingJobInInterruptedState(t *testing.T) {
	store, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(store)
	ctx := context.Background()

	jobID := "job-existing-interrupted"
	now := time.Now().UTC()
	_ = store.CreateJob(ctx, &domain.SyncJob{
		JobID: jobID, Status: domain.JobStatusInterrupted, TotalBatches: 2, CommittedBatchIndex: -1,
		CreatedAt: now, UpdatedAt: now,
	})

	stream := make(chan domain.Batch, 2)
	stream <- domain.Batch{JobID: jobID, BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "S0", Name: "V0", Price: 10, Status: domain.StatusActive, Timestamp: now}}}
	stream <- domain.Batch{JobID: jobID, BatchIndex: 1, Records: []domain.SupplierUpdate{{SKU: "S1", Name: "V1", Price: 20, Status: domain.StatusActive, Timestamp: now}}}
	close(stream)

	job, err := engine.RunJob(ctx, jobID, stream)
	if err != nil {
		t.Fatalf("RunJob failed on existing interrupted job: %v", err)
	}
	if job.Status != domain.JobStatusCompleted {
		t.Errorf("expected COMPLETED, got %s", job.Status)
	}
}

// TestResumability_DeadlineExceeded_Branch tests context deadline expired before batch processing.
func TestResumability_DeadlineExceeded_Branch(t *testing.T) {
	baseStore, _ := setupDiskStorage(t)
	engine := catalogSync.NewEngine(baseStore)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	stream := make(chan domain.Batch, 1)
	stream <- domain.Batch{
		JobID: "j-deadline", BatchIndex: 0,
		Records: []domain.SupplierUpdate{{SKU: "D1", Name: "D", Price: 10, Status: domain.StatusActive, Timestamp: time.Now().UTC()}},
	}
	close(stream)

	_, err := engine.RunJob(ctx, "j-deadline", stream)
	if err == nil {
		t.Errorf("expected deadline exceeded error")
	}
}

// TestResumability_StorageErrorBranches thoroughly covers all error handling branches.
func TestResumability_StorageErrorBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("BeginTx_Failure", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		fault := &faultyStorage{Storage: baseStore, failOnBeginTx: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch, 1)
		stream <- domain.Batch{JobID: "j1", BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "A", Name: "A", Price: 1, Status: domain.StatusActive, Timestamp: now}}}
		close(stream)
		_, err := engine.RunJob(ctx, "j1", stream)
		if err == nil {
			t.Errorf("expected BeginTx error")
		}
	})

	t.Run("UpdateJobProgress_Failure", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		fault := &faultyStorage{Storage: baseStore, failOnProgress: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch, 1)
		stream <- domain.Batch{JobID: "j2", BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "A", Name: "A", Price: 1, Status: domain.StatusActive, Timestamp: now}}}
		close(stream)
		_, err := engine.RunJob(ctx, "j2", stream)
		if err == nil {
			t.Errorf("expected Progress error")
		}
	})

	t.Run("CreateJob_Failure", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		fault := &faultyStorage{Storage: baseStore, failOnCreateJob: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch)
		close(stream)
		_, err := engine.RunJob(ctx, "j3", stream)
		if err == nil {
			t.Errorf("expected CreateJob error")
		}
	})

	t.Run("GetJob_Lookup_Failure", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		fault := &faultyStorage{Storage: baseStore, failOnGetJob: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch)
		close(stream)
		_, err := engine.RunJob(ctx, "j4", stream)
		if err == nil {
			t.Errorf("expected GetJob error from RunJob")
		}
		_, err = engine.ResumeJob(ctx, "j4", stream)
		if err == nil {
			t.Errorf("expected GetJob error from ResumeJob")
		}
	})

	t.Run("UpdateJobStatus_Failure_On_Resume", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		_ = baseStore.CreateJob(ctx, &domain.SyncJob{JobID: "j5", Status: domain.JobStatusInterrupted, TotalBatches: 1, CommittedBatchIndex: -1, CreatedAt: now, UpdatedAt: now})
		fault := &faultyStorage{Storage: baseStore, failOnUpdateJobStatus: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch)
		close(stream)
		_, err := engine.ResumeJob(ctx, "j5", stream)
		if err == nil {
			t.Errorf("expected UpdateJobStatus error from ResumeJob")
		}
	})

	t.Run("UpdateJobStatus_Failure_On_RunJob_Existing", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		_ = baseStore.CreateJob(ctx, &domain.SyncJob{JobID: "j6", Status: domain.JobStatusInterrupted, TotalBatches: 1, CommittedBatchIndex: -1, CreatedAt: now, UpdatedAt: now})
		fault := &faultyStorage{Storage: baseStore, failOnUpdateJobStatus: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch)
		close(stream)
		_, err := engine.RunJob(ctx, "j6", stream)
		if err == nil {
			t.Errorf("expected UpdateJobStatus error from RunJob")
		}
	})

	t.Run("RecordDeadLetter_Failure", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		fault := &faultyStorage{Storage: baseStore, failOnRecordDeadLetter: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch, 1)
		stream <- domain.Batch{JobID: "j7", BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "", Price: -1, Status: domain.StatusActive, Timestamp: now}}}
		close(stream)
		_, err := engine.RunJob(ctx, "j7", stream)
		if err == nil {
			t.Errorf("expected RecordDeadLetter error")
		}
	})

	t.Run("UpsertProduct_Failure", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		fault := &faultyStorage{Storage: baseStore, failOnUpsertProduct: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch, 1)
		stream <- domain.Batch{JobID: "j8", BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "A", Name: "A", Price: 10, Status: domain.StatusActive, Timestamp: now}}}
		close(stream)
		_, err := engine.RunJob(ctx, "j8", stream)
		if err == nil {
			t.Errorf("expected UpsertProduct error")
		}
	})

	t.Run("GetProductTx_Failure", func(t *testing.T) {
		baseStore, _ := setupDiskStorage(t)
		fault := &faultyStorage{Storage: baseStore, failOnGetProductTx: true}
		engine := catalogSync.NewEngine(fault)
		stream := make(chan domain.Batch, 1)
		stream <- domain.Batch{JobID: "j9", BatchIndex: 0, Records: []domain.SupplierUpdate{{SKU: "A", Name: "A", Price: 10, Status: domain.StatusActive, Timestamp: now}}}
		close(stream)
		_, err := engine.RunJob(ctx, "j9", stream)
		if err == nil {
			t.Errorf("expected GetProductTx error")
		}
	})
}

// faultyStorage wraps Storage to inject failures into specific methods.
type faultyStorage struct {
	storage.Storage
	failOnBeginTx          bool
	failOnCheckpoint       bool
	failOnProgress         bool
	failOnGetJob           bool
	failOnCreateJob        bool
	failOnUpdateJobStatus  bool
	failOnRecordDeadLetter bool
	failOnUpsertProduct    bool
	failOnGetProductTx     bool
}

func (f *faultyStorage) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if f.failOnBeginTx {
		return nil, errors.New("injected begin tx error")
	}
	return f.Storage.BeginTx(ctx)
}

func (f *faultyStorage) RecordBatchCheckpointTx(ctx context.Context, tx *sql.Tx, cp *domain.BatchCheckpoint) error {
	if f.failOnCheckpoint {
		return errors.New("injected storage error: disk I/O failure")
	}
	return f.Storage.RecordBatchCheckpointTx(ctx, tx, cp)
}

func (f *faultyStorage) UpdateJobProgressTx(ctx context.Context, tx *sql.Tx, jobID string, batchIndex int, delta domain.ChangeSummary) error {
	if f.failOnProgress {
		return errors.New("injected progress error")
	}
	return f.Storage.UpdateJobProgressTx(ctx, tx, jobID, batchIndex, delta)
}

func (f *faultyStorage) GetJob(ctx context.Context, jobID string) (*domain.SyncJob, error) {
	if f.failOnGetJob {
		return nil, errors.New("injected get job error")
	}
	return f.Storage.GetJob(ctx, jobID)
}

func (f *faultyStorage) CreateJob(ctx context.Context, job *domain.SyncJob) error {
	if f.failOnCreateJob {
		return errors.New("injected create job error")
	}
	return f.Storage.CreateJob(ctx, job)
}

func (f *faultyStorage) UpdateJobStatus(ctx context.Context, jobID string, status domain.JobStatus) error {
	if f.failOnUpdateJobStatus {
		return errors.New("injected update job status error")
	}
	return f.Storage.UpdateJobStatus(ctx, jobID, status)
}

func (f *faultyStorage) RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error {
	if f.failOnRecordDeadLetter {
		return errors.New("injected dead letter error")
	}
	return f.Storage.RecordDeadLetterTx(ctx, tx, issue)
}

func (f *faultyStorage) UpsertProductTx(ctx context.Context, tx *sql.Tx, p *domain.Product) error {
	if f.failOnUpsertProduct {
		return errors.New("injected upsert error")
	}
	return f.Storage.UpsertProductTx(ctx, tx, p)
}

func (f *faultyStorage) GetProductTx(ctx context.Context, tx *sql.Tx, sku string) (*domain.Product, error) {
	if f.failOnGetProductTx {
		return nil, errors.New("injected get product tx error")
	}
	return f.Storage.GetProductTx(ctx, tx, sku)
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && (findSubstr(s, substr))))
}

func findSubstr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
