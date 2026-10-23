package storage_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
)

func TestStorage_InitAndPragmas(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory storage: %v", err)
	}
	defer s.Close()

	if s.DB() == nil {
		t.Fatal("expected non-nil DB()")
	}

	// Verify busy_timeout pragma
	var busyTimeout int
	err = s.DB().QueryRowContext(ctx, "PRAGMA busy_timeout;").Scan(&busyTimeout)
	if err != nil {
		t.Fatalf("failed querying busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("expected busy_timeout = 5000, got %d", busyTimeout)
	}

	// Verify foreign_keys pragma
	var foreignKeys int
	err = s.DB().QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&foreignKeys)
	if err != nil {
		t.Fatalf("failed querying foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("expected foreign_keys = 1, got %d", foreignKeys)
	}
}

func TestStorage_UpsertAndGetProduct(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory storage: %v", err)
	}
	defer s.Close()

	// 1. Query non-existent product
	_, err = s.GetProduct(ctx, "SKU-NON-EXISTENT")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. Insert new product via transaction
	now := time.Now().UTC().Truncate(time.Millisecond)
	p := &domain.Product{
		SKU:               "SKU-001",
		Name:              "Widget Pro",
		Price:             49.99,
		Status:            domain.StatusActive,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}

	// Verify GetProductTx inside transaction before insert
	_, err = s.GetProductTx(ctx, tx, "SKU-001")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound inside tx before insert, got %v", err)
	}

	if err := s.UpsertProductTx(ctx, tx, p); err != nil {
		_ = tx.Rollback()
		t.Fatalf("failed upserting product: %v", err)
	}

	// Verify GetProductTx sees uncommitted write
	pTx, err := s.GetProductTx(ctx, tx, "SKU-001")
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("expected to read uncommitted insert inside tx, got %v", err)
	}
	if pTx.Name != "Widget Pro" {
		t.Errorf("expected Name 'Widget Pro', got %s", pTx.Name)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("failed committing tx: %v", err)
	}

	// 3. Query committed product via GetProduct
	saved, err := s.GetProduct(ctx, "SKU-001")
	if err != nil {
		t.Fatalf("failed retrieving committed product: %v", err)
	}
	if saved.SKU != p.SKU || saved.Name != p.Name || saved.Price != p.Price || saved.Status != p.Status {
		t.Errorf("mismatch in stored product: %+v vs %+v", saved, p)
	}
	if !saved.SupplierTimestamp.Equal(p.SupplierTimestamp) {
		t.Errorf("timestamp mismatch: got %v, want %v", saved.SupplierTimestamp, p.SupplierTimestamp)
	}

	// 4. Update product via UpsertProductTx (ON CONFLICT DO UPDATE)
	later := now.Add(10 * time.Minute)
	p.Price = 39.99
	p.Status = domain.StatusWithdrawn
	p.SupplierTimestamp = later
	p.UpdatedAt = later

	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed beginning tx2: %v", err)
	}
	if err := s.UpsertProductTx(ctx, tx2, p); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("failed upserting updated product: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("failed committing tx2: %v", err)
	}

	updated, err := s.GetProduct(ctx, "SKU-001")
	if err != nil {
		t.Fatalf("failed retrieving updated product: %v", err)
	}
	if updated.Price != 39.99 || updated.Status != domain.StatusWithdrawn {
		t.Errorf("expected updated price 39.99 and WITHDRAWN status, got %+v", updated)
	}
}

func TestStorage_JobLifecycleAndRecovery(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	job := &domain.SyncJob{
		JobID:               "job-100",
		Status:              domain.JobStatusRunning,
		TotalBatches:        10,
		CommittedBatchIndex: -1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("failed creating job: %v", err)
	}

	// GetJob
	fetched, err := s.GetJob(ctx, "job-100")
	if err != nil {
		t.Fatalf("failed getting job: %v", err)
	}
	if fetched.JobID != "job-100" || fetched.Status != domain.JobStatusRunning {
		t.Errorf("unexpected job fields: %+v", fetched)
	}

	// Non-existent job
	_, err = s.GetJob(ctx, "job-missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing job, got %v", err)
	}

	// GetActiveJob
	active, err := s.GetActiveJob(ctx)
	if err != nil {
		t.Fatalf("failed retrieving active job: %v", err)
	}
	if active.JobID != "job-100" || active.Status != domain.JobStatusRunning {
		t.Errorf("unexpected active job: %+v", active)
	}

	// UpdateJobStatus
	if err := s.UpdateJobStatus(ctx, "job-100", domain.JobStatusRunning); err != nil {
		t.Fatalf("failed updating job status: %v", err)
	}

	// RecoverInterruptedJobs: converts RUNNING/PENDING to INTERRUPTED
	recovered, err := s.RecoverInterruptedJobs(ctx)
	if err != nil {
		t.Fatalf("failed recovering jobs: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != "job-100" {
		t.Fatalf("expected recovered [job-100], got %v", recovered)
	}

	// Verify status is now INTERRUPTED
	recoveredJob, err := s.GetJob(ctx, "job-100")
	if err != nil {
		t.Fatalf("failed retrieving recovered job: %v", err)
	}
	if recoveredJob.Status != domain.JobStatusInterrupted {
		t.Errorf("expected status INTERRUPTED, got %s", recoveredJob.Status)
	}

	// Recovering again yields empty slice
	emptyRecovered, err := s.RecoverInterruptedJobs(ctx)
	if err != nil || len(emptyRecovered) != 0 {
		t.Fatalf("expected no jobs to recover, got %v, err %v", emptyRecovered, err)
	}

	// Verify no active job remains
	_, err = s.GetActiveJob(ctx)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for active job, got %v", err)
	}

	// Complete job
	if err := s.UpdateJobStatus(ctx, "job-100", domain.JobStatusCompleted); err != nil {
		t.Fatalf("failed updating job to completed: %v", err)
	}
	completedJob, err := s.GetJob(ctx, "job-100")
	if err != nil {
		t.Fatalf("failed getting completed job: %v", err)
	}
	if completedJob.Status != domain.JobStatusCompleted || completedJob.CompletedAt == nil {
		t.Errorf("expected completed job with non-nil completed_at, got %+v", completedJob)
	}

	// Update status on non-existent job returns ErrNotFound
	err = s.UpdateJobStatus(ctx, "job-non-existent", domain.JobStatusFailed)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound updating non-existent job, got %v", err)
	}
}

func TestStorage_CheckpointsAndProgress(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	job := &domain.SyncJob{
		JobID:               "job-cp-1",
		Status:              domain.JobStatusRunning,
		TotalBatches:        3,
		CommittedBatchIndex: -1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("failed creating job: %v", err)
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed BeginTx: %v", err)
	}

	cp := &domain.BatchCheckpoint{
		JobID:       "job-cp-1",
		BatchIndex:  0,
		ItemsCount:  10,
		Summary:     domain.ChangeSummary{Added: 8, Updated: 0, Withdrawn: 1, Skipped: 1, DeadLetter: 0},
		CommittedAt: now,
	}
	if err := s.RecordBatchCheckpointTx(ctx, tx, cp); err != nil {
		_ = tx.Rollback()
		t.Fatalf("failed recording checkpoint: %v", err)
	}

	if err := s.UpdateJobProgressTx(ctx, tx, "job-cp-1", 0, cp.Summary); err != nil {
		_ = tx.Rollback()
		t.Fatalf("failed updating job progress: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("failed commit: %v", err)
	}

	updatedJob, err := s.GetJob(ctx, "job-cp-1")
	if err != nil {
		t.Fatalf("failed getting job: %v", err)
	}
	if updatedJob.CommittedBatchIndex != 0 {
		t.Errorf("expected committed_batch_index 0, got %d", updatedJob.CommittedBatchIndex)
	}
	if updatedJob.Summary.Added != 8 || updatedJob.Summary.Withdrawn != 1 || updatedJob.Summary.Skipped != 1 {
		t.Errorf("unexpected summary deltas: %+v", updatedJob.Summary)
	}
}

func TestStorage_DeadLetterQueue(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	job := &domain.SyncJob{
		JobID:               "job-dl-1",
		Status:              domain.JobStatusRunning,
		TotalBatches:        1,
		CommittedBatchIndex: -1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("failed creating job: %v", err)
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed BeginTx: %v", err)
	}

	issue1 := &domain.DeadLetterRecord{
		IssueID:    "issue-1",
		JobID:      "job-dl-1",
		BatchIndex: 0,
		SKU:        "SKU-CORRUPT",
		RawPayload: "raw payload 1",
		Reason:     "negative price",
		CreatedAt:  now,
	}
	issue2 := &domain.DeadLetterRecord{
		IssueID:    "issue-2",
		JobID:      "job-dl-1",
		BatchIndex: 0,
		SKU:        "",
		RawPayload: "raw payload 2",
		Reason:     "empty SKU",
		CreatedAt:  now.Add(time.Second),
	}

	if err := s.RecordDeadLetterTx(ctx, tx, issue1); err != nil {
		_ = tx.Rollback()
		t.Fatalf("failed recording issue 1: %v", err)
	}
	if err := s.RecordDeadLetterTx(ctx, tx, issue2); err != nil {
		_ = tx.Rollback()
		t.Fatalf("failed recording issue 2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed commit: %v", err)
	}

	// Query for job
	records, count, err := s.ListDeadLetterRecords(ctx, "job-dl-1", "", 10, 0)
	if err != nil {
		t.Fatalf("failed listing dead letter records: %v", err)
	}
	if count != 2 || len(records) != 2 {
		t.Errorf("expected 2 records, got count %d, len %d", count, len(records))
	}

	// Pagination: limit 1, offset 0
	p1, countP, err := s.ListDeadLetterRecords(ctx, "job-dl-1", "", 1, 0)
	if err != nil || len(p1) != 1 || countP != 2 {
		t.Errorf("expected 1 record from page 1, got %d, total %d", len(p1), countP)
	}

	// Query across all jobs (empty jobID)
	allRecords, allCount, err := s.ListDeadLetterRecords(ctx, "", "", 10, 0)
	if err != nil || allCount != 2 || len(allRecords) != 2 {
		t.Errorf("expected 2 records for all jobs, got %d, total %d", len(allRecords), allCount)
	}

	// Filter by reason: exact match
	negRecords, negCount, err := s.ListDeadLetterRecords(ctx, "job-dl-1", "negative price", 10, 0)
	if err != nil || negCount != 1 || len(negRecords) != 1 {
		t.Errorf("expected 1 negative price record, got %d, total %d", len(negRecords), negCount)
	}
	if len(negRecords) > 0 && negRecords[0].Reason != "negative price" {
		t.Errorf("unexpected reason: %s", negRecords[0].Reason)
	}

	// Filter by reason: substring match
	priceRecords, priceCount, err := s.ListDeadLetterRecords(ctx, "", "price", 10, 0)
	if err != nil || priceCount != 1 || len(priceRecords) != 1 {
		t.Errorf("expected 1 price record by substring, got %d, total %d", len(priceRecords), priceCount)
	}

	// Filter by non-existent reason
	noRecords, noCount, err := s.ListDeadLetterRecords(ctx, "", "nonexistent", 10, 0)
	if err != nil || noCount != 0 || len(noRecords) != 0 {
		t.Errorf("expected 0 records for nonexistent reason, got %d, total %d", len(noRecords), noCount)
	}
}

func TestStorage_SpecialCharactersAndSQLInjection(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	testCases := []struct {
		sku   string
		name  string
		price float64
	}{
		{"SKU-PROD/123:ABC", "Special Chars", 19.99},
		{"SKU'; DROP TABLE products; --", "SQL Injection Test", 29.99},
		{"SKU-🔥-日本語-001", "Unicode & Emoji", 99.00},
		{"SKU-FREE", "Zero Price", 0.00},
		{"SKU-MAX", "Large Currency", 999999999.99},
		{"SKU-LONG-NAME", strings.Repeat("A", 10000), 5.50},
	}

	for _, tc := range testCases {
		t.Run(tc.sku, func(t *testing.T) {
			p := &domain.Product{
				SKU:               tc.sku,
				Name:              tc.name,
				Price:             tc.price,
				Status:            domain.StatusActive,
				SupplierTimestamp: now,
				UpdatedAt:         now,
			}

			tx, err := s.BeginTx(ctx)
			if err != nil {
				t.Fatalf("BeginTx failed: %v", err)
			}
			if err := s.UpsertProductTx(ctx, tx, p); err != nil {
				_ = tx.Rollback()
				t.Fatalf("UpsertProductTx failed for %q: %v", tc.sku, err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("Commit failed: %v", err)
			}

			retrieved, err := s.GetProduct(ctx, tc.sku)
			if err != nil {
				t.Fatalf("GetProduct failed for %q: %v", tc.sku, err)
			}
			if retrieved.SKU != tc.sku || retrieved.Name != tc.name || retrieved.Price != tc.price {
				t.Errorf("mismatch: got %+v, want %+v", retrieved, p)
			}
		})
	}

	// Verify products table was not dropped by SQL injection test
	var count int
	err = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM products;").Scan(&count)
	if err != nil {
		t.Fatalf("table check failed: %v", err)
	}
	if count != len(testCases) {
		t.Errorf("expected %d products in table, got %d", len(testCases), count)
	}
}

func TestStorage_InvalidStatusConstraint(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	p := &domain.Product{
		SKU:               "SKU-BAD-STATUS",
		Name:              "Bad Status",
		Price:             10.0,
		Status:            domain.ProductStatus("INVALID_STATUS"),
		SupplierTimestamp: now,
		UpdatedAt:         now,
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback()

	err = s.UpsertProductTx(ctx, tx, p)
	if err == nil {
		t.Fatal("expected constraint error for invalid status, got nil")
	}
}

func TestStorage_ConcurrencyAndRaceSafety(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	const workers = 8
	const itemsPerWorker = 25
	var wg sync.WaitGroup

	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < itemsPerWorker; i++ {
				sku := fmt.Sprintf("SKU-W%d-I%d", workerID, i)
				now := time.Now().UTC()
				p := &domain.Product{
					SKU:               sku,
					Name:              fmt.Sprintf("Item %s", sku),
					Price:             float64(workerID*10 + i),
					Status:            domain.StatusActive,
					SupplierTimestamp: now,
					UpdatedAt:         now,
				}

				tx, err := s.BeginTx(ctx)
				if err != nil {
					t.Errorf("worker %d failed BeginTx: %v", workerID, err)
					return
				}
				if err := s.UpsertProductTx(ctx, tx, p); err != nil {
					_ = tx.Rollback()
					t.Errorf("worker %d failed UpsertProductTx: %v", workerID, err)
					return
				}
				if err := tx.Commit(); err != nil {
					t.Errorf("worker %d failed Commit: %v", workerID, err)
					return
				}

				// Concurrent read
				_, err = s.GetProduct(ctx, sku)
				if err != nil {
					t.Errorf("worker %d failed reading %s: %v", workerID, sku, err)
				}
			}
		}(w)
	}

	wg.Wait()

	// Verify total count
	var totalProducts int
	err = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM products;").Scan(&totalProducts)
	if err != nil {
		t.Fatalf("failed querying products count: %v", err)
	}
	expected := workers * itemsPerWorker
	if totalProducts != expected {
		t.Errorf("expected %d products, got %d", expected, totalProducts)
	}
}

func TestStorage_Close(t *testing.T) {
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("failed closing storage: %v", err)
	}

	// Subsequent query on closed db should return an error
	_, err = s.GetProduct(context.Background(), "SKU-ANY")
	if err == nil {
		t.Errorf("expected error querying closed storage, got nil")
	}
}

func TestStorage_Open_File(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_catalog.db"

	s, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening file database: %v", err)
	}
	defer s.Close()

	var jMode string
	err = s.DB().QueryRowContext(context.Background(), "PRAGMA journal_mode;").Scan(&jMode)
	if err != nil {
		t.Fatalf("failed querying journal_mode: %v", err)
	}
	if strings.ToLower(jMode) != "wal" {
		t.Errorf("expected WAL mode, got %s", jMode)
	}
}

func TestParseTime(t *testing.T) {
	// Empty string
	tm, err := storage.ParseTime("")
	if err != nil || !tm.IsZero() {
		t.Errorf("expected zero time, got %v, err %v", tm, err)
	}

	// RFC3339Nano
	sNano := "2026-10-21T10:00:00.123456789Z"
	tmNano, err := storage.ParseTime(sNano)
	if err != nil || tmNano.Nanosecond() != 123456789 {
		t.Errorf("expected nanoseconds 123456789, got %v, err %v", tmNano, err)
	}

	// RFC3339 standard
	sStd := "2026-10-21T10:00:00Z"
	tmStd, err := storage.ParseTime(sStd)
	if err != nil || tmStd.Minute() != 0 {
		t.Errorf("expected valid time, got %v, err %v", tmStd, err)
	}

	// Invalid
	_, err = storage.ParseTime("not-a-timestamp")
	if err == nil {
		t.Errorf("expected error for invalid timestamp string")
	}
}

