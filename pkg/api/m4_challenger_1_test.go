package api_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"coderpush/pkg/api"
	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// ============================================================================
// Milestone 4 Empirical Challenger 1 Test Suite
//
// Mandate:
// 1. Immutability Stress Attack: Call POST /api/patches/preview repeatedly with
//    mixed valid and invalid batches. Verify bit-for-bit that catalog SHA-256
//    checksum, row counts, and version tables are 100% unchanged before and after.
// 2. 422 Invariant Quarantine Gating Attack: Send adversarial patches to
//    POST /api/patches/publish containing:
//    - Resurrection of withdrawn items (Invariant A)
//    - Stale/out-of-order timestamps (Invariant B)
//    - Negative prices (Invariant C)
//    - Empty SKUs (Invariant E)
//    Assert 100% fail with HTTP 422 Unprocessable Entity and COMMIT_BLOCKED,
//    committing zero rows to storage.
// 3. Chained Rollbacks & Tombstone Preservation: Execute a sequence of versions
//    (V1 -> V2 with withdrawn item -> V3 price spike -> Rollback V1 -> Rollback V2).
//    Empirically verify that live catalog queries (GetProduct, GET /api/products)
//    strictly preserve WITHDRAWN status and never resurrect tombstones.
// ============================================================================

// dbTableFingerprint computes SHA-256 checksum and row count of all relevant database tables.
type dbTableFingerprint struct {
	ProductsChecksum      string
	ProductsCount         int
	VersionsChecksum      string
	VersionsCount         int
	DeltasChecksum        string
	DeltasCount           int
	DeadLettersChecksum   string
	DeadLettersCount      int
	SyncJobsCount         int
	BatchCheckpointsCount int
}

