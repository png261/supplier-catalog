package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"coderpush/pkg/api"
	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// setupM4TestEnv creates an isolated test environment with SQLite storage, broker, engine, and handler.
func setupM4TestEnv(t *testing.T) (*storage.SQLiteStorage, *api.Broker, http.Handler, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "m4_catalog_test.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite database: %v", err)
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

	return store, broker, mux, tempDir
}

// seedProductHelper seeds an initial product directly into storage.
func seedProductHelper(t *testing.T, store storage.Storage, sku, name string, price float64, status domain.ProductStatus, ts time.Time) {
	t.Helper()
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx for seed: %v", err)
	}
	defer tx.Rollback()

	prod := &domain.Product{
		SKU:               sku,
		Name:              name,
		Price:             price,
		Status:            status,
		SupplierTimestamp: ts,
		UpdatedAt:         ts,
	}
	if err := store.UpsertProductTx(ctx, tx, prod); err != nil {
		t.Fatalf("failed to seed product %s: %v", sku, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit seed product: %v", err)
	}
}

// createVersionHelper persists a catalog version within a transaction.
func createVersionHelper(t *testing.T, store storage.Storage, ver *domain.CatalogVersion, deltas []domain.ItemDelta) {
	t.Helper()
	ctx := context.Background()
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx for version creation: %v", err)
	}
	defer tx.Rollback()

	if err := store.CreateVersionTx(ctx, tx, ver, deltas); err != nil {
		t.Fatalf("failed to create version %s: %v", ver.VersionID, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit version %s: %v", ver.VersionID, err)
	}
}

// -----------------------------------------------------------------------------
// Test 1: POST /api/patches/preview
// -----------------------------------------------------------------------------

