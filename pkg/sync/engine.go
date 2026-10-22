package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	stdsync "sync"
	"time"

	"coderpush/pkg/domain"
)

var (
	ErrJobAlreadyRunning   = errors.New("job is already running")
	ErrJobAlreadyCompleted = errors.New("job is already completed")
	ErrEmptyJobID          = errors.New("job ID cannot be empty")
	ErrNilBatchStream      = errors.New("batch stream cannot be nil")
	ErrNilStorage          = errors.New("sync engine storage is nil")
)

// Storage defines the storage contract required by the synchronization engine.
type Storage interface {
	DB() *sql.DB
	Close() error
	BeginTx(ctx context.Context) (*sql.Tx, error)

	// Products
	GetProduct(ctx context.Context, sku string) (*domain.Product, error)
	GetProductTx(ctx context.Context, tx *sql.Tx, sku string) (*domain.Product, error)
	UpsertProductTx(ctx context.Context, tx *sql.Tx, p *domain.Product) error

	// Jobs & Checkpoints
	CreateJob(ctx context.Context, job *domain.SyncJob) error
	GetJob(ctx context.Context, jobID string) (*domain.SyncJob, error)
	GetActiveJob(ctx context.Context) (*domain.SyncJob, error)
	UpdateJobStatus(ctx context.Context, jobID string, status domain.JobStatus) error
	RecordBatchCheckpointTx(ctx context.Context, tx *sql.Tx, cp *domain.BatchCheckpoint) error
	UpdateJobProgressTx(ctx context.Context, tx *sql.Tx, jobID string, batchIndex int, delta domain.ChangeSummary) error
	RecoverInterruptedJobs(ctx context.Context) ([]string, error)

	// Dead Letter
	RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error
	ListDeadLetterRecords(ctx context.Context, jobID string, reason string, limit, offset int) ([]domain.DeadLetterRecord, int, error)
}

// Engine defines high-level synchronization operations.
type Engine interface {
	ExecuteBatch(ctx context.Context, batch domain.Batch) (domain.ChangeSummary, error)
	RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
	ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)
	RecoverOnStartup(ctx context.Context) ([]string, error)
}

// ProgressHook receives incremental delta and cumulative change metrics after each batch commits.
type ProgressHook func(jobID string, batchIndex int, totalBatches int, delta domain.ChangeSummary, cumulative domain.ChangeSummary)

// SyncEngine implements Engine with strict enforcement of Invariants A, B, C, D, and E.
type SyncEngine struct {
	storage      Storage
	mu           stdsync.Mutex // Serializes batch writes for SQLite single-writer safety
	now          func() time.Time
	activeJobs   map[string]bool // Tracks actively running job IDs to prevent duplicate execution
	progressHook ProgressHook    // Optional progress listener hook
}

// Ensure SyncEngine implements Engine at compile time.
var _ Engine = (*SyncEngine)(nil)

// Option allows configuring SyncEngine options (e.g. clock mocking for tests).
type Option func(*SyncEngine)

// WithTimeFunc configures a custom clock provider for deterministic testing.
func WithTimeFunc(now func() time.Time) Option {
	return func(e *SyncEngine) {
		e.now = now
	}
}

// WithProgressHook attaches an event callback to SyncEngine invoked after each batch commits.
func WithProgressHook(hook ProgressHook) Option {
	return func(e *SyncEngine) {
		e.progressHook = hook
	}
}