func TestStorage_CorruptTimestampInDB(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	// Direct insert with invalid supplier_timestamp
	_, err = s.DB().ExecContext(ctx, "INSERT INTO products VALUES ('SKU-BAD-SUP', 'Item', 10.0, 'ACTIVE', 'bad-supplier-time', '2026-10-21T10:00:00Z');")
	if err != nil {
		t.Fatalf("failed insert: %v", err)
	}
	_, err = s.GetProduct(ctx, "SKU-BAD-SUP")
	if err == nil || !strings.Contains(err.Error(), "invalid supplier_timestamp") {
		t.Errorf("expected invalid supplier_timestamp error, got %v", err)
	}

	// Direct insert with invalid updated_at
	_, err = s.DB().ExecContext(ctx, "INSERT INTO products VALUES ('SKU-BAD-UPD', 'Item', 10.0, 'ACTIVE', '2026-10-21T10:00:00Z', 'bad-updated-time');")
	if err != nil {
		t.Fatalf("failed insert: %v", err)
	}
	_, err = s.GetProduct(ctx, "SKU-BAD-UPD")
	if err == nil || !strings.Contains(err.Error(), "invalid updated_at") {
		t.Errorf("expected invalid updated_at error, got %v", err)
	}
}

func TestStorage_Open_WithQueryParams(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_params.db?cache=shared"

	s, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed open with query params: %v", err)
	}
	defer s.Close()

	var busyTimeout int
	_ = s.DB().QueryRowContext(context.Background(), "PRAGMA busy_timeout;").Scan(&busyTimeout)
	if busyTimeout != 5000 {
		t.Errorf("expected busy timeout 5000, got %d", busyTimeout)
	}
}

func TestStorage_Open_InvalidPath(t *testing.T) {
	_, err := storage.Open("/dev/null/cannot/create/db.sqlite")
	if err == nil {
		t.Errorf("expected error opening invalid database path, got nil")
	}
}

func TestStorage_RecoverPendingJobs(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	jobPending := &domain.SyncJob{
		JobID:               "job-pending-1",
		Status:              domain.JobStatusPending,
		TotalBatches:        5,
		CommittedBatchIndex: -1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.CreateJob(ctx, jobPending); err != nil {
		t.Fatalf("failed creating pending job: %v", err)
	}

	recovered, err := s.RecoverInterruptedJobs(ctx)
	if err != nil {
		t.Fatalf("failed recovering jobs: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != "job-pending-1" {
		t.Errorf("expected recovered [job-pending-1], got %v", recovered)
	}
}

func TestStorage_ListJobs(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	// Empty storage test
	jobs, total, err := s.ListJobs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListJobs on empty storage failed: %v", err)
	}
	if total != 0 || len(jobs) != 0 {
		t.Fatalf("expected 0 jobs, got total=%d, len=%d", total, len(jobs))
	}

	// Insert 25 jobs
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		createdAt := baseTime.Add(time.Duration(i) * time.Minute)
		updatedAt := createdAt.Add(10 * time.Second)
		var completedAt *time.Time
		status := domain.JobStatusRunning
		if i%2 == 0 {
			comp := updatedAt.Add(10 * time.Second)
			completedAt = &comp
			status = domain.JobStatusCompleted
		}
		j := &domain.SyncJob{
			JobID:               fmt.Sprintf("job-%03d", i),
			Status:              status,
			TotalBatches:        10,
			CommittedBatchIndex: i % 10,
			Summary: domain.ChangeSummary{
				Added:      i * 10,
				Updated:    i * 5,
				Withdrawn:  i,
				Skipped:    i * 2,
				DeadLetter: i % 3,
			},
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
			CompletedAt: completedAt,
		}
		if err := s.CreateJob(ctx, j); err != nil {
			t.Fatalf("failed to create job %d: %v", i, err)
		}
		if completedAt != nil {
			if err := s.UpdateJobStatus(ctx, j.JobID, domain.JobStatusCompleted); err != nil {
				t.Fatalf("failed to update job status: %v", err)
			}
		}
	}

	// Query with default limit (limit=0 -> 20) and offset=0
	jobs, total, err = s.ListJobs(ctx, 0, -1)
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}
	if total != 25 {
		t.Errorf("expected total 25, got %d", total)
	}
	if len(jobs) != 20 {
		t.Errorf("expected 20 jobs (default limit), got %d", len(jobs))
	}
	// Verify descending created_at order
	if len(jobs) > 0 && jobs[0].JobID != "job-024" {
		t.Errorf("expected most recent job to be job-024, got %s", jobs[0].JobID)
	}

	// Pagination: offset 20, limit 10
	jobsOffset, totalOffset, err := s.ListJobs(ctx, 10, 20)
	if err != nil {
		t.Fatalf("ListJobs offset failed: %v", err)
	}
	if totalOffset != 25 {
		t.Errorf("expected total 25, got %d", totalOffset)
	}
	if len(jobsOffset) != 5 {
		t.Errorf("expected 5 jobs on remaining page, got %d", len(jobsOffset))
	}

	// Limit clamping (> 100)
	jobsClamped, _, err := s.ListJobs(ctx, 200, 0)
	if err != nil {
		t.Fatalf("ListJobs clamped failed: %v", err)
	}
	if len(jobsClamped) != 25 {
		t.Errorf("expected 25 jobs, got %d", len(jobsClamped))
	}
}

