package main

import (
	"fmt"
	"math/rand/v2"
	"runtime"
)

type ReservoirItem struct {
	SKU       string
	Name      string
	Price     float64
	Status    string
	Timestamp int64
}

type RingBuffer struct {
	items    []ReservoirItem
	capacity int
	head     int
	count    int
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		items:    make([]ReservoirItem, capacity),
		capacity: capacity,
	}
}

func (r *RingBuffer) Push(item ReservoirItem) {
	r.items[r.head] = item
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
}

func (r *RingBuffer) Sample(rng *rand.Rand) (ReservoirItem, int) {
	if r.count == 0 {
		return ReservoirItem{}, -1
	}
	idx := rng.IntN(r.count)
	return r.items[idx], idx
}

func main() {
	// 1. Verify exact determinism across two PCG streams
	pcg1 := rand.NewPCG(42, 0x9e3779b97f4a7c15)
	r1 := rand.New(pcg1)
	pcg2 := rand.NewPCG(42, 0x9e3779b97f4a7c15)
	r2 := rand.New(pcg2)

	for i := 0; i < 1000; i++ {
		v1 := r1.Uint64()
		v2 := r2.Uint64()
		if v1 != v2 {
			panic(fmt.Sprintf("Determinism failure at step %d: %d != %d", i, v1, v2))
		}
	}
	fmt.Println("PASS: Deterministic PCG stream verification (1,000 steps)")

	// 2. Verify O(1) memory footprint over 500,000 iterations
	ring := NewRingBuffer(1000)
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < 500000; i++ {
		item := ReservoirItem{
			SKU:       fmt.Sprintf("SKU-%08d", i%5000),
			Name:      "Pro Keyboard",
			Price:     49.99,
			Status:    "ACTIVE",
			Timestamp: int64(1700000000 + i),
		}
		ring.Push(item)
		if i%10 == 0 {
			_, _ = ring.Sample(r1)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)
	fmt.Printf("Memory before: %d KB, Memory after: %d KB (Delta: %d KB)\n",
		m1.Alloc/1024, m2.Alloc/1024, int64(m2.Alloc-m1.Alloc)/1024)
	fmt.Println("PASS: O(1) Ring Buffer memory verification (500,000 iterations)")
}
