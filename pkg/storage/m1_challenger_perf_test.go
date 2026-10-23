package storage_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/domain"
)

// BenchmarkChallenger_GetProduct_Under5000Deltas benchmarks GetProduct
// with 2,500 active products and 5,000 historical deltas.
func BenchmarkChallenger_GetProduct_Under5000Deltas(b *testing.B) {
	ctx := context.Background()
	s := setupTestDB(b)

	now := time.Now().UTC()
	const totalProducts = 2500
	const historicalVersions = 20
	const deltasPerVersion = 250 // 20 * 250 = 5000 deltas

	// Seed products
	tx, err := s.BeginTx(ctx)
	if err != nil {
		b.Fatalf("BeginTx failed: %v", err)
	}
	for i := 0; i < totalProducts; i++ {
		status := domain.StatusActive
		if i%10 == 0 {
			status = domain.StatusWithdrawn
		}
		if err := s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-%05d", i),
			Name:              fmt.Sprintf("Benchmark Catalog Item %d", i),
			Price:             float64(10 + (i % 100)),
			Status:            status,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		}); err != nil {
			_ = tx.Rollback()
			b.Fatalf("UpsertProductTx failed: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("Commit products failed: %v", err)
	}

	// Seed 5,000 historical deltas across 20 versions
	for v := 1; v <= historicalVersions; v++ {
		txV, _ := s.BeginTx(ctx)
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-bench-hist-%03d", v),
			VersionNumber: v,
			Description:   fmt.Sprintf("Benchmark Historical version %d", v),
			Author:        "bench@coderpush.com",
			Type:          domain.VersionTypePatch,
			CreatedAt:     now.Add(time.Duration(v) * time.Hour),
		}
		deltas := make([]domain.ItemDelta, deltasPerVersion)
		for d := 0; d < deltasPerVersion; d++ {
			skuIdx := (v*deltasPerVersion + d) % totalProducts
			deltas[d] = domain.ItemDelta{
				VersionID:    ver.VersionID,
				SKU:          fmt.Sprintf("SKU-%05d", skuIdx),
				Action:       domain.DeltaActionUpdated,
				NewName:      fmt.Sprintf("Benchmark Catalog Item %d", skuIdx),
				NewPrice:     float64(15 + (skuIdx % 100)),
				NewStatus:    domain.StatusActive,
				NewTimestamp: now,
			}
		}
		if err := s.CreateVersionTx(ctx, txV, ver, deltas); err != nil {
			_ = txV.Rollback()
			b.Fatalf("CreateVersionTx failed: %v", err)
		}
		_ = txV.Commit()
	}

	targetSKU := "SKU-01250"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p, err := s.GetProduct(ctx, targetSKU)
		if err != nil || p == nil {
			b.Fatalf("GetProduct failed: %v", err)
		}
	}
}

// BenchmarkChallenger_ListProducts_Under5000Deltas benchmarks ListProducts
// with 2,500 active products and 5,000 historical deltas.
func BenchmarkChallenger_ListProducts_Under5000Deltas(b *testing.B) {
	ctx := context.Background()
	s := setupTestDB(b)

	now := time.Now().UTC()
	const totalProducts = 2500
	const historicalVersions = 20
	const deltasPerVersion = 250

	tx, _ := s.BeginTx(ctx)
	for i := 0; i < totalProducts; i++ {
		status := domain.StatusActive
		if i%10 == 0 {
			status = domain.StatusWithdrawn
		}
		_ = s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-%05d", i),
			Name:              fmt.Sprintf("Benchmark Catalog Item %d", i),
			Price:             float64(10 + (i % 100)),
			Status:            status,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	_ = tx.Commit()

	for v := 1; v <= historicalVersions; v++ {
		txV, _ := s.BeginTx(ctx)
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-bench-list-%03d", v),
			VersionNumber: v,
			Description:   fmt.Sprintf("Benchmark Historical version %d", v),
			Author:        "bench@coderpush.com",
			Type:          domain.VersionTypePatch,
			CreatedAt:     now.Add(time.Duration(v) * time.Hour),
		}
		deltas := make([]domain.ItemDelta, deltasPerVersion)
		for d := 0; d < deltasPerVersion; d++ {
			skuIdx := (v*deltasPerVersion + d) % totalProducts
			deltas[d] = domain.ItemDelta{
				VersionID:    ver.VersionID,
				SKU:          fmt.Sprintf("SKU-%05d", skuIdx),
				Action:       domain.DeltaActionUpdated,
				NewName:      fmt.Sprintf("Benchmark Catalog Item %d", skuIdx),
				NewPrice:     float64(15 + (skuIdx % 100)),
				NewStatus:    domain.StatusActive,
				NewTimestamp: now,
			}
		}
		_ = s.CreateVersionTx(ctx, txV, ver, deltas)
		_ = txV.Commit()
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		prods, total, err := s.ListProducts(ctx, "ACTIVE", "", 50, 0)
		if err != nil || total == 0 || len(prods) != 50 {
			b.Fatalf("ListProducts failed: %v, total: %d, len: %d", err, total, len(prods))
		}
	}
}

