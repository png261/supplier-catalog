package sync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	stdsync "sync"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"

	"github.com/google/uuid"
)

// PatchEngine defines high-level patch preview and publishing operations.
type PatchEngine interface {
	PreviewPatch(ctx context.Context, patch domain.CatalogPatch) (*domain.PatchPreviewResult, error)
	PublishPatch(ctx context.Context, patch domain.CatalogPatch) (*domain.CatalogVersion, []domain.ItemDelta, error)
}

// PatchService implements PatchEngine with pure in-memory preview calculation
// and atomic versioned publishing.
type PatchService struct {
	storage storage.Storage
	mu      stdsync.Mutex // Serializes publish transactions for single-writer safety
	now     func() time.Time
}

// Ensure PatchService implements PatchEngine at compile time.
var _ PatchEngine = (*PatchService)(nil)

// PatchOption allows configuring PatchService options (e.g. clock mocking for tests).
type PatchOption func(*PatchService)

// WithPatchTimeFunc configures a custom clock provider for deterministic testing.
func WithPatchTimeFunc(now func() time.Time) PatchOption {
	return func(s *PatchService) {
		s.now = now
	}
}

// NewPatchEngine initializes a new PatchService instance.
func NewPatchEngine(store storage.Storage, opts ...PatchOption) *PatchService {
	s := &PatchService{
		storage: store,
		now:     func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// PreviewPatch executes an immutable, pure in-memory calculation projecting catalog state,
// delta classifications, invariant quarantined dead-letters, and post-patch SHA-256 checksum.
// ZERO database rows are created, updated, or deleted (zero DB writes).
func PreviewPatch(ctx context.Context, s storage.Storage, patch domain.CatalogPatch) (*domain.PatchPreviewResult, error) {
	engine := NewPatchEngine(s)
	return engine.PreviewPatch(ctx, patch)
}

// PreviewPatch on PatchService executes the dry-run simulation.
func (s *PatchService) PreviewPatch(ctx context.Context, patch domain.CatalogPatch) (*domain.PatchPreviewResult, error) {
	if s.storage == nil {
		return nil, ErrNilStorage
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(patch.Items) == 0 {
		return nil, domain.ErrEmptyPatchItems
	}

	// 1. Read-only snapshot of current catalog products (zero DB writes)
	currentCatalog, err := s.loadCurrentCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load current catalog snapshot: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 2. Clone catalog into an in-memory working projection
	projectedCatalog := make(map[string]domain.Product, len(currentCatalog))
	for sku, p := range currentCatalog {
		projectedCatalog[sku] = p
	}

	// 3. Process patch items in-memory
	referenceTime := s.now()
	deltas := make([]domain.ItemDelta, 0, len(patch.Items))
	deadLetters := make([]domain.DeadLetterItem, 0)
	invalidRecords := make([]domain.DeadLetterRecord, 0)
	summary := domain.ChangeSummary{}

	for i, item := range patch.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		sku := strings.TrimSpace(item.SKU)

		// Validate basic invariants on the patch item (SKU, price, status, non-zero timestamp)
		if invalidReason := validatePatchItemBasics(item); invalidReason != "" {
			summary.DeadLetter++
			dlItem := domain.DeadLetterItem{
				SKU:        sku,
				Reason:     invalidReason,
				RawPayload: marshalItemPayload(item),
				Timestamp:  referenceTime,
			}
			deadLetters = append(deadLetters, dlItem)

			dlRecord := domain.DeadLetterRecord{
				IssueID:    fmt.Sprintf("dl-patch-preview-%d-%d", i, referenceTime.UnixNano()),
				SKU:        sku,
				RawPayload: dlItem.RawPayload,
				Reason:     invalidReason,
				CreatedAt:  referenceTime,
			}
			invalidRecords = append(invalidRecords, dlRecord)
			continue
		}

		// Resolve item timestamp
		itemTime := referenceTime
		if item.Timestamp != nil && !item.Timestamp.IsZero() {
			itemTime = item.Timestamp.UTC()
		}

		// Look up existing state in projected catalog
		existing, exists := projectedCatalog[sku]

		if !exists {
			// Newly Added Product
			delta := domain.ItemDelta{
				SKU:          sku,
				Action:       domain.DeltaActionAdded,
				NewName:      item.Name,
				NewPrice:     item.Price,
				NewStatus:    item.Status,
				NewTimestamp: itemTime,
			}
			deltas = append(deltas, delta)
			summary.Added++

			projectedCatalog[sku] = domain.Product{
				SKU:               sku,
				Name:              item.Name,
				Price:             item.Price,
				Status:            item.Status,
				SupplierTimestamp: itemTime,
				UpdatedAt:         itemTime,
			}
			continue
		}

		// Invariant A: Tombstone Preservation
		// If product is WITHDRAWN, it MUST NOT be resurrected to ACTIVE
		if existing.Status == domain.StatusWithdrawn && item.Status == domain.StatusActive {
			summary.DeadLetter++
			reason := domain.ErrResurrectionRestricted.Error()
			dlItem := domain.DeadLetterItem{
				SKU:        sku,
				Reason:     reason,
				RawPayload: marshalItemPayload(item),
				Timestamp:  itemTime,
			}
			deadLetters = append(deadLetters, dlItem)

			dlRecord := domain.DeadLetterRecord{
				IssueID:    fmt.Sprintf("dl-resurrection-%s-%d", sku, referenceTime.UnixNano()),
				SKU:        sku,
				RawPayload: dlItem.RawPayload,
				Reason:     reason,
				CreatedAt:  referenceTime,
			}
			invalidRecords = append(invalidRecords, dlRecord)
			continue
		}

		// Invariant F: Idempotency / Identical Skipping
		if isIdenticalPatchItem(existing, item, itemTime) {
			summary.Skipped++
			continue
		}

		// Invariant B: Monotonic Timestamps
		// Incoming timestamp must be strictly greater than existing record
		if itemTime.Before(existing.SupplierTimestamp) || itemTime.Equal(existing.SupplierTimestamp) {
			summary.DeadLetter++
			reason := domain.ErrOutdatedTimestamp.Error()
			dlItem := domain.DeadLetterItem{
				SKU:        sku,
				Reason:     reason,
				RawPayload: marshalItemPayload(item),
				Timestamp:  itemTime,
			}
			deadLetters = append(deadLetters, dlItem)

			dlRecord := domain.DeadLetterRecord{
				IssueID:    fmt.Sprintf("dl-stale-%s-%d", sku, referenceTime.UnixNano()),
				SKU:        sku,
				RawPayload: dlItem.RawPayload,
				Reason:     reason,
				CreatedAt:  referenceTime,
			}
			invalidRecords = append(invalidRecords, dlRecord)
			continue
		}

		// Valid Update or Withdrawal
		wasWithdrawn := existing.Status == domain.StatusWithdrawn
		nowWithdrawn := item.Status == domain.StatusWithdrawn

		action := domain.DeltaActionUpdated
		if !wasWithdrawn && nowWithdrawn {
			action = domain.DeltaActionWithdrawn
			summary.Withdrawn++
		} else {
			summary.Updated++
		}

		oldName := existing.Name
		oldPrice := existing.Price
		oldStatus := existing.Status
		oldTime := existing.SupplierTimestamp

		delta := domain.ItemDelta{
			SKU:          sku,
			Action:       action,
			OldName:      &oldName,
			NewName:      item.Name,
			OldPrice:     &oldPrice,
			NewPrice:     item.Price,
			OldStatus:    &oldStatus,
			NewStatus:    item.Status,
			OldTimestamp: &oldTime,
			NewTimestamp: itemTime,
		}
		deltas = append(deltas, delta)

		projectedCatalog[sku] = domain.Product{
			SKU:               sku,
			Name:              item.Name,
			Price:             item.Price,
			Status:            item.Status,
			SupplierTimestamp: itemTime,
			UpdatedAt:         itemTime,
		}
	}

	// 4. Calculate SHA-256 CatalogChecksum over projected catalog state
	projectedProducts := make([]domain.Product, 0, len(projectedCatalog))
	for _, p := range projectedCatalog {
		projectedProducts = append(projectedProducts, p)
	}
	catalogChecksum := CalculateCatalogChecksum(projectedProducts)

	// CanCommit is true only when zero invariant violations occurred
	canCommit := summary.DeadLetter == 0

	result := &domain.PatchPreviewResult{
		CatalogChecksum: catalogChecksum,
		Summary:         summary,
		AddedCount:      summary.Added,
		UpdatedCount:    summary.Updated,
		WithdrawnCount:  summary.Withdrawn,
		SkippedCount:    summary.Skipped,
		DeadLetterCount: summary.DeadLetter,
		TotalItems:      len(patch.Items),
		Deltas:          deltas,
		DeadLetterItems: deadLetters,
		InvalidRecords:  invalidRecords,
		CanCommit:       canCommit,
	}

	return result, nil
}

// PublishPatch applies a patch atomically inside a database transaction, recording pre-image deltas,
// updating live products table for O(1) lookups, persisting catalog_versions milestone, and gating on CanCommit.
func (s *PatchService) PublishPatch(ctx context.Context, patch domain.CatalogPatch) (*domain.CatalogVersion, []domain.ItemDelta, error) {
	if s.storage == nil {
		return nil, nil, ErrNilStorage
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := patch.Validate(); err != nil {
		return nil, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	// 1. Run preview simulation to validate invariants and verify CanCommit
	preview, err := s.PreviewPatch(ctx, patch)
	if err != nil {
		return nil, nil, err
	}
	if !preview.CanCommit {
		return nil, nil, fmt.Errorf("%w: %d dead-letter item(s) detected", domain.ErrPatchContainsInvalidRecords, preview.DeadLetterCount)
	}

	// 2. Open atomic transaction
	tx, err := s.storage.BeginTx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin publish transaction: %w", err)
	}
	defer tx.Rollback()

	// 3. Apply post-image deltas to products table for O(1) query performance
	for _, delta := range preview.Deltas {
		prod := delta.PostImageProduct()
		if err := s.storage.UpsertProductTx(ctx, tx, &prod); err != nil {
			return nil, nil, fmt.Errorf("failed upserting product %s: %w", delta.SKU, err)
		}
	}

	// 4. Create version milestone
	now := s.now()
	versionID := patch.PatchID
	if strings.TrimSpace(versionID) == "" {
		versionID = fmt.Sprintf("ver-patch-%d-%s", now.UnixNano(), uuid.New().String()[:8])
	}

	publishedDeltas := make([]domain.ItemDelta, len(preview.Deltas))
	for i, d := range preview.Deltas {
		d.VersionID = versionID
		publishedDeltas[i] = d
	}

	ver := &domain.CatalogVersion{
		VersionID:          versionID,
		Description:        patch.Description,
		Author:             patch.Author,
		Type:               domain.VersionTypePatch,
		Summary:            preview.Summary,
		TotalItemsAffected: len(publishedDeltas),
		CreatedAt:          now,
	}

	if err := s.storage.CreateVersionTx(ctx, tx, ver, publishedDeltas); err != nil {
		return nil, nil, fmt.Errorf("failed persisting patch version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed committing publish transaction: %w", err)
	}

	return ver, publishedDeltas, nil
}

// loadCurrentCatalog loads all current products from storage via read-only SELECT.
// Zero database writes or row mutations occur.
func (s *PatchService) loadCurrentCatalog(ctx context.Context) (map[string]domain.Product, error) {
	catalog := make(map[string]domain.Product)

	if s.storage.DB() != nil {
		rows, err := s.storage.DB().QueryContext(ctx, "SELECT sku, name, price, status, supplier_timestamp, updated_at FROM products;")
		if err != nil {
			return nil, fmt.Errorf("failed to query products table: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var p domain.Product
			var statusStr, suppTimeStr, updTimeStr string
			if err := rows.Scan(&p.SKU, &p.Name, &p.Price, &statusStr, &suppTimeStr, &updTimeStr); err != nil {
				return nil, fmt.Errorf("failed scanning product row: %w", err)
			}
			p.Status = domain.ProductStatus(statusStr)
			p.SupplierTimestamp, _ = storage.ParseTime(suppTimeStr)
			p.UpdatedAt, _ = storage.ParseTime(updTimeStr)
			catalog[p.SKU] = p
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating products table: %w", err)
		}
		return catalog, nil
	}

	// Fallback for mocks lacking *sql.DB
	prods, _, err := s.storage.ListProducts(ctx, "", "", 100000, 0)
	if err != nil {
		return nil, err
	}
	for _, p := range prods {
		catalog[p.SKU] = p
	}
	return catalog, nil
}

// CalculateCatalogChecksum calculates a deterministic SHA-256 hash across all catalog products,
// matching the SQLite collation ORDER BY sku ASC with printf('%.8f', price) and RFC3339Nano timestamps.
func CalculateCatalogChecksum(products []domain.Product) string {
	sorted := make([]domain.Product, len(products))
	copy(sorted, products)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].SKU < sorted[j].SKU
	})

	hasher := sha256.New()
	for _, p := range sorted {
		priceStr := fmt.Sprintf("%.8f", p.Price)
		suppTime := p.SupplierTimestamp.UTC().Format(time.RFC3339Nano)
		updTime := p.UpdatedAt.UTC().Format(time.RFC3339Nano)
		rowStr := fmt.Sprintf("%s|%s|%s|%s|%s|%s\n", p.SKU, p.Name, priceStr, string(p.Status), suppTime, updTime)
		hasher.Write([]byte(rowStr))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// CalculateCatalogChecksumFromDB calculates a deterministic SHA-256 hash over all products directly from SQLite.
func CalculateCatalogChecksumFromDB(ctx context.Context, db *sql.DB) (string, error) {
	if db == nil {
		return "", errors.New("database connection is nil")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT sku, name, printf('%.8f', price), status, supplier_timestamp, updated_at
		FROM products ORDER BY sku ASC;
	`)
	if err != nil {
		return "", fmt.Errorf("failed querying products for checksum: %w", err)
	}
	defer rows.Close()

	hasher := sha256.New()
	for rows.Next() {
		var sku, name, priceStr, status, suppTime, updTime string
		if err := rows.Scan(&sku, &name, &priceStr, &status, &suppTime, &updTime); err != nil {
			return "", fmt.Errorf("failed scanning product row: %w", err)
		}
		rowStr := fmt.Sprintf("%s|%s|%s|%s|%s|%s\n", sku, name, priceStr, status, suppTime, updTime)
		hasher.Write([]byte(rowStr))
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating product rows: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// validatePatchItemBasics checks basic field invariants (SKU, price, status, non-zero timestamp).
func validatePatchItemBasics(item domain.PatchItem) string {
	if strings.TrimSpace(item.SKU) == "" {
		return domain.ErrEmptySKU.Error()
	}
	if math.IsNaN(item.Price) || math.IsInf(item.Price, 0) || item.Price < 0 {
		return domain.ErrNegativePrice.Error()
	}
	if !item.Status.IsValid() {
		return domain.ErrInvalidStatus.Error()
	}
	if item.Timestamp != nil && item.Timestamp.IsZero() {
		return domain.ErrZeroTimestamp.Error()
	}
	return ""
}

// isIdenticalPatchItem checks whether an incoming patch item is identical to existing state.
func isIdenticalPatchItem(existing domain.Product, item domain.PatchItem, itemTime time.Time) bool {
	if existing.SKU != strings.TrimSpace(item.SKU) {
		return false
	}
	if existing.Name != item.Name {
		return false
	}
	if math.Abs(existing.Price-item.Price) >= 1e-6 {
		return false
	}
	if existing.Status != item.Status {
		return false
	}
	if item.Timestamp != nil && !existing.SupplierTimestamp.Equal(itemTime) {
		return false
	}
	return true
}

func marshalItemPayload(item domain.PatchItem) string {
	b, err := json.Marshal(item)
	if err == nil {
		return string(b)
	}
	return fmt.Sprintf("SKU:%s, Name:%s, Price:%f, Status:%s", item.SKU, item.Name, item.Price, item.Status)
}
