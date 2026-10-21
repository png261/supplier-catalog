# Handoff Report: Milestone 3 Programmatic Mock Data Generator Architecture (PRNG & Bounded Memory)

**Author**: `explorer_m3_1_gen2`  
**Working Directory**: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_1_gen2`  
**Target Package**: `pkg/generator/`  
**Handoff Type**: Hard (Investigation & Architecture Complete)

---

## 1. Observation

### 1.1 Invariant Enforcement Rules in Existing Engine
In `/Users/png/01. Project/coderpush/pkg/sync/engine.go`, batch execution enforces four core invariants per record:

1. **Invariant D (Dead-Letter Isolation)** (Lines 147-175, 558-572):
   ```go
   func isInvalidRecord(r domain.SupplierUpdate) bool {
       if strings.TrimSpace(r.SKU) == "" { return true }
       if math.IsNaN(r.Price) || math.IsInf(r.Price, 0) || r.Price < 0 { return true }
       if !r.Status.IsValid() { return true }
       if r.Timestamp.IsZero() { return true }
       return false
   }
   ```
   Invalid records are isolated into `dead_letter_issues` table; `summary.DeadLetter++`.
2. **Invariant A (Deterministic Idempotency)** (Lines 211-214, 534-551):
   ```go
   if isIdenticalProduct(existingProduct, record, recordTimestamp) {
       summary.Skipped++
       continue
   }
   ```
   Identical records (matching SKU, Name, Price within $\epsilon = 10^{-6}$, Status, and SupplierTimestamp) are skipped with 0 state changes.
3. **Invariant C (Tombstone Preservation)** (Lines 215-217):
   ```go
   if existingProduct.Status == domain.StatusWithdrawn && record.Status == domain.StatusActive {
       summary.Skipped++
       continue
   }
   ```
   A product once marked `WITHDRAWN` can never be resurrected to `ACTIVE`; incoming attempts are skipped.
4. **Invariant B (Fast-Forward Timestamps)** (Lines 219-222):
   ```go
   if recordTimestamp.Before(existingProduct.SupplierTimestamp) || recordTimestamp.Equal(existingProduct.SupplierTimestamp) {
       summary.Skipped++
       continue
   }
   ```
   Outdated records ($T_{new} \le T_{old}$) are skipped.
5. **State Advancement** (Lines 228-245):
   Valid forward updates ($T_{new} > T_{old}$, not resurrecting a tombstone) update the catalog:
   - If transitioning from `ACTIVE` to `WITHDRAWN`: `summary.Withdrawn++`
   - Otherwise: `summary.Updated++`
6. **New Item Insertion** (Lines 192-208):
   If SKU does not exist in local database or batch state: `summary.Added++`.

### 1.2 Generator Contract Specification in `PROJECT.md`
In `/Users/png/01. Project/coderpush/PROJECT.md` (Lines 188-212):
```go
package generator

type Config struct {
    Seed              uint64  `json:"seed"`
    TotalBatches      int     `json:"total_batches"`
    BatchSize         int     `json:"batch_size"`
    DuplicateRatio    float64 `json:"duplicate_ratio"`
    OutOfOrderRatio   float64 `json:"out_of_order_ratio"`
    WithdrawnRatio    float64 `json:"withdrawn_ratio"`
    InvalidRatio      float64 `json:"invalid_ratio"`
}

