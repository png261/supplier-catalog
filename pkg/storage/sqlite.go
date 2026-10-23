package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"sort"

	"coderpush/pkg/domain"

	"github.com/google/uuid"
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
	GetMetrics(ctx context.Context, filter domain.MetricsFilter) (*domain.AggregatedMetrics, []domain.SyncJob, error)

	// Catalog Versioning & Rollback
	CreateVersionTx(ctx context.Context, tx *sql.Tx, ver *domain.CatalogVersion, deltas []domain.ItemDelta) error
	GetVersion(ctx context.Context, versionID string) (*domain.CatalogVersion, error)
	GetLatestVersion(ctx context.Context) (*domain.CatalogVersion, error)
	ListVersions(ctx context.Context, limit, offset int) ([]domain.CatalogVersion, int, error)
	GetVersionDeltas(ctx context.Context, versionID string) ([]domain.ItemDelta, error)
	GetBaselineStateAtVersion(ctx context.Context, versionID string) (map[string]domain.Product, error)
	RollbackVersion(ctx context.Context, targetVersionID string, author string, description string) (*domain.CatalogVersion, []domain.ItemDelta, error)

	// Migrations
	MigrateUp(ctx context.Context) error
	MigrateDown(ctx context.Context) error
	MigrateToVersion(ctx context.Context, version uint) error
	MigrationVersion(ctx context.Context) (version uint, dirty bool, err error)

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