func TestAPI_Patches_Preview(t *testing.T) {
	store, _, mux, _ := setupM4TestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed catalog with baseline items
	seedProductHelper(t, store, "SKU-PREV-01", "Keyboard Classic", 40.00, domain.StatusActive, now.Add(-10*time.Minute))
	seedProductHelper(t, store, "SKU-PREV-02", "Discontinued Cable", 15.00, domain.StatusWithdrawn, now.Add(-10*time.Minute))

	// Snapshot catalog checksum and row count before preview
	preProducts, preTotal, err := store.ListProducts(ctx, "", "", 100, 0)
	if err != nil {
		t.Fatalf("failed to list initial products: %v", err)
	}
	preChecksum := catalogSync.CalculateCatalogChecksum(preProducts)

	t.Run("ValidPreview_AddedAndUpdated_ImmutabilityPreserved", func(t *testing.T) {
		patchPayload := domain.CatalogPatch{
			PatchID:     "patch-preview-valid",
			Description: "Add mouse and update keyboard",
			Author:      "Alice Tester",
			Items: []domain.PatchItem{
				{
					SKU:    "SKU-PREV-NEW",
					Name:   "Gaming Mouse",
					Price:  60.00,
					Status: domain.StatusActive,
				},
				{
					SKU:       "SKU-PREV-01",
					Name:      "Keyboard Classic RGB",
					Price:     55.00,
					Status:    domain.StatusActive,
					Timestamp: func() *time.Time { t := now; return &t }(),
				},
			},
		}

		bodyBytes, _ := json.Marshal(patchPayload)
		req := httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp domain.PatchPreviewResult
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode preview response: %v", err)
		}

		if !resp.CanCommit {
			t.Errorf("expected CanCommit == true, got false")
		}
		if resp.DeadLetterCount != 0 {
			t.Errorf("expected 0 dead letters, got %d", resp.DeadLetterCount)
		}
		if resp.AddedCount != 1 {
			t.Errorf("expected 1 added, got %d", resp.AddedCount)
		}
		if resp.UpdatedCount != 1 {
			t.Errorf("expected 1 updated, got %d", resp.UpdatedCount)
		}
		if resp.CatalogChecksum == "" {
			t.Error("expected non-empty catalog checksum")
		}
		if len(resp.Deltas) != 2 {
			t.Fatalf("expected 2 deltas, got %d", len(resp.Deltas))
		}

		// Verify Immutability (Zero Database Writes)
		postProducts, postTotal, _ := store.ListProducts(ctx, "", "", 100, 0)
		postChecksum := catalogSync.CalculateCatalogChecksum(postProducts)
		if preChecksum != postChecksum {
			t.Fatalf("IMMUTABILITY VIOLATION: catalog checksum changed from %s to %s", preChecksum, postChecksum)
		}
		if preTotal != postTotal {
			t.Fatalf("IMMUTABILITY VIOLATION: product count changed from %d to %d", preTotal, postTotal)
		}

		// Ensure SKU-PREV-NEW was NOT written to database
		_, err = store.GetProduct(ctx, "SKU-PREV-NEW")
		if err == nil {
			t.Fatalf("IMMUTABILITY VIOLATION: new SKU was persisted to database during preview")
		}
	})

	t.Run("Preview_InvariantViolations_Quarantined", func(t *testing.T) {
		staleTime := now.Add(-1 * time.Hour) // Stale timestamp (older than -10m)
		patchPayload := domain.CatalogPatch{
			Description: "Illegal patch with invariants",
			Author:      "Mallory Attacker",
			Items: []domain.PatchItem{
				// Invariant A violation: Resurrection of WITHDRAWN product
				{
					SKU:    "SKU-PREV-02",
					Name:   "Resurrected Cable",
					Price:  20.00,
					Status: domain.StatusActive,
				},
				// Invariant B violation: Outdated timestamp
				{
					SKU:       "SKU-PREV-01",
					Name:      "Outdated Keyboard",
					Price:     40.00,
					Status:    domain.StatusActive,
					Timestamp: &staleTime,
				},
				// Invariant C violation: Negative price
				{
					SKU:    "SKU-BAD-PRICE",
					Name:   "Freebie Error",
					Price:  -10.00,
					Status: domain.StatusActive,
				},
			},
		}

		bodyBytes, _ := json.Marshal(patchPayload)
		req := httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("preview should return 200 OK even with dead letters, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp domain.PatchPreviewResult
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.CanCommit {
			t.Errorf("expected CanCommit == false on invariant violations")
		}
		if resp.DeadLetterCount != 3 {
			t.Errorf("expected 3 dead letters, got %d", resp.DeadLetterCount)
		}
		if len(resp.DeadLetterItems) != 3 {
			t.Errorf("expected 3 dead letter items, got %d", len(resp.DeadLetterItems))
		}
	})

	t.Run("ValidationErrors_EmptyBodyOrItems", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/patches/preview", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for empty items, got %d", rec.Code)
		}
	})
}

// -----------------------------------------------------------------------------
// Test 2: POST /api/patches/publish
// -----------------------------------------------------------------------------