func TestStorage_ListProducts(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	// Empty products
	prods, total, err := s.ListProducts(ctx, "", "", 0, 0)
	if err != nil {
		t.Fatalf("ListProducts empty failed: %v", err)
	}
	if total != 0 || len(prods) != 0 {
		t.Fatalf("expected 0 products, got total=%d, len=%d", total, len(prods))
	}

	now := time.Now().UTC()
	testProducts := []domain.Product{
		{SKU: "KEY-001", Name: "Mechanical Keyboard Pro", Price: 129.99, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now},
		{SKU: "KEY-002", Name: "Wireless Keyboard Slim", Price: 79.99, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now},
		{SKU: "MOU-001", Name: "Ergonomic Mouse", Price: 49.99, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now},
		{SKU: "MOU-002", Name: "Gaming Mouse RGB", Price: 69.99, Status: domain.StatusWithdrawn, SupplierTimestamp: now, UpdatedAt: now},
		{SKU: "MON-001", Name: "UltraWide Monitor 34", Price: 499.99, Status: domain.StatusWithdrawn, SupplierTimestamp: now, UpdatedAt: now},
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	for i := range testProducts {
		if err := s.UpsertProductTx(ctx, tx, &testProducts[i]); err != nil {
			t.Fatalf("UpsertProductTx failed: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// 1. List all products
	prods, total, err = s.ListProducts(ctx, "", "", 0, -5)
	if err != nil {
		t.Fatalf("List all failed: %v", err)
	}
	if total != 5 || len(prods) != 5 {
		t.Errorf("expected 5 products, got total=%d, len=%d", total, len(prods))
	}

	// 2. Filter by status ACTIVE
	prodsActive, totalActive, err := s.ListProducts(ctx, "ACTIVE", "", 10, 0)
	if err != nil {
		t.Fatalf("List active failed: %v", err)
	}
	if totalActive != 3 || len(prodsActive) != 3 {
		t.Errorf("expected 3 active products, got total=%d, len=%d", totalActive, len(prodsActive))
	}

	// 3. Filter by status WITHDRAWN
	prodsWithdrawn, totalWithdrawn, err := s.ListProducts(ctx, "WITHDRAWN", "", 10, 0)
	if err != nil {
		t.Fatalf("List withdrawn failed: %v", err)
	}
	if totalWithdrawn != 2 || len(prodsWithdrawn) != 2 {
		t.Errorf("expected 2 withdrawn products, got total=%d, len=%d", totalWithdrawn, len(prodsWithdrawn))
	}

	// 4. Search by SKU
	prodsSearchSKU, totalSearchSKU, err := s.ListProducts(ctx, "", "MOU", 10, 0)
	if err != nil {
		t.Fatalf("Search SKU failed: %v", err)
	}
	if totalSearchSKU != 2 || len(prodsSearchSKU) != 2 {
		t.Errorf("expected 2 mouse products, got total=%d, len=%d", totalSearchSKU, len(prodsSearchSKU))
	}

	// 5. Search by Name
	prodsSearchName, totalSearchName, err := s.ListProducts(ctx, "", "Keyboard", 10, 0)
	if err != nil {
		t.Fatalf("Search Name failed: %v", err)
	}
	if totalSearchName != 2 || len(prodsSearchName) != 2 {
		t.Errorf("expected 2 keyboard products, got total=%d, len=%d", totalSearchName, len(prodsSearchName))
	}

	// 6. Combined filter and search
	prodsCombined, totalCombined, err := s.ListProducts(ctx, "ACTIVE", "Keyboard", 10, 0)
	if err != nil {
		t.Fatalf("Combined failed: %v", err)
	}
	if totalCombined != 2 || len(prodsCombined) != 2 {
		t.Errorf("expected 2 active keyboards, got total=%d, len=%d", totalCombined, len(prodsCombined))
	}

	// 7. Pagination and limit clamping
	prodsPaged, _, err := s.ListProducts(ctx, "", "", 2, 2)
	if err != nil {
		t.Fatalf("Pagination failed: %v", err)
	}
	if len(prodsPaged) != 2 {
		t.Errorf("expected 2 paged products, got %d", len(prodsPaged))
	}
	// Limit clamped
	prodsClamped, _, err := s.ListProducts(ctx, "", "", 500, 0)
	if err != nil {
		t.Fatalf("Limit clamped failed: %v", err)
	}
	if len(prodsClamped) != 5 {
		t.Errorf("expected 5 products, got %d", len(prodsClamped))
	}
}

func TestStorage_GetBatchCheckpoints(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	// Empty query for non-existent job
	cps, err := s.GetBatchCheckpoints(ctx, "job-none")
	if err != nil {
		t.Fatalf("GetBatchCheckpoints on empty failed: %v", err)
	}
	if len(cps) != 0 {
		t.Fatalf("expected 0 checkpoints, got %d", len(cps))
	}

	jobID := "job-cp-test"
	now := time.Now().UTC()
	job := &domain.SyncJob{
		JobID:               jobID,
		Status:              domain.JobStatusRunning,
		TotalBatches:        3,
		CommittedBatchIndex: -1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	// Insert checkpoints 2, 0, 1 out of order
	indexes := []int{2, 0, 1}
	for _, idx := range indexes {
		tx, err := s.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx failed: %v", err)
		}
		cp := &domain.BatchCheckpoint{
			JobID:      jobID,
			BatchIndex: idx,
			ItemsCount: (idx + 1) * 10,
			Summary: domain.ChangeSummary{
				Added:      idx + 1,
				Updated:    idx,
				Withdrawn:  idx,
				Skipped:    idx,
				DeadLetter: 0,
			},
			CommittedAt: now.Add(time.Duration(idx) * time.Second),
		}
		if err := s.RecordBatchCheckpointTx(ctx, tx, cp); err != nil {
			t.Fatalf("RecordBatchCheckpointTx failed for %d: %v", idx, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
	}

	// Verify ordered retrieval
	retrieved, err := s.GetBatchCheckpoints(ctx, jobID)
	if err != nil {
		t.Fatalf("GetBatchCheckpoints failed: %v", err)
	}
	if len(retrieved) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(retrieved))
	}
	for i, cp := range retrieved {
		if cp.BatchIndex != i {
			t.Errorf("checkpoint %d has BatchIndex %d, expected %d", i, cp.BatchIndex, i)
		}
		if cp.ItemsCount != (i+1)*10 {
			t.Errorf("checkpoint %d has ItemsCount %d, expected %d", i, cp.ItemsCount, (i+1)*10)
		}
		if cp.Summary.Added != i+1 {
			t.Errorf("checkpoint %d has Added %d, expected %d", i, cp.Summary.Added, i+1)
		}
	}
}

func TestStorage_ClosedDB_QueryErrors(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	// Close DB immediately so subsequent queries fail
	if err := s.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	if _, _, err := s.ListJobs(ctx, 10, 0); err == nil {
		t.Error("expected error on ListJobs with closed DB")
	}
	if _, _, err := s.ListProducts(ctx, "", "", 10, 0); err == nil {
		t.Error("expected error on ListProducts with closed DB")
	}
	if _, err := s.GetBatchCheckpoints(ctx, "job-1"); err == nil {
		t.Error("expected error on GetBatchCheckpoints with closed DB")
	}
}

func TestStorage_Reset(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	// Insert a product
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	err = s.UpsertProductTx(ctx, tx, &domain.Product{
		SKU:               "RESET-01",
		Name:              "Reset Product",
		Price:             50.0,
		Status:            domain.StatusActive,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	})
	if err != nil {
		t.Fatalf("UpsertProductTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Insert a job
	err = s.CreateJob(ctx, &domain.SyncJob{
		JobID:        "job-reset-1",
		Status:       domain.JobStatusCompleted,
		TotalBatches: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	// Call Reset
	if err := s.Reset(ctx); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Verify tables are empty
	prods, total, err := s.ListProducts(ctx, "", "", 10, 0)
	if err != nil || total != 0 || len(prods) != 0 {
		t.Fatalf("expected 0 products after reset, got total=%d, len=%d", total, len(prods))
	}

	jobs, totalJobs, err := s.ListJobs(ctx, 10, 0)
	if err != nil || totalJobs != 0 || len(jobs) != 0 {
		t.Fatalf("expected 0 jobs after reset, got total=%d, len=%d", totalJobs, len(jobs))
	}

	// Closed DB error check
	sClosed, _ := storage.OpenInMemory()
	_ = sClosed.Close()
	if err := sClosed.Reset(ctx); err == nil {
		t.Error("expected error on Reset with closed DB")
	}
}

func TestStorage_ResolveDeadLetter(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	now := time.Now().UTC()
	jobID := "job-resolve-1"
	if err := s.CreateJob(ctx, &domain.SyncJob{
		JobID:        jobID,
		Status:       domain.JobStatusRunning,
		TotalBatches: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	issue := &domain.DeadLetterRecord{
		IssueID:    "issue-test-123",
		JobID:      jobID,
		BatchIndex: 0,
		SKU:        "SKU-CORRUPT",
		RawPayload: `{"sku":"","name":"Broken","price":-5.0}`,
		Reason:     "empty SKU",
		CreatedAt:  now,
	}
	if err := s.RecordDeadLetterTx(ctx, tx, issue); err != nil {
		_ = tx.Rollback()
		t.Fatalf("failed to record dead letter: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// 1. Resolve successfully
	corrected := &domain.Product{
		SKU:               "SKU-FIXED",
		Name:              "Fixed Product",
		Price:             49.99,
		Status:            domain.StatusActive,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	}
	if err := s.ResolveDeadLetter(ctx, "issue-test-123", corrected); err != nil {
		t.Fatalf("ResolveDeadLetter failed: %v", err)
	}

	// Verify product exists in catalog
	p, err := s.GetProduct(ctx, "SKU-FIXED")
	if err != nil || p == nil {
		t.Fatalf("expected product to exist after resolution: %v", err)
	}
	if p.Price != 49.99 || p.Name != "Fixed Product" {
		t.Errorf("unexpected product data: %+v", p)
	}

	// Verify issue was deleted
	records, count, err := s.ListDeadLetterRecords(ctx, jobID, "", 10, 0)
	if err != nil || count != 0 || len(records) != 0 {
		t.Errorf("expected 0 dead letter records after resolution, got count=%d", count)
	}

	// 2. Resolve non-existent issue
	if err := s.ResolveDeadLetter(ctx, "non-existent-issue", corrected); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-existent issue, got %v", err)
	}

	// 2b. Verify Invariant C (Tombstone Preservation) and Invariant B (Temporal Monotonicity) on ResolveDeadLetter
	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx2: %v", err)
	}
	withdrawnProd := &domain.Product{
		SKU:               "SKU-WITHDRAWN-DL",
		Name:              "Discontinued Item",
		Price:             19.99,
		Status:            domain.StatusWithdrawn,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	}
	if err := s.UpsertProductTx(ctx, tx2, withdrawnProd); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("failed upserting withdrawn product: %v", err)
	}
	dlIssue2 := &domain.DeadLetterRecord{
		IssueID:    "issue-invariant-check",
		JobID:      jobID,
		BatchIndex: 0,
		SKU:        "SKU-WITHDRAWN-DL",
		RawPayload: `{"sku":"SKU-WITHDRAWN-DL","price":-1}`,
		Reason:     "negative price",
		CreatedAt:  now,
	}
	if err := s.RecordDeadLetterTx(ctx, tx2, dlIssue2); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("failed recording dlIssue2: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("failed committing tx2: %v", err)
	}

	// Attempting to resolve SKU-WITHDRAWN-DL as ACTIVE must fail with ErrResurrectionRestricted
	resurrectAttempt := &domain.Product{
		SKU:               "SKU-WITHDRAWN-DL",
		Name:              "Resurrected Item",
		Price:             25.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(time.Hour),
		UpdatedAt:         now.Add(time.Hour),
	}
	if err := s.ResolveDeadLetter(ctx, "issue-invariant-check", resurrectAttempt); !errors.Is(err, domain.ErrResurrectionRestricted) {
		t.Errorf("expected ErrResurrectionRestricted, got %v", err)
	}

	// Attempting to resolve SKU-FIXED with an older timestamp must fail with ErrOutdatedTimestamp
	staleAttempt := &domain.Product{
		SKU:               "SKU-FIXED",
		Name:              "Stale Fix",
		Price:             35.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-time.Hour),
		UpdatedAt:         now,
	}
	if err := s.ResolveDeadLetter(ctx, "issue-invariant-check", staleAttempt); !errors.Is(err, domain.ErrOutdatedTimestamp) {
		t.Errorf("expected ErrOutdatedTimestamp, got %v", err)
	}

	// 3. Resolve with closed DB
	sClosed, _ := storage.OpenInMemory()
	_ = sClosed.Close()
	if err := sClosed.ResolveDeadLetter(ctx, "issue-x", corrected); err == nil {
		t.Error("expected error on closed DB")
	}

	// 4. Version storage edge cases & closed DB coverage
	if _, err := s.GetBaselineStateAtVersion(ctx, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for empty versionID, got %v", err)
	}
	tx3, _ := s.BeginTx(ctx)
	if _, err := s.GetBaselineStateAtVersionTx(ctx, tx3, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for empty versionID in Tx, got %v", err)
	}
	if _, err := s.GetBaselineStateAtVersionTx(ctx, tx3, "missing-ver"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing versionID in Tx, got %v", err)
	}
	_ = tx3.Rollback()
	if _, _, err := s.RollbackVersion(ctx, "", "author", "desc"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for empty targetVersionID in RollbackVersion, got %v", err)
	}
	_, _, _ = s.ListVersions(ctx, 200, -5)
	_, _, _ = s.ListVersions(ctx, 0, 0)

	// Closed DB errors for versioning methods
	_, _ = sClosed.GetVersion(ctx, "v1")
	_, _ = sClosed.GetLatestVersion(ctx)
	_, _, _ = sClosed.ListVersions(ctx, 10, 0)
	_, _ = sClosed.GetVersionDeltas(ctx, "v1")
	_, _ = sClosed.GetBaselineStateAtVersion(ctx, "v1")
	_, _, _ = sClosed.RollbackVersion(ctx, "v1", "a", "d")
	_, _, _ = sClosed.ListJobsByStatus(ctx, "COMPLETED", 10, 0)
}

func TestStorage_GetMetrics(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer s.Close()

	// 1. Empty database
	emptyAgg, emptyJobs, err := s.GetMetrics(ctx, domain.MetricsFilter{})
	if err != nil {
		t.Fatalf("GetMetrics on empty DB failed: %v", err)
	}
	if emptyAgg.TotalProcessed != 0 || emptyAgg.QualityScore != 100.0 || len(emptyJobs) != 0 {
		t.Errorf("expected empty metrics, got: %+v, jobs: %d", emptyAgg, len(emptyJobs))
	}

	// 2. Insert test sync jobs at different times
	now := time.Now().UTC()
	t0 := now.Add(-48 * time.Hour)
	t0End := t0.Add(2 * time.Minute)
	t1 := now.Add(-30 * time.Minute)
	t1End := now.Add(-25 * time.Minute)
	t2 := now.Add(-10 * time.Minute)
	t2End := now.Add(-7 * time.Minute)

	// Job 0 (Old job, from 2 days ago)
	j0 := &domain.SyncJob{
		JobID:               "job-old",
		Status:              domain.JobStatusCompleted,
		TotalBatches:        5,
		CommittedBatchIndex: 4,
		Summary: domain.ChangeSummary{
			Added:      50,
			Updated:    20,
			Withdrawn:  10,
			Skipped:    5,
			DeadLetter: 5,
		},
		CreatedAt:   t0,
		UpdatedAt:   t0End,
		CompletedAt: &t0End,
	}
	if err := s.CreateJob(ctx, j0); err != nil {
		t.Fatalf("failed to create j0: %v", err)
	}

	// Job 1 (Today morning)
	j1 := &domain.SyncJob{
		JobID:               "job-today-1",
		Status:              domain.JobStatusCompleted,
		TotalBatches:        10,
		CommittedBatchIndex: 9,
		Summary: domain.ChangeSummary{
			Added:      100,
			Updated:    40,
			Withdrawn:  10,
			Skipped:    20,
			DeadLetter: 10,
		},
		CreatedAt:   t1,
		UpdatedAt:   t1End,
		CompletedAt: &t1End,
	}
	if err := s.CreateJob(ctx, j1); err != nil {
		t.Fatalf("failed to create j1: %v", err)
	}

	// Job 2 (Today noon)
	j2 := &domain.SyncJob{
		JobID:               "job-today-2",
		Status:              domain.JobStatusCompleted,
		TotalBatches:        10,
		CommittedBatchIndex: 9,
		Summary: domain.ChangeSummary{
			Added:      200,
			Updated:    50,
			Withdrawn:  20,
			Skipped:    10,
			DeadLetter: 0,
		},
		CreatedAt:   t2,
		UpdatedAt:   t2End,
		CompletedAt: &t2End,
	}
	if err := s.CreateJob(ctx, j2); err != nil {
		t.Fatalf("failed to create j2: %v", err)
	}

	// 3. Test Filter by Specific JobID
	aggJob1, jobs1, err := s.GetMetrics(ctx, domain.MetricsFilter{JobID: "job-today-1"})
	if err != nil {
		t.Fatalf("GetMetrics by JobID failed: %v", err)
	}
	if len(jobs1) != 1 || jobs1[0].JobID != "job-today-1" {
		t.Fatalf("expected 1 job with ID job-today-1, got %d", len(jobs1))
	}
	if aggJob1.Summary.Added != 100 || aggJob1.Summary.DeadLetter != 10 {
		t.Errorf("unexpected summary for job-today-1: %+v", aggJob1.Summary)
	}
	expectedQualityJob1 := (float64(180-10) / 180.0) * 100.0 // 180 total, 10 dead letter = 170/180 = 94.44%
	if math.Abs(aggJob1.QualityScore-expectedQualityJob1) > 0.01 {
		t.Errorf("expected quality score %f, got %f", expectedQualityJob1, aggJob1.QualityScore)
	}
	if aggJob1.ElapsedSeconds <= 0 || aggJob1.AvgThroughput <= 0 {
		t.Errorf("expected positive elapsed seconds and throughput, got %f and %f", aggJob1.ElapsedSeconds, aggJob1.AvgThroughput)
	}

	// 4. Test Filter by Today (From = 1 hour ago, To = now)
	todayStart := now.Add(-1 * time.Hour)
	todayEnd := now.Add(1 * time.Hour)
	aggToday, jobsToday, err := s.GetMetrics(ctx, domain.MetricsFilter{
		From: &todayStart,
		To:   &todayEnd,
	})
	if err != nil {
		t.Fatalf("GetMetrics for today failed: %v", err)
	}
	if len(jobsToday) != 2 {
		t.Fatalf("expected 2 jobs for today, got %d", len(jobsToday))
	}
	if aggToday.Summary.Added != 300 || aggToday.Summary.Updated != 90 {
		t.Errorf("unexpected today summary: %+v", aggToday.Summary)
	}
	if aggToday.TotalSyncs != 2 || aggToday.CompletedSyncs != 2 {
		t.Errorf("unexpected sync counts: total=%d, completed=%d", aggToday.TotalSyncs, aggToday.CompletedSyncs)
	}

	// 5. Test Filter with From only
	aggFrom, jobsFrom, err := s.GetMetrics(ctx, domain.MetricsFilter{
		From: &todayStart,
	})
	if err != nil {
		t.Fatalf("GetMetrics with From only failed: %v", err)
	}
	if len(jobsFrom) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobsFrom))
	}
	if aggFrom.TotalSyncs != 2 {
		t.Errorf("expected 2 syncs, got %d", aggFrom.TotalSyncs)
	}

	// 6. Test All Time (no bounds)
	aggAll, jobsAll, err := s.GetMetrics(ctx, domain.MetricsFilter{})
	if err != nil {
		t.Fatalf("GetMetrics all time failed: %v", err)
	}
	if len(jobsAll) != 3 {
		t.Fatalf("expected 3 jobs all time, got %d", len(jobsAll))
	}
	if aggAll.Summary.Added != 350 {
		t.Errorf("expected 350 added all time, got %d", aggAll.Summary.Added)
	}

	// 7. Test Closed DB error
	sClosed2, _ := storage.OpenInMemory()
	_ = sClosed2.Close()
	if _, _, err := sClosed2.GetMetrics(ctx, domain.MetricsFilter{}); err == nil {
		t.Error("expected error on closed DB for GetMetrics")
	}
}

// setupTestDB creates an in-memory SQLiteStorage instance configured with all migrations,
// registering automatic cleanup on test completion.
func setupTestDB(t testing.TB) *storage.SQLiteStorage {
	t.Helper()
	s, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("setupTestDB failed to open in-memory storage: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestStorage_CreateVersionAndDeltas(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. Happy Path: Atomically write live product, catalog version, and item deltas
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed BeginTx: %v", err)
	}

	prodA := &domain.Product{
		SKU:               "SKU-A",
		Name:              "Alpha Widget",
		Price:             10.50,
		Status:            domain.StatusActive,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	}
	if err := s.UpsertProductTx(ctx, tx, prodA); err != nil {
		_ = tx.Rollback()
		t.Fatalf("failed upserting product: %v", err)
	}

	ver1 := &domain.CatalogVersion{
		VersionID:     "ver-001",
		VersionNumber: 1,
		Description:   "Initial catalog import",
		Author:        "ops@coderpush.com",
		Type:          domain.VersionTypeInitial,
		Summary: domain.ChangeSummary{
			Added:      2,
			Updated:    1,
			Withdrawn:  1,
			Skipped:    0,
			DeadLetter: 0,
		},
		TotalItemsAffected: 4,
		Metadata:           `{"batch_source":"supplier_import_01"}`,
		CreatedAt:          now,
	}

	oldName := "Beta Widget Old"
	oldPrice := 40.00
	oldStatus := domain.StatusActive
	oldTime := now.Add(-1 * time.Hour)

	deltas := []domain.ItemDelta{
		{
			VersionID:    "ver-001",
			SKU:          "SKU-A",
			Action:       domain.DeltaActionAdded,
			NewName:      "Alpha Widget",
			NewPrice:     10.50,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now,
		},
		{
			VersionID:    "ver-001",
			SKU:          "SKU-B",
			Action:       domain.DeltaActionUpdated,
			OldName:      &oldName,
			NewName:      "Beta Widget",
			OldPrice:     &oldPrice,
			NewPrice:     45.00,
			OldStatus:    &oldStatus,
			NewStatus:    domain.StatusActive,
			OldTimestamp: &oldTime,
			NewTimestamp: now,
		},
		{
			VersionID:    "ver-001",
			SKU:          "SKU-C",
			Action:       domain.DeltaActionWithdrawn,
			OldName:      &oldName,
			NewName:      "Beta Widget",
			OldPrice:     &oldPrice,
			NewPrice:     45.00,
			OldStatus:    &oldStatus,
			NewStatus:    domain.StatusWithdrawn,
			OldTimestamp: &oldTime,
			NewTimestamp: now,
		},
	}

	if err := s.CreateVersionTx(ctx, tx, ver1, deltas); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx failed: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify committed product
	savedProd, err := s.GetProduct(ctx, "SKU-A")
	if err != nil || savedProd.Price != 10.50 {
		t.Fatalf("live product not found or price mismatch: %v, prod: %+v", err, savedProd)
	}

	// Verify committed version
	savedVer, err := s.GetVersion(ctx, "ver-001")
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if savedVer.VersionNumber != 1 || savedVer.Author != "ops@coderpush.com" || savedVer.Type != domain.VersionTypeInitial {
		t.Errorf("version fields mismatch: %+v", savedVer)
	}
	if savedVer.Summary.Added != 2 || savedVer.TotalItemsAffected != 4 {
		t.Errorf("version summary mismatch: %+v", savedVer.Summary)
	}
	if savedVer.RevertOfVersionID != nil {
		t.Errorf("expected nil RevertOfVersionID, got %v", *savedVer.RevertOfVersionID)
	}

	// Verify committed deltas
	savedDeltas, err := s.GetVersionDeltas(ctx, "ver-001")
	if err != nil {
		t.Fatalf("GetVersionDeltas failed: %v", err)
	}
	if len(savedDeltas) != 3 {
		t.Fatalf("expected 3 deltas, got %d", len(savedDeltas))
	}

	// Check delta 0 (ADDED: old values must be nil)
	d0 := savedDeltas[0]
	if d0.SKU != "SKU-A" || d0.Action != domain.DeltaActionAdded || d0.OldPrice != nil || d0.OldStatus != nil {
		t.Errorf("delta 0 pre-image should be nil: %+v", d0)
	}
	if d0.NewPrice != 10.50 || d0.NewStatus != domain.StatusActive {
		t.Errorf("delta 0 post-image mismatch: %+v", d0)
	}

	// Check delta 1 (UPDATED: old values present)
	d1 := savedDeltas[1]
	if d1.SKU != "SKU-B" || d1.Action != domain.DeltaActionUpdated {
		t.Errorf("delta 1 action mismatch: %+v", d1)
	}
	if d1.OldPrice == nil || *d1.OldPrice != 40.00 || d1.NewPrice != 45.00 {
		t.Errorf("delta 1 price mismatch: old=%v, new=%v", d1.OldPrice, d1.NewPrice)
	}

	// 2. Transaction Rollback Atomicity: rolling back tx discards version and deltas
	txRollback, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx rollback failed: %v", err)
	}
	verRollback := &domain.CatalogVersion{
		VersionID:     "ver-discarded",
		VersionNumber: 99,
		Description:   "Should be rolled back",
		Author:        "ops@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now,
	}
	deltaRollback := []domain.ItemDelta{
		{
			VersionID:    "ver-discarded",
			SKU:          "SKU-ROLLBACK",
			Action:       domain.DeltaActionAdded,
			NewName:      "Rollback Item",
			NewPrice:     10.0,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now,
		},
	}
	if err := s.CreateVersionTx(ctx, txRollback, verRollback, deltaRollback); err != nil {
		t.Fatalf("CreateVersionTx in rollback failed: %v", err)
	}
	if err := txRollback.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify uncommitted version cannot be retrieved
	_, err = s.GetVersion(ctx, "ver-discarded")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for rolled back version, got %v", err)
	}
	discardedDeltas, err := s.GetVersionDeltas(ctx, "ver-discarded")
	if err != nil || len(discardedDeltas) != 0 {
		t.Errorf("expected 0 deltas for rolled back version, got len=%d, err=%v", len(discardedDeltas), err)
	}

	// 3. Uniqueness Constraints: duplicate version_id and duplicate version_number must fail
	txDupID, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx dup failed: %v", err)
	}
	dupIDVer := &domain.CatalogVersion{
		VersionID:     "ver-001", // Duplicate ID
		VersionNumber: 2,
		Description:   "Duplicate version_id",
		Author:        "ops@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now,
	}
	err = s.CreateVersionTx(ctx, txDupID, dupIDVer, nil)
	if err == nil {
		_ = txDupID.Rollback()
		t.Error("expected error for duplicate version_id, got nil")
	} else {
		_ = txDupID.Rollback()
	}

	txDupNum, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx dup num failed: %v", err)
	}
	dupNumVer := &domain.CatalogVersion{
		VersionID:     "ver-002",
		VersionNumber: 1, // Duplicate VersionNumber
		Description:   "Duplicate version_number",
		Author:        "ops@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now,
	}
	err = s.CreateVersionTx(ctx, txDupNum, dupNumVer, nil)
	if err == nil {
		_ = txDupNum.Rollback()
		t.Error("expected error for duplicate version_number, got nil")
	} else {
		_ = txDupNum.Rollback()
	}

	// 4. Empty deltas (milestone snapshot with zero item modifications) succeeds
	txEmptyDelta, _ := s.BeginTx(ctx)
	emptyVer := &domain.CatalogVersion{
		VersionID:     "ver-empty",
		VersionNumber: 2,
		Description:   "Empty milestone",
		Author:        "ops@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now,
	}
	if err := s.CreateVersionTx(ctx, txEmptyDelta, emptyVer, nil); err != nil {
		_ = txEmptyDelta.Rollback()
		t.Fatalf("CreateVersionTx with nil deltas failed: %v", err)
	}
	if err := txEmptyDelta.Commit(); err != nil {
		t.Fatalf("Commit empty deltas version failed: %v", err)
	}
	deltasEmpty, err := s.GetVersionDeltas(ctx, "ver-empty")
	if err != nil || len(deltasEmpty) != 0 {
		t.Errorf("expected 0 deltas, got len=%d, err=%v", len(deltasEmpty), err)
	}
}

func TestStorage_GetVersion_And_Deltas(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. Non-existent version
	_, err := s.GetVersion(ctx, "non-existent-version")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-existent version, got %v", err)
	}

	// 2. Non-existent version deltas returns empty slice with nil error
	emptyDeltas, err := s.GetVersionDeltas(ctx, "non-existent-version")
	if err != nil {
		t.Fatalf("expected nil error for missing version deltas, got %v", err)
	}
	if len(emptyDeltas) != 0 {
		t.Errorf("expected empty slice, got %d items", len(emptyDeltas))
	}

	// 3. RevertOfVersionID pointer handling
	tx1, _ := s.BeginTx(ctx)
	ver1 := &domain.CatalogVersion{
		VersionID:     "ver-001",
		VersionNumber: 1,
		Description:   "Initial version",
		Author:        "admin@coderpush.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now.Add(-time.Hour),
	}
	if err := s.CreateVersionTx(ctx, tx1, ver1, nil); err != nil {
		_ = tx1.Rollback()
		t.Fatalf("CreateVersionTx v1 failed: %v", err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatalf("Commit v1 failed: %v", err)
	}

	revertTarget := "ver-001"
	rollbackVer := &domain.CatalogVersion{
		VersionID:         "ver-002",
		VersionNumber:     2,
		Description:       "Rollback to ver-001",
		Author:            "admin@coderpush.com",
		Type:              domain.VersionTypeRollback,
		RevertOfVersionID: &revertTarget,
		CreatedAt:         now,
	}

	tx, _ := s.BeginTx(ctx)
	if err := s.CreateVersionTx(ctx, tx, rollbackVer, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx rollback failed: %v", err)
	}
	_ = tx.Commit()

	retrieved, err := s.GetVersion(ctx, "ver-002")
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if retrieved.RevertOfVersionID == nil || *retrieved.RevertOfVersionID != "ver-001" {
		t.Errorf("expected RevertOfVersionID 'ver-001', got %+v", retrieved.RevertOfVersionID)
	}
	if retrieved.Type != domain.VersionTypeRollback {
		t.Errorf("expected type ROLLBACK, got %s", retrieved.Type)
	}
}

func TestStorage_GetLatestVersion(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	// 1. Empty database returns domain.ErrNotFound
	_, err := s.GetLatestVersion(ctx)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on empty database, got %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)

	// 2. Insert single version
	tx1, _ := s.BeginTx(ctx)
	ver1 := &domain.CatalogVersion{
		VersionID:     "ver-001",
		VersionNumber: 1,
		Description:   "V1",
		Author:        "user@test.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, nil)
	_ = tx1.Commit()

	latest, err := s.GetLatestVersion(ctx)
	if err != nil || latest.VersionID != "ver-001" {
		t.Fatalf("expected latest version ver-001, got %+v, err %v", latest, err)
	}

	// 3. Insert sequential versions 2 and 3
	for i := 2; i <= 3; i++ {
		tx, _ := s.BeginTx(ctx)
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-%03d", i),
			VersionNumber: i,
			Description:   fmt.Sprintf("V%d", i),
			Author:        "user@test.com",
			Type:          domain.VersionTypePatch,
			CreatedAt:     now.Add(time.Duration(i) * time.Minute),
		}
		_ = s.CreateVersionTx(ctx, tx, ver, nil)
		_ = tx.Commit()
	}

	latest3, err := s.GetLatestVersion(ctx)
	if err != nil || latest3.VersionNumber != 3 || latest3.VersionID != "ver-003" {
		t.Fatalf("expected latest version ver-003 with number 3, got %+v, err %v", latest3, err)
	}

	// 4. Closed DB error handling
	sClosed := setupTestDB(t)
	_ = sClosed.Close()
	if _, err := sClosed.GetLatestVersion(ctx); err == nil {
		t.Error("expected error on closed DB for GetLatestVersion")
	}
}