// TestChallenger_SubMillisecondLatencyUnderHighLoad tests point and paginated lookup latencies
// with 2,500 products and 6,000 historical deltas.
func TestChallenger_SubMillisecondLatencyUnderHighLoad(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()
	const totalProducts = 2500
	const historicalVersions = 24
	const deltasPerVersion = 250 // 24 * 250 = 6000 deltas

	// Seed products
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	for i := 0; i < totalProducts; i++ {
		status := domain.StatusActive
		if i%10 == 0 {
			status = domain.StatusWithdrawn
		}
		_ = s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-%05d", i),
			Name:              fmt.Sprintf("Stress Item %d", i),
			Price:             float64(5 + (i % 50)),
			Status:            status,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit products failed: %v", err)
	}

	// Seed 6,000 historical deltas
	for v := 1; v <= historicalVersions; v++ {
		txV, _ := s.BeginTx(ctx)
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-stress-%03d", v),
			VersionNumber: v,
			Description:   fmt.Sprintf("Historical version %d", v),
			Author:        "challenger@coderpush.com",
			Type:          domain.VersionTypePatch,
			CreatedAt:     now.Add(time.Duration(v) * time.Hour),
		}
		deltas := make([]domain.ItemDelta, deltasPerVersion)
		for d := 0; d < deltasPerVersion; d++ {
			skuIdx := (v*deltasPerVersion + d) % totalProducts
			deltas[d] = domain.ItemDelta{
				VersionID:    ver.VersionID,
				SKU:          fmt.Sprintf("SKU-%05d", skuIdx),
				Action:       domain.DeltaActionUpdated,
				NewName:      fmt.Sprintf("Stress Item %d", skuIdx),
				NewPrice:     float64(5 + (skuIdx % 50)),
				NewStatus:    domain.StatusActive,
				NewTimestamp: now,
			}
		}
		_ = s.CreateVersionTx(ctx, txV, ver, deltas)
		_ = txV.Commit()
	}

	// 1. Empirical Point Lookup Latency Measurement
	const pointLookups = 2000
	startPoint := time.Now()
	for i := 0; i < pointLookups; i++ {
		sku := fmt.Sprintf("SKU-%05d", (i*13)%totalProducts)
		p, err := s.GetProduct(ctx, sku)
		if err != nil || p == nil {
			t.Fatalf("GetProduct failed for %s: %v", sku, err)
		}
	}
	pointElapsed := time.Since(startPoint)
	avgPointLatency := pointElapsed / time.Duration(pointLookups)

	t.Logf("Empirical avg point lookup latency with 6,000 deltas: %v (across %d iterations)", avgPointLatency, pointLookups)
	if avgPointLatency >= 1*time.Millisecond {
		t.Fatalf("Point lookup latency regression: got %v, expected < 1ms", avgPointLatency)
	}

	// 2. Empirical Paginated Listing Latency Measurement
	const listLookups = 300
	startList := time.Now()
	for i := 0; i < listLookups; i++ {
		offset := (i * 10) % 500
		prods, total, err := s.ListProducts(ctx, "ACTIVE", "", 50, offset)
		if err != nil || total == 0 || len(prods) == 0 {
			t.Fatalf("ListProducts failed at offset %d: %v", offset, err)
		}
	}
	listElapsed := time.Since(startList)
	avgListLatency := listElapsed / time.Duration(listLookups)

	t.Logf("Empirical avg paginated list latency with 6,000 deltas: %v (across %d iterations)", avgListLatency, listLookups)
	maxListAllowed := 2 * time.Millisecond
	if isRaceDetectorEnabled() {
		maxListAllowed = 50 * time.Millisecond
	}
	if avgListLatency >= maxListAllowed {
		t.Fatalf("List query latency regression: got %v, expected < %v", avgListLatency, maxListAllowed)
	}
}

