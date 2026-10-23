package domain_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"coderpush/pkg/domain"
)

func TestProductStatus_IsValid(t *testing.T) {
	tests := []struct {
		status domain.ProductStatus
		valid  bool
	}{
		{domain.StatusActive, true},
		{domain.StatusWithdrawn, true},
		{domain.ProductStatus("PENDING"), false},
		{domain.ProductStatus(""), false},
		{domain.ProductStatus("active"), false},
	}

	for _, tt := range tests {
		if got := tt.status.IsValid(); got != tt.valid {
			t.Errorf("ProductStatus(%q).IsValid() = %v, want %v", tt.status, got, tt.valid)
		}
	}
}

func TestSupplierUpdate_Validate(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name    string
		update  domain.SupplierUpdate
		wantErr error
	}{
		{
			name: "valid active record",
			update: domain.SupplierUpdate{
				SKU:       "SKU-VALID",
				Name:      "Valid Item",
				Price:     10.50,
				Status:    domain.StatusActive,
				Timestamp: now,
			},
			wantErr: nil,
		},
		{
			name: "valid withdrawn record",
			update: domain.SupplierUpdate{
				SKU:       "SKU-VALID-WITHDRAWN",
				Name:      "Valid Item 2",
				Price:     0.0,
				Status:    domain.StatusWithdrawn,
				Timestamp: now,
			},
			wantErr: nil,
		},
		{
			name: "empty SKU",
			update: domain.SupplierUpdate{
				SKU:       "   ",
				Name:      "Item",
				Price:     10.0,
				Status:    domain.StatusActive,
				Timestamp: now,
			},
			wantErr: domain.ErrEmptySKU,
		},
		{
			name: "negative price",
			update: domain.SupplierUpdate{
				SKU:       "SKU-NEG",
				Name:      "Item",
				Price:     -1.0,
				Status:    domain.StatusActive,
				Timestamp: now,
			},
			wantErr: domain.ErrNegativePrice,
		},
		{
			name: "invalid status",
			update: domain.SupplierUpdate{
				SKU:       "SKU-STATUS",
				Name:      "Item",
				Price:     10.0,
				Status:    domain.ProductStatus("UNKNOWN"),
				Timestamp: now,
			},
			wantErr: domain.ErrInvalidStatus,
		},
		{
			name: "zero timestamp",
			update: domain.SupplierUpdate{
				SKU:       "SKU-ZERO-TIME",
				Name:      "Item",
				Price:     10.0,
				Status:    domain.StatusActive,
				Timestamp: time.Time{},
			},
			wantErr: domain.ErrZeroTimestamp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.update.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChangeSummary_AddAndTotal(t *testing.T) {
	c1 := domain.ChangeSummary{
		Added:      2,
		Updated:    3,
		Withdrawn:  1,
		Skipped:    4,
		DeadLetter: 1,
	}

	c2 := domain.ChangeSummary{
		Added:      1,
		Updated:    2,
		Withdrawn:  0,
		Skipped:    5,
		DeadLetter: 2,
	}

	c1.Add(c2)

	if c1.Added != 3 {
		t.Errorf("expected Added 3, got %d", c1.Added)
	}
	if c1.Updated != 5 {
		t.Errorf("expected Updated 5, got %d", c1.Updated)
	}
	if c1.Withdrawn != 1 {
		t.Errorf("expected Withdrawn 1, got %d", c1.Withdrawn)
	}
	if c1.Skipped != 9 {
		t.Errorf("expected Skipped 9, got %d", c1.Skipped)
	}
	if c1.DeadLetter != 3 {
		t.Errorf("expected DeadLetter 3, got %d", c1.DeadLetter)
	}

	total := c1.TotalProcessed()
	expectedTotal := 3 + 5 + 1 + 9 + 3
	if total != expectedTotal {
		t.Errorf("expected TotalProcessed %d, got %d", expectedTotal, total)
	}
}

func TestVersionType_IsValid(t *testing.T) {
	tests := []struct {
		v     domain.VersionType
		valid bool
	}{
		{domain.VersionTypeInitial, true},
		{domain.VersionTypePatch, true},
		{domain.VersionTypeRollback, true},
		{domain.VersionTypeSync, true},
		{domain.VersionType("INVALID"), false},
		{domain.VersionType(""), false},
		{domain.VersionType("patch"), false},
	}

	for _, tt := range tests {
		if got := tt.v.IsValid(); got != tt.valid {
			t.Errorf("VersionType(%q).IsValid() = %v, want %v", tt.v, got, tt.valid)
		}
	}
}

func TestDeltaAction_IsValid(t *testing.T) {
	tests := []struct {
		a     domain.DeltaAction
		valid bool
	}{
		{domain.DeltaActionAdded, true},
		{domain.DeltaActionUpdated, true},
		{domain.DeltaActionWithdrawn, true},
		{domain.DeltaAction("DELETED"), false},
		{domain.DeltaAction(""), false},
		{domain.DeltaAction("added"), false},
	}

	for _, tt := range tests {
		if got := tt.a.IsValid(); got != tt.valid {
			t.Errorf("DeltaAction(%q).IsValid() = %v, want %v", tt.a, got, tt.valid)
		}
	}
}

func TestPatchItem_ValidateAndConversion(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name    string
		item    domain.PatchItem
		wantErr error
	}{
		{
			name: "valid patch item without timestamp",
			item: domain.PatchItem{
				SKU:    "SKU-100",
				Name:   "Patch Item",
				Price:  19.99,
				Status: domain.StatusActive,
			},
			wantErr: nil,
		},
		{
			name: "valid patch item with timestamp",
			item: domain.PatchItem{
				SKU:       "SKU-100",
				Name:      "Patch Item",
				Price:     19.99,
				Status:    domain.StatusActive,
				Timestamp: &now,
			},
			wantErr: nil,
		},
		{
			name: "empty sku",
			item: domain.PatchItem{
				SKU:    "  ",
				Name:   "Patch Item",
				Price:  19.99,
				Status: domain.StatusActive,
			},
			wantErr: domain.ErrEmptySKU,
		},
		{
			name: "negative price",
			item: domain.PatchItem{
				SKU:    "SKU-100",
				Name:   "Patch Item",
				Price:  -5.00,
				Status: domain.StatusActive,
			},
			wantErr: domain.ErrNegativePrice,
		},
		{
			name: "NaN price",
			item: domain.PatchItem{
				SKU:    "SKU-100",
				Name:   "Patch Item",
				Price:  math.NaN(),
				Status: domain.StatusActive,
			},
			wantErr: domain.ErrNegativePrice,
		},
		{
			name: "positive Inf price",
			item: domain.PatchItem{
				SKU:    "SKU-100",
				Name:   "Patch Item",
				Price:  math.Inf(1),
				Status: domain.StatusActive,
			},
			wantErr: domain.ErrNegativePrice,
		},
		{
			name: "negative Inf price",
			item: domain.PatchItem{
				SKU:    "SKU-100",
				Name:   "Patch Item",
				Price:  math.Inf(-1),
				Status: domain.StatusActive,
			},
			wantErr: domain.ErrNegativePrice,
		},
		{
			name: "invalid status",
			item: domain.PatchItem{
				SKU:    "SKU-100",
				Name:   "Patch Item",
				Price:  10.00,
				Status: domain.ProductStatus("ARCHIVED"),
			},
			wantErr: domain.ErrInvalidStatus,
		},
		{
			name: "zero timestamp pointer",
			item: domain.PatchItem{
				SKU:       "SKU-100",
				Name:      "Patch Item",
				Price:     10.00,
				Status:    domain.StatusActive,
				Timestamp: &time.Time{},
			},
			wantErr: domain.ErrZeroTimestamp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.item.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}

	// Test ToSupplierUpdate conversion
	defaultTime := now.Add(-time.Hour)
	itemNoTS := domain.PatchItem{
		SKU:    "SKU-1",
		Name:   "Item 1",
		Price:  12.5,
		Status: domain.StatusActive,
	}
	up1 := itemNoTS.ToSupplierUpdate(defaultTime)
	if !up1.Timestamp.Equal(defaultTime) {
		t.Errorf("expected fallback time %v, got %v", defaultTime, up1.Timestamp)
	}

	customTS := now.Add(time.Minute)
	itemWithTS := domain.PatchItem{
		SKU:       "SKU-2",
		Name:      "Item 2",
		Price:     15.0,
		Status:    domain.StatusWithdrawn,
		Timestamp: &customTS,
	}
	up2 := itemWithTS.ToSupplierUpdate(defaultTime)
	if !up2.Timestamp.Equal(customTS) {
		t.Errorf("expected custom time %v, got %v", customTS, up2.Timestamp)
	}
}

func TestCatalogPatch_Validate(t *testing.T) {
	tests := []struct {
		name    string
		patch   domain.CatalogPatch
		wantErr error
	}{
		{
			name: "valid patch",
			patch: domain.CatalogPatch{
				Description: "Q4 Price Update",
				Author:      "ops@coderpush.com",
				Items: []domain.PatchItem{
					{SKU: "SKU-1", Name: "Item 1", Price: 10.0, Status: domain.StatusActive},
				},
			},
			wantErr: nil,
		},
		{
			name: "empty description",
			patch: domain.CatalogPatch{
				Description: "  ",
				Author:      "ops@coderpush.com",
				Items: []domain.PatchItem{
					{SKU: "SKU-1", Name: "Item 1", Price: 10.0, Status: domain.StatusActive},
				},
			},
			wantErr: domain.ErrEmptyPatchDescription,
		},
		{
			name: "empty author",
			patch: domain.CatalogPatch{
				Description: "Q4 Update",
				Author:      "   ",
				Items: []domain.PatchItem{
					{SKU: "SKU-1", Name: "Item 1", Price: 10.0, Status: domain.StatusActive},
				},
			},
			wantErr: domain.ErrEmptyPatchAuthor,
		},
		{
			name: "empty items",
			patch: domain.CatalogPatch{
				Description: "Q4 Update",
				Author:      "ops@coderpush.com",
				Items:       []domain.PatchItem{},
			},
			wantErr: domain.ErrEmptyPatchItems,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.patch.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestItemDelta_PostImageProduct(t *testing.T) {
	now := time.Now().UTC()
	delta := domain.ItemDelta{
		DeltaID:      1,
		VersionID:    "ver_123",
		SKU:          "SKU-TEST",
		Action:       domain.DeltaActionAdded,
		NewName:      "New Name",
		NewPrice:     99.99,
		NewStatus:    domain.StatusActive,
		NewTimestamp: now,
	}

	product := delta.PostImageProduct()
	if product.SKU != "SKU-TEST" || product.Name != "New Name" || product.Price != 99.99 || product.Status != domain.StatusActive || !product.UpdatedAt.Equal(now) {
		t.Errorf("PostImageProduct mismatch: %+v", product)
	}
}

func TestDeadLetterItem_JSON(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	item := domain.DeadLetterItem{
		SKU:        "SKU-DL-1",
		Reason:     domain.ErrResurrectionRestricted.Error(),
		RawPayload: `{"sku":"SKU-DL-1","price":10}`,
		Timestamp:  now,
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("failed to marshal DeadLetterItem: %v", err)
	}

	var unmarshaled domain.DeadLetterItem
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal DeadLetterItem: %v", err)
	}

	if unmarshaled.SKU != item.SKU || unmarshaled.Reason != item.Reason || unmarshaled.RawPayload != item.RawPayload || !unmarshaled.Timestamp.Equal(now) {
		t.Errorf("DeadLetterItem unmarshal mismatch: got %+v, want %+v", unmarshaled, item)
	}
}

func TestPatchPreviewResult_Fields(t *testing.T) {
	now := time.Now().UTC()
	res := domain.PatchPreviewResult{
		CatalogChecksum: "abc123sha256",
		Summary: domain.ChangeSummary{
			Added:      2,
			Updated:    3,
			Withdrawn:  1,
			Skipped:    4,
			DeadLetter: 1,
		},
		AddedCount:      2,
		UpdatedCount:    3,
		WithdrawnCount:  1,
		SkippedCount:    4,
		DeadLetterCount: 1,
		TotalItems:      11,
		Deltas: []domain.ItemDelta{
			{SKU: "SKU-1", Action: domain.DeltaActionAdded, NewPrice: 10.0, NewStatus: domain.StatusActive, NewTimestamp: now},
		},
		DeadLetterItems: []domain.DeadLetterItem{
			{SKU: "SKU-BAD", Reason: domain.ErrNegativePrice.Error(), Timestamp: now},
		},
		InvalidRecords: []domain.DeadLetterRecord{
			{SKU: "SKU-BAD", Reason: domain.ErrNegativePrice.Error(), CreatedAt: now},
		},
		CanCommit: false,
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("failed to marshal PatchPreviewResult: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if parsed["catalog_checksum"] != "abc123sha256" {
		t.Errorf("expected catalog_checksum 'abc123sha256', got %v", parsed["catalog_checksum"])
	}
	if parsed["added_count"] != float64(2) {
		t.Errorf("expected added_count 2, got %v", parsed["added_count"])
	}
	if parsed["can_commit"] != false {
		t.Errorf("expected can_commit false, got %v", parsed["can_commit"])
	}
}