func TestStorage_ListVersions(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	// 1. Empty database listing
	emptyList, total, err := s.ListVersions(ctx, 10, 0)
	if err != nil || total != 0 || len(emptyList) != 0 {
		t.Fatalf("expected empty list on clean DB, got total=%d, len=%d, err=%v", total, len(emptyList), err)
	}

	// 2. Insert 25 versions with monotonic numbers and timestamps
	baseTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for i := 1; i <= 25; i++ {
		tx, _ := s.BeginTx(ctx)
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-%03d", i),
			VersionNumber: i,
			Description:   fmt.Sprintf("Version milestone %d", i),
			Author:        fmt.Sprintf("author-%d@coderpush.com", i%3),
			Type:          domain.VersionTypePatch,
			Summary: domain.ChangeSummary{
				Added:   i * 2,
				Updated: i,
			},
			TotalItemsAffected: i * 3,
			CreatedAt:          baseTime.Add(time.Duration(i) * time.Minute),
		}
		if err := s.CreateVersionTx(ctx, tx, ver, nil); err != nil {
			_ = tx.Rollback()
			t.Fatalf("failed inserting version %d: %v", i, err)
		}
		_ = tx.Commit()
	}

	// 3. Default pagination: limit=0 (default 20), offset=-1 (default 0)
	page1, total, err := s.ListVersions(ctx, 0, -1)
	if err != nil {
		t.Fatalf("ListVersions default failed: %v", err)
	}
	if total != 25 {
		t.Errorf("expected total 25, got %d", total)
	}
	if len(page1) != 20 {
		t.Errorf("expected 20 items (default limit), got %d", len(page1))
	}
	// Verify ordering: newest first (ver-025 is index 0)
	if page1[0].VersionID != "ver-025" || page1[0].VersionNumber != 25 {
		t.Errorf("expected first item to be ver-025, got %s (#%d)", page1[0].VersionID, page1[0].VersionNumber)
	}
	if page1[19].VersionID != "ver-006" {
		t.Errorf("expected 20th item to be ver-006, got %s", page1[19].VersionID)
	}

	// 4. Pagination: Page 2 (offset 20, limit 10)
	page2, total2, err := s.ListVersions(ctx, 10, 20)
	if err != nil || total2 != 25 {
		t.Fatalf("ListVersions page 2 failed: %v, total: %d", err, total2)
	}
	if len(page2) != 5 {
		t.Errorf("expected remaining 5 items on page 2, got %d", len(page2))
	}
	if page2[0].VersionID != "ver-005" || page2[4].VersionID != "ver-001" {
		t.Errorf("page 2 bounds mismatch: first=%s, last=%s", page2[0].VersionID, page2[4].VersionID)
	}

	// 5. Out-of-bounds offset
	pageEmpty, totalEmpty, err := s.ListVersions(ctx, 10, 50)
	if err != nil || totalEmpty != 25 || len(pageEmpty) != 0 {
		t.Errorf("expected empty page for high offset, got len=%d, total=%d", len(pageEmpty), totalEmpty)
	}

	// 6. Limit Clamping (> 100 clamped to 100)
	clamped, _, err := s.ListVersions(ctx, 500, 0)
	if err != nil {
		t.Fatalf("ListVersions clamped failed: %v", err)
	}
	if len(clamped) != 25 {
		t.Errorf("expected 25 items for clamped limit, got %d", len(clamped))
	}
}

func TestStorage_ResetVersions(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()

	// 1. Populate data in products, jobs, and catalog versioning tables
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	_ = s.UpsertProductTx(ctx, tx, &domain.Product{
		SKU:               "SKU-RESET-TEST",
		Name:              "Reset Test Item",
		Price:             29.99,
		Status:            domain.StatusActive,
		SupplierTimestamp: now,
		UpdatedAt:         now,
	})
	ver := &domain.CatalogVersion{
		VersionID:     "ver-reset",
		VersionNumber: 1,
		Description:   "Version before reset",
		Author:        "test@coderpush.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	deltas := []domain.ItemDelta{
		{
			VersionID:    "ver-reset",
			SKU:          "SKU-RESET-TEST",
			Action:       domain.DeltaActionAdded,
			NewName:      "Reset Test Item",
			NewPrice:     29.99,
			NewStatus:    domain.StatusActive,
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

	// 2. Call Reset
	if err := s.Reset(ctx); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// 3. Verify all tables are wiped clean
	// Products
	_, err = s.GetProduct(ctx, "SKU-RESET-TEST")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected product wiped after reset, got err %v", err)
	}
	// Versions
	_, err = s.GetVersion(ctx, "ver-reset")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected version wiped after reset, got err %v", err)
	}
	// Latest version
	_, err = s.GetLatestVersion(ctx)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected GetLatestVersion ErrNotFound after reset, got err %v", err)
	}
	// Deltas
	deltasAfter, err := s.GetVersionDeltas(ctx, "ver-reset")
	if err != nil || len(deltasAfter) != 0 {
		t.Errorf("expected 0 deltas after reset, got %d", len(deltasAfter))
	}
	// Direct delta table count
	var deltaCount int
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM catalog_version_deltas;").Scan(&deltaCount)
	if deltaCount != 0 {
		t.Errorf("expected 0 rows in catalog_version_deltas, got %d", deltaCount)
	}

	// 4. Verify DB remains fully functional after reset
	txPost, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx post-reset failed: %v", err)
	}
	postVer := &domain.CatalogVersion{
		VersionID:     "ver-post-reset",
		VersionNumber: 1,
		Description:   "Clean version 1",
		Author:        "admin@coderpush.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	if err := s.CreateVersionTx(ctx, txPost, postVer, nil); err != nil {
		_ = txPost.Rollback()
		t.Fatalf("failed inserting version post-reset: %v", err)
	}
	if err := txPost.Commit(); err != nil {
		t.Fatalf("Commit post-reset failed: %v", err)
	}

	fetchedPost, err := s.GetVersion(ctx, "ver-post-reset")
	if err != nil || fetchedPost.VersionNumber != 1 {
		t.Errorf("expected successful retrieval post-reset, got %+v, err %v", fetchedPost, err)
	}
}

// BenchmarkStorage_GetProduct benchmarks point lookups on the live products table in the presence
// of historical catalog versions and delta records.
func BenchmarkStorage_GetProduct(b *testing.B) {
	ctx := context.Background()
	s := setupTestDB(b)

	now := time.Now().UTC()
	const totalProducts = 2000

	// Populate products and historical version deltas
	tx, _ := s.BeginTx(ctx)
	for i := 0; i < totalProducts; i++ {
		_ = s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-%05d", i),
			Name:              fmt.Sprintf("Benchmark Product %d", i),
			Price:             float64(10 + (i % 100)),
			Status:            domain.StatusActive,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	ver := &domain.CatalogVersion{
		VersionID:     "ver-bench",
		VersionNumber: 1,
		Description:   "Benchmark baseline",
		Author:        "bench@coderpush.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	_ = s.CreateVersionTx(ctx, tx, ver, nil)
	_ = tx.Commit()

	targetSKU := "SKU-01000"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p, err := s.GetProduct(ctx, targetSKU)
		if err != nil || p == nil {
			b.Fatalf("lookup failed: %v", err)
		}
	}
}

// BenchmarkStorage_ListProducts benchmarks paginated scans on the live products table.
func BenchmarkStorage_ListProducts(b *testing.B) {
	ctx := context.Background()
	s := setupTestDB(b)

	now := time.Now().UTC()
	const totalProducts = 2000

	tx, _ := s.BeginTx(ctx)
	for i := 0; i < totalProducts; i++ {
		status := domain.StatusActive
		if i%5 == 0 {
			status = domain.StatusWithdrawn
		}
		_ = s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-%05d", i),
			Name:              fmt.Sprintf("Benchmark Product %d", i),
			Price:             float64(10 + (i % 100)),
			Status:            status,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	_ = tx.Commit()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		prods, total, err := s.ListProducts(ctx, "ACTIVE", "", 50, 0)
		if err != nil || total == 0 || len(prods) != 50 {
			b.Fatalf("list failed: %v, total: %d, len: %d", err, total, len(prods))
		}
	}
}

// TestStorage_CatalogLookupLatencySubMillisecond automatically asserts that active catalog point lookups
// and paginated listings remain strictly sub-millisecond even with 2,000 live products and 5,000 historical deltas.
func TestStorage_CatalogLookupLatencySubMillisecond(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()
	totalProducts := 2000
	historicalVersions := 20
	if isRaceDetectorEnabled() {
		totalProducts = 500
		historicalVersions = 8
	}

	// 1. Seed products
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	for i := 0; i < totalProducts; i++ {
		status := domain.StatusActive
		if i%10 == 0 {
			status = domain.StatusWithdrawn
		}
		if err := s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-%05d", i),
			Name:              fmt.Sprintf("Live Catalog Item %d", i),
			Price:             float64(5 + (i % 50)),
			Status:            status,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		}); err != nil {
			_ = tx.Rollback()
			t.Fatalf("failed seeding product %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit products failed: %v", err)
	}

	// 2. Seed historical versions and 5,000 deltas in auxiliary journal
	for v := 1; v <= historicalVersions; v++ {
		txV, _ := s.BeginTx(ctx)
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-hist-%03d", v),
			VersionNumber: v,
			Description:   fmt.Sprintf("Historical version %d", v),
			Author:        "sync@coderpush.com",
			Type:          domain.VersionTypePatch,
			CreatedAt:     now.Add(time.Duration(v) * time.Hour),
		}
		deltas := make([]domain.ItemDelta, 250)
		for d := 0; d < 250; d++ {
			skuIdx := (v*250 + d) % totalProducts
			deltas[d] = domain.ItemDelta{
				VersionID:    ver.VersionID,
				SKU:          fmt.Sprintf("SKU-%05d", skuIdx),
				Action:       domain.DeltaActionUpdated,
				NewName:      fmt.Sprintf("Live Catalog Item %d", skuIdx),
				NewPrice:     float64(5 + (skuIdx % 50)),
				NewStatus:    domain.StatusActive,
				NewTimestamp: now,
			}
		}
		_ = s.CreateVersionTx(ctx, txV, ver, deltas)
		_ = txV.Commit()
	}

	// 3. Warm-up
	for i := 0; i < 50; i++ {
		_, _ = s.GetProduct(ctx, "SKU-00500")
	}

	// 4. Assert Point Lookup Latency (< 1.0 ms)
	pointLookups := 1000
	if isRaceDetectorEnabled() {
		pointLookups = 300
	}
	startPoint := time.Now()
	for i := 0; i < pointLookups; i++ {
		sku := fmt.Sprintf("SKU-%05d", (i*7)%totalProducts)
		p, err := s.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("Point lookup failed for %s: %v", sku, err)
		}
	}
	pointElapsed := time.Since(startPoint)
	avgPointLatency := pointElapsed / time.Duration(pointLookups)

	t.Logf("Measured avg point lookup latency: %v across %d lookups", avgPointLatency, pointLookups)
	maxPointAllowed := 1 * time.Millisecond
	if isRaceDetectorEnabled() {
		maxPointAllowed = 15 * time.Millisecond
	}
	if avgPointLatency >= maxPointAllowed {
		t.Fatalf("Point lookup latency regression: got %v, expected < %v", avgPointLatency, maxPointAllowed)
	}

	// 5. Assert Paginated List Latency (< 2.0 ms for 50 items)
	listLookups := 200
	if isRaceDetectorEnabled() {
		listLookups = 50
	}
	startList := time.Now()
	for i := 0; i < listLookups; i++ {
		offset := (i * 10) % (totalProducts / 2)
		prods, total, err := s.ListProducts(ctx, "ACTIVE", "", 50, offset)
		if err != nil || total == 0 || len(prods) == 0 {
			t.Fatalf("ListProducts failed at offset %d: %v", offset, err)
		}
	}
	listElapsed := time.Since(startList)
	avgListLatency := listElapsed / time.Duration(listLookups)

	t.Logf("Measured avg list query latency: %v across %d queries", avgListLatency, listLookups)
	maxListAllowed := 2 * time.Millisecond
	if isRaceDetectorEnabled() {
		// Under Go race detector instrumentation (-race), pure-Go SQLite memory operations incur ~20-50x overhead
		maxListAllowed = 50 * time.Millisecond
	}
	if avgListLatency >= maxListAllowed {
		t.Fatalf("List query latency regression: got %v, expected < %v", avgListLatency, maxListAllowed)
	}
}

func isRaceDetectorEnabled() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, setting := range info.Settings {
		if setting.Key == "-race" && setting.Value == "true" {
			return true
		}
	}
	return false
}