func computeDBFingerprint(t *testing.T, db *sql.DB) dbTableFingerprint {
	t.Helper()
	ctx := context.Background()

	// Products
	pRows, err := db.QueryContext(ctx, "SELECT sku, name, printf('%.8f', price), status, supplier_timestamp, updated_at FROM products ORDER BY sku ASC;")
	if err != nil {
		t.Fatalf("failed querying products: %v", err)
	}
	defer pRows.Close()

	pHash := sha256.New()
	pCount := 0
	for pRows.Next() {
		var s, n, pr, st, su, up string
		if err := pRows.Scan(&s, &n, &pr, &st, &su, &up); err != nil {
			t.Fatalf("scan products: %v", err)
		}
		pHash.Write([]byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s\n", s, n, pr, st, su, up)))
		pCount++
	}

	// Catalog Versions
	vRows, err := db.QueryContext(ctx, "SELECT version_id, version_number, description, author, type, COALESCE(revert_of_version_id, ''), total_items_affected FROM catalog_versions ORDER BY version_number ASC;")
	if err != nil {
		t.Fatalf("failed querying catalog_versions: %v", err)
	}
	defer vRows.Close()

	vHash := sha256.New()
	vCount := 0
	for vRows.Next() {
		var vid, desc, auth, vtype, rev string
		var vnum, items int
		if err := vRows.Scan(&vid, &vnum, &desc, &auth, &vtype, &rev, &items); err != nil {
			t.Fatalf("scan catalog_versions: %v", err)
		}
		vHash.Write([]byte(fmt.Sprintf("%s|%d|%s|%s|%s|%s|%d\n", vid, vnum, desc, auth, vtype, rev, items)))
		vCount++
	}

	// Catalog Version Deltas
	dRows, err := db.QueryContext(ctx, "SELECT delta_id, version_id, sku, action, COALESCE(old_name, ''), new_name, COALESCE(old_price, 0), new_price, COALESCE(old_status, ''), new_status FROM catalog_version_deltas ORDER BY delta_id ASC;")
	if err != nil {
		t.Fatalf("failed querying catalog_version_deltas: %v", err)
	}
	defer dRows.Close()

	dHash := sha256.New()
	dCount := 0
	for dRows.Next() {
		var did int64
		var vid, sku, act, on, nn, os, ns string
		var op, np float64
		if err := dRows.Scan(&did, &vid, &sku, &act, &on, &nn, &op, &np, &os, &ns); err != nil {
			t.Fatalf("scan catalog_version_deltas: %v", err)
		}
		dHash.Write([]byte(fmt.Sprintf("%d|%s|%s|%s|%s|%s|%.4f|%.4f|%s|%s\n", did, vid, sku, act, on, nn, op, np, os, ns)))
		dCount++
	}

	// Dead letters
	dlRows, err := db.QueryContext(ctx, "SELECT issue_id, job_id, sku, reason FROM dead_letter_issues ORDER BY issue_id ASC;")
	if err != nil {
		t.Fatalf("failed querying dead_letter_issues: %v", err)
	}
	defer dlRows.Close()

	dlHash := sha256.New()
	dlCount := 0
	for dlRows.Next() {
		var iid, jid, sku, reason string
		if err := dlRows.Scan(&iid, &jid, &sku, &reason); err != nil {
			t.Fatalf("scan dead_letter_issues: %v", err)
		}
		dlHash.Write([]byte(fmt.Sprintf("%s|%s|%s|%s\n", iid, jid, sku, reason)))
		dlCount++
	}

	// Sync jobs & batch checkpoints counts
	var sjCount, bcCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sync_jobs;").Scan(&sjCount); err != nil {
		t.Fatalf("query sync_jobs count: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM batch_checkpoints;").Scan(&bcCount); err != nil {
		t.Fatalf("query batch_checkpoints count: %v", err)
	}

	return dbTableFingerprint{
		ProductsChecksum:      hex.EncodeToString(pHash.Sum(nil)),
		ProductsCount:         pCount,
		VersionsChecksum:      hex.EncodeToString(vHash.Sum(nil)),
		VersionsCount:         vCount,
		DeltasChecksum:        hex.EncodeToString(dHash.Sum(nil)),
		DeltasCount:           dCount,
		DeadLettersChecksum:   hex.EncodeToString(dlHash.Sum(nil)),
		DeadLettersCount:      dlCount,
		SyncJobsCount:         sjCount,
		BatchCheckpointsCount: bcCount,
	}
}

// -----------------------------------------------------------------------------
// 1. IMMUTABILITY STRESS ATTACK: POST /api/patches/preview
// -----------------------------------------------------------------------------

func TestM4Challenger_ImmutabilityStressAttack(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "immutability_attack.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening sqlite store: %v", err)
	}

	broker := api.NewBroker(api.WithClientCap(64), api.WithPingInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := api.NewProgressHook(broker)
	eng := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	handler := api.NewHandler(store, eng, broker, api.WithHandlerDataDir(tempDir))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	t.Cleanup(func() {
		handler.CancelActiveJob()
		cancel()
		broker.Close()
		_ = store.Close()
	})

	baseTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	// Seed 100 products (80 ACTIVE, 20 WITHDRAWN)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	for i := 1; i <= 100; i++ {
		status := domain.StatusActive
		if i%5 == 0 {
			status = domain.StatusWithdrawn
		}
		p := &domain.Product{
			SKU:               fmt.Sprintf("SKU-SEED-%03d", i),
			Name:              fmt.Sprintf("Seed Product %03d", i),
			Price:             float64(i) * 1.5,
			Status:            status,
			SupplierTimestamp: baseTime.Add(time.Duration(i) * time.Minute),
			UpdatedAt:         baseTime.Add(time.Duration(i) * time.Minute),
		}
		if err := store.UpsertProductTx(ctx, tx, p); err != nil {
			t.Fatalf("seed upsert product %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	// Capture initial baseline fingerprint
	preFingerprint := computeDBFingerprint(t, store.DB())
	if preFingerprint.ProductsCount != 100 {
		t.Fatalf("expected 100 products, got %d", preFingerprint.ProductsCount)
	}

	// Vector 1: 50 completely valid patch preview requests
	t.Run("Vector1_ValidBatches", func(t *testing.T) {
		for i := 1; i <= 50; i++ {
			patch := domain.CatalogPatch{
				PatchID:     fmt.Sprintf("patch-valid-%d", i),
				Description: fmt.Sprintf("Valid Patch %d", i),
				Author:      "Challenger",
				Items: []domain.PatchItem{
					{
						SKU:    fmt.Sprintf("SKU-NEW-%d", i),
						Name:   fmt.Sprintf("Brand New Product %d", i),
						Price:  99.99,
						Status: domain.StatusActive,
					},
					{
						SKU:       "SKU-SEED-001",
						Name:      "Updated Seed 1",
						Price:     105.00,
						Status:    domain.StatusActive,
						Timestamp: func() *time.Time { t := baseTime.Add(100 * time.Hour); return &t }(),
					},
				},
			}

			body, _ := json.Marshal(patch)
			req := httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("iteration %d: expected 200, got %d: %s", i, rec.Code, rec.Body.String())
			}

			var preview domain.PatchPreviewResult
			if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
				t.Fatalf("iteration %d: failed parsing preview: %v", i, err)
			}
			if !preview.CanCommit {
				t.Fatalf("iteration %d: expected CanCommit=true", i)
			}
			if preview.DeadLetterCount != 0 {
				t.Fatalf("iteration %d: expected 0 dead letters, got %d", i, preview.DeadLetterCount)
			}
		}
	})

	// Vector 2: 50 completely invalid patch preview requests (all invariant violations)
	t.Run("Vector2_AdversarialInvalidBatches", func(t *testing.T) {
		for i := 1; i <= 50; i++ {
			patch := domain.CatalogPatch{
				PatchID:     fmt.Sprintf("patch-invalid-%d", i),
				Description: fmt.Sprintf("Adversarial Invalid Patch %d", i),
				Author:      "Attacker",
				Items: []domain.PatchItem{
					// Invariant A: Tombstone Resurrection (SKU-SEED-005 is WITHDRAWN)
					{
						SKU:       "SKU-SEED-005",
						Name:      "Resurrect Withdrawn Item",
						Price:     50.00,
						Status:    domain.StatusActive,
						Timestamp: func() *time.Time { t := baseTime.Add(100 * time.Hour); return &t }(),
					},
					// Invariant B: Stale / backward timestamp
					{
						SKU:       "SKU-SEED-001",
						Name:      "Stale Item",
						Price:     50.00,
						Status:    domain.StatusActive,
						Timestamp: func() *time.Time { t := baseTime.Add(-10 * time.Hour); return &t }(),
					},
					// Invariant C: Negative price
					{
						SKU:    fmt.Sprintf("SKU-NEG-%d", i),
						Name:   "Negative Price Item",
						Price:  -25.50,
						Status: domain.StatusActive,
					},
					// Invariant E: Empty SKU
					{
						SKU:    "   ",
						Name:   "Whitespace SKU Item",
						Price:  10.00,
						Status: domain.StatusActive,
					},
				},
			}

			body, _ := json.Marshal(patch)
			req := httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("iteration %d: expected 200 OK for preview dry-run, got %d", i, rec.Code)
			}

			var preview domain.PatchPreviewResult
			if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
				t.Fatalf("iteration %d: failed parsing preview: %v", i, err)
			}
			if preview.CanCommit {
				t.Fatalf("iteration %d: expected CanCommit=false for adversarial batch", i)
			}
			if preview.DeadLetterCount != 4 {
				t.Fatalf("iteration %d: expected 4 dead letters, got %d", i, preview.DeadLetterCount)
			}
		}
	})

	// Vector 3: 50 mixed batches (50% valid, 50% invalid)
	t.Run("Vector3_MixedBatches", func(t *testing.T) {
		for i := 1; i <= 50; i++ {
			patch := domain.CatalogPatch{
				PatchID:     fmt.Sprintf("patch-mixed-%d", i),
				Description: fmt.Sprintf("Mixed Batch %d", i),
				Author:      "Challenger",
				Items: []domain.PatchItem{
					{
						SKU:    fmt.Sprintf("SKU-MIXED-NEW-%d", i),
						Name:   "Legit New Product",
						Price:  75.00,
						Status: domain.StatusActive,
					},
					{
						SKU:    fmt.Sprintf("SKU-MIXED-BAD-%d", i),
						Name:   "Poisoned Item",
						Price:  -0.01, // Negative price violation
						Status: domain.StatusActive,
					},
				},
			}

			body, _ := json.Marshal(patch)
			req := httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("mixed %d: expected 200, got %d", i, rec.Code)
			}

			var preview domain.PatchPreviewResult
			if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
				t.Fatalf("mixed %d: parse: %v", i, err)
			}
			if preview.CanCommit {
				t.Fatalf("mixed %d: CanCommit should be false due to poisoned item", i)
			}
			if preview.AddedCount != 1 || preview.DeadLetterCount != 1 {
				t.Fatalf("mixed %d: expected added=1, dead=1, got added=%d, dead=%d", i, preview.AddedCount, preview.DeadLetterCount)
			}
		}
	})

	// Vector 4: Concurrent Preview Barrage (20 goroutines hitting preview simultaneously)
	t.Run("Vector4_ConcurrentPreviewBarrage", func(t *testing.T) {
		var wg sync.WaitGroup
		concurrency := 20
		barrier := make(chan struct{})

		for g := 0; g < concurrency; g++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				<-barrier

				for loop := 0; loop < 10; loop++ {
					patch := domain.CatalogPatch{
						PatchID:     fmt.Sprintf("conc-patch-%d-%d", workerID, loop),
						Description: "Concurrent Stress Preview",
						Author:      "Concurrent Worker",
						Items: []domain.PatchItem{
							{
								SKU:    fmt.Sprintf("SKU-CONC-%d-%d", workerID, loop),
								Name:   "Conc Item",
								Price:  float64(workerID*10 + loop),
								Status: domain.StatusActive,
							},
						},
					}

					body, _ := json.Marshal(patch)
					req := httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					rec := httptest.NewRecorder()
					mux.ServeHTTP(rec, req)

					if rec.Code != http.StatusOK {
						t.Errorf("worker %d loop %d failed: %d", workerID, loop, rec.Code)
					}
				}
			}(g)
		}

		close(barrier)
		wg.Wait()
	})

	// Vector 5: Edge cases & malformed requests
	t.Run("Vector5_MalformedPreviewRequests", func(t *testing.T) {
		// Empty body
		req := httptest.NewRequest(http.MethodPost, "/api/patches/preview", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on empty body, got %d", rec.Code)
		}

		// Malformed JSON
		req = httptest.NewRequest(http.MethodPost, "/api/patches/preview", strings.NewReader(`{malformed json}`))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on malformed json, got %d", rec.Code)
		}

		// Empty items list
		patch := domain.CatalogPatch{Description: "No items", Author: "Nobody", Items: []domain.PatchItem{}}
		body, _ := json.Marshal(patch)
		req = httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewReader(body))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on empty items, got %d", rec.Code)
		}
	})

	// FINAL BIT-FOR-BIT ASSERTION ACROSS ALL TABLES
	postFingerprint := computeDBFingerprint(t, store.DB())

	if postFingerprint.ProductsCount != preFingerprint.ProductsCount {
		t.Fatalf("MUTATION DETECTED: products count changed from %d to %d", preFingerprint.ProductsCount, postFingerprint.ProductsCount)
	}
	if postFingerprint.ProductsChecksum != preFingerprint.ProductsChecksum {
		t.Fatalf("MUTATION DETECTED: products SHA-256 changed!\nBefore: %s\nAfter:  %s", preFingerprint.ProductsChecksum, postFingerprint.ProductsChecksum)
	}
	if postFingerprint.VersionsCount != preFingerprint.VersionsCount {
		t.Fatalf("MUTATION DETECTED: versions count changed from %d to %d", preFingerprint.VersionsCount, postFingerprint.VersionsCount)
	}
	if postFingerprint.VersionsChecksum != preFingerprint.VersionsChecksum {
		t.Fatalf("MUTATION DETECTED: versions checksum changed!")
	}
	if postFingerprint.DeltasCount != preFingerprint.DeltasCount {
		t.Fatalf("MUTATION DETECTED: deltas count changed from %d to %d", preFingerprint.DeltasCount, postFingerprint.DeltasCount)
	}
	if postFingerprint.DeltasChecksum != preFingerprint.DeltasChecksum {
		t.Fatalf("MUTATION DETECTED: deltas checksum changed!")
	}
	if postFingerprint.DeadLettersCount != preFingerprint.DeadLettersCount {
		t.Fatalf("MUTATION DETECTED: dead letters count changed from %d to %d", preFingerprint.DeadLettersCount, postFingerprint.DeadLettersCount)
	}
	if postFingerprint.DeadLettersChecksum != preFingerprint.DeadLettersChecksum {
		t.Fatalf("MUTATION DETECTED: dead letters checksum changed!")
	}
	if postFingerprint.SyncJobsCount != preFingerprint.SyncJobsCount {
		t.Fatalf("MUTATION DETECTED: sync_jobs count changed!")
	}
	if postFingerprint.BatchCheckpointsCount != preFingerprint.BatchCheckpointsCount {
		t.Fatalf("MUTATION DETECTED: batch_checkpoints count changed!")
	}
}