// NewEngine initializes a new SyncEngine instance with supplied storage and options.
func NewEngine(store Storage, opts ...Option) *SyncEngine {
	e := &SyncEngine{
		storage:    store,
		now:        func() time.Time { return time.Now().UTC() },
		activeJobs: make(map[string]bool),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ExecuteBatch processes a single batch inside an atomic transaction, enforcing Invariants A, B, C, D, E.
func (e *SyncEngine) ExecuteBatch(ctx context.Context, batch domain.Batch) (domain.ChangeSummary, error) {
	if len(batch.Records) == 0 {
		return domain.ChangeSummary{}, nil
	}

	if err := ctx.Err(); err != nil {
		return domain.ChangeSummary{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return domain.ChangeSummary{}, err
	}

	if e.storage == nil {
		return domain.ChangeSummary{}, ErrNilStorage
	}

	tx, err := e.storage.BeginTx(ctx)
	if err != nil {
		return domain.ChangeSummary{}, fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	summary, err := e.executeRecordsTx(ctx, tx, batch)
	if err != nil {
		return domain.ChangeSummary{}, err
	}

	if err := tx.Commit(); err != nil {
		return domain.ChangeSummary{}, fmt.Errorf("failed to commit batch transaction: %w", err)
	}

	return summary, nil
}

// executeRecordsTx processes individual records and dead letters within an active transaction.
func (e *SyncEngine) executeRecordsTx(ctx context.Context, tx *sql.Tx, batch domain.Batch) (domain.ChangeSummary, error) {
	summary := domain.ChangeSummary{}
	batchState := make(map[string]*domain.Product)

	for i, record := range batch.Records {
		if err := ctx.Err(); err != nil {
			return domain.ChangeSummary{}, err
		}

		sku := strings.TrimSpace(record.SKU)
		recordTimestamp := record.Timestamp.UTC()

		// Invariant D: Dead-Letter Isolation
		if isInvalidRecord(record) {
			summary.DeadLetter++
			issueID := fmt.Sprintf("dl-%s-%d-%d-%d", batch.JobID, batch.BatchIndex, i, e.now().UnixNano())
			if batch.JobID == "" {
				issueID = fmt.Sprintf("dl-%d-%d-%d", batch.BatchIndex, i, e.now().UnixNano())
			}

			rawBytes, jsonErr := json.Marshal(record)
			rawPayload := string(rawBytes)
			if jsonErr != nil {
				rawPayload = fmt.Sprintf("SKU:%s, Name:%s, Price:%f, Status:%s, Timestamp:%s",
					record.SKU, record.Name, record.Price, record.Status, record.Timestamp.Format(time.RFC3339Nano))
			}

			dlRecord := &domain.DeadLetterRecord{
				IssueID:    issueID,
				JobID:      batch.JobID,
				BatchIndex: batch.BatchIndex,
				SKU:        sku,
				RawPayload: rawPayload,
				Reason:     getInvalidReason(record),
				CreatedAt:  e.now(),
			}

			if err := e.storage.RecordDeadLetterTx(ctx, tx, dlRecord); err != nil {
				return domain.ChangeSummary{}, fmt.Errorf("failed to record dead letter issue: %w", err)
			}
			continue
		}

		// Step 1: Resolve existing product state
		existingProduct, foundInBatch := batchState[sku]
		if !foundInBatch {
			var err error
			existingProduct, err = e.storage.GetProductTx(ctx, tx, sku)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
					existingProduct = nil
				} else {
					return domain.ChangeSummary{}, fmt.Errorf("failed to query product %s at index %d: %w", sku, i, err)
				}
			}
			batchState[sku] = existingProduct
		}

		// Step 2: New insertion
		if existingProduct == nil {
			newProduct := &domain.Product{
				SKU:               sku,
				Name:              record.Name,
				Price:             record.Price,
				Status:            record.Status,
				SupplierTimestamp: recordTimestamp,
				UpdatedAt:         e.now(),
			}
			if err := e.storage.UpsertProductTx(ctx, tx, newProduct); err != nil {
				return domain.ChangeSummary{}, fmt.Errorf("failed to insert product %s: %w", sku, err)
			}
			batchState[sku] = newProduct
			summary.Added++
			continue
		}

		// Step 3: Evaluate Invariants A, C, B
		if isIdenticalProduct(existingProduct, record, recordTimestamp) {
			summary.Skipped++
			continue
		}
		if existingProduct.Status == domain.StatusWithdrawn && record.Status == domain.StatusActive {
			summary.Skipped++
			continue
		}
		if recordTimestamp.Before(existingProduct.SupplierTimestamp) || recordTimestamp.Equal(existingProduct.SupplierTimestamp) {
			summary.Skipped++
			continue
		}

		// Step 4: Valid update
		wasWithdrawn := existingProduct.Status == domain.StatusWithdrawn
		nowWithdrawn := record.Status == domain.StatusWithdrawn

		updatedProduct := &domain.Product{
			SKU:               sku,
			Name:              record.Name,
			Price:             record.Price,
			Status:            record.Status,
			SupplierTimestamp: recordTimestamp,
			UpdatedAt:         e.now(),
		}
		if err := e.storage.UpsertProductTx(ctx, tx, updatedProduct); err != nil {
			return domain.ChangeSummary{}, fmt.Errorf("failed to update product %s: %w", sku, err)
		}
		batchState[sku] = updatedProduct

		if !wasWithdrawn && nowWithdrawn {
			summary.Withdrawn++
		} else {
			summary.Updated++
		}
	}

	return summary, nil
}

// processBatchTx executes a batch within an atomic database transaction including checkpoint and progress.
func (e *SyncEngine) processBatchTx(ctx context.Context, job *domain.SyncJob, batch domain.Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if e.storage == nil {
		return ErrNilStorage
	}

	tx, err := e.storage.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin batch transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 1. Process records and dead letters inside transaction
	summary, err := e.executeRecordsTx(ctx, tx, batch)
	if err != nil {
		return err
	}

	// 2. Insert batch checkpoint
	checkpoint := &domain.BatchCheckpoint{
		JobID:       job.JobID,
		BatchIndex:  batch.BatchIndex,
		ItemsCount:  len(batch.Records),
		Summary:     summary,
		CommittedAt: e.now(),
	}
	if err := e.storage.RecordBatchCheckpointTx(ctx, tx, checkpoint); err != nil {
		return fmt.Errorf("failed to record batch checkpoint %s[%d]: %w", job.JobID, batch.BatchIndex, err)
	}

	// 3. Atomically update job cumulative progress
	if err := e.storage.UpdateJobProgressTx(ctx, tx, job.JobID, batch.BatchIndex, summary); err != nil {
		return fmt.Errorf("failed to update job progress for %s[%d]: %w", job.JobID, batch.BatchIndex, err)
	}

	// Verify context is still active prior to committing transaction
	if err := ctx.Err(); err != nil {
		return err
	}

	// 4. Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch %s[%d]: %w", job.JobID, batch.BatchIndex, err)
	}

	// 5. Update in-memory job state only after successful commit
	job.CommittedBatchIndex = batch.BatchIndex
	job.Summary.Add(summary)
	job.UpdatedAt = e.now()

	if e.progressHook != nil {
		e.progressHook(job.JobID, batch.BatchIndex, job.TotalBatches, summary, job.Summary)
	}

	return nil
}

// executeJob consumes batches sequentially from stream and applies atomic transactions.
func (e *SyncEngine) executeJob(ctx context.Context, job *domain.SyncJob, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
	for {
		select {
		case <-ctx.Done():
			e.markInterrupted(job)
			return job, ctx.Err()

		case batch, ok := <-batchStream:
			if !ok {
				// Channel closed -> all batches finished!
				goto streamCompleted
			}

			// Ensure batch associates with the active job ID
			batch.JobID = job.JobID

			// Checkpoint Check: Skip batches that have already been committed in previous runs
			if batch.BatchIndex <= job.CommittedBatchIndex {
				continue
			}

			// Gap detection: Next uncommitted batch MUST be exactly job.CommittedBatchIndex + 1
			if batch.BatchIndex > job.CommittedBatchIndex+1 {
				err := fmt.Errorf("gap detected in batch sequence: received batch index %d, expected %d", batch.BatchIndex, job.CommittedBatchIndex+1)
				e.markInterrupted(job)
				return job, err
			}

			// Process and commit batch atomically
			if err := e.processBatchTx(ctx, job, batch); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					e.markInterrupted(job)
					return job, ctx.Err()
				}
				e.markFailed(job)
				return job, err
			}
		}
	}

