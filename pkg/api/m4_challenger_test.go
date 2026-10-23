package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coderpush/pkg/api"
	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

// setupChallengerEnv initializes a test HTTP handler with isolated storage and broker.
func setupChallengerEnv(t *testing.T) (*storage.SQLiteStorage, *api.Broker, http.Handler, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "challenger_test.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test sqlite: %v", err)
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

// TestChallenger_M4_StatusCodes_BadRequests validates all 400 Bad Request boundary conditions.
func TestChallenger_M4_StatusCodes_BadRequests(t *testing.T) {
	_, _, mux, _ := setupChallengerEnv(t)

	testCases := []struct {
		name       string
		method     string
		url        string
		body       string
		expectCode int
		expectMsg  string
	}{
		{
			name:       "Preview_MalformedJSON",
			method:     http.MethodPost,
			url:        "/api/patches/preview",
			body:       `{invalid-json`,
			expectCode: http.StatusBadRequest,
			expectMsg:  "malformed JSON",
		},
		{
			name:       "Preview_EmptyItems",
			method:     http.MethodPost,
			url:        "/api/patches/preview",
			body:       `{"items":[]}`,
			expectCode: http.StatusBadRequest,
			expectMsg:  "empty",
		},
		{
			name:       "Publish_MalformedJSON",
			method:     http.MethodPost,
			url:        "/api/patches/publish",
			body:       `{"broken"`,
			expectCode: http.StatusBadRequest,
			expectMsg:  "malformed JSON",
		},
		{
			name:       "Publish_EmptyAuthor",
			method:     http.MethodPost,
			url:        "/api/patches/publish",
			body:       `{"description":"test","author":"   ","items":[{"sku":"S1","name":"N1","price":10,"status":"ACTIVE"}]}`,
			expectCode: http.StatusBadRequest,
			expectMsg:  "author",
		},
		{
			name:       "Publish_EmptyDescription",
			method:     http.MethodPost,
			url:        "/api/patches/publish",
			body:       `{"description":"  ","author":"alice","items":[{"sku":"S1","name":"N1","price":10,"status":"ACTIVE"}]}`,
			expectCode: http.StatusBadRequest,
			expectMsg:  "description",
		},
		{
			name:       "Publish_EmptyItems",
			method:     http.MethodPost,
			url:        "/api/patches/publish",
			body:       `{"description":"valid","author":"alice","items":[]}`,
			expectCode: http.StatusBadRequest,
			expectMsg:  "empty",
		},
		{
			name:       "Versions_NegativeLimit",
			method:     http.MethodGet,
			url:        "/api/versions?limit=-1",
			body:       "",
			expectCode: http.StatusBadRequest,
			expectMsg:  "limit",
		},
		{
			name:       "Versions_ZeroLimit",
			method:     http.MethodGet,
			url:        "/api/versions?limit=0",
			body:       "",
			expectCode: http.StatusBadRequest,
			expectMsg:  "limit",
		},
		{
			name:       "Versions_AlphaLimit",
			method:     http.MethodGet,
			url:        "/api/versions?limit=abc",
			body:       "",
			expectCode: http.StatusBadRequest,
			expectMsg:  "limit",
		},
		{
			name:       "Versions_NegativeOffset",
			method:     http.MethodGet,
			url:        "/api/versions?offset=-5",
			body:       "",
			expectCode: http.StatusBadRequest,
			expectMsg:  "offset",
		},
		{
			name:       "Rollback_MalformedJSON",
			method:     http.MethodPost,
			url:        "/api/versions/v-123/rollback",
			body:       `{not-json`,
			expectCode: http.StatusBadRequest,
			expectMsg:  "malformed JSON",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var bodyReader io.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.url, bodyReader)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tc.expectCode {
				t.Fatalf("expected code %d, got %d: body=%s", tc.expectCode, rec.Code, rec.Body.String())
			}

			var errResp api.ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
				t.Fatalf("failed decoding error response: %v", err)
			}
			if errResp.Code != api.ErrCodeInvalidRequest {
				t.Errorf("expected error code INVALID_REQUEST, got %s", errResp.Code)
			}
			if !strings.Contains(strings.ToLower(errResp.Error), strings.ToLower(tc.expectMsg)) {
				t.Errorf("expected error message to contain '%s', got '%s'", tc.expectMsg, errResp.Error)
			}
		})
	}
}

// TestChallenger_M4_StatusCodes_NotFound validates 404 Not Found on missing versions.
func TestChallenger_M4_StatusCodes_NotFound(t *testing.T) {
	_, _, mux, _ := setupChallengerEnv(t)

	// Test 1: Diff of non-existent version
	reqDiff := httptest.NewRequest(http.MethodGet, "/api/versions/ver-ghost-999/diff", nil)
	recDiff := httptest.NewRecorder()
	mux.ServeHTTP(recDiff, reqDiff)

	if recDiff.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for non-existent version diff, got %d", recDiff.Code)
	}
	var errDiff api.ErrorResponse
	_ = json.NewDecoder(recDiff.Body).Decode(&errDiff)
	if errDiff.Code != api.ErrCodeNotFound {
		t.Errorf("expected NOT_FOUND, got %s", errDiff.Code)
	}

	// Test 2: Rollback to non-existent version
	reqRollback := httptest.NewRequest(http.MethodPost, "/api/versions/ver-ghost-999/rollback", bytes.NewBufferString(`{}`))
	reqRollback.Header.Set("Content-Type", "application/json")
	recRollback := httptest.NewRecorder()
	mux.ServeHTTP(recRollback, reqRollback)

	if recRollback.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for non-existent version rollback, got %d", recRollback.Code)
	}
	var errRollback api.ErrorResponse
	_ = json.NewDecoder(recRollback.Body).Decode(&errRollback)
	if errRollback.Code != api.ErrCodeNotFound {
		t.Errorf("expected NOT_FOUND, got %s", errRollback.Code)
	}
}