// -----------------------------------------------------------------------------
// 2. 422 INVARIANT QUARANTINE GATING ATTACK: POST /api/patches/publish
// -----------------------------------------------------------------------------

func TestM4Challenger_InvariantQuarantineGatingAttack(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "invariant_gating_attack.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening store: %v", err)
	}

	broker := api.NewBroker(api.WithClientCap(64), api.WithPingInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := api.NewProgressHook(broker)
	eng := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	handler := api.NewHandler(store, eng, broker, api.WithHandlerDataDir(tempDir))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	t.Cleanup(func() {
		handler.CancelActiveJob()
		cancel()
		broker.Close()
		_ = store.Close()
	})

	baseTime := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

	// Seed catalog with baseline items
	seedProductHelper(t, store, "SKU-ACTIVE-01", "Active Product", 100.00, domain.StatusActive, baseTime)
	seedProductHelper(t, store, "SKU-TOMBSTONE-01", "Withdrawn Product", 50.00, domain.StatusWithdrawn, baseTime)

	// Capture clean baseline fingerprint
	baselineFingerprint := computeDBFingerprint(t, store.DB())

	assert422CommitBlocked := func(t *testing.T, patch domain.CatalogPatch, attackName string) {
		t.Helper()
		bodyBytes, _ := json.Marshal(patch)
		req := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("[%s] expected HTTP 422 Unprocessable Entity, got HTTP %d: %s", attackName, rec.Code, rec.Body.String())
		}

		var errResp api.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("[%s] failed parsing error response: %v", attackName, err)
		}

		if errResp.Code != api.ErrCodeCommitBlocked {
			t.Fatalf("[%s] expected error code %s, got %s", attackName, api.ErrCodeCommitBlocked, errResp.Code)
		}

		// Verify zero rows were committed to storage
		currentFingerprint := computeDBFingerprint(t, store.DB())
		if currentFingerprint.ProductsChecksum != baselineFingerprint.ProductsChecksum {
			t.Fatalf("[%s] STORAGE MUTATED! Products table checksum changed after rejected patch", attackName)
		}
		if currentFingerprint.VersionsCount != baselineFingerprint.VersionsCount {
			t.Fatalf("[%s] STORAGE MUTATED! Catalog version was created despite 422 rejection", attackName)
		}
		if currentFingerprint.DeltasCount != baselineFingerprint.DeltasCount {
			t.Fatalf("[%s] STORAGE MUTATED! Delta records were committed despite 422 rejection", attackName)
		}
	}

	// Attack 1: Invariant A - Resurrection of withdrawn items
	t.Run("Attack1_TombstoneResurrection", func(t *testing.T) {
		patch := domain.CatalogPatch{
			Description: "Attempt to resurrect tombstone",
			Author:      "Adversary",
			Items: []domain.PatchItem{
				{
					SKU:       "SKU-TOMBSTONE-01",
					Name:      "Resurrected Tombstone",
					Price:     99.00,
					Status:    domain.StatusActive, // Attempted resurrection!
					Timestamp: func() *time.Time { t := baseTime.Add(1 * time.Hour); return &t }(),
				},
			},
		}
		assert422CommitBlocked(t, patch, "Invariant A Tombstone Resurrection")
	})

	// Attack 2: Invariant B - Stale & non-monotonic timestamps
	t.Run("Attack2_StaleTimestamps", func(t *testing.T) {
		// Strictly older timestamp
		olderPatch := domain.CatalogPatch{
			Description: "Attempt stale timestamp update",
			Author:      "Adversary",
			Items: []domain.PatchItem{
				{
					SKU:       "SKU-ACTIVE-01",
					Name:      "Active Product Stale",
					Price:     120.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := baseTime.Add(-10 * time.Minute); return &t }(),
				},
			},
		}
		assert422CommitBlocked(t, olderPatch, "Invariant B Stale Timestamp")

		// Identical timestamp (non-monotonic)
		identicalPatch := domain.CatalogPatch{
			Description: "Attempt identical timestamp update",
			Author:      "Adversary",
			Items: []domain.PatchItem{
				{
					SKU:       "SKU-ACTIVE-01",
					Name:      "Active Product Same Timestamp",
					Price:     125.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := baseTime; return &t }(),
				},
			},
		}
		assert422CommitBlocked(t, identicalPatch, "Invariant B Non-monotonic Timestamp")
	})

	// Attack 3: Invariant C - Negative prices
	t.Run("Attack3_NegativePrices", func(t *testing.T) {
		negPrices := []float64{-100.0, -0.01, -1e-6}
		for i, np := range negPrices {
			patch := domain.CatalogPatch{
				Description: fmt.Sprintf("Attempt negative price %f", np),
				Author:      "Adversary",
				Items: []domain.PatchItem{
					{
						SKU:    fmt.Sprintf("SKU-NEG-%d", i),
						Name:   "Negative Price Product",
						Price:  np,
						Status: domain.StatusActive,
					},
				},
			}
			assert422CommitBlocked(t, patch, fmt.Sprintf("Invariant C Negative Price %f", np))
		}
	})

	// Attack 4: Invariant E - Empty / Whitespace SKUs
	t.Run("Attack4_EmptySKUs", func(t *testing.T) {
		emptySKUs := []string{"", "   ", "\t\n"}
		for i, es := range emptySKUs {
			patch := domain.CatalogPatch{
				Description: fmt.Sprintf("Attempt empty SKU %d", i),
				Author:      "Adversary",
				Items: []domain.PatchItem{
					{
						SKU:    es,
						Name:   "Empty SKU Product",
						Price:  45.00,
						Status: domain.StatusActive,
					},
				},
			}
			assert422CommitBlocked(t, patch, fmt.Sprintf("Invariant E Empty SKU [%s]", es))
		}
	})

	// Attack 5: Poison-pill in a large batch (49 valid items, 1 poisoned item)
	t.Run("Attack5_PoisonPillInLargeBatch", func(t *testing.T) {
		items := make([]domain.PatchItem, 0, 50)
		for i := 1; i <= 49; i++ {
			items = append(items, domain.PatchItem{
				SKU:    fmt.Sprintf("SKU-VALID-BATCH-%02d", i),
				Name:   fmt.Sprintf("Valid Batch Item %02d", i),
				Price:  float64(i) * 10,
				Status: domain.StatusActive,
			})
		}
		// 50th item is poisoned: resurrection of tombstoned item
		items = append(items, domain.PatchItem{
			SKU:       "SKU-TOMBSTONE-01",
			Name:      "Poison Pill Resurrection",
			Price:     999.00,
			Status:    domain.StatusActive,
			Timestamp: func() *time.Time { t := baseTime.Add(2 * time.Hour); return &t }(),
		})

		poisonPatch := domain.CatalogPatch{
			Description: "50-item batch containing single poison pill",
			Author:      "Adversary",
			Items:       items,
		}

		assert422CommitBlocked(t, poisonPatch, "Poison-pill 50-item batch")

		// Verify that NONE of the 49 valid items were inserted into products
		for i := 1; i <= 49; i++ {
			sku := fmt.Sprintf("SKU-VALID-BATCH-%02d", i)
			_, err := store.GetProduct(ctx, sku)
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("atomic rollback failed: item %s was found in database!", sku)
			}
		}
	})
}

