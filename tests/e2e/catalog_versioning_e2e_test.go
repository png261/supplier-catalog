package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// setupVersioningE2EServer boots an isolated E2E server environment with SQLite storage,
// SSE broker, catalog sync engine, and full API handler stack including multi-tenancy.
func setupVersioningE2EServer(t *testing.T) (*httptest.Server, *storage.SQLiteStorage, *api.Broker, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "e2e_catalog_versioning.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed opening sqlite storage: %v", err)
	}

	broker := api.NewBroker(api.WithClientCap(128), api.WithPingInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := api.NewProgressHook(broker)
	engine := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	serverHandler := api.NewServer(store, engine, broker, "", api.WithDataDir(tempDir))
	ts := httptest.NewServer(serverHandler)

	t.Cleanup(func() {
		ts.Close()
		cancel()
		broker.Close()
		_ = store.Close()
		_ = os.Remove(dbPath)
	})

	return ts, store, broker, tempDir
}

// sseMessage captures an SSE event received over persistent HTTP stream.
type sseMessage struct {
	eventType string
	data      string
}

// startSSEListener connects a persistent client to GET /api/events and returns a channel of events.
func startSSEListener(ctx context.Context, t *testing.T, serverURL string, shopID string) (<-chan sseMessage, func()) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/api/events", nil)
	if err != nil {
		t.Fatalf("failed creating SSE request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if shopID != "" {
		req.Header.Set("X-Shop-Id", shopID)
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed connecting to SSE endpoint: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("expected SSE status 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("expected Content-Type text/event-stream, got %s", ct)
	}

	eventChan := make(chan sseMessage, 50)
	var closed int32

	cleanup := func() {
		if atomic.CompareAndSwapInt32(&closed, 0, 1) {
			_ = resp.Body.Close()
		}
	}

	go func() {
		defer cleanup()
		scanner := bufio.NewScanner(resp.Body)
		var currentType string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				currentType = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			} else if strings.HasPrefix(line, "data: ") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
				select {
				case eventChan <- sseMessage{eventType: currentType, data: data}:
				case <-ctx.Done():
					return
				}
				currentType = ""
			}
		}
	}()

	return eventChan, cleanup
}

// -----------------------------------------------------------------------------
// Test 1: Full Lifecycle E2E Verification
// -----------------------------------------------------------------------------

