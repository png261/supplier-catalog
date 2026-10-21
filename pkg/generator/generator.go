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

	capacity := cfg.ReservoirCapacity
	if capacity <= 0 {
		capacity = 1000
	}

	return &generator{
		cfg:         cfg,
		rng:         rng,
		pcg:         pcg,
		reservoir:   NewRingBuffer(capacity),
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
	if newPrice <= 0 || math.Abs(newPrice-item.Price) < 1e-4 {
		newPrice = item.Price + 1.00
	}

	recordTime := g.currentTime
	update := domain.SupplierUpdate{
		SKU:       item.SKU,
		Name:      item.Name,
		Price:     newPrice,
		Status:    domain.StatusActive,
		Timestamp: recordTime,
	}

	// Update reservoir item state
	item.Price = newPrice
	item.Status = domain.StatusActive
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
	if alteredPrice <= 0 || math.Abs(alteredPrice-item.Price) < 1e-4 {
		alteredPrice = item.Price + 2.00
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
	update := domain.SupplierUpdate{
		SKU:       item.SKU,
		Name:      item.Name,
		Price:     item.Price,
		Status:    domain.StatusWithdrawn,
		Timestamp: recordTime,
	}
	item.Status = domain.StatusWithdrawn
	item.Timestamp = recordTime
	g.reservoir.UpdateAt(idx, item)

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
			sku = "   \t\n"
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