// TestChallenger_M4_StatusCodes_422CommitBlocked tests all invariant gating rejections.
func TestChallenger_M4_StatusCodes_422CommitBlocked(t *testing.T) {
	store, _, mux, _ := setupChallengerEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed catalog with a withdrawn item
	seedProductHelper(t, store, "SKU-TOMB-CHALLENGE", "Withdrawn Item", 25.00, domain.StatusWithdrawn, now.Add(-10*time.Minute))

	testCases := []struct {
		name  string
		patch domain.CatalogPatch
	}{
		{
			name: "ResurrectTombstone",
			patch: domain.CatalogPatch{
				Description: "Try to revive tombstone",
				Author:      "Adversary",
				Items: []domain.PatchItem{
					{SKU: "SKU-TOMB-CHALLENGE", Name: "Resurrected", Price: 30.00, Status: domain.StatusActive},
				},
			},
		},
		{
			name: "NegativePrice",
			patch: domain.CatalogPatch{
				Description: "Try negative price",
				Author:      "Adversary",
				Items: []domain.PatchItem{
					{SKU: "SKU-NEW-NEGATIVE", Name: "Negative item", Price: -1.00, Status: domain.StatusActive},
				},
			},
		},
		{
			name: "StaleTimestamp",
			patch: domain.CatalogPatch{
				Description: "Try stale timestamp",
				Author:      "Adversary",
				Items: []domain.PatchItem{
					{
						SKU:       "SKU-TOMB-CHALLENGE",
						Name:      "Withdrawn Item",
						Price:     25.00,
						Status:    domain.StatusWithdrawn,
						Timestamp: func() *time.Time { t := now.Add(-20 * time.Minute); return &t }(),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.patch)
			req := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", rec.Code, rec.Body.String())
			}

			var errResp api.ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
				t.Fatalf("failed decoding error: %v", err)
			}
			if errResp.Code != api.ErrCodeCommitBlocked {
				t.Errorf("expected COMMIT_BLOCKED, got %s", errResp.Code)
			}

			// Ensure zero database mutations
			p, _ := store.GetProduct(ctx, "SKU-TOMB-CHALLENGE")
			if p.Status != domain.StatusWithdrawn {
				t.Fatalf("CRITICAL: tombstone status changed to %s", p.Status)
			}
		})
	}
}

// TestChallenger_M4_MultiTenantIsolation verifies multi-shop isolation across M4 endpoints.
func TestChallenger_M4_MultiTenantIsolation(t *testing.T) {
	_, _, mux, _ := setupChallengerEnv(t)

	// Step 1: Publish patch in tenant "shop-alpha"
	patchAlpha := domain.CatalogPatch{
		PatchID:     "patch-alpha-01",
		Description: "Patch for Alpha",
		Author:      "Alpha Admin",
		Items: []domain.PatchItem{
			{SKU: "SKU-ALPHA-01", Name: "Alpha Product", Price: 100.00, Status: domain.StatusActive},
		},
	}
	bAlpha, _ := json.Marshal(patchAlpha)
	reqAlpha := httptest.NewRequest(http.MethodPost, "/api/patches/publish", bytes.NewReader(bAlpha))
	reqAlpha.Header.Set("Content-Type", "application/json")
	reqAlpha.Header.Set("X-Shop-Id", "shop-alpha")
	recAlpha := httptest.NewRecorder()
	mux.ServeHTTP(recAlpha, reqAlpha)

	if recAlpha.Code != http.StatusCreated {
		t.Fatalf("failed to publish in shop-alpha: %d %s", recAlpha.Code, recAlpha.Body.String())
	}

	// Step 2: Query versions in "shop-beta" -> MUST NOT see shop-alpha's version!
	reqBetaList := httptest.NewRequest(http.MethodGet, "/api/versions", nil)
	reqBetaList.Header.Set("X-Shop-Id", "shop-beta")
	recBetaList := httptest.NewRecorder()
	mux.ServeHTTP(recBetaList, reqBetaList)

	if recBetaList.Code != http.StatusOK {
		t.Fatalf("failed listing versions in shop-beta: %d", recBetaList.Code)
	}
	var betaVersions api.VersionListResponse
	_ = json.NewDecoder(recBetaList.Body).Decode(&betaVersions)
	if betaVersions.Total != 0 || len(betaVersions.Versions) != 0 {
		t.Fatalf("DATA LEAK: shop-beta can see %d versions from shop-alpha", betaVersions.Total)
	}

	// Step 3: Query diff of "patch-alpha-01" in "shop-beta" -> MUST return 404 Not Found!
	reqBetaDiff := httptest.NewRequest(http.MethodGet, "/api/versions/patch-alpha-01/diff", nil)
	reqBetaDiff.Header.Set("X-Shop-Id", "shop-beta")
	recBetaDiff := httptest.NewRecorder()
	mux.ServeHTTP(recBetaDiff, reqBetaDiff)

	if recBetaDiff.Code != http.StatusNotFound {
		t.Fatalf("TENANT LEAK: shop-beta accessed diff of shop-alpha version with status %d", recBetaDiff.Code)
	}
}