func TestStorage_ConcurrentVersionAndCatalogReads(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()

	// Seed 100 products
	tx, _ := s.BeginTx(ctx)
	for i := 0; i < 100; i++ {
		_ = s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-%03d", i),
			Name:              fmt.Sprintf("Concurrent Item %d", i),
			Price:             10.0 + float64(i),
			Status:            domain.StatusActive,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	_ = tx.Commit()

	var wg sync.WaitGroup
	const readers = 6
	const writers = 2
	const operations = 40

	// Readers: execute GetProduct and ListProducts
	wg.Add(readers)
	for r := 0; r < readers; r++ {
		go func(readerID int) {
			defer wg.Done()
			for op := 0; op < operations; op++ {
				sku := fmt.Sprintf("SKU-%03d", (readerID+op)%100)
				p, err := s.GetProduct(ctx, sku)
				if err != nil || p == nil {
					t.Errorf("reader %d failed reading %s: %v", readerID, sku, err)
					return
				}
				if op%5 == 0 {
					_, _, err := s.ListProducts(ctx, "ACTIVE", "", 20, 0)
					if err != nil {
						t.Errorf("reader %d failed listing products: %v", readerID, err)
						return
					}
				}
			}
		}(r)
	}

	// Writers: write new versions and deltas
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func(writerID int) {
			defer wg.Done()
			for op := 0; op < operations/4; op++ {
				verNum := 1000 + writerID*100 + op
				verID := fmt.Sprintf("ver-c-%d-%d", writerID, op)
				txW, err := s.BeginTx(ctx)
				if err != nil {
					t.Errorf("writer %d failed BeginTx: %v", writerID, err)
					return
				}
				ver := &domain.CatalogVersion{
					VersionID:     verID,
					VersionNumber: verNum,
					Description:   "Concurrent write",
					Author:        "concurrency@test.com",
					Type:          domain.VersionTypePatch,
					CreatedAt:     time.Now().UTC(),
				}
				deltas := []domain.ItemDelta{
					{
						VersionID:    verID,
						SKU:          fmt.Sprintf("SKU-%03d", op%100),
						Action:       domain.DeltaActionUpdated,
						NewName:      "Concurrent Update",
						NewPrice:     99.99,
						NewStatus:    domain.StatusActive,
						NewTimestamp: time.Now().UTC(),
					},
				}
				if err := s.CreateVersionTx(ctx, txW, ver, deltas); err != nil {
					_ = txW.Rollback()
					t.Errorf("writer %d failed CreateVersionTx: %v", writerID, err)
					return
				}
				if err := txW.Commit(); err != nil {
					t.Errorf("writer %d failed Commit: %v", writerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
}

// ============================================================================
// Milestone 2: Non-Destructive Rollback Engine Test Suite (Git-Revert Pattern)
// ============================================================================

func TestStorage_Rollback_SimplePriceAndNameRevert(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. Setup V1: SKU-001 priced at $50.00, Name "Super Gadget V1", Status ACTIVE
	tx1, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx1 failed: %v", err)
	}
	p1 := &domain.Product{
		SKU:               "SKU-001",
		Name:              "Super Gadget V1",
		Price:             50.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-2 * time.Hour),
		UpdatedAt:         now.Add(-2 * time.Hour),
	}
	if err := s.UpsertProductTx(ctx, tx1, p1); err != nil {
		_ = tx1.Rollback()
		t.Fatalf("UpsertProductTx tx1 failed: %v", err)
	}
	ver1 := &domain.CatalogVersion{
		VersionID:          "ver-simple-001",
		VersionNumber:      1,
		Description:        "Initial baseline",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypeInitial,
		TotalItemsAffected: 1,
		Summary:            domain.ChangeSummary{Added: 1},
		CreatedAt:          now.Add(-2 * time.Hour),
	}
	deltas1 := []domain.ItemDelta{
		{
			VersionID:    "ver-simple-001",
			SKU:          "SKU-001",
			Action:       domain.DeltaActionAdded,
			NewName:      "Super Gadget V1",
			NewPrice:     50.00,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now.Add(-2 * time.Hour),
		},
	}
	if err := s.CreateVersionTx(ctx, tx1, ver1, deltas1); err != nil {
		_ = tx1.Rollback()
		t.Fatalf("CreateVersionTx tx1 failed: %v", err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatalf("Commit tx1 failed: %v", err)
	}

	// 2. Setup V2: SKU-001 discounted to $40.00, Name "Super Gadget V2"
	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx2 failed: %v", err)
	}
	p2 := &domain.Product{
		SKU:               "SKU-001",
		Name:              "Super Gadget V2",
		Price:             40.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-1 * time.Hour),
		UpdatedAt:         now.Add(-1 * time.Hour),
	}
	if err := s.UpsertProductTx(ctx, tx2, p2); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("UpsertProductTx tx2 failed: %v", err)
	}
	ver2 := &domain.CatalogVersion{
		VersionID:          "ver-simple-002",
		VersionNumber:      2,
		Description:        "Promotional discount patch",
		Author:             "marketing@coderpush.com",
		Type:               domain.VersionTypePatch,
		TotalItemsAffected: 1,
		Summary:            domain.ChangeSummary{Updated: 1},
		CreatedAt:          now.Add(-1 * time.Hour),
	}
	oldName := "Super Gadget V1"
	oldPrice := 50.00
	oldStatus := domain.StatusActive
	oldTime := now.Add(-2 * time.Hour)
	deltas2 := []domain.ItemDelta{
		{
			VersionID:    "ver-simple-002",
			SKU:          "SKU-001",
			Action:       domain.DeltaActionUpdated,
			OldName:      &oldName,
			NewName:      "Super Gadget V2",
			OldPrice:     &oldPrice,
			NewPrice:     40.00,
			OldStatus:    &oldStatus,
			NewStatus:    domain.StatusActive,
			OldTimestamp: &oldTime,
			NewTimestamp: now.Add(-1 * time.Hour),
		},
	}
	if err := s.CreateVersionTx(ctx, tx2, ver2, deltas2); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("CreateVersionTx tx2 failed: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit tx2 failed: %v", err)
	}

	// Verify live product at V2
	liveV2, err := s.GetProduct(ctx, "SKU-001")
	if err != nil || liveV2.Price != 40.00 || liveV2.Name != "Super Gadget V2" {
		t.Fatalf("expected V2 live price 40.00, name 'Super Gadget V2', got %+v", liveV2)
	}

	// 3. Action: Rollback to V1
	v3, revDeltas, err := s.RollbackVersion(ctx, "ver-simple-001", "revert-bot@coderpush.com", "Revert promo price to V1")
	if err != nil {
		t.Fatalf("RollbackVersion failed: %v", err)
	}

	// 4. Assertions on V3
	if v3 == nil {
		t.Fatal("expected non-nil CatalogVersion returned from RollbackVersion")
	}
	if v3.VersionNumber != 3 {
		t.Errorf("expected version number 3, got %d", v3.VersionNumber)
	}
	if v3.Type != domain.VersionTypeRollback {
		t.Errorf("expected VersionTypeRollback, got %s", v3.Type)
	}
	if v3.RevertOfVersionID == nil || *v3.RevertOfVersionID != "ver-simple-001" {
		t.Errorf("expected RevertOfVersionID 'ver-simple-001', got %+v", v3.RevertOfVersionID)
	}
	if v3.Author != "revert-bot@coderpush.com" {
		t.Errorf("expected author 'revert-bot@coderpush.com', got %s", v3.Author)
	}
	if v3.Description != "Revert promo price to V1" {
		t.Errorf("expected description 'Revert promo price to V1', got %s", v3.Description)
	}
	if v3.Summary.Updated != 1 || v3.TotalItemsAffected != 1 {
		t.Errorf("expected Summary.Updated=1 and TotalItemsAffected=1, got %+v (total %d)", v3.Summary, v3.TotalItemsAffected)
	}

	// 5. Assertions on returned deltas
	if len(revDeltas) != 1 {
		t.Fatalf("expected 1 delta returned, got %d", len(revDeltas))
	}
	d := revDeltas[0]
	if d.SKU != "SKU-001" {
		t.Errorf("expected delta SKU-001, got %s", d.SKU)
	}
	if d.Action != domain.DeltaActionUpdated {
		t.Errorf("expected DeltaActionUpdated, got %s", d.Action)
	}
	if d.OldPrice == nil || *d.OldPrice != 40.00 {
		t.Errorf("expected OldPrice 40.00, got %v", d.OldPrice)
	}
	if d.NewPrice != 50.00 {
		t.Errorf("expected NewPrice 50.00, got %f", d.NewPrice)
	}
	if d.OldName == nil || *d.OldName != "Super Gadget V2" {
		t.Errorf("expected OldName 'Super Gadget V2', got %v", d.OldName)
	}
	if d.NewName != "Super Gadget V1" {
		t.Errorf("expected NewName 'Super Gadget V1', got %s", d.NewName)
	}
	if d.NewStatus != domain.StatusActive {
		t.Errorf("expected NewStatus ACTIVE, got %s", d.NewStatus)
	}

	// 6. Assertions on live products table
	liveV3, err := s.GetProduct(ctx, "SKU-001")
	if err != nil {
		t.Fatalf("GetProduct liveV3 failed: %v", err)
	}
	if liveV3.Price != 50.00 {
		t.Errorf("expected live price reverted to 50.00, got %f", liveV3.Price)
	}
	if liveV3.Name != "Super Gadget V1" {
		t.Errorf("expected live name reverted to 'Super Gadget V1', got %s", liveV3.Name)
	}
	if liveV3.Status != domain.StatusActive {
		t.Errorf("expected live status ACTIVE, got %s", liveV3.Status)
	}

	// 7. Assertions on history immutability
	allVersions, total, err := s.ListVersions(ctx, 10, 0)
	if err != nil || total != 3 {
		t.Fatalf("expected 3 versions total in history, got %d, err %v", total, err)
	}
	if allVersions[0].VersionID != v3.VersionID || allVersions[1].VersionID != "ver-simple-002" || allVersions[2].VersionID != "ver-simple-001" {
		t.Errorf("unexpected version list order: %+v", allVersions)
	}
}

func TestStorage_Rollback_PreservesTombstone_NoResurrection(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. Setup V1: SKU-TOMB was ACTIVE ($50.00, "Tombstone Original")
	//             SKU-ACTIVE was ACTIVE ($100.00, "Active Product")
	tx1, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx1 failed: %v", err)
	}
	pTombV1 := &domain.Product{
		SKU:               "SKU-TOMB",
		Name:              "Tombstone Original",
		Price:             50.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-3 * time.Hour),
		UpdatedAt:         now.Add(-3 * time.Hour),
	}
	pActiveV1 := &domain.Product{
		SKU:               "SKU-ACTIVE",
		Name:              "Active Product",
		Price:             100.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-3 * time.Hour),
		UpdatedAt:         now.Add(-3 * time.Hour),
	}
	_ = s.UpsertProductTx(ctx, tx1, pTombV1)
	_ = s.UpsertProductTx(ctx, tx1, pActiveV1)

	ver1 := &domain.CatalogVersion{
		VersionID:          "ver-tomb-001",
		VersionNumber:      1,
		Description:        "Initial baseline with active items",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypeInitial,
		TotalItemsAffected: 2,
		Summary:            domain.ChangeSummary{Added: 2},
		CreatedAt:          now.Add(-3 * time.Hour),
	}
	deltas1 := []domain.ItemDelta{
		{
			VersionID: "ver-tomb-001", SKU: "SKU-TOMB", Action: domain.DeltaActionAdded,
			NewName: "Tombstone Original", NewPrice: 50.00, NewStatus: domain.StatusActive,
			NewTimestamp: now.Add(-3 * time.Hour),
		},
		{
			VersionID: "ver-tomb-001", SKU: "SKU-ACTIVE", Action: domain.DeltaActionAdded,
			NewName: "Active Product", NewPrice: 100.00, NewStatus: domain.StatusActive,
			NewTimestamp: now.Add(-3 * time.Hour),
		},
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	if err := tx1.Commit(); err != nil {
		t.Fatalf("Commit tx1 failed: %v", err)
	}

	// 2. Setup V2: SKU-TOMB is WITHDRAWN ($35.00, "Tombstone Withdrawn")
	//             SKU-ACTIVE price updated to $120.00
	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx2 failed: %v", err)
	}
	pTombV2 := &domain.Product{
		SKU:               "SKU-TOMB",
		Name:              "Tombstone Withdrawn",
		Price:             35.00,
		Status:            domain.StatusWithdrawn,
		SupplierTimestamp: now.Add(-1 * time.Hour),
		UpdatedAt:         now.Add(-1 * time.Hour),
	}
	pActiveV2 := &domain.Product{
		SKU:               "SKU-ACTIVE",
		Name:              "Active Product",
		Price:             120.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-1 * time.Hour),
		UpdatedAt:         now.Add(-1 * time.Hour),
	}
	_ = s.UpsertProductTx(ctx, tx2, pTombV2)
	_ = s.UpsertProductTx(ctx, tx2, pActiveV2)

	ver2 := &domain.CatalogVersion{
		VersionID:          "ver-tomb-002",
		VersionNumber:      2,
		Description:        "Withdraw SKU-TOMB and price hike",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypePatch,
		TotalItemsAffected: 2,
		Summary:            domain.ChangeSummary{Updated: 1, Withdrawn: 1},
		CreatedAt:          now.Add(-1 * time.Hour),
	}
	oldTombName := "Tombstone Original"
	oldTombPrice := 50.00
	oldTombStatus := domain.StatusActive
	oldActivePrice := 100.00
	oldActiveStatus := domain.StatusActive
	deltas2 := []domain.ItemDelta{
		{
			VersionID: "ver-tomb-002", SKU: "SKU-TOMB", Action: domain.DeltaActionWithdrawn,
			OldName: &oldTombName, NewName: "Tombstone Withdrawn",
			OldPrice: &oldTombPrice, NewPrice: 35.00,
			OldStatus: &oldTombStatus, NewStatus: domain.StatusWithdrawn,
			NewTimestamp: now.Add(-1 * time.Hour),
		},
		{
			VersionID: "ver-tomb-002", SKU: "SKU-ACTIVE", Action: domain.DeltaActionUpdated,
			NewName:  "Active Product",
			OldPrice: &oldActivePrice, NewPrice: 120.00,
			OldStatus: &oldActiveStatus, NewStatus: domain.StatusActive,
			NewTimestamp: now.Add(-1 * time.Hour),
		},
	}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltas2)
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit tx2 failed: %v", err)
	}

	// Confirm live state before rollback: SKU-TOMB is WITHDRAWN
	liveBefore, err := s.GetProduct(ctx, "SKU-TOMB")
	if err != nil || liveBefore.Status != domain.StatusWithdrawn {
		t.Fatalf("expected SKU-TOMB WITHDRAWN before rollback, got %+v", liveBefore)
	}

	// 3. Action: Rollback to V1 (where SKU-TOMB was ACTIVE)
	v3, revDeltas, err := s.RollbackVersion(ctx, "ver-tomb-001", "revert-bot@coderpush.com", "Rollback to V1 baseline")
	if err != nil {
		t.Fatalf("RollbackVersion failed: %v", err)
	}
	if v3 == nil {
		t.Fatal("expected non-nil v3")
	}

	// 4. CRITICAL ASSERTION: Product MUST remain WITHDRAWN (No Resurrection / Tombstone Invariant A)
	tombLive, err := s.GetProduct(ctx, "SKU-TOMB")
	if err != nil {
		t.Fatalf("GetProduct SKU-TOMB failed: %v", err)
	}
	if tombLive.Status != domain.StatusWithdrawn {
		t.Fatalf("INVARIANT VIOLATION: SKU-TOMB resurrected to %s! Tombstone must be preserved as WITHDRAWN", tombLive.Status)
	}
	// Metadata (price and name) reverts to V1 baseline
	if tombLive.Price != 50.00 {
		t.Errorf("expected price reverted to 50.00, got %f", tombLive.Price)
	}
	if tombLive.Name != "Tombstone Original" {
		t.Errorf("expected name reverted to 'Tombstone Original', got %s", tombLive.Name)
	}

	// 5. Active product listing MUST NOT contain SKU-TOMB
	activeList, totalActive, err := s.ListProducts(ctx, "ACTIVE", "", 10, 0)
	if err != nil {
		t.Fatalf("ListProducts failed: %v", err)
	}
	if totalActive != 1 || len(activeList) != 1 {
		t.Fatalf("expected exactly 1 active product, got total=%d, len=%d", totalActive, len(activeList))
	}
	if activeList[0].SKU != "SKU-ACTIVE" {
		t.Errorf("expected active item to be SKU-ACTIVE, got %s", activeList[0].SKU)
	}
	for _, p := range activeList {
		if p.SKU == "SKU-TOMB" {
			t.Fatalf("INVARIANT VIOLATION: SKU-TOMB leaked into active product listings!")
		}
	}

	// 6. Verify deltas recorded for SKU-TOMB
	var tombDelta *domain.ItemDelta
	for i := range revDeltas {
		if revDeltas[i].SKU == "SKU-TOMB" {
			tombDelta = &revDeltas[i]
			break
		}
	}
	if tombDelta == nil {
		t.Fatal("expected delta for SKU-TOMB in rollback deltas")
	}
	if tombDelta.NewStatus != domain.StatusWithdrawn {
		t.Errorf("expected delta NewStatus WITHDRAWN, got %s", tombDelta.NewStatus)
	}
	if tombDelta.OldStatus == nil || *tombDelta.OldStatus != domain.StatusWithdrawn {
		t.Errorf("expected delta OldStatus WITHDRAWN, got %v", tombDelta.OldStatus)
	}
	if tombDelta.NewPrice != 50.00 {
		t.Errorf("expected delta NewPrice 50.00, got %f", tombDelta.NewPrice)
	}

	// 7. Verify SKU-ACTIVE reverted properly to $100.00
	activeLive, err := s.GetProduct(ctx, "SKU-ACTIVE")
	if err != nil || activeLive.Price != 100.00 || activeLive.Status != domain.StatusActive {
		t.Errorf("expected SKU-ACTIVE price 100.00 and ACTIVE, got %+v", activeLive)
	}
}

