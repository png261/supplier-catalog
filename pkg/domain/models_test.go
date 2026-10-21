package domain_test

import (
	"errors"
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