// initSchema configures SQLite pragmas and applies all pending migrations using golang-migrate.
func (s *SQLiteStorage) initSchema(ctx context.Context) error {
	pragmas := `
	PRAGMA busy_timeout = 5000;
	PRAGMA foreign_keys = ON;
	`
	if _, err := s.db.ExecContext(ctx, pragmas); err != nil {
		return fmt.Errorf("failed configuring sqlite pragmas: %w", err)
	}

	if err := s.MigrateUp(ctx); err != nil {
		return fmt.Errorf("failed executing migrations: %w", err)
	}
	return nil
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
	var completedAt any
	if job.CompletedAt != nil {
		completedAt = job.CompletedAt.UTC().Format(time.RFC3339Nano)
	}

	query := `
	INSERT INTO sync_jobs (
		job_id, status, total_batches, committed_batch_index,
		items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
		created_at, updated_at, completed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
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
		completedAt,
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

// GetMetrics returns aggregated metrics and associated sync jobs according to filter criteria.
func (s *SQLiteStorage) GetMetrics(ctx context.Context, filter domain.MetricsFilter) (*domain.AggregatedMetrics, []domain.SyncJob, error) {
	var conditions []string
	var args []any

	if filter.JobID != "" {
		conditions = append(conditions, "job_id = ?")
		args = append(args, filter.JobID)
	} else {
		if filter.From != nil {
			conditions = append(conditions, "created_at >= ?")
			args = append(args, filter.From.UTC().Format(time.RFC3339Nano))
		}
		if filter.To != nil {
			conditions = append(conditions, "created_at <= ?")
			args = append(args, filter.To.UTC().Format(time.RFC3339Nano))
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	aggQuery := fmt.Sprintf(`
	SELECT 
		COALESCE(SUM(items_added), 0),
		COALESCE(SUM(items_updated), 0),
		COALESCE(SUM(items_withdrawn), 0),
		COALESCE(SUM(items_skipped), 0),
		COALESCE(SUM(items_dead_letter), 0),
		COUNT(*),
		COALESCE(SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END), 0)
	FROM sync_jobs %s;
	`, whereClause)

	var agg domain.AggregatedMetrics
	if err := s.db.QueryRowContext(ctx, aggQuery, args...).Scan(
		&agg.Summary.Added,
		&agg.Summary.Updated,
		&agg.Summary.Withdrawn,
		&agg.Summary.Skipped,
		&agg.Summary.DeadLetter,
		&agg.TotalSyncs,
		&agg.CompletedSyncs,
	); err != nil {
		return nil, nil, fmt.Errorf("failed to aggregate metrics: %w", err)
	}

	agg.TotalProcessed = agg.Summary.TotalProcessed()
	if agg.TotalProcessed > 0 {
		validItems := agg.TotalProcessed - agg.Summary.DeadLetter
		if validItems < 0 {
			validItems = 0
		}
		agg.QualityScore = (float64(validItems) / float64(agg.TotalProcessed)) * 100.0
	} else {
		agg.QualityScore = 100.0
	}

	// Fetch matching sync jobs (up to 100, most recent first)
	jobsQuery := fmt.Sprintf(`
	SELECT job_id, status, total_batches, committed_batch_index,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       created_at, updated_at, completed_at
	FROM sync_jobs %s
	ORDER BY created_at DESC
	LIMIT 100;
	`, whereClause)

	rows, err := s.db.QueryContext(ctx, jobsQuery, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query sync jobs for metrics: %w", err)
	}
	defer rows.Close()

	jobs := make([]domain.SyncJob, 0)
	var totalElapsed float64
	for rows.Next() {
		var j domain.SyncJob
		var statusStr, createdStr, updatedStr string
		var completedStr sql.NullString

		if err := rows.Scan(
			&j.JobID,
			&statusStr,
			&j.TotalBatches,
			&j.CommittedBatchIndex,
			&j.Summary.Added,
			&j.Summary.Updated,
			&j.Summary.Withdrawn,
			&j.Summary.Skipped,
			&j.Summary.DeadLetter,
			&createdStr,
			&updatedStr,
			&completedStr,
		); err != nil {
			return nil, nil, fmt.Errorf("failed to scan sync job: %w", err)
		}

		j.Status = domain.JobStatus(statusStr)
		j.CreatedAt, _ = ParseTime(createdStr)
		j.UpdatedAt, _ = ParseTime(updatedStr)
		if completedStr.Valid && completedStr.String != "" {
			if ct, err := ParseTime(completedStr.String); err == nil {
				j.CompletedAt = &ct
				dur := ct.Sub(j.CreatedAt).Seconds()
				if dur > 0 {
					totalElapsed += dur
				}
			}
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating sync jobs rows: %w", err)
	}

	agg.ElapsedSeconds = totalElapsed
	if totalElapsed > 0 && agg.TotalProcessed > 0 {
		agg.AvgThroughput = float64(agg.TotalProcessed) / totalElapsed
	}

	return &agg, jobs, nil
}

// Reset atomically deletes all records from products, sync_jobs, batch_checkpoints,
// dead_letter_issues, catalog_version_deltas, and catalog_versions.
func (s *SQLiteStorage) Reset(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for reset: %w", err)
	}
	defer tx.Rollback()

	queries := []string{
		"DELETE FROM catalog_version_deltas;",
		"DELETE FROM catalog_versions;",
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

// CreateVersionTx persists a catalog version milestone and its pre-image deltas inside a transaction.
func (s *SQLiteStorage) CreateVersionTx(ctx context.Context, tx *sql.Tx, ver *domain.CatalogVersion, deltas []domain.ItemDelta) error {
	if tx == nil {
		return errors.New("transaction cannot be nil")
	}
	if ver == nil {
		return errors.New("catalog version cannot be nil")
	}
	if strings.TrimSpace(ver.VersionID) == "" {
		return errors.New("version_id cannot be empty")
	}

	// Auto-compute sequential version number if not provided
	if ver.VersionNumber <= 0 {
		var maxNum sql.NullInt64
		err := tx.QueryRowContext(ctx, "SELECT MAX(version_number) FROM catalog_versions;").Scan(&maxNum)
		if err != nil {
			return fmt.Errorf("failed to determine next version number: %w", err)
		}
		if maxNum.Valid {
			ver.VersionNumber = int(maxNum.Int64) + 1
		} else {
			ver.VersionNumber = 1
		}
	}

	if ver.CreatedAt.IsZero() {
		ver.CreatedAt = time.Now().UTC()
	}

	if ver.TotalItemsAffected == 0 && len(deltas) > 0 {
		ver.TotalItemsAffected = len(deltas)
	}

	versionQuery := `
	INSERT INTO catalog_versions (
		version_id, version_number, description, author, type,
		revert_of_version_id,
		items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
		total_items_affected, metadata, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`

	var revertOf any
	if ver.RevertOfVersionID != nil && strings.TrimSpace(*ver.RevertOfVersionID) != "" {
		revertOf = strings.TrimSpace(*ver.RevertOfVersionID)
	}

	_, err := tx.ExecContext(ctx, versionQuery,
		ver.VersionID,
		ver.VersionNumber,
		ver.Description,
		ver.Author,
		string(ver.Type),
		revertOf,
		ver.Summary.Added,
		ver.Summary.Updated,
		ver.Summary.Withdrawn,
		ver.Summary.Skipped,
		ver.Summary.DeadLetter,
		ver.TotalItemsAffected,
		ver.Metadata,
		ver.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to insert catalog version %s: %w", ver.VersionID, err)
	}

	if len(deltas) == 0 {
		return nil
	}

	deltaQuery := `
	INSERT INTO catalog_version_deltas (
		version_id, sku, action,
		old_name, new_name,
		old_price, new_price,
		old_status, new_status,
		old_timestamp, new_timestamp
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	stmt, err := tx.PrepareContext(ctx, deltaQuery)
	if err != nil {
		return fmt.Errorf("failed preparing delta insert statement: %w", err)
	}
	defer stmt.Close()

	for i := range deltas {
		d := &deltas[i]
		d.VersionID = ver.VersionID

		var oldName any
		if d.OldName != nil {
			oldName = *d.OldName
		}
		var oldPrice any
		if d.OldPrice != nil {
			oldPrice = *d.OldPrice
		}
		var oldStatus any
		if d.OldStatus != nil {
			oldStatus = string(*d.OldStatus)
		}
		var oldTimestamp any
		if d.OldTimestamp != nil && !d.OldTimestamp.IsZero() {
			oldTimestamp = d.OldTimestamp.UTC().Format(time.RFC3339Nano)
		}

		res, err := stmt.ExecContext(ctx,
			ver.VersionID,
			d.SKU,
			string(d.Action),
			oldName,
			d.NewName,
			oldPrice,
			d.NewPrice,
			oldStatus,
			string(d.NewStatus),
			oldTimestamp,
			d.NewTimestamp.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("failed inserting delta for sku %s: %w", d.SKU, err)
		}

		if id, err := res.LastInsertId(); err == nil {
			d.DeltaID = id
		}
	}

	return nil
}

func scanVersionRow(scanner interface{ Scan(dest ...any) error }) (*domain.CatalogVersion, error) {
	var ver domain.CatalogVersion
	var typeStr, createdStr string
	var revertOfStr sql.NullString

	err := scanner.Scan(
		&ver.VersionID,
		&ver.VersionNumber,
		&ver.Description,
		&ver.Author,
		&typeStr,
		&revertOfStr,
		&ver.Summary.Added,
		&ver.Summary.Updated,
		&ver.Summary.Withdrawn,
		&ver.Summary.Skipped,
		&ver.Summary.DeadLetter,
		&ver.TotalItemsAffected,
		&ver.Metadata,
		&createdStr,
	)
	if err != nil {
		return nil, err
	}

	ver.Type = domain.VersionType(typeStr)
	if revertOfStr.Valid && revertOfStr.String != "" {
		s := revertOfStr.String
		ver.RevertOfVersionID = &s
	}
	ver.CreatedAt, _ = ParseTime(createdStr)

	return &ver, nil
}

// GetVersion retrieves a single catalog version by its ID.
func (s *SQLiteStorage) GetVersion(ctx context.Context, versionID string) (*domain.CatalogVersion, error) {
	query := `
	SELECT version_id, version_number, description, author, type,
	       revert_of_version_id,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       total_items_affected, metadata, created_at
	FROM catalog_versions
	WHERE version_id = ?;
	`
	row := s.db.QueryRowContext(ctx, query, versionID)
	ver, err := scanVersionRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get version %s: %w", versionID, err)
	}
	return ver, nil
}

