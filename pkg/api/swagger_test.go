package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coderpush/pkg/api"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

func setupTestServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "catalog_test.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.Remove(dbPath)
	})

	broker := api.NewBroker()
	t.Cleanup(func() { broker.Close() })

	engine := catalogSync.NewEngine(store)
	srv := api.NewServer(store, engine, broker, "", api.WithDataDir(tempDir))
	return srv, tempDir
}

func TestSwaggerUIEndpoints(t *testing.T) {
	srv, _ := setupTestServer(t)

	// Test 1: GET /swagger/index.html returns 200 OK and HTML containing Swagger UI
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /swagger/index.html, got %d", rec.Code)
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("expected text/html, got %s", contentType)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "swagger-ui") {
		t.Errorf("expected body to contain swagger-ui")
	}

	// Test 2: GET /swagger/doc.json returns 200 OK and valid JSON OpenAPI spec
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil)
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /swagger/doc.json, got %d", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("failed to parse /swagger/doc.json: %v", err)
	}
	if doc["swagger"] != "2.0" {
		t.Errorf("expected swagger 2.0, got %v", doc["swagger"])
	}
	info, ok := doc["info"].(map[string]any)
	if !ok || info["title"] != "Resilient Catalog Synchronization Engine API" {
		t.Errorf("expected API title, got %v", info)
	}

	// Verify all 10 distinct API paths are documented
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("doc has no paths object")
	}
	expectedPaths := []string{
		"/api/jobs",
		"/api/jobs/{id}",
		"/api/jobs/{id}/resume",
		"/api/jobs/{id}/cancel",
		"/api/products",
		"/api/dead-letter",
		"/api/dead-letter/{id}/resolve",
		"/api/metrics",
		"/api/database/reset",
		"/api/events",
	}
	for _, p := range expectedPaths {
		if _, exists := paths[p]; !exists {
			t.Errorf("missing path %s in swagger doc.json", p)
		}
	}

	// Test 3: Redirect /swagger to /swagger/index.html (301)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/swagger", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("expected 301 redirect for /swagger, got %d", rec.Code)
	}

	// Test 4: Physical 404 for non-existent swagger sub-asset
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/swagger/nonexistent.xyz", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /swagger/nonexistent.xyz, got %d", rec.Code)
	}
}