func TestE2E_CatalogVersioning_FullLifecycle(t *testing.T) {
	ts, store, broker, _ := setupVersioningE2EServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpClient := ts.Client()

	// -------------------------------------------------------------------------
	// a & b. Spin up httptest.NewServer and connect persistent SSE client
	// -------------------------------------------------------------------------
	defaultEvents, closeDefaultSSE := startSSEListener(ctx, t, ts.URL, "")
	defer closeDefaultSSE()

	// Wait for default broker to register the subscriber
	deadline := time.Now().Add(2 * time.Second)
	for broker.ClientCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if broker.ClientCount() < 1 {
		t.Fatalf("SSE broker failed to register persistent listener")
	}

	now := time.Now().UTC().Truncate(time.Second)
	t1 := now.Add(-30 * time.Minute)
	t2 := now.Add(-20 * time.Minute)
	t3 := now.Add(-10 * time.Minute)
	t4 := now

	// -------------------------------------------------------------------------
	// c. Initial sync / seeding establishing Version 1
	// -------------------------------------------------------------------------
	v1PatchID := "ver-e2e-v1-initial"
	v1Patch := domain.CatalogPatch{
		PatchID:     v1PatchID,
		Description: "Initial catalog seeding baseline",
		Author:      "catalog-admin@coderpush.com",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-E2E-MOUSE",
				Name:      "Ergonomic Wireless Mouse",
				Price:     49.99,
				Status:    domain.StatusActive,
				Timestamp: &t1,
			},
			{
				SKU:       "SKU-E2E-KEYBOARD",
				Name:      "Mechanical Keyboard RGB",
				Price:     129.99,
				Status:    domain.StatusActive,
				Timestamp: &t1,
			},
			{
				SKU:       "SKU-E2E-MONITOR",
				Name:      "Ultra-Wide 4K Gaming Monitor",
				Price:     599.99,
				Status:    domain.StatusActive,
				Timestamp: &t1,
			},
		},
	}
	v1Body, _ := json.Marshal(v1Patch)
	respV1, err := httpClient.Post(ts.URL+"/api/patches/publish", "application/json", bytes.NewReader(v1Body))
	if err != nil {
		t.Fatalf("failed POST /api/patches/publish for V1: %v", err)
	}
	defer respV1.Body.Close()

	if respV1.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(respV1.Body)
		t.Fatalf("expected V1 201 Created, got %d: %s", respV1.StatusCode, string(b))
	}

	var pubResV1 api.PatchPublishResponse
	if err := json.NewDecoder(respV1.Body).Decode(&pubResV1); err != nil {
		t.Fatalf("failed decoding V1 response: %v", err)
	}
	if pubResV1.Version.VersionID != v1PatchID {
		t.Errorf("expected version ID %s, got %s", v1PatchID, pubResV1.Version.VersionID)
	}
	if pubResV1.Version.VersionNumber != 1 {
		t.Errorf("expected VersionNumber 1, got %d", pubResV1.Version.VersionNumber)
	}
	if pubResV1.Version.Type != domain.VersionTypePatch {
		t.Errorf("expected version type PATCH, got %s", pubResV1.Version.Type)
	}
	if pubResV1.Version.TotalItemsAffected != 3 {
		t.Errorf("expected 3 items affected in V1, got %d", pubResV1.Version.TotalItemsAffected)
	}

	// Verify SSE event for Version 1 creation
	select {
	case evt := <-defaultEvents:
		if evt.eventType != "version_created" {
			t.Errorf("expected SSE event version_created, got %s", evt.eventType)
		}
		if !strings.Contains(evt.data, v1PatchID) {
			t.Errorf("expected version ID %s in SSE data, got %s", v1PatchID, evt.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for V1 version_created SSE event")
	}

	// Verify live products table has 3 active items for O(1) query
	pMouse, err := store.GetProduct(ctx, "SKU-E2E-MOUSE")
	if err != nil || pMouse == nil || pMouse.Price != 49.99 {
		t.Fatalf("V1 live catalog lookup failed for SKU-E2E-MOUSE: %+v, err: %v", pMouse, err)
	}

	// -------------------------------------------------------------------------
	// d. Preview sparse patch via POST /api/patches/preview:
	//    - Verify HTTP 200
	//    - Verify zero database row writes
	//    - Collate SHA-256 catalog checksum matching projected post-publish checksum
	// -------------------------------------------------------------------------
	v2PatchID := "ver-e2e-v2-sparse"
	v2Patch := domain.CatalogPatch{
		PatchID:     v2PatchID,
		Description: "Sparse update: price adjustment and headset addition",
		Author:      "pricing-lead@coderpush.com",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-E2E-MOUSE",
				Name:      "Ergonomic Wireless Mouse Pro",
				Price:     59.99,
				Status:    domain.StatusActive,
				Timestamp: &t2,
			},
			{
				SKU:       "SKU-E2E-HEADSET",
				Name:      "Noise Cancelling Headset",
				Price:     149.99,
				Status:    domain.StatusActive,
				Timestamp: &t2,
			},
		},
	}

	// Capture catalog state before preview
	checksumBeforePreview, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
	if err != nil {
		t.Fatalf("failed calculating DB checksum before preview: %v", err)
	}
	versionsBeforePreview, totalVBefore, _ := store.ListVersions(ctx, 10, 0)
	if totalVBefore != 1 {
		t.Fatalf("expected exactly 1 version before preview, got %d", totalVBefore)
	}

	v2Body, _ := json.Marshal(v2Patch)
	prevResp, err := httpClient.Post(ts.URL+"/api/patches/preview", "application/json", bytes.NewReader(v2Body))
	if err != nil {
		t.Fatalf("POST /api/patches/preview failed: %v", err)
	}
	defer prevResp.Body.Close()

	if prevResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(prevResp.Body)
		t.Fatalf("expected preview 200 OK, got %d: %s", prevResp.StatusCode, string(b))
	}

	var previewResult domain.PatchPreviewResult
	if err := json.NewDecoder(prevResp.Body).Decode(&previewResult); err != nil {
		t.Fatalf("failed decoding preview response: %v", err)
	}

	if !previewResult.CanCommit {
		t.Errorf("expected CanCommit true, got false")
	}
	if previewResult.AddedCount != 1 {
		t.Errorf("expected AddedCount 1, got %d", previewResult.AddedCount)
	}
	if previewResult.UpdatedCount != 1 {
		t.Errorf("expected UpdatedCount 1, got %d", previewResult.UpdatedCount)
	}
	if previewResult.DeadLetterCount != 0 {
		t.Errorf("expected DeadLetterCount 0, got %d", previewResult.DeadLetterCount)
	}
	if previewResult.CatalogChecksum == "" {
		t.Errorf("expected non-empty projected CatalogChecksum in preview")
	}
	projectedChecksum := previewResult.CatalogChecksum

	// Verify ZERO database row writes occurred during preview
	checksumAfterPreview, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
	if err != nil {
		t.Fatalf("failed calculating DB checksum after preview: %v", err)
	}
	if checksumBeforePreview != checksumAfterPreview {
		t.Fatalf("immutability violation: DB checksum changed during preview! Before: %s, After: %s",
			checksumBeforePreview, checksumAfterPreview)
	}

	versionsAfterPreview, totalVAfter, _ := store.ListVersions(ctx, 10, 0)
	if totalVAfter != totalVBefore || len(versionsAfterPreview) != len(versionsBeforePreview) {
		t.Fatalf("immutability violation: catalog_versions modified during preview")
	}

	// Ensure newly projected product does NOT exist in database yet
	pHeadsetBefore, err := store.GetProduct(ctx, "SKU-E2E-HEADSET")
	if err == nil && pHeadsetBefore != nil {
		t.Fatalf("immutability violation: uncommitted product SKU-E2E-HEADSET leaked into DB during preview")
	}

	// -------------------------------------------------------------------------
	// e. Publish patch via POST /api/patches/publish:
	//    - Verify HTTP 201 Created
	//    - Verify Version 2 (type = PATCH)
	//    - Verify deltas journaled
	//    - Verify live products updated for O(1) query
	//    - Collate SHA-256 catalog checksum matching preview projected checksum
	//    - Verify receipt of version_created SSE event
	// -------------------------------------------------------------------------
	pubRespV2, err := httpClient.Post(ts.URL+"/api/patches/publish", "application/json", bytes.NewReader(v2Body))
	if err != nil {
		t.Fatalf("POST /api/patches/publish V2 failed: %v", err)
	}
	defer pubRespV2.Body.Close()

	if pubRespV2.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(pubRespV2.Body)
		t.Fatalf("expected V2 201 Created, got %d: %s", pubRespV2.StatusCode, string(b))
	}

	var pubResV2 api.PatchPublishResponse
	if err := json.NewDecoder(pubRespV2.Body).Decode(&pubResV2); err != nil {
		t.Fatalf("failed decoding V2 publish response: %v", err)
	}

	if pubResV2.Version.VersionID != v2PatchID {
		t.Errorf("expected version ID %s, got %s", v2PatchID, pubResV2.Version.VersionID)
	}
	if pubResV2.Version.VersionNumber != 2 {
		t.Errorf("expected VersionNumber 2, got %d", pubResV2.Version.VersionNumber)
	}
	if pubResV2.Version.Type != domain.VersionTypePatch {
		t.Errorf("expected version type PATCH, got %s", pubResV2.Version.Type)
	}
	if len(pubResV2.Deltas) != 2 {
		t.Fatalf("expected 2 published deltas, got %d", len(pubResV2.Deltas))
	}

	// Verify deltas journaled in storage
	v2Deltas, err := store.GetVersionDeltas(ctx, v2PatchID)
	if err != nil {
		t.Fatalf("failed fetching V2 deltas: %v", err)
	}
	if len(v2Deltas) != 2 {
		t.Fatalf("expected 2 journaled deltas in storage, got %d", len(v2Deltas))
	}

	// Verify live products table updated for O(1) query
	pMouseV2, err := store.GetProduct(ctx, "SKU-E2E-MOUSE")
	if err != nil || pMouseV2 == nil {
		t.Fatalf("failed retrieving updated SKU-E2E-MOUSE: %v", err)
	}
	if pMouseV2.Name != "Ergonomic Wireless Mouse Pro" || pMouseV2.Price != 59.99 {
		t.Errorf("unexpected live state for SKU-E2E-MOUSE: %+v", pMouseV2)
	}

	pHeadsetV2, err := store.GetProduct(ctx, "SKU-E2E-HEADSET")
	if err != nil || pHeadsetV2 == nil {
		t.Fatalf("failed retrieving newly added SKU-E2E-HEADSET: %v", err)
	}
	if pHeadsetV2.Price != 149.99 || pHeadsetV2.Status != domain.StatusActive {
		t.Errorf("unexpected live state for SKU-E2E-HEADSET: %+v", pHeadsetV2)
	}

	// Collate actual DB checksum against projected preview checksum
	actualPostPublishChecksum, err := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
	if err != nil {
		t.Fatalf("failed calculating DB checksum after V2 publish: %v", err)
	}
	if actualPostPublishChecksum != projectedChecksum {
		t.Fatalf("EMPIRICAL PARITY FAILURE: projected preview checksum != actual DB checksum!\nProjected: %s\nActual DB: %s",
			projectedChecksum, actualPostPublishChecksum)
	}

	// Verify SSE event for Version 2 creation
	select {
	case evt := <-defaultEvents:
		if evt.eventType != "version_created" {
			t.Errorf("expected SSE event version_created, got %s", evt.eventType)
		}
		if !strings.Contains(evt.data, v2PatchID) {
			t.Errorf("expected version ID %s in SSE event data, got %s", v2PatchID, evt.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for V2 version_created SSE event")
	}

	// -------------------------------------------------------------------------
	// f. Publish patch withdrawing an item (StatusWithdrawn) -> verify Version 3
	// -------------------------------------------------------------------------
	v3PatchID := "ver-e2e-v3-withdraw"
	v3Patch := domain.CatalogPatch{
		PatchID:     v3PatchID,
		Description: "Withdraw Mechanical Keyboard due to recall",
		Author:      "quality-lead@coderpush.com",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-E2E-KEYBOARD",
				Name:      "Mechanical Keyboard RGB",
				Price:     129.99,
				Status:    domain.StatusWithdrawn,
				Timestamp: &t3,
			},
		},
	}
	v3Body, _ := json.Marshal(v3Patch)
	pubRespV3, err := httpClient.Post(ts.URL+"/api/patches/publish", "application/json", bytes.NewReader(v3Body))
	if err != nil {
		t.Fatalf("POST /api/patches/publish V3 failed: %v", err)
	}
	defer pubRespV3.Body.Close()

	if pubRespV3.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(pubRespV3.Body)
		t.Fatalf("expected V3 201 Created, got %d: %s", pubRespV3.StatusCode, string(b))
	}

	var pubResV3 api.PatchPublishResponse
	if err := json.NewDecoder(pubRespV3.Body).Decode(&pubResV3); err != nil {
		t.Fatalf("failed decoding V3 response: %v", err)
	}
	if pubResV3.Version.VersionNumber != 3 {
		t.Errorf("expected VersionNumber 3, got %d", pubResV3.Version.VersionNumber)
	}
	if pubResV3.Version.Summary.Withdrawn != 1 {
		t.Errorf("expected 1 withdrawn in V3 summary, got %d", pubResV3.Version.Summary.Withdrawn)
	}

	// Verify deltas journaled
	v3Deltas, err := store.GetVersionDeltas(ctx, v3PatchID)
	if err != nil || len(v3Deltas) != 1 {
		t.Fatalf("expected 1 delta for V3, got %d (err: %v)", len(v3Deltas), err)
	}
	if v3Deltas[0].Action != domain.DeltaActionWithdrawn || v3Deltas[0].NewStatus != domain.StatusWithdrawn {
		t.Errorf("unexpected delta action/status: %+v", v3Deltas[0])
	}

	// Verify live product status is now WITHDRAWN
	pKeyboardV3, err := store.GetProduct(ctx, "SKU-E2E-KEYBOARD")
	if err != nil || pKeyboardV3 == nil || pKeyboardV3.Status != domain.StatusWithdrawn {
		t.Fatalf("expected SKU-E2E-KEYBOARD to be WITHDRAWN, got %+v", pKeyboardV3)
	}

	// Verify SSE event for Version 3 creation
	select {
	case evt := <-defaultEvents:
		if evt.eventType != "version_created" {
			t.Errorf("expected version_created SSE event, got %s", evt.eventType)
		}
		if !strings.Contains(evt.data, v3PatchID) {
			t.Errorf("expected %s in SSE data, got %s", v3PatchID, evt.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for V3 version_created SSE event")
	}

	// -------------------------------------------------------------------------
	// g. Send adversarial patch violating Invariants A-E to POST /api/patches/publish:
	//    - Attempting to resurrect withdrawn item (SKU-E2E-KEYBOARD) -> Invariant A
	//    - Negative price (-49.99) -> Invariant C / basic validation
	//    - Stale timestamp (older than existing) -> Invariant B
	//    - Verify HTTP 422 Unprocessable Entity with COMMIT_BLOCKED
	//    - Verify zero database side-effects
	// -------------------------------------------------------------------------
	staleTime := t1.Add(-1 * time.Hour)
	advPatch := domain.CatalogPatch{
		PatchID:     "ver-e2e-adv-attack",
		Description: "Adversarial patch violating catalog invariants",
		Author:      "adversary@attacker.com",
		Items: []domain.PatchItem{
			{
				SKU:       "SKU-E2E-KEYBOARD", // Currently WITHDRAWN in DB
				Name:      "Resurrected Keyboard",
				Price:     119.99,
				Status:    domain.StatusActive, // INVARIANT A VIOLATION: illegal reactivation
				Timestamp: &t4,
			},
			{
				SKU:       "SKU-E2E-MONITOR",
				Name:      "Corrupted Price Monitor",
				Price:     -99.99, // INVARIANT C VIOLATION: negative price
				Status:    domain.StatusActive,
				Timestamp: &t4,
			},
			{
				SKU:       "SKU-E2E-MOUSE",
				Name:      "Stale Update Mouse",
				Price:     35.00,
				Status:    domain.StatusActive,
				Timestamp: &staleTime, // INVARIANT B VIOLATION: stale timestamp
			},
		},
	}

	checksumBeforeAdv, _ := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
	versionsBeforeAdv, totalVAdvBefore, _ := store.ListVersions(ctx, 10, 0)

	advBody, _ := json.Marshal(advPatch)
	advResp, err := httpClient.Post(ts.URL+"/api/patches/publish", "application/json", bytes.NewReader(advBody))
	if err != nil {
		t.Fatalf("POST /api/patches/publish adversarial failed: %v", err)
	}
	defer advResp.Body.Close()

	if advResp.StatusCode != http.StatusUnprocessableEntity {
		b, _ := io.ReadAll(advResp.Body)
		t.Fatalf("expected HTTP 422 Unprocessable Entity, got %d: %s", advResp.StatusCode, string(b))
	}

	var errResp api.ErrorResponse
	if err := json.NewDecoder(advResp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed decoding error response: %v", err)
	}
	if errResp.Code != api.ErrCodeCommitBlocked {
		t.Fatalf("expected error code %s, got %s", api.ErrCodeCommitBlocked, errResp.Code)
	}

	// Verify ZERO database side-effects occurred
	checksumAfterAdv, _ := catalogSync.CalculateCatalogChecksumFromDB(ctx, store.DB())
	if checksumBeforeAdv != checksumAfterAdv {
		t.Fatalf("adversarial attack modified DB checksum despite 422 reject!")
	}

	versionsAfterAdv, totalVAdvAfter, _ := store.ListVersions(ctx, 10, 0)
	if totalVAdvAfter != totalVAdvBefore || len(versionsAfterAdv) != len(versionsBeforeAdv) {
		t.Fatalf("adversarial attack persisted a new version despite 422 reject!")
	}

	// Verify SKU-E2E-KEYBOARD is STILL WITHDRAWN (no resurrection)
	pKeyboardAfterAdv, _ := store.GetProduct(ctx, "SKU-E2E-KEYBOARD")
	if pKeyboardAfterAdv.Status != domain.StatusWithdrawn {
		t.Fatalf("INVARIANT VIOLATION: SKU-E2E-KEYBOARD resurrected by adversarial patch!")
	}

	// Verify no new event arrived on SSE channel
	select {
	case unexpectedEvt := <-defaultEvents:
		t.Fatalf("unexpected SSE event broadcasted for rejected adversarial patch: %+v", unexpectedEvt)
	case <-time.After(150 * time.Millisecond):
		// Expected: no SSE events on commit block
	}

	// -------------------------------------------------------------------------
	// h. Execute Git-revert rollback to Version 1 via POST /api/versions/{v1_id}/rollback:
	//    - Verify HTTP 200 OK
	//    - Verify Version 4 created (type = ROLLBACK, revert_of_version_id = v1_id)
	//    - Verify price/name restored to baseline
	//    - Verify receipt of event: version_rollback SSE event
	//    - CRITICAL INVARIANT A ENFORCEMENT: previously withdrawn product remains WITHDRAWN
	// -------------------------------------------------------------------------
	rollbackReqBody := `{"author":"rollback-engineer@coderpush.com","description":"Emergency revert to V1 baseline"}`
	rollResp, err := httpClient.Post(
		fmt.Sprintf("%s/api/versions/%s/rollback", ts.URL, v1PatchID),
		"application/json",
		strings.NewReader(rollbackReqBody),
	)
	if err != nil {
		t.Fatalf("POST rollback failed: %v", err)
	}
	defer rollResp.Body.Close()

	if rollResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(rollResp.Body)
		t.Fatalf("expected rollback 200 OK, got %d: %s", rollResp.StatusCode, string(b))
	}

	var rollRes api.RollbackResponse
	if err := json.NewDecoder(rollResp.Body).Decode(&rollRes); err != nil {
		t.Fatalf("failed decoding rollback response: %v", err)
	}

	if rollRes.Version.VersionNumber != 4 {
		t.Errorf("expected VersionNumber 4 for rollback, got %d", rollRes.Version.VersionNumber)
	}
	if rollRes.Version.Type != domain.VersionTypeRollback {
		t.Errorf("expected version type ROLLBACK, got %s", rollRes.Version.Type)
	}
	if rollRes.Version.RevertOfVersionID == nil || *rollRes.Version.RevertOfVersionID != v1PatchID {
		t.Errorf("expected revert_of_version_id %s, got %v", v1PatchID, rollRes.Version.RevertOfVersionID)
	}

	// Verify price/name restored to baseline for SKU-E2E-MOUSE
	pMouseRollback, err := store.GetProduct(ctx, "SKU-E2E-MOUSE")
	if err != nil || pMouseRollback == nil {
		t.Fatalf("failed retrieving SKU-E2E-MOUSE after rollback: %v", err)
	}
	if pMouseRollback.Name != "Ergonomic Wireless Mouse" || pMouseRollback.Price != 49.99 {
		t.Errorf("expected SKU-E2E-MOUSE restored to baseline (Ergonomic Wireless Mouse, 49.99), got: %+v", pMouseRollback)
	}

	// Product added after baseline (SKU-E2E-HEADSET) should be WITHDRAWN by rollback
	pHeadsetRollback, err := store.GetProduct(ctx, "SKU-E2E-HEADSET")
	if err != nil || pHeadsetRollback == nil {
		t.Fatalf("failed retrieving SKU-E2E-HEADSET after rollback: %v", err)
	}
	if pHeadsetRollback.Status != domain.StatusWithdrawn {
		t.Errorf("expected post-baseline product SKU-E2E-HEADSET to be WITHDRAWN after rollback, got %s", pHeadsetRollback.Status)
	}

	// CRITICAL INVARIANT A ENFORCEMENT ON ROLLBACK:
	// SKU-E2E-KEYBOARD was WITHDRAWN in V3. In V1 baseline, it was ACTIVE.
	// Rollback to V1 MUST NOT resurrect this product! It must remain WITHDRAWN!
	pKeyboardRollback, err := store.GetProduct(ctx, "SKU-E2E-KEYBOARD")
	if err != nil || pKeyboardRollback == nil {
		t.Fatalf("failed retrieving SKU-E2E-KEYBOARD after rollback: %v", err)
	}
	if pKeyboardRollback.Status != domain.StatusWithdrawn {
		t.Fatalf("CRITICAL INVARIANT A VIOLATION: SKU-E2E-KEYBOARD resurrected on rollback! Status is %s, expected WITHDRAWN", pKeyboardRollback.Status)
	}

	// Verify SSE event for rollback
	select {
	case evt := <-defaultEvents:
		if evt.eventType != "version_rollback" {
			t.Errorf("expected SSE event version_rollback, got %s", evt.eventType)
		}
		if !strings.Contains(evt.data, v1PatchID) {
			t.Errorf("expected revert_of_version_id %s in rollback event, got %s", v1PatchID, evt.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for version_rollback SSE event")
	}

	// -------------------------------------------------------------------------
	// i. Multi-tenant isolation:
	//    Repeat operations on secondary shop (X-Shop-Id: tenant-beta),
	//    verifying complete data and SSE event segregation.
	// -------------------------------------------------------------------------
	betaEvents, closeBetaSSE := startSSEListener(ctx, t, ts.URL, "tenant-beta")
	defer closeBetaSSE()

	time.Sleep(100 * time.Millisecond) // Let tenant-beta broker initialize

	// Query versions on tenant-beta (must be 0, completely isolated from default shop)
	reqBetaVersions, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/versions", nil)
	reqBetaVersions.Header.Set("X-Shop-Id", "tenant-beta")
	respBetaVersions, err := httpClient.Do(reqBetaVersions)
	if err != nil {
		t.Fatalf("failed GET /api/versions for tenant-beta: %v", err)
	}
	defer respBetaVersions.Body.Close()

	var betaVerList api.VersionListResponse
	_ = json.NewDecoder(respBetaVersions.Body).Decode(&betaVerList)
	if betaVerList.Total != 0 || len(betaVerList.Versions) != 0 {
		t.Fatalf("isolation violation: tenant-beta sees %d versions from default tenant!", betaVerList.Total)
	}

	// Publish patch on tenant-beta
	betaPatch := domain.CatalogPatch{
		PatchID:     "ver-beta-v1",
		Description: "Tenant Beta initial catalog",
		Author:      "beta-admin@coderpush.com",
		Items: []domain.PatchItem{
			{
				SKU:       "BETA-SKU-001",
				Name:      "Beta Exclusive Gadget",
				Price:     88.88,
				Status:    domain.StatusActive,
				Timestamp: &t4,
			},
		},
	}
	betaBody, _ := json.Marshal(betaPatch)
	reqBetaPub, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/patches/publish", bytes.NewReader(betaBody))
	reqBetaPub.Header.Set("X-Shop-Id", "tenant-beta")
	reqBetaPub.Header.Set("Content-Type", "application/json")
	respBetaPub, err := httpClient.Do(reqBetaPub)
	if err != nil {
		t.Fatalf("failed publishing patch on tenant-beta: %v", err)
	}
	defer respBetaPub.Body.Close()

	if respBetaPub.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(respBetaPub.Body)
		t.Fatalf("expected tenant-beta publish 201 Created, got %d: %s", respBetaPub.StatusCode, string(b))
	}

	// Verify tenant-beta SSE client received version_created event
	select {
	case evt := <-betaEvents:
		if evt.eventType != "version_created" {
			t.Errorf("expected tenant-beta event version_created, got %s", evt.eventType)
		}
		if !strings.Contains(evt.data, "ver-beta-v1") {
			t.Errorf("expected ver-beta-v1 in beta event data, got %s", evt.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for tenant-beta SSE event")
	}

	// Verify default tenant SSE client did NOT receive tenant-beta's event
	select {
	case unexpectedEvt := <-defaultEvents:
		t.Fatalf("SSE cross-talk leak! Default tenant received event from tenant-beta: %+v", unexpectedEvt)
	case <-time.After(200 * time.Millisecond):
		// Expected: zero events leaked to default tenant
	}

	// Verify data isolation: default tenant catalog does not contain BETA-SKU-001
	pBetaInDefault, err := store.GetProduct(ctx, "BETA-SKU-001")
	if err == nil && pBetaInDefault != nil {
		t.Fatalf("data isolation violation: BETA-SKU-001 leaked into default storage!")
	}
}

// -----------------------------------------------------------------------------
// Test 2: Diff Inspection & Version Pagination E2E
// -----------------------------------------------------------------------------

func TestE2E_CatalogVersioning_DiffAndPagination(t *testing.T) {
	ts, _, _, _ := setupVersioningE2EServer(t)
	httpClient := ts.Client()
	now := time.Now().UTC().Truncate(time.Second)

	// Publish 3 sequential patches
	for i := 1; i <= 3; i++ {
		tsI := now.Add(time.Duration(i) * time.Minute)
		patch := domain.CatalogPatch{
			PatchID:     fmt.Sprintf("ver-page-v%d", i),
			Description: fmt.Sprintf("Pagination version %d", i),
			Author:      "paginator@coderpush.com",
			Items: []domain.PatchItem{
				{
					SKU:       fmt.Sprintf("SKU-PAGE-%02d", i),
					Name:      fmt.Sprintf("Page Item %d", i),
					Price:     float64(i * 10),
					Status:    domain.StatusActive,
					Timestamp: &tsI,
				},
			},
		}
		body, _ := json.Marshal(patch)
		resp, err := httpClient.Post(ts.URL+"/api/patches/publish", "application/json", bytes.NewReader(body))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("failed publishing patch %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}

	// 1. Test GET /api/versions pagination: limit=2, offset=0
	respPage1, err := httpClient.Get(ts.URL + "/api/versions?limit=2&offset=0")
	if err != nil {
		t.Fatalf("failed GET /api/versions page 1: %v", err)
	}
	defer respPage1.Body.Close()
	if respPage1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respPage1.StatusCode)
	}
	var page1 api.VersionListResponse
	_ = json.NewDecoder(respPage1.Body).Decode(&page1)
	if page1.Total != 3 {
		t.Errorf("expected total 3, got %d", page1.Total)
	}
	if len(page1.Versions) != 2 {
		t.Fatalf("expected 2 versions on page 1, got %d", len(page1.Versions))
	}
	// Ordered newest first: ver-page-v3, ver-page-v2
	if page1.Versions[0].VersionID != "ver-page-v3" {
		t.Errorf("expected newest version ver-page-v3 first, got %s", page1.Versions[0].VersionID)
	}

	// 2. Test GET /api/versions pagination: limit=2, offset=2
	respPage2, err := httpClient.Get(ts.URL + "/api/versions?limit=2&offset=2")
	if err != nil {
		t.Fatalf("failed GET /api/versions page 2: %v", err)
	}
	defer respPage2.Body.Close()
	var page2 api.VersionListResponse
	_ = json.NewDecoder(respPage2.Body).Decode(&page2)
	if len(page2.Versions) != 1 || page2.Versions[0].VersionID != "ver-page-v1" {
		t.Errorf("expected 1 version (ver-page-v1) on page 2, got %+v", page2.Versions)
	}

	// 3. Test GET /api/versions/{id}/diff
	respDiff, err := httpClient.Get(ts.URL + "/api/versions/ver-page-v2/diff")
	if err != nil {
		t.Fatalf("failed GET /api/versions/ver-page-v2/diff: %v", err)
	}
	defer respDiff.Body.Close()
	if respDiff.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respDiff.StatusCode)
	}
	var deltas []domain.ItemDelta
	_ = json.NewDecoder(respDiff.Body).Decode(&deltas)
	if len(deltas) != 1 || deltas[0].SKU != "SKU-PAGE-02" {
		t.Errorf("unexpected deltas for ver-page-v2: %+v", deltas)
	}

	// 4. Test GET /api/versions/non-existent/diff -> 404
	respDiff404, err := httpClient.Get(ts.URL + "/api/versions/non-existent-id/diff")
	if err != nil {
		t.Fatalf("failed request: %v", err)
	}
	defer respDiff404.Body.Close()
	if respDiff404.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for missing version diff, got %d", respDiff404.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// Test 3: Concurrent Publishes Thread-Safety & Race Hardening
// -----------------------------------------------------------------------------

func TestE2E_CatalogVersioning_ConcurrentPublishesRaceHardening(t *testing.T) {
	ts, _, _, _ := setupVersioningE2EServer(t)
	httpClient := ts.Client()
	now := time.Now().UTC().Truncate(time.Second)

	const concurrency = 8
	var wg sync.WaitGroup
	var successCount int64
	var errCount int64

	startBarrier := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startBarrier

			tsItem := now.Add(time.Duration(idx+1) * time.Minute)
			patch := domain.CatalogPatch{
				PatchID:     fmt.Sprintf("ver-concurrent-%03d", idx),
				Description: fmt.Sprintf("Concurrent publish worker %d", idx),
				Author:      fmt.Sprintf("worker-%d@coderpush.com", idx),
				Items: []domain.PatchItem{
					{
						SKU:       fmt.Sprintf("SKU-CONC-%03d", idx),
						Name:      fmt.Sprintf("Concurrent Product %d", idx),
						Price:     float64(10 + idx),
						Status:    domain.StatusActive,
						Timestamp: &tsItem,
					},
				},
			}
			body, _ := json.Marshal(patch)
			resp, err := httpClient.Post(ts.URL+"/api/patches/publish", "application/json", bytes.NewReader(body))
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusCreated {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&errCount, 1)
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	if errCount > 0 {
		t.Errorf("experienced %d errors during concurrent publish", errCount)
	}
	if successCount != concurrency {
		t.Errorf("expected %d successful publishes, got %d", concurrency, successCount)
	}

	// Verify all versions were properly committed
	respList, err := httpClient.Get(fmt.Sprintf("%s/api/versions?limit=%d", ts.URL, concurrency+10))
	if err != nil {
		t.Fatalf("failed listing versions: %v", err)
	}
	defer respList.Body.Close()

	var verList api.VersionListResponse
	_ = json.NewDecoder(respList.Body).Decode(&verList)
	if verList.Total != concurrency {
		t.Errorf("expected %d total versions in DB, got %d", concurrency, verList.Total)
	}
}