streamCompleted:
	if err := ctx.Err(); err != nil {
		e.markInterrupted(job)
		return job, err
	}

	// If total_batches was set > 0, verify all batches were received before marking completed
	if job.TotalBatches > 0 && job.CommittedBatchIndex+1 < job.TotalBatches {
		e.markInterrupted(job)
		return job, fmt.Errorf("stream closed prematurely: committed %d of %d batches", job.CommittedBatchIndex+1, job.TotalBatches)
	}

	if job.TotalBatches == 0 {
		job.TotalBatches = job.CommittedBatchIndex + 1
	}

	now := e.now()
	job.Status = domain.JobStatusCompleted
	job.CompletedAt = &now
	job.UpdatedAt = now
	if err := e.storage.UpdateJobStatus(ctx, job.JobID, domain.JobStatusCompleted); err != nil {
		return job, fmt.Errorf("failed to update job status to completed: %w", err)
	}
	return job, nil
}

// markInterrupted transitions job to INTERRUPTED using a background context with timeout and retry.
func (e *SyncEngine) markInterrupted(job *domain.SyncJob) {
	if e.storage != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for attempt := 0; attempt < 10; attempt++ {
			err := e.storage.UpdateJobStatus(cleanupCtx, job.JobID, domain.JobStatusInterrupted)
			if err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	job.Status = domain.JobStatusInterrupted
	job.UpdatedAt = e.now()
}

// markFailed transitions job to FAILED using a background context with timeout and retry.
func (e *SyncEngine) markFailed(job *domain.SyncJob) {
	if e.storage != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for attempt := 0; attempt < 10; attempt++ {
			err := e.storage.UpdateJobStatus(cleanupCtx, job.JobID, domain.JobStatusFailed)
			if err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	job.Status = domain.JobStatusFailed
	job.UpdatedAt = e.now()
}

// RunJob processes all batches from a batch reader/generator, handling checkpoints and crash resumability.
func (e *SyncEngine) RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
	if strings.TrimSpace(jobID) == "" {
		return nil, ErrEmptyJobID
	}
	if batchStream == nil {
		return nil, ErrNilBatchStream
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.storage == nil {
		return nil, ErrNilStorage
	}

	e.mu.Lock()
	if e.activeJobs[jobID] {
		e.mu.Unlock()
		return nil, fmt.Errorf("job %s is already running: %w", jobID, ErrJobAlreadyRunning)
	}
	e.activeJobs[jobID] = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.activeJobs, jobID)
		e.mu.Unlock()
	}()

	job, err := e.storage.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Create new job
			now := e.now()
			job = &domain.SyncJob{
				JobID:               jobID,
				Status:              domain.JobStatusRunning,
				TotalBatches:        0,
				CommittedBatchIndex: -1,
				Summary:             domain.ChangeSummary{},
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			if err := e.storage.CreateJob(ctx, job); err != nil {
				return nil, fmt.Errorf("failed to create sync job %s: %w", jobID, err)
			}
		} else {
			return nil, fmt.Errorf("failed to lookup job %s: %w", jobID, err)
		}
	} else {
		// Existing job validation
		if job.Status == domain.JobStatusCompleted {
			return nil, fmt.Errorf("job %s is already completed: %w", jobID, ErrJobAlreadyCompleted)
		}
		if job.Status != domain.JobStatusRunning {
			if err := e.storage.UpdateJobStatus(ctx, jobID, domain.JobStatusRunning); err != nil {
				return nil, fmt.Errorf("failed to transition job %s to RUNNING: %w", jobID, err)
			}
			job.Status = domain.JobStatusRunning
		}
	}

	return e.executeJob(ctx, job, batchStream)
}