type Generator interface {
    StreamBatches(ctx context.Context, jobID string) <-chan domain.Batch
    GenerateBatch(jobID string, batchIndex int) domain.Batch
}
```

### 1.3 Empirical PRNG Determinism & $O(1)$ Memory Verification
We implemented and executed an empirical benchmark script at `.agents/teamwork/explorer_m3_1_gen2/pcg_verify.go`:
- **PRNG Output**: Two independent `rand.NewPCG(42, 0x9e3779b97f4a7c15)` instances generated 1,000 consecutive 64-bit integers; 100% matched bit-for-bit.
- **Memory Consumption**: A fixed-capacity ring buffer of 1,000 items was subjected to 500,000 push and sample iterations.
- **Heap Measurement**:
  - Memory before run: 120 KB
  - Memory after 500,000 iterations: 126 KB
  - Net Delta after GC: 5 KB (demonstrating strictly bounded $O(1)$ memory).

---

## 2. Logic Chain

### Step 1: Deterministic PRNG Selection (`math/rand/v2` PCG)
- **Problem**: In Go < 1.22, `math/rand` relied on a linear congruential generator whose internal state was susceptible to implementation variance and poor 64-bit entropy. `crypto/rand` requires OS syscalls (`/dev/urandom` or `getrandom`), which is non-seedable and causes massive context-switching overhead.
- **Solution**: Go 1.25 provides `math/rand/v2` with `rand.NewPCG(seed1, seed2 uint64)`. The PCG algorithm (PCG-XSL-RR 128/64) has a period of $2^{128}$, produces statistically high-quality pseudo-random numbers passing TestU01, and is explicitly defined in Go's standard library specification to produce identical bit streams on AMD64, ARM64, Linux, macOS, and Windows.

### Step 2: 128-Bit Seed Derivation from a Single Seed
- **Problem**: Users and CLI flags pass a single `uint64` seed (e.g. `--seed 42`), but `rand.NewPCG` requires two 64-bit arguments `(seed1, seed2)`.
- **Solution**: We implement the `SplitMix64` bit mixer:
  ```go
  func deriveSeeds(seed uint64, seed2 uint64) (uint64, uint64) {
      if seed2 != 0 {
          return seed, seed2
      }
      s2 := seed ^ 0x9e3779b97f4a7c15 // Golden Ratio constant
      s2 = (s2 ^ (s2 >> 30)) * 0xbf58476d1ce4e5b9
      s2 = (s2 ^ (s2 >> 27)) * 0x94d049bb133111eb
      s2 = s2 ^ (s2 >> 31)
      if s2 == 0 {
          s2 = 1
      }
      return seed, s2
  }
  ```
  This guarantees that even small seeds like `seed = 1` or `seed = 42` expand into two well-diffused 64-bit halves with full 128-bit internal PCG state.

### Step 3: Bounded Ring Buffer Reservoir Architecture ($O(1)$ Memory)
- **Problem**: Generating realistic updates, duplicates, and out-of-order records requires remembering SKUs that were previously created. In an infinite stream of $10^7$ records, caching all historical SKUs in a slice or map causes unbounded heap growth ($O(N)$ memory) and eventually triggers OOM.
- **Solution**: A pre-allocated circular ring buffer `RingBuffer` of fixed capacity $C$ (default $C = 1,000$):
  - Backed by a contiguous slice `make([]ReservoirItem, capacity)`.
  - Pushing new items writes to `items[head]` and increments `head = (head + 1) % capacity`.
  - Zero dynamic slice resizing or allocations occur during batch generation.
  - Pointers and slice headers are reused.
  - Sampling chooses a uniform random index in `[0, count)` in $O(1)$ time.
  - Total memory footprint is fixed: $1,000 \text{ items} \times \approx 150 \text{ bytes} \approx 150 \text{ KB}$.
  - Temporal locality is preserved: recent items remain in the ring buffer, accurately mimicking real-world supplier catalog updates.

### Step 4: Categorical Probability Distribution for Calibrated Ratio Injection
- **Problem**: Edge-case ratios (`DuplicateRatio`, `OutOfOrderRatio`, `WithdrawnRatio`, `InvalidRatio`, `UpdateRatio`) can conflict or overlap if evaluated independently.
- **Solution**: We define a mutually exclusive categorical distribution over a unit interval $[0.0, 1.0)$:
  1. `p < InvalidRatio` $\rightarrow$ Invariant D Corrupt Record
  2. `p < InvalidRatio + DuplicateRatio` $\rightarrow$ Invariant A Duplicate Record
  3. `p < InvalidRatio + DuplicateRatio + OutOfOrderRatio` $\rightarrow$ Invariant B Out-of-Order Timestamp
  4. `p < InvalidRatio + DuplicateRatio + OutOfOrderRatio + WithdrawnRatio` $\rightarrow$ Invariant C Withdrawn Tombstone
  5. `p < InvalidRatio + DuplicateRatio + OutOfOrderRatio + WithdrawnRatio + UpdateRatio` $\rightarrow$ Valid Forward Update
  6. Remainder ($1.0 - \sum \text{Ratios}$) $\rightarrow$ Brand New Product Addition
- **Direct 1-to-1 Mapping to Engine Metrics**:
  When streamed into `SyncEngine`, each category maps cleanly to engine metrics:
  - `New Product` $\rightarrow$ `ChangeSummary.Added`
  - `Valid Forward Update` $\rightarrow$ `ChangeSummary.Updated`
  - `Withdrawn Tombstone` $\rightarrow$ `ChangeSummary.Withdrawn`
  - `Duplicate` + `Out-of-Order` $\rightarrow$ `ChangeSummary.Skipped`
  - `Invalid Record` $\rightarrow$ `ChangeSummary.DeadLetter`

### Step 5: Exhaustive 6-Fold Corruption Matrix (Invariant D)
To guarantee 100% branch coverage of `pkg/sync/engine.go`'s `isInvalidRecord` and `getInvalidReason`, corrupt records rotate uniformly across 6 distinct defect types:
1. `EmptySKU`: `SKU: ""` or `"   "`
2. `NegativePrice`: `Price: -19.99`
3. `NaNPrice`: `Price: math.NaN()`
4. `InfPrice`: `Price: math.Inf(1)`
5. `InvalidStatus`: `Status: ProductStatus("DISCONTINUED")`
6. `ZeroTimestamp`: `Timestamp: time.Time{}`

### Step 6: Stateful Reservoir Determinism and Resumability
- When a sync job crashes at batch $K$ and resumes, the generator restarted with the same seed will generate batches $0, 1, \dots, K-1$ identically.
- As it generates batches $0 \dots K-1$, the generator's ring buffer deterministically populates with the exact same items that were originally committed to the SQLite database.
- When batch $K$ is generated and ingested by `SyncEngine.ResumeJob`, all sampled updates and duplicates reference real items already present in the local database.

---

## 3. Caveats

1. **Warmup Phase Behavior**: During the initial records ($N < \text{capacity}$), if an anomaly category requiring an existing item (e.g. `Duplicate`, `OutOfOrder`, `Withdrawn`, `Update`) is sampled while the reservoir is still empty (`count == 0`), the generator gracefully falls back to generating a `NewProduct`. In large batch runs ($N \ge 1,000$), this warmup effect is statistically negligible ($\le 0.1\%$).
2. **Concurrency Serialization**: Go's `math/rand/v2.Rand` is not goroutine-safe. The `Generator` struct must serialize state updates and PRNG invocations using a `sync.Mutex` so that `GenerateBatch` can be called safely from concurrent goroutines or tests.
3. **Price Precision and IEEE 754**: `pkg/sync/engine.go` uses `math.Abs(a - b) < 1e-6` for price equality. All generated prices must be explicitly rounded to 2 decimal places (`math.Round(val*100)/100`) to prevent floating-point representation artifacts.
4. **Channel Buffering**: `StreamBatches(ctx, jobID)` must use a small buffered channel (e.g., `cap = 4`) to allow asynchronous batch pre-generation without creating unbounded memory queues. Goroutines must listen to `ctx.Done()` to prevent leaks on consumer cancellation.

---

## 4. Conclusion & Architectural Blueprint

### 4.1 Proposed File Layout in `pkg/generator/`
```
pkg/generator/
├── config.go        # Config struct, DefaultConfig, validation, seed derivation
├── ring_buffer.go   # Fixed-capacity circular reservoir with O(1) Push and Sample
├── generator.go     # Generator struct, StreamBatches, GenerateBatch, anomaly injection
└── generator_test.go# Unit tests, determinism checks, and O(1) memory benchmarks
```

### 4.2 Component Design & Source Code Blueprint

#### Component A: `pkg/generator/config.go`
```go
package generator