// -----------------------------------------------------------------------------
// 3. CHAINED ROLLBACKS & TOMBSTONE PRESERVATION
//    V1 -> V2 (withdrawn) -> V3 (price spike) -> Rollback V1 -> Rollback V2 -> Rollback V1
// -----------------------------------------------------------------------------

func TestM4Challenger_ChainedRollbacksAndTombstones(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "chained_rollbacks.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	broker := api.NewBroker(api.WithClientCap(64), api.WithPingInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := api.NewProgressHook(broker)
	eng := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	handler := api.NewHandler(store, eng, broker, api.WithHandlerDataDir(tempDir))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	t.Cleanup(func() {
		handler.CancelActiveJob()
		cancel()
		broker.Close()
		_ = store.Close()
	})

	baseTime := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)

	// Step 1: Milestone V1 (Publish Initial 3 Products)
	var v1ID string
	{
		patch := domain.CatalogPatch{
			PatchID:     "patch-v1",
			Description: "Milestone V1: Initial Launch",
			Author:      "Admin",
			Items: []domain.PatchItem{
				{
					SKU:       "SKU-A",
					Name:      "Product Alpha",
					Price:     100.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := baseTime.Add(1 * time.Minute); return &t }(),
				},
				{
					SKU:       "SKU-B",
					Name:      "Product Beta",
					Price:     200.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := baseTime.Add(1 * time.Minute); return &t }(),
				},
				{
					SKU:       "SKU-C",
					Name:      "Product Gamma",
					Price:     300.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := baseTime.Add(1 * time.Minute); return &t }(),
				},
			},
		}

		body, _ := json.Marshal(patch)
		req := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("publish V1: expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.PatchPublishResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse V1 response: %v", err)
		}
		v1ID = resp.Version.VersionID
		if resp.Version.VersionNumber != 1 {
			t.Fatalf("expected V1 version_number=1, got %d", resp.Version.VersionNumber)
		}
	}

	// Verify Live Products at V1
	{
		pA, err := store.GetProduct(ctx, "SKU-A")
		if err != nil || pA.Status != domain.StatusActive || pA.Price != 100.00 {
			t.Fatalf("V1 state SKU-A invalid: %+v, err=%v", pA, err)
		}
		pB, err := store.GetProduct(ctx, "SKU-B")
		if err != nil || pB.Status != domain.StatusActive || pB.Price != 200.00 {
			t.Fatalf("V1 state SKU-B invalid: %+v, err=%v", pB, err)
		}
	}

	// Step 2: Milestone V2 (Withdraw Product A, Update Product B)
	var v2ID string
	{
		patch := domain.CatalogPatch{
			PatchID:     "patch-v2",
			Description: "Milestone V2: Discontinue Alpha and Price Update Beta",
			Author:      "Product Manager",
			Items: []domain.PatchItem{
				{
					SKU:       "SKU-A",
					Name:      "Product Alpha (Discontinued)",
					Price:     100.00,
					Status:    domain.StatusWithdrawn, // WITHDRAWN TOMBSTONE!
					Timestamp: func() *time.Time { t := baseTime.Add(2 * time.Minute); return &t }(),
				},
				{
					SKU:       "SKU-B",
					Name:      "Product Beta Upgraded",
					Price:     250.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := baseTime.Add(2 * time.Minute); return &t }(),
				},
			},
		}

		body, _ := json.Marshal(patch)
		req := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("publish V2: expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.PatchPublishResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse V2 response: %v", err)
		}
		v2ID = resp.Version.VersionID
		if resp.Version.VersionNumber != 2 {
			t.Fatalf("expected V2 version_number=2, got %d", resp.Version.VersionNumber)
		}
	}

	// Verify Live Products at V2
	{
		pA, err := store.GetProduct(ctx, "SKU-A")
		if err != nil || pA.Status != domain.StatusWithdrawn {
			t.Fatalf("V2 state SKU-A should be WITHDRAWN: %+v, err=%v", pA, err)
		}
		pB, err := store.GetProduct(ctx, "SKU-B")
		if err != nil || pB.Status != domain.StatusActive || pB.Price != 250.00 {
			t.Fatalf("V2 state SKU-B invalid: %+v, err=%v", pB, err)
		}
	}

	// Step 3: Milestone V3 (Severe Price Spike on Beta + Add Product Delta)
	var v3ID string
	{
		patch := domain.CatalogPatch{
			PatchID:     "patch-v3",
			Description: "Milestone V3: Buggy Price Spike & Added Delta",
			Author:      "Erroneous Script",
			Items: []domain.PatchItem{
				{
					SKU:       "SKU-B",
					Name:      "Product Beta Spike",
					Price:     9999.00, // Spike from 250.00 to 9999.00
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := baseTime.Add(3 * time.Minute); return &t }(),
				},
				{
					SKU:       "SKU-D",
					Name:      "Product Delta",
					Price:     400.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := baseTime.Add(3 * time.Minute); return &t }(),
				},
			},
		}

		body, _ := json.Marshal(patch)
		req := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("publish V3: expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.PatchPublishResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse V3 response: %v", err)
		}
		v3ID = resp.Version.VersionID
		if resp.Version.VersionNumber != 3 {
			t.Fatalf("expected V3 version_number=3, got %d", resp.Version.VersionNumber)
		}
	}

	// Verify Live Products at V3
	{
		pB, _ := store.GetProduct(ctx, "SKU-B")
		if pB.Price != 9999.00 {
			t.Fatalf("V3 state SKU-B should be 9999.00, got %f", pB.Price)
		}
		pD, _ := store.GetProduct(ctx, "SKU-D")
		if pD.Status != domain.StatusActive || pD.Price != 400.00 {
			t.Fatalf("V3 state SKU-D invalid: %+v", pD)
		}
		pA, _ := store.GetProduct(ctx, "SKU-A")
		if pA.Status != domain.StatusWithdrawn {
			t.Fatalf("V3 state SKU-A should remain WITHDRAWN: %+v", pA)
		}
	}

	// -------------------------------------------------------------------------
	// Step 4: FIRST ROLLBACK -> Revert to V1
	// Target: V1 (where SKU-A was ACTIVE, SKU-B was 200.00, SKU-D did not exist)
	// INVARIANT A REQUIREMENT:
	// Live catalog currently has SKU-A as WITHDRAWN.
	// Reverting to V1 MUST NOT resurrect SKU-A! Status must remain WITHDRAWN!
	// -------------------------------------------------------------------------
	var v4ID string
	{
		rbReq := api.RollbackRequest{
			Author:      "Staff Operator",
			Description: "Reverting buggy price spike to V1 baseline",
		}
		body, _ := json.Marshal(rbReq)
		url := fmt.Sprintf("/api/versions/%s/rollback", v1ID)
		req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("rollback to V1: expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.RollbackResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse rollback V1 response: %v", err)
		}
		v4ID = resp.Version.VersionID
		if resp.Version.VersionNumber != 4 {
			t.Fatalf("expected rollback version_number=4, got %d", resp.Version.VersionNumber)
		}
		if resp.Version.Type != domain.VersionTypeRollback {
			t.Fatalf("expected version type ROLLBACK, got %s", resp.Version.Type)
		}
		if resp.Version.RevertOfVersionID == nil || *resp.Version.RevertOfVersionID != v1ID {
			t.Fatalf("expected revert_of_version_id=%s, got %v", v1ID, resp.Version.RevertOfVersionID)
		}
	}

	// EMPIRICAL VERIFICATION AFTER ROLLBACK TO V1
	{
		// 1. SKU-B price reverted to V1 price (200.00), Status ACTIVE
		pB, err := store.GetProduct(ctx, "SKU-B")
		if err != nil || pB.Price != 200.00 || pB.Status != domain.StatusActive {
			t.Fatalf("Post-Rollback V1: SKU-B should be 200.00 ACTIVE, got: %+v, err=%v", pB, err)
		}

		// 2. SKU-C was untouched (300.00 ACTIVE)
		pC, err := store.GetProduct(ctx, "SKU-C")
		if err != nil || pC.Price != 300.00 || pC.Status != domain.StatusActive {
			t.Fatalf("Post-Rollback V1: SKU-C should be 300.00 ACTIVE, got: %+v, err=%v", pC, err)
		}

		// 3. SKU-D (added in V3 after V1) transitioned to WITHDRAWN
		pD, err := store.GetProduct(ctx, "SKU-D")
		if err != nil || pD.Status != domain.StatusWithdrawn {
			t.Fatalf("Post-Rollback V1: SKU-D (post-baseline) should be WITHDRAWN, got: %+v, err=%v", pD, err)
		}

		// 4. CRITICAL INVARIANT A: SKU-A was ACTIVE in V1 baseline, but is WITHDRAWN in live catalog.
		// It MUST NOT BE RESURRECTED!
		pA, err := store.GetProduct(ctx, "SKU-A")
		if err != nil {
			t.Fatalf("failed reading SKU-A: %v", err)
		}
		if pA.Status != domain.StatusWithdrawn {
			t.Fatalf("INVARIANT A VIOLATION! SKU-A was resurrected to %s during rollback to V1!", pA.Status)
		}
		if pA.Price != 100.00 {
			t.Fatalf("SKU-A price should revert to V1 price 100.00, got %f", pA.Price)
		}

		// Verify via HTTP GET /api/products?status=ACTIVE that SKU-A is NOT returned
		{
			req := httptest.NewRequest(http.MethodGet, "/api/products?status=ACTIVE", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /api/products?status=ACTIVE: %d", rec.Code)
			}
			var list api.ProductListResponse
			json.Unmarshal(rec.Body.Bytes(), &list)
			for _, item := range list.Products {
				if item.SKU == "SKU-A" {
					t.Fatalf("INVARIANT A VIOLATION: SKU-A returned in ACTIVE products query!")
				}
				if item.SKU == "SKU-D" {
					t.Fatalf("INVARIANT A VIOLATION: SKU-D returned in ACTIVE products query!")
				}
			}
		}

		// Verify via HTTP GET /api/products?status=WITHDRAWN that SKU-A is present
		{
			req := httptest.NewRequest(http.MethodGet, "/api/products?status=WITHDRAWN", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /api/products?status=WITHDRAWN: %d", rec.Code)
			}
			var list api.ProductListResponse
			json.Unmarshal(rec.Body.Bytes(), &list)
			foundA := false
			foundD := false
			for _, item := range list.Products {
				if item.SKU == "SKU-A" {
					foundA = true
				}
				if item.SKU == "SKU-D" {
					foundD = true
				}
			}
			if !foundA {
				t.Fatalf("SKU-A missing from WITHDRAWN products query!")
			}
			if !foundD {
				t.Fatalf("SKU-D missing from WITHDRAWN products query!")
			}
		}
	}

	// -------------------------------------------------------------------------
	// Step 5: SECOND ROLLBACK IN CHAIN -> Revert to V2
	// Target: V2 (where SKU-A was WITHDRAWN, SKU-B was 250.00, SKU-C was 300.00)
	// -------------------------------------------------------------------------
	var v5ID string
	{
		rbReq := api.RollbackRequest{
			Author:      "Staff Operator",
			Description: "Rolling forward/back to V2 baseline",
		}
		body, _ := json.Marshal(rbReq)
		url := fmt.Sprintf("/api/versions/%s/rollback", v2ID)
		req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("rollback to V2: expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.RollbackResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse rollback V2 response: %v", err)
		}
		v5ID = resp.Version.VersionID
		if resp.Version.VersionNumber != 5 {
			t.Fatalf("expected rollback version_number=5, got %d", resp.Version.VersionNumber)
		}
		if resp.Version.Type != domain.VersionTypeRollback {
			t.Fatalf("expected version type ROLLBACK, got %s", resp.Version.Type)
		}
		if resp.Version.RevertOfVersionID == nil || *resp.Version.RevertOfVersionID != v2ID {
			t.Fatalf("expected revert_of_version_id=%s, got %v", v2ID, resp.Version.RevertOfVersionID)
		}
	}

	// EMPIRICAL VERIFICATION AFTER ROLLBACK TO V2
	{
		// 1. SKU-B price updated to V2 price (250.00), Status ACTIVE
		pB, err := store.GetProduct(ctx, "SKU-B")
		if err != nil || pB.Price != 250.00 || pB.Status != domain.StatusActive {
			t.Fatalf("Post-Rollback V2: SKU-B should be 250.00 ACTIVE, got: %+v, err=%v", pB, err)
		}

		// 2. SKU-A remains WITHDRAWN
		pA, err := store.GetProduct(ctx, "SKU-A")
		if err != nil || pA.Status != domain.StatusWithdrawn {
			t.Fatalf("Post-Rollback V2: SKU-A should remain WITHDRAWN, got: %+v, err=%v", pA, err)
		}

		// 3. SKU-D (added in V3 after V2) remains WITHDRAWN
		pD, err := store.GetProduct(ctx, "SKU-D")
		if err != nil || pD.Status != domain.StatusWithdrawn {
			t.Fatalf("Post-Rollback V2: SKU-D should remain WITHDRAWN, got: %+v, err=%v", pD, err)
		}

		// 4. SKU-C remains ACTIVE at 300.00
		pC, err := store.GetProduct(ctx, "SKU-C")
		if err != nil || pC.Price != 300.00 || pC.Status != domain.StatusActive {
			t.Fatalf("Post-Rollback V2: SKU-C should remain 300.00 ACTIVE, got: %+v, err=%v", pC, err)
		}
	}

	// -------------------------------------------------------------------------
	// Step 6: THIRD ROLLBACK IN CHAIN -> Revert to V1 AGAIN (V5 -> V1)
	// -------------------------------------------------------------------------
	var v6ID string
	{
		rbReq := api.RollbackRequest{
			Author:      "Staff Operator",
			Description: "Rolling back to V1 again from V5",
		}
		body, _ := json.Marshal(rbReq)
		url := fmt.Sprintf("/api/versions/%s/rollback", v1ID)
		req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("rollback to V1 (again): expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp api.RollbackResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse rollback V1 (again): %v", err)
		}
		v6ID = resp.Version.VersionID
		if resp.Version.VersionNumber != 6 {
			t.Fatalf("expected version_number=6, got %d", resp.Version.VersionNumber)
		}
	}

	// Final Tombstone Preservation Check
	{
		pA, _ := store.GetProduct(ctx, "SKU-A")
		if pA.Status != domain.StatusWithdrawn {
			t.Fatalf("CRITICAL TOMBSTONE FAILURE: SKU-A resurrected after repeated rollback to V1!")
		}
		pB, _ := store.GetProduct(ctx, "SKU-B")
		if pB.Price != 200.00 {
			t.Fatalf("SKU-B price should be 200.00, got %f", pB.Price)
		}
		pD, _ := store.GetProduct(ctx, "SKU-D")
		if pD.Status != domain.StatusWithdrawn {
			t.Fatalf("SKU-D must remain WITHDRAWN!")
		}
	}

	// -------------------------------------------------------------------------
	// Step 7: AUDIT VERSION HISTORY & DELTA INSPECTION
	// -------------------------------------------------------------------------
	t.Run("AuditVersionHistoryAndDiffs", func(t *testing.T) {
		// 1. GET /api/versions (Verify all 6 versions exist, ordered newest first)
		req := httptest.NewRequest(http.MethodGet, "/api/versions?limit=10", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/versions: %d", rec.Code)
		}

		var vList api.VersionListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &vList); err != nil {
			t.Fatalf("parse versions list: %v", err)
		}

		if vList.Total != 6 {
			t.Fatalf("expected 6 total versions, got %d", vList.Total)
		}
		if len(vList.Versions) != 6 {
			t.Fatalf("expected 6 versions returned, got %d", len(vList.Versions))
		}

		// Descending order assertion
		expectedNums := []int{6, 5, 4, 3, 2, 1}
		for i, v := range vList.Versions {
			if v.VersionNumber != expectedNums[i] {
				t.Fatalf("index %d: expected version_number=%d, got %d", i, expectedNums[i], v.VersionNumber)
			}
		}

		// Pagination test
		req = httptest.NewRequest(http.MethodGet, "/api/versions?limit=2&offset=2", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/versions paginated: %d", rec.Code)
		}
		var pList api.VersionListResponse
		json.Unmarshal(rec.Body.Bytes(), &pList)
		if len(pList.Versions) != 2 || pList.Versions[0].VersionNumber != 4 || pList.Versions[1].VersionNumber != 3 {
			t.Fatalf("unexpected paginated versions: %+v", pList.Versions)
		}

		// 2. GET /api/versions/{id}/diff for V1
		req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/versions/%s/diff", v1ID), nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET diff V1: %d", rec.Code)
		}
		var deltasV1 []domain.ItemDelta
		if err := json.Unmarshal(rec.Body.Bytes(), &deltasV1); err != nil {
			t.Fatalf("parse deltas V1: %v", err)
		}
		if len(deltasV1) != 3 {
			t.Fatalf("expected 3 deltas for V1, got %d", len(deltasV1))
		}

		// 3. GET /api/versions/{id}/diff for V4 (Rollback to V1)
		req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/versions/%s/diff", v4ID), nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET diff V4: %d", rec.Code)
		}
		var deltasV4 []domain.ItemDelta
		if err := json.Unmarshal(rec.Body.Bytes(), &deltasV4); err != nil {
			t.Fatalf("parse deltas V4: %v", err)
		}
		if len(deltasV4) == 0 {
			t.Fatalf("expected inverse deltas for V4 rollback, got 0")
		}

		// 4. GET /api/versions/non-existent-id/diff -> 404
		req = httptest.NewRequest(http.MethodGet, "/api/versions/ver-missing-12345/diff", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 on missing version diff, got %d", rec.Code)
		}

		// 5. POST /api/versions/non-existent-id/rollback -> 404
		req = httptest.NewRequest(http.MethodPost, "/api/versions/ver-missing-12345/rollback", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 on missing version rollback, got %d", rec.Code)
		}
	})

	_ = v3ID
	_ = v5ID
	_ = v6ID
}