// GetLatestVersion retrieves the most recent catalog version milestone, ordered by version_number DESC.
func (s *SQLiteStorage) GetLatestVersion(ctx context.Context) (*domain.CatalogVersion, error) {
	query := `
	SELECT version_id, version_number, description, author, type,
	       revert_of_version_id,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       total_items_affected, metadata, created_at
	FROM catalog_versions
	ORDER BY version_number DESC
	LIMIT 1;
	`
	row := s.db.QueryRowContext(ctx, query)
	ver, err := scanVersionRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get latest version: %w", err)
	}
	return ver, nil
}

// ListVersions retrieves a paginated list of catalog versions, ordered newest first (version_number DESC).
func (s *SQLiteStorage) ListVersions(ctx context.Context, limit, offset int) ([]domain.CatalogVersion, int, error) {
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
	countQuery := `SELECT count(*) FROM catalog_versions;`
	if err := s.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count catalog versions: %w", err)
	}

	query := `
	SELECT version_id, version_number, description, author, type,
	       revert_of_version_id,
	       items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
	       total_items_affected, metadata, created_at
	FROM catalog_versions
	ORDER BY version_number DESC
	LIMIT ? OFFSET ?;
	`
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query catalog versions: %w", err)
	}
	defer rows.Close()

	versions := make([]domain.CatalogVersion, 0)
	for rows.Next() {
		ver, err := scanVersionRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed scanning version row: %w", err)
		}
		versions = append(versions, *ver)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating version rows: %w", err)
	}

	return versions, total, nil
}

