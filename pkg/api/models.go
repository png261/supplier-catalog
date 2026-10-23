package api

import (
	"coderpush/pkg/domain"
	"time"
)

// ErrorCode represents standard API error taxonomy.
type ErrorCode string

const (
	ErrCodeInvalidRequest ErrorCode = "INVALID_REQUEST"
	ErrCodeJobNotFound    ErrorCode = "JOB_NOT_FOUND"
	ErrCodeNotFound       ErrorCode = "NOT_FOUND"
	ErrCodeJobConflict    ErrorCode = "JOB_CONFLICT"
	ErrCodeInternalError  ErrorCode = "INTERNAL_ERROR"
	ErrCodeCommitBlocked  ErrorCode = "COMMIT_BLOCKED"
)

// ErrorResponse represents standardized API error payload.
type ErrorResponse struct {
	Error string    `json:"error"`
	Code  ErrorCode `json:"code"`
}

// BatchInputRecord represents raw supplier product update in direct batch payload.
type BatchInputRecord struct {
	SKU       string               `json:"sku"`
	Name      string               `json:"name"`
	Price     float64              `json:"price"`
	Status    domain.ProductStatus `json:"status"`
	Timestamp string               `json:"timestamp"`
}

// BatchInput represents a single batch in direct batch payload.
type BatchInput struct {
	BatchIndex int                `json:"batch_index"`
	Records    []BatchInputRecord `json:"records"`
}

// JobStartRequest specifies parameters for starting a sync job.
type JobStartRequest struct {
	Mode            string       `json:"mode,omitempty"` // "generator" or "batches" (default "generator")
	JobID           string       `json:"job_id,omitempty"`
	Description     string       `json:"description,omitempty"`
	BaseTime        *string      `json:"base_time,omitempty"`
	Seed            *uint64      `json:"seed,omitempty"`
	TotalBatches    *int         `json:"total_batches,omitempty"`
	BatchSize       *int         `json:"batch_size,omitempty"`
	UpdateRatio     *float64     `json:"update_ratio,omitempty"`
	DuplicateRatio  *float64     `json:"duplicate_ratio,omitempty"`
	OutOfOrderRatio *float64     `json:"out_of_order_ratio,omitempty"`
	WithdrawnRatio  *float64     `json:"withdrawn_ratio,omitempty"`
	InvalidRatio    *float64     `json:"invalid_ratio,omitempty"`
	Batches         []BatchInput `json:"batches,omitempty"`
}

// JobResumeRequest specifies optional parameters when resuming a job.
type JobResumeRequest struct {
	Seed         *uint64 `json:"seed,omitempty"`
	TotalBatches *int    `json:"total_batches,omitempty"`
	BatchSize    *int    `json:"batch_size,omitempty"`
}

// SyncJobResponse represents sync job state returned on start/resume.
type SyncJobResponse struct {
	JobID               string               `json:"job_id"`
	Status              domain.JobStatus     `json:"status"`
	TotalBatches        int                  `json:"total_batches"`
	CommittedBatchIndex int                  `json:"committed_batch_index"`
	Summary             domain.ChangeSummary `json:"summary"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	CompletedAt         *time.Time           `json:"completed_at,omitempty"`
}

// JobListResponse represents paginated list of sync jobs.
type JobListResponse struct {
	Jobs   []domain.SyncJob `json:"jobs"`
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

// JobDetailResponse represents job details with checkpoints.
type JobDetailResponse struct {
	Job         *domain.SyncJob          `json:"job"`
	Checkpoints []domain.BatchCheckpoint `json:"checkpoints"`
}

// DeadLetterListResponse represents paginated dead-letter records.
type DeadLetterListResponse struct {
	Issues []domain.DeadLetterRecord `json:"issues"`
	Total  int                       `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
}

// ProductListResponse represents paginated catalog products.
type ProductListResponse struct {
	Products []domain.Product `json:"products"`
	Total    int              `json:"total"`
	Limit    int              `json:"limit"`
	Offset   int              `json:"offset"`
}

// MetricsResponse represents the aggregated analytics and matching sync runs response.
type MetricsResponse struct {
	Filter   domain.MetricsFilter     `json:"filter"`
	Metrics  domain.AggregatedMetrics `json:"metrics"`
	SyncJobs []domain.SyncJob         `json:"sync_jobs"`
}

// ResolveDeadLetterRequest defines the payload for correcting a quarantined dead-letter product.
type ResolveDeadLetterRequest struct {
	SKU       string  `json:"sku" example:"SKU-FIXED-01"`
	Name      string  `json:"name,omitempty" example:"Fixed Ergonomic Chair"`
	Price     float64 `json:"price" example:"149.99"`
	Status    string  `json:"status" example:"ACTIVE"`
	Timestamp string  `json:"timestamp,omitempty" example:"2026-10-22T10:00:00Z"`
}

// JobCancelResponse represents the response payload when an active sync job is cancelled.
type JobCancelResponse struct {
	Status  string `json:"status" example:"cancelling"`
	JobID   string `json:"job_id" example:"job-demo-01"`
	Message string `json:"message" example:"cancellation signal sent successfully"`
}

// DatabaseResetResponse represents the response payload when the tenant database is reset.
type DatabaseResetResponse struct {
	Status  string `json:"status" example:"success"`
	Message string `json:"message" example:"database reset to clean state"`
}

// ResolveDeadLetterResponse represents the response payload when a quarantined record is remediated.
type ResolveDeadLetterResponse struct {
	Status  string         `json:"status" example:"success"`
	Message string         `json:"message" example:"product resolved and imported to catalog"`
	Product domain.Product `json:"product"`
}

// PatchPublishResponse represents the response when a patch is committed and published.
type PatchPublishResponse struct {
	Version domain.CatalogVersion `json:"version"`
	Deltas  []domain.ItemDelta    `json:"deltas"`
}

// VersionListResponse represents paginated catalog versions.
type VersionListResponse struct {
	Versions []domain.CatalogVersion `json:"versions"`
	Total    int                     `json:"total"`
	Limit    int                     `json:"limit"`
	Offset   int                     `json:"offset"`
}

// RollbackRequest defines the payload for reverting the catalog to a previous baseline version.
type RollbackRequest struct {
	Author      string `json:"author,omitempty" example:"ops-team"`
	Operator    string `json:"operator,omitempty" example:"ops-team"`
	Description string `json:"description,omitempty" example:"Rollback due to supplier pricing error"`
}

// RollbackResponse represents the response when a catalog rollback is executed.
type RollbackResponse struct {
	Version domain.CatalogVersion `json:"version"`
	Deltas  []domain.ItemDelta    `json:"deltas"`
}