// -----------------------------------------------------------------------------
// 4. SSE REAL-TIME NOTIFICATIONS: PUBLISH & ROLLBACK STREAMING
// -----------------------------------------------------------------------------

func TestM4Challenger_SSENotifications(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "sse_notifications.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	broker := api.NewBroker(api.WithClientCap(64), api.WithPingInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := api.NewProgressHook(broker)
	eng := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	handler := api.NewHandler(store, eng, broker, api.WithHandlerDataDir(tempDir))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	t.Cleanup(func() {
		handler.CancelActiveJob()
		cancel()
		broker.Close()
		server.Close()
		_ = store.Close()
	})

	// Connect SSE Client
	sseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("create sse req: %v", err)
	}
	sseReq.Header.Set("Accept", "text/event-stream")

	sseClient := &http.Client{Timeout: 0}
	sseResp, err := sseClient.Do(sseReq)
	if err != nil {
		t.Fatalf("connect to sse: %v", err)
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for SSE, got %d", sseResp.StatusCode)
	}

	reader := bufio.NewReader(sseResp.Body)
	eventsChan := make(chan string, 100)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			eventsChan <- strings.TrimRight(line, "\r\n")
		}
	}()

	readNextEvent := func(timeout time.Duration) (string, string) {
		var eventType, eventData string
		deadline := time.After(timeout)
		for {
			select {
			case line := <-eventsChan:
				if strings.HasPrefix(line, "event: ") {
					eventType = strings.TrimPrefix(line, "event: ")
				} else if strings.HasPrefix(line, "data: ") {
					eventData = strings.TrimPrefix(line, "data: ")
				} else if line == "" && eventType != "" {
					return eventType, eventData
				}
			case <-deadline:
				return eventType, eventData
			}
		}
	}

	// Drain initial connected event or pings
	time.Sleep(100 * time.Millisecond)
	for len(eventsChan) > 0 {
		<-eventsChan
	}

	// 1. Trigger Publish -> Assert version_created event
	patch := domain.CatalogPatch{
		Description: "SSE Test Version",
		Author:      "SSE Tester",
		Items: []domain.PatchItem{
			{
				SKU:    "SKU-SSE-01",
				Name:   "SSE Product",
				Price:  88.88,
				Status: domain.StatusActive,
			},
		},
	}
	body, _ := json.Marshal(patch)
	pubReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/patches/publish", bytes.NewReader(body))
	pubReq.Header.Set("Content-Type", "application/json")
	pubResp, err := http.DefaultClient.Do(pubReq)
	if err != nil || pubResp.StatusCode != http.StatusCreated {
		t.Fatalf("publish patch: status=%d, err=%v", pubResp.StatusCode, err)
	}
	var pubResult api.PatchPublishResponse
	json.NewDecoder(pubResp.Body).Decode(&pubResult)

	evType, evData := readNextEvent(2 * time.Second)
	if evType != "version_created" {
		t.Fatalf("expected event 'version_created', got '%s'", evType)
	}
	var payload api.VersionCreatedPayload
	if err := json.Unmarshal([]byte(evData), &payload); err != nil {
		t.Fatalf("parse version_created payload: %v", err)
	}
	if payload.VersionID != pubResult.Version.VersionID {
		t.Fatalf("expected version_id %s, got %s", pubResult.Version.VersionID, payload.VersionID)
	}
	if payload.VersionNumber != 1 {
		t.Fatalf("expected version_number 1, got %d", payload.VersionNumber)
	}

	// 2. Trigger Rollback -> Assert version_rollback event
	rbURL := fmt.Sprintf("%s/api/versions/%s/rollback", server.URL, pubResult.Version.VersionID)
	rbReq, _ := http.NewRequest(http.MethodPost, rbURL, strings.NewReader(`{"author":"SSE Tester","description":"Test Rollback"}`))
	rbReq.Header.Set("Content-Type", "application/json")
	rbResp, err := http.DefaultClient.Do(rbReq)
	if err != nil || rbResp.StatusCode != http.StatusOK {
		t.Fatalf("rollback: status=%d, err=%v", rbResp.StatusCode, err)
	}
	var rbResult api.RollbackResponse
	json.NewDecoder(rbResp.Body).Decode(&rbResult)

	evType, evData = readNextEvent(2 * time.Second)
	if evType != "version_rollback" {
		t.Fatalf("expected event 'version_rollback', got '%s'", evType)
	}
	var rbPayload api.VersionRollbackPayload
	if err := json.Unmarshal([]byte(evData), &rbPayload); err != nil {
		t.Fatalf("parse version_rollback payload: %v", err)
	}
	if rbPayload.VersionID != rbResult.Version.VersionID {
		t.Fatalf("expected rollback version_id %s, got %s", rbResult.Version.VersionID, rbPayload.VersionID)
	}
	if rbPayload.RevertOfVersionID != pubResult.Version.VersionID {
		t.Fatalf("expected revert_of_version_id %s, got %s", pubResult.Version.VersionID, rbPayload.RevertOfVersionID)
	}
}