// GetVersionDeltas retrieves all pre-image deltas for a specific catalog version ordered by delta_id ASC.
func (s *SQLiteStorage) GetVersionDeltas(ctx context.Context, versionID string) ([]domain.ItemDelta, error) {
	query := `
	SELECT delta_id, version_id, sku, action,
	       old_name, new_name,
	       old_price, new_price,
	       old_status, new_status,
	       old_timestamp, new_timestamp
	FROM catalog_version_deltas
	WHERE version_id = ?
	ORDER BY delta_id ASC;
	`
	rows, err := s.db.QueryContext(ctx, query, versionID)
	if err != nil {
		return nil, fmt.Errorf("failed querying deltas for version %s: %w", versionID, err)
	}
	defer rows.Close()

	deltas := make([]domain.ItemDelta, 0)
	for rows.Next() {
		var d domain.ItemDelta
		var actionStr, newStatusStr, newTimeStr string
		var oldNameStr sql.NullString
		var oldPriceNum sql.NullFloat64
		var oldStatusStr sql.NullString
		var oldTimeStr sql.NullString

		if err := rows.Scan(
			&d.DeltaID,
			&d.VersionID,
			&d.SKU,
			&actionStr,
			&oldNameStr,
			&d.NewName,
			&oldPriceNum,
			&d.NewPrice,
			&oldStatusStr,
			&newStatusStr,
			&oldTimeStr,
			&newTimeStr,
		); err != nil {
			return nil, fmt.Errorf("failed scanning delta row: %w", err)
		}

		d.Action = domain.DeltaAction(actionStr)
		d.NewStatus = domain.ProductStatus(newStatusStr)
		d.NewTimestamp, _ = ParseTime(newTimeStr)

		if oldNameStr.Valid {
			s := oldNameStr.String
			d.OldName = &s
		}
		if oldPriceNum.Valid {
			p := oldPriceNum.Float64
			d.OldPrice = &p
		}
		if oldStatusStr.Valid {
			st := domain.ProductStatus(oldStatusStr.String)
			d.OldStatus = &st
		}
		if oldTimeStr.Valid && oldTimeStr.String != "" {
			t, err := ParseTime(oldTimeStr.String)
			if err == nil {
				d.OldTimestamp = &t
			}
		}

		deltas = append(deltas, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating delta rows: %w", err)
	}

	return deltas, nil
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

// querier abstracts *sql.DB and *sql.Tx for executing both single-row and multi-row queries.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// GetBaselineStateAtVersion reconstructs the complete catalog state at the specified target version
// by replaying the pre-image deltas up to that version using a SQL window function.
func (s *SQLiteStorage) GetBaselineStateAtVersion(ctx context.Context, versionID string) (map[string]domain.Product, error) {
	if strings.TrimSpace(versionID) == "" {
		return nil, domain.ErrNotFound
	}
	return s.getBaselineStateAtVersion(ctx, s.db, versionID)
}

// GetBaselineStateAtVersionTx reconstructs the baseline catalog state within an existing transaction boundary.
func (s *SQLiteStorage) GetBaselineStateAtVersionTx(ctx context.Context, tx *sql.Tx, versionID string) (map[string]domain.Product, error) {
	if strings.TrimSpace(versionID) == "" {
		return nil, domain.ErrNotFound
	}
	return s.getBaselineStateAtVersion(ctx, tx, versionID)
}

func (s *SQLiteStorage) getBaselineStateAtVersion(ctx context.Context, q querier, versionID string) (map[string]domain.Product, error) {
	var targetVersionNumber int
	err := q.QueryRowContext(ctx, "SELECT version_number FROM catalog_versions WHERE version_id = ?;", versionID).Scan(&targetVersionNumber)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed querying version %s: %w", versionID, err)
	}

	query := `
	WITH ranked_deltas AS (
		SELECT 
			d.sku,
			d.new_name,
			d.new_price,
			d.new_status,
			d.new_timestamp,
			ROW_NUMBER() OVER (
				PARTITION BY d.sku 
				ORDER BY v.version_number DESC, d.delta_id DESC
			) AS rn
		FROM catalog_version_deltas d
		JOIN catalog_versions v ON d.version_id = v.version_id
		WHERE v.version_number <= ?
	)
	SELECT sku, new_name, new_price, new_status, new_timestamp
	FROM ranked_deltas
	WHERE rn = 1;
	`
	rows, err := q.QueryContext(ctx, query, targetVersionNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct baseline catalog at version %s: %w", versionID, err)
	}
	defer rows.Close()

	baseline := make(map[string]domain.Product)
	for rows.Next() {
		var p domain.Product
		var statusStr, timeStr string
		if err := rows.Scan(&p.SKU, &p.Name, &p.Price, &statusStr, &timeStr); err != nil {
			return nil, fmt.Errorf("failed scanning baseline product row: %w", err)
		}
		p.Status = domain.ProductStatus(statusStr)
		ts, err := ParseTime(timeStr)
		if err != nil {
			return nil, fmt.Errorf("failed parsing baseline timestamp for sku %s: %w", p.SKU, err)
		}
		p.SupplierTimestamp = ts
		p.UpdatedAt = ts
		baseline[p.SKU] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating baseline product rows: %w", err)
	}

	return baseline, nil
}

// RollbackVersion performs a non-destructive Git-revert rollback to targetVersionID in a single atomic transaction.
// It reconstructs the baseline catalog state, computes inverse deltas, strictly preserves tombstones (Invariant A),
// updates the live products table for sub-millisecond O(1) lookups, and creates a new forward-moving ROLLBACK version.
func (s *SQLiteStorage) RollbackVersion(ctx context.Context, targetVersionID string, author string, description string) (*domain.CatalogVersion, []domain.ItemDelta, error) {
	if strings.TrimSpace(targetVersionID) == "" {
		return nil, nil, domain.ErrNotFound
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin rollback transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Verify target version exists in catalog_versions
	var targetVersionNumber int
	err = tx.QueryRowContext(ctx, "SELECT version_number FROM catalog_versions WHERE version_id = ?;", targetVersionID).Scan(&targetVersionNumber)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, domain.ErrNotFound
		}
		return nil, nil, fmt.Errorf("failed querying target version %s: %w", targetVersionID, err)
	}

	// 2. Retrieve baseline products at targetVersionID
	baselineProducts, err := s.getBaselineStateAtVersion(ctx, tx, targetVersionID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed reconstructing baseline at version %s: %w", targetVersionID, err)
	}

	// 3. Load all live products from products table
	liveRows, err := tx.QueryContext(ctx, "SELECT sku, name, price, status, supplier_timestamp, updated_at FROM products;")
	if err != nil {
		return nil, nil, fmt.Errorf("failed reading live products: %w", err)
	}
	defer liveRows.Close()

	liveProducts := make(map[string]domain.Product)
	for liveRows.Next() {
		var p domain.Product
		var statusStr, supTimeStr, upTimeStr string
		if err := liveRows.Scan(&p.SKU, &p.Name, &p.Price, &statusStr, &supTimeStr, &upTimeStr); err != nil {
			return nil, nil, fmt.Errorf("failed scanning live product: %w", err)
		}
		p.Status = domain.ProductStatus(statusStr)
		p.SupplierTimestamp, _ = ParseTime(supTimeStr)
		p.UpdatedAt, _ = ParseTime(upTimeStr)
		liveProducts[p.SKU] = p
	}
	if err := liveRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating live products: %w", err)
	}

	now := time.Now().UTC()
	var deltas []domain.ItemDelta
	var summary domain.ChangeSummary

	// Sort baseline SKUs for deterministic execution order
	baselineSKUs := make([]string, 0, len(baselineProducts))
	for sku := range baselineProducts {
		baselineSKUs = append(baselineSKUs, sku)
	}
	sort.Strings(baselineSKUs)

	// A. For each product in baseline:
	for _, sku := range baselineSKUs {
		baseline := baselineProducts[sku]
		live, exists := liveProducts[sku]

		if exists {
			targetStatus := baseline.Status
			// CRITICAL INVARIANT A ENFORCEMENT:
			// If product in live catalog currently has status == domain.StatusWithdrawn,
			// and baseline has status == domain.StatusActive:
			// The item MUST NOT be resurrected! Status remains domain.StatusWithdrawn,
			// but price and name revert to baseline values.
			if live.Status == domain.StatusWithdrawn && baseline.Status == domain.StatusActive {
				targetStatus = domain.StatusWithdrawn
			}

			// If price, name, or valid status differs: generate inverse delta (action = UPDATED), update live product
			if live.Price != baseline.Price || live.Name != baseline.Name || live.Status != targetStatus {
				oldName := live.Name
				oldPrice := live.Price
				oldStatus := live.Status
				oldTime := live.SupplierTimestamp

				delta := domain.ItemDelta{
					SKU:          sku,
					Action:       domain.DeltaActionUpdated,
					OldName:      &oldName,
					NewName:      baseline.Name,
					OldPrice:     &oldPrice,
					NewPrice:     baseline.Price,
					OldStatus:    &oldStatus,
					NewStatus:    targetStatus,
					OldTimestamp: &oldTime,
					NewTimestamp: now,
				}
				deltas = append(deltas, delta)
				summary.Updated++

				updatedProd := delta.PostImageProduct()
				if err := s.UpsertProductTx(ctx, tx, &updatedProd); err != nil {
					return nil, nil, fmt.Errorf("failed updating product %s during rollback: %w", sku, err)
				}
			}
		} else {
			// Product was in baseline but missing in products table: insert into products
			delta := domain.ItemDelta{
				SKU:          sku,
				Action:       domain.DeltaActionAdded,
				NewName:      baseline.Name,
				NewPrice:     baseline.Price,
				NewStatus:    baseline.Status,
				NewTimestamp: now,
			}
			deltas = append(deltas, delta)
			summary.Added++

			newProd := delta.PostImageProduct()
			if err := s.UpsertProductTx(ctx, tx, &newProd); err != nil {
				return nil, nil, fmt.Errorf("failed inserting missing baseline product %s during rollback: %w", sku, err)
			}
		}
	}

	// B. For items in live catalog that were ADDED AFTER baseline (do not exist in baseline):
	// Sort live SKUs for deterministic execution order
	liveSKUs := make([]string, 0, len(liveProducts))
	for sku := range liveProducts {
		liveSKUs = append(liveSKUs, sku)
	}
	sort.Strings(liveSKUs)

	for _, sku := range liveSKUs {
		live := liveProducts[sku]
		if _, inBaseline := baselineProducts[sku]; !inBaseline {
			// Item added after baseline: transition status to WITHDRAWN so they no longer appear active
			if live.Status == domain.StatusActive {
				oldName := live.Name
				oldPrice := live.Price
				oldStatus := live.Status
				oldTime := live.SupplierTimestamp

				delta := domain.ItemDelta{
					SKU:          sku,
					Action:       domain.DeltaActionWithdrawn,
					OldName:      &oldName,
					NewName:      live.Name,
					OldPrice:     &oldPrice,
					NewPrice:     live.Price,
					OldStatus:    &oldStatus,
					NewStatus:    domain.StatusWithdrawn,
					OldTimestamp: &oldTime,
					NewTimestamp: now,
				}
				deltas = append(deltas, delta)
				summary.Withdrawn++

				withdrawnProd := delta.PostImageProduct()
				if err := s.UpsertProductTx(ctx, tx, &withdrawnProd); err != nil {
					return nil, nil, fmt.Errorf("failed withdrawing product %s during rollback: %w", sku, err)
				}
			}
		}
	}

	// 4. Create new forward version in catalog_versions
	var maxNum sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MAX(version_number) FROM catalog_versions;").Scan(&maxNum); err != nil {
		return nil, nil, fmt.Errorf("failed to query latest version number: %w", err)
	}
	nextNum := 1
	if maxNum.Valid {
		nextNum = int(maxNum.Int64) + 1
	}

	if strings.TrimSpace(author) == "" {
		author = "system"
	}
	if strings.TrimSpace(description) == "" {
		description = fmt.Sprintf("Rollback to version %s", targetVersionID)
	}

	rollbackVersionID := fmt.Sprintf("ver-rollback-%d-%s", now.UnixNano(), uuid.New().String()[:8])
	targetIDRef := targetVersionID

	rollbackVer := &domain.CatalogVersion{
		VersionID:          rollbackVersionID,
		VersionNumber:      nextNum,
		Description:        description,
		Author:             author,
		Type:               domain.VersionTypeRollback,
		RevertOfVersionID:  &targetIDRef,
		Summary:            summary,
		TotalItemsAffected: len(deltas),
		CreatedAt:          now,
	}

	// 5. Record all inverse deltas and new version via CreateVersionTx
	if err := s.CreateVersionTx(ctx, tx, rollbackVer, deltas); err != nil {
		return nil, nil, fmt.Errorf("failed to persist rollback version: %w", err)
	}

	// 6. Commit atomic transaction
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed committing rollback transaction: %w", err)
	}

	return rollbackVer, deltas, nil
}