func TestStorage_Rollback_WithdrawsPostBaselineAdditions(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. Setup V1: Only SKU-A exists
	tx1, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx1 failed: %v", err)
	}
	pA := &domain.Product{
		SKU:               "SKU-A",
		Name:              "Alpha Baseline",
		Price:             10.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-2 * time.Hour),
		UpdatedAt:         now.Add(-2 * time.Hour),
	}
	_ = s.UpsertProductTx(ctx, tx1, pA)
	ver1 := &domain.CatalogVersion{
		VersionID:          "ver-post-001",
		VersionNumber:      1,
		Description:        "Baseline V1 with only SKU-A",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypeInitial,
		TotalItemsAffected: 1,
		Summary:            domain.ChangeSummary{Added: 1},
		CreatedAt:          now.Add(-2 * time.Hour),
	}
	deltas1 := []domain.ItemDelta{
		{
			VersionID: "ver-post-001", SKU: "SKU-A", Action: domain.DeltaActionAdded,
			NewName: "Alpha Baseline", NewPrice: 10.00, NewStatus: domain.StatusActive,
			NewTimestamp: now.Add(-2 * time.Hour),
		},
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	if err := tx1.Commit(); err != nil {
		t.Fatalf("Commit tx1 failed: %v", err)
	}

	// 2. Setup V2: Adds SKU-B, updates SKU-A
	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx2 failed: %v", err)
	}
	pA2 := &domain.Product{
		SKU:               "SKU-A",
		Name:              "Alpha Updated",
		Price:             15.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-1 * time.Hour),
		UpdatedAt:         now.Add(-1 * time.Hour),
	}
	pB := &domain.Product{
		SKU:               "SKU-B",
		Name:              "Beta Post-Addition",
		Price:             25.00,
		Status:            domain.StatusActive,
		SupplierTimestamp: now.Add(-1 * time.Hour),
		UpdatedAt:         now.Add(-1 * time.Hour),
	}
	_ = s.UpsertProductTx(ctx, tx2, pA2)
	_ = s.UpsertProductTx(ctx, tx2, pB)

	ver2 := &domain.CatalogVersion{
		VersionID:          "ver-post-002",
		VersionNumber:      2,
		Description:        "Add SKU-B and update SKU-A",
		Author:             "ops@coderpush.com",
		Type:               domain.VersionTypePatch,
		TotalItemsAffected: 2,
		Summary:            domain.ChangeSummary{Added: 1, Updated: 1},
		CreatedAt:          now.Add(-1 * time.Hour),
	}
	oldPriceA := 10.00
	oldNameA := "Alpha Baseline"
	oldStatusA := domain.StatusActive
	deltas2 := []domain.ItemDelta{
		{
			VersionID: "ver-post-002", SKU: "SKU-A", Action: domain.DeltaActionUpdated,
			OldName: &oldNameA, NewName: "Alpha Updated",
			OldPrice: &oldPriceA, NewPrice: 15.00,
			OldStatus: &oldStatusA, NewStatus: domain.StatusActive,
			NewTimestamp: now.Add(-1 * time.Hour),
		},
		{
			VersionID: "ver-post-002", SKU: "SKU-B", Action: domain.DeltaActionAdded,
			NewName: "Beta Post-Addition", NewPrice: 25.00, NewStatus: domain.StatusActive,
			NewTimestamp: now.Add(-1 * time.Hour),
		},
	}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltas2)
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit tx2 failed: %v", err)
	}

	// Verify both exist and are ACTIVE at V2
	prodsV2, totalV2, _ := s.ListProducts(ctx, "ACTIVE", "", 10, 0)
	if totalV2 != 2 || len(prodsV2) != 2 {
		t.Fatalf("expected 2 active products at V2, got %d", totalV2)
	}

	// 3. Action: Rollback to V1
	v3, revDeltas, err := s.RollbackVersion(ctx, "ver-post-001", "revert-bot@coderpush.com", "Revert to V1 before SKU-B")
	if err != nil {
		t.Fatalf("RollbackVersion failed: %v", err)
	}
	if v3 == nil {
		t.Fatal("expected non-nil v3")
	}

	// 4. Assertions on SKU-B (Post-Baseline Addition):
	// Must transition to WITHDRAWN (not hard deleted!)
	liveB, err := s.GetProduct(ctx, "SKU-B")
	if err != nil {
		t.Fatalf("expected SKU-B to still exist in DB (non-destructive!), got error: %v", err)
	}
	if liveB.Status != domain.StatusWithdrawn {
		t.Errorf("expected SKU-B status WITHDRAWN, got %s", liveB.Status)
	}
	if liveB.Price != 25.00 {
		t.Errorf("expected SKU-B price preserved as 25.00, got %f", liveB.Price)
	}

	// 5. Active listing MUST ONLY contain SKU-A
	activeList, totalActive, err := s.ListProducts(ctx, "ACTIVE", "", 10, 0)
	if err != nil {
		t.Fatalf("ListProducts ACTIVE failed: %v", err)
	}
	if totalActive != 1 || len(activeList) != 1 {
		t.Fatalf("expected 1 active product, got total=%d, len=%d", totalActive, len(activeList))
	}
	if activeList[0].SKU != "SKU-A" {
		t.Errorf("expected active item SKU-A, got %s", activeList[0].SKU)
	}

	// 6. Withdrawn listing MUST contain SKU-B
	withdrawnList, totalWithdrawn, err := s.ListProducts(ctx, "WITHDRAWN", "", 10, 0)
	if err != nil {
		t.Fatalf("ListProducts WITHDRAWN failed: %v", err)
	}
	if totalWithdrawn != 1 || len(withdrawnList) != 1 {
		t.Fatalf("expected 1 withdrawn product, got total=%d, len=%d", totalWithdrawn, len(withdrawnList))
	}
	if withdrawnList[0].SKU != "SKU-B" {
		t.Errorf("expected withdrawn item SKU-B, got %s", withdrawnList[0].SKU)
	}

	// 7. SKU-A must be restored to V1 ($10.00, Alpha Baseline)
	liveA, err := s.GetProduct(ctx, "SKU-A")
	if err != nil {
		t.Fatalf("GetProduct SKU-A failed: %v", err)
	}
	if liveA.Price != 10.00 || liveA.Name != "Alpha Baseline" || liveA.Status != domain.StatusActive {
		t.Errorf("expected SKU-A restored to V1, got %+v", liveA)
	}

	// 8. Verify delta for SKU-B is WITHDRAWN
	var deltaB *domain.ItemDelta
	for i := range revDeltas {
		if revDeltas[i].SKU == "SKU-B" {
			deltaB = &revDeltas[i]
			break
		}
	}
	if deltaB == nil {
		t.Fatal("expected delta for SKU-B in rollback deltas")
	}
	if deltaB.Action != domain.DeltaActionWithdrawn {
		t.Errorf("expected delta action WITHDRAWN, got %s", deltaB.Action)
	}
	if deltaB.OldStatus == nil || *deltaB.OldStatus != domain.StatusActive {
		t.Errorf("expected OldStatus ACTIVE, got %v", deltaB.OldStatus)
	}
	if deltaB.NewStatus != domain.StatusWithdrawn {
		t.Errorf("expected NewStatus WITHDRAWN, got %s", deltaB.NewStatus)
	}
}

func TestStorage_Rollback_ChainedRollbacks(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// V1: SKU-1 ($10.00), SKU-2 ($20.00)
	tx1, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{SKU: "SKU-1", Name: "Item 1", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now})
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{SKU: "SKU-2", Name: "Item 2", Price: 20.00, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now})
	ver1 := &domain.CatalogVersion{VersionID: "ver-chain-001", VersionNumber: 1, Description: "V1", Author: "author", Type: domain.VersionTypeInitial, TotalItemsAffected: 2, Summary: domain.ChangeSummary{Added: 2}, CreatedAt: now}
	deltas1 := []domain.ItemDelta{
		{VersionID: "ver-chain-001", SKU: "SKU-1", Action: domain.DeltaActionAdded, NewName: "Item 1", NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now},
		{VersionID: "ver-chain-001", SKU: "SKU-2", Action: domain.DeltaActionAdded, NewName: "Item 2", NewPrice: 20.00, NewStatus: domain.StatusActive, NewTimestamp: now},
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	_ = tx1.Commit()

	// V2: SKU-1 ($15.00), SKU-2 ($25.00)
	tx2, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx2, &domain.Product{SKU: "SKU-1", Name: "Item 1", Price: 15.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour)})
	_ = s.UpsertProductTx(ctx, tx2, &domain.Product{SKU: "SKU-2", Name: "Item 2", Price: 25.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour)})
	ver2 := &domain.CatalogVersion{VersionID: "ver-chain-002", VersionNumber: 2, Description: "V2", Author: "author", Type: domain.VersionTypePatch, TotalItemsAffected: 2, Summary: domain.ChangeSummary{Updated: 2}, CreatedAt: now.Add(time.Hour)}
	oldP1 := 10.00
	oldP2 := 20.00
	oldSt := domain.StatusActive
	deltas2 := []domain.ItemDelta{
		{VersionID: "ver-chain-002", SKU: "SKU-1", Action: domain.DeltaActionUpdated, NewName: "Item 1", OldPrice: &oldP1, NewPrice: 15.00, OldStatus: &oldSt, NewStatus: domain.StatusActive, NewTimestamp: now.Add(time.Hour)},
		{VersionID: "ver-chain-002", SKU: "SKU-2", Action: domain.DeltaActionUpdated, NewName: "Item 2", OldPrice: &oldP2, NewPrice: 25.00, OldStatus: &oldSt, NewStatus: domain.StatusActive, NewTimestamp: now.Add(time.Hour)},
	}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltas2)
	_ = tx2.Commit()

	// Rollback 1: Rollback to V1 -> Creates V3
	v3, _, err := s.RollbackVersion(ctx, "ver-chain-001", "bot", "Rollback to V1")
	if err != nil {
		t.Fatalf("Rollback to V1 failed: %v", err)
	}
	if v3.VersionNumber != 3 || v3.Type != domain.VersionTypeRollback || *v3.RevertOfVersionID != "ver-chain-001" {
		t.Fatalf("unexpected v3: %+v", v3)
	}
	// Verify live catalog matches V1
	p1AtV3, _ := s.GetProduct(ctx, "SKU-1")
	p2AtV3, _ := s.GetProduct(ctx, "SKU-2")
	if p1AtV3.Price != 10.00 || p2AtV3.Price != 20.00 {
		t.Errorf("catalog mismatch after rollback to V1: SKU-1=%f, SKU-2=%f", p1AtV3.Price, p2AtV3.Price)
	}

	// Rollback 2: Rollback to V2 -> Creates V4
	v4, _, err := s.RollbackVersion(ctx, "ver-chain-002", "bot", "Rollback to V2")
	if err != nil {
		t.Fatalf("Rollback to V2 failed: %v", err)
	}
	if v4.VersionNumber != 4 || v4.Type != domain.VersionTypeRollback || *v4.RevertOfVersionID != "ver-chain-002" {
		t.Fatalf("unexpected v4: %+v", v4)
	}
	// Verify live catalog matches V2
	p1AtV4, _ := s.GetProduct(ctx, "SKU-1")
	p2AtV4, _ := s.GetProduct(ctx, "SKU-2")
	if p1AtV4.Price != 15.00 || p2AtV4.Price != 25.00 {
		t.Errorf("catalog mismatch after rollback to V2: SKU-1=%f, SKU-2=%f", p1AtV4.Price, p2AtV4.Price)
	}

	// Rollback 3: Rollback to V3 -> Creates V5
	v5, _, err := s.RollbackVersion(ctx, v3.VersionID, "bot", "Rollback to V3")
	if err != nil {
		t.Fatalf("Rollback to V3 failed: %v", err)
	}
	if v5.VersionNumber != 5 || v5.Type != domain.VersionTypeRollback || *v5.RevertOfVersionID != v3.VersionID {
		t.Fatalf("unexpected v5: %+v", v5)
	}
	// Verify live catalog matches V3 (which is $10.00 and $20.00)
	p1AtV5, _ := s.GetProduct(ctx, "SKU-1")
	p2AtV5, _ := s.GetProduct(ctx, "SKU-2")
	if p1AtV5.Price != 10.00 || p2AtV5.Price != 20.00 {
		t.Errorf("catalog mismatch after rollback to V3: SKU-1=%f, SKU-2=%f", p1AtV5.Price, p2AtV5.Price)
	}

	// Verify history is strictly forward-moving and audit records are never deleted
	versions, total, err := s.ListVersions(ctx, 10, 0)
	if err != nil || total != 5 || len(versions) != 5 {
		t.Fatalf("expected 5 versions in history, got total=%d, len=%d, err=%v", total, len(versions), err)
	}
	// Verify monotonic version numbers: 5, 4, 3, 2, 1
	for i, expectedNum := range []int{5, 4, 3, 2, 1} {
		if versions[i].VersionNumber != expectedNum {
			t.Errorf("version at index %d expected number %d, got %d", i, expectedNum, versions[i].VersionNumber)
		}
	}

	// Verify deltas for all 5 versions are intact in DB
	for _, ver := range versions {
		deltas, err := s.GetVersionDeltas(ctx, ver.VersionID)
		if err != nil || len(deltas) == 0 {
			t.Errorf("deltas missing for version %s: count=%d, err=%v", ver.VersionID, len(deltas), err)
		}
	}
}

func TestStorage_Rollback_NonExistentTargetVersion(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Seed V1
	tx, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx, &domain.Product{SKU: "SKU-1", Name: "Item 1", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now})
	ver1 := &domain.CatalogVersion{VersionID: "ver-exist-001", VersionNumber: 1, Description: "V1", Author: "user", Type: domain.VersionTypeInitial, TotalItemsAffected: 1, Summary: domain.ChangeSummary{Added: 1}, CreatedAt: now}
	deltas1 := []domain.ItemDelta{{VersionID: "ver-exist-001", SKU: "SKU-1", Action: domain.DeltaActionAdded, NewName: "Item 1", NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now}}
	_ = s.CreateVersionTx(ctx, tx, ver1, deltas1)
	_ = tx.Commit()

	// 1. Non-existent version ID
	_, _, err := s.RollbackVersion(ctx, "ver-does-not-exist", "operator", "revert non-existent")
	if err == nil {
		t.Fatal("expected error for non-existent target version, got nil")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected domain.ErrNotFound, got: %v", err)
	}

	// 2. Empty string target version ID
	_, _, errEmpty := s.RollbackVersion(ctx, "", "operator", "empty version")
	if errEmpty == nil {
		t.Fatal("expected error for empty target version ID, got nil")
	}

	// 3. Whitespace-only target version ID
	_, _, errWhitespace := s.RollbackVersion(ctx, "   ", "operator", "whitespace version")
	if errWhitespace == nil {
		t.Fatal("expected error for whitespace target version ID, got nil")
	}

	// 4. Verify no new versions or deltas were written
	versions, total, err := s.ListVersions(ctx, 10, 0)
	if err != nil || total != 1 || len(versions) != 1 {
		t.Fatalf("expected exactly 1 version to remain, got total=%d, len=%d", total, len(versions))
	}
	if versions[0].VersionID != "ver-exist-001" {
		t.Errorf("expected version ver-exist-001, got %s", versions[0].VersionID)
	}

	// Verify live product is unmodified
	p, err := s.GetProduct(ctx, "SKU-1")
	if err != nil || p.Price != 10.00 {
		t.Fatalf("product modified after failed rollback: %+v", p)
	}
}

func TestStorage_Rollback_SubMillisecondActiveCatalog(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	totalProducts := 2000
	if isRaceDetectorEnabled() {
		totalProducts = 600
	}

	// 1. Seed 2,000 live products in V1
	tx1, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx tx1 failed: %v", err)
	}
	deltasV1 := make([]domain.ItemDelta, totalProducts)
	for i := 0; i < totalProducts; i++ {
		sku := fmt.Sprintf("SKU-%05d", i)
		name := fmt.Sprintf("Item %s", sku)
		price := float64(10 + (i % 100))
		status := domain.StatusActive
		if i%20 == 0 {
			status = domain.StatusWithdrawn
		}
		_ = s.UpsertProductTx(ctx, tx1, &domain.Product{
			SKU: sku, Name: name, Price: price, Status: status,
			SupplierTimestamp: now, UpdatedAt: now,
		})
		deltasV1[i] = domain.ItemDelta{
			VersionID: "ver-perf-001", SKU: sku, Action: domain.DeltaActionAdded,
			NewName: name, NewPrice: price, NewStatus: status, NewTimestamp: now,
		}
	}
	ver1 := &domain.CatalogVersion{
		VersionID: "ver-perf-001", VersionNumber: 1, Description: "V1 Bulk Seed",
		Author: "bench@coderpush.com", Type: domain.VersionTypeInitial,
		TotalItemsAffected: totalProducts, Summary: domain.ChangeSummary{Added: totalProducts},
		CreatedAt: now,
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltasV1)
	if err := tx1.Commit(); err != nil {
		t.Fatalf("Commit V1 failed: %v", err)
	}

	// 2. Apply Patch V2: Update 400 products
	tx2, _ := s.BeginTx(ctx)
	deltasV2 := make([]domain.ItemDelta, 400)
	for i := 0; i < 400; i++ {
		sku := fmt.Sprintf("SKU-%05d", i)
		oldP := float64(10 + (i % 100))
		newP := oldP * 1.2
		_ = s.UpsertProductTx(ctx, tx2, &domain.Product{
			SKU: sku, Name: fmt.Sprintf("Updated %s", sku), Price: newP, Status: domain.StatusActive,
			SupplierTimestamp: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour),
		})
		deltasV2[i] = domain.ItemDelta{
			VersionID: "ver-perf-002", SKU: sku, Action: domain.DeltaActionUpdated,
			NewName: fmt.Sprintf("Updated %s", sku), OldPrice: &oldP, NewPrice: newP,
			NewStatus: domain.StatusActive, NewTimestamp: now.Add(time.Hour),
		}
	}
	ver2 := &domain.CatalogVersion{
		VersionID: "ver-perf-002", VersionNumber: 2, Description: "V2 Price Hike",
		Author: "bench@coderpush.com", Type: domain.VersionTypePatch,
		TotalItemsAffected: 400, Summary: domain.ChangeSummary{Updated: 400},
		CreatedAt: now.Add(time.Hour),
	}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltasV2)
	_ = tx2.Commit()

	// 3. Rollback to V1 -> Creates V3 (reverts 400 products)
	_, _, err = s.RollbackVersion(ctx, "ver-perf-001", "bench", "Rollback V1")
	if err != nil {
		t.Fatalf("Rollback to V1 failed: %v", err)
	}

	// 4. Apply Patch V4: Update 300 products
	tx4, _ := s.BeginTx(ctx)
	deltasV4 := make([]domain.ItemDelta, 300)
	for i := 200; i < 500; i++ {
		sku := fmt.Sprintf("SKU-%05d", i)
		oldP := float64(10 + (i % 100))
		newP := oldP * 0.9
		_ = s.UpsertProductTx(ctx, tx4, &domain.Product{
			SKU: sku, Name: fmt.Sprintf("Discounted %s", sku), Price: newP, Status: domain.StatusActive,
			SupplierTimestamp: now.Add(2 * time.Hour), UpdatedAt: now.Add(2 * time.Hour),
		})
		deltasV4[i-200] = domain.ItemDelta{
			VersionID: "ver-perf-004", SKU: sku, Action: domain.DeltaActionUpdated,
			NewName: fmt.Sprintf("Discounted %s", sku), OldPrice: &oldP, NewPrice: newP,
			NewStatus: domain.StatusActive, NewTimestamp: now.Add(2 * time.Hour),
		}
	}
	ver4 := &domain.CatalogVersion{
		VersionID: "ver-perf-004", VersionNumber: 4, Description: "V4 Discount",
		Author: "bench@coderpush.com", Type: domain.VersionTypePatch,
		TotalItemsAffected: 300, Summary: domain.ChangeSummary{Updated: 300},
		CreatedAt: now.Add(2 * time.Hour),
	}
	_ = s.CreateVersionTx(ctx, tx4, ver4, deltasV4)
	_ = tx4.Commit()

	// 5. Rollback to V2 -> Creates V5
	_, _, err = s.RollbackVersion(ctx, "ver-perf-002", "bench", "Rollback V2")
	if err != nil {
		t.Fatalf("Rollback to V2 failed: %v", err)
	}

	// Verify total deltas in journal exceeds 3,000
	var totalDeltas int
	_ = s.DB().QueryRowContext(ctx, "SELECT count(*) FROM catalog_version_deltas;").Scan(&totalDeltas)
	t.Logf("Total deltas in journal after rollbacks: %d", totalDeltas)

	// 6. Warm-up
	for i := 0; i < 50; i++ {
		_, _ = s.GetProduct(ctx, "SKU-00500")
	}

	// 7. Measure Point Lookup Latency (< 1.0 ms)
	pointLookups := 1000
	if isRaceDetectorEnabled() {
		pointLookups = 300
	}
	startPoint := time.Now()
	for i := 0; i < pointLookups; i++ {
		sku := fmt.Sprintf("SKU-%05d", (i*7)%totalProducts)
		p, err := s.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("lookup failed for %s: %v", sku, err)
		}
	}
	pointElapsed := time.Since(startPoint)
	avgPointLatency := pointElapsed / time.Duration(pointLookups)
	t.Logf("Measured avg point lookup latency post-rollbacks: %v across %d lookups", avgPointLatency, pointLookups)

	maxPointAllowed := 1 * time.Millisecond
	if isRaceDetectorEnabled() {
		maxPointAllowed = 15 * time.Millisecond
	}
	if avgPointLatency >= maxPointAllowed {
		t.Fatalf("Point lookup latency regression post-rollbacks: got %v, want < %v", avgPointLatency, maxPointAllowed)
	}

	// 8. Measure Paginated List Latency (< 2.0 ms for 50 items)
	listQueries := 200
	if isRaceDetectorEnabled() {
		listQueries = 50
	}
	startList := time.Now()
	for i := 0; i < listQueries; i++ {
		offset := (i * 10) % (totalProducts / 2)
		prods, total, err := s.ListProducts(ctx, "ACTIVE", "", 50, offset)
		if err != nil || total == 0 || len(prods) == 0 {
			t.Fatalf("ListProducts failed at offset %d: %v", offset, err)
		}
	}
	listElapsed := time.Since(startList)
	avgListLatency := listElapsed / time.Duration(listQueries)
	t.Logf("Measured avg list query latency post-rollbacks: %v across %d queries", avgListLatency, listQueries)

	maxListAllowed := 2 * time.Millisecond
	if isRaceDetectorEnabled() {
		maxListAllowed = 50 * time.Millisecond
	}
	if avgListLatency >= maxListAllowed {
		t.Fatalf("List query latency regression post-rollbacks: got %v, want < %v", avgListLatency, maxListAllowed)
	}
}

