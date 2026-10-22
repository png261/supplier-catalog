package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"coderpush/pkg/domain"

	_ "modernc.org/sqlite"
)

// Storage defines the complete repository interface for products, sync jobs, and audit records.
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
	ResolveDeadLetter(ctx context.Context, issueID string, p *domain.Product) error

	// Catalog & Audit Queries
	ListJobs(ctx context.Context, limit, offset int) ([]domain.SyncJob, int, error)
	ListProducts(ctx context.Context, status string, search string, limit, offset int) ([]domain.Product, int, error)
	GetBatchCheckpoints(ctx context.Context, jobID string) ([]domain.BatchCheckpoint, error)

	// Maintenance
	Reset(ctx context.Context) error
}

// SQLiteStorage implements Storage backed by embedded SQLite in WAL mode.
type SQLiteStorage struct {
	db *sql.DB
}

// Ensure SQLiteStorage implements Storage at compile time.
var _ Storage = (*SQLiteStorage)(nil)

// Open initializes SQLiteStorage, configures WAL pragmas and connection limits, and executes DDL migrations.
func Open(dsn string) (*SQLiteStorage, error) {
	formattedDSN := dsn
	if dsn == ":memory:" {
		formattedDSN = "file::memory:?cache=shared&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_txlock=immediate"
	} else if !strings.Contains(dsn, "?") {
		formattedDSN = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_txlock=immediate", dsn)
	}

	db, err := sql.Open("sqlite", formattedDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Single connection serializes all writes, preventing SQLITE_BUSY deadlocks and race conditions.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	s := &SQLiteStorage{db: db}
	if err := s.initSchema(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return s, nil
}

// OpenInMemory initializes an in-memory SQLiteStorage instance ideal for tests.
func OpenInMemory() (*SQLiteStorage, error) {
	return Open(":memory:")
}

// DB returns the underlying sql.DB instance.
func (s *SQLiteStorage) DB() *sql.DB {
	return s.db
}

// Close gracefully terminates database connections.
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// BeginTx starts an atomic transaction with immediate locking behavior.
func (s *SQLiteStorage) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// initSchema applies the DDL schema for products, sync_jobs, batch_checkpoints, and dead_letter_issues.
func (s *SQLiteStorage) initSchema(ctx context.Context) error {
	ddl := `
	PRAGMA busy_timeout = 5000;
	PRAGMA foreign_keys = ON;

	CREATE TABLE IF NOT EXISTS products (
		sku TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		price REAL NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('ACTIVE', 'WITHDRAWN')),
		supplier_timestamp TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_products_status ON products(status);

	CREATE TABLE IF NOT EXISTS sync_jobs (
		job_id TEXT PRIMARY KEY,
		status TEXT NOT NULL CHECK(status IN ('PENDING', 'RUNNING', 'INTERRUPTED', 'COMPLETED', 'FAILED')),
		total_batches INTEGER NOT NULL DEFAULT 0,
		committed_batch_index INTEGER NOT NULL DEFAULT -1,
		items_added INTEGER NOT NULL DEFAULT 0,
		items_updated INTEGER NOT NULL DEFAULT 0,
		items_withdrawn INTEGER NOT NULL DEFAULT 0,
		items_skipped INTEGER NOT NULL DEFAULT 0,
		items_dead_letter INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		completed_at TEXT
	);

	CREATE TABLE IF NOT EXISTS batch_checkpoints (
		job_id TEXT NOT NULL,
		batch_index INTEGER NOT NULL,
		items_count INTEGER NOT NULL,
		added_count INTEGER NOT NULL,
		updated_count INTEGER NOT NULL,
		withdrawn_count INTEGER NOT NULL,
		skipped_count INTEGER NOT NULL,
		dead_letter_count INTEGER NOT NULL,
		committed_at TEXT NOT NULL,
		PRIMARY KEY (job_id, batch_index),
		FOREIGN KEY (job_id) REFERENCES sync_jobs(job_id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS dead_letter_issues (
		issue_id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL,
		batch_index INTEGER NOT NULL,
		sku TEXT,
		raw_payload TEXT NOT NULL,
		reason TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_issues_job_id ON dead_letter_issues(job_id, batch_index);
	`
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

// rowQuerier abstracts *sql.DB and *sql.Tx for row queries.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// GetProduct retrieves a product by its SKU from the database.
func (s *SQLiteStorage) GetProduct(ctx context.Context, sku string) (*domain.Product, error) {
	return s.getProduct(ctx, s.db, sku)
}

// GetProductTx retrieves a product within an active transaction boundary.
func (s *SQLiteStorage) GetProductTx(ctx context.Context, tx *sql.Tx, sku string) (*domain.Product, error) {
	return s.getProduct(ctx, tx, sku)
}

func (s *SQLiteStorage) getProduct(ctx context.Context, q rowQuerier, sku string) (*domain.Product, error) {
	query := `SELECT sku, name, price, status, supplier_timestamp, updated_at FROM products WHERE sku = ?;`
	row := q.QueryRowContext(ctx, query, sku)

	var p domain.Product
	var statusStr, supplierTimeStr, updatedTimeStr string

	err := row.Scan(&p.SKU, &p.Name, &p.Price, &statusStr, &supplierTimeStr, &updatedTimeStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan product %s: %w", sku, err)
	}

	p.Status = domain.ProductStatus(statusStr)

	st, err := ParseTime(supplierTimeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid supplier_timestamp for %s: %w", sku, err)
	}
	p.SupplierTimestamp = st

	ut, err := ParseTime(updatedTimeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid updated_at for %s: %w", sku, err)
	}
	p.UpdatedAt = ut

	return &p, nil
}

// UpsertProductTx writes or updates a product atomically inside a transaction.
func (s *SQLiteStorage) UpsertProductTx(ctx context.Context, tx *sql.Tx, p *domain.Product) error {
	query := `
	INSERT INTO products (sku, name, price, status, supplier_timestamp, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(sku) DO UPDATE SET
		name = excluded.name,
		price = excluded.price,
		status = excluded.status,
		supplier_timestamp = excluded.supplier_timestamp,
		updated_at = excluded.updated_at;
	`
	_, err := tx.ExecContext(ctx, query,
		p.SKU,
		p.Name,
		p.Price,
		string(p.Status),
		p.SupplierTimestamp.UTC().Format(time.RFC3339Nano),
		p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert product %s: %w", p.SKU, err)
	}
	return nil
}

// CreateJob persists a new sync job record.
func (s *SQLiteStorage) CreateJob(ctx context.Context, job *domain.SyncJob) error {
	query := `
	INSERT INTO sync_jobs (
		job_id, status, total_batches, committed_batch_index,
		items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err := s.db.ExecContext(ctx, query,
		job.JobID,
		string(job.Status),
		job.TotalBatches,
		job.CommittedBatchIndex,
		job.Summary.Added,
		job.Summary.Updated,
		job.Summary.Withdrawn,
		job.Summary.Skipped,
		job.Summary.DeadLetter,
		job.CreatedAt.UTC().Format(time.RFC3339Nano),
		job.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to create job %s: %w", job.JobID, err)
	}
	return nil
}

// GetJob loads a sync job by its JobID.
func (s *SQLiteStorage) GetJob(ctx context.Context, jobID string) (*domain.SyncJob, error) {
	query := `
	SELECT job_id, status, total_batches, committed_batch_index,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       created_at, updated_at, completed_at
	FROM sync_jobs WHERE job_id = ?;
	`
	row := s.db.QueryRowContext(ctx, query, jobID)

	var job domain.SyncJob
	var statusStr, createdStr, updatedStr string
	var completedStr sql.NullString

	err := row.Scan(
		&job.JobID,
		&statusStr,
		&job.TotalBatches,
		&job.CommittedBatchIndex,
		&job.Summary.Added,
		&job.Summary.Updated,
		&job.Summary.Withdrawn,
		&job.Summary.Skipped,
		&job.Summary.DeadLetter,
		&createdStr,
		&updatedStr,
		&completedStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan job %s: %w", jobID, err)
	}

	job.Status = domain.JobStatus(statusStr)
	job.CreatedAt, _ = ParseTime(createdStr)
	job.UpdatedAt, _ = ParseTime(updatedStr)
	if completedStr.Valid && completedStr.String != "" {
		ct, err := ParseTime(completedStr.String)
		if err == nil {
			job.CompletedAt = &ct
		}
	}

	return &job, nil
}

// GetActiveJob returns the currently RUNNING or PENDING job, or ErrNotFound if idle.
func (s *SQLiteStorage) GetActiveJob(ctx context.Context) (*domain.SyncJob, error) {
	query := `
	SELECT job_id, status, total_batches, committed_batch_index,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       created_at, updated_at, completed_at
	FROM sync_jobs WHERE status IN ('RUNNING', 'PENDING')
	ORDER BY created_at DESC LIMIT 1;
	`
	row := s.db.QueryRowContext(ctx, query)

	var job domain.SyncJob
	var statusStr, createdStr, updatedStr string
	var completedStr sql.NullString

	err := row.Scan(
		&job.JobID,
		&statusStr,
		&job.TotalBatches,
		&job.CommittedBatchIndex,
		&job.Summary.Added,
		&job.Summary.Updated,
		&job.Summary.Withdrawn,
		&job.Summary.Skipped,
		&job.Summary.DeadLetter,
		&createdStr,
		&updatedStr,
		&completedStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to query active job: %w", err)
	}

	job.Status = domain.JobStatus(statusStr)
	job.CreatedAt, _ = ParseTime(createdStr)
	job.UpdatedAt, _ = ParseTime(updatedStr)

	return &job, nil
}

// UpdateJobStatus updates the status and timestamp of a sync job.
func (s *SQLiteStorage) UpdateJobStatus(ctx context.Context, jobID string, status domain.JobStatus) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var query string
	var args []any

	if status == domain.JobStatusCompleted || status == domain.JobStatusFailed {
		query = `UPDATE sync_jobs SET status = ?, updated_at = ?, completed_at = ? WHERE job_id = ?;`
		args = []any{string(status), now, now, jobID}
	} else {
		query = `UPDATE sync_jobs SET status = ?, updated_at = ? WHERE job_id = ?;`
		args = []any{string(status), now, jobID}
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update job status %s: %w", jobID, err)
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RecordBatchCheckpointTx records an immutable checkpoint audit row inside the batch transaction.
func (s *SQLiteStorage) RecordBatchCheckpointTx(ctx context.Context, tx *sql.Tx, cp *domain.BatchCheckpoint) error {
	query := `
	INSERT INTO batch_checkpoints (
		job_id, batch_index, items_count,
		added_count, updated_count, withdrawn_count, skipped_count, dead_letter_count,
		committed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err := tx.ExecContext(ctx, query,
		cp.JobID,
		cp.BatchIndex,
		cp.ItemsCount,
		cp.Summary.Added,
		cp.Summary.Updated,
		cp.Summary.Withdrawn,
		cp.Summary.Skipped,
		cp.Summary.DeadLetter,
		cp.CommittedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to record batch checkpoint %s[%d]: %w", cp.JobID, cp.BatchIndex, err)
	}
	return nil
}

// UpdateJobProgressTx updates cumulative progress counters in sync_jobs atomically inside the batch transaction.
func (s *SQLiteStorage) UpdateJobProgressTx(ctx context.Context, tx *sql.Tx, jobID string, batchIndex int, delta domain.ChangeSummary) error {
	query := `
	UPDATE sync_jobs SET
		committed_batch_index = ?,
		items_added = items_added + ?,
		items_updated = items_updated + ?,
		items_withdrawn = items_withdrawn + ?,
		items_skipped = items_skipped + ?,
		items_dead_letter = items_dead_letter + ?,
		updated_at = ?
	WHERE job_id = ?;
	`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, query,
		batchIndex,
		delta.Added,
		delta.Updated,
		delta.Withdrawn,
		delta.Skipped,
		delta.DeadLetter,
		now,
		jobID,
	)
	if err != nil {
		return fmt.Errorf("failed to update job progress for %s: %w", jobID, err)
	}
	return nil
}

// RecoverInterruptedJobs transitions stale RUNNING or PENDING jobs to INTERRUPTED on application startup.
func (s *SQLiteStorage) RecoverInterruptedJobs(ctx context.Context) ([]string, error) {
	selectQuery := `SELECT job_id FROM sync_jobs WHERE status IN ('RUNNING', 'PENDING');`
	rows, err := s.db.QueryContext(ctx, selectQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale jobs: %w", err)
	}
	defer rows.Close()

	var recoveredIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			recoveredIDs = append(recoveredIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating stale jobs: %w", err)
	}

	if len(recoveredIDs) == 0 {
		return nil, nil
	}

	updateQuery := `
	UPDATE sync_jobs SET status = 'INTERRUPTED', updated_at = ?
	WHERE status IN ('RUNNING', 'PENDING');
	`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, updateQuery, now)
	if err != nil {
		return nil, fmt.Errorf("failed to mark jobs interrupted: %w", err)
	}

	return recoveredIDs, nil
}

// RecordDeadLetterTx writes an isolated corrupt/invalid record to dead_letter_issues inside the batch transaction.
func (s *SQLiteStorage) RecordDeadLetterTx(ctx context.Context, tx *sql.Tx, issue *domain.DeadLetterRecord) error {
	query := `
	INSERT INTO dead_letter_issues (issue_id, job_id, batch_index, sku, raw_payload, reason, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	_, err := tx.ExecContext(ctx, query,
		issue.IssueID,
		issue.JobID,
		issue.BatchIndex,
		issue.SKU,
		issue.RawPayload,
		issue.Reason,
		issue.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to record dead letter issue %s: %w", issue.IssueID, err)
	}
	return nil
}

// ListDeadLetterRecords queries paginated dead-letter records with optional job_id and reason filters.
func (s *SQLiteStorage) ListDeadLetterRecords(ctx context.Context, jobID string, reason string, limit, offset int) ([]domain.DeadLetterRecord, int, error) {
	var conditions []string
	var args []any

	if jobID != "" {
		conditions = append(conditions, "job_id = ?")
		args = append(args, jobID)
	}
	if reason != "" {
		conditions = append(conditions, "reason LIKE '%' || ? || '%'")
		args = append(args, reason)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT count(*) FROM dead_letter_issues %s;", whereClause)
	var totalCount int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("failed to count dead letter issues: %w", err)
	}

	selectQuery := fmt.Sprintf(`
		SELECT issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
		FROM dead_letter_issues %s
		ORDER BY created_at DESC LIMIT ? OFFSET ?;`, whereClause)

	selectArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.QueryContext(ctx, selectQuery, selectArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query dead letter issues: %w", err)
	}
	defer rows.Close()

	records := make([]domain.DeadLetterRecord, 0)
	for rows.Next() {
		var r domain.DeadLetterRecord
		var timeStr string
		var skuStr sql.NullString

		if err := rows.Scan(&r.IssueID, &r.JobID, &r.BatchIndex, &skuStr, &r.RawPayload, &r.Reason, &timeStr); err != nil {
			return nil, 0, fmt.Errorf("failed scanning dead letter row: %w", err)
		}
		if skuStr.Valid {
			r.SKU = skuStr.String
		}
		r.CreatedAt, _ = ParseTime(timeStr)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating dead letter rows: %w", err)
	}

	return records, totalCount, nil
}

// ResolveDeadLetter atomically upserts a corrected product into catalog and deletes the quarantined dead-letter issue.
func (s *SQLiteStorage) ResolveDeadLetter(ctx context.Context, issueID string, p *domain.Product) error {
	if s.db == nil {
		return errors.New("database connection is closed")
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM dead_letter_issues WHERE issue_id = ?;", issueID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("failed to query dead letter issue: %w", err)
	}

	if err := s.UpsertProductTx(ctx, tx, p); err != nil {
		return fmt.Errorf("failed to upsert corrected product: %w", err)
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM dead_letter_issues WHERE issue_id = ?;", issueID)
	if err != nil {
		return fmt.Errorf("failed to remove dead letter issue: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit resolution: %w", err)
	}
	return nil
}

// ListJobs retrieves a paginated list of sync jobs ordered by creation timestamp descending.
func (s *SQLiteStorage) ListJobs(ctx context.Context, limit, offset int) ([]domain.SyncJob, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	countQuery := `SELECT count(*) FROM sync_jobs;`
	if err := s.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count sync jobs: %w", err)
	}

	query := `
	SELECT job_id, status, total_batches, committed_batch_index,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       created_at, updated_at, completed_at
	FROM sync_jobs
	ORDER BY created_at DESC
	LIMIT ? OFFSET ?;
	`
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query sync jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]domain.SyncJob, 0)
	for rows.Next() {
		var j domain.SyncJob
		var statusStr, createdStr, updatedStr string
		var completedStr sql.NullString

		if err := rows.Scan(
			&j.JobID, &statusStr, &j.TotalBatches, &j.CommittedBatchIndex,
			&j.Summary.Added, &j.Summary.Updated, &j.Summary.Withdrawn,
			&j.Summary.Skipped, &j.Summary.DeadLetter,
			&createdStr, &updatedStr, &completedStr,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan sync job row: %w", err)
		}
		j.Status = domain.JobStatus(statusStr)
		j.CreatedAt, _ = ParseTime(createdStr)
		j.UpdatedAt, _ = ParseTime(updatedStr)
		if completedStr.Valid && completedStr.String != "" {
			ct, err := ParseTime(completedStr.String)
			if err == nil {
				j.CompletedAt = &ct
			}
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating sync jobs rows: %w", err)
	}
	return jobs, total, nil
}

// ListProducts retrieves a paginated list of catalog products, optionally filtered by status and search term.
func (s *SQLiteStorage) ListProducts(ctx context.Context, status string, search string, limit, offset int) ([]domain.Product, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var conditions []string
	var args []any

	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if search != "" {
		conditions = append(conditions, "(sku LIKE ? OR name LIKE ?)")
		searchPattern := "%" + search + "%"
		args = append(args, searchPattern, searchPattern)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT count(*) FROM products %s;", whereClause)
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count products: %w", err)
	}

	selectQuery := fmt.Sprintf(`
	SELECT sku, name, price, status, supplier_timestamp, updated_at
	FROM products %s
	ORDER BY sku ASC
	LIMIT ? OFFSET ?;`, whereClause)

	selectArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.QueryContext(ctx, selectQuery, selectArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list products: %w", err)
	}
	defer rows.Close()

	products := make([]domain.Product, 0)
	for rows.Next() {
		var p domain.Product
		var statusStr, supTimeStr, upTimeStr string
		if err := rows.Scan(&p.SKU, &p.Name, &p.Price, &statusStr, &supTimeStr, &upTimeStr); err != nil {
			return nil, 0, fmt.Errorf("failed scanning product row: %w", err)
		}
		p.Status = domain.ProductStatus(statusStr)
		p.SupplierTimestamp, _ = ParseTime(supTimeStr)
		p.UpdatedAt, _ = ParseTime(upTimeStr)
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating product rows: %w", err)
	}
	return products, total, nil
}

// GetBatchCheckpoints returns all committed checkpoints for a given job ID ordered by batch index.
func (s *SQLiteStorage) GetBatchCheckpoints(ctx context.Context, jobID string) ([]domain.BatchCheckpoint, error) {
	query := `
	SELECT job_id, batch_index, items_count, added_count, updated_count, withdrawn_count, skipped_count, dead_letter_count, committed_at
	FROM batch_checkpoints
	WHERE job_id = ?
	ORDER BY batch_index ASC;
	`
	rows, err := s.db.QueryContext(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query batch checkpoints: %w", err)
	}
	defer rows.Close()

	checkpoints := make([]domain.BatchCheckpoint, 0)
	for rows.Next() {
		var cp domain.BatchCheckpoint
		var timeStr string
		if err := rows.Scan(
			&cp.JobID, &cp.BatchIndex, &cp.ItemsCount,
			&cp.Summary.Added, &cp.Summary.Updated, &cp.Summary.Withdrawn,
			&cp.Summary.Skipped, &cp.Summary.DeadLetter,
			&timeStr,
		); err != nil {
			return nil, fmt.Errorf("failed scanning checkpoint: %w", err)
		}
		cp.CommittedAt, _ = ParseTime(timeStr)
		checkpoints = append(checkpoints, cp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating checkpoint rows: %w", err)
	}
	return checkpoints, nil
}

// Reset atomically deletes all records from products, sync_jobs, batch_checkpoints, and dead_letter_issues.
func (s *SQLiteStorage) Reset(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for reset: %w", err)
	}
	defer tx.Rollback()

	queries := []string{
		"DELETE FROM batch_checkpoints;",
		"DELETE FROM dead_letter_issues;",
		"DELETE FROM sync_jobs;",
		"DELETE FROM products;",
	}
	for _, q := range queries {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("failed executing %s: %w", q, err)
		}
	}

	return tx.Commit()
}

// ParseTime parses RFC3339Nano or RFC3339 formatted UTC timestamps.
func ParseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
