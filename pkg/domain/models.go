package domain

import (
	"errors"
	"math"
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
	JobConfig           string        `json:"job_config,omitempty"`
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

// Additional sentinel errors for catalog versioning and patches.
var (
	ErrInvalidVersionType          = errors.New("invalid version type: must be INITIAL, PATCH, ROLLBACK, or SYNC")
	ErrInvalidDeltaAction          = errors.New("invalid delta action: must be ADDED, UPDATED, or WITHDRAWN")
	ErrEmptyPatchDescription       = errors.New("patch description cannot be empty")
	ErrEmptyPatchAuthor            = errors.New("patch author cannot be empty")
	ErrEmptyPatchItems             = errors.New("patch items cannot be empty")
	ErrVersionNotFound             = errors.New("catalog version not found")
	ErrPatchContainsInvalidRecords = errors.New("cannot publish patch: invalid records or invariant violations detected")
)

// VersionType defines the category of a catalog version milestone.
type VersionType string

const (
	VersionTypeInitial  VersionType = "INITIAL"
	VersionTypePatch    VersionType = "PATCH"
	VersionTypeRollback VersionType = "ROLLBACK"
	VersionTypeSync     VersionType = "SYNC"
)

// IsValid checks if the version type is one of the recognized constants.
func (v VersionType) IsValid() bool {
	switch v {
	case VersionTypeInitial, VersionTypePatch, VersionTypeRollback, VersionTypeSync:
		return true
	default:
		return false
	}
}

// DeltaAction represents the item-level modification category in a catalog delta.
type DeltaAction string

const (
	DeltaActionAdded     DeltaAction = "ADDED"
	DeltaActionUpdated   DeltaAction = "UPDATED"
	DeltaActionWithdrawn DeltaAction = "WITHDRAWN"
)

// IsValid checks if the delta action is one of the recognized constants.
func (a DeltaAction) IsValid() bool {
	switch a {
	case DeltaActionAdded, DeltaActionUpdated, DeltaActionWithdrawn:
		return true
	default:
		return false
	}
}

// CatalogVersion records a catalog snapshot milestone and aggregate delta metadata.
type CatalogVersion struct {
	VersionID          string        `json:"version_id"`
	VersionNumber      int           `json:"version_number"`
	Description        string        `json:"description"`
	Author             string        `json:"author"`
	Type               VersionType   `json:"type"`
	RevertOfVersionID  *string       `json:"revert_of_version_id,omitempty"`
	Summary            ChangeSummary `json:"summary"`
	TotalItemsAffected int           `json:"total_items_affected"`
	Metadata           string        `json:"metadata,omitempty"`
	CreatedAt          time.Time     `json:"created_at"`
}

// ItemDelta records the pre-image and post-image state transition for an individual product.
type ItemDelta struct {
	DeltaID      int64          `json:"delta_id,omitempty"`
	VersionID    string         `json:"version_id"`
	SKU          string         `json:"sku"`
	Action       DeltaAction    `json:"action"`
	OldName      *string        `json:"old_name,omitempty"`
	NewName      string         `json:"new_name"`
	OldPrice     *float64       `json:"old_price,omitempty"`
	NewPrice     float64        `json:"new_price"`
	OldStatus    *ProductStatus `json:"old_status,omitempty"`
	NewStatus    ProductStatus  `json:"new_status"`
	OldTimestamp *time.Time     `json:"old_timestamp,omitempty"`
	NewTimestamp time.Time      `json:"new_timestamp"`
}

// PostImageProduct returns the canonical Product representation of this delta's post-image.
func (d ItemDelta) PostImageProduct() Product {
	return Product{
		SKU:               d.SKU,
		Name:              d.NewName,
		Price:             d.NewPrice,
		Status:            d.NewStatus,
		SupplierTimestamp: d.NewTimestamp,
		UpdatedAt:         d.NewTimestamp,
	}
}

// PatchItem represents an individual product modification in a sparse patch payload.
type PatchItem struct {
	SKU       string        `json:"sku"`
	Name      string        `json:"name"`
	Price     float64       `json:"price"`
	Status    ProductStatus `json:"status"`
	Timestamp *time.Time    `json:"timestamp,omitempty"`
}

// Validate verifies SKU non-empty, price non-negative, valid status, and non-zero timestamp if provided.
func (p *PatchItem) Validate() error {
	if strings.TrimSpace(p.SKU) == "" {
		return ErrEmptySKU
	}
	if math.IsNaN(p.Price) || math.IsInf(p.Price, 0) || p.Price < 0 {
		return ErrNegativePrice
	}
	if !p.Status.IsValid() {
		return ErrInvalidStatus
	}
	if p.Timestamp != nil && p.Timestamp.IsZero() {
		return ErrZeroTimestamp
	}
	return nil
}

// ToSupplierUpdate converts a PatchItem into a SupplierUpdate, using fallbackTime if Timestamp is nil.
func (p *PatchItem) ToSupplierUpdate(fallbackTime time.Time) SupplierUpdate {
	ts := fallbackTime
	if p.Timestamp != nil && !p.Timestamp.IsZero() {
		ts = *p.Timestamp
	}
	return SupplierUpdate{
		SKU:       p.SKU,
		Name:      p.Name,
		Price:     p.Price,
		Status:    p.Status,
		Timestamp: ts,
	}
}

// CatalogPatch encapsulates a sparse update request submitted by staff.
type CatalogPatch struct {
	PatchID     string      `json:"patch_id,omitempty"`
	Description string      `json:"description"`
	Author      string      `json:"author"`
	Items       []PatchItem `json:"items"`
}

// Validate verifies that description, author, and items are present and valid.
func (p *CatalogPatch) Validate() error {
	if strings.TrimSpace(p.Description) == "" {
		return ErrEmptyPatchDescription
	}
	if strings.TrimSpace(p.Author) == "" {
		return ErrEmptyPatchAuthor
	}
	if len(p.Items) == 0 {
		return ErrEmptyPatchItems
	}
	return nil
}

// DeadLetterItem represents an invalid or invariant-violating patch item quarantined during preview or processing.
type DeadLetterItem struct {
	SKU        string    `json:"sku"`
	Reason     string    `json:"reason"`
	RawPayload string    `json:"raw_payload,omitempty"`
	Timestamp  time.Time `json:"timestamp,omitempty"`
}

// PatchPreviewResult holds projected counts, deltas, and invalid records for previewing before commit.
type PatchPreviewResult struct {
	CatalogChecksum string             `json:"catalog_checksum"`
	Summary         ChangeSummary      `json:"summary"`
	AddedCount      int                `json:"added_count"`
	UpdatedCount    int                `json:"updated_count"`
	WithdrawnCount  int                `json:"withdrawn_count"`
	SkippedCount    int                `json:"skipped_count"`
	DeadLetterCount int                `json:"dead_letter_count"`
	TotalItems      int                `json:"total_items"`
	Deltas          []ItemDelta        `json:"deltas"`
	DeadLetterItems []DeadLetterItem   `json:"dead_letter_items,omitempty"`
	InvalidRecords  []DeadLetterRecord `json:"invalid_records,omitempty"`
	CanCommit       bool               `json:"can_commit"`
}

// VersionDiff represents the complete change-set of a catalog version milestone.
type VersionDiff struct {
	Version CatalogVersion `json:"version"`
	Deltas  []ItemDelta    `json:"deltas"`
}