// -----------------------------------------------------------------------------
// 5. MULTI-TENANT VERSION & ROLLBACK ISOLATION
// -----------------------------------------------------------------------------

func TestM4Challenger_TenantVersionIsolation(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.Open(filepath.Join(tempDir, "default.db"))
	broker := api.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := api.NewProgressHook(broker)
	eng := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	handler := api.NewHandler(store, eng, broker, api.WithHandlerDataDir(tempDir))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	t.Cleanup(func() {
		handler.CancelActiveJob()
		cancel()
		broker.Close()
		_ = store.Close()
	})

	// 1. Publish patch in Shop A
	patchA := domain.CatalogPatch{
		Description: "Shop A Launch",
		Author:      "Alice",
		Items: []domain.PatchItem{
			{SKU: "SKU-SHOP-A", Name: "Shop A Item", Price: 50.00, Status: domain.StatusActive},
		},
	}
	bodyA, _ := json.Marshal(patchA)
	reqA := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(bodyA))
	reqA.Header.Set("X-Shop-Id", "shop-alpha")
	reqA.Header.Set("Content-Type", "application/json")
	recA := httptest.NewRecorder()
	mux.ServeHTTP(recA, reqA)

	if recA.Code != http.StatusCreated {
		t.Fatalf("shop-alpha publish: expected 201, got %d", recA.Code)
	}
	var respA api.PatchPublishResponse
	json.Unmarshal(recA.Body.Bytes(), &respA)

	// 2. Query versions in Shop B -> Must be completely empty
	reqB := httptest.NewRequest(http.MethodGet, "/api/versions", nil)
	reqB.Header.Set("X-Shop-Id", "shop-beta")
	recB := httptest.NewRecorder()
	mux.ServeHTTP(recB, reqB)

	if recB.Code != http.StatusOK {
		t.Fatalf("shop-beta versions: expected 200, got %d", recB.Code)
	}
	var listB api.VersionListResponse
	json.Unmarshal(recB.Body.Bytes(), &listB)
	if listB.Total != 0 || len(listB.Versions) != 0 {
		t.Fatalf("TENANT LEAK: shop-beta sees %d versions from shop-alpha!", listB.Total)
	}

	// 3. Attempt rollback in Shop B referencing Shop A version ID -> Must return 404
	reqCrossRB := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/versions/%s/rollback", respA.Version.VersionID), nil)
	reqCrossRB.Header.Set("X-Shop-Id", "shop-beta")
	recCrossRB := httptest.NewRecorder()
	mux.ServeHTTP(recCrossRB, reqCrossRB)

	if recCrossRB.Code != http.StatusNotFound {
		t.Fatalf("TENANT VIOLATION: expected 404 for cross-tenant rollback, got %d", recCrossRB.Code)
	}
}