func TestAPI_Patches_Publish(t *testing.T) {
	store, _, mux, _ := setupM4TestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	seedProductHelper(t, store, "SKU-PUB-01", "Existing Desk", 150.00, domain.StatusActive, now.Add(-5*time.Minute))

	patchPayload := domain.CatalogPatch{
		PatchID:     "patch-furniture-01",
		Description: "Update desk and add chair",
		Author:      "Staff Publisher",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-PUB-01",
				Name:      "Standing Desk Pro",
				Price:     180.00,
				Status:    domain.StatusActive,
				Timestamp: func() *time.Time { t := now; return &t }(),
			},
			{
				SKU:       "SKU-PUB-02",
				Name:      "Ergonomic Stool",
				Price:     85.00,
				Status:    domain.StatusActive,
				Timestamp: func() *time.Time { t := now; return &t }(),
			},
		},
	}

	bodyBytes, _ := json.Marshal(patchPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp api.PatchPublishResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode publish response: %v", err)
	}

	if resp.Version.VersionID != "patch-furniture-01" {
		t.Errorf("expected version ID patch-furniture-01, got %s", resp.Version.VersionID)
	}
	if resp.Version.VersionNumber != 1 {
		t.Errorf("expected version number 1, got %d", resp.Version.VersionNumber)
	}
	if resp.Version.Type != domain.VersionTypePatch {
		t.Errorf("expected version type PATCH, got %s", resp.Version.Type)
	}
	if len(resp.Deltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d", len(resp.Deltas))
	}

	// 1. Verify live catalog reflects O(1) materialized updates
	desk, err := store.GetProduct(ctx, "SKU-PUB-01")
	if err != nil {
		t.Fatalf("failed to get product desk: %v", err)
	}
	if desk.Price != 180.00 || desk.Name != "Standing Desk Pro" {
		t.Errorf("live desk not updated: price=%f, name=%s", desk.Price, desk.Name)
	}

	stool, err := store.GetProduct(ctx, "SKU-PUB-02")
	if err != nil {
		t.Fatalf("failed to get product stool: %v", err)
	}
	if stool.Price != 85.00 || stool.Status != domain.StatusActive {
		t.Errorf("live stool not inserted: price=%f, status=%s", stool.Price, stool.Status)
	}

	// 2. Verify audit delta journaling
	deltas, err := store.GetVersionDeltas(ctx, "patch-furniture-01")
	if err != nil {
		t.Fatalf("failed to get version deltas: %v", err)
	}
	if len(deltas) != 2 {
		t.Fatalf("expected 2 stored deltas, got %d", len(deltas))
	}
}

// -----------------------------------------------------------------------------
// Test 3: POST /api/patches/publish (Gated on Dead Letters -> 422 Unprocessable)
// -----------------------------------------------------------------------------

func TestAPI_Patches_Publish_GatedOnDeadLetters(t *testing.T) {
	store, _, mux, _ := setupM4TestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed withdrawn product
	seedProductHelper(t, store, "SKU-TOMBSTONE-01", "Archived Widget", 10.00, domain.StatusWithdrawn, now.Add(-5*time.Minute))

	// Attempt patch that resurrects the tombstone
	patchPayload := domain.CatalogPatch{
		Description: "Illegal resurrection publish",
		Author:      "Intruder",
		Items: []domain.PatchItem{
			{
				SKU:    "SKU-TOMBSTONE-01",
				Name:   "Archived Widget Revived",
				Price:  12.00,
				Status: domain.StatusActive, // Illegal!
			},
		},
	}

	bodyBytes, _ := json.Marshal(patchPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp api.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Code != api.ErrCodeCommitBlocked {
		t.Errorf("expected error code COMMIT_BLOCKED, got %s", errResp.Code)
	}

	// Verify database was NOT modified (atomic rollback)
	prod, _ := store.GetProduct(ctx, "SKU-TOMBSTONE-01")
	if prod.Status != domain.StatusWithdrawn {
		t.Fatalf("CRITICAL INVARIANT VIOLATION: tombstone was resurrected to %s", prod.Status)
	}
	versions, total, _ := store.ListVersions(ctx, 10, 0)
	if total != 0 || len(versions) != 0 {
		t.Errorf("expected 0 versions created on rejected publish, got %d", total)
	}
}

// -----------------------------------------------------------------------------
// Test 4: GET /api/versions (Pagination & Sorting)
// -----------------------------------------------------------------------------

func TestAPI_Versions_List(t *testing.T) {
	store, _, mux, _ := setupM4TestEnv(t)

	// Create 5 versions via storage
	for i := 1; i <= 5; i++ {
		ver := &domain.CatalogVersion{
			VersionID:     fmt.Sprintf("ver-%02d", i),
			VersionNumber: i,
			Description:   fmt.Sprintf("Milestone release %d", i),
			Author:        "Release Bot",
			Type:          domain.VersionTypePatch,
			Summary:       domain.ChangeSummary{Added: i},
			CreatedAt:     time.Now().UTC().Add(time.Duration(i) * time.Minute),
		}
		createVersionHelper(t, store, ver, nil)
	}

	t.Run("DefaultPagination", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/versions", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp api.VersionListResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Total != 5 {
			t.Errorf("expected total 5, got %d", resp.Total)
		}
		if len(resp.Versions) != 5 {
			t.Fatalf("expected 5 versions, got %d", len(resp.Versions))
		}
		// Verify descending order
		if resp.Versions[0].VersionNumber != 5 || resp.Versions[4].VersionNumber != 1 {
			t.Errorf("expected versions ordered newest first, got V%d ... V%d", resp.Versions[0].VersionNumber, resp.Versions[4].VersionNumber)
		}
	})

	t.Run("OffsetAndLimit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/versions?limit=2&offset=2", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		var resp api.VersionListResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)

		if resp.Total != 5 {
			t.Errorf("expected total 5, got %d", resp.Total)
		}
		if len(resp.Versions) != 2 {
			t.Fatalf("expected 2 versions, got %d", len(resp.Versions))
		}
		if resp.Versions[0].VersionNumber != 3 || resp.Versions[1].VersionNumber != 2 {
			t.Errorf("expected V3 and V2, got V%d and V%d", resp.Versions[0].VersionNumber, resp.Versions[1].VersionNumber)
		}
	})

	t.Run("InvalidQueryParams", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/versions?limit=-5", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for limit=-5, got %d", rec.Code)
		}
	})
}

