package domain

import (
	"errors"
	"strings"
	"time"
)

// Common domain sentinel errors.
var (
	ErrNotFound               = errors.New("record not found")
	ErrEmptySKU               = errors.New("sku cannot be empty")
	ErrNegativePrice          = errors.New("price cannot be negative")
	ErrInvalidStatus          = errors.New("status must be ACTIVE or WITHDRAWN")
	ErrZeroTimestamp          = errors.New("timestamp must not be zero")
	ErrResurrectionRestricted = errors.New("tombstone invariant violation: cannot reactivate WITHDRAWN product")
	ErrOutdatedTimestamp      = errors.New("timestamp invariant violation: incoming record older than local state")
	ErrIdenticalUpdate        = errors.New("idempotency invariant: incoming record is identical to local state")
)

// ProductStatus defines the lifecycle status of a catalog product.
type ProductStatus string

const (
	StatusActive    ProductStatus = "ACTIVE"
	StatusWithdrawn ProductStatus = "WITHDRAWN"
)

// IsValid checks if the product status is one of the recognized constants.
func (s ProductStatus) IsValid() bool {
	return s == StatusActive || s == StatusWithdrawn
}

// Product represents a canonical item in the store's local catalog.
type Product struct {
	SKU               string        `json:"sku"`
	Name              string        `json:"name"`
	Price             float64       `json:"price"`
	Status            ProductStatus `json:"status"`
	SupplierTimestamp time.Time     `json:"supplier_timestamp"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

// SupplierUpdate represents an incoming product record from a supplier batch.
type SupplierUpdate struct {
	SKU       string        `json:"sku"`
	Name      string        `json:"name"`
	Price     float64       `json:"price"`
	Status    ProductStatus `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
}

// Validate enforces Invariant D: verifies SKU non-empty, price non-negative, valid status, non-zero timestamp.
func (u *SupplierUpdate) Validate() error {
	if strings.TrimSpace(u.SKU) == "" {
		return ErrEmptySKU
	}
	if u.Price < 0 {
		return ErrNegativePrice
	}
	if !u.Status.IsValid() {
		return ErrInvalidStatus
	}
	if u.Timestamp.IsZero() {
		return ErrZeroTimestamp
	}
	return nil
}

// Batch represents a chunk of supplier updates processed inside an atomic transaction.
type Batch struct {
	JobID      string           `json:"job_id"`
	BatchIndex int              `json:"batch_index"`
	Records    []SupplierUpdate `json:"records"`
}

// ChangeSummary captures metric deltas for a batch or cumulative job run.
type ChangeSummary struct {
	Added      int `json:"added"`
	Updated    int `json:"updated"`
	Withdrawn  int `json:"withdrawn"`
	Skipped    int `json:"skipped"`
	DeadLetter int `json:"dead_letter"`
}

// Add combines another ChangeSummary into this summary.
func (c *ChangeSummary) Add(other ChangeSummary) {
	c.Added += other.Added
	c.Updated += other.Updated
	c.Withdrawn += other.Withdrawn
	c.Skipped += other.Skipped
	c.DeadLetter += other.DeadLetter
}

// TotalProcessed returns the total number of records accounted for.
func (c ChangeSummary) TotalProcessed() int {
	return c.Added + c.Updated + c.Withdrawn + c.Skipped + c.DeadLetter
}

// JobStatus represents the execution state of a catalog synchronization job.
type JobStatus string

const (
	JobStatusPending     JobStatus = "PENDING"
	JobStatusRunning     JobStatus = "RUNNING"
	JobStatusInterrupted JobStatus = "INTERRUPTED"
	JobStatusCompleted   JobStatus = "COMPLETED"
	JobStatusFailed      JobStatus = "FAILED"
)

// SyncJob tracks an end-to-end synchronization run.
type SyncJob struct {
	JobID               string        `json:"job_id"`
	Status              JobStatus     `json:"status"`
	TotalBatches        int           `json:"total_batches"`
	CommittedBatchIndex int           `json:"committed_batch_index"` // -1 before first batch
	Summary             ChangeSummary `json:"summary"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	CompletedAt         *time.Time    `json:"completed_at,omitempty"`
}

// BatchCheckpoint audits an individual committed batch.
type BatchCheckpoint struct {
	JobID       string        `json:"job_id"`
	BatchIndex  int           `json:"batch_index"`
	ItemsCount  int           `json:"items_count"`
	Summary     ChangeSummary `json:"summary"`
	CommittedAt time.Time     `json:"committed_at"`
}

// DeadLetterRecord stores an isolated corrupt or invalid record for staff inspection.
type DeadLetterRecord struct {
	IssueID    string    `json:"issue_id"`
	JobID      string    `json:"job_id"`
	BatchIndex int       `json:"batch_index"`
	SKU        string    `json:"sku"`
	RawPayload string    `json:"raw_payload"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

// MetricsFilter configures query parameters for filtering aggregated performance and catalog metrics.
type MetricsFilter struct {
	TimeRange string     `json:"time_range"` // "today", "yesterday", "7d", "all", "custom"
	From      *time.Time `json:"from,omitempty"`
	To        *time.Time `json:"to,omitempty"`
	JobID     string     `json:"job_id,omitempty"`
}

// AggregatedMetrics holds aggregated catalog changes and performance stats for a filtered window or sync run.
type AggregatedMetrics struct {
	Summary        ChangeSummary `json:"summary"`
	TotalProcessed int           `json:"total_processed"`
	QualityScore   float64       `json:"quality_score"`
	TotalSyncs     int           `json:"total_syncs"`
	CompletedSyncs int           `json:"completed_syncs"`
	ElapsedSeconds float64       `json:"elapsed_seconds"`
	AvgThroughput  float64       `json:"avg_throughput"`
}
