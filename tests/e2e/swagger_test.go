package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"coderpush/pkg/api"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

func createE2EServer(t *testing.T) *httptest.Server {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "e2e_catalog.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	broker := api.NewBroker(api.WithClientCap(64), api.WithPingInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Start(ctx)

	hook := api.NewProgressHook(broker)
	engine := catalogSync.NewEngine(store, catalogSync.WithProgressHook(hook))
	handler := api.NewServer(store, engine, broker, "", api.WithDataDir(tempDir))
	ts := httptest.NewServer(handler)

	t.Cleanup(func() {
		ts.Close()
		cancel()
		broker.Close()
		_ = store.Close()
		_ = os.Remove(dbPath)
	})
	return ts
}

func TestE2E_SwaggerUI_Available(t *testing.T) {
	ts := createE2EServer(t)

	resp, err := http.Get(ts.URL + "/swagger/index.html")
	if err != nil {
		t.Fatalf("failed to get /swagger/index.html: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %s", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "swagger-ui") {
		t.Errorf("expected body to contain swagger-ui")
	}
}

func TestE2E_SwaggerDocJSON_Valid(t *testing.T) {
	ts := createE2EServer(t)

	resp, err := http.Get(ts.URL + "/swagger/doc.json")
	if err != nil {
		t.Fatalf("failed to get /swagger/doc.json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("failed to decode doc.json: %v", err)
	}
	if doc["swagger"] != "2.0" {
		t.Errorf("expected swagger version 2.0, got %v", doc["swagger"])
	}
	info, ok := doc["info"].(map[string]any)
	if !ok || info["title"] != "Resilient Catalog Synchronization Engine API" {
		t.Errorf("unexpected info title: %v", info)
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("missing paths in doc.json")
	}
	requiredPaths := []string{
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
	for _, rp := range requiredPaths {
		if _, exists := paths[rp]; !exists {
			t.Errorf("expected endpoint %s in doc.json", rp)
		}
	}
}

func TestE2E_Swagger_Redirect(t *testing.T) {
	ts := createE2EServer(t)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(ts.URL + "/swagger")
	if err != nil {
		t.Fatalf("failed to get /swagger: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("expected 301 redirect for /swagger, got %d", resp.StatusCode)
	}
}