// -----------------------------------------------------------------------------
// Test 5: GET /api/versions/{id}/diff
// -----------------------------------------------------------------------------

func TestAPI_Versions_Diff(t *testing.T) {
	store, _, mux, _ := setupM4TestEnv(t)
	now := time.Now().UTC()

	ver := &domain.CatalogVersion{
		VersionID:     "ver-diff-01",
		VersionNumber: 1,
		Description:   "Diff test version",
		Author:        "Diff Tester",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now,
	}
	deltas := []domain.ItemDelta{
		{
			VersionID:    "ver-diff-01",
			SKU:          "SKU-DIFF-A",
			Action:       domain.DeltaActionAdded,
			NewName:      "Item A",
			NewPrice:     19.99,
			NewStatus:    domain.StatusActive,
			NewTimestamp: now,
		},
	}
	createVersionHelper(t, store, ver, deltas)

	t.Run("ValidVersionID_ReturnsDeltas", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/versions/ver-diff-01/diff", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var respDeltas []domain.ItemDelta
		if err := json.NewDecoder(rec.Body).Decode(&respDeltas); err != nil {
			t.Fatalf("failed decoding deltas: %v", err)
		}

		if len(respDeltas) != 1 {
			t.Fatalf("expected 1 delta, got %d", len(respDeltas))
		}
		if respDeltas[0].SKU != "SKU-DIFF-A" || respDeltas[0].NewPrice != 19.99 {
			t.Errorf("unexpected delta contents: %+v", respDeltas[0])
		}
	})

	t.Run("InvalidVersionID_Returns404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/versions/nonexistent-id/diff", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})
}

// -----------------------------------------------------------------------------
// Test 6: POST /api/versions/{id}/rollback (Git-revert & Tombstone Invariant)
// -----------------------------------------------------------------------------