// ResumeJob restarts an interrupted job from its last committed batch checkpoint.
func (e *SyncEngine) ResumeJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error) {
	if strings.TrimSpace(jobID) == "" {
		return nil, ErrEmptyJobID
	}
	if batchStream == nil {
		return nil, ErrNilBatchStream
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.storage == nil {
		return nil, ErrNilStorage
	}

	e.mu.Lock()
	if e.activeJobs[jobID] {
		e.mu.Unlock()
		return nil, fmt.Errorf("job %s is already running: %w", jobID, ErrJobAlreadyRunning)
	}
	e.activeJobs[jobID] = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.activeJobs, jobID)
		e.mu.Unlock()
	}()

	job, err := e.storage.GetJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("cannot resume non-existent job %s: %w", jobID, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to fetch job %s for resume: %w", jobID, err)
	}

	if job.Status == domain.JobStatusCompleted {
		return nil, fmt.Errorf("job %s is already completed: %w", jobID, ErrJobAlreadyCompleted)
	}

	// Transition to RUNNING
	if job.Status != domain.JobStatusRunning {
		if err := e.storage.UpdateJobStatus(ctx, jobID, domain.JobStatusRunning); err != nil {
			return nil, fmt.Errorf("failed to transition job %s to RUNNING: %w", jobID, err)
		}
		job.Status = domain.JobStatusRunning
	}

	return e.executeJob(ctx, job, batchStream)
}

// RecoverOnStartup checks for running jobs and transitions them to INTERRUPTED.
func (e *SyncEngine) RecoverOnStartup(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.storage == nil {
		return nil, ErrNilStorage
	}
	return e.storage.RecoverInterruptedJobs(ctx)
}

// --- Helper Functions ---

func isIdenticalProduct(p *domain.Product, r domain.SupplierUpdate, rTimestamp time.Time) bool {
	if p.SKU != strings.TrimSpace(r.SKU) {
		return false
	}
	if p.Name != r.Name {
		return false
	}
	if !priceEqual(p.Price, r.Price) {
		return false
	}
	if p.Status != r.Status {
		return false
	}
	if !p.SupplierTimestamp.Equal(rTimestamp) {
		return false
	}
	return true
}

func priceEqual(a, b float64) bool {
	const epsilon = 1e-6
	return math.Abs(a-b) < epsilon
}

func isInvalidRecord(r domain.SupplierUpdate) bool {
	if strings.TrimSpace(r.SKU) == "" {
		return true
	}
	if math.IsNaN(r.Price) || math.IsInf(r.Price, 0) || r.Price < 0 {
		return true
	}
	if !r.Status.IsValid() {
		return true
	}
	if r.Timestamp.IsZero() {
		return true
	}
	return false
}

func getInvalidReason(r domain.SupplierUpdate) string {
	var reasons []string
	if strings.TrimSpace(r.SKU) == "" {
		reasons = append(reasons, "empty SKU")
	}
	if math.IsNaN(r.Price) {
		reasons = append(reasons, "price is NaN")
	} else if math.IsInf(r.Price, 0) {
		reasons = append(reasons, "price is infinite")
	} else if r.Price < 0 {
		reasons = append(reasons, fmt.Sprintf("negative price: %f", r.Price))
	}
	if !r.Status.IsValid() {
		reasons = append(reasons, fmt.Sprintf("invalid status: %s", r.Status))
	}
	if r.Timestamp.IsZero() {
		reasons = append(reasons, "zero or uninitialized timestamp")
	}
	return strings.Join(reasons, "; ")
}