// TestChallenger_ExplainQueryPlan_IndexUtilization validates that SQLite execution plans
// for active catalog queries directly use B-Tree indexes and avoid temporary sorting.
func TestChallenger_ExplainQueryPlan_IndexUtilization(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	// 1. Check EXPLAIN QUERY PLAN for GetProduct (Point lookup by SKU)
	pointQuery := "EXPLAIN QUERY PLAN SELECT sku, name, price, status, supplier_timestamp, updated_at FROM products WHERE sku = ?;"
	rows, err := s.DB().QueryContext(ctx, pointQuery, "SKU-00001")
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN point lookup failed: %v", err)
	}
	defer rows.Close()

	var pointPlanDetails []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan detail failed: %v", err)
		}
		pointPlanDetails = append(pointPlanDetails, detail)
	}

	pointPlanJoined := strings.Join(pointPlanDetails, " | ")
	t.Logf("Point lookup query plan: %s", pointPlanJoined)

	// Point lookup must use PRIMARY KEY index or SEARCH TABLE USING INDEX
	if !strings.Contains(pointPlanJoined, "SEARCH TABLE products USING") && !strings.Contains(pointPlanJoined, "USING INDEX") {
		t.Errorf("Point lookup does not utilize index: %s", pointPlanJoined)
	}

	// 2. Check EXPLAIN QUERY PLAN for ListProducts (status filtered, ordered by SKU)
	listQuery := "EXPLAIN QUERY PLAN SELECT sku, name, price, status, supplier_timestamp, updated_at FROM products WHERE status = ? ORDER BY sku ASC LIMIT ? OFFSET ?;"
	rowsList, err := s.DB().QueryContext(ctx, listQuery, "ACTIVE", 50, 0)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN list query failed: %v", err)
	}
	defer rowsList.Close()

	var listPlanDetails []string
	for rowsList.Next() {
		var id, parent, notused int
		var detail string
		if err := rowsList.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan list plan detail failed: %v", err)
		}
		listPlanDetails = append(listPlanDetails, detail)
	}

	listPlanJoined := strings.Join(listPlanDetails, " | ")
	t.Logf("Paginated list query plan: %s", listPlanJoined)

	// Must use composite index idx_products_status_sku
	if !strings.Contains(listPlanJoined, "idx_products_status_sku") {
		t.Errorf("List query does not utilize idx_products_status_sku: %s", listPlanJoined)
	}

	// Must NOT use temporary B-tree for sorting
	if strings.Contains(strings.ToLower(listPlanJoined), "temp b-tree") {
		t.Errorf("List query uses temp b-tree for sorting: %s", listPlanJoined)
	}
}