func TestAPI_Versions_Rollback(t *testing.T) {
	store, _, mux, _ := setupM4TestEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Step 1: Create V1 with 2 active items
	seedProductHelper(t, store, "SKU-ROLL-01", "Coffee Beans", 12.00, domain.StatusActive, now)
	seedProductHelper(t, store, "SKU-ROLL-02", "Paper Cups", 5.00, domain.StatusActive, now)
	v1 := &domain.CatalogVersion{
		VersionID:     "ver-baseline-v1",
		VersionNumber: 1,
		Description:   "Baseline V1",
		Author:        "Author",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	deltasV1 := []domain.ItemDelta{
		{VersionID: "ver-baseline-v1", SKU: "SKU-ROLL-01", Action: domain.DeltaActionAdded, NewName: "Coffee Beans", NewPrice: 12.00, NewStatus: domain.StatusActive, NewTimestamp: now},
		{VersionID: "ver-baseline-v1", SKU: "SKU-ROLL-02", Action: domain.DeltaActionAdded, NewName: "Paper Cups", NewPrice: 5.00, NewStatus: domain.StatusActive, NewTimestamp: now},
	}
	createVersionHelper(t, store, v1, deltasV1)

	// Step 2: Create V2 (SKU-ROLL-01 price updated to 18.00; SKU-ROLL-02 withdrawn)
	now2 := now.Add(5 * time.Minute)
	seedProductHelper(t, store, "SKU-ROLL-01", "Coffee Beans Deluxe", 18.00, domain.StatusActive, now2)
	seedProductHelper(t, store, "SKU-ROLL-02", "Paper Cups Discontinued", 5.00, domain.StatusWithdrawn, now2)
	oldName1, oldName2 := "Coffee Beans", "Paper Cups"
	oldPrice1 := 12.00
	oldStatus2 := domain.StatusActive
	v2 := &domain.CatalogVersion{
		VersionID:     "ver-update-v2",
		VersionNumber: 2,
		Description:   "Update V2",
		Author:        "Author",
		Type:          domain.VersionTypePatch,
		CreatedAt:     now2,
	}
	deltasV2 := []domain.ItemDelta{
		{VersionID: "ver-update-v2", SKU: "SKU-ROLL-01", Action: domain.DeltaActionUpdated, OldName: &oldName1, NewName: "Coffee Beans Deluxe", OldPrice: &oldPrice1, NewPrice: 18.00, NewStatus: domain.StatusActive, NewTimestamp: now2},
		{VersionID: "ver-update-v2", SKU: "SKU-ROLL-02", Action: domain.DeltaActionWithdrawn, OldName: &oldName2, NewName: "Paper Cups Discontinued", OldStatus: &oldStatus2, NewStatus: domain.StatusWithdrawn, NewTimestamp: now2},
	}
	createVersionHelper(t, store, v2, deltasV2)

	// Step 3: Execute Rollback to V1
	rollbackReqBody := api.RollbackRequest{
		Author:      "Rollback Admin",
		Description: "Revert prices to V1 baseline",
	}
	bodyBytes, _ := json.Marshal(rollbackReqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/versions/ver-baseline-v1/rollback", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp api.RollbackResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed decoding rollback response: %v", err)
	}

	// Forward-moving version verification
	if resp.Version.VersionNumber != 3 {
		t.Errorf("expected forward version number 3, got %d", resp.Version.VersionNumber)
	}
	if resp.Version.Type != domain.VersionTypeRollback {
		t.Errorf("expected version type ROLLBACK, got %s", resp.Version.Type)
	}
	if resp.Version.RevertOfVersionID == nil || *resp.Version.RevertOfVersionID != "ver-baseline-v1" {
		t.Errorf("expected revert_of_version_id == ver-baseline-v1, got %v", resp.Version.RevertOfVersionID)
	}

	// Live catalog verification: Price reverted
	p1, _ := store.GetProduct(ctx, "SKU-ROLL-01")
	if p1.Price != 12.00 || p1.Name != "Coffee Beans" {
		t.Errorf("expected SKU-ROLL-01 reverted to $12.00 'Coffee Beans', got %f '%s'", p1.Price, p1.Name)
	}

	// CRITICAL INVARIANT A: Tombstone Preservation Guarantee
	// SKU-ROLL-02 was WITHDRAWN in V2. Rollback to V1 (where it was ACTIVE) MUST NOT resurrect it!
	p2, _ := store.GetProduct(ctx, "SKU-ROLL-02")
	if p2.Status != domain.StatusWithdrawn {
		t.Fatalf("CRITICAL TOMBSTONE INVARIANT VIOLATION: product was reactivated to %s on rollback", p2.Status)
	}
	if p2.Name != "Paper Cups" {
		t.Errorf("expected metadata reverted to 'Paper Cups', got '%s'", p2.Name)
	}

	// Non-destructive history check: V1 and V2 still exist untouched
	vList, total, _ := store.ListVersions(ctx, 10, 0)
	if total != 3 || len(vList) != 3 {
		t.Errorf("expected 3 total versions in history, got %d", total)
	}

	// Subtest: Rollback to non-existent ID
	reqMissing := httptest.NewRequest(http.MethodPost, "/api/versions/missing-target-id/rollback", bytes.NewBufferString("{}"))
	recMissing := httptest.NewRecorder()
	mux.ServeHTTP(recMissing, reqMissing)
	if recMissing.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing rollback target, got %d", recMissing.Code)
	}
}

