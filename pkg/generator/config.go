package generator

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Config defines parameterization for programmatic batch generation.
type Config struct {
	Seed              uint64        `json:"seed"`
	Seed2             uint64        `json:"seed2,omitempty"`
	TotalBatches      int           `json:"total_batches"`           // 0 means infinite stream
	BatchSize         int           `json:"batch_size"`              // Records per batch (e.g. 100)
	UpdateRatio       float64       `json:"update_ratio"`            // Updates to existing SKUs (default: 0.20)
	DuplicateRatio    float64       `json:"duplicate_ratio"`         // Duplicate records (default: 0.05)
	OutOfOrderRatio   float64       `json:"out_of_order_ratio"`      // Out-of-order timestamps (default: 0.10)
	WithdrawnRatio    float64       `json:"withdrawn_ratio"`         // Withdrawn tombstones (default: 0.05)
	InvalidRatio      float64       `json:"invalid_ratio"`           // Invariant D corrupt records (default: 0.05)
	CorruptRatio      float64       `json:"corrupt_ratio,omitempty"` // Alias for InvalidRatio
	ReservoirCapacity int           `json:"reservoir_capacity"`      // Fixed ring buffer capacity (default: 1000)
	BaseTime          time.Time     `json:"base_time"`               // Starting timestamp (default: 2026-01-01T00:00:00Z)
	TimeStep          time.Duration `json:"time_step"`               // Time increment between batches (default: 1m)
}

// DefaultConfig returns calibrated defaults aligned with project specifications.
func DefaultConfig() Config {
	return Config{
		Seed:              42,
		Seed2:             0,
		TotalBatches:      10,
		BatchSize:         100,
		UpdateRatio:       0.20,
		DuplicateRatio:    0.05,
		OutOfOrderRatio:   0.10,
		WithdrawnRatio:    0.05,
		InvalidRatio:      0.05,
		ReservoirCapacity: 1000,
		BaseTime:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeStep:          1 * time.Minute,
	}
}

// Validate verifies ratio boundaries and non-negative constraints.
func (c *Config) Validate() error {
	if c.BatchSize <= 0 {
		return errors.New("batch_size must be greater than 0")
	}
	if c.ReservoirCapacity <= 0 {
		return errors.New("reservoir_capacity must be greater than 0")
	}
	if c.TotalBatches < 0 {
		return errors.New("total_batches cannot be negative")
	}

	// Support CorruptRatio alias if InvalidRatio is not explicitly set
	if c.InvalidRatio == 0.0 && c.CorruptRatio > 0.0 {
		c.InvalidRatio = c.CorruptRatio
	}

	ratios := []struct {
		name string
		val  float64
	}{
		{"update_ratio", c.UpdateRatio},
		{"duplicate_ratio", c.DuplicateRatio},
		{"out_of_order_ratio", c.OutOfOrderRatio},
		{"withdrawn_ratio", c.WithdrawnRatio},
		{"invalid_ratio", c.InvalidRatio},
	}

	totalRatios := 0.0
	for _, r := range ratios {
		if math.IsNaN(r.val) || math.IsInf(r.val, 0) || r.val < 0.0 || r.val > 1.0 {
			return fmt.Errorf("%s must be between 0.0 and 1.0, got %f", r.name, r.val)
		}
		totalRatios += r.val
	}

	if totalRatios > 1.0 {
		return fmt.Errorf("sum of anomaly and update ratios cannot exceed 1.0, got %f", totalRatios)
	}

	return nil
}

// deriveSeeds derives two well-diffused 64-bit seeds for rand.NewPCG using SplitMix64.
func deriveSeeds(seed uint64, seed2 uint64) (uint64, uint64) {
	if seed2 != 0 {
		return seed, seed2
	}
	s2 := seed ^ 0x9e3779b97f4a7c15
	s2 = (s2 ^ (s2 >> 30)) * 0xbf58476d1ce4e5b9
	s2 = (s2 ^ (s2 >> 27)) * 0x94d049bb133111eb
	s2 = s2 ^ (s2 >> 31)
	if s2 == 0 {
		s2 = 1
	}
	return seed, s2
}
