package generator

import (
	"math/rand/v2"
	"time"

	"coderpush/pkg/domain"
)

// ReservoirItem records catalog metadata retained in memory for cross-batch mutations.
type ReservoirItem struct {
	SKU       string
	Name      string
	Price     float64
	Status    domain.ProductStatus
	Timestamp time.Time
}

// RingBuffer implements a fixed-capacity circular reservoir with O(1) space and time complexity.
type RingBuffer struct {
	items    []ReservoirItem
	capacity int
	head     int
	count    int
}

// NewRingBuffer allocates a contiguous slice of fixed capacity upfront.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 1000
	}
	return &RingBuffer{
		items:    make([]ReservoirItem, capacity),
		capacity: capacity,
	}
}

// Push adds or overwrites the oldest item in the circular buffer in O(1) time.
func (r *RingBuffer) Push(item ReservoirItem) {
	r.items[r.head] = item
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
}

// Sample returns a uniformly sampled item and its internal index.
func (r *RingBuffer) Sample(rng *rand.Rand) (ReservoirItem, int, bool) {
	if r.count == 0 {
		return ReservoirItem{}, -1, false
	}
	idx := rng.IntN(r.count)
	return r.items[idx], idx, true
}

// UpdateAt updates the metadata of an existing reservoir item at index.
func (r *RingBuffer) UpdateAt(idx int, item ReservoirItem) {
	if idx >= 0 && idx < r.count {
		r.items[idx] = item
	}
}

// Len returns current number of stored items.
func (r *RingBuffer) Len() int {
	return r.count
}

// Cap returns the maximum capacity of the ring buffer.
func (r *RingBuffer) Cap() int {
	return r.capacity
}