// -----------------------------------------------------------------------------
// Test 7: SSE Streaming Integration (version_created & version_rollback events)
// -----------------------------------------------------------------------------

func TestAPI_SSE_CatalogVersionEvents(t *testing.T) {
	_, broker, mux, _ := setupM4TestEnv(t)
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect SSE client
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("failed creating sse request: %v", err)
	}

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("failed connecting to sse: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from SSE, got %d", resp.StatusCode)
	}

	// Wait until broker tracks the client
	deadline := time.Now().Add(2 * time.Second)
	for broker.ClientCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if broker.ClientCount() != 1 {
		t.Fatalf("broker failed to register sse client")
	}

	type receivedEvent struct {
		eventType string
		rawJSON   string
	}
	eventChan := make(chan receivedEvent, 10)

	// Stream scanner in background
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var curType string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				curType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				eventChan <- receivedEvent{eventType: curType, rawJSON: data}
				curType = ""
			}
		}
	}()

	// 1. Trigger version_created by publishing a patch
	patch := domain.CatalogPatch{
		PatchID:     "ver-sse-pub-01",
		Description: "SSE Patch Event Test",
		Author:      "SSE Tester",
		Items: []domain.PatchItem{
			{SKU: "SKU-SSE-01", Name: "SSE Item 1", Price: 10.00, Status: domain.StatusActive},
		},
	}
	bodyBytes, _ := json.Marshal(patch)
	pubResp, err := server.Client().Post(server.URL+"/api/patches/publish", "application/json", bytes.NewReader(bodyBytes))
	if err != nil || pubResp.StatusCode != http.StatusCreated {
		t.Fatalf("publish failed: %v", err)
	}
	_ = pubResp.Body.Close()

	// Assert receipt of version_created event
	select {
	case evt := <-eventChan:
		if evt.eventType != "version_created" {
			t.Errorf("expected event type version_created, got %s", evt.eventType)
		}
		if !strings.Contains(evt.rawJSON, "ver-sse-pub-01") {
			t.Errorf("expected version ID in event data, got %s", evt.rawJSON)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for version_created SSE event")
	}

	// 2. Trigger version_rollback by reverting to the created version
	rollResp, err := server.Client().Post(server.URL+"/api/versions/ver-sse-pub-01/rollback", "application/json", bytes.NewBufferString(`{"author":"Tester"}`))
	if err != nil || rollResp.StatusCode != http.StatusOK {
		t.Fatalf("rollback failed: %v", err)
	}
	_ = rollResp.Body.Close()

	// Assert receipt of version_rollback event
	select {
	case evt := <-eventChan:
		if evt.eventType != "version_rollback" {
			t.Errorf("expected event type version_rollback, got %s", evt.eventType)
		}
		if !strings.Contains(evt.rawJSON, "ver-sse-pub-01") {
			t.Errorf("expected revert_of_version_id in rollback event, got %s", evt.rawJSON)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for version_rollback SSE event")
	}
}

// -----------------------------------------------------------------------------
// Test 8: Concurrency & Race Stress (go test -race)
// -----------------------------------------------------------------------------

func TestAPI_ConcurrencyAndRace(t *testing.T) {
	store, _, mux, _ := setupM4TestEnv(t)
	server := httptest.NewServer(mux)
	defer server.Close()

	now := time.Now().UTC()

	// Seed baseline products and initial version V1
	for i := 0; i < 20; i++ {
		seedProductHelper(t, store, fmt.Sprintf("SKU-CONC-%02d", i), fmt.Sprintf("Conc Prod %d", i), float64(10+i), domain.StatusActive, now)
	}
	v1 := &domain.CatalogVersion{
		VersionID:     "ver-conc-v1",
		VersionNumber: 1,
		Description:   "Baseline for race test",
		Author:        "Bench",
		Type:          domain.VersionTypeInitial,
		CreatedAt:     now,
	}
	createVersionHelper(t, store, v1, nil)

	const workers = 20
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	errCount := int32(0)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			client := server.Client()

			<-startBarrier // Simultaneous launch

			switch workerID % 4 {
			case 0:
				// Worker Category 0: Concurrent Patch Preview
				patch := domain.CatalogPatch{
					Description: fmt.Sprintf("Preview worker %d", workerID),
					Author:      "Tester",
					Items: []domain.PatchItem{
						{SKU: fmt.Sprintf("SKU-CONC-%02d", workerID%20), Name: "Updated Name", Price: 99.00, Status: domain.StatusActive},
					},
				}
				b, _ := json.Marshal(patch)
				resp, err := client.Post(server.URL+"/api/patches/preview", "application/json", bytes.NewReader(b))
				if err != nil || resp.StatusCode != http.StatusOK {
					atomic.AddInt32(&errCount, 1)
				}
				if resp != nil {
					_ = resp.Body.Close()
				}

			case 1:
				// Worker Category 1: Concurrent Patch Publishing
				patch := domain.CatalogPatch{
					Description: fmt.Sprintf("Publish worker %d", workerID),
					Author:      "Publisher",
					Items: []domain.PatchItem{
						{SKU: fmt.Sprintf("SKU-NEW-WORKER-%02d", workerID), Name: "New Product", Price: 50.00, Status: domain.StatusActive},
					},
				}
				b, _ := json.Marshal(patch)
				resp, err := client.Post(server.URL+"/api/patches/publish", "application/json", bytes.NewReader(b))
				if err != nil || (resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK) {
					atomic.AddInt32(&errCount, 1)
				}
				if resp != nil {
					_ = resp.Body.Close()
				}

			case 2:
				// Worker Category 2: Concurrent List Versions and Diff
				resp, err := client.Get(server.URL + "/api/versions?limit=5")
				if err != nil || resp.StatusCode != http.StatusOK {
					atomic.AddInt32(&errCount, 1)
				}
				if resp != nil {
					_ = resp.Body.Close()
				}

				respDiff, err := client.Get(server.URL + "/api/versions/ver-conc-v1/diff")
				if err != nil || respDiff.StatusCode != http.StatusOK {
					atomic.AddInt32(&errCount, 1)
				}
				if respDiff != nil {
					_ = respDiff.Body.Close()
				}

			case 3:
				// Worker Category 3: Concurrent Rollback to V1
				resp, err := client.Post(server.URL+"/api/versions/ver-conc-v1/rollback", "application/json", bytes.NewBufferString(`{"author":"RaceWorker"}`))
				if err != nil || resp.StatusCode != http.StatusOK {
					atomic.AddInt32(&errCount, 1)
				}
				if resp != nil {
					_ = resp.Body.Close()
				}
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	if errors := atomic.LoadInt32(&errCount); errors > 0 {
		t.Fatalf("%d requests failed during concurrency and race stress test", errors)
	}
}