// TestChallenger_HighContentionStress_Race tests 20 concurrent readers and 5 concurrent writers
// under the race detector to empirically verify zero data races, zero deadlocks, and zero SQLITE_BUSY errors.
func TestChallenger_HighContentionStress_Race(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()
	const seedCount = 100

	tx, _ := s.BeginTx(ctx)
	for i := 0; i < seedCount; i++ {
		_ = s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-STRESS-%03d", i),
			Name:              fmt.Sprintf("Contention Item %d", i),
			Price:             20.0 + float64(i),
			Status:            domain.StatusActive,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	_ = tx.Commit()

	var wg sync.WaitGroup
	const readerGoroutines = 20
	const writerGoroutines = 5
	const readOpsPerGoroutine = 50
	const writeOpsPerGoroutine = 10

	errCh := make(chan error, readerGoroutines*readOpsPerGoroutine+writerGoroutines*writeOpsPerGoroutine)

	// Readers
	wg.Add(readerGoroutines)
	for r := 0; r < readerGoroutines; r++ {
		go func(readerID int) {
			defer wg.Done()
			for op := 0; op < readOpsPerGoroutine; op++ {
				sku := fmt.Sprintf("SKU-STRESS-%03d", (readerID+op)%seedCount)
				p, err := s.GetProduct(ctx, sku)
				if err != nil || p == nil {
					errCh <- fmt.Errorf("reader %d failed GetProduct(%s): %w", readerID, sku, err)
					return
				}

				if op%10 == 0 {
					_, _, err := s.ListProducts(ctx, "ACTIVE", "", 20, 0)
					if err != nil {
						errCh <- fmt.Errorf("reader %d failed ListProducts: %w", readerID, err)
						return
					}
				}

				if op%15 == 0 {
					_, _, _ = s.ListVersions(ctx, 10, 0)
				}
			}
		}(r)
	}

	// Writers
	wg.Add(writerGoroutines)
	for w := 0; w < writerGoroutines; w++ {
		go func(writerID int) {
			defer wg.Done()
			for op := 0; op < writeOpsPerGoroutine; op++ {
				verNum := 2000 + writerID*100 + op
				verID := fmt.Sprintf("ver-contention-%d-%d", writerID, op)

				txW, err := s.BeginTx(ctx)
				if err != nil {
					errCh <- fmt.Errorf("writer %d failed BeginTx: %w", writerID, err)
					return
				}

				ver := &domain.CatalogVersion{
					VersionID:     verID,
					VersionNumber: verNum,
					Description:   "Contention Stress Test",
					Author:        "stress@coderpush.com",
					Type:          domain.VersionTypePatch,
					CreatedAt:     time.Now().UTC(),
				}

				deltas := []domain.ItemDelta{
					{
						VersionID:    verID,
						SKU:          fmt.Sprintf("SKU-STRESS-%03d", (writerID*10+op)%seedCount),
						Action:       domain.DeltaActionUpdated,
						NewName:      fmt.Sprintf("Contention Update %d-%d", writerID, op),
						NewPrice:     100.0 + float64(op),
						NewStatus:    domain.StatusActive,
						NewTimestamp: time.Now().UTC(),
					},
				}

				if err := s.CreateVersionTx(ctx, txW, ver, deltas); err != nil {
					_ = txW.Rollback()
					errCh <- fmt.Errorf("writer %d failed CreateVersionTx: %w", writerID, err)
					return
				}

				if err := txW.Commit(); err != nil {
					errCh <- fmt.Errorf("writer %d failed Commit: %w", writerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	var recordedErrors []error
	for err := range errCh {
		recordedErrors = append(recordedErrors, err)
	}

	if len(recordedErrors) > 0 {
		t.Fatalf("High contention stress test encountered %d errors (first: %v)", len(recordedErrors), recordedErrors[0])
	}
}

// TestChallenger_O1LookupIndependenceFromDeltaCount empirically confirms that active catalog
// point lookups remain O(1) constant time regardless of whether catalog_version_deltas has
// 0 deltas or 10,000 deltas.
func TestChallenger_O1LookupIndependenceFromDeltaCount(t *testing.T) {
	ctx := context.Background()
	s := setupTestDB(t)

	now := time.Now().UTC()
	const totalProducts = 2000

	tx, _ := s.BeginTx(ctx)
	for i := 0; i < totalProducts; i++ {
		_ = s.UpsertProductTx(ctx, tx, &domain.Product{
			SKU:               fmt.Sprintf("SKU-O1-%05d", i),
			Name:              fmt.Sprintf("Item %d", i),
			Price:             10.0,
			Status:            domain.StatusActive,
			SupplierTimestamp: now,
			UpdatedAt:         now,
		})
	}
	_ = tx.Commit()

	const iterations = 1000
	targetSKU := "SKU-O1-01000"

	// Measure baseline with 0 deltas
	start0 := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = s.GetProduct(ctx, targetSKU)
	}
	latency0 := time.Since(start0) / time.Duration(iterations)

	// Insert 10,000 deltas across 20 versions
	for v := 1; v <= 20; v++ {
		txV, _ := s.BeginTx(ctx)
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-o1-%03d", v),
			VersionNumber: v,
			Description:   "O(1) Verification",
			Author:        "perf@coderpush.com",
			Type:          domain.VersionTypePatch,
			CreatedAt:     now.Add(time.Duration(v) * time.Hour),
		}
		deltas := make([]domain.ItemDelta, 500)
		for d := 0; d < 500; d++ {
			skuIdx := (v*500 + d) % totalProducts
			deltas[d] = domain.ItemDelta{
				VersionID:    ver.VersionID,
				SKU:          fmt.Sprintf("SKU-O1-%05d", skuIdx),
				Action:       domain.DeltaActionUpdated,
				NewName:      "Updated Item",
				NewPrice:     20.0,
				NewStatus:    domain.StatusActive,
				NewTimestamp: now,
			}
		}
		_ = s.CreateVersionTx(ctx, txV, ver, deltas)
		_ = txV.Commit()
	}

	// Measure latency with 10,000 deltas
	start10k := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = s.GetProduct(ctx, targetSKU)
	}
	latency10k := time.Since(start10k) / time.Duration(iterations)

	t.Logf("Point lookup latency with 0 deltas: %v", latency0)
	t.Logf("Point lookup latency with 10,000 deltas: %v", latency10k)

	// Both must be strictly sub-millisecond (< 1.0 ms)
	if latency0 >= 1*time.Millisecond {
		t.Errorf("latency0 exceeded 1ms: %v", latency0)
	}
	if latency10k >= 1*time.Millisecond {
		t.Errorf("latency10k exceeded 1ms: %v", latency10k)
	}

	// Latency with 10k deltas must not degrade by more than 5x baseline
	ratio := float64(latency10k) / float64(latency0)
	t.Logf("Latency ratio (10k deltas / 0 deltas): %.2fx", ratio)
	if ratio > 5.0 && latency10k > 500*time.Microsecond {
		t.Errorf("Suspicious degradation in lookup latency with 10k deltas: ratio=%.2fx, latency10k=%v", ratio, latency10k)
	}
}