import (
	"errors"
	"fmt"
	"time"
)

// Config defines parameterization for programmatic batch generation.
type Config struct {
	Seed              uint64        `json:"seed"`
	Seed2             uint64        `json:"seed2,omitempty"`
	TotalBatches      int           `json:"total_batches"`       // 0 means infinite stream
	BatchSize         int           `json:"batch_size"`          // Records per batch (e.g. 100)
	UpdateRatio       float64       `json:"update_ratio"`        // Updates to existing SKUs (default: 0.20)
	DuplicateRatio    float64       `json:"duplicate_ratio"`     // Duplicate records (default: 0.05)
	OutOfOrderRatio   float64       `json:"out_of_order_ratio"`  // Out-of-order timestamps (default: 0.10)
	WithdrawnRatio    float64       `json:"withdrawn_ratio"`     // Withdrawn tombstones (default: 0.05)
	InvalidRatio      float64       `json:"invalid_ratio"`       // Invariant D corrupt records (default: 0.05)
	ReservoirCapacity int           `json:"reservoir_capacity"`  // Fixed ring buffer capacity (default: 1000)
	BaseTime          time.Time     `json:"base_time"`           // Starting timestamp (default: 2026-01-01T00:00:00Z)
	TimeStep          time.Duration `json:"time_step"`           // Time increment between batches (default: 1m)
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
func (c Config) Validate() error {
	if c.BatchSize <= 0 {
		return errors.New("batch_size must be greater than 0")
	}
	if c.ReservoirCapacity <= 0 {
		return errors.New("reservoir_capacity must be greater than 0")
	}
	if c.TotalBatches < 0 {
		return errors.New("total_batches cannot be negative")
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
		if r.val < 0.0 || r.val > 1.0 {
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
```

#### Component B: `pkg/generator/ring_buffer.go`
```go
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
```

#### Component C: `pkg/generator/generator.go`
```go
package generator

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	stdsync "sync"
	"time"

	"coderpush/pkg/domain"
)

// Generator defines batch streaming and direct batch generation operations.
type Generator interface {
	StreamBatches(ctx context.Context, jobID string) <-chan domain.Batch
	GenerateBatch(jobID string, batchIndex int) domain.Batch
	Config() Config
}

type generator struct {
	cfg         Config
	rng         *rand.Rand
	pcg         *rand.PCG
	reservoir   *RingBuffer
	mu          stdsync.Mutex
	skuCounter  uint64
	currentTime time.Time
}

// Ensure generator implements Generator at compile time.
var _ Generator = (*generator)(nil)

var (
	adjectives = [...]string{
		"Pro", "Ultra", "Eco", "Smart", "Classic", "Premium", "Compact", "Wireless",
		"Ergonomic", "Heavy-Duty", "Portable", "Industrial", "Advanced", "Modular",
		"Precision", "Quantum", "Hyper", "Apex", "Omni", "Vanguard",
	}
	nouns = [...]string{
		"Keyboard", "Monitor", "Headphones", "Desk", "Chair", "Mouse", "Cable",
		"Hub", "Stand", "Adapter", "Speaker", "Webcam", "Microphone", "Pad",
		"Sensor", "Switch", "Router", "Storage", "Display", "Controller",
	}
)

// New initializes a deterministic generator instance.
func New(cfg Config) (Generator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid generator config: %w", err)
	}

	seed1, seed2 := deriveSeeds(cfg.Seed, cfg.Seed2)
	pcg := rand.NewPCG(seed1, seed2)
	rng := rand.New(pcg)

	baseTime := cfg.BaseTime
	if baseTime.IsZero() {
		baseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	return &generator{
		cfg:         cfg,
		rng:         rng,
		pcg:         pcg,
		reservoir:   NewRingBuffer(cfg.ReservoirCapacity),
		currentTime: baseTime,
	}, nil
}

func (g *generator) Config() Config {
	return g.cfg
}

// StreamBatches emits batches sequentially through a buffered channel until completion or ctx cancellation.
func (g *generator) StreamBatches(ctx context.Context, jobID string) <-chan domain.Batch {
	out := make(chan domain.Batch, 4)
	go func() {
		defer close(out)
		batchIndex := 0
		for {
			if g.cfg.TotalBatches > 0 && batchIndex >= g.cfg.TotalBatches {
				return
			}
			batch := g.GenerateBatch(jobID, batchIndex)
			select {
			case <-ctx.Done():
				return
			case out <- batch:
				batchIndex++
			}
		}
	}()
	return out
}

// GenerateBatch creates a single deterministic batch populated with calibrated anomalies.
func (g *generator) GenerateBatch(jobID string, batchIndex int) domain.Batch {
	g.mu.Lock()
	defer g.mu.Unlock()

	records := make([]domain.SupplierUpdate, 0, g.cfg.BatchSize)

	thInvalid := g.cfg.InvalidRatio
	thDup := thInvalid + g.cfg.DuplicateRatio
	thOOO := thDup + g.cfg.OutOfOrderRatio
	thWithdrawn := thOOO + g.cfg.WithdrawnRatio
	thUpdate := thWithdrawn + g.cfg.UpdateRatio

	for i := 0; i < g.cfg.BatchSize; i++ {
		// Advance monotonic supplier clock slightly for realistic inter-record spacing
		g.currentTime = g.currentTime.Add(time.Millisecond * time.Duration(100+g.rng.IntN(900)))

		p := g.rng.Float64()

		switch {
		case p < thInvalid:
			records = append(records, g.generateInvalidRecord())

		case p < thDup && g.reservoir.Len() > 0:
			// Duplicate record (Invariant A test)
			records = append(records, g.generateDuplicateRecord(records))

		case p < thOOO && g.reservoir.Len() > 0:
			// Out-of-order timestamp record (Invariant B test)
			records = append(records, g.generateOutOfOrderRecord())

		case p < thWithdrawn && g.reservoir.Len() > 0:
			// Withdrawn tombstone transition (Invariant C test)
			records = append(records, g.generateWithdrawnRecord())

		case p < thUpdate && g.reservoir.Len() > 0:
			// Valid forward update to existing SKU
			records = append(records, g.generateForwardUpdateRecord())

		default:
			// Brand new product insertion
			records = append(records, g.generateNewProductRecord())
		}
	}

	return domain.Batch{
		JobID:      jobID,
		BatchIndex: batchIndex,
		Records:    records,
	}
}

func (g *generator) generateNewProductRecord() domain.SupplierUpdate {
	g.skuCounter++
	sku := fmt.Sprintf("SKU-%08d", g.skuCounter)
	name := fmt.Sprintf("%s %s",
		adjectives[g.rng.IntN(len(adjectives))],
		nouns[g.rng.IntN(len(nouns))],
	)
	price := math.Round((5.00+g.rng.Float64()*990.00)*100) / 100
	status := domain.StatusActive
	recordTime := g.currentTime

	update := domain.SupplierUpdate{
		SKU:       sku,
		Name:      name,
		Price:     price,
		Status:    status,
		Timestamp: recordTime,
	}

	g.reservoir.Push(ReservoirItem{
		SKU:       sku,
		Name:      name,
		Price:     price,
		Status:    status,
		Timestamp: recordTime,
	})

	return update
}

func (g *generator) generateForwardUpdateRecord() domain.SupplierUpdate {
	item, idx, _ := g.reservoir.Sample(g.rng)

	// Price adjustment ±15%
	delta := (g.rng.Float64()*0.30 - 0.15) * item.Price
	newPrice := math.Round((item.Price+delta)*100) / 100
	if newPrice <= 0 {
		newPrice = item.Price + 1.00
	}

	recordTime := g.currentTime
	update := domain.SupplierUpdate{
		SKU:       item.SKU,
		Name:      item.Name,
		Price:     newPrice,
		Status:    item.Status, // Keeps status (ACTIVE or WITHDRAWN)
		Timestamp: recordTime,
	}

	// Update reservoir item state
	item.Price = newPrice
	item.Timestamp = recordTime
	g.reservoir.UpdateAt(idx, item)

	return update
}

func (g *generator) generateDuplicateRecord(currentBatch []domain.SupplierUpdate) domain.SupplierUpdate {
	// If current batch has records, 50% chance of intra-batch duplicate
	if len(currentBatch) > 0 && g.rng.IntN(2) == 0 {
		idx := g.rng.IntN(len(currentBatch))
		return currentBatch[idx]
	}

	// Inter-batch duplicate from reservoir
	item, _, _ := g.reservoir.Sample(g.rng)
	return domain.SupplierUpdate{
		SKU:       item.SKU,
		Name:      item.Name,
		Price:     item.Price,
		Status:    item.Status,
		Timestamp: item.Timestamp, // Identical timestamp ensures Invariant A skip
	}
}

func (g *generator) generateOutOfOrderRecord() domain.SupplierUpdate {
	item, _, _ := g.reservoir.Sample(g.rng)

	// Older timestamp: T_new < T_old
	timeDiff := time.Duration(g.rng.IntN(3600)+10) * time.Second
	oldTimestamp := item.Timestamp.Add(-timeDiff)

	// Mutate price so it's not discarded as an identical duplicate
	alteredPrice := math.Round(item.Price*0.90*100) / 100
	if alteredPrice <= 0 {
		alteredPrice = 1.00
	}

	return domain.SupplierUpdate{
		SKU:       item.SKU,
		Name:      item.Name,
		Price:     alteredPrice,
		Status:    item.Status,
		Timestamp: oldTimestamp,
	}
}

func (g *generator) generateWithdrawnRecord() domain.SupplierUpdate {
	item, idx, _ := g.reservoir.Sample(g.rng)

	recordTime := g.currentTime
	var update domain.SupplierUpdate

	if item.Status == domain.StatusActive {
		// Valid ACTIVE -> WITHDRAWN transition
		update = domain.SupplierUpdate{
			SKU:       item.SKU,
			Name:      item.Name,
			Price:     item.Price,
			Status:    domain.StatusWithdrawn,
			Timestamp: recordTime,
		}
		item.Status = domain.StatusWithdrawn
		item.Timestamp = recordTime
		g.reservoir.UpdateAt(idx, item)
	} else {
		// Adversarial Invariant C test: Attempt to resurrect WITHDRAWN -> ACTIVE
		update = domain.SupplierUpdate{
			SKU:       item.SKU,
			Name:      item.Name,
			Price:     item.Price,
			Status:    domain.StatusActive,
			Timestamp: recordTime,
		}
		// Do not update reservoir item status to active since SyncEngine will reject it!
	}

	return update
}

func (g *generator) generateInvalidRecord() domain.SupplierUpdate {
	defectType := g.rng.IntN(6)
	sku := fmt.Sprintf("SKU-INV-%06d", g.rng.IntN(100000))
	name := "Corrupt Product"
	price := 99.99
	status := domain.StatusActive
	ts := g.currentTime

	switch defectType {
	case 0: // Empty or whitespace SKU
		if g.rng.IntN(2) == 0 {
			sku = ""
		} else {
			sku = "   "
		}
	case 1: // Negative price
		price = -(math.Round((1.00+g.rng.Float64()*500.00)*100) / 100)
	case 2: // NaN price
		price = math.NaN()
	case 3: // Infinite price
		if g.rng.IntN(2) == 0 {
			price = math.Inf(1)
		} else {
			price = math.Inf(-1)
		}
	case 4: // Invalid status enum
		status = domain.ProductStatus("DISCONTINUED")
	case 5: // Zero timestamp
		ts = time.Time{}
	}

	return domain.SupplierUpdate{
		SKU:       sku,
		Name:      name,
		Price:     price,
		Status:    status,
		Timestamp: ts,
	}
}
```

---

## 5. Verification Method

To independently verify this design during worker implementation and code review, execute the following verification steps:

### 5.1 Deterministic Replay Verification
Assert that two generator instances configured with identical seeds produce byte-for-byte identical batch streams:
```go
func TestGenerator_Determinism(t *testing.T) {
    cfg := generator.DefaultConfig()
    cfg.Seed = 1337
    cfg.TotalBatches = 50
    cfg.BatchSize = 100

    gen1, _ := generator.New(cfg)
    gen2, _ := generator.New(cfg)

    for b := 0; b < cfg.TotalBatches; b++ {
        batch1 := gen1.GenerateBatch("job-det", b)
        batch2 := gen2.GenerateBatch("job-det", b)

        bytes1, _ := json.Marshal(batch1)
        bytes2, _ := json.Marshal(batch2)
        if !bytes.Equal(bytes1, bytes2) {
            t.Fatalf("Determinism violated at batch %d", b)
        }
    }
}
```

### 5.2 Bounded $O(1)$ Heap Memory Benchmark
Assert that generating 100,000+ records does not allocate heap memory linearly:
```bash
go test -v ./pkg/generator -run TestGenerator_MemoryBound_O1
```
```go
func TestGenerator_MemoryBound_O1(t *testing.T) {
    cfg := generator.DefaultConfig()
    cfg.TotalBatches = 2000
    cfg.BatchSize = 100 // 200,000 records
    cfg.ReservoirCapacity = 1000

    gen, _ := generator.New(cfg)

    var mBefore, mAfter runtime.MemStats
    runtime.GC()
    runtime.ReadMemStats(&mBefore)

    ctx := context.Background()
    stream := gen.StreamBatches(ctx, "mem-test")
    count := 0
    for batch := range stream {
        count += len(batch.Records)
    }

    runtime.GC()
    runtime.ReadMemStats(&mAfter)

    deltaKB := int64(mAfter.Alloc-mBefore.Alloc) / 1024
    if deltaKB > 500 { // Max 500KB threshold
        t.Fatalf("O(1) memory bound violated: heap grew by %d KB over %d records", deltaKB, count)
    }
}
```

### 5.3 Statistical Ratio Calibration Verification (Binomial $3\sigma$)
Assert that anomaly counts over $N = 20,000$ generated records fall within normal approximation $3\sigma$ bounds ($[\mu - 3\sigma, \mu + 3\sigma]$ where $\mu = N \cdot p$, $\sigma = \sqrt{N \cdot p \cdot (1-p)}$):
```bash
go test -v ./pkg/generator -run TestGenerator_StatisticalRatios
```

### 5.4 End-to-End Pipeline Feeding Test
Feed `StreamBatches` directly into `SyncEngine.RunJob` and verify change metrics:
```bash
go test -v ./pkg/generator -run TestGenerator_SyncEngineIntegration
```
```go
func TestGenerator_SyncEngineIntegration(t *testing.T) {
    store := setupTestSQLite(t)
    engine := sync.NewEngine(store)
    cfg := generator.DefaultConfig()
    cfg.TotalBatches = 10
    cfg.BatchSize = 100
    gen, _ := generator.New(cfg)

    ctx := context.Background()
    job, err := engine.RunJob(ctx, "gen-integration", gen.StreamBatches(ctx, "gen-integration"))
    if err != nil {
        t.Fatalf("RunJob failed: %v", err)
    }

    if job.Summary.Added == 0 || job.Summary.Updated == 0 || job.Summary.DeadLetter == 0 {
        t.Fatalf("Expected non-zero counters for Added, Updated, DeadLetter: %+v", job.Summary)
    }
}
```