// -----------------------------------------------------------------------------
// 6. 20-GOROUTINE CONCURRENT PUBLISH, ROLLBACK, PREVIEW & READ RACES
// -----------------------------------------------------------------------------

func TestM4Challenger_ConcurrentPublishAndRollbackRaces(t *testing.T) {
	tempDir := t.TempDir()
	store, err := storage.Open(filepath.Join(tempDir, "race_attack.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	broker := api.NewBroker(api.WithClientCap(128), api.WithPingInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := api.NewProgressHook(broker)
	eng := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	handler := api.NewHandler(store, eng, broker, api.WithHandlerDataDir(tempDir))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	t.Cleanup(func() {
		handler.CancelActiveJob()
		cancel()
		broker.Close()
		_ = store.Close()
	})

	baseTime := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	// Seed: SKU-TOMB-01 is WITHDRAWN. SKU-ACTIVE-02..05 are ACTIVE.
	seedProductHelper(t, store, "SKU-TOMB-01", "Permanently Withdrawn", 10.0, domain.StatusWithdrawn, baseTime)
	for i := 2; i <= 5; i++ {
		seedProductHelper(t, store, fmt.Sprintf("SKU-ACTIVE-%02d", i), fmt.Sprintf("Active %d", i), float64(i)*10.0, domain.StatusActive, baseTime)
	}

	// Create baseline V1
	v1Patch := domain.CatalogPatch{
		Description: "Baseline V1",
		Author:      "Admin",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-ACTIVE-02",
				Name:      "Active 2 V1",
				Price:     25.0,
				Status:    domain.StatusActive,
				Timestamp: func() *time.Time { t := baseTime.Add(1 * time.Minute); return &t }(),
			},
		},
	}
	body, _ := json.Marshal(v1Patch)
	req := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish baseline V1 failed: %d", rec.Code)
	}
	var v1Resp api.PatchPublishResponse
	json.Unmarshal(rec.Body.Bytes(), &v1Resp)
	v1ID := v1Resp.Version.VersionID

	var wg sync.WaitGroup
	concurrency := 20
	barrier := make(chan struct{})

	for g := 0; g < concurrency; g++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-barrier

			switch workerID % 4 {
			case 0: // Publisher
				for loop := 0; loop < 5; loop++ {
					p := domain.CatalogPatch{
						Description: fmt.Sprintf("Concurrent publish W%d-L%d", workerID, loop),
						Author:      fmt.Sprintf("Worker-%d", workerID),
						Items: []domain.PatchItem{
							{
								SKU:       fmt.Sprintf("SKU-ACTIVE-%02d", 2+(loop%4)),
								Name:      fmt.Sprintf("Active Updated %d", loop),
								Price:     float64(workerID*10 + loop),
								Status:    domain.StatusActive,
								Timestamp: func() *time.Time { t := baseTime.Add(time.Duration(10+workerID*10+loop) * time.Minute); return &t }(),
							},
						},
					}
					b, _ := json.Marshal(p)
					r := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(b))
					r.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, r)
					// May succeed (201), be quarantined on out-of-order timestamp (422), or lock (500)
					if w.Code != http.StatusCreated && w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusInternalServerError {
						t.Errorf("publisher got unexpected code %d: %s", w.Code, w.Body.String())
					}
				}

			case 1: // Previewer (with both valid & invalid adversarial items)
				for loop := 0; loop < 10; loop++ {
					p := domain.CatalogPatch{
						Description: "Concurrent Preview",
						Author:      "Previewer",
						Items: []domain.PatchItem{
							// Adversarial tombstone resurrection attempt
							{
								SKU:       "SKU-TOMB-01",
								Name:      "Resurrect Attempt",
								Price:     99.0,
								Status:    domain.StatusActive,
								Timestamp: func() *time.Time { t := baseTime.Add(24 * time.Hour); return &t }(),
							},
						},
					}
					b, _ := json.Marshal(p)
					r := httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewReader(b))
					r.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, r)
					if w.Code != http.StatusOK {
						t.Errorf("previewer got unexpected code %d", w.Code)
					}
				}

			case 2: // Rollbacker
				for loop := 0; loop < 5; loop++ {
					rb := api.RollbackRequest{
						Author:      fmt.Sprintf("RollbackWorker-%d", workerID),
						Description: fmt.Sprintf("Rollback loop %d", loop),
					}
					b, _ := json.Marshal(rb)
					r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/versions/%s/rollback", v1ID), bytes.NewReader(b))
					r.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, r)
					if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
						t.Errorf("rollbacker got unexpected code %d", w.Code)
					}
				}

			case 3: // Reader (Products, Versions, Diffs)
				for loop := 0; loop < 10; loop++ {
					// Products
					r1 := httptest.NewRequest(http.MethodGet, "/api/products?status=ACTIVE", nil)
					w1 := httptest.NewRecorder()
					mux.ServeHTTP(w1, r1)
					if w1.Code != http.StatusOK {
						t.Errorf("reader products failed: %d", w1.Code)
					}

					// Versions
					r2 := httptest.NewRequest(http.MethodGet, "/api/versions?limit=5", nil)
					w2 := httptest.NewRecorder()
					mux.ServeHTTP(w2, r2)
					if w2.Code != http.StatusOK {
						t.Errorf("reader versions failed: %d", w2.Code)
					}

					// Diff
					r3 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/versions/%s/diff", v1ID), nil)
					w3 := httptest.NewRecorder()
					mux.ServeHTTP(w3, r3)
					if w3.Code != http.StatusOK {
						t.Errorf("reader diff failed: %d", w3.Code)
					}
				}
			}
		}(g)
	}

	close(barrier)
	wg.Wait()

	// Post-Race Empirical Integrity Verification
	t.Run("VerifyPostRaceIntegrity", func(t *testing.T) {
		// 1. Invariant A: SKU-TOMB-01 MUST REMAIN WITHDRAWN
		pTomb, err := store.GetProduct(ctx, "SKU-TOMB-01")
		if err != nil {
			t.Fatalf("failed reading SKU-TOMB-01: %v", err)
		}
		if pTomb.Status != domain.StatusWithdrawn {
			t.Fatalf("CONCURRENCY INVARIANT A VIOLATION! SKU-TOMB-01 status became %s", pTomb.Status)
		}

		// 2. Active products list must NEVER include SKU-TOMB-01
		req := httptest.NewRequest(http.MethodGet, "/api/products?status=ACTIVE", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/products?status=ACTIVE failed: %d", rec.Code)
		}
		var list api.ProductListResponse
		json.Unmarshal(rec.Body.Bytes(), &list)
		for _, item := range list.Products {
			if item.SKU == "SKU-TOMB-01" {
				t.Fatalf("CONCURRENCY INVARIANT A VIOLATION! SKU-TOMB-01 found in ACTIVE products!")
			}
		}

		// 3. Versions table integrity
		vReq := httptest.NewRequest(http.MethodGet, "/api/versions?limit=100", nil)
		vRec := httptest.NewRecorder()
		mux.ServeHTTP(vRec, vReq)
		if vRec.Code != http.StatusOK {
			t.Fatalf("GET /api/versions: %d", vRec.Code)
		}
		var vList api.VersionListResponse
		json.Unmarshal(vRec.Body.Bytes(), &vList)
		if vList.Total < 1 {
			t.Fatalf("expected at least 1 version, got %d", vList.Total)
		}

		// Check strictly descending order
		prevNum := int(^uint(0) >> 1) // max int
		for _, v := range vList.Versions {
			if v.VersionNumber >= prevNum {
				t.Fatalf("version numbers not strictly descending: prev=%d, curr=%d", prevNum, v.VersionNumber)
			}
			prevNum = v.VersionNumber
		}
	})
}