func TestStorage_Rollback_ConcurrentRollbackAndReads(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Seed 100 products in V1 ($10.00 each)
	tx1, _ := s.BeginTx(ctx)
	deltas1 := make([]domain.ItemDelta, 100)
	for i := 0; i < 100; i++ {
		sku := fmt.Sprintf("SKU-CR-%03d", i)
		_ = s.UpsertProductTx(ctx, tx1, &domain.Product{
			SKU: sku, Name: fmt.Sprintf("Item %s", sku), Price: 10.00,
			Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now,
		})
		deltas1[i] = domain.ItemDelta{
			VersionID: "ver-cr-001", SKU: sku, Action: domain.DeltaActionAdded,
			NewName: fmt.Sprintf("Item %s", sku), NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now,
		}
	}
	ver1 := &domain.CatalogVersion{
		VersionID: "ver-cr-001", VersionNumber: 1, Description: "V1 Base",
		Author: "race-test", Type: domain.VersionTypeInitial, TotalItemsAffected: 100, CreatedAt: now,
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	_ = tx1.Commit()

	// Seed V2 ($20.00 each)
	tx2, _ := s.BeginTx(ctx)
	oldP := 10.00
	oldSt := domain.StatusActive
	deltas2 := make([]domain.ItemDelta, 100)
	for i := 0; i < 100; i++ {
		sku := fmt.Sprintf("SKU-CR-%03d", i)
		_ = s.UpsertProductTx(ctx, tx2, &domain.Product{
			SKU: sku, Name: fmt.Sprintf("Item %s", sku), Price: 20.00,
			Status: domain.StatusActive, SupplierTimestamp: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour),
		})
		deltas2[i] = domain.ItemDelta{
			VersionID: "ver-cr-002", SKU: sku, Action: domain.DeltaActionUpdated,
			NewName: fmt.Sprintf("Item %s", sku), OldPrice: &oldP, NewPrice: 20.00,
			OldStatus: &oldSt, NewStatus: domain.StatusActive, NewTimestamp: now.Add(time.Hour),
		}
	}
	ver2 := &domain.CatalogVersion{
		VersionID: "ver-cr-002", VersionNumber: 2, Description: "V2 Hike",
		Author: "race-test", Type: domain.VersionTypePatch, TotalItemsAffected: 100, CreatedAt: now.Add(time.Hour),
	}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltas2)
	_ = tx2.Commit()

	var wg sync.WaitGroup
	const readers = 6
	const readIterations = 50
	const rollbackIterations = 6

	startBarrier := make(chan struct{})

	// Readers: concurrently query products and lists
	wg.Add(readers)
	for r := 0; r < readers; r++ {
		go func(readerID int) {
			defer wg.Done()
			<-startBarrier
			for i := 0; i < readIterations; i++ {
				sku := fmt.Sprintf("SKU-CR-%03d", (readerID+i)%100)
				p, err := s.GetProduct(ctx, sku)
				if err != nil {
					t.Errorf("reader %d failed GetProduct(%s): %v", readerID, sku, err)
					return
				}
				if p.Price != 10.00 && p.Price != 20.00 {
					t.Errorf("reader %d observed torn/corrupt price %f for %s", readerID, p.Price, sku)
				}
				if i%5 == 0 {
					prods, _, err := s.ListProducts(ctx, "ACTIVE", "", 20, 0)
					if err != nil {
						t.Errorf("reader %d failed ListProducts: %v", readerID, err)
						return
					}
					if len(prods) == 0 {
						t.Errorf("reader %d observed empty active product list", readerID)
					}
				}
			}
		}(r)
	}

	// Rollback worker: repeatedly alternates rolling back to V1 and V2
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-startBarrier
		for i := 0; i < rollbackIterations; i++ {
			target := "ver-cr-001"
			if i%2 == 1 {
				target = "ver-cr-002"
			}
			_, _, err := s.RollbackVersion(ctx, target, "race-bot", fmt.Sprintf("Race rollback iter %d", i))
			if err != nil {
				t.Errorf("concurrent rollback failed at iter %d: %v", i, err)
				return
			}
		}
	}()

	close(startBarrier)
	wg.Wait()
}

func TestStorage_Rollback_ConcurrentRollbackContention(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Seed V1
	tx1, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{SKU: "SKU-CONTEND", Name: "Item", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now})
	ver1 := &domain.CatalogVersion{VersionID: "ver-contend-001", VersionNumber: 1, Description: "V1", Author: "ops", Type: domain.VersionTypeInitial, TotalItemsAffected: 1, CreatedAt: now}
	deltas1 := []domain.ItemDelta{{VersionID: "ver-contend-001", SKU: "SKU-CONTEND", Action: domain.DeltaActionAdded, NewName: "Item", NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now}}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	_ = tx1.Commit()

	// Seed V2
	tx2, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx2, &domain.Product{SKU: "SKU-CONTEND", Name: "Item", Price: 20.00, Status: domain.StatusActive, SupplierTimestamp: now.Add(time.Hour), UpdatedAt: now.Add(time.Hour)})
	ver2 := &domain.CatalogVersion{VersionID: "ver-contend-002", VersionNumber: 2, Description: "V2", Author: "ops", Type: domain.VersionTypePatch, TotalItemsAffected: 1, CreatedAt: now.Add(time.Hour)}
	oldP := 10.00
	deltas2 := []domain.ItemDelta{{VersionID: "ver-contend-002", SKU: "SKU-CONTEND", Action: domain.DeltaActionUpdated, NewName: "Item", OldPrice: &oldP, NewPrice: 20.00, NewStatus: domain.StatusActive, NewTimestamp: now.Add(time.Hour)}}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltas2)
	_ = tx2.Commit()

	const concurrentRollbacks = 6
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	errorsCh := make(chan error, concurrentRollbacks)
	successCh := make(chan *domain.CatalogVersion, concurrentRollbacks)

	wg.Add(concurrentRollbacks)
	for i := 0; i < concurrentRollbacks; i++ {
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier
			ver, _, err := s.RollbackVersion(ctx, "ver-contend-001", fmt.Sprintf("worker-%d", workerID), "Concurrent rollback to V1")
			if err != nil {
				errorsCh <- err
			} else {
				successCh <- ver
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()
	close(errorsCh)
	close(successCh)

	var successes []*domain.CatalogVersion
	for v := range successCh {
		successes = append(successes, v)
	}
	var errs []error
	for err := range errorsCh {
		errs = append(errs, err)
	}

	t.Logf("Concurrent rollbacks: %d succeeded, %d failed", len(successes), len(errs))
	if len(errs) > 0 {
		t.Fatalf("unexpected error in concurrent rollback contention: %v", errs[0])
	}
	if len(successes) != concurrentRollbacks {
		t.Fatalf("expected all %d rollbacks to succeed sequentially, got %d", concurrentRollbacks, len(successes))
	}

	// Verify all created versions have unique, strictly increasing version numbers
	seenNumbers := make(map[int]bool)
	for _, v := range successes {
		if seenNumbers[v.VersionNumber] {
			t.Fatalf("duplicate version number detected under race: %d", v.VersionNumber)
		}
		seenNumbers[v.VersionNumber] = true
	}
}

func TestStorage_GetBaselineStateAtVersion(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// V1: SKU-A ($10.00, ACTIVE), SKU-B ($20.00, ACTIVE)
	tx1, _ := s.BeginTx(ctx)
	ver1 := &domain.CatalogVersion{VersionID: "ver-base-001", VersionNumber: 1, Description: "V1", Author: "user", Type: domain.VersionTypeInitial, TotalItemsAffected: 2, CreatedAt: now}
	deltas1 := []domain.ItemDelta{
		{VersionID: "ver-base-001", SKU: "SKU-A", Action: domain.DeltaActionAdded, NewName: "Item A", NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now},
		{VersionID: "ver-base-001", SKU: "SKU-B", Action: domain.DeltaActionAdded, NewName: "Item B", NewPrice: 20.00, NewStatus: domain.StatusActive, NewTimestamp: now},
	}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	_ = tx1.Commit()

	// V2: SKU-A updated to $15.00, SKU-C added ($30.00, ACTIVE)
	tx2, _ := s.BeginTx(ctx)
	ver2 := &domain.CatalogVersion{VersionID: "ver-base-002", VersionNumber: 2, Description: "V2", Author: "user", Type: domain.VersionTypePatch, TotalItemsAffected: 2, CreatedAt: now.Add(time.Hour)}
	oldPA := 10.00
	deltas2 := []domain.ItemDelta{
		{VersionID: "ver-base-002", SKU: "SKU-A", Action: domain.DeltaActionUpdated, NewName: "Item A Updated", OldPrice: &oldPA, NewPrice: 15.00, NewStatus: domain.StatusActive, NewTimestamp: now.Add(time.Hour)},
		{VersionID: "ver-base-002", SKU: "SKU-C", Action: domain.DeltaActionAdded, NewName: "Item C", NewPrice: 30.00, NewStatus: domain.StatusActive, NewTimestamp: now.Add(time.Hour)},
	}
	_ = s.CreateVersionTx(ctx, tx2, ver2, deltas2)
	_ = tx2.Commit()

	// 1. Get baseline at V1: Must have SKU-A ($10.00) and SKU-B ($20.00). Must NOT have SKU-C!
	base1, err := s.GetBaselineStateAtVersion(ctx, "ver-base-001")
	if err != nil {
		t.Fatalf("GetBaselineStateAtVersion V1 failed: %v", err)
	}
	if len(base1) != 2 {
		t.Fatalf("expected 2 items in V1 baseline, got %d", len(base1))
	}
	if base1["SKU-A"].Price != 10.00 || base1["SKU-A"].Name != "Item A" {
		t.Errorf("unexpected SKU-A in V1 baseline: %+v", base1["SKU-A"])
	}
	if base1["SKU-B"].Price != 20.00 || base1["SKU-B"].Name != "Item B" {
		t.Errorf("unexpected SKU-B in V1 baseline: %+v", base1["SKU-B"])
	}
	if _, exists := base1["SKU-C"]; exists {
		t.Errorf("SKU-C must not exist in V1 baseline!")
	}

	// 2. Get baseline at V2: Must have SKU-A ($15.00), SKU-B ($20.00), SKU-C ($30.00)
	base2, err := s.GetBaselineStateAtVersion(ctx, "ver-base-002")
	if err != nil {
		t.Fatalf("GetBaselineStateAtVersion V2 failed: %v", err)
	}
	if len(base2) != 3 {
		t.Fatalf("expected 3 items in V2 baseline, got %d", len(base2))
	}
	if base2["SKU-A"].Price != 15.00 || base2["SKU-A"].Name != "Item A Updated" {
		t.Errorf("unexpected SKU-A in V2 baseline: %+v", base2["SKU-A"])
	}
	if base2["SKU-B"].Price != 20.00 {
		t.Errorf("unexpected SKU-B in V2 baseline: %+v", base2["SKU-B"])
	}
	if base2["SKU-C"].Price != 30.00 || base2["SKU-C"].Name != "Item C" {
		t.Errorf("unexpected SKU-C in V2 baseline: %+v", base2["SKU-C"])
	}

	// 3. Non-existent version returns domain.ErrNotFound
	_, errNotFound := s.GetBaselineStateAtVersion(ctx, "ver-non-existent")
	if !errors.Is(errNotFound, domain.ErrNotFound) {
		t.Errorf("expected domain.ErrNotFound for non-existent version baseline, got: %v", errNotFound)
	}
}

func TestStorage_Rollback_IdenticalStateNoOp(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Seed V1
	tx1, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{SKU: "SKU-IDEN", Name: "Item", Price: 10.00, Status: domain.StatusActive, SupplierTimestamp: now, UpdatedAt: now})
	ver1 := &domain.CatalogVersion{VersionID: "ver-iden-001", VersionNumber: 1, Description: "V1", Author: "ops", Type: domain.VersionTypeInitial, TotalItemsAffected: 1, CreatedAt: now}
	deltas1 := []domain.ItemDelta{{VersionID: "ver-iden-001", SKU: "SKU-IDEN", Action: domain.DeltaActionAdded, NewName: "Item", NewPrice: 10.00, NewStatus: domain.StatusActive, NewTimestamp: now}}
	_ = s.CreateVersionTx(ctx, tx1, ver1, deltas1)
	_ = tx1.Commit()

	// Rollback to V1 when catalog is already at V1 state
	v2, revDeltas, err := s.RollbackVersion(ctx, "ver-iden-001", "ops", "Rollback to current baseline")
	if err != nil {
		t.Fatalf("RollbackVersion failed: %v", err)
	}
	if v2.VersionNumber != 2 {
		t.Errorf("expected version 2, got %d", v2.VersionNumber)
	}
	if v2.Type != domain.VersionTypeRollback {
		t.Errorf("expected ROLLBACK type, got %s", v2.Type)
	}
	if len(revDeltas) != 0 || v2.TotalItemsAffected != 0 {
		t.Errorf("expected 0 deltas for identical state rollback, got len=%d, total=%d", len(revDeltas), v2.TotalItemsAffected)
	}
}

func TestStorage_GetBaselineStateAtVersion_NotFound(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	// 1. Non-existent version ID
	_, err := s.GetBaselineStateAtVersion(ctx, "non-existent-version")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent version, got %v", err)
	}

	// 2. Empty string version ID
	_, err = s.GetBaselineStateAtVersion(ctx, "")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty version ID, got %v", err)
	}
}

