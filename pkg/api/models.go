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
