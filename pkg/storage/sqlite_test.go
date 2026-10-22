package storage_test

import (
	"context"
	"errors"
	"fmt"
	"math"
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

	// 3. Resolve with closed DB
	sClosed, _ := storage.OpenInMemory()
	_ = sClosed.Close()
	if err := sClosed.ResolveDeadLetter(ctx, "issue-x", corrected); err == nil {
		t.Error("expected error on closed DB")
	}
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