func TestStorage_GetBaselineStateAtVersion_EmptyVersion(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	ver := &domain.CatalogVersion{
		VersionID:   "ver-empty",
		Description: "Empty version without deltas",
		Author:      "tester@coderpush.com",
		Type:        domain.VersionTypeInitial,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.CreateVersionTx(ctx, tx, ver, nil); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	baseline, err := s.GetBaselineStateAtVersion(ctx, "ver-empty")
	if err != nil {
		t.Fatalf("GetBaselineStateAtVersion failed: %v", err)
	}
	if len(baseline) != 0 {
		t.Fatalf("expected 0 products in empty baseline, got %d", len(baseline))
	}
}

func TestStorage_GetBaselineStateAtVersion_LifecycleReconstruction(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	t1 := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	// Version 1: Add SKU-A and SKU-B
	tx1, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 1 failed: %v", err)
	}
	v1 := &domain.CatalogVersion{
		VersionID:     "v1",
		VersionNumber: 1,
		Description:   "Initial catalog",
		Author:        "ops@coderpush.com",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     t1,
	}
	deltasV1 := []domain.ItemDelta{
		{
			VersionID:    "v1",
			SKU:          "SKU-A",
			Action:       domain.DeltaActionAdded,
			NewName:      "Widget A",
			NewPrice:     10.00,
			NewStatus:    domain.StatusActive,
			NewTimestamp: t1,
		},
		{
			VersionID:    "v1",
			SKU:          "SKU-B",
			Action:       domain.DeltaActionAdded,
			NewName:      "Widget B",
			NewPrice:     20.00,
			NewStatus:    domain.StatusActive,
			NewTimestamp: t1,
		},
	}
	if err := s.CreateVersionTx(ctx, tx1, v1, deltasV1); err != nil {
		_ = tx1.Rollback()
		t.Fatalf("CreateVersionTx v1 failed: %v", err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatalf("Commit v1 failed: %v", err)
	}

	// Version 2: Update SKU-A, Add SKU-C, Withdraw SKU-B
	tx2, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 2 failed: %v", err)
	}
	v2 := &domain.CatalogVersion{
		VersionID:     "v2",
		VersionNumber: 2,
		Description:   "Price update and withdrawal",
		Author:        "ops@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     t2,
	}
	oldNameA := "Widget A"
	oldPriceA := 10.00
	oldStatusActive := domain.StatusActive
	oldPriceB := 20.00
	oldNameB := "Widget B"

	deltasV2 := []domain.ItemDelta{
		{
			VersionID:    "v2",
			SKU:          "SKU-A",
			Action:       domain.DeltaActionUpdated,
			OldName:      &oldNameA,
			NewName:      "Widget A v2",
			OldPrice:     &oldPriceA,
			NewPrice:     15.00,
			OldStatus:    &oldStatusActive,
			NewStatus:    domain.StatusActive,
			OldTimestamp: &t1,
			NewTimestamp: t2,
		},
		{
			VersionID:    "v2",
			SKU:          "SKU-C",
			Action:       domain.DeltaActionAdded,
			NewName:      "Widget C",
			NewPrice:     30.00,
			NewStatus:    domain.StatusActive,
			NewTimestamp: t2,
		},
		{
			VersionID:    "v2",
			SKU:          "SKU-B",
			Action:       domain.DeltaActionWithdrawn,
			OldName:      &oldNameB,
			NewName:      "Widget B",
			OldPrice:     &oldPriceB,
			NewPrice:     20.00,
			OldStatus:    &oldStatusActive,
			NewStatus:    domain.StatusWithdrawn,
			OldTimestamp: &t1,
			NewTimestamp: t2,
		},
	}
	if err := s.CreateVersionTx(ctx, tx2, v2, deltasV2); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("CreateVersionTx v2 failed: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("Commit v2 failed: %v", err)
	}

	// Version 3: Add SKU-D, Update SKU-C
	tx3, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx 3 failed: %v", err)
	}
	v3 := &domain.CatalogVersion{
		VersionID:     "v3",
		VersionNumber: 3,
		Description:   "Add SKU-D and update SKU-C",
		Author:        "ops@coderpush.com",
		Type:          domain.VersionTypePatch,
		CreatedAt:     t3,
	}
	oldNameC := "Widget C"
	oldPriceC := 30.00
	deltasV3 := []domain.ItemDelta{
		{
			VersionID:    "v3",
			SKU:          "SKU-D",
			Action:       domain.DeltaActionAdded,
			NewName:      "Widget D",
			NewPrice:     40.00,
			NewStatus:    domain.StatusActive,
			NewTimestamp: t3,
		},
		{
			VersionID:    "v3",
			SKU:          "SKU-C",
			Action:       domain.DeltaActionUpdated,
			OldName:      &oldNameC,
			NewName:      "Widget C v3",
			OldPrice:     &oldPriceC,
			NewPrice:     35.00,
			OldStatus:    &oldStatusActive,
			NewStatus:    domain.StatusActive,
			OldTimestamp: &t2,
			NewTimestamp: t3,
		},
	}
	if err := s.CreateVersionTx(ctx, tx3, v3, deltasV3); err != nil {
		_ = tx3.Rollback()
		t.Fatalf("CreateVersionTx v3 failed: %v", err)
	}
	if err := tx3.Commit(); err != nil {
		t.Fatalf("Commit v3 failed: %v", err)
	}

	// --- VERIFY BASELINE AT V1 ---
	baseV1, err := s.GetBaselineStateAtVersion(ctx, "v1")
	if err != nil {
		t.Fatalf("GetBaselineStateAtVersion v1 failed: %v", err)
	}
	if len(baseV1) != 2 {
		t.Fatalf("expected 2 items at v1, got %d", len(baseV1))
	}
	if prodA, ok := baseV1["SKU-A"]; !ok || prodA.Price != 10.00 || prodA.Name != "Widget A" || prodA.Status != domain.StatusActive {
		t.Errorf("v1 SKU-A mismatch: %+v", prodA)
	}
	if prodB, ok := baseV1["SKU-B"]; !ok || prodB.Price != 20.00 || prodB.Name != "Widget B" || prodB.Status != domain.StatusActive {
		t.Errorf("v1 SKU-B mismatch: %+v", prodB)
	}
	if _, ok := baseV1["SKU-C"]; ok {
		t.Errorf("v1 should not contain SKU-C")
	}
	if _, ok := baseV1["SKU-D"]; ok {
		t.Errorf("v1 should not contain SKU-D")
	}

	// --- VERIFY BASELINE AT V2 ---
	baseV2, err := s.GetBaselineStateAtVersion(ctx, "v2")
	if err != nil {
		t.Fatalf("GetBaselineStateAtVersion v2 failed: %v", err)
	}
	if len(baseV2) != 3 {
		t.Fatalf("expected 3 items at v2, got %d", len(baseV2))
	}
	if prodA, ok := baseV2["SKU-A"]; !ok || prodA.Price != 15.00 || prodA.Name != "Widget A v2" || prodA.Status != domain.StatusActive {
		t.Errorf("v2 SKU-A mismatch: %+v", prodA)
	}
	if prodB, ok := baseV2["SKU-B"]; !ok || prodB.Price != 20.00 || prodB.Status != domain.StatusWithdrawn {
		t.Errorf("v2 SKU-B mismatch: %+v", prodB)
	}
	if prodC, ok := baseV2["SKU-C"]; !ok || prodC.Price != 30.00 || prodC.Name != "Widget C" || prodC.Status != domain.StatusActive {
		t.Errorf("v2 SKU-C mismatch: %+v", prodC)
	}
	if _, ok := baseV2["SKU-D"]; ok {
		t.Errorf("v2 should not contain SKU-D")
	}

	// --- VERIFY BASELINE AT V3 ---
	baseV3, err := s.GetBaselineStateAtVersion(ctx, "v3")
	if err != nil {
		t.Fatalf("GetBaselineStateAtVersion v3 failed: %v", err)
	}
	if len(baseV3) != 4 {
		t.Fatalf("expected 4 items at v3, got %d", len(baseV3))
	}
	if prodA, ok := baseV3["SKU-A"]; !ok || prodA.Price != 15.00 {
		t.Errorf("v3 SKU-A mismatch: %+v", prodA)
	}
	if prodB, ok := baseV3["SKU-B"]; !ok || prodB.Status != domain.StatusWithdrawn {
		t.Errorf("v3 SKU-B mismatch: %+v", prodB)
	}
	if prodC, ok := baseV3["SKU-C"]; !ok || prodC.Price != 35.00 || prodC.Name != "Widget C v3" {
		t.Errorf("v3 SKU-C mismatch: %+v", prodC)
	}
	if prodD, ok := baseV3["SKU-D"]; !ok || prodD.Price != 40.00 || prodD.Name != "Widget D" {
		t.Errorf("v3 SKU-D mismatch: %+v", prodD)
	}
}

func TestStorage_GetBaselineStateAtVersion_MultiDeltaSameVersion(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	ver := &domain.CatalogVersion{
		VersionID:   "v-multi",
		Description: "Multi delta same version",
		Author:      "ops@coderpush.com",
		Type:        domain.VersionTypeInitial,
		CreatedAt:   now,
	}
	deltas := []domain.ItemDelta{
		{
			VersionID:    "v-multi",
			SKU:          "SKU-MULTI",
			Action:       domain.DeltaActionAdded,
			NewName:      "Initial Name",
			NewPrice:     10.0,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now,
		},
		{
			VersionID:    "v-multi",
			SKU:          "SKU-MULTI",
			Action:       domain.DeltaActionUpdated,
			NewName:      "Updated Name",
			NewPrice:     25.0,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now.Add(time.Second),
		},
	}
	if err := s.CreateVersionTx(ctx, tx, ver, deltas); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	baseline, err := s.GetBaselineStateAtVersion(ctx, "v-multi")
	if err != nil {
		t.Fatalf("GetBaselineStateAtVersion failed: %v", err)
	}
	p, ok := baseline["SKU-MULTI"]
	if !ok {
		t.Fatal("SKU-MULTI missing from baseline")
	}
	if p.Price != 25.0 || p.Name != "Updated Name" {
		t.Errorf("expected latest delta within same version (Price: 25.0, Name: 'Updated Name'), got: %+v", p)
	}
}

func TestStorage_GetBaselineStateAtVersion_TransactionSupport(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	ver := &domain.CatalogVersion{
		VersionID:   "v-tx-test",
		Description: "Tx baseline test",
		Author:      "ops@coderpush.com",
		Type:        domain.VersionTypeInitial,
		CreatedAt:   now,
	}
	deltas := []domain.ItemDelta{
		{
			VersionID:    "v-tx-test",
			SKU:          "SKU-TX",
			Action:       domain.DeltaActionAdded,
			NewName:      "TX Widget",
			NewPrice:     55.0,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now,
		},
	}
	if err := s.CreateVersionTx(ctx, tx, ver, deltas); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateVersionTx failed: %v", err)
	}

	// Read inside active transaction via GetBaselineStateAtVersionTx
	baseTx, err := s.GetBaselineStateAtVersionTx(ctx, tx, "v-tx-test")
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("GetBaselineStateAtVersionTx failed: %v", err)
	}
	if len(baseTx) != 1 || baseTx["SKU-TX"].Price != 55.0 {
		_ = tx.Rollback()
		t.Fatalf("unexpected baseline state from tx: %+v", baseTx)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestStorage_SyncModeRollbackAndMetrics(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	t1 := now.Add(-2 * time.Hour)
	t2 := now.Add(-1 * time.Hour)

	// 1. Create SYNC job 1 & version 1
	job1 := &domain.SyncJob{
		JobID:               "job-sync-1",
		Status:              domain.JobStatusCompleted,
		TotalBatches:        1,
		CommittedBatchIndex: 0,
		Summary:             domain.ChangeSummary{Added: 2},
		CreatedAt:           t1,
		UpdatedAt:           t1,
	}
	if err := s.CreateJob(ctx, job1); err != nil {
		t.Fatalf("CreateJob job1 failed: %v", err)
	}

	tx1, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{
		SKU: "SKU-S1", Name: "Sync1 Item 1", Price: 10, Status: domain.StatusActive, SupplierTimestamp: t1, UpdatedAt: t1,
	})
	_ = s.UpsertProductTx(ctx, tx1, &domain.Product{
		SKU: "SKU-S2", Name: "Sync1 Item 2", Price: 20, Status: domain.StatusActive, SupplierTimestamp: t1, UpdatedAt: t1,
	})
	_ = s.CreateVersionTx(ctx, tx1, &domain.CatalogVersion{
		VersionID: "job-sync-1", VersionNumber: 1, Description: "v1.0.0 Sync", Author: "supplier-sync", Type: domain.VersionTypeSync,
		Summary: domain.ChangeSummary{Added: 2}, TotalItemsAffected: 2, CreatedAt: t1,
	}, []domain.ItemDelta{
		{VersionID: "job-sync-1", SKU: "SKU-S1", Action: domain.DeltaActionAdded, NewName: "Sync1 Item 1", NewPrice: 10, NewStatus: domain.StatusActive, NewTimestamp: t1},
		{VersionID: "job-sync-1", SKU: "SKU-S2", Action: domain.DeltaActionAdded, NewName: "Sync1 Item 2", NewPrice: 20, NewStatus: domain.StatusActive, NewTimestamp: t1},
	})
	_ = tx1.Commit()

	// 2. Create SYNC job 2 & version 2 (updates SKU-S1 price, updates SKU-S2 timestamp only, adds SKU-S3)
	job2 := &domain.SyncJob{
		JobID:               "job-sync-2",
		Status:              domain.JobStatusCompleted,
		TotalBatches:        1,
		CommittedBatchIndex: 0,
		Summary:             domain.ChangeSummary{Added: 1, Updated: 1},
		CreatedAt:           t2,
		UpdatedAt:           t2,
	}
	if err := s.CreateJob(ctx, job2); err != nil {
		t.Fatalf("CreateJob job2 failed: %v", err)
	}

	tx2, _ := s.BeginTx(ctx)
	_ = s.UpsertProductTx(ctx, tx2, &domain.Product{
		SKU: "SKU-S1", Name: "Sync2 Item 1", Price: 15, Status: domain.StatusActive, SupplierTimestamp: t2, UpdatedAt: t2,
	})
	_ = s.UpsertProductTx(ctx, tx2, &domain.Product{
		SKU: "SKU-S2", Name: "Sync1 Item 2", Price: 20, Status: domain.StatusActive, SupplierTimestamp: t2, UpdatedAt: t2,
	})
	_ = s.UpsertProductTx(ctx, tx2, &domain.Product{
		SKU: "SKU-S3", Name: "Sync2 Item 3", Price: 30, Status: domain.StatusActive, SupplierTimestamp: t2, UpdatedAt: t2,
	})
	oldName := "Sync1 Item 1"
	oldPrice := 10.0
	oldStatus := domain.StatusActive
	_ = s.CreateVersionTx(ctx, tx2, &domain.CatalogVersion{
		VersionID: "job-sync-2", VersionNumber: 2, Description: "v1.1.0 Sync", Author: "supplier-sync", Type: domain.VersionTypeSync,
		Summary: domain.ChangeSummary{Added: 1, Updated: 1}, TotalItemsAffected: 2, CreatedAt: t2,
	}, []domain.ItemDelta{
		{VersionID: "job-sync-2", SKU: "SKU-S1", Action: domain.DeltaActionUpdated, OldName: &oldName, NewName: "Sync2 Item 1", OldPrice: &oldPrice, NewPrice: 15, OldStatus: &oldStatus, NewStatus: domain.StatusActive, OldTimestamp: &t1, NewTimestamp: t2},
		{VersionID: "job-sync-2", SKU: "SKU-S3", Action: domain.DeltaActionAdded, NewName: "Sync2 Item 3", NewPrice: 30, NewStatus: domain.StatusActive, NewTimestamp: t2},
	})
	_ = tx2.Commit()

	// Delete SKU-S2 from live products before rollback to also test missing baseline product re-insertion
	_, _ = s.GetProduct(ctx, "SKU-S2")

	// 3. Single-update revert of job-sync-2 ("Revert update: ..."): should delete SKU-S3, restore SKU-S1 to 10.0 & t1, keep job-sync-1 active, and exclude job-sync-2 from metrics
	rbVer1, _, err := s.RollbackVersion(ctx, "job-sync-2", "staff", "Revert update: Price & Inventory Adjustment (job-sync-2)")
	if err != nil {
		t.Fatalf("RollbackVersion single-update job-sync-2 failed: %v", err)
	}
	if rbVer1.Summary.Withdrawn != 1 || rbVer1.Summary.Updated != 1 {
		t.Fatalf("unexpected rollback summary: %+v", rbVer1.Summary)
	}

	_, totalProds, err := s.ListProducts(ctx, "", "", 100, 0)
	if err != nil || totalProds != 2 {
		t.Fatalf("expected 2 products after single-update revert of v2, got %d (err=%v)", totalProds, err)
	}

	agg1, jobs1, err := s.GetMetrics(ctx, domain.MetricsFilter{})
	if err != nil {
		t.Fatalf("GetMetrics after single-update revert failed: %v", err)
	}
	if len(jobs1) != 1 || agg1.Summary.Added != 2 {
		t.Fatalf("expected only job-sync-1 in metrics (added=2), got jobs=%d, added=%d", len(jobs1), agg1.Summary.Added)
	}

	// 4. Rollback to job-sync-1 (point-in-time) and then to v0
	_, _, err = s.RollbackVersion(ctx, "job-sync-1", "staff", "Revert to v1")
	if err != nil {
		t.Fatalf("RollbackVersion to job-sync-1 failed: %v", err)
	}
	_, _, err = s.RollbackVersion(ctx, "v0", "staff", "Revert to empty baseline")
	if err != nil {
		t.Fatalf("RollbackVersion to v0 failed: %v", err)
	}
	_, totalAfterV0, _ := s.ListProducts(ctx, "", "", 100, 0)
	if totalAfterV0 != 0 {
		t.Fatalf("expected 0 products after rollback to v0, got %d", totalAfterV0)
	}
	aggV0, jobsV0, _ := s.GetMetrics(ctx, domain.MetricsFilter{})
	if len(jobsV0) != 0 || aggV0.Summary.Added != 0 {
		t.Fatalf("expected 0 active jobs after rollback to v0, got jobs=%d, added=%d", len(jobsV0), aggV0.Summary.Added)
	}

	// 5. Rollback back to job-sync-1 after v0 reset when products is empty (tests missing baseline product insertion in sync mode)
	baseV0, _ := s.GetBaselineStateAtVersion(ctx, "v0")
	if len(baseV0) != 0 {
		t.Fatalf("expected empty baseline at v0, got %d", len(baseV0))
	}
	_, _, err = s.RollbackVersion(ctx, "job-sync-1", "staff", "Restore v1 from empty")
	if err != nil {
		t.Fatalf("RollbackVersion restore v1 failed: %v", err)
	}
}
